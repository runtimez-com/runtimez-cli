package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/auth"
)

func apiKey() *auth.Credentials {
	return &auth.Credentials{Kind: auth.KindAPIKey, APIKey: "rk_test"}
}

func TestGetUnwrapsEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer rk_test" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]string{{"orgId": "org1", "name": "Acme"}},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, apiKey())
	orgs, err := c.MyOrgs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orgs) != 1 || orgs[0].OrgID != "org1" || orgs[0].Name != "Acme" {
		t.Fatalf("unwrapped payload wrong: %+v", orgs)
	}
}

func TestErrorEnvelopeBecomesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "Forbidden"})
	}))
	defer srv.Close()

	_, err := New(srv.URL, apiKey()).MyOrgs(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *api.Error, got %T: %v", err, err)
	}
	if !apiErr.Forbidden() || apiErr.Message != "Forbidden" {
		t.Fatalf("error did not carry the backend's reason: %+v", apiErr)
	}
}

// A 200 body that says success:false is the one shape a status-only check would call a
// success, so it gets its own test.
func TestSuccessFalseOnHTTP200IsStillAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "nope"})
	}))
	defer srv.Close()

	if _, err := New(srv.URL, apiKey()).MyOrgs(context.Background()); err == nil {
		t.Fatal("success:false on a 200 was treated as success")
	}
}

func TestRetriesServerErrorsThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]string{}})
	}))
	defer srv.Close()

	c := New(srv.URL, apiKey())
	if _, err := c.MyOrgs(context.Background()); err != nil {
		t.Fatalf("a retryable 502 was not retried: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := New(srv.URL, apiKey()).MyOrgs(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("a 404 was retried %d times", got-1)
	}
}

func TestUnauthorizedJWTRefreshesAndRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]any{"success": false, "error": "expired"})
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fresh" {
			t.Errorf("retry used %q, want the refreshed token", got)
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []map[string]string{}})
	}))
	defer srv.Close()

	creds := &auth.Credentials{
		Kind: auth.KindJWT, AccessToken: "stale", RefreshToken: "r1",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	c := New(srv.URL, creds)

	var persisted int
	c.OnCredentialChange = func(*auth.Credentials) error { persisted++; return nil }
	c.Refresh = func(context.Context, string) (*auth.Credentials, error) {
		return &auth.Credentials{
			Kind: auth.KindJWT, AccessToken: "fresh", RefreshToken: "r2",
			ExpiresAt: time.Now().Add(time.Hour),
		}, nil
	}

	if _, err := c.MyOrgs(context.Background()); err != nil {
		t.Fatalf("refresh-and-retry failed: %v", err)
	}
	if persisted != 1 {
		t.Errorf("refreshed credentials persisted %d times, want 1", persisted)
	}
	if creds.AccessToken != "fresh" {
		t.Errorf("in-place credential update did not happen: %q", creds.AccessToken)
	}
}

// An API key has nothing to refresh; retrying would just burn a second request on the same
// rejection.
func TestUnauthorizedAPIKeyDoesNotRetry(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, apiKey())
	c.Refresh = func(context.Context, string) (*auth.Credentials, error) {
		t.Fatal("refresh was attempted for an API key")
		return nil, nil
	}
	if _, err := c.MyOrgs(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

func TestNonJSONErrorBodyStillReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>gateway</html>"))
	}))
	defer srv.Close()

	c := New(srv.URL, apiKey())
	c.MaxRetries = 0
	_, err := c.MyOrgs(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadGateway {
		t.Fatalf("want a 502 api.Error, got %v", err)
	}
}

func TestClusterListDecodesBackendShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/eac/api/1.0/orgs/org1/clusters" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Write([]byte(`{"success":true,"data":[{"id":"c1","name":"prod","status":"CONNECTED",
			"kubernetesVersion":"1.30.4","nodeCount":3,"lastHeartbeatAt":"2026-08-29T10:00:00Z"}]}`))
	}))
	defer srv.Close()

	clusters, err := New(srv.URL, apiKey()).Clusters(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("got %d clusters", len(clusters))
	}
	c := clusters[0]
	if c.ID != "c1" || c.KubernetesVersion != "1.30.4" || c.NodeCount == nil || *c.NodeCount != 3 {
		t.Fatalf("decode lost fields: %+v", c)
	}
	if c.LastHeartbeatAt == nil || c.LastHeartbeatAt.Year() != 2026 {
		t.Fatalf("timestamp not parsed: %+v", c.LastHeartbeatAt)
	}
	// resourceCount was absent from the payload; a nil pointer is what lets the renderer
	// distinguish "not reported" from a real zero.
	if c.ResourceCount != nil {
		t.Errorf("absent field decoded as %v, want nil", *c.ResourceCount)
	}
}
