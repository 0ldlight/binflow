// The release-bundle query family's REST legs (L026-6, D08-R04; wire
// p01-p12/p59, release-bundle.md §10.5), driven straight into dispatchAPI
// (the t513 precedent): the empty-state envelopes, the three 404 message
// families, the type projection (SOURCE records vs the empty-forever
// TARGET default), the dual read gate's matrix, and the slot's three
// entitlement seams. Records enter through the domain service — POST
// /api/release/bundle is the assembly probe now, not a create.

package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t513Stack is the family's own assembly: real auth.Service (the route
// gates' CanManage AND the channel's Can), the real repo service over a
// storage engine (the assembly face's AQL engine needs it), a real
// migrated store with a seeded repository and artifact, a controllable
// license facet.
type t513Stack struct {
	s       *Server
	md      metadata.Store
	authSvc *auth.Service
}

func newT513Stack(t *testing.T, eval fakeLicenseEval) *t513Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	now := metadata.Now()
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := md.Blobs().Put(ctx, &metadata.Blob{
		Sha256: "aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44", Size: 10, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := md.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "libs", Path: "app/1.0/app.jar", Sha256: "aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44aa11bb22cc33dd44",
		Size: 10, Mime: "application/java-archive", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	reposSvc := repo.New(st, md, authSvc, audit.New(md, true))
	reg := addons.New(
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Properties(), addons.RepoOperations(), addons.Trashcan(),
		addons.HA(), addons.XrayIntegration(), addons.Webhook(),
		addons.ReleaseBundle(),
	)
	s := New(Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: reposSvc,
		Addons:   reg,
		License:  eval,
		Metrics:  metrics.NewRegistry(),
	}, nil)
	return &t513Stack{s: s, md: md, authSvc: authSvc}
}

// do drives one request into the API plane with a boxed principal (the
// authenticate middleware's output shape; production reaches the same
// handlers through the route switch).
func (st *t513Stack) do(t *testing.T, method, rest, body string, p *auth.Principal) (int, string, http.Header) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/binflow/api/"+rest, rdr)
	if p != nil {
		req = req.WithContext(withPrincipal(req.Context(), p))
	}
	rec := httptest.NewRecorder()
	pathREST, _, _ := strings.Cut(rest, "?")
	st.s.dispatchAPI(rec, req, pathREST)
	raw, _ := io.ReadAll(rec.Body)
	return rec.Code, string(raw), rec.Header()
}

// adminP is the capability-arm principal (the role derivation the route
// gates walk).
func adminP() *auth.Principal { return &auth.Principal{Name: "root", Admin: true} }

// seedBundle plants one bundle record through the domain service (the
// REST create entrance is gone — R01 made POST the assembly probe).
func (st *t513Stack) seedBundle(t *testing.T, name, version, path string) {
	t.Helper()
	_, err := st.s.bundles.Create(context.Background(), adminP(), &bundle.CreateRequest{
		Name: name, Version: version,
		Items: []*bundle.ManifestItem{{Repo: "libs", Path: path}},
	})
	if err != nil {
		t.Fatalf("seed bundle %s/%s: %v", name, version, err)
	}
}

