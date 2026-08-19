package repo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-71 virtual resolution suite: two-bucket ordering, first-hit-stops
// downloads over local/remote members, the R10 stale/miss rule, the
// exploratory-miss cache hygiene, and the write plane's route-or-405 — the
// service-layer shape of M50 through M53.

// memberSpec declares one virtual member for the matrix builders.
type memberSpec struct {
	key    string
	remote bool
	mark   bool // priorityResolution — the bucket-1 ticket
	// files is the member's content: local members hold these as nodes
	// (seeded through Put), remote members serve them from a counting mock
	// upstream (keys with the leading slash the URL path carries).
	files map[string]string
	// extra appends to the remote member's config (TTL/hardFail knobs).
	extra string
}

// virtualFixture is one built virtual-repository scenario.
type virtualFixture struct {
	e         *env
	vkey      string
	upstreams map[string]*httptest.Server // remote member key -> upstream
	hits      map[string]*atomic.Int64
}

// buildVirtual assembles the whole scenario: the members (each remote with
// its own counting upstream), the virtual repository over them, and the
// local members' seeded content. The virtual's config lists the members in
// the slice's order — the declaration order under test.
func buildVirtual(t *testing.T, e *env, vkey, route string, members []memberSpec) *virtualFixture {
	t.Helper()
	ctx := context.Background()
	fx := &virtualFixture{
		e:         e,
		vkey:      vkey,
		upstreams: map[string]*httptest.Server{},
		hits:      map[string]*atomic.Int64{},
	}
	for _, m := range members {
		if m.remote {
			files := m.files
			if files == nil {
				files = map[string]string{}
			}
			srv, hits := countingUpstream(t, files)
			cfg := remoteCfg(srv.URL, m.extra)
			if m.mark {
				cfg = strings.TrimSuffix(cfg, "}") + `,"priorityResolution":true}`
			}
			mustCreateRemote(t, e, m.key, cfg)
			fx.upstreams[m.key] = srv
			fx.hits[m.key] = hits
			continue
		}
		cfg := "{}"
		if m.mark {
			cfg = `{"priorityResolution":true}`
		}
		if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: m.key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: cfg,
		}); err != nil {
			t.Fatalf("CreateRepo(local %s): %v", m.key, err)
		}
		for p, body := range m.files {
			put(t, e, admin(), m.key, p, body)
		}
	}
	config := `{"repositories":["` + joinKeys(members) + `"]`
	if route != "" {
		config += `,"defaultDeploymentRepo":"` + route + `"`
	}
	config += `}`
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: vkey, Type: repo.TypeVirtual, PackageType: repo.PackageGeneric, Config: config,
	}); err != nil {
		t.Fatalf("CreateRepo(virtual %s): %v", vkey, err)
	}
	return fx
}

// joinKeys renders the member keys as a JSON array body fragment.
func joinKeys(members []memberSpec) string {
	keys := make([]string, len(members))
	for i, m := range members {
		keys[i] = m.key
	}
	return strings.Join(keys, `","`)
}

// getVirtual reads one path through the virtual and returns body + hints.
func getVirtual(t *testing.T, fx *virtualFixture, path string) (string, http.Header, *metadata.Node, error) {
	t.Helper()
	rc, node, err := fx.e.svc.Get(context.Background(), admin(), fx.vkey, path)
	if err != nil {
		return "", nil, nil, err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	body, rerr := io.ReadAll(rc)
	if rerr != nil {
		t.Fatalf("read virtual body: %v", rerr)
	}
	var hints http.Header
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		hints = extra.ExtraHeaders()
	}
	return string(body), hints, node, nil
}

// mustGetVirtual is getVirtual for the happy path.
func mustGetVirtual(t *testing.T, fx *virtualFixture, path string) (string, http.Header) {
	t.Helper()
	body, hints, _, err := getVirtual(t, fx, path)
	if err != nil {
		t.Fatalf("Get(%s/%s): %v", fx.vkey, path, err)
	}
	return body, hints
}

// ---- the local/remote cross-hit and two-bucket-order matrix ----

