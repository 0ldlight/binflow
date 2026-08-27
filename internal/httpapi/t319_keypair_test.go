package httpapi_test

// T-319 REST acceptance (ADR-0038 / docs/design/gpg-keypair.md): the
// /api/security/keypair family's capability gates, the import/update/
// generate/list/get/delete matrix with the in-use guard, the verify faces
// (material and stored), the repo-keyed public key, the v2 association
// endpoints riding the repository update validation, and the honest 503
// on a plane-less stack. Wire literals (paths, KeyPairSummary fields, the
// "OK"/"Key was verified." texts) are the Artifactory REST spellings.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
)

// t319Gate is the unlocked-debian/rpm package-type gate the association
// legs need (the harness's repo.Service rides the static enum otherwise).
type t319Gate struct{}

func (t319Gate) Verdict(_ context.Context, packageType string) repo.PackageTypeVerdict {
	switch packageType {
	case "debian", "rpm", "helm":
		return repo.PackageTypeVerdict{Known: true, Unlocked: true}
	}
	return repo.PackageTypeVerdict{}
}

// t319Stack is the harness plus the wired keypair plane.
type t319Stack struct {
	*harness
	mgr *keypair.Manager
}

func newT319Stack(t *testing.T) *t319Stack {
	t.Helper()
	var stack *t319Stack
	h := newHarnessFull(t, nil, nil, nil, func(deps *httpapi.Deps) {
		c, err := remote.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("remote.NewCipher: %v", err)
		}
		mgr, err := keypair.NewManager(keypair.Options{
			Store:  deps.Metadata.GpgKeypairs(),
			Cipher: c,
			Repos:  deps.Metadata.Repos(),
		})
		if err != nil {
			t.Fatalf("keypair.NewManager: %v", err)
		}
		deps.Keypairs = mgr
		stack = &t319Stack{harness: nil, mgr: mgr}
	}, nil)
	repo.AttachPackageTypeGate(h.svc, t319Gate{})
	stack.harness = h
	return stack
}

func (s *t319Stack) do(t *testing.T, method, path, user, pass string, body []byte, contentType string) (*http.Response, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, s.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(raw)
}

// t319Admin is the admin principal of the repo-service calls.
func t319Admin() *auth.Principal { return &auth.Principal{Name: adminUser, Admin: true} }

// t319Generate drives the BinFlow-native keygen (RSA-2048 for speed) and
// returns the pair name.
func t319Generate(t *testing.T, s *t319Stack, name string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"pairName": name, "keyBits": 2048, "passphrase": "s3cret"})
	resp, raw := s.do(t, http.MethodPost, "/binflow/api/v1/admin/security/keypair/generate", adminUser, adminPass, body, "application/json")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("generate %s: %d %s", name, resp.StatusCode, raw)
	}
}

func TestT319Gates(t *testing.T) {
	s := newT319Stack(t)

	// Anonymous and unknown credentials cannot read the plane (the route's
	// required + CapSecurityRead gate).
	if resp, raw := s.do(t, http.MethodGet, "/binflow/api/security/keypair", "", "", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d %s", resp.StatusCode, raw)
	}
	if resp, raw := s.do(t, http.MethodGet, "/binflow/api/security/keypair", "nouser", "x", nil, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unknown user list = %d %s", resp.StatusCode, raw)
	}
	// Admin writes; the 401/403 pair on the write face.
	if resp, raw := s.do(t, http.MethodPost, "/binflow/api/security/keypair", "", "", []byte(`{}`), "application/json"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous import = %d %s", resp.StatusCode, raw)
	}
}

