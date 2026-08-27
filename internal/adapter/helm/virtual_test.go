package helm

// The VIRTUAL repository's full-stack wire tests (helm.md section 7): the
// aggregated index.yaml with the S8 URL rewriting, the HEAD membership
// probe, the sub-path index refusal, the member-resolved downloads, the
// dependency-proxy faces and the write routing.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// upstreamIndexFor renders one upstream index.yaml exercising the S8
// branches: a charts-base-aligned absolute URL, an external URL (a host
// OUTSIDE the charts base), the upstream's own _external spelling, an
// oci:// reference and a relative URL.
func upstreamIndexFor(base, extDepURL, viaUpstreamURL string, chartTgz []byte) string {
	return "apiVersion: v1\nentries:\n" +
		"  aligned:\n  - name: aligned\n    version: \"1.0.0\"\n    digest: " + sha256Hex(chartTgz) + "\n    urls:\n    - " + base + "/aligned-1.0.0.tgz\n" +
		"  extdep:\n  - name: extdep\n    version: \"1.0.0\"\n    urls:\n    - " + extDepURL + "\n" +
		"  viaupstream:\n  - name: viaupstream\n    version: \"1.0.0\"\n    urls:\n    - " + viaUpstreamURL + "\n" +
		"  ocichart:\n  - name: ocichart\n    version: \"1.0.0\"\n    urls:\n    - oci://registry.example.com/charts/ocichart\n" +
		"  relchart:\n  - name: relchart\n    version: \"1.0.0\"\n    urls:\n    - relchart-1.0.0.tgz\n"
}

func TestVirtualIndexAggregationAndRewrite(t *testing.T) {
	s := newStack(t)
	up := newChartUpstream(t, map[string]string{})
	chart := fixtureChart(t, "aligned", defaultChartYAML("aligned", "1.0.0"), nil)
	// The EXTERNAL host: a second loopback server outside the member's
	// charts base (same-host URLs would be base-aligned, the path branch).
	ext := newChartUpstream(t, map[string]string{})
	extDepURL := ext.srv.URL + "/deps/extdep-1.0.0.tgz"
	viaUpstreamURL := up.srv.URL + "/_external/http/" + hostOf(ext.srv.URL) + "/deps/extdep-1.0.0.tgz"
	up.files["/index.yaml"] = upstreamIndexFor(up.srv.URL, extDepURL, viaUpstreamURL, chart)
	up.files["/aligned-1.0.0.tgz"] = string(chart)
	up.files["/relchart-1.0.0.tgz"] = string(fixtureChart(t, "relchart", defaultChartYAML("relchart", "1.0.0"), nil))

	// One local member with two charts (one shadowed by the remote member's
	// same name+version — first-wins must keep the LOCAL entry: local is
	// declared first).
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	for _, spec := range []struct{ name, version string }{
		{"localchart", "1.0.0"}, {"localchart", "2.0.0"}, {"aligned", "1.0.0"},
	} {
		c := fixtureChart(t, spec.name, defaultChartYAML(spec.name, spec.version), nil)
		if status, body, _ := s.put(fmt.Sprintf("/binflow/helm-l/%s-%s.tgz", spec.name, spec.version), c, nil); status != http.StatusCreated {
			t.Fatalf("PUT %s-%s = (%d, %s)", spec.name, spec.version, status, body)
		}
	}
	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	s.seedVirtualRepo(t, "helm-v", "", "helm-l", "helm-r")

	status, body, hdr := s.get("/binflow/helm-v/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("virtual index = (%d, %s), want 200", status, body)
	}
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "text/yaml") {
		t.Errorf("virtual index Content-Type = %q", ct)
	}
	// The S8 branches (urls[0] after the rewrite):
	for _, want := range []string{
		// local member entries keep their in-repo path (relative mode);
		"- localchart-1.0.0.tgz",
		"- localchart-2.0.0.tgz",
		// remote member, charts-base aligned: the member-relative path.
		"- aligned-1.0.0.tgz",
		// external (outside the charts base), allow-list hit: the folded
		// _external form — the "://" collapses onto "/".
		"- _external/http/" + hostOf(ext.srv.URL) + "/deps/extdep-1.0.0.tgz",
		// the upstream's own _external face: _transitive (the upstream's
		// _external spelling collapses onto the transitive prefix).
		"- _transitive/http/" + hostOf(ext.srv.URL) + "/deps/extdep-1.0.0.tgz",
		// oci:// entries ride verbatim; relative upstream urls are the
		// member-relative path.
		"- oci://registry.example.com/charts/ocichart",
		"- relchart-1.0.0.tgz",
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("virtual index missing the rewritten url %q:\n%s", want, body)
		}
	}
	// Newest-first within a chart.
	if strings.Index(body, "localchart-2.0.0") > strings.Index(body, "localchart-1.0.0") {
		t.Errorf("versions not newest-first:\n%s", body)
	}
	// First-wins: the local member's aligned entry (its digest) shadows the
	// remote member's (the digest field reconciles with the LOCAL bytes —
	// resolution serves the local member's chart).
	if !strings.Contains(body, "digest: "+sha256Hex(mustChart(t, "aligned", "1.0.0"))) {
		t.Errorf("first-wins: the local member's aligned digest lost:\n%s", body)
	}
	// The virtual download plane: the local member's chart serves with the
	// resolution hint; the remote member's chart pulls through.
	if status, _, hdr = s.get("/binflow/helm-v/aligned-1.0.0.tgz"); status != http.StatusOK || hdr.Get("X-BinFlow-Resolved-From") != "helm-l" {
		t.Errorf("virtual chart (local winner) = (%d, from %q), want helm-l", status, hdr.Get("X-BinFlow-Resolved-From"))
	}
	if status, _, hdr = s.get("/binflow/helm-v/relchart-1.0.0.tgz"); status != http.StatusOK || hdr.Get("X-BinFlow-Resolved-From") != "helm-r" {
		t.Errorf("virtual chart (remote member) = (%d, from %q), want helm-r", status, hdr.Get("X-BinFlow-Resolved-From"))
	}
}

