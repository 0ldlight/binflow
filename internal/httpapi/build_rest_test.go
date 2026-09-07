// The build REST family's wire contract (M17 T-508, FR-152.2): the
// endpoint matrix over the real stack — upload/echo round-trip, the append
// merge (key = module id, never an overwrite), the query ladder's
// list/numbers/detail shapes with relative URIs and the started-default =
// latest run, the honest error surface (self-frozen 400 wording, the
// spec-verbatim 404, denial-before-existence 403), the zero-leak arms
// (NFR-S80 on the wire) and the builds.put/builds.get metric family.

package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
)

// buildRESTDoc is the canonical wire document of the suite: two modules
// (artifacts + dependencies + scopes), properties and uninterpreted riders
// (buildAgent, url) the payload echo must carry through.
const buildRESTDoc = `{
  "version": "1.0.1",
  "name": "pub-app",
  "number": "51",
  "type": "GENERIC",
  "started": "2026-09-07T10:00:00.000+0000",
  "buildAgent": {"name": "jenkins", "version": "2.4"},
  "url": "https://ci.example.org/job/pub-app/51",
  "modules": [
    {
      "id": "com.example:api:1.0",
      "type": "maven",
      "artifacts": [
        {"type": "jar", "sha1": "aa", "sha256": "bb", "md5": "cc",
         "name": "api-1.0.jar", "path": "libs/pub-app/api-1.0.jar"}
      ],
      "dependencies": [
        {"type": "jar", "sha1": "dd", "id": "junit:junit:4.13", "scopes": ["test"]}
      ]
    },
    {
      "id": "com.example:web:1.0",
      "type": "maven",
      "dependencies": []
    }
  ],
  "properties": {"env": "prod"}
}`

// newBuildHarness seeds the ACL world of the wire arms: bob r+w+d on
// pub-*, carol r on pub-*, eve nothing (the names/numbers/echo fixtures
// ride the admin principal).
func newBuildHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarnessCfg(t, nil, [][2]string{
		{"bob", "pw"}, {"carol", "pw"}, {"eve", "pw"},
	})
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	put := func(name string, principal string, r, w, d bool) {
		t.Helper()
		if err := h.md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name:      name,
			Repos:     `["` + metadata.DefaultBuildRepo + `"]`,
			Includes:  `["pub-*"]`,
			Excludes:  `[]`,
			CreatedAt: now, UpdatedAt: now,
		}, []*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: r, CanWrite: w, CanDelete: d,
		}}); err != nil {
			t.Fatalf("PutTarget %s: %v", name, err)
		}
	}
	put("pub-full", "bob", true, true, true)
	put("pub-read", "carol", true, false, false)
	return h
}

// putBuildDoc issues the upload PUT.
func putBuildDoc(t *testing.T, h *harness, user, pass, doc string) *http.Response {
	t.Helper()
	return h.do(http.MethodPut, "/binflow/api/build", user, pass, []byte(doc),
		map[string]string{"Content-Type": "application/json"})
}

// getBuild issues one GET under the family and drains the body.
func getBuild(t *testing.T, h *harness, path, user, pass string) (*http.Response, string) {
	t.Helper()
	resp := h.do(http.MethodGet, path, user, pass, nil, nil)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	return resp, string(body)
}

