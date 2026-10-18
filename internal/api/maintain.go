package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// MaintainItem is one lifecycle item (Kubernetes, an add-on, a runtime, a data service) and
// its next support deadline.
type MaintainItem struct {
	ClusterID    string `json:"clusterId"`
	ClusterName  string `json:"clusterName"`
	ItemKey      string `json:"itemKey"`
	ItemType     string `json:"itemType"`
	DisplayName  string `json:"displayName"`
	Version      string `json:"version"`
	Cycle        string `json:"cycle"`
	Location     string `json:"location"`
	Product      string `json:"product"`
	Deadline     string `json:"deadline"`
	DeadlineKind string `json:"deadlineKind"`
	DaysLeft     *int   `json:"daysLeft"`
	SupportState string `json:"supportState"`
	Source       string `json:"source"`
	SourceURL    string `json:"sourceUrl"`
	Confidence   string `json:"confidence"`
	Owner        string `json:"owner"`
	Note         string `json:"note"`
	Platform     bool   `json:"platform"`
}

// MaintainList is the org's Maintain items.
type MaintainList struct {
	Items      []MaintainItem `json:"items"`
	ReadFailed bool           `json:"readFailed"`
	Truncated  bool           `json:"truncated"`
}

// MaintainItems fetches every Maintain item the caller can see.
func (c *Client) MaintainItems(ctx context.Context, orgID string) (*Payload[MaintainList], error) {
	return GetPayload[MaintainList](ctx, c, fmt.Sprintf("/eac/api/1.0/orgs/%s/maintain/items", orgID), nil)
}

// MaintainInventory is the fleet's running packages, most-used first.
type MaintainInventory struct {
	Ecosystem string `json:"ecosystem"`
	Q         string `json:"q"`
	Page      int    `json:"page"`
	Size      int    `json:"size"`
	Total     int    `json:"total"`
	Items     []struct {
		Ecosystem      string   `json:"ecosystem"`
		Name           string   `json:"name"`
		Versions       []string `json:"versions"`
		VersionsCapped bool     `json:"versionsCapped"`
		Workloads      int      `json:"workloads"`
		Pods           int      `json:"pods"`
		EOL            *struct {
			Product      string `json:"product"`
			Cycle        string `json:"cycle"`
			Deadline     string `json:"deadline"`
			DaysLeft     *int   `json:"daysLeft"`
			SupportState string `json:"supportState"`
		} `json:"eol"`
	} `json:"items"`
	WorkloadsWithPackageList    int  `json:"workloadsWithPackageList"`
	WorkloadsWithoutPackageList int  `json:"workloadsWithoutPackageList"`
	ReadFailed                  bool `json:"readFailed"`
	Truncated                   bool `json:"truncated"`
}

// MaintainInventory fetches one page of the package inventory.
func (c *Client) MaintainInventory(ctx context.Context, orgID, ecosystem, q string, page, size int) (*Payload[MaintainInventory], error) {
	v := url.Values{}
	if ecosystem != "" {
		v.Set("ecosystem", ecosystem)
	}
	if q != "" {
		v.Set("q", q)
	}
	v.Set("page", strconv.Itoa(page))
	v.Set("size", strconv.Itoa(size))
	return GetPayload[MaintainInventory](ctx, c,
		fmt.Sprintf("/eac/api/1.0/orgs/%s/maintain/packages/inventory", orgID), v)
}

// DataServiceRef names one data service (a database, cache, broker) inside a cluster.
type DataServiceRef struct {
	ClusterID, Namespace, Kind, Name string
}

func (r DataServiceRef) query() url.Values {
	v := url.Values{}
	v.Set("clusterId", r.ClusterID)
	v.Set("namespace", r.Namespace)
	v.Set("kind", r.Kind)
	v.Set("name", r.Name)
	return v
}

