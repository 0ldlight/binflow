// Group management plane (T-97, PRD FR-27 / SE-01..04): the Artifactory-
// compatible /api/security/groups family over metadata.GroupStore.
//
// Wire shape notes (the endpoint family is a K1 provisional — the reverse
// expansion ticket may calibrate codes/wordings; they are kept as constants
// and one-word messages so a calibration is a narrow edit):
//
//   - GET    /api/security/groups          200 [{name, uri, description}]
//   - GET    /api/security/groups/{name}   200 {name, uri, description} | 404
//   - PUT    /api/security/groups/{name}   201 no body (create) / 200 no body
//     (update of an existing group); body {name, description}, body name
//     mismatching the path is a 400
//   - POST   /api/security/groups/{name}   200 no body (partial update,
//     description only); unknown group 404
//   - DELETE /api/security/groups/{name}   200 plain text; a group
//     referenced by a permission target is NOT deletable — 409 listing the
//     referencing target names (PRD K3: refuse rather than silently strip
//     members of their grants); success cascades membership teardown (FK)
//
// Errors are the user-management plain-text layer (PRD section 5.1 split);
// the admin gate itself renders the route plane's envelope like every
// /api/security route. Groups carry no admin bit (FR-27-AC9).

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// groupNamePattern is FR-27-AC9's charset: lowercase letter first, then
// lowercase letters, digits, dots, underscores or hyphens.
var groupNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// groupNameMaxLen caps the group name length (FR-27-AC9: <= 64).
const groupNameMaxLen = 64

// reservedGroupNames are rejected outright (FR-27-AC9): "anonymous" names
// the built-in anonymous principal, "_system_" the reserved system account.
var reservedGroupNames = map[string]bool{
	"anonymous": true,
	"_system_":  true,
}

// groupBody is the wire shape of the create/update bodies.
type groupBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// groupListItem is one GET /api/security/groups entry (SE-01): name, the
// item's own API link, description.
type groupListItem struct {
	Name        string `json:"name"`
	URI         string `json:"uri"`
	Description string `json:"description"`
}

// validateGroupName answers "" when name is acceptable, else the plain-text
// 400 message (K1 provisional wording).
func validateGroupName(name string) string {
	switch {
	case name == "":
		return "Unable to create group: group name is required."
	case reservedGroupNames[name]:
		return "Unable to create group: '" + name + "' is a reserved name."
	case len(name) > groupNameMaxLen:
		return fmt.Sprintf("Unable to create group: name exceeds %d characters.", groupNameMaxLen)
	case !groupNamePattern.MatchString(name):
		return "Unable to create group: name must match [a-z][a-z0-9._-]* (lowercase first, then lowercase letters, digits, dots, underscores or hyphens)."
	}
	return ""
}

// groupURI is the item's own API link (same shape as the users list).
func groupURI(r *http.Request, name string) string {
	return requestBase(r) + "/binflow/api/security/groups/" + name
}

// handleGroupList serves GET /api/security/groups (SE-01).
func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	groups, err := s.deps.Metadata.Groups().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list groups: "+err.Error())
		return
	}
	items := make([]groupListItem, 0, len(groups))
	for _, g := range groups {
		items = append(items, groupListItem{Name: g.Name, URI: groupURI(r, g.Name), Description: g.Description})
	}
	writeJSONBody(w, http.StatusOK, items)
}

// handleGroupGet serves GET /api/security/groups/{name}: one group's
// observable facts (W17's description round-trip).
func (s *Server) handleGroupGet(w http.ResponseWriter, r *http.Request, name string) {
	g, err := s.deps.Metadata.Groups().Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, metadata.ErrGroupNotFound) {
			writePlainError(w, http.StatusNotFound, "Group not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "get group: "+err.Error())
		return
	}
	writeJSONBody(w, http.StatusOK, groupListItem{Name: g.Name, URI: groupURI(r, g.Name), Description: g.Description})
}

