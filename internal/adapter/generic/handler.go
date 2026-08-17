package generic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// protocol-owned header names (Artifactory compatibility, rest-api.md 1.3).
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
	hdrChecksumDeploy = "X-Checksum-Deploy"
	hdrExplodeArchive = "X-Explode-Archive"
)

// contentTypeFileInfo is the ItemCreated body of a successful upload
// (rest-api.md 1.2): FileInfo JSON with size serialized as a string.
const contentTypeFileInfo = "application/vnd.org.jfrog.artifactory.storage.ItemCreated+json; charset=UTF-8"

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: M1 generic serves local repos.
func (h *Handler) RepoTypes() []string { return []string{repo.TypeLocal} }

// Layout implements adapter.Handler via the shared generic layout parser.
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

// ServeHTTP dispatches the four content verbs. Middleware (auth, audit
// scaffolding, error envelope injection) is httpapi's; this handler only
// owns protocol semantics and renders every failure as the errors[] JSON
// envelope itself, so a bare mount still never leaks an HTML error page.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, relPath, err := h.Layout(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)

	switch r.Method {
	case http.MethodPut:
		h.handlePut(ctx, w, r, p, repoKey, relPath)
	case http.MethodGet, http.MethodHead:
		h.handleGet(ctx, w, r, p, repoKey, relPath)
	case http.MethodDelete:
		h.handleDelete(ctx, w, p, repoKey, relPath)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf("method %s is not supported on content paths", r.Method))
	}
}

// ---- PUT ----

// handlePut implements the upload semantics of rest-api.md sections 1.2/1.3
// and PRD E-11: client checksums are verified per algorithm (409 on
// disagreement, message carrying received/actual), X-Checksum-Deploy
// performs a zero-transfer deploy, a trailing slash creates a folder node.
func (h *Handler) handlePut(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath string) {
	if v := r.Header.Get(hdrExplodeArchive); v != "" && !strings.EqualFold(v, "false") {
		writeError(w, http.StatusBadRequest, "X-Explode-Archive is not supported in BinFlow M1")
		return
	}
	// Old metadata notation is rejected before anything else touches the
	// path (rest-api.md 1.2 step 3, high confidence).
	if suffix := pathSuffix(relPath); suffix == ":properties" || suffix == ":statistics" {
		writeError(w, http.StatusConflict, "Old metadata notation is not supported anymore")
		return
	}

	mime := r.Header.Get("Content-Type")
	if mime == "" {
		mime = "application/octet-stream"
	}

	// X-Checksum-Deploy: zero-transfer deploy against an existing blob.
	if deploy, ok := headerBool(r.Header, hdrChecksumDeploy); ok && deploy {
		h.handleChecksumDeploy(ctx, w, r, p, repoKey, relPath, mime)
		return
	}

	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	node, err := h.svc.Put(ctx, p, repoKey, relPath, r.Body, expect, mime)
	if err != nil {
		h.writeServiceError(w, err, repoKey, relPath)
		return
	}
	h.writeCreated(w, r, repoKey, relPath, node, declaredSet(expect))
}

