package reindent

// This file encodes just enough of the Kubernetes object model for the
// reindenter to know which parent a field belongs to. It is deliberately not a
// full schema: it only records, per type, the fields that type declares and
// (for fields that open a nested block) the type of that block.
//
// The reindenter uses this to answer one question: "when I see key X, which of
// the currently-open scopes is its real parent?" That is what lets it pull a
// field like `dnsPolicy` back out to the PodSpec even when the broken
// indentation left it buried inside a container.

// field describes what a key opens.
//
//   - child == "" means the key holds a scalar / flow value (a leaf): it does
//     not open a mapping we need to track.
//   - seq == true means the key holds a sequence; elem names the element type of
//     that sequence (the type of each `- item`).
//   - otherwise child names the mapping type the key opens.
type field struct {
	child string // mapping type opened by this key ("" = leaf/scalar)
	seq   bool   // key holds a sequence
	elem  string // element type for sequence keys
}

// leaf/seq constructors keep the table below terse.
func mapping(t string) field { return field{child: t} }
func seqOf(t string) field   { return field{seq: true, elem: t} }

var leaf = field{}

// stringMap is the sentinel type for free-form key/value mappings (labels,
// annotations, nodeSelector, data, resource limits...). Any key is a valid
// child, so the reindenter never tries to "dedent out" of one on schema
// grounds — it falls back to indentation there, which is correct because the
// keys really are arbitrary.
const stringMap = "StringMap"

// kindType maps a manifest Kind to its root schema type. Kinds not listed fall
// through to a purely structural (indentation-only) reindent.
var kindType = map[string]string{
	"Pod":         "Pod",
	"Deployment":  "Deployment",
	"ReplicaSet":  "Deployment", // same spec shape for our purposes
	"DaemonSet":   "Deployment",
	"StatefulSet": "Deployment",
	"Job":         "Job",
	"CronJob":     "CronJob",
	"Service":     "Service",
	"ConfigMap":   "ConfigMap",
	"Secret":      "ConfigMap",
}

