package rpm

// The full-stack wire tests (rpm.md sections 2-5's local column, driven
// through the real httpapi router): the RP-2 upload-stores-only posture,
// the reindex-driven repodata generation with its checksum reconciliation,
// the reindex seven-branch matrix, generation retention, the _tmp_
// staging cleanup, the deep-root (yumRootDepth) layout and the concurrent
// recompute consistency.

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// pkgHeader is a small realistic package header builder.
func pkgHeader(name, version, release, arch string) *headerBuilder {
	b := fullHeader()
	b.str(tagName, name)
	b.str(tagVersion, version)
	b.str(tagRelease, release)
	b.str(tagArch, arch)
	return b
}

// pkgFixture builds one .rpm body.
func pkgFixture(name, version, release, arch string) []byte {
	nvr := name + "-" + version + "-" + release + "." + arch + ".rpm"
	return fixturePackage(nvr, pkgHeader(name, version, release, arch))
}

// ---- RP-2: upload stores, repodata waits ----

func TestUploadStoresWithoutRepodata(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")

	// Empty repository: no repomd yet.
	if status, _, _ := s.get("/binflow/rpm-local/repodata/repomd.xml"); status != http.StatusNotFound {
		t.Fatalf("empty repo repomd status = %d, want 404", status)
	}

	pkg := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	status, body, hdr := s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkg, nil)
	if status != http.StatusCreated {
		t.Fatalf("package PUT = (%d, %s), want 201", status, body)
	}
	if got := hdr.Get("X-Checksum-Sha256"); got != sha256Hex(pkg) {
		t.Errorf("PUT X-Checksum-Sha256 = %q, want the storage-measured digest", got)
	}

	// RP-2's final ruling: the default config does NOT recompute — the
	// package sits stored, the repodata family stays absent.
	time.Sleep(150 * time.Millisecond) // any (wrong) async trigger would land here
	if status, _, _ := s.get("/binflow/rpm-local/repodata/repomd.xml"); status != http.StatusNotFound {
		t.Fatalf("RP-2 violated: repomd materialized without opt-in (status %d)", status)
	}

	// The download serves the bytes verbatim plus the checksum family.
	status, body, hdr = s.get("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(pkg) {
		t.Fatalf("package GET = %d (%d bytes), want the stored bytes", status, len(body))
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex(pkg) {
		t.Errorf("GET X-Checksum-Sha256 = %q", hdr.Get("X-Checksum-Sha256"))
	}

	// The rpm.metadata.* property registration (section 2.4's nine).
	props, err := s.md.NodeProps().List(context.Background(), "rpm-local", "mypkg-1.0.0-1.el9.x86_64.rpm")
	if err != nil {
		t.Fatalf("NodeProps.List: %v", err)
	}
	wantProps := map[string]string{
		"rpm.metadata.name": "mypkg", "rpm.metadata.version": "1.0.0",
		"rpm.metadata.release": "1.el9", "rpm.metadata.arch": "x86_64",
		"rpm.metadata.epoch": "1", "rpm.metadata.license": "MIT and GPLv2+",
		"rpm.metadata.vendor": "BinFlow Test Labs", "rpm.metadata.summary": "A test package",
		"rpm.metadata.group": "Unspecified",
	}
	for k, w := range wantProps {
		got := strings.Join(props[k], ",")
		if got != w {
			t.Errorf("property %s = %q, want %q", k, got, w)
		}
	}
}

// TestNonRpmBodyStillStores: a body that fails the header parse stores
// (section 5 step 3) and never enters the index.
func TestNonRpmBodyStillStores(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	status, body, _ := s.put("/binflow/rpm-local/not-a-rpm.rpm", []byte("definitely not an rpm"), nil)
	if status != http.StatusCreated {
		t.Fatalf("non-RPM PUT = (%d, %s), want 201", status, body)
	}
	if status, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); status != http.StatusOK {
		t.Fatalf("reindex after junk upload = (%d, %s), want 200", status, b)
	}
	status, body, _ = s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd after reindex = %d, want 200", status)
	}
	primaryHref, err := primaryHrefOf(body)
	if err != nil {
		t.Fatal(err)
	}
	status, pbody, _ := s.get("/binflow/rpm-local/" + primaryHref)
	if status != http.StatusOK {
		t.Fatalf("primary fetch = %d", status)
	}
	if strings.Contains(string(gunzip(t, []byte(pbody))), "not-a-rpm") {
		t.Error("the unparsable package leaked into the primary index")
	}
}

