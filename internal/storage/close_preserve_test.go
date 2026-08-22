package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// This file pins the ADR-0028 Close semantics (architecture section 5.3.1
// contract 7): a clean shutdown preserves unexpired upload sessions — their
// upload_sessions rows and uploads/<id>/ data files — so SIGTERM and compose
// restart resume exactly like kill -9 does, and the startup sweep + TTL is
// the one and only reclamation path.

// preservedData asserts that uploads/<id>/data exists with exactly the want
// bytes (the state a restart must find for resume to work).
func preservedData(t *testing.T, root, id string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, uploadsDirName, id, dataFileName))
	if err != nil {
		t.Fatalf("preserved data file for %s unreadable: %v", id, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("preserved data for %s = %q, want %q", id, got, want)
	}
}

// TestClosePreservesUnexpiredSessions is the ADR-0028 core: Close keeps the
// row + data file of an in-flight (unexpired) session, the fd detaches
// cleanly, and a fresh engine over the same root both skips the unexpired
// row in its startup sweep and can resume the session to a correct commit.
func TestClosePreservesUnexpiredSessions(t *testing.T) {
	root := t.TempDir()
	store := newMemUploadSessions()
	ctx := context.Background()
	eng, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	first := []byte("70% of a 2GiB layer, then the upgrade window hits")
	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if _, err := s.Append(ctx, bytes.NewReader(first)); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Row + data file survive the clean shutdown.
	if n := store.countRows(); n != 1 {
		t.Fatalf("rows after Close = %d, want 1 (preserved)", n)
	}
	if n := countSessionDirs(t, root); n != 1 {
		t.Fatalf("session dirs after Close = %d, want 1 (preserved)", n)
	}
	preservedData(t, root, id, first)

	// Restart: the startup sweep must NOT touch the unexpired session, and
	// the session must resume from the preserved offset (the PRD scenario C
	// shape: SIGTERM/compose restart, not just kill -9).
	eng2, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatalf("reopen after clean Close: %v", err)
	}
	defer eng2.Close() //nolint:errcheck // test
	if n := store.countRows(); n != 1 {
		t.Fatalf("rows after restart sweep = %d, want 1 (unexpired, kept)", n)
	}
	if n := countSessionDirs(t, root); n != 1 {
		t.Fatalf("session dirs after restart sweep = %d, want 1 (unexpired, kept)", n)
	}
	rs, err := eng2.ResumeSession(ctx, id)
	if err != nil {
		t.Fatalf("ResumeSession after clean Close: %v", err)
	}
	if rs.ID() != id {
		t.Fatalf("resumed id = %q, want %q", rs.ID(), id)
	}
	if off := rs.Offset(); off != int64(len(first)) {
		t.Fatalf("resumed offset = %d, want %d (preserved bytes)", off, len(first))
	}
	second := []byte("; the rest lands after the restart")
	if _, err := rs.Append(ctx, bytes.NewReader(second)); err != nil {
		t.Fatalf("Append after resume: %v", err)
	}
	ref, err := rs.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("Commit after resume: %v", err)
	}
	whole := append(append([]byte(nil), first...), second...)
	want := fmt.Sprintf("%x", sha256.Sum256(whole))
	if ref.Sha256 != want {
		t.Fatalf("resumed commit sha256 = %s, want %s", ref.Sha256, want)
	}
	// The commit consumed the session: no residue, no row.
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after post-restart commit = %d, want 0", n)
	}
	if n := store.countRows(); n != 0 {
		t.Fatalf("rows after post-restart commit = %d, want 0", n)
	}
}

// TestClosePreservesThenSweepReclaimsAfterTTL pins the single-reclamation-path
// invariant: what Close preserves, only expiry + the startup sweep reclaims.
func TestClosePreservesThenSweepReclaimsAfterTTL(t *testing.T) {
	root := t.TempDir()
	store := newMemUploadSessions()
	eng, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	id := s.ID()
	if _, err := s.Append(context.Background(), strings.NewReader("abandoned mid-upload")); err != nil {
		t.Fatal(err)
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if n := countSessionDirs(t, root); n != 1 {
		t.Fatalf("session dirs after Close = %d, want 1 (preserved)", n)
	}

	// Time passes beyond the TTL; the next boot's sweep reclaims both halves.
	expireAllRows(t, store, DefaultSessionTTL+time.Hour)
	eng2, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	defer eng2.Close() //nolint:errcheck // test
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after expired restart sweep = %d, want 0", n)
	}
	if n := store.countRows(); n != 0 {
		t.Fatalf("rows after expired restart sweep = %d, want 0", n)
	}
	// And the reclaimed id is unknown to the resume path.
	if _, err := eng2.ResumeSession(context.Background(), id); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("ResumeSession on swept id err = %v, want ErrSessionNotFound", err)
	}
}

