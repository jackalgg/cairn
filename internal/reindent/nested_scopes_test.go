package reindent

// Regression tests for the "ancestor steals a nested key" family of bugs: a
// key with a legitimate home inside a deeply-nested typed scope (exec.command,
// configMapKeyRef.name, …) was dedented out to a shallower ancestor that also
// declares that key name. Root cause: those scopes were typed as free-form
// stringMap placeholders, so placeKey's "specific ancestor beats wildcard"
// rule fired. The fix is schema data, not logic: model the scopes for real,
// so "top declares the key → stay" wins first.
//
// Every input here is VALID YAML — the pipeline must be a byte-exact no-op.

import (
	"bytes"
	"testing"
)

// mustNoOp asserts the full pipeline leaves a valid file byte-identical.
func mustNoOp(t *testing.T, name string, in []byte) {
	t.Helper()
	valid(t, in) // guard: the test input itself must be valid
	out, changed, diags := Fix(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("%s: valid file was modified:\n--- in ---\n%s\n--- out ---\n%s", name, in, out)
	}
	for _, d := range diags {
		t.Errorf("%s: unexpected diagnostic on valid file: %+v", name, d)
	}
}

func TestLifecycleExecCommandStays(t *testing.T) {
	mustNoOp(t, "lifecycle", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    lifecycle:
      preStop:
        exec:
          command:
          - sh
          - -c
          - sleep 5
      postStart:
        httpGet:
          path: /warmup
          port: 8080
`))
}

func TestProbeHandlersStay(t *testing.T) {
	mustNoOp(t, "probes", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    livenessProbe:
      exec:
        command:
        - cat
        - /tmp/healthy
      initialDelaySeconds: 5
    readinessProbe:
      httpGet:
        path: /ready
        port: 8080
        httpHeaders:
        - name: X-Probe
          value: ready
    startupProbe:
      tcpSocket:
        port: 8080
        host: localhost
`))
}

func TestEnvValueFromStays(t *testing.T) {
	mustNoOp(t, "valueFrom", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    env:
    - name: CFG
      valueFrom:
        configMapKeyRef:
          name: my-config
          key: level
    - name: PASS
      valueFrom:
        secretKeyRef:
          name: my-secret
          key: password
    - name: NODE
      valueFrom:
        fieldRef:
          fieldPath: spec.nodeName
    envFrom:
    - configMapRef:
        name: env-config
`))
}

func TestVolumeSourcesStay(t *testing.T) {
	mustNoOp(t, "volumes", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
  volumes:
  - name: data
    configMap:
      name: my-config
      items:
      - key: config.yaml
        path: config.yaml
  - name: creds
    secret:
      secretName: my-secret
  - name: scratch
    emptyDir:
      sizeLimit: 1Gi
  - name: host
    hostPath:
      path: /var/log
      type: Directory
  - name: pvc
    persistentVolumeClaim:
      claimName: my-claim
`))
}

func TestUnknownScopeKeysNotStolen(t *testing.T) {
	// The redis-chart corruption: volumeClaimTemplates holds full PVC objects
	// whose kind/metadata/spec the ROOT type also declares. Before the
	// unknown-scope humility rule (and StatefulSet modeling), those keys were
	// dedented to column 0, merging documents. The unnamed `extraStuff` scope
	// exercises the guard itself — no schema entry, keys must stay put.
	mustNoOp(t, "statefulset", []byte(`apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis
spec:
  serviceName: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: redis
  volumeClaimTemplates:
  - apiVersion: v1
    kind: PersistentVolumeClaim
    metadata:
      name: data
    spec:
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 8Gi
`))
	mustNoOp(t, "unknown-scope", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  extraStuff:
    kind: nested
    metadata: something
    command: not-a-container-command
  containers:
  - name: app
    image: nginx
`))
}

func TestStylePreservedOnValidFiles(t *testing.T) {
	// A working file in a non-canonical style (4-space indent, deep-indented
	// list items, over-spaced values) parses to the same tree either way —
	// cairn must leave it exactly as written.
	mustNoOp(t, "styled", []byte(`apiVersion: v1
kind: Pod
metadata:
    name: app
    labels:
        app:     web
spec:
    containers:
        - name: app
          image: nginx
          ports:
              - containerPort: 80
`))
}

func TestSemanticRepairStillFiresOnParseableFiles(t *testing.T) {
	// The style gate must NOT suppress real repairs on files that happen to
	// parse: dnsPolicy indented into the container parses fine but belongs to
	// the PodSpec — the tree changes, so the fix is kept.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    dnsPolicy: ClusterFirst
`)
	valid(t, in)
	out, changed, _ := Fix(in)
	if !changed {
		t.Fatalf("semantic repair suppressed by style gate:\n%s", out)
	}
	valid(t, out)
	m := asMap(t, out)
	spec := m["spec"].(map[string]interface{})
	if spec["dnsPolicy"] != "ClusterFirst" {
		t.Errorf("dnsPolicy not recovered to PodSpec: %v\n%s", spec, out)
	}
	c := spec["containers"].([]interface{})[0].(map[string]interface{})
	if _, still := c["dnsPolicy"]; still {
		t.Errorf("dnsPolicy still inside container\n%s", out)
	}
}

func TestItemFieldNotSwallowedByInlineKeyScope(t *testing.T) {
	// Corpus-caught (bitnami/nginx): `weight:` is a SIBLING of the inline
	// `- podAffinityTerm:` key inside the item. The inline key's scope used to
	// open at the marker's indent, so the equally-indented sibling read as
	// "deeper" and was absorbed into podAffinityTerm.
	mustNoOp(t, "affinity-weight", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - podAffinityTerm:
          topologyKey: kubernetes.io/hostname
        weight: 1
  containers:
  - name: app
    image: nginx
`))
}

func TestBlockScalarItemBodyPreserved(t *testing.T) {
	// Corpus-caught (bitnami/nginx): reindent walked the body of a bare `- |`
	// item as ordinary lines and flattened the script's inner indentation
	// (the pre-passes had this bug fixed; reindent itself did not).
	mustNoOp(t, "block-item", []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: bash
    args:
    - -ec
    - |
      if ! is_dir_empty /logs; then
        cp -r /logs /emptydir
      fi
`))
}

func TestNestedScopeRepairStillWorks(t *testing.T) {
	// The point of typing these scopes is placement, not just no-ops: a
	// mis-indented `command:` under exec must be pulled INTO exec (its real
	// parent), not out to the container.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
    livenessProbe:
      exec:
            command:
            - cat
            - /tmp/healthy
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	c := m["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})
	probe, ok := c["livenessProbe"].(map[string]interface{})
	if !ok {
		t.Fatalf("livenessProbe lost: %v\n%s", c, out)
	}
	exec, ok := probe["exec"].(map[string]interface{})
	if !ok {
		t.Fatalf("exec lost from probe: %v\n%s", probe, out)
	}
	cmd, ok := exec["command"].([]interface{})
	if !ok || len(cmd) != 2 {
		t.Errorf("command not nested under exec: %v\n%s", exec, out)
	}
	if _, escaped := c["command"]; escaped {
		t.Errorf("command escaped to container level\n%s", out)
	}
}
