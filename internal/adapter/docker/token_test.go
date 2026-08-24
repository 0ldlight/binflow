package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// fakeAuthorizer is a table-backed Authorizer: the exact (principal, repo,
// path, action) tuples that pass. Everything else denies (the fail-closed
// direction).
type fakeAuthorizer struct {
	allow map[string]bool // "user|repo|path|action" (user "" = anonymous)
}

func (f fakeAuthorizer) Can(_ context.Context, p *auth.Principal, repo, path, action string) bool {
	user := ""
	if p != nil {
		user = p.Name
	}
	return f.allow[user+"|"+repo+"|"+path+"|"+action]
}

// fakeTokens is a TokenRegistry that records the subject and TTL of every
// issued token.
type fakeTokens struct {
	subject string
	ttl     time.Duration
	err     error
}

func (f *fakeTokens) Issue(_ context.Context, username string, ttl time.Duration) (*auth.IssuedToken, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.subject, f.ttl = username, ttl
	return &auth.IssuedToken{AccessToken: "tok-" + username, TokenType: "Bearer",
		ExpiresIn: int64(ttl.Seconds()), Scope: auth.ScopeAPI, Username: username}, nil
}

func (f *fakeTokens) Verify(context.Context, string) (*auth.Principal, error) {
	return nil, errors.New("not implemented in fake")
}

func (f *fakeTokens) Revoke(context.Context, string) error { return nil }

func (f *fakeTokens) RevokeByID(context.Context, int64) error { return nil }

// fakeUsers is the anonymous-subject seeding seam.
type fakeUsers struct {
	rows map[string]*metadata.User
	fail bool
}

func (f *fakeUsers) Get(_ context.Context, name string) (*metadata.User, error) {
	if u, ok := f.rows[name]; ok {
		return u, nil
	}
	return nil, metadata.ErrUserNotFound
}

func (f *fakeUsers) Create(_ context.Context, u *metadata.User) error {
	if f.fail {
		return errors.New("seed store offline")
	}
	if f.rows == nil {
		f.rows = map[string]*metadata.User{}
	}
	if _, ok := f.rows[u.Username]; ok {
		return metadata.ErrDuplicate
	}
	row := *u
	f.rows[u.Username] = &row
	return nil
}

func (f *fakeUsers) Delete(_ context.Context, name string) error {
	delete(f.rows, name)
	return nil
}

func (f *fakeUsers) UpdatePassword(_ context.Context, _ string, _ string) error { return nil }

// UpdateEmail is the 004 widening stub (T-90): the token flow never mutates
// email, so the fake accepts and forgets.
func (f *fakeUsers) UpdateEmail(_ context.Context, _, _ string) error { return nil }

// UpdateProfile is the profile-ticket SPI stub (mechanical wave through the
// package's UserStore fake, T-111 compile fix): the token flow never mutates
// profile columns, so the fake accepts and forgets.
func (f *fakeUsers) UpdateProfile(_ context.Context, _ string, _ string, _ bool) error {
	return nil
}

func (f *fakeUsers) SetEnabled(_ context.Context, _ string, _ bool) error { return nil }

// SetRole is the 011 widening stub (mechanical wave through the package's
// UserStore fake, T-212 compile fix — same posture as UpdateProfile in
// T-111): the token flow never mutates roles, so the fake accepts and
// forgets.
func (f *fakeUsers) SetRole(_ context.Context, _ string, _ string) error { return nil }

// DeleteCascade is the E4 widening stub (mechanical wave through the
// package's UserStore fake, T-251 compile fix — same posture as SetRole in
// T-212): the token flow never deletes accounts. It mirrors the fake's own
// Delete (drop the row, report success) so the fake's one semantic — rows
// reflect the seed — survives the widened interface.
func (f *fakeUsers) DeleteCascade(_ context.Context, name string) error {
	delete(f.rows, name)
	return nil
}

func (f *fakeUsers) List(_ context.Context) ([]*metadata.User, error) { return nil, nil }

func (f *fakeUsers) GetByPasswordHash(_ context.Context, _ string) (*metadata.User, error) {
	return nil, metadata.ErrUserNotFound
}

// newTokenHandler builds a handler with the full token-flow wiring and none
// of the content stack.
func newTokenHandler(authz auth.Authorizer, tokens auth.TokenRegistry, users *fakeUsers, opts Options) *Handler {
	return New(nil, NewStaticRepoLookup(nil), authz, tokens, users, opts, nil)
}

