package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TokenCreateRequest is the body for creating an API token (POST /api/security/token).
// The server accepts both application/json and application/x-www-form-urlencoded
// (auth-model.md section 3.3). Description is sent for wire compatibility but is
// not part of the real endpoint's accepted field set (security.go
// tokenCreateForm) — the server ignores it.
type TokenCreateRequest struct {
	Username    string `json:"username"`
	Scope       string `json:"scope,omitempty"`       // e.g., "api:*"
	ExpiresIn   int64  `json:"expires_in,omitempty"`  // seconds
	Description string `json:"description,omitempty"` // human-readable label
}

// TokenCreateResponse is the server response for a created token (E-17,
// auth-model.md section 3.1). The real endpoint answers the OAuth-style body
// (security.go tokenCreateResponse): access_token, token_type "Bearer",
// expires_in (OMITTED when the token never expires), scope, and BinFlow's
// superset token_id as a JSON NUMBER (int64). The token value is only
// returned at creation time and cannot be retrieved later.
//
// Go-side spellings (see UnmarshalJSON for the wire mapping):
//   - Token carries the wire's access_token value.
//   - TokenID carries the wire's int64 rendered as decimal text — the form
//     the revoke endpoint's token_id parameter consumes.
//   - Username is NOT part of the real response and stays empty against the
//     real server (it is only populated by the legacy decode arm below).
type TokenCreateResponse struct {
	Token     string `json:"-"`
	TokenID   string `json:"-"`
	TokenType string `json:"token_type,omitempty"`
	ExpiresIn int64  `json:"expires_in,omitempty"`
	Scope     string `json:"scope,omitempty"`
	Username  string `json:"-"`
}

// UnmarshalJSON decodes the real endpoint's response body (access_token +
// int64 token_id). The pre-alignment spelling ({"token": ..., string
// token_id, username}) is tolerated as a decode-only fallback so stale
// peers keep working; retire that arm when cmd/bf's fake backend is
// refreshed to the real shapes.
func (r *TokenCreateResponse) UnmarshalJSON(data []byte) error {
	var wire struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
		TokenID     int64  `json:"token_id"`
	}
	errReal := json.Unmarshal(data, &wire)
	if errReal == nil {
		r.Token = wire.AccessToken
		r.TokenID = strconv.FormatInt(wire.TokenID, 10)
		r.TokenType = wire.TokenType
		r.ExpiresIn = wire.ExpiresIn
		r.Scope = wire.Scope
		return nil
	}
	var legacy struct {
		Token     string `json:"token"`
		TokenID   string `json:"token_id"`
		Username  string `json:"username"`
		Scope     string `json:"scope"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		// The real-shape error is the primary contract; surface it.
		return fmt.Errorf("client: decode token response: %w", errReal)
	}
	r.Token = legacy.Token
	r.TokenID = legacy.TokenID
	r.Username = legacy.Username
	r.Scope = legacy.Scope
	r.ExpiresIn = legacy.ExpiresIn
	return nil
}

// TokenRevokeRequest is the body for revoking a token (POST /api/security/token/revoke, E-18).
// Exactly one of TokenID or Token must be set; both are transmitted as form
// parameters (the endpoint parses form encoding, not JSON).
type TokenRevokeRequest struct {
	TokenID string `json:"token_id,omitempty"`
	Token   string `json:"token,omitempty"`
}

// CreateToken creates a new API token (POST /binflow/api/security/token).
// The token is created for the given username (the authenticated caller
// itself, or any subject when the caller is admin). The server returns the
// token value once — it cannot be retrieved later (auth-model.md section 3.3).
func (c *Client) CreateToken(ctx context.Context, req TokenCreateRequest) (*TokenCreateResponse, error) {
	var out TokenCreateResponse
	if err := c.postJSON(ctx, "/binflow/api/security/token", req, &out); err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	return &out, nil
}

// CreateTokenForm creates a token using form-encoded body (compatible with
// Artifactory's POST /api/security/token endpoint, E-17). The server
// accepts both JSON and form-encoded bodies.
func (c *Client) CreateTokenForm(ctx context.Context, req TokenCreateRequest) (*TokenCreateResponse, error) {
	form := url.Values{}
	form.Set("username", req.Username)
	if req.Scope != "" {
		form.Set("scope", req.Scope)
	}
	if req.ExpiresIn > 0 {
		form.Set("expires_in", fmt.Sprintf("%d", req.ExpiresIn))
	}
	if req.Description != "" {
		form.Set("description", req.Description)
	}

	path := c.absURL("/binflow/api/security/token")
	body := strings.NewReader(form.Encode())
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("create token (form): %w", err)
	}
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var out TokenCreateResponse
	if err := c.do(httpReq, &out); err != nil {
		return nil, fmt.Errorf("create token (form): %w", err)
	}
	return &out, nil
}

// RevokeToken revokes an API token by its ID or value (POST /binflow/api/security/token/revoke).
// The endpoint speaks application/x-www-form-urlencoded — token and token_id
// are FORM parameters, not JSON fields (security.go handleTokenRevoke parses
// the form) — and answers 200 plain text: "Token revoked" on success, still
// 200 "Token not found" for an unknown or already-revoked token (idempotent,
// E-18, auth-model.md section 3.4).
func (c *Client) RevokeToken(ctx context.Context, req TokenRevokeRequest) error {
	form := url.Values{}
	if req.TokenID != "" {
		form.Set("token_id", req.TokenID)
	}
	if req.Token != "" {
		form.Set("token", req.Token)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.absURL("/binflow/api/security/token/revoke"), strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("revoke token: build request: %w", err)
	}
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := c.do(httpReq, nil); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}
