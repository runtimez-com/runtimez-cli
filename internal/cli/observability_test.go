package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"15m": 15 * time.Minute,
		"2h":  2 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"90":  90 * time.Minute, // bare digits read as minutes
		"":    0,
	}
	for in, want := range cases {
		got, err := parseSince(in)
		if err != nil || got != want {
			t.Errorf("parseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"soon", "1 week", "-", "5x"} {
		if _, err := parseSince(bad); err == nil {
			t.Errorf("parseSince(%q) accepted nonsense", bad)
		}
	}
}

func TestTimeWindowRejectsInvertedRange(t *testing.T) {
	from := time.Now().Format(time.RFC3339)
	to := time.Now().Add(-time.Hour).Format(time.RFC3339)
	if _, _, err := timeWindow("", from, to); err == nil {
		t.Error("accepted a window whose start is after its end")
	}
}

func TestLogsSearchSendsWindowAndFilters(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"data": []map[string]any{
				{"ts": "2026-08-30T10:00:00Z", "level": "ERROR", "serviceName": "checkout",
					"body": "connection refused", "traceId": "t1"},
			},
			"total": 1,
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "logs", "--since", "15m", "--level", "ERROR", "-s", "checkout", "-q", "status:5*")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if got["severityFilter"] != "ERROR" || got["serviceName"] != "checkout" || got["filterQuery"] != "status:5*" {
		t.Errorf("request lost filters: %+v", got)
	}
	// The window has to reach the server as epoch millis, and be roughly 15 minutes wide.
	start, end := got["startDate"].(float64), got["endDate"].(float64)
	if span := (end - start) / 1000 / 60; span < 14 || span > 16 {
		t.Errorf("window was %.1f minutes, want ~15", span)
	}
	// The cluster scope must travel too, or a multi-cluster org gets everyone's logs.
	if got["clusterId"] != "c1" {
		t.Errorf("cluster scope not sent: %+v", got["clusterId"])
	}
	for _, want := range []string{"Window:", "ERROR", "checkout", "connection refused"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLogsReportsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"data":  []map[string]any{{"ts": "t", "body": "one", "level": "INFO"}},
			"total": 4021,
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "logs")
	if !strings.Contains(out, "of 4021") {
		t.Errorf("did not report how many lines matched:\n%s", out)
	}
}

// --follow renders a running tail; a structured format cannot express that, and silently
// printing one page instead would look like the follow simply found nothing.
func TestFollowRejectsStructuredOutput(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "logs", "--follow", "-o", "json"); code != ExitUsage {
		t.Errorf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestTraceListRendersAndSortsByDuration(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"data": []map[string]any{
				{"traceID": "abc", "serviceName": "gateway", "name": "GET /cart",
					"durationNano": 1_500_000_000, "hasError": true, "timestamp": "2026-08-30T10:00:00Z"},
			},
			"total": 1,
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "trace", "list", "--since", "2h")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if got["sortBy"] != "durationNano" || got["sortOrder"] != "desc" {
		t.Errorf("slowest-first ordering not requested: %+v", got)
	}
	// durationNano is nanoseconds; 1.5e9 is 1.50s, not 1500000000 of anything.
	if !strings.Contains(out, "1.50s") {
		t.Errorf("nanosecond duration not converted:\n%s", out)
	}
	if !strings.Contains(out, "abc") || !strings.Contains(out, "gateway") {
		t.Errorf("trace row missing:\n%s", out)
	}
}

func TestTraceListErrorsFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"data": []map[string]any{
				{"traceID": "ok-1", "hasError": false, "durationNano": 1000},
				{"traceID": "bad-1", "hasError": true, "durationNano": 2000},
			},
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "trace", "list", "--errors")
	if !strings.Contains(out, "bad-1") || strings.Contains(out, "ok-1") {
		t.Errorf("--errors did not filter:\n%s", out)
	}
}

func TestTraceGetRendersASpanTree(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/v1/trace/abc": map[string]any{
			"spans": []map[string]any{
				{"id": "s1", "parentId": "", "name": "GET /cart", "serviceName": "gateway",
					"durationNano": 900_000_000, "timestamp": "2026-08-30T10:00:00Z"},
				{"id": "s2", "parentId": "s1", "name": "checkout.Pay", "serviceName": "checkout",
					"durationNano": 700_000_000, "hasError": true, "timestamp": "2026-08-30T10:00:01Z"},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "trace", "get", "abc")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "2 spans") {
		t.Errorf("span count missing:\n%s", out)
	}
	// The child must be indented under its parent, and the error marked.
	lines := strings.Split(out, "\n")
	var parent, child string
	for _, l := range lines {
		if strings.Contains(l, "GET /cart") {
			parent = l
		}
		if strings.Contains(l, "checkout.Pay") {
			child = l
		}
	}
	if parent == "" || child == "" {
		t.Fatalf("spans not rendered:\n%s", out)
	}
	if indent(child) <= indent(parent) {
		t.Errorf("child span not nested under its parent:\n%s", out)
	}
	if !strings.Contains(child, "ERROR") {
		t.Errorf("errored span not marked:\n%s", child)
	}
	// A root span must start at column zero, or nesting is unreadable.
	if indent(parent) != 0 {
		t.Errorf("root span is indented:\n%q", parent)
	}
}

