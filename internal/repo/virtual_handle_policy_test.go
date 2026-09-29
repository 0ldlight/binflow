package repo_test

// T-541 planted the handle* policy skips of the virtual resolution walk
// (virtual-resolution.md sections 3.4/3.6 — design virtual-four-bucket.md
// section 3 rule 5's formerly unimplemented half, ADR-0051 Errata 一-①'s
// F8 seam collection); T-556 (BIN-38, L033 Arm C's differential, live A
// 7.161.26 r3≡r4) removed the §3.4 RELEASE half — the measurable face of
// the rule family, the maven module-level metadata merge, INCLUDES a
// handleReleases=false member's SNAPSHOT version on the reference, and
// this walk face is unconstructible on both sides (a release artifact
// cannot sit in a handleReleases=false member: A 409s the PUT and the
// direct GET, B's ME-08 refuses the PUT). What remains: a member whose
// handleSnapshots=false is dropped from a snapshot-family path (§3.6,
// L033 Arm C kept that face double-sided); release-resolvable paths
// consult no handle flag. Skipped snapshot families resolve to the
// honest 404 with ZERO upstream contact.

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedRemoteHandlePolicy writes the handle* pair into a REMOTE member row's
// config directly through the store: the canonical write plane has no
// handle* seats (ADR-0051 Errata 一-① — parseRemoteConfig drops unknown
// fields), so this is the shape the seats will write the day they land.
// The walk recomputes per request, so the seed is effective immediately.
func seedRemoteHandlePolicy(t *testing.T, e *env, key string, releases, snapshots bool) {
	t.Helper()
	ctx := context.Background()
	row, err := e.md.Repos().Get(ctx, key)
	if err != nil {
		t.Fatalf("member row %s: %v", key, err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		t.Fatalf("member config %q: %v", row.Config, err)
	}
	cfg["handleReleases"] = releases
	cfg["handleSnapshots"] = snapshots
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("re-render member config: %v", err)
	}
	row.Config = string(b)
	if err := e.md.Repos().Update(ctx, row); err != nil {
		t.Fatalf("seed %s handle policy: %v", key, err)
	}
}

// TestVirtualHandlePolicyMatrix: the handle* quadrants on one REMOTE
// member — both facets of the member live in the walk (cache projection
// and body). Since T-556 the RELEASE family resolves in every quadrant
// (the §3.4 release skip was removed after L033 Arm C's refutation of
// the rule family's measurable face; the walk face itself is vacuous —
// constructibility note in the file header): through the body's
// pull-through on a cold namespace (exactly one upstream hit, the cache
// step's miss probe touches nothing). The SNAPSHOT family stays keyed on
// handleSnapshots (§3.6, double-sided per L033): refused quadrants
// resolve to the 404 with zero upstream hits.
func TestVirtualHandlePolicyMatrix(t *testing.T) {
	const rel = "org/lib/1.0/org-lib-1.0.jar"
	const snap = "org/lib/1.1-SNAPSHOT/org-lib-1.1-SNAPSHOT.jar"
	tests := []struct {
		name            string
		handleReleases  bool
		handleSnapshots bool
		wantSnapshot    bool
	}{
		{name: "true/true: both families resolve", handleReleases: true, handleSnapshots: true, wantSnapshot: true},
		{name: "true/false: release resolves, snapshot refused", handleReleases: true, handleSnapshots: false, wantSnapshot: false},
		{name: "false/true: release STILL resolves (T-556 flip), snapshot resolves", handleReleases: false, handleSnapshots: true, wantSnapshot: true},
		{name: "false/false: release STILL resolves (T-556 flip), snapshot refused", handleReleases: false, handleSnapshots: false, wantSnapshot: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			fx := buildVirtual(t, e, "virt", "", []memberSpec{
				{key: "rem", remote: true, files: map[string]string{
					"/" + rel:  "rel-body",
					"/" + snap: "snap-body",
				}},
			})
			seedRemoteHandlePolicy(t, e, "rem", tt.handleReleases, tt.handleSnapshots)
			hits := int64(0)

			// The release family: resolves in EVERY quadrant (T-556 —
			// handleReleases consults nothing on release paths), through
			// the body step's pull-through (one upstream hit; the cold
			// cache facet's probe misses without upstream contact).
			body, _, _, err := getVirtual(t, fx, rel)
			if err != nil {
				t.Fatalf("release Get under handleReleases=%v: %v", tt.handleReleases, err)
			}
			if body != "rel-body" {
				t.Fatalf("release body = %q, want rel-body", body)
			}
			hits++
			if got := fx.hits["rem"].Load(); got != hits {
				t.Fatalf("release upstream hits = %d, want %d (the body step's pull-through)", got, hits)
			}

			// The snapshot family.
			body, _, _, err = getVirtual(t, fx, snap)
			if tt.wantSnapshot {
				if err != nil {
					t.Fatalf("snapshot Get: %v", err)
				}
				if body != "snap-body" {
					t.Fatalf("snapshot body = %q, want snap-body", body)
				}
				hits++
				if got := fx.hits["rem"].Load(); got != hits {
					t.Fatalf("snapshot upstream hits = %d, want %d (the body step's pull-through; the cache facets are skipped on this plane)", got, hits)
				}
			} else {
				if !errors.Is(err, repo.ErrNodeNotFound) {
					t.Fatalf("refused snapshot Get = %v, want ErrNodeNotFound", err)
				}
				if got := fx.hits["rem"].Load(); got != hits {
					t.Fatalf("refused snapshot upstream hits = %d, want %d (member skipped beside the cache facets)", got, hits)
				}
			}
		})
	}
}

