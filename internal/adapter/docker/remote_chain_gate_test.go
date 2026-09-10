package docker

// L002-1 (C10, ADR-0047 DigestChainGate): the remote blob pull-through is
// MARKER-GATED, not a digest blind proxy. The refs ledger
// RecordRemoteManifest keeps at manifest-landing time is the structural
// marker; a cold-miss blob GET must be named by a chain of the image to
// reach the upstream, and a digest no chain ever named answers the unfound
// family LOCALLY — zero upstream roundtrips, no negative-cache row (the
// answer is deterministic, ADR-0048). The shapes pinned here:
//
//   - in-chain digests fetch upstream (the Artifactory E4-1 posture);
//   - out-of-chain digests refuse locally with the E6-2 body and a frozen
//     upstream counter (E4-2);
//   - the gate OPENS when the manifest lands afterwards (the C10 replay);
//   - a manifest whose refs never recorded (record failure, best-effort)
//     has its uncached blobs refused — the INTENTIONAL coupling of ADR-0047
//     edge ① (Artifactory's marker-write failure couples identically);
//   - an evicted node with refs standing re-fetches (the ledger memory);
//   - an in-chain digest the upstream no longer serves keeps the negative
//     cache (ADR-0048's boundary: only gate refusals skip the row);
//   - the virtual walk gates per remote member with the plain terminal
//     body (no upstream-summary suffix on a deterministic refusal).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// gateUpstream is one hand-rolled counted OCI registry serving a single
// image: the manifest at every reference spelling, plus the blobs it names
// unless withdrawn (the upstream-404 leg). Every request is journaled —
// the zero-roundtrip proofs read the journal, not a wall clock.
type gateUpstream struct {
	srv       *httptest.Server
	manifest  []byte
	cfg, lay  []byte
	withdrawn map[string]bool

	mu   sync.Mutex
	seen []string
}

// note journals one request path.
func (g *gateUpstream) note(path string) {
	g.mu.Lock()
	g.seen = append(g.seen, path)
	g.mu.Unlock()
}

// blobRoundtrips counts the journaled BLOB requests.
func (g *gateUpstream) blobRoundtrips() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, p := range g.seen {
		if strings.Contains(p, "/blobs/") {
			n++
		}
	}
	return n
}

// requests returns the whole journal (the zero-contact proof).
func (g *gateUpstream) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.seen...)
}

// newChainGateFixture assembles the counted upstream (image "myimg": one
// docker v2 manifest naming config+layer) and a downstream stack whose
// docker-remote points at it. The manifest is NOT pre-pulled — each test
// decides when the chain becomes known.
func newChainGateFixture(t *testing.T) (rs *remotePullStack, up *gateUpstream) {
	t.Helper()
	cfg := []byte(`{"architecture":"amd64","os":"linux","created":"2026-09-11T00:00:00Z"}`)
	lay := []byte("l0021-gate-layer-bytes")
	manifest := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+mediaTypeDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:%s","size":%d},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"sha256:%s","size":%d}]}`,
		sha256Hex(cfg), len(cfg), sha256Hex(lay), len(lay)))
	g := &gateUpstream{manifest: manifest, cfg: cfg, lay: lay, withdrawn: map[string]bool{}}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.note(r.Method + " " + r.URL.Path)
		switch {
		case strings.Contains(r.URL.Path, "/manifests/"):
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.Header().Set("Docker-Content-Digest", "sha256:"+sha256Hex(manifest))
			_, _ = w.Write(manifest)
		case strings.HasSuffix(r.URL.Path, "/blobs/sha256:"+sha256Hex(cfg)) && !g.withdrawn[sha256Hex(cfg)]:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(cfg)
		case strings.HasSuffix(r.URL.Path, "/blobs/sha256:"+sha256Hex(lay)) && !g.withdrawn[sha256Hex(lay)]:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(lay)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(g.srv.Close)

	rs = &remotePullStack{newCatalogStack(t, true)}
	rs.seedDockerRemoteRepo(t, "docker-remote", g.srv.URL)
	return rs, g
}

