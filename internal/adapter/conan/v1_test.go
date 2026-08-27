package conan

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The v1 data plane's full matrix (CN-1's final scope): search, digest,
// download_urls, upload_urls, snapshot, the delete family, remove_files
// and the files channel — every endpoint's shape asserted, the base URL's
// two states (server.base_url and request-derived) included.

// seedV2Fixture lands one recipe revision plus one package revision the
// v1 readers must resolve through the index layer.
func seedV2Fixture(t *testing.T, s *stack, key string) (ref, string, string) {
	t.Helper()
	s.seedRepo(t, key, repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev, pid := fixtureRev(2), fixturePID(4)
	if code, body, _ := s.putRecipeFile(key, r, rev, "conanfile.py", []byte("recipe")); code != http.StatusCreated {
		t.Fatalf("recipe PUT = %d (body %s)", code, body)
	}
	if code, body, _ := s.putRecipeFile(key, r, rev, "conanmanifest.txt", []byte("manifest")); code != http.StatusCreated {
		t.Fatalf("manifest PUT = %d (body %s)", code, body)
	}
	if code, body, _ := s.putPkgFile(key, r, rev, pid, fixtureRev(6), "conaninfo.txt",
		[]byte(conaninfoFixture([]string{"os=Macos", "arch=x86_64"}, []string{"shared=True"}, []string{"zlib/1.2.11"}))); code != http.StatusCreated {
		t.Fatalf("pkg PUT = %d (body %s)", code, body)
	}
	if code, body, _ := s.putPkgFile(key, r, rev, pid, fixtureRev(6), "conan_package.tgz", []byte("tgz")); code != http.StatusCreated {
		t.Fatalf("pkg PUT = %d (body %s)", code, body)
	}
	return r, rev, pid
}

// TestV1Snapshot: the recipe and package snapshots answer file -> md5 of
// the LATEST revision (the v2-uploaded one — the index-layer resolution),
// excluding the .timestamp.
func TestV1Snapshot(t *testing.T) {
	s := newStack(t)
	_, _, pid := seedV2Fixture(t, s, "cn-local")

	code, body, _ := s.get(v1("cn-local", "conans/hello/1.0/myuser/stable"))
	if code != http.StatusOK {
		t.Fatalf("recipe snapshot = %d (body %s)", code, body)
	}
	var snap snapshotBody
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("snapshot body %q: %v", body, err)
	}
	_, hasRecipe := snap["conanfile.py"]
	_, hasManifest := snap["conanmanifest.txt"]
	if !hasRecipe || !hasManifest {
		t.Errorf("snapshot = %v, want both recipe files", snap)
	}
	if len(snap) != 2 {
		t.Errorf("snapshot = %v, want exactly the two files (no marker)", snap)
	}
	for _, md5 := range snap {
		if len(md5) != 32 && md5 != "" {
			t.Errorf("snapshot md5 = %q, want the 32-hex ledger value", md5)
		}
	}

	code, body, _ = s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/"+pid))
	if code != http.StatusOK {
		t.Fatalf("package snapshot = %d (body %s)", code, body)
	}
	snap = snapshotBody{}
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("package snapshot body: %v", err)
	}
	_, hasInfo := snap["conaninfo.txt"]
	_, hasTgz := snap["conan_package.tgz"]
	if !hasInfo || !hasTgz {
		t.Errorf("package snapshot = %v, want both package files", snap)
	}

	// An unknown ref answers the files-channel 404.
	code, body, _ = s.get(v1("cn-local", "conans/nope/1.0/_/_"))
	if code != http.StatusNotFound || body != msgPathNotFound {
		t.Errorf("unknown snapshot = (%d, %q), want (404, %q)", code, body, msgPathNotFound)
	}
}

