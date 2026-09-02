package replication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The push-replication engine (M6, ADR-0021 decision 2; T-162, T-195).
//
// One Engine per source instance. repo.Service's Put/PutLandedBlob/
// PutManifest tails call Enqueue (the repo.Replicator seam) after an
// artifact lands; Enqueue appends pending ReplicationTask rows for every
// enabled config whose source_repo matches and wakes the worker. Run owns
// the worker loop: it drains pending (and retryable) tasks — directly after
// a wake, and periodically as the cron fallback (crash recovery: tasks left
// pending/in_progress by a previous process, H41; and since T-195 D1, the
// revival of backoff-exhausted transient failures once they age past
// ReviveDelay) — pushing each through one of the TARGET INSTANCE'S upload
// surfaces, selected by the source repository's package type (T-195): the
// generic REST plane (PUT /binflow/{repo}/{path}, generic/maven), the docker
// /v2 registry plane, the npm publish/dist-tag plane, or the pypi multipart
// upload plane. Nothing ever touches the target's storage directly.
//
// Q6/Q7 interim posture (pending user ruling, BOARD T-162): the receiving
// repository is whatever writable surface the config's target_repo names
// (today: a local repository, optionally fronted by a read-only virtual);
// a path already present on the target with the SAME sha256 is an idempotent
// success (no transfer), a DIFFERENT sha256 is a conflict recorded as a
// terminal failed task with the target left untouched. See the T-162 report
// for the reconciliation notes against ADR-0021's first-write-wins/skipped
// wording.

// Audit actions the engine emits (PRD FR-57 audit clause). String literals,
// not audit-package constants: the M6 vocabulary has not landed in
// internal/audit yet (out of T-162's area); the report flags the follow-up.
const (
	// AuditActionPush records one successful push (transfer or idempotent hit).
	AuditActionPush = "replication.push"
	// AuditActionPushFailed records one task reaching a terminal failure.
	AuditActionPushFailed = "replication.push.failed"
)

// DefaultRetryBackoff is the ticket's exponential schedule: 1s, 2s, 4s, 8s,
// 16s between the initial attempt and the five retries (six attempts total).
var DefaultRetryBackoff = []time.Duration{
	1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second,
}

// Engine defaults (AC ② and the cron fallback's configurable cadence).
const (
	// DefaultSweepInterval is the cron fallback period (ticket: default 1m,
	// injectable for tests).
	DefaultSweepInterval = time.Minute
	// DefaultReviveDelay is how old a backoff-exhausted failed task must be
	// (by completed_at) before a cron sweep may revive it (T-195 D1). The
	// value calibrates on architecture section 8's replication
	// task_retry_delay_seconds: 300 — one revival opportunity per five
	// minutes keeps a dead target's failed backlog from hammering the queue
	// every sweep while still converging within minutes of recovery.
	DefaultReviveDelay = 5 * time.Minute
	// DefaultMaxRevives is the internal unlimited-revival sentinel (see
	// EngineOptions.MaxRevives for the option-value mapping). Unlimited is
	// the default posture (T-195 D1): the PRD FR-57-AC4 wording ("恢复后定时
	// cron 增量复制兜底补齐") carries no deadline, and Artifactory's event
	// replication likewise reconnects indefinitely with a capped delay; the
	// per-revive cost is ONE attempt per ReviveDelay per task.
	DefaultMaxRevives = int64(-1)
	// DefaultSocketTimeout bounds connect/TLS/response-header phases, the
	// same posture as remote.DefaultSocketTimeout; there is deliberately no
	// whole-request timeout — artifact bodies stream unbounded.
	DefaultSocketTimeout = 15 * time.Second
	// DefaultEnqueueTimeout bounds one Enqueue's store writes; the hook fires
	// with a detached context, this keeps a wedged store from leaking
	// goroutines.
	DefaultEnqueueTimeout = 30 * time.Second
	// stateWriteTimeout bounds the detached context used for task-status
	// writes inside processTask: shutdown must still be able to revert an
	// in-flight task to pending after the engine context is canceled.
	stateWriteTimeout = 10 * time.Second
	// sweepScanLimit is how many newest tasks per config the candidate scan
	// examines; the 009 idx_replication_tasks_pending index is the eventual
	// home for a status-first scan once the Store interface grows one (see
	// the T-162 report).
	sweepScanLimit = 10000
	// errorTextLimit truncates last_error payloads.
	errorTextLimit = 512
	// responseSnippetLimit bounds how much of an error response body is
	// folded into a task's last_error for diagnostics.
	responseSnippetLimit = 512
	// pushUserAgent identifies the engine to the target instance.
	pushUserAgent = "binflow-replication/1.0"
)

// notRetryableText is the substring every not-retryable task's last_error
// carries (it is part of ErrNotRetryable's own message, so every wrap since
// T-162 already persists it). The cron revival scan (D1) matches on it to
// keep deterministic faults — conflicts, refused target URLs, vanished
// source facts — terminal while backoff-exhausted transient failures become
// revivable; a unit test pins the composition so the wording cannot drift.
const notRetryableText = "not retryable"

// ErrNotRetryable marks task failures that retrying cannot fix: a refused
// target URL (SSRF guard), an unde cryptable/unconfigured credential, a
// source blob that vanished, or a target-side conflict. processTask turns
// them into terminal failed tasks instead of burning the backoff schedule.
var ErrNotRetryable = errors.New("replication: failure is " + notRetryableText)

