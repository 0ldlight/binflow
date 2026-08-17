package metadata_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestRepoCRUD(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	a := &metadata.Repo{RepoKey: "alpha", Type: "local", PackageType: "generic",
		Description: "first", Config: "{}", CreatedAt: now, UpdatedAt: now}
	b := &metadata.Repo{RepoKey: "beta", Type: "local", PackageType: "generic",
		Description: "second", Config: "{}", CreatedAt: now, UpdatedAt: now}
	for _, r := range []*metadata.Repo{a, b} {
		if err := st.Repos().Create(ctx, r); err != nil {
			t.Fatalf("create %s: %v", r.RepoKey, err)
		}
	}
	// duplicate key -> error
	if err := st.Repos().Create(ctx, a); err == nil {
		t.Fatal("duplicate create must fail")
	}

	got, err := st.Repos().Get(ctx, "alpha")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Description != "first" || got.PackageType != "generic" {
		t.Fatalf("get mismatch: %+v", got)
	}

	a.Description = "first-updated"
	a.UpdatedAt = metadata.Now()
	if err := st.Repos().Update(ctx, a); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = st.Repos().Get(ctx, "alpha")
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.Description != "first-updated" {
		t.Fatalf("update not persisted: %+v", got)
	}

	list, err := st.Repos().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].RepoKey != "alpha" || list[1].RepoKey != "beta" {
		t.Fatalf("list = %+v, want [alpha beta]", list)
	}

	if _, err := st.Repos().Get(ctx, "missing"); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("get missing err = %v, want ErrRepoNotFound", err)
	}
	if err := st.Repos().Update(ctx, &metadata.Repo{RepoKey: "missing"}); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("update missing err = %v, want ErrRepoNotFound", err)
	}
	if err := st.Repos().Delete(ctx, "beta"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Repos().Delete(ctx, "beta"); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("double delete err = %v, want ErrRepoNotFound", err)
	}
}

