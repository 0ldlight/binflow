package webhook

// The delivery engine (M13 T-364, FR-115 / ADR-0041 decision 4): the
// worker pool that drains the webhook_deliveries outbox.
//
// Form (the replication Engine's queue pattern replicated, not shared —
// ADR-0041's axis-3 ruling): DB rows are the queue, a wake signal plus a
// poll ticker drive the drain, a startup sweep recovers crash residue,
// and a canceled context is the graceful-shutdown drain. What differs
// from replication is the retry physics, and those are the T-358 anchor's
// wire semantics, which flipped the ADR's interim constants through the
// ADR's own "锚点回填位" clause (mechanism clauses stand, wire parameters
// follow the spec):
//
//   - retryCount 5, the FIRST TRY COUNTS (webhook.md 5.2) — five total
//     attempts = initial + four retries;
//   - retryWaitMillis 10000 as a FIXED interval — the official service has
//     no backoff curve, so every retry waits the same 10s;
//   - the retry CONDITION is "unable to send OR answer >= 500": a 4xx (or
//     a 3xx, which the no-follow posture turns into an unsent delivery)
//     is terminal after its single attempt;
//   - timeoutMillis 30000 bounds one whole attempt (connect + exchange +
//     body read), enforced both on the request context and the client;
//   - the rate-limit shape of webhook.md 5.1 rides along: a 1000/s
//     average / 10000-burst token bucket paces attempt starts, and the
//     50000 maxConcurrentHandlers cap rejects NEW events at the Emit seam
//     (the dispatcher injects the saturation probe into the Bus);
//   - no persistent dead-letter store beyond the outbox row itself
//     (webhook.md 5.3): a dead row is queryable and replayable, and the
//     observable outlet is the troubleshooting ring (in-process,
//     streamMaxLen 10000 with a 30s janitor — the Redis shape's
//     equivalent) plus the metrics family and one WARN line per death.
//
// Worker concurrency is the ADR's engine-side constant (4) — an internal
// knob, not an official wire parameter, so the anchor backfill left it
// standing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
)

// AuditActionDeadLetter is the dispatcher's audit word (ADR-0041 decision
// 10's fifth): one row reaching terminal abandonment. A string literal,
// not an audit-package constant — joining audit.Actions()' picker list is
// the audit owner's one-liner (the T-362/T-346 convention).
const AuditActionDeadLetter = "webhook.dead_letter"

// Engine constants. The Default* names are the anchor's wire parameters
// (webhook.md 5.1/5.2 official defaults); the rest are BinFlow engine
// knobs (ADR-0041 decision 4: internal constants, no config keys — the
// ADR-0040 "zero new configuration" posture, knob-ize on real demand).
const (
	// DefaultRetryCount is webhook.md 5.2's retryCount: the number of
	// tries, FIRST TRY INCLUDED ("The first try count as one").
	DefaultRetryCount = 5
	// DefaultRetryWait is webhook.md 5.2's retryWaitMillis: a fixed wait
	// between tries — no exponential curve exists in the official service.
	DefaultRetryWait = 10 * time.Second
	// DefaultAttemptTimeout is webhook.md 5.2's timeoutMillis: one
	// request's whole budget, connection and body read included.
	DefaultAttemptTimeout = 30 * time.Second
	// DefaultDeliveryWorkers is the ADR's worker-pool width.
	DefaultDeliveryWorkers = 4
	// DefaultRateFrequencyPerSec / DefaultRateBurstSize / DefaultMaxConcurrent
	// are webhook.md 5.1's event.rateLimit defaults: 1000 events/s average,
	// 10000 burst, 50000 concurrent handlers with over-cap NEW events
	// rejected.
	DefaultRateFrequencyPerSec = 1000.0
	DefaultRateBurstSize       = 10000.0
	DefaultMaxConcurrent       = 50000

	// DefaultDispatchPollInterval is the idle poll cadence (the cron
	// fallback half of the wake signal; the queue index keeps the poll
	// cheap). Bound for the FR-115 AC-7 delivery-latency budget.
	DefaultDispatchPollInterval = 200 * time.Millisecond
	// DefaultMetricsRefreshInterval mirrors the engine-owned counters into
	// the Prometheus registry (scrape cadences are slower than this; the
	// atomics are the source of truth between mirrors).
	DefaultMetricsRefreshInterval = time.Second
	// DefaultTroubleshootingCleanupInterval is the record ring's janitor
	// period (webhook.md section 7's cleanupIntervalMillis 30000).
	DefaultTroubleshootingCleanupInterval = 30 * time.Second

	// stateWriteTimeout bounds the detached context a terminal/retry row
	// write uses: shutdown must still land the row state after the engine
	// context is canceled (the replication writeTask posture).
	stateWriteTimeout = 10 * time.Second
	// errorTextLimit truncates last_error payloads (ADR-0041 decision 6:
	// the column carries an error snippet, <= 4KB).
	errorTextLimit = 4096
)

