package httpapi

// T-362's REST legs, driven straight into dispatchEventAPI (the conductor
// wires the router case; this test is the plane's own contract): the
// seven endpoints' status codes and bodies per webhook.md section 1, the
// role gates (401 anonymous, 403 user, readonly_admin read-only), the
// webhook slot's three seams (community 403 + license header, pro 200,
// the addons.disabled breaker's no-header refusal) and the troubleshooting
// read.

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// fakeLicenseEval is the Deps.License collaborator over a fixed verdict
// (the three gate arms without the document machinery): the LicenseManager
// face plus the addons.Evaluator facet New discovers.
type fakeLicenseEval struct {
	tier     license.Tier
	licensed bool
	disabled bool
}

func (f fakeLicenseEval) AddonEnabled(_ context.Context, _ string, minTier license.Tier) bool {
	if f.disabled {
		return false
	}
	return f.tier >= minTier
}

func (f fakeLicenseEval) State() license.State {
	return license.State{Tier: f.tier, Licensed: f.licensed, Perpetual: true}
}

func (f fakeLicenseEval) Install(context.Context, string) (license.State, error) {
	return f.State(), nil
}

func (f fakeLicenseEval) Uninstall(context.Context) error { return nil }

// t362Stack is the plane's own assembly: real auth.Service (the route
// gates' CanManage), real migrated store, real Bus, a controllable
// license facet.
type t362Stack struct {
	s     *Server
	store *webhook.SQLiteStore
	bus   *webhook.Bus
	md    metadata.Store
}

func newT362Stack(t *testing.T, eval fakeLicenseEval) *t362Stack {
	t.Helper()
	ctx := context.Background()
	path := t.TempDir() + "/binflow.db"
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	dsn := "file:" + url.PathEscape(path) + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store := webhook.NewSQLiteStore(db)
	unlocked := eval.AddonEnabled(ctx, "webhook", license.TierPro)
	bus, err := webhook.NewBus(webhook.BusOptions{
		Store:              store,
		Repos:              md.Repos(),
		Gate:               func(context.Context) bool { return unlocked },
		Origin:             "https://binflow.example.com",
		AllowPrivateTarget: true,
	})
	if err != nil {
		t.Fatalf("NewBus: %v", err)
	}
	cfg := config.Defaults()
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	reg := addons.New(
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Properties(), addons.RepoOperations(), addons.Trashcan(),
		addons.HA(), addons.XrayIntegration(), addons.Webhook(),
	)
	s := New(Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		Webhooks: bus,
		Addons:   reg,
		License:  eval,
	}, nil)
	return &t362Stack{s: s, store: store, bus: bus, md: md}
}

// do drives one request into the plane with a boxed principal (the
// authenticate middleware's output shape; the conductor's router case
// lands between them in production).
func (st *t362Stack) do(t *testing.T, method, rest, body string, p *auth.Principal) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/binflow/event"+rest, rdr)
	if p != nil {
		req = req.WithContext(withPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	// dispatch receives the path only (EscapedPath excludes the query —
	// the production router's spelling).
	pathREST, _, _ := strings.Cut(rest, "?")
	st.s.dispatchEventAPI(rec, req, pathREST)
	res := rec.Result()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res.Header
}

var (
	t362Admin  = &auth.Principal{Name: "admin", Role: auth.RoleAdmin, Admin: true, Source: auth.ProviderLocal}
	t362Ro     = &auth.Principal{Name: "roat", Role: auth.RoleReadOnlyAdmin, Source: auth.ProviderLocal}
	t362User   = &auth.Principal{Name: "plain", Role: auth.RoleUser, Source: auth.ProviderLocal}
	t362NoName = &auth.Principal{Name: "tok", Role: auth.RoleAdmin, Admin: true, TokenID: 7, Source: auth.ProviderLocal}
)

const t362CreateBody = `{
	"key": "ci-deploys",
	"description": "CI notifications",
	"enabled": true,
	"event_filter": {"domain": "artifact", "event_types": ["deployed", "deleted"], "criteria": {"anyLocal": true}},
	"handlers": [{"handler_type": "webhook", "url": "https://ci.example.com/hook"}]
}`

func TestT362RESTCRUDWire(t *testing.T) {
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})

	// POST: 201 + the stored echo.
	code, body, _ := st.do(t, http.MethodPost, "/api/v1/subscriptions", t362CreateBody, t362Admin)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s, want 201", code, body)
	}
	var echo struct {
		Key      string `json:"key"`
		Enabled  bool   `json:"enabled"`
		Handlers []struct {
			HandlerType string `json:"handler_type"`
			URL         string `json:"url"`
		} `json:"handlers"`
		EventFilter struct {
			Domain     string          `json:"domain"`
			EventTypes []string        `json:"event_types"`
			Criteria   json.RawMessage `json:"criteria"`
		} `json:"event_filter"`
	}
	if err := json.Unmarshal([]byte(body), &echo); err != nil {
		t.Fatalf("echo decode: %v (%s)", err, body)
	}
	if echo.Key != "ci-deploys" || !echo.Enabled || len(echo.Handlers) != 1 ||
		echo.Handlers[0].HandlerType != "webhook" || echo.EventFilter.Domain != "artifact" ||
		len(echo.EventFilter.EventTypes) != 2 || string(echo.EventFilter.Criteria) == "" {
		t.Fatalf("echo shape: %s", body)
	}

	// GET list: the bare array.
	code, body, _ = st.do(t, http.MethodGet, "/api/v1/subscriptions", "", t362Admin)
	if code != http.StatusOK || !strings.Contains(body, `"ci-deploys"`) {
		t.Fatalf("list = %d %s", code, body)
	}

	// GET one: 200; missing: the table's exact 404 wording.
	code, body, _ = st.do(t, http.MethodGet, "/api/v1/subscriptions/ci-deploys", "", t362Admin)
	if code != http.StatusOK {
		t.Fatalf("get = %d %s, want 200", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "/api/v1/subscriptions/missing", "", t362Admin)
	if code != http.StatusNotFound || !strings.Contains(body, "Subscription not found") {
		t.Fatalf("get missing = %d %s, want 404 'Subscription not found'", code, body)
	}

	// PUT: 204 with no body; the flip lands.
	flipped := strings.Replace(t362CreateBody, `"enabled": true`, `"enabled": false`, 1)
	code, body, _ = st.do(t, http.MethodPut, "/api/v1/subscriptions/ci-deploys", flipped, t362Admin)
	if code != http.StatusNoContent || body != "" {
		t.Fatalf("update = %d %q, want 204 no body", code, body)
	}
	_, body, _ = st.do(t, http.MethodGet, "/api/v1/subscriptions/ci-deploys", "", t362Admin)
	if strings.Contains(body, `"enabled": true`) {
		t.Fatalf("update did not flip: %s", body)
	}

	// DELETE: 204 then 404.
	code, _, _ = st.do(t, http.MethodDelete, "/api/v1/subscriptions/ci-deploys", "", t362Admin)
	if code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	code, _, _ = st.do(t, http.MethodDelete, "/api/v1/subscriptions/ci-deploys", "", t362Admin)
	if code != http.StatusNotFound {
		t.Fatalf("re-delete = %d, want 404", code)
	}
}

