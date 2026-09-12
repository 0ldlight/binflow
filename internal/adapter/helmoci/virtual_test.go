package helmoci

// M13 T-365 (FR-116.2): the HelmOCI virtual aggregation over the real /v2
// plane — a virtual repository over one LOCAL member (its own pushed
// charts) and one REMOTE member (the T-363 pull-through against a second
// BinFlow instance): dual-domain pulls with byte/digest identity, tag
// unions, the catalog's virtual rows, first-seen shadowing, the
// degradation matrix, the write refusal, and the helmoci slot's
// create-plane legs for the virtual class (community 400 / pro 200 /
// uninstall downgrade, plus the Helm-vs-HelmOCI no-mix rule).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// pushChartTo pushes one chart through the /v2 plane of s's repository
// repoKey and returns (manifest, cfg, chart).
func pushChartTo(t *testing.T, s *stack, repoKey, name, version, flavor string) (manifest, cfg, chart []byte) {
	t.Helper()
	cfg = []byte(fmt.Sprintf(`{"name":%q,"version":%q,"apiVersion":"v2"}`, name, version))
	chart = []byte("the chart tgz body " + flavor + " " + name + " " + version)
	body := func(b []byte) {
		t.Helper()
		path := fmt.Sprintf("/v2/%s/%s/blobs/uploads/?digest=sha256:%s", repoKey, name, sha256HexOf(b))
		if status, respBody, _ := s.post(path, b, nil); status != http.StatusCreated {
			t.Fatalf("blob push %s status = %d (body %s)", path, status, respBody)
		}
	}
	body(cfg)
	body(chart)
	manifest = helmManifestBody(cfg, chart, nil)
	status, respBody, _ := s.put(fmt.Sprintf("/v2/%s/%s/manifests/%s", repoKey, name, version), manifest,
		map[string]string{"Content-Type": mtOCIManifest})
	if status != http.StatusCreated {
		t.Fatalf("manifest PUT %s/%s:%s status = %d (body %s)", repoKey, name, version, status, respBody)
	}
	return manifest, cfg, chart
}

// newVirtualFixture assembles the aggregation pair: an UPSTREAM stack
// whose helmoci-local holds chartA, and a DOWNSTREAM stack whose virtual
// "helmoci-virt" aggregates the local member "virt-local" (holding chartB)
// and the remote member "virt-remote" (pointing at the counted upstream).
func newVirtualFixture(t *testing.T) (down, up *stack, wrapped *httptest.Server, hits *atomic.Int64, manifestA, cfgA, chartA []byte) {
	t.Helper()
	up = newStack(t)
	up.seedRepo(t, "helmoci-local", repo.TypeLocal, Protocol)
	manifestA, cfgA, chartA = pushChartTo(t, up, "helmoci-local", "upchart", "0.1.0", "upstream")
	// The union leg wants a second upstream version.
	pushChartTo(t, up, "helmoci-local", "upchart", "0.2.0", "upstream")
	wrapped, counter := countingWrap(up.srv.Config.Handler)
	t.Cleanup(wrapped.Close)
	hits = counter

	down = newStack(t)
	down.seedRepo(t, "virt-local", repo.TypeLocal, Protocol)
	// Registry-root upstream URL (L000-F): the upstream's repoKey rides
	// the image namespace — helmoci-local/upchart.
	down.seedRemoteRepo(t, "virt-remote", wrapped.URL)
	down.seedRepo(t, "helmoci-virt", repo.TypeVirtual, Protocol)
	if err := down.md.Virtual().SetMembers(context.Background(), "helmoci-virt", []string{"virt-local", "virt-remote"}); err != nil {
		t.Fatalf("seed virtual members: %v", err)
	}
	return down, up, wrapped, hits, manifestA, cfgA, chartA
}

