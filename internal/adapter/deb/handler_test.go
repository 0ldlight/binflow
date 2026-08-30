package deb

// The debPUT + index-engine wire matrix through the real router: the
// FR-97 AC family — AC1's chain (coordinates land, indexes materialize,
// the checksum chain reconciles), AC2's refusal face (DB-2 400 / DB-3
// 403), AC3's by-hash history, TL-4's forced architecture families, the
// delete联动, the /api/deb management matrix and the class doors.

import (
	"context"
	"crypto/md5" //nolint:gosec // test-side digest of test bytes
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// sha256Hex digests one body (test-side helper).
func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// debPut issues the authenticated debPUT with matrix coordinates.
func (s *stack) debPut(t *testing.T, path string, body []byte, dist string, comps, arches []string) (int, string, http.Header) {
	t.Helper()
	var matrix strings.Builder
	if dist != "" {
		fmt.Fprintf(&matrix, ";deb.distribution=%s", dist)
	}
	for _, c := range comps {
		fmt.Fprintf(&matrix, ";deb.component=%s", c)
	}
	for _, a := range arches {
		fmt.Fprintf(&matrix, ";deb.architecture=%s", a)
	}
	return s.put(path+matrix.String(), body, nil)
}

// pollWindow scales an async-recompute polling deadline by build posture —
// FR-121 escape #2 (T-361, race recalibration; NOT a skip): 3x under -race.
// The deb full-load family's flake line (T-313 TestDeleteDropsEntry,
// T-318 "two rounds, different tests, both green isolated", T-329/T-340
// full-tree misses) is exactly these windows starving while the detector
// plus whole-tree `make test` parallelism inflate the background recompute
// chains — the 20s base carried big headroom over the ~1s the recompute
// actually needs, and 60s under -race restores that headroom (~45x the
// work) without weakening a single behind-the-window assertion.
func pollWindow(base time.Duration) time.Duration {
	if raceEnabled { // FR-121 escape #2 — every raceEnabled call site below
		return 3 * base
	}
	return base
}

// waitIndex polls a path until it serves 200 (the debPUT chain's async
// recompute) or the deadline passes.
func (s *stack) waitIndex(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(pollWindow(20 * time.Second)) // FR-121 escape #2
	for time.Now().Before(deadline) {
		status, body, _ := s.get(path)
		if status == http.StatusOK {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("index %s never materialized (the async recompute stalled?)", path)
	return ""
}

// helloDeb builds one canonical test package for stable/main/amd64.
func helloDeb(name, version, arch string) []byte {
	return fixtureDeb("gz", controlFixture(name, version, arch,
		"Maintainer: BinFlow Test <t@binflow.dev>",
		"Depends: libc6 (>= 2.14)",
		"Description: BinFlow test package\n The long description tail."))
}

// ---- AC1: the debPUT chain ----

// TestDebPutRegistersAndIndexes: the full automatic chain — PUT with
// matrix coordinates lands 201, the coordinate properties register, the
// background recompute materializes Packages (+ .gz) and Release, and
// the Packages stanza's Filename/Size/SHA256 reconcile with the wire.
func TestDebPutRegistersAndIndexes(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)

	pkg := helloDeb("mypkg", "1.0", "amd64")
	path := "/binflow/deb-local/pool/main/m/mypkg/mypkg_1.0_amd64.deb"
	status, body, hdr := s.debPut(t, path, pkg, "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s), want 201", status, body)
	}
	if got := hdr.Get("X-Checksum-Sha256"); got != sha256Hex(pkg) {
		t.Errorf("PUT X-Checksum-Sha256 = %q, want the storage-measured digest", got)
	}

	// The coordinate registration (the index engine's source of truth).
	props, err := s.md.NodeProps().List(context.Background(), "deb-local",
		"pool/main/m/mypkg/mypkg_1.0_amd64.deb")
	if err != nil {
		t.Fatalf("NodeProps.List: %v", err)
	}
	if got := strings.Join(props[PropDebDistribution], ","); got != "stable" {
		t.Errorf("deb.distribution = %q", got)
	}
	if got := strings.Join(props[PropDebComponent], ","); got != "main" {
		t.Errorf("deb.component = %q", got)
	}
	if got := strings.Join(props[PropDebArchitecture], ","); got != "amd64" {
		t.Errorf("deb.architecture = %q", got)
	}
	if got := strings.Join(props["deb.metadata.package"], ","); got != "mypkg" {
		t.Errorf("deb.metadata.package = %q", got)
	}

	// The automatic index: Packages + .gz + Release, the stanza's
	// checksum chain against the served .deb bytes (section 7).
	packages := s.waitIndex(t, "/binflow/deb-local/dists/stable/main/binary-amd64/Packages")
	for _, want := range []string{
		"Package: mypkg\n",
		"Version: 1.0\n",
		"Architecture: amd64\n",
		"Filename: pool/main/m/mypkg/mypkg_1.0_amd64.deb\n",
		"SHA256: " + sha256Hex(pkg) + "\n",
	} {
		if !strings.Contains(packages, want) {
			t.Errorf("Packages stanza missing %q:\n%s", want, packages)
		}
	}
	s.waitIndex(t, "/binflow/deb-local/dists/stable/main/binary-amd64/Packages.gz")

	release := s.waitIndex(t, "/binflow/deb-local/dists/stable/Release")
	for _, want := range []string{
		"Suite: stable\n", "Codename: stable\n",
		"Components: main\n",
		"Date: ", "MD5Sum:", "SHA1:", "SHA256:",
		" main/binary-amd64/Packages\n",
		" main/binary-amd64/Packages.gz\n",
	} {
		if !strings.Contains(release, want) {
			t.Errorf("Release missing %q:\n%s", want, release)
		}
	}
	if strings.Contains(release, "Acquire-By-Hash") {
		t.Error("byHash=NONE default must not advertise Acquire-By-Hash")
	}

	// The .deb serves verbatim with the checksum family (the download
	// face apt consumes).
	status, body, hdr = s.get(path)
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(pkg) {
		t.Fatalf("package GET = %d (%d bytes), want the stored bytes", status, len(body))
	}
	if hdr.Get("X-Checksum-Sha256") != sha256Hex(pkg) {
		t.Errorf("GET X-Checksum-Sha256 = %q", hdr.Get("X-Checksum-Sha256"))
	}
}

// TestTL4ForcedArchitectures: the board's final ruling — i386,amd64
// Packages generate for every component even when EMPTY (an all-arch
// package plus the forced families, the singular/plural Architecture
// line, and the config knob that reshapes the set).
func TestTL4ForcedArchitectures(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-forced", repo.TypeLocal, `{}`)

	pkg := helloDeb("anyway", "1.0", "all")
	status, body, _ := s.debPut(t, "/binflow/deb-forced/pool/main/a/anyway/anyway_1.0_all.deb",
		pkg, "stable", []string{"main"}, []string{"all"})
	if status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, body)
	}

	// binary-all carries the entry; the forced families materialize EMPTY.
	s.waitIndex(t, "/binflow/deb-forced/dists/stable/main/binary-all/Packages")
	if all := s.waitIndex(t, "/binflow/deb-forced/dists/stable/main/binary-all/Packages"); !strings.Contains(all, "Package: anyway") {
		t.Errorf("binary-all Packages lost the entry:\n%s", all)
	}
	i386 := s.waitIndex(t, "/binflow/deb-forced/dists/stable/main/binary-i386/Packages")
	if strings.Contains(i386, "Package:") {
		t.Errorf("forced i386 Packages must be EMPTY (TL-4 generates the file, not the entry):\n%s", i386)
	}
	s.waitIndex(t, "/binflow/deb-forced/dists/stable/main/binary-amd64/Packages")

	// The pseudo architecture never joins the Release arch line; the
	// forced families do.
	release := s.waitIndex(t, "/binflow/deb-forced/dists/stable/Release")
	if !strings.Contains(release, "Architectures: amd64 i386\n") {
		t.Errorf("Release Architectures line wrong (pseudo any/all must be filtered):\n%s", release)
	}
}

