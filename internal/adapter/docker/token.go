package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// OAuth-form error codes for the token endpoint's PARAMETER refusals (the
// unsupported grant, the method gate — PRD FR-11's error-body split kept
// for the unobserved corners; the credential 401s answer Artifactory's
// generic {"errors":[{"status":...}]} model per L000-B C02, see
// writeStatusFormError).
const (
	oauthErrInvalidRequest      = "invalid_request"
	oauthErrUnsupportedGrant    = "unsupported_grant_type"
	oauthErrInvalidClient       = "invalid_client"
	oauthErrUnsupportedResponse = "unsupported_response_type"
)

// tokenResponse is the distribution token payload (DE-13): token and
// access_token carry the same value (clients read either), expires_in is
// seconds, issued_at is RFC3339, scope echoes the granted subset (narrowed
// per AC4). The Bearer value is INTENDED to cross the wire here — the
// distribution token protocol's whole payload — so the secret-naming rule
// does not apply to these fields.
type tokenResponse struct {
	Token        string `json:"token"`
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	IssuedAt     string `json:"issued_at"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"` // never set (Q3: no refresh)
}

// tokenRequest is the normalized token-endpoint request: query parameters
// for GET, form body (or query) for POST.
type tokenRequest struct {
	service   string
	account   string
	clientID  string
	scopes    []string
	grantType string
}

// parseTokenRequest reads the parameters from the query string and, for
// POST, the application/x-www-form-urlencoded body. Form values win over
// query values when both spell a parameter (RFC 6749 section 3.1 keeps the
// query for the authorization-code family; docker clients put everything in
// the query on GET and the body on POST — either way one spelling arrives).
//
// offline_token (D44-2): the official distribution token spec defines the
// parameter and lets the server "MAY ignore" it; docker 29's challenge-mode
// login sends offline_token=true on every GET. Accepted and IGNORED — no
// refresh_token is ever returned (Q3: no refresh), so the flag costs
// nothing to honor.
func parseTokenRequest(r *http.Request) tokenRequest {
	var req tokenRequest
	get := func(key string) string {
		if v := r.PostFormValue(key); v != "" {
			return v
		}
		return r.URL.Query().Get(key)
	}
	req.service = get("service")
	req.account = get("account")
	req.clientID = get("client_id")
	req.grantType = get("grant_type")
	if v := r.URL.Query()["scope"]; len(v) > 0 {
		req.scopes = append(req.scopes, v...)
	}
	if v := r.PostForm["scope"]; len(v) > 0 {
		req.scopes = append(req.scopes, v...)
	}
	return req
}

