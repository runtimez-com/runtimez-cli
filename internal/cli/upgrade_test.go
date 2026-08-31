package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestUpgradeCheckRendersAndGates(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/upgrade-readiness": map[string]any{
			"currentVersion": "1.29", "targetVersion": "1.30",
			"score": 74, "riskLevel": "HIGH", "scanStatus": "COMPLETE",
			"support": map[string]any{"daysUntilForcedUpgrade": 45, "forcedUpgradeDate": "2026-10-15"},
			"findings": []map[string]any{
				{"ruleId": "api-removed", "severity": "CRITICAL", "title": "Removed API in use",
					"resourceName": "payments/checkout"},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "upgrade", "check")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"1.29 → 1.30", "74/100", "HIGH", "45 days", "Removed API in use"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	if _, code := run(t, "upgrade", "check", "--fail-on", "high"); code != ExitPolicy {
		t.Errorf("a CRITICAL blocker against --fail-on high exited %d, want %d", code, ExitPolicy)
	}
}

func TestUpgradeCheckPassesTargetVersion(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("targetVersion")
		w.Write([]byte(`{"success":true,"data":{"currentVersion":"1.29","targetVersion":"1.31","findings":[]}}`))
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "upgrade", "check", "--target", "1.31"); code != ExitOK {
		t.Fatal("upgrade check failed")
	}
	if got != "1.31" {
		t.Errorf("targetVersion = %q, want 1.31", got)
	}
}

// A readiness verdict built on stale or partial inventory reads exactly like one built on
// fresh, complete data. Saying so is the difference between a decision and a guess.
func TestUpgradeCheckWarnsOnStaleAndPartialData(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/upgrade-readiness": map[string]any{
			"currentVersion": "1.29", "targetVersion": "1.30", "score": 10, "riskLevel": "LOW",
			"stale": true, "dataAgeSeconds": 7200, "scanStatus": "PARTIAL",
			"coverage": []map[string]any{
				{"tier": "crd-scan", "status": "MISSING"},
				{"tier": "api-usage", "status": "COMPLETE"},
			},
			"findings": []any{},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "upgrade", "check")
	if !strings.Contains(out, "STALE inventory") || !strings.Contains(out, "2h old") {
		t.Errorf("staleness not surfaced:\n%s", out)
	}
	if !strings.Contains(out, "PARTIAL") {
		t.Errorf("partial scan status not surfaced:\n%s", out)
	}
	if !strings.Contains(out, "crd-scan=MISSING") {
		t.Errorf("incomplete coverage tier not named:\n%s", out)
	}
	if strings.Contains(out, "api-usage") {
		t.Errorf("a COMPLETE tier was reported as a gap:\n%s", out)
	}
}

// An illustrative cost and a quoted price look identical once printed.
func TestUpgradeCheckLabelsIllustrativeCost(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/upgrade-readiness": map[string]any{
			"currentVersion": "1.28", "targetVersion": "1.29", "findings": []any{},
			"support": map[string]any{
				"annualExtendedSupportCostEstimate": 43800.0, "dataSourced": false},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "upgrade", "check")
	if !strings.Contains(out, "ILLUSTRATIVE") {
		t.Errorf("an unsourced cost was presented as a quote:\n%s", out)
	}
}

func TestUpgradeFleetSortsBySoonestDeadline(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/upgrade-readiness": []map[string]any{
			{"clusterId": "c-late", "name": "later", "score": 90, "riskLevel": "CRITICAL",
				"daysUntilForcedUpgrade": 300, "currentVersion": "1.30"},
			{"clusterId": "c-soon", "name": "sooner", "score": 20, "riskLevel": "LOW",
				"daysUntilForcedUpgrade": 12, "currentVersion": "1.27"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "upgrade", "fleet")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	// The cluster running out of support is the one that forces a decision, even at a
	// lower risk score.
	if strings.Index(out, "sooner") > strings.Index(out, "later") {
		t.Errorf("fleet not sorted by soonest forced upgrade:\n%s", out)
	}
}

// A cluster that was never analysed has no score. Counting it as fine would be a false pass.
func TestUpgradeFleetReportsUnanalysedClusters(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters/upgrade-readiness": []map[string]any{
			{"clusterId": "c1", "name": "analysed", "score": 10, "riskLevel": "LOW"},
			{"clusterId": "c2", "name": "unknown-version"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "upgrade", "fleet", "--fail-on", "high")
	if code != ExitOK {
		t.Fatalf("exit = %d — nothing analysed reached HIGH:\n%s", code, out)
	}
	if !strings.Contains(out, "NOT covered by --fail-on") {
		t.Errorf("unanalysed clusters were folded in silently:\n%s", out)
	}
}

// --- the CI gate -------------------------------------------------------------

func TestRiskCheckFindsAndGates(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/deployment-risk/evaluate": map[string]any{
			"score": 45, "level": "HIGH", "resourceCount": 1,
			"findings": []map[string]any{
				{"ruleId": "wrs-spec-privileged", "severity": "HIGH", "title": "Container runs privileged",
					"resourceName": "risky", "resourceType": "Deployment", "scoreImpact": 20},
				{"ruleId": "wrs-rs-unset", "severity": "MEDIUM", "title": "No resource requests",
					"resourceName": "risky", "resourceType": "Deployment", "scoreImpact": 10},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	dir := t.TempDir()
	path := dir + "/manifest.yaml"
	if err := writeFile(path, "apiVersion: apps/v1\nkind: Deployment\n"); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, "risk", "check", "-f", path)
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Index(out, "wrs-spec-privileged") > strings.Index(out, "wrs-rs-unset") {
		t.Errorf("findings not sorted by severity:\n%s", out)
	}
	if !strings.Contains(out, "Deployment/risky") {
		t.Errorf("resource not identified:\n%s", out)
	}

	if _, code := run(t, "risk", "check", "-f", path, "--fail-on", "high"); code != ExitPolicy {
		t.Errorf("HIGH finding against --fail-on high exited %d, want %d", code, ExitPolicy)
	}
	if _, code := run(t, "risk", "check", "-f", path, "--fail-on", "critical"); code != ExitOK {
		t.Errorf("no CRITICAL finding should pass --fail-on critical")
	}
}

// A template step that silently produced nothing would otherwise score 0 and pass any gate —
// the worst possible green.
func TestRiskCheckRefusesAnEmptyManifest(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	dir := t.TempDir()
	path := dir + "/empty.yaml"
	if err := writeFile(path, "   \n\n"); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, "risk", "check", "-f", path, "--fail-on", "high")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d — an empty bundle must not report a passing score", code, ExitUsage)
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("unclear message:\n%s", out)
	}
}

func TestRiskCheckRequiresAFile(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "risk", "check"); code != ExitUsage {
		t.Errorf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestRiskCheckCleanBundleSaysSo(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		"/eac/api/1.0/deployment-risk/evaluate": map[string]any{
			"score": 0, "level": "LOW", "resourceCount": 3, "findings": []any{},
			"checkedAt": time.Now().Format(time.RFC3339),
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	dir := t.TempDir()
	path := dir + "/ok.yaml"
	if err := writeFile(path, "kind: Deployment\n"); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, "risk", "check", "-f", path, "--fail-on", "low")
	if code != ExitOK {
		t.Fatalf("a clean bundle failed the gate: exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "this bundle is clean") {
		t.Errorf("clean result unclear:\n%s", out)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
