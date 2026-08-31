package api

import (
	"context"
	"net/http"
	"time"

	"github.com/runtimez-com/runtimez-cli/internal/auth"
)

// TokenResponse mirrors io.runtimez.eac.auth.dto.TokenResponse. ExpiresIn is the ACCESS
// token lifetime in seconds; the refresh token's expiry is deliberately not published.
type TokenResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int64  `json:"expiresIn"`
	TokenType    string `json:"tokenType"`
	OrgID        string `json:"orgId"`
	Role         string `json:"role"`
}

// Credentials converts an issued pair into what the credential store persists.
func (t TokenResponse) Credentials() *auth.Credentials {
	return &auth.Credentials{
		Kind:         auth.KindJWT,
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(t.ExpiresIn) * time.Second),
		OrgID:        t.OrgID,
		Role:         t.Role,
	}
}

// RefreshTokens swaps a refresh token for a fresh pair. Used as the Client.Refresher, so it
// takes a bare base URL rather than an authenticated client — the old access token is,
// by definition, no longer usable at this point.
func RefreshTokens(ctx context.Context, baseURL, refreshToken string) (*auth.Credentials, error) {
	c := New(baseURL, nil)
	tok, err := Post[TokenResponse](ctx, c, "/eac/api/1.0/auth/refresh", nil,
		map[string]string{"refreshToken": refreshToken})
	if err != nil {
		return nil, err
	}
	return tok.Credentials(), nil
}

// ExchangeOAuthCode redeems the one-time code from the browser callback for a token pair.
func ExchangeOAuthCode(ctx context.Context, baseURL, code string) (*OAuthExchangeResult, error) {
	c := New(baseURL, nil)
	res, err := Post[OAuthExchangeResult](ctx, c, "/eac/api/1.0/auth/oauth/exchange", nil,
		map[string]string{"code": code})
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// OAuthExchangeResult mirrors the backend DTO: either a signed-in session, or a brand-new
// user who must name an organization first.
type OAuthExchangeResult struct {
	NeedsOrg    bool           `json:"needsOrg"`
	Tokens      *TokenResponse `json:"tokens"`
	Email       string         `json:"email"`
	DisplayName string         `json:"displayName"`
	Provider    string         `json:"provider"`
}

// OAuthProviders reports which social providers the backend has configured.
func (c *Client) OAuthProviders(ctx context.Context) (map[string]bool, error) {
	return Get[map[string]bool](ctx, c, "/eac/api/1.0/auth/oauth/providers", nil)
}

// Me returns the caller's identity. The payload is an open map on the backend, so it stays
// one here rather than pretending to a schema it does not have.
func (c *Client) Me(ctx context.Context) (map[string]any, error) {
	return Get[map[string]any](ctx, c, "/eac/api/1.0/users/me", nil)
}

// SwitchOrg re-scopes the session to another org the caller belongs to. JWT only — an API
// key is already pinned to its creator's org.
func (c *Client) SwitchOrg(ctx context.Context, orgID string) (*TokenResponse, error) {
	tok, err := Post[TokenResponse](ctx, c, "/eac/api/1.0/auth/switch-org", nil,
		map[string]string{"orgId": orgID})
	if err != nil {
		return nil, err
	}
	return &tok, nil
}

// Logout revokes a refresh token. Always succeeds server-side, even for a token that never
// existed, so a failure here is a transport problem and nothing more.
func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	_, err := c.Do(ctx, http.MethodPost, "/eac/api/1.0/auth/logout", nil,
		map[string]string{"refreshToken": refreshToken})
	return err
}
