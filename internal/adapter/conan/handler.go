package conan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves — the
// repositories.package_type value httpapi dispatches on.
const Protocol = "conan"

// hdrChecksum* is the content plane's client-checksum family (the contract
// every adapter honors on PUT and download; conan 2 sends none — the family
// serves curl deployments and the checksum-deploy capability).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
	hdrChecksumDeploy = "X-Checksum-Deploy"
)

// The capability response headers (spec section 2; TL-2's pinned values —
// every conan endpoint response carries the family, including the error
// arms).
const (
	hdrServerVersion     = "X-Conan-Server-Version"
	hdrServerCaps        = "X-Conan-Server-Capabilities"
	hdrClientVerCheck    = "X-Conan-Client-Version-Check"
	hdrClientVer         = "X-Conan-Client-Version"
	serverVersionPinned  = "0.20.0"
	clientVersionFloor   = "0.16.0"
	clientVersionCurrent = "0.20.0"

	// capsLocal is the local repository's capability list (TL-2).
	capsLocal = "complex_search,checksum_deploy,revisions,matrix_params"
	// capsOnlyV2 is appended for remote/virtual (the classes that refuse
	// the v1 plane; T-312 lands their serving).
	capsOnlyV2 = "only_v2"
)

// msgV1LocalOnly is the S6 refusal's pinned wording (spec section 1: the
// code-explicit branch, quoted verbatim).
func msgV1LocalOnly(repoKey string) string {
	return "Unsupported Conan v1 repository request for '" + repoKey + "'"
}

// msgPathNotFound is the v1 files channel's 404 body (spec section 3.2).
const msgPathNotFound = "Path not found"

// errInvalidChecksum wraps a malformed client-declared X-Checksum-* value
// (the adapter-root sentinel, restated so this package maps it without
// importing a sibling error site it does not use).
var errInvalidChecksum = adapter.ErrInvalidChecksum

// BlobLedger is the read-only digest ledger the download headers and the v1
// md5 snapshots consult (satisfied by metadata.Store.Blobs(); the
// goproxy/cargo seam verbatim).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// TokenIssuer is the mint seam the v1 authenticate endpoint rides (satisfied
// by auth.Service / auth.TokenRegistry; the npm login posture).
type TokenIssuer interface {
	Issue(ctx context.Context, username string, ttl time.Duration) (*auth.IssuedToken, error)
}

// Options tunes the handler (the npm/nuget/cargo posture).
type Options struct {
	// BaseURL is the externally visible origin (server.base_url). Empty
	// means derive from the request (X-Forwarded-Proto honored) — TL-1.
	BaseURL string
	// TokenTTL is the v1 authenticate token lifetime (0 = the conan default
	// below; the npm login precedent of keeping it local to the plane).
	TokenTTL time.Duration
}

// conanTokenTTL is the v1 token lifetime when Options.TokenTTL is unset:
// the auth module's npm-plane default (720h), the same contract conan's own
// remote tokens keep.
const conanTokenTTL = 720 * time.Hour

// Handler is the Conan protocol adapter. It owns the wire protocol only;
// every content operation goes through repo.Service, and the revision
// index machinery (index.json + .timestamp) is the package's own layer
// over the same node namespace.
type Handler struct {
	svc   repo.Service
	repos repo.ClassReader
	blobs BlobLedger
	toks  TokenIssuer
	opts  Options
	index indexLocks // per-index serialization of the read-modify-write cycles
}

// New wires the handler. blobs may be nil (a bare fake stack: downloads
// degrade to sha256-only headers and the v1 snapshots answer the honest
// 500); toks may be nil (authenticate answers the honest 503).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, toks TokenIssuer, opts Options) *Handler {
	ttl := opts.TokenTTL
	if ttl <= 0 {
		ttl = conanTokenTTL
	}
	opts.TokenTTL = ttl
	return &Handler{svc: svc, repos: repos, blobs: blobs, toks: toks, opts: opts}
}

// Register builds the handler and enters both the handler registry and the
// metadata-provider registry under one literal (the cargo.Register
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, toks TokenIssuer, opts Options) *Handler {
	h := New(svc, repos, blobs, toks, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: all three classes (spec section
// 7) — local in full (T-308), remote through the pull-through engine
// inside svc.Get plus the marker-document faces (remote.go), virtual
// through the member-order aggregations (virtual.go).
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler (see layout.go).
func (h *Handler) Layout(r *http.Request) (string, string, error) { return layout(r) }

// classOf resolves the repository class (the cargo defensive arm).
func (h *Handler) classOf(ctx context.Context, repoKey string) (string, error) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return "", errRepoNotFound(repoKey)
		}
		return "", fmt.Errorf("load repository %s: %w", repoKey, err)
	}
	return row.Type, nil
}

