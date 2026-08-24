// User-delete use case of the auth service (M9, ADR-0030 E4 / architecture
// section 14.1): the guards plus the same-transaction cascade behind
// DELETE /api/security/users/{name}. The wire wordings live in the HTTP
// layer; this file owns the DECISIONS and types them as sentinels so the
// handler can render the exact spec text without re-deriving it.
//
// Guard set (all 400 at the wire, BinFlow's own hardening — Artifactory's
// REST face has no visible guard code there, gap-endpoints section 2.3):
//
//   - built-in "admin" (and the synthetic "anonymous" principal's name, for
//     hand-built rows naming it): the seed account is the ultimate recovery
//     path — its password can be re-seeded through BINFLOW_ADMIN_PASSWORD
//     only while the row exists;
//   - the last admin-role account: deleting it would leave the instance
//     without any administrator (the group face's mirror of this guard is
//     rbac-model section 1.2.2; users mirror its wording and 400);
//   - the current authenticated principal (self-delete): offboarding has an
//     established, reversible, audited path — disabling (T-208) — and a
//     mid-request self-revocation leaves the caller's session dead mid-flight.

package auth

import (
	"context"
	"errors"
	"fmt"
)

// Delete guard sentinels (E4). Every one of them is a 400 at the wire; the
// HTTP layer maps them onto the spec's plain-text wordings.
var (
	// ErrDeleteBuiltIn marks an attempt to delete a built-in principal (the
	// seeded "admin" account, and the reserved "anonymous" name should a
	// hand-built row carry it — the real anonymous principal is synthetic
	// and has no row, so the guard is belt-and-braces there).
	ErrDeleteBuiltIn = errors.New("auth: the built-in admin user cannot be deleted")
	// ErrDeleteSelf marks the authenticated principal naming itself as the
	// delete target.
	ErrDeleteSelf = errors.New("auth: the current authenticated user cannot be deleted")
	// ErrDeleteLastAdmin marks a delete that would leave the instance with
	// no admin-role account at all.
	ErrDeleteLastAdmin = errors.New("auth: deleting the last admin-role user is refused")
)

// builtinUndeletable names the principals no delete may ever touch. "admin"
// is the seeded account (store.go seedAdmin; BINFLOW_ADMIN_PASSWORD re-seeds
// only while the row exists). "anonymous" is the synthetic unauthenticated
// principal — it has no users row on a healthy instance, so DELETE answers
// 404 there anyway; the guard only fires on hand-built rows.
var builtinUndeletable = map[string]bool{
	"admin":     true,
	"anonymous": true,
}

// userDeleteSource backs DeleteUser: the admin-role census for the
// last-admin guard and the same-transaction cascade delete. Wired by
// NewFromStore over the metadata stores; nil on services built with bare New
// (unit fakes), for which DeleteUser fails closed.
type userDeleteSource interface {
	// AdminRoleUsers returns the usernames of every account whose stored
	// role is admin (metadata.RoleAdmin), ordered by name.
	AdminRoleUsers(ctx context.Context) ([]string, error)
	// DeleteCascade strips the user's permission_principals ACE rows and
	// deletes the account (FKs cascade user_groups/tokens/web_sessions) in
	// one transaction. ErrUserNotFound when the account does not exist,
	// with zero side effects.
	DeleteCascade(ctx context.Context, username string) error
}

// errUserDeleteUnwired marks a service assembled without the delete seam
// (bare New, unit fakes): the use case fails closed instead of half-running
// its guards against a store it cannot write to.
var errUserDeleteUnwired = errors.New("auth: user deletion is not wired on this service")

// DeleteUser removes one local account with the full E4 guard chain and the
// same-transaction cascade. actor is the authenticated principal issuing the
// call (the self-delete guard); username the target. Errors:
//
//	ErrUserNotFound     -> 404 (existence probe first, zero side effects)
//	ErrDeleteBuiltIn    -> 400 (seeded "admin" / reserved "anonymous")
//	ErrDeleteLastAdmin  -> 400 (target is the only admin-role account)
//	ErrDeleteSelf       -> 400 (actor names itself)
//
// The last-admin census is read-then-delete: a concurrent delete of the
// OTHER admin in the window between census and cascade can still strand the
// instance admin-less. BinFlow's single-instance, single-writer deployment
// shape (architecture section 9) makes that window theoretical; the census
// running inside the guard chain (not inside the store transaction) keeps
// the store seam single-purpose.
func (s *Service) DeleteUser(ctx context.Context, actor, username string) error {
	if s.userDelete == nil {
		// Fail closed before touching any collaborator: a bare-New service
		// has no users store either, so the lookup below would panic there.
		return fmt.Errorf("auth: delete %q: %w", username, errUserDeleteUnwired)
	}
	u, err := s.users.Get(ctx, username)
	if err != nil {
		return fmt.Errorf("auth: delete lookup %q: %w", username, err)
	}
	if builtinUndeletable[username] {
		return fmt.Errorf("%w: %q", ErrDeleteBuiltIn, username)
	}
	// Guard order follows architecture 14.1 E4's listing: last-admin BEFORE
	// self. On the wire the gate already demands an admin caller, so a
	// last-admin target can only BE the caller (any second admin would
	// invalidate "last") — checking self first would make the last-admin
	// 400 unreachable from HTTP and answer the less informative wording.
	if u.Role == RoleAdmin {
		admins, aerr := s.userDelete.AdminRoleUsers(ctx)
		if aerr != nil {
			return fmt.Errorf("auth: admin census for delete %q: %w", username, aerr)
		}
		if len(adminsWithOther(admins, username)) == 0 {
			return fmt.Errorf("%w: %q", ErrDeleteLastAdmin, username)
		}
	}
	if actor != "" && actor == username {
		return fmt.Errorf("%w: %q", ErrDeleteSelf, username)
	}
	if err := s.userDelete.DeleteCascade(ctx, username); err != nil {
		return fmt.Errorf("auth: delete %q: %w", username, err)
	}
	return nil
}

// adminsWithOther filters the census down to the admins that would SURVIVE
// the delete (every admin-role name except the target itself).
func adminsWithOther(admins []string, target string) []string {
	var others []string
	for _, name := range admins {
		if name != target {
			others = append(others, name)
		}
	}
	return others
}
