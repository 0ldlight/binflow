package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The multipart-upload REST plane (M10 T-289 as-built, wire flipped whole in
// M11 T-332 / ADR-0039 to the Artifactory shape — user ruling 2026-08-28
// item 2, evidence anchored in T-304 section 4.2). Six endpoints under
// /binflow/api/v1/uploads, every verb POST except the config probe:
//
//	POST /api/v1/uploads/create?repoKey=&repoPath=&partSizeMB=  open a session
//	GET  /api/v1/uploads/config                                 capability probe
//	POST /api/v1/uploads/urlPart?partNumber=N                   the part-n upload URL
//	POST /api/v1/uploads/status                                 the finish task's progress
//	POST /api/v1/uploads/complete?sha1=                         checksum-gated assembly (202)
//	POST /api/v1/uploads/abort                                  discard
//	PUT  /api/v1/uploads/part/{id}/{n}                          the urlPart target
//
// The engine seam is UNCHANGED from the T-323R posture (the flip is wire,
// not kernel): BeginMultipartSessionContext -> Append per part -> Commit at
// complete. Every part PUT still streams through Session.Append — BinFlow
// relays the bytes into the S3 multipart upload, so the client never needs
// S3 credentials and the checksum chain (sha256/sha1/md5) is computed
// server-side exactly as on every other upload path.
//
// The session token (ADR-0039 mapping 1): Artifactory hands create's caller
// an Access JWT whose extension carries the upload coordinates and whose
// scope (internal:mpu:x) is the only credential the five per-session verbs
// accept. BinFlow does not extend internal/auth for this, so the token is a
// self-resolving capability — "<32 random bytes hex>.<session id>" — whose
// sha256(random half) and expiry ride the engine's own persisted row inside
// the T-323R caller blob. A token-authenticated verb resolves the id from
// the token, re-materializes the session through ResumeSessionContext on a
// registry miss (restart resume survives the flip) and proves possession by
// the constant-time hash comparison; the authorization moment stays create
// (authenticated principal + the target repository's `w`), the
// possession-thereafter posture Artifactory's resource methods themselves
// keep (no per-verb canDeploy check in the reverse-engineered class).
//
// The complete verb is asynchronous on the wire (202): a finish task runs
// the synchronous engine Commit in a background goroutine and records the
// task state machine PARTS -> PROCESSING -> FINISHED (progress 100, the
// checksum-deploy token minted) / NON_RETRYABLE_ERROR (error) — the
// jfrog-client-go completionStatus vocabulary, verified against the real
// client. The artifact NODE is landed by the CLIENT: the FINISHED status
// carries a short-lived checksum-deploy token with which the client
// performs the zero-transfer X-Checksum-Deploy PUT on the content plane —
// the Artifactory flow, and the reason the checksum token exists at all.
// A blob the client never deploys is an unreferenced blob the GC's
// two-phase reclaim takes.
//
// The part PUT (the urlPart target) is S3-PutObject-shaped because that is
// what the real client drives: the URL carries the capability in its query
// string (protocol clients send NO Authorization header on it at all),
// parts arrive through a worker pool in ANY order (the plane absorbs the
// reordering with a bounded staging set, streaming the in-order fast path
// straight into the engine), and the answer is 200 — the client treats any
// other 2xx (202 included) as a part failure to retry away.
//
// Backend honesty (FR-90-AC3, unchanged): the five data endpoints exist
// only where the engine carries storage.MultipartUploads — a filestore (or
// dual-write) instance answers the plain-text 501, never a 404 masquerading
// as "no such route". The config probe is the exception by design: it
// answers 200 {"supported": false} — a probe that could not report "no"
// would be no probe.
//
// Crash windows (documented in ADR-0039 consequence 4): a kill -9 during
// part upload is the T-323R scenario — the row and the flushed parts
// survive, the token re-materializes the session on the restarted process.
// A kill -9 between complete's 202 and the finish task's end loses only the
// in-process task state: the row is unconsumed, status honestly reports
// PARTS again, and the client remedy is to re-issue complete (parts are
// durable; Commit dedups). The S3-side residue of an abandoned session is
// reclaimed by the engine's startup + maintenance sweeps plus the idle
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

// Token lifetimes, mirroring Artifactory's ConstantValues defaults
// (multipart.upload.token.expiry.secs = 2 days,
// multipart.checksum.deploy.token.expiry.secs = 5 minutes) — ADR-0039.
const (
	mpuTokenTTL          = 48 * time.Hour
	mpuChecksumTokenTTL  = 5 * time.Minute
	mpuTokenSecretBytes  = 32 // the unguessable half of the capability token
	mpuTokenSecretHexLen = mpuTokenSecretBytes * 2
	// mpuCLIMinVersion is the jfrog-cli-go version gate the config probe
	// applies (multipart.jfrog.cli.min.required.version). Below it the
	// probe answers supported:false so old clients fall back to monolithic
	// PUTs instead of driving a wire they mishandle.
	mpuCLIMinVersion = "2.62.2"
	mpuCLIPrefix     = "jfrog-cli-go/"
)

// Out-of-order part staging bounds (the S3 part-arrival semantics emulated
// over the engine's ordered Append stream, T-332 real-client finding):
// jfrog-cli uploads parts through a worker pool (--split-count, default 5),
// so part N+1 routinely arrives before part N. The plane absorbs the
// reordering with a BOUNDED in-memory staging set — the S3 posture a
// presigned-part client expects — while the in-order fast path keeps
// streaming straight into Session.Append (memory independent of part size).
// Beyond the bounds the plane refuses the leapfrog honestly (409): the
// client remedy is a smaller split count, and the memory ceiling stays
// independent of the artifact size.
const (
	mpuMaxStagedParts = 64
	mpuMaxStagedBytes = 128 << 20
)

// Session lifecycle states of the engine-backed session (the part stream).
const (
	mpuStateActive   = "active"            // accepting parts
	mpuStateFinal    = "awaiting-complete" // a short (last) part was accepted
	mpuStateFailed   = "failed"            // append/commit broke; abort to reclaim
	mpuStateComplete = "completed"         // assembly succeeded (echo bodies only)
)

// Finish-task states of the wire's async model (complete -> status). The
// vocabulary is ANCHORED on jfrog-client-go's public completionStatus set
// (the real client's own parse table — T-332 real-client verification):
// PARTS = parts still arriving (the client never polls there), PROCESSING
// = the merge is running (the client keeps polling), FINISHED = terminal
// success (progress 100 + the checksum-deploy token), NON_RETRYABLE_ERROR
// = fatal (the client aborts the whole upload). The client's remaining
// spellings — QUEUED, RETRYABLE_ERROR, ABORTED — are not emitted by this
// plane: the finish task starts immediately (no queue), its failures are
// never safely re-runnable (the engine session is consumed either way),
// and an aborted session stops resolving (the plain 404). progress is a
// PERCENT, not a byte count.
const (
	mpuTaskNone       = ""
	mpuTaskParts      = "PARTS"
	mpuTaskProcessing = "PROCESSING"
	mpuTaskFinished   = "FINISHED"
	mpuTaskFailed     = "NON_RETRYABLE_ERROR"
)

// mpuSession is one REST-plane upload session: the engine session plus the
// protocol state this plane owns (part accounting, target coordinates, the
// capability-token binding, the finish task). Every field is guarded by mu,
// and mu serializes the WHOLE per-session operation (alignment check +
// Append + bookkeeping), the docker liveUpload review-B1 posture: two
// concurrent part PUTs cannot both pass the ordering gate against a stale
// counter.
type mpuSession struct {
	mu      sync.Mutex
	id      string // the engine session id (the token's public half)
	sess    storage.Session
	repoKey string
	// clientRepoKey is the repo key THE CLIENT spelled at create ("" = same
	// as repoKey). It differs from repoKey exactly when a virtual's
	// defaultDeploymentRepo resolved the target: the client checksum-deploys
	// through ITS spelling (TestUploadsVirtualDefault), so the narrow
	// checksum-deploy token admits both (T-349).
	clientRepoKey string
	path          string
	mime          string
	partSize      int64
	received      int64          // mirror; the authority is sess.Offset()
	parts         int            // parts accepted so far
	staged        map[int][]byte // out-of-order arrivals awaiting their turn (bounded)
	stagedBy      int64          // total staged bytes
	state         string
	createdBy     string
	createdAt     time.Time
	updatedAt     time.Time
	// Capability binding (T-332): sha256 of the token's random half plus
	// its expiry, persisted in the caller blob and re-proved on every
	// token-authenticated verb.
	tokenSHA256 string
	tokenExpiry time.Time // zero = no expiry recorded (fail closed on use)
	// Finish task (complete's async half).
	taskState string
	taskErr   string
	taskToken string // the checksum-deploy token handed out at Finished
}

// mpuRegistry is the process-wide session table: id -> session. The table
// is in-memory, but a lookup miss is no longer the answer (T-323R):
// resumeFromStore lazily re-materializes the session from the engine's
// persisted row — the coordinates the create verb persisted plus the
// server-side multipart state — which is what makes an in-flight upload
// visible across a restart. Entries leave through abort or the idle sweep
// (complete KEEPS the entry: the finish task's state must stay observable
// through status until the sweep reclaims it, the Artifactory task-record
// posture), and the engine's startup + maintenance sweeps remain the
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
// lock. The sweep therefore only SNAPSHOTS pointers under r.mu, then
// re-checks idleness under each session's own lock: a session touched since
// the snapshot (or mid-terminal-verb) has a fresh updatedAt and survives;
// an already-removed id's delete is a no-op. Abort of an already-finalized
// session is a storage-contract no-op, so a race with a legitimate finish
// can never destroy live state either.
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

// ---- persisted plane state (T-323R, v2 in T-332) ----

// mpuCallerStateVersion pins the persisted coordinate blob's shape. v2 adds
// the capability-token binding (ADR-0039): v1 rows — opened by the pre-flip
// wire — carry no binding and fail closed under the token-addressed verbs
// (the retirement path: an in-flight session at upgrade time dies and the
// client re-uploads). v3 (T-349) adds the client's own repo spelling for the
// narrowed checksum-deploy token; v2 rows stay drivable — a post-restart
// mint on one narrows to the resolved key only (fail-closed).
const mpuCallerStateVersion = 3

// mpuCallerState is the protocol coordinate blob the create verb persists
// inside the engine's upload_sessions row (opaque to the engine, verbatim
// both ways) — the facts this plane owns that no engine state carries:
// where a completed blob's client deploy lands (repoKey/path), the resolved
// part size the part-alignment contract judges against, the echo telemetry
// (createdBy/createdAt) and, since T-332, the session token's sha256 half
// and expiry. The part ACCOUNTING is deliberately absent: parts and the
// awaiting-complete state derive from the engine session's authoritative
// Offset against the persisted part size (every non-final part is exactly
// partSize; a short part closes the stream), so the row can never disagree
// with the bytes.
type mpuCallerState struct {
	Version   int    `json:"version"`
	RepoKey   string `json:"repoKey"`
	Path      string `json:"path"`
	MimeType  string `json:"mimeType"`
	PartSize  int64  `json:"partSizeBytes"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt"` // RFC3339 UTC
	// T-332 (v2): sha256 of the capability token's random half plus its
	// expiry — what a restarted process re-proves possession against.
	TokenSHA256 string `json:"tokenSha256"`
	TokenExpiry string `json:"tokenExpiry"` // RFC3339 UTC
	// T-349 (v3): the client's own repo spelling when a virtual's
	// defaultDeploymentRepo resolved the target (the narrow checksum-deploy
	// token admits both). Absent on v2 rows: the restart mint narrows to the
	// resolved key only — fail-closed for the virtual spelling of an
	// upgrade-straddling session, which the upgrade window's clear-in-flight
	// policy already treats as best-effort.
	ClientRepoKey string `json:"clientRepoKey,omitempty"`
}

// mpuSessionFromStore rebuilds the plane's session view from the engine
// session plus the persisted coordinate blob: offset, part count and state
// derive from the engine's Offset (the authority), the coordinates come
// back verbatim, and the idle clock restarts at the resume (the docker
// T-216 posture — the anchor for a session this process only just
// re-materialized). A blob this plane cannot use (wrong shape, missing
// coordinates, missing the v2 token binding) fails closed with
// storage.ErrSessionNotFound: the session is not drivable through THIS
// plane's token verbs, whatever the engine could rebuild.
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
	if cs.TokenSHA256 == "" {
		// v1 rows (pre-T-332 wire) or foreign blobs: no token binding, no
		// token verbs — the retirement posture, fail closed.
		return nil, notFound("persisted plane state predates the session-token wire")
	}
	if cs.MimeType == "" {
		cs.MimeType = "application/octet-stream" // the create's own default, belt for hand-seeded rows
	}
	tokenExpiry := time.Time{}
	if t, err := time.Parse(time.RFC3339, cs.TokenExpiry); err == nil {
		tokenExpiry = t
	}
	createdAt := time.Now().UTC()
	if t, err := time.Parse(time.RFC3339, cs.CreatedAt); err == nil {
		createdAt = t
	}
	ms := &mpuSession{
		id:            id,
		sess:          sess,
		repoKey:       cs.RepoKey,
		clientRepoKey: cs.ClientRepoKey,
		path:          cs.Path,
		mime:          cs.MimeType,
		partSize:      cs.PartSize,
		staged:        map[int][]byte{},
		state:         mpuStateActive,
		createdBy:     cs.CreatedBy,
		createdAt:     createdAt,
		updatedAt:     time.Now().UTC(),
		tokenSHA256:   cs.TokenSHA256,
		tokenExpiry:   tokenExpiry,
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

// ---- wire shapes (T-332, the Artifactory records) ----

// mpuTokenBody is create's answer: CreateResponse(token).
type mpuTokenBody struct {
	Token string `json:"token"`
}

// mpuSupportedBody is config's answer: ConfigResponse(supported).
type mpuSupportedBody struct {
	Supported bool `json:"supported"`
}

// mpuURLBody is urlPart's answer: UrlResponse(url).
type mpuURLBody struct {
	URL string `json:"url"`
}

// mpuStatusTaskBody is status's answer: StatusResponse(status, error,
// progress, checksumToken) — all four keys always present, the nullable
// ones as JSON null exactly where the record's constructors leave them.
type mpuStatusTaskBody struct {
	Status        string  `json:"status"`
	Error         *string `json:"error"`
	Progress      int     `json:"progress"`
	ChecksumToken *string `json:"checksumToken"`
}

// mpuPartEcho is the part PUT's BinFlow echo (the relay target is BinFlow's
// own URL contract, not one of the six Artifactory endpoints — a lean
// session view for manual drivers; protocol clients check the 2xx).
type mpuPartEcho struct {
	SessionID     string `json:"sessionId"`
	RepoKey       string `json:"repoKey"`
	Path          string `json:"path"`
	State         string `json:"state"`
	PartSizeBytes int64  `json:"partSizeBytes"`
	ReceivedBytes int64  `json:"receivedBytes"`
	PartsReceived int    `json:"partsReceived"`
	NextPart      int    `json:"nextPartNumber"`
}

// bodyOf renders the lean session echo for the part PUT.
func (s *mpuSession) bodyOf() mpuPartEcho {
	return mpuPartEcho{
		SessionID:     s.id,
		RepoKey:       s.repoKey,
		Path:          s.path,
		State:         s.state,
		PartSizeBytes: s.partSize,
		ReceivedBytes: s.received,
		PartsReceived: s.parts,
		NextPart:      s.parts + 1,
	}
}

// ---- the capability token ----

// mpuBearerValue extracts the Authorization: Bearer value (scheme
// case-insensitive per RFC 7235); empty when the header carries another
// scheme or is absent.
func mpuBearerValue(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	scheme, rest, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(rest)
}

// parseMPUToken splits a presented credential into (secret, sessionID) when
// it carries this plane's capability shape "<64 lowercase hex>.<id>". A
// credential without the shape is not this plane's token — the caller falls
// back to the shared middleware verdict.
func parseMPUToken(tok string) (secret, sid string, ok bool) {
	i := strings.LastIndex(tok, ".")
	if i <= 0 || i+1 >= len(tok) {
		return "", "", false
	}
	secret, sid = tok[:i], tok[i+1:]
	if len(secret) != mpuTokenSecretHexLen || !isHexLower(secret) {
		return "", "", false
	}
	if sid == "" || len(sid) > 128 || strings.ContainsAny(sid, "/%#?") {
		return "", "", false
	}
	return secret, sid, true
}

// newMPUTokenSecret draws the unguessable half (crypto/rand, the tokenBytes
// posture of internal/auth: 256 bits, no dictionary surface) and returns it
// with its sha256 — the value the caller blob persists.
func newMPUTokenSecret() (secret, secretSHA string, err error) {
	buf := make([]byte, mpuTokenSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("httpapi: mpu token entropy: %w", err)
	}
	secret = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(sum[:]), nil
}

// mpuTokenIssuer is the auth.Service facet create's finish task mints the
// checksum-deploy token through (ADR-0039 mapping 2): a REAL API token the
// shared middleware verifies, so the client's X-Checksum-Deploy PUT — an
// ordinary content-plane request — authenticates with it. A deps.Auth
// without the facet leaves the Finished body's checksumToken null (logged);
// every production wiring carries *auth.Service.
//
// T-349 (FR-113.3) narrowed the mint: the token is issued CHECKSUM-DEPLOY
// SCOPED to the session's own (repoKey, repoPath), so the middleware admits
// the credential to exactly that one landing PUT and 403s every other use
// (NFR-S63 lateral-use defense; the pre-narrow full-power window was
// ADR-0039 residual 1). A deps.Auth still carrying only the broad Issue
// facet falls back to it (token present, scope unrestricted) — the seam
// degrades to the documented residual rather than breaking the flow.
type mpuTokenIssuer interface {
	Issue(ctx context.Context, username string, ttl time.Duration) (*auth.IssuedToken, error)
}

// mpuScopedTokenIssuer is the narrowed mint facet (T-349). Satisfied by
// *auth.Service alongside mpuTokenIssuer; probed first at mint time.
type mpuScopedTokenIssuer interface {
	IssueChecksumDeployScoped(ctx context.Context, username string, ttl time.Duration, repos []string, path string) (*auth.IssuedToken, error)
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

// uploadsWriteGate is create's authorization door: the target repository
// path's `w` through the same Authorizer the content plane consults. Under
// the T-332 wire this is THE per-upload check — the per-session verbs ride
// the capability (Artifactory's own posture: canDeploy at generateToken,
// scope-thereafter).
func (s *Server) uploadsWriteGate(r *http.Request, repoKey, path string) bool {
	p := principalFrom(r.Context())
	return s.deps.Authz.Can(r.Context(), p, repoKey, path, auth.ActionWrite)
}

// mpuForbiddenDeploy is Artifactory's create-refusal shape: unknown repo,
// remote repo, wrong role or no `w` all land here (ForbiddenException's
// exact message, MultipartUploadServiceImpl.generateToken).
const mpuForbiddenDeploy = "The user is not allowed to deploy to this location"

// mpuVirtualDefault extracts a virtual repository's defaultDeploymentRepo
// from its config blob (repo-semantics section 8.2 spelling; empty when the
// virtual does not carry one — the caller keeps the virtual key and the
// failure stays late, exactly Artifactory's getRepoPath behavior).
func mpuVirtualDefault(row *metadata.Repo) string {
	if row.Type != repo.TypeVirtual || row.Config == "" {
		return ""
	}
	var cfg struct {
		DefaultDeploymentRepo    string `json:"defaultDeploymentRepo"`
		DefaultDeploymentRepoRef string `json:"defaultDeploymentRepoRef"`
	}
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		return ""
	}
	if cfg.DefaultDeploymentRepo != "" {
		return cfg.DefaultDeploymentRepo
	}
	return cfg.DefaultDeploymentRepoRef
}

// resolveMPUTargetRepo maps the create coordinates onto the repository the
// session (and the write gate) address: a virtual with a
// defaultDeploymentRepo resolves to that member; everything else keeps the
// requested key. ok=false renders the refusal and returns the Artifactory
// 403 (unknown repo, remote repo, member lookup failure).
func (s *Server) resolveMPUTargetRepo(w http.ResponseWriter, r *http.Request, repoKey string) (string, bool) {
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil {
		// Artifactory's generateToken: repoExists false falls through to the
		// same ForbiddenException as a permission refusal — an unknown repo
		// is not a distinguished 404 on this endpoint.
		s.log.DebugContext(r.Context(), "httpapi: mpu create on unknown repository", "repo", repoKey)
		writeError(w, http.StatusForbidden, mpuForbiddenDeploy)
		return "", false
	}
	if row.Type == repo.TypeRemote {
		writeError(w, http.StatusForbidden, mpuForbiddenDeploy)
		return "", false
	}
	if def := mpuVirtualDefault(row); def != "" && def != repoKey {
		mrow, merr := s.deps.Repos.Get(r.Context(), def)
		if merr != nil || mrow.Type != repo.TypeLocal {
			writeError(w, http.StatusForbidden, mpuForbiddenDeploy)
			return "", false
		}
		return def, true
	}
	return repoKey, true
}

// resumeMPUSession is the restart-visibility lane's server half (T-323R):
// the seam must carry storage.MultipartUploadContexts (a seam without it
// keeps the process-only posture — the plain 404), the rebuild rides the
// registry's per-id funnel, and only the outcome rendering lives here. The
// unknown-id wording stays the plane's one 404: a session this process
// never saw, one whose row expired (fail-closed), one opened before the
// context pair or by another upload plane, and an aborted session are all
// the client's same re-create cue.
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
	off, repoKey, path := ms.received, ms.repoKey, ms.path
	ms.mu.Unlock()
	s.log.InfoContext(r.Context(), "httpapi: mpu session re-materialized from storage (restart resume)",
		"session", id, "repo", repoKey, "path", path, "offset", off)
	return ms, nil
}

// resolveMPUSessionByID is the id-keyed resolution lane the part PUT's
// regular-credential arm keeps (Basic/API-token drivers addressing the
// relay URL directly): registry lookup, lazy rebuild, write door.
func (s *Server) resolveMPUSessionByID(w http.ResponseWriter, r *http.Request, id string) (*mpuSession, bool) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return nil, false
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "upload session not found")
		return nil, false
	}
	ms, ok := s.uploads.lookup(id)
	if !ok {
		var err error
		ms, err = s.resumeMPUSession(w, r, id)
		if err != nil {
			return nil, false
		}
	}
	if !s.uploadsWriteGate(r, ms.repoKey, ms.path) {
		writeError(w, http.StatusForbidden,
			"permission denied: multipart uploads require write access on the target path")
		return nil, false
	}
	return ms, true
}

