package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "Kubernetes security enforcer",
}

func Execute() {
	rootCmd.Execute()
}
