package rpm

// The VIRTUAL repository's full-stack tests (rpm.md section 6.3 / S11,
// RP-3's tightened boundary): the aggregated repodata across two local
// members, the name+arch priority dedup, the modules/group merge, the
// single-member passthrough, the write routing, and the TTL cache's
// digest-coherence window.

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// virtualFixture is one stack with two reindexed local members and a
// virtual over them.
type virtualFixture struct {
	*stack
	vkey string
}

// newVirtualFixture assembles the fixture: m1 carries alpha+shared, m2
// carries beta+shared (the shared name+arch pair exercises the dedup).
func newVirtualFixture(t *testing.T, opts stackOptions) *virtualFixture {
	t.Helper()
	s := newStackOpt(t, opts)
	s.seedRepo(t, "m1", repo.TypeLocal, "{}")
	s.seedRepo(t, "m2", repo.TypeLocal, "{}")
	s.seedVirtualRepo(t, "rpm-v", "", []string{"m1", "m2"}, nil)
	return &virtualFixture{stack: s, vkey: "rpm-v"}
}

// uploadAndIndex stores one package fixture and reindexes its repository.
func (f *virtualFixture) uploadAndIndex(t *testing.T, repoKey, name, version string) []byte {
	t.Helper()
	pkg := pkgFixture(name, version, "1", "noarch")
	rel := fmt.Sprintf("%s-%s-1.noarch.rpm", name, version)
	if status, body, _ := f.put("/binflow/"+repoKey+"/"+rel, pkg, nil); status != http.StatusCreated {
		t.Fatalf("%s PUT %s = (%d, %s)", repoKey, rel, status, body)
	}
	if status, body, _ := f.post("/binflow/api/yum/" + repoKey + "?async=0"); status != http.StatusOK {
		t.Fatalf("%s reindex = (%d, %s)", repoKey, status, body)
	}
	return pkg
}

// virtualRepomd fetches and parses the virtual's aggregated repomd.
func (f *virtualFixture) virtualRepomd(t *testing.T) repomdDoc {
	t.Helper()
	status, body, _ := f.get("/binflow/" + f.vkey + "/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("virtual repomd = (%d, %s)", status, body)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("virtual repomd parse: %v", err)
	}
	return doc
}

// dataHrefOf finds one data entry's location href.
func dataHrefOf(t *testing.T, doc repomdDoc, typ string) string {
	t.Helper()
	for _, d := range doc.Data {
		if d.Type == typ {
			return d.Location.Href
		}
	}
	t.Fatalf("repomd carries no %s entry (has %d entries)", typ, len(doc.Data))
	return ""
}

// TestVirtualAggregateMergesMembers: two members' repodata merge — the
// aggregate repomd's primary carries BOTH members' packages, every
// checksum field reconciles against the bytes the aggregate serves, and
// the .rpm downloads resolve first-hit through the virtual.
func TestVirtualAggregateMergesMembers(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	alpha := f.uploadAndIndex(t, "m1", "alpha", "1.0")
	beta := f.uploadAndIndex(t, "m2", "beta", "2.0")

	doc := f.virtualRepomd(t)
	primary := dataHrefOf(t, doc, "primary")
	other := dataHrefOf(t, doc, "other")

	for typ, href := range map[string]string{"primary": primary, "other": other} {
		status, gz, _ := f.get("/binflow/" + f.vkey + "/" + href)
		if status != http.StatusOK {
			t.Fatalf("%s %s = %d", typ, href, status)
		}
		var entry repomdData
		for _, d := range doc.Data {
			if d.Type == typ {
				entry = d
			}
		}
		if got := sha256Hex([]byte(gz)); got != entry.Checksum.Value {
			t.Errorf("%s checksum: repomd %s ≠ wire %s", typ, entry.Checksum.Value, got)
		}
		xmlBody := string(gunzip(t, []byte(gz)))
		if got := sha256Hex(gunzip(t, []byte(gz))); got != entry.OpenChecksum.Value {
			t.Errorf("%s open-checksum disagrees", typ)
		}
		if typ == "primary" {
			for _, want := range []string{"<name>alpha</name>", "<name>beta</name>"} {
				if !strings.Contains(xmlBody, want) {
					t.Errorf("merged primary missing %q", want)
				}
			}
			if !strings.Contains(xmlBody, `packages="2"`) {
				t.Errorf("merged primary packages attr wrong:\n%s", xmlBody[:200])
			}
			// Each package's location href resolves through the virtual.
			for _, name := range []string{"alpha-1.0-1.noarch.rpm", "beta-2.0-1.noarch.rpm"} {
				if !strings.Contains(xmlBody, name) {
					t.Errorf("merged primary missing location %q", name)
				}
				st, body, _ := f.get("/binflow/" + f.vkey + "/" + name)
				if st != http.StatusOK {
					t.Fatalf("virtual download %s = %d", name, st)
				}
				_ = body
			}
		}
	}
	_ = alpha
	_ = beta
}

