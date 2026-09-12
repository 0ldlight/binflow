package npm_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
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

// TestNpmClientSuite drives the REAL npm 10 client through the FULL httpapi
// stack (real middleware chain, T-63 api-mount seam, real repo.Service) —
// the M22..M28 acceptance sequence of FR-18. Skipped when no npm is on PATH
// (the protocol ticket's client-availability rule).
func TestNpmClientSuite(t *testing.T) {
	npmBin, err := exec.LookPath("npm")
	if err != nil {
		t.Skipf("npm not available on PATH: %v", err)
	}
	out, err := exec.Command(npmBin, "--version").Output()
	if err != nil {
		t.Skipf("npm --version failed: %v", err)
	}
	t.Logf("npm client %s", strings.TrimSpace(string(out)))

	srv, adminAuth := newClientStack(t)
	regURL := srv.URL + "/binflow/api/npm/npm-local/"
	work := t.TempDir()

	// npmProject scaffolds one project directory with the registry wired in.
	npmProject := func(t *testing.T, pkgJSON string) string {
		t.Helper()
		dir := filepath.Join(work, t.Name())
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if pkgJSON != "" {
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o644); err != nil {
				t.Fatalf("package.json: %v", err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, ".npmrc"), []byte(
			"registry="+regURL+"\n"+
				fmt.Sprintf("//%s/binflow/api/npm/npm-local/:_auth=%s\n", srv.Listener.Addr().String(), adminAuth)), 0o644); err != nil {
			t.Fatalf(".npmrc: %v", err)
		}
		return dir
	}
	// npmRun runs one npm command inside dir.
	npmRun := func(t *testing.T, dir string, args ...string) (string, error) {
		t.Helper()
		cmd := exec.Command(npmBin, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"HOME="+work,
			"npm_config_cache="+filepath.Join(work, "npm-cache"),
			"npm_config_update_notifier=false",
			"npm_config_fund=false",
			"npm_config_audit=false",
			"npm_config_loglevel=warn",
		)
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		return buf.String(), err
	}
	httpGet := func(t *testing.T, path string, hdr map[string]string) (int, http.Header, []byte) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, srv.URL+path, nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		req.Header.Set("Authorization", "Basic "+adminAuth)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, body
	}

	pkgJSON := func(name, version string) string {
		return fmt.Sprintf(`{"name":%q,"version":%q,"description":"m3 npm adapter client suite"}`, name, version)
	}

	// ---- M22: publish (FR-18-AC1) ----
	t.Run("M22 publish", func(t *testing.T) {
		dir := npmProject(t, pkgJSON("demo-pkg", "1.0.0"))
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("module.exports = 42\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := npmRun(t, dir, "publish")
		if err != nil {
			t.Fatalf("npm publish failed: %v\n%s", err, out)
		}
		if !strings.Contains(out, "+ demo-pkg@1.0.0") {
			t.Fatalf("publish output missing '+ demo-pkg@1.0.0': %s", out)
		}
		out, err = npmRun(t, dir, "view", "demo-pkg", "version", "--registry", regURL)
		if err != nil || !strings.Contains(out, "1.0.0") {
			t.Fatalf("npm view: %v\n%s", err, out)
		}
	})

	// ---- M22b: packument surface (FR-18-AC2/AC10) ----
	t.Run("M22b packument", func(t *testing.T) {
		status, hdr, body := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		if status != http.StatusOK {
			t.Fatalf("packument status = %d: %s", status, body)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("packument not JSON: %v", err)
		}
		if doc["dist-tags"].(map[string]any)["latest"] != "1.0.0" {
			t.Fatalf("dist-tags.latest = %v", doc["dist-tags"])
		}
		ver := doc["versions"].(map[string]any)["1.0.0"].(map[string]any)
		dist := ver["dist"].(map[string]any)
		tarballURL, _ := dist["tarball"].(string)
		if !strings.HasPrefix(tarballURL, srv.URL+"/binflow/") {
			t.Fatalf("tarball URL %q not rewritten to BinFlow", tarballURL)
		}
		// shasum == sha1 of the served tarball (the bytes npm packed).
		tbPath := strings.TrimPrefix(tarballURL, srv.URL)
		tst, _, tbytes := httpGet(t, tbPath, nil)
		if tst != http.StatusOK {
			t.Fatalf("tarball status = %d", tst)
		}
		sum := sha1.Sum(tbytes)
		if dist["shasum"] != hex.EncodeToString(sum[:]) {
			t.Fatalf("shasum = %v, want sha1(tarball) = %s", dist["shasum"], hex.EncodeToString(sum[:]))
		}
		// ETag == sha1 of the stored packument JSON; conditional 304.
		etag := hdr.Get("ETag")
		if etag == "" {
			t.Fatal("packument carries no ETag")
		}
		st2, _, _ := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", map[string]string{"If-None-Match": etag})
		if st2 != http.StatusNotModified {
			t.Fatalf("If-None-Match status = %d, want 304", st2)
		}
	})

	// ---- M22c: whoami via .npmrc credentials (NE-06) ----
	t.Run("M22c whoami", func(t *testing.T) {
		dir := npmProject(t, "")
		out, err := npmRun(t, dir, "whoami", "--registry", regURL)
		if err != nil || !strings.Contains(out, "admin") {
			t.Fatalf("npm whoami: %v\n%s", err, out)
		}
	})

	// ---- M23: install + cache-clean reinstall (FR-18-AC3) ----
	t.Run("M23 install", func(t *testing.T) {
		dir := npmProject(t, `{"name":"consumer","version":"1.0.0","private":true}`)
		out, err := npmRun(t, dir, "install", "demo-pkg", "--registry", regURL)
		if err != nil {
			t.Fatalf("npm install failed: %v\n%s", err, out)
		}
		pj, err := os.ReadFile(filepath.Join(dir, "node_modules", "demo-pkg", "package.json"))
		if err != nil {
			t.Fatalf("node_modules/demo-pkg missing: %v", err)
		}
		if !strings.Contains(string(pj), `"version": "1.0.0"`) && !strings.Contains(string(pj), `"version":"1.0.0"`) {
			t.Fatalf("installed version wrong: %s", pj)
		}
		if out, err = npmRun(t, dir, "cache", "clean", "--force"); err != nil {
			t.Fatalf("cache clean: %v\n%s", err, out)
		}
		if err := os.RemoveAll(filepath.Join(dir, "node_modules")); err != nil {
			t.Fatal(err)
		}
		if out, err = npmRun(t, dir, "install", "demo-pkg", "--registry", regURL); err != nil {
			t.Fatalf("reinstall after cache clean failed: %v\n%s", err, out)
		}
	})

	// ---- M24: dist-tag add/ls/rm (FR-18-AC4) ----
	t.Run("M24 dist-tags", func(t *testing.T) {
		dir := npmProject(t, pkgJSON("demo-pkg", "1.0.0"))
		if out, err := npmRun(t, dir, "dist-tag", "add", "demo-pkg@1.0.0", "beta", "--registry", regURL); err != nil {
			t.Fatalf("dist-tag add: %v\n%s", err, out)
		}
		_, _, body := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		if doc["dist-tags"].(map[string]any)["beta"] != "1.0.0" {
			t.Fatalf("beta tag missing: %s", body)
		}
		cons := npmProject(t, `{"name":"consumer2","version":"1.0.0","private":true}`)
		if out, err := npmRun(t, cons, "install", "demo-pkg@beta", "--registry", regURL); err != nil {
			t.Fatalf("install @beta: %v\n%s", err, out)
		}
		// --prefer-online mirrors the evidence rig's own mitigation (L012-1
		// E0-2): the dist-tags GET carries the reference-pinned
		// Cache-Control: max-age=60 with no validators, so npm serves the
		// add's PRE-beta tags from cache and short-circuits rm with "beta
		// is not a dist-tag". Verified live against the Artifactory
		// reference: add followed by an immediate rm (no prefer-online)
		// fails there identically — the quirk is the reference's, not ours.
		if out, err := npmRun(t, dir, "dist-tag", "rm", "demo-pkg", "beta", "--prefer-online", "--registry", regURL); err != nil {
			t.Fatalf("dist-tag rm: %v\n%s", err, out)
		}
		_, _, body = httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		if strings.Contains(string(body), `"beta"`) {
			t.Fatalf("beta tag survived rm: %s", body)
		}
	})

	// ---- M25: scoped package roundtrip (FR-18-AC5) ----
	t.Run("M25 scoped", func(t *testing.T) {
		dir := npmProject(t, pkgJSON("@acme/util", "1.0.0"))
		if err := os.WriteFile(filepath.Join(dir, "index.js"), []byte("module.exports = 7\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := npmRun(t, dir, "publish", "--access", "public"); err != nil {
			t.Fatalf("scoped publish: %v\n%s", err, out)
		}
		cons := npmProject(t, `{"name":"consumer3","version":"1.0.0","private":true}`)
		if out, err := npmRun(t, cons, "install", "@acme/util", "--registry", regURL); err != nil {
			t.Fatalf("scoped install: %v\n%s", err, out)
		}
	})

	// ---- M26: duplicate publish is 403 (FR-18-AC6, v1.1 ruling) ----
	t.Run("M26 duplicate publish", func(t *testing.T) {
		dir := npmProject(t, pkgJSON("demo-pkg", "1.0.0"))
		out, err := npmRun(t, dir, "publish")
		if err == nil {
			t.Fatalf("duplicate npm publish must fail; output: %s", out)
		}
		// npm 10 surfaces the server's 403 generically (E403); npm 11
		// refuses client-side once the served packument already lists the
		// version (no request leaves) — both wordings count as "duplicate
		// refused"; the pinned server contract is asserted on the direct
		// PUT below.
		if !strings.Contains(out, "E403") && !strings.Contains(out, "403") &&
			!strings.Contains(out, "cannot publish over the previously published version") {
			t.Fatalf("duplicate publish error not the 403 family: %s", out)
		}
		// Direct curl-equivalent: PUT the served document back -> 403 E-01.
		_, _, body := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/binflow/api/npm/npm-local/demo-pkg", bytes.NewReader(body))
		req.Header.Set("Authorization", "Basic "+adminAuth)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		putBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("curl duplicate status = %d, want 403; body=%s", resp.StatusCode, putBody)
		}
		if !strings.Contains(string(putBody), "Cannot modify pre-existing version") {
			t.Fatalf("403 body = %s", putBody)
		}
	})

	// ---- M27: unpublish single version (FR-18-AC7/AC11) ----
	t.Run("M27 unpublish", func(t *testing.T) {
		dir := npmProject(t, pkgJSON("demo-pkg", "1.0.1"))
		if out, err := npmRun(t, dir, "publish"); err != nil {
			t.Fatalf("publish 1.0.1: %v\n%s", err, out)
		}

		// The -rev placeholder PUT: 200 fake success, package unchanged.
		_, _, before := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		req, _ := http.NewRequest(http.MethodPut,
			srv.URL+"/binflow/api/npm/npm-local/demo-pkg/-rev/00000000000000000000000000000000",
			strings.NewReader(`"0-0000000000000000000000000000000"`))
		req.Header.Set("Authorization", "Basic "+adminAuth)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		revBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(revBody), `"ok":"updated package"`) {
			t.Fatalf("-rev PUT = %d %s", resp.StatusCode, revBody)
		}
		_, _, after := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		var b1, b2 map[string]any
		_ = json.Unmarshal(before, &b1)
		_ = json.Unmarshal(after, &b2)
		if fmt.Sprint(b1["versions"]) != fmt.Sprint(b2["versions"]) {
			t.Fatalf("placeholder -rev PUT changed the package")
		}

		if out, err := npmRun(t, dir, "unpublish", "demo-pkg@1.0.1", "--force", "--registry", regURL); err != nil {
			t.Fatalf("npm unpublish: %v\n%s", err, out)
		}
		_, _, body := httpGet(t, "/binflow/api/npm/npm-local/demo-pkg", nil)
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		if _, still := doc["versions"].(map[string]any)["1.0.1"]; still {
			t.Fatalf("1.0.1 survived unpublish: %s", body)
		}

		cons := npmProject(t, `{"name":"consumer4","version":"1.0.0","private":true}`)
		if _, err := npmRun(t, cons, "install", "demo-pkg@1.0.1", "--registry", regURL); err == nil {
			t.Fatal("install of unpublished version must fail")
		}
		if out, err := npmRun(t, cons, "install", "demo-pkg@1.0.0", "--registry", regURL); err != nil {
			t.Fatalf("install of surviving version failed: %v\n%s", err, out)
		}
	})

	// ---- M28: anonymous boundary (FR-18-AC8) ----
	t.Run("M28 anonymous boundary", func(t *testing.T) {
		for _, p := range []string{
			"/binflow/api/npm/npm-local/demo-pkg",
			"/binflow/api/npm/npm-local/demo-pkg/-/demo-pkg-1.0.0.tgz",
		} {
			resp, err := srv.Client().Get(srv.URL + p)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("anonymous GET %s = %d, want 200 (default anonymous read)", p, resp.StatusCode)
			}
		}
		req, _ := http.NewRequest(http.MethodPut, srv.URL+"/binflow/api/npm/npm-local/x",
			strings.NewReader("{}"))
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous PUT = %d, want 401", resp.StatusCode)
		}
		if resp.Header.Get("WWW-Authenticate") == "" {
			t.Fatal("anonymous PUT 401 carries no challenge")
		}
	})

	// ---- probes: ping / search-404 (NE-07/NE-08) ----
	t.Run("probes", func(t *testing.T) {
		status, _, body := httpGet(t, "/binflow/api/npm/npm-local/-/ping", nil)
		if status != http.StatusOK || strings.TrimSpace(string(body)) != "{}" {
			t.Fatalf("ping = %d %s", status, body)
		}
		resp, err := srv.Client().Get(srv.URL + "/binflow/api/npm/npm-local/-/v1/search?text=x")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("search = %d, want 404", resp.StatusCode)
		}
	})
}

