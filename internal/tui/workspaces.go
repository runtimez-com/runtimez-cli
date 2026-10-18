package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// Capacity, Maintain, upgrade readiness and the add-on workspaces. Every one of them uses the
// same view package tables as its flag command, and every one turns a status the backend could
// not read into the on-screen notice — an add-on that is not installed must never look like a
// healthy add-on with nothing to report.

// notReadable is the dataset for a non-OK status: the reason as the notice, and an empty
// state that repeats it instead of "No rows".
func notReadable(what, status, message string) dataset {
	msg := what + ": " + status
	if message = strings.TrimSpace(message); message != "" {
		msg += " — " + message
	}
	return dataset{table: &render.Table{EmptyMessage: msg}, notice: msg}
}

func statusOK(s string) bool { return s == "" || strings.EqualFold(s, "OK") }

func joinNotices(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

var capacityView = viewDef{
	Title: "Capacity",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.Capacity(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		c := &p.Typed
		if !statusOK(c.Status) && len(c.Pools) == 0 {
			return notReadable("capacity", c.Status, c.Message), nil
		}
		var notes []string
		for _, a := range c.Headroom {
			if a.Axis == "pods" {
				continue
			}
			notes = append(notes, a.Axis+" requests "+view.Pct(a.RequestsCommitmentPct))
		}
		if b := c.BlockedSummary; b != nil && b.Total > 0 {
			notes = append(notes, fmt.Sprintf("%d pending (%d short of capacity) — :pending", b.Total, b.CapacityShortfall))
		}
		if u := c.Usage; u != nil && u.NodesWithoutSamples > 0 {
			notes = append(notes, fmt.Sprintf("%d node(s) without usage samples", u.NodesWithoutSamples))
		}
		if f := c.Flags; f != nil && (f.NodeRowsTruncated || f.PodRowsTruncated) {
			notes = append(notes, "rows truncated — figures are a lower bound")
		}
		return dataset{table: view.PoolsTable(c.Pools), notice: joinNotices(notes...)}, nil
	},
}

var pendingView = viewDef{
	Title: "Pending pods",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.Capacity(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		c := &p.Typed
		if !statusOK(c.Status) && len(c.Blocked) == 0 {
			return notReadable("capacity", c.Status, c.Message), nil
		}
		d := dataset{table: view.BlockedTable(c)}
		for _, b := range view.SortBlocked(c) {
			d.rowText = append(d.rowText, view.BlockedText(b))
		}
		if b := c.BlockedSummary; b != nil {
			if b.Listed < b.Total {
				d.notice = fmt.Sprintf("%d of %d pending pods listed", b.Listed, b.Total)
			}
			if !b.EventsRead {
				d.notice = joinNotices(d.notice, "scheduler events not read — reasons may be missing")
			}
		}
		return d, nil
	},
}

var storageView = viewDef{
	Title: "Storage",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.Capacity(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		c := &p.Typed
		s := c.Storage
		if s == nil {
			return notReadable("storage", "NOT_REPORTED", "this cluster's capacity has no storage section"), nil
		}
		if !statusOK(s.Status) && len(s.PVCs) == 0 {
			return notReadable("storage", s.Status, s.Reason), nil
		}
		d := dataset{table: view.StorageTable(c)}
		if !statusOK(s.Metrics) {
			d.notice = "volume metrics " + s.Metrics + " — " + s.MetricsReason
		}
		if s.PVCsNotListed > 0 {
			d.notice = joinNotices(d.notice, fmt.Sprintf("%d more claims not listed", s.PVCsNotListed))
		}
		return d, nil
	},
}

// maintainView is org-wide, like the flag command, and hides platform items the same way:
// Kubernetes and add-on deadlines belong to :upgrade.
var maintainView = viewDef{
	Title: "Maintain",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.MaintainItems(ctx, r.OrgID)
		if err != nil {
			return dataset{}, err
		}
		var items []api.MaintainItem
		hidden := 0
		for _, it := range p.Typed.Items {
			if it.Platform {
				hidden++
				continue
			}
			items = append(items, it)
		}
		d := dataset{table: view.MaintainItemsTable(items)}
		for _, it := range view.SortMaintainItems(items) {
			d.rowText = append(d.rowText, view.MaintainItemText(it))
		}
		var notes []string
		if p.Typed.ReadFailed {
			notes = append(notes, "the backend could not read every item — this list is incomplete")
		}
		if p.Typed.Truncated {
			notes = append(notes, "list truncated")
		}
		if hidden > 0 {
			notes = append(notes, fmt.Sprintf("%d platform item(s) hidden — see :upgrade", hidden))
		}
		d.notice = joinNotices(notes...)
		return d, nil
	},
}

