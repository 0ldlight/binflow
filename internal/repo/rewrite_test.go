package repo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The ADR-0042 narrow primitive's matrix (T-371): the plain re-home, the
// empty-source taxonomy, the dedup and conflict dispositions, the shape
// refusals, and the usage-counter invariance a pure metadata move implies.

// rewriteEnv is one seeded environment: a local repository plus a stored
// body addressed by its sha256 (landed through Put so the ledger row and
// the GC-hold bookkeeping both exist).
type rewriteEnv struct {
	*env
	repoKey string
}

func newRewriteEnv(t *testing.T) *rewriteEnv {
	t.Helper()
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "cn-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return &rewriteEnv{env: e, repoKey: "cn-local"}
}

// putFile lands one file through the public Put (the permission pair and
// the blob-first order included) and returns its sha256.
func (e *rewriteEnv) putFile(t *testing.T, path string, body []byte) string {
	t.Helper()
	n, err := e.svc.Put(context.Background(), admin(), e.repoKey, path,
		strings.NewReader(string(body)), storage.BlobRef{Sha256: shaOf(string(body))}, "application/octet-stream")
	if err != nil {
		t.Fatalf("put %s: %v", path, err)
	}
	return n.Sha256
}

// nodeMap snapshots path -> sha256 of every row under prefix ("" = the
// whole repository).
func (e *rewriteEnv) nodeMap(t *testing.T, prefix string) map[string]string {
	t.Helper()
	nodes, err := e.svc.List(context.Background(), admin(), e.repoKey, prefix)
	if err != nil {
		t.Fatalf("list %q: %v", prefix, err)
	}
	out := map[string]string{}
	for _, n := range nodes {
		out[n.Path] = n.Sha256
	}
	return out
}

// TestRewriteSubtreePrefixPlainMove: every row under the source re-homes
// with its sha256 unchanged, nothing stays under the source, the blob
// behind a moved row still streams, and the usage counter does not move (a
// pure metadata rewrite).
func TestRewriteSubtreePrefixPlainMove(t *testing.T) {
	e := newRewriteEnv(t)
	ctx := context.Background()
	sha := e.putFile(t, "myuser/hello/1.0/stable/0/package/abc/0/package/abc/conaninfo.txt", []byte("info"))
	e.putFile(t, "myuser/hello/1.0/stable/0/package/abc/0/package/abc/nested/deep.bin", []byte("deep"))

	e.putFile(t, "myuser/hello/1.0/stable/abc-root-marker.txt", []byte("kept"))
	usageBefore, err := e.svc.Usage(ctx, admin(), e.repoKey)
	if err != nil {
		t.Fatalf("usage before: %v", err)
	}

	// Drop the SETUP Puts' audit rows so the count below measures only the
	// rewrite's own (zero) output.
	e.au.mu.Lock()
	e.au.events = nil
	e.au.mu.Unlock()

	got, err := e.svc.RewriteSubtreePrefix(ctx, e.repoKey,
		"myuser/hello/1.0/stable/0/package/abc/0/package/abc/", "myuser/hello/1.0/stable/0/package/abc/0/")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	// moved: the two files plus the nested folder row; dedup: the source
	// folder row itself (its target — dstPrefix's own row — exists).
	if got.Moved != 3 || got.Deduped != 1 || len(got.Conflicts) != 0 {
		t.Errorf("stats = moved %d dedup %d conflicts %d, want 3/1/0", got.Moved, got.Deduped, len(got.Conflicts))
	}

	// The zero-side-effect contract: not one audit row for the whole
	// rewrite (ADR-0042 decision 3 — no per-node audit, no emission, no
	// observer; the fake records everything Append ever saw).
	if n := len(e.au.events); n != 0 {
		t.Errorf("audit rows appended by the rewrite = %d, want 0", n)
	}

	after := e.nodeMap(t, "myuser/hello/1.0/stable")
	if _, live := after["myuser/hello/1.0/stable/0/package/abc/0/package/abc/"]; live {
		t.Errorf("source folder row survived: %v", after)
	}
	if _, live := after["myuser/hello/1.0/stable/0/package/abc/0/package/"]; live {
		t.Errorf("emptied scaffolding folder survived (随迁删除): %v", after)
	}
	for path, want := range map[string]string{
		"myuser/hello/1.0/stable/0/package/abc/0/conaninfo.txt":   sha,
		"myuser/hello/1.0/stable/0/package/abc/0/nested/deep.bin": shaOf("deep"),
		"myuser/hello/1.0/stable/0/package/abc/0/nested/":         "",
	} {
		gotSha, ok := after[path]
		if !ok {
			t.Errorf("post-rewrite tree lacks %s: %v", path, after)
			continue
		}
		if want != "" && gotSha != want {
			t.Errorf("post-rewrite %s sha = %s, want %s", path, gotSha, want)
		}
	}

	// The moved row's blob still streams through the public read.
	rc, _, err := e.svc.Get(ctx, admin(), e.repoKey, "myuser/hello/1.0/stable/0/package/abc/0/conaninfo.txt")
	if err != nil {
		t.Fatalf("get moved node: %v", err)
	}
	defer rc.Close() //nolint:errcheck // test-local fd
	buf := make([]byte, 4)
	if _, err := rc.Read(buf); err != nil || string(buf) != "info" {
		t.Errorf("moved node body = %q (%v), want info", string(buf), err)
	}

	usageAfter, err := e.svc.Usage(ctx, admin(), e.repoKey)
	if err != nil {
		t.Fatalf("usage after: %v", err)
	}
	if usageAfter.UsedBytes != usageBefore.UsedBytes {
		t.Errorf("usage moved: before %d after %d (a pure metadata rewrite)", usageBefore.UsedBytes, usageAfter.UsedBytes)
	}
}