// handleChecksumDeploy implements X-Checksum-Deploy (rest-api.md 1.3):
// no body transfer; the declared sha256 (or sha1) must name a blob the
// filestore already holds. Missing dedicated headers -> 400; malformed
// digest or unknown blob -> 404; hit -> 201 with zero bytes transmitted.
func (h *Handler) handleChecksumDeploy(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath, mime string) {
	sha256 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha256)))
	sha1 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumSha1)))
	md5 := strings.ToLower(strings.TrimSpace(r.Header.Get(hdrChecksumMd5)))
	if sha256 == "" && sha1 == "" {
		writeError(w, http.StatusBadRequest,
			"Checksum deploy failed. no checksum header 'X-Checksum-Sha1/X-Checksum-Sha256' was found.")
		return
	}
	if sha256 != "" && !isHex(sha256, 64) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha256 value %q.", sha256))
		return
	}
	if sha1 != "" && !isHex(sha1, 40) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed sha1 value %q.", sha1))
		return
	}
	if md5 != "" && !isHex(md5, 32) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum deploy failed: malformed md5 value %q.", md5))
		return
	}

	// Resolve the target digest to a sha256-keyed blob. BinFlow addresses
	// blobs by sha256 only; a sha1-only declaration has no reverse index in
	// M1 (the ledger is sha256-keyed), so it cannot be resolved and lands
	// on the same 404 as an unknown sha256 — the client cannot distinguish
	// "never uploaded" from "wrong checksum", which is the spec's posture.
	// The dominant client form (X-Checksum-Sha256) is fully served.
	sha := sha256
	if sha == "" {
		writeError(w, http.StatusNotFound,
			"Checksum deploy failed: X-Checksum-Sha1-only deploy requires a sha256-keyed lookup, which BinFlow does not provide; supply X-Checksum-Sha256.")
		return
	}
	rc, ref, err := h.opener(ctx, sha)
	if err != nil {
		if errors.Is(err, storage.ErrBlobNotFound) {
			writeError(w, http.StatusNotFound, "Checksum deploy failed: no content found for the given checksum.")
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("open blob for checksum deploy: %v", err))
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd, best-effort cleanup

	expect := storage.BlobRef{Sha256: sha, Sha1: sha1, Md5: md5, Size: ref.Size}
	node, err := h.svc.Put(ctx, p, repoKey, relPath, rc, expect, mime)
	if err != nil {
		h.writeServiceError(w, err, repoKey, relPath)
		return
	}
	h.writeCreated(w, r, repoKey, relPath, node, declaredSet(expect))
}