// errRepoNotFound shapes the missing-repository refusal.
func errRepoNotFound(repoKey string) error {
	return fmt.Errorf("repository %s: %w", repoKey, repo.ErrRepoNotFound)
}

// ServeHTTP dispatches on the parsed wire target. The capability header
// family rides EVERY response (spec section 2), so every path writes
// through a capWriter; the class door runs before every family — the v1
// DATA plane is local-only (S6's pinned 400), the handshake trio is
// class-independent (it is how a client discovers the only_v2 capability
// the remote/virtual rows of section 2's table advertise), and the v2
// plane splits into the local, remote (remote.go) and virtual (virtual.go)
// faces.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		h.writeError(h.cap(w, r, repo.TypeLocal), err, repoKey, rel)
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	rt, ok := parseRoute(rel)
	if !ok {
		writePlain(h.cap(w, r, repo.TypeLocal), http.StatusNotFound, "not found")
		return
	}

	class, err := h.classOf(ctx, repoKey)
	if err != nil {
		h.writeError(h.cap(w, r, repo.TypeLocal), err, repoKey, rel)
		return
	}
	cw := h.cap(w, r, class)

	if class != repo.TypeLocal {
		if isV1DataFamily(rt.kind) {
			// S6: the pinned 400 (spec section 1, code-explicit branch) —
			// the v1 DATA plane is local-only.
			writePlain(cw, http.StatusBadRequest, msgV1LocalOnly(repoKey))
			return
		}
		switch class {
		case repo.TypeRemote:
			h.serveRemote(ctx, cw, r, p, repoKey, rt)
		case repo.TypeVirtual:
			h.serveVirtual(ctx, cw, r, p, repoKey, rt)
		default:
			writePlain(cw, http.StatusNotFound, "not found")
		}
		return
	}

	switch rt.kind {
	case kindNotFoundFamily:
		writePlain(cw, http.StatusNotFound, "not found")

	// ---- the v1 handshake family (spec section 2) ----
	case kindV1Ping:
		h.servePing(cw)
	case kindV1Authenticate:
		h.serveAuthenticate(ctx, cw, p)
	case kindV1CheckCredentials:
		h.serveCheckCredentials(cw, p)

	// ---- search (both planes, one implementation) ----
	case kindV1Search, kindV2Search:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveSearch(ctx, cw, r, p, repoKey)

	// ---- the v2 revision family ----
	case kindV2Latest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveLatest(ctx, cw, p, repoKey, rt.ref)
	case kindV2Revisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRevisions(ctx, cw, p, repoKey, rt.ref)
	case kindV2RevDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		h.serveRevisionDelete(ctx, cw, p, repoKey, rt.ref, rt.rRev)
	case kindV2RecipeDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		h.serveRecipeDelete(ctx, cw, p, repoKey, rt.ref)
	case kindV2RefSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRefSearch(ctx, cw, p, repoKey, rt.ref, "") // "" = implicit latest
	case kindV2RevSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRefSearch(ctx, cw, p, repoKey, rt.ref, rt.rRev)
	case kindV2Files:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if rt.path == "" {
				h.serveFileList(ctx, cw, p, repoKey, rt.ref, rt.rRev, "", "")
			} else {
				h.serveFileGet(ctx, cw, r, p, repoKey, recipeFile(rt.ref.coordinateRoot(), rt.rRev, rt.path))
			}
		case http.MethodPut:
			h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, "", "",
				recipeFile(rt.ref.coordinateRoot(), rt.rRev, rt.path))
		default:
			cw.Header().Set("Allow", "GET, HEAD, PUT")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the recipe files target")
		}
	case kindV2PackagesDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		h.servePackagesDelete(ctx, cw, p, repoKey, rt.ref, rt.rRev)
	case kindV2PkgLatest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.servePkgLatest(ctx, cw, p, repoKey, rt.ref, rt.rRev, rt.pid)
	case kindV2PkgRevisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.servePkgRevisions(ctx, cw, p, repoKey, rt.ref, rt.rRev, rt.pid)
	case kindV2PkgRevDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		h.servePkgRevDelete(ctx, cw, p, repoKey, rt.ref, rt.rRev, rt.pid, rt.pRev)
	case kindV2PkgFiles:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if rt.path == "" {
				h.serveFileList(ctx, cw, p, repoKey, rt.ref, rt.rRev, rt.pid, rt.pRev)
			} else {
				h.serveFileGet(ctx, cw, r, p, repoKey, pkgFile(rt.ref.coordinateRoot(), rt.rRev, rt.pid, rt.pRev, rt.path))
			}
		case http.MethodPut:
			h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, rt.pid, rt.pRev,
				pkgFile(rt.ref.coordinateRoot(), rt.rRev, rt.pid, rt.pRev, rt.path))
		default:
			cw.Header().Set("Allow", "GET, HEAD, PUT")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the package files target")
		}

	// ---- the v1 data family (CN-1 final: the full plane) ----
	case kindV1RecipeSnapshot:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveSnapshot(ctx, cw, p, repoKey, rt.ref, "")
		case http.MethodDelete:
			h.serveV1RecipeDelete(ctx, cw, p, repoKey, rt.ref)
		default:
			cw.Header().Set("Allow", "GET, HEAD, DELETE")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the v1 recipe target")
		}
	case kindV1RefSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveRefSearch(ctx, cw, p, repoKey, rt.ref, "") // "" = implicit latest
	case kindV1Digest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveDigest(ctx, cw, r, p, repoKey, rt.ref, rt.pid)
	case kindV1DownloadURLs:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveDownloadURLs(ctx, cw, r, p, repoKey, rt.ref, rt.pid)
	case kindV1UploadURLs:
		if r.Method != http.MethodPost {
			cw.Header().Set("Allow", "POST")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the upload_urls target")
			return
		}
		h.serveUploadURLs(cw, r, repoKey, rt.ref, rt.pid)
	case kindV1RemoveFiles:
		if r.Method != http.MethodPost {
			cw.Header().Set("Allow", "POST")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the remove_files target")
			return
		}
		h.serveRemoveFiles(ctx, cw, r, p, repoKey, rt.ref, rt.pid)
	case kindV1PkgSnapshot:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveSnapshot(ctx, cw, p, repoKey, rt.ref, rt.pid)
	case kindV1PackagesDelete:
		if r.Method != http.MethodPost {
			cw.Header().Set("Allow", "POST")
			writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the packages/delete target")
			return
		}
		h.servePackagesDeleteIDs(ctx, cw, r, p, repoKey, rt.ref)
	case kindV1Files:
		h.serveV1Files(ctx, cw, r, p, repoKey, rt)
	default:
		writePlain(cw, http.StatusNotFound, "not found")
	}
}

