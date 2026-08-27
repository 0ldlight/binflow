package helm

// The real-stack test harness (the cargo/nuget posture verbatim): sqlite
// metadata, the real storage engine, the real repo.Service, the real auth
// chain and the real httpapi router, with the helm adapter under test
// mounted beside the generic one — every assertion crosses the full
// middleware chain (auth gates, the api/helm alias rewrite, the reindex
// management family, prefix stripping).

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	baseURL     string // Options.BaseURL (server.base_url); "" = request-derived
	addons      *addonsRegistrySeam
	keys        *licenseKeys
	extPatterns []string // Options.ExternalPatterns (nil = the "**" default)
}

// newStack builds the default stack: anonymous reads on, no addon
// registry (the gate legs in gate_test.go build their own licensed
// assembly).
func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOpt(t, stackOptions{})
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
	cfg.Security.AnonymousAccess = true

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
	handler := New(svc, md.Repos(), md.Blobs(), md.NodeProps(), md.Remote(), Options{
		BaseURL:          opt.baseURL,
		ExternalPatterns: opt.extPatterns,
		Now:              func() time.Time { return time.Now() },
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
		Revision:  "t309",
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
// (the validation matrix is not under test here); config seeds the
// Enforce Layout switches when non-empty.
func (s *stack) seedRepo(t *testing.T, key, class, config string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: Protocol, Config: config,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedRemoteRepo writes one REMOTE helm repository row plus its
// remote_configs row directly through the metadata store (the creation
// validation matrix — the helm package type's enum/gate family — is not
// under test here; the licensed gate legs live in gate_test.go). The
// loopback upstream the tests use demands the admin-set private-upstream
// exemption, the same flag a production administrator grants for an
// internal mirror. The TTLs carry the DDL defaults (86400/600); what
// matters is a non-zero window so a landed copy serves its HITs.
func (s *stack) seedRemoteRepo(t *testing.T, key, upstream string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: Protocol, Config: "{}",
	}); err != nil {
		t.Fatalf("seed remote repo %s: %v", key, err)
	}
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: strings.TrimRight(upstream, "/"),
		ContentTTLSeconds:    86400,
		MetadataTTLSeconds:   600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// seedVirtualRepo writes one VIRTUAL helm repository row plus its member
// ledger directly through the metadata store (the same direct-seed posture
// as seedRemoteRepo). The member rows must exist; deployment names the
// defaultDeploymentRepo when non-empty. Position = declaration order (the
// two-bucket rest bucket; no priority marks in these fixtures).
func (s *stack) seedVirtualRepo(t *testing.T, key, deployment string, members ...string) {
	t.Helper()
	cfg := `{"repositories":[` + quoteJoin(members) + `]`
	if deployment != "" {
		cfg += `,"defaultDeploymentRepo":"` + deployment + `"`
	}
	cfg += `}`
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: Protocol, Config: cfg,
	}); err != nil {
		t.Fatalf("seed virtual repo %s: %v", key, err)
	}
	if err := s.md.Virtual().SetMembers(context.Background(), key, members); err != nil {
		t.Fatalf("seed virtual members %s: %v", key, err)
	}
}

// quoteJoin renders ["a","b"] for the member-list config.
func quoteJoin(ss []string) string {
	quoted := make([]string, len(ss))
	for i, v := range ss {
		quoted[i] = `"` + v + `"`
	}
	return strings.Join(quoted, ",")
}

// chartUpstream is one loopback upstream chart repository: fixed file
// bodies plus a request counter per path (the MISS/HIT assertions read
// it).
type chartUpstream struct {
	srv   *httptest.Server
	files map[string]string
	hits  map[string]*atomic.Int64
	mu    sync.Mutex
}

// newChartUpstream starts one upstream over the given path->body map.
func newChartUpstream(t *testing.T, files map[string]string) *chartUpstream {
	t.Helper()
	up := &chartUpstream{files: files, hits: map[string]*atomic.Int64{}}
	up.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up.mu.Lock()
		c := up.hits[r.URL.Path]
		if c == nil {
			c = &atomic.Int64{}
			up.hits[r.URL.Path] = c
		}
		up.mu.Unlock()
		c.Add(1)
		body, ok := up.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, ".yaml") {
			w.Header().Set("Content-Type", "text/yaml")
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(up.srv.Close)
	return up
}

// hitCount reports one path's upstream request count.
func (u *chartUpstream) hitCount(path string) int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	if c := u.hits[path]; c != nil {
		return c.Load()
	}
	return 0
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

// post issues an authenticated POST (the reindex family).
func (s *stack) post(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPost, path, adminUser, adminPass, http.NoBody, nil)
}

// bytesReader adapts a byte slice to an io.Reader (nil-safe).
func bytesReader(b []byte) io.Reader {
	if b == nil {
		return http.NoBody
	}
	return bytes.NewReader(b)
}

// ---- chart fixtures ----

// fixtureChart builds one real .tgz chart archive around a Chart.yaml
// body (and optional extra members), the byte-for-byte shape helm
// package produces: a single root directory.
func fixtureChart(t *testing.T, root string, chartYAML string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	writeMember := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{
			Name: root + "/" + name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar body %s: %v", name, err)
		}
	}
	writeMember("Chart.yaml", chartYAML)
	for name, body := range extra {
		writeMember(name, body)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// defaultChartYAML is the helm-create-like fixture body.
func defaultChartYAML(name, version string) string {
	return "apiVersion: v2\nname: " + name + "\ndescription: A " + name + " chart for BinFlow tests\n" +
		"type: application\nversion: " + version + "\nappVersion: 1.16.0\nhome: https://example.com/" + name + "\n" +
		"keywords:\n  - binflow\n  - test\nsources:\n  - https://example.com/src\n" +
		"maintainers:\n  - name: Tester\n    email: tester@example.com\n"
}

// sha256Hex renders one body's sha256 (the index digest's spelling).
func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
