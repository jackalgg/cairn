package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackalgg/cairn/internal/parser"
	"github.com/jackalgg/cairn/internal/reindent"
	"github.com/spf13/cobra"
)

var (
	fixDryRun  bool
	fixInPlace bool
	fixCheck   bool
)

var fixCmd = &cobra.Command{
	Use:   "fix [path]",
	Short: "Repair broken YAML indentation",
	Long: `Repair the indentation of a YAML file, directory of YAML files, or stdin
so it parses again.

By default fix prints the repaired YAML (or a diff, with --dry-run) without
touching your files. Use --in-place to overwrite the source. For Kubernetes
manifests fix understands the object model, so a field that was indented into
the wrong block is pulled back to where it belongs.`,
	Args: cobra.ExactArgs(1),
	RunE: runFix,
}

func runFix(cmd *cobra.Command, args []string) error {
	sources, err := collectSources(args[0])
	if err != nil {
		return err
	}

	anyFailed := false
	for _, src := range sources {
		fixed, changed := reindent.Reindent(src.data)

		stillBroken := parser.ValidateYAML(fixed) != nil
		if stillBroken {
			anyFailed = true
			fmt.Fprintf(os.Stderr, "%s: still not valid YAML after reindent: %v\n",
				src.name, parser.ValidateYAML(fixed))
		}

		if fixCheck {
			if changed || stillBroken {
				anyFailed = true
				fmt.Fprintf(os.Stderr, "%s: needs indentation repair\n", src.name)
			}
			continue
		}

		if err := emit(src, fixed, changed); err != nil {
			return err
		}
	}

	if anyFailed {
		return fmt.Errorf("unresolved errors remain")
	}
	return nil
}

func emit(src source, fixed []byte, changed bool) error {
	switch {
	case src.name == "<stdin>":
		_, err := os.Stdout.Write(fixed)
		return err
	case fixDryRun:
		fmt.Print(diff(src.name, src.data, fixed))
	case fixInPlace:
		if !changed {
			fmt.Fprintf(os.Stderr, "%s: nothing to repair\n", src.name)
			return nil
		}
		if err := os.WriteFile(src.name, fixed, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Wrote %s\n", src.name)
	default:
		os.Stdout.Write(fixed)
		if changed {
			fmt.Fprintf(os.Stderr, "(preview only; use --in-place to overwrite %s)\n", src.name)
		}
	}
	return nil
}

type source struct {
	name string
	data []byte
}

func collectSources(path string) ([]source, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		return []source{{name: "<stdin>", data: data}}, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []source{{name: path, data: data}}, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var sources []source
	for _, e := range entries {
		if e.IsDir() || !isYAML(e.Name()) {
			continue
		}
		full := filepath.Join(path, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source{name: full, data: data})
	}
	return sources, nil
}

func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// diff renders a minimal line-by-line before/after so a preview shows exactly
// which lines were re-indented.
func diff(name string, before, after []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s (fixed)\n", name, name)
	beforeLines := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	afterLines := strings.Split(strings.TrimSuffix(string(after), "\n"), "\n")
	max := len(beforeLines)
	if len(afterLines) > max {
		max = len(afterLines)
	}
	for i := 0; i < max; i++ {
		var bl, al string
		if i < len(beforeLines) {
			bl = beforeLines[i]
		}
		if i < len(afterLines) {
			al = afterLines[i]
		}
		if bl == al {
			fmt.Fprintf(&b, "  %s\n", al)
			continue
		}
		if i < len(beforeLines) {
			fmt.Fprintf(&b, "- %s\n", bl)
		}
		if i < len(afterLines) {
			fmt.Fprintf(&b, "+ %s\n", al)
		}
	}
	return b.String()
}

func init() {
	fixCmd.Flags().BoolVar(&fixDryRun, "dry-run", false, "Show a diff instead of writing")
	fixCmd.Flags().BoolVar(&fixInPlace, "in-place", false, "Overwrite the source file(s)")
	fixCmd.Flags().BoolVar(&fixCheck, "check", false, "Report whether files need repair; make no changes (exit non-zero if any do)")
	rootCmd.AddCommand(fixCmd)
}