// schemaTable[type][key] = what that key declares.
var schemaTable = map[string]map[string]field{
	"Pod": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("PodSpec"),
		"status":   mapping(stringMap),
	},
	"Deployment": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("DeploymentSpec"),
		"status":   mapping(stringMap),
	},
	"Job": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("JobSpec"),
		"status":   mapping(stringMap),
	},
	"CronJob": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("CronJobSpec"),
		"status":   mapping(stringMap),
	},
	"Service": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("ServiceSpec"),
		"status":   mapping(stringMap),
	},
	"ConfigMap": {
		"apiVersion": leaf, "kind": leaf,
		"metadata":   mapping("ObjectMeta"),
		"data":       mapping(stringMap),
		"binaryData": mapping(stringMap),
		"stringData": mapping(stringMap),
		"immutable":  leaf,
	},

	"ObjectMeta": {
		"name": leaf, "namespace": leaf, "generateName": leaf,
		"labels": mapping(stringMap), "annotations": mapping(stringMap),
	},

	"DeploymentSpec": {
		"replicas": leaf, "minReadySeconds": leaf, "paused": leaf,
		"revisionHistoryLimit": leaf, "progressDeadlineSeconds": leaf,
		"selector": mapping("LabelSelector"),
		"strategy": mapping(stringMap),
		"template": mapping("PodTemplateSpec"),
	},
	"JobSpec": {
		"parallelism": leaf, "completions": leaf, "backoffLimit": leaf,
		"activeDeadlineSeconds": leaf, "ttlSecondsAfterFinished": leaf,
		"selector": mapping("LabelSelector"),
		"template": mapping("PodTemplateSpec"),
	},
	"CronJobSpec": {
		"schedule": leaf, "concurrencyPolicy": leaf, "suspend": leaf,
		"startingDeadlineSeconds": leaf,
		"jobTemplate":             mapping("JobTemplateSpec"),
	},
	"JobTemplateSpec": {
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("JobSpec"),
	},
	"LabelSelector": {
		"matchLabels":      mapping(stringMap),
		"matchExpressions": seqOf(stringMap),
	},
	"PodTemplateSpec": {
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("PodSpec"),
	},

	"PodSpec": {
		"restartPolicy": leaf, "dnsPolicy": leaf, "nodeName": leaf,
		"serviceAccountName": leaf, "serviceAccount": leaf,
		"hostNetwork": leaf, "hostPID": leaf, "hostIPC": leaf,
		"terminationGracePeriodSeconds": leaf, "priorityClassName": leaf,
		"schedulerName": leaf, "automountServiceAccountToken": leaf,
		"nodeSelector":     mapping(stringMap),
		"securityContext":  mapping("PodSecurityContext"),
		"affinity":         mapping(stringMap),
		"containers":       seqOf("Container"),
		"initContainers":   seqOf("Container"),
		"volumes":          seqOf("Volume"),
		"imagePullSecrets": seqOf(stringMap),
		"tolerations":      seqOf(stringMap),
	},
	"Container": {
		"name": leaf, "image": leaf, "imagePullPolicy": leaf,
		"workingDir": leaf, "tty": leaf, "stdin": leaf, "restartPolicy": leaf,
		"command":        seqOf(""),
		"args":           seqOf(""),
		"ports":          seqOf("ContainerPort"),
		"env":            seqOf("EnvVar"),
		"envFrom":        seqOf(stringMap),
		"volumeMounts":   seqOf("VolumeMount"),
		"resources":      mapping("ResourceRequirements"),
		"securityContext": mapping("SecurityContext"),
		"livenessProbe":  mapping("Probe"),
		"readinessProbe": mapping("Probe"),
		"startupProbe":   mapping("Probe"),
		"lifecycle":      mapping(stringMap),
	},
	"ResourceRequirements": {
		"limits":   mapping(stringMap),
		"requests": mapping(stringMap),
	},
	"ContainerPort": {
		"name": leaf, "containerPort": leaf, "hostPort": leaf, "protocol": leaf, "hostIP": leaf,
	},
	"EnvVar": {
		"name": leaf, "value": leaf,
		"valueFrom": mapping(stringMap),
	},
	"VolumeMount": {
		"name": leaf, "mountPath": leaf, "readOnly": leaf, "subPath": leaf, "subPathExpr": leaf,
	},
	"Probe": {
		"initialDelaySeconds": leaf, "periodSeconds": leaf, "timeoutSeconds": leaf,
		"successThreshold": leaf, "failureThreshold": leaf,
		"httpGet":   mapping(stringMap),
		"exec":      mapping(stringMap),
		"tcpSocket": mapping(stringMap),
		"grpc":      mapping(stringMap),
	},
	"SecurityContext": {
		"runAsUser": leaf, "runAsGroup": leaf, "runAsNonRoot": leaf,
		"readOnlyRootFilesystem": leaf, "allowPrivilegeEscalation": leaf,
		"privileged": leaf, "procMount": leaf,
		"capabilities":   mapping(stringMap),
		"seccompProfile": mapping(stringMap),
	},
	"PodSecurityContext": {
		"runAsUser": leaf, "runAsGroup": leaf, "runAsNonRoot": leaf,
		"fsGroup": leaf, "fsGroupChangePolicy": leaf,
		"seccompProfile":     mapping(stringMap),
		"supplementalGroups": seqOf(""),
	},
	"Volume": {
		"name":                  leaf,
		"configMap":             mapping(stringMap),
		"secret":                mapping(stringMap),
		"emptyDir":              mapping(stringMap),
		"hostPath":              mapping(stringMap),
		"persistentVolumeClaim": mapping(stringMap),
		"projected":             mapping(stringMap),
		"downwardAPI":           mapping(stringMap),
	},
	"ServiceSpec": {
		"type": leaf, "clusterIP": leaf, "externalName": leaf,
		"sessionAffinity": leaf, "externalTrafficPolicy": leaf, "loadBalancerIP": leaf,
		"selector": mapping(stringMap),
		"ports":    seqOf("ServicePort"),
	},
	"ServicePort": {
		"name": leaf, "port": leaf, "targetPort": leaf, "protocol": leaf, "nodePort": leaf,
	},
}

// lookup returns the field descriptor for key within type t, and whether t
// declares key at all. A stringMap type declares nothing specifically (any key
// is a free-form scalar child), so lookup reports ok=false for it — callers
// treat stringMap as a wildcard instead.
func lookup(t, key string) (field, bool) {
	if t == "" || t == stringMap {
		return field{}, false
	}
	fields, ok := schemaTable[t]
	if !ok {
		return field{}, false
	}
	f, ok := fields[key]
	return f, ok
}

// declares reports whether type t specifically declares key.
func declares(t, key string) bool {
	_, ok := lookup(t, key)
	return ok
}
