package repo_test

// T-448 (FR-147.2) — the remote-browsing optional档's service wiring: the
// config gate, the off-diff=0 default, the listing merge (direct and, per
// repo-semantics §8.5's expanded口径, virtual), the folder face for
// upstream-only directories, the upstream-fault degradation, the
// click-through pull+count linkage and the zero-leak probes. Upstream
// fixtures are loopback helm classic repositories (one index.yaml = the
// whole tree — the strongest batch-1 shape), served flippable so the
// degradation leg can die mid-test without tearing the server down.

import (
	"context"
	"encoding/json"
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
)

// t448Index is the upstream index.yaml: chart roots at several depths plus
// one path (shared/from-remote-1.0.0.tgz) the local member also carries —
// the §8.1 first-member-wins probe of the virtual leg.
const t448Index = `apiVersion: v1
entries:
  acs-engine:
  - name: acs-engine
    version: "1.0.0"
    urls:
    - charts/acs-engine-1.0.0.tgz
  solo:
  - name: solo
    version: "0.2.0"
    urls:
    - solo-0.2.0.tgz
  deep:
  - name: deep
    version: "1.0.0"
    urls:
    - deep/nested/deep-1.0.0.tgz
  shared:
  - name: shared
    version: "1.0.0"
    urls:
    - shared/from-remote-1.0.0.tgz
`

// t448Upstream is a counting, flippable helm upstream: index.yaml at the
// root, a deterministic body for every .tgz path, 404 for anything else,
// and a broken switch that answers 500 to everything (the degradation leg).
type t448Upstream struct {
	srv    *httptest.Server
	hits   *atomic.Int64
	broken *atomic.Bool
}

func newT448Upstream(t *testing.T) *t448Upstream {
	t.Helper()
	u := &t448Upstream{hits: &atomic.Int64{}, broken: &atomic.Bool{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		if u.broken.Load() {
			http.Error(w, "upstream maintenance", http.StatusInternalServerError)
			return
		}
		switch {
		case r.URL.Path == "/index.yaml":
			_, _ = w.Write([]byte(t448Index))
		case strings.HasSuffix(r.URL.Path, ".tgz"):
			_, _ = w.Write([]byte("body:" + strings.TrimPrefix(r.URL.Path, "/")))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// bodyOf is the deterministic chart body a path serves.
func (u *t448Upstream) bodyOf(path string) string { return "body:" + path }

// t448Env is an env with the batch-1 package types unlocked (helm/deb/rpm
// are registry-known slots — the static enum alone knows the five core
// types, the same gate posture every addon-type test rides).
func t448Env(t *testing.T) *env {
	t.Helper()
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"helm":   unlockedGo,
		"debian": unlockedGo,
		"rpm":    unlockedGo,
	})
	return e
}

// t448Create creates one repository or fails the test.
func t448Create(t *testing.T, e *env, key, rclass, packageType, config string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: packageType, Config: config,
	}); err != nil {
		t.Fatalf("CreateRepo(%s): %v", key, err)
	}
}

// t448RemoteCfg builds a remote config body for the upstream, with or
// without the optional档.
func t448RemoteCfg(base string, flagOn bool) string {
	cfg := `{"url":"` + base + `","allowPrivateUpstream":true`
	if flagOn {
		cfg += `,"listRemoteFolderItems":true`
	}
	return cfg + `}`
}

// t448List lists and flattens the paths.
func t448List(t *testing.T, e *env, p *repo.Principal, repoKey, prefix string) []string {
	t.Helper()
	nodes, err := e.svc.List(context.Background(), p, repoKey, prefix)
	if err != nil {
		t.Fatalf("List(%s, %q): %v", repoKey, prefix, err)
	}
	return t448Paths(nodes)
}

// t448Paths flattens one listing into paths.
func t448Paths(nodes []*metadata.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Path)
	}
	return out
}

// t448NodeCount counts the repository's stored node rows (the zero-落库
// probe's before/after reading).
func t448NodeCount(t *testing.T, e *env, repoKey string) int {
	t.Helper()
	nodes, err := e.md.Nodes().ListByPrefix(context.Background(), repoKey, "")
	if err != nil {
		t.Fatalf("count nodes %s: %v", repoKey, err)
	}
	return len(nodes)
}

