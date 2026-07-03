package k8serrors

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackalgg/cairn/internal/compat"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/policy"
	"github.com/jackalgg/cairn/internal/repair/schemafix"
)

type match struct {
	pattern *regexp.Regexp
	ruleID  string
}

var patterns = []match{
	{regexp.MustCompile(`(?i)runAsNonRoot`), "pss-run-as-non-root"},
	{regexp.MustCompile(`(?i)readOnlyRootFilesystem`), "pss-read-only-rootfs"},
	{regexp.MustCompile(`(?i)unknown field "([^"]+)"`), "schema-unknown-field"},
	{regexp.MustCompile(`(?i)additional properties? ['"]([^'"]+)['"]`), "schema-unknown-field"},
	{regexp.MustCompile(`(?i)field not declared in schema`), "schema-unknown-field"},
	{regexp.MustCompile(`(?i)could not find the requested resource`), "api-version-deprecated"},
	{regexp.MustCompile(`(?i)no matches for kind`), "api-version-deprecated"},
	{regexp.MustCompile(`(?i)field is immutable`), "schema-immutable-field"},
	{regexp.MustCompile(`(?i)failed calling webhook`), "schema-validation"},
	{regexp.MustCompile(`(?i)missing required`), "schema-missing-required"},
	{regexp.MustCompile(`(?i)invalid value`), "schema-validation"},
}

var policyRuleByID = map[string]policy.Rule{
	"pss-run-as-non-root":  &policy.RunAsNonRootRule{},
	"pss-read-only-rootfs": &policy.ReadOnlyRootFSRule{},
}

func ParseErrorText(errorText string) []string {
	lines := strings.Split(errorText, "\n")
	var ruleIDs []string
	seen := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, p := range patterns {
			if p.pattern.MatchString(line) {
				if _, ok := seen[p.ruleID]; !ok {
					seen[p.ruleID] = struct{}{}
					ruleIDs = append(ruleIDs, p.ruleID)
				}
			}
		}
	}
	return ruleIDs
}

func FindingsFromError(doc model.Document, errorText string, targetVersion string) []model.Finding {
	ruleIDs := ParseErrorText(errorText)
	var findings []model.Finding
	for _, id := range ruleIDs {
		if rule, ok := policyRuleByID[id]; ok {
			matched := rule.Check(context.Background(), doc)
			for i := range matched {
				matched[i].Source = model.SourceKubectl
				matched[i].Category = model.CategoryPolicy
				matched[i].Message = "from kubectl error: " + matched[i].Message
			}
			findings = append(findings, matched...)
			continue
		}
		if id == "schema-unknown-field" {
			for _, path := range schemafix.UnknownFieldPaths(errorText) {
				f := model.Finding{
					RuleID:           id,
					Message:          "from kubectl error: unknown field " + path,
					Path:             path,
					GVK:              doc.GVK,
					GVKString:        doc.GVK.String(),
					ResourceName:     doc.Name(),
					Source:           model.SourceKubectl,
					Category:         model.CategoryStructure,
					Severity:         model.SeverityError,
					RepairConfidence: model.RepairCertain,
					DocIndex:         doc.Index,
					SourceFile:       doc.Source,
				}
				findings = append(findings, schemafix.EnrichFinding(doc, f))
			}
			continue
		}
		if id == "api-version-deprecated" {
			compatFindings := compat.CheckDocument(doc, targetVersion)
			for i := range compatFindings {
				compatFindings[i].Source = model.SourceKubectl
				compatFindings[i].Category = model.CategoryStructure
				compatFindings[i].Message = "from kubectl error: " + compatFindings[i].Message
			}
			findings = append(findings, compatFindings...)
			continue
		}
		f := model.Finding{
			RuleID:           id,
			Message:          "kubectl/admission error matched rule " + id,
			Path:             "",
			GVK:              doc.GVK,
			GVKString:        doc.GVK.String(),
			ResourceName:     doc.Name(),
			Source:           model.SourceKubectl,
			Category:         model.CategoryStructure,
			Severity:         model.SeverityError,
			RepairConfidence: model.RepairCertain,
			DocIndex:         doc.Index,
			SourceFile:       doc.Source,
		}
		if enriched := schemafix.EnrichFinding(doc, f); enriched.Fix != nil {
			findings = append(findings, enriched)
		} else {
			findings = append(findings, f)
		}
	}
	return findings
}

func RuleIDsForFix(errorText string) []string {
	return ParseErrorText(errorText)
}