// TestVirtualReleaseCacheFacetServes: the projection half of the T-556
// flip — a STANDING COPY in a release-refusing member's namespace SERVES
// through the virtual (the pre-flip skip 404'd beside a copy the direct
// face had just proven was there; handleReleases consults nothing on
// release paths since L033 Arm C's refutation of the rule family). The
// copy answers with ZERO further upstream contact, and the same member's
// snapshot family still keys on handleSnapshots (the §3.6 skip survives
// the flip). (The former §3.4 sidecar exemption's remote face stays
// structurally unreachable — the FR-20 engine refuses checksum-suffix
// fetches outright, so a remote member's namespace never holds sidecar
// rows.)
func TestVirtualReleaseCacheFacetServes(t *testing.T) {
	const rel = "org/lib/2.0/org-lib-2.0.jar"
	const snap = "org/lib/2.1-SNAPSHOT/org-lib-2.1-SNAPSHOT.jar"
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem", remote: true, files: map[string]string{
			"/" + rel:  "rel-body",
			"/" + snap: "snap-body",
		}},
	})
	seedRemoteHandlePolicy(t, e, "rem", false, true)

	// Warm a standing copy through the member's DIRECT face (the policy
	// governs the virtual walk only; the direct face carries none).
	rc, _, err := e.svc.Get(context.Background(), admin(), "rem", rel)
	if err != nil {
		t.Fatalf("warm %s through the direct face: %v", rel, err)
	}
	rc.Close() //nolint:errcheck // read-only fd
	hits := fx.hits["rem"].Load()
	if hits != 1 {
		t.Fatalf("warm-up upstream hits = %d, want 1 (the MISS landing)", hits)
	}

	// The release artifact: SERVED from the standing copy — the cache
	// facet probes it, zero upstream contact, body step never reached.
	body, _, _, gerr := getVirtual(t, fx, rel)
	if gerr != nil || body != "rel-body" {
		t.Fatalf("virtual Get under handleReleases=false = (%q, %v), want rel-body served from the standing copy", body, gerr)
	}
	if got := fx.hits["rem"].Load(); got != hits {
		t.Fatalf("served-release upstream hits = %d, want unchanged %d (the standing copy answers)", got, hits)
	}

	// The same member's snapshot family still keys on handleSnapshots (§3.6
	// survives the flip): the body step pulls through.
	body, _ = mustGetVirtual(t, fx, snap)
	if body != "snap-body" {
		t.Fatalf("snapshot body = %q, want snap-body (handleSnapshots=true still resolves)", body)
	}
	if got := fx.hits["rem"].Load(); got != hits+1 {
		t.Fatalf("snapshot upstream hits = %d, want %d", got, hits+1)
	}
}

