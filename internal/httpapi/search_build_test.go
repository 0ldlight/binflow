package httpapi_test

// T-511 (FR-152.3 / aql.md §15): the build-family search surfaces on the
// wire — the three AQL entrances through the REAL engine + adapter stack,
// POST /api/search/buildArtifacts and GET /api/search/dependency with the
// §15.4 verbatim copy, the ACL zero-appearance probe end to end, and the
// NFR-P80 P95 leg over the record plane.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// buildSearchDoc renders one upload document. The artifact line carries the
// uploaded node's real sha256 so the association resolves (a disagreement
// lands record-only by design).
func buildSearchDoc(name, number, started, artifactPath, artifactSha256 string, deps [][3]string) string {
	var depLines []string
	for _, d := range deps {
		depLines = append(depLines, fmt.Sprintf(
			`{"type":"jar","sha1":%q,"sha256":%q,"id":%q,"scopes":["compile","test"]}`,
			d[0], d[1], d[2]))
	}
	var modules string
	if artifactPath != "" {
		modules = fmt.Sprintf(`"modules":[{"id":"com.example:api:1.0","type":"maven",
			"artifacts":[{"type":"jar","sha256":%q,"name":"api.jar","path":%q}],
			"dependencies":[%s]}]`, artifactSha256, artifactPath, strings.Join(depLines, ","))
	} else {
		modules = fmt.Sprintf(`"modules":[{"id":"com.example:core:1.0","type":"maven",
			"dependencies":[%s]}]`, strings.Join(depLines, ","))
	}
	return fmt.Sprintf(`{"version":"1.0.1","name":%q,"number":%q,"type":"GENERIC",
		"started":%q,"url":"https://ci.example.org/job/%s/%s",%s}`,
		name, number, started, name, number, modules)
}

// seedBuildSearchWorld uploads the corpus: a libs repository with one
// artifact node, then pub-app#51 (artifact + two dependencies) and
// secret-core#9 (one dependency) through the real upload face.
func seedBuildSearchWorld(t *testing.T, h *harness) (junitSha1, junitSha256 string) {
	t.Helper()
	seedRepo(t, h, "libs")
	content := []byte("build-search-artifact")
	sum := sha256.Sum256(content)
	artifactSha256 := fmt.Sprintf("%x", sum)
	resp := h.do(http.MethodPut, "/binflow/libs/pub-app/api.jar", adminUser, adminPass, content,
		map[string]string{"Content-Type": "application/octet-stream"})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("artifact upload = %d, want 201", resp.StatusCode)
	}

	junitSha1 = strings.Repeat("dd", 20)
	junitSha256 = strings.Repeat("ee", 32)
	otherSha1 := strings.Repeat("ff", 20)
	secretSha1 := strings.Repeat("ab", 20)

	for _, doc := range []string{
		buildSearchDoc("pub-app", "51", "2026-09-05T10:00:00.000+0000",
			"libs/pub-app/api.jar", artifactSha256,
			[][3]string{{junitSha1, junitSha256, "junit:junit:4.13"}, {otherSha1, strings.Repeat("aa", 32), "org:lib:2.0"}}),
		buildSearchDoc("secret-core", "9", "2026-09-06T10:00:00.000+0000",
			"", "", [][3]string{{secretSha1, strings.Repeat("cd", 32), "org:secret:1.0"}}),
	} {
		resp := putBuildDoc(t, h, adminUser, adminPass, doc)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("build upload = %d: %s", resp.StatusCode, body)
		}
	}
	return junitSha1, junitSha256
}

// newBuildSearchHarness wires the ACL world: bob r+w+d on pub-* builds
// (plus r on the whole libs repository — the artifact-address arm), carol
// r on pub-* builds only, eve nothing.
func newBuildSearchHarness(t *testing.T) *harness {
	t.Helper()
	h := newBuildHarness(t)
	// The artifact-address grant: `**` (Ant deep) — the artifact path is
	// two segments deep.
	grant(t, h, "libs-bob", "libs", "**", "bob", true, false, false)
	return h
}

// runAQL posts one AQL query and decodes the streaming envelope.
func runAQL(t *testing.T, h *harness, user, pass, query string) (int, map[string]any) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/search/aql", user, pass, []byte(query),
		map[string]string{"Content-Type": "text/plain"})
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read aql body: %v", err)
	}
	var env struct {
		Results []map[string]any `json:"results"`
		Range   struct {
			StartPos     json.Number `json:"start_pos"`
			EndPos       json.Number `json:"end_pos"`
			Notification string      `json:"notification"`
		} `json:"range"`
	}
	decodeErr := json.Unmarshal(body, &env)
	return resp.StatusCode, map[string]any{
		"raw": string(body), "results": env.Results, "range": env.Range, "decodeErr": decodeErr,
	}
}

