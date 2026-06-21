package k8serrors

import (
	"context"
	"regexp"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/policy"
)

type match struct {
	pattern *regexp.Regexp
	ruleID  string
}

var patterns = []match{
	{regexp.MustCompile(`(?i)runAsNonRoot`), "pss-run-as-non-root"},
	{regexp.MustCompile(`(?i)readOnlyRootFilesystem`), "pss-read-only-rootfs"},
	{regexp.MustCompile(`(?i)unknown field "([^"]+)"`), "schema-unknown-field"},
	{regexp.MustCompile(`(?i)could not find the requested resource`), "api-version-deprecated"},
}

var ruleByID = map[string]policy.Rule{
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

func FindingsFromError(doc model.Document, errorText string) []model.Finding {
	ruleIDs := ParseErrorText(errorText)
	var findings []model.Finding
	for _, id := range ruleIDs {
		rule, ok := ruleByID[id]
		if !ok {
			findings = append(findings, model.Finding{
				RuleID:       id,
				Message:      "kubectl/admission error matched rule " + id,
				Path:         "",
				GVK:          doc.GVK,
				GVKString:    doc.GVK.String(),
				ResourceName: doc.Name(),
				Source:       model.SourceKubectl,
				Severity:     model.SeverityError,
				DocIndex:     doc.Index,
				SourceFile:   doc.Source,
			})
			continue
		}
		matched := rule.Check(context.Background(), doc)
		for i := range matched {
			matched[i].Source = model.SourceKubectl
			matched[i].Message = "from kubectl error: " + matched[i].Message
		}
		findings = append(findings, matched...)
	}
	return findings
}

func RuleIDsForFix(errorText string) []string {
	return ParseErrorText(errorText)
}
