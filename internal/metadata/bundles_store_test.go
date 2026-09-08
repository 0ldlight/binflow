// The release-bundle table family's store faces (025, M17 T-513 /
// ADR-0046 decision 1 + Errata ①⑤): the pair identity, the
// insert-conflict sentinel (the tri-state's UNIQUE-key fact), the
// projections, the resume arm's atomic replace-with-state, the cascade
// chain, the CHECK-level state closed set, and the migration's reopen
// idempotency.

package metadata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func putBundleRow(t *testing.T, st metadata.Store, b *metadata.Bundle, items []*metadata.BundleItem) {
	t.Helper()
	if err := st.Bundles().InsertBundle(context.Background(), b, items); err != nil {
		t.Fatalf("bundles insert %s/%s: %v", b.Name, b.Version, err)
	}
}

func bundleItems(ids ...[2]string) []*metadata.BundleItem {
	out := make([]*metadata.BundleItem, 0, len(ids))
	for i, id := range ids {
		out = append(out, &metadata.BundleItem{
			ID: string(rune('a'+i)) + "-id", RepoKey: id[0], Path: id[1],
			AddedAt: stampA, AddedBy: "ci",
		})
	}
	return out
}

func TestBundlesStoreInsertGetRoundTrip(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	putBundleRow(t, st, &metadata.Bundle{
		Name: "rel-q3", Version: "1.0", State: "COMPLETE",
		Signature: "sha256:abc", CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA,
	}, bundleItems([2]string{"libs", "a.jar"}, [2]string{"libs", "b.jar"}))

	got, err := st.Bundles().GetBundle(ctx, "rel-q3", "1.0")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.State != "COMPLETE" || got.Signature != "sha256:abc" || got.CreatedBy != "ci" {
		t.Errorf("round-trip = %+v, want the stored columns", got)
	}

	items, err := st.Bundles().ListItems(ctx, "rel-q3", "1.0")
	if err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 2 || items[0].Path != "a.jar" || items[1].Path != "b.jar" {
		t.Fatalf("items = %+v, want the identity order (repo_key, path)", items)
	}

	// The pair is the identity: same name, different version is a
	// different row; a missing pair is ErrBundleNotFound.
	putBundleRow(t, st, &metadata.Bundle{
		Name: "rel-q3", Version: "2.0", State: "INPROGRESS",
		Signature: "sha256:def", CreatedBy: "ci", CreatedAt: stampB, UpdatedBy: "ci", UpdatedAt: stampB,
	}, nil)
	if _, err := st.Bundles().GetBundle(ctx, "rel-q3", "9.9"); !errors.Is(err, metadata.ErrBundleNotFound) {
		t.Errorf("get(missing version) = %v, want ErrBundleNotFound", err)
	}
}

func TestBundlesStoreInsertConflictSentinel(t *testing.T) {
	st := open(t)
	row := &metadata.Bundle{
		Name: "rel", Version: "1", State: "COMPLETE", Signature: "sha256:x",
		CreatedBy: "first", CreatedAt: stampA, UpdatedBy: "first", UpdatedAt: stampA,
	}
	putBundleRow(t, st, row, bundleItems([2]string{"libs", "a"}))

	// The second insert at the SAME pair answers ErrBundleExists — the
	// tri-state's UNIQUE-key fact (never an upsert, never a driver leak).
	dup := *row
	dup.CreatedBy = "second"
	if err := st.Bundles().InsertBundle(context.Background(), &dup, nil); !errors.Is(err, metadata.ErrBundleExists) {
		t.Fatalf("duplicate insert = %v, want ErrBundleExists", err)
	}
	// The winner is untouched.
	got, err := st.Bundles().GetBundle(context.Background(), "rel", "1")
	if err != nil || got.CreatedBy != "first" {
		t.Fatalf("row after refused insert = %+v (%v), want the original", got, err)
	}
}

