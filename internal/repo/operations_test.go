package repo_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The copy/move service matrix (M12 T-339): the five-stage pipeline over the
// real engines. The REST-face legs (routing, the license gate's dual form,
// the wire shape) live in internal/httpapi/t339_operations_test.go; this
// file pins the SERVICE semantics of docs/reverse/repo-operations.md
// section 1.

// cmRun runs one operation and fails the test on a fatal (non-message)
// error.
func cmRun(t *testing.T, e *env, p *repo.Principal, req repo.CopyMoveRequest) *repo.CopyMoveResult {
	t.Helper()
	svc, ok := e.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatalf("service does not carry the copy/move capability")
	}
	res, err := svc.CopyOrMove(context.Background(), p, req)
	if err != nil {
		t.Fatalf("CopyOrMove(%s %s->%s): %v", req.Op, req.SrcRepo, req.TargetRepo, err)
	}
	return res
}

// cmRunErr runs one operation expecting a fatal refusal and renders the
// StatusError's exact shape.
func cmRunErr(t *testing.T, e *env, p *repo.Principal, req repo.CopyMoveRequest) (int, string) {
	t.Helper()
	svc, ok := e.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatalf("service does not carry the copy/move capability")
	}
	_, err := svc.CopyOrMove(context.Background(), p, req)
	if err == nil {
		t.Fatalf("CopyOrMove(%s %s->%s): expected a fatal refusal, got success", req.Op, req.SrcRepo, req.TargetRepo)
	}
	var se *repo.StatusError
	if errors.As(err, &se) {
		return se.Code, se.Message
	}
	t.Fatalf("refusal is not a *StatusError: %v", err)
	return 0, ""
}

// mustGet asserts a node exists and returns it.
func mustGet(t *testing.T, e *env, repoKey, path string) *metadata.Node {
	t.Helper()
	n, err := e.md.Nodes().Get(context.Background(), repoKey, path)
	if err != nil {
		t.Fatalf("get %s/%s: %v", repoKey, path, err)
	}
	return n
}

// assertGone asserts no node exists at the path.
func assertGone(t *testing.T, e *env, repoKey, path string) {
	t.Helper()
	if _, err := e.md.Nodes().Get(context.Background(), repoKey, path); err == nil {
		t.Fatalf("%s/%s still exists", repoKey, path)
	} else if !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("get %s/%s: %v", repoKey, path, err)
	}
}

// msgText renders the messages for substring assertions.
func msgText(res *repo.CopyMoveResult) string {
	var b strings.Builder
	for _, m := range res.Messages {
		fmt.Fprintf(&b, "[%s] %s\n", m.Level, m.Message)
	}
	return b.String()
}

// TestCopyFileBasic: the happy-path file copy — source kept, target materialized
// with the same checksums (zero copy through the ledger), the §1.5 INFO
// summary, and the metadata carry.
func TestCopyFileBasic(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "lib/a.jar", "artifact-bytes")
	blobsBefore, err := e.md.Blobs().Count(context.Background())
	if err != nil {
		t.Fatalf("blob count: %v", err)
	}

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "lib/a.jar", TargetRepo: "dst", TargetPath: "lib/a.jar",
	})
	if res.HTTPStatus != 200 {
		t.Fatalf("status = %d, want 200 (%s)", res.HTTPStatus, msgText(res))
	}
	if len(res.Messages) != 1 || res.Messages[0].Level != "INFO" ||
		!strings.Contains(res.Messages[0].Message, "copying src/lib/a.jar to dst/lib/a.jar completed successfully, 1 artifacts and 0 folders were copied") {
		t.Fatalf("summary wrong: %s", msgText(res))
	}
	// Source kept; target carries the same checksums and size.
	src, dst := mustGet(t, e, "src", "lib/a.jar"), mustGet(t, e, "dst", "lib/a.jar")
	if src.Sha256 != dst.Sha256 || src.Size != dst.Size {
		t.Fatalf("checksum/size drift: %+v vs %+v", src, dst)
	}
	// Zero copy: the ledger grew by zero rows.
	blobsAfter, _ := e.md.Blobs().Count(context.Background())
	if blobsAfter != blobsBefore {
		t.Fatalf("ledger grew %d -> %d; copy must be zero-copy", blobsBefore, blobsAfter)
	}
	// The target's bytes read back identical (checksums 随行对账).
	rc, _, err := e.svc.Get(context.Background(), admin(), "dst", "lib/a.jar")
	if err != nil {
		t.Fatalf("get dst copy: %v", err)
	}
	defer rc.Close() //nolint:errcheck // test read
	body := make([]byte, len("artifact-bytes"))
	if _, err := rc.Read(body); err != nil {
		t.Fatalf("read copy: %v", err)
	}
	if string(body) != "artifact-bytes" {
		t.Fatalf("body = %q", body)
	}
	// Metadata carry: created identity rides along (§1.4).
	if dst.CreatedBy != src.CreatedBy || dst.CreatedAt != src.CreatedAt {
		t.Fatalf("created identity not carried: %+v vs %+v", src, dst)
	}
}