// notRetryable wraps err with the ErrNotRetryable sentinel.
func notRetryable(err error) error {
	return fmt.Errorf("%w: %w", ErrNotRetryable, err)
}

// BlobSource is the engine's read-only view of the source instance's blob
// store (consumer-side seam, architecture section 3): storage.Engine
// satisfies it structurally, which keeps the engine off the metadata layer —
// node facts travel with the task row, only bytes are read here.
type BlobSource interface {
	// Open opens the blob for reading (caller closes). Missing blobs error.
	Open(ctx context.Context, sha256 string) (io.ReadCloser, storage.BlobRef, error)
}

// AuditSink is the consumer-side audit seam (structurally satisfied by
// audit.Logger). Append failures are logged and swallowed — auditing must
// never fail a push (the same best-effort contract repo.Service uses).
type AuditSink interface {
	Append(ctx context.Context, e audit.Event) error
}

// EngineOptions configures NewEngine. The zero value is a usable engine with
// the ticket's defaults; every field is a test or assembly seam.
type EngineOptions struct {
	// Now stamps task timestamps (RFC3339 UTC). nil = time.Now().UTC().
	Now func() time.Time
	// Logger receives WARN lines (enqueue failures, task failures). nil =
	// slog.Default().
	Logger *slog.Logger
	// Sleep backs off between retries of one task. nil = real timer sleep,
	// cancellable by context. Tests record the delays instead.
	Sleep func(ctx context.Context, d time.Duration) error
	// RetryBackoff: delays between the six attempts (initial + five
	// retries). nil/empty = DefaultRetryBackoff.
	RetryBackoff []time.Duration
	// SweepInterval is the cron fallback period. <= 0 = DefaultSweepInterval.
	SweepInterval time.Duration
	// SocketTimeout bounds the transport's connect/TLS/header phases.
	// <= 0 = DefaultSocketTimeout.
	SocketTimeout time.Duration
	// DenyPrivateTargets turns the SSRF screening list ON for target
	// addresses. The zero value (private targets allowed) is deliberate:
	// every realistic replication target is a private host
	// (datacenter-to-datacenter, same-host), so the default keeps the
	// feature usable while the guard still enforces scheme/host sanity,
	// per-hop re-screening and DNS-rebinding pinning (ADR-0021: target URLs
	// pass the internal/remote SSRF guard). Flip this to true for the
	// tightest posture (e.g. replication over public hosts only).
	DenyPrivateTargets bool
	// Cipher decrypts ReplicationConfig.TargetPasswordEnc (the ADR-0012
	// enc:v1 scheme, shared with remote credentials). nil = encrypted
	// passwords fail their tasks as not-retryable; legacy plaintext and
	// empty passwords pass through.
	Cipher *remote.Cipher
	// Audit receives push/push.failed events. nil = no auditing.
	Audit AuditSink
	// Resolve overrides the guard's host resolution (test seam).
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	// MaxRevives bounds how many extra attempts the cron sweep may buy a
	// backoff-exhausted failed task (T-195 D1). 0 = the default
	// (DefaultMaxRevives, unlimited); negative = revival disabled (the
	// T-162 posture); positive N = at most N revivals (attempts cap =
	// maxAttempts + N). Deterministic not-retryable failures are NEVER
	// revived regardless of this setting.
	MaxRevives int64
	// ReviveDelay is the minimum age of a failed task (completed_at versus
	// Now) before a sweep may revive it. <= 0 = DefaultReviveDelay. Zero
	// delay is expressible only through a positive-but-tiny value.
	ReviveDelay time.Duration
	// Meta is the source-side metadata seam the protocol-aware push planes
	// (docker/npm/pypi, T-195) select on. nil = every task pushes through
	// the generic REST plane (the T-162 behavior — correct for generic and
	// maven repositories, insufficient for the protocol layouts).
	Meta MetaSource
	// Blocks is the global blockPush/blockPull gate (T-422, §9.2-B): a
	// blocked push direction stops event enqueues, task claims and manual
	// triggers. nil = the pre-T-422 posture (no block state exists, nothing
	// is ever blocked) — the zero value of every pre-existing assembly and
	// test.
	Blocks *BlockGate
}

// engineConfig is the normalized EngineOptions.
type engineConfig struct {
	now          func() time.Time
	sleep        func(ctx context.Context, d time.Duration) error
	backoff      []time.Duration
	sweepEvery   time.Duration
	socketEvery  time.Duration
	cipher       *remote.Cipher
	audit        AuditSink
	allowPrivate bool
	maxRevives   int64
	reviveEvery  time.Duration
	meta         MetaSource
	blocks       *BlockGate
}

// maxAttempts is the total attempt budget: the initial attempt plus one retry
// per backoff entry (1s→2s→4s→8s→16s, five retries, six attempts).
func (c *engineConfig) maxAttempts() int64 { return int64(len(c.backoff)) + 1 }

// Engine is the push-replication worker. Build with NewEngine, attach to the
// repo service's Replicator seam, then run Run in a dedicated goroutine until
// the context is canceled. An Engine must not be copied after NewEngine.
type Engine struct {
	store  Store
	blobs  BlobSource
	cfg    engineConfig
	log    *slog.Logger
	guard  *remote.Guard
	client *http.Client
	// pkgTypes memoizes the source repositories' package types (T-195 plane
	// selection). Touched only from the drain goroutine — Run's single
	// worker owns every read and write, so no lock is needed.
	pkgTypes map[string]string
	// wake coalesces enqueue signals (capacity 1: one pending drain is
	// enough, whichever side wins the race).
	wake chan struct{}
}

