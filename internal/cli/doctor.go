package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/auth"
	"github.com/runtimez-com/runtimez-cli/internal/config"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

// check is one diagnostic line.
type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose configuration, connectivity, auth and agent health",
		Long: `Walks the whole path a normal command takes — config, credentials, backend
reachability, authentication, org and cluster resolution, telemetry ingest — and reports
where it breaks. Exits non-zero if any check fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := printer(cmd)
			if err != nil {
				return err
			}
			checks := runDiagnostics(cmd, cmd.Context())

			if p.Format.Structured() {
				if err := p.Print(checks, nil); err != nil {
					return err
				}
			} else {
				printChecks(cmd.OutOrStdout(), checks)
			}
			for _, c := range checks {
				if c.Status == "fail" {
					return &ExitError{Code: ExitFailure, Err: errors.New("one or more checks failed")}
				}
			}
			return nil
		},
	}
}

func runDiagnostics(cmd *cobra.Command, ctx context.Context) []check {
	var out []check
	add := func(name, status, detail string) { out = append(out, check{name, status, detail}) }

	path, perr := config.Path()
	if perr != nil {
		add("config", "fail", perr.Error())
		return out
	}
	cfg, err := config.Load()
	if err != nil {
		add("config", "fail", err.Error())
		return out
	}
	add("config", "ok", path)

	store := auth.Open()
	add("credential store", "ok", store.Kind())

	e, err := loadEnv(cmd, false)
	if err != nil {
		add("context", "fail", err.Error())
		return out
	}
	if e.ctx.Name == "" {
		add("context", "warn", "no context selected — run `rtz login`")
	} else {
		add("context", "ok", fmt.Sprintf("%s -> %s", e.ctx.Name, e.client.BaseURL))
	}
	_ = cfg

	// Reachability is probed with an unauthenticated endpoint, so a bad token cannot be
	// mistaken for a dead backend.
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := e.client.OAuthProviders(rctx); err != nil {
		add("api reachable", "fail", err.Error())
		return out
	}
	add("api reachable", "ok", e.client.BaseURL)

	if e.creds == nil {
		add("authentication", "fail", "not signed in — run `rtz login` or set RTZ_TOKEN")
		return out
	}
	me, err := e.client.Me(ctx)
	if err != nil {
		add("authentication", "fail", err.Error())
		return out
	}
	add("authentication", "ok", fmt.Sprintf("%s (%s)", firstNonEmpty(str(me["email"]), "unknown"), e.creds.Kind))

	orgID := firstNonEmpty(e.orgID, str(me["orgId"]))
	if orgID == "" {
		add("organization", "fail", "no org resolved — run `rtz org ls`")
		return out
	}
	add("organization", "ok", orgID)

	clusters, err := e.client.Clusters(ctx, orgID)
	if err != nil {
		add("clusters", "fail", err.Error())
		return out
	}
	switch {
	case len(clusters) == 0:
		add("clusters", "warn", "no clusters visible to this identity")
	case e.cluster == "":
		add("clusters", "warn", fmt.Sprintf("%d visible, none selected — run `rtz use <cluster>`", len(clusters)))
	default:
		found := false
		for _, c := range clusters {
			if c.ID == e.cluster {
				found = true
				add("clusters", "ok", fmt.Sprintf("%s (%s), %d visible", c.ID, render.Dash(c.Name), len(clusters)))
				break
			}
		}
		if !found {
			add("clusters", "fail", fmt.Sprintf("selected cluster %s is not visible to this identity", e.cluster))
		}
	}

	health, err := e.client.PipelineHealth(ctx, orgID)
	if err != nil {
		var apiErr *api.Error
		if errors.As(err, &apiErr) && apiErr.Forbidden() {
			add("telemetry ingest", "warn", "not readable with this credential")
		} else {
			add("telemetry ingest", "warn", err.Error())
		}
	} else {
		add("telemetry ingest", "ok", summarize(health))
	}

	return out
}

func printChecks(w io.Writer, checks []check) {
	table := &render.Table{Headers: []string{"", "CHECK", "DETAIL"}}
	for _, c := range checks {
		table.Rows = append(table.Rows, []string{marker(c.Status), c.Name, c.Detail})
	}
	_ = (&render.Printer{Out: w, Format: render.FormatTable}).Print(checks, table)
}

func marker(status string) string {
	switch status {
	case "ok":
		return "ok"
	case "warn":
		return "warn"
	default:
		return "FAIL"
	}
}

func summarize(m map[string]any) string {
	if len(m) == 0 {
		return "no data"
	}
	for _, key := range []string{"status", "state", "healthy"} {
		if v, ok := m[key]; ok {
			return fmt.Sprintf("%s=%v", key, v)
		}
	}
	return fmt.Sprintf("%d fields reported", len(m))
}
