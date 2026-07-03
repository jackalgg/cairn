package generator

import (
	"context"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/policy"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var metadataFieldsToRemove = []string{
	"uid",
	"resourceVersion",
	"generation",
	"creationTimestamp",
	"managedFields",
	"selfLink",
}

func Clean(doc *model.Document) {
	u := &doc.Object
	unstructured.RemoveNestedField(u.Object, "status")
	if meta, ok, _ := unstructured.NestedMap(u.Object, "metadata"); ok {
		for _, field := range metadataFieldsToRemove {
			delete(meta, field)
		}
		unstructured.SetNestedMap(u.Object, meta, "metadata")
	}
	removeDefaultServiceClusterIP(u)
}

func removeDefaultServiceClusterIP(u *unstructured.Unstructured) {
	if u.GetKind() != "Service" {
		return
	}
	spec, ok, _ := unstructured.NestedMap(u.Object, "spec")
	if !ok {
		return
	}
	if specType, _ := spec["type"].(string); specType == "" || specType == "ClusterIP" {
		if clusterIP, _ := spec["clusterIP"].(string); clusterIP != "" {
			delete(spec, "clusterIP")
			unstructured.SetNestedMap(u.Object, spec, "spec")
		}
	}
}

func ApplyDefaults(doc *model.Document) error {
	u := &doc.Object
	if !policyApplies(u) {
		return nil
	}
	runAsRule := &policy.RunAsNonRootRule{}
	for _, f := range runAsRule.Check(context.Background(), *doc) {
		if f.Fix != nil && f.Fix.Apply != nil {
			if err := f.Fix.Apply(u); err != nil {
				return err
			}
		}
	}
	roRule := &policy.ReadOnlyRootFSRule{}
	for _, f := range roRule.Check(context.Background(), *doc) {
		if f.Fix != nil && f.Fix.Apply != nil {
			if err := f.Fix.Apply(u); err != nil {
				return err
			}
		}
	}
	return nil
}

func policyApplies(u *unstructured.Unstructured) bool {
	kind := u.GetKind()
	switch kind {
	case "Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job", "CronJob":
		return true
	default:
		return false
	}
}
