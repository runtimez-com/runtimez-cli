package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Severity ordering used by --fail-on. Higher is worse, matching the backend's
// RiskLevel/Severity semantics where a bigger score is a bigger problem.
var severityRank = map[string]int{
	"LOW": 1, "MEDIUM": 2, "MED": 2, "HIGH": 3, "CRITICAL": 4, "CRIT": 4,
}

// SeverityRank returns the ordinal for a severity or level name, and whether it was known.
// An unknown value must not silently sort as "harmless" — the caller decides what to do.
func SeverityRank(s string) (int, bool) {
	r, ok := severityRank[normalizeSeverity(s)]
	return r, ok
}

func normalizeSeverity(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			r -= 32
		}
		out = append(out, r)
	}
	return string(out)
}

// ---------------------------------------------------------------- risk posture

// WorkloadRisk is the per-cluster workload risk posture.
type WorkloadRisk struct {
	Summary struct {
		Total    int `json:"total"`
		Critical int `json:"critical"`
		High     int `json:"high"`
		Medium   int `json:"medium"`
		Low      int `json:"low"`
	} `json:"summary"`
	Workloads       []WorkloadRiskItem `json:"workloads"`
	ChurnWindowDays int                `json:"churnWindowDays"`

	// Signal availability. These are the difference between "this workload is fine" and
	// "we could not see enough to say" — a low score with metrics unavailable is not a
	// clean bill of health, and reporting it as one is the worst thing this command could do.
	CVEScanAvailable        bool `json:"cveScanAvailable"`
	MetricsAvailable        bool `json:"metricsAvailable"`
	RuntimeAvailable        bool `json:"runtimeAvailable"`
	NetworkSignalsAvailable bool `json:"networkSignalsAvailable"`

	DegradedWorkloads    int        `json:"degradedWorkloads"`
	UnevaluableWorkloads int        `json:"unevaluableWorkloads"`
	UnmeasuredWorkloads  *int       `json:"unmeasuredWorkloads"`
	CheckedAt            *time.Time `json:"checkedAt"`
}

// MissingSignals names the evidence sources that were unavailable for this evaluation.
func (w WorkloadRisk) MissingSignals() []string {
	var out []string
	for _, s := range []struct {
		ok   bool
		name string
	}{
		{w.MetricsAvailable, "metrics"},
		{w.CVEScanAvailable, "CVE scan"},
		{w.RuntimeAvailable, "runtime"},
		{w.NetworkSignalsAvailable, "network signals"},
	} {
		if !s.ok {
			out = append(out, s.name)
		}
	}
	return out
}

// WorkloadRiskItem is one scored workload. Score runs 0-100 and higher is worse.
type WorkloadRiskItem struct {
	Namespace string             `json:"namespace"`
	Name      string             `json:"name"`
	Kind      string             `json:"kind"`
	UID       string             `json:"uid"`
	Score     int                `json:"score"`
	Level     string             `json:"level"`
	Factors   []WorkloadRiskFact `json:"factors"`
}

// WorkloadRiskFact is one contributing factor behind a score.
type WorkloadRiskFact struct {
	Category          string `json:"category"`
	Severity          string `json:"severity"`
	Title             string `json:"title"`
	Detail            string `json:"detail"`
	ScoreContribution int    `json:"scoreContribution"`
	Recommendation    string `json:"recommendation"`
	ProposedLimit     string `json:"proposedLimit"`
	ObservedPeak      string `json:"observedPeak"`
}

