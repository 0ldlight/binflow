package npm_test

// T-72's real-client leg: npm 10 through a VIRTUAL repository — the M54
// one-shot install (local member's package + the remote member's upstream
// package through one registry URL) and the M55 merge observable (`npm
// view versions` over the union). Skips when no npm is on PATH; the ticket
// log records a full run.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// countingUpstream wraps one handler counting every request (the "was the
// remote member actually consulted" observable).
type countingUpstream struct {
	mu  sync.Mutex
	n   int
	mux *http.ServeMux
}

func (c *countingUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	c.mux.ServeHTTP(w, r)
}

func (c *countingUpstream) hits() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestNpmVirtualClientSuite(t *testing.T) {
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		t.Skipf("npm not available on PATH: %v", err)
	}
	out, err := exec.Command(npmBin, "--version").Output()
	if err != nil {
		t.Skipf("npm --version failed: %v", err)
	}
	t.Logf("npm client %s", strings.TrimSpace(string(out)))

	srv, adminAuth, upstream := newVirtualClientStack(t)
	regURL := srv.URL + "/binflow/api/npm/npmv-virt/"
	work := t.TempDir()

	npmrc := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(work, t.Name())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".npmrc"), []byte(
			"registry="+regURL+"\n"+
				fmt.Sprintf("//%s/binflow/api/npm/npmv-virt/:_auth=%s\n", srv.Listener.Addr().String(), adminAuth)), 0o644); err != nil {
			t.Fatalf(".npmrc: %v", err)
		}
		return dir
	}

	// Seed the local member with a REAL npm publish. npm resolves the
	// project .npmrc from the directory holding package.json, so the seed
	// project carries its own (registry pointed at the MEMBER — publishing
	// through the un-routed virtual would be the C5 405, a different test's
	// business) and "files":[] keeps the .npmrc out of the tarball.
	seedReg := srv.URL + "/binflow/api/npm/npmv-loc/"
	pkgDir := filepath.Join(work, "seed-pkg")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir seed-pkg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "package.json"), []byte(
		`{"name":"demo-pkg","version":"1.0.0","description":"the local member's copy","files":[]}`), 0o644); err != nil {
		t.Fatalf("package.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, ".npmrc"), []byte(
		"registry="+seedReg+"\n"+
			fmt.Sprintf("//%s/binflow/api/npm/npmv-loc/:_auth=%s\n", srv.Listener.Addr().String(), adminAuth)), 0o644); err != nil {
		t.Fatalf("seed .npmrc: %v", err)
	}
	if out, err := runClient(t, npmBin, pkgDir, "publish", "--access", "public"); err != nil {
		t.Fatalf("npm publish to the local member failed: %s", out)
	} else {
		t.Logf("seed publish: %s", oneLineClient(out))
	}

	// M54: one install through the VIRTUAL fetches BOTH members' packages
	// (demo-pkg from the local member, up-pkg pulled through the remote
	// member) — the CI/CD single-URL story.
	proj := npmrc(t)
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(
		`{"name":"proj","version":"1.0.0","dependencies":{"demo-pkg":"1.0.0","up-pkg":"1.0.0"}}`), 0o644); err != nil {
		t.Fatalf("package.json: %v", err)
	}
	if out, err := runClient(t, npmBin, proj, "install", "--no-audit", "--no-fund"); err != nil {
		t.Fatalf("M54 npm install through the virtual failed: %s", out)
	} else {
		t.Logf("M54 npm install: %s", oneLineClient(out))
	}
	for _, pkg := range []string{"demo-pkg", "up-pkg"} {
		if _, err := os.Stat(filepath.Join(proj, "node_modules", pkg, "package.json")); err != nil {
			t.Errorf("M54: %s not installed through the virtual: %v", pkg, err)
		}
	}

	// M55: the merge observable — demo-pkg exists in BOTH members (1.0.0
	// local, 2.0.0 upstream), and `npm view` through the virtual reports
	// the union with latest recomputed.
	if out, err := runClient(t, npmBin, proj, "view", "demo-pkg", "versions", "--json"); err != nil {
		t.Fatalf("M55 npm view through the virtual failed: %s", out)
	} else {
		var versions []string
		if err := json.Unmarshal([]byte(out), &versions); err != nil {
			t.Fatalf("M55 npm view output not a version list: %v (%s)", err, out)
		}
		want := map[string]bool{"1.0.0": false, "2.0.0": false}
		for _, v := range versions {
			if _, ok := want[v]; ok {
				want[v] = true
			}
		}
		for v, seen := range want {
			if !seen {
				t.Errorf("M55: merged version %s missing from npm view (%v)", v, versions)
			}
		}
		t.Logf("M55 npm view versions: %v", versions)
	}

	// The remote member's package resolved through the virtual is the
	// upstream's copy.
	if out, err := runClient(t, npmBin, proj, "view", "up-pkg", "version"); err != nil {
		t.Fatalf("npm view up-pkg through the virtual failed: %s", out)
	} else {
		t.Logf("up-pkg via virtual: %s", oneLineClient(out))
	}
	if got := upstream.hits(); got == 0 {
		t.Errorf("the remote member was never consulted (upstream hits 0)")
	}
	t.Logf("upstream requests through the virtual: %d", upstream.hits())
}

