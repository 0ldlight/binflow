package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/client"
)

// SourceConfig configures the Artifactory source reader.
type SourceConfig struct {
	// BaseURL is the Artifactory base URL INCLUDING the context path when
	// one is deployed (e.g. "http://host:8081/artifactory"). Endpoints are
	// appended as "<base>/api/...".
	BaseURL string

	// APIKey is sent as the X-JFrog-Art-Api header when non-empty.
	APIKey string

	// Token is sent as "Authorization: Bearer <token>" when non-empty and
	// takes precedence over APIKey.
	Token string

	// HTTP is the http.Client used for source reads. Nil selects
	// http.DefaultClient.
	HTTP *http.Client
}

// Reader reads migration source data from an Artifactory instance over its
// REST API. Shapes follow docs/reverse/rest-api.md section 2 (repositories)
// and docs/reverse/auth-model.md sections 1 and 3 (users, tokens).
type Reader struct {
	base   string
	apiKey string
	token  string
	hc     *http.Client
}

// NewSourceReader validates the configuration and returns a Reader.
func NewSourceReader(cfg SourceConfig) (*Reader, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("migrate: source base URL is empty")
	}
	u, err := url.Parse(base)
	// A scheme-less string like "host:8081/artifactory" parses as a
	// bogus scheme with an empty host — reject it too (the port would be
	// silently swallowed into the scheme).
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, fmt.Errorf("migrate: source base URL %q must be absolute (include the context path, e.g. http://host:8081/artifactory)", cfg.BaseURL)
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Reader{base: base, apiKey: cfg.APIKey, token: cfg.Token, hc: hc}, nil
}

// SourceRepoListItem is one GET /api/repositories entry (rest-api.md 2:
// key/description/type/url/packageType; type is lowercase).
type SourceRepoListItem struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	PackageType string `json:"packageType"`
}

// SourceRepoConfig is the GET /api/repositories/{repoKey} configuration
// body. Only the fields BinFlow can consume are decoded; the rest of the
// Artifactory field set (layout refs, snapshot policies, property sets...)
// has no BinFlow counterpart and is deliberately dropped.
type SourceRepoConfig struct {
	Key             string   `json:"key"`
	Rclass          string   `json:"rclass"`
	PackageType     string   `json:"packageType"`
	Description     string   `json:"description"`
	URL             string   `json:"url"`      // remote upstream URL
	Username        string   `json:"username"` // remote upstream username
	IncludesPattern string   `json:"includesPattern"`
	ExcludesPattern string   `json:"excludesPattern"`
	Repositories    []string `json:"repositories"` // virtual member keys
}

// SourceUser is one GET /api/security/users entry (auth-model.md 1.2).
type SourceUser struct {
	Name  string `json:"name"`
	URI   string `json:"uri"`
	Realm string `json:"realm"`
}

// SourceUserDetail is the GET /api/security/users/{userName} body
// (auth-model.md 1.2). Passwords are never part of any response.
type SourceUserDetail struct {
	Name   string   `json:"name"`
	Email  string   `json:"email"`
	Admin  bool     `json:"admin"`
	Groups []string `json:"groups"`
	Realm  string   `json:"realm"`
}

// SourceToken is one GET /api/security/token entry (auth-model.md 3.3,
// admin only). METADATA ONLY: the listing never carries token values —
// they are returned exactly once at creation time — which is why the
// token phase can only account and skip.
type SourceToken struct {
	TokenID     string `json:"token_id"`
	Subject     string `json:"subject"`
	IssuedAt    int64  `json:"issued_at"`
	Expiry      *int64 `json:"expiry"`
	Refreshable bool   `json:"refreshable"`
}

// ListRepositories returns the repository list in server order. The real
// server sorts by type then key (federated < local < remote < virtual), so
// virtual repositories — whose members are local/remote — come last; the
// pipeline RELIES on this order and preserves it.
func (r *Reader) ListRepositories(ctx context.Context) ([]SourceRepoListItem, error) {
	var out []SourceRepoListItem
	if err := r.get(ctx, "/api/repositories", &out); err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	return out, nil
}

