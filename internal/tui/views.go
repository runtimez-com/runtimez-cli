package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/k8s"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/view"
)

// Client is the slice of the API this UI needs. Narrow on purpose: it keeps the model
// testable without a live backend, and makes the dependency explicit rather than ambient.
type Client interface {
	Resources(ctx context.Context, orgID, clusterID, kind, namespace string) ([]k8s.Resource, error)
	Detail(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*api.WorkloadDetail, error)
	WorkloadRiskPosture(ctx context.Context, orgID, clusterID string) (*api.WorkloadRisk, error)
	SearchLogs(ctx context.Context, req api.LogSearch) (*api.LogPage, error)
	ListTraces(ctx context.Context, req api.TraceListRequest) (*api.TracePage, error)
	AutoRCA(ctx context.Context, orgID, clusterID, from, to string) (*api.AutoRCA, error)
	SlowOperations(ctx context.Context, orgID, clusterID, from, to string, limit int) (*api.SlowOperations, error)
	Changes(ctx context.Context, orgID, clusterID string, sinceHours, page, size int) (*api.Changes, error)
	WorkloadRCA(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*api.WorkloadRCA, error)
	WorkloadChanges(ctx context.Context, orgID, clusterID, namespace, name string, page, size int) (*api.Changes, error)
	Ask(ctx context.Context, orgID, clusterID string, q api.AgentQuery) (*api.AgentAnswer, error)
	StreamInvestigation(ctx context.Context, orgID, clusterID, streamID string, fn func(api.StreamFrame) error) error
	CancelInvestigation(ctx context.Context, orgID, clusterID, streamID string) error

	// Capacity, Maintain, upgrade readiness and the add-on workspaces.
	Capacity(ctx context.Context, orgID, clusterID string) (*api.Payload[api.Capacity], error)
	MaintainItems(ctx context.Context, orgID string) (*api.Payload[api.MaintainList], error)
	UpgradeReadiness(ctx context.Context, orgID, clusterID, targetVersion string) (*api.Payload[api.UpgradeReadiness], error)
	NodePools(ctx context.Context, orgID, clusterID string) (*api.Payload[api.NodePools], error)
	NodeGroups(ctx context.Context, orgID, clusterID string) (*api.Payload[api.NodeGroups], error)
	IngressRoutes(ctx context.Context, orgID, clusterID string) (*api.Payload[api.IngressRoutes], error)
	MeshWorkloads(ctx context.Context, orgID, clusterID string) (*api.Payload[api.MeshWorkloads], error)
	GitOpsApps(ctx context.Context, orgID, clusterID string) (*api.Payload[api.GitOpsApps], error)
	VpcCni(ctx context.Context, orgID, clusterID string) (*api.Payload[api.VpcCni], error)
	EbsCsi(ctx context.Context, orgID, clusterID string) (*api.Payload[api.EbsCsi], error)
}

// scope is the object the screen is currently about.
//
// Without it a drill-down key can only ever mean "the whole cluster", which is the
// difference between a viewer and something you can troubleshoot with: you find the
// crashing pod, then have to leave and retype its name somewhere else.
type scope struct {
	Kind      string
	Namespace string
	Name      string
	// Service is the name this workload's telemetry is indexed under — usually the
	// controller name rather than the pod's.
	Service string
}

func (s scope) empty() bool { return s.Name == "" }

func (s scope) ref() string {
	if s.Namespace == "" {
		return s.Name
	}
	return s.Namespace + "/" + s.Name
}

// scopeOf builds a scope from a selected row.
func scopeOf(r k8s.Resource) scope {
	return scope{
		Kind: r.Kind, Namespace: r.Namespace, Name: r.Name,
		Service: k8s.ServiceNameFor(r),
	}
}

// fetchReq is everything a view needs to load itself.
type fetchReq struct {
	Client    Client
	OrgID     string
	ClusterID string
	Namespace string
	Scope     scope
}

