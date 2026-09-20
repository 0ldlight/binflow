package repo_test

// T-406 (parity hotfix): the artifacts-browser face on remote and virtual
// repositories — the rework that made the browser mirror Artifactory left
// the tree listing REMOTE and VIRTUAL repositories while the content plane
// refused both with a 400, so every click on either class rendered an error
// card ("无法展示制品"). The parity ruling:
//
//   - remote: the listing face serves the CACHE (pull-through landings are
//     ordinary node rows under the remote key; the upstream is never probed
//     from the listing face), and the folder face materializes ancestor
//     rows on read — folders are never upstream resources.
//   - virtual: aggregate listing keeps its FR-21-AC8 P2 refusal; the console
//     renders a member-aware empty state instead of firing the doomed call.

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT406RemoteCacheBrowseFace: after one pull-through fetch the cache is
// browsable — root List, folder-descend Get (ErrIsFolder + materialized
// row), and the upstream is contacted only by the content fetch, never by
// the browse faces.
func TestT406RemoteCacheBrowseFace(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	up, hits := countingUpstream(t, map[string]string{"/d/inner.txt": "remote-body"})
	createRemote(t, e, up.URL, "")

	// Seed the cache with exactly one fetch (one upstream hit).
	if _, err := getRemote(t, e, admin(), "d/inner.txt"); err != nil {
		t.Fatalf("Get(miss→land): %v", err)
	}
	before := hits.Load()

	// Root listing: cache rows only, no upstream contact.
	all, err := e.svc.List(ctx, admin(), "generic-remote", "")
	if err != nil {
		t.Fatalf("List(remote root): %v", err)
	}
	if len(all) == 0 {
		t.Fatal("List(remote root) = empty, want the landed cache rows")
	}

	// Folder descend: ErrIsFolder with the materialized marker row —
	// the pre-T-406 landing wrote file rows without ancestor folders.
	_, node, err := e.svc.Get(ctx, admin(), "generic-remote", "d/")
	if !errors.Is(err, repo.ErrIsFolder) || node == nil {
		t.Fatalf("Get(folder d/) = (%v, %v), want ErrIsFolder + node", err, node)
	}
	if node.Sha256 != metadata.FolderMarkerSHA {
		t.Fatalf("folder row sha = %q, want the empty-folder marker", node.Sha256)
	}
	// Idempotent: the second descend serves the stored marker row.
	if _, node2, err2 := e.svc.Get(ctx, admin(), "generic-remote", "d/"); !errors.Is(err2, repo.ErrIsFolder) || node2 == nil {
		t.Fatalf("Get(folder d/) second = (%v, %v), want ErrIsFolder + node", err2, node2)
	}

	// A childless folder probe falls through to the engine — protocol faces
	// legitimately serve slash-terminated resources (pypi /simple/<proj>/),
	// so the upstream answers (here 404 → the Unfound family), one hit.
	if _, _, err := e.svc.Get(ctx, admin(), "generic-remote", "nope/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("Get(childless folder) = %v, want the Unfound family wrapping ErrNodeNotFound", err)
	}
	if got := hits.Load(); got != before+1 {
		t.Fatalf("childless folder probe upstream hits = %d, want %d (cached faces silent)", got, before+1)
	}
}

// TestT406VirtualAggregateFaceOpened: the virtual aggregate refusal this
// test pinned for T-406 ("FR-21-AC8 stays P2") is RETIRED by T-412
// (FR-136.1) — List on a virtual repository answers the member union
// instead of ErrRepoTypeNotSupported. The full union/order/ACL matrix lives
// in virtual_aggregate_test.go; this pins the flip on the original T-406
// fixture (two empty members → an honest empty page, no error, no upstream
// contact).
func TestT406VirtualAggregateFaceOpened(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "maven-local", repo.PackageMaven)
	mustCreateRemote(t, e, "maven-remote-x", `{"url":"http://127.0.0.1:9099/m2"}`)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "maven-virtual", Type: repo.TypeVirtual, PackageType: repo.PackageMaven,
		Config: `{"repositories":["maven-remote-x","maven-local"],"defaultDeploymentRepo":"maven-local"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual): %v", err)
	}
	nodes, err := e.svc.List(ctx, admin(), "maven-virtual", "")
	if err != nil {
		t.Fatalf("List(virtual root) = %v, want the open aggregate face (empty)", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("List(virtual root) = %d rows, want 0 (both members empty)", len(nodes))
	}
}

// TestT406RemotePyPIProjectIndexOverridesFolderMarker: UAT carries folder
// markers under pypi remote /simple/<project>/ from the pre-protocol cache
// face. Those paths are upstream HTML resources, not browsable folders, so
// the pypi adapter must still be able to fetch through the stale marker.
func TestT406RemotePyPIProjectIndexOverridesFolderMarker(t *testing.T) {
	ctx := context.Background()
	up, hits := countingUpstream(t, map[string]string{"/simple/six/": "<html>six</html>"})
	e := newEnv(t)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "pypi-remote", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
		Config: `{"url":"` + up.URL + `","allowPrivateUpstream":true}`,
	}); err != nil {
		t.Fatalf("CreateRepo(pypi-remote): %v", err)
	}
	if err := e.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: metadata.FolderMarkerSHA, Sha1: "folder", Md5: "folder", Size: 0,
	}); err != nil {
		t.Fatalf("seed folder blob: %v", err)
	}
	if err := e.md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "pypi-remote", Path: "simple/six/", Sha256: metadata.FolderMarkerSHA,
	}); err != nil {
		t.Fatalf("seed stale folder marker: %v", err)
	}

	rc, node, err := e.svc.Get(ctx, admin(), "pypi-remote", "simple/six/")
	if err != nil {
		t.Fatalf("Get(pypi simple page through stale marker): %v", err)
	}
	defer rc.Close() //nolint:errcheck // test cleanup best effort; Get already returned the body
	body, rerr := io.ReadAll(rc)
	if rerr != nil {
		t.Fatalf("read upstream page: %v", rerr)
	}
	if string(body) != "<html>six</html>" || node.Sha256 == metadata.FolderMarkerSHA {
		t.Fatalf("Get body/node = %q / %+v; want the upstream page and a file node", body, node)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
}
