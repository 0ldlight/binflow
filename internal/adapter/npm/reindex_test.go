package npm

// T-354's copy-side reindex entry (the T-351 L16 flip): a copy that lands
// tarballs through the copy/move pipeline bypasses the publish plane, so
// the packument must be rebuilt from storage facts — the tarballs
// themselves. The end-to-end leg wires the REAL CopyMoveObserver seam the
// way cmd does (copy fires it with the candidate directory set), then a
// real-shaped packument read answers the versions a plain `npm install`
// resolves against.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// npmTarball packs one real tgz: package/package.json carrying the
// manifest (the rebuild's source of facts).
func npmTarball(t *testing.T, name, version, description string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"name": name, "version": version, "description": description,
	})
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "package/package.json", Mode: 0o644, Size: int64(len(manifest))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(manifest); err != nil {
		t.Fatalf("tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gz close: %v", err)
	}
	return buf.Bytes()
}

// testObserver mimics the cmd assembly's dispatch (the copyMoveIndexObserver
// shape) for the npm face only: run ReindexDirs as the system identity.
type testObserver struct{ h *Handler }

func (o testObserver) AfterCopyMove(ctx context.Context, op, targetRepo string, dirs []string) {
	if op != repo.OpCopy {
		return
	}
	// Errors land in the test log; the contract forbids propagation anyway.
	_ = o.h.ReindexDirs(ctx, repo.SystemPrincipal(), targetRepo, dirs)
}

// TestReindexDirsCopyFlip: the T-351 notarget reproduction flipped — copy
// tarballs (and only tarballs: the packument node stays behind) into a
// fresh npm repository through the REAL copy pipeline with the observer
// wired, then the target's packument answers both versions with a live
// latest tag and honest dist digests.
func TestReindexDirsCopyFlip(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	if err := s.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "npm-dst", Type: repo.TypeLocal, PackageType: Protocol,
	}); err != nil {
		t.Fatalf("seed dst repo: %v", err)
	}

	tb10 := npmTarball(t, "flip-pkg", "1.0.0", "first")
	tb11 := npmTarball(t, "flip-pkg", "1.1.0", "second")
	for _, tb := range []string{string(tb10), string(tb11)} {
		doc := publishDoc("flip-pkg", versionOf(t, tb), tb, nil, nil)
		rr := s.call(http.MethodPut, "/npm-local/flip-pkg", mustJSON(doc), adminPrincipal, nil)
		if rr.Code != http.StatusCreated {
			t.Fatalf("publish = %d %s", rr.Code, bodyOf(rr))
		}
	}
	// Drop the source packument + package folders' index noise: the copy
	// source below addresses the TARBALL files only, the exact shape the
	// notarget repro used (the packument node never leaves the source).
	if err := s.svc.Delete(ctx, adminPrincipal, "npm-local", packumentPath("flip-pkg")); err != nil {
		t.Fatalf("drop source packument: %v", err)
	}

	repo.AttachCopyMoveObserver(s.svc, testObserver{h: s.h})
	cms, ok := s.svc.(repo.CopyMoveService)
	if !ok {
		t.Fatalf("the stack's service lacks the CopyMoveService face")
	}
	res, err := cms.CopyOrMove(ctx, adminPrincipal, repo.CopyMoveRequest{
		Op: repo.OpCopy, SrcRepo: "npm-local", SrcPath: "flip-pkg/-",
		TargetRepo: "npm-dst", TargetPath: "flip-pkg/-",
	})
	if err != nil || res.HTTPStatus != http.StatusOK {
		t.Fatalf("copy = (err %v, status %d, msgs %s)", err, res.HTTPStatus, msgTextOf(res))
	}

	// The observer fires asynchronously: poll the target's packument until
	// it answers (the flip), then pin the content.
	deadline := time.Now().Add(5 * time.Second)
	var doc map[string]any
	for {
		rr := s.call(http.MethodGet, "/npm-dst/flip-pkg", "", adminPrincipal, nil)
		if rr.Code == http.StatusOK {
			if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
				t.Fatalf("packument body: %v (%s)", err, bodyOf(rr))
			}
			if len(versionsOf(doc)) == 2 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the copy never rebuilt the target packument (last %d %s)", rr.Code, bodyOf(rr))
		}
		time.Sleep(20 * time.Millisecond)
	}

	// dist-tags.latest alive (the notarget flip's exact read), versions
	// complete; the STORED document (the write-plane view — the GET face
	// rewrites dist.tarball to the absolute URL) carries the relative
	// layout path and the honest measured digests.
	if tag := distTagsOf(doc)["latest"]; tag != "1.1.0" {
		t.Fatalf("latest = %q, want 1.1.0 (a stale or missing latest is the notarget shape)", tag)
	}
	stored := s.readDoc(t, "npm-dst", "flip-pkg")
	for v, want := range map[string][]byte{"1.0.0": tb10, "1.1.0": tb11} {
		m := mapOf(versionsOf(stored)[v])
		if m == nil {
			t.Fatalf("version %s missing from the rebuilt packument: %v", v, versionsOf(stored))
		}
		dist := distOf(m)
		s512 := sha512.Sum512(want)
		if got := stringOf(dist["integrity"]); got != "sha512-"+base64.StdEncoding.EncodeToString(s512[:]) {
			t.Fatalf("version %s integrity = %q, want the measured sha512", v, got)
		}
		if got := stringOf(dist["tarball"]); got != tarballPath("flip-pkg", v) {
			t.Fatalf("version %s tarball = %q, want %s", v, got, tarballPath("flip-pkg", v))
		}
	}
}

