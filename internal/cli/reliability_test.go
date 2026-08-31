package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const clusterBase = "/eac/api/1.0/orgs/org1/clusters/c1"

// allSignals is a posture where every evidence source was available, so a low score really
// does mean low risk.
func allSignals(workloads []map[string]any, summary map[string]int) map[string]any {
	return map[string]any{
		"summary": summary, "workloads": workloads,
		"cveScanAvailable": true, "metricsAvailable": true,
		"runtimeAvailable": true, "networkSignalsAvailable": true,
	}
}

func TestRiskListsWorstFirst(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workload-risk": allSignals([]map[string]any{
			{"namespace": "a", "name": "low-risk", "kind": "Deployment", "score": 10, "level": "LOW"},
			{"namespace": "b", "name": "on-fire", "kind": "Deployment", "score": 91, "level": "CRITICAL",
				"factors": []map[string]any{{"title": "No memory limit", "severity": "HIGH", "category": "RELIABILITY"}}},
		}, map[string]int{"total": 2, "critical": 1, "low": 1}),
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "risk")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if strings.Index(out, "on-fire") > strings.Index(out, "low-risk") {
		t.Errorf("risk not sorted worst-first:\n%s", out)
	}
	if !strings.Contains(out, "No memory limit") {
		t.Errorf("top factor missing:\n%s", out)
	}
}

func TestRiskFailOnGatesTheExitCode(t *testing.T) {
	routes := map[string]any{
		clusterBase + "/workload-risk": allSignals([]map[string]any{
			{"namespace": "b", "name": "on-fire", "score": 91, "level": "CRITICAL"},
		}, map[string]int{"total": 1, "critical": 1}),
	}
	srv := resourceServer(t, routes)
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "risk", "--fail-on", "high"); code != ExitPolicy {
		t.Errorf("CRITICAL against --fail-on high exited %d, want %d", code, ExitPolicy)
	}
	// Nothing reaches the bar, so the gate must pass.
	if _, code := run(t, "risk"); code != ExitOK {
		t.Errorf("no --fail-on exited %d, want 0", code)
	}
}

func TestRiskFailOnBelowThresholdPasses(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workload-risk": allSignals([]map[string]any{
			{"namespace": "a", "name": "ok", "score": 20, "level": "LOW"},
		}, map[string]int{"total": 1, "low": 1}),
	})
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "risk", "--fail-on", "high"); code != ExitOK {
		t.Errorf("LOW against --fail-on high exited %d, want 0", code)
	}
}

// A typo that parsed as "never fail" would turn a CI gate into a no-op reporting success
// forever, which is worse than no gate at all.
func TestUnknownFailOnValueIsRejected(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workload-risk": allSignals(nil, map[string]int{}),
	})
	defer srv.Close()
	isolateCluster(t, srv)

	for _, bad := range []string{"hgih", "sev1", "critcal", "yes"} {
		if _, code := run(t, "risk", "--fail-on", bad); code != ExitUsage {
			t.Errorf("--fail-on %q exited %d, want %d (usage)", bad, code, ExitUsage)
		}
	}
}

// A score computed without metrics is not a clean bill of health.
func TestRiskWarnsWhenSignalsAreMissing(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workload-risk": map[string]any{
			"summary":          map[string]int{"total": 1, "low": 1},
			"workloads":        []map[string]any{{"namespace": "a", "name": "quiet", "score": 3, "level": "LOW"}},
			"metricsAvailable": false, "cveScanAvailable": false,
			"runtimeAvailable": true, "networkSignalsAvailable": true,
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "risk")
	if !strings.Contains(out, "not a clean bill of health") {
		t.Errorf("missing-signal caveat absent:\n%s", out)
	}
	if !strings.Contains(out, "metrics") || !strings.Contains(out, "CVE scan") {
		t.Errorf("caveat did not name the missing sources:\n%s", out)
	}
}

func TestRiskNoWarningWhenEverySignalIsPresent(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workload-risk": allSignals(
			[]map[string]any{{"namespace": "a", "name": "ok", "score": 3, "level": "LOW"}},
			map[string]int{"total": 1, "low": 1}),
	})
	defer srv.Close()
	isolateCluster(t, srv)

	if out, _ := run(t, "risk"); strings.Contains(out, "not a clean bill of health") {
		t.Errorf("warned about signals that were all available:\n%s", out)
	}
}

// Zero findings means "clean" only when a scan has actually run.
func TestSecurityPostureDistinguishesCleanFromNeverScanned(t *testing.T) {
	never := resourceServer(t, map[string]any{
		clusterBase + "/security-posture": map[string]any{
			"score": 0, "riskLevel": "LOW", "findings": []any{}, "scannedAt": nil},
	})
	defer never.Close()
	isolateCluster(t, never)

	out, _ := run(t, "risk", "security")
	if !strings.Contains(out, "NEVER been scanned") {
		t.Errorf("an unscanned cluster read as clean:\n%s", out)
	}
}

