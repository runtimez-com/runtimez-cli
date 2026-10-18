package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newAddonsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "addons",
		Aliases: []string{"addon"},
		Short:   "Add-on workspaces: Karpenter, Cluster Autoscaler, Traefik, Istio, Argo CD, VPC CNI, EBS CSI",
		Long: `Each add-on workspace lists its objects with their state and the rule findings behind it.
Pass an object to see its findings, the command that verifies each, and its detail.

--fail-on exits 4 when any listed finding reaches that severity.`,
	}
	cmd.AddCommand(
		addonCmd(addonSpec[api.NodePools, api.NodePoolDetail]{
			use: "nodepools [name]", aliases: []string{"karpenter", "np"}, what: "Karpenter NodePools",
			list: (*api.Client).NodePools,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, _ string) (*api.Payload[api.NodePoolDetail], error) {
				return c.NodePool(ctx, org, cl, a[0])
			},
			head: func(l *api.NodePools) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.NodePools) ([]api.AddonReason, error) {
				if h := l.Header; h != nil && h.ManagedBy != "" {
					fmt.Fprintf(w, "  Managed by %s\n", h.ManagedBy)
				}
				var rs []api.AddonReason
				for _, p := range l.NodePools {
					rs = append(rs, p.Reasons...)
				}
				return rs, printSection(e, w, "NodePools", view.NodePoolsTable(l.NodePools))
			},
			show: func(e *env, w io.Writer, d *api.NodePoolDetail) error {
				if d.NodePool == nil {
					return nil
				}
				stateLine(w, "NodePool "+d.NodePool.Name, d.NodePool.State, d.NodePool.StateDetail)
				return detailSections(e, w, d.NodePool.Reasons,
					section{"NodeClaims", view.NodeClaimsTable(d)},
					eventsSection(d.Events, d.EventsAvailable))
			},
		}),
		addonCmd(addonSpec[api.NodeGroups, api.NodeGroupDetail]{
			use: "cluster-autoscaler [nodegroup]", aliases: []string{"ca"}, what: "Cluster Autoscaler node groups",
			list: (*api.Client).NodeGroups,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, _ string) (*api.Payload[api.NodeGroupDetail], error) {
				return c.NodeGroup(ctx, org, cl, a[0])
			},
			head: func(l *api.NodeGroups) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.NodeGroups) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				if h := l.Header; h != nil {
					fmt.Fprintf(w, "  Autoscaler status: %s\n", render.Dash(h.AutoscalerStatus))
					if h.StatusStale {
						fmt.Fprintln(w, "  (the status ConfigMap is stale — CA may not be running)")
					}
					rs = append(rs, h.Reasons...)
					if len(h.Reasons) > 0 {
						if err := printSection(e, w, "Cluster-wide findings", view.ReasonsTable(h.Reasons)); err != nil {
							return nil, err
						}
					}
				}
				for _, g := range l.NodeGroups {
					rs = append(rs, g.Reasons...)
				}
				return rs, printSection(e, w, "Node groups", view.NodeGroupsTable(l.NodeGroups))
			},
			show: func(e *env, w io.Writer, d *api.NodeGroupDetail) error {
				if d.NodeGroup == nil {
					return nil
				}
				stateLine(w, "Node group "+d.NodeGroup.Name, d.NodeGroup.State, d.NodeGroup.Detail)
				return detailSections(e, w, d.NodeGroup.Reasons,
					section{"Nodes", view.NodeGroupNodesTable(d)},
					eventsSection(d.Events, d.EventsAvailable))
			},
		}),
		addonCmd(addonSpec[api.IngressRoutes, api.IngressRouteDetail]{
			use: "ingress [namespace/name]", aliases: []string{"traefik", "routes"}, what: "Traefik ingress routes", kindFlag: true,
			list: (*api.Client).IngressRoutes,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, kind string) (*api.Payload[api.IngressRouteDetail], error) {
				return c.IngressRoute(ctx, org, cl, a[0], a[1], kind)
			},
			head: func(l *api.IngressRoutes) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.IngressRoutes) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				for _, r := range l.Routes {
					rs = append(rs, r.Reasons...)
				}
				return rs, printSection(e, w, "Routes", view.IngressRoutesTable(l.Routes))
			},
			show: func(e *env, w io.Writer, d *api.IngressRouteDetail) error {
				if d.Route == nil {
					return nil
				}
				stateLine(w, d.Route.Kind+" "+d.Route.Namespace+"/"+d.Route.Name, d.Route.State, d.Route.StateWhy)
				return detailSections(e, w, d.Route.Reasons, section{"Backend workloads", view.BackendWorkloadsTable(d)})
			},
		}),
		addonCmd(addonSpec[api.MeshWorkloads, api.MeshWorkloadDetail]{
			use: "mesh [namespace/name]", aliases: []string{"istio"}, what: "Istio mesh workloads", kindFlag: true,
			list: (*api.Client).MeshWorkloads,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, kind string) (*api.Payload[api.MeshWorkloadDetail], error) {
				return c.MeshWorkload(ctx, org, cl, a[0], a[1], kind)
			},
			head: func(l *api.MeshWorkloads) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.MeshWorkloads) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				for _, m := range l.Workloads {
					rs = append(rs, m.Reasons...)
				}
				return rs, printSection(e, w, "Workloads", view.MeshWorkloadsTable(l.Workloads))
			},
			show: func(e *env, w io.Writer, d *api.MeshWorkloadDetail) error {
				if d.Workload == nil {
					return nil
				}
				stateLine(w, d.Workload.Kind+" "+d.Workload.Namespace+"/"+d.Workload.Name, d.Workload.State, d.Workload.StateWhy)
				return detailSections(e, w, d.Workload.Reasons, section{"Pods", view.MeshPodsTable(d)})
			},
		}),
		addonCmd(addonSpec[api.GitOpsApps, api.GitOpsAppDetail]{
			use: "gitops [namespace/name]", aliases: []string{"argocd", "apps"}, what: "Argo CD Applications",
			list: (*api.Client).GitOpsApps,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, _ string) (*api.Payload[api.GitOpsAppDetail], error) {
				return c.GitOpsApp(ctx, org, cl, a[0], a[1])
			},
			head: func(l *api.GitOpsApps) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.GitOpsApps) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				for _, a := range l.Applications {
					rs = append(rs, a.Reasons...)
				}
				return rs, printSection(e, w, "Applications", view.GitOpsAppsTable(l.Applications))
			},
			show: func(e *env, w io.Writer, d *api.GitOpsAppDetail) error {
				if d.Application == nil {
					return nil
				}
				a := d.Application
				stateLine(w, "Application "+a.Namespace+"/"+a.Name, a.State, "sync "+render.Dash(a.Sync)+", health "+render.Dash(a.Health))
				return detailSections(e, w, a.Reasons, section{"Workloads", view.GitOpsWorkloadsTable(d)})
			},
		}),
		addonCmd(addonSpec[api.VpcCni, api.VpcCniNodeDetail]{
			use: "vpc-cni [node]", aliases: []string{"cni"}, what: "Amazon VPC CNI nodes",
			list: (*api.Client).VpcCni,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, _ string) (*api.Payload[api.VpcCniNodeDetail], error) {
				return c.VpcCniNode(ctx, org, cl, a[0])
			},
			head: func(l *api.VpcCni) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.VpcCni) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				if h := l.Header; h != nil {
					fmt.Fprintf(w, "  CNI %s · network policy %s\n", render.Dash(h.CNIVersion), render.Dash(h.NetworkPolicyEnforcement))
					rs = append(rs, h.Reasons...)
					if len(h.Reasons) > 0 {
						if err := printSection(e, w, "Cluster-wide findings", view.ReasonsTable(h.Reasons)); err != nil {
							return nil, err
						}
					}
				}
				for _, n := range l.Nodes {
					rs = append(rs, n.Reasons...)
				}
				return rs, printSection(e, w, "Nodes", view.VpcCniNodesTable(l.Nodes))
			},
			show: func(e *env, w io.Writer, d *api.VpcCniNodeDetail) error {
				if d.Node == nil {
					return nil
				}
				stateLine(w, "Node "+d.Node.Name, d.Node.State, d.Node.StateDetail)
				return detailSections(e, w, d.Node.Reasons, section{"Errors", view.VpcCniErrorsTable(d)})
			},
		}),
		addonCmd(addonSpec[api.EbsCsi, api.EbsCsiNodeDetail]{
			use: "ebs-csi [node]", aliases: []string{"ebs"}, what: "Amazon EBS CSI nodes",
			list: (*api.Client).EbsCsi,
			detail: func(c *api.Client, ctx context.Context, org, cl string, a []string, _ string) (*api.Payload[api.EbsCsiNodeDetail], error) {
				return c.EbsCsiNode(ctx, org, cl, a[0])
			},
			head: func(l *api.EbsCsi) api.WorkspaceHead { return l.WorkspaceHead },
			rows: func(e *env, w io.Writer, l *api.EbsCsi) ([]api.AddonReason, error) {
				var rs []api.AddonReason
				if h := l.Header; h != nil {
					fmt.Fprintf(w, "  Driver %s · controller %s · node plugin %s\n", render.Dash(h.DriverVersion),
						render.Dash(h.ControllerReady), render.Dash(h.NodePluginReady))
				}
				rs = append(rs, l.ClusterReasons...)
				if len(l.ClusterReasons) > 0 {
					if err := printSection(e, w, "Cluster-wide findings", view.ReasonsTable(l.ClusterReasons)); err != nil {
						return nil, err
					}
				}
				for _, n := range l.Nodes {
					rs = append(rs, n.Reasons...)
				}
				if err := printSection(e, w, "Nodes", view.EbsCsiNodesTable(l.Nodes)); err != nil {
					return nil, err
				}
				var failing []api.EbsCsiAttachment
				for _, a := range l.Attachments {
					rs = append(rs, a.Reasons...)
					if !a.Attached || a.Problem != "" {
						failing = append(failing, a)
					}
				}
				if err := printSection(e, w, "Failing attachments", view.EbsCsiAttachmentsTable(failing)); err != nil {
					return nil, err
				}
				var wr []api.AddonReason
				for _, wl := range l.Workloads {
					for _, r := range wl.Reasons {
						if r.ObjectRef == "" {
							r.ObjectKind, r.ObjectRef = wl.Kind, wl.Namespace+"/"+wl.Name
						}
						wr = append(wr, r)
					}
				}
				rs = append(rs, wr...)
				return rs, printSection(e, w, "Workloads stuck on EBS", view.ReasonsTable(wr))
			},
			show: func(e *env, w io.Writer, d *api.EbsCsiNodeDetail) error {
				if d.Node == nil {
					return nil
				}
				stateLine(w, "Node "+d.Node.Name, d.Node.State, "")
				return detailSections(e, w, d.Node.Reasons, section{"Attachments", view.EbsCsiAttachmentsTable(d.Attachments)})
			},
		}),
	)
	return cmd
}

