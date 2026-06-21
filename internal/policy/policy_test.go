package policy

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackalgg/cairn/internal/parser"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRunAsNonRootRule(t *testing.T) {
	docs, err := parser.ParseFile(filepath.Join("..", "..", "test.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	doc := docs[0]
	rule := &RunAsNonRootRule{}

	findings := rule.Check(context.Background(), doc)
	if len(findings) == 0 {
		t.Fatal("expected findings for insecure deployment")
	}
	for _, f := range findings {
		if f.RuleID != "pss-run-as-non-root" {
			t.Errorf("ruleID = %q", f.RuleID)
		}
		if f.GVKString == "" {
			t.Error("GVKString should be set")
		}
		if f.Fix == nil {
			t.Error("expected fix")
		}
	}

	u := doc.Object.DeepCopy()
	if err := findings[0].Fix.Apply(u); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	val, found, err := unstructured.NestedBool(u.Object, "spec", "template", "spec", "securityContext", "runAsNonRoot")
	if err != nil || !found || !val {
		t.Errorf("runAsNonRoot = %v found=%v err=%v", val, found, err)
	}
}

func TestReadOnlyRootFSRule(t *testing.T) {
	docs, err := parser.ParseFile(filepath.Join("..", "..", "test.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	doc := docs[0]
	rule := &ReadOnlyRootFSRule{}
	findings := rule.Check(context.Background(), doc)
	if len(findings) == 0 {
		t.Fatal("expected findings")
	}
	u := doc.Object.DeepCopy()
	if err := findings[0].Fix.Apply(u); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	containers, found, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		t.Fatalf("containers: found=%v err=%v", found, err)
	}
	c0, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatal("container not a map")
	}
	sc, ok := c0["securityContext"].(map[string]interface{})
	if !ok {
		t.Fatal("securityContext missing")
	}
	if sc["readOnlyRootFilesystem"] != true {
		t.Error("readOnlyRootFilesystem not set")
	}
}

func TestFloatingTagRule(t *testing.T) {
	docs, err := parser.ParseFile(filepath.Join("..", "..", "test.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	doc := docs[0]
	rule := &FloatingTagRule{}
	findings := rule.Check(context.Background(), doc)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].RuleID != "image-floating-tag" {
		t.Errorf("ruleID = %q", findings[0].RuleID)
	}
	if findings[0].Fix != nil {
		t.Error("floating tag should not have auto-fix")
	}
}