func indent(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// A span whose parent is missing from the payload is still shown; treating it as
// unreachable would render a partial trace as an empty one.
func TestTraceGetShowsOrphanedSpans(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/v1/trace/abc": map[string]any{
			"spans": []map[string]any{
				{"id": "s2", "parentId": "missing", "name": "orphan", "serviceName": "svc", "durationNano": 1000},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "trace", "get", "abc")
	if !strings.Contains(out, "orphan") {
		t.Errorf("orphaned span dropped:\n%s", out)
	}
}

func TestTraceGetUnreadablePayloadSaysSo(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/v1/trace/abc": map[string]any{"unexpected": "shape"},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "trace", "get", "abc")
	if !strings.Contains(out, "no spans this client could read") {
		t.Errorf("an unreadable payload did not say so:\n%s", out)
	}
	if !strings.Contains(out, "-o json") {
		t.Errorf("no escape hatch offered:\n%s", out)
	}
}

func TestMetricsQuerySummarisesSeries(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{
			"bucketSeconds": 60, "isRate": false, "effectiveUnit": "By",
			"series": []map[string]any{{
				"label": "pod=checkout",
				"points": []map[string]any{
					{"t": "2026-08-30T10:00:00Z", "v": 100.0},
					{"t": "2026-08-30T10:01:00Z", "v": 300.0},
					{"t": "2026-08-30T10:02:00Z", "v": 200.0},
				},
			}},
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "metrics", "query", "k8s.pod.memory.usage", "--entity-type", "K8S_POD",
		"--agg", "max", "--group-by", "k8s.pod.name", "--filter", "namespace=payments")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if got["metricName"] != "k8s.pod.memory.usage" || got["aggType"] != "max" {
		t.Errorf("query not sent faithfully: %+v", got)
	}
	filters := got["filters"].([]any)
	if len(filters) != 1 || filters[0].(map[string]any)["key"] != "namespace" {
		t.Errorf("filters not parsed: %+v", got["filters"])
	}
	// last/min/max, not an invented average.
	for _, want := range []string{"pod=checkout", "200", "100", "300", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A rate rewrite changes what the numbers mean, so it cannot stay implicit.
func TestMetricsQueryAnnouncesARateRewrite(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/observability/query": map[string]any{
			"bucketSeconds": 60, "isRate": true, "effectiveUnit": "By/s",
			"series": []map[string]any{{"label": "x", "points": []map[string]any{{"t": "t", "v": 1.0}}}},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "metrics", "query", "net.bytes", "--entity-type", "K8S_POD")
	if !strings.Contains(out, "per-second RATE") {
		t.Errorf("rate rewrite not announced:\n%s", out)
	}
}

// A filter that silently fails to apply returns more data than asked for, which reads as a
// bigger problem than there is.
func TestMetricsRejectsMalformedFilter(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "metrics", "query", "m", "--entity-type", "K8S_POD", "--filter", "namespace"); code != ExitUsage {
		t.Errorf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestMetricsListSorted(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/observability/metric-names": []map[string]any{
			{"metricName": "z.metric", "entityType": "K8S_POD", "unit": "By"},
			{"metricName": "a.metric", "entityType": "K8S_POD", "unit": "1"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "metrics", "list", "--entity-type", "K8S_POD")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Index(out, "a.metric") > strings.Index(out, "z.metric") {
		t.Errorf("metric names not sorted:\n%s", out)
	}
}

func TestMetricsTagsSwitchesBetweenKeysAndValues(t *testing.T) {
	var hit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []string{"payments"}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "metrics", "tags", "--entity-type", "K8S_POD"); code != ExitOK {
		t.Fatalf("tags exited non-zero")
	}
	if !strings.HasSuffix(hit, "/tag-keys") {
		t.Errorf("bare tags hit %q, want tag-keys", hit)
	}
	if _, code := run(t, "metrics", "tags", "--entity-type", "K8S_POD", "--key", "namespace"); code != ExitOK {
		t.Fatalf("tags --key exited non-zero")
	}
	if !strings.HasSuffix(hit, "/tag-values") {
		t.Errorf("tags --key hit %q, want tag-values", hit)
	}
}

// --- ask --------------------------------------------------------------------

// askServer streams the given frames, then answers. The stream must be subscribed before
// the query is issued, which this asserts by ordering.
func askServer(t *testing.T, frames []string, answer map[string]any) (*httptest.Server, func() bool) {
	t.Helper()
	subscribed := make(chan struct{})
	var queried bool
	var streamFirst = true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/agent/stream/") && r.Method == http.MethodGet:
			close(subscribed)
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			for _, f := range frames {
				fmt.Fprint(w, f)
				if flusher != nil {
					flusher.Flush()
				}
			}
		case strings.HasSuffix(r.URL.Path, "/agent/query"):
			select {
			case <-subscribed:
			case <-time.After(2 * time.Second):
				streamFirst = false
			}
			queried = true
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": answer})
		default:
			json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{}})
		}
	}))
	return srv, func() bool { return queried && streamFirst }
}

