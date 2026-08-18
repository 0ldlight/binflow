package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/storage"
)

// The blob-upload domain: the three push styles of the distribution spec
// (monolithic POST, streamed/chunked PATCH, finalize PUT), the offset query
// and cancel endpoints, and the cross-repo mount. The adapter owns the
// protocol state the storage layer deliberately does not see (the received
// offset and the Docker-Upload-UUID <-> storage.Session pairing); the
// sessionRegistry below is that pairing's home.

// uploadRoute marker of the tail: "blobs/uploads" or "blobs/uploads/<id>".
const uploadsTailPrefix = "blobs/uploads"

// liveUpload is one in-flight upload session: the storage session plus the
// protocol's own received counter. The counter mirrors what Append returned
// (and state.json persists); keeping it here means the Content-Range contract
// is judged against the adapter's own bookkeeping, never by trusting a client
// claim.
type liveUpload struct {
	sess     storage.Session
	received int64
	started  time.Time
	// poisoned is set when Append failed mid-stream: the session must be
	// aborted before it can serve any further PATCH or a finalize. The
	// registry keeps the entry until the client aborts or finalizes so the
	// failure is observable (a blind 202 would let a client believe its
	// bytes landed).
	poisoned bool
}

// sessionRegistry is the process-wide upload-session table: UUID -> live
// upload. Deliberately in-memory (architecture ruling: cross-process resume
// is M3+; a restarted registry answers 404 for every pre-restart session and
// the client restarts its upload from zero, which the spec allows). Entries
// leave through finalize (Commit consumed the session) or abort; the storage
// engine's own TTL sweep is the disk-side backstop for abandoned ones.
type sessionRegistry struct {
	mu   sync.Mutex
	byID map[string]*liveUpload
}

// newSessionRegistry builds the empty table.
func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{byID: map[string]*liveUpload{}}
}

// add registers a fresh upload under the session's own ID.
func (r *sessionRegistry) add(sess storage.Session) *liveUpload {
	up := &liveUpload{sess: sess, started: time.Now()}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[sess.ID()] = up
	return up
}

// lookup resolves one UUID. ok=false is the spec's BLOB_UPLOAD_UNKNOWN.
func (r *sessionRegistry) lookup(id string) (*liveUpload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	up, ok := r.byID[id]
	return up, ok
}

// remove drops a finished/aborted upload; safe to call twice.
func (r *sessionRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// count reports the live-upload total (tests and observability).
func (r *sessionRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byID)
}

// ---- upload endpoint dispatch ----

// serveBlobUploads routes /v2/<name>/blobs/uploads[/<id>] after the repo and
// authorization gates have passed. tail is the raw tail after the image name
// ("blobs/uploads" or "blobs/uploads/<id>").
func (h *Handler) serveBlobUploads(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	rest, ok := strings.CutPrefix(tail, uploadsTailPrefix)
	if !ok {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown uploads route "+tail, nil)
		return
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" { // "blobs/uploads" (no id): the initiating POST
		h.serveUploadStart(w, r, ref)
		return
	}
	// "blobs/uploads/<uuid>": the per-session verbs. The UUID is matched
	// strictly against the registry (a malformed id is simply unknown — the
	// spec's grammar allows more spellings than BinFlow ever mints).
	h.serveUploadSession(w, r, ref, rest)
}

// serveUploadStart implements POST /v2/<name>/blobs/uploads/ (DE-02/DE-03):
// mount when asked (and possible), single-request monolithic when ?digest= is
// present, otherwise the 202 session grant.
func (h *Handler) serveUploadStart(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"method "+r.Method+" is not supported on the uploads route", nil)
		return
	}
	q := r.URL.Query()

	// Cross-repo mount (DE-03): ?mount=<digest>&from=<repo>. Requires read
	// on the SOURCE repository (NFR-S12 — the mount reads content there);
	// any miss degrades to a plain session (spec: "fall back to a normal
	// upload"), never an error.
	if mount := q.Get("mount"); mount != "" {
		from := q.Get("from")
		if h.tryMount(w, r, ref, mount, from) {
			return
		}
		// Fall through: the 202 below IS the spec's degraded mount.
	}

	// Single-request monolithic push (DE-02, "规格照抄" 7): POST with
	// ?digest= and a body finishes in one round trip. The docker client
	// uses this for small layers; oras and curl scripts use it constantly.
	if digest := q.Get("digest"); digest != "" {
		h.monolithicUpload(w, r, ref, digest)
		return
	}

	// Initiating POST: grant a session. The body of the initiating POST is
	// by definition empty for this style (the stream comes on PATCH/PUT), so
	// a non-empty body is drained and ignored rather than trusted.
	sess, err := h.newUploadSession(r)
	if err != nil {
		h.writeStoreFailure(w, r, "begin upload session", err)
		return
	}
	h.sess.add(sess)
	loc := h.uploadLocation(ref, sess.ID(), nil)
	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set("Location", loc)
	hdr.Set("Docker-Upload-UUID", sess.ID())
	hdr.Set("Range", "0-0") // official shape: no bytes received yet
	hdr.Set("Content-Length", "0")
	w.WriteHeader(http.StatusAccepted)
}

