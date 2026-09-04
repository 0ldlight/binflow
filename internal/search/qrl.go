package search

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// The query rate limiter (M16 T-452, FR-148.2 / aql.md §14.4): the DB-query
// rate plane, DELAY-based — deliberately orthogonal to the K63 admission
// gate (engine.go): the 429 concurrency arm and the 408 deadline stay the
// only rejection surfaces, the QRL only ever slows a caller down (aql.md
// §14.4's boundary note: "两机制正交"). Three states, exactly the anchor's
// semantics:
//
//	disabled   factory default — the whole limiter is bypassed, zero
//	           bookkeeping, zero behavior (the K63 gate alone governs);
//	enabled    over-budget queries BLOCK until the frame rolls over;
//	simulation over-budget queries pass untouched, the would-be delay is
//	           only recorded (the observation mode).
//
// The BinFlow mapping rulings (in-ticket, aql.md §14.4 leaves them to
// T-452): the DEFAULT bucket's factory numbers ECHO the K63 trio — 4
// permits (the concurrency ceiling), a 10,000ms frame (the execution
// deadline), a 1,000ms charged-time quota (the row cap's number) — so the
// admin REST readout can never disagree with the enforced gate (K72:
// "默认值维持 K63 定案 1000/4/10s"); the two-type model (DEFAULT /
// LOW_PRIORITY) is kept verbatim with identical defaults — BinFlow has no
// low-priority request lane yet, the second bucket exists so the wire shape
// is the anchor's, and the internal SYSTEM type is not configurable (the
// limiter's own bookkeeping never throttles itself, same ruling as the
// anchor's).

// QRLMode is the limiter's tri-state.
type QRLMode string

// The three states (aql.md §14.4's table).
const (
	QRLModeDisabled   QRLMode = "disabled"
	QRLModeEnabled    QRLMode = "enabled"
	QRLModeSimulation QRLMode = "simulation"
)

// ValidQRLModes is the closed state set (wire validation).
var ValidQRLModes = []QRLMode{QRLModeDisabled, QRLModeEnabled, QRLModeSimulation}

// ValidateQRLMode checks one wire mode spelling (the transport's 400 arm).
func ValidateQRLMode(mode string) error {
	for _, m := range ValidQRLModes {
		if QRLMode(mode) == m {
			return nil
		}
	}
	return fmt.Errorf("%w: unknown mode %q (expected one of disabled, enabled, simulation)", ErrQRLInvalidSetting, mode)
}

// QRL rate-bucket types (the anchor's two-type model; internal SYSTEM is
// not on the wire).
const (
	QRLTypeDefault     = "DEFAULT"
	QRLTypeLowPriority = "LOW_PRIORITY"
)

// qrlTypes is the closed bucket-type set.
var qrlTypes = []string{QRLTypeDefault, QRLTypeLowPriority}

// K63GateSnapshot exposes the K63 gate's own constants (K72's consistency
// anchor): the QRL default readout must echo these numbers, and the tests
// pin the equality so the REST face and the enforced gate cannot drift.
type K63GateSnapshot struct {
	RowCap        int64 // ResultCap — echoed as the time-quota number
	Concurrency   int64 // maxConcurrent — echoed as permits-per-frame
	TimeoutMillis int64 // queryTimeout — echoed as the frame length
}

// K63Gate returns the enforced gate's constants.
func K63Gate() K63GateSnapshot {
	return K63GateSnapshot{
		RowCap:        ResultCap,
		Concurrency:   int64(maxConcurrent),
		TimeoutMillis: queryTimeout.Milliseconds(),
	}
}

// QRLSetting is one bucket's wire form (aql.md §14.4's GET/POST body:
// {"rlType":"DEFAULT","permitsPerTimeFrame":N,"timeFrameMillis":N,
// "timeQuota":N}).
type QRLSetting struct {
	RLType              string `json:"rlType"`
	PermitsPerTimeFrame int64  `json:"permitsPerTimeFrame"`
	TimeFrameMillis     int64  `json:"timeFrameMillis"`
	TimeQuota           int64  `json:"timeQuota"`
}

// QRLDefaultSettings returns the two default buckets, both carrying the
// K63-mapped trio (the K72 ruling). A fresh copy every call — callers merge
// into their own state.
func QRLDefaultSettings() []QRLSetting {
	k := K63Gate()
	return []QRLSetting{
		{RLType: QRLTypeDefault, PermitsPerTimeFrame: k.Concurrency, TimeFrameMillis: k.TimeoutMillis, TimeQuota: k.RowCap},
		{RLType: QRLTypeLowPriority, PermitsPerTimeFrame: k.Concurrency, TimeFrameMillis: k.TimeoutMillis, TimeQuota: k.RowCap},
	}
}

