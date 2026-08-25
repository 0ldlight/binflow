package httpapi_test

// T-279 AC 3 (REST three-endpoint legs): the license plane over a REAL
// stack — sqlite metadata, real auth.Service (admin/readonly_admin/plain
// user), real router chain — with the license.Manager verifying against a
// per-test key pair (the ADR-mandated injection seam; the embedded
// production constant is never consulted). Legs: GET floor state, install
// 201, tampered/forged/expired 400 with state untouched, DELETE 200 +
// community, the readonly_admin GET-200/write-403 split, audit
// license.install/delete/invalid rows, doc-echo and log redaction
// (NFR-S52), and the LC-02 negative (the Artifactory plural path never
// installs anything).

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// licenseStack is the self-contained T-279 assembly (the shared harness
// predates the M10 collaborator; this file owns its own wiring).
type licenseStack struct {
	ts   *httptest.Server
	md   metadata.Store
	logs *logSink
	keys testKeys
}

func newLicenseStack(t *testing.T) *licenseStack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)

	// readonly_admin + plain user fixtures (the AC-7 permission legs).
	seedLicenseUser(t, md, "roat", "roat-pw", "readonly_admin")
	seedLicenseUser(t, md, "plain", "plain-pw", "user")

	k := newTestKeys(t)
	sink := &logSink{}
	logger := newSlogTo(sink)
	mgr, err := license.New(license.Options{
		Store:      md.Licenses(),
		VerifyKeys: k.keys,
		Audit:      audit.BestEffort(audit.New(md, true)),
		Log:        logger,
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		License:  mgr,
	}, logger)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &licenseStack{ts: ts, md: md, logs: sink, keys: k}
}

func seedLicenseUser(t *testing.T, md metadata.Store, name, pass, role string) {
	t.Helper()
	hash, err := auth.HashPassword(pass)
	if err != nil {
		t.Fatalf("hash %s: %v", name, err)
	}
	ctx := context.Background()
	if err := md.Users().Create(ctx, &metadata.User{
		Username: name, PasswordHash: hash, Enabled: true, Role: role,
		IsAdmin: role == "admin",
	}); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
}

// logSink and helpers (this file's own copy — the shared harness owns a
// private one already paired to its constructor).
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, string(p))
	return len(p), nil
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "")
}

func newSlogTo(sink *logSink) *slog.Logger {
	return slog.New(slog.NewTextHandler(sink, nil))
}

// do issues one request against the stack with optional Basic auth.
func (st *licenseStack) do(t *testing.T, method, path, user, pass, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, st.ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (st *licenseStack) get(t *testing.T, path, user, pass string) (int, string) {
	return st.do(t, http.MethodGet, path, user, pass, "")
}

// licenseGetBody decodes the GET body.
func licenseGetBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("GET body not JSON: %v (%s)", err, body)
	}
	return m
}

// TestLicenseEndpointsLifecycle: the install/query/uninstall closed loop
// (L02/L04 shape) plus the anti-forgery legs (L03) — every rejection
// leaves the previous state in force (D7).
func TestLicenseEndpointsLifecycle(t *testing.T) {
	st := newLicenseStack(t)
	const p = "/binflow/api/system/license"

	// Unlicensed floor (L04).
	code, body := st.get(t, p, adminUser, adminPass)
	if code != http.StatusOK {
		t.Fatalf("floor GET = %d %s", code, body)
	}
	m := licenseGetBody(t, body)
	if m["licensed"] != false || m["tier"] != "community" {
		t.Fatalf("floor body wrong: %s", body)
	}

	// Anonymous GET: the management-plane 401 challenge.
	if code, _ := st.get(t, p, "", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", code)
	}

	// Install a valid pro document: 201, immediate effect.
	pro := st.keys.spec(time.Now().UTC()).sign(t)
	code, body = st.do(t, http.MethodPost, p, adminUser, adminPass, pro)
	if code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
	m = licenseGetBody(t, body)
	if m["tier"] != "pro" || m["licensee"] != "Acme Corp" || m["licensed"] != true {
		t.Fatalf("post-install body wrong: %s", body)
	}
	_, body = st.get(t, p, adminUser, adminPass)
	m = licenseGetBody(t, body)
	if m["tier"] != "pro" || m["expiresAt"] == "" {
		t.Fatalf("GET after install wrong: %s", body)
	}
	if days, ok := m["daysToExpiry"].(float64); !ok || days < 360 {
		t.Fatalf("daysToExpiry wrong: %v (%s)", m["daysToExpiry"], body)
	}

	// The document itself never rides a response (no doc echo, NFR-S52).
	if strings.Contains(body, strings.Split(pro, ".")[0]) {
		t.Fatalf("GET echoes the payload segment: %s", body)
	}

	// Anti-forgery (L03): tampered, foreign-key and expired documents all
	// 400, and the pro state survives each refusal (D7).
	foreign := newTestKeys(t).spec(time.Now().UTC()).sign(t)
	expiredSpec := st.keys.spec(time.Now().UTC())
	e := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	expiredSpec.expiresAt = &e
	expiredDoc := expiredSpec.sign(t)
	for _, bad := range []string{
		"not-a-license",
		tamperPayloadContent(t, pro),
		foreign,
		expiredDoc,
	} {
		if code, body = st.do(t, http.MethodPost, p, adminUser, adminPass, bad); code != http.StatusBadRequest {
			t.Fatalf("forged install = %d %s (doc %.30s)", code, body, bad)
		}
		_, body = st.get(t, p, adminUser, adminPass)
		if m = licenseGetBody(t, body); m["tier"] != "pro" {
			t.Fatalf("rejected install changed the state: %s", body)
		}
	}

	// The expired refusal names LICENSE_EXPIRED; the others collapse to
	// LICENSE_INVALID without internal detail.
	if _, body = st.do(t, http.MethodPost, p, adminUser, adminPass, "not-a-license"); !strings.Contains(body, "LICENSE_INVALID") {
		t.Fatalf("invalid-class message wrong: %s", body)
	}
	if _, body = st.do(t, http.MethodPost, p, adminUser, adminPass, expiredDoc); !strings.Contains(body, "LICENSE_EXPIRED") {
		t.Fatalf("expired-class message wrong: %s", body)
	}

	// Uninstall: 200 + the exact ADR text + the floor.
	code, body = st.do(t, http.MethodDelete, p, adminUser, adminPass, "")
	if code != http.StatusOK || strings.TrimSpace(body) != "License removed successfully." {
		t.Fatalf("uninstall = %d %q", code, body)
	}
	_, body = st.get(t, p, adminUser, adminPass)
	if m = licenseGetBody(t, body); m["tier"] != "community" || m["licensed"] != false {
		t.Fatalf("post-uninstall body wrong: %s", body)
	}
	// Idempotent uninstall.
	if code, _ = st.do(t, http.MethodDelete, p, adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("idempotent uninstall = %d", code)
	}
}