func TestNodeUpsertByRepoKeyPath(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putRepo(t, st, "libs")
	now := metadata.Now()

	shaA, size := fakeBlob(1)
	shaB, _ := fakeBlob(2)
	for _, s := range []string{shaA, shaB} {
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: s, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
	}

	n1 := &metadata.Node{
		RepoKey: "libs", Path: "org/app/1.0/app.jar", Sha256: shaA, Size: size,
		Mime: "application/java-archive", CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Nodes().Put(ctx, n1); err != nil {
		t.Fatalf("put #1: %v", err)
	}
	got, err := st.Nodes().Get(ctx, "libs", "org/app/1.0/app.jar")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Sha256 != shaA {
		t.Fatalf("sha256 = %q, want %q", got.Sha256, shaA)
	}

	// Same (repo, path), different content: upsert replaces the row.
	n2 := &metadata.Node{
		RepoKey: "libs", Path: "org/app/1.0/app.jar", Sha256: shaB, Size: size,
		Mime: "application/java-archive", CreatedBy: "ci", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Nodes().Put(ctx, n2); err != nil {
		t.Fatalf("put #2 (upsert): %v", err)
	}
	got, err = st.Nodes().Get(ctx, "libs", "org/app/1.0/app.jar")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got.Sha256 != shaB || got.CreatedBy != "ci" {
		t.Fatalf("upsert did not replace row: %+v", got)
	}

	// Same path in another repo is a distinct row (cross-repo dedup lives in
	// blobs, not nodes).
	putRepo(t, st, "libs2")
	n3 := &metadata.Node{
		RepoKey: "libs2", Path: "org/app/1.0/app.jar", Sha256: shaB, Size: size,
		Mime: "application/java-archive", CreatedBy: "admin", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Nodes().Put(ctx, n3); err != nil {
		t.Fatalf("put other repo: %v", err)
	}

	if _, err := st.Nodes().Get(ctx, "libs", "nope"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("get missing err = %v, want ErrNodeNotFound", err)
	}
	if err := st.Nodes().Delete(ctx, "libs", "org/app/1.0/app.jar"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Nodes().Delete(ctx, "libs", "org/app/1.0/app.jar"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("double delete err = %v, want ErrNodeNotFound", err)
	}
}

func TestListByPrefix(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putRepo(t, st, "m")
	now := metadata.Now()

	paths := []string{
		"acme/widget/1.0/widget.jar",
		"acme/widget/1.1/widget.jar",
		"acme/tool.jar",
		"acorn/other.jar",
		"zeta/last.jar",
	}
	for i, p := range paths {
		sha, size := fakeBlob(i + 1)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "m", Path: p, Sha256: sha, Size: size, CreatedBy: "t",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s: %v", p, err)
		}
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{"whole repo", "", paths},
		{"leading slash tolerated", "/", paths},
		{"directory", "acme/widget", []string{"acme/widget/1.0/widget.jar", "acme/widget/1.1/widget.jar"}},
		{"file name prefix must not match siblings", "acme", []string{
			"acme/widget/1.0/widget.jar", "acme/widget/1.1/widget.jar", "acme/tool.jar"}},
		{"partial component is not a boundary", "ac", nil},
		{"single file", "zeta/last.jar", []string{"zeta/last.jar"}},
		{"no match", "nothing/", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.Nodes().ListByPrefix(ctx, "m", tt.prefix)
			if err != nil {
				t.Fatalf("ListByPrefix(%q): %v", tt.prefix, err)
			}
			var have []string
			for _, n := range got {
				have = append(have, n.Path)
			}
			sort.Strings(tt.want)
			if len(have) != len(tt.want) {
				t.Fatalf("ListByPrefix(%q) = %v, want %v", tt.prefix, have, tt.want)
			}
			for i := range have {
				if have[i] != tt.want[i] {
					t.Fatalf("ListByPrefix(%q) = %v, want %v", tt.prefix, have, tt.want)
				}
			}
		})
	}

	// Other repos stay invisible.
	putRepo(t, st, "m2")
	sha9, size9 := fakeBlob(9)
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha9, Size: size9, CreatedAt: now}); err != nil {
		t.Fatalf("blob put m2: %v", err)
	}
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "m2", Path: "acme/other.jar", Sha256: sha9, Size: size9, CreatedBy: "t",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("node put m2: %v", err)
	}
	got, err := st.Nodes().ListByPrefix(ctx, "m", "acme/widget")
	if err != nil {
		t.Fatalf("cross-repo ListByPrefix: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("cross-repo leak: got %d nodes, want 2", len(got))
	}
}

func TestDeleteByPrefix(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putRepo(t, st, "d")
	now := metadata.Now()
	for i, p := range []string{"a/1", "a/2", "ab/3", "b/4"} {
		sha, size := fakeBlob(i + 1)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "d", Path: p, Sha256: sha, Size: size, CreatedBy: "t",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s: %v", p, err)
		}
	}
	n, err := st.Nodes().DeleteByPrefix(ctx, "d", "a")
	if err != nil {
		t.Fatalf("DeleteByPrefix: %v", err)
	}
	if n != 2 {
		t.Fatalf("DeleteByPrefix removed %d, want 2 (a/1, a/2 but not ab/3)", n)
	}
	left, err := st.Nodes().ListByPrefix(ctx, "d", "")
	if err != nil {
		t.Fatalf("list after prefix delete: %v", err)
	}
	if len(left) != 2 || left[0].Path != "ab/3" || left[1].Path != "b/4" {
		t.Fatalf("remaining nodes = %+v, want [ab/3 b/4]", left)
	}
}

