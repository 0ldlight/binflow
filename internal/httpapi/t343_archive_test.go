package httpapi_test

// T-343's REST legs: the license gate's dual form across all three archive
// faces (Q4: one repo-operations slot for the family), the folder download
// over the wire (unzip round-trip reconciliation, the default-off 403, the
// anonymous 401), the archive!/ member read through the content plane, and
// the exploded upload — the M10 E-25 400-rejection reversal: X-Explode-
// Archive now deploys and answers 201 (V-1's ruling: the documented code
// over the decompiled implicit 200).

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t343Stack is the T-343 assembly: the t339 shape (the manifest WITH the
// repo-operations slot, a real license Manager) plus the folder-download
// configuration seam.
type t343Stack struct {
	ts   *httptest.Server
	md   metadata.Store
	svc  repo.Service
	mgr  *license.Manager
	keys testKeys
}

func newT343Stack(t *testing.T) *t343Stack {
	return newT343StackCfg(t, repo.DefaultFolderDownloadConfig())
}

// newT343StackCfg builds the stack with a folder-download configuration
// (the ConfigureFolderDownload assembly seam).
func newT343StackCfg(t *testing.T, folderCfg repo.FolderDownloadConfig) *t343Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Security.AnonymousAccess = false
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))
	repo.ConfigureFolderDownload(svc, folderCfg)

	k := newTestKeys(t)
	mgr, err := license.New(license.Options{
		Store:      md.Licenses(),
		VerifyKeys: k.keys,
		Audit:      audit.BestEffort(audit.New(md, true)),
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: svc,
		License:  mgr,
		Addons:   productionManifest(),
		Adapters: []adapter.Handler{generic.New(svc, md.Blobs())},
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t343Stack{ts: ts, md: md, svc: svc, mgr: mgr, keys: k}
}

// doBytes issues one request with a binary body and returns status, body
// and headers.
func (st *t343Stack) doBytes(method, path, user, pass string, body []byte, hdr map[string]string) (int, []byte, http.Header) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, st.ts.URL+path, rdr)
	if err != nil {
		panic(err) // unreachable: fixed-shape test paths
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := st.ts.Client().Do(req)
	if err != nil {
		panic(err) // unreachable: the live listener serves the test's lifetime
	}
	defer resp.Body.Close() //nolint:errcheck // test read
	got, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, got, resp.Header
}

func (st *t343Stack) do(method, path, user, pass string, body string) (int, string) {
	code, got, _ := st.doBytes(method, path, user, pass, []byte(body), nil)
	return code, string(got)
}

