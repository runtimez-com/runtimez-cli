package api

import "context"

// Org is one organization the caller belongs to (MyOrgResponse).
type Org struct {
	OrgID   string `json:"orgId"`
	Name    string `json:"name"`
	Plan    string `json:"plan"`
	Role    string `json:"role"`
	Current bool   `json:"current"`
}

// MyOrgs lists the orgs the caller can reach.
func (c *Client) MyOrgs(ctx context.Context) ([]Org, error) {
	return Get[[]Org](ctx, c, "/eac/api/1.0/orgs/mine", nil)
}
