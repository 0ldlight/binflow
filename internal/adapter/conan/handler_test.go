package conan

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The handler-surface legs: the capability header family (TL-2, verbatim
// on every response incl. errors), the v1 handshake trio, the class door
// (S6's pinned 400) and the v2 plane's success/error matrix.

// TestCapabilityHeadersVerbatim: every conan response — success AND error —
// carries the pinned family (TL-2): X-Conan-Server-Version 0.20.0 and the
// local capability list, plus the version-check answer when the client
// announced a version.
func TestCapabilityHeadersVerbatim(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	if code, _, _ := s.putRecipeFile("cn-local", r, fixtureRev(1), "conanfile.py", []byte("class")); code != http.StatusCreated {
		t.Fatalf("recipe PUT status = %d, want 201", code)
	}

	cases := []struct {
		name string
		path string
	}{
		{"ping", v1("cn-local", "ping")},
		{"latest", v2("cn-local", "hello/1.0/myuser/stable/latest")},
		{"revisions", v2("cn-local", "hello/1.0/myuser/stable/revisions")},
		{"file get", v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(1)+"/files/conanfile.py")},
		{"search", v2("cn-local", "search?q=hello*")},
		{"error 404", v2("cn-local", "hello/9.9/myuser/stable/latest")},
		{"error 405", v2("cn-local", "hello/1.0/myuser/stable/revisions")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, hdr := s.do(http.MethodGet, tc.path, adminUser, adminPass, nil, map[string]string{
				hdrClientVer: "2.0.14",
			})
			if code == 0 {
				t.Fatal("no response")
			}
			if got := hdr.Get(hdrServerVersion); got != "0.20.0" {
				t.Errorf("%s: %s = %q, want 0.20.0", tc.name, hdrServerVersion, got)
			}
			if got := hdr.Get(hdrServerCaps); got != capsLocal {
				t.Errorf("%s: %s = %q, want %q", tc.name, hdrServerCaps, got, capsLocal)
			}
			if got := hdr.Get(hdrClientVerCheck); got != "server_outdated" {
				t.Errorf("%s: %s = %q, want server_outdated (2.0.14 > 0.20.0)", tc.name, hdrClientVerCheck, got)
			}
		})
	}
}

// TestClientVersionCheckMatrix pins the comparison table (spec section 2):
// below floor deprecated, below pinned outdated, at pinned current, above
// server_outdated; absent answers no header.
func TestClientVersionCheckMatrix(t *testing.T) {
	cases := []struct {
		client string
		want   string
	}{
		{"0.15.9", "deprecated"},
		{"0.16.0", "outdated"},
		{"0.19.2", "outdated"},
		{"0.20.0", "current"},
		{"0.21.0", "server_outdated"},
		{"2.0.14", "server_outdated"},
		{"2.4.1", "server_outdated"},
		{"", ""},
		{"garbage", ""},
	}
	for _, tc := range cases {
		if got := clientVersionCheck(tc.client); got != tc.want {
			t.Errorf("clientVersionCheck(%q) = %q, want %q", tc.client, got, tc.want)
		}
	}
}

// TestHandshakeTrio: ping anonymous 200 empty; authenticate anonymous 401,
// with Basic 200 + plain-text token usable as Bearer; check_credentials
// 200 empty with the credential, 401 anonymous.
func TestHandshakeTrio(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)

	code, body, _ := s.get(v1("cn-local", "ping"))
	if code != http.StatusOK || body != "" {
		t.Fatalf("ping = (%d, %q), want (200, \"\")", code, body)
	}

	code, _, hdr := s.get(v1("cn-local", "users/authenticate"))
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous authenticate = %d, want 401", code)
	}
	if hdr.Get("WWW-Authenticate") == "" {
		t.Error("anonymous authenticate lacks the Basic challenge")
	}

	code, tok, _ := s.do(http.MethodGet, v1("cn-local", "users/authenticate"), adminUser, adminPass, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("authenticate = %d (body %s), want 200", code, tok)
	}
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.HasPrefix(tok, "{") {
		t.Fatalf("authenticate body %q is not a bare token", tok)
	}

	code, _, _ = s.do(http.MethodGet, v1("cn-local", "users/check_credentials"), "", "", nil,
		map[string]string{"Authorization": "Bearer " + tok})
	if code != http.StatusOK {
		t.Fatalf("check_credentials with minted Bearer = %d, want 200", code)
	}
	code, _, _ = s.get(v1("cn-local", "users/check_credentials"))
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous check_credentials = %d, want 401", code)
	}
}

