package api

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ---------------------------------------------------------------- logs

// LogSearch is the log query. Timestamps are epoch milliseconds, matching the backend DTO.
type LogSearch struct {
	StartDate      int64  `json:"startDate"`
	EndDate        int64  `json:"endDate"`
	Query          string `json:"query,omitempty"`
	ServiceName    string `json:"serviceName,omitempty"`
	ClusterID      string `json:"clusterId,omitempty"`
	ClusterName    string `json:"clusterName,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	Offset         int    `json:"offset,omitempty"`
	SeverityFilter string `json:"severityFilter,omitempty"`
	// FilterQuery is the Datadog-style language: free text, field:value, @attributes,
	// wildcards, ranges, AND/OR/NOT. The server rejects an unparseable query with a 400,
	// which is better than silently matching everything.
	FilterQuery string `json:"filterQuery,omitempty"`
}

// LogPage is one page of results. NextOffset is null on the last page.
type LogPage struct {
	Data       []LogRecord `json:"data"`
	Total      int64       `json:"total"`
	NextOffset *int        `json:"nextOffset"`
}

// LogRecord is one log line. Field names follow the repository's SELECT aliases.
type LogRecord struct {
	TS            string `json:"ts"`
	TraceID       string `json:"traceId"`
	SpanID        string `json:"spanId"`
	ServiceName   string `json:"serviceName"`
	Level         string `json:"level"`
	Body          string `json:"body"`
	ExceptionType string `json:"exceptionType"`
	StackTrace    string `json:"stackTrace"`
	ScopeName     string `json:"scopeName"`
}

// SearchLogs runs a log query.
func (c *Client) SearchLogs(ctx context.Context, req LogSearch) (*LogPage, error) {
	out, err := Post[LogPage](ctx, c, "/eac/api/v1/logs", nil, req)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LogServices lists services that produced logs in a window.
func (c *Client) LogServices(ctx context.Context, clusterID string, startMs, endMs int64) ([]string, error) {
	q := url.Values{
		"startMs": {strconv.FormatInt(startMs, 10)},
		"endMs":   {strconv.FormatInt(endMs, 10)},
	}
	if clusterID != "" {
		q.Set("clusterId", clusterID)
	}
	return Get[[]string](ctx, c, "/eac/api/v1/logs/services", q)
}

// LogsForTrace returns the logs correlated to a trace.
func (c *Client) LogsForTrace(ctx context.Context, traceID string) ([]LogRecord, error) {
	return Get[[]LogRecord](ctx, c,
		"/eac/api/v1/logs/trace/"+url.PathEscape(traceID), nil)
}

// ---------------------------------------------------------------- traces

// TraceListRequest queries the trace index. Timestamps are epoch milliseconds.
type TraceListRequest struct {
	StartDate   int64  `json:"startDate"`
	EndDate     int64  `json:"endDate"`
	Query       string `json:"query,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	Offset      int    `json:"offset,omitempty"`
	ClusterID   string `json:"clusterId,omitempty"`
	ClusterName string `json:"clusterName,omitempty"`
	SortBy      string `json:"sortBy,omitempty"`
	SortOrder   string `json:"sortOrder,omitempty"`
}

// TracePage is one page of trace rows.
type TracePage struct {
	Data       []TraceRow `json:"data"`
	Total      int64      `json:"total"`
	NextOffset *int       `json:"nextOffset"`
}

// TraceRow is one root span in the trace list.
type TraceRow struct {
	Timestamp    string  `json:"timestamp"`
	TraceID      string  `json:"traceID"`
	SpanID       string  `json:"spanID"`
	ServiceName  string  `json:"serviceName"`
	Name         string  `json:"name"`
	DurationNano int64   `json:"durationNano"`
	StatusCode   int     `json:"statusCode"`
	HasError     bool    `json:"hasError"`
	RiskScore    float64 `json:"riskScore"`
	QualityScore float64 `json:"qualityScore"`
}

// DurationMs converts the nanosecond duration the index stores.
func (t TraceRow) DurationMs() float64 { return float64(t.DurationNano) / 1e6 }