// requireMethod renders the 405 (with Allow) unless the method matches;
// false means the request was refused and the caller returns.
func (h *Handler) requireMethod(cw *capWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	cw.Header().Set("Allow", strings.Join(allowed, ", "))
	writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on this route")
	return false
}

// isV1DataFamily reports whether the route belongs to the v1 DATA plane —
// the S6 branch's predicate. The handshake trio (ping, authenticate,
// check_credentials — reachable under BOTH version prefixes) is
// deliberately NOT in this set: it is the class-independent capability
// negotiation (spec section 2's table applied to the remote/virtual rows,
// whose advertised capabilities include only_v2 — a client could never
// learn that from a class that refuses the probe).
func isV1DataFamily(k routeKind) bool {
	switch k {
	case kindV1Search, kindV1RecipeSnapshot, kindV1RefSearch, kindV1Digest,
		kindV1DownloadURLs, kindV1UploadURLs, kindV1RemoveFiles, kindV1PkgSnapshot,
		kindV1PackagesDelete, kindV1Files:
		return true
	}
	return false
}

// ---- capability headers (spec section 2 / TL-2) ----

// cap builds the ResponseWriter wrapper stamping the capability family on
// every response this adapter writes.
func (h *Handler) cap(w http.ResponseWriter, r *http.Request, class string) *capWriter {
	return &capWriter{ResponseWriter: w, class: class, clientVer: r.Header.Get(hdrClientVer)}
}

// capWriter is the capability-header writer: the pinned server version and
// the class-aware capability list land at WriteHeader time, plus the
// version-check answer when the request carried a client version (spec
// section 2's table).
type capWriter struct {
	http.ResponseWriter
	class     string
	clientVer string
}

func (c *capWriter) WriteHeader(code int) {
	h := c.Header()
	h.Set(hdrServerVersion, serverVersionPinned)
	h.Set(hdrServerCaps, capabilitiesFor(c.class))
	if v := clientVersionCheck(c.clientVer); v != "" {
		h.Set(hdrClientVerCheck, v)
	}
	c.ResponseWriter.WriteHeader(code)
}

// capabilitiesFor renders TL-2's pinned list: local carries the four
// capabilities; remote/virtual append only_v2 (the v1-refusing classes).
func capabilitiesFor(class string) string {
	if class == repo.TypeLocal {
		return capsLocal
	}
	return capsLocal + "," + capsOnlyV2
}

// clientVersionCheck renders the version-check header value (spec section
// 2's comparison table): below 0.16.0 deprecated, below 0.20.0 outdated, at
// 0.20.0 current, above server_outdated. An absent or unparseable client
// version answers "" (no header).
func clientVersionCheck(client string) string {
	v := normalizeConanVersion(client)
	switch {
	case v == "":
		return ""
	case compareConanVersion(v, clientVersionFloor) < 0:
		return "deprecated"
	case compareConanVersion(v, clientVersionCurrent) < 0:
		return "outdated"
	case compareConanVersion(v, clientVersionCurrent) == 0:
		return "current"
	default:
		return "server_outdated"
	}
}

