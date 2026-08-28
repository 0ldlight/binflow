package nuget

import (
	"context"
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual v2 merge (nuget.md section 7.2) over a local member and a
// remote member: both buckets contribute, the id-level dedup drops a
// lower bucket's WHOLE package, the download links re-anchor onto the
// virtual's Download face, and the virtual Download walk takes the local
// members first.

// seedV2VirtualFixture builds the two-member fixture: the local member
// carries Loc.Merged and Loc.Only; the upstream carries Loc.Merged (the
// dedup case — the local bucket must own it) and Rem.Only.
func seedV2VirtualFixture(t *testing.T) (*stack, *v2Upstream) {
	t.Helper()
	s := newStack(t)
	s.seedRepo(t, "ng-rem", repo.TypeRemote)
	s.seedRepo(t, "ng-virt", repo.TypeVirtual)

	// The local member carries priorityResolution — the two-bucket order's
	// higher tier — so the id-level dedup (7.2-5) applies across the tiers.
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "ng-loc", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("seed priority local member: %v", err)
	}
	for _, p := range []struct{ id, ver string }{
		{"Loc.Merged", "1.0.0"},
		{"Loc.Only", "2.0.0"},
	} {
		mustPushV2(t, s, "ng-loc", buildNupkg(t, p.id, p.ver, flatDeps("none")))
	}
	up := newV2Upstream(t, func(w http.ResponseWriter, _ *http.Request, res string) {
		if strings.HasPrefix(res, "api/v2/Search()") {
			w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
			_, _ = w.Write([]byte(upstreamV2Feed("Loc.Merged", "9.9.9") + "")) //nolint:gosec // test fixture
			return
		}
		if strings.HasPrefix(res, "api/v2/FindPackagesById()") {
			if strings.Contains(strings.ToLower(res), "loc.merged") {
				w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
				_, _ = w.Write([]byte(upstreamV2Feed("Loc.Merged", "9.9.9"))) //nolint:gosec // test fixture
				return
			}
			if strings.Contains(strings.ToLower(res), "rem.only") {
				w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
				_, _ = w.Write([]byte(upstreamV2Feed("Rem.Only", "5.0.0"))) //nolint:gosec // test fixture
				return
			}
		}
		http.NotFound(w, nil)
	})
	s.seedRemoteConfig(t, "ng-rem", up.srv.URL)
	s.seedVirtualMembers(t, "ng-virt", "ng-loc", "ng-rem")
	return s, up
}

// TestV2VirtualMergeDedup: the id-level dedup across buckets — the local
// bucket's Loc.Merged wins whole, the remote bucket's 9.9.9 never leaks;
// FindPackagesById merges members for the other ids.
func TestV2VirtualMergeDedup(t *testing.T) {
	s, up := seedV2VirtualFixture(t)

	// The local member's package keeps its own version.
	status, body, _ := s.get(apiV2Path("ng-virt") + "/FindPackagesById()?id='Loc.Merged'")
	if status != http.StatusOK {
		t.Fatalf("merged feed = %d %s", status, body)
	}
	var feed atomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("merged feed: %v\n%s", err, body)
	}
	if len(feed.Entries) != 1 || feed.Entries[0].Properties.Version != "1.0.0" {
		t.Fatalf("merged entries = %+v, want the local bucket's 1.0.0 alone", feed.Entries)
	}
	if n := up.hitsTotal(); n == 0 {
		t.Errorf("the remote member was never consulted (expected its fan-out read)")
	}

	// The remote member's id surfaces through the same virtual face.
	status, body, _ = s.get(apiV2Path("ng-virt") + "/FindPackagesById()?id='Rem.Only'")
	if status != http.StatusOK || !strings.Contains(body, "5.0.0") {
		t.Fatalf("remote-member feed = (%d, %s…)", status, firstLine(body))
	}
	// Virtual feeds cite the virtual's v2 Download face.
	if !strings.Contains(body, "/binflow/api/nuget/v2/ng-virt/Download/rem.only/5.0.0") {
		t.Errorf("virtual feed link not on the Download face:\n%s", firstLine(body))
	}
}

