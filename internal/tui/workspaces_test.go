package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/runtimez-com/runtimez-cli/internal/api"
)

func canned[T any](f *fakeClient, name string) (*api.Payload[T], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	p := &api.Payload[T]{}
	if v, ok := f.workspaces[name].(*T); ok {
		p.Typed = *v
	}
	return p, nil
}

func (f *fakeClient) Capacity(_ context.Context, _, _ string) (*api.Payload[api.Capacity], error) {
	return canned[api.Capacity](f, "Capacity")
}
func (f *fakeClient) MaintainItems(_ context.Context, _ string) (*api.Payload[api.MaintainList], error) {
	return canned[api.MaintainList](f, "MaintainItems")
}
func (f *fakeClient) UpgradeReadiness(_ context.Context, _, _, _ string) (*api.Payload[api.UpgradeReadiness], error) {
	return canned[api.UpgradeReadiness](f, "UpgradeReadiness")
}
func (f *fakeClient) NodePools(_ context.Context, _, _ string) (*api.Payload[api.NodePools], error) {
	return canned[api.NodePools](f, "NodePools")
}
func (f *fakeClient) NodeGroups(_ context.Context, _, _ string) (*api.Payload[api.NodeGroups], error) {
	return canned[api.NodeGroups](f, "NodeGroups")
}
func (f *fakeClient) IngressRoutes(_ context.Context, _, _ string) (*api.Payload[api.IngressRoutes], error) {
	return canned[api.IngressRoutes](f, "IngressRoutes")
}
func (f *fakeClient) MeshWorkloads(_ context.Context, _, _ string) (*api.Payload[api.MeshWorkloads], error) {
	return canned[api.MeshWorkloads](f, "MeshWorkloads")
}
func (f *fakeClient) GitOpsApps(_ context.Context, _, _ string) (*api.Payload[api.GitOpsApps], error) {
	return canned[api.GitOpsApps](f, "GitOpsApps")
}
func (f *fakeClient) VpcCni(_ context.Context, _, _ string) (*api.Payload[api.VpcCni], error) {
	return canned[api.VpcCni](f, "VpcCni")
}
func (f *fakeClient) EbsCsi(_ context.Context, _, _ string) (*api.Payload[api.EbsCsi], error) {
	return canned[api.EbsCsi](f, "EbsCsi")
}

func open(t *testing.T, c *fakeClient, view string) Model {
	t.Helper()
	m := newModel(t, c)
	m = key(m, ":")
	m = typeIn(m, view)
	return submit(t, m)
}

func i64p(v int64) *int64     { return &v }
func f64p(v float64) *float64 { return &v }
func intp(v int) *int         { return &v }

func karpenter(status string) *api.NodePools {
	l := &api.NodePools{NodePools: []api.NodePoolRow{
		{Name: "default", State: "HEALTHY", Nodes: i64p(3)},
		{Name: "gpu", State: "DEGRADED", StateDetail: "at its cpu limit", Nodes: i64p(0), Reasons: []api.AddonReason{
			{RuleID: "karpenter-limit-reached", Severity: "HIGH", Title: "NodePool at its cpu limit",
				VerifyCommand: "kubectl get nodepool gpu -o yaml", Recommendation: "raise spec.limits.cpu"},
		}},
	}}
	l.Status = status
	return l
}

// Every new command-bar name resolves, so none of them falls through to "unknown command".
func TestWorkspaceViewsAreRegistered(t *testing.T) {
	for _, name := range []string{"capacity", "cap", "pending", "storage", "pvc", "maintain", "eol", "upgrade",
		"nodepools", "karpenter", "ca", "routes", "traefik", "mesh", "istio", "gitops", "argocd",
		"vpccni", "cni", "ebscsi", "ebs"} {
		if _, ok := views[name]; !ok {
			t.Errorf(":%s is not a view", name)
		}
	}
	if _, ok := views["ing"]; !ok || views["ing"].Kind != "Ingress" {
		t.Error(":ing must stay the Kubernetes Ingress listing")
	}
}