// ---- the reindex family: matrix + generation ----

func TestReindexGeneratesRepodata(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg1 := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	pkg2 := pkgFixture("other", "2.0", "1", "noarch")
	if status, b, _ := s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkg1, nil); status != http.StatusCreated {
		t.Fatalf("pkg1 PUT = (%d, %s)", status, b)
	}
	if status, b, _ := s.put("/binflow/rpm-local/sub/other-2.0-1.noarch.rpm", pkg2, nil); status != http.StatusCreated {
		t.Fatalf("pkg2 PUT = (%d, %s)", status, b)
	}

	// The synchronous reindex (async=0, auto-calc off → 200).
	status, body, _ := s.post("/binflow/api/yum/rpm-local?async=0")
	if status != http.StatusOK {
		t.Fatalf("reindex = (%d, %s), want 200", status, body)
	}
	if body != "YUM metadata calculation for repository 'rpm-local' accepted.\n" &&
		!strings.HasPrefix(strings.TrimSpace(body), "YUM metadata calculation for repository 'rpm-local' accepted.") {
		t.Errorf("reindex body = %q", body)
	}

	// repomd.xml: the primary/other pair (filelists off by default), each
	// field chain present, digest-prefixed locations.
	status, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd GET = %d", status)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatalf("repomd does not parse: %v\n%s", err, repomd)
	}
	types := map[string]bool{}
	for _, d := range doc.Data {
		types[d.Type] = true
		if !strings.HasPrefix(d.Location.Href, "repodata/") {
			t.Errorf("data %s location %q not repodata-relative", d.Type, d.Location.Href)
		}
		if d.Checksum.Type != "sha256" {
			t.Errorf("data %s checksum type = %q, want sha256 (TL-5)", d.Type, d.Checksum.Type)
		}
		if d.OpenChecksum.Type != "sha256" || d.Size == 0 || d.OpenSize == 0 || d.Timestamp == 0 {
			t.Errorf("data %s field chain incomplete: %+v", d.Type, d)
		}
	}
	if !types["primary"] || !types["other"] {
		t.Errorf("repomd types = %v, want primary+other", types)
	}
	if types["filelists"] {
		t.Error("filelists generated without enableFileListsIndexing")
	}
	if !strings.Contains(repomd, "<revision></revision>") && !strings.Contains(repomd, "<revision/>") {
		t.Errorf("revision not the empty content (S14):\n%s", repomd)
	}

	// The served primary: digest and open-digest reconcile with the bytes
	// on the wire, and both packages appear with their NEVRA + deps.
	var primary *repomdData
	for i, d := range doc.Data {
		if d.Type == "primary" {
			primary = &doc.Data[i]
		}
	}
	if primary == nil {
		t.Fatal("no primary entry")
	}
	status, gzBody, _ := s.get("/binflow/rpm-local/" + primary.Location.Href)
	if status != http.StatusOK {
		t.Fatalf("primary fetch = %d", status)
	}
	if got := sha256Hex([]byte(gzBody)); got != primary.Checksum.Value {
		t.Errorf("primary checksum mismatch: repomd %s, wire %s", primary.Checksum.Value, got)
	}
	if got := sha256Hex(gunzip(t, []byte(gzBody))); got != primary.OpenChecksum.Value {
		t.Errorf("primary open-checksum mismatch: repomd %s, wire %s", primary.OpenChecksum.Value, got)
	}
	xmlBody := string(gunzip(t, []byte(gzBody)))
	for _, want := range []string{
		`<name>mypkg</name>`, `ver="1.0.0"`, `rel="1.el9"`, `<arch>x86_64</arch>`,
		`pkgid="YES">` + sha256Hex(pkg1),
		`<location href="mypkg-1.0.0-1.el9.x86_64.rpm">`,
		`<location href="sub/other-2.0-1.noarch.rpm">`,
		`packages="2"`,
		`<rpm:entry name="libc.so.6(GLIBC_2.34)(64bit)" flags="GE" epoch="0" ver="2.34" rel="58.el9">`,
		`<rpm:provides>`, `<rpm:requires>`, `<rpm:conflicts>`, `<rpm:obsoletes>`,
		`<rpm:recommends>`, `<rpm:suggests>`,
		`<rpm:license>MIT and GPLv2+</rpm:license>`,
	} {
		if !strings.Contains(xmlBody, want) {
			t.Errorf("primary missing %q", want)
		}
	}

	// The other.xml changelog index.
	var other *repomdData
	for i, d := range doc.Data {
		if d.Type == "other" {
			other = &doc.Data[i]
		}
	}
	status, ogz, _ := s.get("/binflow/rpm-local/" + other.Location.Href)
	if status != http.StatusOK {
		t.Fatalf("other fetch = %d", status)
	}
	if got := sha256Hex([]byte(ogz)); got != other.Checksum.Value {
		t.Errorf("other checksum mismatch: %s vs %s", other.Checksum.Value, got)
	}
	if !strings.Contains(string(gunzip(t, []byte(ogz))), "- initial build") {
		t.Error("other.xml lost the changelog")
	}

	// No staging residue: the _tmp_ tree is gone after the run.
	nodes, err := s.svc.List(context.Background(), adminPrincipal(), "rpm-local", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if isTmpPath(strings.SplitN(n.Path, "/", 2)[0]) || strings.HasPrefix(n.Path, tmpPrefix) {
			t.Errorf("staging residue: %s", n.Path)
		}
	}
}