var upgradeView = viewDef{
	Title: "Upgrade impact",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.UpgradeReadiness(ctx, r.OrgID, r.ClusterID, "")
		if err != nil {
			return dataset{}, err
		}
		u := &p.Typed
		var notes []string
		notes = append(notes, fmt.Sprintf("%s → %s, score %d/100 %s", render.Dash(u.CurrentVersion),
			render.Dash(u.TargetVersion), u.Score, render.Dash(u.RiskLevel)))
		if n := len(u.AddonHops); n > 0 {
			notes = append(notes, fmt.Sprintf("%d add-on(s) must move", n))
		}
		if u.Stale {
			notes = append(notes, "built on STALE inventory")
		}
		if s := strings.ToUpper(u.ScanStatus); s == "PARTIAL" || s == "INSUFFICIENT_DATA" || s == "STALE" {
			notes = append(notes, "scan "+u.ScanStatus+" — blockers may be missing")
		}
		if u.AppImpact == nil {
			// An older backend: fall back to the findings list rather than an empty screen.
			notes = append(notes, "no per-application view from this backend — showing findings")
			return dataset{table: view.FindingsTable(u.Findings), notice: joinNotices(notes...)}, nil
		}
		if n := u.AppImpact.PlatformFindings; n > 0 {
			notes = append(notes, fmt.Sprintf("%d platform-only finding(s) — `rtz upgrade check`", n))
		}
		return dataset{table: view.AppImpactTable(u), rowText: view.AppImpactTexts(u), notice: joinNotices(notes...)}, nil
	},
}

// workspaceNotice is the line every add-on view carries: the add-on and version it read,
// and whether its rules ran.
func workspaceNotice(h api.WorkspaceHead, extra ...string) string {
	var parts []string
	if a := h.Addon; a != nil {
		parts = append(parts, strings.TrimSpace(a.DisplayName+" "+a.Version))
		if !a.RulesEvaluated {
			parts = append(parts, "rules NOT evaluated — an empty findings column is not \"no findings\"")
		}
	}
	return joinNotices(append(parts, extra...)...)
}

var nodePoolsView = viewDef{
	Title: "Karpenter NodePools",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.NodePools(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("Karpenter", l.Status, l.Message), nil
		}
		d := dataset{table: view.NodePoolsTable(l.NodePools)}
		for _, np := range l.NodePools {
			d.rowText = append(d.rowText, view.ReasonsText("NodePool "+np.Name, np.State, np.StateDetail, np.Reasons))
		}
		managed := ""
		if h := l.Header; h != nil && h.ManagedBy != "" {
			managed = "managed by " + h.ManagedBy
		}
		d.notice = workspaceNotice(l.WorkspaceHead, managed)
		return d, nil
	},
}

var nodeGroupsView = viewDef{
	Title: "Cluster Autoscaler",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.NodeGroups(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("Cluster Autoscaler", l.Status, l.Message), nil
		}
		d := dataset{table: view.NodeGroupsTable(l.NodeGroups)}
		for _, g := range l.NodeGroups {
			d.rowText = append(d.rowText, view.ReasonsText("Node group "+g.Name, g.State, g.Detail, g.Reasons))
		}
		var extra []string
		if h := l.Header; h != nil {
			extra = append(extra, "status "+render.Dash(h.AutoscalerStatus))
			if h.StatusStale {
				extra = append(extra, "status ConfigMap is STALE — CA may not be running")
			}
			if n := len(h.Reasons); n > 0 {
				extra = append(extra, fmt.Sprintf("%d cluster-wide finding(s) — `rtz addons ca`", n))
			}
		}
		d.notice = workspaceNotice(l.WorkspaceHead, extra...)
		return d, nil
	},
}

