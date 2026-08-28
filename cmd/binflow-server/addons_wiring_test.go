package main

// T-282 cmd-side wiring: addonManifest() must carry the complete M10 slot
// set (the compile-time assembly manifest — the one place a slot lands),
// and a real openStack + Deps assembly must serve it on /api/v1/addons with
// the community floor's honest evaluation (five core + properties unlocked,
// everything gated locked). The full newAssembledServer (adapters included)
// runs only once per process (the adapter registry contract); this file
// follows the light-Deps shape the license/metrics wiring tests established
// — the manifest function itself IS the adapter-carrying assembly's input,
// asserted here directly.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
)

// TestT282ManifestComplete: the assembly manifest's slot set, kinds and
// tier marks — the FR-86-AC1 guard at the source of truth.
func TestT282ManifestComplete(t *testing.T) {
	r := addonManifest()
	if r.Len() != 17 {
		t.Fatalf("manifest carries %d slots, want 17", r.Len())
	}
	want := map[string]struct {
		kind addons.Kind
		tier license.Tier
	}{
		"generic":          {addons.KindPackageType, license.TierCommunity},
		"docker":           {addons.KindPackageType, license.TierCommunity},
		"maven":            {addons.KindPackageType, license.TierCommunity},
		"npm":              {addons.KindPackageType, license.TierCommunity},
		"pypi":             {addons.KindPackageType, license.TierCommunity},
		"go":               {addons.KindPackageType, license.TierPro},
		"nuget":            {addons.KindPackageType, license.TierPro},
		"cargo":            {addons.KindPackageType, license.TierPro},
		"conan":            {addons.KindPackageType, license.TierPro}, // T-308
		"helm":             {addons.KindPackageType, license.TierPro}, // T-309
		"rpm":              {addons.KindPackageType, license.TierPro}, // T-311
		"debian":           {addons.KindPackageType, license.TierPro}, // T-310
		"properties":       {addons.KindFeature, license.TierCommunity},
		"repo-operations":  {addons.KindFeature, license.TierPro}, // T-339 (Q4 ruling)
		"trashcan":         {addons.KindFeature, license.TierPro}, // T-345 (Q3 interim)
		"ha":               {addons.KindFeature, license.TierEnterprise},
		"xray-integration": {addons.KindFeature, license.TierEnterprise},
	}
	for _, a := range r.All() {
		w, ok := want[a.ID]
		if !ok {
			t.Fatalf("unexpected slot %q in the assembly manifest", a.ID)
		}
		if a.Kind != w.kind || a.MinTier != w.tier {
			t.Fatalf("slot %s = (%s, %s), want (%s, %s)", a.ID, a.Kind, a.MinTier, w.kind, w.tier)
		}
	}
	// The five mounted adapters must all carry slots (httpapi.New's guard
	// enforces this on the real assembly; the manifest is asserted directly
	// so a missing descriptor fails HERE with the slot named).
	for _, proto := range []string{"generic", "docker", "maven", "npm", "pypi"} {
		if _, ok := r.ForPackageType(proto); !ok {
			t.Fatalf("mounted adapter %q has no slot in the manifest (httpapi.New would panic)", proto)
		}
	}
}

// TestT282AddonsServedOnRealStack: openStack's real license Manager joined
// with the manifest over the real router chain — the unlicensed floor's
// wire matrix (L06's display leg).
func TestT282AddonsServedOnRealStack(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()

	st, err := openStack(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(slog.New(slog.NewTextHandler(io.Discard, nil)))

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     st.authSvc,
		Authz:    st.authSvc,
		Metadata: st.md,
		Repos:    st.md.Repos(),
		License:  st.licenseMgr,
		Addons:   addonManifest(),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/binflow/api/v1/addons", nil)
	req.SetBasicAuth("admin", "password")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET addons: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET addons = %d %s", resp.StatusCode, body)
	}

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("addons body not a bare array: %v (%s)", err, body)
	}
	if len(rows) != 17 {
		t.Fatalf("real-stack addons carries %d slots, want 17: %s", len(rows), body)
	}
	unlocked := map[string]bool{"generic": true, "docker": true, "maven": true, "npm": true, "pypi": true, "properties": true}
	for _, row := range rows {
		id, _ := row["id"].(string)
		if want, ok := unlocked[id]; ok {
			if row["enabled"] != want {
				t.Fatalf("floor slot %s enabled = %v, want true: %s", id, row["enabled"], body)
			}
			continue
		}
		if row["enabled"] != false || !strings.Contains(row["reason"].(string), "license tier community <") {
			t.Fatalf("gated slot %s wrong on the unlicensed floor: %s", id, body)
		}
	}
}
