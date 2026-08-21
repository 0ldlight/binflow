package client

import (
	"context"
	"fmt"
)

// RepoCreateRequest is the body for creating or updating a repository.
// Fields mirror the /api/repositories/{key} PUT body (E-04/E-07).
type RepoCreateRequest struct {
	Key         string   `json:"key"`
	Rclass      string   `json:"rclass"`                // "local", "remote", or "virtual"
	PackageType string   `json:"packageType"`           // "generic", "docker", "maven", "npm", "pypi"
	Description string   `json:"description,omitempty"` // optional human-readable description
	URL         string   `json:"url,omitempty"`         // remote URL (for remote repos)
	Username    string   `json:"username,omitempty"`    // remote auth username
	Password    string   `json:"password,omitempty"`    // remote auth password
	Members     []string `json:"members,omitempty"`     // member repo keys (for virtual repos)
	// QuotaBytes is the maximum storage quota in bytes (0 = unlimited).
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
	// Includes is the include pattern (default "**/*").
	Includes string `json:"includes,omitempty"`
	// Excludes is the exclude pattern.
	Excludes string `json:"excludes,omitempty"`
}

// RepoInfo is the server response for a single repository (E-04/E-07 response).
type RepoInfo struct {
	Key         string   `json:"key"`
	Rclass      string   `json:"rclass"`
	PackageType string   `json:"packageType"`
	Description string   `json:"description,omitempty"`
	URL         string   `json:"url,omitempty"`
	Members     []string `json:"members,omitempty"`
	QuotaBytes  int64    `json:"quotaBytes,omitempty"`
	Includes    string   `json:"includes,omitempty"`
	Excludes    string   `json:"excludes,omitempty"`
}

// CreateRepo creates a new repository (PUT /binflow/api/repositories/{key}).
// It returns the created repository info on success.
func (c *Client) CreateRepo(ctx context.Context, req RepoCreateRequest) (*RepoInfo, error) {
	path := fmt.Sprintf("/binflow/api/repositories/%s", pathEscape(req.Key))
	var out RepoInfo
	if err := c.putJSON(ctx, path, req, &out); err != nil {
		return nil, fmt.Errorf("create repo %q: %w", req.Key, err)
	}
	return &out, nil
}

// UpdateRepo updates an existing repository (POST /binflow/api/repositories/{key}).
// It returns the updated repository info on success.
func (c *Client) UpdateRepo(ctx context.Context, key string, req RepoCreateRequest) (*RepoInfo, error) {
	path := fmt.Sprintf("/binflow/api/repositories/%s", pathEscape(key))
	var out RepoInfo
	if err := c.postJSON(ctx, path, req, &out); err != nil {
		return nil, fmt.Errorf("update repo %q: %w", key, err)
	}
	return &out, nil
}

// GetRepo retrieves a single repository by key (GET /binflow/api/repositories/{key}).
func (c *Client) GetRepo(ctx context.Context, key string) (*RepoInfo, error) {
	path := fmt.Sprintf("/binflow/api/repositories/%s", pathEscape(key))
	var out RepoInfo
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("get repo %q: %w", key, err)
	}
	return &out, nil
}

// ListRepos lists all repositories (GET /binflow/api/repositories).
func (c *Client) ListRepos(ctx context.Context) ([]RepoInfo, error) {
	var out []RepoInfo
	if err := c.getJSON(ctx, "/binflow/api/repositories", &out); err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return out, nil
}

// DeleteRepo deletes a repository by key (DELETE /binflow/api/repositories/{key}).
func (c *Client) DeleteRepo(ctx context.Context, key string) error {
	path := fmt.Sprintf("/binflow/api/repositories/%s", pathEscape(key))
	if err := c.deleteJSON(ctx, path, nil); err != nil {
		return fmt.Errorf("delete repo %q: %w", key, err)
	}
	return nil
}