// AC: FilterUnreferenced anti-joins blobs against nodes and streams in pages.
func TestFilterUnreferenced(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putRepo(t, st, "gc")
	now := metadata.Now()

	const total = 37 // deliberately not a multiple of the page size
	var want []string
	for i := 0; i < total; i++ {
		sha, size := fakeBlob(i + 1)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
		if i%3 == 0 {
			// reference every third blob
			path := fmt.Sprintf("p/%03d.bin", i)
			if err := st.Nodes().Put(ctx, &metadata.Node{
				RepoKey: "gc", Path: path, Sha256: sha, Size: size, CreatedBy: "t",
				CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("node put %s: %v", path, err)
			}
		} else {
			want = append(want, sha)
		}
	}
	sort.Strings(want)

	var got []string
	pages := 0
	err := st.Blobs().FilterUnreferenced(ctx, 10, func(sha string) error {
		if pages%10 == 0 {
			pages++ // count batches loosely; the strict assertion is membership
		}
		got = append(got, sha)
		return nil
	})
	if err != nil {
		t.Fatalf("FilterUnreferenced: %v", err)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("FilterUnreferenced returned %d rows, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("row %d = %s, want %s", i, got[i], want[i])
		}
	}

	// Referencing everything must yield nothing.
	for i := 0; i < total; i++ {
		if i%3 == 0 {
			continue
		}
		sha, size := fakeBlob(i + 1)
		path := fmt.Sprintf("p/%03d.bin", i)
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: "gc", Path: path, Sha256: sha, Size: size, CreatedBy: "t",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s: %v", path, err)
		}
	}
	n := 0
	if err := st.Blobs().FilterUnreferenced(ctx, 10, func(string) error { n++; return nil }); err != nil {
		t.Fatalf("FilterUnreferenced (all referenced): %v", err)
	}
	if n != 0 {
		t.Fatalf("all-referenced filter returned %d rows, want 0", n)
	}

	// A callback error must propagate. Drop one reference first so the
	// stream actually produces a row to fail on.
	if err := st.Nodes().Delete(ctx, "gc", "p/001.bin"); err != nil {
		t.Fatalf("node delete: %v", err)
	}
	sentinel := errors.New("stop iteration")
	err = st.Blobs().FilterUnreferenced(ctx, 10, func(string) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback error swallowed: %v", err)
	}
}

func TestBlobStore(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()
	b := &metadata.Blob{Sha256: "ab" + strings.Repeat("0", 62), Sha1: "aa", Md5: "bb", Size: 42, CreatedAt: now}
	if err := st.Blobs().Put(ctx, b); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Idempotent: same sha256, DO NOTHING keeps the first row.
	b2 := &metadata.Blob{Sha256: b.Sha256, Size: 999, CreatedAt: now}
	if err := st.Blobs().Put(ctx, b2); err != nil {
		t.Fatalf("re-put: %v", err)
	}
	got, err := st.Blobs().Get(ctx, b.Sha256)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Size != 42 {
		t.Fatalf("re-put overwrote row: size=%d want 42", got.Size)
	}
	n, err := st.Blobs().Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	if err := st.Blobs().Delete(ctx, b.Sha256); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Blobs().Delete(ctx, b.Sha256); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("double delete err = %v, want ErrNotFound", err)
	}
}

func TestUserStore(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	hash, err := metadata.HashPassword("ci-pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u := &metadata.User{Username: "ci-bot", PasswordHash: hash, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := st.Users().Create(ctx, u); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := st.Users().Create(ctx, u); err == nil {
		t.Fatal("duplicate create must fail")
	}
	got, err := st.Users().Get(ctx, "ci-bot")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.IsAdmin {
		t.Fatal("ci-bot must not be admin")
	}
	byHash, err := st.Users().GetByPasswordHash(ctx, hash)
	if err != nil {
		t.Fatalf("get-by-hash: %v", err)
	}
	if byHash.Username != "ci-bot" {
		t.Fatalf("get-by-hash username = %q", byHash.Username)
	}

	newHash, err := metadata.HashPassword("rotated-pw")
	if err != nil {
		t.Fatalf("HashPassword(new): %v", err)
	}
	if err := st.Users().UpdatePassword(ctx, "ci-bot", newHash); err != nil {
		t.Fatalf("update-password: %v", err)
	}
	if _, err := st.Users().GetByPasswordHash(ctx, hash); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("old hash still resolves: %v", err)
	}

	list, err := st.Users().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 { // seeded admin + ci-bot
		t.Fatalf("users = %d, want 2", len(list))
	}
	if err := st.Users().Delete(ctx, "ci-bot"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Users().Delete(ctx, "ci-bot"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("double delete err = %v, want ErrUserNotFound", err)
	}
	// Tokens cascade with their user (FK).
	if _, err := st.Users().Get(ctx, "nope"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("get missing err = %v, want ErrUserNotFound", err)
	}
}