func TestAskStreamsStepsThenPrintsTheAnswer(t *testing.T) {
	frames := []string{
		"event: step\ndata: {\"step\":1,\"phase\":\"thinking\",\"thought\":\"check restarts\",\"tool\":\"getPods\"}\n\n",
		"event: step\ndata: {\"step\":1,\"phase\":\"observation\",\"tool\":\"getPods\",\"resultSummary\":\"3 pods, 1 crashlooping\"}\n\n",
		"event: done\ndata: {}\n\n",
	}
	srv, ok := askServer(t, frames, map[string]any{
		"answer":    "checkout is OOMKilling.",
		"sessionId": "s-1",
		"toolsUsed": []string{"getPods"},
		"verdict": map[string]any{
			"headline": "checkout is out of memory", "workload": "payments/checkout",
			"confidence": "HIGH", "blastRadius": "one service",
		},
		"suggestedCommands": []map[string]any{{"label": "Raise the limit", "command": "kubectl set resources ..."}},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "ask", "why is checkout failing")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !ok() {
		t.Error("the query was issued before the stream was subscribed — early frames would be lost")
	}
	for _, want := range []string{"check restarts", "getPods", "3 pods, 1 crashlooping",
		"checkout is out of memory", "HIGH", "checkout is OOMKilling.", "Raise the limit", "s-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// --quiet drops the reasoning but must keep the answer.
func TestAskQuietSuppressesStepsOnly(t *testing.T) {
	frames := []string{
		"event: step\ndata: {\"step\":1,\"phase\":\"thinking\",\"thought\":\"internal reasoning\"}\n\n",
		"event: done\ndata: {}\n\n",
	}
	srv, _ := askServer(t, frames, map[string]any{"answer": "the answer"})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "ask", "--quiet", "why")
	if strings.Contains(out, "internal reasoning") {
		t.Errorf("--quiet still printed reasoning:\n%s", out)
	}
	if !strings.Contains(out, "the answer") {
		t.Errorf("--quiet suppressed the answer too:\n%s", out)
	}
}

// A partial investigation reads exactly like a complete one unless it says otherwise.
func TestAskWarnsOnStepLimit(t *testing.T) {
	srv, _ := askServer(t, []string{"event: done\ndata: {}\n\n"},
		map[string]any{"answer": "partial", "hitStepLimit": true})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "ask", "why")
	if !strings.Contains(out, "PARTIAL investigation") {
		t.Errorf("step-limit truncation not surfaced:\n%s", out)
	}
}

func TestAskSendsSessionContinuation(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/agent/query") {
			json.NewDecoder(r.Body).Decode(&body)
			json.NewEncoder(w).Encode(map[string]any{"success": true,
				"data": map[string]any{"answer": "ok", "sessionId": "s-9"}})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: done\ndata: {}\n\n")
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "ask", "--session", "s-9", "follow up"); code != ExitOK {
		t.Fatal("ask --session failed")
	}
	if body["sessionId"] != "s-9" {
		t.Errorf("session not continued: %+v", body)
	}
	if body["streamId"] == nil || body["streamId"] == "" {
		t.Error("no streamId sent — the stream could not be correlated to the run")
	}
}

// The backend answers a missing entityType with a 500, which reads as a server fault. The
// CLI must catch it first and name the flag.
func TestMetricsRequiresEntityType(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	for _, args := range [][]string{
		{"metrics", "list"},
		{"metrics", "tags"},
		{"metrics", "entities"},
		{"metrics", "query", "some.metric"},
	} {
		out, code := run(t, args...)
		if code != ExitUsage {
			t.Errorf("%v exited %d, want %d (usage)", args, code, ExitUsage)
		}
		if !strings.Contains(out, "--entity-type is required") {
			t.Errorf("%v did not name the missing flag:\n%s", args, out)
		}
	}
}

// tag-values is keyed on "key"; sending "tagKey" produces a 500.
func TestTagValuesUsesTheKeyParameter(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []string{"payments"}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "metrics", "tags", "--entity-type", "K8S_POD", "--key", "namespace"); code != ExitOK {
		t.Fatal("tag values failed")
	}
	if !strings.Contains(query, "key=namespace") {
		t.Errorf("query = %q, want key=namespace", query)
	}
	if strings.Contains(query, "tagKey=") {
		t.Errorf("sent the wrong parameter name: %q", query)
	}
	if !strings.Contains(query, "entityType=K8S_POD") {
		t.Errorf("entityType not sent: %q", query)
	}
}
