package maven

// L014-2: the server-side unique-snapshot rewrite (spec section 1.3, wire
// form pinned by the live artifactory-ux 7.161.20 probes — reports/
// compatibility/L013-maven-evidence.md §1.1 plus the L014-2 probe log) and
// the registration-only checksum sidecar (BUG 2). Table rows mirror the
// probe sequence: leader mint, follower trip-join, coordinate overwrite,
// cross-trip arithmetic, behavior tri-state, already-unique pass-through.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/storage"
)

// uniqueNameRegExp matches the timestamped spelling: module-baseRev-ts-N[-classifier].ext
var uniqueNameRegExp = regexp.MustCompile(`demo-app-2\.0\.0-(\d{8}\.\d{6})-(\d+)(-[a-z]+)?\.(jar|pom)$`)

// locOf extracts and matches a PUT Location's file name.
func locOf(t *testing.T, resp *http.Response) (ts string, n string, clf string, ext string) {
	t.Helper()
	loc := resp.Header.Get("Location")
	m := uniqueNameRegExp.FindStringSubmatch(loc)
	if m == nil {
		t.Fatalf("Location not a unique snapshot spelling: %q", loc)
	}
	return m[1], m[2], m[3], m[4]
}

func mustPut(t *testing.T, hs *harness, repoKey, path string, body []byte) *http.Response {
	t.Helper()
	resp := hs.serve(http.MethodPut, "/"+repoKey+"/"+path, body, nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT %s = %d (%s)", path, resp.StatusCode, drain(t, resp))
	}
	return resp
}

func mustGet(t *testing.T, hs *harness, path string) (int, string) {
	t.Helper()
	resp := hs.serve(http.MethodGet, path, nil, nil, true)
	return resp.StatusCode, string(drain(t, resp))
}

func jarB(s string) []byte { return []byte("snap-bytes-" + s) }

func pomB(v string) []byte {
	return []byte("<project><groupId>com.acme</groupId><artifactId>demo-app</artifactId><version>" +
		v + "</version></project>")
}

