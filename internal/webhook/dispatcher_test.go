package webhook_test

// T-364's delivery-engine legs, all against REAL HTTP receivers on the
// loopback interface (httptest servers — the same real-client posture as
// T-362's send tests; AllowPrivateTarget is therefore opted in here, the
// SSRF default-refusal arm having been pinned by T-362 already). The
// matrix the ticket demands:
//
//   - delivery + HMAC signature verified from the received bytes;
//   - the retry matrix: 4xx terminal (one attempt), 5xx retried at the
//     FIXED interval until success, attempt timeout = send failure =
//     retryable, exhaustion -> dead;
//   - the rate-limit shape (token-bucket pacing of attempt starts) and
//     the concurrency cap (over-cap NEW events rejected at Emit);
//   - outbox survival across a "process down" window, the startup sweep
//     of kill -9 delivering residue, and graceful shutdown leaving
//     nothing delivering;
//   - replay dead->pending;
//   - the troubleshooting records the dispatcher writes (retries_attempted,
//     redacted auth header) and their query face;
//   - the five Prometheus families off a shared registry;
//   - a concurrent emit/drain leg for -race.

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// ---- harness ----

// fixture is one delivery test's world: the migrated store (with its
// database path, for second-handle legs), the bus, and the one enabled
// artifact/deployed subscription pointing at the receiver. extra is
// spliced into the handler object (secret/debug arms).
type fixture struct {
	store *webhook.SQLiteStore
	bus   *webhook.Bus
	sub   *webhook.Subscription
	path  string
}

