package maven

// T-562 / BIN-44 stage 2: the plain-SNAPSHOT walk resolve family (contract
// maven/plain-snapshot-unique-walk-resolve + maven/virtual-plain-walk-
// cross-member-selection, the L035 W1/W2/W3/W4 A-oracle matrix). The arms
// mirror the forensics cases: the trigger matrix (GET/HEAD/Range/sidecar
// all address the resolved entity), the selection rules (filename ts,
// numeric buildNumber, four-tuple family scoping), the honest-miss gates
// (t8 non-SNAPSHOT spelling, t9 no cross-extension fallback) and the
// virtual cross-member mtime pick.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// putPom lands one pom with declared checksums (the wire-PUT shape the
// difftest legs use), returning the response.
func putPom(hs *harness, repoKey, gavPath string, body []byte) *http.Response {
	s1, _, _ := digests(body)
	return hs.serve(http.MethodPut, "/"+repoKey+"/"+gavPath, body, map[string]string{
		"X-Checksum-Sha1": s1,
	}, true)
}

// pomFor renders a minimal pom matching the given (dotted) GAV — the
// consistency gate's shape.
func pomFor(group, module, version, marker string) []byte {
	return []byte(fmt.Sprintf(
		"<project><groupId>%s</groupId><artifactId>%s</artifactId><version>%s</version><!-- %s --></project>",
		group, module, version, marker))
}

// TestPlainSnapshotWalkTriggerMatrix is W1: in a unique home the plain
// pom/jar GET, HEAD, Range and checksum sidecars all serve the REWRITTEN
// entity — validators, slices and digests address the resolved target —
// while the version metadata document reads directly and a
// wrong-extension directory stays the honest 404 (t9).
func TestPlainSnapshotWalkTriggerMatrix(t *testing.T) {
	hs := newHarness(t)
	dir := "com/walk/wt/1.0-SNAPSHOT"
	plainPom := "/maven-unique/" + dir + "/wt-1.0-SNAPSHOT.pom"
	pom := pomFor("com.walk", "wt", "1.0-SNAPSHOT", "w1pom")
	jar := []byte("w1 jar payload\n")

	resp := putPom(hs, "maven-unique", dir+"/wt-1.0-SNAPSHOT.pom", pom)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("plain pom PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "-20") || strings.HasSuffix(loc, "-SNAPSHOT.pom") {
		t.Fatalf("unique home PUT Location = %q, want the timestamped rewrite", loc)
	}
	if resp := putPom(hs, "maven-unique", "com/walk/wj/2.0-SNAPSHOT/wj-2.0-SNAPSHOT.jar", jar); resp.StatusCode != http.StatusCreated {
		t.Fatalf("plain jar PUT = %d", resp.StatusCode)
	}

	// t1 GET: the rewritten entity's bytes.
	if code, got := mustGet(t, hs, plainPom); code != http.StatusOK || got != string(pom) {
		t.Errorf("t1 GET plain pom = %d %.40s, want 200 the rewritten bytes", code, got)
	}
	// t2 HEAD: validators echo the resolved target.
	h := hs.serve(http.MethodHead, plainPom, nil, nil, true)
	if h.StatusCode != http.StatusOK {
		t.Errorf("t2 HEAD plain pom = %d, want 200", h.StatusCode)
	}
	s1, _, _ := digests(pom)
	if h.Header.Get("Content-Length") != fmt.Sprint(len(pom)) || h.Header.Get("ETag") != s1 {
		t.Errorf("t2 HEAD validators = len %s etag %s, want len %d etag %s (the target's)",
			h.Header.Get("Content-Length"), h.Header.Get("ETag"), len(pom), s1)
	}
	// t3 GET plain jar.
	if code, got := mustGet(t, hs, "/maven-unique/com/walk/wj/2.0-SNAPSHOT/wj-2.0-SNAPSHOT.jar"); code != http.StatusOK || got != string(jar) {
		t.Errorf("t3 GET plain jar = %d %.20s, want 200 the jar bytes", code, got)
	}
	// t4 Range: sliced on the resolved entity.
	rng := hs.serve(http.MethodGet, plainPom, nil, map[string]string{"Range": "bytes=0-15"}, true)
	body := drain(t, rng)
	if rng.StatusCode != http.StatusPartialContent || len(body) != 16 || string(body) != string(pom[:16]) {
		t.Errorf("t4 Range plain pom = %d (%d bytes), want 206 of the resolved entity's first 16 bytes",
			rng.StatusCode, len(body))
	}
	// t5/t6 checksum sidecars: the resolved entity's digests.
	_, md5v, _ := digests(pom)
	for algo, want := range map[string]string{"sha1": s1, "md5": md5v} {
		sc := hs.serve(http.MethodGet, plainPom+"."+algo, nil, nil, true)
		if got := strings.TrimSpace(string(drain(t, sc))); sc.StatusCode != http.StatusOK || got != want {
			t.Errorf("t5/t6 sidecar .%s = %d %s, want 200 %s (the resolved target's digest)", algo, sc.StatusCode, got, want)
		}
	}
	// t7 version metadata reads directly (no walk interference).
	hs.waitCalc()
	if code, _ := mustGet(t, hs, "/maven-unique/"+dir+"/maven-metadata.xml"); code != http.StatusOK {
		t.Errorf("t7 version metadata GET = %d, want 200 (direct read)", code)
	}
	// t9 plain pom where only a .jar candidate exists: no cross-extension
	// fallback, the honest 404 stands.
	jarDir := "com/walk/gt9/4.0-SNAPSHOT"
	if resp := putPom(hs, "maven-unique", jarDir+"/gt9-4.0-20260601.000001-1.jar", []byte("t9 jar")); resp.StatusCode != http.StatusCreated {
		t.Fatalf("t9 seed jar PUT = %d", resp.StatusCode)
	}
	if code, _ := mustGet(t, hs, "/maven-unique/"+jarDir+"/gt9-4.0-SNAPSHOT.pom"); code != http.StatusNotFound {
		t.Errorf("t9 plain pom with only jar candidates = %d, want 404", code)
	}
}

