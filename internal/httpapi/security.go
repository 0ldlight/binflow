package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Artifactory-compatible security plane (auth-model.md; PRD E-16..E-19 with
// the v1.3 calibrated shapes). Error bodies follow the three-format split
// (PRD section 5.1): user management answers PLAIN TEXT, the token
// endpoints answer the OAuth-style {"error","error_description"} JSON.
//
// Routes and their auth gates live in router.go; the handlers below run only
// after authentication (and the admin gate where the route demands it).

// ---- change password (E-16) ----

// changePasswordOwn is the body of PUT /api/security/password (BinFlow's own
// spelling, PRD FR-5-AC3): the authenticated user rotates their password.
type changePasswordOwn struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// changePasswordAlias is the body of the real 7.x route
// POST /api/security/users/authorization/changePassword (auth-model.md
// section 2.1): userName plus the newPassword1/newPassword2 confirmation
// pair.
type changePasswordAlias struct {
	UserName     string `json:"userName"`
	OldPassword  string `json:"oldPassword"`
	NewPassword1 string `json:"newPassword1"`
	NewPassword2 string `json:"newPassword2"`
}

// handleChangePasswordOwn serves the BinFlow-owned route: the target is
// always the authenticated principal.
func (s *Server) handleChangePasswordOwn(w http.ResponseWriter, r *http.Request) {
	var body changePasswordOwn
	if err := decodeJSONBody(w, r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := principalFrom(r.Context())
	if err := s.deps.Passwords.ChangePassword(r.Context(), p.Name, body.OldPassword, body.NewPassword); err != nil {
		writePlainError(w, http.StatusBadRequest, changePasswordMessage(err))
		return
	}
	writeText(w, http.StatusOK, "Password has been successfully changed")
}

// handleChangePasswordAlias serves the real-route alias: userName defaults
// to the caller; only admin may target another user (auth-model.md 2.1).
// newPassword1 != newPassword2 -> 400 "New passwords do not match".
func (s *Server) handleChangePasswordAlias(w http.ResponseWriter, r *http.Request) {
	var body changePasswordAlias
	if err := decodeJSONBody(w, r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := principalFrom(r.Context())
	target := body.UserName
	if target == "" {
		target = p.Name
	} else if !strings.EqualFold(target, p.Name) && !p.Admin {
		writePlainError(w, http.StatusForbidden, "only administrators may change another user's password")
		return
	}
	if body.NewPassword1 != body.NewPassword2 {
		writePlainError(w, http.StatusBadRequest, "New passwords do not match")
		return
	}
	if err := s.deps.Passwords.ChangePassword(r.Context(), target, body.OldPassword, body.NewPassword1); err != nil {
		writePlainError(w, http.StatusBadRequest, changePasswordMessage(err))
		return
	}
	writeText(w, http.StatusOK, "Password has been successfully changed")
}

// changePasswordMessage maps auth.PasswordChanger sentinels onto the spec's
// plain-text wordings (auth-model.md section 2.2). Everything here is a 400:
// the caller is already inside the authenticated context.
func changePasswordMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "Incorrect username/password"
	case errors.Is(err, auth.ErrEmptyPassword):
		return "New passwords cannot be empty"
	case errors.Is(err, auth.ErrSamePassword):
		return "New password has to be different from the old one"
	}
	return err.Error()
}

// decodeJSONBody parses one JSON request body (change-password plane),
// capping its size. The ResponseWriter is only used for the read cap.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) error {
	if err := decodeJSONBodyOf(r, v); err != nil {
		return err
	}
	// keep the writer referenced for future non-JSON variants without
	// changing the call sites' shape
	_ = w
	return nil
}

// decodeJSONBodyOf parses one JSON request body strictly (unknown trailing
// content rejected), capping the read at 1 MiB.
func decodeJSONBodyOf(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("request body is not valid JSON: %w", err)
	}
	return nil
}

// ---- token create / revoke (E-17/E-18) ----

