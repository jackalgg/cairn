package schema

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/repair/schemafix"
	"github.com/yannh/kubeconform/pkg/validator"
)

type Validator struct {
	v validator.Validator
}

func New(kubernetesVersion string, strict bool) (*Validator, error) {
	opts := validator.Opts{
		KubernetesVersion:    kubernetesVersion,
		Strict:               strict,
		IgnoreMissingSchemas: true,
	}
	v, err := validator.New(nil, opts)
	if err != nil {
		return nil, err
	}
	return &Validator{v: v}, nil
}

func (s *Validator) ValidateDocument(ctx context.Context, doc model.Document) []model.Finding {
	reader := bytes.NewReader(doc.RawYAML)
	results := s.v.ValidateWithContext(ctx, doc.Source, ioNopCloser(reader))
	var findings []model.Finding
	for _, res := range results {
		if res.Status == validator.Valid || res.Status == validator.Skipped || res.Status == validator.Empty {
			continue
		}
		if res.Status == validator.Error {
			findings = append(findings, schemafix.EnrichFinding(doc, model.Finding{
				RuleID:       "schema-error",
				Message:      fmt.Sprintf("schema validation error: %v", res.Err),
				Path:         "",
				GVK:          doc.GVK,
				GVKString:    doc.GVK.String(),
				ResourceName: doc.Name(),
				Source:       model.SourceSchema,
				Severity:     model.SeverityError,
				DocIndex:     doc.Index,
				SourceFile:   doc.Source,
			}))
			continue
		}
		for _, ve := range res.ValidationErrors {
			ruleID := schemafix.ClassifyMessage(ve.Msg)
			findings = append(findings, schemafix.EnrichFinding(doc, model.Finding{
				RuleID:       ruleID,
				Message:      ve.Msg,
				Path:         ve.Path,
				GVK:          doc.GVK,
				GVKString:    doc.GVK.String(),
				ResourceName: doc.Name(),
				Source:       model.SourceSchema,
				Severity:     model.SeverityError,
				DocIndex:     doc.Index,
				SourceFile:   doc.Source,
			}))
		}
		if len(res.ValidationErrors) == 0 && res.Err != nil {
			findings = append(findings, schemafix.EnrichFinding(doc, model.Finding{
				RuleID:       "schema-validation",
				Message:      res.Err.Error(),
				Path:         "",
				GVK:          doc.GVK,
				GVKString:    doc.GVK.String(),
				ResourceName: doc.Name(),
				Source:       model.SourceSchema,
				Severity:     model.SeverityError,
				DocIndex:     doc.Index,
				SourceFile:   doc.Source,
			}))
		}
	}
	return findings
}

type nopCloser struct {
	*bytes.Reader
}

func ioNopCloser(r *bytes.Reader) nopCloser {
	return nopCloser{Reader: r}
}

func (nopCloser) Close() error { return nil }

func FormatSchemaVersion(version string) string {
	if version == "" {
		return "master"
	}
	return strings.TrimPrefix(version, "v")
}
