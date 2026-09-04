package cargo

// T-476 regression pins (the T-474 family): the publish chain spooled
// crate bodies to the OS temp dir (os.CreateTemp("", ...)), which a
// hardened deployment — read-only rootfs, the kubernetes norm — refuses
// with "read-only file system" (the same family as the UAT helm/nuget
// push incidents), and cargo reads the resulting 200 + warnings arm as
// SUCCESS (R-1) — a silent total loss. The spool now stages under the
// storage-volume staging dir (Options.SpoolDir, the cmd assembly's
// <data_dir>/staging), and the staging refusal is the one carve-out from
// CG-2's IOException track: 507 on the errors envelope. This file pins
// the staging location + cleanup, independence from a read-only OS temp
// dir, and the bounded refusal when the staging root itself is
// unwritable.

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

// TestDecodePublishFrameStagesUnderConfiguredDir pins the unit seam: a
// configured dir hosts the staged crate (name shape, byte-for-byte
// roundtrip), and the staging root is created on demand.
func TestDecodePublishFrameStagesUnderConfiguredDir(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging", "cargo") // deliberately not pre-created
	meta := `{"name":"stagecrate","vers":"0.1.0"}`
	crate := []byte("pretend this is a gzipped tarball")

	rawJSON, spool, err := decodePublishFrame(staging, strings.NewReader(string(publishBody(meta, crate))))
	if err != nil {
		t.Fatalf("decodePublishFrame: %v", err)
	}
	defer func() { _ = os.Remove(spool) }()

	if string(rawJSON) != meta {
		t.Errorf("metadata = %q, want %q", rawJSON, meta)
	}
	if got := filepath.Dir(spool); got != staging {
		t.Errorf("staged crate landed in %s, want the configured staging dir %s", got, staging)
	}
	if base := filepath.Base(spool); !strings.HasPrefix(base, "binflow-cargo-") || !strings.HasSuffix(base, ".crate") {
		t.Errorf("staged crate name %q lacks the binflow-cargo-*.crate shape", base)
	}
	got, err := os.ReadFile(spool)
	if err != nil {
		t.Fatalf("read staged crate: %v", err)
	}
	if string(got) != string(crate) {
		t.Errorf("staged roundtrip = %q, want %q", got, crate)
	}
}

// TestDecodePublishFrameOsTempFallbackFailsWhenTmpReadOnly reproduces the
// incident family at the unit seam: dir "" (the pre-T-476 arm, now the
// bare-test fallback only) resolves against os.TempDir, and a read-only
// TMPDIR — exactly the hardened container's shape — refuses it, wrapped
// in the staging-unavailable family (the 507 carve-out). The
// handler-level green counterpart is TestPublishSurvivesReadOnlyOsTemp:
// the same poisoned TMPDIR with the production wiring sails through.
func TestDecodePublishFrameOsTempFallbackFailsWhenTmpReadOnly(t *testing.T) {
	ro := mustReadOnlyDir(t)
	t.Setenv("TMPDIR", ro)
	meta := `{"name":"rocrate","vers":"0.1.0"}`
	_, _, err := decodePublishFrame("", strings.NewReader(string(publishBody(meta, []byte("crate")))))
	if err == nil {
		t.Fatal("decodePublishFrame with the OS-temp fallback succeeded under a read-only TMPDIR; want the incident's refusal")
	}
	if !errors.Is(err, adapter.ErrStagingUnavailable) {
		t.Errorf("refusal wraps adapter.ErrStagingUnavailable: %v", err)
	}
}

