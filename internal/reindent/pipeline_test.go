package reindent

import (
	"bytes"
	"strings"
	"testing"
)

func TestFixKitchenSink(t *testing.T) {
	// One file with every class of breakage the pipeline repairs: deprecated
	// apiVersion, lowercase kind, jammed and over-spaced colons, a missing list
	// marker, a lost-dash scalar item, and broken indentation.
	in := []byte(`apiversion: extensions/v1beta1
kind: deployment
metadata:
  name:legacy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: legacy
  template:
    metadata:
      labels:
        app: legacy
    spec:
   containers:
        name:   app
        image:nginx
        args:
        - sh
        - -c
          sleep 1d
`)
	out, changed, diags := Fix(in)
	if !changed {
		t.Fatalf("nothing repaired")
	}
	valid(t, out)
	m := asMap(t, out)
	if m["apiVersion"] != "apps/v1" || m["kind"] != "Deployment" {
		t.Errorf("identity wrong: %v/%v\n%s", m["apiVersion"], m["kind"], out)
	}
	podSpec := m["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})
	containers, ok := podSpec["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("containers wrong: %v\n%s", podSpec["containers"], out)
	}
	c := containers[0].(map[string]interface{})
	if c["name"] != "app" || c["image"] != "nginx" {
		t.Errorf("container fields wrong: %v\n%s", c, out)
	}
	args := c["args"].([]interface{})
	if len(args) != 3 || args[2] != "sleep 1d" {
		t.Errorf("args wrong: %v\n%s", args, out)
	}
	// The identity repairs must be narrated.
	joined := ""
	for _, d := range diags {
		joined += d.Msg + "\n"
	}
	if !strings.Contains(joined, "apps/v1") || !strings.Contains(joined, "Deployment") {
		t.Errorf("diagnostics missing identity repairs: %s", joined)
	}
}

func TestFixIdempotent(t *testing.T) {
	in := []byte(`apiversion: extensions/v1beta1
kind: deployment
metadata:
  name:legacy
spec:
  selector:
    matchLabels:
      app: legacy
  template:
    metadata:
      labels:
        app: legacy
    spec:
      containers:
        name: app
        image: nginx
`)
	first, _, _ := Fix(in)
	valid(t, first)
	second, changedAgain, _ := Fix(first)
	if changedAgain || !bytes.Equal(first, second) {
		t.Errorf("Fix not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// --- verifier tripwires -------------------------------------------------------
// The invariant is only real if the verifiers actually catch out-of-contract
// edits. Each case feeds a verifier a hand-corrupted "after" and expects a
// rejection, plus a legitimate edit and expects acceptance.

func TestVerifyReindent(t *testing.T) {
	before := []byte("a:\n    b: 1\n")
	if err := verifyReindent(before, []byte("a:\n  b: 1\n")); err != nil {
		t.Errorf("legit whitespace-only edit rejected: %v", err)
	}
	if err := verifyReindent(before, []byte("a:\n  b: 2\n")); err == nil {
		t.Errorf("content change not caught")
	}
	if err := verifyReindent(before, []byte("a:\n")); err == nil {
		t.Errorf("dropped line not caught")
	}
}

func TestVerifyColons(t *testing.T) {
	before := []byte("a:1\nb:   2\n")
	if err := verifyColons(before, []byte("a: 1\nb: 2\n")); err != nil {
		t.Errorf("legit colon-gap edit rejected: %v", err)
	}
	if err := verifyColons(before, []byte("a: 1\nb: 3\n")); err == nil {
		t.Errorf("value change not caught")
	}
}

func TestVerifyMarkers(t *testing.T) {
	before := []byte("items:\n  name: a\n")
	if err := verifyMarkers(before, []byte("items:\n- name: a\n")); err != nil {
		t.Errorf("legit marker insertion rejected: %v", err)
	}
	if err := verifyMarkers(before, []byte("items:\n- name: b\n")); err == nil {
		t.Errorf("content change alongside marker not caught")
	}
	if err := verifyMarkers(before, []byte("items:\n- - name: a\n")); err == nil {
		t.Errorf("double marker not caught")
	}
}

func TestVerifyIdentity(t *testing.T) {
	before := []byte("apiversion: extensions/v1beta1\nkind: deployment\nmetadata:\n  name: x\n")
	// Legit: rewrite both identity lines.
	after := []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n")
	if err := verifyIdentity(before, after); err != nil {
		t.Errorf("legit identity rewrite rejected: %v", err)
	}
	// Legit: insertion of a missing apiVersion.
	if err := verifyIdentity(
		[]byte("kind: Pod\nmetadata:\n  name: x\n"),
		[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n")); err != nil {
		t.Errorf("legit insertion rejected: %v", err)
	}
	// Out of contract: a non-identity line was edited.
	if err := verifyIdentity(before,
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: y\n")); err == nil {
		t.Errorf("non-identity edit not caught")
	}
	// Out of contract: a line was deleted.
	if err := verifyIdentity(before,
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n")); err == nil {
		t.Errorf("deleted line not caught")
	}
}