// mustChart rebuilds one fixture for digest reconciliation.
func mustChart(t *testing.T, name, version string) []byte {
	t.Helper()
	return fixtureChart(t, name, defaultChartYAML(name, version), nil)
}

// hostOf strips the scheme off one test URL (the folded path form).
func hostOf(u string) string { return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://") }

func TestVirtualHeadAndRootRules(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "c", defaultChartYAML("c", "1.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-l/c-1.0.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("PUT = (%d, %s)", status, body)
	}
	s.seedVirtualRepo(t, "helm-v", "", "helm-l")

	// HEAD: members exist → 200, empty body (the cheap probe).
	status, body, hdr := s.do(http.MethodHead, "/binflow/helm-v/index.yaml", "", "", nil, nil)
	if status != http.StatusOK || body != "" {
		t.Fatalf("virtual HEAD = (%d, %q), want 200 with no body", status, body)
	}
	if hdr.Get("Content-Type") != "" {
		t.Errorf("HEAD carries Content-Type %q", hdr.Get("Content-Type"))
	}
	// A member-less virtual: HEAD 404, GET 404.
	s.seedRepo(t, "helm-empty", repo.TypeVirtual, "{}")
	if status, _, _ = s.do(http.MethodHead, "/binflow/helm-empty/index.yaml", "", "", nil, nil); status != http.StatusNotFound {
		t.Errorf("member-less HEAD = %d, want 404", status)
	}
	if status, _, _ = s.get("/binflow/helm-empty/index.yaml"); status != http.StatusNotFound {
		t.Errorf("member-less GET = %d, want 404", status)
	}
	// The sub-path index request: the pinned refusal (section 2).
	status, body, _ = s.get("/binflow/helm-v/sub/index.yaml")
	if status != http.StatusNotFound || !strings.Contains(body, "unsupported location") {
		t.Fatalf("sub-path index = (%d, %s), want the pinned 404", status, body)
	}
	// Index writes are the server-generated refusal on every class.
	if status, _, _ = s.put("/binflow/helm-v/index.yaml", []byte("apiVersion: v1\n"), nil); status != http.StatusForbidden {
		t.Errorf("virtual index PUT = %d, want 403", status)
	}
	// The api/helm alias serves the aggregate read-only.
	if status, _, _ = s.get("/binflow/api/helm/helm-v/index.yaml"); status != http.StatusOK {
		t.Errorf("alias virtual index = %d, want 200", status)
	}
}

func TestVirtualWriteRouting(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	up := newChartUpstream(t, map[string]string{})
	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	s.seedVirtualRepo(t, "helm-v", "helm-l", "helm-l", "helm-r")

	// PUT through the virtual lands in the deployment member AND the
	// member's own index gains the entry (the recompute follows the
	// service's routing, node.RepoKey).
	chart := fixtureChart(t, "routed", defaultChartYAML("routed", "1.0.0"), nil)
	status, body, hdr := s.put("/binflow/helm-v/routed-1.0.0.tgz", chart, nil)
	if status != http.StatusCreated {
		t.Fatalf("virtual PUT = (%d, %s), want 201", status, body)
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex(chart) {
		t.Errorf("virtual PUT checksum = %q", hdr.Get("X-Checksum-Sha256"))
	}
	if status, body, _ = s.get("/binflow/helm-l/index.yaml"); status != http.StatusOK || !strings.Contains(body, "routed-1.0.0.tgz") {
		t.Fatalf("member index after the routed write = (%d, %s)", status, body)
	}
	// The virtual read plane serves it back.
	if status, body, _ = s.get("/binflow/helm-v/routed-1.0.0.tgz"); status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Errorf("virtual read-back = %d, want the routed bytes", status)
	}
	// DELETE never propagates (C5): the truthful routed wording.
	if status, body, _ = s.delete("/binflow/helm-v/routed-1.0.0.tgz"); status != http.StatusMethodNotAllowed || !strings.Contains(body, "not propagated") {
		t.Fatalf("virtual DELETE = (%d, %s), want the C5 405", status, body)
	}
	// A .prov sidecar routes the same way.
	prov := "-----BEGIN PGP SIGNED MESSAGE-----\nprov\n"
	if status, _, _ = s.put("/binflow/helm-v/routed-1.0.0.tgz.prov", []byte(prov), nil); status != http.StatusCreated {
		t.Errorf("virtual prov PUT = %d, want 201", status)
	}
	if status, body, _ = s.get("/binflow/helm-v/routed-1.0.0.tgz.prov"); status != http.StatusOK || body != prov {
		t.Errorf("virtual prov read = (%d, %q)", status, body)
	}
}

func TestVirtualUnroutedWriteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	s.seedVirtualRepo(t, "helm-v", "", "helm-l")
	chart := fixtureChart(t, "x", defaultChartYAML("x", "1.0.0"), nil)
	status, body, hdr := s.put("/binflow/helm-v/x-1.0.0.tgz", chart, nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "No local repository was configured") {
		t.Fatalf("un-routed virtual PUT = (%d, %s), want the C5 405", status, body)
	}
	if hdr.Get("Allow") != "GET" {
		t.Errorf("un-routed PUT Allow = %q", hdr.Get("Allow"))
	}
}