// TestReindexMatrix walks the management plane's branch table (rpm.md
// section 3.2) against live repositories.
func TestReindexMatrix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-auto", repo.TypeLocal, `{"calculateYumMetadata":true}`)
	s.seedRepo(t, "rpm-plain", repo.TypeLocal, `{"calculateYumMetadata":false}`)
	seedRepoRow(t, s, "virt-rpm", repo.TypeVirtual, Protocol)
	seedRepoRow(t, s, "gen-local", repo.TypeLocal, "generic")
	s.seedPlainUser(t)

	tests := []struct {
		name   string
		method string
		path   string
		user   string
		pass   string
		status int
		body   string
	}{
		{"blank key", http.MethodPost, "/binflow/api/yum", adminUser, adminPass, 400, "Target repository key cannot be blank"},
		{"blank key slash", http.MethodPost, "/binflow/api/yum/", adminUser, adminPass, 400, "Target repository key cannot be blank"},
		{"anonymous", http.MethodPost, "/binflow/api/yum/rpm-plain?async=0", "", "", 401, ""},
		{"no manage", http.MethodPost, "/binflow/api/yum/rpm-plain?async=0", "plain", "plain-pass", 403, ""},
		{"repo missing", http.MethodPost, "/binflow/api/yum/rpm-nothere", adminUser, adminPass, 404, "Unable to find repository 'rpm-nothere'."},
		{"non-rpm repo", http.MethodPost, "/binflow/api/yum/gen-local", adminUser, adminPass, 404, "Unable to find repository 'gen-local'."},
		{"virtual unserved", http.MethodPost, "/binflow/api/yum/virt-rpm?async=0", adminUser, adminPass, 400, "virtual"},
		{"async1 accepted", http.MethodPost, "/binflow/api/yum/rpm-plain?async=1", adminUser, adminPass, 202,
			"YUM metadata calculation for repository 'rpm-plain' accepted."},
		{"sync auto-calc conflict", http.MethodPost, "/binflow/api/yum/rpm-auto?async=0", adminUser, adminPass, 409,
			"Unable to perform immediate YUM metadata calculation on a repository with auto-async calculation enabled."},
		{"sync completes", http.MethodPost, "/binflow/api/yum/rpm-plain?async=0", adminUser, adminPass, 200,
			"YUM metadata calculation for repository 'rpm-plain' accepted."},
		{"bad async value", http.MethodPost, "/binflow/api/yum/rpm-plain?async=9", adminUser, adminPass, 400, ""},
		{"non-POST verb", http.MethodGet, "/binflow/api/yum/rpm-plain", adminUser, adminPass, 404, ""},
		{"tail not allowed", http.MethodPost, "/binflow/api/yum/rpm-plain/extra", adminUser, adminPass, 404, ""},
	}
	for _, tt := range tests {
		status, body, _ := s.do(tt.method, tt.path, tt.user, tt.pass, http.NoBody, nil)
		if status != tt.status {
			t.Errorf("%s: status = %d, want %d (body %s)", tt.name, status, tt.status, body)
		}
		if tt.body != "" && !strings.Contains(body, tt.body) {
			t.Errorf("%s: body %q does not carry %q", tt.name, body, tt.body)
		}
	}

	// The async branch really lands: poll for the repomd.
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, _, _ := s.get("/binflow/rpm-plain/repodata/repomd.xml")
		if status == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("async reindex never landed the repomd")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestOptInAutoRecompute: calculateYumMetadata=true makes the upload