// pullGateManifest pulls the fixture manifest by tag through the remote
// plane (landing it + recording its refs) and fails the test on anything
// but a clean 200.
func pullGateManifest(t *testing.T, rs *remotePullStack) {
	t.Helper()
	code, body, _ := rs.get("/v2/docker-remote/myimg/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("manifest pull = (%d, %s), want 200", code, body)
	}
}

// decodeSpecError extracts errors[0] from a spec body.
func decodeSpecError(t *testing.T, body string) *specError {
	t.Helper()
	var eb specErrorBody
	if err := json.Unmarshal([]byte(body), &eb); err != nil || len(eb.Errors) != 1 {
		t.Fatalf("body %q: not one spec entry (%v)", body, err)
	}
	return &eb.Errors[0]
}

// assertLocalBlobRefusal pins the E6-2 unfound shape of a gate refusal:
// 404, BLOB_UNKNOWN, the static message, detail.blobSum = the digest — and
// NO upstream-summary suffix (the deterministic answer keeps the body
// plain, the Artifactory E4-2 form).
func assertLocalBlobRefusal(t *testing.T, code int, body, digestParam string) {
	t.Helper()
	if code != http.StatusNotFound {
		t.Fatalf("status = %d (body %s), want the local 404", code, body)
	}
	e := decodeSpecError(t, body)
	if e.Code != ErrCodeBlobUnknown {
		t.Errorf("code = %q, want %q", e.Code, ErrCodeBlobUnknown)
	}
	if e.Message != "blob unknown to registry" {
		t.Errorf("message = %q, want the static E6-2 string (no summary suffix)", e.Message)
	}
	detail, _ := e.Detail.(map[string]any)
	if detail["blobSum"] != digestParam {
		t.Errorf("detail = %v, want blobSum %q", e.Detail, digestParam)
	}
}

// TestChainGateColdDigestPosture: with the chain known (manifest pulled,
// refs recorded), the per-digest posture table — the two in-chain digests
// proxy upstream (one roundtrip each, MISS), the out-of-chain digest
// refuses locally with the journal proving ZERO blob roundtrips and no
// negative-cache row behind the refusal.
func TestChainGateColdDigestPosture(t *testing.T) {
	rs, up := newChainGateFixture(t)
	pullGateManifest(t, rs)
	manifestRoundtrips := up.blobRoundtrips()

	for _, tc := range []struct {
		name     string
		hex      string
		wantBody string
	}{
		{name: "in-chain config digest proxies upstream", hex: sha256Hex(up.cfg), wantBody: string(up.cfg)},
		{name: "in-chain layer digest proxies upstream", hex: sha256Hex(up.lay), wantBody: string(up.lay)},
		{name: "out-of-chain digest refuses locally", hex: sha256Hex([]byte("never-named-by-any-chain"))},
	} {
		code, body, _ := rs.get("/v2/docker-remote/myimg/blobs/sha256:"+tc.hex, nil)
		if tc.wantBody != "" {
			if code != http.StatusOK || body != tc.wantBody {
				t.Errorf("%s = (%d, %d bytes), want the proxied copy", tc.name, code, len(body))
			}
			continue
		}
		assertLocalBlobRefusal(t, code, body, "sha256:"+tc.hex)
	}
	// Precise per-row counters (the table above asserts the bodies; the
	// journal arithmetic is exact here): two proxied blobs = 2 roundtrips.
	if got := up.blobRoundtrips() - manifestRoundtrips; got != 2 {
		t.Fatalf("blob roundtrips = %d, want 2 (the two in-chain fetches only)", got)
	}
	// The gate refusal left no negative row (ADR-0048: deterministic
	// answers need no TTL memory).
	outHex := sha256Hex([]byte("never-named-by-any-chain"))
	if _, err := rs.md.Remote().GetCache(context.Background(), "docker-remote", "myimg/blobs/"+outHex); err == nil {
		t.Fatal("the gate refusal wrote a negative-cache row")
	}
}

