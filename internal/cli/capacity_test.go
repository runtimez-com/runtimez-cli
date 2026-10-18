package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// capacityFixture mirrors CapacityResponse: units on the wire are millicores and bytes, and
// percentages are already 0-100.
func capacityFixture() map[string]any {
	return map[string]any{
		"status": "OK", "lastSyncAt": "2026-10-17T10:00:00Z",
		"autoscaler": map[string]any{"state": "KARPENTER", "acting": true, "managedBy": "EKS Auto Mode"},
		"usage":      map[string]any{"status": "OK", "nodesWithSamples": 2, "nodesWithoutSamples": 1},
		"headroom": []map[string]any{
			{"axis": "cpu", "unit": "millicores", "allocatable": 8000, "requested": 6000, "requestsCommitmentPct": 75.0,
				"used": 2500, "usedPct": 31.3, "headroom": 2000, "largestFit": 1500, "largestFitNode": "ip-10-0-1-5"},
			{"axis": "memory", "unit": "bytes", "allocatable": 34359738368, "requested": 17179869184,
				"requestsCommitmentPct": 50.0, "headroom": 17179869184},
		},
		"pools": []map[string]any{{
			"name": "default", "source": "karpenter", "nodes": 3, "pods": 41, "allocatablePods": 110,
			"cpuRequestsCommitmentPct": 75.0, "memoryRequestsCommitmentPct": 50.0,
			"headroom":   map[string]any{"cpuMillicores": 2000, "memoryBytes": 17179869184},
			"roomToGrow": map[string]any{"nodes": 7, "atCeiling": false},
		}},
		"blocked": []map[string]any{
			{"namespace": "shop", "name": "cart-abc", "ownerKind": "Deployment", "ownerName": "cart",
				"pendingSeconds": 600, "primaryCategory": "INSUFFICIENT_CPU", "capacityShortfall": true,
				"message": "0/3 nodes are available: 3 Insufficient cpu."},
			{"namespace": "shop", "name": "cart-def", "pendingSeconds": 30, "primaryCategory": "GATED"},
		},
		"blockedSummary": map[string]any{"total": 2, "listed": 2, "capacityShortfall": 1, "gated": 1,
			"byCategory": map[string]int{"INSUFFICIENT_CPU": 1, "GATED": 1}, "eventsRead": true},
		"flags": map[string]any{"podRowsTruncated": true},
		"storage": map[string]any{"status": "OK", "metrics": "PARTIAL", "metricsReason": "kubelet stats missing on 1 node",
			"pvcs": []map[string]any{
				{"namespace": "data", "name": "pg-0", "phase": "Bound", "storageClass": "gp3",
					"capacityBytes": 107374182400, "usedBytes": 96636764160, "usedPct": 90.0, "daysToFull": 4.2,
					"forecast": "OK", "mountedBy": "StatefulSet/pg"},
			},
			"pending": []map[string]any{{"namespace": "data", "name": "pg-1", "storageClass": "gp3",
				"reason": "WaitForFirstConsumer", "pendingSeconds": 120}},
		},
	}
}

func TestCapacitySummaryRendersHeadroomPoolsAndPending(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": capacityFixture()})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "capacity")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{
		"KARPENTER, acting", "EKS Auto Mode",
		"Headroom (2)", "8000m", "6000m", "75%", "32.0Gi", "16.0Gi",
		"Pools (1)", "default", "+7 nodes", "41/110",
		"Pending pods: 2 (1 capacity shortfall, 1 gated", "GATED=1 INSUFFICIENT_CPU=1",
		"rtz capacity --section blocked",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// "We could not see" is stated: one node without samples, truncated pod rows.
	for _, want := range []string{"1 node(s) have no usage samples", "pod rows were truncated"} {
		if !strings.Contains(out, want) {
			t.Errorf("caveat %q not surfaced:\n%s", want, out)
		}
	}
}

func TestCapacitySectionBlockedLongestWaitingFirst(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": capacityFixture()})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "capacity", "--section", "blocked")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if i, j := strings.Index(out, "cart-abc"), strings.Index(out, "cart-def"); i < 0 || j < 0 || i > j {
		t.Errorf("longest-pending pod not first:\n%s", out)
	}
	if !strings.Contains(out, "Deployment/cart") || !strings.Contains(out, "Insufficient cpu") {
		t.Errorf("owner or scheduler message missing:\n%s", out)
	}
}

func TestCapacitySectionStorageWarnsOnPartialMetrics(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": capacityFixture()})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity", "--section", "storage")
	for _, want := range []string{"pg-0", "90%", "4d", "100.0Gi", "pg-1", "Pending", "volume metrics: PARTIAL"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A typo must never silently fall back to the summary.
func TestCapacityRejectsUnknownSection(t *testing.T) {
	srv := resourceServer(t, map[string]any{})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "capacity", "--section", "nodez")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d:\n%s", code, ExitUsage, out)
	}
}

// A cluster that was never synced must read as "not known", not as an empty, healthy one.
func TestCapacityNonOKStatusIsStated(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": map[string]any{
		"status": "NEVER_SYNCED", "message": "the agent has not reported this cluster yet",
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity")
	if !strings.Contains(out, "NEVER_SYNCED") || !strings.Contains(out, "has not reported") {
		t.Errorf("non-OK status not surfaced:\n%s", out)
	}
}

