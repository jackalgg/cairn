package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix [file]",
	Short: "Fix Kubernetes YAML",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Fixing:", args[0])
	},
}

func init() {
	rootCmd.AddCommand(fixCmd)
}
