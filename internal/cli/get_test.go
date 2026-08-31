package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// resourceServer answers the cluster resource routes for org1/c1.
func resourceServer(t *testing.T, routes map[string]any) *httptest.Server {
	t.Helper()
	srv := backend(t, routes)
	return srv
}

func isolateCluster(t *testing.T, srv *httptest.Server) {
	t.Helper()
	isolate(t, srv)
	t.Setenv("RTZ_CLUSTER", "c1")
}

const resourcesPath = "/eac/api/1.0/orgs/org1/clusters/c1/resources"

func TestGetPodsRendersDerivedColumns(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		resourcesPath: []map[string]any{{
			"kind": "Pod", "namespace": "payments", "name": "checkout-abc",
			"spec":   `{"nodeName":"node-1"}`,
			"status": `{"phase":"Running","restartCount":3}`,
		}},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "pods")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"NAMESPACE", "payments", "checkout-abc", "Running", "3"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// -n <ns> narrows the query, and the NAMESPACE column is then redundant noise.
func TestGetWithNamespaceDropsTheNamespaceColumn(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{
			{"kind": "Pod", "namespace": "payments", "name": "checkout-abc", "status": `{"phase":"Running"}`},
		}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "pods", "-n", "payments")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(gotQuery, "namespace=payments") {
		t.Errorf("namespace was not sent to the API: %q", gotQuery)
	}
	if strings.Contains(out, "NAMESPACE") {
		t.Errorf("single-namespace listing still printed the NAMESPACE column:\n%s", out)
	}
}

// The API filters kind by literal equality, so a short form must be translated or the query
// silently returns nothing.
func TestGetTranslatesKindAliases(t *testing.T) {
	var gotKind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKind = r.URL.Query().Get("kind")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	for alias, want := range map[string]string{
		"po": "Pod", "deploy": "Deployment", "sts": "StatefulSet",
		"ds": "DaemonSet", "svc": "Service", "ing": "Ingress", "no": "Node",
	} {
		if _, code := run(t, "get", alias); code != ExitOK {
			t.Fatalf("get %s exited %d", alias, code)
		}
		if gotKind != want {
			t.Errorf("get %s sent kind=%q, want %q", alias, gotKind, want)
		}
	}
}

func TestGetAllClearsTheKindFilter(t *testing.T) {
	var hadKind bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadKind = r.URL.Query()["kind"]
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "get", "all"); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if hadKind {
		t.Error("`get all` sent a kind filter")
	}
}

func TestGetRejectsUnknownKind(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "wibble")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage):\n%s", code, ExitUsage, out)
	}
}

// An exact CRD kind the alias table has never heard of must still work.
func TestGetPassesThroughAnExactKind(t *testing.T) {
	var gotKind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKind = r.URL.Query().Get("kind")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]any{}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "get", "NodePool"); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if gotKind != "NodePool" {
		t.Errorf("kind = %q, want NodePool", gotKind)
	}
}

func TestGetNameArgumentFiltersClientSide(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		resourcesPath: []map[string]any{
			{"kind": "Deployment", "namespace": "a", "name": "web", "spec": `{"replicas":1}`, "status": `{"readyReplicas":1}`},
			{"kind": "Deployment", "namespace": "a", "name": "api", "spec": `{"replicas":2}`, "status": `{"readyReplicas":2}`},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "deploy", "web")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "web") || strings.Contains(out, "api") {
		t.Errorf("name filter did not narrow the list:\n%s", out)
	}
}

func TestGetLabelSelector(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		resourcesPath: []map[string]any{
			{"kind": "Pod", "namespace": "a", "name": "web-1", "labels": `{"app":"web"}`, "status": `{"phase":"Running"}`},
			{"kind": "Pod", "namespace": "a", "name": "db-1", "labels": `{"app":"db"}`, "status": `{"phase":"Running"}`},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "pods", "-l", "app=web")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "web-1") || strings.Contains(out, "db-1") {
		t.Errorf("selector did not filter:\n%s", out)
	}
}

// Set-based selectors are not supported; quietly ignoring one would return the wrong rows
// while looking like it worked.
func TestGetRejectsSetBasedSelector(t *testing.T) {
	srv := resourceServer(t, map[string]any{resourcesPath: []map[string]any{}})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "get", "pods", "-l", "app in (web,api)"); code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

// The server caps at 5000 rows with no pagination. Presenting that as the whole cluster is
// the failure this warning exists to prevent.
func TestGetWarnsWhenTheServerRowCapIsHit(t *testing.T) {
	rows := make([]map[string]any, 5000)
	for i := range rows {
		rows[i] = map[string]any{"kind": "Pod", "namespace": "a", "name": "p", "status": `{"phase":"Running"}`}
	}
	srv := resourceServer(t, map[string]any{resourcesPath: rows})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "pods")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "TRUNCATED") {
		t.Errorf("no truncation warning at the row cap")
	}
}

