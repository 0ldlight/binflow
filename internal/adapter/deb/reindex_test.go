package deb

// T-354's copy-side reindex entry: the deb index is property-coordinate
// driven, so the entry runs the whole-repository recompute regardless of
// the candidate directory set — the copy-shape test below seeds the gap
// the exact way T-351's L13 saw it (a .deb with its coordinate properties
// landed in a target repository with NO index tree) and the Packages
// family materializes.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestReindexDirsWholeRepoRecompute: copy the coordinate-carrying .deb
// into a fresh repository through the REAL copy pipeline (properties ride
// along — the index engine's source of truth), leave the index tree absent
// (the T-351 L13 gap), then ReindexDirs materializes Packages (+ .gz) and
// Release under the property-named distribution.
func TestReindexDirsWholeRepoRecompute(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.seedRepo(t, "deb-local", repo.TypeLocal, `{}`)
	s.seedRepo(t, "deb-dst", repo.TypeLocal, `{}`)

	pkg := helloDeb("mypkg", "1.0", "amd64")
	srcPath := "pool/main/m/mypkg/mypkg_1.0_amd64.deb"
	status, body, _ := s.debPut(t, "/binflow/deb-local/"+srcPath, pkg, "stable", []string{"main"}, []string{"amd64"})
	if status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s), want 201", status, body)
	}
	s.waitIndex(t, "/binflow/deb-local/dists/stable/main/binary-amd64/Packages")

	// The copy: the real pipeline, no observer (the gap shape — the copy
	// lands rows and properties, the derived index never follows).
	cms, ok := s.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatalf("the stack's service lacks the CopyMoveService face")
	}
	res, err := cms.CopyOrMove(ctx, &repo.Principal{Name: "admin", Admin: true}, repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "deb-local", SrcPath: srcPath,
		TargetRepo: "deb-dst", TargetPath: srcPath,
	})
	if err != nil || res.HTTPStatus != http.StatusOK {
		t.Fatalf("copy = (err %v, status %d)", err, res.HTTPStatus)
	}
	if status, _, _ := s.get("/binflow/deb-dst/dists/stable/main/binary-amd64/Packages"); status != http.StatusNotFound {
		t.Fatalf("pre-reindex Packages = %d, want the T-351 gap shape (404)", status)
	}

	// The candidate dirs the observer would hand over are deliberately
	// ignored (property coordinates, not paths, name the distribution):
	// the entry recomputes the whole repository.
	h := New(s.svc, s.md.Repos(), s.md.Blobs(), s.md.NodeProps(), Options{})
	if err := h.ReindexDirs(ctx, repo.SystemPrincipal(), "deb-dst",
		[]string{"pool/main/m/mypkg/"}); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}

	packages := s.waitIndex(t, "/binflow/deb-dst/dists/stable/main/binary-amd64/Packages")
	for _, want := range []string{"Package: mypkg", "Version: 1.0", "Architecture: amd64", srcPath} {
		if !strings.Contains(packages, want) {
			t.Fatalf("Packages stanza missing %q:\n%s", want, packages)
		}
	}
	s.waitIndex(t, "/binflow/deb-dst/dists/stable/main/binary-amd64/Packages.gz")
	if rel := s.waitIndex(t, "/binflow/deb-dst/dists/stable/Release"); !strings.Contains(rel, "main") {
		t.Fatalf("Release does not name the component:\n%s", rel)
	}
}
