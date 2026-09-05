package repo_test

// The exploded-upload staging matrix (T-477 — the T-474/T-476 spool
// family's service-layer arm): ExplodeArchive's §4.2 to_extract_ spool
// must stage on the storage volume's staging root (the ConfigureStaging
// wiring the cmd assembly installs), never depend on a writable OS temp
// directory. The pins mirror the adapter family's spool_readonly tests:
// the incident topology (read-only TMPDIR) must refuse the OLD form
// (staging root "" — the pre-fix posture) and pass in the NEW form (the
// production wiring), the refusal face is 507 with bounded disclosure,
// and no to_extract_ residue survives either leg.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

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

// explodeStagingStatus runs one exploded upload expecting a refusal and
// renders its StatusError shape (archErr's sibling, kept here so the
// staging matrix asserts the typed face directly).
func explodeStagingStatus(t *testing.T, e *env, req repo.ExplodeRequest, body []byte) *repo.StatusError {
	t.Helper()
	_, err := archRun(t, e).ExplodeArchive(context.Background(), admin(), req, bytes.NewReader(body))
	if err == nil {
		t.Fatalf("ExplodeArchive(%s): unexpected success", req.Path)
	}
	var se *repo.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("refusal %v is not a *repo.StatusError", err)
	}
	return se
}

// assertBoundedRefusal pins the 507 face's bounded disclosure: the message
// names the attempted root (label) and never the temp-file family or the
// os-error internals; the wrapped cause still names the resolved root so
// the server log and error chain stay diagnosable.
func assertBoundedRefusal(t *testing.T, se *repo.StatusError, label, resolvedRoot string) {
	t.Helper()
	const wantCode = 507 // http.StatusInsufficientStorage, the family's mapping
	if se.Code != wantCode {
		t.Fatalf("refusal code = %d, want %d (message %q)", se.Code, wantCode, se.Message)
	}
	if !strings.Contains(se.Message, label) {
		t.Errorf("refusal message %q does not name the attempted root %q", se.Message, label)
	}
	for _, leak := range []string{"to_extract_", "permission denied", "read-only file system"} {
		if strings.Contains(se.Message, leak) {
			t.Errorf("refusal message %q leaks %q (os internals ride the server log only)", se.Message, leak)
		}
	}
	cause := errors.Unwrap(se)
	if cause == nil || !strings.Contains(cause.Error(), resolvedRoot) {
		t.Errorf("wrapped cause %v does not name the resolved root %s", cause, resolvedRoot)
	}
}

// TestExplodeArchiveSurvivesReadOnlyOsTemp is the incident red→green pin:
// under a read-only OS temp directory (the read-only-rootfs topology that
// killed the pre-fix spool with a bare 500), the production wiring
// (harness default = <data_dir>/staging, the cmd assembly's exact path)
// completes the exploded upload and the entries land.
func TestExplodeArchiveSurvivesReadOnlyOsTemp(t *testing.T) {
	// All writable scratch (the env's data/db dirs, the read-only stand-in
	// itself) is allocated under the REAL temp dir before TMPDIR is
	// poisoned — t.TempDir honors TMPDIR at call time.
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildZip(t, map[string]string{"a.txt": "alpha", "sub/b.txt": "bravo"})
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	res := explode(t, e, admin(), repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, body)
	if res.Files != 2 {
		t.Fatalf("files = %d, want 2", res.Files)
	}
	if got := mustGet(t, e, "lib", "rel/a.txt"); got.Size != int64(len("alpha")) {
		t.Fatalf("entry a.txt = %+v", got)
	}
}

// TestExplodeArchiveOsTempFallbackFailsWhenTmpReadOnly is the old-form
// control pin: with the staging root reset to "" (the pre-fix posture —
// the OS-temp spool), the same read-only TMPDIR must refuse. The refusal
// is the 507 face naming "the OS temp directory" honestly, bounded.
func TestExplodeArchiveOsTempFallbackFailsWhenTmpReadOnly(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildZip(t, map[string]string{"a.txt": "alpha"})
	ro := mustReadOnlyDir(t)
	repo.ConfigureStaging(e.svc, "") // the bare, pre-fix posture
	t.Setenv("TMPDIR", ro)

	se := explodeStagingStatus(t, e, repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, body)
	assertBoundedRefusal(t, se, "the OS temp directory", ro)
}

// TestExplodeArchiveStagingRefusalIsDiagnosableAndBounded pins the
// configured-root refusal: an existing but unwritable staging root (0444)
// answers 507 naming the configured root verbatim, still bounded — the
// operator's own configuration value is the one actionable fact.
func TestExplodeArchiveStagingRefusalIsDiagnosableAndBounded(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	body := buildZip(t, map[string]string{"a.txt": "alpha"})
	ro := mustReadOnlyDir(t)
	repo.ConfigureStaging(e.svc, ro)

	se := explodeStagingStatus(t, e, repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, body)
	assertBoundedRefusal(t, se, "\""+ro+"\"", ro)
}

// TestExplodeArchiveStagesUnderConfiguredRoot pins the new form's
// placement: a staging root that does not exist yet is created on demand
// (the per-call MkdirAll posture — a volume mounted after boot
// self-heals) and the upload completes through it.
func TestExplodeArchiveStagesUnderConfiguredRoot(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	root := filepath.Join(t.TempDir(), "nested", "staging") // does not exist yet
	repo.ConfigureStaging(e.svc, root)

	body := buildZip(t, map[string]string{"a.txt": "alpha"})
	res := explode(t, e, admin(), repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, body)
	if res.Files != 1 {
		t.Fatalf("files = %d, want 1", res.Files)
	}
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		t.Fatalf("staging root %s not created on demand: %v", root, err)
	}
}

// TestExplodeArchiveLeavesNoStagingResidue pins the cleanup contract on
// both exit shapes: the success leg and the post-staging failure leg
// (a corrupt archive refuses AFTER the body staged) leave no to_extract_
// file behind in the staging root.
func TestExplodeArchiveLeavesNoStagingResidue(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	root := filepath.Join(e.dataDir, "staging") // the harness's production posture

	explode(t, e, admin(), repo.ExplodeRequest{RepoKey: "lib", Path: "ok/pkg.zip"},
		buildZip(t, map[string]string{"a.txt": "alpha"}))
	if _, err := archRun(t, e).ExplodeArchive(context.Background(), admin(),
		repo.ExplodeRequest{RepoKey: "lib", Path: "bad/pkg.zip"},
		strings.NewReader("not a zip body")); err == nil {
		t.Fatal("corrupt archive exploded unexpectedly")
	}

	assertNoToExtractResidue(t, root)
}

// assertNoToExtractResidue fails the test when any to_extract_ spool file
// survived under root.
func assertNoToExtractResidue(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read staging root %s: %v", root, err)
	}
	for _, ent := range entries {
		if strings.HasPrefix(ent.Name(), "to_extract_") {
			t.Errorf("spool residue %s survived the explode chain under %s", ent.Name(), root)
		}
	}
}
