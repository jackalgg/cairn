package reindent

import (
	"bytes"
	"strings"
	"testing"
)

func TestIdentityDeprecatedAPIVersion(t *testing.T) {
	in := []byte(`apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: app
`)
	out, changed, diags := FixIdentity(in)
	if !changed {
		t.Fatalf("deprecated apiVersion not repaired")
	}
	if !bytes.Contains(out, []byte("apiVersion: apps/v1\n")) {
		t.Errorf("apiVersion not canonical:\n%s", out)
	}
	if len(diags) != 1 || diags[0].Line != 1 || !strings.Contains(diags[0].Msg, "apps/v1") {
		t.Errorf("diagnostic wrong: %+v", diags)
	}
}

func TestIdentityKindCasingUnlocksSchema(t *testing.T) {
	// Lowercase kind is fixed, and — because identity runs first — the schema
	// then applies: the collapsed containers block is restored under spec.
	in := []byte(`apiversion: v1
kind: pod
metadata:
  name: app
spec:
containers:
- name: app
  image: nginx
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	if m["kind"] != "Pod" || m["apiVersion"] != "v1" {
		t.Errorf("identity not repaired: kind=%v api=%v\n%s", m["kind"], m["apiVersion"], out)
	}
	spec, ok := m["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec missing:\n%s", out)
	}
	if _, ok := spec["containers"].([]interface{}); !ok {
		t.Errorf("containers not restored under spec (schema not unlocked):\n%s", out)
	}
}

func TestIdentityInsertsMissingAPIVersion(t *testing.T) {
	in := []byte(`# comment stays first
kind: Pod
metadata:
  name: app
`)
	out, changed, diags := FixIdentity(in)
	if !changed {
		t.Fatalf("missing apiVersion not inserted")
	}
	lines := strings.Split(string(out), "\n")
	if lines[0] != "# comment stays first" || lines[1] != "apiVersion: v1" || lines[2] != "kind: Pod" {
		t.Errorf("insertion misplaced:\n%s", out)
	}
	if len(diags) != 1 || !strings.Contains(diags[0].Msg, "inserted") {
		t.Errorf("diagnostic wrong: %+v", diags)
	}
}

func TestIdentityPreservesComment(t *testing.T) {
	in := []byte(`apiVersion: apps/v1beta2 # legacy note
kind: Deployment
metadata:
  name: app
`)
	out, _, _ := FixIdentity(in)
	if !bytes.Contains(out, []byte("apiVersion: apps/v1 # legacy note\n")) {
		t.Errorf("trailing comment lost:\n%s", out)
	}
}

func TestIdentityLeavesUnknownKinds(t *testing.T) {
	// CRDs and non-Kubernetes YAML must be untouched: no kind match, no edits.
	inputs := [][]byte{
		[]byte("apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: app\n"),
		[]byte("name: ci\non: push\njobs:\n  build:\n    steps:\n    - run: make\n"),
	}
	for i, in := range inputs {
		out, changed, _ := FixIdentity(in)
		if changed || !bytes.Equal(out, in) {
			t.Errorf("case %d: unknown document was modified:\n%s", i, out)
		}
	}
}

func TestIdentityMultiDocument(t *testing.T) {
	in := []byte(`kind: Pod
metadata:
  name: a
---
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: b
`)
	out, _, diags := FixIdentity(in)
	if !bytes.Contains(out, []byte("apiVersion: v1\nkind: Pod")) {
		t.Errorf("first doc not repaired:\n%s", out)
	}
	if !bytes.Contains(out, []byte("apiVersion: apps/v1\nkind: Deployment")) {
		t.Errorf("second doc not repaired:\n%s", out)
	}
	// Line numbers must be absolute in the input, per doc.
	if len(diags) != 2 || diags[0].Line != 1 || diags[1].Line != 5 {
		t.Errorf("diagnostic lines wrong: %+v", diags)
	}
}

func TestIdentityNoOpOnCanonicalFile(t *testing.T) {
	in := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
`)
	out, changed, diags := FixIdentity(in)
	if changed || !bytes.Equal(out, in) || len(diags) != 0 {
		t.Errorf("canonical file modified (changed=%v diags=%+v):\n%s", changed, diags, out)
	}
}

func TestIdentityDoesNotTouchNestedKindLines(t *testing.T) {
	// A `kind:` deeper than column 0 (e.g. in a volume or roleRef) is not the
	// document's identity and must be left alone.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  volumes:
  - name: v
    downwardAPI:
      kind: something
`)
	out, changed, _ := FixIdentity(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("nested kind line was modified:\n%s", out)
	}
}