func newFixture(t *testing.T, receiverURL, extra string, cipher *remote.Cipher) fixture {
	t.Helper()
	store, md, path := openStore(t)
	if err := md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "maven-local", Type: "local", PackageType: "maven",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store: store, Gate: allowGate(), Repos: md.Repos(),
		Cipher:             cipher,
		AllowPrivateTarget: true, // loopback receivers; the default-refusal arm is T-362's
		Origin:             "https://binflow.example.com",
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	body := `{
		"key": "ci-hook",
		"enabled": true,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiverURL + `"` + extra + `}]
	}`
	var parsed *webhook.SubscriptionRequest
	if cipher != nil {
		parsed = mustParseCipher(t, body, cipher)
	} else {
		parsed = mustParse(t, body)
	}
	sub, err := bus.Create(context.Background(), parsed, "admin")
	if err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	return fixture{store: store, bus: bus, sub: sub, path: path}
}

// startDispatcher runs the engine with test-fast timings and returns the
// stop/drain function. mut may override any option.
func startDispatcher(t *testing.T, bus *webhook.Bus, mut func(*webhook.DispatcherOptions)) (*webhook.Dispatcher, func()) {
	t.Helper()
	opts := webhook.DispatcherOptions{
		Bus:                            bus,
		Logger:                         slog.New(slog.NewTextHandler(io.Discard, nil)),
		RetryWait:                      60 * time.Millisecond,
		AttemptTimeout:                 2 * time.Second,
		PollInterval:                   15 * time.Millisecond,
		Workers:                        4,
		FrequencyPerSec:                100000, // effectively unlimited pacing
		BurstSize:                      100000,
		MaxConcurrent:                  50000,
		TroubleshootingCleanupInterval: time.Hour, // janitor off; capacity legs test it directly
	}
	if mut != nil {
		mut(&opts)
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	d, err := webhook.NewDispatcher(opts)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := d.Run(ctx); err != nil {
			t.Errorf("dispatcher Run: %v", err)
		}
	}()
	return d, func() {
		cancel()
		<-done
	}
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// emitDeployed fires one artifact/deployed event at a distinct path.
func emitDeployed(bus *webhook.Bus, path string) {
	bus.Emit(context.Background(), webhook.Event{
		Domain: webhook.DomainArtifact, Type: webhook.TypeArtifactDeployed,
		Repo: "maven-local", Path: path, Sha256: "deadbeef", Size: 7,
		Actor: webhook.Actor{ID: "ci-bot", Realm: "internal"},
	})
}

// rowOf returns the subscription's single outbox row (failures elsewhere).
func rowOf(t *testing.T, f fixture) *webhook.Delivery {
	t.Helper()
	rows, err := f.store.ListDeliveries(context.Background(), f.sub.ID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("outbox rows = %d (%v), want 1", len(rows), err)
	}
	return rows[0]
}

// rowStatus reports the single row's status without failing on the way.
func rowStatus(f fixture) string {
	rows, err := f.store.ListDeliveries(context.Background(), f.sub.ID, 10)
	if err != nil || len(rows) != 1 {
		return ""
	}
	return rows[0].Status
}

// deliveredCount sums the subscription's delivered rows.
func deliveredCount(f fixture) int {
	rows, err := f.store.ListDeliveries(context.Background(), f.sub.ID, 100)
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range rows {
		if r.Status == webhook.StatusDelivered {
			n++
		}
	}
	return n
}

// recordingHandler captures log lines for the startup-watermark assertion.
type recordingHandler struct {
	mu    sync.Mutex
	lines []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lines = append(h.lines, r.Message)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) has(substr string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

// oneHit captures a receiver request.
type oneHit struct {
	at   time.Time
	body string
	auth string
}

// scriptedReceiver serves the status sequence (last entry repeats) and
// records every hit with its arrival time, body and auth header.
func scriptedReceiver(t *testing.T, statuses ...int) (*httptest.Server, func() []oneHit) {
	t.Helper()
	var mu sync.Mutex
	var hits []oneHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		n := len(hits)
		hits = append(hits, oneHit{at: time.Now(), body: string(body), auth: r.Header.Get(webhook.EventAuthHeader)})
		mu.Unlock()
		status := statuses[len(statuses)-1]
		if n < len(statuses) {
			status = statuses[n]
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []oneHit {
		mu.Lock()
		defer mu.Unlock()
		return append([]oneHit(nil), hits...)
	}
}

// ---- the legs ----

// The AC-4/AC-6 chain: an emitted event is delivered over real HTTP, the
// receiver verifies the HMAC signature off the exact bytes it received,
// the outbox row lands delivered, and the five metric families expose the
// state (queue depth back to zero).
func TestT364DeliversSignedEnvelope(t *testing.T) {
	receiver, hits := scriptedReceiver(t, http.StatusOK)
	cipher, err := remote.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	f := newFixture(t, receiver.URL, `, "secret": "s3cr3t", "use_secret_for_signing": true`, cipher)

	reg := metrics.NewRegistry()
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.Registry = reg
		o.MetricsRefreshInterval = 20 * time.Millisecond
	})
	defer stop()

	emitDeployed(f.bus, "com/acme/app/1.0/app-1.0.jar")
	waitFor(t, 5*time.Second, "delivery", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})

	got := hits()
	if len(got) != 1 {
		t.Fatalf("receiver saw %d requests, want 1", len(got))
	}
	// The signature is verified against the EXACT received bytes — the
	// documented openssl-compatible one-liner's equivalent (webhook.md 6).
	if want := webhook.Signature("s3cr3t", []byte(got[0].body)); got[0].auth != want {
		t.Fatalf("auth header = %q, want HMAC %q", got[0].auth, want)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(got[0].body), &env); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	if env["domain"] != "artifact" || env["event_type"] != "deployed" ||
		env["subscription_key"] != f.sub.Key || env["jpd_origin"] != "https://binflow.example.com" {
		t.Fatalf("envelope identity fields: %v", env)
	}
	if data, _ := env["data"].(map[string]any); data == nil ||
		data["repo_key"] != "maven-local" || data["sha256"] != "deadbeef" || data["size"] != float64(7) {
		t.Fatalf("envelope data: %v", env["data"])
	}
	if uc, _ := env["userContext"].(map[string]any); uc == nil || uc["id"] != "ci-bot" {
		t.Fatalf("userContext: %v", env["userContext"])
	}

	row := rowOf(t, f)
	if row.Attempts != 1 || row.DeliveredAt == nil {
		t.Fatalf("delivered row: attempts=%d delivered_at=%v", row.Attempts, row.DeliveredAt)
	}

	waitFor(t, 2*time.Second, "metrics mirror", func() bool {
		out := reg.Format()
		return strings.Contains(out, "binflow_webhook_deliveries_total 1") &&
			strings.Contains(out, "binflow_webhook_queue_depth 0")
	})
	for _, family := range []string{
		"binflow_webhook_deliveries_total",
		"binflow_webhook_retries_total",
		"binflow_webhook_dead_letter_total",
		"binflow_webhook_queue_depth",
		"binflow_webhook_enqueue_failures_total",
	} {
		if !strings.Contains(reg.Format(), family) {
			t.Fatalf("metrics exposition missing family %s:\n%s", family, reg.Format())
		}
	}
}

// The retry matrix's terminal arm: a 4xx answer is NEVER retried — one
// attempt, dead row, no retries scheduled (the anchor's negative leg:
// retrying happens only on send failure or >= 500).
func TestT364FourxxIsTerminal(t *testing.T) {
	receiver, hits := scriptedReceiver(t, http.StatusNotFound)
	f := newFixture(t, receiver.URL, "", nil)
	d, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.RetryWait = 80 * time.Millisecond
	})
	defer stop()

	emitDeployed(f.bus, "a/b.jar")
	waitFor(t, 5*time.Second, "dead row", func() bool {
		return rowStatus(f) == webhook.StatusDead
	})
	// Give the would-be retry window ample time to misfire, then pin the
	// single attempt.
	time.Sleep(250 * time.Millisecond)
	if got := len(hits()); got != 1 {
		t.Fatalf("receiver saw %d requests after terminal 4xx, want exactly 1", got)
	}
	row := rowOf(t, f)
	if row.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (4xx never retries)", row.Attempts)
	}
	if row.LastStatusCode == nil || *row.LastStatusCode != 404 {
		t.Fatalf("last_status_code = %v, want 404", row.LastStatusCode)
	}
	if !strings.Contains(row.LastError, "404") {
		t.Fatalf("last_error = %q, want the 404 answer recorded", row.LastError)
	}
	if st := d.Stats(); st.DeadLettered != 1 || st.Retries != 0 || st.Delivered != 0 {
		t.Fatalf("stats = %+v, want dead=1 retries=0 delivered=0", st)
	}
}

// The retry matrix's retry arm: 5xx answers retry at the FIXED interval
// (no backoff curve) until the receiver recovers.
func TestT364FivexxRetriesFixedIntervalThenSucceeds(t *testing.T) {
	receiver, hits := scriptedReceiver(t,
		http.StatusInternalServerError, http.StatusInternalServerError, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)
	d, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.RetryWait = 300 * time.Millisecond
	})
	defer stop()

	emitDeployed(f.bus, "a/c.jar")
	waitFor(t, 8*time.Second, "delivered after retries", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})
	got := hits()
	if len(got) != 3 {
		t.Fatalf("receiver saw %d requests, want 3 (two 500s then the 200)", len(got))
	}
	if row := rowOf(t, f); row.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", row.Attempts)
	}
	// The fixed interval: both inter-attempt gaps sit at the configured
	// wait (lower bound asserted; the scheduler may add slop — what a
	// backoff curve would change is the gaps GROWING, pinned below).
	gap1 := got[1].at.Sub(got[0].at)
	gap2 := got[2].at.Sub(got[1].at)
	if gap1 < 250*time.Millisecond {
		t.Fatalf("first retry gap = %s, want >= ~300ms (fixed interval)", gap1)
	}
	if gap2 < 250*time.Millisecond {
		t.Fatalf("second retry gap = %s, want >= ~300ms (fixed interval)", gap2)
	}
	if st := d.Stats(); st.Retries != 2 || st.Delivered != 1 || st.DeadLettered != 0 {
		t.Fatalf("stats = %+v, want retries=2 delivered=1 dead=0", st)
	}
}

// The retry matrix's timeout arm: an attempt that exceeds the whole-request
// budget is a SEND FAILURE — retryable, and when every attempt times out
// the row dead-letters with the timeout class recorded.
func TestT364AttemptTimeoutRetriesThenDeadLetters(t *testing.T) {
	var hits atomic.Int64
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		<-release // hold every response past the attempt budget
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { unblock(); receiver.Close() })

	f := newFixture(t, receiver.URL, "", nil)
	d, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.AttemptTimeout = 120 * time.Millisecond
		o.RetryWait = 80 * time.Millisecond
		o.RetryCount = 3
	})
	defer stop()
	defer unblock()

	emitDeployed(f.bus, "slow/receiver.bin")
	waitFor(t, 8*time.Second, "dead after timeouts", func() bool {
		return rowStatus(f) == webhook.StatusDead
	})
	if n := hits.Load(); n != 3 {
		t.Fatalf("attempts reached the receiver %d times, want 3 (retryCount, first counted)", n)
	}
	row := rowOf(t, f)
	if row.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", row.Attempts)
	}
	if !strings.Contains(row.LastError, "send failed") {
		t.Fatalf("last_error = %q, want the timeout class recorded", row.LastError)
	}
	if st := d.Stats(); st.DeadLettered != 1 || st.Retries != 2 {
		t.Fatalf("stats = %+v, want dead=1 retries=2", st)
	}
}

// The rate-limit shape (webhook.md 5.1): after the burst budget is spent,
// attempt starts pace at the frequency rate — the receiver's arrival times
// must spread accordingly.
func TestT364RateLimitPacesAttemptStarts(t *testing.T) {
	receiver, hits := scriptedReceiver(t, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.FrequencyPerSec = 4 // one attempt per 250ms once the burst is gone
		o.BurstSize = 1
	})
	defer stop()

	for i := 0; i < 3; i++ {
		emitDeployed(f.bus, "paced/"+strconv.Itoa(i))
	}
	waitFor(t, 8*time.Second, "all three delivered", func() bool {
		return deliveredCount(f) == 3
	})
	got := hits()
	if len(got) != 3 {
		t.Fatalf("receiver saw %d requests, want 3", len(got))
	}
	spread := got[2].at.Sub(got[0].at)
	// Theoretical spread: ~500ms (the first attempt spends the burst
	// token, the two others wait 250ms each). The lower bound is the
	// assertion that matters: an unpaced engine would land all three
	// within milliseconds of the enqueue.
	if spread < 350*time.Millisecond {
		t.Fatalf("attempt starts spread = %s, want >= ~500ms under a 4/s + burst-1 bucket", spread)
	}
}

// The concurrency cap (webhook.md 5.1 maxConcurrentHandlers): while the
// cap-many deliveries are in flight, NEW events are REJECTED at the Emit
// seam — WARN-visible and counted, never silently queued.
func TestT364ConcurrencyCapRejectsNewEvents(t *testing.T) {
	entered := make(chan struct{}, 16)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var released atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		if !released.Load() {
			<-release
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(func() { unblock(); receiver.Close() })

	f := newFixture(t, receiver.URL, "", nil)
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.MaxConcurrent = 1
		o.Workers = 2
	})
	defer stop()

	emitDeployed(f.bus, "first/event.bin")
	<-entered // the first delivery is in flight and holding the cap
	emitDeployed(f.bus, "second/event.bin")
	if n := f.bus.EnqueueFailures(); n != 1 {
		t.Fatalf("enqueue failures = %d, want 1 (the over-cap event rejected)", n)
	}
	// Release the holder: the in-flight attempt completes and lands.
	released.Store(true)
	unblock()
	waitFor(t, 5*time.Second, "in-flight delivery completes", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})
	// The rejected event stays lost (the official rejection is a drop,
	// not a deferral): one row total, nothing pending.
	if n, _ := f.store.CountPending(context.Background()); n != 0 {
		t.Fatalf("pending = %d, want 0", n)
	}
	if rows, _ := f.store.ListDeliveries(context.Background(), f.sub.ID, 10); len(rows) != 1 {
		t.Fatalf("outbox rows = %d, want 1 (the rejected event never enqueued)", len(rows))
	}
}

// AC-1's survival leg: rows enqueued while no engine runs survive in the
// outbox (the "process down" window — a kill -9 between enqueue and drain
// loses nothing the outbox holds), and the startup watermark line is on
// the record.
func TestT364OutboxSurvivalAndWatermark(t *testing.T) {
	receiver, _ := scriptedReceiver(t, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)

	// Engine down: the event lands in the outbox and stays.
	emitDeployed(f.bus, "survives/restart.bin")
	if n, _ := f.store.CountPending(context.Background()); n != 1 {
		t.Fatalf("pending before start = %d, want 1", n)
	}
	time.Sleep(100 * time.Millisecond)
	if n, _ := f.store.CountPending(context.Background()); n != 1 {
		t.Fatalf("pending after engine-down window = %d, want 1 (outbox is the durable state)", n)
	}

	logs := &recordingHandler{}
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) { o.Logger = slog.New(logs) })
	defer stop()
	waitFor(t, 5*time.Second, "surviving row delivered", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})
	if !logs.has("webhook: delivery dispatcher started") {
		t.Fatalf("startup watermark line missing from %v", logs.lines)
	}
}

// AC-1's sweep leg: a row stranded mid-attempt by a kill -9 (delivering,
// attempts already counted) is swept to pending at startup with the
// attempt count preserved — the crash buys exactly one extra attempt.
// Graceful shutdown, by contrast, must leave nothing delivering.
func TestT364SweepDeliveringRows(t *testing.T) {
	receiver, _ := scriptedReceiver(t, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)

	emitDeployed(f.bus, "killed/mid-attempt.bin")
	id := rowOf(t, f).ID

	// A second handle onto the SAME database stands in for the dead
	// process's last write: claimed (attempts=1), died before any outcome.
	direct, err := sql.Open("sqlite", "file:"+url.PathEscape(f.path)+
		"?_pragma=busy_timeout("+itoa(metadata.BusyTimeoutMs)+")")
	if err != nil {
		t.Fatalf("open direct handle: %v", err)
	}
	t.Cleanup(func() { _ = direct.Close() })
	if _, err := direct.Exec(`UPDATE webhook_deliveries SET status='delivering', attempts=1 WHERE id=?`, id); err != nil {
		t.Fatalf("strand row: %v", err)
	}
	if n, _ := f.store.CountPending(context.Background()); n != 0 {
		t.Fatalf("pending with stranded row = %d, want 0", n)
	}

	logs := &recordingHandler{}
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) { o.Logger = slog.New(logs) })
	waitFor(t, 5*time.Second, "swept row delivered", func() bool {
		d, err := f.store.GetDelivery(context.Background(), id)
		return err == nil && d.Status == webhook.StatusDelivered
	})
	got, _ := f.store.GetDelivery(context.Background(), id)
	if got.Attempts != 2 {
		t.Fatalf("attempts after sweep+redelivery = %d, want 2 (the interrupted attempt stayed counted)", got.Attempts)
	}
	if !logs.has("webhook: delivery dispatcher started") {
		t.Fatalf("startup watermark line missing from %v", logs.lines)
	}
	// Graceful drain: nothing stays delivering after stop().
	stop()
	d, err := f.store.GetDelivery(context.Background(), id)
	if err != nil || d.Status == webhook.StatusDelivering {
		t.Fatalf("row left delivering after graceful stop: %+v (%v)", d, err)
	}
}

// The replay face: a dead row replays to pending (attempts reset), and
// replay of a live/finished row refuses with the honest sentinel.
func TestT364ReplayDeadRow(t *testing.T) {
	var notFound atomic.Bool
	notFound.Store(true)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if notFound.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	f := newFixture(t, receiver.URL, "", nil)
	emitDeployed(f.bus, "replay/me.bin")
	d, stop := startDispatcher(t, f.bus, nil)
	defer stop()
	waitFor(t, 5*time.Second, "dead row", func() bool {
		return rowStatus(f) == webhook.StatusDead
	})
	id := rowOf(t, f).ID

	// Replay of a missing row refuses NotFound.
	if err := d.Replay(context.Background(), "no-such-id"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("replay missing = %v, want ErrNotFound", err)
	}
	// The receiver recovers; replay resets and redelivers.
	notFound.Store(false)
	if err := d.Replay(context.Background(), id); err != nil {
		t.Fatalf("replay: %v", err)
	}
	waitFor(t, 5*time.Second, "replayed row delivered", func() bool {
		got, err := f.store.GetDelivery(context.Background(), id)
		return err == nil && got.Status == webhook.StatusDelivered
	})
	got, _ := f.store.GetDelivery(context.Background(), id)
	if got.Attempts != 1 {
		t.Fatalf("attempts after replay = %d, want 1 (reset)", got.Attempts)
	}
	// Replay of a finished row refuses NotDead.
	if err := d.Replay(context.Background(), id); err == nil || !strings.Contains(err.Error(), "not dead") {
		t.Fatalf("replay delivered = %v, want ErrNotDead", err)
	}
}

// The troubleshooting records the dispatcher writes: every failed attempt
// is recorded with its retries_attempted observable, the auth header value
// is redacted to the sentinel (the secret never enters a record), and the
// query face filters by subscription.
func TestT364TroubleshootingRecordsRetries(t *testing.T) {
	receiver, _ := scriptedReceiver(t,
		http.StatusInternalServerError, http.StatusInternalServerError, http.StatusOK)
	cipher, err := remote.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	f := newFixture(t, receiver.URL, `, "secret": "s3cr3t", "use_secret_for_signing": true`, cipher)
	_, stop := startDispatcher(t, f.bus, func(o *webhook.DispatcherOptions) {
		o.RetryWait = 80 * time.Millisecond
	})
	defer stop()

	emitDeployed(f.bus, "recorded/failures.bin")
	waitFor(t, 8*time.Second, "delivered after two failures", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})
	recs, err := f.bus.Troubleshooting(context.Background(), webhook.TroubleshootQuery{Subscription: f.sub.Key})
	if err != nil {
		t.Fatalf("troubleshooting: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2 (both failed attempts; success unrecorded without debug)", len(recs))
	}
	// Newest first: retries_attempted 1 then 0; both carry the error and
	// the redacted auth header.
	for i, want := range []int{1, 0} {
		r := recs[i]
		if r.Request.RetriesAttempted != want {
			t.Fatalf("record[%d].retries_attempted = %d, want %d", i, r.Request.RetriesAttempted, want)
		}
		if len(r.Errors) == 0 || !strings.Contains(r.Errors[0], "500") {
			t.Fatalf("record[%d].errors = %v, want the 500 answer", i, r.Errors)
		}
		if got := r.Request.Headers.Get(webhook.EventAuthHeader); got != "********" {
			t.Fatalf("record[%d] auth header = %q, want the redaction sentinel", i, got)
		}
		if r.Event.SubscriptionKey != f.sub.Key || r.Event.Domain != "artifact" || r.Event.EventType != "deployed" {
			t.Fatalf("record[%d] event identity: %+v", i, r.Event)
		}
		if r.Event.ID == "" || len(r.Event.ID) != 26 {
			t.Fatalf("record[%d] event id %q is not a 26-char ULID", i, r.Event.ID)
		}
	}
}

// A disabled subscription's already-enqueued rows still deliver (the
// outbox snapshot contract: disable gates NEW events at the emit seam; the
// stop-delivery mechanism is deletion, which cascades). Pinning the
// judgment call so a future flip is a deliberate change, not a drift.
func TestT364DisabledSubscriptionSnapshotStillDelivers(t *testing.T) {
	receiver, _ := scriptedReceiver(t, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)

	emitDeployed(f.bus, "snapshot/before-disable.bin")
	off := `{
		"key": "ci-hook",
		"enabled": false,
		"event_filter": {"domain": "artifact", "event_types": ["deployed"], "criteria": {"anyLocal": true}},
		"handlers": [{"handler_type": "webhook", "url": "` + receiver.URL + `"}]
	}`
	if _, err := f.bus.Update(context.Background(), "ci-hook", mustParse(t, off), "admin"); err != nil {
		t.Fatalf("disable update: %v", err)
	}
	// New events of a disabled subscription enqueue nothing (the T-362
	// matcher); the already-enqueued row delivers.
	emitDeployed(f.bus, "gated/after-disable.bin")
	_, stop := startDispatcher(t, f.bus, nil)
	defer stop()
	waitFor(t, 5*time.Second, "snapshot row delivered", func() bool {
		return rowStatus(f) == webhook.StatusDelivered
	})
	if n, _ := f.store.CountPending(context.Background()); n != 0 {
		t.Fatalf("pending = %d, want 0 (post-disable event never enqueued)", n)
	}
	if rows, _ := f.store.ListDeliveries(context.Background(), f.sub.ID, 10); len(rows) != 1 {
		t.Fatalf("outbox rows = %d, want 1", len(rows))
	}
}

// The -race leg: concurrent emits drain through the four-worker pool with
// zero loss and zero duplication.
func TestT364ConcurrentEmitDrainsCompletely(t *testing.T) {
	receiver, hits := scriptedReceiver(t, http.StatusOK)
	f := newFixture(t, receiver.URL, "", nil)
	_, stop := startDispatcher(t, f.bus, nil)
	defer stop()

	const goroutines, perG = 6, 4
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				emitDeployed(f.bus, fmt.Sprintf("race/g%d/i%d.bin", g, i))
			}
		}(g)
	}
	wg.Wait()
	total := goroutines * perG
	waitFor(t, 10*time.Second, "all delivered", func() bool {
		return deliveredCount(f) == total
	})
	got := hits()
	if len(got) != total {
		t.Fatalf("receiver saw %d requests, want %d", len(got), total)
	}
	seen := map[string]bool{}
	for _, hit := range got {
		var env map[string]any
		if err := json.Unmarshal([]byte(hit.body), &env); err != nil {
			t.Fatalf("envelope: %v", err)
		}
		data, _ := env["data"].(map[string]any)
		if data == nil {
			t.Fatalf("envelope carries no data: %s", hit.body)
		}
		seen[data["path"].(string)] = true
	}
	if len(seen) != total {
		t.Fatalf("distinct delivered paths = %d, want %d", len(seen), total)
	}
	if n := f.bus.EnqueueFailures(); n != 0 {
		t.Fatalf("enqueue failures = %d, want 0", n)
	}
}
