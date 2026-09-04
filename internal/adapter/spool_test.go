package adapter

// T-474's shared staging primitive: upload bodies must spool onto the
// storage volume (the cmd assembly's <data_dir>/staging), never assume a
// writable OS temp dir; the "" fallback stays diagnosable — every refusal
// names the root it attempted and carries ErrStagingUnavailable for the
// adapters' 507 mapping.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestStagingDirHonorsConfiguredRoot pins the production posture: the
// configured root is returned verbatim and created on demand (a volume
// mounted after boot self-heals on the next upload).
func TestStagingDirHonorsConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "staging") // deliberately not pre-created
	got, err := StagingDir(root)
	if err != nil {
		t.Fatalf("StagingDir: %v", err)
	}
	if got != root {
		t.Errorf("StagingDir = %q, want the configured root %q", got, root)
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		t.Errorf("staging root %s not created on demand: %v", root, err)
	}
}

// TestStagingDirFallbackResolvesOsTemp pins the "" fallback: it resolves
// against os.TempDir (the bare test-harness posture) — the fallback
// itself is fine, it just must never be silent when it bites.
func TestStagingDirFallbackResolvesOsTemp(t *testing.T) {
	got, err := StagingDir("")
	if err != nil {
		t.Fatalf("StagingDir(\"\"): %v", err)
	}
	if got != os.TempDir() {
		t.Errorf("StagingDir(\"\") = %q, want the OS temp dir %q", got, os.TempDir())
	}
}

// TestStagingRefusalNamesTheAttemptedRoot is the incident's core pin
// (T-474): a staging root that cannot be written answers
// ErrStagingUnavailable whose text names the actual root attempted — a
// diagnosable refusal, not the pre-fix bare 500 that leaked only an
// opaque os error. Two honest arms: a root that cannot even be CREATED
// fails at StagingDir itself; a root that EXISTS but is unwritable passes
// mkdir (MkdirAll cannot probe write bits on a dir it did not create) and
// the refusal is the file creation's, one call later in StageFile.
func TestStagingRefusalNamesTheAttemptedRoot(t *testing.T) {
	ro := readOnlyDir(t)

	// Existing-but-unwritable root: MkdirAll is a successful no-op on it,
	// so StagingDir resolves — StageFile's CreateTemp owns the refusal.
	got, err := StagingDir(ro)
	if err != nil {
		t.Fatalf("StagingDir on an existing read-only root: %v (mkdir cannot detect write bits; StageFile owns the refusal)", err)
	}
	if got != ro {
		t.Errorf("StagingDir = %q, want the configured root %q", got, ro)
	}

	// Root that cannot be created at all (read-only parent): the refusal
	// is StagingDir's own — the family sentinel, naming the attempted root.
	absent := filepath.Join(ro, "staging")
	if _, err := StagingDir(absent); !errors.Is(err, ErrStagingUnavailable) {
		t.Fatalf("StagingDir under a read-only parent: %v, want ErrStagingUnavailable", err)
	} else if !strings.Contains(err.Error(), absent) {
		t.Errorf("refusal %q does not name the attempted root %q", err.Error(), absent)
	}

	// The write arm: file creation under the unwritable root refuses with
	// the family sentinel, naming the root.
	if _, err := StageFile(ro, "binflow-x-*.tmp"); !errors.Is(err, ErrStagingUnavailable) {
		t.Errorf("StageFile refusal wraps ErrStagingUnavailable: %v", err)
	} else if !strings.Contains(err.Error(), ro) {
		t.Errorf("StageFile refusal %q does not name the attempted root %q", err.Error(), ro)
	}
}

// TestStageFileLandsUnderConfiguredRoot pins the file-level seam: the
// staged file lives under the configured root with the caller's pattern.
func TestStageFileLandsUnderConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "staging")
	f, err := StageFile(root, "binflow-x-*.tmp")
	if err != nil {
		t.Fatalf("StageFile: %v", err)
	}
	defer func() { _ = f.Close() }()
	defer func() { _ = os.Remove(f.Name()) }()
	if dir := filepath.Dir(f.Name()); dir != root {
		t.Errorf("staged file landed in %s, want %s", dir, root)
	}
	if base := filepath.Base(f.Name()); !strings.HasPrefix(base, "binflow-x-") || !strings.HasSuffix(base, ".tmp") {
		t.Errorf("staged file name %q lacks the binflow-x-*.tmp shape", base)
	}
}

// readOnlyDir returns a freshly chmod-0500 temp dir (the read-only-rootfs
// stand-in) and schedules the perm restore. Skipped where write bits are
// not enforced: windows (chmod is a no-op on dirs) and root.
func readOnlyDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory write bits are not enforced for this runner; the read-only stand-in cannot bite")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod 0500 %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // before t.TempDir's own cleanup (LIFO)
	return dir
}
