package api

import (
	"context"
	"fmt"
	"time"
)

// Cluster mirrors io.runtimez.eac.kube.dto.ClusterResponse.
type Cluster struct {
	ID                      string     `json:"id"`
	OrgID                   string     `json:"orgId"`
	ProjectID               string     `json:"projectId"`
	EnvironmentID           string     `json:"environmentId"`
	Name                    string     `json:"name"`
	Provider                string     `json:"provider"`
	Type                    string     `json:"type"`
	ConnectionMode          string     `json:"connectionMode"`
	AccessMode              string     `json:"accessMode"`
	Status                  string     `json:"status"`
	AgentVersion            string     `json:"agentVersion"`
	KubernetesVersion       string     `json:"kubernetesVersion"`
	NodeCount               *int       `json:"nodeCount"`
	ResourceCount           *int       `json:"resourceCount"`
	LastHeartbeatAt         *time.Time `json:"lastHeartbeatAt"`
	CreatedBy               string     `json:"createdBy"`
	CreatedAt               *time.Time `json:"createdAt"`
	UpdatedAt               *time.Time `json:"updatedAt"`
	ComplianceFramework     string     `json:"complianceFramework"`
	ScanEnabled             *bool      `json:"scanEnabled"`
	AvailableUpgradeTargets []string   `json:"availableUpgradeTargets"`
	LastSyncAt              *time.Time `json:"lastSyncAt"`
}

// Clusters lists the clusters visible to the caller in an org.
func (c *Client) Clusters(ctx context.Context, orgID string) ([]Cluster, error) {
	return Get[[]Cluster](ctx, c, fmt.Sprintf("/eac/api/1.0/orgs/%s/clusters", orgID), nil)
}

// ClusterByID fetches a single cluster.
func (c *Client) ClusterByID(ctx context.Context, orgID, clusterID string) (*Cluster, error) {
	out, err := Get[Cluster](ctx, c, fmt.Sprintf("/eac/api/1.0/orgs/%s/clusters/%s", orgID, clusterID), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// PipelineHealth reports telemetry ingest liveness for the org — the closest thing to
// "is the agent actually reporting" that the API exposes.
func (c *Client) PipelineHealth(ctx context.Context, orgID string) (map[string]any, error) {
	return Get[map[string]any](ctx, c,
		fmt.Sprintf("/eac/api/1.0/orgs/%s/observability/pipeline-health", orgID), nil)
}