// serveToken implements GET/POST /v2/token (DE-13, ADR-0010 clause 4).
//
// Two entry designs live side by side (PRD FR-11 "双 token 入口并存"):
// /binflow/api/security/token is the admin-only management plane; THIS
// endpoint is the docker-login entry — any valid user (Basic credential,
// or a valid Bearer token presented as the identity) exchanges for their
// own pull/push-scoped token, because the docker client cannot be required
// to hold admin privileges just to pull.
//
// Flow: reject unsupported parameters (offline_token, refresh grants)
// BEFORE burning a credential check; authenticate via the middleware's
// decision (a rejected credential renders the OAuth 401 here — this route
// overrides the plane's default spec-body challenge because the client on
// this endpoint is mid-OAuth, not mid-registry); anonymous + anonymous
// access open issues a pull-only token for the requested scopes (nil
// principal authorizes nothing but reads when the flag is on); an
// authenticated principal gets the ACL-narrowed grant. The issued token is
// a plain TokenRegistry token: same table, same revocation chain (D23),
// scope narrowing is response-level only — enforcement stays per-request
// Authorizer.Can.
func (h *Handler) serveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		h.writeOAuthError(w, http.StatusMethodNotAllowed, oauthErrUnsupportedResponse,
			"token endpoint accepts GET and POST only")
		return
	}
	if r.Method == http.MethodPost {
		// ParseForm is bounded by net/http's default 10MB and only parses
		// for the form content type; a JSON or multipart body is ignored
		// (parameters then come from the query, the docker client's actual
		// spelling).
		_ = r.ParseForm()
	}
	if h.tokens == nil {
		// Assembly without a token registry: honest 503 rather than a panic.
		h.writeOAuthError(w, http.StatusServiceUnavailable, oauthErrInvalidRequest,
			"token issuance is not configured on this instance")
		return
	}
	req := parseTokenRequest(r)

	if req.grantType == "refresh_token" {
		h.writeOAuthError(w, http.StatusBadRequest, oauthErrUnsupportedGrant,
			"refresh_token grant is not supported (re-login instead)")
		return
	}

	// Credential resolution (D44-3): the middleware already settled the
	// Authorization HEADER (Basic/Bearer). When it settled nothing and the
	// POST body carries the OAuth form credential pair, that pair carries
	// the same weight as Basic — buildkit/helm push through this spelling
	// (grant_type=password), and ignoring it minted them a useless
	// anonymous token. A PRESENT-but-wrong form credential is a 401, never
	// a silent fallback to anonymous issuance.
	p := principalOf(r)
	if p == nil {
		if user, pass, ok := formCredentials(r); ok {
			fp, err := h.authenticateForm(r.Context(), user, pass)
			if err != nil {
				// A PRESENT-but-wrong form credential is the refused-
				// credential family: the Artifactory generic error model,
				// verbatim "Bad Credentials" (L000-B C02/E1-5).
				writeStatusFormError(w, http.StatusUnauthorized, "Bad Credentials")
				return
			}
			p = fp
		}
	}
	if p == nil && !h.opts.AnonymousAccess {
		// No credential (and anonymous closed): the Artifactory generic
		// error model with the exact observed message (L000-B E1-4:
		// {"errors":[{"status":401,"message":"Authentication is required"}]})
		// plus the Basic challenge — the one 401 whose client has nothing
		// to retry, so the challenge solicits credentials instead of
		// pointing back at the token endpoint it just came from (T-55 kept
		// this header untouched while the bodies unified).
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Registry"`)
		writeStatusFormError(w, http.StatusUnauthorized, "Authentication is required")
		return
	}

	scopes := parseScopes(req.scopes...)
	granted := h.narrowScopes(r.Context(), p, scopes)

	// Token subject: the authenticated principal, or the synthetic disabled
	// account for the anonymous path (the row needs a username; a REAL
	// account must never own publicly-obtainable tokens). Seeding is
	// idempotent and fails closed.
	subject := anonymousSubject
	if p != nil {
		subject = p.Name
	} else if err := h.seedAnonymousOnce(r.Context(), h.users); err != nil {
		h.log.ErrorContext(r.Context(), "docker: anonymous token subject unavailable",
			"error", err.Error())
		h.writeOAuthError(w, http.StatusInternalServerError, oauthErrInvalidRequest,
			"token issuance failed")
		return
	}

	ttl := h.tokenTTL()
	tok, err := h.tokens.Issue(r.Context(), subject, ttl)
	if err != nil {
		h.log.ErrorContext(r.Context(), "docker: token issue failed", "error", err.Error())
		h.writeOAuthError(w, http.StatusInternalServerError, oauthErrInvalidRequest,
			"token issuance failed")
		return
	}
	resp := tokenResponse{
		Token:       tok.AccessToken,
		AccessToken: tok.AccessToken,
		ExpiresIn:   int64(ttl.Seconds()),
		IssuedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	if len(granted) > 0 {
		resp.Scope = strings.Join(granted, " ")
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp) //nolint:gosec // G117: the token IS the response payload (DE-13); a secret that never crosses the wire is not a token
}

// formCredentials extracts the OAuth form-body credential pair of a POST
// token request: username+password (the grant_type=password spelling docker
// login, buildkit and helm use), or client_id+client_secret. Only FORM
// values qualify — the same secrets in the query string are ignored (they
// leak into logs and proxies; the spec carries them in the body).
func formCredentials(r *http.Request) (user, pass string, ok bool) {
	if r.Method != http.MethodPost {
		return "", "", false
	}
	if u, p := r.PostFormValue("username"), r.PostFormValue("password"); u != "" && p != "" {
		return u, p, true
	}
	if u, p := r.PostFormValue("client_id"), r.PostFormValue("client_secret"); u != "" && p != "" {
		return u, p, true
	}
	return "", "", false
}

// errFormCredential is the single failure of the form-credential path —
// unknown user, wrong password and disabled account all map onto it so the
// 401 body cannot distinguish them (FR-11-AC4's no-existence-leak rule,
// same as the Basic path).
var errFormCredential = errors.New("docker: form credential rejected")