func TestT362RESTValidation400s(t *testing.T) {
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	cases := []struct {
		name string
		body string
		want string
	}{
		{"key pattern", strings.Replace(t362CreateBody, `"ci-deploys"`, `"1ci"`, 1), "key must match"},
		{"unknown type", strings.Replace(t362CreateBody, `"deleted"`, `"promoted"`, 1), "not registered"},
		{"unknown criteria", strings.Replace(t362CreateBody, `"anyLocal": true`, `"anyLocal": true, "oops": 1`, 1), "unknown key"},
		{"two handlers", strings.Replace(t362CreateBody,
			`"url": "https://ci.example.com/hook"}]`,
			`"url": "https://ci.example.com/hook"},{"handler_type":"webhook","url":"https://x.example.com"}]`, 1), "exactly one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body, _ := st.do(t, http.MethodPost, "/api/v1/subscriptions", tc.body, t362Admin)
			if code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
				t.Fatalf("create = %d %s, want 400 containing %q", code, body, tc.want)
			}
		})
	}
	// Zero side effects: nothing persisted by the refused creates.
	subs, err := st.bus.List(context.Background())
	if err != nil || len(subs) != 0 {
		t.Fatalf("refused creates persisted %d rows (%v)", len(subs), err)
	}
}

func TestT362RESTRoleGates(t *testing.T) {
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})

	// Anonymous: 401 (the route's required:true).
	code, _, _ := st.do(t, http.MethodGet, "/api/v1/subscriptions", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", code)
	}
	// Plain user: everything 403, zero side effects.
	for _, arm := range []struct {
		method, rest string
		body         string
	}{
		{http.MethodGet, "/api/v1/subscriptions", ""},
		{http.MethodPost, "/api/v1/subscriptions", t362CreateBody},
		{http.MethodGet, "/api/v1/subscriptions/ci-deploys", ""},
		{http.MethodPut, "/api/v1/subscriptions/ci-deploys", t362CreateBody},
		{http.MethodDelete, "/api/v1/subscriptions/ci-deploys", ""},
		{http.MethodPost, "/api/v1/subscriptions/test", t362CreateBody},
		{http.MethodGet, "/api/v1/troubleshooting", ""},
	} {
		code, body, _ := st.do(t, arm.method, arm.rest, arm.body, t362User)
		if code != http.StatusForbidden {
			t.Fatalf("user %s %s = %d %s, want 403", arm.method, arm.rest, code, body)
		}
	}
	if subs, _ := st.bus.List(context.Background()); len(subs) != 0 {
		t.Fatalf("user-gated writes persisted %d rows", len(subs))
	}
	// readonly_admin: reads pass, writes 403 (FR-115.5).
	if code, _, _ := st.do(t, http.MethodGet, "/api/v1/subscriptions", "", t362Ro); code != http.StatusOK {
		t.Fatalf("readonly list = %d, want 200", code)
	}
	if code, _, _ := st.do(t, http.MethodGet, "/api/v1/troubleshooting", "", t362Ro); code != http.StatusOK {
		t.Fatalf("readonly troubleshooting = %d, want 200", code)
	}
	if code, _, _ := st.do(t, http.MethodPost, "/api/v1/subscriptions", t362CreateBody, t362Ro); code != http.StatusForbidden {
		t.Fatalf("readonly create = %d, want 403", code)
	}
}