// ErrQRLInvalidSetting is the merge-write validation failure (unknown bucket
// type or a non-positive value); the transport renders it as the 400
// envelope.
var ErrQRLInvalidSetting = errors.New("invalid query rate limiter setting")

// ValidateQRLSetting checks one bucket entry: the type must be the closed
// two-type set and all three values strictly positive (a zero frame or
// permit count would deadlock every query; Artifactory's own validation is
// not on record — registered as the BinFlow C-layer rule).
func ValidateQRLSetting(s QRLSetting) error {
	known := false
	for _, t := range qrlTypes {
		if s.RLType == t {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: rlType %q is not one of [DEFAULT, LOW_PRIORITY]", ErrQRLInvalidSetting, s.RLType)
	}
	if s.PermitsPerTimeFrame <= 0 || s.TimeFrameMillis <= 0 || s.TimeQuota <= 0 {
		return fmt.Errorf("%w: %s requires positive permitsPerTimeFrame, timeFrameMillis and timeQuota", ErrQRLInvalidSetting, s.RLType)
	}
	return nil
}

// QRLBucketSample is one bucket's sampled-window counters (aql.md §14.4's
// metric field family: totalQueries / totalPermits / slowedDownMillis /
// slowedDownByTimeMillis / chargedQueryTime — every counter is
// per-sampling-window, reset by Sample).
type QRLBucketSample struct {
	RLType                 string `json:"rlType"`
	TotalQueries           int64  `json:"totalQueries"`
	TotalPermits           int64  `json:"totalPermits"`
	SlowedDownMillis       int64  `json:"slowedDownMillis"`
	SlowedDownByTimeMillis int64  `json:"slowedDownByTimeMillis"`
	ChargedQueryTime       int64  `json:"chargedQueryTime"`
}

// QRLSample is one sampling window's snapshot.
type QRLSample struct {
	Mode         QRLMode           `json:"mode"`
	WindowMillis int64             `json:"windowMillis"` // time since the previous sample
	Buckets      []QRLBucketSample `json:"buckets"`
}

// qrlBucket is one type's limiter state plus its window metrics.
type qrlBucket struct {
	setting QRLSetting
	// window admission state (rolls with the frame)
	windowStart time.Time
	permitsUsed int64
	chargedMs   int64
	// window metric counters (zeroed by Sample)
	totalQueries   int64
	totalPermits   int64
	slowedMs       int64
	slowedByTimeMs int64
	chargedTimeMs  int64
}

// rollWindow advances the frame boundaries until now falls inside the
// current window, zeroing the admission state at each boundary crossed.
func (b *qrlBucket) rollWindow(now time.Time) {
	frame := time.Duration(b.setting.TimeFrameMillis) * time.Millisecond
	for now.Sub(b.windowStart) >= frame {
		b.windowStart = b.windowStart.Add(frame)
		b.permitsUsed = 0
		b.chargedMs = 0
	}
}

// QueryRateLimiter is the process-wide DB-query rate plane. Safe for
// concurrent use; constructed once per process and shared with the engine
// (EngineOptions.QRL).
type QueryRateLimiter struct {
	mu      sync.Mutex
	nowFn   func() time.Time
	mode    QRLMode
	buckets map[string]*qrlBucket
	// lastSampleAt anchors the next sample's WindowMillis.
	lastSampleAt time.Time
}

// NewQueryRateLimiter builds the limiter in the factory state: disabled,
// both buckets at the K63-mapped defaults. now may be nil (time.Now).
func NewQueryRateLimiter(now func() time.Time) *QueryRateLimiter {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	q := &QueryRateLimiter{nowFn: now, mode: QRLModeDisabled, buckets: map[string]*qrlBucket{}}
	q.resetLocked(q.nowFn())
	return q
}

// resetLocked restores the factory state (mode disabled, default buckets).
// Caller holds the mutex.
func (q *QueryRateLimiter) resetLocked(now time.Time) {
	q.mode = QRLModeDisabled
	q.buckets = map[string]*qrlBucket{}
	for _, s := range QRLDefaultSettings() {
		q.buckets[s.RLType] = &qrlBucket{setting: s, windowStart: now}
	}
	q.lastSampleAt = now
}

// Mode returns the current tri-state.
func (q *QueryRateLimiter) Mode() QRLMode {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.mode
}

// Settings returns the effective bucket settings in wire order (DEFAULT
// first).
func (q *QueryRateLimiter) Settings() []QRLSetting {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]QRLSetting, 0, len(q.buckets))
	for _, t := range qrlTypes {
		out = append(out, q.buckets[t].setting)
	}
	return out
}

// QRLConfig is one REST merge-write: Mode empty keeps the current state
// (the disabled arm's verdict is the transport's); each provided bucket
// entry replaces that type's values, absent entries keep theirs.
type QRLConfig struct {
	Mode     QRLMode
	Settings []QRLSetting
}

