package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The multipart-upload REST plane (M10 T-289, FR-90.1 / architecture
// section 15.4 — the inv-4 L3 endpoint set's BinFlow carrier). Six
// endpoints under /binflow/api/v1/uploads:
//
//	POST   /api/v1/uploads/create               open a session
//	POST   /api/v1/uploads/config               re-part a byte-less session
//	GET    /api/v1/uploads/urlPart/{id}/{n}     the part-n upload URL
//	GET    /api/v1/uploads/status[/{id}]        one session / the list form
//	POST   /api/v1/uploads/complete/{id}        checksum-gated node landing
//	POST   /api/v1/uploads/abort/{id}           discard
//	PUT    /api/v1/uploads/part/{id}/{n}        the urlPart target
//
// The plane reuses the storage Session face verbatim (the section 15.4
// ruling: BeginSession-family -> Append per part -> Commit at complete,
// zero storage-kernel changes): every part PUT streams through
// Session.Append — BinFlow relays the bytes into the S3 multipart upload,
// so the client never needs S3 credentials, the bucket endpoint stays
// private, and the checksum chain (sha256/sha1/md5) is computed server-side
// exactly as on every other upload path. The "part URL" the urlPart verb
// hands out is therefore a BinFlow URL whose capability is the session id
// (the section 5.3.1 contract-4 semantics: an unguessable uuid; the
// authorization door stays the target repository's `w`, checked per
// request).
//
// Backend honesty (FR-90-AC3): the plane exists only where the engine
// carries storage.MultipartUploads — a pure-S3 assembly. A filestore (or
// dual-write) instance wires no seam and EVERY endpoint answers the plain
// -text 501 below, never a 404 masquerading as "no such route": a client
// can tell "absent on this backend" from "not a BinFlow endpoint".
//
// Restart visibility (T-323R, closing the T-323 intersection register): the
// plane's sessions survive a restart wherever the seam carries
// storage.MultipartUploadContexts. The create verb then persists the
// plane's protocol coordinates (repoKey/path/mime/part size/creator) as an
// opaque blob inside the engine's own upload_sessions row, and a registry
// miss on any per-session verb re-materializes the session through
// ResumeSessionContext — the docker adapter's T-216 lazy-rebuild posture
// (kill -9 and graceful SIGTERM both leave the row and the server-side
// multipart state behind, ADR-0028). Sessions opened before the context
// pair (or by another upload plane) carry no blob and keep the plain 404.
// The bare status LIST stays a view of what THIS process knows: a
// restarted session appears there only after one of its verbs has addressed
// it. The S3-side residue of an abandoned session is reclaimed by the
// engine's startup + maintenance sweeps (T-203 D-6, T-324) plus the idle
// sweep here.

// mpuEndpointBase is the route prefix of the plane (under /binflow/api).
const mpuEndpointBase = "/binflow/api/v1/uploads"

// mpuIdleTTL bounds how long an untouched session may linger before the
// sweep aborts it — a browser upload abandoned mid-flight otherwise holds
// an S3 multipart upload (and its uploaded parts' storage) until the next
// restart sweep. Mirrors storage.DefaultSessionTTL so the REST plane's
// in-process eviction and the engine's startup sweep agree on one number;
// the sweep cadence is the docker registry's (TTL/48).
const (
	mpuIdleTTL        = storage.DefaultSessionTTL
	mpuSweepInterval  = mpuIdleTTL / 48
	mpuMaxPartSizeMB  = int64(5 * 1024) // S3's per-part ceiling, fail-fast
	mpuMaxSessionPath = 512             // adapter.MaxRelPathLen, restated to keep httpapi leaf-ward imports as-is
)

// Session lifecycle states of the wire body.
const (
	mpuStateActive   = "active"            // accepting parts
	mpuStateFinal    = "awaiting-complete" // a short (last) part was accepted
	mpuStateFailed   = "failed"            // append/commit broke; abort to reclaim
	mpuStateComplete = "completed"         // landed as a node (echo bodies only)
)

// mpuSession is one REST-plane upload session: the engine session plus the
// protocol state this plane owns (part accounting, target coordinates,
// timestamps). Every field is guarded by mu, and mu serializes the WHOLE
// per-session operation (alignment check + Append + bookkeeping), the
// docker liveUpload review-B1 posture: two concurrent part PUTs cannot
// both pass the ordering gate against a stale counter.
type mpuSession struct {
	mu        sync.Mutex
	id        string // the REST session id (the engine session id at create)
	sess      storage.Session
	repoKey   string
	path      string
	mime      string
	partSize  int64
	received  int64 // mirror; the authority is sess.Offset()
	parts     int   // parts accepted so far
	state     string
	createdBy string
	createdAt time.Time
	updatedAt time.Time
}

// mpuRegistry is the process-wide session table: id -> session. The table
// is in-memory, but a lookup miss is no longer the answer (T-323R):
// resumeFromStore lazily re-materializes the session from the engine's
// persisted row — the coordinates the create verb persisted plus the
// server-side multipart state — which is what makes an in-flight upload
// visible across a restart. Entries leave through complete, abort or the
// idle sweep, and the engine's startup + maintenance sweeps remain the
// crash-window backstop for whatever the process lost.
type mpuRegistry struct {
	mu   sync.Mutex
	byID map[string]*mpuSession
	// resuming tracks in-flight lazy rebuilds (the per-id single-flight
	// funnel, the docker T-216 posture): the engine resolves same-id
	// concurrent resumes last-writer-wins, so two racing first requests must
	// not both reach it. Guarded by mu.
	resuming  map[string]*mpuResumeAttempt
	ttl       time.Duration
	sweepOnce sync.Once
}

// mpuResumeAttempt is one in-flight lazy rebuild: exactly one caller (the
// "flyer") runs ResumeSessionContext while every other request for the same
// id waits on done and consumes the same outcome.
type mpuResumeAttempt struct {
	done chan struct{} // closed exactly once, after sess/err are final
	sess *mpuSession   // set on success
	err  error         // set on failure (may wrap storage.ErrSessionNotFound)
}

func newMPURegistry() *mpuRegistry {
	return &mpuRegistry{
		byID:     map[string]*mpuSession{},
		resuming: map[string]*mpuResumeAttempt{},
		ttl:      mpuIdleTTL,
	}
}

// startSweep launches the idle-eviction loop once per process.
func (r *mpuRegistry) startSweep() {
	r.sweepOnce.Do(func() {
		go func() {
			t := time.NewTicker(mpuSweepInterval)
			defer t.Stop()
			for range t.C {
				r.evictIdle(time.Now())
			}
		}()
	})
}

// evictIdle aborts and drops every session idle beyond the TTL.
//
// Lock order (B1, T-289 review): the registry lock is NEVER held across a
// session lock, and no session lock is ever held while taking the registry
// lock — the two orders this sweep used to mix were a whole-plane deadlock
// (config's failure arm) and a sweep that stalled every lookup/add/remove
// for the length of one long Append. The sweep therefore only SNAPSHOTS
// pointers under r.mu, then re-checks idleness under each session's own
// lock: a session touched since the snapshot (or mid-terminal-verb) has a
// fresh updatedAt and survives; an already-removed id's delete is a no-op.
// Abort of an already-finalized session is a storage-contract no-op, so a
// race with a legitimate complete can never destroy live state either.
func (r *mpuRegistry) evictIdle(now time.Time) []string {
	r.mu.Lock()
	candidates := make([]*mpuSession, 0, len(r.byID))
	for _, s := range r.byID {
		candidates = append(candidates, s)
	}
	r.mu.Unlock()

	var ids []string
	for _, s := range candidates {
		s.mu.Lock()
		if now.Sub(s.updatedAt) <= r.ttl {
			s.mu.Unlock()
			continue // touched since the snapshot: keep
		}
		_ = s.sess.Abort(context.Background())
		s.state = mpuStateFailed
		s.mu.Unlock()
		r.mu.Lock()
		delete(r.byID, s.id) // no-op if a terminal verb removed it meanwhile
		r.mu.Unlock()
		ids = append(ids, s.id)
	}
	return ids
}

// add registers a session under its REST id.
func (r *mpuRegistry) add(s *mpuSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[s.id] = s
}

// lookup resolves one id; ok=false enters the lazy-rebuild lane (see
// resumeFromStore), not directly the plane's 404.
func (r *mpuRegistry) lookup(id string) (*mpuSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byID[id]
	return s, ok
}

// remove drops a finished/aborted session; safe twice.
func (r *mpuRegistry) remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
}

