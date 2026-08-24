package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

// S3Engine implements the Engine interface using an S3-compatible object store
// (via minio-go/v7). The blob key shape is <bucketPrefix>/blobs/<sha256[0:2]>/<sha256>,
// identical to the disk layout. Multipart uploads map to Engine sessions:
// BeginSession -> NewMultipartUpload, Append -> PutObjectPart, Commit ->
// CompleteMultipartUpload, Abort -> AbortMultipartUpload.
//
// Session Appends stream in bounded memory: at most one part buffer per
// session is resident, independent of upload size (T-202, QA T-173 D-4).
//
// S3Engine is safe for concurrent use.
type S3Engine struct {
	core         *minio.Core // Core has Client embedded + multipart primitives
	bucket       string
	bucketPrefix string // prepended to every object key; empty means root
	partSize     int64  // session Append flush threshold; see resolveS3PartSize
	holds        *holdSet

	mu       sync.RWMutex
	closed   bool
	sessions map[string]*s3Session
	clock    func() time.Time // clock override for tests; nil = time.Now
}

const (
	// DefaultS3PartSize is the multipart part size used to stream session
	// Appends when PartSize is not configured: Append buffers at most this
	// many bytes before flushing one PutObjectPart. 16 MiB bounds per-upload
	// memory while amortizing part round-trips (1 GiB upload = 64 parts).
	DefaultS3PartSize = int64(16 << 20)
	// MinS3PartSize is the smallest legal non-terminal part in the S3
	// multipart contract: every part except the last must be >= 5 MiB or
	// CompleteMultipartUpload fails with EntityTooSmall. Configured values
	// below the floor are clamped up (fail-fast at open instead of failing
	// mid-upload at complete).
	MinS3PartSize = int64(5 << 20)

	// blobCreatedAtMetaKey is the S3 user-metadata key that records a blob's
	// creation time (UTC RFC3339). It is the S3 counterpart of the disk blob's
	// mtime: the GC grace clock and the orphan-sweep age basis both read it,
	// falling back to the object's LastModified when it is absent (ADR-0006
	// erratum 2, T-203 D-5).
	//
	// Note: minio-go rounds user-metadata keys through Go's http.Header, which
	// canonicalizes them ("blob-created-at" reads back as "Blob-Created-At"),
	// so every read of this key must do a case-insensitive match.
	blobCreatedAtMetaKey = "blob-created-at"

	// uploadKeySegment is the "sessions" path segment under which multipart
	// upload keys live: <prefix>/sessions/<uuid>/data. It is the S3
	// counterpart of the disk engine's uploads/ dir (uploadsDirName); the S3
	// layout keeps its own historical segment name.
	uploadKeySegment = "sessions"
)

// resolveS3PartSize defaults and clamps the session part size.
func resolveS3PartSize(n int64) int64 {
	switch {
	case n <= 0:
		return DefaultS3PartSize
	case n < MinS3PartSize:
		return MinS3PartSize
	default:
		return n
	}
}

// api returns the high-level minio client. minio.Core shadows several method
// names (GetObject, PutObject, CopyObject, ListObjects, ...) with lower-level
// signatures of its own, so the embedded Client must be selected explicitly
// to reach the high-level API.
func (e *S3Engine) api() *minio.Client { return e.core.Client }

// S3EngineOptions configures OpenS3Engine.
type S3EngineOptions struct {
	// BucketPrefix is prepended to every object key. Defaults to "".
	BucketPrefix string
	// PartSize is the multipart part size used to stream session uploads:
	// Append buffers at most PartSize bytes in memory before flushing one
	// PutObjectPart, and Commit uploads the trailing remainder as the final
	// part (which may be smaller). Zero means DefaultS3PartSize; values below
	// MinS3PartSize are clamped up to it.
	PartSize int64
	// SessionTTL bounds the age of an orphaned multipart upload before the
	// startup sweep aborts it (T-203 D-6). Zero means DefaultSessionTTL.
	SessionTTL time.Duration
	// GCHoldTTL bounds how long an unreleased GC hold protects a sha
	// ([M9] ADR-0031; the storage.gc_hold_ttl_seconds key, wired by the
	// assembler). Zero means DefaultGCHoldTTL; values below MinGCHoldTTL
	// clamp up — see hold.go.
	GCHoldTTL time.Duration
	// Now overrides the clock (tests only). Nil uses time.Now.
	Now func() time.Time
}

// sessionTTL resolves the orphan-sweep grace, mirroring Options.ttl on the
// disk engine.
func (o *S3EngineOptions) sessionTTL() time.Duration {
	if o == nil || o.SessionTTL <= 0 {
		return DefaultSessionTTL
	}
	return o.SessionTTL
}

