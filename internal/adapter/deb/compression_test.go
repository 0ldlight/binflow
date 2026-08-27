package deb

// The optional index compression legs (the P2 leftover of T-310's D1,
// restored in T-314 for the renderable subset): xz and lzma companions
// render deterministically and sweep on config change; bz2 still has no
// writer in the dependency set and degrades away (the registered gap).

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// unxz decompresses one xz body (test-side).
func unxz(t *testing.T, body []byte) []byte {
	t.Helper()
	xr, err := xz.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("xz open: %v", err)
	}
	out := new(bytes.Buffer)
	if _, err := out.ReadFrom(xr); err != nil {
		t.Fatalf("xz read: %v", err)
	}
	return out.Bytes()
}

// unlzma decompresses one lzma-alone body (test-side).
func unlzma(t *testing.T, body []byte) []byte {
	t.Helper()
	lr, err := lzma.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("lzma open: %v", err)
	}
	out := new(bytes.Buffer)
	if _, err := out.ReadFrom(lr); err != nil {
		t.Fatalf("lzma read: %v", err)
	}
	return out.Bytes()
}

// TestOptionalCompressionXzAndLzma: the configured optional spellings
// render beside the mandatory pair, the Release checksum sections list
// them with digests that reconcile against the served bytes, and a
// config flip plus a whole-repository reindex sweeps the dropped
// spelling.
func TestOptionalCompressionXzAndLzma(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-comp", repo.TypeLocal, `{"optionalIndexCompressionFormats":["xz"]}`)
	if status, b, _ := s.debPut(t, "/binflow/deb-comp/pool/main/c/comppkg/comppkg_1.0_amd64.deb",
		helloDeb("comppkg", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-comp/dists/stable/Release")

	plainPath := "main/binary-amd64/Packages"
	_, plain, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath)
	status, xzBody, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath + ".xz")
	if status != http.StatusOK {
		t.Fatalf("Packages.xz = %d", status)
	}
	if got := unxz(t, []byte(xzBody)); string(got) != plain {
		t.Errorf("Packages.xz decompresses to a different body")
	}
	_, release, _ := s.get("/binflow/deb-comp/dists/stable/Release")
	if want := releaseChecksumOf(t, release, "SHA256", plainPath+".xz"); want != sha256Hex([]byte(xzBody)) {
		t.Errorf("Packages.xz sha256: Release %s ≠ served %s", want, sha256Hex([]byte(xzBody)))
	}

	// Determinism: a full reindex renders byte-identical companions (the
	// digest stability the checksum chain and by-hash rely on).
	if status, body, _ := s.post("/binflow/api/deb/reindex/deb-comp?async=0"); status != http.StatusOK {
		t.Fatalf("reindex = (%d, %s)", status, body)
	}
	status, xz2, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath + ".xz")
	if status != http.StatusOK || xz2 != xzBody {
		t.Errorf("Packages.xz unstable across reindexes (%d bytes vs %d)", len(xzBody), len(xz2))
	}

	// Flip the config to lzma + a whole-repository reindex: the lzma
	// companion appears, the xz spelling sweeps.
	s.updateRepoConfig(t, "deb-comp", `{"optionalIndexCompressionFormats":["lzma"]}`)
	if status, body, _ := s.post("/binflow/api/deb/reindex/deb-comp?async=0"); status != http.StatusOK {
		t.Fatalf("reindex after flip = (%d, %s)", status, body)
	}
	status, lzmaBody, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath + ".lzma")
	if status != http.StatusOK {
		t.Fatalf("Packages.lzma = %d", status)
	}
	if got := unlzma(t, []byte(lzmaBody)); string(got) != plain {
		t.Errorf("Packages.lzma decompresses to a different body")
	}
	if status, _, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath + ".xz"); status != http.StatusNotFound {
		t.Errorf("dropped xz spelling still serves = %d, want the sweep's 404", status)
	}
	// The mandatory pair never leaves.
	if status, _, _ := s.get("/binflow/deb-comp/dists/stable/" + plainPath + ".gz"); status != http.StatusOK {
		t.Errorf("Packages.gz = %d after the flip", status)
	}
}

// TestOptionalCompressionBz2Degrades: bz2 carries no writer in this
// dependency set (the registered D1 gap) — the name parses, the WARN
// fires at config load, plain + .gz keep serving, and no broken .bz2
// file ever lands.
func TestOptionalCompressionBz2Degrades(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-bz2", repo.TypeLocal, `{"optionalIndexCompressionFormats":["bz2"]}`)
	if status, b, _ := s.debPut(t, "/binflow/deb-bz2/pool/main/b/bzpkg/bzpkg_1.0_amd64.deb",
		helloDeb("bzpkg", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-bz2/dists/stable/Release")

	path := "main/binary-amd64/Packages"
	if status, _, _ := s.get("/binflow/deb-bz2/dists/stable/" + path + ".bz2"); status != http.StatusNotFound {
		t.Errorf("Packages.bz2 = %d, want 404 (no writer — never a broken file)", status)
	}
	for _, suffix := range []string{"", ".gz"} {
		if status, _, _ := s.get("/binflow/deb-bz2/dists/stable/" + path + suffix); status != http.StatusOK {
			t.Errorf("Packages%s = %d, want the mandatory pair serving", suffix, status)
		}
	}
	_, release, _ := s.get("/binflow/deb-bz2/dists/stable/Release")
	if strings.Contains(release, ".bz2") {
		t.Errorf("Release lists a bz2 entry it cannot serve:\n%s", release)
	}
}

// updateRepoConfig rewrites one seeded repository row's config JSON (the
// config-plane flip the compression legs exercise).
func (s *stack) updateRepoConfig(t *testing.T, key, config string) {
	t.Helper()
	if err := s.md.Repos().Update(t.Context(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: Protocol, Config: config,
	}); err != nil {
		t.Fatalf("update repo config %s: %v", key, err)
	}
}
