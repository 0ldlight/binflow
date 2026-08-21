// Console session endpoints (PRD FR-23 CE-03..05, ADR-0014 as amended by
// the T-108 errata): the /api/v1/session verb triple — POST login, GET
// whoami, DELETE logout — plus the binflow_session cookie contract.
//
// The cookie is minted here and VERIFIED by the authenticator's third arm
// (auth.Service.Authenticate), so a session carries the same weight as Basic
// or token credentials on every plane: content paths, protocol adapters and
// the management surface all consume the one shared authentication chain.

package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// sessionRegistry is the consumer-side facet of the authenticator the login
// endpoints need (defined here at the consumer, per project convention).
// auth.Service implements it; assembly is a type assertion on Deps.Auth in
// New, so cmd's Deps list stays untouched.
type sessionRegistry interface {
	AuthenticateCredentials(ctx context.Context, username, password string) (*auth.Principal, error)
	IssueSession(ctx context.Context, username string, ttl time.Duration) (*auth.IssuedSession, error)
	RevokeSession(ctx context.Context, plaintext string) error
}

// sessionCookiePath scopes the cookie to the product prefix: the console and
// every authenticated API live under /binflow, and a wider path would ship
// the credential to paths that never read it (PRD FR-23 / NFR-S19).
const sessionCookiePath = "/binflow"

// sessionBody is the login request's normalized shape: JSON
// {"username","password"} and the form spelling both land here.
type sessionBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// sessionWhoami is the 200 body of GET /api/v1/session and POST login.
// Source is the identity provider that owns the credential (local/oidc/ldap,
// FR-56-AC1/H36; T-157 leftover 3 closed by T-179).
type sessionWhoami struct {
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
	Source   string `json:"source"`
}

// parseSessionBody accepts JSON and form login bodies (CE-03: "JSON/form
// 双形态"). Shape errors are 400s on the /api/v1 envelope.
func parseSessionBody(r *http.Request) (sessionBody, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if ct != "" && ct != "application/json" && ct != "application/x-www-form-urlencoded" {
		return sessionBody{}, errors.New("unsupported content type (form-urlencoded or JSON)")
	}
	// Read once (capped), decide the shape from the first byte — the same
	// sniff the token plane uses: a JSON object has no key=value pairs and
	// a form body never starts with '{'.
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return sessionBody{}, errors.New("read request body: " + err.Error())
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))

	if strings.HasPrefix(strings.TrimLeft(string(payload), " \t\r\n"), "{") {
		var body sessionBody
		if err := decodeJSONBodyOf(r, &body); err != nil {
			return sessionBody{}, err
		}
		return body, nil
	}
	vals, err := url.ParseQuery(string(payload))
	if err != nil {
		return sessionBody{}, errors.New("malformed form body")
	}
	return sessionBody{Username: vals.Get("username"), Password: vals.Get("password")}, nil
}