// The webhook metrics family (ADR-0041 decision 10; names per the
// binflow_<subsystem>_<metric> rule). The five families are engine-owned
// gauges-at-mirror-time: the dispatcher's atomics and the Bus's loss
// counter are the source of truth, the registry only ever sees deltas, so
// a scrape can never miss or double-count an event.
const (
	MetricDeliveriesTotal      = "binflow_webhook_deliveries_total"
	MetricRetriesTotal         = "binflow_webhook_retries_total"
	MetricDeadLetterTotal      = "binflow_webhook_dead_letter_total"
	MetricQueueDepth           = "binflow_webhook_queue_depth"
	MetricEnqueueFailuresTotal = "binflow_webhook_enqueue_failures_total"
)

// AuditSink is the consumer-side audit seam (structurally satisfied by
// audit.Logger and audit.BestEffort's recorder). Append failures are
// logged and swallowed — auditing must never fail a delivery transition.
type AuditSink interface {
	Append(ctx context.Context, e audit.Event) error
}

// DispatcherOptions assembles the delivery engine. The zero value beyond
// Bus is the anchor's defaults; every field is a test or assembly seam.
type DispatcherOptions struct {
	// Bus is required — the dispatcher reuses its per-attempt send path
	// (Guard, signing, bounded reads) and injects its wake/saturation
	// seams into it.
	Bus *Bus
	// Now stamps row times (RFC3339Nano UTC). nil = time.Now().UTC().
	Now func() time.Time
	// Logger receives the WARN/INFO lines. nil = slog.Default().
	Logger *slog.Logger
	// Registry, when non-nil, receives the five webhook metric families.
	// Registration errors surface at assembly (an honest startup failure,
	// not a silent metrics gap).
	Registry *metrics.Registry
	// Audit receives the webhook.dead_letter word. nil = no auditing.
	Audit AuditSink

	// RetryCount: total attempts, first try counted (<= 0 = default 5).
	RetryCount int
	// RetryWait: the fixed inter-attempt interval (<= 0 = default 10s).
	RetryWait time.Duration
	// AttemptTimeout: one whole attempt's budget (<= 0 = default 30s).
	AttemptTimeout time.Duration
	// Workers: concurrent delivery workers (<= 0 = default 4).
	Workers int
	// FrequencyPerSec / BurstSize: the token bucket pacing attempt starts
	// (webhook.md 5.1's frequency/burstSize; <= 0 = defaults).
	FrequencyPerSec float64
	BurstSize       float64
	// MaxConcurrent: the concurrent-delivery cap; NEW events are rejected
	// at the Emit seam while the dispatcher holds this many in flight
	// (webhook.md 5.1's maxConcurrentHandlers; <= 0 = default 50000).
	MaxConcurrent int
	// PollInterval: the idle poll cadence (<= 0 = default 200ms).
	PollInterval time.Duration
	// TroubleshootingCleanupInterval: the record ring janitor's period
	// (<= 0 = default 30s).
	TroubleshootingCleanupInterval time.Duration
	// MetricsRefreshInterval: the registry mirror cadence (<= 0 = default
	// 1s). A seam for tests, not a knob: scrape cadences are slower.
	MetricsRefreshInterval time.Duration
}

