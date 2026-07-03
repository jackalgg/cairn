package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/compat"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
	"github.com/jackalgg/cairn/internal/policy"
	"github.com/jackalgg/cairn/internal/repair/ai"
	"github.com/jackalgg/cairn/internal/repair/structure"
	"github.com/jackalgg/cairn/internal/repair/syntax"
	"github.com/jackalgg/cairn/internal/schema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type RepairOnly string

const (
	RepairAll       RepairOnly = "all"
	RepairStructure RepairOnly = "structure"
	RepairPolicy    RepairOnly = "policy"
)

type Options struct {
	KubernetesVersion string
	TargetVersion     string
	SchemaValidation  bool
	PolicyChecks      bool
	CompatChecks      bool
	SyntaxRepair      bool
	RepairOnly        RepairOnly
	Probe             *cluster.Probe
	MinSeverity       model.Severity
	Rules             []policy.Rule
	StructureRules    []structure.Rule
	AIProvider        ai.Provider
	AcceptRisk        bool
}

type Engine struct {
	opts   Options
	schema *schema.Validator
}

func New(opts Options) (*Engine, error) {
	if opts.Rules == nil {
		opts.Rules = policy.DefaultRules()
	}
	if opts.StructureRules == nil {
		opts.StructureRules = structure.DefaultRules()
	}
	if opts.TargetVersion == "" {
		opts.TargetVersion = opts.KubernetesVersion
	}
	if opts.MinSeverity == "" {
		opts.MinSeverity = model.SeverityWarning
	}
	if opts.RepairOnly == "" {
		opts.RepairOnly = RepairAll
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
	if path == "-" {
		return e.scanStdin(ctx)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return e.scanDirectory(ctx, path)
	}
	return e.scanPathWithSyntaxRepair(ctx, path)
}

func (e *Engine) scanDirectory(ctx context.Context, dir string) (*model.ScanResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var allDocs []model.Document
	var allFindings []model.Finding
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isYAMLFile(name) {
			continue
		}
		path := filepath.Join(dir, name)
		result, err := e.scanPathWithSyntaxRepair(ctx, path)
		if err != nil {
			return nil, err
		}
		allDocs = append(allDocs, result.Documents...)
		allFindings = append(allFindings, result.Findings...)
	}
	return &model.ScanResult{Documents: allDocs, Findings: allFindings}, nil
}

func isYAMLFile(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".yaml" || ext == ".yml"
}

// ScanBytes scans an in-memory YAML buffer, attributing findings to source.
// It is used after an interactive repair to evaluate the already-fixed content
// without re-reading from disk.
func (e *Engine) ScanBytes(ctx context.Context, source string, data []byte) (*model.ScanResult, error) {
	return e.scanRaw(ctx, source, data)
}

func (e *Engine) scanStdin(ctx context.Context) (*model.ScanResult, error) {
	source, data, err := parser.ReadPath("-")
	if err != nil {
		return nil, err
	}
	return e.scanRaw(ctx, source, data)
}

func (e *Engine) scanPathWithSyntaxRepair(ctx context.Context, path string) (*model.ScanResult, error) {
	source, data, err := parser.ReadPath(path)
	if err != nil {
		return nil, err
	}
	return e.scanRaw(ctx, source, data)
}

func (e *Engine) scanRaw(ctx context.Context, source string, data []byte) (*model.ScanResult, error) {
	current := append([]byte(nil), data...)

	// Surface every detectable syntax issue (tabs, indentation, missing list
	// markers, colon spacing) as findings regardless of whether the document
	// is a Kubernetes resource.
	var syntaxFindings []model.Finding
	for _, p := range syntax.Detect(source, current) {
		syntaxFindings = append(syntaxFindings, p.Finding())
	}

	// Apply certain-confidence repairs (currently tab expansion) in memory so
	// the Kubernetes layer can still analyze an otherwise unparseable file.
	auto := syntax.RepairAuto(source, current, model.RepairCertain)
	current = auto.Data

	docs, err := parser.Parse(source, current)
	if err != nil {
		// As a last resort, try AI-assisted repair when explicitly enabled.
		if e.opts.AIProvider != nil && e.opts.AcceptRisk {
			if patched, explanation, aiErr := e.opts.AIProvider.SuggestRepair(ctx, current, err, syntaxFindings); aiErr == nil && patched != nil {
				current = patched
				syntaxFindings = append(syntaxFindings, model.Finding{
					RuleID:           "syntax-ai-repair",
					Message:          explanation,
					Source:           model.SourceSyntax,
					Category:         model.CategorySyntax,
					Severity:         model.SeverityWarning,
					RepairConfidence: model.RepairHeuristic,
					SourceFile:       source,
				})
				docs, err = parser.Parse(source, current)
			}
		}
		if err != nil {
			// The document cannot be parsed for the Kubernetes layer, but the
			// syntax findings already describe what is wrong. Ensure at least
			// one finding represents the parse failure.
			if len(syntaxFindings) == 0 {
				syntaxFindings = append(syntaxFindings, parseErrorFinding(source, err))
			}
			return &model.ScanResult{
				Findings: FilterBySeverity(syntaxFindings, e.opts.MinSeverity),
			}, nil
		}
	}

	result, err := e.ScanDocuments(ctx, docs)
	if err != nil {
		return nil, err
	}
	if len(syntaxFindings) > 0 {
		result.Findings = append(syntaxFindings, result.Findings...)
		result.Findings = FilterBySeverity(result.Findings, e.opts.MinSeverity)
	}
	return result, nil
}