// TestCopyPropertiesCarry: node properties ride along (§1.4's 全量复制; the
// M10/T-317 property system's family semantics).
func TestCopyPropertiesCarry(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "a/b.bin", "x")
	if err := e.md.NodeProps().Merge(context.Background(), "src", "a/b.bin",
		map[string][]string{"lic": {"apache-2.0"}, "team": {"core", "registry"}}); err != nil {
		t.Fatalf("seed props: %v", err)
	}
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a/b.bin", TargetRepo: "dst", TargetPath: "x/b.bin",
	})
	got, err := e.md.NodeProps().List(context.Background(), "dst", "x/b.bin")
	if err != nil {
		t.Fatalf("list dst props: %v", err)
	}
	if len(got["lic"]) != 1 || got["lic"][0] != "apache-2.0" || len(got["team"]) != 2 {
		t.Fatalf("properties did not ride along: %v", got)
	}
	// Source keeps its set (copy, not move).
	if src, _ := e.md.NodeProps().List(context.Background(), "src", "a/b.bin"); len(src) != 2 {
		t.Fatalf("source properties lost on a copy: %v", src)
	}
}

// TestCopyFolderTree: the tree walk, folder properties, the unix target
// adjustments (into-dir, rename, trailing slash) and the artifact/folder
// counts.
func TestCopyFolderTree(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "proj/1.0/proj-1.0.jar", "j")
	put(t, e, admin(), "src", "proj/1.0/proj-1.0.pom", "p")
	put(t, e, admin(), "src", "proj/2.0/proj-2.0.jar", "j2")
	put(t, e, admin(), "src", "outside.bin", "o")
	if err := e.md.NodeProps().Merge(context.Background(), "src", "proj/1.0/", map[string][]string{"rel": {"stable"}}); err != nil {
		t.Fatalf("seed folder props: %v", err)
	}

	// Rename: target missing -> the source folder BECOMES the target.
	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "proj", TargetRepo: "dst", TargetPath: "renamed",
	})
	if res.Artifacts != 3 || res.Folders != 3 { // proj/, 1.0/, 2.0/
		t.Fatalf("counts = %d/%d, want 3/3 (%s)", res.Artifacts, res.Folders, msgText(res))
	}
	mustGet(t, e, "dst", "renamed/1.0/proj-1.0.jar")
	if fp, _ := e.md.NodeProps().List(context.Background(), "dst", "renamed/1.0/"); len(fp["rel"]) != 1 {
		t.Fatalf("folder properties did not ride along: %v", fp)
	}

	// Into-dir: an existing folder target receives the source UNDER it.
	put(t, e, admin(), "dst", "bucket/", "")
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "proj", TargetRepo: "dst", TargetPath: "bucket",
	})
	mustGet(t, e, "dst", "bucket/proj/2.0/proj-2.0.jar")

	// Trailing slash: explicit into-directory, same shape.
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "proj/1.0", TargetRepo: "dst", TargetPath: "v10/",
	})
	mustGet(t, e, "dst", "v10/1.0/proj-1.0.jar")

	// Folder addressed WITHOUT its trailing slash (§1.1's auto-slash).
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "proj/2.0", TargetRepo: "dst", TargetPath: "v20",
	})
	mustGet(t, e, "dst", "v20/proj-2.0.jar")
}