// mpuCredentialVerdict renders the shared middleware's own rejection for a
// presented-but-unverifiable credential that is NOT this plane's token —
// the dispatch-level capability-route exemption (router.go) hands the
// verdict here, and this keeps the presented-but-rejected-never-downgrades
// posture intact on these routes.
func mpuCredentialVerdict(w http.ResponseWriter, anonymous bool) {
	if anonymous {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	w.Header().Set("WWW-Authenticate", basicChallenge)
	writeError(w, http.StatusUnauthorized, "invalid credentials")
}

// resolveMPUSessionByToken is the capability lane the four token verbs (and
// the part PUT's Bearer arm) drive: parse the Bearer as this plane's token,
// resolve the session (registry + lazy rebuild — restart resume), prove
// possession against the persisted binding. The verdict ladder mirrors the
// reverse-engineered resource: a well-formed but unknown/mismatched
// capability is the plane's 404 (extractTokenExtension's NavigationException
// maps NotFoundException); a non-MPU credential is the shared 401/403; an
// expired binding is a credential refusal (401).
func (s *Server) resolveMPUSessionByToken(w http.ResponseWriter, r *http.Request) (*mpuSession, bool) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return nil, false
	}
	tok := mpuRequestToken(r)
	if tok == "" {
		// No token at all: anonymous on a capability route. A valid
		// non-token credential is NOT enough for these verbs (Artifactory
		// demands the internal:mpu:x scope) — 403 for a principal, the 401
		// challenge for anonymous.
		if principalFrom(r.Context()) != nil {
			writeError(w, http.StatusForbidden,
				"the multipart upload endpoints require the session token issued by create")
			return nil, false
		}
		if _, rejected := authRejectedFrom(r.Context()); rejected {
			mpuCredentialVerdict(w, false)
			return nil, false
		}
		mpuCredentialVerdict(w, true)
		return nil, false
	}
	secret, sid, ok := parseMPUToken(tok)
	if !ok {
		// A Bearer that is not this plane's shape: either a valid API token
		// (the middleware resolved a principal — but it holds no session
		// capability, the scope refusal) or a rejected credential (the
		// middleware's own verdict).
		if principalFrom(r.Context()) != nil {
			writeError(w, http.StatusForbidden,
				"the multipart upload endpoints require the session token issued by create")
			return nil, false
		}
		mpuCredentialVerdict(w, false)
		return nil, false
	}
	ms, ok := s.uploads.lookup(sid)
	if !ok {
		var err error
		ms, err = s.resumeMPUSession(w, r, sid)
		if err != nil {
			return nil, false
		}
	}
	ms.mu.Lock()
	binding, expiry := ms.tokenSHA256, ms.tokenExpiry
	ms.mu.Unlock()
	sum := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(binding)) != 1 {
		// The id resolved but the credential is not its token: an unknown
		// capability, the 404 shape.
		writeError(w, http.StatusNotFound, "upload session not found: "+sid)
		return nil, false
	}
	if !expiry.IsZero() && !time.Now().UTC().Before(expiry) {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return nil, false
	}
	return ms, true
}

