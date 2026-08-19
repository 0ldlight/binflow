package maven

// T-72: the virtual maven-metadata.xml merge matrix (FR-21-AC6/ME-05's
// virtual face). Every case runs the REAL stack — real storage, real
// service with the T-71 resolver and the T-66 engine, the real calculator
// maintaining the members' own documents — through the adapter's GET face.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// virtualFixture is one merge-matrix row's world.
type virtualFixture struct {
	hs *harness
	// upstream serves one remote member's metadata (and counts).
	upstream *httptest.Server
	hits     *atomic.Int64
}

// newVirtualFixture seeds two local members (mv-a first, mv-b second), one
// remote member behind upstream and a virtual over them in that
// declaration order; priority marks the caller adds per case.
func newVirtualFixture(t *testing.T, virtualConfig string) *virtualFixture {
	t.Helper()
	hs := newHarness(t)
	ctx := context.Background()

	f := &virtualFixture{hs: hs, hits: &atomic.Int64{}}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		switch r.URL.Path {
		case "/com/acme/lib/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>com.acme</groupId>
  <artifactId>lib</artifactId>
  <versioning>
    <latest>9.9.9</latest>
    <release>9.9.9</release>
    <lastUpdated>20240102030405</lastUpdated>
    <versions>
      <version>9.9.9</version>
    </versions>
  </versioning>
</metadata>
`))
		case "/com/acme/only-remote/maven-metadata.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<metadata>
  <groupId>com.acme</groupId>
  <artifactId>only-remote</artifactId>
  <versioning>
    <latest>3.0.0</latest>
    <release>3.0.0</release>
    <lastUpdated>20240102030406</lastUpdated>
    <versions>
      <version>3.0.0</version>
    </versions>
  </versioning>
</metadata>
`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	rows := []*metadata.Repo{
		{RepoKey: "mv-a", Type: repo.TypeLocal, PackageType: Protocol, Config: "{}"},
		{RepoKey: "mv-b", Type: repo.TypeLocal, PackageType: Protocol, Config: "{}"},
		{RepoKey: "mv-rem", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"` + f.upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "mv-virt", Type: repo.TypeVirtual, PackageType: Protocol, Config: virtualConfig},
	}
	for _, row := range rows {
		if _, err := hs.svc.CreateRepo(ctx, adminP, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}
	return f
}

// deployReleasePom lands one release pom (module documents are the
// calculator's async row — waitCalc drains it).
func (f *virtualFixture) deployReleasePom(t *testing.T, repoKey, version string) {
	t.Helper()
	path := fmt.Sprintf("com/acme/lib/%s/lib-%s.pom", version, version)
	if resp := f.hs.serve(http.MethodPut, "/"+repoKey+"/"+path, []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom PUT %s/%s = %d (%s)", repoKey, path, resp.StatusCode, drain(t, resp))
	}
	f.hs.waitCalc()
}

// deploySnapshotPom lands one unique snapshot pom (the version directory's
// recalculation is synchronous; the module row still drains).
func (f *virtualFixture) deploySnapshotPom(t *testing.T, repoKey, ts string, n int) {
	t.Helper()
	path := fmt.Sprintf("com/acme/lib/1.0-SNAPSHOT/lib-1.0-%s-%d.pom", ts, n)
	if resp := f.hs.serve(http.MethodPut, "/"+repoKey+"/"+path, []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("snapshot pom PUT %s/%s = %d (%s)", repoKey, path, resp.StatusCode, drain(t, resp))
	}
	f.hs.waitCalc()
}

// TestVirtualMetadataModuleMerge is the AC's core matrix: the same GAV
// spread over several members merges into one version list, recomputed and
// re-sorted, with the members' overlapping version deduplicated.
func TestVirtualMetadataModuleMerge(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b","mv-rem"]}`)

	f.deployReleasePom(t, "mv-a", "1.0.0")
	f.deployReleasePom(t, "mv-b", "1.1.0")
	f.deployReleasePom(t, "mv-b", "1.0.0")  // the overlap: one entry, not two
	f.deployReleasePom(t, "mv-b", "1.0.10") // Maven order: 1.0.10 > 1.0.9-style traps

	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "")
	if status != http.StatusOK {
		t.Fatalf("virtual metadata = %d (%s)", status, body)
	}
	// Versions union in Maven order (1.0.10 sorts after 1.1.0? No: 1.1.0 >
	// 1.0.10 — minor 1 beats 0), the remote member's 9.9.9 included, and
	// latest/release recomputed over the union.
	want := []string{
		"<version>1.0.0</version>", "<version>1.0.10</version>",
		"<version>1.1.0</version>", "<version>9.9.9</version>",
		"<latest>9.9.9</latest>", "<release>9.9.9</release>",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("merged module document missing %q:\n%s", w, body)
		}
	}
	if i := strings.Index(body, "<version>1.1.0</version>"); !strings.Contains(body[i+1:], "<version>") || strings.Index(body, "<version>9.9.9</version>") <= i {
		t.Errorf("versions not in Maven order (1.1.0 must precede 9.9.9):\n%s", body)
	}
	if strings.Count(body, "<version>1.0.0</version>") != 1 {
		t.Errorf("overlapping member version not deduplicated:\n%s", body)
	}
}

// TestVirtualMetadataPriorityShortCircuit: once a priority-bucket member
// has produced the document, the non-priority bucket is not consulted —
// the marked member's list IS the answer (foundByPriority).
func TestVirtualMetadataPriorityShortCircuit(t *testing.T) {
	// mv-b carries the mark: it forms the priority bucket alone, so mv-a's
	// 1.0.0 must not appear even though mv-a is declared first.
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	if _, err := f.hs.svc.UpdateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey: "mv-b", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark mv-b priority: %v", err)
	}
	f.deployReleasePom(t, "mv-a", "1.0.0")
	f.deployReleasePom(t, "mv-b", "1.1.0")

	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "")
	if status != http.StatusOK {
		t.Fatalf("virtual metadata = %d (%s)", status, body)
	}
	mustContain(t, "priority merge", body, []string{"<version>1.1.0</version>"}, []string{"<version>1.0.0</version>"})

	// The same GAV on the download plane is unchanged (first-hit keeps the
	// two-bucket order): the artifact GET is NOT aggregated.
	if resp := f.hs.serve(http.MethodGet, "/mv-virt/com/acme/lib/1.0.0/lib-1.0.0.pom", nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("download resolution changed under aggregation = %d", resp.StatusCode)
	}
}

