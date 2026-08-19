package npm

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// TestPackumentGetSurface: the served packument carries the [NPM-API] shape,
// rewrites dist.tarball to this registry, never returns _attachments, and
// answers the conditional contract with the stored document's sha1.
func TestPackumentGetSurface(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	rr := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument not JSON: %v", err)
	}
	for _, k := range []string{"_id", "name", "dist-tags", "versions", "time", "_rev"} {
		if _, ok := doc[k]; !ok {
			t.Fatalf("packument missing %q: %s", k, bodyOf(rr))
		}
	}
	if _, ok := doc["_attachments"]; ok {
		t.Fatalf("packument leaked _attachments")
	}
	ver := doc["versions"].(map[string]any)["1.0.0"].(map[string]any)
	dist := ver["dist"].(map[string]any)
	wantURL := "http://registry.test/binflow/api/npm/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz"
	if dist["tarball"] != wantURL {
		t.Fatalf("tarball = %v, want %v", dist["tarball"], wantURL)
	}

	// ETag == sha1 of the STORED packument node's bytes (M22b/FR-18-AC10).
	node, err := s.md.Nodes().Get(t.Context(), "npm-local", "demo-pkg/packument.json")
	if err != nil {
		t.Fatalf("packument node: %v", err)
	}
	stored := storedNodeBody(t, s, node)
	sum := sha1.Sum(stored)
	wantETag := hex.EncodeToString(sum[:])
	if got := rr.Header().Get("ETag"); got != wantETag {
		t.Fatalf("ETag = %q, want %q", got, wantETag)
	}
	if got := rr.Header().Get("X-Checksum-Sha1"); got != wantETag {
		t.Fatalf("X-Checksum-Sha1 = %q, want %q", got, wantETag)
	}

	// Conditional revalidation with the npm-spelled quoted tag -> 304.
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal,
		map[string]string{"If-None-Match": `"` + wantETag + `"`})
	if rr.Code != http.StatusNotModified {
		t.Fatalf("If-None-Match status = %d, want 304", rr.Code)
	}
	if rr.Header().Get("ETag") != wantETag {
		t.Fatalf("304 lost the ETag header")
	}

	// The stored document stays protocol-neutral: no rewritten URL in it.
	if strings.Contains(string(stored), "registry.test") {
		t.Fatalf("stored packument carries a rendered URL: %s", stored)
	}
}

