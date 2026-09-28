package repo_test

// T-530 (F1): the <K>-cache projection face at the service layer
// (remote-cache-projection.md sections 1.1/2.1/2.2): a remote's derived
// cache key is READ-addressable with local semantics — a standing copy
// serves byte-exact, a miss is the ordinary 404 with ZERO upstream contact
// (the pull-through machinery belongs to the remote key alone) — the gate
// evaluates on the PARENT key, List/ResolveMeta delegate, and no -cache
// key is ever a creatable or updatable entity (section 1.3).

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// buildProjectionEnv creates one remote with a counting upstream holding a
// single path, plus a local repo (for the not-a-remote-parent leg).
func buildProjectionEnv(t *testing.T) (*env, *virtualFixture) {
	t.Helper()
	e := newEnv(t)
	fx := &virtualFixture{
		e:         e,
		upstreams: map[string]*httptest.Server{},
		hits:      map[string]*atomic.Int64{},
	}
	srv, hits := countingUpstream(t, map[string]string{"/cached/it.bin": "up-bytes"})
	fx.upstreams["rem"] = srv
	fx.hits["rem"] = hits
	mustCreateRemote(t, e, "rem", remoteCfg(srv.URL, ""))
	mustCreateRepo(t, e, "loc")
	return e, fx
}

// TestCacheProjectionGetServesStandingCopy: after ONE pull-through on the
// remote key, the projection serves the copy byte-exact with zero further
// upstream traffic, and a path that was never fetched answers the plain
// 404 WITHOUT contacting the upstream (the miss never pulls).
func TestCacheProjectionGetServesStandingCopy(t *testing.T) {
	const path = "cached/it.bin"
	ctx := context.Background()
	e, fx := buildProjectionEnv(t)

	// Warm the remote through its own key (the only face that pulls).
	rc, _, err := e.svc.Get(ctx, admin(), "rem", path)
	if err != nil {
		t.Fatalf("warm remote Get: %v", err)
	}
	rc.Close() //nolint:errcheck // read-only fd
	warm := fx.hits["rem"].Load()

	// The projection serves the standing copy byte-exact.
	rc, node, err := e.svc.Get(ctx, admin(), "rem-cache", path)
	if err != nil {
		t.Fatalf("projection Get: %v", err)
	}
	body, rerr := io.ReadAll(rc)
	rc.Close() //nolint:errcheck // read-only fd
	if rerr != nil || string(body) != "up-bytes" {
		t.Fatalf("projection body = %q (%v), want the standing copy", string(body), rerr)
	}
	// The node row is the parent's honest row (the projection holds no
	// entities of its own — remote-cache-projection.md section 1.1).
	if node.RepoKey != "rem" || node.Path != path {
		t.Fatalf("projection node = %s/%s, want the parent's row", node.RepoKey, node.Path)
	}
	if got := fx.hits["rem"].Load(); got != warm {
		t.Fatalf("projection hit upstream = %d, want unchanged %d", got, warm)
	}

	// A never-fetched path: the plain 404, zero upstream (miss never pulls).
	if _, _, err := e.svc.Get(ctx, admin(), "rem-cache", "never/fetched.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("projection miss = %v, want ErrNodeNotFound", err)
	}
	if got := fx.hits["rem"].Load(); got != warm {
		t.Fatalf("projection miss hit upstream = %d, want unchanged %d (a miss never pulls)", got, warm)
	}
}

// TestCacheProjectionFolderAndUnknownFaces: the folder spellings answer
// ErrIsFolder off the parent's rows (children prove the folder), and keys
// whose parent is not a remote repository have NO projection — they fall
// back to the ordinary repository lookup and answer the repo-not-found.
func TestCacheProjectionFolderAndUnknownFaces(t *testing.T) {
	const path = "cached/it.bin"
	ctx := context.Background()
	e, _ := buildProjectionEnv(t)
	rc, _, err := e.svc.Get(ctx, admin(), "rem", path)
	if err != nil {
		t.Fatalf("warm remote Get: %v", err)
	}
	rc.Close() //nolint:errcheck // read-only fd

	// The folder holding the copy proves itself by its children.
	if _, _, err := e.svc.Get(ctx, admin(), "rem-cache", "cached/"); !errors.Is(err, repo.ErrIsFolder) {
		t.Fatalf("projection folder = %v, want ErrIsFolder", err)
	}
	// An empty folder spelling is the plain miss.
	if _, _, err := e.svc.Get(ctx, admin(), "rem-cache", "nope/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("projection empty folder = %v, want ErrNodeNotFound", err)
	}

	// A local repository's -cache spelling: no projection (parent not
	// remote) — the ordinary unknown-repository answer.
	if _, _, err := e.svc.Get(ctx, admin(), "loc-cache", path); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("local -cache Get = %v, want ErrRepoNotFound", err)
	}
	// A nonexistent parent: same ordinary answer.
	if _, _, err := e.svc.Get(ctx, admin(), "ghost-cache", path); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("ghost -cache Get = %v, want ErrRepoNotFound", err)
	}
}