// TestRewriteSubtreePrefixDedupAndConflict: the two occupied-target arms.
// Same sha256 at the target → the source row drops, counted Deduped, the
// target row untouched. A DIFFERENT sha256 with the source newer → the
// source row wins the target, the pair reported with both digests and the
// loser's blob still present in the store.
func TestRewriteSubtreePrefixDedupAndConflict(t *testing.T) {
	t.Run("dedup same sha", func(t *testing.T) {
		e := newRewriteEnv(t)
		ctx := context.Background()
		body := []byte("same bytes")
		sha := e.putFile(t, "root/0/package/aa/0/file.txt", body)
		e.putFile(t, "root/0/package/aa/0/package/aa/file.txt", body)

		got, err := e.svc.RewriteSubtreePrefix(ctx, e.repoKey, "root/0/package/aa/0/package/aa/", "root/0/package/aa/0/")
		if err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		if got.Moved != 0 || got.Deduped != 2 || len(got.Conflicts) != 0 {
			t.Errorf("stats = moved %d dedup %d conflicts %d, want 0/2/0 (file + source folder row)", got.Moved, got.Deduped, len(got.Conflicts))
		}
		after := e.nodeMap(t, "root")
		if after["root/0/package/aa/0/file.txt"] != sha {
			t.Errorf("target row lost or changed: %v", after)
		}
		if _, live := after["root/0/package/aa/0/package/"]; live {
			t.Errorf("emptied scaffolding folder survived (随迁删除): %v", after)
		}
		if len(after) != 6 { // the five parent folder rows plus the file
			t.Errorf("post-dedup tree = %v, want the single target file and its folders", after)
		}
	})

	t.Run("conflict newer source wins", func(t *testing.T) {
		e := newRewriteEnv(t)
		ctx := context.Background()
		e.putFile(t, "root/0/package/bb/0/file.txt", []byte("old target"))
		// The system clock makes every later Put "newer" (the env's
		// controllable clock advances below).
		e.clk.Advance(time.Hour)
		srcSha := e.putFile(t, "root/0/package/bb/0/package/bb/file.txt", []byte("newer source"))

		got, err := e.svc.RewriteSubtreePrefix(ctx, e.repoKey, "root/0/package/bb/0/package/bb/", "root/0/package/bb/0/")
		if err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		if got.Moved != 0 || got.Deduped != 1 || len(got.Conflicts) != 1 {
			t.Fatalf("stats = moved %d dedup %d conflicts %d, want 0/1/1 (folder-row dedup + the file conflict)", got.Moved, got.Deduped, len(got.Conflicts))
		}
		c := got.Conflicts[0]
		if c.Path != "root/0/package/bb/0/file.txt" || c.Kept != srcSha {
			t.Errorf("conflict = %+v, want kept %s at the target", c, srcSha)
		}
		if c.Dropped == "" || c.Dropped == c.Kept {
			t.Errorf("conflict dropped sha = %q, want the loser's digest", c.Dropped)
		}
		if sha := e.nodeMap(t, "root")["root/0/package/bb/0/file.txt"]; sha != srcSha {
			t.Errorf("target row sha = %s, want the newer source's %s", sha, srcSha)
		}
		// The loser's content is still in the blob store (GC grace): the
		// ledger row answers.
		if _, err := e.md.Blobs().Get(ctx, c.Dropped); err != nil {
			t.Errorf("loser blob ledger row: %v (content must survive the conflict)", err)
		}
	})
}