// storedNodeBody reads a node's bytes through the service (test helper).
func storedNodeBody(t *testing.T, s *stack, node *metadata.Node) []byte {
	t.Helper()
	rc, _, err := s.svc.Get(t.Context(), adminPrincipal, node.RepoKey, node.Path)
	if err != nil {
		t.Fatalf("open node %s: %v", node.Path, err)
	}
	defer rc.Close() //nolint:errcheck // read-only test helper fd
	buf := make([]byte, 0, node.Size)
	tmp := make([]byte, 4096)
	for {
		n, err := rc.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}

// TestPackumentSLIMNegotiation: the install-v1 Accept gets the slim body and
// the exact content type echoed (P1).
func TestPackumentSLIMNegotiation(t *testing.T) {
	s := newStack(t)
	pub := publishDoc("demo-pkg", "1.0.0", "TB", nil, nil)
	pub["readme"] = "A VERY LONG README"
	if rr := s.call(http.MethodPut, "/npm-local/demo-pkg", mustJSON(pub), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", rr.Code, bodyOf(rr))
	}

	rr := s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal,
		map[string]string{"Accept": contentTypeSLIM})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != contentTypeSLIM {
		t.Fatalf("content-type = %q, want %q", ct, contentTypeSLIM)
	}
	if strings.Contains(bodyOf(rr), "A VERY LONG README") {
		t.Fatalf("SLIM body kept the readme: %s", bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"dist":`) {
		t.Fatalf("SLIM body lost dist: %s", bodyOf(rr))
	}

	// The default Accept keeps the full document.
	rr = s.call(http.MethodGet, "/npm-local/demo-pkg", "", adminPrincipal, nil)
	if !strings.Contains(bodyOf(rr), "A VERY LONG README") {
		t.Fatalf("full body lost the readme: %s", bodyOf(rr))
	}
}

// TestVersionGet: /<name>/<version> serves that version's manifest with the
// rewritten tarball; unknown versions are 404.
func TestVersionGet(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	rr := s.call(http.MethodGet, "/npm-local/demo-pkg/1.0.0", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), `"version":"1.0.0"`) {
		t.Fatalf("body = %s", bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), "/binflow/api/npm/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz") {
		t.Fatalf("version tarball not rewritten: %s", bodyOf(rr))
	}
	if rr := s.call(http.MethodGet, "/npm-local/demo-pkg/9.9.9", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown version status = %d", rr.Code)
	}
}

// TestTarballDownload: the tarball GET/HEAD carries the M1 header set
// (checksum triple, ETag = sha1 unquoted, Last-Modified, Accept-Ranges),
// serves Range 206/416 and the conditional 304; the content plane spelling
// reaches the same node (NE-03's second entrance).
func TestTarballDownload(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")

	path := "/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz"
	rr := s.call(http.MethodGet, path, "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	for _, h := range []string{"X-Checksum-Sha256", "X-Checksum-Sha1", "X-Checksum-Md5", "ETag", "Last-Modified", "Accept-Ranges"} {
		if rr.Header().Get(h) == "" {
			t.Fatalf("missing %s", h)
		}
	}
	etag := rr.Header().Get("ETag")
	if len(etag) != 40 || strings.ContainsAny(etag, `"'`) {
		t.Fatalf("ETag = %q, want a bare 40-hex sha1", etag)
	}
	if rr.Body.String() != "TARBALL-demo-pkg-1.0.0" {
		t.Fatalf("tarball bytes = %q", rr.Body.String())
	}

	// HEAD answers headers without a body.
	rr = s.call(http.MethodHead, path, "", adminPrincipal, nil)
	if rr.Code != http.StatusOK || rr.Body.Len() != 0 {
		t.Fatalf("HEAD status = %d len = %d", rr.Code, rr.Body.Len())
	}

	// Range.
	rr = s.call(http.MethodGet, path, "", adminPrincipal, map[string]string{"Range": "bytes=0-4"})
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("range status = %d", rr.Code)
	}
	if rr.Body.String() != "TARBA" {
		t.Fatalf("range body = %q", rr.Body.String())
	}
	rr = s.call(http.MethodGet, path, "", adminPrincipal, map[string]string{"Range": "bytes=99999-"})
	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfiable status = %d", rr.Code)
	}

	// Conditional.
	rr = s.call(http.MethodGet, path, "", adminPrincipal, map[string]string{"If-None-Match": etag})
	if rr.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d", rr.Code)
	}
}

