package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// capture records one query parameter of every request and answers with an empty payload.
func capture(t *testing.T, got *string, param string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = r.URL.Query().Get(param)
		w.Write([]byte(`{"success":true,"data":{}}`))
	}))
}

const maintainBase = "/eac/api/1.0/orgs/org1/maintain"

func maintainItems(readFailed bool) map[string]any {
	return map[string]any{
		"readFailed": readFailed,
		"items": []map[string]any{
			{"clusterName": "prod", "itemType": "RUNTIME", "displayName": "Node.js 18", "version": "18.20",
				"deadline": "2026-11-01", "daysLeft": 15, "supportState": "ENDING", "location": "shop/Deployment/web"},
			{"clusterName": "prod", "itemType": "DATA_SERVICE", "displayName": "PostgreSQL 13", "version": "13.14",
				"deadline": "2025-11-13", "daysLeft": -338, "supportState": "EOL", "location": "data/StatefulSet/pg"},
			{"clusterName": "prod", "itemType": "KUBERNETES", "displayName": "Kubernetes 1.31", "daysLeft": 40, "platform": true},
			{"clusterName": "prod", "itemType": "RUNTIME", "displayName": "Python 3.12", "daysLeft": 700},
			{"clusterName": "prod", "itemType": "RUNTIME", "displayName": "Mystery runtime"},
		},
	}
}

func TestMaintainItemsSoonestFirstPlatformHidden(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/items": maintainItems(false)})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "items")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	pg, node, py, mystery := strings.Index(out, "PostgreSQL"), strings.Index(out, "Node.js"),
		strings.Index(out, "Python"), strings.Index(out, "Mystery")
	if !(pg >= 0 && pg < node && node < py && py < mystery) {
		t.Errorf("not soonest-first with unknown deadlines last:\n%s", out)
	}
	if !strings.Contains(out, "338d ago") {
		t.Errorf("a past deadline does not read as past:\n%s", out)
	}
	if strings.Contains(out, "Kubernetes 1.31") || !strings.Contains(out, "1 platform item(s)") {
		t.Errorf("platform item shown or its hiding not stated:\n%s", out)
	}
	if out, _ := run(t, "maintain", "items", "--platform"); !strings.Contains(out, "Kubernetes 1.31") {
		t.Errorf("--platform did not include it:\n%s", out)
	}
}

func TestMaintainItemsWithinFilters(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/items": maintainItems(false)})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "maintain", "items", "--within", "30")
	if !strings.Contains(out, "Node.js") || !strings.Contains(out, "PostgreSQL") {
		t.Errorf("items inside the window missing:\n%s", out)
	}
	if strings.Contains(out, "Python") || strings.Contains(out, "Mystery") {
		t.Errorf("items outside the window (or with no deadline) listed:\n%s", out)
	}
}

func TestMaintainItemsFailOnDaysGates(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/items": maintainItems(false)})
	defer srv.Close()
	isolateCluster(t, srv)

	if out, code := run(t, "maintain", "items", "--fail-on-days", "30"); code != ExitPolicy {
		t.Errorf("deadline in 15d against --fail-on-days 30 exited %d, want %d:\n%s", code, ExitPolicy, out)
	}
	// A lapsed deadline (PostgreSQL, 338 days ago) is the most due of all.
	if _, code := run(t, "maintain", "items", "--fail-on-days", "0"); code != ExitPolicy {
		t.Errorf("a past-due item did not fail --fail-on-days 0 (exit %d)", code)
	}
	if _, code := run(t, "maintain", "items", "--fail-on-days", "-5"); code != ExitUsage {
		t.Errorf("a negative day count was accepted (exit %d)", code)
	}
}

// A list the backend could not finish reading must never exit 0.
func TestMaintainItemsReadFailedExitsNonZero(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/items": maintainItems(true)})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "items")
	if code != ExitFailure || !strings.Contains(out, "incomplete") {
		t.Errorf("readFailed exit = %d, want %d:\n%s", code, ExitFailure, out)
	}
	if _, code := run(t, "maintain", "items", "-o", "json"); code != ExitFailure {
		t.Errorf("readFailed under -o json exited %d", code)
	}
}

