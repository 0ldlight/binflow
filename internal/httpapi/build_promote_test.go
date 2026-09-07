// The promote/retention REST family's wire contract (M17 T-509, FR-152.2):
// the promote endpoint matrix over the real stack — the 200 messages[]
// stream, the status flip visible on the detail face's statuses[], the
// promote verb's permission gate (the NFR-S81 403 probe, before existence),
// the honest error ladder (400/404/401), the docker promotion leg driven
// end-to-end over the REAL /v2 wire (push, promote, pull), the retention
// window's sync and async arms, and the builds.promote metric family.

package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/storage"
)

// newPromoteHarness seeds the promote/retention ACL world: frank the full
// promoter (r+w+d+a on the content repos, r+w+d on the build repo), gina the
// build-repo reader (no target w), eve nothing. The admin principal drives
// the fixtures.
func newPromoteHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessCfg(t, nil, [][2]string{
		{"frank", "pw"}, {"gina", "pw"}, {"eve", "pw"},
	})
	ctx := t.Context()
	now := time.Now().UTC().Format(time.RFC3339)
	put := func(name string, repos []string, principal string, r, w, d, a bool) {
		t.Helper()
		reposJSON, _ := json.Marshal(repos)
		if err := h.md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: string(reposJSON), Includes: `["**"]`, Excludes: `[]`,
			CreatedAt: now, UpdatedAt: now,
		}, []*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: r, CanWrite: w, CanDelete: d, CanAnnotate: a,
		}}); err != nil {
			t.Fatalf("PutTarget %s: %v", name, err)
		}
	}
	put("prm-content", []string{"dev-libs", "rel-libs", "dev-docker", "rel-docker"}, "frank", true, true, true, true)
	put("prm-build", []string{metadata.DefaultBuildRepo}, "frank", true, true, true, false)
	put("prm-build-read", []string{metadata.DefaultBuildRepo}, "gina", true, false, false, false)
	return h
}

// promoteViaREST issues the promote POST and drains the body.
func promoteViaREST(t *testing.T, h *harness, user, pass, name, number, body string) (*http.Response, string) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/build/promote/"+name+"/"+number,
		user, pass, []byte(body), map[string]string{"Content-Type": "application/json"})
	return resp, mustGet(t, resp)
}

// seedGenericNode lands one real node through the repository service (the
// storage engine commits the bytes, the ledger and node rows follow).
func seedGenericNode(t *testing.T, h *harness, repoKey, path, content, mime string) string {
	t.Helper()
	if _, err := h.svc.Put(t.Context(), &auth.Principal{
		Name: adminUser, Role: auth.RoleAdmin, Admin: true,
	}, repoKey, path, strings.NewReader(content), storage.BlobRef{}, mime); err != nil {
		t.Fatalf("seed node %s/%s: %v", repoKey, path, err)
	}
	return sha256Of([]byte(content))
}

