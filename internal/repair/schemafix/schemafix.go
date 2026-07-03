package schemafix

import (
	"regexp"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	additionalPropertyRE = regexp.MustCompile(`(?i)additional properties? ['"]([^'"]+)['"]`)
	unknownFieldRE       = regexp.MustCompile(`(?i)unknown field ['"]([^'"]+)['"]`)
	missingRequiredRE    = regexp.MustCompile(`(?i)(?:missing|required).*(?:property|field) ['"]([^'"]+)['"]`)
	wrongTypeRE          = regexp.MustCompile(`(?i)Expected:\s*(\w+).*Given:\s*(\w+)`)
	// notDeclaredRE matches the server-side-apply wording, e.g.
	// ".spec.notAField: field not declared in schema".
	notDeclaredRE = regexp.MustCompile(`([A-Za-z0-9_.]+):\s*field not declared in schema`)
)

// UnknownFieldPaths extracts the dotted field paths of every unknown field
// reported anywhere in errorText, across both kubeconform and server-side-apply
// wordings. Paths containing list indices (which RemoveNestedField cannot
// address) are skipped.
func UnknownFieldPaths(errorText string) []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.Trim(p, ".")
		if p == "" || strings.ContainsAny(p, "[]") || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	for _, m := range notDeclaredRE.FindAllStringSubmatch(errorText, -1) {
		add(m[1])
	}
	for _, m := range unknownFieldRE.FindAllStringSubmatch(errorText, -1) {
		add(m[1])
	}
	for _, m := range additionalPropertyRE.FindAllStringSubmatch(errorText, -1) {
		add(m[1])
	}
	return paths
}

func EnrichFinding(doc model.Document, f model.Finding) model.Finding {
	f.Category = model.CategoryStructure
	if f.RepairConfidence == "" {
		f.RepairConfidence = model.RepairCertain
	}
	if f.Fix == nil {
		f.Fix = FixForFinding(doc, f)
	}
	return f
}

func ClassifyMessage(message string) string {
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "additional propert"), strings.Contains(lower, "unknown field"):
		return "schema-unknown-field"
	case strings.Contains(lower, "required"), strings.Contains(lower, "missing"):
		return "schema-missing-required"
	case strings.Contains(lower, "invalid type"):
		return "schema-wrong-type"
	default:
		return "schema-validation"
	}
}

func FixForFinding(doc model.Document, f model.Finding) *model.Fix {
	switch f.RuleID {
	case "schema-unknown-field":
		path := fieldPathFromMessage(f.Message, f.Path)
		if path == nil {
			return nil
		}
		return &model.Fix{
			RuleID:      f.RuleID,
			Description: "Remove unknown field at " + strings.Join(path, "."),
			Apply: func(u *unstructured.Unstructured) error {
				unstructured.RemoveNestedField(u.Object, path...)
				return nil
			},
		}
	case "schema-missing-required":
		path, value := requiredFieldDefault(doc, f.Message, f.Path)
		if path == nil {
			return nil
		}
		pathCopy := append([]string(nil), path...)
		return &model.Fix{
			RuleID:      f.RuleID,
			Description: "Add missing required field at " + strings.Join(pathCopy, "."),
			Apply: func(u *unstructured.Unstructured) error {
				return unstructured.SetNestedField(u.Object, value, pathCopy...)
			},
		}
	case "schema-wrong-type":
		path, value := coerceType(f.Message, f.Path)
		if path == nil {
			return nil
		}
		pathCopy := append([]string(nil), path...)
		return &model.Fix{
			RuleID:      f.RuleID,
			Description: "Coerce field type at " + strings.Join(pathCopy, "."),
			Apply: func(u *unstructured.Unstructured) error {
				return unstructured.SetNestedField(u.Object, value, pathCopy...)
			},
		}
	}
	return nil
}

func fieldPathFromMessage(message, path string) []string {
	if path != "" {
		return strings.Split(path, ".")
	}
	if m := unknownFieldRE.FindStringSubmatch(message); len(m) == 2 {
		return []string{m[1]}
	}
	if m := additionalPropertyRE.FindStringSubmatch(message); len(m) == 2 {
		return []string{m[1]}
	}
	return nil
}

func requiredFieldDefault(doc model.Document, message, path string) ([]string, interface{}) {
	if path != "" {
		parts := strings.Split(path, ".")
		field := parts[len(parts)-1]
		return parts, defaultForField(doc, field)
	}
	if m := missingRequiredRE.FindStringSubmatch(message); len(m) == 2 {
		return []string{m[1]}, defaultForField(doc, m[1])
	}
	if strings.Contains(strings.ToLower(message), "restartpolicy") {
		return []string{"spec", "restartPolicy"}, "Always"
	}
	return nil, nil
}

func defaultForField(doc model.Document, field string) interface{} {
	switch field {
	case "restartPolicy":
		return "Always"
	case "replicas":
		return int64(1)
	case "type":
		return "ClusterIP"
	case "ports":
		return []interface{}{}
	default:
		return ""
	}
}

func coerceType(message, path string) ([]string, interface{}) {
	if path == "" {
		return nil, nil
	}
	m := wrongTypeRE.FindStringSubmatch(message)
	if len(m) != 3 {
		return nil, nil
	}
	parts := strings.Split(path, ".")
	switch strings.ToLower(m[1]) {
	case "integer", "number":
		return parts, int64(1)
	case "boolean":
		return parts, false
	case "array":
		return parts, []interface{}{}
	case "object":
		return parts, map[string]interface{}{}
	case "string":
		return parts, ""
	}
	return nil, nil
}