// -o json prints the server document, not the trimmed struct the tables use.
func TestCapacityJSONIsLossless(t *testing.T) {
	fx := capacityFixture()
	fx["supply"] = map[string]any{"fieldTheCLIDoesNotType": 42}
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": fx})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "capacity", "-o", "json")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got["supply"].(map[string]any)["fieldTheCLIDoesNotType"] != float64(42) {
		t.Errorf("an untyped field was dropped:\n%s", out)
	}
	if strings.Contains(out, "e+") {
		t.Errorf("a byte count was printed in exponent form:\n%s", out)
	}
}

func TestCapacityYAMLKeepsExactIntegers(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity": capacityFixture()})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity", "-o", "yaml")
	if !strings.Contains(out, "allocatable: 34359738368") {
		t.Errorf("byte count not printed exactly:\n%s", out)
	}
}

func TestCapacityTrendSaysWhenStillCollecting(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity/trend": map[string]any{
		"status": "COLLECTING", "message": "3 of 7 days collected", "days": 30, "minDays": 7, "daysCollected": 3,
		"target": 80.0, "targetSource": "environment",
		"pools": []map[string]any{{"name": "default",
			"cpu":    map[string]any{"axis": "cpu", "status": "COLLECTING", "currentPct": 62.0},
			"memory": map[string]any{"axis": "memory", "status": "OK", "days": 21, "fullOn": "2026-11-07", "currentPct": 70.0},
			"grade":  map[string]any{"status": "TIGHT"}}},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity", "trend")
	for _, want := range []string{"COLLECTING", "3 of 7 days collected", "default", "TIGHT", "62%", "21d"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCapacityTrendPassesDays(t *testing.T) {
	var got string
	srv := capture(t, &got, "days")
	defer srv.Close()
	isolateCluster(t, srv)

	if _, code := run(t, "capacity", "trend", "--days", "14"); code != ExitOK {
		t.Fatal("trend failed")
	}
	if got != "14" {
		t.Errorf("days = %q, want 14", got)
	}
}

// An unpriced node is not a free node.
func TestCapacityCostFlagsUnpricedNodes(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity/cost": map[string]any{
		"status": "OK", "nodes": 4, "pricedNodes": 3, "clusterMonthly": 1234.5, "clusterHourly": 1.69,
		"priceSource": map[string]any{"label": "AWS list price", "onDemandDate": "2026-10-01"},
		"pools": []map[string]any{
			{"pool": "default", "nodes": 3, "pricedNodes": 3, "monthly": 1234.5, "hourly": 1.69},
			{"pool": "gpu", "nodes": 1, "pricedNodes": 0},
		},
		"levers": []map[string]any{{"label": "Move default to spot", "saving": true, "monthly": 420.0, "actsIn": "default"}},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity", "cost")
	for _, want := range []string{"$1234.50/month", "3 of 4 nodes priced", "AWS list price as of 2026-10-01",
		"1 node(s) unpriced", "unpriced", "Move default to spot", "$420.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCapacityNamespacesWarnsWhenQuotasUncollected(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/capacity/namespaces": map[string]any{
		"status": "OK", "quotasCollected": false,
		"namespaces": []map[string]any{
			{"namespace": "small", "pods": 2, "cpuShare": 10.0, "quotaStatus": "NOT_COLLECTED",
				"requested": map[string]any{"cpuMillicores": 200, "memoryBytes": 268435456}},
			{"namespace": "big", "pods": 20, "pendingPods": 3, "cpuShare": 90.0, "quotaStatus": "QUOTA",
				"requested": map[string]any{"cpuMillicores": 1800, "memoryBytes": 2147483648},
				"fullest":   map[string]any{"resource": "requests.cpu", "hard": "2", "used": "1800m", "pct": 90.0}},
		},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "capacity", "namespaces")
	if i, j := strings.Index(out, "big"), strings.Index(out, "small"); i < 0 || j < 0 || i > j {
		t.Errorf("largest CPU share not first:\n%s", out)
	}
	for _, want := range []string{"requests.cpu 1800m/2 (90%)", "ResourceQuotas were not collected"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCapacityFleetSoonestFullFirst(t *testing.T) {
	srv := resourceServer(t, map[string]any{"/eac/api/1.0/orgs/org1/capacity/fleet": map[string]any{
		"clustersTotal": 3, "clustersAnalyzed": 2,
		"totals": map[string]any{"clustersCounted": 2, "notCounted": map[string]int{"NEVER_SYNCED": 1},
			"cpu": map[string]any{"requestsCommitmentPct": 60.0}},
		"clusters": []map[string]any{
			{"clusterId": "c-late", "name": "late", "status": "OK", "soonestFull": map[string]any{"pool": "a", "axis": "cpu", "days": 90}},
			{"clusterId": "c-soon", "name": "soon", "status": "OK", "soonestFull": map[string]any{"pool": "b", "axis": "memory", "days": 5}},
		},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "capacity", "fleet")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if i, j := strings.Index(out, "soon"), strings.Index(out, "late"); i < 0 || j < 0 || i > j {
		t.Errorf("soonest-full cluster not first:\n%s", out)
	}
	if !strings.Contains(out, "not counted in the totals: NEVER_SYNCED=1") {
		t.Errorf("uncounted clusters not surfaced:\n%s", out)
	}
}
