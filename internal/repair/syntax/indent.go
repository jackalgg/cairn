package syntax

import (
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/parser"
)

// blockReflowProposal is the fallback when structuralProposal makes no
// progress. It searches over block starts and base indents, normalises each
// candidate block to 2-space depth steps (capping the error line's depth so
// it escapes its predecessor's scalar context), folds in odd-space fixes on
// the lines that follow, and scores every result that produces valid YAML.
//
// Scoring: prefer the candidate where the error line ends up deepest (most
// integrated into the document), provided it lands at the same canonical
// depth as the block's first element (they become siblings). Break ties by
// preferring larger blocks (more lines corrected at once).
func blockReflowProposal(source string, data []byte, errLine int) (model.RepairProposal, bool) {
	lines := splitLines(data)
	if errLine < 1 || errLine > len(lines) {
		return model.RepairProposal{}, false
	}

	// Indent of the first non-empty line after errLine — tells us how deep
	// the error line's children sit, and therefore how deep the error line
	// itself should be.
	nextIndent := -1
	for i := errLine; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		nextIndent = leadingSpaces(lines[i])
		break
	}

	lo := errLine - 10
	if lo < 1 {
		lo = 1
	}

	// Ideal indent for the error line: one level above the next line so the
	// next line becomes its direct child. Candidates closest to this win.
	idealErrInd := nextIndent - 2
	if idealErrInd < 0 {
		idealErrInd = 0
	}

	type scored struct {
		proposal       model.RepairProposal
		score          int  // lower = closer to ideal = better
		firstLineUnder bool // first line was under-indented vs base; preferred
		blockSize      int  // smaller = minimal surgery = better (tiebreaker)
	}
	var best *scored

	seen := map[string]bool{} // deduplicate by before+after text

	for blockStart := lo; blockStart <= errLine; blockStart++ {
		anchorIndent := 0
		if blockStart > 1 {
			anchorIndent = leadingSpaces(lines[blockStart-2])
		}

		bases := dedupInts([]int{anchorIndent + 2, anchorIndent, 0, 2, 4})
		for _, base := range bases {
			if base < 0 {
				continue
			}

			blockSlice := lines[blockStart-1 : errLine]
			blockLen := len(blockSlice)
			depthCap := rebaseDepthCap(nextIndent, base)
			normed := reflowBlock(blockSlice, base, blockLen-1, depthCap)

			// The error line must land at the same canonical indent as
			// the block's first non-empty line (they become siblings).
			// This rejects solutions that merely bury the error line one
			// level deeper inside the wrong parent.
			firstInd := -1
			for _, n := range normed {
				if strings.TrimSpace(n) != "" {
					firstInd = leadingSpaces(n)
					break
				}
			}
			errInd := leadingSpaces(normed[blockLen-1])
			if firstInd >= 0 && errInd != firstInd {
				continue
			}

			// Gather odd-space lines in the window after the error line.
			spanEnd := errLine
			extras := oddIndentFixes(lines, errLine, errLine+8)
			for ln := range extras {
				if ln > spanEnd {
					spanEnd = ln
				}
			}

			// Build the full candidate file.
			cand := make([]string, len(lines))
			copy(cand, lines)
			for i, n := range normed {
				cand[blockStart-1+i] = n
			}
			for ln, fix := range extras {
				cand[ln-1] = fix
			}

			before := strings.Join(lines[blockStart-1:spanEnd], "\n")
			after := strings.Join(cand[blockStart-1:spanEnd], "\n")
			if before == after || seen[before+"\x00"+after] {
				continue
			}

			result := joinLines(cand, trailingNewline(data))
			if parser.ValidateYAML(result) != nil {
				continue
			}
			seen[before+"\x00"+after] = true

			blockSize := errLine - blockStart + 1
			score := errInd - idealErrInd
			if score < 0 {
				score = -score
			}
			firstLineUnder := leadingSpaces(blockSlice[0]) < base

			// Prefer: lower score → under-indented first line → smaller block.
			better := false
			if best == nil {
				better = true
			} else if score < best.score {
				better = true
			} else if score == best.score {
				if firstLineUnder && !best.firstLineUnder {
					better = true
				} else if firstLineUnder == best.firstLineUnder && blockSize < best.blockSize {
					better = true
				}
			}
			if better {
				best = &scored{
					proposal: model.RepairProposal{
						RuleID:      "syntax-indent",
						Title:       "Fix indentation",
						Description: "normalised block indentation to 2-space steps",
						SourceFile:  source,
						StartLine:   blockStart,
						EndLine:     spanEnd,
						Before:      before,
						After:       after,
						Confidence:  model.RepairHeuristic,
					},
					score:          score,
					firstLineUnder: firstLineUnder,
					blockSize:      blockSize,
				}
			}
		}
	}

	if best != nil {
		return best.proposal, true
	}
	return model.RepairProposal{}, false
}

