// T-91 acceptance surface (PRD FR-23, CE-01..07, W01~W08/W37a/W38/W01b):
// the console mount, the session verb triple, the cookie contract, the CSRF
// Origin matrix, and the three-arm credential equivalence across the
// content, adapter and management planes.

package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// sessionCookie is one parsed Set-Cookie line of the login response.
type sessionCookie struct {
	raw   string
	value string
	attrs map[string]string
}

// login issues one login request and returns the response plus the parsed
// session cookie (nil when the response carries none).
func login(t *testing.T, h *harness, body, contentType string) (*http.Response, *sessionCookie) {
	t.Helper()
	hdr := map[string]string{}
	if contentType != "" {
		hdr["Content-Type"] = contentType
	}
	resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", []byte(body), hdr)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if raw := resp.Header.Get("Set-Cookie"); raw != "" {
		return resp, parseSetCookie(raw)
	}
	return resp, nil
}

// loginJSON is the JSON spelling of login (the PRD's primary form).
func loginJSON(t *testing.T, h *harness, user, pass string) (*http.Response, *sessionCookie) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	return login(t, h, string(body), "application/json")
}

// parseSetCookie splits one Set-Cookie line into value + attribute map
// (lower-cased names).
func parseSetCookie(raw string) *sessionCookie {
	c := &sessionCookie{raw: raw, attrs: map[string]string{}}
	parts := strings.Split(raw, ";")
	head := strings.SplitN(parts[0], "=", 2)
	if len(head) == 2 {
		c.value = head[1]
	}
	for _, p := range parts[1:] {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		if len(kv) == 2 {
			c.attrs[k] = kv[1]
		} else {
			c.attrs[k] = ""
		}
	}
	return c
}

// cookieHdr is the request header form of a session cookie.
func (c *sessionCookie) cookieHdr() map[string]string {
	return map[string]string{"Cookie": "binflow_session=" + c.value}
}

// mustLogin asserts a successful admin login and returns the cookie.
func mustLogin(t *testing.T, h *harness) *sessionCookie {
	t.Helper()
	resp, c := loginJSON(t, h, adminUser, adminPass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
	}
	if c == nil {
		t.Fatal("login response carries no Set-Cookie")
	}
	return c
}

// createRepoHTTP creates a generic repo through the management API (Basic).
func createRepoHTTP(t *testing.T, h *harness, key string) {
	t.Helper()
	body := `{"rclass":"local","packageType":"generic"}`
	resp := h.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass, []byte(body),
		map[string]string{"Content-Type": "application/json"})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create repo %s status = %d, body=%s", key, resp.StatusCode, mustGet(t, resp))
	}
}

// ---- CE-01/CE-02: console mount (W01/W02) ----

// TestConsoleSegmentMount: /binflow/ui/** and /binflow/assets/** route to
// the console handler while /binflow/<repo>/<path> keeps routing content —
// and a repo whose key merely embeds a reserved word is unaffected.
func TestConsoleSegmentMount(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")
	createRepoHTTP(t, h, "ui-local") // embedded spelling: NOT reserved

	// CE-01: the root redirect (followed this time — the target now serves).
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(h.srv.URL + "/binflow/")
	if err != nil {
		t.Fatalf("GET /binflow/: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/binflow/ui/" {
		t.Fatalf("/binflow/ = %d %q, want 301 /binflow/ui/", resp.StatusCode, resp.Header.Get("Location"))
	}

	// CE-02/W01: the shell and the deep-link history fallback.
	for _, path := range []string{"/binflow/ui/", "/binflow/ui/repositories", "/binflow/ui/artifacts/x/y/z"} {
		r := h.do(http.MethodGet, path, "", "", nil, nil)
		body := mustGet(t, r)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, body=%s", path, r.StatusCode, body)
		}
		if !strings.Contains(body, `id="root"`) {
			t.Fatalf("GET %s: no SPA mount point in shell", path)
		}
		if cc := r.Header.Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("GET %s Cache-Control = %q, want no-cache", path, cc)
		}
	}

	// W02: fingerprinted assets are immutable when the build ran, 404 when
	// the hash is unknown; the placeholder-only embed simply has no assets.
	if name := firstEmbeddedAsset(t); name != "" {
		r := h.do(http.MethodGet, "/binflow/assets/"+name, "", "", nil, nil)
		if r.StatusCode != http.StatusOK {
			t.Fatalf("asset %s status = %d", name, r.StatusCode)
		}
		if cc := r.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
			t.Fatalf("asset Cache-Control = %q, want immutable", cc)
		}
	} else {
		t.Log("console embedded without built assets (placeholder shell): asset leg skipped")
	}
	if r := h.do(http.MethodGet, "/binflow/assets/nope-does-not-exist.js", "", "", nil, nil); r.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown asset status = %d, want 404", r.StatusCode)
	}

	// The mount must not shadow the content plane.
	put := h.do(http.MethodPut, "/binflow/generic-local/acme/a.bin", adminUser, adminPass,
		[]byte("hello"), nil)
	if put.StatusCode != http.StatusCreated {
		t.Fatalf("content PUT status = %d, body=%s", put.StatusCode, mustGet(t, put))
	}
	get := h.do(http.MethodGet, "/binflow/generic-local/acme/a.bin", "", "", nil, nil)
	if get.StatusCode != http.StatusOK {
		t.Fatalf("content GET status = %d (anonymous read default on)", get.StatusCode)
	}
	// The embedded-reserved repo still serves.
	put2 := h.do(http.MethodPut, "/binflow/ui-local/x.bin", adminUser, adminPass, []byte("x"), nil)
	if put2.StatusCode != http.StatusCreated {
		t.Fatalf("ui-local PUT status = %d, body=%s", put2.StatusCode, mustGet(t, put2))
	}
}