// TestLicenseEndpointsPermissions (AC-7): readonly_admin reads the state
// and cannot write; a plain user cannot even read.
func TestLicenseEndpointsPermissions(t *testing.T) {
	st := newLicenseStack(t)
	const p = "/binflow/api/system/license"
	doc := st.keys.spec(time.Now().UTC()).sign(t)

	if code, body := st.get(t, p, "roat", "roat-pw"); code != http.StatusOK {
		t.Fatalf("readonly_admin GET = %d %s", code, body)
	}
	if code, _ := st.do(t, http.MethodPost, p, "roat", "roat-pw", doc); code != http.StatusForbidden {
		t.Fatalf("readonly_admin POST = %d, want 403", code)
	}
	if code, _ := st.do(t, http.MethodDelete, p, "roat", "roat-pw", ""); code != http.StatusForbidden {
		t.Fatalf("readonly_admin DELETE = %d, want 403", code)
	}
	if code, _ := st.get(t, p, "plain", "plain-pw"); code != http.StatusForbidden {
		t.Fatalf("plain user GET = %d, want 403", code)
	}
	// The refused POST changed nothing.
	if code, body := st.get(t, p, adminUser, adminPass); code != http.StatusOK {
		t.Fatalf("admin GET = %d %s", code, body)
	} else if m := licenseGetBody(t, body); m["licensed"] != false {
		t.Fatalf("refused readonly_admin install leaked state: %s", body)
	}
}

// TestLicenseAuditTrail: install/delete/invalid events land with actor and
// the license facts (the one surface allowed to carry the licensee).
func TestLicenseAuditTrail(t *testing.T) {
	st := newLicenseStack(t)
	const p = "/binflow/api/system/license"
	pro := st.keys.spec(time.Now().UTC()).sign(t)

	if code, body := st.do(t, http.MethodPost, p, adminUser, adminPass, pro); code != http.StatusCreated {
		t.Fatalf("install = %d %s", code, body)
	}
	if code, _ := st.do(t, http.MethodPost, p, adminUser, adminPass, "garbage"); code != http.StatusBadRequest {
		t.Fatal("garbage accepted")
	}
	if code, _ := st.do(t, http.MethodDelete, p, adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatal("delete failed")
	}

	events, err := st.md.Audits().Query(context.Background(), metadata.AuditQuery{})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	got := map[string]bool{}
	for _, e := range events {
		if !strings.HasPrefix(e.Action, "license.") {
			continue
		}
		got[e.Action] = true
		if e.Actor != "admin" {
			t.Fatalf("%s actor = %q, want admin", e.Action, e.Actor)
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &d); err != nil {
			t.Fatalf("%s detail not JSON: %v", e.Action, err)
		}
		if e.Action != license.ActionInvalid {
			if d["tier"] != "pro" || d["licensee"] != "Acme Corp" || d["expiresAt"] == nil {
				t.Fatalf("%s detail missing facts: %s", e.Action, e.Detail)
			}
		}
	}
	for _, want := range []string{license.ActionInstall, license.ActionDelete, license.ActionInvalid} {
		if !got[want] {
			t.Fatalf("audit action %q missing (have %v)", want, got)
		}
	}
}

