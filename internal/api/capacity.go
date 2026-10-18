package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// The capacity types below are the subset of the backend's CapacityResponse family that the
// tables render. The full document is kept alongside (Payload.Raw) for -o json|yaml.

// Res is a CPU / memory pair. A nil field means the backend did not know the value.
type Res struct {
	CPUMillicores *int64 `json:"cpuMillicores"`
	MemoryBytes   *int64 `json:"memoryBytes"`
}

// Capacity is one cluster's schedulable capacity from its last sync.
type Capacity struct {
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	LastSyncAt *time.Time `json:"lastSyncAt"`
	ComputedAt *time.Time `json:"computedAt"`
	Autoscaler *struct {
		State     string `json:"state"`
		Acting    bool   `json:"acting"`
		Detail    string `json:"detail"`
		ManagedBy string `json:"managedBy"`
	} `json:"autoscaler"`
	Usage *struct {
		Status              string `json:"status"`
		Reason              string `json:"reason"`
		NodesWithSamples    int    `json:"nodesWithSamples"`
		NodesWithoutSamples int    `json:"nodesWithoutSamples"`
	} `json:"usage"`
	Headroom []struct {
		Axis                  string   `json:"axis"`
		Unit                  string   `json:"unit"`
		Allocatable           *int64   `json:"allocatable"`
		Requested             *int64   `json:"requested"`
		RequestsCommitmentPct *float64 `json:"requestsCommitmentPct"`
		Used                  *int64   `json:"used"`
		UsedPct               *float64 `json:"usedPct"`
		Headroom              *int64   `json:"headroom"`
		LargestFit            *int64   `json:"largestFit"`
		LargestFitNode        string   `json:"largestFitNode"`
	} `json:"headroom"`
	ClusterCeiling *struct {
		MaxNodes     *int   `json:"maxNodes"`
		CurrentNodes int    `json:"currentNodes"`
		Source       string `json:"source"`
		Reason       string `json:"reason"`
	} `json:"clusterCeiling"`
	Pools []CapacityPool `json:"pools"`
	Nodes []struct {
		Name         string `json:"name"`
		Pool         string `json:"pool"`
		Zone         string `json:"zone"`
		InstanceType string `json:"instanceType"`
		CapacityType string `json:"capacityType"`
		Eligible     bool   `json:"eligible"`
		Exclusion    string `json:"exclusion"`
		Allocatable  *Res   `json:"allocatable"`
		Requested    *Res   `json:"requested"`
		Used         *Res   `json:"used"`
		Headroom     *Res   `json:"headroom"`
		Pods         int    `json:"pods"`
	} `json:"nodes"`
	Blocked        []BlockedPod `json:"blocked"`
	BlockedSummary *struct {
		Total             int            `json:"total"`
		Listed            int            `json:"listed"`
		CapacityShortfall int            `json:"capacityShortfall"`
		Gated             int            `json:"gated"`
		Nominated         int            `json:"nominated"`
		ByCategory        map[string]int `json:"byCategory"`
		EventsRead        bool           `json:"eventsRead"`
		EventsTruncated   bool           `json:"eventsTruncated"`
	} `json:"blockedSummary"`
	Flags *struct {
		NodeRowsTruncated bool     `json:"nodeRowsTruncated"`
		PodRowsTruncated  bool     `json:"podRowsTruncated"`
		Nodeless          bool     `json:"nodeless"`
		Notes             []string `json:"notes"`
	} `json:"flags"`
	Storage *struct {
		Status        string `json:"status"`
		Reason        string `json:"reason"`
		Metrics       string `json:"metrics"`
		MetricsReason string `json:"metricsReason"`
		PVCs          []struct {
			Namespace     string   `json:"namespace"`
			Name          string   `json:"name"`
			Phase         string   `json:"phase"`
			StorageClass  string   `json:"storageClass"`
			CapacityBytes *int64   `json:"capacityBytes"`
			UsedBytes     *int64   `json:"usedBytes"`
			UsedPct       *float64 `json:"usedPct"`
			DaysToFull    *float64 `json:"daysToFull"`
			Forecast      string   `json:"forecast"`
			MountedBy     string   `json:"mountedBy"`
			UsageReason   string   `json:"usageReason"`
		} `json:"pvcs"`
		PVCsNotListed int `json:"pvcsNotListed"`
		Pending       []struct {
			Namespace      string `json:"namespace"`
			Name           string `json:"name"`
			StorageClass   string `json:"storageClass"`
			Reason         string `json:"reason"`
			Problem        bool   `json:"problem"`
			PendingSeconds *int64 `json:"pendingSeconds"`
		} `json:"pending"`
		Notes []string `json:"notes"`
	} `json:"storage"`
	Network *struct {
		Status           string `json:"status"`
		Reason           string `json:"reason"`
		CNIVersion       string `json:"cniVersion"`
		PodsWaitingForIP *int   `json:"podsWaitingForIp"`
		Pools            []struct {
			Name             string   `json:"name"`
			Nodes            int      `json:"nodes"`
			IPHeadroom       *int64   `json:"ipHeadroom"`
			SlotHeadroom     *int64   `json:"slotHeadroom"`
			NodesIPsFirst    int      `json:"nodesIpsFirst"`
			LaunchableNodes  *int     `json:"launchableNodes"`
			LaunchableReason string   `json:"launchableReason"`
			SubnetExhausted  bool     `json:"subnetExhausted"`
			LaunchSubnets    []string `json:"launchSubnets"`
		} `json:"pools"`
		Notes []string `json:"notes"`
	} `json:"network"`
	Batch *struct {
		Status      string         `json:"status"`
		Message     string         `json:"message"`
		Collected   bool           `json:"collected"`
		PendingJobs int            `json:"pendingJobs"`
		PendingPods int            `json:"pendingPods"`
		Verdicts    map[string]int `json:"verdicts"`
		Jobs        []struct {
			Namespace            string `json:"namespace"`
			Name                 string `json:"name"`
			CronJob              string `json:"cronJob"`
			Running              int    `json:"running"`
			Pending              int    `json:"pending"`
			OldestPendingSeconds *int64 `json:"oldestPendingSeconds"`
		} `json:"jobs"`
		JobsNotListed int `json:"jobsNotListed"`
		CronJobs      []struct {
			Namespace     string `json:"namespace"`
			Name          string `json:"name"`
			Schedule      string `json:"schedule"`
			Suspended     bool   `json:"suspended"`
			ActiveRuns    int    `json:"activeRuns"`
			Verdict       string `json:"verdict"`
			VerdictReason string `json:"verdictReason"`
		} `json:"cronJobs"`
		CronJobsNotListed int      `json:"cronJobsNotListed"`
		Notes             []string `json:"notes"`
	} `json:"batch"`
	Special *struct {
		Status    string `json:"status"`
		Reason    string `json:"reason"`
		Resources []struct {
			Name         string   `json:"name"`
			Kind         string   `json:"kind"`
			Unit         string   `json:"unit"`
			Allocatable  *int64   `json:"allocatable"`
			Requested    int64    `json:"requested"`
			RequestedPct *float64 `json:"requestedPct"`
			Free         *int64   `json:"free"`
			NodesOffered int      `json:"nodesOffering"`
			BlockedPods  int      `json:"blockedPods"`
		} `json:"resources"`
		Notes []string `json:"notes"`
	} `json:"special"`
}