// rebaseDepthCap computes the maximum depth the error line may occupy so that
// the line following the block can be its direct child. A negative cap means
// no restriction.
func rebaseDepthCap(nextIndent, base int) int {
	if nextIndent < 0 {
		return -1
	}
	c := (nextIndent-base)/2 - 1
	if c < 0 {
		c = 0
	}
	return c
}

// oddIndentFixes returns line-number → replacement for lines in [start, end)
// whose leading-space count is not a multiple of 2 (e.g. 5 → 4 spaces).
func oddIndentFixes(lines []string, start, end int) map[int]string {
	if end > len(lines) {
		end = len(lines)
	}
	out := map[int]string{}
	for i := start; i < end; i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		ind := leadingSpaces(line)
		if ind > 0 && ind%2 != 0 {
			content := strings.TrimLeft(line, " ")
			out[i+1] = strings.Repeat(" ", (ind/2)*2) + content
		}
	}
	return out
}

// dedupInts returns the elements of s with duplicates removed, preserving order.
func dedupInts(s []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// reflowBlock re-indents a slice of lines using a depth stack. Each increase
// in raw indentation advances one depth level; each decrease pops back. Lines
// are emitted at base + depth*2 spaces. For the line at errOffset (the last
// line in the block), depthCap clamps the depth so deeply-nested errors are
// pulled back to the level implied by what follows the block.
func reflowBlock(lines []string, base, errOffset, depthCap int) []string {
	type entry struct{ rawIndent int }
	var stack []entry
	out := make([]string, len(lines))

	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
			out[i] = line
			continue
		}
		raw := leadingSpaces(line)

		// Pop stack for lines at same or shallower raw indent.
		for len(stack) > 0 && raw <= stack[len(stack)-1].rawIndent {
			stack = stack[:len(stack)-1]
		}

		depth := len(stack)

		// Clamp the error line's depth so it escapes being buried inside
		// the previous line's scalar value.
		if i == errOffset && depthCap >= 0 && depth > depthCap {
			depth = depthCap
			if len(stack) > depth {
				stack = stack[:depth]
			}
		}

		stack = append(stack, entry{raw})

		newInd := base + depth*2
		if newInd < 0 {
			newInd = 0
		}
		out[i] = strings.Repeat(" ", newInd) + trimmed
	}
	return out
}

// structuralProposal attempts to repair the line that yaml reports as the parse
// failure. It generates a handful of candidate rewrites (re-indentations and a
// missing list-marker variant) and returns the first one that either makes the
// whole document parse or at least pushes the parse error further down the
// file. These are heuristic guesses, so the caller is expected to confirm them.
func structuralProposal(source string, data []byte, parseErr error) (model.RepairProposal, bool) {
	errLine, ok := parser.ParseErrorLine(parseErr)
	if !ok {
		return model.RepairProposal{}, false
	}
	lines := splitLines(data)
	if errLine < 1 || errLine > len(lines) {
		return model.RepairProposal{}, false
	}

	// yaml often blames the line where a block's indentation was established
	// rather than the line that actually violates it, so try the reported line
	// and the next non-empty line as repair targets.
	var fallback *model.RepairProposal
	for _, target := range targetLines(lines, errLine) {
		orig := lines[target-1]
		for _, cand := range candidateRewrites(lines, target) {
			if cand.line == orig {
				continue
			}
			trial := withLine(lines, target, cand.line)
			trialErr := parser.ValidateYAML(trial)
			if trialErr == nil {
				return makeStructuralProposal(source, target, orig, cand), true
			}
			if fallback == nil {
				if newLine, ok := parser.ParseErrorLine(trialErr); ok && newLine > errLine {
					p := makeStructuralProposal(source, target, orig, cand)
					fallback = &p
				}
			}
		}
	}

	if fallback != nil {
		return *fallback, true
	}
	return model.RepairProposal{}, false
}