// TestV2VirtualSearchMerge: the search face merges both members with the
// same id-level rule.
func TestV2VirtualSearchMerge(t *testing.T) {
	s, _ := seedV2VirtualFixture(t)
	status, body, _ := s.get(apiV2Path("ng-virt") + "/Search()")
	if status != http.StatusOK {
		t.Fatalf("virtual Search = %d %s", status, body)
	}
	var feed atomFeed
	if err := xml.Unmarshal([]byte(body), &feed); err != nil {
		t.Fatalf("virtual Search: %v", err)
	}
	ids := map[string]string{}
	for _, e := range feed.Entries {
		ids[e.Properties.ID] = e.Properties.Version
	}
	if len(ids) != 2 {
		t.Fatalf("virtual Search ids = %v, want Loc.Merged + Loc.Only (dedup applied)", ids)
	}
	if ids["Loc.Merged"] != "1.0.0" {
		t.Errorf("Loc.Merged version = %q, want the local bucket's 1.0.0", ids["Loc.Merged"])
	}
	if _, ok := ids["Loc.Only"]; !ok {
		t.Errorf("Loc.Only missing from %v", ids)
	}
}

// TestV2VirtualDownloadWalk: the Download face takes the local members
// first, then the remote members (section 5.3).
func TestV2VirtualDownloadWalk(t *testing.T) {
	s, _ := seedV2VirtualFixture(t)

	// The local member's package serves from the local member.
	pkg := buildNupkg(t, "Loc.Only", "2.0.0", flatDeps("none"))
	status, body, _ := s.get(apiV2Path("ng-virt") + "/Download/loc.only/2.0.0")
	if status != http.StatusOK || body != string(pkg.body) {
		t.Fatalf("virtual local download = %d (len %d)", status, len(body))
	}
	// The miss wording on nothing found.
	status, body, _ = s.get(apiV2Path("ng-virt") + "/Download/nope.pkg/1.0.0")
	if status != http.StatusNotFound || !strings.Contains(body, "Unable to find NuPkg 'nope.pkg-1.0.0' in 'ng-virt'") {
		t.Errorf("virtual download miss = (%d, %q)", status, firstLine(body))
	}
}

// hitsTotal is the fixture's total contact counter (the dedup leg only
// asserts SOMETHING was consulted).
func (up *v2Upstream) hitsTotal() int64 { return up.hits.Load() }

// TestV2VirtualPublishRouting: the unrouted virtual answers the rclass
// 400; a routed virtual publishes onto the deployment member through the
// ordinary Put chain (svc routes; the 201 names the deployed path).
func TestV2VirtualPublishRouting(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-dep", repo.TypeLocal)
	s.seedRepo(t, "ng-virt2", repo.TypeVirtual)
	if err := s.md.Repos().Update(context.Background(), &metadata.Repo{
		RepoKey: "ng-virt2", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"defaultDeploymentRepo":"ng-dep"}`,
	}); err != nil {
		t.Fatalf("route virtual: %v", err)
	}

	pkg := buildNupkg(t, "Virt.Pkg", "1.0.0", flatDeps("none"))
	status, body, _ := s.put(v2ContentPath("ng-virt2"), pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("routed virtual publish = %d %s", status, body)
	}
	if !strings.Contains(body, "Successfully published NuPkg to: virt.pkg/1.0.0/virt.pkg.1.0.0"+suffixNupkg) {
		t.Errorf("routed publish body = %q", firstLine(body))
	}
	// The package landed in the deployment member.
	if status, _, _ := s.get(apiPath("ng-dep") + "/flatcontainer/virt.pkg/1.0.0/virt.pkg.1.0.0.nupkg"); status != http.StatusOK {
		t.Errorf("deployment-member landing = %d", status)
	}
	// The unrouted virtual (no defaultDeploymentRepo) answers the 400.
	s.seedRepo(t, "ng-virt3", repo.TypeVirtual)
	status, body, _ = s.put(v2ContentPath("ng-virt3"), pkg.body, nil)
	if status != http.StatusBadRequest || !strings.Contains(body, msgV2LocalOnly) {
		t.Errorf("unrouted virtual publish = (%d, %q)", status, firstLine(body))
	}
}