// resumeFromStore is the restart-visibility lane (T-323R, the docker
// adapter's T-216 funnel adapted to this plane): the in-registry fast path
// first, then — on a miss — the lazy rebuild from the engine's persisted
// row. No start-up preloading: the rebuild happens on the first request
// that addresses the id.
//
// The rebuild is funneled per id for the same reason the docker registry
// funnels its own: the engine resolves same-id concurrent resumes
// last-writer-wins (the superseded handle's next Append fails "already
// finalized"), so two racing first requests must not both reach it. The
// first miss registers an attempt and becomes the single flyer; the rest
// wait for its outcome and reuse the winner's session. The rebuild itself
// runs OUTSIDE r.mu on purpose (ResumeSession does S3 I/O — a ListParts
// walk — and must not stall unrelated lookups). Waiters do not bail on
// their own request context: the flyer's rebuild is shared state.
//
// Error mapping: storage.ErrSessionNotFound — an unknown id, an
// expired-but-unswept row (fail-closed), a row without plane coordinates,
// or a seam that cannot rebuild — is returned as-is for the caller's 404.
// Any other engine failure is returned verbatim and NOTHING is cached, so
// the next request retries the rebuild from scratch; only a coordinate
// blob this plane cannot use aborts the engine session (the row is this
// plane's own — only the context begin writes a blob — and its coordinates
// are damaged beyond interpretation).
func (r *mpuRegistry) resumeFromStore(ctx context.Context, seam storage.MultipartUploadContexts, id string) (*mpuSession, error) {
	if s, ok := r.lookup(id); ok {
		return s, nil
	}
	r.mu.Lock()
	// Double-check under the lock: a rebuild race's flyer may have published
	// between the fast lookup and here.
	if s, ok := r.byID[id]; ok {
		r.mu.Unlock()
		return s, nil
	}
	if att, ok := r.resuming[id]; ok {
		r.mu.Unlock()
		<-att.done // the funnel: reuse the flyer's outcome
		return att.sess, att.err
	}
	att := &mpuResumeAttempt{done: make(chan struct{})}
	r.resuming[id] = att
	r.mu.Unlock()

	// Single flyer. ctx arrives WithoutCancel from the caller: once waiters
	// share the attempt, the rebuild must not die with the request that
	// triggered it.
	sess, caller, err := seam.ResumeSessionContext(ctx, id)
	r.mu.Lock()
	delete(r.resuming, id)
	if err == nil {
		ms, perr := mpuSessionFromStore(id, sess, caller)
		if perr == nil {
			att.sess = ms
			r.byID[id] = ms
		} else {
			err = perr
			_ = sess.Abort(ctx) // this plane's own row, its coordinates unusable
		}
	}
	att.err = err
	r.mu.Unlock()
	close(att.done)
	return att.sess, att.err
}

