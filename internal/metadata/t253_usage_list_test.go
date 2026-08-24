package metadata_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-253/E1: Usage().List — the single-query aggregate behind
// GET /api/v1/storage/usage (ADR-0030 / architecture section 14.1 E1). The
// cases pin the LEFT JOIN semantics (a meterless repository reports zero
// instead of vanishing), the repo_key ordering, and the counts arm's folder
// exclusion (sentinel rows are directory markers, not artifacts).

// t253OpenStore opens a throwaway store and seeds one repo row helper.
func t253OpenStore(t *testing.T) (context.Context, metadata.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: filepath.Join(t.TempDir(), "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return ctx, st
}

func t253SeedRepo(ctx context.Context, t *testing.T, st metadata.Store, key, rclass, config, updatedAt string) {
	t.Helper()
	if err := st.Repos().Create(ctx, &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: "generic",
		Config: config, CreatedAt: updatedAt, UpdatedAt: updatedAt,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// t253SeedFile lands one file node through the metered path (the same
// writer the service rides), returning nothing: the assertions read the
// aggregate afterwards.
func t253SeedFile(ctx context.Context, t *testing.T, st metadata.Store, key, path, sha string, size int64, now string) {
	t.Helper()
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob %s: %v", sha, err)
	}
	if err := st.Usage().PutNodeWithUsage(ctx, &metadata.Node{
		RepoKey: key, Path: path, Sha256: sha, Size: size,
		CreatedAt: now, UpdatedAt: now,
	}, now); err != nil {
		t.Fatalf("seed node %s/%s: %v", key, path, err)
	}
}

func TestT253UsageListAggregate(t *testing.T) {
	ctx, st := t253OpenStore(t)
	const now = "2026-08-24T01:00:00Z"

	// Three shapes: a metered local repo carrying a quota, an untouched
	// local repo (no repo_usage row at all), and a remote row (unmetered by
	// design). Keys deliberately land out of order to pin the ordering.
	t253SeedRepo(ctx, t, st, "zeta-local", "local", `{"quotaBytes":512}`, "2026-08-24T02:00:00Z")
	t253SeedRepo(ctx, t, st, "alpha-empty", "local", `{}`, "2026-08-24T03:00:00Z")
	t253SeedRepo(ctx, t, st, "mid-remote", "remote", `{"url":"https://up.invalid"}`, "2026-08-24T04:00:00Z")
	t253SeedFile(ctx, t, st, "zeta-local", "a.bin", "aa", int64(100), now)
	t253SeedFile(ctx, t, st, "zeta-local", "d/b.bin", "bb", int64(28), now)

	rows, err := st.Usage().List(ctx, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []struct {
		key       string
		typ       string
		used      int64
		updatedAt string
	}{
		{"alpha-empty", "local", 0, "2026-08-24T03:00:00Z"},
		{"mid-remote", "remote", 0, "2026-08-24T04:00:00Z"},
		{"zeta-local", "local", 128, "2026-08-24T02:00:00Z"},
	}
	if len(rows) != len(want) {
		t.Fatalf("List returned %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		got := rows[i]
		if got.RepoKey != w.key || got.Type != w.typ || got.UsedBytes != w.used || got.UpdatedAt != w.updatedAt {
			t.Errorf("row %d = {key:%s type:%s used:%d updated:%s}, want {%s %s %d %s}",
				i, got.RepoKey, got.Type, got.UsedBytes, got.UpdatedAt, w.key, w.typ, w.used, w.updatedAt)
		}
		if got.NodeCount != 0 {
			t.Errorf("row %s: NodeCount = %d without includeCounts, want 0", got.RepoKey, got.NodeCount)
		}
	}

	// Per-repo Get agreement: the aggregate's UsedBytes is the same number
	// the single-repo endpoint serves (E1 AC1's data-side half).
	for _, w := range want {
		u, err := st.Usage().Get(ctx, w.key)
		if err != nil {
			t.Fatalf("Get(%s): %v", w.key, err)
		}
		if u.LogicalBytes != w.used {
			t.Errorf("Get(%s) = %d, aggregate said %d", w.key, u.LogicalBytes, w.used)
		}
	}
}

func TestT253UsageListCountsExcludesFolderRows(t *testing.T) {
	ctx, st := t253OpenStore(t)
	const now = "2026-08-24T01:00:00Z"
	t253SeedRepo(ctx, t, st, "cnt", "local", `{}`, now)

	// Two file nodes plus one folder sentinel row at "d/" — the marker
	// shape the service's materializeAncestors writes (sha256 = the shared
	// zero sentinel, size 0).
	t253SeedFile(ctx, t, st, "cnt", "a.bin", "aa", int64(7), now)
	t253SeedFile(ctx, t, st, "cnt", "d/b.bin", "bb", int64(9), now)
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: metadata.FolderMarkerSHA, Size: 0, CreatedAt: now}); err != nil {
		t.Fatalf("seed folder marker blob: %v", err)
	}
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "cnt", Path: "d/", Sha256: metadata.FolderMarkerSHA, Size: 0,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed folder row: %v", err)
	}

	rows, err := st.Usage().List(ctx, true)
	if err != nil {
		t.Fatalf("List(counts): %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List(counts) returned %d rows, want 1", len(rows))
	}
	if rows[0].NodeCount != 2 {
		t.Errorf("NodeCount = %d, want 2 (folder sentinel excluded)", rows[0].NodeCount)
	}
	if rows[0].UsedBytes != 16 {
		t.Errorf("UsedBytes = %d, want 16", rows[0].UsedBytes)
	}

	// The empty-repository shape: counts on, zero files — the row stays
	// present with count 0 (an empty repository is information, not
	// absence).
	t253SeedRepo(ctx, t, st, "aaa-empty", "local", `{}`, now)
	rows, err = st.Usage().List(ctx, true)
	if err != nil {
		t.Fatalf("List(counts) second: %v", err)
	}
	if len(rows) != 2 || rows[0].RepoKey != "aaa-empty" || rows[0].NodeCount != 0 {
		t.Fatalf("empty repo row missing or wrong: %+v", rows)
	}
}
