package view

import (
	"sort"
	"strconv"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// Every add-on list table ends in the same two columns — how many findings the row carries
// and the worst one — so a reader scanning any workspace looks in the same place.

func findingCells(rs []api.AddonReason) (string, string) {
	w := WorstReason(rs)
	if w == nil {
		return "0", "<none>"
	}
	return strconv.Itoa(len(rs)), w.Severity + " " + truncate(w.Title, 60)
}

// ReasonsTable lists one row's findings, worst first, with the command that verifies each.
func ReasonsTable(rs []api.AddonReason) *render.Table {
	rows := sortReasons(rs)
	t := &render.Table{
		Headers:      []string{"SEVERITY", "RULE", "TITLE", "OBJECT", "VERIFY"},
		WideHeaders:  []string{"MESSAGE", "RECOMMENDATION"},
		EmptyMessage: "No findings.",
	}
	for _, r := range rows {
		obj := strings.TrimSpace(r.ObjectKind + " " + r.ObjectRef)
		t.Rows = append(t.Rows, []string{
			render.Dash(r.Severity), render.Dash(r.RuleID), render.Dash(truncate(r.Title, 70)),
			render.Dash(obj), render.Dash(r.VerifyCommand),
		})
		t.WideRows = append(t.WideRows, []string{render.Dash(r.Message), render.Dash(r.Recommendation)})
	}
	return t
}

// EventsTable lists a workspace detail's events, newest first.
func EventsTable(evs []api.WorkspaceEvent) *render.Table {
	rows := append([]api.WorkspaceEvent(nil), evs...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].At == nil || rows[j].At == nil {
			return rows[j].At == nil && rows[i].At != nil
		}
		return rows[i].At.After(*rows[j].At)
	})
	t := &render.Table{
		Headers:      []string{"AGE", "TYPE", "REASON", "OBJECT", "MESSAGE"},
		EmptyMessage: "No events.",
	}
	for _, e := range rows {
		t.Rows = append(t.Rows, []string{
			Since(e.At), render.Dash(e.Type), render.Dash(e.Reason),
			render.Dash(strings.TrimSpace(e.ObjectKind + "/" + e.ObjectName)), render.Dash(truncate(e.Message, 90)),
		})
	}
	return t
}

func NodePoolsTable(rows []api.NodePoolRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODEPOOL", "STATE", "NODES", "CLAIMS", "DRIFTED", "NOT READY", "FINDINGS", "WORST"},
		WideHeaders:  []string{"WEIGHT", "CONSOLIDATION", "DETAIL"},
		EmptyMessage: "No NodePools.",
	}
	for _, p := range rows {
		n, w := findingCells(p.Reasons)
		t.Rows = append(t.Rows, []string{p.Name, render.Dash(p.State), Int64(p.Nodes), strconv.Itoa(p.NodeClaimCount),
			strconv.Itoa(p.DriftedCount), strconv.Itoa(p.NotReadyNodeCount), n, w})
		t.WideRows = append(t.WideRows, []string{Int(p.Weight), render.Dash(p.ConsolidationPolicy), render.Dash(p.StateDetail)})
	}
	return t
}

func NodeClaimsTable(d *api.NodePoolDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODECLAIM", "PHASE", "TYPE", "CAPACITY", "ZONE", "NODE", "DRIFTED", "REASON"},
		EmptyMessage: "No NodeClaims.",
	}
	for _, c := range d.NodeClaims {
		drift := "no"
		if c.Drifted {
			drift = "yes"
		}
		t.Rows = append(t.Rows, []string{c.Name, render.Dash(c.Phase), render.Dash(c.InstanceType), render.Dash(c.CapacityType),
			render.Dash(c.Zone), render.Dash(c.NodeName), drift, render.Dash(c.PhaseReason)})
	}
	return t
}

