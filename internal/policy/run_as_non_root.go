package policy

import (
	"context"
	"fmt"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type RunAsNonRootRule struct{}

func (r *RunAsNonRootRule) ID() string { return "pss-run-as-non-root" }

func (r *RunAsNonRootRule) AppliesTo(u *unstructured.Unstructured) bool {
	return isWorkloadKind(u.GetKind())
}

func (r *RunAsNonRootRule) Check(ctx context.Context, doc model.Document) []model.Finding {
	u := &doc.Object
	var findings []model.Finding
	for _, ref := range podSpecPaths(u) {
		podPath := ref.podSpecPath
		scPath := append(append([]string{}, podPath...), "securityContext")
		sc, found, _ := unstructured.NestedMap(u.Object, scPath...)
		if !found {
			findings = append(findings, r.finding(doc, joinPath(scPath...), "pod securityContext is missing runAsNonRoot: true"))
			continue
		}
		if val, ok := boolField(sc, "runAsNonRoot"); !ok || !val {
			findings = append(findings, r.finding(doc, joinPath(scPath...), "pod securityContext.runAsNonRoot must be true"))
		}
		containers, _ := getContainers(u, podPath)
		for i, c := range containers {
			csc, ok := c["securityContext"].(map[string]interface{})
			name := containerName(c, i)
			path := joinPath(append(append([]string{}, podPath...), "containers", name, "securityContext")...)
			if !ok {
				findings = append(findings, r.finding(doc, path, fmt.Sprintf("container %q is missing securityContext.runAsNonRoot: true", name)))
				continue
			}
			if val, ok := boolField(csc, "runAsNonRoot"); !ok || !val {
				findings = append(findings, r.finding(doc, path, fmt.Sprintf("container %q securityContext.runAsNonRoot must be true", name)))
			}
		}
	}
	return findings
}

func (r *RunAsNonRootRule) finding(doc model.Document, path, message string) model.Finding {
	return model.Finding{
		RuleID:       r.ID(),
		Message:      message,
		Path:         path,
		GVK:          doc.GVK,
		GVKString:    doc.GVK.String(),
		ResourceName: doc.Name(),
		Source:       model.SourcePolicy,
		Severity:     model.SeverityError,
		DocIndex:     doc.Index,
		SourceFile:   doc.Source,
		Fix: &model.Fix{
			RuleID:      r.ID(),
			Description: "Set runAsNonRoot: true on pod and container securityContext",
			Apply:       applyRunAsNonRoot,
		},
	}
}

func applyRunAsNonRoot(u *unstructured.Unstructured) error {
	for _, ref := range podSpecPaths(u) {
		podPath := ref.podSpecPath
		scPath := append(append([]string{}, podPath...), "securityContext")
		if err := setNestedField(u.Object, true, append(scPath, "runAsNonRoot")...); err != nil {
			return err
		}
		containersPath := append(append([]string{}, podPath...), "containers")
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
			csc["runAsNonRoot"] = true
			items[i] = m
		}
		if err := unstructured.SetNestedSlice(u.Object, items, containersPath...); err != nil {
			return err
		}
	}
	return nil
}
