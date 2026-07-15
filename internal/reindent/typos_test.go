package reindent

import (
	"bytes"
	"strings"
	"testing"
)

// diagsContain reports whether any diagnostic message contains want.
func diagsContain(diags []Diagnostic, want string) bool {
	for _, d := range diags {
		if strings.Contains(d.Msg, want) {
			return true
		}
	}
	return false
}

func TestTypoUniqueDistanceOneFixed(t *testing.T) {
	// The flagship case: an extra letter in a PodSpec field. The fix must also
	// cascade — once `contaieners` becomes `containers`, the dashless item is
	// sub-case B for the marker pass and the whole file repairs to valid.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  contaieners:
    name: app
    image: nginx
`)
	out, changed, diags := Fix(in)
	if !changed {
		t.Fatalf("typo not fixed:\n%s", out)
	}
	if !diagsContain(diags, `field "contaieners" → "containers"`) {
		t.Errorf("missing fix diagnostic, got %v", diags)
	}
	valid(t, out)
	m := asMap(t, out)
	containers, ok := m["spec"].(map[string]interface{})["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("containers not repaired after typo fix:\n%s", out)
	}
}

func TestTypoTranspositionFixed(t *testing.T) {
	// Adjacent transposition is one edit (OSA distance): `naem` → `name`,
	// including on a list-item line.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  naem: app
spec:
  containers:
  - naem: app
    image: nginx
`)
	out, _, diags := FixTypos(in)
	if !diagsContain(diags, `field "naem" → "name" (in ObjectMeta)`) ||
		!diagsContain(diags, `field "naem" → "name" (in Container)`) {
		t.Errorf("transposition fixes missing, got %v", diags)
	}
	if !bytes.Contains(out, []byte("  name: app")) || !bytes.Contains(out, []byte("- name: app")) {
		t.Errorf("keys not renamed:\n%s", out)
	}
}

func TestTypoKindValueFixed(t *testing.T) {
	// A kind VALUE one edit from a known kind is repaired, and the corrected
	// kind unlocks the schema for the same run's field checks.
	in := []byte(`apiVersion: apps/v1
kind: Deploymet
metadata:
  name: web
spec:
  replics: 1
`)
	out, changed, diags := FixTypos(in)
	if !changed {
		t.Fatalf("kind typo not fixed:\n%s", out)
	}
	if !diagsContain(diags, `kind "Deploymet" → "Deployment"`) {
		t.Errorf("missing kind diagnostic, got %v", diags)
	}
	if !diagsContain(diags, `field "replics" → "replicas"`) {
		t.Errorf("kind fix did not unlock schema for field fix, got %v", diags)
	}
	if !bytes.Contains(out, []byte("kind: Deployment")) || !bytes.Contains(out, []byte("replicas: 1")) {
		t.Errorf("output not rewritten:\n%s", out)
	}
}

func TestTypoAmbiguousSuggestedNotFixed(t *testing.T) {
	// `hostIPD` is one edit from BOTH hostPID (transposition) and hostIPC
	// (substitution) — ambiguous, so suggest and leave the bytes alone.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  hostIPD: true
  containers:
  - name: app
    image: nginx
`)
	out, changed, diags := FixTypos(in)
	if changed {
		t.Errorf("ambiguous typo was edited:\n%s", out)
	}
	if !diagsContain(diags, `unknown field "hostIPD" in PodSpec — did you mean "hostIPC" or "hostPID"?`) {
		t.Errorf("missing ambiguity suggestion, got %v", diags)
	}
}

func TestTypoDistanceTwoSuggestedNotFixed(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    comandd: ["sh"]
`)
	out, changed, diags := FixTypos(in)
	if changed {
		t.Errorf("distance-2 typo was edited:\n%s", out)
	}
	if !diagsContain(diags, `unknown field "comandd" in Container — did you mean "command"?`) {
		t.Errorf("missing distance-2 suggestion, got %v", diags)
	}
}

func TestTypoStringMapScopesNeverFire(t *testing.T) {
	// Keys under labels/annotations/data are arbitrary — `naem` there is a
	// user's key, not a typo of ObjectMeta's name.
	in := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
  labels:
    naem: whatever
data:
  contaieners: "3"
`)
	out, changed, diags := FixTypos(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("stringMap keys were edited:\n%s", out)
	}
	if len(diags) != 0 {
		t.Errorf("stringMap keys produced diagnostics: %v", diags)
	}
}

func TestUnknownFieldWarnOnlyInCompleteTypes(t *testing.T) {
	// ObjectMeta's table is complete, so `metadata.run` warns (no edit).
	// Container's table is partial, so an unmodeled field there stays silent.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
  run: app
spec:
  containers:
  - name: app
    image: nginx
    terminationMessagePath: /dev/log
`)
	out, changed, diags := FixTypos(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("warn-only pass edited the file:\n%s", out)
	}
	if !diagsContain(diags, `unknown field "run" in ObjectMeta`) {
		t.Errorf("missing unknown-field warning, got %v", diags)
	}
	for _, d := range diags {
		if strings.Contains(d.Msg, "terminationMessagePath") {
			t.Errorf("partial type warned on a valid field: %v", d)
		}
	}
}

func TestTypoSpacedKeyFixed(t *testing.T) {
	// badindent.yaml's `res ources` — a space inside the key is one deletion
	// from `resources`.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    res ources: {}
`)
	out, _, diags := FixTypos(in)
	if !diagsContain(diags, `field "res ources" → "resources"`) {
		t.Errorf("spaced key not repaired, got %v", diags)
	}
	if !bytes.Contains(out, []byte("    resources: {}")) {
		t.Errorf("key not renamed in place:\n%s", out)
	}
}

func TestTypoValidFileUntouched(t *testing.T) {
	// Real fields one edit apart (clusterIP/clusterIPs) and unmodeled-but-valid
	// kinds must produce no edits and no noise.
	in := []byte(`apiVersion: v1
kind: Service
metadata:
  name: svc
spec:
  clusterIP: 10.0.0.1
  clusterIPs:
  - 10.0.0.1
  ports:
  - port: 80
---
apiVersion: v1
kind: Node
metadata:
  name: worker-1
`)
	out, changed, diags := FixTypos(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("valid file edited:\n%s", out)
	}
	if len(diags) != 0 {
		t.Errorf("valid file produced diagnostics: %v", diags)
	}
}

func TestTypoPipelineIdempotent(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  naem: app
spec:
  contaieners:
    name: app
    image: nginx
`)
	first, _, _ := Fix(in)
	valid(t, first)
	second, changed, _ := Fix(first)
	if changed || !bytes.Equal(first, second) {
		t.Errorf("pipeline not idempotent with typo pass:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"containers", "containers", 0},
		{"contaieners", "containers", 1}, // insertion
		{"naem", "name", 1},              // adjacent transposition
		{"lables", "labels", 1},          // adjacent transposition
		{"res ources", "resources", 1},   // deletion
		{"replics", "replicas", 1},
		{"comandd", "command", 2},
		{"run", "name", 3}, // capped
		{"Node", "Pod", 2},
	}
	for _, c := range cases {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
