package deb

// The license-gating integration legs (the T-285..T-311 verification
// form, through the deb adapter's own surface):
//
//   - D3: community tier (no document) refuses the debian repository
//     create with the configuration-plane 400 naming the addon and the
//     tier gap;
//   - a pro document installed through the REAL license.Manager (test
//     keypair through the constructor seam) flips create to 200 and the
//     debPUT to 201;
//   - uninstall returns the write face to the gated refusal (403 +
//     X-Binflow-License-Required: debian) while reads keep serving (D1).

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/repo"
)

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
	return &licenseKeys{kid: "t310-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t310-" + tier,
		"licensee":  "T-310 test",
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

// addonsRegistrySeam bundles the assembled registry with the gate
// adapter (cmd's packageTypeGate shape, mirrored test-side because repo
// must not import license/addons).
type addonsRegistrySeam struct {
	reg *addons.Registry
}

func newAddonsSeam() *addonsRegistrySeam {
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.Debian())}
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

// createDebRepo issues the REST repository create (the D3 surface).
func (s *stack) createDebRepo(t *testing.T, key, rclass string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"debian"}`, key, rclass)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestGateD3CommunityRefusesCreate: no document installed → the debian
// create answers the D3 400 naming the addon and the tier gap.
func TestGateD3CommunityRefusesCreate(t *testing.T) {
	s, _ := newLicensedStack(t)
	status, body := s.createDebRepo(t, "deb-local", "local")
	if status != http.StatusBadRequest {
		t.Fatalf("community deb create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"debian", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}
}

// TestGateProLifecycle: install pro → create 200 + debPUT 201;
// uninstall → debPUT 403 with the X-Binflow-License-Required marker
// while reads keep serving (D1 + D2), create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	if status, _ := s.createDebRepo(t, "deb-local", "local"); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}

	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createDebRepo(t, "deb-local", "local"); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	pkg := helloDeb("gated", "1.0", "amd64")
	path := "/binflow/deb-local/pool/main/g/gated/gated_1.0_amd64.deb"
	if status, body, _ := s.debPut(t, path, pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("pro debPUT status = %d, want 201 (body %s)", status, body)
	}
	s.waitIndex(t, "/binflow/deb-local/dists/stable/main/binary-amd64/Packages")

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	pkg2 := helloDeb("gated", "2.0", "amd64")
	status, body, hdr := s.debPut(t, path, pkg2, "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall debPUT status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "debian" {
		t.Errorf("X-Binflow-License-Required = %q, want debian", got)
	}
	status, body, _ = s.get(path)
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(pkg) {
		t.Errorf("post-uninstall download = (%d, %d bytes), want the served copy", status, len(body))
	}
	if status, b2 := s.createDebRepo(t, "deb-other", "local"); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}