// TestVirtualLocalHandlePolicyMatrix: LOCAL members carry handle* in their
// caller-owned passthrough config — the one spelling REACHABLE through the
// ordinary write plane today (UpdateRepo keeps the blob verbatim; the
// remote canonical form drops the pair). Release paths resolve in every
// quadrant since the T-556 flip (L033 Arm C refuted the rule family's
// measurable face; the walk face is vacuous on both sides — see the file
// header); the snapshot family keys on handleSnapshots.
func TestVirtualLocalHandlePolicyMatrix(t *testing.T) {
	const rel = "com/acme/lib/1.0/lib-1.0.jar"
	const snap = "com/acme/lib/1.1-SNAPSHOT/lib-1.1-SNAPSHOT.jar"
	tests := []struct {
		name            string
		handleReleases  bool
		handleSnapshots bool
		wantSnapshot    bool
	}{
		{name: "true/true: both families resolve", handleReleases: true, handleSnapshots: true, wantSnapshot: true},
		{name: "true/false: release resolves, snapshot refused", handleReleases: true, handleSnapshots: false, wantSnapshot: false},
		{name: "false/true: release STILL resolves (T-556 flip), snapshot resolves", handleReleases: false, handleSnapshots: true, wantSnapshot: true},
		{name: "false/false: release STILL resolves (T-556 flip), snapshot refused", handleReleases: false, handleSnapshots: false, wantSnapshot: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			fx := buildVirtual(t, e, "virt", "", []memberSpec{
				{key: "loc", files: map[string]string{rel: "rel-body", snap: "snap-body"}},
			})
			cfg, err := json.Marshal(map[string]bool{
				"handleReleases":  tt.handleReleases,
				"handleSnapshots": tt.handleSnapshots,
			})
			if err != nil {
				t.Fatalf("render policy config: %v", err)
			}
			if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "loc", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: string(cfg),
			}); err != nil {
				t.Fatalf("set the local member's handle policy: %v", err)
			}

			// Release: resolves in every quadrant (T-556).
			body, _, _, gerr := getVirtual(t, fx, rel)
			if gerr != nil || body != "rel-body" {
				t.Fatalf("release Get under handleReleases=%v = (%q, %v), want rel-body", tt.handleReleases, body, gerr)
			}

			// Snapshot: keyed on handleSnapshots (§3.6 survives).
			body, _, _, gerr = getVirtual(t, fx, snap)
			if tt.wantSnapshot {
				if gerr != nil || body != "snap-body" {
					t.Fatalf("snapshot Get = (%q, %v), want snap-body", body, gerr)
				}
			} else if !errors.Is(gerr, repo.ErrNodeNotFound) {
				t.Fatalf("refused snapshot Get = %v, want ErrNodeNotFound", gerr)
			}
		})
	}
}

// TestVirtualLocalHandleReleasesFalseServesAllPaths: the former §3.4
// sidecar exemption is moot since the T-556 flip removed the release skip
// wholesale — a release-refusing member serves BOTH the release artifact
// and the digest beside it through the virtual (one ordinary path family,
// no special sidecar case remains), and its own direct face is untouched
// (the policy governs the virtual walk only). On the reference this face
// is unconstructible through the ordinary plane (A 409s release PUTs to
// handleReleases=false locals) — the seeded-row leg pins the flipped
// walk's parity with the adapter's module-merge face, which IS evidenced
// (L033 Arm C hwm).
func TestVirtualLocalHandleReleasesFalseServesAllPaths(t *testing.T) {
	const rel = "com/acme/lib/2.0/lib-2.0.jar"
	const side = rel + ".sha1"
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "loc", files: map[string]string{rel: "rel-body", side: "digest"}},
	})
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "loc", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"handleReleases":false}`,
	}); err != nil {
		t.Fatalf("set handleReleases=false: %v", err)
	}

	body, _, _, err := getVirtual(t, fx, rel)
	if err != nil || body != "rel-body" {
		t.Fatalf("virtual Get of the release under handleReleases=false = (%q, %v), want rel-body", body, err)
	}
	body, _ = mustGetVirtual(t, fx, side)
	if body != "digest" {
		t.Fatalf("sidecar body = %q, want digest (no sidecar special case remains)", body)
	}
	rc, _, err := e.svc.Get(context.Background(), admin(), "loc", rel)
	if err != nil {
		t.Fatalf("direct member Get of the release under handleReleases=false: %v (the policy governs the virtual walk only)", err)
	}
	rc.Close() //nolint:errcheck // read-only fd
}