// normalizeConanVersion keeps the leading numeric triple (drops any
// pre-release/build suffix — the comparison the spec table describes keys
// on the triple).
func normalizeConanVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	var parts []string
	for i := 0; i < 3; i++ {
		j := 0
		for j < len(v) && v[j] >= '0' && v[j] <= '9' {
			j++
		}
		parts = append(parts, v[:j])
		if j >= len(v) || v[j] != '.' {
			break
		}
		v = v[j+1:]
		if v == "" {
			break
		}
	}
	for _, p := range parts {
		if p == "" {
			return ""
		}
	}
	return strings.Join(parts, ".")
}

// compareConanVersion compares two numeric-triple spellings (-1/0/+1).
func compareConanVersion(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for len(as) < 3 {
		as = append(as, "0")
	}
	for len(bs) < 3 {
		bs = append(bs, "0")
	}
	for i := 0; i < 3; i++ {
		av, bv := mustAtoi(as[i]), mustAtoi(bs[i])
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// mustAtoi parses a small non-negative int ("" and garbage are 0 — the
// normalizer already rejected those shapes).
func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// ---- rendering ----

// writePlain renders one plain-text body (the conan error family and the
// ping/authenticate bodies — conan's protocol has no JSON error envelope).
func writePlain(w http.ResponseWriter, status int, msg string) {
	body := []byte(msg)
	h := w.Header()
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed text, never client bytes
}

// writeJSON renders one JSON document.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed JSON, never client bytes
}

// writeJSONDoc marshals and renders doc at 200 (a marshal failure answers
// the honest 500).
func writeJSONDoc(w http.ResponseWriter, doc any) {
	body, err := json.Marshal(doc)
	if err != nil {
		writePlain(w, http.StatusInternalServerError, "render response: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// writeError maps service errors onto the protocol surface (the goproxy
// matrix under the plain-text conan family: one shared mapping).
func (h *Handler) writeError(w http.ResponseWriter, err error, repoKey, path string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writePlain(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writePlain(w, http.StatusConflict, fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, path, err))
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writePlain(w, http.StatusNotFound, msgPathNotFound)
	case errors.Is(err, repo.ErrRepoNotFound):
		writePlain(w, http.StatusNotFound, fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey))
	case errors.Is(err, repo.ErrInvalidPath), errors.Is(err, errInvalidChecksum),
		errors.Is(err, metadata.ErrInvalidProperties):
		writePlain(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(w, http.StatusUnauthorized, "unauthorized user")
	case errors.Is(err, repo.ErrForbidden):
		writePlain(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, repo.ErrQuotaExceeded):
		writePlain(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		writePlain(w, http.StatusInternalServerError, err.Error())
	}
}

// baseURLFor resolves the absolute origin the generated documents cite
// (Options.BaseURL = server.base_url wins; otherwise the request's
// scheme+host, X-Forwarded-Proto honored — TL-1, the npm/nuget posture).
func (h *Handler) baseURLFor(r *http.Request) string {
	if h.opts.BaseURL != "" {
		return strings.TrimRight(h.opts.BaseURL, "/")
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// digestTriple and digestsOf: the download-header family (the cargo
// posture — sha256 from the node, sha1/md5 from the ledger, degraded).
type digestTriple struct{ sha256, sha1, md5 string }

func (h *Handler) digestsOf(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.blobs == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.blobs.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// md5Of resolves one node's md5 through the ledger (the v1 snapshot's
// value; "" when the ledger is unwired or the row lacks it).
func (h *Handler) md5Of(ctx context.Context, node *metadata.Node) string {
	return h.digestsOf(ctx, node).md5
}

// serveNode streams one stored node: checksum headers, Content-Length,
// Content-Type, body suppressed on HEAD.
func (h *Handler) serveNode(ctx context.Context, w http.ResponseWriter, r *http.Request, node *metadata.Node, rc io.ReadSeekCloser, ctype string) {
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		for k, vv := range extra.ExtraHeaders() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	sums := h.digestsOf(ctx, node)
	hdr := w.Header()
	if sums.sha256 != "" {
		hdr.Set(hdrChecksumSha256, sums.sha256)
	}
	if sums.sha1 != "" {
		hdr.Set(hdrChecksumSha1, sums.sha1)
	}
	if sums.md5 != "" {
		hdr.Set(hdrChecksumMd5, sums.md5)
	}
	if node.Size > 0 {
		hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	}
	hdr.Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}
