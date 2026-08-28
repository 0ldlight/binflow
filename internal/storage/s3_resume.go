package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// S3 multipart restart resume (T-323, paying the architecture section 11.31
// debt): the engine persists one upload_sessions row per session whose state
// blob carries the S3 upload id, and ResumeSession re-materializes a crashed
// upload from that row plus ListParts — the S3 counterpart of the disk arm's
// T-209 row-plus-rehash recovery.
//
// The digest-chain problem specific to this backend: an in-progress MPU's
// parts CANNOT be read back (S3/MinIO answer NoSuchKey for partNumber GETs
// until CompleteMultipartUpload assembles the object — verified against a
// live MinIO, T-323 log), so the disk arm's "re-hash the partial bytes"
// recovery has no S3 equivalent. A rebuilt session therefore starts with an
// EMPTY digest chain over preResumed bytes that are already durable
// server-side, and its Commit verifies through the one readable form of the
// full content — the assembled temp object — via a streaming readback gate
// (commitRebuiltLocked). Slower than an in-memory chain, never wrong: the
// gate hashes the bytes that will actually be published.

// s3SessionState is the persisted per-session bookkeeping (T-323), opaque to
// the metadata layer like the disk arm's sessionState. Version pins the
// shape; v1 is the T-323 form.
//
//   - UploadID is the irreplaceable fact: no in-process state survives a
//     kill -9, and the S3 upload id is the only handle that reaches the
//     parts again. ResumeSession fails closed without it.
//   - Received is advisory telemetry kept in step at every part flush; the
//     resumable offset is re-derived from ListParts (the pending part
//     buffer's bytes die with the process, so the row's counter can only
//     ever over-report — it is never trusted).
//   - Caller (T-323R) is the /api/v1/uploads plane's opaque protocol
//     coordinate blob, persisted verbatim at BeginMultipartSessionContext
//     and handed back by ResumeSessionContext. The engine never interprets
//     it; its absence simply keeps the row off the context-adoption lane
//     (the plain ResumeSession contract is unchanged for such rows).
type s3SessionState struct {
	Version   int    `json:"version"`
	ID        string `json:"id"`
	UploadID  string `json:"upload_id"`
	UploadKey string `json:"upload_key"`
	PartSize  int64  `json:"part_size"`
	CreatedAt string `json:"created_at"` // RFC3339 UTC
	Received  int64  `json:"received"`   // advisory; ListParts is the resume truth
	Caller    string `json:"caller,omitempty"`
}

const s3SessionStateVersion = 1

// sessionStateLocked snapshots the session's persistable state. Callers hold
// s.mu (it reads the mutable counters).
func (s *s3Session) sessionStateLocked() s3SessionState {
	return s3SessionState{
		Version:   s3SessionStateVersion,
		ID:        s.id,
		UploadID:  s.uploadID,
		UploadKey: s.uploadKey,
		PartSize:  s.partSize,
		CreatedAt: s.createdAt.UTC().Format(time.RFC3339),
		Received:  s.received,
		Caller:    s.caller,
	}
}

func marshalS3SessionState(st s3SessionState) string {
	b, err := json.Marshal(st)
	if err != nil {
		return "" // unreachable: every field is a scalar
	}
	return string(b)
}

func unmarshalS3SessionState(state string) s3SessionState {
	var st s3SessionState
	if err := json.Unmarshal([]byte(state), &st); err != nil {
		return s3SessionState{}
	}
	return st
}

