package maven

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Protocol-owned header names (the M1 content-plane contract, rest-api.md
// section 1.3) — maven inherits the exact set so wagon/mvn see one product.
const (
	hdrChecksumSha256 = "X-Checksum-Sha256"
	hdrChecksumSha1   = "X-Checksum-Sha1"
	hdrChecksumMd5    = "X-Checksum-Md5"
	hdrChecksumDeploy = "X-Checksum-Deploy"
	hdrExplodeArchive = "X-Explode-Archive"
)

// contentTypeItemCreated is the ItemCreated body Content-Type of a
// successful artifact deploy (rest-api.md section 1.2).
const contentTypeItemCreated = "application/vnd.org.jfrog.artifactory.storage.ItemCreated+json; charset=UTF-8"

// sidecarContentType serves the computed checksum body of a sidecar GET
// (.sha1/.md5/.sha256 map to the checksum media type, config-formats.md
// section 3).
const sidecarContentType = "application/x-checksum"

// maxMetadataBuffer bounds the in-memory buffering of client metadata
// uploads (maven-metadata.xml PUTs are a few KB in practice); beyond it the
// body streams without the idempotent-retransmit shortcut.
const maxMetadataBuffer = 1 << 20

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: M3's maven serves local
// repositories directly; remote is the pull-through engine's (T-66) and
// virtual the two-bucket resolver's (T-71) — the adapter's transfer plane
// passes those classes through repo.Service and only adds the two
// maven-specific intercepts (sidecar 404 on remote, method gates).
func (h *Handler) RepoTypes() []string {
	return []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual}
}

// Layout implements adapter.Handler via the shared content-path parser;
// the maven-specific structure validation runs inside ServeHTTP so every
// verb sees the same classification (and the same 400s, M20).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	return adapter.Layout(r)
}

// ServeHTTP dispatches the four content verbs. Middleware (auth, error
// envelope) is httpapi's; this handler owns the maven semantics and
// renders every failure as the errors[] JSON envelope itself (the three
// protocol adapters share that body shape, maven-npm-pypi.md section 0).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, relPath, err := h.Layout(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The Maven indexer namespace is a permanent non-goal (PRD section
	// 2.2): answer the honest E-01 404 before the layout parser would
	// reject the same path with a 400 (ME-10's status is the contract).
	if seg, _, _ := strings.Cut(relPath, "/"); seg == ".index" {
		writeError(w, http.StatusNotFound,
			".index is not implemented in BinFlow (the Maven repository index is a documented non-goal)")
		return
	}
	l, err := Parse(relPath)
	if err != nil {
		// C2 interim: the layout model is settled (high confidence), the
		// refusal code is not in the spec — 400 is BinFlow's ruling.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	p := adapter.PrincipalFrom(ctx)
	switch r.Method {
	case http.MethodPut:
		h.handlePut(ctx, w, r, p, repoKey, relPath, l)
	case http.MethodGet, http.MethodHead:
		h.handleGet(ctx, w, r, p, repoKey, relPath, l)
	case http.MethodDelete:
		h.handleDelete(ctx, w, p, repoKey, relPath, l)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeError(w, http.StatusMethodNotAllowed,
			fmt.Sprintf("method %s is not supported on maven content paths", r.Method))
	}
}

// ---- GET / HEAD ----

// handleGet serves downloads with the inherited M1 header set (X-Checksum-*,
// ETag=sha1, Last-Modified, Accept-Ranges, Content-Type; Range 206/416 and
// conditional 304 — ME-02), plus the maven-specific reads: the computed
// checksum sidecar body, the remote-repository sidecar pass-through 404 and
// the virtual-repository metadata merge (T-72: maven-metadata.xml and its
// sidecars answer from the in-memory merge of the members' documents, never
// a single member's first-hit copy).
func (h *Handler) handleGet(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if row, err := h.class.Get(ctx, repoKey); err == nil && row.Type == repo.TypeVirtual {
		if h.serveVirtualMetadata(ctx, w, r, repoKey, relPath, l) {
			return
		}
	}
	if l.Kind == KindSidecar {
		// A REMOTE repository never serves checksum files, cached or
		// upstream (maven-npm-pypi.md section 1.5, high confidence; FR-20
		// step 2): the request dies before any engine involvement, and it
		// must die for anonymous readers too — hence the class seam, not
		// the authenticated GetRepo face.
		if row, err := h.class.Get(ctx, repoKey); err == nil && row.Type == repo.TypeRemote {
			writeError(w, http.StatusNotFound, "Checksums are not downloadable.")
			return
		}
		h.serveSidecar(ctx, w, r, p, repoKey, relPath, l)
		return
	}
	h.serveFile(ctx, w, r, p, repoKey, relPath)
}

