// Closed-set role model and the management-plane capability chain (M7,
// ADR-0026 as finalized by T-214). Three planes, one decision point:
//
//   - content plane (r/w/d/m)          -> Can (authorizer.go), unchanged
//     target evaluation for users; role short-circuits for admin (all true)
//     and readonly_admin (r true, w/d/m false, targets never consulted);
//   - management plane, global         -> CanManage (this file), the precise
//     replacement of routeAuth's admin boolean (T-215 migrates the routes);
//   - management plane, single repo    -> CanManageRepo (this file): admin
//     true / readonly_admin !write / user = Can(repo, "", "m") — the m action
//     IS repo-level admin.
//
// Invariants (architecture 3.4a; violating any of them is a review reject):
//  1. no privilege chain: m opens no security:*/system:* capability;
//  2. single decision point: everything lives on *Service next to Can;
//  3. m never appears on the docker protocol plane (scope words stay
//     pull/push/delete, internal/adapter/docker/scope.go's vocabulary);
//  4. wire compatibility: the wire field is adminRole with snake values
//     spelled exactly like these constants (error wording is T-215's).

package auth

import (
	"context"
	"log/slog"
)

// Role is the closed set of system roles (users.role, migration 011). The
// closed set is code constants, not DB rows — adding a role is an
// architecture change (a new ADR), never a data change (ADR-0026 decision 1).
type Role string

const (
	// RoleAdmin is the full-trust role: content plane bypass plus every
	// management capability (identical to the pre-M7 is_admin boolean).
	RoleAdmin Role = "admin"
	// RoleReadOnlyAdmin reads everything and modifies nothing: management
	// plane limited to the three read capabilities, content plane globally
	// read-only (r always granted, w/d/m always denied). Permission targets
	// are SHORT-CIRCUITED for this role — target rows have no effect on it,
	// which is what makes the name a security invariant rather than a
	// default (T-214 ruling ①: combination is ineffective, not illegal).
	RoleReadOnlyAdmin Role = "readonly_admin"
	// RoleUser is the default: management plane denied (self-service routes
	// are required-only and not part of the management plane), content plane
	// and repo-level admin resolved through permission targets.
	RoleUser Role = "user"
)

// ParseRole validates one wire/DB spelling against the closed set. The second
// return is false for anything outside {admin, readonly_admin, user},
// including the kebab spelling deliberately rejected by T-214③. Consumers map
// false to their 400 wording (T-215).
func ParseRole(s string) (Role, bool) {
	switch Role(s) {
	case RoleAdmin, RoleReadOnlyAdmin, RoleUser:
		return Role(s), true
	default:
		return "", false
	}
}

// EffectiveRole resolves the principal's role: the Role field when it names a
// closed-set value, otherwise derived from the Admin convenience flag
// (hand-built principals — older tests and call sites — set only Admin), and
// RoleUser for everyone else. Admin principals built as Principal{Admin:true}
// therefore keep passing everything, and a RoleUser principal that
// inconsistently carries Admin=true is treated as admin (Admin remains the
// derived compatibility field the architecture preserves).
func (p *Principal) EffectiveRole() Role {
	if p == nil {
		return RoleUser
	}
	if r, ok := ParseRole(string(p.Role)); ok {
		return r
	}
	if p.Admin {
		return RoleAdmin
	}
	return RoleUser
}

// principalRole normalizes a stored/claimed role spelling onto the closed
// set: unknown and empty values become RoleUser (fail-safe default for rows
// predating 011 or hand-built fixtures).
func principalRole(role string) Role {
	if r, ok := ParseRole(role); ok {
		return r
	}
	return RoleUser
}

// newPrincipal builds a principal whose Role and Admin fields agree (Admin is
// the derived mirror of Role, architecture 3.4a).
func newPrincipal(name string, role Role, source Provider) *Principal {
	return &Principal{Name: name, Role: role, Admin: role == RoleAdmin, Source: source}
}

// ManagementCapability is one closed-set management-plane capability — the
// precise replacement of routeAuth's admin boolean (the route inventory is
// architecture section 7.1 [M7], T-214 final; T-215 migrates route by route).
type ManagementCapability string