func TestKarpenterViewListsAndOpensFindings(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"NodePools": karpenter("OK")}}
	m := open(t, c, "karpenter")

	out := m.View()
	for _, want := range []string{"Karpenter NodePools", "default", "gpu", "DEGRADED", "HIGH NodePool at its cpu limit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	m = key(m, "down")
	m = key(m, "enter")
	if m.mode != modeDetail {
		t.Fatalf("enter on an add-on row did not open its pane (status %q)", m.status)
	}
	pane := m.View()
	for _, want := range []string{"NodePool gpu", "at its cpu limit", "karpenter-limit-reached",
		"verify: kubectl get nodepool gpu -o yaml", "fix:    raise spec.limits.cpu"} {
		if !strings.Contains(pane, want) {
			t.Errorf("missing %q in pane:\n%s", want, pane)
		}
	}
	m = key(m, "esc")
	if m.mode != modeNormal || m.rowPane != "" {
		t.Errorf("esc did not close the pane")
	}
}

// The pane must open the row under the cursor even after a sort reorders the rows.
func TestRowPaneFollowsSort(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"NodePools": karpenter("OK")}}
	m := open(t, c, "karpenter")
	m = key(m, "s") // NODEPOOL ascending: default, gpu
	m = key(m, "i") // descending: gpu first
	m = key(m, "enter")
	if !strings.Contains(m.rowPane, "NodePool gpu") {
		t.Errorf("pane opened the wrong row after a sort:\n%s", m.rowPane)
	}
}

// An add-on that is not installed reads as "not installed", never as an empty healthy list.
func TestWorkspaceNonOKStatusIsTheNotice(t *testing.T) {
	mesh := &api.MeshWorkloads{}
	mesh.Status, mesh.Message = "NOT_DETECTED", "no istiod Deployment found"
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"MeshWorkloads": mesh}}
	m := open(t, c, "istio")

	out := m.View()
	if !strings.Contains(out, "NOT_DETECTED") || !strings.Contains(out, "no istiod Deployment found") {
		t.Errorf("non-OK status not on screen:\n%s", out)
	}
	if strings.Contains(out, "No workloads.") {
		t.Errorf("an undetected add-on rendered as an empty list:\n%s", out)
	}
}

func TestWorkspaceRulesNotEvaluatedIsStated(t *testing.T) {
	l := karpenter("OK")
	l.Addon = &struct {
		AddonID        string `json:"addonId"`
		DisplayName    string `json:"displayName"`
		Version        string `json:"version"`
		RuleCount      int    `json:"ruleCount"`
		RulesEvaluated bool   `json:"rulesEvaluated"`
	}{DisplayName: "Karpenter", Version: "1.2.0"}
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"NodePools": l}}
	m := open(t, c, "nodepools")
	if out := m.View(); !strings.Contains(out, "rules NOT evaluated") || !strings.Contains(out, "Karpenter 1.2.0") {
		t.Errorf("unevaluated rules not on screen:\n%s", out)
	}
}

func TestCapacityViewPoolsAndCaveats(t *testing.T) {
	cap := &api.Capacity{Status: "OK",
		Pools: []api.CapacityPool{{Name: "default", Nodes: 3, Pods: 41, AllocatablePods: i64p(110),
			CPURequestsCommitmentPct: f64p(75)}},
		Blocked: []api.BlockedPod{
			{Namespace: "shop", Name: "short-wait", PendingSeconds: i64p(30)},
			{Namespace: "shop", Name: "long-wait", PendingSeconds: i64p(900), PrimaryCategory: "INSUFFICIENT_CPU",
				Message: "0/3 nodes are available", VerifyCommand: "kubectl -n shop describe pod long-wait"},
		},
	}
	cap.BlockedSummary = &struct {
		Total             int            `json:"total"`
		Listed            int            `json:"listed"`
		CapacityShortfall int            `json:"capacityShortfall"`
		Gated             int            `json:"gated"`
		Nominated         int            `json:"nominated"`
		ByCategory        map[string]int `json:"byCategory"`
		EventsRead        bool           `json:"eventsRead"`
		EventsTruncated   bool           `json:"eventsTruncated"`
	}{Total: 2, Listed: 2, CapacityShortfall: 1, EventsRead: true}
	cap.Usage = &struct {
		Status              string `json:"status"`
		Reason              string `json:"reason"`
		NodesWithSamples    int    `json:"nodesWithSamples"`
		NodesWithoutSamples int    `json:"nodesWithoutSamples"`
	}{Status: "OK", NodesWithSamples: 2, NodesWithoutSamples: 1}
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"Capacity": cap}}

	out := open(t, c, "capacity").View()
	for _, want := range []string{"Capacity", "default", "41/110", "2 pending (1 short of capacity)", "1 node(s) without usage samples"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	m := open(t, c, "pending")
	m = key(m, "enter") // the first row is the longest-waiting pod
	for _, want := range []string{"shop/long-wait", "INSUFFICIENT_CPU", "0/3 nodes are available", "verify: kubectl -n shop describe pod long-wait"} {
		if !strings.Contains(m.rowPane, want) {
			t.Errorf("missing %q in pane:\n%s", want, m.rowPane)
		}
	}
}