func TestT319CRUDMatrix(t *testing.T) {
	s := newT319Stack(t)

	// Generate → list → get → verify stored → associate → public by repo →
	// delete guard → disassociate → delete ok.
	t319Generate(t, s, "deb-signing")

	resp, raw := s.do(t, http.MethodGet, "/binflow/api/security/keypair", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"pairName": "deb-signing"`) {
		t.Fatalf("list = %d %s", resp.StatusCode, raw)
	}

	resp, raw = s.do(t, http.MethodGet, "/binflow/api/security/keypair/deb-signing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get = %d %s", resp.StatusCode, raw)
	}
	var summary keypair.Summary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatalf("get body: %v", err)
	}
	if summary.PairType != "GPG" || summary.PublicKey == "" || summary.Algorithm != "RSA-2048" {
		t.Fatalf("summary = %+v", summary)
	}
	for _, forbidden := range []string{"privateKey", "passphrase"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("get body carries %q: %s", forbidden, raw)
		}
	}

	resp, raw = s.do(t, http.MethodPost, "/binflow/api/security/keypair/verify", adminUser, adminPass,
		[]byte(`{"pairName":"deb-signing"}`), "application/json")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(raw) != "Key was verified." {
		t.Fatalf("verify stored = %d %q", resp.StatusCode, raw)
	}

	// Unknown pair: 404 on get/delete/verify.
	resp, _ = s.do(t, http.MethodGet, "/binflow/api/security/keypair/missing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get missing = %d", resp.StatusCode)
	}
	resp, _ = s.do(t, http.MethodDelete, "/binflow/api/security/keypair/missing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete missing = %d", resp.StatusCode)
	}

	// A debian repository + association via the v2 face.
	if _, err := s.svc.CreateRepo(context.Background(), t319Admin(), &metadata.Repo{
		RepoKey: "t319-deb", Type: repo.TypeLocal, PackageType: "debian", Config: "{}",
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	resp, raw = s.do(t, http.MethodPost, "/binflow/api/v2/repositories/t319-deb/keyPairs", adminUser, adminPass,
		[]byte("deb-signing"), "text/plain")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(raw) != "OK" {
		t.Fatalf("associate = %d %s", resp.StatusCode, raw)
	}

	// The repo-keyed public key answers the armored block.
	resp, raw = s.do(t, http.MethodGet, "/binflow/api/security/keypair/public/repositories/t319-deb", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatalf("public by repo = %d %s", resp.StatusCode, raw)
	}

	// The get echo names the referencing repository.
	resp, raw = s.do(t, http.MethodGet, "/binflow/api/security/keypair/deb-signing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK || !strings.Contains(raw, `"repositories"`) || !strings.Contains(raw, "t319-deb") {
		t.Fatalf("get with references = %d %s", resp.StatusCode, raw)
	}

	// In-use delete refuses with the 400 naming the repository.
	resp, raw = s.do(t, http.MethodDelete, "/binflow/api/security/keypair/deb-signing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "t319-deb") {
		t.Fatalf("in-use delete = %d %s", resp.StatusCode, raw)
	}

	// Disassociate (wrong name 404s), then delete answers the plain OK.
	resp, _ = s.do(t, http.MethodDelete, "/binflow/api/v2/repositories/t319-deb/keyPairs/wrong-name", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disassociate wrong name = %d", resp.StatusCode)
	}
	resp, raw = s.do(t, http.MethodDelete, "/binflow/api/v2/repositories/t319-deb/keyPairs/deb-signing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(raw) != "OK" {
		t.Fatalf("disassociate = %d %s", resp.StatusCode, raw)
	}
	resp, raw = s.do(t, http.MethodDelete, "/binflow/api/security/keypair/deb-signing", adminUser, adminPass, nil, "")
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(raw) != "OK" {
		t.Fatalf("delete = %d %s", resp.StatusCode, raw)
	}
}

func TestT319AssociationRidesRepoValidation(t *testing.T) {
	s := newT319Stack(t)
	t319Generate(t, s, "kp")

	// A helm repository: the association refuses (HL-4 — signing is a
	// debian/rpm local behavior).
	if _, err := s.svc.CreateRepo(context.Background(), t319Admin(), &metadata.Repo{
		RepoKey: "t319-helm", Type: repo.TypeLocal, PackageType: "helm", Config: "{}",
	}); err != nil {
		t.Fatalf("create helm repo: %v", err)
	}
	resp, raw := s.do(t, http.MethodPost, "/binflow/api/v2/repositories/t319-helm/keyPairs", adminUser, adminPass,
		[]byte("kp"), "text/plain")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "debian/rpm") {
		t.Fatalf("helm associate = %d %s", resp.StatusCode, raw)
	}

	// A dangling reference refuses (existence rule).
	if _, err := s.svc.CreateRepo(context.Background(), t319Admin(), &metadata.Repo{
		RepoKey: "t319-deb2", Type: repo.TypeLocal, PackageType: "debian", Config: "{}",
	}); err != nil {
		t.Fatalf("create deb repo: %v", err)
	}
	resp, raw = s.do(t, http.MethodPost, "/binflow/api/v2/repositories/t319-deb2/keyPairs", adminUser, adminPass,
		[]byte("no-such-pair"), "text/plain")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "does not exist") {
		t.Fatalf("dangling associate = %d %s", resp.StatusCode, raw)
	}

	// An empty body refuses.
	resp, _ = s.do(t, http.MethodPost, "/binflow/api/v2/repositories/t319-deb2/keyPairs", adminUser, adminPass,
		[]byte("  "), "text/plain")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty associate = %d", resp.StatusCode)
	}
}

func TestT319ImportValidationAndVaultRefusal(t *testing.T) {
	s := newT319Stack(t)

	// pairType RSA refuses with the scope note.
	resp, raw := s.do(t, http.MethodPost, "/binflow/api/security/keypair", adminUser, adminPass,
		[]byte(`{"pairName":"rsa","pairType":"RSA","alias":"a","privateKey":"x","publicKey":"y"}`), "application/json")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "out of M11 scope") {
		t.Fatalf("rsa import = %d %s", resp.StatusCode, raw)
	}

	// The vault fields refuse by name (the inert-field trap).
	resp, raw = s.do(t, http.MethodPost, "/binflow/api/security/keypair", adminUser, adminPass,
		[]byte(`{"pairName":"v","pairType":"GPG","alias":"a","privateKey":"x","publicKey":"y","vaultKey":"kv/keys/1"}`), "application/json")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "vault") {
		t.Fatalf("vault import = %d %s", resp.StatusCode, raw)
	}

	// Bad armor refuses.
	resp, _ = s.do(t, http.MethodPost, "/binflow/api/security/keypair", adminUser, adminPass,
		[]byte(`{"pairName":"bad","pairType":"GPG","alias":"a","privateKey":"not armor","publicKey":"not armor"}`), "application/json")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad armor import = %d", resp.StatusCode)
	}

	// PUT on an absent name answers 404 (the update face).
	material, err := keypair.GenerateKey(keypair.GenerateParams{KeyBits: 2048})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	imp := map[string]string{
		"pairName": "legit", "pairType": "GPG", "alias": "l",
		"privateKey": material.PrivateArmored, "publicKey": material.PublicArmored,
	}
	body, _ := json.Marshal(imp)
	resp, _ = s.do(t, http.MethodPut, "/binflow/api/security/keypair", adminUser, adminPass, body, "application/json")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update absent = %d", resp.StatusCode)
	}

	// POST imports (201), PUT then updates (200).
	resp, raw = s.do(t, http.MethodPost, "/binflow/api/security/keypair", adminUser, adminPass, body, "application/json")
	if resp.StatusCode != http.StatusCreated || !strings.Contains(raw, `"pairName": "legit"`) {
		t.Fatalf("import = %d %s", resp.StatusCode, raw)
	}
	resp, raw = s.do(t, http.MethodPut, "/binflow/api/security/keypair", adminUser, adminPass, body, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update = %d %s", resp.StatusCode, raw)
	}

	// Generate collision answers 409.
	t319Generate(t, s, "collide")
	body2, _ := json.Marshal(map[string]any{"pairName": "collide", "keyBits": 2048})
	resp, _ = s.do(t, http.MethodPost, "/binflow/api/v1/admin/security/keypair/generate", adminUser, adminPass, body2, "application/json")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("generate collide = %d", resp.StatusCode)
	}
}

func TestT319PlaneNotWiredAnswers503(t *testing.T) {
	h := newHarness(t) // no Deps.Keypairs
	resp, raw := t319Do(h, t, http.MethodGet, "/binflow/api/security/keypair", adminUser, adminPass)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unwired list = %d %s", resp.StatusCode, raw)
	}
}

func t319Do(h *harness, t *testing.T, method, path, user, pass string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(raw)
}
