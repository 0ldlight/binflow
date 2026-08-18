package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-40 catalog-domain suite. Two stacks:
//
//   - catalogStack: the true stack (real sqlite metadata, real storage
//     engine, real repo.Service, real auth.Service/Authorizer) invoked
//     in-process with a chosen principal — the pagination cursors, the Q5
//     ACL matrix and the delete cascade run against production
//     collaborators, the same assembly the httpapi harness mounts;
//   - fake-backed handlers for the transport-fault branches (a failing
//     RepoLookup.List, a nil service).
//
// AC mapping: DE-11 (catalog shape/order), DE-12 (tags/list shape, null
// tags, NAME_UNKNOWN), D09 (pagination follow-next, invalid n), Q5 (the
// visibility matrix), FR-7-AC5 (deleted repositories vanish).

// catalogStack is the true-stack harness.
type catalogStack struct {
	t     *testing.T
	h     *Handler
	svc   repo.Service
	md    metadata.Store
	authz *auth.Service
	admin *auth.Principal
}

// newCatalogStack assembles the stack; anonymousAccess flips the instance's
// anonymous mode (both Q5 rows need it).
func newCatalogStack(t *testing.T, anonymousAccess bool) *catalogStack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	authSvc := auth.NewFromStore(md, anonymousAccess)
	svc := repo.New(st, md, authSvc, nil)
	h := New(svc, NewRepoLookup(md.Repos()), authSvc, authSvc, md.Users(),
		Options{AnonymousAccess: anonymousAccess}, nil).
		WithStorage(st, md.Blobs())
	return &catalogStack{
		t: t, h: h, svc: svc, md: md, authz: authSvc,
		admin: &auth.Principal{Name: "admin", Admin: true},
	}
}

// serve runs one request through the handler with p in the context (nil p
// exercises the anonymous rows).
func (cs *catalogStack) serve(method, path string, p *auth.Principal) *http.Response {
	cs.t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if p != nil {
		req = req.WithContext(adapter.WithPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	cs.h.ServeHTTP(rec, req)
	return rec.Result()
}

// seedDockerRepo creates a local docker repository through the real service.
func (cs *catalogStack) seedDockerRepo(key string) {
	cs.t.Helper()
	if _, err := cs.svc.CreateRepo(context.Background(), cs.admin, &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageDocker,
	}); err != nil {
		cs.t.Fatalf("CreateRepo %s: %v", key, err)
	}
}

// pushBlob uploads one blob through the real wire chain (the monolithic
// POST ?digest= style — the body rides the request) and returns its wire
// digest.
func (cs *catalogStack) pushBlob(name string, content []byte) string {
	cs.t.Helper()
	dgst := "sha256:" + sha256Hex(content)
	req := httptest.NewRequest(http.MethodPost, "/v2/"+name+"/blobs/uploads/?digest="+dgst,
		strings.NewReader(string(content)))
	req = req.WithContext(adapter.WithPrincipal(req.Context(), cs.admin))
	rec := httptest.NewRecorder()
	cs.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		cs.t.Fatalf("blob push %s: status %d body=%s", name, rec.Code, rec.Body.String())
	}
	return dgst
}

// putManifest issues the manifest PUT (the one request serve() cannot
// express: it carries a body and a Content-Type).
func (cs *catalogStack) putManifest(name, ref string, body []byte) *httptest.ResponseRecorder {
	cs.t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v2/"+name+"/manifests/"+ref, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", mediaTypeDockerManifest)
	req = req.WithContext(adapter.WithPrincipal(req.Context(), cs.admin))
	rec := httptest.NewRecorder()
	cs.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		cs.t.Fatalf("manifest push %s:%s: status %d body=%s", name, ref, rec.Code, rec.Body.String())
	}
	return rec
}

