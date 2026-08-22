package replication_test

// T-162 unit tests: the push engine against the real 009 store (via
// model_test.go's openStore) and scripted httptest targets. The sleep seam
// records backoff delays instead of waiting, so the 1s→2s→4s→8s→16s schedule
// is asserted on values, not wall time.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- fakes ----

// fakeBlobs is the BlobSource stand-in: fixed content per sha256.
type fakeBlobs struct {
	mu      sync.Mutex
	opens   []string
	content map[string]string
	missing map[string]bool
}

func (f *fakeBlobs) Open(_ context.Context, sha string) (io.ReadCloser, storage.BlobRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens = append(f.opens, sha)
	if f.missing[sha] {
		return nil, storage.BlobRef{}, fmt.Errorf("blob %s: %w", sha, storage.ErrBlobNotFound)
	}
	body := f.content[sha]
	return io.NopCloser(strings.NewReader(body)), storage.BlobRef{Sha256: sha, Size: int64(len(body))}, nil
}

// scriptTarget is a programmable push target speaking the REST upload
// contract's slice the engine uses: HEAD (existence + X-Checksum-Sha256) and
// PUT (X-Checksum-Sha256 declared, 201 created).
type scriptTarget struct {
	mu sync.Mutex
	// HEAD answers headStatus (default 404); a 200 serves headSum.
	headStatus int
	headSum    string
	// PUT answers the putStatuses sequence (default a single 201); the last
	// entry repeats once the script is exhausted.
	putStatuses []int
	// putSum overrides the checksum echoed on PUT responses; "" echoes the
	// request's declared checksum.
	putSum string

	headCalls int
	putCalls  int
	lastBody  string
	lastAuth  string
	lastSum   string
}

func (s *scriptTarget) handler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case http.MethodHead:
		s.headCalls++
		status := s.headStatus
		if status == 0 {
			status = http.StatusNotFound // zero value = "not present"
		}
		if status == http.StatusOK {
			w.Header().Set("X-Checksum-Sha256", s.headSum)
		}
		w.WriteHeader(status)
	case http.MethodPut:
		s.putCalls++
		body, _ := io.ReadAll(r.Body)
		s.lastBody = string(body)
		s.lastAuth = r.Header.Get("Authorization")
		s.lastSum = r.Header.Get("X-Checksum-Sha256")
		status := http.StatusCreated
		if len(s.putStatuses) > 0 {
			idx := s.putCalls - 1
			if idx >= len(s.putStatuses) {
				idx = len(s.putStatuses) - 1
			}
			status = s.putStatuses[idx]
		}
		sum := s.putSum
		if sum == "" {
			sum = s.lastSum
		}
		w.Header().Set("X-Checksum-Sha256", sum)
		w.WriteHeader(status)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *scriptTarget) snapshot() (heads, puts int, body, auth, sum string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headCalls, s.putCalls, s.lastBody, s.lastAuth, s.lastSum
}

// sleepRecorder captures backoff delays (the test seam for AC ②).
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *sleepRecorder) sleep(_ context.Context, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delays = append(s.delays, d)
	return nil
}

func (s *sleepRecorder) recorded() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.delays...)
}

// auditSink collects the engine's audit events.
type auditSink struct {
	mu     sync.Mutex
	events []audit.Event
}

func (a *auditSink) Append(_ context.Context, e audit.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
	return nil
}

func (a *auditSink) collected() []audit.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]audit.Event(nil), a.events...)
}

// ---- helpers ----

const testPayload = "binflow replication payload"

func testSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// engineFixture wires an engine over the real 009 store plus a scripted
// target; Run is started in its own goroutine and stopped on cleanup.
type engineFixture struct {
	ctx    context.Context
	cancel context.CancelFunc
	store  replication.Store
	blobs  *fakeBlobs
	target *scriptTarget
	server *httptest.Server
	sleep  *sleepRecorder
	audit  *auditSink
	engine *replication.Engine
}