// newClientStack assembles the production-shaped stack: real httpapi.Server
// with the generic+docker+npm adapters mounted, sqlite metadata, real storage
// and the npm-local repository seeded through repo.Service (the T-64 type
// matrix). It returns the server and the admin Basic auth header value.
func newClientStack(t *testing.T) (*httptest.Server, string) {
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

	admin := &auth.Principal{Name: "admin", Admin: true}
	if _, err := svc.CreateRepo(ctx, admin, &metadata.Repo{
		RepoKey: "npm-local", Type: repo.TypeLocal, PackageType: npm.Protocol,
		Description: "npm client suite",
	}); err != nil {
		t.Fatalf("create npm-local: %v", err)
	}

	npmHandler := npm.New(svc, md.Repos(), npm.Options{}).
		WithAuth(authSvc, md.Users(), authSvc).
		WithLedger(md.Blobs())
	// NOTE: no npm.Register here — the process-wide registry is pinned by the
	// in-package registration test, and both test packages link into one
	// binary (a second Register would panic by design). The httpapi mount
	// below goes through Deps.Adapters, the production assembly path.

	dockerHandler := docker.New(svc, docker.NewRepoLookup(md.Repos()), authSvc, authSvc, md.Users(),
		docker.Options{AnonymousAccess: cfg.Security.AnonymousAccess}, nil).
		WithStorage(st, md.Blobs())
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
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), dockerHandler, npmHandler},
		Version:   "client-suite",
		Revision:  "test",
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, base64.StdEncoding.EncodeToString([]byte("admin:password"))
}
