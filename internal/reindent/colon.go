package reindent

// This file implements colon-spacing repair: a mapping key written as
// "key:value" (which YAML actually parses as the plain scalar string
// "key:value", not a mapping) is rewritten to the canonical "key: value".
//
// It is a content-editing repair pass, run FIRST in the pipeline — before
// InsertMarkers and Reindent — because those passes recognize a key by its
// "key: " shape (splitKeyValue requires the space), so the colon must be fixed
// before they can see the line as a key at all.
//
// It is schema-aware, sharing the same scope walk as markers.go, for one
// reason: telling a broken mapping item ("- name:web", which SHOULD be fixed)
// from a legitimate scalar sequence item ("- kill:9" under `command:`, which
// must NOT be) is impossible line-by-line but trivial with scope — a list
// item's colon is only spaced when the sequence's element type declares that
// key. (If markers.go and this file gain a third sibling, factor the shared
// walk into one walkStructure helper with per-line callbacks.)
//
// The colon fix itself is deliberately narrow to stay safe under auto-apply:
//   - the key must be a single space-free token ([A-Za-z0-9_.-]+), so prose and
//     flow content are left alone;
//   - the value must not start with a space (already correct) or a "//" protocol
//     prefix (so a bare "http://host" is never touched — but a real path value
//     like "/var/www" still is);
//   - a bare numeric "key" with an all-digit value (a clock like "12:30") is
//     left alone;
//   - block-scalar bodies are skipped by the walk, so a script line such as
//     "echo foo:bar" inside a `|` block is never rewritten.

import (
	"regexp"
	"strings"
)

// colonKeyRe matches a key token whose colon is immediately followed by a value
// with no separating space. Groups: 1=leading indent, 2=key, 3=value.
var colonKeyRe = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.-]+):([^\s].*)$`)

// SpaceColons rewrites "key:value" mapping keys to "key: value". It reports
// whether anything changed and returns data untouched when nothing did, so the
// pass is a strict no-op on files that don't need it.
func SpaceColons(data []byte) (out []byte, changed bool) {
	text := strings.ReplaceAll(string(data), "\t", "  ")
	trailingNewline := strings.HasSuffix(text, "\n")
	body := text
	if trailingNewline {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")

	var result []string
	fixes := 0
	doc := []string{}
	flush := func() {
		if len(doc) > 0 {
			d, n := spaceColonsDoc(doc)
			result = append(result, d...)
			fixes += n
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

	if fixes == 0 {
		return data, false
	}
	rebuilt := strings.Join(result, "\n")
	if trailingNewline {
		rebuilt += "\n"
	}
	return []byte(rebuilt), true
}

func spaceColonsDoc(lines []string) ([]string, int) {
	stack := []mframe{{typ: detectKind(lines), origIndent: -1}}
	out := make([]string, 0, len(lines))
	fixes := 0

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

		// A list item: fix its inline colon only when the sequence's element
		// type declares that key (a mapping sequence). Scalar sequences leave
		// "- kill:9" untouched because declares("", ...) is false.
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
				t.isSeq = true
				break
			}
			elem := top().elem
			inline := ""
			if content != "-" {
				inline = strings.TrimSpace(content[1:])
			}
			if fixed, ok := fixColon(inline); ok && declares(elem, colonKey(inline)) {
				inline = fixed
				raw = indent(orig) + "- " + inline
				fixes++
			}
			out = append(out, raw)

			item := mframe{typ: elem, isItem: true, origIndent: orig, seen: map[string]bool{}}
			if k, v, ok := splitKeyValue(inline); ok {
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

		// A plain line under any mapping/sequence scope is a mapping key, so a
		// jammed colon is always safe to space here.
		for len(stack) > 1 && top().origIndent >= orig {
			popm()
		}
		if fixed, ok := fixColon(content); ok {
			content = fixed
			raw = indent(orig) + content
			fixes++
		}
		out = append(out, raw)

		if key, value, ok := splitKeyValue(content); ok {
			if value == "" {
				pushMScope(&stack, top().typ, key, orig)
			} else if isBlockScalar(value) {
				skipBlockScalar(lines, &i, orig, &out)
			}
		}
	}
	return out, fixes
}

// fixColon returns content with a space inserted after a jammed key colon, and
// whether it applied. content must have no leading whitespace.
func fixColon(content string) (string, bool) {
	m := colonKeyRe.FindStringSubmatch(content)
	if m == nil {
		return content, false
	}
	key, value := m[2], m[3]
	if strings.HasPrefix(value, "//") || looksLikeTime(key, value) {
		return content, false
	}
	return key + ": " + value, true
}

// colonKey returns the key token of a jammed "key:value" content, or "" if it
// isn't one. Used to ask the schema whether a list item's key is a real field.
func colonKey(content string) string {
	m := colonKeyRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[2]
}

// looksLikeTime guards against rewriting clock-style values such as "12:30",
// where a bare numeric key is followed by an all-digit value.
func looksLikeTime(key, value string) bool {
	if len(value) < 1 || len(key) < 1 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	last := key[len(key)-1]
	return last >= '0' && last <= '9'
}