// Dispatcher is the outbox delivery engine. Build with NewDispatcher, then
// run Run in a dedicated goroutine until the context is canceled (the
// startup-liberty/drain contract of replication.Engine.Run). A Dispatcher
// must not be copied after NewDispatcher.
type Dispatcher struct {
	bus         *Bus
	store       Store
	log         *slog.Logger
	now         func() time.Time
	retryCount  int64
	retryWait   time.Duration
	timeout     time.Duration
	workers     int
	maxConc     int64
	pollEvery   time.Duration
	cleanEvery  time.Duration
	mirrorEvery time.Duration
	limiter     *tokenBucket
	audit       AuditSink

	// Engine-owned counters: the metric family's source of truth (the
	// registry mirror only ever sees deltas off these).
	inflight  atomic.Int64
	delivered atomic.Int64
	retries   atomic.Int64
	dead      atomic.Int64

	// wake coalesces post-enqueue signals; capacity equals the worker
	// count so one signal can engage every parked worker.
	wake chan struct{}

	reg         *metrics.Registry
	mDeliver    *metrics.Counter
	mRetry      *metrics.Counter
	mDead       *metrics.Counter
	mQueue      *metrics.Gauge
	mEnqFailure *metrics.Counter

	// mirror bookkeeping (Run's own goroutine only — no atomics needed).
	lastDelivered, lastRetries, lastDead, lastEnqFailure int64
}

