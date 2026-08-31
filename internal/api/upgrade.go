package api

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// UpgradeReadiness is one cluster's readiness for a target Kubernetes version.
type UpgradeReadiness struct {
	CurrentVersion string `json:"currentVersion"`
	TargetVersion  string `json:"targetVersion"`
	Score          int    `json:"score"`
	RiskLevel      string `json:"riskLevel"`
	Support        struct {
		DaysUntilForcedUpgrade *int   `json:"daysUntilForcedUpgrade"`
		StandardSupportEnd     string `json:"standardSupportEnd"`
		ExtendedSupportEnd     string `json:"extendedSupportEnd"`
		ForcedUpgradeDate      string `json:"forcedUpgradeDate"`
		CostWarning            string `json:"costWarning"`
		VendorManaged          *bool  `json:"vendorManaged"`
		// DataSourced=false means the figure is illustrative, not quoted. Presenting an
		// illustrative cost as a real one is how a number ends up in a budget.
		DataSourced *bool    `json:"dataSourced"`
		AnnualCost  *float64 `json:"annualExtendedSupportCostEstimate"`
	} `json:"support"`
	Findings                []Finding      `json:"findings"`
	FindingCountsBySeverity map[string]int `json:"findingCountsBySeverity"`
	CheckedAt               *time.Time     `json:"checkedAt"`
	LastSyncAt              *time.Time     `json:"lastSyncAt"`
	DataAgeSeconds          *int64         `json:"dataAgeSeconds"`
	// Stale and ScanStatus are how the backend says "this verdict is built on old or partial
	// data" — a readiness answer without them is not interpretable.
	Stale      bool   `json:"stale"`
	ScanStatus string `json:"scanStatus"`
	Coverage   []struct {
		Tier   string `json:"tier"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	} `json:"coverage"`
}

// FleetUpgradeItem is one cluster's row in the fleet view.
type FleetUpgradeItem struct {
	ClusterID                string   `json:"clusterId"`
	Name                     string   `json:"name"`
	CurrentVersion           string   `json:"currentVersion"`
	ImpliedTargetVersion     string   `json:"impliedTargetVersion"`
	Score                    *int     `json:"score"`
	RiskLevel                string   `json:"riskLevel"`
	DaysUntilForcedUpgrade   *int     `json:"daysUntilForcedUpgrade"`
	CriticalCount            *int     `json:"criticalCount"`
	HighCount                *int     `json:"highCount"`
	MediumCount              *int     `json:"mediumCount"`
	LowCount                 *int     `json:"lowCount"`
	ForcedUpgradeDate        string   `json:"forcedUpgradeDate"`
	EstimatedRemediationDays *float64 `json:"estimatedRemediationDays"`
}

// UpgradeReadiness fetches one cluster's readiness. targetVersion is optional; the backend
// picks the implied next version when it is empty.
func (c *Client) UpgradeReadiness(ctx context.Context, orgID, clusterID, targetVersion string) (*UpgradeReadiness, error) {
	q := url.Values{}
	if targetVersion != "" {
		q.Set("targetVersion", targetVersion)
	}
	out, err := Get[UpgradeReadiness](ctx, c, clusterPath(orgID, clusterID, "/upgrade-readiness"), q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// FleetUpgradeReadiness fetches every cluster's readiness at its implied target.
func (c *Client) FleetUpgradeReadiness(ctx context.Context, orgID string) ([]FleetUpgradeItem, error) {
	return Get[[]FleetUpgradeItem](ctx, c,
		fmt.Sprintf("/eac/api/1.0/orgs/%s/clusters/upgrade-readiness", orgID), nil)
}

// ManifestRisk is the result of evaluating a rendered manifest bundle.
type ManifestRisk struct {
	Score         int            `json:"score"`
	Level         string         `json:"level"`
	Findings      []ManifestFind `json:"findings"`
	ResourceCount int            `json:"resourceCount"`
	CheckedAt     *time.Time     `json:"checkedAt"`
}

// ManifestFind is one finding against a manifest.
type ManifestFind struct {
	RuleID         string `json:"ruleId"`
	Category       string `json:"category"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
	ScoreImpact    int    `json:"scoreImpact"`
	ResourceName   string `json:"resourceName"`
	ResourceType   string `json:"resourceType"`
}

// EvaluateManifest scores a rendered multi-doc YAML bundle. Stateless and cluster-free, so
// it works from CI with nothing but a token.
func (c *Client) EvaluateManifest(ctx context.Context, yaml string) (*ManifestRisk, error) {
	out, err := Post[ManifestRisk](ctx, c, "/eac/api/1.0/deployment-risk/evaluate", nil,
		map[string]string{"yaml": yaml})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
