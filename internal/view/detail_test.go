package view

import (
	"strings"
	"testing"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
)

func podFixture() *api.WorkloadDetail {
	return &api.WorkloadDetail{
		Workload: &k8s.Resource{
			Kind: "Pod", Namespace: "rtzflags", Name: "flg-stall-subject-688899b5cb-fr2k8",
			OwnerKind: "ReplicaSet", OwnerName: "flg-stall-subject-688899b5cb",
			Labels: `{"app":"flg-stall-subject"}`,
			Spec: `{"nodeName":"agent-2","serviceAccountName":"default","containers":[
				{"name":"app","image":"busybox:1.36","readinessProbe":true,
				 "resources":{"requests":{"cpu":"10m","memory":"16Mi"},
				              "limits":{"cpu":"50m","memory":"32Mi"}}}]}`,
			Status: `{"phase":"Running","qosClass":"Burstable","restartCount":15,
				"containerStatuses":[{"name":"app","ready":false,"restartCount":15,
					"state":{"type":"running"},
					"lastTerminated":{"reason":"Completed","exitCode":0}}],
				"conditions":[{"type":"Ready","status":"False","reason":"ContainersNotReady",
					"message":"containers with unready status: [app]"}]}`,
		},
		Events: []api.Event{{
			Name: "flg-stall-subject.18d2",
			Spec: `{"reason":"Unhealthy","message":"Readiness probe failed:","type":"Warning",
				"count":11268,"lastTimestamp":"2026-09-06T12:10:01Z",
				"involvedObject":{"kind":"Pod","name":"flg-stall-subject-688899b5cb-fr2k8"}}`,
		}},
	}
}

func TestPodDetailRendersWhatAPodActuallyHas(t *testing.T) {
	var b strings.Builder
	Detail(&b, podFixture())
	out := b.String()

	for _, want := range []string{
		"Kind:      Pod", "Phase:     Running", "Node:      agent-2",
		"Owner:     ReplicaSet/flg-stall-subject-688899b5cb", "QoS:       Burstable",
		"Containers (1)", "busybox:1.36", "10m/50m", "16Mi/32Mi", "readiness",
		"Last terminated (1)", "Completed",
		"Conditions (1)", "ContainersNotReady",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// A pod has no ready-replica ratio and owns no pods. Printing "Ready: 0/0" and
	// "Pods (0)" reads as two findings when they are only the wrong questions.
	for _, unwanted := range []string{"Ready:    0/0", "Pods (0)", "ReplicaSets ("} {
		if strings.Contains(out, unwanted) {
			t.Errorf("controller-shaped section %q leaked into the pod pane:\n%s", unwanted, out)
		}
	}
}

// The regression this pins: every field but the name lives inside the event's spec blob, so
// decoding them as top-level keys renders a table of blanks — and a blank Events table
// looks like a healthy workload.
func TestEventsRenderTheirContentNotBlanks(t *testing.T) {
	var b strings.Builder
	Detail(&b, podFixture())
	out := b.String()

	for _, want := range []string{"Warning", "Unhealthy", "11268", "Readiness probe failed:"} {
		if !strings.Contains(out, want) {
			t.Errorf("event field %q missing from:\n%s", want, out)
		}
	}
	if strings.Contains(out, "<none>   <none>") {
		t.Errorf("events rendered as blanks:\n%s", out)
	}
}

// Controllers keep the renderer they had; the pod branch must not have taken theirs.
func TestControllerDetailStillRendersItsOwnShape(t *testing.T) {
	var b strings.Builder
	Detail(&b, &api.WorkloadDetail{
		Workload: &k8s.Resource{Kind: "Deployment", Namespace: "payments", Name: "checkout",
			Spec: `{"replicas":3}`, Status: `{"readyReplicas":2}`},
		Pods: []k8s.Resource{{Kind: "Pod", Namespace: "payments", Name: "checkout-a",
			Status: `{"phase":"Running"}`}},
	})
	out := b.String()
	for _, want := range []string{"Kind:     Deployment", "Ready:    2/3", "Pods (1)", "checkout-a"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