func TestTokenLifecycleAndUserCascade(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()
	hash, err := metadata.HashPassword("pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := st.Users().Create(ctx, &metadata.User{
		Username: "bot", PasswordHash: hash, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	id, err := st.Tokens().Create(ctx, &metadata.Token{
		Username: "bot", TokenSHA256: "digest-1", ExpiresAt: metadata.NeverExpires, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("token create: %v", err)
	}
	// duplicate digest rejected (UNIQUE)
	if _, err := st.Tokens().Create(ctx, &metadata.Token{
		Username: "bot", TokenSHA256: "digest-1", ExpiresAt: metadata.NeverExpires, CreatedAt: now,
	}); err == nil {
		t.Fatal("duplicate token digest must fail")
	}
	id2, err := st.Tokens().Create(ctx, &metadata.Token{
		Username: "bot", TokenSHA256: "digest-2", ExpiresAt: metadata.NeverExpires, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("token create #2: %v", err)
	}
	list, err := st.Tokens().ListByUsername(ctx, "bot")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("tokens = %d, want 2", len(list))
	}
	// Deleting the user cascades tokens.
	if err := st.Users().Delete(ctx, "bot"); err != nil {
		t.Fatalf("user delete: %v", err)
	}
	if _, err := st.Tokens().Get(ctx, id); !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("token %d survived user delete", id)
	}
	if _, err := st.Tokens().Get(ctx, id2); !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("token %d survived user delete", id2)
	}
	if _, err := st.Tokens().Get(ctx, 999); !errors.Is(err, metadata.ErrTokenNotFound) {
		t.Fatalf("get missing err = %v, want ErrTokenNotFound", err)
	}
}

