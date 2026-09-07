// T-513's REST legs, driven straight into dispatchAPI (the t362
// precedent — the route cases and handlers are this ticket's own
// assembly): the create/query ladder's status codes and bodies per
// release-bundle.md §1 + ADR-0046 Errata ①②, the conflict 409's VERBATIM
// flat body, the release-bundle slot's three seams (community 403 + the
// license header, pro unlocked, the addons.disabled breaker's no-header
// refusal), the dual read gate's matrix (capability arm, Any Distribution
// channel arm, zero-leak), and the family's BinFlow-native parameter
// refusals.

package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
)

// t513Stack is the family's own assembly: real auth.Service (the route
// gates' CanManage AND the channel's Can), real migrated store with a
// seeded repository and artifact, a controllable license facet.
type t513Stack struct {
	s       *Server
	md      metadata.Store
	authSvc *auth.Service
}

func newT513Stack(t *testing.T, eval fakeLicenseEval) *t513Stack {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
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
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
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

// adminP/roP are the capability-arm principals (the role derivation the
// route gates walk).
func adminP() *auth.Principal { return &auth.Principal{Name: "root", Admin: true} }

// createBody renders the explicit-manifest create document.
func createBody(name, version string, artifacts string) string {
	return `{"name":"` + name + `","version":"` + version + `","artifacts":[` + artifacts + `]}`
}

const jarRow = `{"repo":"libs","path":"app/1.0/app.jar"}`

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

// TestBundleWireCreateQueryLadder: the pro-tier full chain — 202 create,
// descriptor GET with the snapshot fields, names/versions/status faces,
// the HEAD checksum, then the tri-state's 200 resume and both 409 arms
// with the VERBATIM body.
func TestBundleWireCreateQueryLadder(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})

	// 202 arm: the artifact resolves -> COMPLETE.
	code, body, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-2026", "1.0", jarRow), adminP())
	if code != http.StatusAccepted {
		t.Fatalf("create = %d %s, want 202", code, body)
	}
	if !strings.Contains(body, `"/api/release/bundles/rel-2026/1.0"`) {
		t.Fatalf("create body = %s, want the bundle_path echo", body)
	}

	// The descriptor carries the snapshot and the SOURCE constant.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0", "", adminP())
	if code != http.StatusOK {
		t.Fatalf("descriptor = %d %s, want 200", code, body)
	}
	for _, want := range []string{`"status": "COMPLETE"`, `"type": "SOURCE"`, `"sha256:`, `"app/1.0/app.jar"`, `"size": 10`} {
		if !strings.Contains(body, want) {
			t.Errorf("descriptor missing %s: %s", want, body)
		}
	}

	// The query ladder.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles", "", adminP())
	if code != http.StatusOK || !strings.Contains(body, `"rel-2026"`) {
		t.Fatalf("names = %d %s", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026", "", adminP())
	if code != http.StatusOK || !strings.Contains(body, `"1.0"`) {
		t.Fatalf("versions = %d %s", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/1.0/status", "", adminP())
	if code != http.StatusOK || strings.TrimSpace(body) != `"COMPLETE"` {
		t.Fatalf("status = %d %s, want the JSON string \"COMPLETE\"", code, body)
	}

	// HEAD: bodyless, X-Checksum-Sha256 present.
	req := httptest.NewRequest(http.MethodHead, "/binflow/api/release/bundles/rel-2026/1.0", nil)
	req = req.WithContext(withPrincipal(req.Context(), adminP()))
	rec := httptest.NewRecorder()
	st.s.dispatchAPI(rec, req, "release/bundles/rel-2026/1.0")
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Checksum-Sha256") == "" {
		t.Fatal("HEAD must carry X-Checksum-Sha256")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body = %q, want bodyless", rec.Body.String())
	}

	// 409 arm one: COMPLETE bundle, same manifest.
	code, body, _ = st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-2026", "1.0", jarRow), adminP())
	if code != http.StatusConflict {
		t.Fatalf("create on COMPLETE = %d, want 409", code)
	}
	if strings.TrimSpace(body) != `{"status":409,"message":"Bundle already exists"}` {
		t.Fatalf("409 body = %q, want the VERBATIM flat form (E6)", body)
	}
	// 409 arm two: different manifest.
	code, body, _ = st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-2026", "1.0", `{"repo":"libs","path":"app/1.0/other.jar"}`), adminP())
	if code != http.StatusConflict || !strings.Contains(body, "Bundle already exists") {
		t.Fatalf("different-manifest create = %d %s, want the 409", code, body)
	}

	// 200 arm: a pending artifact (not on this instance), then the same
	// manifest resumes after it lands.
	code, _, _ = st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-2027", "2.0", jarRow+","+"{\"repo\":\"libs\",\"path\":\"app/1.0/app.pom\"}"), adminP())
	if code != http.StatusAccepted {
		t.Fatalf("pending create = %d, want 202", code)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2027/2.0/status", "", adminP())
	if code != http.StatusOK || strings.TrimSpace(body) != `"INPROGRESS"` {
		t.Fatalf("pending status = %d %s, want INPROGRESS", code, body)
	}
	// The same manifest again: still INPROGRESS -> 200 resume.
	code, body, _ = st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-2027", "2.0", jarRow+","+"{\"repo\":\"libs\",\"path\":\"app/1.0/app.pom\"}"), adminP())
	if code != http.StatusOK {
		t.Fatalf("resume = %d %s, want 200", code, body)
	}
	// 404 face: a missing pair.
	code, body, _ = st.do(t, http.MethodGet, "release/bundles/rel-2026/9.9", "", adminP())
	if code != http.StatusNotFound || !strings.Contains(body, "Release Bundle not found") {
		t.Fatalf("missing descriptor = %d %s, want the 404", code, body)
	}
}

// TestBundleWireSlotSeams: the release-bundle slot's three entitlement
// shapes — community 403 + the license header on the WRITE face with
// reads untouched (D1), pro unlocked, and the addons.disabled breaker's
// no-header refusal.
func TestBundleWireSlotSeams(t *testing.T) {
	// Community: the write face refuses with the programmable header.
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierCommunity, licensed: true})
	code, body, hdr := st.do(t, http.MethodPost, "release/bundle",
		createBody("rel", "1", jarRow), adminP())
	if code != http.StatusForbidden {
		t.Fatalf("community create = %d %s, want 403", code, body)
	}
	if got := hdr.Get("X-Binflow-License-Required"); got != BundleAddonID {
		t.Fatalf("license header = %q, want %q", got, BundleAddonID)
	}

	// Pro: unlocked.
	st = newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	if code, body, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("rel", "1", jarRow), adminP()); code != http.StatusAccepted {
		t.Fatalf("pro create = %d %s, want 202", code, body)
	}

	// The breaker: a slot the floor would unlock but the Manager refuses
	// — the no-header 403 naming the knob.
	st = newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true, disabled: true})
	code, body, hdr = st.do(t, http.MethodPost, "release/bundle",
		createBody("rel", "2", jarRow), adminP())
	if code != http.StatusForbidden {
		t.Fatalf("disabled create = %d %s, want 403", code, body)
	}
	if hdr.Get("X-Binflow-License-Required") != "" {
		t.Fatal("the breaker refusal must NOT carry the license header (installing a license is not the remedy)")
	}
	if !strings.Contains(body, "disabled by configuration") {
		t.Fatalf("breaker wording = %s, want the knob named", body)
	}
}

