package fixer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestOutputPath_usesBasenameOnly(t *testing.T) {
	out := t.TempDir()
	path, err := outputPath(out, filepath.Join("..", "nested", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "manifest.yaml" {
		t.Fatalf("basename not preserved: %s", path)
	}
	rel, err := filepath.Rel(out, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("path escapes output dir: %s", path)
	}
}

func TestApply_usesDocumentKey(t *testing.T) {
	dir := t.TempDir()
	fileA := filepath.Join(dir, "a.yaml")
	fileB := filepath.Join(dir, "b.yaml")
	if err := os.WriteFile(fileA, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileB, []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &model.ScanResult{
		Documents: []model.Document{
			{
				Index:  0,
				Source: fileA,
				GVK:    schema.FromAPIVersionAndKind("v1", "ConfigMap"),
				Object: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "a"},
				}},
				RawYAML: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"),
			},
			{
				Index:  0,
				Source: fileB,
				GVK:    schema.FromAPIVersionAndKind("v1", "ConfigMap"),
				Object: unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "v1", "kind": "ConfigMap", "metadata": map[string]interface{}{"name": "b"},
				}},
				RawYAML: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n"),
			},
		},
	}
	findings := []model.Finding{{
		RuleID: "test", SourceFile: fileB, DocIndex: 0,
		Fix: &model.Fix{Apply: func(u *unstructured.Unstructured) error {
			return unstructured.SetNestedStringMap(u.Object, map[string]string{"fixed": "true"}, "metadata", "labels")
		}},
	}}

	outDir := filepath.Join(dir, "out")
	files, err := Apply(result, findings, Options{OutDir: outDir})
	if err != nil {
		t.Fatal(err)
	}
	var fixedB bool
	for _, fr := range files {
		if fr.SourceFile == fileB {
			fixedB = true
			if !strings.Contains(string(fr.After), "fixed") {
				t.Fatal("expected label fix in file b")
			}
		}
		if fr.SourceFile == fileA && strings.Contains(string(fr.After), "fixed") {
			t.Fatal("file a should not receive file b fix")
		}
	}
	if !fixedB {
		t.Fatal("expected file b to be fixed")
	}
}
