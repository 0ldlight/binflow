package rpm

// The REMOTE repository's full-stack tests (rpm.md section 6's remote
// row): the pull-through over a loopback origin (this stack's own local
// repository — the "本地起源" the dispatch note allows), the expirable-set
// cache behavior the S10 classification drives, the write posture, and
// the .rpm property backfill.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// remoteFixture is one stack whose LOCAL rpm-org repository is the remote
// rpm-r's upstream origin.
type remoteFixture struct {
	*stack
	origin string // the origin's base wire path ("/binflow/rpm-org")
	rkey   string
}

// newRemoteFixture assembles the origin (indexed, one package) and the
// remote mirror over it.
func newRemoteFixture(t *testing.T) *remoteFixture {
	t.Helper()
	s := newStack(t)
	s.seedRepo(t, "rpm-org", repo.TypeLocal, "{}")
	s.seedRemoteRepo(t, "rpm-r", s.srv.URL+"/binflow/rpm-org")
	f := &remoteFixture{stack: s, origin: "/binflow/rpm-org", rkey: "rpm-r"}
	pkg := pkgFixture("hello", "1.0", "1", "noarch")
	if status, body, _ := s.put(f.origin+"/hello-1.0-1.noarch.rpm", pkg, nil); status != 201 {
		t.Fatalf("origin PUT = (%d, %s)", status, body)
	}
	if status, body, _ := s.post("/binflow/api/yum/rpm-org?async=0"); status != 200 {
		t.Fatalf("origin reindex = (%d, %s)", status, body)
	}
	return f
}

// TestRemotePullThroughRepodata: repomd and the digest-named indexes
// fetch through the engine (MISS then HIT, one upstream request each),
// byte-identical to the origin's.
func TestRemotePullThroughRepodata(t *testing.T) {
	f := newRemoteFixture(t)

	_, originRepomd, _ := f.get(f.origin + "/repodata/repomd.xml")
	status, repomd, hdr := f.get("/binflow/" + f.rkey + "/repodata/repomd.xml")
	if status != 200 || repomd != originRepomd {
		t.Fatalf("remote repomd = (%d, %d bytes) vs origin %d bytes", status, len(repomd), len(originRepomd))
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("first repomd fetch cache state = %q, want MISS", hdr.Get("X-BinFlow-Cache"))
	}
	if status, _, hdr = f.get("/binflow/" + f.rkey + "/repodata/repomd.xml"); status != 200 || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second repomd fetch = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}

	// The digest-named index the repomd points at (artifact-semantics
	// class under S10): fetched, cached, checksum-reconciled.
	href, err := primaryHrefOf(repomd)
	if err != nil {
		t.Fatal(err)
	}
	_, originPrimary, _ := f.get(f.origin + "/" + href)
	status, primary, hdr := f.get("/binflow/" + f.rkey + "/" + href)
	if status != 200 || primary != originPrimary {
		t.Fatalf("remote primary = (%d, %d bytes) vs origin %d bytes", status, len(primary), len(originPrimary))
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("first primary fetch cache state = %q, want MISS", hdr.Get("X-BinFlow-Cache"))
	}
	if _, _, hdr = f.get("/binflow/" + f.rkey + "/" + href); hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Errorf("second primary fetch cache state = %q, want HIT", hdr.Get("X-BinFlow-Cache"))
	}

	// The remote's repodata is the origin's VERBATIM — never recomputed
	// locally (section 6's remote row closing rule).
	if status, again, _ := f.get("/binflow/" + f.rkey + "/repodata/repomd.xml"); status != 200 || again != originRepomd {
		t.Error("remote repomd drifted from the upstream copy (local recompute must not run)")
	}
}

