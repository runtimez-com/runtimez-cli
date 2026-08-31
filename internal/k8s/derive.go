// Package k8s turns the control plane's resource rows into the fields an operator actually
// reads in a table.
//
// The backend hands back rows whose labels/annotations/spec/status are JSON *strings*
// (KubeResourceClickHouseRepository.listByKindRows), so every useful column has to be dug
// out of them. The fallback chains below are a deliberate port of the UI's derive.js — the
// runtimez agent normalizes some fields (it flattens per-pod restarts into
// status.restartCount and puts every controller's ready count in readyReplicas, including
// DaemonSets) while standard Kubernetes uses others. Reading only one shape silently
// reports zero for the other, which looks like a healthy cluster rather than a parse bug.
package k8s

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Resource is one row as the API returns it.
type Resource struct {
	UID         string `json:"uid"`
	Kind        string `json:"kind"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	OwnerKind   string `json:"ownerKind"`
	OwnerName   string `json:"ownerName"`
	Labels      string `json:"labels"`
	Annotations string `json:"annotations"`
	Spec        string `json:"spec"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
}

// obj is a decoded JSON object with forgiving accessors: a missing key, a null, and a
// wrong-typed value all read as "absent" rather than panicking, because these payloads come
// from several agent versions at once.
type obj map[string]any

func parse(raw string) obj {
	if strings.TrimSpace(raw) == "" {
		return obj{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return obj{}
	}
	return m
}

// SpecOf and StatusOf expose the decoded blobs for callers that need something beyond the
// derived models.
func (r Resource) SpecOf() map[string]any   { return parse(r.Spec) }
func (r Resource) StatusOf() map[string]any { return parse(r.Status) }
func (r Resource) LabelsOf() map[string]any { return parse(r.Labels) }

func (o obj) str(key string) string {
	s, _ := o[key].(string)
	return s
}

// num reads a JSON number, which decodes as float64. Returns ok=false when the key is
// absent or null, so a caller can tell "not reported" from a real zero.
func (o obj) num(key string) (int, bool) {
	switch v := o[key].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n, true
		}
	}
	return 0, false
}

func (o obj) child(key string) obj {
	m, _ := o[key].(map[string]any)
	return obj(m)
}

func (o obj) list(key string) []any {
	l, _ := o[key].([]any)
	return l
}

// firstNum walks a fallback chain and returns the first key that is actually present.
func firstNum(o obj, keys ...string) int {
	for _, k := range keys {
		if v, ok := o.num(k); ok {
			return v
		}
	}
	return 0
}

// Pod is the derived view of a Pod row.
type Pod struct {
	Namespace     string
	Name          string
	Phase         string
	Restarts      int
	LastRestartAt string
	NodeName      string
	OwnerKind     string
	OwnerName     string
	CreatedAt     string
}

// ParsePod mirrors derive.js parsePod.
func ParsePod(r Resource) Pod {
	status := parse(r.Status)
	spec := parse(r.Spec)

	// The agent flattens per-pod restarts into status.restartCount. Fall back to summing the
	// standard containerStatuses for a payload that carries the full Pod status shape.
	restarts, ok := status.num("restartCount")
	if !ok {
		for _, cs := range status.list("containerStatuses") {
			if m, isMap := cs.(map[string]any); isMap {
				n, _ := obj(m).num("restartCount")
				restarts += n
			}
		}
	}

	last := status.str("lastRestartAt")
	if last == "" {
		var finished []string
		for _, cs := range status.list("containerStatuses") {
			m, isMap := cs.(map[string]any)
			if !isMap {
				continue
			}
			if f := obj(m).child("lastState").child("terminated").str("finishedAt"); f != "" {
				finished = append(finished, f)
			}
		}
		// Lexicographic max is the most recent: these are ISO-8601 UTC.
		sort.Strings(finished)
		if len(finished) > 0 {
			last = finished[len(finished)-1]
		}
	}

	phase := status.str("phase")
	if phase == "" {
		phase = "Unknown"
	}

	return Pod{
		Namespace: r.Namespace, Name: r.Name, Phase: phase,
		Restarts: restarts, LastRestartAt: last,
		NodeName:  spec.str("nodeName"),
		OwnerKind: r.OwnerKind, OwnerName: r.OwnerName, CreatedAt: r.CreatedAt,
	}
}