// TestMoveSemantics: move = copy + the per-file delete; the source tree
// (folder rows included) vanishes, the target serves the bytes, and the
// shared blob survives for GC to never see a dangling reference.
func TestMoveSemantics(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "d/f1.bin", "one")
	put(t, e, admin(), "src", "d/f2.bin", "two")
	put(t, e, admin(), "src", "d/sub/f3.bin", "three")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpMove, SrcRepo: "src", SrcPath: "d", TargetRepo: "dst", TargetPath: "d",
	})
	if res.HTTPStatus != 200 || res.Artifacts != 3 {
		t.Fatalf("move failed: %s", msgText(res))
	}
	assertGone(t, e, "src", "d/f1.bin")
	assertGone(t, e, "src", "d/sub/f3.bin")
	assertGone(t, e, "src", "d/")        // the folder row itself
	assertGone(t, e, "src", "d/sub/")    // pruned with the subtree
	mustGet(t, e, "dst", "d/sub/f3.bin") // ...while the target holds everything
	// The blob is still referenced (the target's node) and readable.
	rc, _, err := e.svc.Get(context.Background(), admin(), "dst", "d/f2.bin")
	if err != nil {
		t.Fatalf("get moved node: %v", err)
	}
	rc.Close() //nolint:errcheck // test read
}

// TestMovePartialKeepsFailedFolders: with failFast=0, items that fail keep
// their source rows and pin their ancestor folders (no half-deleted trees).
func TestMovePartialKeepsFailedFolders(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "d/ok.bin", "v")
	put(t, e, admin(), "src", "d/blocked.bin", "v")
	// bob may read the source and write the target, but has no delete on
	// d/blocked.bin (the move's per-item source-delete gate, §1.3 #4).
	e.az.add("bob", repo.ActionRead, "")
	e.az.add("bob", repo.ActionWrite, "")
	e.az.add("bob", repo.ActionDelete, "d/o")

	res := cmRun(t, e, &repo.Principal{Name: "bob"}, repo.CopyMoveRequest{
		Op: repo.OpMove, SrcRepo: "src", SrcPath: "d", TargetRepo: "dst", TargetPath: "d",
	})
	if res.HTTPStatus != 403 {
		t.Fatalf("status = %d, want 403 (%s)", res.HTTPStatus, msgText(res))
	}
	mustGet(t, e, "src", "d/blocked.bin") // the refused item stays
	mustGet(t, e, "src", "d/")            // ...and pins its folder row
	mustGet(t, e, "dst", "d/ok.bin")      // the passing item did move
	assertGone(t, e, "src", "d/ok.bin")
}

// TestDryRunZeroSideEffects: dry=1 walks the full validation chain, reports
// the Dry-run summary and leaves the store untouched (GET-before/after).
func TestDryRunZeroSideEffects(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "d/f.bin", "v")
	nodesBefore, err := e.md.Nodes().ListByPrefix(context.Background(), "dst", "")
	if err != nil {
		t.Fatalf("list dst: %v", err)
	}

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "d", TargetRepo: "dst", TargetPath: "d", DryRun: true,
	})
	if res.Artifacts != 1 || res.Folders != 1 {
		t.Fatalf("dry counts = %d/%d (%s)", res.Artifacts, res.Folders, msgText(res))
	}
	if !strings.HasPrefix(res.Messages[len(res.Messages)-1].Message, "Dry run for copying") {
		t.Fatalf("summary lacks the Dry-run prefix: %s", msgText(res))
	}
	nodesAfter, _ := e.md.Nodes().ListByPrefix(context.Background(), "dst", "")
	if len(nodesAfter) != len(nodesBefore) {
		t.Fatalf("dry run wrote %d rows", len(nodesAfter)-len(nodesBefore))
	}
	assertGone(t, e, "dst", "d/f.bin")
}