// newEngineFixture builds the stack; cfgMutate adjusts the single config row
// created for the fixture (target URL overridden onto the scripted server).
func newEngineFixture(t *testing.T, target *scriptTarget, opts *replication.EngineOptions, cfgMutate func(*replication.ReplicationConfig)) *engineFixture {
	t.Helper()
	ctx := context.Background()
	_, store := openStore(t)

	server := httptest.NewServer(http.HandlerFunc(target.handler))
	t.Cleanup(server.Close)

	fake := &fakeBlobs{content: map[string]string{testSHA256(testPayload): testPayload}}
	sleep := &sleepRecorder{}
	sink := &auditSink{}
	engineOpts := replication.EngineOptions{Sleep: sleep.sleep, Audit: sink}
	if opts != nil {
		engineOpts = *opts
		if engineOpts.Sleep == nil {
			engineOpts.Sleep = sleep.sleep
		}
		if engineOpts.Audit == nil {
			engineOpts.Audit = sink
		}
	}
	eng, err := replication.NewEngine(store, fake, engineOpts)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	cfg := fixtureConfig("dr-test")
	cfg.TargetURL = server.URL
	cfg.SourceRepo = "libs-local"
	cfg.TargetRepo = "mirror"
	cfg.TargetPasswordEnc = ""
	if cfgMutate != nil {
		cfgMutate(cfg)
	}
	if _, err := store.CreateConfig(ctx, cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = eng.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("engine Run did not return after cancel")
		}
		eng.CloseIdleConnections()
	})
	return &engineFixture{
		ctx: ctx, cancel: cancel, store: store, blobs: fake, target: target,
		server: server, sleep: sleep, audit: sink, engine: eng,
	}
}

// waitTask polls the config's newest tasks until pred holds on one of them.
func (f *engineFixture) waitTask(t *testing.T, pred func(*replication.ReplicationTask) bool, what string) *replication.ReplicationTask {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := f.store.ListTasks(f.ctx, 1, 10)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		for _, task := range tasks {
			if pred(task) {
				return task
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a task that is %s", what)
	return nil
}

// status returns the newest task's row (list index 0, newest first).
func (f *engineFixture) newestTask(t *testing.T) *replication.ReplicationTask {
	t.Helper()
	tasks, err := f.store.ListTasks(f.ctx, 1, 10)
	if err != nil || len(tasks) == 0 {
		return nil
	}
	return tasks[0]
}

// ---- enqueue fan-out (AC ①, config filtering) ----

func TestEnqueueFanOut(t *testing.T) {
	sha := testSHA256(testPayload)
	cases := []struct {
		name     string
		mutate   func(*replication.ReplicationConfig)
		enqueue  string // repoKey the hook announces
		wantNew  int
		wantPath bool
	}{
		{name: "enabled matching config enqueues", enqueue: "libs-local", wantNew: 1, wantPath: true},
		{name: "disabled config does not enqueue", mutate: func(c *replication.ReplicationConfig) { c.Enabled = false }, enqueue: "libs-local", wantNew: 0},
		{name: "upload in another repo does not enqueue", enqueue: "other-repo", wantNew: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEngineFixture(t, &scriptTarget{}, nil, tc.mutate)
			f.engine.Enqueue(f.ctx, tc.enqueue, "org/app/1.bin", sha)
			if tc.wantPath {
				f.waitTask(t, func(task *replication.ReplicationTask) bool {
					return task.NodePath == "org/app/1.bin"
				}, "enqueued for org/app/1.bin")
			} else {
				// No wake fires; give the (idle) engine a moment and assert
				// nothing appeared.
				time.Sleep(50 * time.Millisecond)
			}
			tasks, err := f.store.ListTasks(f.ctx, 1, 10)
			if err != nil {
				t.Fatalf("ListTasks: %v", err)
			}
			if len(tasks) != tc.wantNew {
				t.Fatalf("tasks = %d, want %d", len(tasks), tc.wantNew)
			}
			if tc.wantNew > 0 {
				got := tasks[0]
				if got.Status != replication.TaskStatusPending && got.Status != replication.TaskStatusInProgress &&
					got.Status != replication.TaskStatusSuccess {
					t.Fatalf("status = %q, want a runnable/success status", got.Status)
				}
				if got.BlobSHA256 != sha {
					t.Fatalf("BlobSHA256 = %q, want %q", got.BlobSHA256, sha)
				}
			}
		})
	}
}

// Empty arguments are dropped silently (the hook's folder-node guard makes
// this unreachable in production; Enqueue stays defensive on its own).
func TestEnqueueDropsEmptyArguments(t *testing.T) {
	f := newEngineFixture(t, &scriptTarget{}, nil, nil)
	for _, args := range [][3]string{{"", "p", "s"}, {"r", "", "s"}, {"r", "p", ""}} {
		f.engine.Enqueue(f.ctx, args[0], args[1], args[2])
	}
	time.Sleep(50 * time.Millisecond)
	if tasks := f.newestTask(t); tasks != nil {
		t.Fatalf("no task expected for empty arguments, got %+v", tasks)
	}
}

// ---- happy path: transfer through the REST upload surface ----

func TestPushHappyPath(t *testing.T) {
	sha := testSHA256(testPayload)
	key := make([]byte, 32)
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	enc, err := cipher.Encrypt("target-secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	f := newEngineFixture(t, &scriptTarget{}, &replication.EngineOptions{Cipher: cipher},
		func(c *replication.ReplicationConfig) {
			c.TargetUsername = "repl"
			c.TargetPasswordEnc = enc
		})
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)

	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")

	if task.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", task.Attempts)
	}
	if task.CompletedAt == "" {
		t.Fatal("CompletedAt empty on success")
	}
	heads, puts, body, auth, sum := f.target.snapshot()
	if heads != 1 || puts != 1 {
		t.Fatalf("target calls = %d HEAD, %d PUT; want 1/1", heads, puts)
	}
	if body != testPayload {
		t.Fatalf("pushed body = %q, want %q", body, testPayload)
	}
	if sum != sha {
		t.Fatalf("declared X-Checksum-Sha256 = %q, want %q", sum, sha)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("repl:target-secret"))
	if auth != wantAuth {
		t.Fatalf("Authorization = %q, want the decrypted Basic credential", auth)
	}
	events := f.audit.collected()
	if len(events) != 1 || events[0].Action != replication.AuditActionPush {
		t.Fatalf("audit events = %+v, want one replication.push", events)
	}
	if events[0].Repo != "libs-local" || events[0].Path != "org/app/1.bin" {
		t.Fatalf("audit event repo/path = %q/%q", events[0].Repo, events[0].Path)
	}
}

