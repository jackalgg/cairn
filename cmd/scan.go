package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var scanCmd = &cobra.Command{
	Use:   "scan [file]",
	Short: "Scan Kubernetes YAML",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Scanning:", args[0])
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
}