// TestClassDoorS6: the v1 plane on a remote or virtual repository answers
// the pinned 400 (S6, verbatim); v2 on those classes answers the honest
// not-served refusal carrying only_v2 in the capability list.
func TestClassDoorS6(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	s.seedRepo(t, "cn-remote", repo.TypeRemote)
	s.seedRepo(t, "cn-virt", repo.TypeVirtual)

	for _, key := range []string{"cn-remote", "cn-virt"} {
		code, body, hdr := s.get(v1(key, "ping"))
		if code != http.StatusBadRequest {
			t.Errorf("%s: v1/ping = %d, want 400", key, code)
		}
		if want := msgV1LocalOnly(key); body != want {
			t.Errorf("%s: v1/ping body = %q, want %q", key, body, want)
		}
		if got := hdr.Get(hdrServerCaps); got != capsLocal+","+capsOnlyV2 {
			t.Errorf("%s: capabilities = %q, want %q", key, got, capsLocal+","+capsOnlyV2)
		}

		code, _, _ = s.do(http.MethodGet, v1(key, "users/authenticate"), adminUser, adminPass, nil, nil)
		if code != http.StatusBadRequest {
			t.Errorf("%s: v1 authenticate = %d, want 400", key, code)
		}
		code, _, _ = s.do(http.MethodDelete, v1(key, "conans/hello/1.0/_/_"), adminUser, adminPass, nil, nil)
		if code != http.StatusBadRequest {
			t.Errorf("%s: v1 delete = %d, want 400", key, code)
		}

		// v2 arms: the honest not-yet-landed refusal.
		code, body, _ = s.get(v2(key, "search?q=*"))
		if code != http.StatusNotFound || !strings.Contains(body, "remote pull-through") {
			t.Errorf("%s: v2 search = (%d, %q), want the not-served 404", key, code, body)
		}
	}
}

// TestV2LatestAndRevisionsShape: latest returns {revision,time}; revisions
// returns the isomorphic index document with the wire reference; an empty
// chain answers the pinned 404.
func TestV2LatestAndRevisionsShape(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}

	revA, revB := fixtureRev(1), fixtureRev(7)
	for _, rev := range []string{revA, revB} {
		if code, body, _ := s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("body-"+rev)); code != http.StatusCreated {
			t.Fatalf("PUT %s = %d (body %s), want 201", rev, code, body)
		}
	}

	code, body, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if code != http.StatusOK {
		t.Fatalf("latest = %d (body %s), want 200", code, body)
	}
	var latest revEntry
	if err := json.Unmarshal([]byte(body), &latest); err != nil {
		t.Fatalf("latest body %q: %v", body, err)
	}
	if latest.Revision == "" || latest.Time == "" {
		t.Fatalf("latest = %+v, want both fields", latest)
	}

	code, body, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK {
		t.Fatalf("revisions = %d, want 200", code)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body %q: %v", body, err)
	}
	if doc.Reference != "hello/1.0@myuser/stable" {
		t.Errorf("reference = %q, want hello/1.0@myuser/stable", doc.Reference)
	}
	if len(doc.Revisions) != 2 {
		t.Fatalf("revisions = %d entries, want 2", len(doc.Revisions))
	}
	// Time-descending (the index's own rule): the later-registered
	// revision leads.
	if doc.Revisions[0].Revision != revB || doc.Revisions[1].Revision != revA {
		t.Errorf("revision order = [%s, %s], want [%s, %s]",
			doc.Revisions[0].Revision, doc.Revisions[1].Revision, revB, revA)
	}

	// The empty chain answers the pinned wording (S9).
	code, body, _ = s.get(v2("cn-local", "nope/1.0/_/_/revisions"))
	if code != http.StatusNotFound || body != msgNoRevisions {
		t.Errorf("empty revisions = (%d, %q), want (404, %q)", code, body, msgNoRevisions)
	}
	code, body, _ = s.get(v2("cn-local", "nope/1.0/_/_/latest"))
	if code != http.StatusNotFound || body != msgNoRevisions {
		t.Errorf("empty latest = (%d, %q), want (404, %q)", code, body, msgNoRevisions)
	}
}

