package k8s

import (
	"testing"
	"time"
)

// The agent and standard Kubernetes report the same facts under different keys. Reading only
// one shape reports zero for the other — which renders as a calm "0/0" rather than a parse
// bug, so each fallback gets its own test.

func TestParseWorkload_AgentShape_ReadyReplicas(t *testing.T) {
	w := ParseWorkload(Resource{
		Kind: "Deployment", Namespace: "payments", Name: "checkout",
		Spec:   `{"replicas":3,"template":{"spec":{"containers":[{"image":"checkout:1.4.2"}]}}}`,
		Status: `{"readyReplicas":2}`,
	})

	if got := w.ReadyRatio(); got != "2/3" {
		t.Errorf("ReadyRatio() = %q, want 2/3", got)
	}
	if w.Image != "checkout:1.4.2" {
		t.Errorf("Image = %q", w.Image)
	}
}

// A DaemonSet from standard k8s reports numberReady and desiredNumberScheduled, and has no
// spec.replicas at all.
func TestParseWorkload_StandardDaemonSet(t *testing.T) {
	w := ParseWorkload(Resource{
		Kind:   "DaemonSet",
		Spec:   `{"template":{"spec":{"containers":[{"image":"fluentd:1"}]}}}`,
		Status: `{"numberReady":5,"desiredNumberScheduled":6}`,
	})

	if got := w.ReadyRatio(); got != "5/6" {
		t.Errorf("standard DaemonSet ReadyRatio() = %q, want 5/6", got)
	}
}

// The agent normalizes every controller into readyReplicas, DaemonSets included, and does
// NOT populate numberReady — so readyReplicas has to win the chain.
func TestParseWorkload_AgentDaemonSetPrefersReadyReplicas(t *testing.T) {
	w := ParseWorkload(Resource{
		Kind:   "DaemonSet",
		Status: `{"readyReplicas":4,"numberReady":0,"desiredNumberScheduled":4}`,
	})

	if got := w.ReadyRatio(); got != "4/4" {
		t.Errorf("agent DaemonSet ReadyRatio() = %q, want 4/4", got)
	}
}

func TestParseWorkload_FlattenedContainers(t *testing.T) {
	w := ParseWorkload(Resource{
		Kind: "Deployment",
		Spec: `{"replicas":1,"containers":[{"image":"flat:2"}]}`,
	})
	if w.Image != "flat:2" {
		t.Errorf("flattened spec.containers not read: %q", w.Image)
	}
}

// Zero replicas is a real, meaningful state (a scaled-down deployment) and must not fall
// through the chain to some other field.
func TestParseWorkload_ScaledToZeroIsNotAFallthrough(t *testing.T) {
	w := ParseWorkload(Resource{
		Kind:   "Deployment",
		Spec:   `{"replicas":0}`,
		Status: `{"replicas":7}`,
	})
	if got := w.ReadyRatio(); got != "0/0" {
		t.Errorf("ReadyRatio() = %q, want 0/0 — spec.replicas:0 must win over status.replicas", got)
	}
}

func TestParseWorkload_EmptyStatusIsZeroNotPanic(t *testing.T) {
	w := ParseWorkload(Resource{Kind: "Deployment", Spec: "", Status: ""})
	if got := w.ReadyRatio(); got != "0/0" {
		t.Errorf("ReadyRatio() = %q", got)
	}
	if w.Kind != "Deployment" {
		t.Errorf("Kind = %q", w.Kind)
	}
}

func TestParseWorkload_MalformedJSONDoesNotPanic(t *testing.T) {
	w := ParseWorkload(Resource{Kind: "Deployment", Spec: "{not json", Status: "[]"})
	if got := w.ReadyRatio(); got != "0/0" {
		t.Errorf("ReadyRatio() = %q", got)
	}
}