// TestPlainSnapshotWalkSelection is W2: max(filename ts, buildNumber) —
// ts chronological, ts ties on NUMERIC buildNumber, upload order
// irrelevant, extension and classifier families resolving independently.
func TestPlainSnapshotWalkSelection(t *testing.T) {
	hs := newHarness(t)
	tests := []struct {
		name  string
		art   string
		seed  [][2]string // file name -> marker
		probe string
		want  string
	}{
		{"s1 filename-ts beats upload order (newer ts uploaded first)", "w2s1",
			[][2]string{{"w2s1-1.0-20260601.000001-1.pom", "s1-newts"}, {"w2s1-1.0-20260101.000001-1.pom", "s1-oldts"}},
			"w2s1-1.0-SNAPSHOT.pom", "s1-newts"},
		{"s2 ts tie -> larger buildNumber", "w2s2",
			[][2]string{{"w2s2-1.0-20260501.000001-1.pom", "s2-bn1"}, {"w2s2-1.0-20260501.000001-2.pom", "s2-bn2"}},
			"w2s2-1.0-SNAPSHOT.pom", "s2-bn2"},
		{"s2b ts tie, reverse upload order -> bn still wins", "w2s2b",
			[][2]string{{"w2s2b-1.0-20260501.000001-2.pom", "s2b-bn2"}, {"w2s2b-1.0-20260501.000001-1.pom", "s2b-bn1"}},
			"w2s2b-1.0-SNAPSHOT.pom", "s2b-bn2"},
		{"s3 buildNumber numeric (10 beats 9)", "w2s3",
			[][2]string{{"w2s3-1.0-20260502.000001-9.pom", "s3-bn9"}, {"w2s3-1.0-20260502.000001-10.pom", "s3-bn10"}},
			"w2s3-1.0-SNAPSHOT.pom", "s3-bn10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := "com/walk/" + tt.art + "/1.0-SNAPSHOT"
			for _, s := range tt.seed {
				if resp := putPom(hs, "maven-unique", dir+"/"+s[0], pomFor("com.walk", tt.art, "1.0-SNAPSHOT", s[1])); resp.StatusCode != http.StatusCreated {
					t.Fatalf("seed %s = %d (%s)", s[0], resp.StatusCode, drain(t, resp))
				}
			}
			code, got := mustGet(t, hs, "/maven-unique/"+dir+"/"+tt.probe)
			if code != http.StatusOK || !strings.Contains(got, tt.want) {
				t.Errorf("plain GET = %d %.60s, want 200 the %s entity", code, got, tt.want)
			}
		})
	}

	// s4 extension scoping: newer jar + older pom in one dir — each family
	// resolves its own max.
	s4 := "com/walk/w2s4/1.0-SNAPSHOT"
	putPom(hs, "maven-unique", s4+"/w2s4-1.0-20260101.000001-1.jar", []byte("s4 jar"))
	putPom(hs, "maven-unique", s4+"/w2s4-1.0-20260601.000001-1.pom", pomFor("com.walk", "w2s4", "1.0-SNAPSHOT", "s4-pom"))
	if code, got := mustGet(t, hs, "/maven-unique/"+s4+"/w2s4-1.0-SNAPSHOT.pom"); code != http.StatusOK || !strings.Contains(got, "s4-pom") {
		t.Errorf("s4 plain pom = %d %.40s, want the pom family's own winner", code, got)
	}
	if code, got := mustGet(t, hs, "/maven-unique/"+s4+"/w2s4-1.0-SNAPSHOT.jar"); code != http.StatusOK || got != "s4 jar" {
		t.Errorf("s4 plain jar = %d %.20s, want the jar family's own winner", code, got)
	}

	// s5 classifier scoping: the sources family resolves its only
	// candidate even though the pom family's max-ts build has no sources.
	s5 := "com/walk/w2s5/1.0-SNAPSHOT"
	putPom(hs, "maven-unique", s5+"/w2s5-1.0-20260101.000001-1-sources.jar", []byte("s5 sources"))
	putPom(hs, "maven-unique", s5+"/w2s5-1.0-20260707.000001-2.pom", pomFor("com.walk", "w2s5", "1.0-SNAPSHOT", "s5-pom"))
	if code, got := mustGet(t, hs, "/maven-unique/"+s5+"/w2s5-1.0-SNAPSHOT-sources.jar"); code != http.StatusOK || got != "s5 sources" {
		t.Errorf("s5 plain sources jar = %d %.20s, want the sources family's own winner (no cross-family miss)", code, got)
	}
}