// seedChannelUser installs one plain user plus the ANY DISTRIBUTION
// channel target granting read on rel-* names (the T-491 wire face).
func (st *t513Stack) seedChannelUser(t *testing.T, user string) {
	t.Helper()
	hash, err := auth.HashPassword("pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ctx := context.Background()
	if err := st.md.Users().Create(ctx, &metadata.User{
		Username: user, PasswordHash: hash, IsAdmin: false, Enabled: true,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	now := metadata.Now()
	target := &metadata.PermissionTarget{
		Name:      "rel-channel",
		Repos:     `["ANY DISTRIBUTION"]`,
		Includes:  `["rel-*"]`,
		Excludes:  `[]`,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.md.Permissions().PutTarget(ctx, target, []*metadata.PermissionPrincipal{{
		TargetName: "rel-channel", Principal: user, PrincipalType: "user", CanRead: true,
	}}); err != nil {
		t.Fatalf("put channel target: %v", err)
	}
}

// TestBundleWireQueryEmptyState: the §10.5 empty-state ladder on a fresh
// pro instance — the names map {}, the always-200 versions face, the three
// 404 message families (Bundle not found / <name>:<version> not found /
// Release bundle not found), the bodyless HEAD 404.
func TestBundleWireQueryEmptyState(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	tests := []struct {
		name   string
		method string
		rest   string
		want   int
		body   string // "" = assert bodyless
	}{
		{"names empty map", http.MethodGet, "release/bundles", 200, `"bundles": {}`},
		{"names type=source", http.MethodGet, "release/bundles?type=source", 200, `"bundles": {}`},
		{"versions always 200", http.MethodGet, "release/bundles/no-such-bundle-l026", 200, `"versions": []`},
		{"versions always 200 typed", http.MethodGet, "release/bundles/no-such-bundle-l026?type=source", 200, `"versions": []`},
		{"descriptor 404", http.MethodGet, "release/bundles/no-such-bundle-l026/1.0", 404, "Bundle not found"},
		{"descriptor 404 format=jws", http.MethodGet, "release/bundles/no-such-bundle-l026/1.0?format=jws", 404, "Bundle not found"},
		{"descriptor 404 type=source", http.MethodGet, "release/bundles/no-such-bundle-l026/1.0?type=source", 404, "Bundle not found"},
		{"status 404 colon form", http.MethodGet, "release/bundles/no-such-bundle-l026/1.0/status", 404, "no-such-bundle-l026:1.0 not found"},
		{"artifacts 404", http.MethodGet, "release/bundles/no-such-bundle-l026/1.0/artifacts", 404, "Bundle not found"},
		{"delete target 404", http.MethodDelete, "release/bundles/no-such-bundle-l026/1.0", 404, "Bundle not found"},
		{"delete source 404", http.MethodDelete, "release/bundles/source/no-such-bundle-l026/1.0", 404, "Release bundle not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body, _ := st.do(t, tt.method, tt.rest, "", admin)
			if code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.rest, code, body, tt.want)
			}
			if tt.body == "" {
				if strings.TrimSpace(body) != "" {
					t.Fatalf("body = %q, want bodyless", body)
				}
				return
			}
			if !strings.Contains(body, tt.body) {
				t.Errorf("body %q must contain %q", body, tt.body)
			}
			if strings.Contains(body, "null") {
				t.Errorf("body %q must not render null", body)
			}
		})
	}

	// p07: the HEAD 404 is bodyless — no envelope bytes at all — but the
	// reference mirrors the GET face's media type on it (L027-1 c07: A
	// sends Content-Type: application/json, no charset).
	req := httptest.NewRequest(http.MethodHead, "/binflow/api/release/bundles/no-such-bundle-l026/1.0", nil)
	req = req.WithContext(withPrincipal(req.Context(), admin))
	rec := httptest.NewRecorder()
	st.s.dispatchAPI(rec, req, "release/bundles/no-such-bundle-l026/1.0")
	if rec.Code != http.StatusNotFound || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %q, want bodyless 404", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("HEAD 404 Content-Type = %q, want application/json", ct)
	}
}