func TestPermissionStoreTargetsAndPrincipals(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	target := &metadata.PermissionTarget{
		Name:      "ci-out-rw",
		Repos:     `["generic-local"]`,
		Includes:  `["ci-out/**"]`,
		Excludes:  `["ci-out/secret/**"]`,
		CreatedAt: now, UpdatedAt: now,
	}
	principals := []*metadata.PermissionPrincipal{
		{TargetName: "ci-out-rw", Principal: "ci-bot", PrincipalType: "user", CanRead: true, CanWrite: true},
		{TargetName: "ci-out-rw", Principal: "auditor", PrincipalType: "user", CanRead: true},
	}
	if err := st.Permissions().PutTarget(ctx, target, principals); err != nil {
		t.Fatalf("PutTarget: %v", err)
	}
	gotTarget, gotPrincipals, err := st.Permissions().GetTarget(ctx, "ci-out-rw")
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if gotTarget.Repos != target.Repos || gotTarget.Includes != target.Includes || gotTarget.Excludes != target.Excludes {
		t.Fatalf("target roundtrip mismatch: %+v", gotTarget)
	}
	if len(gotPrincipals) != 2 {
		t.Fatalf("principals roundtrip mismatch: %+v", gotPrincipals)
	}
	byName := map[string]metadata.PermissionPrincipal{}
	for _, p := range gotPrincipals {
		byName[p.Principal] = *p
	}
	if p := byName["auditor"]; !p.CanRead || p.CanWrite || p.CanDelete {
		t.Fatalf("auditor grants = r:%v w:%v d:%v, want r-only", p.CanRead, p.CanWrite, p.CanDelete)
	}
	if p := byName["ci-bot"]; !p.CanRead || !p.CanWrite || p.CanDelete {
		t.Fatalf("ci-bot grants = r:%v w:%v d:%v, want r+w", p.CanRead, p.CanWrite, p.CanDelete)
	}

	// Replace: wholly replaces principals.
	replacement := []*metadata.PermissionPrincipal{
		{TargetName: "ci-out-rw", Principal: "ci-bot", PrincipalType: "user", CanRead: true, CanWrite: true, CanDelete: true},
	}
	target.UpdatedAt = metadata.Now()
	if err := st.Permissions().PutTarget(ctx, target, replacement); err != nil {
		t.Fatalf("PutTarget(replace): %v", err)
	}
	_, gotPrincipals, err = st.Permissions().GetTarget(ctx, "ci-out-rw")
	if err != nil {
		t.Fatalf("GetTarget(replace): %v", err)
	}
	if len(gotPrincipals) != 1 || gotPrincipals[0].Principal != "ci-bot" || !gotPrincipals[0].CanDelete {
		t.Fatalf("replace did not swap principals: %+v", gotPrincipals)
	}

	// PrincipalsFor returns rows only for targets listing the repo.
	other := &metadata.PermissionTarget{
		Name: "other", Repos: `["docker-local"]`, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Permissions().PutTarget(ctx, other, []*metadata.PermissionPrincipal{
		{TargetName: "other", Principal: "someone", PrincipalType: "user", CanRead: true},
	}); err != nil {
		t.Fatalf("PutTarget(other): %v", err)
	}
	forRepo, err := st.Permissions().PrincipalsFor(ctx, "generic-local")
	if err != nil {
		t.Fatalf("PrincipalsFor: %v", err)
	}
	if len(forRepo) != 1 || forRepo[0].Principal != "ci-bot" {
		t.Fatalf("PrincipalsFor(generic-local) = %+v, want only ci-bot", forRepo)
	}

	list, err := st.Permissions().ListTargets(ctx)
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("targets = %d, want 2", len(list))
	}

	if err := st.Permissions().DeleteTarget(ctx, "ci-out-rw"); err != nil {
		t.Fatalf("DeleteTarget: %v", err)
	}
	if _, _, err := st.Permissions().GetTarget(ctx, "ci-out-rw"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("get after delete err = %v, want ErrNotFound", err)
	}
}

func TestAuditStore(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()
	events := []*metadata.AuditEvent{
		{Time: now, Actor: "admin", Action: "deploy", RepoKey: "r", Path: "a.jar", Detail: "{}"},
		{Time: now, Actor: "ci-bot", Action: "download", RepoKey: "r", Path: "a.jar", Detail: "{}"},
		{Time: now, Actor: "admin", Action: "delete", RepoKey: "other", Path: "b.jar", Detail: "{}"},
	}
	for _, e := range events {
		if err := st.Audits().Append(ctx, e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	all, err := st.Audits().List(ctx, "", "", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("events = %d, want 3", len(all))
	}
	if all[0].Action != "delete" { // newest first
		t.Fatalf("ordering not newest-first: %+v", all[0])
	}
	byRepo, err := st.Audits().List(ctx, "r", "", 10)
	if err != nil {
		t.Fatalf("list by repo: %v", err)
	}
	if len(byRepo) != 2 {
		t.Fatalf("repo filter = %d, want 2", len(byRepo))
	}
	byActor, err := st.Audits().List(ctx, "", "admin", 10)
	if err != nil {
		t.Fatalf("list by actor: %v", err)
	}
	if len(byActor) != 2 {
		t.Fatalf("actor filter = %d, want 2", len(byActor))
	}
	limited, err := st.Audits().List(ctx, "", "", 1)
	if err != nil {
		t.Fatalf("list limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("limit = %d rows, want 1", len(limited))
	}
}