// ---- endpoint handlers ----

// handleUploadsCreate serves POST /api/v1/uploads/create?repoKey=&repoPath=
// &partSizeMB= (ADR-0039): the parameters ride the QUERY STRING, the answer
// is 200 with the session token. Gates in Artifactory's order — param
// presence (400), role door, repo existence/type + the write door (the one
// 403), part size bounds — then the engine begin with the v2 caller blob.
func (s *Server) handleUploadsCreate(w http.ResponseWriter, r *http.Request) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return
	}
	q := r.URL.Query()
	repoKey := q.Get("repoKey")
	repoPath := q.Get("repoPath")
	if repoKey == "" || repoPath == "" {
		// The Artifactory wording verbatim (assertRepoKeyAndPathNotNull's
		// BadRequestException).
		writeError(w, http.StatusBadRequest, "Query param repoKey or repoPath is null")
		return
	}
	if msg := mpuUploadsPathError(repoPath); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	partSizeMB := int64(0)
	if raw := q.Get("partSizeMB"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "partSizeMB must be an integer number of megabytes")
			return
		}
		partSizeMB = v
	}
	// The RolesAllowed({"admin","user"}) door: readonly_admin holds no
	// deploy leg on this plane.
	p := principalFrom(r.Context())
	if p == nil || (p.EffectiveRole() != auth.RoleAdmin && p.EffectiveRole() != auth.RoleUser) {
		writeError(w, http.StatusForbidden, mpuForbiddenDeploy)
		return
	}
	// T-304 section 1.2-B (flipped with this wire): no package-type gate —
	// protocol repositories take MPU writes like any local; a virtual
	// resolves to its defaultDeploymentRepo, a virtual without one keeps
	// the virtual key (the failure stays late, at the deploy).
	targetRepo, ok := s.resolveMPUTargetRepo(w, r, repoKey)
	if !ok {
		return
	}
	if !s.uploadsWriteGate(r, targetRepo, repoPath) {
		writeError(w, http.StatusForbidden, mpuForbiddenDeploy)
		return
	}
	partSize, ok := resolveMPUPartSize(partSizeMB)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"partSizeMB must be between 0 (engine default) and %d", mpuMaxPartSizeMB))
		return
	}

	now := time.Now().UTC()
	createdBy := ""
	if p != nil {
		createdBy = p.Name
	}
	// The capability token's secret half is drawn BEFORE the begin: its
	// sha256 rides the caller blob the context begin persists, and the
	// token string itself is only complete once the engine hands back the
	// session id (secret "." id).
	secret, secretSHA, err := newMPUTokenSecret()
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: mpu token mint failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "opening the multipart upload session failed")
		return
	}
	tokenExpiry := now.Add(mpuTokenTTL)
	caller, merr := json.Marshal(mpuCallerState{
		Version:       mpuCallerStateVersion,
		RepoKey:       targetRepo,
		ClientRepoKey: repoKey,
		Path:          repoPath,
		MimeType:      "application/octet-stream",
		PartSize:      partSize,
		CreatedBy:     createdBy,
		CreatedAt:     now.UTC().Format(time.RFC3339),
		TokenSHA256:   secretSHA,
		TokenExpiry:   tokenExpiry.UTC().Format(time.RFC3339),
	})
	var sess storage.Session
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
			"repo", targetRepo, "path", repoPath, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "opening the multipart upload session failed")
		return
	}
	ms := &mpuSession{
		id:            sess.ID(),
		sess:          sess,
		repoKey:       targetRepo,
		clientRepoKey: repoKey,
		path:          repoPath,
		mime:          "application/octet-stream",
		partSize:      partSize,
		staged:        map[int][]byte{},
		state:         mpuStateActive,
		createdBy:     createdBy,
		createdAt:     now,
		updatedAt:     now,
		tokenSHA256:   secretSHA,
		tokenExpiry:   tokenExpiry,
	}
	s.uploads.add(ms)
	s.uploads.startSweep()
	s.log.InfoContext(r.Context(), "httpapi: mpu session opened",
		"session", ms.id, "repo", targetRepo, "path", repoPath,
		"partSizeBytes", partSize, "createdBy", createdBy)
	writeJSONBody(w, http.StatusOK, mpuTokenBody{Token: secret + "." + ms.id})
}