// TestV1URLFamily: digest and download_urls cite the v1 files channel
// with revision `0` and the configured base; upload_urls maps its request
// keys onto PUT addresses. Both base states ride the same stack set.
func TestV1URLFamily(t *testing.T) {
	s := newStack(t)
	_, _, pid := seedV2Fixture(t, s, "cn-local")

	code, body, _ := s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/digest"))
	if code != http.StatusOK {
		t.Fatalf("digest = %d (body %s)", code, body)
	}
	var urls urlBody
	if err := json.Unmarshal([]byte(body), &urls); err != nil {
		t.Fatalf("digest body: %v", err)
	}
	if got := urls["conanmanifest.txt"]; got == "" || !strings.Contains(got, "/v1/files/myuser/hello/1.0/stable/0/export/conanmanifest.txt") {
		t.Errorf("digest url = %q, want the 0-revision channel address", got)
	}

	code, body, _ = s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/download_urls"))
	if code != http.StatusOK {
		t.Fatalf("download_urls = %d", code)
	}
	urls = urlBody{}
	if err := json.Unmarshal([]byte(body), &urls); err != nil {
		t.Fatalf("download_urls body: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("download_urls = %v, want both files", urls)
	}
	for name, u := range urls {
		if !strings.Contains(u, "/v1/files/myuser/hello/1.0/stable/0/export/"+name) {
			t.Errorf("download url for %s = %q, wrong shape", name, u)
		}
	}

	code, body, _ = s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/"+pid+"/download_urls"))
	if code != http.StatusOK {
		t.Fatalf("package download_urls = %d (body %s)", code, body)
	}
	if !strings.Contains(body, "/v1/files/myuser/hello/1.0/stable/0/package/"+pid+"/conaninfo.txt") {
		t.Errorf("package download_urls = %s, want the pid channel address", body)
	}

	code, body, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/upload_urls"),
		[]byte(`{"conanfile.py": 1024, "conanmanifest.txt": 512}`), nil)
	if code != http.StatusOK {
		t.Fatalf("upload_urls = %d (body %s)", code, body)
	}
	urls = urlBody{}
	if err := json.Unmarshal([]byte(body), &urls); err != nil {
		t.Fatalf("upload_urls body: %v", err)
	}
	if len(urls) != 2 || !strings.Contains(urls["conanfile.py"], "/v1/files/myuser/hello/1.0/stable/0/export/conanfile.py") {
		t.Errorf("upload_urls = %v, wrong shape", urls)
	}

	// The package arm's upload_urls.
	code, body, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/"+pid+"/upload_urls"),
		[]byte(`{"conaninfo.txt": 8}`), nil)
	if code != http.StatusOK || !strings.Contains(body, "/v1/files/myuser/hello/1.0/stable/0/package/"+pid+"/conaninfo.txt") {
		t.Fatalf("package upload_urls = (%d, %s)", code, body)
	}

	// A malformed body answers 400; a traversal file name answers 400.
	if code, body, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/upload_urls"), []byte(`{`), nil); code != http.StatusBadRequest {
		t.Errorf("malformed upload_urls = (%d, %s), want 400", code, body)
	}
	if code, _, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/upload_urls"),
		[]byte(`{"../escape": 1}`), nil); code != http.StatusBadRequest {
		t.Errorf("traversal upload_urls = %d, want 400", code)
	}
}

// TestV1URLBaseStates: server.base_url wins; the empty config derives
// from the request (TL-1).
func TestV1URLBaseStates(t *testing.T) {
	s := newStackOpt(t, stackOptions{baseURL: "https://conan.example.com", anonymous: true})
	seedV2Fixture(t, s, "cn-based")

	_, body, _ := s.get(v1("cn-based", "conans/hello/1.0/myuser/stable/digest"))
	if !strings.Contains(body, "https://conan.example.com/binflow/cn-based/v1/files/") {
		t.Errorf("configured base digest = %s, want the server.base_url origin", body)
	}

	s2 := newStack(t)
	seedV2Fixture(t, s2, "cn-deriv")
	_, body, _ = s2.get(v1("cn-deriv", "conans/hello/1.0/myuser/stable/digest"))
	if !strings.Contains(body, s2.srv.URL+"/binflow/cn-deriv/v1/files/") {
		t.Errorf("derived base digest = %s, want the request origin %s", body, s2.srv.URL)
	}
}

// TestV1FilesChannel: PUT lands at the `0` tree and registers the
// revision; GET serves the LATEST revision's tree through the same
// address; the miss answers the pinned wording; checksum-deploy's miss is
// the explicit 404 (S8).
func TestV1FilesChannel(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)

	put := func(p string, b []byte, hdr map[string]string) (int, string) {
		code, body, _ := s.put(v1("cn-local", "files/myuser/hello/1.0/stable/"+p), b, hdr)
		return code, body
	}
	if code, body := put("export/conanfile.py", []byte("v1 recipe"), nil); code != http.StatusCreated {
		t.Fatalf("channel PUT = (%d, %s), want 201", code, body)
	}
	// The `0/` spelling addresses the same node.
	if code, body := put("0/export/conanfile.py", []byte("v1 recipe"), nil); code != http.StatusCreated {
		t.Fatalf("channel PUT with 0 = (%d, %s), want 201 (idempotent)", code, body)
	}
	if code, _ := put("0/package/"+fixturePID(1)+"/conan_package.tgz", []byte("tgz"), nil); code != http.StatusCreated {
		t.Fatalf("package channel PUT = %d, want 201", code)
	}

	// The v1 registration made `0` the latest: the v2 plane sees it.
	code, body, _ := s.get(v2("cn-local", "hello/1.0/myuser/stable/latest"))
	if code != http.StatusOK || !strings.Contains(body, `"revision":"0"`) {
		t.Fatalf("post-v1-upload latest = (%d, %s), want revision 0 registered", code, body)
	}

	// GET serves through the same address.
	code, got, _ := s.get(v1("cn-local", "files/myuser/hello/1.0/stable/export/conanfile.py"))
	if code != http.StatusOK || got != "v1 recipe" {
		t.Fatalf("channel GET = (%d, %q)", code, got)
	}
	code, got, _ = s.get(v1("cn-local", "files/myuser/hello/1.0/stable/0/package/"+fixturePID(1)+"/conan_package.tgz"))
	if code != http.StatusOK || got != "tgz" {
		t.Fatalf("package channel GET = (%d, %q)", code, got)
	}

	// The pinned miss.
	code, got, _ = s.get(v1("cn-local", "files/myuser/hello/1.0/stable/export/missing.txt"))
	if code != http.StatusNotFound || got != msgPathNotFound {
		t.Errorf("channel miss = (%d, %q), want (404, %q)", code, got, msgPathNotFound)
	}

	// S8: checksum-deploy's miss is the explicit passthrough.
	code, got, _ = s.put(v1("cn-local", "files/myuser/hello/1.0/stable/export/new.txt"), nil,
		map[string]string{hdrChecksumDeploy: "true", hdrChecksumSha256: fixtureRev(3)})
	if code != http.StatusNotFound || got != msgPathNotFound {
		t.Errorf("channel checksum-deploy miss = (%d, %q), want the explicit 404", code, got)
	}

	// A v2 upload of a NEWER revision flips what the 0-address serves
	// (the index-layer latest resolution).
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	if code, _, _ := s.putRecipeFile("cn-local", r, fixtureRev(9), "conanfile.py", []byte("v2 recipe")); code != http.StatusCreated {
		t.Fatalf("v2 PUT = %d", code)
	}
	code, got, _ = s.get(v1("cn-local", "files/myuser/hello/1.0/stable/export/conanfile.py"))
	if code != http.StatusOK || got != "v2 recipe" {
		t.Fatalf("channel GET after v2 upload = (%d, %q), want the latest tree", code, got)
	}
}