func TestGetBelowCapDoesNotWarn(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		resourcesPath: []map[string]any{{"kind": "Pod", "namespace": "a", "name": "p", "status": `{"phase":"Running"}`}},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "get", "pods")
	if strings.Contains(out, "TRUNCATED") {
		t.Errorf("warned about truncation on a 1-row result:\n%s", out)
	}
}

func TestGetJSONIsTheRawRows(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		resourcesPath: []map[string]any{
			{"kind": "Pod", "namespace": "a", "name": "p1", "status": `{"phase":"Running"}`},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "get", "pods", "-o", "json")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not a bare array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["name"] != "p1" {
		t.Errorf("payload = %+v", got)
	}
}

func TestGetRequiresACluster(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv) // no RTZ_CLUSTER

	if _, code := run(t, "get", "pods"); code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestSearchRendersHits(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/resources/search": []map[string]any{
			{"kind": "Deployment", "namespace": "payments", "name": "checkout"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "search", "check")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "checkout") || !strings.Contains(out, "payments") {
		t.Errorf("hit not rendered:\n%s", out)
	}
}

func TestNamespacesSorted(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/namespaces": []string{"zoo", "apps", "kube-system"},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "ns")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Index(out, "apps") > strings.Index(out, "zoo") {
		t.Errorf("namespaces not sorted:\n%s", out)
	}
}

func TestCountsSortedDescendingWithTotal(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/counts": map[string]int64{"Pod": 120, "Deployment": 14, "Service": 30},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "counts")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Index(out, "Pod") > strings.Index(out, "Deployment") {
		t.Errorf("counts not ordered by size:\n%s", out)
	}
	if !strings.Contains(out, "164") {
		t.Errorf("total row missing or wrong (want 164):\n%s", out)
	}
}

func TestDescribeRendersSectionsWithZeroCounts(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/workloads/payments/checkout/detail": map[string]any{
			"workload": map[string]any{
				"kind": "Deployment", "namespace": "payments", "name": "checkout",
				"spec":   `{"replicas":3,"template":{"spec":{"containers":[{"image":"checkout:1.4.2"}]}}}`,
				"status": `{"readyReplicas":2}`, "labels": `{"app":"checkout"}`,
			},
			"pods": []map[string]any{
				{"kind": "Pod", "namespace": "payments", "name": "checkout-1",
					"status": `{"phase":"CrashLoopBackOff","restartCount":12}`},
			},
			"events": []map[string]any{
				{"type": "Warning", "reason": "BackOff", "message": "Back-off restarting failed container", "count": 9},
			},
			"services":  []map[string]any{},
			"ingresses": []map[string]any{},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "describe", "payments/checkout")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"payments/checkout", "2/3", "checkout:1.4.2",
		"Pods (1)", "CrashLoopBackOff", "12", "Events (1)", "BackOff", "Services (0)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDescribeAcceptsDashN(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/workloads/payments/checkout/detail": map[string]any{
			"workload": map[string]any{"kind": "Deployment", "namespace": "payments", "name": "checkout",
				"spec": `{"replicas":1}`, "status": `{"readyReplicas":1}`},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "describe", "checkout", "-n", "payments"); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
}

func TestDescribeWithoutANamespaceIsAUsageError(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "describe", "checkout"); code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestDescribeConflictingNamespacesIsAUsageError(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "describe", "a/checkout", "-n", "b"); code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

// A missing workload returns 200 with a null body, not a 404 — so it has to be reported as
// a miss rather than rendered as an empty-but-fine workload.
func TestDescribeMissingWorkloadFails(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/c1/workloads/payments/ghost/detail": map[string]any{"workload": nil},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "describe", "payments/ghost")
	if code == ExitOK {
		t.Fatalf("a missing workload exited 0:\n%s", out)
	}
	if !strings.Contains(out, "no workload") {
		t.Errorf("unhelpful message:\n%s", out)
	}
}

func TestFleetSummary(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/fleet-summary": map[string]any{
			"fleetRiskScore": 72, "fleetRiskLevel": "HIGH",
			"findingsBySeverity":  map[string]int{"crit": 2, "high": 5, "med": 9, "low": 20},
			"clusters":            map[string]int{"total": 4, "connected": 3, "degraded": 1},
			"releases7d":          map[string]int{"healthy": 6, "degraded": 1, "total": 7},
			"clusterLastVerdicts": map[string]string{"c1": "HEALTHY", "c2": "DEGRADED"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "fleet")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"72/100", "HIGH", "higher is worse", "4 total", "2 critical", "c2", "DEGRADED"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A null score means "no data", and printing 0/100 would read as a perfectly healthy fleet.
func TestFleetWithNoScoreSaysNoData(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/fleet-summary": map[string]any{
			"fleetRiskScore": nil,
			"clusters":       map[string]int{"total": 0},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "fleet")
	if !strings.Contains(out, "<no data>") {
		t.Errorf("null score did not render as no-data:\n%s", out)
	}
	if strings.Contains(out, "0/100") {
		t.Errorf("null score rendered as a healthy 0/100:\n%s", out)
	}
}
