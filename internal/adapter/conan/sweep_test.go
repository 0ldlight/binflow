package conan

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The ADR-0042 sweep matrix (T-371): the legacy double tree migrates onto
// the spec layout with the three-angle sha256 reconciliation, the
// predicate-consuming second run changes nothing, the dedup arm keeps
// content single-copy with conflicts=0, and a zero-conan instance scans
// nothing.

// captureHandler is a slog.Handler that records every line's level and
// message (the sweep's INFO/WARN observability assertions).
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

// lines returns the captured messages at or above level.
func (h *captureHandler) lines(level slog.Level) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Level >= level {
			out = append(out, r.Message)
		}
	}
	return out
}

// seedLegacyDoubleTree lands the T-340 repro shape — the storage outcome
// the pre-T-371 channelFileName bug produced for every v1 channel package
// file — through the REAL channel wire: the doubled tail
// (`0/package/<pid>/package/<pid>/<name>`) is exactly what the buggy trim
// turned the client's flat name into, so the fixed binary replays the
// legacy tree byte-for-byte. The recipe arm lands spec (it never had the
// bug) and both revision registrations run (the channel PUT's own tail),
// so the index layer resolves `0` as latest exactly as a legacy instance
// would have.
func seedLegacyDoubleTree(t *testing.T, s *stack, key string) (ref, string) {
	t.Helper()
	s.seedRepo(t, key, repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	pid := fixturePID(1)

	put := func(p string, b []byte) {
		t.Helper()
		if code, body, _ := s.put(v1(key, "files/myuser/hello/1.0/stable/"+p), b, nil); code != http.StatusCreated {
			t.Fatalf("channel PUT %s = (%d, %s), want 201", p, code, body)
		}
	}
	put("export/conanfile.py", []byte("recipe"))
	put("export/conanmanifest.txt", []byte("manifest"))
	put("0/package/"+pid+"/package/"+pid+"/conaninfo.txt",
		[]byte(conaninfoFixture([]string{"os=Macos", "arch=x86_64"}, []string{"shared=True"}, []string{"zlib/1.2.11"})))
	put("0/package/"+pid+"/package/"+pid+"/conanmanifest.txt", []byte("pkg manifest"))
	put("0/package/"+pid+"/package/"+pid+"/conan_package.tgz", []byte("tgz"))
	return r, pid
}

// fileRows snapshots the repo's FILE rows (folder scaffolding excluded) as
// path -> sha256 — the reconciliation's content ledger.
func fileRows(t *testing.T, s *stack, key string) map[string]string {
	t.Helper()
	nodes, err := s.svc.List(context.Background(), adminPrincipal(), key, "")
	if err != nil {
		t.Fatalf("list %s: %v", key, err)
	}
	out := map[string]string{}
	for _, n := range nodes {
		if !strings.HasSuffix(n.Path, "/") {
			out[n.Path] = n.Sha256
		}
	}
	return out
}

// blobManifest walks the engine's blobs directory (path -> size): the
// reconciliation's blob-face ledger — the sweep must leave it untouched.
func blobManifest(t *testing.T, s *stack) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	root := filepath.Join(s.dataDir, "blobs")
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out[rel] = info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk blobs: %v", err)
	}
	return out
}

// foldLegacyPath maps one legacy double-spelled path onto its spec target
// ("" when the path is not the double form) — the reconciliation's mapping
// fold, spelled independently of doubleLayoutPrefix's groupby (the test
// re-derives the tail strip directly).
func foldLegacyPath(path string) string {
	i := strings.Index(path, "/0/package/")
	if i < 0 {
		return ""
	}
	head := path[:i+1] // <coordinateRoot>/
	rest := path[i+len("/0/package/"):]
	pid, more, found := strings.Cut(rest, "/")
	if !found || !strings.HasPrefix(more, "0/package/"+pid+"/") {
		return ""
	}
	return head + "0/package/" + pid + "/0/" + more[len("0/package/"+pid+"/"):]
}

// assertMapsEqual fails with a diff of the two string maps.
func assertMapsEqual(t *testing.T, got, want map[string]string, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d entries, want %d (%v vs %v)", label, len(got), len(want), got, want)
		return
	}
	for k, v := range want {
		if gv, ok := got[k]; !ok {
			t.Errorf("%s: lacks %q (have %v)", label, k, got)
		} else if gv != v {
			t.Errorf("%s: %q sha = %s, want %s", label, k, gv, v)
		}
	}
}

