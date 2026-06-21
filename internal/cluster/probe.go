package cluster

import (
	"context"
	"fmt"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Info struct {
	ServerVersion string
}

type Probe struct {
	info *Info
}

func NewProbe(kubeconfig, contextName string) (*Probe, error) {
	config, err := loadConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	client, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	version, err := client.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cluster probe: %w", err)
	}
	return &Probe{info: &Info{ServerVersion: version.GitVersion}}, nil
}

func loadConfig(kubeconfig, contextName string) (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	configOverrides := &clientcmd.ConfigOverrides{}
	if contextName != "" {
		configOverrides.CurrentContext = contextName
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

func (p *Probe) EnrichFindings(ctx context.Context, findings []model.Finding) []model.Finding {
	if p == nil || p.info == nil {
		return findings
	}
	for i := range findings {
		if findings[i].Severity == model.SeverityWarning && findings[i].Source == model.SourcePolicy {
			if findings[i].RuleID == "pss-run-as-non-root" || findings[i].RuleID == "pss-read-only-rootfs" {
				findings[i].Message = fmt.Sprintf("%s (cluster %s may enforce Pod Security Standards)", findings[i].Message, p.info.ServerVersion)
			}
		}
	}
	return findings
}

func (p *Probe) ServerVersion() string {
	if p == nil || p.info == nil {
		return ""
	}
	return p.info.ServerVersion
}
