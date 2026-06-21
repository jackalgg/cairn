package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/fixer"
	"github.com/jackalgg/cairn/internal/report"
	"github.com/spf13/cobra"
)

var compatCmd = &cobra.Command{
	Use:   "compat [path]",
	Short: "Check and fix API version compatibility for target Kubernetes version",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		eng, err := newEngine(ctx, false, false, true)
		if err != nil {
			return err
		}

		result, err := eng.ScanPath(ctx, args[0])
		if err != nil {
			return err
		}

		if err := report.WriteScanResult(os.Stdout, result, report.Format(outputFormat)); err != nil {
			return err
		}

		findings := engine.FilterFixable(result.Findings)
		if len(findings) == 0 {
			if engine.HasErrors(result.Findings) {
				return fmt.Errorf("compatibility issues found without auto-fix")
			}
			return nil
		}

		fileResults, err := fixer.Apply(result, findings, fixer.Options{
			DryRun: compatDryRun,
			OutDir: compatOut,
		})
		if err != nil {
			return err
		}

		for _, fr := range fileResults {
			if compatDryRun || compatOut != "" {
				fmt.Print(fr.Diff)
			}
			if fr.Written {
				fmt.Fprintf(os.Stderr, "Wrote %s\n", fr.OutputPath)
			}
		}

		if compatDryRun {
			return nil
		}

		if engine.HasErrors(result.Findings) {
			return fmt.Errorf("compatibility issues remain")
		}
		return nil
	},
}

var (
	compatDryRun bool
	compatOut    string
)

func init() {
	compatCmd.Flags().BoolVar(&compatDryRun, "dry-run", false, "Show diff without writing files")
	compatCmd.Flags().StringVar(&compatOut, "out", "", "Write migrated manifests to directory")
	rootCmd.AddCommand(compatCmd)
}
