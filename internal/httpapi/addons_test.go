package httpapi_test

// T-282 AC legs over a real stack (sqlite metadata, real auth.Service with
// admin/readonly_admin/plain user, real license.Manager on test keys, real
// repo.Service): the /api/v1/addons three-role gate, the community-floor
// matrix (five core + properties unlocked, the gated slots locked with tier
// reasons), the live tier flip on the SAME instance (install pro ->
// enterprise -> uninstall), the addons.disabled breaker's display, the
// nil-registry honest empty array, and the repo-create plane's dynamic
// legal set (unknown type 400 with the registry's own list; the known-but-
// adapterless pilot types still refused by repo.Service's enum — the
// T-285 hand-off pinned as today's contract).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// addonsStack is the T-282 assembly (this file owns its wiring; the license
// file's stack predates the addons collaborator).
type addonsStack struct {
	ts   *httptest.Server
	md   metadata.Store
	keys testKeys
}

// newAddonsStack assembles the full management plane. reg is the manifest
// to mount (nil = the pre-M10 shape); disabledCSV feeds the Manager.
func newAddonsStack(t *testing.T, reg *addons.Registry, disabledCSV string) *addonsStack {
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

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	seedLicenseUser(t, md, "roat", "roat-pw", "readonly_admin")
	seedLicenseUser(t, md, "plain", "plain-pw", "user")

	svc := repo.New(st, md, authSvc, audit.New(md, true))

	k := newTestKeys(t)
	sink := &logSink{}
	logger := newSlogTo(sink)
	mgr, err := license.New(license.Options{
		Store:       md.Licenses(),
		VerifyKeys:  k.keys,
		DisabledCSV: disabledCSV,
		Audit:       audit.BestEffort(audit.New(md, true)),
		Log:         logger,
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
		ReposSvc: svc,
		License:  mgr,
		Addons:   reg,
	}, logger)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &addonsStack{ts: ts, md: md, keys: k}
}

// productionManifest mirrors cmd/binflow-server's addonManifest() (kept in
// sync by TestT282CmdManifestParity on the cmd side).
func productionManifest() *addons.Registry {
	return addons.New(
		addons.Generic(), addons.Docker(), addons.Maven(), addons.Npm(), addons.Pypi(),
		addons.Go(), addons.NuGet(), addons.Cargo(),
		addons.Properties(), addons.HA(), addons.XrayIntegration(),
	)
}

func (st *addonsStack) do(t *testing.T, method, path, user, pass, body string) (int, string) {
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

// addonRows decodes the bare array into id -> row.
func addonRows(t *testing.T, body string) ([]map[string]any, map[string]map[string]any) {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("addons body is not a bare array: %v (%s)", err, body)
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Fatalf("addons body is not a bare array: %s", body)
	}
	byID := map[string]map[string]any{}
	for _, r := range rows {
		byID[r["id"].(string)] = r
	}
	return rows, byID
}

// TestAddonsEndpointRoles: anonymous 401 / plain user 403 / readonly_admin
// 200 / admin 200 (FR-86-AC1's gate; the route rides CapSystemRead).
func TestAddonsEndpointRoles(t *testing.T) {
	st := newAddonsStack(t, productionManifest(), "")
	const p = "/binflow/api/v1/addons"

	if code, _ := st.do(t, http.MethodGet, p, "", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", code)
	}
	if code, _ := st.do(t, http.MethodGet, p, "plain", "plain-pw", ""); code != http.StatusForbidden {
		t.Fatalf("plain user GET = %d, want 403", code)
	}
	code, body := st.do(t, http.MethodGet, p, "roat", "roat-pw", "")
	if code != http.StatusOK {
		t.Fatalf("readonly_admin GET = %d %s, want 200", code, body)
	}
	if code, body := st.do(t, http.MethodGet, p, adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("admin GET = %d %s, want 200", code, body)
	} else if rows, _ := addonRows(t, body); len(rows) != 11 {
		t.Fatalf("admin GET carries %d slots, want 11: %s", len(rows), body)
	}

	// The write verbs have no route: the addon plane is read-only (slot
	// tiers are code, not data).
	if code, _ := st.do(t, http.MethodPost, p, adminUser, adminPass, "{}"); code != http.StatusNotFound {
		t.Fatalf("POST addons = %d, want the E-26 404", code)
	}
}

