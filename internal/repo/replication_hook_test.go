package repo_test

// T-162 AC ①: the Put-tail push-replication hook. A fake Replicator records
// enqueue calls; the assertions pin the contract — file Puts notify with the
// landed sha256 on a detached goroutine (never failing or blocking the
// upload), folder Puts and the unwired (nil) case stay silent.

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// recordingReplicator captures enqueue calls.
type recordingReplicator struct {
	mu    sync.Mutex
	calls []string
	// block, when non-nil, parks every Enqueue until the channel closes:
	// proves the hook never waits on replication.
	block <-chan struct{}
}

func (r *recordingReplicator) Enqueue(_ context.Context, repoKey, path, sha256 string) {
	if r.block != nil {
		<-r.block
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, repoKey+"|"+path+"|"+sha256)
}

func (r *recordingReplicator) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// newHookService builds a minimal real stack (engine + metadata + service)
// with one local repository and the replicator attached.
func newHookService(t *testing.T, repl repo.Replicator) (repo.Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	eng, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: filepath.Join(t.TempDir(), "seed.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: "2026-08-22T00:00:00Z", UpdatedAt: "2026-08-22T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	svc := repo.New(eng, md, nil, nil)
	if repl != nil {
		repo.AttachReplicator(svc, repl)
	}
	return svc, ctx
}

func TestPutTailFiresReplicationHook(t *testing.T) {
	repl := &recordingReplicator{}
	svc, ctx := newHookService(t, repl)

	node, err := svc.Put(ctx, admin(), "generic-local", "acme/app-1.bin",
		strings.NewReader("payload"), storage.BlobRef{}, "")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	want := "generic-local|acme/app-1.bin|" + node.Sha256
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		if got = repl.recorded(); len(got) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("enqueue calls = %v, want [%s]", got, want)
	}
}

// Folder Puts carry no blob and must not replicate.
func TestPutFolderDoesNotFireHook(t *testing.T) {
	repl := &recordingReplicator{}
	svc, ctx := newHookService(t, repl)
	if _, err := svc.Put(ctx, admin(), "generic-local", "acme/folder/",
		strings.NewReader(""), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("folder Put: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := repl.recorded(); len(got) != 0 {
		t.Fatalf("enqueue calls = %v, want none for folder deploys", got)
	}
}

// A replicator that never returns must not delay the upload response: the
// hook is fire-and-forget on its own goroutine (AC ①'s 非阻塞 clause).
func TestPutNeverWaitsOnReplicator(t *testing.T) {
	parked := make(chan struct{})
	repl := &recordingReplicator{block: parked}
	svc, ctx := newHookService(t, repl)

	start := time.Now()
	if _, err := svc.Put(ctx, admin(), "generic-local", "acme/slow-repl.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Put took %s while the replicator was parked; the hook must be non-blocking", elapsed)
	}
	close(parked)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(repl.recorded()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(repl.recorded()) != 1 {
		t.Fatalf("enqueue calls = %v, want the parked call to complete after release", repl.recorded())
	}
}

// No replicator wired (the M1~M5 default): Put works unchanged.
func TestPutWithoutReplicator(t *testing.T) {
	svc, ctx := newHookService(t, nil)
	if _, err := svc.Put(ctx, admin(), "generic-local", "acme/no-repl.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("Put without a replicator: %v", err)
	}
}

// A panicking replicator must never take the process down.
func TestPanickingReplicatorIsContained(t *testing.T) {
	svc, ctx := newHookService(t, panicReplicator{})
	done := make(chan error, 1)
	go func() {
		_, err := svc.Put(ctx, admin(), "generic-local", "acme/panic.bin",
			strings.NewReader("payload"), storage.BlobRef{}, "")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Put with a panicking replicator: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Put never returned from a replicator panic")
	}
	// Give the recovered goroutine a beat; the test process surviving to
	// report IS the assertion.
	time.Sleep(50 * time.Millisecond)
}

type panicReplicator struct{}

func (panicReplicator) Enqueue(context.Context, string, string, string) {
	panic("replicator boom")
}