// TestVirtualResolutionMatrix walks the M50/M51/AC9 decision table: which
// member serves, in what header-observable way, for every combination of
// member classes, marks and declaration orders.
func TestVirtualResolutionMatrix(t *testing.T) {
	const path = "com/acme/lib/1.0/lib-1.0.jar"
	tests := []struct {
		name      string
		members   []memberSpec
		wantBody  string
		wantFrom  string
		wantCache string // "" = no X-BinFlow-Cache expected (local member)
	}{
		{
			name:     "local member hit, remote member miss",
			members:  []memberSpec{{key: "loc-a", files: map[string]string{path: "from-local"}}, {key: "rem-b", remote: true}},
			wantBody: "from-local", wantFrom: "loc-a",
		},
		{
			name:     "remote member pull-through hit with local empty (M50 upstream package)",
			members:  []memberSpec{{key: "loc-a"}, {key: "rem-b", remote: true, files: map[string]string{"/" + path: "from-upstream"}}},
			wantBody: "from-upstream", wantFrom: "rem-b", wantCache: "MISS",
		},
		{
			name: "declaration order decides between two locals (M51)",
			members: []memberSpec{
				{key: "loc-a", files: map[string]string{path: "A"}},
				{key: "loc-b", files: map[string]string{path: "B"}},
			},
			wantBody: "A", wantFrom: "loc-a",
		},
		{
			name: "declaration order decides between two remotes",
			members: []memberSpec{
				{key: "rem-a", remote: true, files: map[string]string{"/" + path: "A"}},
				{key: "rem-b", remote: true, files: map[string]string{"/" + path: "B"}},
			},
			wantBody: "A", wantFrom: "rem-a", wantCache: "MISS",
		},
		{
			name: "remote before local both present: remote wins (two buckets, no implicit local-first)",
			members: []memberSpec{
				{key: "rem-a", remote: true, files: map[string]string{"/" + path: "from-remote"}},
				{key: "loc-b", files: map[string]string{path: "from-local"}},
			},
			wantBody: "from-remote", wantFrom: "rem-a", wantCache: "MISS",
		},
		{
			name: "priority mark promotes the later member (AC9)",
			members: []memberSpec{
				{key: "loc-a", files: map[string]string{path: "A"}},
				{key: "loc-b", mark: true, files: map[string]string{path: "B"}},
			},
			wantBody: "B", wantFrom: "loc-b",
		},
		{
			name: "priority bucket keeps declaration order among the marked",
			members: []memberSpec{
				{key: "loc-a", mark: true, files: map[string]string{path: "A"}},
				{key: "loc-b", mark: true, files: map[string]string{path: "B"}},
				{key: "loc-c", files: map[string]string{path: "C"}},
			},
			wantBody: "A", wantFrom: "loc-a",
		},
		{
			name: "marked remote beats an unmarked earlier local",
			members: []memberSpec{
				{key: "loc-a", files: map[string]string{path: "from-local"}},
				{key: "rem-b", remote: true, mark: true, files: map[string]string{"/" + path: "from-remote"}},
			},
			wantBody: "from-remote", wantFrom: "rem-b", wantCache: "MISS",
		},
		{
			name: "marked local beats an unmarked earlier remote",
			members: []memberSpec{
				{key: "rem-a", remote: true, files: map[string]string{"/" + path: "from-remote"}},
				{key: "loc-b", mark: true, files: map[string]string{path: "from-local"}},
			},
			wantBody: "from-local", wantFrom: "loc-b",
		},
		{
			name: "mixed marks: marked pair first in declaration order, then the rest",
			members: []memberSpec{
				{key: "loc-a", files: map[string]string{path: "rest-a"}},
				{key: "rem-b", remote: true, mark: true, files: map[string]string{"/" + path: "prio-rem"}},
				{key: "loc-c", mark: true, files: map[string]string{path: "prio-loc"}},
				{key: "loc-d", files: map[string]string{path: "rest-d"}},
			},
			wantBody: "prio-rem", wantFrom: "rem-b", wantCache: "MISS",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := buildVirtual(t, newEnv(t), "virt", "", tt.members)
			body, hints := mustGetVirtual(t, fx, path)
			if body != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
			if got := hints.Get(repo.HdrResolvedFrom); got != tt.wantFrom {
				t.Fatalf("%s = %q, want %q", repo.HdrResolvedFrom, got, tt.wantFrom)
			}
			if got := hints.Get("X-BinFlow-Cache"); got != tt.wantCache {
				t.Fatalf("X-BinFlow-Cache = %q, want %q", got, tt.wantCache)
			}
		})
	}
}