// CapacityPool is one node pool's capacity.
type CapacityPool struct {
	Name                        string   `json:"name"`
	Source                      string   `json:"source"`
	AutoscalerState             string   `json:"autoscalerState"`
	Nodes                       int      `json:"nodes"`
	EligibleNodes               int      `json:"eligibleNodes"`
	Allocatable                 *Res     `json:"allocatable"`
	Requested                   *Res     `json:"requested"`
	Used                        *Res     `json:"used"`
	Headroom                    *Res     `json:"headroom"`
	CPURequestsCommitmentPct    *float64 `json:"cpuRequestsCommitmentPct"`
	MemoryRequestsCommitmentPct *float64 `json:"memoryRequestsCommitmentPct"`
	CPUUsedPct                  *float64 `json:"cpuUsedPct"`
	MemoryUsedPct               *float64 `json:"memoryUsedPct"`
	Pods                        int      `json:"pods"`
	AllocatablePods             *int64   `json:"allocatablePods"`
	NodeCeiling                 *int64   `json:"nodeCeiling"`
	RoomToGrow                  *struct {
		Nodes         *int64 `json:"nodes"`
		AtCeiling     bool   `json:"atCeiling"`
		CeilingSource string `json:"ceilingSource"`
		Reason        string `json:"reason"`
	} `json:"roomToGrow"`
}