// serveUploadSession routes the per-session verbs:
//
//	PATCH  chunked/streamed append        (DE-04)
//	PUT    finalize against ?digest=      (DE-05)
//	GET    offset query, 204 + Range      (official endpoint)
//	DELETE cancel, 204                    (official endpoint)
func (h *Handler) serveUploadSession(w http.ResponseWriter, r *http.Request, ref nameRef, id string) {
	switch r.Method {
	case http.MethodPatch, http.MethodPut:
		up, ok := h.sess.lookup(id)
		if !ok {
			writeBlobUploadUnknown(w, id)
			return
		}
		if r.Method == http.MethodPatch {
			h.patchUpload(w, r, ref, id, up)
			return
		}
		h.finalizeUpload(w, r, ref, id, up)
	case http.MethodGet, http.MethodHead:
		// The offset query. The docker client does not use it (it holds its
		// own offset), but crane/oras and resume tooling do; the official
		// endpoint is GET (204 + Range + Docker-Upload-UUID).
		up, ok := h.sess.lookup(id)
		if !ok {
			writeBlobUploadUnknown(w, id)
			return
		}
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set("Docker-Upload-UUID", id)
		hdr.Set("Range", rangeHeader(up.received))
		hdr.Set("Content-Length", "0")
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		up, ok := h.sess.lookup(id)
		if !ok {
			writeBlobUploadUnknown(w, id)
			return
		}
		_ = up.sess.Abort(context.WithoutCancel(r.Context()))
		h.sess.remove(id)
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set("Content-Length", "0")
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH, PUT, DELETE")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			"method "+r.Method+" is not supported on an upload session", nil)
	}
}

