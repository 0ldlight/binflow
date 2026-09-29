package maven

// T-566 / BIN-48: the module-level maven-metadata.xml WRITE-PATH
// MATERIALIZATION (contract maven/deploy-put-version-metadata-auto-
// materialize): a wire pom PUT lands the module document as a PHYSICAL
// node before the PUT response returns — visible to any immediate
// post-PUT observation (the deep-list count face's deterministic
// +1/module), merged by the next version PUT, reclaimed by artifact
// DELETE; the SNAPSHOT version-level materialization face (already
// synchronous) is regression-fenced, and the read-time derivation faces
// (the M3 capability rider and the virtual merge) coexist over the
// materialized nodes.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// moduleMetaGet fetches a module-level document through the wire plane.
func moduleMetaGet(hs *harness, repoKey, orgDir, module string, hdr map[string]string) (int, string) {
	t := hs.t
	resp := hs.serve(http.MethodGet, fmt.Sprintf("/%s/%s/%s/maven-metadata.xml", repoKey, orgDir, module), nil, hdr, true)
	return resp.StatusCode, string(drain(t, resp))
}

// nodeExists asserts a physical node row exists at the path (the storage
// existence/count face — the contract's residual divergence body).
func nodeExists(hs *harness, repoKey, path string) bool {
	nodes, err := hs.md.Nodes().ListByPrefix(context.Background(), repoKey, path)
	if err != nil {
		return false
	}
	for _, n := range nodes {
		if n.Path == path {
			return true
		}
	}
	return false
}

// TestModuleMetadataWritePathMaterializes is contract ⑨'s core: the pom
// PUT response returns only after the module document is a physical node
// (no waitCalc drain — the assertion runs inside the race window the
// async posture used to leave open), the second version merges into it,
// the release version level never materializes, and the artifact DELETE
// reclaims the list.
func TestModuleMetadataWritePathMaterializes(t *testing.T) {
	hs := newHarness(t)
	const org, mod = "com.walk", "mm"
	dir := "com/walk/mm"

	// First release pom: the module document is there the moment the PUT
	// responds (write-path sync materialization — L035 m1's A face).
	if resp := putPom(hs, "maven-unique", dir+"/1.0.0/mm-1.0.0.pom", pomFor(org, mod, "1.0.0", "m-v1")); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT 1.0.0 = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if !nodeExists(hs, "maven-unique", dir+"/maven-metadata.xml") {
		t.Fatalf("module metadata node absent immediately after the PUT response — the write-path materialization did not land")
	}
	code, body := moduleMetaGet(hs, "maven-unique", "com/walk", "mm", nil)
	if code != http.StatusOK || !strings.Contains(body, "<version>1.0.0</version>") ||
		!strings.Contains(body, "<latest>1.0.0</latest>") || !strings.Contains(body, "<release>1.0.0</release>") {
		t.Errorf("module metadata after first PUT = %d %.120s, want 200 versions=[1.0.0] latest=release=1.0.0", code, body)
	}
	// The count face's sidecar: the materialized document's .sha1 answers
	// its own digest.
	s1, _, _ := digests([]byte(body))
	sc := hs.serve(http.MethodGet, "/maven-unique/"+dir+"/maven-metadata.xml.sha1", nil, nil, true)
	if got := strings.TrimSpace(string(drain(t, sc))); sc.StatusCode != http.StatusOK || got != s1 {
		t.Errorf("module metadata .sha1 = %d %s, want 200 the document's digest %s", sc.StatusCode, got, s1)
	}
	// The release version level never materializes (m0/m3 boundary).
	if code, _ := mustGet(t, hs, "/maven-unique/"+dir+"/1.0.0/maven-metadata.xml"); code != http.StatusNotFound {
		t.Errorf("release version metadata = %d, want 404 (release never materializes the version level)", code)
	}

	// Second release version: merged immediately, latest/release flip.
	if resp := putPom(hs, "maven-unique", dir+"/1.1.0/mm-1.1.0.pom", pomFor(org, mod, "1.1.0", "m-v2")); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT 1.1.0 = %d", resp.StatusCode)
	}
	code, body = moduleMetaGet(hs, "maven-unique", "com/walk", "mm", nil)
	if code != http.StatusOK || !strings.Contains(body, "<version>1.0.0</version>") ||
		!strings.Contains(body, "<version>1.1.0</version>") ||
		!strings.Contains(body, "<latest>1.1.0</latest>") || !strings.Contains(body, "<release>1.1.0</release>") {
		t.Errorf("module metadata after second PUT = %d %.160s, want merged versions=[1.0.0,1.1.0] latest=release=1.1.0", code, body)
	}

	// Artifact DELETE reclaims the version from the list (m4; the delete
	// side's recalc stays asynchronous — drained here for the assertion).
	if resp := hs.serve(http.MethodDelete, "/maven-unique/"+dir+"/1.0.0/mm-1.0.0.pom", nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE 1.0.0 pom = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	hs.waitCalc()
	code, body = moduleMetaGet(hs, "maven-unique", "com/walk", "mm", nil)
	if code != http.StatusOK || strings.Contains(body, "<version>1.0.0</version>") || !strings.Contains(body, "<version>1.1.0</version>") {
		t.Errorf("module metadata after DELETE = %d %.120s, want versions=[1.1.0] (1.0.0 reclaimed)", code, body)
	}
	if code, _ := mustGet(t, hs, "/maven-unique/"+dir+"/1.0.0/maven-metadata.xml"); code != http.StatusNotFound {
		t.Errorf("deleted version's metadata = %d, want 404", code)
	}
	// Deleting the last pom removes the document outright (the reclaim
	// end of the materialization lifecycle).
	if resp := hs.serve(http.MethodDelete, "/maven-unique/"+dir+"/1.1.0/mm-1.1.0.pom", nil, nil, true); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE 1.1.0 pom = %d", resp.StatusCode)
	}
	hs.waitCalc()
	if code, _ := moduleMetaGet(hs, "maven-unique", "com/walk", "mm", nil); code != http.StatusNotFound {
		t.Errorf("module metadata after last pom delete = %d, want 404 (document reclaimed)", code)
	}
}

