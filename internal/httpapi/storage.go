package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Artifactory-compatible item info and file listing (rest-api.md section 3;
// PRD E-09/E-10). GET /api/storage/{repo}/{path} answers FileInfo for files
// and FolderInfo for directories; the ?list family returns a flat file
// listing. Everything else under ?properties/?stats/?lastModified/
// ?permissions is deliberately unimplemented and falls to the E-26 404.
//
// Read authorization follows the content plane (the endpoint exposes exactly
// what a content GET exposes, metadata flavor): anonymous reads pass when
// anonymous_access is on; ?list is stricter — authenticated users only
// (rest-api.md section 3, high confidence).

// fileInfoBody is the FileInfo/FolderInfo wire shape (rest-api.md section 3,
// high-confidence field set): timestamps are ISO8601 with milliseconds and
// zone, size is a STRING, children appear on folders only.
type fileInfoBody struct {
	URI               string          `json:"uri"`
	DownloadURI       string          `json:"downloadUri"`
	Repo              string          `json:"repo"`
	Path              string          `json:"path"`
	Created           string          `json:"created"`
	CreatedBy         string          `json:"createdBy"`
	LastModified      string          `json:"lastModified,omitempty"`
	ModifiedBy        string          `json:"modifiedBy,omitempty"`
	LastUpdated       string          `json:"lastUpdated,omitempty"`
	Size              string          `json:"size"`
	MimeType          string          `json:"mimeType,omitempty"`
	Checksums         *checksumTriple `json:"checksums,omitempty"`
	OriginalChecksums *checksumTriple `json:"originalChecksums,omitempty"`
	Children          []folderChild   `json:"children,omitempty"`
}

// checksumTriple is the sha1/md5/sha256 digest object (fields omitted when
// unknown; folder nodes carry no checksums at all).
type checksumTriple struct {
	Sha1   string `json:"sha1,omitempty"`
	Md5    string `json:"md5,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
}

// folderChild is one FolderInfo children entry: uri "/<name>", folder flag.
type folderChild struct {
	URI    string `json:"uri"`
	Folder bool   `json:"folder"`
}

// isFolderPath reports whether a node path is the folder spelling (trailing
// slash — the repo layer's convention).
func isFolderPath(path string) bool { return strings.HasSuffix(path, "/") }

// isoMillisUTC renders an RFC3339 timestamp as ISO8601 with milliseconds and
// zone offset (rest-api.md section 0). Stored values are UTC so the zone arm
// prints +00:00; empty/unparseable input renders the zero time so clients'
// strict codecs always see a timestamp-shaped string.
func isoMillisUTC(stored string) string {
	t := time.Time{}
	if stored != "" {
		if parsed, err := time.Parse(time.RFC3339, stored); err == nil {
			t = parsed
		}
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// handleStorageItem serves GET /api/storage/{repo}/{path} (E-09). A file
// answers the full FileInfo field set; a directory answers FolderInfo with
// its direct children sorted by name (rest-api.md section 3). relPath is the
// decoded repo-relative path ("" = the repository root).
func (s *Server) handleStorageItem(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	// Unimplemented query arms (E-09 scope): properties/stats/lastModified/
	// permissions answer the E-26 404 rather than silently returning the
	// plain item body.
	for _, q := range []string{"properties", "propertiesXml", "stats", "lastModified", "permissions"} {
		if _, ok := r.URL.Query()[q]; ok {
			notImplemented(w, "/binflow/api/storage item query '"+q+"'")
			return
		}
	}

	p := principalFrom(r.Context())
	if relPath == "" {
		s.serveRootFolder(w, r, p, repoKey)
		return
	}

	node, err := s.storageNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	if isFolderPath(node.Path) {
		s.writeFolderInfo(w, r, repoKey, node)
		return
	}
	s.writeFileInfo(w, r, repoKey, node)
}

// storageNode resolves one node row through the content-plane service call
// so the read-ACL and the anonymous policy apply exactly as on a download.
// Get addresses a folder row with ErrIsFolder AND the row itself.
//
// Folder spelling: storage rows keep the trailing slash ("acme/"), while a
// client addressing the directory through /api/storage usually omits it
// ("acme"). The exact spelling is tried first, the slash-append form second
// — the reverse order would make an explicit trailing-slash request
// indistinguishable from a sloppy one, and the 404 wording must key on what
// the client actually sent.
func (s *Server) storageNode(r *http.Request, p *auth.Principal, repoKey, relPath string) (*metadata.Node, error) {
	_, node, err := s.deps.ReposSvc.Get(r.Context(), p, repoKey, relPath)
	if errors.Is(err, repo.ErrIsFolder) && node != nil {
		return node, nil
	}
	if errors.Is(err, repo.ErrNodeNotFound) && relPath != "" && !isFolderPath(relPath) {
		_, node, err = s.deps.ReposSvc.Get(r.Context(), p, repoKey, relPath+"/")
		if errors.Is(err, repo.ErrIsFolder) && node != nil {
			return node, nil
		}
	}
	return node, err
}

// serveRootFolder renders FolderInfo for the repository root: children are
// the direct first segments under "" (the root itself has no node row).
func (s *Server) serveRootFolder(w http.ResponseWriter, r *http.Request, p *auth.Principal, repoKey string) {
	nodes, err := s.deps.ReposSvc.List(r.Context(), p, repoKey, "")
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	s.writeFolderInfoBody(w, r, repoKey, "/", nil, childInfos(nodes, ""))
}

// fileInfoOf builds the FileInfo wire shape (E-09 field set) of one file
// node: digests come from the node plus the blob ledger (sha256 on the node,
// sha1/md5 keyed by the blob, ADR-0006). Extracted from writeFileInfo so the
// search planes (T-92) render the exact same field set without duplication.
func (s *Server) fileInfoOf(ctx context.Context, base, repoKey string, node *metadata.Node) fileInfoBody {
	sums := s.digestTripleOf(ctx, node)
	modified := node.UpdatedAt
	if modified == "" {
		modified = node.CreatedAt
	}
	return fileInfoBody{
		URI:          storageURI(base, repoKey, node.Path),
		DownloadURI:  storageURI(base, repoKey, node.Path),
		Repo:         repoKey,
		Path:         "/" + node.Path,
		Created:      isoMillisUTC(node.CreatedAt),
		CreatedBy:    node.CreatedBy,
		LastModified: isoMillisUTC(modified),
		ModifiedBy:   node.CreatedBy,
		LastUpdated:  isoMillisUTC(modified),
		Size:         strconv.FormatInt(node.Size, 10),
		MimeType:     mimeOrDefault(node.Mime),
		Checksums:    sums,
		// /api/storage is not an upload context: the stored triple is the
		// best echo available for originalChecksums (the same rule the
		// generic adapter applies on download-side renders).
		OriginalChecksums: sums,
	}
}

// writeFileInfo renders the file body: the full field set with digests from
// the node plus the blob ledger (sha256 on the node, sha1/md5 keyed by the
// blob, ADR-0006).
func (s *Server) writeFileInfo(w http.ResponseWriter, r *http.Request, repoKey string, node *metadata.Node) {
	writeJSONBody(w, http.StatusOK, s.fileInfoOf(r.Context(), requestBase(r), repoKey, node))
}

// mimeOrDefault defaults an absent stored mime (FR-4-AC13 posture).
func mimeOrDefault(m string) string {
	if strings.TrimSpace(m) == "" {
		return "application/octet-stream"
	}
	return m
}

// writeFolderInfo renders FolderInfo for an explicit folder node row:
// children are the direct entries under the directory.
func (s *Server) writeFolderInfo(w http.ResponseWriter, r *http.Request, repoKey string, node *metadata.Node) {
	dir := strings.TrimSuffix(node.Path, "/")
	nodes, err := s.deps.ReposSvc.List(r.Context(), principalFrom(r.Context()), repoKey, dir)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	s.writeFolderInfoBody(w, r, repoKey, "/"+node.Path, node, childInfos(nodes, dir))
}

// writeFolderInfoBody emits the FolderInfo shape. node is nil for the
// repository root (no row exists there); timestamps degrade to zero time.
func (s *Server) writeFolderInfoBody(w http.ResponseWriter, r *http.Request, repoKey, displayPath string, node *metadata.Node, children []folderChild) {
	stamp := ""
	created := ""
	createdBy := ""
	if node != nil {
		created, createdBy = node.CreatedAt, node.CreatedBy
		stamp = node.UpdatedAt
		if stamp == "" {
			stamp = node.CreatedAt
		}
	}
	body := fileInfoBody{
		URI:       storageURI(requestBase(r), repoKey, strings.TrimPrefix(displayPath, "/")),
		Repo:      repoKey,
		Path:      displayPath,
		Created:   isoMillisUTC(created),
		CreatedBy: createdBy,
		Size:      "0",
		Children:  children,
	}
	if stamp != "" {
		body.LastModified = isoMillisUTC(stamp)
		body.ModifiedBy = createdBy
		body.LastUpdated = isoMillisUTC(stamp)
	}
	writeJSONBody(w, http.StatusOK, body)
}

// childInfos projects every node under dir (dir "" = repository root) onto
// the direct-children listing: one entry per first path segment, folder flag
// from the trailing-slash spelling, sorted by name (rest-api.md section 3).
func childInfos(nodes []*metadata.Node, dir string) []folderChild {
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	seen := make(map[string]bool, len(nodes))
	var out []folderChild
	for _, n := range nodes {
		rel := strings.TrimPrefix(n.Path, prefix)
		if rel == "" {
			continue // the folder row itself
		}
		name, isFolder := firstSegment(rel)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, folderChild{URI: "/" + name, Folder: isFolder})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].URI < out[j].URI })
	return out
}

// firstSegment returns the first path segment of rel and whether it names a
// directory: a deeper tail ("a/b") or the folder spelling ("a/") both mark
// the head as a folder; a bare "a" is a file.
func firstSegment(rel string) (head string, isFolder bool) {
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i], true
	}
	return rel, false
}

// storageURI builds the uri/downloadUri fields: <base>/<repo>/<path>. A
// trailing slash on relPath is preserved (folder addressing).
func storageURI(base, repoKey, relPath string) string {
	return base + "/" + repoKey + "/" + relPath
}

// digestTripleOf resolves the node's three digests (sha256 from the node,
// sha1/md5 from the blob ledger row; a ledger miss degrades to sha256-only).
func (s *Server) digestTripleOf(ctx context.Context, node *metadata.Node) *checksumTriple {
	t := &checksumTriple{Sha256: node.Sha256}
	if node.Sha256 == "" {
		return t
	}
	b, err := s.deps.Metadata.Blobs().Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.Sha1, t.Md5 = b.Sha1, b.Md5
	return t
}

// ---- ?list (E-10, P2) ----

// listFile is one files[] entry of the ?list response: uri is RELATIVE to
// the queried directory (rest-api.md section 3).
type listFile struct {
	URI          string `json:"uri"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
	Folder       bool   `json:"folder"`
	SHA1         string `json:"sha1,omitempty"`
	SHA2         string `json:"sha2,omitempty"`
}

// listResponse is the ?list body.
type listResponse struct {
	URI     string     `json:"uri"`
	Created string     `json:"created"`
	Files   []listFile `json:"files"`
}

// handleStorageList serves GET /api/storage/{repo}/{path}?list (E-10). The
// spec's rejection ladder (rest-api.md section 3, high confidence):
// anonymous -> 403; the repository root -> 400 "Cannot list files of root.";
// a file target -> 400. deep=1 asks for the full recursion; depth=N bounds
// it (M1 subset: list/deep/depth, FR-3-AC8).
func (s *Server) handleStorageList(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	if p == nil {
		writeError(w, http.StatusForbidden, "listing repository files requires an authenticated user")
		return
	}
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "Cannot list files of root.")
		return
	}

	node, err := s.storageNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	if !isFolderPath(node.Path) {
		writeError(w, http.StatusBadRequest,
			"Cannot list files of a file '"+repoKey+"/"+node.Path+"'.")
		return
	}

	dir := strings.TrimSuffix(node.Path, "/")
	nodes, err := s.deps.ReposSvc.List(r.Context(), p, repoKey, dir)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}

	// depth bounds the recursion below the queried directory; deep=1 means
	// unlimited (depth 0).
	depth := 1
	if strings.TrimSpace(r.URL.Query().Get("deep")) == "1" {
		depth = 0
	} else if dv := r.URL.Query().Get("depth"); dv != "" {
		if n, perr := strconv.Atoi(dv); perr == nil && n > 0 {
			depth = n
		}
	}

	prefix := dir + "/"
	resp := listResponse{
		URI:     storageURI(requestBase(r), repoKey, "api/storage/"+node.Path),
		Created: isoMillisUTC(node.CreatedAt),
		Files:   []listFile{},
	}
	for _, n := range nodes {
		rel := strings.TrimPrefix(n.Path, prefix)
		if rel == "" {
			continue // the queried folder row itself
		}
		levels := strings.Count(rel, "/")
		folder := isFolderPath(n.Path)
		if folder && levels == 0 {
			continue // the folder row of a direct child: its content lists it
		}
		if depth > 0 && levels+1 > depth {
			continue
		}
		entry := listFile{
			URI:          rel,
			Size:         n.Size,
			LastModified: isoMillisUTC(n.UpdatedAt),
			Folder:       folder,
		}
		if !folder && n.Sha256 != "" {
			entry.SHA2 = n.Sha256
			if b, berr := s.deps.Metadata.Blobs().Get(r.Context(), n.Sha256); berr == nil && b != nil {
				entry.SHA1 = b.Sha1
			}
		}
		resp.Files = append(resp.Files, entry)
	}
	writeJSONBody(w, http.StatusOK, resp)
}

// writeStorageError maps repo.Service failures of the storage plane.
func (s *Server) writeStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, "Unable to find repository: "+err.Error())
	case errors.Is(err, repo.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, "Unable to find item: "+err.Error())
	case errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.Error("httpapi: storage service failure", "error", err.Error())
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("storage operation failed: %v", err))
	}
}