// snapshot lists every live session sorted by session id (deterministic
// wire order for the bare status arm — id order, not age: age is not a
// claim this table makes).
func (r *mpuRegistry) snapshot() []*mpuSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*mpuSession, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// ---- persisted plane state (T-323R) ----

// mpuCallerStateVersion pins the persisted coordinate blob's shape.
const mpuCallerStateVersion = 1

// mpuCallerState is the protocol coordinate blob the create verb persists
// inside the engine's upload_sessions row (opaque to the engine, verbatim
// both ways) — the facts this plane owns that no engine state carries:
// where a completed blob lands (repoKey/path), its mime, the resolved part
// size the part-alignment contract judges against, and the echo telemetry
// (createdBy/createdAt). The part ACCOUNTING is deliberately absent: parts
// and the awaiting-complete state derive from the engine session's
// authoritative Offset against the persisted part size (every non-final
// part is exactly partSize; a short part closes the stream), so the row
// can never disagree with the bytes.
type mpuCallerState struct {
	Version   int    `json:"version"`
	RepoKey   string `json:"repoKey"`
	Path      string `json:"path"`
	MimeType  string `json:"mimeType"`
	PartSize  int64  `json:"partSizeBytes"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt"` // RFC3339 UTC
}

// mpuSessionFromStore rebuilds the plane's session view from the engine
// session plus the persisted coordinate blob: offset, part count and state
// derive from the engine's Offset (the authority), the coordinates come
// back verbatim, and the idle clock restarts at the resume (the docker
// T-216 posture — the anchor for a session this process only just
// re-materialized). A blob this plane cannot use (wrong shape, missing
// coordinates) fails closed with storage.ErrSessionNotFound: the session is
// not resumable through THIS plane, whatever the engine could rebuild.
func mpuSessionFromStore(id string, sess storage.Session, caller []byte) (*mpuSession, error) {
	notFound := func(why string) error {
		return fmt.Errorf("httpapi: mpu session %s: %w: %s", id, storage.ErrSessionNotFound, why)
	}
	var cs mpuCallerState
	if err := json.Unmarshal(caller, &cs); err != nil {
		return nil, notFound("persisted plane state is unreadable")
	}
	if cs.RepoKey == "" || cs.Path == "" || cs.PartSize <= 0 {
		return nil, notFound("persisted plane state lacks its coordinates")
	}
	if cs.MimeType == "" {
		cs.MimeType = "application/octet-stream" // the create's own default, belt for hand-seeded rows
	}
	createdAt := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339, cs.CreatedAt); err == nil {
		createdAt = t
	}
	ms := &mpuSession{
		id:        id,
		sess:      sess,
		repoKey:   cs.RepoKey,
		path:      cs.Path,
		mime:      cs.MimeType,
		partSize:  cs.PartSize,
		state:     mpuStateActive,
		createdBy: cs.CreatedBy,
		createdAt: createdAt,
		updatedAt: time.Now().UTC(),
	}
	// The engine's offset is the resume authority: a restarted session
	// resumes at the last FLUSHED part boundary (bytes that lived only in
	// the engine's pending part buffer die with the process), so the derived
	// accounting is exact by construction — parts are ceil(offset/partSize)
	// and a non-partSize-multiple offset means a short (final) part had
	// been accepted.
	ms.received = sess.Offset()
	if ms.received > 0 {
		ms.parts = int((ms.received + ms.partSize - 1) / ms.partSize)
		if ms.received%ms.partSize != 0 {
			ms.state = mpuStateFinal
		}
	}
	return ms, nil
}

// ---- wire shapes ----

// mpuCreateRequest is the create/config body.
type mpuCreateRequest struct {
	RepoKey    string `json:"repoKey"`
	Path       string `json:"path"`
	PartSizeMB int64  `json:"partSizeMB"`
	MimeType   string `json:"mimeType"`
}

// mpuConfigRequest re-parts one session.
type mpuConfigRequest struct {
	SessionID  string `json:"sessionId"`
	PartSizeMB int64  `json:"partSizeMB"`
}

// mpuCompleteRequest carries the checksums complete verifies.
type mpuCompleteRequest struct {
	Sha256 string `json:"sha256"`
	Sha1   string `json:"sha1"`
	Md5    string `json:"md5"`
}

// mpuSessionBody is the session echo (create/config/urlPart/status/part).
type mpuSessionBody struct {
	SessionID     string `json:"sessionId"`
	RepoKey       string `json:"repoKey"`
	Path          string `json:"path"`
	State         string `json:"state"`
	PartSizeBytes int64  `json:"partSizeBytes"`
	ReceivedBytes int64  `json:"receivedBytes"`
	PartsReceived int    `json:"partsReceived"`
	NextPart      int    `json:"nextPartNumber"`
	CreatedBy     string `json:"createdBy,omitempty"`
	CreatedAt     string `json:"createdAt,omitempty"`
	UpdatedAt     string `json:"updatedAt,omitempty"`
	// URL family: filled on create/config/urlPart (the caller is bootstrapping
	// its part loop); the status echoes stay lean.
	URLPartURI  string `json:"urlPartUri,omitempty"`
	PartURI     string `json:"partUploadUri,omitempty"`
	StatusURI   string `json:"statusUri,omitempty"`
	CompleteURI string `json:"completeUri,omitempty"`
	AbortURI    string `json:"abortUri,omitempty"`
}

