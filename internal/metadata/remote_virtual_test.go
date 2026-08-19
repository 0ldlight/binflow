package metadata_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func putTypedRepo(t *testing.T, st metadata.Store, key, repoType string) {
	t.Helper()
	now := metadata.Now()
	err := st.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repoType, PackageType: "maven",
		Description: "test", Config: "{}", CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("create %s repo %s: %v", repoType, key, err)
	}
}

func cacheEntry(repo, path, etag, expiresAt, kind string) *metadata.RemoteCacheEntry {
	return &metadata.RemoteCacheEntry{
		RepoKey: repo, Path: path, ETag: etag,
		LastModified: "Wed, 19 Aug 2026 00:00:00 GMT",
		FetchedAt:    "2026-08-19T00:00:00Z",
		ExpiresAt:    expiresAt, Kind: kind,
	}
}

func memberNames(ms []*metadata.VirtualMember) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.MemberRepo
	}
	return out
}

// T-62 AC ②: remote_configs CRUD round-trip, refresh and missing-row
// sentinels — table-driven over the four operations.
func TestRemoteConfigCRUD(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "maven-remote", "remote")

	cfg := &metadata.RemoteConfig{
		RepoKey: "maven-remote", URL: "https://repo.example.test/maven",
		Username: "ci", Password: "enc:v1:AAECAw==",
		ContentTTLSeconds: 7200, MetadataTTLSeconds: 300,
		AllowPrivateUpstream: true, BlockedOut: false,
	}
	if err := st.Remote().CreateConfig(ctx, cfg); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}
	got, err := st.Remote().GetConfig(ctx, "maven-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if *got != *cfg {
		t.Fatalf("config roundtrip mismatch:\n got %+v\nwant %+v", got, cfg)
	}

	// Update refreshes every non-key column.
	cfg.URL = "https://repo2.example.test/maven"
	cfg.ContentTTLSeconds = 86400
	cfg.BlockedOut = true
	cfg.AllowPrivateUpstream = false
	if err := st.Remote().UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	got, err = st.Remote().GetConfig(ctx, "maven-remote")
	if err != nil {
		t.Fatalf("GetConfig after update: %v", err)
	}
	if got.URL != cfg.URL || got.ContentTTLSeconds != 86400 || !got.BlockedOut || got.AllowPrivateUpstream {
		t.Fatalf("config update did not refresh: %+v", got)
	}

	// Missing-row sentinels for update/get/delete.
	if err := st.Remote().UpdateConfig(ctx, &metadata.RemoteConfig{RepoKey: "ghost"}); !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("UpdateConfig unknown repo err = %v, want ErrRemoteConfigNotFound", err)
	}
	if _, err := st.Remote().GetConfig(ctx, "ghost"); !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("GetConfig unknown repo err = %v, want ErrRemoteConfigNotFound", err)
	}
	if err := st.Remote().DeleteConfig(ctx, "ghost"); !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("DeleteConfig unknown repo err = %v, want ErrRemoteConfigNotFound", err)
	}

	if err := st.Remote().DeleteConfig(ctx, "maven-remote"); err != nil {
		t.Fatalf("DeleteConfig: %v", err)
	}
	if _, err := st.Remote().GetConfig(ctx, "maven-remote"); !errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("GetConfig after delete err = %v, want ErrRemoteConfigNotFound", err)
	}

	// FK: a config row needs its repository row.
	err = st.Remote().CreateConfig(ctx, &metadata.RemoteConfig{RepoKey: "ghost", URL: "https://x.test"})
	if err == nil {
		t.Fatal("CreateConfig for nonexistent repo must fail under foreign_keys=ON")
	}
	if errors.Is(err, metadata.ErrRemoteConfigNotFound) {
		t.Fatalf("unexpected sentinel in FK error: %v", err)
	}
}