// TestReindexDirsIdempotentOverLiveDoc: a rebuild over a LIVE packument
// (the copy also landed the packument node) keeps every published version
// and the created stamp — recomputation converges, it does not churn.
func TestReindexDirsIdempotentOverLiveDoc(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	tb := npmTarball(t, "live-pkg", "2.0.0", "live")
	doc := publishDoc("live-pkg", "2.0.0", string(tb), nil, nil)
	if rr := s.call(http.MethodPut, "/npm-local/live-pkg", mustJSON(doc), adminPrincipal, nil); rr.Code != http.StatusCreated {
		t.Fatalf("publish = %d %s", rr.Code, bodyOf(rr))
	}
	before := s.readDoc(t, "npm-local", "live-pkg")
	created := stringOf(mapOf(before["time"])["created"])

	if err := s.h.ReindexDirs(ctx, repo.SystemPrincipal(), "npm-local",
		[]string{"live-pkg/-", "live-pkg/"}); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}
	after := s.readDoc(t, "npm-local", "live-pkg")
	if len(versionsOf(after)) != 1 || versionsOf(after)["2.0.0"] == nil {
		t.Fatalf("rebuild lost the published version: %v", after["versions"])
	}
	if got := stringOf(mapOf(after["time"])["created"]); got != created {
		t.Fatalf("created stamp churned: %q -> %q", created, got)
	}
	if tag := distTagsOf(after)["latest"]; tag != "2.0.0" {
		t.Fatalf("latest = %q, want 2.0.0", tag)
	}
}

// TestReindexDirsSkipsNonPackages: directories that cannot name a package
// (the repository root, a bare tarball marker, an illegal name) are
// skipped without error and without writing anything.
func TestReindexDirsSkipsNonPackages(t *testing.T) {
	s := newStack(t)
	if err := s.h.ReindexDirs(context.Background(), repo.SystemPrincipal(), "npm-local",
		[]string{"", "-", ".hidden/", "_internal/", "not a name/"}); err != nil {
		t.Fatalf("ReindexDirs over non-package dirs: %v", err)
	}
	nodes, err := s.svc.List(context.Background(), adminPrincipal, "npm-local", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("the skip set materialized nodes: %v", nodes)
	}
}