// serveSidecar answers a checksum sidecar GET/HEAD with the server-computed
// digest of the TARGET — never a passthrough of stored sidecar bytes (the
// stored bytes only register the client's original claim, ME-03).
func (h *Handler) serveSidecar(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	// sha512 is outside the three-digest model: no honest computed body
	// exists, and pretending the stored claim is the answer would violate
	// the computed-value contract. 404 it.
	if l.Algo == "sha512" {
		writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
		return
	}
	rc, node, err := h.svc.Get(ctx, p, repoKey, l.Target)
	if err != nil {
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	// The resolution hints ride the target's stream even though the sidecar
	// body is server-computed: an operator asking "which member served this
	// checksum's target" gets the same X-BinFlow-Resolved-From answer the
	// artifact GET carries.
	applyReaderHints(w, rc)
	_ = rc.Close() //nolint:errcheck // read-only fd; only the node metadata is needed

	digest, ok := h.digestOf(ctx, node, l.Algo)
	if !ok {
		writeError(w, http.StatusInternalServerError,
			fmt.Sprintf("digest %s of '%s/%s' is not available (ledger gap)", l.Algo, repoKey, l.Target))
		return
	}
	body := digest // bare hex, no trailing newline (ME-03/FR-16)

	hdr := w.Header()
	hdr.Set("Content-Type", sidecarContentType)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set("ETag", body) // unquoted digest, the M1 ETag convention
	hdr.Set("Last-Modified", nodeTime(node).UTC().Format(http.TimeFormat))
	hdr.Set(hdrChecksumSha256, node.Sha256)
	if evalConditional(r, body, nodeTime(node)) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, body)
}