// Configure validates and applies one merge-write. The settings slice may
// be nil (a pure mode flip); unknown entries or non-positive values refuse
// the whole write (ErrQRLInvalidSetting), leaving the previous state
// untouched.
func (q *QueryRateLimiter) Configure(cfg QRLConfig) error {
	if cfg.Mode != "" {
		if err := ValidateQRLMode(string(cfg.Mode)); err != nil {
			return err
		}
	}
	merged := map[string]QRLSetting{}
	for _, t := range qrlTypes {
		merged[t] = q.buckets[t].setting
	}
	for _, s := range cfg.Settings {
		if err := ValidateQRLSetting(s); err != nil {
			return err
		}
		merged[s.RLType] = s
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.nowFn()
	if cfg.Mode != "" {
		q.mode = cfg.Mode
	}
	for _, t := range qrlTypes {
		q.buckets[t] = &qrlBucket{setting: merged[t], windowStart: now}
	}
	return nil
}

// Reset restores the factory state (the DELETE verb: back to disabled with
// default buckets).
func (q *QueryRateLimiter) Reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.resetLocked(q.nowFn())
}

// Acquire is the admission call the engine's execution segment makes. The
// disabled state is a pure bypass — no state, no counters. Enabled blocks
// at most until the current frame rolls over (bounded additionally by ctx:
// a canceled context returns ctx.Err() and the caller gives up its query).
// Simulation never blocks: the would-be delay lands in the slowed counters
// only.
func (q *QueryRateLimiter) Acquire(ctx context.Context, rlType string) error {
	q.mu.Lock()
	if q.mode == QRLModeDisabled {
		q.mu.Unlock()
		return nil
	}
	b := q.buckets[rlType]
	now := q.nowFn()
	b.rollWindow(now)
	b.totalQueries++
	if b.permitsUsed < b.setting.PermitsPerTimeFrame && b.chargedMs < b.setting.TimeQuota {
		b.permitsUsed++
		b.totalPermits++
		q.mu.Unlock()
		return nil
	}
	// Over budget: the delay is the rest of the frame. The two slowed
	// counters split by cause — permit starvation vs charged-time quota
	// (aql.md §14.4's slowedDownMillis / slowedDownByTimeMillis pair).
	frame := time.Duration(b.setting.TimeFrameMillis) * time.Millisecond
	wait := b.windowStart.Add(frame).Sub(now)
	if wait < 0 {
		wait = 0
	}
	if b.permitsUsed >= b.setting.PermitsPerTimeFrame {
		b.slowedMs += wait.Milliseconds()
	} else {
		b.slowedByTimeMs += wait.Milliseconds()
	}
	simulate := q.mode == QRLModeSimulation
	q.mu.Unlock()

	if simulate || wait == 0 {
		// Simulation admits everything (observation only); a zero wait is
		// the boundary race — admit rather than park on a stopped clock.
		q.admitAfterWait(rlType, 0)
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	q.admitAfterWait(rlType, wait)
	return nil
}

// admitAfterWait takes the permit a throttled caller waited for (the frame
// has rolled by construction; rollWindow is the belt-and-braces for a nowFn
// that stands still).
func (q *QueryRateLimiter) admitAfterWait(rlType string, waited time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	b := q.buckets[rlType]
	b.rollWindow(q.nowFn())
	b.permitsUsed++
	b.totalPermits++
	_ = waited // the slowdown was already recorded at the decision point
}

// Charge books one query's execution time against the bucket's charged-time
// quota and the chargedQueryTime metric. The disabled state books nothing.
func (q *QueryRateLimiter) Charge(rlType string, d time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.mode == QRLModeDisabled {
		return
	}
	b := q.buckets[rlType]
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	b.rollWindow(q.nowFn())
	b.chargedMs += ms
	b.chargedTimeMs += ms
}

// Sample snapshots the window metric counters and resets them (the metrics
// job's tick — aql.md §14.4: counters are per sampling window). The
// admission state (permits/charge of the running frame) is untouched.
func (q *QueryRateLimiter) Sample() QRLSample {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.nowFn()
	out := QRLSample{Mode: q.mode, WindowMillis: now.Sub(q.lastSampleAt).Milliseconds(), Buckets: make([]QRLBucketSample, 0, len(q.buckets))}
	for _, t := range qrlTypes {
		b := q.buckets[t]
		out.Buckets = append(out.Buckets, QRLBucketSample{
			RLType:                 t,
			TotalQueries:           b.totalQueries,
			TotalPermits:           b.totalPermits,
			SlowedDownMillis:       b.slowedMs,
			SlowedDownByTimeMillis: b.slowedByTimeMs,
			ChargedQueryTime:       b.chargedTimeMs,
		})
		b.totalQueries, b.totalPermits = 0, 0
		b.slowedMs, b.slowedByTimeMs, b.chargedTimeMs = 0, 0, 0
	}
	q.lastSampleAt = now
	return out
}