// TestBundleWireGateMatrix: the route doors and the dual read gate —
// anonymous 401, plain user create 403 (CapSystemWrite is admin-only for
// plain principals), readonly_admin read 200 / create 403, the Any
// Distribution channel holder reading by name pattern, and the zero-leak
// list/versions faces for the ungranted.
func TestBundleWireGateMatrix(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	// Two bundles seeded by the admin: one inside the channel's include,
	// one outside.
	for _, pair := range [][2]string{{"rel-2026", "1"}, {"prod-annual", "1"}} {
		if code, body, _ := st.do(t, http.MethodPost, "release/bundle",
			createBody(pair[0], pair[1], jarRow), adminP()); code != http.StatusAccepted {
			t.Fatalf("seed %s = %d %s", pair[0], code, body)
		}
	}

	// Anonymous: the 401 challenge (the route door).
	code, _, _ := st.do(t, http.MethodGet, "release/bundles", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", code)
	}

	// Plain user without any grant: create 403 (capability door), reads
	// 403-empty — the list answers 200 with the EMPTY visible set (never
	// a 403 oracle), the single get answers 403.
	dev := &auth.Principal{Name: "dev"}
	var body string
	if code, body, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("x", "1", jarRow), dev); code != http.StatusForbidden {
		t.Fatalf("plain create = %d %s, want 403", code, body)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles", "", dev)
	if code != http.StatusOK || strings.Contains(body, "rel-2026") || strings.Contains(body, "prod-annual") {
		t.Fatalf("plain list = %d %s, want 200 with the empty visible set", code, body)
	}
	if code, _, _ := st.do(t, http.MethodGet, "release/bundles/rel-2026/1", "", dev); code != http.StatusForbidden {
		t.Fatalf("plain get = %d, want 403", code)
	}

	// readonly_admin: reads everything (the capability arm), writes
	// nothing (the role short-circuit).
	ro := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles", "", ro)
	if code != http.StatusOK || !strings.Contains(body, "prod-annual") {
		t.Fatalf("readonly list = %d %s, want both names", code, body)
	}
	if code, _, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("x", "2", jarRow), ro); code != http.StatusForbidden {
		t.Fatalf("readonly create = %d, want 403", code)
	}

	// The Any Distribution channel holder: reads rel-* by name, is
	// refused on prod-annual, and sees only the included name in the
	// list. The channel never widens the write face.
	st.seedChannelUser(t, "rel-bot")
	bot := &auth.Principal{Name: "rel-bot"}
	if code, _, _ := st.do(t, http.MethodGet, "release/bundles/rel-2026/1", "", bot); code != http.StatusOK {
		t.Fatalf("channel get rel-2026 = %d, want 200", code)
	}
	if code, _, _ := st.do(t, http.MethodGet, "release/bundles/prod-annual/1", "", bot); code != http.StatusForbidden {
		t.Fatalf("channel get prod-annual = %d, want 403 (includes apply by name)", code)
	}
	code, body, _ = st.do(t, http.MethodGet, "release/bundles", "", bot)
	if code != http.StatusOK || !strings.Contains(body, "rel-2026") || strings.Contains(body, "prod-annual") {
		t.Fatalf("channel list = %d %s, want rel-2026 only", code, body)
	}
	if code, _, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("rel-new", "1", jarRow), bot); code != http.StatusForbidden {
		t.Fatalf("channel create = %d, want 403 (write stays capability-gated)", code)
	}
}

