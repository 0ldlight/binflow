package nuget

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

// The license-gating integration legs (the T-285 verification form,
// through the nuget pilot's own surface):
//
//   - D3: community tier (no document) refuses the nuget repository
//     create with the configuration-plane 400 naming the addon and tier;
//   - a pro document installed through the REAL license.Manager (test
//     keypair through the constructor seam) flips create to 200 and the
//     push to 201;
//   - uninstall returns the write face to the gated refusal (403 +
//     X-Binflow-License-Required) while reads keep serving (D1);
//   - the internal-write exemption (the architect's T-283 risk bit): a
//     GET on a remote repository lands its pull-through copy under a
//     gate that denies every write verb — the gate is the HTTP verb
//     face's, so the engine's landing succeeds and the copy serves.

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
	return &licenseKeys{kid: "t287-test", pub: pub, priv: priv}
}

// sign renders one document v1: <b64url(payloadJSON)>.<b64url(sig)>.
func (k *licenseKeys) sign(t *testing.T, tier string, expires *string) string {
	t.Helper()
	payload := map[string]any{
		"typ":       license.DocType,
		"alg":       license.DocAlg,
		"kid":       k.kid,
		"ver":       license.DocVersion,
		"licenseId": "t287-" + tier,
		"licensee":  "T-287 test",
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
	return &addonsRegistrySeam{reg: addons.New(addons.Generic(), addons.NuGet())}
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

// createNugetRepo issues the REST repository create (the D3 surface).
func (s *stack) createNugetRepo(t *testing.T, key, rclass, config string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"key":%q,"rclass":%q,"packageType":"nuget"%s}`, key, rclass, config)
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestGateD3CommunityRefusesCreate: no document installed → the nuget
// create answers the D3 400 naming the addon and the tier gap.
func TestGateD3CommunityRefusesCreate(t *testing.T) {
	s, _ := newLicensedStack(t)
	status, body := s.createNugetRepo(t, "ng-local", "local", "")
	if status != http.StatusBadRequest {
		t.Fatalf("community nuget create status = %d, want 400 (body %s)", status, body)
	}
	for _, token := range []string{"nuget", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}
}

// TestGateProLifecycle: install pro → create 200 + push 201; uninstall →
// push 403 with the X-Binflow-License-Required marker, reads keep
// serving, create returns to the D3 400.
func TestGateProLifecycle(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()

	if status, _ := s.createNugetRepo(t, "ng-local", "local", ""); status != http.StatusBadRequest {
		t.Fatalf("pre-install create status = %d, want 400", status)
	}

	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}
	if status, body := s.createNugetRepo(t, "ng-local", "local", ""); status != http.StatusOK {
		t.Fatalf("pro create status = %d, want 200 (body %s)", status, body)
	}

	pkg := buildNupkg(t, "Gated.Pkg", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-local", "gated.pkg", "1.0.0"), pkg.body, nil); status != http.StatusCreated {
		t.Fatalf("pro push status = %d, want 201 (body %s)", status, body)
	}

	// Uninstall: writes gated, reads keep serving (D1 + D2).
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body, hdr := s.put(pushPath("ng-local", "gated.pkg", "1.0.1"), pkg.body, nil)
	if status != http.StatusForbidden {
		t.Fatalf("post-uninstall push status = %d, want 403 (body %s)", status, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "nuget" {
		t.Errorf("X-Binflow-License-Required = %q, want nuget", got)
	}
	status, body, _ = s.get(packagePath("ng-local", "gated.pkg", "1.0.0", "nupkg"))
	if status != http.StatusOK || !strings.Contains(string(pkg.body[:4]), "PK") {
		// The zip magic is PK: the served copy must be byte-identical.
		t.Errorf("post-uninstall GET = (%d, len %d), want the served copy", status, len(body))
	}
	if status, body, _ := s.get(packagePath("ng-local", "gated.pkg", "1.0.0", "nupkg")); status != http.StatusOK || body != string(pkg.body) {
		t.Errorf("post-uninstall GET body differs (len %d vs %d)", len(body), len(pkg.body))
	}
	if status, b2 := s.createNugetRepo(t, "ng-other", "local", ""); status != http.StatusBadRequest {
		t.Errorf("post-uninstall create status = %d, want 400 (body %s)", status, b2)
	}
}

// TestGateInternalWriteExemption: the pull-through landing a GET triggers
// must not consult the verb-face gate (the repository row is seeded
// directly — rows created while licensed keep serving after a downgrade;
// the D1 principle — and the gate denies every write the whole time).
func TestGateInternalWriteExemption(t *testing.T) {
	s, _ := newLicensedStack(t)

	pkg := buildNupkg(t, "Rem.Pkg", "1.0.0", flatDeps("none"))
	var hits atomic.Int64
	var upSrv *httptest.Server
	upSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+remNupkgPath("rem.pkg", "1.0.0")):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(pkg.body)
		case strings.HasSuffix(r.URL.Path, "/v3-flatcontainer/rem.pkg/index.json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"versions":["1.0.0"]}`))
		case strings.HasSuffix(r.URL.Path, "/"+upstreamRegistrationPrefix+"/rem.pkg/index.json"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(upstreamRegistration("rem.pkg", "Rem.Pkg", "1.0.0", upSrv.URL, pkg))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upSrv.Close)

	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "ng-remote", upSrv.URL)

	// Sanity: the write face is gated (403 + marker).
	p2 := buildNupkg(t, "Gated.Pkg", "1.0.0", flatDeps("none"))
	status, _, hdr := s.put(pushPath("ng-remote", "gated.pkg", "1.0.0"), p2.body, nil)
	if status != http.StatusForbidden || hdr.Get("X-Binflow-License-Required") != "nuget" {
		t.Fatalf("gated push on remote = %d (%q), want 403 + marker", status, hdr.Get("X-Binflow-License-Required"))
	}

	// The GET-triggered landing passes the gate by construction: two 200s,
	// one upstream contact (the second is the cache hit).
	for i := 0; i < 2; i++ {
		status, body, _ := s.get(apiPath("ng-remote") + "/registration/rem.pkg/index.json")
		if status != http.StatusOK || !strings.Contains(body, "\"Rem.Pkg\"") {
			t.Fatalf("pull-through registration GET #%d = (%d, %s…), want the rewritten upstream copy",
				i, status, firstLine(body))
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("upstream contacts = %d, want 1 (second read must be the cache hit)", n)
	}

	// The landed copy is a fact of the repository.
	nodes, err := s.md.Nodes().ListByPrefix(context.Background(), "ng-remote", "")
	if err != nil {
		t.Fatalf("list remote nodes: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == regMarker("rem.pkg") {
			found = true
		}
	}
	if !found {
		t.Errorf("landed registration marker missing; nodes = %+v", nodes)
	}
}

