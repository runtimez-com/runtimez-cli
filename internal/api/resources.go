package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/k8s"
)

// ResourceRowLimit is the server-side cap in listByKindRows. There is no pagination behind
// it, so a result of exactly this length means rows were dropped and the caller must say so
// rather than present a truncated list as the whole cluster.
const ResourceRowLimit = 5000

func clusterPath(orgID, clusterID, suffix string) string {
	return fmt.Sprintf("/eac/api/1.0/orgs/%s/clusters/%s%s", orgID, clusterID, suffix)
}

// Resources lists synced resources, optionally filtered by kind and namespace. Both filters
// are exact matches on the stored values, so kind must be the canonical Kubernetes kind
// ("Deployment", not "deploy").
func (c *Client) Resources(ctx context.Context, orgID, clusterID, kind, namespace string) ([]k8s.Resource, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if namespace != "" {
		q.Set("namespace", namespace)
	}
	return Get[[]k8s.Resource](ctx, c, clusterPath(orgID, clusterID, "/resources"), q)
}

// SearchHit is one row from the resource search, which returns only identity columns.
type SearchHit struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
}

// SearchResources substring-matches workload names. The backend searches Deployment,
// StatefulSet and DaemonSet names only — not every kind.
func (c *Client) SearchResources(ctx context.Context, orgID, clusterID, q string, limit int) ([]SearchHit, error) {
	query := url.Values{"q": {q}}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return Get[[]SearchHit](ctx, c, clusterPath(orgID, clusterID, "/resources/search"), query)
}

// Namespaces lists the namespaces present in the last sync.
func (c *Client) Namespaces(ctx context.Context, orgID, clusterID string) ([]string, error) {
	return Get[[]string](ctx, c, clusterPath(orgID, clusterID, "/namespaces"), nil)
}

// Counts returns a resource count per kind.
func (c *Client) Counts(ctx context.Context, orgID, clusterID string) (map[string]int64, error) {
	return Get[map[string]int64](ctx, c, clusterPath(orgID, clusterID, "/counts"), nil)
}

// WorkloadDetail is the aggregated view behind `rtz describe`. Every list holds rows in the
// same shape as Resources.
type WorkloadDetail struct {
	Workload    *k8s.Resource  `json:"workload"`
	Pods        []k8s.Resource `json:"pods"`
	Events      []Event        `json:"events"`
	Services    []k8s.Resource `json:"services"`
	Ingresses   []k8s.Resource `json:"ingresses"`
	PVCs        []k8s.Resource `json:"pvcs"`
	ReplicaSets []k8s.Resource `json:"replicaSets"`
}

// Event is a correlated Kubernetes event. The backend returns warnings first, capped at 50.
// Field names vary across agent versions, so the alternates are decoded rather than guessed.
type Event struct {
	Type           string `json:"type"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	Count          int    `json:"count"`
	InvolvedObject string `json:"involvedObject"`
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	LastTimestamp  string `json:"lastTimestamp"`
	EventTime      string `json:"eventTime"`
	FirstTimestamp string `json:"firstTimestamp"`
}

// When picks whichever timestamp this event actually carries.
func (e Event) When() string {
	for _, ts := range []string{e.LastTimestamp, e.EventTime, e.FirstTimestamp} {
		if ts != "" {
			return ts
		}
	}
	return ""
}

// Detail fetches one workload with its pods, events and related objects. kind is optional
// and disambiguates a name shared by two controller kinds in one namespace.
func (c *Client) Detail(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*WorkloadDetail, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	path := clusterPath(orgID, clusterID, fmt.Sprintf("/workloads/%s/%s/detail",
		url.PathEscape(namespace), url.PathEscape(name)))
	out, err := Get[WorkloadDetail](ctx, c, path, q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// FleetSummary is the org-level rollup. Scores run 0-100 and higher is worse.
type FleetSummary struct {
	FleetRiskScore     *int   `json:"fleetRiskScore"`
	FleetRiskLevel     string `json:"fleetRiskLevel"`
	FindingsBySeverity struct {
		Crit int `json:"crit"`
		High int `json:"high"`
		Med  int `json:"med"`
		Low  int `json:"low"`
	} `json:"findingsBySeverity"`
	Clusters struct {
		Total     int `json:"total"`
		Connected int `json:"connected"`
		Degraded  int `json:"degraded"`
	} `json:"clusters"`
	Releases7d struct {
		Healthy              int `json:"healthy"`
		Degraded             int `json:"degraded"`
		AwaitingVerification int `json:"awaitingVerification"`
		InsufficientData     int `json:"insufficientData"`
		Total                int `json:"total"`
	} `json:"releases7d"`
	ClusterLastVerdicts map[string]string `json:"clusterLastVerdicts"`
	ClusterLastOutcomes map[string]string `json:"clusterLastOutcomes"`
	Environments        []struct {
		Value string `json:"value"`
		Label string `json:"label"`
	} `json:"environments"`
	GeneratedAt *time.Time `json:"generatedAt"`
}

// Fleet returns the org-wide rollup. windowDays defaults to 7 server-side.
func (c *Client) Fleet(ctx context.Context, orgID string, windowDays int, env, clusterID string) (*FleetSummary, error) {
	q := url.Values{}
	if windowDays > 0 {
		q.Set("window", strconv.Itoa(windowDays))
	}
	if env != "" {
		q.Set("env", env)
	}
	if clusterID != "" {
		q.Set("clusterId", clusterID)
	}
	out, err := Get[FleetSummary](ctx, c, fmt.Sprintf("/eac/api/1.0/orgs/%s/fleet-summary", orgID), q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