// seedLocalGenericRepo creates one local generic repository.
func seedLocalGenericRepo(t *testing.T, h *harness, key string) {
	t.Helper()
	if err := h.md.Repos().Create(t.Context(), &metadata.Repo{
		RepoKey: key, Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: "2026-09-07T09:00:00Z", UpdatedAt: "2026-09-07T09:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// TestBuildRESTPromoteStatusFlipAndGenericMigration: the AC's core wire
// walk — POST promote (body status/targetRepo) answers 200 with the
// messages[] stream, the run's statuses[] flips on the detail face (the
// newest row first), the generic artifact MOVES to the target repository
// (the default), and body properties land on the promoted node.
func TestBuildRESTPromoteStatusFlipAndGenericMigration(t *testing.T) {
	h := newPromoteHarness(t)
	seedLocalGenericRepo(t, h, "dev-libs")
	seedLocalGenericRepo(t, h, "rel-libs")
	sha := seedGenericNode(t, h, "dev-libs", "wire-app/1.bin", "wire-payload", "application/octet-stream")

	doc := fmt.Sprintf(`{
	  "name": "wire-app", "number": "9", "type": "GENERIC",
	  "started": "2026-09-07T10:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [
	    {"type": "bin", "sha256": %q, "name": "1.bin", "path": "dev-libs/wire-app/1.bin"}
	  ]}]
	}`, sha)
	if resp := putBuildDoc(t, h, adminUser, adminPass, doc); resp.StatusCode != 200 {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}

	resp, body := promoteViaREST(t, h, "frank", "pw", "wire-app", "9",
		`{"status":"released","targetRepo":"rel-libs","ciUser":"jenkins","timestamp":"2026-09-07T12:00:01.000+0000","properties":{"release":"v9"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("promote = %d: %s", resp.StatusCode, body)
	}
	var promoted struct {
		Messages []struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &promoted); err != nil {
		t.Fatalf("promote JSON: %v (%s)", err, body)
	}
	if len(promoted.Messages) == 0 || promoted.Messages[len(promoted.Messages)-1].Level != "info" {
		t.Fatalf("promote messages = %+v, want a trailing info summary", promoted.Messages)
	}

	// The status flip: statuses[] on the detail face, newest first, the
	// promotion's six-tuple rendered.
	_, detail := getBuild(t, h, "/binflow/api/build/wire-app/9", adminUser, adminPass)
	for _, want := range []string{
		`"statuses"`, `"status": "released"`, `"ciUser": "jenkins"`,
		`"repository": "rel-libs"`,
	} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail after promote missing %s: %s", want, detail)
		}
	}

	// The artifact moved and the properties rode it.
	n, err := h.md.Nodes().Get(t.Context(), "rel-libs", "wire-app/1.bin")
	if err != nil {
		t.Fatalf("target node: %v", err)
	}
	if n.Sha256 != sha {
		t.Fatalf("target sha256 = %s, want %s", n.Sha256, sha)
	}
	if _, err := h.md.Nodes().Get(t.Context(), "dev-libs", "wire-app/1.bin"); err == nil {
		t.Fatal("source node survived the default move")
	}
	props, err := h.md.NodeProps().List(t.Context(), "rel-libs", "wire-app/1.bin")
	if err != nil || len(props["release"]) != 1 || props["release"][0] != "v9" {
		t.Fatalf("promoted properties = %v (err %v)", props, err)
	}
}

// TestBuildRESTPromoteACLAndErrorSurface: the promote verb's honest ladder —
// the 403 probe for a user without the gate (before existence, no oracle),
// the properties-arm annotate demand, the 404, the 400 family, anonymous
// 401, and the E-26 404 of the unrouted spellings.
func TestBuildRESTPromoteACLAndErrorSurface(t *testing.T) {
	h := newPromoteHarness(t)
	seedLocalGenericRepo(t, h, "dev-libs")
	seedLocalGenericRepo(t, h, "rel-libs")
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}

	// The gate: gina reads the build repo but holds no w on the target —
	// 403 for an existent AND a nonexistent run alike (no oracle).
	for _, run := range []string{"pub-app/51", "ghost-app/1"} {
		resp, body := promoteViaREST(t, h, "gina", "pw", run2name(run), run2number(run),
			`{"status":"released","targetRepo":"rel-libs"}`)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("gina promote %s = %d (%s), want 403", run, resp.StatusCode, body)
		}
	}
	// eve holds nothing at all: 403 with no oracle either.
	resp, _ := promoteViaREST(t, h, "eve", "pw", "pub-app", "51", `{"targetRepo":"rel-libs"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("eve promote = %d, want 403", resp.StatusCode)
	}
	// The missing run: the family's verbatim 404 (past the gate).
	resp, body := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "77", `{"status":"x"}`)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Build-Info not found") {
		t.Fatalf("missing run promote = %d %s", resp.StatusCode, body)
	}
	// The malformed body and the unparseable timestamp: the 400 family.
	cases := []struct {
		name   string
		body   string
		wantIn string
	}{
		{"not json", `{"status":`, "promotion body is not valid JSON"},
		{"bad timestamp", `{"timestamp":"soon"}`, "must be an ISO8601 timestamp"},
		{"virtual target", `{"targetRepo":"no-such"}`, "not found"},
		{"bad properties", `{"targetRepo":"rel-libs","properties":{"":"v"}}`, "promotion properties"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51", tc.body)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s = %d (%s), want 400", tc.name, resp.StatusCode, body)
			}
			if !strings.Contains(body, tc.wantIn) {
				t.Fatalf("body %q missing %q", body, tc.wantIn)
			}
		})
	}
	// Anonymous meets the 401 challenge at the route door.
	resp = h.do(http.MethodPost, "/binflow/api/build/promote/pub-app/51", "", "", []byte(`{}`), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous promote = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
	// Foreign verbs and the retention family's deeper tail keep the E-26 404.
	resp = h.do(http.MethodGet, "/binflow/api/build/promote/pub-app/51", adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET promote = %d, want the E-26 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = h.do(http.MethodPost, "/binflow/api/build/retention/pub-app/51", adminUser, adminPass, []byte(`{}`), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("retention with a number tail = %d, want the E-26 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// run2name / run2number split a "name/number" promote path for the table
// arms above.
func run2name(run string) string   { return run[:strings.IndexByte(run, '/')] }
func run2number(run string) string { return run[strings.IndexByte(run, '/')+1:] }

// TestBuildRESTPromoteDockerLegOverV2: the docker promotion leg, end to end
// over the REAL registry wire — a multi-arch image (an index citing two
// child manifests) pushed through /v2 into the dev repository, promoted
// through the build face, then PULLED back from the target repository: the
// tag resolves, the index body is byte-identical, both children and their
// config blobs serve, and the manifest digest set reconciles source-side
// (recorded before the move) against the target (§2.6's full-migration
// contract; the sha256 对账 the AC demands — on the wire).
func TestBuildRESTPromoteDockerLegOverV2(t *testing.T) {
	h := newPromoteHarness(t)
	seedDockerRepo(t, h, "dev-docker")
	seedDockerRepo(t, h, "rel-docker")
	image := "myapp"

	// Push two single-arch children BY DIGEST, then the index by tag.
	type child struct{ dgst, body string }
	var children []child
	for _, arch := range []string{"amd64", "arm64"} {
		layerDgst := v2SeedBlob(t, h, "dev-docker/"+image, []byte("layer-"+arch))
		cfgDgst := v2SeedBlob(t, h, "dev-docker/"+image, []byte(`{"architecture":"`+arch+`","os":"linux"}`))
		body := fmt.Sprintf(
			`{"schemaVersion":2,"mediaType":"`+ctDockerManifest+`",`+
				`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":35},`+
				`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":12}]}`,
			cfgDgst, layerDgst)
		resp := h.do(http.MethodPut, "/v2/dev-docker/"+image+"/manifests/sha256:"+sha256Of([]byte(body)),
			adminUser, adminPass, []byte(body), map[string]string{"Content-Type": ctDockerManifest})
		if out := mustGet(t, resp); resp.StatusCode != http.StatusCreated {
			t.Fatalf("child push (%s) = %d %s", arch, resp.StatusCode, out)
		}
		children = append(children, child{dgst: sha256Of([]byte(body)), body: body})
	}
	indexBody := fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+ctDockerList+`","manifests":[%s,%s]}`,
		fmt.Sprintf(`{"mediaType":"%s","digest":"sha256:%s","size":%d,"platform":{"architecture":"amd64","os":"linux"}}`,
			ctDockerManifest, children[0].dgst, len(children[0].body)),
		fmt.Sprintf(`{"mediaType":"%s","digest":"sha256:%s","size":%d,"platform":{"architecture":"arm64","os":"linux"}}`,
			ctDockerManifest, children[1].dgst, len(children[1].body)))
	root := sha256Of([]byte(indexBody))
	resp := h.do(http.MethodPut, "/v2/dev-docker/"+image+"/manifests/1",
		adminUser, adminPass, []byte(indexBody), map[string]string{"Content-Type": ctDockerList})
	if out := mustGet(t, resp); resp.StatusCode != http.StatusCreated {
		t.Fatalf("index push = %d %s", resp.StatusCode, out)
	}

	// The build associates the ROOT manifest; the promote migrates the
	// whole closure.
	doc := fmt.Sprintf(`{
	  "name": "img-wire", "number": "2", "type": "DOCKER",
	  "started": "2026-09-07T10:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [
	    {"type": "docker", "sha256": %q, "name": "myapp:1", "path": "dev-docker/%s/manifests/%s"}
	  ]}]
	}`, root, image, root)
	if resp := putBuildDoc(t, h, adminUser, adminPass, doc); resp.StatusCode != 200 {
		t.Fatalf("build upload = %d", resp.StatusCode)
	}
	presp, pbody := promoteViaREST(t, h, "frank", "pw", "img-wire", "2",
		`{"status":"released","targetRepo":"rel-docker"}`)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("docker promote = %d: %s", presp.StatusCode, pbody)
	}

	// PULL from the target: the tag serves the byte-identical index.
	getOK := func(path, accept string) (string, *http.Response) {
		t.Helper()
		resp := h.do(http.MethodGet, "/v2/rel-docker/"+path, adminUser, adminPass, nil,
			map[string]string{"Accept": accept})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("target GET %s = %d %s", path, resp.StatusCode, body)
		}
		return body, resp
	}
	gotIndex, iresp := getOK(image+"/manifests/1", ctDockerList)
	if gotIndex != indexBody {
		t.Fatalf("target index body differs from the pushed bytes")
	}
	if iresp.Header.Get("Docker-Content-Digest") != "sha256:"+root {
		t.Fatalf("target index digest = %s", iresp.Header.Get("Docker-Content-Digest"))
	}
	// Both children serve by digest, byte-identical — the closure walked.
	for _, c := range children {
		got, _ := getOK(image+"/manifests/sha256:"+c.dgst, ctDockerManifest)
		if got != c.body {
			t.Fatalf("target child %s body differs", c.dgst[:12])
		}
	}
	// The layer blobs followed (one probe per arch): the blob plane serves
	// them from the target repository.
	for _, arch := range []string{"amd64", "arm64"} {
		getOK(image+"/blobs/sha256:"+sha256Of([]byte("layer-"+arch)), "*/*")
	}
	// The move cleaned the source: the dev tag is gone (a pull there now
	// answers the registry's MANIFEST_UNKNOWN).
	sresp := h.do(http.MethodGet, "/v2/dev-docker/"+image+"/manifests/1",
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerList})
	sout := mustGet(t, sresp)
	if sresp.StatusCode != http.StatusNotFound || !strings.Contains(sout, "MANIFEST_UNKNOWN") {
		t.Fatalf("source tag after move = %d %s, want 404 MANIFEST_UNKNOWN", sresp.StatusCode, sout)
	}
}