// resumeSession re-materializes an in-progress multipart session from its
// persisted row and the server-side upload state:
//
//   - A missing row, a store-less engine, an expired-but-not-yet-swept row,
//     or a state blob too damaged to yield an upload id answers
//     ErrSessionNotFound (fail-closed, the disk arm's contract: the sweep is
//     the row's only legitimate reclamation path, and a resume must not race
//     it or resurrect what it cannot rebuild).
//   - requireCaller (the ResumeSessionContext lane, T-323R) additionally
//     demands a caller blob in the state: a row without one belongs to
//     another upload plane and answers ErrSessionNotFound BEFORE any S3
//     call or registry attach — the REST plane can neither adopt a foreign
//     session's coordinates nor disturb its live handle.
//   - ListParts rebuilds the committed part list; the durable byte count —
//     their size sum — becomes both the session offset and preResumed. The
//     digest chain restarts empty over those bytes (see the file comment).
//   - A row whose upload no longer exists server-side (aborted elsewhere,
//     completed by a crash-in-flight commit, reclaimed by a sweep) gets a
//     FRESH multipart upload under the same key and resumes from offset 0 —
//     the disk arm's "missing data file under a surviving row" posture.
//   - A live session registered under the same id is replaced
//     last-writer-wins; the superseded handle is detached after the engine
//     lock is released (the disk arm's lock-order rule).
func (e *S3Engine) resumeSession(ctx context.Context, id string, requireCaller bool) (*s3Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, err)
	}
	if id == "" {
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrSessionNotFound)
	}
	if e.rows == nil {
		// The pre-T-323 posture: no persistence plane, nothing to rebuild —
		// the historical hard-404 (architecture section 5.3.1 contract 5 as
		// it stood before the section 11.31 debt was paid).
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrSessionNotFound)
	}
	row, err := e.rows.Get(ctx, id)
	if err != nil {
		if errors.Is(err, metadata.ErrUploadSessionNotFound) {
			return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrSessionNotFound)
		}
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, err)
	}
	// Fail closed BEFORE touching S3: an expired row belongs to the sweep,
	// and a resume racing it must not resurrect (or here: re-attach) what is
	// about to be reclaimed.
	if sessionRowExpired(row, e.timeNow()) {
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrSessionNotFound)
	}
	st := unmarshalS3SessionState(row.State)
	if requireCaller && st.Caller == "" {
		// Not this lane's row (another upload plane's session, or one begun
		// before the context pair existed): refuse before touching S3 or the
		// session registry, leaving the row and any live handle exactly as
		// they were.
		return nil, fmt.Errorf("storage: s3: resume session %s: %w: persisted state carries no caller context", id, ErrSessionNotFound)
	}
	if st.UploadID == "" {
		return nil, fmt.Errorf("storage: s3: resume session %s: %w: persisted state carries no upload id", id, ErrSessionNotFound)
	}
	uploadKey := st.UploadKey
	if uploadKey == "" {
		uploadKey = e.uploadKeyPrefix() + id + "/data" // derivable: belt for hand-seeded rows
	}
	createdAt := e.timeNow()
	if t, err := time.Parse(time.RFC3339, row.CreatedAt); err == nil {
		createdAt = t
	}

	s := &s3Session{
		eng:        e,
		id:         id,
		uploadID:   st.UploadID,
		uploadKey:  uploadKey,
		partSize:   resolveS3PartSize(st.PartSize),
		digests:    newDigesters(),
		createdAt:  createdAt,
		caller:     st.Caller,
		preResumed: 0,
	}

	parts, err := e.listParts(ctx, uploadKey, st.UploadID)
	if err != nil {
		if isNoSuchUpload(err) {
			// The upload is gone server-side; the row survives unexpired.
			// Re-open a fresh multipart upload under the same key and
			// resume from zero — the client restarts its stream from the
			// (now zero) offset, exactly like the disk arm's recreated
			// empty data file. The row's state must carry the NEW upload id
			// before this session is usable: a crash before the SetState
			// simply re-enters this arm.
			freshCtx := context.WithoutCancel(ctx)
			newID, nerr := e.core.NewMultipartUpload(freshCtx, e.bucket, uploadKey, minio.PutObjectOptions{
				ContentType: "application/octet-stream",
			})
			if nerr != nil {
				return nil, fmt.Errorf("storage: s3: resume session %s: re-open upload: %w", id, nerr)
			}
			s.uploadID = newID
			if serr := e.rows.SetState(freshCtx, id, marshalS3SessionState(s.sessionStateLocked())); serr != nil {
				_ = e.core.AbortMultipartUpload(freshCtx, e.bucket, uploadKey, newID)
				return nil, fmt.Errorf("storage: s3: resume session %s: persist re-opened upload: %w", id, serr)
			}
		} else {
			return nil, fmt.Errorf("storage: s3: resume session %s: list parts: %w", id, err)
		}
	} else {
		// Rebuild from the durable parts: the offset is their size sum (the
		// pending part buffer's bytes died with the process), the part
		// list carries their ETags for CompleteMultipartUpload, and the
		// digest chain restarts empty over the preResumed prefix.
		for _, p := range parts {
			s.parts = append(s.parts, minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag})
			s.received += p.Size
		}
		s.preResumed = s.received
	}

	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, fmt.Errorf("storage: s3: resume session %s: %w", id, ErrEngineClosed)
	}
	var old *s3Session
	if prev, ok := e.sessions[id]; ok {
		old = prev
	}
	e.sessions[id] = s
	e.mu.Unlock()
	if old != nil {
		// Detach AFTER releasing e.mu (locking s.mu under e.mu would invert
		// the s.mu -> e.mu order finishLocked/forgetSession establish).
		// detach preserves the shared upload and row: the replacement owns
		// them, and cleaning up here would destroy the session being
		// returned.
		old.detach()
	}
	return s, nil
}