// ListTraces queries the trace index.
func (c *Client) ListTraces(ctx context.Context, req TraceListRequest) (*TracePage, error) {
	out, err := Post[TracePage](ctx, c, "/eac/api/v1/trace/list", nil, req)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Trace fetches every span of one trace. The payload is open-ended, so it stays a map.
func (c *Client) Trace(ctx context.Context, traceID string) (map[string]any, error) {
	return Get[map[string]any](ctx, c, "/eac/api/v1/trace/"+url.PathEscape(traceID), nil)
}

// AnalyzeTrace asks the backend to interpret one trace.
func (c *Client) AnalyzeTrace(ctx context.Context, traceID, envName string) (map[string]any, error) {
	body := map[string]string{}
	if envName != "" {
		body["envName"] = envName
	}
	return Post[map[string]any](ctx, c,
		"/eac/api/v1/trace/"+url.PathEscape(traceID)+"/analyze", nil, body)
}

// ---------------------------------------------------------------- metrics

// MetricsQuery is a time-series request.
type MetricsQuery struct {
	EntityType    string      `json:"entityType"`
	MetricName    string      `json:"metricName"`
	From          string      `json:"from"`
	To            string      `json:"to"`
	Filters       []TagFilter `json:"filters,omitempty"`
	GroupBy       []string    `json:"groupBy,omitempty"`
	AggType       string      `json:"aggType,omitempty"`
	BucketSeconds *int        `json:"bucketSeconds,omitempty"`
	EntityID      string      `json:"entityId,omitempty"`
}

// TagFilter narrows a metrics query.
type TagFilter struct {
	Key   string `json:"key"`
	Op    string `json:"op,omitempty"`
	Value string `json:"value"`
}

// MetricsResult is a set of time series.
type MetricsResult struct {
	BucketSeconds int            `json:"bucketSeconds"`
	IsRate        bool           `json:"isRate"`
	EffectiveUnit string         `json:"effectiveUnit"`
	Series        []MetricSeries `json:"series"`
}

// MetricSeries is one grouped series.
type MetricSeries struct {
	GroupKey map[string]string `json:"groupKey"`
	Label    string            `json:"label"`
	Points   []MetricPoint     `json:"points"`
}

// MetricPoint is one bucket.
type MetricPoint struct {
	T string  `json:"t"`
	V float64 `json:"v"`
}

func obsPath(orgID, suffix string) string {
	return fmt.Sprintf("/eac/api/1.0/orgs/%s/observability%s", orgID, suffix)
}

// QueryMetrics runs a time-series query.
func (c *Client) QueryMetrics(ctx context.Context, orgID string, q MetricsQuery) (*MetricsResult, error) {
	out, err := Post[MetricsResult](ctx, c, obsPath(orgID, "/query"), nil, q)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// MetricNames lists available metrics for one entity type.
//
// entityType is REQUIRED by the backend — omitting it yields a 500, not a validation error,
// so callers must supply it.
func (c *Client) MetricNames(ctx context.Context, orgID, entityType string) ([]map[string]any, error) {
	return Get[[]map[string]any](ctx, c, obsPath(orgID, "/metric-names"),
		url.Values{"entityType": {entityType}})
}

// Entities lists the entities of one type that have reported metrics.
func (c *Client) Entities(ctx context.Context, orgID, entityType string) ([]map[string]any, error) {
	return Get[[]map[string]any](ctx, c, obsPath(orgID, "/entities"),
		url.Values{"entityType": {entityType}})
}

// TagKeys lists the tag keys available for a metric.
func (c *Client) TagKeys(ctx context.Context, orgID, entityType, metricName string) ([]string, error) {
	q := url.Values{"entityType": {entityType}}
	if metricName != "" {
		q.Set("metricName", metricName)
	}
	return Get[[]string](ctx, c, obsPath(orgID, "/tag-keys"), q)
}

// TagValues lists the values seen for one tag key.
func (c *Client) TagValues(ctx context.Context, orgID, entityType, metricName, tagKey string) ([]string, error) {
	// The backend names this parameter "key"; sending "tagKey" produces a 500, not a 400.
	q := url.Values{"key": {tagKey}, "entityType": {entityType}}
	if metricName != "" {
		q.Set("metricName", metricName)
	}
	return Get[[]string](ctx, c, obsPath(orgID, "/tag-values"), q)
}

// EpochMs is the millisecond timestamp the log and trace APIs expect.
func EpochMs(t time.Time) int64 { return t.UnixNano() / int64(time.Millisecond) }
