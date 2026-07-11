// Package reindent repairs the indentation of broken YAML — with special
// knowledge of Kubernetes manifests — by reconstructing the nesting of the
// document and re-emitting it with canonical two-space indentation.
//
// The central idea is that broken indentation cannot be fixed by trusting the
// broken indentation. So instead of searching for a per-line patch that happens
// to parse, the reindenter rebuilds the scope tree from signals that are not
// the (corrupt) indentation:
//
//   - structure: a bare `key:` opens a block, `- ` opens a list item, and
//   - schema: for known Kubernetes types, a field name identifies its parent,
//     so a field can be pulled back to the right ancestor regardless of how far
//     the broken file had indented it.
//
// It then re-emits every line at its reconstructed depth. Line *content* is
// never rewritten — only leading whitespace changes — so values, quoting and
// block scalars survive untouched.
package reindent

import "strings"

// Reindent returns data with its indentation reconstructed. It reports whether
// anything changed. Content other than leading whitespace is preserved exactly.
func Reindent(data []byte) (out []byte, changed bool) {
	text := strings.ReplaceAll(string(data), "\t", "  ")
	trailingNewline := strings.HasSuffix(text, "\n")
	body := text
	if trailingNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")

	// Split into YAML documents on `---` so each gets its own scope stack.
	var result []string
	doc := []string{}
	flush := func() {
		if len(doc) > 0 {
			result = append(result, reindentDoc(doc)...)
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

	rebuilt := strings.Join(result, "\n")
	if trailingNewline {
		rebuilt += "\n"
	}
	return []byte(rebuilt), rebuilt != text
}

// frame is one open scope on the stack.
type frame struct {
	typ        string // schema type whose fields are this frame's children ("" = unknown)
	isSeq      bool   // children are list items rather than map keys
	isItem     bool   // this frame was opened by a list marker (`- `)
	elem       string // element type for a sequence frame
	col        int    // output column of this frame's own key/marker
	childCol   int    // output column for this frame's children
	origIndent int    // original indent where this frame was opened
}

func (f frame) wildcard() bool { return f.typ == "" || f.typ == stringMap }

func reindentDoc(lines []string) []string {
	rootType := detectKind(lines)
	stack := []frame{{typ: rootType, childCol: 0, origIndent: -1}}
	out := make([]string, 0, len(lines))

	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		content := strings.TrimLeft(raw, " ")
		content = strings.TrimRight(content, " ")

		if content == "" {
			out = append(out, "")
			continue
		}
		orig := leadingSpaces(raw)

		if strings.HasPrefix(content, "#") {
			// Attach comments to the current child level.
			out = append(out, indent(top(stack).childCol)+content)
			continue
		}

		if content == "-" || strings.HasPrefix(content, "- ") {
			out = append(out, emitListItem(&stack, orig, content))
			continue
		}

		// A mapping key or entry.
		key, value, ok := splitKeyValue(content)
		if !ok {
			// Not a recognizable key line (bare scalar / flow continuation).
			// Place it at the current child level.
			out = append(out, indent(top(stack).childCol)+content)
			continue
		}

		col := placeKey(&stack, key, orig)
		out = append(out, indent(col)+content)

		if isBlockScalar(value) {
			out = append(out, consumeBlockScalar(lines, &i, orig, col)...)
			continue
		}
		if value == "" { // opens a nested block
			pushChild(&stack, top(stack).typ, key, col, orig)
		}
	}
	return out
}

// placeKey pops the stack until the frame that should parent key is on top,
// then returns the column at which key should be emitted.
func placeKey(stack *[]frame, key string, orig int) int {
	for len(*stack) > 1 {
		t := top(*stack)
		switch {
		case declares(t.typ, key):
			// This scope specifically owns the key: it stays here.
			return t.childCol
		case ancestorDeclares(*stack, key):
			// A specific ancestor owns the key: dedent toward it. This beats
			// wildcard absorption so a real field that was over-indented into a
			// free-form block (labels, annotations) is still pulled back out.
			pop(stack)
		case t.wildcard() && orig > t.origIndent:
			// Free-form scope (labels, unknown block) that no ancestor claims;
			// key is indented as a child, so keep it here — indentation is the
			// only signal.
			return t.childCol
		case orig <= t.origIndent:
			// Structural dedent: we're at or above this scope's opener.
			pop(stack)
		default:
			// Deeper than the current scope and nobody claims the key: treat
			// it as a child here.
			return t.childCol
		}
	}
	return top(*stack).childCol
}

// pushChild pushes a scope for a block-opening key, typed from the schema when
// the parent type is known.
func pushChild(stack *[]frame, parentType, key string, col, orig int) {
	f := frame{col: col, childCol: col + 2, origIndent: orig}
	if fd, ok := lookup(parentType, key); ok {
		if fd.seq {
			// A sequence: items align under the key (kubectl style), so their
			// marker column equals the key column.
			f.isSeq = true
			f.elem = fd.elem
			f.childCol = col
		} else {
			f.typ = fd.child
		}
	}
	*stack = append(*stack, f)
}

// emitListItem places a `- ...` line, opening or continuing a sequence, and
// pushes a frame for the item's own fields. It handles inline content after the
// marker (`- name: app`).
func emitListItem(stack *[]frame, orig int, content string) string {
	// Find the open sequence this item belongs to. A block sequence may be
	// indented the same as its key (`steps:` then `- run` at equal indent) or
	// deeper — both are valid YAML — so a map-key scope at equal-or-deeper
	// indent *holds* this sequence rather than being a sibling of it. Only a
	// prior list item's own scope at equal indent is a sibling to pop.
	for len(*stack) > 1 {
		t := top(*stack)
		if t.isSeq {
			break
		}
		if orig < t.origIndent || (orig == t.origIndent && t.isItem) {
			pop(stack)
			continue
		}
		// This map-key scope holds the sequence: adopt it, aligning item
		// markers under the opener key (kubectl style).
		(*stack)[len(*stack)-1].isSeq = true
		(*stack)[len(*stack)-1].childCol = t.col
		break
	}

	seq := top(*stack)
	markerCol := seq.childCol
	// Item fields sit two spaces past the marker.
	item := frame{typ: seq.elem, isItem: true, col: markerCol, childCol: markerCol + 2, origIndent: orig}
	*stack = append(*stack, item)

	inline := ""
	if content != "-" {
		inline = strings.TrimSpace(content[1:])
	}
	if inline == "" {
		return indent(markerCol) + "-"
	}

	// If the inline content is itself a key that opens a block, register it so
	// following lines nest correctly.
	if key, value, ok := splitKeyValue(inline); ok && value == "" && !isBlockScalar(value) {
		pushChild(stack, item.typ, key, markerCol+2, orig)
	}
	return indent(markerCol) + "- " + inline
}

// consumeBlockScalar copies the body of a block scalar (| or >) verbatim,
// shifting it by the same amount the header moved so its internal shape is
// preserved. It advances *i past the consumed body.
func consumeBlockScalar(lines []string, i *int, headerOrig, headerCol int) []string {
	shift := headerCol - headerOrig
	var body []string
	for *i+1 < len(lines) {
		next := lines[*i+1]
		if strings.TrimSpace(next) == "" {
			body = append(body, "")
			*i++
			continue
		}
		if leadingSpaces(next) <= headerOrig {
			break
		}
		col := leadingSpaces(next) + shift
		if col < 0 {
			col = 0
		}
		body = append(body, indent(col)+strings.TrimLeft(next, " "))
		*i++
	}
	// Trim trailing blank lines we may have swallowed so they re-flow naturally.
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
		*i--
	}
	return body
}

