package storage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The T-323 resume suite: the S3 arm's restart resume (upload id rows +
// ListParts rebuild, the architecture section 11.31 debt). The crash model
// throughout is engine replacement — a fresh S3Engine over the same bucket,
// the same rows store and no shared in-process state — which is exactly what
// a kill -9 leaves behind: the server-side MPU and the upload_sessions row
// survive, everything in the process dies. The live-MinIO leg at the bottom
// upgrades the same model to a real SIGKILL of a real process.

// newS3EngineWithRows opens a wired engine (T-323): same mock bucket, same
// rows store, fresh process state.
func newS3EngineWithRows(t *testing.T, mock *mockS3Server, bucket string, rows metadata.UploadSessionStore, now func() time.Time) *S3Engine {
	t.Helper()
	eng, err := OpenS3Engine(mock.core, bucket, &S3EngineOptions{
		Sessions: rows,
		Now:      now,
	})
	if err != nil {
		t.Fatalf("OpenS3Engine: %v", err)
	}
	s3e, ok := eng.(*S3Engine)
	if !ok {
		t.Fatal("OpenS3Engine did not return *S3Engine")
	}
	t.Cleanup(func() { _ = s3e.Close() })
	return s3e
}

// s3DigestTriple is the test-side digest of content (sha256 + size: the
// fields the resume assertions reconcile against).
func s3DigestTriple(content []byte) BlobRef {
	s256 := sha256.Sum256(content)
	return BlobRef{Sha256: hex.EncodeToString(s256[:]), Size: int64(len(content))}
}

