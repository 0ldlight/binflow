package deb

// The Debian adapter's content plane (debian.md sections 1 / 3 / 6): a
// pure storage-path protocol — apt GETs repo-relative paths verbatim.
// This handler owns the wire behavior only; every content operation goes
// through repo.Service.
//
// The debPUT chain (section 3) is the write face: a .deb/.dsc lands at
// any path with its <dist>/<component>/<arch> coordinates in the matrix
// parameters, the coordinates register as deb.*/dsc.* node properties,
// and the PUT answers 201 immediately — the index recompute runs in the
// background (section 3 step 3, the async work posture). The board's
// DB-2 ruling tightens Artifactory's silence: a .deb without the full
// coordinate triple is a 400, never a silently-unindexed store. DB-3
// closes the index family under dists/ to client writes: those files are
// the debPUT chain's OUTPUT (403 with the pinned posture), not a second
// input path competing with the engine.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves — the
// repositories.package_type value httpapi dispatches on (the Artifactory
// spelling; the /api/deb management face keeps the deb short form).
const Protocol = "debian"

// The content plane's client-checksum family (the shared contract every
// adapter honors on PUT and download; apt itself sends none — the family
// serves curl deployments).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// errInvalidChecksum restates the adapter-root sentinel's 400 semantics.
var errInvalidChecksum = adapter.ErrInvalidChecksum

// BlobLedger is the read-only digest ledger the index engine and the
// download headers consult (satisfied by metadata.Store.Blobs()).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// NodeProps is the node-property seam the coordinate registration rides
// on (satisfied by metadata.Store.NodeProps(); nil disables the
// coordinate-aware indexing — uploads still store, the reindex treats
// every package as unregistered).
type NodeProps interface {
	List(ctx context.Context, repoKey, path string) (map[string][]string, error)
}

// Options tunes the handler.
type Options struct {
	// Now overrides the clock (tests — the Release Date field and the
	// by-hash retention order key on it).
	Now func() time.Time
}

// Handler is the Debian adapter. It owns the wire protocol only.
type Handler struct {
	svc      repo.Service
	repos    repo.ClassReader
	blobs    BlobLedger
	props    NodeProps
	opts     Options
	rewrites indexMutexes // per-repoKey serialization of the index rewrites
}

// New wires the handler. props may be nil (see NodeProps).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, props: props, opts: opts}
}