// pushImage drives the real wire chain (two monolithic blob POSTs + the
// manifest PUT) for <name>:<tag> and returns the manifest digest.
func (cs *catalogStack) pushImage(name, tag string) string {
	cs.t.Helper()
	layer := []byte("layer-of-" + name + ":" + tag)
	cfg := []byte(`{"architecture":"amd64","os":"linux"}`)
	layerDgst, cfgDgst := cs.pushBlob(name, layer), cs.pushBlob(name, cfg)
	body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+mediaTypeDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":%d}]}`,
		cfgDgst, len(cfg), layerDgst, len(layer)))
	return cs.putManifest(name, tag, body).Header().Get("Docker-Content-Digest")
}

// seedManifestRow lands one manifest index row directly through the real
// service (the blob body is irrelevant to the listing): the bulk seeder for
// pagination-boundary cases where hundreds of wire pushes would only burn
// time.
func (cs *catalogStack) seedManifestRow(repoKey, image, digest, tag string) {
	cs.t.Helper()
	_, err := cs.svc.PutManifest(context.Background(), cs.admin, repoKey, image, digest, tag,
		mediaTypeDockerManifest, 128, nil)
	if err != nil {
		cs.t.Fatalf("seed manifest %s/%s: %v", repoKey, image, err)
	}
}

// decodeCatalog parses the _catalog body.
func decodeCatalog(t *testing.T, resp *http.Response) catalogBody {
	t.Helper()
	var body catalogBody
	if err := json.Unmarshal(readBody(t, resp), &body); err != nil {
		t.Fatalf("catalog body is not JSON: %v", err)
	}
	return body
}

// followNext extracts the next page URL from the Link header ("" when the
// header is absent) and asserts the relation spelling.
func followNext(t *testing.T, resp *http.Response) string {
	t.Helper()
	link := resp.Header.Get("Link")
	if link == "" {
		return ""
	}
	if !strings.HasSuffix(link, `; rel="next"`) || !strings.HasPrefix(link, "<") {
		t.Fatalf("Link header malformed: %q", link)
	}
	inner := strings.TrimSuffix(link, `; rel="next"`)
	return strings.Trim(inner, "<>")
}

// ---- AC1: catalog and tags/list shapes ----