// NewEngine assembles the engine. store and blobs are required; opts zero
// fields fall back to the documented defaults.
func NewEngine(store Store, blobs BlobSource, opts EngineOptions) (*Engine, error) {
	if store == nil {
		return nil, errors.New("replication: NewEngine: store is nil")
	}
	if blobs == nil {
		return nil, errors.New("replication: NewEngine: blobs is nil")
	}
	cfg := engineConfig{
		now:          opts.Now,
		sleep:        opts.Sleep,
		backoff:      append([]time.Duration(nil), opts.RetryBackoff...),
		sweepEvery:   opts.SweepInterval,
		socketEvery:  opts.SocketTimeout,
		cipher:       opts.Cipher,
		audit:        opts.Audit,
		allowPrivate: !opts.DenyPrivateTargets,
		maxRevives:   opts.MaxRevives,
		reviveEvery:  opts.ReviveDelay,
		meta:         opts.Meta,
		blocks:       opts.Blocks,
	}
	if cfg.now == nil {
		cfg.now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.sleep == nil {
		cfg.sleep = sleepCtx
	}
	if len(cfg.backoff) == 0 {
		cfg.backoff = append([]time.Duration(nil), DefaultRetryBackoff...)
	}
	for i, d := range cfg.backoff {
		if d <= 0 {
			return nil, fmt.Errorf("replication: NewEngine: retry backoff[%d] = %s: must be positive", i, d)
		}
	}
	if cfg.sweepEvery <= 0 {
		cfg.sweepEvery = DefaultSweepInterval
	}
	if cfg.reviveEvery <= 0 {
		cfg.reviveEvery = DefaultReviveDelay
	}
	// Normalize the revival budget onto the internal spelling: cfg.maxRevives
	// is the exact cap of EXTRA attempts (>= 0); DefaultMaxRevives (< 0) is
	// the unlimited sentinel and the zero-value default, so an explicit
	// negative option value (the "disable" switch) maps onto 0 extras.
	switch {
	case opts.MaxRevives > 0:
		cfg.maxRevives = opts.MaxRevives
	case opts.MaxRevives < 0:
		cfg.maxRevives = 0
	default:
		cfg.maxRevives = DefaultMaxRevives // -1: unlimited
	}
	if cfg.socketEvery <= 0 {
		cfg.socketEvery = DefaultSocketTimeout
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	guard := remote.NewGuard(remote.GuardOptions{
		RepoKey:              "replication",
		AllowPrivateUpstream: cfg.allowPrivate,
		Logger:               log,
		Resolve:              opts.Resolve,
	})
	transport := &http.Transport{
		DialContext:           guard.Dialer(cfg.socketEvery),
		TLSHandshakeTimeout:   cfg.socketEvery,
		ResponseHeaderTimeout: cfg.socketEvery,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     false,
		DisableCompression:    true,
	}
	return &Engine{
		store:    store,
		blobs:    blobs,
		cfg:      cfg,
		log:      log,
		guard:    guard,
		pkgTypes: map[string]string{},
		client: &http.Client{
			Transport: transport,
			// Redirects are not followed: a replication target answering 3xx
			// is a configuration smell worth a task failure, not a hop to
			// chase (credentials must never leak to a second host).
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		wake: make(chan struct{}, 1),
	}, nil
}

// Compile-time pin: the engine satisfies the repo package's enqueue seam
// (the hook side asserts the same structurally; this pin keeps the signature
// from drifting apart from repo.Replicator without an import).
var _ interface {
	Enqueue(ctx context.Context, repoKey, path, sha256 string)
} = (*Engine)(nil)

// nowStamp renders the clock as the RFC3339 UTC text the store expects.
func (e *Engine) nowStamp() string { return e.cfg.now().Format(time.RFC3339) }

// signal wakes the worker; the buffered channel coalesces while a drain runs.
func (e *Engine) signal() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Kick wakes the worker from outside the engine (T-422): the block gate's
// post-update hook — an UNBLOCK resumes the parked queue immediately
// instead of waiting out the sweep tick. Blocking needs no kick: the next
// drain pass parks itself at the gate (§9.2-B-4).
func (e *Engine) Kick() { e.signal() }

// pushBlocked consults the global gate (nil = never blocked).
func (e *Engine) pushBlocked() bool {
	return e.cfg.blocks != nil && e.cfg.blocks.PushBlocked()
}

// Enqueue is the repo.Service.Put-tail hook (AC ①): one pending
// ReplicationTask per ENABLED config whose source_repo matches, then a wake.
// It must never fail or slow the upload path — callers run it on a detached
// context in its own goroutine, it logs (not returns) every failure, and the
// recover shield keeps a store panic from taking the process down.
//
// No dedup (ADR-0021 note 28): re-enqueuing the same (config, path) appends
// a new ledger row; target-side sha256 dedup makes the extra push a no-op.
func (e *Engine) Enqueue(ctx context.Context, repoKey, path, sha256 string) {
	if repoKey == "" || path == "" || sha256 == "" {
		return
	}
	// The global push block (T-422, §9.2-B-4b): a blocked instance appends
	// NO task rows — a re-deploy of the source repository must not reach
	// the target while the brake is on (the events are simply not captured;
	// an unblocked later state converges through a manual full sync).
	if e.pushBlocked() {
		e.log.WarnContext(ctx, "replication: enqueue skipped: push replication is blocked",
			"repo", repoKey, "path", path)
		return
	}
	defer func() {
		if v := recover(); v != nil {
			e.log.Warn("replication: enqueue panicked", "repo", repoKey, "path", path, "panic", v)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, DefaultEnqueueTimeout)
	defer cancel()
	configs, err := e.store.ListConfigs(ctx)
	if err != nil {
		e.log.WarnContext(ctx, "replication: enqueue: list configs failed",
			"repo", repoKey, "error", err)
		return
	}
	created := 0
	for _, c := range configs {
		if !c.Enabled || c.SourceRepo != repoKey {
			continue
		}
		_, err := e.store.CreateTask(ctx, &ReplicationTask{
			ReplicationID: c.ID,
			BlobSHA256:    sha256,
			NodePath:      path,
			Status:        TaskStatusPending,
			CreatedAt:     e.nowStamp(),
		})
		if err != nil {
			e.log.WarnContext(ctx, "replication: enqueue: create task failed",
				"replication", c.Name, "path", path, "error", err)
			continue
		}
		created++
	}
	if created > 0 {
		e.signal()
	}
}

// Run is the worker loop (AC ① executor + AC ② cron fallback): an initial
// drain picks up tasks a previous process left behind (H41 crash recovery),
// then every wake and every sweep tick drains again — the drain's candidate
// scan IS the cron fallback, it re-queues retryable failed and stale
// in_progress tasks by processing them. Run returns when ctx is canceled:
// the in-flight attempt aborts at its next context boundary and is reverted
// to pending (nothing stays in_progress), no new task is claimed, and
// whatever is left pending waits for the next process start. Callers that
// need the drain to finish should cancel and then wait for Run to return.
func (e *Engine) Run(ctx context.Context) error {
	ticker := time.NewTicker(e.cfg.sweepEvery)
	defer ticker.Stop()
	e.drain(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-e.wake:
			e.drain(ctx)
		case <-ticker.C:
			e.drain(ctx)
		}
	}
}

// drain claims and processes every runnable task of every enabled config,
// oldest first. Single-threaded by design (one worker goroutine owns Run):
// the stall a dead target induces on its queue is intended backpressure, and
// in_progress rows found by the scan can only be crash residue — nothing
// else writes that status while the loop owns the pass.
//
// The failed-task arms implement the D1 revival contract: a task whose
// backoff burned out (attempts at the cycle cap, transient failure class)
// becomes revivable once it has aged past the configured ReviveDelay, buying
// one more attempt per sweep — the cron fallback PRD FR-57-AC4 demands.
// Deterministic not-retryable failures (the last_error marker) and tasks
// past their revival budget stay terminal.
func (e *Engine) drain(ctx context.Context) {
	// The global push block gates the claim track too (T-422, §9.2-B-4):
	// while the brake is on, nothing is claimed and nothing is revived —
	// pending rows simply wait (an attempt already in flight runs to its
	// own conclusion inside processTask, whose loop top re-checks the gate
	// between attempts).
	if e.pushBlocked() {
		e.log.WarnContext(ctx, "replication: drain skipped: push replication is blocked")
		return
	}
	configs, err := e.store.ListConfigs(ctx)
	if err != nil {
		e.log.WarnContext(ctx, "replication: drain: list configs failed", "error", err)
		return
	}
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		tasks, err := e.store.ListTasks(ctx, cfg.ID, sweepScanLimit)
		if err != nil {
			e.log.WarnContext(ctx, "replication: drain: list tasks failed",
				"replication", cfg.Name, "error", err)
			continue
		}
		runnable := make([]*ReplicationTask, 0, len(tasks))
		for _, t := range tasks {
			switch t.Status {
			case TaskStatusPending:
				runnable = append(runnable, t)
			case TaskStatusFailed:
				if t.Attempts < e.cfg.maxAttempts() || e.revivable(t) {
					runnable = append(runnable, t)
				}
			case TaskStatusInProgress:
				runnable = append(runnable, t)
			}
		}
		sort.SliceStable(runnable, func(i, j int) bool {
			if runnable[i].CreatedAt != runnable[j].CreatedAt {
				return runnable[i].CreatedAt < runnable[j].CreatedAt
			}
			return runnable[i].ID < runnable[j].ID
		})
		for _, t := range runnable {
			if ctx.Err() != nil {
				return
			}
			e.processTask(ctx, cfg, t)
		}
	}
}

// reviveCap is the total attempt budget across all revival cycles:
// the backoff cycle's maxAttempts plus the configured extra attempts.
func (c *engineConfig) reviveCap() int64 {
	if c.maxRevives < 0 { // the unlimited sentinel
		return 1<<62 - 1
	}
	return c.maxAttempts() + c.maxRevives
}

// revivable decides whether one cron sweep may hand a backoff-exhausted
// failed task another attempt (D1). The gates, in order: the budget (zero
// extras or attempts already at the cap), the class (the not-retryable
// marker in last_error — conflicts and configuration faults stay terminal),
// and the age (completed_at at least ReviveDelay in the past, so a dead
// target's backlog costs one probe per interval, not one per sweep; the
// stamp's RFC3339 SECOND granularity can admit a revive up to one second
// early — harmless for a rate limiter). A missing or unparsable
// completed_at fails open toward revival: eventual consistency outranks
// bookkeeping skepticism for transient-class failures.
func (e *Engine) revivable(t *ReplicationTask) bool {
	if e.cfg.maxRevives == 0 || t.Attempts >= e.cfg.reviveCap() {
		return false
	}
	if strings.Contains(t.LastError, notRetryableText) {
		return false
	}
	if t.CompletedAt == "" {
		return true
	}
	stamp, err := time.Parse(time.RFC3339, t.CompletedAt)
	if err != nil {
		return true
	}
	return e.cfg.now().Sub(stamp) >= e.cfg.reviveEvery
}

// truncateErr bounds an error text for the last_error column.
func truncateErr(err error) string {
	msg := err.Error()
	if len(msg) > errorTextLimit {
		return msg[:errorTextLimit]
	}
	return msg
}

// writeTask persists a task's mutable columns on a context detached from the
// engine's shutdown signal: terminal and revert writes must land even while
// the process is draining (a canceled context would strand the row in
// in_progress forever).
func (e *Engine) writeTask(task *ReplicationTask) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), stateWriteTimeout)
	defer cancel()
	if err := e.store.UpdateTask(ctx, task); err != nil {
		e.log.WarnContext(ctx, "replication: task update failed", "task", task.ID, "error", err)
		return err
	}
	return nil
}

