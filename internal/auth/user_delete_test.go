package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// newDeleteService opens a throwaway sqlite store and wires the full service
// the way cmd does (NewFromStore), so the delete use case runs against the
// real cascade (FKs on, one transaction) instead of fakes.
func newDeleteService(t *testing.T) (*auth.Service, metadata.Store) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(dir, "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return auth.NewFromStore(st, false), st
}

// seedDeleteUser inserts one enabled local account with the given role.
func seedDeleteUser(ctx context.Context, t *testing.T, st metadata.Store, name, role string) {
	t.Helper()
	hash, err := auth.HashPassword("pw-" + name)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := metadata.Now()
	if err := st.Users().Create(ctx, &metadata.User{
		Username: name, PasswordHash: hash, Enabled: true,
		Role: role, IsAdmin: role == string(auth.RoleAdmin),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

// TestDeleteUserGuards walks the E4 guard matrix table-driven: every guard
// fires as its sentinel, in the documented order, and leaves the account
// untouched (the 400 path is side-effect free).
func TestDeleteUserGuards(t *testing.T) {
	ctx := context.Background()

	// Fixture: the seeded admin plus a second admin and a plain user.
	svc, st := newDeleteService(t)
	seedDeleteUser(ctx, t, st, "admin-two", string(auth.RoleAdmin))
	seedDeleteUser(ctx, t, st, "plain", string(auth.RoleUser))

	tests := []struct {
		name    string
		target  string
		actor   string
		wantErr error
	}{
		{"built-in admin is refused", "admin", "admin-two", auth.ErrDeleteBuiltIn},
		{"built-in admin is refused even to itself", "admin", "admin", auth.ErrDeleteBuiltIn},
		{"reserved anonymous name is refused", "anonymous", "admin-two", auth.ErrUserNotFound},
		{"self-delete is refused", "admin-two", "admin-two", auth.ErrDeleteSelf},
		{"plain self-delete is refused", "plain", "plain", auth.ErrDeleteSelf},
		{"unknown user is not-found", "ghost", "admin-two", auth.ErrUserNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.DeleteUser(ctx, tc.actor, tc.target)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("DeleteUser(%q from %q) error = %v, want %v", tc.target, tc.actor, err, tc.wantErr)
			}
			// Zero side effects: every real account survives the refusal.
			users, lerr := st.Users().List(ctx)
			if lerr != nil {
				t.Fatalf("list: %v", lerr)
			}
			if len(users) != 3 { // admin + admin-two + plain
				t.Fatalf("user count = %d after refused delete, want 3 (side effect leaked)", len(users))
			}
		})
	}
}

// TestDeleteUserLastAdmin pins the last-admin guard's exact census: with the
// built-in admin demoted, deleting the only remaining admin-role account is
// refused, while the same delete succeeds once a second admin exists.
func TestDeleteUserLastAdmin(t *testing.T) {
	ctx := context.Background()
	svc, st := newDeleteService(t)

	// Demote the built-in admin: solo-admin now belongs to admin-two.
	if err := st.Users().SetRole(ctx, "admin", string(auth.RoleUser)); err != nil {
		t.Fatalf("demote admin: %v", err)
	}
	seedDeleteUser(ctx, t, st, "admin-two", string(auth.RoleAdmin))

	if err := svc.DeleteUser(ctx, "admin", "admin-two"); !errors.Is(err, auth.ErrDeleteLastAdmin) {
		t.Fatalf("solo admin delete error = %v, want ErrDeleteLastAdmin", err)
	}
	if _, err := st.Users().Get(ctx, "admin-two"); err != nil {
		t.Fatalf("admin-two must survive the refused delete: %v", err)
	}

	// readonly_admin does NOT count toward the census — only the admin role
	// can manage the instance, so a readonly_admin "backup" saves nothing.
	seedDeleteUser(ctx, t, st, "auditor", string(auth.RoleReadOnlyAdmin))
	if err := svc.DeleteUser(ctx, "admin", "admin-two"); !errors.Is(err, auth.ErrDeleteLastAdmin) {
		t.Fatalf("solo admin delete with readonly_admin present error = %v, want ErrDeleteLastAdmin", err)
	}

	// A second real admin reopens the delete.
	seedDeleteUser(ctx, t, st, "admin-three", string(auth.RoleAdmin))
	if err := svc.DeleteUser(ctx, "admin", "admin-two"); err != nil {
		t.Fatalf("delete with a second admin present: %v", err)
	}
	if _, err := st.Users().Get(ctx, "admin-two"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("admin-two row = %v, want ErrUserNotFound", err)
	}
}

// TestDeleteUserCascade asserts the whole same-transaction cascade on the
// real store: the account row, its tokens, its web sessions and its group
// memberships vanish, and its user-typed permission_principals rows are
// stripped while a same-named GROUP principal survives (the strip keys on
// principal_type).
func TestDeleteUserCascade(t *testing.T) {
	ctx := context.Background()
	svc, st := newDeleteService(t)
	seedDeleteUser(ctx, t, st, "victim", string(auth.RoleUser))

	// A token the victim holds.
	tok, err := svc.Issue(ctx, "victim", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	// A web session the victim holds.
	if err := st.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: "deadbeef", Username: "victim",
		CreatedAt: metadata.Now(), ExpiresAt: metadata.Now(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	// Group membership.
	now := metadata.Now()
	if err := st.Groups().Create(ctx, &metadata.Group{Name: "devs", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := st.Groups().SetUserGroups(ctx, "victim", []string{"devs"}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
	// A permission target carrying the victim as a USER principal and a
	// group principal whose NAME coincides with the victim's username —
	// the strip must take the user row only.
	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{Name: "t", Repos: `["r"]`, CreatedAt: now, UpdatedAt: now},
		[]*metadata.PermissionPrincipal{
			{TargetName: "t", Principal: "victim", PrincipalType: "user", CanRead: true},
			{TargetName: "t", Principal: "victim", PrincipalType: "group", CanRead: true},
		}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := svc.DeleteUser(ctx, "admin", "victim"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := st.Users().Get(ctx, "victim"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("user row = %v, want ErrUserNotFound", err)
	}
	// Verify answers invalid credentials for the orphaned token (the row is
	// gone; owner lookup fails closed — ADR-0025 guardrail 3's seam).
	if _, err := svc.Verify(ctx, tok.AccessToken); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Verify after delete = %v, want ErrInvalidCredentials", err)
	}
	if toks, err := st.Tokens().ListByUsername(ctx, "victim"); err != nil || len(toks) != 0 {
		t.Fatalf("token rows after delete = %v/%d, want none", err, len(toks))
	}
	sess, err := st.WebSessions().GetBySHA256(ctx, "deadbeef")
	if !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("session row after delete = %v/%v, want ErrWebSessionNotFound", sess, err)
	}
	if groups, err := st.Groups().GroupsOfUser(ctx, "victim"); err != nil || len(groups) != 0 {
		t.Fatalf("memberships after delete = %v/%d, want none", err, len(groups))
	}
	_, principals, err := st.Permissions().GetTarget(ctx, "t")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if len(principals) != 1 || principals[0].PrincipalType != "group" {
		t.Fatalf("principals after delete = %+v, want only the group-typed row", principals)
	}
}

// TestDeleteUserUnwired pins the fail-closed posture of a service assembled
// without the delete seam (bare New — the shape unit fakes produce): the use
// case answers a typed error before touching any collaborator, instead of
// half-running guards against stores it cannot write to (or nil ones).
func TestDeleteUserUnwired(t *testing.T) {
	bare := auth.New(nil, nil, nil, true)
	if err := bare.DeleteUser(context.Background(), "admin", "plain"); err == nil {
		t.Fatal("unwired delete succeeded, want fail-closed error")
	}
}