// TestScopeMatrix (AC3, table-driven): parse + narrowing over the scope
// matrix — multi-scope space separation, comma actions, invalid shapes
// tolerated and dropped, pull/push/delete mapped onto r/w/d, subject split
// into repoKey + image path, anonymous catalog vs anonymous read, "*" scope
// answered with the concrete subset.
func TestScopeMatrix(t *testing.T) {
	admin := &auth.Principal{Name: "admin", Admin: true}
	reader := &auth.Principal{Name: "ci"}
	anon := (*auth.Principal)(nil)

	authz := fakeAuthorizer{allow: map[string]bool{
		// ci has read on team1/app (any path under the image)
		"ci|team1|app|r": true,
		// anonymous read on team1/public
		"|team1|public|r": true,
	}}

	tests := []struct {
		name      string
		principal *auth.Principal
		raw       []string
		want      []string
	}{
		{
			name:      "single pull scope granted to an authorized reader",
			principal: reader,
			raw:       []string{"repository:team1/app:pull"},
			want:      []string{"repository:team1/app:pull"},
		},
		{
			name:      "multi scope space separated, only the authorized half survives",
			principal: reader,
			raw:       []string{"repository:team1/app:pull repository:team1/app:pull,push"},
			want:      []string{"repository:team1/app:pull"},
		},
		{
			name:      "comma actions split, push stripped for a read-only principal",
			principal: reader,
			raw:       []string{"repository:team1/app:pull,push,delete"},
			want:      []string{"repository:team1/app:pull"},
		},
		{
			name:      "admin keeps every action",
			principal: admin,
			raw:       []string{"repository:team1/app:pull,push,delete"},
			want:      []string{"repository:team1/app:pull,push,delete"},
		},
		{
			name:      "nested image subject splits repoKey and path",
			principal: admin,
			raw:       []string{"repository:team1/acme/app:pull"},
			want:      []string{"repository:team1/acme/app:pull"},
		},
		{
			name:      "subject without a slash degrades to a repo-key-only question",
			principal: admin,
			raw:       []string{"repository:team1:pull"},
			want:      []string{"repository:team1:pull"},
		},
		{
			name:      "invalid scope tokens are tolerated and dropped",
			principal: admin,
			raw:       []string{"repository:team1/app:pull gibberish repository::pull repo/x no-colons"},
			want:      []string{"repository:team1/app:pull"},
		},
		{
			name:      "unknown actions drop the token",
			principal: admin,
			raw:       []string{"repository:team1/app:waddle"},
			want:      nil,
		},
		{
			name:      "catalog scope for an authenticated principal",
			principal: reader,
			raw:       []string{"registry:catalog:*"},
			want:      []string{"registry:catalog:*"},
		},
		{
			name:      "catalog scope denied to anonymous",
			principal: anon,
			raw:       []string{"registry:catalog:*"},
			want:      nil,
		},
		{
			name:      "anonymous read granted where the ACL allows it",
			principal: anon,
			raw:       []string{"repository:team1/public:pull"},
			want:      []string{"repository:team1/public:pull"},
		},
		{
			name:      "anonymous write never granted",
			principal: anon,
			raw:       []string{"repository:team1/public:pull,push"},
			want:      []string{"repository:team1/public:pull"},
		},
		{
			name:      "star scope answered with the concrete granted subset",
			principal: reader,
			raw:       []string{"repository:team1/app:*"},
			want:      []string{"repository:team1/app:pull"},
		},
		{
			name:      "multiple scope parameters join",
			principal: admin,
			raw:       []string{"repository:team1/app:pull", "repository:team2/app:push"},
			want:      []string{"repository:team1/app:pull", "repository:team2/app:push"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTokenHandler(authz, &fakeTokens{}, &fakeUsers{}, Options{})
			got := h.narrowScopes(context.Background(), tc.principal, parseScopes(tc.raw...))
			if len(got) != len(tc.want) {
				t.Fatalf("narrowScopes = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("narrowScopes[%d] = %q, want %q (all: %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestNarrowScopesFailClosed: a nil Authorizer (bare assembly) grants
// nothing — the token is still issued, the scope field comes back empty.
func TestNarrowScopesFailClosed(t *testing.T) {
	h := newTokenHandler(nil, &fakeTokens{}, &fakeUsers{}, Options{})
	got := h.narrowScopes(context.Background(), &auth.Principal{Name: "x"},
		parseScopes("repository:team1/app:pull,push"))
	if len(got) != 0 {
		t.Fatalf("nil authorizer granted %v, want none", got)
	}
}

// TestDeriveChallengeScope (AC2): the challenge scope per method — GET/HEAD
// pull, writes pull,push, deletes pull,delete; subject spelled as the full
// name the client addressed.
func TestDeriveChallengeScope(t *testing.T) {
	ref := nameRef{repoKey: "team1", image: "acme/app"}
	tests := []struct {
		method string
		want   string
	}{
		{http.MethodGet, "repository:team1/acme/app:pull"},
		{http.MethodHead, "repository:team1/acme/app:pull"},
		{http.MethodPut, "repository:team1/acme/app:pull,push"},
		{http.MethodPost, "repository:team1/acme/app:pull,push"},
		{http.MethodPatch, "repository:team1/acme/app:pull,push"},
		{http.MethodDelete, "repository:team1/acme/app:pull,delete"},
		{"OPTIONS", "repository:team1/acme/app:pull"},
	}
	for _, tc := range tests {
		if got := deriveChallengeScope(tc.method, ref); got != tc.want {
			t.Errorf("deriveChallengeScope(%s) = %q, want %q", tc.method, got, tc.want)
		}
	}
}

// TestTokenEndpointSuite (AC1, table-driven): the token endpoint's request
// matrix — GET and POST forms, both credentials, anonymous both modes, the
// rejected parameters, wrong credentials (rendered by the router plane but
// asserted here through the handler's own posture), and the response shape.
func TestTokenEndpointSuite(t *testing.T) {
	authz := fakeAuthorizer{allow: map[string]bool{
		"ci|team1|app|r": true,
	}}
	users := &fakeUsers{}
	tokens := &fakeTokens{}

	tests := []struct {
		name       string
		anonOpen   bool
		method     string
		query      string
		form       string
		principal  *auth.Principal
		wantStatus int
		wantBody   func(t *testing.T, body string, h *Handler)
	}{
		{
			name:       "GET with admin credentials",
			anonOpen:   true,
			method:     http.MethodGet,
			query:      "service=binflow&scope=repository:team1/app:pull,push&account=admin",
			principal:  &auth.Principal{Name: "admin", Admin: true},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var tr tokenResponse
				if err := json.Unmarshal([]byte(body), &tr); err != nil {
					t.Fatalf("body %q: %v", body, err)
				}
				if tr.Token == "" || tr.AccessToken != tr.Token {
					t.Fatalf("token/access_token = %q/%q, want equal and non-empty", tr.Token, tr.AccessToken)
				}
				if tr.ExpiresIn != int64((720 * time.Hour).Seconds()) {
					t.Fatalf("expires_in = %d, want the 720h default", tr.ExpiresIn)
				}
				if _, err := time.Parse(time.RFC3339, tr.IssuedAt); err != nil {
					t.Fatalf("issued_at %q is not RFC3339: %v", tr.IssuedAt, err)
				}
				if tr.Scope != "repository:team1/app:pull,push" {
					t.Fatalf("scope = %q, want the full grant for admin", tr.Scope)
				}
				if tr.RefreshToken != "" {
					t.Fatalf("refresh_token = %q, want never set", tr.RefreshToken)
				}
			},
		},
		{
			name:       "POST form with the same parameters",
			anonOpen:   true,
			method:     http.MethodPost,
			form:       "service=binflow&scope=repository:team1/app:pull&account=ci",
			principal:  &auth.Principal{Name: "ci"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var tr tokenResponse
				if err := json.Unmarshal([]byte(body), &tr); err != nil {
					t.Fatalf("body %q: %v", body, err)
				}
				if tr.Token == "" {
					t.Fatal("token empty")
				}
				if tr.Scope != "repository:team1/app:pull" {
					t.Fatalf("scope = %q, want the ACL-narrowed pull", tr.Scope)
				}
			},
		},
		{
			name:       "POST with parameters in the query instead of the body",
			anonOpen:   true,
			method:     http.MethodPost,
			query:      "service=binflow&scope=repository:team1/app:pull",
			principal:  &auth.Principal{Name: "ci"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var tr tokenResponse
				_ = json.Unmarshal([]byte(body), &tr)
				if tr.Token == "" {
					t.Fatal("token empty")
				}
			},
		},
		{
			name:       "anonymous with anonymous access open issues a pull-only token",
			anonOpen:   true,
			method:     http.MethodGet,
			query:      "service=binflow&scope=repository:team1/app:pull,push",
			principal:  nil,
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var tr tokenResponse
				if err := json.Unmarshal([]byte(body), &tr); err != nil {
					t.Fatalf("body %q: %v", body, err)
				}
				if tr.Token == "" {
					t.Fatal("anonymous token empty")
				}
				if tr.Scope != "" {
					t.Fatalf("anonymous scope = %q, want empty (no grant)", tr.Scope)
				}
				if tokens.subject != anonymousSubject {
					t.Fatalf("anonymous token subject = %q, want %q", tokens.subject, anonymousSubject)
				}
			},
		},
		{
			name:       "anonymous with anonymous access closed is a 401",
			anonOpen:   false,
			method:     http.MethodGet,
			query:      "service=binflow&scope=repository:team1/app:pull",
			principal:  nil,
			wantStatus: http.StatusUnauthorized,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var oe oauthErrorBody
				if err := json.Unmarshal([]byte(body), &oe); err != nil {
					t.Fatalf("body %q: %v", body, err)
				}
				if oe.Error != oauthErrInvalidClient {
					t.Fatalf("error = %q, want invalid_client", oe.Error)
				}
			},
		},
		{
			// D44-2: docker 29's challenge-mode login sends offline_token=true
			// on every token GET; the spec lets the server ignore it, so the
			// request now succeeds and the response never carries a
			// refresh_token (Q3: no refresh).
			name:       "offline_token accepted and ignored",
			anonOpen:   true,
			method:     http.MethodGet,
			query:      "service=binflow&offline_token=true&scope=repository:team1/app:pull",
			principal:  &auth.Principal{Name: "admin", Admin: true},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var tr tokenResponse
				if err := json.Unmarshal([]byte(body), &tr); err != nil {
					t.Fatalf("body %q: %v", body, err)
				}
				if tr.Token == "" {
					t.Fatal("token empty with offline_token=true")
				}
				if tr.RefreshToken != "" {
					t.Fatalf("refresh_token = %q, want never set", tr.RefreshToken)
				}
			},
		},
		{
			name:       "refresh_token grant is rejected",
			anonOpen:   true,
			method:     http.MethodPost,
			form:       "grant_type=refresh_token&refresh_token=x",
			principal:  &auth.Principal{Name: "admin", Admin: true},
			wantStatus: http.StatusBadRequest,
			wantBody: func(t *testing.T, body string, _ *Handler) {
				var oe oauthErrorBody
				_ = json.Unmarshal([]byte(body), &oe)
				if oe.Error != oauthErrUnsupportedGrant {
					t.Fatalf("error = %q, want unsupported_grant_type", oe.Error)
				}
			},
		},
		{
			name:       "method other than GET/POST",
			anonOpen:   true,
			method:     http.MethodPut,
			principal:  &auth.Principal{Name: "admin", Admin: true},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "nil token registry is a 503, not a panic",
			anonOpen:   true,
			method:     http.MethodGet,
			principal:  &auth.Principal{Name: "admin", Admin: true},
			wantStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tok auth.TokenRegistry = tokens
			if strings.Contains(tc.name, "nil token registry") {
				tok = nil
			}
			h := New(nil, NewStaticRepoLookup(nil), authz, tok, users,
				Options{AnonymousAccess: tc.anonOpen, TokenTTL: 720 * time.Hour}, nil)

			req := &http.Request{Method: tc.method, URL: &url.URL{Path: TokenPath,
				RawQuery: tc.query}, Body: nil, RemoteAddr: "127.0.0.1:1"}
			if tc.form != "" {
				req.Header = http.Header{}
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				req.Body = io.NopCloser(strings.NewReader(tc.form))
			}
			req = req.WithContext(adapter.WithPrincipal(req.Context(), tc.principal))

			w := &captureWriter{hdr: http.Header{}}
			h.ServeHTTP(w, req)

			if w.status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.status, tc.wantStatus, w.body.String())
			}
			if got := w.hdr.Get(HeaderAPIVersion); got != "registry/2.0" {
				t.Fatalf("api-version header = %q", got)
			}
			if tc.wantBody != nil {
				tc.wantBody(t, w.body.String(), h)
			}
		})
	}
}

