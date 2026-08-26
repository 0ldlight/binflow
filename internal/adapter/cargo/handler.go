package cargo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves — the
// repositories.package_type value httpapi dispatches on. "cargo" (the Go
// package name carries no dot).
const Protocol = "cargo"

// hdrChecksum* is the content plane's client-checksum family (the same
// contract every adapter honors on PUT and download; cargo clients send
// none — the family serves curl deployments).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// errInvalidChecksum wraps a malformed client-declared X-Checksum-* value
// (the adapter-root sentinel's 400 semantics, restated so this package
// maps it without importing a sibling error site it does not use).
var errInvalidChecksum = adapter.ErrInvalidChecksum

// msgGitDeprecated is the git-index refusal wording (spec section 1).
const msgGitDeprecated = "Cargo's Git index is deprecated and no longer supported. Please migrate this repository to the sparse HTTP index (cargoInternalIndex=true)."

// msgDownload404 is the download refusal's pinned body (spec section 2).
const msgDownload404 = "unable to download crate"

// BlobLedger is the read-only digest ledger the download headers consult
// (satisfied by metadata.Store.Blobs(); the goproxy/nuget seam verbatim).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// NodeProps is the node-property seam yank state and search facts ride on
// (satisfied by metadata.Store.NodeProps() — the protocol's own state,
// not just annotation: the crate.yanked property IS the yank flag).
type NodeProps interface {
	List(ctx context.Context, repoKey, path string) (map[string][]string, error)
	Merge(ctx context.Context, repoKey, path string, props map[string][]string) error
	Delete(ctx context.Context, repoKey, path string, keys []string) error
}

// Options tunes the handler (the npm/nuget posture).
type Options struct {
	// BaseURL is the externally visible origin (server.base_url). Empty
	// means derive from the request (X-Forwarded-Proto honored) — TL-1.
	BaseURL string
	// AnonymousAccess mirrors the instance's global anonymous read flag:
	// false makes config.json carry "auth-required": true (spec section
	// 3.1) — cargo then authenticates its index/download requests.
	AnonymousAccess bool
}

// Handler is the Cargo protocol adapter. It owns the wire protocol only;
// every content operation goes through repo.Service.
type Handler struct {
	svc      repo.Service
	repos    repo.ClassReader
	blobs    BlobLedger
	props    NodeProps
	opts     Options
	rewrites rewriteMutexes // per-(repoKey, crate) serialization of the index rewrites (B1)
}

// New wires the handler. props may be nil (a bare fake stack: yank
// answers the honest 500, search degrades to name-only facts).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, props: props, opts: opts}
}

// Register builds the handler and enters both the handler registry and
// the metadata-provider registry under one literal (the pypi.Register
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	h := New(svc, repos, blobs, props, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: T-294 serves LOCAL in full — the
// remote pull-through and virtual aggregation are their own M11 tickets
// (spec sections 8/S4/S5) and the class door refuses them until they
// land.
func (h *Handler) RepoTypes() []string { return []string{repo.TypeLocal} }

// Layout implements adapter.Handler (see layout.go).
func (h *Handler) Layout(r *http.Request) (string, string, error) { return layout(r) }

// classOf resolves the repository class (the goproxy defensive arm).
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

// ServeHTTP dispatches on the parsed wire target. Errors render as the
// official errors[] JSON envelope ({"errors":[{"detail":"…"}]} — spec
// section 2): cargo surfaces both the 4xx/5xx form and the legacy
// 200+errors form as failures, and CG-2 keeps BinFlow on the former only.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writeEnvelope(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	rt, ok := parseRoute(rel)
	if !ok {
		writeEnvelope(w, http.StatusNotFound, "not found")
		return
	}
	if rt.kind != kindRoot && rt.kind != kindGitFace {
		class, err := h.classOf(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		if class != repo.TypeLocal {
			writeEnvelope(w, http.StatusNotFound, fmt.Sprintf(
				"cargo %s repositories are not served by this BinFlow release (the sparse index ships local repositories; remote and virtual land with their own tickets)", class))
			return
		}
	}

	switch rt.kind {
	case kindRoot:
		// The repository-root probe (spec section 2): 200 with an empty body.
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the repository root")
		}
	case kindGitFace:
		// Sparse only (spec section 1): the git index face answers the
		// deprecation wording whatever the verb.
		writeEnvelope(w, http.StatusNotFound, msgGitDeprecated)
	case kindConfig, kindIndexFile:
		// The index plane is read-only to clients: a write verb on a
		// VALID index address is the server-generated 403 (the DB-3
		// posture — a hand-written index line could break the cksum
		// reconciliation), not the generic 405.
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if rt.kind == kindConfig {
				h.serveConfig(w, r, repoKey)
				return
			}
			h.serveIndexFile(ctx, w, r, p, repoKey, rt.pkgPath)
		case http.MethodPut, http.MethodDelete, http.MethodPost:
			writeEnvelope(w, http.StatusForbidden,
				"'"+rel+"' is server-generated (the sparse index); direct writes are not permitted")
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on index files")
		}
	case kindDownload:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveDownload(ctx, w, r, p, repoKey, rt)
	case kindPublish:
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", "PUT")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the publish target")
			return
		}
		h.servePublish(ctx, w, r, p, repoKey)
	case kindSearch:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveSearch(ctx, w, r, p, repoKey)
	case kindYank, kindUnyank:
		want, flag := http.MethodDelete, true
		if rt.kind == kindUnyank {
			want, flag = http.MethodPut, false
		}
		if r.Method != want {
			w.Header().Set("Allow", want)
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the "+strings.TrimPrefix(rel, "api/v1/crates/")+" target")
			return
		}
		h.serveYank(ctx, w, p, repoKey, rt.name, rt.version, flag)
	case kindBareContent:
		h.serveBareContent(ctx, w, r, p, repoKey, rel)
	default:
		writeEnvelope(w, http.StatusNotFound, "not found")
	}
}