// serveFile streams an artifact or stored metadata node with the full M1
// download contract.
func (h *Handler) serveFile(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, relPath)
	if err != nil {
		if errors.Is(err, repo.ErrIsFolder) {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, relPath)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	// Service-level engines may attach response hints to the body stream —
	// a remote member's X-BinFlow-Cache / X-Binflow-Upstream-Error (T-66), a
	// virtual resolution's X-BinFlow-Resolved-From on top of those (T-71).
	// The probe is structural: this handler never imports the engine or
	// learns the repository class (architecture section 5.4).
	applyReaderHints(w, rc)

	sums := h.digestTriple(ctx, node)
	lastMod := nodeTime(node)

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
	hdr.Set("Content-Type", mimeForPath(relPath, node.Mime))

	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	parser := httpRangeParser{total: node.Size}
	rng, malformed, ignore := parser.parseRange(r.Header.Get("Range"))
	switch {
	case malformed:
		hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(node.Size, 10))
		hdr.Del("Content-Length")
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
	hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// ---- DELETE ----

// handleDelete is the M1 idempotent delete (204; repeat delete the same
// 404 as any unknown path) plus ME-07's asynchronous recalculation of the
// affected directory tree: an artifact delete touches its version
// directory and the module version list, a metadata delete the document's
// own directory (which the recalculation regenerates while the facts
// warrant one).
func (h *Handler) handleDelete(ctx context.Context, w http.ResponseWriter,
	p *repo.Principal, repoKey, relPath string, l Layout) {
	if err := h.svc.Delete(ctx, p, repoKey, relPath); err != nil {
		h.writeServiceError(w, err, http.MethodDelete, repoKey, relPath)
		return
	}
	// Only a LOCAL repository's facts drive recalculation: a remote
	// delete dropped a cache copy (its metadata is upstream's business)
	// and a virtual delete never reached here (RE-08's 405).
	if row, err := h.class.Get(ctx, repoKey); err == nil && row.Type == repo.TypeLocal {
		h.calc.afterDelete(p, repoKey, l)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- error mapping ----

// writeServiceError maps repo.Service sentinels onto the maven protocol
// statuses. It mirrors the generic adapter's table (the transfer plane is
// shared) and adds the class refusals: PUT on a remote repository is the
// RE-05 405, PUT on an un-routed virtual repository the Q2/C5 405 with the
// errata's fixed wording.
//
// A *repo.StatusError renders VERBATIM first (T-82, the generic adapter's
// T-66 seam): the repository-class engines — the remote pull-through's RE-04
// fault matrix and RE-05 read-only 405, the virtual resolver's RE-08 delete
// refusal and C5 write refusal — own their exact client rendering in the
// service layer while this handler stays class-agnostic (architecture
// section 5.4). Before this branch the DELETE-on-virtual refusal fell into
// the ErrRepoTypeNotSupported arm's non-PUT 400 shape instead of its 405.
func (h *Handler) writeServiceError(w http.ResponseWriter, err error, method, repoKey, relPath string) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		for k, vv := range se.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		writeError(w, se.Code, se.Message)
		return
	}
	switch {
	case errors.Is(err, storage.ErrChecksumMismatch):
		writeError(w, http.StatusConflict, checksumMismatchMessage(err, repoKey, relPath))
	case errors.Is(err, repo.ErrNodeNotFound):
		if method == http.MethodGet || method == http.MethodHead {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return
		}
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
	case errors.Is(err, repo.ErrIsFolder):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, repo.ErrRepoTypeNotSupported):
		// A non-local repository on a WRITE path: the class refusals the
		// remote/virtual engines own (RE-05/Q2). Since T-66/T-71 those
		// refusals arrive as *repo.StatusError and render verbatim in the
		// branch above; this arm is the fallback for plain sentinel wraps
		// (the engines being unwired, docker-class refusals leaking through)
		// and keeps the 405 shape rather than pre-empting svc.Put, so a
		// service-side write ROUTE (T-71's defaultDeploymentRepo) passes
		// through untouched — the adapter only shapes the refusal.
		if method != http.MethodPut && method != http.MethodPost {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		w.Header().Set("Allow", "GET, HEAD")
		if h.repoClassIsVirtual(repoKey) {
			writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf(
				"No local repository was configured as local deployment repository for the (%s) virtual repository.", repoKey))
			return
		}
		writeError(w, http.StatusMethodNotAllowed, fmt.Sprintf(
			"Cannot deploy '%s/%s': remote repositories are read-only pull-through caches.", repoKey, relPath))
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

// repoClassIsVirtual resolves the repository class via the anonymous seam
// (the PUT already carries an authenticated principal, but the refusal
// wording choice is routing data, which is exactly what ClassReader is
// for; a lookup failure falls back to the remote wording).
func (h *Handler) repoClassIsVirtual(repoKey string) bool {
	row, err := h.class.Get(context.Background(), repoKey)
	return err == nil && row.Type == repo.TypeVirtual
}

// checksumMismatchMessage reshapes the storage error ("sha256 received X,
// actual Y") into the client-checksums wording (repo-semantics section 5),
// the same reshape the generic adapter performs so both adapters answer a
// mismatch byte-identically.
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

// notFoundMessage is the download-side 404 wording (rest-api.md 1.4).
func notFoundMessage(repoKey, relPath string) string {
	return fmt.Sprintf("Failed to find the requested resource '%s/%s'.", repoKey, relPath)
}

// applyReaderHints copies a body stream's structural response hints onto the
// response (the generic adapter's T-66 seam, restated locally because adapter
// packages share no unexported code — the area rule). A plain local blob
// carries none, so the probe is a no-op for the M1 paths.
func applyReaderHints(w http.ResponseWriter, body io.ReadSeekCloser) {
	if extra, ok := body.(interface{ ExtraHeaders() http.Header }); ok {
		for k, vv := range extra.ExtraHeaders() {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
}

// ---- digest helpers ----

type digestTriple struct{ sha256, sha1, md5 string }

// digestTriple resolves a node's three digests; a ledger miss degrades to
// sha256-only (the download keeps serving, the same posture as generic).
func (h *Handler) digestTriple(ctx context.Context, node *metadata.Node) digestTriple {
	t := digestTriple{sha256: node.Sha256}
	if h.ledger == nil || node.Sha256 == "" {
		return t
	}
	b, err := h.ledger.Get(ctx, node.Sha256)
	if err != nil || b == nil {
		return t
	}
	t.sha1, t.md5 = b.Sha1, b.Md5
	return t
}

// digestOf resolves one named digest ("sha256"/"sha1"/"md5") of a node.
func (h *Handler) digestOf(ctx context.Context, node *metadata.Node, algo string) (string, bool) {
	switch algo {
	case "sha256":
		return node.Sha256, node.Sha256 != ""
	case "sha1", "md5":
		t := h.digestTriple(ctx, node)
		if algo == "sha1" {
			return t.sha1, t.sha1 != ""
		}
		return t.md5, t.md5 != ""
	}
	return "", false
}

// nodeTime parses a node timestamp, zero on failure.
func nodeTime(n *metadata.Node) time.Time {
	for _, s := range []string{n.UpdatedAt, n.CreatedAt} {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
