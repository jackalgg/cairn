package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/generator"
	"github.com/jackalgg/cairn/internal/report"
	"github.com/spf13/cobra"
)

var (
	generateNamespace string
	generateKinds     []string
	generateName      string
	generateSelector  string
	generateOut       string
	generateHarden    bool
	generateDryRun    bool
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Export and clean manifests from a live cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}

		client, err := cluster.NewClient(kubeconfig, kubeContext)
		if err != nil {
			return fmt.Errorf("cluster client: %w", err)
		}

		k8sVersion := kubernetesVersion
		if k8sVersion == "1.30" && client.ServerVersion() != "" {
			k8sVersion = cluster.FormatServerVersion(client.ServerVersion())
		}
		target := targetVersion
		if target == "" {
			target = k8sVersion
		}

		eng, err := engine.New(engine.Options{
			KubernetesVersion: k8sVersion,
			TargetVersion:     target,
			SchemaValidation:  true,
			PolicyChecks:      true,
			CompatChecks:      true,
			MinSeverity:       engine.MustSeverity(minSeverity),
		})
		if err != nil {
			return err
		}

		result, err := generator.Generate(ctx, client, eng, generator.Options{
			Namespace: generateNamespace,
			Kinds:     generateKinds,
			Name:      generateName,
			Selector:  generateSelector,
			Harden:    generateHarden,
			OutDir:    generateOut,
			DryRun:    generateDryRun,
		})
		if err != nil {
			return err
		}

		if generateDryRun || generateOut == "" {
			for _, doc := range result.Documents {
				if err := report.WriteDocument(os.Stdout, doc); err != nil {
					return err
				}
			}
			return nil
		}

		for _, path := range result.Written {
			fmt.Fprintf(os.Stderr, "Wrote %s\n", path)
		}
		fmt.Fprintf(os.Stderr, "Generated %d manifest(s).\n", len(result.Written))
		return nil
	},
}

func init() {
	generateCmd.Flags().StringVarP(&generateNamespace, "namespace", "n", "default", "Namespace to export from")
	generateCmd.Flags().StringSliceVar(&generateKinds, "kind", nil, "Resource kinds to export (repeatable)")
	generateCmd.Flags().StringVar(&generateName, "name", "", "Filter by resource name")
	generateCmd.Flags().StringVar(&generateSelector, "selector", "", "Label selector")
	generateCmd.Flags().StringVarP(&generateOut, "output", "o", "", "Output directory")
	generateCmd.Flags().BoolVar(&generateHarden, "harden", true, "Apply security defaults to exported manifests")
	generateCmd.Flags().BoolVar(&generateDryRun, "dry-run", false, "Print manifests without writing files")
	rootCmd.AddCommand(generateCmd)
}