// auditPush appends one best-effort audit event.
func (e *Engine) auditPush(ctx context.Context, failed bool, cfg *ReplicationConfig, t *ReplicationTask, attempt int64, err error) {
	if e.cfg.audit == nil {
		return
	}
	action := AuditActionPush
	detail := map[string]any{
		"replication": cfg.Name,
		"target":      cfg.TargetURL,
		"target_repo": cfg.TargetRepo,
		"sha256":      t.BlobSHA256,
		"attempts":    attempt,
	}
	if failed {
		action = AuditActionPushFailed
		detail["status"] = t.Status
		detail["error"] = truncateErr(err)
	}
	raw, jerr := json.Marshal(detail)
	if jerr != nil {
		raw = []byte(`{"error":"detail marshal failed"}`)
	}
	ev := audit.Event{
		Actor:  "replication",
		Action: action,
		Repo:   cfg.SourceRepo,
		Path:   t.NodePath,
		Detail: string(raw),
	}
	if aerr := e.cfg.audit.Append(ctx, ev); aerr != nil {
		e.log.WarnContext(ctx, "replication: audit append failed", "action", action, "error", aerr)
	}
}

// processTask drives one task to a terminal state: claim (in_progress,
// attempts+1), push, then success / not-retryable failure / exhausted
// failure, backing off between attempts per the configured schedule. An
// engine-shutdown interruption reverts the task to pending for the next
// process start (the interrupted attempt stays counted).
func (e *Engine) processTask(ctx context.Context, cfg *ReplicationConfig, task *ReplicationTask) {
	attempt := task.Attempts
	var lastClaim *ReplicationTask
	for {
		if err := ctx.Err(); err != nil {
			e.revertTask(task, err)
			return
		}
		// The block gate between attempts (T-422): a brake that landed while
		// this task was cycling stops the NEXT attempt — the row reverts to
		// pending exactly like a shutdown interruption and waits for the
		// unblock. The revert carries the last claim so an attempt already
		// spent stays counted.
		if e.pushBlocked() {
			if lastClaim != nil {
				e.revertTask(lastClaim, ErrPushBlocked)
			} else {
				e.revertTask(task, ErrPushBlocked)
			}
			return
		}
		attempt++
		claim := *task
		claim.Status = TaskStatusInProgress
		claim.Attempts = attempt
		claim.LastError = ""
		claim.CompletedAt = ""
		lastClaim = &claim
		if err := e.writeTask(&claim); err != nil {
			return // store fault: leave the row as loaded, the sweep retries
		}
		err := e.pushOnce(ctx, cfg, task.BlobSHA256, task.NodePath)
		if err == nil {
			done := claim
			done.Status = TaskStatusSuccess
			done.CompletedAt = e.nowStamp()
			// A terminal-state write failure only loses the bookkeeping
			// update (logged by writeTask); the push itself succeeded.
			_ = e.writeTask(&done)
			e.auditPush(ctx, false, cfg, task, attempt, nil)
			return
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			e.revertTask(&claim, err)
			return
		}
		if errors.Is(err, ErrNotRetryable) {
			terminal := claim
			terminal.Status = TaskStatusFailed
			// A not-retryable failure is recorded as exhaustively tried so
			// the sweep does not revive it (Q7 interim: conflicts and
			// configuration faults stay put until an admin re-triggers).
			terminal.Attempts = e.cfg.maxAttempts()
			terminal.LastError = truncateErr(err)
			terminal.CompletedAt = e.nowStamp()
			_ = e.writeTask(&terminal)
			e.auditPush(ctx, true, cfg, task, attempt, err)
			return
		}
		if attempt >= e.cfg.maxAttempts() {
			terminal := claim
			terminal.Status = TaskStatusFailed
			terminal.LastError = truncateErr(err)
			terminal.CompletedAt = e.nowStamp()
			_ = e.writeTask(&terminal)
			e.auditPush(ctx, true, cfg, task, attempt, err)
			return
		}
		if serr := e.cfg.sleep(ctx, e.cfg.backoff[int(attempt)-1]); serr != nil {
			e.revertTask(&claim, serr)
			return
		}
	}
}

