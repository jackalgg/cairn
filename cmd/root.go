package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/repair/ai"
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
	schemaValidation  bool
	fixRepairSyntax   bool
	fixRepairOnly     string
	fixMaxRounds      int
	fixInPlace        bool
	fixAI             bool
	fixAcceptRisk     bool
)

var rootCmd = &cobra.Command{
	Use:   "cairn",
	Short: "Scan and repair misconfigured YAML (and harden Kubernetes manifests)",
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
	rootCmd.PersistentFlags().BoolVar(&schemaValidation, "schema", false, "Validate Kubernetes resources against OpenAPI schemas (requires network on first run)")
}

func newEngine(ctx context.Context, schemaValidation, policyChecks, compatChecks bool) (*engine.Engine, error) {
	sev, err := engine.ParseSeverity(minSeverity)
	if err != nil {
		return nil, err
	}
	repairOnly, err := engine.ParseRepairOnly(fixRepairOnly)
	if err != nil {
		return nil, err
	}
	opts := engine.Options{
		KubernetesVersion: kubernetesVersion,
		TargetVersion:     targetVersion,
		SchemaValidation:  schemaValidation,
		PolicyChecks:      policyChecks,
		CompatChecks:      compatChecks,
		SyntaxRepair:      fixRepairSyntax,
		RepairOnly:        repairOnly,
		MinSeverity:       sev,
		AcceptRisk:        fixAcceptRisk,
	}
	if fixAI {
		opts.AIProvider = ai.NoopProvider{}
	}
	if clusterEnabled {
		probe, err := cluster.NewProbe(kubeconfig, kubeContext)
		if err != nil {
			return nil, fmt.Errorf("cluster probe: %w", err)
		}
		opts.Probe = probe
		if kubernetesVersion == "1.30" && probe.ServerVersion() != "" {
			opts.KubernetesVersion = cluster.FormatServerVersion(probe.ServerVersion())
		}
		if targetVersion == "" {
			opts.TargetVersion = opts.KubernetesVersion
		}
	}
	return engine.New(opts)
}