// WorkloadRiskPosture fetches the scored workload inventory.
func (c *Client) WorkloadRiskPosture(ctx context.Context, orgID, clusterID string) (*WorkloadRisk, error) {
	out, err := Get[WorkloadRisk](ctx, c, clusterPath(orgID, clusterID, "/workload-risk"), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Posture is the shared shape of the security and compliance posture endpoints.
type Posture struct {
	Score                   int            `json:"score"`
	RiskLevel               string         `json:"riskLevel"`
	Findings                []Finding      `json:"findings"`
	FindingCountsBySeverity map[string]int `json:"findingCountsBySeverity"`
	ScannedAt               *time.Time     `json:"scannedAt"`
	LastScanID              string         `json:"lastScanId"`
	CheckedAt               *time.Time     `json:"checkedAt"`
}

// Finding is one policy finding.
type Finding struct {
	ID             string `json:"id"`
	RuleID         string `json:"ruleId"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	Impact         string `json:"impact"`
	Recommendation string `json:"recommendation"`
	Category       string `json:"category"`
	Severity       string `json:"severity"`
	ResourceName   string `json:"resourceName"`
	ResourceKind   string `json:"resourceKind"`
	Namespace      string `json:"namespace"`
}

// SecurityPosture returns policy findings for the cluster.
func (c *Client) SecurityPosture(ctx context.Context, orgID, clusterID string) (*Posture, error) {
	out, err := Get[Posture](ctx, c, clusterPath(orgID, clusterID, "/security-posture"), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CompliancePosture returns control findings for the cluster's framework.
func (c *Client) CompliancePosture(ctx context.Context, orgID, clusterID string) (*Posture, error) {
	out, err := Get[Posture](ctx, c, clusterPath(orgID, clusterID, "/compliance-posture"), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// CvePosture is the image-vulnerability posture.
type CvePosture struct {
	Score                   int            `json:"score"`
	RiskLevel               string         `json:"riskLevel"`
	Findings                []CveFinding   `json:"findings"`
	FindingCountsBySeverity map[string]int `json:"findingCountsBySeverity"`
	NewCountsBySeverity     map[string]int `json:"newCountsBySeverity"`
	ScannedAt               *time.Time     `json:"scannedAt"`
	LastScanID              string         `json:"lastScanId"`
	CheckedAt               *time.Time     `json:"checkedAt"`
}

// CveFinding is one vulnerability in one image.
type CveFinding struct {
	CveID            string `json:"cveId"`
	PackageName      string `json:"packageName"`
	InstalledVersion string `json:"installedVersion"`
	FixedVersion     string `json:"fixedVersion"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	Target           string `json:"target"`
	ResourceKind     string `json:"resourceKind"`
	ResourceName     string `json:"resourceName"`
}

// CvePosture returns image vulnerabilities. Empty findings mean "clean OR never scanned" —
// ScannedAt is what separates the two.
func (c *Client) CvePosture(ctx context.Context, orgID, clusterID string) (*CvePosture, error) {
	out, err := Get[CvePosture](ctx, c, clusterPath(orgID, clusterID, "/cve-posture"), nil)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- RCA

// AutoRCA is the "what is broken right now" answer.
type AutoRCA struct {
	// Degraded=false means nothing is at risk in this window, and every other field is null.
	Degraded    bool        `json:"degraded"`
	Message     string      `json:"message"`
	TopSuspect  *TopSuspect `json:"topSuspect"`
	Explanation *RCAExplain `json:"explanation"`
	GeneratedAt *time.Time  `json:"generatedAt"`
}

// TopSuspect is the auto-selected riskiest workload.
type TopSuspect struct {
	Namespace string      `json:"namespace"`
	Name      string      `json:"name"`
	Kind      string      `json:"kind"`
	UID       string      `json:"uid"`
	Score     int         `json:"score"`
	Level     string      `json:"level"`
	Reasons   []RCAReason `json:"reasons"`
}

// RCAReason is one truncated contributing factor.
type RCAReason struct {
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Category string `json:"category"`
}

// RCAExplain is the narrative layer. It may be LLM-generated, so the accounting fields
// travel with it.
type RCAExplain struct {
	Summary        string        `json:"summary"`
	RankedCauses   []RankedCause `json:"rankedCauses"`
	WhatChanged    string        `json:"whatChanged"`
	Recommendation string        `json:"recommendation"`
	LLMUsed        bool          `json:"llmUsed"`
	FromCache      bool          `json:"fromCache"`
	ModelID        string        `json:"modelId"`
	GateReason     string        `json:"gateReason"`
	CausalChain    []CausalStep  `json:"causalChain"`
	TokenUsage     *struct {
		InputTokens  int `json:"inputTokens"`
		OutputTokens int `json:"outputTokens"`
	} `json:"tokenUsage"`
}

// RankedCause is one probable cause, most likely first.
type RankedCause struct {
	Cause       string   `json:"cause"`
	Confidence  string   `json:"confidence"`
	EvidenceIDs []string `json:"evidenceIds"`
}

// CausalStep is one link in the what-happened narrative.
type CausalStep struct {
	Step        int      `json:"step"`
	Role        string   `json:"role"`
	Description string   `json:"description"`
	EvidenceIDs []string `json:"evidenceIds"`
}

// AutoRCA asks the cluster what is currently degraded.
func (c *Client) AutoRCA(ctx context.Context, orgID, clusterID, from, to string) (*AutoRCA, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	out, err := Get[AutoRCA](ctx, c, clusterPath(orgID, clusterID, "/auto-rca"), q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// WorkloadRCA is the deterministic evidence bundle for one workload.
type WorkloadRCA struct {
	Namespace          string           `json:"namespace"`
	Name               string           `json:"name"`
	Kind               string           `json:"kind"`
	K8sReason          string           `json:"k8sReason"`
	ExitCode           *int             `json:"exitCode"`
	TerminatedAt       string           `json:"terminatedAt"`
	RestartCount       int              `json:"restartCount"`
	TerminationSummary string           `json:"terminationSummary"`
	OOMKilled          bool             `json:"oomKilled"`
	WarningEvents      []RCAEvent       `json:"warningEvents"`
	LogTailLines       []string         `json:"logTailLines"`
	SpansInFlight      []RCASpan        `json:"spansInFlight"`
	TermLogsAvailable  bool             `json:"termLogsAvailable"`
	TracesAvailable    bool             `json:"tracesAvailable"`
	Upstream           []DependencyEdge `json:"upstream"`
	Downstream         []DependencyEdge `json:"downstream"`
	ErrorRatePct       float64          `json:"errorRatePct"`
	P99LatencyMs       int64            `json:"p99LatencyMs"`
	RequestCount       int64            `json:"requestCount"`
	AppHealthAvailable bool             `json:"appHealthAvailable"`
}

// RCAEvent is a correlated warning event.
type RCAEvent struct {
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Type          string `json:"type"`
	LastTimestamp string `json:"lastTimestamp"`
}

// RCASpan is a trace span correlated to the failure window.
type RCASpan struct {
	TraceID    string `json:"traceId"`
	SpanName   string `json:"spanName"`
	StatusCode string `json:"statusCode"`
	DurationMs int64  `json:"durationMs"`
}

// DependencyEdge is a traffic edge to or from the workload.
type DependencyEdge struct {
	Namespace string `json:"namespace"`
	OwnerKind string `json:"ownerKind"`
	OwnerName string `json:"ownerName"`
	Bytes     int64  `json:"bytes"`
}

// WorkloadRCA fetches the evidence bundle for one workload.
func (c *Client) WorkloadRCA(ctx context.Context, orgID, clusterID, namespace, name, kind string) (*WorkloadRCA, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	path := clusterPath(orgID, clusterID, fmt.Sprintf("/workloads/%s/%s/rca",
		url.PathEscape(namespace), url.PathEscape(name)))
	out, err := Get[WorkloadRCA](ctx, c, path, q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ExplainRCA asks for the narrative. force bypasses the result cache, which means a fresh
// LLM call and fresh token cost — never default it on.
func (c *Client) ExplainRCA(ctx context.Context, orgID, clusterID, namespace, name, kind string, force bool) (*RCAExplain, error) {
	q := url.Values{}
	if kind != "" {
		q.Set("kind", kind)
	}
	if force {
		q.Set("force", "true")
	}
	path := clusterPath(orgID, clusterID, fmt.Sprintf("/workloads/%s/%s/rca/explain",
		url.PathEscape(namespace), url.PathEscape(name)))
	out, err := Post[RCAExplain](ctx, c, path, q, map[string]any{})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- golden signals

// SlowOperations is the slowest operations in a window.
type SlowOperations struct {
	Items  []SlowOperation `json:"items"`
	Window *SignalWindow   `json:"window"`
}

// SlowOperation is one operation's latency profile.
type SlowOperation struct {
	ServiceName  string  `json:"serviceName"`
	Operation    string  `json:"operation"`
	SpanKind     string  `json:"spanKind"`
	P99Ms        float64 `json:"p99Ms"`
	P50Ms        float64 `json:"p50Ms"`
	CallCount    int64   `json:"callCount"`
	ErrorRatePct float64 `json:"errorRatePct"`
	Namespace    string  `json:"namespace"`
	WorkloadKind string  `json:"workloadKind"`
}

// SignalWindow is the time range a signal result covers.
type SignalWindow struct {
	From *time.Time `json:"from"`
	To   *time.Time `json:"to"`
}

// SlowTraces is the slowest individual traces in a window.
type SlowTraces struct {
	Items  []SlowTrace   `json:"items"`
	Window *SignalWindow `json:"window"`
}

// SlowTrace is one slow trace with the service the backend suspects.
type SlowTrace struct {
	TraceID          string  `json:"traceId"`
	RootOperation    string  `json:"rootOperation"`
	RootService      string  `json:"rootService"`
	TotalMs          float64 `json:"totalMs"`
	WallClockMs      float64 `json:"wallClockMs"`
	ServiceCount     int64   `json:"serviceCount"`
	HasError         bool    `json:"hasError"`
	StartTime        string  `json:"startTime"`
	SuspectService   string  `json:"suspectService"`
	SuspectNamespace string  `json:"suspectNamespace"`
	SuspectKind      string  `json:"suspectKind"`
}

func windowQuery(from, to string, limit int) url.Values {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

// SlowOperations lists the slowest operations by p99.
func (c *Client) SlowOperations(ctx context.Context, orgID, clusterID, from, to string, limit int) (*SlowOperations, error) {
	out, err := Get[SlowOperations](ctx, c, clusterPath(orgID, clusterID, "/slow-operations"), windowQuery(from, to, limit))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// SlowTraces lists the slowest traces.
func (c *Client) SlowTraces(ctx context.Context, orgID, clusterID, from, to string, limit int) (*SlowTraces, error) {
	out, err := Get[SlowTraces](ctx, c, clusterPath(orgID, clusterID, "/slow-traces"), windowQuery(from, to, limit))
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------- changes

// Changes is a page of change events.
type Changes struct {
	Items []ChangeEvent `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	Size  int           `json:"size"`
}

// ChangeEvent is one observed change to a resource.
type ChangeEvent struct {
	UID           string        `json:"uid"`
	Kind          string        `json:"kind"`
	Namespace     string        `json:"namespace"`
	Name          string        `json:"name"`
	ChangeType    string        `json:"changeType"`
	FieldManager  string        `json:"fieldManager"`
	FieldChanges  []FieldChange `json:"fieldChanges"`
	ChangeSummary string        `json:"changeSummary"`
	Revision      string        `json:"revision"`
	PrevRevision  string        `json:"prevRevision"`
	ChangedAt     *time.Time    `json:"changedAt"`
}

// FieldChange is one modified path.
type FieldChange struct {
	Path     string `json:"path"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
}

// Changes lists recent changes across the cluster.
func (c *Client) Changes(ctx context.Context, orgID, clusterID string, sinceHours, page, size int) (*Changes, error) {
	q := url.Values{}
	if sinceHours > 0 {
		q.Set("sinceHours", strconv.Itoa(sinceHours))
	}
	q.Set("page", strconv.Itoa(page))
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	out, err := Get[Changes](ctx, c, clusterPath(orgID, clusterID, "/changes"), q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// WorkloadChanges lists changes for one workload.
func (c *Client) WorkloadChanges(ctx context.Context, orgID, clusterID, namespace, name string, page, size int) (*Changes, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	if size > 0 {
		q.Set("size", strconv.Itoa(size))
	}
	path := clusterPath(orgID, clusterID, fmt.Sprintf("/workloads/%s/%s/changes",
		url.PathEscape(namespace), url.PathEscape(name)))
	out, err := Get[Changes](ctx, c, path, q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