// OpenS3Engine creates an S3Engine backed by the given minio Core, bucket and
// optional prefix. Callers must ensure the bucket exists before calling
// OpenS3Engine. On success the engine has swept in-progress multipart uploads
// that predate the session TTL (orphans from interrupted/crashed uploads), the
// S3 counterpart of the disk engine's startup session sweep (T-203 D-6).
func OpenS3Engine(core *minio.Core, bucket string, opts *S3EngineOptions) (Engine, error) {
	if core == nil {
		return nil, errors.New("storage: s3: core is nil")
	}
	if bucket == "" {
		return nil, errors.New("storage: s3: bucket is empty")
	}
	if opts == nil {
		opts = &S3EngineOptions{}
	}
	e := &S3Engine{
		core:         core,
		bucket:       bucket,
		bucketPrefix: opts.BucketPrefix,
		partSize:     resolveS3PartSize(opts.PartSize),
		holds:        newHoldSet(opts.GCHoldTTL, opts.Now),
		sessions:     make(map[string]*s3Session),
		clock:        opts.Now,
	}
	if err := e.sweepOrphanUploads(context.Background(), opts.sessionTTL()); err != nil {
		return nil, fmt.Errorf("storage: s3: open: sweep orphan uploads: %w", err)
	}
	return e, nil
}

// OpenS3EngineWithClient creates an S3Engine from a minio.Client. This is a
// convenience wrapper that constructs a Core internally. Like OpenS3Engine it
// sweeps orphaned multipart uploads on open and fails if the sweep errors.
func OpenS3EngineWithClient(client *minio.Client, bucket string, opts *S3EngineOptions) (Engine, error) {
	var core *minio.Core
	if client != nil {
		core = &minio.Core{Client: client}
	}
	return OpenS3Engine(core, bucket, opts)
}

// objectKey returns the S3 key for a blob: <prefix>/blobs/<sha256[0:2]>/<sha256>.
func (e *S3Engine) objectKey(sha256 string) string {
	prefix := e.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	return prefix + blobsDirName + "/" + sha256[:2] + "/" + sha256
}

// objectKeyPrefix returns the S3 prefix for listing all blobs:
// <prefix>/blobs/ (or blobs/ when prefix is empty).
func (e *S3Engine) objectKeyPrefix() string {
	prefix := e.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	return prefix + blobsDirName + "/"
}

// uploadKeyPrefix returns the S3 prefix under which multipart upload keys
// live: <prefix>/sessions/ (or sessions/ when prefix is empty). It is the
// listing prefix for the orphan sweep (T-203 D-6).
func (e *S3Engine) uploadKeyPrefix() string {
	prefix := e.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	return prefix + uploadKeySegment + "/"
}

// checkOpen returns ErrEngineClosed if the engine is closed.
func (e *S3Engine) checkOpen() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return ErrEngineClosed
	}
	return nil
}

// timeNow returns the configured clock or the system clock.
func (e *S3Engine) timeNow() time.Time {
	if e.clock != nil {
		return e.clock()
	}
	return time.Now()
}

// ---------------------------------------------------------------------------
// Engine interface
// ---------------------------------------------------------------------------

// BeginSession creates a new multipart upload session. The session ID is a
// uuid; the S3 upload ID is stored internally.
func (e *S3Engine) BeginSession(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: s3: begin session: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: s3: begin session: %w", err)
	}

	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("storage: s3: begin session: %w", err)
	}
	// The multipart upload key is a temporary path under a "sessions" prefix.
	uploadKey := e.uploadKeyPrefix() + id + "/data"

	uploadID, err := e.core.NewMultipartUpload(ctx, e.bucket, uploadKey, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3: begin session: create multipart upload: %w", err)
	}

	s := &s3Session{
		eng:       e,
		id:        id,
		uploadID:  uploadID,
		uploadKey: uploadKey,
		partSize:  e.partSize,
		digests:   newDigesters(),
		createdAt: e.timeNow(),
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = e.core.AbortMultipartUpload(context.Background(), e.bucket, uploadKey, uploadID)
		return nil, fmt.Errorf("storage: s3: begin session %s: %w", id, ErrEngineClosed)
	}
	e.sessions[id] = s
	e.mu.Unlock()
	return s, nil
}

// ResumeSession returns ErrSessionNotFound. S3 multipart uploads are not
// resumable in this implementation: multipart state lives server-side and is
// never persisted to upload_sessions, so an expired id is indistinguishable
// from an unknown one — the hard-404 contract of architecture section 5.3.1
// contract 5, pinned by TestS3ResumeSessionNotSupported.
func (e *S3Engine) ResumeSession(_ context.Context, id string) (Session, error) {
	return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrSessionNotFound)
}