// TestV1Deletes: DELETE conans/<ref> removes the LATEST revision chain
// (the older revisions survive — spec section 3.2's parenthetical);
// packages/delete and remove_files do their named jobs.
func TestV1Deletes(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	revOld, revNew := fixtureRev(1), fixtureRev(8)
	pid := fixturePID(2)
	s.putRecipeFile("cn-local", r, revOld, "conanfile.py", []byte("old"))
	s.putRecipeFile("cn-local", r, revNew, "conanfile.py", []byte("new"))
	s.putPkgFile("cn-local", r, revNew, pid, fixtureRev(5), "conan_package.tgz", []byte("tgz"))

	// packages/delete drops the pid under the latest revision.
	code, body, _ := s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/delete"),
		[]byte(`{"package_ids":["`+pid+`"]}`), nil)
	if code != http.StatusOK {
		t.Fatalf("packages/delete = (%d, %s)", code, body)
	}
	if code, _, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+revNew+"/packages/"+pid+"/revisions")); code != http.StatusNotFound {
		t.Errorf("post-delete pkg revisions = %d, want 404", code)
	}
	if code, _, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/delete"),
		[]byte(`{"package_ids":["not!!"]}`), nil); code != http.StatusBadRequest {
		t.Errorf("illegal pid packages/delete = %d, want 400", code)
	}

	// remove_files on the recipe arm.
	s.putRecipeFile("cn-local", r, revNew, "extra.txt", []byte("e"))
	code, _, _ = s.post(v1("cn-local", "conans/hello/1.0/myuser/stable/remove_files"),
		[]byte(`{"files":["extra.txt"]}`), nil)
	if code != http.StatusOK {
		t.Fatalf("remove_files = %d, want 200", code)
	}
	if code, _, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions/"+revNew+"/files/extra.txt")); code != http.StatusNotFound {
		t.Errorf("post-remove file = %d, want 404", code)
	}

	// DELETE the recipe: the LATEST chain goes, the older revision stays.
	code, _, _ = s.delete(v1("cn-local", "conans/hello/1.0/myuser/stable"))
	if code != http.StatusOK {
		t.Fatalf("v1 recipe delete = %d, want 200", code)
	}
	code, body, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK || !strings.Contains(body, revOld) || strings.Contains(body, revNew) {
		t.Errorf("post-delete revisions = (%d, %s), want only the older revision", code, body)
	}
	// Deleting again removes that one.
	if code, _, _ = s.delete(v1("cn-local", "conans/hello/1.0/myuser/stable")); code != http.StatusOK {
		t.Errorf("second v1 delete = %d, want 200", code)
	}
	if code, _, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/latest")); code != http.StatusNotFound {
		t.Errorf("post-second-delete latest = %d, want 404", code)
	}
}

// TestV1Search: the v1 search shares the v2 implementation and shape.
func TestV1Search(t *testing.T) {
	s := newStack(t)
	seedV2Fixture(t, s, "cn-local")

	code, body, _ := s.get(v1("cn-local", "conans/search?q=hel*"))
	if code != http.StatusOK || !strings.Contains(body, `"hello/1.0@myuser/stable"`) {
		t.Fatalf("v1 search = (%d, %s)", code, body)
	}
	code, body, _ = s.get(v1("cn-local", "conans/search?q=zzz*"))
	if code != http.StatusOK || strings.Contains(body, `"results":null`) == false && !strings.Contains(body, `"results":[]`) {
		t.Fatalf("v1 empty search = (%d, %s), want an empty (non-null) results array", code, body)
	}
}
