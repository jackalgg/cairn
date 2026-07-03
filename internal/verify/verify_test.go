package verify

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type stubRunner struct {
	fn func(doc model.Document) error
}

func (s stubRunner) DryRun(_ context.Context, doc model.Document) error { return s.fn(doc) }
func (s stubRunner) Backend() string                                    { return "stub" }

func k8sDoc(index int, name string, obj map[string]interface{}) model.Document {
	obj["apiVersion"] = "v1"
	obj["kind"] = "Pod"
	meta, _ := obj["metadata"].(map[string]interface{})
	if meta == nil {
		meta = map[string]interface{}{}
	}
	meta["name"] = name
	obj["metadata"] = meta
	return model.Document{
		Index:  index,
		GVK:    schema.GroupVersionKind{Version: "v1", Kind: "Pod"},
		Object: unstructured.Unstructured{Object: obj},
	}
}

func TestVerifyReportsPerDocument(t *testing.T) {
	runner := stubRunner{fn: func(doc model.Document) error {
		if doc.Name() == "bad" {
			return fmt.Errorf("admission denied: runAsNonRoot != true")
		}
		return nil
	}}
	v := New(runner)
	docs := []model.Document{
		k8sDoc(0, "good", map[string]interface{}{}),
		k8sDoc(1, "bad", map[string]interface{}{}),
	}
	results := v.Verify(context.Background(), docs)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if !results[0].OK {
		t.Errorf("doc 0 should pass")
	}
	if results[1].OK {
		t.Errorf("doc 1 should fail")
	}
	if AllOK(results) {
		t.Error("AllOK should be false")
	}
	if FailureText(results) == "" {
		t.Error("FailureText should carry the rejection reason")
	}
}

func TestVerifySkipsNonKubernetes(t *testing.T) {
	runner := stubRunner{fn: func(doc model.Document) error { return fmt.Errorf("should not be called") }}
	v := New(runner)
	plain := model.Document{Index: 0, Object: unstructured.Unstructured{Object: map[string]interface{}{"name": "x"}}}
	if results := v.Verify(context.Background(), []model.Document{plain}); len(results) != 0 {
		t.Fatalf("expected non-Kubernetes doc to be skipped, got %+v", results)
	}
}

func TestUnavailableVerifier(t *testing.T) {
	v := New(nil)
	if v.Available() {
		t.Error("nil runner should be unavailable")
	}
	if v.Backend() != "none" {
		t.Errorf("Backend = %q, want none", v.Backend())
	}
	if results := v.Verify(context.Background(), []model.Document{k8sDoc(0, "x", map[string]interface{}{})}); results != nil {
		t.Errorf("unavailable verifier should return nil results, got %+v", results)
	}
}

// TestConvergenceLoop mirrors the fix command's verify/repair loop: the runner
// rejects the document until a repair sets the required field, after which a
// re-verify passes. It proves the loop converges and stops making progress when
// it cannot fix further.
func TestConvergenceLoop(t *testing.T) {
	runner := stubRunner{fn: func(doc model.Document) error {
		ok, _, _ := unstructured.NestedBool(doc.Object.Object, "spec", "securityContext", "runAsNonRoot")
		if !ok {
			return fmt.Errorf("admission denied: runAsNonRoot must be true")
		}
		return nil
	}}
	v := New(runner)

	doc := k8sDoc(0, "app", map[string]interface{}{"spec": map[string]interface{}{}})
	docs := []model.Document{doc}

	applyFix := func(d model.Document) model.Document {
		_ = unstructured.SetNestedField(d.Object.Object, true, "spec", "securityContext", "runAsNonRoot")
		return d
	}

	const maxRounds = 3
	converged := false
	for round := 0; round < maxRounds; round++ {
		results := v.Verify(context.Background(), docs)
		if AllOK(results) {
			converged = true
			break
		}
		docs[0] = applyFix(docs[0])
	}
	if !converged {
		t.Fatal("expected loop to converge after applying the fix")
	}
}

func TestNilRunnerConstructors(t *testing.T) {
	if NewClusterRunner(nil) != nil {
		t.Error("NewClusterRunner(nil) should be nil")
	}
	if NewSchemaRunner(nil) != nil {
		t.Error("NewSchemaRunner(nil) should be nil")
	}
}
