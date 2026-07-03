package model

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Document struct {
	Index   int
	GVK     GroupVersionKind
	Object  unstructured.Unstructured
	RawYAML []byte
	Source  string
}

func (d Document) Name() string {
	return d.Object.GetName()
}

// IsKubernetes reports whether the document parsed as a Kubernetes resource
// (a mapping carrying a Kind). Non-Kubernetes YAML (plain configs, top-level
// lists, scalars) has an empty Kind and skips the Kubernetes-specific checks.
func (d Document) IsKubernetes() bool {
	return d.GVK.Kind != ""
}

func DocumentKey(source string, index int) string {
	return fmt.Sprintf("%s#%d", source, index)
}

func (d Document) Key() string {
	return DocumentKey(d.Source, d.Index)
}

type ScanResult struct {
	Documents []Document
	Findings  []Finding
}
