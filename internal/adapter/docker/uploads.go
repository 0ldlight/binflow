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
//
// Concurrency (review B1): every field is guarded by mu, and mu serializes
// the WHOLE per-session operation — the Content-Range judgment, the Append
// and the received assignment happen inside one critical section, so two
// concurrent PATCHes on the same UUID cannot both pass the anchor check
// against a stale offset, and received can never move backwards on an
// out-of-order completion. Registry lookups hand out the pointer; callers
// take mu before touching state.
type liveUpload struct {
	mu       sync.Mutex
	sess     storage.Session
	received int64
	started  time.Time
	// poisoned is set when Append failed mid-stream: the session must be
	// aborted before it can serve any further PATCH or a finalize. The
	// registry keeps the entry until the client aborts or finalizes so the
	// failure is observable (a blind 202 would let a client believe its
	// bytes landed).
	poisoned bool
	// done is set when the session left the registry through a terminal
	// verb (finalize or abort) while a racing request already held the
	// entry pointer. The late arrival must observe "session gone"
	// (BLOB_UPLOAD_UNKNOWN), never the storage layer's "already finalized"
	// error dressed as a 500 — from the protocol's side a finalized session
	// does not exist anymore.
	done bool
}

// idleTTL bounds how long an untouched upload session may linger in the
// registry before the sweep evicts it (review B2): a docker push that dies
// mid-flight leaves its session behind forever otherwise — one open data fd
// plus a disk directory per abandoned POST, with nothing to reclaim them
// (the storage engine's own sweep runs at startup only and deliberately
// skips live sessions). The default mirrors storage.DefaultSessionTTL (24h)
// so the adapter's in-memory eviction and the engine's disk-side grace agree
// on one number; the sweep interval is a fraction of it.
const (
	idleSessionTTL    = storage.DefaultSessionTTL
	idleSweepInterval = idleSessionTTL / 48 // twice an hour at the 24h default
)

// sessionRegistry is the process-wide upload-session table: UUID -> live
// upload. Deliberately in-memory (architecture ruling: cross-process resume
// is M3+; a restarted registry answers 404 for every pre-restart session and
// the client restarts its upload from zero, which the spec allows). Entries
// leave through finalize (Commit consumed the session), abort, or the idle
// sweep (B2); the storage engine's startup sweep is the disk-side backstop.
type sessionRegistry struct {
	mu   sync.Mutex
	byID map[string]*liveUpload
	// ttl is the idle bound the sweep enforces; overridable in tests (the
	// production value is idleSessionTTL).
	ttl time.Duration
	// sweepOnce starts the background sweeper exactly once per process (the
	// handler is built once; tests that never trigger it pay nothing).
	sweepOnce sync.Once
}

// newSessionRegistry builds the empty table.
func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{byID: map[string]*liveUpload{}, ttl: idleSessionTTL}
}

// startSweep launches the idle-eviction loop (once per process). The loop
// holds no registry lock while aborting sessions (Abort does filesystem
// work); an entry racing a legitimate finalize is removed idempotently on
// both sides, and an Abort of an already-finalized session is a no-op
// (storage contract), so the sweep can never destroy live state.
func (r *sessionRegistry) startSweep() {
	r.sweepOnce.Do(func() {
		go func() {
			t := time.NewTicker(idleSweepInterval)
			defer t.Stop()
			for range t.C {
				r.evictIdle(time.Now())
			}
		}()
	})
}

// evictIdle aborts and removes every session idle beyond the TTL. It takes
// a snapshot under the lock, then works without it (Abort touches the disk).
func (r *sessionRegistry) evictIdle(now time.Time) []string {
	type candidate struct {
		id string
		up *liveUpload
	}
	var expired []candidate
	r.mu.Lock()
	for id, up := range r.byID {
		if now.Sub(up.lastActivityLocked()) > r.ttl {
			expired = append(expired, candidate{id: id, up: up})
		}
	}
	// Remove under the lock so a racing PATCH observes BLOB_UPLOAD_UNKNOWN
	// instead of joining a session that is being torn down.
	for _, c := range expired {
		delete(r.byID, c.id)
	}
	r.mu.Unlock()
	for _, c := range expired {
		c.up.mu.Lock()
		_ = c.up.sess.Abort(context.Background())
		c.up.poisoned = true
		c.up.mu.Unlock()
	}
	ids := make([]string, 0, len(expired))
	for _, c := range expired {
		ids = append(ids, c.id)
	}
	return ids
}

// lastActivityLocked reports the session's idleness anchor. The started
// stamp IS the anchor: received grows monotonically with started fixed, and
// the sweep only cares about wall-clock abandonment, so the birth time is
// both sufficient and the honest bound (a session that has been streaming
// for hours is not "idle" in the disk-usage sense the TTL addresses — but
// it also holds an fd the whole time, and 24h is a generous ceiling for any
// single docker push).
func (u *liveUpload) lastActivityLocked() time.Time { return u.started }

// add registers a fresh upload under the session's own ID.
func (r *sessionRegistry) add(sess storage.Session) *liveUpload {
	up := &liveUpload{sess: sess, started: time.Now()}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[sess.ID()] = up
	return up
}

