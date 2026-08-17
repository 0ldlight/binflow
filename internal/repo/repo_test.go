package repo_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- Repository CRUD validation (AC 2) ----

// TestCreateRepoKeyValidation: the repo-key matrix — charset, length,
// reserved words (PRD FR-3-AC4, ADR-0008).
func TestCreateRepoKeyValidation(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want error
	}{
		{"minimal", "ab", nil},
		{"letters digits dashes", "my-repo-2", nil},
		{"all lowercase letters", "abcdef", nil},
		{"single char", "a", repo.ErrInvalidRepoKey},
		{"starts with digit", "1abc", repo.ErrInvalidRepoKey},
		{"starts with dash", "-abc", repo.ErrInvalidRepoKey},
		{"uppercase", "Bad_Key", repo.ErrInvalidRepoKey},
		{"special char", "bad!", repo.ErrInvalidRepoKey},
		{"underscore", "bad_key", repo.ErrInvalidRepoKey},
		{"dot", "bad.key", repo.ErrInvalidRepoKey},
		{"space", "bad key", repo.ErrInvalidRepoKey},
		{"too long", "a" + strings.Repeat("b", 63), repo.ErrInvalidRepoKey},
		{"max length ok", "a" + strings.Repeat("b", 62), nil},
		{"reserved api", "api", repo.ErrReservedRepoKey},
		{"reserved v2", "v2", repo.ErrReservedRepoKey},
		{"reserved embedded", "api-local", nil}, // only exact matches reserve
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: tt.key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
			})
			if tt.want == nil {
				if err != nil {
					t.Fatalf("CreateRepo(%q) unexpected error: %v", tt.key, err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("CreateRepo(%q) error = %v, want %v", tt.key, err, tt.want)
			}
		})
	}
}

// TestCreateRepoTypeValidation: rclass/packageType gating (AC 2).
func TestCreateRepoTypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		rclass      string
		packageType string
		want        error
	}{
		{"local generic", "local", "generic", nil},
		{"remote generic → M3", "remote", "generic", repo.ErrRepoTypeNotSupported},
		{"virtual generic → M3", "virtual", "generic", repo.ErrRepoTypeNotSupported},
		{"local docker → M3", "local", "docker", repo.ErrRepoTypeNotSupported},
		{"local maven → M3", "local", "maven", repo.ErrRepoTypeNotSupported},
		{"local npm → M3", "local", "npm", repo.ErrRepoTypeNotSupported},
		{"local pypi → M3", "local", "pypi", repo.ErrRepoTypeNotSupported},
		{"unknown rclass", "federated", "generic", repo.ErrInvalidRepoType},
		{"unknown package", "local", "conda", repo.ErrInvalidRepoType},
		{"empty rclass", "", "generic", repo.ErrInvalidRepoType},
		{"empty package", "local", "", repo.ErrInvalidRepoType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "generic-local", Type: tt.rclass, PackageType: tt.packageType,
			})
			if tt.want == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			// The M3 wording must reach the API surface (AC 2: "supported
			// from M3" semantics live in the message; httpapi maps the
			// sentinel to a 400-shaped response).
			if errors.Is(tt.want, repo.ErrRepoTypeNotSupported) && !strings.Contains(err.Error(), "M3") {
				t.Fatalf("error %q does not mention M3", err)
			}
		})
	}
}

// TestRepoCRUDLifecycle: create → duplicate → get → list → update → delete.
func TestRepoCRUDLifecycle(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	created, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Description: "first",
	})
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if created.Config != "{}" {
		t.Fatalf("default config = %q, want {}", created.Config)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("timestamps not set: %+v", created)
	}

	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); !errors.Is(err, repo.ErrRepoExists) {
		t.Fatalf("duplicate CreateRepo error = %v, want ErrRepoExists", err)
	}

	got, err := e.svc.GetRepo(ctx, admin(), "generic-local")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if got.Type != repo.TypeLocal || got.PackageType != repo.PackageGeneric || got.Description != "first" {
		t.Fatalf("GetRepo mismatch: %+v", got)
	}

	if _, err := e.svc.GetRepo(ctx, admin(), "nope"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("GetRepo(unknown) error = %v, want ErrRepoNotFound", err)
	}

	// Update: description/config only; immutability guards.
	updated, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Description: "second", Config: `{"x":1}`,
	})
	if err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	if updated.Description != "second" || updated.Config != `{"x":1}` {
		t.Fatalf("UpdateRepo result: %+v", updated)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeVirtual,
	}); !errors.Is(err, repo.ErrInvalidRepoType) {
		t.Fatalf("type flip error = %v, want ErrInvalidRepoType", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", PackageType: "docker",
	}); !errors.Is(err, repo.ErrInvalidRepoType) {
		t.Fatalf("package flip error = %v, want ErrInvalidRepoType", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: "ghost"}); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("UpdateRepo(unknown) error = %v, want ErrRepoNotFound", err)
	}

	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "second-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo(second): %v", err)
	}
	repos, err := e.svc.ListRepos(ctx, admin())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 2 || repos[0].RepoKey != "generic-local" || repos[1].RepoKey != "second-local" {
		t.Fatalf("ListRepos = %d rows, keys %+v", len(repos), repos)
	}

	if err := e.svc.DeleteRepo(ctx, admin(), "second-local", false); err != nil {
		t.Fatalf("DeleteRepo(empty): %v", err)
	}
	if _, err := e.svc.GetRepo(ctx, admin(), "second-local"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("deleted repo still visible: %v", err)
	}
	if err := e.svc.DeleteRepo(ctx, admin(), "second-local", false); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("DeleteRepo(unknown) error = %v, want ErrRepoNotFound", err)
	}
}

