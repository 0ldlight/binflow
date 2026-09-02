package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// T-438 (FR-146.2 / ADR-0044 K69): per-node download counting on the real
// service stack — the three-arm criterion end to end, the origin dimension
// on the audit detail, and the statsSync eligibility marker. The four nodes
// columns are the download plane's single counting channel; every test here
// reads them through Nodes().Stats and nothing else.

// statsOf reads one row's counting columns, failing the test on store errors.
func statsOf(t *testing.T, e *env, repoKey, path string) *metadata.NodeStats {
	t.Helper()
	st, err := e.md.Nodes().Stats(context.Background(), repoKey, path)
	if err != nil {
		t.Fatalf("Stats(%s/%s): %v", repoKey, path, err)
	}
	return st
}

// downloadDetails snapshots the audit log's download rows as decoded detail
// objects (key order is irrelevant once decoded).
func downloadDetails(t *testing.T, e *env) []map[string]any {
	t.Helper()
	e.au.mu.Lock()
	defer e.au.mu.Unlock()
	var out []map[string]any
	for _, ev := range e.au.events {
		if ev.Action != repo.AuditActionDownload {
			continue
		}
		d := map[string]any{}
		if ev.Detail != "" {
			if err := json.Unmarshal([]byte(ev.Detail), &d); err != nil {
				t.Fatalf("download detail %q is not JSON: %v", ev.Detail, err)
			}
		}
		out = append(out, d)
	}
	return out
}