// NewDispatcher assembles the engine, normalizes the anchor's parameters
// onto their defaults and injects the two Emit-seam hooks into the Bus:
// the wake notifier (a successful enqueue signals the pool) and the
// saturation probe (over-cap events are rejected at the seam, webhook.md
// 5.1's maxConcurrentHandlers shape). One dispatcher per bus: a second
// dispatcher's hooks replace the first's.
func NewDispatcher(opts DispatcherOptions) (*Dispatcher, error) {
	if opts.Bus == nil {
		return nil, errors.New("webhook: NewDispatcher: bus is nil")
	}
	if opts.Bus.store == nil {
		return nil, errors.New("webhook: NewDispatcher: bus carries no store")
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	retryCount := int64(opts.RetryCount)
	if retryCount < 1 {
		retryCount = DefaultRetryCount
	}
	retryWait := opts.RetryWait
	if retryWait <= 0 {
		retryWait = DefaultRetryWait
	}
	timeout := opts.AttemptTimeout
	if timeout <= 0 {
		timeout = DefaultAttemptTimeout
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultDeliveryWorkers
	}
	freq := opts.FrequencyPerSec
	if freq <= 0 {
		freq = DefaultRateFrequencyPerSec
	}
	burst := opts.BurstSize
	if burst <= 0 {
		burst = DefaultRateBurstSize
	}
	maxConc := int64(opts.MaxConcurrent)
	if maxConc <= 0 {
		maxConc = DefaultMaxConcurrent
	}
	pollEvery := opts.PollInterval
	if pollEvery <= 0 {
		pollEvery = DefaultDispatchPollInterval
	}
	cleanEvery := opts.TroubleshootingCleanupInterval
	if cleanEvery <= 0 {
		cleanEvery = DefaultTroubleshootingCleanupInterval
	}
	mirrorEvery := opts.MetricsRefreshInterval
	if mirrorEvery <= 0 {
		mirrorEvery = DefaultMetricsRefreshInterval
	}
	d := &Dispatcher{
		bus:         opts.Bus,
		store:       opts.Bus.store,
		log:         log,
		now:         now,
		retryCount:  retryCount,
		retryWait:   retryWait,
		timeout:     timeout,
		workers:     workers,
		maxConc:     maxConc,
		pollEvery:   pollEvery,
		cleanEvery:  cleanEvery,
		mirrorEvery: mirrorEvery,
		limiter:     newTokenBucket(freq, burst, now),
		audit:       opts.Audit,
		wake:        make(chan struct{}, workers),
	}
	if opts.Registry != nil {
		c, err := opts.Registry.NewCounter(MetricDeliveriesTotal,
			"Webhook events successfully delivered (2xx answer) since process start.")
		if err != nil {
			return nil, fmt.Errorf("webhook: metric %s: %w", MetricDeliveriesTotal, err)
		}
		d.mDeliver = c
		c, err = opts.Registry.NewCounter(MetricRetriesTotal,
			"Webhook delivery retries scheduled (attempts after the first, send-failure or >=500 answers) since process start.")
		if err != nil {
			return nil, fmt.Errorf("webhook: metric %s: %w", MetricRetriesTotal, err)
		}
		d.mRetry = c
		c, err = opts.Registry.NewCounter(MetricDeadLetterTotal,
			"Webhook deliveries abandoned to dead status (terminal 3xx/4xx answer or retry budget exhausted) since process start.")
		if err != nil {
			return nil, fmt.Errorf("webhook: metric %s: %w", MetricDeadLetterTotal, err)
		}
		d.mDead = c
		g, err := opts.Registry.NewGauge(MetricQueueDepth,
			"Pending webhook delivery rows in the outbox.")
		if err != nil {
			return nil, fmt.Errorf("webhook: metric %s: %w", MetricQueueDepth, err)
		}
		d.mQueue = g
		d.mQueue.Set(0)
		c, err = opts.Registry.NewCounter(MetricEnqueueFailuresTotal,
			"Webhook events lost at the Emit seam (enqueue failures and concurrency-cap rejections) since process start.")
		if err != nil {
			return nil, fmt.Errorf("webhook: metric %s: %w", MetricEnqueueFailuresTotal, err)
		}
		d.mEnqFailure = c
		d.reg = opts.Registry
	}
	// The Emit-seam hooks (the bus fields are package-private by design:
	// the dispatcher is the only legitimate writer).
	opts.Bus.notify = d.signal
	opts.Bus.saturated = func() bool { return d.inflight.Load() >= d.maxConc }
	return d, nil
}

// signal wakes parked workers; the buffered channel coalesces (capacity
// workers: one enqueue can engage the whole pool, a burst cannot pile up
// signals beyond that).
func (d *Dispatcher) signal() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Run is the engine's lifecycle: startup sweep + watermark line, the
// worker pool, the metrics mirror and the record-ring janitor. It returns
// on context cancellation AFTER every worker has parked: in-flight
// attempts abort at their next context boundary and their outcome writes
// land on a detached context, so no row is ever left delivering by a clean
// shutdown (a kill -9 is the startup sweep's problem instead).
func (d *Dispatcher) Run(ctx context.Context) error {
	swept, err := d.store.SweepDelivering(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "webhook: startup sweep failed", "error", err.Error())
		swept = -1
	}
	pending, err := d.store.CountPending(ctx)
	if err != nil {
		d.log.WarnContext(ctx, "webhook: startup watermark count failed", "error", err.Error())
	}
	// The one-line startup watermark (the ADR-0040 outbox-watermark form).
	d.log.InfoContext(ctx, "webhook: delivery dispatcher started",
		"outbox_pending", pending, "swept_delivering", swept,
		"workers", d.workers, "retry_count", d.retryCount,
		"retry_wait", d.retryWait.String(), "attempt_timeout", d.timeout.String())

	var wg sync.WaitGroup
	for i := 0; i < d.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.work(ctx)
		}()
	}
	d.refreshMetrics(ctx)
	metricsTick := time.NewTicker(d.mirrorEvery)
	defer metricsTick.Stop()
	cleanupTick := time.NewTicker(d.cleanEvery)
	defer cleanupTick.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil
		case <-metricsTick.C:
			d.refreshMetrics(ctx)
		case <-cleanupTick.C:
			d.bus.trimTroubleshooting()
		}
	}
}