// TestS3ResumeSessionFailClosed is the fail-closed table: every malformed
// resume premise answers ErrSessionNotFound, never a half-rebuilt session.
func TestS3ResumeSessionFailClosed(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "no rows store wired",
			run: func(t *testing.T) {
				eng := newS3EngineWithRows(t, mock, bucket, nil, nil)
				s, err := eng.BeginSession(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer s.Abort(context.Background()) //nolint:errcheck // cleanup best-effort; the resume refusal is the assertion
				if _, err := eng.ResumeSession(context.Background(), s.ID()); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
				}
			},
		},
		{
			name: "unknown id",
			run: func(t *testing.T) {
				rows := newMemUploadSessions()
				eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
				if _, err := eng.ResumeSession(context.Background(), "no-such-id"); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
				}
			},
		},
		{
			name: "blank id",
			run: func(t *testing.T) {
				rows := newMemUploadSessions()
				eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
				if _, err := eng.ResumeSession(context.Background(), ""); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
				}
			},
		},
		{
			name: "expired-but-not-yet-swept row",
			run: func(t *testing.T) {
				rows := newMemUploadSessions()
				eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
				s, err := eng.BeginSession(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				expireAllRows(t, rows, time.Hour)
				if _, err := eng.ResumeSession(context.Background(), s.ID()); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
				}
				// The failed resume must not have consumed the row: the
				// sweep is its only reclamation path.
				if _, err := rows.Get(context.Background(), s.ID()); err != nil {
					t.Fatalf("row consumed by rejected resume: %v", err)
				}
			},
		},
		{
			name: "state blob without an upload id",
			run: func(t *testing.T) {
				rows := newMemUploadSessions()
				now := time.Now()
				if err := rows.Create(context.Background(), &metadata.UploadSession{
					ID:        "hand-seeded",
					State:     `{"version":1,"id":"hand-seeded"}`,
					CreatedAt: now.UTC().Format(time.RFC3339),
					ExpiresAt: now.Add(time.Hour).UTC().Format(time.RFC3339),
				}); err != nil {
					t.Fatal(err)
				}
				eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
				if _, err := eng.ResumeSession(context.Background(), "hand-seeded"); !errors.Is(err, ErrSessionNotFound) {
					t.Fatalf("ResumeSession error = %v, want ErrSessionNotFound", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

// TestS3ResumeSessionRebuilds is the happy rebuild: two engines over one
// bucket + rows store, the second one resuming what the first began. The
// durable offset is the ListParts size sum — the pending part buffer's bytes
// die with the process, so a resume from mid-part reports the last FLUSHED
// boundary even though the row's advisory received counter is higher.
func TestS3ResumeSessionRebuilds(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize // 5 MiB parts: deterministic flush boundaries
	part1 := make([]byte, MinS3PartSize)
	part2 := make([]byte, MinS3PartSize)
	pending := make([]byte, 100<<10)
	for _, b := range [][]byte{part1, part2, pending} {
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
	}

	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), io.MultiReader(
		bytes.NewReader(part1), bytes.NewReader(part2), bytes.NewReader(pending))); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	// The crash: the first engine's process state is abandoned outright
	// (kill -9 semantics — no Close runs; the test cleanup handles the
	// harmless leftovers).
	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, err := second.ResumeSession(context.Background(), id)
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if got := rs.Offset(); got != 2*MinS3PartSize {
		t.Fatalf("resumed Offset = %d, want %d (two flushed parts; the %d pending bytes died with the process)",
			got, 2*MinS3PartSize, len(pending))
	}
	// The row's advisory counter must reflect the flushes (>= durable) — it
	// is observability, never the resume truth.
	row, err := rows.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if st := unmarshalS3SessionState(row.State); st.Received < 2*MinS3PartSize {
		t.Fatalf("row received = %d, want >= %d (flushed parts are persisted)", st.Received, 2*MinS3PartSize)
	}

	// Resume continues: the client re-sends from the durable offset (the
	// pending 100 KiB plus nothing else), commit lands, content reconciles.
	whole := append(append(append([]byte{}, part1...), part2...), pending...)
	if _, err := rs.Append(context.Background(), bytes.NewReader(pending)); err != nil {
		t.Fatal(err)
	}
	ref, err := rs.Commit(context.Background(), s3DigestTriple(whole))
	if err != nil {
		t.Fatalf("Commit after resume: %v", err)
	}
	if want := s3DigestTriple(whole).Sha256; ref.Sha256 != want {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, want)
	}
	if ref.Size != int64(len(whole)) {
		t.Fatalf("commit size = %d, want %d", ref.Size, len(whole))
	}
	// The terminal verb consumed the row.
	if _, err := rows.Get(context.Background(), id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
		t.Fatalf("row survived Commit: %v", err)
	}
	// Byte-for-byte reconciliation through the engine's own read path.
	rc, _, err := second.Open(context.Background(), ref.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, whole) {
		t.Fatalf("reconciled %d bytes, want %d", len(got), len(whole))
	}
}

// TestS3ResumeCommitChecksumMismatchDiscards: a resumed session whose
// declared digest disagrees with the assembled content is rejected whole —
// no blob lands, the assembled temp object is discarded, the row is
// consumed, and the bucket carries no residue.
func TestS3ResumeCommitChecksumMismatchDiscards(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	part1 := bytes.Repeat([]byte("."), int(MinS3PartSize))
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(part1)); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, err := second.ResumeSession(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	bad := BlobRef{Sha256: strings.Repeat("0", 64), Size: int64(len(part1))}
	if _, err := rs.Commit(context.Background(), bad); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Commit error = %v, want ErrChecksumMismatch", err)
	}
	if _, _, err := second.Open(context.Background(), bad.Sha256); !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("Open after rejected commit: %v, want ErrBlobNotFound", err)
	}
	if _, err := rows.Get(context.Background(), id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
		t.Fatalf("row survived rejected commit: %v", err)
	}
	// Neither an in-progress MPU nor the assembled temp object may linger.
	uploads := len(mock.buckets[bucket].uploads)
	objects := len(mock.buckets[bucket].objects)
	if uploads != 0 {
		t.Fatalf("%d multipart uploads linger after rejected commit", uploads)
	}
	if objects != 0 {
		t.Fatalf("%d objects linger after rejected commit (temp not discarded)", objects)
	}
}

// TestS3ResumeNoSuchUploadRestartsFromZero: a row whose upload no longer
// exists server-side (aborted elsewhere / reclaimed by a sweep) gets a fresh
// multipart upload under the same key and resumes from offset 0 — the disk
// arm's recreated-empty-data-file posture.
func TestS3ResumeNoSuchUploadRestartsFromZero(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte("."), int(MinS3PartSize)))); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	// Out-of-band abort: the sweep, an operator, a lifecycle rule.
	row, err := rows.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	st := unmarshalS3SessionState(row.State)
	if err := mock.core.AbortMultipartUpload(context.Background(), bucket, st.UploadKey, st.UploadID); err != nil {
		t.Fatal(err)
	}

	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, err := second.ResumeSession(context.Background(), id)
	if err != nil {
		t.Fatalf("ResumeSession after NoSuchUpload: %v", err)
	}
	if got := rs.Offset(); got != 0 {
		t.Fatalf("resumed Offset = %d, want 0 (fresh upload)", got)
	}
	// The row now carries the replacement upload id.
	row, err = rows.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if st2 := unmarshalS3SessionState(row.State); st2.UploadID == st.UploadID || st2.UploadID == "" {
		t.Fatalf("row upload id = %q, want a fresh non-empty id (was %q)", st2.UploadID, st.UploadID)
	}

	whole := bytes.Repeat([]byte("C"), int(3*MinS3PartSize)+12345)
	if _, err := rs.Append(context.Background(), bytes.NewReader(whole)); err != nil {
		t.Fatal(err)
	}
	ref, err := rs.Commit(context.Background(), s3DigestTriple(whole))
	if err != nil {
		t.Fatalf("Commit after fresh re-open: %v", err)
	}
	if ref.Sha256 != s3DigestTriple(whole).Sha256 {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, s3DigestTriple(whole).Sha256)
	}
}