// TestBundleWireRefusals: the family's honest refusals — the create
// body's face-out official channels (aql/signature/uuid), ?type= and
// ?format=jws on the reads, and the E-26 404 of the unrouted faces
// (DELETE, the Distribution store/transaction families).
func TestBundleWireRefusals(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	admin := adminP()

	tests := []struct {
		name   string
		method string
		rest   string
		body   string
		want   int
		word   string
	}{
		{"aql channel", http.MethodPost, "release/bundle",
			`{"name":"r","version":"1","artifacts":[],"aql":"items.find({\"name\":\"*.jar\"})"}`,
			http.StatusBadRequest, "AQL assembly is not supported"},
		{"signature channel", http.MethodPost, "release/bundle",
			`{"name":"r","version":"1","artifacts":[],"signature":"jws"}`,
			http.StatusBadRequest, "signed release bundles are not supported"},
		{"uuid channel", http.MethodPost, "release/bundle",
			`{"name":"r","version":"1","artifacts":[],"uuid":"abc"}`,
			http.StatusBadRequest, "uuid field belongs to the Distribution-side"},
		{"empty manifest", http.MethodPost, "release/bundle",
			`{"name":"r","version":"1","artifacts":[]}`,
			http.StatusBadRequest, "manifest is empty"},
		{"type parameter", http.MethodGet, "release/bundles?type=source", "",
			http.StatusBadRequest, "type parameter is not supported"},
		{"format parameter", http.MethodGet, "release/bundles/a/1?format=jws", "",
			http.StatusBadRequest, "format parameter is not supported"},
		{"delete unrouted", http.MethodDelete, "release/bundles/a/1", "",
			http.StatusNotFound, "not implemented"},
		{"delete source unrouted", http.MethodDelete, "release/bundles/source/a/1", "",
			http.StatusNotFound, "not implemented"},
		{"store family unrouted", http.MethodPut, "release/store", "{}",
			http.StatusNotFound, "not implemented"},
		{"transaction family unrouted", http.MethodPost, "release/bundle/transaction", "jws",
			http.StatusNotFound, "not implemented"},
		{"config family unrouted", http.MethodGet, "release/bundles/config", "",
			http.StatusNotFound, "not implemented"},
		{"bad path tail", http.MethodGet, "release/bundles/a/1/artifacts", "",
			http.StatusNotFound, "not implemented"},
		{"project key", http.MethodPost, "release/bundle?projectKey=p",
			createBody("r", "1", jarRow), http.StatusBadRequest, "projects are not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, body, _ := st.do(t, tt.method, tt.rest, tt.body, admin)
			if code != tt.want {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.rest, code, body, tt.want)
			}
			if !strings.Contains(body, tt.word) {
				t.Errorf("body %q must contain %q", body, tt.word)
			}
		})
	}
}

// TestBundleWireMetricsExposed: the create counter family pre-seeds its
// outcome labels on the exposition (the restart-kindness rule).
func TestBundleWireMetricsExposed(t *testing.T) {
	st := newT513Stack(t, fakeLicenseEval{tier: license.TierPro, licensed: true})
	scrape := func() string {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		st.s.metricsHandler().ServeHTTP(rec, req)
		raw, _ := io.ReadAll(rec.Body)
		return string(raw)
	}
	before := scrape()
	for _, want := range []string{"binflow_bundle_creates_total", "binflow_bundle_gets_total", `outcome="resumed"`} {
		if !strings.Contains(before, want) {
			t.Errorf("exposition missing %s", want)
		}
	}
	// And one create moves the counter.
	if code, body, _ := st.do(t, http.MethodPost, "release/bundle",
		createBody("rel", "1", jarRow), adminP()); code != http.StatusAccepted {
		t.Fatalf("create = %d %s", code, body)
	}
	if after := scrape(); after == before {
		t.Error("the create counter did not move")
	}
}
