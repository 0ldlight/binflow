package helm

// T-329 D-E (the D-E unlock, helm half): the Enforce Layout switches arrive
// through the repositories REST plane and DRIVE the upload hook's 403s.
// Pre-D-E the only write path for forceMetadataNameVersion /
// forceNonDuplicateChart was this package's test harness seeding the config
// blob directly (TestEnforceLayoutMatrix), so enforce could never be
// switched on in production — T-329's L32 arm. This leg runs the licensed
// assembly (helm is a pro-tier slot) so the repository is created by the
// same REST PUT that carries the switches; the verbatim 403 wordings and
// the flip-off round trip all follow from that single write.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/license"
)

// restPutEnforceRepo issues the repositories PUT (CREATE since ADR-0050 —
// PUT is create-only) with a JSON body through the full router.
func (s *stack) restPutEnforceRepo(t *testing.T, key, body string) (int, string) {
	t.Helper()
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// restPostEnforceRepo issues the repositories POST — the UPDATE spelling
// since ADR-0050 (the body may omit rclass: the handler defaults it from
// the stored row).
func (s *stack) restPostEnforceRepo(t *testing.T, key, body string) (int, string) {
	t.Helper()
	status, respBody, _ := s.do(http.MethodPost, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// TestRestEnforceSwitchesDriveUploadHook: one REST-carried configuration
// exercises the whole D-E surface —
//
//  1. create carrying both switches, GET echoes both (the round trip
//     D-E failed on);
//  2. the mismatched-filename upload answers the verbatim 403 (switch one);
//  3. the canonical filename lands 201, and a SECOND path for the same
//     name+version answers the verbatim duplicate 403 (switch two, the
//     filename arm both-switches-on selects);
//  4. flipping both switches off over REST (explicit false) returns the
//     upload face to 201 — the production-plane disable D-E made
//     impossible.
func TestRestEnforceSwitchesDriveUploadHook(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}

	// 1. Create through the REST plane, switches and all (helm is a pro
	// slot, hence the licensed stack).
	status, body := s.restPutEnforceRepo(t, "helm-rest-enf",
		`{"rclass":"local","packageType":"helm",`+
			`"forceMetadataNameVersion":true,"forceNonDuplicateChart":true}`)
	if status != http.StatusOK {
		t.Fatalf("create = (%d, %s)", status, body)
	}
	gStatus, gBody, _ := s.do(http.MethodGet, "/binflow/api/repositories/helm-rest-enf", adminUser, adminPass, nil, nil)
	if gStatus != http.StatusOK {
		t.Fatalf("GET repository = (%d, %s)", gStatus, gBody)
	}
	for _, want := range []string{`"forceMetadataNameVersion": true`, `"forceNonDuplicateChart": true`} {
		if !strings.Contains(gBody, want) {
			t.Errorf("GET configuration %q does not echo %s", gBody, want)
		}
	}

	// 2. Switch one: the mismatched filename is refused with the pinned
	// wording (helm.md section 4.3, letter for letter).
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "0.1.0"), nil)
	status, body, _ = s.put("/binflow/helm-rest-enf/wrong-name.tgz", chart, nil)
	if status != http.StatusForbidden {
		t.Fatalf("mismatched-filename PUT = (%d, %s), want 403", status, body)
	}
	if want := "This action is prevented due to the Enforce Layout Policy, the metadata of the package helm-rest-enf/wrong-name.tgz could not be read or is malformed."; body != want+"\n" {
		t.Fatalf("403 body = %q, want the verbatim policy wording %q", body, want)
	}
	// The refusal left nothing behind (the hook fires before the landing).
	if st, _, _ := s.get("/binflow/helm-rest-enf/wrong-name.tgz"); st != http.StatusNotFound {
		t.Fatalf("refused upload left a node behind: GET = %d, want 404", st)
	}

	// 3. The canonical filename lands; the second path for the same
	// name+version is the duplicate 403.
	if status, body, _ = s.put("/binflow/helm-rest-enf/mychart-0.1.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("canonical PUT = (%d, %s), want 201", status, body)
	}
	status, body, _ = s.put("/binflow/helm-rest-enf/subdir/mychart-0.1.0.tgz", chart, nil)
	if status != http.StatusForbidden {
		t.Fatalf("duplicate-path PUT = (%d, %s), want 403", status, body)
	}
	if want := "This action is prevented due to the Enforce Layout Policy, a package with the same name and version mychart-0.1.0 already exists in the repository."; body != want+"\n" {
		t.Fatalf("403 body = %q, want the verbatim policy wording %q", body, want)
	}

	// 4. Flip both switches off over REST (POST, the update spelling since
	// ADR-0050): the explicit false pair survives the round trip and the
	// very upload that was refused now lands.
	if status, body = s.restPostEnforceRepo(t, "helm-rest-enf",
		`{"forceMetadataNameVersion":false,"forceNonDuplicateChart":false}`); status != http.StatusOK {
		t.Fatalf("flip-off update = (%d, %s)", status, body)
	}
	gStatus, gBody, _ = s.do(http.MethodGet, "/binflow/api/repositories/helm-rest-enf", adminUser, adminPass, nil, nil)
	if gStatus != http.StatusOK {
		t.Fatalf("GET repository after flip-off = (%d, %s)", gStatus, gBody)
	}
	for _, want := range []string{`"forceMetadataNameVersion": false`, `"forceNonDuplicateChart": false`} {
		if !strings.Contains(gBody, want) {
			t.Errorf("GET configuration after flip-off %q does not echo %s", gBody, want)
		}
	}
	if status, body, _ = s.put("/binflow/helm-rest-enf/wrong-name.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("post-flip-off mismatched-filename PUT = (%d, %s), want 201", status, body)
	}
}
