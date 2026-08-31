package helm

// T-367 (FR-117) wire tests: the heterogeneous chartsBaseUrl pull (the
// chart bodies live under a base the upstream index does NOT cite — the
// fetch still finds them), the _external landing (second pull = local HIT,
// the folded node visible in the repository) and the virtual member's
// charts-base recognition.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedRemoteRepoChartsBase is seedRemoteRepo with the canonical config
// carrying chartsBaseUrl (the seat T-367 adds).
func (s *stack) seedRemoteRepoChartsBase(t *testing.T, key, upstream, chartsBase string) {
	t.Helper()
	cfg := fmt.Sprintf(`{"chartsBaseUrl":%q}`, chartsBase)
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: Protocol, Config: cfg,
	}); err != nil {
		t.Fatalf("seed remote repo %s: %v", key, err)
	}
	if err := s.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey:              key,
		URL:                  strings.TrimRight(upstream, "/"),
		ContentTTLSeconds:    86400,
		MetadataTTLSeconds:   600,
		AllowPrivateUpstream: true,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

func TestT367HeterogeneousBasePull(t *testing.T) {
	s := newStack(t)
	// The INDEX host: the repository URL answers the (relative-urls) index
	// only. The CHARTS host: a different loopback server carrying the
	// bodies under the same in-repo paths.
	indexUp := newChartUpstream(t, map[string]string{})
	chartUp := newChartUpstream(t, map[string]string{})
	chart := fixtureChart(t, "hetchart", defaultChartYAML("hetchart", "1.0.0"), nil)
	indexUp.files["/index.yaml"] = "apiVersion: v1\nentries:\n  hetchart:\n  - name: hetchart\n    version: \"1.0.0\"\n    urls:\n    - hetchart-1.0.0.tgz\n"
	chartUp.files["/hetchart-1.0.0.tgz"] = string(chart)

	s.seedRemoteRepoChartsBase(t, "helm-r", indexUp.srv.URL, chartUp.srv.URL)

	// The index comes from the REPOSITORY URL (metadata class, S10's
	// fallback chain)…
	status, body, hdr := s.get("/binflow/helm-r/index.yaml")
	if status != http.StatusOK || !strings.Contains(body, "hetchart") {
		t.Fatalf("remote index = (%d, %d bytes), want the upstream index", status, len(body))
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("index cache = %q, want MISS", hdr.Get("X-BinFlow-Cache"))
	}
	if n := indexUp.hitCount("/index.yaml"); n != 1 {
		t.Errorf("index host hits = %d, want 1", n)
	}
	// …and the chart body from the CHARTS BASE, bytes intact.
	status, body, _ = s.get("/binflow/helm-r/hetchart-1.0.0.tgz")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Fatalf("heterogeneous chart = (%d, %d bytes), want the charts-base bytes", status, len(body))
	}
	if n := chartUp.hitCount("/hetchart-1.0.0.tgz"); n != 1 {
		t.Errorf("charts-base hits = %d, want 1", n)
	}
	if n := indexUp.hitCount("/hetchart-1.0.0.tgz"); n != 0 {
		t.Errorf("index-host chart hits = %d, want 0 (the base owns the bodies)", n)
	}
	// The second pull is the landed copy: HIT, zero further egress.
	if status, _, hdr = s.get("/binflow/helm-r/hetchart-1.0.0.tgz"); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second chart = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := chartUp.hitCount("/hetchart-1.0.0.tgz"); n != 1 {
		t.Errorf("charts-base hits after re-read = %d, want 1 (cached)", n)
	}
}

func TestT367ExternalLandsInCache(t *testing.T) {
	f := newRemoteFixture(t)
	dep := fixtureChart(t, "depchart", defaultChartYAML("depchart", "1.0.0"), nil)
	f.up.files["/deps/depchart-1.0.0.tgz"] = string(dep)
	abs := f.extURL("/deps/depchart-1.0.0.tgz")

	status, body, hdr := f.get(f.externalWirePath(abs))
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(dep) {
		t.Fatalf("_external = (%d, %d bytes), want the dependency bytes", status, len(body))
	}
	if hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Errorf("first _external cache = %q, want MISS (the landing hop)", hdr.Get("X-BinFlow-Cache"))
	}
	if hdr.Get("X-Binflow-Upstream") != abs {
		t.Errorf("_external upstream header = %q", hdr.Get("X-Binflow-Upstream"))
	}
	// The landing is a real node at the folded path (helm.md section 3's
	// remote cache layout): visible in the repository's own namespace.
	folded, ok := foldExternalURL(abs)
	if !ok {
		t.Fatalf("fold %q", abs)
	}
	nodes, err := f.md.Nodes().ListByPrefix(context.Background(), "helm-r", segExternal)
	if err != nil {
		t.Fatalf("list the external cache: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == folded {
			found = true
		}
	}
	if !found {
		t.Fatalf("the folded node %q is absent from the cache: %v", folded, nodes)
	}
	// The second pull serves the landed copy: HIT, zero third-party egress.
	if status, _, hdr = f.get(f.externalWirePath(abs)); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second _external = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := f.up.hitCount("/deps/depchart-1.0.0.tgz"); n != 1 {
		t.Errorf("third-party hits = %d, want 1 (cached)", n)
	}
}

