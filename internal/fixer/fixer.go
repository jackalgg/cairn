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
	DryRun bool
	OutDir string
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

	working := make(map[int]model.Document, len(result.Documents))
	for _, d := range result.Documents {
		working[d.Index] = engine.CloneDocument(d)
	}

	applied := map[string]struct{}{}
	for _, f := range fixable {
		key := fmt.Sprintf("%d:%s", f.DocIndex, f.RuleID)
		if _, ok := applied[key]; ok {
			continue
		}
		applied[key] = struct{}{}
		doc, ok := working[f.DocIndex]
		if !ok {
			continue
		}
		if err := engine.ApplyFixToDocument(&doc, f.Fix); err != nil {
			return nil, fmt.Errorf("%s doc %d: %w", f.SourceFile, f.DocIndex, err)
		}
		working[f.DocIndex] = doc
	}

	bySource := groupDocumentsBySource(working)
	var fileResults []FileResult
	for source, docs := range bySource {
		before := joinDocuments(originalBySource(result.Documents, source))
		after, err := marshalDocuments(docs)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", source, err)
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

		if opts.OutDir != "" && !opts.DryRun {
			outPath, err := outputPath(opts.OutDir, source)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(outPath, after, 0o644); err != nil {
				return nil, err
			}
			fr.Written = true
			fr.OutputPath = outPath
		} else if !opts.DryRun && opts.OutDir == "" && source != "<stdin>" {
			if err := os.WriteFile(source, after, 0o644); err != nil {
				return nil, err
			}
			fr.Written = true
			fr.OutputPath = source
		}

		fileResults = append(fileResults, fr)
	}
	return fileResults, nil
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

func groupDocumentsBySource(working map[int]model.Document) map[string][]model.Document {
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

func outputPath(outDir, source string) (string, error) {
	if source == "<stdin>" {
		return filepath.Join(outDir, "fixed.yaml"), nil
	}
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(source) {
		return filepath.Join(absOut, filepath.Base(source)), nil
	}
	return filepath.Join(absOut, source), nil
}
