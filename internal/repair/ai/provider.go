package ai

import (
	"context"

	"github.com/jackalgg/cairn/internal/model"
)

// Provider suggests repairs for YAML that deterministic rules could not fix.
type Provider interface {
	SuggestRepair(ctx context.Context, rawYAML []byte, parseErr error, hints []model.Finding) (patched []byte, explanation string, err error)
}

// NoopProvider is the default when --ai is not configured.
type NoopProvider struct{}

func (NoopProvider) SuggestRepair(ctx context.Context, rawYAML []byte, parseErr error, hints []model.Finding) ([]byte, string, error) {
	return nil, "", ErrNotConfigured
}

var ErrNotConfigured = errNotConfigured{}

type errNotConfigured struct{}

func (errNotConfigured) Error() string {
	return "AI repair provider is not configured"
}
