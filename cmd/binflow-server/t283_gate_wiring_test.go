package main

// T-283 cmd-side wiring: openStack must (1) flow config's addons.disabled
// CSV into the license Manager (the breaker live from the first request,
// WARN on a core id), (2) attach the packageTypeGate onto repo.Service (the
// D3 weave live — a gated slot's create refuses on the community floor, a
// five-core create does not), and (3) the gate adapter's verdict table
// itself: unknown/floor/gated/disabled/allowlist derivations.

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT283DisabledCSVFlowsToManager: the config key reaches the Manager
// through openStack — the ids are off, everything else stays on, and a CORE
// id in the list logs the WARN (the breaker is legal but loud).
func TestT283DisabledCSVFlowsToManager(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Addons.Disabled = "npm,go"

	logger, logs := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	ctx := context.Background()
	if st.licenseMgr.AddonEnabled(ctx, "npm", license.TierCommunity) {
		t.Fatal("npm (core, in the CSV) still enabled")
	}
	if st.licenseMgr.AddonEnabled(ctx, "go", license.TierPro) {
		t.Fatal("go (in the CSV) still enabled")
	}
	if !st.licenseMgr.AddonEnabled(ctx, "generic", license.TierCommunity) {
		t.Fatal("generic disturbed by the breaker")
	}
	if !strings.Contains(logs(), "addons.disabled includes a core package type") {
		t.Fatalf("the core-id WARN is missing from the boot log:\n%s", logs())
	}
}

// TestT283PackageTypeGateAttached: the D3 weave is live on the assembled
// service — a gated slot's create refuses on the community floor with the
// pointed clause, a five-core create is untouched.
func TestT283PackageTypeGateAttached(t *testing.T) {
	cfg := configDefaults()
	cfg.Storage.DataDir = t.TempDir()
	logger, _ := captureLogger(t)
	st, err := openStack(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("openStack: %v", err)
	}
	defer st.close(logger)

	if st.addonsReg == nil || st.addonsReg.Len() != 20 {
		t.Fatalf("stack.addonsReg = %v, want the 20-slot manifest", st.addonsReg)
	}
	admin := &repo.Principal{Name: "admin", Admin: true}
	_, err = st.svc.CreateRepo(context.Background(), admin, &metadata.Repo{
		RepoKey: "t283-go", Type: repo.TypeLocal, PackageType: "go",
	})
	if err == nil || !strings.Contains(err.Error(), "license tier 'community' < 'pro'") {
		t.Fatalf("gated create on the assembled service = %v, want the D3 clause", err)
	}
	if _, err := st.svc.CreateRepo(context.Background(), admin, &metadata.Repo{
		RepoKey: "t283-generic", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("five-core create disturbed: %v", err)
	}
}

// fakeCmdEval is the gate adapter's license facet under test control.
type fakeCmdEval struct {
	state license.State
	allow func(id string, need license.Tier) bool
}

func (f fakeCmdEval) AddonEnabled(_ context.Context, id string, need license.Tier) bool {
	return f.allow(id, need)
}
func (f fakeCmdEval) State() license.State { return f.state }

// TestT283GateAdapterVerdicts: the refusal derivation order — disabled
// config first, then the tier floor, then the allowlist — so the clause
// always names the DECIDING cause (the view/statusOf mirror).
func TestT283GateAdapterVerdicts(t *testing.T) {
	reg := addonManifest()
	ctx := context.Background()

	floor := fakeCmdEval{
		state: license.State{Tier: license.TierCommunity},
		allow: func(_ string, need license.Tier) bool { return need <= license.TierCommunity },
	}
	pro := fakeCmdEval{
		state: license.State{Licensed: true, Tier: license.TierPro},
		allow: func(_ string, need license.Tier) bool { return license.TierPro >= need },
	}
	breaker := fakeCmdEval{
		state: license.State{Licensed: true, Tier: license.TierEnterprise},
		allow: func(id string, _ license.Tier) bool { return id != "npm" },
	}
	narrow := fakeCmdEval{
		state: license.State{Licensed: true, Tier: license.TierEnterprise},
		allow: func(id string, need license.Tier) bool { return need <= license.TierCommunity || id == "ha" },
	}

	tests := []struct {
		name      string
		ev        addons.Evaluator
		pt        string
		known     bool
		unlocked  bool
		refusalIn string
	}{
		{name: "unknown type", ev: floor, pt: "wheelbarrow"},
		{name: "floor slot", ev: floor, pt: "generic", known: true, unlocked: true},
		{name: "gated at community", ev: floor, pt: "go", known: true,
			refusalIn: "license tier 'community' < 'pro'"},
		{name: "gated under pro", ev: pro, pt: "go", known: true, unlocked: true},
		{name: "breaker outranks the document", ev: breaker, pt: "npm", known: true,
			refusalIn: "disabled by configuration"},
		{name: "allowlist narrows the grant", ev: narrow, pt: "go", known: true,
			refusalIn: "not named in the license addon allowlist"},
		{name: "allowlist spares the named slot", ev: narrow, pt: "ha", known: true, unlocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := packageTypeGate{reg: reg, ev: tt.ev}
			v := g.Verdict(ctx, tt.pt)
			if v.Known != tt.known || v.Unlocked != tt.unlocked {
				t.Fatalf("verdict = %+v, want known=%v unlocked=%v", v, tt.known, tt.unlocked)
			}
			if tt.refusalIn != "" && !strings.Contains(v.Refusal, tt.refusalIn) {
				t.Fatalf("refusal = %q, want it to contain %q", v.Refusal, tt.refusalIn)
			}
			if tt.refusalIn == "" && v.Refusal != "" {
				t.Fatalf("unlocked/unknown verdict carries a refusal: %q", v.Refusal)
			}
		})
	}
}
