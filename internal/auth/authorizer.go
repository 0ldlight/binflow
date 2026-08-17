package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
)

// Can implements Authorizer (architecture section 3.4). Decision order:
// admin bypass; named permission targets (repo listed + include hit + no
// exclude hit + principal row carries the action); no grant denies; the
// anonymous rule only applies to nil principals. Store errors deny and are
// logged (fail closed) — a broken permission table must never open access.
func (s *Service) Can(ctx context.Context, p *Principal, repoKey, path, action string) bool {
	if p != nil && p.Admin {
		return true
	}
	if p == nil {
		// ADR-0009: anonymous access is read-only and content-only. Can is
		// the content-path decision point; the management plane keeps its
		// own authenticated-or-401 gate (httpapi, T-14).
		return s.anonymousRead && action == ActionRead
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
		if !strings.EqualFold(row.Principal, p.Name) || row.PrincipalType != "user" {
			continue
		}
		if !rowAllows(row, action) {
			continue
		}
		t, ok := targetByName[row.TargetName]
		if !ok {
			continue
		}
		repos, includes, excludes, err := decodeTarget(t)
		if err != nil {
			slog.ErrorContext(ctx, "auth: malformed permission target, skipping",
				slog.String("target", t.Name), slog.String("error", err.Error()))
			continue
		}
		if !slices.Contains(repos, repoKey) {
			continue
		}
		// include/exclude: empty includes means "everything" (auth-model.md
		// section 4); any exclude hit wins over includes.
		if len(includes) > 0 && !matchesAny(includes, path) {
			continue
		}
		if matchesAny(excludes, path) {
			continue
		}
		return true
	}
	return false
}

// rowAllows reports whether the principal row grants the action.
func rowAllows(row PermissionRow, action string) bool {
	switch action {
	case ActionRead:
		return row.CanRead
	case ActionWrite:
		return row.CanWrite
	case ActionDelete:
		return row.CanDelete
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
