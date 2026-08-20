package httpapi_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/adapter/pypi"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// TestE26FullMatrix: every unmapped root path and every unimplemented
// /binflow/api endpoint answers 404 + the errors[] envelope with "not
// implemented" wording — never a 500, never an empty 200 (PRD E-26,
// C24).
//
// M2 reversal (PRD section 8.1 baseline item 1 / T-32 risk R10): /v2/**
// is the docker root-level exception (ADR-0010) and is NO LONGER part of
// this 404 matrix — TestV2RootException owns its assertions now. The
// /binflow/v2/** spelling stays in the matrix: ADR-0010 clause 2 declined
// the double mount, so it keeps answering the E-26 envelope 404.
func TestE26FullMatrix(t *testing.T) {
	h := newHarness(t)

	tests := []struct {
		path string
		hint bool // message must mention /binflow
	}{
		{"/artifactory/api/system/ping", true},
		{"/artifactory/libs-release-local/x.jar", true},
		{"/api/system/info", true},
		{"/whatever", true},
		{"/binflow/v2/", false},
		{"/binflow/v2/blobs/uploads", false},
		// T-69 R5 flip source: /binflow/api/npm/** ROUTES once the npm
		// handler is mounted (the T-63 seam + the npm adapter). This row
		// keeps asserting the UNMOUNTED posture — the default harness never
		// mounts protocol handlers — and TestE26NpmMountRouting (added by
		// T-69) pins the mounted behavior that supersedes it.
		{"/binflow/api/npm/xx", false},
		// T-70 R5 flip source: /binflow/api/pypi/** ROUTES once the pypi
		// handler is mounted (the T-63 seam + the pypi adapter). This row
		// keeps asserting the UNMOUNTED posture — the default harness never
		// mounts protocol handlers — and TestE26PyPIMountRouting (added by
		// T-70) pins the mounted behavior that supersedes it.
		{"/binflow/api/pypi/simple", false},
		// pypi-ui is a permanent E-26 resident (PRD PE-06/M58): the look-
		// alike prefix never mounts, mounted pypi handler or not.
		{"/binflow/api/pypi-ui/packages", false},
		// T-92 R5 flip (PRD section 5.6: "/binflow/api/search/** -> 404" is
		// superseded): /api/search/artifact and /api/search/checksum now
		// ROUTE (SR-01/SR-02), so the old matrix row for the artifact
		// entrance moved to TestSearchArtifactW14 (its parameterless
		// anonymous GET now answers the 400 of the missing name). The
		// UNIMPLEMENTED search family stays here forever (SR-04, W36) —
		// TestSearchUnimplementedFamilyW36 pins the authenticated posture.
		{"/binflow/api/search/props", false},
		{"/binflow/api/search/users", false},
		{"/binflow/api/search/artifactory", false},
		{"/binflow/api/search/pattern", false},
		{"/binflow/api/search/badge", false},
		{"/binflow/api/search/gavc", false},
		{"/binflow/api/replication", false},
		{"/binflow/api/system/info", false},
		{"/binflow/api/system/configuration", false},
		{"/binflow/api/builds", false},
		{"/binflow/api", false},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			resp := h.do(http.MethodGet, tc.path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			eb := decodeError(t, resp)
			if eb.Errors[0].Status != http.StatusNotFound {
				t.Fatalf("envelope status = %d", eb.Errors[0].Status)
			}
			msg := eb.Errors[0].Message
			if !strings.Contains(strings.ToLower(msg), "not implemented") &&
				!strings.Contains(msg, "/binflow") {
				t.Fatalf("message %q carries neither the not-implemented wording nor the prefix hint", msg)
			}
			if tc.hint && !strings.Contains(msg, "/binflow") {
				t.Fatalf("message %q missing the /binflow prefix hint", msg)
			}
		})
	}
}

// TestDotSegmentReachesAdapterUnnormalized is the routing core of
// FR-4-AC10: a raw request line with dot segments must reach the adapter
// layout verbatim and come back as the adapter's 400 — never as net/http's
// cleanPath 3xx redirect. The Go client refuses to send such a request
// line, so the probe goes over raw TCP (with Basic auth inline so the
// request passes the write gate and dies at the layout defense).
func TestDotSegmentReachesAdapterUnnormalized(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	for _, path := range []string{
		"/binflow/generic-local/a/../../etc/passwd",
		"/binflow/generic-local/a/./b",
	} {
		t.Run(path, func(t *testing.T) { probeRawPut(t, h, path) })
	}

	// A leading double slash escapes neither as a redirect nor a silent
	// rewrite: the empty first segment misses every repository row and
	// answers the envelope 404 (empty-segment rule, FR-4-AC11 defense at
	// the dispatch boundary).
	t.Run("/binflow//generic-local//x", func(t *testing.T) {
		conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()
		_, _ = fmt.Fprintf(conn, "PUT /binflow//generic-local//x HTTP/1.1\r\nHost: t\r\nAuthorization: Basic %s\r\nContent-Length: 1\r\nConnection: close\r\n\r\nz", base64Admin())
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			t.Fatalf("redirect %d to %q — normalization leaked", resp.StatusCode, resp.Header.Get("Location"))
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		decodeError(t, resp)
	})
}

