package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
)

// Can implements Authorizer (architecture section 3.4, evaluation chain ① as
// widened by ADR-0026). Decision order:
//
//   - nil principal: the anonymous rule (read-only, only when the flag is on)
//     — ADR-0009, unchanged;
//   - admin role: bypass everything (the pre-M7 is_admin behavior);
//   - readonly_admin role: globally read-only — r is always granted, w/d/m
//     always denied, and permission targets are NEVER consulted for this role
//     (the short-circuit is the security invariant itself: a target row
//     carrying w for a group the readonly admin belongs to has no effect —
//     "combination is ineffective, not illegal", T-214 ruling ①);
//   - user role: named permission targets (repo listed + include hit + no
//     exclude hit + principal row carries the action); no grant denies.
//
// The m action (M7, ADR-0026 decision 3) is repo-scoped: a target matches on
// its repos list only — includes/excludes never apply to it (manage has no
// path subdomain, docs/reverse/auth-model.md section 4) — and m implies none
// of r/w/d. Since T-97 (SE-07) a principal row covers the user either through
// its own user row or through a group row naming one of Principal.Groups — a
// union, never an intersection. Store errors deny and are logged (fail
// closed) — a broken permission table must never open access.
func (s *Service) Can(ctx context.Context, p *Principal, repoKey, path, action string) bool {
	if p == nil {
		// ADR-0009: anonymous access is read-only and content-only. Can is
		// the content-path decision point; the management plane keeps its
		// own authenticated-or-401 gate (httpapi, T-14).
		return s.anonymousRead && action == ActionRead
	}
	switch p.EffectiveRole() {
	case RoleAdmin:
		return true
	case RoleReadOnlyAdmin:
		// Globally read-only: targets are short-circuited (see the doc
		// comment) — the role alone answers.
		return action == ActionRead
	}

	rows, err := s.permissions.PrincipalsFor(ctx, repoKey)
	if err != nil {
		slog.ErrorContext(ctx, "auth: permission lookup failed, denying",
			slog.String("repo", repoKey), slog.String("user", p.Name),
			slog.String("action", action), slog.String("error", err.Error()))
		return false
	}
	if len(rows) == 0 {
		return false
	}

	targets, err := s.permissions.ListTargets(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "auth: permission target listing failed, denying",
			slog.String("repo", repoKey), slog.String("user", p.Name),
			slog.String("action", action), slog.String("error", err.Error()))
		return false
	}
	targetByName := make(map[string]Target, len(targets))
	for _, t := range targets {
		targetByName[t.Name] = t
	}

	for _, row := range rows {
		if !rowCoversPrincipal(row, p) {
			continue
		}
		if !rowAllows(row, action) {
			continue
		}
		t, ok := targetByName[row.TargetName]
		if !ok {
			continue
		}
		if action == ActionManage {
			// Repo-scoped match: only the repos list decides. A malformed
			// JSON repos column skips the target (fail closed), the same
			// posture as the path plane below.
			covers, err := targetListsRepo(t, repoKey)
			if err != nil {
				slog.ErrorContext(ctx, "auth: malformed permission target, skipping",
					slog.String("target", t.Name), slog.String("error", err.Error()))
				continue
			}
			if !covers {
				continue
			}
			return true
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
		return true
	}
	return false
}

// targetCovers reports whether one permission target's scope covers
// (repoKey, path): the repo is listed, the path matches an include pattern
// (empty includes means "everything", auth-model.md section 4) and no
// exclude pattern (any exclude hit wins over includes). The shared
// predicate of Can and ItemPrincipals — the ?permissions view (SE-08) must
// never disagree with the authorization decision on what a target covers.
func targetCovers(t Target, repoKey, path string) (bool, error) {
	repos, includes, excludes, err := decodeTarget(t)
	if err != nil {
		return false, err
	}
	if !slices.Contains(repos, repoKey) {
		return false, nil
	}
	if len(includes) > 0 && !matchesAny(includes, path) {
		return false, nil
	}
	if matchesAny(excludes, path) {
		return false, nil
	}
	return true, nil
}

// targetListsRepo reports whether one permission target's repos list names
// repoKey — the ENTIRE m-action coverage rule (ADR-0026 decision 3: manage is
// repository-configuration power, includes/excludes are path-plane concepts
// and never participate). Malformed JSON is an error the caller logs and
// skips, mirroring targetCovers.
func targetListsRepo(t Target, repoKey string) (bool, error) {
	repos, _, _, err := decodeTarget(t)
	if err != nil {
		return false, err
	}
	return slices.Contains(repos, repoKey), nil
}

// rowAllows reports whether the principal row grants the action. m consults
// only can_manage — carrying m implies none of r/w/d, and vice versa.
func rowAllows(row PermissionRow, action string) bool {
	switch action {
	case ActionRead:
		return row.CanRead
	case ActionWrite:
		return row.CanWrite
	case ActionDelete:
		return row.CanDelete
	case ActionManage:
		return row.CanManage
	default:
		return false
	}
}

// decodeTarget unpacks the JSON array columns of a permission target.
func decodeTarget(t Target) (repos, includes, excludes []string, err error) {
	if err := json.Unmarshal([]byte(t.Repos), &repos); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal([]byte(t.Includes), &includes); err != nil {
		return nil, nil, nil, err
	}
	if err := json.Unmarshal([]byte(t.Excludes), &excludes); err != nil {
		return nil, nil, nil, err
	}
	return repos, includes, excludes, nil
}