func (st *t343Stack) createRepo(t *testing.T, key string) {
	t.Helper()
	code, body := st.do(http.MethodPut, "/binflow/api/repositories/"+key, adminUser, adminPass,
		`{"rclass":"local","packageType":"generic"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("create repo %s = %d %s", key, code, body)
	}
}

func (st *t343Stack) putContent(t *testing.T, repoKey, path string, content []byte) {
	t.Helper()
	code, body, _ := st.doBytes(http.MethodPut, "/binflow/"+repoKey+"/"+path, adminUser, adminPass, content, nil)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("PUT %s/%s = %d %s", repoKey, path, code, body)
	}
}

func (st *t343Stack) installPro(t *testing.T) {
	t.Helper()
	spec := st.keys.spec(time.Now().UTC())
	spec.tier = "pro"
	signed := spec.sign(t)
	code, body := st.do(http.MethodPost, "/binflow/api/system/license", adminUser, adminPass, signed)
	if code != http.StatusCreated {
		t.Fatalf("install pro = %d %s", code, body)
	}
}

// t343Zip builds a zip fixture.
func t343Zip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range t343Sorted(entries) {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(entries[name])); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func t343Sorted(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// envelopeMsg adapts the shared t283 helper to byte bodies.
func envelopeMsg(t *testing.T, body []byte) string {
	t.Helper()
	return envelopeMessage(t, string(body))
}

// ---- the real-client leg (curl / unzip / shasum — the curl_compat
// convention: real tools against the full stack, honest skip when a tool
// is absent) ----

// t343Tool resolves a real client binary or skips.
func t343Tool(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s not available on PATH; real-client leg skipped", name)
	}
	return p
}

// t343Run runs a real binary and fails the test on a non-zero exit.
func t343Run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(t343Tool(t, name), args...) //nolint:gosec // test-only client invocation
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v (%s)", name, args, err, out)
	}
	return string(out)
}

// TestT343CurlRealClients: the acceptance chain with the real clients —
// curl deploys a REAL zip built by /usr/bin/zip's own writer is not needed
// (the fixture is a conforming zip), curl extracts the nested member, curl
// explodes an upload, and the folder download is reconciled by real unzip
// + shasum against the locally hashed source bytes.
func TestT343CurlRealClients(t *testing.T) {
	t343Tool(t, "curl")
	t343Tool(t, "unzip")
	t343Tool(t, "shasum")

	st := newT343StackCfg(t, repo.FolderDownloadConfig{Enabled: true})
	st.installPro(t)
	st.createRepo(t, "lib")

	// Source files on disk (curl -T uploads files, the real client shape).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello-real-client"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	// A nested zip: inner.zip carries payload.txt; outer.zip carries inner.
	inner := t343Zip(t, map[string]string{"payload.txt": "nested-payload"})
	outer := t343Zip(t, map[string]string{
		"inner.zip":     string(inner),
		"dir/hello.txt": "hello-real-client",
	})
	outerPath := filepath.Join(dir, "outer.zip")
	if err := os.WriteFile(outerPath, outer, 0o644); err != nil {
		t.Fatalf("write outer: %v", err)
	}
	explodeSrc := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(explodeSrc, t343Zip(t, map[string]string{"real/entry.txt": "exploded-by-curl"}), 0o644); err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	base := st.ts.URL
	auth := "-u" + adminUser + ":" + adminPass

	// Deploy the archive with real curl (-T).
	if out := t343Run(t, "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"-T", outerPath, auth, base+"/binflow/lib/pkg/outer.zip"); strings.TrimSpace(out) != "201" {
		t.Fatalf("curl -T outer.zip = %s", out)
	}

	// Member read: bytes equal the fixture member.
	got := t343Run(t, "curl", "-s", auth, base+"/binflow/lib/pkg/outer.zip!/dir/hello.txt")
	if got != "hello-real-client" {
		t.Fatalf("curl member read = %q", got)
	}

	// Nested member: the first "!/" at every level.
	got = t343Run(t, "curl", "-s", auth, base+"/binflow/lib/pkg/outer.zip!/inner.zip!/payload.txt")
	if got != "nested-payload" {
		t.Fatalf("curl nested member read = %q", got)
	}

	// Exploded upload: curl -T + the header; 201 (the E-25 reversal, the
	// V-1 ruling), then the entry reads back.
	if out := t343Run(t, "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"-T", explodeSrc, "-H", "X-Explode-Archive: true", auth, base+"/binflow/lib/rel/bundle.zip"); strings.TrimSpace(out) != "201" {
		t.Fatalf("curl explode = %s", out)
	}
	got = t343Run(t, "curl", "-s", auth, base+"/binflow/lib/rel/real/entry.txt")
	if got != "exploded-by-curl" {
		t.Fatalf("curl exploded entry = %q", got)
	}

	// Folder download reconciled by real unzip + shasum: the subtree's one
	// file (pkg/outer.zip) extracts and hashes identically to the source
	// archive that curl uploaded.
	zipOut := filepath.Join(dir, "download.zip")
	if out := t343Run(t, "curl", "-s", "-o", zipOut, "-w", "%{http_code}",
		auth, base+"/binflow/api/archive/download/lib/pkg?archiveType=zip"); strings.TrimSpace(out) != "200" {
		t.Fatalf("curl folder download = %s", out)
	}
	outDir := filepath.Join(dir, "unzipped")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		t.Fatalf("mkdir out: %v", err)
	}
	t343Run(t, "unzip", "-q", zipOut, "-d", outDir)
	srcSum := strings.Fields(t343Run(t, "shasum", "-a", "256", outerPath))[0]
	gotSum := strings.Fields(t343Run(t, "shasum", "-a", "256", filepath.Join(outDir, "pkg", "outer.zip")))[0]
	if srcSum != gotSum {
		t.Fatalf("unzip sha256 mismatch: %s vs %s", srcSum, gotSum)
	}
}

// TestT343GateDualForm: the Q4 ruling's two shapes across all three faces —
// the unlicensed stack answers the D4 403 with X-Binflow-License-Required:
// repo-operations; one pro install later the same requests run.
func TestT343GateDualForm(t *testing.T) {
	st := newT343StackCfg(t, repo.FolderDownloadConfig{Enabled: true})
	st.createRepo(t, "lib")
	st.putContent(t, "lib", "pkg/a.zip", t343Zip(t, map[string]string{"f.txt": "v"}))

	cases := []struct {
		name   string
		method string
		path   string
		body   []byte
		hdr    map[string]string
	}{
		{
			name:   "folder download",
			method: http.MethodGet,
			path:   "/binflow/api/archive/download/lib?archiveType=zip",
		},
		{
			name:   "member read",
			method: http.MethodGet,
			path:   "/binflow/lib/pkg/a.zip!/f.txt",
		},
		{
			name:   "explode",
			method: http.MethodPut,
			path:   "/binflow/lib/rel/b.zip",
			body:   t343Zip(t, map[string]string{"x.txt": "x"}),
			hdr:    map[string]string{"X-Explode-Archive": "true"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, body, hdr := st.doBytes(tc.method, tc.path, adminUser, adminPass, tc.body, tc.hdr)
			if code != http.StatusForbidden {
				t.Fatalf("community %s = %d %s, want 403", tc.name, code, body)
			}
			if got := hdr.Get("X-Binflow-License-Required"); got != "repo-operations" {
				t.Fatalf("license header = %q", got)
			}
			if msg := envelopeMsg(t, body); !strings.Contains(msg,
				"license required: addon 'repo-operations' needs tier 'pro' (current: none)") {
				t.Fatalf("envelope message wrong: %s", msg)
			}
		})
	}

	// Pro: the same three requests run their chains.
	st.installPro(t)
	if code, body, _ := st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib?archiveType=zip", adminUser, adminPass, nil, nil); code != http.StatusOK {
		t.Fatalf("pro folder download = %d %s", code, body)
	}
	if code, body, _ := st.doBytes(http.MethodGet,
		"/binflow/lib/pkg/a.zip!/f.txt", adminUser, adminPass, nil, nil); code != 200 || string(body) != "v" {
		t.Fatalf("pro member read = %d %q", code, body)
	}
	if code, body, _ := st.doBytes(http.MethodPut, "/binflow/lib/rel/b.zip",
		adminUser, adminPass, t343Zip(t, map[string]string{"y.txt": "y"}),
		map[string]string{"X-Explode-Archive": "true"}); code != http.StatusCreated {
		t.Fatalf("pro explode = %d %s", code, body)
	}
}

// TestT343FolderDownloadREST: the wire chain — the default-off 403, the
// anonymous 401 (its own wording, before the read question), the
// archiveType 400, and the unzip reconciliation of the streamed zip
// (per-file sha256 对账) plus the tar.gz flavor and checksum companions.
func TestT343FolderDownloadREST(t *testing.T) {
	st := newT343Stack(t) // spec defaults: enabled=false
	st.installPro(t)
	st.createRepo(t, "lib")
	st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
	st.putContent(t, "lib", "d/sub/b.bin", []byte("beta-bytes"))

	// Default off: the §2.2 step-6 403, AFTER qualification — the same
	// request on a MISSING repository answers the repository 404 instead.
	code, body, _ := st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib?archiveType=zip", adminUser, adminPass, nil, nil)
	if code != http.StatusForbidden || !strings.Contains(envelopeMsg(t, body),
		"Download Folder functionality is disabled.") {
		t.Fatalf("default-off = %d %s", code, body)
	}
	code, body, _ = st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/missing?archiveType=zip", adminUser, adminPass, nil, nil)
	if code != http.StatusNotFound || !strings.Contains(envelopeMsg(t, body),
		"missing is not a repository.") {
		t.Fatalf("missing repo = %d %s", code, body)
	}

	// The anonymous gate precedes everything (spec wording, not the shared
	// challenge text).
	code, body, hdr := st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib?archiveType=zip", "", "", nil, nil)
	if code != http.StatusUnauthorized || !strings.Contains(envelopeMsg(t, body),
		"You must be logged in to download a folder or repository.") {
		t.Fatalf("anonymous = %d %s", code, body)
	}
	if got := hdr.Get("WWW-Authenticate"); got == "" {
		t.Fatalf("anonymous 401 carries no challenge")
	}

	// archiveType is required.
	code, body, _ = st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib", adminUser, adminPass, nil, nil)
	if code != http.StatusBadRequest || !strings.Contains(envelopeMsg(t, body),
		"Unsupported archive type: '' of possible types : 'zip, tar, tar.gz, tgz'") {
		t.Fatalf("missing archiveType = %d %s", code, body)
	}

	// Enable and reconcile: every file's bytes survive the zip round trip.
	repo.ConfigureFolderDownload(st.svc, repo.FolderDownloadConfig{Enabled: true})
	var raw []byte
	code, raw, hdr = st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib/d?archiveType=zip&includeChecksumFiles=true",
		adminUser, adminPass, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("folder download = %d %s", code, raw)
	}
	if got := hdr.Get("Content-Type"); got != "application/zip" {
		t.Fatalf("content type = %q", got)
	}
	if got := hdr.Get("Content-Disposition"); !strings.Contains(got, `filename="d.zip"`) {
		t.Fatalf("disposition = %q", got)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("body is not a zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		got[f.Name] = string(b)
	}
	if got["d/a.txt"] != "alpha-bytes" || got["d/sub/b.bin"] != "beta-bytes" {
		t.Fatalf("round trip = %v", got)
	}
	if !strings.HasSuffix(got["d/a.txt.sha256"], "") || len(got["d/a.txt.sha256"]) != 64 {
		t.Fatalf("sha256 companion = %q", got["d/a.txt.sha256"])
	}

	// The tar.gz flavor and its gzip framing.
	code, raw, hdr = st.doBytes(http.MethodGet,
		"/binflow/api/archive/download/lib/d?archiveType=tar.gz", adminUser, adminPass, nil, nil)
	if code != http.StatusOK || hdr.Get("Content-Type") != "application/gzip" {
		t.Fatalf("tar.gz = %d %s", code, raw)
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	defer gz.Close() //nolint:errcheck // test read
	tr := tar.NewReader(gz)
	seen := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar walk: %v", err)
		}
		b, _ := io.ReadAll(tr)
		seen[hdr.Name] = string(b)
	}
	if seen["d/a.txt"] != "alpha-bytes" {
		t.Fatalf("tar.gz contents = %v", seen)
	}

	// Non-GET spellings fall to the E-26 404.
	if code, _ := st.do(http.MethodPost, "/binflow/api/archive/download/lib?archiveType=zip", adminUser, adminPass, ""); code != 404 {
		t.Fatalf("POST folder download = %d, want the E-26 404", code)
	}
}

// TestT343MemberReadREST: the archive!/ face over the content plane —
// member bytes and Content-Type, the verbatim miss 404, the member
// checksum suffix, and a plain "!" (no slash) NOT triggering the family.
func TestT343MemberReadREST(t *testing.T) {
	st := newT343Stack(t)
	st.installPro(t)
	st.createRepo(t, "lib")
	st.putContent(t, "lib", "pkg/a.zip", t343Zip(t, map[string]string{
		"dir/hello.txt": "hello-bytes",
	}))
	st.putContent(t, "lib", "plain!name.txt", []byte("bang-but-no-slash"))

	code, body, hdr := st.doBytes(http.MethodGet,
		"/binflow/lib/pkg/a.zip!/dir/hello.txt", adminUser, adminPass, nil, nil)
	if code != 200 || string(body) != "hello-bytes" {
		t.Fatalf("member read = %d %q", code, body)
	}
	if got := hdr.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}

	// The member checksum suffix answers the computed sha1.
	code, body, _ = st.doBytes(http.MethodGet,
		"/binflow/lib/pkg/a.zip!/dir/hello.txt.sha1", adminUser, adminPass, nil, nil)
	if code != 200 || len(string(body)) != 40 {
		t.Fatalf("member checksum = %d %q", code, body)
	}

	// The verbatim miss.
	code, body, _ = st.doBytes(http.MethodGet,
		"/binflow/lib/pkg/a.zip!/nope.txt", adminUser, adminPass, nil, nil)
	if code != 404 || !strings.Contains(envelopeMsg(t, body),
		"Unable to find zip resource: 'nope.txt' using full URI 'lib/pkg/a.zip!/nope.txt'") {
		t.Fatalf("miss = %d %s", code, body)
	}

	// A "!" without the following "/" is an ordinary file name.
	code, body, _ = st.doBytes(http.MethodGet,
		"/binflow/lib/plain!name.txt", adminUser, adminPass, nil, nil)
	if code != 200 || string(body) != "bang-but-no-slash" {
		t.Fatalf("plain bang name = %d %q", code, body)
	}

	// Non-GET on the member spelling is refused with Allow.
	code, _, hdr = st.doBytes(http.MethodDelete,
		"/binflow/lib/pkg/a.zip!/dir/hello.txt", adminUser, adminPass, nil, nil)
	if code != http.StatusMethodNotAllowed || hdr.Get("Allow") != "GET" {
		t.Fatalf("DELETE member = %d", code)
	}
}

// TestT343ExplodeREST: the M10 E-25 reversal — X-Explode-Archive deploys
// (201, EMPTY body: V-1's ruling), entries land under the PUT parent, the
// archive itself never lands, the whitelist keeps its 400, and the Atomic
// spelling rides the same face.
func TestT343ExplodeREST(t *testing.T) {
	st := newT343Stack(t)
	st.installPro(t)
	st.createRepo(t, "lib")
	payload := t343Zip(t, map[string]string{
		"a.txt":     "alpha",
		"sub/b.txt": "bravo",
	})

	code, body, hdr := st.doBytes(http.MethodPut, "/binflow/lib/rel/pkg.zip",
		adminUser, adminPass, payload, map[string]string{"X-Explode-Archive": "true"})
	if code != http.StatusCreated {
		t.Fatalf("explode = %d %s, want 201 (the E-25 reversal)", code, body)
	}
	if len(body) != 0 {
		t.Fatalf("explode body = %q, want empty", body)
	}
	if got := hdr.Get("X-Binflow-Exploded-Files"); got != "2" {
		t.Fatalf("exploded-files header = %q", got)
	}
	for _, p := range []string{"rel/a.txt", "rel/sub/b.txt"} {
		if code, body := st.do(http.MethodGet, "/binflow/lib/"+p, adminUser, adminPass, ""); code != 200 {
			t.Fatalf("GET %s = %d %s", p, code, body)
		}
	}
	if code, _ := st.do(http.MethodGet, "/binflow/lib/rel/pkg.zip", adminUser, adminPass, ""); code != 404 {
		t.Fatalf("the archive itself landed: %d", code)
	}

	// The whitelist: .rar keeps the verbatim 400.
	code, body, _ = st.doBytes(http.MethodPut, "/binflow/lib/rel/x.rar",
		adminUser, adminPass, payload, map[string]string{"X-Explode-Archive": "true"})
	if code != http.StatusBadRequest || !strings.Contains(envelopeMsg(t, body),
		"Unsupported archive extension: 'rar' of possible extensions : 'zip, tar, tar.gz, tgz'") {
		t.Fatalf("whitelist = %d %s", code, body)
	}

	// The Atomic spelling rides the same face.
	code, _, _ = st.doBytes(http.MethodPut, "/binflow/lib/rel/atomic.zip",
		adminUser, adminPass, payload, map[string]string{"X-Explode-Archive-Atomic": "true"})
	if code != http.StatusCreated {
		t.Fatalf("atomic explode = %d", code)
	}

	// A garbage header value is an explicit 400, never half-selected.
	code, body, _ = st.doBytes(http.MethodPut, "/binflow/lib/rel/g.zip",
		adminUser, adminPass, payload, map[string]string{"X-Explode-Archive": "maybe"})
	if code != http.StatusBadRequest {
		t.Fatalf("garbage header = %d %s", code, body)
	}

	// Anonymous writes never reach the branch (the route's 401 door).
	code, _, _ = st.doBytes(http.MethodPut, "/binflow/lib/rel/anon.zip",
		"", "", payload, map[string]string{"X-Explode-Archive": "true"})
	if code != http.StatusUnauthorized {
		t.Fatalf("anonymous explode = %d", code)
	}
}
