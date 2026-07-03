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
	SourceSyntax  Source = "syntax"
	SourceSchema  Source = "schema"
	SourcePolicy  Source = "policy"
	SourceKubectl Source = "kubectl"
	SourceCluster Source = "cluster"
	SourceCompat  Source = "compat"
)

type Category string

const (
	CategorySyntax    Category = "syntax"
	CategoryStructure Category = "structure"
	CategoryPolicy    Category = "policy"
)

type RepairConfidence string

const (
	RepairCertain   RepairConfidence = "certain"
	RepairHeuristic RepairConfidence = "heuristic"
)

type GroupVersionKind = schema.GroupVersionKind

type Fix struct {
	RuleID      string
	Description string
	Apply       func(doc *unstructured.Unstructured) error
	ApplyRaw    func(raw []byte) ([]byte, error)
}

type Finding struct {
	RuleID           string   `json:"ruleId"`
	Message          string   `json:"message"`
	Path             string   `json:"path"`
	GVK              GroupVersionKind
	GVKString        string           `json:"gvk"`
	ResourceName     string           `json:"resourceName"`
	Source           Source           `json:"source"`
	Category         Category         `json:"category"`
	Severity         Severity         `json:"severity"`
	RepairConfidence RepairConfidence `json:"repairConfidence"`
	DocIndex         int              `json:"docIndex"`
	SourceFile       string           `json:"sourceFile"`
	Line             int              `json:"line,omitempty"`
	EndLine          int              `json:"endLine,omitempty"`
	// Repairable marks findings that `cairn fix` can repair through the
	// line-oriented syntax engine rather than a Fix function.
	Repairable bool `json:"repairable,omitempty"`
	Fix        *Fix `json:"-"`
}

func (f Finding) HasFix() bool {
	return f.Fix != nil && (f.Fix.Apply != nil || f.Fix.ApplyRaw != nil)
}

// CanRepair reports whether `cairn fix` can address this finding, either via a
// Fix function or the line-oriented syntax repair engine.
func (f Finding) CanRepair() bool {
	return f.HasFix() || f.Repairable
}

func CategoryForSource(s Source) Category {
	switch s {
	case SourceSyntax:
		return CategorySyntax
	case SourceSchema, SourceCompat, SourceKubectl:
		return CategoryStructure
	case SourcePolicy:
		return CategoryPolicy
	default:
		return CategoryStructure
	}
}