// work is one delivery worker: claim a due row, deliver it, repeat. An
// empty claim parks on the wake signal or the poll timer — the cron-style
// fallback that also recovers rows a previous process scheduled.
func (d *Dispatcher) work(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		row, err := d.store.ClaimDueDelivery(ctx, d.now().Format(time.RFC3339Nano))
		if err != nil {
			// Busy-class contention is expected under the four-way pool
			// plus the emit path; anything else deserves a WARN.
			if metadata.IsStoreBusy(err) {
				d.log.DebugContext(ctx, "webhook: claim busy", "error", err.Error())
			} else {
				d.log.WarnContext(ctx, "webhook: claim failed", "error", err.Error())
			}
			if !sleepCtx(ctx, d.pollEvery) {
				return
			}
			continue
		}
		if row == nil {
			select {
			case <-ctx.Done():
				return
			case <-d.wake:
			case <-time.After(d.pollEvery):
			}
			continue
		}
		d.deliver(ctx, row)
	}
}

// deliver drives one claimed row through one attempt and lands the
// outcome. The claim already counted the attempt (attempts+1 in the CAS),
// so the branch here is purely the anchor's classification:
//
//   - 2xx: delivered;
//   - send failure (no answer: transport, guard refusal, timeout) or
//     >=500: retry after the fixed wait, or dead at the budget's end;
//   - anything else (3xx never followed, 4xx never retried): dead now.
func (d *Dispatcher) deliver(ctx context.Context, row *Delivery) {
	d.inflight.Add(1)
	defer d.inflight.Add(-1)

	sub, err := d.store.GetSubscriptionByID(ctx, row.SubscriptionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// The subscription is gone; the cascade already removed the
			// row (or is about to). Nothing to deliver, nothing to write.
			return
		}
		// A transient read must not strand the row in delivering: hand it
		// back to pending at the fixed interval (the anchor's cadence
		// applies to every retryable failure, lookup included).
		wctx, cancel := detachedWrite()
		defer cancel()
		d.writeRetry(wctx, row, truncateErr(fmt.Sprintf("target lookup failed: %v", err)), nil)
		return
	}

	// The secret is read at delivery time (ADR-0041 decision 3's rotation
	// clause: a rotated secret applies to in-flight rows immediately).
	secret := ""
	if sub.Handler.HasSecret() && d.bus.cipher != nil {
		if plain, _, derr := d.bus.cipher.Decrypt(sub.Handler.SecretEnc); derr == nil {
			secret = plain
		}
	}

	payload := []byte(row.Payload)
	if d.limiter != nil {
		if err := d.limiter.acquire(ctx); err != nil {
			// Shutdown while waiting for a token: hand the row back.
			wctx, cancel := detachedWrite()
			defer cancel()
			d.writeRetry(wctx, row, "", nil)
			return
		}
	}

	attemptCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	res := d.bus.sendAttempt(attemptCtx, &sub.Handler, payload, secret)
	d.bus.recordAttempt(&sub.Handler, payload, res, int(row.Attempts-1), sub.Debug)

	wctx, wcancel := detachedWrite()
	defer wcancel()
	code := int64(res.StatusCode)
	var codePtr *int64
	if res.StatusCode != 0 {
		codePtr = &code
	}
	switch {
	case res.Error == "" && res.StatusCode >= 200 && res.StatusCode < 300:
		if err := d.store.MarkDelivered(wctx, row.ID, row.Attempts,
			d.now().Format(time.RFC3339Nano), code); err != nil {
			d.log.WarnContext(wctx, "webhook: delivered-state write failed",
				"delivery", row.ID, "error", err.Error())
			return
		}
		d.delivered.Add(1)
	case res.StatusCode >= 500 || res.StatusCode == 0:
		if row.Attempts >= d.retryCount {
			d.deadLetter(wctx, row, sub, res, codePtr)
		} else {
			d.writeRetry(wctx, row, truncateErr(res.Error), codePtr)
		}
	default:
		// 3xx (never followed) and 4xx (never retried): terminal.
		d.deadLetter(wctx, row, sub, res, codePtr)
	}
}

