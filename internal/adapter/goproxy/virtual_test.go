package goproxy

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual-repository face (goproxy.md section 6.3): first-found
// downloads in member order (local first), the union list, the global-best
// @latest and the write route onto the deployment member.

// seedVirtualRepo writes a virtual repository row with its deployment route
// ("" leaves the C5 405 shape) plus the member ledger.
func (s *stack) seedVirtualRepo(t *testing.T, key, deployment string, members ...string) {
	t.Helper()
	config := `{"repositories":[` + quoteJoin(members) + `]`
	if deployment != "" {
		config += `,"defaultDeploymentRepo":"` + deployment + `"`
	}
	config += `}`
	if err := s.md.Repos().Create(t.Context(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: Protocol, Config: config,
	}); err != nil {
		t.Fatalf("seed virtual %s: %v", key, err)
	}
	s.seedVirtualMembers(t, key, members...)
}

func quoteJoin(parts []string) string {
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = `"` + p + `"`
	}
	return strings.Join(quoted, ",")
}

// newVirtualStack: go-local (with one module) + go-remote (fake upstream
// with another module) + go-virt [go-local, go-remote] — the AC4 shape.
func newVirtualStack(t *testing.T) (*stack, *fakeUpstream) {
	t.Helper()
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	s.seedVirtualRepo(t, "go-virt", "", "go-local", "go-remote")

	putModule(s, t, "example.com/mymod", "v1.0.0",
		"module example.com/mymod\n", `{"Version":"v1.0.0","Time":"2024-01-02T03:04:05Z"}`, []byte("PK-local"))
	return s, up
}