// T-62 AC ②: remote_cache upsert/get/delete — re-fetch refreshes validators
// and clocks, single-path delete serves the RE-06 force-refresh move, and
// delete_by_repo scopes to one repository.
func TestRemoteCacheUpsertGetDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "maven-remote", "remote")
	putTypedRepo(t, st, "npm-remote", "remote")

	if err := st.Remote().PutCache(ctx, cacheEntry("maven-remote", "junit/junit/4.13/junit-4.13.jar",
		`"etag-1"`, "2026-08-19T02:00:00Z", metadata.RemoteCacheKindContent)); err != nil {
		t.Fatalf("PutCache: %v", err)
	}
	// Upsert on the same (repo, path): validators and clocks refresh, no conflict.
	if err := st.Remote().PutCache(ctx, cacheEntry("maven-remote", "junit/junit/4.13/junit-4.13.jar",
		`"etag-2"`, "2026-08-19T03:00:00Z", metadata.RemoteCacheKindMetadata)); err != nil {
		t.Fatalf("PutCache upsert: %v", err)
	}
	got, err := st.Remote().GetCache(ctx, "maven-remote", "junit/junit/4.13/junit-4.13.jar")
	if err != nil {
		t.Fatalf("GetCache: %v", err)
	}
	if got.ETag != `"etag-2"` || got.ExpiresAt != "2026-08-19T03:00:00Z" || got.Kind != metadata.RemoteCacheKindMetadata {
		t.Fatalf("cache upsert did not refresh: %+v", got)
	}
	// Same path under another repo is a different row (per repo+path keying).
	if err := st.Remote().PutCache(ctx, cacheEntry("npm-remote", "junit/junit/4.13/junit-4.13.jar",
		`"etag-n"`, "2026-08-19T04:00:00Z", metadata.RemoteCacheKindContent)); err != nil {
		t.Fatalf("PutCache other repo: %v", err)
	}

	if _, err := st.Remote().GetCache(ctx, "maven-remote", "missing/path"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("GetCache unknown path err = %v, want ErrRemoteCacheNotFound", err)
	}
	if err := st.Remote().DeleteCache(ctx, "maven-remote", "missing/path"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("DeleteCache unknown path err = %v, want ErrRemoteCacheNotFound", err)
	}

	// Single-path delete (RE-06): the next GET is a cache miss again.
	if err := st.Remote().DeleteCache(ctx, "maven-remote", "junit/junit/4.13/junit-4.13.jar"); err != nil {
		t.Fatalf("DeleteCache: %v", err)
	}
	if _, err := st.Remote().GetCache(ctx, "maven-remote", "junit/junit/4.13/junit-4.13.jar"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("GetCache after delete err = %v, want ErrRemoteCacheNotFound", err)
	}

	// delete_by_repo scopes to one repository and reports the row count.
	n, err := st.Remote().DeleteCacheByRepo(ctx, "maven-remote")
	if err != nil || n != 0 {
		t.Fatalf("DeleteCacheByRepo empty repo = (%d, %v), want (0, nil)", n, err)
	}
	for i := 0; i < 3; i++ {
		if err := st.Remote().PutCache(ctx, cacheEntry("maven-remote", fmt.Sprintf("p%d", i),
			`"e"`, "2026-08-19T05:00:00Z", metadata.RemoteCacheKindContent)); err != nil {
			t.Fatalf("PutCache p%d: %v", i, err)
		}
	}
	if n, err := st.Remote().DeleteCacheByRepo(ctx, "maven-remote"); err != nil || n != 3 {
		t.Fatalf("DeleteCacheByRepo = (%d, %v), want (3, nil)", n, err)
	}
	// The other repo's row survives.
	if _, err := st.Remote().GetCache(ctx, "npm-remote", "junit/junit/4.13/junit-4.13.jar"); err != nil {
		t.Fatalf("DeleteCacheByRepo leaked into another repo: %v", err)
	}
}

// T-62 AC ②: ListExpiredCache returns only entries past the threshold, in
// expires_at order, honoring the limit (the idx_remote_cache_expiry sweep
// candidates).
func TestRemoteCacheExpiryCandidates(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "maven-remote", "remote")

	entries := []struct {
		path, expiresAt string
	}{
		{"late", "2026-08-19T10:00:00Z"},
		{"early", "2026-08-19T01:00:00Z"},
		{"mid", "2026-08-19T05:00:00Z"},
		{"never", "9999-12-31T00:00:00Z"},
	}
	for _, e := range entries {
		if err := st.Remote().PutCache(ctx, cacheEntry("maven-remote", e.path, ` "e"`, e.expiresAt, metadata.RemoteCacheKindContent)); err != nil {
			t.Fatalf("PutCache %s: %v", e.path, err)
		}
	}
	now := "2026-08-19T06:00:00Z"

	got, err := st.Remote().ListExpiredCache(ctx, now, 0)
	if err != nil {
		t.Fatalf("ListExpiredCache: %v", err)
	}
	var paths []string
	for _, e := range got {
		paths = append(paths, e.Path)
	}
	// Only expired rows, expires_at ascending; the boundary row (== now) counts as expired.
	want := []string{"early", "mid"}
	if len(paths) != len(want) || paths[0] != want[0] || paths[1] != want[1] {
		t.Fatalf("expired candidates = %v, want %v", paths, want)
	}

	limited, err := st.Remote().ListExpiredCache(ctx, now, 1)
	if err != nil || len(limited) != 1 || limited[0].Path != "early" {
		t.Fatalf("limited candidates = %+v (err %v), want exactly [early]", limited, err)
	}
}

