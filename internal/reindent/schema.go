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
//
// HOUSE RULE: stringMap is ONLY for scopes whose keys are genuinely
// user-chosen. A scope with real, known fields must get a real type, even a
// partial one — a stringMap placeholder there lets placeKey's
// "ancestor-declares beats wildcard" rule steal nested keys (an exec.command
// dedented out to the container because Container also declares `command`;
// same for configMapKeyRef.name vs EnvVar's `name`). That bug class corrupts
// VALID files, so placeholders are never acceptable for typed scopes.
const stringMap = "StringMap"

// kindType maps a manifest Kind to its root schema type. Kinds not listed fall
// through to a purely structural (indentation-only) reindent.
var kindType = map[string]string{
	"Pod":                   "Pod",
	"Deployment":            "Deployment",
	"ReplicaSet":            "Deployment", // same spec shape for our purposes
	"DaemonSet":             "Deployment",
	"StatefulSet":           "StatefulSet", // NOT Deployment: serviceName, volumeClaimTemplates, updateStrategy
	"Job":                   "Job",
	"CronJob":               "CronJob",
	"Service":               "Service",
	"ConfigMap":             "ConfigMap",
	"Secret":                "ConfigMap",
	"PersistentVolumeClaim": "PersistentVolumeClaim",
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

	// ObjectMeta is COMPLETE (every real metadata field is listed) — that is
	// what lets the typo pass warn on unknown fields there (completeTypes in
	// typos.go). If upstream Kubernetes ever adds a field, add it here too.
	"ObjectMeta": {
		"name": leaf, "namespace": leaf, "generateName": leaf,
		"uid": leaf, "resourceVersion": leaf, "generation": leaf, "selfLink": leaf,
		"creationTimestamp": leaf, "deletionTimestamp": leaf,
		"deletionGracePeriodSeconds": leaf,
		"clusterName":                leaf, // removed in k8s 1.25, but old manifests carry it
		"labels":                     mapping(stringMap),
		"annotations":                mapping(stringMap),
		"ownerReferences":            seqOf(stringMap),
		"managedFields":              seqOf(stringMap),
		"finalizers":                 seqOf(""),
	},

	"StatefulSet": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("StatefulSetSpec"),
		"status":   mapping(stringMap),
	},
	"StatefulSetSpec": {
		"replicas": leaf, "serviceName": leaf, "podManagementPolicy": leaf,
		"revisionHistoryLimit": leaf, "minReadySeconds": leaf,
		"selector":                             mapping("LabelSelector"),
		"template":                             mapping("PodTemplateSpec"),
		"updateStrategy":                       mapping("UpdateStrategy"),
		"volumeClaimTemplates":                 seqOf("PersistentVolumeClaim"),
		"persistentVolumeClaimRetentionPolicy": mapping("PVCRetentionPolicy"),
	},
	// UpdateStrategy merges StatefulSet/DaemonSet rollingUpdate shapes
	// (partition/maxUnavailable) — loose but steal-proof.
	"UpdateStrategy": {
		"type":          leaf,
		"rollingUpdate": mapping("RollingUpdate"),
	},
	"PVCRetentionPolicy": {
		"whenDeleted": leaf, "whenScaled": leaf,
	},
	"PersistentVolumeClaim": {
		"apiVersion": leaf, "kind": leaf,
		"metadata": mapping("ObjectMeta"),
		"spec":     mapping("PVCSpec"),
		"status":   mapping(stringMap),
	},
	"PVCSpec": {
		"storageClassName": leaf, "volumeName": leaf, "volumeMode": leaf,
		"volumeAttributesClassName": leaf,
		"accessModes":               seqOf(""),
		"resources":                 mapping("ResourceRequirements"),
		"selector":                  mapping("LabelSelector"),
		"dataSource":                mapping(stringMap),
		"dataSourceRef":             mapping(stringMap),
	},
	"DeploymentSpec": {
		"replicas": leaf, "minReadySeconds": leaf, "paused": leaf,
		"revisionHistoryLimit": leaf, "progressDeadlineSeconds": leaf,
		"selector": mapping("LabelSelector"),
		"strategy": mapping("DeploymentStrategy"),
		"template": mapping("PodTemplateSpec"),
	},
	"DeploymentStrategy": {
		"type":          leaf,
		"rollingUpdate": mapping("RollingUpdate"),
	},
	"RollingUpdate": {
		"maxSurge": leaf, "maxUnavailable": leaf,
		"partition": leaf, // StatefulSet variant
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
		"command":         seqOf(""),
		"args":            seqOf(""),
		"ports":           seqOf("ContainerPort"),
		"env":             seqOf("EnvVar"),
		"envFrom":         seqOf("EnvFromSource"),
		"volumeMounts":    seqOf("VolumeMount"),
		"resources":       mapping("ResourceRequirements"),
		"securityContext": mapping("SecurityContext"),
		"livenessProbe":   mapping("Probe"),
		"readinessProbe":  mapping("Probe"),
		"startupProbe":    mapping("Probe"),
		"lifecycle":       mapping("Lifecycle"),
	},
	"Lifecycle": {
		"postStart":  mapping("LifecycleHandler"),
		"preStop":    mapping("LifecycleHandler"),
		"stopSignal": leaf, // added in k8s 1.33
	},
	"LifecycleHandler": {
		"exec":      mapping("ExecAction"),
		"httpGet":   mapping("HTTPGetAction"),
		"tcpSocket": mapping("TCPSocketAction"),
		"sleep":     mapping("SleepAction"),
	},
	"ExecAction": {
		"command": seqOf(""),
	},
	"HTTPGetAction": {
		"path": leaf, "port": leaf, "host": leaf, "scheme": leaf,
		"httpHeaders": seqOf("HTTPHeader"),
	},
	"HTTPHeader": {
		"name": leaf, "value": leaf,
	},
	"TCPSocketAction": {
		"port": leaf, "host": leaf,
	},
	"GRPCAction": {
		"port": leaf, "service": leaf,
	},
	"SleepAction": {
		"seconds": leaf,
	},
	"EnvFromSource": {
		"prefix":       leaf,
		"configMapRef": mapping("LocalObjectReference"),
		"secretRef":    mapping("LocalObjectReference"),
	},
	"LocalObjectReference": {
		"name": leaf, "optional": leaf,
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
		"valueFrom": mapping("EnvVarSource"),
	},
	"EnvVarSource": {
		"fieldRef":         mapping("ObjectFieldSelector"),
		"resourceFieldRef": mapping("ResourceFieldSelector"),
		"configMapKeyRef":  mapping("KeySelector"),
		"secretKeyRef":     mapping("KeySelector"),
	},
	"ObjectFieldSelector": {
		"apiVersion": leaf, "fieldPath": leaf,
	},
	"ResourceFieldSelector": {
		"containerName": leaf, "resource": leaf, "divisor": leaf,
	},
	"KeySelector": {
		"name": leaf, "key": leaf, "optional": leaf,
	},
	"VolumeMount": {
		"name": leaf, "mountPath": leaf, "readOnly": leaf, "subPath": leaf, "subPathExpr": leaf,
	},
	"Probe": {
		"initialDelaySeconds": leaf, "periodSeconds": leaf, "timeoutSeconds": leaf,
		"successThreshold": leaf, "failureThreshold": leaf,
		"terminationGracePeriodSeconds": leaf,
		"httpGet":                       mapping("HTTPGetAction"),
		"exec":                          mapping("ExecAction"),
		"tcpSocket":                     mapping("TCPSocketAction"),
		"grpc":                          mapping("GRPCAction"),
	},
	"SecurityContext": {
		"runAsUser": leaf, "runAsGroup": leaf, "runAsNonRoot": leaf,
		"readOnlyRootFilesystem": leaf, "allowPrivilegeEscalation": leaf,
		"privileged": leaf, "procMount": leaf,
		"capabilities":   mapping("Capabilities"),
		"seccompProfile": mapping("SeccompProfile"),
	},
	"Capabilities": {
		"add": seqOf(""), "drop": seqOf(""),
	},
	"SeccompProfile": {
		"type": leaf, "localhostProfile": leaf,
	},
	"PodSecurityContext": {
		"runAsUser": leaf, "runAsGroup": leaf, "runAsNonRoot": leaf,
		"fsGroup": leaf, "fsGroupChangePolicy": leaf,
		"seccompProfile":     mapping("SeccompProfile"),
		"supplementalGroups": seqOf(""),
	},
	"Volume": {
		"name":                  leaf,
		"configMap":             mapping("ConfigMapVolumeSource"),
		"secret":                mapping("SecretVolumeSource"),
		"emptyDir":              mapping("EmptyDirVolumeSource"),
		"hostPath":              mapping("HostPathVolumeSource"),
		"persistentVolumeClaim": mapping("PVCVolumeSource"),
		"projected":             mapping("ProjectedVolumeSource"),
		"downwardAPI":           mapping("DownwardAPIVolumeSource"),
	},
	"ConfigMapVolumeSource": {
		"name": leaf, "defaultMode": leaf, "optional": leaf,
		"items": seqOf("KeyToPath"),
	},
	"SecretVolumeSource": {
		"secretName": leaf, "defaultMode": leaf, "optional": leaf,
		"items": seqOf("KeyToPath"),
	},
	"KeyToPath": {
		"key": leaf, "path": leaf, "mode": leaf,
	},
	"EmptyDirVolumeSource": {
		"medium": leaf, "sizeLimit": leaf,
	},
	"HostPathVolumeSource": {
		"path": leaf, "type": leaf,
	},
	"PVCVolumeSource": {
		"claimName": leaf, "readOnly": leaf,
	},
	"ProjectedVolumeSource": {
		"defaultMode": leaf,
		"sources":     seqOf("VolumeProjection"),
	},
	"VolumeProjection": {
		"configMap":           mapping("ConfigMapProjection"),
		"secret":              mapping("SecretProjection"),
		"serviceAccountToken": mapping("ServiceAccountTokenProjection"),
		"downwardAPI":         mapping("DownwardAPIVolumeSource"),
		"clusterTrustBundle":  mapping(stringMap),
	},
	"ConfigMapProjection": {
		"name": leaf, "optional": leaf,
		"items": seqOf("KeyToPath"),
	},
	"SecretProjection": {
		"name": leaf, "optional": leaf,
		"items": seqOf("KeyToPath"),
	},
	"ServiceAccountTokenProjection": {
		"path": leaf, "expirationSeconds": leaf, "audience": leaf,
	},
	"DownwardAPIVolumeSource": {
		"defaultMode": leaf,
		"items":       seqOf("DownwardAPIVolumeFile"),
	},
	"DownwardAPIVolumeFile": {
		"path": leaf, "mode": leaf,
		"fieldRef":         mapping("ObjectFieldSelector"),
		"resourceFieldRef": mapping("ResourceFieldSelector"),
	},
	"ServiceSpec": {
		// clusterIPs must be listed: it is a real field one edit from
		// clusterIP, and the typo pass would otherwise "fix" it.
		"type": leaf, "clusterIP": leaf, "clusterIPs": seqOf(""), "externalName": leaf,
		"sessionAffinity": leaf, "externalTrafficPolicy": leaf, "loadBalancerIP": leaf,
		"internalTrafficPolicy": leaf, "ipFamilyPolicy": leaf,
		"allocateLoadBalancerNodePorts": leaf, "healthCheckNodePort": leaf,
		"publishNotReadyAddresses": leaf, "loadBalancerClass": leaf,
		"trafficDistribution":      leaf,
		"ipFamilies":               seqOf(""),
		"externalIPs":              seqOf(""),
		"loadBalancerSourceRanges": seqOf(""),
		"sessionAffinityConfig":    mapping("SessionAffinityConfig"),
		"selector":                 mapping(stringMap),
		"ports":                    seqOf("ServicePort"),
	},
	"SessionAffinityConfig": {
		"clientIP": mapping("ClientIPConfig"),
	},
	"ClientIPConfig": {
		"timeoutSeconds": leaf,
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