// Open returns an io.ReadCloser over the blob body. The returned reader is a
// streaming S3 response body — callers must Close it.
func (e *S3Engine) Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: s3: open blob: %w", err)
	}
	key := e.objectKey(sha256)
	// Use Client-level GetObject via the embedded *Client.
	obj, err := e.api().GetObject(ctx, e.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: s3: open blob %s: %w", sha256, err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return nil, BlobRef{}, fmt.Errorf("storage: s3: open blob %s: %w", sha256, ErrBlobNotFound)
		}
		return nil, BlobRef{}, fmt.Errorf("storage: s3: open blob %s: %w", sha256, err)
	}
	return obj, BlobRef{Sha256: sha256, Size: info.Size}, nil
}

// Stat streams the blob once, verifies it hashes to its own path and returns
// all three digests.
func (e *S3Engine) Stat(ctx context.Context, sha256 string) (BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return BlobRef{}, fmt.Errorf("storage: s3: stat blob: %w", err)
	}
	key := e.objectKey(sha256)
	obj, err := e.api().GetObject(ctx, e.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return BlobRef{}, fmt.Errorf("storage: s3: stat blob %s: %w", sha256, err)
	}
	defer obj.Close() //nolint:errcheck // read-only stream

	d := newDigesters()
	size, err := io.Copy(d.writer(), obj)
	if err != nil {
		return BlobRef{}, fmt.Errorf("storage: s3: stat blob %s: %w", sha256, err)
	}
	sums := d.sums()
	if sums.sha256 != sha256 {
		return BlobRef{}, fmt.Errorf("storage: s3: stat blob %s: %w: content hashes to %s", sha256, ErrBlobCorrupt, sums.sha256)
	}
	return BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: size}, nil
}

// Delete physically removes a blob from S3.
func (e *S3Engine) Delete(ctx context.Context, sha256 string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: s3: delete blob: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return fmt.Errorf("storage: s3: delete blob %s: %w", sha256, err)
	}
	key := e.objectKey(sha256)
	err := e.api().RemoveObject(ctx, e.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return fmt.Errorf("storage: s3: delete blob %s: %w", sha256, ErrBlobNotFound)
		}
		return fmt.Errorf("storage: s3: delete blob %s: %w", sha256, err)
	}
	return nil
}

// GC implements the legacy Engine.GC face over the S3 bucket: it adapts the
// callback form to GCMarker and delegates to GCSweep (ADR-0031).
func (e *S3Engine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if referenced == nil {
		return nil, errors.New("storage: s3: gc: referenced callback is nil")
	}
	return e.GCSweep(ctx, ReferencedFunc(referenced), grace, apply)
}

// ReleaseGCHold implements Engine.ReleaseGCHold (in-memory, infallible; the
// disk twin documents the semantics).
func (e *S3Engine) ReleaseGCHold(sha256 string) error {
	e.holds.release(sha256)
	return nil
}

// GCSweep implements Engine.GCSweep over the S3 bucket: the [M9] ADR-0031
// gates applied to the object listing. The candidacy hold gate and the
// per-candidate delete gates (hold re-check, then Live — that order is the
// soundness invariant, see gc.go's gcDeleteGate) wrap the pre-existing
// grace logic unchanged. A held sha is skipped regardless of grace; the
// hold is acquired by s3Session.Commit before the CopyObject that publishes
// the blob key, which is this backend's visibility moment (the counterpart
// of the disk engine's pre-rename acquire).
func (e *S3Engine) GCSweep(ctx context.Context, m GCMarker, grace time.Duration, apply bool) ([]string, error) {
	return e.gcSweep(ctx, m, grace, apply, true)
}