// ---- idempotency: same checksum on target = success without transfer ----

func TestPushIdempotentHit(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{headStatus: http.StatusOK, headSum: sha}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")
	if heads, puts, _, _, _ := f.target.snapshot(); heads != 1 || puts != 0 {
		t.Fatalf("target calls = %d HEAD, %d PUT; want 1/0 (idempotent hit, no transfer)", heads, puts)
	}
	if opens := len(f.blobs.opens); opens != 0 {
		t.Fatalf("source blob opened %d times, want 0", opens)
	}
}

// ---- Q7 interim conflict: different checksum on target ----

func TestPushConflictIsTerminal(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{headStatus: http.StatusOK, headSum: testSHA256("other content")}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "failed (conflict)")
	if heads, puts, _, _, _ := f.target.snapshot(); heads != 1 || puts != 0 {
		t.Fatalf("target calls = %d HEAD, %d PUT; want 1/0 (target untouched)", heads, puts)
	}
	if task.Attempts != 6 {
		t.Fatalf("Attempts = %d, want the cap 6 (not retryable)", task.Attempts)
	}
	if !strings.Contains(task.LastError, "conflict") {
		t.Fatalf("LastError = %q, want a conflict diagnosis", task.LastError)
	}
	if task.CompletedAt == "" {
		t.Fatal("CompletedAt empty on terminal failure")
	}
	if delays := f.sleep.recorded(); len(delays) != 0 {
		t.Fatalf("backoff delays = %v, want none for a terminal failure", delays)
	}
}

// ---- AC ②: exponential backoff, five retries, then terminal ----