// requireMethod renders the 405 (with Allow) unless the method matches;
// false means the request was refused and the caller returns.
func (h *Handler) requireMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, m := range allowed {
		if r.Method == m {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on this route")
	return false
}

// serveDownload answers GET v1/crates/{name}/{version}/download: the
// stored .crate stream. The 404 body is pinned (spec section 2).
func (h *Handler) serveDownload(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	path := cratePath(rt.name, rt.version)
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			writeEnvelope(w, http.StatusNotFound, msgDownload404)
			return
		}
		h.writeError(w, err, repoKey, path)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "application/octet-stream")
}

// serveBareContent is the raw storage face (no protocol plane): GET/HEAD
// stream the node, PUT lands bytes verbatim EXCEPT under the two
// server-owned prefixes (index/ and .cargo/ — a hand-written index line
// or sidecar could break the cksum reconciliation; the DB-3 posture),
// DELETE removes — the curl/debug reachability the other adapters give.
func (h *Handler) serveBareContent(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		ctype := node.Mime
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		h.serveNode(ctx, w, r, node, rc, ctype)
	case http.MethodPut:
		if strings.HasPrefix(rel, segIndex+"/") || strings.HasPrefix(rel, dirMeta+"/") {
			writeEnvelope(w, http.StatusForbidden,
				"'"+rel+"' is server-generated (the sparse index and the cargo metadata sidecar); direct writes are not permitted")
			return
		}
		expect, err := declaredDigests(r.Header)
		if err != nil {
			writeEnvelope(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := h.svc.Put(ctx, p, repoKey, rel, r.Body, expect, "application/octet-stream"); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.Header().Set("Location", rel)
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on content paths")
	}
}

// ---- rendering ----

// writeEnvelope renders one official error body:
// {"errors":[{"detail":"<msg>"}]} (spec section 2).
func writeEnvelope(w http.ResponseWriter, status int, msg string) {
	body, err := json.Marshal(struct {
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}{Errors: []struct {
		Detail string `json:"detail"`
	}{{Detail: msg}}})
	if err != nil {
		// unreachable: a flat string marshals
		body = []byte(`{"errors":[{"detail":"internal error"}]}`)
	}
	writeJSON(w, status, body)
}

// writeJSON renders one JSON document with the shared success posture.
func writeJSON(w http.ResponseWriter, status int, body []byte) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed JSON, never client bytes
}

// writeError maps service errors onto the protocol surface (the goproxy
// matrix under the JSON envelope: one shared mapping across the faces).
func (h *Handler) writeError(w http.ResponseWriter, err error, repoKey, path string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writeEnvelope(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeEnvelope(w, http.StatusConflict, fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, path, err))
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writeEnvelope(w, http.StatusNotFound, "not found")
	case errors.Is(err, repo.ErrRepoNotFound):
		writeEnvelope(w, http.StatusNotFound, fmt.Sprintf("repository %s not found", repoKey))
	case errors.Is(err, repo.ErrInvalidPath), errors.Is(err, errInvalidChecksum), errors.Is(err, errInvalidPackage),
		errors.Is(err, metadata.ErrInvalidProperties):
		writeEnvelope(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeEnvelope(w, http.StatusUnauthorized, "unauthorized user")
	case errors.Is(err, repo.ErrForbidden):
		writeEnvelope(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, repo.ErrQuotaExceeded):
		writeEnvelope(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		writeEnvelope(w, http.StatusInternalServerError, err.Error())
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

// digestTriple and digestsOf: the download-header family (goproxy
// verbatim — sha256 from the node, sha1/md5 from the ledger, degraded).
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

// serveNode streams one stored node: hint merge, checksum headers,
// Content-Length, Content-Type, body suppressed on HEAD.
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
