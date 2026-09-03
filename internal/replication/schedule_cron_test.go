package replication_test

// The cron-scheduled full-sync lane (M16 T-450, FR-150.4 / ADR-0044
// decision 8③④): a dual-instance fixture with the real scheduler engine
// over the 021 ledger, the domain's ScheduleRunner registered, and the
// config's schedule landed through the REST face — then the three AC
// assertions:
//
//  1. the scheduled fire converges the target (per-path sha256 parity);
//  2. ZERO duplicate delivery at the content layer (the L48 wording):
//     incremental events inside the scheduling window push their bytes
//     exactly once, and the scheduled re-fires of the SAME artifacts
//     transfer nothing (target-side sha256 idempotent hits) — the target's
//     byte-transfer counter and its blob set both stay pinned;
//  3. Replicate Now coexists with the schedule idempotently (200, the
//     pass re-seeds, the counter still does not move).
//
// The target wraps its handler with a counting middleware that counts only
// content PUTs (an X-Checksum-Sha256 header present) — the property-merge
// PUTs and HEAD probes never carry bytes.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/scheduler"
	"github.com/lzwzzy/binflow/internal/storage"
)

// countingTarget is instance B plus the content-transfer counter.
type countingTarget struct {
	*binflow
	puts    atomic.Int64
	srv     *httptest.Server
	dataDir string
}