// TestSnapshotMaterializeFacesUnchanged fences the SNAPSHOT posture
// around the change: the unique home's plain pom PUT keeps materializing
// the VERSION-level document synchronously (in the directory listing
// before any metadata GET — L035 m6), the module document joins it
// (+1/module), and the read-time derivation faces coexist over the
// materialized nodes: the M3 capability rider still strips
// <snapshotVersions> for a rejected client on the version level while
// the module level passes untouched, and the virtual merge serves the
// member's materialized facts.
func TestSnapshotMaterializeFacesUnchanged(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	if _, err := hs.svc.CreateRepo(ctx, adminP, &metadata.Repo{
		RepoKey: "mat-v", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["maven-unique"]}`,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}
	const dir = "com/walk/ms/3.0-SNAPSHOT"

	if resp := putPom(hs, "maven-unique", dir+"/ms-3.0-SNAPSHOT.pom", pomFor("com.walk", "ms", "3.0-SNAPSHOT", "ms-p1")); resp.StatusCode != http.StatusCreated {
		t.Fatalf("plain snapshot pom PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	// m6 face: the version document is a physical node BEFORE any metadata
	// GET (write-path sync, not read-triggered), and the module document
	// materializes with it.
	if !nodeExists(hs, "maven-unique", dir+"/maven-metadata.xml") {
		t.Errorf("snapshot version metadata node absent before any metadata GET — the sync materialization regressed")
	}
	if !nodeExists(hs, "maven-unique", "com/walk/ms/maven-metadata.xml") {
		t.Errorf("module metadata node absent after snapshot pom PUT — the module materialization missed the snapshot pom")
	}
	code, body := mustGet(t, hs, "/maven-unique/"+dir+"/maven-metadata.xml")
	if code != http.StatusOK || !strings.Contains(body, "<snapshotVersions>") || !strings.Contains(body, "<buildNumber>") {
		t.Errorf("snapshot version metadata = %d %.120s, want 200 snapshot block + snapshotVersions", code, body)
	}
	// The module list carries the snapshot version (release omitted when
	// every version is a snapshot).
	if code, mbody := moduleMetaGet(hs, "maven-unique", "com/walk", "ms", nil); code != http.StatusOK ||
		!strings.Contains(mbody, "<version>3.0-SNAPSHOT</version>") || strings.Contains(mbody, "<release>") {
		t.Errorf("module metadata over snapshot = %d %.140s, want versions=[3.0-SNAPSHOT] with no <release>", code, mbody)
	}

	// M3 rider coexistence over the materialized nodes: a rejected client
	// (Java/1.8 — the bare-java form) gets the version document WITHOUT
	// <snapshotVersions> (<snapshot> kept); the module level passes
	// untouched for the same client.
	rej := map[string]string{"User-Agent": "Java/1.8.0_391"}
	sr := hs.serve(http.MethodGet, "/maven-unique/"+dir+"/maven-metadata.xml", nil, rej, true)
	stripped := string(drain(t, sr))
	if sr.StatusCode != http.StatusOK || strings.Contains(stripped, "<snapshotVersions>") || !strings.Contains(stripped, "<snapshot>") {
		t.Errorf("rejected-UA snapshot metadata = %d %.140s, want 200 stripped of <snapshotVersions> with <snapshot> kept", sr.StatusCode, stripped)
	}
	code, mNoUA := moduleMetaGet(hs, "maven-unique", "com/walk", "ms", nil)
	_, mRej := moduleMetaGet(hs, "maven-unique", "com/walk", "ms", rej)
	if code != http.StatusOK || mNoUA != mRej {
		t.Errorf("module metadata rider leak: rejected-UA body differs from the plain body (module level passes untouched)")
	}

	// The virtual merge serves the member's materialized module facts.
	if code, vbody := moduleMetaGet(hs, "mat-v", "com/walk", "ms", nil); code != http.StatusOK || !strings.Contains(vbody, "<version>3.0-SNAPSHOT</version>") {
		t.Errorf("virtual module metadata = %d %.120s, want 200 the merged member facts", code, vbody)
	}
}
