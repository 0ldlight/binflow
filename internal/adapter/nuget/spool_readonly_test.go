package nuget

// T-476 regression pins (the T-474 family): the push chain spooled
// package bodies to the OS temp dir (os.CreateTemp("", ...)), which a
// hardened deployment — read-only rootfs, the kubernetes norm — refuses
// with "read-only file system" (the UAT incident: "spool upload: open
// /tmp/binflow-nuget-865178418.nupkg: read-only file system" on a bare
// 500). The spool now stages under the storage-volume staging dir
// (Options.SpoolDir, the cmd assembly's <data_dir>/staging — the same
// volume the blob store writes on). This file pins the staging location
// + cleanup, independence from a read-only OS temp dir, and the
// sanitized 507 refusal when the staging root itself is unwritable.

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestSpoolNupkgStagesUnderConfiguredDir pins the unit seam: a configured
// dir hosts the staged file (name shape, byte-for-byte roundtrip, the
// measured digest), and the staging root is created on demand.
func TestSpoolNupkgStagesUnderConfiguredDir(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging", "nuget") // deliberately not pre-created
	pkg := buildNupkg(t, "Stage.Pkg", "1.0.0", flatDeps("none"))
	sp, err := spoolNupkg(staging, strings.NewReader(string(pkg.body)))
	if err != nil {
		t.Fatalf("spoolNupkg: %v", err)
	}
	defer sp.close()

	name := sp.file.Name()
	if got := filepath.Dir(name); got != staging {
		t.Errorf("staged file landed in %s, want the configured staging dir %s", got, staging)
	}
	if base := filepath.Base(name); !strings.HasPrefix(base, "binflow-nuget-") || !strings.HasSuffix(base, suffixNupkg) {
		t.Errorf("staged file name %q lacks the binflow-nuget-*%s shape", base, suffixNupkg)
	}
	got, err := io.ReadAll(sp.file) // spoolNupkg leaves the file rewound to 0
	if err != nil {
		t.Fatalf("read staged file: %v", err)
	}
	if string(got) != string(pkg.body) {
		t.Errorf("staged roundtrip = %d bytes, want the %d body bytes", len(got), len(pkg.body))
	}
	if sp.size != int64(len(pkg.body)) || sp.sha512Base64() != pkg.sha512 {
		t.Errorf("staged measurement = (%d bytes, sha512 %s), want (%d, %s)", sp.size, sp.sha512Base64(), len(pkg.body), pkg.sha512)
	}
}

// TestSpoolNupkgOsTempFallbackFailsWhenTmpReadOnly reproduces the UAT
// incident at the unit seam: dir "" (the pre-T-476 arm, now the
// bare-test fallback only) resolves against os.TempDir, and a read-only
// TMPDIR — exactly the hardened container's shape — refuses it, wrapped
// in the staging-unavailable family (the 507 mapping). The handler-level
// green counterpart is TestPushSurvivesReadOnlyOsTemp: the same poisoned
// TMPDIR with the production wiring sails through.
func TestSpoolNupkgOsTempFallbackFailsWhenTmpReadOnly(t *testing.T) {
	ro := mustReadOnlyDir(t)
	t.Setenv("TMPDIR", ro)
	_, err := spoolNupkg("", strings.NewReader("x"))
	if err == nil {
		t.Fatal("spoolNupkg with the OS-temp fallback succeeded under a read-only TMPDIR; want the incident's refusal")
	}
	if !errors.Is(err, adapter.ErrStagingUnavailable) {
		t.Errorf("refusal wraps adapter.ErrStagingUnavailable: %v", err)
	}
}