// remNupkgPath is the upstream flatcontainer nupkg path (the prefix the
// provider joins).
func remNupkgPath(id, version string) string {
	return upstreamFlatPrefix + "/" + id + "/" + version + "/" + id + "." + version + suffixNupkg
}

// upstreamRegistration renders one fake upstream registration document
// whose embedded URLs point at the fake upstream (the rewrite input).
func upstreamRegistration(id, displayID, version, base string, pkg *nupkgFixture) []byte {
	doc := `{
 "count": 1,
 "items": [{
  "@id": "` + base + `/v3/registration5-gz-semver2/` + id + `/index.json",
  "@type": "catalog:CatalogPage",
  "count": 1, "lower": "` + version + `", "upper": "` + version + `",
  "items": [{
   "@id": "` + base + `/v3/registration5-gz-semver2/` + id + `/` + version + `.json",
   "catalogEntry": {
    "id": "` + displayID + `", "version": "` + version + `",
    "description": "upstream fixture",
    "packageContent": "` + base + `/v3-flatcontainer/` + id + `/` + version + `/` + id + `.` + version + `.nupkg",
    "packageHash": "` + pkg.sha512 + `",
    "packageHashAlgorithm": "SHA512"
   },
   "packageContent": "` + base + `/v3-flatcontainer/` + id + `/` + version + `/` + id + `.` + version + `.nupkg"
  }]
 }]
}`
	return []byte(doc)
}