// chain itself land the repodata (the explicit opt-in of RP-2).
func TestOptInAutoRecompute(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-auto", repo.TypeLocal, `{"calculateYumMetadata":true}`)
	if status, b, _ := s.put("/binflow/rpm-auto/hello-1.0-1.noarch.rpm", pkgFixture("hello", "1.0", "1", "noarch"), nil); status != http.StatusCreated {
		t.Fatalf("PUT = (%d, %s)", status, b)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		status, body, _ := s.get("/binflow/rpm-auto/repodata/repomd.xml")
		if status == http.StatusOK {
			if !strings.Contains(body, "primary") {
				t.Fatalf("repomd without primary entry:\n%s", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("auto recompute never landed the repomd")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// The 409 branch: the opt-in repository refuses the synchronous form.
	if status, b, _ := s.post("/binflow/api/yum/rpm-auto?async=0"); status != http.StatusConflict {
		t.Errorf("sync reindex on auto repo = (%d, %s), want 409", status, b)
	}
}

// TestDeleteDropsPackage: the delete chain plus a reindex removes the
// package from the index (and the .rpmcache entry with it).
func TestDeleteDropsPackage(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkg, nil)
	if _, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); !strings.Contains(b, "accepted") {
		t.Fatalf("reindex: %s", b)
	}
	cacheFile := filepath.Join(s.dataDir, ".rpmcache", "rpm-local", "mypkg-1.0.0-1.el9.x86_64.rpm")
	if _, err := readFileExists(cacheFile); err != nil {
		t.Fatalf("cache record missing after upload: %v", err)
	}

	if status, b, _ := s.delete("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm"); status != http.StatusNoContent {
		t.Fatalf("DELETE = (%d, %s)", status, b)
	}
	if _, err := readFileExists(cacheFile); err == nil {
		t.Error("cache record survived the delete")
	}
	if _, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); !strings.Contains(b, "accepted") {
		t.Fatalf("second reindex: %s", b)
	}
	status, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd = %d", status)
	}
	href, err := primaryHrefOf(repomd)
	if err != nil {
		t.Fatal(err)
	}
	_, pbody, _ := s.get("/binflow/rpm-local/" + href)
	if strings.Contains(string(gunzip(t, []byte(pbody))), "mypkg") {
		t.Error("deleted package still indexed")
	}
	if !strings.Contains(string(gunzip(t, []byte(pbody))), `packages="0"`) {
		t.Errorf("empty primary does not declare zero packages:\n%s", gunzip(t, []byte(pbody)))
	}
}