// TestBuildRESTUploadAndEchoRoundTrip: PUT answers 200 EMPTY, the detail
// GET echoes the interpreted truth (canonical started, modules with
// artifacts/dependencies, properties) AND the payload's uninterpreted
// riders (buildAgent, url).
func TestBuildRESTUploadAndEchoRoundTrip(t *testing.T) {
	h := newBuildHarness(t)

	resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.TrimSpace(string(body)) != "" {
		t.Fatalf("upload body = %q, want the official EMPTY success body", body)
	}

	resp, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail = %d: %s", resp.StatusCode, doc)
	}
	var detail struct {
		URI       string `json:"uri"`
		BuildInfo struct {
			Name       string `json:"name"`
			Number     string `json:"number"`
			Started    string `json:"started"`
			Type       string `json:"type"`
			BuildAgent struct {
				Name string `json:"name"`
			} `json:"buildAgent"`
			URL        string            `json:"url"`
			Properties map[string]string `json:"properties"`
			Modules    []struct {
				ID        string `json:"id"`
				Artifacts []struct {
					Name string `json:"name"`
					Path string `json:"path"`
				} `json:"artifacts"`
				Dependencies []struct {
					ID     string   `json:"id"`
					Scopes []string `json:"scopes"`
				} `json:"dependencies"`
			} `json:"modules"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(doc), &detail); err != nil {
		t.Fatalf("detail JSON: %v (%s)", err, doc)
	}
	if want := "/api/build/pub-app/51"; detail.URI != want {
		t.Fatalf("uri = %q, want %q", detail.URI, want)
	}
	bi := detail.BuildInfo
	if bi.Name != "pub-app" || bi.Number != "51" || bi.Type != "GENERIC" {
		t.Fatalf("header echo = %s#%s type %s", bi.Name, bi.Number, bi.Type)
	}
	if bi.Started != "2026-09-07T10:00:00.000+0000" {
		t.Fatalf("started echo = %q, want the canonical literal", bi.Started)
	}
	if bi.BuildAgent.Name != "jenkins" || !strings.HasPrefix(bi.URL, "https://ci.example.org") {
		t.Fatalf("payload riders lost: %+v / %q", bi.BuildAgent, bi.URL)
	}
	if bi.Properties["env"] != "prod" {
		t.Fatalf("properties echo = %+v", bi.Properties)
	}
	if len(bi.Modules) != 2 {
		t.Fatalf("modules echo = %+v", bi.Modules)
	}
	api := bi.Modules[0]
	if api.ID != "com.example:api:1.0" || len(api.Artifacts) != 1 ||
		api.Artifacts[0].Name != "api-1.0.jar" {
		t.Fatalf("api module echo = %+v", api)
	}
	if len(api.Dependencies) != 1 || api.Dependencies[0].ID != "junit:junit:4.13" ||
		len(api.Dependencies[0].Scopes) != 1 || api.Dependencies[0].Scopes[0] != "test" {
		t.Fatalf("dependencies echo = %+v", api.Dependencies)
	}
	// The artifact's path names no live node: record-only, the echo carries
	// no association form.
	if api.Artifacts[0].Path != "" {
		t.Fatalf("unresolvable artifact echoed a path: %q", api.Artifacts[0].Path)
	}
	// A module without segments still echoes empty ARRAYS, never null.
	if bi.Modules[1].Artifacts == nil || bi.Modules[1].Dependencies == nil {
		t.Fatalf("empty module segments must echo [], got null: %+v", bi.Modules[1])
	}
}

// TestBuildRESTAppendMergesByIDOnTheWire: the AC's merge assertion — a
// second publish carrying a dependencies section MERGES into the same
// module by id (204 empty), and the GET face shows the union with the
// original rows intact.
func TestBuildRESTAppendMergesByIDOnTheWire(t *testing.T) {
	h := newBuildHarness(t)
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != http.StatusOK {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}

	appendBody := `[{
	  "id": "com.example:api:1.0",
	  "artifacts": [{"type": "pom", "sha1": "11", "name": "api-1.0.pom"}],
	  "dependencies": [{"type": "jar", "sha1": "22", "id": "org:lib:2.0", "scopes": ["compile"]}]
	}]`
	resp := h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51",
		adminUser, adminPass, []byte(appendBody),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("append = %d, want 204", resp.StatusCode)
	}
	ab, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.TrimSpace(string(ab)) != "" {
		t.Fatalf("append body = %q, want EMPTY", ab)
	}

	_, doc := getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	var detail struct {
		BuildInfo struct {
			Modules []struct {
				ID           string            `json:"id"`
				Artifacts    []json.RawMessage `json:"artifacts"`
				Dependencies []struct {
					ID string `json:"id"`
				} `json:"dependencies"`
			} `json:"modules"`
		} `json:"buildInfo"`
	}
	if err := json.Unmarshal([]byte(doc), &detail); err != nil {
		t.Fatalf("detail JSON: %v", err)
	}
	var apiMod *struct {
		ID           string            `json:"id"`
		Artifacts    []json.RawMessage `json:"artifacts"`
		Dependencies []struct {
			ID string `json:"id"`
		} `json:"dependencies"`
	}
	for i := range detail.BuildInfo.Modules {
		if detail.BuildInfo.Modules[i].ID == "com.example:api:1.0" {
			apiMod = &detail.BuildInfo.Modules[i]
		}
	}
	if apiMod == nil {
		t.Fatalf("merged module missing: %s", doc)
	}
	if len(apiMod.Dependencies) != 2 || len(apiMod.Artifacts) != 2 {
		t.Fatalf("module after append = %d artifacts / %d deps, want 2/2 (merge, not overwrite): %s",
			len(apiMod.Artifacts), len(apiMod.Dependencies), doc)
	}
	if apiMod.Dependencies[0].ID != "junit:junit:4.13" ||
		apiMod.Dependencies[1].ID != "org:lib:2.0" {
		t.Fatalf("dependency union order = %+v", apiMod.Dependencies)
	}
}

// TestBuildRESTQueryLadder: names list (relative /<name> URIs, lastStarted),
// numbers face (/<number> URIs, newest first), the detail's started-default
// = latest run and ?started= disambiguation through a foreign-zone literal.
func TestBuildRESTQueryLadder(t *testing.T) {
	h := newBuildHarness(t)
	// A fresh instance's names list is 200 with [] — never null, never 404.
	_, doc := getBuild(t, h, "/binflow/api/build", adminUser, adminPass)
	if !strings.Contains(doc, `"builds": []`) {
		t.Fatalf("fresh names list = %s, want builds []", doc)
	}

	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("upload A = %d", resp.StatusCode)
	}
	older := strings.Replace(buildRESTDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T08:00:00Z"`, 1)
	if resp := putBuildDoc(t, h, adminUser, adminPass, older); resp.StatusCode != 200 {
		t.Fatalf("upload B = %d", resp.StatusCode)
	}
	secret := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "sec-app"`, 1)
	if resp := putBuildDoc(t, h, adminUser, adminPass, secret); resp.StatusCode != 200 {
		t.Fatalf("upload secret = %d", resp.StatusCode)
	}

	// Names: uri "/<name>" relative, lastStarted = the group's latest run.
	_, doc = getBuild(t, h, "/binflow/api/build", adminUser, adminPass)
	for _, want := range []string{
		`"uri": "/pub-app"`, `"uri": "/sec-app"`,
		`"lastStarted": "2026-09-07T10:00:00.000+0000"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("names list missing %s: %s", want, doc)
		}
	}

	// Numbers: both runs, newest first, uri "/<number>".
	_, doc = getBuild(t, h, "/binflow/api/build/pub-app", adminUser, adminPass)
	if !strings.Contains(doc, `"uri": "/51"`) || !strings.Contains(doc, `"buildsNumbers"`) {
		t.Fatalf("numbers face = %s", doc)
	}
	if strings.Index(doc, `"started": "2026-09-07T10:00:00.000+0000"`) >
		strings.Index(doc, `"started": "2026-09-07T08:00:00.000+0000"`) {
		t.Fatalf("numbers face not newest-first: %s", doc)
	}

	// Detail default = the latest run; ?started= in a foreign zone reaches
	// the same stored run (the read-side normalization gate; the '+' of a
	// zone offset rides the query string percent-encoded, as every real
	// client sends it).
	_, doc = getBuild(t, h, "/binflow/api/build/pub-app/51", adminUser, adminPass)
	if !strings.Contains(doc, `"started": "2026-09-07T10:00:00.000+0000"`) {
		t.Fatalf("default detail is not the latest run: %s", doc)
	}
	_, doc = getBuild(t, h,
		"/binflow/api/build/pub-app/51?started=2026-09-07T16:00:00.000%2B0800",
		adminUser, adminPass)
	if !strings.Contains(doc, `"started": "2026-09-07T08:00:00.000+0000"`) {
		t.Fatalf("?started= foreign-zone literal missed the 08:00 run: %s", doc)
	}

	// Missing faces: the family's uniform 404 (zero-leak on numbers).
	resp, doc := getBuild(t, h, "/binflow/api/build/pub-app/99", adminUser, adminPass)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(doc, "Build-Info not found") {
		t.Fatalf("missing detail = %d %s", resp.StatusCode, doc)
	}
	resp, doc = getBuild(t, h, "/binflow/api/build/ghost-app", adminUser, adminPass)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(doc, "Build-Info not found") {
		t.Fatalf("missing numbers = %d %s", resp.StatusCode, doc)
	}
}

