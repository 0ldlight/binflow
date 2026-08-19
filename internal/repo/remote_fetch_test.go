package repo_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-66 service-level closed loop: repo.Service.Get/Put/Delete dispatch
// for remote repositories over the real engine, real storage and a real
// (loopback) mock upstream — the service-layer shape of M41/M43/M44/M48,
// with the upstream-count negative assertions the PRD demands.

// remoteCfg builds a create-time remote config body for the given upstream.
func remoteCfg(base, extra string) string {
	return `{"url":"` + base + `","allowPrivateUpstream":true` + extra + `}`
}

// createRemote creates one remote generic repository pointing at base.
func createRemote(t *testing.T, e *env, base, extra string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: remoteCfg(base, extra),
	}); err != nil {
		t.Fatalf("CreateRepo(generic-remote): %v", err)
	}
}

// countingUpstream serves files (404 when absent) and counts requests.
func countingUpstream(t *testing.T, files map[string]string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	hits := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if body, ok := files[r.URL.Path]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func getRemote(t *testing.T, e *env, p *repo.Principal, path string) (string, error) {
	t.Helper()
	rc, _, err := e.svc.Get(context.Background(), p, "generic-remote", path)
	if err != nil {
		return "", err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	b, rerr := io.ReadAll(rc)
	if rerr != nil {
		t.Fatalf("read body: %v", rerr)
	}
	return string(b), nil
}

// ---- M41/M48 shape: pull-through, cache hit, 405, cache delete, refetch ----

func TestServiceRemoteClosedLoop(t *testing.T) {
	files := map[string]string{"/dir/up.bin": "hello-upstream"}
	srv, hits := countingUpstream(t, files)
	e := newEnv(t)
	createRemote(t, e, srv.URL, "")

	// First GET: pulled through and landed in the remote repo's namespace.
	rc, node, err := e.svc.Get(context.Background(), admin(), "generic-remote", "dir/up.bin")
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close() // read-only fd
	if string(b) != "hello-upstream" {
		t.Fatalf("body = %q", string(b))
	}
	if node.RepoKey != "generic-remote" || node.Path != "dir/up.bin" {
		t.Fatalf("node landed at %s/%s", node.RepoKey, node.Path)
	}

	// Second GET: served from the cache — the upstream counter must NOT
	// move (M41's negative assertion).
	if _, err := getRemote(t, e, admin(), "dir/up.bin"); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}

	// PUT: 405 + Allow: GET (RE-05 / M48).
	_, err = e.svc.Put(context.Background(), admin(), "generic-remote", "x.bin",
		strings.NewReader("no"), storage.BlobRef{}, "application/octet-stream")
	var se *repo.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("Put on remote = %v, want a StatusError", err)
	}
	if se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("Put status = %d, want 405", se.Code)
	}
	if got := se.Header.Get("Allow"); got != "GET" {
		t.Fatalf("Allow = %q, want GET", got)
	}
	// The write refusal still unwraps to the M1 sentinel for older mappings.
	if !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Fatalf("405 must wrap ErrRepoTypeNotSupported, got %v", err)
	}
	// PutFromBlob/PutLandedBlob refuse identically (the write plane).
	if _, err := e.svc.PutLandedBlob(context.Background(), admin(), "generic-remote", "x.bin",
		storage.BlobRef{Sha256: node.Sha256}, ""); !errors.As(err, &se) || se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PutLandedBlob on remote = %v, want 405", err)
	}

	// DELETE: the local cache only; the next GET refetches (M48).
	if err := e.svc.Delete(context.Background(), admin(), "generic-remote", "dir/up.bin"); err != nil {
		t.Fatalf("Delete cache: %v", err)
	}
	files["/dir/up.bin"] = "hello-again"
	body, err := getRemote(t, e, admin(), "dir/up.bin")
	if err != nil {
		t.Fatalf("post-delete Get: %v", err)
	}
	if body != "hello-again" {
		t.Fatalf("refetched body = %q", body)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream hits after invalidate+refetch = %d, want 2", got)
	}
	// Deleting a path with nothing cached is the idempotent 404.
	if err := e.svc.Delete(context.Background(), admin(), "generic-remote", "never-there.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("unknown-path delete = %v, want ErrNodeNotFound", err)
	}
}