// TestPublishSurvivesReadOnlyOsTemp is the incident family's red→green
// pin: with the production wiring (the stack default, Options.SpoolDir =
// <dataDir>/staging) the publish must succeed on a host whose OS temp
// dir is read-only — and the success face is CG-2's 200 + EMPTY
// warnings.other (a warning string there would read as a lost publish).
func TestPublishSurvivesReadOnlyOsTemp(t *testing.T) {
	// Stack first: its own t.TempDir roots (and any helper scratch) must
	// resolve against the REAL OS temp before it is poisoned.
	s := newStack(t) // default spoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	meta := `{"name":"robustcrate","vers":"0.1.0"}`
	crate := []byte("robust crate bytes")
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	status, body, _ := s.publish(t, "cargo-local", meta, crate)
	if status != http.StatusOK {
		t.Fatalf("publish under a read-only OS temp = %d %q, want 200", status, body)
	}
	if strings.Contains(body, "Failed to publish with error") {
		t.Errorf("publish body carries a failure string: %q", body)
	}
	gstatus, gbody, _ := s.get(repoPath("cargo-local") + "/v1/crates/robustcrate/0.1.0/download")
	if gstatus != http.StatusOK || string(gbody) != string(crate) {
		t.Fatalf("crate GET after publish = %d (len %d, want %d stored bytes)", gstatus, len(gbody), len(crate))
	}
}

// TestPublishSpoolFailureRefusalIsDiagnosableAndBounded pins the T-476
// carve-out: an unwritable staging root answers 507 Insufficient Storage
// on the face's own errors envelope — NOT CG-2's 200 + warnings arm,
// which cargo reads as success. The disclosure is BOUNDED, not absent —
// the detail names the attempted staging root (the operator's own
// config, the actionable fact) while the os-error internals and the temp
// file name stay in the server log.
func TestPublishSpoolFailureRefusalIsDiagnosableAndBounded(t *testing.T) {
	ro := mustReadOnlyDir(t)
	s := newStackOpt(t, stackOptions{spoolDir: ro})
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	meta := `{"name":"boundcrate","vers":"0.1.0"}`

	status, body, hdr := s.publish(t, "cargo-local", meta, []byte("crate"))
	if status != http.StatusInsufficientStorage {
		t.Fatalf("publish with an unwritable staging root = %d %q, want 507", status, body)
	}
	if !strings.Contains(body, ro) {
		t.Errorf("refusal is not diagnosable: %q does not name the attempted root %s", body, ro)
	}
	if strings.Contains(body, "binflow-cargo-") || strings.Contains(body, "read-only file system") {
		t.Errorf("refusal leaks internals beyond the root: %q", body)
	}
	if !strings.Contains(body, `"errors":[`) {
		t.Errorf("refusal body is not the errors envelope: %q", body)
	}
	if ct := hdr.Get("Content-Type"); ct != "application/json" {
		t.Errorf("refusal Content-Type = %q, want the writeJSON family's application/json", ct)
	}
}

// TestPublishLeavesNoSpoolResidue pins the cleanup contract: both the
// success landing and the metadata-parse refusal (a failure that happens
// AFTER the crate staged — the 200 + warnings arm) remove the staged
// file — the staging volume never accumulates upload residue.
func TestPublishLeavesNoSpoolResidue(t *testing.T) {
	staging := filepath.Join(t.TempDir(), "staging")
	s := newStackOpt(t, stackOptions{spoolDir: staging})
	s.seedRepo(t, "cargo-local", repo.TypeLocal)

	if status, body, _ := s.publish(t, "cargo-local", `{"name":"rescrate","vers":"0.1.0"}`, []byte("crate")); status != http.StatusOK {
		t.Fatalf("publish = %d %q, want 200", status, body)
	}
	assertNoSpoolResidue(t, staging)

	// The post-spool failure leg: a metadata frame that is not JSON
	// parses late (after the crate staged) and rides the 200 + warnings
	// arm — the deferred remove must still clean up.
	if status, body, _ := s.publish(t, "cargo-local", `nonsense`, []byte("crate")); status != http.StatusOK ||
		!strings.Contains(body, "Failed to publish with error") {
		t.Fatalf("bad-meta publish = (%d, %q), want 200 + the warnings arm", status, body)
	}
	assertNoSpoolResidue(t, staging)
}

// assertNoSpoolResidue fails when any binflow-cargo-* file survives
// under the staging root.
func assertNoSpoolResidue(t *testing.T, staging string) {
	t.Helper()
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatalf("read staging dir %s: %v", staging, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "binflow-cargo-") {
			t.Errorf("spool residue %s survived the publish chain under %s", e.Name(), staging)
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
