package reindent

import (
	"bytes"
	"testing"

	yaml "go.yaml.in/yaml/v3"
)

func valid(t *testing.T, data []byte) {
	t.Helper()
	var v interface{}
	if err := yaml.Unmarshal(data, &v); err != nil {
		t.Fatalf("result is not valid YAML: %v\n---\n%s", err, data)
	}
}

// asMap round-trips through YAML so tests can assert on structure rather than
// exact whitespace.
func asMap(t *testing.T, data []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("not a mapping: %v\n%s", err, data)
	}
	return m
}

func TestBrokenDeployment(t *testing.T) {
	// spec's children collapsed to column 0, plus an odd-indented `spec:` and
	// an over-indented `template:`. Classic cascading indentation failure.
	in := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: insecure-app
spec:
replicas: 1
selector:
        matchLabels:
          app: insecure
            template:
    metadata:
      labels:
        app: insecure
     spec:
      containers:
      - name: app
        image: nginx:latest
`)
	out, _ := Reindent(in)
	valid(t, out)

	m := asMap(t, out)
	spec, ok := m["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec is not a mapping:\n%s", out)
	}
	if spec["replicas"] != 1 {
		t.Errorf("spec.replicas = %v, want 1", spec["replicas"])
	}
	tmpl, ok := spec["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec.template missing:\n%s", out)
	}
	podSpec, ok := tmpl["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec.template.spec missing:\n%s", out)
	}
	containers, ok := podSpec["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("containers not a 1-element list: %v\n%s", podSpec["containers"], out)
	}
	c := containers[0].(map[string]interface{})
	if c["name"] != "app" || c["image"] != "nginx:latest" {
		t.Errorf("container = %v, want name=app image=nginx:latest", c)
	}
}

func TestPodSpecFieldDedent(t *testing.T) {
	// dnsPolicy/restartPolicy are PodSpec fields left buried at the wrong indent
	// after the containers list. Schema knowledge must pull them back to spec.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  labels:
    run: bad-indents2
  name: bad-indents2
spec:
   containers:
  - image: busybox
    name: bad-indents2
    resources: {}
   dnsPolicy: ClusterFirst
  restartPolicy: Always
status: {}
`)
	out, _ := Reindent(in)
	valid(t, out)

	m := asMap(t, out)
	spec := m["spec"].(map[string]interface{})
	if _, ok := spec["dnsPolicy"]; !ok {
		t.Errorf("dnsPolicy not restored to spec:\n%s", out)
	}
	if _, ok := spec["restartPolicy"]; !ok {
		t.Errorf("restartPolicy not restored to spec:\n%s", out)
	}
	containers, ok := spec["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("containers wrong: %v\n%s", spec["containers"], out)
	}
	c := containers[0].(map[string]interface{})
	if _, stray := c["dnsPolicy"]; stray {
		t.Errorf("dnsPolicy wrongly nested inside container:\n%s", out)
	}
	if c["name"] != "bad-indents2" {
		t.Errorf("container name = %v\n%s", c["name"], out)
	}
}

func TestOddSiblingIndents(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key1: val1
   key2: val2
  key3: val3
`)
	out, _ := Reindent(in)
	valid(t, out)
	m := asMap(t, out)
	data := m["data"].(map[string]interface{})
	for _, k := range []string{"key1", "key2", "key3"} {
		if _, ok := data[k]; !ok {
			t.Errorf("data.%s missing:\n%s", k, out)
		}
	}
}

func TestPlainYAMLRenormalizes(t *testing.T) {
	// No Kubernetes kind: purely structural reindent should still normalize
	// inconsistent widths while preserving nesting intent.
	in := []byte(`root:
    a: 1
    b:
          c: 2
    d: 3
`)
	out, _ := Reindent(in)
	valid(t, out)
	m := asMap(t, out)
	root := m["root"].(map[string]interface{})
	if root["a"] != 1 || root["d"] != 3 {
		t.Errorf("siblings lost: %v\n%s", root, out)
	}
	b := root["b"].(map[string]interface{})
	if b["c"] != 2 {
		t.Errorf("nested c lost: %v\n%s", root, out)
	}
}

func TestValidFileIsIdempotent(t *testing.T) {
	in := []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: app
  template:
    metadata:
      labels:
        app: app
    spec:
      containers:
      - name: app
        image: nginx:latest
        ports:
        - containerPort: 80
`)
	out, changed := Reindent(in)
	if changed {
		t.Errorf("already-valid file was modified:\n%s", out)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("output differs from input:\n%s", out)
	}
}

func TestBlockScalarPreserved(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  script: |
    #!/bin/sh
    echo hello
      indented line
`)
	out, _ := Reindent(in)
	valid(t, out)
	if !bytes.Contains(out, []byte("    #!/bin/sh\n")) {
		t.Errorf("block scalar body not preserved:\n%s", out)
	}
	if !bytes.Contains(out, []byte("      indented line")) {
		t.Errorf("block scalar internal indentation lost:\n%s", out)
	}
}

func TestListItemsAlignedWithKey(t *testing.T) {
	// Non-Kubernetes file (GitHub Actions) whose sequence items are indented
	// deeper than their key. Reindenting must keep it valid and be idempotent.
	in := []byte(`name: ci-pipeline
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make build
      - run: make test
`)
	out, _ := Reindent(in)
	valid(t, out)

	m := asMap(t, out)
	jobs := m["jobs"].(map[string]interface{})
	build := jobs["build"].(map[string]interface{})
	steps, ok := build["steps"].([]interface{})
	if !ok || len(steps) != 2 {
		t.Fatalf("steps not a 2-element list: %v\n%s", build["steps"], out)
	}
}

func TestIdempotent(t *testing.T) {
	// Reindenting an already-canonical file must be a no-op. This is what makes
	// `--check` safe to use in CI.
	inputs := [][]byte{
		[]byte("name: ci\non: push\njobs:\n  build:\n    steps:\n    - run: make\n"),
		[]byte("apiVersion: v1\nkind: Pod\nmetadata:\n  name: p\nspec:\n  containers:\n  - name: c\n    image: nginx\n"),
		[]byte("a:\n  b:\n  - 1\n  - 2\n  c: 3\n"),
	}
	for i, in := range inputs {
		first, _ := Reindent(in)
		valid(t, first)
		second, changedAgain := Reindent(first)
		if changedAgain || !bytes.Equal(first, second) {
			t.Errorf("case %d not idempotent:\nfirst:\n%s\nsecond:\n%s", i, first, second)
		}
	}
}

func TestMultiDocument(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: a
spec:
containers:
- name: a
  image: nginx
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: b
data:
  k: v
`)
	out, _ := Reindent(in)
	valid(t, out)
	if !bytes.Contains(out, []byte("\n---\n")) {
		t.Errorf("document separator lost:\n%s", out)
	}
}
