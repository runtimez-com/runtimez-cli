package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// mcpBackend stands in for eac's /eac/mcp: it records what the bridge sent and answers per
// method the way the real endpoint does (JSON for requests, 202 for notifications).
type mcpBackend struct {
	mu       sync.Mutex
	requests []recordedMcpRequest
	token    string
}

type recordedMcpRequest struct {
	protocolHeader string
	auth           string
	body           map[string]any
}

func (m *mcpBackend) handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != mcpPath || r.Method != http.MethodPost {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	m.mu.Lock()
	m.requests = append(m.requests, recordedMcpRequest{
		protocolHeader: r.Header.Get(mcpProtocolHeader), auth: r.Header.Get("Authorization"), body: body})
	m.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer "+m.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	id, hasID := body["id"]
	if !hasID {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var result any
	switch body["method"] {
	case "initialize":
		result = map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "runtimez"}}
	case "tools/list":
		result = map[string]any{"tools": []map[string]any{{"name": "runtimez_whoami"}}}
	case "tools/call":
		params, _ := body["params"].(map[string]any)
		result = map[string]any{"echo": params["arguments"], "isError": false}
	default:
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32601, "message": "method not found"}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func runMcp(t *testing.T, stdin string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs([]string{"mcp"})
	code := ExitOK
	if err := root.Execute(); err != nil {
		code = codeFor(err)
		out.WriteString("error: " + err.Error() + "\n")
	}
	return out.String(), code
}

func lines(s string) []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			panic("non-JSON line on stdout: " + l)
		}
		out = append(out, m)
	}
	return out
}

func TestMcpBridgeForwardsRequestsAcknowledgesNotificationsAndInjectsCluster(t *testing.T) {
	be := &mcpBackend{token: "rk_test"}
	srv := httptest.NewServer(http.HandlerFunc(be.handler))
	defer srv.Close()
	isolate(t, srv)
	t.Setenv("RTZ_CLUSTER", "c1")

	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"runtimez_cluster_get","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"runtimez_cluster_get","arguments":{"cluster":"staging"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"runtimez_whoami"}}`,
	}, "\n") + "\n"

	out, code := runMcp(t, stdin)
	if code != ExitOK {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	got := lines(out)
	if len(got) != 5 {
		t.Fatalf("expected 5 responses (the notification has none), got %d:\n%s", len(got), out)
	}
	if got[0]["id"].(float64) != 1 || got[1]["id"].(float64) != 2 {
		t.Errorf("responses out of order or ids lost: %v", got)
	}

	// The default cluster is injected only when the client passed none.
	echo := func(i int) map[string]any {
		res, _ := got[i]["result"].(map[string]any)
		args, _ := res["echo"].(map[string]any)
		return args
	}
	if echo(2)["cluster"] != "c1" {
		t.Errorf("tools/call without cluster should carry the context cluster, got %v", echo(2))
	}
	if echo(3)["cluster"] != "staging" {
		t.Errorf("an explicit cluster must be kept, got %v", echo(3))
	}
	if echo(4)["cluster"] != "c1" {
		t.Errorf("a call with no arguments object should still get the default cluster, got %v", echo(4))
	}

	// Every request carried the bearer; the protocol header appears only after initialize.
	if len(be.requests) != 6 {
		t.Fatalf("backend saw %d requests, want 6", len(be.requests))
	}
	for i, r := range be.requests {
		if r.auth != "Bearer rk_test" {
			t.Errorf("request %d auth = %q", i, r.auth)
		}
	}
	if be.requests[0].protocolHeader != "" {
		t.Errorf("initialize must not carry a protocol version yet, got %q", be.requests[0].protocolHeader)
	}
	if be.requests[2].protocolHeader != "2025-06-18" {
		t.Errorf("post-initialize requests must echo the negotiated version, got %q", be.requests[2].protocolHeader)
	}
}

func TestMcpBridgeTurnsAuthFailuresIntoJsonRpcErrors(t *testing.T) {
	be := &mcpBackend{token: "rk_other"}
	srv := httptest.NewServer(http.HandlerFunc(be.handler))
	defer srv.Close()
	isolate(t, srv)

	out, code := runMcp(t, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`+"\n")
	if code != ExitOK {
		t.Fatalf("the bridge must keep serving after a 401; exit = %d:\n%s", code, out)
	}
	got := lines(out)
	if len(got) != 1 {
		t.Fatalf("want one error response, got %d:\n%s", len(got), out)
	}
	errObj, _ := got[0]["error"].(map[string]any)
	if errObj["code"].(float64) != mcpErrUnauthorized || !strings.Contains(errObj["message"].(string), "rtz login") {
		t.Errorf("unexpected error payload: %v", got[0])
	}
	if got[0]["id"].(float64) != 9 {
		t.Errorf("error must carry the request id, got %v", got[0]["id"])
	}
}

func TestMcpBridgeForwardsServerJsonRpcErrorsAndRejectsBadStdin(t *testing.T) {
	be := &mcpBackend{token: "rk_test"}
	srv := httptest.NewServer(http.HandlerFunc(be.handler))
	defer srv.Close()
	isolate(t, srv)

	out, _ := runMcp(t, "not json\n"+`{"jsonrpc":"2.0","id":2,"method":"resources/list"}`+"\n")
	got := lines(out)
	if len(got) != 2 {
		t.Fatalf("want 2 responses, got %d:\n%s", len(got), out)
	}
	if e, _ := got[0]["error"].(map[string]any); e["code"].(float64) != -32700 {
		t.Errorf("bad stdin should be a parse error, got %v", got[0])
	}
	if e, _ := got[1]["error"].(map[string]any); e["code"].(float64) != -32601 {
		t.Errorf("the server's own JSON-RPC error must be forwarded untouched, got %v", got[1])
	}
}