func NodeGroupsTable(rows []api.NodeGroupRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODEGROUP", "STATE", "NODES", "TARGET", "MIN", "MAX", "FINDINGS", "WORST"},
		WideHeaders:  []string{"DETAIL"},
		EmptyMessage: "No node groups.",
	}
	for _, g := range rows {
		n, w := findingCells(g.Reasons)
		t.Rows = append(t.Rows, []string{g.Name, render.Dash(g.State), strconv.Itoa(g.NodeCount),
			Int(g.Target), Int(g.MinSize), Int(g.MaxSize), n, w})
		t.WideRows = append(t.WideRows, []string{render.Dash(g.Detail)})
	}
	return t
}

func NodeGroupNodesTable(d *api.NodeGroupDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODE", "READY", "TYPE", "ZONE", "SCALE-DOWN DISABLED"},
		EmptyMessage: "No nodes in this group.",
	}
	for _, n := range d.Nodes {
		sd := "no"
		if n.ScaleDownDisabled {
			sd = "yes"
		}
		t.Rows = append(t.Rows, []string{n.Name, Bool(n.Ready), render.Dash(n.InstanceType), render.Dash(n.Zone), sd})
	}
	return t
}

func IngressRoutesTable(rows []api.IngressRouteRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "HOSTS", "STATE", "FINDINGS", "WORST"},
		WideHeaders:  []string{"WHY"},
		EmptyMessage: "No routes.",
	}
	for _, r := range rows {
		var hosts []string
		for _, m := range r.Matches {
			hosts = append(hosts, m.Hosts...)
		}
		n, w := findingCells(r.Reasons)
		t.Rows = append(t.Rows, []string{r.Namespace, r.Kind, r.Name, render.Dash(truncate(strings.Join(dedupe(hosts), ","), 40)),
			render.Dash(r.State), n, w})
		t.WideRows = append(t.WideRows, []string{render.Dash(r.StateWhy)})
	}
	return t
}

func BackendWorkloadsTable(d *api.IngressRouteDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "SERVICE"},
		EmptyMessage: "No backend workloads resolved.",
	}
	for _, b := range d.BackendWorkloads {
		t.Rows = append(t.Rows, []string{b.Namespace, b.Kind, b.Name, render.Dash(b.Service)})
	}
	return t
}

func MeshWorkloadsTable(rows []api.MeshWorkloadRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "PROXY", "MESHED", "MTLS", "STATE", "FINDINGS", "WORST"},
		WideHeaders:  []string{"PROXY VERSION", "WHY"},
		EmptyMessage: "No workloads.",
	}
	for _, m := range rows {
		proxy, meshed, version, mtls := unknown, unknown, "<none>", unknown
		if p := m.Proxy; p != nil {
			proxy, meshed, version = render.Dash(p.Status), strconv.Itoa(p.PodsMeshed)+"/"+strconv.Itoa(p.Pods), render.Dash(p.Version)
		}
		if x := m.Mtls; x != nil && x.Known {
			mtls = render.Dash(x.Mode)
		}
		n, w := findingCells(m.Reasons)
		t.Rows = append(t.Rows, []string{m.Namespace, m.Kind, m.Name, proxy, meshed, mtls, render.Dash(m.State), n, w})
		t.WideRows = append(t.WideRows, []string{version, render.Dash(m.StateWhy)})
	}
	return t
}

func MeshPodsTable(d *api.MeshWorkloadDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"POD", "MESHED", "NATIVE SIDECAR", "VERSION", "REVISION", "OPTED OUT"},
		EmptyMessage: "No pods.",
	}
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	for _, p := range d.Pods {
		t.Rows = append(t.Rows, []string{p.Name, yn(p.Meshed), yn(p.NativeSidecar), render.Dash(p.Version),
			render.Dash(p.Revision), yn(p.OptedOut)})
	}
	return t
}

