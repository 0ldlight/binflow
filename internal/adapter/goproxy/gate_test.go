package goproxy

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The license-gating integration legs (T-285 task 4; the T-283 weave points
// exercised through the go pilot's own surface):
//
//   - D3: community tier (no document) refuses the go repository create
//     with the configuration-plane 400 naming the addon and tier;
//   - a pro document installed through the REAL license.Manager (test
//     keypair through the constructor seam — the stock embedded key stays
//     untouched) flips create to 200 and the PUT trio to 201;
//   - uninstall returns the write face to the gated refusal (403 +
//     X-Binflow-License-Required) while reads keep serving (D1);
//   - the internal-write exemption (the architect's T-283 risk bit): a GET
//     on a remote repository lands its pull-through copy under a gate that
//     denies every write verb — the gate is the HTTP verb face's, so the
//     engine's landing succeeds and the copy serves.
//
// The unit-level matrix is what this file pins; the live-instance pro form
// additionally needs the release keypair swap (T-281's flow) and stays a
// release-side leg.

// licenseKeys is one injected verify keypair (the ADR-mandated Manager
// constructor seam; tests never touch the embedded production key).
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
	return &licenseKeys{kid: "t285-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t285-" + tier,
		"licensee":  "T-285 test",
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
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.Go())}
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

// newLicensedStack assembles the gated stack: addon registry in Deps (the
// httpapi write gate live), a real Manager as the license collaborator, the
// D3 gate attached to repo.Service.
func newLicensedStack(t *testing.T) (*stack, *licenseKeys) {
	t.Helper()
	keys := newLicenseKeys(t)
	s := newStackOpt(t, stackOptions{addons: newAddonsSeam(), keys: keys})
	if s.license == nil {
		t.Fatal("licensed stack built without a Manager")
	}
	return s, keys
}

// createGoRepo issues the REST repository create (the D3 surface; the
// repositories plane mounts at /binflow/api/repositories/<key>).
func (s *stack) createGoRepo(t *testing.T, key, rclass, config string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"go"%s}`, key, rclass, config)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestGateD3CommunityRefusesCreate: no document installed -> the go create
// answers the configuration-plane 400 whose body names the addon and the
// tier gap (not a licensing 403 — ADR-0032's D3).
func TestGateD3CommunityRefusesCreate(t *testing.T) {
	s, _ := newLicensedStack(t)
	status, body := s.createGoRepo(t, "go-local", "local", "")
	if status != http.StatusBadRequest {
		t.Fatalf("community go create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"go", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}
}

// TestGateProLifecycle: install pro -> create 200 + PUT 201; uninstall ->
// PUT 403 with the X-Binflow-License-Required marker, GET keeps serving,
// create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	// Community: refused.
	if status, _ := s.createGoRepo(t, "go-local", "local", ""); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}

	// Install the pro document through the Manager (the REST face's
	// backing operation; the route's parsing is not this ticket's surface).
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createGoRepo(t, "go-local", "local", ""); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	// The PUT trio works under the license.
	zip := []byte("PK-fixture")
	if status, body, _ := s.put("/binflow/go-local/example.com/mymod/@v/v1.0.0.zip", zip, nil); status != http.StatusCreated {
		t.Fatalf("pro PUT .zip status = %d, want 201 (body %s)", status, body)
	}

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body, hdr := s.put("/binflow/go-local/example.com/mymod/@v/v1.0.1.zip", zip, nil)
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall PUT status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "go" {
		t.Errorf("X-Binflow-License-Required = %q, want go", got)
	}
	status, body, _ = s.get("/binflow/go-local/example.com/mymod/@v/v1.0.0.zip")
	if status != http.StatusOK || string(body) != "PK-fixture" {
		t.Errorf("post-uninstall GET .zip = (%d, %q), want the served copy", status, body)
	}
	if status, b2 := s.createGoRepo(t, "go-other", "local", ""); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}

// TestGateInternalWriteExemption: the T-283 risk bit through the go pilot's
// remote face — the pull-through landing a GET triggers must not consult
// the verb-face gate. The repository row is seeded directly (rows created
// while licensed keep serving after a downgrade; the D1 principle), the
// upstream is a loopback fake, and the gate denies every write the whole
// time (no document installed).
func TestGateInternalWriteExemption(t *testing.T) {
	s, _ := newLicensedStack(t)

	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/@v/v1.0.0.mod"):
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("module example.com/m\n"))
		case strings.HasSuffix(r.URL.Path, "/@v/v1.0.0.zip"):
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("PK-fixture"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)

	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.URL)

	// Sanity: the write face is gated (403 + marker).
	status, _, hdr := s.put("/binflow/go-remote/example.com/m/@v/v1.0.0.zip", []byte("PK"), nil)
	if status != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "go" {
		t.Fatalf("gated PUT on remote = %d (%q), want 403 + marker", status, hdr.Get("X-Binflow-License-Required"))
	}

	// The GET-triggered landing passes the gate by construction: two 200s,
	// one upstream contact (the second is the cache hit).
	for i := 0; i < 2; i++ {
		status, body, _ := s.get("/binflow/go-remote/example.com/m/@v/v1.0.0.mod")
		if status != http.StatusOK || body != "module example.com/m\n" {
			t.Fatalf("pull-through GET #%d = (%d, %q), want the upstream copy", i, status, body)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("upstream contacts = %d, want 1 (second read must be the cache hit)", n)
	}

	// The landed copy is a fact of the repository: the node row exists in
	// the remote namespace even though every write VERB is denied.
	nodes, err := s.md.Nodes().ListByPrefix(context.Background(), "go-remote", "")
	if err != nil {
		t.Fatalf("list remote nodes: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == "example.com/m/@v/v1.0.0.mod" {
			found = true
		}
	}
	if !found {
		t.Errorf("landed cache node missing; nodes = %+v", nodes)
	}
}
