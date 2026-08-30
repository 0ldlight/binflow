package nuget

import (
	"bytes"
	"encoding/xml"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The T-337 v2 route matrix (nuget.md section 2's 18-endpoint table) over
// the full middleware chain, local class. The semVerLevel ladder, the
// remote proxy and the virtual merge have their own files.

// v2ContentPath renders the CONTENT-plane spelling of the v2 base (the
// api-mount rewrite currently refuses the empty-rest base root — the
// registered cross-package gap; both spellings carry identical semantics
// once it lands).
func v2ContentPath(repoKey string) string { return "/binflow/" + repoKey + "/v2" }

// mustPushV2 publishes one package through the v2 root form and fails the
// test on anything but 201.
func mustPushV2(t *testing.T, s *stack, repoKey string, pkg *nupkgFixture) {
	t.Helper()
	status, body, _ := s.put(v2ContentPath(repoKey), pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("v2 push %s: %d %s", repoKey, status, body)
	}
}

// TestV2ServiceDocument: GET the base root answers the OData service
// document with the workspace/collection shape and the DataServiceVersion
// header (nuget.md section 2 #1).
func TestV2ServiceDocument(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	for _, path := range []string{v2ContentPath("ng-local"), v2ContentPath("ng-local") + "/"} {
		status, body, hdr := s.get(path)
		if status != http.StatusOK {
			t.Fatalf("%s status = %d", path, status)
		}
		var doc struct {
			XMLName    xml.Name
			Workspaces []struct {
				Collections []struct {
					Href string `xml:"href,attr"`
				} `xml:"collection"`
			} `xml:"workspace"`
		}
		if err := xml.Unmarshal([]byte(body), &doc); err != nil {
			t.Fatalf("%s is not the service document: %v\n%s", path, err, body)
		}
		if doc.XMLName.Local != "service" || len(doc.Workspaces) != 1 ||
			len(doc.Workspaces[0].Collections) != 1 || doc.Workspaces[0].Collections[0].Href != "Packages" {
			t.Errorf("%s shape wrong:\n%s", path, body)
		}
		if v := hdr.Get("DataServiceVersion"); v == "" {
			t.Errorf("%s carries no DataServiceVersion", path)
		}
		if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "application/xml") {
			t.Errorf("%s content-type = %q", path, ct)
		}
	}
	// POST on the base root is not a v2 verb.
	if status, _, hdr := s.do(http.MethodPost, v2ContentPath("ng-local"), adminUser, adminPass, nil, nil); status != http.StatusMethodNotAllowed {
		t.Errorf("POST base root = %d, want 405", status)
	} else if !strings.Contains(hdr.Get("Allow"), "PUT") {
		t.Errorf("POST base root Allow = %q", hdr.Get("Allow"))
	}
}

// TestV2MetadataHeader: the EDMX document declares the Search/GetUpdates
// imports and carries the header (nuget.md section 2 #2).
func TestV2MetadataHeader(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	status, body, hdr := s.get(apiV2Path("ng-local") + "/$metadata")
	if status != http.StatusOK {
		t.Fatalf("$metadata status = %d", status)
	}
	for _, decl := range []string{
		`FunctionImport Name="Search"`,
		`FunctionImport Name="GetUpdates"`,
		`FunctionImport Name="FindPackagesById"`,
	} {
		if !strings.Contains(body, decl) {
			t.Errorf("$metadata misses %s:\n%s", decl, body)
		}
	}
	if v := hdr.Get("DataServiceVersion"); v == "" {
		t.Errorf("$metadata carries no DataServiceVersion")
	}
}

// TestV2SearchFaces: Search() over stored facts — the term filter, the
// prerelease policy, the 404-on-empty, the no-parens spelling and $count
// (nuget.md section 2 #3/#4).
func TestV2SearchFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	for _, p := range []struct{ id, ver string }{
		{"Alpha.Lib", "1.0.0"},
		{"Alpha.Extra", "2.0.0-beta1"},
		{"Beta.Tool", "3.1.4"},
	} {
		mustPushV2(t, s, "ng-local", buildNupkg(t, p.id, p.ver, flatDeps("none")))
	}

	feed := func(q string) (int, atomFeed) {
		status, body, _ := s.get(apiV2Path("ng-local") + "/Search()" + q)
		var doc atomFeed
		if status == http.StatusOK {
			if err := xml.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatalf("Search()%s body: %v\n%s", q, err, body)
			}
		}
		return status, doc
	}
	status, doc := feed("")
	if status != http.StatusOK || len(doc.Entries) != 2 {
		t.Fatalf("empty-term search = (%d, %d entries), want the 2 stable packages", status, len(doc.Entries))
	}
	status, doc = feed("?searchTerm='alpha'")
	if status != http.StatusOK || len(doc.Entries) != 1 {
		t.Fatalf("alpha search = (%d, %d entries), want 1", status, len(doc.Entries))
	}
	if doc.Entries[0].Properties.ID != "Alpha.Lib" {
		t.Errorf("alpha entry id = %q", doc.Entries[0].Properties.ID)
	}
	// The prerelease view adds the beta package; its entry is the beta.
	status, doc = feed("?searchTerm='extra'&includePrerelease=true")
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "2.0.0-beta1" {
		t.Fatalf("extra prerelease search = (%d, %+v)", status, doc)
	}
	// No stable match: the 404 (the empty-collection semantics).
	if status, _ := feed("?searchTerm='nosuch'"); status != http.StatusNotFound {
		t.Errorf("nosuch search = %d, want 404", status)
	}
	// The no-parens spelling routes identically.
	if status, _, _ := s.get(apiV2Path("ng-local") + "/Search?searchTerm='alpha'"); status != http.StatusOK {
		t.Errorf("no-parens Search = %d", status)
	}
	// $count.
	if status, body, _ := s.get(apiV2Path("ng-local") + "/Search()/$count"); status != http.StatusOK || strings.TrimSpace(body) != "2" {
		t.Errorf("Search/$count = (%d, %q), want 2", status, body)
	}
}