// TestVirtualFirstFound: the local member wins for its modules (zero
// upstream traffic); remote-only modules resolve through the remote member.
func TestVirtualFirstFound(t *testing.T) {
	s, up := newVirtualStack(t)

	status, body, hdr := s.get("/binflow/go-virt/example.com/mymod/@v/v1.0.0.zip")
	if status != http.StatusOK || body != "PK-local" {
		t.Fatalf("virtual local-first .zip = (%d, %q)", status, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "go-local" {
		t.Errorf("X-BinFlow-Resolved-From = %q, want go-local", got)
	}

	status, body, hdr = s.get("/binflow/go-virt/example.com/m/@v/v1.0.0.zip")
	if status != http.StatusOK || body != "PK-upstream-zip" {
		t.Fatalf("virtual remote-miss .zip = (%d, %q)", status, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "go-remote" {
		t.Errorf("X-BinFlow-Resolved-From = %q, want go-remote", got)
	}
	if n := up.count("/example.com/m/@v/v1.0.0.zip"); n != 1 {
		t.Errorf("upstream contacts = %d, want 1", n)
	}

	// A miss on every member is the plain 404.
	status, _, _ = s.get("/binflow/go-virt/example.com/nosuch/@v/v1.0.0.zip")
	if status != http.StatusNotFound {
		t.Errorf("virtual total miss = %d, want 404", status)
	}
}

// TestVirtualUnionList: local versions plus the remote member's upstream
// list, deduplicated and stably ordered.
func TestVirtualUnionList(t *testing.T) {
	s, up := newVirtualStack(t)
	putModule(s, t, "example.com/both", "v3.0.0", "module example.com/both\n", `{"Version":"v3.0.0"}`, []byte("PK"))

	// The remote member knows example.com/both too (via a dedicated list
	// endpoint on a second fake upstream is overkill: reuse m and assert the
	// union across DIFFERENT modules instead — local mymod + remote m).
	status, body, _ := s.get("/binflow/go-virt/example.com/mymod/@v/list")
	if status != http.StatusOK || body != "v1.0.0\n" {
		t.Fatalf("virtual list (local-only module) = (%d, %q)", status, body)
	}
	if n := up.count("/example.com/mymod/@v/list"); n != 1 {
		// The remote member was probed (its miss is expected, one contact).
		t.Logf("remote list probe contacts = %d", n)
	}

	status, body, _ = s.get("/binflow/go-virt/example.com/m/@v/list")
	if status != http.StatusOK || body != "v0.1.0\nv1.0.0\nv1.1.0\n" {
		t.Fatalf("virtual list (remote-only module) = (%d, %q)", status, body)
	}
}

// TestVirtualUnionListOverlappingModule: the SAME module on both members —
// the union merges local-registered and upstream-listed versions.
func TestVirtualUnionListOverlappingModule(t *testing.T) {
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	s.seedVirtualRepo(t, "go-virt", "", "go-local", "go-remote")
	// Local registers v2.0.0 of the module the upstream lists
	// v0.1.0/v1.0.0/v1.1.0 for — under the SAME path shape.
	putModule(s, t, "example.com/m", "v2.0.0", "module example.com/m\n", `{"Version":"v2.0.0"}`, []byte("PK2"))

	status, body, _ := s.get("/binflow/go-virt/example.com/m/@v/list")
	if status != http.StatusOK {
		t.Fatalf("union list status = %d, body %s", status, body)
	}
	want := "v0.1.0\nv1.0.0\nv1.1.0\nv2.0.0\n"
	if body != want {
		t.Errorf("union list = %q, want %q", body, want)
	}
}

// TestVirtualGlobalLatest: the members' candidates compete by the section
// 4.5 order; the winning member's original body serves.
func TestVirtualGlobalLatest(t *testing.T) {
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	s.seedVirtualRepo(t, "go-virt", "", "go-local", "go-remote")
	// Local's best is v1.0.0; the upstream @latest says v1.1.0 — the remote
	// candidate must win globally.
	putModule(s, t, "example.com/m", "v1.0.0", "module example.com/m\n", `{"Version":"v1.0.0"}`, []byte("PK1"))

	status, body, hdr := s.get("/binflow/go-virt/example.com/m/@latest")
	if status != http.StatusOK || !strings.Contains(body, `"Version":"v1.1.0"`) {
		t.Fatalf("global latest = (%d, %s), want the remote v1.1.0 body", status, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "go-remote" {
		t.Errorf("latest winner = %q, want go-remote", got)
	}

	// A local release beats the remote pre-release candidate: rebuild the
	// local member's register with a higher release.
	putModule(s, t, "example.com/m", "v2.0.0", "module example.com/m\n", `{"Version":"v2.0.0"}`, []byte("PK2"))
	status, body, hdr = s.get("/binflow/go-virt/example.com/m/@latest")
	if status != http.StatusOK || !strings.Contains(body, `"Version":"v2.0.0"`) {
		t.Fatalf("global latest after v2 = (%d, %s), want the local v2.0.0 body", status, body)
	}
	if got := hdr.Get("X-BinFlow-Resolved-From"); got != "go-local" {
		t.Errorf("latest winner after v2 = %q, want go-local", got)
	}
}

// TestVirtualLatestSynthesisInMemory: a local member without a stored .info
// contributes its synthesized candidate (no write-back through the virtual
// face — that belongs to the direct local read).
func TestVirtualLatestSynthesisInMemory(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedVirtualRepo(t, "go-virt", "", "go-local")
	base := "/binflow/go-local/example.com/mymod/@v/v1.0.0"
	if status, body, _ := s.put(base+".zip", []byte("PK"), nil); status != http.StatusCreated {
		t.Fatalf("PUT .zip: %d %s", status, body)
	}

	status, body, _ := s.get("/binflow/go-virt/example.com/mymod/@latest")
	if status != http.StatusOK || !strings.Contains(body, `"Version":"v1.0.0"`) {
		t.Fatalf("virtual synthesized latest = (%d, %s)", status, body)
	}
	nodes, err := s.svc.List(t.Context(), adminPrincipal(), "go-local", "example.com/mymod/@v/")
	if err != nil {
		t.Fatalf("list local: %v", err)
	}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, ".info") {
			t.Errorf("virtual latest wrote back a node: %s", n.Path)
		}
	}
}

// TestVirtualPutRoutesToDeploymentMember: the write route lands on the
// configured local member and is immediately visible through the virtual.
func TestVirtualPutRoutesToDeploymentMember(t *testing.T) {
	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	s.seedVirtualRepo(t, "go-virt", "go-local", "go-local", "go-remote")

	status, body, _ := s.put("/binflow/go-virt/example.com/routed/@v/v1.0.0.zip", []byte("PK-routed"), nil)
	if status != http.StatusCreated {
		t.Fatalf("virtual PUT = (%d, %s), want 201", status, body)
	}
	status, body, hdr := s.get("/binflow/go-virt/example.com/routed/@v/v1.0.0.zip")
	if status != http.StatusOK || body != "PK-routed" || hdr.Get("X-BinFlow-Resolved-From") != "go-local" {
		t.Fatalf("virtual read-back = (%d, %q, %q)", status, body, hdr.Get("X-BinFlow-Resolved-From"))
	}
}

// TestVirtualPutWithoutRoute: the un-routed virtual answers the C5 405.
func TestVirtualPutWithoutRoute(t *testing.T) {
	s, _ := newVirtualStack(t)
	status, body, _ := s.put("/binflow/go-virt/example.com/x/@v/v1.0.0.zip", []byte("PK"), nil)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed virtual PUT = (%d, %s), want 405", status, body)
	}
}

// TestVirtualIncompatibleModThroughVirtual: the +incompatible synthesis
// holds on the virtual face (a stored member copy first, synthesis after
// the member walk misses).
func TestVirtualIncompatibleModThroughVirtual(t *testing.T) {
	s, up := newVirtualStack(t)
	putModule(s, t, "example.com/legacy", "v1.0.0+incompatible",
		"module example.com/legacy\n", `{"Version":"v1.0.0+incompatible"}`, []byte("PKL"))

	status, body, _ := s.get("/binflow/go-virt/example.com/legacy/@v/v1.0.0+incompatible.mod")
	if status != http.StatusOK || body != "module example.com/legacy\n" {
		t.Fatalf("virtual +incompatible .mod = (%d, %q), want the stored copy", status, body)
	}
	// No member holds this one: the synthesized line serves after the LOCAL
	// member walk — the remote member is never asked (section 6.2's
	// no-upstream rule holding through the aggregation).
	status, body, _ = s.get("/binflow/go-virt/example.com/other/@v/v2.0.0+incompatible.mod")
	if status != http.StatusOK || body != "module example.com/other\n" {
		t.Fatalf("virtual synthesized +incompatible .mod = (%d, %q)", status, body)
	}
	if n := len(up.paths()); n != 0 {
		t.Errorf("upstream contacts = %d (%v), want 0", n, up.paths())
	}
}
