package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// adminPass is the documented evaluation default seeded by metadata.Open
// when BINFLOW_ADMIN_PASSWORD is unset (ADR-0009). init() clears the env
// so the default is deterministic regardless of the developer shell.
const (
	adminUser = "admin"
	adminPass = "password"
)

func init() { _ = os.Unsetenv("BINFLOW_ADMIN_PASSWORD") }

// harness is a real-stack test environment: sqlite metadata, the real
// storage engine, real repo.Service, real auth.Service and the real
// generic adapter — injected per-stack so the process-wide adapter
// registry never couples parallel tests. Only the HTTP listener is
// httptest.
type harness struct {
	t        *testing.T
	srv      *httptest.Server
	st       storage.Engine
	md       metadata.Store
	svc      repo.Service
	authSvc  *auth.Service
	dataDir  string
	logLines *[]string
	mu       *sync.Mutex
}

func newHarness(t *testing.T) *harness { return newHarnessCfg(t, nil, nil) }

// newHarnessCfg builds the stack. mutate adjusts the config before
// assembly (anonymous-access off, CORS on, ...); users seeds extra local
// non-admin accounts (name/password pairs). extra mounts additional
// protocol handlers behind the standard two (M3 seam tests inject fake
// npm/pypi handlers this way; the default stack stays at M2's surface).
func newHarnessCfg(t *testing.T, mutate func(*config.Config), users [][2]string, extra ...adapter.Handler) *harness {
	return newHarnessAuth(t, mutate, nil, nil, users, extra...)
}

// newHarnessAuth is newHarnessCfg plus a seam over the auth service
// itself: authMutate receives the assembled service and returns the one
// the stack should use (T-192 injects a lowered argon2 gate for the
// disconnect-storm regression; nil keeps the default assembly). It runs
// BEFORE repo/adapter wiring so every consumer sees the same service.
// storeMutate is the same seam one layer down (T-252 injects a
// query-counting GroupStore for the E5 N+1 gate); it wraps the store
// BEFORE auth/repo/httpapi wiring, again so every consumer sees one store.
func newHarnessAuth(t *testing.T, mutate func(*config.Config), authMutate func(*auth.Service) *auth.Service, storeMutate func(metadata.Store) metadata.Store, users [][2]string, extra ...adapter.Handler) *harness {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	if storeMutate != nil {
		md = storeMutate(md)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	if mutate != nil {
		mutate(cfg)
	}

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	if authMutate != nil {
		authSvc = authMutate(authSvc)
	}
	// The real audit logger, matching cmd assembly (T-95): governance
	// assertions (quota.exceeded et al.) read the events the REST path
	// actually records, instead of the nil the harness used to wire.
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	genericHandler := generic.New(svc, md.Blobs())

	for _, u := range users {
		hash, err := auth.HashPassword(u[1])
		if err != nil {
			t.Fatalf("hash password for %s: %v", u[0], err)
		}
		if err := md.Users().Create(ctx, &metadata.User{
			Username: u[0], PasswordHash: hash, IsAdmin: false, Enabled: true,
		}); err != nil {
			t.Fatalf("seed user %s: %v", u[0], err)
		}
	}

	lines, logger, mu := newCapturingLogger()
	dockerHandler := docker.New(svc, docker.NewRepoLookup(md.Repos()),
		authSvc, authSvc, md.Users(), docker.Options{
			AnonymousAccess: cfg.Security.AnonymousAccess,
			BaseURL:         cfg.Server.BaseURL,
			TokenTTL:        cfg.Auth.TokenDefaultTTL,
		}, logger).
		WithStorage(st, md.Blobs())
	mounted := append([]adapter.Handler{genericHandler, dockerHandler}, extra...)
	s := httpapi.New(httpapi.Deps{
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
		Console:   console.Handler(),
		Adapters:  mounted,
		Version:   "1.0.0-test",
		Revision:  "abc123",
	}, logger)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &harness{
		t: t, srv: ts, st: st, md: md, svc: svc, authSvc: authSvc, dataDir: dataDir,
		logLines: lines, mu: mu,
	}
}

// lineWriter appends whole log lines under a mutex (slog handlers may run
// concurrently).
type lineWriter struct {
	lines *[]string
	mu    *sync.Mutex
}

func (w lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.lines = append(*w.lines, strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// newCapturingLogger returns a line store, a logger writing into it and
// the mutex guarding the store.
func newCapturingLogger() (*[]string, *slog.Logger, *sync.Mutex) {
	var lines []string
	var mu sync.Mutex
	logger := slog.New(slog.NewTextHandler(lineWriter{&lines, &mu}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &lines, logger, &mu
}

// logs returns a snapshot of the captured log output.
func (h *harness) logs() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b strings.Builder
	for _, l := range *h.logLines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func (h *harness) resetLogs() {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.logLines = nil
}

// do issues one request; user != "" adds Basic auth. body may be nil.
func (h *harness) do(method, path, user, pass string, body []byte, hdr map[string]string) *http.Response {
	h.t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, h.srv.URL+path, rdr)
	if err != nil {
		h.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// mutatedConfig aliases the config type for mutate callbacks in tests.
type mutatedConfig = config.Config

// rebuildWithDataDir returns a live server identical to the harness except
// that the health/readiness storage probe targets dataDir (the B1 test's
// read-only directory).
func (h *harness) rebuildWithDataDir(t *testing.T, dataDir string) *rebuiltServer {
	t.Helper()
	s := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     h.authSvc,
		Authz:    h.authSvc,
		Metadata: h.md,
		Repos:    h.md.Repos(),
		DataDir:  dataDir,
		Console:  console.Handler(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &rebuiltServer{ts: ts}
}

// errNoRepo satisfies the repo-lookup seam of the minimal panic stack.
var errNoRepo = errors.New("no repo")

// noRepoLookup is a RepoLookup stub for stacks that never route content.
type noRepoLookup struct{}

func (noRepoLookup) Get(_ context.Context, _ string) (*metadata.Repo, error) {
	return nil, errNoRepo
}

// newPanicConsoleServer builds the smallest stack whose terminal handler
// panics: the console placeholder is swapped for a panicking handler, so
// recover is exercised through the real chain (requestID -> accessLog ->
// recover -> ... -> handler) without faking a failure in the content
// path.
type panicConsoleServer struct {
	handler http.Handler
	logs    func() string
}

func newPanicConsoleServer(t *testing.T) *panicConsoleServer {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/panic.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	authSvc := auth.NewFromStore(md, true)

	lines, logger, mu := newCapturingLogger()
	s := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    noRepoLookup{},
		Console:  http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }),
	}, logger)
	return &panicConsoleServer{
		handler: s.Handler(),
		logs: func() string {
			mu.Lock()
			defer mu.Unlock()
			var b strings.Builder
			for _, l := range *lines {
				b.WriteString(l)
				b.WriteByte('\n')
			}
			return b.String()
		},
	}
}
