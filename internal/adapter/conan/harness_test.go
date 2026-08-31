package conan

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The real-stack test harness (the cargo harness posture verbatim): sqlite
// metadata, the real storage engine, the real repo.Service, the real auth
// chain and the real httpapi router, with the conan adapter under test
// mounted beside the generic one — every assertion crosses the full
// middleware chain (auth gates, the addon write gate when a registry is
// mounted, prefix stripping).

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
	h       *Handler         // the adapter under test (direct seam access)
	dataDir string           // the engine root (the sweep's blob-face reconciliation walks it)
}

// handlerForTest exposes the adapter for the index-layer assertions that
// read the stored nodes directly.
func (s *stack) handlerForTest() *Handler { return s.h }

// adminPrincipal is the seeded admin's service-level identity (the direct
// service-seam calls the index assertions make).
func adminPrincipal() *repo.Principal {
	return &repo.Principal{Name: adminUser, Admin: true}
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
	handler := New(svc, md.Repos(), md.Blobs(), authSvc, Options{BaseURL: opt.baseURL})

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
		Revision:  "t308",
	}
	if opt.addons != nil {
		deps.Addons = opt.addons.reg
	}
	deps.License = mgr
	s := httpapi.New(deps, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &stack{t: t, srv: ts, st: st, md: md, svc: svc, auth: authSvc, license: mgr, h: handler, dataDir: dataDir}
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

// seedRepoCfg seeds one repository row carrying a config blob (the
// forceConanAuthentication posture tests — the raw-seeded shape the
// adapter's tolerant probe reads).
func (s *stack) seedRepoCfg(t *testing.T, key, class, cfg string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: Protocol, Config: cfg,
	}); err != nil {
		t.Fatalf("seed repo %s (cfg %s): %v", key, cfg, err)
	}
}

// setRepoConfig rewrites one repository row's config blob (the flip-off
// roundtrip arm).
func (s *stack) setRepoConfig(t *testing.T, key, cfg string) {
	t.Helper()
	row, err := s.md.Repos().Get(context.Background(), key)
	if err != nil {
		t.Fatalf("load repo %s: %v", key, err)
	}
	row.Config = cfg
	if err := s.md.Repos().Update(context.Background(), row); err != nil {
		t.Fatalf("set config %s = %s: %v", key, cfg, err)
	}
}

// seedRemoteConfig attaches one remote_configs row (loopback upstreams
// need the SSRF exemption — the admin-set flag ADR-0012 defines; the
// goproxy harness posture).
func (s *stack) seedRemoteConfig(t *testing.T, key, url string) {
	t.Helper()
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: url, AllowPrivateUpstream: true,
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// seedVirtualRepo writes one virtual repository row carrying both the
// member list and the write route in its config JSON (the raw-seeded
// shape the service's tolerant readers accept).
func (s *stack) seedVirtualRepo(t *testing.T, key string, members []string, deploy string) {
	t.Helper()
	cfg := fmt.Sprintf(`{"repositories":[%s]`, quoteJoin(members))
	if deploy != "" {
		cfg += `,"defaultDeploymentRepo":"` + deploy + `"`
	}
	cfg += "}"
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: Protocol, Config: cfg,
	}); err != nil {
		t.Fatalf("seed virtual %s: %v", key, err)
	}
	if err := s.md.Virtual().SetMembers(context.Background(), key, members); err != nil {
		t.Fatalf("seed members %s <- %v: %v", key, members, err)
	}
}

// quoteJoin renders one JSON string-array body.
func quoteJoin(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = `"` + it + `"`
	}
	return strings.Join(quoted, ",")
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

// get issues an anonymous GET.
func (s *stack) get(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodGet, path, "", "", nil, nil)
}

// put issues an authenticated PUT with a byte body.
func (s *stack) put(path string, body []byte, hdr map[string]string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPut, path, adminUser, adminPass, bytesReader(body), hdr)
}

// post issues an authenticated POST with a byte body.
func (s *stack) post(path string, body []byte, hdr map[string]string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPost, path, adminUser, adminPass, bytesReader(body), hdr)
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

// v2 renders a v2 plane path.
func v2(repo string, rest string) string { return repoPath(repo) + "/v2/conans/" + rest }