// TestCloseOpenCloseIdempotentLoop runs two full Open-Close cycles: every
// cycle preserves its unexpired sessions, none of the cycles' sweeps reclaims
// them, and the second engine can still open over the first's residue.
func TestCloseOpenCloseIdempotentLoop(t *testing.T) {
	root := t.TempDir()
	store := newMemUploadSessions()
	ctx := context.Background()

	// Cycle 1: one in-flight session, clean Close.
	eng1, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	s1, err := eng1.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id1 := s1.ID()
	if _, err := s1.Append(ctx, strings.NewReader("cycle one partial")); err != nil {
		t.Fatal(err)
	}
	if err := eng1.Close(); err != nil {
		t.Fatalf("Close cycle 1: %v", err)
	}

	// Cycle 2: cycle 1's session must survive the sweep; add a second one.
	eng2, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatalf("reopen cycle 2: %v", err)
	}
	if _, err := eng2.ResumeSession(ctx, id1); err != nil {
		t.Fatalf("resume cycle 1 session in cycle 2: %v", err)
	}
	s2, err := eng2.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id2 := s2.ID()
	if _, err := s2.Append(ctx, strings.NewReader("cycle two partial")); err != nil {
		t.Fatal(err)
	}
	if err := eng2.Close(); err != nil {
		t.Fatalf("Close cycle 2: %v", err)
	}

	// Both cycles' preserved state is intact after the loop.
	if n := countSessionDirs(t, root); n != 2 {
		t.Fatalf("session dirs after two Open-Close cycles = %d, want 2", n)
	}
	if n := store.countRows(); n != 2 {
		t.Fatalf("rows after two Open-Close cycles = %d, want 2", n)
	}
	preservedData(t, root, id1, []byte("cycle one partial"))
	preservedData(t, root, id2, []byte("cycle two partial"))

	// A third open sees both unexpired rows and keeps both (then closes
	// cleanly again — the loop is repeatable).
	eng3, err := OpenEngine(root, Options{Sessions: store})
	if err != nil {
		t.Fatal(err)
	}
	if n := countSessionDirs(t, root); n != 2 {
		t.Fatalf("session dirs after third open = %d, want 2 (unexpired, kept)", n)
	}
	if err := eng3.Close(); err != nil {
		t.Fatalf("Close cycle 3: %v", err)
	}
}

// TestCloseRetentionLog captures the ADR-0028 retention INFO: preserved
// count + id list, truncated past the cap, silent when nothing is preserved.
func TestCloseRetentionLog(t *testing.T) {
	t.Run("INFO lists preserved ids", func(t *testing.T) {
		root := t.TempDir()
		var buf syncBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		eng, err := OpenEngine(root, Options{Logger: log})
		if err != nil {
			t.Fatal(err)
		}
		s, err := eng.BeginSession(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		id := s.ID()
		if err := eng.Close(); err != nil {
			t.Fatal(err)
		}
		line := buf.String()
		if !strings.Contains(line, "level=INFO") {
			t.Fatalf("retention log level not INFO: %q", line)
		}
		if !strings.Contains(line, "preserving unexpired upload sessions") {
			t.Fatalf("retention log missing the preservation message: %q", line)
		}
		if !strings.Contains(line, "count=1") {
			t.Fatalf("retention log missing count=1: %q", line)
		}
		if !strings.Contains(line, id) {
			t.Fatalf("retention log missing session id %s: %q", id, line)
		}
	})
	t.Run("list truncates past the cap", func(t *testing.T) {
		root := t.TempDir()
		var buf syncBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		eng, err := OpenEngine(root, Options{Logger: log})
		if err != nil {
			t.Fatal(err)
		}
		ids := make(map[string]bool, maxPreservedSessionIDs+5)
		for i := 0; i < maxPreservedSessionIDs+5; i++ {
			s, err := eng.BeginSession(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ids[s.ID()] = true
		}
		if err := eng.Close(); err != nil {
			t.Fatal(err)
		}
		line := buf.String()
		if !strings.Contains(line, fmt.Sprintf("count=%d", maxPreservedSessionIDs+5)) {
			t.Fatalf("retention log count wrong: %q", line)
		}
		if !strings.Contains(line, "truncated=5") {
			t.Fatalf("retention log missing truncation marker: %q", line)
		}
	})
	t.Run("silent when nothing is preserved", func(t *testing.T) {
		root := t.TempDir()
		var buf syncBuffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		eng, err := OpenEngine(root, Options{Logger: log})
		if err != nil {
			t.Fatal(err)
		}
		if err := eng.Close(); err != nil {
			t.Fatal(err)
		}
		if line := buf.String(); line != "" {
			t.Fatalf("Close with empty registry logged %q, want silence", line)
		}
	})
}

// syncBuffer is a mutex-guarded bytes.Buffer (slog handlers may write from
// any goroutine; the race detector must see a synchronized buffer).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
