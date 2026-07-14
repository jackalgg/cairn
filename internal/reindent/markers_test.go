package reindent

import (
	"bytes"
	"testing"
)

// repair runs the full verified fix pipeline the way cmd/fix does, so these
// tests exercise the passes in composition.
func repair(data []byte) []byte {
	out, _, _ := Fix(data)
	return out
}

func TestMissingMarkerSingleItem(t *testing.T) {
	// Sub-case B: containers is a sequence (schema), but its single item lost the
	// dash. The nested ports item lost its dash too — both must be restored.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
    name: app
    image: nginx:1.25
    ports:
      containerPort: 80
`)
	out := repair(in)
	valid(t, out)

	m := asMap(t, out)
	spec := m["spec"].(map[string]interface{})
	containers, ok := spec["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("containers not a 1-element list: %v\n%s", spec["containers"], out)
	}
	c := containers[0].(map[string]interface{})
	if c["name"] != "app" || c["image"] != "nginx:1.25" {
		t.Errorf("container = %v, want name=app image=nginx:1.25\n%s", c, out)
	}
	ports, ok := c["ports"].([]interface{})
	if !ok || len(ports) != 1 {
		t.Fatalf("nested ports not a 1-element list: %v\n%s", c["ports"], out)
	}
}

func TestMissingMarkerSplitsItems(t *testing.T) {
	// Sub-case C: two containers, but the second lost its dash. The repeated
	// `name` key is the boundary signal that starts a new item.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: a
    image: nginx
    name: b
    image: redis
`)
	out := repair(in)
	valid(t, out)

	m := asMap(t, out)
	spec := m["spec"].(map[string]interface{})
	containers, ok := spec["containers"].([]interface{})
	if !ok || len(containers) != 2 {
		t.Fatalf("containers not a 2-element list: %v\n%s", spec["containers"], out)
	}
	c0 := containers[0].(map[string]interface{})
	c1 := containers[1].(map[string]interface{})
	if c0["name"] != "a" || c0["image"] != "nginx" {
		t.Errorf("container[0] = %v, want name=a image=nginx\n%s", c0, out)
	}
	if c1["name"] != "b" || c1["image"] != "redis" {
		t.Errorf("container[1] = %v, want name=b image=redis\n%s", c1, out)
	}
}

func TestScalarSequenceUntouched(t *testing.T) {
	// A correctly-dashed scalar sequence must be left exactly as-is.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: busybox
    args:
    - sh
    - -c
    - sleep 1d
`)
	out, changed := InsertMarkers(in)
	if changed {
		t.Errorf("InsertMarkers touched a scalar sequence:\n%s", out)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("scalar-sequence input was modified:\n%s", out)
	}
}

func TestScalarSequenceLostDash(t *testing.T) {
	// Sub-case D (schema-confirmed scalar seqs only): a bare `sleep 1d` under
	// args — indented as if it were a continuation of `-c` — is a scalar item
	// that lost its dash and must be restored to a separate element.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - args:
    - sh
    - -c
      sleep 1d
    image: ubuntu
    name: app
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	c := m["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})
	args := c["args"].([]interface{})
	if len(args) != 3 || args[0] != "sh" || args[1] != "-c" || args[2] != "sleep 1d" {
		t.Errorf("args not split into 3 items: %v\n%s", args, out)
	}
	if c["image"] != "ubuntu" || c["name"] != "app" {
		t.Errorf("container fields lost after args repair: %v\n%s", c, out)
	}
}

func TestBlockScalarItemUnderScalarSeq(t *testing.T) {
	// A bare block-scalar item (`- |`) under args must keep its body as one
	// element; its lines must NOT become separate lost-dash items.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: c
    image: busybox
    args:
    - sh
    - -c
    - |
      echo hi
      sleep 1d
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	args := m["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})["args"].([]interface{})
	if len(args) != 3 {
		t.Fatalf("block scalar item was split: %v\n%s", args, out)
	}
	if args[2] != "echo hi\nsleep 1d\n" {
		t.Errorf("block scalar body corrupted: %q\n%s", args[2], out)
	}
}

func TestUnknownSequenceNotSplit(t *testing.T) {
	// `steps` is not a schema-known scalar sequence, so a dashless `deploy`
	// under it must be left alone rather than guessed into an item.
	in := []byte(`steps:
- build
- test
  deploy
`)
	_, changed := InsertMarkers(in)
	if changed {
		t.Errorf("InsertMarkers split an unknown (non-schema) sequence")
	}
}

func TestFreeformSequenceUntouched(t *testing.T) {
	// tolerations is a sequence of free-form maps (stringMap element). Its keys
	// are arbitrary, so there is no schema signal to insert a marker — defer.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  tolerations:
    key: node
    operator: Exists
  containers:
  - name: c
    image: nginx
`)
	_, changed := InsertMarkers(in)
	if changed {
		t.Errorf("InsertMarkers inserted a marker into a free-form sequence")
	}
}

func TestMarkerRepairIsNoOpOnValidFile(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: a
    image: nginx
    ports:
    - containerPort: 80
  - name: b
    image: redis
`)
	out, changed := InsertMarkers(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("valid file changed by marker repair:\n%s", out)
	}
}

func TestMarkerRepairIdempotent(t *testing.T) {
	inputs := [][]byte{
		[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: app\nspec:\n  containers:\n    name: app\n    image: nginx\n"),
		[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: app\nspec:\n  containers:\n  - name: a\n    image: x\n    name: b\n    image: y\n"),
	}
	for i, in := range inputs {
		first := repair(in)
		valid(t, first)
		second := repair(first)
		if !bytes.Equal(first, second) {
			t.Errorf("case %d not idempotent:\nfirst:\n%s\nsecond:\n%s", i, first, second)
		}
	}
}
