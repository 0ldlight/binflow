package repo_test

// T-490 (FR-156.1): the blackedOut write gate — the behavior half of the
// M16 T-439 drift closure. A local repository whose config carries
// blackedOut:true refuses every deploy face with the spec's exact 404
// (rest-api.md section 1.2 step 6 / repo-semantics.md section 2 step 2 —
// both medium confidence, pinned here per the confidence discipline), and
// the refusal order is assertValidPath's own: the blackout arm answers
// BEFORE the include/exclude patterns and the permission pair. Reads and
// deletes are deliberately untouched (the spec's read-side listing-404
// note is registered spec-pending in the T-490 report, not implemented).

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// blackout flips the repository's blackedOut mark through the config plane
// (the round-trip face itself is local_config_fields_test.go's table).
func blackout(t *testing.T, e *env, key string, on bool) {
	t.Helper()
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Config: fmt.Sprintf(`{"blackedOut":%t}`, on),
	}); err != nil {
		t.Fatalf("UpdateRepo(%s, blackedOut=%t): %v", key, on, err)
	}
}

// assertBlackoutRefusal pins the exact verdict: a *repo.StatusError, 404,
// the spec message naming the repository and the addressed path, and the
// ErrBlackedOut sentinel as its cause.
func assertBlackoutRefusal(t *testing.T, err error, repoKey, path string) {
	t.Helper()
	if err == nil {
		t.Fatalf("write to blacked-out %s/%s: expected refusal, got nil", repoKey, path)
	}
	var se *repo.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("write to blacked-out %s/%s: error %v is not a *repo.StatusError", repoKey, path, err)
	}
	if se.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (rest-api.md 1.2 step 6: RepoRejectException default)", se.Code)
	}
	want := fmt.Sprintf("The repository '%s' is blacked out and cannot serve artifact '%s/%s'.",
		repoKey, repoKey, path)
	if se.Message != want {
		t.Errorf("message = %q, want the spec wording %q", se.Message, want)
	}
	if !errors.Is(err, repo.ErrBlackedOut) {
		t.Errorf("cause: errors.Is(ErrBlackedOut) = false for %v", err)
	}
}

// TestBlackedOutRefusesEveryDeployFace: the gate sits on the shared write
// plane, so every landing face refuses — the generic file PUT, the folder
// deploy, the checksum-deploy's metadata half (PutFromBlob shape is the
// same resolveWriteRepo chain; the streaming Put stands in for it here and
// the MPU landing rides it in the httpapi suite), the docker layer/config
// finalize (PutLandedBlob's chain) and the docker manifest push.
func TestBlackedOutRefusesEveryDeployFace(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	mustCreateDockerRepo(t, e, "dock")
	blackout(t, e, "lib", true)
	blackout(t, e, "dock", true)

	ctx := context.Background()

	// The generic file deploy.
	_, err := e.svc.Put(ctx, admin(), "lib", "a/b.txt",
		strings.NewReader("x"), storage.BlobRef{}, "text/plain")
	assertBlackoutRefusal(t, err, "lib", "a/b.txt")

	// The folder deploy (trailing-slash spelling).
	_, err = e.svc.Put(ctx, admin(), "lib", "a/dir/",
		strings.NewReader(""), storage.BlobRef{}, "")
	assertBlackoutRefusal(t, err, "lib", "a/dir/")

	// The docker manifest push (the /v2 write face; blobs are assumed
	// committed by the adapter — the service never re-derives them).
	_, err = e.svc.PutManifest(ctx, admin(), "dock", "app",
		strings.Repeat("ab", 32), "v1",
		"application/vnd.docker.distribution.manifest.v2+json", 100,
		mkRefs("dock", "app", strings.Repeat("ab", 32), strings.Repeat("cd", 32)))
	assertBlackoutRefusal(t, err, "dock", "app/manifests/"+strings.Repeat("ab", 32))
}

// TestBlackedOutGateOrderBeforePermissions: repo-semantics section 2 runs
// assertValidPath (the blackout arm) BEFORE the permission pair, so a
// principal with no grants on a blacked-out repository learns the
// blackout, not the ACL refusal.
func TestBlackedOutGateOrderBeforePermissions(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	blackout(t, e, "lib", true)

	_, err := e.svc.Put(context.Background(), alice(), "lib", "f.txt",
		strings.NewReader("x"), storage.BlobRef{}, "text/plain")
	assertBlackoutRefusal(t, err, "lib", "f.txt")
}

// TestBlackedOutVirtualWriteRoutesToMember: a routed virtual write is the
// TARGET member's write (resolveWriteRepo) — the member's blackout mark
// refuses it, and the message names the member.
func TestBlackedOutVirtualWriteRoutesToMember(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "member")
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["member"],"defaultDeploymentRepo":"member"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virt): %v", err)
	}
	blackout(t, e, "member", true)

	_, err := e.svc.Put(context.Background(), admin(), "virt", "f.txt",
		strings.NewReader("x"), storage.BlobRef{}, "text/plain")
	assertBlackoutRefusal(t, err, "member", "f.txt")
}

// TestBlackedOutExplodeRefused: the exploded archive is a deploy — its
// staging write refuses on the target repository's mark.
func TestBlackedOutExplodeRefused(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	blackout(t, e, "lib", true)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("entry.txt")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	_, err = archRun(t, e).ExplodeArchive(context.Background(), admin(),
		repo.ExplodeRequest{RepoKey: "lib", Path: "rel/pkg.zip"}, bytes.NewReader(buf.Bytes()))
	assertBlackoutRefusal(t, err, "lib", "rel/pkg.zip")
}

// TestBlackedOutFlipOffAndFacesUnaffected: the mark is a live switch (flip
// off through the same config plane, the write succeeds again) and the
// read/delete faces stay open while it is on — the AC's gate is the DEPLOY
// plane only (the read-side listing-404 note is spec-pending; the delete
// posture is Artifactory-undocumented, so the as-built openness is pinned
// here as the boundary, not as a spec claim).
func TestBlackedOutFlipOffAndFacesUnaffected(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "lib")
	put(t, e, admin(), "lib", "keep.txt", "kept")
	blackout(t, e, "lib", true)

	ctx := context.Background()
	// Reads stay open: the cached node still serves.
	if rc, _, err := e.svc.Get(ctx, admin(), "lib", "keep.txt"); err != nil {
		t.Fatalf("Get during blackout: %v", err)
	} else {
		_ = rc.Close()
	}
	// Deletes stay open (as-built boundary).
	if err := e.svc.Delete(ctx, admin(), "lib", "keep.txt"); err != nil {
		t.Fatalf("Delete during blackout: %v", err)
	}

	// Flip off: the write plane reopens with zero further ceremony.
	blackout(t, e, "lib", false)
	put(t, e, admin(), "lib", "fresh.txt", "fresh")
}