// RepositoryConfig fetches the full configuration of one repository.
func (r *Reader) RepositoryConfig(ctx context.Context, key string) (*SourceRepoConfig, error) {
	var out SourceRepoConfig
	if err := r.get(ctx, "/api/repositories/"+url.PathEscape(key), &out); err != nil {
		return nil, fmt.Errorf("repository %q config: %w", key, err)
	}
	return &out, nil
}

// ListUsers returns the user list in server order.
func (r *Reader) ListUsers(ctx context.Context) ([]SourceUser, error) {
	var out []SourceUser
	if err := r.get(ctx, "/api/security/users", &out); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return out, nil
}

// UserDetail fetches the full record of one user.
func (r *Reader) UserDetail(ctx context.Context, name string) (*SourceUserDetail, error) {
	var out SourceUserDetail
	if err := r.get(ctx, "/api/security/users/"+url.PathEscape(name), &out); err != nil {
		return nil, fmt.Errorf("user %q detail: %w", name, err)
	}
	return &out, nil
}

// ListTokens returns the token metadata listing (admin only). A non-admin
// or otherwise refused call surfaces as an *APIError the caller can
// classify with IsStatus.
func (r *Reader) ListTokens(ctx context.Context) ([]SourceToken, error) {
	var out struct {
		Tokens []SourceToken `json:"tokens"`
	}
	if err := r.get(ctx, "/api/security/token", &out); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return out.Tokens, nil
}

