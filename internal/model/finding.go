package model

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Source string

const (
	SourceSchema  Source = "schema"
	SourcePolicy  Source = "policy"
	SourceKubectl Source = "kubectl"
	SourceCluster Source = "cluster"
	SourceCompat  Source = "compat"
)

type GroupVersionKind = schema.GroupVersionKind

type Fix struct {
	RuleID      string
	Description string
	Apply       func(doc *unstructured.Unstructured) error
}

type Finding struct {
	RuleID       string   `json:"ruleId"`
	Message      string   `json:"message"`
	Path         string   `json:"path"`
	GVK          GroupVersionKind
	GVKString    string   `json:"gvk"`
	ResourceName string   `json:"resourceName"`
	Source       Source   `json:"source"`
	Severity     Severity `json:"severity"`
	DocIndex     int      `json:"docIndex"`
	SourceFile   string   `json:"sourceFile"`
	Fix          *Fix     `json:"-"`
}

func (f Finding) HasFix() bool {
	return f.Fix != nil
}
