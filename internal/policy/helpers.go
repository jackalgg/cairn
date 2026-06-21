package policy

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var workloadKinds = map[string]struct{}{
	"Deployment":  {},
	"StatefulSet": {},
	"DaemonSet":   {},
	"ReplicaSet":  {},
	"Job":         {},
	"CronJob":     {},
	"Pod":         {},
}

func isWorkloadKind(kind string) bool {
	_, ok := workloadKinds[kind]
	return ok
}

type podSpecRef struct {
	podSpecPath []string
}

func podSpecPaths(u *unstructured.Unstructured) []podSpecRef {
	kind := u.GetKind()
	switch kind {
	case "Pod":
		return []podSpecRef{{podSpecPath: []string{"spec"}}}
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
		return []podSpecRef{{podSpecPath: []string{"spec", "template", "spec"}}}
	case "CronJob":
		return []podSpecRef{{podSpecPath: []string{"spec", "jobTemplate", "spec", "template", "spec"}}}
	default:
		return nil
	}
}

func getContainers(u *unstructured.Unstructured, podSpecPath []string) ([]map[string]interface{}, []string) {
	path := append(append([]string{}, podSpecPath...), "containers")
	items, found, _ := unstructured.NestedSlice(u.Object, path...)
	if !found {
		return nil, path
	}
	var containers []map[string]interface{}
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		containers = append(containers, m)
		_ = i
	}
	return containers, path
}

func containerName(c map[string]interface{}, index int) string {
	if name, ok := c["name"].(string); ok && name != "" {
		return name
	}
	return fmt.Sprintf("container-%d", index)
}

func boolField(m map[string]interface{}, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func ensureNestedMap(obj map[string]interface{}, fields ...string) (map[string]interface{}, error) {
	if len(fields) == 0 {
		return obj, nil
	}
	cur := obj
	for i, f := range fields {
		v, ok := cur[f]
		if !ok {
			v = map[string]interface{}{}
			cur[f] = v
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
			cur[f] = v
		}
		if i == len(fields)-1 {
			return m, nil
		}
		cur = m
	}
	return cur, nil
}

func setNestedField(obj map[string]interface{}, value interface{}, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	cur := obj
	for i, f := range fields {
		if i == len(fields)-1 {
			cur[f] = value
			return nil
		}
		v, ok := cur[f]
		if !ok {
			v = map[string]interface{}{}
			cur[f] = v
		}
		m, ok := v.(map[string]interface{})
		if !ok {
			m = map[string]interface{}{}
			cur[f] = v
		}
		cur = m
	}
	return nil
}

func joinPath(parts ...string) string {
	return strings.Join(parts, ".")
}
