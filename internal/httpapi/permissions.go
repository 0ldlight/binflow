package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// BinFlow-owned permission target CRUD (PRD E-24, FR-5-AC8/AC10): the
// /binflow/api/v1/permissions plane over metadata.PermissionStore. Artifacts
// of the same capability exist at Artifactory's /api/v2/security/
// permissions; BinFlow deliberately does not promise that path (M4 will
// re-evaluate). Errors here are plain text — this is management plane.
//
// The replace operation is one store transaction (PutTarget replaces the
// target row and every principal row), so a failed update cannot leave half
// a grant behind.
//
// T-217 (FR-65, ADR-0026 decision 3 / architecture section 7.1 family 4
// exception): the write verbs' gate is evaluated HERE, not at the route —
// "CapSecurityWrite ∨ target.repos ⊆ the caller's manage coverage" is
// body-dependent (the repositories arrive in the POST body / from the stored
// target on DELETE), so the route carries only the authentication demand and
// this file owns the OR. The manage wire itself: principals action lists
// accept and echo "manage" (docs/reverse/auth-model.md section 4's action
// set subset; existing targets without the bit behave exactly as before).
//
// T-444 (M16, FR-146.1 / ADR-0044 K68 / architecture section 25.6): the
// action vocabulary renews to {read, deploy-cache, annotate, delete,
// manage} — the reference's five permission-matrix columns. "write" stays
// ACCEPTED on POST as a deploy-cache alias (legacy scripts keep working;
// removal evaluated M17) but the GET echo renders the canonical single
// form: the compat arm is receive-only. The DB column (can_write) and the
// internal code (w) are unchanged — deploy/cache stay one merged column;
// the split that happened is the property-write face (annotate, its own
// bit since migration 023), not a deploy/cache separation.
//
// T-254 (M9 E6, ADR-0030 / architecture section 14.1.6) completes the
// family's READ arm through the same move: GET /api/v1/permissions with a
// non-empty ?filter= rides a required-only route and handlePermissionList
// evaluates "CapSecurityRead full list ∨ non-empty manage coverage filtered
// subset ∨ the same 403", with the coverage question answered by
// auth.ManageCoverage — E9's single decision point, which the write arms'
// canManageAllRepos below also rides, so the read and write faces of the
// coverage can never disagree. The parameterless GET is frozen byte for
// byte (route, gate, error body and list rendering).
//
// T-491 (M17, FR-156.2 / M16 B-2.16): repos[] accepts the three preset
// wildcard buckets (auth.BucketAnyLocal / BucketAnyRemote /
// BucketAnyDistribution — the reference product's internal constants; the
// console renders them "Any Local" / "Any Remote" / "Any Distribution").
// The buckets name repository populations, so the existence check skips
// them while every other spelling still demands a repository row; the
// GET echo round-trips whatever was stored, verbatim as before. The
// evaluation semantics live in auth (wildcard.go) — this file only admits
// the spellings onto the wire.

// permissionBody is the wire shape of one permission target.
type permissionBody struct {
	Name            string                   `json:"name"`
	Repos           []string                 `json:"repos"`
	IncludePatterns []string                 `json:"includePatterns"`
	ExcludePatterns []string                 `json:"excludePatterns"`
	Principals      permissionPrincipalsBody `json:"principals"`
}

// permissionPrincipalsBody carries the two principal columns: per-user and
// per-group action lists (T-97 / SE-07: groups ride the same target, the
// authorizer unions both sides). Both maps render as {} when empty.
type permissionPrincipalsBody struct {
	Users  map[string][]string `json:"users"`
	Groups map[string][]string `json:"groups"`
}

// errPermissionEditDenied is the write-verb denial of the family-4 exception
// gate. One wording serves both arms: the caller holds no security:write
// capability AND the target's repositories do not sit inside its manage
// coverage (or it holds no manage bit at all).
const errPermissionEditDenied = "administrator privileges required (or, for manage holders: every repository the target names must sit inside your manage coverage)"

// canManageAllRepos reports whether every named repository sits inside the
// caller's manage coverage — the coverage half of family 4's exception OR.
// Since T-254 it rides auth.ManageCoverage (E9's single decision point,
// architecture section 14.1.9) instead of a per-repo CanManageRepo walk:
// for role=user the coverage set is exactly {r : Can(r, "", "m")} — same
// predicate, one evaluation instead of two store reads per repository —
// readonly_admin fails it (the role holds no m anywhere, and CanManageRepo's
// write arm denied it the same way), admin passes through the universe
// sentinel. The empty set DENIES: the arm must certify a non-empty subset
// (create validation demands at least one repository anyway), so a caller
// with no manage bit anywhere — or a body naming none — fails like
// everyone else.
func (s *Server) canManageAllRepos(ctx context.Context, p *auth.Principal, repos []string) bool {
	if p == nil || len(repos) == 0 {
		return false
	}
	coverage, universe := manageCoverageOf(ctx, s.deps.Authz, p)
	if universe {
		return true
	}
	for _, repoKey := range repos {
		if _, ok := coverage[repoKey]; !ok {
			return false
		}
	}
	return true
}