// patchUpload implements PATCH (DE-04): streamed append when Content-Range is
// absent (official "Stream upload"); strict offset alignment when present —
// the start MUST equal the server's received count, a mismatch is 416 with
// the authoritative Range and an empty body (docker-registry.md 2.2#3).
func (h *Handler) patchUpload(w http.ResponseWriter, r *http.Request, ref nameRef, id string, up *liveUpload) {
	if up.poisoned {
		// A poisoned session can never become a trustworthy blob: refuse the
		// append and tell the client where the server stands. The client's
		// recovery is DELETE + restart (spec-legal).
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set("Location", h.uploadLocation(ref, id, nil))
		hdr.Set("Range", rangeHeader(up.received))
		hdr.Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, ok := alignContentRange(r.Header.Get("Content-Range"), up.received)
	if !ok {
		// 416 with the server's authoritative offset and NO body (the spec's
		// error posture for chunk misalignment; docker-registry.md 2.2#3).
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set("Location", h.uploadLocation(ref, id, nil))
		hdr.Set("Range", rangeHeader(up.received))
		hdr.Set("Content-Length", "0")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	_ = start // aligned or absent; Append is strictly cumulative either way

	// Append returns the CUMULATIVE offset (storage contract), not the
	// chunk length — assign, never add.
	received, err := up.sess.Append(r.Context(), r.Body)
	if err != nil {
		// The session is poisoned from here (storage contract); keep the
		// entry so the client observes the failure instead of silently
		// re-anchoring at zero under a fresh session.
		up.poisoned = true
		if errors.Is(err, storage.ErrSessionPoisoned) || r.Context().Err() != nil {
			// A cancelled/failed append: the client is gone or the stream
			// broke; render nothing further (the write below is best
			// effort).
			h.log.WarnContext(r.Context(), "docker: upload append failed",
				"upload", id, "received", up.received, "error", err.Error())
			return
		}
		h.writeStoreFailure(w, r, "append upload "+id, err)
		return
	}
	up.received = received

	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set("Location", h.uploadLocation(ref, id, nil))
	hdr.Set("Range", rangeHeader(up.received))
	hdr.Set("Content-Length", "0")
	w.WriteHeader(http.StatusAccepted)
}

// finalizeUpload implements PUT ?digest= (DE-05): the body may be empty (the
// stream completed on PATCH) or carry the final chunk; Commit verifies the
// digest and publishes atomically. A mismatch is 400 DIGEST_INVALID with the
// session aborted and zero residue (D12/FR-8-AC5).
func (h *Handler) finalizeUpload(w http.ResponseWriter, r *http.Request, ref nameRef, id string, up *liveUpload) {
	digest := r.URL.Query().Get("digest")
	hex, err := parseDigestParam(digest)
	if err != nil {
		// No digest or malformed: the finalize cannot even be attempted.
		// Abort so no half-received session lingers as residue.
		_ = up.sess.Abort(context.WithoutCancel(r.Context()))
		h.sess.remove(id)
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("digest %q is not a valid sha256 digest", digest),
			map[string]string{"digest": digest})
		return
	}
	if up.poisoned {
		// A poisoned session must never be committed (its digests cannot be
		// trusted); refuse and consume it so nothing leaks.
		_ = up.sess.Abort(context.WithoutCancel(r.Context()))
		h.sess.remove(id)
		writeSpecError(w, http.StatusBadRequest, ErrCodeBlobUploadInvalid,
			"upload session was interrupted and cannot be finalized; restart the upload", nil)
		return
	}

	// The finalize body (optional last chunk) joins the stream before Commit.
	if r.ContentLength != 0 {
		if _, err := up.sess.Append(r.Context(), r.Body); err != nil {
			_ = up.sess.Abort(context.WithoutCancel(r.Context()))
			h.sess.remove(id)
			up.poisoned = true
			h.writeStoreFailure(w, r, "append final chunk of upload "+id, err)
			return
		}
	}

	ref0, err := up.sess.Commit(r.Context(), storage.BlobRef{Sha256: hex})
	if err != nil {
		// Commit has already finalized the session either way (storage
		// contract); the registry entry goes too. A checksum disagreement is
		// the client's DIGEST_INVALID; anything else is a store failure.
		h.sess.remove(id)
		if isChecksumMismatch(err) {
			writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
				fmt.Sprintf("provided digest did not match uploaded content: %s", err.Error()),
				map[string]string{"digest": digest})
			return
		}
		h.writeStoreFailure(w, r, "commit upload "+id, err)
		return
	}
	h.sess.remove(id)
	h.writeBlobCreated(w, r, ref, ref0)
}

// monolithicUpload is POST ?digest= with the whole body (DE-02's single
// request arm): one session, one append, one commit — 201 or the honest
// failure with the session aborted.
func (h *Handler) monolithicUpload(w http.ResponseWriter, r *http.Request, ref nameRef, digest string) {
	hex, err := parseDigestParam(digest)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("digest %q is not a valid sha256 digest", digest),
			map[string]string{"digest": digest})
		return
	}
	sess, err := h.newUploadSession(r)
	if err != nil {
		h.writeStoreFailure(w, r, "begin upload session", err)
		return
	}
	h.sess.add(sess)
	defer h.sess.remove(sess.ID())

	if _, err := sess.Append(r.Context(), r.Body); err != nil {
		_ = sess.Abort(context.WithoutCancel(r.Context()))
		if r.Context().Err() != nil {
			return // client vanished mid-push; T-41's posture
		}
		h.writeStoreFailure(w, r, "append monolithic upload "+sess.ID(), err)
		return
	}
	committed, err := sess.Commit(r.Context(), storage.BlobRef{Sha256: hex})
	if err != nil {
		if isChecksumMismatch(err) {
			writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
				fmt.Sprintf("provided digest did not match uploaded content: %s", err.Error()),
				map[string]string{"digest": digest})
			return
		}
		h.writeStoreFailure(w, r, "commit monolithic upload "+sess.ID(), err)
		return
	}
	h.writeBlobCreated(w, r, ref, committed)
}

