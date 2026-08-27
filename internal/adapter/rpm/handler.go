package rpm

// The RPM/YUM adapter's content plane (rpm.md sections 1 / 3.1): a pure
// storage-path protocol — dnf GETs repo-relative paths verbatim, no API
// mount of its own. This handler owns the wire behavior only; every
// content operation goes through repo.Service.
//
// RP-2's final ruling shapes the PUT chain: calculateYumMetadata defaults
// FALSE (the Artifactory posture the board confirmed) — an upload STORES
// (plus the rpm.metadata.* property registration), and the repodata only
// recomputes when the repository explicitly opts in or the /api/yum
// reindex family fires.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
// repositories.package_type value httpapi dispatches on.
const Protocol = "rpm"

// The content plane's client-checksum family (the shared contract every
// adapter honors on PUT and download; dnf itself sends none — the family
// serves curl deployments).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// errInvalidChecksum restates the adapter-root sentinel's 400 semantics.
var errInvalidChecksum = adapter.ErrInvalidChecksum

// msgChecksumNotDownloadable is the pinned sidecar refusal (rpm.md
// section 3.1).
const msgChecksumNotDownloadable = "Checksums are not downloadable."

// BlobLedger is the read-only digest ledger the download headers consult
// (satisfied by metadata.Store.Blobs(); the helm/cargo seam verbatim).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// Options tunes the handler.
type Options struct {
	// DataDir roots the .rpmcache parse cache (rpm.md section 2.4); ""
	// disables the cache (parse on demand).
	DataDir string
	// AggTTL overrides the virtual aggregate cache's short TTL (RP-3;
	// tests shrink it). 0 keeps the 30s default.
	AggTTL time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Handler is the RPM adapter. It owns the wire protocol only.
type Handler struct {
	svc      repo.Service
	repos    repo.ClassReader
	blobs    BlobLedger
	props    NodeProps
	cache    *rpmCache
	opts     Options
	rewrites indexMutexes // per-repoKey serialization of the repodata rewrites
	aggs     virtualAggs  // the virtual aggregates' in-process cache (RP-3)
}

// New wires the handler (the T-311 seam, kept compiling for the cmd
// assembly until the T-315 wiring lands: props nil means the remote .rpm
// property backfill degrades to its skip).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, opts Options) *Handler {
	return NewWithProps(svc, repos, blobs, nil, opts)
}

// NewWithProps is New with the node-property seam the remote backfill
// writes through (cmd assembly's T-315 call — the one-line diff the
// ticket report hands the conductor).
func NewWithProps(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, props: props, cache: newRpmCache(opts.DataDir), opts: opts}
}

// Register builds the handler and enters both the handler registry and
// the metadata-provider registry under one literal (the pypi/cargo
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, opts Options) *Handler {
	return RegisterWithProps(svc, repos, blobs, nil, opts)
}

// RegisterWithProps is Register with the node-property seam (cmd's T-315
// wiring).
func RegisterWithProps(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	h := NewWithProps(svc, repos, blobs, props, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: LOCAL in full (T-311), REMOTE as
// the pull-through mirror with the expirable-set classification and the
// property backfill (T-315, remote.go), VIRTUAL as the aggregated
// repodata plus the first-hit member downloads (T-315, virtual.go).
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

// msgClassNotServed is the unserved-class refusal (the defensive arm for
// a class value outside the three this adapter serves).
const msgClassNotServed = "rpm %s repositories are not served by this BinFlow release (the local, remote and virtual faces ship here)"

// msgServerGenerated is the direct-write refusal on the generated family
// (the DB-3 posture this plane borrows: a hand-written index could break
// the checksum chain dnf verifies).
const msgServerGenerated = "'%s' is server-generated (the yum repository index); direct writes are not permitted (upload packages with PUT <path>.rpm or trigger reindex through the /binflow/api/yum family)"

// msgStagingReserved refuses client writes into the reindex staging area.
const msgStagingReserved = "'%s' is the rpm reindex staging area; direct writes are not permitted"

// ServeHTTP dispatches on the parsed wire target and the repository CLASS
// (T-315: local in full, remote the pull-through mirror, virtual the
// aggregated repodata). Error bodies are PLAIN TEXT on this face (the
// storage-path family's posture; dnf only consumes the status).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	rt := parseRoute(rel)
	var class string
	if rt.kind != kindRoot {
		class, err = h.classOf(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		switch class {
		case repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual:
		default:
			writeText(w, http.StatusNotFound, fmt.Sprintf(msgClassNotServed, class))
			return
		}
	}

	switch rt.kind {
	case kindRoot:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	case kindRpm:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			if class == repo.TypeRemote {
				h.serveRemoteRpm(ctx, w, r, p, repoKey, rel)
				return
			}
			// Local and virtual: the local node / the first-hit member
			// resolution are the same svc.Get read.
			h.serveStoredFile(ctx, w, r, repoKey, rel, "application/x-rpm")
		case http.MethodPut:
			if class == repo.TypeRemote {
				h.serveRemoteWrite(ctx, w, r, p, repoKey, rel, "application/x-rpm")
				return
			}
			// Virtual routes onto the deployment member inside the
			// service (an un-routed virtual answers the C5 405 there).
			h.serveUploadRpm(ctx, w, r, repoKey, rel)
		case http.MethodDelete:
			h.serveDeleteRpm(w, r, p, repoKey, rel, class)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindSidecar:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			writeText(w, http.StatusNotFound, msgChecksumNotDownloadable)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	case kindRepomd:
		h.serveRepomdFace(ctx, w, r, p, repoKey, rel, class)
	case kindIndex:
		h.serveIndexFace(ctx, w, r, p, repoKey, rel, class)
	case kindGroup:
		h.servePlainFile(ctx, w, r, repoKey, rel, "text/xml")
	case kindRepodata:
		h.servePlainFile(ctx, w, r, repoKey, rel, "application/octet-stream")
	case kindTmp:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, repoKey, rel, "application/octet-stream")
		default:
			writeText(w, http.StatusForbidden, fmt.Sprintf(msgStagingReserved, rel))
		}
	case kindBare:
		h.servePlainFile(ctx, w, r, repoKey, rel, "application/octet-stream")
	default:
		writeText(w, http.StatusNotFound, "not found")
	}
}

