package helm

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
const Protocol = "helm"

// The content plane's client-checksum family (the shared contract every
// adapter honors on PUT and download; helm clients send none — the family
// serves curl deployments).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// errInvalidChecksum restates the adapter-root sentinel's 400 semantics.
var errInvalidChecksum = adapter.ErrInvalidChecksum

// BlobLedger is the read-only digest ledger the download headers consult
// (satisfied by metadata.Store.Blobs(); the cargo/goproxy seam verbatim).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// NodeProps is the node-property seam the chart facts ride on (the
// protocol's own state: chart.name/chart.version drive the delete-event
// index removal and the duplicate-chart search).
type NodeProps interface {
	List(ctx context.Context, repoKey, path string) (map[string][]string, error)
}

// Options tunes the handler.
type Options struct {
	// BaseURL is the externally visible origin (server.base_url; TL-1).
	// The RELATIVE urls default (HL-2) never cites it — only the reserved
	// absolute mode does.
	BaseURL string
	// AbsoluteURLs is the reserved absolute-urls seat (HL-2 ruled relative
	// the default; the zero value IS the product posture — no config key
	// exposes this).
	AbsoluteURLs bool
	// Now overrides the clock (tests).
	Now func() time.Time
}

// Handler is the classic Helm chart repository adapter. It owns the wire
// protocol only; every content operation goes through repo.Service.
type Handler struct {
	svc      repo.Service
	repos    repo.ClassReader
	blobs    BlobLedger
	props    NodeProps
	opts     Options
	rewrites indexMutexes // per-repoKey serialization of the index rewrites
}

// New wires the handler. props may be nil (a bare fake stack: the delete
// event degrades to the warn-and-skip, the duplicate search to its
// filename arm).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, props: props, opts: opts}
}

// Register builds the handler and enters both the handler registry and
// the metadata-provider registry under one literal (the pypi/cargo
// convention; cmd assembly calls it exactly once).
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, props NodeProps, opts Options) *Handler {
	h := New(svc, repos, blobs, props, opts)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: T-309 serves LOCAL in full — the
// remote pull-through and the virtual aggregation are their own M11
// tickets (T-313 and siblings); the class door refuses them until they
// land. HelmOCI never routes here (HL-3: the docker adapter serves it).
func (h *Handler) RepoTypes() []string { return []string{repo.TypeLocal} }

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

// msgClassNotServed is the not-yet-landed class refusal.
const msgClassNotServed = "helm %s repositories are not served by this BinFlow release (the classic local repository ships here; remote and virtual land with their own tickets)"

// msgExternalLocal is the remote-family refusal on a local repository
// (helm.md section 2: local → 400).
const msgExternalLocal = "external dependency downloads (_external/_transitive) are served by remote and virtual helm repositories only"

// msgIndexServerGenerated is the direct-write refusal on the index (the
// DB-3 posture: a hand-written index could break the digest/urls
// reconciliation).
const msgIndexServerGenerated = "'%s' is server-generated (the chart index); direct writes are not permitted (upload charts with PUT <chart>.tgz)"

// ServeHTTP dispatches on the parsed wire target. Error bodies are PLAIN
// TEXT on this face (helm surfaces the body verbatim; Artifactory's helm
// errors are plain strings — the one pinned wording, the Enforce Layout
// 403s, is plain text by construction).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	rt := parseRoute(rel)
	if rt.kind != kindRoot {
		class, err := h.classOf(ctx, repoKey)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		if class != repo.TypeLocal {
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
	case kindIndex:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveIndex(ctx, w, r, p, repoKey)
		case http.MethodPut, http.MethodDelete, http.MethodPost:
			writeText(w, http.StatusForbidden, fmt.Sprintf(msgIndexServerGenerated, rel))
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	case kindChart:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveStoredFile(ctx, w, r, p, repoKey, rel, "application/x-gzip")
		case http.MethodPut:
			h.serveUploadChart(ctx, w, r, p, repoKey, rel)
		case http.MethodDelete:
			h.serveDeleteChart(ctx, w, p, repoKey, rel)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD, PUT, DELETE")
		}
	case kindTarGz:
		h.servePlainFile(ctx, w, r, p, repoKey, rel, "application/x-gzip")
	case kindProv, kindBareContent:
		ctype := "application/octet-stream"
		if rt.kind == kindProv {
			ctype = "text/plain; charset=utf-8"
		}
		h.servePlainFile(ctx, w, r, p, repoKey, rel, ctype)
	case kindExternal, kindTransitive:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			writeText(w, http.StatusBadRequest, msgExternalLocal)
		default:
			h.methodNotAllowed(w, r, "GET, HEAD")
		}
	default:
		writeText(w, http.StatusNotFound, "not found")
	}
}

// methodNotAllowed renders the 405 with Allow.
func (h *Handler) methodNotAllowed(w http.ResponseWriter, r *http.Request, allow string) {
	w.Header().Set("Allow", allow)
	writeText(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on this route")
}

// serveIndex answers GET index.yaml: the stored repo-root node, text/yaml.
func (h *Handler) serveIndex(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, indexPath)
	if err != nil {
		h.writeError(w, err, repoKey, indexPath)
		return
	}
	h.serveNode(ctx, w, r, node, rc, "text/yaml")
}

// serveStoredFile streams a stored node with the pinned content type.
func (h *Handler) serveStoredFile(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, ctype string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	h.serveNode(ctx, w, r, node, rc, ctype)
}

