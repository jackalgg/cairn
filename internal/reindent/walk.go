package reindent

// Shared plumbing for the scope-walking pre-passes (colon.go, markers.go,
// typos.go). Each pass still owns its per-line logic — no pass may know
// another's internals — but the mechanical frame they all repeat lives here:
// splitting a file into `---` documents and resolving which open sequence owns
// a `- ` marker.

import "strings"

// mapDocs normalizes tabs, splits data into `---`-separated documents, applies
// fn to each, and rebuilds the file. fn receives the document's lines and the
// 0-based line offset of the document within the file (for diagnostics), and
// returns the transformed lines plus how many edits it made. When no fn call
// reports an edit, data is returned unchanged so the pass is a strict no-op.
func mapDocs(data []byte, fn func(lines []string, offset int) ([]string, int)) (out []byte, changed bool) {
	text := strings.ReplaceAll(string(data), "\t", "  ")
	trailingNewline := strings.HasSuffix(text, "\n")
	body := text
	if trailingNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")

	var result []string
	edits := 0
	docStart := 0
	flush := func(end int) {
		if end > docStart {
			d, n := fn(lines[docStart:end], docStart)
			result = append(result, d...)
			edits += n
		}
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			flush(i)
			result = append(result, "---")
			docStart = i + 1
		}
	}
	flush(len(lines))

	if edits == 0 {
		return data, false
	}
	rebuilt := strings.Join(result, "\n")
	if trailingNewline {
		rebuilt += "\n"
	}
	return []byte(rebuilt), true
}

// resolveMarkerScope pops the stack until the sequence that owns a `- ` marker
// at indent orig is on top (or marks the top mapping scope as a sequence).
// poppedItem: see reindent.go emitListItem — a sequence whose own items sat
// deeper than this marker does not own it.
func resolveMarkerScope(stack *[]mframe, orig int) {
	poppedItem := -1
	for len(*stack) > 1 {
		t := &(*stack)[len(*stack)-1]
		if t.isSeq {
			if poppedItem >= 0 && orig < poppedItem {
				*stack = (*stack)[:len(*stack)-1]
				continue
			}
			break
		}
		if orig < t.origIndent || (orig == t.origIndent && t.isItem) {
			if t.isItem {
				poppedItem = t.origIndent
			}
			*stack = (*stack)[:len(*stack)-1]
			continue
		}
		// A map-key scope at equal-or-deeper indent holds this sequence.
		t.isSeq = true
		break
	}
}
