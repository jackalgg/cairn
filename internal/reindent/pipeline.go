package reindent

// This file is the repair pipeline: the ordered list of repair passes and the
// per-pass verification that keeps the tool trustworthy as passes accumulate.
//
// Every pass has ONE narrow job and a declared "edit budget" — the only kind of
// change it is allowed to make. After a pass runs, its verifier independently
// re-derives whether the output stayed inside that budget; if not, the pass's
// output is DISCARDED (the pipeline continues from the previous bytes) and a
// diagnostic reports the refusal. A bug in any single pass therefore cannot
// corrupt a file: the worst it can do is fix nothing.
//
// Adding a feature to cairn = adding one file with an apply function, one
// verifier, and one line in the pipeline table below. Passes never call each
// other and never share state; they compose only through their bytes.

import (
	"fmt"
	"regexp"
	"strings"
)

// Diagnostic is a human-readable notice produced while fixing: a repair that
// was applied, a suggestion the tool was not confident enough to apply, or an
// internal safety refusal. Line is 1-based in the pass's input; 0 means the
// notice applies to the whole file.
type Diagnostic struct {
	Line int
	Msg  string
}

// pass is one repair step. apply performs the pass's single transformation and
// reports whether it changed anything; verify checks the transformation stayed
// inside the pass's edit budget. A non-nil verify error discards the output.
type pass struct {
	name   string
	apply  func([]byte) ([]byte, bool, []Diagnostic)
	verify func(before, after []byte) error
}

// pipeline is the ordered repair chain. Identity runs first (it needs no other
// pass and later passes want the corrected kind for schema lookups), then
// colon-spacing (so "key:value" lines become recognizable keys), then marker
// insertion, then reindent — the only pass that touches leading whitespace.
var pipeline = []pass{
	{"identity", FixIdentity, verifyIdentity},
	{"colon-spacing", wrap(SpaceColons), verifyColons},
	{"list-markers", wrap(InsertMarkers), verifyMarkers},
	{"reindent", wrap(Reindent), verifyReindent},
}

func wrap(f func([]byte) ([]byte, bool)) func([]byte) ([]byte, bool, []Diagnostic) {
	return func(data []byte) ([]byte, bool, []Diagnostic) {
		out, changed := f(data)
		return out, changed, nil
	}
}

// Fix runs the full repair pipeline over data and returns the repaired bytes,
// whether anything changed, and the diagnostics gathered along the way.
func Fix(data []byte) (out []byte, changed bool, diags []Diagnostic) {
	out = data
	for _, p := range pipeline {
		next, ch, ds := p.apply(out)
		if !ch {
			diags = append(diags, ds...) // pure suggestions, nothing to verify
			continue
		}
		if err := p.verify(out, next); err != nil {
			diags = append(diags, Diagnostic{Msg: fmt.Sprintf(
				"internal: %s pass attempted an out-of-contract edit (%v); its changes were discarded", p.name, err)})
			continue
		}
		out, changed = next, true
		diags = append(diags, ds...)
	}
	return out, changed, diags
}

// --- verifiers ---------------------------------------------------------------
//
// Each verifier re-checks its pass's edit budget from the outside, comparing
// only the pass's own input and output. Tabs are normalized on both sides
// where the pass itself normalizes them.

// normLines splits into lines with tabs converted, mirroring what the
// whitespace-editing passes do internally.
func normLines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(b), "\t", "  "), "\n"), "\n")
}

// rawLines splits into lines verbatim, for passes that never touch tabs.
func rawLines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// stripWS removes every space and tab from s, leaving only content characters.
func stripWS(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
}

// verifyReindent: budget is leading/trailing whitespace only — every line's
// trimmed content must be byte-identical, in order.
func verifyReindent(before, after []byte) error {
	b, a := normLines(before), normLines(after)
	if len(b) != len(a) {
		return fmt.Errorf("line count changed %d -> %d", len(b), len(a))
	}
	for i := range b {
		if strings.TrimSpace(b[i]) != strings.TrimSpace(a[i]) {
			return fmt.Errorf("line %d content changed", i+1)
		}
	}
	return nil
}

// verifyColons: budget is whitespace within a line (the gap after a key colon,
// or after a list dash) — no content character may be added, removed, or
// reordered on any line.
func verifyColons(before, after []byte) error {
	b, a := normLines(before), normLines(after)
	if len(b) != len(a) {
		return fmt.Errorf("line count changed %d -> %d", len(b), len(a))
	}
	for i := range b {
		if stripWS(b[i]) != stripWS(a[i]) {
			return fmt.Errorf("line %d non-whitespace content changed", i+1)
		}
	}
	return nil
}

// verifyMarkers: budget is inserting a "- " marker (plus indentation shift on
// that same line) — every line must keep its content exactly, or gain exactly
// one leading "- ".
func verifyMarkers(before, after []byte) error {
	b, a := normLines(before), normLines(after)
	if len(b) != len(a) {
		return fmt.Errorf("line count changed %d -> %d", len(b), len(a))
	}
	for i := range b {
		tb, ta := strings.TrimSpace(b[i]), strings.TrimSpace(a[i])
		if ta == tb {
			continue
		}
		if strings.HasPrefix(ta, "- ") && strings.TrimSpace(ta[2:]) == tb {
			continue
		}
		return fmt.Errorf("line %d changed beyond a '- ' insertion", i+1)
	}
	return nil
}

var (
	identKeyRe    = regexp.MustCompile(`(?i)^(apiversion|kind)\s*:`)
	apiKeyRe      = regexp.MustCompile(`(?i)^apiversion\s*:`)
	insertedAPIRe = regexp.MustCompile(`^apiVersion: \S+$`)
)

// verifyIdentity: budget is rewriting top-level apiVersion/kind lines and
// inserting a missing "apiVersion: <x>" line. Everything else must be
// byte-identical, in order.
func verifyIdentity(before, after []byte) error {
	b, a := rawLines(before), rawLines(after)
	i, j := 0, 0
	for i < len(b) || j < len(a) {
		switch {
		case i < len(b) && j < len(a) && b[i] == a[j]:
			i++
			j++
		case j < len(a) && insertedAPIRe.MatchString(a[j]) && (i >= len(b) || !apiKeyRe.MatchString(b[i])):
			j++ // an inserted apiVersion line
		case i < len(b) && j < len(a) && identKeyRe.MatchString(b[i]) && identKeyRe.MatchString(a[j]):
			i++ // an in-place apiVersion/kind rewrite
			j++
		default:
			return fmt.Errorf("line %d: edit outside apiVersion/kind", i+1)
		}
	}
	return nil
}