// TestSweepV1FilesLayoutMigratesLegacyTrees (AC2): the T-340 double-tree
// fixture migrates onto the spec layout with the three-angle sha256
// reconciliation — (1) the per-node ledger folds to equality, (2) the
// repository's FILE-node count is unchanged, (3) the blobs directory is
// untouched — and all four post-condition faces recover: bare snapshot
// keys, bare v2 files-list keys, non-empty ref-search settings, and the
// channel GET resolving through latest-pRev onto the spec path.
func TestSweepV1FilesLayoutMigratesLegacyTrees(t *testing.T) {
	s := newStack(t)
	_, pid := seedLegacyDoubleTree(t, s, "cn-local")

	preFiles := fileRows(t, s, "cn-local")
	preBlobs := blobManifest(t, s)
	// 2 recipe files + the coordinate's index.json and .timestamp + the
	// pid's index.json and .timestamp + the 3 double-spelled package files.
	if len(preFiles) != 9 {
		t.Fatalf("pre-sweep file rows = %d (%v), want 9", len(preFiles), preFiles)
	}
	doubles := 0
	for p := range preFiles {
		if foldLegacyPath(p) != "" {
			doubles++
		}
	}
	if doubles != 3 {
		t.Fatalf("double-spelled file rows = %d, want 3", doubles)
	}

	h := &captureHandler{}
	if err := SweepV1FilesLayout(context.Background(), s.svc, slog.New(h)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// (1) per-node: the folded pre-ledger equals the post-ledger.
	folded := map[string]string{}
	for p, sha := range preFiles {
		if dst := foldLegacyPath(p); dst != "" {
			folded[dst] = sha
		} else {
			folded[p] = sha
		}
	}
	assertMapsEqual(t, fileRows(t, s, "cn-local"), folded, "per-node reconciliation")

	// (2) aggregate: same FILE-node count (no dedup arm in this fixture).
	if post := fileRows(t, s, "cn-local"); len(post) != len(preFiles) {
		t.Errorf("node count moved: pre %d post %d (dedup/conflicts are the only exempt arms)", len(preFiles), len(post))
	}

	// (3) blob face: the blobs directory is byte-for-byte what it was.
	postBlobs := blobManifest(t, s)
	if len(postBlobs) != len(preBlobs) {
		t.Errorf("blob count moved: pre %d post %d", len(preBlobs), len(postBlobs))
	}
	for rel, size := range preBlobs {
		if postBlobs[rel] != size {
			t.Errorf("blob %s: pre %d post %d bytes", rel, size, postBlobs[rel])
		}
	}

	// No double-spelled path survives, scaffolding included (随迁删除).
	nodes, err := s.svc.List(context.Background(), adminPrincipal(), "cn-local", "")
	if err != nil {
		t.Fatalf("post-sweep list: %v", err)
	}
	for _, n := range nodes {
		if strings.Contains(n.Path, "/0/package/"+pid+"/0/package/") {
			t.Errorf("double-spelled row survived: %s", n.Path)
		}
	}

	// Post-condition faces (ADR-0042 decision 7).
	code, body, _ := s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/packages/"+pid))
	if code != http.StatusOK {
		t.Fatalf("post-sweep package snapshot = (%d, %s)", code, body)
	}
	var snap snapshotBody
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("snapshot body %q: %v", body, err)
	}
	for _, name := range []string{"conaninfo.txt", "conanmanifest.txt", "conan_package.tgz"} {
		if _, ok := snap[name]; !ok {
			t.Errorf("package snapshot keys = %v, want the BARE name %s", snap, name)
		}
	}
	for name := range snap {
		if strings.Contains(name, "/") {
			t.Errorf("package snapshot key %q carries a path prefix (the D-F2 defect)", name)
		}
	}

	code, body, _ = s.get(v2("cn-local", "hello/1.0/myuser/stable/revisions/0/packages/"+pid+"/revisions/0/files"))
	if code != http.StatusOK {
		t.Fatalf("post-sweep v2 files list = (%d, %s)", code, body)
	}
	var listing filesResponse
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatalf("files body %q: %v", body, err)
	}
	if _, ok := listing.Files["conaninfo.txt"]; !ok || len(listing.Files) != 3 {
		t.Errorf("v2 files keys = %v, want the three BARE names", listing.Files)
	}

	code, body, _ = s.get(v1("cn-local", "conans/hello/1.0/myuser/stable/search"))
	if code != http.StatusOK {
		t.Fatalf("post-sweep ref search = (%d, %s)", code, body)
	}
	var meta map[string]*pkgMeta
	if err := json.Unmarshal([]byte(body), &meta); err != nil {
		t.Fatalf("ref search body %q: %v", body, err)
	}
	got := meta[pid]
	if got == nil {
		t.Fatalf("ref search = %v, want the pid row", meta)
	}
	if got.Settings["os"] != "Macos" || got.Settings["arch"] != "x86_64" ||
		got.Options["shared"] != "True" || got.Requires["zlib/1.2.11"] != "" {
		t.Errorf("ref search settings/options/requires = %v (the D-F2 empty-map defect)", got)
	}

	// The channel GET rides latest-pRev resolution onto the spec path.
	code, got2, _ := s.get(v1("cn-local", "files/myuser/hello/1.0/stable/0/package/"+pid+"/conan_package.tgz"))
	if code != http.StatusOK || got2 != "tgz" {
		t.Errorf("post-sweep channel GET = (%d, %q), want (200, tgz)", code, got2)
	}

	// The per-repository INFO line (decision 4's pinned shape).
	info := h.lines(slog.LevelInfo)
	want := "conan v1 layout sweep: repo=cn-local moved=3 dedup=1 conflicts=0"
	if len(info) != 1 || info[0] != want {
		t.Errorf("sweep INFO lines = %v, want [%s]", info, want)
	}
	if warns := h.lines(slog.LevelWarn); len(warns) != 0 {
		t.Errorf("sweep WARN lines = %v, want none (conflict=0 green gate)", warns)
	}
}

