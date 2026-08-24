package metadata_test

// T-251 (M9, ADR-0030 E2/E4): the two store seams the users-domain endpoints
// ride. DeleteCascade is pinned at the store level because its ATOMICITY is
// the contract — the ACE strip and the users-row drop land or vanish
// together, and the missing-user path must leave an orphan user-typed ACE
// row untouched (zero side effects on the 404). MembershipsByUser is pinned
// for shape and ordering: one aggregated map, group names sorted within each
// user's set, absent entry for group-less users (the wire layer renders []).

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestUserDeleteCascade(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	seed := func(name string) {
		t.Helper()
		if err := st.Users().Create(ctx, &metadata.User{
			Username: name, PasswordHash: "x", Enabled: true,
			Role: string(metadata.RoleUser), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed("gone")
	seed("stays")

	// A target naming "gone" as a USER principal, plus one naming "stays".
	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{Name: "t", Repos: `["r"]`, CreatedAt: now, UpdatedAt: now},
		[]*metadata.PermissionPrincipal{
			{TargetName: "t", Principal: "gone", PrincipalType: "user", CanWrite: true},
			{TargetName: "t", Principal: "stays", PrincipalType: "user", CanRead: true},
		}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	// Group membership of "gone" — the FK cascade must take the row.
	if err := st.Groups().Create(ctx, &metadata.Group{Name: "devs", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := st.Groups().SetUserGroups(ctx, "gone", []string{"devs"}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	if err := st.Users().DeleteCascade(ctx, "gone"); err != nil {
		t.Fatalf("DeleteCascade: %v", err)
	}
	if _, err := st.Users().Get(ctx, "gone"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("gone row = %v, want ErrUserNotFound", err)
	}
	_, principals, err := st.Permissions().GetTarget(ctx, "t")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if len(principals) != 1 || principals[0].Principal != "stays" {
		t.Fatalf("principals after cascade = %+v, want only stays", principals)
	}
	if gs, err := st.Groups().GroupsOfUser(ctx, "gone"); err != nil || len(gs) != 0 {
		t.Fatalf("memberships after cascade = %v/%d, want none", err, len(gs))
	}
}

// TestUserDeleteCascadeMissingZeroSideEffects pins the 404 path's atomicity:
// an orphan user-typed ACE row naming a nonexistent account is NOT stripped
// when the delete answers ErrUserNotFound (the strip runs in the same
// transaction, which rolls back on the missing row).
func TestUserDeleteCascadeMissingZeroSideEffects(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{Name: "t", Repos: `["r"]`, CreatedAt: now, UpdatedAt: now},
		[]*metadata.PermissionPrincipal{
			{TargetName: "t", Principal: "ghost", PrincipalType: "user", CanRead: true},
		}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := st.Users().DeleteCascade(ctx, "ghost"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("DeleteCascade ghost = %v, want ErrUserNotFound", err)
	}
	_, principals, err := st.Permissions().GetTarget(ctx, "t")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if len(principals) != 1 {
		t.Fatalf("orphan ACE row was stripped on the 404 path: %+v", principals)
	}
}

// TestMembershipsByUser walks the aggregate: sorted per-user sets, multiple
// users sharing a group, and the absent-entry convention for group-less
// users.
func TestMembershipsByUser(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	for _, name := range []string{"aa-devs", "zz-devs"} {
		if err := st.Groups().Create(ctx, &metadata.Group{Name: name, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatalf("seed group %s: %v", name, err)
		}
	}
	seed := func(name string, groups []string) {
		t.Helper()
		if err := st.Users().Create(ctx, &metadata.User{
			Username: name, PasswordHash: "x", Enabled: true,
			Role: string(metadata.RoleUser), CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if len(groups) > 0 {
			if err := st.Groups().SetUserGroups(ctx, name, groups); err != nil {
				t.Fatalf("seed membership %s: %v", name, err)
			}
		}
	}
	// u-one joins in reverse declaration order — the map's sets must come
	// back ordered by group name regardless.
	seed("u-one", []string{"zz-devs", "aa-devs"})
	seed("u-two", []string{"aa-devs"})
	seed("u-bare", nil)

	got, err := st.Groups().MembershipsByUser(ctx)
	if err != nil {
		t.Fatalf("MembershipsByUser: %v", err)
	}
	want := map[string][]string{
		"u-one": {"aa-devs", "zz-devs"},
		"u-two": {"aa-devs"},
	}
	if len(got) != len(want) {
		t.Fatalf("map = %v, want exactly %d entries (group-less users absent)", got, len(want))
	}
	for name, groups := range want {
		if !equalStrings(got[name], groups) {
			t.Fatalf("map[%s] = %v, want %v", name, got[name], groups)
		}
	}
	if _, ok := got["u-bare"]; ok {
		t.Fatalf("group-less user carries a map entry: %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