const (
	// CapSystemRead covers health panel, storage stats, audit queries,
	// replication status and migration status reads.
	CapSystemRead ManagementCapability = "system:read"
	// CapSystemWrite covers GC triggers (every route including dry-run),
	// migration start and replication config CRUD.
	CapSystemWrite ManagementCapability = "system:write"
	// CapSecurityRead covers users/groups/token listing and detail reads.
	CapSecurityRead ManagementCapability = "security:read"
	// CapSecurityWrite covers users/groups/permission-target CRUD, role
	// assignment and token revocation — admin-only by invariant 1.
	CapSecurityWrite ManagementCapability = "security:write"
	// CapRepoRead covers the repository-configuration global reads (list,
	// detail) that are not single-repo addressed (those go through
	// CanManageRepo).
	CapRepoRead ManagementCapability = "repo:read"
	// CapRepoWrite covers repo creation and deletion — global admin-only,
	// deliberately NOT delegated to m holders (ADR-0026 decision 3: the
	// create arm and DELETE stay behind this gate).
	CapRepoWrite ManagementCapability = "repo:write"
)

// ManagementCapabilities is the closed capability set, for validation and QA
// matrix enumeration (ADR-0026 rationale: a closed set is exhaustively
// verifiable — Role 3 x Capability 6 covers the whole management plane).
var ManagementCapabilities = []ManagementCapability{
	CapSystemRead, CapSystemWrite,
	CapSecurityRead, CapSecurityWrite,
	CapRepoRead, CapRepoWrite,
}