func TestVirtualExternalAndTransitive(t *testing.T) {
	s := newStack(t)
	dep := fixtureChart(t, "extdep", defaultChartYAML("extdep", "1.0.0"), nil)
	up := newChartUpstream(t, map[string]string{
		"/deps/extdep-1.0.0.tgz": string(dep),
	})
	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	s.seedVirtualRepo(t, "helm-v", "", "helm-r")

	// _external through the virtual: the (only) remote member egresses.
	wire := "/binflow/helm-v/_external/http/" + hostOf(up.srv.URL) + "/deps/extdep-1.0.0.tgz"
	status, body, hdr := s.get(wire)
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(dep) {
		t.Fatalf("virtual _external = (%d, %d bytes)", status, len(body))
	}
	if hdr.Get("X-Binflow-Upstream") != up.srv.URL+"/deps/extdep-1.0.0.tgz" {
		t.Errorf("virtual _external upstream = %q", hdr.Get("X-Binflow-Upstream"))
	}

	// _transitive through the virtual: the member is read at the upstream
	// _external storage path — the upstream serves it as a plain path here.
	transitiveDep := fixtureChart(t, "viadeps", defaultChartYAML("viadeps", "1.0.0"), nil)
	up.files["/_external/http/"+hostOf(up.srv.URL)+"/deps/viadeps-1.0.0.tgz"] = string(transitiveDep)
	wire = "/binflow/helm-v/_transitive/http/" + hostOf(up.srv.URL) + "/deps/viadeps-1.0.0.tgz"
	if status, body, _ = s.get(wire); status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(transitiveDep) {
		t.Fatalf("virtual _transitive = (%d, %d bytes)", status, len(body))
	}

	// A virtual over LOCAL members only has no egress face: the pinned 400.
	s.seedRepo(t, "helm-l2", repo.TypeLocal, "{}")
	s.seedVirtualRepo(t, "helm-vl", "", "helm-l2")
	if status, body, _ = s.get("/binflow/helm-vl/_external/http/" + hostOf(up.srv.URL) + "/deps/extdep-1.0.0.tgz"); status != http.StatusBadRequest || !strings.Contains(body, "not configured as an external dependency") {
		t.Fatalf("local-only virtual _external = (%d, %s), want the pinned 400", status, body)
	}
}