// Register builds the handler and enters both the handler registry and
// the metadata-provider registry under one literal (the pypi/cargo/rpm
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	h := New(svc, repos, blobs, props, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: the content plane serves all
// three classes — LOCAL in full (T-310), REMOTE as the pull-through
// proxy mirror (T-314, remote.go: every read rides repo.Service's
// FR-20 engine, writes refuse), VIRTUAL as the stanza-aggregated index
// face (T-314, virtual.go: Packages/Sources merged across members with
// the Release recomputed at the virtual root, downloads first-found,
// debPUT routed).
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler (see layout.go — the debPUT matrix
// coordinates ride the shared peel, so the adapter contract's two-value
// form discards them; the handler itself resolves the full form).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	key, rel, _, err := layout(r)
	return key, rel, err
}

// classOf resolves the repository class (the rpm defensive arm).
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

// msgClassNotServed is the unserved-class refusal (the defensive arm for
// a class value outside the three this adapter serves).
const msgClassNotServed = "debian %s repositories are not served by this BinFlow release (the local automatic pipeline, the remote pull-through mirror and the virtual stanza aggregation are the served faces)"

// msgServerGenerated is the direct-write refusal on the generated family
// (DB-3's posture: a hand-written index would break the checksum chain
// apt verifies, and would compete with the automatic recompute).
const msgServerGenerated = "'%s' is server-generated (the debian repository index); direct writes are not permitted (the index is generated by the debPUT chain — PUT <path>.deb;deb.distribution=<dist>;deb.component=<comp>;deb.architecture=<arch> — or trigger a recompute through the /binflow/api/deb/reindex family)"

// msgMissingCoordinates is DB-2's refusal (the board-confirmed tightening
// of Artifactory's silently-store posture).
const msgMissingCoordinates = "uploading a .deb to an automatic debian repository requires the distribution, component and architecture matrix parameters (PUT <path>.deb;deb.distribution=<dist>;deb.component=<comp>;deb.architecture=<arch>; the same keys with the dsc. prefix serve .dsc uploads)"

// msgNotADeb is the malformed-package refusal on the .dsc face (a .dsc
// whose body does not carry a source paragraph cannot index; the .deb
// face stays store-and-warn per the rpm parity).
const msgNotADsc = "the .dsc body does not carry a parseable source paragraph (Source/Version fields are required)"

// ServeHTTP dispatches on the parsed wire target and the repository
// CLASS (local / remote / virtual — each class's face lives in its own
// file). Error bodies are PLAIN TEXT on this face (the storage-path
// family's posture; apt only consumes the status).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, props, err := layout(r)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()

	rt := parseRoute(rel)
	class := repo.TypeLocal
	if rt.kind != kindRoot {
		class, err = h.classOf(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
	}
	switch class {
	case repo.TypeRemote:
		h.serveRemote(ctx, w, r, repoKey, rel, rt)
		return
	case repo.TypeVirtual:
		h.serveVirtual(ctx, w, r, repoKey, rel, rt, props)
		return
	case repo.TypeLocal:
	default:
		writeText(w, http.StatusNotFound, fmt.Sprintf(msgClassNotServed, class))
		return
	}

	switch rt.kind {
	case kindRoot:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	case kindDeb:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "application/vnd.debian.binary-package")
		case http.MethodPut:
			h.serveUploadDeb(ctx, w, r, repoKey, rel, props, repoKey)
		case http.MethodDelete:
			h.serveDeleteTracked(ctx, w, repoKey, rel, "deb")
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindDsc:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "text/plain; charset=utf-8")
		case http.MethodPut:
			h.serveUploadDsc(ctx, w, r, repoKey, rel, props, repoKey)
		case http.MethodDelete:
			h.serveDeleteTracked(ctx, w, repoKey, rel, "dsc")
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindIndex:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, indexContentType(rel))
		default:
			// DB-3: the generated family is the engine's output — PUT and
			// DELETE alike answer the 403 (a client delete would desync
			// Release from the indexes exactly as a write would).
			writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
		}
	case kindDists, kindBare:
		h.servePlainFile(ctx, w, r, repoKey, rel)
	default:
		writeText(w, http.StatusNotFound, "not found")
	}
}

// indexContentType pins the served index family's types.
func indexContentType(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".gz"):
		return "application/gzip"
	case strings.HasSuffix(rel, ".bz2"):
		return "application/x-bzip2"
	case strings.HasSuffix(rel, ".xz"):
		return "application/x-xz"
	case strings.HasSuffix(rel, ".lzma"):
		return "application/x-lzma"
	default:
		return "text/plain; charset=utf-8"
	}
}

// methodNotAllowed renders the 405 with Allow.
func (h *Handler) methodNotAllowed(w http.ResponseWriter, r *http.Request, allow string) {
	w.Header().Set("Allow", allow)
	writeText(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on this route")
}

// serveStoredFile streams a stored node with the pinned content type.
func (h *Handler) serveStoredFile(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel, ctype string) {
	p := adapter.PrincipalFrom(ctx)
	rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	h.serveNode(ctx, w, r, node, rc, ctype)
}

// servePlainFile is the raw storage face for the non-generated families
// (pool trees, companion tarballs, the non-index paths under dists/):
// GET/HEAD stream, PUT lands bytes verbatim, DELETE removes. Matrix
// deploy properties still ride the PUT (the shared T-286 seam).
func (h *Handler) servePlainFile(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel string) {
	p := adapter.PrincipalFrom(ctx)
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveStoredFile(ctx, w, r, repoKey, rel, "application/octet-stream")
	case http.MethodPut:
		expect, err := declaredDigests(r.Header)
		if err != nil {
			writeText(w, http.StatusBadRequest, err.Error())
			return
		}
		node, err := h.svc.Put(ctx, p, repoKey, rel, r.Body, expect, "application/octet-stream")
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		h.writeCreated(w, rel, node)
	case http.MethodDelete:
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
	}
}