// TestRepoCRUDRequiresAdmin: non-admin and anonymous principals are locked
// out of the management plane (AC 3 M1 skeleton).
func TestRepoCRUDRequiresAdmin(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	calls := []struct {
		name string
		call func(p *repo.Principal) error
	}{
		{"create", func(p *repo.Principal) error {
			_, err := e.svc.CreateRepo(ctx, p, &metadata.Repo{RepoKey: "x-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric})
			return err
		}},
		{"delete", func(p *repo.Principal) error {
			return e.svc.DeleteRepo(ctx, p, "generic-local", true)
		}},
		{"get", func(p *repo.Principal) error {
			_, err := e.svc.GetRepo(ctx, p, "generic-local")
			return err
		}},
		{"list", func(p *repo.Principal) error {
			_, err := e.svc.ListRepos(ctx, p)
			return err
		}},
		{"update", func(p *repo.Principal) error {
			_, err := e.svc.UpdateRepo(ctx, p, &metadata.Repo{RepoKey: "generic-local", Description: "x"})
			return err
		}},
	}
	for _, tt := range calls {
		t.Run(tt.name+" / anonymous", func(t *testing.T) {
			if err := tt.call(nil); !errors.Is(err, repo.ErrUnauthorized) {
				t.Fatalf("error = %v, want ErrUnauthorized", err)
			}
		})
		t.Run(tt.name+" / non-admin", func(t *testing.T) {
			// Reads (get/list) only demand authentication and succeed for
			// any authenticated principal; the mutating calls demand admin.
			err := tt.call(alice())
			switch tt.name {
			case "get", "list":
				if err != nil {
					t.Fatalf("authenticated read error = %v", err)
				}
			default:
				if !errors.Is(err, repo.ErrForbidden) {
					t.Fatalf("error = %v, want ErrForbidden", err)
				}
			}
		})
	}
}

// ---- Content: Put/Get/List happy paths ----

// TestPutGetRoundtrip: upload → node metadata → download → checksums agree.
func TestPutGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	content := "hello binflow"
	n := put(t, e, admin(), "generic-local", "acme/artifact.bin", content)
	if n.Sha256 != shaOf(content) {
		t.Fatalf("node sha256 = %s, want %s", n.Sha256, shaOf(content))
	}
	if n.Size != int64(len(content)) {
		t.Fatalf("node size = %d, want %d", n.Size, len(content))
	}
	if n.CreatedBy != "admin" {
		t.Fatalf("node createdBy = %q", n.CreatedBy)
	}
	if n.CreatedAt == "" || n.UpdatedAt != n.CreatedAt {
		t.Fatalf("timestamps: created %q updated %q", n.CreatedAt, n.UpdatedAt)
	}

	// blob ledger has the row (blob-first ordering made it land).
	if _, err := e.md.Blobs().Get(ctx, n.Sha256); err != nil {
		t.Fatalf("blobs row missing: %v", err)
	}

	rc, got, err := e.svc.Get(ctx, admin(), "generic-local", "acme/artifact.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close() //nolint:errcheck
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(b) != content {
		t.Fatalf("body = %q, want %q", b, content)
	}
	if got.Sha256 != n.Sha256 {
		t.Fatalf("Get metadata mismatch: %+v", got)
	}
}

// TestGetFolderNode: folders return metadata plus ErrIsFolder, not a body.
func TestGetFolderNode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "acme/", "")

	rc, n, err := e.svc.Get(ctx, admin(), "generic-local", "acme/")
	if !errors.Is(err, repo.ErrIsFolder) {
		t.Fatalf("Get(folder) error = %v, want ErrIsFolder", err)
	}
	if rc != nil {
		_ = rc.Close()
		t.Fatalf("folder Get returned a body reader")
	}
	if n == nil || n.Path != "acme/" {
		t.Fatalf("folder metadata missing: %+v", n)
	}
}

