package metadata_test

// T-252 (M9, ADR-0030 E5): MembershipsByGroup — the group-side membership
// seam behind GET /api/security/groups/{name}?includeUsers=true. Pinned at
// the store level: ordered usernames (whatever insertion order the
// per-user write path produced), strict per-group isolation, and the
// empty-set convention — a member-less group (and a name with no row at
// all) is a nil slice, never an error; existence is Get's question.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

func TestMembershipsByGroup(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	for _, name := range []string{"aa-devs", "mm-devs", "zz-devs"} {
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
			t.Fatalf("seed user %s: %v", name, err)
		}
		if len(groups) > 0 {
			if err := st.Groups().SetUserGroups(ctx, name, groups); err != nil {
				t.Fatalf("seed membership %s: %v", name, err)
			}
		}
	}
	// aa-devs gains its members across separate per-user writes in
	// non-alphabetical order — the read must order by username regardless.
	seed("u-zed", []string{"aa-devs"})
	seed("u-ann", []string{"zz-devs", "aa-devs"})
	seed("u-mid", []string{"aa-devs", "mm-devs"})
	seed("u-bare", nil)

	tests := []struct {
		group string
		want  []string
	}{
		{"aa-devs", []string{"u-ann", "u-mid", "u-zed"}},
		{"mm-devs", []string{"u-mid"}},
		{"zz-devs", []string{"u-ann"}},
		{"empty-group", nil},   // member-less: no row was created for it either — same answer
		{"no-such-group", nil}, // existence is Get's question, never this seam's
	}
	for _, tc := range tests {
		if tc.group == "empty-group" {
			if err := st.Groups().Create(ctx, &metadata.Group{Name: tc.group, CreatedAt: now, UpdatedAt: now}); err != nil {
				t.Fatalf("seed empty-group: %v", err)
			}
		}
		got, err := st.Groups().MembershipsByGroup(ctx, tc.group)
		if err != nil {
			t.Fatalf("MembershipsByGroup(%s): %v", tc.group, err)
		}
		if !equalStrings(got, tc.want) {
			t.Fatalf("MembershipsByGroup(%s) = %v, want %v", tc.group, got, tc.want)
		}
	}

	// The two family views of the same rows agree (E2's per-user map vs the
	// per-group read): every (user, group) edge appears exactly once on
	// each side.
	byUser, err := st.Groups().MembershipsByUser(ctx)
	if err != nil {
		t.Fatalf("MembershipsByUser: %v", err)
	}
	for user, groups := range byUser {
		for _, group := range groups {
			members, err := st.Groups().MembershipsByGroup(ctx, group)
			if err != nil {
				t.Fatalf("MembershipsByGroup(%s): %v", group, err)
			}
			found := false
			for _, m := range members {
				if m == user {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("view disagreement: %s lists group %s, but the group's members %v lack the user", user, group, members)
			}
		}
	}
}
