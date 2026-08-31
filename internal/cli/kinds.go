package cli

import (
	"fmt"
	"sort"
	"strings"
)

// canonicalKinds maps the short forms an operator already types into the exact kind stored
// by the sync. The backend filters `kind` with a literal equality, so "deploy" matches
// nothing at all — silently, as an empty list. Resolving here turns that into either the
// right query or a named error.
var canonicalKinds = map[string]string{
	"po": "Pod", "pod": "Pod", "pods": "Pod",
	"deploy": "Deployment", "deployment": "Deployment", "deployments": "Deployment",
	"sts": "StatefulSet", "statefulset": "StatefulSet", "statefulsets": "StatefulSet",
	"ds": "DaemonSet", "daemonset": "DaemonSet", "daemonsets": "DaemonSet",
	"rs": "ReplicaSet", "replicaset": "ReplicaSet", "replicasets": "ReplicaSet",
	"svc": "Service", "service": "Service", "services": "Service",
	"ing": "Ingress", "ingress": "Ingress", "ingresses": "Ingress",
	"no": "Node", "node": "Node", "nodes": "Node",
	"ns": "Namespace", "namespace": "Namespace", "namespaces": "Namespace",
	"cm": "ConfigMap", "configmap": "ConfigMap", "configmaps": "ConfigMap",
	"secret": "Secret", "secrets": "Secret",
	"pvc": "PersistentVolumeClaim", "persistentvolumeclaim": "PersistentVolumeClaim",
	"pv": "PersistentVolume", "persistentvolume": "PersistentVolume",
	"job": "Job", "jobs": "Job",
	"cj": "CronJob", "cronjob": "CronJob", "cronjobs": "CronJob",
	"hpa": "HorizontalPodAutoscaler", "horizontalpodautoscaler": "HorizontalPodAutoscaler",
	"pdb": "PodDisruptionBudget", "poddisruptionbudget": "PodDisruptionBudget",
	"sa": "ServiceAccount", "serviceaccount": "ServiceAccount", "serviceaccounts": "ServiceAccount",
	"crd": "CustomResourceDefinition",
	"sc":  "StorageClass", "storageclass": "StorageClass",
	"netpol": "NetworkPolicy", "networkpolicy": "NetworkPolicy",
	"limitrange": "LimitRange", "lr": "LimitRange",
	"resourcequota": "ResourceQuota", "quota": "ResourceQuota",
}

// resolveKind turns user input into the stored kind. "all" clears the filter. An input that
// is already a canonical kind ("Deployment") passes through, so an unlisted CRD kind still
// works when typed exactly.
func resolveKind(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" || strings.EqualFold(trimmed, "all") {
		return "", nil
	}
	if k, ok := canonicalKinds[strings.ToLower(trimmed)]; ok {
		return k, nil
	}
	// An uppercase first letter is the convention for a Kubernetes kind; anything else is
	// far more likely a typo than a CRD, and querying it would return a confusing empty list.
	if trimmed[0] >= 'A' && trimmed[0] <= 'Z' {
		return trimmed, nil
	}
	return "", usageErrorf("unknown resource kind %q — try one of: %s, or the exact kind name",
		input, strings.Join(shortKindHints(), ", "))
}

func shortKindHints() []string {
	hints := []string{"pods", "deploy", "sts", "ds", "svc", "ing", "nodes", "jobs", "all"}
	sort.Strings(hints)
	return hints
}

// truncationNotice warns when the server's row cap was reached. Presenting a capped list as
// the whole cluster is the kind of quiet wrong answer that gets acted on.
func truncationNotice(got, limit int) string {
	if got < limit {
		return ""
	}
	return fmt.Sprintf(
		"warning: server returned its maximum of %d rows — this list is TRUNCATED; narrow it with -n <namespace> or a kind filter",
		limit)
}