// TestOverride401SpecialCase: §1.3 #5 — an existing target without the
// delete grant answers the deliberate 401 (not 403), as a per-item message.
func TestOverride401SpecialCase(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "a.bin", "new")
	put(t, e, admin(), "dst", "a.bin", "old")
	// bob: read source, write target, NO delete on the target path.
	e.az.add("bob", repo.ActionRead, "")
	e.az.add("bob", repo.ActionWrite, "")

	res := cmRun(t, e, &repo.Principal{Name: "bob"}, repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "a.bin",
	})
	if res.HTTPStatus != 401 {
		t.Fatalf("status = %d, want 401 (%s)", res.HTTPStatus, msgText(res))
	}
	found := false
	for _, m := range res.Messages {
		if m.Status == 401 && strings.Contains(m.Message, "User doesn't have permissions to override 'dst/a.bin'. Needs delete permissions.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the 401 override message is missing: %s", msgText(res))
	}
	// The old target survives untouched.
	if n := mustGet(t, e, "dst", "a.bin"); n.Size != int64(len("old")) {
		t.Fatalf("target was modified despite the refusal")
	}
}

// TestPermissionMatrix: the §1.3 chain's 403 arms — read, create, and the
// move-only delete gate — with the spec's message texts.
func TestPermissionMatrix(t *testing.T) {
	t.Run("read denied", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "src")
		mustCreateRepo(t, e, "dst")
		put(t, e, admin(), "src", "a.bin", "v")
		e.az.add("bob", repo.ActionWrite, "")
		// The ROOT read gate is fatal (the Get posture: an unauthorized
		// principal never aims the store walk) and carries §1.3 #1's text.
		code, msg := cmRunErr(t, e, &repo.Principal{Name: "bob"}, repo.CopyMoveRequest{
			Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "a.bin",
		})
		if code != 403 || !strings.Contains(msg,
			"User doesn't have permissions to read 'src/a.bin'. Needs read permissions.") {
			t.Fatalf("read refusal wrong: (%d, %q)", code, msg)
		}
	})
	t.Run("create denied", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "src")
		mustCreateRepo(t, e, "dst")
		put(t, e, admin(), "src", "a.bin", "v")
		e.az.add("bob", repo.ActionRead, "")
		res := cmRun(t, e, &repo.Principal{Name: "bob"}, repo.CopyMoveRequest{
			Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "a.bin",
		})
		if res.HTTPStatus != 403 || !strings.Contains(msgText(res),
			"User doesn't have permissions to create 'dst/a.bin'. Needs write permissions.") {
			t.Fatalf("create refusal wrong: %s", msgText(res))
		}
	})
	t.Run("move delete denied", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "src")
		mustCreateRepo(t, e, "dst")
		put(t, e, admin(), "src", "a.bin", "v")
		e.az.add("bob", repo.ActionRead, "")
		e.az.add("bob", repo.ActionWrite, "")
		res := cmRun(t, e, &repo.Principal{Name: "bob"}, repo.CopyMoveRequest{
			Op: repo.OpMove, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "a.bin",
		})
		if res.HTTPStatus != 403 || !strings.Contains(msgText(res),
			"User doesn't have permissions to move 'src/a.bin'. Needs delete permissions.") {
			t.Fatalf("move-delete refusal wrong: %s", msgText(res))
		}
		assertGone(t, e, "dst", "a.bin") // nothing landed: validation precedes transfer
	})
}