func TestBundlesStoreProjections(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	for _, b := range []*metadata.Bundle{
		{Name: "rel-b", Version: "1", State: "COMPLETE", Signature: "s1",
			CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA},
		{Name: "rel-b", Version: "2", State: "INPROGRESS", Signature: "s2",
			CreatedBy: "ci", CreatedAt: stampB, UpdatedBy: "ci", UpdatedAt: stampB},
		{Name: "rel-a", Version: "1", State: "COMPLETE", Signature: "s3",
			CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA},
	} {
		putBundleRow(t, st, b, nil)
	}

	names, err := st.Bundles().ListBundleNames(ctx)
	if err != nil {
		t.Fatalf("names: %v", err)
	}
	if len(names) != 2 || names[0].Name != "rel-a" || names[1].Name != "rel-b" {
		t.Fatalf("names = %+v, want rel-a then rel-b (name order)", names)
	}
	if names[1].Versions != 2 || names[1].LastCreated != stampB {
		t.Fatalf("rel-b projection = %+v, want 2 versions and the newest created stamp", names[1])
	}

	versions, err := st.Bundles().ListBundleVersions(ctx, "rel-b")
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) != 2 || versions[0].Version != "2" || versions[0].State != "INPROGRESS" {
		t.Fatalf("versions = %+v, want 2 first (created_at DESC)", versions)
	}

	if rows, err := st.Bundles().ListBundleVersions(ctx, "no-such"); err != nil || len(rows) != 0 {
		t.Fatalf("versions(missing) = %v (%v), want empty", rows, err)
	}
}

func TestBundlesStoreReplaceItemsAtomic(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putBundleRow(t, st, &metadata.Bundle{
		Name: "rel", Version: "3", State: "INPROGRESS", Signature: "sig",
		CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA,
	}, bundleItems([2]string{"libs", "a"}, [2]string{"libs", "b"}))

	replacement := []*metadata.BundleItem{
		{ID: "x", RepoKey: "libs", Path: "a", Sha256: "sha-a", Size: 1, AddedAt: stampB, AddedBy: "ci"},
		{ID: "y", RepoKey: "libs", Path: "b", Sha256: "sha-b", Size: 2, AddedAt: stampB, AddedBy: "ci"},
	}
	if err := st.Bundles().ReplaceItems(ctx, "rel", "3", "COMPLETE", replacement, "resumer", stampB); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := st.Bundles().GetBundle(ctx, "rel", "3")
	if err != nil {
		t.Fatalf("get after replace: %v", err)
	}
	// The state and updated_* moved in the SAME transaction as the segment.
	if got.State != "COMPLETE" || got.UpdatedBy != "resumer" || got.UpdatedAt != stampB {
		t.Fatalf("row after replace = %+v, want the moved state/updated columns", got)
	}
	if got.CreatedBy != "ci" || got.CreatedAt != stampA {
		t.Errorf("creation bookkeeping moved: %+v, want the original", got)
	}
	items, err := st.Bundles().ListItems(ctx, "rel", "3")
	if err != nil {
		t.Fatalf("items after replace: %v", err)
	}
	if len(items) != 2 || items[0].Sha256 != "sha-a" || items[1].Sha256 != "sha-b" {
		t.Fatalf("segment after replace = %+v, want the replacement whole", items)
	}

	// A missing pair answers ErrBundleNotFound (the FK's caller mapping).
	if err := st.Bundles().ReplaceItems(ctx, "rel", "9", "COMPLETE", nil, "x", stampB); !errors.Is(err, metadata.ErrBundleNotFound) {
		t.Fatalf("replace(missing pair) = %v, want ErrBundleNotFound", err)
	}
}

func TestBundlesStoreStateCheckClosedSet(t *testing.T) {
	st := open(t)
	// The CHECK is the DB-level closed set: a third state spelling must
	// not land even when a caller bypasses the service's validation.
	err := st.Bundles().InsertBundle(context.Background(), &metadata.Bundle{
		Name: "rel", Version: "4", State: "CLOSE_INPROGRESS", Signature: "s",
		CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA,
	}, nil)
	if err == nil || errors.Is(err, metadata.ErrBundleExists) {
		t.Fatalf("insert with out-of-set state = %v, want the CHECK refusal", err)
	}
}

func TestBundlesStoreReopenIdempotent(t *testing.T) {
	path := t.TempDir() + "/reopen.db"
	ctx := context.Background()

	st1, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	putBundleRow(t, st1, &metadata.Bundle{
		Name: "rel", Version: "5", State: "COMPLETE", Signature: "sig",
		CreatedBy: "ci", CreatedAt: stampA, UpdatedBy: "ci", UpdatedAt: stampA,
	}, bundleItems([2]string{"libs", "a"}))
	if err := st1.Close(); err != nil {
		t.Fatalf("close 1: %v", err)
	}

	// The reopen replays 025 under CREATE IF NOT EXISTS — no error, and
	// the rows survive (the t212 rewind convention).
	st2, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("open 2 (migration replay): %v", err)
	}
	defer func() { _ = st2.Close() }()
	got, err := st2.Bundles().GetBundle(ctx, "rel", "5")
	if err != nil || got.State != "COMPLETE" {
		t.Fatalf("row after reopen = %+v (%v), want the survivor", got, err)
	}
}