// firstEmbeddedAsset picks one asset filename from the embedded console dist
// ("" when the placeholder shell is embedded — `make console` not run).
func firstEmbeddedAsset(t *testing.T) string {
	t.Helper()
	entries, err := fs.ReadDir(console.Dist(), "assets")
	if err != nil || len(entries) == 0 {
		return ""
	}
	return entries[0].Name()
}

// TestConsoleSegmentDoesNotSwallowTraversals: a dot-segment spelling that
// starts inside the ui segment must die at the console's path.Clean
// backstop — it may never address content on the other side.
func TestConsoleSegmentDoesNotSwallowTraversals(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")
	// Drive the assembled handler directly: an HTTP client would normalize
	// the URL before sending (the router deliberately never does).
	req := httptest.NewRequest(http.MethodGet, "/binflow/ui/../generic-local/acme/a.bin", nil)
	rec := httptest.NewRecorder()
	h.srv.Config.Handler.ServeHTTP(rec, req) //nolint:errcheck // probe of the assembled handler
	if rec.Code != http.StatusNotFound {
		t.Fatalf("dot-segment probe through the ui segment = %d, want 404 (cleaned out of segment)", rec.Code)
	}
}

// ---- CE-07: reserved repo keys (W01b) ----

// TestReservedRepoKeysHTTP: every ADR-0008 (T-108 union) segment is refused
// at the management API with 400; a legal key still creates.
func TestReservedRepoKeysHTTP(t *testing.T) {
	h := newHarness(t)
	body := `{"rclass":"local","packageType":"generic"}`
	for _, key := range []string{"ui", "docs", "console", "api", "v2", "assets"} {
		resp := h.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
			[]byte(body), map[string]string{"Content-Type": "application/json"})
		got := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("PUT repo %q status = %d, want 400; body=%s", key, resp.StatusCode, got)
		}
		if !strings.Contains(got, "reserved") {
			t.Fatalf("PUT repo %q body %q does not name the reserved-word reason", key, got)
		}
	}
	createRepoHTTP(t, h, "plain-local") // legal keys unaffected (W01b second leg)
}

// ---- CE-03: login (W03/W04/W04b/W05) ----