func TestT367VirtualExternalLandsInMember(t *testing.T) {
	s := newStack(t)
	up := newChartUpstream(t, map[string]string{})
	third := newChartUpstream(t, map[string]string{})
	dep := fixtureChart(t, "extdep", defaultChartYAML("extdep", "1.0.0"), nil)
	third.files["/deps/extdep-1.0.0.tgz"] = string(dep)
	extDepURL := third.srv.URL + "/deps/extdep-1.0.0.tgz"
	up.files["/index.yaml"] = "apiVersion: v1\nentries:\n  extdep:\n  - name: extdep\n    version: \"1.0.0\"\n    urls:\n    - " + extDepURL + "\n"

	s.seedRemoteRepo(t, "helm-r", up.srv.URL)
	s.seedVirtualRepo(t, "helm-v", "", "helm-r")

	// The aggregated index folds the external URL onto the virtual's
	// _external face (S8)…
	status, body, _ := s.get("/binflow/helm-v/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("virtual index = (%d, %s)", status, body)
	}
	folded, ok := foldExternalURL(extDepURL)
	if !ok || !strings.Contains(body, folded+"\n") {
		t.Fatalf("virtual index missing the folded url %q:\n%s", folded, body)
	}
	// …and pulling it lands the dependency in the MEMBER's cache.
	wire := "/binflow/helm-v/" + folded
	if status, body, hdr := s.get(wire); status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(dep) {
		t.Fatalf("virtual _external = (%d, %d bytes), want the dependency bytes", status, len(body))
	} else if hdr.Get("X-BinFlow-Resolved-From") != "helm-r" {
		t.Errorf("resolved-from = %q, want helm-r", hdr.Get("X-BinFlow-Resolved-From"))
	}
	if n := third.hitCount("/deps/extdep-1.0.0.tgz"); n != 1 {
		t.Errorf("third-party hits = %d, want 1", n)
	}
	if status, _, hdr := s.get(wire); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second virtual _external = (%d, cache %q), want 200/HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if n := third.hitCount("/deps/extdep-1.0.0.tgz"); n != 1 {
		t.Errorf("third-party hits after re-read = %d, want 1 (member-cached)", n)
	}
	nodes, err := s.md.Nodes().ListByPrefix(context.Background(), "helm-r", segExternal)
	if err != nil || len(nodes) == 0 {
		t.Fatalf("member external cache = (%v, %v), want the landed folded node", nodes, err)
	}
}

func TestT367VirtualChartsBaseRecognition(t *testing.T) {
	s := newStack(t)
	// The member's upstream: the index lives at the repository URL, the
	// chart bodies under a DIVERGENT charts base (a second host).
	up := newChartUpstream(t, map[string]string{})
	base := newChartUpstream(t, map[string]string{})
	chart := fixtureChart(t, "basechart", defaultChartYAML("basechart", "1.0.0"), nil)
	base.files["/basechart-1.0.0.tgz"] = string(chart)
	up.files["/index.yaml"] = "apiVersion: v1\nentries:\n" +
		"  basechart:\n  - name: basechart\n    version: \"1.0.0\"\n    urls:\n    - " + base.srv.URL + "/basechart-1.0.0.tgz\n"

	s.seedRemoteRepoChartsBase(t, "helm-r", up.srv.URL, base.srv.URL)
	s.seedVirtualRepo(t, "helm-v", "", "helm-r")

	// The member's recognition base IS the configured chartsBaseUrl: the
	// entry collapses to the member-relative path (the virtual serves it),
	// NOT to the folded _external form a repo-URL base would produce.
	status, body, _ := s.get("/binflow/helm-v/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("virtual index = (%d, %s)", status, body)
	}
	if !strings.Contains(body, "- basechart-1.0.0.tgz\n") {
		t.Fatalf("the charts-base entry did not collapse to the member-relative path:\n%s", body)
	}
	if strings.Contains(body, segExternal+"/") {
		t.Fatalf("the charts-base entry was mis-recognized as external:\n%s", body)
	}
	// And the collapsed path resolves through the member — whose own fetch
	// hop goes to the charts base (the full heterogeneous chain through the
	// virtual).
	if status, body, _ = s.get("/binflow/helm-v/basechart-1.0.0.tgz"); status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(chart) {
		t.Fatalf("virtual chart via the charts base = (%d, %d bytes), want the charts-base bytes", status, len(body))
	}
	if n := base.hitCount("/basechart-1.0.0.tgz"); n != 1 {
		t.Errorf("charts-base hits = %d, want 1", n)
	}
	if n := up.hitCount("/basechart-1.0.0.tgz"); n != 0 {
		t.Errorf("index-host chart hits = %d, want 0", n)
	}
}