// TestMultiCoordinateRegistration: the repeated-key multi-value rule —
// one .deb registers into two architectures at once.
func TestMultiCoordinateRegistration(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-multi", repo.TypeLocal, `{}`)
	pkg := helloDeb("dual", "1.0", "any")
	status, body, _ := s.debPut(t, "/binflow/deb-multi/pool/main/d/dual/dual_1.0.deb",
		pkg, "stable", []string{"main"}, []string{"amd64", "i386"})
	if status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, body)
	}
	for _, arch := range []string{"amd64", "i386"} {
		p := s.waitIndex(t, fmt.Sprintf("/binflow/deb-multi/dists/stable/main/binary-%s/Packages", arch))
		if !strings.Contains(p, "Package: dual") {
			t.Errorf("binary-%s Packages lost the multi-registered entry:\n%s", arch, p)
		}
	}
}

// ---- AC2: the refusal face ----

// TestMissingCoordinatesRefused: DB-2 — every incomplete coordinate
// triple answers 400 (the board-confirmed tightening), table-driven.
func TestMissingCoordinatesRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	pkg := helloDeb("orphan", "1.0", "amd64")
	tests := []struct {
		name  string
		path  string
		dist  string
		comps []string
		archs []string
	}{
		{"no coordinates at all", "/binflow/deb-local/pool/main/o/orphan/orphan_1.0_amd64.deb", "", nil, nil},
		{"no architecture", "/binflow/deb-local/pool/main/o/orphan/orphan_1.0_amd64.deb", "stable", []string{"main"}, nil},
		{"no component", "/binflow/deb-local/pool/main/o/orphan/orphan_1.0_amd64.deb", "stable", nil, []string{"amd64"}},
		{"no distribution", "/binflow/deb-local/pool/main/o/orphan/orphan_1.0_amd64.deb", "", []string{"main"}, []string{"amd64"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body, _ := s.debPut(t, tt.path, pkg, tt.dist, tt.comps, tt.archs)
			if status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", status, body)
			}
			if !strings.Contains(body, "deb.distribution") {
				t.Errorf("400 body must carry the coordinate example: %s", body)
			}
		})
	}

	// Nothing stored: the refusal happens BEFORE the landing.
	if status, _, _ := s.get("/binflow/deb-local/pool/main/o/orphan/orphan_1.0_amd64.deb"); status != http.StatusNotFound {
		t.Errorf("refused .deb stored anyway (status %d)", status)
	}
}

