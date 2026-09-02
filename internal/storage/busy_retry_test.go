package storage

// T-423 (FR-139.2, T-377 D1): the busy retry budget for metadata-backed
// write bookkeeping. These tests pin the helper's contract — retry only
// busy-class errors, stop on cancellation, keep the retryable sentinel on
// exhaustion — and then the two wired sites: a BeginSession whose row
// create starves and an Append whose state persist starves both land
// their blob anyway.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// busyErr builds a store-shaped busy-class error (the wrapExec product:
// ErrStoreBusy wrapped around the driver text). The classifier contract is
// errors.Is-based, so tests construct the sentinel chain directly instead
// of manufacturing real driver contention.
func busyErr(label string) error {
	return fmt.Errorf("metadata: %s: %w: database is locked (5) (SQLITE_BUSY)",
		label, metadata.ErrStoreBusy)
}

// flakySessionStore fails the first N creates/set-states with busy-class
// errors, then delegates to the real store.
type flakySessionStore struct {
	metadata.UploadSessionStore
	createFails atomic.Int64
	createCalls atomic.Int64
	setFails    atomic.Int64
	setCalls    atomic.Int64
}

func (f *flakySessionStore) Create(ctx context.Context, u *metadata.UploadSession) error {
	f.createCalls.Add(1)
	if f.createFails.Add(-1) >= 0 {
		return busyErr("upload-sessions create")
	}
	return f.UploadSessionStore.Create(ctx, u)
}

func (f *flakySessionStore) SetState(ctx context.Context, id, state string) error {
	f.setCalls.Add(1)
	if f.setFails.Add(-1) >= 0 {
		return busyErr("upload-sessions set-state")
	}
	return f.UploadSessionStore.SetState(ctx, id, state)
}

// fastBusyRetry is the tests' budget: same ladder semantics, no waiting.
func fastBusyRetry() BusyRetryPolicy {
	return BusyRetryPolicy{Attempts: 5, Backoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond}
}