// TestVirtualAllMiss404: no member holds the path — the virtual answers the
// ordinary download-side miss (RE-07's tail).
func TestVirtualAllMiss404(t *testing.T) {
	fx := buildVirtual(t, newEnv(t), "virt", "", []memberSpec{
		{key: "loc-a"}, {key: "rem-b", remote: true},
	})
	_, _, _, err := getVirtual(t, fx, "no/such.bin")
	if !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("all-miss Get = %v, want ErrNodeNotFound", err)
	}
}

// TestVirtualFolderMembersKeepWalking: a local member holding only a FOLDER
// row at the path is not a download hit — the walk continues to the member
// that holds the artifact (aggregate browse is P2, FR-21-AC8).
func TestVirtualFolderMembersKeepWalking(t *testing.T) {
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "loc-folder"},
		{key: "loc-file", files: map[string]string{"com/acme/a.jar": "artifact"}},
	})
	put(t, e, admin(), "loc-folder", "com/acme/", "") // folder node only
	body, hints := mustGetVirtual(t, fx, "com/acme/a.jar")
	if body != "artifact" || hints.Get(repo.HdrResolvedFrom) != "loc-file" {
		t.Fatalf("body = %q from %q, want the file member behind the folder member", body, hints.Get(repo.HdrResolvedFrom))
	}
}

// ---- the R10 stale/miss rule (FR-21-AC7) ----

// TestVirtualRemoteStaleHitDoesNotSkip: a remote member holding an EXPIRED
// copy that the upstream no longer serves is STILL that member's result
// (expired-but-serving, STALE + X-Binflow-Upstream-Error) — the walk must
// not fall through to a later member that holds fresher content. This is
// the core R10/C3 assertion.
func TestVirtualRemoteStaleHitDoesNotSkip(t *testing.T) {
	const path = "junit/junit/4.13.2/junit-4.13.2.jar"
	files := map[string]string{"/" + path: "old-copy"}
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-short-ttl", remote: true, files: files, extra: `,"retrievalCachePeriodSecs":1`},
		{key: "loc-fresher", files: map[string]string{path: "fresher-local"}},
	})

	// Warm the remote member's cache through the virtual itself (a MISS
	// landing in the member's namespace — the M50 flow).
	if body, _ := mustGetVirtual(t, fx, path); body != "old-copy" {
		t.Fatalf("warm-up body = %q", body)
	}
	// Expire the copy, then break the upstream's knowledge of the path.
	e.clk.Advance(2 * time.Second)
	delete(files, "/"+path)

	// The stale copy still serves from the remote member.
	body, hints := mustGetVirtual(t, fx, path)
	if body != "old-copy" {
		t.Fatalf("stale-resolution body = %q, want the expired copy (must not fall through to the fresher local member)", body)
	}
	if got := hints.Get(repo.HdrResolvedFrom); got != "rem-short-ttl" {
		t.Fatalf("%s = %q, want the stale remote member", repo.HdrResolvedFrom, got)
	}
	if got := hints.Get("X-BinFlow-Cache"); got != "STALE" {
		t.Fatalf("X-BinFlow-Cache = %q, want STALE", got)
	}
	if got := hints.Get("X-Binflow-Upstream-Error"); got == "" {
		t.Fatalf("X-Binflow-Upstream-Error missing on a stale serve")
	}

	// Composition note (pinned deliberately): the upstream 404 that served
	// the stale copy also wrote the member's NEGATIVE row (the six-step
	// order T-66 pinned: the negative check precedes the local copy). The
	// NEXT request inside that window is therefore a TRUE member miss and
	// DOES continue down the buckets — C3's enumeration lists the negative
	// cache as a continue case.
	body, hints = mustGetVirtual(t, fx, path)
	if body != "fresher-local" || hints.Get(repo.HdrResolvedFrom) != "loc-fresher" {
		t.Fatalf("post-negative body = %q from %q, want the local member after the negative window opened", body, hints.Get(repo.HdrResolvedFrom))
	}
}

