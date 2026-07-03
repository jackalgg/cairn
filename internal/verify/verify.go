// Package verify confirms that repaired YAML would actually be accepted by the
// Kubernetes API server. The preferred backend is a server-side dry-run (the
// same path `kubectl apply --dry-run=server` uses); when no cluster is reachable
// it falls back to offline schema validation.
package verify

import (
	"context"
	"fmt"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/model"
	"github.com/jackalgg/cairn/internal/schema"
)

// Runner performs a single appliability check for one document. A nil error
// means the document would apply; a non-nil error carries the rejection reason.
type Runner interface {
	DryRun(ctx context.Context, doc model.Document) error
	Backend() string
}

// Result is the verification outcome for a single document.
type Result struct {
	DocIndex int
	Name     string
	GVK      string
	OK       bool
	Message  string
}

// Verifier checks a set of documents with a configured Runner.
type Verifier struct {
	runner Runner
}

// New returns a Verifier backed by runner. A nil runner yields a no-op verifier
// (Available reports false).
func New(runner Runner) *Verifier {
	return &Verifier{runner: runner}
}

// Available reports whether any verification backend is configured.
func (v *Verifier) Available() bool {
	return v != nil && v.runner != nil
}

// Backend names the active verification mechanism.
func (v *Verifier) Backend() string {
	if !v.Available() {
		return "none"
	}
	return v.runner.Backend()
}

// Verify dry-runs every Kubernetes document and returns a result per document.
// Non-Kubernetes documents are skipped (nothing to apply via kubectl).
func (v *Verifier) Verify(ctx context.Context, docs []model.Document) []Result {
	if !v.Available() {
		return nil
	}
	var results []Result
	for _, doc := range docs {
		if !doc.IsKubernetes() {
			continue
		}
		res := Result{
			DocIndex: doc.Index,
			Name:     doc.Name(),
			GVK:      doc.GVK.String(),
			OK:       true,
		}
		if err := v.runner.DryRun(ctx, doc); err != nil {
			res.OK = false
			res.Message = err.Error()
		}
		results = append(results, res)
	}
	return results
}

// AllOK reports whether every result passed.
func AllOK(results []Result) bool {
	for _, r := range results {
		if !r.OK {
			return false
		}
	}
	return true
}

// FailureText joins the messages of all failing results, suitable for feeding
// back into the kubectl-error to fix mapping.
func FailureText(results []Result) string {
	var msg string
	for _, r := range results {
		if r.OK {
			continue
		}
		if msg != "" {
			msg += "\n"
		}
		msg += r.Message
	}
	return msg
}

// clusterRunner verifies via a live cluster server-side dry-run.
type clusterRunner struct {
	client *cluster.Client
}

// NewClusterRunner returns a Runner backed by a cluster client, or nil if the
// client is nil.
func NewClusterRunner(client *cluster.Client) Runner {
	if client == nil {
		return nil
	}
	return &clusterRunner{client: client}
}

func (r *clusterRunner) DryRun(ctx context.Context, doc model.Document) error {
	obj := doc.Object.DeepCopy()
	return r.client.DryRunApply(ctx, obj)
}

func (r *clusterRunner) Backend() string { return "cluster dry-run" }

// schemaRunner verifies offline using OpenAPI schema validation.
type schemaRunner struct {
	validator *schema.Validator
}

// NewSchemaRunner returns a Runner backed by a schema validator, or nil if the
// validator is nil.
func NewSchemaRunner(validator *schema.Validator) Runner {
	if validator == nil {
		return nil
	}
	return &schemaRunner{validator: validator}
}

func (r *schemaRunner) DryRun(ctx context.Context, doc model.Document) error {
	findings := r.validator.ValidateDocument(ctx, doc)
	for _, f := range findings {
		if f.Severity == model.SeverityError {
			return fmt.Errorf("%s", f.Message)
		}
	}
	return nil
}

func (r *schemaRunner) Backend() string { return "schema (offline)" }
