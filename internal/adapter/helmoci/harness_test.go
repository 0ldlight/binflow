package helmoci

// The real-stack test harness (the helm/cargo posture): sqlite metadata,
// the real storage engine, the real repo.Service, the real auth chain and
// the real httpapi router, with the docker /v2 plane and the helmoci shell
// under test mounted beside generic — every assertion crosses the full
// middleware chain (the /v2 root exception's dispatch, the license weave's
// /v2 arm, prefix stripping on the content plane).

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/lzwzzy/binflow/internal/adapter/docker"
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
	auth    *auth.Service
	license *license.Manager // non-nil on the licensed assembly
}

// stackOptions tunes the assembly (see newStackOpt).
type stackOptions struct {
	addons *addonsRegistrySeam // non-nil mounts the manifest + the D3 gate
	keys   *licenseKeys        // non-nil builds the real license.Manager
}

// newStack builds the default ungated stack (anonymous reads on, no addon
// question — the protocol legs in chain_test.go build this one).
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

	// The docker /v2 plane — the one handler BOTH the /v2 root exception
	// and the helmoci shell's content-plane delegation reach (the harness
	// mirrors cmd's assembly: one instance, two registrations).
	plane := docker.New(svc, docker.NewRepoLookup(md.Repos()),
		authSvc, authSvc, md.Users(), docker.Options{
			AnonymousAccess: cfg.Security.AnonymousAccess,
		}, nil).
		WithStorage(st, md.Blobs())
	handler := New(plane)

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
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), plane, handler},
		Version:   "1.0.0-test",
		Revision:  "t342",
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

// seedRepo writes one repository row directly through the metadata store
// (bypassing the creation validation the gate legs exercise through the
// REST plane instead).
func (s *stack) seedRepo(t *testing.T, key, class, packageType string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
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
	return s.do(http.MethodPut, path, adminUser, adminPass, bytes.NewReader(body), hdr)
}

// get issues an anonymous GET.
func (s *stack) get(path string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodGet, path, "", "", nil, nil)
}

// post issues an authenticated POST with a body.
func (s *stack) post(path string, body []byte, hdr map[string]string) (int, string, http.Header) {
	s.t.Helper()
	return s.do(http.MethodPost, path, adminUser, adminPass, bytes.NewReader(body), hdr)
}

// ---- the license-gate fixtures (the helm gate_test posture) ----

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
	return &licenseKeys{kid: "t342-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t342-" + tier,
		"licensee":  "T-342 test",
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
// import license/addons).
type addonsRegistrySeam struct {
	reg *addons.Registry
}

func newAddonsSeam() *addonsRegistrySeam {
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.Docker(), addons.HelmOCI())}
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

// newLicensedStack assembles the gated stack.
func newLicensedStack(t *testing.T) (*stack, *licenseKeys) {
	t.Helper()
	keys := newLicenseKeys(t)
	s := newStackOpt(t, stackOptions{addons: newAddonsSeam(), keys: keys})
	if s.license == nil {
		t.Fatal("licensed stack built without a Manager")
	}
	return s, keys
}

// createHelmOCIRepo issues the REST repository create (the D3 surface).
func (s *stack) createHelmOCIRepo(t *testing.T, key, rclass string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"helmoci"}`, key, rclass)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// ---- OCI push fixtures ----

// sha256HexOf renders one body's sha256 as bare lowercase hex.
func sha256HexOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
