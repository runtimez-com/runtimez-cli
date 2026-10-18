package api

import (
	"context"
	"net/url"
	"time"
)

// The add-on workspaces (Karpenter, Cluster Autoscaler, Traefik ingress, Istio mesh, Argo CD,
// Amazon VPC CNI, Amazon EBS CSI) share one envelope: a status that is "OK" only when the
// add-on was found and read, a message saying why otherwise, and rows that each carry the
// rule findings ("reasons") that explain their state.

// WorkspaceHead is the part of every workspace response that says how far to trust it.
type WorkspaceHead struct {
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	LastSyncAt *time.Time `json:"lastSyncAt"`
	Addon      *struct {
		AddonID        string `json:"addonId"`
		DisplayName    string `json:"displayName"`
		Version        string `json:"version"`
		RuleCount      int    `json:"ruleCount"`
		RulesEvaluated bool   `json:"rulesEvaluated"`
	} `json:"addon"`
	Counts map[string]int `json:"counts"`
}

// OK reports whether the add-on was found and read. Every other status is a "we could not
// see", never a clean bill of health.
func (h WorkspaceHead) OK() bool { return h.Status == "" || h.Status == "OK" }

// AddonReason is one rule finding attached to a workspace row.
type AddonReason struct {
	RuleID         string `json:"ruleId"`
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation"`
	VerifyCommand  string `json:"verifyCommand"`
	ObjectKind     string `json:"objectKind"`
	ObjectRef      string `json:"objectRef"`
}

// WorkspaceEvent is a Kubernetes event in a workspace detail.
type WorkspaceEvent struct {
	At         *time.Time `json:"at"`
	Type       string     `json:"type"`
	Reason     string     `json:"reason"`
	Message    string     `json:"message"`
	ObjectKind string     `json:"objectKind"`
	ObjectName string     `json:"objectName"`
	Count      *int       `json:"count"`
}

// --- Karpenter node pools ----------------------------------------------------

// NodePoolRow is one Karpenter NodePool.
type NodePoolRow struct {
	Name                string        `json:"name"`
	State               string        `json:"state"`
	StateDetail         string        `json:"stateDetail"`
	Nodes               *int64        `json:"nodes"`
	Weight              *int          `json:"weight"`
	ConsolidationPolicy string        `json:"consolidationPolicy"`
	NodeClaimCount      int           `json:"nodeClaimCount"`
	DriftedCount        int           `json:"driftedCount"`
	NotReadyNodeCount   int           `json:"notReadyNodeCount"`
	Reasons             []AddonReason `json:"reasons"`
}

// NodePools is the Karpenter workspace list.
type NodePools struct {
	WorkspaceHead
	Header *struct {
		Controller *struct {
			Version string `json:"version"`
		} `json:"controller"`
		ManagedBy string `json:"managedBy"`
	} `json:"header"`
	NodePools []NodePoolRow `json:"nodePools"`
}

// NodePoolDetail is one NodePool with its claims and events.
type NodePoolDetail struct {
	WorkspaceHead
	NodePool   *NodePoolRow `json:"nodePool"`
	NodeClaims []struct {
		Name         string `json:"name"`
		Phase        string `json:"phase"`
		PhaseReason  string `json:"phaseReason"`
		InstanceType string `json:"instanceType"`
		CapacityType string `json:"capacityType"`
		Zone         string `json:"zone"`
		NodeName     string `json:"nodeName"`
		Drifted      bool   `json:"drifted"`
	} `json:"nodeClaims"`
	Events          []WorkspaceEvent `json:"events"`
	EventsAvailable bool             `json:"eventsAvailable"`
}

func (c *Client) NodePools(ctx context.Context, orgID, clusterID string) (*Payload[NodePools], error) {
	return GetPayload[NodePools](ctx, c, clusterPath(orgID, clusterID, "/autoscaling/nodepools"), nil)
}

func (c *Client) NodePool(ctx context.Context, orgID, clusterID, name string) (*Payload[NodePoolDetail], error) {
	return GetPayload[NodePoolDetail](ctx, c,
		clusterPath(orgID, clusterID, "/autoscaling/nodepools/"+url.PathEscape(name)), nil)
}

