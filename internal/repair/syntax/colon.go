package syntax

import (
	"regexp"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
)

// colonKeyRe matches a block-mapping key whose colon is immediately followed by
// a value with no separating space, e.g. "name:web". It deliberately requires
// a simple (space-free) key so flow content and prose are left alone, and the
// value after the colon must not start with another space or a slash (to avoid
// rewriting things like "http://host").
var colonKeyRe = regexp.MustCompile(`^(\s*)(-\s+)?([A-Za-z0-9_.-]+):([^\s/].*)$`)

// colonProposals finds mapping keys written as "key:value" and proposes the
// canonical "key: value" form. Such lines usually parse as a plain scalar
// rather than a mapping, so this is a likely-but-not-certain (heuristic) fix.
func colonProposals(source string, data []byte) []model.RepairProposal {
	lines := splitLines(data)
	var out []model.RepairProposal
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := colonKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		value := m[4]
		// Skip values that are clearly intentional (timestamps like 10:30
		// are caught by the slash/space rules above only loosely, so also
		// skip when the "value" is itself digits forming a time).
		if looksLikeTime(m[3], value) {
			continue
		}
		fixed := m[1] + m[2] + m[3] + ": " + value
		out = append(out, model.RepairProposal{
			RuleID:      "syntax-colon-space",
			Title:       "Add space after colon",
			Description: "mapping key \"" + m[3] + "\" is missing a space after its colon",
			SourceFile:  source,
			StartLine:   i + 1,
			EndLine:     i + 1,
			Before:      line,
			After:       fixed,
			Confidence:  model.RepairHeuristic,
		})
	}
	return out
}

// looksLikeTime guards against rewriting clock-style values such as "12:30".
func looksLikeTime(key, value string) bool {
	if len(value) < 2 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	// Pure digits after a colon (e.g. "12:30") are most likely a time or a
	// sexagesimal value, not a missing space.
	last := key[len(key)-1]
	return last >= '0' && last <= '9'
}