// serveUploadDeb is the debPUT binary chain (section 3): coordinate
// validation (DB-2) → spool → parse the control paragraph (best effort —
// an unparsable body still stores, the indexer skips it with a WARN, the
// rpm parity) → land with the coordinate properties → the background
// recompute of the affected distributions → 201 (never blocked on the
// index). recomputeRepo is the repository whose index the recompute
// targets: the addressed key on the local class, the write-routed MEMBER
// on the virtual class (the service routes the landing itself; the index
// belongs to the member).
func (h *Handler) serveUploadDeb(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel string, props adapter.DeployProps, recomputeRepo string) {
	coords := debCoordinates(props)
	if !coords.complete() {
		writeText(w, http.StatusBadRequest, msgMissingCoordinates)
		return
	}
	if err := coords.validate(); err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	dists := coords.distributions

	spoolPath, err := spoolBody(r.Body)
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("spool request body: %v", err))
		return
	}
	defer func() { _ = os.Remove(spoolPath) }()

	f, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("reopen spool: %v", err))
		return
	}
	doc, parseErr := parseDebControl(f)
	_ = f.Close()
	if parseErr != nil {
		slog.WarnContext(ctx, "deb: package stored without control parsing (not a .deb body?)",
			slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", parseErr.Error()))
	}

	uploadProps := coords.props("deb")
	if doc != nil {
		for k, v := range debFactProps(doc) {
			uploadProps[k] = v
		}
	}

	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err = os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("reopen spool: %v", err))
		return
	}
	node, err := h.svc.PutWithOptions(ctx, adapter.PrincipalFrom(ctx), repoKey, rel, f, expect,
		"application/vnd.debian.binary-package", repo.PutOptions{Properties: uploadProps})
	_ = f.Close()
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	p := adapter.PrincipalFrom(ctx)
	h.recomputeDists(ctx, p, recomputeRepo, dists)
	h.writeCreated(w, rel, node)
}

// serveUploadDsc is the debPUT source chain: the dsc.* coordinates (the
// architecture axis is the server's — source), a body that must carry a
// parseable source paragraph (a .dsc is the index's only input; a broken
// one is refused, unlike the .deb's store-and-warn), the same property
// registration and background recompute. recomputeRepo carries the
// virtual-class write route (see serveUploadDeb).
func (h *Handler) serveUploadDsc(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel string, props adapter.DeployProps, recomputeRepo string) {
	coords := dscCoordinates(props)
	if !coords.complete() {
		writeText(w, http.StatusBadRequest, msgMissingCoordinates)
		return
	}
	if err := coords.validate(); err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	dists := coords.distributions

	body, err := io.ReadAll(io.LimitReader(r.Body, maxControlBytes))
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("read request body: %v", err))
		return
	}
	doc, parseErr := parseDsc(strings.NewReader(string(body)))
	if parseErr != nil || doc == nil || doc.Get("Source") == "" || doc.Get("Version") == "" {
		writeText(w, http.StatusBadRequest, msgNotADsc)
		return
	}

	uploadProps := coords.props("dsc")
	for k, v := range debFactProps(doc) {
		uploadProps[k] = v
	}
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	node, err := h.svc.PutWithOptions(ctx, adapter.PrincipalFrom(ctx), repoKey, rel,
		strings.NewReader(string(body)), expect, "text/plain; charset=utf-8",
		repo.PutOptions{Properties: uploadProps})
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	p := adapter.PrincipalFrom(ctx)
	h.recomputeDists(ctx, p, recomputeRepo, dists)
	h.writeCreated(w, rel, node)
}

