package replication_test

// T-162 AC ③: the two-instance integration scenario. Both BinFlow instances
// are real in-process stacks (storage engine + sqlite metadata + auth +
// repo.Service + generic adapter + httpapi router); only the listeners are
// httptest. Source A takes a client upload through its REST surface (which
// exercises the Put-tail hook), the engine pushes into target B's replica
// surface, and B's read-only replica view answers GET 200 with the same
// sha256 while PUT/DELETE are 405.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"

	_ "modernc.org/sqlite" // driver for the replication store's own connection
)

// binflow is one fully assembled BinFlow instance behind an httptest
// listener.
type binflow struct {
	t       *testing.T
	url     string
	dataDir string
	svc     repo.Service
	st      storage.Engine
	md      metadata.Store
	adminPw string
}

// newBinFlow assembles one instance. Each repo spec is created through the
// service so virtual member ledgers and validations run for real.
func newBinFlow(t *testing.T, name, adminPw string, repos []*metadata.Repo) *binflow {
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
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		GC:        st,
		DataDir:   dataDir,
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs())},
		Version:   "test",
	}, nil)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return &binflow{t: t, url: ts.URL, dataDir: dataDir, svc: svc, st: st, md: md, adminPw: adminPw}
}

// do issues one request against the instance; user != "" adds Basic auth.
func (b *binflow) do(method, path, user, pw string, body []byte) (*http.Response, []byte) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.url+path, rdr)
	if err != nil {
		b.t.Fatalf("build %s %s: %v", method, path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pw)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// replicationStore opens the replication store on the instance's metadata
// database (a second connection; the stack's own store stays open — the
// same posture the cmd wiring will have).
func (b *binflow) replicationStore(t *testing.T) replication.Store {
	t.Helper()
	dsn := "file:" + url.PathEscape(filepath.Join(b.dataDir, "binflow.db")) +
		"?_pragma=foreign_keys(1)&_pragma=busy_timeout(15000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return replication.NewSQLiteStore(db)
}

// waitForTask polls the config's task list until pred holds.
func waitForTask(t *testing.T, store replication.Store, cfgID int64, pred func(*replication.ReplicationTask) bool, what string) *replication.ReplicationTask {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := store.ListTasks(context.Background(), cfgID, 10)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		for _, task := range tasks {
			if pred(task) {
				return task
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a task that is %s", what)
	return nil
}

// TestTwoInstancePushReplication is AC ③ end to end: upload on A → engine
// push → B serves the same bytes under the read-only replica view.
func TestTwoInstancePushReplication(t *testing.T) {
	ctx := context.Background()

	// Target B: a local backing repository receiving the pushes, fronted by
	// an un-routed virtual repository — BinFlow's read-only aggregate view.
	// This is the Q6 interim construct (a dedicated replica repository class
	// awaits the user ruling): the replica FACE is read-only (PUT/DELETE
	// 405), the push lands on the backing local through the plain REST
	// upload surface.
	b := newBinFlow(t, "B", "pw-target", []*metadata.Repo{
		{RepoKey: "replica-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
		{RepoKey: "replica", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
			Config: `{"repositories":["replica-local"]}`},
	})

	// Source A: one local repository plus the replication engine attached to
	// the service's enqueue seam.
	a := newBinFlow(t, "A", "pw-source", []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	store := a.replicationStore(t)

	key := bytes.Repeat([]byte{7}, 32)
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	encPw, err := cipher.Encrypt("pw-target")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	cfg := &replication.ReplicationConfig{
		Name:              "dr-libs-to-b",
		SourceRepo:        "libs",
		TargetURL:         b.url,
		TargetRepo:        "replica-local",
		TargetUsername:    "admin",
		TargetPasswordEnc: encPw,
		Enabled:           true,
		CreatedAt:         metadata.Now(),
		UpdatedAt:         metadata.Now(),
	}
	cfgID, err := store.CreateConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	engine, err := replication.NewEngine(store, a.st, replication.EngineOptions{
		Cipher: cipher,
		Audit:  audit.New(a.md, true),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	repo.AttachReplicator(a.svc, engine)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine Run did not return after cancel")
		}
		engine.CloseIdleConnections()
	})

	// Upload on A through the real REST surface (the Put tail fires the
	// hook, AC ①).
	payload := []byte("the two-instance replication payload")
	sum := sha256.Sum256(payload)
	sha := hex.EncodeToString(sum[:])
	resp, _ := a.do(http.MethodPut, "/binflow/libs/org/app/1.0/app-1.0.bin", "admin", "pw-source", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload on A: status %d, want 201", resp.StatusCode)
	}

	// The engine drains the task to success.
	task := waitForTask(t, store, cfgID, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")
	if task.BlobSHA256 != sha || task.NodePath != "org/app/1.0/app-1.0.bin" {
		t.Fatalf("task = %+v, want the uploaded sha/path", task)
	}

	// Target B serves the same artifact through the replica view: same
	// status, same checksum header, same bytes (AC ③ leg 1).
	gresp, gbody := b.do(http.MethodGet, "/binflow/replica/org/app/1.0/app-1.0.bin", "", "", nil)
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("GET on B replica: status %d, want 200", gresp.StatusCode)
	}
	if got := gresp.Header.Get("X-Checksum-Sha256"); got != sha {
		t.Fatalf("GET on B replica: X-Checksum-Sha256 = %q, want %q", got, sha)
	}
	if !bytes.Equal(gbody, payload) {
		t.Fatalf("GET on B replica: body = %q, want the uploaded payload", gbody)
	}
	// And directly on the backing repository the push landed on.
	dresp, _ := b.do(http.MethodGet, "/binflow/replica-local/org/app/1.0/app-1.0.bin", "", "", nil)
	if dresp.StatusCode != http.StatusOK || dresp.Header.Get("X-Checksum-Sha256") != sha {
		t.Fatalf("GET on B replica-local: status %d checksum %q, want 200/%s",
			dresp.StatusCode, dresp.Header.Get("X-Checksum-Sha256"), sha)
	}

	// The replica view is read-only: PUT and DELETE answer 405 (AC ③ leg 2).
	presp, _ := b.do(http.MethodPut, "/binflow/replica/org/other.bin", "admin", "pw-target", []byte("x"))
	if presp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("PUT on B replica: status %d, want 405", presp.StatusCode)
	}
	dresp2, _ := b.do(http.MethodDelete, "/binflow/replica/org/app/1.0/app-1.0.bin", "admin", "pw-target", nil)
	if dresp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE on B replica: status %d, want 405", dresp2.StatusCode)
	}
}

// TestUploadUnaffectedByUnreachableTarget pins AC ①'s non-blocking clause:
// with the target down, the upload on A still succeeds promptly and the task
// exhausts its retries in the background.
func TestUploadUnaffectedByUnreachableTarget(t *testing.T) {
	ctx := context.Background()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // a bound-then-closed listener: connections are refused

	a := newBinFlow(t, "A2", "pw-source", []*metadata.Repo{
		{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	store := a.replicationStore(t)
	cfg := &replication.ReplicationConfig{
		Name:              "dr-to-nowhere",
		SourceRepo:        "libs",
		TargetURL:         deadURL,
		TargetRepo:        "replica-local",
		TargetUsername:    "admin",
		TargetPasswordEnc: "",
		Enabled:           true,
		CreatedAt:         metadata.Now(),
		UpdatedAt:         metadata.Now(),
	}
	cfgID, err := store.CreateConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	engine, err := replication.NewEngine(store, a.st, replication.EngineOptions{
		RetryBackoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond},
		Sleep:        func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	repo.AttachReplicator(a.svc, engine)
	runCtx, cancel := context.WithCancel(ctx)
	go func() { _ = engine.Run(runCtx) }()
	t.Cleanup(cancel)

	start := time.Now()
	resp, _ := a.do(http.MethodPut, "/binflow/libs/pkg/broken-target.bin", "admin", "pw-source", []byte("payload"))
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload on A: status %d, want 201 despite the dead target", resp.StatusCode)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("upload took %s with a dead target; the hook must stay off the request path", elapsed)
	}

	// The background task burns its six attempts and lands terminal.
	task := waitForTask(t, store, cfgID, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed && task.Attempts >= 6
	}, "failed after exhausting retries")
	if task.LastError == "" {
		t.Fatal("LastError empty on terminal failure")
	}
}