// handleUploadsConfig serves GET /api/v1/uploads/config (T-304 section 1.2-D
// + ADR-0039): the capability probe. A jfrog-cli-go user agent below
// mpuCLIMinVersion gets supported:false so the client falls back to
// monolithic uploads; everyone else learns whether THIS backend carries the
// MPU seam. The probe answers 200 on every backend — a probe that could not
// say "no" would be no probe (the five data endpoints keep the honest 501).
func (s *Server) handleUploadsConfig(w http.ResponseWriter, r *http.Request) {
	if ua := r.UserAgent(); strings.HasPrefix(ua, mpuCLIPrefix) {
		if mpuCLIVersionTooOld(strings.TrimPrefix(ua, mpuCLIPrefix)) {
			writeJSONBody(w, http.StatusOK, mpuSupportedBody{Supported: false})
			return
		}
	}
	writeJSONBody(w, http.StatusOK, mpuSupportedBody{Supported: s.deps.Uploads != nil})
}

// mpuCLIVersionTooOld compares a dotted version against the gate in the
// reverse-engineered resource's shape: numeric segment walk, a shorter
// version than the gate is old (2.62 < 2.62.2), and a non-numeric segment
// aborts the comparison as NOT old (the try/catch's catch arm) — the
// exact arithmetic, not a library sort.
func mpuCLIVersionTooOld(version string) bool {
	return versionLessThan(version, mpuCLIMinVersion)
}