// TestCacheProjectionGateOnParent: the read gate evaluates on the PARENT
// key (section 2.2's ACL mapping) — anonymous is challenged, an ungranted
// principal is denied, and the parent's grant lets the projection read
// through.
func TestCacheProjectionGateOnParent(t *testing.T) {
	const path = "cached/it.bin"
	ctx := context.Background()
	e, _ := buildProjectionEnv(t)
	rc, _, err := e.svc.Get(ctx, admin(), "rem", path)
	if err != nil {
		t.Fatalf("warm remote Get: %v", err)
	}
	rc.Close() //nolint:errcheck // read-only fd

	if _, _, err := e.svc.Get(ctx, nil, "rem-cache", path); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous projection Get = %v, want ErrUnauthorized", err)
	}
	if _, _, err := e.svc.Get(ctx, alice(), "rem-cache", path); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted projection Get = %v, want ErrForbidden", err)
	}
	// A grant on the PARENT opens the projection (the strip-the-suffix
	// mapping is the whole point) — and a grant on the projection key
	// ITSELF does nothing (there is no such permission target).
	e.az.addOnRepo("alice", repo.ActionRead, "rem", "")
	if _, _, err := e.svc.Get(ctx, alice(), "rem-cache", path); err != nil {
		t.Fatalf("parent-granted projection Get: %v", err)
	}
}

// TestCacheProjectionListDelegates: List on the projection key lists the
// parent remote's cached rows — same set as the remote's own listing.
func TestCacheProjectionListDelegates(t *testing.T) {
	const path = "cached/it.bin"
	ctx := context.Background()
	e, _ := buildProjectionEnv(t)
	rc, _, err := e.svc.Get(ctx, admin(), "rem", path)
	if err != nil {
		t.Fatalf("warm remote Get: %v", err)
	}
	rc.Close() //nolint:errcheck // read-only fd

	direct, err := e.svc.List(ctx, admin(), "rem", "")
	if err != nil {
		t.Fatalf("remote List: %v", err)
	}
	via, err := e.svc.List(ctx, admin(), "rem-cache", "")
	if err != nil {
		t.Fatalf("projection List: %v", err)
	}
	if len(via) != len(direct) {
		t.Fatalf("projection list = %d rows, remote list = %d rows", len(via), len(direct))
	}
	found := false
	for _, n := range via {
		if n.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("projection list %v lacks the cached row", via)
	}
}

// TestCacheProjectionKeyNotCreatable: section 1.3 — no repository class may
// claim a -cache-suffixed key, on create OR update (the update refuses
// BEFORE the existence lookup: a PUT to a never-existing -cache key is the
// config 400, not a 404).
func TestCacheProjectionKeyNotCreatable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "rem", `{"url":"http://127.0.0.1:1/m2"}`)

	for _, rclass := range []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual} {
		cfg := "{}"
		if rclass == repo.TypeRemote {
			cfg = `{"url":"http://127.0.0.1:1/m2"}`
		}
		_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "squat-cache", Type: rclass, PackageType: repo.PackageGeneric, Config: cfg,
		})
		if !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "'-cache' suffix") {
			t.Fatalf("%s create of -cache key = %v, want the config 400 naming the suffix", rclass, err)
		}
	}
	// The update arm, before existence: a never-existing key, and a REAL
	// remote renamed into the suffix (blocked before the rename applies).
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "never-was-cache", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}",
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("update of never-existing -cache key = %v, want ErrInvalidRepoConfig (not 404)", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "rem-cache", Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: `{"url":"http://127.0.0.1:1/m2"}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("update of rem-cache = %v, want ErrInvalidRepoConfig", err)
	}
	// The parent itself still updates fine (no suffix).
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "rem", Config: `{"url":"http://127.0.0.1:2/m2"}`,
	}); err != nil {
		t.Fatalf("parent update: %v", err)
	}
}