// mpuPartURLBody is the urlPart answer.
type mpuPartURLBody struct {
	SessionID   string `json:"sessionId"`
	PartNumber  int    `json:"partNumber"`
	OffsetBytes int64  `json:"offsetBytes"`
	URL         string `json:"url"`
}

// mpuCompleteBody is the complete answer.
type mpuCompleteBody struct {
	SessionID   string          `json:"sessionId"`
	RepoKey     string          `json:"repoKey"`
	Path        string          `json:"path"`
	State       string          `json:"state"`
	Size        int64           `json:"size"`
	DownloadURI string          `json:"downloadUri"`
	Checksums   *checksumTriple `json:"checksums"`
}

// bodyOf renders one session's echo. urls controls the URL family (create,
// config and urlPart carry them; status stays lean).
func (s *mpuSession) bodyOf(r *http.Request, urls bool) mpuSessionBody {
	b := mpuSessionBody{
		SessionID:     s.id,
		RepoKey:       s.repoKey,
		Path:          s.path,
		State:         s.state,
		PartSizeBytes: s.partSize,
		ReceivedBytes: s.received,
		PartsReceived: s.parts,
		NextPart:      s.parts + 1,
		CreatedBy:     s.createdBy,
		CreatedAt:     s.createdAt.UTC().Format(time.RFC3339),
		UpdatedAt:     s.updatedAt.UTC().Format(time.RFC3339),
	}
	if urls {
		base := requestBase(r) + mpuEndpointBase
		b.URLPartURI = fmt.Sprintf("%s/urlPart/%s/{partNumber}", base, s.id)
		b.PartURI = fmt.Sprintf("%s/part/%s/{partNumber}", base, s.id)
		b.StatusURI = base + "/status/" + s.id
		b.CompleteURI = base + "/complete/" + s.id
		b.AbortURI = base + "/abort/" + s.id
	}
	return b
}

// ---- shared gates ----

// uploadsBackendAbsent renders the honest 501 (FR-90-AC3): plain text that
// names the S3-only fact — deliberately not the envelope 404's wording, so
// "absent on this backend" stays distinguishable from "not a BinFlow
// endpoint", and not a JSON envelope, so scripted clients grep one line.
func writeUploadsUnavailable(w http.ResponseWriter) {
	writePlainText(w, http.StatusNotImplemented,
		"multipart uploads REST API is not supported on this backend (S3 storage backend required)")
}

// resolveMPUPartSize maps a partSizeMB request value onto the effective
// byte size: 0 keeps the engine default, values below the S3 multipart
// floor clamp up (the engine's own resolveS3PartSize table), values above
// the S3 per-part ceiling fail fast. ok=false answers a 400.
func resolveMPUPartSize(partSizeMB int64) (int64, bool) {
	switch {
	case partSizeMB < 0:
		return 0, false
	case partSizeMB == 0:
		return storage.DefaultS3PartSize, true
	case partSizeMB > mpuMaxPartSizeMB:
		// BEFORE the shift (B3, T-289 review): partSizeMB >= 2^43 overflows
		// int64 under << 20, wraps negative and used to sail through the
		// below-floor clamp as a "tiny" size. The megabyte-domain bound
		// makes the byte-domain check trivially safe.
		return 0, false
	}
	bytes := partSizeMB << 20
	if bytes < storage.MinS3PartSize {
		bytes = storage.MinS3PartSize
	}
	return bytes, true
}

// mpuUploadsPathError is the create-path validation: no traversal, no
// folder spelling, no matrix parameters (the MPU plane deploys plain
// files; matrix-parameter deploys ride the content PUT, T-286), bounded
// length, no empty segment.
func mpuUploadsPathError(path string) string {
	switch {
	case path == "":
		return "path is required"
	case strings.HasPrefix(path, "/"):
		return "path must be repository-relative (no leading slash)"
	case strings.HasSuffix(path, "/"):
		return "path must name a file (no trailing slash)"
	case strings.Contains(path, ";"):
		return "path must not carry matrix parameters; deploy properties through the content PUT"
	case len(path) > mpuMaxSessionPath:
		return "path exceeds the 512-character artifact-path limit"
	}
	for _, seg := range strings.Split(path, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "path has an empty, '.' or '..' segment"
		}
	}
	return ""
}

// uploadsWriteGate is the plane's per-request door: the target repository
// path's `w` through the same Authorizer the content plane consults
// (section 15.4: "required + 目标仓 w"). Checked on create against the
// body coordinates and on every per-session verb against the session's —
// the id is a capability, not an authorization.
func (s *Server) uploadsWriteGate(r *http.Request, repoKey, path string) bool {
	p := principalFrom(r.Context())
	return s.deps.Authz.Can(r.Context(), p, repoKey, path, auth.ActionWrite)
}

