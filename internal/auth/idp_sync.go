// Identity-provider group/admin synchronization (T-185, T-174 defect D4):
// the write plane behind "every login refreshes the group membership"
// (PRD FR-54 OD-05 / FR-55 LD-02, ADR-0020 administrator-mapping clause).
//
// Before T-185 the OIDC/LDAP authentication arms built the Principal with
// the claims' groups but never persisted them: the membership lived only in
// the request-scoped Principal, so the session arm (which rebuilds the
// Principal from the user row on every request) dropped them again —
// group-authorized repositories answered 403 for OIDC/LDAP session users,
// and is_admin was frozen at first login (the QA H28 failure).
//
// Semantics pinned here (PRD medium confidence, fixed by tests):
//
//   - REPLACE, not union: on every successful provider authentication the
//     user's user_groups set becomes the claims' group set. A group the IdP
//     removed disappears from the membership at the next login, which is
//     what makes revoked group permissions expire immediately (FR-54-AC5).
//     Local memberships an admin assigned to a federated user are therefore
//     also refreshed away — mixed-provenance membership would need
//     per-row source tracking (M6+ scope, flagged in the T-185 report).
//   - Materialize-nothing: only claims groups that have a local group row
//     are synced. An IdP group grants nothing until an admin creates the
//     group and attaches permissions (the exact H28 posture: the QA flow
//     pre-created "developers"). Unknown names are skipped, never fatal —
//     an IdP typically carries far more groups than BinFlow mirrors.
//   - nil claims groups (GroupFilter unset / groups_claim absent from the
//     token) means "no group information", not "zero groups": the sync is
//     skipped so configurations without a group plane never clobber
//     memberships. A present-but-empty claim ("groups": []) IS a zero set
//     and clears the membership.
//   - is_admin: the provider is authoritative on EVERY authentication
//     (ADR-0020), so a drifted row is refreshed in place; the request's
//     Principal uses the claims' value either way.

package auth

import (
	"context"
	"log/slog"
)

// groupSyncSource is the write side of metadata.GroupStore the IdP sync
// needs (interface defined at the consumer, per project convention).
type groupSyncSource interface {
	// SetUserGroups atomically replaces the user's membership with the
	// named groups (every name must exist locally; the sync filters first).
	SetUserGroups(ctx context.Context, username string, groupNames []string) error
	// ListGroupNames returns every local group name — the existence filter
	// of the sync (an unmateralized IdP group is skipped, not created).
	ListGroupNames(ctx context.Context) ([]string, error)
}

// GroupSyncSource re-exports the seam so callers building alternative stores
// (tests, caching wrappers) can implement it without the unexported type;
// metadata.GroupStore is adapted onto it in deps.go.
type GroupSyncSource = groupSyncSource

// adminFlagWriter is the write seam the is_admin refresh needs: one account's
// admin flag, everything else preserved.
type adminFlagWriter interface {
	SetAdmin(ctx context.Context, username string, isAdmin bool) error
}

// AdminFlagWriter re-exports the seam (same pattern as GroupSyncSource).
type AdminFlagWriter = adminFlagWriter

// WithGroupSync returns a copy of svc whose IdP group sync is backed by src.
// Without it the sync is inert (the pre-T-185 posture), which keeps New()-
// built services byte-compatible; NewFromStore wires it for every
// store-backed assembly.
func (s *Service) WithGroupSync(src GroupSyncSource) *Service {
	clone := *s
	clone.groupSync = src
	return &clone
}

// WithAdminFlagWriter returns a copy of svc whose is_admin refresh is backed
// by w. See WithGroupSync for the inert-by-default posture.
func (s *Service) WithAdminFlagWriter(w AdminFlagWriter) *Service {
	clone := *s
	clone.adminWriter = w
	return &clone
}

// syncProviderGroups mirrors the provider's group set into user_groups
// (username's membership). Failure posture — fail loud, not fatal: a broken
// sync is logged and the authentication proceeds (the Principal already
// carries the claims' groups for this request). Rejecting the login instead
// would turn a damaged groups table into a total IdP lockout, the wrong
// blast radius for an enrichment write — the same posture fillGroups
// settled for the read side.
func (s *Service) syncProviderGroups(ctx context.Context, c *Claims, username string) {
	if c == nil || c.Groups == nil || s.groupSync == nil || s.groups == nil {
		return
	}
	claimed := c.Groups
	current, err := s.groups.GroupsOfUser(ctx, username)
	if err != nil {
		slog.ErrorContext(ctx, "auth: idp group sync membership read failed",
			slog.String("user", username), slog.String("error", err.Error()))
		return
	}
	if sameNameSet(current, claimed) {
		return // steady state: every claimed group is already synced
	}
	local, err := s.groupSync.ListGroupNames(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auth: idp group sync group listing failed",
			slog.String("user", username), slog.String("error", err.Error()))
		return
	}
	synced, skipped := materializedNames(claimed, local)
	if len(skipped) > 0 {
		// Debug, not warn: unmapped IdP groups are the expected case (the
		// directory carries many groups BinFlow never mirrors).
		slog.DebugContext(ctx, "auth: idp groups have no local row, not synced",
			slog.String("user", username), slog.Any("groups", skipped))
	}
	if sameNameSet(current, synced) {
		return // nothing to write (only unmapped names differ)
	}
	if err := s.groupSync.SetUserGroups(ctx, username, synced); err != nil {
		slog.ErrorContext(ctx, "auth: idp group sync write failed",
			slog.String("user", username), slog.String("error", err.Error()))
		return
	}
	slog.InfoContext(ctx, "auth: idp group membership synced",
		slog.String("user", username), slog.Any("groups", synced))
}

// refreshProviderAdmin refreshes the user row's is_admin to the provider's
// verdict and returns the value the Principal must carry. The provider is
// the authority (ADR-0020): a failed persistence is logged and the claims'
// value still governs this request — the row catches up on the next
// authentication.
func (s *Service) refreshProviderAdmin(ctx context.Context, c *Claims, username string, dbAdmin bool) bool {
	if c == nil || s.adminWriter == nil || c.Admin == dbAdmin {
		return dbAdmin
	}
	if err := s.adminWriter.SetAdmin(ctx, username, c.Admin); err != nil {
		slog.ErrorContext(ctx, "auth: provider admin flag refresh failed",
			slog.String("user", username), slog.Bool("provider_admin", c.Admin),
			slog.String("error", err.Error()))
	}
	return c.Admin
}

// materializedNames splits the claimed names into those with a local group
// row (synced, claims order preserved) and those without (skipped). Empty
// and duplicate names are dropped — neither can reference a group row.
func materializedNames(claimed, local []string) (synced, skipped []string) {
	localSet := make(map[string]struct{}, len(local))
	for _, n := range local {
		localSet[n] = struct{}{}
	}
	seen := make(map[string]struct{}, len(claimed))
	for _, n := range claimed {
		if n == "" {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		if _, ok := localSet[n]; ok {
			synced = append(synced, n)
		} else {
			skipped = append(skipped, n)
		}
	}
	return synced, skipped
}

// sameNameSet reports whether two name lists hold the same distinct set
// (order-insensitive). GroupsOfUser returns distinct names; claimed lists
// are deduplicated by materializedNames before the comparison.
func sameNameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]struct{}, len(a))
	for _, n := range a {
		seen[n] = struct{}{}
	}
	for _, n := range b {
		if _, ok := seen[n]; !ok {
			return false
		}
	}
	return true
}