// v1 renders a v1 plane path.
func v1(repo string, rest string) string { return repoPath(repo) + "/v1/" + rest }

// putRecipeFile uploads one recipe file through the v2 plane (the admin
// credential; the test's basic building block) and returns the status.
func (s *stack) putRecipeFile(repo string, r ref, rrev, name string, body []byte) (int, string, http.Header) {
	s.t.Helper()
	return s.put(v2(repo, r.name+"/"+r.version+"/"+r.user+"/"+r.channel+"/revisions/"+rrev+"/files/"+name), body, nil)
}

// putPkgFile uploads one package file through the v2 plane.
func (s *stack) putPkgFile(repo string, r ref, rrev, pid, prev, name string, body []byte) (int, string, http.Header) {
	s.t.Helper()
	return s.put(v2(repo, r.name+"/"+r.version+"/"+r.user+"/"+r.channel+"/revisions/"+rrev+"/packages/"+pid+
		"/revisions/"+prev+"/files/"+name), body, nil)
}

// fixtureRev returns a fixed 64-hex revision spelling.
func fixtureRev(seed byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = "0123456789abcdef"[(int(seed)+i)%16]
	}
	return string(b)
}

// fixturePID returns a fixed 40-hex packageId spelling.
func fixturePID(seed byte) string {
	b := make([]byte, 40)
	for i := range b {
		b[i] = "0123456789abcdef"[(int(seed)+i)%16]
	}
	return string(b)
}

// conaninfoFixture renders one conaninfo.txt body.
func conaninfoFixture(settings, options, requires []string) string {
	var b bytes.Buffer
	b.WriteString("[settings]\n")
	for _, kv := range settings {
		b.WriteString("    " + kv + "\n")
	}
	b.WriteString("\n[requires]\n")
	for _, req := range requires {
		b.WriteString("    " + req + "\n")
	}
	b.WriteString("\n[options]\n")
	for _, kv := range options {
		b.WriteString("    " + kv + "\n")
	}
	b.WriteString("\n[full_package_mode]\n")
	return b.String()
}

// ---- the licensed assembly (gate_test.go's seam) ----

// licenseKeys is one injected verify keypair.
type licenseKeys struct {
	kid  string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newLicenseKeys(t *testing.T) *licenseKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return &licenseKeys{kid: "t308-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t308-" + tier,
		"licensee":  "T-308 test",
		"tier":      tier,
		"issuedAt":  time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		"notBefore": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
	if expires != nil {
		payload["expiresAt"] = *expires
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sig := ed25519.Sign(k.priv, raw)
	b64 := base64.RawURLEncoding
	return b64.EncodeToString(raw) + "." + b64.EncodeToString(sig)
}

// proDoc renders one pro document with a far expiry.
func proDoc(t *testing.T, k *licenseKeys) string {
	t.Helper()
	far := time.Now().UTC().Add(30 * 24 * time.Hour).Format(time.RFC3339)
	return k.sign(t, "pro", &far)
}

// addonsRegistrySeam bundles the assembled registry with the gate adapter
// (cmd's packageTypeGate shape, mirrored test-side because repo must not
// import license/addons — the cargo gate_test posture verbatim).
type addonsRegistrySeam struct {
	reg *addons.Registry
}

func newAddonsSeam() *addonsRegistrySeam {
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.Conan())}
}

// gate builds the repo.PackageTypeGate over registry + Manager.
func (s *addonsRegistrySeam) gate(ev *license.Manager) repo.PackageTypeGate {
	return gateAdapter{reg: s.reg, ev: ev}
}

type gateAdapter struct {
	reg *addons.Registry
	ev  *license.Manager
}

func (g gateAdapter) Verdict(ctx context.Context, packageType string) repo.PackageTypeVerdict {
	st, ok := g.reg.StatusOf(ctx, g.ev, packageType)
	if !ok {
		return repo.PackageTypeVerdict{}
	}
	v := repo.PackageTypeVerdict{Known: true, Unlocked: st.Enable}
	if !st.Enable {
		v.Refusal = fmt.Sprintf("license tier '%s' < '%s'", g.ev.State().Tier.String(), st.Addon.MinTier)
	}
	return v
}
