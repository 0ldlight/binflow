package pypi

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// adminPass is the documented evaluation default seeded by metadata.Open
// when BINFLOW_ADMIN_PASSWORD is unset (ADR-0009); init clears the env so
// the default is deterministic regardless of the developer shell.
const (
	adminUser = "admin"
	adminPass = "password"
)

func init() { _ = os.Unsetenv("BINFLOW_ADMIN_PASSWORD") }

// stack is a real-stack test environment: sqlite metadata, the real storage
// engine, real repo.Service, the real auth chain and the real httpapi
// router, with the pypi adapter under test mounted beside the generic one —
// the same posture as internal/httpapi's harness, so every assertion runs
// through the full middleware chain (auth gates, the /binflow/api/pypi
// dispatch seam, prefix stripping) and never the bare handler.
type stack struct {
	t   *testing.T
	srv *httptest.Server
	st  storage.Engine
	md  metadata.Store
	svc repo.Service
}

// newStack builds the default stack (anonymous access on, one pypi local
// repository seeded).
func newStack(t *testing.T) *stack {
	t.Helper()
	s := newStackCfg(t, nil)
	s.seedRepo(t, "pypi-local", repo.TypeLocal, repo.PackagePypi)
	return s
}

// newStackCfg builds the stack; mutate adjusts the config before assembly.
func newStackCfg(t *testing.T, mutate func(*config.Config)) *stack {
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
	if mutate != nil {
		mutate(cfg)
	}

	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)
	pypiHandler := New(svc, md.Repos(), md.Blobs(), st)

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
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), pypiHandler},
		Version:   "1.0.0-test",
		Revision:  "t70",
	}, nil)

	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &stack{t: t, srv: ts, st: st, md: md, svc: svc}
}

// seedRepo writes a repository row directly through the metadata store
// (repo.Service's validation matrix is not under test here; the adapter
// only reads the row's class and package type).
func (s *stack) seedRepo(t *testing.T, key, class, packageType string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: class, PackageType: packageType,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// do issues one request; user != "" adds Basic auth; body may be nil; hdr
// may be nil. The response body is NOT consumed. Redirects are followed
// (the production clients do); use doRaw for redirect-semantics probes.
func (s *stack) do(method, path, user, pass string, body io.Reader, hdr map[string]string) *http.Response {
	s.t.Helper()
	return s.doClient(s.srv.Client(), method, path, user, pass, body, hdr)
}

// doRaw issues the request with a client that does NOT follow redirects,
// so 3xx semantics (the simple index's trailing-slash 302) are observable.
func (s *stack) doRaw(method, path, user, pass string, body io.Reader, hdr map[string]string) *http.Response {
	s.t.Helper()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return s.doClient(noFollow, method, path, user, pass, body, hdr)
}

// doClient issues the request through the given client.
func (s *stack) doClient(client *http.Client, method, path, user, pass string, body io.Reader, hdr map[string]string) *http.Response {
	s.t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, body)
	if err != nil {
		s.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		s.t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// get fetches a path anonymously and returns status + body.
func (s *stack) get(path string) (int, string, http.Header) {
	s.t.Helper()
	resp := s.do(http.MethodGet, path, "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatalf("read body %s: %v", path, err)
	}
	return resp.StatusCode, string(b), resp.Header
}

// upload posts a twine-shaped multipart form. fields are written in sorted
// key order for determinism; filename == "" omits the content part.
func (s *stack) upload(path string, fields map[string]string, filename string, content []byte) (int, string) {
	s.t.Helper()
	body, ct := multipartForm(s.t, fields, filename, content, false)
	resp := s.do(http.MethodPost, path, adminUser, adminPass, body, map[string]string{"Content-Type": ct})
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatalf("read upload response: %v", err)
	}
	return resp.StatusCode, string(b)
}

// uploadOK performs the canonical successful upload (file_upload action,
// twine-shaped fields, no digests) and fatals unless the server answers
// 200 — the fixture every index/download test starts from.
func (s *stack) uploadOK(t *testing.T, name, version, filename string, content []byte) {
	t.Helper()
	status, body := s.upload("/binflow/api/pypi/pypi-local", map[string]string{
		":action":          "file_upload",
		"protocol_version": "1",
		"name":             name,
		"version":          version,
		"filetype":         "bdist_wheel",
		"summary":          "T-70 fixture",
	}, filename, content)
	if status != http.StatusOK {
		t.Fatalf("fixture upload %s/%s/%s: status %d, body %s", name, version, filename, status, body)
	}
}

// multipartForm builds a multipart/form-data body. contentFirst puts the
// content part BEFORE the value fields (the order-agnostic probe).
func multipartForm(t *testing.T, fields map[string]string, filename string, content []byte, contentFirst bool) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	writeContent := func() {
		if filename == "" {
			return
		}
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", formDataDisposition("content", filename))
		if len(content) > 0 && strings.HasSuffix(filename, ".whl") {
			hdr.Set("Content-Type", "application/zip")
		}
		pw, err := w.CreatePart(hdr)
		if err != nil {
			t.Fatalf("create content part: %v", err)
		}
		if _, err := pw.Write(content); err != nil {
			t.Fatalf("write content part: %v", err)
		}
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	if contentFirst {
		writeContent()
	}
	for _, k := range keys {
		if err := w.WriteField(k, fields[k]); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	if !contentFirst {
		writeContent()
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return &buf, w.FormDataContentType()
}

// formDataDisposition renders a Content-Disposition value for one form
// part, quoting the parameters and backslash-escaping embedded quotes the
// way requests/twine do (RFC 2231 quoted-string form).
func formDataDisposition(field, filename string) string {
	var b strings.Builder
	b.WriteString(`form-data; name="`)
	b.WriteString(strings.ReplaceAll(field, `"`, `\"`))
	b.WriteString(`"; filename="`)
	b.WriteString(strings.ReplaceAll(filename, `"`, `\"`))
	b.WriteString(`"`)
	return b.String()
}
