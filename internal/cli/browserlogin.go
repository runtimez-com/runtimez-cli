package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/api"
	"github.com/runtimez-com/runtimez-cli/internal/auth"
	"github.com/runtimez-com/runtimez-cli/internal/browser"
	"github.com/runtimez-com/runtimez-cli/internal/config"
)

// loginTimeout bounds the wait for the browser round trip.
const loginTimeout = 3 * time.Minute

// browserLogin runs the loopback OAuth flow.
//
// The listener is started BEFORE the browser opens, because the redirect can arrive faster
// than a slow terminal can print. The port is chosen by the OS and travels in returnTo,
// which the backend validates against a strict loopback pattern.
func browserLogin(cmd *cobra.Command, baseURL, provider, contextName string, noBrowser bool) error {
	out := cmd.OutOrStdout()

	probe := api.New(baseURL, nil)
	probe.HTTP.Timeout = flags.timeout
	providers, err := probe.OAuthProviders(cmd.Context())
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w", baseURL, err)
	}
	provider, err = pickProvider(providers, provider)
	if err != nil {
		return err
	}

	// 127.0.0.1 rather than localhost: the backend allowlists the literal address, and a
	// hostname could resolve to something else entirely.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("cannot open a local listener for the sign-in redirect: %w", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	codes := make(chan string, 1)
	srv := &http.Server{Handler: callbackHandler(codes)}
	go func() { _ = srv.Serve(listener) }()
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	authorize := fmt.Sprintf("%s/eac/api/1.0/auth/oauth/%s/authorize?returnTo=%s",
		strings.TrimRight(baseURL, "/"), url.PathEscape(provider), url.QueryEscape(redirect))

	opened := false
	if !noBrowser && browser.Available() {
		if err := browser.Open(authorize); err == nil {
			opened = true
			fmt.Fprintf(out, "Opening %s sign-in in your browser…\n", provider)
		}
	}
	if !opened {
		// The listener is on THIS machine, so a browser elsewhere needs the port forwarded
		// or the redirect lands nowhere. Saying that plainly beats a silent hang.
		fmt.Fprintf(out, "Open this URL to sign in:\n\n  %s\n\n", authorize)
		fmt.Fprintf(out, "The redirect returns to 127.0.0.1:%d on THIS machine.\n", port)
		fmt.Fprintf(out, "Over SSH, forward it first from your laptop:\n\n  ssh -L %d:127.0.0.1:%d %s\n\n",
			port, port, hostHint())
		fmt.Fprintln(out, "Without a tunnel, use an API key instead: rtz login --token rk_...")
	}

	fmt.Fprintln(out, "Waiting for the browser…")

	ctx, cancel := context.WithTimeout(cmd.Context(), loginTimeout)
	defer cancel()

	var code string
	select {
	case code = <-codes:
	case <-ctx.Done():
		return authErrorf("timed out waiting for the browser after %s — or use `rtz login --token rk_...`", loginTimeout)
	}

	res, err := api.ExchangeOAuthCode(cmd.Context(), baseURL, code)
	if err != nil {
		return err
	}
	if res.NeedsOrg {
		return &ExitError{Code: ExitFailure, Err: fmt.Errorf(
			"%s has no runtimez organization yet — create one in the web app first, then run `rtz login` again",
			firstNonEmpty(res.Email, "this account"))}
	}
	if res.Tokens == nil {
		return errors.New("the backend returned no tokens for this sign-in")
	}

	creds := res.Tokens.Credentials()
	name := firstNonEmpty(contextName, flags.contextName, defaultContextName(baseURL))

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cctx := cfg.Get(name)
	if cctx == nil {
		cctx = &config.Context{Name: name}
	}
	cctx.APIURL = baseURL
	if creds.OrgID != "" {
		cctx.OrgID = creds.OrgID
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

	fmt.Fprintf(out, "\nSigned in to %s as %s\n", baseURL, firstNonEmpty(res.Email, "(unknown)"))
	fmt.Fprintf(out, "Context %q is now current (credentials in the %s store)\n", name, store.Kind())
	fmt.Fprintln(out, "Next: rtz cluster ls")
	return nil
}

// callbackHandler captures the one-time code and tells the person they can close the tab.
func callbackHandler(codes chan<- string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, "<h3>Sign-in failed</h3><p>No code was returned. Run <code>rtz login</code> again.</p>")
			return
		}
		fmt.Fprint(w, "<h3>Signed in</h3><p>You can close this tab and return to your terminal.</p>")
		select {
		case codes <- code:
		default:
		}
	})
	return mux
}

// pickProvider resolves which social provider to use, refusing to guess when the choice is
// ambiguous and the user did not say.
func pickProvider(available map[string]bool, want string) (string, error) {
	enabled := make([]string, 0, len(available))
	for name, on := range available {
		if on {
			enabled = append(enabled, name)
		}
	}
	sort.Strings(enabled)

	if want != "" {
		for _, name := range enabled {
			if strings.EqualFold(name, want) {
				return name, nil
			}
		}
		if len(enabled) == 0 {
			return "", usageErrorf("this backend has no OAuth providers configured — use `rtz login --token rk_...`")
		}
		return "", usageErrorf("provider %q is not enabled here (available: %s)", want, strings.Join(enabled, ", "))
	}

	switch len(enabled) {
	case 0:
		return "", usageErrorf("this backend has no OAuth providers configured — use `rtz login --token rk_...`")
	case 1:
		return enabled[0], nil
	default:
		return "", usageErrorf("more than one sign-in provider is available (%s) — pick one with --provider",
			strings.Join(enabled, ", "))
	}
}

// hostHint fills the ssh -L example with something recognisable.
func hostHint() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "<this-host>"
}