// revertTask puts an interrupted task back to pending (shutdown semantics):
// the row must not survive a drain in in_progress, and the next process
// start's initial drain picks it up again.
func (e *Engine) revertTask(task *ReplicationTask, cause error) {
	revert := *task
	revert.Status = TaskStatusPending
	revert.LastError = truncateErr(fmt.Errorf("attempt interrupted: %w", cause))
	revert.CompletedAt = ""
	_ = e.writeTask(&revert)
}

// sleepCtx is the production backoff sleep: a context-cancellable timer.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// resolvePassword decrypts the config's target credential (never logged).
func (e *Engine) resolvePassword(cfg *ReplicationConfig) (string, error) {
	if cfg.TargetPasswordEnc == "" {
		return "", nil
	}
	// remote.(*Cipher).Decrypt tolerates a nil receiver for plaintext and
	// refuses encrypted values with no master key — exactly the engine's
	// contract.
	secret, _, err := e.cfg.cipher.Decrypt(cfg.TargetPasswordEnc)
	if err != nil {
		return "", notRetryable(fmt.Errorf("target credential: %w", err))
	}
	return secret, nil
}

// targetBase parses and validates the config's target URL (shared by every
// push plane's URL builder). The validations are the not-retryable class: a
// mangled config row must fail the task deterministically, never turn a
// push into a traversal or a scheme the SSRF guard would reject per hop.
func (e *Engine) targetBase(cfg *ReplicationConfig) (*url.URL, error) {
	if strings.TrimSpace(cfg.TargetRepo) == "" {
		return nil, notRetryable(fmt.Errorf("config %q: target_repo is empty", cfg.Name))
	}
	base, err := url.Parse(strings.TrimSpace(cfg.TargetURL))
	if err != nil {
		return nil, notRetryable(fmt.Errorf("config %q: target url: %w", cfg.Name, err))
	}
	if (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, notRetryable(fmt.Errorf("config %q: target url %q: only absolute http/https URLs", cfg.Name, cfg.TargetURL))
	}
	return base, nil
}

