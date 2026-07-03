package syntax

import (
	"strings"

	"github.com/jackalgg/cairn/internal/model"
)

// tabProposals reports a single proposal covering the contiguous span of lines
// that contain tab characters. Tabs are illegal as YAML indentation and abort
// parsing immediately, so replacing them with two spaces is a certain fix.
func tabProposals(source string, data []byte) []model.RepairProposal {
	lines := splitLines(data)
	first, last := -1, -1
	for i, line := range lines {
		if strings.ContainsRune(line, '\t') {
			if first == -1 {
				first = i
			}
			last = i
		}
	}
	if first == -1 {
		return nil
	}

	before := strings.Join(lines[first:last+1], "\n")
	fixed := make([]string, 0, last-first+1)
	for _, line := range lines[first : last+1] {
		fixed = append(fixed, expandTabs(line))
	}
	after := strings.Join(fixed, "\n")

	return []model.RepairProposal{{
		RuleID:      "syntax-tabs",
		Title:       "Replace tabs with spaces",
		Description: "tab characters are not valid YAML indentation; expanded to two spaces",
		SourceFile:  source,
		StartLine:   first + 1,
		EndLine:     last + 1,
		Before:      before,
		After:       after,
		Confidence:  model.RepairCertain,
	}}
}

// expandTabs replaces every tab in a line with two spaces. Leading tabs become
// indentation; tabs elsewhere are normalized the same way for consistency.
func expandTabs(line string) string {
	return strings.ReplaceAll(line, "\t", "  ")
}