// dataset is what a view produces: the table to draw, plus the Kubernetes rows behind it
// when there are any. Reliability views have no resource rows, which is exactly why the
// model cannot assume every screen is a resource listing.
type dataset struct {
	table     *render.Table
	resources []k8s.Resource
	// notice is a caveat that belongs on screen with the data — missing signals, an
	// unscanned cluster, a listing that is cluster-wide when you expected it scoped —
	// rather than in a log the operator never sees.
	notice string
	// body replaces the table for screens that are prose rather than rows.
	body string
	// rowText is one pre-rendered detail per table row (same index), for screens whose rows
	// are not Kubernetes resources but still have something to open — an add-on object's
	// findings, a Maintain item, a pending pod. Built from the list response: no request.
	rowText []string
}

// viewDef is one navigable screen.
type viewDef struct {
	Title string
	// Kind is the canonical Kubernetes kind for resource views; empty for the rest.
	Kind string
	// namespaced marks views that honour the :ns scope.
	namespaced bool
	// scopable marks views that change when a workload is selected. A view that is not
	// scopable keeps the scope in the header but does not pretend to narrow by it.
	scopable bool
	fetch    func(ctx context.Context, r fetchReq) (dataset, error)
}

// resourceView builds a standard Kubernetes listing.
func resourceView(title, kind string) viewDef {
	return viewDef{
		Title: title, Kind: kind, namespaced: true,
		fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
			rows, err := r.Client.Resources(ctx, r.OrgID, r.ClusterID, kind, r.Namespace)
			if err != nil {
				return dataset{}, err
			}
			return dataset{table: view.TableFor(kind, rows, r.Namespace == ""), resources: rows}, nil
		},
	}
}

var riskView = viewDef{
	Title: "Workload risk",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		p, err := r.Client.WorkloadRiskPosture(ctx, r.OrgID, r.ClusterID)
		if err != nil {
			return dataset{}, err
		}
		d := dataset{table: view.RiskTable(p.Workloads)}
		if missing := p.MissingSignals(); len(missing) > 0 {
			d.notice = "scored without " + joinWords(missing) + " — a low score is not a clean bill of health"
		}
		return d, nil
	},
}

var signalsView = viewDef{
	Title: "Slow operations",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		res, err := r.Client.SlowOperations(ctx, r.OrgID, r.ClusterID, "", "", 50)
		if err != nil {
			return dataset{}, err
		}
		return dataset{table: view.SlowOperationsTable(res.Items)}, nil
	},
}

var logsView = viewDef{
	Title: "Logs", scopable: true,
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		end := time.Now()
		req := api.LogSearch{
			StartDate: api.EpochMs(end.Add(-time.Hour)), EndDate: api.EpochMs(end),
			ClusterID: r.ClusterID, Limit: 200,
		}
		req.ServiceName = r.Scope.Service

		page, err := r.Client.SearchLogs(ctx, req)
		if err != nil {
			return dataset{}, err
		}
		d := dataset{table: view.LogsTable(page.Data), notice: logScopeNotice(r.Scope)}
		if page.Total > int64(len(page.Data)) {
			d.notice += " — showing " + itoa(len(page.Data)) + " of " + itoa64(page.Total)
		}
		return d, nil
	},
}

// logScopeNotice says out loud whether the lines on screen belong to the selected workload
// or to the whole cluster. Mistaking one for the other is how an operator concludes a
// workload is silent when it is only unselected.
func logScopeNotice(s scope) string {
	if s.Service == "" {
		return "last hour, all services"
	}
	return "last hour, service " + s.Service
}

var tracesView = viewDef{
	Title: "Traces", scopable: true,
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		end := time.Now()
		req := api.TraceListRequest{
			StartDate: api.EpochMs(end.Add(-time.Hour)), EndDate: api.EpochMs(end),
			ClusterID: r.ClusterID, Limit: 100, SortBy: "durationNano", SortOrder: "desc",
		}
		notice := "last hour, slowest first"
		if svc := r.Scope.Service; svc != "" {
			// KQL, per KqlTranslator: serviceName is an allow-listed column and the value is
			// quoted. An untranslatable query is a 400, not a silent full-window match.
			req.Query = fmt.Sprintf("serviceName : %q", svc)
			notice += ", service " + svc
		}
		page, err := r.Client.ListTraces(ctx, req)
		if err != nil {
			return dataset{}, err
		}
		return dataset{table: view.TracesTable(page.Data), notice: notice}, nil
	},
}

