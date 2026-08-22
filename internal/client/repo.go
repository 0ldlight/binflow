package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RepoCreateRequest is the body for creating or updating a repository.
// Fields mirror the /api/repositories/{key} PUT body the REAL server decodes
// (E-04/E-07; internal/httpapi repoConfig — the encode face was aligned to it
// in T-191 after T-167 found the wire drift). The Go field names keep the
// historical client vocabulary (Members/Includes/Excludes); the JSON tags are
// the server's spellings, which is the only thing that crosses the wire.
type RepoCreateRequest struct {
	Key         string `json:"key"`
	Rclass      string `json:"rclass"`                // "local", "remote", or "virtual"
	PackageType string `json:"packageType"`           // "generic", "docker", "maven", "npm", "pypi"
	Description string `json:"description,omitempty"` // optional human-readable description
	URL         string `json:"url,omitempty"`         // remote URL (for remote repos)
	Username    string `json:"username,omitempty"`    // remote auth username
	Password    string `json:"password,omitempty"`    // remote auth password
	// Members lists the member repo keys (virtual repositories). The wire key
	// is the server's "repositories" (repoConfig.Repositories); a body spelled
	// "members" is silently unknown to the server and the virtual create then
	// fails for lack of a member list.
	Members []string `json:"repositories,omitempty"`
	// QuotaBytes is the maximum storage quota in bytes (0 = unlimited).
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
	// Includes is the include pattern (default "**/*"); wire key
	// "includesPattern" (repoConfig.IncludesPattern).
	Includes string `json:"includesPattern,omitempty"`
	// Excludes is the exclude pattern; wire key "excludesPattern".
	Excludes string `json:"excludesPattern,omitempty"`
}

// RepoInfo is the server response shape for repository reads (E-04/E-05).
// The tags are the REAL spellings of the single-repo configuration body
// (repositories.go repoConfig). Note the GET body carries the stored
// canonical config nested under "configuration" (repoConfigOf) — the virtual
// members and the local patterns are folded into the flat fields by
// UnmarshalJSON, the same nested-to-flat move ArtifactInfo makes for
// checksums.
type RepoInfo struct {
	Key         string   `json:"key"`
	Rclass      string   `json:"rclass"`
	PackageType string   `json:"packageType"`
	Description string   `json:"description,omitempty"`
	URL         string   `json:"url,omitempty"`
	Members     []string `json:"repositories,omitempty"`
	QuotaBytes  int64    `json:"quotaBytes,omitempty"`
	Includes    string   `json:"includesPattern,omitempty"`
	Excludes    string   `json:"excludesPattern,omitempty"`
}