// ---- M45 shape: the checksum sidecar refusal through the service ----

func TestServiceRemoteChecksumSidecar(t *testing.T) {
	files := map[string]string{"/up.bin": "artifact"}
	srv, hits := countingUpstream(t, files)
	e := newEnv(t)
	createRemote(t, e, srv.URL, "")

	if _, err := getRemote(t, e, admin(), "up.bin"); err != nil {
		t.Fatalf("prefetch: %v", err)
	}
	before := hits.Load()

	_, err := getRemote(t, e, admin(), "up.bin.sha1")
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("sidecar Get = %v, want 404 StatusError", err)
	}
	if se.Message != "Checksums are not downloadable." {
		t.Fatalf("sidecar message = %q", se.Message)
	}
	// The unfound family still wraps ErrNodeNotFound for /api/storage's
	// older mapping.
	if !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("sidecar must wrap ErrNodeNotFound, got %v", err)
	}
	if got := hits.Load(); got != before {
		t.Fatalf("upstream hits = %d, want %d (sidecars never proxied)", got, before)
	}
}

// ---- authorization: the read gate runs BEFORE any upstream contact ----

func TestServiceRemoteReadGatePrecedesUpstream(t *testing.T) {
	srv, hits := countingUpstream(t, map[string]string{"/secret.bin": "x"})
	e := newEnv(t)
	createRemote(t, e, srv.URL, "")

	if _, err := getRemote(t, e, alice(), "secret.bin"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("unauthorized remote Get = %v, want ErrForbidden", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("an unauthorized read must never reach the upstream: hits = %d", got)
	}
	if _, err := getRemote(t, e, nil, "secret.bin"); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous remote Get = %v, want ErrUnauthorized", err)
	}
}

// ---- the credential chain through the service (FR-15-AC9 / FR-20-AC10) ----

func TestServiceRemoteCredentialChain(t *testing.T) {
	key := bytes.Repeat([]byte{0x33}, 32)
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(key))

	dbDir := t.TempDir()
	e := newEnvAt(t, t.TempDir(), dbDir, nil) // the engine boots under the key

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		u, p, ok := r.BasicAuth()
		if !ok || u != "ci" || p != "tpasswd" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("secret-content"))
	}))
	t.Cleanup(srv.Close)

	createRemote(t, e, srv.URL, `,"username":"ci","password":"tpasswd"`)

	// The stored row carries the sealed form (FR-15-AC9-1).
	cfg, err := e.md.Remote().GetConfig(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !strings.HasPrefix(cfg.Password, "enc:v1:") {
		t.Fatalf("stored password = %q, want enc:v1: ciphertext", cfg.Password)
	}
	// The database FILES carry no plaintext (the AC's grep; WAL mode may
	// hold recent writes in the -wal sidecar, so both files are read).
	dbBytes, err := readDBTree(dbDir)
	if err != nil {
		t.Fatalf("read db files: %v", err)
	}
	if strings.Contains(dbBytes, "tpasswd") {
		t.Fatalf("database file contains the plaintext password")
	}
	if !strings.Contains(dbBytes, "enc:v1:") {
		t.Fatalf("database file lacks the ciphertext marker")
	}

	// The fetch authenticates with the decrypted credential (AC10).
	body, err := getRemote(t, e, admin(), "sec.bin")
	if err != nil {
		t.Fatalf("authenticated Get: %v", err)
	}
	if body != "secret-content" {
		t.Fatalf("body = %q", body)
	}

	// GET config never echoes the credential (NFR-S14).
	got, err := e.svc.GetRepo(context.Background(), admin(), "generic-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if strings.Contains(got.Config, "tpasswd") || strings.Contains(got.Config, "enc:v1") {
		t.Fatalf("config echo leaks the credential: %s", got.Config)
	}

	// Wrong password (full-replace update): the upstream 401 is unfound
	// with the summary in the message.
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: remoteCfg(srv.URL, `,"username":"ci","password":"wrong"`),
	}); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	e.clk.Advance(7201 * time.Second) // expire the cached copy to force a refetch
	_, err = getRemote(t, e, admin(), "sec.bin")
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("refused credential = %v, want 404 StatusError", err)
	}
	if !strings.Contains(se.Message, "401") {
		t.Fatalf("message must carry the upstream 401 summary: %q", se.Message)
	}
}