// t448Find locates one row in a listing.
func t448Find(nodes []*metadata.Node, path string) *metadata.Node {
	for _, n := range nodes {
		if n.Path == path {
			return n
		}
	}
	return nil
}

// t448WithRemote resolves the RemoteBrowsePlane capability (the optional
// SPI segment every service assembled through repo.New carries) and runs
// one enriched listing.
func t448WithRemote(t *testing.T, e *env, p *repo.Principal, repoKey, prefix string) *repo.RemoteBrowseListing {
	t.Helper()
	plane, ok := e.svc.(repo.RemoteBrowsePlane)
	if !ok {
		t.Fatalf("service does not carry the RemoteBrowsePlane seam")
	}
	listing, err := plane.ListWithRemote(context.Background(), p, repoKey, prefix)
	if err != nil {
		t.Fatalf("ListWithRemote(%s, %q): %v", repoKey, prefix, err)
	}
	return listing
}

// ---- AC1's config half: the flag's acceptance domain and canonical echo ----

// TestT448ListRemoteFolderItemsConfigGate pins the optional档's config face:
// accepted on the batch-1 types, refused BY NAME everywhere else (the
// chartsBaseUrl posture — an inert accepted field is the trap), explicit
// false legal on every type, and the canonical echo always carrying the
// boolean (the hardFail/enableTokenAuthentication echo posture).
func TestT448ListRemoteFolderItemsConfigGate(t *testing.T) {
	tests := []struct {
		name        string
		packageType string
		config      string
		wantRefusal string
	}{
		{"helm accepts true", "helm", `{"url":"https://up.example/helm","listRemoteFolderItems":true}`, ""},
		{"debian accepts true", "debian", `{"url":"https://up.example/deb","listRemoteFolderItems":true}`, ""},
		{"rpm accepts true", "rpm", `{"url":"https://up.example/rpm","listRemoteFolderItems":true}`, ""},
		{"generic refuses true", "generic", `{"url":"https://up.example/gen","listRemoteFolderItems":true}`, "listRemoteFolderItems"},
		{"docker refuses true", "docker", `{"url":"https://up.example/v2","listRemoteFolderItems":true}`, "listRemoteFolderItems"},
		{"npm refuses true", "npm", `{"url":"https://up.example/npm","listRemoteFolderItems":true}`, "listRemoteFolderItems"},
		{"generic accepts explicit false", "generic", `{"url":"https://up.example/gen","listRemoteFolderItems":false}`, ""},
		{"helm absent keeps the default", "helm", `{"url":"https://up.example/helm"}`, ""},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := t448Env(t)
			key := "t448-cfg-" + string(rune('a'+i))
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeRemote, PackageType: tc.packageType, Config: tc.config,
			})
			if tc.wantRefusal == "" {
				if err != nil {
					t.Fatalf("create refused: %v", err)
				}
				return
			}
			if err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), tc.wantRefusal) {
				t.Fatalf("err = %v, want ErrInvalidRepoConfig naming %q", err, tc.wantRefusal)
			}
		})
	}

	// The canonical echo carries the boolean both ways, and the update path
	// flips it (the update arm runs the same parse).
	e := t448Env(t)
	created, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t448-echo", Type: repo.TypeRemote, PackageType: "helm",
		Config: `{"url":"https://up.example/helm"}`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if flag := t448EchoFlag(t, e, "t448-echo"); flag {
		t.Fatalf("absent flag echoed true")
	}
	upd := *created
	upd.Config = `{"url":"https://up.example/helm","listRemoteFolderItems":true}`
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &upd); err != nil {
		t.Fatalf("update with the flag on: %v", err)
	}
	if flag := t448EchoFlag(t, e, "t448-echo"); !flag {
		t.Fatalf("update did not turn the flag on")
	}
}

// t448EchoFlag reads the canonical echo's flag.
func t448EchoFlag(t *testing.T, e *env, key string) bool {
	t.Helper()
	row, err := e.svc.GetRepo(context.Background(), admin(), key)
	if err != nil {
		t.Fatalf("GetRepo(%s): %v", key, err)
	}
	var cfg struct {
		ListRemoteFolderItems bool `json:"listRemoteFolderItems"`
	}
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		t.Fatalf("echo config decode: %v (body %s)", err, row.Config)
	}
	return cfg.ListRemoteFolderItems
}