// TestAQLBuildFamilyWire is AC1 over the real stack: the three entrances
// answer 200 with the §15.1 default members, include drives the
// dependency checksums, and the §6 identity triple renders on every
// non-admin row (with the masked identities).
func TestAQLBuildFamilyWire(t *testing.T) {
	h := newBuildSearchHarness(t)
	seedBuildSearchWorld(t, h)

	code, env := runAQL(t, h, adminUser, adminPass, `builds.find({"name":{"$match":"*"}})`)
	if code != http.StatusOK {
		t.Fatalf("builds.find = %d: %v", code, env["raw"])
	}
	results := env["results"].([]map[string]any)
	if len(results) != 2 {
		t.Fatalf("admin rows = %d, want 2 (pub-app + secret-core): %v", len(results), env["raw"])
	}
	first := results[0]
	for _, key := range []string{"url", "name", "number", "started", "repo", "created", "created_by", "modified", "modified_by"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("builds row missing default member %q: %v", key, first)
		}
	}
	if first["started"] != "2026-09-05T10:00:00.000Z" {
		t.Fatalf("started echo = %v, want the normalized ISO form", first["started"])
	}
	if first["url"] != "https://ci.example.org/job/pub-app/51" {
		t.Fatalf("url echo = %v", first["url"])
	}

	// modules + dependencies entrances.
	code, env = runAQL(t, h, adminUser, adminPass, `modules.find({})`)
	if code != http.StatusOK || len(env["results"].([]map[string]any)) != 2 {
		t.Fatalf("modules.find = %d rows=%v: %v", code, env["results"], env["raw"])
	}
	code, env = runAQL(t, h, adminUser, adminPass, `dependencies.find({})`)
	if code != http.StatusOK || len(env["results"].([]map[string]any)) != 3 {
		t.Fatalf("dependencies.find = %d rows=%v: %v", code, env["results"], env["raw"])
	}
	dep := env["results"].([]map[string]any)[0]
	if _, ok := dep["sha1"]; ok {
		t.Fatalf("sha1 rendered without include: %v", dep)
	}
	code, env = runAQL(t, h, adminUser, adminPass, `dependencies.find({}).include("name","sha1")`)
	if code != http.StatusOK {
		t.Fatalf("include query = %d: %v", code, env["raw"])
	}
	dep = env["results"].([]map[string]any)[0]
	if _, ok := dep["sha1"]; !ok {
		t.Fatalf("include(\"sha1\") missing: %v", dep)
	}
	if _, ok := dep["scope"]; ok {
		t.Fatalf("include suppressed member still rendered: %v", dep)
	}
}

// TestAQLBuildFamilyACLProbe is the AC3 wire probe: bob (pub-* builds)
// never sees a secret-core row on any entrance, the §6 triple renders on
// his rows, and the identities are masked.
func TestAQLBuildFamilyACLProbe(t *testing.T) {
	h := newBuildSearchHarness(t)
	seedBuildSearchWorld(t, h)

	for _, q := range []string{
		`builds.find({"name":{"$match":"*"}})`,
		`modules.find({})`,
		`dependencies.find({})`,
		`dependencies.find({"name":"org:secret:1.0"})`,
	} {
		code, env := runAQL(t, h, "bob", "pw", q)
		if code != http.StatusOK {
			t.Fatalf("%s = %d: %v", q, code, env["raw"])
		}
		for _, row := range env["results"].([]map[string]any) {
			if row["name"] == "secret-core" || row["number"] == "9" {
				t.Fatalf("%s: unauthorized build row appeared: %v", q, row)
			}
			if q[:6] == "builds" {
				if row["created_by"] != "unknown" || row["modified_by"] != "unknown" {
					t.Fatalf("identities unmasked for non-admin: %v", row)
				}
			}
		}
		if q[:6] == "builds" && len(env["results"].([]map[string]any)) != 1 {
			t.Fatalf("%s: bob rows = %d, want 1: %v", q, len(env["results"].([]map[string]any)), env["raw"])
		}
	}
	// eve sees nothing at all — the honest empty set, never an error.
	code, env := runAQL(t, h, "eve", "pw", `builds.find({})`)
	if code != http.StatusOK || len(env["results"].([]map[string]any)) != 0 {
		t.Fatalf("eve builds.find = %d rows=%v", code, env["results"])
	}
}

