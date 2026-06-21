package policy

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type FloatingTagRule struct{}

func (r *FloatingTagRule) ID() string { return "image-floating-tag" }

func (r *FloatingTagRule) AppliesTo(u *unstructured.Unstructured) bool {
	return isWorkloadKind(u.GetKind())
}

func (r *FloatingTagRule) Check(ctx context.Context, doc model.Document) []model.Finding {
	u := &doc.Object
	var findings []model.Finding
	for _, ref := range podSpecPaths(u) {
		containers, _ := getContainers(u, ref.podSpecPath)
		for i, c := range containers {
			image, _ := c["image"].(string)
			if !hasFloatingTag(image) {
				continue
			}
			name := containerName(c, i)
			path := joinPath(append(append([]string{}, ref.podSpecPath...), "containers", name, "image")...)
			findings = append(findings, model.Finding{
				RuleID:       r.ID(),
				Message:      fmt.Sprintf("container %q uses floating image tag %q; pin to a digest or specific version", name, image),
				Path:         path,
				GVK:          doc.GVK,
				GVKString:    doc.GVK.String(),
				ResourceName: doc.Name(),
				Source:       model.SourcePolicy,
				Severity:     model.SeverityWarning,
				DocIndex:     doc.Index,
				SourceFile:   doc.Source,
			})
		}
	}
	return findings
}

func hasFloatingTag(image string) bool {
	if image == "" {
		return true
	}
	if strings.HasSuffix(image, ":latest") {
		return true
	}
	if !strings.Contains(image, ":") && !strings.Contains(image, "@sha256:") {
		return true
	}
	return false
}