// TestAddonsEndpointCommunityFloor: the unlicensed instance's matrix — five
// core package types and properties unlocked with no reason, the gated
// slots locked naming the tier they need (FR-86-AC1 row by row).
func TestAddonsEndpointCommunityFloor(t *testing.T) {
	st := newAddonsStack(t, productionManifest(), "")
	_, body := st.do(t, http.MethodGet, "/binflow/api/v1/addons", adminUser, adminPass, "")
	rows, byID := addonRows(t, body)

	wantOrder := []string{"generic", "docker", "maven", "npm", "pypi", "go", "nuget", "cargo", "properties", "ha", "xray-integration"}
	if len(rows) != len(wantOrder) {
		t.Fatalf("slot count = %d, want %d", len(rows), len(wantOrder))
	}
	for i, id := range wantOrder {
		if rows[i]["id"] != id {
			t.Fatalf("row %d = %v, want %q (assembly order must be stable)", i, rows[i]["id"], id)
		}
	}
	unlocked := map[string]bool{"generic": true, "docker": true, "maven": true, "npm": true, "pypi": true, "properties": true}
	for id, row := range byID {
		wantEnabled := unlocked[id]
		if row["enabled"] != wantEnabled {
			t.Fatalf("slot %s enabled = %v, want %v (%s)", id, row["enabled"], wantEnabled, body)
		}
		switch {
		case wantEnabled:
			if _, has := row["reason"]; has {
				t.Fatalf("unlocked slot %s carries a reason: %v", id, row)
			}
			if row["minTier"] != "community" {
				t.Fatalf("floor slot %s minTier = %v, want community", id, row["minTier"])
			}
		case id == "ha" || id == "xray-integration":
			if row["minTier"] != "enterprise" || row["reason"] != "license tier community < enterprise" {
				t.Fatalf("enterprise slot %s wrong: %v", id, row)
			}
			// The reservation note must ride the echo (PRD 86.2/AC3).
			if !strings.Contains(row["description"].(string), "M11+") {
				t.Fatalf("placeholder slot %s lacks the M11+ note: %v", id, row)
			}
		default:
			if row["minTier"] != "pro" || row["reason"] != "license tier community < pro" {
				t.Fatalf("gated slot %s wrong: %v", id, row)
			}
		}
		if row["kind"] != "package-type" && row["kind"] != "feature" {
			t.Fatalf("slot %s kind = %v", id, row["kind"])
		}
		if row["displayName"] == "" || row["description"] == "" {
			t.Fatalf("slot %s lacks console metadata: %v", id, row)
		}
	}
	if byID["generic"]["kind"] != "package-type" || byID["properties"]["kind"] != "feature" {
		t.Fatalf("kind split wrong: %s", body)
	}
}

// TestAddonsEndpointTierFlipLive (AC-3, same-instance leg): install pro ->
// enterprise -> uninstall through the REAL license REST plane, the addons
// matrix following with no restart (85.2's immediate-effect display half).
func TestAddonsEndpointTierFlipLive(t *testing.T) {
	st := newAddonsStack(t, productionManifest(), "")
	const addonsPath = "/binflow/api/v1/addons"
	const licensePath = "/binflow/api/system/license"

	enabledOf := func() map[string]bool {
		_, body := st.do(t, http.MethodGet, addonsPath, adminUser, adminPass, "")
		_, byID := addonRows(t, body)
		out := map[string]bool{}
		for id, row := range byID {
			out[id] = row["enabled"].(bool)
		}
		return out
	}

	// Floor.
	e := enabledOf()
	if e["go"] || e["ha"] || !e["generic"] {
		t.Fatalf("floor matrix wrong: %v", e)
	}

	// Pro unlocks the pilots; the enterprise placeholders stay locked.
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	if code, body := st.do(t, http.MethodPost, licensePath, adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
	e = enabledOf()
	for _, id := range []string{"generic", "docker", "maven", "npm", "pypi", "properties", "go", "nuget", "cargo"} {
		if !e[id] {
			t.Fatalf("pro matrix left %s locked: %v", id, e)
		}
	}
	if e["ha"] || e["xray-integration"] {
		t.Fatalf("pro matrix unlocked an enterprise slot: %v", e)
	}

	// Enterprise unlocks everything.
	spec = st.keys.spec(time.Now().UTC())
	spec.tier = "enterprise"
	if code, body := st.do(t, http.MethodPost, licensePath, adminUser, adminPass, spec.sign(t)); code != http.StatusCreated {
		t.Fatalf("install enterprise = %d %s", code, body)
	}
	e = enabledOf()
	for id, on := range e {
		if !on {
			t.Fatalf("enterprise matrix left %s locked", id)
		}
	}

	// Uninstall returns the floor (D6's display half, no restart).
	if code, body := st.do(t, http.MethodDelete, licensePath, adminUser, adminPass, ""); code != http.StatusOK {
		t.Fatalf("uninstall = %d %s", code, body)
	}
	e = enabledOf()
	if e["go"] || !e["generic"] {
		t.Fatalf("post-uninstall matrix wrong: %v", e)
	}
}

// TestAddonsEndpointDisabledBreaker: addons.disabled reaches the view — npm
// (a five-core slot!) reports disabled by configuration while the rest keep
// their floor verdicts (85.3's display half; the config key itself is T-283's
// wiring, the Manager consumes it already).
func TestAddonsEndpointDisabledBreaker(t *testing.T) {
	st := newAddonsStack(t, productionManifest(), "npm,go")
	_, body := st.do(t, http.MethodGet, "/binflow/api/v1/addons", adminUser, adminPass, "")
	_, byID := addonRows(t, body)

	for _, id := range []string{"npm", "go"} {
		if byID[id]["enabled"] != false || byID[id]["reason"] != "disabled by configuration" {
			t.Fatalf("disabled slot %s wrong: %v", id, byID[id])
		}
	}
	for _, id := range []string{"generic", "maven", "properties"} {
		if byID[id]["enabled"] != true {
			t.Fatalf("untouched slot %s disturbed by the breaker: %v", id, byID[id])
		}
	}
}

// TestAddonsEndpointNilRegistry: a pre-M10 unit stack (no registry mounted)
// answers the honest empty array, not an error (the Docs-handler
// precedent).
func TestAddonsEndpointNilRegistry(t *testing.T) {
	st := newAddonsStack(t, nil, "")
	code, body := st.do(t, http.MethodGet, "/binflow/api/v1/addons", adminUser, adminPass, "")
	if code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("nil-registry GET = %d %q, want 200 []", code, body)
	}
}

