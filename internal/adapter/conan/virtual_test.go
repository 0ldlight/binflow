package conan

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// remoteConfigForTest builds the vbad member's config (no SSRF exemption:
// the loopback upstream is refused by the guard, the classified-400
// fixture).
func remoteConfigForTest(url string) metadata.RemoteConfig {
	return metadata.RemoteConfig{RepoKey: "vbad", URL: url, ContentTTLSeconds: 7200, MetadataTTLSeconds: 600}
}

// The virtual-class integration tests (T-312, spec section 7's virtual
// row / S11's merge rules): the revision merge, the files-union, the
// packageId union, the stored-facts search, and the write plane's route
// (copy-prevention included) and refusals.

// virtualFixture is one three-member virtual stack: local member A (the
// write route target), local member B, and a remote member proxying a
// mock upstream.
type virtualFixture struct {
	*stack
	upstream            *httptest.Server
	rf                  ref
	rrevA, rrevB, rrevC string
	prevR               string
	pidR                string
}

// newVirtualFixture seeds:
//
//	member A (local):  revision rA at T1, export file local-a.py
//	member B (local):  revision rB at T2 (later), export file local-b.py,
//	                   plus rA as a DUPLICATE row (the dedupe probe) and a
//	                   second coordinate other/1.0 (the search union probe)
//	remote member R:   revision rC at T3 (the newest), its own listing
//	                   (upstream-r.py) and package rows
func newVirtualFixture(t *testing.T) *virtualFixture {
	t.Helper()
	s := newStack(t)
	f := &virtualFixture{stack: s, rf: ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}}
	rrevA, rrevB := fixtureRev(1), fixtureRev(2)
	f.rrevA, f.rrevB, f.rrevC = rrevA, rrevB, fixtureRev(3)
	f.prevR = fixtureRev(4)
	f.pidR = fixturePID(5)

	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		root := f.rf.coordinateRoot()
		switch {
		case r.URL.Path == "/v2/conans/hello/1.0/myuser/stable/revisions":
			_, _ = fmt.Fprintf(w, `{"reference":"hello/1.0@myuser/stable","revisions":[{"revision":"%s","time":"2026-08-27T12:00:00.000Z"}]}`, f.rrevC)
		case r.URL.Path == "/v2/conans/hello/1.0/myuser/stable/revisions/"+f.rrevC+"/files":
			_, _ = w.Write([]byte(`{"files":{"upstream-r.py":{},"conanmanifest.txt":{}}}`))
		case r.URL.Path == "/v2/conans/hello/1.0/myuser/stable/revisions/"+f.rrevC+"/files/upstream-r.py":
			_, _ = w.Write([]byte("remote-member-body"))
		case r.URL.Path == "/v2/conans/hello/1.0/myuser/stable/revisions/"+f.rrevC+"/search":
			_, _ = fmt.Fprintf(w, `{"%s":{"settings":{"os":"Macos"},"options":{},"requires":{},"recipe_hash":"%s"}}`, f.pidR, f.rrevC)
		case strings.HasPrefix(r.URL.Path, "/"+root+"/"):
			// The remote member's cached tree is invisible upstream — a
			// content probe on the remote member's coordinate is served by
			// the marker + file endpoints above only.
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	s.seedRepo(t, "va", repo.TypeLocal)
	s.seedRepo(t, "vb", repo.TypeLocal)
	s.seedRepo(t, "vrem", repo.TypeRemote)
	s.seedRemoteConfig(t, "vrem", f.upstream.URL)
	s.seedVirtualRepo(t, "vv", []string{"va", "vb", "vrem"}, "va")

	// Member A: rA at T1 with one file.
	f.seedMemberIndex(t, "va", fmt.Sprintf(
		`{"reference":"hello/1.0@myuser/stable","revisions":[{"revision":"%s","time":"2026-08-27T10:00:00.000Z"}]}`, rrevA))
	f.putMemberFile(t, "va", rrevA, "local-a.py", "member-a-body")

	// Member B: rB at T2 (later) + rA duplicated (the dedupe probe), its
	// own file, and a second coordinate for the search union.
	f.seedMemberIndex(t, "vb", fmt.Sprintf(
		`{"reference":"hello/1.0@myuser/stable","revisions":[{"revision":"%s","time":"2026-08-27T11:00:00.000Z"},{"revision":"%s","time":"2026-08-27T10:00:00.000Z"}]}`,
		rrevB, rrevA))
	f.putMemberFile(t, "vb", rrevB, "local-b.py", "member-b-body")
	f.seedMemberIndexAt(t, "vb", ref{name: "other", version: "2.0", user: "_", channel: "_"},
		`{"reference":"other/2.0@_/_","revisions":[{"revision":"`+fixtureRev(6)+`","time":"2026-08-27T09:00:00.000Z"}]}`)
	return f
}