// SourceFile is one entry of the deep ?list file listing (rest-api.md
// section 3, high confidence: {"uri","size","lastModified","folder","sha1",
// "sha2"}). The wire name of the SHA-256 digest is "sha2" (the same spelling
// BinFlow's own list face answers); "uri" is relative to the queried
// directory and may carry a leading slash depending on the source version.
type SourceFile struct {
	Path         string `json:"uri"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
	Folder       bool   `json:"folder"`
	Sha1         string `json:"sha1"`
	Sha256       string `json:"sha2"`
}

// sourceFileListBody is the ?list response envelope.
type sourceFileListBody struct {
	URI     string       `json:"uri"`
	Created string       `json:"created"`
	Files   []SourceFile `json:"files"`
}

// folderInfoChildren is the children array of a FolderInfo read
// (rest-api.md section 3: [{uri:"/<name>", folder:bool}], name-sorted).
type folderInfoChildren struct {
	Children []struct {
		URI    string `json:"uri"`
		Folder bool   `json:"folder"`
	} `json:"children"`
}

// ListRepoFiles returns every FILE of one repository (folders are filtered;
// their content is included by the deep listing).
//
// Primary shape: GET /api/storage/{repo}?list&deep=1&listFolders=0 — the
// documented whole-repo listing (PRD FR-63 flow step 5 names it verbatim).
// A 404 means an empty/never-written repository and yields no files. A 400
// covers the spec row "Cannot list files of root." read STRICTLY (repo-root
// listings refused — the reading BinFlow's own list face implements): the
// reader then falls back to walking the FolderInfo children tree, which the
// same spec section documents at high confidence. The two paths together
// make the tool robust under either reading of the spec.
//
// Walker-collected files carry no digests (FolderInfo children have none);
// the artifact copy computes SHA-256 itself and verifies whenever the
// listing did provide one.
func (r *Reader) ListRepoFiles(ctx context.Context, repo string) ([]SourceFile, error) {
	var body sourceFileListBody
	err := r.get(ctx, "/api/storage/"+url.PathEscape(repo)+"?list&deep=1&listFolders=0", &body)
	switch {
	case err == nil:
		return filterSourceFiles(body.Files), nil
	case IsStatus(err, http.StatusNotFound):
		return nil, nil // empty repository: nothing to migrate
	case IsStatus(err, http.StatusBadRequest):
		return r.walkRepoFiles(ctx, repo)
	default:
		return nil, fmt.Errorf("list files of %q: %w", repo, err)
	}
}

// walkRepoFiles enumerates a repository's files through FolderInfo reads
// (breadth-first over the children arrays) when the flat deep listing is
// refused at the repo root.
func (r *Reader) walkRepoFiles(ctx context.Context, repo string) ([]SourceFile, error) {
	var out []SourceFile
	folders := []string{""} // relative folder paths, "" = repo root
	for len(folders) > 0 {
		folder := folders[0]
		folders = folders[1:]

		path := "/api/storage/" + url.PathEscape(repo)
		if folder != "" {
			path += "/" + client.EscapePathSegments(folder)
		}
		var body folderInfoChildren
		if err := r.get(ctx, path, &body); err != nil {
			if folder == "" && IsStatus(err, http.StatusNotFound) {
				return out, nil // empty repository
			}
			return nil, fmt.Errorf("walk %q at %q: %w", repo, folder, err)
		}
		for _, child := range body.Children {
			name := strings.TrimPrefix(child.URI, "/")
			if name == "" {
				continue
			}
			rel := name
			if folder != "" {
				rel = folder + "/" + name
			}
			if child.Folder {
				folders = append(folders, rel)
			} else {
				out = append(out, SourceFile{Path: rel})
			}
		}
	}
	return out, nil
}

// filterSourceFiles normalizes listing entries into plain relative file
// paths, dropping folders and leading slashes.
func filterSourceFiles(files []SourceFile) []SourceFile {
	out := make([]SourceFile, 0, len(files))
	for _, f := range files {
		if f.Folder {
			continue
		}
		f.Path = strings.TrimPrefix(f.Path, "/")
		if f.Path == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// OpenFile opens one artifact of the source for reading (download face,
// rest-api.md section 1: GET /{repoKey}/{path} — no /api prefix). The caller
// must close the returned reader.
func (r *Reader) OpenFile(ctx context.Context, repo, path string) (io.ReadCloser, error) {
	u := r.base + "/" + url.PathEscape(repo) + "/" + client.EscapePathSegments(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build GET %s/%s: %w", repo, path, err)
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	} else if r.apiKey != "" {
		req.Header.Set("X-JFrog-Art-Api", r.apiKey)
	}

	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s/%s: %w", repo, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.Body, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return nil, &APIError{Op: "GET " + repo + "/" + path, StatusCode: resp.StatusCode, Body: string(body)}
}

// escapePathSegments is gone (T-233, T-231 legacy item 1): the reader now
// calls client.EscapePathSegments, the single wire-side escaping contract —
// one implementation instead of two byte-identical private copies, so the
// listing/download spelling and the writer's PUT spelling cannot drift.

// get performs one authenticated GET and decodes the JSON body into out.
func (r *Reader) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base+path, nil)
	if err != nil {
		return fmt.Errorf("build GET %s: %w", path, err)
	}
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	} else if r.apiKey != "" {
		req.Header.Set("X-JFrog-Art-Api", r.apiKey)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.hc.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("GET %s: read body: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Op: "GET " + path, StatusCode: resp.StatusCode, Body: string(body)}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("GET %s: decode body: %w", path, err)
	}
	return nil
}

// APIError is a non-200 response from the source (or a misrouted target)
// API. Artifactory error bodies are plain text (auth-model.md 0), so only
// the raw body is carried, truncated for readability.
type APIError struct {
	Op         string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("migrate: %s: HTTP %d: %s", e.Op, e.StatusCode, truncateBody(e.Body))
}

// truncateBody keeps error output readable when a server answers with an
// HTML error page.
func truncateBody(body string) string {
	const limit = 200
	if len(body) <= limit {
		return body
	}
	return body[:limit] + "..."
}

// IsStatus reports whether err is (or wraps) an *APIError with one of the
// given status codes.
func IsStatus(err error, codes ...int) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.StatusCode == code {
			return true
		}
	}
	return false
}
