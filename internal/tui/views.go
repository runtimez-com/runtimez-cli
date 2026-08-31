package tui

import (
	"context"
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
}

// dataset is what a view produces: the table to draw, plus the Kubernetes rows behind it
// when there are any. Reliability views have no resource rows, which is exactly why the
// model cannot assume every screen is a resource listing.
type dataset struct {
	table     *render.Table
	resources []k8s.Resource
	// notice is a caveat that belongs on screen with the data — missing signals, an
	// unscanned cluster — rather than in a log the operator never sees.
	notice string
}

// viewDef is one navigable screen.
type viewDef struct {
	Title string
	// Kind is the canonical Kubernetes kind for resource views; empty for the rest.
	Kind string
	// namespaced marks views that honour the :ns scope.
	namespaced bool
	fetch      func(ctx context.Context, c Client, orgID, clusterID, namespace string) (dataset, error)
}

// resourceView builds a standard Kubernetes listing.
func resourceView(title, kind string) viewDef {
	return viewDef{
		Title: title, Kind: kind, namespaced: true,
		fetch: func(ctx context.Context, c Client, org, cluster, ns string) (dataset, error) {
			rows, err := c.Resources(ctx, org, cluster, kind, ns)
			if err != nil {
				return dataset{}, err
			}
			return dataset{table: view.TableFor(kind, rows, ns == ""), resources: rows}, nil
		},
	}
}

var riskView = viewDef{
	Title: "Workload risk",
	fetch: func(ctx context.Context, c Client, org, cluster, _ string) (dataset, error) {
		p, err := c.WorkloadRiskPosture(ctx, org, cluster)
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
	fetch: func(ctx context.Context, c Client, org, cluster, _ string) (dataset, error) {
		res, err := c.SlowOperations(ctx, org, cluster, "", "", 50)
		if err != nil {
			return dataset{}, err
		}
		return dataset{table: view.SlowOperationsTable(res.Items)}, nil
	},
}

var logsView = viewDef{
	Title: "Logs",
	fetch: func(ctx context.Context, c Client, org, cluster, _ string) (dataset, error) {
		end := time.Now()
		page, err := c.SearchLogs(ctx, api.LogSearch{
			StartDate: api.EpochMs(end.Add(-time.Hour)), EndDate: api.EpochMs(end),
			ClusterID: cluster, Limit: 200,
		})
		if err != nil {
			return dataset{}, err
		}
		d := dataset{table: view.LogsTable(page.Data), notice: "last hour"}
		if page.Total > int64(len(page.Data)) {
			d.notice = "last hour — showing " + itoa(len(page.Data)) + " of " + itoa64(page.Total)
		}
		return d, nil
	},
}

var tracesView = viewDef{
	Title: "Traces",
	fetch: func(ctx context.Context, c Client, org, cluster, _ string) (dataset, error) {
		end := time.Now()
		page, err := c.ListTraces(ctx, api.TraceListRequest{
			StartDate: api.EpochMs(end.Add(-time.Hour)), EndDate: api.EpochMs(end),
			ClusterID: cluster, Limit: 100, SortBy: "durationNano", SortOrder: "desc",
		})
		if err != nil {
			return dataset{}, err
		}
		return dataset{table: view.TracesTable(page.Data), notice: "last hour, slowest first"}, nil
	},
}

var changesView = viewDef{
	Title: "Changes",
	fetch: func(ctx context.Context, c Client, org, cluster, _ string) (dataset, error) {
		res, err := c.Changes(ctx, org, cluster, 168, 0, 100)
		if err != nil {
			return dataset{}, err
		}
		d := dataset{table: view.ChangesTable(res.Items)}
		if res.Total > int64(len(res.Items)) {
			d.notice = "showing the most recent " + itoa(len(res.Items)) + " of " + itoa64(res.Total)
		}
		return d, nil
	},
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
}

// plannedViews name screens that exist in the roadmap but not yet in the binary, so the
// command bar can say "not yet" instead of silently doing nothing.
var plannedViews = map[string]string{
	// ask is interactive rather than tabular; it lives at `rtz ask` until the UI grows a
	// conversation pane.
	"ask":     "the `rtz ask` command",
	"upgrade": "M5",
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