// TestS3ResumeExpirySweepReclaimsRowsAndUploads: the open-time sweep reclaims
// an expired row TOGETHER WITH its server-side multipart state.
func TestS3ResumeExpirySweepReclaimsRowsAndUploads(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte("."), int(MinS3PartSize)))); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	expireAllRows(t, rows, time.Hour)
	// The restarting engine (the sweep runs at open).
	_ = newS3EngineWithRows(t, mock, bucket, rows, nil)

	if _, err := rows.Get(context.Background(), id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
		t.Fatalf("expired row survived the sweep: %v", err)
	}
	n := len(mock.buckets[bucket].uploads)
	if n != 0 {
		t.Fatalf("%d multipart uploads survived the sweep", n)
	}
}

// TestS3SweepAgedOrphanMPUDeletesItsRow: an orphan MPU past the TTL takes its
// row with it (the key embeds the session id) — a row must not outlive its
// upload by a TTL.
func TestS3SweepAgedOrphanMPUDeletesItsRow(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte("."), int(MinS3PartSize)))); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	// The next start happens 25 h later (TTL + 1 h): both the MPU and the
	// row are past every clock.
	_ = newS3EngineWithRows(t, mock, bucket, rows, func() time.Time {
		return time.Now().Add(25 * time.Hour)
	})
	if _, err := rows.Get(context.Background(), id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
		t.Fatalf("row outlived its aged-out MPU: %v", err)
	}
	n := len(mock.buckets[bucket].uploads)
	if n != 0 {
		t.Fatalf("%d aged multipart uploads survived the sweep", n)
	}
}

// TestS3ClosePreservesWiredSessionsForResume: with rows wired, a graceful
// Close (the SIGTERM / compose-restart 口径) preserves the multipart upload
// and its row — the ADR-0028 posture — and the restarted engine finishes the
// upload. The store-less arm keeps the historical abort and is pinned
// separately below.
func TestS3ClosePreservesWiredSessionsForResume(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	part1 := bytes.Repeat([]byte("."), int(MinS3PartSize))
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(part1)); err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	n := len(mock.buckets[bucket].uploads)
	if n != 1 {
		t.Fatalf("Close with rows wired left %d multipart uploads, want 1 (preserved)", n)
	}
	if _, err := rows.Get(context.Background(), id); err != nil {
		t.Fatalf("Close deleted the row: %v", err)
	}

	// The restart resumes and completes.
	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, err := second.ResumeSession(context.Background(), id)
	if err != nil {
		t.Fatalf("ResumeSession after graceful Close: %v", err)
	}
	tail := bytes.Repeat([]byte("g"), 4096)
	if _, err := rs.Append(context.Background(), bytes.NewReader(tail)); err != nil {
		t.Fatal(err)
	}
	whole := append(append([]byte{}, part1...), tail...)
	ref, err := rs.Commit(context.Background(), s3DigestTriple(whole))
	if err != nil {
		t.Fatalf("Commit after graceful-close resume: %v", err)
	}
	if ref.Sha256 != s3DigestTriple(whole).Sha256 {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, s3DigestTriple(whole).Sha256)
	}
}

// TestS3CloseStorelessStillAborts: without rows nothing is resumable, so
// Close keeps the historical eager reclamation.
func TestS3CloseStorelessStillAborts(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"

	eng := newS3EngineWithRows(t, mock, bucket, nil, nil)
	eng.partSize = MinS3PartSize
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte("."), int(MinS3PartSize)))); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatal(err)
	}
	n := len(mock.buckets[bucket].uploads)
	if n != 0 {
		t.Fatalf("store-less Close left %d multipart uploads, want 0 (aborted)", n)
	}
}

// failingSetStateSessions wraps memUploadSessions with a SetState that fails
// once instructed to — the persist-failure poisoning gate.
type failingSetStateSessions struct {
	*memUploadSessions
	failSetState func(id string) bool
}

