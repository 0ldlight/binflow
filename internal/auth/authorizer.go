package auth

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Can implements Authorizer (architecture section 3.4, evaluation chain ① as
// widened by ADR-0026). Decision order:
//
//   - nil principal: the anonymous rule (read-only, only when the flag is on)
//     — ADR-0009, unchanged;
//   - admin role: bypass everything (the pre-M7 is_admin behavior);
//   - readonly_admin role: globally read-only — r is always granted, w/d/m/a
//     always denied (a = annotate, M16 ADR-0044 K68: the property-write face
//     is a write face), and permission targets are NEVER consulted for this role
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
//
// T-491 (FR-156.2): the repos-listing arm widens to the preset wildcard
// buckets (wildcard.go). When repoKey resolves to a repository of a class a
// bucket names, the principal rows of targets listing that bucket join the
// evaluation (one extra PrincipalsFor read) and targetCovers/targetListsRepo
// accept either the exact key or the bucket. Nothing else moves: patterns,
// verb columns and the union-across-targets combination are untouched, and
// the bucket-row read failing denies exactly like the first one.
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

	// The one bucket that covers repoKey this evaluation ("" when none —
	// wildcard.go): the bucket's rows join the walk below and both coverage
	// predicates consume it, so the m plane and the path plane share the
	// widened repos rule.
	bucket := s.bucketFor(ctx, repoKey)
	rows, err := s.permissions.PrincipalsFor(ctx, repoKey)
	if err != nil {
		slog.ErrorContext(ctx, "auth: permission lookup failed, denying",
			slog.String("repo", repoKey), slog.String("user", p.Name),
			slog.String("action", action), slog.String("error", err.Error()))
		return false
	}
	if bucket != "" {
		brows, err := s.permissions.PrincipalsFor(ctx, bucket)
		if err != nil {
			// The same fail-closed posture as the first read: a broken
			// permission table must never open access, and answering from
			// partial data would be exactly that.
			slog.ErrorContext(ctx, "auth: wildcard bucket lookup failed, denying",
				slog.String("repo", repoKey), slog.String("bucket", bucket),
				slog.String("user", p.Name), slog.String("action", action),
				slog.String("error", err.Error()))
			return false
		}
		rows = append(rows, brows...)
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
			covers, err := targetListsRepo(t, repoKey, bucket)
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
		covers, err := targetCovers(t, repoKey, bucket, path)
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
// (repoKey, path): the repo is listed (directly or through the applicable
// wildcard bucket, T-491 — repoListed), the path matches an include pattern
// (empty includes means "everything", auth-model.md section 4) and no
// exclude pattern (any exclude hit wins over includes). The shared
// predicate of Can and ItemPrincipals — the ?permissions view (SE-08) must
// never disagree with the authorization decision on what a target covers.
// bucket is the literal wildcard.go resolved for repoKey ("" disables the
// bucket arm: exact-key matching only).
func targetCovers(t Target, repoKey, bucket, path string) (bool, error) {
	repos, includes, excludes, err := decodeTarget(t)
	if err != nil {
		return false, err
	}
	if !repoListed(repos, repoKey, bucket) {
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
// and never participate). T-491: the listing accepts the applicable
// wildcard bucket like targetCovers does, so a manage grant through ANY
// LOCAL is manage on every local repository, present and future. Malformed
// JSON is an error the caller logs and skips, mirroring targetCovers.
func targetListsRepo(t Target, repoKey, bucket string) (bool, error) {
	repos, _, _, err := decodeTarget(t)
	if err != nil {
		return false, err
	}
	return repoListed(repos, repoKey, bucket), nil
}

// rowAllows reports whether the principal row grants the action. m consults
// only can_manage — carrying m implies none of r/w/d, and vice versa. a
// (annotate, M16 T-444 / ADR-0044 K68) consults only can_annotate: w no
// longer implies the property-write face (the split), and neither does m
// (the no-privilege-chain invariant — m is configuration power, not a
// content-plane grant).
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
	case ActionAnnotate:
		return row.CanAnnotate
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
