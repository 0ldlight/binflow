package client

import (
	"context"
	"fmt"
)

// UserCreateRequest is the body for creating or updating a user (PUT
// /api/security/users/{name}). Mirrors the server's expected fields (E-19).
type UserCreateRequest struct {
	Name        string   `json:"name"`
	Password    string   `json:"password"`
	Email       string   `json:"email,omitempty"`
	Admin       bool     `json:"admin"`
	ShouldExist bool     `json:"shouldExist,omitempty"` // true = fail if user doesn't exist yet
	Groups      []string `json:"groups,omitempty"`
}

// UserUpdateRequest is the body for updating a user (POST /api/security/users/{name}).
// Fields that are zero-valued or empty are not changed.
type UserUpdateRequest struct {
	Password string   `json:"password,omitempty"`
	Email    string   `json:"email,omitempty"`
	Admin    *bool    `json:"admin,omitempty"` // nil pointer = unchanged
	Groups   []string `json:"groups,omitempty"`
}

// UserInfo describes a single user (E-19 response).
type UserInfo struct {
	Name   string   `json:"name"`
	Email  string   `json:"email,omitempty"`
	Admin  bool     `json:"admin"`
	Groups []string `json:"groups,omitempty"`
	Realm  string   `json:"realm,omitempty"` // "local", "oidc", "ldap"
}

// UserListResponse is the list of users (GET /api/security/users).
type UserListResponse []UserInfo

// ChangePasswordRequest is the body for changing the current user's password
// (PUT /api/security/password).
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// CreateUser creates a new user (PUT /binflow/api/security/users/{name}).
func (c *Client) CreateUser(ctx context.Context, req UserCreateRequest) (*UserInfo, error) {
	path := fmt.Sprintf("/binflow/api/security/users/%s", pathEscape(req.Name))
	var out UserInfo
	if err := c.putJSON(ctx, path, req, &out); err != nil {
		return nil, fmt.Errorf("create user %q: %w", req.Name, err)
	}
	return &out, nil
}

// UpdateUser updates an existing user (POST /binflow/api/security/users/{name}).
func (c *Client) UpdateUser(ctx context.Context, name string, req UserUpdateRequest) (*UserInfo, error) {
	path := fmt.Sprintf("/binflow/api/security/users/%s", pathEscape(name))
	var out UserInfo
	if err := c.postJSON(ctx, path, req, &out); err != nil {
		return nil, fmt.Errorf("update user %q: %w", name, err)
	}
	return &out, nil
}

// GetUser retrieves a single user by name (GET /binflow/api/security/users/{name}).
func (c *Client) GetUser(ctx context.Context, name string) (*UserInfo, error) {
	path := fmt.Sprintf("/binflow/api/security/users/%s", pathEscape(name))
	var out UserInfo
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("get user %q: %w", name, err)
	}
	return &out, nil
}

// ListUsers lists all users (GET /binflow/api/security/users).
func (c *Client) ListUsers(ctx context.Context) (UserListResponse, error) {
	var out UserListResponse
	if err := c.getJSON(ctx, "/binflow/api/security/users", &out); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// ChangeSelfPassword changes the current user's password
// (PUT /binflow/api/security/password).
func (c *Client) ChangeSelfPassword(ctx context.Context, current, newPassword string) error {
	req := ChangePasswordRequest{
		CurrentPassword: current,
		NewPassword:     newPassword,
	}
	if err := c.putJSON(ctx, "/binflow/api/security/password", req, nil); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return nil
}