func TestMaintainViewHidesPlatformAndOpensItem(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"MaintainItems": &api.MaintainList{
		ReadFailed: true,
		Items: []api.MaintainItem{
			{DisplayName: "Python 3.12", DaysLeft: intp(700)},
			{DisplayName: "Node.js 18", DaysLeft: intp(15), Location: "shop/Deployment/web", SourceURL: "https://endoflife.date/nodejs"},
			{DisplayName: "Kubernetes 1.31", DaysLeft: intp(5), Platform: true},
		},
	}}}
	m := open(t, c, "maintain")
	out := m.View()
	if strings.Contains(out, "Kubernetes 1.31") || !strings.Contains(out, "1 platform item(s) hidden") {
		t.Errorf("platform item shown, or its hiding not stated:\n%s", out)
	}
	if !strings.Contains(out, "this list is incomplete") {
		t.Errorf("readFailed not on screen:\n%s", out)
	}
	m = key(m, "enter") // soonest first: Node.js
	if !strings.Contains(m.rowPane, "Node.js 18") || !strings.Contains(m.rowPane, "endoflife.date") {
		t.Errorf("pane is not the soonest item:\n%s", m.rowPane)
	}
}

func TestUpgradeViewIsPerApplication(t *testing.T) {
	u := &api.UpgradeReadiness{CurrentVersion: "1.33", TargetVersion: "1.34", Score: 40, RiskLevel: "MEDIUM", Stale: true}
	u.AppImpact = &struct {
		Applications []struct {
			Namespace  string              `json:"namespace"`
			MustChange []api.AppImpactItem `json:"mustChange"`
			Affected   []api.AppImpactItem `json:"affected"`
		} `json:"applications"`
		PlatformFindings    int  `json:"platformFindings"`
		UnattributedObjects int  `json:"unattributedObjects"`
		NamespacesRead      bool `json:"namespacesRead"`
	}{NamespacesRead: true}
	u.AppImpact.Applications = append(u.AppImpact.Applications, struct {
		Namespace  string              `json:"namespace"`
		MustChange []api.AppImpactItem `json:"mustChange"`
		Affected   []api.AppImpactItem `json:"affected"`
	}{Namespace: "shop", MustChange: []api.AppImpactItem{{Title: "Remove flowcontrol v1beta3", Severity: "HIGH",
		Objects: []string{"FlowSchema/a"}, VerifyCommand: "kubectl get flowschemas"}}})
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"UpgradeReadiness": u}}

	m := open(t, c, "upgrade")
	out := m.View()
	for _, want := range []string{"Upgrade impact", "1.33 → 1.34", "STALE", "shop", "must change", "Remove flowcontrol v1beta3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	m = key(m, "enter")
	if !strings.Contains(m.rowPane, "verify: kubectl get flowschemas") || !strings.Contains(m.rowPane, "FlowSchema/a") {
		t.Errorf("pane missing the fix detail:\n%s", m.rowPane)
	}
}

// A resource view must still describe the workload, not a stale row pane.
func TestEnterOnAResourceRowStillDescribes(t *testing.T) {
	c := &fakeClient{rows: pods(), workspaces: map[string]any{"NodePools": karpenter("OK")}}
	m := open(t, c, "karpenter")
	m = key(m, ":")
	m = typeIn(m, "pods")
	m = submit(t, m)
	if len(m.data.rowText) != 0 {
		t.Fatalf("rowText leaked into a resource view")
	}
}