// TestSnapshotRewriteWire drives the handler the way a Maven 2 style
// deployer does (raw -SNAPSHOT file names) and pins the wire: rewritten
// Location, no node under the -SNAPSHOT spelling, the metadata arithmetic,
// and the behavior tri-state.
func TestSnapshotRewriteWire(t *testing.T) {
	hs := newHarness(t)
	const dir = "com/acme/demo-app/2.0.0-SNAPSHOT"

	// BUG 1 arms on the DEFAULT repository (config-absent = unique, the
	// L013-4 A6 wire).
	resp := mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("a"))
	ts1, n1, clf1, ext1 := locOf(t, resp)
	if n1 != "1" || clf1 != "" || ext1 != "jar" {
		t.Fatalf("first leader = ts %s N %s clf %q ext %q, want N 1 no classifier jar", ts1, n1, clf1, ext1)
	}

	// the -SNAPSHOT spelling itself never materializes
	if code, _ := mustGet(t, hs, "/maven-local/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar"); code != http.StatusNotFound {
		t.Errorf("GET original -SNAPSHOT name = %d, want 404 (rewritten storage only)", code)
	}
	if code, _ := mustGet(t, hs, "/maven-local/"+dir+"/demo-app-2.0.0-"+ts1+"-1.jar"); code != http.StatusOK {
		t.Errorf("GET rewritten name = %d, want 200", code)
	}

	// follower: the pom joins the leader's trip (same ts, same N)
	resp = mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT.pom", pomB("2.0.0-SNAPSHOT"))
	ts2, n2, _, ext2 := locOf(t, resp)
	if ts2 != ts1 || n2 != "1" || ext2 != "pom" {
		t.Fatalf("follower pom = %s-%s, want %s-1 pom", ts2, n2, ts1)
	}

	// leader second trip: buildNumber +1 (fresh ts when the second differs;
	// same-second re-trip is the idempotent overwrite branch below)
	resp = mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("b"))
	ts3, n3, _, _ := locOf(t, resp)
	if n3 != "2" {
		t.Fatalf("second leader N = %s, want 2", n3)
	}
	if ts3 == ts1 {
		t.Logf("second trip landed in the same second (%s): the coordinate-overwrite branch joined trip 1", ts3)
	}

	// re-deploy of the SAME coordinate with the metadata still on the pom's
	// trip: overwrite in place (reuse the coordinate's ts-N)
	resp = mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("c"))
	ts4, n4, _, _ := locOf(t, resp)
	if ts4 != ts3 || n4 != n3 {
		t.Fatalf("coordinate re-deploy = %s-%s, want %s-%s (in-place overwrite)", ts4, n4, ts3, n3)
	}

	// classifier file: follower — joins the metadata's trip
	resp = mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT-sources.jar", jarB("s"))
	_, n5, clf5, _ := locOf(t, resp)
	if clf5 != "-sources" {
		t.Fatalf("classifier Location = %q, want -sources", clf5)
	}
	if n5 == "0" {
		t.Fatalf("classifier N = 0")
	}

	// metadata arithmetic: the snapshot block tracks the newest unique pom
	code, body := mustGet(t, hs, "/maven-local/"+dir+"/maven-metadata.xml")
	if code != http.StatusOK {
		t.Fatalf("snapdir metadata = %d", code)
	}
	if !strings.Contains(body, "<buildNumber>") {
		t.Errorf("metadata missing snapshot block:\n%s", body)
	}

	// already-unique spelling never rewrites (B3 arm)
	resp = mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-20240819.101500-9.jar", jarB("u"))
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/demo-app-2.0.0-20240819.101500-9.jar") {
		t.Errorf("already-unique Location = %q", loc)
	}

	// non-unique: uploaded name verbatim (spec section 1.3)
	resp = mustPut(t, hs, "maven-nonunique", "com/acme/demo-app/3.0.0-SNAPSHOT/demo-app-3.0.0-SNAPSHOT.jar", jarB("nu"))
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/demo-app-3.0.0-SNAPSHOT.jar") {
		t.Errorf("non-unique Location = %q, want uploaded name", loc)
	}

	// deployer: uploaded name verbatim
	resp = mustPut(t, hs, "maven-deployer", "com/acme/demo-app/3.0.0-SNAPSHOT/demo-app-3.0.0-SNAPSHOT.jar", jarB("d"))
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/demo-app-3.0.0-SNAPSHOT.jar") {
		t.Errorf("deployer Location = %q, want uploaded name", loc)
	}

	// explicit unique repository behaves like the default
	resp = mustPut(t, hs, "maven-unique", "com/acme/demo-app/2.0.0-SNAPSHOT/demo-app-2.0.0-SNAPSHOT.jar", jarB("u1"))
	if _, n, _, _ := locOf(t, resp); n != "1" {
		t.Errorf("explicit unique first N = %s, want 1", n)
	}
}