// TestSweepV1FilesLayoutIdempotent (AC3): the second run is a no-op — the
// predicate consumed the legacy form, moved=0, and the tree is identical.
func TestSweepV1FilesLayoutIdempotent(t *testing.T) {
	s := newStack(t)
	seedLegacyDoubleTree(t, s, "cn-local")

	h := &captureHandler{}
	log := slog.New(h)
	if err := SweepV1FilesLayout(context.Background(), s.svc, log); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	afterFirst := fileRows(t, s, "cn-local")

	h2 := &captureHandler{}
	if err := SweepV1FilesLayout(context.Background(), s.svc, slog.New(h2)); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	info := h2.lines(slog.LevelInfo)
	if len(info) != 1 || info[0] != "conan v1 layout sweep: repo=cn-local moved=0 dedup=0 conflicts=0" {
		t.Errorf("second-run INFO lines = %v, want moved=0 (二跑零改)", info)
	}
	assertMapsEqual(t, fileRows(t, s, "cn-local"), afterFirst, "second-run tree")
}

// TestSweepV1FilesLayoutDedupArm (AC3): when the spec target already holds
// the SAME sha256, the source row drops (counted dedup), content stays
// single-copy and conflicts stays 0 — the production green gate.
func TestSweepV1FilesLayoutDedupArm(t *testing.T) {
	s := newStack(t)
	_, pid := seedLegacyDoubleTree(t, s, "cn-local")

	// The spec-side twin: the same bytes already landed at the spec path
	// (the retransmit-idempotent form a legacy mixed state can hold).
	code, body, _ := s.put(v1("cn-local", "files/myuser/hello/1.0/stable/0/package/"+pid+"/conan_package.tgz"),
		[]byte("tgz"), nil)
	if code != http.StatusCreated {
		t.Fatalf("spec-side twin PUT = (%d, %s)", code, body)
	}

	preBlobs := blobManifest(t, s)
	h := &captureHandler{}
	if err := SweepV1FilesLayout(context.Background(), s.svc, slog.New(h)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	info := h.lines(slog.LevelInfo)
	if len(info) != 1 || !strings.Contains(info[0], "conflicts=0") || !strings.Contains(info[0], "dedup=2") {
		t.Errorf("sweep INFO lines = %v, want dedup=2 (the twin file + the source folder row) conflicts=0", info)
	}
	if warns := h.lines(slog.LevelWarn); len(warns) != 0 {
		t.Errorf("sweep WARN lines = %v, want none", warns)
	}

	// Content single-copy: exactly one FILE row under the pRev root carries
	// the tgz, and the blob face never moved.
	rows := fileRows(t, s, "cn-local")
	hits := 0
	for p, sha := range rows {
		if strings.Contains(p, "/0/package/"+pid+"/0/") && strings.HasSuffix(p, "/conan_package.tgz") {
			hits++
			if sha == "" {
				t.Errorf("tgz row %s has no sha", p)
			}
		}
	}
	if hits != 1 {
		t.Errorf("tgz rows under the pRev root = %d, want the single deduped copy", hits)
	}
	postBlobs := blobManifest(t, s)
	if len(postBlobs) != len(preBlobs) {
		t.Errorf("blob count moved: pre %d post %d", len(preBlobs), len(postBlobs))
	}
}

// countingListService counts the List calls riding through it — the
// zero-conan instance's zero-scan observable.
type countingListService struct {
	repo.Service
	lists int
}

func (c *countingListService) List(ctx context.Context, p *repo.Principal, repoKey, prefix string) ([]*metadata.Node, error) {
	c.lists++
	return c.Service.List(ctx, p, repoKey, prefix)
}

// TestSweepV1FilesLayoutZeroConanNoScan (AC4): an instance without a LOCAL
// conan repository scans nothing — not even the repository listing beyond
// the manifest (a generic local and a conan REMOTE both present, both
// filtered out).
func TestSweepV1FilesLayoutZeroConanNoScan(t *testing.T) {
	s := newStack(t)
	// A GENERIC local repository (the seedRepo helpers hardcode the conan
	// package type, so the generic row lands directly) plus a conan REMOTE
	// repository: the v1 data plane is local-only (S6) and so is the sweep's
	// scope — the class and package-type filters keep both out.
	ctx := context.Background()
	for _, row := range []*metadata.Repo{
		{RepoKey: "gen-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric},
		{RepoKey: "cn-remote", Type: repo.TypeRemote, PackageType: Protocol},
	} {
		if err := s.md.Repos().Create(ctx, row); err != nil {
			t.Fatalf("seed %s: %v", row.RepoKey, err)
		}
	}

	counting := &countingListService{Service: s.svc}
	if err := SweepV1FilesLayout(context.Background(), counting, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if counting.lists != 0 {
		t.Errorf("List calls on a zero-conan instance = %d, want 0 (零扫描)", counting.lists)
	}
}