// declaredDigests parses the X-Checksum-* headers into a BlobRef. Malformed
// values (wrong width, non-hex) are 400-shaped errors, distinct from a
// checksum *mismatch* (409): a client that cannot even spell its digest
// never reached the comparison.
func declaredDigests(hdr http.Header) (storage.BlobRef, error) {
	parse := func(name string, width int) (string, error) {
		v := strings.ToLower(strings.TrimSpace(hdr.Get(name)))
		if v == "" {
			return "", nil
		}
		if !isHex(v, width) {
			return "", fmt.Errorf("%w: %s must be exactly %d hex characters, got %q",
				adapter.ErrInvalidChecksum, name, width, v)
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

// declaredSet remembers which algorithms the client actually declared, so
// originalChecksums echoes exactly those (repo-semantics section 5: the
// policy only ever inspects algorithms the client supplied).
func declaredSet(expect storage.BlobRef) map[string]bool {
	m := map[string]bool{}
	if expect.Sha256 != "" {
		m["sha256"] = true
	}
	if expect.Sha1 != "" {
		m["sha1"] = true
	}
	if expect.Md5 != "" {
		m["md5"] = true
	}
	return m
}

// writeCreated renders the 201 response: Location, X-Checksum-Sha256 header
// and the FileInfo/FolderInfo-shaped ItemCreated body (rest-api.md 1.2).
func (h *Handler) writeCreated(w http.ResponseWriter, r *http.Request, repoKey, relPath string, node *metadata.Node, declared map[string]bool) {
	sums := h.digestsOf(r.Context(), node)
	w.Header().Set("Location", requestBase(r)+"/"+repoKey+"/"+escapePath(relPath))
	if sums.sha256 != "" {
		w.Header().Set(hdrChecksumSha256, sums.sha256)
	}
	w.Header().Set("Content-Type", contentTypeFileInfo)
	w.WriteHeader(http.StatusCreated)
	body := h.itemInfo(requestBase(r), repoKey, relPath, node, sums, declared)
	writeJSON(w, body)
}

// ---- GET / HEAD ----

// handleGet streams the artifact body (GET) or just the metadata headers
// (HEAD) per rest-api.md section 1.4: checksum headers, ETag = sha1
// (unquoted), Last-Modified, Accept-Ranges, Content-Type. Range and
// conditional requests (FR-4-AC14/AC15) are honored on both verbs: a HEAD
// answers 206/304/416 exactly like a GET, minus the body.
func (h *Handler) handleGet(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, relPath string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrIsFolder) {
			// Folder nodes have no body; Artifactory's content path answers
			// folder GETs through /api/storage, so on the raw path a plain
			// 404-shaped "not a file" is the honest answer.
			writeError(w, http.StatusNotFound, fmt.Sprintf("Failed to find the requested resource '%s/%s'.", repoKey, relPath))
			return
		}
		h.writeServiceError(w, err, repoKey, relPath)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	sums := h.digestsOf(ctx, node)
	lastMod := parseRFC3339(node.UpdatedAt)
	if lastMod.IsZero() {
		lastMod = parseRFC3339(node.CreatedAt)
	}

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
	if sums.sha1 != "" {
		hdr.Set("ETag", sums.sha1) // unquoted sha1, rest-api.md 1.4
	}
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("Content-Type", mimeOr(node.Mime))

	// Conditional requests first: a fresh store answers 304 with no body.
	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// Then Range: one satisfiable byte range slices the stream (206); an
	// unsatisfiable/malformed spec is a 416; anything BinFlow does not
	// implement (multi-range, other units) is ignored and serves the full
	// 200 body (FR-4-AC14: never 5xx).
	parser := httpRangeParser{total: node.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		hdr.Del("Content-Length") // an unsatisfiable range has no body length
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	case !ignore && rng.length() > 0:
		if _, err := rc.Seek(rng.start, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("seek blob for range: %v", err))
			return
		}
		hdr.Set("Content-Range", rng.contentRange(node.Size))
		hdr.Set("Content-Length", strconv.FormatInt(rng.length(), 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.CopyN(w, rc, rng.length())
		return
	}

	// Full 200 body.
	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// ---- DELETE ----

// handleDelete implements the idempotent delete: success is 204 with no
// body, a repeat delete is the same 404 as any unknown path (rest-api.md
// 1.1, repo-semantics section 4).
func (h *Handler) handleDelete(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, relPath string) {
	if suffix := pathSuffix(relPath); suffix == ":properties" || suffix == ":statistics" {
		writeError(w, http.StatusConflict, "Old metadata notation is not supported anymore")
		return
	}
	if err := h.svc.Delete(ctx, p, repoKey, relPath); err != nil {
		h.writeServiceError(w, err, repoKey, relPath)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- shared helpers ----

// writeServiceError maps repo.Service sentinels onto protocol statuses with
// the errors[] envelope. Checksum mismatches carry the spec's received/
// actual wording (repo-semantics section 5, client-checksums policy).
func (h *Handler) writeServiceError(w http.ResponseWriter, err error, repoKey, relPath string) {
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeError(w, http.StatusConflict, checksumMismatchMessage(err, repoKey, relPath))
	case errors.Is(err, repo.ErrNodeNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("Could not locate artifact. Path: '%s/%s'.", repoKey, relPath))
	case errors.Is(err, repo.ErrRepoNotFound):
		writeError(w, http.StatusNotFound, fmt.Sprintf("Failed to find the repository '%s' specified in the request.", repoKey))
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

// checksumMismatchMessage reshapes the storage error ("sha256 received X,
// actual Y") into the client-checksums policy wording. Storage's message
// carries both values; parse them out rather than string-building a second
// source of truth.
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

// digestsOf resolves the node's three digests. sha256 lives on the node;
// sha1/md5 are ledger facts keyed by the blob (ADR-0006: no sidecar). A
// ledger miss degrades to sha256-only — the download keeps serving while
// the consistency tooling notices the gap.
func (h *Handler) digestsOf(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.md == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.md.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

type digestTriple struct{ sha256, sha1, md5 string }

// pathSuffix returns the ":name" suffix of the final path segment, if any.
func pathSuffix(rel string) string {
	if i := strings.LastIndexByte(rel, ':'); i >= 0 {
		return rel[i:]
	}
	return ""
}

// isHex reports whether s is exactly n lowercase-or-uppercase hex chars.
func isHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// mimeOr defaults an absent stored mime to octet-stream (FR-4-AC13).
func mimeOr(m string) string {
	if strings.TrimSpace(m) == "" {
		return "application/octet-stream"
	}
	return m
}

// headerBool parses an optional boolean-ish header; ok=false means absent.
func headerBool(hdr http.Header, name string) (val, ok bool) {
	v := strings.TrimSpace(hdr.Get(name))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	}
	return false, false
}

// requestBase is scheme://host from the request (Location header base).
func requestBase(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// escapePath percent-encodes the path for the Location header.
func escapePath(rel string) string {
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// parseRFC3339 parses an RFC3339 timestamp, returning zero on failure.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