func (f *failingSetStateSessions) SetState(ctx context.Context, id, state string) error {
	if f.failSetState != nil && f.failSetState(id) {
		return errors.New("mem: injected set-state failure")
	}
	return f.memUploadSessions.SetState(ctx, id, state)
}

// TestS3PartFlushPersistFailurePoisons: a row write that fails after a part
// landed poisons the session — the disk arm's fail-closed parity (bookkeeping
// diverged from the upload; a later resume must not trust either).
func TestS3PartFlushPersistFailurePoisons(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := &failingSetStateSessions{memUploadSessions: newMemUploadSessions()}

	eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
	eng.partSize = MinS3PartSize
	var sessionID string
	rows.failSetState = func(id string) bool { return id == sessionID }

	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sessionID = s.ID()
	if _, err := s.Append(context.Background(), bytes.NewReader(bytes.Repeat([]byte("."), int(MinS3PartSize)))); err == nil {
		t.Fatal("Append over a failing persist succeeded, want the poisoned failure")
	}
	if _, err := s.Append(context.Background(), strings.NewReader("more")); !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Append after persist failure: %v, want ErrSessionPoisoned", err)
	}
	if _, err := s.Commit(context.Background(), BlobRef{}); !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Commit after persist failure: %v, want ErrSessionPoisoned", err)
	}
}

// TestS3ConcurrentResumeSameID: racing resumes of one id converge — every
// caller gets a usable session (the loser's handle is detached, its next
// mutation fails "already finalized"), and the upload completes exactly
// once with reconciled content.
func TestS3ConcurrentResumeSameID(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	part1 := bytes.Repeat([]byte("."), int(MinS3PartSize))
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(part1)); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	const racers = 8
	sessions := make([]Session, racers)
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sessions[i], errs[i] = second.ResumeSession(context.Background(), id)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("racer %d: ResumeSession: %v", i, errs[i])
		}
		if got := sessions[i].Offset(); got != MinS3PartSize {
			t.Fatalf("racer %d: Offset = %d, want %d", i, got, MinS3PartSize)
		}
	}

	// Exactly one racer's handle is the live registration (last-writer-wins,
	// the disk arm's same-id ruling); the detached handles refuse.
	second.mu.RLock()
	winner := second.sessions[id]
	second.mu.RUnlock()
	if winner == nil {
		t.Fatal("no session registered after the racing resumes")
	}
	winnerIdx := -1
	for i := range sessions {
		if sessions[i] == Session(winner) {
			winnerIdx = i
		}
	}
	if winnerIdx < 0 {
		t.Fatal("the registered session is none of the racers' handles")
	}
	tail := bytes.Repeat([]byte("k"), 8192)
	if _, err := sessions[winnerIdx].Append(context.Background(), bytes.NewReader(tail)); err != nil {
		t.Fatalf("winner Append: %v", err)
	}
	for i := 0; i < racers; i++ {
		if i == winnerIdx {
			continue
		}
		if _, err := sessions[i].Append(context.Background(), strings.NewReader("x")); err == nil {
			t.Fatalf("detached racer %d Append succeeded, want already-finalized refusal", i)
		}
	}
	whole := append(append([]byte{}, part1...), tail...)
	ref, err := sessions[winnerIdx].Commit(context.Background(), s3DigestTriple(whole))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Sha256 != s3DigestTriple(whole).Sha256 {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, s3DigestTriple(whole).Sha256)
	}
}

