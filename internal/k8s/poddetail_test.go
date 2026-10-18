package k8s

import "testing"

// These payloads are copied from a live agent response, not invented. A parser tested only
// against a shape someone imagined passes while the real one decodes to blanks — which is
// exactly how the Events table shipped empty.
const livePodSpec = `{
  "nodeName": "k3d-dev-cluster-agent-2",
  "hostNetwork": false,
  "containers": [{
    "name": "app",
    "image": "busybox:1.36",
    "resources": {"requests": {"cpu": "10m", "memory": "16Mi"},
                  "limits": {"cpu": "50m", "memory": "32Mi"}},
    "livenessProbe": false, "readinessProbe": true, "startupProbe": false
  }],
  "initContainers": [],
  "serviceAccountName": "default"
}`

const livePodStatus = `{
  "phase": "Running",
  "qosClass": "Burstable",
  "restartCount": 15,
  "lastRestartAt": "2026-09-06T12:51:52Z",
  "containerStatuses": [{
    "name": "app",
    "image": "docker.io/library/busybox:1.36",
    "ready": false,
    "restartCount": 15,
    "state": {"type": "running", "startedAt": "2026-09-06T12:51:52Z"},
    "lastTerminated": {"reason": "Completed", "exitCode": 0,
                       "finishedAt": "2026-09-06T12:51:52Z", "message": null}
  }],
  "initContainerStatuses": [],
  "conditions": [
    {"type": "Initialized", "status": "True"},
    {"type": "Ready", "status": "False", "reason": "ContainersNotReady",
     "message": "containers with unready status: [app]"}
  ]
}`

func TestParsePodDetailMergesSpecAndStatus(t *testing.T) {
	d := ParsePodDetail(Resource{
		Kind: "Pod", Namespace: "rtzflags", Name: "flg-stall-subject-688899b5cb-fr2k8",
		Spec: livePodSpec, Status: livePodStatus,
	})

	if d.QoS != "Burstable" || d.NodeName != "k3d-dev-cluster-agent-2" {
		t.Errorf("QoS=%q node=%q", d.QoS, d.NodeName)
	}
	if d.ServiceAccount != "default" {
		t.Errorf("account = %q", d.ServiceAccount)
	}
	if len(d.Containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(d.Containers))
	}

	c := d.Containers[0]
	// The spec image is what was asked for; the status image is what the runtime resolved.
	// Showing the spec one keeps `describe` comparable with the manifest.
	if c.Image != "busybox:1.36" {
		t.Errorf("image = %q, want the spec image", c.Image)
	}
	if c.Ready {
		t.Error("container reported ready when status says false")
	}
	if c.Restarts != 15 {
		t.Errorf("restarts = %d", c.Restarts)
	}
	if got := c.StateLabel(); got != "running" {
		t.Errorf("state = %q", got)
	}
	if c.CPU() != "10m/50m" || c.Mem() != "16Mi/32Mi" {
		t.Errorf("cpu=%q mem=%q", c.CPU(), c.Mem())
	}
	// A readiness probe that never passes is the usual reason a Running pod is not Ready,
	// so which probes exist has to be on screen.
	if len(c.Probes) != 1 || c.Probes[0] != "readiness" {
		t.Errorf("probes = %v, want [readiness]", c.Probes)
	}
	if c.LastTerminatedReason != "Completed" || !c.HasLastExitCode || c.LastExitCode != 0 {
		t.Errorf("lastTerminated = %q exit=%d has=%v",
			c.LastTerminatedReason, c.LastExitCode, c.HasLastExitCode)
	}
	if len(d.Conditions) != 2 || d.Conditions[1].Reason != "ContainersNotReady" {
		t.Errorf("conditions = %+v", d.Conditions)
	}
}

// Exit code 0 is a real value and "no exit code recorded" is a different one. Collapsing
// them hides a clean exit behind the same dash as a container that never terminated.
func TestZeroExitCodeIsDistinctFromNoExitCode(t *testing.T) {
	with := ParsePodDetail(Resource{Kind: "Pod", Status: `{"containerStatuses":[
		{"name":"a","lastTerminated":{"reason":"Completed","exitCode":0}}]}`})
	if !with.Containers[0].HasLastExitCode || with.Containers[0].LastExitCode != 0 {
		t.Error("exit code 0 was not recorded")
	}
	without := ParsePodDetail(Resource{Kind: "Pod", Status: `{"containerStatuses":[
		{"name":"a","state":{"type":"running"}}]}`})
	if without.Containers[0].HasLastExitCode {
		t.Error("a container that never terminated reported an exit code")
	}
}

// A pod whose payload carried no containers is an agent-version question, not a workload
// one, and the renderer has to be able to tell them apart.
func TestMissingContainerDetailIsFlagged(t *testing.T) {
	d := ParsePodDetail(Resource{Kind: "Pod", Status: `{"phase":"Running"}`})
	if !d.MissingContainers {
		t.Error("a payload with no containers was not flagged")
	}
}

