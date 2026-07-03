package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/report"
	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan any YAML for syntax and configuration issues",
	Long: `Scan a YAML file, directory, or stdin for the common mistakes that keep
manifests from loading: tab indentation, inconsistent indentation, missing list
markers, and keys missing the space after a colon.

Documents that are Kubernetes resources (carry apiVersion and kind) are also
checked for policy issues, deprecated APIs, and, with --schema, schema
validity. Plain YAML is checked for syntax only.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		eng, err := newEngine(ctx, schemaValidation, true, true)
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

		if engine.HasErrors(result.Findings) {
			return fmt.Errorf("scan found error-severity findings")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
