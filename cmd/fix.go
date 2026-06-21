package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/fixer"
	"github.com/jackalgg/cairn/internal/k8serrors"
	"github.com/jackalgg/cairn/internal/report"
	"github.com/spf13/cobra"
)

var (
	fixDryRun     bool
	fixOut        string
	fixIDs        []string
	fixFromError  string
	fixStdinErrors bool
)

var fixCmd = &cobra.Command{
	Use:   "fix [path]",
	Short: "Apply remediations to Kubernetes YAML",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		eng, err := newEngine(ctx, true, true, true)
		if err != nil {
			return err
		}

		result, err := eng.ScanPath(ctx, args[0])
		if err != nil {
			return err
		}

		findings := result.Findings

		if fixFromError != "" || fixStdinErrors {
			errorText, err := readErrorText(fixFromError, fixStdinErrors)
			if err != nil {
				return err
			}
			ruleIDs := k8serrors.RuleIDsForFix(errorText)
			if len(ruleIDs) == 0 {
				return fmt.Errorf("no fixable rules matched the provided error text")
			}
			findings = engine.FilterByRuleID(findings, ruleIDs)
			if len(findings) == 0 {
				for _, doc := range result.Documents {
					findings = append(findings, k8serrors.FindingsFromError(doc, errorText)...)
				}
			}
		}

		if len(fixIDs) > 0 {
			findings = engine.FilterByRuleID(findings, fixIDs)
		}

		findings = engine.FilterFixable(findings)
		if len(findings) == 0 {
			fmt.Fprintln(os.Stderr, "No fixable findings.")
			return nil
		}

		fileResults, err := fixer.Apply(result, findings, fixer.Options{
			DryRun: fixDryRun,
			OutDir: fixOut,
		})
		if err != nil {
			return err
		}

		for _, fr := range fileResults {
			if fixDryRun || fixOut != "" {
				fmt.Print(fr.Diff)
			}
			if fr.Written {
				fmt.Fprintf(os.Stderr, "Wrote %s\n", fr.OutputPath)
			}
		}

		if fixDryRun {
			return nil
		}

		remaining, err := eng.ScanPath(ctx, args[0])
		if err != nil {
			return err
		}
		if err := report.WriteScanResult(os.Stderr, remaining, report.Format(outputFormat)); err != nil {
			return err
		}
		if engine.HasErrors(remaining.Findings) {
			return fmt.Errorf("unfixed error-severity findings remain")
		}
		return nil
	},
}

func readErrorText(fromFlag string, stdin bool) (string, error) {
	var parts []string
	if fromFlag != "" {
		parts = append(parts, fromFlag)
	}
	if stdin {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			parts = append(parts, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("no error text provided")
	}
	return strings.Join(parts, "\n"), nil
}

func init() {
	fixCmd.Flags().BoolVar(&fixDryRun, "dry-run", false, "Show diff without writing files")
	fixCmd.Flags().StringVar(&fixOut, "out", "", "Write fixed manifests to directory")
	fixCmd.Flags().StringSliceVar(&fixIDs, "fix-id", nil, "Apply only findings with these rule IDs")
	fixCmd.Flags().StringVar(&fixFromError, "from-error", "", "Apply fixes matching kubectl/admission error text")
	fixCmd.Flags().BoolVar(&fixStdinErrors, "stdin-errors", false, "Read kubectl error text from stdin")
	rootCmd.AddCommand(fixCmd)
}