// TestVirtualTrueMissFallsThrough: every flavor of a member's TRUE 404 —
// upstream miss with no copy, negative cache, assumed-offline without a
// copy — continues down the bucket order (FR-21-AC7's enumeration).
func TestVirtualTrueMissFallsThrough(t *testing.T) {
	const path = "org/app/1.0/app-1.0.jar"

	t.Run("upstream 404 without a copy", func(t *testing.T) {
		e := newEnv(t)
		fx := buildVirtual(t, e, "virt", "", []memberSpec{
			{key: "rem-miss", remote: true},
			{key: "loc-has", files: map[string]string{path: "local-copy"}},
		})
		body, hints := mustGetVirtual(t, fx, path)
		if body != "local-copy" || hints.Get(repo.HdrResolvedFrom) != "loc-has" {
			t.Fatalf("body = %q from %q, want the local member behind the missing remote", body, hints.Get(repo.HdrResolvedFrom))
		}
		if got := fx.hits["rem-miss"].Load(); got != 1 {
			t.Fatalf("miss upstream hits = %d, want 1", got)
		}
	})

	t.Run("negative cache answers before the walk continues", func(t *testing.T) {
		e := newEnv(t)
		fx := buildVirtual(t, e, "virt", "", []memberSpec{
			{key: "rem-neg", remote: true},
			{key: "loc-has", files: map[string]string{path: "local-copy"}},
		})
		// A direct GET remembers the miss (the negative row is the member's
		// own state — direct traffic keeps its side effects).
		if _, _, err := e.svc.Get(context.Background(), admin(), "rem-neg", path); !errors.Is(err, repo.ErrNodeNotFound) {
			t.Fatalf("direct miss = %v", err)
		}
		if got := fx.hits["rem-neg"].Load(); got != 1 {
			t.Fatalf("direct miss upstream hits = %d, want 1", got)
		}
		// The virtual probe answers from the negative row: no further
		// upstream traffic, and the pre-existing row is PRESERVED (the
		// cleanup only drops what a probe itself wrote).
		body, hints := mustGetVirtual(t, fx, path)
		if body != "local-copy" || hints.Get(repo.HdrResolvedFrom) != "loc-has" {
			t.Fatalf("body = %q from %q, want the local member", body, hints.Get(repo.HdrResolvedFrom))
		}
		if got := fx.hits["rem-neg"].Load(); got != 1 {
			t.Fatalf("upstream hits after negative-window probe = %d, want 1", got)
		}
		if _, err := e.md.Remote().GetCache(context.Background(), "rem-neg", path); err != nil {
			t.Fatalf("pre-existing negative row must survive the probe: %v", err)
		}
	})

	t.Run("assumed offline without a copy", func(t *testing.T) {
		e := newEnv(t)
		fx := buildVirtual(t, e, "virt", "", []memberSpec{
			{key: "rem-dead", remote: true},
			{key: "loc-has", files: map[string]string{path: "local-copy"}},
		})
		fx.upstreams["rem-dead"].Close() // transport refused -> offline mark
		body, hints := mustGetVirtual(t, fx, path)
		if body != "local-copy" || hints.Get(repo.HdrResolvedFrom) != "loc-has" {
			t.Fatalf("body = %q from %q, want the local member behind the offline remote", body, hints.Get(repo.HdrResolvedFrom))
		}
	})
}

