package reindent

// This file implements identity repair: fixing a document's apiVersion and
// kind — the two fields that tell Kubernetes what the object IS, and that
// every schema-aware pass in cairn keys off. It repairs, per document:
//
//   - a wrong or deprecated apiVersion for a known kind
//     (`extensions/v1beta1` for a Deployment → `apps/v1`),
//   - a missing apiVersion (inserted, since it is derivable from the kind),
//   - kind value casing (`deployment` → `Deployment`), which also unlocks the
//     schema for every later pass in the pipeline,
//   - apiVersion/kind KEY casing (`apiversion:` → `apiVersion:`).
//
// It runs first in the pipeline because it needs nothing from the other passes
// (it re-emits matched lines in canonical `key: value` form, so even a jammed
// `kind:Deployment` is readable here) and because correcting the kind lets the
// colon/marker/reindent passes use their schema on documents they would
// otherwise treat as unknown.
//
// It is deliberately table-driven and exact: a kind not in the table is left
// completely alone (CRDs, non-Kubernetes YAML), and only top-level (column-0)
// apiVersion/kind lines are considered. Misspelled kind VALUES ("Deploymnet")
// are out of scope here — that is the typo pass's job.

import (
	"fmt"
	"regexp"
	"strings"
)

// canonicalAPI maps each kind cairn models to the single apiVersion current
// Kubernetes serves it at. Older group/versions (extensions/v1beta1,
// apps/v1beta2, batch/v1beta1…) were all removed by v1.25, so rewriting to the
// canonical version is safe for any live cluster.
var canonicalAPI = map[string]string{
	"Pod": "v1", "Service": "v1", "ConfigMap": "v1", "Secret": "v1",
	"Deployment": "apps/v1", "ReplicaSet": "apps/v1",
	"DaemonSet": "apps/v1", "StatefulSet": "apps/v1",
	"Job": "batch/v1", "CronJob": "batch/v1",
}

// identityLineRe matches a top-level apiVersion/kind line in any casing, jammed
// or spaced. Groups: 1=key as written, 2=everything after the colon.
var identityLineRe = regexp.MustCompile(`(?i)^(apiversion|kind)\s*:(.*)$`)

// FixIdentity repairs apiVersion and kind per document. It reports whether it
// changed anything and describes each repair as a Diagnostic (1-based input
// line numbers).
func FixIdentity(data []byte) (out []byte, changed bool, diags []Diagnostic) {
	text := string(data)
	trailingNewline := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")

	type insertion struct {
		at   int // insert before lines[at]
		line string
	}
	var inserts []insertion

	start := 0
	process := func(end int) {
		ins, ok := fixDocIdentity(lines, start, end, &diags)
		if ok {
			changed = true
		}
		if ins.line != "" {
			inserts = append(inserts, ins)
			changed = true
		}
	}
	for idx, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			process(idx)
			start = idx + 1
		}
	}
	process(len(lines))

	if !changed {
		return data, false, diags
	}
	for k := len(inserts) - 1; k >= 0; k-- {
		at := inserts[k].at
		lines = append(lines[:at], append([]string{inserts[k].line}, lines[at:]...)...)
	}
	rebuilt := strings.Join(lines, "\n")
	if trailingNewline {
		rebuilt += "\n"
	}
	return []byte(rebuilt), true, diags
}

// fixDocIdentity repairs one document's identity lines in place and returns a
// pending apiVersion insertion (zero-valued if none) plus whether any line was
// rewritten.
func fixDocIdentity(lines []string, start, end int, diags *[]Diagnostic) (ins struct {
	at   int
	line string
}, changed bool) {
	kindIdx, apiIdx := -1, -1
	var kindKey, kindRest, apiKey, apiRest string
	for i := start; i < end; i++ {
		m := identityLineRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if strings.EqualFold(m[1], "kind") && kindIdx < 0 {
			kindIdx, kindKey, kindRest = i, m[1], m[2]
		}
		if strings.EqualFold(m[1], "apiVersion") && apiIdx < 0 {
			apiIdx, apiKey, apiRest = i, m[1], m[2]
		}
	}

	// Resolve the kind, fixing value casing when it identifies a known kind.
	kind := ""
	if kindIdx >= 0 {
		val, comment := splitValueComment(kindRest)
		if canonical, ok := matchKind(val); ok {
			kind = canonical
			if val != canonical || kindKey != "kind" {
				lines[kindIdx] = renderIdentity("kind", canonical, comment)
				changed = true
				if val != canonical {
					*diags = append(*diags, Diagnostic{kindIdx + 1,
						fmt.Sprintf("kind %q → %q", val, canonical)})
				} else {
					*diags = append(*diags, Diagnostic{kindIdx + 1,
						fmt.Sprintf("normalized key %q → \"kind\"", kindKey)})
				}
			}
		}
	}

	switch {
	case kind != "" && apiIdx >= 0:
		canonical := canonicalAPI[kind]
		val, comment := splitValueComment(apiRest)
		if val != canonical {
			lines[apiIdx] = renderIdentity("apiVersion", canonical, comment)
			changed = true
			*diags = append(*diags, Diagnostic{apiIdx + 1,
				fmt.Sprintf("apiVersion %q → %q (canonical for %s)", val, canonical, kind)})
		} else if apiKey != "apiVersion" {
			lines[apiIdx] = renderIdentity("apiVersion", canonical, comment)
			changed = true
			*diags = append(*diags, Diagnostic{apiIdx + 1,
				fmt.Sprintf("normalized key %q → \"apiVersion\"", apiKey)})
		}
	case kind != "" && apiIdx < 0:
		// apiVersion is derivable from the kind: insert it before the first
		// content line of the document.
		for i := start; i < end; i++ {
			t := strings.TrimSpace(lines[i])
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			ins.at = i
			ins.line = "apiVersion: " + canonicalAPI[kind]
			*diags = append(*diags, Diagnostic{i + 1,
				fmt.Sprintf("inserted missing \"apiVersion: %s\" (required for %s)", canonicalAPI[kind], kind)})
			break
		}
	case kind == "" && apiIdx >= 0 && apiKey != "apiVersion":
		// Unknown kind, but the apiVersion KEY casing is fixable on its own;
		// the value is preserved verbatim.
		lines[apiIdx] = "apiVersion:" + apiRest
		changed = true
		*diags = append(*diags, Diagnostic{apiIdx + 1,
			fmt.Sprintf("normalized key %q → \"apiVersion\"", apiKey)})
	}
	return ins, changed
}

// matchKind resolves a kind value against the known table, tolerating casing.
func matchKind(v string) (string, bool) {
	if v == "" {
		return "", false
	}
	if _, ok := canonicalAPI[v]; ok {
		return v, true
	}
	for k := range canonicalAPI {
		if strings.EqualFold(k, v) {
			return k, true
		}
	}
	return "", false
}

// splitValueComment separates a raw after-colon remainder into its value
// (unquoted, trimmed) and any trailing comment.
func splitValueComment(rest string) (val, comment string) {
	if i := strings.Index(rest, " #"); i >= 0 {
		comment = strings.TrimSpace(rest[i:])
		rest = rest[:i]
	} else if t := strings.TrimSpace(rest); strings.HasPrefix(t, "#") {
		return "", t
	}
	val = strings.Trim(strings.TrimSpace(rest), `"'`)
	return val, comment
}

func renderIdentity(key, val, comment string) string {
	if comment != "" {
		return key + ": " + val + " " + comment
	}
	return key + ": " + val
}