// TestIllegalCoordinateRefused: the token floor (path-traversal
// coordinates never become index paths).
func TestIllegalCoordinateRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	pkg := helloDeb("evil", "1.0", "amd64")
	status, body, _ := s.debPut(t, "/binflow/deb-local/pool/main/e/evil/evil_1.0_amd64.deb",
		pkg, "../../etc", []string{"main"}, []string{"amd64"})
	if status != http.StatusBadRequest {
		t.Fatalf("traversal coordinate status = %d, want 400 (body %s)", status, body)
	}
}

// TestIndexDirectWriteForbidden: DB-3 — the generated family refuses
// client PUT and DELETE with the 403 posture, table-driven over the
// pattern's edges; the read side keeps serving.
func TestIndexDirectWriteForbidden(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	pkg := helloDeb("locked", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-local/pool/main/l/locked/locked_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	release := s.waitIndex(t, "/binflow/deb-local/dists/stable/Release")

	targets := []string{
		"/binflow/deb-local/dists/stable/Release",
		"/binflow/deb-local/dists/stable/Release.gpg",
		"/binflow/deb-local/dists/stable/InRelease",
		"/binflow/deb-local/dists/stable/main/binary-amd64/Packages",
		"/binflow/deb-local/dists/stable/main/binary-amd64/Packages.gz",
		"/binflow/deb-local/dists/stable/main/binary-amd64/by-hash/SHA256/" + sha256Hex([]byte(release)),
	}
	for _, path := range targets {
		t.Run("PUT "+path, func(t *testing.T) {
			status, body, _ := s.put(path, []byte("hand-written index"), nil)
			if status != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %s)", status, body)
			}
			if !strings.Contains(body, path[len("/binflow/deb-local/"):]) {
				t.Errorf("403 body must name the path: %s", body)
			}
			if !strings.Contains(body, "debPUT") {
				t.Errorf("403 body must point at the debPUT chain: %s", body)
			}
		})
	}
	// DELETE on the generated family is the same 403.
	if status, body, _ := s.delete("/binflow/deb-local/dists/stable/Release"); status != http.StatusForbidden {
		t.Fatalf("index DELETE status = %d, want 403 (body %s)", status, body)
	}
	// A non-index path under dists/ stays plain storage (the PRD's
	// enumerated pattern: only the index family is refused).
	if status, body, _ := s.put("/binflow/deb-local/dists/stable/README", []byte("hi"), nil); status != http.StatusCreated {
		t.Errorf("dists README PUT = (%d, %s), want 201", status, body)
	}
	// The read side keeps serving.
	if status, _, _ := s.get("/binflow/deb-local/dists/stable/Release"); status != http.StatusOK {
		t.Errorf("Release GET = %d, want 200", status)
	}
}

