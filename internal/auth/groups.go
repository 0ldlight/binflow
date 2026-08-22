// Group plane of the auth service (T-97 / PRD FR-27, SE-07): the
// authentication-time membership fill of Principal.Groups and the
// effective-permission view consumed by GET /api/storage/{repo}/{path}
// ?permissions (SE-08).
//
// Authorization semantics: the groups a user belongs to contribute exactly
// the permission_principals rows with principal_type='group' naming them —
// a union with the user's own rows, nothing more. Groups carry no admin bit
// (FR-27-AC9): an "admin"-named group grants nothing administrative, the
// effective-admin decision stays per-user by design (an intentional
// incompatibility with Artifactory's group admin, SE-07).

package auth

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
)

// groupSource is the consumer-side slice of metadata.GroupStore the group
// plane needs (interface defined at the consumer, per project convention).
// Implementations return group NAMES ordered deterministically.
type groupSource interface {
	GroupsOfUser(ctx context.Context, username string) ([]string, error)
}

// GroupSource re-exports the consumer-side seam so callers building
// alternative stores (tests, caching wrappers) can implement it without the
// unexported type; metadata.GroupStore is adapted onto it in deps.go.
type GroupSource = groupSource

// WithGroups returns a copy of svc whose group plane is backed by src.
// Without it the fill is inert: principals carry no groups and group-typed
// permission rows authorize nobody (the pre-M4 posture, which keeps New()-
// built services in older tests byte-compatible). NewFromStore applies it
// for every store-backed assembly.
func (s *Service) WithGroups(src GroupSource) *Service {
	clone := *s
	clone.groups = src
	return &clone
}

// fillGroups resolves the principal's group memberships onto p
// (Principal.Groups). Nil p (anonymous) and an unwired source are no-ops.
//
// Failure posture — fail closed on the GROUP side only: a broken membership
// lookup must never open a group-derived grant, so the principal proceeds
// with no groups (its own direct grants keep working; the error is logged
// loudly). Rejecting the whole credential instead would turn a damaged join
// into a total outage for accounts whose access has nothing to do with
// groups, which is the wrong blast radius for an enrichment read.
//
// T-192: a context that already ended (client disconnected while the
// credential was being verified) skips the doomed lookup and its ERROR
// line entirely — an abandonment storm must not flood the operator log
// with failed enrichment reads no response will ever consume.
func (s *Service) fillGroups(ctx context.Context, p *Principal) {
	if p == nil || s.groups == nil {
		return
	}
	if ctx.Err() != nil {
		return
	}
	names, err := s.groups.GroupsOfUser(ctx, p.Name)
	if err != nil {
		slog.ErrorContext(ctx, "auth: group membership lookup failed, principal carries no groups",
			slog.String("user", p.Name), slog.String("error", err.Error()))
		return
	}
	// Union with any existing groups (e.g., OIDC claims groups already set by
	// authenticateOIDC). Deduplicate to avoid double-counting overlapping
	// memberships.
	seen := make(map[string]bool, len(p.Groups)+len(names))
	for _, g := range p.Groups {
		seen[g] = true
	}
	for _, g := range names {
		if !seen[g] {
			p.Groups = append(p.Groups, g)
			seen[g] = true
		}
	}
}

// rowCoversPrincipal reports whether one permission_principals row addresses
// p: the user's own row (principal_type "user", case-insensitive name — the
// M1 rule), or a group row naming one of the principal's groups (SE-07
// union). Rows of any other type never cover.
func rowCoversPrincipal(row PermissionRow, p *Principal) bool {
	switch row.PrincipalType {
	case "user":
		return strings.EqualFold(row.Principal, p.Name)
	case "group":
		return slices.Contains(p.Groups, row.Principal)
	default:
		return false
	}
}

// PrincipalBits is the per-principal action triple of the effective
// permission view (SE-08): which of read/write/delete one principal holds
// on one repo path through the targets covering it.
type PrincipalBits struct {
	Read   bool
	Write  bool
	Delete bool
}

// ItemPrincipals computes the effective-permission view of (repoKey, path)
// (SE-08, rest-api.md section 3): for every permission target whose scope
// covers the pair — the exact predicate Can applies per row — the union of
// its user and group principal grants. The path follows the repo layer's
// convention (a trailing '/' addresses a folder), so pattern semantics
// (folder prefix rule, exclude priority) mirror authorization precisely.
//
// Malformed target rows are skipped with a log line (Can's posture); a
// permission-source failure is returned for the caller to answer 500. The
// maps are never nil: an item no target covers answers empty views.
func (s *Service) ItemPrincipals(ctx context.Context, repoKey, path string) (users, groups map[string]PrincipalBits, err error) {
	users = map[string]PrincipalBits{}
	groups = map[string]PrincipalBits{}
	rows, err := s.permissions.PrincipalsFor(ctx, repoKey)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: permission lookup: %w", err)
	}
	if len(rows) == 0 {
		return users, groups, nil
	}
	targets, err := s.permissions.ListTargets(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("auth: permission target listing: %w", err)
	}
	targetByName := make(map[string]Target, len(targets))
	for _, t := range targets {
		targetByName[t.Name] = t
	}
	// merge unions one covering row's actions into the principal's view.
	merge := func(m map[string]PrincipalBits, row PermissionRow) {
		b := m[row.Principal]
		b.Read = b.Read || row.CanRead
		b.Write = b.Write || row.CanWrite
		b.Delete = b.Delete || row.CanDelete
		m[row.Principal] = b
	}
	for _, row := range rows {
		if row.PrincipalType != "user" && row.PrincipalType != "group" {
			continue
		}
		t, ok := targetByName[row.TargetName]
		if !ok {
			continue
		}
		covers, err := targetCovers(t, repoKey, path)
		if err != nil {
			slog.ErrorContext(ctx, "auth: malformed permission target, skipping",
				slog.String("target", t.Name), slog.String("error", err.Error()))
			continue
		}
		if !covers {
			continue
		}
		if row.PrincipalType == "group" {
			merge(groups, row)
		} else {
			merge(users, row)
		}
	}
	return users, groups, nil
}
