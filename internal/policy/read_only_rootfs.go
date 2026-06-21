package policy

import (
	"context"
	"fmt"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ReadOnlyRootFSRule struct{}

func (r *ReadOnlyRootFSRule) ID() string { return "pss-read-only-rootfs" }

func (r *ReadOnlyRootFSRule) AppliesTo(u *unstructured.Unstructured) bool {
	return isWorkloadKind(u.GetKind())
}

func (r *ReadOnlyRootFSRule) Check(ctx context.Context, doc model.Document) []model.Finding {
	u := &doc.Object
	var findings []model.Finding
	for _, ref := range podSpecPaths(u) {
		containers, _ := getContainers(u, ref.podSpecPath)
		for i, c := range containers {
			name := containerName(c, i)
			path := joinPath(append(append([]string{}, ref.podSpecPath...), "containers", name, "securityContext")...)
			csc, ok := c["securityContext"].(map[string]interface{})
			if !ok {
				findings = append(findings, r.finding(doc, path, fmt.Sprintf("container %q is missing securityContext.readOnlyRootFilesystem: true", name)))
				continue
			}
			if val, ok := boolField(csc, "readOnlyRootFilesystem"); !ok || !val {
				findings = append(findings, r.finding(doc, path, fmt.Sprintf("container %q securityContext.readOnlyRootFilesystem must be true", name)))
			}
		}
	}
	return findings
}

func (r *ReadOnlyRootFSRule) finding(doc model.Document, path, message string) model.Finding {
	return model.Finding{
		RuleID:       r.ID(),
		Message:      message,
		Path:         path,
		GVK:          doc.GVK,
		GVKString:    doc.GVK.String(),
		ResourceName: doc.Name(),
		Source:       model.SourcePolicy,
		Severity:     model.SeverityWarning,
		DocIndex:     doc.Index,
		SourceFile:   doc.Source,
		Fix: &model.Fix{
			RuleID:      r.ID(),
			Description: "Set readOnlyRootFilesystem: true on container securityContext",
			Apply:       applyReadOnlyRootFS,
		},
	}
}

func applyReadOnlyRootFS(u *unstructured.Unstructured) error {
	for _, ref := range podSpecPaths(u) {
		containersPath := append(append([]string{}, ref.podSpecPath...), "containers")
		items, found, err := unstructured.NestedSlice(u.Object, containersPath...)
		if err != nil || !found {
			continue
		}
		for i, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			csc, ok := m["securityContext"].(map[string]interface{})
			if !ok {
				csc = map[string]interface{}{}
				m["securityContext"] = csc
			}
			csc["readOnlyRootFilesystem"] = true
			items[i] = m
		}
		if err := unstructured.SetNestedSlice(u.Object, items, containersPath...); err != nil {
			return err
		}
	}
	return nil
}
