// Package auth stores and refreshes CLI credentials.
//
// Two credential kinds exist and they are not interchangeable:
//
//   - An API key (rk_...) authorizes exactly as the user who created it does for every
//     cluster-scoped endpoint, but carries no role — so org/user administration is out of
//     reach for it by design.
//   - A JWT pair from the browser login carries the caller's real role and can do everything.
package auth

import (
	"time"
)

// Kind distinguishes the two credential shapes.
type Kind string

const (
	KindAPIKey Kind = "api-key"
	KindJWT    Kind = "jwt"
)

// Credentials is what gets persisted for one context.
type Credentials struct {
	Kind         Kind      `json:"kind"`
	APIKey       string    `json:"apiKey,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	OrgID        string    `json:"orgId,omitempty"`
	Role         string    `json:"role,omitempty"`
}

// Bearer is the value for the Authorization header.
func (c *Credentials) Bearer() string {
	if c == nil {
		return ""
	}
	if c.Kind == KindAPIKey {
		return c.APIKey
	}
	return c.AccessToken
}

// NeedsRefresh reports whether the access token is expired or close enough that a
// long-running command would trip over it mid-flight.
func (c *Credentials) NeedsRefresh() bool {
	if c == nil || c.Kind != KindJWT || c.RefreshToken == "" {
		return false
	}
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(60 * time.Second).After(c.ExpiresAt)
}