// TestPutValidation: path and repo failures surface before any I/O.
func TestPutValidation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	tests := []struct {
		name    string
		repoKey string
		path    string
		p       *repo.Principal
		want    error
	}{
		{"anonymous", "generic-local", "a.bin", nil, repo.ErrUnauthorized},
		{"unknown repo", "ghost", "a.bin", admin(), repo.ErrRepoNotFound},
		{"empty path", "generic-local", "", admin(), repo.ErrInvalidPath},
		{"leading slash", "generic-local", "/a.bin", admin(), repo.ErrInvalidPath},
		{"dot segment", "generic-local", "a/../b.bin", admin(), repo.ErrInvalidPath},
		{"double dot", "generic-local", "../escape.bin", admin(), repo.ErrInvalidPath},
		{"single dot", "generic-local", "./a.bin", admin(), repo.ErrInvalidPath},
		{"double slash", "generic-local", "a//b.bin", admin(), repo.ErrInvalidPath},
		{"root", "generic-local", "/", admin(), repo.ErrInvalidPath},
		{"backslash", "generic-local", `a\b.bin`, admin(), repo.ErrInvalidPath},
		{"trailing dotdot", "generic-local", "a/b/..", admin(), repo.ErrInvalidPath},
		{"too long", "generic-local", strings.Repeat("a", 513), admin(), repo.ErrInvalidPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.svc.Put(ctx, tt.p, tt.repoKey, tt.path,
				strings.NewReader("x"), storage.BlobRef{}, "")
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

// TestPutChecksumMismatchPropagates: storage.ErrChecksumMismatch (and the
// whole storage sentinel family) travels through Put untouched.
func TestPutChecksumMismatchPropagates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	_, err := e.svc.Put(ctx, admin(), "generic-local", "a.bin",
		strings.NewReader("content"), storage.BlobRef{Sha256: shaOf("other")}, "")
	if !errors.Is(err, storage.ErrChecksumMismatch) {
		t.Fatalf("error = %v, want storage.ErrChecksumMismatch", err)
	}
	// No node, no session residue.
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "a.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("node visible after failed put: %v", err)
	}
}

// ---- local semantics (AC 3, repo-semantics section 3) ----

// TestPutIdempotentRedeploy: same declared sha256 on the same path skips
// BOTH the write and the overwrite (delete) permission checks and keeps
// created/createdBy (high-confidence spec row).
func TestPutIdempotentRedeploy(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	first := put(t, e, admin(), "generic-local", "ci-out/x.bin", "artifact-v1")

	// alice has NO grants at all — the spec's "无 deploy 权限也可完成".
	again, err := e.svc.Put(ctx, alice(), "generic-local", "ci-out/x.bin",
		strings.NewReader("artifact-v1"), storage.BlobRef{Sha256: first.Sha256}, "text/plain")
	if err != nil {
		t.Fatalf("idempotent retransmit by unprivileged user: %v", err)
	}
	if again.CreatedBy != first.CreatedBy || again.CreatedAt != first.CreatedAt {
		t.Fatalf("idempotent retransmit rewrote provenance: %+v vs %+v", again, first)
	}
	if again.Sha256 != first.Sha256 {
		t.Fatalf("checksum drifted: %s vs %s", again.Sha256, first.Sha256)
	}

	// Undeclared checksum is NOT idempotent: it is an overwrite attempt and
	// alice still lacks delete permission.
	_, err = e.svc.Put(ctx, alice(), "generic-local", "ci-out/x.bin",
		strings.NewReader("artifact-v1"), storage.BlobRef{}, "")
	if !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("undeclared retransmit error = %v, want ErrForbidden", err)
	}
	if !strings.Contains(err.Error(), "DELETE") {
		t.Fatalf("overwrite denial does not mention DELETE: %v", err)
	}
}

// TestPutOverwrite: different checksum = overwrite; admin succeeds,
// non-admin without delete permission fails; provenance is preserved while
// the modified-side fields move.
func TestPutOverwrite(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	first := put(t, e, admin(), "generic-local", "app/1.0/app.bin", "v1")

	// alice holds write but NOT delete: overwrite must fail.
	e.az.add("alice", repo.ActionWrite, "")
	_, err := e.svc.Put(ctx, alice(), "generic-local", "app/1.0/app.bin",
		strings.NewReader("v2"), storage.BlobRef{}, "")
	if !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("overwrite without delete error = %v, want ErrForbidden", err)
	}

	// alice gains delete: overwrite succeeds.
	e.az.add("alice", repo.ActionDelete, "")
	e.clk.Advance(time.Minute) // so UpdatedAt visibly moves
	second, err := e.svc.Put(ctx, alice(), "generic-local", "app/1.0/app.bin",
		strings.NewReader("v2"), storage.BlobRef{}, "application/x-new")
	if err != nil {
		t.Fatalf("overwrite with delete: %v", err)
	}
	if second.Sha256 != shaOf("v2") {
		t.Fatalf("overwritten sha256 = %s", second.Sha256)
	}
	if second.CreatedBy != first.CreatedBy || second.CreatedAt != first.CreatedAt {
		t.Fatalf("overwrite rewrote provenance: %+v vs %+v", second, first)
	}
	if second.UpdatedAt == first.UpdatedAt {
		t.Fatalf("UpdatedAt did not move: %q", second.UpdatedAt)
	}

	// The download returns the NEW content and the blob ledger keeps both
	// blobs (the old one unreferenced, awaiting GC).
	rc, _, err := e.svc.Get(ctx, admin(), "generic-local", "app/1.0/app.bin")
	if err != nil {
		t.Fatalf("Get after overwrite: %v", err)
	}
	defer rc.Close() //nolint:errcheck
	body, _ := io.ReadAll(rc)
	if string(body) != "v2" {
		t.Fatalf("body after overwrite = %q", body)
	}
	if _, err := e.md.Blobs().Get(ctx, first.Sha256); err != nil {
		t.Fatalf("old blob row vanished: %v", err)
	}
}