// TestPlainSnapshotWalkNonSnapshotGate is t8: a non-SNAPSHOT, non-
// timestamped file name in a SNAPSHOT directory holding candidates answers
// the honest 404 — the walk gate lives in the REQUESTED file name's
// -SNAPSHOT spelling, and the read plane never parse-refuses (contract
// maven/non-snapshot-spelling-get-gate-404).
func TestPlainSnapshotWalkNonSnapshotGate(t *testing.T) {
	hs := newHarness(t)
	dir := "com/walk/gt8/3.0-SNAPSHOT"
	if resp := putPom(hs, "maven-unique", dir+"/gt8-3.0-20260601.000001-1.pom", pomFor("com.walk", "gt8", "3.0-SNAPSHOT", "gt8ts")); resp.StatusCode != http.StatusCreated {
		t.Fatalf("t8 seed = %d", resp.StatusCode)
	}
	resp := hs.serve(http.MethodGet, "/maven-unique/"+dir+"/gt8-3.0.pom", nil, nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("t8 GET non-SNAPSHOT spelling = %d (%s), want 404 honest miss", resp.StatusCode, drain(t, resp))
	}
}

// TestPlainSnapshotWalkLiveResolve is W3/c1: the plain GET is a live
// resolve — a second plain PUT flips it to the new trip's bytes (never a
// frozen alias), and the HEAD validators follow.
func TestPlainSnapshotWalkLiveResolve(t *testing.T) {
	hs := newHarness(t)
	dir := "com/walk/wm/2.0-SNAPSHOT"
	plain := "/maven-unique/" + dir + "/wm-2.0-SNAPSHOT.pom"
	p1 := pomFor("com.walk", "wm", "2.0-SNAPSHOT", "wm-p1")
	p2 := pomFor("com.walk", "wm", "2.0-SNAPSHOT", "wm-p2")
	if resp := putPom(hs, "maven-unique", dir+"/wm-2.0-SNAPSHOT.pom", p1); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT p1 = %d", resp.StatusCode)
	}
	if code, got := mustGet(t, hs, plain); code != http.StatusOK || !strings.Contains(got, "wm-p1") {
		t.Fatalf("plain GET after p1 = %d %.40s, want p1", code, got)
	}
	if resp := putPom(hs, "maven-unique", dir+"/wm-2.0-SNAPSHOT.pom", p2); resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT p2 = %d", resp.StatusCode)
	}
	if code, got := mustGet(t, hs, plain); code != http.StatusOK || !strings.Contains(got, "wm-p2") {
		t.Errorf("plain GET after p2 = %d %.40s, want the live resolve to p2", code, got)
	}
	s2h, _, _ := digests(p2)
	h := hs.serve(http.MethodHead, plain, nil, nil, true)
	if h.StatusCode != http.StatusOK || h.Header.Get("ETag") != s2h {
		t.Errorf("HEAD plain after p2 = %d etag %s, want p2's validators", h.StatusCode, h.Header.Get("ETag"))
	}
}