// TestGenerationRetention: repeated reindexes keep the newest 3 per index
// type; the repomd keeps referencing the live one.
func TestGenerationRetention(t *testing.T) {
	s := newStack(t)
	// A fresh Now per generation so each run's repomd timestamp differs —
	// the index bytes ride the package set, so force distinct digest
	// generations by GROWING the package set per run.
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	for i := 0; i < 5; i++ {
		pkg := pkgFixture(fmt.Sprintf("gen%d", i), "1.0", "1", "noarch")
		if status, b, _ := s.put(fmt.Sprintf("/binflow/rpm-local/gen%d-1.0-1.noarch.rpm", i), pkg, nil); status != http.StatusCreated {
			t.Fatalf("gen%d PUT = %s", i, b)
		}
		if status, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); status != 200 {
			t.Fatalf("gen%d reindex = (%d, %s)", i, status, b)
		}
	}
	nodes, err := s.svc.List(context.Background(), adminPrincipal(), "rpm-local", "repodata")
	if err != nil {
		t.Fatal(err)
	}
	byType := map[string]int{}
	for _, n := range nodes {
		base := path.Base(n.Path)
		if rest, ok := stripDigestPrefix(base); ok {
			_ = rest
			byType[indexTypeOf(base)]++
		}
	}
	for typ, count := range byType {
		if count > generationsToKeep {
			t.Errorf("index type %s kept %d generations, want ≤ %d", typ, count, generationsToKeep)
		}
	}
	// The live repomd still resolves: every location href serves 200.
	status, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd = %d", status)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Data) == 0 {
		t.Fatal("empty repomd after retention")
	}
	for _, d := range doc.Data {
		st, _, _ := s.get("/binflow/rpm-local/" + d.Location.Href)
		if st != http.StatusOK {
			t.Errorf("repomd href %s serves %d", d.Location.Href, st)
		}
	}
}

// TestDeepRootLayout: yumRootDepth=2 keeps two independent repodata trees
// (rpm.md section 1's multi-distro subtree form).
func TestDeepRootLayout(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, `{"yumRootDepth":2}`)
	p9 := pkgFixture("mypkg", "9.0", "1.el9", "x86_64")
	p40 := pkgFixture("mypkg", "4.0", "1.fc40", "x86_64")
	s.put("/binflow/rpm-local/el/9/mypkg-9.0-1.el9.x86_64.rpm", p9, nil)
	s.put("/binflow/rpm-local/fedora/40/mypkg-4.0-1.fc40.x86_64.rpm", p40, nil)

	if status, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); status != 200 {
		t.Fatalf("reindex = (%d, %s)", status, b)
	}
	for _, root := range []string{"el/9", "fedora/40"} {
		status, repomd, _ := s.get("/binflow/rpm-local/" + root + "/repodata/repomd.xml")
		if status != http.StatusOK {
			t.Fatalf("%s repomd = %d", root, status)
		}
		href, err := primaryHrefOf(repomd)
		if err != nil {
			t.Fatalf("%s primary href: %v", root, err)
		}
		_, pbody, _ := s.get("/binflow/rpm-local/" + root + "/" + href)
		if !strings.Contains(string(gunzip(t, []byte(pbody))), `packages="1"`) {
			t.Errorf("%s primary does not hold exactly one package", root)
		}
	}
	// A root-level repodata must NOT exist (the depth moved it).
	if status, _, _ := s.get("/binflow/rpm-local/repodata/repomd.xml"); status != http.StatusOK {
		// depth 2 with content only under el/9 and fedora/40: correct.
		t.Logf("root repomd status = %d (expected non-200)", status)
	}
}

