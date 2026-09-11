package repo_test

// L006-A (D02-R03/R04, the P0 partial debt): the cross-rclass round-trip of
// the four T-439-pinned domains on the REMOTE and VIRTUAL arms. The local
// arm's contract lives in local_config_fields_test.go; here the scope is the
// live reference's own (:8082, 7.161.20): a remote repository round-trips
// ALL FOUR (repoLayoutRef defaulting to maven-2-default, the other three
// verbatim, negative snapshot caps included), while a virtual repository
// keeps ONLY repoLayoutRef — blackedOut/maxUniqueSnapshots/
// archiveBrowsingEnabled sent to a virtual repository are dropped BY THE
// REFERENCE TOO, and BinFlow copies that drop rather than inventing seats.

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// mustCreateVirtual creates one virtual repository around the given config
// after seeding its member.
func mustCreateVirtual(t testing.TB, e *env, key, config string) {
	t.Helper()
	mustCreateTypedRepo(t, e, key+"-member", repo.PackageGeneric)
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: config,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual %s): %v", key, err)
	}
}

// TestRemoteFourDomainRoundTrip: create and update round-trip the four
// domains on the remote canonical form, with the reference's defaults and
// its verbatim-negative posture.
func TestRemoteFourDomainRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		body string
		want map[string]any
	}{
		{"all four set", `{"url":"http://u","repoLayoutRef":"ivy-default",` +
			`"blackedOut":true,"maxUniqueSnapshots":7,"archiveBrowsingEnabled":true}`,
			map[string]any{"repoLayoutRef": "ivy-default", "blackedOut": true,
				"maxUniqueSnapshots": float64(7), "archiveBrowsingEnabled": true}},
		{"bare url materializes defaults", `{"url":"http://u"}`,
			map[string]any{"repoLayoutRef": "maven-2-default", "blackedOut": false,
				"maxUniqueSnapshots": float64(0), "archiveBrowsingEnabled": false}},
		{"explicit product defaults survive (K71)", `{"url":"http://u",` +
			`"blackedOut":false,"maxUniqueSnapshots":0,"archiveBrowsingEnabled":false}`,
			map[string]any{"blackedOut": false, "maxUniqueSnapshots": float64(0),
				"archiveBrowsingEnabled": false}},
		{"negative snapshot cap stores verbatim (reference echoes -1)", `{"url":"http://u","maxUniqueSnapshots":-1}`,
			map[string]any{"maxUniqueSnapshots": float64(-1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			e := newEnv(t)
			// Create arm, then the update arm on the same row (full-replace
			// semantics — the same blob re-sent through UpdateRepo).
			mustCreateRemote(t, e, "rr", tt.body)
			if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: "rr", Type: repo.TypeRemote, Config: tt.body,
			}); err != nil {
				t.Fatalf("UpdateRepo: %v", err)
			}
			for _, round := range []string{"create", "update"} {
				row, err := e.svc.GetRepo(ctx, admin(), "rr")
				if err != nil {
					t.Fatalf("GetRepo (%s): %v", round, err)
				}
				cfg := remoteCfgOf(t, row)
				for k, want := range tt.want {
					if cfg[k] != want {
						t.Errorf("%s round-trip: %s = %v (%T), want %v", round, k, cfg[k], cfg[k], want)
					}
				}
			}
		})
	}
}

// TestVirtualRepoLayoutRefRoundTrip: the virtual arm keeps repoLayoutRef
// (echo when set, absent when not — no default) and DROPS the other three
// domains exactly like the reference does.
func TestVirtualRepoLayoutRefRoundTrip(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateVirtual(t, e, "vv", `{"repositories":["vv-member"],"repoLayoutRef":"simple-default",`+
		`"blackedOut":true,"maxUniqueSnapshots":9,"archiveBrowsingEnabled":true}`)
	row, err := e.svc.GetRepo(ctx, admin(), "vv")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	cfg := remoteCfgOf(t, row) // decode helper is shape-agnostic
	if cfg["repoLayoutRef"] != "simple-default" {
		t.Errorf("repoLayoutRef = %v, want simple-default (full echo %v)", cfg["repoLayoutRef"], cfg)
	}
	for _, dropped := range []string{"blackedOut", "maxUniqueSnapshots", "archiveBrowsingEnabled"} {
		if _, ok := cfg[dropped]; ok {
			t.Errorf("virtual echo carries %q — the reference drops it on the virtual arm (full echo %v)",
				dropped, cfg)
		}
	}

	// The bare virtual config (no repoLayoutRef) echoes WITHOUT the key —
	// the reference's virtual arm has no xsd default.
	mustCreateVirtual(t, e, "vv2", `{"repositories":["vv2-member"]}`)
	row2, err := e.svc.GetRepo(ctx, admin(), "vv2")
	if err != nil {
		t.Fatalf("GetRepo(v2): %v", err)
	}
	cfg2 := remoteCfgOf(t, row2)
	if _, ok := cfg2["repoLayoutRef"]; ok {
		t.Errorf("bare virtual echo carries repoLayoutRef %v — the reference omits it when unset", cfg2)
	}
}