// TestV2PackagesFamily: the Packages() feed, the single-entry addressing,
// the Id projection, the miss family and $count (nuget.md section 2
// #7-#10).
func TestV2PackagesFamily(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Pk.One", "1.0.0", flatDeps("none")))
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Pk.One", "1.1.0", flatDeps("none")))

	status, body, _ := s.get(apiV2Path("ng-local") + "/Packages()")
	if status != http.StatusOK {
		t.Fatalf("Packages() = %d %s", status, body)
	}
	var feed atomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("Packages() body: %v", err)
	}
	if len(feed.Entries) != 2 {
		t.Fatalf("Packages() entries = %d, want 2", len(feed.Entries))
	}
	if feed.Entries[0].Properties.Version != "1.0.0" || feed.Entries[1].Properties.Version != "1.1.0" {
		t.Errorf("Packages() order = %s, %s", feed.Entries[0].Properties.Version, feed.Entries[1].Properties.Version)
	}

	// The single entry (#8) — hit and miss.
	status, body, _ = s.get(apiV2Path("ng-local") + "/Packages(Id='Pk.One',Version='1.1.0')")
	// T-351 D-2: the standalone entry carries its own d/m declarations.
	if status != http.StatusOK || !strings.Contains(body, `<entry xmlns:d=`) || strings.Contains(body, "<feed") {
		t.Fatalf("single entry = (%d, %s…)", status, firstLine(body))
	}
	if status, _, _ = s.get(apiV2Path("ng-local") + "/Packages(Id='Pk.One',Version='9.9.9')"); status != http.StatusNotFound {
		t.Errorf("entry miss = %d, want 404", status)
	}

	// The Id projection (#9).
	status, body, _ = s.get(apiV2Path("ng-local") + "/Packages(Id='Pk.One')/Id")
	if status != http.StatusOK || !strings.Contains(body, "<d:Id ") || !strings.Contains(body, "Pk.One") {
		t.Errorf("Id projection = (%d, %s…)", status, firstLine(body))
	}

	// $count (#10).
	if status, body, _ = s.get(apiV2Path("ng-local") + "/Packages()/$count"); status != http.StatusOK || strings.TrimSpace(body) != "2" {
		t.Errorf("Packages/$count = (%d, %q)", status, body)
	}
	// $count never addresses a single entry.
	if status, _, _ = s.get(apiV2Path("ng-local") + "/Packages(Id='Pk.One')/$count"); status != http.StatusNotFound {
		t.Errorf("entry $count = %d, want 404", status)
	}
}

