package reindent

// This file implements list-marker repair: inserting a missing `- ` before a
// line that should be a sequence element. It is a separate pass that runs
// BEFORE Reindent, so the reindenter's invariant — that it only ever changes
// leading whitespace — is preserved. Marker insertion is the one transformation
// that legitimately edits line content, and it is deliberately quarantined here
// with a narrow, schema-gated contract:
//
//   - It only ever ADDS a `- ` marker; it never deletes, reorders, or rewrites.
//   - It only acts inside a sequence whose elements are a *known mapping type*
//     (containers/ports/env/volumeMounts/…). Scalar sequences (`args:`,
//     `command:`) and free-form sequences (`tolerations:`) are gated out for
//     free, because their element type is not a schema mapping, so `declares`
//     returns false and no branch fires.
//
// Two shapes are repaired:
//
//   Sub-case B — a single item lost its dash:
//       containers:            containers:
//         name: app     ->     - name: app
//         image: nginx           image: nginx
//
//   Sub-case C — a later item lost its dash (detected by a repeated key):
//       containers:            containers:
//       - name: a              - name: a
//         image: x              image: x
//         name: b       ->     - name: b
//         image: y              image: y
//
// It leaves indentation approximate; the subsequent Reindent pass re-places
// every line, including the freshly marked items, at its canonical column.

import "strings"

// InsertMarkers repairs missing list markers in schema-confirmed sequences of
// mappings. It reports whether any marker was inserted; when none was, it
// returns data unchanged so the pass is a strict no-op on files that don't need
// it (which keeps the fix pipeline idempotent).
func InsertMarkers(data []byte) (out []byte, changed bool) {
	text := strings.ReplaceAll(string(data), "\t", "  ")
	trailingNewline := strings.HasSuffix(text, "\n")
	body := text
	if trailingNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")

	var result []string
	inserted := 0
	doc := []string{}
	flush := func() {
		if len(doc) > 0 {
			d, n := insertMarkersDoc(doc)
			result = append(result, d...)
			inserted += n
			doc = nil
		}
	}
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			flush()
			result = append(result, "---")
			continue
		}
		doc = append(doc, ln)
	}
	flush()

	if inserted == 0 {
		return data, false
	}
	rebuilt := strings.Join(result, "\n")
	if trailingNewline {
		rebuilt += "\n"
	}
	return []byte(rebuilt), true
}

// mframe is one open scope while scanning for missing markers. It tracks less
// than reindent's frame — no output columns — because this pass only decides
// where to insert markers, not where to emit lines.
type mframe struct {
	typ        string          // schema type of this scope's children ("" = unknown)
	isSeq      bool            // children are list items
	elem       string          // element type for a sequence scope
	isItem     bool            // opened by a real or inserted marker
	origIndent int             // original indent where this scope was opened
	seen       map[string]bool // for item scopes: direct field keys seen so far
}