// gcSweep is GCSweep with the hold-gate switch the background migration's
// inventory listing turns off (see the disk engine's gcList).
func (e *S3Engine) gcSweep(ctx context.Context, m GCMarker, grace time.Duration, apply, countHolds bool) ([]string, error) {
	if m == nil {
		return nil, errors.New("storage: s3: gc: marker is nil")
	}
	if grace <= 0 {
		grace = DefaultGCGrace
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: s3: gc: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: s3: gc: %w", err)
	}

	refs, err := m.Mark()
	if err != nil {
		return nil, fmt.Errorf("storage: s3: gc: referenced set: %w", err)
	}

	var gate *gcDeleteGate
	if apply {
		gate = &gcDeleteGate{holds: e.holds, recheck: newGCRecheck(m)}
	}

	now := e.timeNow()
	graceCutoff := now.Add(-grace)

	var candidates []string
	var deleted []string

	objCh := e.api().ListObjects(ctx, e.bucket, minio.ListObjectsOptions{
		Prefix:    e.objectKeyPrefix(),
		Recursive: true,
	})

	for obj := range objCh {
		if obj.Err != nil {
			return candidates, fmt.Errorf("storage: s3: gc: list objects: %w", obj.Err)
		}
		sha := extractSha256FromKey(obj.Key)
		if sha == "" {
			continue
		}
		if _, ok := refs[sha]; ok {
			continue // mark hit: keep
		}
		if countHolds && e.holds.held(sha) {
			continue // in-flight upload owns this sha: keep, grace aside
		}
		// The grace clock is the blob's creation time as recorded in its
		// "blob-created-at" user metadata, falling back to LastModified when the
		// metadata is absent (e.g. blobs imported via mc or older deployments).
		// This aligns the S3 GC with the disk backend, where a migrated blob's
		// grace is measured from its original creation, not its S3 arrival
		// (T-203 D-5). ListObjects does not carry user metadata, so a StatObject
		// (HEAD) is issued per unreferenced object only.
		ageBasis := obj.LastModified
		info, statErr := e.api().StatObject(ctx, e.bucket, obj.Key, minio.StatObjectOptions{})
		if statErr == nil {
			if created, ok := blobCreatedAtFromMeta(info.UserMetadata); ok {
				ageBasis = created
			}
		}
		if ageBasis.After(graceCutoff) {
			continue // inside grace window: skip
		}
		candidates = append(candidates, sha)
		if apply {
			skip, liveErr := gate.skip(sha)
			if liveErr != nil {
				return candidates, fmt.Errorf("storage: s3: gc: live recheck %s: %w", sha, liveErr)
			}
			if skip {
				continue
			}
			if err := e.api().RemoveObject(ctx, e.bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				return candidates, fmt.Errorf("storage: s3: gc: delete %s: %w", sha, err)
			}
			deleted = append(deleted, sha)
		}
	}

	sort.Strings(candidates)
	sort.Strings(deleted)
	if apply {
		return deleted, nil
	}
	return candidates, nil
}

// gcList enumerates every blob key with all GC candidacy gates disabled —
// the raw inventory the background migration diffing needs (holds included).
func (e *S3Engine) gcList(ctx context.Context) ([]string, error) {
	return e.gcSweep(ctx, emptyMarker{}, time.Nanosecond, false, false)
}

// blobCreatedAtFromMeta resolves a blob's creation time from its S3
// user-metadata map under the "blob-created-at" key. It returns (t, true) on a
// valid RFC3339 value and (zero, false) when the key is absent or malformed —
// the caller then falls back to the object's LastModified (ADR-0006 erratum 2,
// T-203 D-5). The lookup is case-insensitive because minio-go canonicalizes
// user-metadata keys through http.Header ("blob-created-at" -> "Blob-Created-At").
func blobCreatedAtFromMeta(meta map[string]string) (time.Time, bool) {
	for k, v := range meta {
		if strings.EqualFold(k, blobCreatedAtMetaKey) {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return time.Time{}, false
			}
			return t, true
		}
	}
	return time.Time{}, false
}

// extractSha256FromKey parses a sha256 hex string from an S3 key of the form
// <prefix>/blobs/<xx>/<sha256>. Returns "" if the key does not match.
func extractSha256FromKey(key string) string {
	idx := strings.LastIndex(key, "/")
	if idx < 0 {
		return ""
	}
	sha := key[idx+1:]
	if !validSha256(sha) {
		return ""
	}
	penultimate := key[:idx]
	idx2 := strings.LastIndex(penultimate, "/")
	if idx2 < 0 {
		return ""
	}
	shard := penultimate[idx2+1:]
	if len(shard) != 2 || sha[:2] != shard {
		return ""
	}
	return sha
}

// Close aborts all live sessions and marks the engine unusable. Idempotent.
func (e *S3Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	live := make([]*s3Session, 0, len(e.sessions))
	for _, s := range e.sessions {
		live = append(live, s)
	}
	e.sessions = nil
	e.mu.Unlock()

	var firstErr error
	for _, s := range live {
		if err := s.abortMultipart(context.Background()); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("storage: s3: close: session %s: %w", s.id, err)
		}
	}
	return firstErr
}

// forgetSession unregisters a session.
func (e *S3Engine) forgetSession(s *s3Session) {
	e.mu.Lock()
	if cur, ok := e.sessions[s.id]; ok && cur == s {
		delete(e.sessions, s.id)
	}
	e.mu.Unlock()
}

// listIncompleteUploads returns every in-progress multipart upload under the
// upload-key prefix, paginating with keyMarker/uploadIDMarker until the list is
// exhausted. These are the S3 counterpart of the disk engine's abandoned
// session directories: an upload interrupted by crash/kill leaves a live MPU
// that no live process tracks (T-203 D-6).
func (e *S3Engine) listIncompleteUploads(ctx context.Context) ([]minio.ObjectMultipartInfo, error) {
	const maxUploads = 1000
	var (
		all            []minio.ObjectMultipartInfo
		keyMarker      string
		uploadIDMarker string
	)
	for {
		res, err := e.core.ListMultipartUploads(ctx, e.bucket, e.uploadKeyPrefix(),
			keyMarker, uploadIDMarker, "", maxUploads)
		if err != nil {
			// A missing bucket is a benign cold-start state: there is nothing to
			// sweep. S3-compatible stores report it as NoSuchBucket (some also an
			// empty code with a 404 status), so treat both as an empty listing.
			if resp := minio.ToErrorResponse(err); resp.Code == "NoSuchBucket" ||
				(resp.Code == "" && resp.StatusCode == 404) {
				return nil, nil
			}
			return nil, fmt.Errorf("storage: s3: list multipart uploads: %w", err)
		}
		all = append(all, res.Uploads...)
		if !res.IsTruncated {
			return all, nil
		}
		keyMarker = res.NextKeyMarker
		uploadIDMarker = res.NextUploadIDMarker
		if err := ctx.Err(); err != nil {
			return all, fmt.Errorf("storage: s3: list multipart uploads: %w", err)
		}
	}
}