// TestReindexDirsUnparsableTarball: a stored .tgz that is not a gzip
// stream is an error (the rebuild refuses to guess), naming the path.
func TestReindexDirsUnparsableTarball(t *testing.T) {
	s := newStack(t)
	seedTarball(t, s, "broken-pkg", "broken-pkg-0.1.0.tgz", []byte("not a gzip stream"))
	err := s.h.ReindexDirs(context.Background(), repo.SystemPrincipal(), "npm-local",
		[]string{"broken-pkg/-/"})
	if err == nil || !strings.Contains(err.Error(), "broken-pkg-0.1.0.tgz") {
		t.Fatalf("err = %v, want the tarball named", err)
	}
}

// TestReindexDirsFilenameVersionFallback: a manifest without a version
// falls back to the file name's spelling (the C8 layout).
func TestReindexDirsFilenameVersionFallback(t *testing.T) {
	s := newStack(t)
	tb := npmTarball(t, "nover-pkg", "", "no version field")
	seedTarball(t, s, "nover-pkg", "nover-pkg-3.2.1.tgz", tb)
	if err := s.h.ReindexDirs(context.Background(), repo.SystemPrincipal(), "npm-local",
		[]string{"nover-pkg/-/"}); err != nil {
		t.Fatalf("ReindexDirs: %v", err)
	}
	doc := s.readDoc(t, "npm-local", "nover-pkg")
	if versionsOf(doc)["3.2.1"] == nil {
		t.Fatalf("filename-derived version missing: %v", doc["versions"])
	}
}

// TestPackageNameOfDir: the directory->name derivation table.
func TestPackageNameOfDir(t *testing.T) {
	cases := []struct {
		dir, want string
	}{
		{"flip-pkg/-/", "flip-pkg"},
		{"flip-pkg/-", "flip-pkg"},
		{"flip-pkg/", "flip-pkg"},
		{"@scope/thing/-/", "@scope/thing"},
		{"@scope/thing/", "@scope/thing"},
		{"", ""},
		{"-", ""},
		{"/", ""},
	}
	for _, c := range cases {
		if got := packageNameOfDir(c.dir); got != c.want {
			t.Errorf("packageNameOfDir(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}

// ---- helpers ----

// storageRefOf declares a seed node's honest digest.
func storageRefOf(b []byte) storage.BlobRef {
	s := sha256.Sum256(b)
	return storage.BlobRef{Sha256: hex.EncodeToString(s[:])}
}

// seedTarball lands a raw node at the tarball path (no publish plane).
func seedTarball(t *testing.T, s *stack, name, file string, body []byte) {
	t.Helper()
	if _, err := s.svc.Put(context.Background(), adminPrincipal, "npm-local",
		name+"/"+tarballDir+file, bytes.NewReader(body), storageRefOf(body), "application/octet-stream"); err != nil {
		t.Fatalf("seed %s: %v", file, err)
	}
}

// readDoc GETs and decodes a stored packument (the write-plane view).
func (s *stack) readDoc(t *testing.T, repoKey, name string) map[string]any {
	t.Helper()
	doc, _, _, err := s.h.loadPackumentForWrite(context.Background(), adminPrincipal, repoKey, name)
	if err != nil {
		t.Fatalf("load packument %s/%s: %v", repoKey, name, err)
	}
	return doc
}

// versionOf digs the version out of a packed tarball (test-side inverse of
// the packing helper).
func versionOf(t *testing.T, tb string) string {
	t.Helper()
	gz, err := gzip.NewReader(strings.NewReader(tb))
	if err != nil {
		t.Fatalf("test tarball: %v", err)
	}
	defer gz.Close() //nolint:errcheck // test helper
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if err != nil {
			t.Fatalf("test tarball member: %v", err)
		}
		if hd.Name != "package/package.json" {
			continue
		}
		var m map[string]any
		if err := json.NewDecoder(tr).Decode(&m); err != nil {
			t.Fatalf("test manifest: %v", err)
		}
		return stringOf(m["version"])
	}
}

func msgTextOf(res *repo.CopyMoveResult) string {
	var b strings.Builder
	for _, m := range res.Messages {
		fmt.Fprintf(&b, "[%s] %s; ", m.Level, m.Message)
	}
	return b.String()
}
