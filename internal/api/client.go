// Package api is the only package in this CLI that knows HTTP. Both the flag commands and
// the TUI are consumers of it, which is what keeps the two surfaces from drifting into
// separate code paths.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/auth"
	"github.com/runtimez-com/runtimez-cli/internal/version"
)

// envelope is the shape every EAC endpoint returns: ApiResponse.ok(data) /
// ApiResponse.error(msg). Callers never see it — Do unwraps to data.
type envelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Error   string          `json:"error"`
}

// Error is a failed API call, carrying enough to print one useful line.
type Error struct {
	Status  int
	Method  string
	Path    string
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s %s: %s (HTTP %d)", e.Method, e.Path, e.Message, e.Status)
	}
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.Path, e.Status)
}

// Unauthorized reports whether this error means "log in again".
func (e *Error) Unauthorized() bool { return e.Status == http.StatusUnauthorized }

// Forbidden reports whether the caller authenticated but lacks access. For an API key this
// most often means the command needs a role an API key cannot carry.
func (e *Error) Forbidden() bool { return e.Status == http.StatusForbidden }

// Refresher exchanges a refresh token for a new pair. Injected so this package does not
// have to own credential persistence.
type Refresher func(ctx context.Context, refreshToken string) (*auth.Credentials, error)

// Client talks to one EAC backend.
type Client struct {
	BaseURL string
	HTTP    *http.Client

	// Creds is mutated in place on refresh; OnCredentialChange persists the new pair.
	Creds              *auth.Credentials
	OnCredentialChange func(*auth.Credentials) error
	Refresh            Refresher

	// MaxRetries bounds retries on 429 and 5xx. Zero means the default.
	MaxRetries int
}

// New builds a client with sane transport defaults.
func New(baseURL string, creds *auth.Credentials) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		Creds:      creds,
		MaxRetries: 2,
	}
}

// URL builds an absolute URL from a path and optional query values.
func (c *Client) URL(path string, query url.Values) string {
	u := c.BaseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// Do performs a request and returns the unwrapped `data` payload.
//
// body may be nil. A 401 on a JWT credential triggers one refresh-and-retry; an API key
// has nothing to refresh, so it surfaces immediately.
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request body: %w", err)
		}
	}

	if c.Creds.NeedsRefresh() {
		// Best-effort: a failed pre-emptive refresh still lets the request try, and the
		// 401 path below gives a second chance with a clearer error.
		_ = c.refresh(ctx)
	}

	data, err := c.attempt(ctx, method, path, query, payload)
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Unauthorized() && c.Creds != nil && c.Creds.Kind == auth.KindJWT {
		if rerr := c.refresh(ctx); rerr == nil {
			return c.attempt(ctx, method, path, query, payload)
		}
	}
	return data, err
}

func (c *Client) attempt(ctx context.Context, method, path string, query url.Values, payload []byte) (json.RawMessage, error) {
	retries := c.MaxRetries
	if retries < 0 {
		retries = 0
	}

	var lastErr error
	for i := 0; i <= retries; i++ {
		if i > 0 {
			backoff := time.Duration(1<<uint(i-1)) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.URL(path, query), reader)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "rtz/"+version.Version)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer := c.Creds.Bearer(); bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s %s: %w", method, path, err)
			continue // a transport error is exactly what retries are for
		}

		data, err := decode(resp, method, path)
		resp.Body.Close()
		if err == nil {
			return data, nil
		}
		lastErr = err

		var apiErr *Error
		if errors.As(err, &apiErr) && !retryable(apiErr.Status) {
			return nil, err
		}
	}
	return nil, lastErr
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func decode(resp *http.Response, method, path string) (json.RawMessage, error) {
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if readErr != nil {
		return nil, fmt.Errorf("%s %s: read response: %w", method, path, readErr)
	}

	var env envelope
	// A non-JSON body is normal for gateway errors and auth redirects, so a decode failure
	// is not itself the story — the status is.
	decoded := json.Unmarshal(raw, &env) == nil

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := ""
		if decoded && env.Error != "" {
			msg = env.Error
		} else if decoded && env.Message != "" {
			msg = env.Message
		} else if len(raw) > 0 && len(raw) < 400 {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, &Error{Status: resp.StatusCode, Method: method, Path: path, Message: msg}
	}

	if !decoded {
		return nil, fmt.Errorf("%s %s: response was not JSON", method, path)
	}
	if !env.Success {
		return nil, &Error{Status: resp.StatusCode, Method: method, Path: path, Message: env.Error}
	}
	return env.Data, nil
}

func (c *Client) refresh(ctx context.Context) error {
	if c.Refresh == nil || c.Creds == nil || c.Creds.RefreshToken == "" {
		return errors.New("no refresh token available")
	}
	next, err := c.Refresh(ctx, c.Creds.RefreshToken)
	if err != nil {
		return err
	}
	*c.Creds = *next
	if c.OnCredentialChange != nil {
		return c.OnCredentialChange(c.Creds)
	}
	return nil
}

// Get is the typed read helper. It is a free function because Go methods cannot have their
// own type parameters.
func Get[T any](ctx context.Context, c *Client, path string, query url.Values) (T, error) {
	var out T
	raw, err := c.Do(ctx, http.MethodGet, path, query, nil)
	if err != nil {
		return out, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode %s: %w", path, err)
	}
	return out, nil
}

// Post is the typed write helper.
func Post[T any](ctx context.Context, c *Client, path string, query url.Values, body any) (T, error) {
	var out T
	raw, err := c.Do(ctx, http.MethodPost, path, query, body)
	if err != nil {
		return out, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decode %s: %w", path, err)
	}
	return out, nil
}