// TestVirtualMetadataSnapshotMerge: version-level documents merge on
// buildNumber — the larger wins the snapshot block and the
// snapshotVersions (the MNG-5180 section: the merged output KEEPS
// snapshotVersions, which Maven's own client-side merge drops).
func TestVirtualMetadataSnapshotMerge(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)

	f.deploySnapshotPom(t, "mv-a", "20240101.120000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.130000", 1)
	f.deploySnapshotPom(t, "mv-b", "20240102.131000", 2)

	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "1.0-SNAPSHOT")
	if status != http.StatusOK {
		t.Fatalf("virtual snapshot metadata = %d (%s)", status, body)
	}
	mustContain(t, "merged snapshot document", body,
		[]string{"<buildNumber>2</buildNumber>", "<timestamp>20240102.131000</timestamp>", "<snapshotVersions>"},
		[]string{"<buildNumber>1</buildNumber>", "<latest>"})
	// The winning member's spelling is the merged value entry (MNG-5180:
	// the entry resolves the timestamped file name).
	if !strings.Contains(body, "<value>1.0-20240102.131000-2</value>") {
		t.Errorf("snapshotVersions did not carry the winning build's value:\n%s", body)
	}
}

// TestVirtualMetadataComputedPerRequest: nothing is cached — a deploy that
// lands between two GETs is visible to the very next one.
func TestVirtualMetadataComputedPerRequest(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deployReleasePom(t, "mv-a", "1.0.0")

	if _, body := f.hs.getMeta("mv-virt", "com.acme", "lib", ""); strings.Contains(body, "1.2.0") {
		t.Fatalf("precondition: 1.2.0 already visible\n%s", body)
	}
	f.deployReleasePom(t, "mv-b", "1.2.0")
	if _, body := f.hs.getMeta("mv-virt", "com.acme", "lib", ""); !strings.Contains(body, "<version>1.2.0</version>") {
		t.Errorf("per-request recompute did not pick up the new deploy:\n%s", body)
	}
}