var changesView = viewDef{
	Title: "Changes", scopable: true,
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		if !r.Scope.empty() {
			res, err := r.Client.WorkloadChanges(ctx, r.OrgID, r.ClusterID,
				r.Scope.Namespace, r.Scope.Name, 0, 100)
			if err != nil {
				return dataset{}, err
			}
			return dataset{
				table:  view.ChangesTable(res.Items),
				notice: "changes to " + r.Scope.ref(),
			}, nil
		}
		res, err := r.Client.Changes(ctx, r.OrgID, r.ClusterID, 168, 0, 100)
		if err != nil {
			return dataset{}, err
		}
		d := dataset{table: view.ChangesTable(res.Items), notice: "cluster-wide, last 7 days"}
		if res.Total > int64(len(res.Items)) {
			d.notice += " — showing the most recent " + itoa(len(res.Items)) + " of " + itoa64(res.Total)
		}
		return d, nil
	},
}

// rcaView is the per-workload root cause. It needs a subject: an RCA with no workload is
// not a smaller answer, it is a different question, which is what :triage is for.
var rcaView = viewDef{
	Title: "Root cause", scopable: true,
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		if r.Scope.empty() {
			return dataset{
				table: &render.Table{EmptyMessage: "Select a workload first — press r on a row, or :triage for what is failing now."},
			}, nil
		}
		res, err := r.Client.WorkloadRCA(ctx, r.OrgID, r.ClusterID,
			r.Scope.Namespace, r.Scope.Name, r.Scope.Kind)
		if err != nil {
			return dataset{}, err
		}
		return dataset{
			body:   view.WorkloadRCAText(res),
			notice: "root cause for " + r.Scope.ref(),
		}, nil
	},
}

// triageView answers "what do I look at first" instead of "what exists". It merges the
// auto-RCA suspect with every pod that is not healthy right now, because a control plane
// that has not yet scored a workload should not hide a pod that is crash-looping.
var triageView = viewDef{
	Title: "Triage",
	fetch: func(ctx context.Context, r fetchReq) (dataset, error) {
		pods, podErr := r.Client.Resources(ctx, r.OrgID, r.ClusterID, "Pod", "")
		rca, rcaErr := r.Client.AutoRCA(ctx, r.OrgID, r.ClusterID, "", "")

		// Neither source is required: a cluster with no RCA still has unhealthy pods worth
		// showing, and vice versa. Only a total failure is an error.
		if podErr != nil && rcaErr != nil {
			return dataset{}, podErr
		}

		t := &render.Table{
			Headers:      []string{"WHY", "NAMESPACE", "NAME", "STATUS", "RESTARTS", "AGE"},
			EmptyMessage: "Nothing is failing right now — every pod is Running or Succeeded.",
		}
		var rows []k8s.Resource
		var notes []string

		if rcaErr != nil {
			notes = append(notes, "auto-RCA unavailable")
		} else if rca != nil && rca.Degraded && rca.TopSuspect != nil {
			s := rca.TopSuspect
			t.Rows = append(t.Rows, []string{
				"top suspect", s.Namespace, s.Name,
				fmt.Sprintf("%s (%d)", s.Level, s.Score), "-", "-",
			})
			rows = append(rows, k8s.Resource{
				Kind: s.Kind, Namespace: s.Namespace, Name: s.Name,
			})
		}

		if podErr != nil {
			notes = append(notes, "pod listing unavailable")
		}
		for _, p := range unhealthyPods(pods) {
			d := k8s.ParsePod(p)
			t.Rows = append(t.Rows, []string{
				triageReason(d), d.Namespace, d.Name, d.Phase,
				itoa(d.Restarts), k8s.Age(d.CreatedAt),
			})
			rows = append(rows, p)
		}

		out := dataset{table: t, resources: rows}
		if len(notes) > 0 {
			// A partial answer said to be complete is worse than no answer.
			out.notice = "partial: " + strings.Join(notes, ", ")
		}
		return out, nil
	},
}