// Workload is the derived view of a Deployment, StatefulSet or DaemonSet row.
type Workload struct {
	Kind      string
	Namespace string
	Name      string
	Ready     int
	Desired   int
	Image     string
	CreatedAt string
}

// ParseWorkload mirrors derive.js parseWorkload, including both readiness fallback chains.
func ParseWorkload(r Resource) Workload {
	status := parse(r.Status)
	spec := parse(r.Spec)

	// Containers live under spec.template.spec.containers in a standard manifest and under
	// spec.containers when the agent has flattened it.
	containers := spec.child("template").child("spec").list("containers")
	if len(containers) == 0 {
		containers = spec.list("containers")
	}
	image := ""
	if len(containers) > 0 {
		if m, ok := containers[0].(map[string]any); ok {
			image = obj(m).str("image")
		}
	}

	kind := r.Kind
	if kind == "" {
		kind = "Deployment"
	}

	return Workload{
		Kind: kind, Namespace: r.Namespace, Name: r.Name,
		// readyReplicas first: the agent normalizes every controller into it, DaemonSets
		// included, and does not populate numberReady.
		Ready:   firstNum(status, "readyReplicas", "numberReady", "availableReplicas", "numberAvailable"),
		Desired: pickDesired(spec, status),
		Image:   image, CreatedAt: r.CreatedAt,
	}
}

// pickDesired takes spec.replicas for Deployment/StatefulSet and desiredNumberScheduled for
// DaemonSet, falling back through the remaining standard fields.
func pickDesired(spec, status obj) int {
	if v, ok := spec.num("replicas"); ok {
		return v
	}
	return firstNum(status, "desiredNumberScheduled", "currentNumberScheduled", "replicas")
}

// ReadyRatio renders the READY column.
func (w Workload) ReadyRatio() string { return fmt.Sprintf("%d/%d", w.Ready, w.Desired) }

// Service is the derived view of a Service row.
type Service struct {
	Namespace  string
	Name       string
	Type       string
	ClusterIP  string
	ExternalIP string
	Ports      string
	CreatedAt  string
}

// ParseService mirrors derive.js parseService.
func ParseService(r Resource) Service {
	spec := parse(r.Spec)
	status := parse(r.Status)

	var ports []string
	for _, p := range spec.list("ports") {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		po := obj(m)
		var parts []string
		if v, ok := po.num("port"); ok {
			parts = append(parts, strconv.Itoa(v))
		}
		if v, ok := po.num("nodePort"); ok {
			parts = append(parts, strconv.Itoa(v))
		}
		s := strings.Join(parts, ":")
		if proto := po.str("protocol"); proto != "" && s != "" {
			s += "/" + proto
		}
		if s != "" {
			ports = append(ports, s)
		}
	}

	svcType := spec.str("type")
	if svcType == "" {
		svcType = "ClusterIP"
	}
	clusterIP := spec.str("clusterIP")
	if clusterIP == "" {
		clusterIP = "-"
	}

	extIP := ""
	if svcType == "LoadBalancer" {
		extIP = "<pending>"
		if ing := status.child("loadBalancer").list("ingress"); len(ing) > 0 {
			if m, ok := ing[0].(map[string]any); ok {
				if ip := obj(m).str("ip"); ip != "" {
					extIP = ip
				} else if h := obj(m).str("hostname"); h != "" {
					extIP = h
				}
			}
		}
	}

	return Service{
		Namespace: r.Namespace, Name: r.Name, Type: svcType,
		ClusterIP: clusterIP, ExternalIP: extIP,
		Ports: strings.Join(ports, ","), CreatedAt: r.CreatedAt,
	}
}

