package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEngineCloseBehaviorMatrix pins the post-Close contract for every
// Engine method (review blocker-1): mutating operations fail with
// ErrEngineClosed, read paths keep serving committed blobs.
func TestEngineCloseBehaviorMatrix(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ref := put(t, eng, []byte("committed before shutdown"))
	// An unreferenced old blob so GC has a candidate to act on.
	orphan := put(t, eng, []byte("orphan before shutdown"))
	backdateBlob(t, root, orphan.Sha256, 2*DefaultGCGrace)

	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	t.Run("BeginSession refused", func(t *testing.T) {
		if _, err := eng.BeginSession(context.Background()); !errors.Is(err, ErrEngineClosed) {
			t.Fatalf("err = %v, want ErrEngineClosed", err)
		}
	})
	t.Run("Delete refused", func(t *testing.T) {
		if err := eng.Delete(context.Background(), ref.Sha256); !errors.Is(err, ErrEngineClosed) {
			t.Fatalf("err = %v, want ErrEngineClosed", err)
		}
	})
	t.Run("GC refused", func(t *testing.T) {
		if _, err := eng.GC(context.Background(), refsSet(), DefaultGCGrace, false); !errors.Is(err, ErrEngineClosed) {
			t.Fatalf("err = %v, want ErrEngineClosed", err)
		}
	})
	t.Run("Open still serves", func(t *testing.T) {
		f, got, err := eng.Open(context.Background(), ref.Sha256)
		if err != nil {
			t.Fatalf("Open after Close: %v", err)
		}
		defer f.Close() //nolint:errcheck // test
		body, err := io.ReadAll(f)
		if err != nil || string(body) != "committed before shutdown" || got.Size != int64(len("committed before shutdown")) {
			t.Fatalf("read after Close = %q (%v) size %d", body, err, got.Size)
		}
	})
	t.Run("Stat still verifies", func(t *testing.T) {
		if _, err := eng.Stat(context.Background(), ref.Sha256); err != nil {
			t.Fatalf("Stat after Close: %v", err)
		}
	})
	t.Run("Close idempotent", func(t *testing.T) {
		if err := eng.Close(); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})
}

// errReader yields its bytes, then fails. It simulates a request body that
// dies mid-upload (connection reset) — the most common real Append failure.
type errReader struct {
	data    []byte
	offset  int
	failure error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	return 0, r.failure
}

// TestAppendFailurePoisonsSession is the ENOSPC-class regression test
// (review major-2): after a failed Append the session must never be
// reusable — a reuse would rename a file whose content no longer matches
// its running digest into the blob store.
func TestAppendFailurePoisonsSession(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()

	bodyErr := errors.New("simulated connection reset")
	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// First Append succeeds.
	if _, err := s.Append(ctx, strings.NewReader("healthy prefix; ")); err != nil {
		t.Fatal(err)
	}
	// Second Append reads fine, then dies partway.
	if _, err := s.Append(ctx, &errReader{data: []byte("then the body breaks"), failure: bodyErr}); !errors.Is(err, bodyErr) {
		t.Fatalf("failing Append err = %v, want bodyErr", err)
	}

	// Every subsequent operation must refuse with ErrSessionPoisoned.
	if _, err := s.Append(ctx, strings.NewReader("retry")); !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Append after failure err = %v, want ErrSessionPoisoned", err)
	}
	if _, err := s.Commit(ctx, BlobRef{}); !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Commit after failure err = %v, want ErrSessionPoisoned", err)
	}
	// Nothing entered the blob store.
	if n := countBlobs(t, root); n != 0 {
		t.Fatalf("blobs after poisoned commit attempt = %d, want 0", n)
	}
	// Abort still works and leaves zero residue.
	if err := s.Abort(ctx); err != nil {
		t.Fatalf("Abort poisoned session: %v", err)
	}
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after abort = %d, want 0", n)
	}
}

// TestAppendCtxCancelPoisonsSession: a cancelled context mid-stream is the
// same hazard class as a reader error — the session must not survive it.
func TestAppendCtxCancelPoisonsSession(t *testing.T) {
	eng := newEngine(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Reader that cancels the context mid-stream, then keeps yielding data:
	// copyWithCtx must observe cancellation and stop.
	cancelReader := &cancelOnceReader{data: bytes.Repeat([]byte("x"), 1<<20), cancel: cancel}
	if _, err := s.Append(ctx, cancelReader); !errors.Is(err, context.Canceled) {
		t.Fatalf("Append err = %v, want context.Canceled", err)
	}
	if _, err := s.Commit(context.Background(), BlobRef{}); !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Commit err = %v, want ErrSessionPoisoned", err)
	}
}

type cancelOnceReader struct {
	data   []byte
	offset int
	cancel context.CancelFunc
	called bool
}

func (r *cancelOnceReader) Read(p []byte) (int, error) {
	if !r.called {
		r.called = true
		r.cancel()
	}
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:r.offset+4096])
	r.offset += n
	return n, nil
}

// TestPoisonedSessionOnEngineClose: Close reclaims a poisoned session like
// any other; the poison flag never blocks cleanup paths.
func TestPoisonedSessionOnEngineClose(t *testing.T) {
	root := t.TempDir()
	eng, err := OpenEngine(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := eng.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), &errReader{data: []byte("x"), failure: errors.New("boom")}); err == nil {
		t.Fatal("expected Append failure")
	}
	if err := eng.Close(); err != nil {
		t.Fatalf("Close with poisoned session: %v", err)
	}
	if n := countSessionDirs(t, root); n != 0 {
		t.Fatalf("session dirs after Close = %d, want 0", n)
	}
}

// TestGCBoundaryExactGrace pins the sweep comparison as inclusive at the
// boundary: age <= grace keeps the blob, age just past grace collects it
// (review minor-4; a refactor to `<` must fail this test). The at-edge
// fixture is backdated slightly less than grace so filesystem mtime
// granularity cannot flip the comparison.
func TestGCBoundaryExactGrace(t *testing.T) {
	root := t.TempDir()
	eng := newEngineAt(t, root, Options{})
	ctx := context.Background()
	atEdge := put(t, eng, []byte("just inside grace"))
	justPast := put(t, eng, []byte("just past grace"))
	backdateBlob(t, root, atEdge.Sha256, time.Hour-50*time.Millisecond)
	backdateBlob(t, root, justPast.Sha256, time.Hour+time.Minute)

	got, err := eng.GC(ctx, refsSet(), time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != justPast.Sha256 {
		t.Fatalf("dry-run = %v, want only [%s] (age<=grace must be kept)", got, justPast.Sha256)
	}
}

// TestSweepBoundaryExactTTL pins the startup sweep the same way: age <= ttl
// keeps the session, age just past ttl removes it. The fixture uses a 1s
// margin (not the raw boundary) because mkStaleSession and the sweep each
// take their own time.Now() snapshot; the comparison operator itself is
// what this test protects.
func TestSweepBoundaryExactTTL(t *testing.T) {
	root := t.TempDir()
	mkStaleSession(t, root, "inside-ttl", time.Hour-time.Second)
	mkStaleSession(t, root, "past-ttl", time.Hour+time.Minute)
	eng, err := OpenEngine(root, Options{SessionTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close() //nolint:errcheck // test
	entries, err := os.ReadDir(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 1 || left[0] != "inside-ttl" {
		t.Fatalf("sessions left = %v, want [inside-ttl] (age<ttl must be kept)", left)
	}
}