// TestNonDebBodyStillStores: a body that fails the control parse stores
// (the rpm parity) and never enters the index.
func TestNonDebBodyStillStores(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	status, body, _ := s.debPut(t, "/binflow/deb-local/pool/main/j/junk/junk_1.0_amd64.deb",
		[]byte("definitely not a deb"), "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusCreated {
		t.Fatalf("non-deb PUT = (%d, %s), want 201", status, body)
	}
	packages := s.waitIndex(t, "/binflow/deb-local/dists/stable/main/binary-amd64/Packages")
	if strings.Contains(packages, "junk") {
		t.Errorf("the unparsable package leaked into the index:\n%s", packages)
	}
}

// ---- AC3: By-Hash ----

// TestByHashHistory: the by-hash families write (ALL policy: three
// algorithms), the Release advertises Acquire-By-Hash, and the OLD
// digest's address survives an index update (the L-d5 assertion).
func TestByHashHistory(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-hash", repo.TypeLocal, `{"byHash":"ALL"}`)

	v1 := helloDeb("hashy", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-hash/pool/main/h/hashy/hashy_1.0_amd64.deb",
		v1, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v1 debPUT = (%d, %s)", status, b)
	}
	release1 := s.waitIndex(t, "/binflow/deb-hash/dists/stable/Release")
	if !strings.Contains(release1, "Acquire-By-Hash: yes\n") {
		t.Fatalf("byHash=ALL must advertise Acquire-By-Hash:\n%s", release1)
	}
	// The by-hash address of the v1 Packages resolves to the same bytes.
	pkg1 := s.waitIndex(t, "/binflow/deb-hash/dists/stable/main/binary-amd64/Packages")
	digest1 := sha256Hex([]byte(pkg1))
	byHash1 := "/binflow/deb-hash/dists/stable/main/binary-amd64/by-hash/SHA256/" + digest1
	if status, body, _ := s.get(byHash1); status != http.StatusOK || body != pkg1 {
		t.Fatalf("v1 by-hash address = %d, want the same bytes", status)
	}

	// v2 lands: the index content moves, the old digest's address stays
	// (the ≥2-generation guarantee).
	v2 := helloDeb("hashy", "2.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-hash/pool/main/h/hashy/hashy_2.0_amd64.deb",
		v2, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v2 debPUT = (%d, %s)", status, b)
	}
	pkg2 := s.waitIndex2(t, "/binflow/deb-hash/dists/stable/main/binary-amd64/Packages", "hashy_2.0")
	if sha256Hex([]byte(pkg2)) == digest1 {
		t.Fatal("the Packages index did not move between versions")
	}
	if status, body, _ := s.get(byHash1); status != http.StatusOK || body != pkg1 {
		t.Fatalf("old by-hash address = %d, want the historical generation kept", status)
	}
	// The new digest's address serves too, and the MD5Sum/SHA1 families
	// exist under the ALL policy.
	newPath := "/binflow/deb-hash/dists/stable/main/binary-amd64/by-hash/SHA256/" + sha256Hex([]byte(pkg2))
	if got, _, _ := s.get(newPath); got != http.StatusOK {
		t.Errorf("new by-hash address = %d", got)
	}
	if got, _, _ := s.get("/binflow/deb-hash/dists/stable/main/binary-amd64/by-hash/MD5Sum/" + md5Of([]byte(pkg2))); got != http.StatusOK {
		t.Errorf("by-hash MD5Sum family = %d, want 200 (ALL policy)", got)
	}
}