// TestPrecheckMatrix: the §1.2 fatal refusals with the spec's verbatim
// messages and codes.
func TestPrecheckMatrix(t *testing.T) {
	setup := func(t *testing.T) *env {
		e := newEnv(t)
		mustCreateRepo(t, e, "src")
		mustCreateRepo(t, e, "dst")
		// The remote/virtual rows are seeded through the store (the
		// service's config validation demands upstream URLs and members
		// these class-refusal legs do not care about).
		for _, r := range []*metadata.Repo{
			{RepoKey: "rem", Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: "{}"},
			{RepoKey: "virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric, Config: "{}"},
		} {
			if err := e.md.Repos().Create(context.Background(), r); err != nil {
				t.Fatalf("seed %s: %v", r.RepoKey, err)
			}
		}
		put(t, e, admin(), "src", "a.bin", "v")
		return e
	}
	cases := []struct {
		name    string
		mutate  func(req *repo.CopyMoveRequest)
		code    int
		message string
	}{
		{
			name:   "remote target",
			mutate: func(r *repo.CopyMoveRequest) { r.TargetRepo = "rem" },
			code:   400,
			message: "Target repository rem is a remote repository. " +
				"copy/move to remote repositories is not allowed.",
		},
		{
			name:   "virtual target",
			mutate: func(r *repo.CopyMoveRequest) { r.TargetRepo = "virt" },
			code:   400,
			message: "Target repository virt is a virtual repository. " +
				"copy/move to virtual repositories is not allowed.",
		},
		{
			name: "jfrog target blocked",
			mutate: func(r *repo.CopyMoveRequest) {
				r.TargetRepo, r.TargetPath = "dst", ".jfrog/metadata"
			},
			code:    404,
			message: "Internal metadata request blocked",
		},
		{
			name:    "same path",
			mutate:  func(r *repo.CopyMoveRequest) { r.TargetRepo, r.TargetPath = "src", "a.bin" },
			code:    400,
			message: "Skipping copy src/a.bin: Destination and source are the same",
		},
		{
			name:    "unknown target repo",
			mutate:  func(r *repo.CopyMoveRequest) { r.TargetRepo = "nope" },
			code:    400,
			message: "repository nope not found",
		},
		{
			name:    "missing source item",
			mutate:  func(r *repo.CopyMoveRequest) { r.SrcPath = "nothing.bin" },
			code:    400,
			message: "Could not find item at src/nothing.bin",
		},
		{
			name:    "empty target key",
			mutate:  func(r *repo.CopyMoveRequest) { r.TargetRepo = "" },
			code:    400,
			message: "Target repository key is empty",
		},
		{
			name: "folder under file",
			mutate: func(r *repo.CopyMoveRequest) {
				r.SrcPath, r.TargetRepo, r.TargetPath = "adir", "dst", "b.bin"
			},
			code:    400,
			message: "Can't move folder under file 'dst/b.bin'.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := setup(t)
			put(t, e, admin(), "src", "adir/", "")  // folder source for the under-file arm
			put(t, e, admin(), "dst", "b.bin", "v") // the file a folder cannot land under
			req := repo.CopyMoveRequest{
				Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "c.bin",
			}
			tc.mutate(&req)
			code, msg := cmRunErr(t, e, admin(), req)
			if code != tc.code || !strings.Contains(msg, tc.message) {
				t.Fatalf("refusal = (%d, %q), want (%d, contains %q)", code, msg, tc.code, tc.message)
			}
		})
	}
}

// TestGovernancePatternRefusal: the §1.3 #3 include/exclude arm with the
// spec's 403 (BinFlow's upload-plane 409 divergence is the content plane's).
func TestGovernancePatternRefusal(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "gov", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"excludesPattern":"**/*.secret"}`,
	}); err != nil {
		t.Fatalf("create gov repo: %v", err)
	}
	put(t, e, admin(), "src", "k.txt", "v")
	put(t, e, admin(), "src", "k.secret", "v")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "k.secret", TargetRepo: "gov", TargetPath: "k.secret",
	})
	if res.HTTPStatus != 403 || !strings.Contains(msgText(res),
		"The repository 'gov' rejected the path 'gov/k.secret' due to a conflict with its include/exclude patterns.") {
		t.Fatalf("pattern refusal wrong: %s", msgText(res))
	}
	assertGone(t, e, "gov", "k.secret")
}

// TestQuotaExtension: the BinFlow W26 parity arm — a copy into an over-quota
// target fails the ITEM with the 413, never the request.
func TestQuotaExtension(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "tiny", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"quotaBytes":4}`,
	}); err != nil {
		t.Fatalf("create tiny repo: %v", err)
	}
	put(t, e, admin(), "src", "big.bin", "0123456789")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "big.bin", TargetRepo: "tiny", TargetPath: "big.bin",
	})
	if res.HTTPStatus != 413 {
		t.Fatalf("status = %d, want 413 (%s)", res.HTTPStatus, msgText(res))
	}
	assertGone(t, e, "tiny", "big.bin")
}