// T-62 AC ②: VirtualStore — atomic member replace, position sequence read,
// clearing, and the FK/cascades that keep the ledger consistent.
func TestVirtualMembersReplaceAndOrder(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "virt", "virtual")
	putTypedRepo(t, st, "libs-release", "local")
	putTypedRepo(t, st, "libs-remote", "remote")
	putTypedRepo(t, st, "team-release", "local")

	if err := st.Virtual().SetMembers(ctx, "virt", []string{"libs-release", "libs-remote", "team-release"}); err != nil {
		t.Fatalf("SetMembers: %v", err)
	}
	ms, err := st.Virtual().ListMembers(ctx, "virt")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if fmt.Sprint(memberNames(ms)) != fmt.Sprint([]string{"libs-release", "libs-remote", "team-release"}) {
		t.Fatalf("members out of declaration order: %v", memberNames(ms))
	}
	for i, m := range ms {
		if m.Position != int64(i) || m.VirtualRepo != "virt" {
			t.Fatalf("member %d = %+v, want position %d of virt", i, m, i)
		}
	}

	// Replace re-numbers from zero: dropping the middle member keeps a
	// dense 0..n-1 sequence (declaration order is the resolution order).
	if err := st.Virtual().SetMembers(ctx, "virt", []string{"team-release", "libs-remote"}); err != nil {
		t.Fatalf("SetMembers replace: %v", err)
	}
	ms, err = st.Virtual().ListMembers(ctx, "virt")
	if err != nil {
		t.Fatalf("ListMembers after replace: %v", err)
	}
	if len(ms) != 2 || ms[0].MemberRepo != "team-release" || ms[0].Position != 0 ||
		ms[1].MemberRepo != "libs-remote" || ms[1].Position != 1 {
		t.Fatalf("replace did not renumber: %+v", ms)
	}

	// Unknown or member-less virtual repo reads as empty, not an error.
	empty, err := st.Virtual().ListMembers(ctx, "no-such-virtual")
	if err != nil || len(empty) != 0 {
		t.Fatalf("ListMembers unknown repo = (%d rows, %v), want (0, nil)", len(empty), err)
	}

	// Duplicate members surface the primary key constraint as an error
	// (caller-side validation owns the 400).
	if err := st.Virtual().SetMembers(ctx, "virt", []string{"libs-release", "libs-release"}); err == nil {
		t.Fatal("SetMembers with duplicate member must fail on the primary key")
	}
	// The failed replace leaves the previous list intact (atomic).
	ms, err = st.Virtual().ListMembers(ctx, "virt")
	if err != nil || len(ms) != 2 || ms[0].MemberRepo != "team-release" {
		t.Fatalf("failed replace corrupted the member list: %+v (err %v)", ms, err)
	}

	// FK: members must exist.
	if err := st.Virtual().SetMembers(ctx, "virt", []string{"ghost"}); err == nil {
		t.Fatal("SetMembers with nonexistent member must fail under foreign_keys=ON")
	}

	// Clearing.
	if err := st.Virtual().SetMembers(ctx, "virt", nil); err != nil {
		t.Fatalf("SetMembers clear: %v", err)
	}
	if ms, err := st.Virtual().ListMembers(ctx, "virt"); err != nil || len(ms) != 0 {
		t.Fatalf("members after clear = %+v (err %v), want none", ms, err)
	}
}