// resolveMPUSession is the per-session verbs' shared entry: backend gate,
// registry lookup, the lazy rebuild a restart miss triggers, write door.
// Failures render here and return ok=false.
func (s *Server) resolveMPUSession(w http.ResponseWriter, r *http.Request, id string) (*mpuSession, bool) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return nil, false
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "upload session not found")
		return nil, false
	}
	sess, ok := s.uploads.lookup(id)
	if !ok {
		var err error
		sess, err = s.resumeMPUSession(w, r, id)
		if err != nil {
			return nil, false
		}
	}
	if !s.uploadsWriteGate(r, sess.repoKey, sess.path) {
		writeError(w, http.StatusForbidden,
			"permission denied: multipart uploads require write access on the target path")
		return nil, false
	}
	return sess, true
}

// resumeMPUSession is the restart-visibility lane's server half (T-323R):
// the seam must carry storage.MultipartUploadContexts (a seam without it
// keeps the pre-T-323R process-only posture — the plain 404), the rebuild
// rides the registry's per-id funnel, and only the outcome rendering lives
// here. The unknown-id wording stays the plane's one 404: a session this
// process never saw, one whose row expired (fail-closed), one opened
// before the context pair or by another upload plane, and an aborted
// session are all the client's same re-create cue.
func (s *Server) resumeMPUSession(w http.ResponseWriter, r *http.Request, id string) (*mpuSession, error) {
	seam, ok := s.deps.Uploads.(storage.MultipartUploadContexts)
	if !ok {
		writeError(w, http.StatusNotFound, "upload session not found: "+id)
		return nil, fmt.Errorf("resume unavailable")
	}
	// Once waiters share the attempt the rebuild must not die with the
	// request that triggered it.
	ms, err := s.uploads.resumeFromStore(context.WithoutCancel(r.Context()), seam, id)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "upload session not found: "+id)
			return nil, err
		}
		s.log.ErrorContext(r.Context(), "httpapi: mpu session resume failed",
			"session", id, "error", err.Error())
		if errors.Is(err, storage.ErrEngineClosed) {
			writeError(w, http.StatusServiceUnavailable,
				"resuming the upload session failed (storage is shutting down); retry shortly")
			return nil, err
		}
		writeError(w, http.StatusInternalServerError,
			"resuming the upload session failed; retry")
		return nil, err
	}
	ms.mu.Lock()
	off, repo, path := ms.received, ms.repoKey, ms.path
	ms.mu.Unlock()
	s.log.InfoContext(r.Context(), "httpapi: mpu session re-materialized from storage (restart resume)",
		"session", id, "repo", repo, "path", path, "offset", off)
	return ms, nil
}

// ---- endpoint handlers ----

// handleUploadsCreate serves POST /api/v1/uploads/create: open a session
// against (repoKey, path) with an optional part size. 201 + the session
// echo carrying the URL family.
func (s *Server) handleUploadsCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return
	}
	var req mpuCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed create body: "+err.Error())
		return
	}
	req.RepoKey = strings.TrimSpace(req.RepoKey)
	req.Path = strings.TrimSpace(req.Path)
	if req.RepoKey == "" {
		writeError(w, http.StatusBadRequest, "repoKey is required")
		return
	}
	if msg := mpuUploadsPathError(req.Path); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	// The repository gate mirrors the storage plane's order: unknown repo
	// 404, non-local 400, then this plane's own ruling — protocol-managed
	// layouts (docker/maven/npm/pypi/go/nuget) take their own protocol
	// upload paths, and an MPU landing arbitrary nodes under them would
	// write outside the layout those adapters own. Generic local repos are
	// the MPU plane's whole address space (T-289 ruling, see the log).
	row, err := s.deps.Repos.Get(r.Context(), req.RepoKey)
	if err != nil {
		s.writeRepoLookupError(w, req.RepoKey, err)
		return
	}
	if row.Type != repo.TypeLocal {
		writeError(w, http.StatusBadRequest,
			"multipart uploads are only supported on local repositories (requested repository type: "+row.Type+")")
		return
	}
	if row.PackageType != repo.PackageGeneric {
		writeError(w, http.StatusBadRequest,
			"multipart uploads are only supported on generic repositories (requested package type: "+row.PackageType+"); protocol repositories take their own upload paths")
		return
	}
	if !s.uploadsWriteGate(r, req.RepoKey, req.Path) {
		writeError(w, http.StatusForbidden,
			"permission denied: multipart uploads require write access on the target path")
		return
	}
	partSize, ok := resolveMPUPartSize(req.PartSizeMB)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"partSizeMB must be between 0 (engine default) and %d", mpuMaxPartSizeMB))
		return
	}
	mime := strings.TrimSpace(req.MimeType)
	if mime == "" {
		mime = "application/octet-stream"
	}

	now := time.Now().UTC()
	p := principalFrom(r.Context())
	createdBy := ""
	if p != nil {
		createdBy = p.Name
	}
	// The protocol coordinates ride the engine's own session row (T-323R):
	// the context begin persists them as an opaque blob, which is what makes
	// the session re-materializable through this plane after a restart. A
	// seam without the context pair (pre-T-323R shapes, narrow fakes) keeps
	// the plain begin and the process-only posture.
	caller, merr := json.Marshal(mpuCallerState{
		Version:   mpuCallerStateVersion,
		RepoKey:   req.RepoKey,
		Path:      req.Path,
		MimeType:  mime,
		PartSize:  partSize,
		CreatedBy: createdBy,
		CreatedAt: now.UTC().Format(time.RFC3339),
	})
	var sess storage.Session
	err = merr // json.Marshal of scalars cannot fail; kept honest anyway
	if merr == nil {
		if cs, cok := s.deps.Uploads.(storage.MultipartUploadContexts); cok {
			sess, err = cs.BeginMultipartSessionContext(r.Context(), partSize, caller)
		} else {
			sess, err = s.deps.Uploads.BeginMultipartSession(r.Context(), partSize)
		}
	} else {
		err = merr
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: mpu create failed",
			"repo", req.RepoKey, "path", req.Path, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "opening the multipart upload session failed")
		return
	}
	ms := &mpuSession{
		id:        sess.ID(),
		sess:      sess,
		repoKey:   req.RepoKey,
		path:      req.Path,
		mime:      mime,
		partSize:  partSize,
		state:     mpuStateActive,
		createdBy: createdBy,
		createdAt: now,
		updatedAt: now,
	}
	s.uploads.add(ms)
	s.uploads.startSweep()
	// The session is published: a racing part PUT can arrive between add and
	// this echo, so the mutable fields bodyOf reads serialize under ms.mu
	// like every other reader.
	ms.mu.Lock()
	body := ms.bodyOf(r, true)
	ms.mu.Unlock()
	writeJSONBody(w, http.StatusCreated, body)
}

