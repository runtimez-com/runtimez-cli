package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the CLI end to end the way a shell would, and returns stdout plus the exit
// code the process would have used.
func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	root := newRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)

	code := ExitOK
	if err := root.Execute(); err != nil {
		code = codeFor(err)
		out.WriteString("error: " + err.Error() + "\n")
	}
	return out.String(), code
}

// backend stands in for eac, answering with the ApiResponse envelope.
func backend(t *testing.T, routes map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "no route " + r.URL.Path})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": body})
	}))
}

func isolate(t *testing.T, srv *httptest.Server) {
	t.Helper()
	t.Setenv("RTZ_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	t.Setenv("RTZ_TOKEN", "rk_test")
	t.Setenv("RTZ_API", srv.URL)
	t.Setenv("RTZ_ORG", "org1")
	t.Setenv("RTZ_CLUSTER", "")
	t.Setenv("RTZ_CONTEXT", "")
}

func TestVersionCommand(t *testing.T) {
	out, code := run(t, "version")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.HasPrefix(out, "rtz ") {
		t.Errorf("unexpected version output: %q", out)
	}
}

func TestClusterListTable(t *testing.T) {
	srv := backend(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters": []map[string]any{
			{"id": "c1", "name": "prod", "status": "CONNECTED", "kubernetesVersion": "1.30.4", "nodeCount": 3},
		},
	})
	defer srv.Close()
	isolate(t, srv)

	out, code := run(t, "cluster", "ls")
	if code != ExitOK {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	for _, want := range []string{"ID", "NAME", "c1", "prod", "CONNECTED", "1.30.4"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// resourceCount was absent from the payload, and "0" would be a different claim.
	if !strings.Contains(out, "<none>") {
		t.Errorf("absent count did not render as <none>:\n%s", out)
	}
}

func TestClusterListJSONIsUnwrapped(t *testing.T) {
	srv := backend(t, map[string]any{
		"/eac/api/1.0/orgs/org1/clusters": []map[string]any{{"id": "c1", "name": "prod"}},
	})
	defer srv.Close()
	isolate(t, srv)

	out, code := run(t, "cluster", "ls", "-o", "json")
	if code != ExitOK {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not a bare JSON array: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0]["id"] != "c1" {
		t.Errorf("payload wrong: %+v", got)
	}
}

func TestOrgListUsesTokenOrg(t *testing.T) {
	srv := backend(t, map[string]any{
		"/eac/api/1.0/orgs/mine": []map[string]any{
			{"orgId": "org1", "name": "Acme", "plan": "TRIAL", "role": "ORG_ADMIN", "current": true},
		},
	})
	defer srv.Close()
	isolate(t, srv)

	out, code := run(t, "org", "ls")
	if code != ExitOK {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "Acme") || !strings.Contains(out, "TRIAL") {
		t.Errorf("org row missing:\n%s", out)
	}
}

func TestMissingCredentialsExitsWithAuthCode(t *testing.T) {
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)
	t.Setenv("RTZ_TOKEN", "")

	out, code := run(t, "whoami")
	if code != ExitAuth {
		t.Fatalf("exit = %d, want %d (auth). output:\n%s", code, ExitAuth, out)
	}
}

func TestMissingOrgIsAUsageError(t *testing.T) {
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)
	t.Setenv("RTZ_ORG", "")

	out, code := run(t, "cluster", "ls")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage). output:\n%s", code, ExitUsage, out)
	}
}

func TestUnknownOutputFormatIsAUsageError(t *testing.T) {
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)

	_, code := run(t, "cluster", "ls", "-o", "xml")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	_, code := run(t, "version", "--nope")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

// A rejected key must be reported as an auth failure, not a generic error, so scripts can
// tell "sign in again" apart from "the backend is broken".
func TestRejectedTokenExitsWithAuthCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Unauthorized"})
	}))
	defer srv.Close()
	isolate(t, srv)

	_, code := run(t, "whoami")
	if code != ExitAuth {
		t.Fatalf("exit = %d, want %d (auth)", code, ExitAuth)
	}
}

