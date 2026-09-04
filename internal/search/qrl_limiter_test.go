package search

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The query rate limiter in isolation (T-452, aql.md §14.4): the K63
// default mapping (K72), the three states' admission semantics, the window
// metrics' sampling contract and the merge-write validation.

// qrlClock is a controllable clock for the frame math (real time still
// drives the enabled-mode timer — tests keep frames in tens of
// milliseconds).
type qrlClock struct{ now time.Time }

func (c *qrlClock) Now() time.Time { return c.now }

func TestQRLDefaultsEchoK63Gate(t *testing.T) {
	k := K63Gate()
	if k.RowCap != 1000 || k.Concurrency != 4 || k.TimeoutMillis != 10000 {
		t.Fatalf("K63Gate drifted: %+v (the K63 ruling is 1000/4/10s)", k)
	}
	got := QRLDefaultSettings()
	want := []QRLSetting{
		{RLType: QRLTypeDefault, PermitsPerTimeFrame: 4, TimeFrameMillis: 10000, TimeQuota: 1000},
		{RLType: QRLTypeLowPriority, PermitsPerTimeFrame: 4, TimeFrameMillis: 10000, TimeQuota: 1000},
	}
	if len(got) != len(want) {
		t.Fatalf("defaults = %+v, want the two-type shape", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("defaults[%d] = %+v, want %+v — the REST readout must echo the K63 trio (K72)", i, got[i], want[i])
		}
	}
}

func TestQRLFactoryStateIsPureBypass(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	if q.Mode() != QRLModeDisabled {
		t.Fatalf("factory mode = %q, want disabled", q.Mode())
	}
	// Even absurd settings on the buckets cannot matter: the disabled
	// limiter admits instantly and books nothing.
	if err := q.Acquire(context.Background(), QRLTypeDefault); err != nil {
		t.Fatalf("disabled acquire: %v", err)
	}
	q.Charge(QRLTypeDefault, time.Hour)
	s := q.Sample()
	for _, b := range s.Buckets {
		if b.TotalQueries != 0 || b.ChargedQueryTime != 0 {
			t.Fatalf("disabled limiter booked %+v — the bypass must be zero-bookkeeping", b)
		}
	}
}

func TestQRLEnabledAdmitsThenDelays(t *testing.T) {
	clk := &qrlClock{now: time.UnixMilli(1_000_000).UTC()}
	q := NewQueryRateLimiter(clk.Now)
	const frame = 25 * time.Millisecond
	if err := q.Configure(QRLConfig{
		Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 1,
			TimeFrameMillis: frame.Milliseconds(), TimeQuota: 60_000}},
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	ctx := context.Background()
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("first acquire inside the permit budget: %v", err)
	}
	// The second acquire is over budget: it must WAIT for the frame to roll
	// (the anchor's enabled semantics — block, never reject), and the wait
	// lands in the permit-starvation counter.
	start := time.Now()
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("throttled acquire: %v", err)
	}
	if waited := time.Since(start); waited < 15*time.Millisecond {
		t.Fatalf("throttled acquire returned after %s — it must wait out the frame", waited)
	}
	s := q.Sample()
	b := s.Buckets[0]
	if b.TotalQueries != 2 || b.TotalPermits != 2 {
		t.Fatalf("sample = %+v, want 2 attempts and 2 permits (the wait ends in admission)", b)
	}
	if b.SlowedDownMillis == 0 {
		t.Fatalf("permit-starvation counter empty: %+v", b)
	}
	if b.SlowedDownByTimeMillis != 0 {
		t.Fatalf("quota counter must stay zero in the permit arm: %+v", b)
	}
}

func TestQRLEnabledRespectsContextCancel(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	if err := q.Configure(QRLConfig{
		Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 1,
			TimeFrameMillis: 60_000, TimeQuota: 60_000}},
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	ctx := context.Background()
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	if err := q.Acquire(cancelCtx, QRLTypeDefault); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled throttle wait = %v, want context.Canceled", err)
	}
}

