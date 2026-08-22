package npm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Session endpoints (NE-06/NE-07): ping, whoami and the couch-style login.
//
// Login credential resolution mirrors the docker /v2/token form-credential
// ruling (T-37): the middleware settles the Authorization HEADER; the couch
// login body carries name/password when the client holds no header, and that
// pair carries the same weight as Basic. A PRESENT-but-wrong body credential
// is a 401, never a silent anonymous acceptance.
//
// Stack note (T-63 review N4 follow-up): through the full httpapi assembly
// a PUT is credential-gated BEFORE the adapter runs, so an anonymous login
// PUT meets the standard 401 Basic challenge at the gate; the couch branch
// below serves stacks that reach the handler without that gate (direct
// mounts, future route carve-outs) and the authenticated flows (`.npmrc`
// `_auth` + `npm login`, the PRD M22c equivalent-credential posture).

// loginTokenTTL is the npm login token lifetime: the auth module's default
// (720h) kept local so the npm plane could diverge without touching the
// shared config.
const loginTokenTTL = 720 * time.Hour

// loginBodyMax bounds the couch user document.
const loginBodyMax = 1 << 20

// servePing answers GET /-/ping: 200 with an empty JSON object (NE-07).
func (h *Handler) servePing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "ping accepts GET and HEAD only")
		return
	}
	writeJSONBody(w, http.StatusOK, map[string]any{})
}

// serveWhoami answers GET /-/whoami: the authenticated username, 401 for
// anonymous (npm's whoami sends Basic when configured — with `_auth` in
// .npmrc the CLI answers from config without a roundtrip, so this endpoint
// mostly serves token-authenticated clients).
func (h *Handler) serveWhoami(w http.ResponseWriter, r *http.Request, p *Principal) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeError(w, http.StatusMethodNotAllowed, "whoami accepts GET only")
		return
	}
	if p == nil {
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"username": p.Name})
}

// couchLoginBody is the npm legacy login document.
type couchLoginBody struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Email    string `json:"email"`
}

// serveLogin answers PUT /-/user/org.couchdb.user:<name>: verify the
// credential (header principal, else the body pair), mint one TokenRegistry
// token for the account and answer 201 with it (NE-06; the /v2/token
// dual-entry precedent — same table, same revocation chain).
func (h *Handler) serveLogin(ctx context.Context, w http.ResponseWriter, r *http.Request, p *Principal, userID string) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		writeError(w, http.StatusMethodNotAllowed, "the couch user endpoint accepts PUT only")
		return
	}
	name := strings.TrimPrefix(userID, couchUserPrefix)

	var body couchLoginBody
	if raw, err := io.ReadAll(io.LimitReader(r.Body, loginBodyMax)); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}

	subject := ""
	switch {
	case p != nil:
		// Header-authenticated: the principal IS the login. A body naming a
		// DIFFERENT account is refused (minting tokens for someone else is
		// not a login).
		if body.Name != "" && body.Name != p.Name {
			writeError(w, http.StatusForbidden, "authenticated principal cannot log in as another user")
			return
		}
		subject = p.Name
	case body.Name != "" && body.Password != "":
		// T-204 (T-192 leftover 2): the argon2id comparison runs through
		// the verifier seam — the auth service's concurrency gate with
		// request-scoped cancellation — instead of the pure package
		// function. A verifier error (the client hung up while queued for
		// a slot) folds into the same uniform 401: the response lands on a
		// socket nobody is reading, and the body must not distinguish it
		// anyway. A nil verifier (assembly without the identity service)
		// fails closed.
		u, err := h.lookupUser(ctx, body.Name)
		ok := err == nil && u != nil && u.Enabled && h.verifier != nil
		if ok {
			var verr error
			ok, verr = h.verifier.VerifyPassword(ctx, body.Password, u.PasswordHash)
			ok = verr == nil && ok
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		subject = u.Username
	default:
		w.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if subject == "" {
		subject = name
	}

	if h.tokens == nil {
		writeError(w, http.StatusServiceUnavailable, "token issuance is not configured on this instance")
		return
	}
	tok, err := h.tokens.Issue(ctx, subject, loginTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token issuance failed: "+err.Error())
		return
	}
	writeJSONBody(w, http.StatusCreated, map[string]any{
		"ok":    true,
		"id":    couchUserPrefix + subject,
		"rev":   "1-" + subject,
		"token": tok.AccessToken,
	})
}

// lookupUser resolves one account through the user-directory seam.
func (h *Handler) lookupUser(ctx context.Context, name string) (*metadata.User, error) {
	if h.users == nil {
		return nil, errors.New("user directory not wired")
	}
	return h.users.Get(ctx, name)
}