// TestTokenSubjectIsThePrincipal: an authenticated exchange mints the token
// under the caller's name (the revocation chain and audit trail key on it).
func TestTokenSubjectIsThePrincipal(t *testing.T) {
	tokens := &fakeTokens{}
	h := New(nil, NewStaticRepoLookup(nil), fakeAuthorizer{}, tokens, &fakeUsers{},
		Options{AnonymousAccess: true, TokenTTL: time.Hour}, nil)
	req := &http.Request{Method: http.MethodGet,
		URL: &url.URL{Path: TokenPath, RawQuery: "service=binflow"}}
	req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "ci"}))
	w := &captureWriter{hdr: http.Header{}}
	h.ServeHTTP(w, req)
	if w.status != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.status, w.body.String())
	}
	if tokens.subject != "ci" {
		t.Fatalf("subject = %q, want ci", tokens.subject)
	}
	if tokens.ttl != time.Hour {
		t.Fatalf("ttl = %s, want 1h", tokens.ttl)
	}
}

// TestTokenTTLFloor: a non-positive configured TTL is floored to 1h (the
// endpoint's posture is limited TTL — AC1).
func TestTokenTTLFloor(t *testing.T) {
	h := newTokenHandler(nil, &fakeTokens{}, &fakeUsers{}, Options{TokenTTL: 0})
	if got := h.tokenTTL(); got != time.Hour {
		t.Fatalf("tokenTTL = %s, want 1h", got)
	}
	h = newTokenHandler(nil, &fakeTokens{}, &fakeUsers{}, Options{TokenTTL: -time.Hour})
	if got := h.tokenTTL(); got != time.Hour {
		t.Fatalf("negative tokenTTL = %s, want 1h", got)
	}
}

