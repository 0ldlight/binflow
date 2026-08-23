package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ArtifactInfo describes a single artifact node (file or folder) in a
// repository. Mirrors the server's /api/storage/{repo}/{path} item-info
// response (E-09): size is a STRING and the digests ride the NESTED
// checksums object (storage.go fileInfoBody) — UnmarshalJSON folds them
// into the flat Sha1/MD5/Sha256 fields.
type ArtifactInfo struct {
	URI          string `json:"uri"`
	Size         string `json:"size"`
	LastModified string `json:"lastModified"`
	Folder       bool   `json:"folder"`
	Sha1         string `json:"sha1,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
	MD5          string `json:"md5,omitempty"`
	MimeType     string `json:"mimeType,omitempty"`
}

// artifactChecksums is the nested checksums object of the real item-info
// body (storage.go checksumTriple: sha1/md5/sha256).
type artifactChecksums struct {
	Sha1   string `json:"sha1"`
	Md5    string `json:"md5"`
	Sha256 string `json:"sha256"`
}

// UnmarshalJSON folds the real body's nested checksums object into the flat
// digest fields. A flat spelling is still accepted (pre-alignment peers);
// the nested values only fill fields the flat spelling left empty.
func (a *ArtifactInfo) UnmarshalJSON(data []byte) error {
	type artifactInfoAlias ArtifactInfo // avoids recursing into UnmarshalJSON
	var v struct {
		artifactInfoAlias
		Checksums *artifactChecksums `json:"checksums"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*a = ArtifactInfo(v.artifactInfoAlias)
	if v.Checksums != nil {
		if a.Sha1 == "" {
			a.Sha1 = v.Checksums.Sha1
		}
		if a.MD5 == "" {
			a.MD5 = v.Checksums.Md5
		}
		if a.Sha256 == "" {
			a.Sha256 = v.Checksums.Sha256
		}
	}
	return nil
}

// ArtifactListEntry is one entry in the ?list response (E-10). Size is an
// int64 NUMBER on the wire (storage.go listFile) — unlike the item-info
// body, whose size is a string.
type ArtifactListEntry struct {
	URI    string `json:"uri"`
	Folder bool   `json:"folder"`
	Size   int64  `json:"size,omitempty"`
}

// ArtifactListResponse is the server response for a storage listing.
type ArtifactListResponse struct {
	URI     string              `json:"uri"`
	Created string              `json:"created"`
	Files   []ArtifactListEntry `json:"files"`
}

// GetArtifactInfo gets metadata for a single artifact (GET /binflow/api/storage/{repo}/{path}).
func (c *Client) GetArtifactInfo(ctx context.Context, repo, nodePath string) (*ArtifactInfo, error) {
	var out ArtifactInfo
	if err := c.getJSON(ctx, storagePlanePath(repo, nodePath), &out); err != nil {
		return nil, fmt.Errorf("get artifact info %s/%s: %w", repo, nodePath, err)
	}
	return &out, nil
}

// ListArtifacts lists artifacts under a repo path (GET /binflow/api/storage/{repo}/{path}?list).
func (c *Client) ListArtifacts(ctx context.Context, repo, nodePath string) (*ArtifactListResponse, error) {
	p := strings.TrimSuffix(storagePlanePath(repo, nodePath), "/") + "?list"
	var out ArtifactListResponse
	if err := c.getJSON(ctx, p, &out); err != nil {
		return nil, fmt.Errorf("list artifacts %s/%s: %w", repo, nodePath, err)
	}
	return &out, nil
}

// UploadArtifact uploads a file to a repository path (PUT /binflow/{repo}/{path}).
// The content is read from the provided reader. Size is the total byte count
// (used for Content-Length and progress reporting). contentType defaults to
// "application/octet-stream" when empty.
func (c *Client) UploadArtifact(ctx context.Context, repo, nodePath string, body io.Reader, size int64, contentType string) error {
	if err := c.uploadFile(ctx, contentPlanePath(repo, nodePath), body, size, contentType); err != nil {
		return fmt.Errorf("upload artifact %s/%s: %w", repo, nodePath, err)
	}
	return nil
}

// DownloadArtifact downloads a file from a repository path (GET /binflow/{repo}/{path}).
// The caller must close the returned ReadCloser.
func (c *Client) DownloadArtifact(ctx context.Context, repo, nodePath string) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, "GET", contentPlanePath(repo, nodePath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("download artifact %s/%s: %w", repo, nodePath, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.Body, nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return nil, parseStatusError(resp.StatusCode, body)
}

// DeleteArtifact deletes a file from a repository path (DELETE /binflow/{repo}/{path}).
func (c *Client) DeleteArtifact(ctx context.Context, repo, nodePath string) error {
	req, err := c.newRequest(ctx, "DELETE", contentPlanePath(repo, nodePath), nil)
	if err != nil {
		return err
	}
	if err := c.do(req, nil); err != nil {
		return fmt.Errorf("delete artifact %s/%s: %w", repo, nodePath, err)
	}
	return nil
}

// contentPlanePath builds the artifact content-plane request path
// /binflow/{repo}/{path} with the repo key and EVERY artifact-path segment
// percent-escaped (EscapePathSegments). nodePath is the literal node path;
// a raw '%', '#', '?' or space in a name must never reach the URL —
// url.Parse rejects "%.t"-class escapes outright and a raw '#'/'?' would be
// read as fragment/query separators (T-231, the T-228 D-1 migration
// failure). An empty nodePath keeps the repository-root spelling
// "/binflow/{repo}/", unchanged from the pre-escaping construction.
func contentPlanePath(repo, nodePath string) string {
	return "/binflow/" + pathEscape(repo) + "/" + EscapePathSegments(trimPrefix(nodePath, "/"))
}

// storagePlanePath builds the item-info/listing request path
// /binflow/api/storage/{repo}/{path} with the same per-segment escaping as
// the content plane, so both planes address the identical node.
func storagePlanePath(repo, nodePath string) string {
	return "/binflow/api/storage/" + pathEscape(repo) + "/" + EscapePathSegments(trimPrefix(nodePath, "/"))
}

// trimPrefix removes a leading "/" from s, if present.
func trimPrefix(s, prefix string) string {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}
