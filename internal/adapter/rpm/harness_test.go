package rpm

// The real-stack test harness (the helm/cargo posture verbatim): sqlite
// metadata, the real storage engine, the real repo.Service, the real auth
// chain and the real httpapi router, with the rpm adapter under test
// mounted beside the generic one — every assertion crosses the full
// middleware chain (auth gates, the license gate, the /api/yum family,
// prefix stripping).

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
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
	t        *testing.T
	srv      *httptest.Server
	st       storage.Engine
	md       metadata.Store
	svc      repo.Service
	auth     *auth.Service    // the real chain (token issuance for client legs)
	license  *license.Manager // non-nil on the licensed assembly
	keypairs *keypair.Manager // the real keypair plane (T-322); the signing seam assembles from the same rows
	dataDir  string
}

// stackOptions tunes the assembly (see newStackOpt).
type stackOptions struct {
	dataDir string // Options.DataDir (the .rpmcache root); "" = disabled
	aggTTL  time.Duration
	addons  *addonsRegistrySeam
	keys    *licenseKeys
	// signer overrides the signing seam (T-322 legs that drive the error
	// taxonomy); nil assembles the real keypair.SigningService over the
	// stack's own rows and cipher.
	signer   RepomdSigner
	spoolDir string // Options.SpoolDir; "" = the cmd assembly's <dataDir>/staging (T-476)
}

// newStack builds the default stack: anonymous reads on, no addon
// registry (the gate legs in gate_test.go build their own licensed
// assembly).
func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOpt(t, stackOptions{dataDir: t.TempDir()})
}

// newStackOpt assembles the stack. A non-nil keys builds a REAL
// license.Manager over the stack's own licenses store (test keypair
// through the constructor seam), attaches the D3 gate onto repo.Service
// and mounts the addon registry — the licensed posture cmd serves.
func newStackOpt(t *testing.T, opt stackOptions) *stack {
	t.Helper()
	ctx := context.Background()
	dataDir := opt.dataDir
	if dataDir == "" {
		dataDir = t.TempDir()
	}

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

	// The signing seam (T-322, sign.go): the real keypair plane over the
	// stack's own rows — the same collaborators cmd wires (the manager's
	// REST faces and the SigningService assemble from one store/cipher/
	// repo triple), with a fixed test master key. opt.signer overrides the
	// seam for the error-taxonomy legs.
	kcipher, err := remote.NewCipher([]byte("t322-rpm-signing-master-key-0000")) // 32 bytes
	if err != nil {
		t.Fatalf("remote.NewCipher: %v", err)
	}
	kpMgr, err := keypair.NewManager(keypair.Options{
		Store:  md.GpgKeypairs(),
		Cipher: kcipher,
		Repos:  md.Repos(),
	})
	if err != nil {
		t.Fatalf("keypair.NewManager: %v", err)
	}
	signSvc, err := keypair.NewSigningService(md.GpgKeypairs(), kcipher, md.Repos())
	if err != nil {
		t.Fatalf("keypair.NewSigningService: %v", err)
	}
	var signer RepomdSigner = signSvc
	if opt.signer != nil {
		signer = opt.signer
	}

	// The provider registration feeds the remote engine's TTL split; the
	// duplicate guard makes repeated stacks safe. The handler stays OUT of
	// the global adapter registry — httpapi mounts Deps.Adapters
	// explicitly.
	RegisterMetadata()
	spoolDir := opt.spoolDir
	if spoolDir == "" {
		// Mirror the cmd assembly (T-476): PUT bodies stage on the
		// storage volume's staging/ dir, never the OS temp dir.
		spoolDir = filepath.Join(dataDir, "staging")
	}
	handler := NewWithProps(svc, md.Repos(), md.Blobs(), md.NodeProps(), Options{
		DataDir:  dataDir,
		AggTTL:   opt.aggTTL,
		Now:      func() time.Time { return time.Now() },
		Signer:   signer,
		SpoolDir: spoolDir,
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
		Revision:  "t311",
	}
	if opt.addons != nil {
		deps.Addons = opt.addons.reg
	}
	deps.License = mgr
	s := httpapi.New(deps, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &stack{t: t, srv: ts, st: st, md: md, svc: svc, auth: authSvc, license: mgr, keypairs: kpMgr, dataDir: dataDir}
}

// seedRepo writes a repository row directly through the metadata store
// (the validation matrix is not under test here); config seeds the rpm
// section when non-empty.
func (s *stack) seedRepo(t *testing.T, key, class, config string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: Protocol, Config: config,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedRemoteRepo writes one REMOTE rpm repository row plus its remote
// config (the helm fixture posture): the loopback upstream the engine
// fetches, the default TTL pair and the private-upstream allowance the
// test-origin loopback needs.
func (s *stack) seedRemoteRepo(t *testing.T, key, upstream string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: Protocol, Config: "{}",
	}); err != nil {
		t.Fatalf("seed remote repo %s: %v", key, err)
	}
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey:              key,
		URL:                  strings.TrimRight(upstream, "/"),
		ContentTTLSeconds:    86400,
		MetadataTTLSeconds:   600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// seedVirtualRepo writes one VIRTUAL rpm repository row plus its member
// ledger directly through the metadata store. The member rows must
// exist; deployment names the defaultDeploymentRepo when non-empty;
// priorities maps member keys marked priorityResolution=true.
func (s *stack) seedVirtualRepo(t *testing.T, key, deployment string, members []string, priorities map[string]bool) {
	t.Helper()
	cfg := `{"repositories":[`
	for i, m := range members {
		if i > 0 {
			cfg += ","
		}
		cfg += `"` + m + `"`
	}
	cfg += `]`
	if deployment != "" {
		cfg += `,"defaultDeploymentRepo":"` + deployment + `"`
	}
	cfg += `}`
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: Protocol, Config: cfg,
	}); err != nil {
		t.Fatalf("seed virtual repo %s: %v", key, err)
	}
	// The member LEDGER drives resolution (the config JSON's repositories
	// array is the REST plane's input shape; the store's own face is
	// SetMembers).
	if err := s.md.Virtual().SetMembers(context.Background(), key, members); err != nil {
		t.Fatalf("seed virtual members %s: %v", key, err)
	}
	for m, on := range priorities {
		if !on {
			continue
		}
		row, err := s.md.Repos().Get(context.Background(), m)
		if err != nil {
			t.Fatalf("load member %s for priority mark: %v", m, err)
		}
		// Splice the mark into the member's config blob (the order seam's
		// tolerant probe only reads the one flag).
		switch row.Config {
		case "", "{}":
			row.Config = `{"priorityResolution":true}`
		default:
			row.Config = strings.TrimSuffix(row.Config, "}") + `,"priorityResolution":true}`
		}
		if err := s.md.Repos().Update(context.Background(), row); err != nil {
			t.Fatalf("mark member %s priority: %v", m, err)
		}
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

// post issues an authenticated POST (the reindex family).
func (s *stack) post(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPost, path, adminUser, adminPass, http.NoBody, nil)
}

// gunzip decompresses one body (test-side helper).
func gunzip(t *testing.T, body []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip open: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return out
}

// bytesReader adapts a byte slice to an io.Reader (nil-safe).
func bytesReader(b []byte) io.Reader {
	if b == nil {
		return http.NoBody
	}
	return bytes.NewReader(b)
}

// fixturePackage builds one real-shaped .rpm around a header builder (the
// parser's own fixture assembler, shared).
func fixturePackage(name string, main *headerBuilder) []byte {
	return fixtureRPM(name, nil, main, []byte("dummy-payload"))
}