func TestRetryBackoffSchedule(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{putStatuses: []int{http.StatusInternalServerError}}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "failed after exhausting retries")

	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	delays := f.sleep.recorded()
	if len(delays) != len(want) {
		t.Fatalf("backoff delays = %v, want %v", delays, want)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Fatalf("backoff delays = %v, want %v", delays, want)
		}
	}
	if _, puts, _, _, _ := f.target.snapshot(); puts != 6 {
		t.Fatalf("PUT attempts = %d, want 6 (initial + five retries)", puts)
	}
	if task.Attempts != 6 || task.CompletedAt == "" {
		t.Fatalf("task attempts/completed = %d/%q, want 6/set", task.Attempts, task.CompletedAt)
	}
	if !strings.Contains(task.LastError, "500") {
		t.Fatalf("LastError = %q, want the target status in the diagnosis", task.LastError)
	}
	events := f.audit.collected()
	if len(events) != 1 || events[0].Action != replication.AuditActionPushFailed {
		t.Fatalf("audit events = %+v, want one replication.push.failed", events)
	}
}

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{putStatuses: []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusCreated}}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success on the third attempt")
	if task.Attempts != 3 {
		t.Fatalf("Attempts = %d, want 3", task.Attempts)
	}
	want := []time.Duration{1 * time.Second, 2 * time.Second}
	if delays := f.sleep.recorded(); len(delays) != 2 || delays[0] != want[0] || delays[1] != want[1] {
		t.Fatalf("backoff delays = %v, want %v", delays, want)
	}
}

// 409 (declared checksum mismatch / refused notation) is deterministic: no
// backoff is burned on it.
func TestDeclaredChecksumMismatchIsTerminal(t *testing.T) {
	sha := testSHA256(testPayload)
	f := newEngineFixture(t, &scriptTarget{putStatuses: []int{http.StatusConflict}}, nil, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "failed (409 terminal)")
	if _, puts, _, _, _ := f.target.snapshot(); puts != 1 {
		t.Fatalf("PUT attempts = %d, want 1", puts)
	}
	if delays := f.sleep.recorded(); len(delays) != 0 {
		t.Fatalf("backoff delays = %v, want none", delays)
	}
	if task.Attempts != 6 {
		t.Fatalf("Attempts = %d, want cap 6", task.Attempts)
	}
}

// ---- cron fallback (sweep) and crash residue ----

func TestCronSweepProcessesResidue(t *testing.T) {
	sha := testSHA256(testPayload)
	cases := []struct {
		name    string
		seed    func(t *testing.T, f *engineFixture, cfgID int64)
		wantPut int
	}{
		{
			name: "pending task from a crashed process",
			seed: func(t *testing.T, f *engineFixture, cfgID int64) {
				if _, err := f.store.CreateTask(f.ctx, &replication.ReplicationTask{
					ReplicationID: cfgID, BlobSHA256: sha, NodePath: "crash/residue.bin",
					Status: replication.TaskStatusPending, CreatedAt: "2026-08-22T00:00:00Z",
				}); err != nil {
					t.Fatalf("CreateTask: %v", err)
				}
			},
			wantPut: 1,
		},
		{
			name: "failed task below the attempt cap is retried",
			seed: func(t *testing.T, f *engineFixture, cfgID int64) {
				if _, err := f.store.CreateTask(f.ctx, &replication.ReplicationTask{
					ReplicationID: cfgID, BlobSHA256: sha, NodePath: "retry/me.bin",
					Status: replication.TaskStatusFailed, Attempts: 2,
					LastError: "earlier outage", CreatedAt: "2026-08-22T00:00:00Z",
				}); err != nil {
					t.Fatalf("CreateTask: %v", err)
				}
			},
			wantPut: 1,
		},
		{
			// T-195 D1: only DETERMINISTIC failures stay terminal — the
			// not-retryable marker in last_error is what the revival scan
			// keys on (an exhausted TRANSIENT failure is revival fodder, see
			// TestCronRevivalAfterBackoffBurnout).
			name: "not-retryable task stays terminal",
			seed: func(t *testing.T, f *engineFixture, cfgID int64) {
				if _, err := f.store.CreateTask(f.ctx, &replication.ReplicationTask{
					ReplicationID: cfgID, BlobSHA256: sha, NodePath: "leave/me.bin",
					Status: replication.TaskStatusFailed, Attempts: 6,
					LastError:   "replication: failure is not retryable: conflict: target holds other bytes",
					CompletedAt: "2020-08-22T00:01:00Z", // aged far past any delay
					CreatedAt:   "2020-08-22T00:00:00Z",
				}); err != nil {
					t.Fatalf("CreateTask: %v", err)
				}
			},
			wantPut: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEngineFixture(t, &scriptTarget{}, &replication.EngineOptions{SweepInterval: 30 * time.Millisecond}, nil)
			configs, err := f.store.ListConfigs(f.ctx)
			if err != nil || len(configs) != 1 {
				t.Fatalf("ListConfigs: %v (%d)", err, len(configs))
			}
			tc.seed(t, f, configs[0].ID)

			if tc.wantPut > 0 {
				f.waitTask(t, func(task *replication.ReplicationTask) bool {
					return task.Status == replication.TaskStatusSuccess
				}, "success via sweep")
				if _, puts, _, _, _ := f.target.snapshot(); puts != tc.wantPut {
					t.Fatalf("PUT calls = %d, want %d", puts, tc.wantPut)
				}
			} else {
				// Give the sweep two ticks, then assert nothing moved.
				time.Sleep(100 * time.Millisecond)
				if _, puts, _, _, _ := f.target.snapshot(); puts != 0 {
					t.Fatalf("PUT calls = %d, want 0 (terminal task untouched)", puts)
				}
				task := f.newestTask(t)
				if task == nil || task.Status != replication.TaskStatusFailed || task.Attempts != 6 {
					t.Fatalf("task = %+v, want untouched failed/attempts=6", task)
				}
			}
		})
	}
}

