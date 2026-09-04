package helm

// T-474 regression pins: the classic chart PUT spooled request bodies to
// the OS temp dir (os.CreateTemp("", ...)), which a hardened deployment —
// read-only rootfs, the kubernetes norm — refuses with "read-only file
// system", and the raw error leaked the internal path in a bare 500. The
// spool now stages under the storage-volume staging dir (Options.SpoolDir,
// the cmd assembly's <data_dir>/staging — the same volume the blob store
// writes on). This file pins the staging location + cleanup, independence
// from a read-only OS temp dir, and the sanitized refusal when the staging
// root itself is unwritable.

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
	staging := filepath.Join(t.TempDir(), "staging", "helm") // deliberately not pre-created
	const body = "chart-bytes"
	path, err := spoolBody(staging, strings.NewReader(body))
	if err != nil {
		t.Fatalf("spoolBody: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	if got := filepath.Dir(path); got != staging {
		t.Errorf("spool file landed in %s, want the configured staging dir %s", got, staging)
	}
	if base := filepath.Base(path); !strings.HasPrefix(base, "binflow-helm-") || !strings.HasSuffix(base, ".tgz") {
		t.Errorf("spool file name %q lacks the binflow-helm-*.tgz shape", base)
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
// at the unit seam: dir "" (the pre-T-474 arm, now the bare-test fallback
// only) resolves against os.TempDir, and a read-only TMPDIR — exactly the
// UAT container's shape — refuses it, wrapped in the staging-unavailable
// family (the 507 mapping). The handler-level green counterpart is
// TestChartPutSurvivesReadOnlyOsTemp: the same poisoned TMPDIR with the
// production wiring sails through.
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

// TestChartPutSurvivesReadOnlyOsTemp is the incident's red→green pin: with
// the production wiring (the stack default, Options.SpoolDir =
// <dataDir>/staging) the classic chart PUT must succeed on a host whose
// OS temp dir is read-only — the exact UAT shape that answered 500 before
// T-474.
func TestChartPutSurvivesReadOnlyOsTemp(t *testing.T) {
	// Stack first: its own t.TempDir roots (and any helper scratch) must
	// resolve against the REAL OS temp before it is poisoned.
	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "helm-local", repo.TypeLocal, "")
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "1.0.99"), nil)
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	status, body, _ := s.put("/binflow/helm-local/mychart-1.0.99.tgz", chart, nil)
	if status != http.StatusCreated {
		t.Fatalf("chart PUT under a read-only OS temp = %d %q, want 201", status, body)
	}
	gstatus, gbody, _ := s.get("/binflow/helm-local/mychart-1.0.99.tgz")
	if gstatus != http.StatusOK || string(gbody) != string(chart) {
		t.Fatalf("chart GET after PUT = %d (len %d, want %d)", gstatus, len(gbody), len(chart))
	}
}

// TestChartPutSpoolFailureRefusalIsDiagnosableAndBounded pins the
// reconciled error face (T-474): an unwritable staging root answers 507
// Insufficient Storage with the family's plain-text body. The disclosure
// is BOUNDED, not absent — the body names the attempted staging root (the
// operator's own config, the actionable fact) while the os-error
// internals and the temp file name (the pre-fix leak's actual noise:
// "/tmp/binflow-helm-984094729.tgz: read-only file system") stay in the
// server log.
func TestChartPutSpoolFailureRefusalIsDiagnosableAndBounded(t *testing.T) {
	ro := mustReadOnlyDir(t)
	s := newStackOpt(t, stackOptions{spoolDir: ro})
	s.seedRepo(t, "helm-local", repo.TypeLocal, "")
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "2.0.0"), nil)

	status, body, hdr := s.put("/binflow/helm-local/mychart-2.0.0.tgz", chart, nil)
	if status != http.StatusInsufficientStorage {
		t.Fatalf("chart PUT with an unwritable staging root = %d %q, want 507", status, body)
	}
	if !strings.Contains(body, ro) {
		t.Errorf("refusal is not diagnosable: %q does not name the attempted root %s", body, ro)
	}
	if strings.Contains(body, "binflow-helm-") || strings.Contains(body, "read-only file system") {
		t.Errorf("refusal leaks internals beyond the root: %q", body)
	}
	if ct := hdr.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Errorf("refusal Content-Type = %q, want the writeText family's text/plain", ct)
	}
}

// TestChartPutLeavesNoSpoolResidue pins the cleanup contract: both the
// success landing and the checksum-mismatch refusal (a failure that
// happens AFTER the spool) remove the staged file — the staging volume
// never accumulates upload residue.
func TestChartPutLeavesNoSpoolResidue(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	s := newStackOpt(t, stackOptions{spoolDir: staging})
	s.seedRepo(t, "helm-local", repo.TypeLocal, "")
	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "3.0.0"), nil)

	if status, body, _ := s.put("/binflow/helm-local/mychart-3.0.0.tgz", chart, nil); status != http.StatusCreated {
		t.Fatalf("chart PUT = %d %q, want 201", status, body)
	}
	assertNoSpoolResidue(t, staging)

	// The post-spool failure leg: a wrong declared sha256 meets the 409
	// after the body is staged and parsed — the defer must still clean up.
	wrong := strings.Repeat("0", 64)
	status, body, _ := s.put("/binflow/helm-local/mychart-3.0.1.tgz", chart,
		map[string]string{"X-Checksum-Sha256": wrong})
	if status != http.StatusConflict {
		t.Fatalf("checksum-mismatch PUT = %d %q, want 409", status, body)
	}
	assertNoSpoolResidue(t, staging)
}

// assertNoSpoolResidue fails when any binflow-helm-* file survives under
// the staging root.
func assertNoSpoolResidue(t *testing.T, staging string) {
	t.Helper()
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatalf("read staging dir %s: %v", staging, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "binflow-helm-") {
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
