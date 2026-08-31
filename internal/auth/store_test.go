package auth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// The file store is exercised directly rather than through Open(): on a developer laptop
// Open() finds a real keychain, and a test must never write to that.
func newFileStore(t *testing.T) Store {
	t.Helper()
	t.Setenv("RTZ_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	return fileStore{}
}

func TestFileStoreRoundTrip(t *testing.T) {
	s := newFileStore(t)
	want := &Credentials{Kind: KindAPIKey, APIKey: "rk_abc", OrgID: "org1"}

	if err := s.Save("prod", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("prod")
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != want.APIKey || got.Kind != want.Kind || got.OrgID != want.OrgID {
		t.Fatalf("round trip lost data: %+v", got)
	}
}

func TestFileStoreMissingRefIsErrNotFound(t *testing.T) {
	s := newFileStore(t)
	if _, err := s.Load("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Delete("absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete want ErrNotFound, got %v", err)
	}
}

func TestFileStoreKeepsContextsSeparate(t *testing.T) {
	s := newFileStore(t)
	if err := s.Save("prod", &Credentials{Kind: KindAPIKey, APIKey: "rk_prod"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("stage", &Credentials{Kind: KindAPIKey, APIKey: "rk_stage"}); err != nil {
		t.Fatal(err)
	}

	// Saving the second must not clobber the first — two contexts against two backends is
	// the normal case, not an edge case.
	prod, err := s.Load("prod")
	if err != nil || prod.APIKey != "rk_prod" {
		t.Fatalf("prod credentials lost: %+v %v", prod, err)
	}

	if err := s.Delete("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("stage"); err != nil {
		t.Fatalf("deleting one context removed another: %v", err)
	}
}

func TestFileStoreWritesRestrictedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions do not apply on Windows")
	}
	s := newFileStore(t)
	if err := s.Save("prod", &Credentials{Kind: KindAPIKey, APIKey: "rk_abc"}); err != nil {
		t.Fatal(err)
	}
	path, err := credentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("credentials file mode = %v, want 0600", perm)
	}
}

func TestBearerPicksTheRightToken(t *testing.T) {
	apiKey := &Credentials{Kind: KindAPIKey, APIKey: "rk_abc", AccessToken: "ignored"}
	if got := apiKey.Bearer(); got != "rk_abc" {
		t.Errorf("api key Bearer() = %q", got)
	}
	jwt := &Credentials{Kind: KindJWT, AccessToken: "jwt"}
	if got := jwt.Bearer(); got != "jwt" {
		t.Errorf("jwt Bearer() = %q", got)
	}
	var nilCreds *Credentials
	if got := nilCreds.Bearer(); got != "" {
		t.Errorf("nil Bearer() = %q, want empty", got)
	}
}

func TestNeedsRefresh(t *testing.T) {
	cases := []struct {
		name string
		in   *Credentials
		want bool
	}{
		{"nil", nil, false},
		{"api key never refreshes", &Credentials{Kind: KindAPIKey, APIKey: "rk_a"}, false},
		{"no refresh token", &Credentials{Kind: KindJWT, ExpiresAt: time.Now().Add(-time.Hour)}, false},
		{"unknown expiry", &Credentials{Kind: KindJWT, RefreshToken: "r"}, false},
		{"expired", &Credentials{Kind: KindJWT, RefreshToken: "r", ExpiresAt: time.Now().Add(-time.Minute)}, true},
		// The 60s skew is the point: a token valid for another 10 seconds will expire
		// mid-command, and refreshing after the 401 costs a wasted round trip.
		{"about to expire", &Credentials{Kind: KindJWT, RefreshToken: "r", ExpiresAt: time.Now().Add(10 * time.Second)}, true},
		{"fresh", &Credentials{Kind: KindJWT, RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}, false},
	}
	for _, tc := range cases {
		if got := tc.in.NeedsRefresh(); got != tc.want {
			t.Errorf("%s: NeedsRefresh() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
