package httpapi

// Authentication-configuration REST plane (M11 T-305, ADR-0035 / FR-92):
// the three protocol sections' read/write surface plus the test-connection
// endpoints, over auth.ConfigManager.
//
//	GET  /binflow/api/v1/admin/security/ldap           CapSecurityRead
//	PUT  /binflow/api/v1/admin/security/ldap           CapSecurityWrite
//	POST /binflow/api/v1/admin/security/ldap/test      CapSecurityWrite
//	GET  /binflow/api/v1/admin/security/oauth          CapSecurityRead
//	PUT  /binflow/api/v1/admin/security/oauth          CapSecurityWrite
//	POST /binflow/api/v1/admin/security/oauth/test     CapSecurityWrite
//	GET  /binflow/api/v1/admin/security/saml/config    CapSecurityRead
//	PUT  /binflow/api/v1/admin/security/saml/config    CapSecurityWrite
//	POST /binflow/api/v1/admin/security/saml/config/test CapSecurityWrite
//
// The path families, field names and anchored error texts follow
// docs/reverse/auth-integration.md v2 (the T-302 baseline): the Artifactory
// UI REST homes are /ui/api/v1/admin/security/{ldap,oauth,saml/config},
// re-homed under BinFlow's /binflow/api management root (ADR-0035 decision
// 1: dispatchAPI explicit routes, errors[] envelope, no
// apiProtocolMounts). BinFlow's single-section model (the auth_configs
// closed set) collapses each Artifactory list/collection face to its
// single-setting equivalent — the registered divergences live in
// reports/agents/T-305.md.
//
// Secrets never echo: GET renders the fixed 20-asterisk sentinel for a set
// secret (auth-integration §1.6) and PUT is write-only (absent or sentinel
// keeps the stored value, "" clears, new plaintext replaces — ADR-0035
// decision 5). Audit: auth.config.update carries actor/section/changed-key
// NAMES only; auth.config.test carries actor/section/result category.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Audit actions of the configuration plane. String literals, not
// audit-package constants: the replication.config.* precedent (T-180) —
// the vocabulary lands in audit.Actions()' picker as the audit owner's
// one-liner (leftover note in the T-305 report).
const (
	auditActionAuthConfigUpdate = "auth.config.update"
	auditActionAuthConfigTest   = "auth.config.test"
)

// authConfigMaxBodyBytes caps section payloads: a fully-populated LDAP or
// SAML section (certificate included) is a few tens of KiB; 1 MiB is
// generous headroom while staying a bound.
const authConfigMaxBodyBytes = 1 << 20

// AuthConfigPlane is the consumer-side seam behind the nine endpoints (the
// ConfigManager's management facet; nil Deps keeps them at the honest 503).
type AuthConfigPlane interface {
	GetAuthSection(ctx context.Context, section string) (json.RawMessage, error)
	PutAuthSection(ctx context.Context, section string, body []byte, actor string) (json.RawMessage, []string, error)
	TestAuthSection(ctx context.Context, section string, body []byte) (auth.TestReport, error)
}

// handleAuthConfigGet serves the masked echo of one section. An unset
// section answers its default shape (SAML: the anchored empty object {},
// auth-integration §3.2).
func (s *Server) handleAuthConfigGet(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.AuthConfigs == nil {
			writeError(w, http.StatusServiceUnavailable, "auth configuration plane is not available on this instance")
			return
		}
		doc, err := s.deps.AuthConfigs.GetAuthSection(r.Context(), section)
		if err != nil {
			s.writeAuthConfigError(w, r, "reading", section, err)
			return
		}
		if doc == nil {
			doc = auth.DefaultAuthSectionDoc(section)
		}
		writeJSONRaw(w, http.StatusOK, doc)
	}
}

