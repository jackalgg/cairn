package generator

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackalgg/cairn/internal/cluster"
	"github.com/jackalgg/cairn/internal/engine"
	"github.com/jackalgg/cairn/internal/model"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type Result struct {
	Documents []model.Document
	Findings  []model.Finding
	Written   []string
}

func Generate(ctx context.Context, client *cluster.Client, eng *engine.Engine, opts Options) (*Result, error) {
	if client == nil {
		return nil, fmt.Errorf("cluster client is required")
	}
	if len(opts.Kinds) == 0 {
		opts.Kinds = []string{"Deployment", "Service", "ConfigMap"}
	}

	docs, err := Export(ctx, client, opts)
	if err != nil {
		return nil, err
	}
	for i := range docs {
		Clean(&docs[i])
		if opts.Harden {
			if err := ApplyDefaults(&docs[i]); err != nil {
				return nil, err
			}
		}
	}

	scan, err := eng.ScanDocuments(ctx, docs)
	if err != nil {
		return nil, err
	}

	if opts.Harden {
		fixable := engine.FilterFixable(scan.Findings)
		working := make(map[string]model.Document, len(scan.Documents))
		for _, d := range scan.Documents {
			working[d.Key()] = d
		}
		for _, f := range fixable {
			key := model.DocumentKey(f.SourceFile, f.DocIndex)
			doc, ok := working[key]
			if !ok {
				continue
			}
			if err := engine.ApplyFixToDocument(&doc, f.Fix); err != nil {
				return nil, err
			}
			working[key] = doc
		}
		docs := make([]model.Document, 0, len(working))
		for _, d := range working {
			docs = append(docs, d)
		}
		scan, err = eng.ScanDocuments(ctx, docs)
		if err != nil {
			return nil, err
		}
	}

	if engine.HasErrors(scan.Findings) {
		return scanResultWithError(scan)
	}

	written, err := writeDocuments(scan.Documents, opts)
	if err != nil {
		return nil, err
	}

	return &Result{
		Documents: scan.Documents,
		Findings:  scan.Findings,
		Written:   written,
	}, nil
}

func scanResultWithError(scan *model.ScanResult) (*Result, error) {
	var msgs []string
	for _, f := range scan.Findings {
		if f.Severity == model.SeverityError {
			msgs = append(msgs, fmt.Sprintf("%s: %s", f.RuleID, f.Message))
		}
	}
	return &Result{Documents: scan.Documents, Findings: scan.Findings}, fmt.Errorf("generated manifests failed validation: %s", strings.Join(msgs, "; "))
}

func writeDocuments(docs []model.Document, opts Options) ([]string, error) {
	if opts.DryRun || opts.OutDir == "" {
		return nil, nil
	}
	var written []string
	for _, doc := range docs {
		data, err := yaml.Marshal(doc.Object.Object)
		if err != nil {
			return nil, err
		}
		name := filenameFor(doc)
		path := filepath.Join(opts.OutDir, name)
		if err := fixerWrite(path, data); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

func fixerWrite(path string, data []byte) error {
	return writeFileAtomic(path, append(data, '\n'))
}

func filenameFor(doc model.Document) string {
	ns := doc.Object.GetNamespace()
	kind := strings.ToLower(doc.GVK.Kind)
	name := doc.Name()
	if ns != "" {
		return fmt.Sprintf("%s-%s-%s.yaml", kind, ns, name)
	}
	return fmt.Sprintf("%s-%s.yaml", kind, name)
}

func Export(ctx context.Context, client *cluster.Client, opts Options) ([]model.Document, error) {
	var docs []model.Document
	index := 0
	for _, kind := range opts.Kinds {
		gvr, ok := cluster.GVRForKind(kind)
		if !ok {
			return nil, fmt.Errorf("unsupported kind for export: %s", kind)
		}
		list, err := listResource(ctx, client, gvr, kind, opts)
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", kind, err)
		}
		for _, item := range list.Items {
			if opts.Name != "" && item.GetName() != opts.Name {
				continue
			}
			u := item
			raw, err := yaml.Marshal(u.Object)
			if err != nil {
				return nil, err
			}
			gvk := schema.FromAPIVersionAndKind(u.GetAPIVersion(), u.GetKind())
			docs = append(docs, model.Document{
				Index:   index,
				GVK:     gvk,
				Object:  unstructured.Unstructured{Object: u.Object},
				RawYAML: raw,
				Source:  filenameFor(model.Document{GVK: gvk, Object: unstructured.Unstructured{Object: u.Object}}),
			})
			index++
		}
	}
	return docs, nil
}

func listResource(ctx context.Context, client *cluster.Client, gvr schema.GroupVersionResource, kind string, opts Options) (*unstructured.UnstructuredList, error) {
	listOpts := metav1.ListOptions{LabelSelector: opts.Selector}
	if !isNamespacedKind(kind) {
		return client.Dynamic().Resource(gvr).List(ctx, listOpts)
	}
	ns := opts.Namespace
	if ns == "" {
		ns = "default"
	}
	return client.Dynamic().Resource(gvr).Namespace(ns).List(ctx, listOpts)
}

func isNamespacedKind(kind string) bool {
	switch kind {
	case "Namespace", "PersistentVolume", "ClusterRole", "ClusterRoleBinding", "StorageClass":
		return false
	default:
		return true
	}
}
