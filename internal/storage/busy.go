package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The busy retry budget for metadata-backed write bookkeeping (T-423,
// FR-139.2): T-377's D1 corner — 24-worker cache-tree first pull starved
// 27/10000 upload-session row writes past the metadata store's
// busy_timeout, and the escaping SQLITE_BUSY surfaced as client-visible
// 500s. The store's own budget (metadata.BusyTimeoutMs) is unchanged: a
// waiter already retries inside it, but under a sustained concurrent
// cache-fill storm (WAL checkpoints plus fsync-per-commit epochs) an
// unlucky waiter's window can expire while the write lock keeps changing
// hands. This helper is the SECOND-CHANCE budget ABOVE the store: when a
// busy-class error escapes anyway, the operation re-runs — each attempt
// re-enters a full fresh busy_timeout window, and the staggered backoff
// desynchronizes the herd. Every operation wrapped here is idempotent
// row bookkeeping (upserts, or a self-cleaning failed create), which is
// the hard contract for re-running it.

// BusyRetryPolicy bounds RetryOnBusy.
type BusyRetryPolicy struct {
	// Attempts is the total number of executions of fn, first try
	// included. Values below 1 mean DefaultBusyRetry's.
	Attempts int
	// Backoff is the sleep before the first retry. It doubles per retry
	// up to MaxBackoff, so concurrent waiters stagger instead of
	// re-stamping the herd on one clock tick. Values below 1ms mean
	// DefaultBusyRetry's.
	Backoff time.Duration
	// MaxBackoff caps the doubling. Values below Backoff mean
	// DefaultBusyRetry's.
	MaxBackoff time.Duration
}

// DefaultBusyRetry is the production policy: one initial try plus three
// second chances. The observed D1 escape rate was 0.27% per attempt, so
// the residual failure odds after the budget are negligible while a
// genuinely stuck store still fails within one bounded span (attempts ×
// the store's busy_timeout plus the backoff ladder) instead of hanging.
var DefaultBusyRetry = BusyRetryPolicy{
	Attempts:   4,
	Backoff:    250 * time.Millisecond,
	MaxBackoff: time.Second,
}

// resolved fills the zero-value fields from DefaultBusyRetry.
func (p BusyRetryPolicy) resolved() BusyRetryPolicy {
	if p.Attempts < 1 {
		p.Attempts = DefaultBusyRetry.Attempts
	}
	if p.Backoff < time.Millisecond {
		p.Backoff = DefaultBusyRetry.Backoff
	}
	if p.MaxBackoff < p.Backoff {
		p.MaxBackoff = DefaultBusyRetry.MaxBackoff
	}
	if p.MaxBackoff < p.Backoff {
		p.MaxBackoff = p.Backoff
	}
	return p
}

// RetryOnBusy runs fn and, while it fails with busy-class metadata
// contention (metadata.ErrStoreBusy — SQLITE_BUSY/SQLITE_LOCKED that
// outlived the store's busy_timeout), re-runs it within pol.
//
// Contract and behavior:
//
//   - fn must be safe to re-run: an idempotent row write, or an operation
//     that fully undoes itself on failure (a failed session-row create
//     removes its directory before returning). Every call site in this
//     package and the remote cache-fill path meets that bar.
//   - A non-busy error (and success) returns after exactly one execution —
//     the budget never retries permanent failures.
//   - A cancelled context stops the ladder early by returning the last
//     busy error: the caller that is already gone must not keep paying for
//     retries (a detached-context bookkeeping write — the session state
//     persist — opts out by construction and runs its ladder to the end).
//   - When the budget is exhausted the LAST busy-class error returns,
//     still carrying metadata.ErrStoreBusy, so upper layers keep their
//     retryable-versus-broken classification (T-54) intact.
//
// log (nil = silent) gets one WARN line per retry — the mechanism's
// observability: a cache-fill storm that leans on the budget is visible
// in the server log without any metric surface.
func RetryOnBusy(ctx context.Context, log *slog.Logger, pol BusyRetryPolicy, op string, fn func(context.Context) error) error {
	p := pol.resolved()
	backoff := p.Backoff
	var err error
	for attempt := 1; ; attempt++ {
		if err = fn(ctx); err == nil || !metadata.IsStoreBusy(err) {
			return err
		}
		if attempt >= p.Attempts {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		if log != nil {
			log.Warn("storage: metadata busy — retrying write bookkeeping",
				"op", op, "attempt", attempt+1, "attempts", p.Attempts,
				"backoff_ms", backoff.Milliseconds())
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > p.MaxBackoff {
			backoff = p.MaxBackoff
		}
	}
}
