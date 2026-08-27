package deb

// The virtual-class legs (debian.md section 8's virtual row / S10): the
// stanza merge with the Release recomputed at the virtual root, the
// first-found downloads, the routed debPUT (the recompute targeting the
// MEMBER), the write refusals, the unserved faces (signatures, by-hash)
// and the remote-member aggregation with its failure tolerance.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// seedVirtualMembers assembles two local members with disjoint facts:
// memberA carries alpha stable/main/amd64, memberB carries beta
// stable/contrib/i386 plus a same-address copy of alpha (the dedupe
// leg's input).
func seedVirtualMembers(t *testing.T, s *stack, memberA, memberB string, withDuplicate bool) {
	t.Helper()
	s.seedRepo(t, memberA, repo.TypeLocal, `{}`)
	alpha := helloDeb("alpha", "1.0", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/"+memberA+"/pool/main/a/alpha/alpha_1.0_amd64.deb",
		alpha, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("memberA debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/"+memberA+"/dists/stable/Release")

	s.seedRepo(t, memberB, repo.TypeLocal, `{}`)
	beta := helloDeb("beta", "2.0", "i386")
	if status, b, _ := s.debPut(t, "/binflow/"+memberB+"/pool/contrib/b/beta/beta_2.0_i386.deb",
		beta, "stable", []string{"contrib"}, []string{"i386"}); status != http.StatusCreated {
		t.Fatalf("memberB debPUT = (%d, %s)", status, b)
	}
	if withDuplicate {
		// The same ADDRESS memberA serves, DIFFERENT bytes, registered in
		// memberB's own index (a plain coordinate-less land would hit
		// DB-2's 400): the merge must keep memberA's row (first-seen),
		// whose checksums describe the file the first-found walk actually
		// serves.
		dup := fixtureDeb("xz", controlFixture("alpha", "1.0", "amd64",
			"Maintainer: Dup <d@binflow.dev>", "Description: memberB's different bytes"))
		if status, b, _ := s.debPut(t, "/binflow/"+memberB+"/pool/main/a/alpha/alpha_1.0_amd64.deb",
			dup, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
			t.Fatalf("memberB duplicate land = (%d, %s)", status, b)
		}
	}
	s.waitIndex(t, "/binflow/"+memberB+"/dists/stable/Release")
}

// TestVirtualReleaseAggregates: the Release recomputes at the virtual
// root with the members' union, and the checksum sections describe the
// VIRTUAL's own rendered bytes (the section 7 obligation on the
// aggregate).
func TestVirtualReleaseAggregates(t *testing.T) {
	s := newStack(t)
	seedVirtualMembers(t, s, "deb-va", "deb-vb", false)
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-va", "deb-vb"}, "")

	status, release, _ := s.get("/binflow/deb-virt/dists/stable/Release")
	if status != http.StatusOK {
		t.Fatalf("virtual Release = %d", status)
	}
	for _, want := range []string{
		"Components: contrib main\n",
		"Architectures: amd64 i386\n",
		"Origin: deb-virt\n",
		"Suite: stable\n",
	} {
		if !strings.Contains(release, want) {
			t.Errorf("virtual Release lacks %q:\n%s", want, release)
		}
	}
	if strings.Contains(release, "Acquire-By-Hash") {
		t.Errorf("virtual Release advertises by-hash (the aggregate keeps no history)")
	}

	// The checksum entries describe the aggregate's own bytes: the merged
	// main Packages carries alpha ONLY (one stanza), contrib carries beta.
	for fam, wantStanza := range map[string]string{
		"main/binary-amd64/Packages":    "Package: alpha\n",
		"contrib/binary-i386/Packages":  "Package: beta\n",
		"main/binary-amd64/Packages.gz": "",
	} {
		entry := releaseChecksumOf(t, release, "SHA256", fam)
		status, body, _ := s.get("/binflow/deb-virt/dists/stable/" + fam)
		if status != http.StatusOK {
			t.Fatalf("virtual %s = %d", fam, status)
		}
		if got := sha256Hex([]byte(body)); got != entry {
			t.Errorf("virtual %s sha256: Release %s ≠ served %s", fam, entry, got)
		}
		if wantStanza != "" && !strings.Contains(body, wantStanza) {
			t.Errorf("virtual %s lacks %q:\n%s", fam, wantStanza, body)
		}
	}

	// The merged gz spelling serves and decompresses to the plain body.
	status, gz, _ := s.get("/binflow/deb-virt/dists/stable/main/binary-amd64/Packages.gz")
	if status != http.StatusOK {
		t.Fatalf("virtual Packages.gz = %d", status)
	}
	_, plain, _ := s.get("/binflow/deb-virt/dists/stable/main/binary-amd64/Packages")
	if string(gunzipE2E(t, []byte(gz))) != plain {
		t.Errorf("virtual Packages.gz decompresses to a different body")
	}

	// A dist no member serves is the plain not-found.
	if status, _, _ := s.get("/binflow/deb-virt/dists/sid/Release"); status != http.StatusNotFound {
		t.Errorf("unknown dist Release = %d, want 404", status)
	}
}

// TestVirtualMergeDedupeAndDownload: the same download address in two
// members keeps the FIRST member's stanza (its checksums describe the
// bytes the first-found walk serves), and both members' distinct
// packages ride the merged index with self-consistent downloads.
func TestVirtualMergeDedupeAndDownload(t *testing.T) {
	s := newStack(t)
	seedVirtualMembers(t, s, "deb-va", "deb-vb", true)
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-va", "deb-vb"}, "")

	_, packages, _ := s.get("/binflow/deb-virt/dists/stable/main/binary-amd64/Packages")
	if strings.Count(packages, "Package: alpha\n") != 1 {
		t.Fatalf("duplicate alpha stanza survived the merge:\n%s", packages)
	}
	// memberA's copy serves (the first member in the order), and the
	// stanza's checksums describe exactly those bytes.
	alpha := helloDeb("alpha", "1.0", "amd64")
	if got := stanzaField(packages, "SHA256"); got != sha256Hex(alpha) {
		t.Errorf("merged alpha SHA256 %s ≠ memberA's %s (first-seen rule)", got, sha256Hex(alpha))
	}
	status, body, _ := s.get("/binflow/deb-virt/pool/main/a/alpha/alpha_1.0_amd64.deb")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(alpha) {
		t.Fatalf("virtual alpha download = %d (checksum consistent: %v)", status,
			sha256Hex([]byte(body)) == sha256Hex(alpha))
	}

	// beta rides the merged view of its own family and downloads through
	// the member walk.
	_, contrib, _ := s.get("/binflow/deb-virt/dists/stable/contrib/binary-i386/Packages")
	if !strings.Contains(contrib, "Package: beta\n") {
		t.Fatalf("contrib merge lacks beta:\n%s", contrib)
	}
	status, _, _ = s.get("/binflow/deb-virt/pool/contrib/b/beta/beta_2.0_i386.deb")
	if status != http.StatusOK {
		t.Fatalf("virtual beta download = %d", status)
	}
}

// TestVirtualSourcesMerge: the source family merges the same way.
func TestVirtualSourcesMerge(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-sa", repo.TypeLocal, `{}`)
	dsc := fixtureDsc("srcpkg", "1.0", "Checksums-Sha256:\n aaaa 111 srcpkg_1.0.tar.gz\n")
	if status, b, _ := s.put("/binflow/deb-sa/pool/main/s/srcpkg/srcpkg_1.0.dsc;"+
		"dsc.distribution=stable;dsc.component=main", dsc, nil); status != http.StatusCreated {
		t.Fatalf("memberA dscPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-sa/dists/stable/main/source/Sources")

	s.seedRepo(t, "deb-sb", repo.TypeLocal, `{}`)
	dsc2 := fixtureDsc("otherpkg", "2.0", "Checksums-Sha256:\n bbbb 222 otherpkg_2.0.tar.gz\n")
	if status, b, _ := s.put("/binflow/deb-sb/pool/main/o/otherpkg/otherpkg_2.0.dsc;"+
		"dsc.distribution=stable;dsc.component=main", []byte(dsc2), nil); status != http.StatusCreated {
		t.Fatalf("memberB dscPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-sb/dists/stable/main/source/Sources")

	s.seedVirtualRepo(t, "deb-virt", []string{"deb-sa", "deb-sb"}, "")
	_, release, _ := s.get("/binflow/deb-virt/dists/stable/Release")
	if !strings.Contains(release, "main/source/Sources\n") && !strings.Contains(release, " main/source/Sources") {
		// The entry line carries the checksum columns; the path check below
		// is the precise one.
		if !strings.Contains(release, "main/source/Sources") {
			t.Fatalf("virtual Release does not list the Sources family:\n%s", release)
		}
	}
	_, sources, _ := s.get("/binflow/deb-virt/dists/stable/main/source/Sources")
	for _, want := range []string{"Package: srcpkg\n", "Package: otherpkg\n"} {
		if !strings.Contains(sources, want) {
			t.Errorf("merged Sources lacks %q:\n%s", want, sources)
		}
	}
}

// TestVirtualWrites: the un-routed debPUT answers the C5 405; a routed
// debPUT lands in the member, the member's OWN index recomputes, and
// the aggregate immediately reflects it; DELETE never propagates; the
// generated family refuses direct writes.
func TestVirtualWrites(t *testing.T) {
	s := newStack(t)
	seedVirtualMembers(t, s, "deb-va", "deb-vb", false)

	// Un-routed: the C5 405 (the service's own refusal through the arm).
	s.seedVirtualRepo(t, "deb-noroute", []string{"deb-va", "deb-vb"}, "")
	status, body, hdr := s.debPut(t, "/binflow/deb-noroute/pool/main/g/gamma/gamma_1.0_amd64.deb",
		helloDeb("gamma", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed virtual debPUT = (%d, %s), want 405", status, body)
	}
	if !strings.Contains(body, "No local repository was configured as local deployment repository for the (deb-noroute) virtual repository.") {
		t.Errorf("un-routed body = %q", body)
	}
	if allow := hdr.Get("Allow"); allow != http.MethodGet {
		t.Errorf("un-routed Allow = %q", allow)
	}

	// Routed: 201, the member's index recomputes, the aggregate reflects.
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-va", "deb-vb"}, "deb-va")
	status, body, _ = s.debPut(t, "/binflow/deb-virt/pool/main/g/gamma/gamma_1.0_amd64.deb",
		helloDeb("gamma", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusCreated {
		t.Fatalf("routed virtual debPUT = (%d, %s), want 201", status, body)
	}
	s.waitIndex2(t, "/binflow/deb-va/dists/stable/main/binary-amd64/Packages", "Package: gamma")
	merged := s.waitIndex2(t, "/binflow/deb-virt/dists/stable/main/binary-amd64/Packages", "Package: gamma")
	for _, want := range []string{"Package: alpha\n", "Package: gamma\n"} {
		if !strings.Contains(merged, want) {
			t.Errorf("merged Packages lacks %q after the routed write:\n%s", want, merged)
		}
	}

	// DELETE never propagates — the routed wording.
	status, body, _ = s.delete("/binflow/deb-virt/pool/main/g/gamma/gamma_1.0_amd64.deb")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "Deletes are not propagated") {
		t.Fatalf("virtual DELETE = (%d, %s)", status, body)
	}
	status, body, _ = s.delete("/binflow/deb-noroute/pool/main/a/alpha/alpha_1.0_amd64.deb")
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "No local repository was configured") {
		t.Fatalf("un-routed virtual DELETE = (%d, %s)", status, body)
	}

	// The generated family refuses direct writes (DB-3 on the aggregate).
	status, body, _ = s.put("/binflow/deb-virt/dists/stable/Release", []byte("forged"), nil)
	if status != http.StatusForbidden || !strings.Contains(body, "server-generated") {
		t.Fatalf("virtual index PUT = (%d, %s), want the 403", status, body)
	}
	status, _, _ = s.delete("/binflow/deb-virt/dists/stable/Release")
	if status != http.StatusForbidden {
		t.Fatalf("virtual index DELETE = %d, want 403", status)
	}

	// A plain companion file routes like any write.
	status, body, _ = s.put("/binflow/deb-virt/pool/main/g/gamma/gamma.orig.tar.gz", []byte("tarball"), nil)
	if status != http.StatusCreated {
		t.Fatalf("routed plain PUT = (%d, %s)", status, body)
	}
	if status, body, _ = s.get("/binflow/deb-va/pool/main/g/gamma/gamma.orig.tar.gz"); status != http.StatusOK {
		t.Fatalf("plain PUT landed in the member = (%d, %s)", status, body)
	}
}

// TestVirtualUnservedFaces: the signature family and by-hash addresses
// answer the plain 404 (apt degrades to the canonical unsigned Release
// and to canonical names).
func TestVirtualUnservedFaces(t *testing.T) {
	s := newStack(t)
	seedVirtualMembers(t, s, "deb-va", "deb-vb", false)
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-va", "deb-vb"}, "")
	for _, path := range []string{
		"/binflow/deb-virt/dists/stable/InRelease",
		"/binflow/deb-virt/dists/stable/Release.gpg",
		"/binflow/deb-virt/dists/stable/main/binary-amd64/by-hash/SHA256/deadbeef",
	} {
		if status, _, _ := s.get(path); status != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, status)
		}
	}
	// The Release itself serves (the unsigned posture's anchor).
	if status, _, _ := s.get("/binflow/deb-virt/dists/stable/Release"); status != http.StatusOK {
		t.Errorf("virtual Release = %d, want 200", status)
	}
}

// TestVirtualRemoteMember: a remote member aggregates through the FR-20
// chain (the upstream's Release inventory feeding the union), and a
// dead remote member is tolerated beside a live local one — but a
// virtual where ONLY the dead member exists surfaces the failure
// instead of masking it as a plain 404.
func TestVirtualRemoteMember(t *testing.T) {
	s := newStack(t)
	origin := seedRemoteOrigin(t, s, "deb-origin")
	s.seedRepo(t, "deb-upstream", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-upstream", s.srv.URL+"/binflow/deb-origin")

	s.seedRepo(t, "deb-localm", repo.TypeLocal, `{}`)
	local := helloDeb("localpkg", "3.0", "all")
	if status, b, _ := s.debPut(t, "/binflow/deb-localm/pool/main/l/localpkg/localpkg_3.0_all.deb",
		local, "stable", []string{"main"}, []string{"all"}); status != http.StatusCreated {
		t.Fatalf("local member debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-localm/dists/stable/Release")

	s.seedVirtualRepo(t, "deb-virt", []string{"deb-upstream", "deb-localm"}, "")
	status, release, _ := s.get("/binflow/deb-virt/dists/stable/Release")
	if status != http.StatusOK {
		t.Fatalf("virtual Release with a remote member = %d", status)
	}
	// The remote member's upstream family (main/binary-amd64) and the
	// local member's (main/binary-all) both ride the union.
	for _, fam := range []string{"main/binary-amd64/Packages", "main/binary-all/Packages"} {
		if !strings.Contains(release, fam) {
			t.Errorf("virtual Release lacks the %s family:\n%s", fam, release)
		}
	}
	_, merged, _ := s.get("/binflow/deb-virt/dists/stable/main/binary-amd64/Packages")
	if !strings.Contains(merged, "Package: mirrorpkg\n") {
		t.Errorf("merged Packages lacks the remote member's package:\n%s", merged)
	}
	// The remote member's artifact downloads through the virtual walk.
	status, body, _ := s.get("/binflow/deb-virt/pool/main/m/mirrorpkg/mirrorpkg_1.0-1_amd64.deb")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(origin) {
		t.Fatalf("virtual download of the remote member's artifact = %d", status)
	}

	// A dead remote member beside a live local one: tolerated, the local
	// member's view still serves.
	s.seedRepo(t, "deb-dead", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-dead", "http://127.0.0.1:1/dead")
	s.seedVirtualRepo(t, "deb-virt2", []string{"deb-dead", "deb-localm"}, "")
	if status, _, _ := s.get("/binflow/deb-virt2/dists/stable/Release"); status != http.StatusOK {
		t.Fatalf("tolerated dead member = %d, want the live member's Release", status)
	}

	// ONLY a failed member: the remembered CLASSIFIED failure surfaces
	// (the SSRF refusal — a 400 no offline window ever softens — is the
	// deterministic shape; a plain unreachable upstream without hardFail
	// answers the engine's unfound family instead, which aggregates as
	// "member has nothing", the RE-04 stale-first posture).
	s.seedRepo(t, "deb-refused", repo.TypeRemote, `{}`)
	s.seedRemoteConfigExempt(t, "deb-refused", "http://127.0.0.1:1/refused", false)
	s.seedVirtualRepo(t, "deb-virt3", []string{"deb-refused"}, "")
	deadStatus, deadBody, _ := s.get("/binflow/deb-virt3/dists/stable/Release")
	if deadStatus != http.StatusBadRequest {
		t.Fatalf("failed-only virtual = (%d, %s), want the surfaced 400", deadStatus, deadBody)
	}
}
