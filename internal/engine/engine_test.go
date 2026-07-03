package engine

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestScanPath_testYAML(t *testing.T) {
	ctx := context.Background()
	eng, err := New(Options{
		KubernetesVersion: "1.30",
		SchemaValidation:  false,
		PolicyChecks:      true,
		CompatChecks:      false,
		MinSeverity:       "warning",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	path := filepath.Join("..", "..", "test.yaml")
	result, err := eng.ScanPath(ctx, path)
	if err != nil {
		t.Fatalf("ScanPath: %v", err)
	}
	if len(result.Documents) != 1 {
		t.Fatalf("documents = %d, want 1", len(result.Documents))
	}
	if len(result.Findings) < 2 {
		t.Fatalf("expected at least 2 findings, got %d", len(result.Findings))
	}

	ruleIDs := map[string]bool{}
	for _, f := range result.Findings {
		ruleIDs[f.RuleID] = true
		if f.GVKString == "" {
			t.Errorf("finding %s missing GVKString", f.RuleID)
		}
	}
	if !ruleIDs["pss-run-as-non-root"] {
		t.Error("expected pss-run-as-non-root finding")
	}
	if !ruleIDs["image-floating-tag"] {
		t.Error("expected image-floating-tag finding")
	}
}

func TestScanReportsSyntaxFindingsForBrokenYAML(t *testing.T) {
	ctx := context.Background()
	eng, err := New(Options{SchemaValidation: false, PolicyChecks: false, CompatChecks: false, MinSeverity: "warning"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Tab indentation breaks parsing; the syntax layer should surface it.
	result, err := eng.ScanBytes(ctx, "broken.yaml", []byte("metadata:\n\tname: x\n baz: y\n"))
	if err != nil {
		t.Fatalf("ScanBytes: %v", err)
	}
	if len(result.Findings) == 0 {
		t.Fatal("expected syntax findings for broken YAML")
	}
	found := false
	for _, f := range result.Findings {
		if f.Source == model.SourceSyntax && f.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a syntax finding with a line number, got %+v", result.Findings)
	}
}

func TestScanSkipsKubernetesChecksForPlainYAML(t *testing.T) {
	ctx := context.Background()
	eng, err := New(Options{SchemaValidation: false, PolicyChecks: true, CompatChecks: true, MinSeverity: "warning"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := eng.ScanBytes(ctx, "config.yaml", []byte("name: pipeline\nsteps:\n  - build\n"))
	if err != nil {
		t.Fatalf("ScanBytes: %v", err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("plain YAML should produce no findings, got %+v", result.Findings)
	}
}

func TestFilterFixable(t *testing.T) {
	findings := []struct {
		id   string
		fix  bool
		want bool
	}{
		{"pss-run-as-non-root", true, true},
		{"image-floating-tag", false, false},
	}
	var input []model.Finding
	for _, c := range findings {
		f := model.Finding{RuleID: c.id, SourceFile: "a.yaml", DocIndex: 0}
		if c.fix {
			f.Fix = &model.Fix{RuleID: c.id, Apply: func(u *unstructured.Unstructured) error { return nil }}
		}
		input = append(input, f)
	}
	out := FilterFixable(input)
	if len(out) != 1 {
		t.Fatalf("FilterFixable = %d, want 1", len(out))
	}
}

func TestFilterBySeverity(t *testing.T) {
	input := []model.Finding{
		{Severity: model.SeverityError},
		{Severity: model.SeverityWarning},
		{Severity: model.SeverityInfo},
	}
	out := FilterBySeverity(input, "warning")
	if len(out) != 2 {
		t.Fatalf("got %d findings, want 2", len(out))
	}
}