// TestPushSurvivesReadOnlyOsTemp is the incident's red→green pin: with
// the production wiring (the stack default, Options.SpoolDir =
// <dataDir>/staging) the DIRECT flatcontainer push must succeed on a host
// whose OS temp dir is read-only — the exact UAT shape that answered the
// bare 500.
func TestPushSurvivesReadOnlyOsTemp(t *testing.T) {
	// Stack first: its own t.TempDir roots (and any helper scratch) must
	// resolve against the REAL OS temp before it is poisoned.
	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	pkg := buildNupkg(t, "Robust.Pkg", "1.0.0", flatDeps("none"))
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	// The UAT incident's exact command shape: PUT the publish base itself.
	status, body, _ := s.put(apiPath("ng-local")+"/"+segFlat, pkg.body, nil)
	if status != http.StatusCreated {
		t.Fatalf("direct push under a read-only OS temp = %d %q, want 201", status, body)
	}
	gstatus, gbody, _ := s.get(packagePath("ng-local", "robust.pkg", "1.0.0", "nupkg"))
	if gstatus != http.StatusOK || string(gbody) != string(pkg.body) {
		t.Fatalf("package GET after push = %d (len %d, want %d stored bytes)", gstatus, len(gbody), len(pkg.body))
	}
}

// TestPushSpoolFailureRefusalIsDiagnosableAndBounded pins the error face
// (T-476, the T-474 ruling): an unwritable staging root answers 507
// Insufficient Storage with the writePlain family body. The disclosure
// is BOUNDED, not absent — the body names the attempted staging root (the
// operator's own config, the actionable fact) while the os-error
// internals and the temp file name (the pre-fix leak's actual noise:
// "/tmp/binflow-nuget-865178418.nupkg: read-only file system") stay in
// the server log.
func TestPushSpoolFailureRefusalIsDiagnosableAndBounded(t *testing.T) {
	ro := mustReadOnlyDir(t)
	s := newStackOpt(t, stackOptions{spoolDir: ro})
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	pkg := buildNupkg(t, "Bound.Pkg", "1.0.0", flatDeps("none"))

	status, body, hdr := s.put(apiPath("ng-local")+"/"+segFlat, pkg.body, nil)
	if status != http.StatusInsufficientStorage {
		t.Fatalf("direct push with an unwritable staging root = %d %q, want 507", status, body)
	}
	if !strings.Contains(body, ro) {
		t.Errorf("refusal is not diagnosable: %q does not name the attempted root %s", body, ro)
	}
	if strings.Contains(body, "binflow-nuget-") || strings.Contains(body, "read-only file system") {
		t.Errorf("refusal leaks internals beyond the root: %q", body)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("refusal Content-Type = %q, want the writePlain family's text/plain", ct)
	}
}

// TestPushLeavesNoSpoolResidue pins the cleanup contract: both the
// success landing and the identity-mismatch refusal (a failure that
// happens AFTER the spool) remove the staged file — the staging volume
// never accumulates upload residue.
func TestPushLeavesNoSpoolResidue(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	s := newStackOpt(t, stackOptions{spoolDir: staging})
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	pkg := buildNupkg(t, "Res.Pkg", "1.0.0", flatDeps("none"))
	other := buildNupkg(t, "Other.Pkg", "1.0.0", flatDeps("none"))

	if status, body, _ := s.put(pushPath("ng-local", "res.pkg", "1.0.0"), pkg.body, nil); status != http.StatusCreated {
		t.Fatalf("addressed push = %d %q, want 201", status, body)
	}
	assertNoSpoolResidue(t, staging)

	// The post-spool failure leg: the addressed id disagrees with the
	// embedded nuspec — the 400 lands after the body staged, and the
	// close() must still clean up.
	if status, body, _ := s.put(pushPath("ng-local", "res.pkg", "2.0.0"), other.body, nil); status != http.StatusBadRequest {
		t.Fatalf("identity-mismatch push = %d %q, want 400", status, body)
	}
	assertNoSpoolResidue(t, staging)
}

// assertNoSpoolResidue fails when any binflow-nuget-* file survives
// under the staging root.
func assertNoSpoolResidue(t *testing.T, staging string) {
	t.Helper()
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatalf("read staging dir %s: %v", staging, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "binflow-nuget-") {
			t.Errorf("spool residue %s survived the push chain under %s", e.Name(), staging)
		}
	}
}

// mustReadOnlyDir returns a freshly chmod-0444 temp dir (the read-only
// rootfs stand-in) and schedules the perm restore. Skipped where write
// bits are not enforced: windows (chmod is a no-op on dirs) and root.
func mustReadOnlyDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory write bits are not enforced for this runner; the read-only stand-in cannot bite")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o444); err != nil {
		t.Fatalf("chmod 0444 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // before t.TempDir's own cleanup (LIFO)
	return dir
}