// TestV2FindPackagesCountAndSemantics: FindPackagesById's collection
// semantics — the 200 empty feed on unknown ids, the $count twin, and the
// missing-id 404 (nuget.md section 2 #5/#6).
func TestV2FindPackagesCountAndSemantics(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Count.Pkg", "1.0.0", flatDeps("none")))
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Count.Pkg", "2.0.0", flatDeps("none")))

	if status, body, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()/$count?id='Count.Pkg'"); status != http.StatusOK || strings.TrimSpace(body) != "2" {
		t.Errorf("FindPackagesById/$count = (%d, %q)", status, body)
	}
	status, body, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()/$count?id='No.Such'")
	if status != http.StatusOK || strings.TrimSpace(body) != "0" {
		t.Errorf("unknown id $count = (%d, %q), want 0 (the collection counts)", status, body)
	}
	// Unknown id answers the EMPTY feed with 200 — never the 404.
	status, body, _ = s.get(apiV2Path("ng-local") + "/FindPackagesById()?id='No.Such'")
	if status != http.StatusOK || !strings.Contains(body, "<feed") || strings.Contains(body, "<entry>") {
		t.Errorf("unknown id feed = (%d, %s…)", status, firstLine(body))
	}
	if status, _, _ := s.get(apiV2Path("ng-local") + "/FindPackagesById()?id=''"); status != http.StatusNotFound {
		t.Errorf("empty id = %d, want 404", status)
	}
}

// TestV2GetUpdatesFaces: the server-side semantics live-verified against
// nuget.org (nuget.md section 2 #11/#12): strictly-newer versions, the
// prerelease policy, the version range, latest-only vs all-versions, and
// the 404 on nothing-new.
func TestV2GetUpdatesFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0", "2.1.0-beta1"} {
		mustPushV2(t, s, "ng-local", buildNupkg(t, "Up.Pkg", v, flatDeps("none")))
	}

	get := func(q string) (int, []string) {
		status, body, _ := s.get(apiV2Path("ng-local") + "/GetUpdates()/" + q)
		if status != http.StatusOK {
			return status, nil
		}
		var feed atomFeed
		if err := xml.Unmarshal([]byte(body), &feed); err != nil {
			t.Fatalf("GetUpdates body: %v\n%s", err, body)
		}
		versions := make([]string, 0, len(feed.Entries))
		for _, e := range feed.Entries {
			versions = append(versions, e.Properties.Version)
		}
		return status, versions
	}

	status, versions := get("?packageIds='Up.Pkg'&versions='1.0.0'")
	if status != http.StatusOK || len(versions) != 1 || versions[0] != "2.0.0" {
		t.Fatalf("latest-only update = (%d, %v), want [2.0.0]", status, versions)
	}
	status, versions = get("?packageIds='Up.Pkg'&versions='1.0.0'&includeAllVersions=true")
	if status != http.StatusOK || len(versions) != 2 || versions[0] != "1.1.0" || versions[1] != "2.0.0" {
		t.Fatalf("all-versions update = (%d, %v), want [1.1.0 2.0.0]", status, versions)
	}
	status, versions = get("?packageIds='Up.Pkg'&versions='1.0.0'&includeAllVersions=true&versionConstraints='[1.0.5,2.0.0)'")
	if status != http.StatusOK || len(versions) != 1 || versions[0] != "1.1.0" {
		t.Fatalf("constrained update = (%d, %v), want [1.1.0]", status, versions)
	}
	status, versions = get("?packageIds='Up.Pkg'&versions='1.0.0'&includeAllVersions=true&includePrerelease=true")
	if status != http.StatusOK || len(versions) != 3 || versions[2] != "2.1.0-beta1" {
		t.Fatalf("prerelease update = (%d, %v), want the beta tail", status, versions)
	}
	// Nothing newer: the 404 (empty collection).
	if status, _ := get("?packageIds='Up.Pkg'&versions='2.0.0'"); status != http.StatusNotFound {
		t.Errorf("nothing-new update = %d, want 404", status)
	}
	// $count.
	if status, body, _ := s.get(apiV2Path("ng-local") + "/GetUpdates()/$count?packageIds='Up.Pkg'&versions='1.0.0'&includeAllVersions=true"); status != http.StatusOK || strings.TrimSpace(body) != "2" {
		t.Errorf("GetUpdates/$count = (%d, %q)", status, body)
	}
}