// TestS3CommitCompleteRaceReclaimsMPU pins the complete-race arms (B5's
// ruling extended, T-323): when CompleteMultipartUpload reports failure but
// the target blob EXISTS (another writer won the publish race), the session
// still reclaims its own multipart upload instead of leaking it to the TTL
// sweep. Both commit paths are covered — fresh (in-memory digest gate) and
// rebuilt (readback gate) — with the mock failing the complete and planting
// the winner's blob in the same instant, the interleaving a real race
// produces.
func TestS3CommitCompleteRaceReclaimsMPU(t *testing.T) {
	newCase := func(t *testing.T, resumed bool) {
		mock := newMockS3Server()
		t.Cleanup(func() { mock.Close() })
		bucket := "test-bucket"
		rows := newMemUploadSessions()

		eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
		eng.partSize = MinS3PartSize
		part1 := bytes.Repeat([]byte("L"), int(MinS3PartSize))
		want := s3DigestTriple(part1)

		s, err := eng.BeginSession(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append(context.Background(), bytes.NewReader(part1)); err != nil {
			t.Fatal(err)
		}
		id := s.ID()

		var victim Session
		if resumed {
			// The crash-resume model: a second engine rebuilds the session
			// from the row + ListParts (preResumed > 0 -> readback gate).
			second := newS3EngineWithRows(t, mock, bucket, rows, nil)
			victim, err = second.ResumeSession(context.Background(), id)
			if err != nil {
				t.Fatalf("ResumeSession: %v", err)
			}
		} else {
			victim = s
		}

		// Arm the race: the next complete FAILS while the winner's blob
		// lands under the engine's blob key in the same instant.
		mock.mu.Lock()
		mock.failNextComplete = 1
		mock.completeHook = func(b *mockBucket) {
			b.objects[eng.objectKey(want.Sha256)] = part1
			b.lastModified[eng.objectKey(want.Sha256)] = time.Now()
		}
		mock.mu.Unlock()

		ref, err := victim.Commit(context.Background(), BlobRef{Sha256: want.Sha256})
		if err != nil {
			t.Fatalf("Commit through the race: %v", err)
		}
		if ref.Sha256 != want.Sha256 {
			t.Fatalf("race-arm sha256 = %s, want %s", ref.Sha256, want.Sha256)
		}
		// THE regression: no in-progress multipart upload survives the
		// raced commit (the arm aborts best-effort).
		if n := len(mock.buckets[bucket].uploads); n != 0 {
			t.Fatalf("%d multipart uploads leaked by the complete-race arm, want 0", n)
		}
		// The winner's blob stays; the row is consumed either way.
		if _, err := rows.Get(context.Background(), id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
			t.Fatalf("row survived the raced commit: %v", err)
		}
	}
	t.Run("fresh path", func(t *testing.T) { newCase(t, false) })
	t.Run("rebuilt path", func(t *testing.T) { newCase(t, true) })
}

// TestS3ResumeMultiPartPagination: a rebuild enumerating more parts than one
// ListParts page (the mock pages at 2) still sees every part and the exact
// durable offset.
func TestS3ResumeMultiPartPagination(t *testing.T) {
	mock := newMockS3Server()
	mock.listPartsPageSize = 2 // page below the 5-part upload
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	var payload []byte
	for i := 0; i < 5; i++ {
		payload = append(payload, bytes.Repeat([]byte{byte('a' + i)}, int(MinS3PartSize))...)
	}
	s, err := first.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, err := second.ResumeSession(context.Background(), id)
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if got := rs.Offset(); got != int64(len(payload)) {
		t.Fatalf("resumed Offset = %d, want %d (5 paginated parts)", got, len(payload))
	}
	ref, err := rs.Commit(context.Background(), s3DigestTriple(payload))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Sha256 != s3DigestTriple(payload).Sha256 {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, s3DigestTriple(payload).Sha256)
	}
}

// ---------------------------------------------------------------------------
// Live-MinIO kill -9 leg (env-gated): a REAL process SIGKILL mid-upload, a
// real restart, ListParts rebuild, resume to completion and byte-for-byte
// reconciliation. Enabled by:
//
//	BINFLOW_T323_MINIO_ENDPOINT=http://127.0.0.1:29000
//	BINFLOW_T323_MINIO_ACCESS_KEY=... BINFLOW_T323_MINIO_SECRET_KEY=...
//	BINFLOW_T323_MINIO_BUCKET=t323
//
// The child (this binary re-exec'd) opens the sqlite rows store and the S3
// engine, streams one 5 MiB part, reports readiness, and blocks; the parent
// SIGKILLs it — in-memory digest chains and part buffers die with the
// process, the upload_sessions row (sqlite) and the MPU (MinIO) survive —
// then a fresh in-process engine resumes, finishes the upload and Stat
// reconciles the landed blob.
// ---------------------------------------------------------------------------

const (
	t323ChildEnv    = "BINFLOW_T323_CHILD"
	t323EndpointEnv = "BINFLOW_T323_MINIO_ENDPOINT"
	t323AccessEnv   = "BINFLOW_T323_MINIO_ACCESS_KEY"
	t323SecretEnv   = "BINFLOW_T323_MINIO_SECRET_KEY"
	t323BucketEnv   = "BINFLOW_T323_MINIO_BUCKET"
	t323DBEnv       = "BINFLOW_T323_MINIO_DB"
	t323PayloadEnv  = "BINFLOW_T323_MINIO_PAYLOAD"
	t323PartSize    = 5 << 20
)

