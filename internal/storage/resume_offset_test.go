package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// frozenResumeClock pins the engine clock (Options.Now) so expiry boundaries
// in TestResumeSessionExpiredFailClosed are exact: rows seeded at or around it
// decide resumability deterministically, independent of wall time.
var frozenResumeClock = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

// seedResumeRow fabricates a crash-residue row for id with an explicit
// expires_at, plus the uploads/<id>/ directory and (when data is non-empty)
// a partial data file. Rows are seeded AFTER OpenEngine on purpose: the
// startup sweep ran against an empty store with real time.Now, so only
// ResumeSession's own expiry check can reject them — that check is the code
// under test.
func seedResumeRow(t *testing.T, root string, store *memUploadSessions, id, expiresAt string, data []byte) {
	t.Helper()
	st := sessionState{Version: 1, ID: id, CreatedAt: frozenResumeClock, Received: int64(len(data))}
	if err := store.Create(context.Background(), &metadata.UploadSession{
		ID:        id,
		State:     marshalSessionState(st),
		CreatedAt: frozenResumeClock.UTC().Format(time.RFC3339),
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("seed session row %s: %v", id, err)
	}
	dir := filepath.Join(root, uploadsDirName, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("seed session dir %s: %v", dir, err)
	}
	if len(data) > 0 {
		if err := os.WriteFile(filepath.Join(dir, dataFileName), data, 0o600); err != nil {
			t.Fatalf("seed session data %s: %v", id, err)
		}
	}
}

// TestSessionOffset pins the Offset contract (architecture section 3.1 [M7],
// section 5.3.1 contract 1): the disk backend's value is the uploads/<id>/data
// file length — the authoritative offset source for REST resume — and reads
// serialize against Append/Commit.
func TestSessionOffset(t *testing.T) {
	ctx := context.Background()

	t.Run("live session tracks the data-file length", func(t *testing.T) {
		root := t.TempDir()
		eng := newEngineAt(t, root, Options{})
		s, err := eng.BeginSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Offset(); got != 0 {
			t.Fatalf("Offset before any Append = %d, want 0", got)
		}
		var want int64
		for _, part := range []string{"alpha", "beta", "gamma"} {
			if _, err := s.Append(ctx, strings.NewReader(part)); err != nil {
				t.Fatal(err)
			}
			want += int64(len(part))
			if got := s.Offset(); got != want {
				t.Fatalf("Offset after %q = %d, want %d", part, got, want)
			}
			// The disk backend's offset IS the data-file length.
			info, err := os.Stat(filepath.Join(root, uploadsDirName, s.ID(), dataFileName))
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != s.Offset() {
				t.Fatalf("data file size = %d, Offset = %d, want equal", info.Size(), s.Offset())
			}
		}
	})

	t.Run("resumed session offset is the re-derived file length", func(t *testing.T) {
		root := t.TempDir()
		store := newMemUploadSessions()
		eng := newEngineAt(t, root, Options{Sessions: store})
		s, err := eng.BeginSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		first := "first chunk; "
		if _, err := s.Append(ctx, strings.NewReader(first)); err != nil {
			t.Fatal(err)
		}
		id := s.ID()

		// Crash: a second engine over the same root resumes the id; Offset
		// must report the re-derived file length (NOT the row's bookkeeping,
		// NOT zero) — this is the value REST resume re-exposes.
		eng2 := newEngineAt(t, root, Options{Sessions: store})
		rs, err := eng2.ResumeSession(ctx, id)
		if err != nil {
			t.Fatalf("ResumeSession: %v", err)
		}
		if got := rs.Offset(); got != int64(len(first)) {
			t.Fatalf("Offset after resume = %d, want %d (re-derived file length)", got, len(first))
		}
		second := "second chunk"
		if _, err := rs.Append(ctx, strings.NewReader(second)); err != nil {
			t.Fatal(err)
		}
		if got := rs.Offset(); got != int64(len(first)+len(second)) {
			t.Fatalf("Offset after post-resume Append = %d, want %d", got, len(first)+len(second))
		}
		ref, err := rs.Commit(ctx, BlobRef{})
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("%x", sha256.Sum256([]byte(first+second)))
		if ref.Sha256 != want {
			t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, want)
		}
	})

	t.Run("Offset is safe to read while Append runs", func(t *testing.T) {
		eng := newEngine(t, Options{})
		s, err := eng.BeginSession(ctx)
		if err != nil {
			t.Fatal(err)
		}
		chunk := bytes.Repeat([]byte("x"), 4096)
		const rounds = 64
		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			prev := int64(-1)
			for {
				select {
				case <-stop:
					return
				default:
				}
				// A concurrent reader must never observe a mid-write
				// counter: values are monotonic and bounded by the total.
				off := s.Offset()
				if off < prev || off > rounds*int64(len(chunk)) {
					t.Errorf("Offset observed %d after %d (want monotonic, <= %d)", off, prev, rounds*len(chunk))
					return
				}
				prev = off
			}
		}()
		for i := 0; i < rounds; i++ {
			if _, err := s.Append(ctx, bytes.NewReader(chunk)); err != nil {
				t.Fatal(err)
			}
		}
		close(stop)
		wg.Wait()
		if got := s.Offset(); got != rounds*int64(len(chunk)) {
			t.Fatalf("final Offset = %d, want %d", got, rounds*len(chunk))
		}
	})
}