// UnmarshalJSON accepts both spellings of the repository class field: the
// single-repo configuration body answers "rclass" (repositories.go
// repoConfig) while the list endpoint answers the Artifactory-compatible
// "type" (repositories.go repoListItem). "rclass" wins when both appear.
// It also folds the "configuration" echo of the real GET body — the stored
// canonical config ({"repositories":[...]} for virtual, the verbatim
// includesPattern/excludesPattern map for local) — into the flat fields;
// an explicit top-level spelling (a JSON-answering mutation peer) wins over
// the echo.
func (r *RepoInfo) UnmarshalJSON(data []byte) error {
	type repoInfoAlias RepoInfo // avoids recursing into UnmarshalJSON
	var v struct {
		repoInfoAlias
		Type string `json:"type"`
		// Configuration mirrors the GET body's echo of the stored canonical
		// config (repositories.go repoConfigOf); only the typed subset the
		// flat view exposes is decoded.
		Configuration struct {
			Repositories    []string `json:"repositories"`
			IncludesPattern string   `json:"includesPattern"`
			ExcludesPattern string   `json:"excludesPattern"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*r = RepoInfo(v.repoInfoAlias)
	if r.Rclass == "" {
		r.Rclass = v.Type
	}
	if len(r.Members) == 0 {
		r.Members = v.Configuration.Repositories
	}
	if r.Includes == "" {
		r.Includes = v.Configuration.IncludesPattern
	}
	if r.Excludes == "" {
		r.Excludes = v.Configuration.ExcludesPattern
	}
	return nil
}

// CreateRepo creates a new repository (PUT /binflow/api/repositories/{key}).
// The real endpoint answers 200 with a PLAIN-TEXT confirmation
// ("Successfully created repository '<key>'" — repositories.go
// handleRepoPut); there is no JSON body to decode, so the returned RepoInfo
// echoes the request (see mutateRepo). An existing key turns the call into
// an update, answered with the update wording — both spellings succeed.
func (c *Client) CreateRepo(ctx context.Context, req RepoCreateRequest) (*RepoInfo, error) {
	info, err := c.mutateRepo(ctx, http.MethodPut, req.Key, req)
	if err != nil {
		return nil, fmt.Errorf("create repo %q: %w", req.Key, err)
	}
	return info, nil
}

// UpdateRepo updates an existing repository (POST /binflow/api/repositories/{key}).
// The real endpoint answers 200 plain text ("Repository <key> update
// successfully." — repositories.go handleRepoPost); the returned RepoInfo
// echoes the request. An unknown key answers 404.
func (c *Client) UpdateRepo(ctx context.Context, key string, req RepoCreateRequest) (*RepoInfo, error) {
	info, err := c.mutateRepo(ctx, http.MethodPost, key, req)
	if err != nil {
		return nil, fmt.Errorf("update repo %q: %w", key, err)
	}
	return info, nil
}

// mutateRepo issues the repository create (PUT) or update (POST) spelling
// and parses the response by ENDPOINT semantics, not a fixed JSON contract:
// the real repository plane answers 200 plain text for both verbs
// (repositories.go writeText), so a non-JSON body is the success NORM and
// never a decode error. When the body is JSON (a peer still answering the
// pre-alignment shape) it is decoded over the request echo. Failures keep
// the errors[] envelope path through doRaw.
func (c *Client) mutateRepo(ctx context.Context, method, key string, req RepoCreateRequest) (*RepoInfo, error) {
	b, err := json.Marshal(req) //nolint:gosec // G117: Password is the remote-repo upstream credential the E-07 create body explicitly transports (repo.Service drops it after validation, NFR-S14)
	if err != nil {
		return nil, fmt.Errorf("client: marshal request body: %w", err)
	}
	path := "/binflow/api/repositories/" + pathEscape(key)
	httpReq, err := c.newRequest(ctx, method, path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	body, err := c.doRaw(httpReq)
	if err != nil {
		return nil, err
	}
	// Echo the request: the plain-text success body carries no repo fields.
	out := RepoInfo{
		Key:         key,
		Rclass:      req.Rclass,
		PackageType: req.PackageType,
		Description: req.Description,
		URL:         req.URL,
		Members:     req.Members,
		QuotaBytes:  req.QuotaBytes,
		Includes:    req.Includes,
		Excludes:    req.Excludes,
	}
	if looksLikeJSON(body) {
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("client: decode response: %w", err)
		}
	}
	return &out, nil
}

// GetRepo retrieves a single repository by key (GET /binflow/api/repositories/{key}).
func (c *Client) GetRepo(ctx context.Context, key string) (*RepoInfo, error) {
	path := "/binflow/api/repositories/" + pathEscape(key)
	var out RepoInfo
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, fmt.Errorf("get repo %q: %w", key, err)
	}
	return &out, nil
}

// ListRepos lists all repositories (GET /binflow/api/repositories). The real
// list body carries the Artifactory-compatible "type" field for the class
// (decoded into Rclass, see RepoInfo.UnmarshalJSON).
func (c *Client) ListRepos(ctx context.Context) ([]RepoInfo, error) {
	var out []RepoInfo
	if err := c.getJSON(ctx, "/binflow/api/repositories", &out); err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	return out, nil
}

// DeleteRepo deletes a repository by key (DELETE /binflow/api/repositories/{key}).
// The real endpoint answers 200 plain text ("Repository <key> deleted
// successfully."); a non-empty repository demands ?deleteContent=true,
// which this spelling does not send.
func (c *Client) DeleteRepo(ctx context.Context, key string) error {
	path := "/binflow/api/repositories/" + pathEscape(key)
	if err := c.deleteJSON(ctx, path, nil); err != nil {
		return fmt.Errorf("delete repo %q: %w", key, err)
	}
	return nil
}