// versionLessThan walks two dotted numeric versions in lockstep; a parse
// failure anywhere answers false (the caller treats it as comparable-new).
func versionLessThan(v, target string) bool {
	vp := strings.Split(v, ".")
	tp := strings.Split(target, ".")
	n := len(vp)
	if len(tp) < n {
		n = len(tp)
	}
	for i := 0; i < n; i++ {
		a, errA := strconv.Atoi(vp[i])
		b, errB := strconv.Atoi(tp[i])
		if errA != nil || errB != nil {
			return false
		}
		if a < b {
			return true
		}
		if a > b {
			return false
		}
	}
	return len(vp) < len(tp)
}

// handleUploadsURLPart serves POST /api/v1/uploads/urlPart?partNumber=N: the
// URL part n PUTs to. Part numbers are 1-based and strictly sequential —
// the Session face is an ordered append stream, so this plane's contract is
// "part n lands at offset (n-1)*partSize"; the PUT enforces it, this verb
// only states it (a lookahead answer is a URL like any other; the ordering
// gate lives where the bytes do).
func (s *Server) handleUploadsURLPart(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("partNumber")
	if raw == "" {
		writeError(w, http.StatusBadRequest, "Query param partNumber is null")
		return
	}
	part, err := strconv.Atoi(raw)
	if err != nil || part < 1 {
		writeError(w, http.StatusBadRequest, "partNumber must be a positive integer")
		return
	}
	ms, ok := s.resolveMPUSessionByToken(w, r)
	if !ok {
		return
	}
	ms.mu.Lock()
	state, task := ms.state, ms.taskState
	ms.mu.Unlock()
	if task == mpuTaskFinished || task == mpuTaskFailed {
		// The assembly consumed the upload either way — the not-found
		// posture, not a conflict the client could resolve.
		writeError(w, http.StatusNotFound, "upload session not found: "+ms.id)
		return
	}
	if task == mpuTaskProcessing || state == mpuStateFailed {
		writeError(w, http.StatusConflict,
			"the session is not accepting parts (completion in flight or failed); query status")
		return
	}
	// The URL carries the capability in its query string (T-332 real-client
	// finding): protocol clients treat this answer as S3-presigned-shaped —
	// they PUT to it with NO Authorization header at all — so the token must
	// ride the URL itself. The server re-echoes the very credential the
	// caller presented (only its sha256 is persisted), and the access log
	// records the path only, never the query — the capability does not land
	// in the logs.
	writeJSONBody(w, http.StatusOK, mpuURLBody{
		URL: fmt.Sprintf("%s%s/part/%s/%d?token=%s",
			requestBase(r), mpuEndpointBase, ms.id, part, url.QueryEscape(mpuPresentedToken(r, ms.id))),
	})
}

