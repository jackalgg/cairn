package fixer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/sergi/go-diff/diffmatchpatch"
	"sigs.k8s.io/yaml"
)

type Options struct {
	DryRun  bool
	OutDir  string
	InPlace bool
}

type FileResult struct {
	SourceFile string
	Before     []byte
	After      []byte
	Diff       string
	Written    bool
	OutputPath string
}

func Apply(result *model.ScanResult, findings []model.Finding, opts Options) ([]FileResult, error) {
	fixable := engine.FilterFixable(findings)
	if len(fixable) == 0 {
		return nil, nil
	}

	working := make(map[string]model.Document, len(result.Documents))
	for _, d := range result.Documents {
		working[d.Key()] = engine.CloneDocument(d)
	}

	rawBySource := rawContentBySource(result.Documents)

	applied := map[string]struct{}{}
	for _, f := range fixable {
		key := fmt.Sprintf("%s:%s:%d", f.SourceFile, f.RuleID, f.DocIndex)
		if _, ok := applied[key]; ok {
			continue
		}
		applied[key] = struct{}{}

		if f.Fix != nil && f.Fix.ApplyRaw != nil {
			raw, ok := rawBySource[f.SourceFile]
			if !ok {
				continue
			}
			patched, err := f.Fix.ApplyRaw(raw)
			if err != nil {
				return nil, fmt.Errorf("%s: raw fix %s: %w", f.SourceFile, f.RuleID, err)
			}
			rawBySource[f.SourceFile] = patched
			continue
		}

		docKey := model.DocumentKey(f.SourceFile, f.DocIndex)
		doc, ok := working[docKey]
		if !ok {
			continue
		}
		if err := engine.ApplyFixToDocument(&doc, f.Fix); err != nil {
			return nil, fmt.Errorf("%s doc %d: %w", f.SourceFile, f.DocIndex, err)
		}
		working[docKey] = doc
	}

	bySource := groupDocumentsBySource(working)
	var fileResults []FileResult
	for source, docs := range bySource {
		before := joinDocuments(originalBySource(result.Documents, source))
		var after []byte
		if raw, ok := rawBySource[source]; ok && hasRawFixForSource(fixable, source) {
			after = raw
		} else {
			var err error
			after, err = marshalDocuments(docs)
			if err != nil {
				return nil, fmt.Errorf("marshal %s: %w", source, err)
			}
		}
		if bytes.Equal(before, after) {
			continue
		}

		fr := FileResult{
			SourceFile: source,
			Before:     before,
			After:      after,
			Diff:       unifiedDiff(source, string(before), string(after)),
		}

		if opts.DryRun {
			fileResults = append(fileResults, fr)
			continue
		}

		if opts.OutDir != "" {
			outPath, err := outputPath(opts.OutDir, source)
			if err != nil {
				return nil, err
			}
			if err := writeFileAtomic(outPath, after, 0o600); err != nil {
				return nil, err
			}
			fr.Written = true
			fr.OutputPath = outPath
		} else if source != "<stdin>" {
			if !opts.InPlace {
				return nil, fmt.Errorf("refusing to overwrite %s without --in-place (use --out or --dry-run)", source)
			}
			if err := writeFileAtomic(source, after, 0o600); err != nil {
				return nil, err
			}
			fr.Written = true
			fr.OutputPath = source
		}

		fileResults = append(fileResults, fr)
	}
	return fileResults, nil
}

func hasRawFixForSource(findings []model.Finding, source string) bool {
	for _, f := range findings {
		if f.SourceFile == source && f.Fix != nil && f.Fix.ApplyRaw != nil {
			return true
		}
	}
	return false
}

func rawContentBySource(docs []model.Document) map[string][]byte {
	out := map[string][]byte{}
	for _, d := range docs {
		if _, ok := out[d.Source]; !ok {
			out[d.Source] = joinDocuments(originalBySource(docs, d.Source))
		}
	}
	return out
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cairn-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func originalBySource(docs []model.Document, source string) []model.Document {
	var out []model.Document
	for _, d := range docs {
		if d.Source == source {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

func groupDocumentsBySource(working map[string]model.Document) map[string][]model.Document {
	bySource := map[string][]model.Document{}
	for _, d := range working {
		bySource[d.Source] = append(bySource[d.Source], d)
	}
	for source := range bySource {
		sort.Slice(bySource[source], func(i, j int) bool {
			return bySource[source][i].Index < bySource[source][j].Index
		})
	}
	return bySource
}

func joinDocuments(docs []model.Document) []byte {
	var parts [][]byte
	for _, d := range docs {
		parts = append(parts, bytes.TrimSpace(d.RawYAML))
	}
	return []byte(strings.Join(sliceOfStrings(parts), "\n---\n"))
}

func sliceOfStrings(b [][]byte) []string {
	out := make([]string, len(b))
	for i, p := range b {
		out[i] = string(p)
	}
	return out
}

func marshalDocuments(docs []model.Document) ([]byte, error) {
	var parts []string
	for _, d := range docs {
		data, err := yaml.Marshal(d.Object.Object)
		if err != nil {
			return nil, err
		}
		parts = append(parts, strings.TrimSpace(string(data)))
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(parts, "\n---\n") + "\n"), nil
}

func unifiedDiff(path, before, after string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(before, after, false)
	return fmt.Sprintf("--- %s\n+++ %s (fixed)\n%s", path, path, dmp.DiffPrettyText(diffs))
}

// Diff renders a colored before/after diff for a source. Exported so the raw
// syntax-repair path can reuse the same presentation as findings-based fixes.
func Diff(path, before, after string) string {
	return unifiedDiff(path, before, after)
}

// WriteAtomic writes data to path via a temp file + rename with 0600 perms.
func WriteAtomic(path string, data []byte) error {
	return writeFileAtomic(path, data, 0o600)
}

// OutputPath resolves the destination for source within outDir, rejecting paths
// that would escape the output directory.
func OutputPath(outDir, source string) (string, error) {
	return outputPath(outDir, source)
}

func outputPath(outDir, source string) (string, error) {
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	if source == "<stdin>" {
		return filepath.Join(absOut, "fixed.yaml"), nil
	}
	base := filepath.Base(filepath.Clean(source))
	candidate := filepath.Join(absOut, base)
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absOut, absCandidate)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return "", fmt.Errorf("output path escapes directory: %s", source)
	}
	return absCandidate, nil
}