// TestVirtualMemberFaultsPropagate: a classified NON-unfound member failure
// is not a miss — "only a true 404 continues" (FR-21-AC7). The SSRF chain's
// 400 and hardFail's 502 reach the client verbatim instead of being masked
// by a later member's copy.
func TestVirtualMemberFaultsPropagate(t *testing.T) {
	const path = "org/app/1.0/app-1.0.jar"

	t.Run("ssrf refusal", func(t *testing.T) {
		e := newEnv(t)
		// A remote member pointing at a loopback address WITHOUT the
		// exemption: the guarded dial refuses before any upstream traffic.
		mustCreateRemote(t, e, "rem-guarded", `{"url":"http://127.0.0.1:1/m2"}`)
		mustCreateRepo(t, e, "loc-has")
		put(t, e, admin(), "loc-has", path, "local-copy")
		if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
			RepoKey: "virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
			Config: `{"repositories":["rem-guarded","loc-has"]}`,
		}); err != nil {
			t.Fatalf("CreateRepo(virtual): %v", err)
		}
		_, _, _, err := getVirtual(t, &virtualFixture{e: e, vkey: "virt"}, path)
		var se *repo.StatusError
		if !errors.As(err, &se) || se.Code != http.StatusBadRequest {
			t.Fatalf("virtual Get over a guarded member = %v, want a 400 StatusError", err)
		}
		if !strings.Contains(se.Message, "private or suppressed upstream") {
			t.Fatalf("message = %q, want the SSRF wording", se.Message)
		}
	})

	t.Run("hardFail without a copy", func(t *testing.T) {
		e := newEnv(t)
		fx := buildVirtual(t, e, "virt", "", []memberSpec{
			{key: "rem-hard", remote: true, extra: `,"hardFail":true`},
			{key: "loc-has", files: map[string]string{path: "local-copy"}},
		})
		fx.upstreams["rem-hard"].Close()
		_, _, _, err := getVirtual(t, fx, path)
		var se *repo.StatusError
		if !errors.As(err, &se) || se.Code != http.StatusBadGateway {
			t.Fatalf("virtual Get over a hardFail member = %v, want a 502 StatusError", err)
		}
	})
}

// TestVirtualExploratoryMissLeavesNoNegativeRow: ADR-0013's cache-hygiene
// rule — a member probed through the virtual that comes up empty must leave
// the member's cache exactly as it found it (no negative row), while a
// DIRECT miss keeps writing one.
func TestVirtualExploratoryMissLeavesNoNegativeRow(t *testing.T) {
	const path = "org/app/1.0/app-1.0.jar"
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-miss", remote: true},
		{key: "loc-has", files: map[string]string{path: "local-copy"}},
	})

	if body, _ := mustGetVirtual(t, fx, path); body != "local-copy" {
		t.Fatalf("body = %q", body)
	}
	if _, err := e.md.Remote().GetCache(ctx, "rem-miss", path); !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		t.Fatalf("probe must leave no negative row, GetCache err = %v", err)
	}

	// Control: the same miss addressed DIRECTLY does remember itself.
	if _, _, err := e.svc.Get(ctx, admin(), "rem-miss", path); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("direct miss = %v", err)
	}
	if _, err := e.md.Remote().GetCache(ctx, "rem-miss", path); err != nil {
		t.Fatalf("direct miss must write the negative row: %v", err)
	}
	if got := fx.hits["rem-miss"].Load(); got != 2 {
		t.Fatalf("upstream hits = %d, want 2 (probe + direct)", got)
	}
}

// ---- member-change immediacy (AC2, M51's second half, AC9's second half) ----

// TestVirtualMemberChangesImmediateEffect: member reordering and priority
// (re)marking are visible to the very next read — the order is recomputed
// per request off the ledger, never cached.
func TestVirtualMemberChangesImmediateEffect(t *testing.T) {
	const path = "com/acme/lib/1.0/lib-1.0.jar"
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "loc-a", files: map[string]string{path: "A"}},
		{key: "loc-b", files: map[string]string{path: "B"}},
	})

	if body, hints := mustGetVirtual(t, fx, path); body != "A" || hints.Get(repo.HdrResolvedFrom) != "loc-a" {
		t.Fatalf("declaration-order body = %q from %q", body, hints.Get(repo.HdrResolvedFrom))
	}

	// Reorder: B first.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "virt", Config: `{"repositories":["loc-b","loc-a"]}`,
	}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	if body, hints := mustGetVirtual(t, fx, path); body != "B" || hints.Get(repo.HdrResolvedFrom) != "loc-b" {
		t.Fatalf("post-reorder body = %q from %q", body, hints.Get(repo.HdrResolvedFrom))
	}

	// Promote A with the priority mark while B stays first in declaration.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "loc-a", Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark loc-a: %v", err)
	}
	if body, hints := mustGetVirtual(t, fx, path); body != "A" || hints.Get(repo.HdrResolvedFrom) != "loc-a" {
		t.Fatalf("post-mark body = %q from %q, want the marked member to jump the buckets", body, hints.Get(repo.HdrResolvedFrom))
	}

	// Unmark: declaration order returns (AC9's "no marks" regression).
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "loc-a", Config: `{}`,
	}); err != nil {
		t.Fatalf("unmark loc-a: %v", err)
	}
	if body, hints := mustGetVirtual(t, fx, path); body != "B" || hints.Get(repo.HdrResolvedFrom) != "loc-b" {
		t.Fatalf("post-unmark body = %q from %q, want declaration order back", body, hints.Get(repo.HdrResolvedFrom))
	}
}