// validCapability reports whether capability names a member of the closed
// set. Unknown values deny for every role — a typo'd route gate must fail
// closed, not pass admins through an undefined capability.
func validCapability(capability ManagementCapability) bool {
	for _, c := range ManagementCapabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// CanManage decides one global management-plane question (architecture 3.4a
// evaluation chain ②):
//
//	p == nil              -> false (the authentication gate answers 401 first;
//	                         middleware order is unchanged, this layer only
//	                         evaluates)
//	admin                 -> true for every capability
//	readonly_admin        -> the three read capabilities only
//	user                  -> false (self-service routes — password change,
//	                         token minting, whoami, session — are required-only
//	                         and never consult CanManage)
//
// The capability must name the closed set; unknown values deny for everyone.
func (s *Service) CanManage(_ context.Context, p *Principal, capability ManagementCapability) bool {
	if p == nil || !validCapability(capability) {
		return false
	}
	switch p.EffectiveRole() {
	case RoleAdmin:
		return true
	case RoleReadOnlyAdmin:
		return capability == CapSystemRead || capability == CapSecurityRead || capability == CapRepoRead
	default:
		return false
	}
}

// CanManageRepo decides one single-repo management question (architecture
// 3.4a evaluation chain ③) — the /api/repositories/{key} family, quota fields
// and the usage view:
//
//	p == nil          -> false (401 is the middleware's, as above)
//	admin             -> true
//	readonly_admin    -> !write (read verbs pass, write verbs deny)
//	user              -> Can(p, repoKey, "", "m") — repo-level admin is the m
//	                     action, i.e. a permission target listing repoKey with
//	                     a principal row carrying can_manage (includes and
//	                     excludes never participate).
//
// Repo creation and deletion stay on the global CapRepoWrite gate and are
// deliberately NOT reachable through this seam (ADR-0026 decision 3).
func (s *Service) CanManageRepo(ctx context.Context, p *Principal, repoKey string, write bool) bool {
	if p == nil {
		return false
	}
	switch p.EffectiveRole() {
	case RoleAdmin:
		return true
	case RoleReadOnlyAdmin:
		return !write
	default:
		return s.Can(ctx, p, repoKey, "", ActionManage)
	}
}

// ManagementAuthorizer is the management-plane facet of *Service, discovered
// by consumers through type assertion (the same pattern as the session and
// permission-view facets): the Authorizer interface itself stays unchanged so
// existing fakes keep compiling. T-215's route gates consume this seam.
type ManagementAuthorizer interface {
	CanManage(ctx context.Context, p *Principal, capability ManagementCapability) bool
	CanManageRepo(ctx context.Context, p *Principal, repoKey string, write bool) bool
}

// coverageRowsSource is the optional full-rows facet of the permission
// source (M9 E9, T-254): every principal row of every target in one call,
// the shape ManageCoverage's single-pass evaluation needs. It rides a type
// assertion like every other optional facet so permissionSource's existing
// implementations (unit fakes predating the seam) keep compiling; a source
// without the facet simply cannot answer coverage questions and
// ManageCoverage denies.
type coverageRowsSource interface {
	Principals(ctx context.Context) ([]PermissionRow, error)
}

// ManageCoverage answers "which repositories does this principal hold the
// manage bit on" — E9's single decision point (M9, ADR-0030 / architecture
// section 14.1.9): E6's filter branch and the family-4 write arms both ride
// this function, so the read arm and the write arms can never disagree on
// what a holder covers. The evaluation is the EffectiveRole-family union:
// for every permission target carrying a can_manage row that addresses the
// principal — its own user row (case-insensitive name, Can's rule) or a
// group row naming one of Principal.Groups (the SE-07 union) — the target's
// whole repos list joins the set. Includes/excludes never participate:
// manage has no path subdomain (ADR-0026 decision 3).
//
// The boolean is the UNIVERSE sentinel: an admin's coverage is unbounded —
// the role bypass grants m on every repository, present and future, which
// no enumerable set can express — so the map is nil whenever the boolean is
// true and callers must treat that combination as "covers everything" (a
// subset test against a nil map alone would deny an admin, the footgun the
// sentinel exists to prevent). readonly_admin holds no manage bit anywhere
// (Can denies m for the role), so its coverage is the empty, non-universe
// set — CanManageRepo's write arm denies it exactly the same way.
//
// Failure posture mirrors Can: store errors log and deny — the empty,
// non-universe set — because a broken permission table must never widen
// access. The cost is O(targets+rows) over exactly two store reads per
// call; the caching seam is deliberately left unimplemented (section
// 14.1.9's budget: a thousand-row instance evaluates fine, once per
// request).
func (s *Service) ManageCoverage(ctx context.Context, p *Principal) (map[string]struct{}, bool) {
	if p == nil {
		// Uniform with every other non-universe answer: a usable (empty)
		// set, never nil — only the universe sentinel nils the map.
		return map[string]struct{}{}, false
	}
	switch p.EffectiveRole() {
	case RoleAdmin:
		return nil, true
	case RoleReadOnlyAdmin:
		return map[string]struct{}{}, false
	}
	src, ok := s.permissions.(coverageRowsSource)
	if !ok {
		slog.ErrorContext(ctx, "auth: permission source lacks the rows facet, denying manage coverage",
			slog.String("user", p.Name))
		return map[string]struct{}{}, false
	}
	rows, err := src.Principals(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auth: manage coverage rows failed, denying",
			slog.String("user", p.Name), slog.String("error", err.Error()))
		return map[string]struct{}{}, false
	}
	targets, err := s.permissions.ListTargets(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auth: manage coverage target listing failed, denying",
			slog.String("user", p.Name), slog.String("error", err.Error()))
		return map[string]struct{}{}, false
	}
	// One pass over the rows: which targets carry m for THIS principal.
	holdsManage := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.CanManage && rowCoversPrincipal(row, p) {
			holdsManage[row.TargetName] = true
		}
	}
	coverage := make(map[string]struct{})
	for _, t := range targets {
		if !holdsManage[t.Name] {
			continue
		}
		repos, _, _, err := decodeTarget(t)
		if err != nil {
			// Malformed JSON columns skip the target (fail closed), Can's
			// posture for the same rows.
			slog.ErrorContext(ctx, "auth: malformed permission target, skipping",
				slog.String("target", t.Name), slog.String("error", err.Error()))
			continue
		}
		for _, repoKey := range repos {
			coverage[repoKey] = struct{}{}
		}
	}
	return coverage, false
}

// compile-time proof that the single decision point carries the facet.
var _ ManagementAuthorizer = (*Service)(nil)