// waitIndex2 polls until the path serves 200 AND the body contains want.
func (s *stack) waitIndex2(t *testing.T, path, want string) string {
	t.Helper()
	deadline := time.Now().Add(pollWindow(20 * time.Second)) // FR-121 escape #2
	var last string
	for time.Now().Before(deadline) {
		status, body, _ := s.get(path)
		last = body
		if status == http.StatusOK && strings.Contains(body, want) {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("index %s never showed %q (last:\n%s)", path, want, last)
	return ""
}

// ---- the delete chain ----

// TestDeleteDropsEntry: the L-d6 assertion — deleting the LAST package
// of a distribution sweeps its whole index tree (no components left, the
// forced families have no component to hang on), and a fresh debPUT
// rebuilds it.
func TestDeleteDropsEntry(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-del", repo.TypeLocal, `{}`)
	pkg := helloDeb("vanisher", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-del/pool/main/v/vanisher/vanisher_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	// Wait for the run's LAST write (Release) so the delete cannot race
	// the debPUT's own recompute.
	s.waitIndex(t, "/binflow/deb-del/dists/stable/Release")

	if status, b, _ := s.delete("/binflow/deb-del/pool/main/v/vanisher/vanisher_1.0_amd64.deb"); status != http.StatusNoContent {
		t.Fatalf("DELETE = (%d, %s)", status, b)
	}
	// The async recompute sweeps the emptied distribution: no components,
	// no index family, no Release (the sweep's stale-file arm). This poll
	// is the T-313/T-340 flake site itself — the race-scaled window is
	// FR-121 escape #2's headline call site. The settle waits for BOTH
	// swept paths, not Release alone: the tree sweep removes the family
	// sequentially, so a Release-only poll can return with Packages still
	// mid-deletion and the immediate assert below would read the gap
	// (FR-121 escape #3, same shape the T-361 round-B run caught in the
	// by-hash cycler).
	deadline := time.Now().Add(pollWindow(20 * time.Second)) // FR-121 escape #2 (T-313/T-340 flake site)
	for time.Now().Before(deadline) {
		r, _, _ := s.get("/binflow/deb-del/dists/stable/Release")
		p, _, _ := s.get("/binflow/deb-del/dists/stable/main/binary-amd64/Packages")
		if r == http.StatusNotFound && p == http.StatusNotFound {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, path := range []string{
		"/binflow/deb-del/dists/stable/Release",
		"/binflow/deb-del/dists/stable/main/binary-amd64/Packages",
	} {
		if status, _, _ := s.get(path); status != http.StatusNotFound {
			t.Errorf("%s after the emptying delete = %d, want the swept 404", path, status)
		}
	}

	// A fresh debPUT rebuilds the tree (the engine is not one-shot).
	pkg2 := helloDeb("reviver", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-del/pool/main/r/reviver/reviver_1.0_amd64.deb",
		pkg2, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("rebuild debPUT = (%d, %s)", status, b)
	}
	revived := s.waitIndex2(t, "/binflow/deb-del/dists/stable/main/binary-amd64/Packages", "Package: reviver")
	if strings.Contains(revived, "vanisher") {
		t.Errorf("the deleted package's stanza survived the rebuild:\n%s", revived)
	}
	s.waitIndex(t, "/binflow/deb-del/dists/stable/Release")
}

// ---- the .dsc / Sources face ----

// TestDscChain: the source chain — coordinate validation (the dsc.*
// prefix), the Sources stanza (Package rename, Directory, the checksums
// pass-through) and the DB-2 refusal on the .dsc prefix.
func TestDscChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-src", repo.TypeLocal, `{}`)
	dsc := fixtureDsc("srcpkg", "1.0-1",
		"Checksums-Sha256:\n aaaa 111 srcpkg_1.0.tar.gz\n bbbb 222 srcpkg_1.0-1.dsc\n")
	status, body, _ := s.put("/binflow/deb-src/pool/main/s/srcpkg/srcpkg_1.0-1.dsc;dsc.distribution=stable;dsc.component=main", dsc, nil)
	if status != http.StatusCreated {
		t.Fatalf("dsc PUT = (%d, %s)", status, body)
	}
	sources := s.waitIndex(t, "/binflow/deb-src/dists/stable/main/source/Sources")
	for _, want := range []string{
		"Package: srcpkg\n", // the Source->Package rename
		"Version: 1.0-1\n",
		"Directory: pool/main/s/srcpkg\n",
		" srcpkg_1.0-1.dsc\n", // the checksums section rides verbatim
	} {
		if !strings.Contains(sources, want) {
			t.Errorf("Sources stanza missing %q:\n%s", want, sources)
		}
	}

	// A .dsc without its dsc.* coordinates answers the same 400 family.
	status, body, _ = s.put("/binflow/deb-src/pool/main/s/srcpkg/srcpkg_2.0-1.dsc", dsc, nil)
	if status != http.StatusBadRequest {
		t.Fatalf("coordinate-less dsc PUT = (%d, %s), want 400", status, body)
	}
	// A broken .dsc body refuses (a .dsc is the Sources index's only
	// input — the store-and-warn posture is the .deb face's).
	status, body, _ = s.put("/binflow/deb-src/pool/main/s/srcpkg/broken.dsc;dsc.distribution=stable;dsc.component=main", []byte("junk"), nil)
	if status != http.StatusBadRequest {
		t.Fatalf("broken dsc PUT = (%d, %s), want 400", status, body)
	}
}

// ---- the management plane (/api/deb, through the real router) ----

// TestDebReindexMatrix: the section 4.3 branch matrix, table-driven.
func TestDebReindexMatrix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	s.seedRepo(t, "deb-virtual", repo.TypeVirtual, `{}`)

	// A plain generic repo row for the non-debian branch.
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "not-deb", Type: repo.TypeLocal, PackageType: "generic", Config: "{}",
	}); err != nil {
		t.Fatalf("seed generic repo: %v", err)
	}

	tests := []struct {
		name    string
		method  string
		path    string
		status  int
		bodyHas string
	}{
		{"blank key", http.MethodPost, "/binflow/api/deb/reindex/", http.StatusBadRequest, "cannot be blank"},
		{"missing repo", http.MethodPost, "/binflow/api/deb/reindex/nope", http.StatusNotFound, "Unable to find repository 'nope'."},
		{"non-debian repo", http.MethodPost, "/binflow/api/deb/reindex/not-deb", http.StatusBadRequest, "doesn't handle debian requests"},
		{"virtual class", http.MethodPost, "/binflow/api/deb/reindex/deb-virtual", http.StatusBadRequest, "virtual stanza aggregation land"},
		{"wrong verb", http.MethodGet, "/binflow/api/deb/reindex/deb-local", http.StatusNotFound, "is not implemented"},
		{"unknown spelling", http.MethodPost, "/binflow/api/deb/other/deb-local", http.StatusNotFound, "is not implemented"},
		{"bad async", http.MethodPost, "/binflow/api/deb/reindex/deb-local?async=2", http.StatusBadRequest, "async must be 0 or 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body, _ := s.do(tt.method, tt.path, adminUser, adminPass, nil, nil)
			if status != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", status, tt.status, body)
			}
			if tt.bodyHas != "" && !strings.Contains(body, tt.bodyHas) {
				t.Errorf("body %q must contain %q", body, tt.bodyHas)
			}
		})
	}
}

