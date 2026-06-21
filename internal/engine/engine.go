package engine

import (
	"context"
	"fmt"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/compat"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
	"github.com/jackalgg/cairn/internal/policy"
	"github.com/jackalgg/cairn/internal/schema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Options struct {
	KubernetesVersion string
	TargetVersion     string
	SchemaValidation  bool
	PolicyChecks      bool
	CompatChecks      bool
	Probe             *cluster.Probe
	MinSeverity       model.Severity
	Rules             []policy.Rule
}

type Engine struct {
	opts   Options
	schema *schema.Validator
}

func New(opts Options) (*Engine, error) {
	if opts.Rules == nil {
		opts.Rules = policy.DefaultRules()
	}
	if opts.TargetVersion == "" {
		opts.TargetVersion = opts.KubernetesVersion
	}
	if opts.MinSeverity == "" {
		opts.MinSeverity = model.SeverityWarning
	}

	e := &Engine{opts: opts}
	if opts.SchemaValidation {
		v, err := schema.New(schema.FormatSchemaVersion(opts.KubernetesVersion), true)
		if err != nil {
			return nil, fmt.Errorf("schema validator: %w", err)
		}
		e.schema = v
	}
	return e, nil
}

func (e *Engine) ScanPath(ctx context.Context, path string) (*model.ScanResult, error) {
	docs, err := parser.ParsePath(path)
	if err != nil {
		return nil, err
	}
	return e.ScanDocuments(ctx, docs)
}

func (e *Engine) ScanDocuments(ctx context.Context, docs []model.Document) (*model.ScanResult, error) {
	var findings []model.Finding
	for _, doc := range docs {
		findings = append(findings, e.scanDocument(ctx, doc)...)
	}
	if e.opts.Probe != nil {
		findings = e.opts.Probe.EnrichFindings(ctx, findings)
	}
	findings = FilterBySeverity(findings, e.opts.MinSeverity)
	return &model.ScanResult{Documents: docs, Findings: findings}, nil
}

func (e *Engine) scanDocument(ctx context.Context, doc model.Document) []model.Finding {
	var findings []model.Finding
	if e.schema != nil {
		findings = append(findings, e.schema.ValidateDocument(ctx, doc)...)
	}
	if e.opts.PolicyChecks {
		u := &doc.Object
		for _, rule := range e.opts.Rules {
			if !rule.AppliesTo(u) {
				continue
			}
			findings = append(findings, rule.Check(ctx, doc)...)
		}
	}
	if e.opts.CompatChecks && e.opts.TargetVersion != "" {
		findings = append(findings, compat.CheckDocument(doc, e.opts.TargetVersion)...)
	}
	return findings
}

func FilterFixable(findings []model.Finding) []model.Finding {
	var out []model.Finding
	seen := map[string]struct{}{}
	for _, f := range findings {
		if !f.HasFix() {
			continue
		}
		key := fixKey(f)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

func fixKey(f model.Finding) string {
	return fmt.Sprintf("%s:%s:%d:%s", f.SourceFile, f.RuleID, f.DocIndex, f.Path)
}

func FilterByRuleID(findings []model.Finding, ruleIDs []string) []model.Finding {
	if len(ruleIDs) == 0 {
		return findings
	}
	allowed := map[string]struct{}{}
	for _, id := range ruleIDs {
		allowed[id] = struct{}{}
	}
	var out []model.Finding
	for _, f := range findings {
		if _, ok := allowed[f.RuleID]; ok {
			out = append(out, f)
		}
	}
	return out
}

func FilterBySeverity(findings []model.Finding, min model.Severity) []model.Finding {
	minRank := severityRank(min)
	var out []model.Finding
	for _, f := range findings {
		if severityRank(f.Severity) >= minRank {
			out = append(out, f)
		}
	}
	return out
}

func HasErrors(findings []model.Finding) bool {
	for _, f := range findings {
		if f.Severity == model.SeverityError {
			return true
		}
	}
	return false
}

func severityRank(s model.Severity) int {
	switch s {
	case model.SeverityError:
		return 3
	case model.SeverityWarning:
		return 2
	case model.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func ParseSeverity(s string) (model.Severity, error) {
	switch s {
	case string(model.SeverityError):
		return model.SeverityError, nil
	case string(model.SeverityWarning):
		return model.SeverityWarning, nil
	case string(model.SeverityInfo):
		return model.SeverityInfo, nil
	default:
		return "", fmt.Errorf("unknown severity %q (use error, warning, or info)", s)
	}
}

func ApplyFixToDocument(doc *model.Document, fix *model.Fix) error {
	if fix == nil || fix.Apply == nil {
		return fmt.Errorf("nil fix")
	}
	u := doc.Object.DeepCopy()
	if err := fix.Apply(u); err != nil {
		return err
	}
	doc.Object = *u
	return nil
}

func CloneDocument(doc model.Document) model.Document {
	return model.Document{
		Index:   doc.Index,
		GVK:     doc.GVK,
		Object:  *doc.Object.DeepCopy(),
		RawYAML: append([]byte(nil), doc.RawYAML...),
		Source:  doc.Source,
	}
}

func DocumentObject(doc model.Document) *unstructured.Unstructured {
	return &doc.Object
}