// serveRepomdFace serves the metadata three-piece's GET/HEAD and refuses
// its writes: local and virtual keep the server-generated 403 (a virtual
// write into a member's repodata could poison the member's index); remote
// routes onto the service's uniform write arms (PUT the RE-05 405, DELETE
// the RE-06 eviction). The virtual repomd.xml itself is the AGGREGATE
// (virtual.go); the signature pair stays unsigned (RP-3) — a member's
// signature would not verify against the merged document.
func (h *Handler) serveRepomdFace(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, class string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if class == repo.TypeVirtual {
			root, tail, _ := splitRepodata(rel)
			if tail == "repomd.xml" {
				h.serveVirtualRepomd(ctx, w, r, repoKey, root, rel)
				return
			}
			writeText(w, http.StatusNotFound, fmt.Sprintf(msgVirtualUnsigned, repoKey))
			return
		}
		h.serveStoredFile(ctx, w, r, repoKey, rel, repomdContentType(rel))
	case http.MethodPut:
		if class == repo.TypeRemote {
			h.serveRemoteWrite(ctx, w, r, p, repoKey, rel, repomdContentType(rel))
			return
		}
		writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
	case http.MethodDelete:
		if class == repo.TypeRemote {
			h.serveRemoteEvict(ctx, w, p, repoKey, rel)
			return
		}
		writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
	default:
		h.methodNotAllowed(w, r, "GET, HEAD")
	}
}

// serveIndexFace serves the digest-prefixed index family: the virtual
// consults the aggregate's own files first (its digests name merged
// bodies no member carries); local and remote stream the stored node (a
// remote read IS the engine's pull-through with the artifact-semantics
// TTL the classification gives these immutable-by-digest files).
func (h *Handler) serveIndexFace(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, class string) {
	ctype := "application/gzip"
	if strings.HasSuffix(rel, ".sqlite.bz2") {
		ctype = "application/x-bzip2"
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if class == repo.TypeVirtual {
			root, _, _ := splitRepodata(rel)
			h.serveVirtualIndex(ctx, w, r, repoKey, root, rel, ctype)
			return
		}
		h.serveStoredFile(ctx, w, r, repoKey, rel, ctype)
	case http.MethodPut:
		if class == repo.TypeRemote {
			h.serveRemoteWrite(ctx, w, r, p, repoKey, rel, ctype)
			return
		}
		writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
	case http.MethodDelete:
		if class == repo.TypeRemote {
			h.serveRemoteEvict(ctx, w, p, repoKey, rel)
			return
		}
		writeText(w, http.StatusForbidden, fmt.Sprintf(msgServerGenerated, rel))
	default:
		h.methodNotAllowed(w, r, "GET, HEAD")
	}
}

// repomdContentType pins the metadata three-piece's types.
func repomdContentType(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".asc"), strings.HasSuffix(rel, ".key"):
		return "text/plain; charset=utf-8"
	default:
		return "text/xml"
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

