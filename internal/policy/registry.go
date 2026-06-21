package policy

import (
	"context"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Rule interface {
	ID() string
	AppliesTo(u *unstructured.Unstructured) bool
	Check(ctx context.Context, doc model.Document) []model.Finding
}

var defaultRules = []Rule{
	&RunAsNonRootRule{},
	&ReadOnlyRootFSRule{},
	&FloatingTagRule{},
}

func DefaultRules() []Rule {
	out := make([]Rule, len(defaultRules))
	copy(out, defaultRules)
	return out
}
