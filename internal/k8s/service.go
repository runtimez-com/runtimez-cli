package k8s

import "strings"

// ServiceNameFor derives the name a workload's telemetry is indexed under, so a drill-down
// from a pod row can find its logs and traces.
//
// A pod is named <deployment>-<pod-template-hash>-<random>, and its ReplicaSet owner is
// named <deployment>-<pod-template-hash>. The hash is not guessed: it is on the pod as the
// pod-template-hash label, so the deployment name is recovered by removing exactly that
// suffix. Trimming "the last two dash-separated segments" would mangle any workload whose
// own name contains a dash.
func ServiceNameFor(r Resource) string {
	labels := obj(parse(r.Labels))

	// An explicit service name wins: someone who set it meant it.
	for _, key := range []string{
		"app.kubernetes.io/name",
		"app",
		"k8s-app",
	} {
		if v := labels.str(key); v != "" {
			return v
		}
	}

	if r.Kind == "Pod" && r.OwnerName != "" {
		owner := r.OwnerName
		if r.OwnerKind == "ReplicaSet" {
			if hash := labels.str("pod-template-hash"); hash != "" {
				owner = strings.TrimSuffix(owner, "-"+hash)
			}
		}
		return owner
	}
	return r.Name
}
