package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func addonHead(name string) map[string]any {
	return map[string]any{"addonId": strings.ToLower(name), "displayName": name, "version": "1.2.0",
		"ruleCount": 30, "rulesEvaluated": true}
}

func TestAddonsNodePoolsListsFindingsAndGates(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/autoscaling/nodepools": map[string]any{
		"status": "OK", "addon": addonHead("Karpenter"), "lastSyncAt": "2026-10-17T10:00:00Z",
		"counts": map[string]int{"HEALTHY": 1, "DEGRADED": 1},
		"header": map[string]any{"managedBy": "EKS Auto Mode"},
		"nodePools": []map[string]any{
			{"name": "default", "state": "HEALTHY", "nodes": 3, "nodeClaimCount": 3},
			{"name": "gpu", "state": "DEGRADED", "nodes": 0, "nodeClaimCount": 1, "driftedCount": 1,
				"reasons": []map[string]any{
					{"ruleId": "karpenter-limit-reached", "severity": "HIGH", "title": "NodePool at its cpu limit"},
					{"ruleId": "karpenter-drift", "severity": "LOW", "title": "Drifted claim"},
				}},
		},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "addons", "nodepools")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"Karpenter 1.2.0 · 30 rules", "DEGRADED=1 HEALTHY=1", "Managed by EKS Auto Mode",
		"NodePools (2)", "gpu", "HIGH NodePool at its cpu limit"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, code := run(t, "addons", "karpenter", "--fail-on", "high"); code != ExitPolicy {
		t.Errorf("a HIGH finding against --fail-on high exited %d", code)
	}
	if _, code := run(t, "addons", "nodepools", "--fail-on", "critical"); code != ExitOK {
		t.Errorf("--fail-on critical with no CRITICAL exited %d", code)
	}
	if _, code := run(t, "addons", "nodepools", "-o", "json", "--fail-on", "high"); code != ExitPolicy {
		t.Errorf("--fail-on did not gate under -o json (exit %d)", code)
	}
	if _, code := run(t, "addons", "nodepools", "--fail-on", "hgh"); code != ExitUsage {
		t.Errorf("a typo in --fail-on was accepted (exit %d)", code)
	}
}

// An add-on that is not installed must say so, and must never pass a gate.
func TestAddonsNotDetectedIsStatedNotEmpty(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/mesh/workloads": map[string]any{
		"status": "NOT_DETECTED", "message": "no istiod Deployment found",
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "addons", "istio", "--fail-on", "high")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"NOT_DETECTED", "no istiod Deployment found", "--fail-on has nothing to gate"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "No workloads.") {
		t.Errorf("an undetected add-on rendered as an empty table:\n%s", out)
	}
}

func TestAddonsRulesNotEvaluatedWarns(t *testing.T) {
	head := addonHead("Argo CD")
	head["rulesEvaluated"] = false
	srv := resourceServer(t, map[string]any{clusterBase + "/gitops/applications": map[string]any{
		"status": "OK", "addon": head,
		"applications": []map[string]any{{"namespace": "argocd", "name": "shop", "sync": "Synced", "health": "Healthy"}},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "addons", "gitops")
	if !strings.Contains(out, "rules were not evaluated") {
		t.Errorf("unevaluated rules not surfaced:\n%s", out)
	}
}

func TestAddonsIngressDetailPathAndKind(t *testing.T) {
	var path, kind string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, kind = r.URL.Path, r.URL.Query().Get("kind")
		w.Write([]byte(`{"success":true,"data":{"status":"OK",
			"route":{"kind":"IngressRoute","namespace":"shop","name":"web","state":"BROKEN","stateWhy":"backend Service missing",
			  "reasons":[{"ruleId":"traefik-backend-missing","severity":"CRITICAL","title":"Backend Service does not exist",
			    "verifyCommand":"kubectl -n shop get svc web","objectKind":"Service","objectRef":"shop/web"}]},
			"backendWorkloads":[{"kind":"Deployment","namespace":"shop","name":"web","service":"web"}]}}`))
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "addons", "ingress", "shop/web", "--kind", "IngressRoute")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if path != clusterBase+"/ingress/routes/shop/web" || kind != "IngressRoute" {
		t.Errorf("path = %q kind = %q", path, kind)
	}
	for _, want := range []string{"IngressRoute shop/web: BROKEN — backend Service missing", "Findings (1)",
		"CRITICAL", "kubectl -n shop get svc web", "Service shop/web", "Backend workloads (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if out, code := run(t, "addons", "ingress", "web"); code != ExitUsage {
		t.Errorf("a ref without a namespace exited %d:\n%s", code, out)
	}
}

func TestAddonsNodeDetailUsesNameQuery(t *testing.T) {
	var path, name string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, name = r.URL.Path, r.URL.Query().Get("name")
		w.Write([]byte(`{"success":true,"data":{"status":"OK",
			"node":{"name":"ip-10-0-1-5","state":"IP_EXHAUSTED","ipAssigned":29,"ipUsable":29,"ipMax":29},
			"errors":[{"family":"ipamd","key":"InsufficientCidrBlocks","lastHour":4,"lastDay":40}]}}`))
	}))
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "addons", "vpc-cni", "ip-10-0-1-5")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if path != clusterBase+"/networking/vpc-cni/node" || name != "ip-10-0-1-5" {
		t.Errorf("path = %q name = %q", path, name)
	}
	if !strings.Contains(out, "IP_EXHAUSTED") || !strings.Contains(out, "InsufficientCidrBlocks") {
		t.Errorf("detail not rendered:\n%s", out)
	}
}

func TestAddonsDetailNotFoundIsStated(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/autoscaling/nodepools/nope": map[string]any{
		"status": "NOT_FOUND", "message": "no NodePool named nope",
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, _ := run(t, "addons", "nodepools", "nope")
	if !strings.Contains(out, "NOT_FOUND") || !strings.Contains(out, "no NodePool named nope") {
		t.Errorf("missing row not stated:\n%s", out)
	}
}

func TestAddonsEbsCsiShowsFailingAttachmentsAndStuckWorkloads(t *testing.T) {
	srv := resourceServer(t, map[string]any{clusterBase + "/storage/ebs-csi": map[string]any{
		"status": "OK", "addon": addonHead("Amazon EBS CSI"),
		"header": map[string]any{"driverVersion": "v1.38.1", "controllerReady": "2/2", "nodePluginReady": "3/3"},
		"nodes":  []map[string]any{{"name": "n1", "state": "OK", "attached": 3, "limit": 25}},
		"attachments": []map[string]any{
			{"name": "csi-ok", "node": "n1", "persistentVolume": "pv-a", "attached": true},
			{"name": "csi-bad", "node": "n1", "persistentVolume": "pv-b", "attached": false, "problem": "ATTACH_FAILED",
				"message": "volume is in another AZ"},
		},
		"workloads": []map[string]any{{"kind": "StatefulSet", "namespace": "data", "name": "pg",
			"reasons": []map[string]any{{"ruleId": "ebs-az-mismatch", "severity": "HIGH", "title": "Volume and node in different zones"}}}},
	}})
	defer srv.Close()
	isolateCluster(t, srv)

	out, code := run(t, "addons", "ebs-csi")
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	for _, want := range []string{"Driver v1.38.1", "3/25", "Failing attachments (1)", "csi-bad", "another AZ",
		"Workloads stuck on EBS (1)", "StatefulSet data/pg", "different zones"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "csi-ok") {
		t.Errorf("a healthy attachment was listed as failing:\n%s", out)
	}
}
