// OIDC login-flow endpoints (M6, ADR-0020, PRD FR-54 / OD-01/OD-02, T-157):
// the browser SSO pair GET /binflow/api/v1/oidc/login (302 to the IdP's
// authorization endpoint, Authorization Code Grant + PKCE S256) and GET
// /binflow/api/v1/oidc/callback (code exchange -> ID Token verification ->
// console session).
//
// The pair exists ONLY on instances whose assembly wired Deps.OIDC
// (oidc.enabled=true): a disabled instance answers the E-26 404 for both
// routes so neither the endpoint nor the SSO posture is discoverable
// (FR-54-AC6/H29).
//
// Flow state (anti-CSRF `state` + PKCE `code_verifier`, ADR-0020 security
// clause) rides a short-lived HttpOnly cookie scoped to the two routes
// themselves — no server-side transaction store, so the flow survives
// restarts and needs no new table. The callback compares `state` in
// constant time; a mismatched or absent transaction is refused, never
// downgraded to a stateless exchange.

package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
)

// OIDCLoginFlow is the consumer-side seam for the browser login flow
// (defined here at the consumer, per project convention). *auth.OIDCProvider
// satisfies it: OAuth2Config drives both the authorization redirect
// (AuthCodeURL) and the code exchange (Exchange). ID Token VERIFICATION and
// the user mapping (resolve-or-auto-create, ADR-0020 decision 4) do not ride
// this seam — the callback funnels the token through the shared
// Authenticator's OIDC Bearer arm (oidcPrincipal below), so the login flow
// can never grow a second, drifting mapping implementation.
type OIDCLoginFlow interface {
	OAuth2Config() *oauth2.Config
}

const (
	// oidcTxCookieName carries the login-flow transaction (state + PKCE
	// verifier). "tx" = one in-flight authorization request, not a session.
	oidcTxCookieName = "binflow_oidc_tx" //nolint:gosec // cookie name, not a credential
	// oidcTxTTL bounds the whole browser round trip through the IdP: generous
	// for a human login, short enough that an abandoned flow's verifier dies.
	oidcTxTTL = 10 * time.Minute
	// oidcStateBytes is the anti-CSRF state entropy (256 bits; hex-encoded).
	oidcStateBytes = 32
	// oidcUIRedirect is the post-login landing segment (OD-02: the callback
	// ends with "302 /binflow/ui/", where the SPA's session guard takes
	// over). It is a fixed internal path — never client-supplied — so the
	// callback cannot be turned into an open redirector.
	oidcUIRedirect = prefix + "/ui/"
)

// handleOIDCLogin serves GET /binflow/api/v1/oidc/login (OD-01): mint a
// fresh state + PKCE pair, stash them in the transaction cookie, 302 to the
// IdP's authorization endpoint. The route is a browser navigation and is
// deliberately unauthenticated — the credential arrives later, inside the
// callback's authorization code.
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	if s.deps.OIDC == nil {
		// oidc.enabled=false: the endpoint does not exist (FR-54-AC6/H29).
		notImplemented(w, "/binflow/api/v1/oidc/login")
		return
	}
	if s.sessions == nil {
		// No session registry means the flow could never finish; fail now
		// instead of after a round trip through the IdP.
		writeError(w, http.StatusServiceUnavailable, "console sessions are not available on this instance")
		return
	}
	state, err := oidcState()
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: oidc state entropy failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "oidc login failed")
		return
	}
	verifier, challenge, err := auth.GeneratePKCEPair()
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: oidc pkce generation failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "oidc login failed")
		return
	}
	// Path is the oidc segment only: the cookie ships on the two flow routes
	// and nowhere else (same least-privilege rule as the session cookie's
	// /binflow path, NFR-S19). SameSite=Lax survives the IdP's top-level GET
	// redirect back to the callback.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124 cannot see the derived Secure attribute; see sessionCookieSecure
		Name:     oidcTxCookieName,
		Value:    state + "." + verifier,
		Path:     prefix + "/api/v1/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   sessionCookieSecure(r, s.deps.Config.Server.BaseURL),
		MaxAge:   int(oidcTxTTL.Seconds()),
	})
	authURL := s.deps.OIDC.OAuth2Config().AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", auth.PKCECodeChallengeMethod),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOIDCCallback serves GET /binflow/api/v1/oidc/callback (OD-02):
