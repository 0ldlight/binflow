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
	// oidcPurposeStepUp is the login-flow purpose that turns the pair into a
	// step-up re-authentication (M7, T-219, ADR-0027 decision 4): the
	// authorize URL forces prompt=login and the callback answers with a
	// single-use mint grant instead of a session. The grant rides the
	// redirect's FRAGMENT — fragments never reach a server or proxy log,
	// and the console (T-218/T-225) reads it before the mint POST.
	oidcPurposeStepUp   = "step_up"
	stepUpGrantFragment = "#step_up_grant="
)

// handleOIDCLogin serves GET /binflow/api/v1/oidc/login (OD-01): mint a
// fresh state + PKCE pair, stash them in the transaction cookie, 302 to the
// IdP's authorization endpoint. The route is a browser navigation and is
// deliberately unauthenticated — the credential arrives later, inside the
// callback's authorization code.
//
// M7 (T-219, ADR-0027 decision 4): ?purpose=step_up re-shapes the flow into
// a re-authentication — the authorize URL forces prompt=login (the OIDC Core
// standard "ask again" parameter, not a home-grown protocol) and the purpose
// rides the transaction cookie so the callback branch cannot be forged from
// the outside. A step-up init without an ACTIVE console session is refused
// here, before the IdP round trip: the flow's payout is bound to that
// session, so a sessionless run could never finish.
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
	purpose := r.URL.Query().Get("purpose")
	if purpose != "" && purpose != oidcPurposeStepUp {
		writeError(w, http.StatusBadRequest, "unknown login purpose: "+purpose)
		return
	}
	if purpose == oidcPurposeStepUp {
		p := principalFrom(r.Context())
		if p == nil || !p.ViaSession {
			writeError(w, http.StatusUnauthorized,
				"step-up re-authentication requires an active console session")
			return
		}
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
	// redirect back to the callback. The step-up purpose rides the value's
	// third dot-separated field (empty for a plain login) — the callback
	// reads it back from the same HttpOnly cookie, never from the request.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124 cannot see the derived Secure attribute; see sessionCookieSecure
		Name:     oidcTxCookieName,
		Value:    state + "." + verifier + "." + purpose,
		Path:     prefix + "/api/v1/oidc",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   sessionCookieSecure(r, s.deps.Config.Server.BaseURL),
		MaxAge:   int(oidcTxTTL.Seconds()),
	})
	authParams := []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", auth.PKCECodeChallengeMethod),
	}
	if purpose == oidcPurposeStepUp {
		// OIDC Core standard parameter: force a fresh authentication even
		// though the IdP still holds an SSO session — the whole point of
		// the step-up leg (ADR-0027: prompt=login, not a freshness window).
		authParams = append(authParams, oauth2.SetAuthURLParam("prompt", "login"))
	}
	http.Redirect(w, r, s.deps.OIDC.OAuth2Config().AuthCodeURL(state, authParams...), http.StatusFound)
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

	fail := func(status int, msg, reason string) {
		// T-187 / T-174 D7+D8: the OIDC arm's rejections land under the
		// PRD action name with the arm and reason in the Detail payload.
		s.audit.Record(r.Context(), audit.Event{
			Actor: "oidc", Action: audit.ActionAuthFail, RemoteAddr: r.RemoteAddr,
			Detail: audit.AuthEventDetail(string(auth.ProviderOIDC), reason),
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
		fail(http.StatusBadRequest, "oidc provider returned an error: "+e, auth.ReasonProviderError)
		return
	}
	code, state := q.Get("code"), q.Get("state")
	if code == "" || state == "" {
		fail(http.StatusBadRequest, "authorization code and state are required", auth.ReasonBadRequest)
		return
	}
	c, err := r.Cookie(oidcTxCookieName)
	if err != nil || c.Value == "" {
		fail(http.StatusBadRequest, "oidc login transaction missing or expired; restart the login from the console",
			auth.ReasonBadRequest)
		return
	}
	// The transaction value is state.verifier[.purpose]: the first two are
	// hex (dot-free by construction), the optional third names a purpose
	// only this server wrote (T-219) — a plain login's cookie has no third
	// field, which the empty purpose spells.
	wantState, rest, found := strings.Cut(c.Value, ".")
	verifier, purpose, vFound := strings.Cut(rest, ".")
	if !found || !vFound || wantState == "" || verifier == "" {
		// Pre-M7 cookies (state.verifier, one dot) are not valid here either:
		// the transaction is single-use and bounded by oidcTxTTL, so by the
		// time this code ships no live cookie predates the three-field form.
		fail(http.StatusBadRequest, "malformed oidc login transaction", auth.ReasonBadRequest)
		return
	}
	if purpose != "" && purpose != oidcPurposeStepUp {
		fail(http.StatusBadRequest, "malformed oidc login transaction", auth.ReasonBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(wantState)) != 1 {
		// A state mismatch means the callback was not issued for this
		// browser's transaction (forged callback, cross-flow replay, or a
		// stale cookie). Log at warn: forgery attempts belong in the
		// operator's view; the response reveals nothing.
		s.log.WarnContext(r.Context(), "httpapi: oidc state mismatch on callback",
			"path", r.URL.EscapedPath(), "remote", r.RemoteAddr)
		fail(http.StatusBadRequest, "oidc state mismatch", auth.ReasonBadCredentials)
		return
	}

	tok, err := s.deps.OIDC.OAuth2Config().Exchange(r.Context(), code,
		oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: oidc code exchange failed", "error", err.Error())
		fail(http.StatusBadGateway, "authorization code exchange failed",
			auth.ProviderFailureReason(err))
		return
	}
	idToken, _ := tok.Extra("id_token").(string)
	if idToken == "" {
		s.log.ErrorContext(r.Context(), "httpapi: oidc token response carried no id_token")
		fail(http.StatusBadGateway, "token response carried no id_token", auth.ReasonProviderError)
		return
	}

	p, err := s.oidcPrincipal(r, idToken)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// Signature/issuer/audience/expiry rejection or a disabled user:
			// uniform 401, the same wording as the password login.
			s.log.WarnContext(r.Context(), "httpapi: oidc id token rejected", "error", err.Error())
			_, reason := loginFailureClass(err)
			fail(http.StatusUnauthorized, "invalid credentials", reason)
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: oidc login failed", "error", err.Error())
		fail(http.StatusInternalServerError, "oidc login failed", auth.ReasonProviderError)
		return
	}

	if s.sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "console sessions are not available on this instance")
		return
	}
	// Step-up branch (M7, T-219, ADR-0027 decision 4): the purpose came from
	// this server's own HttpOnly transaction cookie, so it is trusted
	// routing, not a client claim. The re-authentication does NOT mint a
	// session — the caller already holds one (the premise of the flow) — it
	// pays out one single-use mint grant bound to that session's owner, and
	// lands the browser back on the console with the grant in the redirect
	// FRAGMENT (never a query parameter: fragments stay out of server and
	// proxy logs).
	if purpose == oidcPurposeStepUp {
		s.completeStepUpReauth(w, r, p)
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
		Detail: audit.AuthEventDetail(principalSource(p), ""),
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

// completeStepUpReauth finishes the purpose=step_up callback: reauth is the
// OIDC-Bearer-resolved principal of the fresh ID Token (prompt=login), sess
// the console session that initiated the flow. The grant is issued only when
// the re-authenticated identity IS the session's owner — a grant for user A
// re-authenticated as user B would hand B's second factor to A's mint. The
// tx cookie was already cleared on entry; every exit here keeps it that way.
func (s *Server) completeStepUpReauth(w http.ResponseWriter, r *http.Request, reauth *auth.Principal) {
	sess := principalFrom(r.Context())
	if sess == nil || !sess.ViaSession {
		s.audit.Record(r.Context(), audit.Event{
			Actor: "oidc", Action: audit.ActionAuthFail, RemoteAddr: r.RemoteAddr,
			Detail: audit.AuthEventDetail(string(auth.ProviderOIDC), auth.ReasonBadCredentials),
		})
		writeError(w, http.StatusUnauthorized,
			"step-up re-authentication requires an active console session")
		return
	}
	if reauth == nil || !strings.EqualFold(reauth.Name, sess.Name) {
		s.audit.Record(r.Context(), audit.Event{
			Actor: sess.Name, Action: audit.ActionAuthFail, RemoteAddr: r.RemoteAddr,
			Detail: audit.AuthEventDetail(string(auth.ProviderOIDC), auth.ReasonBadCredentials),
		})
		s.log.WarnContext(r.Context(), "httpapi: oidc step-up identity mismatch",
			"user", sess.Name)
		writeError(w, http.StatusUnauthorized,
			"the re-authenticated identity does not match the current session")
		return
	}
	if s.stepUp == nil {
		// The mint gate would fail closed without the facet; refuse the
		// payout here rather than handing the browser a dead grant.
		writeError(w, http.StatusServiceUnavailable, "step-up is not available on this instance")
		return
	}
	grant, err := s.stepUp.IssueStepUpGrant(r.Context(), sess.Name, sess.SessionHash,
		s.deps.Config.Auth.TokenStepUpGrantTTL)
	if err != nil {
		s.log.ErrorContext(r.Context(), "httpapi: step-up grant issue failed",
			"user", sess.Name, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "oidc step-up failed")
		return
	}
	s.log.InfoContext(r.Context(), "httpapi: oidc step-up grant issued",
		"user", sess.Name, "ttl", s.deps.Config.Auth.TokenStepUpGrantTTL.String())
	// The fragment carries the grant plaintext (its only appearance outside
	// the ledger — NFR-S2); the console reads it and posts it as
	// step_up_grant. Single-use, session-bound and TTL-bounded by the ledger.
	http.Redirect(w, r, oidcUIRedirect+stepUpGrantFragment+grant, http.StatusFound)
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