// TestAddonsRepoCreateDynamicSet (FR-86.5's enum half): with the registry
// mounted, the repo-create legality question reads the registry — an
// unknown type 400s with the registry's own dynamic list, a five-core type
// creates exactly as before (retro-fit zero change), and the known-but-
// adapterless pilot types still fall to repo.Service's static enum (the
// documented T-285 hand-off: the slot exists, the adapter does not).
func TestAddonsRepoCreateDynamicSet(t *testing.T) {
	st := newAddonsStack(t, productionManifest(), "")
	put := func(key, packageType string) (int, string) {
		return st.do(t, http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
			`{"rclass":"local","packageType":"`+packageType+`"}`)
	}

	// Five-core floor: creation unchanged.
	if code, body := put("t282-generic", "generic"); code != http.StatusOK {
		t.Fatalf("generic create = %d %s, want 200 (retro-fit zero change)", code, body)
	}

	// Unknown type: the handler's dynamic 400 — the message names the
	// registry's slots, including the registered-but-gated ones.
	code, body := put("t282-bogus", "wheelbarrow")
	if code != http.StatusBadRequest {
		t.Fatalf("unknown type = %d %s, want 400", code, body)
	}
	for _, want := range []string{"wheelbarrow", "generic", "go", "nuget", "cargo", "not a registered addon slot"} {
		if !strings.Contains(body, want) {
			t.Fatalf("unknown-type message lacks %q: %s", want, body)
		}
	}

	// Known slot, no adapter yet: the service's static enum still refuses
	// (the pilot slots become creatable when their tickets land).
	if code, body := put("t282-go", "go"); code != http.StatusBadRequest || !strings.Contains(body, "must be one of") {
		t.Fatalf("adapterless known slot = %d %s, want the service enum's 400", code, body)
	}

	// The registry IS the source: a manifest without docker refuses docker
	// at the handler with the same dynamic 400 (no hardcoded five anywhere).
	stNoDocker := newAddonsStack(t, addons.New(addons.Generic(), addons.Go()), "")
	code, body = stNoDocker.do(t, http.MethodPut, "/binflow/api/repositories/t282-docker", adminUser, adminPass,
		`{"rclass":"local","packageType":"docker"}`)
	if code != http.StatusBadRequest || !strings.Contains(body, "must be one of generic, go") {
		t.Fatalf("registry-sourced refusal wrong = %d %s", code, body)
	}

	// RBAC order unchanged: the route gate answers a plain user before any
	// package-type question (license gates never precede RBAC).
	if code, _ := st.do(t, http.MethodPut, "/binflow/api/repositories/t282-plain", "plain", "plain-pw",
		`{"rclass":"local","packageType":"generic"}`); code != http.StatusForbidden {
		t.Fatalf("plain user create = %d, want 403 (RBAC first)", code)
	}
}

// TestAddonsAdapterCoverageAssemblyGuard (§15.2.4's panic case): with a
// registry mounted, an adapter protocol without a package-type slot is an
// assembly bug that must surface at New — never a silently ungated,
// invisible package type. The covered shape stays silent.
func TestAddonsAdapterCoverageAssemblyGuard(t *testing.T) {
	newServer := func(reg *addons.Registry, proto string) (panicked string) {
		defer func() {
			if r := recover(); r != nil {
				panicked, _ = r.(string)
			}
		}()
		s := httpapi.New(httpapi.Deps{
			Config:   config.Defaults(),
			Adapters: []adapter.Handler{&t63Handler{proto: proto}},
			Addons:   reg,
		}, nil)
		_ = s
		return ""
	}

	if got := newServer(productionManifest(), "generic"); got != "" {
		t.Fatalf("covered assembly panicked: %s", got)
	}
	got := newServer(productionManifest(), "widget")
	if !strings.Contains(got, "widget") || !strings.Contains(got, "no addon slot") {
		t.Fatalf("uncovered adapter panic = %q, want it to name the protocol and the missing slot", got)
	}

	// No registry mounted (pre-M10 unit stacks): the guard is inert.
	if got := newServer(nil, "widget"); got != "" {
		t.Fatalf("registry-less assembly panicked: %s", got)
	}
}