// handleAuthConfigPut installs one section (verify-then-replace; a refusal
// leaves the live configuration in force).
func (s *Server) handleAuthConfigPut(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.AuthConfigs == nil {
			writeError(w, http.StatusServiceUnavailable, "auth configuration plane is not available on this instance")
			return
		}
		body, ok := s.readAuthConfigBody(w, r)
		if !ok {
			return
		}
		echo, changed, err := s.deps.AuthConfigs.PutAuthSection(r.Context(), section, body, actorName(r))
		if err != nil {
			s.writeAuthConfigError(w, r, "updating", section, err)
			return
		}
		s.audit.Record(r.Context(), audit.Event{
			Actor:  actorName(r),
			Action: auditActionAuthConfigUpdate,
			Detail: authConfigUpdateDetail(section, changed),
		})
		s.log.InfoContext(r.Context(), "httpapi: auth config section updated",
			"section", section, "actor", actorName(r))
		writeJSONRaw(w, http.StatusOK, echo)
	}
}

// handleAuthConfigTest runs one test connection against the candidate (or
// stored) section.
func (s *Server) handleAuthConfigTest(section string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.deps.AuthConfigs == nil {
			writeError(w, http.StatusServiceUnavailable, "auth configuration plane is not available on this instance")
			return
		}
		body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, authConfigMaxBodyBytes))
		report, err := s.deps.AuthConfigs.TestAuthSection(r.Context(), section, body)
		if err != nil {
			s.writeAuthConfigError(w, r, "testing", section, err)
			return
		}
		s.audit.Record(r.Context(), audit.Event{
			Actor:  actorName(r),
			Action: auditActionAuthConfigTest,
			Detail: authConfigTestDetail(section, report),
		})
		status := http.StatusOK
		if !report.OK {
			status = http.StatusBadRequest
		}
		writeJSONBody(w, status, report)
	}
}

// readAuthConfigBody drains the (bounded) section payload.
func (s *Server) readAuthConfigBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, authConfigMaxBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "auth config body could not be read (max 1 MiB)")
		return nil, false
	}
	return body, true
}

// writeAuthConfigError maps the manager's error families onto the errors[]
// envelope: strict-schema/validation/discovery refusals answer 400 with the
// anchored or diagnostic message; store busy answers 503; anything else is
// the honest 500.
func (s *Server) writeAuthConfigError(w http.ResponseWriter, r *http.Request, verb, section string, err error) {
	switch {
	case errors.Is(err, auth.ErrUnknownAuthConfigField),
		errors.Is(err, auth.ErrSectionUnknown),
		errors.Is(err, auth.ErrNoMasterKey),
		errors.Is(err, auth.ErrTestCredsIncomplete):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		// Validation and discovery refusals arrive as plain wrapped errors
		// from ValidateAuthSection / buildOIDCProvider — the whole
		// "auth config:" family is a 400 (a candidate the server refuses,
		// never a server fault).
		if isAuthConfigRefusal(err) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "auth config store is busy, retry")
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: auth config plane failure",
			"verb", verb, "section", section, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "auth config "+verb+" failed")
	}
}

// isAuthConfigRefusal recognizes the validation family by its message
// prefix (the section validators speak "auth config:").
func isAuthConfigRefusal(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "auth config:")
}

// authConfigUpdateDetail builds the auth.config.update audit payload:
// section + changed-key NAMES — values never travel (92.6 / ADR-0035
// decision 5: sensitive zero-persistence, and non-sensitive values are not
// the trail's business either).
func authConfigUpdateDetail(section string, changed []string) string {
	d := map[string]any{
		"section": section,
		"changed": changed,
		"values":  "redacted",
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// authConfigTestDetail builds the auth.config.test payload: result category
// words only.
func authConfigTestDetail(section string, report auth.TestReport) string {
	d := map[string]string{
		"section": section,
		"result":  report.Category,
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// writeJSONRaw writes a pre-rendered JSON document.
func writeJSONRaw(w http.ResponseWriter, status int, doc json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(append([]byte{}, doc...), '\n'))
}