// TestV2FilesGetPutList: the file endpoints' matrix — 201 on PUT, stream
// on GET, HEAD carries Content-Length without a body, the listing renders
// the files map and never the .timestamp.
func TestV2FilesGetPutList(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "_", channel: "_"} // S12 placeholders
	rev := fixtureRev(3)
	body := []byte("from conan import ConanFile\n")

	if code, resp, _ := s.putRecipeFile("cn-local", r, rev, "conanfile.py", body); code != http.StatusCreated || resp != "" {
		t.Fatalf("PUT = (%d, %q), want (201, \"\")", code, resp)
	}
	if code, _, _ := s.putRecipeFile("cn-local", r, rev, "sub/dir/data.txt", []byte("x")); code != http.StatusCreated {
		t.Fatalf("PUT nested = %d, want 201", code)
	}

	base := v2("cn-local", "hello/1.0/_/_/revisions/"+rev+"/files")
	code, got, hdr := s.do(http.MethodGet, base+"/conanfile.py", adminUser, adminPass, nil, nil)
	if code != http.StatusOK || got != string(body) {
		t.Fatalf("GET file = (%d, %q), want the PUT body", code, got)
	}
	if hdr.Get(hdrChecksumSha256) == "" {
		t.Error("GET file lacks the X-Checksum-Sha256 header")
	}

	code, got, hdr = s.do(http.MethodHead, base+"/conanfile.py", adminUser, adminPass, nil, nil)
	if code != http.StatusOK || got != "" {
		t.Fatalf("HEAD file = (%d, %d bytes), want (200, 0)", code, len(got))
	}
	if hdr.Get("Content-Length") == "" {
		t.Error("HEAD file lacks Content-Length")
	}

	code, got, _ = s.get(base)
	if code != http.StatusOK {
		t.Fatalf("files list = %d (body %s), want 200", code, got)
	}
	var list filesResponse
	if err := json.Unmarshal([]byte(got), &list); err != nil {
		t.Fatalf("files body %q: %v", got, err)
	}
	if _, ok := list.Files["conanfile.py"]; !ok {
		t.Errorf("files = %v, want conanfile.py present", list.Files)
	}
	if _, ok := list.Files["sub/dir/data.txt"]; !ok {
		t.Errorf("files = %v, want sub/dir/data.txt present", list.Files)
	}
	if _, ok := list.Files[".timestamp"]; ok {
		t.Error("files listing leaked the .timestamp marker")
	}

	// 404 family.
	code, _, _ = s.get(base + "/missing.txt")
	if code != http.StatusNotFound {
		t.Errorf("missing file = %d, want 404", code)
	}
	code, _, _ = s.get(v2("cn-local", "hello/1.0/_/_/revisions/"+fixtureRev(9)+"/files"))
	if code != http.StatusNotFound {
		t.Errorf("missing revision files = %d, want 404", code)
	}

	// 405 with Allow.
	code, _, hdr = s.do(http.MethodPost, base+"/conanfile.py", adminUser, adminPass, nil, nil)
	if code != http.StatusMethodNotAllowed || hdr.Get("Allow") == "" {
		t.Errorf("POST file = (%d, allow %q), want 405 + Allow", code, hdr.Get("Allow"))
	}
}