// ---- the read gate ----

// TestVirtualReadGatePrecedesMemberContact: an unauthorized read on the
// virtual never reaches a member — and never the upstream (the same
// posture T-66 pinned for remote repositories).
func TestVirtualReadGatePrecedesMemberContact(t *testing.T) {
	const path = "org/app/1.0/app-1.0.jar"
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "rem-b", remote: true, files: map[string]string{"/" + path: "upstream"}},
	})
	// Anonymous with a fail-closed authorizer: challenged, zero upstream
	// traffic.
	if _, _, err := e.svc.Get(context.Background(), nil, "virt", path); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous virtual Get = %v, want ErrUnauthorized", err)
	}
	if got := fx.hits["rem-b"].Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0 (the gate precedes member contact)", got)
	}
	// An authenticated principal without the grant is denied, not challenged.
	if _, _, err := e.svc.Get(context.Background(), alice(), "virt", path); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted virtual Get = %v, want ErrForbidden", err)
	}
	if got := fx.hits["rem-b"].Load(); got != 0 {
		t.Fatalf("upstream hits after the denied read = %d, want 0", got)
	}
}

// ---- the write plane (M52/M53) ----

// msg405Of is the C5 wording (PRD v1.1 section 5.5 C5 / repo-semantics
// section 8.2), restated for equality assertions.
func msg405Of(key string) string {
	return "No local repository was configured as local deployment repository for the (" + key + ") virtual repository."
}

// TestVirtualWriteUnrouted405: PUT-family and DELETE on a virtual without a
// defaultDeploymentRepo answer the C5 405 with the exact errata wording.
func TestVirtualWriteUnrouted405(t *testing.T) {
	const path = "com/acme/x/1.0.0/x-1.0.0.jar"
	ctx := context.Background()
	e := newEnv(t)
	buildVirtual(t, e, "virt", "", []memberSpec{{key: "loc-a"}})

	check := func(what string, err error) {
		t.Helper()
		var se *repo.StatusError
		if !errors.As(err, &se) {
			t.Fatalf("%s = %v, want a StatusError", what, err)
		}
		if se.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 405", what, se.Code)
		}
		if got := se.Header.Get("Allow"); got != http.MethodGet {
			t.Fatalf("%s Allow = %q, want GET", what, got)
		}
		if se.Message != msg405Of("virt") {
			t.Fatalf("%s message = %q, want the C5 wording %q", what, se.Message, msg405Of("virt"))
		}
		if !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Fatalf("%s must wrap ErrRepoTypeNotSupported, got %v", what, err)
		}
	}
	_, err := e.svc.Put(ctx, admin(), "virt", path, strings.NewReader("x"), storage.BlobRef{}, "")
	check("Put", err)
	_, err = e.svc.PutFromBlob(ctx, admin(), "virt", path, storage.BlobRef{Sha256: shaOf("x")}, "")
	check("PutFromBlob", err)
	_, err = e.svc.PutLandedBlob(ctx, admin(), "virt", path, storage.BlobRef{Sha256: shaOf("x")}, "")
	check("PutLandedBlob", err)
	check("Delete", e.svc.Delete(ctx, admin(), "virt", path))

	// A raw-seeded virtual row (adapter harnesses write `{}` configs
	// directly into the store) answers the same 405, not a parse failure.
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "virt-raw", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric, Config: "{}",
	}); err != nil {
		t.Fatalf("seed raw virtual: %v", err)
	}
	_, err = e.svc.Put(ctx, admin(), "virt-raw", path, strings.NewReader("x"), storage.BlobRef{}, "")
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusMethodNotAllowed || se.Message != msg405Of("virt-raw") {
		t.Fatalf("raw-seeded virtual Put = %v, want the C5 405", err)
	}
}