// lookup resolves one UUID. ok=false is the spec's BLOB_UPLOAD_UNKNOWN.
// The entry's own lock is NOT held here; the caller serializes its session
// operation through it (see liveUpload.mu).
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

	// Initiating POST: grant a session. The initiating POST carries no body
	// by definition for this style (the stream comes on PATCH/PUT); a client
	// that sends one anyway is not read — the connection is simply closed
	// after the 202, which every registry client tolerates (N4: the previous
	// comment claimed a drain that never happened).
	sess, err := h.newUploadSession(r)
	if err != nil {
		h.writeStoreFailure(w, r, "begin upload session", err)
		return
	}
	h.sess.add(sess)
	h.sess.startSweep() // B2: the idle-eviction loop rides the first session
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
//
// Every verb takes the session's own lock for its whole operation (review
// B1): the storage Session is single-threaded by contract, and the
// received/poisoned protocol state must never interleave.
func (h *Handler) serveUploadSession(w http.ResponseWriter, r *http.Request, ref nameRef, id string) {
	switch r.Method {
	case http.MethodPatch, http.MethodPut:
		up, ok := h.sess.lookup(id)
		if !ok {
			writeBlobUploadUnknown(w, id)
			return
		}
		up.mu.Lock()
		defer up.mu.Unlock()
		if up.done {
			// The session ended while this request was in flight (the
			// winner of the race already finalized or aborted it); the
			// protocol sees no session here.
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
		up.mu.Lock()
		received, done := up.received, up.done
		up.mu.Unlock()
		if done {
			writeBlobUploadUnknown(w, id)
			return
		}
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set("Docker-Upload-UUID", id)
		hdr.Set("Range", rangeHeader(received))
		hdr.Set("Content-Length", "0")
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		up, ok := h.sess.lookup(id)
		if !ok {
			writeBlobUploadUnknown(w, id)
			return
		}
		up.mu.Lock()
		_ = up.sess.Abort(context.WithoutCancel(r.Context()))
		up.done = true
		up.mu.Unlock()
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
// The caller holds up.mu for the whole operation (B1): the anchor judgment
// and the Append+received assignment are one indivisible critical section.
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
	if _, ok := alignContentRange(r.Header.Get("Content-Range"), up.received); !ok {
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

	// Append returns the CUMULATIVE offset (storage contract), not the
	// chunk length — assign, never add.
	received, err := up.sess.Append(r.Context(), r.Body)
	if err != nil {
		// The session is poisoned from here (storage contract); keep the
		// entry so the client observes the failure instead of silently
		// re-anchoring at zero under a fresh session.
		up.poisoned = true
		h.log.WarnContext(r.Context(), "docker: upload append failed",
			"upload", id, "received", up.received, "error", err.Error())
		if r.Context().Err() != nil {
			// The stream died with the connection: render the poisoned
			// session's restart cue explicitly rather than falling off the
			// handler into net/http's implicit 200-with-empty-body (review
			// non-blocking #1). A still-connected client reads the same
			// 416-with-authoritative-offset it would get from any later
			// PATCH; a disconnected one never sees either.
			hdr := w.Header()
			writeAPIVersionHdr(hdr)
			hdr.Set("Location", h.uploadLocation(ref, id, nil))
			hdr.Set("Range", rangeHeader(up.received))
			hdr.Set("Content-Length", "0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
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
		up.done = true
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
		up.done = true
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
			up.done = true
			h.writeStoreFailure(w, r, "append final chunk of upload "+id, err)
			return
		}
	}

	ref0, err := up.sess.Commit(r.Context(), storage.BlobRef{Sha256: hex})
	if err != nil {
		// Commit has already finalized the session either way (storage
		// contract); the registry entry goes too. A checksum disagreement is
		// the client's DIGEST_INVALID; anything else is a store failure.
		up.done = true
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
	up.done = true
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
// on.
//
// Failure semantics (review B4): a registration failure after a successful
// Commit renders 5xx, NOT 201. The blob itself is durable (Commit
// published it) but no node row exists — answering 201 would confirm a
// push whose blob is invisible to reads and whose manifest would fail
// reference validation, with nothing telling the client to retry. A 5xx is
// the retry signal, and the retry is safe end to end: the re-push's Commit
// dedups onto the existing physical blob and Put's idempotent-retransmit
// rule completes the ledger+node rows. The orphaned physical blob of the
// failed attempt is GC's by-design recovery path. A permission-shaped
// refusal (denied) stays 403 — retrying cannot fix that.
func (h *Handler) writeBlobCreated(w http.ResponseWriter, r *http.Request, ref nameRef, blob storage.BlobRef) {
	if err := h.registerBlobNode(r, ref, blob); err != nil {
		// Both outcomes log at ERROR (N3): the denied branch is a security
		// signal worth its line just as much as the failure branch.
		h.log.ErrorContext(r.Context(), "docker: blob node registration failed",
			"repo", ref.repoKey, "digest", blob.Sha256, "denied", isDenied(err), "error", err.Error())
		if isDenied(err) {
			writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
				"requested access to the resource is denied", nil)
			return
		}
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"blob stored but its repository record could not be written; the push is safe to retry",
			map[string]string{"digest": digestPrefixHex(blob.Sha256)})
		return
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