// mpuPresentedToken returns the validated capability token this request
// presented (query parameter or Bearer — the part lane accepts both
// spellings), or "" when the request carried none for this session.
func mpuPresentedToken(r *http.Request, sid string) string {
	tok := mpuRequestToken(r)
	_, tsid, ok := parseMPUToken(tok)
	if !ok || tsid != sid {
		return ""
	}
	return tok
}

// handleUploadsStatus serves POST /api/v1/uploads/status: the finish task's
// progress model (StatusResponse's four keys). No task yet -> Uploading; a
// task in flight -> Finishing; the terminal states carry the error or the
// checksum-deploy token (the client's zero-transfer landing credential,
// minted at Finished through the auth facet).
func (s *Server) handleUploadsStatus(w http.ResponseWriter, r *http.Request) {
	ms, ok := s.resolveMPUSessionByToken(w, r)
	if !ok {
		return
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	body := mpuStatusTaskBody{Status: mpuTaskParts, Progress: 0}
	switch {
	case ms.taskState == mpuTaskFinished:
		body.Status = mpuTaskFinished
		body.Progress = 100
		if ms.taskToken != "" {
			tok := ms.taskToken
			body.ChecksumToken = &tok
		}
	case ms.taskState == mpuTaskFailed:
		body.Status = mpuTaskFailed
		msg := ms.taskErr
		body.Error = &msg
	case ms.taskState == mpuTaskProcessing:
		body.Status = mpuTaskProcessing
	case ms.state == mpuStateFailed:
		// A session the part stream broke (engine failure, torn part): the
		// honest task answer is the failure the next verb would hit.
		body.Status = mpuTaskFailed
		msg := "the session is in a failed state; abort it and start a new upload"
		body.Error = &msg
	}
	writeJSONBody(w, http.StatusOK, body)
}

// handleUploadsComplete serves POST /api/v1/uploads/complete?sha1=: the
// checksum-gated assembly, asynchronous on the wire (202, the
// Response.accepted() shape). The declared digest is sha1 — the algorithm
// flips with the wire (ADR-0039); sha256/sha1/md5 are all still computed
// server-side into the blob ledger. The finish task runs the engine's
// synchronous Commit in the background; a well-formed but WRONG sha1 fails
// the task (status carries the error) rather than this response.
func (s *Server) handleUploadsComplete(w http.ResponseWriter, r *http.Request) {
	sha1Param := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sha1")))
	if sha1Param == "" {
		writeError(w, http.StatusBadRequest, "Query param sha1 is null")
		return
	}
	if len(sha1Param) != 40 || !isHexLower(sha1Param) {
		writeError(w, http.StatusBadRequest, "sha1 must be exactly 40 hex characters")
		return
	}
	ms, ok := s.resolveMPUSessionByToken(w, r)
	if !ok {
		return
	}

	// Every exit below unlocks EXPLICITLY (the B1 lock order: no registry
	// lock under a session lock).
	ms.mu.Lock()
	if ms.state == mpuStateFailed && ms.taskState == mpuTaskNone {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"the session is in a failed state; abort it and start a new upload")
		return
	}
	switch ms.taskState {
	case mpuTaskProcessing:
		// Already in flight: the outcome the caller asked for is underway —
		// the idempotent 202 (BinFlow-defined; Artifactory's storage library
		// arm is unverifiable, registered in the ADR).
		ms.mu.Unlock()
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusAccepted)
		return
	case mpuTaskFinished, mpuTaskFailed:
		// The assembly consumed the session (Commit finalizes either way,
		// the storage contract): the not-found posture.
		ms.mu.Unlock()
		writeError(w, http.StatusNotFound, "upload session not found: "+ms.id)
		return
	}
	ms.taskState = mpuTaskProcessing
	ms.updatedAt = time.Now().UTC()
	ms.mu.Unlock()

	go s.mpuFinishTask(ms, sha1Param)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusAccepted)
}