func insertMarkersDoc(lines []string) ([]string, int) {
	stack := []mframe{{typ: detectKind(lines), origIndent: -1}}
	out := make([]string, 0, len(lines))
	insertions := 0

	top := func() *mframe { return &stack[len(stack)-1] }
	popm := func() {
		if len(stack) > 1 {
			stack = stack[:len(stack)-1]
		}
	}

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		content := strings.TrimSpace(raw)
		if content == "" || strings.HasPrefix(content, "#") {
			out = append(out, raw)
			continue
		}
		orig := leadingSpaces(raw)

		// A list marker: dedent to the holding sequence, then open an item scope
		// so a later item that lost *its* dash (sub-case C) can be detected.
		if content == "-" || strings.HasPrefix(content, "- ") {
			for len(stack) > 1 {
				t := top()
				if t.isSeq {
					break
				}
				if orig < t.origIndent || (orig == t.origIndent && t.isItem) {
					popm()
					continue
				}
				// A map-key scope at equal-or-deeper indent holds this sequence.
				t.isSeq = true
				break
			}
			elem := top().elem
			item := mframe{typ: elem, isItem: true, origIndent: orig, seen: map[string]bool{}}
			inline := ""
			if content != "-" {
				inline = strings.TrimSpace(content[1:])
			}
			out = append(out, raw)
			k, v, ok := splitKeyValue(inline)
			if ok {
				item.seen[k] = true
				stack = append(stack, item)
				if v == "" {
					pushMScope(&stack, elem, k, orig+2)
				} else if isBlockScalar(v) {
					skipBlockScalar(lines, &i, orig, &out)
				}
			} else {
				stack = append(stack, item)
			}
			continue
		}

		key, value, ok := splitKeyValue(content)
		if !ok {
			out = append(out, raw)
			continue
		}

		// Structural dedent: leave scopes whose opener is at or below this indent.
		for len(stack) > 1 && top().origIndent >= orig {
			popm()
		}

		t := top()
		switch {
		case t.isSeq && declares(t.elem, key):
			// Sub-case B: the first mapping item under a sequence key has no dash.
			// Emit the marker at the sequence's own indent so the reindenter
			// reads it as a sibling item, not a nested sub-sequence; its fields
			// stay deeper and reindent nests them under the marker.
			out = append(out, insertMarker(raw, t.origIndent))
			insertions++
			stack = append(stack, mframe{typ: t.elem, isItem: true, origIndent: t.origIndent, seen: map[string]bool{key: true}})
			openBlock(&stack, t.elem, key, value, orig, lines, &i, &out)
		case t.isItem && t.seen[key]:
			// Sub-case C: a repeated field key marks a new item that lost its
			// dash. Emit the marker aligned with the current item's marker so
			// reindent treats it as the next sibling item.
			itemOrig := t.origIndent
			popm() // leave the current item; the sequence is now on top
			elem := top().elem
			out = append(out, insertMarker(raw, itemOrig))
			insertions++
			stack = append(stack, mframe{typ: elem, isItem: true, origIndent: itemOrig, seen: map[string]bool{key: true}})
			openBlock(&stack, elem, key, value, orig, lines, &i, &out)
		default:
			out = append(out, raw)
			if t.isItem {
				t.seen[key] = true
			}
			openBlock(&stack, t.typ, key, value, orig, lines, &i, &out)
		}
	}
	return out, insertions
}

// openBlock handles what a just-placed key opens: a nested mapping/sequence
// scope (empty value) or a block scalar whose body must be skipped verbatim so
// its contents are never mistaken for keys.
func openBlock(stack *[]mframe, parentType, key, value string, orig int, lines []string, i *int, out *[]string) {
	if value == "" {
		pushMScope(stack, parentType, key, orig)
		return
	}
	if isBlockScalar(value) {
		skipBlockScalar(lines, i, orig, out)
	}
}

// pushMScope pushes the scope a block-opening key introduces, typed from the
// schema when the parent type is known.
func pushMScope(stack *[]mframe, parentType, key string, keyOrig int) {
	f := mframe{origIndent: keyOrig}
	if fd, ok := lookup(parentType, key); ok {
		if fd.seq {
			f.isSeq = true
			f.elem = fd.elem
		} else {
			f.typ = fd.child
		}
	}
	*stack = append(*stack, f)
}

// insertMarker returns raw with a "- " marker inserted, normalizing the leading
// whitespace to the line's original indent (Reindent fixes the exact column).
func insertMarker(raw string, orig int) string {
	return indent(orig) + "- " + strings.TrimLeft(raw, " ")
}

// skipBlockScalar copies a block scalar's body verbatim, advancing *i past it,
// so its lines are never parsed as keys.
func skipBlockScalar(lines []string, i *int, headerOrig int, out *[]string) {
	for *i+1 < len(lines) {
		next := lines[*i+1]
		if strings.TrimSpace(next) == "" {
			*out = append(*out, next)
			*i++
			continue
		}
		if leadingSpaces(next) <= headerOrig {
			break
		}
		*out = append(*out, next)
		*i++
	}
}
