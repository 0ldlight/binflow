package cargo

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual-class integration tests (T-318, spec section 8's virtual
// row): the index merge (ruling ①'s first-seen dedup), first-hit download
// across a reordered member list, the remote member's pull-through inside
// the aggregate, the write route and its refusals, the search merge, and
// the first-hit yank family.

// setVirtualRoute writes the write-route config onto one virtual row (the
// service's config-time validation is not under test here — the raw row
// carries the route the adapter probes).
func (s *stack) setVirtualRoute(t *testing.T, virtual, target string) {
	t.Helper()
	row, err := s.md.Repos().Get(t.Context(), virtual)
	if err != nil {
		t.Fatalf("load virtual row %s: %v", virtual, err)
	}
	row.Config = `{"defaultDeploymentRepo":"` + target + `"}`
	if err := s.md.Repos().Update(t.Context(), row); err != nil {
		t.Fatalf("set route %s <- %s: %v", virtual, target, err)
	}
}

// newVirtualStack seeds the two-local-member fixture: cargo-a, cargo-b and
// the virtual cargo-v over [cargo-a, cargo-b].
func newVirtualStack(t *testing.T) *stack {
	t.Helper()
	s := newStack(t)
	s.seedRepo(t, "cargo-a", repo.TypeLocal)
	s.seedRepo(t, "cargo-b", repo.TypeLocal)
	s.seedRepo(t, "cargo-v", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-v", "cargo-a", "cargo-b")
	return s
}

// TestVirtualIndexMerge: the merged index file deduplicates on
// (name, vers ignoring build metadata) with the FIRST-SEEN member's raw
// row kept (ruling ①), orders by SemVer, and carries the computed
// validators (ETag over the rendered merge, 304 on If-None-Match) plus the
// base member's X-BinFlow-Resolved-From. A crate no member carries answers
// the honest 404.
func TestVirtualIndexMerge(t *testing.T) {
	s := newVirtualStack(t)

	crateA1 := fixtureCrate("shared", "0.1.0")
	bCopy := []byte("member-b-copy-of-shared-0.1.0")
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("shared", "0.1.0"), crateA1); status != http.StatusOK {
		t.Fatalf("a shared 0.1.0 = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("shared", "0.2.0"), fixtureCrate("shared", "0.2.0")); status != http.StatusOK {
		t.Fatalf("a shared 0.2.0 = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-b", fixtureMeta("shared", "0.1.0"), bCopy); status != http.StatusOK {
		t.Fatalf("b shared 0.1.0 = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-b", fixtureMeta("bonly", "0.3.0"), fixtureCrate("bonly", "0.3.0")); status != http.StatusOK {
		t.Fatalf("b bonly = %d (%s)", status, body)
	}

	status, body, hdr := s.get(repoPath("cargo-v") + "/index/sh/ar/shared")
	if status != http.StatusOK {
		t.Fatalf("merged shared index = %d (%s)", status, body)
	}
	if got := strings.Count(body, "\n"); got != 2 {
		t.Fatalf("merged shared index has %d rows, want exactly 2 (the duplicate 0.1.0 folded):\n%s", got, body)
	}
	if !strings.Contains(body, `"cksum":"`+sha256hex(crateA1)+`"`) {
		t.Errorf("merged shared index must keep member A's 0.1.0 row (first-seen), got:\n%s", body)
	}
	if strings.Contains(body, sha256hex(bCopy)) {
		t.Errorf("merged shared index must drop member B's duplicate 0.1.0 row, got:\n%s", body)
	}
	if i, j := strings.Index(body, `"vers":"0.1.0"`), strings.Index(body, `"vers":"0.2.0"`); i < 0 || j < 0 || i > j {
		t.Errorf("merged rows must be SemVer-ordered, got:\n%s", body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-a" {
		t.Errorf("merged index Resolved-From = %q, want cargo-a (the base member)", got)
	}

	// The conditional leg: the computed ETag answers 304.
	etag := hdr.Get("ETag")
	if etag == "" {
		t.Fatalf("merged index must carry an ETag over the rendered merge")
	}
	req := repoPath("cargo-v") + "/index/sh/ar/shared"
	status, _, _ = s.do(http.MethodGet, req, "", "", nil, map[string]string{"If-None-Match": etag})
	if status != http.StatusNotModified {
		t.Fatalf("merged index If-None-Match = %d, want 304", status)
	}

	// A crate only member B carries: B is the base of that merge.
	status, body, hdr = s.get(repoPath("cargo-v") + "/index/bo/nl/bonly")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.3.0"`) {
		t.Fatalf("merged bonly index = (%d, %s)", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-b" {
		t.Errorf("bonly Resolved-From = %q, want cargo-b", got)
	}

	// No member carries the crate: the honest 404.
	status, body, _ = s.get(repoPath("cargo-v") + "/index/no/su/nosuchcrate")
	if status != http.StatusNotFound {
		t.Fatalf("unknown crate merged index = (%d, %s), want 404", status, body)
	}

	// The build-metadata fold: 1.0.0+one and 1.0.0+two are ONE version
	// (the official uniqueness MUST) — the first-seen member's spelling.
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("bdmeta", "1.0.0+one"), fixtureCrate("bdmeta", "1.0.0+one")); status != http.StatusOK {
		t.Fatalf("a bdmeta = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-b", fixtureMeta("bdmeta", "1.0.0+two"), fixtureCrate("bdmeta", "1.0.0+two")); status != http.StatusOK {
		t.Fatalf("b bdmeta = %d (%s)", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-v") + "/index/bd/me/bdmeta")
	if status != http.StatusOK || strings.Count(body, "\n") != 1 || !strings.Contains(body, `"vers":"1.0.0+one"`) {
		t.Fatalf("merged bdmeta index = (%d, %s), want one first-seen row", status, body)
	}
}

// TestVirtualDownloadFirstHit: the download resolves through the member
// order — the first member holding the crate serves it, X-BinFlow-Resolved-From
// names it, and a member-list reorder is visible to the very next request
// (FR-15-AC6: resolution computes per request off the ledger).
func TestVirtualDownloadFirstHit(t *testing.T) {
	s := newVirtualStack(t)

	fromA := []byte("dup-bytes-from-member-a")
	fromB := []byte("dup-bytes-from-member-b")
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("dupcrate", "0.1.0"), fromA); status != http.StatusOK {
		t.Fatalf("a dupcrate = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-b", fixtureMeta("dupcrate", "0.1.0"), fromB); status != http.StatusOK {
		t.Fatalf("b dupcrate = %d (%s)", status, body)
	}

	status, body, hdr := s.get(repoPath("cargo-v") + "/v1/crates/dupcrate/0.1.0/download")
	if status != http.StatusOK || body != string(fromA) {
		t.Fatalf("virtual download = (%d, %q), want member A's bytes", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-a" {
		t.Errorf("virtual download Resolved-From = %q, want cargo-a", got)
	}

	// Reorder the member list: the next request resolves through B.
	s.seedVirtualMembers(t, "cargo-v", "cargo-b", "cargo-a")
	status, body, hdr = s.get(repoPath("cargo-v") + "/v1/crates/dupcrate/0.1.0/download")
	if status != http.StatusOK || body != string(fromB) {
		t.Fatalf("reordered virtual download = (%d, %q), want member B's bytes", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-b" {
		t.Errorf("reordered download Resolved-From = %q, want cargo-b", got)
	}

	// An unknown version keeps the pinned download 404.
	status, body, _ = s.get(repoPath("cargo-v") + "/v1/crates/dupcrate/9.9.9/download")
	if status != http.StatusNotFound || body != `{"errors":[{"detail":"unable to download crate"}]}` {
		t.Fatalf("unknown version download = (%d, %s), want the pinned 404", status, body)
	}
}

// TestVirtualRemoteMemberChain: a remote member inside the virtual — the
// merged index pulls the member's cached copy through the engine, the
// download first-hits through the pull-through chain, and the virtual
// search merges the remote member's upstream rows (the query-keyed marker
// IS that member's live upstream search).
func TestVirtualRemoteMemberChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-up", repo.TypeLocal)
	s.seedRepo(t, "cargo-rem", repo.TypeRemote)
	s.seedRemoteConfig(t, "cargo-rem", s.srv.URL+"/binflow/cargo-up")
	s.seedRepo(t, "cargo-a", repo.TypeLocal)
	s.seedRepo(t, "cargo-v", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-v", "cargo-a", "cargo-rem")

	crate := fixtureCrate("faraway", "0.4.0")
	if status, body, _ := s.publish(t, "cargo-up", fixtureMeta("faraway", "0.4.0"), crate); status != http.StatusOK {
		t.Fatalf("upstream faraway = %d (%s)", status, body)
	}

	// The merged index: cargo-a contributes nothing, the remote member's
	// pull-through copy is the merge's base.
	status, body, hdr := s.get(repoPath("cargo-v") + "/index/fa/ra/faraway")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.4.0"`) {
		t.Fatalf("virtual faraway index = (%d, %s), want the remote member's row", status, body)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-rem" {
		t.Errorf("virtual faraway index Resolved-From = %q, want cargo-rem", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first merged index fetch cache state = %q, want MISS", got)
	}

	// The download first-hits through the engine chain.
	status, body, hdr = s.get(repoPath("cargo-v") + "/v1/crates/faraway/0.4.0/download")
	if status != http.StatusOK || body != string(crate) {
		t.Fatalf("virtual faraway download = (%d, %d bytes), want the upstream crate", status, len(body))
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-rem" {
		t.Errorf("virtual download Resolved-From = %q, want cargo-rem", got)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
		t.Errorf("first virtual download cache state = %q, want MISS", got)
	}
	status, _, hdr = s.get(repoPath("cargo-v") + "/v1/crates/faraway/0.4.0/download")
	if status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("second virtual download must be a cache HIT (got %d, %q)", status, hdr.Get("X-BinFlow-Cache"))
	}

	// The search merge: the remote member's row arrives through its
	// query-keyed marker (the member's own upstream search, cached).
	status, body, _ = s.get(repoPath("cargo-v") + "/api/v1/crates?q=faraway&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"faraway"`) {
		t.Fatalf("virtual search = (%d, %s), want the remote member's row", status, body)
	}
}

// TestVirtualWriteRouting: a ROUTED virtual publish lands in the
// deployment member (crate, sidecar and index rewrite all addressed there)
// and is immediately visible through the aggregate; an un-routed virtual
// answers the C5 405 on every write face; DELETE never propagates — routed
// and un-routed each keep their truthful wording.
func TestVirtualWriteRouting(t *testing.T) {
	s := newVirtualStack(t)
	s.seedRepo(t, "cargo-unrouted", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-unrouted", "cargo-a", "cargo-b")
	s.setVirtualRoute(t, "cargo-v", "cargo-a")

	// The routed publish: 200, lands in cargo-a only.
	if status, body, _ := s.publish(t, "cargo-v", fixtureMeta("routed", "0.1.0"), fixtureCrate("routed", "0.1.0")); status != http.StatusOK {
		t.Fatalf("routed publish = %d (%s)", status, body)
	}
	status, body, _ := s.get(repoPath("cargo-a") + "/index/ro/ut/routed")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) {
		t.Fatalf("member A's own index = (%d, %s), want the routed crate's row", status, body)
	}
	status, _, _ = s.get(repoPath("cargo-b") + "/index/ro/ut/routed")
	if status != http.StatusNotFound {
		t.Fatalf("member B must not carry the routed crate (got %d)", status)
	}
	status, body, _ = s.get(repoPath("cargo-v") + "/index/ro/ut/routed")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) {
		t.Fatalf("virtual index after the routed publish = (%d, %s), want immediate visibility", status, body)
	}

	// The routed bare write: lands in the member, the convergence runs
	// against the member (the virtual cannot List).
	status, body, _ = s.put(repoPath("cargo-v")+"/docs/readme.txt", []byte("routed-bare"), nil)
	if status != http.StatusCreated {
		t.Fatalf("routed bare PUT = (%d, %s), want 201", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-a") + "/docs/readme.txt")
	if status != http.StatusOK || body != "routed-bare" {
		t.Fatalf("the routed bare write must land in member A, got (%d, %q)", status, body)
	}

	// The un-routed virtual: the C5 405 before any body drains.
	status, body, hdr := s.publish(t, "cargo-unrouted", fixtureMeta("nope", "0.1.0"), fixtureCrate("nope", "0.1.0"))
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed publish = (%d, %s), want 405", status, body)
	}
	if !strings.Contains(body, "No local repository was configured as local deployment repository for the (cargo-unrouted) virtual repository.") {
		t.Errorf("un-routed publish body = %s, want the C5 spelling", body)
	}
	if got := hdr.Get("Allow"); got != http.MethodGet {
		t.Errorf("un-routed publish Allow = %q, want GET", got)
	}
	status, body, _ = s.put(repoPath("cargo-unrouted")+"/docs/readme.txt", []byte("x"), nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "No local repository was configured") {
		t.Fatalf("un-routed bare PUT = (%d, %s), want the C5 405", status, body)
	}

	// DELETE never propagates, whatever the route.
	status, body, _ = s.delete(repoPath("cargo-unrouted") + "/index/ro/ut/routed")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "No local repository was configured") {
		t.Fatalf("un-routed virtual DELETE = (%d, %s), want the C5 405", status, body)
	}
	status, body, _ = s.delete(repoPath("cargo-v") + "/index/ro/ut/routed")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "Deletes are not propagated through the virtual repository") {
		t.Fatalf("routed virtual DELETE = (%d, %s), want the truthful 405", status, body)
	}
}

// TestVirtualSearchMerge: local members contribute stored-fact rows,
// remote members their upstream rows through the query marker, names union
// FIRST-SEEN (the member that would serve the download wins the name, even
// against a higher upstream version).
func TestVirtualSearchMerge(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-up", repo.TypeLocal)
	s.seedRepo(t, "cargo-rem", repo.TypeRemote)
	s.seedRemoteConfig(t, "cargo-rem", s.srv.URL+"/binflow/cargo-up")
	s.seedRepo(t, "cargo-a", repo.TypeLocal)
	s.seedRepo(t, "cargo-v", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-v", "cargo-a", "cargo-rem")

	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("alpha", "0.1.0"), fixtureCrate("alpha", "0.1.0")); status != http.StatusOK {
		t.Fatalf("a alpha = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("dupname", "0.1.0"), fixtureCrate("dupname", "0.1.0")); status != http.StatusOK {
		t.Fatalf("a dupname = %d (%s)", status, body)
	}
	if status, body, _ := s.publish(t, "cargo-up", fixtureMeta("dupname", "0.9.0"), fixtureCrate("dupname", "0.9.0")); status != http.StatusOK {
		t.Fatalf("up dupname = %d (%s)", status, body)
	}

	status, body, _ := s.get(repoPath("cargo-v") + "/api/v1/crates?q=alpha&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"alpha"`) {
		t.Fatalf("virtual search alpha = (%d, %s), want the local member's row", status, body)
	}

	status, body, _ = s.get(repoPath("cargo-v") + "/api/v1/crates?q=dupname&per_page=10")
	if status != http.StatusOK {
		t.Fatalf("virtual search dupname = (%d, %s)", status, body)
	}
	if !strings.Contains(body, `"max_version":"0.1.0"`) || strings.Contains(body, `"max_version":"0.9.0"`) {
		t.Errorf("virtual search dupname = %s, want the FIRST-SEEN member's row (0.1.0)", body)
	}

	// The bare term merges both members' rows.
	status, body, _ = s.get(repoPath("cargo-v") + "/api/v1/crates?per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"alpha"`) || !strings.Contains(body, `"name":"dupname"`) {
		t.Fatalf("virtual search bare = (%d, %s), want both members' rows", status, body)
	}
}

// TestVirtualYank: yank through the virtual follows FIRST-HIT — a local
// holder takes the flag and its own index rewrite (the merged row flips
// with it); a remote holder is a cached upstream copy and refuses; an
// unknown crate keeps TL-6's 404.
func TestVirtualYank(t *testing.T) {
	s := newVirtualStack(t)
	if status, body, _ := s.publish(t, "cargo-a", fixtureMeta("vflag", "0.1.0"), fixtureCrate("vflag", "0.1.0")); status != http.StatusOK {
		t.Fatalf("a vflag = %d (%s)", status, body)
	}

	status, body, _ := s.delete(repoPath("cargo-v") + "/api/v1/crates/vflag/0.1.0/yank")
	if status != http.StatusOK || body != `{"ok":true}` {
		t.Fatalf("virtual yank = (%d, %s), want 200 ok", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-v") + "/index/vf/la/vflag")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":true`) {
		t.Fatalf("merged index after yank = (%d, %s), want the flipped row", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-a") + "/index/vf/la/vflag")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":true`) {
		t.Fatalf("member index after yank = (%d, %s), want the flag where the crate lives", status, body)
	}

	status, body, _ = s.put(repoPath("cargo-v")+"/api/v1/crates/vflag/0.1.0/unyank", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("virtual unyank = (%d, %s)", status, body)
	}
	status, body, _ = s.get(repoPath("cargo-v") + "/index/vf/la/vflag")
	if status != http.StatusOK || strings.Contains(body, `"yanked":true`) {
		t.Fatalf("merged index after unyank = (%d, %s), want the flag gone", status, body)
	}

	// A remote holder: the read-only refusal.
	s2 := newStack(t)
	s2.seedRepo(t, "cargo-up", repo.TypeLocal)
	s2.seedRepo(t, "cargo-rem", repo.TypeRemote)
	s2.seedRemoteConfig(t, "cargo-rem", s2.srv.URL+"/binflow/cargo-up")
	s2.seedRepo(t, "cargo-v", repo.TypeVirtual)
	s2.seedVirtualMembers(t, "cargo-v", "cargo-rem")
	if status, body, _ := s2.publish(t, "cargo-up", fixtureMeta("cached", "0.1.0"), fixtureCrate("cached", "0.1.0")); status != http.StatusOK {
		t.Fatalf("up cached = %d (%s)", status, body)
	}
	if status, _, _ := s2.get(repoPath("cargo-v") + "/v1/crates/cached/0.1.0/download"); status != http.StatusOK {
		t.Fatalf("virtual cached warmup = %d", status)
	}
	status, body, _ = s2.delete(repoPath("cargo-v") + "/api/v1/crates/cached/0.1.0/yank")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
		t.Fatalf("yank of a remote-held crate = (%d, %s), want the read-only 405", status, body)
	}

	// TL-6: the unknown crate answers 404 through the virtual too.
	status, body, _ = s.delete(repoPath("cargo-v") + "/api/v1/crates/nosuch/0.1.0/yank")
	if status != http.StatusNotFound {
		t.Fatalf("unknown crate yank = (%d, %s), want 404", status, body)
	}
}
