package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol is the package-type identifier this adapter serves (the
// repositories.package_type value httpapi dispatches on).
const Protocol = "pypi"

// Protocol-reserved first path segments after the repository key. A project
// literally named "simple", "packages" or "pypi" is still reachable through
// its storage path and its index page (/simple/simple/ parses fine — the
// reservation applies only to the first segment); this mirrors every
// simple-index implementation that prefixes protocol routes onto one
// namespace.
const (
	segSimple   = "simple"
	segPackages = "packages"
	// segLegacyJSON is the warehouse legacy JSON API mount point
	// (/pypi/<name>/json) — deliberately unimplemented (PRD PE-04/M58).
	segLegacyJSON = "pypi"
)

// BlobLedger is the read-only digest ledger the adapter consults for
// sha1/md5 download headers (the ancillary digests of a node's blob;
// ADR-0006 keeps no sidecar files). Satisfied by metadata.Store.Blobs() —
// the same seam the generic adapter consumes.
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// Handler is the PyPI protocol adapter (architecture sections 5.1/5.4.3).
// It owns the wire protocol only; every content operation goes through
// repo.Service (uploads land via a storage session + PutLandedBlob, the
// T-64 use case built for exactly this late-path-binding upload shape).
type Handler struct {
	svc   repo.Service
	repos repo.ClassReader
	blobs BlobLedger
	st    storage.Engine
}

// New wires the handler. svc is the content use-case service; repos is the
// class-lookup seam (remote/virtual repositories refuse uploads with the
// PRD 405 wording before any byte is read); blobs serves the sha1/md5
// download headers; st streams upload bodies into content-addressed
// sessions (the docker adapter's WithStorage precedent).
func New(svc repo.Service, repos repo.ClassReader, blobs BlobLedger, st storage.Engine) *Handler {
	return &Handler{svc: svc, repos: repos, blobs: blobs, st: st}
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: every M3 repository class carries
// package_type=pypi rows (FR-15-AC1), and all of them dispatch here — local
// repositories are served in full; remote/virtual repositories pass their
// reads to repo.Service (whose engines own proxying/aggregation, T-66/T-71)
// and refuse writes with the class-specific 405.
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler. It splits the /binflow-stripped path
// into (repoKey, relPath) with the PyPI amendment that the bare repository
// root is a legal address (twine POSTs to .../api/pypi/<repo> with no
// trailing slash, and the domain-root probe GETs .../<repo>/).
//
// Decoding happens HERE, before any segment analysis (the T-12 rule the
// generic layout established): an encoded traversal attempt (%2e%2e) is
// judged on its decoded form and dies with the 400 defense.
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	if r == nil || r.URL == nil {
		return "", "", fmt.Errorf("%w: empty request URL", adapter.ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed percent-encoding in %q: %w", adapter.ErrBadRequestPath, raw, err)
	}
	decoded = strings.TrimPrefix(decoded, "/")
	if decoded == "" {
		return "", "", fmt.Errorf("%w: path is empty (no repository key)", adapter.ErrBadRequestPath)
	}
	key, rest, _ := strings.Cut(decoded, "/")
	if key == "" {
		return "", "", fmt.Errorf("%w: empty repository key", adapter.ErrBadRequestPath)
	}
	if len(key) > adapter.MaxRepoKeyLen {
		return "", "", fmt.Errorf("%w: repository key longer than %d characters", adapter.ErrBadRequestPath, adapter.MaxRepoKeyLen)
	}
	if adapter.IsReservedSegment(key) {
		return "", "", fmt.Errorf("%w: %q is a reserved routing segment", adapter.ErrBadRequestPath, key)
	}
	if err := validateRelPath(rest); err != nil {
		return "", "", err
	}
	return key, rest, nil
}

// validateRelPath enforces the shared artifact-path rules on the decoded
// repo-relative portion, with the PyPI root amendment: "" addresses the
// repository root (upload endpoint / probe), and a single trailing slash
// addresses a folder-shaped route (the simple index). Everything else
// follows the generic defense: no dot segments, no empty segments (double
// slash), no backslashes, no control bytes, bounded length.
func validateRelPath(rel string) error {
	if rel == "" {
		return nil // the repository root is a legal PyPI address
	}
	if len(rel) > adapter.MaxRelPathLen {
		return fmt.Errorf("%w: artifact path of %d characters exceeds the %d limit",
			adapter.ErrBadRequestPath, len(rel), adapter.MaxRelPathLen)
	}
	if strings.Contains(rel, "\\") {
		return fmt.Errorf("%w: backslash is not a path separator", adapter.ErrBadRequestPath)
	}
	if strings.ContainsFunc(rel, isControlByte) {
		return fmt.Errorf("%w: control characters are not allowed in artifact paths", adapter.ErrBadRequestPath)
	}
	body := strings.TrimSuffix(rel, "/")
	if body == "" {
		return fmt.Errorf("%w: empty artifact path segments in %q", adapter.ErrBadRequestPath, rel)
	}
	for _, seg := range strings.Split(body, "/") {
		switch seg {
		case "":
			return fmt.Errorf("%w: empty path segment in %q (double slash?)", adapter.ErrBadRequestPath, rel)
		case ".", "..":
			return fmt.Errorf("%w: dot segment %q in %q escapes or dilutes the repository root",
				adapter.ErrBadRequestPath, seg, rel)
		}
	}
	return nil
}

// isControlByte reports whether r is a control character (C0 range, DEL).
func isControlByte(r rune) bool { return r < 0x20 || r == 0x7f }

// ServeHTTP dispatches on the first repo-relative segment. Protocol routes
// (simple/, packages/, pypi/) are recognized first; every other shape is a
// bare content path — the second entrance to the same nodes the index hrefs
// address (PRD PE-03).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, rel, err := h.Layout(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if rel == "" {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			// Domain-root connectivity probe (maven-npm-pypi.md section 0:
			// 200 with an empty body).
			w.WriteHeader(http.StatusOK)
		case http.MethodPost:
			h.handleUpload(w, r, repoKey)
		default:
			w.Header().Set("Allow", "GET, HEAD, POST")
			writeError(w, http.StatusMethodNotAllowed,
				fmt.Sprintf("method %s is not supported on the PyPI repository root", r.Method))
		}
		return
	}

	first, tail, _ := strings.Cut(rel, "/")
	switch first {
	case segSimple:
		h.serveSimple(w, r, repoKey, rel, tail)
	case segPackages:
		// tail may itself be "" (a bare /packages or /packages/): no file
		// lives at that address; the download handler's not-found wording
		// answers it.
		h.serveDownload(w, r, repoKey, tail)
	case segLegacyJSON:
		// /pypi/<name>/json and every deeper shape: the warehouse legacy
		// JSON API is deliberately out of scope (PRD PE-04, M58 probe).
		writeNotImplemented(w, "the PyPI JSON API (/pypi/<name>/json)")
	default:
		// Bare content path: <name>/<version>/<filename> (or a folder
		// spelling) — read-only second entrance (PE-03).
		h.serveDownload(w, r, repoKey, rel)
	}
}

