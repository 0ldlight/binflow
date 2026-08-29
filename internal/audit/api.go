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
	Action     string // deploy|delete|download|login.success|auth.failed|repo.create|...
	Repo       string
	Path       string
	RemoteAddr string
	Detail     string // JSON object string
}

// Detail keys of the authentication events (T-187 / T-174 D8). The
// audit_events row schema has no columns for them, so like remote_addr they
// ride the Detail JSON object — the query plane renders that object
// verbatim, so consumers see the fields inside detail.
const (
	DetailKeyMethod = "method"
	DetailKeyReason = "reason"
)

// AuthEventDetail builds the Detail payload of an authentication audit
// event: {"method": ...} on success, {"method": ..., "reason": ...} on
// failure (PRD FR-56-AC3). method is the arm (local/oidc/ldap); reason is
// the minimal classification (bad_credentials / user_not_found /
// provider_error / tls_handshake / ...). An empty reason is omitted (the
// success shape); neither key is credential-shaped, so Redact leaves them
// alone.
func AuthEventDetail(method, reason string) string {
	m := map[string]string{DetailKeyMethod: method}
	if reason != "" {
		m[DetailKeyReason] = reason
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}" // unreachable: a flat string map always marshals
	}
	return string(b)
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
//
// ActionAuthFail is the M6 PRD spelling (FR-56-AC3, section 6.4): it
// replaced the M4 "login.failed" name wholesale in T-187 (T-174 D7) — one
// vocabulary, no alias. Rows written before T-187 still carry the old name
// (the log is append-only; history is never rewritten), so a filter on
// "login.failed" keeps matching pre-upgrade events only.
const (
	ActionDeploy         = "deploy"
	ActionDelete         = "delete"
	ActionDownload       = "download"
	ActionLoginOK        = "login.success"
	ActionAuthFail       = "auth.failed"
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
	// ActionCleanupRun records one unused-cleanup engine run (T-324,
	// FR-102.2): the remote-cache policy pass plus the GC/session legs it
	// drove, manual (REST) or scheduled (cron). The audit trail is the
	// run-history surface, same posture as gc.run (ADR-0015 erratum ②).
	ActionCleanupRun = "cleanup.run"
	// ActionGroupMember records one membership-set change of a user (the
	// groups[] field of PUT/POST /api/security/users/{name}). Defined by
	// T-97 per its dispatch note (the T-93 vocabulary covers group CRUD but
	// no member-change action; NFR-S25 requires membership changes to leave
	// an audit trail). Detail carries {"user", "groups"}.
	ActionGroupMember = "group.member"
	// ActionUserRoleChange records one role assignment or move of a user
	// (the adminRole field and the admin boolean of the users plane, M7
	// FR-64/ADR-0026 decision 6 — NFR-S42: role changes leave a trail).
	// Detail carries {"user", "old", "new"}; old is empty when the account
	// was created directly with a non-default role.
	ActionUserRoleChange = "user.role.change"
	// ActionUserDelete records one account deletion (M9, FR-78.2 /
	// ADR-0030 E4 — NFR-S48: the delete leaves a trail; the cascade killed
	// every credential the account held). Detail carries {"user"}. Only
	// successful deletions land here: the guard rejections (built-in,
	// self, last admin) are decisions, not deletions.
	ActionUserDelete = "user.delete"
)