// TestEnsureAnonymousSubject: the synthetic account is created once,
// idempotently, enabled (its tokens must verify as Bearer — D44-1) and
// never admin; the password hash is a REAL argon2id hash of a random
// secret, so no password can ever verify against it.
func TestEnsureAnonymousSubject(t *testing.T) {
	users := &fakeUsers{}
	if err := ensureAnonymousSubject(context.Background(), users); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	u, err := users.Get(context.Background(), anonymousSubject)
	if err != nil {
		t.Fatalf("get after seed: %v", err)
	}
	if !u.Enabled || u.IsAdmin {
		t.Fatalf("synthetic account enabled=%v admin=%v, want true/false", u.Enabled, u.IsAdmin)
	}
	if u.PasswordHash == "" || !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Fatalf("synthetic account hash = %q, want a real argon2id PHC", u.PasswordHash)
	}
	if auth.VerifyPassword("password", u.PasswordHash) || auth.VerifyPassword("", u.PasswordHash) {
		t.Fatal("synthetic account hash verifies a guessable password")
	}
	if err := ensureAnonymousSubject(context.Background(), users); err != nil {
		t.Fatalf("second seed must be a no-op: %v", err)
	}
}

// TestEnsureAnonymousSubjectFailure: an offline seed store surfaces the
// error (the anonymous token path fails closed).
func TestEnsureAnonymousSubjectFailure(t *testing.T) {
	users := &fakeUsers{fail: true}
	if err := ensureAnonymousSubject(context.Background(), users); err == nil {
		t.Fatal("seed failure swallowed")
	}
}