// writeServiceError maps repo.Service sentinels onto the errors[] envelope
// with the same statuses the generic adapter uses (the /binflow plane's
// unified error family, NFR-S16); only the wordings are PyPI-specific.
func (h *Handler) writeServiceError(w http.ResponseWriter, err error, method, repoKey, path string) {
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeError(w, http.StatusConflict, checksumMismatchMessage(err, repoKey, path))
	case errors.Is(err, repo.ErrNodeNotFound):
		if method == http.MethodGet || method == http.MethodHead {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, path))
			return
		}
		writeError(w, http.StatusNotFound, fmt.Sprintf("Could not locate artifact. Path: '%s/%s'.", repoKey, path))
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey))
	case errors.Is(err, repo.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "Authentication is required to deploy artifacts.")
	case errors.Is(err, repo.ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repo.ErrIsFolder):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// notFoundMessage is the download-side 404 wording (rest-api.md section
// 1.4), shared by the packages/ mount and the bare content entrance.
func notFoundMessage(repoKey, relPath string) string {
	return fmt.Sprintf("Failed to find the requested resource '%s/%s'.", repoKey, relPath)
}

// checksumMismatchMessage reshapes the storage error ("md5 received X,
// actual Y") into the client-checksums policy wording (repo-semantics
// section 5) — the same two-source parse the generic adapter performs, so
// the storage message stays the single source of truth.
func checksumMismatchMessage(err error, repoKey, relPath string) string {
	msg := err.Error()
	received, actual := "", ""
	for _, algo := range []string{"sha256", "sha1", "md5"} {
		marker := algo + " received "
		if i := strings.Index(msg, marker); i >= 0 {
			rest := msg[i+len(marker):]
			if j := strings.Index(rest, ","); j >= 0 {
				received, actual = rest[:j], strings.TrimPrefix(rest[j+1:], " actual ")
				break
			}
		}
	}
	if received == "" || actual == "" {
		return fmt.Sprintf("Checksum error for '%s/%s': %v", repoKey, relPath, err)
	}
	return fmt.Sprintf("Checksum error for '%s/%s': received '%s' but actual is '%s'",
		repoKey, relPath, received, actual)
}

// writeError emits the errors[] envelope (the unified non-2xx body,
// architecture section 7.3). Rendering failures are ignored: by the time an
// error body fails to encode the connection is gone anyway.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(errorEnvelope{Errors: []errorEntry{{Status: status, Message: message}}})
}

// writeNotImplemented renders the E-26-family 404 whose message carries the
// "not implemented" wording, so a client can tell "wrong product" from
// "wrong milestone" (PRD section 5.1).
func writeNotImplemented(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, what+" is not implemented in BinFlow")
}

type errorEnvelope struct {
	Errors []errorEntry `json:"errors"`
}

type errorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}