// TestV2PackageFaces: the package sub-family — latest/revisions/files and
// the two DELETE arms' pinned wordings.
func TestV2PackageFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev, pid := fixtureRev(2), fixturePID(4)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))

	pkgBase := v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/packages/"+pid)
	code, _, _ := s.get(pkgBase + "/revisions")
	if code != http.StatusNotFound {
		t.Fatalf("empty pkg revisions = %d, want 404", code)
	}

	s.putPkgFile("cn-local", r, rev, pid, fixtureRev(5), "conaninfo.txt", []byte(conaninfoFixture(nil, nil, nil)))
	s.putPkgFile("cn-local", r, rev, pid, fixtureRev(5), "conan_package.tgz", []byte("tgz"))

	code, body, _ := s.get(pkgBase + "/latest")
	if code != http.StatusOK {
		t.Fatalf("pkg latest = %d (body %s)", code, body)
	}
	var latest revEntry
	if err := json.Unmarshal([]byte(body), &latest); err != nil || latest.Revision != fixtureRev(5) {
		t.Fatalf("pkg latest = %q (%v), want revision %s", body, err, fixtureRev(5))
	}

	code, body, _ = s.get(pkgBase + "/revisions")
	if code != http.StatusOK {
		t.Fatalf("pkg revisions = %d, want 200", code)
	}
	var doc pkgIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("pkg revisions body %q: %v", body, err)
	}
	if doc.Reference != "hello/1.0@myuser/stable#"+rev+":"+pid {
		t.Errorf("pkg reference = %q, want the #rrev:pid form", doc.Reference)
	}

	code, body, _ = s.get(pkgBase + "/revisions/" + fixtureRev(5) + "/files")
	if code != http.StatusOK {
		t.Fatalf("pkg files = %d, want 200", code)
	}
	if !strings.Contains(body, "conan_package.tgz") || strings.Contains(body, ".timestamp") {
		t.Errorf("pkg files = %s, want the two files without the marker", body)
	}

	// The one-pRev delete.
	code, _, _ = s.delete(pkgBase + "/revisions/" + fixtureRev(5))
	if code != http.StatusOK {
		t.Fatalf("pRev delete = %d, want 200", code)
	}
	code, _, _ = s.get(pkgBase + "/revisions")
	if code != http.StatusNotFound {
		t.Errorf("post-delete pkg revisions = %d, want 404", code)
	}

	// The all-packages delete: pinned wording on empty.
	code, body, _ = s.delete(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(9)+"/packages"))
	if code != http.StatusNotFound || body != "Couldn't find packages for deletion" {
		t.Errorf("packages delete on empty = (%d, %q), want the pinned 404", code, body)
	}
	s.putPkgFile("cn-local", r, rev, pid, fixtureRev(6), "conaninfo.txt", []byte("x"))
	code, _, _ = s.delete(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/packages"))
	if code != http.StatusOK {
		t.Fatalf("packages delete = %d, want 200", code)
	}
}