// ---- AC1's behavior half: off = diff zero, on = the merged tree ----

// TestT448FlagOffListingStaysCacheOnly: without the flag the listing is the
// T-406 cache face — no derived rows, and ZERO upstream contact from the
// browse face (the engine is never called; the off posture is structural).
func TestT448FlagOffListingStaysCacheOnly(t *testing.T) {
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-off", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, false))

	if got := t448List(t, e, admin(), "t448-off", ""); len(got) != 0 {
		t.Fatalf("empty-cache listing = %v, want empty", got)
	}
	if got := up.hits.Load(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0 (no browse contact without the flag)", got)
	}

	// One pull-through landing: the listing then shows exactly the cache.
	if _, _, err := e.svc.Get(context.Background(), admin(), "t448-off", "solo-0.2.0.tgz"); err != nil {
		t.Fatalf("pull solo: %v", err)
	}
	if got := t448List(t, e, admin(), "t448-off", ""); len(got) != 1 || got[0] != "solo-0.2.0.tgz" {
		t.Fatalf("cached listing = %v, want [solo-0.2.0.tgz]", got)
	}
	if got := up.hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1 (the pull only — the listing added none)", got)
	}
	listing := t448WithRemote(t, e, admin(), "t448-off", "")
	if listing.RemoteDegraded != "" {
		t.Fatalf("flag-off note = %q, want empty", listing.RemoteDegraded)
	}
}