// TestPutFolderNode: trailing slash creates an empty marker node; a
// non-empty body is rejected.
func TestPutFolderNode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	n, err := e.svc.Put(ctx, admin(), "generic-local", "acme/", nil, storage.BlobRef{}, "")
	if err != nil {
		t.Fatalf("folder deploy: %v", err)
	}
	// Folder nodes reference the shared empty-marker blob, never real
	// content, and carry no size.
	if n.Sha256 != "0000000000000000000000000000000000000000000000000000000000000000" || n.Size != 0 {
		t.Fatalf("folder node shape unexpected: %+v", n)
	}
	// The marker blob is never exposed through the content read path.
	if _, _, err := e.st.Open(context.Background(), n.Sha256); !errors.Is(err, storage.ErrBlobNotFound) {
		t.Fatalf("folder marker unexpectedly openable: %v", err)
	}
	if _, err := e.svc.Put(ctx, admin(), "generic-local", "acme2/",
		strings.NewReader("nope"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrInvalidPath) {
		t.Fatalf("non-empty folder deploy error = %v, want ErrInvalidPath", err)
	}
	// Folder nodes are listed like any other node.
	got, err := e.md.Nodes().Get(ctx, "generic-local", "acme/")
	if err != nil {
		t.Fatalf("folder node row: %v", err)
	}
	if got.Path != "acme/" {
		t.Fatalf("folder row path = %q", got.Path)
	}
}

// ---- Delete semantics (AC 3, repo-semantics section 4) ----

// TestDeleteFileAndIdempotency: file delete removes the node (not the
// blob); a second delete is ErrNodeNotFound (idempotent 404 semantics).
func TestDeleteFileAndIdempotency(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	n := put(t, e, admin(), "generic-local", "a/b/c.bin", "content")

	if err := e.svc.Delete(ctx, admin(), "generic-local", "a/b/c.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "a/b/c.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("node still present: %v", err)
	}
	// The blob survives — deletion is a reference drop, GC owns the bytes.
	if _, err := e.st.Stat(ctx, n.Sha256); err != nil {
		t.Fatalf("blob removed by node delete: %v", err)
	}
	if err := e.svc.Delete(ctx, admin(), "generic-local", "a/b/c.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("second Delete error = %v, want ErrNodeNotFound", err)
	}
	if err := e.svc.Delete(ctx, admin(), "generic-local", "never/existed.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("Delete(unknown) error = %v, want ErrNodeNotFound", err)
	}
}

// TestDeletePrunesEmptyParents: leaf delete cascades upward through folder
// rows that became empty (repo-semantics section 4 prune row).
func TestDeletePrunesEmptyParents(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	put(t, e, admin(), "generic-local", "x/y/z/", "")                // folders x/, x/y/, x/y/z/
	put(t, e, admin(), "generic-local", "x/y/z/file.bin", "content") // the leaf

	if err := e.svc.Delete(ctx, admin(), "generic-local", "x/y/z/file.bin"); err != nil {
		t.Fatalf("Delete leaf: %v", err)
	}
	for _, p := range []string{"x/y/z/", "x/y/", "x/"} {
		if _, err := e.md.Nodes().Get(ctx, "generic-local", p); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("folder %q not pruned: %v", p, err)
		}
	}
	// A sibling anywhere in the chain stops the prune at that level. Here
	// the folder rows were never explicitly created (implicit directories),
	// so only the file nodes exist and the prune stops at p/q's surviving
	// child.
	put(t, e, admin(), "generic-local", "p/q/one.bin", "1")
	put(t, e, admin(), "generic-local", "p/q/r/two.bin", "2")
	if err := e.svc.Delete(ctx, admin(), "generic-local", "p/q/r/two.bin"); err != nil {
		t.Fatalf("Delete nested leaf: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "p/q/one.bin"); err != nil {
		t.Fatalf("sibling pruned: %v", err)
	}
}