// TestV2Deletes: recipe delete removes everything (the coordinate root
// disappears from search); revision delete removes one rRev and its index
// row, leaving the sibling.
func TestV2Deletes(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	revA, revB := fixtureRev(1), fixtureRev(7)
	s.putRecipeFile("cn-local", r, revA, "conanfile.py", []byte("a"))
	s.putRecipeFile("cn-local", r, revB, "conanfile.py", []byte("b"))

	// Revision delete: pinned wording on the missing one.
	code, body, _ := s.delete(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(9)))
	if code != http.StatusNotFound || !strings.Contains(body, "Couldn't find path") {
		t.Errorf("missing rev delete = (%d, %q), want the pinned 404", code, body)
	}
	code, _, _ = s.delete(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+revA))
	if code != http.StatusOK {
		t.Fatalf("rev delete = %d, want 200", code)
	}
	code, body, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK || !strings.Contains(body, revB) || strings.Contains(body, revA) {
		t.Errorf("post-delete revisions = (%d, %s), want only the sibling", code, body)
	}

	// Recipe delete: the whole coordinate.
	code, _, _ = s.delete(v2("cn-local", "hello/1.0/myuser/stable"))
	if code != http.StatusOK {
		t.Fatalf("recipe delete = %d, want 200", code)
	}
	code, _, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if code != http.StatusNotFound {
		t.Errorf("post-delete latest = %d, want 404", code)
	}
	code, _, _ = s.delete(v2("cn-local", "hello/1.0/myuser/stable"))
	if code != http.StatusNotFound {
		t.Errorf("second recipe delete = %d, want 404", code)
	}
}

// TestV2ChecksumChain: malformed X-Checksum → 400; a disagreeing sha256 →
// 409; checksum-deploy lands zero-byte by reference and a miss answers
// the 404.
func TestV2ChecksumChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	rev := fixtureRev(4)
	path := v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/files/conanfile.py")

	code, body, _ := s.put(path, []byte("x"), map[string]string{hdrChecksumSha256: "nothex"})
	if code != http.StatusBadRequest {
		t.Errorf("malformed checksum = (%d, %s), want 400", code, body)
	}
	sum := blobRefOf([]byte("x")).Sha256
	code, _, _ = s.put(path, []byte("y"), map[string]string{hdrChecksumSha256: sum})
	if code != http.StatusConflict {
		t.Errorf("mismatched checksum = %d, want 409", code)
	}
	if code, _, _ = s.put(path, []byte("x"), map[string]string{hdrChecksumSha256: sum}); code != http.StatusCreated {
		t.Fatalf("agreeing checksum PUT = %d, want 201", code)
	}

	// Checksum deploy: reference the landed blob at a second path.
	deployPath := v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/files/conanfile2.py")
	code, _, _ = s.put(deployPath, nil, map[string]string{hdrChecksumDeploy: "true", hdrChecksumSha256: sum})
	if code != http.StatusCreated {
		t.Fatalf("checksum deploy = %d, want 201", code)
	}
	code, got, _ := s.get(deployPath)
	if code != http.StatusOK || got != "x" {
		t.Fatalf("deployed content = (%d, %q), want the referenced bytes", code, got)
	}
	code, _, _ = s.put(deployPath+"x", nil, map[string]string{
		hdrChecksumDeploy: "true",
		hdrChecksumSha256: fixtureRev(8), // a digest nothing holds
	})
	if code != http.StatusNotFound {
		t.Errorf("checksum deploy miss = %d, want 404", code)
	}
	code, body, _ = s.put(deployPath, nil, map[string]string{hdrChecksumDeploy: "true"})
	if code != http.StatusBadRequest {
		t.Errorf("checksum deploy without headers = (%d, %s), want 400", code, body)
	}
}

// TestAnonymousWriteRefused: on an anonymous-enabled instance the reads
// pass and the writes demand the credential (the content plane's own ACL;
// forceConanAuthentication is the unimplemented repo-config arm — the
// default false posture).
func TestAnonymousWriteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	s.putRecipeFile("cn-local", r, fixtureRev(1), "conanfile.py", []byte("x"))

	if code, _, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/latest")); code != http.StatusOK {
		t.Errorf("anonymous latest = %d, want 200", code)
	}
	code, _, _ := s.do(http.MethodPut,
		v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(2)+"/files/conanfile.py"),
		"", "", bytesReader([]byte("y")), nil)
	// The anonymous arm meets the write gate's 401 challenge (writes are
	// never anonymous, whatever the anonymous-access switch says).
	if code != http.StatusUnauthorized {
		t.Errorf("anonymous PUT = %d, want 401", code)
	}
}
