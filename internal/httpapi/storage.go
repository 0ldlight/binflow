package httpapi

import (
	"context"
	"crypto/md5" //nolint:gosec // G401: md5 here is the reference's propertiesMd5 wire digest (a compatibility hash, never addressing or integrity — those are sha256-only, ADR-0003); same ruling as internal/adapter/docker/digest.go
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
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
// listing; ?permissions answers the effective-permission view (SE-08, T-97);
// ?properties answers the property read/write family (FR-89, T-286 —
// properties.go); ?stats answers the per-node download statistics (StatsInfo,
// M16 T-438 / ADR-0044 K69 — the four nodes counting columns' only wire
// face). Everything else under ?propertiesXml/?lastModified is deliberately
// unimplemented and falls to the E-26 404.
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
	// DockerTags is a map from bare hex digest to tag names, populated when the
	// ?docker_tags query parameter is set on a docker repository FolderInfo GET.
	// The field is omitted entirely when empty (no Docker repo or no tags).
	DockerTags map[string][]string `json:"dockerTags,omitempty"`
	// Properties is the node's property set (M10 T-286, FR-89.3: the detail
	// body carries it additively). Omitted when the node has none — the
	// pre-M10 wire form stays byte-identical for property-less nodes (the
	// M9-frozen assertions must keep passing untouched).
	Properties map[string][]string `json:"properties,omitempty"`
	// RemoteDegraded is the remote-browse layer's error-state note on the
	// FolderInfo face (M16 T-461's wire leg over T-448's §5-2 seam,
	// remote-browsing.md §4-1): non-empty only when an ENGAGED layer (a
	// listRemoteFolderItems remote repository, or such a member of a virtual)
	// met an upstream fault or sits inside the assumed-offline silence — the
	// cached children stay and the note says why the remote layer went quiet.
	// Omitted on every healthy, flag-off or local tree, and always empty on
	// FILE bodies (the note is a listing-level fact; the console renders its
	// absence as no annotation at all).
	RemoteDegraded string `json:"remoteDegraded,omitempty"`
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
	// Unimplemented query arms (E-09 scope): propertiesXml/lastModified
	// answer the E-26 404 rather than silently returning the plain item
	// body. (?permissions left this list in T-97 — SE-08 routes it to
	// handleStoragePermissions before this handler runs; ?properties left
	// it in T-286 — FR-89 routes the three verbs to the properties family
	// before this handler runs; ?stats left it in T-438 — K69 routes it to
	// handleStorageStats before this handler runs.)
	for _, q := range []string{"propertiesXml", "lastModified"} {
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

// statsInfoBody is the StatsInfo wire shape of the ?stats arm (rest-api.md
// section 3's high-confidence anchor; ADR-0044 K69 decision 4). The official
// seven-field set also carries remoteLastDownloaded and
// remoteLastDownloadedBy — both are UNSOURCED in BinFlow (no smart-remote
// pull-back statistics exist) and are therefore omitted, never faked
// (architecture 11.49; the add-a-column path is a small migration ticket).
type statsInfoBody struct {
	URI                 string `json:"uri"`
	DownloadCount       int64  `json:"downloadCount"`
	LastDownloaded      string `json:"lastDownloaded,omitempty"`
	LastDownloadedBy    string `json:"lastDownloadedBy,omitempty"`
	RemoteDownloadCount int64  `json:"remoteDownloadCount"`
}

// handleStorageStats serves GET /api/storage/{repo}/{path}?stats (M16 T-438,
// FR-146.2): the per-node download statistics — the four nodes counting
// columns' only wire face. The route keeps the item-info read gate
// (content-plane semantics, anonymous follows the flag): the counts are not
// identity information. lastDownloadedBy is the exception — the identity
// arm rides the audit log's read capability (K69 decision 5): a caller
// holding CapSystemRead (admin or readonly_admin) sees the field, everyone
// else gets it omitted (omitempty), never blanked.
func (s *Server) handleStorageStats(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	node, err := s.statsNode(r, p, repoKey, relPath)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	st, err := s.deps.Metadata.Nodes().Stats(r.Context(), node.RepoKey, node.Path)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	body := statsInfoBody{
		URI:                 storageURI(requestBase(r), repoKey, node.Path),
		DownloadCount:       st.DownloadCount,
		RemoteDownloadCount: st.RemoteDownloadCount,
	}
	if st.LastDownloadedAt != "" {
		body.LastDownloaded = isoMillisUTC(st.LastDownloadedAt)
	}
	if managementAllowed(s.deps.Authz, func(m auth.ManagementAuthorizer) bool {
		return m.CanManage(r.Context(), p, auth.CapSystemRead)
	}) {
		body.LastDownloadedBy = st.LastDownloadedBy
	}
	writeJSONBody(w, http.StatusOK, body)
}

// statsNode resolves the addressed node for the ?stats face WITHOUT the
// content-plane Get call: ?stats is a telemetry probe, none of K69's three
// counting arms covers reading statistics, and a face that fed the counter
// it reports would be self-counting absurdity (the ADR's "probes don't
// count"). List carries the same path-scoped read gate as Get (anonymous
// follows the flag), answers both the file and the folder spelling through
// its exact-match arm, resolves a VIRTUAL address onto the member rows the
// counts live on, and writes no audit row — so the probe is invisible to
// the counters and the audit trail alike.
func (s *Server) statsNode(r *http.Request, p *auth.Principal, repoKey, relPath string) (*metadata.Node, error) {
	trimmed := strings.TrimSuffix(relPath, "/")
	if trimmed == "" {
		// The repository root carries no node row (serveRootFolder's
		// posture), so it has no statistics source: the item family's 404
		// rather than a fabricated zero. Folder rows DO exist and answer
		// with their structural zeros.
		return nil, fmt.Errorf("node %s/: %w", repoKey, repo.ErrNodeNotFound)
	}
	nodes, err := s.deps.ReposSvc.List(r.Context(), p, repoKey, trimmed)
	if err != nil {
		return nil, err
	}
	want := trimmed
	if isFolderPath(relPath) {
		want = trimmed + "/"
	}
	for _, n := range nodes {
		if n.Path == want {
			return n, nil
		}
	}
	return nil, fmt.Errorf("node %s/%s: %w", repoKey, relPath, repo.ErrNodeNotFound)
}

// dockerTagsForFolder resolves tag→digests[] for a docker image folder when
// the ?docker_tags query parameter is set (T-134 G32a: docker tree rendering).
// Returns nil when the repo is not a docker repo, the path is not a docker
// image directory, or the caller does not hold read on the image.
func (s *Server) dockerTagsForFolder(r *http.Request, repoKey, relPath string) map[string][]string {
	p := principalFrom(r.Context())
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil || row == nil || row.PackageType != repo.PackageDocker || row.Type != repo.TypeLocal {
		return nil
	}
	// The relPath is a folder path like "image/" or "image/manifests/".
	// Extract the image name: the part before the first "/" or before "/manifests/".
	trimmed := strings.TrimSuffix(relPath, "/")
	if trimmed == "" {
		return nil
	}
	image := trimmed
	// If the path is "image/manifests", strip the "/manifests" suffix.
	if strings.HasSuffix(trimmed, "/manifests") {
		image = strings.TrimSuffix(trimmed, "/manifests")
	} else if strings.HasSuffix(trimmed, "/blobs") {
		image = strings.TrimSuffix(trimmed, "/blobs")
	}
	// For bare image name (no sub-directory), use it directly. A multi-level
	// name's manifest/blobs suffix was already stripped above; a single-
	// component image like "app" needs no adjustment.
	// If the stripped path still has a "/" without being manifests/blobs, it's
	// something else — skip.
	tags, err := s.deps.ReposSvc.ListTags(r.Context(), p, repoKey, image, 0, "")
	if err != nil {
		return nil
	}
	if len(tags) == 0 {
		return nil
	}
	// Build digest→tag[] map. A digest can have multiple tags.
	out := make(map[string][]string, len(tags))
	for _, t := range tags {
		out[t.Digest] = append(out[t.Digest], t.Tag)
	}
	return out
}

// ---- ?permissions (SE-08, T-97) ----

// permissionViewer is the effective-permission facet of the authorizer the
// ?permissions view consumes (defined here at the consumer, per project
// convention): auth.Service implements it, discovered by assertion on
// Deps.Authz in New so the cmd wiring stays untouched.
type permissionViewer interface {
	// ItemPrincipals returns the user and group grants covering one repo
	// path through the permission targets that scope it (SE-08).
	ItemPrincipals(ctx context.Context, repoKey, path string) (users, groups map[string]auth.PrincipalBits, err error)
}

// permissionsView is the GET /api/storage/{repo}/{path}?permissions body
// (SE-08): the item uri plus the effective principal view, users and groups,
// each a map from the PRINCIPAL NAME to the permission letters (r/w/d/m/a)
// it holds on the item through the targets covering the path:
//
//	{"uri":..., "principals":{"users":{"jane":["r"]},"groups":{"devs":["r","w"]}}}
//
// Orientation (T-97 review B1, fixed): key = principal name, value = the
// sorted permission-letter set — the shape of the reference implementation
// (principal-keyed maps, empty collections skipped). The rest-api.md
// section 3 parenthetical reads the other way and is being errata'd by the
// conductor; a principal holding no action renders no entry, and an item no
// target covers answers empty objects.
type permissionsView struct {
	URI        string `json:"uri"`
	Principals struct {
		Users  map[string][]string `json:"users"`
		Groups map[string][]string `json:"groups"`
	} `json:"principals"`
}

// handleStoragePermissions serves GET /api/storage/{repo}/{path}?permissions
// (SE-08): which users and groups hold r/w/d on one item through the
// permission targets covering it — computed by the SAME predicate the
// Authorizer applies (auth.ItemPrincipals), so the view can never disagree
// with an actual authorization decision.
//
// Gates, in order: the route's admin gate comes FIRST (T-97 review B2: the
// view enumerates every principal name and its r/w/d distribution —
// security-configuration data — so it sits behind canManage like the rest
// of the management plane; BinFlow has no manage action and admin is the
// nearest mapping. A read-gated anonymous opening would let any visitor of
// an anonymous-read instance enumerate usernames/group names, defeating the
// login plane's existence-hiding). Then: unknown repository 404 (envelope,
// the storage plane's wording); non-local repository 400 (rest-api.md
// section 3: "non local/cached -> 400"; BinFlow has no shadow-cache repos,
// so remote and virtual both answer 400 — checked BEFORE any node
// resolution so a remote query never triggers an upstream fetch); then the
// item resolves through the content-plane service call (404 wording exactly
// as a plain item GET). The repository root ("") has no node row and takes
// the List call serveRootFolder uses.
func (s *Server) handleStoragePermissions(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	if s.permView == nil {
		writeError(w, http.StatusServiceUnavailable, "permission view is not available on this instance")
		return
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil {
		s.writeRepoLookupError(w, repoKey, err)
		return
	}
	if row.Type != repo.TypeLocal {
		writeError(w, http.StatusBadRequest,
			"permission view is only supported on local repositories (requested repository type: "+row.Type+")")
		return
	}

	p := principalFrom(r.Context())
	path := relPath
	if relPath == "" {
		if _, err := s.deps.ReposSvc.List(r.Context(), p, repoKey, ""); err != nil {
			s.writeStorageError(w, err)
			return
		}
	} else {
		node, nerr := s.storageNode(r, p, repoKey, relPath)
		if nerr != nil {
			s.writeStorageError(w, nerr)
			return
		}
		// The node row's canonical spelling keeps the folder/file distinction
		// ("devs/" vs "devs/w.bin") the path matcher keys on.
		path = node.Path
	}

	users, groups, err := s.permView.ItemPrincipals(r.Context(), repoKey, path)
	if err != nil {
		s.log.Error("httpapi: permission view failed", "repo", repoKey, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "permission view failed")
		return
	}
	view := permissionsView{URI: storageURI(requestBase(r), repoKey, path)}
	view.Principals.Users = principalLetters(users)
	view.Principals.Groups = principalLetters(groups)
	writeJSONBody(w, http.StatusOK, view)
}

// principalLetters renders the per-principal bit map in the wire orientation
// of the reference implementation: key = principal name, value = its
// permission letters in r/w/d order; a principal holding no action through
// the covering targets renders no entry (empty collections are skipped).
// M7 (ADR-0026, inventory family 7) appends the m letter — the repo-scoped
// manage bit, orthogonal to the path-plane letters and carried by any
// target that lists the repository (auth.PrincipalBits.Manage computes it
// with Can's exact predicate, so the view and the decision cannot diverge).
// M16 (T-444, ADR-0044 K68) appends the a letter — annotate, the
// property-write bit, rendered with BinFlow's internal compact code (the
// reference spells the annotate letter 'n'; BinFlow's closed action set
// uses 'a', the internal-code/wire-word layering K68 point 2 pins).
func principalLetters(m map[string]auth.PrincipalBits) map[string][]string {
	out := map[string][]string{}
	for name, bits := range m {
		letters := make([]string, 0, 5)
		if bits.Read {
			letters = append(letters, "r")
		}
		if bits.Write {
			letters = append(letters, "w")
		}
		if bits.Delete {
			letters = append(letters, "d")
		}
		if bits.Manage {
			letters = append(letters, "m")
		}
		if bits.Annotate {
			letters = append(letters, "a")
		}
		if len(letters) > 0 {
			out[name] = letters
		}
	}
	return out
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

// remoteBrowseViewer is the note-carrying listing seam's consumer face
// (consumer-side interface, the copyMoveRunner precedent — asserted off
// Deps.ReposSvc so the big repo.Service interface and the hand-written
// adapter fakes that implement it method by method stay untouched): the
// concrete service's RemoteBrowsePlane SPI segment (T-448), the exact walk
// List performs plus the remote-browse layer's degradation note.
type remoteBrowseViewer interface {
	ListWithRemote(ctx context.Context, p *repo.Principal, repoKey, prefix string) (*repo.RemoteBrowseListing, error)
}

// listWithNote is the storage faces' listing call: through the note-carrying
// seam when the assembly offers it, plain List otherwise (a service without
// the seam can by construction never engage the remote-browse layer, so "" is
// the honest note there). One resolution channel — ListWithRemote IS List's
// walk (repo.Service's own comment) — so the rows, the ordering and the gate
// answers never diverge between the two arms; only the note rides along.
func (s *Server) listWithNote(ctx context.Context, p *auth.Principal, repoKey, prefix string) ([]*metadata.Node, string, error) {
	if svc, ok := s.deps.ReposSvc.(remoteBrowseViewer); ok {
		listing, err := svc.ListWithRemote(ctx, p, repoKey, prefix)
		if err != nil {
			return nil, "", err
		}
		return listing.Nodes, listing.RemoteDegraded, nil
	}
	nodes, err := s.deps.ReposSvc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return nil, "", err
	}
	return nodes, "", nil
}

// serveRootFolder renders FolderInfo for the repository root: children are
// the direct first segments under "" (the root itself has no node row).
func (s *Server) serveRootFolder(w http.ResponseWriter, r *http.Request, p *auth.Principal, repoKey string) {
	nodes, degraded, err := s.listWithNote(r.Context(), p, repoKey, "")
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	s.writeFolderInfoBody(w, r, repoKey, "/", nil, childInfos(nodes, ""), nil, degraded)
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
		DownloadURI:  downloadURI(base, repoKey, node.Path),
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
// blob, ADR-0006), plus the node's properties when it carries any (the
// additive detail-body echo of FR-89.3 — filled HERE, not in fileInfoOf, so
// the search planes sharing fileInfoOf keep their lean field set).
func (s *Server) writeFileInfo(w http.ResponseWriter, r *http.Request, repoKey string, node *metadata.Node) {
	body := s.fileInfoOf(r.Context(), requestBase(r), repoKey, node)
	body.Properties = s.nodePropsOf(r.Context(), node)
	writeJSONBody(w, http.StatusOK, body)
}

// nodePropsOf reads one node's property set for the detail-body echo. A
// node without properties (the overwhelming default) answers nil — the
// field omits — and a store failure degrades the same way with a log
// line: the item body's core facts never hostage to the annotation plane.
func (s *Server) nodePropsOf(ctx context.Context, node *metadata.Node) map[string][]string {
	props, err := s.deps.Metadata.NodeProps().List(ctx, node.RepoKey, node.Path)
	if err != nil {
		s.log.ErrorContext(ctx, "httpapi: detail-body properties read failed",
			"repo", node.RepoKey, "path", node.Path, "error", err.Error())
		return nil
	}
	if len(props) == 0 {
		return nil
	}
	return props
}

// mimeOrDefault defaults an absent stored mime (FR-4-AC13 posture).
func mimeOrDefault(m string) string {
	if strings.TrimSpace(m) == "" {
		return "application/octet-stream"
	}
	return m
}

// writeFolderInfo renders FolderInfo for an explicit folder node row:
// children are the direct entries under the directory. When ?docker_tags is
// set on a docker repo, the response includes dockerTags.
func (s *Server) writeFolderInfo(w http.ResponseWriter, r *http.Request, repoKey string, node *metadata.Node) {
	dir := strings.TrimSuffix(node.Path, "/")
	nodes, degraded, err := s.listWithNote(r.Context(), principalFrom(r.Context()), repoKey, dir)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}
	var dockerTags map[string][]string
	if _, ok := r.URL.Query()["docker_tags"]; ok {
		// relPath for the folder is the node path without trailing slash.
		// The storage API path uses "/" + node.Path.
		dockerTags = s.dockerTagsForFolder(r, repoKey, node.Path)
	}
	s.writeFolderInfoBody(w, r, repoKey, "/"+node.Path, node, childInfos(nodes, dir), dockerTags, degraded)
}

// writeFolderInfoBody emits the FolderInfo shape. node is nil for the
// repository root (no row exists there); timestamps degrade to zero time.
// dockerTags is an optional digest→tag[] map for docker tree rendering.
// remoteDegraded is the remote-browse layer's error-state note, rendered as
// the optional remoteDegraded field when the listing's remote layer went
// quiet ("" — the field omits — on every healthy/flag-off/local tree).
func (s *Server) writeFolderInfoBody(w http.ResponseWriter, r *http.Request, repoKey, displayPath string, node *metadata.Node, children []folderChild, dockerTags map[string][]string, remoteDegraded string) {
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
	if len(dockerTags) > 0 {
		body.DockerTags = dockerTags
	}
	body.RemoteDegraded = remoteDegraded
	if node != nil {
		// Folder rows are property carriers like files (section 15.3.2);
		// the repository root (node == nil) has no node row and no echo.
		body.Properties = s.nodePropsOf(r.Context(), node)
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

// storageURI builds the uri field: <base>/binflow/api/storage/<repo>/<path>.
// The repo key appears exactly ONCE in the resulting URI (G33a: URI base family
// unification — prior to T-140, some callers prepended "api/storage/" to relPath
// which caused a wrong path shape; the family now shares the single definition).
// A trailing slash on relPath is preserved (folder addressing).
func storageURI(base, repoKey, relPath string) string {
	return base + "/binflow/api/storage/" + repoKey + "/" + relPath
}

// downloadURI builds the downloadUri field: <base>/binflow/<repo>/<path> —
// the DIRECT content-plane address (rest-api.md section 3's FileInfo carries
// uri and downloadUri as distinct fields; downloadUri is the downloadable
// URI, the address a GET downloads from, not the metadata view). The M1
// as-built (T-92) pointed both fields at the api/storage form; the M17
// errata (T-493, FR-157② / LC-107) restores the download semantics on the
// single definition the FileInfo body and the E-09 search envelope share —
// the generic/maven adapters' upload-201 bodies already spoke this form.
// Folder bodies keep omitting downloadUri (the as-built; Artifactory's
// folder example carries none either).
func downloadURI(base, repoKey, relPath string) string {
	return base + "/binflow/" + repoKey + "/" + relPath
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

// ---- ?list (E-10, P2; LOOP 009 L009-2 param family) ----

// fileListMediaType is the ?list response's pinned vendor content type
// (L008-3 §1 item 11: application/vnd.org.jfrog.artifactory.storage.FileList+json).
const fileListMediaType = "application/vnd.org.jfrog.artifactory.storage.FileList+json"

// listQueryParams are the seven integer parameters of the ?list family. Every
// PRESENT value must parse as an integer (the reference's getQueryParameterAsInt)
// — anything else answers 400 `For input string: "<v>"`. The boolean arms are on
// iff the parsed value equals 1 (L008-3 §1 items 1-2, 4, 8).
var listQueryParams = []string{
	"deep", "depth", "listFolders", "mdTimestamps", "statsTimestamps", "includeRootPath", "includePropertiesMd5",
}

// listOptions is the ?list family's validated parameter set (L010-1 P2: all
// seven params now consumed).
type listOptions struct {
	deep        bool // deep=1: recurse; any other integer value stays flat
	depth       int  // only a modifier: clamps the deep=1 recursion, never triggers it
	listFolders bool // listFolders=1: folder rows join files[]
	includeRoot bool // includeRootPath=1: the queried folder itself leads files[]
	// mdTimestamps=1: entries (files AND folders) carrying properties gain
	// mdTimestamps.properties = the node's last property-mutation time; with
	// statsTimestamps=1 downloaded file entries additionally gain
	// mdTimestamps.artifactory.stats = the last download time.
	mdTimestamps bool
	// statsTimestamps=1: file entries with at least one download gain
	// mdTimestamps.artifactory.stats (never-download files and folder rows
	// omit the key).
	statsTimestamps bool
	// includePropertiesMd5=1: entries carrying properties gain propertiesMd5
	// = md5 over the canonical property serialization (propsSetMd5).
	includePropsMd5 bool
}

// parseListOptions validates the seven integer params in the reference's
// order-independence: each PRESENT value is parsed once; the error message is
// Java's Integer.parseInt wording, byte-for-byte (`For input string: "abc"`).
// A param whose value is EMPTY or blank never reaches the parse — the
// reference's getQueryParameterAsInt short-circuits on
// containsKey && isNotBlank BEFORE parseInt and treats the param as 0
// (ArtifactResource.java:376-382; `?list&deep` is simply an absent deep).
func parseListOptions(q url.Values) (listOptions, error) {
	var o listOptions
	for _, name := range listQueryParams {
		vals, ok := q[name]
		if !ok || len(vals) == 0 || strings.TrimSpace(vals[0]) == "" {
			continue
		}
		n, err := strconv.Atoi(vals[0])
		// Java's parseInt is int32-bounded: a value outside [MinInt32,
		// MaxInt32] throws the SAME NumberFormatException wording (public
		// spec; `depth=2147483648` -> `For input string: "2147483648"`).
		// Go's Atoi is 64-bit and only errors past int64 — the (2^31, 2^63)
		// window must be caught explicitly (Review A).
		if err != nil || n > math.MaxInt32 || n < math.MinInt32 {
			return o, errors.New(`For input string: "` + vals[0] + `"`)
		}
		switch name {
		case "deep":
			o.deep = n == 1
		case "depth":
			o.depth = n
		case "listFolders":
			o.listFolders = n == 1
		case "includeRootPath":
			o.includeRoot = n == 1
		case "mdTimestamps":
			o.mdTimestamps = n == 1
		case "statsTimestamps":
			o.statsTimestamps = n == 1
		case "includePropertiesMd5":
			o.includePropsMd5 = n == 1
		}
	}
	return o, nil
}

// listFile is one files[] entry of the ?list response: uri carries a LEADING
// slash and the path RELATIVE to the queried directory (`/f1.txt`, `/d2/f3.txt`);
// a folder row spells `/d2` (no trailing slash), size -1, no digests, and mixes
// with file rows in the one alphabetical order (L008-3 §1 items 4, 9).
type listFile struct {
	URI          string `json:"uri"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
	Folder       bool   `json:"folder"`
	SHA1         string `json:"sha1,omitempty"`
	SHA2         string `json:"sha2,omitempty"`
	// MDTimestamps carries mdTimestamps.properties (property-mutation time of
	// property-carrying entries) and/or mdTimestamps.artifactory.stats (last
	// download time of downloaded file entries) — each key present only when
	// the param asked for it AND the node has the underlying fact. Map
	// marshaling sorts keys, matching the reference's artifactory.stats <
	// properties order (L010-1 live evidence). Sits after sha2, before
	// propertiesMd5 (same evidence).
	MDTimestamps map[string]string `json:"mdTimestamps,omitempty"`
	// PropertiesMd5 is the property set's canonical md5 (propsSetMd5), only
	// on property-carrying entries under includePropertiesMd5=1.
	PropertiesMd5 string `json:"propertiesMd5,omitempty"`
}

// listResponse is the ?list body.
type listResponse struct {
	URI     string     `json:"uri"`
	Created string     `json:"created"`
	Files   []listFile `json:"files"`
	// RemoteDegraded mirrors the FolderInfo note on the flat listing face —
	// same seam (listWithNote), same value, so the two faces can never
	// disagree about a tree (the one-resolution-channel discipline of
	// RemoteBrowsePlane). Omitted on healthy/flag-off/local trees.
	RemoteDegraded string `json:"remoteDegraded,omitempty"`
}

// handleStorageList serves GET /api/storage/{repo}/{path}?list (E-10).
// Behavior per the L008-3 differential (32-arm matrix, live both sides):
//
//   - anonymous -> 403 (kept); the seven integer params validate FIRST — any
//     present non-numeric value answers 400 `For input string: "<v>"`;
//   - the repository root lists (200, its direct child files); the reference
//     refuses only requests carrying no repository segment at all, and the
//     router never dispatches those here;
//   - a file target -> 400 `Expected a folder but found a file, at: <repo>:<path>`
//     (colon spelling);
//   - recursion triggers on deep=1 ONLY; depth is a modifier that clamps the
//     recursion (deep=1&depth=N = N levels, depth<=0 = unlimited) and never
//     triggers recursion alone;
//   - folder rows appear only under listFolders=1 (`/d2` form, size -1);
//     includeRootPath=1 leads files[] with the queried folder as `/`;
//   - mdTimestamps=1 / statsTimestamps=1 / includePropertiesMd5=1 add the
//     P2 metadata keys (enrichListEntry / propsSetMd5);
//   - created is the request's wall clock; the body's Content-Type is the
//     FileList vendor media type.
func (s *Server) handleStorageList(w http.ResponseWriter, r *http.Request, repoKey, relPath string) {
	p := principalFrom(r.Context())
	if p == nil {
		writeError(w, http.StatusForbidden, "listing repository files requires an authenticated user")
		return
	}
	opts, err := parseListOptions(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// The queried directory: relPath "" is the repository root (listable —
	// no node row exists there, exactly like serveRootFolder's posture).
	var node *metadata.Node
	dir := ""
	if relPath != "" {
		node, err = s.storageNode(r, p, repoKey, relPath)
		if err != nil {
			s.writeStorageError(w, err)
			return
		}
		if !isFolderPath(node.Path) {
			writeError(w, http.StatusBadRequest,
				"Expected a folder but found a file, at: "+repoKey+":"+node.Path)
			return
		}
		dir = strings.TrimSuffix(node.Path, "/")
	}

	nodes, degraded, err := s.listWithNote(r.Context(), p, repoKey, dir)
	if err != nil {
		s.writeStorageError(w, err)
		return
	}

	// Recursion semantics (L008-3 §1 item 3): flat by default; deep=1 opens
	// the tree with depth as its only clamp (0/negative = unlimited).
	limit := 1
	if opts.deep {
		limit = opts.depth
	}

	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}

	// P2 metadata enrichment (L010-1): mdTimestamps needs each node's last
	// property-mutation time (audit-derived, see propModifiedTimes); the
	// property-presence keys and propertiesMd5 need the per-entry property
	// sets — both read lazily below, only under the asking params.
	var propMtimes map[string]string
	if opts.mdTimestamps {
		propMtimes = s.propModifiedTimes(r.Context(), repoKey)
	}

	rootModified := ""
	if node != nil {
		rootModified = node.UpdatedAt
		if rootModified == "" {
			rootModified = node.CreatedAt
		}
	} else if opts.includeRoot {
		// The repository root has no node row; its "/" entry still carries a
		// REAL timestamp on the reference (the storage root's own mtime —
		// live evidence L010-1), never the epoch zero. The repo row's
		// creation time is BinFlow's honest analog (the root materializes
		// with the repository itself).
		if row, rerr := s.deps.Repos.Get(r.Context(), repoKey); rerr == nil && row != nil {
			rootModified = row.CreatedAt
		}
	}
	files := make([]listFile, 0, len(nodes)+1)
	if opts.includeRoot {
		rootEntry := listFile{
			URI: "/", Size: -1, Folder: true,
			LastModified: isoMillisUTC(rootModified),
		}
		if node != nil {
			s.enrichListEntry(r.Context(), &rootEntry, node, opts, propMtimes)
		}
		files = append(files, rootEntry)
	}
	for _, n := range nodes {
		rel := strings.TrimPrefix(n.Path, prefix)
		if rel == "" {
			continue // the queried folder row itself
		}
		folder := isFolderPath(n.Path)
		if folder && !opts.listFolders {
			continue // folder rows join only under listFolders=1
		}
		name := strings.TrimSuffix(rel, "/")
		if levels := strings.Count(name, "/") + 1; limit > 0 && levels > limit {
			continue
		}
		entry := listFile{
			URI:          "/" + name,
			LastModified: isoMillisUTC(n.UpdatedAt),
			Folder:       folder,
		}
		if folder {
			entry.Size = -1
		} else {
			entry.Size = n.Size
			if n.Sha256 != "" {
				entry.SHA2 = n.Sha256
				if b, berr := s.deps.Metadata.Blobs().Get(r.Context(), n.Sha256); berr == nil && b != nil {
					entry.SHA1 = b.Sha1
				}
			}
		}
		s.enrichListEntry(r.Context(), &entry, n, opts, propMtimes)
		files = append(files, entry)
	}
	// The whole listing is one alphabetical order over the entry uris
	// (L008-3 §1 item 14) — folder rows mix with file rows, `/` leads.
	sort.Slice(files, func(i, j int) bool { return files[i].URI < files[j].URI })

	display := ""
	if node != nil {
		display = node.Path
	}
	resp := listResponse{
		// No trailing slash on the queried folder's own uri (root = bare
		// repo key; L008-3 §1 item 9).
		URI:            strings.TrimSuffix(storageURI(requestBase(r), repoKey, display), "/"),
		Created:        time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Files:          files,
		RemoteDegraded: degraded,
	}
	writeJSONBodyCT(w, http.StatusOK, fileListMediaType, resp)
}

// enrichListEntry applies the P2 metadata parameters to one files[] entry
// (L010-1, live evidence on the reference): mdTimestamps=1 adds
// mdTimestamps.properties (only when the node carries properties AND a
// mutation time is known) and mdTimestamps.artifactory.stats (only on file
// rows with at least one download, under statsTimestamps=1);
// includePropertiesMd5=1 adds the property set's canonical md5. Every key is
// a fact of the node — nothing is faked (a property-carrying node whose
// mutation time is beyond the audit window simply omits the properties key,
// the statsInfoBody "never faked" posture).
func (s *Server) enrichListEntry(ctx context.Context, entry *listFile, node *metadata.Node, opts listOptions, propMtimes map[string]string) {
	var props map[string][]string
	if opts.mdTimestamps || opts.includePropsMd5 {
		var err error
		props, err = s.deps.Metadata.NodeProps().List(ctx, node.RepoKey, node.Path)
		if err != nil {
			s.log.ErrorContext(ctx, "httpapi: list properties read failed",
				"repo", node.RepoKey, "path", node.Path, "error", err.Error())
			props = nil
		}
	}
	if len(props) == 0 {
		props = nil // property-less entries carry neither P2 key
	}
	if opts.mdTimestamps && props != nil {
		if t, ok := propMtimes[node.Path]; ok && t != "" {
			if entry.MDTimestamps == nil {
				entry.MDTimestamps = map[string]string{}
			}
			entry.MDTimestamps["properties"] = isoMillisUTC(t)
		}
	}
	if opts.statsTimestamps && !entry.Folder {
		st, err := s.deps.Metadata.Nodes().Stats(ctx, node.RepoKey, node.Path)
		if err != nil {
			s.log.ErrorContext(ctx, "httpapi: list stats read failed",
				"repo", node.RepoKey, "path", node.Path, "error", err.Error())
		} else if st != nil && st.LastDownloadedAt != "" {
			// The reference takes max(lastDownloaded, remoteLastDownloaded);
			// BinFlow has no smart-remote pull-back statistic (the ?stats
			// face's own UNSOURCED posture), so the local arm is the whole
			// max.
			if entry.MDTimestamps == nil {
				entry.MDTimestamps = map[string]string{}
			}
			entry.MDTimestamps["artifactory.stats"] = isoMillisUTC(st.LastDownloadedAt)
		}
	}
	if opts.includePropsMd5 && props != nil {
		entry.PropertiesMd5 = propsSetMd5(props)
	}
}

// propModifiedTimes derives every node's last property-mutation time in one
// repository from the audit log (props.write / props.delete rows, the only
// faces that mutate node properties). Two newest-first queries, first sight
// per path wins; a path present in both actions keeps the newer time
// (RFC3339 UTC text compares chronologically).
//
// ponytail: page capped at 1000 events per action — a repository with more
// property history than that derives stale/absent mtimes for the tail. The
// proper face is a metadata GROUP BY (path, max(time)) like
// AuditStore.LastActionTimes; promote when a listing over heavy property
// history measurably needs it.
func (s *Server) propModifiedTimes(ctx context.Context, repoKey string) map[string]string {
	out := map[string]string{}
	for _, action := range []string{propsAuditWrite, propsAuditDelete} {
		events, err := s.deps.Metadata.Audits().Query(ctx, metadata.AuditQuery{
			RepoKey: repoKey, Action: action, Limit: 1000,
		})
		if err != nil {
			// Degrade to whatever the other action yields: a missing mtime
			// omits a key, it never wrongs one.
			s.log.ErrorContext(ctx, "httpapi: property-mtime audit query failed",
				"repo", repoKey, "action", action, "error", err.Error())
			continue
		}
		for _, e := range events {
			if e.Path == "" {
				continue
			}
			if prev, seen := out[e.Path]; !seen || e.Time > prev {
				out[e.Path] = e.Time
			}
		}
	}
	return out
}

// propsSetMd5 renders the propertiesMd5 of one property set: md5 over the
// concatenation of key+value for every value, keys and values each in
// ascending order, with NO separator anywhere (derived black-box on the
// reference, L010-1: ten arms including multi-value, multi-key,
// insertion-order reversal, and folder/file equality — e.g. {pa:[y],pb:[x]}
// -> md5("paypbx"); the folder and the file carrying the same set answer
// the same digest, so no path or repo salt exists).
func propsSetMd5(props map[string][]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := md5.New() //nolint:gosec // G401: the reference's propertiesMd5 compatibility digest, see the import ruling
	for _, k := range keys {
		vs := append([]string(nil), props[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			_, _ = h.Write([]byte(k))
			_, _ = h.Write([]byte(v))
		}
	}
	return hex.EncodeToString(h.Sum(nil))
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