// TestFailFastStopsWalk: failFast=1 halts the walk at the first problem —
// the later sibling is never touched.
func TestFailFastStopsWalk(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "dst", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"excludesPattern":"**/*secret*"}`,
	}); err != nil {
		t.Fatalf("create dst: %v", err)
	}
	put(t, e, admin(), "src", "d/a-secret", "v")
	put(t, e, admin(), "src", "d/b.txt", "v")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "d", TargetRepo: "dst", TargetPath: "d", FailFast: true,
	})
	if res.HTTPStatus != 403 {
		t.Fatalf("status = %d, want 403 (%s)", res.HTTPStatus, msgText(res))
	}
	// Sorted walk: a-secret fails first, failFast stops before b.txt.
	assertGone(t, e, "dst", "d/b.txt")
}

// TestEmptySourcePathWarning: the §1.1 warning + the root-tree semantics
// (the whole repository becomes the source).
func TestEmptySourcePathWarning(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "x/a.bin", "v")
	put(t, e, admin(), "src", "y/b.bin", "v")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "", TargetRepo: "dst", TargetPath: "",
	})
	if res.HTTPStatus != 200 {
		t.Fatalf("status = %d (%s)", res.HTTPStatus, msgText(res))
	}
	if !strings.Contains(msgText(res), "Source repository path is empty, path set to root") {
		t.Fatalf("the empty-source warning is missing: %s", msgText(res))
	}
	mustGet(t, e, "dst", "x/a.bin")
	mustGet(t, e, "dst", "y/b.bin")
}

// TestOverwriteReplacesProperties: §1.4's delete-then-copy — an overridden
// target keeps NEITHER its old bytes NOR its old properties; the source's
// property set is the whole set.
func TestOverwriteReplacesProperties(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "a.bin", "new")
	put(t, e, admin(), "dst", "a.bin", "old")
	if err := e.md.NodeProps().Merge(context.Background(), "dst", "a.bin",
		map[string][]string{"stale": {"yes"}}); err != nil {
		t.Fatalf("seed target props: %v", err)
	}
	if err := e.md.NodeProps().Merge(context.Background(), "src", "a.bin",
		map[string][]string{"fresh": {"yes"}}); err != nil {
		t.Fatalf("seed source props: %v", err)
	}

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "a.bin", TargetRepo: "dst", TargetPath: "a.bin",
	})
	if res.HTTPStatus != 200 {
		t.Fatalf("overwrite copy failed: %s", msgText(res))
	}
	got, _ := e.md.NodeProps().List(context.Background(), "dst", "a.bin")
	if len(got["stale"]) != 0 || len(got["fresh"]) != 1 {
		t.Fatalf("override kept old properties / lost new ones: %v", got)
	}
	if n := mustGet(t, e, "dst", "a.bin"); n.Size != int64(len("new")) {
		t.Fatalf("override kept the old bytes: size %d", n.Size)
	}
}

// TestEmptyTargetFolderSwept: §1.4's orphan rule — a target folder the
// transfer created whose every child was refused is removed at the end.
func TestEmptyTargetFolderSwept(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "gov", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"excludesPattern":"**/*.secret"}`,
	}); err != nil {
		t.Fatalf("create gov repo: %v", err)
	}
	put(t, e, admin(), "src", "d/only.secret", "v")

	res := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "d", TargetRepo: "gov", TargetPath: "d",
	})
	if res.HTTPStatus != 403 {
		t.Fatalf("status = %d, want 403 (%s)", res.HTTPStatus, msgText(res))
	}
	// No empty "d/" row may linger in the target.
	if _, err := e.md.Nodes().Get(context.Background(), "gov", "d/"); err == nil {
		t.Fatalf("the empty target folder survived the sweep")
	}
}