// TestVirtualPlainWalkCrossMemberMtime is W4 (contract
// maven/virtual-plain-walk-cross-member-selection): the cross-member pick
// keys on the candidates' STORAGE mtime — the last landed candidate wins —
// not on the filename (ts, bn) keys the member-internal walk uses. v2 is
// the discriminator: declared [new-ts m1, old-ts m2], uploaded new then
// old, the reference serves the OLD body (global filename-ts refuted).
func TestVirtualPlainWalkCrossMemberMtime(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	for _, r := range []*metadata.Repo{
		{RepoKey: "mwalk-m1", Type: repo.TypeLocal, PackageType: Protocol,
			Config: `{"snapshotVersionBehavior":"unique"}`},
		{RepoKey: "mwalk-m2", Type: repo.TypeLocal, PackageType: Protocol,
			Config: `{"snapshotVersionBehavior":"unique"}`},
		// The t10 shape: a single-member virtual passes the member's walk
		// through.
		{RepoKey: "mwalk-vsolo", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["maven-unique"]}`},
		{RepoKey: "mwalk-v", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["mwalk-m1","mwalk-m2"]}`},
	} {
		if _, err := hs.svc.CreateRepo(ctx, adminP, r); err != nil {
			t.Fatalf("seed %s: %v", r.RepoKey, err)
		}
	}
	dir := "com/walk/vw/vw/3.0-SNAPSHOT"
	newBody := pomFor("com.walk.vw", "vw", "3.0-SNAPSHOT", "vNEW")
	oldBody := pomFor("com.walk.vw", "vw", "3.0-SNAPSHOT", "vOLD")

	// Upload order new -> old with a distinct-mtime gap (the store's node
	// stamps are second-granular): the old body landed LAST and must win.
	putPom(hs, "mwalk-m1", dir+"/vw-3.0-20260808.000001-1.pom", newBody)
	time.Sleep(1100 * time.Millisecond)
	putPom(hs, "mwalk-m2", dir+"/vw-3.0-20260101.000001-1.pom", oldBody)

	if code, got := mustGet(t, hs, "/mwalk-v/"+dir+"/vw-3.0-SNAPSHOT.pom"); code != http.StatusOK || !strings.Contains(got, "vOLD") {
		t.Errorf("virtual plain GET = %d %.40s, want 200 the LAST-LANDED (vOLD) body — the cross-member key is storage mtime, not filename-ts", code, got)
	}
	// The virtual sidecar walks the same winner.
	s1o, _, _ := digests(oldBody)
	sc := hs.serve(http.MethodGet, "/mwalk-v/"+dir+"/vw-3.0-SNAPSHOT.pom.sha1", nil, nil, true)
	if got := strings.TrimSpace(string(drain(t, sc))); sc.StatusCode != http.StatusOK || got != s1o {
		t.Errorf("virtual plain sidecar = %d %s, want the mtime winner's digest", sc.StatusCode, got)
	}

	// t10: a single-member virtual transparently serves the member's walk.
	solo := "com/walk/solo/1.0-SNAPSHOT"
	soloPom := pomFor("com.walk", "solo", "1.0-SNAPSHOT", "solo")
	putPom(hs, "maven-unique", solo+"/solo-1.0-SNAPSHOT.pom", soloPom)
	h := hs.serve(http.MethodHead, "/mwalk-vsolo/"+solo+"/solo-1.0-SNAPSHOT.pom", nil, nil, true)
	if h.StatusCode != http.StatusOK || h.Header.Get("Content-Length") != fmt.Sprint(len(soloPom)) {
		t.Errorf("single-member virtual HEAD plain = %d len %s, want 200 the member walk's entity",
			h.StatusCode, h.Header.Get("Content-Length"))
	}
}