// addonSpec is one workspace: L is its list response, D its detail response.
type addonSpec[L, D any] struct {
	use, what string
	aliases   []string
	kindFlag  bool
	list      func(*api.Client, context.Context, string, string) (*api.Payload[L], error)
	detail    func(c *api.Client, ctx context.Context, org, cluster string, args []string, kind string) (*api.Payload[D], error)
	head      func(*L) api.WorkspaceHead
	rows      func(*env, io.Writer, *L) ([]api.AddonReason, error)
	show      func(*env, io.Writer, *D) error
}

// detailHead reads the status fields every detail response embeds.
func detailHead(d any) api.WorkspaceHead {
	switch v := d.(type) {
	case *api.NodePoolDetail:
		return v.WorkspaceHead
	case *api.NodeGroupDetail:
		return v.WorkspaceHead
	case *api.IngressRouteDetail:
		return v.WorkspaceHead
	case *api.MeshWorkloadDetail:
		return v.WorkspaceHead
	case *api.GitOpsAppDetail:
		return v.WorkspaceHead
	case *api.VpcCniNodeDetail:
		return v.WorkspaceHead
	case *api.EbsCsiNodeDetail:
		return v.WorkspaceHead
	}
	return api.WorkspaceHead{}
}

func addonCmd[L, D any](s addonSpec[L, D]) *cobra.Command {
	var failOn, kind string
	nsName := strings.Contains(s.use, "namespace/name")

	cmd := &cobra.Command{
		Use:     s.use,
		Aliases: s.aliases,
		Short:   s.what + ", with their findings",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			threshold, err := failOnThreshold(failOn)
			if err != nil {
				return err
			}
			var ref []string
			if len(args) == 1 {
				if nsName {
					if ref, err = splitRef(args[0], 2, "<namespace>/<name>"); err != nil {
						return err
					}
				} else {
					ref = args
				}
			}
			e, orgID, clusterID, err := clusterEnv(cmd)
			if err != nil {
				return err
			}
			out, warn := cmd.OutOrStdout(), cmd.ErrOrStderr()

			if ref != nil {
				p, err := s.detail(e.client, cmd.Context(), orgID, clusterID, ref, kind)
				if err != nil {
					return err
				}
				if e.printer.Format.Structured() {
					return e.printer.Print(p.Raw, nil)
				}
				h := detailHead(&p.Typed)
				if !h.OK() {
					warnStatus(warn, s.what, h.Status, h.Message)
					return nil
				}
				return s.show(e, out, &p.Typed)
			}

			p, err := s.list(e.client, cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			h := s.head(&p.Typed)
			if e.printer.Format.Structured() {
				if err := e.printer.Print(p.Raw, nil); err != nil {
					return err
				}
				if !h.OK() {
					return nil
				}
				var discard strings.Builder
				rs, err := s.rows(&env{printer: &render.Printer{Out: &discard, Format: render.FormatTable}}, &discard, &p.Typed)
				if err != nil {
					return err
				}
				return gate(threshold, severities(rs), "add-on findings")
			}
			addonHeader(out, h, s.what)
			if !h.OK() {
				// Not installed, never synced, rules off: say so instead of an empty table, and
				// keep --fail-on from passing a cluster nobody could read.
				warnStatus(warn, s.what, h.Status, h.Message)
				if threshold > 0 {
					fmt.Fprintf(warn, "warning: %s were not evaluated — --fail-on has nothing to gate\n", s.what)
				}
				return nil
			}
			if h.Addon != nil && !h.Addon.RulesEvaluated {
				fmt.Fprintf(warn, "warning: %s rules were not evaluated — an empty findings column is not \"no findings\"\n", h.Addon.DisplayName)
			}
			rs, err := s.rows(e, out, &p.Typed)
			if err != nil {
				return err
			}
			return gate(threshold, severities(rs), "add-on findings")
		},
	}
	cmd.Flags().StringVar(&failOn, "fail-on", "", "exit 4 when a listed finding reaches this severity")
	if s.kindFlag {
		cmd.Flags().StringVar(&kind, "kind", "", "object kind when a namespace/name is ambiguous")
	}
	return cmd
}