// seedMemberIndex writes one member's recipe index document directly
// through the service seam (deterministic times, the merge order's input).
func (f *virtualFixture) seedMemberIndex(t *testing.T, member, doc string) {
	t.Helper()
	f.seedMemberIndexAt(t, member, f.rf, doc)
}

// seedMemberIndexAt is seedMemberIndex for an explicit coordinate.
func (f *virtualFixture) seedMemberIndexAt(t *testing.T, member string, rf ref, doc string) {
	t.Helper()
	if _, err := f.svc.Put(context.Background(), adminPrincipal(), member, recipeIndex(rf.coordinateRoot()),
		strings.NewReader(doc), blobRefOf([]byte(doc)), "application/json"); err != nil {
		t.Fatalf("seed index %s: %v", member, err)
	}
}

// putMemberFile writes one member's recipe file directly.
func (f *virtualFixture) putMemberFile(t *testing.T, member, rrev, name, body string) {
	t.Helper()
	if _, err := f.svc.Put(context.Background(), adminPrincipal(), member,
		recipeFile(f.rf.coordinateRoot(), rrev, name), strings.NewReader(body),
		blobRefOf([]byte(body)), "text/plain"); err != nil {
		t.Fatalf("seed file %s/%s: %v", member, name, err)
	}
}