// TestVirtualWriteRoutedToDeploymentRepo: with a defaultDeploymentRepo the
// write lands in the TARGET member under the target's own semantics —
// permissions, the overwrite pair, the checksum chain — and is visible to
// virtual reads immediately (M53).
func TestVirtualWriteRoutedToDeploymentRepo(t *testing.T) {
	const path = "com/acme/x/1.0.0/x-1.0.0.jar"
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "loc-target", []memberSpec{
		{key: "loc-target"}, {key: "rem-other", remote: true},
	})

	// Admin deploys through the virtual; the node lands in the member.
	n, err := e.svc.Put(ctx, admin(), "virt", path, strings.NewReader("deployed"), storage.BlobRef{}, "")
	if err != nil {
		t.Fatalf("routed Put: %v", err)
	}
	if n.RepoKey != "loc-target" {
		t.Fatalf("node landed in %q, want the deployment member", n.RepoKey)
	}
	// M53: the artifact is addressable BOTH ways, immediately.
	if body, hints := mustGetVirtual(t, fx, path); body != "deployed" || hints.Get(repo.HdrResolvedFrom) != "loc-target" {
		t.Fatalf("virtual read of the routed deploy = %q from %q", body, hints.Get(repo.HdrResolvedFrom))
	}
	rc, _, err := e.svc.Get(ctx, admin(), "loc-target", path)
	if err != nil {
		t.Fatalf("member read of the routed deploy: %v", err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close() //nolint:errcheck // read-only fd
	if string(b) != "deployed" {
		t.Fatalf("member body = %q", string(b))
	}

	// The permission pair follows the TARGET repository: a write grant
	// deploys through the virtual; overwriting a different checksum
	// additionally needs delete (repo-semantics section 3); no grant at all
	// is a 403 before the body matters.
	e.az.add("alice", repo.ActionWrite, "")
	if _, err := e.svc.Put(ctx, alice(), "virt", "by-alice.bin", strings.NewReader("by-alice"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("granted routed Put: %v", err)
	}
	if _, err := e.svc.Put(ctx, alice(), "virt", "by-alice.bin", strings.NewReader("other"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("overwrite without delete must be forbidden: %v", err)
	}
	if _, err := e.svc.Put(ctx, alice(), "virt", path, strings.NewReader("x"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("overwriting the admin deploy without delete must be forbidden: %v", err)
	}
	bob := &repo.Principal{Name: "bob"}
	if _, err := e.svc.Put(ctx, bob, "virt", "bob.bin", strings.NewReader("x"), storage.BlobRef{}, ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted routed Put = %v, want ErrForbidden", err)
	}

	// The checksum-deploy variant routes identically (the blob of the
	// admin deploy is already committed).
	if _, err := e.svc.PutFromBlob(ctx, admin(), "virt", "moved.bin", storage.BlobRef{Sha256: shaOf("deployed")}, ""); err != nil {
		t.Fatalf("routed PutFromBlob: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "loc-target", "moved.bin"); err != nil {
		t.Fatalf("checksum deploy must land in the member: %v", err)
	}

	// The landed-blob variant routes identically (the docker/chunked path).
	if _, err := e.svc.PutLandedBlob(ctx, admin(), "virt", "landed.bin", storage.BlobRef{Sha256: shaOf("by-alice")}, ""); err != nil {
		t.Fatalf("routed PutLandedBlob: %v", err)
	}
	if _, err := e.md.Nodes().Get(ctx, "loc-target", "landed.bin"); err != nil {
		t.Fatalf("landed blob must land in the member: %v", err)
	}

	// A folder deploy through the virtual lands the folder marker in the
	// member, not in the virtual's (nonexistent) namespace.
	if _, err := e.svc.Put(ctx, admin(), "virt", "com/acme/y/", strings.NewReader(""), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("routed folder Put: %v", err)
	}
	fn, err := e.md.Nodes().Get(ctx, "loc-target", "com/acme/y/")
	if err != nil {
		t.Fatalf("folder marker must land in the member: %v", err)
	}
	if fn.Sha256 != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("folder marker sha = %q", fn.Sha256)
	}

	// Removing the route flips the very next write back to the 405.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "virt", Config: `{"repositories":["loc-target","rem-other"]}`,
	}); err != nil {
		t.Fatalf("route removal: %v", err)
	}
	_, err = e.svc.Put(ctx, admin(), "virt", path, strings.NewReader("x"), storage.BlobRef{}, "")
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post-removal Put = %v, want 405", err)
	}
}

// TestVirtualDeleteNeverPropagates: DELETE on a virtual is the 405 whether
// or not a write route is configured — BinFlow's deliberate incompatibility
// (RE-08); a routed repository gets the truthful wording instead of
// claiming no deployment repository is configured.
func TestVirtualDeleteNeverPropagates(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	buildVirtual(t, e, "virt", "loc-target", []memberSpec{{key: "loc-target"}})
	put(t, e, admin(), "loc-target", "a.bin", "content")

	err := e.svc.Delete(ctx, admin(), "virt", "a.bin")
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("routed virtual Delete = %v, want 405", err)
	}
	if got := se.Header.Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}
	if strings.Contains(se.Message, "No local repository was configured") || !strings.Contains(se.Message, "not propagated") {
		t.Fatalf("routed Delete message = %q, want the not-propagated wording", se.Message)
	}
	// The member's artifact is untouched.
	if _, err := e.md.Nodes().Get(ctx, "loc-target", "a.bin"); err != nil {
		t.Fatalf("member node must survive the virtual delete refusal: %v", err)
	}
}

