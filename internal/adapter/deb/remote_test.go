package deb

// The remote-class legs (debian.md section 8's remote row / S9): the
// pull-through mirror against a same-server local origin (the conan
// T-312 loopback posture), the read-only write refusals, the RE-06
// cache-eviction DELETE, and the unfound mapping.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// seedRemoteOrigin assembles the origin local repository with one
// package indexed for stable/main/amd64 and returns the .deb bytes.
func seedRemoteOrigin(t *testing.T, s *stack, key string) []byte {
	t.Helper()
	s.seedRepo(t, key, repo.TypeLocal, `{}`)
	pkg := helloDeb("mirrorpkg", "1.0-1", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/"+key+"/pool/main/m/mirrorpkg/mirrorpkg_1.0-1_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("origin debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/"+key+"/dists/stable/Release")
	return pkg
}

// TestRemotePullThrough: the mirror chain — Release, Packages.gz and the
// .deb all proxy the origin's bytes through the engine (the cache-state
// hint riding the reader), and the second .deb read serves the landed
// copy.
func TestRemotePullThrough(t *testing.T) {
	s := newStack(t)
	seedRemoteOrigin(t, s, "deb-origin")
	s.seedRepo(t, "deb-mirror", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-mirror", s.srv.URL+"/binflow/deb-origin")

	// The origin's own copy, for byte-equality.
	_, originRelease, _ := s.get("/binflow/deb-origin/dists/stable/Release")
	_, originGz, _ := s.get("/binflow/deb-origin/dists/stable/main/binary-amd64/Packages.gz")

	status, release, hdr := s.get("/binflow/deb-mirror/dists/stable/Release")
	if status != http.StatusOK {
		t.Fatalf("mirrored Release = %d", status)
	}
	if release != originRelease {
		t.Errorf("mirrored Release differs from the origin's bytes")
	}
	if cs := hdr.Get("X-BinFlow-Cache"); cs == "" {
		t.Errorf("mirrored Release carries no cache-state hint")
	}

	status, gz, _ := s.get("/binflow/deb-mirror/dists/stable/main/binary-amd64/Packages.gz")
	if status != http.StatusOK || gz != originGz {
		t.Fatalf("mirrored Packages.gz = %d (equal bytes: %v)", status, gz == originGz)
	}

	status, body, hdr := s.get("/binflow/deb-mirror/pool/main/m/mirrorpkg/mirrorpkg_1.0-1_amd64.deb")
	if status != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex([]byte(helloDeb("mirrorpkg", "1.0-1", "amd64"))) {
		t.Fatalf("mirrored .deb = %d (%d bytes)", status, len(body))
	}
	if hint := hdr.Get("X-BinFlow-Cache"); !strings.Contains(hint, "MISS") {
		t.Errorf("first .deb read hint = %q, want a MISS family", hint)
	}
	status, _, hdr = s.get("/binflow/deb-mirror/pool/main/m/mirrorpkg/mirrorpkg_1.0-1_amd64.deb")
	if status != http.StatusOK {
		t.Fatalf("second .deb read = %d", status)
	}
	if hint := hdr.Get("X-BinFlow-Cache"); !strings.Contains(hint, "HIT") {
		t.Errorf("second .deb read hint = %q, want a HIT family", hint)
	}
}

// TestRemoteWriteRefusals: every PUT refuses read-only BEFORE the debPUT
// grammar runs (no body drained), the index family included (the remote
// posture outranks DB-3 — the cached upstream copy is not "generated"
// here at all).
func TestRemoteWriteRefusals(t *testing.T) {
	s := newStack(t)
	seedRemoteOrigin(t, s, "deb-origin")
	s.seedRepo(t, "deb-mirror", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-mirror", s.srv.URL+"/binflow/deb-origin")

	status, body, hdr := s.debPut(t, "/binflow/deb-mirror/pool/main/m/mirrorpkg/mirrorpkg_2.0-1_amd64.deb",
		helloDeb("mirrorpkg", "2.0-1", "amd64"), "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("remote debPUT = (%d, %s), want 405", status, body)
	}
	if allow := hdr.Get("Allow"); allow != http.MethodGet {
		t.Errorf("remote debPUT Allow = %q", allow)
	}
	if !strings.Contains(body, "read-only proxy cache") {
		t.Errorf("remote debPUT body = %q", body)
	}

	status, body, _ = s.put("/binflow/deb-mirror/dists/stable/Release", []byte("forged"), nil)
	if status != http.StatusMethodNotAllowed || !strings.Contains(body, "read-only proxy cache") {
		t.Fatalf("remote index PUT = (%d, %s), want the read-only 405", status, body)
	}
}

// TestRemoteDeleteEvicts: DELETE on the remote is RE-06 cache eviction
// (204, idempotent), and the next read re-fetches upstream.
func TestRemoteDeleteEvicts(t *testing.T) {
	s := newStack(t)
	seedRemoteOrigin(t, s, "deb-origin")
	s.seedRepo(t, "deb-mirror", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-mirror", s.srv.URL+"/binflow/deb-origin")

	path := "/binflow/deb-mirror/pool/main/m/mirrorpkg/mirrorpkg_1.0-1_amd64.deb"
	if status, _, _ := s.get(path); status != http.StatusOK {
		t.Fatalf("prime the cache = %d", status)
	}
	if status, body, _ := s.delete(path); status != http.StatusNoContent {
		t.Fatalf("evict = (%d, %s), want 204", status, body)
	}
	// A never-cached path answers the shared not-found (the local plane's
	// own DELETE-of-missing shape).
	if status, _, _ := s.delete("/binflow/deb-mirror/pool/never-cached.deb"); status != http.StatusNotFound {
		t.Fatalf("evict on a never-cached path = %d, want 404", status)
	}
	if status, _, hdr := s.get(path); status != http.StatusOK {
		t.Fatalf("re-fetch after evict = %d", status)
	} else if hint := hdr.Get("X-BinFlow-Cache"); !strings.Contains(hint, "MISS") {
		t.Errorf("post-evict read hint = %q, want a re-fetch", hint)
	}
}

// TestRemoteUnfoundIs404: an upstream 404 maps onto the plane's plain
// not-found (the negative-cache posture the engine owns).
func TestRemoteUnfoundIs404(t *testing.T) {
	s := newStack(t)
	seedRemoteOrigin(t, s, "deb-origin")
	s.seedRepo(t, "deb-mirror", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-mirror", s.srv.URL+"/binflow/deb-origin")
	if status, _, _ := s.get("/binflow/deb-mirror/dists/nosuchsuite/Release"); status != http.StatusNotFound {
		t.Fatalf("unknown suite = %d, want 404", status)
	}
	if status, _, _ := s.get("/binflow/deb-mirror"); status != http.StatusOK {
		t.Errorf("root probe = %d, want 200", status)
	}
}