// ---- D1 (T-195): revival of backoff-exhausted tasks by the cron sweep ----

// TestCronRevivalAfterBackoffBurnout pins the D1 acceptance shape: a dead
// target burns the whole backoff schedule (six attempts, task terminal
// failed), the target recovers, and the cron sweep hands the task ONE more
// attempt per pass until it lands — the same task row, the seventh attempt.
// The burnout's terminal state is proven by the AUDIT LEDGER (one
// push.failed before the revived push), not by polling the row: the
// failed→success transition can complete between two poll ticks because
// completed_at's RFC3339 second granularity admits a revive up to one
// second early (harmless for a rate limiter, see revivable).
func TestCronRevivalAfterBackoffBurnout(t *testing.T) {
	sha := testSHA256(testPayload)
	// Six 500s burn the backoff cycle; the seventh PUT (the first revived
	// attempt) succeeds — the "target recovered" flip.
	f := newEngineFixture(t,
		&scriptTarget{putStatuses: []int{
			http.StatusInternalServerError, http.StatusInternalServerError,
			http.StatusInternalServerError, http.StatusInternalServerError,
			http.StatusInternalServerError, http.StatusInternalServerError,
			http.StatusCreated,
		}},
		&replication.EngineOptions{
			SweepInterval: 30 * time.Millisecond,
			ReviveDelay:   400 * time.Millisecond,
		}, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)

	// The task converges: the SAME row reaches success on the seventh
	// attempt (six cycle attempts + one revival).
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess && task.Attempts == 7
	}, "success via cron revival")
	if task.CompletedAt == "" {
		t.Fatal("CompletedAt empty on success")
	}
	if _, puts, _, _, _ := f.target.snapshot(); puts != 7 {
		t.Fatalf("PUT calls = %d, want 7 (six cycle attempts + one revival)", puts)
	}
	// The burnout terminal state happened first: exactly one failed-audit
	// event (the cycle's end) followed by one success event (the revived
	// push). A task that never burned out would show a single success.
	events := f.audit.collected()
	if len(events) != 2 ||
		events[0].Action != replication.AuditActionPushFailed ||
		events[1].Action != replication.AuditActionPush {
		t.Fatalf("audit events = %v, want push.failed (burnout) then push (revival)", events)
	}
}

