// Package syntax detects and repairs the common, parse-breaking mistakes that
// keep YAML from loading at all: tab indentation, inconsistent or odd
// indentation, missing list markers, and keys missing the space after a colon.
//
// Detection is line oriented and operates on raw bytes so comments, key
// ordering, and formatting survive a repair. Each issue is surfaced as a
// model.RepairProposal that the caller can apply, skip, or review interactively.
package syntax

import (
	"fmt"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
)

// maxAutoRounds bounds the iterative auto-repair loop so a proposal that fails
// to make progress can never spin forever.
const maxAutoRounds = 50

// Detect returns the repair proposals currently derivable from data, in the
// order they should be addressed. Tabs come first because they block parsing
// outright; a single structural (indentation / list marker) proposal follows
// when the document still fails to parse; finally heuristic cosmetic fixes
// (colon spacing) are appended.
//
// Callers that apply a proposal should re-run Detect on the updated buffer:
// later proposals are computed against the post-fix text, which keeps line
// numbers correct and lets dependent fixes (for example indentation that only
// makes sense once tabs are gone) emerge in turn.
func Detect(source string, data []byte) []model.RepairProposal {
	var proposals []model.RepairProposal

	if tabs := tabProposals(source, data); len(tabs) > 0 {
		return tabs
	}

	if err := parser.ValidateYAML(data); err != nil {
		if p, ok := structuralProposal(source, data, err); ok {
			proposals = append(proposals, p)
		} else if errLine, ok2 := parser.ParseErrorLine(err); ok2 {
			// Single-line repair made no progress; try reflowing the
			// broken block as a whole.
			if p2, ok3 := blockReflowProposal(source, data, errLine); ok3 {
				proposals = append(proposals, p2)
			}
		}
	}

	proposals = append(proposals, colonProposals(source, data)...)
	return proposals
}

// Apply returns a copy of data with the proposal's line range replaced by its
// After text. An out-of-range proposal is returned unchanged.
func Apply(data []byte, p model.RepairProposal) []byte {
	lines := splitLines(data)
	if p.StartLine < 1 || p.EndLine > len(lines) || p.StartLine > p.EndLine {
		return data
	}
	out := make([]string, 0, len(lines))
	out = append(out, lines[:p.StartLine-1]...)
	out = append(out, strings.Split(p.After, "\n")...)
	out = append(out, lines[p.EndLine:]...)
	return joinLines(out, trailingNewline(data))
}

// Signature uniquely identifies a proposal so a caller can remember that the
// user skipped it and avoid re-presenting the same edit on the next round.
func Signature(p model.RepairProposal) string {
	return fmt.Sprintf("%s@%d:%d:%s", p.RuleID, p.StartLine, p.EndLine, p.Before)
}

// AutoResult is returned by RepairAuto.
type AutoResult struct {
	Data     []byte
	Applied  []model.RepairProposal
	Findings []model.Finding
	Changed  bool
}

// RepairAuto applies repairs without prompting, accepting every proposal whose
// confidence is at least minConfidence. It is used for non-interactive contexts
// such as piped stdin or the --yes flag. The loop re-detects after each applied
// proposal so dependent fixes are handled in sequence.
func RepairAuto(source string, data []byte, minConfidence model.RepairConfidence) AutoResult {
	current := append([]byte(nil), data...)
	res := AutoResult{Data: current}

	for round := 0; round < maxAutoRounds; round++ {
		proposals := Detect(source, current)
		var next *model.RepairProposal
		for i := range proposals {
			if confidenceAtLeast(proposals[i].Confidence, minConfidence) {
				next = &proposals[i]
				break
			}
		}
		if next == nil {
			break
		}
		current = Apply(current, *next)
		res.Applied = append(res.Applied, *next)
		res.Findings = append(res.Findings, next.Finding())
		res.Changed = true
	}

	res.Data = current
	return res
}

func confidenceAtLeast(c, min model.RepairConfidence) bool {
	if min == model.RepairCertain {
		return c == model.RepairCertain
	}
	// Heuristic threshold accepts both certain and heuristic fixes.
	return c == model.RepairCertain || c == model.RepairHeuristic
}

func splitLines(data []byte) []string {
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string, trailing bool) []byte {
	s := strings.Join(lines, "\n")
	if trailing {
		s += "\n"
	}
	return []byte(s)
}

func trailingNewline(data []byte) bool {
	return len(data) > 0 && data[len(data)-1] == '\n'
}

func leadingSpaces(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
			continue
		}
		break
	}
	return n
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}