func TestSecurityPostureScannedAndCleanDoesNotWarn(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/security-posture": map[string]any{
			"score": 0, "riskLevel": "LOW", "findings": []any{},
			"scannedAt": time.Now().Add(-time.Hour).Format(time.RFC3339)},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "risk", "security")
	if strings.Contains(out, "NEVER been scanned") {
		t.Errorf("a scanned, clean cluster was reported as unscanned:\n%s", out)
	}
}

func TestSecurityFindingsSortedBySeverity(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/security-posture": map[string]any{
			"score": 60, "riskLevel": "HIGH",
			"scannedAt": time.Now().Format(time.RFC3339),
			"findings": []map[string]any{
				{"ruleId": "r-low", "title": "minor", "severity": "LOW", "resourceName": "a/b"},
				{"ruleId": "r-crit", "title": "major", "severity": "CRITICAL", "resourceName": "c/d"},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "risk", "security")
	if strings.Index(out, "r-crit") > strings.Index(out, "r-low") {
		t.Errorf("findings not sorted by severity:\n%s", out)
	}
}

// --fixable is a display filter. Letting it lower the exit code would make a CI gate depend
// on how someone chose to look at the data.
func TestCveFixableFilterDoesNotWeakenTheGate(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/cve-posture": map[string]any{
			"score": 80, "riskLevel": "CRITICAL",
			"scannedAt": time.Now().Format(time.RFC3339),
			"findings": []map[string]any{
				{"cveId": "CVE-1", "severity": "CRITICAL", "packageName": "openssl", "fixedVersion": ""},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "risk", "cve", "--fixable", "--fail-on", "high")
	if code != ExitPolicy {
		t.Fatalf("exit = %d, want %d — an unfixable critical still fails the gate\n%s", code, ExitPolicy, out)
	}
	// The row itself is filtered out of the display, which is the point of the flag.
	if strings.Contains(out, "CVE-1") {
		t.Errorf("--fixable still displayed an unfixable finding:\n%s", out)
	}
}

func TestCveShowsNoFixMarker(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/cve-posture": map[string]any{
			"score": 50, "riskLevel": "HIGH", "scannedAt": time.Now().Format(time.RFC3339),
			"findings": []map[string]any{
				{"cveId": "CVE-2", "severity": "HIGH", "packageName": "zlib", "fixedVersion": ""},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "risk", "cve")
	if !strings.Contains(out, "<no fix>") {
		t.Errorf("an unfixable CVE did not say so:\n%s", out)
	}
}

// --- RCA --------------------------------------------------------------------

// degraded=false leaves every other field null; rendering a suspect there would invent a
// problem that the backend explicitly said does not exist.
func TestAutoRcaHealthyClusterPrintsTheMessageOnly(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/auto-rca": map[string]any{
			"degraded": false, "message": "No at-risk workloads in this cluster."},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "rca")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "No at-risk workloads") {
		t.Errorf("healthy message missing:\n%s", out)
	}
	if strings.Contains(out, "Risk:") || strings.Contains(out, "Probable causes") {
		t.Errorf("invented an RCA for a healthy cluster:\n%s", out)
	}
}

func TestAutoRcaDegradedRendersSuspectAndNarrative(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/auto-rca": map[string]any{
			"degraded": true,
			"topSuspect": map[string]any{
				"namespace": "payments", "name": "checkout", "kind": "Deployment",
				"score": 88, "level": "CRITICAL",
				"reasons": []map[string]any{{"title": "OOMKilled twice", "severity": "HIGH"}},
			},
			"explanation": map[string]any{
				"summary":        "The container exceeded its memory limit.",
				"recommendation": "Raise the memory limit to 512Mi.",
				"rankedCauses":   []map[string]any{{"cause": "memory limit too low", "confidence": "HIGH"}},
				"causalChain":    []map[string]any{{"step": 1, "role": "ROOT_CAUSE", "description": "limit exceeded"}},
				"llmUsed":        true, "fromCache": false, "modelId": "claude-opus-5", "gateReason": "EVIDENCE_SUFFICIENT",
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "rca")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"payments/checkout", "88/100", "OOMKilled twice",
		"exceeded its memory limit", "What happened", "root cause", "Probable causes", "Raise the memory limit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A model-written narrative and a deterministic gate result deserve different levels of
// trust, and the prose alone does not reveal which one you got.
func TestRcaExplainStatesItsProvenance(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workloads/payments/checkout/rca/explain": map[string]any{
			"summary": "Nothing conclusive.", "llmUsed": false, "gateReason": "INSUFFICIENT_EVIDENCE",
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "rca", "explain", "payments/checkout")
	if !strings.Contains(out, "deterministic (no model call)") {
		t.Errorf("gate-only result did not say so:\n%s", out)
	}
	if !strings.Contains(out, "INSUFFICIENT_EVIDENCE") {
		t.Errorf("gate reason missing:\n%s", out)
	}
}