// sweepOrphanUploads aborts in-progress multipart uploads whose initiated time
// predates ttl. It runs once at engine open: any MPU older than the TTL is, by
// definition, no longer tied to a live in-process upload (BeginSession always
// stamps a fresh Initiated time), so aborting them reclaims the storage that an
// interrupted/crashed session left behind (T-203 D-6). One abort failure does
// not stop the sweep: the offending upload is reported via the returned error
// and retried on the next start.
func (e *S3Engine) sweepOrphanUploads(ctx context.Context, ttl time.Duration) error {
	uploads, err := e.listIncompleteUploads(ctx)
	if err != nil {
		return err
	}
	now := e.timeNow()
	cutoff := now.Add(-ttl)
	var firstErr error
	reclaimed := 0
	for _, u := range uploads {
		if u.Initiated.After(cutoff) {
			continue // still within TTL: a recent upload, leave it alone
		}
		// Abort regardless of ctx cancellation: orphan reclamation is best-effort
		// housekeeping and must not be half-done once started.
		if err := e.core.AbortMultipartUpload(context.Background(), e.bucket, u.Key, u.UploadID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("storage: s3: sweep orphan uploads: abort %s: %w", u.Key, err)
			}
			continue
		}
		reclaimed++
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// s3Session implements the Session interface backed by S3 multipart upload.
// ---------------------------------------------------------------------------

type s3Session struct {
	eng       *S3Engine
	id        string
	uploadID  string
	uploadKey string
	createdAt time.Time

	mu       sync.Mutex
	digests  *digesters
	parts    []minio.CompletePart // accumulated PutObjectPart results
	partBuf  []byte               // pending bytes not yet uploaded; len < partSize
	partSize int64                // flush threshold for partBuf; resolved at BeginSession
	received int64
	done     bool
	poisoned bool
	cause    error
}

// ID returns the session uuid.
func (s *s3Session) ID() string { return s.id }

// Offset returns the cumulative bytes received so far (committed parts plus
// the pending part buffer). S3 sessions are never re-materialized — this
// engine's ResumeSession is a hard ErrSessionNotFound (architecture section
// 5.3.1 contract 5) — so Offset only ever describes a live in-process
// session. Reads serialize against Append/Commit via s.mu.
func (s *s3Session) Offset() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.received
}

// Append streams r into the multipart upload in bounded memory. Bytes flow
// through the running digesters into the pending part buffer; every time the
// buffer reaches the session part size it is flushed as one PutObjectPart.
// The trailing partial buffer (< partSize) is uploaded by Commit as the final
// part, so per-session memory is capped at partSize regardless of upload
// size — the whole reader is never resident (T-202, fixing the T-173 D-4
// 1 GiB -> ~1.96 GiB RSS growth). Multiple Appends coalesce into shared
// parts, which also keeps sub-5MiB protocol chunks from becoming illegal
// tiny parts. Returns the cumulative offset.
func (s *s3Session) Append(ctx context.Context, r io.Reader) (int64, error) {
	if s == nil {
		return 0, errors.New("storage: s3: append: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return 0, fmt.Errorf("storage: s3: append session %s: session already finalized", s.id)
	}
	if s.poisoned {
		return 0, fmt.Errorf("storage: s3: append session %s: %w: %w", s.id, ErrSessionPoisoned, s.cause)
	}
	if err := ctx.Err(); err != nil {
		s.poison(err)
		return 0, fmt.Errorf("storage: s3: append session %s: %w", s.id, err)
	}

	if s.partSize <= 0 {
		s.partSize = DefaultS3PartSize // hand-built sessions; BeginSession always resolves
	}
	if s.partBuf == nil {
		// Start small and grow geometrically toward the part-size flush
		// threshold: a tiny blob never pays for a full part buffer (the
		// disk->S3 migration copies thousands of small blobs through one
		// session each), while a large upload converges to it exactly once.
		s.partBuf = make([]byte, 0, 32*1024)
	}
	digests := s.digests.writer()

	// Read in chunks that never cross the part boundary, so partBuf stays
	// <= partSize and a full buffer is flushed the moment it fills. The
	// context is checked per chunk (same contract as the disk engine's
	// copyWithCtx): a cancelled upload stops consuming the request body.
	scratch := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			s.poison(err)
			return 0, fmt.Errorf("storage: s3: append session %s: %w", s.id, err)
		}
		room := int(s.partSize) - len(s.partBuf)
		if room > len(scratch) {
			room = len(scratch)
		}
		nr, rerr := r.Read(scratch[:room])
		if nr > 0 {
			if _, err := digests.Write(scratch[:nr]); err != nil {
				s.poison(err)
				return 0, fmt.Errorf("storage: s3: append session %s: %w", s.id, err)
			}
			s.partBuf = append(s.partBuf, scratch[:nr]...)
			s.received += int64(nr)
			if int64(len(s.partBuf)) >= s.partSize {
				if err := s.flushPartLocked(ctx); err != nil {
					s.poison(err)
					return 0, err
				}
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return s.received, nil
			}
			s.poison(rerr)
			return 0, fmt.Errorf("storage: s3: append session %s: %w", s.id, rerr)
		}
	}
}

