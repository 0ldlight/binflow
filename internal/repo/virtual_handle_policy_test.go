package repo_test

// T-541: the handle* policy skips of the virtual resolution walk
// (virtual-resolution.md sections 3.4/3.6 — design virtual-four-bucket.md
// section 3 rule 5's formerly unimplemented half, ADR-0051 Errata 一-①'s
// F8 seam collection): a member whose handleReleases=false is dropped from
// a release-resolvable path — body step AND cache projection both — a
// member whose handleSnapshots=false is dropped from a snapshot-family
// path, and §3.4's checksum-sidecar clause keeps a release-refusing member
// serving digest files. The four-quadrant matrix pins both facets of one
// remote member plus the local-member face; skipped families must resolve
// to the honest 404 with ZERO upstream contact.

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

// TestVirtualHandlePolicyMatrix: the four handle* quadrants on one REMOTE
// member — both facets of the member live in the walk (cache projection
// and body). A family the pair refuses resolves to the 404 with zero
// upstream hits (both steps skipped); a family it accepts resolves through
// the body's pull-through (exactly one upstream hit, the cache step misses
// on a cold namespace without touching the upstream).
func TestVirtualHandlePolicyMatrix(t *testing.T) {
	const rel = "org/lib/1.0/org-lib-1.0.jar"
	const snap = "org/lib/1.1-SNAPSHOT/org-lib-1.1-SNAPSHOT.jar"
	tests := []struct {
		name            string
		handleReleases  bool
		handleSnapshots bool
		wantRelease     bool
		wantSnapshot    bool
	}{
		{name: "true/true: both families resolve", handleReleases: true, handleSnapshots: true, wantRelease: true, wantSnapshot: true},
		{name: "true/false: release resolves, snapshot refused", handleReleases: true, handleSnapshots: false, wantRelease: true, wantSnapshot: false},
		{name: "false/true: release refused, snapshot resolves", handleReleases: false, handleSnapshots: true, wantRelease: false, wantSnapshot: true},
		{name: "false/false: both families refused", handleReleases: false, handleSnapshots: false, wantRelease: false, wantSnapshot: false},
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

			// The release family.
			body, _, _, err := getVirtual(t, fx, rel)
			if tt.wantRelease {
				if err != nil {
					t.Fatalf("release Get: %v", err)
				}
				if body != "rel-body" {
					t.Fatalf("release body = %q, want rel-body", body)
				}
				hits++
				if got := fx.hits["rem"].Load(); got != hits {
					t.Fatalf("release upstream hits = %d, want %d (the body step's pull-through)", got, hits)
				}
			} else {
				if !errors.Is(err, repo.ErrNodeNotFound) {
					t.Fatalf("refused release Get = %v, want ErrNodeNotFound", err)
				}
				if got := fx.hits["rem"].Load(); got != hits {
					t.Fatalf("refused release upstream hits = %d, want %d (both steps skipped)", got, hits)
				}
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

// TestVirtualReleaseSkipDropsCacheFacet: the projection-level half of the
// release skip — a STANDING COPY in the refusing member's namespace must
// not serve through the virtual (the cache facet inherits the refusal):
// the 404 stands beside a copy the direct face just proved is there, with
// zero further upstream contact. (The §3.4 sidecar exemption's remote face
// is structurally unreachable — the FR-20 engine refuses checksum-suffix
// fetches outright, so a remote member's namespace never holds sidecar
// rows; the exemption's observable face is the LOCAL member, pinned in
// TestVirtualLocalReleaseSkipSidecarExempt.)
func TestVirtualReleaseSkipDropsCacheFacet(t *testing.T) {
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

	// The release artifact: refused — the cache facet is skipped beside the
	// body, the standing copy does not serve, the upstream is not asked.
	if _, _, _, err := getVirtual(t, fx, rel); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("virtual Get of refused release = %v, want ErrNodeNotFound (the standing copy must not serve)", err)
	}
	if got := fx.hits["rem"].Load(); got != hits {
		t.Fatalf("refused release upstream hits = %d, want unchanged %d", got, hits)
	}

	// The same member's snapshot family still resolves (the policy is
	// family-keyed, not member-wide): the body step pulls through.
	body, _ := mustGetVirtual(t, fx, snap)
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
// remote canonical form drops the pair). The four quadrants against the
// local member's node rows.
func TestVirtualLocalHandlePolicyMatrix(t *testing.T) {
	const rel = "com/acme/lib/1.0/lib-1.0.jar"
	const snap = "com/acme/lib/1.1-SNAPSHOT/lib-1.1-SNAPSHOT.jar"
	tests := []struct {
		name            string
		handleReleases  bool
		handleSnapshots bool
		wantRelease     bool
		wantSnapshot    bool
	}{
		{name: "true/true: both families resolve", handleReleases: true, handleSnapshots: true, wantRelease: true, wantSnapshot: true},
		{name: "true/false: release resolves, snapshot refused", handleReleases: true, handleSnapshots: false, wantRelease: true, wantSnapshot: false},
		{name: "false/true: release refused, snapshot resolves", handleReleases: false, handleSnapshots: true, wantRelease: false, wantSnapshot: true},
		{name: "false/false: both families refused", handleReleases: false, handleSnapshots: false, wantRelease: false, wantSnapshot: false},
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

			body, _, _, gerr := getVirtual(t, fx, rel)
			if tt.wantRelease {
				if gerr != nil || body != "rel-body" {
					t.Fatalf("release Get = (%q, %v), want rel-body", body, gerr)
				}
			} else if !errors.Is(gerr, repo.ErrNodeNotFound) {
				t.Fatalf("refused release Get = %v, want ErrNodeNotFound", gerr)
			}

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

// TestVirtualLocalReleaseSkipSidecarExempt: §3.4's sidecar clause on the
// local face — the release-refusing member still serves the digest beside
// the release artifact it refuses, and its own direct face is untouched
// (the policy governs the virtual walk only).
func TestVirtualLocalReleaseSkipSidecarExempt(t *testing.T) {
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

	if _, _, _, err := getVirtual(t, fx, rel); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("virtual Get of refused release = %v, want ErrNodeNotFound", err)
	}
	body, _ := mustGetVirtual(t, fx, side)
	if body != "digest" {
		t.Fatalf("sidecar body = %q, want the exempt digest", body)
	}
	rc, _, err := e.svc.Get(context.Background(), admin(), "loc", rel)
	if err != nil {
		t.Fatalf("direct member Get of the refused release: %v (the policy governs the virtual walk only)", err)
	}
	rc.Close() //nolint:errcheck // read-only fd
}
