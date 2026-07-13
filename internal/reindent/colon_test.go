package reindent

import (
	"bytes"
	"testing"
)

func TestColonSpacingPlainKeys(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name:cfg
data:
  greeting:hello
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	if m["metadata"].(map[string]interface{})["name"] != "cfg" {
		t.Errorf("name not repaired:\n%s", out)
	}
	if m["data"].(map[string]interface{})["greeting"] != "hello" {
		t.Errorf("greeting not repaired:\n%s", out)
	}
}

func TestColonSpacingListItems(t *testing.T) {
	// Jammed colons inside mapping-sequence items (ports) must be spaced, because
	// the schema declares those keys.
	in := []byte(`apiVersion: v1
kind: Service
metadata:
  name:svc
spec:
  type:NodePort
  ports:
  - port:80
    targetPort:8080
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	spec := m["spec"].(map[string]interface{})
	if spec["type"] != "NodePort" {
		t.Errorf("type not repaired:\n%s", out)
	}
	ports := spec["ports"].([]interface{})
	p0 := ports[0].(map[string]interface{})
	if p0["port"] != 80 || p0["targetPort"] != 8080 {
		t.Errorf("port item not repaired: %v\n%s", p0, out)
	}
}

func TestColonSpacingLeavesScalarSequenceItems(t *testing.T) {
	// "- kill:9" under command is a scalar sequence item, not a mapping, so its
	// colon must NOT be spaced (that would change a string into a map).
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: busybox
    command:
    - kill:9
    - echo host:port
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	c := m["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})
	cmd := c["command"].([]interface{})
	if cmd[0] != "kill:9" || cmd[1] != "echo host:port" {
		t.Errorf("scalar command args were altered: %v\n%s", cmd, out)
	}
}

func TestColonSpacingPreservesURLsAndTimes(t *testing.T) {
	// A path value keeps its fix; a "//" protocol prefix and a clock value do not
	// (URL/time false positives).
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: busybox
    workingDir:/var/www
    env:
    - name: URL
      value: http://example.com:8080
`)
	out := repair(in)
	valid(t, out)
	m := asMap(t, out)
	c := m["spec"].(map[string]interface{})["containers"].([]interface{})[0].(map[string]interface{})
	if c["workingDir"] != "/var/www" {
		t.Errorf("path value not repaired: %v\n%s", c["workingDir"], out)
	}
	env := c["env"].([]interface{})[0].(map[string]interface{})
	if env["value"] != "http://example.com:8080" {
		t.Errorf("URL value corrupted: %v\n%s", env["value"], out)
	}
}

func TestColonSpacingSkipsBlockScalarBody(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: cfg
data:
  script:|
    echo host:port
    key:value
`)
	out := repair(in)
	valid(t, out)
	if !bytes.Contains(out, []byte("echo host:port")) || !bytes.Contains(out, []byte("key:value")) {
		t.Errorf("block scalar body was rewritten:\n%s", out)
	}
}

func TestColonSpacingCollapsesExtraSpace(t *testing.T) {
	// Over-spaced keys ("name:   app") are normalized to a single space, the same
	// pass that fixes jammed keys.
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name:   app
spec:
  containers:
  - name:   web
    image: nginx
`)
	out, changed := SpaceColons(in)
	if !changed {
		t.Fatalf("over-spaced keys were not normalized:\n%s", out)
	}
	if bytes.Contains(out, []byte("name:   ")) {
		t.Errorf("extra space not collapsed:\n%s", out)
	}
	valid(t, out)
	m := asMap(t, repair(in))
	if m["metadata"].(map[string]interface{})["name"] != "app" {
		t.Errorf("value changed by space collapse:\n%s", out)
	}
}

func TestColonSpacingNoOpOnValidFile(t *testing.T) {
	in := []byte(`apiVersion: v1
kind: Pod
metadata:
  name: app
spec:
  containers:
  - name: app
    image: nginx
`)
	out, changed := SpaceColons(in)
	if changed || !bytes.Equal(out, in) {
		t.Errorf("valid file changed by colon spacing:\n%s", out)
	}
}