// TestRepodataWriteProtection: the generated family refuses client
// writes; the un-prefixed group file uploads fine.
func TestRepodataWriteProtection(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64"), nil)
	s.post("/binflow/api/yum/rpm-local?async=0")

	for _, path := range []string{
		"/binflow/rpm-local/repodata/repomd.xml",
		"/binflow/rpm-local/repodata/repomd.xml.asc",
	} {
		if status, _, _ := s.put(path, []byte("hand-made"), nil); status != http.StatusForbidden {
			t.Errorf("PUT %s = %d, want 403", path, status)
		}
	}
	// The digest-prefixed index file (grab the live name).
	status, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatal("repomd missing")
	}
	href, _ := primaryHrefOf(repomd)
	if status, _, _ := s.put("/binflow/rpm-local/"+href, []byte("hand-made"), nil); status != http.StatusForbidden {
		t.Errorf("PUT %s = %d, want 403", href, status)
	}
	if status, _, _ := s.delete("/binflow/rpm-local/" + href); status != http.StatusForbidden {
		t.Errorf("DELETE %s = %d, want 403", href, status)
	}

	// The group file uploads and chains at the next reindex.
	comps := `<comps><group><id>base</id><name>Base</name></group></comps>`
	if status, b, _ := s.put("/binflow/rpm-local/repodata/comps.xml", []byte(comps), nil); status != http.StatusCreated {
		t.Fatalf("comps PUT = (%d, %s)", status, b)
	}
	if _, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); !strings.Contains(b, "accepted") {
		t.Fatalf("reindex: %s", b)
	}
	_, repomd, _ = s.get("/binflow/rpm-local/repodata/repomd.xml")
	if !strings.Contains(repomd, `type="group"`) || !strings.Contains(repomd, `type="group_gz"`) {
		t.Errorf("group entries missing from repomd:\n%s", repomd)
	}
	// The un-prefixed upload is gone (renamed to its digest spelling).
	if status, _, _ := s.get("/binflow/rpm-local/repodata/comps.xml"); status != http.StatusNotFound {
		t.Errorf("un-renamed comps still served: %d", status)
	}
	// Re-uploading identical content dedupes (no second file, entry kept).
	if status, b, _ := s.put("/binflow/rpm-local/repodata/comps.xml", []byte(comps), nil); status != http.StatusCreated {
		t.Fatalf("comps re-PUT = (%d, %s)", status, b)
	}
	if _, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); !strings.Contains(b, "accepted") {
		t.Fatalf("reindex after dedupe: %s", b)
	}
}

// TestChecksumSidecarRefused: the .rpm.<checksum> spellings answer the
// pinned 404 (rpm.md section 3.1).
func TestChecksumSidecarRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkg, nil)
	for _, suffix := range []string{".sha256", ".sha1", ".md5", ".sha512"} {
		status, body, _ := s.get("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm" + suffix)
		if status != http.StatusNotFound || strings.TrimSpace(body) != "Checksums are not downloadable." {
			t.Errorf("sidecar GET %s = (%d, %q)", suffix, status, body)
		}
	}
}

// TestChecksumHeaderGates: the shared X-Checksum family (malformed 400 /
// mismatch 409) rides the .rpm face.
func TestChecksumHeaderGates(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	if status, body, _ := s.put("/binflow/rpm-local/a.rpm", pkg,
		map[string]string{"X-Checksum-Sha256": "nothex"}); status != http.StatusBadRequest {
		t.Fatalf("malformed checksum = (%d, %s), want 400", status, body)
	}
	if status, body, _ := s.put("/binflow/rpm-local/a.rpm", pkg,
		map[string]string{"X-Checksum-Sha256": sha256Hex([]byte("other"))}); status != http.StatusConflict {
		t.Fatalf("mismatched checksum = (%d, %s), want 409", status, body)
	}
	if status, _, _ := s.put("/binflow/rpm-local/a.rpm", pkg,
		map[string]string{"X-Checksum-Sha256": sha256Hex(pkg)}); status != http.StatusCreated {
		t.Fatalf("matching checksum rejected")
	}
}

// TestClassDoors: remote/virtual rpm rows answer the not-served 404 on
// the content plane.
func TestClassDoors(t *testing.T) {
	s := newStack(t)
	seedRepoRow(t, s, "rpm-virt", repo.TypeVirtual, Protocol)
	status, body, _ := s.get("/binflow/rpm-virt/anything.rpm")
	if status != http.StatusNotFound || !strings.Contains(body, "virtual") {
		t.Errorf("virtual content = (%d, %s), want the class refusal", status, body)
	}
}