func TestQRLSimulationRecordsWithoutBlocking(t *testing.T) {
	clk := &qrlClock{now: time.Now().UTC()}
	q := NewQueryRateLimiter(clk.Now)
	if err := q.Configure(QRLConfig{
		Mode: QRLModeSimulation,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 1,
			TimeFrameMillis: 60_000, TimeQuota: 60_000}},
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	ctx := context.Background()
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	// The over-budget acquire must return IMMEDIATELY (the observation
	// mode) — a 60s frame would make any blocking an instant test failure.
	done := make(chan error, 1)
	go func() { done <- q.Acquire(ctx, QRLTypeDefault) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("simulation acquire: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("simulation acquire blocked — the observation mode never throttles")
	}
	s := q.Sample()
	b := s.Buckets[0]
	if b.TotalQueries != 2 || b.TotalPermits != 2 {
		t.Fatalf("sample = %+v, want both admits counted", b)
	}
	if b.SlowedDownMillis != 60_000 {
		t.Fatalf("slowedDownMillis = %d, want the full would-be frame delay 60000", b.SlowedDownMillis)
	}
}

func TestQRLChargeQuotaThrottleArm(t *testing.T) {
	clk := &qrlClock{now: time.Now().UTC()}
	q := NewQueryRateLimiter(clk.Now)
	const frame = 20 * time.Millisecond
	if err := q.Configure(QRLConfig{
		Mode: QRLModeSimulation,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 100,
			TimeFrameMillis: frame.Milliseconds(), TimeQuota: 10}},
	}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	ctx := context.Background()
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	// A charged execution over the time quota flips the NEXT acquire onto
	// the quota arm (the slowedDownByTimeMillis counter).
	q.Charge(QRLTypeDefault, 50*time.Millisecond)
	if err := q.Acquire(ctx, QRLTypeDefault); err != nil {
		t.Fatalf("acquire after charge: %v", err)
	}
	s := q.Sample()
	b := s.Buckets[0]
	if b.SlowedDownByTimeMillis == 0 {
		t.Fatalf("quota counter empty: %+v", b)
	}
	if b.SlowedDownMillis != 0 {
		t.Fatalf("permit counter must stay zero in the quota arm: %+v", b)
	}
	if b.ChargedQueryTime < 50 {
		t.Fatalf("chargedQueryTime = %d, want the charged 50ms", b.ChargedQueryTime)
	}
}

func TestQRLSampleResetsTheWindow(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	if err := q.Configure(QRLConfig{Mode: QRLModeSimulation}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if err := q.Acquire(context.Background(), QRLTypeDefault); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if s := q.Sample(); s.Buckets[0].TotalQueries != 1 {
		t.Fatalf("first sample = %+v, want the one attempt", s.Buckets[0])
	}
	if s := q.Sample(); s.Buckets[0].TotalQueries != 0 {
		t.Fatalf("second sample = %+v — Sample must reset the window counters", s.Buckets[0])
	}
}

func TestQRLConfigureValidationAndMerge(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	if err := q.Configure(QRLConfig{Mode: "paused"}); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if err := q.Configure(QRLConfig{Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: "SYSTEM", PermitsPerTimeFrame: 1, TimeFrameMillis: 1, TimeQuota: 1}}}); err == nil {
		t.Fatal("internal SYSTEM type accepted on the wire")
	}
	if err := q.Configure(QRLConfig{Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 0, TimeFrameMillis: 1000, TimeQuota: 1000}}}); err == nil {
		t.Fatal("zero permits accepted (would deadlock every query)")
	}
	// A refused write leaves the previous state untouched.
	if q.Mode() != QRLModeDisabled {
		t.Fatalf("refused write leaked the mode: %q", q.Mode())
	}
	// The merge: one bucket provided, the other keeps its values.
	if err := q.Configure(QRLConfig{Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 7, TimeFrameMillis: 5000, TimeQuota: 500}}}); err != nil {
		t.Fatalf("merge write: %v", err)
	}
	got := q.Settings()
	if got[0].PermitsPerTimeFrame != 7 || got[0].TimeFrameMillis != 5000 || got[0].TimeQuota != 500 {
		t.Fatalf("DEFAULT after merge = %+v", got[0])
	}
	if got[1].RLType != QRLTypeLowPriority || got[1].PermitsPerTimeFrame != 4 {
		t.Fatalf("LOW_PRIORITY after merge = %+v, want the untouched defaults", got[1])
	}
	// Reset restores the factory state.
	q.Reset()
	if q.Mode() != QRLModeDisabled {
		t.Fatalf("reset mode = %q, want disabled", q.Mode())
	}
	for _, s := range q.Settings() {
		if s.PermitsPerTimeFrame != 4 || s.TimeFrameMillis != 10000 || s.TimeQuota != 1000 {
			t.Fatalf("reset settings = %+v, want the K63 defaults", s)
		}
	}
}