func TestMaintainPackagesStatesUnscannedWorkloads(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Write([]byte(`{"success":true,"data":{"page":0,"size":50,"total":1,
			"workloadsWithPackageList":8,"workloadsWithoutPackageList":3,
			"items":[{"ecosystem":"maven","name":"log4j-core","versions":["2.17.1","2.14.0"],"workloads":4,"pods":9,
			  "eol":{"supportState":"EOL","deadline":"2024-01-01","daysLeft":-655}}]}}`))
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "packages", "--ecosystem", "maven", "--q", "log4j")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if q.Get("ecosystem") != "maven" || q.Get("q") != "log4j" || q.Get("size") != "50" {
		t.Errorf("query = %v", q)
	}
	for _, want := range []string{"log4j-core", "2.17.1,2.14.0", "EOL 2024-01-01", "655d ago", "3 workload(s) have no package list"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMaintainImpactScopesToSelectedClusterAndOrdersVerdicts(t *testing.T) {
	var q url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.Query()
		w.Write([]byte(`{"success":true,"data":{"product":"postgresql","displayName":"PostgreSQL",
			"currentVersion":"13.14","target":"17","rulePack":true,
			"summary":{"bumpDriver":1,"unknown":1,"review":0,"ready":1},
			"dependents":[
			  {"namespace":"shop","kind":"Deployment","name":"ok-app","verdict":"READY"},
			  {"namespace":"shop","kind":"Deployment","name":"mystery","verdict":"UNKNOWN","reason":"no driver seen"},
			  {"namespace":"shop","kind":"Deployment","name":"old-jdbc","verdict":"BUMP_DRIVER",
			   "reason":"SCRAM needs pgjdbc 42.2+","drivers":[{"driver":"pgjdbc","version":"9.4"}]}],
			"inbound":{"reported":true,"viaNode":2,"other":0}}}`))
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "impact", "data/StatefulSet/pg", "--target", "17")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if q.Get("clusterId") != "c1" || q.Get("namespace") != "data" || q.Get("kind") != "StatefulSet" ||
		q.Get("name") != "pg" || q.Get("target") != "17" {
		t.Errorf("query = %v", q)
	}
	b, u, r := strings.Index(out, "old-jdbc"), strings.Index(out, "mystery"), strings.Index(out, "ok-app")
	if !(b >= 0 && b < u && u < r) {
		t.Errorf("verdicts not ordered BUMP_DRIVER, UNKNOWN, READY:\n%s", out)
	}
	for _, want := range []string{"PostgreSQL 13.14 → 17", "1 bump driver · 1 unknown", "pgjdbc 9.4", "2 via a node"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMaintainDependentsWarnsOnPartialNodeCoverage(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/data-services/dependents": map[string]any{
		"coverage": "PARTIAL", "nodesExpected": 3, "nodesReported": 2,
		"dependents": []map[string]any{{"namespace": "shop", "kind": "Deployment", "name": "api",
			"via": "Service/pg", "port": 5432, "sockets": 12}},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "dependents", "data/StatefulSet/pg")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"Dependents (1)", "api", "Service/pg", "5432", "only 2 of 3 nodes reported"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMaintainRefMustHaveThreeParts(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if out, code := run(t, "maintain", "impact", "data/pg"); code != ExitUsage {
		t.Errorf("a two-part ref exited %d, want %d:\n%s", code, ExitUsage, out)
	}
}

// -o json keeps every server field but still honours the filters.
func TestMaintainItemsJSONHonoursFilters(t *testing.T) {
	srv := resourceServer(t, map[string]any{maintainBase + "/items": maintainItems(false)})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "maintain", "items", "--within", "30", "-o", "json")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Node.js 18") || strings.Contains(out, "Python") || strings.Contains(out, "Kubernetes 1.31") {
		t.Errorf("filters not applied to -o json:\n%s", out)
	}
	if !strings.Contains(out, `"location": "shop/Deployment/web"`) {
		t.Errorf("server fields dropped:\n%s", out)
	}
}