func TestVirtualMemberFailureDoesNotBlockOthers(t *testing.T) {
	s := newStack(t)
	// One healthy local member and one remote member whose upstream is
	// down (connection refused — the loopback server is closed upfront).
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "healthy", defaultChartYAML("healthy", "1.0.0"), nil)
	if status, body, _ := s.put("/binflow/helm-l/healthy-1.0.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("PUT = (%d, %s)", status, body)
	}
	dead := newChartUpstream(t, nil)
	deadURL := dead.srv.URL
	dead.srv.Close()
	s.seedRemoteRepo(t, "helm-r", deadURL)
	s.seedVirtualRepo(t, "helm-v", "", "helm-l", "helm-r")

	// The dead remote member is an unfound miss (offline, no copy) — the
	// local member's entries still answer.
	status, body, _ := s.get("/binflow/helm-v/index.yaml")
	if status != http.StatusOK || !strings.Contains(body, "healthy-1.0.0.tgz") {
		t.Fatalf("aggregate with a dead member = (%d, %s), want the local entries", status, body)
	}

	// ALL members failing: the aggregate is the honest 404 (no member
	// contributed anything; the dead member's unfound answer is a quiet
	// miss, not a surfaced failure).
	s.seedVirtualRepo(t, "helm-v2", "", "helm-r")
	if status, _, _ = s.get("/binflow/helm-v2/index.yaml"); status != http.StatusNotFound {
		t.Errorf("all-miss aggregate = %d, want 404", status)
	}
}

// TestVirtualEmptyMemberContributes: a member whose stored index EXISTS
// but carries zero entries counts as a contribution — the aggregate is the
// 200 empty-entries document (the Artifactory merger posture: the merged
// view exists, it is just empty), distinct from the no-index 404.
func TestVirtualEmptyMemberContributes(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-empty-idx", repo.TypeLocal, "{}")
	// Seed the member's empty stored index at the service level (the
	// client plane refuses index writes — the DB-3 posture).
	empty := "apiVersion: v1\nentries: {}\ngenerated: \"2026-08-27T00:00:00Z\"\n"
	if _, err := s.svc.Put(context.Background(), &auth.Principal{Name: adminUser, Admin: true},
		"helm-empty-idx", "index.yaml", strings.NewReader(empty),
		storage.BlobRef{Sha256: sha256Hex([]byte(empty))}, "text/yaml"); err != nil {
		t.Fatalf("seed the empty member index: %v", err)
	}
	s.seedVirtualRepo(t, "helm-v", "", "helm-empty-idx")
	status, body, _ := s.get("/binflow/helm-v/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("empty-member aggregate = (%d, %s), want the 200 empty document", status, body)
	}
	if !strings.Contains(body, "apiVersion: v1") || !strings.Contains(body, "entries: {}") {
		t.Errorf("empty aggregate body:\n%s", body)
	}

	// Mixed: an empty member beside a charted one serves the charted one's
	// entries only.
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	chart := fixtureChart(t, "mixed", defaultChartYAML("mixed", "1.0.0"), nil)
	if st, b, _ := s.put("/binflow/helm-l/mixed-1.0.0.tgz", chart, nil); st != http.StatusCreated {
		t.Fatalf("PUT = (%d, %s)", st, b)
	}
	s.seedVirtualRepo(t, "helm-v3", "", "helm-empty-idx", "helm-l")
	if status, body, _ = s.get("/binflow/helm-v3/index.yaml"); status != http.StatusOK || !strings.Contains(body, "mixed-1.0.0.tgz") {
		t.Fatalf("mixed aggregate = (%d, %s), want the charted member's entries", status, body)
	}
}