// TestRenderAuthFailureTokenPathIsOAuth (T-55, PRD v1.2/C3): a refused
// credential on the token endpoint's route renders the OAUTH-form body with
// the Bearer challenge header unchanged; every other /v2 route keeps the
// registry spec body. The router reaches RenderAuthFailure through the
// v2AuthFailure seam, so this pins the adapter-side split both ways.
func TestRenderAuthFailureTokenPathIsOAuth(t *testing.T) {
	h := newTokenHandler(nil, nil, &fakeUsers{}, Options{AnonymousAccess: true, BaseURL: "http://reg.example"})

	cases := []struct {
		path  string
		oauth bool
	}{
		{TokenPath, true},
		{TokenPath + "/sub", true},
		{"/v2/", false},
		{"/v2/team1/app/manifests/latest", false},
	}
	for _, tc := range cases {
		w := &captureWriter{hdr: http.Header{}}
		req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: tc.path}}
		h.RenderAuthFailure(w, req)

		if w.status != http.StatusUnauthorized {
			t.Fatalf("%s status = %d", tc.path, w.status)
		}
		body := w.body.String()
		if strings.Contains(body, `"status"`) {
			t.Fatalf("%s body carries the /binflow envelope: %s", tc.path, body)
		}
		if tc.oauth {
			if !strings.Contains(body, `"error":"invalid_client"`) {
				t.Fatalf("%s body is not the OAuth form: %s", tc.path, body)
			}
			if strings.Contains(body, `"errors"`) {
				t.Fatalf("%s body carries the registry spec envelope: %s", tc.path, body)
			}
		} else {
			if !strings.Contains(body, `"code":"UNAUTHORIZED"`) {
				t.Fatalf("%s body is not the registry spec form: %s", tc.path, body)
			}
		}
		// The challenge header keeps the ADR-0010 clause 4 shape on BOTH
		// branches (realm=<base>/v2/token, service=binflow).
		want := `Bearer realm="http://reg.example/v2/token",service="binflow"`
		if got := w.hdr.Get("WWW-Authenticate"); got != want {
			t.Fatalf("%s challenge = %q, want %q", tc.path, got, want)
		}
		if got := w.hdr.Get(HeaderAPIVersion); got != "registry/2.0" {
			t.Fatalf("%s api-version = %q", tc.path, got)
		}
	}
}