// TestPruneKeepsFoldersWithLiveChildren: review B1 regression — an explicit
// folder row must survive when any child lives, in both the leaf-delete and
// subtree-delete forms. The pre-fix code queried with the trailing-slash
// spelling, whose "d//%" subtree arm matched nothing, so the "is it empty?"
// check was blind and folder rows with surviving children were deleted.
func TestPruneKeepsFoldersWithLiveChildren(t *testing.T) {
	ctx := context.Background()

	t.Run("leaf delete keeps folder with live sibling", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "generic-local")
		put(t, e, admin(), "generic-local", "d/", "") // explicit folder row
		put(t, e, admin(), "generic-local", "d/one.bin", "1")
		put(t, e, admin(), "generic-local", "d/two.bin", "2")

		if err := e.svc.Delete(ctx, admin(), "generic-local", "d/one.bin"); err != nil {
			t.Fatalf("Delete leaf: %v", err)
		}
		// The folder row AND the surviving child must both be present.
		if _, err := e.md.Nodes().Get(ctx, "generic-local", "d/"); err != nil {
			t.Fatalf("B1 regression: folder row lost while d/two.bin lives: %v", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "generic-local", "d/two.bin"); err != nil {
			t.Fatalf("live child lost: %v", err)
		}
		// Folder remains addressable as a folder.
		if _, n, err := e.svc.Get(ctx, admin(), "generic-local", "d/"); !errors.Is(err, repo.ErrIsFolder) || n == nil {
			t.Fatalf("folder Get after sibling delete: %v, %v", err, n)
		}
	})

	t.Run("subtree delete keeps shared parent folder", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "generic-local")
		put(t, e, admin(), "generic-local", "a/", "") // explicit shared parent
		put(t, e, admin(), "generic-local", "a/keep.bin", "k")
		put(t, e, admin(), "generic-local", "a/b/", "")
		put(t, e, admin(), "generic-local", "a/b/c.bin", "c")

		if err := e.svc.Delete(ctx, admin(), "generic-local", "a/b/"); err != nil {
			t.Fatalf("Delete subtree: %v", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "generic-local", "a/"); err != nil {
			t.Fatalf("B1 regression: shared parent folder lost: %v", err)
		}
		if _, err := e.md.Nodes().Get(ctx, "generic-local", "a/keep.bin"); err != nil {
			t.Fatalf("sibling file lost: %v", err)
		}
		for _, gone := range []string{"a/b/", "a/b/c.bin"} {
			if _, err := e.md.Nodes().Get(ctx, "generic-local", gone); !errors.Is(err, metadata.ErrNodeNotFound) {
				t.Fatalf("%q not deleted: %v", gone, err)
			}
		}
	})

	t.Run("fully empty chain still prunes to root", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRepo(t, e, "generic-local")
		put(t, e, admin(), "generic-local", "x/", "")
		put(t, e, admin(), "generic-local", "x/y/", "")
		put(t, e, admin(), "generic-local", "x/y/z.bin", "z")

		if err := e.svc.Delete(ctx, admin(), "generic-local", "x/y/z.bin"); err != nil {
			t.Fatalf("Delete leaf: %v", err)
		}
		for _, gone := range []string{"x/y/", "x/"} {
			if _, err := e.md.Nodes().Get(ctx, "generic-local", gone); !errors.Is(err, metadata.ErrNodeNotFound) {
				t.Fatalf("%q not pruned when truly empty: %v", gone, err)
			}
		}
	})
}

// TestListPrefixFormsEquivalent: review B2 regression — "d" and "d/" must
// return identical result sets (the folder row plus everything beneath).
func TestListPrefixFormsEquivalent(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "d/", "")
	put(t, e, admin(), "generic-local", "d/one.bin", "1")
	put(t, e, admin(), "generic-local", "d/sub/two.bin", "2")
	put(t, e, admin(), "generic-local", "dx/other.bin", "3") // prefix sibling

	a, err := e.svc.List(ctx, admin(), "generic-local", "d")
	if err != nil {
		t.Fatalf("List(d): %v", err)
	}
	b, err := e.svc.List(ctx, admin(), "generic-local", "d/")
	if err != nil {
		t.Fatalf("List(d/): %v", err)
	}
	if len(a) != 3 || len(b) != 3 {
		t.Fatalf("B2: List(d)=%d List(d/)=%d rows, want 3 each", len(a), len(b))
	}
	amap := map[string]bool{}
	for _, n := range a {
		amap[n.Path] = true
	}
	for _, n := range b {
		if !amap[n.Path] {
			t.Fatalf("B2: forms disagree on %q", n.Path)
		}
	}
	// Deep form equivalence too.
	c, err := e.svc.List(ctx, admin(), "generic-local", "d/sub")
	if err != nil || len(c) != 1 || c[0].Path != "d/sub/two.bin" {
		t.Fatalf("List(d/sub) = %+v, %v", c, err)
	}
	// Root is not a listable prefix.
	if _, err := e.svc.List(ctx, admin(), "generic-local", "/"); !errors.Is(err, repo.ErrInvalidPath) {
		t.Fatalf("List(/) error = %v, want ErrInvalidPath", err)
	}
}

// TestDeleteFolderSparesSameNamedFile: review M2 — a file "d" coexisting
// with a folder "d/" survives a directory delete.
func TestDeleteFolderSparesSameNamedFile(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "d/", "")    // folder row
	put(t, e, admin(), "generic-local", "d", "file") // same-named file row
	put(t, e, admin(), "generic-local", "d/one.bin", "1")

	if err := e.svc.Delete(ctx, admin(), "generic-local", "d/"); err != nil {
		t.Fatalf("Delete folder: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "d"); err != nil {
		t.Fatalf("M2: same-named file removed by folder delete: %v", err)
	}
	for _, gone := range []string{"d/", "d/one.bin"} {
		if _, err := e.md.Nodes().Get(ctx, "generic-local", gone); !errors.Is(err, metadata.ErrNodeNotFound) {
			t.Fatalf("%q not deleted: %v", gone, err)
		}
	}
}

