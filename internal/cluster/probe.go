package cluster

import (
	"context"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Probe struct {
	client *Client
}

func NewProbe(kubeconfig, contextName string) (*Probe, error) {
	client, err := NewClient(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	return &Probe{client: client}, nil
}

func (p *Probe) EnrichFindings(ctx context.Context, findings []model.Finding) []model.Finding {
	if p == nil || p.client == nil {
		return findings
	}
	return p.client.EnrichFindings(ctx, findings)
}

func (p *Probe) ServerVersion() string {
	if p == nil || p.client == nil {
		return ""
	}
	return p.client.ServerVersion()
}

func (p *Probe) Client() *Client {
	if p == nil {
		return nil
	}
	return p.client
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