// servePlainFile is the raw storage face for non-chart files: GET/HEAD
// stream, PUT lands bytes verbatim, DELETE removes.
func (h *Handler) servePlainFile(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, ctype string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.serveStoredFile(ctx, w, r, p, repoKey, rel, ctype)
	case http.MethodPut:
		if rel == dirIndexMeta || strings.HasPrefix(rel, dirIndexMeta+"/") {
			// The virtual-repository cache root (helm.md section 3's
			// .index constant): server-generated metadata the virtual
			// plane (T-313) owns — the DB-3 posture reserves it now.
			writeText(w, http.StatusForbidden,
				"'"+rel+"' is server-generated (the helm virtual index cache); direct writes are not permitted")
			return
		}
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

// serveUploadChart is the PUT <path>.tgz chain (helm.md section 4):
// spool → parse → Enforce Layout → checksum-gated landing with chart.*
// properties → the index read-modify-write → 201.
func (h *Handler) serveUploadChart(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	spoolPath, err := spoolBody(r.Body)
	if err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("spool request body: %v", err))
		return
	}
	defer func() { _ = os.Remove(spoolPath) }()

	arc, parseErr := h.parseSpooledChart(spoolPath)
	if parseErr != nil {
		// Not a refusal on its own (section 4.1 step 3): the package
		// stores, the indexer skips it — unless the policy turns the skip
		// into the 403 below.
		arc = nil
	}

	// The Enforce Layout hook (section 4.3): judged BEFORE any byte lands.
	if err := h.enforceUpload(ctx, p, repoKey, rel, arc); err != nil {
		var policy enforceLayoutError
		if errors.As(err, &policy) {
			writeText(w, http.StatusForbidden, policy.Error())
			return
		}
		h.writeError(w, err, repoKey, rel)
		return
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
		"application/x-gzip", repo.PutOptions{Properties: chartProps(arc, h.now())})
	_ = f.Close()
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}

	// The index step (section 4.1): same name+version replaces its entry;
	// an unparsable chart never indexes (parseErr logged for the operator).
	if _, _, ok := arc.identity(); ok {
		if err := h.withIndexLock(repoKey, func() error {
			return h.indexChartLocked(ctx, p, repoKey, rel, node.Sha256, arc)
		}); err != nil {
			writeText(w, http.StatusInternalServerError, fmt.Sprintf("index update: %v", err))
			return
		}
	} else if parseErr != nil {
		slogWarnUnindexed(ctx, repoKey, rel, parseErr)
	}
	h.writeCreated(w, rel, node)
}

// serveDeleteChart is the DELETE chain: the node's chart.* identity first
// (the removal key), the node, then the index entry (missing properties =
// warn and skip — section 4.1 step 6).
func (h *Handler) serveDeleteChart(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, rel string) {
	name, version, identified := nodeChartIdentity(ctx, h.props, repoKey, rel)
	if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	if !identified {
		slogWarnNoIdentity(ctx, repoKey, rel)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.withIndexLock(repoKey, func() error {
		return h.unindexChartLocked(ctx, p, repoKey, name, version)
	}); err != nil {
		writeText(w, http.StatusInternalServerError, fmt.Sprintf("index update: %v", err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readChartArchive opens one stored .tgz node and parses it.
func (h *Handler) readChartArchive(ctx context.Context, p *repo.Principal, repoKey, path string) (*chartArchive, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	return parseChartArchive(rc)
}

// parseSpooledChart parses the spooled upload body.
func (h *Handler) parseSpooledChart(spoolPath string) (*chartArchive, error) {
	f, err := os.Open(spoolPath) //nolint:gosec // G304: our own os.CreateTemp path, never client input
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fd
	return parseChartArchive(f)
}

// spoolBody drains the request body into a temp file (charts reach tens
// of megabytes; the parse and the landing both re-read it).
func spoolBody(body io.Reader) (string, error) {
	f, err := os.CreateTemp("", "binflow-helm-*.tgz")
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

// writeText renders one plain-text body (the helm face's error family).
func writeText(w http.ResponseWriter, status int, msg string) {
	body := msg + "\n"
	hdr := w.Header()
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body) //nolint:gosec // G705: server-computed text, never client bytes
}

// writeError maps service errors onto the protocol surface.
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
	case errors.As(err, new(enforceLayoutError)):
		writeText(w, http.StatusForbidden, err.Error())
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
// node, sha1/md5 from the ledger, degraded — the cargo posture).
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
// helm sends none, curl deployments may; malformed → 400).
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
	sha256, err := parse(hdrChecksumSha256, 64)
	if err != nil {
		return storage.BlobRef{}, err
	}
	sha1, err := parse(hdrChecksumSha1, 40)
	if err != nil {
		return storage.BlobRef{}, err
	}
	md5, err := parse(hdrChecksumMd5, 32)
	if err != nil {
		return storage.BlobRef{}, err
	}
	return storage.BlobRef{Sha256: sha256, Sha1: sha1, Md5: md5}, nil
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

// blobRefOf renders one in-memory body's digest facts (the index write
// and the plain landings' server-measured arm).
func blobRefOf(body []byte) storage.BlobRef {
	sum := sha256.Sum256(body)
	return storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}
}

// slogWarnUnindexed logs the skipped-indexing shape (section 4.1 step 3).
func slogWarnUnindexed(ctx context.Context, repoKey, rel string, cause error) {
	slog.WarnContext(ctx, "helm: chart stored without indexing (unparsable archive)",
		slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", cause.Error()))
}

// slogWarnNoIdentity logs the delete event's missing-identity skip
// (section 4.1 step 6).
func slogWarnNoIdentity(ctx context.Context, repoKey, rel string) {
	slog.WarnContext(ctx, "helm: deleted chart carried no chart.name/chart.version properties; index entry kept",
		slog.String("repo", repoKey), slog.String("path", rel))
}
