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
// S3Engine is safe for concurrent use.
type S3Engine struct {
	core         *minio.Core // Core has Client embedded + multipart primitives
	bucket       string
	bucketPrefix string // prepended to every object key; empty means root

	mu       sync.RWMutex
	closed   bool
	sessions map[string]*s3Session
	clock    func() time.Time // clock override for tests; nil = time.Now
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
	// Now overrides the clock (tests only). Nil uses time.Now.
	Now func() time.Time
}

// OpenS3Engine creates an S3Engine backed by the given minio Core, bucket and
// optional prefix. Callers must ensure the bucket exists before calling
// OpenS3Engine.
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
	return &S3Engine{
		core:         core,
		bucket:       bucket,
		bucketPrefix: opts.BucketPrefix,
		sessions:     make(map[string]*s3Session),
		clock:        opts.Now,
	}, nil
}

// OpenS3EngineWithClient creates an S3Engine from a minio.Client. This is a
// convenience wrapper that constructs a Core internally.
func OpenS3EngineWithClient(client *minio.Client, bucket string, opts *S3EngineOptions) Engine {
	if opts == nil {
		opts = &S3EngineOptions{}
	}
	return &S3Engine{
		core:         &minio.Core{Client: client},
		bucket:       bucket,
		bucketPrefix: opts.BucketPrefix,
		sessions:     make(map[string]*s3Session),
		clock:        opts.Now,
	}
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
	prefix := e.bucketPrefix
	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/") + "/"
	}
	uploadKey := prefix + "sessions/" + id + "/data"

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
// resumable in this implementation.
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

// GC implements mark-sweep GC over the S3 bucket.
func (e *S3Engine) GC(ctx context.Context, referenced func() (map[string]struct{}, error), grace time.Duration, apply bool) ([]string, error) {
	if referenced == nil {
		return nil, errors.New("storage: s3: gc: referenced callback is nil")
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

	refs, err := referenced()
	if err != nil {
		return nil, fmt.Errorf("storage: s3: gc: referenced set: %w", err)
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
		if obj.LastModified.After(graceCutoff) {
			continue // inside grace window: skip
		}
		candidates = append(candidates, sha)
		if apply {
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
	received int64
	done     bool
	poisoned bool
	cause    error
}

// ID returns the session uuid.
func (s *s3Session) ID() string { return s.id }

// Append uploads r as a new part in the multipart upload. The part is buffered
// in memory to compute digests and then uploaded to S3 via PutObjectPart.
// Returns the cumulative offset.
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

	// Buffer the part to compute digests and get a known size. S3 PutObjectPart
	// requires a reader with known size (io.ReadSeeker or io.Reader + S3 upload
	// will read the entire body). We use a bytes.Buffer.
	var buf bytes.Buffer
	tee := io.TeeReader(r, &buf)
	// Write to both the digesters and a discard to get the byte count.
	written, err := io.Copy(io.MultiWriter(s.digests.writer(), io.Discard), tee)
	if err != nil {
		s.poison(err)
		return 0, fmt.Errorf("storage: s3: append session %s: %w", s.id, err)
	}
	if written == 0 {
		return s.received, nil
	}

	partNumber := len(s.parts) + 1
	uploadInfo, err := s.eng.core.PutObjectPart(ctx, s.eng.bucket, s.uploadKey, s.uploadID,
		partNumber, bytes.NewReader(buf.Bytes()), int64(buf.Len()), minio.PutObjectPartOptions{})
	if err != nil {
		s.poison(err)
		return 0, fmt.Errorf("storage: s3: append session %s: part %d: %w", s.id, partNumber, err)
	}

	s.parts = append(s.parts, minio.CompletePart{
		PartNumber: partNumber,
		ETag:       uploadInfo.ETag,
	})
	s.received += written
	return s.received, nil
}

// Commit finalizes the multipart upload:
//  1. Verify expected digests against streamed content.
//  2. CompleteMultipartUpload (or PutObject for empty/zero-part payloads).
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

	// Check if the blob already exists (idempotent dedup).
	_, err := s.eng.api().StatObject(ctx, s.eng.bucket, targetKey, minio.StatObjectOptions{})
	if err == nil {
		// Blob already exists — abort the multipart upload to clean up.
		_ = s.eng.core.AbortMultipartUpload(context.Background(), s.eng.bucket, s.uploadKey, s.uploadID)
		s.finishLocked()
		return actual, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code != "NoSuchKey" && resp.StatusCode != 404 {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: stat target %s: %w", s.id, targetKey, err)
	}

	// Step 2: Complete (or put) the upload.
	if len(s.parts) == 0 {
		err := s.putEmptyBlob(ctx, targetKey, actual)
		if err != nil {
			s.failLocked()
			return BlobRef{}, err
		}
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
			"blob-created-at": time.Now().UTC().Format(time.RFC3339),
		},
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
				"blob-created-at": time.Now().UTC().Format(time.RFC3339),
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