// handleUploadsConfig serves POST /api/v1/uploads/config: re-part a
// session that has received no bytes. The part boundary is engine-session
// state, so the change swaps the underlying engine session (zero bytes
// were received; the swap is semantically invisible) while the REST id —
// the capability every URL carries — stays put.
func (s *Server) handleUploadsConfig(w http.ResponseWriter, r *http.Request) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return
	}
	var req mpuConfigRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed config body: "+err.Error())
		return
	}
	ms, ok := s.resolveMPUSession(w, r, strings.TrimSpace(req.SessionID))
	if !ok {
		return
	}
	partSize, ok := resolveMPUPartSize(req.PartSizeMB)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"partSizeMB must be between 0 (engine default) and %d", mpuMaxPartSizeMB))
		return
	}

	// Every exit below unlocks EXPLICITLY: the failure arm must release the
	// session lock BEFORE touching the registry (B1, T-289 review — the
	// registry lock is never taken while a session lock is held), which a
	// deferred unlock would make impossible.
	ms.mu.Lock()
	if ms.state == mpuStateFailed {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"the session is in a failed state; abort it and start a new upload")
		return
	}
	if ms.state != mpuStateActive || ms.received != 0 || ms.parts != 0 {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"the session already carries bytes; part size is fixed once uploading starts (abort and re-create to change it)")
		return
	}
	if partSize == ms.partSize {
		body := ms.bodyOf(r, true)
		ms.mu.Unlock()
		writeJSONBody(w, http.StatusOK, body)
		return
	}
	// Swap: the old engine session is discarded unconditionally (WithoutCancel
	// — Abort is cleanup and must not die with this request), the new one
	// opens at the requested size. An open failure marks the REST session
	// failed and drops it — the old engine session is already gone, so the
	// entry would only ever answer 409s. The registry remove runs AFTER the
	// unlock (the B1 lock order).
	_ = ms.sess.Abort(context.WithoutCancel(r.Context()))
	sess, err := s.deps.Uploads.BeginMultipartSession(context.WithoutCancel(r.Context()), partSize)
	if err != nil {
		ms.state = mpuStateFailed
		ms.mu.Unlock()
		s.uploads.remove(ms.id)
		s.log.ErrorContext(r.Context(), "httpapi: mpu config swap failed",
			"session", ms.id, "partSize", partSize, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "re-opening the multipart upload session failed; abort and re-create")
		return
	}
	ms.sess = sess
	ms.partSize = partSize
	ms.updatedAt = time.Now().UTC()
	body := ms.bodyOf(r, true)
	ms.mu.Unlock()
	writeJSONBody(w, http.StatusOK, body)
}

// handleUploadsURLPart serves GET /api/v1/uploads/urlPart/{id}/{n}: the
// URL part n PUTs to, plus the offset the part must start at. Part numbers
// are 1-based and strictly sequential — the Session face is an ordered
// append stream, so this plane's contract is "part n lands at offset
// (n-1)*partSize"; the PUT enforces it, this verb only states it (a
// lookahead answer is a URL like any other; the ordering gate lives where
// the bytes do).
func (s *Server) handleUploadsURLPart(w http.ResponseWriter, r *http.Request, id string, part int) {
	if part < 1 {
		writeError(w, http.StatusBadRequest, "partNumber must be a positive integer")
		return
	}
	ms, ok := s.resolveMPUSession(w, r, id)
	if !ok {
		return
	}
	ms.mu.Lock()
	state, partSize := ms.state, ms.partSize
	ms.mu.Unlock()
	if state == mpuStateComplete {
		writeError(w, http.StatusConflict, "the session is already completed")
		return
	}
	base := requestBase(r) + mpuEndpointBase
	writeJSONBody(w, http.StatusOK, mpuPartURLBody{
		SessionID:   ms.id,
		PartNumber:  part,
		OffsetBytes: int64(part-1) * partSize,
		URL:         fmt.Sprintf("%s/part/%s/%d", base, ms.id, part),
	})
}

