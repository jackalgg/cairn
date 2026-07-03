package parser

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	yamlv3 "go.yaml.in/yaml/v3"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func ParseFile(path string) ([]model.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

func ReadPath(path string) (string, []byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		return "<stdin>", data, err
	}
	data, err := os.ReadFile(path)
	return path, data, err
}

func Parse(source string, data []byte) ([]model.Document, error) {
	chunks := splitDocuments(data)
	var docs []model.Document
	for i, raw := range chunks {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		// First validate the chunk as arbitrary YAML so that syntax errors
		// surface (and can trigger repair) while non-mapping documents
		// (top-level lists, scalars, plain configs) are still accepted.
		var generic interface{}
		if err := yamlv3.Unmarshal(raw, &generic); err != nil {
			return nil, fmt.Errorf("document %d in %s: %w", i+1, source, err)
		}
		doc := model.Document{
			Index:   len(docs),
			RawYAML: append([]byte(nil), raw...),
			Source:  source,
		}
		// Only mappings can be Kubernetes resources. Use the JSON-based
		// unmarshal so numbers and nested types match what the schema
		// validator expects.
		if _, ok := generic.(map[string]interface{}); ok {
			var obj unstructured.Unstructured
			if err := yaml.Unmarshal(raw, &obj.Object); err == nil {
				doc.Object = obj
				doc.GVK = extractGVK(&obj)
			}
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func ParseReader(source string, r io.Reader) ([]model.Document, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Parse(source, data)
}

func ParsePath(path string) ([]model.Document, error) {
	if path == "-" {
		return ParseReader("<stdin>", os.Stdin)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return parseDirectory(path)
	}
	return ParseFile(path)
}

func parseDirectory(dir string) ([]model.Document, error) {
	var docs []model.Document
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isYAMLFile(path) {
			return nil
		}
		parsed, err := ParseFile(path)
		if err != nil {
			return err
		}
		docs = append(docs, parsed...)
		return nil
	})
	return docs, err
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func splitDocuments(data []byte) [][]byte {
	lines := bytes.Split(data, []byte("\n"))
	var chunks [][]byte
	var current []byte
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Equal(trimmed, []byte("---")) && len(current) > 0 {
			chunks = append(chunks, bytes.TrimSpace(current))
			current = nil
			continue
		}
		if len(current) > 0 {
			current = append(current, '\n')
		}
		current = append(current, line...)
	}
	if len(bytes.TrimSpace(current)) > 0 {
		chunks = append(chunks, bytes.TrimSpace(current))
	}
	if len(chunks) == 0 && len(bytes.TrimSpace(data)) > 0 {
		return [][]byte{bytes.TrimSpace(data)}
	}
	return chunks
}

func extractGVK(u *unstructured.Unstructured) schema.GroupVersionKind {
	apiVersion := u.GetAPIVersion()
	kind := u.GetKind()
	if apiVersion == "" || kind == "" {
		if v, ok := u.Object["apiVersion"].(string); ok {
			apiVersion = v
		}
		if v, ok := u.Object["kind"].(string); ok {
			kind = v
		}
	}
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return schema.GroupVersionKind{Kind: kind}
	}
	return gv.WithKind(kind)
}