// TestVirtualWriteRouteTargetDrift: a write route naming a repository that
// no longer exists surfaces the honest not-found, not a silent landing.
func TestVirtualWriteRouteTargetDrift(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "loc-gone", []memberSpec{{key: "loc-gone"}})
	if err := e.svc.DeleteRepo(ctx, admin(), "loc-gone", true); err != nil {
		t.Fatalf("delete the target member: %v", err)
	}
	_, err := e.svc.Put(ctx, admin(), "virt", "a.bin", strings.NewReader("x"), storage.BlobRef{}, "")
	if !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("drifted route Put = %v, want ErrRepoNotFound", err)
	}
	// Reads keep working: the ledger row cascaded away with the member, so
	// the virtual is simply empty now.
	if _, _, err := e.svc.Get(ctx, admin(), fx.vkey, "a.bin"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("post-drift virtual Get = %v, want ErrNodeNotFound", err)
	}
}

// ---- pure helpers of the ordering/routing model ----

// TestMemberPriorityResolutionProbe: the mark probe's contract over both
// canonical spellings plus the degraded shapes (via the in-package seam).
func TestMemberPriorityResolutionProbe(t *testing.T) {
	tests := []struct {
		config string
		want   bool
	}{
		{`{"priorityResolution":true}`, true},
		{`{"priorityResolution":false}`, false},
		{`{}`, false},
		{``, false},
		{`{"url":"http://u","priorityResolution":true,"hardFail":true}`, true},
		{`{"priorityResolution":"yes"}`, false}, // hand-mangled: the safe default
		{`not json`, false},
	}
	for _, tt := range tests {
		if got := repo.MemberPriorityResolutionForTest(tt.config); got != tt.want {
			t.Errorf("memberPriorityResolution(%q) = %v, want %v", tt.config, got, tt.want)
		}
	}
}

// TestVirtualRouteTargetAliases: the tolerant route reader accepts the three
// Artifactory spellings a raw-seeded row may carry.
func TestVirtualRouteTargetAliases(t *testing.T) {
	tests := []struct {
		config string
		want   string
	}{
		{`{"defaultDeploymentRepo":"maven-local"}`, "maven-local"},
		{`{"defaultDeploymentRepoRef":"maven-local"}`, "maven-local"},
		{`{"deploymentRepository":"maven-local"}`, "maven-local"},
		{`{"repositories":["a"],"defaultDeploymentRepo":"a"}`, "a"},
		{`{"repositories":["a"]}`, ""},
		{`{}`, ""},
		{``, ""},
		{`not json`, ""},
	}
	for _, tt := range tests {
		if got := repo.VirtualRouteTargetForTest(tt.config); got != tt.want {
			t.Errorf("route target of %s = %q, want %q", tt.config, got, tt.want)
		}
	}
}