// TestT448FlagOnListingMergesUpstreamDisplayRows: with the flag on the tree
// carries the upstream's uncached paths beside the cache rows — display-only
// (zero 落库, a derived file row has no digest), cached rows first on a
// shared path, one index fetch per metadata-TTL window, and the rows sorted.
func TestT448FlagOnListingMergesUpstreamDisplayRows(t *testing.T) {
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-on", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, true))

	// A cached chart beside the uncached ones.
	if _, _, err := e.svc.Get(context.Background(), admin(), "t448-on", "charts/acs-engine-1.0.0.tgz"); err != nil {
		t.Fatalf("pull acs-engine: %v", err)
	}
	before := t448NodeCount(t, e, "t448-on")

	nodes, err := e.svc.List(context.Background(), admin(), "t448-on", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{
		"charts/", "charts/acs-engine-1.0.0.tgz",
		"deep/", "index.yaml", "shared/", "solo-0.2.0.tgz",
	}
	if got := t448Paths(nodes); !equalStrings(got, want) {
		t.Fatalf("merged listing = %v, want %v", got, want)
	}
	// Cached-first: the pulled chart's row is the CACHE row (real digest);
	// derived file rows are digest-less by construction.
	if n := t448Find(nodes, "charts/acs-engine-1.0.0.tgz"); n.Sha256 != shaOf(up.bodyOf("charts/acs-engine-1.0.0.tgz")) {
		t.Fatalf("cached row lost its digest: %+v", n)
	}
	for _, p := range []string{"index.yaml", "solo-0.2.0.tgz"} {
		if n := t448Find(nodes, p); n == nil || n.Sha256 != "" {
			t.Fatalf("derived row %s is not display-only: %+v", p, n)
		}
	}
	if n := t448Find(nodes, "deep/"); n == nil || n.Sha256 != metadata.FolderMarkerSHA {
		t.Fatalf("derived folder row malformed: %+v", n)
	}
	// Zero 落库 and one index fetch per window.
	if after := t448NodeCount(t, e, "t448-on"); after != before {
		t.Fatalf("node rows %d -> %d: derived rows must never land", before, after)
	}
	if got := up.hits.Load(); got != 2 { // the pull + one index.yaml
		t.Fatalf("upstream hits = %d, want 2", got)
	}
	if _, err := e.svc.List(context.Background(), admin(), "t448-on", ""); err != nil {
		t.Fatalf("second List: %v", err)
	}
	if got := up.hits.Load(); got != 2 {
		t.Fatalf("upstream hits after re-list = %d, want 2 (snapshot window)", got)
	}
	// A subfolder listing narrows to that prefix's rows.
	if got := t448List(t, e, admin(), "t448-on", "deep"); !equalStrings(got, []string{"deep/", "deep/nested/"}) {
		t.Fatalf("deep listing = %v, want [deep/ deep/nested/]", got)
	}
}

// ---- AC2: degradation keeps the cached rows; the layer speaks ----

// TestT448UpstreamFaultDegradesLayerAndKeepsCachedRows: an upstream fault
// never fails the listing — the cached rows stay and the note rides the
// RemoteBrowseListing; the assumed-offline silence bounds contact to one
// probe per window (§4-1/§4-2).
func TestT448UpstreamFaultDegradesLayerAndKeepsCachedRows(t *testing.T) {
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-deg", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, true))
	if _, _, err := e.svc.Get(context.Background(), admin(), "t448-deg", "charts/acs-engine-1.0.0.tgz"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	listing := t448WithRemote(t, e, admin(), "t448-deg", "")
	if listing.RemoteDegraded != "" {
		t.Fatalf("healthy listing note = %q, want empty", listing.RemoteDegraded)
	}

	// The upstream dies and the snapshot ages out of its metadata TTL.
	up.broken.Store(true)
	e.clk.Advance(601 * time.Second)
	listing = t448WithRemote(t, e, admin(), "t448-deg", "")
	if listing.RemoteDegraded == "" {
		t.Fatalf("degraded listing carried no note")
	}
	if got := t448Paths(listing.Nodes); !equalStrings(got, []string{"charts/acs-engine-1.0.0.tgz"}) {
		t.Fatalf("degraded listing = %v, want the cached rows only", got)
	}
	if got := up.hits.Load(); got != 3 { // pull + healthy index + one failed probe
		t.Fatalf("upstream hits = %d, want 3", got)
	}
	// Inside the assumed-offline silence: the note stays, contact does not.
	listing = t448WithRemote(t, e, admin(), "t448-deg", "")
	if listing.RemoteDegraded == "" {
		t.Fatalf("silence-window listing carried no note")
	}
	if got := up.hits.Load(); got != 3 {
		t.Fatalf("upstream hits inside the silence = %d, want 3", got)
	}
	// Past the silence the layer probes exactly once more (and fails).
	e.clk.Advance(301 * time.Second)
	if _, err := e.svc.List(context.Background(), admin(), "t448-deg", ""); err != nil {
		t.Fatalf("plain List during degradation: %v", err)
	}
	if got := up.hits.Load(); got != 4 {
		t.Fatalf("upstream hits after the silence = %d, want 4", got)
	}
}

// ---- AC2: the folder face and the click-through linkage ----

// TestT448UncachedDerivedFolderResolvesOnFolderFace: a folder that exists
// only upstream answers ErrIsFolder off the enumeration snapshot (zero
// writes — the ADR-0013 posture), so the tree can expand uncached remote
// directories; with the flag off the same probe keeps falling through to
// the engine's honest miss.
func TestT448UncachedDerivedFolderResolvesOnFolderFace(t *testing.T) {
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-on", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, true))
	t448Create(t, e, "t448-off", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, false))
	before := t448NodeCount(t, e, "t448-on")

	rc, node, err := e.svc.Get(context.Background(), admin(), "t448-on", "deep/")
	if !errors.Is(err, repo.ErrIsFolder) || rc != nil || node == nil {
		t.Fatalf("Get(deep/) = (%v, %+v, %v), want ErrIsFolder + display marker", rc, node, err)
	}
	if node.Path != "deep/" || node.Sha256 != metadata.FolderMarkerSHA {
		t.Fatalf("display folder marker malformed: %+v", node)
	}
	if after := t448NodeCount(t, e, "t448-on"); after != before {
		t.Fatalf("node rows %d -> %d: the folder face must write nothing", before, after)
	}
	// An upstream file path is NOT a folder (the parent enumeration finds no
	// folder entry for it) — it keeps flowing to the engine, which serves it.
	if _, _, err := e.svc.Get(context.Background(), admin(), "t448-off", "deep/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("flag-off Get(deep/) = %v, want the engine's miss", err)
	}
	if _, _, err := e.svc.Get(context.Background(), admin(), "t448-on", "deep/"); !errors.Is(err, repo.ErrIsFolder) {
		t.Fatalf("flag-on Get(deep/) repeat = %v, want ErrIsFolder", err)
	}
}

// TestT448ClickThroughPullsAndCounts: clicking a derived file row is an
// ordinary pull-through — the artifact lands, and the T-438 single-source
// count pair lands with the audit row (arm 3, remote-serving: both columns).
func TestT448ClickThroughPullsAndCounts(t *testing.T) {
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-click", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, true))

	rc, node, err := e.svc.Get(context.Background(), admin(), "t448-click", "solo-0.2.0.tgz")
	if err != nil {
		t.Fatalf("click-through Get: %v", err)
	}
	body, rerr := io.ReadAll(rc)
	if rerr != nil {
		t.Fatalf("read body: %v", rerr)
	}
	_ = rc.Close() //nolint:errcheck // read-only fd
	if string(body) != up.bodyOf("solo-0.2.0.tgz") {
		t.Fatalf("body = %q", string(body))
	}
	if node.Sha256 != shaOf(up.bodyOf("solo-0.2.0.tgz")) {
		t.Fatalf("landed node digest = %s", node.Sha256)
	}
	st, err := e.md.Nodes().Stats(context.Background(), "t448-click", "solo-0.2.0.tgz")
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if st.DownloadCount != 1 || st.RemoteDownloadCount != 1 || st.LastDownloadedBy != "admin" {
		t.Fatalf("counts = %+v, want the remote-serving arm's 1/1 by admin", st)
	}
	var sawAudit bool
	for _, ev := range e.au.events {
		if ev.Action == repo.AuditActionDownload && ev.Repo == "t448-click" && ev.Path == "solo-0.2.0.tgz" {
			sawAudit = true
		}
	}
	if !sawAudit {
		t.Fatalf("no download audit row for the click-through")
	}
}

// ---- §8.5: the virtual tree's expanded口径 ----

// t448VirtualEnv builds the §8.5 shape: a helm virtual over a local member
// and a flag-on helm remote member whose upstream also lists the path the
// local member carries (the first-member-wins probe).
func t448VirtualEnv(t *testing.T) (*env, *t448Upstream) {
	t.Helper()
	up := newT448Upstream(t)
	e := t448Env(t)
	t448Create(t, e, "t448-local", repo.TypeLocal, "helm", `{}`)
	put(t, e, admin(), "t448-local", "shared/from-remote-1.0.0.tgz", "body:local-copy")
	t448Create(t, e, "t448-remote", repo.TypeRemote, "helm", t448RemoteCfg(up.srv.URL, true))
	t448Create(t, e, "t448-virtual", repo.TypeVirtual, "helm", `{"repositories":["t448-local","t448-remote"]}`)
	return e, up
}

// TestT448VirtualListIncludesRemoteDerivedRows: a flag-on remote member
// contributes its upstream-derived rows to the virtual tree at the member's
// own position (repo-semantics §8.5's expanded口径 — the T-412 "cache rows
// only" reading was the all-flag-off default), first member still winning a
// shared path.
func TestT448VirtualListIncludesRemoteDerivedRows(t *testing.T) {
	e, up := t448VirtualEnv(t)

	nodes, err := e.svc.List(context.Background(), admin(), "t448-virtual", "")
	if err != nil {
		t.Fatalf("List(virtual): %v", err)
	}
	want := []string{
		"charts/", "deep/", "index.yaml", "shared/", "shared/from-remote-1.0.0.tgz", "solo-0.2.0.tgz",
	}
	if got := t448Paths(nodes); !equalStrings(got, want) {
		t.Fatalf("virtual listing = %v, want %v", got, want)
	}
	// §8.1 order: the path BOTH members carry answers the FIRST member's
	// cached row — the local copy, not the remote's derived spelling.
	if n := t448Find(nodes, "shared/from-remote-1.0.0.tgz"); n.RepoKey != "t448-local" || n.Sha256 != shaOf("body:local-copy") {
		t.Fatalf("shared path owner = %+v, want the local member's cached row", n)
	}
	// Derived rows keep the MEMBER's key (the storage-plane truth).
	if n := t448Find(nodes, "index.yaml"); n.RepoKey != "t448-remote" {
		t.Fatalf("derived row carries %q, want the remote member key", n.RepoKey)
	}
	// One enumeration per window across the whole virtual face.
	if got := up.hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
}

// TestT448VirtualFolderFaceResolvesDerivedFolder: the virtual folder face
// answers an upstream-only directory of a flag-on remote member with the
// same display-only synthesized marker the cached-children arm uses —
// writing nothing into any member's namespace.
func TestT448VirtualFolderFaceResolvesDerivedFolder(t *testing.T) {
	e, _ := t448VirtualEnv(t)
	beforeLocal, beforeRemote := t448NodeCount(t, e, "t448-local"), t448NodeCount(t, e, "t448-remote")

	rc, node, err := e.svc.Get(context.Background(), admin(), "t448-virtual", "deep/")
	if !errors.Is(err, repo.ErrIsFolder) || rc != nil || node == nil {
		t.Fatalf("Get(virtual deep/) = (%v, %+v, %v), want ErrIsFolder + marker", rc, node, err)
	}
	if node.RepoKey != "t448-virtual" || node.Path != "deep/" || node.Sha256 != metadata.FolderMarkerSHA {
		t.Fatalf("virtual folder marker malformed: %+v", node)
	}
	if t448NodeCount(t, e, "t448-local") != beforeLocal || t448NodeCount(t, e, "t448-remote") != beforeRemote {
		t.Fatalf("virtual folder probe wrote into a member's namespace")
	}
}

// ---- AC3: the zero-leak probes ----

// TestT448DerivedRowsLeakZeroWithoutRead: a caller the MEMBER never
// authorized learns nothing about its upstream — zero derived rows in the
// virtual tree (the member's cache rows keep the T-412 posture: the
// virtual's own gate covers them), zero folder resolutions, and zero
// upstream contact; on the repository's own face the listing gate refuses
// before anything is consulted.
func TestT448DerivedRowsLeakZeroWithoutRead(t *testing.T) {
	e, up := t448VirtualEnv(t)
	// alice may read the virtual, nothing else.
	e.az.addOnRepo("alice", repo.ActionRead, "t448-virtual", "")
	// Warm the snapshot as admin so the probe measures LEAKAGE, not laziness.
	if _, err := e.svc.List(context.Background(), admin(), "t448-virtual", ""); err != nil {
		t.Fatalf("admin warm-up list: %v", err)
	}
	hitsAfterWarm := up.hits.Load()

	nodes, err := e.svc.List(context.Background(), alice(), "t448-virtual", "")
	if err != nil {
		t.Fatalf("alice List(virtual): %v", err)
	}
	for _, banned := range []string{"index.yaml", "solo-0.2.0.tgz", "charts/", "deep/"} {
		if t448Find(nodes, banned) != nil {
			t.Fatalf("unauthorized caller saw the derived row %q", banned)
		}
	}
	// The members' cached rows keep the aggregate's own gate (T-412) — the
	// local member's marker and artifact, and the folder row is the LOCAL
	// member's, never the remote member's derived spelling.
	for _, cached := range []string{"shared/", "shared/from-remote-1.0.0.tgz"} {
		if n := t448Find(nodes, cached); n == nil || n.RepoKey != "t448-local" {
			t.Fatalf("cached row %s missing or foreign: %+v", cached, n)
		}
	}
	// The folder face claims nothing the gate would not show.
	if _, _, err := e.svc.Get(context.Background(), alice(), "t448-virtual", "deep/"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("alice Get(virtual deep/) = %v, want ErrNodeNotFound", err)
	}
	if got := up.hits.Load(); got != hitsAfterWarm {
		t.Fatalf("upstream hits %d -> %d: a denied caller must cost zero contact", hitsAfterWarm, got)
	}
	// The repository's own face refuses at the listing gate.
	if _, err := e.svc.List(context.Background(), alice(), "t448-remote", ""); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("alice List(remote) = %v, want ErrForbidden", err)
	}
	if got := up.hits.Load(); got != hitsAfterWarm {
		t.Fatalf("upstream hits moved on the refused face: %d", got)
	}
}

// equalStrings is slice equality.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