// TestSnapshotSidecarRewriteAndRegistration: the checksum companion of a
// -SNAPSHOT artifact registers against the REWRITTEN coordinate and
// materializes nothing (BUG 2).
func TestSnapshotSidecarRewriteAndRegistration(t *testing.T) {
	hs := newHarness(t)
	const dir = "com/acme/demo-app/2.0.0-SNAPSHOT"

	resp := mustPut(t, hs, "maven-local", dir+"/demo-app-2.0.0-SNAPSHOT.jar", jarB("a"))
	ts1, n1, _, _ := locOf(t, resp)
	s1, _, _ := digests(jarB("a"))

	// sidecar PUT under the -SNAPSHOT spelling: 201, Location = the
	// REWRITTEN main file, no .sha1 item anywhere in the directory
	resp = hs.serve(http.MethodPut, "/maven-local/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar.sha1", []byte(s1), nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("snapshot sidecar = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "/demo-app-2.0.0-"+ts1+"-"+n1+".jar") {
		t.Errorf("sidecar Location = %q, want the rewritten main file", loc)
	}
	nodes, err := hs.md.Nodes().ListByPrefix(context.Background(), "maven-local", dir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, node := range nodes {
		if strings.HasSuffix(node.Path, ".sha1") || strings.HasSuffix(node.Path, ".md5") {
			t.Errorf("sidecar materialized: %s", node.Path)
		}
	}
	// the sidecar GET answers the computed digest of the REWRITTEN target;
	// the -SNAPSHOT spelling itself addresses a target that never lands
	// (GET plays no name adjustment — the rewritten spelling is the only
	// address, the same wire the reference serves)
	if code, got := mustGet(t, hs, "/maven-local/"+dir+"/demo-app-2.0.0-"+ts1+"-"+n1+".jar.sha1"); code != http.StatusOK || got != s1 {
		t.Errorf("rewritten sidecar GET = %d %q, want 200 %q", code, got, s1)
	}
	if code, _ := mustGet(t, hs, "/maven-local/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar.sha1"); code != http.StatusNotFound {
		t.Errorf("-SNAPSHOT sidecar GET = %d, want 404 (no such target)", code)
	}
	// a wrong value still 409s (the policy gate runs before registration)
	resp = hs.serve(http.MethodPut, "/maven-local/"+dir+"/demo-app-2.0.0-SNAPSHOT.jar.sha1", []byte("deadbeef"), nil, true)
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("wrong snapshot sidecar = %d, want 409", resp.StatusCode)
	}
}

// TestSnapshotRewriteDeterministic drives the adjustment algorithm
// directly with a pinned clock: the trip table the live reference probe
// established (L014-2 probe sequence, 17 observations).
func TestSnapshotRewriteDeterministic(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	p := &auth.Principal{Name: "admin", Admin: true}

	clock := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	calc := newCalculator(hs.svc, hs.md.Nodes(), func() time.Time { return clock })

	rel := func(file string) Layout {
		t.Helper()
		l, err := Parse("com/acme/demo-app/2.0.0-SNAPSHOT/" + file)
		if err != nil {
			t.Fatalf("Parse %s: %v", file, err)
		}
		return l
	}
	adjust := func(file string) string {
		t.Helper()
		return calc.adjustUniqueSnapshot(ctx, p, "maven-local", rel(file))
	}
	land := func(file string, body []byte) {
		t.Helper()
		name := adjust(file)
		path := "com/acme/demo-app/2.0.0-SNAPSHOT/" + name
		if _, err := hs.svc.Put(ctx, p, "maven-local", path, strings.NewReader(string(body)),
			storage.BlobRef{}, "application/octet-stream"); err != nil {
			t.Fatalf("land %s: %v", path, err)
		}
		// the landed unique file fires the sync version-dir recalc, as the
		// adapter chain does
		calc.recalcSync(ctx, p, trigger{repoKey: "maven-local", orgPath: "com.acme",
			module: "demo-app", version: "2.0.0-SNAPSHOT"})
	}

	cases := []struct {
		name string
		file string
		want string
	}{
		{"fresh leader mints (T0,1)", "demo-app-2.0.0-SNAPSHOT.jar", "demo-app-2.0.0-20260913.100000-1.jar"},
		{"pom follows the trip", "demo-app-2.0.0-SNAPSHOT.pom", "demo-app-2.0.0-20260913.100000-1.pom"},
	}
	for _, tc := range cases {
		if got := adjust(tc.file); got != tc.want {
			t.Errorf("%s: adjust = %q, want %q", tc.name, got, tc.want)
		}
		land(tc.file, []byte(tc.name))
	}

	// second trip: metadata now says (T0,1); leader mints (T1,2)
	clock = clock.Add(90 * time.Second)
	if got := adjust("demo-app-2.0.0-SNAPSHOT.jar"); got != "demo-app-2.0.0-20260913.100130-2.jar" {
		t.Fatalf("second trip leader = %q", got)
	}
	land("demo-app-2.0.0-SNAPSHOT.jar", []byte("v2"))

	// metadata still tracks the POM's trip (T0,1): the jar coordinate at
	// N=2 >= candidate 2 → in-place overwrite
	clock = clock.Add(90 * time.Second)
	if got := adjust("demo-app-2.0.0-SNAPSHOT.jar"); got != "demo-app-2.0.0-20260913.100130-2.jar" {
		t.Fatalf("coordinate overwrite = %q, want the existing 100130-2", got)
	}
	land("demo-app-2.0.0-SNAPSHOT.jar", []byte("v3"))

	// classifier joins the METADATA's trip even though the jar sits newer
	if got := adjust("demo-app-2.0.0-SNAPSHOT-javadoc.jar"); got != "demo-app-2.0.0-20260913.100000-1-javadoc.jar" {
		t.Fatalf("follower with lagging metadata = %q, want the metadata trip 100000-1", got)
	}

	// already-unique names pass through untouched
	if got := adjust("demo-app-2.0.0-20240819.101500-9.jar"); got != "demo-app-2.0.0-20240819.101500-9.jar" {
		t.Errorf("already-unique adjusted: %q", got)
	}
}