// copyObserver records the observer fire for the async-trigger tests.
type copyObserver struct {
	mu    sync.Mutex
	fires []string
	done  chan struct{}
}

func (o *copyObserver) AfterCopyMove(_ context.Context, op, targetRepo string, dirs []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.fires = append(o.fires, op+" "+targetRepo+" "+strings.Join(dirs, ","))
	select {
	case <-o.done:
	default:
		close(o.done)
	}
}

// TestCopyObserverFires: §1.4 — copy fires the async recompute trigger with
// the candidate directory set; move and dry runs never fire it.
func TestCopyObserverFires(t *testing.T) {
	e := newEnv(t)
	obs := &copyObserver{done: make(chan struct{})}
	repo.AttachCopyMoveObserver(e.svc, obs)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "com/acme/lib/1.0/a.jar", "v")
	put(t, e, admin(), "src", "com/acme/lib/1.0/b.jar", "v")

	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "com/acme/lib/1.0/a.jar", TargetRepo: "dst", TargetPath: "com/acme/lib/1.0/a.jar",
	})
	select {
	case <-obs.done:
	case <-time.After(2 * time.Second):
		t.Fatalf("the copy observer never fired")
	}
	obs.mu.Lock()
	fires := append([]string(nil), obs.fires...)
	obs.mu.Unlock()
	if len(fires) != 1 || fires[0] != "copy dst com/acme/lib/1.0/" {
		t.Fatalf("observer fire = %v, want one copy fire with the candidate dir", fires)
	}

	// Move and dry run do not fire.
	obs2 := &copyObserver{done: make(chan struct{})}
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpMove, SrcRepo: "src", SrcPath: "com/acme/lib/1.0/b.jar", TargetRepo: "dst", TargetPath: "com/acme/lib/1.0/b.jar",
	})
	cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "dst", SrcPath: "com/acme/lib/1.0/a.jar", TargetRepo: "src", TargetPath: "com/acme/lib/1.0/a.jar", DryRun: true,
	})
	select {
	case <-obs2.done:
		t.Fatalf("move/dry must not fire the observer")
	case <-time.After(100 * time.Millisecond):
	}
}

// dryRunBudget is the wall-clock ceiling for the 10k-tree dry run, in two
// postures — FR-121 escape #1 (T-361, race recalibration; NOT a skip):
//
//   - no-race: 5s, the production P95 proxy (P54). Every no-race execution
//     asserts the production number unchanged — this test has no -short
//     guard, so CircleCI's fast-test leg (`go test ./internal/... -short`,
//     no -race) and any bare local `go test` run the arm in full.
//   - race: 45s (9x the production figure). Measured calibration on the
//     16-core dev machine, 2026-08-31 (T-361): the same walk takes
//     0.59-0.68s without the detector and 11.5-14.0s WITH it, isolated —
//     a 20-24x inflation structural to instrumenting a 10k-node metadata
//     walk — and full-tree parallel `make test` load adds roughly another
//     2x (the T-339/T-356 flake: one miss in 35-package first rounds,
//     isolated reruns green). 45s sits ~3x above the worst isolated race
//     observation with load headroom, while any regression this arm guards
//     (an accidental per-node physical I/O or an O(n^2) walk — the class
//     T-339 landed it against) blows into minutes under race too and still
//     fails loudly. Skipping instead would leave the default `make test`
//     gate (race) with no budget signal at all.
func dryRunBudget() time.Duration {
	if raceEnabled { // FR-121 escape #1 — this package's only race-keyed budget arm
		return 45 * time.Second
	}
	return 5 * time.Second
}

