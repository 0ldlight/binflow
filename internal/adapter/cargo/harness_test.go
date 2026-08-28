package cargo

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

// The real-stack test harness (the nuget harness posture verbatim):
// sqlite metadata, the real storage engine, the real repo.Service, the
// real auth chain and the real httpapi router, with the cargo adapter
// under test mounted beside the generic one — every assertion crosses
// the full middleware chain (auth gates, the addon write gate when a
// registry is mounted, prefix stripping).

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
	auth    *auth.Service    // the real chain (token issuance for client legs)
	license *license.Manager // non-nil on the licensed assembly
}

// stackOptions tunes the assembly (see newStackOpt).
type stackOptions struct {
	baseURL   string // Options.BaseURL (server.base_url); "" = request-derived
	anonymous bool   // the global anonymous read flag
	addons    *addonsRegistrySeam
	keys      *licenseKeys
}

// newStack builds the default stack: anonymous reads on, no addon
// registry (the gate legs in gate_test.go build their own licensed
// assembly).
func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOpt(t, stackOptions{anonymous: true})
}

// newStackOpt assembles the stack. A non-nil keys builds a REAL
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
	cfg.Security.AnonymousAccess = opt.anonymous

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

	// The provider registration feeds the future remote engine's TTL
	// split; the duplicate guard makes repeated stacks safe. The handler
	// stays OUT of the global adapter registry — httpapi mounts
	// Deps.Adapters explicitly.
	RegisterMetadata()
	handler := New(svc, md.Repos(), md.Blobs(), md.NodeProps(), Options{
		BaseURL:         opt.baseURL,
		AnonymousAccess: opt.anonymous,
	})

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
		Revision:  "t294",
	}
	if opt.addons != nil {
		deps.Addons = opt.addons.reg
	}
	deps.License = mgr
	s := httpapi.New(deps, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &stack{t: t, srv: ts, st: st, md: md, svc: svc, auth: authSvc, license: mgr}
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

// seedVirtualMembers wires a virtual repository's member ledger.
func (s *stack) seedVirtualMembers(t *testing.T, virtual string, members ...string) {
	t.Helper()
	if err := s.md.Virtual().SetMembers(context.Background(), virtual, members); err != nil {
		t.Fatalf("seed members %s <- %v: %v", virtual, members, err)
	}
}

// seedRemoteConfig attaches one remote_configs row (loopback upstreams
// need the SSRF exemption — the admin-set flag ADR-0012 defines; the
// goproxy/conan harness posture).
func (s *stack) seedRemoteConfig(t *testing.T, key, url string) {
	t.Helper()
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: url, AllowPrivateUpstream: true,
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
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

// put issues an authenticated PUT.
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

// repoPath renders the content-plane path of one repo.
func repoPath(repo string) string { return "/binflow/" + repo }

// publishBody frames one publish payload ([u32 LE][JSON][u32 LE][crate]).
func publishBody(metaJSON string, crate []byte) []byte {
	meta := []byte(metaJSON)
	var out bytes.Buffer
	out.Write(uint32le(uint32(len(meta))))
	out.Write(meta)
	out.Write(uint32le(uint32(len(crate))))
	out.Write(crate)
	return out.Bytes()
}

// uint32le renders one little-endian length prefix.
func uint32le(v uint32) []byte {
	var b [4]byte
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
	return b[:]
}
