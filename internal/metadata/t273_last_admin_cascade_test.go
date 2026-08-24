package metadata_test

// T-273 (M9 fix window): the last-admin census rides the DeleteCascade
// transaction as a guarded DELETE. These tests pin the STORE-level contract:
// the sentinel pair (ErrLastAdmin vs ErrUserNotFound), the zero-side-effects
// rollback of the refusal, the census spelling (only role='admin' counts,
// readonly_admin is not a surviving administrator), and the concurrent
// mutual-delete shape the guard exists for — two legs racing the last two
// admins cannot both land.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// seedT273 inserts one enabled local account with the given role spelling.
func seedT273(ctx context.Context, t *testing.T, st metadata.Store, name, role string) {
	t.Helper()
	now := metadata.Now()
	if err := st.Users().Create(ctx, &metadata.User{
		Username: name, PasswordHash: "x", Enabled: true,
		Role: role, IsAdmin: role == metadata.RoleAdmin,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

// adminRoleCount censuses the store directly: how many role='admin' rows
// exist. The store is the authority under test, so the probe must not go
// through the delete seam's own census helpers.
func adminRoleCount(ctx context.Context, t *testing.T, st metadata.Store) int {
	t.Helper()
	users, err := st.Users().List(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	n := 0
	for _, u := range users {
		if u.Role == metadata.RoleAdmin {
			n++
		}
	}
	return n
}

// TestDeleteCascadeLastAdminRefused pins the guarded DELETE's refusal: with
// the seeded admin demoted, the only admin-role account cannot be cascade
// deleted — and the refusal is side-effect free (the account row, and the
// user-typed ACE rows the strip would have taken, both survive the rollback).
func TestDeleteCascadeLastAdminRefused(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	if err := st.Users().SetRole(ctx, "admin", metadata.RoleUser); err != nil {
		t.Fatalf("demote seeded admin: %v", err)
	}
	seedT273(ctx, t, st, "solo", metadata.RoleAdmin)
	// An ACE row naming the solo admin — the strip must roll back with the
	// refused delete, not leak ahead of it.
	if err := st.Permissions().PutTarget(ctx,
		&metadata.PermissionTarget{Name: "t", Repos: `["r"]`, CreatedAt: now, UpdatedAt: now},
		[]*metadata.PermissionPrincipal{
			{TargetName: "t", Principal: "solo", PrincipalType: "user", CanRead: true},
		}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	if err := st.Users().DeleteCascade(ctx, "solo"); !errors.Is(err, metadata.ErrLastAdmin) {
		t.Fatalf("DeleteCascade solo admin = %v, want ErrLastAdmin", err)
	}
	if _, err := st.Users().Get(ctx, "solo"); err != nil {
		t.Fatalf("solo admin must survive the refused delete: %v", err)
	}
	_, principals, err := st.Permissions().GetTarget(ctx, "t")
	if err != nil {
		t.Fatalf("get target: %v", err)
	}
	if len(principals) != 1 || principals[0].Principal != "solo" {
		t.Fatalf("ACE row after refused delete = %+v, want the solo row intact", principals)
	}
}

// TestDeleteCascadeAdminWithPeerSucceeds pins the guard's other arm: an
// admin-role row whose deletion leaves another admin-role row behind IS
// deleted — the census predicate must not turn every admin delete into a
// refusal. A readonly_admin peer, by contrast, is NOT a surviving
// administrator (same census spelling as the service-level pre-check).
func TestDeleteCascadeAdminWithPeerSucceeds(t *testing.T) {
	ctx := context.Background()

	t.Run("admin peer present", func(t *testing.T) {
		st := open(t)
		seedT273(ctx, t, st, "adm-a", metadata.RoleAdmin)
		seedT273(ctx, t, st, "adm-b", metadata.RoleAdmin)
		if err := st.Users().DeleteCascade(ctx, "adm-a"); err != nil {
			t.Fatalf("DeleteCascade with a surviving admin peer: %v", err)
		}
		if _, err := st.Users().Get(ctx, "adm-a"); !errors.Is(err, metadata.ErrUserNotFound) {
			t.Fatalf("adm-a row = %v, want ErrUserNotFound", err)
		}
		if got := adminRoleCount(ctx, t, st); got != 2 { // seeded admin + adm-b
			t.Fatalf("admin-role rows after delete = %d, want 2", got)
		}
	})

	t.Run("readonly_admin peer does not count", func(t *testing.T) {
		st := open(t)
		if err := st.Users().SetRole(ctx, "admin", metadata.RoleUser); err != nil {
			t.Fatalf("demote seeded admin: %v", err)
		}
		seedT273(ctx, t, st, "solo", metadata.RoleAdmin)
		seedT273(ctx, t, st, "auditor", metadata.RoleReadOnlyAdmin)
		if err := st.Users().DeleteCascade(ctx, "solo"); !errors.Is(err, metadata.ErrLastAdmin) {
			t.Fatalf("DeleteCascade with only a readonly_admin peer = %v, want ErrLastAdmin", err)
		}
	})

	t.Run("plain user ignores the census entirely", func(t *testing.T) {
		st := open(t)
		seedT273(ctx, t, st, "plain", metadata.RoleUser)
		if err := st.Users().DeleteCascade(ctx, "plain"); err != nil {
			t.Fatalf("DeleteCascade plain user: %v", err)
		}
	})
}

// TestDeleteCascadeConcurrentMutualDeletes is the T-273 race window leg at
// the store level: the seeded admin demoted, exactly two admin-role accounts
// left, and two goroutines cascade-deleting each of them at once. Before the
// census moved into the transaction both legs could commit and leave zero
// admins. The guard must keep at least one admin alive on every iteration,
// and the two legs must never BOTH report success.
func TestDeleteCascadeConcurrentMutualDeletes(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Users().SetRole(ctx, "admin", metadata.RoleUser); err != nil {
		t.Fatalf("demote seeded admin: %v", err)
	}

	const iterations = 30
	for i := 0; i < iterations; i++ {
		for _, name := range []string{"race-a", "race-b"} {
			if _, err := st.Users().Get(ctx, name); errors.Is(err, metadata.ErrUserNotFound) {
				seedT273(ctx, t, st, name, metadata.RoleAdmin)
			} else if err != nil {
				t.Fatalf("probe %s: %v", name, err)
			}
		}

		start := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for leg, target := range []string{"race-a", "race-b"} {
			wg.Add(1)
			go func(leg int, target string) {
				defer wg.Done()
				<-start
				errs[leg] = st.Users().DeleteCascade(ctx, target)
			}(leg, target)
		}
		close(start)
		wg.Wait()

		for leg, err := range errs {
			switch {
			case err == nil, errors.Is(err, metadata.ErrLastAdmin), errors.Is(err, metadata.ErrUserNotFound):
			case metadata.IsStoreBusy(err): // transient contention: rolled back, fail-closed
			default:
				t.Fatalf("iteration %d leg %d: unsanctioned error: %v", i, leg, err)
			}
		}
		if errs[0] == nil && errs[1] == nil {
			t.Fatalf("iteration %d: both mutual admin deletes committed — the census guard did not fire", i)
		}
		if got := adminRoleCount(ctx, t, st); got < 1 {
			t.Fatalf("iteration %d: admin-role rows = %d, want >= 1 (instance stranded admin-less)", i, got)
		}
	}
}
