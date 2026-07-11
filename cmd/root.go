package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "Fix broken indentation in YAML (and Kubernetes/Docker manifests)",
	Long: `cairn repairs YAML files whose indentation is broken so they no longer
parse. It reconstructs the document's nesting — using knowledge of common
Kubernetes manifest structure — and rewrites every line at the correct depth.
Only leading whitespace changes; values, quoting and block scalars are left
exactly as they were.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