// mpuFinishTask is complete's background half: the engine's synchronous
// Commit (assembly + sha1 gate + the temp->blob finalize), then the task
// record. On success it mints the checksum-deploy token the Finished status
// hands the client — a REAL API token through the auth facet, short-lived
// (5 minutes), owned by the session's creator and, since T-349 (FR-113.3),
// CHECKSUM-DEPLOY SCOPED: the deployScopeGuard admits the credential to
// exactly this session's landing PUT and 403s everything else. The task runs
// on a detached context on purpose: a client disconnect after the 202 must
// not kill the assembly (宁可慢不可丢数据).
func (s *Server) mpuFinishTask(ms *mpuSession, sha1Param string) {
	ctx := context.Background()
	ref, err := ms.sess.Commit(ctx, storage.BlobRef{Sha1: sha1Param})
	ms.mu.Lock()
	ms.updatedAt = time.Now().UTC()
	if err != nil {
		ms.state = mpuStateFailed
		ms.taskState = mpuTaskFailed
		if errors.Is(err, storage.ErrChecksumMismatch) {
			ms.taskErr = fmt.Sprintf("provided checksum did not match uploaded content: %s", err.Error())
		} else {
			ms.taskErr = "committing the multipart upload failed: " + err.Error()
		}
		msg := ms.taskErr
		ms.mu.Unlock()
		s.log.WarnContext(ctx, "httpapi: mpu finish task failed",
			"session", ms.id, "repo", ms.repoKey, "path", ms.path, "error", msg)
		return
	}
	ms.state = mpuStateComplete
	ms.received = ref.Size
	ms.mu.Unlock()

	// The ledger row is the STORAGE bookkeeping half of Artifactory's
	// completeMultipartUpload — the binary store's checksum index. It rides
	// the exported BlobStore seam (its Put contract is verbatim "caller
	// commits the physical blob first"; ON CONFLICT DO NOTHING keeps any
	// pre-existing row's sha1/md5 authority, exactly right for identical
	// bytes). The NODE is deliberately NOT written here: that is the
	// client's checksum-deploy, and every governance gate (pattern, quota,
	// the overwrite pair) runs on THAT call, the Artifactory position.
	if lerr := s.deps.Metadata.Blobs().Put(ctx, &metadata.Blob{
		Sha256:    ref.Sha256,
		Sha1:      ref.Sha1,
		Md5:       ref.Md5,
		Size:      ref.Size,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); lerr != nil {
		ms.mu.Lock()
		ms.taskState = mpuTaskFailed
		ms.taskErr = "registering the assembled blob in the ledger failed: " + lerr.Error()
		ms.state = mpuStateFailed
		msg := ms.taskErr
		ms.mu.Unlock()
		s.log.ErrorContext(ctx, "httpapi: mpu ledger registration failed",
			"session", ms.id, "sha256", ref.Sha256, "error", msg)
		return
	}

	token := ""
	if scoped, ok := s.deps.Auth.(mpuScopedTokenIssuer); ok {
		landing := []string{ms.repoKey}
		if ms.clientRepoKey != "" && ms.clientRepoKey != ms.repoKey {
			landing = append(landing, ms.clientRepoKey)
		}
		if issued, ierr := scoped.IssueChecksumDeployScoped(ctx, ms.createdBy, mpuChecksumTokenTTL, landing, ms.path); ierr == nil {
			token = issued.AccessToken
		} else {
			s.log.WarnContext(ctx, "httpapi: mpu checksum-deploy token mint failed",
				"session", ms.id, "owner", ms.createdBy, "error", ierr.Error())
		}
	} else if issuer, ok := s.deps.Auth.(mpuTokenIssuer); ok {
		// Narrow facet absent (an alternative wiring without *auth.Service):
		// the broad mint, logged as the unrestricted residual it is.
		s.log.WarnContext(ctx, "httpapi: auth facet cannot mint scoped checksum-deploy tokens; falling back to an unrestricted token",
			"session", ms.id)
		if issued, ierr := issuer.Issue(ctx, ms.createdBy, mpuChecksumTokenTTL); ierr == nil {
			token = issued.AccessToken
		} else {
			s.log.WarnContext(ctx, "httpapi: mpu checksum-deploy token mint failed",
				"session", ms.id, "owner", ms.createdBy, "error", ierr.Error())
		}
	} else {
		s.log.WarnContext(ctx, "httpapi: auth facet cannot mint checksum-deploy tokens; status will carry none",
			"session", ms.id)
	}
	ms.mu.Lock()
	ms.taskState = mpuTaskFinished
	ms.taskToken = token
	ms.mu.Unlock()
	s.log.InfoContext(ctx, "httpapi: mpu assembly finished",
		"session", ms.id, "repo", ms.repoKey, "path", ms.path,
		"size", ref.Size, "sha1", ref.Sha1, "checksumToken", token != "")
}

// handleUploadsAbort serves POST /api/v1/uploads/abort: discard the session
// and its S3 multipart state. 204; an unknown (or already aborted) id is
// the plane's 404 — abort is not idempotent on the wire because the session
// stops resolving the moment it leaves the registry (AC1's "abort 后 status
// 404" posture). A Finished upload refuses (409): the artifact is already
// assembled; discarding it is not this verb's call.
func (s *Server) handleUploadsAbort(w http.ResponseWriter, r *http.Request) {
	ms, ok := s.resolveMPUSessionByToken(w, r)
	if !ok {
		return
	}
	ms.mu.Lock()
	switch ms.taskState {
	case mpuTaskFinished:
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, "the session is already completed")
		return
	case mpuTaskFailed:
		// The assembly consumed the engine session; abort here is the
		// record's cleanup.
	case mpuTaskProcessing:
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"completion is in flight; query status until it settles")
		return
	}
	_ = ms.sess.Abort(context.WithoutCancel(r.Context()))
	ms.state = mpuStateFailed
	ms.mu.Unlock()
	s.uploads.remove(ms.id)
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusNoContent)
}