var routesView = viewDef{
	Title: "Ingress routes",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.IngressRoutes(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("ingress", l.Status, l.Message), nil
		}
		d := dataset{table: view.IngressRoutesTable(l.Routes), notice: workspaceNotice(l.WorkspaceHead)}
		for _, rt := range l.Routes {
			d.rowText = append(d.rowText, view.ReasonsText(rt.Kind+" "+rt.Namespace+"/"+rt.Name, rt.State, rt.StateWhy, rt.Reasons))
		}
		return d, nil
	},
}

var meshView = viewDef{
	Title: "Mesh workloads",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.MeshWorkloads(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("mesh", l.Status, l.Message), nil
		}
		d := dataset{table: view.MeshWorkloadsTable(l.Workloads), notice: workspaceNotice(l.WorkspaceHead)}
		for _, w := range l.Workloads {
			d.rowText = append(d.rowText, view.ReasonsText(w.Kind+" "+w.Namespace+"/"+w.Name, w.State, w.StateWhy, w.Reasons))
		}
		return d, nil
	},
}

var gitopsView = viewDef{
	Title: "Argo CD Applications",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.GitOpsApps(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("Argo CD", l.Status, l.Message), nil
		}
		d := dataset{table: view.GitOpsAppsTable(l.Applications), notice: workspaceNotice(l.WorkspaceHead)}
		for _, a := range l.Applications {
			why := "sync " + render.Dash(a.Sync) + ", health " + render.Dash(a.Health)
			d.rowText = append(d.rowText, view.ReasonsText("Application "+a.Namespace+"/"+a.Name, a.State, why, a.Reasons))
		}
		return d, nil
	},
}

var vpcCniView = viewDef{
	Title: "Amazon VPC CNI",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.VpcCni(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("VPC CNI", l.Status, l.Message), nil
		}
		d := dataset{table: view.VpcCniNodesTable(l.Nodes)}
		for _, n := range l.Nodes {
			d.rowText = append(d.rowText, view.ReasonsText("Node "+n.Name, n.State, n.StateDetail, n.Reasons))
		}
		var extra []string
		if h := l.Header; h != nil {
			extra = append(extra, "network policy "+render.Dash(h.NetworkPolicyEnforcement))
			if n := len(h.Reasons); n > 0 {
				extra = append(extra, fmt.Sprintf("%d cluster-wide finding(s) — `rtz addons vpc-cni`", n))
			}
		}
		d.notice = workspaceNotice(l.WorkspaceHead, extra...)
		return d, nil
	},
}

var ebsCsiView = viewDef{
	Title: "Amazon EBS CSI",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.EbsCsi(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		l := &p.Typed
		if !l.OK() {
			return notReadable("EBS CSI", l.Status, l.Message), nil
		}
		d := dataset{table: view.EbsCsiNodesTable(l.Nodes)}
		for _, n := range l.Nodes {
			d.rowText = append(d.rowText, view.ReasonsText("Node "+n.Name, n.State, "", n.Reasons))
		}
		failing := 0
		for _, a := range l.Attachments {
			if !a.Attached || a.Problem != "" {
				failing++
			}
		}
		var extra []string
		if failing > 0 {
			extra = append(extra, fmt.Sprintf("%d failing attachment(s)", failing))
		}
		if n := len(l.Workloads); n > 0 {
			extra = append(extra, fmt.Sprintf("%d workload(s) stuck on EBS", n))
		}
		if n := len(l.ClusterReasons); n > 0 {
			extra = append(extra, fmt.Sprintf("%d cluster-wide finding(s)", n))
		}
		if len(extra) > 0 {
			extra = append(extra, "`rtz addons ebs-csi` for the full list")
		}
		d.notice = workspaceNotice(l.WorkspaceHead, extra...)
		return d, nil
	},
}