// memberIndexDoc reads one member's own index back (the pollution probe).
func (f *virtualFixture) memberIndexDoc(t *testing.T, member string) *recipeIndexDoc {
	t.Helper()
	rc, _, err := f.svc.Get(context.Background(), adminPrincipal(), member, recipeIndex(f.rf.coordinateRoot()))
	if err != nil {
		t.Fatalf("read %s index: %v", member, err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	var doc recipeIndexDoc
	if err := json.NewDecoder(rc).Decode(&doc); err != nil {
		t.Fatalf("decode %s index: %v", member, err)
	}
	return &doc
}

// TestVirtualRevisionMerge: the revisions document merges every member's
// chain (remote included), deduplicates by revision string with the
// first-seen member's row kept, and orders by time descending; latest is
// the merged head.
func TestVirtualRevisionMerge(t *testing.T) {
	f := newVirtualFixture(t)

	code, body, _ := f.get(v2("vv", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK {
		t.Fatalf("virtual revisions = %d (body %s)", code, body)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body %q: %v", body, err)
	}
	// The three distinct revisions across the members, time-descending;
	// rA appears once although two members carry it.
	if len(doc.Revisions) != 3 {
		t.Fatalf("merged revisions = %d (%+v), want 3", len(doc.Revisions), doc.Revisions)
	}
	want := []string{f.rrevC, f.rrevB, f.rrevA}
	for i, r := range want {
		if doc.Revisions[i].Revision != r {
			t.Errorf("merged[%d] = %s, want %s (full chain %+v)", i, doc.Revisions[i].Revision, r, doc.Revisions)
		}
	}

	code, body, _ = f.get(v2("vv", "hello/1.0/myuser/stable/latest"))
	if code != http.StatusOK || !strings.Contains(body, f.rrevC) {
		t.Errorf("virtual latest = (%d, %s), want the remote member's newest revision", code, body)
	}

	// A coordinate no member carries answers the pinned 404.
	code, body, _ = f.get(v2("vv", "nope/1.0/_/_/revisions"))
	if code != http.StatusNotFound || body != msgNoRevisions {
		t.Errorf("missing revisions = (%d, %q), want (404, %q)", code, body, msgNoRevisions)
	}
}

// TestVirtualFilesUnion: the files listing unions the members' sets
// (local facts + the remote member's marker document); the file bodies
// resolve first-found across the members with the resolver's hint.
func TestVirtualFilesUnion(t *testing.T) {
	f := newVirtualFixture(t)

	// Member B's rB listing: the local members' files under one revision.
	listing := "hello/1.0/myuser/stable/revisions/" + f.rrevB + "/files"
	code, body, _ := f.get(v2("vv", listing))
	if code != http.StatusOK {
		t.Fatalf("virtual listing (local revision) = %d (body %s)", code, body)
	}
	var files filesResponse
	if err := json.Unmarshal([]byte(body), &files); err != nil {
		t.Fatalf("listing body %q: %v", body, err)
	}
	if _, ok := files.Files["local-b.py"]; !ok {
		t.Errorf("listing lacks member B's file: %v", files.Files)
	}

	// The remote member's revision: its marker listing unioned with
	// whatever local members hold (none — rC is remote-only).
	listing = "hello/1.0/myuser/stable/revisions/" + f.rrevC + "/files"
	code, body, _ = f.get(v2("vv", listing))
	if code != http.StatusOK {
		t.Fatalf("virtual listing (remote revision) = %d (body %s)", code, body)
	}
	files = filesResponse{}
	if err := json.Unmarshal([]byte(body), &files); err != nil {
		t.Fatalf("listing body %q: %v", body, err)
	}
	if _, ok := files.Files["upstream-r.py"]; !ok {
		t.Errorf("listing lacks the remote member's file: %v", files.Files)
	}

	// The body resolves through the member that holds it.
	code, body, hdr := f.get(v2("vv", "hello/1.0/myuser/stable/revisions/"+f.rrevC+"/files/upstream-r.py"))
	if code != http.StatusOK || body != "remote-member-body" {
		t.Errorf("remote-member file via virtual = (%d, %q)", code, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "vrem" {
		t.Errorf("Resolved-From = %q, want vrem", got)
	}
	code, body, hdr = f.get(v2("vv", "hello/1.0/myuser/stable/revisions/"+f.rrevA+"/files/local-a.py"))
	if code != http.StatusOK || body != "member-a-body" {
		t.Errorf("member A file via virtual = (%d, %q)", code, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "va" {
		t.Errorf("Resolved-From = %q, want va", got)
	}
}

// TestVirtualRefSearchUnion: the packageId rows union across members —
// the local members' fact-derived rows plus the remote member's cached
// document.
func TestVirtualRefSearchUnion(t *testing.T) {
	f := newVirtualFixture(t)

	// Seed one local package row on member B: index + conaninfo.
	pidB := fixturePID(7)
	pidx := fmt.Sprintf(`{"reference":"hello/1.0@myuser/stable#%s:%s","revisions":[{"revision":"%s","time":"2026-08-27T11:30:00.000Z"}]}`,
		f.rrevB, pidB, f.prevR)
	if _, err := f.svc.Put(context.Background(), adminPrincipal(), "vb",
		pkgIndex(f.rf.coordinateRoot(), f.rrevB, pidB), strings.NewReader(pidx),
		blobRefOf([]byte(pidx)), "application/json"); err != nil {
		t.Fatalf("seed pkg index: %v", err)
	}
	info := conaninfoFixture([]string{"os=Linux"}, nil, nil)
	if _, err := f.svc.Put(context.Background(), adminPrincipal(), "vb",
		pkgFile(f.rf.coordinateRoot(), f.rrevB, pidB, f.prevR, "conaninfo.txt"), strings.NewReader(info),
		blobRefOf([]byte(info)), "text/plain"); err != nil {
		t.Fatalf("seed conaninfo: %v", err)
	}

	code, body, _ := f.get(v2("vv", "hello/1.0/myuser/stable/revisions/"+f.rrevB+"/search") + "?q=")
	if code != http.StatusOK {
		t.Fatalf("virtual ref search = %d (body %s)", code, body)
	}
	var rows map[string]*pkgMeta
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("ref search body %q: %v", body, err)
	}
	if meta := rows[pidB]; meta == nil || meta.Settings["os"] != "Linux" || meta.RecipeHash != f.rrevB {
		t.Errorf("local pid row = %+v, want member B's facts", rows[pidB])
	}

	// The remote member's revision carries its own pid rows.
	code, body, _ = f.get(v2("vv", "hello/1.0/myuser/stable/revisions/"+f.rrevC+"/search") + "?q=")
	if code != http.StatusOK {
		t.Fatalf("virtual ref search (remote rev) = %d (body %s)", code, body)
	}
	rows = map[string]*pkgMeta{}
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("ref search body %q: %v", body, err)
	}
	if meta := rows[f.pidR]; meta == nil || meta.Settings["os"] != "Macos" {
		t.Errorf("remote pid row = %+v, want the upstream settings", rows[f.pidR])
	}
}

// TestVirtualSearchUnion: the stored-facts search unions the LOCAL
// members' coordinates (a remote member's catalogue is not enumerable —
// the T-287 posture).
func TestVirtualSearchUnion(t *testing.T) {
	f := newVirtualFixture(t)

	code, body, _ := f.get(v2("vv", "search?q=*"))
	if code != http.StatusOK {
		t.Fatalf("virtual search = %d (body %s)", code, body)
	}
	var res searchResponse
	if err := json.Unmarshal([]byte(body), &res); err != nil {
		t.Fatalf("search body %q: %v", body, err)
	}
	want := map[string]bool{"hello/1.0@myuser/stable": false, "other/2.0@_/_": false}
	for _, r := range res.Results {
		if _, ok := want[r]; ok {
			want[r] = true
		}
	}
	for r, seen := range want {
		if !seen {
			t.Errorf("search results %v lack %q", res.Results, r)
		}
	}
}

// TestVirtualWriteRoute: a PUT through the routed virtual lands in the
// deployment member; the member's own index gains ONLY its revisions plus
// the new one (never the other members' rows — the copy-prevention rule),
// and the merged view immediately reflects the write.
func TestVirtualWriteRoute(t *testing.T) {
	f := newVirtualFixture(t)
	rrevNew := fixtureRev(8)

	code, body, _ := f.put(v2("vv", "hello/1.0/myuser/stable/revisions/"+rrevNew+"/files/conanfile.py"), []byte("via-virtual"), nil)
	if code != http.StatusCreated {
		t.Fatalf("virtual PUT = %d (body %s), want 201", code, body)
	}

	// The deployment member holds the file; the OTHER member does not.
	rc, _, err := f.svc.Get(context.Background(), adminPrincipal(), "va",
		recipeFile(f.rf.coordinateRoot(), rrevNew, "conanfile.py"))
	if err != nil {
		t.Fatalf("routed file missing in va: %v", err)
	}
	_ = rc.Close() //nolint:errcheck // read-only fd
	if _, _, err := f.svc.Get(context.Background(), adminPrincipal(), "vb",
		recipeFile(f.rf.coordinateRoot(), rrevNew, "conanfile.py")); err == nil {
		t.Error("routed file leaked into member vb")
	}

	// The copy-prevention probe: va's own index carries rA + rNew, never
	// vb's rB (appending onto the MERGED chain would have copied it).
	doc := f.memberIndexDoc(t, "va")
	got := map[string]bool{}
	for _, e := range doc.Revisions {
		got[e.Revision] = true
	}
	if !got[f.rrevA] || !got[rrevNew] {
		t.Errorf("va index = %+v, want rA and the new revision", doc.Revisions)
	}
	if got[f.rrevB] {
		t.Errorf("va index carries vb's revision %s — the merged view leaked into the write target: %+v", f.rrevB, doc.Revisions)
	}

	// The merged view immediately reflects the routed write.
	code, body, _ = f.get(v2("vv", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK || !strings.Contains(body, rrevNew) {
		t.Errorf("merged revisions after routed write = (%d, %s)", code, body)
	}

	// Deletes never propagate through the virtual.
	for _, path := range []string{
		"hello/1.0/myuser/stable",
		"hello/1.0/myuser/stable/revisions/" + rrevNew,
	} {
		code, body, _ = f.delete(v2("vv", path))
		if code != http.StatusMethodNotAllowed {
			t.Errorf("virtual DELETE %s = (%d, %q), want 405", path, code, body)
		}
	}
}

// TestVirtualUnroutedWrite: a virtual without a write route answers the
// C5 405 (the service's refusal naming the repository).
func TestVirtualUnroutedWrite(t *testing.T) {
	f := newVirtualFixture(t)
	// Re-point the virtual at no deployment member.
	f.seedVirtualRepo(t, "vu", []string{"va", "vb"}, "")

	code, body, _ := f.put(v2("vu", "hello/1.0/myuser/stable/revisions/"+fixtureRev(9)+"/files/conanfile.py"), []byte("x"), nil)
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed virtual PUT = (%d, %q), want 405", code, body)
	}
	if !strings.Contains(body, "vu") || !strings.Contains(body, "No local repository was configured") {
		t.Errorf("un-routed PUT body = %q, want the C5 wording naming the repository", body)
	}
}

// TestVirtualPackageChain: the packageId revision chain merges across
// members the same way the recipe chain does.
func TestVirtualPackageChain(t *testing.T) {
	f := newVirtualFixture(t)
	pid := fixturePID(10)
	prevA, prevR := fixtureRev(11), fixtureRev(12)

	for _, tc := range []struct{ member, prev, time string }{
		{"va", prevA, "2026-08-27T10:30:00.000Z"},
		{"vb", prevR, "2026-08-27T11:30:00.000Z"},
	} {
		pidx := fmt.Sprintf(`{"reference":"hello/1.0@myuser/stable#%s:%s","revisions":[{"revision":"%s","time":"%s"}]}`,
			f.rrevA, pid, tc.prev, tc.time)
		if _, err := f.svc.Put(context.Background(), adminPrincipal(), tc.member,
			pkgIndex(f.rf.coordinateRoot(), f.rrevA, pid), strings.NewReader(pidx),
			blobRefOf([]byte(pidx)), "application/json"); err != nil {
			t.Fatalf("seed pkg index %s: %v", tc.member, err)
		}
	}

	base := "hello/1.0/myuser/stable/revisions/" + f.rrevA + "/packages/" + pid
	code, body, _ := f.get(v2("vv", base+"/revisions"))
	if code != http.StatusOK {
		t.Fatalf("virtual pkg revisions = %d (body %s)", code, body)
	}
	var doc pkgIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("pkg revisions body %q: %v", body, err)
	}
	if len(doc.Revisions) != 2 || doc.Revisions[0].Revision != prevR || doc.Revisions[1].Revision != prevA {
		t.Errorf("merged pkg chain = %+v, want both revisions time-descending", doc.Revisions)
	}
	if doc.Reference != "hello/1.0@myuser/stable#"+f.rrevA+":"+pid {
		t.Errorf("pkg reference = %q", doc.Reference)
	}

	code, body, _ = f.get(v2("vv", base+"/latest"))
	if code != http.StatusOK || !strings.Contains(body, prevR) {
		t.Errorf("virtual pkg latest = (%d, %s)", code, body)
	}
}

// TestVirtualMemberFailureTolerated: one remote member's CLASSIFIED
// failure (the SSRF refusal — a 400 the engine renders without the
// unfound mark; an upstream 5xx downgrades to the unfound family under
// the default no-hardFail policy and is a plain miss) does not block the
// other members' answers, but an aggregation where NOTHING was collected
// surfaces the remembered failure instead of a plain 404.
func TestVirtualMemberFailureTolerated(t *testing.T) {
	s := newStack(t)
	rf := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}

	// A loopback upstream WITHOUT the SSRF exemption: every fetch answers
	// the engine's classified 400 (RejectionError, never unfound).
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("never reached"))
	}))
	t.Cleanup(up.Close)

	s.seedRepo(t, "va", repo.TypeLocal)
	s.seedRepo(t, "vbad", repo.TypeRemote)
	cfg := remoteConfigForTest(up.URL)
	if err := s.md.Remote().CreateConfig(context.Background(), &cfg); err != nil {
		t.Fatalf("seed remote config: %v", err)
	}
	s.seedVirtualRepo(t, "vv", []string{"vbad", "va"}, "")

	// The local member still answers through the failing remote member.
	doc := `{"reference":"hello/1.0@myuser/stable","revisions":[{"revision":"` + fixtureRev(13) + `","time":"2026-08-27T10:00:00.000Z"}]}`
	if _, err := s.svc.Put(context.Background(), adminPrincipal(), "va", recipeIndex(rf.coordinateRoot()),
		strings.NewReader(doc), blobRefOf([]byte(doc)), "application/json"); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	code, body, _ := s.get(v2("vv", "hello/1.0/myuser/stable/revisions"))
	if code != http.StatusOK || !strings.Contains(body, fixtureRev(13)) {
		t.Errorf("merged revisions with a failing member = (%d, %s), want the local member's chain", code, body)
	}

	// A coordinate NO member carries: the remembered failure surfaces (the
	// npm/pypi rule — never mask a classified fault as a plain 404).
	code, body, _ = s.get(v2("vv", "nope/1.0/_/_/revisions"))
	if code != http.StatusBadRequest || !strings.Contains(body, "refused") {
		t.Errorf("all-member-miss revisions = (%d, %q), want the surfaced classified failure", code, body)
	}
}
