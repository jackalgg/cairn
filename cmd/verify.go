package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/verify"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify [path]",
	Short: "Check whether YAML would be accepted by kubectl apply",
	Long: `Verify that manifests would apply cleanly using a server-side dry-run against
the cluster (the same path kubectl apply --dry-run=server uses). When no cluster
is reachable, pass --schema to validate against OpenAPI schemas offline instead.

verify does not modify files; it only reports per-document pass/fail and exits
non-zero if any Kubernetes document would be rejected.`,
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

		verifier := buildVerifier()
		if !verifier.Available() {
			return fmt.Errorf("no verification backend available (start a cluster, or pass --schema for offline validation)")
		}
		fmt.Fprintf(os.Stderr, "Verification: %s\n", verifier.Backend())

		result, err := eng.ScanPath(ctx, args[0])
		if err != nil {
			return err
		}

		results := verifier.Verify(ctx, result.Documents)
		if len(results) == 0 {
			fmt.Fprintln(os.Stderr, "No Kubernetes documents to verify.")
			return nil
		}

		for _, r := range results {
			name := r.Name
			if name == "" {
				name = "(unnamed)"
			}
			if r.OK {
				fmt.Fprintf(os.Stdout, "VERIFIED %s %s\n", r.GVK, name)
			} else {
				fmt.Fprintf(os.Stdout, "VERIFY FAILED %s %s: %s\n", r.GVK, name, r.Message)
			}
		}

		if !verify.AllOK(results) {
			return fmt.Errorf("one or more documents would not apply")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
}