// listParts enumerates every uploaded part of an in-progress multipart
// upload, paginating with the part-number marker until the listing is
// exhausted. The result is sorted by part number (S3 returns ascending
// anyway; the sort pins it against S3-compatible variance).
func (e *S3Engine) listParts(ctx context.Context, uploadKey, uploadID string) ([]minio.ObjectPart, error) {
	const maxParts = 1000
	var all []minio.ObjectPart
	marker := 0
	for {
		res, err := e.core.ListObjectParts(ctx, e.bucket, uploadKey, uploadID, marker, maxParts)
		if err != nil {
			return nil, err
		}
		all = append(all, res.ObjectParts...)
		if !res.IsTruncated {
			break
		}
		marker = res.NextPartNumberMarker
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("storage: s3: list parts: %w", err)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].PartNumber < all[j].PartNumber })
	return all, nil
}

// isNoSuchUpload reports whether err is the S3 NoSuchUpload verdict (the
// upload id no longer resolves: aborted, completed, or reclaimed). Some
// S3-compatible stores report it as a bare 404 with an empty code — the same
// tolerance the engine's other error mappings apply.
func isNoSuchUpload(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchUpload" || (resp.Code == "" && resp.StatusCode == 404)
}

// commitRebuiltLocked finalizes a ResumeSession-rebuilt session
// (preResumed > 0). The in-memory digest chain covers only the post-resume
// bytes, so the expected-digest gate runs against the assembled OBJECT —
// the one readable form of the full content:
//
//  1. Flush the trailing partial part (if any) into the private sessions/
//     key. The fresh path gates before its tail ships; this path cannot,
//     and the relaxation is bounded to a key no client or GC can observe —
//     a rejected upload below discards the assembled object wholesale, so
//     nothing ever reaches the blob store on a mismatch.
//  2. CompleteMultipartUpload — the upload key becomes a readable object.
//  3. Readback gate: stream the object once through the three digesters and
//     verify against expect (normalizeHex, the same table as the fresh
//     gate). A mismatch discards the object and reports
//     ErrChecksumMismatch; the readback's own sums become the published
//     BlobRef when expect leaves a digest unspecified.
//  4. Acquire the in-flight GC hold, dedup-stat, then the fresh path's
//     copy-with-metadata + temp-delete publish.
//
// Callers hold s.mu.
func (s *s3Session) commitRebuiltLocked(ctx context.Context, expect BlobRef) (BlobRef, error) {
	// Step 1: the tail part (if any). preResumed > 0 implies the rebuild
	// found parts; a part-less rebuild is the fresh-MPU arm, which sets
	// preResumed = 0 and never reaches here.
	if len(s.partBuf) > 0 {
		if err := s.flushPartLocked(ctx); err != nil {
			s.failLocked()
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w", s.id, err)
		}
	}
	if len(s.parts) == 0 {
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: rebuilt session has no parts", s.id)
	}

	// Step 2: assemble. The completed object exists only under the private
	// sessions/ key — invisible to clients and to every GC listing.
	if _, err := s.eng.core.CompleteMultipartUpload(ctx, s.eng.bucket, s.uploadKey, s.uploadID, s.parts, minio.PutObjectOptions{}); err != nil {
		// A concurrent Commit of the same content may have won the publish
		// race; a declared sha256 lets the dedup probe answer it.
		if want, nerr := normalizeHex(expect.Sha256, sha256HexLen); nerr == nil && want != "" {
			if _, statErr := s.eng.api().StatObject(context.Background(), s.eng.bucket,
				s.eng.objectKey(want), minio.StatObjectOptions{}); statErr == nil {
				// The complete reported failure: the temp OBJECT may not
				// exist, but the MPU may still be in-progress — abort
				// best-effort either way (B5's ruling, the fresh path's
				// race-arm posture).
				_ = s.eng.core.AbortMultipartUpload(context.Background(), s.eng.bucket, s.uploadKey, s.uploadID)
				_ = s.eng.api().RemoveObject(context.Background(), s.eng.bucket, s.uploadKey, minio.RemoveObjectOptions{})
				s.finishLocked()
				return BlobRef{Sha256: want, Size: s.received}, nil
			}
		}
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: complete multipart: %w", s.id, err)
	}
	// From here the assembled temp OBJECT (not just an MPU) exists; every
	// failure path below must RemoveObject it, not merely abort the upload.
	discardTemp := func() {
		_ = s.eng.api().RemoveObject(context.Background(), s.eng.bucket, s.uploadKey, minio.RemoveObjectOptions{})
	}

	// Step 3: the readback gate — one streaming pass, three digests, memory
	// independent of size. This is the byte-for-byte verification of what
	// will be published.
	obj, err := s.eng.api().GetObject(ctx, s.eng.bucket, s.uploadKey, minio.GetObjectOptions{})
	if err != nil {
		discardTemp()
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: read back assembled object: %w", s.id, err)
	}
	d := newDigesters()
	size, err := io.Copy(d.writer(), obj)
	_ = obj.Close()
	if err != nil {
		discardTemp()
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: read back assembled object: %w", s.id, err)
	}
	sums := d.sums()
	actual := BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: size}
	if size != s.received {
		// The assembled object's length disagrees with the parts it was
		// built from: server-side inconsistency, fail closed on it.
		discardTemp()
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: assembled %d bytes, sessions accounts %d",
			s.id, ErrBlobCorrupt, size, s.received)
	}
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
			discardTemp()
			s.failLocked()
			bad := fmt.Errorf("%s: %w", chk.name, err)
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: %w", s.id, ErrChecksumMismatch, bad)
		}
		if want != "" && want != chk.got {
			discardTemp()
			s.failLocked()
			return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: %w: %s received %s, assembled object hashes to %s",
				s.id, ErrChecksumMismatch, chk.name, want, chk.got)
		}
	}

	// Step 4: publish. The hold lands before anything can make the blob key
	// visible (the CopyObject below) — the same anchoring invariant as the
	// fresh path, where the visibility moment is identical.
	s.eng.holds.acquire(actual.Sha256)
	holdKept := false
	defer func() {
		if !holdKept {
			s.eng.holds.release(actual.Sha256)
		}
	}()

	targetKey := s.eng.objectKey(actual.Sha256)
	if _, err := s.eng.api().StatObject(ctx, s.eng.bucket, targetKey, minio.StatObjectOptions{}); err == nil {
		// Blob already exists — idempotent dedup; the temp object is pure
		// residue now.
		discardTemp()
		holdKept = true
		s.finishLocked()
		return actual, nil
	}

	_, err = s.eng.api().CopyObject(ctx, minio.CopyDestOptions{
		Bucket: s.eng.bucket,
		Object: targetKey,
		UserMetadata: map[string]string{
			blobCreatedAtMetaKey: s.eng.timeNow().UTC().Format(time.RFC3339),
		},
		// See the fresh path's note: without ReplaceMetadata the COPY keeps
		// the source's metadata and silently drops UserMetadata.
		ReplaceMetadata: true,
	}, minio.CopySrcOptions{
		Bucket: s.eng.bucket,
		Object: s.uploadKey,
	})
	if err != nil {
		discardTemp()
		s.failLocked()
		return BlobRef{}, fmt.Errorf("storage: s3: commit session %s: copy to target %s: %w", s.id, targetKey, err)
	}
	_ = s.eng.api().RemoveObject(context.Background(), s.eng.bucket, s.uploadKey, minio.RemoveObjectOptions{})

	holdKept = true
	s.finishLocked()
	return actual, nil
}