// handleGroupPut serves PUT /api/security/groups/{name} (SE-02): create
// (201, no body) or update the description of an existing group (200, no
// body). The AC pins the mismatch code at 400 (users use 409 there; groups
// follow the PRD's K1 value, not the users precedent).
func (s *Server) handleGroupPut(w http.ResponseWriter, r *http.Request, name string) {
	var body groupBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	if msg := validateGroupName(name); msg != "" {
		writePlainError(w, http.StatusBadRequest, msg)
		return
	}
	if body.Name != "" && body.Name != name {
		writePlainError(w, http.StatusBadRequest,
			"The group name provided in the request body does not match the group name in the request path.")
		return
	}

	_, err := s.deps.Metadata.Groups().Get(r.Context(), name)
	if err != nil && !errors.Is(err, metadata.ErrGroupNotFound) {
		writePlainError(w, http.StatusInternalServerError, "get group: "+err.Error())
		return
	}
	if err == nil {
		// Update of an existing group: 200, no body (K1).
		if err := s.deps.Metadata.Groups().Update(r.Context(), &metadata.Group{
			Name: name, Description: body.Description, UpdatedAt: nowRFC3339UTC(),
		}); err != nil {
			writePlainError(w, http.StatusInternalServerError, "update group: "+err.Error())
			return
		}
		s.recordGroupAudit(r, audit.ActionGroupUpdate, name, body.Description)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Create: 201, no body (K1).
	now := nowRFC3339UTC()
	if err := s.deps.Metadata.Groups().Create(r.Context(), &metadata.Group{
		Name: name, Description: body.Description, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		writePlainError(w, http.StatusInternalServerError, "create group: "+err.Error())
		return
	}
	s.recordGroupAudit(r, audit.ActionGroupCreate, name, body.Description)
	w.WriteHeader(http.StatusCreated)
}

// handleGroupPost serves POST /api/security/groups/{name} (SE-03): partial
// update, description only; unknown group 404 (auth-model section 1.4's
// partial-update posture applied to groups).
func (s *Server) handleGroupPost(w http.ResponseWriter, r *http.Request, name string) {
	var body groupBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name != "" && body.Name != name {
		writePlainError(w, http.StatusBadRequest,
			"The group name provided in the request body does not match the group name in the request path.")
		return
	}
	if _, err := s.deps.Metadata.Groups().Get(r.Context(), name); err != nil {
		if errors.Is(err, metadata.ErrGroupNotFound) {
			writePlainError(w, http.StatusNotFound, "Group not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "get group: "+err.Error())
		return
	}
	if err := s.deps.Metadata.Groups().Update(r.Context(), &metadata.Group{
		Name: name, Description: body.Description, UpdatedAt: nowRFC3339UTC(),
	}); err != nil {
		writePlainError(w, http.StatusInternalServerError, "update group: "+err.Error())
		return
	}
	s.recordGroupAudit(r, audit.ActionGroupUpdate, name, body.Description)
	w.WriteHeader(http.StatusOK)
}

// handleGroupDelete serves DELETE /api/security/groups/{name} (SE-04). A
// group referenced by a permission target is refused with 409 naming the
// targets (PRD K3, BinFlow-strict): deleting it would silently strip every
// member of the grants those targets carry. Deleting an unreferenced group
// succeeds and the membership rows cascade away through the FK (architecture
// section 6: users' groups[] then simply no longer list it).
func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request, name string) {
	if _, err := s.deps.Metadata.Groups().Get(r.Context(), name); err != nil {
		if errors.Is(err, metadata.ErrGroupNotFound) {
			writePlainError(w, http.StatusNotFound, "Group not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "get group: "+err.Error())
		return
	}
	refs, err := s.deps.Metadata.Permissions().GroupReferences(r.Context(), name)
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "lookup group references: "+err.Error())
		return
	}
	if len(refs) > 0 {
		writePlainError(w, http.StatusConflict, fmt.Sprintf(
			"Cannot delete group '%s': it is referenced by permission target(s): %s. Remove the group from those targets first.",
			name, strings.Join(refs, ", ")))
		return
	}
	if err := s.deps.Metadata.Groups().Delete(r.Context(), name); err != nil {
		if errors.Is(err, metadata.ErrGroupNotFound) {
			writePlainError(w, http.StatusNotFound, "Group not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "delete group: "+err.Error())
		return
	}
	s.recordGroupAudit(r, audit.ActionGroupDelete, name, "")
	writeText(w, http.StatusOK, "The group: '"+name+"' has been removed successfully.")
}

// recordGroupAudit lands one group-plane event (best-effort, like every
// audit emitter in this package). Detail carries the group name plus the
// new description; nothing user-supplied here is credential-shaped, and
// Redact still runs on the stored payload.
func (s *Server) recordGroupAudit(r *http.Request, action, name, description string) {
	p := principalFrom(r.Context())
	actor := ""
	if p != nil {
		actor = p.Name
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actor,
		Action: action,
		Detail: auditDetail("name", name, "description", description),
	})
}

// auditDetail renders key/value pairs as a JSON object string ('{}' on a
// marshal failure, which cannot happen for string values).
func auditDetail(kv ...string) string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ---- membership maintenance (SE-06: the groups[] field of the users plane) ----

// validateGroupNames answers whether every named group exists, writing the
// spec's 400 wording (auth-model.md section 1.3 item 8, high confidence)
// when one does not. The pre-check exists for the MESSAGE: the store's own
// FK-validated SetUserGroups fails with a wrapped sentinel but only the
// pre-check knows which name to name.
func (s *Server) validateGroupNames(w http.ResponseWriter, r *http.Request, names []string) bool {
	if len(names) == 0 {
		return true
	}
	groups, err := s.deps.Metadata.Groups().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list groups: "+err.Error())
		return false
	}
	known := make(map[string]bool, len(groups))
	for _, g := range groups {
		known[g.Name] = true
	}
	for _, n := range names {
		if !known[n] {
			writePlainError(w, http.StatusBadRequest, fmt.Sprintf("Unable to find group by name '%s'.", n))
			return false
		}
	}
	return true
}

// setUserGroups atomically replaces the user's membership set and records
// the group.member audit event. Effective immediately: the next
// authenticated request re-reads the membership (auth.Service fills
// Principal.Groups per request, NFR-S25 — no cache to invalidate).
func (s *Server) setUserGroups(r *http.Request, username string, groups []string) error {
	if err := s.deps.Metadata.Groups().SetUserGroups(r.Context(), username, groups); err != nil {
		return err
	}
	p := principalFrom(r.Context())
	actor := ""
	if p != nil {
		actor = p.Name
	}
	if groups == nil {
		groups = []string{}
	}
	detail, err := json.Marshal(map[string]any{"user": username, "groups": groups})
	if err != nil {
		detail = []byte("{}")
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actor,
		Action: audit.ActionGroupMember,
		Detail: string(detail),
	})
	return nil
}