// TestResumeSessionExpiredFailClosed pins section 5.3.1 contract 3: an
// expired-but-not-yet-swept row is unknown (ErrSessionNotFound, fail-closed),
// on the same <= boundary the sweep's ListExpired uses. Table-driven per the
// ticket: unexpired resumable / expired boundary / no row / row without a
// data file (O_CREATE re-zero semantics preserved).
func TestResumeSessionExpiredFailClosed(t *testing.T) {
	now := frozenResumeClock
	root := t.TempDir()
	store := newMemUploadSessions()
	eng := newEngineAt(t, root, Options{Sessions: store, Now: func() time.Time { return now }})
	ctx := context.Background()

	t.Run("unexpired row resumes", func(t *testing.T) {
		id := "aaaa0000-0000-4000-8000-000000000001"
		data := []byte("partial upload bytes")
		seedResumeRow(t, root, store, id, now.Add(time.Hour).UTC().Format(time.RFC3339), data)
		s, err := eng.ResumeSession(ctx, id)
		if err != nil {
			t.Fatalf("ResumeSession(unexpired): %v", err)
		}
		if got := s.Offset(); got != int64(len(data)) {
			t.Fatalf("Offset = %d, want %d", got, len(data))
		}
	})

	// The sweep reclaims rows with expires_at <= now; a resume racing it must
	// fail closed on exactly the same boundary, never disagree about which
	// rows are reclaimable. Degenerate expires_at values (empty, malformed)
	// fail closed too: expiry is a write-path invariant.
	expired := []struct {
		name      string
		expiresAt string
	}{
		{"strictly past", now.Add(-time.Minute).UTC().Format(time.RFC3339)},
		{"exactly now (<= boundary)", now.UTC().Format(time.RFC3339)},
		{"empty expires_at", ""},
		{"malformed expires_at", "not-a-timestamp"},
	}
	for i, tc := range expired {
		t.Run("expired row fails closed: "+tc.name, func(t *testing.T) {
			id := fmt.Sprintf("aaaa0000-0000-4000-8000-%012d", 2+i)
			data := []byte("bytes the sweep will reclaim")
			seedResumeRow(t, root, store, id, tc.expiresAt, data)
			if _, err := eng.ResumeSession(ctx, id); !errors.Is(err, ErrSessionNotFound) {
				t.Fatalf("ResumeSession err = %v, want ErrSessionNotFound", err)
			}
			// Fail-closed means rejected, not reclaimed: the row and its data
			// file must survive untouched for the sweep (the row's only
			// legitimate reclamation path) to collect.
			if _, err := store.Get(ctx, id); err != nil {
				t.Fatalf("rejected resume deleted the row: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(root, uploadsDirName, id, dataFileName))
			if err != nil {
				t.Fatalf("rejected resume touched the data file: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("data file mutated by rejected resume: %q", got)
			}
		})
	}

	t.Run("no row fails closed", func(t *testing.T) {
		if _, err := eng.ResumeSession(ctx, "aaaa0000-0000-4000-8000-00000000dead"); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("ResumeSession(missing row) err = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("row without data file resumes at zero", func(t *testing.T) {
		id := "aaaa0000-0000-4000-8000-00000000feed"
		seedResumeRow(t, root, store, id, now.Add(time.Hour).UTC().Format(time.RFC3339), nil)
		s, err := eng.ResumeSession(ctx, id)
		if err != nil {
			t.Fatalf("ResumeSession(row without data file): %v", err)
		}
		if got := s.Offset(); got != 0 {
			t.Fatalf("Offset = %d, want 0 (O_CREATE re-zero)", got)
		}
		// The re-zeroed session is fully usable: append + commit round-trip.
		payload := []byte("written after re-zero")
		if off, err := s.Append(ctx, bytes.NewReader(payload)); err != nil || off != int64(len(payload)) {
			t.Fatalf("Append after re-zero = (%d, %v), want (%d, nil)", off, err, len(payload))
		}
		ref, err := s.Commit(ctx, BlobRef{})
		if err != nil {
			t.Fatalf("Commit after re-zero: %v", err)
		}
		if want := fmt.Sprintf("%x", sha256.Sum256(payload)); ref.Sha256 != want {
			t.Fatalf("sha256 after re-zero = %s, want %s", ref.Sha256, want)
		}
	})
}

// TestS3SessionOffsetTracksAppends covers the S3 leg of the Offset contract:
// an in-flight multipart session reports its received bytes. Rebuilt
// (resumed) sessions' Offset is covered by the resume suite in
// s3_resume_test.go; the store-less ResumeSession posture is pinned by
// TestS3ResumeSessionStoreless.
func TestS3SessionOffsetTracksAppends(t *testing.T) {
	eng, _, _ := newS3Engine(t)
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Offset(); got != 0 {
		t.Fatalf("Offset before Append = %d, want 0", got)
	}
	if _, err := s.Append(context.Background(), strings.NewReader("alpha")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), strings.NewReader("beta")); err != nil {
		t.Fatal(err)
	}
	if got := s.Offset(); got != int64(len("alphabeta")) {
		t.Fatalf("Offset = %d, want %d", got, len("alphabeta"))
	}
	if err := s.Abort(context.Background()); err != nil {
		t.Fatal(err)
	}
}