// --- Cluster Autoscaler node groups ------------------------------------------

// NodeGroupRow is one Cluster Autoscaler node group.
type NodeGroupRow struct {
	Name      string        `json:"name"`
	State     string        `json:"state"`
	Detail    string        `json:"detail"`
	Target    *int          `json:"target"`
	MinSize   *int          `json:"minSize"`
	MaxSize   *int          `json:"maxSize"`
	NodeCount int           `json:"nodeCount"`
	Reasons   []AddonReason `json:"reasons"`
}

// NodeGroups is the Cluster Autoscaler workspace list.
type NodeGroups struct {
	WorkspaceHead
	Header *struct {
		AutoscalerStatus string        `json:"autoscalerStatus"`
		StatusStale      bool          `json:"statusStale"`
		Reasons          []AddonReason `json:"reasons"`
	} `json:"header"`
	NodeGroups []NodeGroupRow `json:"nodeGroups"`
}

// NodeGroupDetail is one node group with its nodes and events.
type NodeGroupDetail struct {
	WorkspaceHead
	NodeGroup *NodeGroupRow `json:"nodeGroup"`
	Nodes     []struct {
		Name              string `json:"name"`
		Ready             *bool  `json:"ready"`
		ScaleDownDisabled bool   `json:"scaleDownDisabled"`
		InstanceType      string `json:"instanceType"`
		Zone              string `json:"zone"`
	} `json:"nodes"`
	Events          []WorkspaceEvent `json:"events"`
	EventsAvailable bool             `json:"eventsAvailable"`
}

func (c *Client) NodeGroups(ctx context.Context, orgID, clusterID string) (*Payload[NodeGroups], error) {
	return GetPayload[NodeGroups](ctx, c, clusterPath(orgID, clusterID, "/autoscaling/cluster-autoscaler"), nil)
}

func (c *Client) NodeGroup(ctx context.Context, orgID, clusterID, name string) (*Payload[NodeGroupDetail], error) {
	return GetPayload[NodeGroupDetail](ctx, c,
		clusterPath(orgID, clusterID, "/autoscaling/cluster-autoscaler/nodegroup"), url.Values{"name": {name}})
}

// --- Ingress routes (Traefik) -------------------------------------------------

// IngressRouteRow is one route (Ingress, IngressRoute, HTTPRoute …).
type IngressRouteRow struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StateWhy  string `json:"stateWhy"`
	Matches   []struct {
		Hosts []string `json:"hosts"`
		Paths []string `json:"paths"`
	} `json:"matches"`
	Reasons []AddonReason `json:"reasons"`
}

// IngressRoutes is the Ingress workspace list.
type IngressRoutes struct {
	WorkspaceHead
	Routes []IngressRouteRow `json:"routes"`
}

// IngressRouteDetail is one route with the workloads behind it.
type IngressRouteDetail struct {
	WorkspaceHead
	Route            *IngressRouteRow `json:"route"`
	BackendWorkloads []struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Service   string `json:"service"`
	} `json:"backendWorkloads"`
}

func (c *Client) IngressRoutes(ctx context.Context, orgID, clusterID string) (*Payload[IngressRoutes], error) {
	return GetPayload[IngressRoutes](ctx, c, clusterPath(orgID, clusterID, "/ingress/routes"), nil)
}

func (c *Client) IngressRoute(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*Payload[IngressRouteDetail], error) {
	return GetPayload[IngressRouteDetail](ctx, c,
		clusterPath(orgID, clusterID, "/ingress/routes/"+url.PathEscape(namespace)+"/"+url.PathEscape(name)), kindQuery(kind))
}

// --- Mesh workloads (Istio) ---------------------------------------------------

// MeshWorkloadRow is one workload's mesh state.
type MeshWorkloadRow struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	State     string `json:"state"`
	StateWhy  string `json:"stateWhy"`
	Proxy     *struct {
		Status     string `json:"status"`
		PodsMeshed int    `json:"podsMeshed"`
		Pods       int    `json:"pods"`
		Version    string `json:"version"`
	} `json:"proxy"`
	Mtls *struct {
		Mode  string `json:"mode"`
		Known bool   `json:"known"`
	} `json:"mtls"`
	Reasons []AddonReason `json:"reasons"`
}