func TestS3ResumeKill9AgainstLiveMinIO(t *testing.T) {
	if os.Getenv(t323ChildEnv) == "1" {
		t323ChildRun()
		return
	}
	endpoint := os.Getenv(t323EndpointEnv)
	if endpoint == "" {
		t.Skip("live-MinIO kill -9 leg disabled (set BINFLOW_T323_MINIO_ENDPOINT et al. to enable)")
	}

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rows.db")
	bucket := os.Getenv(t323BucketEnv)

	// The payload: 5 MiB + 5 MiB + 1 MiB (the m10-mpu-probe shape).
	var whole []byte
	for _, n := range []int{t323PartSize, t323PartSize, 1 << 20} {
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		whole = append(whole, buf...)
	}
	want := s3DigestTriple(whole)
	// The child streams the FIRST 5 MiB of this exact payload (a shared
	// file, not its own randomness): the resumed upload's content is the
	// parent's whole, which is what the commit gate reconciles against.
	payloadPath := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(payloadPath, whole, 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run", "^TestS3ResumeKill9AgainstLiveMinIO$", "-test.v")
	cmd.Env = append(os.Environ(),
		t323ChildEnv+"=1",
		t323DBEnv+"="+dbPath,
		t323PayloadEnv+"="+payloadPath,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Readiness protocol: "SESSION <id>" then "READY".
	var id string
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if after, ok := strings.CutPrefix(line, "SESSION "); ok {
			id = after
		}
		if line == "READY" {
			break
		}
	}
	if id == "" {
		_ = cmd.Process.Kill()
		t.Fatal("child never reported a session id")
	}
	t.Logf("child streaming into session %s; delivering SIGKILL", id)
	if err := cmd.Process.Kill(); err != nil { // SIGKILL: no Close, no cleanup
		t.Fatal(err)
	}
	_ = cmd.Wait()

	// ---- the restart: fresh store, fresh engine, same bucket, same rows ----
	md, err := metadata.Open(ctx, metadata.Options{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen sqlite rows store: %v", err)
	}
	defer md.Close() //nolint:errcheck // read-only from here
	core := t323MinioCore(t, endpoint)
	eng, err := OpenS3Engine(core, bucket, &S3EngineOptions{
		Sessions: md.UploadSessions(),
		PartSize: t323PartSize,
	})
	if err != nil {
		t.Fatalf("restart engine: %v", err)
	}
	defer eng.Close() //nolint:errcheck // test teardown
	s3eng := eng.(*S3Engine)

	s, err := eng.ResumeSession(ctx, id)
	if err != nil {
		t.Fatalf("ResumeSession after kill -9: %v", err)
	}
	if got := s.Offset(); got != t323PartSize {
		t.Fatalf("post-kill resumed Offset = %d, want %d (the one flushed part)", got, t323PartSize)
	}
	// The client re-sends everything past the durable offset.
	if _, err := s.Append(ctx, bytes.NewReader(whole[t323PartSize:])); err != nil {
		t.Fatalf("Append after resume: %v", err)
	}
	ref, err := s.Commit(ctx, want)
	if err != nil {
		t.Fatalf("Commit after kill -9 resume: %v", err)
	}
	if ref.Sha256 != want.Sha256 || ref.Size != want.Size {
		t.Fatalf("commit ref = %+v, want sha256 %s size %d", ref, want.Sha256, want.Size)
	}
	// Integrity reconciliation: Stat re-hashes the landed blob end to end.
	st, err := eng.Stat(ctx, ref.Sha256)
	if err != nil {
		t.Fatalf("Stat reconciliation: %v", err)
	}
	if st.Sha256 != want.Sha256 || st.Size != want.Size {
		t.Fatalf("Stat = %+v, want sha256 %s size %d", st, want.Sha256, want.Size)
	}
	rc, _, err := eng.Open(ctx, ref.Sha256)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, whole) {
		t.Fatalf("byte reconciliation failed: %d of %d bytes", len(got), len(whole))
	}
	// The session row is gone: the terminal verb consumed it.
	if _, err := md.UploadSessions().Get(ctx, id); !errors.Is(err, metadata.ErrUploadSessionNotFound) {
		t.Fatalf("row survived the resumed commit: %v", err)
	}
	// No in-progress MPU residue for the session key.
	uploads, err := s3eng.listIncompleteUploads(ctx)
	if err != nil {
		t.Fatalf("post-commit MPU audit: %v", err)
	}
	for _, u := range uploads {
		if strings.Contains(u.Key, id) {
			t.Fatalf("session %s left its multipart upload in progress (%s)", id, u.Key)
		}
	}
}

// t323ChildRun is the child half of the kill -9 leg: stream one part, report,
// block until killed.
func t323ChildRun() {
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Path: os.Getenv(t323DBEnv)})
	if err != nil {
		fmt.Println("CHILD-ERR open:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	core, err := minio.New(t323Host(os.Getenv(t323EndpointEnv)), &minio.Options{
		Creds:        credentials.NewStaticV4(os.Getenv(t323AccessEnv), os.Getenv(t323SecretEnv), ""),
		Secure:       strings.HasPrefix(os.Getenv(t323EndpointEnv), "https://"),
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		fmt.Println("CHILD-ERR client:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	eng, err := OpenS3Engine(&minio.Core{Client: core}, os.Getenv(t323BucketEnv), &S3EngineOptions{
		Sessions: md.UploadSessions(),
		PartSize: t323PartSize,
	})
	if err != nil {
		fmt.Println("CHILD-ERR engine:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	s, err := eng.BeginSession(ctx)
	if err != nil {
		fmt.Println("CHILD-ERR begin:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	part := make([]byte, t323PartSize)
	payload, err := os.ReadFile(os.Getenv(t323PayloadEnv))
	if err != nil {
		fmt.Println("CHILD-ERR payload:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	if len(payload) < t323PartSize {
		fmt.Println("CHILD-ERR payload too short:", len(payload))
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	copy(part, payload[:t323PartSize])
	if _, err := s.Append(ctx, bytes.NewReader(part)); err != nil {
		fmt.Println("CHILD-ERR append:", err)
		time.Sleep(10 * time.Minute)
		os.Exit(1)
	}
	fmt.Println("SESSION", s.ID())
	fmt.Println("READY")
	time.Sleep(10 * time.Minute) // block until the SIGKILL lands
}

// t323Host strips the scheme off a probe endpoint URL (minio.New wants a
// bare host:port).
func t323Host(endpoint string) string {
	return strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
}

// t323MinioCore builds the minio Core from the T-323 probe environment.
func t323MinioCore(t *testing.T, endpoint string) *minio.Core {
	t.Helper()
	client, err := minio.New(t323Host(endpoint), &minio.Options{
		Creds:        credentials.NewStaticV4(os.Getenv(t323AccessEnv), os.Getenv(t323SecretEnv), ""),
		Secure:       strings.HasPrefix(endpoint, "https://"),
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatalf("minio client for %s: %v", endpoint, err)
	}
	return &minio.Core{Client: client}
}

// ---- T-323R: the caller-context lane (the /api/v1/uploads coordinates) ----

// TestS3MultipartCallerContextRoundTrip: the context begin persists the
// opaque caller blob inside the session row, the context resume hands it
// back verbatim alongside the rebuilt session, the blob survives every
// later SetState (part flushes), and the plain ResumeSession contract is
// unchanged for caller-carrying rows (the docker adapter's lane).
func TestS3MultipartCallerContextRoundTrip(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()
	ctx := context.Background()

	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	first.partSize = MinS3PartSize
	caller := []byte(`{"version":1,"repoKey":"generic-local","path":"big/a.bin","mimeType":"application/x-test","partSizeBytes":5242880}`)
	s, err := first.BeginMultipartSessionContext(ctx, MinS3PartSize, caller)
	if err != nil {
		t.Fatalf("BeginMultipartSessionContext: %v", err)
	}
	part1 := make([]byte, MinS3PartSize)
	pending := make([]byte, 100<<10)
	if _, err := s.Append(ctx, io.MultiReader(bytes.NewReader(part1), bytes.NewReader(pending))); err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	// The row carries the blob, and a part FLUSH (a SetState) did not
	// strand it: the coordinates ride every re-persist untouched.
	row, err := rows.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := unmarshalS3SessionState(row.State).Caller; got != string(caller) {
		t.Fatalf("row caller state = %q after a part flush, want the verbatim blob", got)
	}

	// The crash + the restarted process's context resume.
	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	rs, gotCaller, err := second.ResumeSessionContext(ctx, id)
	if err != nil {
		t.Fatalf("ResumeSessionContext: %v", err)
	}
	if !bytes.Equal(gotCaller, caller) {
		t.Fatalf("resumed caller = %q, want verbatim %q", gotCaller, caller)
	}
	if off := rs.Offset(); off != MinS3PartSize {
		t.Fatalf("resumed offset = %d, want %d", off, MinS3PartSize)
	}

	// The rebuilt session finishes: commit reconciles the whole content.
	whole := append(append([]byte{}, part1...), pending...)
	if _, err := rs.Append(ctx, bytes.NewReader(pending)); err != nil {
		t.Fatal(err)
	}
	ref, err := rs.Commit(ctx, s3DigestTriple(whole))
	if err != nil {
		t.Fatalf("commit after context resume: %v", err)
	}
	if want := s3DigestTriple(whole).Sha256; ref.Sha256 != want {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, want)
	}

	// The plain lane is unchanged: a caller-carrying row still resumes
	// through Engine.ResumeSession (the docker adapter's posture).
	s2, err := first.BeginMultipartSessionContext(ctx, MinS3PartSize, caller)
	if err != nil {
		t.Fatal(err)
	}
	third := newS3EngineWithRows(t, mock, bucket, rows, nil)
	if _, err := third.ResumeSession(ctx, s2.ID()); err != nil {
		t.Fatalf("plain ResumeSession on a caller-carrying row: %v", err)
	}
}

// TestS3ResumeContextFailClosedAndBounds: the context lane refuses what it
// must — a caller-less row (another plane's session) answers
// ErrSessionNotFound WITHOUT consuming the row or disturbing the live
// handle, and the begin validates the blob's presence and size before
// anything opens server-side.
func TestS3ResumeContextFailClosedAndBounds(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	rows := newMemUploadSessions()
	ctx := context.Background()

	// A caller-less row is the plain lane's shape.
	first := newS3EngineWithRows(t, mock, bucket, rows, nil)
	s, err := first.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := s.ID()

	second := newS3EngineWithRows(t, mock, bucket, rows, nil)
	if _, _, err := second.ResumeSessionContext(ctx, id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ResumeSessionContext on a caller-less row = %v, want ErrSessionNotFound", err)
	}
	// Fail-closed: the row survives, and the FIRST engine's live handle was
	// neither detached nor disturbed (a REST probe of a docker-session id
	// must not break the docker push in flight).
	if _, err := rows.Get(ctx, id); err != nil {
		t.Fatalf("row consumed by the refused context resume: %v", err)
	}
	if _, err := s.Append(ctx, bytes.NewReader(bytes.Repeat([]byte("."), 16))); err != nil {
		t.Fatalf("live session disturbed by the refused context resume: %v", err)
	}

	// The begin bounds the blob before anything opens server-side.
	eng := newS3EngineWithRows(t, mock, bucket, rows, nil)
	before := mock.uploadCount(bucket)
	if _, err := eng.BeginMultipartSessionContext(ctx, MinS3PartSize, nil); err == nil {
		t.Fatal("empty caller accepted")
	}
	oversize := bytes.Repeat([]byte("x"), MaxMultipartCallerState+1)
	if _, err := eng.BeginMultipartSessionContext(ctx, MinS3PartSize, oversize); err == nil {
		t.Fatal("oversize caller accepted")
	}
	if n := mock.uploadCount(bucket) - before; n != 0 {
		t.Fatalf("%d multipart uploads leaked by the refused begins", n)
	}
}

// TestS3SweepReclaimsCompletedSessionObjects: the crash window between
// CompleteMultipartUpload and the publish (CopyObject) parks a FINISHED
// object under sessions/ that no in-progress-MPU listing can see — the
// age-gated object sweep (T-323R's ruling on T-323 §6's registered residue)
// reclaims it, while a fresh sessions object survives.
func TestS3SweepReclaimsCompletedSessionObjects(t *testing.T) {
	mock := newMockS3Server()
	t.Cleanup(func() { mock.Close() })
	bucket := "test-bucket"
	_ = mock.bucket(bucket) // the fixture's bucket must exist before any engine opens

	aged := "sessions/crash-window-uuid/data"
	fresh := "sessions/mid-commit-uuid/data"
	mock.setObject(bucket, aged, []byte("assembled but never published"))
	mock.setObject(bucket, fresh, []byte("complete in flight"))
	// The sweep runs one TTL later: `aged` was parked before the window and
	// `fresh` carries the sweep-moment timestamp (a live commit's object is
	// seconds old at the only moment it exists).
	future := time.Now().Add(DefaultSessionTTL + time.Hour)
	mock.setObjectLastModified(bucket, fresh, future)

	_ = newS3EngineWithRows(t, mock, bucket, nil, func() time.Time { return future })

	if _, ok := mock.buckets[bucket].objects[aged]; ok {
		t.Fatal("the crash-window sessions object survived the age-gated sweep")
	}
	if _, ok := mock.buckets[bucket].objects[fresh]; !ok {
		t.Fatal("the fresh sessions object was swept: a live commit window must never be touched")
	}
}