// TestV2DownloadAndBareFaces: the protocolized Download face (#14) and the
// bare .nupkg download (#15).
func TestV2DownloadAndBareFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	pkg := buildNupkg(t, "Dl.Pkg", "1.2.3", flatDeps("none"))
	mustPushV2(t, s, "ng-local", pkg)

	status, body, hdr := s.get(apiV2Path("ng-local") + "/Download/dl.pkg/1.2.3")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("Download = %d (len %d, want %d)", status, len(body), len(pkg.body))
	}
	if ct := hdr.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Download content-type = %q", ct)
	}
	status, body, _ = s.get(apiV2Path("ng-local") + "/Download/dl.pkg/9.9.9")
	if status != http.StatusNotFound || !strings.Contains(body, "Unable to find NuPkg 'dl.pkg-9.9.9' in 'ng-local'") {
		t.Errorf("Download miss = (%d, %q)", status, firstLine(body))
	}

	// The bare .nupkg face: any depth, the last segment .nupkg, storage
	// addressing verbatim (the flatcontainer layout's identity path).
	status, body, _ = s.get(apiV2Path("ng-local") + "/dl.pkg/1.2.3/dl.pkg.1.2.3.nupkg")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("bare nupkg = %d (len %d)", status, len(body))
	}
}

// TestV2PublishForms: the PUT pair — the root form (#17, the dotnet
// multipart carrier with the package field) and the path-prefix form
// (#18); the prefix publish stays addressable through the three-level
// chain (nuget.md sections 5.1/6).
func TestV2PublishForms(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)

	// The root form as dotnet sends it: multipart/form-data, field "package".
	pkg := buildNupkg(t, "Root.Pkg", "1.0.0", flatDeps("none"))
	var mp bytes.Buffer
	mw := multipart.NewWriter(&mp)
	fw, err := mw.CreateFormFile("package", "package.nupkg")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	if _, err := fw.Write(pkg.body); err != nil {
		t.Fatalf("form write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("form close: %v", err)
	}
	status, body, _ := s.do(http.MethodPut, v2ContentPath("ng-local"), adminUser, adminPass, &mp, map[string]string{"Content-Type": mw.FormDataContentType()})
	if status != http.StatusCreated {
		t.Fatalf("root publish = %d %s", status, body)
	}
	if want := "Successfully published NuPkg to: root.pkg/1.0.0/root.pkg.1.0.0" + suffixNupkg; !strings.Contains(body, want) {
		t.Errorf("root publish body = %q, want %q", firstLine(body), want)
	}

	// The multipart body WITHOUT the package field: the exact-wording 400.
	var broken bytes.Buffer
	bw := multipart.NewWriter(&broken)
	if fw2, err := bw.CreateFormFile("notpackage", "x"); err == nil {
		_, _ = fw2.Write([]byte("junk")) //nolint:errcheck // test fixture
	}
	_ = bw.Close() //nolint:errcheck // test fixture
	status, body, _ = s.do(http.MethodPut, v2ContentPath("ng-local"), adminUser, adminPass, &broken, map[string]string{"Content-Type": bw.FormDataContentType()})
	if status != http.StatusBadRequest || !strings.Contains(body, "Unable to find 'package' field in request form data.") {
		t.Errorf("missing-field publish = (%d, %q)", status, firstLine(body))
	}

	// The path-prefix form (#18): the URL is a PREFIX; the identity is the
	// nuspec's; the package lands at <prefix>/<id>.<version>.nupkg.
	pfx := buildNupkg(t, "Pfx.Pkg", "2.0.0", flatDeps("none"))
	status, body, _ = s.put(v2ContentPath("ng-local")+"/team/pfx.pkg/2.0.0", pfx.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("prefix publish = %d %s", status, body)
	}
	if want := "Successfully published NuPkg to: team/pfx.pkg/2.0.0/pfx.pkg.2.0.0" + suffixNupkg; !strings.Contains(body, want) {
		t.Errorf("prefix publish body = %q", firstLine(body))
	}
	// The v2 feed finds the prefixed package through the deep chain (the
	// property-index level), and Download serves it.
	status, body, _ = s.get(apiV2Path("ng-local") + "/FindPackagesById()?id='Pfx.Pkg'")
	if status != http.StatusOK || !strings.Contains(body, "2.0.0") {
		t.Fatalf("prefixed feed = (%d, %s…)", status, firstLine(body))
	}
	status, body, _ = s.get(apiV2Path("ng-local") + "/Download/pfx.pkg/2.0.0")
	if status != http.StatusOK || body != string(pfx.body) {
		t.Fatalf("prefixed download = %d (len %d)", status, len(body))
	}
	// The bare face addresses the literal landed path too.
	status, body, _ = s.get(apiV2Path("ng-local") + "/team/pfx.pkg/2.0.0/pfx.pkg.2.0.0.nupkg")
	if status != http.StatusOK || body != string(pfx.body) {
		t.Fatalf("prefixed bare download = %d", status)
	}
}

