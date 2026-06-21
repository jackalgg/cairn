package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/engine"
	"github.com/spf13/cobra"
)

var (
	outputFormat      string
	kubernetesVersion string
	clusterEnabled    bool
	kubeconfig        string
	kubeContext       string
	targetVersion     string
	minSeverity       string
)

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "Kubernetes YAML security scanner and remediation tool",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&outputFormat, "format", "human", "Output format: human|json")
	rootCmd.PersistentFlags().StringVar(&kubernetesVersion, "kubernetes-version", "1.30", "Kubernetes version for schema validation")
	rootCmd.PersistentFlags().BoolVar(&clusterEnabled, "cluster", false, "Probe live cluster for version and PSS context")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig file")
	rootCmd.PersistentFlags().StringVar(&kubeContext, "context", "", "Kubeconfig context name")
	rootCmd.PersistentFlags().StringVar(&targetVersion, "target-version", "", "Target Kubernetes version for API compatibility checks")
	rootCmd.PersistentFlags().StringVar(&minSeverity, "severity", "warning", "Minimum severity to report: error|warning|info")
}

func newEngine(ctx context.Context, schemaValidation, policyChecks, compatChecks bool) (*engine.Engine, error) {
	sev, err := engine.ParseSeverity(minSeverity)
	if err != nil {
		return nil, err
	}
	opts := engine.Options{
		KubernetesVersion: kubernetesVersion,
		TargetVersion:     targetVersion,
		SchemaValidation:  schemaValidation,
		PolicyChecks:      policyChecks,
		CompatChecks:      compatChecks,
		MinSeverity:       sev,
	}
	if clusterEnabled {
		probe, err := cluster.NewProbe(kubeconfig, kubeContext)
		if err != nil {
			return nil, fmt.Errorf("cluster probe: %w", err)
		}
		opts.Probe = probe
	}
	return engine.New(opts)
}