// servePlainFile is the raw storage face for the non-generated families:
// GET/HEAD stream, PUT lands bytes verbatim, DELETE removes.
func (h *Handler) servePlainFile(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel, ctype string) {
	p := adapter.PrincipalFrom(ctx)
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveStoredFile(ctx, w, r, repoKey, rel, ctype)
	case http.MethodPut:
		expect, err := declaredDigests(r.Header)
		if err != nil {
			writeText(w, http.StatusBadRequest, err.Error())
			return
		}
		node, err := h.svc.Put(ctx, p, repoKey, rel, r.Body, expect, ctype)
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

// serveUploadRpm is the PUT <path>.rpm chain: spool → parse the header →
// store with the rpm.metadata.* property set → (opt-in) the background
// repodata recompute. A body that fails to parse still stores (section 5
// step 3: the PUT is never refused on header grounds).
func (h *Handler) serveUploadRpm(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, rel string) {
	p := adapter.PrincipalFrom(ctx)
	spoolPath, err := spoolBody(r.Body)
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("spool request body: %v", err))
		return
	}
	defer func() { _ = os.Remove(spoolPath) }()

	var hdr *Header
	if parseErr := func() error {
		f, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd
		hdr, err = ParseHeader(f)
		return err
	}(); parseErr != nil {
		hdr = nil
		slog.WarnContext(ctx, "rpm: package stored without header parsing (not an RPM body?)",
			slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", parseErr.Error()))
	}

	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	f, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("reopen spool: %v", err))
		return
	}
	node, err := h.svc.PutWithOptions(ctx, p, repoKey, rel, f, expect,
		"application/x-rpm", repo.PutOptions{Properties: rpmProps(hdr)})
	_ = f.Close()
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}

	// The parse cache and the opt-in recompute (RP-2) target the
	// repository that HOLDS the package — a virtual PUT's bytes land in
	// the routed deployment member (node.RepoKey), and the recompute
	// decision is the MEMBER's own configuration, never the virtual's.
	landKey := repoKey
	if node != nil && node.RepoKey != "" {
		landKey = node.RepoKey
	}
	if hdr != nil {
		h.cache.store(landKey, rel, node.Sha256, node.Size, hdr)
		h.maybeRecompute(ctx, p, landKey, rel)
	}
	h.writeCreated(w, rel, node)
}

// serveDeleteRpm is the DELETE chain. The service owns the class posture
// verbatim: local removes and recomputes (the chain below), remote is the
// RE-06 cache eviction, virtual the RE-08 never-propagates 405 — the
// local-only steps (parse-cache drop, recompute trigger) run on the local
// class alone.
func (h *Handler) serveDeleteRpm(w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, class string) {
	ctx := r.Context()
	if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	if class == repo.TypeLocal {
		h.cache.remove(repoKey, rel)
		h.maybeRecompute(ctx, p, repoKey, rel)
	}
	w.WriteHeader(http.StatusNoContent)
}

// maybeRecompute fires the background single-root recompute when the
// repository opted in (RP-2: the default is OFF).
func (h *Handler) maybeRecompute(ctx context.Context, p *repo.Principal, repoKey, rel string) {
	cfg, err := h.configFor(ctx, repoKey)
	if err != nil {
		slog.WarnContext(ctx, "rpm: recompute trigger skipped (config unreadable)",
			slog.String("repo", repoKey), slog.String("error", err.Error()))
		return
	}
	if !cfg.CalculateYumMetadata {
		return
	}
	root, ok := yumRootOf(rel, cfg.YumRootDepth)
	if !ok {
		return // shallower than yumRootDepth: section 4.1's depth filter
	}
	principal := p
	//nolint:gosec // G118: the recompute deliberately detaches from the
	// request's lifetime — the async posture must survive the client
	// hanging up, and the per-repo index lock serializes runs.
	go h.recomputeRoot(context.Background(), principal, repoKey, root)
}

// rpmProps builds the rpm.metadata.* property set (rpm.md section 2.4's
// nine attributes; empty header fields stay unwritten).
func rpmProps(hdr *Header) map[string][]string {
	if hdr == nil {
		return nil
	}
	props := map[string][]string{
		"rpm.metadata.name":    {hdr.Name},
		"rpm.metadata.version": {hdr.Version},
		"rpm.metadata.release": {hdr.Release},
		"rpm.metadata.arch":    {hdr.Arch},
	}
	if hdr.Epoch != "" {
		props["rpm.metadata.epoch"] = []string{hdr.Epoch}
	}
	for key, v := range map[string]string{
		"rpm.metadata.license": hdr.License,
		"rpm.metadata.group":   hdr.Group,
		"rpm.metadata.vendor":  hdr.Vendor,
		"rpm.metadata.summary": hdr.Summary,
	} {
		if v != "" {
			props[key] = []string{v}
		}
	}
	return props
}

// spoolBody drains the request body into a temp file (packages reach tens
// of megabytes; the parse and the landing both re-read it).
func spoolBody(body io.Reader) (string, error) {
	f, err := os.CreateTemp("", "binflow-rpm-*.rpm")
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

// writeError maps service errors onto the protocol surface (the helm
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
// node, sha1/md5 from the ledger, degraded — the helm/cargo posture).
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
// dnf sends none, curl deployments may; malformed → 400).
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

// blobRefOf renders one in-memory body's digest facts (the index writes'
// server-measured arm).
func blobRefOf(body []byte) storage.BlobRef {
	sum := sha256.Sum256(body)
	return storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}
}
