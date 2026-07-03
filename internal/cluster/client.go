package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackalgg/cairn/internal/model"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
)

type Info struct {
	ServerVersion string
	PSSNamespaces map[string]string
}

type Client struct {
	info      Info
	discovery discovery.DiscoveryInterface
	dynamic   dynamic.Interface
	core      kubernetes.Interface
	mapper    meta.RESTMapper
}

func NewClient(kubeconfig, contextName string) (*Client, error) {
	config, err := loadConfig(kubeconfig, contextName)
	if err != nil {
		return nil, err
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	version, err := discoveryClient.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("cluster client: %w", err)
	}
	c := &Client{
		info: Info{
			ServerVersion: version.GitVersion,
			PSSNamespaces: map[string]string{},
		},
		discovery: discoveryClient,
		dynamic:   dynamicClient,
		core:      coreClient,
	}
	if err := c.loadPSSNamespaces(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) Info() Info {
	return c.info
}

func (c *Client) ServerVersion() string {
	return c.info.ServerVersion
}

func (c *Client) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

func (c *Client) Dynamic() dynamic.Interface {
	return c.dynamic
}

func (c *Client) Core() kubernetes.Interface {
	return c.core
}

func (c *Client) loadPSSNamespaces(ctx context.Context) error {
	nsList, err := c.core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ns := range nsList.Items {
		if label, ok := ns.Labels["pod-security.kubernetes.io/enforce"]; ok {
			c.info.PSSNamespaces[ns.Name] = label
		}
	}
	return nil
}

func (c *Client) PSSForNamespace(namespace string) string {
	if c == nil {
		return ""
	}
	return c.info.PSSNamespaces[namespace]
}

// restMapper lazily builds (and caches) a discovery-backed RESTMapper so any
// GroupVersionKind, including CRDs, can be resolved to its resource and scope.
func (c *Client) restMapper() (meta.RESTMapper, error) {
	if c.mapper != nil {
		return c.mapper, nil
	}
	groupResources, err := restmapper.GetAPIGroupResources(c.discovery)
	if err != nil {
		return nil, err
	}
	c.mapper = restmapper.NewDiscoveryRESTMapper(groupResources)
	return c.mapper, nil
}

// DryRunApply performs a server-side apply in dry-run mode (dryRun=All), the
// same admission/schema/defaulting path that `kubectl apply --dry-run=server`
// exercises. A nil error means the API server would accept the object; a non-nil
// error carries the rejection reason (admission, schema, immutability, etc.).
func (c *Client) DryRunApply(ctx context.Context, obj *unstructured.Unstructured) error {
	if c == nil {
		return fmt.Errorf("no cluster client")
	}
	if obj.GetName() == "" {
		return fmt.Errorf("metadata.name is required to apply")
	}
	gvk := obj.GroupVersionKind()
	if gvk.Kind == "" {
		return fmt.Errorf("object has no kind")
	}

	mapper, err := c.restMapper()
	if err != nil {
		return fmt.Errorf("build REST mapper: %w", err)
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", gvk.String(), err)
	}

	ri := c.dynamic.Resource(mapping.Resource)
	var dr dynamic.ResourceInterface = ri
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns := obj.GetNamespace()
		if ns == "" {
			ns = "default"
		}
		dr = ri.Namespace(ns)
	}

	_, err = dr.Apply(ctx, obj.GetName(), obj, metav1.ApplyOptions{
		FieldManager: "cairn",
		DryRun:       []string{metav1.DryRunAll},
		Force:        true,
	})
	return err
}

func (c *Client) EnrichFindings(ctx context.Context, findings []model.Finding) []model.Finding {
	if c == nil {
		return findings
	}
	for i := range findings {
		if findings[i].Severity == model.SeverityWarning && findings[i].Source == model.SourcePolicy {
			if findings[i].RuleID == "pss-run-as-non-root" || findings[i].RuleID == "pss-read-only-rootfs" {
				findings[i].Message = fmt.Sprintf("%s (cluster %s may enforce Pod Security Standards)", findings[i].Message, c.info.ServerVersion)
			}
		}
	}
	return findings
}

func FormatServerVersion(version string) string {
	return strings.TrimPrefix(version, "v")
}

// GVRForKind returns a GroupVersionResource for common Kubernetes kinds.
func GVRForKind(kind string) (schema.GroupVersionResource, bool) {
	gvr, ok := kindToGVR[kind]
	return gvr, ok
}

var kindToGVR = map[string]schema.GroupVersionResource{
	"Pod":                   {Group: "", Version: "v1", Resource: "pods"},
	"Service":               {Group: "", Version: "v1", Resource: "services"},
	"ConfigMap":             {Group: "", Version: "v1", Resource: "configmaps"},
	"Secret":                {Group: "", Version: "v1", Resource: "secrets"},
	"Namespace":             {Group: "", Version: "v1", Resource: "namespaces"},
	"Deployment":            {Group: "apps", Version: "v1", Resource: "deployments"},
	"StatefulSet":           {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"DaemonSet":             {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"ReplicaSet":            {Group: "apps", Version: "v1", Resource: "replicasets"},
	"Job":                   {Group: "batch", Version: "v1", Resource: "jobs"},
	"CronJob":               {Group: "batch", Version: "v1", Resource: "cronjobs"},
	"Ingress":               {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	"NetworkPolicy":         {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
	"PersistentVolumeClaim": {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
}