// Capacity fetches the cluster's capacity model.
func (c *Client) Capacity(ctx context.Context, orgID, clusterID string) (*Payload[Capacity], error) {
	return GetPayload[Capacity](ctx, c, clusterPath(orgID, clusterID, "/capacity"), nil)
}

// CapacityForecast is one axis's days-to-full projection.
type CapacityForecast struct {
	Axis       string   `json:"axis"`
	Status     string   `json:"status"`
	Reason     string   `json:"reason"`
	Days       *int     `json:"days"`
	FullOn     string   `json:"fullOn"`
	CurrentPct *float64 `json:"currentPct"`
	Basis      string   `json:"basis"`
}

// CapacityTrend is the daily capacity history and its forecast.
type CapacityTrend struct {
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	Days          int     `json:"days"`
	From          string  `json:"from"`
	To            string  `json:"to"`
	Target        float64 `json:"target"`
	TargetSource  string  `json:"targetSource"`
	MinDays       int     `json:"minDays"`
	DaysCollected int     `json:"daysCollected"`
	Pools         []struct {
		Name       string            `json:"name"`
		Source     string            `json:"source"`
		Autoscaled bool              `json:"autoscaled"`
		CPU        *CapacityForecast `json:"cpu"`
		Memory     *CapacityForecast `json:"memory"`
		Grade      *struct {
			Status    string   `json:"status"`
			CPUPct    *float64 `json:"cpuPct"`
			MemoryPct *float64 `json:"memoryPct"`
			TargetPct float64  `json:"targetPct"`
			Reason    string   `json:"reason"`
		} `json:"grade"`
	} `json:"pools"`
	Growth []struct {
		Namespace   string `json:"namespace"`
		CPUDelta    int64  `json:"cpuDelta"`
		MemoryDelta int64  `json:"memoryDelta"`
		Appeared    bool   `json:"appeared"`
		Gone        bool   `json:"gone"`
	} `json:"growth"`
	Notes []string `json:"notes"`
}

// CapacityTrend fetches the trend; days 0 lets the backend pick its default window.
func (c *Client) CapacityTrend(ctx context.Context, orgID, clusterID string, days int) (*Payload[CapacityTrend], error) {
	q := url.Values{}
	if days > 0 {
		q.Set("days", strconv.Itoa(days))
	}
	return GetPayload[CapacityTrend](ctx, c, clusterPath(orgID, clusterID, "/capacity/trend"), q)
}

// CapacityCost is the cluster's node cost at list (or Cost Explorer) prices.
type CapacityCost struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	PriceSource *struct {
		Provider       string `json:"provider"`
		Label          string `json:"label"`
		OnDemandSource string `json:"onDemandSource"`
		OnDemandDate   string `json:"onDemandDate"`
		AgeDays        *int64 `json:"ageDays"`
	} `json:"priceSource"`
	Nodes            int      `json:"nodes"`
	PricedNodes      int      `json:"pricedNodes"`
	ClusterHourly    *float64 `json:"clusterHourly"`
	ClusterMonthly   *float64 `json:"clusterMonthly"`
	EffectiveMonthly *float64 `json:"effectiveMonthly"`
	Pools            []struct {
		Pool              string   `json:"pool"`
		Source            string   `json:"source"`
		Nodes             int      `json:"nodes"`
		PricedNodes       int      `json:"pricedNodes"`
		Hourly            *float64 `json:"hourly"`
		Monthly           *float64 `json:"monthly"`
		CPURequestsPct    *float64 `json:"cpuRequestsPct"`
		MemoryRequestsPct *float64 `json:"memoryRequestsPct"`
	} `json:"pools"`
	Levers []struct {
		ID      string   `json:"id"`
		Label   string   `json:"label"`
		Saving  bool     `json:"saving"`
		Monthly *float64 `json:"monthly"`
		Basis   string   `json:"basis"`
		Reason  string   `json:"reason"`
		ActsIn  string   `json:"actsIn"`
	} `json:"levers"`
	Coverage *struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
	} `json:"coverage"`
	Notes []string `json:"notes"`
}

// CapacityCost fetches the Cost tab.
func (c *Client) CapacityCost(ctx context.Context, orgID, clusterID string) (*Payload[CapacityCost], error) {
	return GetPayload[CapacityCost](ctx, c, clusterPath(orgID, clusterID, "/capacity/cost"), nil)
}

