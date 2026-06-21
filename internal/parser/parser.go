package parser

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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

func Parse(source string, data []byte) ([]model.Document, error) {
	chunks := splitDocuments(data)
	var docs []model.Document
	for i, raw := range chunks {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		var obj unstructured.Unstructured
		if err := yaml.Unmarshal(raw, &obj.Object); err != nil {
			return nil, fmt.Errorf("document %d in %s: %w", i+1, source, err)
		}
		gvk := extractGVK(&obj)
		docs = append(docs, model.Document{
			Index:   len(docs),
			GVK:     gvk,
			Object:  obj,
			RawYAML: append([]byte(nil), raw...),
			Source:  source,
		})
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
	parts := bytes.Split(data, []byte("\n---"))
	if len(parts) == 1 {
		parts = bytes.Split(data, []byte("---"))
	}
	var chunks [][]byte
	for _, part := range parts {
		part = bytes.TrimSpace(part)
		if len(part) == 0 {
			continue
		}
		chunks = append(chunks, part)
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
