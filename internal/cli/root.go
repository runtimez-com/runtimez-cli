// Package cli wires cobra commands over internal/api. Commands here stay thin: parse flags,
// call one API method, hand the result to internal/render.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/auth"
	"github.com/runtimez-com/runtimez-cli/internal/config"
	"github.com/runtimez-com/runtimez-cli/internal/render"
	"github.com/runtimez-com/runtimez-cli/internal/tui"
	"github.com/runtimez-com/runtimez-cli/internal/version"
)

// DefaultAPIURL is the hosted runtimez backend, used when nothing else names one.
//
// A self-hosted deployment overrides it once with --api or RTZ_API and the context remembers
// it. Defaulting to localhost would have been correct only for someone running eac on the
// machine they are typing on — for everyone who installs a release, it points at nothing.
const DefaultAPIURL = "https://app.runtimez.io"

type globalFlags struct {
	contextName string
	apiURL      string
	orgID       string
	clusterID   string
	output      string
	timeout     time.Duration
	refresh     time.Duration
	initialView string
}

var flags globalFlags

// Execute runs the root command and returns the process exit code.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		return report(err)
	}
	return ExitOK
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "rtz",
		Short: "Operate and troubleshoot Kubernetes from the runtimez control plane",
		Long: `rtz answers operational questions — what is running, what changed, what is risky,
is this cluster upgrade-safe, why is this workload slow — straight from the runtimez API.

No kubeconfig, no cluster VPN, no browser.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Bare `rtz` opens the interactive UI. On a terminal that cannot host it — a pipe, a
		// CI log, TERM=dumb — help is printed instead of a half-rendered screen.
		RunE: func(cmd *cobra.Command, args []string) error {
			if !tui.Capable() {
				return cmd.Help()
			}
			return runTUI(cmd)
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&flags.contextName, "context", "", "config context to use (env RTZ_CONTEXT)")
	pf.StringVar(&flags.apiURL, "api", "", "API base URL, overriding the context (env RTZ_API)")
	pf.StringVar(&flags.orgID, "org", "", "organization id, overriding the context (env RTZ_ORG)")
	pf.StringVar(&flags.clusterID, "cluster", "", "cluster id, overriding the context (env RTZ_CLUSTER)")
	pf.StringVarP(&flags.output, "output", "o", "table", "output format: table, wide, json, yaml")
	pf.DurationVar(&flags.timeout, "timeout", 60*time.Second, "per-request timeout")
	pf.DurationVar(&flags.refresh, "refresh", 10*time.Second, "interactive UI auto-refresh interval")
	pf.StringVar(&flags.initialView, "view", "pods", "view to open the interactive UI on")

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &ExitError{Code: ExitUsage, Err: err}
	})

	root.AddCommand(
		newVersionCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newWhoamiCmd(),
		newOrgCmd(),
		newClusterCmd(),
		newUseCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newGetCmd(),
		newSearchCmd(),
		newDescribeCmd(),
		newNamespacesCmd(),
		newCountsCmd(),
		newFleetCmd(),
		newRiskCmd(),
		newRcaCmd(),
		newSignalsCmd(),
		newChangesCmd(),
		newLogsCmd(),
		newTraceCmd(),
		newMetricsCmd(),
		newAskCmd(),
		newUpgradeCmd(),
	)
	return root
}

// env is a resolved invocation: config plus overrides plus credentials plus a client.
type env struct {
	cfg     *config.Config
	ctx     *config.Context
	creds   *auth.Credentials
	client  *api.Client
	printer *render.Printer
	orgID   string
	cluster string
}

// firstNonEmpty picks the first set value, in precedence order.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// printer builds the output printer alone — for commands that touch no backend.
//
// It writes through the command rather than straight to os.Stdout so that output stays
// redirectable: tests capture it, and a caller can retarget it without every command
// remembering to.
func printer(cmd *cobra.Command) (*render.Printer, error) {
	format, err := render.ParseFormat(flags.output)
	if err != nil {
		return nil, &ExitError{Code: ExitUsage, Err: err}
	}
	return &render.Printer{Out: cmd.OutOrStdout(), Format: format}, nil
}

// loadEnv resolves everything a backend-touching command needs.
//
// Precedence is flag, then environment, then the selected context — the same order kubectl
// uses, because a --flag that loses to a config file is the kind of surprise that costs an
// hour during an incident.
func loadEnv(cmd *cobra.Command, requireAuth bool) (*env, error) {
	p, err := printer(cmd)
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	name := firstNonEmpty(flags.contextName, os.Getenv("RTZ_CONTEXT"), cfg.CurrentContext)
	var cctx *config.Context
	if name != "" {
		cctx = cfg.Get(name)
		if cctx == nil && flags.contextName != "" {
			return nil, usageErrorf("context %q not found in %s", name, mustConfigPath())
		}
	}
	if cctx == nil {
		cctx = &config.Context{Name: name}
	}

	apiURL := firstNonEmpty(flags.apiURL, os.Getenv("RTZ_API"), cctx.APIURL, DefaultAPIURL)
	orgID := firstNonEmpty(flags.orgID, os.Getenv("RTZ_ORG"), cctx.OrgID)
	clusterID := firstNonEmpty(flags.clusterID, os.Getenv("RTZ_CLUSTER"), cctx.ClusterID)

	creds, err := resolveCredentials(cctx)
	if err != nil && requireAuth {
		return nil, err
	}

	client := api.New(apiURL, creds)
	client.HTTP.Timeout = flags.timeout
	client.Refresh = func(ctx context.Context, refreshToken string) (*auth.Credentials, error) {
		return api.RefreshTokens(ctx, apiURL, refreshToken)
	}
	ref := credentialRef(cctx)
	client.OnCredentialChange = func(c *auth.Credentials) error {
		return auth.Open().Save(ref, c)
	}

	// An org-scoped path is in almost every URL, so filling it from the token beats failing
	// with "missing --org" when the answer was already in hand.
	if orgID == "" && creds != nil {
		orgID = creds.OrgID
	}

	return &env{
		cfg: cfg, ctx: cctx, creds: creds, client: client,
		printer: p, orgID: orgID, cluster: clusterID,
	}, nil
}

// requireOrg fails with a usage error rather than issuing a request that cannot succeed.
func (e *env) requireOrg() (string, error) {
	if e.orgID == "" {
		return "", usageErrorf("no organization selected — pass --org, or run `rtz org ls` then `rtz use`")
	}
	return e.orgID, nil
}

// requireCluster does the same for cluster-scoped commands.
func (e *env) requireCluster() (string, error) {
	if e.cluster == "" {
		return "", usageErrorf("no cluster selected — pass --cluster, or run `rtz cluster ls` then `rtz use <cluster>`")
	}
	return e.cluster, nil
}

// credentialRef keys the credential store. It follows the context name so two contexts
// against different backends never share a token.
func credentialRef(c *config.Context) string {
	if c == nil || c.Name == "" {
		return "default"
	}
	if c.AuthRef != "" {
		return c.AuthRef
	}
	return c.Name
}

func resolveCredentials(cctx *config.Context) (*auth.Credentials, error) {
	// An explicit token in the environment wins outright: that is how CI passes one, and it
	// must not be silently overridden by whatever happens to be on disk.
	if tok := os.Getenv("RTZ_TOKEN"); tok != "" {
		return &auth.Credentials{Kind: auth.KindAPIKey, APIKey: tok}, nil
	}
	creds, err := auth.Open().Load(credentialRef(cctx))
	if errors.Is(err, auth.ErrNotFound) {
		return nil, authErrorf("not signed in — run `rtz login` or set RTZ_TOKEN")
	}
	if err != nil {
		return nil, err
	}
	return creds, nil
}

func mustConfigPath() string {
	p, err := config.Path()
	if err != nil {
		return "the config file"
	}
	return p
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := printer(cmd)
			if err != nil {
				return err
			}
			if p.Format.Structured() {
				return p.Print(map[string]string{
					"version": version.Version,
					"commit":  version.Commit,
					"date":    version.Date,
				}, nil)
			}
			fmt.Fprintln(cmd.OutOrStdout(), version.String())
			return nil
		},
	}
}
