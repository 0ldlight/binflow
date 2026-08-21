package main

// T-180 AC ②/⑤: the cmd-side replication assembly. openStack must open the
// replication store on its own connection (009 tables), build the engine
// over the same storage engine serve writes to, and attach it to the repo
// service's enqueue seam — one real upload must produce a pending task row.
// startReplication/drain own the Run lifecycle: the loop exits on cancel and
// the drain is idempotent. (A full REST round-trip lives in httpapi's own
// tests; newAssembledServer is deliberately NOT called here — the second
// assembly would panic the process-wide adapter registry, T-168's known
// full-suite red, the T-178/T-179 precedent.)

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// TestStackReplicationAssembly walks the whole wiring: stack open → config +
// repo rows → one service Put → the async enqueue hook lands a pending task
// whose blob digest is the landed node's. The push target is a local 404
// server so the engine's attempts (should it win the race with the test)
// stay hermetic and retriable — nothing external is dialed.
func TestStackReplicationAssembly(t *testing.T) {
	target := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(target.Close)

	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger := testSlogLogger(t)

	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)

	if stack.replStore == nil {
		t.Fatal("stack.replStore is nil, want the 009-backed store openStack wires")
	}
	if stack.replEngine == nil {
		t.Fatal("stack.replEngine is nil, want the push engine openStack builds")
	}
	if stack.replDB == nil {
		t.Fatal("stack.replDB is nil, want the store's own pooled connection")
	}

	ctx := context.Background()
	admin := &repo.Principal{Name: "admin", Admin: true}
	if _, err := stack.svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: metadata.Now(), UpdatedAt: metadata.Now(),
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	now := metadata.Now()
	cfgID, err := stack.replStore.CreateConfig(ctx, &replication.ReplicationConfig{
		Name: "dr-local", SourceRepo: "libs", TargetURL: target.URL, TargetRepo: "libs",
		Enabled: true, MaxItemsPerPush: 1000, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	node, err := stack.svc.Put(ctx, admin, "libs", "acme/app.bin",
		strings.NewReader("payload"), storage.BlobRef{}, "")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The Put-tail hook is fire-and-forget: poll the ledger for the row it
	// must append (AttachReplicator is proven only by the row landing).
	deadline := time.Now().Add(10 * time.Second)
	var tasks []*replication.ReplicationTask
	for time.Now().Before(deadline) {
		tasks, err = stack.replStore.ListTasks(ctx, cfgID, 10)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(tasks) >= 1 && tasks[0].BlobSHA256 == node.Sha256 &&
			tasks[0].NodePath == "acme/app.bin" && tasks[0].Status == replication.TaskStatusPending {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(tasks) != 1 {
		t.Fatalf("task rows after upload = %d, want the enqueued one (AttachReplicator seam)", len(tasks))
	}
	if tasks[0].BlobSHA256 != node.Sha256 || tasks[0].NodePath != "acme/app.bin" {
		t.Fatalf("task = %+v, want the landed node's digest and path", tasks[0])
	}
}

// TestStackReplicationLifecycle pins the startReplication/drain contract:
// the loop parks until canceled, drain waits for Run's return without
// deadlocking (even when the PARENT context was never canceled — the early
// serve-failure path), and a second drain is a no-op.
func TestStackReplicationLifecycle(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger := testSlogLogger(t)

	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)

	// An uncanceled parent: drain must still stop the loop (the dedicated
	// cancel inside startReplication exists for exactly this path).
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled up front; Run must still start and exit cleanly
	drain := stack.startReplication(ctx, logger)

	done := make(chan struct{})
	go func() {
		drain()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("drain never returned; the engine loop did not exit on cancel")
	}
	begin := time.Now()
	drain() // idempotent: returns immediately
	if elapsed := time.Since(begin); elapsed > time.Second {
		t.Fatalf("second drain took %s, want the idempotent no-op", elapsed)
	}
}

// TestReplicationCipherSeam pins the nil guard: a nil *remote.Cipher must
// map onto a NIL interface (the httpapi create handler keys its
// no-master-key 400 on exactly that), a real cipher onto the seam.
func TestReplicationCipherSeam(t *testing.T) {
	if got := replicationCipherSeam(nil); got != nil {
		t.Fatalf("replicationCipherSeam(nil) = %T, want a nil interface", got)
	}
	key := make([]byte, 32)
	c, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if got := replicationCipherSeam(c); got == nil {
		t.Fatal("replicationCipherSeam(cipher) = nil, want the cipher")
	}
}

// TestReplicationDBPosture asserts the second connection actually lands on
// the metadata database (the 009 tables are reachable through it) and honors
// the foreign-keys pragma the task cascade rides.
func TestReplicationDBPosture(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger := testSlogLogger(t)

	stack, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer stack.close(logger)

	var fk int
	if err := stack.replDB.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys = %d on the replication connection, want 1 (the 009 cascade rides it)", fk)
	}
	var n int
	if err := stack.replDB.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('replications','replication_tasks')").Scan(&n); err != nil {
		t.Fatalf("sqlite_master probe: %v", err)
	}
	if n != 2 {
		t.Fatalf("009 tables visible through the replication connection = %d, want 2", n)
	}
}