// targetURL builds the target-side artifact URL:
// {TargetURL}/binflow/{TargetRepo}/{NodePath}. Every path segment is
// escaped individually; dot segments and empty segments (which the source's
// node-path validation already forbids) are refused defensively — a mangled
// config row must not turn the push into a traversal.
func (e *Engine) targetURL(cfg *ReplicationConfig, nodePath string) (*url.URL, error) {
	base, err := e.targetBase(cfg)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(nodePath, "/")
	for i, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return nil, notRetryable(fmt.Errorf("config %q: node path %q: illegal segment %q", cfg.Name, nodePath, seg))
		}
		segments[i] = url.PathEscape(seg)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/binflow/" + cfg.TargetRepo + "/" + strings.Join(segments, "/")
	u.RawPath = "" // re-derived from Path by String()
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

// planeURL builds a target content-plane address with explicit segments:
// {TargetURL}/binflow/{segments...} — the first segment is the push plane's
// caller-supplied repository key (cfg.TargetRepo for the content mount, the
// npm service routes' "-" namespace included). Same escaping and refusal
// contract as targetURL.
func (e *Engine) planeURL(cfg *ReplicationConfig, nodePath string, segments ...string) (*url.URL, error) {
	base, err := e.targetBase(cfg)
	if err != nil {
		return nil, err
	}
	esc := make([]string, len(segments))
	for i, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return nil, notRetryable(fmt.Errorf("config %q: path %q: illegal segment %q", cfg.Name, nodePath, seg))
		}
		esc[i] = url.PathEscape(seg)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/binflow/" + strings.Join(esc, "/")
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return &u, nil
}

// readAllBounded reads one source blob fully into memory under cap. Plane
// callers need the whole body in RAM (docker manifests are re-PUT per tag,
// npm attachments ride inline in the publish JSON, and retries must be able
// to re-send); the caps keep a corrupt size record from ballooning the
// engine. An over-cap blob and a vanished/short blob are both deterministic
// faults on the source side: not retryable.
func (e *Engine) readAllBounded(ctx context.Context, sha256 string, limit int64) ([]byte, error) {
	rc, _, err := e.blobs.Open(ctx, sha256)
	if err != nil {
		return nil, notRetryable(fmt.Errorf("source blob %s: %w", sha256, err))
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read source blob %s: %w", sha256, err)
	}
	if int64(len(body)) > limit {
		return nil, notRetryable(fmt.Errorf("source blob %s: %d bytes exceeds the %d byte plane limit", sha256, len(body), limit))
	}
	return body, nil
}