// writeRetry schedules the row's next attempt at the fixed interval. A
// write failure is logged, never propagated: the row stays delivering and
// the next process start's sweep recovers it (the outbox is the durable
// state, the worker is not).
func (d *Dispatcher) writeRetry(ctx context.Context, row *Delivery, lastError string, statusCode *int64) {
	next := d.now().Add(d.retryWait).Format(time.RFC3339Nano)
	if err := d.store.ScheduleRetry(ctx, row.ID, row.Attempts, next, lastError, statusCode); err != nil {
		d.log.WarnContext(ctx, "webhook: retry-state write failed",
			"delivery", row.ID, "error", err.Error())
		return
	}
	d.retries.Add(1)
}

// deadLetter lands the terminal abandonment: row state, counter, the one
// WARN line the ADR asks for, and the best-effort audit word.
func (d *Dispatcher) deadLetter(ctx context.Context, row *Delivery, sub *Subscription, res *attemptResult, statusCode *int64) {
	msg := truncateErr(res.Error)
	if err := d.store.MarkDead(ctx, row.ID, row.Attempts, msg, statusCode); err != nil {
		d.log.WarnContext(ctx, "webhook: dead-state write failed",
			"delivery", row.ID, "error", err.Error())
		return
	}
	d.dead.Add(1)
	d.log.Warn("webhook: delivery dead-lettered",
		"subscription", sub.Key, "event_type", row.EventType,
		"attempts", row.Attempts, "status_code", res.StatusCode,
		"url", sub.Handler.URL, "error", msg)
	d.auditDeadLetter(ctx, row, sub, res)
}

// auditDeadLetter appends the webhook.dead_letter word (best-effort; the
// detail carries the subscription/event identity and the SANITIZED error
// — url.Error strings were already stripped of userinfo by the send path,
// and stored URLs never carry userinfo by validation).
func (d *Dispatcher) auditDeadLetter(ctx context.Context, row *Delivery, sub *Subscription, res *attemptResult) {
	if d.audit == nil {
		return
	}
	detail, err := json.Marshal(map[string]any{
		"subscription": sub.Key,
		"event_type":   row.EventType,
		"attempts":     row.Attempts,
		"status_code":  res.StatusCode,
		"url":          sub.Handler.URL,
		"error":        truncateErr(res.Error),
	})
	if err != nil {
		detail = []byte(`{"error":"detail marshal failed"}`)
	}
	ev := audit.Event{
		Actor:  "webhook",
		Action: AuditActionDeadLetter,
		Detail: string(detail),
	}
	if aerr := d.audit.Append(ctx, ev); aerr != nil {
		d.log.WarnContext(ctx, "webhook: dead-letter audit append failed", "error", aerr.Error())
	}
}

