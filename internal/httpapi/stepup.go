// The token-mint step-up gate (M7, FR-68 / ADR-0027): the second credential
// POST /api/security/token demands from a non-admin WEB-SESSION caller when
// auth.token_step_up is on. This file owns the consumer-side facet of the
// auth service, the trigger condition and the two 401 error bodies; the leg
// selection (password vs mint grant) follows the session owner's provider,
// and the verification itself lives in internal/auth/stepup.go.
//
// Exempt arms (ADR-0027 decision 1): admin sessions, Basic, Bearer/API-key
// (first factor already fresh), /v2/token (Basic-backed by construction) and
// anonymous (the route's 401 challenge answered before the handler runs).

package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
)

// stepUpRegistry is the auth service's step-up facet, discovered by type
// assertion on Deps.Auth (the same consumer-side pattern as the session and
// permission-view facets). Unit fakes without the facet fail CLOSED: the
// gate answers step_up_required rather than minting past a missing decision
// point.
type stepUpRegistry interface {
	// IssueStepUpGrant mints one single-use grant bound to
	// {username, sessionHash} (the OIDC callback's re-auth payout).
	IssueStepUpGrant(ctx context.Context, username, sessionHash string, ttl time.Duration) (string, error)
	// ConsumeStepUpGrant settles one mint attempt (single-use: the grant
	// burns whether or not the binding matched).
	ConsumeStepUpGrant(grant, username, sessionHash string) bool
	// VerifyStepUpPassword settles the password leg (local argon2 verify /
	// LDAP re-bind; every failure wraps auth.ErrStepUpInvalid).
	VerifyStepUpPassword(ctx context.Context, username, password string) error
}

// The OAuth-form error spellings of the step-up plane (ADR-0027 decision 5,
// verbatim): the token family already answers this body shape, so its
// clients parse every non-2xx uniformly.
const (
	stepUpRequiredCode = "step_up_required"
	stepUpRequiredDesc = "step-up authentication required to mint a token"
	stepUpInvalidCode  = "step_up_invalid"
	stepUpInvalidDesc  = "step-up credential rejected, expired, or already used"

	// stepUpMethodPassword and stepUpMethodOIDC are the token.issue audit
	// dimension's closed vocabulary (ADR-0027 decision 7): password covers
	// BOTH the local and the LDAP leg (one body field), oidc_reauth the
	// mint-grant leg.
	stepUpMethodPassword = "password"
	stepUpMethodOIDC     = "oidc_reauth"
)

// stepUpTriggered decides whether the mint request owes a second credential
// (ADR-0027 decision 1): the arm is the web-session cookie, the caller is
// not admin (adminMinter is the same CapSecurityWrite capability decision
// the Q11 guardrails below use — admin is exactly the role that holds it),
// and the operator armed the switch. Every other arm passes untouched.
func (s *Server) stepUpTriggered(p *auth.Principal, adminMinter bool) bool {
	return s.deps.Config.Auth.TokenStepUp && p != nil && p.ViaSession && !adminMinter
}

// requireStepUp settles the second credential of one mint request. It
// renders the 401 itself and returns ok=false on refusal; on success it
// returns the audit method of the leg that satisfied the gate. The leg is
// the session owner's provider (Principal.Source on the session arm):
//
//   - local/ldap: body step_up_password — missing -> step_up_required,
//     anything that fails verification -> step_up_invalid;
//   - oidc: body step_up_grant — same split, the consumption binding being
//     {caller, this session}.
//
// A credential of the WRONG leg is ignored (the owed one is then missing ->
// step_up_required): the caller did not present what their leg demands.
func (s *Server) requireStepUp(w http.ResponseWriter, r *http.Request, p *auth.Principal, req tokenCreateForm) (method string, ok bool) {
	if s.stepUp == nil {
		// Fail closed (a config-on stack over a facet-less authenticator is
		// an assembly mistake; the mint refusal is the safe posture).
		s.log.ErrorContext(r.Context(), "httpapi: step-up demanded but the authenticator carries no step-up facet")
		writeOAuthError(w, http.StatusUnauthorized, stepUpRequiredCode, stepUpRequiredDesc)
		return "", false
	}
	switch auth.Provider(p.Source) {
	case auth.ProviderOIDC:
		if req.StepUpGrant == "" {
			writeOAuthError(w, http.StatusUnauthorized, stepUpRequiredCode, stepUpRequiredDesc)
			return "", false
		}
		if !s.stepUp.ConsumeStepUpGrant(req.StepUpGrant, p.Name, p.SessionHash) {
			s.log.WarnContext(r.Context(), "httpapi: step-up grant rejected",
				"user", p.Name, "path", r.URL.EscapedPath())
			writeOAuthError(w, http.StatusUnauthorized, stepUpInvalidCode, stepUpInvalidDesc)
			return "", false
		}
		return stepUpMethodOIDC, true
	default:
		// local and ldap (unknown provider spellings normalize to local on
		// the session arm, so the default IS the password leg).
		if req.StepUpPassword == "" {
			writeOAuthError(w, http.StatusUnauthorized, stepUpRequiredCode, stepUpRequiredDesc)
			return "", false
		}
		if err := s.stepUp.VerifyStepUpPassword(r.Context(), p.Name, req.StepUpPassword); err != nil {
			s.log.WarnContext(r.Context(), "httpapi: step-up password rejected",
				"user", p.Name, "error", err.Error())
			writeOAuthError(w, http.StatusUnauthorized, stepUpInvalidCode, stepUpInvalidDesc)
			return "", false
		}
		return stepUpMethodPassword, true
	}
}