// T-62 AC ②: repository deletion cascades both member edges (virtual_repo
// and member_repo FKs) — removing a member repo withdraws it from every
// virtual that listed it.
func TestVirtualMemberCascades(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "virt", "virtual")
	putTypedRepo(t, st, "libs-release", "local")
	putTypedRepo(t, st, "libs-remote", "remote")

	if err := st.Virtual().SetMembers(ctx, "virt", []string{"libs-release", "libs-remote"}); err != nil {
		t.Fatalf("SetMembers: %v", err)
	}
	if err := st.Repos().Delete(ctx, "libs-remote"); err != nil {
		t.Fatalf("delete member repo: %v", err)
	}
	ms, err := st.Virtual().ListMembers(ctx, "virt")
	if err != nil {
		t.Fatalf("ListMembers after member delete: %v", err)
	}
	if len(ms) != 1 || ms[0].MemberRepo != "libs-release" {
		t.Fatalf("member rows did not cascade with the member repo: %+v", ms)
	}

	if err := st.Repos().Delete(ctx, "virt"); err != nil {
		t.Fatalf("delete virtual repo: %v", err)
	}
	if ms, err := st.Virtual().ListMembers(ctx, "virt"); err != nil || len(ms) != 0 {
		t.Fatalf("member rows did not cascade with the virtual repo: %+v (err %v)", ms, err)
	}
}

// T-62 AC ②: deleting a repository cascades its remote_cache validator rows
// (the FK DeleteCacheByRepo backs up for the teardown path).
func TestRemoteCacheCascadeOnRepoDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "maven-remote", "remote")
	putTypedRepo(t, st, "npm-remote", "remote")

	for _, repo := range []string{"maven-remote", "npm-remote"} {
		if err := st.Remote().PutCache(ctx, cacheEntry(repo, "some/path", ` "e"`,
			"2026-08-19T05:00:00Z", metadata.RemoteCacheKindContent)); err != nil {
			t.Fatalf("PutCache %s: %v", repo, err)
		}
	}
	if err := st.Repos().Delete(ctx, "maven-remote"); err != nil {
		t.Fatalf("repo delete: %v", err)
	}
	if _, err := st.Remote().GetCache(ctx, "maven-remote", "some/path"); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("cache row survived repo delete, err = %v", err)
	}
	if _, err := st.Remote().GetCache(ctx, "npm-remote", "some/path"); err != nil {
		t.Fatalf("unrelated repo's cache row lost: %v", err)
	}
}

// T-62 AC ②: the new sub-stores wire up on the exported Store surface and
// survive a concurrent mixed workload under the race detector — cache
// upserts, member replaces and expiry scans on the same handle.
func TestRemoteVirtualConcurrentWorkload(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putTypedRepo(t, st, "maven-remote", "remote")
	putTypedRepo(t, st, "virt", "virtual")
	putTypedRepo(t, st, "m1", "local")
	putTypedRepo(t, st, "m2", "local")

	if st.Remote() == nil || st.Virtual() == nil {
		t.Fatal("Remote()/Virtual() returned nil")
	}

	const workers = 4
	const perWorker = 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker*2)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				path := fmt.Sprintf("g%d/artifact-%d.jar", w, i)
				if err := st.Remote().PutCache(ctx, cacheEntry("maven-remote", path,
					fmt.Sprintf(`"e%d"`, i), "2026-08-19T01:00:00Z", metadata.RemoteCacheKindContent)); err != nil {
					errs <- fmt.Errorf("cache put %s: %w", path, err)
					return
				}
				if _, err := st.Remote().GetCache(ctx, "maven-remote", path); err != nil {
					errs <- fmt.Errorf("cache get %s: %w", path, err)
					return
				}
			}
		}(w)
	}
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				members := []string{"m1", "m2"}
				if w == 1 {
					members = []string{"m2", "m1"}
				}
				if err := st.Virtual().SetMembers(ctx, "virt", members); err != nil {
					errs <- fmt.Errorf("set members: %w", err)
					return
				}
				ms, err := st.Virtual().ListMembers(ctx, "virt")
				if err != nil {
					errs <- fmt.Errorf("list members: %w", err)
					return
				}
				if len(ms) != 2 {
					errs <- fmt.Errorf("members = %d rows, want 2 (atomic replace)", len(ms))
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	// Every cache write landed exactly once.
	got, err := st.Remote().ListExpiredCache(ctx, "2026-08-19T02:00:00Z", 0)
	if err != nil {
		t.Fatalf("ListExpiredCache: %v", err)
	}
	if len(got) != workers*perWorker {
		t.Fatalf("expired candidates = %d, want %d", len(got), workers*perWorker)
	}
}