// manageCoverageAuthorizer is the E9 facet of the authorizer this plane
// consumes (M9, T-254, architecture section 14.1.9): the manage-coverage
// question behind E6's filter arm and the family-4 write arms. It is
// defined here, at the consumer, so the Authorizer interface and its
// existing fakes stay untouched — the same discovery pattern as
// ManagementAuthorizer.
type manageCoverageAuthorizer interface {
	ManageCoverage(ctx context.Context, p *auth.Principal) (map[string]struct{}, bool)
}

// manageCoverageOf resolves the authorizer's coverage facet and asks one
// question. A nil authorizer, or one without the facet, is the empty
// non-universe set — the fail-closed posture managementAllowed established
// for the capability facet: a missing decision point is a deny, never a
// pass. (Store failures deny the same way inside the seam, Can's posture.)
func manageCoverageOf(ctx context.Context, a auth.Authorizer, p *auth.Principal) (map[string]struct{}, bool) {
	m, ok := a.(manageCoverageAuthorizer)
	if !ok {
		return map[string]struct{}{}, false
	}
	return m.ManageCoverage(ctx, p)
}

// handlePermissionCreate serves POST /api/v1/permissions (create or wholly
// replace the named target, FR-5-AC8).
//
// The family-4 exception gate (T-217, ADR-0026 decision 3): security writers
// pass as before; everyone else must hold the manage bit on EVERY repository
// the body names. A body that cannot decode cannot name a coverage subset,
// so an unprivileged caller with garbage still meets the 403 the pre-T-217
// route gate answered before any parsing.
func (s *Server) handlePermissionCreate(w http.ResponseWriter, r *http.Request) {
	var body permissionBody
	decodeErr := decodeJSONBodyOf(r, &body)
	p := principalFrom(r.Context())
	if s.canManage(r.Context(), p, auth.CapSecurityWrite) {
		if decodeErr != nil {
			writePlainError(w, http.StatusBadRequest, decodeErr.Error())
			return
		}
	} else {
		// Family-4 exception, non-writer arm: the body's repositories must
		// sit inside the caller's manage coverage — and, when a target of the
		// same name already exists, that row's too (review round B1): POST is
		// create-or-REPLACE, so a holder covering only the body could
		// otherwise revoke or rewrite grants on out-of-coverage repositories
		// by replacing the target. The union check is both subsets passing.
		// A body that cannot decode cannot name a coverage subset — the
		// pre-T-217 route gate's 403-before-parsing posture is kept, and a
		// body-side denial skips the store lookup entirely.
		covered := decodeErr == nil && s.canManageAllRepos(r.Context(), p, body.Repos)
		if covered && strings.TrimSpace(body.Name) != "" {
			if existing, _, gerr := s.deps.Metadata.Permissions().GetTarget(r.Context(), body.Name); gerr == nil {
				covered = s.canManageAllRepos(r.Context(), p, unmarshalStrings(existing.Repos))
			} else if !errors.Is(gerr, metadata.ErrNotFound) {
				writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+gerr.Error())
				return
			}
		}
		if decodeErr != nil || !covered {
			writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
			return
		}
	}
	if strings.TrimSpace(body.Name) == "" {
		writePlainError(w, http.StatusBadRequest, "permission target name is required")
		return
	}
	if len(body.Repos) == 0 {
		writePlainError(w, http.StatusBadRequest, "Permission target request missing repositories: repos must contain at least one repository")
		return
	}
	for _, repo := range body.Repos {
		// T-491 (FR-156.2 / M16 B-2.16): the three preset wildcard buckets
		// are legal repos[] entries — they name a repository POPULATION
		// (every local / every remote repository, or the bundle-domain
		// pseudo-key channel of ADR-0046 point 3), so no repository row can
		// back them and the existence check must skip them. Everything else
		// still demands a real row; a literal outside the closed bucket set
		// (including the reference's fourth family member "ANY", which
		// BinFlow does not implement — wildcard.go's spec-pending note)
		// keeps the unknown-repository 400.
		if auth.IsWildcardBucket(repo) {
			continue
		}
		if _, err := s.deps.Repos.Get(r.Context(), repo); err != nil {
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("permission target references an unknown repository %q", repo))
			return
		}
	}
	users, err := s.deps.Metadata.Users().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list users for validation: "+err.Error())
		return
	}
	known := make(map[string]bool, len(users))
	for _, u := range users {
		known[u.Username] = true
	}
	for name := range body.Principals.Users {
		if !known[name] {
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("Unable to find user by name '%s'.", name))
			return
		}
	}
	groups, err := s.deps.Metadata.Groups().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list groups for validation: "+err.Error())
		return
	}
	knownGroups := make(map[string]bool, len(groups))
	for _, g := range groups {
		knownGroups[g.Name] = true
	}
	for name := range body.Principals.Groups {
		if !knownGroups[name] {
			// Same family wording as the users arm (auth-model.md section 4:
			// a principal referencing an unknown user/group is a 400).
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("Unable to find group by name '%s'.", name))
			return
		}
	}

	principals := make([]*metadata.PermissionPrincipal, 0, len(body.Principals.Users)+len(body.Principals.Groups))
	appendRows := func(entries map[string][]string, principalType string) bool {
		for name, actions := range entries {
			row := &metadata.PermissionPrincipal{
				TargetName: body.Name, Principal: name, PrincipalType: principalType,
			}
			for _, a := range actions {
				switch strings.ToLower(strings.TrimSpace(a)) {
				case "read":
					row.CanRead = true
				case "deploy-cache":
					// T-444 (FR-146.1 / ADR-0044 K68): the canonical write
					// word — deploy/cache stay one merged column (can_write),
					// the reference's Deploy/Cache parity.
					row.CanWrite = true
				case "write":
					// T-444: the legacy spelling, accepted as the
					// deploy-cache alias for one compat round (K68 point 2:
					// existing scripts and the pre-T-455 console keep
					// working; removal evaluated M17). It grants deploy-cache
					// ONLY — annotate is its own word now, and the alias does
					// not silently carry the property-write face.
					row.CanWrite = true
				case "annotate":
					// T-444: the property-write action (the ?properties
					// family's PUT/DELETE gate) — its own grant, no longer
					// the write action's shadow.
					row.CanAnnotate = true
				case "delete":
					row.CanDelete = true
				case "manage":
					// T-217 (FR-65): the repo-scoped admin bit joins the wire
					// action set — auth-model.md section 4's manage action.
					row.CanManage = true
				default:
					writePlainError(w, http.StatusBadRequest, fmt.Sprintf(
						"unknown permission action %q (supported: read, deploy-cache, annotate, delete, manage; write is accepted as a deploy-cache alias)", a))
					return false
				}
			}
			principals = append(principals, row)
		}
		return true
	}
	if !appendRows(body.Principals.Users, "user") || !appendRows(body.Principals.Groups, "group") {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	target := &metadata.PermissionTarget{
		Name:      body.Name,
		Repos:     marshalStrings(body.Repos),
		Includes:  marshalStrings(body.IncludePatterns),
		Excludes:  marshalStrings(body.ExcludePatterns),
		UpdatedAt: now,
	}
	existing, _, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), body.Name)
	switch {
	case err == nil:
		target.CreatedAt = existing.CreatedAt
	case errors.Is(err, metadata.ErrNotFound):
		target.CreatedAt = now
	default:
		writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+err.Error())
		return
	}
	if err := s.deps.Metadata.Permissions().PutTarget(r.Context(), target, principals); err != nil {
		writePlainError(w, http.StatusInternalServerError, "store permission target: "+err.Error())
		return
	}
	// Authorization-change audit (NFR-S25: every grant change leaves a
	// trail): the vocabulary distinguishes a fresh target from a replace.
	action := audit.ActionPermissionUpdate
	if existing == nil {
		action = audit.ActionPermissionCreate
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: action,
		Detail: auditDetail("name", body.Name, "principals", strconv.Itoa(len(principals))),
	})
	w.WriteHeader(http.StatusCreated)
}