// The M6~M12 accumulated vocabulary (T-346 / FR-113.4, the audit owner's
// one-liner every emitting ticket registered): actions that landed with
// their own planes — spelled as local literals at the emit sites, each
// report flagging "joins Actions() with the audit owner's follow-up". This
// is that follow-up: the picker now carries every action the trail can
// hold. Emit sites keep their literals (their own tests pin those); these
// constants are the picker's single source and the anchor for future
// emitters. Provenance per family:
//
//   - props.* (M4 properties plane), replication.*/keypair.* (M6 T-180 /
//     M11 T-319 configuration planes), auth.config.* (M11 T-305; the SAML
//     SP-key verbs are T-331), license.* (the license plane + the T-283
//     addon gate), artifact.copy/move (M12 T-339), artifact.explode (M12
//     T-343), trash.* (M12 T-345), storage.replay.* (M12 T-338 fail-open).
const (
	ActionPropsWrite  = "props.write"
	ActionPropsDelete = "props.delete"

	ActionReplicationPush       = "replication.push"
	ActionReplicationPushFailed = "replication.push.failed"
	ActionReplicationCfgCreate  = "replication.config.create"
	ActionReplicationCfgDelete  = "replication.config.delete"

	ActionKeypairCreate    = "keypair.create"
	ActionKeypairUpdate    = "keypair.update"
	ActionKeypairGenerate  = "keypair.generate"
	ActionKeypairDelete    = "keypair.delete"
	ActionKeypairVerify    = "keypair.verify"
	ActionKeypairAssociate = "keypair.associate"

	ActionAuthConfigUpdate = "auth.config.update"
	ActionAuthConfigTest   = "auth.config.test"
	ActionSAMLKeyGenerate  = "auth.config.samlkey.generate"
	ActionSAMLKeyRegen     = "auth.config.samlkey.regenerate"

	ActionLicenseInstall   = "license.install"
	ActionLicenseDelete    = "license.delete"
	ActionLicenseInvalid   = "license.invalid"
	ActionLicenseAddonDeny = "license.addon.denied"

	ActionArtifactCopy    = "artifact.copy"
	ActionArtifactMove    = "artifact.move"
	ActionArtifactExplode = "artifact.explode"

	ActionTrashRestore   = "trash.restore"
	ActionTrashEmpty     = "trash.empty"
	ActionTrashClean     = "trash.clean"
	ActionTrashRetention = "trash.retention"

	ActionStorageReplayWindow = "storage.replay.window"
	ActionStorageReplayDrain  = "storage.replay.drained"
)

// Actions returns the full action vocabulary (GE-02 + the T-346 sweep):
// every action the audit query plane can filter on. It is the picker and
// assertion source, not a gate — filtering on an unknown action simply
// matches nothing (forward compatibility with future vocabulary).
func Actions() []string {
	return []string{
		ActionDeploy, ActionDelete, ActionDownload,
		ActionLoginOK, ActionAuthFail,
		ActionRepoCreate, ActionRepoUpdate, ActionRepoDelete,
		ActionTokenIssue, ActionTokenRevoke,
		ActionPasswordChange,
		ActionGroupCreate, ActionGroupUpdate, ActionGroupDelete,
		ActionGroupMember,
		ActionUserRoleChange, ActionUserDelete,
		ActionPermissionCreate, ActionPermissionUpdate, ActionPermissionDelete,
		ActionGCRun, ActionExportRun, ActionImportRun,
		ActionQuotaExceeded, ActionCleanupRun,
		// T-346 (FR-113.4): the accumulated planes, finally in the picker.
		ActionPropsWrite, ActionPropsDelete,
		ActionReplicationPush, ActionReplicationPushFailed,
		ActionReplicationCfgCreate, ActionReplicationCfgDelete,
		ActionKeypairCreate, ActionKeypairUpdate, ActionKeypairGenerate,
		ActionKeypairDelete, ActionKeypairVerify, ActionKeypairAssociate,
		ActionAuthConfigUpdate, ActionAuthConfigTest,
		ActionSAMLKeyGenerate, ActionSAMLKeyRegen,
		ActionLicenseInstall, ActionLicenseDelete, ActionLicenseInvalid,
		ActionLicenseAddonDeny,
		ActionArtifactCopy, ActionArtifactMove, ActionArtifactExplode,
		ActionTrashRestore, ActionTrashEmpty, ActionTrashClean, ActionTrashRetention,
		ActionStorageReplayWindow, ActionStorageReplayDrain,
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