// handleUploadsPart serves PUT /api/v1/uploads/part/{id}/{n} — the urlPart
// target. Credential lanes, widest first: the capability in the URL's
// query string (the presigned shape protocol clients actually drive — they
// send NO Authorization header on the part PUT at all, the T-332 real-
// client finding), the same token as a Bearer, or the shared credentials
// with the target path's `w` (the manual-driver shape this relay URL always
// took).
//
// Part arrival: S3 semantics — parts may arrive in ANY order (worker-pool
// clients routinely leapfrog). The in-order fast path streams straight
// into Session.Append (memory independent of part size); a leapfrog part
// lands in a bounded staging set until its predecessors close the gap
// (mpuMaxStagedParts/mpuMaxStagedBytes). Each part is exactly
// partSizeBytes except the final one (which may be shorter and closes the
// part stream), Content-Length is mandatory (the size gates run before any
// byte reaches the engine), and the offset authority is the engine session
// (Session.Offset), re-checked before every append.
func (s *Server) handleUploadsPart(w http.ResponseWriter, r *http.Request, id string, part int) {
	if s.deps.Uploads == nil {
		writeUploadsUnavailable(w)
		return
	}
	if part < 1 {
		writeError(w, http.StatusBadRequest, "partNumber must be a positive integer")
		return
	}
	var ms *mpuSession
	if tok := r.URL.Query().Get("token"); tok != "" {
		if _, sid, isMPU := parseMPUToken(tok); isMPU {
			if sid != id {
				writeError(w, http.StatusNotFound, "upload session not found: "+id)
				return
			}
			resolved, ok := s.resolveMPUSessionByToken(w, r)
			if !ok {
				return
			}
			ms = resolved
		}
	}
	if ms == nil {
		if tok := mpuBearerValue(r); tok != "" {
			if _, sid, isMPU := parseMPUToken(tok); isMPU {
				if sid != id {
					// The URL and the credential disagree: not this session.
					writeError(w, http.StatusNotFound, "upload session not found: "+id)
					return
				}
				resolved, ok := s.resolveMPUSessionByToken(w, r)
				if !ok {
					return
				}
				ms = resolved
			} else if principalFrom(r.Context()) == nil {
				// A non-MPU Bearer the middleware could not verify: its own
				// verdict, rendered here (the capability-route exemption).
				mpuCredentialVerdict(w, false)
				return
			}
		}
	}
	if ms == nil {
		var ok bool
		ms, ok = s.resolveMPUSessionByID(w, r, id)
		if !ok {
			return
		}
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

	ms.mu.Lock()
	if ms.state == mpuStateComplete || ms.taskState == mpuTaskFinished {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, "the session is already completed")
		return
	}
	if ms.taskState == mpuTaskProcessing {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, "completion is in flight; no further parts")
		return
	}
	if ms.state == mpuStateFailed {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"the session is in a failed state; abort it and start a new upload")
		return
	}
	if ms.state == mpuStateFinal {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict,
			"a short final part was already accepted; complete the session (no further parts)")
		return
	}
	want := ms.parts + 1
	if part < want {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"part %d was already received (the stream is at part %d); duplicate parts cannot be rewritten",
			part, want))
		return
	}
	// The engine's own offset is the alignment authority (the [M7] rule):
	// the registry mirror agrees in every non-failure flow, and a drifted
	// mirror (a truncated stream the engine rejected, an engine swap)
	// must be caught here, not silently papered over.
	if off := ms.sess.Offset(); off != int64(want-1)*ms.partSize {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"session offset drift: part %d starts at %d bytes, session is at %d; query status and re-align",
			want, int64(want-1)*ms.partSize, off))
		return
	}
	// In-order fast path (no staged gap ahead): stream straight into the
	// engine — the memory stays independent of the part size.
	if part == want && len(ms.staged) == 0 {
		ok := s.mpuAppendPart(w, r, ms, part, n)
		var echo mpuPartEcho
		if ok {
			echo = ms.bodyOf()
		}
		ms.mu.Unlock()
		if ok {
			// 200, the S3 PutObject shape: the real client treats the
			// urlPart target as presigned and any other 2xx (202 included)
			// as a part failure to retry away (T-332 real-client finding).
			writeJSONBody(w, http.StatusOK, echo)
		}
		return
	}
	// Leapfrog: bound the staging set, buffer the body, then flush every
	// contiguous prefix that opened up.
	if len(ms.staged) >= mpuMaxStagedParts || ms.stagedBy+n > mpuMaxStagedBytes {
		ms.mu.Unlock()
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"part %d is too far ahead (part %d is next): the out-of-order staging budget is %d parts / %d bytes; lower the client split count",
			part, want, mpuMaxStagedParts, mpuMaxStagedBytes))
		return
	}
	ms.mu.Unlock()

	buf := make([]byte, n)
	if _, err := io.ReadFull(r.Body, buf); err != nil && err != io.EOF {
		// Torn part: fewer bytes than the declared Content-Length arrived.
		// Nothing reached the engine; the honest verdict is a 400 (the
		// same torn-part posture as the in-order path).
		ms.mu.Lock()
		ms.state = mpuStateFailed
		ms.updatedAt = time.Now().UTC()
		ms.mu.Unlock()
		s.log.WarnContext(r.Context(), "httpapi: mpu part torn before staging",
			"session", ms.id, "part", part, "error", err.Error())
		writeError(w, http.StatusBadRequest, fmt.Sprintf(
			"part %d ended early (torn upload); abort the session and restart", part))
		return
	}

	ms.mu.Lock()
	ms.staged[part] = buf
	ms.stagedBy += n
	flushed := 0
	for ms.state == mpuStateActive {
		next, ok := ms.staged[ms.parts+1]
		if !ok {
			break
		}
		delete(ms.staged, ms.parts+1)
		ms.stagedBy -= int64(len(next))
		if !s.mpuAppendBuffer(w, r, ms, ms.parts+1, next) {
			break
		}
		flushed++
	}
	echo := ms.bodyOf()
	landed := ms.state == mpuStateActive || ms.state == mpuStateFinal
	ms.updatedAt = time.Now().UTC()
	ms.mu.Unlock()
	_ = flushed
	if !landed {
		return // the failure arm already wrote its verdict
	}
	// 200, the S3 PutObject shape (see the in-order path's note).
	writeJSONBody(w, http.StatusOK, echo)
}

// mpuAppendPart streams one IN-ORDER part from the request body into the
// engine session. ms.mu must be held; ok=false means the verdict is
// already written.
func (s *Server) mpuAppendPart(w http.ResponseWriter, r *http.Request, ms *mpuSession, part int, n int64) bool {
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
		return false
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
		return false
	}
	ms.received = written
	ms.parts = part
	ms.updatedAt = time.Now().UTC()
	if n < ms.partSize {
		ms.state = mpuStateFinal // a short part is by definition the last
	}
	return true
}

// mpuAppendBuffer flushes one staged part into the engine session (the
// reorder lane's engine half). ms.mu must be held; ok=false means the
// verdict is already written.
func (s *Server) mpuAppendBuffer(w http.ResponseWriter, r *http.Request, ms *mpuSession, part int, buf []byte) bool {
	written, err := ms.sess.Append(r.Context(), bytes.NewReader(buf))
	if err != nil {
		ms.state = mpuStateFailed
		ms.updatedAt = time.Now().UTC()
		s.log.WarnContext(r.Context(), "httpapi: mpu staged part flush failed",
			"session", ms.id, "part", part, "error", err.Error())
		writeError(w, http.StatusInternalServerError,
			"storing the part failed; the session must be aborted and the upload restarted")
		return false
	}
	ms.received = written
	ms.parts = part
	if int64(len(buf)) < ms.partSize {
		ms.state = mpuStateFinal
		// A short part closes the stream by definition; anything still
		// staged behind it is a client contract violation (S3 completeness
		// is declared at complete, this stream's is the first short part).
		if len(ms.staged) > 0 {
			ms.state = mpuStateFailed
			ms.updatedAt = time.Now().UTC()
			s.log.WarnContext(r.Context(), "httpapi: mpu short part staged behind; session failed",
				"session", ms.id, "part", part, "staged", len(ms.staged))
			writeError(w, http.StatusConflict,
				"a short final part closed the stream while later parts were still in flight; abort and restart with a uniform part size")
			return false
		}
	}
	return true
}

// mpuRequestToken returns the capability credential this request presented
// in EITHER spelling — the ?token= query parameter (the presigned-URL
// shape the real clients drive on part PUTs) or the Authorization Bearer
// header (the four verb endpoints). Empty when neither is present.
func mpuRequestToken(r *http.Request) string {
	if tok := r.URL.Query().Get("token"); tok != "" {
		return tok
	}
	return mpuBearerValue(r)
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

// withUploadsPart adapts the {id}/{partNumber} tail of the part route: a
// missing, non-numeric or extra-segment part number is the E-26 404; a
// numeric-but-invalid value (zero, negative) reaches the handler, which
// owns the honest 400.
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