// TestDebReindexSync: async=0 recomputes the whole repository
// synchronously (a repository whose debPUTs predate the adapter — the
// properties present, no index run yet — materializes on demand).
func TestDebReindexSync(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-sync", repo.TypeLocal, `{}`)
	pkg := helloDeb("syncer", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-sync/pool/main/s/syncer/syncer_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-sync/dists/stable/Release")

	status, body, _ := s.post("/binflow/api/deb/reindex/deb-sync?async=0")
	if status != http.StatusOK {
		t.Fatalf("sync reindex = (%d, %s)", status, body)
	}
	if !strings.Contains(body, "Debian index calculation for repository 'deb-sync' accepted.") {
		t.Errorf("reindex body = %q", body)
	}
}

// TestDebReindexAsync: async=1 answers 202 immediately and the sweep
// lands in the background.
func TestDebReindexAsync(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-async", repo.TypeLocal, `{}`)
	status, body, _ := s.post("/binflow/api/deb/reindex/deb-async?async=1")
	if status != http.StatusAccepted {
		t.Fatalf("async reindex = (%d, %s), want 202", status, body)
	}
}

// ---- class doors and the plain faces ----

// TestClassDoors: the class doors behave — a memberless virtual answers
// the plain not-found (the aggregate face serves since T-314, an empty
// member set aggregates nothing), the root probe and the unknown-repo
// door keep their shapes.
func TestClassDoors(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-virtual", repo.TypeVirtual, `{}`)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	status, body, _ := s.get("/binflow/deb-virtual/dists/stable/Release")
	if status != http.StatusNotFound || !strings.Contains(body, "'deb-virtual/dists/stable/Release' not found") {
		t.Fatalf("virtual content = (%d, %s)", status, body)
	}
	// The root probe on a live local repository.
	if status, _, _ := s.get("/binflow/deb-local"); status != http.StatusOK {
		t.Errorf("root probe = %d, want 200", status)
	}
	// The unknown-repository door.
	if status, _, _ := s.get("/binflow/deb-nope/anything.deb"); status != http.StatusNotFound {
		t.Errorf("unknown repo content = %d, want 404", status)
	}
}

// TestPlainFileFaces: pool companion files (orig.tar.gz and friends)
// ride the plain storage face — PUT lands verbatim, GET streams.
func TestPlainFileFaces(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	tarball := []byte("pretend orig tarball bytes")
	status, body, _ := s.put("/binflow/deb-local/pool/main/h/hello/hello_2.10.orig.tar.gz", tarball, nil)
	if status != http.StatusCreated {
		t.Fatalf("orig.tar PUT = (%d, %s)", status, body)
	}
	status, body, _ = s.get("/binflow/deb-local/pool/main/h/hello/hello_2.10.orig.tar.gz")
	if status != http.StatusOK || string(body) != string(tarball) {
		t.Fatalf("orig.tar GET = %d (%d bytes)", status, len(body))
	}
}

// TestChecksumHeaderGates: the X-Checksum family (malformed 400 /
// mismatch 409), the shared contract.
func TestChecksumHeaderGates(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	pkg := helloDeb("sums", "1.0", "amd64")
	path := "/binflow/deb-local/pool/main/s/sums/sums_1.0_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64"

	if status, body, _ := s.put(path, pkg, map[string]string{"X-Checksum-Sha256": "not-hex"}); status != http.StatusBadRequest {
		t.Fatalf("malformed checksum status = %d (body %s)", status, body)
	}
	if status, body, _ := s.put(path, pkg, map[string]string{"X-Checksum-Sha256": strings.Repeat("a", 64)}); status != http.StatusConflict {
		t.Fatalf("mismatched checksum status = %d (body %s)", status, body)
	}
	if status, body, _ := s.put(path, pkg, map[string]string{"X-Checksum-Sha256": sha256Hex(pkg)}); status != http.StatusCreated {
		t.Fatalf("matching checksum status = %d (body %s)", status, body)
	}
}

// TestUnsignedSweep: DB-1 — stale signature files never survive a
// recompute (pre-seed them, reindex, both gone).
func TestUnsignedSweep(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-unsig", repo.TypeLocal, `{}`)
	pkg := helloDeb("sweeper", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-unsig/pool/main/s/sweeper/sweeper_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-unsig/dists/stable/Release")
	// Seed stale signatures directly through the service (the client face
	// refuses writes into the family — DB-3's own guarantee).
	admin := &repo.Principal{Name: "admin", Admin: true}
	for _, p := range []string{"dists/stable/Release.gpg", "dists/stable/InRelease"} {
		if _, err := s.svc.Put(context.Background(), admin, "deb-unsig", p,
			strings.NewReader("stale signature"), storage.BlobRef{Sha256: sha256Hex([]byte("stale signature"))},
			"application/pgp-signature"); err != nil {
			t.Fatalf("seed stale signature %s: %v", p, err)
		}
	}
	if status, b, _ := s.post("/binflow/api/deb/reindex/deb-unsig?async=0"); status != http.StatusOK {
		t.Fatalf("reindex = (%d, %s)", status, b)
	}
	for _, p := range []string{"dists/stable/Release.gpg", "dists/stable/InRelease"} {
		if status, _, _ := s.get("/binflow/deb-unsig/" + p); status != http.StatusNotFound {
			t.Errorf("stale %s survived the unsigned recompute (status %d)", p, status)
		}
	}
}

// TestByHashPolicySha256: the SHA256 policy — Release carries ONLY the
// SHA256 section (the section-trimming rule) and the by-hash tree has
// only the SHA256 family.
func TestByHashPolicySha256(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-sha", repo.TypeLocal, `{"byHash":"SHA256"}`)
	pkg := helloDeb("trim", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-sha/pool/main/t/trim/trim_1.0_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	release := s.waitIndex(t, "/binflow/deb-sha/dists/stable/Release")
	if !strings.Contains(release, "Acquire-By-Hash: yes\n") {
		t.Fatalf("byHash=SHA256 must advertise Acquire-By-Hash:\n%s", release)
	}
	if strings.Contains(release, "MD5Sum:") || strings.Contains(release, "\nSHA1:") {
		t.Errorf("byHash=SHA256 must trim the Release to the SHA256 section:\n%s", release)
	}
	if !strings.Contains(release, "SHA256:") {
		t.Error("the SHA256 section itself must stay")
	}
	if status, _, _ := s.get("/binflow/deb-sha/dists/stable/main/binary-amd64/by-hash/SHA256/" + sha256Hex([]byte(s.waitIndex(t, "/binflow/deb-sha/dists/stable/main/binary-amd64/Packages")))); status != http.StatusOK {
		t.Errorf("SHA256 by-hash family = %d", status)
	}
	if status, _, _ := s.get("/binflow/deb-sha/dists/stable/main/binary-amd64/by-hash/MD5Sum/whatever"); status != http.StatusNotFound {
		t.Errorf("MD5Sum by-hash family = %d, want 404 (SHA256 policy)", status)
	}
}

// md5Of digests one body (test-side helper).
func md5Of(b []byte) string {
	m := md5.Sum(b) //nolint:gosec // test-side digest of test bytes
	return hex.EncodeToString(m[:])
}
