package repo_test

// T-530 (D-2): DELETE through a virtual addresses the virtual's OWN
// storage only (virtual-resolution.md section 7.5's errata + the L028
// live-measured reference behavior): members are never touched, an empty
// own namespace answers the not-found (BinFlow aggregates are computed per
// request, so the answer is the standing 404), and drift rows seeded under
// the virtual key — the only own-storage content this build can hold — DO
// drop (a folder spelling takes its subtree).

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedVirtualOwnRow writes one raw node row directly under the VIRTUAL's
// namespace (the drift shape adapter harnesses and hand-mangled seeds
// produce; the service's own writes never land there). The blob row goes
// first — nodes.sha256 carries the FK onto blobs.
func seedVirtualOwnRow(t *testing.T, e *env, vkey, path, sha string, size int64) {
	t.Helper()
	if err := e.md.Blobs().Put(context.Background(), &metadata.Blob{Sha256: sha, Size: size}); err != nil {
		t.Fatalf("seed blob %s: %v", sha, err)
	}
	if err := e.md.Nodes().Put(context.Background(), &metadata.Node{
		RepoKey: vkey, Path: path, Sha256: sha, Size: size, CreatedBy: "admin",
	}); err != nil {
		t.Fatalf("seed own-storage row %s/%s: %v", vkey, path, err)
	}
}

// TestVirtualDeleteOwnStorageChain is the D-2 service-level chain: miss ->
// member survives -> member direct delete -> virtual re-resolve miss.
func TestVirtualDeleteOwnStorageChain(t *testing.T) {
	const path = "com/acme/x/1.0.0/x-1.0.0.jar"
	ctx := context.Background()
	e := newEnv(t)
	buildVirtual(t, e, "virt", "loc-target", []memberSpec{{key: "loc-target"}})
	put(t, e, admin(), "loc-target", path, "content")

	// The own namespace is empty: the delete answers the not-found.
	if err := e.svc.Delete(ctx, admin(), "virt", path); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("virtual Delete on empty own storage = %v, want ErrNodeNotFound", err)
	}
	// The member's artifact is untouched.
	if _, err := e.md.Nodes().Get(ctx, "loc-target", path); err != nil {
		t.Fatalf("member node must survive the virtual delete: %v", err)
	}

	// The member direct delete still works (204 semantics at the wire).
	if err := e.svc.Delete(ctx, admin(), "loc-target", path); err != nil {
		t.Fatalf("member direct Delete: %v", err)
	}
	// And the virtual re-resolve is now a miss too.
	if _, _, err := e.svc.Get(ctx, admin(), "virt", path); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("post-delete virtual Get = %v, want ErrNodeNotFound", err)
	}
}

// TestVirtualDeleteDropsOwnStorageRows: drift rows under the virtual key
// ARE the own storage — a file row drops alone, a folder spelling takes its
// subtree (the local plane's directory rule), and a bare-folder spelling
// without its slash resolves through the slash-append arm.
func TestVirtualDeleteDropsOwnStorageRows(t *testing.T) {
	ctx := context.Background()
	const folderSHA = "0000000000000000000000000000000000000000000000000000000000000000"

	t.Run("file row", func(t *testing.T) {
		e := newEnv(t)
		buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}})
		seedVirtualOwnRow(t, e, "virt", "drift/a.bin", shaOf("drift"), 5)
		if err := e.svc.Delete(ctx, admin(), "virt", "drift/a.bin"); err != nil {
			t.Fatalf("own-storage file delete: %v", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "virt", "drift/a.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("row must be gone, got %v", err)
		}
	})

	t.Run("folder spelling takes the subtree", func(t *testing.T) {
		e := newEnv(t)
		buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}})
		seedVirtualOwnRow(t, e, "virt", "dir/", folderSHA, 0)
		seedVirtualOwnRow(t, e, "virt", "dir/inner.bin", shaOf("inner"), 5)
		seedVirtualOwnRow(t, e, "virt", "dir/sub/deep.bin", shaOf("deep"), 4)
		seedVirtualOwnRow(t, e, "virt", "sibling.bin", shaOf("sib"), 3)
		// Addressed WITHOUT the trailing slash — the slash-append arm.
		if err := e.svc.Delete(ctx, admin(), "virt", "dir"); err != nil {
			t.Fatalf("own-storage folder delete: %v", err)
		}
		for _, gone := range []string{"dir/", "dir/inner.bin", "dir/sub/deep.bin"} {
			if _, err := e.md.Nodes().Get(ctx, "virt", gone); !errors.Is(err, metadata.ErrNodeNotFound) {
				t.Fatalf("%s must be gone with the folder, got %v", gone, err)
			}
		}
		// The sibling outside the subtree survives.
		if _, err := e.md.Nodes().Get(ctx, "virt", "sibling.bin"); err != nil {
			t.Fatalf("sibling must survive: %v", err)
		}
		// The plain folder spelling (with the slash) drops the same way.
		seedVirtualOwnRow(t, e, "virt", "dir2/", folderSHA, 0)
		seedVirtualOwnRow(t, e, "virt", "dir2/x.bin", shaOf("x"), 1)
		if err := e.svc.Delete(ctx, admin(), "virt", "dir2/"); err != nil {
			t.Fatalf("own-storage folder delete (slashed): %v", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "virt", "dir2/x.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("slashed folder subtree must be gone, got %v", err)
		}
	})
}

// TestVirtualDeleteOwnStorageGate: the D-2 delete still walks the write
// gate on the VIRTUAL key — an ungranted principal never reaches the
// namespace walk.
func TestVirtualDeleteOwnStorageGate(t *testing.T) {
	e := newEnv(t)
	buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}})
	seedVirtualOwnRow(t, e, "virt", "drift/a.bin", shaOf("drift"), 5)
	if err := e.svc.Delete(context.Background(), alice(), "virt", "drift/a.bin"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted virtual Delete = %v, want ErrForbidden", err)
	}
	if _, err := e.md.Nodes().Get(context.Background(), "virt", "drift/a.bin"); err != nil {
		t.Fatalf("the row must survive the refused delete: %v", err)
	}
}
