package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/zalando/go-keyring"

	"github.com/runtimez-com/runtimez-cli/internal/config"
)

// ErrNotFound means no credentials are stored for that reference.
var ErrNotFound = errors.New("no credentials stored")

const keyringService = "runtimez-cli"

// Store persists credentials for a context reference.
type Store interface {
	Load(ref string) (*Credentials, error)
	Save(ref string, c *Credentials) error
	Delete(ref string) error
	// Kind names the backing store, for `rtz doctor` and for telling a user where their
	// token actually landed.
	Kind() string
}

var (
	openOnce sync.Once
	opened   Store
)

// Open returns the OS keychain when one is reachable, and a 0600 file otherwise.
//
// The fallback is not a degraded mode to warn about: headless Linux — a container, a CI
// runner, an SSH session with no D-Bus session bus — has no Secret Service at all, and that
// is the single most common place this CLI runs.
func Open() Store {
	openOnce.Do(func() {
		if keyringUsable() {
			opened = &keyringStore{}
			return
		}
		opened = &fileStore{}
	})
	return opened
}

// keyringUsable probes with a real round trip. Availability cannot be inferred from GOOS:
// macOS and Windows always have a store, Linux has one only sometimes, and a probe is the
// only answer that is true on the machine actually running.
func keyringUsable() bool {
	const probe = "__rtz_probe__"
	if err := keyring.Set(keyringService, probe, "1"); err != nil {
		return false
	}
	if _, err := keyring.Get(keyringService, probe); err != nil {
		return false
	}
	_ = keyring.Delete(keyringService, probe)
	return true
}

type keyringStore struct{}

func (keyringStore) Kind() string { return "keychain" }

func (keyringStore) Load(ref string) (*Credentials, error) {
	raw, err := keyring.Get(keyringService, ref)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read keychain: %w", err)
	}
	var c Credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("decode stored credentials: %w", err)
	}
	return &c, nil
}

func (keyringStore) Save(ref string, c *Credentials) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	if err := keyring.Set(keyringService, ref, string(raw)); err != nil {
		return fmt.Errorf("write keychain: %w", err)
	}
	return nil
}

func (keyringStore) Delete(ref string) error {
	err := keyring.Delete(keyringService, ref)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

type fileStore struct{}

func (fileStore) Kind() string { return "file" }

func credentialsPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

func (fileStore) readAll() (map[string]*Credentials, string, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, "", err
	}
	out := map[string]*Credentials{}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return out, path, nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	return out, path, nil
}

func (f fileStore) Load(ref string) (*Credentials, error) {
	all, _, err := f.readAll()
	if err != nil {
		return nil, err
	}
	c, ok := all[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (f fileStore) Save(ref string, c *Credentials) error {
	all, path, err := f.readAll()
	if err != nil {
		return err
	}
	all[ref] = c
	return writeCredentials(path, all)
}

func (f fileStore) Delete(ref string) error {
	all, path, err := f.readAll()
	if err != nil {
		return err
	}
	if _, ok := all[ref]; !ok {
		return ErrNotFound
	}
	delete(all, ref)
	return writeCredentials(path, all)
}

func writeCredentials(path string, all map[string]*Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	raw, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	// 0600 is a no-op on Windows, where the keychain is always available and this path is
	// therefore not reached in practice.
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