// TestRemotePullThroughRpm: the artifact face — MISS, HIT, checksum
// headers, and the honest 404 for an upstream miss.
func TestRemotePullThroughRpm(t *testing.T) {
	f := newRemoteFixture(t)
	_, originPkg, _ := f.get(f.origin + "/hello-1.0-1.noarch.rpm")

	status, body, hdr := f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm")
	if status != 200 || sha256Hex([]byte(body)) != sha256Hex([]byte(originPkg)) {
		t.Fatalf("remote rpm = (%d, %d bytes), want the upstream bytes", status, len(body))
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex([]byte(originPkg)) {
		t.Errorf("rpm X-Checksum-Sha256 disagrees")
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("first rpm fetch cache state = %q", hdr.Get("X-BinFlow-Cache"))
	}
	if _, _, hdr = f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Errorf("second rpm fetch cache state = %q, want HIT", hdr.Get("X-BinFlow-Cache"))
	}
	if status, _, _ = f.get("/binflow/" + f.rkey + "/absent-9.9-1.noarch.rpm"); status != 404 {
		t.Errorf("absent rpm = %d, want 404", status)
	}
	// The checksum sidecar never proxies (section 3.1's remote arm).
	if status, body, _ = f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm.sha256"); status != 404 || !strings.Contains(body, "Checksums are not downloadable.") {
		t.Errorf("remote sidecar = (%d, %s), want the pinned 404", status, body)
	}
}

// TestRemotePropertyBackfill: the landed copy's header parses
// asynchronously and the rpm.metadata.* set reaches the node (the §6
// remote step 3 chain).
func TestRemotePropertyBackfill(t *testing.T) {
	f := newRemoteFixture(t)
	if status, _, _ := f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); status != 200 {
		t.Fatalf("rpm fetch = %d", status)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		props, err := f.md.NodeProps().List(t.Context(), f.rkey, "hello-1.0-1.noarch.rpm")
		if err == nil {
			if got := props["rpm.metadata.name"]; len(got) == 1 && got[0] == "hello" {
				if arch := props["rpm.metadata.arch"]; len(arch) != 1 || arch[0] != "noarch" {
					t.Errorf("rpm.metadata.arch = %v", arch)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rpm.metadata.* never landed: %+v (err %v)", props, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestRemoteWritesRefused: PUT meets the engine-wide RE-05 read-only 405
// on every face; DELETE rides the RE-06 cache-eviction verb.
func TestRemoteWritesRefused(t *testing.T) {
	f := newRemoteFixture(t)
	pkg := pkgFixture("nope", "1.0", "1", "noarch")

	status, body, hdr := f.put("/binflow/"+f.rkey+"/nope-1.0-1.noarch.rpm", pkg, nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy") {
		t.Fatalf("remote PUT = (%d, %s), want the read-only 405", status, body)
	}
	if hdr.Get("Allow") != "GET" {
		t.Errorf("remote PUT Allow = %q", hdr.Get("Allow"))
	}
	// The generated family routes onto the same refusal (not the local
	// server-generated 403 — the remote's repodata is the upstream's).
	if status, body, _ = f.put("/binflow/"+f.rkey+"/repodata/repomd.xml", []byte("x"), nil); status != http.StatusMethodNotAllowed {
		t.Fatalf("remote repomd PUT = (%d, %s), want 405", status, body)
	}

	// DELETE: 404 with nothing cached; 204 after a copy lands — and the
	// next read refetches upstream.
	if status, _, _ = f.delete("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); status != 404 {
		t.Fatalf("uncached DELETE = %d, want 404", status)
	}
	if status, _, _ = f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); status != 200 {
		t.Fatalf("rpm fetch = %d", status)
	}
	if status, _, _ = f.delete("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); status != 204 {
		t.Fatalf("cached DELETE = %d, want 204", status)
	}
	if status, _, hdr := f.get("/binflow/" + f.rkey + "/hello-1.0-1.noarch.rpm"); status != 200 || hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Fatalf("post-eviction fetch = (%d, cache %q), want 200/MISS", status, hdr.Get("X-BinFlow-Cache"))
	}
}