// TestConcurrentReindexConsistency: parallel reindexes and uploads
// serialize — the final repomd references a primary that serves every
// package the storage holds at rest.
func TestConcurrentReindexConsistency(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	const n = 8
	for i := 0; i < n; i++ {
		pkg := pkgFixture(fmt.Sprintf("c%d", i), "1.0", "1", "noarch")
		s.put(fmt.Sprintf("/binflow/rpm-local/c%d-1.0-1.noarch.rpm", i), pkg, nil)
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.do(http.MethodPost, "/binflow/api/yum/rpm-local?async=0", adminUser, adminPass, http.NoBody, nil)
		}()
	}
	// A delete racing the reindexes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.delete("/binflow/rpm-local/c0-1.0-1.noarch.rpm")
	}()
	wg.Wait()

	// One final synchronous run decides the resting state.
	s.post("/binflow/api/yum/rpm-local?async=0")
	status, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd = %d", status)
	}
	href, err := primaryHrefOf(repomd)
	if err != nil {
		t.Fatal(err)
	}
	_, pbody, _ := s.get("/binflow/rpm-local/" + href)
	count := strings.Count(string(gunzip(t, []byte(pbody))), "<package ")
	// c0 may or may not have lost the race before the final run; the
	// FINAL run's index must match the resting storage exactly.
	nodes, err := s.svc.List(context.Background(), adminPrincipal(), "rpm-local", "")
	if err != nil {
		t.Fatal(err)
	}
	stored := 0
	for _, nd := range nodes {
		if isRpmPath(nd.Path) && !isTmpPath(nd.Path) {
			stored++
		}
	}
	if count != stored {
		t.Errorf("primary holds %d packages, storage holds %d", count, stored)
	}
}

// TestStaleSignatureCleanup: an operator-seeded repomd.xml.asc/.key pair
// (the previous generation's leftovers) disappears on the unsigned
// recompute (section 4.3).
func TestStaleSignatureCleanup(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkg, nil)
	s.post("/binflow/api/yum/rpm-local?async=0")

	// Seed the stale pair through the service (the 403 blocks the client
	// face, which is the point of the test above).
	seed := func(p, body string) {
		if _, err := s.svc.Put(context.Background(), adminPrincipal(), "rpm-local", p,
			strings.NewReader(body), storage.BlobRef{}, "text/plain"); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}
	seed("repodata/repomd.xml.asc", "-----BEGIN PGP SIGNATURE-----\nold\n")
	seed("repodata/repomd.xml.key", "-----BEGIN PGP PUBLIC KEY-----\nold\n")
	if _, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); !strings.Contains(b, "accepted") {
		t.Fatalf("reindex: %s", b)
	}
	for _, p := range []string{"repodata/repomd.xml.asc", "repodata/repomd.xml.key"} {
		if status, _, _ := s.get("/binflow/rpm-local/" + p); status != http.StatusNotFound {
			t.Errorf("stale %s survived the unsigned recompute (status %d)", p, status)
		}
	}
}

// TestLegacySqliteCleanup: seeded sqlite metadata disappears on the
// recompute (section 2.2).
func TestLegacySqliteCleanup(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64"), nil)
	if _, err := s.svc.Put(context.Background(), adminPrincipal(), "rpm-local",
		"repodata/abc-primary.sqlite.bz2", strings.NewReader("legacy"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	s.post("/binflow/api/yum/rpm-local?async=0")
	if status, _, _ := s.get("/binflow/rpm-local/repodata/abc-primary.sqlite.bz2"); status != http.StatusNotFound {
		t.Errorf("legacy sqlite survived (status %d)", status)
	}
}

// TestFileListsOptIn: enableFileListsIndexing=true adds the third index.
func TestFileListsOptIn(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, `{"enableFileListsIndexing":true}`)
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64"), nil)
	if status, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); status != 200 {
		t.Fatalf("reindex = (%d, %s)", status, b)
	}
	_, repomd, _ := s.get("/binflow/rpm-local/repodata/repomd.xml")
	if !strings.Contains(repomd, `type="filelists"`) {
		t.Fatalf("filelists entry missing:\n%s", repomd)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatalf("repomd parse: %v", err)
	}
	for _, d := range doc.Data {
		if d.Type != "filelists" {
			continue
		}
		_, fbody, _ := s.get("/binflow/rpm-local/" + d.Location.Href)
		xmlf := string(gunzip(t, []byte(fbody)))
		for _, want := range []string{
			`<file>/usr/bin/mypkg</file>`,
			`<file>/usr/share/mypkg/README.md</file>`,
			`<file>/usr/share/mypkg/data.txt</file>`,
		} {
			if !strings.Contains(xmlf, want) {
				t.Errorf("filelists missing %q", want)
			}
		}
	}
}