func TestParsePod_AgentFlattenedRestartCount(t *testing.T) {
	p := ParsePod(Resource{
		Namespace: "payments", Name: "checkout-abc",
		Spec:   `{"nodeName":"node-1"}`,
		Status: `{"phase":"Running","restartCount":7,"lastRestartAt":"2026-08-29T10:00:00Z"}`,
	})

	if p.Restarts != 7 {
		t.Errorf("Restarts = %d, want 7", p.Restarts)
	}
	if p.Phase != "Running" || p.NodeName != "node-1" {
		t.Errorf("unexpected pod: %+v", p)
	}
	if p.LastRestartAt != "2026-08-29T10:00:00Z" {
		t.Errorf("LastRestartAt = %q", p.LastRestartAt)
	}
}

// Standard k8s has no status.restartCount; the per-container counts have to be summed or a
// crash-looping pod reads as healthy.
func TestParsePod_SumsContainerStatusesWhenFlattenedCountAbsent(t *testing.T) {
	p := ParsePod(Resource{
		Status: `{"phase":"Running","containerStatuses":[
			{"restartCount":3,"lastState":{"terminated":{"finishedAt":"2026-08-29T09:00:00Z"}}},
			{"restartCount":4,"lastState":{"terminated":{"finishedAt":"2026-08-29T11:00:00Z"}}}]}`,
	})

	if p.Restarts != 7 {
		t.Errorf("Restarts = %d, want 7 (3+4)", p.Restarts)
	}
	// The most recent terminated timestamp, not the first one encountered.
	if p.LastRestartAt != "2026-08-29T11:00:00Z" {
		t.Errorf("LastRestartAt = %q, want the latest", p.LastRestartAt)
	}
}

// A real zero must not be replaced by the container sum — both paths yield 0, but the
// flattened count being present means it is authoritative.
func TestParsePod_ZeroFlattenedCountWins(t *testing.T) {
	p := ParsePod(Resource{
		Status: `{"restartCount":0,"containerStatuses":[{"restartCount":9}]}`,
	})
	if p.Restarts != 0 {
		t.Errorf("Restarts = %d, want 0 — an explicit 0 is authoritative", p.Restarts)
	}
}

func TestParsePod_MissingPhaseIsUnknown(t *testing.T) {
	if p := ParsePod(Resource{Status: `{}`}); p.Phase != "Unknown" {
		t.Errorf("Phase = %q, want Unknown", p.Phase)
	}
}

func TestParseService_LoadBalancerIngress(t *testing.T) {
	s := ParseService(Resource{
		Name:   "web",
		Spec:   `{"type":"LoadBalancer","clusterIP":"10.0.0.5","ports":[{"port":80,"nodePort":30080,"protocol":"TCP"}]}`,
		Status: `{"loadBalancer":{"ingress":[{"hostname":"a.elb.amazonaws.com"}]}}`,
	})

	if s.ExternalIP != "a.elb.amazonaws.com" {
		t.Errorf("ExternalIP = %q", s.ExternalIP)
	}
	if s.Ports != "80:30080/TCP" {
		t.Errorf("Ports = %q, want 80:30080/TCP", s.Ports)
	}
}

func TestParseService_LoadBalancerWithoutIngressIsPending(t *testing.T) {
	s := ParseService(Resource{Spec: `{"type":"LoadBalancer"}`, Status: `{}`})
	if s.ExternalIP != "<pending>" {
		t.Errorf("ExternalIP = %q, want <pending>", s.ExternalIP)
	}
}

// A ClusterIP service has no external IP, and inventing "<pending>" for it would suggest
// one is coming.
func TestParseService_ClusterIPHasNoExternalIP(t *testing.T) {
	s := ParseService(Resource{Spec: `{"clusterIP":"10.0.0.5"}`})
	if s.ExternalIP != "" {
		t.Errorf("ExternalIP = %q, want empty", s.ExternalIP)
	}
	if s.Type != "ClusterIP" {
		t.Errorf("Type = %q, want the ClusterIP default", s.Type)
	}
}

func TestParseIngress_V1Backend(t *testing.T) {
	i := ParseIngress(Resource{
		Spec: `{"ingressClassName":"nginx","rules":[{"host":"shop.example.com","http":{"paths":[
			{"path":"/checkout","backend":{"service":{"name":"checkout","port":{"number":8080}}}}]}}]}`,
	})

	if len(i.Hosts) != 1 || i.Hosts[0] != "shop.example.com" {
		t.Fatalf("Hosts = %v", i.Hosts)
	}
	if len(i.Paths) != 1 || i.Paths[0] != "/checkout->checkout:8080" {
		t.Errorf("Paths = %v", i.Paths)
	}
}