// TestV2DeleteFaces: the v2 hard delete (#16) — the 200 wording, the
// canonical and the prefixed resolutions, the 404 and the rclass refusals.
func TestV2DeleteFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Del.Pkg", "1.0.0", flatDeps("none")))
	pkg2 := buildNupkg(t, "Del.Pfx", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(v2ContentPath("ng-local")+"/staging/del.pfx/1.0.0", pkg2.body, nil); status != http.StatusCreated {
		t.Fatalf("prefix push: %d %s", status, body)
	}

	// The canonical resolution.
	status, body, _ := s.delete(apiV2Path("ng-local") + "/del.pkg/1.0.0")
	if status != http.StatusOK || !strings.Contains(body, "Successfully removed 'del.pkg/1.0.0'") {
		t.Fatalf("delete = (%d, %q)", status, firstLine(body))
	}
	if status, _, _ = s.get(apiV2Path("ng-local") + "/Download/del.pkg/1.0.0"); status != http.StatusNotFound {
		t.Errorf("post-delete download = %d, want 404", status)
	}
	// The prefixed resolution (the third level finds it; extra segments
	// are ignored — the first two segments are the id/version).
	status, body, _ = s.delete(apiV2Path("ng-local") + "/del.pfx/1.0.0/extra/segments")
	if status != http.StatusOK || !strings.Contains(body, "Successfully removed 'del.pfx/1.0.0/extra/segments'") {
		t.Fatalf("prefixed delete = (%d, %q)", status, firstLine(body))
	}
	// Miss and remote refusal.
	if status, _, _ = s.delete(apiV2Path("ng-local") + "/del.pkg/9.9.9"); status != http.StatusNotFound {
		t.Errorf("delete miss = %d, want 404", status)
	}
	if status, body, _ = s.delete(apiV2Path("ng-remote") + "/x.pkg/1.0.0"); status != http.StatusBadRequest || !strings.Contains(body, "This operation can only be performed on local repositories.") {
		t.Errorf("remote delete = (%d, %q)", status, firstLine(body))
	}
}

