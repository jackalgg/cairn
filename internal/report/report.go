package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"sigs.k8s.io/yaml"
)

type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
)

type jsonFinding struct {
	RuleID       string `json:"ruleId"`
	Message      string `json:"message"`
	Path         string `json:"path"`
	GVK          string `json:"gvk"`
	ResourceName string `json:"resourceName"`
	Source       string `json:"source"`
	Severity     string `json:"severity"`
	DocIndex     int    `json:"docIndex"`
	SourceFile   string `json:"sourceFile"`
	Line         int    `json:"line,omitempty"`
	HasFix       bool   `json:"hasFix"`
}

type jsonOutput struct {
	Summary struct {
		Documents int `json:"documents"`
		Findings  int `json:"findings"`
		Errors    int `json:"errors"`
		Warnings  int `json:"warnings"`
		Fixable   int `json:"fixable"`
	} `json:"summary"`
	Findings []jsonFinding `json:"findings"`
}

func WriteScanResult(w io.Writer, result *model.ScanResult, format Format) error {
	switch format {
	case FormatJSON:
		return writeJSON(w, result)
	case FormatHuman, "":
		return writeHuman(w, result)
	default:
		return fmt.Errorf("unknown format %q (use human or json)", format)
	}
}

func writeHuman(w io.Writer, result *model.ScanResult) error {
	if len(result.Findings) == 0 {
		fmt.Fprintf(w, "No findings (%d document(s) scanned).\n", len(result.Documents))
		return nil
	}

	order := sortedFindings(result.Findings)
	for _, f := range order {
		fixable := ""
		if f.CanRepair() {
			fixable = " [fixable]"
		}
		location := f.SourceFile
		if f.Line > 0 {
			location = fmt.Sprintf("%s:%d", f.SourceFile, f.Line)
		}
		resource := fmt.Sprintf("%s/%s", f.GVKString, f.ResourceName)
		if f.GVKString == "" && f.ResourceName == "" {
			resource = ""
		}
		fmt.Fprintf(w, "%s %s %s %s\n  %s%s\n  path: %s\n  file: %s (doc %d)\n\n",
			strings.ToUpper(string(f.Severity)),
			f.RuleID,
			resource,
			f.Source,
			f.Message,
			fixable,
			f.Path,
			location,
			f.DocIndex,
		)
	}

	errors, warnings, fixable := countFindings(result.Findings)
	fmt.Fprintf(w, "Summary: %d finding(s) (%d error, %d warning, %d fixable) across %d document(s)\n",
		len(result.Findings), errors, warnings, fixable, len(result.Documents))
	return nil
}

func writeJSON(w io.Writer, result *model.ScanResult) error {
	errors, warnings, fixable := countFindings(result.Findings)
	out := jsonOutput{}
	out.Summary.Documents = len(result.Documents)
	out.Summary.Findings = len(result.Findings)
	out.Summary.Errors = errors
	out.Summary.Warnings = warnings
	out.Summary.Fixable = fixable

	for _, f := range sortedFindings(result.Findings) {
		out.Findings = append(out.Findings, jsonFinding{
			RuleID:       f.RuleID,
			Message:      f.Message,
			Path:         f.Path,
			GVK:          f.GVKString,
			ResourceName: f.ResourceName,
			Source:       string(f.Source),
			Severity:     string(f.Severity),
			DocIndex:     f.DocIndex,
			SourceFile:   f.SourceFile,
			Line:         f.Line,
			HasFix:       f.CanRepair(),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func sortedFindings(findings []model.Finding) []model.Finding {
	out := append([]model.Finding(nil), findings...)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.SourceFile != b.SourceFile {
			return a.SourceFile < b.SourceFile
		}
		if a.DocIndex != b.DocIndex {
			return a.DocIndex < b.DocIndex
		}
		return a.RuleID < b.RuleID
	})
	return out
}

func countFindings(findings []model.Finding) (errors, warnings, fixable int) {
	for _, f := range findings {
		switch f.Severity {
		case model.SeverityError:
			errors++
		case model.SeverityWarning:
			warnings++
		}
		if f.CanRepair() {
			fixable++
		}
	}
	return errors, warnings, fixable
}

func PrintFixDiffs(w io.Writer, diffs []string) {
	for _, d := range diffs {
		if d == "" {
			continue
		}
		fmt.Fprintln(w, d)
	}
}

func WriteDocument(w io.Writer, doc model.Document) error {
	data, err := yaml.Marshal(doc.Object.Object)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "---\n%s", data); err != nil {
		return err
	}
	return nil
}
