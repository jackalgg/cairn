package compat

import (
	"fmt"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type migration struct {
	from schema.GroupVersionKind
	to   schema.GroupVersionKind
}

var migrations = []migration{
	{
		from: schema.FromAPIVersionAndKind("extensions/v1beta1", "Deployment"),
		to:   schema.FromAPIVersionAndKind("apps/v1", "Deployment"),
	},
	{
		from: schema.FromAPIVersionAndKind("apps/v1beta1", "Deployment"),
		to:   schema.FromAPIVersionAndKind("apps/v1", "Deployment"),
	},
	{
		from: schema.FromAPIVersionAndKind("extensions/v1beta1", "DaemonSet"),
		to:   schema.FromAPIVersionAndKind("apps/v1", "DaemonSet"),
	},
	{
		from: schema.FromAPIVersionAndKind("apps/v1beta2", "DaemonSet"),
		to:   schema.FromAPIVersionAndKind("apps/v1", "DaemonSet"),
	},
	{
		from: schema.FromAPIVersionAndKind("extensions/v1beta1", "Ingress"),
		to:   schema.FromAPIVersionAndKind("networking.k8s.io/v1", "Ingress"),
	},
	{
		from: schema.FromAPIVersionAndKind("networking.k8s.io/v1beta1", "Ingress"),
		to:   schema.FromAPIVersionAndKind("networking.k8s.io/v1", "Ingress"),
	},
}

func CheckDocument(doc model.Document, targetVersion string) []model.Finding {
	gvk := doc.GVK
	var findings []model.Finding
	for _, m := range migrations {
		if gvk != m.from {
			continue
		}
		findings = append(findings, model.Finding{
			RuleID:       "api-version-deprecated",
			Message:      fmt.Sprintf("%s/%s uses deprecated apiVersion %s; migrate to %s for target %s", gvk.Kind, doc.Name(), m.from.GroupVersion().String(), m.to.GroupVersion().String(), targetVersion),
			Path:         "apiVersion",
			GVK:          gvk,
			GVKString:    gvk.String(),
			ResourceName: doc.Name(),
			Source:       model.SourceCompat,
			Severity:     model.SeverityWarning,
			DocIndex:     doc.Index,
			SourceFile:   doc.Source,
			Fix: &model.Fix{
				RuleID:      "api-version-deprecated",
				Description: fmt.Sprintf("Migrate apiVersion from %s to %s", m.from.GroupVersion(), m.to.GroupVersion()),
				Apply:       migrateFunc(m.to),
			},
		})
	}
	return findings
}

func migrateFunc(to schema.GroupVersionKind) func(*unstructured.Unstructured) error {
	return func(u *unstructured.Unstructured) error {
		u.SetAPIVersion(to.GroupVersion().String())
		u.SetKind(to.Kind)
		if u.Object != nil {
			u.Object["apiVersion"] = to.GroupVersion().String()
			u.Object["kind"] = to.Kind
		}
		return nil
	}
}

func ApplyMigrations(u *unstructured.Unstructured) bool {
	gvk := schema.FromAPIVersionAndKind(u.GetAPIVersion(), u.GetKind())
	for _, m := range migrations {
		if gvk == m.from {
			_ = migrateFunc(m.to)(u)
			return true
		}
	}
	return false
}