// TestV2PublishDuplicateArm: section 5.1's four-arm matrix, table-driven
// (D-10, ruled 2026-08-30 — the DE predicate is exists && !canDelete,
// byte-blind by construction):
//
//	②a different bytes + w-only principal → 409, the exact wording;
//	②b SAME bytes + w-only principal      → 409, the exact wording — the
//	  T-378 FLIP: the as-built 201 idempotent arm died with the ruling.
//	  The M12 L03 parity assertion (T-356's as-built 201 record) is
//	  reversed HERE; T-378 is that leg's exemption of record;
//	③  the delete right holds             → overwrite, 201 (both the
//	  different-bytes and the same-bytes spelling), Download serves the
//	  pushed bytes;
//	④  fresh deployPath                   → 201, w alone suffices.
//
// The rclass 400s (remote, unrouted virtual) ride below the table.
func TestV2PublishDuplicateArm(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	s.seedUser(t, "writer", "writerpass") // w but no d
	s.seedGrant(t, "nuget-writer", "writer", "ng-local", true, true, false)

	// push builds one fresh package and publishes it as the given
	// principal, answering (status, body, pushed bytes). Identical
	// id/version/deps rebuild byte-identical packages — the fixture is
	// deterministic, which is what the same-bytes arms rest on.
	push := func(id, deps, user, pass string) (int, string, string) {
		pkg := buildNupkg(t, id, "1.0.0", deps)
		status, body, _ := s.do(http.MethodPut, v2ContentPath("ng-local"), user, pass, bytesReader(pkg.body), nil)
		return status, body, string(pkg.body)
	}

	tests := []struct {
		name string
		id   string
		// seed true first lands the deployPath as admin with seedDeps
		// (arm ④ seeds nothing — the case push IS the first; the deps
		// strings may legitimately be "" since flatDeps("none") is the
		// no-dependency spelling, so the flag is explicit).
		seed     bool
		seedDeps string
		pushDeps string
		asWriter bool
		want     int
		wantBody string
		// downloadWant: after a 201, Download must serve the case's own
		// pushed bytes (arm ③'s overwrite proof).
		downloadWant bool
	}{
		{
			name:     "arm2a-different-bytes-w-only",
			id:       "Dup.A",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("Serilog", "4.0.0"),
			asWriter: true,
			want:     http.StatusConflict,
			wantBody: "Package already exist: dup.a/1.0.0/dup.a.1.0.0" + suffixNupkg,
		},
		{
			name:     "arm2b-same-bytes-w-only (D-10 flip, T-378; M12 L03 reversed)",
			id:       "Dup.B",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("none"), // == seed deps: byte-identical package
			asWriter: true,
			want:     http.StatusConflict,
			wantBody: "Package already exist: dup.b/1.0.0/dup.b.1.0.0" + suffixNupkg,
		},
		{
			name:         "arm3-d-right-overwrites-different-bytes",
			id:           "Dup.C",
			seed:         true,
			seedDeps:     flatDeps("none"),
			pushDeps:     flatDeps("Serilog", "4.0.0"),
			asWriter:     false, // admin holds d
			want:         http.StatusCreated,
			wantBody:     "Successfully published NuPkg to: dup.c/1.0.0/dup.c.1.0.0" + suffixNupkg,
			downloadWant: true,
		},
		{
			name:     "arm3-d-right-same-bytes-retransmit",
			id:       "Dup.E",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("none"), // same bytes, but d holds: still 201
			asWriter: false,
			want:     http.StatusCreated,
			wantBody: "Successfully published NuPkg to: dup.e/1.0.0/dup.e.1.0.0" + suffixNupkg,
		},
		{
			name:     "arm4-fresh-package",
			id:       "Dup.D",
			pushDeps: flatDeps("none"),
			asWriter: true,
			want:     http.StatusCreated,
			wantBody: "Successfully published NuPkg to: dup.d/1.0.0/dup.d.1.0.0" + suffixNupkg,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.seed {
				if status, body, _ := push(tc.id, tc.seedDeps, adminUser, adminPass); status != http.StatusCreated {
					t.Fatalf("seed publish: %d %s", status, body)
				}
			}
			user, pass := adminUser, adminPass
			if tc.asWriter {
				user, pass = "writer", "writerpass"
			}
			status, body, pushed := push(tc.id, tc.pushDeps, user, pass)
			if status != tc.want {
				t.Fatalf("publish = %d %s, want %d", status, body, tc.want)
			}
			if !strings.Contains(body, tc.wantBody) {
				t.Errorf("body = %q, want it to contain %q", firstLine(body), tc.wantBody)
			}
			if tc.downloadWant {
				gstatus, gbody, _ := s.get(apiV2Path("ng-local") + "/Download/" + lowerASCII(tc.id) + "/1.0.0")
				if gstatus != http.StatusOK || gbody != pushed {
					t.Errorf("overwritten download = (%d, len %d), want the pushed bytes", gstatus, len(gbody))
				}
			}
		})
	}

	// Remote and unrouted virtual: the rclass 400s (before any drain).
	probe := buildNupkg(t, "Dup.R", "1.0.0", flatDeps("none"))
	status, body, _ := s.put(v2ContentPath("ng-remote"), probe.body, nil)
	if status != http.StatusBadRequest || !strings.Contains(body, "This operation can only be performed on local repositories.") {
		t.Errorf("remote publish = (%d, %q)", status, firstLine(body))
	}
	status, body, _ = s.put(v2ContentPath("ng-virt"), probe.body, nil)
	if status != http.StatusBadRequest || !strings.Contains(body, "This operation can only be performed on local repositories.") {
		t.Errorf("unrouted virtual publish = (%d, %q)", status, firstLine(body))
	}
}

