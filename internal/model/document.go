package model

import (
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

type ScanResult struct {
	Documents []Document
	Findings  []Finding
}