// actorName resolves the audit actor from the request principal ("" lets
// audit's Append stamp "anonymous"; the management routes are authenticated
// so this is the admin's name in practice).
func actorName(r *http.Request) string {
	if p := principalFrom(r.Context()); p != nil {
		return p.Name
	}
	return ""
}

// marshalStrings renders a string slice as a JSON array column value
// ("[]" for empty).
func marshalStrings(vals []string) string {
	if len(vals) == 0 {
		return "[]"
	}
	b, err := json.Marshal(vals)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// handlePermissionList serves GET /api/v1/permissions: every target with its
// principals expanded (FR-5-AC10). The no-filter branch is FROZEN by M9's
// additive-only decree (architecture section 14.1.6: byte-identical
// behavior, gate and error body) — the E6 filter arm lives beside it in
// handlePermissionListManage, and only the body construction is shared.
func (s *Server) handlePermissionList(w http.ResponseWriter, r *http.Request) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	out := make([]permissionBody, 0, len(targets))
	for _, t := range targets {
		_, principalRows, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), t.Name)
		// A per-target read failure renders that target with empty
		// principals — the list is a read plane, one bad row must not 500
		// the inventory (pre-T-254 posture, kept verbatim).
		if err != nil {
			principalRows = nil
		}
		out = append(out, permissionBodyOf(t, unmarshalStrings(t.Repos), principalRows))
	}
	writeJSONBody(w, http.StatusOK, out)
}

