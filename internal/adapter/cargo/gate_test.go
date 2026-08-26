package cargo

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

// The license-gating integration legs (the T-285/T-287 verification form,
// through the cargo adapter's own surface):
//
//   - D3: community tier (no document) refuses the cargo repository
//     create with the configuration-plane 400 naming the addon and tier;
//   - a pro document installed through the REAL license.Manager (test
//     keypair through the constructor seam) flips create to 200 and the
//     publish to 200;
//   - uninstall returns the write face to the gated refusal (403 +
//     X-Binflow-License-Required) while reads keep serving (D1).

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
	return &licenseKeys{kid: "t294-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t294-" + tier,
		"licensee":  "T-294 test",
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
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.Cargo())}
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
	s := newStackOpt(t, stackOptions{addons: newAddonsSeam(), keys: keys, anonymous: true})
	if s.license == nil {
		t.Fatal("licensed stack built without a Manager")
	}
	return s, keys
}

// createCargoRepo issues the REST repository create (the D3 surface).
func (s *stack) createCargoRepo(t *testing.T, key, rclass string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"cargo"}`, key, rclass)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestGateD3CommunityRefusesCreate: no document installed → the cargo
// create answers the D3 400 naming the addon and the tier gap.
func TestGateD3CommunityRefusesCreate(t *testing.T) {
	s, _ := newLicensedStack(t)
	status, body := s.createCargoRepo(t, "cargo-local", "local")
	if status != http.StatusBadRequest {
		t.Fatalf("community cargo create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"cargo", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}
}

// TestGateProLifecycle: install pro → create 200 + publish 200; uninstall
// → publish 403 with the X-Binflow-License-Required marker, reads keep
// serving (D1 + D2), create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	if status, _ := s.createCargoRepo(t, "cargo-local", "local"); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}

	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createCargoRepo(t, "cargo-local", "local"); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	crate := fixtureCrate("gated", "0.1.0")
	if status, body, _ := s.publish(t, "cargo-local", fixtureMeta("gated", "0.1.0"), crate); status != http.StatusOK {
		t.Fatalf("pro publish status = %d, want 200 (body %s)", status, body)
	}

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body, hdr := s.publish(t, "cargo-local", fixtureMeta("gated", "0.2.0"), fixtureCrate("gated", "0.2.0"))
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall publish status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "cargo" {
		t.Errorf("X-Binflow-License-Required = %q, want cargo", got)
	}
	status, body, _ = s.get(repoPath("cargo-local") + "/v1/crates/gated/0.1.0/download")
	if status != http.StatusOK || body != string(crate) {
		t.Errorf("post-uninstall download = (%d, %d bytes), want the served copy", status, len(body))
	}
	status, body, _ = s.get(repoPath("cargo-local") + "/index/ga/te/gated")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) {
		t.Errorf("post-uninstall index read = (%d, %s), want D1 keep-serving", status, body)
	}
	if status, b2 := s.createCargoRepo(t, "cargo-other", "local"); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}