// DataServiceInbound is the callers that could not be named (via a node port, a load
// balancer, or nothing we can see).
type DataServiceInbound struct {
	Reported  bool  `json:"reported"`
	Truncated bool  `json:"truncated"`
	ViaNode   int64 `json:"viaNode"`
	Other     int64 `json:"other"`
}

// DataServiceDependents is who connects to a data service.
type DataServiceDependents struct {
	Coverage       string     `json:"coverage"`
	LastSnapshotAt *time.Time `json:"lastSnapshotAt"`
	NodesExpected  int        `json:"nodesExpected"`
	NodesReported  int        `json:"nodesReported"`
	Services       []string   `json:"services"`
	Dependents     []struct {
		Namespace   string     `json:"namespace"`
		Kind        string     `json:"kind"`
		Name        string     `json:"name"`
		Via         string     `json:"via"`
		Port        int        `json:"port"`
		LastSeen    *time.Time `json:"lastSeen"`
		Sockets     int64      `json:"sockets"`
		Role        string     `json:"role"`
		Pooler      string     `json:"pooler"`
		ClusterName string     `json:"clusterName"`
	} `json:"dependents"`
	Truncated           bool                `json:"truncated"`
	ReadFailed          bool                `json:"readFailed"`
	PlatformExcluded    int                 `json:"platformExcluded"`
	Inbound             *DataServiceInbound `json:"inbound"`
	OtherClustersHidden int                 `json:"otherClustersHidden"`
}

// DataServiceDependents fetches the blast radius of one data service.
func (c *Client) DataServiceDependents(ctx context.Context, orgID string, ref DataServiceRef) (*Payload[DataServiceDependents], error) {
	return GetPayload[DataServiceDependents](ctx, c,
		fmt.Sprintf("/eac/api/1.0/orgs/%s/maintain/data-services/dependents", orgID), ref.query())
}

// DataServiceImpact is what breaks if one data service is upgraded to a target version.
type DataServiceImpact struct {
	Product         string `json:"product"`
	DisplayName     string `json:"displayName"`
	CurrentVersion  string `json:"currentVersion"`
	Target          string `json:"target"`
	TargetDefaulted bool   `json:"targetDefaulted"`
	RulePack        bool   `json:"rulePack"`
	Operator        string `json:"operator"`
	UpgradeNotes    []struct {
		ID       string `json:"id"`
		Since    string `json:"since"`
		Severity string `json:"severity"`
		Title    string `json:"title"`
	} `json:"upgradeNotes"`
	Coverage   string `json:"coverage"`
	Dependents []struct {
		Namespace   string `json:"namespace"`
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Verdict     string `json:"verdict"`
		Reason      string `json:"reason"`
		ClusterName string `json:"clusterName"`
		Drivers     []struct {
			Driver  string `json:"driver"`
			Version string `json:"version"`
		} `json:"drivers"`
		Via []string `json:"via"`
	} `json:"dependents"`
	Summary *struct {
		BumpDriver int `json:"bumpDriver"`
		Unknown    int `json:"unknown"`
		Review     int `json:"review"`
		Ready      int `json:"ready"`
	} `json:"summary"`
	PlatformExcluded    int                 `json:"platformExcluded"`
	Truncated           bool                `json:"truncated"`
	ReadFailed          bool                `json:"readFailed"`
	Inbound             *DataServiceInbound `json:"inbound"`
	OtherClustersHidden int                 `json:"otherClustersHidden"`
}

// DataServiceImpact fetches the upgrade impact; an empty target lets the backend pick one.
func (c *Client) DataServiceImpact(ctx context.Context, orgID string, ref DataServiceRef, target string) (*Payload[DataServiceImpact], error) {
	v := ref.query()
	if target != "" {
		v.Set("target", target)
	}
	return GetPayload[DataServiceImpact](ctx, c,
		fmt.Sprintf("/eac/api/1.0/orgs/%s/maintain/data-services/impact", orgID), v)
}
