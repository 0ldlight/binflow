// N3 pin (T-220, iteration-386 debt ledger): the narrow window between an
// Append reaching EOF (all bytes on disk) and the session-state bookkeeping
// write. A cancellation landing in that window — a client disconnecting right
// after the last byte — must neither poison the session nor drop the SetState:
// the data file is complete, so the row must catch up and the session must
// stay committable. The fix detaches the bookkeeping write from the caller's
// context (context.WithoutCancel in Append); this test turns the fix red the
// moment someone hands the raw request ctx back to persistStateLocked.

package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// cancelAtEOFReader serves data verbatim, then cancels cancel exactly when it
// reports io.EOF — the reader-shaped model of "the client sent the whole body
// and dropped the connection".
type cancelAtEOFReader struct {
	data   []byte
	off    int
	cancel context.CancelFunc
}

func (r *cancelAtEOFReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		r.cancel()
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

// ctxAwareSessions wraps memUploadSessions so SetState honors context
// cancellation the way the real sqlite/postgres drivers do (they abort on a
// cancelled ctx before touching the database). The mem double ignores ctx,
// which would mask exactly the regression this test pins.
type ctxAwareSessions struct {
	*memUploadSessions
	cancelledSetState atomic.Bool
}

func (c *ctxAwareSessions) SetState(ctx context.Context, id, state string) error {
	if err := ctx.Err(); err != nil {
		c.cancelledSetState.Store(true)
		return err
	}
	return c.memUploadSessions.SetState(ctx, id, state)
}

// TestAppendEOFCancelDoesNotPoison pins the N3 behavior: full-body EOF
// followed by ctx cancellation leaves the session healthy (Append succeeds,
// the state row carries the full offset) and committable.
func TestAppendEOFCancelDoesNotPoison(t *testing.T) {
	store := &ctxAwareSessions{memUploadSessions: newMemUploadSessions()}
	eng := newEngineAt(t, t.TempDir(), Options{Sessions: store})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}

	payload := []byte("n3: cancellation races the bookkeeping write after EOF")
	n, err := s.Append(ctx, &cancelAtEOFReader{data: payload, cancel: cancel})
	if err != nil {
		t.Fatalf("Append with cancel-at-EOF reader: %v, want nil (EOF beat the cancellation)", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("Append wrote %d bytes, want %d", n, len(payload))
	}

	// SetState must have landed with the full offset despite the cancellation.
	row, err := store.Get(context.Background(), s.ID())
	if err != nil {
		t.Fatalf("Get session row: %v", err)
	}
	var st sessionState
	if err := json.Unmarshal([]byte(row.State), &st); err != nil {
		t.Fatalf("unmarshal state %q: %v", row.State, err)
	}
	if st.Received != int64(len(payload)) {
		t.Fatalf("row state received = %d, want %d (bookkeeping must catch up to the data file)", st.Received, len(payload))
	}

	// Not poisoned: the same session still commits cleanly over a live ctx.
	// A poisoned session answers Commit with ErrSessionPoisoned instead.
	ref, err := s.Commit(context.Background(), BlobRef{
		Sha256: fmt.Sprintf("%x", sha256.Sum256(payload)),
	})
	if err != nil {
		t.Fatalf("Commit after EOF-cancel Append: %v, want nil (session must not be poisoned)", err)
	}
	if want := fmt.Sprintf("%x", sha256.Sum256(payload)); ref.Sha256 != want {
		t.Fatalf("commit sha256 = %s, want %s", ref.Sha256, want)
	}
}

// TestAppendMidBodyCancelStillPoisons is the fail-closed counterpart: a
// cancellation that lands while the body is still streaming DOES poison the
// session (a partial write diverges the file from the digest chain), so the
// N3 detachment cannot be read as "cancellation never matters".
func TestAppendMidBodyCancelStillPoisons(t *testing.T) {
	store := &ctxAwareSessions{memUploadSessions: newMemUploadSessions()}
	eng := newEngineAt(t, t.TempDir(), Options{Sessions: store})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := eng.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}

	// halfBodyReader cancels after serving the first chunk, with a second
	// chunk still pending — copyWithCtx observes the cancellation while the
	// body is incomplete.
	if _, err := s.Append(ctx, &halfBodyReader{data: []byte("first-chunk"), cancel: cancel}); err == nil {
		t.Fatal("Append over a mid-body cancellation succeeded, want error")
	}

	// The poisoned session refuses a second Append with ErrSessionPoisoned.
	_, err = s.Append(context.Background(), strings.NewReader("more"))
	if !errors.Is(err, ErrSessionPoisoned) {
		t.Fatalf("Append after mid-body cancel: %v, want ErrSessionPoisoned", err)
	}
}

// halfBodyReader serves its first chunk, cancels the context, and then keeps
// offering the remainder — the copy loop must stop on the cancellation
// instead of draining the rest.
type halfBodyReader struct {
	data   []byte
	cancel context.CancelFunc
	served bool
}

func (r *halfBodyReader) Read(p []byte) (int, error) {
	if !r.served {
		r.served = true
		n := copy(p, r.data)
		return n, nil
	}
	r.cancel()
	return 0, nil // no EOF: the body claims to have more to say
}