// TestBuildRESTErrorSurface: the honest ladder — self-frozen 400 wording,
// the verbatim append 404, 401 anonymous, the refused project/diff params,
// and the VOIDED path-segment PUT skeleton keeping the E-26 404.
func TestBuildRESTErrorSurface(t *testing.T) {
	h := newBuildHarness(t)
	// The parent pub-app#51 exists: the body-shape 400s below must be
	// reached past the family's 404-before-body ladder.
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
		wantIn string
	}{
		{"malformed json", http.MethodPut, "/binflow/api/build",
			`{"name": "pub-app",`, 400, "build info is not valid JSON"},
		{"missing name", http.MethodPut, "/binflow/api/build",
			`{"number": "1", "started": "2026-09-07T10:00:00Z"}`, 400, "build name is empty"},
		{"bad started", http.MethodPut, "/binflow/api/build",
			`{"name": "pub-app", "number": "1", "started": "soon"}`, 400, "must be an ISO8601 timestamp"},
		{"append non-array body", http.MethodPost, "/binflow/api/build/append/pub-app/51",
			`{"id": "m"}`, 400, "not a JSON array of modules"},
		{"append missing parent", http.MethodPost, "/binflow/api/build/append/pub-app/77",
			`[]`, 404, "Build-Info not found"},
		{"project param refused", http.MethodPut, "/binflow/api/build?project=team",
			buildRESTDoc, 400, "projects are not supported in BinFlow"},
		{"diff param refused", http.MethodGet,
			"/binflow/api/build/pub-app/51?diff=50", ``, 400, "diff parameter is not supported"},
		{"voided path skeleton", http.MethodPut, "/binflow/api/build/pub-app/51",
			buildRESTDoc, 404, "is not implemented in BinFlow"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var payload []byte
			if tc.body != "" {
				payload = []byte(tc.body)
			}
			resp := h.do(tc.method, tc.path, adminUser, adminPass, payload,
				map[string]string{"Content-Type": "application/json"})
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("%s %s = %d (%s), want %d", tc.method, tc.path, resp.StatusCode, body, tc.want)
			}
			if !strings.Contains(string(body), tc.wantIn) {
				t.Fatalf("body %q missing %q", body, tc.wantIn)
			}
		})
	}

	// Anonymous meets the 401 challenge at the route door.
	resp := h.do(http.MethodPut, "/binflow/api/build", "", "", []byte(buildRESTDoc), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous upload = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestBuildRESTACLArms: NFR-S80 on the wire — the write arm's denial (403,
// before any existence lookup), the read arm's zero-appearance, and the
// zero-leak list filtering (the numbers face of an unreadable name answers
// the family's 404, never an empty 200).
func TestBuildRESTACLArms(t *testing.T) {
	h := newBuildHarness(t)
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("seed upload = %d", resp.StatusCode)
	}
	secret := strings.Replace(buildRESTDoc, `"name": "pub-app"`, `"name": "sec-app"`, 1)
	if resp := putBuildDoc(t, h, adminUser, adminPass, secret); resp.StatusCode != 200 {
		t.Fatalf("seed secret = %d", resp.StatusCode)
	}

	// eve holds nothing: PUT 403 whether or not the build exists (no
	// oracle), GET detail 403, append 403.
	for _, doc := range []string{
		strings.Replace(buildRESTDoc, `"number": "51"`, `"number": "52"`, 1), // does not exist
		buildRESTDoc, // exists
	} {
		resp := putBuildDoc(t, h, "eve", "pw", doc)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("eve PUT = %d, want 403 (denial precedes existence)", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	if resp, _ := getBuild(t, h, "/binflow/api/build/pub-app/51", "eve", "pw"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("eve GET = %d, want 403", resp.StatusCode)
	}
	resp := h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51", "eve", "pw",
		[]byte(`[]`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("eve append = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// carol reads but cannot write: detail 200, PUT/append 403.
	if resp, _ := getBuild(t, h, "/binflow/api/build/pub-app/51", "carol", "pw"); resp.StatusCode != 200 {
		t.Fatalf("carol GET = %d, want 200", resp.StatusCode)
	}
	if resp := putBuildDoc(t, h, "carol", "pw", buildRESTDoc); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("carol PUT = %d, want 403", resp.StatusCode)
	}
	resp = h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51", "carol", "pw",
		[]byte(`[]`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("carol append = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Zero-leak: carol's names list shows pub-app alone; sec-app's numbers
	// face answers 404 (indistinguishable from a name that does not exist).
	_, doc := getBuild(t, h, "/binflow/api/build", "carol", "pw")
	if !strings.Contains(doc, "/pub-app") || strings.Contains(doc, "sec-app") {
		t.Fatalf("carol names list leaked: %s", doc)
	}
	if resp, _ := getBuild(t, h, "/binflow/api/build/sec-app", "carol", "pw"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("carol secret numbers = %d, want the zero-leak 404", resp.StatusCode)
	}
	// Zero-appearance: carol cannot read sec-app's detail.
	if resp, _ := getBuild(t, h, "/binflow/api/build/sec-app/51", "carol", "pw"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("carol secret detail = %d, want 403", resp.StatusCode)
	}

	// bob (r+w+d) publishes and re-publishes — the overwrite arm passes.
	if resp := putBuildDoc(t, h, "bob", "pw", buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("bob re-publish = %d, want 200 (w+d overwrite arm)", resp.StatusCode)
	}
}

// TestBuildRESTMetricFamily: the builds.put/builds.get family rides the
// standard /metrics scrape — pre-seeded series, upload's two outcome arms,
// append's merge arm, the three read faces, and the duration histogram's
// presence.
func TestBuildRESTMetricFamily(t *testing.T) {
	h := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Metrics = metrics.NewRegistry()
	}, nil)
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("upload = %d", resp.StatusCode)
	}
	if resp := putBuildDoc(t, h, adminUser, adminPass, buildRESTDoc); resp.StatusCode != 200 {
		t.Fatalf("re-upload = %d", resp.StatusCode)
	}
	resp := h.do(http.MethodPost, "/binflow/api/build/append/pub-app/51",
		adminUser, adminPass, []byte(`[]`), map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != 204 {
		t.Fatalf("append = %d", resp.StatusCode)
	}
	_ = resp.Body.Close()
	for _, p := range []string{
		"/binflow/api/build", "/binflow/api/build/pub-app", "/binflow/api/build/pub-app/51",
	} {
		if resp, _ := getBuild(t, h, p, adminUser, adminPass); resp.StatusCode != 200 {
			t.Fatalf("GET %s = %d", p, resp.StatusCode)
		}
	}

	scrapeResp, err := http.Get(h.srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	scrape, _ := io.ReadAll(scrapeResp.Body)
	_ = scrapeResp.Body.Close()
	text := string(scrape)
	for _, want := range []string{
		`binflow_builds_put_total{operation="upload",outcome="created"} 1`,
		`binflow_builds_put_total{operation="upload",outcome="replaced"} 1`,
		`binflow_builds_put_total{operation="append",outcome="merged"} 1`,
		`binflow_builds_get_total{face="names"} 1`,
		`binflow_builds_get_total{face="numbers"} 1`,
		`binflow_builds_get_total{face="detail"} 1`,
		`binflow_builds_put_duration_seconds_count{operation="upload"}`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("scrape missing %s:\n%s", want, text)
		}
	}
}