// unhealthyPods keeps anything an operator would stop on. Succeeded is excluded — a
// completed Job pod is not a problem — but Pending is included, because a pod that cannot
// schedule looks calm and is not.
func unhealthyPods(pods []k8s.Resource) []k8s.Resource {
	var out []k8s.Resource
	for _, p := range pods {
		d := k8s.ParsePod(p)
		switch d.Phase {
		case "Succeeded", "Completed":
			continue
		case "Running":
			// Restarts are the signal a Running pod is not actually well.
			if d.Restarts > 0 {
				out = append(out, p)
			}
		default:
			out = append(out, p)
		}
	}
	return out
}

func triageReason(d k8s.Pod) string {
	switch {
	case d.Phase == "Running" && d.Restarts > 0:
		return "restarting"
	case d.Phase == "Pending":
		return "not scheduled"
	case d.Phase == "Failed":
		return "failed"
	default:
		return strings.ToLower(d.Phase)
	}
}

// views is the command-bar registry. Names match the CLI's vocabulary so one set of words
// covers both surfaces.
var views = map[string]viewDef{
	"pods":         resourceView("Pods", "Pod"),
	"po":           resourceView("Pods", "Pod"),
	"deploy":       resourceView("Deployments", "Deployment"),
	"deployments":  resourceView("Deployments", "Deployment"),
	"sts":          resourceView("StatefulSets", "StatefulSet"),
	"statefulsets": resourceView("StatefulSets", "StatefulSet"),
	"ds":           resourceView("DaemonSets", "DaemonSet"),
	"daemonsets":   resourceView("DaemonSets", "DaemonSet"),
	"svc":          resourceView("Services", "Service"),
	"services":     resourceView("Services", "Service"),
	"ing":          resourceView("Ingresses", "Ingress"),
	"ingresses":    resourceView("Ingresses", "Ingress"),
	"nodes":        resourceView("Nodes", "Node"),
	"no":           resourceView("Nodes", "Node"),
	"jobs":         resourceView("Jobs", "Job"),
	"all":          resourceView("All resources", ""),
	"risk":         riskView,
	"signals":      signalsView,
	"changes":      changesView,
	"logs":         logsView,
	"traces":       tracesView,
	"rca":          rcaView,
	"triage":       triageView,

	"capacity":  capacityView,
	"cap":       capacityView,
	"pending":   pendingView,
	"storage":   storageView,
	"pvc":       storageView,
	"maintain":  maintainView,
	"eol":       maintainView,
	"upgrade":   upgradeView,
	"nodepools": nodePoolsView,
	"karpenter": nodePoolsView,
	"ca":        nodeGroupsView,
	"routes":    routesView,
	"traefik":   routesView,
	"mesh":      meshView,
	"istio":     meshView,
	"gitops":    gitopsView,
	"argocd":    gitopsView,
	"vpccni":    vpcCniView,
	"cni":       vpcCniView,
	"ebscsi":    ebsCsiView,
	"ebs":       ebsCsiView,
}

// plannedViews name screens that exist in the roadmap but not yet in the binary, so the
// command bar can say "not yet" instead of silently doing nothing.
var plannedViews = map[string]string{
	"shield": "the Attack Paths page in the web app",
}

func joinWords(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	out := ""
	for i, s := range items {
		switch {
		case i == 0:
			out = s
		case i == len(items)-1:
			out += " and " + s
		default:
			out += ", " + s
		}
	}
	return out
}

func itoa(n int) string     { return k8s.Itoa(n) }
func itoa64(n int64) string { return k8s.Itoa(int(n)) }
