package rpm

// T-476 regression pins (the T-474 family): the rpm PUT chain spooled
// request bodies to the OS temp dir (os.CreateTemp("", ...)), which a
// hardened deployment — read-only rootfs, the kubernetes norm — refuses
// with "read-only file system", and the raw error leaked the internal
// path on a bare 500. The spool now stages under the storage-volume
// staging dir (Options.SpoolDir, the cmd assembly's <data_dir>/staging —
// the same volume the blob store writes on). This file pins the staging
// location + cleanup, independence from a read-only OS temp dir, and the
// sanitized 507 refusal when the staging root itself is unwritable.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestSpoolBodyStagesUnderConfiguredDir pins the unit seam: a configured
// dir hosts the spool file (name shape, byte-for-byte roundtrip), and the
// staging root is created on demand.
func TestSpoolBodyStagesUnderConfiguredDir(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging", "rpm") // deliberately not pre-created
	const body = "package-bytes"
	path, err := spoolBody(staging, strings.NewReader(body))
	if err != nil {
		t.Fatalf("spoolBody: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	if got := filepath.Dir(path); got != staging {
		t.Errorf("spool file landed in %s, want the configured staging dir %s", got, staging)
	}
	if base := filepath.Base(path); !strings.HasPrefix(base, "binflow-rpm-") || !strings.HasSuffix(base, ".rpm") {
		t.Errorf("spool file name %q lacks the binflow-rpm-*.rpm shape", base)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read spool file: %v", err)
	}
	if string(got) != body {
		t.Errorf("spool roundtrip = %q, want %q", got, body)
	}
}

// TestSpoolBodyOsTempFallbackFailsWhenTmpReadOnly reproduces the incident
// at the unit seam: dir "" (the pre-T-476 arm, now the bare-test fallback
// only) resolves against os.TempDir, and a read-only TMPDIR — exactly the
// hardened container's shape — refuses it, wrapped in the
// staging-unavailable family (the 507 mapping). The handler-level green
// counterpart is TestRpmPutSurvivesReadOnlyOsTemp: the same poisoned
// TMPDIR with the production wiring sails through.
func TestSpoolBodyOsTempFallbackFailsWhenTmpReadOnly(t *testing.T) {
	ro := mustReadOnlyDir(t)
	t.Setenv("TMPDIR", ro)
	_, err := spoolBody("", strings.NewReader("x"))
	if err == nil {
		t.Fatal("spoolBody with the OS-temp fallback succeeded under a read-only TMPDIR; want the incident's refusal")
	}
	if !errors.Is(err, adapter.ErrStagingUnavailable) {
		t.Errorf("refusal wraps adapter.ErrStagingUnavailable: %v", err)
	}
}

// TestRpmPutSurvivesReadOnlyOsTemp is the incident's red→green pin: with
// the production wiring (the stack default, Options.SpoolDir =
// <dataDir>/staging) the rpm PUT must succeed on a host whose OS temp dir
// is read-only — the exact topology the T-474 family answered 500 on.
func TestRpmPutSurvivesReadOnlyOsTemp(t *testing.T) {
	// Stack first: its own t.TempDir roots (and any helper scratch) must
	// resolve against the REAL OS temp before it is poisoned.
	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("hellorpm", "1.0.0", "1", "noarch")
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	status, body, _ := s.put("/binflow/rpm-local/hellorpm-1.0.0-1.noarch.rpm", pkg, nil)
	if status != http.StatusCreated {
		t.Fatalf("rpm PUT under a read-only OS temp = %d %q, want 201", status, body)
	}
	gstatus, gbody, _ := s.get("/binflow/rpm-local/hellorpm-1.0.0-1.noarch.rpm")
	if gstatus != http.StatusOK || string(gbody) != string(pkg) {
		t.Fatalf("package GET after PUT = %d (len %d, want %d stored bytes)", gstatus, len(gbody), len(pkg))
	}
}

// TestRpmPutSpoolFailureRefusalIsDiagnosableAndBounded pins the error
// face (T-476, the T-474 ruling): an unwritable staging root answers 507
// Insufficient Storage with the plain-text family body. The disclosure is
// BOUNDED, not absent — the body names the attempted staging root (the
// operator's own config, the actionable fact) while the os-error
// internals and the temp file name (the pre-fix leak's actual noise)
// stay in the server log.
func TestRpmPutSpoolFailureRefusalIsDiagnosableAndBounded(t *testing.T) {
	ro := mustReadOnlyDir(t)
	s := newStackOpt(t, stackOptions{spoolDir: ro})
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("boundrpm", "1.0.0", "1", "noarch")

	status, body, hdr := s.put("/binflow/rpm-local/boundrpm-1.0.0-1.noarch.rpm", pkg, nil)
	if status != http.StatusInsufficientStorage {
		t.Fatalf("rpm PUT with an unwritable staging root = %d %q, want 507", status, body)
	}
	if !strings.Contains(body, ro) {
		t.Errorf("refusal is not diagnosable: %q does not name the attempted root %s", body, ro)
	}
	if strings.Contains(body, "binflow-rpm-") || strings.Contains(body, "read-only file system") {
		t.Errorf("refusal leaks internals beyond the root: %q", body)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("refusal Content-Type = %q, want the writeText family's text/plain", ct)
	}
}

// TestRpmPutLeavesNoSpoolResidue pins the cleanup contract: both the
// success landing and the checksum-mismatch refusal (a failure that
// happens AFTER the spool) remove the staged file — the staging volume
// never accumulates upload residue.
func TestRpmPutLeavesNoSpoolResidue(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	s := newStackOpt(t, stackOptions{spoolDir: staging})
	s.seedRepo(t, "rpm-local", repo.TypeLocal, "{}")
	pkg := pkgFixture("resrpm", "1.0.0", "1", "noarch")

	if status, body, _ := s.put("/binflow/rpm-local/resrpm-1.0.0-1.noarch.rpm", pkg, nil); status != http.StatusCreated {
		t.Fatalf("rpm PUT = %d %q, want 201", status, body)
	}
	assertNoSpoolResidue(t, staging)

	// The post-spool failure leg: a wrong declared sha256 meets the 409
	// after the body is staged and parsed — the defer must still clean up.
	wrong := strings.Repeat("0", 64)
	status, body, _ := s.put("/binflow/rpm-local/resrpm-1.0.1-1.noarch.rpm", pkg,
		map[string]string{"X-Checksum-Sha256": wrong})
	if status != http.StatusConflict {
		t.Fatalf("checksum-mismatch rpm PUT = %d %q, want 409", status, body)
	}
	assertNoSpoolResidue(t, staging)
}

// assertNoSpoolResidue fails when any binflow-rpm-* file survives under
// the staging root.
func assertNoSpoolResidue(t *testing.T, staging string) {
	t.Helper()
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatalf("read staging dir %s: %v", staging, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "binflow-rpm-") {
			t.Errorf("spool residue %s survived the upload chain under %s", e.Name(), staging)
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