// newCountingTarget assembles the target instance with the byte-transfer
// middleware wrapped around the real handler (newBinFlow's assembly plus
// the wrapper — the one difference).
func newCountingTarget(t *testing.T, name, adminPw string, repos []*metadata.Repo) *countingTarget {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("%s: storage.OpenEngine: %v", name, err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: filepath.Join(dataDir, "binflow.db"), AdminPassword: adminPw,
	})
	if err != nil {
		t.Fatalf("%s: metadata.Open: %v", name, err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	admin := &repo.Principal{Name: "admin", Admin: true}
	for _, r := range repos {
		r.CreatedAt = metadata.Now()
		r.UpdatedAt = metadata.Now()
		if _, err := svc.CreateRepo(ctx, admin, r); err != nil {
			t.Fatalf("%s: CreateRepo %s: %v", name, r.RepoKey, err)
		}
	}
	srv := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: md,
		Repos: md.Repos(), ReposSvc: svc, Passwords: authSvc, Tokens: authSvc,
		GC: st, DataDir: dataDir,
		Adapters: []adapter.Handler{generic.New(svc, md.Blobs())},
		Version:  "test",
	}, nil)
	ct := &countingTarget{binflow: &binflow{
		t: t, dataDir: dataDir, svc: svc, st: st, md: md, adminPw: adminPw,
	}, dataDir: dataDir}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.Header.Get("X-Checksum-Sha256") != "" {
			ct.puts.Add(1)
		}
		srv.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	ct.srv = ts
	ct.url = ts.URL
	return ct
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// blobFileCount walks the instance's blobs tree counting regular files —
// the target's physical blob set (the content-layer zero-duplicate oracle).
func blobFileCount(t *testing.T, dataDir string) int {
	t.Helper()
	root := filepath.Join(dataDir, "blobs")
	n := 0
	err := filepath.Walk(root, func(_ string, info os.FileInfo, werr error) error {
		if werr != nil {
			return werr
		}
		if info.Mode().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return n
}

// TestScheduledFullSyncTwoInstanceZeroDuplicateDelivery is the L48 lane.
func TestScheduledFullSyncTwoInstanceZeroDuplicateDelivery(t *testing.T) {
	ctx := context.Background()

	b := newCountingTarget(t, "B450", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	a := newT420Source(t, []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})

	// 1. Land three artifacts on A (no config exists yet — the full-sync gap).
	artifacts := map[string][]byte{
		"org/app/1.0/app-1.0.bin": []byte("t450 artifact one"),
		"org/app/1.1/app-1.1.bin": []byte("t450 artifact two"),
		"org/lib/extra.bin":       []byte("t450 artifact three"),
	}
	sums := map[string]string{}
	for path, body := range artifacts {
		sum := sha256Hex(body)
		sums[path] = sum
		resp, _ := a.do(http.MethodPut, "/binflow/libs/"+path, "admin", "pw-source", body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload %s: status %d, want 201", path, resp.StatusCode)
		}
	}

	// 2. Create the config WITH a cron arm through the REST face (the
	// every-second expression keeps the scheduled fires dense for the test).
	cfgBody, _ := json.Marshal(map[string]any{
		"name": "t450-scheduled", "source_repo": "libs", "target_url": b.url,
		"target_repo": "replica-local", "target_username": "admin",
		"target_password": "pw-target", "enabled": true,
		"cron_exp": "* * * * * ?",
	})
	resp, raw := a.do(http.MethodPost, "/binflow/api/v1/replications", "admin", "pw-source", cfgBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create config with cron: status %d body %s, want 201", resp.StatusCode, raw)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("create decode: %v (%s)", err, raw)
	}

	// 3. THE SCHEDULER: the real engine over the same 021 ledger the REST
	// face wrote, the domain's ScheduleRunner registered (the cmd
	// assembly's exact shape), a test-tightened tick.
	sched, err := scheduler.New(scheduler.Options{
		Store:        a.md.Schedules(),
		TickInterval: 25 * time.Millisecond,
		Audit:        audit.New(a.md, true),
	})
	if err != nil {
		t.Fatalf("scheduler.New: %v", err)
	}
	if err := sched.Register(scheduler.DomainReplication,
		replication.NewScheduleRunner(a.store, a.md.Schedules(), a.engine)); err != nil {
		t.Fatalf("Register replication: %v", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		_ = sched.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-schedDone:
		case <-time.After(10 * time.Second):
			t.Error("scheduler Run did not return after cancel")
		}
	})

	// 4. The scheduled fire converges the target: per-path sha256 parity.
	waitFor(t, "the scheduled full sync to converge the target", func() bool {
		return len(repoFileSums(t, b.md, "replica-local")) == 3
	})
	target := repoFileSums(t, b.md, "replica-local")
	for path, sum := range sums {
		if target[path] != sum {
			t.Errorf("target %s = %s, want the source sha %s", path, target[path], sum)
		}
	}
	// Content-layer accounting: exactly three byte transfers so far (the
	// scheduled fire transferred each artifact ONCE).
	if got := b.puts.Load(); got != 3 {
		t.Fatalf("content PUTs after the first scheduled fire = %d, want 3", got)
	}

	// 5. The schedule's own state and word: last_run landed ok, the
	// replication.schedule.run audit row carries actor "scheduler".
	waitFor(t, "the schedule row to record its run", func() bool {
		row, gerr := a.md.Schedules().Get(ctx, "replication", fmt.Sprintf("%d", created.ID))
		return gerr == nil && row.LastRunAt != "" && row.LastStatus == "ok"
	})
	waitFor(t, "the replication.schedule.run audit word", func() bool {
		events, qerr := a.md.Audits().Query(ctx, metadata.AuditQuery{Action: "replication.schedule.run", Limit: 10})
		return qerr == nil && len(events) >= 1 && events[0].Actor == "scheduler"
	})

	// 6. THE L48 ASSERTION — an incremental event inside the scheduling
	// window does not double-deliver: a fourth artifact uploaded now rides
	// the EVENT track (one transfer), while the scheduled fires keep
	// re-seeding the whole repository and transfer NOTHING (every path is
	// already at its sha on the target).
	fourth := []byte("t450 artifact four")
	sums["org/new/late.bin"] = sha256Hex(fourth)
	resp, _ = a.do(http.MethodPut, "/binflow/libs/org/new/late.bin", "admin", "pw-source", fourth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload the in-window artifact: status %d, want 201", resp.StatusCode)
	}
	waitFor(t, "the in-window artifact to ride the event track", func() bool {
		target := repoFileSums(t, b.md, "replica-local")
		return len(target) == 4 && target["org/new/late.bin"] == sums["org/new/late.bin"]
	})
	// Let at least one more scheduled fire re-seed the FULL repository
	// (now four paths) after the incremental landed.
	prevFires := scheduledRunCount(t, a)
	waitFor(t, "a further scheduled fire over the four-path repository", func() bool {
		return scheduledRunCount(t, a) >= prevFires+1
	})
	if got := b.puts.Load(); got != 4 {
		t.Fatalf("content PUTs after the in-window event + a further scheduled fire = %d, want 4 (zero duplicate delivery)", got)
	}
	if got := blobFileCount(t, b.dataDir); got != 4 {
		t.Fatalf("target blob files = %d, want 4 (the re-fires transferred no bytes)", got)
	}

	// 7. Replicate Now coexists with the schedule idempotently: the manual
	// trigger answers 200, the pass re-seeds every path, the target still
	// receives nothing new.
	resp, raw = a.do(http.MethodPost,
		fmt.Sprintf("/binflow/api/v1/replications/%d/run", created.ID), "admin", "pw-source", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Replicate Now alongside the schedule: status %d body %s, want 200", resp.StatusCode, raw)
	}
	var runBody struct {
		Scheduled int64 `json:"scheduled"`
	}
	if err := json.Unmarshal(raw, &runBody); err != nil || runBody.Scheduled != 4 {
		t.Fatalf("Replicate Now body = %s (%v), want the four re-seeded paths", raw, err)
	}
	waitFor(t, "the manual pass to drain", func() bool {
		target := repoFileSums(t, b.md, "replica-local")
		return len(target) == 4
	})
	if got := b.puts.Load(); got != 4 {
		t.Fatalf("content PUTs after Replicate Now = %d, want 4 (idempotent coexistence)", got)
	}
	if got := blobFileCount(t, b.dataDir); got != 4 {
		t.Fatalf("target blob files after Replicate Now = %d, want 4", got)
	}
}

// scheduledRunCount reads the replication.schedule.run audit count (the
// fires observed so far).
func scheduledRunCount(t *testing.T, a *t420Source) int {
	t.Helper()
	events, err := a.md.Audits().Query(context.Background(),
		metadata.AuditQuery{Action: "replication.schedule.run", Limit: 500})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return len(events)
}