// handleUploadsStatus serves GET /api/v1/uploads/status/{id} (one session)
// and the bare form (the list view). The list is FILTERED per session by
// the same write gate the single-session arm walks (B4, T-289 review): a
// plain authenticated caller otherwise enumerated every live session's
// repoKey/path/createdBy — including repositories it holds no grant on,
// below the RBAC floor the single-session arm already enforces. A caller
// without a matching grant sees an empty list, not a 403: the arm's
// answer is a filtered view (the /api/v1/storage/usage family's silent-
// exclusion posture), and session ids stay unguessable capabilities.
func (s *Server) handleUploadsStatus(w http.ResponseWriter, r *http.Request, id string) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return
	}
	if id == "" {
		live := s.uploads.snapshot()
		out := make([]mpuSessionBody, 0, len(live))
		for _, ms := range live {
			if !s.uploadsWriteGate(r, ms.repoKey, ms.path) {
				continue
			}
			ms.mu.Lock()
			out = append(out, ms.bodyOf(r, false))
			ms.mu.Unlock()
		}
		writeJSONBody(w, http.StatusOK, out)
		return
	}
	ms, ok := s.resolveMPUSession(w, r, id)
	if !ok {
		return
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	writeJSONBody(w, http.StatusOK, ms.bodyOf(r, false))
}

// handleUploadsPart serves PUT /api/v1/uploads/part/{id}/{n} — the urlPart
// target. Contract: parts arrive strictly in order, each exactly
// partSizeBytes except the final one (which may be shorter and closes the
// part stream), Content-Length is mandatory (the size gates run before any
// byte reaches the engine), and the offset authority is the engine session
// (Session.Offset), re-checked before every append.
func (s *Server) handleUploadsPart(w http.ResponseWriter, r *http.Request, id string, part int) {
	if part < 1 {
		writeError(w, http.StatusBadRequest, "partNumber must be a positive integer")
		return
	}
	ms, ok := s.resolveMPUSession(w, r, id)
	if !ok {
		return
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()
	if ms.state == mpuStateComplete {
		writeError(w, http.StatusConflict, "the session is already completed")
		return
	}
	if ms.state == mpuStateFailed {
		writeError(w, http.StatusConflict, "the session is in a failed state; abort it and start a new upload")
		return
	}
	if ms.state == mpuStateFinal {
		writeError(w, http.StatusConflict,
			"a short final part was already accepted; complete the session (no further parts)")
		return
	}
	if want := ms.parts + 1; part != want {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"parts must upload in order: expected part %d, got %d", want, part))
		return
	}
	// The engine's own offset is the alignment authority (the [M7] rule):
	// the registry mirror agrees in every non-failure flow, and a drifted
	// mirror (a truncated stream the engine rejected, an engine swap)
	// must be caught here, not silently papered over.
	if off := ms.sess.Offset(); off != int64(part-1)*ms.partSize {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"session offset drift: part %d starts at %d bytes, session is at %d; query status and re-align",
			part, int64(part-1)*ms.partSize, off))
		return
	}
	n := r.ContentLength
	switch {
	case n < 0:
		// 411: the size gates below are pre-stream, so an unset length has
		// no honest path (reading "until it ends" would strip the oversize
		// and short-part gates of their input). No Content-Length header is
		// set here (B2, T-289 review): declaring 0 while writing the
		// errors[] body makes the server drop the body it just wrote.
		writeError(w, http.StatusLengthRequired,
			"part uploads require a Content-Length header")
		return
	case n > ms.partSize:
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"part %d is %d bytes; the session part size is %d (split it or raise partSizeMB)",
			part, n, ms.partSize))
		return
	}

	written, err := ms.sess.Append(r.Context(), r.Body)
	if err != nil {
		// The engine session is poisoned from here (storage contract); keep
		// the entry so the failure is observable and abort is reachable.
		ms.state = mpuStateFailed
		ms.updatedAt = time.Now().UTC()
		s.log.WarnContext(r.Context(), "httpapi: mpu part append failed",
			"session", ms.id, "part", part, "error", err.Error())
		writeError(w, http.StatusInternalServerError,
			"storing the part failed; the session must be aborted and the upload restarted")
		return
	}
	if written != int64(part-1)*ms.partSize+n {
		// The stream ended early (Append returned at EOF with fewer bytes
		// than declared): the bytes that DID arrive are durable session
		// state; report the honest offset and mark the session failed —
		// the part boundary contract cannot continue on a torn part.
		ms.received = written
		ms.state = mpuStateFailed
		ms.updatedAt = time.Now().UTC()
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"part %d ended early: %d of %d declared bytes arrived; abort the session and restart",
			part, written-int64(part-1)*ms.partSize, n))
		return
	}
	ms.received = written
	ms.parts = part
	ms.updatedAt = time.Now().UTC()
	if n < ms.partSize {
		ms.state = mpuStateFinal // a short part is by definition the last
	}
	writeJSONBody(w, http.StatusAccepted, ms.bodyOf(r, false))
}