// TestFolderDeployChecksWriteBeforeBody: review M3 — an unauthorized
// principal's folder deploy must fail before the body is consumed.
func TestFolderDeployChecksWriteBeforeBody(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	// alice has no write grant; the body would be read and discarded by the
	// old code. A failing reader proves the body is never touched.
	var bodyRead bool
	boom := errReader{onRead: func() { bodyRead = true }}
	if _, err := e.svc.Put(ctx, alice(), "generic-local", "d/", boom, storage.BlobRef{}, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("folder deploy error = %v, want ErrForbidden", err)
	}
	if bodyRead {
		t.Fatalf("M3: body consumed before the permission gate")
	}

	// With the grant, an empty body creates the folder; a non-empty body or
	// a failing reader is rejected.
	e.az.add("alice", repo.ActionWrite, "")
	if _, err := e.svc.Put(ctx, alice(), "generic-local", "d/", strings.NewReader(""), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("folder deploy with grant: %v", err)
	}
	if _, err := e.svc.Put(ctx, alice(), "generic-local", "d2/", strings.NewReader("x"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrInvalidPath) {
		t.Fatalf("non-empty folder body error = %v, want ErrInvalidPath", err)
	}
	if _, err := e.svc.Put(ctx, alice(), "generic-local", "d3/", errReader{err: errors.New("boom")}, storage.BlobRef{}, ""); !errors.Is(err, repo.ErrInvalidPath) {
		t.Fatalf("failing-reader folder body error = %v, want ErrInvalidPath", err)
	}
}

// errReader fails every Read, recording the call.
type errReader struct {
	err    error
	onRead func()
}

func (r errReader) Read([]byte) (int, error) {
	if r.onRead != nil {
		r.onRead()
	}
	if r.err != nil {
		return 0, r.err
	}
	return 0, errors.New("read error")
}

// TestDeleteFolderRecursive: trailing-slash delete removes the subtree.
func TestDeleteFolderRecursive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")

	put(t, e, admin(), "generic-local", "d/", "")
	put(t, e, admin(), "generic-local", "d/one.bin", "1")
	put(t, e, admin(), "generic-local", "d/sub/", "")
	put(t, e, admin(), "generic-local", "d/sub/two.bin", "2")
	put(t, e, admin(), "generic-local", "keeper.bin", "k")
	put(t, e, admin(), "generic-local", "dx/sibling.bin", "s") // prefix sibling, must survive

	if err := e.svc.Delete(ctx, admin(), "generic-local", "d/"); err != nil {
		t.Fatalf("Delete folder: %v", err)
	}
	nodes, err := e.svc.List(ctx, admin(), "generic-local", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	paths := map[string]bool{}
	for _, n := range nodes {
		paths[n.Path] = true
	}
	if len(paths) != 2 || !paths["keeper.bin"] || !paths["dx/sibling.bin"] {
		t.Fatalf("recursive delete residue: %v", paths)
	}
	// Deleting a folder that does not exist (no row, no subtree) is 404.
	if err := e.svc.Delete(ctx, admin(), "generic-local", "d/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("folder re-delete error = %v, want ErrNodeNotFound", err)
	}
}

// TestDeleteRequiresPermission: alice needs the delete grant.
func TestDeleteRequiresPermission(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "sec/a.bin", "x")

	if err := e.svc.Delete(ctx, nil, "generic-local", "sec/a.bin"); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous delete error = %v", err)
	}
	if err := e.svc.Delete(ctx, alice(), "generic-local", "sec/a.bin"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("alice delete error = %v, want ErrForbidden", err)
	}
	e.az.add("alice", repo.ActionDelete, "")
	if err := e.svc.Delete(ctx, alice(), "generic-local", "sec/a.bin"); err != nil {
		t.Fatalf("alice delete with grant: %v", err)
	}
}

// TestGetListPaths: unknown repo/node on read paths, prefix filtering.
func TestGetListPaths(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "a/one.bin", "1")
	put(t, e, admin(), "generic-local", "b/two.bin", "2")

	if _, _, err := e.svc.Get(ctx, admin(), "ghost", "a.bin"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("Get(ghost repo) error = %v", err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "generic-local", "nope.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("Get(nope) error = %v", err)
	}
	if _, err := e.svc.List(ctx, admin(), "ghost", ""); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("List(ghost) error = %v", err)
	}
	all, err := e.svc.List(ctx, admin(), "generic-local", "")
	if err != nil || len(all) != 2 {
		t.Fatalf("List all = %d, %v", len(all), err)
	}
	sub, err := e.svc.List(ctx, admin(), "generic-local", "a")
	if err != nil || len(sub) != 1 || sub[0].Path != "a/one.bin" {
		t.Fatalf("List a = %+v, %v", sub, err)
	}
	// Prefix "a" must not match "ab/…"-style paths (case-sensitive LIKE,
	// T-10 review B1 semantics).
	put(t, e, admin(), "generic-local", "abx/three.bin", "3")
	if got, _ := e.svc.List(ctx, admin(), "generic-local", "a"); len(got) != 1 {
		t.Fatalf("prefix overmatch: %+v", got)
	}
}