// TestBigTreeCopyNo5xx: the AC3 scale leg — a ten-thousand-node tree copy
// answers with zero item-level 5xx messages, a sampled checksum match, and
// the dry run over the same tree stays under the budget (dryRunBudget:
// 5s production figure, 45s under -race — FR-121 escape #1). The tree is
// seeded through the store (one shared blob row, many node rows): the copy
// path is a metadata walk by design (zero-copy), so this is the faithful
// scale shape.
func TestBigTreeCopyNo5xx(t *testing.T) {
	const width, depth = 100, 100 // 100 dirs x 100 files = 10k files + folders
	e := newEnv(t)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	ctx := context.Background()

	if err := e.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: shaOf("bulk"), Sha1: "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		Md5: "d41d8cd98f00b204e9800998ecf8427e", Size: 4, CreatedAt: "2026-08-29T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	// The folder marker's shared ledger row (the FK every folder row cites).
	if err := e.md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: metadata.FolderMarkerSHA, Size: 0, CreatedAt: "2026-08-29T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed folder marker: %v", err)
	}
	now := "2026-08-29T00:00:00Z"
	if err := e.md.Usage().PutNodeWithUsage(ctx, &metadata.Node{
		RepoKey: "src", Path: "root/", Sha256: metadata.FolderMarkerSHA, CreatedBy: "seeder", CreatedAt: now,
	}, now); err != nil {
		t.Fatalf("seed root: %v", err)
	}
	for d := 0; d < depth; d++ {
		dir := fmt.Sprintf("root/d%02d/", d)
		if err := e.md.Usage().PutNodeWithUsage(ctx, &metadata.Node{
			RepoKey: "src", Path: dir, Sha256: metadata.FolderMarkerSHA, CreatedBy: "seeder", CreatedAt: now,
		}, now); err != nil {
			t.Fatalf("seed dir: %v", err)
		}
		for f := 0; f < width; f++ {
			if err := e.md.Usage().PutNodeWithUsage(ctx, &metadata.Node{
				RepoKey: "src", Path: fmt.Sprintf("root/d%02d/f%03d.bin", d, f),
				Sha256: shaOf("bulk"), Size: 4, Mime: "application/octet-stream",
				CreatedBy: "seeder", CreatedAt: now,
			}, now); err != nil {
				t.Fatalf("seed node: %v", err)
			}
		}
	}

	// Dry run first: full validation chain, budget-checked (P95 proxy: the
	// single-run wall time on the developer/CI machine — two-tier ceiling
	// per dryRunBudget above).
	start := time.Now()
	dry := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "root", TargetRepo: "dst", TargetPath: "root", DryRun: true,
	})
	elapsed := time.Since(start)
	if dry.HTTPStatus != 200 || dry.Artifacts != width*depth {
		t.Fatalf("dry run wrong: %d artifacts (%s)", dry.Artifacts, msgText(dry))
	}
	if budget := dryRunBudget(); elapsed > budget { // FR-121 escape #1
		t.Fatalf("dry run over the 10k tree took %s (>%.0fs budget, race=%t)", elapsed, budget.Seconds(), raceEnabled)
	}
	t.Logf("dry run over %d nodes: %s", width*depth+depth+1, elapsed)

	// Live copy: zero 5xx, sampled checksum match.
	live := cmRun(t, e, admin(), repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "src", SrcPath: "root", TargetRepo: "dst", TargetPath: "root",
	})
	if live.HTTPStatus != 200 || live.Artifacts != width*depth {
		t.Fatalf("live copy wrong: %d artifacts, status %d (%s)", live.Artifacts, live.HTTPStatus, msgText(live))
	}
	for _, m := range live.Messages {
		if m.Status >= 500 {
			t.Fatalf("5xx message on the big-tree copy: %+v", m)
		}
	}
	for _, probe := range []string{"root/d00/f000.bin", "root/d50/f050.bin", "root/d99/f099.bin"} {
		src, dst := mustGet(t, e, "src", probe), mustGet(t, e, "dst", probe)
		if src.Sha256 != dst.Sha256 || src.Size != dst.Size {
			t.Fatalf("sample %s drifted", probe)
		}
	}
}
