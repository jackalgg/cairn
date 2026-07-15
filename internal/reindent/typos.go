package reindent

// This file implements field-typo repair: a mapping key that is not declared
// by its (schema-typed) scope but is one edit away from exactly one field that
// is — `contaieners:` under a PodSpec — is renamed to that field. The same
// treatment applies to the top-level `kind:` VALUE (`Deploymet` → `Deployment`),
// which identity.go deliberately leaves to this pass (it only fixes casing).
//
// The posture, agreed 2026-07-14, keeps auto-apply safe:
//
//   - AUTO-FIX only a UNIQUE edit-distance-1 match against the current scope's
//     schema fields, and only for keys of 3+ characters; every fix is reported
//     as a diagnostic.
//   - Anything fuzzier — multiple distance-1 candidates, or a distance-2
//     match — becomes a SUGGESTION diagnostic, never an edit.
//   - Free-form scopes (stringMap: labels, annotations, data, …) and unknown
//     scopes never fire; their keys really are arbitrary.
//   - An undeclared key with no candidate is reported as an unknown field, but
//     ONLY inside types whose schema table is COMPLETE (completeTypes); partial
//     tables must not accuse valid fields of being unknown.
//
// It runs after colon-spacing (so jammed keys have been made recognizable) and
// before list-markers (so a corrected key can unlock the schema-gated marker
// repairs — e.g. a dashless item whose first key was typo'd).

import (
	"fmt"
	"sort"
	"strings"
)

// completeTypes marks schema types whose field table is exhaustive, so an
// undeclared key there is genuinely unknown and worth a warning. Partial types
// (Container, PodSpec, …) must never be listed until their tables are complete.
var completeTypes = map[string]bool{
	"ObjectMeta": true,
}

// FixTypos repairs unique distance-1 field-name typos and kind-value typos,
// and emits suggestions/warnings for what it will not touch.
func FixTypos(data []byte) ([]byte, bool, []Diagnostic) {
	var diags []Diagnostic
	out, changed := mapDocs(data, func(lines []string, offset int) ([]string, int) {
		return fixTyposDoc(lines, offset, &diags)
	})
	return out, changed, diags
}

func fixTyposDoc(lines []string, offset int, diags *[]Diagnostic) ([]string, int) {
	fixes := fixKindValue(lines, offset, diags)

	stack := []mframe{{typ: detectKind(lines), origIndent: -1}}
	out := make([]string, 0, len(lines))

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

		// A list item: its inline key is checked against the sequence's element
		// type. Scalar sequences (elem "") never fire — their items aren't keys.
		if content == "-" || strings.HasPrefix(content, "- ") {
			resolveMarkerScope(&stack, orig)
			elem := top().elem
			inline := ""
			if content != "-" {
				inline = strings.TrimSpace(content[1:])
			}
			if k, v, ok := splitKeyValue(inline); ok {
				if fixedKey, fixed := repairKey(elem, k, offset+i+1, diags); fixed {
					raw = renameKey(raw, k, fixedKey)
					k = fixedKey
					fixes++
				}
				out = append(out, raw)
				stack = append(stack, mframe{typ: elem, isItem: true, origIndent: orig})
				if v == "" {
					pushMScope(&stack, elem, k, orig+2)
				} else if isBlockScalar(v) {
					skipBlockScalar(lines, &i, orig, &out)
				}
			} else {
				out = append(out, raw)
				stack = append(stack, mframe{typ: elem, isItem: true, origIndent: orig})
				if isBlockScalar(inline) {
					skipBlockScalar(lines, &i, orig, &out)
				}
			}
			continue
		}

		key, value, ok := splitKeyValue(content)
		if !ok {
			for len(stack) > 1 && top().origIndent >= orig {
				popm()
			}
			out = append(out, raw)
			continue
		}

		placeMKey(&stack, key, orig)

		// The owning type: the top mapping scope's, or — when the key sits
		// deeper under an open sequence — the element type (that is the
		// lost-marker item shape the list-markers pass repairs afterwards).
		t := top()
		owner := t.typ
		if t.isSeq && orig > t.origIndent {
			owner = t.elem
		}
		if fixedKey, fixed := repairKey(owner, key, offset+i+1, diags); fixed {
			raw = renameKey(raw, key, fixedKey)
			key = fixedKey
			fixes++
		}
		out = append(out, raw)

		if value == "" {
			pushMScope(&stack, owner, key, orig)
		} else if isBlockScalar(value) {
			skipBlockScalar(lines, &i, orig, &out)
		}
	}
	return out, fixes
}

