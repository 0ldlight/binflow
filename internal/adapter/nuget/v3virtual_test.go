package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual v3 search merge (nuget.md section 8.2) against a local
// member's stored facts and a remote member's live upstream search.

// TestV3VirtualSearchMerge: local facts and the remote member's live
// upstream candidates BOTH contribute; the merged output cites THIS
// virtual's registration base (step 6) and pages over the merged set
// (step 5).
func TestV3VirtualSearchMerge(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-loc", repo.TypeLocal)
	s.seedRepo(t, "ng-rem", repo.TypeRemote)

	pkg := buildNupkg(t, "Virt.Local", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-loc", "virt.local", "1.0.0"), pkg.body, nil); status != http.StatusCreated {
		t.Fatalf("local push: %d %s", status, body)
	}
	var up *v3SearchUpstream
	up = newV3SearchUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalHits":1,"data":[{"id":"Virt.Remote","version":"2.0.0",` + //nolint:gosec // test fixture
			`"versions":[{"version":"1.9.0"},{"version":"2.0.0"}],` +
			`"registration":"` + up.srv.URL + `/v3/registration5-gz-semver2/virt.remote/index.json"}]}`))
	})
	up.serveSearchIndex(t)
	s.seedRemoteConfig(t, "ng-rem", up.srv.URL)

	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	s.seedVirtualMembers(t, "ng-virt", "ng-loc", "ng-rem")

	status, body, _ := s.get(apiPath("ng-virt") + "/query")
	if status != http.StatusOK {
		t.Fatalf("virtual search = %d %s", status, body)
	}
	var doc searchResponse
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("virtual search body: %v (%s)", err, body)
	}
	// Step 5: totalHits is the MERGED size, not any member's count.
	if doc.TotalHits != 2 || len(doc.Data) != 2 {
		t.Fatalf("merged hits = %d/%d, want 2/2 (local facts + the remote upstream):\n%s", doc.TotalHits, len(doc.Data), body)
	}
	byID := map[string]*searchHit{}
	for _, hit := range doc.Data {
		byID[lowerASCII(hit.ID)] = hit
	}
	if _, ok := byID["virt.local"]; !ok {
		t.Errorf("local member's package missing from the merge:\n%s", body)
	}
	remote, ok := byID["virt.remote"]
	if !ok {
		t.Fatalf("remote member's upstream candidate missing from the merge:\n%s", body)
	}
	// The upstream's version set survives the merge (the union).
	if len(remote.Versions) != 2 || remote.Versions[0].Version != "1.9.0" {
		t.Errorf("remote hit versions = %+v, want the union [1.9.0 2.0.0]", remote.Versions)
	}
	// Step 6: the OUTPUT cites this virtual's registration base — the
	// upstream member URL is gone.
	for _, hit := range doc.Data {
		want := s.srv.URL + "/binflow/api/nuget/v3/ng-virt/registration/" + lowerASCII(hit.ID) + "/index.json"
		if hit.Registration != want {
			t.Errorf("hit %s registration = %q, want %q", hit.ID, hit.Registration, want)
		}
	}
	if strings.Contains(body, up.srv.URL) {
		t.Errorf("merged output still cites the upstream:\n%s", body)
	}

	// The semVerLevel spelling of the same merge (section 8.1-7).
	status, body, _ = s.get(apiPath("ng-virt") + "/query?semVerLevel=2.0.0")
	if status != http.StatusOK || !strings.Contains(body, "/binflow/api/nuget/v3/ng-virt/registration-semver2/virt.local/index.json") {
		t.Errorf("semVerLevel merge = (%d, %s…), want the -semver2 registration base", status, firstLine(body))
	}
}

// TestV3VirtualSearchPriorityBuckets: the id-level putIfAbsent across
// buckets (step 4) — a prioritised member's id shadows the lower bucket's
// same id WHOLE, while ids only the lower bucket carries still surface.
func TestV3VirtualSearchPriorityBuckets(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-low", repo.TypeLocal)
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "ng-prio", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("seed prioritised member: %v", err)
	}

	shared := buildNupkg(t, "Shared.Pkg", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-prio", "shared.pkg", "1.0.0"), shared.body, nil); status != http.StatusCreated {
		t.Fatalf("prio push: %d %s", status, body)
	}
	sharedLow := buildNupkg(t, "Shared.Pkg", "9.9.9", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-low", "shared.pkg", "9.9.9"), sharedLow.body, nil); status != http.StatusCreated {
		t.Fatalf("low push (shared): %d %s", status, body)
	}
	only := buildNupkg(t, "Only.Low", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(pushPath("ng-low", "only.low", "1.0.0"), only.body, nil); status != http.StatusCreated {
		t.Fatalf("low push (only): %d %s", status, body)
	}

	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	s.seedVirtualMembers(t, "ng-virt", "ng-prio", "ng-low")

	status, body, _ := s.get(apiPath("ng-virt") + "/query")
	if status != http.StatusOK {
		t.Fatalf("virtual search = %d %s", status, body)
	}
	var doc searchResponse
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("virtual search body: %v (%s)", err, body)
	}
	if doc.TotalHits != 2 {
		t.Fatalf("merged total = %d, want 2:\n%s", doc.TotalHits, body)
	}
	for _, hit := range doc.Data {
		switch lowerASCII(hit.ID) {
		case "shared.pkg":
			// The prioritised bucket owns the id WHOLE: its 1.0.0 stands
			// and the lower bucket's 9.9.9 never version-merges in.
			if hit.Version != "1.0.0" || len(hit.Versions) != 1 {
				t.Errorf("shared.pkg = %s %+v, want the prioritised bucket's whole answer", hit.Version, hit.Versions)
			}
		case "only.low":
			// Ids the higher bucket lacks still surface from the lower one.
		default:
			t.Errorf("unexpected id %q in the merge", hit.ID)
		}
	}
}