// detectKind scans the top-level keys for `kind:` and maps it to a schema type.
func detectKind(lines []string) string {
	for _, ln := range lines {
		content := strings.TrimSpace(ln)
		if key, value, ok := splitKeyValue(content); ok && key == "kind" {
			return kindType[strings.TrimSpace(value)]
		}
	}
	return ""
}

// ancestorDeclares reports whether any frame below the top specifically
// declares key.
func ancestorDeclares(stack []frame, key string) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		if declares(stack[i].typ, key) {
			return true
		}
	}
	return false
}

func top(stack []frame) frame { return stack[len(stack)-1] }
func pop(stack *[]frame) {
	if len(*stack) > 1 {
		*stack = (*stack)[:len(*stack)-1]
	}
}

func indent(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}

// splitKeyValue splits "key: value" into its key and value. It returns ok=false
// when the line is not a mapping key (no top-level colon followed by space or
// end of line). Colons inside quotes or flow collections are ignored.
func splitKeyValue(s string) (key, value string, ok bool) {
	inSingle, inDouble, depth := false, false, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case inDouble:
			if c == '"' {
				inDouble = false
			}
		case c == '\'':
			inSingle = true
		case c == '"':
			inDouble = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			if depth > 0 {
				depth--
			}
		case c == ':' && depth == 0:
			if i == len(s)-1 || s[i+1] == ' ' {
				key = strings.TrimSpace(s[:i])
				value = strings.TrimSpace(s[i+1:])
				if key == "" {
					return "", "", false
				}
				return key, value, true
			}
		}
	}
	return "", "", false
}

// isBlockScalar reports whether a mapping value introduces a block scalar
// (literal `|` or folded `>`, with optional chomping/indent indicators).
func isBlockScalar(value string) bool {
	if value == "" {
		return false
	}
	// Strip a trailing comment.
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}
	if value == "" {
		return false
	}
	if value[0] != '|' && value[0] != '>' {
		return false
	}
	for _, r := range value[1:] {
		if r != '+' && r != '-' && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