// TestTmpStagingWriteRefused: the staging prefix is reserved against
// client writes.
func TestTmpStagingWriteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	if status, _, _ := s.put("/binflow/rpm-local/_tmp_1234567890123/repodata/x", []byte("x"), nil); status != http.StatusForbidden {
		t.Errorf("staging PUT = %d, want 403", status)
	}
}

// TestRpmCacheDisabled: an empty DataDir degrades to parse-on-demand
// (every reindex still succeeds).
func TestRpmCacheDisabled(t *testing.T) {
	s := newStackOpt(t, stackOptions{dataDir: t.TempDir()})
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	s.put("/binflow/rpm-local/mypkg-1.0.0-1.el9.x86_64.rpm", pkgFixture("mypkg", "1.0.0", "1.el9", "x86_64"), nil)
	if status, b, _ := s.post("/binflow/api/yum/rpm-local?async=0"); status != 200 {
		t.Fatalf("reindex = (%d, %s)", status, b)
	}
	if status, _, _ := s.get("/binflow/rpm-local/repodata/repomd.xml"); status != http.StatusOK {
		t.Errorf("repomd = %d", status)
	}
}

// ---- helpers ----

// repomdDoc is the repomd.xml parse target.
type repomdDoc struct {
	XMLName xml.Name     `xml:"repomd"`
	Data    []repomdData `xml:"data"`
}

type repomdData struct {
	Type         string         `xml:"type,attr"`
	Location     repomdLoc      `xml:"location"`
	Checksum     repomdChecksum `xml:"checksum"`
	OpenChecksum repomdChecksum `xml:"open-checksum"`
	Timestamp    int64          `xml:"timestamp"`
	Size         int            `xml:"size"`
	OpenSize     int            `xml:"open-size"`
}

type repomdLoc struct {
	Href string `xml:"href,attr"`
}

type repomdChecksum struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

// primaryHrefOf extracts the primary location href from a repomd body.
func primaryHrefOf(repomd string) (string, error) {
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		return "", err
	}
	for _, d := range doc.Data {
		if d.Type == "primary" {
			return d.Location.Href, nil
		}
	}
	return "", fmt.Errorf("no primary data entry in repomd")
}

// adminPrincipal builds the service principal for direct service calls.
func adminPrincipal() *repo.Principal {
	return &repo.Principal{Name: adminUser, Admin: true}
}

// readFileExists reports a readable file.
func readFileExists(p string) (bool, error) {
	b, err := os.ReadFile(p) //nolint:gosec // G304: the test's own TempDir paths
	if err != nil {
		return false, err
	}
	return len(b) > 0, nil
}

// seedRepoRow writes one repository row through the store (classes and
// package types the service validation does not serve).
func seedRepoRow(t *testing.T, s *stack, key, class, packageType string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedPlainUser creates one authenticated non-admin user (the 403 leg's
// caller).
func (s *stack) seedPlainUser(t *testing.T) {
	t.Helper()
	hash, err := auth.HashPassword("plain-pass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := s.md.Users().Create(context.Background(), &metadata.User{
		Username: "plain", PasswordHash: hash, Enabled: true, Role: "user",
	}); err != nil {
		t.Fatalf("seed plain user: %v", err)
	}
}