// TestV2AuthDoors: the two-door posture rides the shared middleware chain
// (nuget.md section 3's BinFlow merge): anonymous access off answers the
// 401 challenge on the feed faces, the authorized credential serves; a
// credential without grants answers the 403.
func TestV2AuthDoors(t *testing.T) {
	s := newStackOpt(t, stackOptions{noAnonymous: true})
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	mustPushV2(t, s, "ng-local", buildNupkg(t, "Gate.Pkg", "1.0.0", flatDeps("none")))
	s.seedUser(t, "stranger", "strangerpass")

	status, _, hdr := s.get(apiV2Path("ng-local") + "/Search()")
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous Search = %d, want 401", status)
	}
	if ch := hdr.Get("WWW-Authenticate"); !strings.HasPrefix(ch, "Basic ") {
		t.Errorf("WWW-Authenticate = %q", ch)
	}
	status, _, hdr = s.get(apiV2Path("ng-local") + "/$metadata")
	if status != http.StatusUnauthorized || hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("anonymous $metadata = (%d)", status)
	}
	// The bare .nupkg face rides the storage door (the read gate).
	if status, _, _ = s.get(apiV2Path("ng-local") + "/gate.pkg/1.0.0/gate.pkg.1.0.0.nupkg"); status != http.StatusUnauthorized {
		t.Errorf("anonymous bare nupkg = %d, want 401", status)
	}
	// The admin credential serves everything.
	if status, _, _ := s.do(http.MethodGet, apiV2Path("ng-local")+"/Search()", adminUser, adminPass, nil, nil); status != http.StatusOK {
		t.Errorf("admin Search = %d", status)
	}
	// A stranger's credential: the 403 door.
	if status, _, _ := s.do(http.MethodGet, apiV2Path("ng-local")+"/Search()", "stranger", "strangerpass", nil, nil); status != http.StatusForbidden {
		t.Errorf("stranger Search = %d, want 403", status)
	}
}

