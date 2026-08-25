package nuget

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The real-stack test harness (the goproxy harness posture verbatim):
// sqlite metadata, the real storage engine, the real repo.Service (the
// remote pull-through engine included), the real auth chain and the real
// httpapi router, with the nuget adapter under test mounted beside the
// generic one — every assertion crosses the full middleware chain (auth
// gates, the addon write gate when a registry is mounted, the api-mount
// rewrite, prefix stripping).

const (
	adminUser = "admin"
	adminPass = "password"
)

func init() { _ = os.Unsetenv("BINFLOW_ADMIN_PASSWORD") }

// stack is one assembled test environment.
type stack struct {
	t       *testing.T
	srv     *httptest.Server
	st      storage.Engine
	md      metadata.Store
	svc     repo.Service
	license *license.Manager // non-nil on the licensed assembly
}

// newStack builds the default stack (anonymous reads on, no addon
// registry — the pre-M10 posture; the gate legs in gate_test.go build
// their own licensed assembly).
func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOpt(t, stackOptions{})
}

// stackOptions tunes the assembly (see newStackOpt).
type stackOptions struct {
	addons *addonsRegistrySeam
	keys   *licenseKeys
}

// newStackOpt assembles the stack; a non-nil keys builds a REAL
// license.Manager over the stack's own licenses store (test keypair
// through the constructor seam), attaches the D3 gate onto repo.Service
// and mounts the addon registry — the licensed posture cmd serves.
func newStackOpt(t *testing.T, opt stackOptions) *stack {
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

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)

	var mgr *license.Manager
	if opt.keys != nil {
		mgr, err = license.New(license.Options{
			Store:      md.Licenses(),
			VerifyKeys: map[string]ed25519.PublicKey{opt.keys.kid: opt.keys.pub},
		})
		if err != nil {
			t.Fatalf("license.New: %v", err)
		}
		if opt.addons != nil {
			repo.AttachPackageTypeGate(svc, opt.addons.gate(mgr))
		}
	}

	// The provider registration feeds the remote engine's upstream hop and
	// TTL split; the duplicate guard makes repeated stacks safe. The
	// handler stays OUT of the global adapter registry — httpapi mounts
	// Deps.Adapters explicitly.
	RegisterMetadata()
	handler := New(svc, md.Repos(), md.Blobs(), md.Remote(), Options{})

	deps := httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		DataDir:   dataDir,
		Console:   console.Handler(),
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), handler},
		Version:   "1.0.0-test",
		Revision:  "t287",
	}
	if opt.addons != nil {
		deps.Addons = opt.addons.reg
	}
	deps.License = mgr
	s := httpapi.New(deps, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &stack{t: t, srv: ts, st: st, md: md, svc: svc, license: mgr}
}

// seedRepo writes a repository row directly through the metadata store
// (the validation matrix is not under test here).
func (s *stack) seedRepo(t *testing.T, key, class string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: Protocol,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedRemoteConfig attaches one remote_configs row (loopback upstreams
// need the SSRF exemption — the admin-set flag ADR-0012 defines).
func (s *stack) seedRemoteConfig(t *testing.T, key, url string) {
	t.Helper()
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: url, AllowPrivateUpstream: true,
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// seedVirtualMembers wires a virtual repository's member ledger (front =
// local-first resolution).
func (s *stack) seedVirtualMembers(t *testing.T, virtual string, members ...string) {
	t.Helper()
	if err := s.md.Virtual().SetMembers(context.Background(), virtual, members); err != nil {
		t.Fatalf("seed members %s <- %v: %v", virtual, members, err)
	}
}

// do issues one request; user != "" adds Basic auth. The response body is
// fully read and returned.
func (s *stack) do(method, path, user, pass string, body io.Reader, hdr map[string]string) (int, string, http.Header) {
	s.t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, body)
	if err != nil {
		s.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		s.t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(b), resp.Header
}

// put issues an authenticated PUT of raw bytes.
func (s *stack) put(path string, body []byte, hdr map[string]string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPut, path, adminUser, adminPass, bytesReader(body), hdr)
}

// get issues an anonymous GET.
func (s *stack) get(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodGet, path, "", "", nil, nil)
}

// delete issues an authenticated DELETE.
func (s *stack) delete(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodDelete, path, adminUser, adminPass, nil, nil)
}

// bytesReader adapts a byte slice to an io.Reader (nil-safe).
func bytesReader(b []byte) io.Reader {
	if b == nil {
		return http.NoBody
	}
	return bytes.NewReader(b)
}

// apiPath renders the v3 api-mount path of one repo.
func apiPath(repo string) string { return "/binflow/api/nuget/v3/" + repo }

// apiV2Path renders the v2 api-mount path of one repo.
func apiV2Path(repo string) string { return "/binflow/api/nuget/v2/" + repo }