// TestBundleWireSeededRecordFaces: a record seeded through the domain
// service answers the SOURCE projection under ?type=source and stays
// invisible to the TARGET default — the source-only instance's honest
// reading of §2.2's projection semantics (the locked design; registered).
func TestBundleWireSeededRecordFaces(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()
	st.seedBundle(t, "rel-2026", "1.0", "app/1.0/app.jar")

	// The names map: SOURCE carries the record, the TARGET default is {}.
	code, body, _ := st.do(t, http.MethodGet, "release/bundles?type=source", "", admin)
	if code != 200 || !strings.Contains(body, `"rel-2026": [`) || !strings.Contains(body, `"1.0"`) {
		t.Fatalf("source names = %d %s, want the map with rel-2026 -> [\"1.0\"]", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles", "", admin)
	if code != 200 || !strings.Contains(body, `"bundles": {}`) || strings.Contains(body, "rel-2026") {
		t.Fatalf("default names = %d %s, want the empty TARGET map", code, body)
	}

	// Versions: same projection split.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026?type=source", "", admin)
	if code != 200 || !strings.Contains(body, `"versions": [`) || !strings.Contains(body, `"1.0"`) {
		t.Fatalf("source versions = %d %s, want [\"1.0\"]", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026", "", admin)
	if code != 200 || !strings.Contains(body, `"versions": []`) {
		t.Fatalf("default versions = %d %s, want the empty TARGET array", code, body)
	}

	// The descriptor: SOURCE answers, TARGET 404s.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0?type=source", "", admin)
	if code != 200 {
		t.Fatalf("source descriptor = %d %s, want 200", code, body)
	}
	for _, want := range []string{`"status": "COMPLETE"`, `"type": "SOURCE"`, "app/1.0/app.jar"} {
		if !strings.Contains(body, want) {
			t.Errorf("descriptor missing %s: %s", want, body)
		}
	}
	if code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0", "", admin); code != 404 || !strings.Contains(body, "Bundle not found") {
		t.Fatalf("default descriptor = %d %s, want the TARGET 404", code, body)
	}

	// Status and artifacts: same split; the colon-form 404 for TARGET.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0/status?type=source", "", admin)
	if code != 200 || strings.TrimSpace(body) != `"COMPLETE"` {
		t.Fatalf("source status = %d %s, want \"COMPLETE\"", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0/status", "", admin)
	if code != 404 || !strings.Contains(body, "rel-2026:1.0 not found") {
		t.Fatalf("default status = %d %s, want the colon-form 404", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0/artifacts?type=source", "", admin)
	if code != 200 || !strings.Contains(body, "app/1.0/app.jar") {
		t.Fatalf("source artifacts = %d %s, want the manifest row", code, body)
	}
	if code, _, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0/artifacts", "", admin); code != 404 {
		t.Fatalf("default artifacts = %d, want 404", code)
	}

	// HEAD ignores ?type= (恒 SOURCE): the checksum probe finds the record.
	req := httptest.NewRequest(http.MethodHead, "/binflow/api/release/bundles/rel-2026/1.0?type=target", nil)
	req = req.WithContext(withPrincipal(req.Context(), admin))
	rec := httptest.NewRecorder()
	st.s.dispatchAPI(rec, req, "release/bundles/rel-2026/1.0")
	if rec.Code != 200 || rec.Header().Get("X-Checksum-Sha256") == "" || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d (checksum %q, body %q), want bodyless 200 with the checksum", rec.Code,
			rec.Header().Get("X-Checksum-Sha256"), rec.Body.String())
	}

	// DELETE: the main face is 恒 TARGET — 404 even though the SOURCE
	// record exists; the /source/ face finds it and hits the honest 500
	// (the metadata store carries no delete seam — registered gap).
	if code, body, _ = st.do(t, http.MethodDelete, "release/bundles/rel-2026/1.0", "", admin); code != 404 || !strings.Contains(body, "Bundle not found") {
		t.Fatalf("target delete = %d %s, want the TARGET 404", code, body)
	}
	if code, body, _ = st.do(t, http.MethodDelete, "release/bundles/source/rel-2026/1.0", "", admin); code != 500 {
		t.Fatalf("source delete on an existing record = %d %s, want the registered-gap 500", code, body)
	}

	// format=jws on a FOUND record: the honest BinFlow refusal (the signed
	// form is face-out — the lookup precedes the format arm per p12's
	// ordering evidence).
	if code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0?format=jws&type=source", "", admin); code != 400 {
		t.Fatalf("format=jws on a found record = %d %s, want the honest 400", code, body)
	}
}

// TestBundleWireGateMatrix: the route doors and the dual read gate —
// anonymous 401, the plain user's zero-leak empty views, readonly_admin's
// capability arm, the Any Distribution channel holder reading by name
// pattern.
func TestBundleWireGateMatrix(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	st.seedBundle(t, "rel-2026", "1.0", "app/1.0/app.jar")
	st.seedBundle(t, "prod-annual", "1.0", "app/1.0/app.jar")

	// Anonymous: the 401 challenge (the route door).
	if code, _, _ := st.do(t, http.MethodGet, "release/bundles?type=source", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", code)
	}

	// Plain user without any grant: the list answers 200 with the EMPTY
	// visible set (never a 403 oracle); the single get answers 403.
	dev := &auth.Principal{Name: "dev"}
	code, body, _ := st.do(t, http.MethodGet, "release/bundles?type=source", "", dev)
	if code != 200 || strings.Contains(body, "rel-2026") || strings.Contains(body, "prod-annual") {
		t.Fatalf("plain list = %d %s, want 200 with the empty visible set", code, body)
	}
	if code, _, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0?type=source", "", dev); code != http.StatusForbidden {
		t.Fatalf("plain get = %d, want 403", code)
	}

	// readonly_admin: reads everything (the capability arm).
	ro := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles?type=source", "", ro)
	if code != 200 || !strings.Contains(body, "rel-2026") || !strings.Contains(body, "prod-annual") {
		t.Fatalf("readonly list = %d %s, want both names", code, body)
	}

	// The Any Distribution channel holder: reads rel-* by name, is refused
	// on prod-annual, sees only the included name in the list.
	st.seedChannelUser(t, "rel-bot")
	bot := &auth.Principal{Name: "rel-bot"}
	if code, _, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0?type=source", "", bot); code != 200 {
		t.Fatalf("channel get rel-2026 = %d, want 200", code)
	}
	if code, _, _ = st.do(t, http.MethodGet, "release/bundles/prod-annual/1.0?type=source", "", bot); code != 403 {
		t.Fatalf("channel get prod-annual = %d, want 403 (includes apply by name)", code)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles?type=source", "", bot)
	if code != 200 || !strings.Contains(body, "rel-2026") || strings.Contains(body, "prod-annual") {
		t.Fatalf("channel list = %d %s, want rel-2026 only", code, body)
	}
}

// TestBundleWireSlotSeams: the release-bundle slot's three entitlement
// shapes on the family's faces — community 403 + the license header on the
// write faces with reads untouched (D1), the addons.disabled breaker's
// no-header refusal.
func TestBundleWireSlotSeams(t *testing.T) {
	// Community: the write faces refuse with the programmable header;
	// reads stay open.
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	code, body, hdr := st.do(t, http.MethodPost, "release/bundle", `{"aql":"items.find({})"}`, adminP())
	if code != http.StatusForbidden {
		t.Fatalf("community assembly = %d %s, want 403", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != BundleAddonID {
		t.Fatalf("license header = %q, want %q", got, BundleAddonID)
	}
	if code, body, _ := st.do(t, http.MethodGet, "release/bundles", "", adminP()); code != 200 {
		t.Fatalf("community read = %d %s, want 200 (D1)", code, body)
	}

	// Pro: the assembly face reaches its validation arm.
	st = newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	if code, body, _ := st.do(t, http.MethodPost, "release/bundle", `{}`, adminP()); code != 400 || !strings.Contains(body, "Missing AQL query") {
		t.Fatalf("pro assembly = %d %s, want the validation 400", code, body)
	}

	// The breaker: a slot the floor would unlock but the Manager refuses —
	// the no-header 403 naming the knob.
	st = newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true, disabled: true})
	code, body, hdr = st.do(t, http.MethodPost, "release/bundle", `{}`, adminP())
	if code != http.StatusForbidden {
		t.Fatalf("disabled assembly = %d %s, want 403", code, body)
	}
	if hdr.Get("X-Binflow-License-Required") != "" {
		t.Fatal("the breaker refusal must NOT carry the license header (installing a license is not the remedy)")
	}
	if !strings.Contains(body, "disabled by configuration") {
		t.Fatalf("breaker wording = %s, want the knob named", body)
	}
}

// TestBundleWireMetricsExposed: the family's counter families pre-seed
// their labels on the exposition (the restart-kindness rule).
func TestBundleWireMetricsExposed(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	st.s.metricsHandler().ServeHTTP(rec, req)
	raw, _ := io.ReadAll(rec.Body)
	for _, want := range []string{"binflow_bundle_creates_total", "binflow_bundle_gets_total", `outcome="resumed"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("exposition missing %s", want)
		}
	}
}
