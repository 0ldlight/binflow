package goproxy

import (
	"context"
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
// repositories.package_type value httpapi dispatches on. It is "go", not
// "goproxy" (architecture section 15.2.4; the Go package name avoids the
// keyword).
const Protocol = "go"

// hdrChecksum* are the content plane's client-checksum family (repo-semantics
// section 5): declared on PUT (malformed -> 400, disagreement -> 409) and
// echoed with the measured values on download.
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
)

// msgNotFound is the plain not-found body (goproxy.md section 4.4; the go
// command only inspects the STATUS — 404/410 let it fall through to the next
// GOPROXY source — but the body stays the conventional lowercase wording).
const msgNotFound = "not found"

// BlobLedger is the read-only digest ledger the adapter consults for the
// sha1/md5 download headers (the same consumer-side seam generic and pypi
// use; satisfied by metadata.Store.Blobs()).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// Handler is the GOPROXY protocol adapter. It owns the wire protocol only;
// every content operation goes through repo.Service — including the remote
// pull-through (svc.Get dispatches to the internal/remote engine, whose
// upstream hop applies this protocol's re-escaping through the
// metadata-provider facet in provider.go).
type Handler struct {
	svc   repo.Service
	repos repo.ClassReader
	blobs BlobLedger
}

// New wires the handler. svc is the content use-case service; repos is the
// class-lookup seam (the list/latest strategies and the +incompatible .mod
// rule are class-dependent); blobs serves the sha1/md5 download headers.
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs}
}

// Register builds the handler and enters both the handler registry and the
// metadata-provider registry under one literal — the pypi.Register
// convention. cmd assembly calls it exactly once.
func Register(svc repo.Service, repos repo.ClassReader, blobs BlobLedger) *Handler {
	h := New(svc, repos, blobs)
	adapter.Register(h)
	RegisterMetadata()
	return h
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: the pilot type serves all three
// classes (T-283's dynamic legal set has no class ruling for it) — local in
// full, remote through the pull-through engine inside svc.Get, virtual
// through the first-found resolver plus this adapter's aggregation face.
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler (see layout.go).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	// Indirection through the shared parser keeps signature parity with the
	// other adapters; the body lives in layout.go next to the wire shapes.
	return layout(r)
}

// classOf resolves the repository class. Unknown keys answer the protocol's
// 404 (the router already refuses unknown repos before dispatch, so this is
// the defensive arm for direct mounts).
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

// ServeHTTP dispatches on the parsed wire target. Errors render as
// text/plain bodies with the protocol's status semantics (go.dev/ref/mod:
// error responses carry Content-Type text/plain — the errors[] JSON envelope
// is deliberately NOT used on this plane; the go command never parses it and
// the official spec fixes the plain form).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writePlain(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	// The repository-root probe (goproxy.md section 2): 200 with an empty
	// body — the go client never calls it, humans and monitors do.
	if rel == "" {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the repository root")
		}
		return
	}

	t, ok := parseTarget(rel)
	if !ok {
		writePlain(w, http.StatusNotFound, msgNotFound)
		return
	}
	class, err := h.classOf(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}

	switch t.kind {
	case kindList:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on @v/list")
			return
		}
		h.serveList(ctx, w, r, p, repoKey, class, t)
	case kindLatest:
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on @latest")
			return
		}
		h.serveLatest(ctx, w, r, p, repoKey, class, t)
	case kindFile:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveFile(ctx, w, r, p, repoKey, class, t)
		case http.MethodPut:
			h.servePut(ctx, w, r, p, repoKey, class, t)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT")
			writePlain(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on version files")
		}
	default:
		writePlain(w, http.StatusNotFound, msgNotFound)
	}
}

// writePlain renders one protocol error: text/plain, trailing newline. The
// nosniff guard keeps browsers from content-sniffing these bodies as HTML
// (the httpapi writePlainText posture — several messages interpolate
// client-supplied identifiers).
func writePlain(w http.ResponseWriter, status int, msg string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg + "\n")) //nolint:gosec // G705: protocol error text under nosniff + text/plain, the go client's fixed error form
}