// TestCronRevivalGates walks the revival eligibility rules.
func TestCronRevivalGates(t *testing.T) {
	sha := testSHA256(testPayload)
	stamp := func() string { return time.Now().UTC().Add(-time.Hour).Format(time.RFC3339) }
	cases := []struct {
		name    string
		opts    *replication.EngineOptions
		task    *replication.ReplicationTask
		wantPut int
	}{
		{
			// The not-retryable marker keeps deterministic faults terminal
			// however old they are (conflicts must not churn).
			name: "not-retryable marker blocks revival",
			opts: &replication.EngineOptions{SweepInterval: 20 * time.Millisecond, ReviveDelay: time.Millisecond},
			task: &replication.ReplicationTask{BlobSHA256: sha, NodePath: "a.bin",
				Status: replication.TaskStatusFailed, Attempts: 6,
				LastError: "replication: failure is not retryable: conflict", CompletedAt: stamp()},
			wantPut: 0,
		},
		{
			// Aged transient failure + ReviveDelay not yet elapsed: no.
			name: "unaged exhausted task waits out the delay",
			opts: &replication.EngineOptions{SweepInterval: 20 * time.Millisecond, ReviveDelay: time.Hour},
			task: &replication.ReplicationTask{BlobSHA256: sha, NodePath: "b.bin",
				Status: replication.TaskStatusFailed, Attempts: 6,
				LastError: "connection refused", CompletedAt: time.Now().UTC().Format(time.RFC3339)},
			wantPut: 0,
		},
		{
			// A positive MaxRevives is a hard budget: two extra attempts,
			// then terminal for good.
			name: "bounded budget exhausts",
			opts: &replication.EngineOptions{SweepInterval: 20 * time.Millisecond, ReviveDelay: time.Millisecond, MaxRevives: 2},
			task: &replication.ReplicationTask{BlobSHA256: sha, NodePath: "c.bin",
				Status: replication.TaskStatusFailed, Attempts: 6,
				LastError: "connection refused", CompletedAt: stamp()},
			wantPut: 2,
		},
		{
			// A negative MaxRevives disables revival entirely (the T-162
			// posture remains selectable).
			name: "negative option disables revival",
			opts: &replication.EngineOptions{SweepInterval: 20 * time.Millisecond, ReviveDelay: time.Millisecond, MaxRevives: -1},
			task: &replication.ReplicationTask{BlobSHA256: sha, NodePath: "d.bin",
				Status: replication.TaskStatusFailed, Attempts: 6,
				LastError: "connection refused", CompletedAt: stamp()},
			wantPut: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newEngineFixture(t, &scriptTarget{putStatuses: []int{http.StatusInternalServerError}}, tc.opts, nil)
			tc.task.ReplicationID = 1
			tc.task.CreatedAt = "2026-08-22T00:00:00Z"
			if _, err := f.store.CreateTask(f.ctx, tc.task); err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			if tc.wantPut > 0 {
				// Wait until the budget is spent, then assert it held.
				f.waitTask(t, func(task *replication.ReplicationTask) bool {
					return task.Attempts >= 6+int64(tc.wantPut)
				}, "revival budget spent")
			}
			// Give the sweep several more ticks; the count must not move.
			time.Sleep(120 * time.Millisecond)
			_, puts, _, _, _ := f.target.snapshot()
			if puts != tc.wantPut {
				t.Fatalf("PUT calls = %d, want %d", puts, tc.wantPut)
			}
			task := f.newestTask(t)
			if task == nil || task.Status != replication.TaskStatusFailed {
				t.Fatalf("task = %+v, want terminal failed", task)
			}
		})
	}
}

// TestNotRetryableMarkerComposition pins the D1 marker contract: the
// sentinel's own message carries the substring the revival scan matches on,
// so every not-retryable wrap (the engine's "%w: %w" composition, unchanged
// since T-162) persists a classifiable last_error.
func TestNotRetryableMarkerComposition(t *testing.T) {
	if !strings.Contains(replication.ErrNotRetryable.Error(), "not retryable") {
		t.Fatalf("sentinel text %q lacks the revival marker", replication.ErrNotRetryable.Error())
	}
	inner := errors.New("conflict: target holds other bytes")
	wrapped := fmt.Errorf("push a/b: %w", fmt.Errorf("%w: %w", replication.ErrNotRetryable, inner))
	if !strings.Contains(wrapped.Error(), "not retryable") {
		t.Fatalf("wrapped text %q lacks the marker", wrapped.Error())
	}
	if !errors.Is(wrapped, replication.ErrNotRetryable) {
		t.Fatal("wrapped error lost the sentinel chain")
	}
}

// ---- shutdown: in-flight tasks revert to pending, Run returns ----