// authenticateForm verifies one form credential pair against the user
// store. This is the token endpoint's own exchange — the ONE place an
// adapter handles credentials — because the middleware settles headers
// long before a form body is parseable. Password semantics follow the
// platform's argon2id verification; the Basic path's password-as-API-token
// duality is deliberately not duplicated here (docker/buildkit/helm send
// real passwords; token-authenticated automation uses the header).
//
// T-204 (T-192 leftover 2): the argon2id comparison runs through the
// verifier seam — the auth service's concurrency gate with request-scoped
// cancellation — instead of the pure package function. A verifier error
// (the client hung up while queued for a slot) folds into the same
// uniform rejection: the response lands on a socket nobody is reading,
// and the body must not distinguish it anyway (FR-11-AC4). A nil verifier
// (assembly without the identity service) fails closed like a nil user
// store.
func (h *Handler) authenticateForm(ctx context.Context, user, pass string) (*Principal, error) {
	if h.users == nil || h.verifier == nil {
		return nil, errFormCredential
	}
	u, err := h.users.Get(ctx, user)
	if err != nil || !u.Enabled {
		return nil, errFormCredential
	}
	ok, verr := h.verifier.VerifyPassword(ctx, pass, u.PasswordHash)
	if verr != nil || !ok {
		return nil, errFormCredential
	}
	// Role rides the principal since M7 (T-212 review handover 1: this
	// arm used to build Admin-only, folding a readonly_admin form login
	// down to a plain user — under-authorization, fail-closed, but the
	// auditor's global read went missing). Role and Admin stay consistent
	// because every users-row write maintains the mirror in one statement
	// (ADR-0026 decision 6); a hand-built row with a garbage role falls
	// back to the Admin flag inside EffectiveRole.
	return &Principal{Name: u.Username, Role: Role(u.Role), Admin: u.IsAdmin}, nil
}

// writeOAuthError renders the token endpoint's OAuth-form error body with
// the api-version header (every /v2 response carries it, DE-17). The
// WWW-Authenticate header — when a 401 wants one — is the CALLER's call:
// the two 401 flavors speak different challenges (T-55 kept both headers
// byte-identical while unifying the bodies).
func (h *Handler) writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set(HeaderAPIVersion, APIVersionValue)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oauthErrorBody{Error: code, ErrorDescription: description})
}

// renderTokenAuthFailure shapes a REFUSED credential (wrong password,
// unknown user, stale/revoked Bearer) that reached /v2/token: the Bearer
// challenge header stays byte-identical to the plane's default (the client
// still needs realm/service to retry, ADR-0010 clause 4), while the body
// is Artifactory's generic error model with its exact observed message —
// 401 {"errors":[{"status":401,"message":"Bad Credentials"}]}, the form
// the docker CLI renders as "unknown: Bad Credentials" (L000-B C02/E1-5,
// superseding the PRD v1.2/C3 OAuth-form ruling for the credential 401s;
// the parameter 400s below stay OAuth — unobserved client-facing corners
// keep their standing shape).
// L004-1: the Bearer arms message-split exactly like the ping face (live
// capture a_tok_expiredbearer.h answers "Token failed verification:
// expired" on this endpoint too); the challenge header keeps the T-55
// Bearer form — the live reference's Basic realm on this endpoint is
// reported for adjudication, not changed here.
func (h *Handler) renderTokenAuthFailure(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", h.bearerChallenge(r, ""))
	writeStatusFormError(w, http.StatusUnauthorized, h.bearerRefusalMessage(r.Context(), r))
}

// isTokenRoute reports whether path belongs to the token endpoint's route
// family (the same set serveNameRoute dispatches into serveToken).
func isTokenRoute(path string) bool {
	return path == TokenPath || strings.HasPrefix(path, TokenPath+"/")
}

// oauthErrorBody is the OAuth error shape of the token plane.
type oauthErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// tokenTTL resolves the token lifetime: the config default (720h), floor
// 1h. The floor keeps a misconfigured 0/negative TTL from minting
// non-expiring tokens on an endpoint whose whole security posture is
// "limited TTL" (AC1).
func (h *Handler) tokenTTL() time.Duration {
	ttl := h.opts.TokenTTL
	if ttl < time.Hour {
		ttl = time.Hour
	}
	return ttl
}