// errRepoNotFound shapes the missing-repository refusal.
func errRepoNotFound(repoKey string) error {
	return fmt.Errorf("repository %s: %w", repoKey, repo.ErrRepoNotFound)
}

// writeError maps service errors onto the protocol surface. A
// *repo.StatusError renders verbatim (the remote engine's fault matrix and
// the virtual 405 own their statuses); the unfound family is the plain 404;
// checksum mismatches are 409; everything the validation chain rejected
// upstream is a 400.
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
		writePlain(w, http.StatusConflict, checksumMismatchMessage(err, repoKey, path))
	case errors.Is(err, repo.ErrNodeNotFound), errors.Is(err, repo.ErrIsFolder):
		writePlain(w, http.StatusNotFound, msgNotFound)
	case errors.Is(err, repo.ErrRepoNotFound):
		writePlain(w, http.StatusNotFound, fmt.Sprintf("repository %s not found", repoKey))
	case errors.Is(err, repo.ErrInvalidPath), errors.Is(err, adapter.ErrInvalidChecksum):
		writePlain(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(w, http.StatusUnauthorized, "authentication required")
	case errors.Is(err, repo.ErrForbidden):
		writePlain(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrQuotaExceeded):
		writePlain(w, http.StatusRequestEntityTooLarge, err.Error())
	default:
		writePlain(w, http.StatusInternalServerError, err.Error())
	}
}

// checksumMismatchMessage reshapes the storage error into the
// client-checksums policy wording (the generic adapter's two-source parse;
// storage's message stays the single source of truth).
func checksumMismatchMessage(err error, repoKey, path string) string {
	msg := err.Error()
	for _, algo := range []string{"sha256", "sha1", "md5"} {
		marker := algo + " received "
		if i := strings.Index(msg, marker); i >= 0 {
			rest := msg[i+len(marker):]
			if j := strings.Index(rest, ","); j >= 0 {
				return fmt.Sprintf("Checksum error for '%s/%s': received '%s' but actual is '%s'",
					repoKey, path, rest[:j], strings.TrimPrefix(rest[j+1:], " actual "))
			}
		}
	}
	return fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, path, err)
}

// contentTypeOf maps a version-file extension onto its wire Content-Type
// (goproxy.md section 2's success-response column).
func contentTypeOf(ext string) string {
	switch ext {
	case "info":
		return "application/json"
	case "mod":
		return "text/plain; charset=utf-8"
	case "zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

// digestTriple is the measured digest set of a served node.
type digestTriple struct{ sha256, sha1, md5 string }

// digestsOf resolves a node's three digests (sha256 from the node row,
// sha1/md5 from the blobs ledger; a ledger miss degrades to sha256-only —
// the same tolerance the generic adapter shows).
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

// serveNode streams one stored node: checksum headers, hint merge (the
// remote engine's X-BinFlow-Cache, the virtual resolver's
// X-BinFlow-Resolved-From), Content-Length, and the body (suppressed on
// HEAD).
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

// serveSynth renders a synthesized body (the .info/.mod synthesis chains)
// with the same response-header contract as a stored node.
func (h *Handler) serveSynth(w http.ResponseWriter, body []byte, ctype string, sum digestTriple) {
	hdr := w.Header()
	if sum.sha256 != "" {
		hdr.Set(hdrChecksumSha256, sum.sha256)
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("Content-Type", ctype)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed JSON/text body, never client bytes
}

// synthMod renders the synthesized go.mod of a version without one (the
// official synthesis obligation, triggered on the +incompatible shape per
// goproxy.md section 4.2): the single module line, nothing else.
func synthMod(module string) []byte {
	return []byte("module " + module + "\n")
}

// infoBody is the synthesized-.info shape: BinFlow emits the minimal field
// set (Version + Time; goproxy.md section 4.1's convergence decision).
type infoBody struct {
	Version string `json:"Version"`
	Time    string `json:"Time,omitempty"`
}
