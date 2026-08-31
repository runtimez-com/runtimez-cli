package config

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("RTZ_CONFIG", path)
	return path
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	withTempConfig(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load on a fresh machine: %v", err)
	}
	if len(cfg.Contexts) != 0 || cfg.CurrentContext != "" {
		t.Fatalf("expected an empty config, got %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := withTempConfig(t)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Upsert(&Context{Name: "prod", APIURL: "https://api.example.com", OrgID: "org1", ClusterID: "c1"})
	cfg.CurrentContext = "prod"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The config file carries no secrets, but it does carry the whole target list; 0600
	// keeps it consistent with the credentials file beside it.
	if perm := info.Mode().Perm(); perm != 0o600 && os.Getenv("GOOS") != "windows" {
		t.Errorf("config permissions = %v, want 0600", perm)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := got.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if ctx.OrgID != "org1" || ctx.ClusterID != "c1" || ctx.APIURL != "https://api.example.com" {
		t.Errorf("round trip lost data: %+v", ctx)
	}
}

func TestCurrentWithoutSelection(t *testing.T) {
	withTempConfig(t)
	cfg, _ := Load()
	if _, err := cfg.Current(); err == nil {
		t.Fatal("expected an error when no context is selected")
	}
}

func TestRemoveClearsSelection(t *testing.T) {
	withTempConfig(t)
	cfg, _ := Load()
	cfg.Upsert(&Context{Name: "a", APIURL: "http://a"})
	cfg.Upsert(&Context{Name: "b", APIURL: "http://b"})
	if err := cfg.Use("a"); err != nil {
		t.Fatal(err)
	}

	if !cfg.Remove("a") {
		t.Fatal("Remove reported no match")
	}
	if cfg.CurrentContext != "" {
		t.Errorf("removing the current context left it selected: %q", cfg.CurrentContext)
	}
	if cfg.Get("b") == nil {
		t.Error("Remove took out the wrong context")
	}
}

func TestUpsertReplacesInPlace(t *testing.T) {
	withTempConfig(t)
	cfg, _ := Load()
	cfg.Upsert(&Context{Name: "a", APIURL: "http://a", OrgID: "one"})
	cfg.Upsert(&Context{Name: "a", APIURL: "http://a", OrgID: "two"})

	if n := len(cfg.Contexts); n != 1 {
		t.Fatalf("Upsert duplicated the context: %d entries", n)
	}
	if cfg.Get("a").OrgID != "two" {
		t.Errorf("Upsert did not replace the value: %+v", cfg.Get("a"))
	}
}

func TestUseRejectsUnknownContext(t *testing.T) {
	withTempConfig(t)
	cfg, _ := Load()
	if err := cfg.Use("nope"); err == nil {
		t.Fatal("expected an error for an unknown context")
	}
}
