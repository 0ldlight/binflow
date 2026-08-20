package audit

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Event is one audit record (architecture section 3.5, extended with
// RemoteAddr per ticket T-11: the actor's network origin).
//
// The metadata row schema (architecture section 6, audit_events) has no
// remote_addr column, so the Logger merges RemoteAddr into the stored
// Detail JSON under key "remote_addr" — structure stays aligned with
// metadata.AuditStore while the field remains queryable in the payload.
//
// Detail is a JSON object string. Never put credentials in it (NFR-S3):
// pass events through Redact to strip the standard slip-ups.
type Event struct {
	ID         int64  // row id; zero on the append path, set by Query (GE-01)
	Time       string // RFC3339 UTC; stamped by the Logger when empty
	Actor      string // principal name, or ActorAnonymous
	Action     string // deploy|delete|download|login.success|login.failed|repo.create|...
	Repo       string
	Path       string
	RemoteAddr string
	Detail     string // JSON object string
}

// Filter narrows a Query (full-parameter form, GE-01/T-93). Every field is
// optional; the zero filter returns the newest events. Since and Until must
// be canonical RFC3339 UTC text — pass caller input through
// NormalizeTimestamp first — and form a closed-open window on Time:
// Since inclusive, Until exclusive. Cursor is the opaque keyset cursor of
// the previous page's last event (Page.NextCursor); Limit <= 0 means 100.
type Filter struct {
	Repo   string
	Actor  string
	Action string
	Since  string // RFC3339 UTC, inclusive lower bound
	Until  string // RFC3339 UTC, exclusive upper bound
	Limit  int    // <= 0 means 100
	Cursor string // "" means first page
}

// Page is one Query result: the matched events newest-first (time DESC,
// id DESC) plus the opaque cursor of the following page. NextCursor is ""
// on the last page — a full page is terminal exactly when NextCursor is
// empty, which is why Query probes one row beyond the limit.
type Page struct {
	Events     []Event
	NextCursor string
}

// NormalizeTimestamp validates one Since/Until parameter and returns it in
// the canonical form the store compares textually: RFC3339 in UTC, second
// precision. Offsets are honored (+02:00 becomes Z) and sub-second digits
// are floored — audit rows carry second-granularity times (metadata.Now),
// so canonical whole seconds are exactly the domain where lexicographic
// text order equals chronological order. The empty value passes through
// (the bound is absent); anything that does not parse as RFC3339 is an
// error the HTTP plane answers with 400.
func NormalizeTimestamp(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return "", fmt.Errorf("audit: timestamp %q is not RFC3339: %w", v, err)
	}
	return ts.UTC().Format(time.RFC3339), nil
}

// ActorAnonymous is the actor recorded for unauthenticated access
// (ADR-0009: audit events record actor "anonymous").
const ActorAnonymous = "anonymous"

// Action vocabulary (architecture section 3.5 / section 6 DDL comment).
const (
	ActionDeploy         = "deploy"
	ActionDelete         = "delete"
	ActionDownload       = "download"
	ActionLoginOK        = "login.success"
	ActionLoginFail      = "login.failed"
	ActionRepoCreate     = "repo.create"
	ActionRepoUpdate     = "repo.update"
	ActionRepoDelete     = "repo.delete"
	ActionTokenIssue     = "token.issue"
	ActionTokenRevoke    = "token.revoke"
	ActionPasswordChange = "password.change"
)

// M4 governance vocabulary (PRD FR-29 / GE-02). The emitting surfaces land
// with their own tickets — T-94 gc.run, T-95 quota.exceeded, T-96
// export.run/import.run, T-97 group.*/permission.* — while this ticket
// ships the shared query face: append sites spell actions through these
// constants, and Actions is the list the audit query plane and the console
// action picker settle on.
const (
	ActionGroupCreate      = "group.create"
	ActionGroupUpdate      = "group.update"
	ActionGroupDelete      = "group.delete"
	ActionPermissionCreate = "permission.create"
	ActionPermissionUpdate = "permission.update"
	ActionPermissionDelete = "permission.delete"
	ActionGCRun            = "gc.run"
	ActionExportRun        = "export.run"
	ActionImportRun        = "import.run"
	ActionQuotaExceeded    = "quota.exceeded"
	// ActionGroupMember records one membership-set change of a user (the
	// groups[] field of PUT/POST /api/security/users/{name}). Defined by
	// T-97 per its dispatch note (the T-93 vocabulary covers group CRUD but
	// no member-change action; NFR-S25 requires membership changes to leave
	// an audit trail). Detail carries {"user", "groups"}.
	ActionGroupMember = "group.member"
)

// Actions returns the full M1~M4 action vocabulary (GE-02): every action
// the audit query plane can filter on. It is the picker and assertion
// source, not a gate — filtering on an unknown action simply matches
// nothing (forward compatibility with future vocabulary).
func Actions() []string {
	return []string{
		ActionDeploy, ActionDelete, ActionDownload,
		ActionLoginOK, ActionLoginFail,
		ActionRepoCreate, ActionRepoUpdate, ActionRepoDelete,
		ActionTokenIssue, ActionTokenRevoke,
		ActionPasswordChange,
		ActionGroupCreate, ActionGroupUpdate, ActionGroupDelete,
		ActionGroupMember,
		ActionPermissionCreate, ActionPermissionUpdate, ActionPermissionDelete,
		ActionGCRun, ActionExportRun, ActionImportRun,
		ActionQuotaExceeded,
	}
}

// credentialKeys are Detail keys (normalized to lowercase) that must never
// persist (NFR-S3). Redact replaces their values with "[REDACTED]";
// comparison is case-insensitive so the protocol's camelCase field names
// (userName/oldPassword/newPassword1/... auth-model.md section 2.1) and
// header spellings (Authorization, X-JFrog-Art-Api, api-key, bearer) are
// covered alongside the snake_case forms.
var credentialKeys = map[string]bool{
	// password family
	"password": true, "old_password": true, "new_password": true,
	"new_password1": true, "new_password2": true,
	"oldpassword": true, "newpassword": true, "newpassword1": true, "newpassword2": true,
	// token family
	"token": true, "access_token": true, "refresh_token": true,
	"accesstoken": true, "refreshtoken": true,
	// header / key family
	"authorization": true, "api_key": true, "apikey": true,
	"x-jfrog-art-api": true, "x-api-key": true, "bearer": true,
	"secret": true, "master_key": true,
}

// redactedValue replaces credential values in stored Detail payloads.
const redactedValue = "[REDACTED]"

// Redact returns e with credential-shaped Detail keys masked
// (case-insensitive key match). A malformed or empty Detail is normalized
// to "{}" rather than rejected — audit must record the event even when its
// payload author was sloppy.
func Redact(e Event) Event {
	if e.Detail == "" {
		e.Detail = "{}"
		return e
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(e.Detail), &m); err != nil {
		return e // not an object: store as-is (it lost any key structure too)
	}
	for k, v := range m {
		if v == nil {
			continue
		}
		if credentialKeys[strings.ToLower(k)] {
			m[k] = redactedValue
		}
	}
	if b, err := json.Marshal(m); err == nil {
		e.Detail = string(b)
	}
	return e
}