// TestSessionLogin: the login contract — dual body shapes, the whoami
// triple's unauthenticated leg, cookie attributes, uniform 401 wording and
// the login audit pair.
func TestSessionLogin(t *testing.T) {
	h := newHarness(t)

	t.Run("whoami anonymous is 401 (W03)", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous whoami = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("JSON login issues the cookie (W04/W04b)", func(t *testing.T) {
		resp, c := loginJSON(t, h, adminUser, adminPass)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
		}
		var who struct {
			Username string `json:"username"`
			Admin    bool   `json:"admin"`
		}
		if err := json.Unmarshal([]byte(mustGet(t, resp)), &who); err != nil {
			t.Fatalf("body not JSON: %v", err)
		}
		if who.Username != adminUser || !who.Admin {
			t.Fatalf("login body = %+v, want admin/true", who)
		}
		if c == nil {
			t.Fatal("no Set-Cookie on login")
		}
		for _, attr := range []string{"httponly", "path", "samesite"} {
			if _, ok := c.attrs[attr]; !ok {
				t.Fatalf("Set-Cookie %q missing %s: %s", c.raw, attr, c.raw)
			}
		}
		if c.attrs["path"] != "/binflow" {
			t.Fatalf("cookie Path = %q, want /binflow", c.attrs["path"])
		}
		if !strings.EqualFold(c.attrs["samesite"], "lax") {
			t.Fatalf("cookie SameSite = %q, want Lax", c.attrs["samesite"])
		}
		if _, secure := c.attrs["secure"]; secure {
			t.Fatalf("plain-HTTP login must not set Secure: %s", c.raw)
		}
		if c.attrs["max-age"] != "86400" {
			t.Fatalf("cookie Max-Age = %q, want 86400 (24h default)", c.attrs["max-age"])
		}
		if len(c.value) != 64 {
			t.Fatalf("cookie value length = %d, want 64 hex chars (256-bit id)", len(c.value))
		}
	})

	t.Run("form login also works (CE-03 dual shape)", func(t *testing.T) {
		form := url.Values{"username": {adminUser}, "password": {adminPass}}.Encode()
		resp, c := login(t, h, form, "application/x-www-form-urlencoded")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("form login status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
		}
		if c == nil {
			t.Fatal("form login issued no cookie")
		}
	})

	t.Run("whoami with the cookie (W06)", func(t *testing.T) {
		c := mustLogin(t, h)
		resp := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("whoami status = %d, body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(body, `"admin"`) || !strings.Contains(body, adminUser) {
			t.Fatalf("whoami body = %s", body)
		}
	})

	t.Run("wrong credentials are a uniform 401 (W05)", func(t *testing.T) {
		wrongPw, _ := loginJSON(t, h, adminUser, "definitely-wrong")
		unknownUser, _ := loginJSON(t, h, "no-such-user", "definitely-wrong")
		b1, b2 := mustGet(t, wrongPw), mustGet(t, unknownUser)
		if wrongPw.StatusCode != http.StatusUnauthorized || unknownUser.StatusCode != http.StatusUnauthorized {
			t.Fatalf("statuses = %d/%d, want 401/401", wrongPw.StatusCode, unknownUser.StatusCode)
		}
		if b1 != b2 {
			t.Fatalf("401 bodies differ (existence leak):\n%s\n%s", b1, b2)
		}
		if strings.Contains(b1, "no such user") || strings.Contains(b1, "unknown") {
			t.Fatalf("401 body leaks existence: %s", b1)
		}
	})

	t.Run("login.failed lands in the audit trail (W05/W23 anchor)", func(t *testing.T) {
		_, _ = loginJSON(t, h, adminUser, "definitely-wrong")
		events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{
			Actor: adminUser, Action: "login.failed",
		})
		if err != nil {
			t.Fatalf("audit query: %v", err)
		}
		if len(events) == 0 {
			t.Fatal("no login.failed event recorded")
		}
		for _, e := range events {
			if strings.Contains(e.Detail, "definitely-wrong") {
				t.Fatalf("audit detail leaks the password: %s", e.Detail)
			}
		}
	})

	t.Run("login.success lands in the audit trail", func(t *testing.T) {
		_ = mustLogin(t, h)
		events, err := h.md.Audits().Query(context.Background(), metadata.AuditQuery{
			Actor: adminUser, Action: "login.success",
		})
		if err != nil {
			t.Fatalf("audit query: %v", err)
		}
		if len(events) == 0 {
			t.Fatal("no login.success event recorded")
		}
	})

	t.Run("shape errors are 400", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			body        string
			contentType string
		}{
			{"empty body", "", "application/json"},
			{"missing password", `{"username":"admin"}`, "application/json"},
			{"missing username", `{"password":"x"}`, "application/json"},
			{"malformed JSON", `{`, "application/json"},
			{"unsupported content type", `{"username":"admin","password":"x"}`, "text/plain"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				resp, c := login(t, h, tc.body, tc.contentType)
				if resp.StatusCode != http.StatusBadRequest {
					t.Fatalf("status = %d, body=%s", resp.StatusCode, mustGet(t, resp))
				}
				if c != nil {
					t.Fatalf("failed login issued a cookie: %s", c.raw)
				}
			})
		}
	})

	t.Run("session value never appears in logs (NFR-S19)", func(t *testing.T) {
		h.resetLogs()
		c := mustLogin(t, h)
		resp := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
		_ = mustGet(t, resp)
		out := h.do(http.MethodDelete, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
		_ = mustGet(t, out)
		if logs := h.logs(); strings.Contains(logs, c.value) {
			t.Fatalf("session id leaked into logs:\n%s", logs)
		}
	})
}