// tokenCreateResponse is the calibrated create body (auth-model.md 3.1 plus
// the BinFlow superset field token_id, PRD v1.3): access_token, token_type
// "Bearer", expires_in (omitted when 0 = never), scope, token_id.
type tokenCreateResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    *int64 `json:"expires_in,omitempty"`
	Scope        string `json:"scope"`
	TokenID      int64  `json:"token_id"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// oauthError is the token-plane error body (auth-model.md section 3.1):
// {"error": "<code>", "error_description": "<msg>"}.
type oauthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// writeOAuthError emits the OAuth-style error with its HTTP status.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	body, err := json.Marshal(oauthError{Error: code, ErrorDescription: description})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render token error: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// tokenCreateForm is the normalized create request: the real endpoint speaks
// form-urlencoded; BinFlow additionally accepts the JSON projection with the
// same field names (PRD E-17, BinFlow extension).
type tokenCreateForm struct {
	GrantType   string
	Username    string
	Scope       string
	ExpiresIn   *int64
	Refreshable bool
	Audience    string
}

// tokenCreateJSON is the JSON body shape (identical semantics, JSON names).
type tokenCreateJSON struct {
	GrantType   string `json:"grant_type"`
	Username    string `json:"username"`
	Scope       string `json:"scope"`
	ExpiresIn   *int64 `json:"expires_in"`
	Refreshable bool   `json:"refreshable"`
	Audience    string `json:"audience"`
}

// parseTokenCreateRequest accepts both content types and normalizes onto
// tokenCreateForm. The JSON probe runs whenever the form decode produced an
// all-empty struct: net/http only parses the body as a form when the
// Content-Type names the form media type, so a body sent without one (curl
// -d does this) must be sniffed — a JSON object has no key=value pairs, and
// a form body cannot start with '{'.
func parseTokenCreateRequest(r *http.Request) (tokenCreateForm, error) {
	var form tokenCreateForm
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))

	if ct == "application/json" {
		return decodeTokenJSON(r)
	}
	if ct != "" && ct != "application/x-www-form-urlencoded" {
		return form, fmt.Errorf("unsupported content type %q (form-urlencoded or JSON)", ct)
	}

	// Read the body once and decide its shape from its first non-space byte.
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return form, fmt.Errorf("read request body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))

	trimmed := strings.TrimLeft(string(payload), " \t\r\n")
	if strings.HasPrefix(trimmed, "{") {
		return decodeTokenJSON(r)
	}
	vals, err := url.ParseQuery(string(payload))
	if err != nil {
		return form, fmt.Errorf("malformed form body: %w", err)
	}
	form.GrantType = vals.Get("grant_type")
	form.Username = vals.Get("username")
	form.Scope = vals.Get("scope")
	form.Audience = vals.Get("audience")
	form.Refreshable = isFormTrue(vals.Get("refreshable"))
	if v := vals.Get("expires_in"); v != "" {
		n, err := parseFormInt(v)
		if err != nil {
			return form, err
		}
		form.ExpiresIn = &n
	}
	return form, nil
}

// decodeTokenJSON decodes the JSON projection of the create request.
func decodeTokenJSON(r *http.Request) (tokenCreateForm, error) {
	var body tokenCreateJSON
	if err := decodeJSONBodyOf(r, &body); err != nil {
		return tokenCreateForm{}, err
	}
	return tokenCreateForm(body), nil
}

// isFormTrue interprets a form boolean.
func isFormTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// parseFormInt parses one integer form value.
func parseFormInt(v string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid expires_in value: %s", v)
	}
	return n, nil
}

// tokenIssueDetail is the JSON payload of the token.issue audit event
// (G31a/T-133, extended by T-190): fingerprint + TTL as before, plus the
// token subject (actor != subject when an admin mints on behalf) and the
// authentication arm of the minting caller (local/oidc/ldap, Q11 guardrail 4).
type tokenIssueDetail struct {
	Fingerprint string `json:"fingerprint"`
	TTLSeconds  int64  `json:"ttl_seconds"`
	Subject     string `json:"subject"`
	Source      string `json:"source"`
}

// handleTokenCreate serves POST /api/security/token (E-17). Permission model
// per the T-188 ruling (PRD M6 v1.2 Q11, aligned with auth-model.md 3.1):
// admin is unrestricted (may name any subject, may mint never-expiring
// tokens); a non-admin authenticated caller — local, OIDC or LDAP arm alike —
// may mint only for THEMSELVES (username naming anyone else -> 403) and only
// with a finite TTL (0 < effective ttl <= auth.token_nonadmin_max_ttl, K9;
// 0/over-cap -> 401 invalid_request with the Artifactory wording). Anonymous
// callers never reach the handler (the route gate answers 401).
func (s *Server) handleTokenCreate(w http.ResponseWriter, r *http.Request) {
	req, err := parseTokenCreateRequest(r)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.GrantType == "" {
		req.GrantType = "client_credentials"
	}
	if req.GrantType != "client_credentials" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type",
			"Grant type is not supported: "+req.GrantType)
		return
	}
	if req.Scope != "" && !validScope(req.Scope) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope",
			"scope is malformed: "+req.Scope)
		return
	}
	if req.ExpiresIn != nil && *req.ExpiresIn < 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			fmt.Sprintf("Invalid expires_in value: %d", *req.ExpiresIn))
		return
	}
	if req.Refreshable && (req.ExpiresIn == nil || *req.ExpiresIn == 0) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request",
			"Only refreshable tokens with expiry can have custom audience")
		return
	}

	p := principalFrom(r.Context())
	subject := p.Name
	if req.Username != "" && !strings.EqualFold(req.Username, p.Name) {
		if !p.Admin {
			// 403 OAuth form, the same wording the admin route gate uses
			// (Q11/K9: keep the established "administrator privileges
			// required" surface instead of a novel message).
			writeOAuthError(w, http.StatusForbidden, "invalid_request",
				"administrator privileges required")
			return
		}
		subject = req.Username
	}

	ttl := s.deps.Config.Auth.TokenDefaultTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	var expiresIn int64
	if req.ExpiresIn != nil && *req.ExpiresIn == 0 {
		ttl = 0 // never expires (auth-model.md 3.5)
	} else if req.ExpiresIn != nil {
		ttl = time.Duration(*req.ExpiresIn) * time.Second
	}
	if !p.Admin {
		// Q11 guardrail 2: a finite TTL bounded by the configured cap. The
		// check applies to the EFFECTIVE lifetime (an absent expires_in
		// inherits the default TTL, which the cap then also bounds), so an
		// operator cannot widen the self-service exposure by raising the
		// default alone. Wording per auth-model.md 3.1.
		maxTTL := s.deps.Config.Auth.TokenNonAdminMaxTTL
		if maxTTL <= 0 {
			maxTTL = 365 * 24 * time.Hour // validated positive at boot; belt-and-braces
		}
		if ttl <= 0 || ttl > maxTTL {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_request",
				fmt.Sprintf("The user: '%s' can only create user token with expires in larger than 0 and smaller than %d seconds (requested: %d)",
					p.Name, int64(maxTTL.Seconds()), int64(ttl.Seconds())))
			return
		}
	}
	if ttl > 0 {
		expiresIn = int64(ttl.Seconds())
	}

	tok, err := s.deps.Tokens.Issue(r.Context(), subject, ttl)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "username is required or unknown")
			return
		}
		s.log.Error("httpapi: token issue failed", "error", err.Error())
		writeOAuthError(w, http.StatusInternalServerError, "invalid_request", "token creation failed")
		return
	}
	// Record the audit event BEFORE the plaintext AccessToken is serialized
	// and then discarded. The fingerprint is sha256 first 8 hex chars — the
	// same digest the verifier uses, so revoke-by-fingerprint is consistent.
	// Detail carries the fingerprint, the TTL, the token SUBJECT (differs
	// from the actor when an admin mints on behalf of someone) and the
	// authentication arm that minted (Q11 guardrail 4: local/oidc/ldap);
	// the plaintext never enters the audit payload (NFR-S3).
	detail, derr := json.Marshal(tokenIssueDetail{
		Fingerprint: auth.TokenFingerprint(tok.AccessToken),
		TTLSeconds:  expiresIn,
		Subject:     subject,
		Source:      string(p.Source),
	})
	if derr != nil {
		// A marshal failure of three strings and an int is not survivable
		// code; fall back to the pre-T-190 minimal shape so the event still
		// lands (audit must record even when its payload author failed).
		detail = []byte(fmt.Sprintf(`{"fingerprint":"%s","ttl_seconds":%d}`,
			auth.TokenFingerprint(tok.AccessToken), expiresIn))
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:      p.Name,
		Action:     audit.ActionTokenIssue,
		RemoteAddr: r.RemoteAddr,
		Detail:     string(detail),
	})
	resp := tokenCreateResponse{
		AccessToken: tok.AccessToken,
		TokenType:   tok.TokenType,
		Scope:       tok.Scope,
		TokenID:     tok.TokenID,
	}
	if expiresIn > 0 {
		v := expiresIn
		resp.ExpiresIn = &v
	}
	writeJSONBody(w, http.StatusOK, resp)
}

// validScope accepts BinFlow's M1 scope vocabulary: space-separated tokens
// from api:*/applied-permissions groups; "internal:" prefixed tokens are
// rejected with the spec's invalid_scope (auth-model.md 3.1).
func validScope(scope string) bool {
	for _, tok := range strings.Fields(scope) {
		if strings.HasPrefix(tok, "internal:") {
			return false
		}
	}
	return true
}

// handleTokenRevoke serves POST /api/security/token/revoke (E-18): form
// parameters token XOR token_id. Both present -> 400 mutually-exclusive;
// neither -> 400 required; success -> 200 "Token revoked"; unknown or
// already-revoked -> still 200 "Token not found" (idempotent,
// auth-model.md 3.4).
func (s *Server) handleTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "malformed form body: "+err.Error())
		return
	}
	p := principalFrom(r.Context())
	token := r.PostFormValue("token")
	tokenID := strings.TrimSpace(r.PostFormValue("token_id"))
	if token != "" && tokenID != "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token and token_id are mutually exclusive")
		return
	}
	if token == "" && tokenID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token or token_id is required")
		return
	}
	if hint := r.PostFormValue("token_type_hint"); hint != "" &&
		hint != "access_token" && hint != "refresh_token" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Token type is not supported: "+hint)
		return
	}

	if token != "" {
		if err := s.deps.Tokens.Revoke(r.Context(), token); err != nil {
			if errors.Is(err, auth.ErrTokenNotFound) {
				writeText(w, http.StatusOK, "Token not found")
				return
			}
			s.log.Error("httpapi: token revoke failed", "error", err.Error())
			writeOAuthError(w, http.StatusInternalServerError, "invalid_request", "token revocation failed")
			return
		}
		s.audit.Record(r.Context(), audit.Event{
			Actor:      p.Name,
			Action:     audit.ActionTokenRevoke,
			RemoteAddr: r.RemoteAddr,
			Detail:     fmt.Sprintf(`{"fingerprint":"%s"}`, auth.TokenFingerprint(token)),
		})
		writeText(w, http.StatusOK, "Token revoked")
		return
	}

	id, err := parseID(tokenID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token_id must be a numeric id")
		return
	}
	if err := s.deps.Tokens.RevokeByID(r.Context(), id); err != nil {
		if errors.Is(err, auth.ErrTokenNotFound) {
			writeText(w, http.StatusOK, "Token not found")
			return
		}
		s.log.Error("httpapi: token revoke-by-id failed", "error", err.Error())
		writeOAuthError(w, http.StatusInternalServerError, "invalid_request", "token revocation failed")
		return
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:      p.Name,
		Action:     audit.ActionTokenRevoke,
		RemoteAddr: r.RemoteAddr,
		Detail:     fmt.Sprintf(`{"token_id":%d}`, id),
	})
	writeText(w, http.StatusOK, "Token revoked")
}

// parseID parses a decimal token id.
func parseID(v string) (int64, error) {
	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// ---- users (E-19) ----

// userListItem is one GET /api/security/users entry (auth-model.md 1.2):
// name, uri (the item's own API link), realm — plus BinFlow's superset
// field source, the owning identity provider (T-185 / T-174 D1: "local",
// "oidc" or "ldap"; PRD FR-54-AC2/FR-55-AC2 assert it). Never a password
// field.
type userListItem struct {
	Name   string `json:"name"`
	URI    string `json:"uri"`
	Realm  string `json:"realm"`
	Source string `json:"source"`
}

// userDetail is the single-user body: the observable account facts, never a
// password or hash (FR-5-AC11). Email always renders ("" when unset — SE-05
// round-trips the stored value); groups is the membership set ([] when the
// user belongs to none); lastLoggedIn stays absent until login times are
// tracked. Source mirrors the list entry's provider field (T-185/D1).
type userDetail struct {
	Name                     string   `json:"name"`
	Email                    string   `json:"email"`
	Admin                    bool     `json:"admin"`
	Groups                   []string `json:"groups"`
	LastLoggedIn             string   `json:"lastLoggedIn,omitempty"`
	Realm                    string   `json:"realm"`
	Source                   string   `json:"source"`
	ProfileUpdatable         bool     `json:"profileUpdatable"`
	InternalPasswordDisabled bool     `json:"internalPasswordDisabled"`
	DisableUIAccess          bool     `json:"disableUIAccess"`
}

// providerSource normalizes a stored users.provider value for the wire:
// unknown or pre-008 spellings collapse to "local" (the same rule
// auth.adaptUser applies on the read path, so hand-built rows cannot
// smuggle an arbitrary value into the API surface).
func providerSource(provider string) string {
	switch auth.Provider(provider) {
	case auth.ProviderOIDC, auth.ProviderLDAP:
		return provider
	default:
		return string(auth.ProviderLocal)
	}
}

// providerRealm maps the owning provider onto the realm field
// (auth-model.md 1.1, high confidence: realm is "internal"/"ldap"/… per
// user): local rows report Artifactory's "internal", federated rows report
// their provider name (BinFlow models one realm per provider, so the names
// coincide).
func providerRealm(provider string) string {
	if src := providerSource(provider); src != string(auth.ProviderLocal) {
		return src
	}
	return "internal"
}

// userCreateBody is the create/replace body of both routes. Groups is the
// M4 membership field (SE-06): nil means "not addressed" on partial update
// and "no groups" on create/replace.
type userCreateBody struct {
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Admin    bool     `json:"admin"`
	Enabled  *bool    `json:"enabled"`
	Groups   []string `json:"groups"`
}

// handleUserList serves GET /api/security/users (admin): the name/uri/realm
// entries of every account.
func (s *Server) handleUserList(w http.ResponseWriter, r *http.Request) {
	users, err := s.deps.Metadata.Users().List(r.Context())
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}
	items := make([]userListItem, 0, len(users))
	for _, u := range users {
		items = append(items, userListItem{
			Name:   u.Username,
			URI:    requestBase(r) + "/binflow/api/security/users/" + u.Username,
			Realm:  providerRealm(u.Provider),
			Source: providerSource(u.Provider),
		})
	}
	writeJSONBody(w, http.StatusOK, items)
}

// handleUserGet serves GET /api/security/users/{name} (admin): the single
// user with the 004 email round-trip (W40) and the M4 groups echo (SE-05).
func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request, name string) {
	u, err := s.deps.Metadata.Users().Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, metadata.ErrUserNotFound) {
			writePlainError(w, http.StatusNotFound, "User not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}
	groupRows, err := s.deps.Metadata.Groups().GroupsOfUser(r.Context(), name)
	if err != nil {
		writePlainError(w, http.StatusInternalServerError, "resolve user groups: "+err.Error())
		return
	}
	groups := make([]string, 0, len(groupRows))
	for _, g := range groupRows {
		groups = append(groups, g.Name)
	}
	writeJSONBody(w, http.StatusOK, userDetail{
		Name:             u.Username,
		Email:            u.Email,
		Admin:            u.IsAdmin,
		Groups:           groups,
		Realm:            providerRealm(u.Provider),
		Source:           providerSource(u.Provider),
		ProfileUpdatable: true,
	})
}

// handleUserCreatePut serves PUT /api/security/users/{name} (the real
// Artifactory create-or-replace route): 201 with no body in BOTH outcomes —
// create and replace (auth-model.md 1.3 item 11; the M1 409-on-existing
// posture was a simplification retired by T-97, whose membership flow
// W19b re-PUTs an existing user). Validation chain per auth-model.md 1.3 —
// reserved name, body/path name mismatch (409), blank email, blank
// password, unknown group all 400 plain text.
func (s *Server) handleUserCreatePut(w http.ResponseWriter, r *http.Request, name string) {
	s.userCreate(w, r, name, true /*replace*/)
}

// handleUserCreatePost serves POST /api/security/users (BinFlow's own
// collection route): create-only — an existing name is the M1 409 (the
// collection has no path to key a replace on; partial update lives on
// POST /api/security/users/{name}).
func (s *Server) handleUserCreatePost(w http.ResponseWriter, r *http.Request) {
	s.userCreate(w, r, "", false /*replace*/)
}

// userCreate is the shared create/replace implementation. pathName is the
// {name} route segment (empty for the collection route, which then requires
// body.name); replace allows updating an existing account in place.
func (s *Server) userCreate(w http.ResponseWriter, r *http.Request, pathName string, replace bool) {
	var body userCreateBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := pathName
	if name == "" {
		name = body.Name
	}
	if name == "" {
		writePlainError(w, http.StatusBadRequest, "Unable to create user.")
		return
	}
	if name == "_system_" {
		writePlainError(w, http.StatusBadRequest, "Unable to create user.")
		return
	}
	if body.Name != "" && body.Name != name {
		writePlainError(w, http.StatusConflict,
			"The username that was provided in the request path does not match the username in the provided user configuration object.")
		return
	}
	if strings.TrimSpace(body.Email) == "" {
		writePlainError(w, http.StatusBadRequest, "Please provide a valid user email.")
		return
	}
	if body.Password == "" {
		writePlainError(w, http.StatusBadRequest, "Please provide a valid user password.")
		return
	}
	// Artifactory lower-cases usernames on create (auth-model.md 1.1);
	// BinFlow keeps the exact spelling (its auth path is case-sensitive)
	// but rejects a name that would collide after case folding.
	if name != strings.ToLower(name) {
		writePlainError(w, http.StatusBadRequest,
			"user names must be lowercase (Artifactory lower-cases on create; BinFlow rejects the mixed-case spelling instead of silently renaming)")
		return
	}
	if !s.validateGroupNames(w, r, body.Groups) {
		return
	}

	existing, err := s.deps.Metadata.Users().Get(r.Context(), name)
	switch {
	case err == nil:
		if !replace {
			writePlainError(w, http.StatusConflict, "The user already exists: "+name)
			return
		}
	case errors.Is(err, metadata.ErrUserNotFound):
		// create below
	default:
		writePlainError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	if existing == nil {
		hash, herr := auth.HashPassword(body.Password)
		if herr != nil {
			writePlainError(w, http.StatusInternalServerError, "hash password: "+herr.Error())
			return
		}
		// T-208: enabled defaults true on create (the prior hard-coded
		// posture); an explicit false disables the brand-new account.
		enabled := body.Enabled == nil || *body.Enabled
		now := nowRFC3339UTC()
		if err := s.deps.Metadata.Users().Create(r.Context(), &metadata.User{
			Username: name, PasswordHash: hash, IsAdmin: body.Admin, Enabled: enabled,
			Email:     strings.TrimSpace(body.Email),
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			writePlainError(w, http.StatusInternalServerError, "create user: "+err.Error())
			return
		}
	} else {
		// Replace (create-or-replace semantics): password, email and the
		// admin flag take the body's values; enabled is taken from the body
		// when provided (T-208) and keeps its stored value otherwise.
		hash, herr := auth.HashPassword(body.Password)
		if herr != nil {
			writePlainError(w, http.StatusInternalServerError, "hash password: "+herr.Error())
			return
		}
		if err := s.deps.Metadata.Users().UpdatePassword(r.Context(), name, hash); err != nil {
			writePlainError(w, http.StatusInternalServerError, "replace password: "+err.Error())
			return
		}
		if err := s.deps.Metadata.Users().UpdateProfile(r.Context(), name,
			strings.TrimSpace(body.Email), body.Admin); err != nil {
			writePlainError(w, http.StatusInternalServerError, "replace profile: "+err.Error())
			return
		}
		if body.Enabled != nil {
			if err := s.deps.Metadata.Users().SetEnabled(r.Context(), name, *body.Enabled); err != nil {
				writePlainError(w, http.StatusInternalServerError, "replace enabled: "+err.Error())
				return
			}
		}
	}
	if err := s.setUserGroups(r, name, body.Groups); err != nil {
		writePlainError(w, http.StatusInternalServerError, "set user groups: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusCreated) // 201, no body (auth-model.md 1.2/1.3-11)
}

// userUpdateBody is the partial-update body of POST /api/security/users/
// {name} (SE-06, auth-model.md 1.4: unprovided fields keep their stored
// values). Every mutable field is a pointer so "absent" and "explicitly
// empty" stay distinguishable — clearing the membership is groups:[], not
// an omitted groups field.
type userUpdateBody struct {
	Name     string    `json:"name"`
	Email    *string   `json:"email"`
	Password *string   `json:"password"`
	Admin    *bool     `json:"admin"`
	Enabled  *bool     `json:"enabled"`
	Groups   *[]string `json:"groups"`
}

// handleUserUpdatePost serves POST /api/security/users/{name} (SE-06): the
// partial update — name mismatch 409, unknown user 404, provided-but-blank
// email/password 400, groups[] replaced wholesale (unknown group 400).
func (s *Server) handleUserUpdatePost(w http.ResponseWriter, r *http.Request, name string) {
	var body userUpdateBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writePlainError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name != "" && body.Name != name {
		writePlainError(w, http.StatusConflict,
			"The username that was provided in the request path does not match the username in the provided user configuration object.")
		return
	}
	u, err := s.deps.Metadata.Users().Get(r.Context(), name)
	if err != nil {
		if errors.Is(err, metadata.ErrUserNotFound) {
			writePlainError(w, http.StatusNotFound, "User not found")
			return
		}
		writePlainError(w, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}
	if body.Email != nil && strings.TrimSpace(*body.Email) == "" {
		writePlainError(w, http.StatusBadRequest, "Please provide a valid user email.")
		return
	}
	if body.Password != nil && *body.Password == "" {
		writePlainError(w, http.StatusBadRequest, "Please provide a valid user password.")
		return
	}
	var groups []string
	if body.Groups != nil {
		groups = *body.Groups
		if !s.validateGroupNames(w, r, groups) {
			return
		}
	}

	if body.Password != nil {
		hash, herr := auth.HashPassword(*body.Password)
		if herr != nil {
			writePlainError(w, http.StatusInternalServerError, "hash password: "+herr.Error())
			return
		}
		if err := s.deps.Metadata.Users().UpdatePassword(r.Context(), name, hash); err != nil {
			writePlainError(w, http.StatusInternalServerError, "update password: "+err.Error())
			return
		}
	}
	if body.Email != nil || body.Admin != nil {
		email, isAdmin := u.Email, u.IsAdmin
		if body.Email != nil {
			email = strings.TrimSpace(*body.Email)
		}
		if body.Admin != nil {
			isAdmin = *body.Admin
		}
		if err := s.deps.Metadata.Users().UpdateProfile(r.Context(), name, email, isAdmin); err != nil {
			writePlainError(w, http.StatusInternalServerError, "update profile: "+err.Error())
			return
		}
	}
	if body.Enabled != nil {
		// T-208: the partial-update seam lets an admin disable an account.
		// Absent enabled leaves the stored value untouched (the pointer keeps
		// "absent" distinct from an explicit false).
		if err := s.deps.Metadata.Users().SetEnabled(r.Context(), name, *body.Enabled); err != nil {
			writePlainError(w, http.StatusInternalServerError, "update enabled: "+err.Error())
			return
		}
	}
	if body.Groups != nil {
		if err := s.setUserGroups(r, name, groups); err != nil {
			writePlainError(w, http.StatusInternalServerError, "set user groups: "+err.Error())
			return
		}
	}
	w.WriteHeader(http.StatusOK) // 200, no body (auth-model.md 1.2)
}

// nowRFC3339UTC stamps a metadata timestamp.
func nowRFC3339UTC() string { return time.Now().UTC().Format(time.RFC3339) }