func TestRetryOnBusySucceedsWithinBudget(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int64
	err := RetryOnBusy(ctx, nil, fastBusyRetry(), "op", func(context.Context) error {
		if calls.Add(1) <= 2 {
			return busyErr("op")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RetryOnBusy: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("executions = %d, want 3 (two busy, one clean)", got)
	}
}

func TestRetryOnBusyReturnsNonBusyErrorAfterOneExecution(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("disk exploded")
	var calls atomic.Int64
	err := RetryOnBusy(ctx, nil, fastBusyRetry(), "op", func(context.Context) error {
		calls.Add(1)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the original non-busy error", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("executions = %d, want 1 — permanent failures are never retried", got)
	}
}

func TestRetryOnBusyExhaustsBudgetKeepingRetryableSentinel(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int64
	pol := BusyRetryPolicy{Attempts: 3, Backoff: time.Millisecond, MaxBackoff: time.Millisecond}
	err := RetryOnBusy(ctx, nil, pol, "op", func(context.Context) error {
		calls.Add(1)
		return busyErr("op")
	})
	if !metadata.IsStoreBusy(err) {
		t.Fatalf("exhausted error lost the retryable sentinel: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("executions = %d, want 3 (the whole budget)", got)
	}
}

func TestRetryOnBusyStopsRetryingForGoneCaller(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int64
	err := RetryOnBusy(ctx, nil, fastBusyRetry(), "op", func(context.Context) error {
		calls.Add(1)
		return busyErr("op")
	})
	if !metadata.IsStoreBusy(err) {
		t.Fatalf("err = %v, want the busy verdict for the gone caller", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("executions = %d, want 1 — a cancelled context stops the ladder", got)
	}
}

func TestRetryOnBusyWakesFromBackoffWhenContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int64
	pol := BusyRetryPolicy{Attempts: 1 << 20, Backoff: time.Minute, MaxBackoff: time.Minute}
	done := make(chan error, 1)
	go func() {
		done <- RetryOnBusy(ctx, nil, pol, "op", func(context.Context) error {
			calls.Add(1)
			return busyErr("op")
		})
	}()
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !metadata.IsStoreBusy(err) {
			t.Fatalf("err = %v, want the busy verdict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RetryOnBusy did not wake from the backoff on cancellation")
	}
	if got := calls.Load(); got > 2 {
		t.Fatalf("executions = %d, want at most 2 — cancellation must not keep paying", got)
	}
}

// TestDefaultBusyRetryBudgetIsTheRegisteredDecision pins the production
// budget T-423 registered against T-377 D1's 0.27%-per-attempt escape rate:
// four total executions and a sub-second backoff ladder.
func TestDefaultBusyRetryBudgetIsTheRegisteredDecision(t *testing.T) {
	if DefaultBusyRetry.Attempts != 4 {
		t.Fatalf("Attempts = %d, want 4", DefaultBusyRetry.Attempts)
	}
	if DefaultBusyRetry.Backoff != 250*time.Millisecond {
		t.Fatalf("Backoff = %v, want 250ms", DefaultBusyRetry.Backoff)
	}
	if DefaultBusyRetry.MaxBackoff != time.Second {
		t.Fatalf("MaxBackoff = %v, want 1s", DefaultBusyRetry.MaxBackoff)
	}
	// The zero value resolves to the same budget.
	var zero BusyRetryPolicy
	if got := zero.resolved(); got != DefaultBusyRetry {
		t.Fatalf("zero policy resolved to %+v, want the default", got)
	}
}

// TestBeginSessionSurvivesBusyRowCreate reproduces T-377 D1's exact site —
// the upload_sessions row create starving past busy_timeout — and pins the
// budgeted outcome: the begin succeeds, the retry inserted the SAME row,
// and the session is fully usable.
func TestBeginSessionSurvivesBusyRowCreate(t *testing.T) {
	ctx := context.Background()
	store, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer store.Close() //nolint:errcheck // test cleanup
	flaky := &flakySessionStore{UploadSessionStore: store.UploadSessions()}
	flaky.createFails.Store(2)

	st, err := OpenEngine(t.TempDir(), Options{Sessions: flaky, BusyRetry: fastBusyRetry()})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // test cleanup

	sess, err := st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession under busy row create: %v", err)
	}
	if got := flaky.createCalls.Load(); got != 3 {
		t.Fatalf("row create executions = %d, want 3 (two busy, one clean)", got)
	}
	if _, err := sess.Append(ctx, strings.NewReader("payload")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	body, _, err := st.Open(ctx, ref.Sha256)
	if err != nil {
		t.Fatalf("Open landed blob: %v", err)
	}
	defer body.Close() //nolint:errcheck // test cleanup
	got, _ := io.ReadAll(body)
	if string(got) != "payload" {
		t.Fatalf("landed bytes = %q, want %q", got, "payload")
	}
}

// TestAppendSurvivesBusySessionStateWrite pins the second wired site: the
// received-counter SetState starving must not poison a session whose data
// bytes are already durable — the budgeted retry completes the bookkeeping
// and the commit lands.
func TestAppendSurvivesBusySessionStateWrite(t *testing.T) {
	ctx := context.Background()
	store, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer store.Close() //nolint:errcheck // test cleanup
	flaky := &flakySessionStore{UploadSessionStore: store.UploadSessions()}
	flaky.setFails.Store(1)

	st, err := OpenEngine(t.TempDir(), Options{Sessions: flaky, BusyRetry: fastBusyRetry()})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // test cleanup

	sess, err := st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader("durable-bytes")); err != nil {
		t.Fatalf("Append under busy state persist: %v", err)
	}
	if got := flaky.setCalls.Load(); got != 2 {
		t.Fatalf("state persist executions = %d, want 2 (one busy, one clean)", got)
	}
	ref, err := sess.Commit(ctx, BlobRef{})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if ref.Size != int64(len("durable-bytes")) {
		t.Fatalf("ref size = %d, want %d", ref.Size, len("durable-bytes"))
	}
}

// TestBeginSessionReportsBusyWhenBudgetExhausts keeps the honest tail: a
// store that stays busy past the whole budget surfaces the retryable
// sentinel (upper layers answer 503-retry, never a silent success).
func TestBeginSessionReportsBusyWhenBudgetExhausts(t *testing.T) {
	ctx := context.Background()
	store, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer store.Close() //nolint:errcheck // test cleanup
	flaky := &flakySessionStore{UploadSessionStore: store.UploadSessions()}
	flaky.createFails.Store(1 << 30)

	pol := BusyRetryPolicy{Attempts: 3, Backoff: time.Millisecond, MaxBackoff: time.Millisecond}
	st, err := OpenEngine(t.TempDir(), Options{Sessions: flaky, BusyRetry: pol})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // test cleanup

	_, err = st.BeginSession(ctx)
	if !metadata.IsStoreBusy(err) {
		t.Fatalf("err = %v, want the busy sentinel past the exhausted budget", err)
	}
	if got := flaky.createCalls.Load(); got != 3 {
		t.Fatalf("row create executions = %d, want 3 (the whole budget)", got)
	}
}