// permissionManageFilterPresent reports whether the request carries a
// non-empty ?filter= ask (M9 E6): an empty value is no ask — the T-253
// optional-parameter convention — so ?filter= rides the FROZEN no-filter
// route for every caller, and ANY non-empty value (valid or not) takes the
// filter branch where the handler validates it. Whitespace-only values are
// empties here and tolerated-then-ignored there, one predicate on both
// sides of the route split.
func permissionManageFilterPresent(r *http.Request) bool {
	for _, v := range r.URL.Query()["filter"] {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// handlePermissionListManage serves GET /api/v1/permissions?filter=manage —
// E6, the m-holder readability face (M9, ADR-0030 / architecture section
// 14.1.6, FR-79.2). The route demands only authentication (the gate is
// query-dependent, so it is evaluated HERE, the family-4 write-verb
// pattern): security readers (admin, readonly_admin) receive the full list,
// byte-equivalent to the no-filter response; a plain user with a non-empty
// manage coverage receives exactly the targets whose every repository sits
// inside that coverage (partially covered targets are HIDDEN — B1's replace
// arm demands union(body, stored) ⊆ coverage, so a partially covered target
// is not editable and showing it would only invite a doomed save); the
// empty coverage answers the SAME 403 the no-filter route gate answers, so
// a principal without the manage bit cannot distinguish the two branches —
// zero new distinguishability (NFR-S49's information isolation: no
// out-of-coverage target's name, repository, pattern or principal appears
// anywhere in the response). An unknown filter value is the governance
// family's explicit 400 (refused, never silently ignored — T-253's
// ?include convention).
func (s *Server) handlePermissionListManage(w http.ResponseWriter, r *http.Request) {
	for _, v := range r.URL.Query()["filter"] {
		switch strings.TrimSpace(v) {
		case "":
			// An empty value carries no ask (?filter=&...): tolerated, like
			// every other optional parameter's empty spelling. The router
			// only sends non-empty asks down this branch; a repeated
			// parameter's empty entries land here.
		case "manage":
		default:
			writeError(w, http.StatusBadRequest,
				`filter must be "manage" (unknown filter value: `+strconv.Quote(strings.TrimSpace(v))+")")
			return
		}
	}
	p := principalFrom(r.Context())
	if s.canManage(r.Context(), p, auth.CapSecurityRead) {
		// The full list, rendered through the same single-trip renderer —
		// byte-equality with the no-filter response is pinned by test
		// (TestT254FilterManageRoleMatrix), which is what "equivalent"
		// means here.
		s.writePermissionTargets(w, r, nil)
		return
	}
	coverage, universe := manageCoverageOf(r.Context(), s.deps.Authz, p)
	if !universe && len(coverage) == 0 {
		// The same status, content type and body the route gate answers for
		// the no-filter request — writeError is the very helper that renders
		// "administrator privileges required".
		writeError(w, http.StatusForbidden, "administrator privileges required")
		return
	}
	s.writePermissionTargets(w, r, coverage)
}

// writePermissionTargets renders the permissions list. coverage == nil
// means no filtering (every target); otherwise a target renders iff it
// names at least one repository and every named repository sits inside the
// coverage — the editable set, PRD FR-79.2's gloss of the subset predicate.
// (An empty repos list cannot pass the write arm's non-empty certification,
// so the vacuous "empty set ⊆ coverage" reading would show a target the
// holder cannot edit; the wire's own validation keeps such targets from
// existing, the guard is for hand-seeded rows.) Rows arrive through ONE
// Principals query bucketed per target — the single-trip shape; the
// no-filter handler above deliberately keeps its historical
// GetTarget-per-target walk, frozen with its bytes.
func (s *Server) writePermissionTargets(w http.ResponseWriter, r *http.Request, coverage map[string]struct{}) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	allRows, err := s.deps.Metadata.Permissions().Principals(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission principals: "+err.Error())
		return
	}
	rowsByTarget := make(map[string][]*metadata.PermissionPrincipal, len(targets))
	for _, row := range allRows {
		rowsByTarget[row.TargetName] = append(rowsByTarget[row.TargetName], row)
	}
	out := make([]permissionBody, 0, len(targets))
	for _, t := range targets {
		repos := unmarshalStrings(t.Repos)
		if coverage != nil {
			covered := len(repos) > 0
			for _, repoKey := range repos {
				if _, ok := coverage[repoKey]; !ok {
					covered = false
					break
				}
			}
			if !covered {
				continue
			}
		}
		out = append(out, permissionBodyOf(t, repos, rowsByTarget[t.Name]))
	}
	writeJSONBody(w, http.StatusOK, out)
}

// permissionBodyOf builds one list row: the target's decoded columns plus
// its principal rows expanded into the wire's action letters. The shared
// construction of the frozen no-filter list and E6's filtered list — the
// two faces must render field for field identically (14.1.6: item fields
// identical to the full list).
func permissionBodyOf(t *metadata.PermissionTarget, repos []string, rows []*metadata.PermissionPrincipal) permissionBody {
	body := permissionBody{
		Name:            t.Name,
		Repos:           repos,
		IncludePatterns: unmarshalStrings(t.Includes),
		ExcludePatterns: unmarshalStrings(t.Excludes),
		Principals: permissionPrincipalsBody{
			Users:  map[string][]string{},
			Groups: map[string][]string{},
		},
	}
	for _, row := range rows {
		// T-217 (FR-65): the manage bit echoes in the same r/w/d order
		// plus m. T-444 (ADR-0044 K68): the write word echoes its canonical
		// deploy-cache form (the alias arm is receive-only) and annotate
		// takes its own column — the reference's five-column order.
		actions := make([]string, 0, 5)
		if row.CanRead {
			actions = append(actions, "read")
		}
		if row.CanWrite {
			actions = append(actions, "deploy-cache")
		}
		if row.CanAnnotate {
			actions = append(actions, "annotate")
		}
		if row.CanDelete {
			actions = append(actions, "delete")
		}
		if row.CanManage {
			actions = append(actions, "manage")
		}
		if row.PrincipalType == "group" {
			body.Principals.Groups[row.Principal] = actions
			continue
		}
		body.Principals.Users[row.Principal] = actions
	}
	return body
}

// unmarshalStrings parses a JSON array column value ("[]" for empty).
func unmarshalStrings(v string) []string {
	var out []string
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return []string{}
	}
	return out
}

