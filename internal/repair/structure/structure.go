package structure

import (
	"context"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/repair/schemafix"
	"github.com/jackalgg/cairn/internal/schema"
)

type Rule interface {
	ID() string
	Check(ctx context.Context, doc model.Document, validator *schema.Validator) []model.Finding
}

var defaultRules = []Rule{
	&SchemaRule{},
}

func DefaultRules() []Rule {
	out := make([]Rule, len(defaultRules))
	copy(out, defaultRules)
	return out
}

type SchemaRule struct{}

func (r *SchemaRule) ID() string { return "schema-structure" }

func (r *SchemaRule) Check(ctx context.Context, doc model.Document, validator *schema.Validator) []model.Finding {
	if validator == nil {
		return nil
	}
	rawFindings := validator.ValidateDocument(ctx, doc)
	var out []model.Finding
	for _, f := range rawFindings {
		out = append(out, schemafix.EnrichFinding(doc, f))
	}
	return out
}
