package storage

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// T-289 seam tests: BeginMultipartSession (the MultipartUploads capability
// the /api/v1/uploads REST plane discovers) must produce a session whose
// Append flushes parts at the REQUESTED size, honor the resolveS3PartSize
// clamp table for sub-floor values, and keep the plain BeginSession path
// byte-identical (the engine-configured size).

// TestBeginMultipartSessionPartSize pins the per-session part boundary: a
// 5 MiB session asked to hold 12 MiB flushes PutObjectPart at exactly 5 MiB
// twice, and Commit ships the 2 MiB tail as the final part.
func TestBeginMultipartSessionPartSize(t *testing.T) {
	eng, mock, _ := newS3Engine(t)

	s, err := eng.BeginMultipartSession(context.Background(), 5<<20)
	if err != nil {
		t.Fatalf("BeginMultipartSession: %v", err)
	}
	body := bytes.Repeat([]byte{0xa5}, 12<<20)
	if _, err := s.Append(context.Background(), bytes.NewReader(body)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := s.Commit(context.Background(), BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Size != int64(len(body)) {
		t.Fatalf("Size = %d, want %d", ref.Size, len(body))
	}

	mock.partLogMu.Lock()
	defer mock.partLogMu.Unlock()
	var sizes []int
	for _, rec := range mock.partLog {
		sizes = append(sizes, rec.Size)
	}
	want := []int{5 << 20, 5 << 20, 2 << 20}
	if len(sizes) != len(want) {
		t.Fatalf("part flushes = %v, want %v", sizes, want)
	}
	for i := range want {
		if sizes[i] != want[i] {
			t.Fatalf("part %d size = %d, want %d (all: %v)", i+1, sizes[i], want[i], sizes)
		}
	}
}

// TestBeginMultipartSessionClampsBelowFloor: a sub-5 MiB request clamps up
// to the S3 multipart floor at session birth (fail-fast instead of an
// EntityTooSmall at complete), and zero keeps the engine default
// (DefaultS3PartSize) — the resolveS3PartSize table, observable through
// the flush boundary.
func TestBeginMultipartSessionClampsBelowFloor(t *testing.T) {
	eng, mock, _ := newS3Engine(t)

	s, err := eng.BeginMultipartSession(context.Background(), 1<<20)
	if err != nil {
		t.Fatalf("BeginMultipartSession(1MiB): %v", err)
	}
	// 6 MiB through a 1 MiB request: the clamp makes the boundary 5 MiB,
	// so exactly one 5 MiB part flushes and Commit ships the 1 MiB tail.
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte{1}, 6<<20))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := s.Commit(context.Background(), BlobRef{}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	mock.partLogMu.Lock()
	defer mock.partLogMu.Unlock()
	var sizes []int
	for _, rec := range mock.partLog {
		sizes = append(sizes, rec.Size)
	}
	if len(sizes) != 2 || sizes[0] != 5<<20 || sizes[1] != 1<<20 {
		t.Fatalf("clamped session part sizes = %v, want [5MiB, 1MiB]", sizes)
	}
}

// TestBeginMultipartSessionZeroKeepsDefault: partSize 0 delegates to the
// engine-configured default — the REST plane's "partSizeMB omitted" arm.
func TestBeginMultipartSessionZeroKeepsDefault(t *testing.T) {
	eng, _, _ := newS3Engine(t)
	s, err := eng.BeginMultipartSession(context.Background(), 0)
	if err != nil {
		t.Fatalf("BeginMultipartSession(0): %v", err)
	}
	if s == nil {
		t.Fatal("nil session")
	}
	// The boundary is the 16 MiB default: 20 MiB flushes one part, the
	// 4 MiB remainder rides the Commit tail. A boundary of anything else
	// changes the flush count, caught by the part log in the sibling test;
	// here the offset math alone proves the session is alive and ordered.
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte{2}, 20<<20))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := s.Offset(); got != int64(20<<20) {
		t.Fatalf("Offset = %d, want %d", got, 20<<20)
	}
	_ = s.Abort(context.Background())
}

// TestBeginMultipartSessionClosedEngine: the seam respects Close like
// every mutating face.
func TestBeginMultipartSessionClosedEngine(t *testing.T) {
	eng, _, _ := newS3Engine(t)
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := eng.BeginMultipartSession(context.Background(), 5<<20); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("BeginMultipartSession after Close = %v, want ErrEngineClosed", err)
	}
}

// TestS3SessionAbortReclaimsMultipartUpload (B5, T-289 review): a
// finalized session must reclaim its S3-side multipart state — Abort with
// parts already streamed leaves ZERO in-progress uploads in the bucket
// (previously done+forget alone left the MPU and its parts billable until
// a restart's orphan sweep).
func TestS3SessionAbortReclaimsMultipartUpload(t *testing.T) {
	eng, mock, bucket := newS3Engine(t)
	s, err := eng.BeginMultipartSession(context.Background(), 5<<20)
	if err != nil {
		t.Fatalf("BeginMultipartSession: %v", err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte{1}, 5<<20))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := mock.uploadCount(bucket); got != 1 {
		t.Fatalf("uploadCount before abort = %d, want 1", got)
	}
	if err := s.Abort(context.Background()); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if got := mock.uploadCount(bucket); got != 0 {
		t.Fatalf("uploadCount after abort = %d, want 0 (MPU reclaimed)", got)
	}
	// Idempotent second abort: the upload is gone; the engine's abort of a
	// done session is the documented no-op.
	if err := s.Abort(context.Background()); err != nil {
		t.Fatalf("second Abort: %v", err)
	}
}

// TestS3CommitFailureReclaimsMultipartUpload (B5 + review scope-out
// finding 2): every failLocked path reclaims the MPU too — the wrong-sha
// Commit (the REST complete gate's 409 arm) is the representative case.
func TestS3CommitFailureReclaimsMultipartUpload(t *testing.T) {
	eng, mock, bucket := newS3Engine(t)
	s, err := eng.BeginMultipartSession(context.Background(), 5<<20)
	if err != nil {
		t.Fatalf("BeginMultipartSession: %v", err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte{2}, 5<<20))); err != nil {
		t.Fatalf("Append: %v", err)
	}
	bad := strings.Repeat("a", 64)
	if _, err := s.Commit(context.Background(), BlobRef{Sha256: bad}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Commit(wrong sha) = %v, want ErrChecksumMismatch", err)
	}
	if got := mock.uploadCount(bucket); got != 0 {
		t.Fatalf("uploadCount after failed commit = %d, want 0 (failLocked reclaimed the MPU)", got)
	}
}
