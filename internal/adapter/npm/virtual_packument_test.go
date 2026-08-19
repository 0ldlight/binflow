package npm

// T-72: the virtual packument merge matrix (FR-21-AC5). Every case runs
// the real stack (real service, real engine, counting mock upstream) and
// drives the adapter's read/write faces exactly as httpapi hands them over.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// npmVirtualFixture seeds two local members (npmv-a first, npmv-b second),
// one remote member behind a counting mock upstream and a virtual over
// them in declaration order.
type npmVirtualFixture struct {
	s        *stack
	upstream *httptest.Server
}

func newNPMVirtualFixture(t *testing.T, virtualConfig string) *npmVirtualFixture {
	t.Helper()
	s := newStack(t)
	ctx := context.Background()

	f := &npmVirtualFixture{s: s}
	f.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/up-pkg/packument.json":
			_, _ = w.Write([]byte(`{"_id":"up-pkg","name":"up-pkg",` +
				`"description":"from the upstream member",` +
				`"dist-tags":{"latest":"3.0.0","up-next":"3.0.0"},` +
				`"time":{"created":"2026-01-01T00:00:00Z","3.0.0":"2026-01-02T00:00:00Z"},` +
				`"versions":{` +
				`"3.0.0":{"name":"up-pkg","version":"3.0.0","dist":{"tarball":"up-pkg/-/up-pkg-3.0.0.tgz"}},` +
				`"1.0.0":{"name":"up-pkg","version":"1.0.0","dist":{"tarball":"up-pkg/-/up-pkg-1.0.0.tgz"}}}}`))
		case "/up-pkg/-/up-pkg-3.0.0.tgz":
			_, _ = w.Write([]byte("UP-TARBALL-3"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.upstream.Close)

	rows := []*metadata.Repo{
		{RepoKey: "npmv-a", Type: repo.TypeLocal, PackageType: Protocol},
		{RepoKey: "npmv-b", Type: repo.TypeLocal, PackageType: Protocol},
		{RepoKey: "npmv-rem", Type: repo.TypeRemote, PackageType: Protocol,
			Config: `{"url":"` + f.upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "npmv-virt", Type: repo.TypeVirtual, PackageType: Protocol, Config: virtualConfig},
	}
	for _, row := range rows {
		if _, err := s.svc.CreateRepo(ctx, adminPrincipal, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}
	return f
}

// publish seeds one version into a LOCAL member through the publish plane.
func (f *npmVirtualFixture) publish(t *testing.T, repoKey, name, version, tarball, description string) {
	t.Helper()
	doc := publishDoc(name, version, tarball, map[string]string{"latest": version}, nil)
	if description != "" {
		doc["description"] = description
	}
	if rr := f.s.call(http.MethodPut, "/"+repoKey+"/"+name, mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("seed publish %s/%s@%s = %d; body=%s", repoKey, name, version, rr.Code, bodyOf(rr))
	}
}

// packument fetches one member's or the virtual's packument as a map.
func (f *npmVirtualFixture) packument(t *testing.T, repoKey, name string) (int, map[string]any) {
	t.Helper()
	rr := f.s.call(http.MethodGet, "/"+repoKey+"/"+name, "", adminPrincipal, nil)
	doc := map[string]any{}
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal([]byte(bodyOf(rr)), &doc); err != nil {
			t.Fatalf("parse packument %s/%s: %v", repoKey, name, err)
		}
	}
	return rr.Code, doc
}

// TestVirtualPackumentMergeMatrix is the AC's table: the same package name
// spread over several members merges — first member the base, later
// versions putIfAbsent, dist-tags/time unions, generic fields first-wins,
// latest recomputed — computed per request, never cached. demo-pkg carries
// the local-member rows; up-pkg (which ONLY the remote member knows) the
// remote-union rows.
func TestVirtualPackumentMergeMatrix(t *testing.T) {
	f := newNPMVirtualFixture(t, `{"repositories":["npmv-a","npmv-b","npmv-rem"]}`)

	f.publish(t, "npmv-a", "demo-pkg", "1.0.0", "A-TARBALL", "from member a")
	f.publish(t, "npmv-b", "demo-pkg", "1.0.0", "B-TARBALL-SAME-VERSION", "from member b")
	f.publish(t, "npmv-b", "demo-pkg", "2.0.0", "B2-TARBALL", "")

	code, doc := f.packument(t, "npmv-virt", "demo-pkg")
	if code != http.StatusOK {
		t.Fatalf("virtual packument = %d", code)
	}
	versions := versionsOf(doc)
	for _, v := range []string{"1.0.0", "2.0.0"} {
		if versions[v] == nil {
			t.Errorf("merged versions missing %s: %v", v, versions)
		}
	}
	// putIfAbsent: the FIRST member's 1.0.0 wins — its tarball reference,
	// not member b's bytes.
	a1 := mapOf(mapOf(versions["1.0.0"])["dist"])
	if a1 == nil || stringOf(a1["shasum"]) != shasumOf("A-TARBALL") {
		t.Errorf("putIfAbsent lost the base member's 1.0.0: %v", versions["1.0.0"])
	}
	// Generic fields: first-arrival (the base member's description).
	if got := stringOf(doc["description"]); got != "from member a" {
		t.Errorf("merged description = %q, want the base member's", got)
	}

	// Per-request recompute: a publish landing between two GETs shows up in
	// the very next one.
	f.publish(t, "npmv-b", "demo-pkg", "2.1.0", "B21-TARBALL", "")
	_, doc = f.packument(t, "npmv-virt", "demo-pkg")
	if versionsOf(doc)["2.1.0"] == nil {
		t.Errorf("per-request merge did not pick up 2.1.0")
	}

	// The remote-member package: local members miss, the remote member is
	// the base — its versions, tags, time and generic fields all carry.
	code, doc = f.packument(t, "npmv-virt", "up-pkg")
	if code != http.StatusOK {
		t.Fatalf("remote-only packument through virtual = %d", code)
	}
	versions = versionsOf(doc)
	for _, v := range []string{"1.0.0", "3.0.0"} {
		if versions[v] == nil {
			t.Errorf("remote member's %s lost from the union: %v", v, versions)
		}
	}
	if got := stringOf(doc["description"]); got != "from the upstream member" {
		t.Errorf("remote-based description = %q", got)
	}
	tags := distTagsOf(doc)
	if tags["up-next"] != "3.0.0" {
		t.Errorf("dist-tags union lost the remote member's up-next: %v", tags)
	}
	if tags["latest"] != "3.0.0" {
		t.Errorf("recomputed latest = %q, want 3.0.0 (the union's greatest)", tags["latest"])
	}
	if tm := mapOf(doc["time"]); tm["3.0.0"] == nil {
		t.Errorf("time union lost the remote member's 3.0.0: %v", tm)
	}

	// The merged tarball hrefs point INTO the virtual (first-hit resolution
	// answers them); the remote member's 3.0.0 tarball downloads through it.
	rr := f.s.call(http.MethodGet, "/npmv-virt/up-pkg/-/up-pkg-3.0.0.tgz", "", adminPrincipal, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("remote-member tarball via virtual = %d; body=%s", rr.Code, bodyOf(rr))
	}
	if body := bodyOf(rr); body != "UP-TARBALL-3" {
		t.Errorf("remote-member tarball body = %q", body)
	}
}

// shasumOf is the hex sha1 of a fixture payload (the putIfAbsent marker —
// the base member's dist bytes are what the merged entry must carry).
func shasumOf(payload string) string {
	sha1Sum, _, _ := digestTriple([]byte(payload))
	return hexEncode(sha1Sum)
}

// TestVirtualPackumentPriorityDoesNotShortCircuit: npm's merge has no
// foundByPriority — a marked member reorders the BASE, it never hides the
// other members' versions.
func TestVirtualPackumentPriorityDoesNotShortCircuit(t *testing.T) {
	f := newNPMVirtualFixture(t, `{"repositories":["npmv-a","npmv-b"]}`)
	if _, err := f.s.svc.UpdateRepo(context.Background(), adminPrincipal, &metadata.Repo{
		RepoKey: "npmv-b", Type: repo.TypeLocal, PackageType: Protocol,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark npmv-b priority: %v", err)
	}
	f.publish(t, "npmv-a", "demo-pkg", "1.0.0", "A", "from member a")
	f.publish(t, "npmv-b", "demo-pkg", "2.0.0", "B", "from member b")

	_, doc := f.packument(t, "npmv-virt", "demo-pkg")
	versions := versionsOf(doc)
	if versions["1.0.0"] == nil || versions["2.0.0"] == nil {
		t.Fatalf("npm merge short-circuited on the priority bucket: %v", versions)
	}
	// The priority member leads the two-bucket order, so it is the BASE:
	// its generic fields win.
	if got := stringOf(doc["description"]); got != "from member b" {
		t.Errorf("priority bucket did not lead the merge (description %q)", got)
	}
}

// TestVirtualPackumentMemberFaults: one member's classified failure must
// not block the others (the PRD's collection posture); a collection where
// NOTHING was gathered surfaces the remembered failure honestly.
func TestVirtualPackumentMemberFaults(t *testing.T) {
	ctx := context.Background()
	f := newNPMVirtualFixture(t, `{"repositories":["npmv-a","npmv-rem"]}`)

	// The faulting member: a remote without the loopback exemption — every
	// fetch is the engine's SSRF 400. It must exist BEFORE the virtual that
	// lists it (member validation).
	if _, err := f.s.svc.CreateRepo(ctx, adminPrincipal, &metadata.Repo{
		RepoKey: "npmv-fault", Type: repo.TypeRemote, PackageType: Protocol,
		Config: `{"url":"` + f.upstream.URL + `"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(npmv-fault): %v", err)
	}
	if _, err := f.s.svc.CreateRepo(ctx, adminPrincipal, &metadata.Repo{
		RepoKey: "npmv-fmix", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["npmv-a","npmv-fault"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(npmv-fmix): %v", err)
	}
	f.publish(t, "npmv-a", "demo-pkg", "1.0.0", "A", "")

	// The healthy member still answers through the fault.
	if code, doc := f.packument(t, "npmv-fmix", "demo-pkg"); code != http.StatusOK || versionsOf(doc)["1.0.0"] == nil {
		t.Fatalf("one member's fault blocked the merge (status %d)", code)
	}
	// A package no member has: the remembered failure, not a bare 404.
	rr := f.s.call(http.MethodGet, "/npmv-fmix/ghost-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusBadRequest || !strings.Contains(bodyOf(rr), "suppressed upstream") {
		t.Fatalf("all-member-fault packument = %d; body=%s", rr.Code, bodyOf(rr))
	}

	// And a virtual whose ONLY member faults: same honest verdict.
	if _, err := f.s.svc.CreateRepo(ctx, adminPrincipal, &metadata.Repo{
		RepoKey: "npmv-vfault", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"repositories":["npmv-fault"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(npmv-vfault): %v", err)
	}
	rr = f.s.call(http.MethodGet, "/npmv-vfault/ghost-pkg", "", adminPrincipal, nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("single-faulting-member virtual = %d, want 400", rr.Code)
	}
}

// TestVirtualPublishUsesDeploymentMemberOnly: the WRITE plane never sees
// the merge — a publish through a ROUTED virtual lands in the deployment
// member off ITS OWN document, so the other members' versions are not
// copied into it.
func TestVirtualPublishUsesDeploymentMemberOnly(t *testing.T) {
	f := newNPMVirtualFixture(t,
		`{"repositories":["npmv-a","npmv-b"],"defaultDeploymentRepo":"npmv-a"}`)
	f.publish(t, "npmv-b", "demo-pkg", "2.0.0", "B", "")

	// Publish a NEW version through the virtual: the route sends it to
	// npmv-a, whose own document must gain ONLY that version.
	doc := publishDoc("demo-pkg", "1.5.0", "NEW", nil, nil)
	rr := f.s.call(http.MethodPut, "/npmv-virt/demo-pkg", mustJSON(doc), adminPrincipal, nil)
	if rr.Code != http.StatusCreated {
		t.Fatalf("routed virtual publish = %d; body=%s", rr.Code, bodyOf(rr))
	}
	_, memberDoc := f.packument(t, "npmv-a", "demo-pkg")
	versions := versionsOf(memberDoc)
	if versions["1.5.0"] == nil {
		t.Errorf("the routed publish did not land in the deployment member: %v", versions)
	}
	if versions["2.0.0"] != nil {
		t.Errorf("the write plane leaked the merge into the member (2.0.0 copied): %v", versions)
	}
	// ...while the virtual's READ face still shows the union.
	_, virtDoc := f.packument(t, "npmv-virt", "demo-pkg")
	if versionsOf(virtDoc)["2.0.0"] == nil || versionsOf(virtDoc)["1.5.0"] == nil {
		t.Errorf("virtual read face lost the union: %v", versionsOf(virtDoc))
	}
}