// TestSearchBuildArtifactsWire pins the §15.4 contract: the three verbatim
// 400s (the "your" typo included), the verbatim 404s, the LATEST sentinel,
// the status arm, repos[]/mappings[] and the ACL gates.
func TestSearchBuildArtifactsWire(t *testing.T) {
	h := newBuildSearchHarness(t)
	seedBuildSearchWorld(t, h)
	post := func(user, pass, body string) (*http.Response, string) {
		t.Helper()
		resp := h.do(http.MethodPost, "/binflow/api/search/buildArtifacts", user, pass, []byte(body),
			map[string]string{"Content-Type": "application/json"})
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(b)
	}

	// The three verbatim 400s.
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no-name", `{"buildNumber":"51"}`, "Cannot search without build name."},
		{"no-number-or-status", `{"buildName":"pub-app"}`, "Cannot search without build number or build status."},
		{"both-given", `{"buildName":"pub-app","buildNumber":"51","buildStatus":"staged"}`,
			"Cannot search with both build number and build status parameters, please omit build number if your are looking for latest build by status or omit build status to search for specific build version."},
	} {
		resp, body := post(adminUser, adminPass, tc.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", tc.name, resp.StatusCode)
		}
		var eb struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(body), &eb); err != nil || eb.Errors[0].Message != tc.want {
			t.Fatalf("%s message = %q (%v), want verbatim %q", tc.name, body, err, tc.want)
		}
	}

	// Anonymous: the privileged face's 401 challenge.
	resp, _ := post("", "", `{"buildName":"pub-app","buildNumber":"51"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", resp.StatusCode)
	}

	// The happy number arm: downloadUri rows, the direct content address.
	resp, body := post(adminUser, adminPass, `{"buildName":"pub-app","buildNumber":"51"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("number arm = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Results []struct {
			DownloadURI string `json:"downloadUri"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(out.Results) != 1 || !strings.HasSuffix(out.Results[0].DownloadURI, "/binflow/libs/pub-app/api.jar") {
		t.Fatalf("downloadUri rows = %+v", out.Results)
	}

	// The LATEST sentinel resolves the newest run of the name.
	resp, body = post(adminUser, adminPass, `{"buildName":"pub-app","buildNumber":"LATEST"}`)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "downloadUri") {
		t.Fatalf("LATEST arm = %d: %s", resp.StatusCode, body)
	}

	// The status arm without promotion history: the verbatim 404.
	resp, body = post(adminUser, adminPass, `{"buildName":"pub-app","buildStatus":"staged"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status arm = %d, want 404: %s", resp.StatusCode, body)
	}
	if want := "Could not find any build artifacts for build 'pub-app' status 'staged'"; !strings.Contains(body, want) {
		t.Fatalf("status 404 = %q, want %q", body, want)
	}

	// The number-miss 404.
	resp, body = post(adminUser, adminPass, `{"buildName":"pub-app","buildNumber":"999"}`)
	if resp.StatusCode != http.StatusNotFound ||
		!strings.Contains(body, "Could not find any build artifacts for build 'pub-app' number '999'") {
		t.Fatalf("number-miss = %d: %s", resp.StatusCode, body)
	}

	// repos[] narrows; an empty narrowing is the 404.
	resp, body = post(adminUser, adminPass, `{"buildName":"pub-app","buildNumber":"51","repos":["other-libs"]}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("repos-narrowed-miss = %d, want 404: %s", resp.StatusCode, body)
	}

	// mappings[] rewrites the path of the download address.
	resp, body = post(adminUser, adminPass,
		`{"buildName":"pub-app","buildNumber":"51","mappings":[{"input":"^pub-app/(.*)$","output":"mapped/$1"}]}`)
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(body, "/binflow/libs/mapped/api.jar") {
		t.Fatalf("mappings arm = %d: %s", resp.StatusCode, body)
	}

	// The build-domain gate: eve cannot read the build — 403, no oracle.
	resp, body = post("eve", "pw", `{"buildName":"pub-app","buildNumber":"51"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("eve = %d, want 403: %s", resp.StatusCode, body)
	}
	// The artifact-address arm: carol reads the build but not the
	// artifact's repository — the row is filtered, the empty set 404s.
	resp, body = post("carol", "pw", `{"buildName":"pub-app","buildNumber":"51"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("carol artifact-gate = %d, want 404 (unreadable artifact repo): %s", resp.StatusCode, body)
	}
	// bob holds both: 200.
	resp, body = post("bob", "pw", `{"buildName":"pub-app","buildNumber":"51"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bob = %d, want 200: %s", resp.StatusCode, body)
	}
}

// TestSearchDependencyWire pins the checksum reverse lookup: uri rows in
// the build-info form, the dedupe, the ACL gate and the 400 family.
func TestSearchDependencyWire(t *testing.T) {
	h := newBuildSearchHarness(t)
	junitSha1, junitSha256 := seedBuildSearchWorld(t, h)
	get := func(user, pass, query string) (*http.Response, string) {
		t.Helper()
		resp := h.do(http.MethodGet, "/binflow/api/search/dependency?"+query, user, pass, nil, nil)
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, string(b)
	}

	resp, _ := get("", "", "sha1="+junitSha1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", resp.StatusCode)
	}
	resp, body := get(adminUser, adminPass, "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "at least one of sha1 or sha256") {
		t.Fatalf("no-params = %d: %s", resp.StatusCode, body)
	}
	resp, body = get(adminUser, adminPass, "sha1=xyz")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "40 hex") {
		t.Fatalf("bad-hex = %d: %s", resp.StatusCode, body)
	}

	// The sha1 hit: one build uri.
	resp, body = get(adminUser, adminPass, "sha1="+junitSha1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sha1 hit = %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Results []struct {
			URI string `json:"uri"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode: %v (%s)", err, body)
	}
	if len(out.Results) != 1 || !strings.HasSuffix(out.Results[0].URI, "/binflow/api/build/pub-app/51") {
		t.Fatalf("uri rows = %+v", out.Results)
	}
	// The sha256 arm hits the same build.
	resp, body = get(adminUser, adminPass, "sha256="+junitSha256)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "pub-app") {
		t.Fatalf("sha256 hit = %d: %s", resp.StatusCode, body)
	}
	// A miss keeps the family's dominant empty arm.
	resp, body = get(adminUser, adminPass, "sha1="+strings.Repeat("99", 20))
	if resp.StatusCode != http.StatusOK || !emptyResults(t, body) {
		t.Fatalf("miss = %d: %s", resp.StatusCode, body)
	}
	// eve reads no build: the honest empty set.
	resp, body = get("eve", "pw", "sha1="+junitSha1)
	if resp.StatusCode != http.StatusOK || !emptyResults(t, body) {
		t.Fatalf("eve = %d: %s", resp.StatusCode, body)
	}
}

// emptyResults decodes a {"results": [...]} envelope and reports the empty
// set (writeJSONBody pretty-prints — a substring probe would pin spacing).
func emptyResults(t *testing.T, body string) bool {
	t.Helper()
	var env struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("decode results envelope: %v (%s)", err, body)
	}
	return len(env.Results) == 0
}

// TestBuildSearchP95 is the NFR-P80 leg: the single-build corpus (ten
// modules, a hundred dependencies) answers every entrance inside the 300ms
// P95 budget over the REAL stack — engine + adapter + SQLite through the
// HTTP entrance production serves.
func TestBuildSearchP95(t *testing.T) {
	h := newBuildHarness(t)
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	store := h.md.Builds()
	if err := store.PutBuild(ctx, &metadata.Build{
		Name: "perf-build", Number: "1", Started: "2026-09-07T00:00:00.000+0000",
		Repo: metadata.DefaultBuildRepo, CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
		Payload: `{"name":"perf-build","url":"https://ci/job/perf/1"}`,
	}); err != nil {
		t.Fatalf("seed build: %v", err)
	}
	mods := make([]*metadata.BuildModule, 10)
	for i := range mods {
		deps := make([]*metadata.BuildDependency, 10)
		for j := range deps {
			deps[j] = &metadata.BuildDependency{
				Seq: int64(j), ID: fmt.Sprintf("org.example:dep-%d-%d:1.0", i, j), Type: "jar",
				Scopes: "compile", Sha1: fmt.Sprintf("%040d", i*10+j),
			}
		}
		mods[i] = &metadata.BuildModule{ID: fmt.Sprintf("com.example:mod-%d:1.0", i), Dependencies: deps}
	}
	if err := store.PutModules(ctx, "perf-build", "1", "2026-09-07T00:00:00.000+0000",
		metadata.DefaultBuildRepo, mods); err != nil {
		t.Fatalf("seed modules: %v", err)
	}

	p95 := func(durations []time.Duration) time.Duration {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		return durations[(len(durations)*95)/100-1]
	}
	for _, q := range []string{
		`builds.find({"name":"perf-build"})`,
		`modules.find({"name":{"$match":"com.example:*"}})`,
		`dependencies.find({"scope":"compile"})`,
	} {
		var durations []time.Duration
		for i := 0; i < 40; i++ {
			start := time.Now()
			code, env := runAQL(t, h, adminUser, adminPass, q)
			durations = append(durations, time.Since(start))
			if code != http.StatusOK {
				t.Fatalf("%s = %d: %v", q, code, env["raw"])
			}
			if len(env["results"].([]map[string]any)) == 0 {
				t.Fatalf("%s: empty result", q)
			}
		}
		p := p95(durations)
		t.Logf("%s P95 = %v (n=%d)", q, p, len(durations))
		if p > 300*time.Millisecond {
			t.Fatalf("%s P95 = %v, want <= 300ms (NFR-P80)", q, p)
		}
	}
}
