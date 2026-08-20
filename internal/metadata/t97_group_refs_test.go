package metadata_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// putTargetRows installs one target with arbitrary principal rows (the
// existing console_governance helpers cover user-only shapes; the reference
// query needs mixed types).
func putTargetRows(t *testing.T, st metadata.Store, name, principalType, principal string, r, w, d bool) {
	t.Helper()
	now := metadata.Now()
	err := st.Permissions().PutTarget(context.Background(),
		&metadata.PermissionTarget{
			Name: name, Repos: `["generic-local"]`, Includes: `["**"]`,
			CreatedAt: now, UpdatedAt: now,
		},
		[]*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: principalType,
			CanRead: r, CanWrite: w, CanDelete: d,
		}})
	if err != nil {
		t.Fatalf("put target %s: %v", name, err)
	}
}

// T-97 SE-04's delete guard: GroupReferences names every target carrying a
// group row for the group — and nothing else (user rows spelling the same
// name are irrelevant; unreferenced groups answer empty).
func TestGroupReferences(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	putGroup(t, st, "devs", "")
	putGroup(t, st, "lonely", "")
	putTargetRows(t, st, "devs-rw", "group", "devs", true, true, false)
	putTargetRows(t, st, "devs-read", "group", "devs", true, false, false)
	putTargetRows(t, st, "user-called-devs", "user", "devs", true, true, true)

	tests := []struct {
		group string
		want  []string
	}{
		{"devs", []string{"devs-read", "devs-rw"}}, // ordered by target name
		{"lonely", nil},
		{"missing", nil},
	}
	for _, tc := range tests {
		got, err := st.Permissions().GroupReferences(ctx, tc.group)
		if err != nil {
			t.Fatalf("GroupReferences(%q): %v", tc.group, err)
		}
		if len(got) != len(tc.want) {
			t.Fatalf("GroupReferences(%q) = %v, want %v", tc.group, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("GroupReferences(%q) = %v, want %v", tc.group, got, tc.want)
			}
		}
	}

	// Deleting the referencing target releases the guard.
	if err := st.Permissions().DeleteTarget(ctx, "devs-rw"); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	got, err := st.Permissions().GroupReferences(ctx, "devs")
	if err != nil {
		t.Fatalf("GroupReferences after delete: %v", err)
	}
	if len(got) != 1 || got[0] != "devs-read" {
		t.Fatalf("GroupReferences after delete = %v, want [devs-read]", got)
	}
}

// T-97 SE-05/06: UpdateProfile refreshes email + admin flag in one
// statement; unknown users answer ErrUserNotFound; the password hash and
// enabled flag keep their stored values.
func TestUpdateProfile(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	putUser(t, st, "jane", "jane@example.com")
	before, err := st.Users().Get(ctx, "jane")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := st.Users().UpdateProfile(ctx, "jane", "new@example.com", true); err != nil {
		t.Fatalf("update profile: %v", err)
	}
	after, err := st.Users().Get(ctx, "jane")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.Email != "new@example.com" || !after.IsAdmin {
		t.Fatalf("profile = email %q admin %v, want new@example.com true", after.Email, after.IsAdmin)
	}
	if after.PasswordHash != before.PasswordHash {
		t.Fatal("profile update must not touch the password hash")
	}
	if !after.Enabled {
		t.Fatal("profile update must not disable the account")
	}

	err = st.Users().UpdateProfile(ctx, "ghost", "x@example.com", false)
	if !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("unknown user err = %v, want ErrUserNotFound", err)
	}
}