// do issues one guarded request (SSRF chain point 1/2 here, point 3 in the
// transport's dialer) with the config's Basic credentials. The response body
// is the CALLER's to close.
func (e *Engine) do(ctx context.Context, method string, u *url.URL, cfg *ReplicationConfig, body io.Reader, size int64, hdr http.Header) (*http.Response, error) {
	if err := e.guard.CheckURL(ctx, u.String()); err != nil {
		return nil, notRetryable(fmt.Errorf("config %q: %w", cfg.Name, err))
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", method, err)
	}
	for k, vv := range hdr {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	if size > 0 {
		req.ContentLength = size
	}
	req.Header.Set("User-Agent", pushUserAgent)
	if secret, perr := e.resolvePassword(cfg); perr != nil {
		return nil, perr
	} else if cfg.TargetUsername != "" || secret != "" {
		req.SetBasicAuth(cfg.TargetUsername, secret)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		if remote.IsRejection(err) {
			return nil, notRetryable(fmt.Errorf("config %q: %w", cfg.Name, err))
		}
		return nil, fmt.Errorf("%s %s: %w", method, u.Redacted(), err)
	}
	return resp, nil
}

// readSnippet drains (bounded) and closes an error response body, returning
// a short diagnostic snippet.
func readSnippet(resp *http.Response) string {
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseSnippetLimit))
	if err != nil || len(raw) == 0 {
		return ""
	}
	return string(raw)
}

// classify maps a non-2xx target response onto a task error. Deterministic
// client-fault statuses (409: declared-checksum mismatch / refused notation)
// are not retryable; everything else (auth refused, missing repo, target
// 5xx) is — the backoff schedule and the cron sweep handle the transient
// cases, and an admin fixing the target revives attempts-below-cap tasks.
func classify(resp *http.Response, what string) error {
	snippet := readSnippet(resp)
	err := fmt.Errorf("%s: target answered %d %s", what, resp.StatusCode, strings.TrimSpace(snippet))
	if resp.StatusCode == http.StatusConflict {
		return notRetryable(err)
	}
	return err
}

// pushOnce performs ONE push attempt for (sha256, nodePath) under cfg,
// selecting the push plane by the SOURCE repository's package type
// (T-195): docker repositories speak the /v2 registry protocol (blobs by
// digest, manifests with tags — anything else answers 404 UNSUPPORTED on
// the target, T-175 D2); npm repositories converge the package through the
// npm publish and dist-tag faces (raw node writes are refused there,
// T-175 D3); pypi repositories ride the warehouse multipart upload face;
// everything else (generic, maven) keeps the T-162 generic REST plane the
// QA-verified path already exercises. Without a MetaSource every task takes
// the generic plane — the T-162 behavior.
//
// Retriable failures are returned unwrapped (the retry loop backs off);
// deterministic faults carry ErrNotRetryable.
func (e *Engine) pushOnce(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath string) error {
	if e.cfg.meta != nil {
		pt, err := e.packageType(ctx, cfg.SourceRepo)
		if err != nil {
			return err
		}
		var perr error
		plane := pt
		switch pt {
		case "docker":
			perr = e.pushDocker(ctx, cfg, sha256, nodePath)
		case "npm":
			perr = e.pushNpm(ctx, cfg, sha256, nodePath)
		case "pypi":
			perr = e.pushPypi(ctx, cfg, sha256, nodePath)
		default:
			// generic, maven and any future plain-path type: the T-162
			// REST plane (content-path PUT), QA-verified.
			return e.pushGeneric(ctx, cfg, sha256, nodePath)
		}
		if perr != nil {
			return fmt.Errorf("push %s/%s → %s/%s (%s plane): %w",
				cfg.SourceRepo, nodePath, cfg.TargetURL, cfg.TargetRepo, plane, perr)
		}
		return nil
	}
	return e.pushGeneric(ctx, cfg, sha256, nodePath)
}

// packageType resolves (and memoizes) the source repository's package type.
// A missing repository row is not-retryable (the FK normally prevents it; a
// row that vanished anyway cannot be retried into existence); any other
// lookup failure is transient and left retryable.
func (e *Engine) packageType(ctx context.Context, repoKey string) (string, error) {
	if pt, ok := e.pkgTypes[repoKey]; ok {
		return pt, nil
	}
	pt, err := e.cfg.meta.PackageType(ctx, repoKey)
	if err != nil {
		return "", err
	}
	e.pkgTypes[repoKey] = pt
	return pt, nil
}