// TestSessionCookieSecureFlag: Secure follows server.base_url first, then
// the request scheme — plain HTTP never gets it (PRD FR-23 R1 附注).
func TestSessionCookieSecureFlag(t *testing.T) {
	h := newHarnessCfg(t, func(c *config.Config) { c.Server.BaseURL = "https://mirror.example.com" }, nil)
	_, c := loginJSON(t, h, adminUser, adminPass)
	if c == nil {
		t.Fatal("no cookie")
	}
	if _, ok := c.attrs["secure"]; !ok {
		t.Fatalf("https base_url must set Secure: %s", c.raw)
	}
}

// ---- CE-04/CE-05: logout, revocation, restart, TTL ----

// TestSessionLogoutRevocation (W07): DELETE revokes server-side; the
// replayed cookie fails with 401.
func TestSessionLogoutRevocation(t *testing.T) {
	h := newHarness(t)
	c := mustLogin(t, h)

	del := h.do(http.MethodDelete, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, body=%s", del.StatusCode, mustGet(t, del))
	}
	// The clearing cookie: epoch expiry / negative Max-Age.
	if raw := del.Header.Get("Set-Cookie"); !strings.Contains(raw, "Max-Age=0") &&
		!strings.Contains(raw, "Max-Age=-1") && !strings.Contains(raw, "Expires=Thu, 01 Jan 1970") {
		t.Fatalf("logout does not clear the cookie: %q", raw)
	}

	again := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
	if again.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed whoami = %d, want 401 (server-side revocation)", again.StatusCode)
	}

	// The row is revoked, not gone: revocation is the logout fact.
	row, err := h.md.WebSessions().GetBySHA256(context.Background(), sha256Hex([]byte(c.value)))
	if err != nil {
		t.Fatalf("row after logout: %v", err)
	}
	if row.RevokedAt == "" {
		t.Fatal("row not marked revoked")
	}
}

// TestSessionRestartPersistence (W37a): sessions are rows — a server rebuilt
// over the same data directory (fresh auth service, fresh HTTP surface)
// still honors the cookie.
func TestSessionRestartPersistence(t *testing.T) {
	h := newHarness(t)
	c := mustLogin(t, h)

	// The "restart": brand-new auth service and HTTP server, same stores
	// (metadata + storage survive the process by contract).
	restarted := httpapi.New(httpapi.Deps{
		Config:   config.Defaults(),
		Auth:     auth.NewFromStore(h.md, true),
		Authz:    auth.NewFromStore(h.md, true),
		Metadata: h.md,
		Repos:    h.md.Repos(),
		ReposSvc: h.svc,
		Console:  console.Handler(),
		Adapters: []adapter.Handler{generic.New(h.svc, h.md.Blobs())},
	}, nil)
	ts := httptest.NewServer(restarted.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/binflow/api/v1/session", nil)
	req.Header.Set("Cookie", "binflow_session="+c.value)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("whoami after restart: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("whoami after restart = %d, body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), adminUser) {
		t.Fatalf("whoami after restart body = %s", body)
	}
}

// TestSessionTTLExpiryHTTP (W08): the seconds override key reaches the row —
// a session issued under a 1s TTL dies while a default-TTL session lives.
func TestSessionTTLExpiryHTTP(t *testing.T) {
	short := newHarnessCfg(t, func(c *config.Config) { c.Console.SessionTTL = time.Second }, nil)
	c := mustLogin(t, short)
	if got := parseSetCookie(shortLastSetCookie(t, short)); got.attrs["max-age"] != "1" {
		t.Fatalf("short-TTL cookie Max-Age = %q, want 1", got.attrs["max-age"])
	}
	time.Sleep(1400 * time.Millisecond)
	resp := short.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("whoami past TTL = %d, want 401", resp.StatusCode)
	}

	// Control: the default-TTL harness is still alive after the same clock.
	long := newHarness(t)
	c2 := mustLogin(t, long)
	resp2 := long.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c2.cookieHdr())
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("default-TTL whoami = %d, want 200", resp2.StatusCode)
	}
}