// TestBuildRESTRetentionWindowOnTheWire: the retention face — async=false
// runs the window inline (200 empty, the runs visibly gone, the audit trail
// carrying one build.delete row per run), async=true (the default) also
// completes (polled), and the ladder answers 403/404 honestly.
func TestBuildRESTRetentionWindowOnTheWire(t *testing.T) {
	h := newPromoteHarness(t)
	// Three runs of one name; number 1 is outside the floor.
	seed := func(number, started string) {
		t.Helper()
		doc := fmt.Sprintf(`{
		  "name": "rwire-app", "number": %q, "type": "GENERIC", "started": %q,
		  "modules": [{"id": "m"}]
		}`, number, started)
		if resp := putBuildDoc(t, h, adminUser, adminPass, doc); resp.StatusCode != 200 {
			t.Fatalf("seed %s = %d", number, resp.StatusCode)
		}
	}
	seed("1", "2026-08-01T10:00:00.000+0000")
	seed("2", "2026-09-01T10:00:00.000+0000")
	seed("3", "2026-09-05T10:00:00.000+0000")

	post := func(user, pass, name, query string) (*http.Response, string) {
		t.Helper()
		resp := h.do(http.MethodPost, "/binflow/api/build/retention/"+name+query,
			user, pass, []byte(`{"count":2,"minimumBuildDate":"2026-09-01T00:00:00Z"}`),
			map[string]string{"Content-Type": "application/json"})
		return resp, mustGet(t, resp)
	}
	// The gate: gina (no d) 403s without an oracle; the unknown name 404s.
	for _, name := range []string{"rwire-app", "ghost-app"} {
		resp, _ := post("gina", "pw", name, "?async=false")
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("gina retention (%s) = %d, want 403", name, resp.StatusCode)
		}
	}
	resp, body := post(adminUser, adminPass, "ghost-app", "?async=false")
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "Build-Info not found") {
		t.Fatalf("unknown retention = %d %s", resp.StatusCode, body)
	}
	// The sync arm: number 1 (outside the floor, beyond count=2 is nobody —
	// only 3 runs) is deleted inline.
	resp, body = post(adminUser, adminPass, "rwire-app", "?async=false")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sync retention = %d %s", resp.StatusCode, body)
	}
	if strings.TrimSpace(body) != "" {
		t.Fatalf("retention body = %q, want the empty official form", body)
	}
	_, numbers := getBuild(t, h, "/binflow/api/build/rwire-app", adminUser, adminPass)
	if strings.Contains(numbers, `"/1"`) || !strings.Contains(numbers, `"/2"`) || !strings.Contains(numbers, `"/3"`) {
		t.Fatalf("runs after sync retention = %s, want 1 gone and 2,3 standing", numbers)
	}
	// The audit trail: the window's build.retention row and run 1's
	// build.delete row.
	auditOK := func(action string) bool {
		t.Helper()
		rows, err := h.md.Audits().Query(t.Context(), metadata.AuditQuery{Action: action, Limit: 10})
		return err == nil && len(rows) > 0
	}
	if !auditOK("build.retention") || !auditOK("build.delete") {
		t.Fatal("retention audit rows missing (build.retention / build.delete)")
	}

	// The async default: seed two more runs and let the detached window
	// complete (polled — the official async=true posture).
	seed("4", "2026-08-20T10:00:00.000+0000")
	resp, _ = post(adminUser, adminPass, "rwire-app", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("async retention = %d, want 200", resp.StatusCode)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, numbers := getBuild(t, h, "/binflow/api/build/rwire-app", adminUser, adminPass)
		if !strings.Contains(numbers, `"/4"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("async window never deleted run 4: %s", numbers)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestBuildRESTPromoteMetricFamily: the builds.promote family rides the
// standard /metrics scrape — the three pre-seeded outcome series plus the
// duration histogram after a real promotion.
func TestBuildRESTPromoteMetricFamily(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Metrics = metrics.NewRegistry()
	}, nil)
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}
	// A status-only promotion and a dry run.
	if resp, _ := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51", `{"status":"staged"}`); resp.StatusCode != 200 {
		t.Fatalf("status-only promote = %d", resp.StatusCode)
	}
	if resp, _ := promoteViaREST(t, h, adminUser, adminPass, "pub-app", "51", `{"dryRun":true}`); resp.StatusCode != 200 {
		t.Fatalf("dry-run promote = %d", resp.StatusCode)
	}
	scrapeResp, err := http.Get(h.srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	scrape, _ := io.ReadAll(scrapeResp.Body)
	_ = scrapeResp.Body.Close()
	text := string(scrape)
	for _, want := range []string{
		`binflow_builds_promote_total{outcome="status-only"} 1`,
		`binflow_builds_promote_total{outcome="dry-run"} 1`,
		`binflow_builds_promote_total{outcome="promoted"} 0`,
		`binflow_builds_promote_duration_seconds_count`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("scrape missing %s:\n%s", want, text)
		}
	}
}