// verify the round-trip integrity (IdP error, state vs the transaction
// cookie, PKCE verifier), exchange the code at the token endpoint, resolve
// the ID Token through the shared OIDC Bearer arm, then mint the console
// session exactly like a password login and land the browser on /binflow/ui/.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.deps.OIDC == nil {
		notImplemented(w, "/binflow/api/v1/oidc/callback")
		return
	}
	// The transaction is single-use: clear the cookie on every exit path
	// (set before any writeError, which commits the header). A replayed
	// callback then fails the state check below.
	clearOIDCTxCookie(w, r, s.deps.Config.Server.BaseURL)

	fail := func(status int, msg string) {
		s.audit.Record(r.Context(), audit.Event{
			Actor: "oidc", Action: audit.ActionLoginFail, RemoteAddr: r.RemoteAddr,
		})
		writeError(w, status, msg)
	}

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		// The IdP refused (user denied consent, server error): surface the
		// error code, never the full description (it is upstream-controlled
		// text bound for an operator's logs, not a browser page).
		s.log.WarnContext(r.Context(), "httpapi: oidc provider returned an error",
			"error", e, "description", q.Get("error_description"))
		fail(http.StatusBadRequest, "oidc provider returned an error: "+e)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		fail(http.StatusBadRequest, "authorization code and state are required")
		return
	}
	c, err := r.Cookie(oidcTxCookieName)
	if err != nil || c.Value == "" {
		fail(http.StatusBadRequest, "oidc login transaction missing or expired; restart the login from the console")
		return
	}
	wantState, verifier, found := strings.Cut(c.Value, ".")
	if !found || wantState == "" || verifier == "" {
		fail(http.StatusBadRequest, "malformed oidc login transaction")
		return
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(wantState)) != 1 {
		// A state mismatch means the callback was not issued for this
		// browser's transaction (forged callback, cross-flow replay, or a
		// stale cookie). Log at warn: forgery attempts belong in the
		// operator's view; the response reveals nothing.
		s.log.WarnContext(r.Context(), "httpapi: oidc state mismatch on callback",
			"path", r.URL.EscapedPath(), "remote", r.RemoteAddr)
		fail(http.StatusBadRequest, "oidc state mismatch")
		return
	}

	tok, err := s.deps.OIDC.OAuth2Config().Exchange(r.Context(), code,
		oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: oidc code exchange failed", "error", err.Error())
		fail(http.StatusBadGateway, "authorization code exchange failed")
		return
	}
	idToken, _ := tok.Extra("id_token").(string)
	if idToken == "" {
		s.log.ErrorContext(r.Context(), "httpapi: oidc token response carried no id_token")
		fail(http.StatusBadGateway, "token response carried no id_token")
		return
	}

	p, err := s.oidcPrincipal(r, idToken)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// Signature/issuer/audience/expiry rejection or a disabled user:
			// uniform 401, the same wording as the password login.
			s.log.WarnContext(r.Context(), "httpapi: oidc id token rejected", "error", err.Error())
			fail(http.StatusUnauthorized, "invalid credentials")
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: oidc login failed", "error", err.Error())
		fail(http.StatusInternalServerError, "oidc login failed")
		return
	}

	if s.sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "console sessions are not available on this instance")
		return
	}
	ttl := s.deps.Config.Console.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour // unreachable via Load (Validate enforces > 0); hand-built configs only
	}
	sess, err := s.sessions.IssueSession(r.Context(), p.Name, ttl)
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: session issue failed",
			"user", p.Name, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor: p.Name, Action: audit.ActionLoginOK, RemoteAddr: r.RemoteAddr,
	})
	s.log.InfoContext(r.Context(), "httpapi: oidc session issued",
		"user", p.Name, "expires_at", sess.ExpiresAt.Format(time.RFC3339))
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124 cannot see the derived Secure attribute; see sessionCookieSecure below
		Name:     auth.CookieSessionName,
		Value:    sess.ID,
		Path:     sessionCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   sessionCookieSecure(r, s.deps.Config.Server.BaseURL),
		MaxAge:   int(ttl.Seconds()),
	})
	http.Redirect(w, r, oidcUIRedirect, http.StatusFound)
}

// oidcPrincipal resolves the exchanged ID Token into a Principal by
// presenting it on the shared Authenticator's Bearer arm: the OIDC arm
// (ADR-0020) verifies the token (JWKS signature, issuer, audience, expiry),
// maps the configured claims, resolves the local user row and auto-creates
// it on first login — one production mapping, consumed by both the CLI
// Bearer plane and this browser flow. The synthesized request carries ONLY
// the Authorization header: the callback's own cookies (already consumed by
// the state check) can never influence the decision.
func (s *Server) oidcPrincipal(r *http.Request, idToken string) (*auth.Principal, error) {
	authReq, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil { // unreachable: the URL is a constant
		return nil, fmt.Errorf("httpapi: building bearer request: %w", err)
	}
	authReq = authReq.WithContext(r.Context())
	authReq.Header.Set("Authorization", "Bearer "+idToken)
	p, err := s.deps.Auth.Authenticate(authReq.Context(), authReq)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("httpapi: oidc bearer arm returned no principal: %w", auth.ErrInvalidCredentials)
	}
	return p, nil
}

// oidcState mints the anti-CSRF state (oidcStateBytes crypto/rand bytes,
// hex). Like every credential-shaped value it never appears in logs.
func oidcState() (string, error) {
	buf := make([]byte, oidcStateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("httpapi: oidc state entropy: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// clearOIDCTxCookie expires the transaction cookie (epoch, same attributes
// so the overwrite matches the cookie it replaces).
func clearOIDCTxCookie(w http.ResponseWriter, r *http.Request, baseURL string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: deletion cookie; Secure mirrors the issuing posture
		Name:     oidcTxCookieName,
		Value:    "",
		Path:     prefix + "/api/v1/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   sessionCookieSecure(r, baseURL),
		MaxAge:   -1,
	})
}