// TestVirtualMetadataSidecarAndConditional: the checksum sidecars of the
// merged document are computed over the MERGED bytes, and If-None-Match
// short-circuits 304.
func TestVirtualMetadataSidecarAndConditional(t *testing.T) {
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-b"]}`)
	f.deployReleasePom(t, "mv-a", "1.0.0")
	f.deployReleasePom(t, "mv-b", "1.1.0")

	resp := f.hs.serve(http.MethodGet, "/mv-virt/com/acme/lib/maven-metadata.xml.sha1", nil, nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("merged sidecar = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	sha1Body := strings.TrimSpace(string(drain(t, resp)))
	if sha1Body != resp.Header.Get("X-Checksum-Sha1") || sha1Body != resp.Header.Get("ETag") {
		t.Errorf("sidecar body %q disagrees with headers %+v", sha1Body, resp.Header)
	}
	if resp.Header.Get("X-Checksum-Sha256") == "" {
		t.Errorf("merged response missing the sha256 header")
	}

	etag := resp.Header.Get("ETag")
	cond := f.hs.serve(http.MethodGet, "/mv-virt/com/acme/lib/maven-metadata.xml", nil,
		map[string]string{"If-None-Match": etag}, true)
	if cond.StatusCode != http.StatusNotModified {
		t.Fatalf("If-None-Match on the merged document = %d", cond.StatusCode)
	}

	// A member's own sidecar (first-hit on the member plane) stays the
	// member's — aggregation is the virtual face only.
	if resp := f.hs.serve(http.MethodGet, "/mv-a/com/acme/lib/maven-metadata.xml.sha256", nil, nil, true); resp.StatusCode != http.StatusOK {
		t.Fatalf("member sidecar = %d", resp.StatusCode)
	}
}

// TestVirtualMetadataMemberFaults: a classified member failure propagates
// VERBATIM (the block pass-through), an unfound member contributes
// nothing, and a member whose document does not parse is skipped.
func TestVirtualMetadataMemberFaults(t *testing.T) {
	ctx := context.Background()

	// Block passthrough: the SSRF-refusing remote member's own 400 is the
	// whole answer even though a healthy local member holds the GAV.
	f := newVirtualFixture(t, `{"repositories":["mv-a","mv-rem"]}`)
	if _, err := f.hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "mv-ssrf", Type: repo.TypeRemote, PackageType: Protocol,
		Config: `{"url":"` + f.upstream.URL + `"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(mv-ssrf): %v", err)
	}
	f.deployReleasePom(t, "mv-a", "1.0.0")

	// The un-marked remote is allowed (fixture) — point a second virtual at
	// the refusing one to build the fault case.
	if _, err := f.hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "mv-fault", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["mv-a","mv-ssrf"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(mv-fault): %v", err)
	}
	resp := f.hs.serve(http.MethodGet, "/mv-fault/com/acme/lib/maven-metadata.xml", nil, nil, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("member-fault merge = %d, want the engine's 400 (%s)", resp.StatusCode, drain(t, resp))
	}
	if body := drain(t, resp); !strings.Contains(string(body), "suppressed upstream") {
		t.Errorf("member-fault body = %s, want the engine's SSRF wording", body)
	}

	// Unfound members contribute nothing: the GAV only one remote knows
	// still aggregates (and a miss on a member the walk passed through
	// leaves its negative row — the FR-20 quieting, by design here).
	status, rbody := f.hs.getMeta("mv-virt", "com.acme", "only-remote", "")
	if status != http.StatusOK || !strings.Contains(rbody, "<version>3.0.0</version>") {
		t.Fatalf("remote-only GAV through the virtual = %d (%s)", status, rbody)
	}

	// A member whose stored document does not parse is skipped, not fatal:
	// land garbage where the calculator will not regenerate it (a group
	// directory no pom-bearing child backs).
	if _, err := f.hs.svc.Put(ctx, adminP, "mv-b", "com/acme/lib/maven-metadata.xml",
		strings.NewReader("not-xml"), storage.BlobRef{}, "application/xml"); err != nil {
		t.Fatalf("seed garbage member document: %v", err)
	}
	status, body := f.hs.getMeta("mv-virt", "com.acme", "lib", "")
	if status != http.StatusOK || !strings.Contains(body, "<version>1.0.0</version>") {
		t.Fatalf("merge with one corrupt member = %d (%s)", status, body)
	}

	// All members empty: the honest 404.
	if status, _ := f.hs.getMeta("mv-virt", "com.acme", "nobody", ""); status != http.StatusNotFound {
		t.Fatalf("unknown GAV through the virtual = %d, want 404", status)
	}
}