// shortLastSetCookie re-logs-in and captures the raw Set-Cookie line (the
// login helper parses but does not expose the raw string).
func shortLastSetCookie(t *testing.T, h *harness) string {
	t.Helper()
	resp, _ := loginJSON(t, h, adminUser, adminPass)
	return resp.Header.Get("Set-Cookie")
}

// TestSessionConcurrentWhoami: one session under concurrent use (the W37
// hundred-concurrent shape, sized for -race) — all requests resolve, no
// cross-session bleed.
func TestSessionConcurrentWhoami(t *testing.T) {
	h := newHarness(t)
	mine := mustLogin(t, h)
	other := loginOn(t, h, "jane", "jane-pw")

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/binflow/api/v1/session", nil)
			req.Header.Set("Cookie", "binflow_session="+mine.value)
			resp, err := h.srv.Client().Do(req)
			if err != nil {
				errs <- err
				return
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), adminUser) {
				errs <- fmt.Errorf("whoami = %d %s", resp.StatusCode, body)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// The other session still resolves its own user (no bleed).
	resp := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, other.cookieHdr())
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"jane"`) {
		t.Fatalf("second session whoami = %d %s", resp.StatusCode, body)
	}
}

// loginOn seeds a non-admin user through the harness and logs in as them.
func loginOn(t *testing.T, h *harness, user, pass string) *sessionCookie {
	t.Helper()
	h.do(http.MethodPut, "/binflow/api/security/users/"+user, adminUser, adminPass,
		[]byte(`{"name":"`+user+`","email":"`+user+`@example.com","password":"`+pass+`"}`),
		map[string]string{"Content-Type": "application/json"})
	resp, c := loginJSON(t, h, user, pass)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s = %d, body=%s", user, resp.StatusCode, mustGet(t, resp))
	}
	return c
}

// ---- three-arm equivalence across the planes (CE-03/CE-04/CE-05, ux R10) ----

// TestSessionCookieEquivalence: the cookie carries the same authority as
// Basic on the content plane, the docker adapter plane and the management
// plane — the console's uploads and deletes live on exactly this contract.
func TestSessionCookieEquivalence(t *testing.T) {
	// anonymous OFF makes every leg below a real authentication proof (an
	// anonymous-enabled instance would 200 without any credential).
	h := newHarnessCfg(t, func(c *config.Config) { c.Security.AnonymousAccess = false }, nil)
	c := mustLogin(t, h)
	createRepoHTTP(t, h, "generic-local")

	t.Run("content plane", func(t *testing.T) {
		put := h.do(http.MethodPut, "/binflow/generic-local/acme/cookie.bin", "", "",
			[]byte("via-cookie"), c.cookieHdr())
		if put.StatusCode != http.StatusCreated {
			t.Fatalf("cookie PUT = %d, body=%s", put.StatusCode, mustGet(t, put))
		}
		get := h.do(http.MethodGet, "/binflow/generic-local/acme/cookie.bin", "", "", nil, c.cookieHdr())
		if get.StatusCode != http.StatusOK {
			t.Fatalf("cookie GET = %d, want 200", get.StatusCode)
		}
		del := h.do(http.MethodDelete, "/binflow/generic-local/acme/cookie.bin", "", "", nil, c.cookieHdr())
		if del.StatusCode != http.StatusNoContent {
			t.Fatalf("cookie DELETE = %d, want 204 (the generic adapter's contract)", del.StatusCode)
		}
		// The no-cookie legs prove the plane is actually closed.
		deny := h.do(http.MethodPut, "/binflow/generic-local/acme/x.bin", "", "", []byte("x"), nil)
		if deny.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous PUT on closed instance = %d, want 401", deny.StatusCode)
		}
	})

	t.Run("management plane", func(t *testing.T) {
		resp := h.do(http.MethodGet, "/binflow/api/v1/storage/stats", "", "", nil, c.cookieHdr())
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("cookie on management stats = %d, want 200 (admin)", resp.StatusCode)
		}
	})

	t.Run("docker adapter plane (ux R10)", func(t *testing.T) {
		// /v2/ ping on a closed instance: no credential -> 401 Bearer
		// challenge; the session cookie -> 200. The docker token flow and
		// tags/catalog reads sit behind the same shared authenticator.
		ping := func(hdr map[string]string) int {
			req, _ := http.NewRequest(http.MethodGet, h.srv.URL+"/v2/", nil)
			for k, v := range hdr {
				req.Header.Set(k, v)
			}
			resp, err := h.srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET /v2/: %v", err)
			}
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		if code := ping(nil); code != http.StatusUnauthorized {
			t.Fatalf("anonymous /v2/ ping on closed instance = %d, want 401", code)
		}
		if code := ping(c.cookieHdr()); code != http.StatusOK {
			t.Fatalf("cookie /v2/ ping = %d, want 200 (third arm on the adapter plane)", code)
		}
	})

	t.Run("whoami answers for every arm (CE-04)", func(t *testing.T) {
		basic := h.do(http.MethodGet, "/binflow/api/v1/session", adminUser, adminPass, nil, nil)
		if basic.StatusCode != http.StatusOK || !strings.Contains(mustGet(t, basic), adminUser) {
			t.Fatalf("Basic whoami = %d", basic.StatusCode)
		}
	})
}

// ---- CE-06: CSRF Origin guard (W38) ----

// csrfArm carries one table row's credential shape: Basic rides (user,pass),
// the cookie and token ride headers.
type csrfArm struct {
	user string
	pass string
	hdr  map[string]string
}

// TestCSRFOriginMatrix: the Origin verdict for cookie-authenticated writes —
// cross-origin 403, no-Origin/same-origin pass, Basic/Token immune, GET
// exempt — and the guard runs before routing (a cross-origin DELETE on a
// real route meets the 403, not the route's own response).
func TestCSRFOriginMatrix(t *testing.T) {
	h := newHarness(t)
	createRepoHTTP(t, h, "generic-local")
	c := mustLogin(t, h)
	tok := mintAdminToken(t, h)
	cross := map[string]string{"Origin": "http://evil.example"}

	contentPUT := func(arm csrfArm, hdr map[string]string) int {
		resp := h.do(http.MethodPut, "/binflow/generic-local/acme/csrf.bin",
			arm.user, arm.pass, []byte("x"), mergeHdr(arm.hdr, hdr))
		_ = mustGet(t, resp)
		return resp.StatusCode
	}

	cookieArm := csrfArm{hdr: c.cookieHdr()}
	basicArm := csrfArm{user: adminUser, pass: adminPass}
	tokenArm := csrfArm{hdr: map[string]string{"X-JFrog-Art-Api": tok}}

	for _, tc := range []struct {
		name string
		arm  csrfArm
		hdr  map[string]string
		want int
	}{
		{"cookie + cross-origin write is 403 (W38 leg 1)", cookieArm, cross, http.StatusForbidden},
		{"cookie + no Origin passes (W38 leg 2)", cookieArm, nil, http.StatusCreated},
		{"cookie + same-origin passes", cookieArm, map[string]string{"Origin": h.srv.URL}, http.StatusCreated},
		{"cookie + Origin null is 403", cookieArm, map[string]string{"Origin": "null"}, http.StatusForbidden},
		{"Basic + cross-origin is immune (CI posture)", basicArm, cross, http.StatusCreated},
		{"token header + cross-origin is immune", tokenArm, cross, http.StatusCreated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := contentPUT(tc.arm, tc.hdr); got != tc.want {
				t.Fatalf("PUT verdict = %d, want %d", got, tc.want)
			}
		})
	}

	t.Run("cookie GET is exempt (reads)", func(t *testing.T) {
		if code := contentPUT(cookieArm, nil); code != http.StatusCreated {
			t.Fatalf("seed PUT = %d", code)
		}
		resp := h.do(http.MethodGet, "/binflow/generic-local/acme/csrf.bin", "", "", nil,
			mergeHdr(c.cookieHdr(), cross))
		_ = mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET with cross-origin Origin = %d, want 200 (GET exempt)", resp.StatusCode)
		}
	})

	t.Run("guard precedes routing on a real route", func(t *testing.T) {
		// Cross-origin cookie DELETE on /api/v1/session: the CSRF verdict
		// (403) must outrank the route's own 204 (W39's ordering property).
		resp := h.do(http.MethodDelete, "/binflow/api/v1/session", "", "", nil,
			mergeHdr(c.cookieHdr(), cross))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin logout = %d, want 403 (CSRF before handler)", resp.StatusCode)
		}
		// The session survives the rejected logout.
		who := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil, c.cookieHdr())
		if who.StatusCode != http.StatusOK {
			t.Fatalf("session revoked by a rejected cross-origin logout: %d", who.StatusCode)
		}
	})

	t.Run("stale cookie with cross-origin Origin is 401 (auth precedes CSRF)", func(t *testing.T) {
		resp := h.do(http.MethodPut, "/binflow/generic-local/acme/x.bin", "", "", []byte("x"),
			map[string]string{"Cookie": "binflow_session=stale", "Origin": "http://evil.example"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("stale cookie verdict = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("X-Forwarded-Proto https matches an https Origin", func(t *testing.T) {
		origin := strings.Replace(h.srv.URL, "http://", "https://", 1)
		hdr := map[string]string{"Origin": origin, "X-Forwarded-Proto": "https"}
		if got := contentPUT(cookieArm, hdr); got != http.StatusCreated {
			t.Fatalf("TLS-terminated same-origin PUT = %d, want 201", got)
		}
	})

	t.Run("SPA habit header is neither required nor rejected", func(t *testing.T) {
		hdr := map[string]string{"X-BinFlow-Console": "1"}
		if got := contentPUT(cookieArm, hdr); got != http.StatusCreated {
			t.Fatalf("X-BinFlow-Console PUT = %d, want 201 (layer 3 is a habit, not a gate)", got)
		}
	})
}

// ---- review fixes: B1 (stale-cookie login DoS) + B2 (login-CSRF) ----

// seedStaleSessionCookie plants a session row directly through the store so
// a test can carry a cookie whose row is expired ("" = no row at all — the
// tossed-cookie shape).
func seedStaleSessionCookie(t *testing.T, h *harness, id, expiresAt string) {
	t.Helper()
	if expiresAt == "" {
		return // no row: an unknown/tossed cookie value
	}
	err := h.md.WebSessions().Create(context.Background(), &metadata.WebSession{
		IDHash:     sha256Hex([]byte(id)),
		Username:   adminUser,
		CreatedAt:  time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		ExpiresAt:  expiresAt,
		LastUsedAt: time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("seed stale session row: %v", err)
	}
}

// TestLoginEndpointResistsStaleCookies (B1, security review): the login
// endpoint is the expected destination of stale cookies — unknown (tossed
// by a sibling subdomain), expired, revoked — and must judge the request on
// its OWN credentials, never on the ambient cookie. Without the dispatch
// exemption, a tossed garbage cookie turned every correct login into an
// indefinite 401.
func TestLoginEndpointResistsStaleCookies(t *testing.T) {
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	cases := []struct {
		name string
		id   string
		row  string // "" = no row; otherwise the expires_at stamp to seed
	}{
		{"tossed unknown cookie", "attacker-chosen-garbage", ""},
		{"expired row cookie", "expired-fixture-id", past},
	}
	for _, tc := range cases {
		t.Run(tc.name+" + correct credentials -> 200", func(t *testing.T) {
			h := newHarness(t)
			seedStaleSessionCookie(t, h, tc.id, tc.row)
			body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
			resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
				map[string]string{
					"Content-Type": "application/json",
					"Cookie":       "binflow_session=" + tc.id,
				})
			got := mustGet(t, resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("login with stale cookie = %d, want 200 (B1); body=%s", resp.StatusCode, got)
			}
			if resp.Header.Get("Set-Cookie") == "" {
				t.Fatal("login issued no fresh Set-Cookie")
			}
		})
	}

	t.Run("revoked-row cookie + correct credentials -> 200", func(t *testing.T) {
		h := newHarness(t)
		resp0, c0 := loginJSON(t, h, adminUser, adminPass)
		if resp0.StatusCode != http.StatusOK {
			t.Fatalf("setup login = %d", resp0.StatusCode)
		}
		out := h.do(http.MethodDelete, "/binflow/api/v1/session", "", "", nil, c0.cookieHdr())
		if out.StatusCode != http.StatusNoContent {
			t.Fatalf("setup logout = %d", out.StatusCode)
		}
		body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
			map[string]string{
				"Content-Type": "application/json",
				"Cookie":       "binflow_session=" + c0.value,
			})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("relogin with revoked cookie = %d, want 200 (B1); body=%s",
				resp.StatusCode, mustGet(t, resp))
		}
	})

	t.Run("stale cookie + WRONG credentials still 401, uniform", func(t *testing.T) {
		h := newHarness(t)
		stale := map[string]string{
			"Content-Type": "application/json",
			"Cookie":       "binflow_session=attacker-chosen-garbage",
		}
		wrongPw, _ := login(t, h, `{"username":"admin","password":"wrong"}`, "application/json")
		// The uniform-wording comparison needs the same header shape minus
		// the cookie: re-issue without it.
		noCookie, _ := login(t, h, `{"username":"admin","password":"wrong"}`, "application/json")
		_ = wrongPw
		withCookie := h.do(http.MethodPost, "/binflow/api/v1/session", "", "",
			[]byte(`{"username":"admin","password":"wrong"}`), stale)
		b1, b2 := mustGet(t, withCookie), mustGet(t, noCookie)
		if withCookie.StatusCode != http.StatusUnauthorized {
			t.Fatalf("stale cookie + wrong password = %d, want 401", withCookie.StatusCode)
		}
		if b1 != b2 {
			t.Fatalf("401 wording changed under a stale cookie:\n%s\n%s", b1, b2)
		}
	})

	t.Run("exemption is login-only: stale cookie still 401 elsewhere", func(t *testing.T) {
		h := newHarness(t)
		resp := h.do(http.MethodGet, "/binflow/api/v1/session", "", "", nil,
			map[string]string{"Cookie": "binflow_session=attacker-chosen-garbage"})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("whoami with stale cookie = %d, want 401 (exemption must not leak)", resp.StatusCode)
		}
	})
}

// TestLoginOriginGuard (B2, security review): cross-origin login posts are
// refused — login-CSRF needs no victim cookie (the response's Set-Cookie
// writes into the victim's browser regardless of SameSite), so the entry
// carries its own Origin verdict. No Origin / same-origin pass (curl, CI
// and the SPA are untouched).
func TestLoginOriginGuard(t *testing.T) {
	h := newHarness(t)
	cross := map[string]string{"Origin": "http://evil.example"}

	t.Run("cross-origin JSON login -> 403, no Set-Cookie", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
			mergeHdr(map[string]string{"Content-Type": "application/json"}, cross))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin JSON login = %d, want 403 (B2)", resp.StatusCode)
		}
		if sc := resp.Header.Get("Set-Cookie"); sc != "" {
			t.Fatalf("cross-origin login issued a cookie: %s", sc)
		}
	})

	t.Run("cross-origin form login -> 403 (the browser attack shape)", func(t *testing.T) {
		form := url.Values{"username": {adminUser}, "password": {adminPass}}.Encode()
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", []byte(form),
			mergeHdr(map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, cross))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin form login = %d, want 403 (B2)", resp.StatusCode)
		}
	})

	t.Run("no Origin passes (CI posture)", func(t *testing.T) {
		resp, _ := loginJSON(t, h, adminUser, adminPass)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("no-Origin login = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("same-origin Origin passes", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
			mergeHdr(map[string]string{"Content-Type": "application/json"},
				map[string]string{"Origin": h.srv.URL}))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("same-origin login = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("stale cookie + cross-origin Origin -> 403 (B1 and B2 interlock)", func(t *testing.T) {
		// After B1 exempts the route from the hard-401, the Origin verdict
		// is what stops the combined probe.
		body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
			mergeHdr(map[string]string{
				"Content-Type": "application/json",
				"Cookie":       "binflow_session=attacker-chosen-garbage",
			}, cross))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("stale cookie + cross-origin login = %d, want 403 (B2 after B1)", resp.StatusCode)
		}
	})

	t.Run("live session + cross-origin re-login -> 403 via csrfGuard", func(t *testing.T) {
		// Already authenticated: the write guard (not the login entry)
		// owns the verdict.
		c := mustLogin(t, h)
		body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
		resp := h.do(http.MethodPost, "/binflow/api/v1/session", "", "", body,
			mergeHdr(map[string]string{"Content-Type": "application/json"}, c.cookieHdr(), cross))
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("authenticated cross-origin re-login = %d, want 403", resp.StatusCode)
		}
	})
}

// mintAdminToken mints one API token through the management plane.
func mintAdminToken(t *testing.T, h *harness) string {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte("grant_type=client_credentials"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint token = %d, body=%s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("mint token body: %s (%v)", body, err)
	}
	return tok.AccessToken
}

// ---- helpers ----

// mergeHdr unions header maps (later maps win on key collisions).
func mergeHdr(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