// Replay resets one dead row to pending (attempts zeroed, due now) and
// wakes the pool — the ADR decision 4 replay face behind whatever REST or
// console surface the assembly mounts on it.
func (d *Dispatcher) Replay(ctx context.Context, deliveryID string) error {
	if err := d.store.ReplayDelivery(ctx, deliveryID, d.now().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	d.signal()
	return nil
}

// DispatcherStats is the engine-owned counter snapshot (the metrics
// family's source of truth, readable without a registry).
type DispatcherStats struct {
	Delivered       int64
	Retries         int64
	DeadLettered    int64
	InFlight        int64
	EnqueueFailures int64
}

// Stats snapshots the engine counters (plus the Bus's loss counter).
func (d *Dispatcher) Stats() DispatcherStats {
	return DispatcherStats{
		Delivered:       d.delivered.Load(),
		Retries:         d.retries.Load(),
		DeadLettered:    d.dead.Load(),
		InFlight:        d.inflight.Load(),
		EnqueueFailures: d.bus.EnqueueFailures(),
	}
}

// refreshMetrics mirrors the engine counters into the registry and reads
// the outbox watermark into the queue-depth gauge. Deltas only (the
// atomics stay the single source of truth); a failed count keeps the
// previous value at Debug — a store hiccup must not fail a mirror pass.
func (d *Dispatcher) refreshMetrics(ctx context.Context) {
	if d.reg == nil {
		return
	}
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if n, err := d.store.CountPending(mctx); err != nil {
		d.log.DebugContext(mctx, "webhook: queue-depth snapshot failed", "error", err.Error())
	} else {
		d.mQueue.Set(float64(n))
	}
	if v := d.delivered.Load(); v != d.lastDelivered {
		d.mDeliver.Add(float64(v - d.lastDelivered))
		d.lastDelivered = v
	}
	if v := d.retries.Load(); v != d.lastRetries {
		d.mRetry.Add(float64(v - d.lastRetries))
		d.lastRetries = v
	}
	if v := d.dead.Load(); v != d.lastDead {
		d.mDead.Add(float64(v - d.lastDead))
		d.lastDead = v
	}
	if v := d.bus.EnqueueFailures(); v != d.lastEnqFailure {
		d.mEnqFailure.Add(float64(v - d.lastEnqFailure))
		d.lastEnqFailure = v
	}
}

// detachedWrite builds the outcome-write context: detached from the
// engine's shutdown signal but bounded, so terminal and retry writes land
// after cancellation without wedging on a closed store.
func detachedWrite() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(context.Background()), stateWriteTimeout)
}

// sleepCtx parks for d or until ctx ends; false means the context is done.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// truncateErr bounds an error text for the last_error column.
func truncateErr(msg string) string {
	if len(msg) > errorTextLimit {
		return msg[:errorTextLimit]
	}
	return msg
}

// tokenBucket is webhook.md 5.1's frequency/burstSize shape: a bucket of
// burstSize tokens refilled at frequency tokens per second. Acquiring a
// token never fails an event — after the burst is spent, attempt starts
// simply pace at the frequency rate ("到阈值后按 frequency 匀速").
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	now      func() time.Time
	last     time.Time
}

func newTokenBucket(rate, capacity float64, now func() time.Time) *tokenBucket {
	return &tokenBucket{
		tokens:   capacity, // the process starts with the full burst budget
		capacity: capacity,
		rate:     rate,
		now:      now,
		last:     now(),
	}
}

// refill folds elapsed time into the bucket (caller holds the lock).
func (b *tokenBucket) refill() {
	now := b.now()
	if dt := now.Sub(b.last); dt > 0 {
		b.tokens = min(b.capacity, b.tokens+dt.Seconds()*b.rate)
		b.last = now
	}
}

// acquire consumes one token, waiting for the refill when the bucket is
// empty. It fails only on context cancellation (shutdown).
func (b *tokenBucket) acquire(ctx context.Context) error {
	for {
		b.mu.Lock()
		b.refill()
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		deficit := 1 - b.tokens
		wait := time.Duration(deficit / b.rate * float64(time.Second))
		b.mu.Unlock()
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// rateLimited reports whether the limiter is engaged (test/assembly
// introspection; the shape is always on with the anchor's defaults).
func (d *Dispatcher) rateLimited() bool { return d.limiter != nil }