// tryMount attempts the cross-repo mount (DE-03). Returns true when it
// rendered a response (201 on success; degradation answers nothing and lets
// the caller fall through to the 202).
func (h *Handler) tryMount(w http.ResponseWriter, r *http.Request, dest nameRef, mountParam, fromParam string) bool {
	hex, err := parseDigestParam(mountParam)
	if err != nil || fromParam == "" {
		return false // malformed mount: degrade, the 202 follows
	}
	// The source must be a readable docker repository of its own. from is a
	// full "<repoKey>/<image>" name (the spec's mount semantics address the
	// SOURCE repository name, which in BinFlow's layout carries the repo key
	// as its first segment — ADR-0010 clause 3's naming rule).
	srcRepo, srcImage := splitMountSource(fromParam)
	p := principalOf(r)
	if !h.canMountFrom(r.Context(), p, srcRepo, srcImage) {
		return false // no read on the source: degrade (spec), never leak
	}
	// The blob must exist in the source repository's docker layout; the
	// service's PutFromBlob refuses orphans, and a blob only the GLOBAL
	// store holds (uploaded through another repo, never referenced here)
	// is not mountable — the spec's mount reads the named source repo.
	srcPath := blobNodePath(srcImage, hex)
	if !h.blobPresent(r.Context(), p, srcRepo, srcPath) {
		return false
	}
	// ref0 is the ledger row's full digest triple when one exists (it does
	// for every blob that landed through an upload); fall back to the bare
	// addressing digest otherwise.
	ref0 := storage.BlobRef{Sha256: hex}
	if row := h.ledgerRow(r.Context(), hex); row != nil {
		ref0 = *row
	}
	if _, err := h.svc.PutFromBlob(r.Context(), p, dest.repoKey, blobNodePath(dest.image, hex),
		ref0, mimeOctetStream); err != nil {
		// A permission-shaped refusal on the DESTINATION is a real denial
		// (the route gate already passed, but PutFromBlob re-checks the
		// node-level pair); render it. Anything else degrades to 202 — a
		// mount is an optimization, the plain upload is always correct.
		if isDenied(err) {
			writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
				"requested access to the resource is denied: mount into "+dest.repoKey, nil)
			return true
		}
		h.log.WarnContext(r.Context(), "docker: cross-repo mount degraded to upload",
			"from", fromParam, "to", dest.repoKey, "digest", hex, "error", err.Error())
		return false
	}
	h.writeMountCreated(w, r, dest, ref0)
	return true
}

// ---- helpers ----