// fixKindValue repairs a typo'd top-level `kind:` value in place (the lines
// slice is edited directly) when it is one edit from exactly one known kind.
// It runs before the walk so the corrected kind unlocks the schema for the
// whole document.
func fixKindValue(lines []string, offset int, diags *[]Diagnostic) int {
	for i, ln := range lines {
		if leadingSpaces(ln) != 0 {
			continue
		}
		key, value, ok := splitKeyValue(strings.TrimSpace(ln))
		if !ok || key != "kind" || value == "" {
			continue
		}
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		if _, known := kindType[value]; known {
			return 0
		}
		// Distance 1 only: many real kinds cairn doesn't model (Node, Role, …)
		// sit two edits from a modeled one, and suggesting those would be
		// noise on perfectly valid manifests.
		var d1 []string
		for k := range kindType {
			if editDistance(value, k) == 1 {
				d1 = append(d1, k)
			}
		}
		sort.Strings(d1)
		switch {
		case len(d1) == 1:
			lines[i] = renameValue(ln, value, d1[0])
			*diags = append(*diags, Diagnostic{Line: offset + i + 1, Msg: fmt.Sprintf(
				"kind %q → %q", value, d1[0])})
			return 1
		case len(d1) > 1:
			*diags = append(*diags, Diagnostic{Line: offset + i + 1, Msg: fmt.Sprintf(
				"unknown kind %q — did you mean %s? (ambiguous, not fixed)", value, quoteList(d1))})
		}
		return 0
	}
	return 0
}

// repairKey decides what to do with key inside a scope of type owner: rename
// (unique distance-1 match), suggest (ambiguous or distance-2), warn (unknown
// field in a complete type), or nothing. It reports the possibly-corrected key
// and whether it changed.
func repairKey(owner, key string, line int, diags *[]Diagnostic) (string, bool) {
	if owner == "" || owner == stringMap {
		return key, false
	}
	fields, ok := schemaTable[owner]
	if !ok {
		return key, false
	}
	if _, ok := fields[key]; ok {
		return key, false
	}
	var d1, d2 []string
	for f := range fields {
		switch editDistance(key, f) {
		case 1:
			d1 = append(d1, f)
		case 2:
			d2 = append(d2, f)
		}
	}
	sort.Strings(d1)
	sort.Strings(d2)
	switch {
	case len(d1) == 1 && len(key) >= 3:
		*diags = append(*diags, Diagnostic{Line: line, Msg: fmt.Sprintf(
			"field %q → %q (in %s)", key, d1[0], owner)})
		return d1[0], true
	case len(d1) > 0:
		*diags = append(*diags, Diagnostic{Line: line, Msg: fmt.Sprintf(
			"unknown field %q in %s — did you mean %s? (not fixed)", key, owner, quoteList(d1))})
	case len(d2) > 0 && len(key) >= 4:
		*diags = append(*diags, Diagnostic{Line: line, Msg: fmt.Sprintf(
			"unknown field %q in %s — did you mean %s? (not confident enough to fix)", key, owner, quoteList(d2))})
	case completeTypes[owner]:
		*diags = append(*diags, Diagnostic{Line: line, Msg: fmt.Sprintf(
			"unknown field %q in %s", key, owner)})
	}
	return key, false
}

// renameKey rewrites the key token of a "key: value" (or "- key: value") line,
// preserving the indent, marker, and everything from the colon on.
func renameKey(raw, oldKey, newKey string) string {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return raw
	}
	return strings.Replace(raw[:idx], oldKey, newKey, 1) + raw[idx:]
}

// renameValue rewrites the value of a "key: value" line, preserving everything
// through the colon (and a trailing comment, since oldValue is a single token).
func renameValue(raw, oldValue, newValue string) string {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return raw
	}
	return raw[:idx] + strings.Replace(raw[idx:], oldValue, newValue, 1)
}

// quoteList renders candidates as `"a" or "b"` for diagnostics.
func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, " or ")
}

// editDistance returns the optimal-string-alignment (Damerau-Levenshtein)
// distance between a and b, capped at 3: any distance greater than 2 reports
// 3, letting callers bail out early on obviously-unrelated names. Adjacent
// transposition counts as ONE edit because it is the most common typo shape
// (`naem` → `name`, `lables` → `labels`).
func editDistance(a, b string) int {
	la, lb := len(a), len(b)
	diff := la - lb
	if diff < 0 {
		diff = -diff
	}
	if diff > 2 {
		return 3
	}
	prev2 := make([]int, lb+1)
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] && prev2[j-2]+1 < cur[j] {
				cur[j] = prev2[j-2] + 1
			}
			if cur[j] < best {
				best = cur[j]
			}
		}
		if best > 2 {
			return 3
		}
		prev2, prev, cur = prev, cur, prev2
	}
	if prev[lb] > 2 {
		return 3
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