// handleUploadsComplete serves POST /api/v1/uploads/complete/{id}: the
// checksum-gated landing. Commit verifies the declared digests against the
// streamed content (nothing lands on a mismatch — 409, the client-checksum
// posture of repo-semantics section 5), then repo.Service.PutLandedBlob
// writes the ledger row plus the node exactly as a generic deploy would.
// A registration failure after a durable Commit answers 5xx (never 201 —
// the docker writeBlobCreated ruling B4: the retry is safe end to end).
func (s *Server) handleUploadsComplete(w http.ResponseWriter, r *http.Request, id string) {
	ms, ok := s.resolveMPUSession(w, r, id)
	if !ok {
		return
	}
	var req mpuCompleteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed complete body: "+err.Error())
		return
	}
	if err := validateMPUChecksums(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ms.mu.Lock()
	if ms.state == mpuStateFailed {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, "the session is in a failed state; abort it and start a new upload")
		return
	}
	if ms.state == mpuStateComplete {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, "the session is already completed")
		return
	}
	ref, err := ms.sess.Commit(r.Context(), storage.BlobRef{
		Sha256: req.Sha256, Sha1: req.Sha1, Md5: req.Md5,
	})
	// Commit finalizes the engine session either way (storage contract):
	// the registry entry goes too, whatever happens next.
	ms.state = mpuStateComplete
	ms.updatedAt = time.Now().UTC()
	ms.mu.Unlock()
	s.uploads.remove(ms.id)
	if err != nil {
		if errors.Is(err, storage.ErrChecksumMismatch) {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"provided checksum did not match uploaded content: %s", err.Error()))
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: mpu commit failed",
			"session", ms.id, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "committing the multipart upload failed")
		return
	}
	node, err := s.deps.ReposSvc.PutLandedBlob(r.Context(), principalFrom(r.Context()),
		ms.repoKey, ms.path, ref, ms.mime)
	_ = node // the body echoes the BlobRef's own facts; the row adds nothing wire-visible
	if err != nil {
		var se *repo.StatusError
		if errors.As(err, &se) {
			// A governance refusal (quota, pattern) renders verbatim — the
			// status itself tells the client the artifact did not land.
			s.log.WarnContext(r.Context(), "httpapi: mpu node landing refused",
				"repo", ms.repoKey, "path", ms.path, "status", se.Code, "error", se.Message)
			writeError(w, se.Code, se.Message)
			return
		}
		// The blob is durable but no node row exists: 5xx is the retry
		// signal, and the retry is safe (Commit dedups; the idempotent
		// retransmit rule completes the rows).
		s.log.ErrorContext(r.Context(), "httpapi: mpu node landing failed",
			"repo", ms.repoKey, "path", ms.path, "error", err.Error())
		writeError(w, http.StatusInternalServerError,
			"the blob is stored but its repository record could not be written; the upload is safe to retry")
		return
	}
	writeJSONBody(w, http.StatusCreated, mpuCompleteBody{
		SessionID:   ms.id,
		RepoKey:     ms.repoKey,
		Path:        ms.path,
		State:       mpuStateComplete,
		Size:        ref.Size,
		DownloadURI: requestBase(r) + "/binflow/" + ms.repoKey + "/" + ms.path,
		Checksums: &checksumTriple{
			Sha1:   ref.Sha1,
			Md5:    ref.Md5,
			Sha256: ref.Sha256,
		},
	})
}

// handleUploadsAbort serves POST /api/v1/uploads/abort/{id}: discard the
// session and its S3 multipart state. 204; an unknown (or already
// aborted/completed) id is the plane's 404 — abort is not idempotent on
// the wire because the session id stops resolving the moment it leaves the
// registry (AC1's "abort 后 status 404" posture).
func (s *Server) handleUploadsAbort(w http.ResponseWriter, r *http.Request, id string) {
	ms, ok := s.resolveMPUSession(w, r, id)
	if !ok {
		return
	}
	ms.mu.Lock()
	_ = ms.sess.Abort(context.WithoutCancel(r.Context()))
	ms.state = mpuStateFailed
	ms.mu.Unlock()
	s.uploads.remove(ms.id)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNoContent)
}

// validateMPUChecksums normalizes and checks the declared digests before
// they reach Commit: uppercase hex is accepted and lowercased in place —
// the engine's normalizeHex is case-insensitive, and the REST face must
// not be stricter than the layer beneath it (T-289 review non-blocking).
// A malformed digest is a 400; a well-formed one that disagrees with the
// content is Commit's 409.
func validateMPUChecksums(req *mpuCompleteRequest) error {
	for _, chk := range []struct {
		name  string
		v     *string
		width int
	}{
		{"sha256", &req.Sha256, 64},
		{"sha1", &req.Sha1, 40},
		{"md5", &req.Md5, 32},
	} {
		v := strings.ToLower(strings.TrimSpace(*chk.v))
		*chk.v = v
		if v == "" {
			continue
		}
		if len(v) != chk.width || !isHexLower(v) {
			return fmt.Errorf("%s must be exactly %d hex characters", chk.name, chk.width)
		}
	}
	if req.Sha256 == "" {
		return errors.New("sha256 is required (the complete gate verifies the whole upload against it)")
	}
	return nil
}

// isHexLower reports whether s is non-empty lowercase hex.
func isHexLower(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// withUploadsID adapts a (w, r, id) handler to the route tail: exactly one
// non-empty segment after prefix, anything else is the E-26 404 (the
// withName posture — the plane's grammar, not a per-request error).
func (s *Server) withUploadsID(rest, prefix string, h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(rest, prefix)
		if id == "" || strings.Contains(id, "/") {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		h(w, r, id)
	}
}

// withUploadsPart is withUploadsID's two-segment twin for the
// {id}/{partNumber} tails (urlPart and part). A missing, non-numeric or
// extra-segment part number is the E-26 404; a numeric-but-invalid value
// (zero, negative) reaches the handler, which owns the honest 400.
func (s *Server) withUploadsPart(rest, prefix string, h func(http.ResponseWriter, *http.Request, string, int)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tail := strings.TrimPrefix(rest, prefix)
		id, num, found := strings.Cut(tail, "/")
		if !found || id == "" || num == "" || strings.Contains(num, "/") {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		h(w, r, id, n)
	}
}