// Ingress is the derived view of an Ingress row.
type Ingress struct {
	Namespace string
	Name      string
	Class     string
	Hosts     []string
	Paths     []string
	CreatedAt string
}

// ParseIngress mirrors derive.js parseIngress, including the networking.k8s.io v1beta1
// backend shape that older clusters still emit.
func ParseIngress(r Resource) Ingress {
	spec := parse(r.Spec)
	out := Ingress{Namespace: r.Namespace, Name: r.Name, Class: spec.str("ingressClassName"), CreatedAt: r.CreatedAt}

	for _, ru := range spec.list("rules") {
		m, ok := ru.(map[string]any)
		if !ok {
			continue
		}
		rule := obj(m)
		host := rule.str("host")
		if host == "" {
			host = "*"
		}
		out.Hosts = append(out.Hosts, host)

		for _, p := range rule.child("http").list("paths") {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			path := obj(pm)
			backend := path.child("backend")
			svc := backend.child("service").str("name")
			if svc == "" {
				svc = backend.str("serviceName") // v1beta1
			}
			if svc == "" {
				svc = "?"
			}
			port := ""
			if v, ok := backend.child("service").child("port").num("number"); ok {
				port = ":" + strconv.Itoa(v)
			} else if v, ok := backend.num("servicePort"); ok {
				port = ":" + strconv.Itoa(v)
			}
			p := path.str("path")
			if p == "" {
				p = "/"
			}
			out.Paths = append(out.Paths, fmt.Sprintf("%s->%s%s", p, svc, port))
		}
	}
	return out
}

// Node is the derived view of a Node row.
type Node struct {
	Name       string
	Ready      bool
	Roles      []string
	Version    string
	InternalIP string
	OS         string
	Kernel     string
	Runtime    string
	CreatedAt  string
}

// ParseNode mirrors derive.js parseNode.
func ParseNode(r Resource) Node {
	status := parse(r.Status)
	labels := parse(r.Labels)

	var roles []string
	for k := range labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		roles = []string{"worker"}
	}

	ready := false
	for _, c := range status.list("conditions") {
		if m, ok := c.(map[string]any); ok {
			cond := obj(m)
			if cond.str("type") == "Ready" {
				ready = cond.str("status") == "True"
			}
		}
	}

	ip := ""
	for _, a := range status.list("addresses") {
		if m, ok := a.(map[string]any); ok {
			addr := obj(m)
			if addr.str("type") == "InternalIP" {
				ip = addr.str("address")
				break
			}
		}
	}

	info := status.child("nodeInfo")
	osName := ""
	if info.str("osImage") != "" {
		o, arch := info.str("operatingSystem"), info.str("architecture")
		if o == "" {
			o = "linux"
		}
		if arch == "" {
			arch = "amd64"
		}
		osName = o + "/" + arch
	}

	return Node{
		Name: r.Name, Ready: ready, Roles: roles,
		Version: info.str("kubeletVersion"), InternalIP: ip,
		OS: osName, Kernel: info.str("kernelVersion"), Runtime: info.str("containerRuntimeVersion"),
		CreatedAt: r.CreatedAt,
	}
}

// NodeStatus renders the STATUS column the way kubectl does.
func (n Node) NodeStatus() string {
	if n.Ready {
		return "Ready"
	}
	return "NotReady"
}

// Age renders elapsed time from the row's createdAt, which the backend formats as
// '%Y-%m-%dT%H:%M:%SZ'.
func Age(createdAt string) string {
	if strings.TrimSpace(createdAt) == "" {
		return "<unknown>"
	}
	t, err := time.Parse("2006-01-02T15:04:05Z", createdAt)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, createdAt); err != nil {
			return "<unknown>"
		}
	}
	return Duration(time.Since(t))
}

// Duration is the compact age format: 45s, 12m, 5h, 9d.
func Duration(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	}
}

// Itoa formats an int. Exported so the UI packages share one integer formatter rather than
// each growing their own.
func Itoa(n int) string { return strconv.Itoa(n) }