// TestVirtualDedupPriority: S11's rule — non-priority members with the
// same name+arch COEXIST (two entries); a PRIORITY member's package drops
// every non-priority member's same name+arch entry.
func TestVirtualDedupPriority(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "shared", "1.0")
	f.uploadAndIndex(t, "m2", "shared", "2.0")

	count := func() int {
		doc := f.virtualRepomd(t)
		status, gz, _ := f.get("/binflow/" + f.vkey + "/" + dataHrefOf(t, doc, "primary"))
		if status != http.StatusOK {
			t.Fatalf("primary = %d", status)
		}
		return strings.Count(string(gunzip(t, []byte(gz))), "<name>shared</name>")
	}

	if got := count(); got != 2 {
		t.Fatalf("no-priority shared entries = %d, want 2 (non-priority members never dedup among themselves)", got)
	}

	// Mark m2 priority: m1's shared drops, m2's keeps.
	s := f.stack
	row, err := s.md.Repos().Get(t.Context(), "m2")
	if err != nil {
		t.Fatal(err)
	}
	row.Config = `{"priorityResolution":true}`
	if err := s.md.Repos().Update(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("priority shared entries = %d, want 1 (the non-priority member's same name+arch drops)", got)
	}
}

// TestVirtualSingleMemberPassthrough: fewer than two members with repomd
// → NO aggregate; the read passthroughs to the first-hit member, byte
// identical to the member's own repomd.
func TestVirtualSingleMemberPassthrough(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	// m2 stays empty: only one member carries repodata.
	s := f.stack
	s.seedVirtualRepo(t, "rpm-solo", "", []string{"m1", "m2"}, nil)

	vStatus, vBody, _ := s.get("/binflow/rpm-solo/repodata/repomd.xml")
	mStatus, mBody, _ := s.get("/binflow/m1/repodata/repomd.xml")
	if vStatus != http.StatusOK || vStatus != mStatus || vBody != mBody {
		t.Fatalf("single-member virtual repomd passthrough broken (%d vs %d, %d bytes vs %d)",
			vStatus, mStatus, len(vBody), len(mBody))
	}
}

// TestVirtualNoRepodataAnywhere: zero members with repomd → the honest
// 404 (nothing to aggregate, nothing to passthrough to).
func TestVirtualNoRepodataAnywhere(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	if status, body, _ := f.get("/binflow/" + f.vkey + "/repodata/repomd.xml"); status != http.StatusNotFound {
		t.Fatalf("empty virtual repomd = (%d, %s), want 404", status, body)
	}
}

// TestVirtualModulesMerge: the members' modules.yaml uploads merge as one
// concatenated YAML stream; the P2 chain (the local recompute's modules
// pass-through) feeds both sides.
func TestVirtualModulesMerge(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")

	m1 := "document: m1-module\ndata: one\n"
	m2 := "document: m2-module\ndata: two\n"
	for repoKey, doc := range map[string]string{"m1": m1, "m2": m2} {
		if status, body, _ := f.put("/binflow/"+repoKey+"/repodata/modules.yaml", []byte(doc), nil); status != http.StatusCreated {
			t.Fatalf("%s modules PUT = (%d, %s)", repoKey, status, body)
		}
		if status, body, _ := f.post("/binflow/api/yum/" + repoKey + "?async=0"); status != http.StatusOK {
			t.Fatalf("%s reindex = (%d, %s)", repoKey, status, body)
		}
	}

	agg := f.virtualRepomd(t)
	href := dataHrefOf(t, agg, "modules")
	if !strings.Contains(href, "-modules.yaml.gz") {
		t.Fatalf("modules href %q not the digest-prefixed spelling", href)
	}
	status, gz, _ := f.get("/binflow/" + f.vkey + "/" + href)
	if status != http.StatusOK {
		t.Fatalf("aggregate modules = %d", status)
	}
	body := string(gunzip(t, []byte(gz)))
	for _, want := range []string{"m1-module", "m2-module", "---"} {
		if !strings.Contains(body, want) {
			t.Errorf("merged modules missing %q:\n%s", want, body)
		}
	}

	// The member-local P2 chain: each member's own repomd carries its own
	// modules entry (the upload gzips under its digest name).
	for repoKey, want := range map[string]string{"m1": "m1-module", "m2": "m2-module"} {
		status, mRepomd, _ := f.get("/binflow/" + repoKey + "/repodata/repomd.xml")
		if status != http.StatusOK {
			t.Fatalf("%s repomd = %d", repoKey, status)
		}
		var doc repomdDoc
		if err := xml.Unmarshal([]byte(mRepomd), &doc); err != nil {
			t.Fatal(err)
		}
		href := dataHrefOf(t, doc, "modules")
		_, mgz, _ := f.get("/binflow/" + repoKey + "/" + href)
		if !strings.Contains(string(gunzip(t, []byte(mgz))), want) {
			t.Errorf("%s's own modules entry lost %q", repoKey, want)
		}
	}
}