func GitOpsAppsTable(rows []api.GitOpsAppRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "APPLICATION", "PROJECT", "SYNC", "HEALTH", "STATE", "FINDINGS", "WORST"},
		WideHeaders:  []string{"OPERATION"},
		EmptyMessage: "No Applications.",
	}
	for _, a := range rows {
		n, w := findingCells(a.Reasons)
		t.Rows = append(t.Rows, []string{a.Namespace, a.Name, render.Dash(a.Project), render.Dash(a.Sync),
			render.Dash(a.Health), render.Dash(a.State), n, w})
		t.WideRows = append(t.WideRows, []string{render.Dash(a.OperationPhase)})
	}
	return t
}

func GitOpsWorkloadsTable(d *api.GitOpsAppDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"NAMESPACE", "KIND", "NAME", "READY", "TRACKED BY"},
		EmptyMessage: "No workloads deployed by this Application.",
	}
	for _, w := range d.Workloads {
		t.Rows = append(t.Rows, []string{w.Namespace, w.Kind, w.Name, Int(w.ReadyReplicas) + "/" + Int(w.DesiredReplicas),
			render.Dash(w.TrackedBy)})
	}
	return t
}

func VpcCniNodesTable(rows []api.VpcCniNodeRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODE", "STATE", "TYPE", "ZONE", "IPS ASSIGNED", "IPS USABLE", "ENIS", "FINDINGS", "WORST"},
		WideHeaders:  []string{"MAX PODS", "CNI RESTARTS", "DETAIL"},
		EmptyMessage: "No nodes.",
	}
	for _, n := range rows {
		f, w := findingCells(n.Reasons)
		t.Rows = append(t.Rows, []string{n.Name, render.Dash(n.State), render.Dash(n.InstanceType), render.Dash(n.Zone),
			Int(n.IPAssigned), Int(n.IPUsable) + "/" + Int(n.IPMax), Int(n.EnisAllocated) + "/" + Int(n.EnisMax), f, w})
		t.WideRows = append(t.WideRows, []string{Int(n.MaxPods), Int(n.CNIPodRestarts), render.Dash(n.StateDetail)})
	}
	return t
}

func VpcCniErrorsTable(d *api.VpcCniNodeDetail) *render.Table {
	t := &render.Table{
		Headers:      []string{"FAMILY", "ERROR", "LAST HOUR", "LAST DAY", "LAST SEEN", "DETAIL"},
		EmptyMessage: "No ipamd or AWS API errors recorded.",
	}
	for _, e := range d.Errors {
		t.Rows = append(t.Rows, []string{render.Dash(e.Family), render.Dash(e.Key), strconv.Itoa(e.LastHour),
			strconv.Itoa(e.LastDay), Since(e.LastSeen), render.Dash(truncate(e.Detail, 70))})
	}
	return t
}

func EbsCsiNodesTable(rows []api.EbsCsiNodeRow) *render.Table {
	t := &render.Table{
		Headers:      []string{"NODE", "STATE", "READY", "DRIVER", "ATTACHED", "FAILING", "FINDINGS", "WORST"},
		EmptyMessage: "No nodes.",
	}
	for _, n := range rows {
		f, w := findingCells(n.Reasons)
		t.Rows = append(t.Rows, []string{n.Name, render.Dash(n.State), Bool(n.NodeReady), Bool(n.DriverRegistered),
			Int(n.Attached) + "/" + Int(n.Limit), strconv.Itoa(n.FailingAttachments), f, w})
	}
	return t
}

func EbsCsiAttachmentsTable(rows []api.EbsCsiAttachment) *render.Table {
	t := &render.Table{
		Headers:      []string{"ATTACHMENT", "NODE", "VOLUME", "ATTACHED", "PROBLEM", "SINCE", "MESSAGE"},
		EmptyMessage: "No VolumeAttachments.",
	}
	for _, a := range rows {
		att := "no"
		if a.Attached {
			att = "yes"
		}
		t.Rows = append(t.Rows, []string{a.Name, render.Dash(a.Node), render.Dash(a.PersistentVolume), att,
			render.Dash(a.Problem), Since(a.Since), render.Dash(truncate(a.Message, 70))})
	}
	return t
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	out := s[:0:0]
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