// TestCatalogListsImagesLexicographic (DE-11, FR-10-AC1, D09 core): the
// catalog aggregates every docker repository's manifest-carrying images as
// "<repoKey>/<image>" names in lexicographic order — including nested
// names — while a generic repository never leaks in and an empty docker
// repository never appears (the source is docker_manifests, not the
// repositories table).
func TestCatalogListsImagesLexicographic(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.seedDockerRepo("team2")
	cs.seedDockerRepo("empty-docker")
	// A generic repository with real content: the catalog must skip it by
	// package type (it has no docker manifests either way, but the row's
	// presence is what the filter must survive).
	if _, err := cs.svc.CreateRepo(context.Background(), cs.admin, &metadata.Repo{
		RepoKey: "plain", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo plain: %v", err)
	}
	if _, err := cs.svc.Put(context.Background(), cs.admin, "plain", "x.bin",
		strings.NewReader("generic-bytes"), storage.BlobRef{Sha256: sha256Hex([]byte("generic-bytes"))}, ""); err != nil {
		t.Fatalf("generic Put: %v", err)
	}

	cs.pushImage("team1/app", "v1")
	cs.pushImage("team1/acme/team/app", "v1") // nested name (D09)
	cs.pushImage("team2/other", "latest")

	resp := cs.serve(http.MethodGet, "/v2/_catalog", cs.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get(HeaderAPIVersion); got != APIVersionValue {
		t.Fatalf("api-version = %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := decodeCatalog(t, resp)
	want := []string{"team1/acme/team/app", "team1/app", "team2/other"}
	if len(body.Repositories) != len(want) {
		t.Fatalf("repositories = %v, want %v", body.Repositories, want)
	}
	for i := range want {
		if body.Repositories[i] != want[i] {
			t.Fatalf("repositories[%d] = %q, want %q (full: %v)", i, body.Repositories[i], want[i], body.Repositories)
		}
	}
}

// TestCatalogEmptyIsArray: no images at all renders "repositories":[] — the
// null form is the tags endpoint's convention, never the catalog's.
func TestCatalogEmptyIsArray(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	resp := cs.serve(http.MethodGet, "/v2/_catalog", cs.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if raw := string(readBody(t, resp)); !strings.Contains(raw, `"repositories":[]`) {
		t.Fatalf("empty catalog body = %q, want an empty array", raw)
	}
}

// TestTagsListShapeAndOrder (DE-12, FR-10-AC2): tags return in tag order
// whatever the push order was, under the full "<repoKey>/<image>" name.
func TestTagsListShapeAndOrder(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.pushImage("team1/app", "v3")
	cs.pushImage("team1/app", "v1")
	cs.pushImage("team1/app", "v2")

	resp := cs.serve(http.MethodGet, "/v2/team1/app/tags/list", cs.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get(HeaderAPIVersion); got != APIVersionValue {
		t.Fatalf("api-version = %q", got)
	}
	var body tagsBody
	if err := json.Unmarshal(readBody(t, resp), &body); err != nil {
		t.Fatalf("tags body: %v", err)
	}
	if body.Name != "team1/app" {
		t.Fatalf("name = %q, want team1/app", body.Name)
	}
	want := []string{"v1", "v2", "v3"}
	if len(body.Tags) != len(want) {
		t.Fatalf("tags = %v, want %v", body.Tags, want)
	}
	for i := range want {
		if body.Tags[i] != want[i] {
			t.Fatalf("tags[%d] = %q, want %q", i, body.Tags[i], want[i])
		}
	}
}

// TestTagsListNullOnTaglessImage (PRD v1.1/R4): an image that exists (a
// digest-only push, or every tag deleted) answers "tags":null — NOT an
// empty array; jq '.tags == null' is the pinned QA assertion.
func TestTagsListNullOnTaglessImage(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	// Digest-only push: the image lands in the catalog with zero tags.
	cfg := cs.pushBlob("team1/ghost", []byte(`{"os":"linux"}`))
	layer := cs.pushBlob("team1/ghost", []byte("ghost-layer"))
	body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+mediaTypeDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":13},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":12}]}`,
		cfg, layer))
	cs.putManifest("team1/ghost", "sha256:"+sha256Hex(body), body)

	resp := cs.serve(http.MethodGet, "/v2/team1/ghost/tags/list", cs.admin)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var raw struct {
		Name string    `json:"name"`
		Tags *[]string `json:"tags"` // nil pointer <=> JSON null
	}
	if err := json.Unmarshal(readBody(t, resp), &raw); err != nil {
		t.Fatalf("tags body: %v", err)
	}
	if raw.Name != "team1/ghost" {
		t.Fatalf("name = %q", raw.Name)
	}
	if raw.Tags != nil {
		t.Fatalf("tags = %v, want JSON null", *raw.Tags)
	}
}

// TestTagsListNameUnknown (FR-10-AC3): an image without manifest rows — and
// a repository that does not exist at all — answer 404 NAME_UNKNOWN, never
// an empty list.
func TestTagsListNameUnknown(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.pushImage("team1/app", "v1")

	tests := []struct {
		name string
		path string
	}{
		{"unknown image in a known repo", "/v2/team1/ghost/tags/list"},
		{"unknown repository", "/v2/nope/ghost/tags/list"},
		{"non-docker repository", "/v2/plain/x/tags/list"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := cs.serve(http.MethodGet, tc.path, cs.admin)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("%s status = %d body=%s", tc.path, resp.StatusCode, body)
			}
			eb := decodeSpecBody(t, body)
			if eb.Errors[0].Code != ErrCodeNameUnknown {
				t.Fatalf("%s code = %q, want NAME_UNKNOWN", tc.path, eb.Errors[0].Code)
			}
		})
	}
}

// decodeSpecBody parses the registry error envelope (the package-internal
// twin of the httpapi test helper).
func decodeSpecBody(t *testing.T, body []byte) specErrorBody {
	t.Helper()
	var eb specErrorBody
	if err := json.Unmarshal(body, &eb); err != nil {
		t.Fatalf("body %q is not the spec error schema: %v", body, err)
	}
	if len(eb.Errors) != 1 {
		t.Fatalf("body %q: want one error entry", body)
	}
	return eb
}

// ---- AC2: pagination (D09) ----

// TestCatalogPaginationFollowNext (D09): ?n=1 plus the Link header
// enumerates the whole catalog without repeats — the last cursor is
// exclusive — and pages may span repositories.
func TestCatalogPaginationFollowNext(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.seedDockerRepo("team2")
	cs.seedManifestRow("team1", "app-b", strings.Repeat("b", 64), "")
	cs.seedManifestRow("team1", "app-a", strings.Repeat("a", 64), "")
	cs.seedManifestRow("team2", "zeta", strings.Repeat("c", 64), "")
	cs.seedManifestRow("team2", "alpha", strings.Repeat("d", 64), "")

	want := []string{"team1/app-a", "team1/app-b", "team2/alpha", "team2/zeta"}
	var got []string
	path := "/v2/_catalog?n=1"
	for i := 0; ; i++ {
		resp := cs.serve(http.MethodGet, path, cs.admin)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d status = %d", i, resp.StatusCode)
		}
		body := decodeCatalog(t, resp)
		if len(body.Repositories) != 1 {
			t.Fatalf("page %d returned %d entries, want 1", i, len(body.Repositories))
		}
		got = append(got, body.Repositories...)
		next := followNext(t, resp)
		if next == "" {
			break
		}
		// The link is path-absolute with both parameters carried over.
		if !strings.HasPrefix(next, "/v2/_catalog?") {
			t.Fatalf("next link %q is not the catalog path", next)
		}
		u, err := url.Parse(next)
		if err != nil {
			t.Fatalf("next link %q: %v", next, err)
		}
		if u.Query().Get("n") != "1" {
			t.Fatalf("next link %q lost n", next)
		}
		path = next
		if i > len(want) {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(got) != len(want) {
		t.Fatalf("enumerated %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("enumerated[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestCatalogPageBoundaries: a page that exactly exhausts the listing
// carries no Link; one entry more does; the cursor is exclusive (last is
// never re-served).
func TestCatalogPageBoundaries(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.seedManifestRow("team1", "a", strings.Repeat("1", 64), "")
	cs.seedManifestRow("team1", "b", strings.Repeat("2", 64), "")
	cs.seedManifestRow("team1", "c", strings.Repeat("3", 64), "")

	// Exactly full: three entries, n=3 -> no next page.
	resp := cs.serve(http.MethodGet, "/v2/_catalog?n=3", cs.admin)
	if body := decodeCatalog(t, resp); len(body.Repositories) != 3 {
		t.Fatalf("n=3 returned %v", body.Repositories)
	}
	if link := followNext(t, resp); link != "" {
		t.Fatalf("exactly-full page carries Link %q", link)
	}

	// One short of the end: two pages; the Link cursor is the page's LAST
	// entry (exclusive — following it resumes after it).
	resp = cs.serve(http.MethodGet, "/v2/_catalog?n=2", cs.admin)
	if link := followNext(t, resp); link == "" {
		t.Fatal("partial page carries no Link")
	} else if !strings.Contains(link, "last=team1%2Fb&n=2") {
		t.Fatalf("Link %q does not carry the page's last entry as the cursor", link)
	}
	// The cursor itself is excluded: asking after the first entry yields
	// exactly the remaining two.
	resp = cs.serve(http.MethodGet, "/v2/_catalog?n=100&last=team1/a", cs.admin)
	body := decodeCatalog(t, resp)
	if len(body.Repositories) != 2 || body.Repositories[0] != "team1/b" || body.Repositories[1] != "team1/c" {
		t.Fatalf("after exclusive cursor team1/a: %v, want [team1/b team1/c]", body.Repositories)
	}
	// A cursor at/beyond the tail leaves nothing and reports no next page.
	resp = cs.serve(http.MethodGet, "/v2/_catalog?n=100&last=team1/c", cs.admin)
	if body := decodeCatalog(t, resp); len(body.Repositories) != 0 {
		t.Fatalf("cursor at the tail returned %v", body.Repositories)
	}
	if link := followNext(t, resp); link != "" {
		t.Fatalf("empty tail carries Link %q", link)
	}
}

// TestCatalogDefaultPageSize (D09/DE-11): with n absent the page holds 100
// entries (the official default — Artifactory's full-list deviation is not
// adopted); an explicit larger n serves the rest.
func TestCatalogDefaultPageSize(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("bulk")
	for i := 0; i < 105; i++ {
		cs.seedManifestRow("bulk", fmt.Sprintf("img-%03d", i), fmt.Sprintf("%064x", i), "")
	}
	resp := cs.serve(http.MethodGet, "/v2/_catalog", cs.admin)
	body := decodeCatalog(t, resp)
	if len(body.Repositories) != defaultPageSize {
		t.Fatalf("default page = %d entries, want %d", len(body.Repositories), defaultPageSize)
	}
	if link := followNext(t, resp); link == "" {
		t.Fatal("a 105-entry catalog with a 100-entry default page must carry a Link")
	}
	resp = cs.serve(http.MethodGet, "/v2/_catalog?n=105", cs.admin)
	if body := decodeCatalog(t, resp); len(body.Repositories) != 105 {
		t.Fatalf("n=105 returned %d entries", len(body.Repositories))
	}
	if link := followNext(t, resp); link != "" {
		t.Fatalf("full listing carries Link %q", link)
	}
}

// TestPaginationNumberInvalid (FR-10-AC4): n=0, negatives, non-numeric and
// overflowing values answer 400 PAGINATION_NUMBER_INVALID on BOTH endpoints
// — one pagination contract (table-driven).
func TestPaginationNumberInvalid(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.pushImage("team1/app", "v1")

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"zero", "n=0"},
		{"negative", "n=-1"},
		{"non-numeric", "n=abc"},
		{"fraction", "n=1.5"},
		{"overflow", "n=99999999999999999999"},
	} {
		for _, endpoint := range []string{"/v2/_catalog", "/v2/team1/app/tags/list"} {
			t.Run(tc.name+" "+endpoint, func(t *testing.T) {
				resp := cs.serve(http.MethodGet, endpoint+"?"+tc.query, cs.admin)
				body := readBody(t, resp)
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("%s?%s status = %d body=%s", endpoint, tc.query, resp.StatusCode, body)
				}
				eb := decodeSpecBody(t, body)
				if eb.Errors[0].Code != ErrCodePaginationNumberInvalid {
					t.Fatalf("code = %q, want PAGINATION_NUMBER_INVALID", eb.Errors[0].Code)
				}
			})
		}
	}
}

// TestTagsListPaginationFollowNext (D09): n=1 plus the Link enumerates
// every tag without repeats; the cursor shares the tag charset and a
// malformed one is refused.
func TestTagsListPaginationFollowNext(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	for _, tag := range []string{"v3", "v1", "v2", "v10"} {
		cs.pushImage("team1/app", tag)
	}
	want := []string{"v1", "v10", "v2", "v3"} // lexicographic, not numeric

	var got []string
	path := "/v2/team1/app/tags/list?n=1"
	for i := 0; ; i++ {
		resp := cs.serve(http.MethodGet, path, cs.admin)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("page %d status = %d", i, resp.StatusCode)
		}
		var body tagsBody
		if err := json.Unmarshal(readBody(t, resp), &body); err != nil {
			t.Fatalf("tags body: %v", err)
		}
		if len(body.Tags) != 1 {
			t.Fatalf("page %d returned %v, want one tag", i, body.Tags)
		}
		got = append(got, body.Tags...)
		next := followNext(t, resp)
		if next == "" {
			break
		}
		if !strings.HasPrefix(next, "/v2/team1/app/tags/list?") {
			t.Fatalf("next link %q is not the tags path", next)
		}
		path = next
		if i > len(want) {
			t.Fatal("pagination did not terminate")
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("enumerated[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}

	// A cursor outside the tag charset (the service's ErrInvalidCursor)
	// answers the pagination 400 family.
	resp := cs.serve(http.MethodGet, "/v2/team1/app/tags/list?last=!!!", cs.admin)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d body=%s", resp.StatusCode, body)
	}
	if eb := decodeSpecBody(t, body); eb.Errors[0].Code != ErrCodePaginationNumberInvalid {
		t.Fatalf("invalid cursor code = %q", eb.Errors[0].Code)
	}
}

// ---- AC3: delete + Q5 visibility matrix ----

// TestCatalogRepoDeleteVanishes (FR-7-AC5 end to end): after a repository
// teardown with deleteContent=true the catalog no longer carries any of its
// names.
func TestCatalogRepoDeleteVanishes(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.seedDockerRepo("doomed")
	cs.pushImage("doomed/app", "v1")
	cs.pushImage("team1/app", "v1")

	if body := decodeCatalog(t, cs.serve(http.MethodGet, "/v2/_catalog", cs.admin)); len(body.Repositories) != 2 {
		t.Fatalf("pre-delete catalog = %v", body.Repositories)
	}
	if err := cs.svc.DeleteRepo(context.Background(), cs.admin, "doomed", true); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}
	body := decodeCatalog(t, cs.serve(http.MethodGet, "/v2/_catalog", cs.admin))
	if len(body.Repositories) != 1 || body.Repositories[0] != "team1/app" {
		t.Fatalf("post-delete catalog = %v, want [team1/app]", body.Repositories)
	}
}

// grantRead lands one permission target giving user read on repoKey
// through the real permission store (the E-24 shape).
func (cs *catalogStack) grantRead(name, repoKey, user string) {
	cs.t.Helper()
	if err := cs.md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: name, Repos: `["` + repoKey + `"]`, Includes: "[]", Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: name, Principal: user, PrincipalType: "user", CanRead: true,
	}}); err != nil {
		cs.t.Fatalf("PutTarget %s: %v", name, err)
	}
}

// TestCatalogVisibilityMatrix (Q5 interim ruling, table-driven): admin sees
// everything; an authenticated non-admin sees exactly the repositories its
// read ACL covers; an ungranted principal sees an empty (200) catalog;
// anonymous sees everything while anonymous_access is on.
func TestCatalogVisibilityMatrix(t *testing.T) {
	cs := newCatalogStack(t, true)
	for _, key := range []string{"alpha", "beta", "gamma"} {
		cs.seedDockerRepo(key)
		cs.pushImage(key+"/app", "v1")
	}
	cs.grantRead("alice-alpha", "alpha", "alice")

	alice := &auth.Principal{Name: "alice"}
	bob := &auth.Principal{Name: "bob"}

	tests := []struct {
		name      string
		principal *auth.Principal
		want      []string
	}{
		{"admin sees everything", cs.admin, []string{"alpha/app", "beta/app", "gamma/app"}},
		{"authorized non-admin sees its ACL only", alice, []string{"alpha/app"}},
		{"ungranted non-admin sees an empty catalog", bob, []string{}},
		{"anonymous sees everything while open", nil, []string{"alpha/app", "beta/app", "gamma/app"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := cs.serve(http.MethodGet, "/v2/_catalog", tc.principal)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d body=%s", resp.StatusCode, readBody(t, resp))
			}
			body := decodeCatalog(t, resp)
			if len(body.Repositories) != len(tc.want) {
				t.Fatalf("repositories = %v, want %v", body.Repositories, tc.want)
			}
			for i := range tc.want {
				if body.Repositories[i] != tc.want[i] {
					t.Fatalf("repositories[%d] = %q, want %q", i, body.Repositories[i], tc.want[i])
				}
			}
		})
	}
}

// TestCatalogClosedInstanceChallenges (AC4): with anonymous access off, an
// anonymous catalog request answers 401 with the Bearer challenge carrying
// the registry-level catalog scope — the grant T-37's token endpoint
// narrows for any authenticated principal.
func TestCatalogClosedInstanceChallenges(t *testing.T) {
	cs := newCatalogStack(t, false)
	cs.seedDockerRepo("team1")
	cs.pushImage("team1/app", "v1")

	resp := cs.serve(http.MethodGet, "/v2/_catalog", nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous closed catalog status = %d body=%s", resp.StatusCode, body)
	}
	if eb := decodeSpecBody(t, body); eb.Errors[0].Code != ErrCodeUnauthorized {
		t.Fatalf("code = %q, want UNAUTHORIZED", eb.Errors[0].Code)
	}
	ch := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(ch, `scope="`+scopeRegistryCatalog+`"`) {
		t.Fatalf("challenge %q carries no catalog scope", ch)
	}
	if !strings.Contains(ch, `service="`+ServiceID+`"`) {
		t.Fatalf("challenge %q carries no service", ch)
	}

	// The tags/list plane stays on the content-read scope instead (AC4:
	// "tags/list 按内容读面") — the challenge names the repository.
	resp = cs.serve(http.MethodGet, "/v2/team1/app/tags/list", nil)
	body = readBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous closed tags/list status = %d body=%s", resp.StatusCode, body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `scope="repository:team1/app:pull"`) {
		t.Fatalf("tags/list challenge %q carries no repository pull scope", ch)
	}
}

// ---- route/method defense ----

// TestCatalogRouteAndMethodMatrix: the catalog is a GET resource; its
// sub-paths are not routes; tags/list equally refuses other verbs — every
// 405 carries RFC 9110's Allow header.
func TestCatalogRouteAndMethodMatrix(t *testing.T) {
	cs := newCatalogStack(t, true)
	cs.seedDockerRepo("team1")
	cs.pushImage("team1/app", "v1")

	tests := []struct {
		name   string
		method string
		path   string
		status int
		allow  string
	}{
		{"POST catalog", http.MethodPost, "/v2/_catalog", http.StatusMethodNotAllowed, http.MethodGet},
		{"PUT catalog", http.MethodPut, "/v2/_catalog", http.StatusMethodNotAllowed, http.MethodGet},
		{"catalog subpath", http.MethodGet, "/v2/_catalog/extra", http.StatusNotFound, ""},
		{"POST tags/list", http.MethodPost, "/v2/team1/app/tags/list", http.StatusMethodNotAllowed, http.MethodGet},
		{"bare tags tail", http.MethodGet, "/v2/team1/app/tags", http.StatusNotFound, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := cs.serve(tc.method, tc.path, cs.admin)
			body := readBody(t, resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("%s %s status = %d body=%s", tc.method, tc.path, resp.StatusCode, body)
			}
			if tc.allow != "" && resp.Header.Get("Allow") != tc.allow {
				t.Fatalf("Allow = %q, want %q", resp.Header.Get("Allow"), tc.allow)
			}
			// Every answer is the spec body, never the /binflow envelope.
			if tc.status != http.StatusOK {
				_ = decodeSpecBody(t, body)
			}
		})
	}
}

// ---- transport-fault branches (fake collaborators) ----

// failingListLookup fails only List (the B2 posture: a listing fault is not
// an empty catalog).
type failingListLookup struct{}

func (failingListLookup) Get(context.Context, string) (RepoRow, error) { return nil, nil }

func (failingListLookup) List(context.Context) ([]RepoRow, error) {
	return nil, fmt.Errorf("database is locked")
}

// TestCatalogFaultBranches: a repository-listing transport fault answers the
// spec-body 500; a handler assembled without a service answers 503
// honestly instead of panicking.
func TestCatalogFaultBranches(t *testing.T) {
	adminCtx := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/v2/_catalog", nil)
		return req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
	}

	t.Run("listing fault is 500", func(t *testing.T) {
		h := New(newFakeService(), failingListLookup{}, adminPassAuthorizer{}, nil, nil,
			Options{AnonymousAccess: true}, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, adminCtx())
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		_ = decodeSpecBody(t, rec.Body.Bytes())
	})

	t.Run("no service is 503", func(t *testing.T) {
		h := New(nil, NewStaticRepoLookup(map[string]string{"team1": "docker"}), adminPassAuthorizer{},
			nil, nil, Options{AnonymousAccess: true}, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, adminCtx())
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		_ = decodeSpecBody(t, rec.Body.Bytes())
	})
}