// TestVirtualModulesPriorityNotCollected: S11's literal collection rule —
// modules come from the NON-PRIORITY members only; the priority member's
// modules do not contribute.
func TestVirtualModulesPriorityNotCollected(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")
	m1 := "document: m1-module\n"
	m2 := "document: m2-module\n"
	f.put("/binflow/m1/repodata/modules.yaml", []byte(m1), nil)
	f.put("/binflow/m2/repodata/modules.yaml", []byte(m2), nil)
	f.post("/binflow/api/yum/m1?async=0")
	f.post("/binflow/api/yum/m2?async=0")

	// m1 priority: only m2's modules survive the aggregate.
	s := f.stack
	row, err := s.md.Repos().Get(t.Context(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	row.Config = `{"priorityResolution":true}`
	if err := s.md.Repos().Update(t.Context(), row); err != nil {
		t.Fatal(err)
	}

	agg := f.virtualRepomd(t)
	_, gz, _ := s.get("/binflow/" + f.vkey + "/" + dataHrefOf(t, agg, "modules"))
	body := string(gunzip(t, []byte(gz)))
	if strings.Contains(body, "m1-module") {
		t.Errorf("priority member's modules contributed (S11 says non-priority only):\n%s", body)
	}
	if !strings.Contains(body, "m2-module") {
		t.Errorf("non-priority member's modules missing:\n%s", body)
	}
}

// TestVirtualGroupMerge: one member's comps group document merges into
// the aggregate as the group + group_gz pair under the member's spelling.
func TestVirtualGroupMerge(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")
	comps := `<comps><group><id>base</id><name>Base</name></group></comps>`
	if status, body, _ := f.put("/binflow/m1/repodata/comps.xml", []byte(comps), nil); status != http.StatusCreated {
		t.Fatalf("comps PUT = (%d, %s)", status, body)
	}
	f.post("/binflow/api/yum/m1?async=0")

	agg := f.virtualRepomd(t)
	groupHref := dataHrefOf(t, agg, "group")
	if !strings.Contains(groupHref, "-comps.xml") {
		t.Fatalf("aggregate group href %q lost the member's spelling", groupHref)
	}
	if _, ok := func() (string, bool) {
		for _, d := range agg.Data {
			if d.Type == "group_gz" {
				return d.Location.Href, true
			}
		}
		return "", false
	}(); !ok {
		t.Fatal("aggregate carries no group_gz entry")
	}
	status, body, _ := f.get("/binflow/" + f.vkey + "/" + groupHref)
	if status != http.StatusOK || !strings.Contains(body, "<id>base</id>") {
		t.Fatalf("aggregate group body wrong (%d): %s", status, body)
	}
}

// TestVirtualAggTTLCoherence: the RP-3 cache keeps a client's repomd→
// primary pair coherent (the same digest across the window), and the
// member's next generation (a new upload + reindex) surfaces once the
// short TTL lapses.
func TestVirtualAggTTLCoherence(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir(), aggTTL: 300 * time.Millisecond})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")

	first := f.virtualRepomd(t)
	primary := dataHrefOf(t, first, "primary")
	// Within the window: the same aggregate answers, and its primary
	// serves by the SAME digest name.
	again := f.virtualRepomd(t)
	if dataHrefOf(t, again, "primary") != primary {
		t.Fatal("aggregate primary digest changed inside the TTL window")
	}
	if status, _, _ := f.get("/binflow/" + f.vkey + "/" + primary); status != http.StatusOK {
		t.Fatalf("aggregate primary by digest = %d", status)
	}

	// A new member generation after the TTL: the digest moves.
	f.uploadAndIndex(t, "m1", "alpha", "1.1")
	time.Sleep(400 * time.Millisecond)
	fresh := f.virtualRepomd(t)
	if dataHrefOf(t, fresh, "primary") == primary {
		t.Fatal("aggregate did not refresh after the TTL and a member reindex")
	}
	if !strings.Contains(func() string {
		_, gz, _ := f.get("/binflow/" + f.vkey + "/" + dataHrefOf(t, fresh, "primary"))
		return string(gunzip(t, []byte(gz)))
	}(), "<name>alpha</name>") {
		t.Fatal("refreshed aggregate lost alpha")
	}
}

