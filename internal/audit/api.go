package audit

import (
	"encoding/json"
	"strings"
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
	Time       string // RFC3339 UTC; stamped by the Logger when empty
	Actor      string // principal name, or ActorAnonymous
	Action     string // deploy|delete|download|login.success|login.failed|repo.create|...
	Repo       string
	Path       string
	RemoteAddr string
	Detail     string // JSON object string
}

// Filter narrows a Query.
type Filter struct {
	Repo  string
	Actor string
	Limit int // <= 0 means 100
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