// TestLicenseLogRedaction (NFR-S52): through install, rejection and
// uninstall, neither the licensee nor any document segment reaches the log
// stream.
func TestLicenseLogRedaction(t *testing.T) {
	st := newLicenseStack(t)
	const p = "/binflow/api/system/license"
	doc := st.keys.spec(time.Now().UTC()).sign(t)

	if code, _ := st.do(t, http.MethodPost, p, adminUser, adminPass, doc); code != http.StatusCreated {
		t.Fatal("install failed")
	}
	if code, _ := st.do(t, http.MethodPost, p, adminUser, adminPass, "garbage"); code != http.StatusBadRequest {
		t.Fatal("garbage accepted")
	}
	if code, _ := st.do(t, http.MethodDelete, p, adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatal("delete failed")
	}

	logs := st.logs.String()
	if strings.Contains(logs, "Acme Corp") {
		t.Fatalf("licensee leaked into logs: %s", logs)
	}
	for _, seg := range strings.Split(doc, ".") {
		if seg != "" && strings.Contains(logs, seg) {
			t.Fatalf("document segment leaked into logs: %s", logs)
		}
	}
}

// TestLicenseArtifactoryPluralPath (LC-02 negative): /api/system/licenses
// has no route — the E-26 guidance, so a JFrog-format document has zero
// chance of installing.
func TestLicenseArtifactoryPluralPath(t *testing.T) {
	st := newLicenseStack(t)
	code, body := st.do(t, http.MethodPost, "/binflow/api/system/licenses", adminUser, adminPass, "jfrog-doc")
	if code != http.StatusNotFound || !strings.Contains(body, "not implemented") {
		t.Fatalf("plural path = %d %s, want the E-26 404", code, body)
	}
}

// TestLicenseNilManagerFloor: a stack assembled without the collaborator
// keeps GET at the honest floor and refuses the mutations (the
// Docs-handler default pattern).
func TestLicenseNilManagerFloor(t *testing.T) {
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/b.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	defer func() { _ = md.Close() }()
	cfg := config.Defaults()
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, false)
	s := httpapi.New(httpapi.Deps{
		Config: cfg, Auth: authSvc, Authz: authSvc, Metadata: md, Repos: md.Repos(),
	}, nil)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	do := func(method string) (int, string) {
		req, _ := http.NewRequest(method, ts.URL+"/binflow/api/system/license", nil)
		req.SetBasicAuth(adminUser, adminPass)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := do(http.MethodGet); code != http.StatusOK || !strings.Contains(body, `"community"`) {
		t.Fatalf("nil-manager GET = %d %s", code, body)
	}
	if code, _ := do(http.MethodPost); code != http.StatusServiceUnavailable {
		t.Fatalf("nil-manager POST = %d, want 503", code)
	}
	if code, _ := do(http.MethodDelete); code != http.StatusServiceUnavailable {
		t.Fatalf("nil-manager DELETE = %d, want 503", code)
	}
}

// The document helpers below mirror internal/license's test shapes; they
// live here so the HTTP legs read as wire-level acts.

const testKid = "test-key"

type testKeys struct {
	priv ed25519.PrivateKey
	keys map[string]ed25519.PublicKey
}

func newTestKeys(t *testing.T) testKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return testKeys{priv: priv, keys: map[string]ed25519.PublicKey{testKid: pub}}
}

func (k testKeys) spec(now time.Time) docSpec {
	exp := now.Add(365 * 24 * time.Hour).Format(time.RFC3339)
	return docSpec{
		typ: "binflow-license", alg: "EdDSA", kid: testKid, ver: 1,
		licenseID: "0f1e2d3c-test", licensee: "Acme Corp", tier: "pro",
		issuedAt:  now.Add(-24 * time.Hour).Format(time.RFC3339),
		notBefore: now.Add(-1 * time.Hour).Format(time.RFC3339),
		expiresAt: &exp, signWith: k.priv,
	}
}

type docSpec struct {
	typ, alg, kid             string
	ver                       int
	licenseID, licensee, tier string
	issuedAt, notBefore       string
	expiresAt                 *string
	signWith                  ed25519.PrivateKey
}

func (s docSpec) sign(t *testing.T) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"typ": s.typ, "alg": s.alg, "kid": s.kid, "ver": s.ver,
		"licenseId": s.licenseID, "licensee": s.licensee, "tier": s.tier,
		"issuedAt": s.issuedAt, "notBefore": s.notBefore, "expiresAt": s.expiresAt,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	sig := ed25519.Sign(s.signWith, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

func tamperPayloadContent(t *testing.T, doc string) string {
	t.Helper()
	head, tail, _ := strings.Cut(doc, ".")
	payload, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	i := strings.Index(string(payload), "Acme")
	if i < 0 {
		t.Fatalf("payload lacks the licensee anchor: %s", payload)
	}
	payload[i] = 'X'
	return base64.RawURLEncoding.EncodeToString(payload) + "." + tail
}