func TestInitContainersAreMarkedAndComeFirst(t *testing.T) {
	d := ParsePodDetail(Resource{Kind: "Pod",
		Spec:   `{"initContainers":[{"name":"migrate","image":"m:1"}],"containers":[{"name":"app","image":"a:1"}]}`,
		Status: `{"initContainerStatuses":[{"name":"migrate","ready":true}],"containerStatuses":[{"name":"app","ready":true}]}`})
	if len(d.Containers) != 2 {
		t.Fatalf("containers = %d", len(d.Containers))
	}
	if d.Containers[0].Name != "migrate" || !d.Containers[0].Init {
		t.Errorf("first = %+v, want the init container", d.Containers[0])
	}
	if d.Containers[1].Init {
		t.Error("app was marked as an init container")
	}
}

// A container in the spec but not yet in the status has not started — which is precisely
// what someone describing a stuck pod is looking for, so it must not be dropped.
func TestContainerPresentInOnlyOneHalfStillAppears(t *testing.T) {
	d := ParsePodDetail(Resource{Kind: "Pod",
		Spec:   `{"containers":[{"name":"app","image":"a:1"},{"name":"sidecar","image":"s:1"}]}`,
		Status: `{"containerStatuses":[{"name":"app","ready":true}]}`})
	if len(d.Containers) != 2 {
		t.Fatalf("containers = %d, want both", len(d.Containers))
	}
	if d.Containers[1].Name != "sidecar" || d.Containers[1].StateLabel() != "<unknown>" {
		t.Errorf("unstarted container = %+v", d.Containers[1])
	}
}

// The event's fields live inside the spec blob. Decoding them as top-level keys yields a
// struct that is empty except for the name — a table of blanks, which is what shipped.
func TestParseEventReadsTheSpecBlob(t *testing.T) {
	e := ParseEvent(`{"reason":"Unhealthy","message":"Readiness probe failed:","type":"Warning",
		"count":11268,"lastTimestamp":"2026-09-06T12:10:01Z",
		"involvedObject":{"kind":"Pod","name":"flg-stall-subject-688899b5cb-fr2k8","namespace":"rtzflags"}}`, "")

	if e.Type != "Warning" || e.Reason != "Unhealthy" || e.Count != 11268 {
		t.Errorf("type=%q reason=%q count=%d", e.Type, e.Reason, e.Count)
	}
	if e.LastTimestamp != "2026-09-06T12:10:01Z" {
		t.Errorf("lastTimestamp = %q", e.LastTimestamp)
	}
	if e.InvolvedKind != "Pod" || e.InvolvedName == "" {
		t.Errorf("involvedObject = %s/%s", e.InvolvedKind, e.InvolvedName)
	}
}

// An event with no timestamp in its spec still has the row's creation time, and rendering
// "<unknown>" for an event we can date is a needless loss.
func TestEventFallsBackToTheRowTimestamp(t *testing.T) {
	e := ParseEvent(`{"reason":"Killing","type":"Normal"}`, "2026-09-06T09:00:00Z")
	if e.LastTimestamp != "2026-09-06T09:00:00Z" {
		t.Errorf("lastTimestamp = %q", e.LastTimestamp)
	}
}

// Logs and traces are indexed under the workload's name, not the pod's. Recovering it by
// trimming "the last two segments" would mangle any workload whose own name has a dash —
// the pod carries the exact hash to remove.
func TestServiceNameStripsExactlyTheReplicaSetHash(t *testing.T) {
	cases := []struct {
		name string
		r    Resource
		want string
	}{
		{"dashed workload name", Resource{
			Kind: "Pod", Name: "flg-stall-subject-688899b5cb-fr2k8",
			OwnerKind: "ReplicaSet", OwnerName: "flg-stall-subject-688899b5cb",
			Labels: `{"pod-template-hash":"688899b5cb"}`}, "flg-stall-subject"},
		{"explicit app label wins", Resource{
			Kind: "Pod", Name: "checkout-gateway-5b865fbfcb-2jd59",
			OwnerKind: "ReplicaSet", OwnerName: "checkout-gateway-5b865fbfcb",
			Labels: `{"app":"checkout-gateway","pod-template-hash":"5b865fbfcb"}`}, "checkout-gateway"},
		{"statefulset owner is already the name", Resource{
			Kind: "Pod", Name: "postgres-0", OwnerKind: "StatefulSet", OwnerName: "postgres",
			Labels: `{}`}, "postgres"},
		{"no hash label leaves the owner alone", Resource{
			Kind: "Pod", Name: "orphan-xyz", OwnerKind: "ReplicaSet", OwnerName: "orphan-abc123",
			Labels: `{}`}, "orphan-abc123"},
		{"a controller is its own service", Resource{
			Kind: "Deployment", Name: "checkout-gateway", Labels: `{}`}, "checkout-gateway"},
		{"a bare pod falls back to its own name", Resource{
			Kind: "Pod", Name: "debug-shell", Labels: `{}`}, "debug-shell"},
	}
	for _, c := range cases {
		if got := ServiceNameFor(c.r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