// flushPartLocked uploads the pending part buffer as the next part of the
// multipart upload and resets the buffer for reuse. PutObjectPart is
// synchronous — the body (a bytes.Reader of a known size) is fully consumed
// before it returns — so reusing the buffer's storage afterwards is safe.
// Callers hold s.mu.
func (s *s3Session) flushPartLocked(ctx context.Context) error {
	if len(s.partBuf) == 0 {
		return nil
	}
	partNumber := len(s.parts) + 1
	uploadInfo, err := s.eng.core.PutObjectPart(ctx, s.eng.bucket, s.uploadKey, s.uploadID,
		partNumber, bytes.NewReader(s.partBuf), int64(len(s.partBuf)), minio.PutObjectPartOptions{})
	if err != nil {
		return fmt.Errorf("storage: s3: session %s: put part %d (%d bytes): %w",
			s.id, partNumber, len(s.partBuf), err)
	}
	s.parts = append(s.parts, minio.CompletePart{
		PartNumber: partNumber,
		ETag:       uploadInfo.ETag,
	})
	s.partBuf = s.partBuf[:0]
	return nil
}

// Commit finalizes the multipart upload:
//  1. Verify expected digests against streamed content.
//  2. Flush the trailing partial part (if any) as the final part, then
//     CompleteMultipartUpload (or PutObject for empty/zero-part payloads).
//  3. Copy the result to the final blob key with metadata {"blob-created-at"}.
//  4. Delete the temp upload key.
func (s *s3Session) Commit(ctx context.Context, expect BlobRef) (BlobRef, error) {
	if s == nil {
		return BlobRef{}, errors.New("storage: s3: commit: nil session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: session already finalized", s.id)
	}
	if s.poisoned {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: %w", s.id, ErrSessionPoisoned, s.cause)
	}
	if err := ctx.Err(); err != nil {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w", s.id, err)
	}

	sums := s.digests.sums()
	actual := BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: s.received}

	// Step 1: verify expected digests.
	for _, chk := range []struct {
		name   string
		got    string
		want   string
		hexLen int
	}{
		{"sha256", actual.Sha256, expect.Sha256, sha256HexLen},
		{"sha1", actual.Sha1, expect.Sha1, sha1HexLen},
		{"md5", actual.Md5, expect.Md5, md5HexLen},
	} {
		want, err := normalizeHex(chk.want, chk.hexLen)
		if err != nil {
			s.failLocked()
			bad := fmt.Errorf("%s: %w", chk.name, err)
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: %w", s.id, ErrChecksumMismatch, bad)
		}
		if want != "" && want != chk.got {
			s.failLocked()
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: %s received %s, actual %s",
				s.id, ErrChecksumMismatch, chk.name, want, chk.got)
		}
	}

	targetKey := s.eng.objectKey(actual.Sha256)

	// [M9] ADR-0031 W-1: acquire the in-flight hold before anything below can
	// publish the blob key (the CopyObject of step 3 is this backend's
	// visibility moment — the counterpart of the disk engine's rename). The
	// S3 multipart machinery itself needs no separate protection: in-flight
	// parts live under the sessions/ prefix, which no GC sweep scans. Every
	// failure path releases the hold via the deferred complement; every
	// success path keeps it for the caller's ReleaseGCHold.
	s.eng.holds.acquire(actual.Sha256)
	holdKept := false
	defer func() {
		if !holdKept {
			s.eng.holds.release(actual.Sha256)
		}
	}()

	// Check if the blob already exists (idempotent dedup).
	_, err := s.eng.api().StatObject(ctx, s.eng.bucket, targetKey, minio.StatObjectOptions{})
	if err == nil {
		// Blob already exists — abort the multipart upload to clean up.
		_ = s.eng.core.AbortMultipartUpload(context.Background(), s.eng.bucket, s.uploadKey, s.uploadID)
		holdKept = true
		s.finishLocked()
		return actual, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code != "NoSuchKey" && resp.StatusCode != 404 {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: stat target %s: %w", s.id, targetKey, err)
	}

	// Step 2: flush the trailing partial part as the final part, then
	// complete (or put, for zero-byte payloads). The flush sits after the
	// digest gate on purpose: a rejected upload must not ship even its tail.
	// The trailing part may be smaller than the part size — S3 only requires
	// non-terminal parts to be >= 5 MiB.
	if len(s.partBuf) > 0 {
		if err := s.flushPartLocked(ctx); err != nil {
			s.failLocked()
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w", s.id, err)
		}
	}
	if len(s.parts) == 0 {
		err := s.putEmptyBlob(ctx, targetKey, actual)
		if err != nil {
			s.failLocked()
			return BlobRef{}, err
		}
		holdKept = true
		s.finishLocked()
		return actual, nil
	}

	_, err = s.eng.core.CompleteMultipartUpload(ctx, s.eng.bucket, s.uploadKey, s.uploadID, s.parts, minio.PutObjectOptions{})
	if err != nil {
		// Race: another goroutine may have completed the same multipart upload
		// and written the blob already. Check if the target exists.
		_, statErr := s.eng.api().StatObject(context.Background(), s.eng.bucket, targetKey, minio.StatObjectOptions{})
		if statErr == nil {
			// Blob exists — another goroutine won the race.
			holdKept = true
			s.finishLocked()
			return actual, nil
		}
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: complete multipart: %w", s.id, err)
	}

	// Step 3: Copy to the final blob key with metadata.
	// Core's CopyObject has signature: CopyObject(ctx, sourceBucket, sourceObject, destBucket, destObject, metadata, srcOpts, dstOpts).
	// We use the high-level Client CopyObject instead which takes CopyDestOptions and CopySrcOptions.
	_, err = s.eng.api().CopyObject(ctx, minio.CopyDestOptions{
		Bucket: s.eng.bucket,
		Object: targetKey,
		UserMetadata: map[string]string{
			blobCreatedAtMetaKey: s.eng.timeNow().UTC().Format(time.RFC3339),
		},
		// Without ReplaceMetadata the S3 COPY operation keeps the source's
		// metadata and silently drops UserMetadata (minio-go api-compose-object.go
		// documents "UserMetadata is only set to destination if ReplaceMetadata is
		// true"). The source here is the temp upload key with no blob-created-at,
		// so the destination must replace to preserve the creation timestamp
		// (T-203 D-5).
		ReplaceMetadata: true,
	}, minio.CopySrcOptions{
		Bucket: s.eng.bucket,
		Object: s.uploadKey,
	})
	if err != nil {
		_ = s.eng.api().RemoveObject(context.Background(), s.eng.bucket, s.uploadKey, minio.RemoveObjectOptions{})
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: copy to target %s: %w", s.id, targetKey, err)
	}

	// Step 4: Delete the source (upload key).
	_ = s.eng.api().RemoveObject(context.Background(), s.eng.bucket, s.uploadKey, minio.RemoveObjectOptions{})

	holdKept = true
	s.finishLocked()
	return actual, nil
}