// CapacityNamespaces is per-namespace requests, quotas and QoS.
type CapacityNamespaces struct {
	Status          string `json:"status"`
	Message         string `json:"message"`
	QuotasCollected bool   `json:"quotasCollected"`
	Namespaces      []struct {
		Namespace   string   `json:"namespace"`
		Pods        int      `json:"pods"`
		PendingPods int      `json:"pendingPods"`
		Requested   *Res     `json:"requested"`
		Used        *Res     `json:"used"`
		CPUShare    *float64 `json:"cpuShare"`
		MemoryShare *float64 `json:"memoryShare"`
		QuotaStatus string   `json:"quotaStatus"`
		Fullest     *struct {
			Resource string   `json:"resource"`
			Hard     string   `json:"hard"`
			Used     string   `json:"used"`
			Pct      *float64 `json:"pct"`
		} `json:"fullest"`
	} `json:"namespaces"`
	Notes []string `json:"notes"`
}

// CapacityNamespaces fetches the Namespaces section.
func (c *Client) CapacityNamespaces(ctx context.Context, orgID, clusterID string) (*Payload[CapacityNamespaces], error) {
	return GetPayload[CapacityNamespaces](ctx, c, clusterPath(orgID, clusterID, "/capacity/namespaces"), nil)
}

// FleetAxis is one resource axis summed over clusters.
type FleetAxis struct {
	Allocatable           *int64   `json:"allocatable"`
	Requested             *int64   `json:"requested"`
	RequestsCommitmentPct *float64 `json:"requestsCommitmentPct"`
	Used                  *int64   `json:"used"`
	UsedPct               *float64 `json:"usedPct"`
	Headroom              *int64   `json:"headroom"`
}

// FleetSoonestFull names the pool and axis that fills first.
type FleetSoonestFull struct {
	ClusterName string `json:"clusterName"`
	Pool        string `json:"pool"`
	Axis        string `json:"axis"`
	Days        *int   `json:"days"`
	FullOn      string `json:"fullOn"`
	Status      string `json:"status"`
}

// FleetCapacity is capacity across the org's clusters.
type FleetCapacity struct {
	ClustersTotal    int  `json:"clustersTotal"`
	ClustersAnalyzed int  `json:"clustersAnalyzed"`
	Truncated        bool `json:"truncated"`
	Totals           *struct {
		ClustersCounted        int               `json:"clustersCounted"`
		CountedFromEarlierSync int               `json:"countedFromEarlierSync"`
		NotCounted             map[string]int    `json:"notCounted"`
		CPU                    *FleetAxis        `json:"cpu"`
		Memory                 *FleetAxis        `json:"memory"`
		Pending                *int              `json:"pending"`
		CapacityShortfall      *int              `json:"capacityShortfall"`
		SoonestFull            *FleetSoonestFull `json:"soonestFull"`
	} `json:"totals"`
	Clusters []struct {
		ClusterID         string            `json:"clusterId"`
		Name              string            `json:"name"`
		Status            string            `json:"status"`
		Reason            string            `json:"reason"`
		CPU               *FleetAxis        `json:"cpu"`
		Memory            *FleetAxis        `json:"memory"`
		Pending           *int              `json:"pending"`
		CapacityShortfall *int              `json:"capacityShortfall"`
		AutoscalerState   string            `json:"autoscalerState"`
		SoonestFull       *FleetSoonestFull `json:"soonestFull"`
		Grade             string            `json:"grade"`
		FromEarlierSync   bool              `json:"fromEarlierSync"`
	} `json:"clusters"`
}

// FleetCapacity fetches capacity for every cluster in the org.
func (c *Client) FleetCapacity(ctx context.Context, orgID string) (*Payload[FleetCapacity], error) {
	return GetPayload[FleetCapacity](ctx, c, fmt.Sprintf("/eac/api/1.0/orgs/%s/capacity/fleet", orgID), nil)
}

// BlockedPod is a pod the scheduler could not place.
type BlockedPod struct {
	Namespace         string `json:"namespace"`
	Name              string `json:"name"`
	OwnerKind         string `json:"ownerKind"`
	OwnerName         string `json:"ownerName"`
	PendingSeconds    *int64 `json:"pendingSeconds"`
	State             string `json:"state"`
	PrimaryCategory   string `json:"primaryCategory"`
	CapacityShortfall bool   `json:"capacityShortfall"`
	Message           string `json:"message"`
	NominatedNode     string `json:"nominatedNode"`
	VerifyCommand     string `json:"verifyCommand"`
}
