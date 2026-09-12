package rpm

// T-327R (the D-B unlock, rpm half): the index-engine policy keys arrive
// through the repositories REST plane and DRIVE the engine. Pre-T-327R the
// only write path for calculateYumMetadata & co. was this package's test
// harness seeding the config blob directly (D-B: PUT 200 then GET keyless,
// the 409 branch unreachable over REST). This leg runs the licensed
// assembly so the rpm repository itself is created by the same REST PUT
// that carries the policy keys — the round trip and both engine effects
// (the auto-async 409, the filelists index) follow from that single write.

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/license"
)

// restPutRepo issues the repositories PUT (CREATE since ADR-0050 — PUT is
// create-only) with a JSON body through the full router.
func (s *stack) restPutRepo(t *testing.T, key, body string) (int, string) {
	t.Helper()
	status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
	return status, respBody
}

// restPostRepo issues the repositories POST — the UPDATE spelling since
// ADR-0050 (the body may omit rclass: the handler defaults it from the
// stored row).
func (s *stack) restPostRepo(t *testing.T, key, body string) (int, string) {
	t.Helper()
	status, respBody, _ := s.do(http.MethodPost, "/binflow/api/repositories/"+key, adminUser, adminPass,
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

// TestRestPolicyKeysDriveReindexBranches: one REST-carried configuration
// exercises the whole D-B surface —
//
//  1. create carrying the policy keys, GET echoes every key (the round
//     trip D-B failed on);
//  2. async=0 on the auto-calc repository is the pinned 409 (rpm.md
//     section 3.2), unreachable over REST before T-327R;
//  3. the upload chain recomputes on its own (calculateYumMetadata=true)
//     AND renders the filelists index (enableFileListsIndexing=true — the
//     key the integration docs' D-D note conditions);
//  4. flipping calculateYumMetadata off via REST turns the 409 back into
//     the synchronous 200 (the explicit-false pointer's whole point).
func TestRestPolicyKeysDriveReindexBranches(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if st := s.license.State(); !st.Licensed || st.Tier != license.TierPro {
		t.Fatalf("licensed state = %+v, want pro", st)
	}

	// 1. Create through the REST plane, policy keys and all.
	status, body := s.restPutRepo(t, "rpm-rest",
		`{"rclass":"local","packageType":"rpm",`+
			`"calculateYumMetadata":true,"enableFileListsIndexing":true,`+
			`"yumRootDepth":0,"yumGroupFileNames":"comps.xml"}`)
	if status != http.StatusOK {
		t.Fatalf("create = (%d, %s)", status, body)
	}
	conf := s.restGetRepoConfig(t, "rpm-rest")
	for k, v := range map[string]any{
		"calculateYumMetadata":    true,
		"enableFileListsIndexing": true,
		"yumRootDepth":            float64(0),
		"yumGroupFileNames":       "comps.xml",
	} {
		if conf[k] != v {
			t.Errorf("configuration[%s] = %v, want %v", k, conf[k], v)
		}
	}

	// 2. The auto-async 409 (the pinned wording, rpm.md section 3.2).
	status, body, _ = s.post("/binflow/api/yum/rpm-rest?async=0")
	if status != http.StatusConflict {
		t.Fatalf("sync reindex on REST-configured auto repo = (%d, %s), want 409", status, body)
	}
	if want := "Unable to perform immediate YUM metadata calculation on a repository with auto-async calculation enabled."; !strings.Contains(body, want) {
		t.Fatalf("409 body %q does not carry the pinned wording %q", body, want)
	}

	// 3. The upload chain: the opt-in recompute lands repomd by itself and
	// the filelists index joins the pair (D-D's conditional key, now
	// REST-reachable).
	pkg := pkgFixture("restcfg", "1.0.0", "1.el9", "x86_64")
	if status, b, _ := s.put("/binflow/rpm-rest/restcfg-1.0.0-1.el9.x86_64.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("package PUT = (%d, %s)", status, b)
	}
	var repomd string
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, repomd, _ = s.get("/binflow/rpm-rest/repodata/repomd.xml")
		if status == http.StatusOK && strings.Contains(repomd, `type="filelists"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("auto recompute never landed a filelists-bearing repomd (status %d):\n%s", status, repomd)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatalf("repomd does not parse: %v\n%s", err, repomd)
	}
	types := map[string]bool{}
	for _, d := range doc.Data {
		types[d.Type] = true
	}
	for _, want := range []string{"primary", "other", "filelists"} {
		if !types[want] {
			t.Errorf("repomd types = %v, want %s among them", types, want)
		}
	}

	// 4. Flip the opt-in off over REST (POST, the update spelling since
	// ADR-0050): the explicit false survives the round trip and the same
	// request answers the synchronous 200.
	if status, b := s.restPostRepo(t, "rpm-rest", `{"calculateYumMetadata":false}`); status != http.StatusOK {
		t.Fatalf("flip-off update = (%d, %s)", status, b)
	}
	conf = s.restGetRepoConfig(t, "rpm-rest")
	if conf["calculateYumMetadata"] != false {
		t.Fatalf("calculateYumMetadata after flip-off = %v, want explicit false", conf["calculateYumMetadata"])
	}
	status, body, _ = s.post("/binflow/api/yum/rpm-rest?async=0")
	if status != http.StatusOK || !strings.Contains(body, "accepted") {
		t.Fatalf("sync reindex after flip-off = (%d, %s), want the 200 accepted wording", status, body)
	}
}