// TestV2ODataOptions: the generic option face — $filter's latest-flag and
// Id forms, the semVerLevel drop of the IsLatestVersion filter, $orderby,
// $top/$skip and $inlinecount (nuget.md section 4).
func TestV2ODataOptions(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	for _, v := range []string{"1.0.0", "1.1.0", "2.0.0"} {
		mustPushV2(t, s, "ng-local", buildNupkg(t, "Opt.Pkg", v, flatDeps("none")))
	}

	odatas := func(vals ...string) string {
		q := make(url.Values)
		for i := 0; i+1 < len(vals); i += 2 {
			q.Add(vals[i], vals[i+1])
		}
		return "?" + q.Encode()
	}
	feed := func(q string) (int, atomFeed) {
		status, body, _ := s.get(apiV2Path("ng-local") + "/Packages()" + q)
		var doc atomFeed
		if status == http.StatusOK {
			if err := xml.Unmarshal([]byte(body), &doc); err != nil {
				t.Fatalf("Packages()%s: %v\n%s", q, err, body)
			}
		}
		return status, doc
	}

	status, doc := feed(odatas("$filter", "IsLatestVersion eq true"))
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "2.0.0" {
		t.Fatalf("latest filter = (%d, %d entries)", status, len(doc.Entries))
	}
	status, doc = feed(odatas("$filter", "IsAbsoluteLatestVersion eq true"))
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "2.0.0" {
		t.Fatalf("absolute-latest filter = (%d entries)", len(doc.Entries))
	}
	status, doc = feed(odatas("$filter", "Id eq 'Opt.Pkg'"))
	if status != http.StatusOK || len(doc.Entries) != 3 {
		t.Fatalf("Id filter = (%d entries)", len(doc.Entries))
	}
	// The semVerLevel=2.0.0 + IsLatestVersion combination DROPS the filter
	// (nuget.ignoreIsLatestVersionFilter defaults true).
	status, doc = feed(odatas("$filter", "IsLatestVersion eq true", "semVerLevel", "2.0.0"))
	if status != http.StatusOK || len(doc.Entries) != 3 {
		t.Fatalf("dropped filter = (%d entries), want all 3", len(doc.Entries))
	}
	// An unsupported filter is the honest 400.
	if status, _ := feed(odatas("$filter", "Version eq '1.0.0'")); status != http.StatusBadRequest {
		t.Errorf("unsupported filter = %d, want 400", status)
	}

	// $orderby desc + $top + $skip + $inlinecount.
	status, doc = feed(odatas("$orderby", "Version desc", "$top", "1"))
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "2.0.0" {
		t.Fatalf("orderby/top = (%d entries, first %s)", len(doc.Entries), firstVersion(doc))
	}
	status, doc = feed(odatas("$orderby", "Version desc", "$skip", "1", "$top", "1", "$inlinecount", "allpages"))
	if status != http.StatusOK || len(doc.Entries) != 1 || doc.Entries[0].Properties.Version != "1.1.0" {
		t.Fatalf("skip = first %s", firstVersion(doc))
	}
	if _, body, _ := s.get(apiV2Path("ng-local") + "/Packages()" + odatas("$inlinecount", "allpages", "$top", "1")); !strings.Contains(body, "<m:count>3</m:count>") {
		t.Errorf("inlinecount body misses the total:\n%s", firstLine(body))
	}
	// Parameter names fold case-insensitively.
	if status, _, _ := s.get(apiV2Path("ng-local") + "/Packages()?%24FILTER=" + url.QueryEscape("IsLatestVersion eq true")); status != http.StatusOK {
		t.Errorf("folded-case $filter = %d", status)
	}
}

// firstVersion is the order assertions' helper.
func firstVersion(doc atomFeed) string {
	if len(doc.Entries) == 0 {
		return "(none)"
	}
	return doc.Entries[0].Properties.Version
}