// handlePermissionDelete serves DELETE /binflow/api/v1/permissions/{name}:
// the grants vanish with the target (the store deletes both tables in one
// transaction); an unknown name is a 404 for security writers.
//
// Family 4's exception gate (T-217): a non-security-writer may delete only a
// target whose every repository sits inside its manage coverage. For such
// callers an UNKNOWN name answers the coverage 403, not the 404 — an unknown
// target is definitionally outside any coverage, and hiding its existence
// keeps the permission plane's inventory (security:read data) away from
// principals who cannot list it.
func (s *Server) handlePermissionDelete(w http.ResponseWriter, r *http.Request, name string) {
	p := principalFrom(r.Context())
	secWrite := s.canManage(r.Context(), p, auth.CapSecurityWrite)
	target, _, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), name)
	switch {
	case err == nil:
	case errors.Is(err, metadata.ErrNotFound):
		if secWrite {
			writePlainError(w, http.StatusNotFound, "permission target not found: "+name)
			return
		}
		writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
		return
	default:
		writePlainError(w, http.StatusInternalServerError, "lookup permission target: "+err.Error())
		return
	}
	if !secWrite && !s.canManageAllRepos(r.Context(), p, unmarshalStrings(target.Repos)) {
		writePlainError(w, http.StatusForbidden, errPermissionEditDenied)
		return
	}
	if err := s.deps.Metadata.Permissions().DeleteTarget(r.Context(), name); err != nil {
		writePlainError(w, http.StatusInternalServerError, "delete permission target: "+err.Error())
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: audit.ActionPermissionDelete,
		Detail: auditDetail("name", name),
	})
	w.WriteHeader(http.StatusNoContent)
}