// TestVirtualDualDomainChain (AC1): pulls through the virtual serve BOTH
// member domains — the remote member's pull-through (MISS on first touch,
// byte/digest-identical, the upstream counter frozen once cached) and the
// local member's standing copies — plus the by-digest route, the tag
// union, the catalog's virtual rows and the unfound family.
func TestVirtualDualDomainChain(t *testing.T) {
	down, _, _, hits, manifestA, cfgA, chartA := newVirtualFixture(t)
	manifestB, _, chartB := pushChartTo(t, down, "virt-local", "downchart", "0.1.0", "downstream-local")
	pushChartTo(t, down, "virt-local", "helmoci-local/upchart", "0.3.0", "downstream-local")

	accept := map[string]string{"Accept": mtOCIManifest}

	// The REMOTE domain: first touch pulls through into the member's cache.
	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil, accept)
	if status != http.StatusOK || body != string(manifestA) {
		t.Fatalf("virtual pull of the upstream chart = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(manifestA))
	}
	if got := hdr.Get("Docker-Content-Digest"); got != "sha256:"+sha256HexOf(manifestA) {
		t.Errorf("Docker-Content-Digest = %q, want the measured digest", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first virtual pull X-BinFlow-Cache = %q, want MISS", got)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-remote" {
		t.Errorf("X-BinFlow-Resolved-From = %q, want virt-remote", got)
	}
	// The chart's blobs through the virtual: byte-identical (config + layer).
	for _, blob := range [][]byte{cfgA, chartA} {
		status, body, hdr = down.get("/v2/helmoci-virt/helmoci-local/upchart/blobs/sha256:" + sha256HexOf(blob))
		if status != http.StatusOK || body != string(blob) {
			t.Fatalf("virtual blob GET = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(blob))
		}
		if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-remote" {
			t.Errorf("virtual blob X-BinFlow-Resolved-From = %q, want virt-remote", got)
		}
	}

	// The LOCAL domain: the member's standing copy, no cache semantics.
	status, body, hdr = down.do(http.MethodGet, "/v2/helmoci-virt/downchart/manifests/0.1.0", "", "", nil, accept)
	if status != http.StatusOK || body != string(manifestB) {
		t.Fatalf("virtual pull of the local chart = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(manifestB))
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-local" {
		t.Errorf("local-domain X-BinFlow-Resolved-From = %q, want virt-local", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "" {
		t.Errorf("local-domain pull carries X-BinFlow-Cache = %q, want none", got)
	}
	status, body, _ = down.get("/v2/helmoci-virt/downchart/blobs/sha256:" + sha256HexOf(chartB))
	if status != http.StatusOK || body != string(chartB) {
		t.Fatalf("local-domain blob GET = (%d, %d bytes)", status, len(body))
	}

	// The cached second round trip: HIT markers, the upstream frozen.
	afterFirst := hits.Load()
	status, body, hdr = down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil, accept)
	if status != http.StatusOK || body != string(manifestA) {
		t.Fatalf("second virtual pull = (%d, %d bytes)", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("second virtual pull X-BinFlow-Cache = %q, want HIT", got)
	}
	status, body, hdr = down.get("/v2/helmoci-virt/helmoci-local/upchart/manifests/sha256:" + sha256HexOf(manifestA))
	if status != http.StatusOK || body != string(manifestA) {
		t.Fatalf("by-digest virtual pull = (%d, %d bytes)", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Errorf("by-digest X-BinFlow-Cache = %q, want HIT over the cached state", got)
	}
	if hits.Load() != afterFirst {
		t.Fatalf("cached virtual round trip contacted the upstream: %d -> %d", afterFirst, hits.Load())
	}

	// The tag UNION across both domains of the same image name: the remote
	// member's CACHED 0.1.0 plus the local member's 0.3.0. The upstream's
	// 0.2.0 stays invisible by design (T-363 D-2: a remote member's tag
	// set is its cached rows — no upstream tags/list proxying; helm pulls
	// with --version never consult it).
	status, body, _ = down.get("/v2/helmoci-virt/helmoci-local/upchart/tags/list")
	if status != http.StatusOK {
		t.Fatalf("virtual tags/list status = %d (body %s)", status, body)
	}
	for _, tag := range []string{"0.1.0", "0.3.0"} {
		if !strings.Contains(body, `"`+tag+`"`) {
			t.Errorf("virtual tags/list %q does not name %q", body, tag)
		}
	}
	if strings.Contains(body, `"0.2.0"`) {
		t.Errorf("virtual tags/list %q names the uncached upstream tag 0.2.0 — the D-2 cached-rows posture was violated", body)
	}

	// The catalog names the virtual's aggregated images (the memberless
	// read of a virtual row used to fault the catalog — T-365's flip).
	status, body, _ = down.do(http.MethodGet, "/v2/_catalog", adminUser, adminPass, nil, nil)
	if status != http.StatusOK {
		t.Fatalf("catalog with a virtual row status = %d (body %s)", status, body)
	}
	for _, name := range []string{"helmoci-virt/helmoci-local/upchart", "helmoci-virt/downchart"} {
		if !strings.Contains(body, `"`+name+`"`) {
			t.Errorf("catalog %q does not name %q", body, name)
		}
	}

	// The unfound family: no member answers.
	status, body, _ = down.get("/v2/helmoci-virt/nochart/manifests/1.0.0")
	if status != http.StatusNotFound || !strings.Contains(body, "MANIFEST_UNKNOWN") {
		t.Errorf("unknown chart through the virtual = (%d, %s), want the 404 spec shape", status, body)
	}
	status, body, _ = down.get("/v2/helmoci-virt/nochart/blobs/sha256:" + sha256HexOf([]byte("x")))
	if status != http.StatusNotFound || !strings.Contains(body, "BLOB_UNKNOWN") {
		t.Errorf("unknown blob through the virtual = (%d, %s), want the 404 spec shape", status, body)
	}

	// Writes refuse with the service-rendered 405 (the C5 spelling
	// un-routed) — the read walk did not open a deploy path.
	status, body, hdr = down.put("/v2/helmoci-virt/helmoci-local/upchart/manifests/9.9.9", manifestA,
		map[string]string{"Content-Type": mtOCIManifest})
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("virtual manifest PUT status = %d, want 405 (body %s)", status, body)
	}
	if allow := hdr.Get("Allow"); allow != http.MethodGet {
		t.Errorf("virtual PUT Allow = %q, want GET", allow)
	}
	if !strings.Contains(body, "No local repository was configured as local deployment repository") {
		t.Errorf("virtual PUT body %q does not carry the C5 spelling", body)
	}
	status, body, hdr = down.post("/v2/helmoci-virt/helmoci-local/upchart/blobs/uploads/", nil, nil)
	if status != http.StatusMethodNotAllowed || hdr.Get("Allow") != http.MethodGet {
		t.Fatalf("virtual upload POST = (%d, Allow %q), want the 405/GET pair (body %s)", status, hdr.Get("Allow"), body)
	}
}

// TestVirtualFirstSeenShadowing (AC1): the same tag on two members — the
// two-bucket order decides (first-seen semantics), and a member reorder
// moves the winner.
func TestVirtualFirstSeenShadowing(t *testing.T) {
	down, _, _, _, manifestUp, _, _ := newVirtualFixture(t)
	// The local member shadows the upstream's 0.1.0 with its own body.
	manifestLocal, _, _ := pushChartTo(t, down, "virt-local", "helmoci-local/upchart", "0.1.0", "shadowing-local")
	if string(manifestLocal) == string(manifestUp) {
		t.Fatal("the shadow fixture built identical manifests — the test cannot discriminate")
	}

	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifestLocal) {
		t.Fatalf("shadowed pull = (%d, %d bytes), want the local member's %d bytes", status, len(body), len(manifestLocal))
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-local" {
		t.Errorf("shadowed X-BinFlow-Resolved-From = %q, want virt-local (declaration order)", got)
	}

	// Reorder the members: the remote member leads, its pull-through wins.
	if err := down.md.Virtual().SetMembers(context.Background(), "helmoci-virt", []string{"virt-remote", "virt-local"}); err != nil {
		t.Fatalf("reorder members: %v", err)
	}
	status, body, hdr = down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifestUp) {
		t.Fatalf("reordered pull = (%d, %d bytes), want the remote member's %d bytes", status, len(body), len(manifestUp))
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "virt-remote" {
		t.Errorf("reordered X-BinFlow-Resolved-From = %q, want virt-remote", got)
	}
}

// TestVirtualDegradationMatrix (FR-116.5 through the aggregation): the
// upstream gone and the cached copy expired — the virtual still serves
// (STALE + the marker), an uncached reference answers the unfound family,
// and nothing answers a naked 5xx.
func TestVirtualDegradationMatrix(t *testing.T) {
	down, _, wrapped, _, manifestA, _, chartA := newVirtualFixture(t)

	status, body, _ := down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifestA) {
		t.Fatalf("preheat pull = (%d, %d bytes)", status, len(body))
	}
	// Expire the member's cached manifest and layers.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	for _, path := range []string{
		"helmoci-local/upchart/manifests/" + sha256HexOf(manifestA),
		"helmoci-local/upchart/blobs/" + sha256HexOf(chartA),
	} {
		if err := down.md.Remote().PutCache(context.Background(), &metadata.RemoteCacheEntry{
			RepoKey: "virt-remote", Path: path, Kind: metadata.RemoteCacheKindContent,
			FetchedAt: past, ExpiresAt: past,
		}); err != nil {
			t.Fatalf("expire %s: %v", path, err)
		}
	}
	wrapped.Close()

	status, body, hdr := down.do(http.MethodGet, "/v2/helmoci-virt/helmoci-local/upchart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifestA) {
		t.Fatalf("degraded virtual pull = (%d, %d bytes), want the stale copy", status, len(body))
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "STALE" {
		t.Errorf("degraded X-BinFlow-Cache = %q, want STALE", got)
	}
	if got := hdr.Get("X-Binflow-Upstream-Error"); got == "" {
		t.Error("degraded serve carries no X-Binflow-Upstream-Error marker")
	}

	// Uncached against the dead upstream: the unfound family (the local
	// member does not hold the chart either — the walk exhausts).
	status, body, _ = down.get("/v2/helmoci-virt/helmoci-local/upchart/manifests/5.5.5")
	if status != http.StatusNotFound || !strings.Contains(body, "MANIFEST_UNKNOWN") {
		t.Errorf("uncached degraded virtual pull = (%d, %s), want the 404 spec shape", status, body)
	}
}

// TestVirtualCreateGates (AC2): the helmoci slot's create-plane legs for
// the VIRTUAL class — the community D3 400 naming helmoci, the pro 200
// with the member ledger landed, the uninstall downgrade, and the
// Helm/HelmOCI no-mix rule holding on both directions.
func TestVirtualCreateGates(t *testing.T) {
	s, keys := newLicensedStack(t)
	ctx := context.Background()
	create := func(body string) (int, string) {
		status, respBody, _ := s.do(http.MethodPut, "/binflow/api/repositories/"+keyOf(body), adminUser, adminPass,
			strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
		return status, respBody
	}

	virtualBody := `{"key":"helmoci-virt","rclass":"virtual","packageType":"helmoci","repositories":["later-local","later-remote"]}`

	// Community: the D3 refusal names helmoci (the slot question fires
	// before the member existence check).
	status, body := create(virtualBody)
	if status != http.StatusBadRequest {
		t.Fatalf("community helmoci virtual create = (%d, %s), want the D3 400", status, body)
	}
	for _, token := range []string{"helmoci", "pro"} {
		if !strings.Contains(body, token) {
			t.Errorf("D3 body %q does not name %q", body, token)
		}
	}

	// Pro: the members land first, then the virtual aggregates them.
	if _, err := s.license.Install(ctx, proDoc(t, keys)); err != nil {
		t.Fatalf("install pro: %v", err)
	}
	if status, body := create(`{"key":"later-local","rclass":"local","packageType":"helmoci"}`); status != http.StatusOK {
		t.Fatalf("pro local create = (%d, %s)", status, body)
	}
	if status, body := create(`{"key":"later-remote","rclass":"remote","packageType":"helmoci","url":"http://127.0.0.1:9/v2/up"}`); status != http.StatusOK {
		t.Fatalf("pro remote create = (%d, %s)", status, body)
	}
	status, body = create(virtualBody)
	if status != http.StatusOK {
		t.Fatalf("pro helmoci virtual create = (%d, %s), want 200", status, body)
	}
	row, err := s.md.Repos().Get(ctx, "helmoci-virt")
	if err != nil || row.Type != repo.TypeVirtual || row.PackageType != Protocol {
		t.Fatalf("virtual row = (%v, %s/%s), want (virtual, helmoci)", err, row.Type, row.PackageType)
	}
	members, err := s.md.Virtual().ListMembers(ctx, "helmoci-virt")
	if err != nil || len(members) != 2 || members[0].MemberRepo != "later-local" || members[1].MemberRepo != "later-remote" {
		t.Fatalf("virtual members = (%v, %+v), want the declared order", err, members)
	}

	// The no-mix rule (M12's boundary, unchanged): a helm member refuses a
	// helmoci virtual, and a helmoci member refuses a helm virtual — in
	// BOTH directions, with the family wording.
	if status, body := create(`{"key":"t365-helm","rclass":"local","packageType":"helm"}`); status != http.StatusOK {
		t.Fatalf("pro helm local create = (%d, %s)", status, body)
	}
	status, body = create(`{"key":"helmoci-mixed","rclass":"virtual","packageType":"helmoci","repositories":["t365-helm"]}`)
	if status != http.StatusBadRequest || !strings.Contains(body, "cannot mix the Helm and HelmOCI") {
		t.Fatalf("helmoci virtual with a helm member = (%d, %s), want the mix 400", status, body)
	}
	status, body = create(`{"key":"helm-mixed","rclass":"virtual","packageType":"helm","repositories":["later-local"]}`)
	if status != http.StatusBadRequest || !strings.Contains(body, "cannot mix the Helm and HelmOCI") {
		t.Fatalf("helm virtual with a helmoci member = (%d, %s), want the mix 400", status, body)
	}

	// T-431 opened the docker virtual cell on the matrix, so this mix now
	// meets the same-type member rule instead: a helmoci member refuses a
	// docker virtual (the family-shared /v2 plane keeps single-type member
	// sets, T-367's rider).
	status, body = create(`{"key":"docker-virt","rclass":"virtual","packageType":"docker","repositories":["later-local"]}`)
	if status != http.StatusBadRequest || !strings.Contains(body, "cannot mix the docker and helmoci package types") {
		t.Fatalf("docker virtual with a helmoci member = (%d, %s), want the mix 400", status, body)
	}

	// Uninstall: the create face returns to the D3 400.
	if err := s.license.Uninstall(ctx); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	status, body = create(`{"key":"helmoci-virt2","rclass":"virtual","packageType":"helmoci","repositories":["later-local"]}`)
	if status != http.StatusBadRequest || !strings.Contains(body, "helmoci") {
		t.Fatalf("post-uninstall helmoci virtual create = (%d, %s), want the D3 400", status, body)
	}
}

// keyOf digs the repository key out of one create body (the route needs it
// in the path too).
func keyOf(body string) string {
	const needle = `"key":`
	i := strings.Index(body, needle)
	if i < 0 {
		return "unknown"
	}
	rest := body[i+len(needle):]
	start := strings.Index(rest, `"`)
	end := strings.Index(rest[start+1:], `"`)
	if start < 0 || end < 0 {
		return "unknown"
	}
	return rest[start+1 : start+1+end]
}