// MeshWorkloads is the Mesh workspace list.
type MeshWorkloads struct {
	WorkspaceHead
	Workloads []MeshWorkloadRow `json:"workloads"`
}

// MeshWorkloadDetail is one workload with its pods' proxy facts.
type MeshWorkloadDetail struct {
	WorkspaceHead
	Workload *MeshWorkloadRow `json:"workload"`
	Pods     []struct {
		Name          string `json:"name"`
		Meshed        bool   `json:"meshed"`
		NativeSidecar bool   `json:"nativeSidecar"`
		Version       string `json:"version"`
		Revision      string `json:"revision"`
		OptedOut      bool   `json:"optedOut"`
	} `json:"pods"`
}

func (c *Client) MeshWorkloads(ctx context.Context, orgID, clusterID string) (*Payload[MeshWorkloads], error) {
	return GetPayload[MeshWorkloads](ctx, c, clusterPath(orgID, clusterID, "/mesh/workloads"), nil)
}

func (c *Client) MeshWorkload(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*Payload[MeshWorkloadDetail], error) {
	return GetPayload[MeshWorkloadDetail](ctx, c,
		clusterPath(orgID, clusterID, "/mesh/workloads/"+url.PathEscape(namespace)+"/"+url.PathEscape(name)), kindQuery(kind))
}

// --- GitOps applications (Argo CD) -------------------------------------------

// GitOpsAppRow is one Argo CD Application.
type GitOpsAppRow struct {
	Namespace      string        `json:"namespace"`
	Name           string        `json:"name"`
	Project        string        `json:"project"`
	Sync           string        `json:"sync"`
	Health         string        `json:"health"`
	OperationPhase string        `json:"operationPhase"`
	State          string        `json:"state"`
	Reasons        []AddonReason `json:"reasons"`
}

// GitOpsApps is the GitOps workspace list.
type GitOpsApps struct {
	WorkspaceHead
	Applications []GitOpsAppRow `json:"applications"`
}

// GitOpsAppDetail is one Application with the workloads it deploys.
type GitOpsAppDetail struct {
	WorkspaceHead
	Application *GitOpsAppRow `json:"application"`
	Workloads   []struct {
		Kind            string `json:"kind"`
		Namespace       string `json:"namespace"`
		Name            string `json:"name"`
		DesiredReplicas *int   `json:"desiredReplicas"`
		ReadyReplicas   *int   `json:"readyReplicas"`
		TrackedBy       string `json:"trackedBy"`
	} `json:"workloads"`
}

func (c *Client) GitOpsApps(ctx context.Context, orgID, clusterID string) (*Payload[GitOpsApps], error) {
	return GetPayload[GitOpsApps](ctx, c, clusterPath(orgID, clusterID, "/gitops/applications"), nil)
}

func (c *Client) GitOpsApp(ctx context.Context, orgID, clusterID, namespace, name string) (*Payload[GitOpsAppDetail], error) {
	return GetPayload[GitOpsAppDetail](ctx, c,
		clusterPath(orgID, clusterID, "/gitops/applications/"+url.PathEscape(namespace)+"/"+url.PathEscape(name)), nil)
}

// --- Amazon VPC CNI -----------------------------------------------------------

// VpcCniNodeRow is one node's pod-IP state.
type VpcCniNodeRow struct {
	Name           string        `json:"name"`
	State          string        `json:"state"`
	StateDetail    string        `json:"stateDetail"`
	InstanceType   string        `json:"instanceType"`
	Zone           string        `json:"zone"`
	MaxPods        *int          `json:"maxPods"`
	IPMax          *int          `json:"ipMax"`
	IPUsable       *int          `json:"ipUsable"`
	IPAssigned     *int          `json:"ipAssigned"`
	EnisAllocated  *int          `json:"enisAllocated"`
	EnisMax        *int          `json:"enisMax"`
	CNIPodRestarts *int          `json:"cniPodRestarts"`
	Reasons        []AddonReason `json:"reasons"`
}