// newVirtualClientStack assembles the full httpapi stack with the npm
// adapter mounted plus a counting mock upstream behind one remote member,
// and a virtual over [local, remote] — the M54 topology.
func newVirtualClientStack(t *testing.T) (*httptest.Server, string, *countingUpstream) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)

	upstream := &countingUpstream{mux: http.NewServeMux()}
	upServer := httptest.NewServer(upstream)
	t.Cleanup(upServer.Close)

	admin := &auth.Principal{Name: "admin", Admin: true}
	rows := []*metadata.Repo{
		{RepoKey: "npmv-loc", Type: repo.TypeLocal, PackageType: npm.Protocol},
		{RepoKey: "npmv-rem", Type: repo.TypeRemote, PackageType: npm.Protocol,
			Config: `{"url":"` + upServer.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "npmv-virt", Type: repo.TypeVirtual, PackageType: npm.Protocol,
			Config: `{"repositories":["npmv-loc","npmv-rem"]}`},
	}
	for _, row := range rows {
		if _, err := svc.CreateRepo(ctx, admin, row); err != nil {
			t.Fatalf("create %s: %v", row.RepoKey, err)
		}
	}

	// The upstream's two packages, in BinFlow's own layout (the shape the
	// aggregation fetches): up-pkg@1.0.0 and demo-pkg@2.0.0 (the merge
	// partner of the local member's 1.0.0).
	seedUpstreamPackage(t, upstream.mux, "up-pkg", "1.0.0")
	seedUpstreamPackage(t, upstream.mux, "demo-pkg", "2.0.0")

	npmHandler := npm.New(svc, md.Repos(), npm.Options{BaseURL: ""}).
		WithAuth(authSvc, md.Users(), authSvc).
		WithLedger(md.Blobs())
	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		DataDir:   dataDir,
		Console:   console.Handler(),
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), npmHandler},
		Version:   "t72-client-suite",
		Revision:  "test",
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, base64.StdEncoding.EncodeToString([]byte("admin:password")), upstream
}

// seedUpstreamPackage serves one package on the mock upstream in the
// BinFlow layout: <name>/packument.json plus <name>/-/<name>-<v>.tgz, the
// tarball a REAL npm package archive (valid package/ directory, correct
// integrity declared).
func seedUpstreamPackage(t *testing.T, mux *http.ServeMux, name, version string) {
	t.Helper()
	tarball := npmTarball(t, name, version)
	sha1Sum := sha1.Sum(tarball)
	sha512Sum := sha512.Sum512(tarball)
	tarballRef := fmt.Sprintf("%s/-/%s-%s.tgz", name, name, version)
	packument := fmt.Sprintf(`{"_id":%[1]q,"name":%[1]q,"description":"the upstream member's copy",`+
		`"dist-tags":{"latest":%[2]q},"time":{"created":"2026-01-01T00:00:00Z",%[2]q:"2026-01-01T00:00:00Z"},`+
		`"versions":{%[2]q:{"name":%[1]q,"version":%[2]q,`+
		`"dist":{"tarball":%[5]q,`+
		`"shasum":%[3]q,"integrity":"sha512-%[4]s"}}}}`,
		name, version, hex.EncodeToString(sha1Sum[:]),
		base64.StdEncoding.EncodeToString(sha512Sum[:]), tarballRef)
	mux.HandleFunc("/"+name+"/packument.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(packument))
	})
	mux.HandleFunc("/"+name+"/-/"+name+"-"+version+".tgz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(tarball)
	})
}

// npmTarball builds one valid npm package tarball (the package/ directory
// convention npm's unpacker expects).
func npmTarball(t *testing.T, name, version string) []byte {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{
		"name": name, "version": version, "description": "upstream fixture",
	})
	if err != nil {
		t.Fatalf("marshal fixture manifest: %v", err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for path, data := range map[string][]byte{
		"package/package.json": manifest,
		"package/index.js":     []byte("module.exports = 'upstream';\n"),
	} {
		hdr := &tar.Header{Name: path, Mode: 0o644, Size: int64(len(data))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// runClient runs one npm command in dir, returning combined output.
func runClient(t *testing.T, bin, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// oneLineClient flattens client output for the log.
func oneLineClient(s string) string { return strings.Join(strings.Fields(s), " ") }
