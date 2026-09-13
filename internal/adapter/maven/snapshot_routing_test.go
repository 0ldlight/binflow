package maven

// L014-2 review B1/B2: the snapshot rewrite must not drift with the
// deployer's PERMISSIONS (a write-without-read principal still gets
// trip arithmetic — the trip derives from the ungated facts seam, never a
// read-gated svc.Get) and must address the ROUTED MEMBER under a virtual
// repository key (the virtual key holds no storage facts; consulting it
// mints a fresh (ts, N) per PUT and accumulates same-buildNumber files).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedWriteOnlyTarget grants principal user write (and nothing else) on
// every path of repoKey.
func seedWriteOnlyTarget(t *testing.T, hs *harness, name, user, repoKey string) {
	t.Helper()
	mk := func(l []string) string {
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	now := metadata.Now()
	if err := hs.md.Permissions().PutTarget(context.Background(),
		&metadata.PermissionTarget{
			Name: name, Repos: mk([]string{repoKey}), Includes: mk([]string{"**"}),
			Excludes: mk(nil), CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: user, PrincipalType: "user",
			CanRead: false, CanWrite: true,
		}}); err != nil {
		t.Fatalf("seed permission target: %v", err)
	}
}

// TestSnapshotRewriteWriteOnlyPrincipal is review B1: the trip arithmetic
// survives a deployer the read gate refuses. Before the fix the trip read
// went through svc.Get (ActionRead-gated): ErrForbidden degraded the trip
// to none, the candidate fell back to 1, and every deploy OVERWROTE the
// previous build's unique file in place — the immutability contract broken
// as a function of permissions. The advancing sequence is pom-then-pom
// (a pom re-put opens the next trip; consecutive same-coordinate leader
// PUTs reuse their spelling by design, which is an overwrite the write
// grant alone must refuse).
func TestSnapshotRewriteWriteOnlyPrincipal(t *testing.T) {
	hs := newHarness(t)
	seedWriteOnlyTarget(t, hs, "maven-snap-wo", "ci-bot", "maven-local")
	const dir = "com/acme/demo-app/2.0.0-SNAPSHOT"
	ci := &auth.Principal{Name: "ci-bot"}

	putPom := func(v string) *http.Response {
		t.Helper()
		return hs.serveAs(http.MethodPut, "/maven-local/"+dir+"/demo-app-2.0.0-SNAPSHOT.pom", pomB(v), nil, ci)
	}

	resp := putPom("2.0.0-SNAPSHOT")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("write-only PUT trip 1 = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	ts1, n1, _, _ := locOf(t, resp)

	// the read gate bites for this principal (the premise of B1: the trip
	// must not need it)
	if resp := hs.serveAs(http.MethodGet, "/maven-local/"+dir+"/maven-metadata.xml", nil, nil, ci); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("write-only principal metadata GET = %d, want 403 (the gate this test pivots on)", resp.StatusCode)
	}

	// the pom re-put opens the NEXT trip under this principal too — the
	// ungated facts seam, not the denied read, decides the numbering
	resp = putPom("2.0.0-SNAPSHOT-r2")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("write-only PUT trip 2 = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	_, n2, _, _ := locOf(t, resp)
	if n1 != "1" || n2 != "2" {
		t.Fatalf("trip numbering under write-only principal = %s then %s, want 1 then 2", n1, n2)
	}

	// trip one's bytes survive at their own spelling (the immutability
	// contract): the ORIGINAL version line, not the re-put one
	if code, got := mustGet(t, hs, "/maven-local/"+dir+"/demo-app-2.0.0-"+ts1+"-"+n1+".pom"); code != http.StatusOK || !strings.Contains(got, "<version>2.0.0-SNAPSHOT</version>") || strings.Contains(got, "-r2") {
		t.Errorf("trip-one pom after trip two = %d %q, want 200 the original version line", code, got)
	}
}

// TestSnapshotRewriteVirtualRouting is review B2: a virtual repository with
// a defaultDeploymentRepo routes the write onto its member — the snapshot
// arithmetic consults the MEMBER's facts and behavior config, the wire
// (Location, addressed key) keeps the client's virtual spelling, and the
// numbering continues across requests made through the virtual key.
func TestSnapshotRewriteVirtualRouting(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()

	if err := hs.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-vtarget", Type: repo.TypeLocal, PackageType: Protocol, Config: `{}`}); err != nil {
		t.Fatalf("seed maven-vtarget: %v", err)
	}
	if err := hs.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "maven-vroute", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["maven-vtarget"],"defaultDeploymentRepo":"maven-vtarget"}`}); err != nil {
		t.Fatalf("seed maven-vroute: %v", err)
	}

	const dir = "com/acme/demo-app/2.0.0-SNAPSHOT"
	admin := &auth.Principal{Name: "admin", Admin: true}

	// trip 1 (leader) and the pom (follower) through the VIRTUAL key
	resp := hs.serveAs(http.MethodPut, "/maven-vroute/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("v1"), nil, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("virtual PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/maven-vroute/") {
		t.Errorf("virtual PUT Location = %q, want the ADDRESSED (virtual) key rendered", loc)
	}
	ts1, n1, _, _ := locOf(t, resp)
	if n1 != "1" {
		t.Fatalf("virtual first trip N = %s, want 1", n1)
	}

	resp = hs.serveAs(http.MethodPut, "/maven-vroute/"+dir+"/demo-app-2.0.0-SNAPSHOT.pom", pomB("2.0.0-SNAPSHOT"), nil, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("virtual pom PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if ts2, _, _, _ := locOf(t, resp); ts2 != ts1 {
		t.Errorf("virtual follower pom ts = %s, want the member trip %s", ts2, ts1)
	}

	// trip 2 through the virtual key: the MEMBER's facts continue the
	// numbering (a virtual-key consultation would see no files and restart
	// at 1 — the accumulation bug)
	resp = hs.serveAs(http.MethodPut, "/maven-vroute/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("v2"), nil, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("virtual second trip = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if _, n2, _, _ := locOf(t, resp); n2 != "2" {
		t.Fatalf("virtual second trip N = %s, want 2 (member facts continue)", n2)
	}

	// the files live in the MEMBER; the virtual key holds no nodes
	memberNodes, err := hs.md.Nodes().ListByPrefix(ctx, "maven-vtarget", dir)
	if err != nil {
		t.Fatalf("member list: %v", err)
	}
	files := 0
	for _, n := range memberNodes {
		if !strings.HasSuffix(n.Path, "/") && !strings.Contains(n.Path, "maven-metadata.xml") {
			files++
		}
	}
	if files != 3 { // jar trip1, pom trip1, jar trip2
		t.Fatalf("member unique files = %d, want 3: %v", files, memberNodes)
	}
	if virtNodes, verr := hs.md.Nodes().ListByPrefix(ctx, "maven-vroute", ""); verr != nil || len(virtNodes) != 0 {
		t.Errorf("virtual key storage rows = %d (err %v), want 0", len(virtNodes), verr)
	}

	// an UNROUTED virtual keeps the service's C5 405 (the rewrite declines;
	// it must not mint a name for a write that never lands)
	if resp := hs.serveAs(http.MethodPut,
		"/maven-virtual/com/acme/demo-app/2.0.0-SNAPSHOT/demo-app-2.0.0-SNAPSHOT.jar",
		jarB("x"), nil, admin); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("unrouted virtual PUT = %d, want 405", resp.StatusCode)
	}
}