func TestT362RESTLicenseSeams(t *testing.T) {
	// Community tier: writes 403 carrying the license header; reads 200.
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierCommunity})
	code, body, hdr := st.do(t, http.MethodPost, "/api/v1/subscriptions", t362CreateBody, t362Admin)
	if code != http.StatusForbidden {
		t.Fatalf("community create = %d %s, want 403", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "webhook" {
		t.Fatalf("license header = %q, want webhook", got)
	}
	if code, _, _ := st.do(t, http.MethodGet, "/api/v1/subscriptions", "", t362Admin); code != http.StatusOK {
		t.Fatalf("community read = %d, want 200 (D1)", code)
	}

	// The addons.disabled breaker: 403 with the breaker wording and NO
	// license header (the refusal installing a license cannot clear).
	brk := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, disabled: true})
	code, body, hdr = brk.do(t, http.MethodPost, "/api/v1/subscriptions", t362CreateBody, t362Admin)
	if code != http.StatusForbidden {
		t.Fatalf("breaker create = %d %s, want 403", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != "" {
		t.Fatalf("breaker must not carry the license header, got %q", got)
	}
	if !strings.Contains(body, "addons.disabled") {
		t.Fatalf("breaker wording: %s", body)
	}
	if code, _, _ := brk.do(t, http.MethodGet, "/api/v1/subscriptions", "", t362Admin); code != http.StatusOK {
		t.Fatalf("breaker read = %d, want 200", code)
	}
}

func TestT362RESTTestEndpointAndTroubleshooting(t *testing.T) {
	var seen []string
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(receiver.Close)

	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	draft := strings.Replace(t362CreateBody,
		`"url": "https://ci.example.com/hook"`, `"url": "`+receiver.URL+`"`, 1)
	// debug:true: even a successful trial send leaves its排障 record
	// (webhook.md section 7) — the query below reads it back.
	draft = strings.Replace(draft, `"enabled": true`, `"enabled": true, "debug": true`, 1)
	code, body, _ := st.do(t, http.MethodPost, "/api/v1/subscriptions/test", draft, t362NoName)
	if code != http.StatusOK {
		t.Fatalf("test = %d %s, want 200", code, body)
	}
	var out struct {
		Message string `json:"message"`
		OK      bool   `json:"ok"`
		Attempt struct {
			StatusCode    int    `json:"status_code"`
			ElapsedMillis int64  `json:"elapsed_millis"`
			Error         string `json:"error"`
		} `json:"attempt"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("test outcome decode: %v (%s)", err, body)
	}
	if !out.OK || out.Attempt.StatusCode != 200 || out.Message != "Test successful" {
		t.Fatalf("outcome = %+v", out)
	}
	if len(seen) != 1 {
		t.Fatalf("draft fired %d sends, want 1", len(seen))
	}
	// The draft never persisted.
	if subs, _ := st.bus.List(context.Background()); len(subs) != 0 {
		t.Fatalf("test endpoint persisted %d subscriptions", len(subs))
	}
	// The token principal rides userContext (isToken + the id).
	var env struct {
		UserContext struct {
			ID      string `json:"id"`
			IsToken bool   `json:"isToken"`
			Realm   string `json:"realm"`
		} `json:"userContext"`
	}
	if err := json.Unmarshal([]byte(seen[0]), &env); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	if env.UserContext.ID != "tok" || !env.UserContext.IsToken || env.UserContext.Realm != "internal" {
		t.Fatalf("userContext: %+v", env.UserContext)
	}

	// Troubleshooting: the failure-recorded send is queryable.
	code, body, _ = st.do(t, http.MethodGet, "/api/v1/troubleshooting?subscription=ci-deploys", "", t362Admin)
	if code != http.StatusOK || !strings.Contains(body, `"subscription_key": "ci-deploys"`) {
		t.Fatalf("troubleshooting = %d %s", code, body)
	}
}

func TestT362RESTNilPlane503(t *testing.T) {
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	st.s.deps.Webhooks = nil
	code, body, _ := st.do(t, http.MethodGet, "/api/v1/subscriptions", "", t362Admin)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("nil plane = %d %s, want 503", code, body)
	}
}

func TestT362RESTUnknownSpelling404(t *testing.T) {
	st := newT362Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	code, _, _ := st.do(t, http.MethodGet, "/api/v1/webhooks", "", t362Admin)
	if code != http.StatusNotFound {
		t.Fatalf("unknown spelling = %d, want the E-26 404", code)
	}
	code, _, _ = st.do(t, http.MethodPatch, "/api/v1/subscriptions", "", t362Admin)
	if code != http.StatusNotFound {
		t.Fatalf("unknown verb = %d, want 404", code)
	}
}