// serveDeleteTracked is the .deb/.dsc DELETE chain: read the stored
// coordinates (the recompute's scope), remove the node, recompute the
// affected distributions in the background.
func (h *Handler) serveDeleteTracked(ctx context.Context, w http.ResponseWriter, repoKey, rel, prefix string) {
	p := adapter.PrincipalFrom(ctx)
	var dists []string
	if raw := h.propsOf(ctx, repoKey, rel); len(raw) > 0 {
		dists = raw[prefix+".distribution"]
	}
	if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	h.recomputeDists(ctx, p, repoKey, dists)
	w.WriteHeader(http.StatusNoContent)
}

// debFactProps renders the parsed control facts worth persisting as
// node properties (the deb.metadata.* family — the same index-facts
// registration the rpm plane pins; empty fields stay unwritten).
func debFactProps(doc *controlDoc) map[string][]string {
	if doc == nil {
		return nil
	}
	props := map[string][]string{}
	for key, field := range map[string]string{
		"deb.metadata.package":      "Package",
		"deb.metadata.version":      "Version",
		"deb.metadata.architecture": "Architecture",
		"deb.metadata.source":       "Source",
	} {
		if v := doc.Get(field); v != "" {
			props[key] = []string{v}
		}
	}
	return props
}

// spoolBody drains the request body into a temp file (packages reach tens
// of megabytes; the parse and the landing both re-read it).
func spoolBody(body io.Reader) (string, error) {
	f, err := os.CreateTemp("", "binflow-deb-*.deb")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// writeCreated renders the 201 with the Location and checksum headers.
func (h *Handler) writeCreated(w http.ResponseWriter, rel string, node *metadata.Node) {
	if node != nil && node.Sha256 != "" {
		w.Header().Set(hdrChecksumSha256, node.Sha256)
	}
	w.Header().Set("Location", rel)
	w.WriteHeader(http.StatusCreated)
}

// ---- rendering ----

// writeText renders one plain-text body (the storage-path face's family).
func writeText(w http.ResponseWriter, status int, msg string) {
	body := msg + "\n"
	hdr := w.Header()
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: server-computed text, never client bytes
}

// writeError maps service errors onto the protocol surface (the rpm
// mapping verbatim — the shared service seam's error family).
func (h *Handler) writeError(w http.ResponseWriter, err error, repoKey, path string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writeText(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeText(w, http.StatusConflict, fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, path, err))
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", repoKey, path))
	case errors.Is(err, repo.ErrRepoNotFound), errors.Is(err, metadata.ErrRepoNotFound):
		writeText(w, http.StatusNotFound, fmt.Sprintf("repository %s not found", repoKey))
	case errors.Is(err, repo.ErrInvalidPath), errors.Is(err, errInvalidChecksum):
		writeText(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeText(w, http.StatusUnauthorized, "unauthorized user")
	case errors.Is(err, repo.ErrForbidden):
		writeText(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, repo.ErrQuotaExceeded):
		writeText(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		writeText(w, http.StatusInternalServerError, err.Error())
	}
}

// digestTriple and digestsOf: the download-header family (sha256 from the
// node, sha1/md5 from the ledger, degraded — the helm/rpm posture).
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

// declaredDigests parses the X-Checksum-* family (the shared contract:
// apt sends none, curl deployments may; malformed → 400).
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				errInvalidChecksum, name, width, v)
		}
		return v, nil
	}
	sha256v, err := parse(hdrChecksumSha256, 64)
	if err != nil {
		return storage.BlobRef{}, err
	}
	sha1v, err := parse(hdrChecksumSha1, 40)
	if err != nil {
		return storage.BlobRef{}, err
	}
	md5v, err := parse(hdrChecksumMd5, 32)
	if err != nil {
		return storage.BlobRef{}, err
	}
	return storage.BlobRef{Sha256: sha256v, Sha1: sha1v, Md5: md5v}, nil
}

// isHex checks one lowercase hex string of the exact width.
func isHex(v string, width int) bool {
	if len(v) != width {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
