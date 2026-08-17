package repo_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- Test harness: the real engines on temp dirs (highest fidelity) ----

// env is one service under test: the real storage engine plus the real
// SQLite metadata store, each on its own temp directory.
type env struct {
	svc     repo.Service
	st      storage.Engine
	md      metadata.Store
	az      *policyAuthz
	au      *auditLog
	clk     *clock
	dataDir string
}

// clock is a controllable UTC clock so tests can assert timestamp movement.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// newEnv opens a fresh environment with a controllable clock.
func newEnv(t TB) *env {
	t.Helper()
	return newEnvAt(t, t.TempDir(), t.TempDir(), nil)
}

// newEnvAt opens the environment at explicit directories (for
// metadata-injection tests that must inspect the store afterwards). When
// mdOverride is non-nil it replaces the freshly opened store (the
// error-injection decorator wraps one).
func newEnvAt(t TB, dataDir, dbDir string, mdOverride metadata.Store) *env {
	t.Helper()
	ctx := context.Background()
	eng, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	if mdOverride != nil {
		_ = md.Close()
		md = mdOverride
	}
	clk := &clock{now: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	az := &policyAuthz{}
	au := &auditLog{}
	svc := newServiceWithClock(eng, md, az, au, clk.Now)
	t.Cleanup(func() {
		_ = eng.Close()
		if mdOverride == nil {
			_ = md.Close()
		}
	})
	return &env{svc: svc, st: eng, md: md, az: az, au: au, dataDir: dataDir, clk: clk}
}

// TB is the testing.TB subset the helpers need (allows benchmarks too).
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
	Cleanup(f func())
}

// newServiceWithClock wires the service with an injected clock. The
// production constructor New() uses time.Now internally; tests reach the
// same constructor through a small indirection to keep the clock honest.
func newServiceWithClock(eng storage.Engine, md metadata.Store, az repo.Authorizer, au repo.AuditLogger, now func() time.Time) repo.Service {
	return repo.NewWithClock(eng, md, az, au, now)
}

// admin is the M1 ubiquitous principal.
func admin() *repo.Principal { return &repo.Principal{Name: "admin", Admin: true} }

// alice is a non-admin principal.
func alice() *repo.Principal { return &repo.Principal{Name: "alice"} }

// mustCreateRepo creates a local generic repository or fails the test.
func mustCreateRepo(t TB, e *env, key string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo(%s): %v", key, err)
	}
}

// put uploads content and returns the stored node.
func put(t TB, e *env, p *repo.Principal, repoKey, path, content string) *metadata.Node {
	t.Helper()
	n, err := e.svc.Put(context.Background(), p, repoKey, path,
		strings.NewReader(content), storage.BlobRef{}, "application/octet-stream")
	if err != nil {
		t.Fatalf("Put(%s/%s): %v", repoKey, path, err)
	}
	return n
}

// shaOf computes the lowercase sha256 of s.
func shaOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---- fakeAuthorizer: static grants keyed by action ----

// grant is one (action → allow) rule; a pathPrefix "" matches everything.
type grant struct {
	action     string
	pathPrefix string
	allow      bool
}

// policyAuthz grants per (principal, action, path-prefix). nil rules mean
// deny — matching the fail-closed contract.
type policyAuthz struct {
	mu     sync.Mutex
	byUser map[string][]grant
}

func (a *policyAuthz) Can(_ context.Context, p *repo.Principal, _, path, action string) bool {
	if p == nil || p.Admin {
		return p != nil && p.Admin
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, g := range a.byUser[p.Name] {
		if g.action != action {
			continue
		}
		if g.pathPrefix == "" || strings.HasPrefix(path, g.pathPrefix) {
			return g.allow
		}
	}
	return false
}

// add grants user the action under prefix.
func (a *policyAuthz) add(user, action, prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.byUser == nil {
		a.byUser = map[string][]grant{}
	}
	a.byUser[user] = append(a.byUser[user], grant{action: action, pathPrefix: prefix, allow: true})
}

// ---- audit recorder ----

type auditLog struct {
	mu     sync.Mutex
	events []repo.AuditEvent
	fail   bool
}

func (l *auditLog) Append(_ context.Context, e repo.AuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fail {
		return fmt.Errorf("audit append disabled (test)")
	}
	l.events = append(l.events, e)
	return nil
}

func (l *auditLog) actions() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.events))
	for i, e := range l.events {
		out[i] = e.Action
	}
	return out
}

// ---- error-injecting metadata decorator ----

// hookStore wraps metadata.Store to fail selected sub-store calls, plus an
// ordered write journal for the blob-first assertion.
type hookStore struct {
	metadata.Store
	// fail controls injection: called before every hooked method.
	fail func(op string) error
	// journal records every hooked write op in order.
	mu      sync.Mutex
	journal []string
}

// wrapHooks decorates every sub-store with journaling and injection.
func wrapHooks(inner metadata.Store, fail func(op string) error) *hookStore {
	h := &hookStore{Store: inner, fail: fail}
	return h
}

// Repos overrides the inner store with the hooked variant.
func (h *hookStore) Repos() metadata.RepoStore { return &hookRepo{RepoStore: h.Store.Repos(), h: h} }

// Nodes overrides the inner store with the hooked variant.
func (h *hookStore) Nodes() metadata.NodeStore { return &hookNode{NodeStore: h.Store.Nodes(), h: h} }

// Blobs overrides the inner store with the hooked variant.
func (h *hookStore) Blobs() metadata.BlobStore { return &hookBlob{BlobStore: h.Store.Blobs(), h: h} }

func (h *hookStore) note(op string) { h.mu.Lock(); h.journal = append(h.journal, op); h.mu.Unlock() }

func (h *hookStore) check(op string) error {
	if h.fail != nil {
		if err := h.fail(op); err != nil {
			return err
		}
	}
	h.note(op)
	return nil
}

type hookRepo struct {
	metadata.RepoStore
	h *hookStore
}

func (s *hookRepo) Create(ctx context.Context, r *metadata.Repo) error {
	if err := s.h.check("repos.create"); err != nil {
		return err
	}
	return s.RepoStore.Create(ctx, r)
}

func (s *hookRepo) Delete(ctx context.Context, key string) error {
	if err := s.h.check("repos.delete"); err != nil {
		return err
	}
	return s.RepoStore.Delete(ctx, key)
}

type hookNode struct {
	metadata.NodeStore
	h *hookStore
}

func (s *hookNode) Put(ctx context.Context, n *metadata.Node) error {
	if err := s.h.check("nodes.put"); err != nil {
		return err
	}
	return s.NodeStore.Put(ctx, n)
}

func (s *hookNode) Delete(ctx context.Context, repoKey, path string) error {
	if err := s.h.check("nodes.delete"); err != nil {
		return err
	}
	return s.NodeStore.Delete(ctx, repoKey, path)
}

func (s *hookNode) DeleteByPrefix(ctx context.Context, repoKey, prefix string) (int64, error) {
	if err := s.h.check("nodes.delete-by-prefix"); err != nil {
		return 0, err
	}
	return s.NodeStore.DeleteByPrefix(ctx, repoKey, prefix)
}

type hookBlob struct {
	metadata.BlobStore
	h *hookStore
}

func (s *hookBlob) Put(ctx context.Context, b *metadata.Blob) error {
	if err := s.h.check("blobs.put"); err != nil {
		return err
	}
	return s.BlobStore.Put(ctx, b)
}

// entries returns a copy of the write journal.
func (h *hookStore) entries() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.journal))
	copy(out, h.journal)
	return out
}