// newUploadSession opens a storage session (the engine may be unwired in
// bare test assemblies; that is a store failure the caller renders).
func (h *Handler) newUploadSession(r *http.Request) (storage.Session, error) {
	if h.store == nil {
		return nil, errNoStorageEngine
	}
	sess, err := h.store.BeginSession(r.Context())
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// writeBlobCreated renders the 201 of a landed blob (PUT finalize,
// monolithic POST): Location is the blob URL, Docker-Content-Digest echoes
// the addressing digest, and the checksum family is served whole
// (X-Checksum-Sha256/Sha1/Md5, "规格照抄" 6).
//
// The metadata side lands here too. The registration deliberately rides
// repo.Service.Put — the SAME path a generic upload takes — by streaming
// the just-committed blob back through it: the service's own session
// Commit is an idempotent dedup hit on the already-present physical blob
// (no second copy ever lands), and its putNode writes the blobs-ledger row
// plus the node in the mandated order. PutFromBlob alone cannot serve
// here: it requires the ledger row to pre-exist and only Put's path
// creates it — a fresh docker push has no earlier generic deploy to lean
// on. A registration failure after a successful Commit is
// unreferenced-blob residue by design (GC's grace window); the failure is
// logged loudly but the 201 still stands because the blob IS durable.
func (h *Handler) writeBlobCreated(w http.ResponseWriter, r *http.Request, ref nameRef, blob storage.BlobRef) {
	if err := h.registerBlobNode(r, ref, blob); err != nil && !isDenied(err) {
		h.log.ErrorContext(r.Context(), "docker: blob node registration failed",
			"repo", ref.repoKey, "digest", blob.Sha256, "error", err.Error())
	}
	h.writeMountCreated(w, r, ref, blob)
}

// registerBlobNode drives the landed blob through repo.Service.Put so the
// ledger + node rows appear exactly as on a generic deploy.
func (h *Handler) registerBlobNode(r *http.Request, ref nameRef, blob storage.BlobRef) error {
	rc, _, err := h.store.Open(r.Context(), blob.Sha256)
	if err != nil {
		return fmt.Errorf("open committed blob for registration: %w", err)
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	_, err = h.svc.Put(r.Context(), principalOf(r), ref.repoKey,
		blobNodePath(ref.image, blob.Sha256), rc, blob, mimeOctetStream)
	return err
}

// writeMountCreated is the bare 201 render shared by the upload finalize
// and the mount (the mount has already done its own PutFromBlob).
func (h *Handler) writeMountCreated(w http.ResponseWriter, _ *http.Request, ref nameRef, blob storage.BlobRef) {
	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	hdr.Set("Location", blobURL(ref.repoKey, ref.image, blob.Sha256))
	hdr.Set("Docker-Content-Digest", digestPrefixHex(blob.Sha256))
	if blob.Sha1 != "" {
		hdr.Set(hdrChecksumSha1, blob.Sha1)
	}
	if blob.Md5 != "" {
		hdr.Set(hdrChecksumMd5, blob.Md5)
	}
	hdr.Set(hdrChecksumSha256, blob.Sha256)
	hdr.Set("Content-Length", "0")
	w.WriteHeader(http.StatusCreated)
}

// writeBlobUploadUnknown is the spec's BLOB_UPLOAD_UNKNOWN for session
// addressing misses (unknown UUID, or a session from before a restart).
func writeBlobUploadUnknown(w http.ResponseWriter, id string) {
	writeSpecError(w, http.StatusNotFound, ErrCodeBlobUploadUnknown,
		"blob upload unknown to registry: "+id, nil)
}

// uploadLocation builds the session URL: the RELATIVE root-level path
// (R6: no scheme/host — every registry client resolves it against the
// request; absolute Locations behind misconfigured proxies break more than
// they fix).
func (h *Handler) uploadLocation(ref nameRef, id string, _ []string) string {
	return uploadURL(ref.repoKey, ref.image, id)
}

// writeStoreFailure renders a storage-layer failure: logged once here, then
// the spec-body 500. A closed engine (shutdown race) is a 503 so clients
// retry rather than treat the registry as broken.
func (h *Handler) writeStoreFailure(w http.ResponseWriter, r *http.Request, what string, err error) {
	status := http.StatusInternalServerError
	code := ErrCodeUnknown
	if errors.Is(err, storage.ErrEngineClosed) {
		status = http.StatusServiceUnavailable
		code = ErrCodeUnavailable
	}
	h.log.ErrorContext(r.Context(), "docker: "+what, "error", err.Error())
	writeSpecError(w, status, code, what+": "+err.Error(), nil)
}

// rangeHeader renders the inclusive "0-<received-1>" Range value; a
// zero-byte session renders "0-0" (the official POST 202 shape — the first
// patchable byte is 0).
func rangeHeader(received int64) string {
	if received <= 0 {
		return "0-0"
	}
	return fmt.Sprintf("0-%d", received-1)
}

// alignContentRange validates an optional Content-Range header against the
// server's received offset (DE-04): absent means stream upload (always
// aligned); present must be "<start>-<end>" with start == received. The end
// is advisory (chunk sizing is the client's business); only the anchor is
// protocol.
func alignContentRange(header string, received int64) (start int64, ok bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, true
	}
	// A "bytes " unit prefix is tolerated (RFC 9110 spelling of the same
	// anchor; docker sends the bare "<start>-<end>" form).
	header = strings.TrimPrefix(strings.TrimSpace(header), "bytes ")
	first, _, found := strings.Cut(header, "-")
	if !found {
		return 0, false
	}
	n, err := parseUint64(first)
	if err != nil {
		return 0, false
	}
	return n, n == received
}

// errNoStorageEngine marks an assembly without the storage seam.
var errNoStorageEngine = errors.New("docker: no storage engine wired for blob uploads")
