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
// It rides CanManageRepo(write=true) so the decision stays on the single
// authorizer seam: for role=user that reduces to Can(repo, "", "m"), the
// exact predicate route gates use; readonly_admin fails it (write arm),
// keeping the read-only invariant. The empty set DENIES: the arm must
// certify a non-empty subset (create validation demands at least one
// repository anyway), so a caller with no manage bit anywhere — or a body
// naming none — fails like everyone else.
func (s *Server) canManageAllRepos(ctx context.Context, p *auth.Principal, repos []string) bool {
	if p == nil || len(repos) == 0 {
		return false
	}
	for _, repoKey := range repos {
		if !managementAllowed(s.deps.Authz, func(m auth.ManagementAuthorizer) bool {
			return m.CanManageRepo(ctx, p, repoKey, true)
		}) {
			return false
		}
	}
	return true
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
				case "write":
					row.CanWrite = true
				case "delete":
					row.CanDelete = true
				case "manage":
					// T-217 (FR-65): the repo-scoped admin bit joins the wire
					// action set — auth-model.md section 4's manage action,
					// the only widening beyond r/w/d (annotate and friends
					// stay refused below).
					row.CanManage = true
				default:
					writePlainError(w, http.StatusBadRequest, fmt.Sprintf(
						"unknown permission action %q (supported: read, write, delete, manage)", a))
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
// principals expanded (FR-5-AC10).
func (s *Server) handlePermissionList(w http.ResponseWriter, r *http.Request) {
	targets, err := s.deps.Metadata.Permissions().ListTargets(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list permission targets: "+err.Error())
		return
	}
	out := make([]permissionBody, 0, len(targets))
	for _, t := range targets {
		body := permissionBody{
			Name:            t.Name,
			Repos:           unmarshalStrings(t.Repos),
			IncludePatterns: unmarshalStrings(t.Includes),
			ExcludePatterns: unmarshalStrings(t.Excludes),
			Principals: permissionPrincipalsBody{
				Users:  map[string][]string{},
				Groups: map[string][]string{},
			},
		}
		_, principalRows, err := s.deps.Metadata.Permissions().GetTarget(r.Context(), t.Name)
		if err == nil {
			for _, row := range principalRows {
				// T-217 (FR-65): the manage bit echoes in the same r/w/d
				// order plus m — the wire's round-trip of the family-4
				// exception (what was granted through "manage" must read
				// back through "manage").
				actions := make([]string, 0, 4)
				if row.CanRead {
					actions = append(actions, "read")
				}
				if row.CanWrite {
					actions = append(actions, "write")
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
		}
		out = append(out, body)
	}
	writeJSONBody(w, http.StatusOK, out)
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