// putEmptyBlob creates an empty blob at the target key with metadata.
func (s *s3Session) putEmptyBlob(ctx context.Context, targetKey string, actual BlobRef) error {
	emptySum := sha256.Sum256(nil)
	emptySha256 := hex.EncodeToString(emptySum[:])
	if actual.Sha256 != emptySha256 {
		return fmt.Errorf("storage: s3: commit session %s: %w: empty upload but digest is %s", s.id, ErrChecksumMismatch, actual.Sha256)
	}
	_, err := s.eng.api().PutObject(ctx, s.eng.bucket, targetKey,
		bytes.NewReader(nil), 0, minio.PutObjectOptions{
			ContentType: "application/octet-stream",
			UserMetadata: map[string]string{
				blobCreatedAtMetaKey: s.eng.timeNow().UTC().Format(time.RFC3339),
			},
		})
	if err != nil {
		return fmt.Errorf("storage: s3: commit session %s: put empty blob: %w", s.id, err)
	}
	return nil
}

// Abort discards the session and aborts the multipart upload. Idempotent and
// nil-receiver safe.
func (s *s3Session) Abort(ctx context.Context) error {
	if s == nil {
		return nil
	}
	_ = ctx.Err() // accepted: Abort must clean up regardless of cancellation
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	s.finishLocked()
	return nil
}

// abortMultipart is the engine-shutdown path: abort the S3 multipart upload.
func (s *s3Session) abortMultipart(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return nil
	}
	s.done = true
	s.poisoned = true
	return s.eng.core.AbortMultipartUpload(ctx, s.eng.bucket, s.uploadKey, s.uploadID)
}