// TestChainGateShutUntilManifestLands: the C10 replay shape — a REAL
// digest of the image (the config) is refused locally while no manifest
// chain is known (zero upstream contact, the pre-fix blind-proxy posture
// gone), and the SAME digest fetches upstream the moment a manifest
// naming it lands. The marker's write time is manifest-landing, exactly
// Artifactory's createManifestMarkers timing.
func TestChainGateShutUntilManifestLands(t *testing.T) {
	rs, up := newChainGateFixture(t)
	cfgDigest := "sha256:" + sha256Hex(up.cfg)

	code, body, _ := rs.get("/v2/docker-remote/myimg/blobs/"+cfgDigest, nil)
	assertLocalBlobRefusal(t, code, body, cfgDigest)
	if got := up.requests(); len(got) != 0 {
		t.Fatalf("the shut gate still contacted the upstream: %v", got)
	}

	pullGateManifest(t, rs) // the chain lands — the gate opens
	code, body, hdr := rs.get("/v2/docker-remote/myimg/blobs/"+cfgDigest, nil)
	if code != http.StatusOK || body != string(up.cfg) {
		t.Fatalf("post-manifest blob GET = (%d, %d bytes), want the proxied config", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("post-manifest X-BinFlow-Cache = %q, want MISS (the upstream fetch)", got)
	}
	if up.blobRoundtrips() != 1 {
		t.Fatalf("blob roundtrips = %d, want exactly the one fetch", up.blobRoundtrips())
	}
}

// TestChainGateUnrecordedRefsRefuseBlobs: ADR-0047 edge ① — the refs write
// is best-effort; a manifest whose refs never landed (simulated by
// clearing the set, PutRefs' documented empty-replaces semantics) still
// SERVES, but its uncached blobs are refused locally. The coupling is
// INTENTIONAL (Artifactory's marker-write failure behaves identically).
func TestChainGateUnrecordedRefsRefuseBlobs(t *testing.T) {
	rs, up := newChainGateFixture(t)
	pullGateManifest(t, rs)
	manifestHex := sha256Hex(up.manifest)
	if err := rs.md.Docker().PutRefs(context.Background(), "docker-remote", "myimg", manifestHex, nil); err != nil {
		t.Fatalf("clear refs: %v", err)
	}

	cfgDigest := "sha256:" + sha256Hex(up.cfg)
	code, body, _ := rs.get("/v2/docker-remote/myimg/blobs/"+cfgDigest, nil)
	assertLocalBlobRefusal(t, code, body, cfgDigest)
	if up.blobRoundtrips() != 0 {
		t.Fatalf("unrecorded refs still fetched upstream: %d roundtrips", up.blobRoundtrips())
	}
	// The manifest itself keeps serving (the landed copy stands).
	code, body, _ = rs.get("/v2/docker-remote/myimg/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("manifest after refs clear = (%d, %s), want the cached copy", code, body)
	}
}

// TestChainGateEvictedNodeRefetches: the node-existence short-circuit — a
// blob that landed then lost its node row (eviction) probes MISS, but the
// refs row standing in the ledger re-opens the fetch: the ledger is the
// "evicted, may re-fetch" memory ADR-0047 keeps deliberately.
func TestChainGateEvictedNodeRefetches(t *testing.T) {
	rs, up := newChainGateFixture(t)
	pullGateManifest(t, rs)
	cfgHex := sha256Hex(up.cfg)
	if code, body, _ := rs.get("/v2/docker-remote/myimg/blobs/sha256:"+cfgHex, nil); code != http.StatusOK {
		t.Fatalf("first blob GET = (%d, %s), want 200", code, body)
	}
	if err := rs.md.Nodes().Delete(context.Background(), "docker-remote", "myimg/blobs/"+cfgHex); err != nil {
		t.Fatalf("evict node: %v", err)
	}

	code, body, hdr := rs.get("/v2/docker-remote/myimg/blobs/sha256:"+cfgHex, nil)
	if code != http.StatusOK || body != string(up.cfg) {
		t.Fatalf("post-eviction blob GET = (%d, %d bytes), want the re-fetched copy", code, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("post-eviction X-BinFlow-Cache = %q, want MISS (the refetch)", got)
	}
	if up.blobRoundtrips() != 2 {
		t.Fatalf("blob roundtrips = %d, want 2 (first fetch + refetch)", up.blobRoundtrips())
	}
}

// TestChainGateInChainUpstream404KeepsNegativeCache: ADR-0048's boundary —
// a digest the chain NAMES but the upstream no longer serves pays the one
// upstream roundtrip and then remembers the miss in the negative cache
// (the "declared but gone upstream" anomaly memory); only GATE refusals
// skip the row.
func TestChainGateInChainUpstream404KeepsNegativeCache(t *testing.T) {
	rs, up := newChainGateFixture(t)
	pullGateManifest(t, rs)
	cfgHex := sha256Hex(up.cfg)
	up.withdrawn[cfgHex] = true

	path := "myimg/blobs/" + cfgHex
	code, body, _ := rs.get("/v2/docker-remote/myimg/blobs/sha256:"+cfgHex, nil)
	assertLocalBlobRefusal(t, code, body, "sha256:"+cfgHex)
	if up.blobRoundtrips() != 1 {
		t.Fatalf("in-chain miss roundtrips = %d, want the one upstream ask", up.blobRoundtrips())
	}
	entry, err := rs.md.Remote().GetCache(context.Background(), "docker-remote", path)
	if err != nil {
		t.Fatalf("the in-chain upstream miss left no negative row: %v", err)
	}
	if entry.Kind != "negative" {
		t.Errorf("negative row kind = %q, want \"negative\"", entry.Kind)
	}
	// The second ask answers from the row — the upstream counter freezes.
	code, body, _ = rs.get("/v2/docker-remote/myimg/blobs/sha256:"+cfgHex, nil)
	assertLocalBlobRefusal(t, code, body, "sha256:"+cfgHex)
	if up.blobRoundtrips() != 1 {
		t.Fatalf("negative-cached miss re-asked the upstream: %d roundtrips", up.blobRoundtrips())
	}
}

// TestChainGateVirtualWalkPerMember: the virtual walk's blob arm consults
// the gate per REMOTE member — an out-of-chain digest through the virtual
// answers the plain unfound body (no upstream-summary suffix) with the
// member's upstream journal empty, and an in-chain digest proxies through
// the member after the chain lands.
func TestChainGateVirtualWalkPerMember(t *testing.T) {
	rs, up := newChainGateFixture(t)
	if _, err := rs.svc.CreateRepo(context.Background(), rs.admin, &metadata.Repo{
		RepoKey: "docker-virt", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
		Config: `{"repositories":["docker-remote"]}`,
	}); err != nil {
		t.Fatalf("create virtual: %v", err)
	}

	outDigest := "sha256:" + sha256Hex([]byte("virt-out-of-chain"))
	code, body, _ := rs.serveReq(http.MethodGet, "/v2/docker-virt/myimg/blobs/"+outDigest, nil, nil)
	assertLocalBlobRefusal(t, code, body, outDigest)
	if got := up.requests(); len(got) != 0 {
		t.Fatalf("the virtual walk's shut gate contacted the member upstream: %v", got)
	}

	// The chain lands through the virtual itself; the member's ledger
	// opens, the same digest proxies.
	code, body, _ = rs.serveReq(http.MethodGet, "/v2/docker-virt/myimg/manifests/1.0", nil, acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("virtual manifest pull = (%d, %s), want 200", code, body)
	}
	cfgDigest := "sha256:" + sha256Hex(up.cfg)
	code, body, hdr := rs.serveReq(http.MethodGet, "/v2/docker-virt/myimg/blobs/"+cfgDigest, nil, nil)
	if code != http.StatusOK || body != string(up.cfg) {
		t.Fatalf("virtual blob GET = (%d, %d bytes), want the proxied config", code, len(body))
	}
	if got := hdr.Get("X-Binflow-Resolved-From"); got != "docker-remote" {
		t.Errorf("resolved-from = %q, want the remote member", got)
	}
	if up.blobRoundtrips() != 1 {
		t.Fatalf("member upstream blob roundtrips = %d, want 1", up.blobRoundtrips())
	}
}