func TestLoginRejectsNonAPIKey(t *testing.T) {
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)

	_, code := run(t, "login", "--token", "not-a-key")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d (usage)", code, ExitUsage)
	}
}

func TestLoginVerifiesBeforePersisting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	isolate(t, srv)

	_, code := run(t, "login", "--token", "rk_bad", "--api-url", srv.URL)
	if code != ExitAuth {
		t.Fatalf("exit = %d, want %d (auth)", code, ExitAuth)
	}
	// Nothing should have been written for a key the backend rejected.
	out, _ := run(t, "config", "get-contexts")
	if strings.Contains(out, srv.URL) {
		t.Errorf("a rejected key still created a context:\n%s", out)
	}
}

func TestDoctorReportsFailureExitCode(t *testing.T) {
	// No routes at all: reachability fails, which doctor must surface as a non-zero exit.
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)

	out, code := run(t, "doctor")
	if code == ExitOK {
		t.Fatalf("doctor passed against a backend with no routes:\n%s", out)
	}
	if !strings.Contains(out, "api reachable") {
		t.Errorf("doctor did not report reachability:\n%s", out)
	}
}

func TestDoctorPassesAgainstAHealthyBackend(t *testing.T) {
	srv := backend(t, map[string]any{
		"/eac/api/1.0/auth/oauth/providers":                    map[string]bool{"google": true},
		"/eac/api/1.0/users/me":                                map[string]any{"email": "dev@example.com", "orgId": "org1"},
		"/eac/api/1.0/orgs/org1/clusters":                      []map[string]any{{"id": "c1", "name": "prod"}},
		"/eac/api/1.0/orgs/org1/observability/pipeline-health": map[string]any{"status": "OK"},
	})
	defer srv.Close()
	isolate(t, srv)
	t.Setenv("RTZ_CLUSTER", "c1")

	out, code := run(t, "doctor")
	if code != ExitOK {
		t.Fatalf("exit = %d, output:\n%s", code, out)
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("healthy backend produced a failure:\n%s", out)
	}
}

// The selected cluster not being in the visible list is a real misconfiguration, and
// silently listing something else would be worse than failing.
func TestDoctorFailsWhenSelectedClusterIsNotVisible(t *testing.T) {
	srv := backend(t, map[string]any{
		"/eac/api/1.0/auth/oauth/providers":                    map[string]bool{},
		"/eac/api/1.0/users/me":                                map[string]any{"email": "dev@example.com", "orgId": "org1"},
		"/eac/api/1.0/orgs/org1/clusters":                      []map[string]any{{"id": "c1"}},
		"/eac/api/1.0/orgs/org1/observability/pipeline-health": map[string]any{"status": "OK"},
	})
	defer srv.Close()
	isolate(t, srv)
	t.Setenv("RTZ_CLUSTER", "c-missing")

	out, code := run(t, "doctor")
	if code == ExitOK {
		t.Fatalf("doctor accepted an invisible cluster:\n%s", out)
	}
}

// FR-28: a terminal that cannot host the full-screen UI gets help, not a half-rendered
// screen. Test output is never a TTY, so a bare invocation here exercises that path.
func TestBareInvocationWithoutATTYPrintsHelp(t *testing.T) {
	srv := backend(t, map[string]any{})
	defer srv.Close()
	isolate(t, srv)

	out, code := run(t)
	if code != ExitOK {
		t.Fatalf("exit = %d:\n%s", code, out)
	}
	if !strings.Contains(out, "Available Commands") {
		t.Errorf("bare rtz on a non-TTY did not print help:\n%s", out)
	}
}

// A shipped binary must not default to the developer's own machine. This is the one constant
// a customer inherits without ever setting it.
func TestDefaultAPIURLIsNotLocalhost(t *testing.T) {
	if strings.Contains(DefaultAPIURL, "localhost") || strings.Contains(DefaultAPIURL, "127.0.0.1") {
		t.Fatalf("DefaultAPIURL = %q — a released binary would point at the user's own machine", DefaultAPIURL)
	}
	if !strings.HasPrefix(DefaultAPIURL, "https://") {
		t.Errorf("DefaultAPIURL = %q — credentials must not travel over plaintext by default", DefaultAPIURL)
	}
}