// handleSessionCreate serves POST /api/v1/session (CE-03): check credentials
// with the Basic arm's exact rules, mint a server-side session, answer
// {"username","admin"} plus the Set-Cookie. Wrong credentials are a uniform
// 401 whose wording reveals nothing about WHICH half was wrong (FR-23-AC5);
// both outcomes land in the audit log.
//
// B2 (T-91 security review): the entry carries its own Origin verdict.
// csrfGuard cannot cover this route — a login request is anonymous (the
// credential is the body, not a cookie), and login-CSRF needs no victim
// cookie: a cross-site form posts the ATTACKER's credentials and the
// response's Set-Cookie lands in the victim's browser (SameSite governs
// sending, never writing). Same-origin/no-Origin requests pass — curl and
// CI are untouched.
func (s *Server) handleSessionCreate(w http.ResponseWriter, r *http.Request) {
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && !sameOrigin(r, origin) {
		s.log.WarnContext(r.Context(), "httpapi: cross-origin login rejected",
			slog.String("path", r.URL.EscapedPath()),
			slog.String("origin", origin),
		)
		writeError(w, http.StatusForbidden,
			"cross-origin request rejected: session-cookie authentication requires a same-origin Origin")
		return
	}
	if s.sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "console sessions are not available on this instance")
		return
	}
	body, err := parseSessionBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Username == "" || body.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	p, err := s.sessions.AuthenticateCredentials(r.Context(), body.Username, body.Password)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			s.log.ErrorContext(r.Context(), "httpapi: login check failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "login failed")
			return
		}
		// Uniform wording: unknown user, wrong password, disabled account
		// and token-principal mismatch are indistinguishable here.
		s.audit.Record(r.Context(), audit.Event{
			Actor: body.Username, Action: audit.ActionLoginFail, RemoteAddr: r.RemoteAddr,
		})
		writeError(w, http.StatusUnauthorized, "invalid credentials")
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
	// Auth metric (T-163): one login counted by provider source.
	if s.metrics != nil {
		s.metrics.countLogin(principalSource(p))
	}
	// Info-level structure, never the session value (NFR-S19).
	s.log.InfoContext(r.Context(), "httpapi: session issued",
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
	writeJSONBody(w, http.StatusOK, sessionWhoami{Username: p.Name, Admin: p.Admin, Source: principalSource(p)})
}

// handleSessionWhoami serves GET /api/v1/session (CE-04): the current
// principal of ANY arm (session, Basic, token — the route gate already
// demanded a credential). The SPA's route guard reads this; Source reports
// the owning provider (H36).
func (s *Server) handleSessionWhoami(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p == nil {
		// Defensive: the route gate (required) answers 401 before the
		// handler; reaching here anonymous means an assembly drift.
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSONBody(w, http.StatusOK, sessionWhoami{Username: p.Name, Admin: p.Admin, Source: principalSource(p)})
}

// handleSessionDelete serves DELETE /api/v1/session (CE-05): revoke the
// session server-side (a replayed cookie must fail with 401, W07) and clear
// the cookie browser-side. 204 in every success shape, including a logout
// that arrived on a non-session credential — there is nothing to revoke on
// those arms, and logout is idempotent by contract.
func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p != nil && p.ViaSession && s.sessions != nil {
		if c, err := r.Cookie(auth.CookieSessionName); err == nil && c.Value != "" {
			if err := s.sessions.RevokeSession(r.Context(), c.Value); err != nil {
				if !errors.Is(err, metadata.ErrWebSessionNotFound) {
					s.log.ErrorContext(r.Context(), "httpapi: session revoke failed",
						"user", p.Name, "error", err.Error())
					writeError(w, http.StatusInternalServerError, "logout failed")
					return
				}
			}
			s.log.InfoContext(r.Context(), "httpapi: session revoked", "user", p.Name)
		}
	}
	// Clear the cookie on the way out (epoch expiry; same attributes so the
	// overwrite matches the cookie it replaces).
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: deletion cookie; Secure mirrors the issuing posture below
		Name:     auth.CookieSessionName,
		Value:    "",
		Path:     sessionCookiePath,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   sessionCookieSecure(r, s.deps.Config.Server.BaseURL),
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// sessionCookieSecure decides the Secure attribute (PRD FR-23 R1 附注): an
// explicit server.base_url scheme wins (it is the operator's statement about
// the externally visible deployment — TLS may be terminated upstream);
// otherwise the request's own scheme decides (r.TLS or X-Forwarded-Proto).
// Plain HTTP never gets Secure, or the browser would drop the cookie and
// the console would look broken.
func sessionCookieSecure(r *http.Request, baseURL string) bool {
	if baseURL != "" {
		if u, err := url.Parse(baseURL); err == nil && u.Scheme != "" {
			return u.Scheme == "https"
		}
	}
	return requestScheme(r) == "https"
}

// noopRecorder is the audit fallback for assemblies without a metadata
// store (unit-test stacks): login events drop rather than panic. Production
// always injects Metadata, so this recorder never runs in the served
// binary.
type noopRecorder struct{}

func (noopRecorder) Record(context.Context, audit.Event) {}