func addonHeader(w io.Writer, h api.WorkspaceHead, what string) {
	name := what
	if a := h.Addon; a != nil && a.DisplayName != "" {
		name = a.DisplayName
		if a.Version != "" {
			name += " " + a.Version
		}
		if a.RuleCount > 0 {
			name += " · " + strconv.Itoa(a.RuleCount) + " rules"
		}
	}
	fmt.Fprintf(w, "%s (synced %s ago)\n", name, view.Since(h.LastSyncAt))
	if len(h.Counts) > 0 {
		fmt.Fprintf(w, "  %s\n", view.Counts(h.Counts))
	}
}

func severities(rs []api.AddonReason) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Severity)
	}
	return out
}

func stateLine(w io.Writer, what, state, why string) {
	fmt.Fprintf(w, "%s: %s", what, render.Dash(state))
	if why = strings.TrimSpace(why); why != "" {
		fmt.Fprintf(w, " — %s", why)
	}
	fmt.Fprintln(w)
}

type section struct {
	title string
	table *render.Table
}

func eventsSection(evs []api.WorkspaceEvent, available bool) section {
	t := view.EventsTable(evs)
	if !available {
		t.EmptyMessage = "Events were not available for this sync."
	}
	return section{"Events", t}
}

func detailSections(e *env, w io.Writer, reasons []api.AddonReason, more ...section) error {
	if err := printSection(e, w, "Findings", view.ReasonsTable(reasons)); err != nil {
		return err
	}
	for _, s := range more {
		if err := printSection(e, w, s.title, s.table); err != nil {
			return err
		}
	}
	return nil
}