func TestRcaExplainReportsCacheReplay(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workloads/payments/checkout/rca/explain": map[string]any{
			"summary": "Cached answer.", "llmUsed": true, "fromCache": true, "modelId": "claude-opus-5",
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "rca", "explain", "payments/checkout")
	if !strings.Contains(out, "replayed from cache") {
		t.Errorf("cache replay not reported:\n%s", out)
	}
}

// Zero errors and zero latency for a workload with no telemetry would read as a perfectly
// healthy service.
func TestWorkloadRcaSaysWhenThereIsNoTraceData(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workloads/payments/checkout/rca": map[string]any{
			"namespace": "payments", "name": "checkout", "kind": "Deployment",
			"restartCount": 4, "k8sReason": "CrashLoopBackOff", "oomKilled": true,
			"appHealthAvailable": false, "errorRatePct": 0, "p99LatencyMs": 0, "requestCount": 0,
			"termLogsAvailable": false,
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "rca", "payments/checkout")
	if !strings.Contains(out, "no trace data") {
		t.Errorf("absent telemetry rendered as healthy:\n%s", out)
	}
	if strings.Contains(out, "0.00% errors") {
		t.Errorf("printed a fabricated 0%% error rate:\n%s", out)
	}
	for _, want := range []string{"CrashLoopBackOff", "OOMKilled", "not collected"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWorkloadRcaShowsHealthWhenTracesExist(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/workloads/payments/checkout/rca": map[string]any{
			"namespace": "payments", "name": "checkout", "restartCount": 0,
			"appHealthAvailable": true, "errorRatePct": 12.5, "p99LatencyMs": 940, "requestCount": 3300,
			"termLogsAvailable": true, "logTailLines": []string{"panic: nil map"},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "rca", "payments/checkout")
	for _, want := range []string{"12.50% errors", "p99 940ms", "panic: nil map"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// --- signals and changes -----------------------------------------------------

// A latency figure without its window is not interpretable.
func TestSignalsStatesItsWindow(t *testing.T) {
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	srv := resourceServer(t, map[string]any{
		clusterBase + "/slow-operations": map[string]any{
			"window": map[string]any{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339)},
			"items": []map[string]any{
				{"serviceName": "checkout", "operation": "POST /pay", "p99Ms": 2400.0, "p50Ms": 85.0,
					"callCount": 900, "errorRatePct": 1.25},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "signals")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Window:") {
		t.Errorf("no window stated:\n%s", out)
	}
	// Sub-second stays in ms, seconds scale up — 2400ms is easier to read as 2.40s.
	if !strings.Contains(out, "2.40s") || !strings.Contains(out, "85.0ms") {
		t.Errorf("latency formatting wrong:\n%s", out)
	}
}

func TestSlowTracesSubcommand(t *testing.T) {
	srv := resourceServer(t, map[string]any{
		clusterBase + "/slow-traces": map[string]any{
			"items": []map[string]any{
				{"traceId": "abc123", "rootService": "gateway", "rootOperation": "GET /cart",
					"totalMs": 5100.0, "serviceCount": 6, "hasError": true,
					"suspectService": "checkout", "suspectNamespace": "payments"},
			},
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "signals", "traces")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"abc123", "gateway", "5.10s", "payments/checkout"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Reading page 1 of 40 as the whole history is how a change gets missed.
func TestChangesReportsPagination(t *testing.T) {
	when := time.Now().Add(-30 * time.Minute)
	srv := resourceServer(t, map[string]any{
		clusterBase + "/changes": map[string]any{
			"items": []map[string]any{
				{"kind": "Deployment", "namespace": "payments", "name": "checkout",
					"changeType": "UPDATE", "changeSummary": "image bumped",
					"changedAt": when.Format(time.RFC3339)},
			},
			"total": 812, "page": 0, "size": 20,
		},
	})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "changes")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "of 812") {
		t.Errorf("pagination not reported:\n%s", out)
	}
	for _, want := range []string{"UPDATE", "image bumped", "30m"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestChangesForOneWorkloadUsesTheWorkloadRoute(t *testing.T) {
	var hit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.Path
		json.NewEncoder(w).Encode(map[string]any{"success": true,
			"data": map[string]any{"items": []any{}, "total": 0}})
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "changes", "payments/checkout"); code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.HasSuffix(hit, "/workloads/payments/checkout/changes") {
		t.Errorf("used the cluster-wide route for a workload query: %q", hit)
	}
}

func TestReliabilityCommandsRequireACluster(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv) // no RTZ_CLUSTER

	for _, args := range [][]string{{"risk"}, {"rca"}, {"signals"}, {"changes"}} {
		if _, code := run(t, args...); code != ExitUsage {
			t.Errorf("%v exited %d, want %d (usage)", args, code, ExitUsage)
		}
	}
}