// VpcCni is the Networking workspace list.
type VpcCni struct {
	WorkspaceHead
	Header *struct {
		CNIVersion               string        `json:"cniVersion"`
		NetworkPolicyEnforcement string        `json:"networkPolicyEnforcement"`
		Reasons                  []AddonReason `json:"reasons"`
	} `json:"header"`
	Nodes []VpcCniNodeRow `json:"nodes"`
}

// VpcCniNodeDetail is one node with its ipamd / AWS API error counts.
type VpcCniNodeDetail struct {
	WorkspaceHead
	Node   *VpcCniNodeRow `json:"node"`
	Errors []struct {
		Family   string     `json:"family"`
		Key      string     `json:"key"`
		Detail   string     `json:"detail"`
		LastHour int        `json:"lastHour"`
		LastDay  int        `json:"lastDay"`
		LastSeen *time.Time `json:"lastSeen"`
	} `json:"errors"`
}

func (c *Client) VpcCni(ctx context.Context, orgID, clusterID string) (*Payload[VpcCni], error) {
	return GetPayload[VpcCni](ctx, c, clusterPath(orgID, clusterID, "/networking/vpc-cni"), nil)
}

func (c *Client) VpcCniNode(ctx context.Context, orgID, clusterID, name string) (*Payload[VpcCniNodeDetail], error) {
	return GetPayload[VpcCniNodeDetail](ctx, c,
		clusterPath(orgID, clusterID, "/networking/vpc-cni/node"), url.Values{"name": {name}})
}

// --- Amazon EBS CSI -----------------------------------------------------------

// EbsCsiNodeRow is one node's EBS attach state.
type EbsCsiNodeRow struct {
	Name               string        `json:"name"`
	State              string        `json:"state"`
	NodeReady          *bool         `json:"nodeReady"`
	DriverRegistered   *bool         `json:"driverRegistered"`
	Attached           *int          `json:"attached"`
	Limit              *int          `json:"limit"`
	FailingAttachments int           `json:"failingAttachments"`
	Reasons            []AddonReason `json:"reasons"`
}

// EbsCsiAttachment is one VolumeAttachment.
type EbsCsiAttachment struct {
	Name             string        `json:"name"`
	Node             string        `json:"node"`
	PersistentVolume string        `json:"persistentVolume"`
	Attached         bool          `json:"attached"`
	Problem          string        `json:"problem"`
	Message          string        `json:"message"`
	Since            *time.Time    `json:"since"`
	Reasons          []AddonReason `json:"reasons"`
}

// EbsCsi is the Storage workspace list.
type EbsCsi struct {
	WorkspaceHead
	Header *struct {
		DriverVersion   string `json:"driverVersion"`
		ControllerReady string `json:"controllerReady"`
		NodePluginReady string `json:"nodePluginReady"`
	} `json:"header"`
	Nodes          []EbsCsiNodeRow    `json:"nodes"`
	Attachments    []EbsCsiAttachment `json:"attachments"`
	ClusterReasons []AddonReason      `json:"clusterReasons"`
	Workloads      []struct {
		Kind      string        `json:"kind"`
		Namespace string        `json:"namespace"`
		Name      string        `json:"name"`
		Reasons   []AddonReason `json:"reasons"`
	} `json:"workloads"`
}

// EbsCsiNodeDetail is one node with its attachments.
type EbsCsiNodeDetail struct {
	WorkspaceHead
	Node        *EbsCsiNodeRow     `json:"node"`
	Attachments []EbsCsiAttachment `json:"attachments"`
}

func (c *Client) EbsCsi(ctx context.Context, orgID, clusterID string) (*Payload[EbsCsi], error) {
	return GetPayload[EbsCsi](ctx, c, clusterPath(orgID, clusterID, "/storage/ebs-csi"), nil)
}

func (c *Client) EbsCsiNode(ctx context.Context, orgID, clusterID, name string) (*Payload[EbsCsiNodeDetail], error) {
	return GetPayload[EbsCsiNodeDetail](ctx, c,
		clusterPath(orgID, clusterID, "/storage/ebs-csi/node"), url.Values{"name": {name}})
}

func kindQuery(kind string) url.Values {
	if kind == "" {
		return nil
	}
	return url.Values{"kind": {kind}}
}
