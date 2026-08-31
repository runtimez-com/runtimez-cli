package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/auth"
	"github.com/runtimez-com/runtimez-cli/internal/config"
)

func newLoginCmd() *cobra.Command {
	var (
		token       string
		contextName string
		apiURL      string
		provider    string
		noBrowser   bool
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in and store credentials",
		Long: `Sign in to a runtimez backend and store the credentials for a named context.

Run it bare to sign in through your browser; the redirect returns to a listener on
127.0.0.1 that this command opens for the purpose.

An API key (rk_...) must carry the api:read scope. It then authorizes exactly as the user who
created it does for every cluster-scoped command, but carries no role — so organization and
user administration stay out of its reach by design and need an interactive sign-in.

An ingest key (ingest:metrics and friends) will NOT work here: those authorize the ingest
gateway, not this API.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			base := firstNonEmpty(apiURL, flags.apiURL, DefaultAPIURL)
			if strings.TrimSpace(token) == "" {
				return browserLogin(cmd, base, provider, contextName, noBrowser)
			}
			if !strings.HasPrefix(token, "rk_") {
				return usageErrorf("an API key starts with rk_ (got %q…)", safePrefix(token))
			}

			creds := &auth.Credentials{Kind: auth.KindAPIKey, APIKey: token}

			// Verify before persisting. Storing a token that does not work just moves the
			// failure to the next command, where it is harder to explain.
			client := api.New(base, creds)
			client.HTTP.Timeout = flags.timeout
			ctx := cmd.Context()
			me, err := client.Me(ctx)
			if err != nil {
				var apiErr *api.Error
				if errors.As(err, &apiErr) && apiErr.Unauthorized() {
					// The overwhelmingly likely cause is scope, not a typo: ingest keys are the
					// ones people already have, and they authorize the ingest gateway rather
					// than this API.
					return authErrorf(
						"that API key was rejected by %s — an rk_ key needs the api:read scope "+
							"(ingest:* keys authorize the ingest gateway, not the API)", base)
				}
				return err
			}

			orgID, _ := me["orgId"].(string)
			email, _ := me["email"].(string)
			creds.OrgID = orgID

			name := firstNonEmpty(contextName, flags.contextName, defaultContextName(base))
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cctx := cfg.Get(name)
			if cctx == nil {
				cctx = &config.Context{Name: name}
			}
			cctx.APIURL = base
			if orgID != "" {
				cctx.OrgID = orgID
			}
			cfg.Upsert(cctx)
			cfg.CurrentContext = name
			if err := cfg.Save(); err != nil {
				return err
			}

			store := auth.Open()
			if err := store.Save(credentialRef(cctx), creds); err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Signed in to %s as %s\n", base, firstNonEmpty(email, "(unknown)"))
			fmt.Fprintf(out, "Context %q is now current (credentials in the %s store)\n", name, store.Kind())
			if orgID != "" {
				fmt.Fprintf(out, "Organization: %s\n", orgID)
			}
			fmt.Fprintln(out, "Next: rtz cluster ls")
			return nil
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "API key to sign in with (rk_...)")
	cmd.Flags().StringVar(&contextName, "name", "", "name for the stored context (default: derived from the API host)")
	cmd.Flags().StringVar(&apiURL, "api-url", "", "backend to sign in to (default: --api or "+DefaultAPIURL+")")
	cmd.Flags().StringVar(&provider, "provider", "", "sign-in provider: google or github")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL instead of opening a browser")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for the current context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			name := firstNonEmpty(flags.contextName, cfg.CurrentContext)
			if name == "" {
				return usageErrorf("no context selected")
			}
			cctx := cfg.Get(name)
			if cctx == nil {
				return usageErrorf("context %q not found", name)
			}

			store := auth.Open()
			ref := credentialRef(cctx)

			// Best-effort revoke: a JWT's refresh token outlives the local copy, so dropping
			// the file without telling the server would leave it usable.
			if creds, lerr := store.Load(ref); lerr == nil && creds.Kind == auth.KindJWT && creds.RefreshToken != "" {
				client := api.New(cctx.APIURL, creds)
				_ = client.Logout(context.Background(), creds.RefreshToken)
			}

			if err := store.Delete(ref); err != nil && !errors.Is(err, auth.ErrNotFound) {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed credentials for context %q\n", name)
			return nil
		},
	}
}

// defaultContextName turns an API URL into a short, stable context name.
func defaultContextName(apiURL string) string {
	s := apiURL
	for _, prefix := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if i := strings.IndexAny(s, "/:"); i > 0 {
		s = s[:i]
	}
	if s == "" {
		return "default"
	}
	return s
}

func safePrefix(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[:4]
}