func (s *s3Session) poison(cause error) {
	if !s.poisoned {
		s.poisoned = true
		s.cause = cause
	}
}

func (s *s3Session) finishLocked() {
	s.done = true
	s.partBuf = nil // release the bounded part buffer promptly
	s.eng.forgetSession(s)
}

func (s *s3Session) failLocked() {
	s.poisoned = true
	s.finishLocked()
}

// ---------------------------------------------------------------------------
// S3 backend implementation (internal Backend interface)
// ---------------------------------------------------------------------------

// s3Backend is a Backend implementation that wraps minio client. It is used
// internally by S3Engine for direct blob CRUD operations (without sessions).
type s3Backend struct {
	core         *minio.Core
	bucket       string
	bucketPrefix string
}

// api returns the high-level minio client; see S3Engine.api for why the
// embedded Client is selected explicitly.
func (b *s3Backend) api() *minio.Client { return b.core.Client }

func (b *s3Backend) objectKey(sha256 string) string {
	prefix := b.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	return prefix + blobsDirName + "/" + sha256[:2] + "/" + sha256
}

func (b *s3Backend) objectKeyPrefix() string {
	prefix := b.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	return prefix + blobsDirName + "/"
}

// Put stores a blob under sha256. It streams r through digest verification
// and stores the blob in S3.
func (b *s3Backend) Put(ctx context.Context, sha256 string, r io.Reader, size int64) (BlobRef, error) {
	key := b.objectKey(sha256)

	// Check if blob already exists (idempotent dedup).
	_, err := b.api().StatObject(ctx, b.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return BlobRef{Sha256: sha256, Size: size}, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code != "NoSuchKey" && resp.StatusCode != 404 {
		return BlobRef{}, fmt.Errorf("storage: s3: put blob %s: stat: %w", sha256, err)
	}

	// Stream r into S3 while computing digests.
	d := newDigesters()
	tee := io.TeeReader(r, d.writer())
	_, err = b.api().PutObject(ctx, b.bucket, key, tee, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
		UserMetadata: map[string]string{
			"blob-created-at": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return BlobRef{}, fmt.Errorf("storage: s3: put blob %s: %w", sha256, err)
	}

	sums := d.sums()
	if sums.sha256 != sha256 {
		_ = b.api().RemoveObject(context.Background(), b.bucket, key, minio.RemoveObjectOptions{})
		return BlobRef{}, fmt.Errorf("storage: s3: put blob %s: %w: content hashes to %s", sha256, ErrChecksumMismatch, sums.sha256)
	}

	return BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: size}, nil
}

// Get opens a blob for reading.
func (b *s3Backend) Get(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error) {
	key := b.objectKey(sha256)
	obj, err := b.api().GetObject(ctx, b.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return nil, BlobRef{}, fmt.Errorf("storage: s3: get blob %s: %w", sha256, ErrBlobNotFound)
		}
		return nil, BlobRef{}, fmt.Errorf("storage: s3: get blob %s: %w", sha256, err)
	}
	info, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return nil, BlobRef{}, fmt.Errorf("storage: s3: get blob %s: %w", sha256, ErrBlobNotFound)
		}
		return nil, BlobRef{}, fmt.Errorf("storage: s3: get blob %s: %w", sha256, err)
	}
	return obj, BlobRef{Sha256: sha256, Size: info.Size}, nil
}

// Delete removes a blob.
func (b *s3Backend) Delete(ctx context.Context, sha256 string) error {
	key := b.objectKey(sha256)
	err := b.api().RemoveObject(ctx, b.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return fmt.Errorf("storage: s3: delete blob %s: %w", sha256, ErrBlobNotFound)
		}
		return fmt.Errorf("storage: s3: delete blob %s: %w", sha256, err)
	}
	return nil
}

// Exists probes whether a blob exists.
func (b *s3Backend) Exists(ctx context.Context, sha256 string) (bool, error) {
	key := b.objectKey(sha256)
	_, err := b.api().StatObject(ctx, b.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return false, nil
		}
		return false, fmt.Errorf("storage: s3: exists blob %s: %w", sha256, err)
	}
	return true, nil
}

// List returns all blob sha256 values in the store.
func (b *s3Backend) List(ctx context.Context) ([]string, error) {
	var results []string
	objCh := b.api().ListObjects(ctx, b.bucket, minio.ListObjectsOptions{
		Prefix:    b.objectKeyPrefix(),
		Recursive: true,
	})
	for obj := range objCh {
		if obj.Err != nil {
			return results, fmt.Errorf("storage: s3: list blobs: %w", obj.Err)
		}
		sha := extractSha256FromKey(obj.Key)
		if sha != "" {
			results = append(results, sha)
		}
	}
	sort.Strings(results)
	return results, nil
}