// TestSessionEndpoints: ping answers {} and whoami answers the username (or
// 401 anonymous); the domain root answers 200 empty; the NE-08 endpoints
// answer the E-26 404.
func TestSessionEndpoints(t *testing.T) {
	s := newStack(t)

	if rr := s.call(http.MethodGet, "/npm-local/-/ping", "", adminPrincipal, nil); rr.Code != http.StatusOK || bodyOf(rr) != "{}\n" {
		t.Fatalf("ping = %d %q", rr.Code, bodyOf(rr))
	}
	rr := s.call(http.MethodGet, "/npm-local/-/whoami", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK || !strings.Contains(bodyOf(rr), `"username":"admin"`) {
		t.Fatalf("whoami = %d %s", rr.Code, bodyOf(rr))
	}
	if rr := s.call(http.MethodGet, "/npm-local/-/whoami", "", nil, nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous whoami = %d, want 401", rr.Code)
	}

	root := s.call(http.MethodGet, "/npm-local/", "", nil, nil)
	if root.Code != http.StatusOK {
		t.Fatalf("domain root = %d", root.Code)
	}
	if bodyOf(root) != "" {
		t.Fatalf("domain root body = %q, want empty", bodyOf(root))
	}

	for _, p := range []string{
		"/npm-local/-/v1/search?text=x",
		"/npm-local/-/npm/v1/security/audits/quick",
		"/npm-local/-/npm/v1/security/advisories/bulk",
		"/npm-local/-/npm/v1/attestations/demo-pkg@1.0.0",
		"/npm-local/-/all",
		"/npm-local/_external/x",
		"/npm-local/.npm/demo-pkg/package.json",
	} {
		if rr := s.call(http.MethodGet, p, "", nil, nil); rr.Code != http.StatusNotFound {
			t.Fatalf("%s = %d, want 404", p, rr.Code)
		}
	}
}

// TestPackumentNodeNotDirectlyAddressable: the storage path of the packument
// is not a servable npm address (architecture 5.4.2 "not directly
// readable"); packument access goes through the package URL.
func TestPackumentNodeNotDirectlyAddressable(t *testing.T) {
	s := newStack(t)
	seedPackage(t, s, "demo-pkg", "1.0.0")
	if rr := s.call(http.MethodGet, "/npm-local/demo-pkg/packument.json", "", adminPrincipal, nil); rr.Code != http.StatusNotFound {
		t.Fatalf("direct packument read = %d, want 404", rr.Code)
	}
}

// TestN4NonNpmRepositoryStrict404: requests reaching this handler for a
// repository whose row is not npm answer a strict 404 (the T-63 review N4
// decision — the row decides, never a best-effort content-plane guess).
func TestN4NonNpmRepositoryStrict404(t *testing.T) {
	s := newStack(t)
	rr := s.call(http.MethodGet, "/generic-local/some/pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rr.Code, bodyOf(rr))
	}
	if !strings.Contains(bodyOf(rr), "not an npm repository") {
		t.Fatalf("body = %s", bodyOf(rr))
	}
	// Unknown repo keys answer the repo-lookup wording.
	rr = s.call(http.MethodGet, "/no-such-repo/pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusNotFound || !strings.Contains(bodyOf(rr), "Failed to find the repository") {
		t.Fatalf("unknown repo = %d %s", rr.Code, bodyOf(rr))
	}
}

// TestLoginIssuesToken: the couch login PUT with a body credential mints a
// TokenRegistry token; a wrong password is 401; an authenticated principal
// cannot log in as someone else.
func TestLoginIssuesToken(t *testing.T) {
	s := newStack(t)
	body := `{"_id":"org.couchdb.user:devel","name":"devel","password":"devpass","type":"user","roles":[]}`

	rr := s.call(http.MethodPut, "/npm-local/-/user/org.couchdb.user:devel", body, nil, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("login status = %d; body=%s", rr.Code, bodyOf(rr))
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("login body not JSON: %v", err)
	}
	tok, _ := resp["token"].(string)
	if tok == "" {
		t.Fatalf("login response carries no token: %s", bodyOf(rr))
	}
	if resp["id"] != "org.couchdb.user:devel" {
		t.Fatalf("id = %v", resp["id"])
	}
	// The minted token authenticates (TokenRegistry roundtrip).
	if _, err := s.h.tokens.Verify(t.Context(), tok); err != nil {
		t.Fatalf("minted token does not verify: %v", err)
	}

	bad := `{"_id":"org.couchdb.user:devel","name":"devel","password":"WRONG","type":"user","roles":[]}`
	if rr := s.call(http.MethodPut, "/npm-local/-/user/org.couchdb.user:devel", bad, nil, nil); rr.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rr.Code)
	}

	// Header-authenticated login of ANOTHER user is 403.
	other := `{"_id":"org.couchdb.user:devel","name":"devel","password":"x","type":"user","roles":[]}`
	if rr := s.call(http.MethodPut, "/npm-local/-/user/org.couchdb.user:devel", other, adminPrincipal, nil); rr.Code != http.StatusForbidden {
		t.Fatalf("cross-user login = %d, want 403", rr.Code)
	}
}