// Older clusters still emit the v1beta1 backend shape; dropping it would blank the column.
func TestParseIngress_V1Beta1Backend(t *testing.T) {
	i := ParseIngress(Resource{
		Spec: `{"rules":[{"http":{"paths":[
			{"path":"/","backend":{"serviceName":"legacy","servicePort":80}}]}}]}`,
	})

	if len(i.Hosts) != 1 || i.Hosts[0] != "*" {
		t.Errorf("a hostless rule should render as *, got %v", i.Hosts)
	}
	if len(i.Paths) != 1 || i.Paths[0] != "/->legacy:80" {
		t.Errorf("Paths = %v", i.Paths)
	}
}

func TestParseNode_RolesConditionsAndInfo(t *testing.T) {
	n := ParseNode(Resource{
		Name:   "ip-10-0-1-5",
		Labels: `{"node-role.kubernetes.io/control-plane":"","kubernetes.io/os":"linux"}`,
		Status: `{"conditions":[{"type":"MemoryPressure","status":"False"},{"type":"Ready","status":"True"}],
			"addresses":[{"type":"Hostname","address":"h"},{"type":"InternalIP","address":"10.0.1.5"}],
			"nodeInfo":{"kubeletVersion":"v1.30.4","osImage":"Amazon Linux 2","operatingSystem":"linux",
				"architecture":"arm64","kernelVersion":"5.10","containerRuntimeVersion":"containerd://1.7"}}`,
	})

	if !n.Ready || n.NodeStatus() != "Ready" {
		t.Errorf("node should be Ready: %+v", n)
	}
	if len(n.Roles) != 1 || n.Roles[0] != "control-plane" {
		t.Errorf("Roles = %v", n.Roles)
	}
	if n.InternalIP != "10.0.1.5" {
		t.Errorf("InternalIP = %q — must pick InternalIP, not the first address", n.InternalIP)
	}
	if n.OS != "linux/arm64" || n.Version != "v1.30.4" {
		t.Errorf("node info wrong: %+v", n)
	}
}

func TestParseNode_NotReadyAndDefaultRole(t *testing.T) {
	n := ParseNode(Resource{
		Name:   "worker-1",
		Labels: `{}`,
		Status: `{"conditions":[{"type":"Ready","status":"False"}]}`,
	})
	if n.NodeStatus() != "NotReady" {
		t.Errorf("NodeStatus() = %q", n.NodeStatus())
	}
	if len(n.Roles) != 1 || n.Roles[0] != "worker" {
		t.Errorf("Roles = %v, want the worker default", n.Roles)
	}
}

// A node with no Ready condition at all is not Ready — absence is not health.
func TestParseNode_MissingReadyConditionIsNotReady(t *testing.T) {
	n := ParseNode(Resource{Status: `{"conditions":[{"type":"DiskPressure","status":"False"}]}`})
	if n.Ready {
		t.Error("a node with no Ready condition must not report Ready")
	}
}

func TestAge(t *testing.T) {
	if got := Age(""); got != "<unknown>" {
		t.Errorf("Age(empty) = %q", got)
	}
	if got := Age("garbage"); got != "<unknown>" {
		t.Errorf("Age(garbage) = %q", got)
	}
	// The backend formats created_at as '%Y-%m-%dT%H:%M:%SZ'.
	recent := time.Now().Add(-90 * time.Minute).UTC().Format("2006-01-02T15:04:05Z")
	if got := Age(recent); got != "1h" {
		t.Errorf("Age(90m ago) = %q, want 1h", got)
	}
}

func TestDuration(t *testing.T) {
	cases := map[time.Duration]string{
		-time.Second:     "0s",
		45 * time.Second: "45s",
		12 * time.Minute: "12m",
		5 * time.Hour:    "5h",
		50 * time.Hour:   "2d",
	}
	for in, want := range cases {
		if got := Duration(in); got != want {
			t.Errorf("Duration(%v) = %q, want %q", in, got, want)
		}
	}
}