func parseErrorFinding(source string, err error) model.Finding {
	f := model.Finding{
		RuleID:           "syntax-parse-error",
		Message:          fmt.Sprintf("YAML parse error: %v", err),
		Source:           model.SourceSyntax,
		Category:         model.CategorySyntax,
		Severity:         model.SeverityError,
		RepairConfidence: model.RepairHeuristic,
		SourceFile:       source,
	}
	if line, ok := parser.ParseErrorLine(err); ok {
		f.Line = line
		f.EndLine = line
	}
	return f
}

func (e *Engine) ScanDocuments(ctx context.Context, docs []model.Document) (*model.ScanResult, error) {
	var findings []model.Finding
	for _, doc := range docs {
		findings = append(findings, e.scanDocument(ctx, doc)...)
	}
	if e.opts.Probe != nil {
		findings = e.opts.Probe.EnrichFindings(ctx, findings)
	}
	findings = FilterByRepairOnly(findings, e.opts.RepairOnly)
	findings = FilterBySeverity(findings, e.opts.MinSeverity)
	return &model.ScanResult{Documents: docs, Findings: findings}, nil
}

func (e *Engine) scanDocument(ctx context.Context, doc model.Document) []model.Finding {
	var findings []model.Finding
	// Schema, policy, and compatibility checks only make sense for Kubernetes
	// resources. Plain YAML (configs, lists, scalars) is validated for syntax
	// only and skips this layer.
	if !doc.IsKubernetes() {
		return findings
	}
	if e.schema != nil {
		if len(e.opts.StructureRules) > 0 {
			for _, rule := range e.opts.StructureRules {
				findings = append(findings, rule.Check(ctx, doc, e.schema)...)
			}
		} else {
			findings = append(findings, e.schema.ValidateDocument(ctx, doc)...)
		}
	}
	if e.opts.PolicyChecks {
		u := &doc.Object
		for _, rule := range e.opts.Rules {
			if !rule.AppliesTo(u) {
				continue
			}
			matched := rule.Check(ctx, doc)
			for i := range matched {
				matched[i].Category = model.CategoryPolicy
				if matched[i].RepairConfidence == "" {
					matched[i].RepairConfidence = model.RepairCertain
				}
			}
			findings = append(findings, matched...)
		}
	}
	if e.opts.CompatChecks && e.opts.TargetVersion != "" {
		compatFindings := compat.CheckDocument(doc, e.opts.TargetVersion)
		for i := range compatFindings {
			compatFindings[i].Category = model.CategoryStructure
			if compatFindings[i].RepairConfidence == "" {
				compatFindings[i].RepairConfidence = model.RepairCertain
			}
		}
		findings = append(findings, compatFindings...)
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

func FilterByRepairOnly(findings []model.Finding, mode RepairOnly) []model.Finding {
	switch mode {
	case RepairStructure:
		var out []model.Finding
		for _, f := range findings {
			if f.Category == model.CategorySyntax || f.Category == model.CategoryStructure {
				out = append(out, f)
			}
		}
		return out
	case RepairPolicy:
		var out []model.Finding
		for _, f := range findings {
			if f.Category == model.CategoryPolicy {
				out = append(out, f)
			}
		}
		return out
	default:
		return findings
	}
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

func MustSeverity(s string) model.Severity {
	sev, err := ParseSeverity(s)
	if err != nil {
		return model.SeverityWarning
	}
	return sev
}

func ParseRepairOnly(s string) (RepairOnly, error) {
	switch s {
	case "", "all":
		return RepairAll, nil
	case "structure":
		return RepairStructure, nil
	case "policy":
		return RepairPolicy, nil
	default:
		return "", fmt.Errorf("unknown repair-only mode %q (use all, structure, or policy)", s)
	}
}

func ApplyFixToDocument(doc *model.Document, fix *model.Fix) error {
	if fix == nil {
		return fmt.Errorf("nil fix")
	}
	if fix.ApplyRaw != nil {
		patched, err := fix.ApplyRaw(doc.RawYAML)
		if err != nil {
			return err
		}
		doc.RawYAML = patched
		return nil
	}
	if fix.Apply == nil {
		return fmt.Errorf("nil fix apply")
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