func TestShutdownRevertsInFlightTask(t *testing.T) {
	sha := testSHA256(testPayload)
	// The sleep seam blocks until the engine context is canceled: the task
	// sits between attempt 1 and its retry when shutdown hits.
	slept := make(chan struct{})
	f := newEngineFixture(t, &scriptTarget{putStatuses: []int{http.StatusInternalServerError}}, &replication.EngineOptions{
		Sleep: func(ctx context.Context, _ time.Duration) error {
			close(slept)
			<-ctx.Done()
			return ctx.Err()
		},
	}, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)

	select {
	case <-slept:
	case <-time.After(10 * time.Second):
		t.Fatal("engine never reached the backoff sleep")
	}
	f.cancel()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if task := f.newestTask(t); task != nil && task.Status == replication.TaskStatusPending {
			if task.Attempts != 1 {
				t.Fatalf("Attempts = %d, want the interrupted attempt counted (1)", task.Attempts)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task never reverted to pending after shutdown")
}

// ---- configuration faults: terminal, no packets leave ----

func TestBadTargetConfigurationIsTerminal(t *testing.T) {
	sha := testSHA256(testPayload)
	cases := []struct {
		name   string
		mutate func(*replication.ReplicationConfig)
		path   string
	}{
		{
			name:   "non-http scheme",
			mutate: func(c *replication.ReplicationConfig) { c.TargetURL = "ftp://target.example.com" },
			path:   "a/b.bin",
		},
		{
			name: "empty target repo",
			mutate: func(c *replication.ReplicationConfig) {
				c.TargetURL = "http://127.0.0.1:1"
				c.TargetRepo = ""
			},
			path: "a/b.bin",
		},
		{
			name: "dot segment in node path (defense in depth)",
			mutate: func(c *replication.ReplicationConfig) {
				c.TargetURL = "http://127.0.0.1:1"
			},
			path: "../etc/passwd",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Short sweep: the seeded task arrives with no Enqueue, so the
			// cron fallback (not a wake) must pick it up.
			f := newEngineFixture(t, &scriptTarget{}, &replication.EngineOptions{SweepInterval: 30 * time.Millisecond}, tc.mutate)
			if _, err := f.store.CreateTask(f.ctx, &replication.ReplicationTask{
				ReplicationID: 1, BlobSHA256: sha, NodePath: tc.path,
				Status: replication.TaskStatusPending, CreatedAt: "2026-08-22T00:00:00Z",
			}); err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
				return task.Status == replication.TaskStatusFailed
			}, "terminal failure")
			if heads, puts, _, _, _ := f.target.snapshot(); heads+puts != 0 {
				t.Fatalf("scripted target saw %d HEAD + %d PUT, want 0", heads, puts)
			}
			if task.Attempts != 6 {
				t.Fatalf("Attempts = %d, want cap 6 (not retryable)", task.Attempts)
			}
			if delays := f.sleep.recorded(); len(delays) != 0 {
				t.Fatalf("backoff delays = %v, want none", delays)
			}
		})
	}
}

// An encrypted password without a configured master key fails terminally
// (ADR-0012 fail-fast posture) and never leaks into the task row.
func TestEncryptedPasswordWithoutCipherIsTerminal(t *testing.T) {
	sha := testSHA256(testPayload)
	key := make([]byte, 32)
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	enc, err := cipher.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	f := newEngineFixture(t, &scriptTarget{}, nil, func(c *replication.ReplicationConfig) {
		c.TargetUsername = "repl"
		c.TargetPasswordEnc = enc
	})
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "terminal failure (no master key)")
	if heads, puts, _, auth, _ := f.target.snapshot(); heads+puts != 0 || auth != "" {
		t.Fatalf("target saw calls with auth %q, want none", auth)
	}
	if strings.Contains(task.LastError, "secret") {
		t.Fatalf("LastError leaks the credential: %q", task.LastError)
	}
	if !strings.Contains(task.LastError, "BINFLOW_REMOTE_CREDENTIALS_KEY") {
		t.Fatalf("LastError = %q, want the master-key diagnosis", task.LastError)
	}
}

// A source blob that vanished (ledger says replicate, filestore says no) is
// terminal, not worth a backoff cycle.
func TestMissingSourceBlobIsTerminal(t *testing.T) {
	sha := testSHA256("never landed")
	f := newEngineFixture(t, &scriptTarget{}, nil, nil)
	f.blobs.missing = map[string]bool{sha: true}
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", sha)
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "terminal failure (source blob missing)")
	if _, puts, _, _, _ := f.target.snapshot(); puts != 0 {
		t.Fatalf("PUT calls = %d, want 0", puts)
	}
	if task.Attempts != 6 || len(f.sleep.recorded()) != 0 {
		t.Fatalf("attempts = %d, delays = %v; want terminal without backoff", task.Attempts, f.sleep.recorded())
	}
}