// ---- DeleteRepo branches (AC 3) ----

// TestDeleteRepoBranches: non-empty without the flag names deleteContent;
// with the flag everything goes; empty repo deletes cleanly.
func TestDeleteRepoBranches(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "a.bin", "x")

	err := e.svc.DeleteRepo(ctx, admin(), "generic-local", false)
	if !errors.Is(err, repo.ErrRepoNotEmpty) {
		t.Fatalf("DeleteRepo(non-empty, no flag) error = %v, want ErrRepoNotEmpty", err)
	}
	if !strings.Contains(err.Error(), "deleteContent") {
		t.Fatalf("error does not hint deleteContent: %v", err)
	}
	// The repo and the node survive.
	if _, err := e.svc.GetRepo(ctx, admin(), "generic-local"); err != nil {
		t.Fatalf("repo vanished after refused delete: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "generic-local", "a.bin"); err != nil {
		t.Fatalf("node vanished after refused delete: %v", err)
	}

	if err := e.svc.DeleteRepo(ctx, admin(), "generic-local", true); err != nil {
		t.Fatalf("DeleteRepo(deleteContent): %v", err)
	}
	if _, err := e.svc.GetRepo(ctx, admin(), "generic-local"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("repo still present: %v", err)
	}
	if nodes, _ := e.md.Nodes().ListByPrefix(ctx, "generic-local", ""); len(nodes) != 0 {
		t.Fatalf("nodes survived DeleteRepo(deleteContent): %d", len(nodes))
	}

	// Empty repository: plain delete works, no flag needed.
	mustCreateRepo(t, e, "empty-local")
	if err := e.svc.DeleteRepo(ctx, admin(), "empty-local", false); err != nil {
		t.Fatalf("DeleteRepo(empty): %v", err)
	}
}

// ---- Transaction-boundary tests (AC 1 & 4, the correctness core) ----

// TestPutMetadataFailureRollsBackNode: injecting a failure at each metadata
// write leaves NO node; the physical blob may remain (GC's grace period is
// the designed recovery path, ADR-0006).
func TestPutMetadataFailureRollsBackNode(t *testing.T) {
	tests := []struct {
		name   string
		failOp string
	}{
		{"node write fails", "nodes.put"},
		{"blob-row write fails", "blobs.put"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			dbDir := t.TempDir()

			// Seed a real store first: the schema and admin row must exist
			// before the decorator wraps it.
			base, err := metadata.Open(context.Background(), metadata.Options{
				Driver: "sqlite", Path: filepath.Join(dbDir, "seed.db"),
			})
			if err != nil {
				t.Fatalf("metadata.Open(seed): %v", err)
			}
			ctx := context.Background()
			if err := base.Repos().Create(ctx, &metadata.Repo{
				RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
				Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
			}); err != nil {
				t.Fatalf("seed repo: %v", err)
			}

			var injectFailed bool
			hooked := wrapHooks(base, func(op string) error {
				// Fail exactly once: the retry must succeed (transient
				// storage hiccup semantics, matching the crash-window model
				// where the next attempt re-runs everything).
				if op == tt.failOp && !injectFailed {
					injectFailed = true
					return fmt.Errorf("injected %s failure", op)
				}
				return nil
			})
			eng, err := storage.OpenEngine(dataDir, storage.Options{})
			if err != nil {
				t.Fatalf("OpenEngine: %v", err)
			}
			t.Cleanup(func() {
				_ = eng.Close()
				_ = base.Close()
			})
			svc := repo.NewWithClock(eng, hooked, nil, nil, timeUTC)

			if _, err := svc.Put(ctx, admin(), "generic-local", "acme/x.bin",
				strings.NewReader("payload"), storage.BlobRef{}, ""); err == nil {
				t.Fatalf("Put unexpectedly succeeded with %s failing", tt.failOp)
			}

			// The node must NOT exist.
			if _, err := base.Nodes().Get(ctx, "generic-local", "acme/x.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
				t.Fatalf("node visible after metadata failure: %v", err)
			}
			// The blob ledger state depends on the injection point; what is
			// invariant is that the blob file may exist (unreferenced, GC
			// reclaims it) and that re-uploading succeeds.
			if _, err := svc.Put(ctx, admin(), "generic-local", "acme/x.bin",
				strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
				t.Fatalf("retry after metadata failure: %v", err)
			}
			n, err := base.Nodes().Get(ctx, "generic-local", "acme/x.bin")
			if err != nil {
				t.Fatalf("node missing after retry: %v", err)
			}
			if n.Sha256 != shaOf("payload") {
				t.Fatalf("retry sha mismatch: %s", n.Sha256)
			}
		})
	}
}