// ---- a password offered while no key is configured is dropped, not stored ----

func TestServiceRemotePasswordDroppedWithoutKey(t *testing.T) {
	e := newEnv(t) // no env key in this process
	srv, _ := countingUpstream(t, map[string]string{"/a": "x"})
	createRemote(t, e, srv.URL, `,"username":"ci","password":"would-be-plaintext"`)

	cfg, err := e.md.Remote().GetConfig(context.Background(), "generic-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Password != "" {
		t.Fatalf("password without a key must be dropped, got %q", cfg.Password)
	}
	// The fetch still works (anonymous).
	if body, err := getRemote(t, e, admin(), "a"); err != nil || body != "x" {
		t.Fatalf("anonymous fallback fetch = (%q, %v)", body, err)
	}
}

// ---- startup fail-fast through repo.New (FR-15-AC9-2) ----

func TestServiceStartupFailFastWithoutKey(t *testing.T) {
	key := bytes.Repeat([]byte{0x11}, 32)
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(key))
	dbDir := t.TempDir()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	c, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	sealed, err := c.Encrypt("pw")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	const now = "2026-08-19T12:00:00Z"
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "r", Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: `{}`,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if err := md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{
		RepoKey: "r", URL: "http://example.com", Password: sealed,
	}); err != nil {
		t.Fatalf("create config: %v", err)
	}
	_ = md.Close()

	_ = os.Unsetenv("BINFLOW_REMOTE_CREDENTIALS_KEY")
	// Reopen the store: repo.New's engine runs its credential pass against
	// a live handle (the restart the AC describes).
	md2, err := metadata.Open(context.Background(), metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("reopen metadata: %v", err)
	}
	defer md2.Close() //nolint:errcheck // teardown of the reopened handle
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	defer st.Close() //nolint:errcheck // teardown of the probe engine

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("repo.New must fail fast when credentials exist without a key")
		}
		msg := fmtText(r)
		if !strings.Contains(msg, "BINFLOW_REMOTE_CREDENTIALS_KEY") {
			t.Fatalf("panic message must name the env: %s", msg)
		}
	}()
	_ = repo.New(st, md2, nil, nil)
}

// readDBTree concatenates the sqlite main file and its WAL sidecar (recent
// writes live there until a checkpoint).
func readDBTree(dbDir string) (string, error) {
	var out string
	for _, name := range []string{"binflow.db", "binflow.db-wal"} {
		b, err := os.ReadFile(filepath.Join(dbDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		out += string(b)
	}
	return out, nil
}

// fmtText renders a recover() value for assertion.
func fmtText(v any) string {
	switch x := v.(type) {
	case error:
		return x.Error()
	case string:
		return x
	default:
		return ""
	}
}

// ---- the fetch log line: fields present, credentials absent (NFR-S14) ----

func TestServiceRemoteFetchLogFields(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	oldDefault := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	srv, _ := countingUpstream(t, map[string]string{"/a.bin": "logged"})
	e := newEnv(t)
	createRemote(t, e, srv.URL, `,"username":"ci","password":"seekrit"`)

	if _, err := getRemote(t, e, admin(), "a.bin"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	out := buf.String()
	for _, field := range []string{
		"upstream_host=", "cache_result=", "upstream_status=", "upstream_duration_ms=",
	} {
		if !strings.Contains(out, field) {
			t.Fatalf("fetch log lacks %s: %s", field, out)
		}
	}
	for _, leak := range []string{"seekrit", "Authorization"} {
		if strings.Contains(out, leak) {
			t.Fatalf("fetch log leaks %q: %s", leak, out)
		}
	}
}