// pushGeneric performs ONE generic-plane push attempt for (sha256, nodePath)
// under cfg:
//
//  1. HEAD the target path — present with the same sha256 is an idempotent
//     success (zero transfer, ADR-0021 idempotency clause); present with a
//     different sha256 is a Q7-interim conflict (not retryable, target
//     untouched); absent proceeds to upload.
//  2. PUT the blob bytes with the declared X-Checksum-Sha256 — the target
//     verifies the digest during ingest (a 409 means the source bytes no
//     longer match their own checksum, a not-retryable fault).
//  3. Property carry (T-317, FR-101.2): after EITHER success arm, the
//     source node's properties (read at push time) merge onto the target
//     node through the target instance's property face — replication.md
//     2.1's syncProperties=true default posture, so an idempotent re-push
//     also converges properties that landed after the first transfer.
//     Protocol planes (docker/npm/pypi) build their target nodes through
//     protocol faces whose property semantics belong to those adapters —
//     the carry is the generic plane's (generic/maven), T-317 report.
func (e *Engine) pushGeneric(ctx context.Context, cfg *ReplicationConfig, sha256, nodePath string) error {
	u, err := e.targetURL(cfg, nodePath)
	if err != nil {
		return err
	}
	what := fmt.Sprintf("push %s/%s → %s/%s", cfg.SourceRepo, nodePath, cfg.TargetURL, cfg.TargetRepo)

	// Step 1: existence + checksum probe.
	resp, err := e.do(ctx, http.MethodHead, u, cfg, nil, 0, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		sum := resp.Header.Get("X-Checksum-Sha256")
		if sum == "" {
			// No comparable digest served: fall through to the upload, the
			// PUT's own idempotent-retransmit semantics stay safe.
			_ = resp.Body.Close()
		} else {
			_ = resp.Body.Close()
			if strings.EqualFold(sum, sha256) {
				// Idempotent hit: the target already holds these bytes —
				// the property carry still runs (late-tagged properties
				// converge on the re-push).
				return e.syncPropertiesOnce(ctx, cfg, what, nodePath)
			}
			return notRetryable(fmt.Errorf(
				"%s: conflict: target holds sha256 %s at the path, source sha256 is %s; target left untouched (Q7 interim)",
				what, strings.ToLower(sum), sha256))
		}
	case http.StatusNotFound:
		_ = resp.Body.Close()
	default:
		return fmt.Errorf("%s: %w", what, classify(resp, what))
	}

	// Step 2: transfer.
	rc, ref, err := e.blobs.Open(ctx, sha256)
	if err != nil {
		return notRetryable(fmt.Errorf("%s: source blob %s: %w", what, sha256, err))
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	hdr := http.Header{}
	hdr.Set("X-Checksum-Sha256", sha256)
	resp, err = e.do(ctx, http.MethodPut, u, cfg, rc, ref.Size, hdr)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		sum := resp.Header.Get("X-Checksum-Sha256")
		_ = resp.Body.Close()
		if sum != "" && !strings.EqualFold(sum, sha256) {
			return notRetryable(fmt.Errorf("%s: target confirmed sha256 %s, expected %s", what, strings.ToLower(sum), sha256))
		}
		return e.syncPropertiesOnce(ctx, cfg, what, nodePath)
	default:
		return fmt.Errorf("%s: %w", what, classify(resp, what))
	}
}

// propsTargetURL builds the target instance's property-merge address for
// one node path: {TargetURL}/binflow/api/storage/{TargetRepo}/{path} — the
// INSTANCE-level API mount, not the content mount — with the RAW properties
// query pre-rendered (the value grammar below). Same base validation and
// segment-escaping contract as targetURL/planeURL.
func (e *Engine) propsTargetURL(cfg *ReplicationConfig, nodePath string, rawQuery string) (*url.URL, error) {
	base, err := e.targetBase(cfg)
	if err != nil {
		return nil, err
	}
	segments := strings.Split(nodePath, "/")
	for i, seg := range segments {
		if seg == "" || seg == "." || seg == ".." {
			return nil, notRetryable(fmt.Errorf("config %q: node path %q: illegal segment %q", cfg.Name, nodePath, seg))
		}
		segments[i] = url.PathEscape(seg)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + "/binflow/api/storage/" + cfg.TargetRepo + "/" + strings.Join(segments, "/")
	u.RawQuery = rawQuery
	return &u, nil
}

// renderPropsQuery renders one property map as the target's ?properties=
// raw value (the comma grammar the M10 property plane parses): RAW commas
// separate segments, a segment with '=' opens a key, one without continues
// the previous key's value set — so values are percent-encoded (a %2C
// survives as content) and keys ride verbatim (the validated key charset
// has no reserved characters). Keys and values render in sorted order for
// deterministic requests.
func renderPropsQuery(props map[string][]string) string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	segs := make([]string, 0, len(props)*2)
	for _, k := range keys {
		values := append([]string(nil), props[k]...)
		sort.Strings(values)
		segs = append(segs, k+"="+url.QueryEscape(values[0]))
		for _, v := range values[1:] {
			segs = append(segs, url.QueryEscape(v))
		}
	}
	return strings.Join(segs, ",")
}

// syncPropertiesOnce is the property carry of one push attempt (T-317,
// FR-101.2): read the SOURCE node's properties at push time and merge them
// onto the TARGET node. A node without properties is a clean skip (no
// request); a property-plane failure is an ordinary push failure — the
// retry re-enters pushGeneric, the blob arm answers idempotently and the
// property arm gets the attempt budget (the carry folds into the attempt
// loop instead of trailing it as best-effort: a property the operator
// tagged MUST arrive, or the task must say why it did not).
func (e *Engine) syncPropertiesOnce(ctx context.Context, cfg *ReplicationConfig, what, nodePath string) error {
	if e.cfg.meta == nil {
		return nil
	}
	props, err := e.cfg.meta.NodeProps(ctx, cfg.SourceRepo, nodePath)
	if err != nil {
		return fmt.Errorf("%s: source properties: %w", what, err)
	}
	if len(props) == 0 {
		return nil
	}
	u, err := e.propsTargetURL(cfg, nodePath, "properties="+renderPropsQuery(props))
	if err != nil {
		return err
	}
	resp, err := e.do(ctx, http.MethodPut, u, cfg, nil, 0, nil)
	if err != nil {
		return fmt.Errorf("%s: property carry: %w", what, err)
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		_ = resp.Body.Close()
		return nil
	case http.StatusBadRequest, http.StatusConflict:
		// The target rejected the write itself (invalid set, conflicting
		// state): deterministic, not worth the backoff schedule.
		return notRetryable(fmt.Errorf("%s: property carry: %w", what, classify(resp, what)))
	default:
		return fmt.Errorf("%s: property carry: %w", what, classify(resp, what))
	}
}

// CloseIdleConnections releases pooled target connections (shutdown path).
func (e *Engine) CloseIdleConnections() {
	if tr, ok := e.client.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
}