// probeRawPut sends one un-normalized PUT over raw TCP with admin
// credentials and asserts the adapter layout's 400 (never a redirect).
func probeRawPut(t *testing.T, h *harness, path string) {
	t.Helper()
	conn, err := net.Dial("tcp", h.srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = fmt.Fprintf(conn, "PUT %s HTTP/1.1\r\nHost: t\r\nAuthorization: Basic %s\r\nContent-Length: 1\r\nConnection: close\r\n\r\nz",
		path, base64Admin())
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		t.Fatalf("redirect %d to %q — cleanPath normalization leaked in front of the adapter",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 from the adapter layout; body=%s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("400 body content-type = %q", ct)
	}
	decodeError(t, resp)
}

// base64Admin is the Basic header value for the seeded evaluation admin.
func base64Admin() string {
	return base64.StdEncoding.EncodeToString([]byte(adminUser + ":" + adminPass))
}

// TestEncodedDotSegmentReachesAdapter: the percent-encoded traversal
// variant (%2e%2e) survives the router untouched as well.
func TestEncodedDotSegmentReachesAdapter(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	for _, path := range []string{
		"/binflow/generic-local/acme/%2e%2e/%2e%2e/etc/passwd",
		"/binflow/generic-local/a/..%2fb",
	} {
		resp := h.do(http.MethodPut, path, adminUser, adminPass, []byte("z"), nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", path, resp.StatusCode, body)
		}
		if !strings.Contains(body, `"errors"`) {
			t.Fatalf("%s: body %q is not the envelope", path, body)
		}
	}
}

// TestContentRoundtripThroughRouter: upload and download through the full
// router (prefix strip + principal seam + adapter dispatch) with real
// bytes, proving the dispatch path preserves the body, the checksum
// headers and the 201/200 contract.
func TestContentRoundtripThroughRouter(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	content := []byte("the artifact body")
	sum := sha256Hex(content)

	resp := h.do(http.MethodPut, "/binflow/generic-local/acme/artifact.bin", adminUser, adminPass,
		content, map[string]string{"X-Checksum-Sha256": sum})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d; body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("X-Checksum-Sha256"); got != sum {
		t.Fatalf("response X-Checksum-Sha256 = %q, want %q", got, sum)
	}
	var created struct {
		Repo string `json:"repo"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("PUT body not FileInfo JSON: %v (%s)", err, body)
	}
	if created.Repo != "generic-local" || created.Path != "/acme/artifact.bin" {
		t.Fatalf("FileInfo repo/path = %q/%q", created.Repo, created.Path)
	}

	resp = h.do(http.MethodGet, "/binflow/generic-local/acme/artifact.bin", "", "", nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	if got != string(content) {
		t.Fatalf("GET body mismatch: %q", got)
	}
	if resp.Header.Get("X-Checksum-Sha256") != sum {
		t.Fatalf("GET X-Checksum-Sha256 = %q", resp.Header.Get("X-Checksum-Sha256"))
	}
}

// TestUnknownRepoIs404Envelope: a content path under a repo key with no
// repository row answers the spec-worded 404, envelope-shaped.
func TestUnknownRepoIs404Envelope(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/binflow/no-such-repo/x.bin", "", "", nil, nil)
	eb := decodeError(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if !strings.Contains(eb.Errors[0].Message, "Failed to find the repository 'no-such-repo'") {
		t.Fatalf("message = %q", eb.Errors[0].Message)
	}
}

// TestE26PyPIMountRouting pins the MOUNTED half of the E-26 pypi rows
// (T-70, R5 flip): once the pypi adapter is registered, /binflow/api/pypi/**
// routes through the T-63 seam onto the content plane — the bare
// not-implemented 404 is superseded by the ROUTED responses (an unknown
// repository keeps the spec's repo-not-found 404; a live pypi repository
// answers the adapter's own protocol semantics). The unmounted posture
// stays pinned by TestE26FullMatrix's rows on the default harness.
func TestE26PyPIMountRouting(t *testing.T) {
	s := newPyPiStack(t)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // substring of the envelope message ("" = unchecked)
	}{
		{
			"unknown repo under the mount routes to the content-plane 404",
			"/binflow/api/pypi/simple", http.StatusNotFound,
			"Failed to find the repository 'simple'",
		},
		{
			"live pypi repository answers protocol semantics, not E-26",
			"/binflow/api/pypi/pypi-local/simple/demo-pkg/", http.StatusNotFound,
			"project 'demo-pkg' not found",
		},
		{
			"pypi-ui never mounts even with the handler present (PE-06/M58)",
			"/binflow/api/pypi-ui/packages", http.StatusNotFound,
			"not implemented",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(http.MethodGet, tc.path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			eb := decodeError(t, resp)
			if tc.wantBody != "" && !strings.Contains(eb.Errors[0].Message, tc.wantBody) {
				t.Fatalf("message %q does not contain %q", eb.Errors[0].Message, tc.wantBody)
			}
		})
	}
}

// newPyPiStack builds a full real stack with the REAL pypi adapter mounted
// beside the generic one (the default harness cannot inject it: the pypi
// handler needs the repo.Service the harness assembles internally, so this
// mini-stack mirrors the harness composition).
func newPyPiStack(t *testing.T) *harness {
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
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)
	pypiHandler := pypi.New(svc, md.Repos(), md.Blobs(), st)
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "pypi-local", Type: repo.TypeLocal, PackageType: repo.PackagePypi,
	}); err != nil {
		t.Fatalf("seed pypi-local: %v", err)
	}

	ts := httptest.NewServer(httpapi.New(httpapi.Deps{
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
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), pypiHandler},
		Version:   "1.0.0-test",
	}, nil).Handler())
	t.Cleanup(ts.Close)

	return &harness{t: t, srv: ts, st: st, md: md, svc: svc, authSvc: authSvc}
}

// TestE26NpmMountRouting pins the MOUNTED half of the E-26 npm row
// (T-69, R5 flip): once the npm adapter is registered, /binflow/api/npm/**
// routes through the T-63 seam onto the content plane — the bare
// not-implemented 404 is superseded by the ROUTED responses (an unknown
// repository keeps the spec's repo-not-found 404; a live npm repository
// answers the adapter's own protocol semantics, and the NE-08 endpoints
// answer the adapter's strict 404). The unmounted posture stays pinned by
// TestE26FullMatrix's row on the default harness.
func TestE26NpmMountRouting(t *testing.T) {
	s := newNpmStack(t)

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantBody   string // substring of the envelope message ("" = unchecked)
	}{
		{
			"unknown repo under the mount routes to the content-plane 404",
			"/binflow/api/npm/xx", http.StatusNotFound,
			"Failed to find the repository 'xx'",
		},
		{
			"live npm repository answers protocol semantics, not E-26",
			"/binflow/api/npm/npm-local/-/ping", http.StatusOK,
			"",
		},
		{
			"NE-08 endpoints answer the adapter's strict 404 (M58)",
			"/binflow/api/npm/npm-local/-/v1/search?text=x", http.StatusNotFound,
			"not implemented",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.do(http.MethodGet, tc.path, "", "", nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusOK {
				return
			}
			eb := decodeError(t, resp)
			if tc.wantBody != "" && !strings.Contains(eb.Errors[0].Message, tc.wantBody) {
				t.Fatalf("message %q does not contain %q", eb.Errors[0].Message, tc.wantBody)
			}
		})
	}
}

// newNpmStack builds a full real stack with the REAL npm adapter mounted
// beside the generic one (the T-70 mini-stack pattern: the default harness
// cannot inject the handler, which needs the repo.Service the harness
// assembles internally, so this mirrors the harness composition).
func newNpmStack(t *testing.T) *harness {
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
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)
	npmHandler := npm.New(svc, md.Repos(), npm.Options{}).
		WithAuth(authSvc, md.Users(), authSvc).
		WithLedger(md.Blobs())
	if err := md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "npm-local", Type: repo.TypeLocal, PackageType: repo.PackageNpm,
	}); err != nil {
		t.Fatalf("seed npm-local: %v", err)
	}

	ts := httptest.NewServer(httpapi.New(httpapi.Deps{
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
		Version:   "1.0.0-test",
	}, nil).Handler())
	t.Cleanup(ts.Close)

	return &harness{t: t, srv: ts, st: st, md: md, svc: svc, authSvc: authSvc}
}
