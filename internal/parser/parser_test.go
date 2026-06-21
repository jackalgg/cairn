package parser

import (
	"path/filepath"
	"testing"
)

func TestParseTestYAML(t *testing.T) {
	path := filepath.Join("..", "..", "test.yaml")
	docs, err := ParseFile(path)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	doc := docs[0]
	if doc.GVK.Kind != "Deployment" {
		t.Errorf("kind = %q, want Deployment", doc.GVK.Kind)
	}
	if doc.GVK.GroupVersion().String() != "apps/v1" {
		t.Errorf("apiVersion = %q, want apps/v1", doc.GVK.GroupVersion().String())
	}
	if doc.Name() != "insecure-app" {
		t.Errorf("name = %q, want insecure-app", doc.Name())
	}
	containers, found, err := unstructuredNestedSlice(doc.Object.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		t.Fatalf("containers not found: found=%v err=%v", found, err)
	}
	c0, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatal("container is not a map")
	}
	if c0["image"] != "nginx:latest" {
		t.Errorf("image = %v, want nginx:latest", c0["image"])
	}
}

func TestParseMultiDoc(t *testing.T) {
	input := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cm1
---
apiVersion: v1
kind: Secret
metadata:
  name: secret1
`)
	docs, err := Parse("multi.yaml", input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(docs))
	}
	if docs[0].GVK.Kind != "ConfigMap" {
		t.Errorf("doc0 kind = %q", docs[0].GVK.Kind)
	}
	if docs[1].GVK.Kind != "Secret" {
		t.Errorf("doc1 kind = %q", docs[1].GVK.Kind)
	}
}

func unstructuredNestedSlice(obj map[string]interface{}, fields ...string) ([]interface{}, bool, error) {
	cur := interface{}(obj)
	for _, f := range fields {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		v, ok := m[f]
		if !ok {
			return nil, false, nil
		}
		cur = v
	}
	s, ok := cur.([]interface{})
	return s, ok, nil
}
