package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TokenCreateRequest is the body for creating an API token (POST /api/security/token).
// The server accepts both application/json and application/x-www-form-urlencoded
// (auth-model.md section 3.3).
type TokenCreateRequest struct {
	Username    string `json:"username"`
	Scope       string `json:"scope,omitempty"`       // e.g., "api:*"
	ExpiresIn   int64  `json:"expires_in,omitempty"`  // seconds
	Description string `json:"description,omitempty"` // human-readable label
}

// TokenCreateResponse is the server response for a created token (E-17).
// The token value is only returned at creation time and cannot be retrieved
// later.
type TokenCreateResponse struct {
	TokenID     string `json:"token_id"`
	Token       string `json:"token"`
	Username    string `json:"username"`
	Scope       string `json:"scope,omitempty"`
	ExpiresIn   int64  `json:"expires_in,omitempty"`
	Description string `json:"description,omitempty"`
	IssuedAt    string `json:"issued_at,omitempty"`
}

// TokenRevokeRequest is the body for revoking a token (POST /api/security/token/revoke, E-18).
// Exactly one of TokenID or Token must be set.
type TokenRevokeRequest struct {
	TokenID string `json:"token_id,omitempty"`
	Token   string `json:"token,omitempty"`
}

// CreateToken creates a new API token (POST /binflow/api/security/token).
// The token is created for the given username. The server returns the token
// value once — it cannot be retrieved later (auth-model.md section 3.3).
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
// The server is idempotent — revoking an already-revoked token returns 200,
// not 404 (E-18, auth-model.md section 3.4).
func (c *Client) RevokeToken(ctx context.Context, req TokenRevokeRequest) error {
	if err := c.postJSON(ctx, "/binflow/api/security/token/revoke", req, nil); err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	return nil
}