// TestRewriteSubtreePrefixTaxonomy: the explicit error classes — an empty
// source is ErrNodeNotFound, a target inside the source is ErrInvalidPath,
// the same prefix is ErrInvalidPath, an unknown repository is
// ErrRepoNotFound and a virtual repository is ErrRepoTypeNotSupported.
func TestRewriteSubtreePrefixTaxonomy(t *testing.T) {
	e := newRewriteEnv(t)
	ctx := context.Background()
	e.putFile(t, "src/only.txt", []byte("x"))
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "v-agg", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}

	tests := []struct {
		name    string
		repoKey string
		src     string
		dst     string
		wantErr error
	}{
		{name: "empty source", repoKey: e.repoKey, src: "nothing/here/", dst: "elsewhere/",
			wantErr: repo.ErrNodeNotFound},
		{name: "dst inside src", repoKey: e.repoKey, src: "src/", dst: "src/deeper/",
			wantErr: repo.ErrInvalidPath},
		{name: "same prefix", repoKey: e.repoKey, src: "src/", dst: "src",
			wantErr: repo.ErrInvalidPath},
		{name: "empty prefix", repoKey: e.repoKey, src: "", dst: "dst/",
			wantErr: repo.ErrInvalidPath},
		{name: "unknown repo", repoKey: "no-such-repo", src: "a/", dst: "b/",
			wantErr: repo.ErrRepoNotFound},
		{name: "virtual repo", repoKey: "v-agg", src: "a/", dst: "b/",
			wantErr: repo.ErrRepoTypeNotSupported},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.svc.RewriteSubtreePrefix(ctx, tc.repoKey, tc.src, tc.dst)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("rewrite error = %v, want %v", err, tc.wantErr)
			}
		})
	}

	// A same-named FILE beside the source folder is spared (the directory
	// move never takes a file whose name lacks the slash).
	e.putFile(t, "side/file.txt", []byte("inside"))
	e.putFile(t, "side", []byte("beside"))
	if _, err := e.svc.RewriteSubtreePrefix(ctx, e.repoKey, "side/", "moved/"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	after := e.nodeMap(t, "")
	if _, ok := after["side"]; !ok {
		t.Errorf("same-named file was taken by the folder move: %v", after)
	}
	if _, ok := after["moved/file.txt"]; !ok {
		t.Errorf("folder content did not land: %v", after)
	}
}