// TestQRLThrottleDeadlineIsThe408Family pins the deadline mapping of a
// throttle wait: a QRL wait that exhausts the engine's 10s deadline
// surfaces as ErrQueryTimeout (the K63 deadline bounds the whole segment,
// the QRL wait included) — never a raw context error leaking to the 500
// class.
func TestQRLThrottleDeadlineIsThe408Family(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	// One permit, a 60s frame: the second query must wait far beyond any
	// deadline the engine applies.
	if err := q.Configure(QRLConfig{Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 1,
			TimeFrameMillis: 60_000, TimeQuota: 60_000}}}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	e := NewEngine(EngineOptions{
		Nodes: &funcQueryer{},
		ACL:   &funcACL{scope: []repo.ReadScope{{Repo: "r"}}},
		QRL:   q,
	})
	e.timeout = 20 * time.Millisecond // the production 10s, tightened (the engine's own test knob)
	const query = `items.find({"repo":"r"}).include("name")`
	p := &repo.Principal{Name: "admin", Admin: true}
	if _, err := e.Run(context.Background(), p, query); err != nil {
		t.Fatalf("first run: %v", err)
	}
	_, err := e.Run(context.Background(), p, query)
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("deadline-exhausted throttle = %v, want the ErrQueryTimeout (408) family", err)
	}
}

// TestQRLGateOrthogonality is K72's behavior half: with the QRL ACTIVE the
// K63 admission gate still answers ErrResourceBusy at the same four slots
// — the rate plane delays, the concurrency gate rejects, neither bends the
// other (aql.md §14.4's boundary note, T-435 finding 3).
func TestQRLGateOrthogonality(t *testing.T) {
	q := NewQueryRateLimiter(nil)
	// A generous enabled limiter (100 permits / 10s frame): it can never be
	// the reason anything fails.
	if err := q.Configure(QRLConfig{Mode: QRLModeEnabled,
		Settings: []QRLSetting{{RLType: QRLTypeDefault, PermitsPerTimeFrame: 100,
			TimeFrameMillis: 10_000, TimeQuota: 1_000_000}}}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	block := make(chan struct{})
	release := make(chan struct{})
	fq := &funcQueryer{run: func(_ context.Context, _ metadata.NodeQuery) ([]*metadata.NodeQueryRow, error) {
		block <- struct{}{}
		<-release
		return nil, nil
	}}
	e := NewEngine(EngineOptions{
		Nodes: fq,
		ACL:   &funcACL{scope: []repo.ReadScope{{Repo: "r"}}},
		QRL:   q,
	})
	const query = `items.find({"repo":"r"}).include("name")`
	errs := make(chan error, maxConcurrent+1)
	for i := 0; i < maxConcurrent; i++ {
		go func() {
			_, err := e.Run(context.Background(), &repo.Principal{Name: "admin", Admin: true}, query)
			errs <- err
		}()
		<-block
	}
	// The gate is full: the next Run must be the 429 family, NOT a QRL
	// wait (the QRL has 100 permits left — this rejection is the K63
	// ceiling and nothing else).
	if _, err := e.Run(context.Background(), &repo.Principal{Name: "admin", Admin: true}, query); !errors.Is(err, ErrResourceBusy) {
		t.Fatalf("full-gate run = %v, want ErrResourceBusy (the K63 gate unchanged)", err)
	}
	close(release)
	for i := 0; i < maxConcurrent; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("blocked run %d: %v", i, err)
		}
	}
	// The QRL admitted every query that ran (the permit budget never
	// engaged below 100).
	s := q.Sample()
	if s.Buckets[0].TotalPermits != int64(maxConcurrent) {
		t.Fatalf("permits = %d, want %d admitted", s.Buckets[0].TotalPermits, maxConcurrent)
	}
}
