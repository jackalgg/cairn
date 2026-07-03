package cluster

import (
	"context"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestDryRunApply_Integration exercises the real server-side dry-run path. It
// requires a reachable cluster and is skipped unless CAIRN_CLUSTER_TEST=1.
func TestDryRunApply_Integration(t *testing.T) {
	if os.Getenv("CAIRN_CLUSTER_TEST") != "1" {
		t.Skip("set CAIRN_CLUSTER_TEST=1 to run cluster integration tests")
	}
	client, err := NewClient("", "")
	if err != nil {
		t.Skipf("no cluster reachable: %v", err)
	}

	cm := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata": map[string]interface{}{
			"name":      "cairn-dryrun-test",
			"namespace": "default",
		},
		"data": map[string]interface{}{"hello": "world"},
	}}

	if err := client.DryRunApply(context.Background(), cm); err != nil {
		t.Fatalf("DryRunApply valid ConfigMap: %v", err)
	}

	bad := cm.DeepCopy()
	unstructured.RemoveNestedField(bad.Object, "metadata", "name")
	if err := client.DryRunApply(context.Background(), bad); err == nil {
		t.Fatal("expected error for object without metadata.name")
	}
}