// targetLines returns the candidate line numbers to repair for a parse error
// reported at errLine. yaml usually blames the line where a block's indentation
// was first established, while the line that actually violates it is the next
// one, so the following non-empty line is tried first, then the reported line.
func targetLines(lines []string, errLine int) []int {
	var out []int
	for i := errLine; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, i+1)
		break
	}
	if errLine >= 1 && errLine <= len(lines) && strings.TrimSpace(lines[errLine-1]) != "" {
		out = append(out, errLine)
	}
	return out
}

type rewrite struct {
	line   string
	reason string
}

func makeStructuralProposal(source string, errLine int, orig string, cand rewrite) model.RepairProposal {
	return model.RepairProposal{
		RuleID:      cand.ruleID(),
		Title:       cand.title(),
		Description: cand.reason,
		SourceFile:  source,
		StartLine:   errLine,
		EndLine:     errLine,
		Before:      orig,
		After:       cand.line,
		Confidence:  model.RepairHeuristic,
	}
}

func (r rewrite) ruleID() string {
	if strings.Contains(r.reason, "list marker") {
		return "syntax-list-marker"
	}
	return "syntax-indent"
}

func (r rewrite) title() string {
	if strings.Contains(r.reason, "list marker") {
		return "Add missing list marker"
	}
	return "Fix indentation"
}

// candidateRewrites builds the ordered set of rewrites to try for the failing
// line: several re-indentations relative to the previous line, then variants
// that add a "- " list marker at plausible indentation levels.
func candidateRewrites(lines []string, errLine int) []rewrite {
	orig := lines[errLine-1]
	content := strings.TrimLeft(orig, " ")
	curIndent := leadingSpaces(orig)
	prevIndent := previousIndent(lines, errLine)
	prevIsSeqItem, seqIndent := previousSequenceItem(lines, errLine)

	var out []rewrite
	seen := map[string]bool{}
	add := func(line, reason string) {
		if line == orig || seen[line] {
			return
		}
		seen[line] = true
		out = append(out, rewrite{line: line, reason: reason})
	}

	markerReason := "added missing list marker (-) for a sequence item"

	// When the previous sibling is a sequence item, a line that lost its "- "
	// is the most likely culprit, so try the marker fix first.
	if prevIsSeqItem && !strings.HasPrefix(content, "- ") {
		add(spaces(seqIndent)+"- "+content, markerReason)
	}

	for _, ind := range indentCandidates(curIndent, prevIndent) {
		add(spaces(ind)+content, "adjusted indentation to align with surrounding keys")
	}

	if !strings.HasPrefix(content, "- ") {
		for _, ind := range []int{curIndent, prevIndent, prevIndent + 2} {
			if ind < 0 {
				continue
			}
			add(spaces(ind)+"- "+content, markerReason)
		}
	}

	return out
}

// previousSequenceItem reports whether the closest non-empty, non-comment line
// above errLine is a block sequence item ("- ..."), and that item's indentation.
func previousSequenceItem(lines []string, errLine int) (bool, int) {
	for i := errLine - 2; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") || trimmed == "-" {
			return true, leadingSpaces(line)
		}
		return false, 0
	}
	return false, 0
}

func indentCandidates(curIndent, prevIndent int) []int {
	raw := []int{
		prevIndent + 2,
		prevIndent,
		prevIndent - 2,
		(curIndent / 2) * 2,
		((curIndent + 1) / 2) * 2,
		curIndent - 1,
		curIndent + 1,
	}
	var out []int
	seen := map[int]bool{}
	for _, n := range raw {
		if n < 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// previousIndent returns the indentation of the closest non-empty,
// non-comment line above errLine, or 0 if there is none.
func previousIndent(lines []string, errLine int) int {
	for i := errLine - 2; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			continue
		}
		return leadingSpaces(lines[i])
	}
	return 0
}

func withLine(lines []string, lineNo int, replacement string) []byte {
	out := make([]string, len(lines))
	copy(out, lines)
	out[lineNo-1] = replacement
	return []byte(strings.Join(out, "\n"))
}
