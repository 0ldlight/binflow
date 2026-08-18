package docker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// OAuth-form error codes for the token endpoint (the /v2/token plane renders
// OAuth-shaped errors, NOT the registry spec body — PRD FR-11's error-body
// split: the token endpoint negotiates OAuth, every other /v2 route speaks
// the distribution error schema).
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
	service      string
	account      string
	clientID     string
	scopes       []string
	offlineToken bool
	grantType    string
}

// parseTokenRequest reads the parameters from the query string and, for
// POST, the application/x-www-form-urlencoded body. Form values win over
// query values when both spell a parameter (RFC 6749 section 3.1 keeps the
// query for the authorization-code family; docker clients put everything in
// the query on GET and the body on POST — either way one spelling arrives).
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
	req.offlineToken = isTruthy(get("offline_token"))
	req.grantType = get("grant_type")
	if v := r.URL.Query()["scope"]; len(v) > 0 {
		req.scopes = append(req.scopes, v...)
	}
	if v := r.PostForm["scope"]; len(v) > 0 {
		req.scopes = append(req.scopes, v...)
	}
	return req
}

// isTruthy interprets a form boolean (offline_token=true/1/yes/on).
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
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

	if req.offlineToken {
		h.writeOAuthError(w, http.StatusBadRequest, oauthErrInvalidRequest,
			"offline_token is not supported")
		return
	}
	if req.grantType == "refresh_token" {
		h.writeOAuthError(w, http.StatusBadRequest, oauthErrUnsupportedGrant,
			"refresh_token grant is not supported (re-login instead)")
		return
	}

	p := adapter.PrincipalFrom(r.Context())
	if p == nil && !h.opts.AnonymousAccess {
		// No credential (and anonymous closed): OAuth-form 401 with the
		// Basic challenge — this is the one 401 whose client has nothing to
		// retry, so the challenge solicits credentials instead of pointing
		// back at the token endpoint it just came from (T-55 kept this
		// header untouched while the bodies unified).
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Registry"`)
		h.writeOAuthError(w, http.StatusUnauthorized, oauthErrInvalidClient,
			"authentication required")
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
// still needs realm/service to retry, ADR-0010 clause 4), while the body is
// the PRD v1.2/C3 ruling's unified OAuth form — /v2/token's clients speak
// OAuth, not the registry error schema, and its 400s were already OAuth.
func (h *Handler) renderTokenAuthFailure(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="%s",service="%s"`, h.realmBase(r)+TokenPath, ServiceID))
	h.writeOAuthError(w, http.StatusUnauthorized, oauthErrInvalidClient, "authentication required")
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
