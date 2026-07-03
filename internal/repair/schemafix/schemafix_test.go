package schemafix

import (
	"testing"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassifyMessage_unknownField(t *testing.T) {
	got := ClassifyMessage(`unknown field "notARealField"`)
	if got != "schema-unknown-field" {
		t.Fatalf("got %q", got)
	}
}

func TestFixForFinding_removeUnknownField(t *testing.T) {
	doc := model.Document{
		Object: unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata":   map[string]interface{}{"name": "x"},
			"spec": map[string]interface{}{
				"notARealField": true,
				"containers":    []interface{}{},
			},
		}},
		GVK: schema.FromAPIVersionAndKind("v1", "Pod"),
	}
	f := model.Finding{
		RuleID:  "schema-unknown-field",
		Message: `unknown field "notARealField"`,
		Path:    "spec.notARealField",
	}
	fix := FixForFinding(doc, f)
	if fix == nil || fix.Apply == nil {
		t.Fatal("expected fix")
	}
	u := doc.Object.DeepCopy()
	if err := fix.Apply(u); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := unstructured.NestedFieldNoCopy(u.Object, "spec", "notARealField"); found {
		t.Fatal("field should be removed")
	}
}