// TestVirtualWriteRouting: a routed PUT .rpm lands in the deployment
// MEMBER (the recompute and the cache target it there); an un-routed
// virtual answers the C5 405; the generated repodata family stays 403.
func TestVirtualWriteRouting(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")

	// Un-routed: the C5 405.
	pkg := pkgFixture("routed", "1.0", "1", "noarch")
	if status, body, hdr := f.put("/binflow/"+f.vkey+"/routed-1.0-1.noarch.rpm", pkg, nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed virtual PUT = (%d, %s), want the C5 405", status, body)
	} else if hdr.Get("Allow") != "GET" {
		t.Errorf("un-routed 405 Allow = %q", hdr.Get("Allow"))
	}

	// Routed: lands in m1 (the member's own face serves it; the virtual's
	// first-hit does too).
	s := f.stack
	s.seedVirtualRepo(t, "rpm-w", "m1", []string{"m1", "m2"}, nil)
	if status, body, _ := s.put("/binflow/rpm-w/routed-1.0-1.noarch.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("routed virtual PUT = (%d, %s), want 201", status, body)
	}
	if status, _, _ := s.get("/binflow/m1/routed-1.0-1.noarch.rpm"); status != http.StatusOK {
		t.Fatalf("routed PUT did not land in the member (m1 GET = %d)", status)
	}
	if status, _, _ := s.get("/binflow/rpm-w/routed-1.0-1.noarch.rpm"); status != http.StatusOK {
		t.Fatalf("virtual first-hit download = %d", status)
	}

	// The generated family stays server-generated on the virtual.
	doc := f.virtualRepomd(t)
	if status, body, _ := f.put("/binflow/"+f.vkey+"/"+dataHrefOf(t, doc, "primary"), []byte("x"), nil); status != http.StatusForbidden {
		t.Fatalf("virtual index PUT = (%d, %s), want 403", status, body)
	}
	if status, body, _ := f.put("/binflow/"+f.vkey+"/repodata/repomd.xml", []byte("x"), nil); status != http.StatusForbidden {
		t.Fatalf("virtual repomd PUT = (%d, %s), want 403", status, body)
	}
	// DELETE never propagates through the virtual.
	if status, _, _ := f.delete("/binflow/" + f.vkey + "/routed-1.0-1.noarch.rpm"); status != http.StatusMethodNotAllowed {
		t.Fatalf("virtual DELETE = %d, want the never-propagates 405", status)
	}
}

// TestVirtualUnsignedRepomdPair: the aggregate is unsigned (RP-3) — the
// signature pair answers the honest 404, never a member's signature.
func TestVirtualUnsignedRepomdPair(t *testing.T) {
	f := newVirtualFixture(t, stackOptions{dataDir: t.TempDir()})
	f.uploadAndIndex(t, "m1", "alpha", "1.0")
	f.uploadAndIndex(t, "m2", "beta", "2.0")
	for _, rel := range []string{"repodata/repomd.xml.asc", "repodata/repomd.xml.key"} {
		if status, body, _ := f.get("/binflow/" + f.vkey + "/" + rel); status != http.StatusNotFound {
			t.Fatalf("%s = (%d, %s), want the unsigned 404", rel, status, body)
		}
	}
}

// TestVirtualSubtreeAggregate: the deep yumRootDepth form aggregates at
// the subtree root (the members' el/9/repodata merges at el/9).
func TestVirtualSubtreeAggregate(t *testing.T) {
	s := newStackOpt(t, stackOptions{dataDir: t.TempDir()})
	s.seedRepo(t, "m1", repo.TypeLocal, `{"yumRootDepth":2}`)
	s.seedRepo(t, "m2", repo.TypeLocal, `{"yumRootDepth":2}`)
	s.seedVirtualRepo(t, "rpm-d", "", []string{"m1", "m2"}, nil)
	for _, spec := range []struct{ repoKey, name string }{
		{"m1", "alpha"}, {"m2", "beta"},
	} {
		pkg := pkgFixture(spec.name, "1.0", "1", "noarch")
		if status, body, _ := s.put("/binflow/"+spec.repoKey+"/el/9/"+spec.name+"-1.0-1.noarch.rpm", pkg, nil); status != http.StatusCreated {
			t.Fatalf("%s deep PUT = (%d, %s)", spec.repoKey, status, body)
		}
		if status, body, _ := s.post("/binflow/api/yum/" + spec.repoKey + "?async=0"); status != http.StatusOK {
			t.Fatalf("%s deep reindex = (%d, %s)", spec.repoKey, status, body)
		}
	}
	status, repomd, _ := s.get("/binflow/rpm-d/el/9/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("deep virtual repomd = (%d, %s)", status, repomd)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatal(err)
	}
	_, gz, _ := s.get("/binflow/rpm-d/el/9/" + dataHrefOf(t, doc, "primary"))
	primary := string(gunzip(t, []byte(gz)))
	for _, want := range []string{"<name>alpha</name>", "<name>beta</name>"} {
		if !strings.Contains(primary, want) {
			t.Errorf("deep aggregate primary missing %q", want)
		}
	}
}