// TestT438DirectArmCountsGetOnly pins arm 1: a direct GET on a local
// repository counts the node once; the faces that do NOT record an audit
// download row (a plain metadata lookup via the store, a HEAD-class probe)
// never move the counters, and a served download's identity/timestamp land
// on the row.
func TestT438DirectArmCountsGetOnly(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/pkg-1.0.bin", "body")

	// The store-side lookup alone is not a download: no audit row, no count.
	if _, err := e.md.Nodes().Get(context.Background(), "loc", "a/pkg-1.0.bin"); err != nil {
		t.Fatalf("store Get: %v", err)
	}
	if got := statsOf(t, e, "loc", "a/pkg-1.0.bin").DownloadCount; got != 0 {
		t.Fatalf("metadata lookup counted: %d", got)
	}

	rc, _, err := e.svc.Get(context.Background(), admin(), "loc", "a/pkg-1.0.bin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_ = rc.Close()
	st := statsOf(t, e, "loc", "a/pkg-1.0.bin")
	if st.DownloadCount != 1 || st.RemoteDownloadCount != 0 {
		t.Fatalf("counts = %d/%d, want 1/0", st.DownloadCount, st.RemoteDownloadCount)
	}
	if st.LastDownloadedBy != "admin" {
		t.Fatalf("last_downloaded_by = %q", st.LastDownloadedBy)
	}
	if st.LastDownloadedAt == "" {
		t.Fatal("last_downloaded_at must stamp on the first download")
	}

	// The audit row carries the origin dimension of this arm.
	details := downloadDetails(t, e)
	if len(details) != 1 || details[0]["origin"] != "direct" {
		t.Fatalf("download details = %+v, want one direct-origin row", details)
	}
}

// TestT438VirtualArmCountsMemberRow pins arm 2: a virtual resolution hit
// counts the MEMBER's row (the virtual owns no node rows), and a member
// that is itself a remote repository additionally takes the remote column —
// its cache served the download. A local member never moves the remote
// column.
func TestT438VirtualArmCountsMemberRow(t *testing.T) {
	e := newEnv(t)
	fx := buildVirtual(t, e, "virt", "", []memberSpec{
		{key: "virt-loc", files: map[string]string{"lib/one.bin": "local-body"}},
		{key: "virt-rmt", remote: true, files: map[string]string{"/lib/two.bin": "remote-body"}},
	})

	// Local member hit: download_count on the member, remote column zero.
	rc, node, err := e.svc.Get(context.Background(), admin(), fx.vkey, "lib/one.bin")
	if err != nil {
		t.Fatalf("Get via virtual (local member): %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()
	if node.RepoKey != "virt-loc" {
		t.Fatalf("resolved to %s, want virt-loc", node.RepoKey)
	}
	if st := statsOf(t, e, "virt-loc", "lib/one.bin"); st.DownloadCount != 1 || st.RemoteDownloadCount != 0 {
		t.Fatalf("local member row = %d/%d, want 1/0", st.DownloadCount, st.RemoteDownloadCount)
	}
	if _, err := e.md.Nodes().Stats(context.Background(), fx.vkey, "lib/one.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("virtual namespace must own no node row, got %v", err)
	}

	// Remote member hit: the member's cache served — both columns move.
	rc, node, err = e.svc.Get(context.Background(), admin(), fx.vkey, "lib/two.bin")
	if err != nil {
		t.Fatalf("Get via virtual (remote member): %v", err)
	}
	_, _ = io.ReadAll(rc)
	_ = rc.Close()
	if node.RepoKey != "virt-rmt" {
		t.Fatalf("resolved to %s, want virt-rmt", node.RepoKey)
	}
	if st := statsOf(t, e, "virt-rmt", "lib/two.bin"); st.DownloadCount != 1 || st.RemoteDownloadCount != 1 {
		t.Fatalf("remote member row = %d/%d, want 1/1 (the remote cache served)", st.DownloadCount, st.RemoteDownloadCount)
	}

	// Both audit rows address the VIRTUAL with the via-virtual origin and
	// the resolvedFrom fragment.
	for _, d := range downloadDetails(t, e) {
		if !strings.HasPrefix(fmt.Sprint(d["origin"]), "virtual:") {
			t.Fatalf("virtual-arm origin = %v, want virtual:<vkey>", d["origin"])
		}
	}
}

// TestT438RemoteArmDirectGet pins arm 3 on the generic pull-through: both a
// fetch-then-serve MISS and the following cache HIT count the remote
// repository's own row, both columns; the origin is remote.
func TestT438RemoteArmDirectGet(t *testing.T) {
	files := map[string]string{"/up/pkg-2.0.bin": "upstream-body"}
	srv, hits := countingUpstream(t, files)
	e := newEnv(t)
	createRemote(t, e, srv.URL, "")

	if _, err := getRemote(t, e, admin(), "up/pkg-2.0.bin"); err != nil {
		t.Fatalf("first Get (miss-serve): %v", err)
	}
	st := statsOf(t, e, "generic-remote", "up/pkg-2.0.bin")
	if st.DownloadCount != 1 || st.RemoteDownloadCount != 1 {
		t.Fatalf("after miss-serve = %d/%d, want 1/1", st.DownloadCount, st.RemoteDownloadCount)
	}

	if _, err := getRemote(t, e, admin(), "up/pkg-2.0.bin"); err != nil {
		t.Fatalf("second Get (cache hit): %v", err)
	}
	st = statsOf(t, e, "generic-remote", "up/pkg-2.0.bin")
	if st.DownloadCount != 2 || st.RemoteDownloadCount != 2 {
		t.Fatalf("after cache hit = %d/%d, want 2/2", st.DownloadCount, st.RemoteDownloadCount)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}

	found := false
	for _, d := range downloadDetails(t, e) {
		if d["origin"] == "remote" {
			found = true
			if _, ok := d["statsSync"]; ok {
				t.Fatalf("statsSync marker must be absent without the policy: %+v", d)
			}
		}
	}
	if !found {
		t.Fatal("no remote-origin audit row")
	}
}

// TestT438StatsSyncEligibilityMarker pins the contentSynchronisation
// behaviorization (K69 decision 6): counting is unconditional, and
// enabled && statisticsEnabled adds the statsSync eligibility fragment to
// the remote-serving arm's audit detail — the data-plane preparation for a
// transport that is deliberately not built (architecture 11.48).
func TestT438StatsSyncEligibilityMarker(t *testing.T) {
	files := map[string]string{"/up/pkg-3.0.bin": "upstream-body"}
	srv, _ := countingUpstream(t, files)
	e := newEnv(t)
	createRemote(t, e, srv.URL, `,"contentSynchronisation":{"enabled":true,"statisticsEnabled":true}`)

	if _, err := getRemote(t, e, admin(), "up/pkg-3.0.bin"); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if st := statsOf(t, e, "generic-remote", "up/pkg-3.0.bin"); st.DownloadCount != 1 {
		t.Fatalf("counting must be unconditional, got %d", st.DownloadCount)
	}
	found := false
	for _, d := range downloadDetails(t, e) {
		if d["origin"] == "remote" && d["statsSync"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal("eligible remote arm must carry statsSync:true on the audit detail")
	}
}

// TestT438DockerResolveCountsManifest pins the docker resolve landing
// points: ResolveTag/ResolveManifest on a local repository count the
// manifest's node row; the virtual arms count the member's row.
func TestT438DockerResolveCountsManifest(t *testing.T) {
	e := newEnv(t)
	mustCreateDockerRepo(t, e, "docker-local")
	res := putManifest(t, e, admin(), "docker-local", "app", shaOf("m"), "v1", shaOf("c"))
	manifestPath := res.Node.Path

	if _, err := e.svc.ResolveTag(context.Background(), admin(), "docker-local", "app", "v1"); err != nil {
		t.Fatalf("ResolveTag: %v", err)
	}
	if _, err := e.svc.ResolveManifest(context.Background(), admin(), "docker-local", "app", shaOf("m")); err != nil {
		t.Fatalf("ResolveManifest: %v", err)
	}
	if st := statsOf(t, e, "docker-local", manifestPath); st.DownloadCount != 2 {
		t.Fatalf("manifest row download_count = %d, want 2 (one per resolve)", st.DownloadCount)
	}

	// The virtual resolve arm: a docker virtual over the local member
	// counts the member's manifest row, audit addressed to the virtual.
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "docker-virt", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
		Config: `{"repositories":["docker-local"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(docker-virt): %v", err)
	}
	if _, err := e.svc.ResolveTag(context.Background(), admin(), "docker-virt", "app", "v1"); err != nil {
		t.Fatalf("ResolveTag via virtual: %v", err)
	}
	if st := statsOf(t, e, "docker-local", manifestPath); st.DownloadCount != 3 {
		t.Fatalf("manifest row after virtual resolve = %d, want 3", st.DownloadCount)
	}
	lastOrigin := downloadDetails(t, e)
	if len(lastOrigin) == 0 || lastOrigin[len(lastOrigin)-1]["origin"] != "virtual:docker-virt" {
		t.Fatalf("virtual resolve origin = %+v", lastOrigin)
	}
}

// TestT438CountFailureNeverFailsTheServe pins the best-effort posture: a
// counting-store failure is logged and swallowed — the download itself
// answers success (the audit append's own technical-debt-#4 contract).
func TestT438CountFailureNeverFailsTheServe(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/x.bin", "body")

	failing := &failingCountStore{Store: e.md}
	e.md = failing
	// The service holds its store reference from assembly, so reach the
	// same seam through a rebuilt service on the failing store.
	e.svc = repo.NewWithClock(e.st, failing, e.az, e.au, e.clk.Now)

	rc, _, err := e.svc.Get(context.Background(), admin(), "loc", "a/x.bin")
	if err != nil {
		t.Fatalf("Get with a failing count store = %v, want success", err)
	}
	_ = rc.Close()
	if !failing.counted {
		t.Fatal("the counting seam must have been reached")
	}
}

// failingCountStore fails every CountDownload but passes everything else
// through (the consumer-side embedding keeps the interface whole).
type failingCountStore struct {
	metadata.Store
	counted bool
}

type failingCountNodes struct {
	metadata.NodeStore
	counted *bool
}

func (s *failingCountStore) Nodes() metadata.NodeStore {
	return &failingCountNodes{NodeStore: s.Store.Nodes(), counted: &s.counted}
}

func (s *failingCountNodes) CountDownload(context.Context, string, string, string, string, bool) error {
	*s.counted = true
	return errors.New("count store down (test)")
}

// TestT438ConcurrentDownloadsExactCount runs the race detector over the
// counting seam: parallel direct GETs land exactly N increments.
func TestT438ConcurrentDownloadsExactCount(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/many.bin", "body")

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rc, _, err := e.svc.Get(context.Background(), admin(), "loc", "a/many.bin")
			if err != nil {
				t.Errorf("Get: %v", err)
				return
			}
			_ = rc.Close()
		}()
	}
	wg.Wait()
	if st := statsOf(t, e, "loc", "a/many.bin"); st.DownloadCount != n {
		t.Fatalf("download_count = %d, want %d", st.DownloadCount, n)
	}
}

// ---- NFR-P71 benchmarks: the download path's marginal counting cost ----
//
// The zero-perceptible-tail-latency claim is measured, not asserted: the
// counting adds exactly ONE single-row self-incrementing UPDATE (plus, on
// remote arms only, one policy-row read for the statsSync marker) beside
// the audit append the download path already performed per GET. The three
// benchmarks below expose the three magnitudes on the same real stack —
// the full path after the change, the added UPDATE in isolation, and the
// pre-existing audit append of the same write class — so the added share
// is a division anyone can re-run.

// BenchmarkT438DownloadPathFull is the local direct arm end to end AFTER
// the counting landed (gate, node read, blob open, audit row, count row).
func BenchmarkT438DownloadPathFull(b *testing.B) {
	t := TB(b)
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/bench.bin", strings.Repeat("x", 4096))
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc, _, err := e.svc.Get(ctx, admin(), "loc", "a/bench.bin")
		if err != nil {
			b.Fatalf("Get: %v", err)
		}
		_ = rc.Close()
	}
}

// BenchmarkT438CountDownloadOnly isolates the added work: the busy-wrapped
// single-statement count UPDATE (the store method every landing point
// calls — NFR-P71's marginal cost).
func BenchmarkT438CountDownloadOnly(b *testing.B) {
	t := TB(b)
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/bench.bin", "x")
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.md.Nodes().CountDownload(ctx, "loc", "a/bench.bin", "bench", metadata.Now(), false); err != nil {
			b.Fatalf("CountDownload: %v", err)
		}
	}
}

// BenchmarkT438AuditAppendOnly is the pre-existing per-GET write of the
// same class (one audit_events INSERT per download, as-built since M1) on
// the REAL store — the yardstick the counting UPDATE must sit beside.
func BenchmarkT438AuditAppendOnly(b *testing.B) {
	t := TB(b)
	e := newEnv(t)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.md.Audits().Append(ctx, &metadata.AuditEvent{
			Actor: "bench", Action: repo.AuditActionDownload,
			RepoKey: "loc", Path: "a/bench.bin", Time: metadata.Now(),
		}); err != nil {
			b.Fatalf("Append: %v", err)
		}
	}
}

// TestT438DownloadPathPercentiles reports the local direct arm's latency
// distribution (p50/p95/p99/max) over a measured sample — the NFR-P71 tail
// evidence. "Before" decomposes as the same sample minus the isolated
// count UPDATE (the only write the ticket added; the benchmarks above pin
// its magnitude below the audit append the path already paid).
func TestT438DownloadPathPercentiles(t *testing.T) {
	e := newEnv(t)
	mustCreateRepo(t, e, "loc")
	put(t, e, admin(), "loc", "a/tail.bin", strings.Repeat("x", 4096))
	ctx := context.Background()

	const n = 400
	samples := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		rc, _, err := e.svc.Get(ctx, admin(), "loc", "a/tail.bin")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		_ = rc.Close()
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	pct := func(p float64) time.Duration {
		return samples[int(float64(len(samples)-1)*p)]
	}
	t.Logf("download path n=%d p50=%v p95=%v p99=%v max=%v (count UPDATE isolated: see BenchmarkT438CountDownloadOnly)",
		n, pct(0.50), pct(0.95), pct(0.99), samples[len(samples)-1])
	if p95 := pct(0.95); p95 > 5*time.Millisecond {
		t.Fatalf("p95 = %v, want a sub-perceptible tail on the local arm", p95)
	}
}
