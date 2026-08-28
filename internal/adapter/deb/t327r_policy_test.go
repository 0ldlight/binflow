package deb

// T-327R (the D-B unlock, deb half): the by-hash policy family arrives
// through the repositories REST plane and drives the index engine. The
// ticket's "byHash=strong" spelling is not a legal value — the spec's enum
// is ALL / SHA256 / NONE (debian.md section 5, high confidence) — so this
// leg drives SHA256 (the policy that also trims the Release sections) and
// registers the wording difference in the T-327R report. Pre-T-327R the
// only write path for these keys was the test harness seeding the config
// blob directly; here the debian repository is CREATED by the REST PUT
// that carries them, on the licensed assembly the gate legs use.
//
// The configuration splits across two repositories on purpose: the by-hash
// retention buckets per algorithm directory and mixes an index family's
// plain + .gz + optional spellings, so a historyCycles below the
// spelling count prunes CURRENT-generation digests (registered in the
// T-327R report as an engine-side follow-up — out of this ticket's
// transport scope). Keeping the compression-family keys on a NONE-policy
// repository keeps this leg about what it tests: REST reachability.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
)

// restPutRepo issues the repositories PUT (create or update) with a JSON
// body through the full router.
func (s *stack) restPutRepo(t *testing.T, key, body string) (int, string) {
	t.Helper()
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// restGetRepoConfig fetches the stored configuration map ({} when empty).
func (s *stack) restGetRepoConfig(t *testing.T, key string) map[string]any {
	t.Helper()
	status, body, _ := s.do(http.MethodGet, "/binflow/api/repositories/"+key, adminUser, adminPass, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET repository %s = %d (body %s)", key, status, body)
	}
	var doc struct {
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("GET repository %s body does not parse: %v\n%s", key, err, body)
	}
	return doc.Configuration
}

// TestRestByHashConfigDrivesIndexEngine: the by-hash policy family over
// REST —
//
//  1. create carrying the keys, GET echoes each one (the round trip D-B
//     failed on: PUT 200, GET keyless);
//  2. debPUT lands and the async recompute renders Release with
//     Acquire-By-Hash: yes AND the configured Origin/Label (the fallback
//     is the repository key, so distinct values prove the keys drove it);
//  3. the by-hash tree serves the digest-addressed Packages copy and the
//     SHA256 policy trims the Release sections (section 4.1).
func TestRestByHashConfigDrivesIndexEngine(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}

	// 1. Create through the REST plane, policy keys and all.
	status, body := s.restPutRepo(t, "deb-rest",
		`{"rclass":"local","packageType":"debian",`+
			`"byHash":"SHA256","origin":"t327-origin","label":"t327-label"}`)
	if status != http.StatusOK {
		t.Fatalf("create = (%d, %s)", status, body)
	}
	conf := s.restGetRepoConfig(t, "deb-rest")
	for k, v := range map[string]any{
		"byHash": "SHA256",
		"origin": "t327-origin",
		"label":  "t327-label",
	} {
		if conf[k] != v {
			t.Errorf("configuration[%s] = %#v, want %#v", k, conf[k], v)
		}
	}

	// 2. The debPUT chain under the REST-configured policy.
	pkg := helloDeb("restcfg", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-rest/pool/main/r/restcfg/restcfg_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	release := s.waitIndex(t, "/binflow/deb-rest/dists/stable/Release")
	for _, want := range []string{
		"Acquire-By-Hash: yes\n",
		"Origin: t327-origin\n",
		"Label: t327-label\n",
	} {
		if !strings.Contains(release, want) {
			t.Errorf("Release missing %q:\n%s", want, release)
		}
	}
	if strings.Contains(release, "MD5Sum:") || strings.Contains(release, "\nSHA1:") {
		t.Errorf("byHash=SHA256 must trim the Release sections:\n%s", release)
	}

	// 3. The by-hash address of the plain Packages serves the same bytes.
	packages := s.waitIndex(t, "/binflow/deb-rest/dists/stable/main/binary-amd64/Packages")
	byHash := "/binflow/deb-rest/dists/stable/main/binary-amd64/by-hash/SHA256/" + sha256Hex([]byte(packages))
	if status, got, _ := s.get(byHash); status != http.StatusOK || got != packages {
		t.Errorf("by-hash address = (%d, %d bytes), want the canonical %d bytes", status, len(got), len(packages))
	}
	// The MD5Sum family stays absent under the SHA256 policy.
	if status, _, _ := s.get("/binflow/deb-rest/dists/stable/main/binary-amd64/by-hash/MD5Sum/x"); status != http.StatusNotFound {
		t.Errorf("MD5Sum by-hash family = %d, want 404 (SHA256 policy)", status)
	}
}

// TestRestCompressionAndArchConfigDrivesIndexEngine: the remaining deb
// keys over REST on a NONE-policy repository — the renderable optional
// compression (xz) lands its companion, the forced architecture family
// (s390x, no packages anywhere) still generates its empty Packages file
// (TL-4 via REST config), and historyCycles round-trips stored.
func TestRestCompressionAndArchConfigDrivesIndexEngine(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}

	status, body := s.restPutRepo(t, "deb-family",
		`{"rclass":"local","packageType":"debian",`+
			`"optionalIndexCompressionFormats":["xz"],"debianDefaultArchitectures":"amd64,s390x",`+
			`"historyCycles":5}`)
	if status != http.StatusOK {
		t.Fatalf("create = (%d, %s)", status, body)
	}
	conf := s.restGetRepoConfig(t, "deb-family")
	for k, v := range map[string]any{
		"historyCycles":                   float64(5),
		"optionalIndexCompressionFormats": []any{"xz"},
		"debianDefaultArchitectures":      "amd64,s390x",
	} {
		if got, ok := conf[k]; !ok || !equalJSONValue(got, v) {
			t.Errorf("configuration[%s] = %#v, want %#v", k, got, v)
		}
	}

	pkg := helloDeb("fam", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-family/pool/main/f/fam/fam_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-family/dists/stable/Release")

	// The renderable optional companion (xz).
	if status, xzBody, _ := s.get("/binflow/deb-family/dists/stable/main/binary-amd64/Packages.xz"); status != http.StatusOK {
		t.Errorf("Packages.xz = %d, want 200 (optionalIndexCompressionFormats via REST)", status)
	} else if !strings.Contains(string(unxz(t, []byte(xzBody))), "Package: fam") {
		t.Error("Packages.xz body is not the xz form of the index")
	}

	// The forced family with no packages still generates its file, and no
	// by-hash tree exists under the default NONE policy.
	if status, empty, _ := s.get("/binflow/deb-family/dists/stable/main/binary-s390x/Packages"); status != http.StatusOK {
		t.Errorf("forced s390x family Packages = %d, want 200 (debianDefaultArchitectures via REST)", status)
	} else if strings.TrimSpace(empty) != "" {
		t.Errorf("forced s390x Packages should be empty, got:\n%s", empty)
	}
	if status, _, _ := s.get("/binflow/deb-family/dists/stable/main/binary-amd64/by-hash/SHA256/x"); status != http.StatusNotFound {
		t.Errorf("by-hash tree = %d, want 404 (NONE policy, no Acquire-By-Hash)", status)
	}
}

// equalJSONValue compares one decoded JSON value against its want form
// (float64 numbers, exact strings, bools, arrays element-wise).
func equalJSONValue(got, want any) bool {
	switch w := want.(type) {
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !equalJSONValue(g[i], w[i]) {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
