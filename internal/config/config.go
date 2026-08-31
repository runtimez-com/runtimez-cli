// Package config holds the on-disk CLI configuration: named contexts pointing at an API
// base URL, an org, and a cluster — the kubectl model, because that is the muscle memory
// this CLI is competing with.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ErrNoContext is returned when no context is selected and none can be inferred.
var ErrNoContext = errors.New("no context selected")

// Context is one named target: which backend, which org, which cluster.
type Context struct {
	Name      string `yaml:"name"`
	APIURL    string `yaml:"apiUrl"`
	OrgID     string `yaml:"orgId,omitempty"`
	ClusterID string `yaml:"clusterId,omitempty"`
	// AuthRef keys the credential store entry for this context. Credentials themselves
	// never live in this file (FR-3).
	AuthRef string `yaml:"authRef,omitempty"`
}

// Config is the whole file.
type Config struct {
	CurrentContext string     `yaml:"currentContext,omitempty"`
	Contexts       []*Context `yaml:"contexts,omitempty"`

	path string `yaml:"-"`
}

// Dir resolves the config directory across platforms: ~/.config/runtimez on Linux,
// ~/Library/Application Support/runtimez on macOS, %APPDATA%\runtimez on Windows.
// RTZ_CONFIG overrides the full file path (and therefore the directory).
func Dir() (string, error) {
	if p := os.Getenv("RTZ_CONFIG"); p != "" {
		return filepath.Dir(p), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "runtimez"), nil
}

// Path is the config file location.
func Path() (string, error) {
	if p := os.Getenv("RTZ_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the config, returning an empty one if the file does not exist yet —
// a first run is not an error.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	cfg := &Config{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

// Save writes the config back, creating the directory on first use.
func (c *Config) Save() error {
	path := c.path
	if path == "" {
		p, err := Path()
		if err != nil {
			return err
		}
		path = p
		c.path = p
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	out, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Get returns the named context, or nil.
func (c *Config) Get(name string) *Context {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx
		}
	}
	return nil
}

// Current returns the active context.
func (c *Config) Current() (*Context, error) {
	if c.CurrentContext == "" {
		return nil, ErrNoContext
	}
	ctx := c.Get(c.CurrentContext)
	if ctx == nil {
		return nil, fmt.Errorf("%w: %q is not defined", ErrNoContext, c.CurrentContext)
	}
	return ctx, nil
}

// Upsert adds or replaces a context by name and returns it.
func (c *Config) Upsert(ctx *Context) *Context {
	if existing := c.Get(ctx.Name); existing != nil {
		*existing = *ctx
		return existing
	}
	c.Contexts = append(c.Contexts, ctx)
	sort.Slice(c.Contexts, func(i, j int) bool { return c.Contexts[i].Name < c.Contexts[j].Name })
	return c.Get(ctx.Name)
}

// Remove deletes a context, clearing the selection if it was the current one.
func (c *Config) Remove(name string) bool {
	for i, ctx := range c.Contexts {
		if ctx.Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			if c.CurrentContext == name {
				c.CurrentContext = ""
			}
			return true
		}
	}
	return false
}

// Use selects a context by name.
func (c *Config) Use(name string) error {
	if c.Get(name) == nil {
		return fmt.Errorf("context %q not found", name)
	}
	c.CurrentContext = name
	return nil
}
