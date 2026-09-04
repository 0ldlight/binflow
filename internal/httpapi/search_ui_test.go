package httpapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// T-452 (FR-148.3 / aql.md §14.5): the UI search family's four resources
// over the real stack. The wire models are the registered medium-confidence
// BinFlow mappings (the anchor pins paths/verbs/roles and the two verbatim
// copies — the stash off-copy and the syntax-search error envelope); every
// test here pins the implemented behavior so a later live calibration has
// a concrete baseline to diff against (V-m).

// uiDo issues one UI-family request and decodes status + body.
func uiDo(t *testing.T, h *harness, method, path, user, pass string, body []byte) (int, string) {
	t.Helper()
	resp := h.do(method, "/binflow/api/"+path, user, pass, body, nil)
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, mustGet(t, resp)
}

// uiHits decodes the family's {"results":[FileInfo]} envelope into
// "<repo><path>" keys (the E-09 superset row the legacy doors answer —
// the registered family posture; only the identity fields are asserted
// here, the full shape is t92's own spec).
func uiHits(t *testing.T, body string) []string {
	t.Helper()
	var b struct {
		Results []struct {
			Repo string `json:"repo"`
			Path string `json:"path"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	out := make([]string, 0, len(b.Results))
	for _, r := range b.Results {
		out = append(out, r.Repo+r.Path)
	}
	return out
}

// TestUIArtifactSearchDoors pins the artifactsearch search doors: the
// quick kernel, the checksum kernel, the trash narrowing, and the family's
// shared gates (anonymous 401, body-validation 400s).
func TestUIArtifactSearchDoors(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	sum := sha256.Sum256([]byte("ui-bytes"))
	deposit(t, h, "generic-local", "acme/artifact.bin", "ui-bytes")

	// quick: the name fragment through the K64 kernel.
	status, body := uiDo(t, h, http.MethodPost, "artifactsearch/quick", adminUser, adminPass,
		[]byte(`{"searchTerm":"artifact"}`))
	if status != http.StatusOK {
		t.Fatalf("quick status = %d body=%s", status, body)
	}
	if got := uiHits(t, body); len(got) != 1 || got[0] != "generic-local/acme/artifact.bin" {
		t.Fatalf("quick hits = %v", got)
	}

	// checksum: the exact digest.
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/checksum", adminUser, adminPass,
		[]byte(`{"sha256":"`+hex.EncodeToString(sum[:])+`"}`))
	if status != http.StatusOK {
		t.Fatalf("checksum status = %d body=%s", status, body)
	}
	if got := uiHits(t, body); len(got) != 1 {
		t.Fatalf("checksum hits = %v", got)
	}

	// gavc: the coordinate model rides the legacy kernel (validation only —
	// the maven data leg is T-417's own spec).
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/gavc", adminUser, adminPass,
		[]byte(`{"a":"app"}`))
	if status != http.StatusOK {
		t.Fatalf("gavc status = %d body=%s", status, body)
	}

	// trash: narrowed to the trashcan repository — the miss is the family's
	// 200 empty array (the E-09 envelope, never a 404).
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/trash", adminUser, adminPass,
		[]byte(`{"searchTerm":"artifact"}`))
	if status != http.StatusOK {
		t.Fatalf("trash status = %d body=%s", status, body)
	}
	if got := uiHits(t, body); len(got) != 0 {
		t.Fatalf("trash hits = %v, want the empty array (nothing resides in the trashcan)", got)
	}

	// The family gates: anonymous 401 on every door; the body 400s.
	for _, path := range []string{"artifactsearch/quick", "artifactsearch/gavc", "artifactsearch/checksum", "artifactsearch/trash"} {
		resp := h.do(http.MethodPost, "/binflow/api/"+path, "", "", []byte(`{"searchTerm":"x"}`), nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous POST %s = %d, want 401", path, resp.StatusCode)
		}
	}
	for _, bad := range []struct{ path, body string }{
		{"artifactsearch/quick", `{}`},
		{"artifactsearch/quick", `not json`},
		{"artifactsearch/gavc", `{}`},
		{"artifactsearch/checksum", `{}`},
		{"artifactsearch/trash", `{"searchTerm":""}`},
	} {
		status, body = uiDo(t, h, http.MethodPost, bad.path, adminUser, adminPass, []byte(bad.body))
		if status != http.StatusBadRequest {
			t.Fatalf("%s with %q = %d %s, want the body 400", bad.path, bad.body, status, body)
		}
	}
}

// TestUIArtifactSearchPkgDoors pins the pkg doors: the option set (GET),
// the criteria-array search narrowed to the type's repositories (POST) and
// the criteria-to-AQL converter (tonative).
func TestUIArtifactSearchPkgDoors(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	seedTypedRepo(t, h, "maven-local", "maven")
	deposit(t, h, "generic-local", "acme/artifact.bin", "pkg-bytes")

	// The option set: the generic repositories only.
	status, body := uiDo(t, h, http.MethodGet, "artifactsearch/pkg/generic", adminUser, adminPass, nil)
	if status != http.StatusOK {
		t.Fatalf("pkg GET status = %d body=%s", status, body)
	}
	var opts struct {
		Repos []string `json:"repos"`
	}
	if err := json.Unmarshal([]byte(body), &opts); err != nil {
		t.Fatalf("pkg GET body %q: %v", body, err)
	}
	if len(opts.Repos) != 1 || opts.Repos[0] != "generic-local" {
		t.Fatalf("pkg GET repos = %v, want exactly the generic repository", opts.Repos)
	}

	// The criteria array: the name search stays inside the type.
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/pkg/generic", adminUser, adminPass,
		[]byte(`[{"name":"artifact"},{"name":"nothing-matches-this"}]`))
	if status != http.StatusOK {
		t.Fatalf("pkg POST status = %d body=%s", status, body)
	}
	if got := uiHits(t, body); len(got) != 1 || got[0] != "generic-local/acme/artifact.bin" {
		t.Fatalf("pkg POST hits = %v", got)
	}

	// tonative: the criteria rendered as the equivalent AQL text — a string
	// is answered, never executed; a quote-bearing term cannot break out of
	// the quoted literal.
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/pkg/tonative", adminUser, adminPass,
		[]byte(`[{"name":"lib"}]`))
	if status != http.StatusOK {
		t.Fatalf("tonative status = %d body=%s", status, body)
	}
	var conv struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(body), &conv); err != nil {
		t.Fatalf("tonative body %q: %v", body, err)
	}
	if conv.Query != `items.find({"name":{"$match":"*lib*"}})` {
		t.Fatalf("tonative query = %q", conv.Query)
	}
	status, body = uiDo(t, h, http.MethodPost, "artifactsearch/pkg/tonative", adminUser, adminPass,
		[]byte(`[{"name":"a\"b"}]`))
	if status != http.StatusOK {
		t.Fatalf("tonative quote status = %d body=%s", status, body)
	}
	if err := json.Unmarshal([]byte(body), &conv); err != nil || !strings.Contains(conv.Query, `\"b`) {
		t.Fatalf("tonative quote escaping = %q", conv.Query)
	}

	// deleteArtifact stays unrouted (the anchor's read-only registration).
	resp := h.do(http.MethodPost, "/binflow/api/artifactsearch/deleteArtifact", adminUser, adminPass,
		[]byte(`{"repoKey":"generic-local","path":"acme/artifact.bin"}`), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleteArtifact = %d, want the E-26 404 (the registered gap)", resp.StatusCode)
	}
}

// TestUIStashResultsFactoryOff pins the stash family's verbatim off-copy:
// every one of the ten operations answers the anchor's 404 message —
// distinguished from the E-26 not-implemented 404 — and the family gate
// still demands a credential.
func TestUIStashResultsFactoryOff(t *testing.T) {
	h := newHarness(t)
	ops := []struct{ method, path string }{
		{http.MethodPost, "stashResults"},
		{http.MethodGet, "stashResults"},
		{http.MethodDelete, "stashResults"},
		{http.MethodPost, "stashResults/subtract"},
		{http.MethodPost, "stashResults/intersect"},
		{http.MethodPost, "stashResults/add"},
		{http.MethodPost, "stashResults/export"},
		{http.MethodPost, "stashResults/copy"},
		{http.MethodPost, "stashResults/move"},
		{http.MethodPost, "stashResults/discard"},
	}
	for _, op := range ops {
		status, body := uiDo(t, h, op.method, op.path, adminUser, adminPass, []byte(`{}`))
		if status != http.StatusNotFound || !strings.Contains(body, "Stash search results endpoint is disabled") {
			t.Fatalf("%s %s = %d %s, want the verbatim off-copy", op.method, op.path, status, body)
		}
	}
	// Anonymous still meets the 401 challenge BEFORE the off-copy (the
	// family gate).
	resp := h.do(http.MethodPost, "/binflow/api/stashResults", "", "", []byte(`{}`), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous stashResults = %d, want 401", resp.StatusCode)
	}
}

// TestUIPackagesSearchDoors pins the packages doors: the addressed file,
// the folder listing, and the anchor's 404-EMPTY-BODY miss arm.
func TestUIPackagesSearchDoors(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/lib/1.0.0/lib-1.0.0.bin", "lead-bytes")
	deposit(t, h, "generic-local", "acme/lib/1.0.0/lib-1.0.0.pom", "pom-bytes")

	// leadFile on the file: the row with the uri/repo/path/name shape.
	status, body := uiDo(t, h, http.MethodPost, "packagesSearch/leadFile", adminUser, adminPass,
		[]byte(`{"repoKey":"generic-local","path":"acme/lib/1.0.0/lib-1.0.0.bin"}`))
	if status != http.StatusOK {
		t.Fatalf("leadFile status = %d body=%s", status, body)
	}
	var lead struct {
		URI     string `json:"uri"`
		RepoKey string `json:"repoKey"`
		Path    string `json:"path"`
		Name    string `json:"name"`
		Size    int64  `json:"size"`
	}
	if err := json.Unmarshal([]byte(body), &lead); err != nil {
		t.Fatalf("leadFile body %q: %v", body, err)
	}
	if lead.RepoKey != "generic-local" || lead.Name != "lib-1.0.0.bin" ||
		!strings.HasSuffix(lead.URI, "/api/storage/generic-local/acme/lib/1.0.0/lib-1.0.0.bin") {
		t.Fatalf("leadFile row = %+v", lead)
	}

	// artifacts under the folder: the file children.
	status, body = uiDo(t, h, http.MethodPost, "packagesSearch/artifacts", adminUser, adminPass,
		[]byte(`{"repoKey":"generic-local","path":"acme/lib/1.0.0"}`))
	if status != http.StatusOK {
		t.Fatalf("artifacts status = %d body=%s", status, body)
	}
	var arts struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &arts); err != nil {
		t.Fatalf("artifacts body %q: %v", body, err)
	}
	if len(arts.Results) != 2 {
		t.Fatalf("artifacts = %+v, want both files", arts.Results)
	}

	// The miss arm: 404 with an EMPTY body (the anchor's verbatim posture —
	// no envelope). A folder is not a lead file either.
	for _, body2 := range []string{
		`{"repoKey":"generic-local","path":"acme/nothing.bin"}`,
		`{"repoKey":"generic-local","path":"acme/lib/1.0.0"}`,
		`{"repoKey":"no-such-repo","path":"x.bin"}`,
	} {
		resp := h.do(http.MethodPost, "/binflow/api/packagesSearch/leadFile", adminUser, adminPass, []byte(body2), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("leadFile %s = %d, want 404", body2, resp.StatusCode)
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if len(raw) != 0 {
			t.Fatalf("leadFile miss body = %q, want EMPTY", raw)
		}
	}

	// Addressing validation: the 400s.
	for _, bad := range []string{`{}`, `{"repoKey":"generic-local"}`, `{"repoKey":"generic-local","path":"../x"}`} {
		status, body = uiDo(t, h, http.MethodPost, "packagesSearch/leadFile", adminUser, adminPass, []byte(bad))
		if status != http.StatusBadRequest {
			t.Fatalf("leadFile %s = %d %s, want 400", bad, status, body)
		}
	}
}

// TestUISyntaxSearch pins the syntax-search door: the AQL text body, the
// compact UI envelope, the syntax-error 400 envelope, and the truncation
// surface.
func TestUISyntaxSearch(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	deposit(t, h, "generic-local", "acme/artifact.bin", "syntax-bytes")

	status, body := uiDo(t, h, http.MethodPost, "syntax-search", adminUser, adminPass,
		[]byte(`items.find({"repo":"generic-local"}).include("name","repo")`))
	if status != http.StatusOK {
		t.Fatalf("syntax-search status = %d body=%s", status, body)
	}
	var got struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if len(got.Results) != 1 || got.Results[0]["name"] != "artifact.bin" || got.Results[0]["repo"] != "generic-local" {
		t.Fatalf("results = %+v", got.Results)
	}

	// A syntax error: the 400 errors[] envelope (the anchor's
	// ErrorResponse arm).
	status, body = uiDo(t, h, http.MethodPost, "syntax-search", adminUser, adminPass,
		[]byte(`items.find({`))
	if status != http.StatusBadRequest || !strings.Contains(body, `"errors"`) {
		t.Fatalf("syntax error = %d %s, want the 400 envelope", status, body)
	}

	// An unsupported domain: the honest 400 (the family's shared engine).
	status, body = uiDo(t, h, http.MethodPost, "syntax-search", adminUser, adminPass,
		[]byte(`builds.find({"name":"x"})`))
	if status != http.StatusBadRequest {
		t.Fatalf("unsupported domain = %d %s, want 400", status, body)
	}

	// Anonymous 401 (the family gate); foreign verbs keep the E-26 404.
	resp := h.do(http.MethodPost, "/binflow/api/syntax-search", "", "", []byte(`items.find({})`), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous syntax-search = %d, want 401", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/binflow/api/syntax-search", adminUser, adminPass, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET syntax-search = %d, want the E-26 404", resp.StatusCode)
	}
}