// TestPutBlobFirstOrder: the blobs row must be visible before the node row
// is written (T-25 a-case ruling). The journaling decorator fails the node
// write on its first attempt and records the order of successful writes.
func TestPutBlobFirstOrder(t *testing.T) {
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()

	base, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dbDir, "seed.db")})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	if err := base.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	var hooked *hookStore
	var orderChecked bool
	hooked = wrapHooks(base, func(op string) error {
		if op == "nodes.put" && !orderChecked {
			orderChecked = true
			// The blob row must already be jouralled when the first node
			// write fires — that is the whole blob-first invariant.
			journal := hooked.entries()
			for _, op2 := range journal {
				if op2 == "blobs.put" {
					return nil // order was right; let the write through
				}
			}
			return fmt.Errorf("ORDER VIOLATION: nodes.put attempted before blobs.put; journal %v", journal)
		}
		return nil
	})

	eng, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	svc := repo.NewWithClock(eng, hooked, nil, nil, timeUTC)

	if _, err := svc.Put(ctx, admin(), "generic-local", "order.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	journal := hooked.entries()
	blobAt, nodeAt := -1, -1
	for i, op := range journal {
		switch op {
		case "blobs.put":
			blobAt = i
		case "nodes.put":
			if nodeAt == -1 {
				nodeAt = i
			}
		}
	}
	if blobAt == -1 || nodeAt == -1 || blobAt > nodeAt {
		t.Fatalf("write order journal = %v (blobAt=%d nodeAt=%d)", journal, blobAt, nodeAt)
	}
}

// TestPutPhysicalBlobBeforeMetadata: the blob file is on disk before any
// metadata write starts (architecture 3.3: reverse order is forbidden; a
// node must never point at a missing blob).
func TestPutPhysicalBlobBeforeMetadata(t *testing.T) {
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()

	base, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dbDir, "seed.db")})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = base.Close() })
	if err := base.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	want := shaOf("payload")
	hooked := wrapHooks(base, func(op string) error {
		if op != "blobs.put" {
			return nil
		}
		// The very first metadata write must find the blob file present.
		blobPath := filepath.Join(dataDir, "blobs", want[:2], want)
		if _, err := os.Stat(blobPath); err != nil {
			return fmt.Errorf("ORDER VIOLATION: blobs.put ran before the physical blob existed: %w", err)
		}
		return nil
	})
	eng, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	svc := repo.NewWithClock(eng, hooked, nil, nil, timeUTC)

	if _, err := svc.Put(ctx, admin(), "generic-local", "phys.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

// ---- Read-path authorization matrix ----

// TestReadAuthorizationMatrix: admin always; anonymous denied without an
// authorizer (fail-closed) and allowed with one that grants anonymous read.
func TestReadAuthorizationMatrix(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "a.bin", "x")

	// The harness's policyAuthz denies anonymous by default.
	if _, _, err := e.svc.Get(ctx, nil, "generic-local", "a.bin"); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous Get error = %v, want ErrUnauthorized", err)
	}
	if _, err := e.svc.List(ctx, nil, "generic-local", ""); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous List error = %v, want ErrUnauthorized", err)
	}
	if _, _, err := e.svc.Get(ctx, alice(), "generic-local", "a.bin"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("alice Get error = %v, want ErrForbidden", err)
	}
	// Grant read → alice reads.
	e.az.add("alice", repo.ActionRead, "")
	if _, _, err := e.svc.Get(ctx, alice(), "generic-local", "a.bin"); err != nil {
		t.Fatalf("alice Get with grant: %v", err)
	}
}

// TestNilAuthorizerFailsClosed: with no authorizer wired, only admins act.
func TestNilAuthorizerFailsClosed(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	eng, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dbDir, "binflow.db")})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close(); _ = md.Close() })
	svc := repo.New(eng, md, nil, nil)

	if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if _, err := svc.Put(ctx, alice(), "generic-local", "a.bin",
		strings.NewReader("x"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("Put with nil authorizer error = %v, want ErrForbidden", err)
	}
	if _, _, err := svc.Get(ctx, alice(), "generic-local", "a.bin"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("Get with nil authorizer error = %v, want ErrForbidden", err)
	}
}

// ---- Audit ----

// TestAuditBestEffort: audit failures never block the business operation.
func TestAuditBestEffort(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	e.au.fail = true

	if _, err := e.svc.Put(ctx, admin(), "generic-local", "a.bin",
		strings.NewReader("x"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("Put with failing audit: %v", err)
	}
	if err := e.svc.Delete(ctx, admin(), "generic-local", "a.bin"); err != nil {
		t.Fatalf("Delete with failing audit: %v", err)
	}
}

// TestAuditEvents: successful operations emit the expected actions.
func TestAuditEvents(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "generic-local")
	put(t, e, admin(), "generic-local", "a.bin", "x")
	if _, _, err := e.svc.Get(ctx, admin(), "generic-local", "a.bin"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := e.svc.Delete(ctx, admin(), "generic-local", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got := e.au.actions()
	want := []string{
		repo.AuditActionRepoCreate,
		repo.AuditActionDeploy,
		repo.AuditActionDownload,
		repo.AuditActionDelete,
	}
	if len(got) != len(want) {
		t.Fatalf("audit actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit actions = %v, want %v", got, want)
		}
	}
}

// ---- helpers ----

// timeUTC is the fixed clock for the injection tests.
func timeUTC() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) }
