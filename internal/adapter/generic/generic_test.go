package generic_test

import (
	"context"
	"crypto/md5"  //nolint:gosec // fixture digest expectation only
	"crypto/sha1" //nolint:gosec // fixture digest expectation only
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- harness: real service stack, no mocks below repo.Service ----

type env struct {
	svc     repo.Service
	handler *generic.Handler
	st      storage.Engine
	md      metadata.Store
	srv     *httptest.Server
	clk     *clock
	anon    bool
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time  { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) RFC3339() string { return c.Now().Format(time.RFC3339) }

func newEnv(t *testing.T) *env {
	t.Helper()
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	clk := &clock{now: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	az := &allowAll{}
	svc := repo.NewWithClock(st, md, az, nil, clk.Now)
	h := generic.NewWithClock(svc, md.Blobs(), clk.RFC3339)
	if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate the T-14 contract: /binflow prefix stripped, principal
		// injected (admin for this harness; anonymous variants set env.anon).
		rel := strings.TrimPrefix(r.URL.Path, "/binflow")
		r2 := r.Clone(adapter.WithPrincipal(r.Context(), principalFor(t, envOf(r))))
		r2.URL.Path = rel
		h.ServeHTTP(w, r2)
	}))
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
		_ = md.Close()
	})
	return &env{svc: svc, handler: h, st: st, md: md, srv: srv, clk: clk}
}

// principalFor resolves the harness's principal mode from the request
// context (the env pointer rides the original request through a package
// var; simpler: admin always, anonymous tests use plain http.Get).
func principalFor(t *testing.T, e *env) *repo.Principal {
	t.Helper()
	if e == nil || e.anon {
		return nil
	}
	return admin()
}

// envOf fishes the harness env back out of the request; the httptest
// closure always has it, requests carry no marker.
var curEnv struct {
	mu sync.Mutex
	e  *env
}

func envOf(_ *http.Request) *env {
	curEnv.mu.Lock()
	defer curEnv.mu.Unlock()
	return curEnv.e
}

func admin() *repo.Principal { return &repo.Principal{Name: "admin", Admin: true} }

// allowAll authorizes everything (the handler under test is not the ACL).
type allowAll struct{}

func (allowAll) Can(context.Context, *repo.Principal, string, string, string) bool { return true }

// do runs one request against the harness with the admin principal.
func (e *env) do(t *testing.T, method, path string, body io.Reader, hdr map[string]string) *http.Response {
	t.Helper()
	curEnv.mu.Lock()
	curEnv.e = e
	curEnv.mu.Unlock()
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func digestsOf(content string) (sha256S, sha1S, md5S string) {
	a := sha256.Sum256([]byte(content))
	b := sha1.Sum([]byte(content)) //nolint:gosec // fixture expectation
	c := md5.Sum([]byte(content))  //nolint:gosec // fixture expectation
	return hex.EncodeToString(a[:]), hex.EncodeToString(b[:]), hex.EncodeToString(c[:])
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close() //nolint:errcheck // read-only probe, nothing to act on
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// ---- PUT ----

func TestPutCreatedShape(t *testing.T) {
	e := newEnv(t)
	content := "hello binflow"
	sha, sha1v, md5v := digestsOf(content)
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/acme/artifact.bin",
		strings.NewReader(content), map[string]string{"Content-Type": "application/x-bin"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT status = %d, body=%s", resp.StatusCode, body(t, resp))
	}
	if got := resp.Header.Get("X-Checksum-Sha256"); got != sha {
		t.Fatalf("X-Checksum-Sha256 = %q, want %q", got, sha)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasSuffix(loc, "/generic-local/acme/artifact.bin") {
		t.Fatalf("Location = %q", loc)
	}
	var fi fileInfoJSON
	if err := json.Unmarshal([]byte(body(t, resp)), &fi); err != nil {
		t.Fatalf("FileInfo JSON: %v", err)
	}
	if fi.Repo != "generic-local" || fi.Path != "/acme/artifact.bin" {
		t.Fatalf("repo/path = %q/%q", fi.Repo, fi.Path)
	}
	if fi.Size != fmt.Sprint(len(content)) {
		t.Fatalf("size = %q (must be a string of the byte count)", fi.Size)
	}
	if fi.CreatedBy != "admin" {
		t.Fatalf("createdBy = %q", fi.CreatedBy)
	}
	if fi.Checksums.Sha256 != sha || fi.Checksums.Sha1 != sha1v || fi.Checksums.Md5 != md5v {
		t.Fatalf("checksums = %+v", fi.Checksums)
	}
	if fi.MimeType != "application/x-bin" {
		t.Fatalf("mimeType = %q", fi.MimeType)
	}
	if !strings.Contains(fi.Created, ".") || !strings.Contains(fi.Created, "Z") && !strings.Contains(fi.Created, "+") {
		t.Fatalf("created timestamp %q lacks millis/zone", fi.Created)
	}
}

type fileInfoJSON struct {
	URI         string `json:"uri"`
	DownloadURI string `json:"downloadUri"`
	Repo        string `json:"repo"`
	Path        string `json:"path"`
	Created     string `json:"created"`
	CreatedBy   string `json:"createdBy"`
	Size        string `json:"size"`
	MimeType    string `json:"mimeType"`
	Checksums   struct {
		Sha1   string `json:"sha1"`
		Md5    string `json:"md5"`
		Sha256 string `json:"sha256"`
	} `json:"checksums"`
	OriginalChecksums struct {
		Sha1   string `json:"sha1"`
		Md5    string `json:"md5"`
		Sha256 string `json:"sha256"`
	} `json:"originalChecksums"`
}

// ---- table: statuses ----

func TestContentVerbsTable(t *testing.T) {
	e := newEnv(t)
	content := "roundtrip body"
	sha, _, _ := digestsOf(content)
	badSha := strings.Repeat("0", 64)
	missSha := strings.Repeat("f", 64)

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		hdr       map[string]string
		want      int
		wantInMsg string
	}{
		{name: "upload", method: http.MethodPut, path: "/binflow/generic-local/acme/a.bin", body: content, want: 201},
		{name: "upload with matching checksum", method: http.MethodPut, path: "/binflow/generic-local/acme/b.bin", body: content,
			hdr: map[string]string{"X-Checksum-Sha256": sha}, want: 201},
		{name: "upload checksum mismatch 409", method: http.MethodPut, path: "/binflow/generic-local/acme/bad.bin", body: content,
			hdr: map[string]string{"X-Checksum-Sha256": badSha}, want: 409, wantInMsg: "received"},
		{name: "upload malformed checksum 400", method: http.MethodPut, path: "/binflow/generic-local/acme/mal.bin", body: content,
			hdr: map[string]string{"X-Checksum-Sha256": "zz"}, want: 400},
		{name: "sha1 mismatch 409", method: http.MethodPut, path: "/binflow/generic-local/acme/s1.bin", body: content,
			hdr: map[string]string{"X-Checksum-Sha1": strings.Repeat("0", 40)}, want: 409},
		{name: "md5 mismatch 409", method: http.MethodPut, path: "/binflow/generic-local/acme/m5.bin", body: content,
			hdr: map[string]string{"X-Checksum-Md5": strings.Repeat("0", 32)}, want: 409},
		{name: "get missing 404 json", method: http.MethodGet, path: "/binflow/generic-local/acme/nope.bin", want: 404,
			wantInMsg: "Failed to find the requested resource"},
		{name: "head missing 404 json", method: http.MethodHead, path: "/binflow/generic-local/acme/nope.bin", want: 404}, // HEAD carries no body; envelope asserted via GET
		{name: "delete missing 404", method: http.MethodDelete, path: "/binflow/generic-local/acme/nope.bin", want: 404,
			wantInMsg: "Could not locate artifact"},
		{name: "repo missing 404", method: http.MethodGet, path: "/binflow/no-such-repo/a.bin", want: 404},
		{name: "mkdir trailing slash", method: http.MethodPut, path: "/binflow/generic-local/acme/", want: 201},
		{name: "mkdir with body 400", method: http.MethodPut, path: "/binflow/generic-local/x/", body: "junk", want: 400},
		{name: "checksum deploy hit", method: http.MethodPut, path: "/binflow/generic-local/acme/copy.bin",
			hdr: map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": sha}, want: 201},
		{name: "checksum deploy miss", method: http.MethodPut, path: "/binflow/generic-local/acme/miss.bin",
			hdr: map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": missSha}, want: 404},
		{name: "checksum deploy no header", method: http.MethodPut, path: "/binflow/generic-local/acme/nohdr.bin",
			hdr: map[string]string{"X-Checksum-Deploy": "true"}, want: 400},
		{name: "checksum deploy malformed", method: http.MethodPut, path: "/binflow/generic-local/acme/malformed.bin",
			hdr: map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": "abc"}, want: 404},
		{name: "explode archive rejected", method: http.MethodPut, path: "/binflow/generic-local/acme/z.bin", body: content,
			hdr: map[string]string{"X-Explode-Archive": "true"}, want: 400},
		{name: "old metadata notation properties", method: http.MethodPut, path: "/binflow/generic-local/acme/a.bin:properties", body: content, want: 409},
		{name: "old metadata notation statistics", method: http.MethodPut, path: "/binflow/generic-local/acme/a.bin:statistics", body: content, want: 409},
		{name: "colon in ordinary filename is legal", method: http.MethodPut, path: "/binflow/generic-local/acme/we:ird.bin", body: content, want: 201},
		{name: "dot escape 400", method: http.MethodPut, path: "/binflow/generic-local/a/../../etc/passwd", body: "x", want: 400},
		{name: "double slash 400", method: http.MethodGet, path: "/binflow/generic-local/a//b", want: 400},
		{name: "dot segment 400", method: http.MethodGet, path: "/binflow/generic-local/a/./b", want: 400},
		{name: "method not allowed", method: http.MethodPost, path: "/binflow/generic-local/a.bin", body: "x", want: 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rdr io.Reader
			if tt.body != "" {
				rdr = strings.NewReader(tt.body)
			}
			resp := e.do(t, tt.method, tt.path, rdr, tt.hdr)
			got := body(t, resp)
			if resp.StatusCode != tt.want {
				t.Fatalf("%s %s = %d, want %d, body=%s", tt.method, tt.path, resp.StatusCode, tt.want, got)
			}
			if tt.wantInMsg != "" && !strings.Contains(got, tt.wantInMsg) {
				t.Fatalf("body %q lacks %q", got, tt.wantInMsg)
			}
			if resp.StatusCode >= 400 {
				if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
					t.Fatalf("error Content-Type = %q, want JSON envelope", ct)
				}
				if tt.method == http.MethodHead {
					return // HEAD carries no body; envelope asserted via the GET twin
				}
				var envl struct {
					Errors []struct {
						Status  int    `json:"status"`
						Message string `json:"message"`
					} `json:"errors"`
				}
				if err := json.Unmarshal([]byte(got), &envl); err != nil {
					t.Fatalf("error body is not the errors[] envelope: %v (%s)", err, got)
				}
				if len(envl.Errors) == 0 || envl.Errors[0].Status != tt.want {
					t.Fatalf("envelope = %+v", envl)
				}
			}
		})
	}
}

// ---- upload-context originalChecksums + folder sentinel (T-13 review m1/m4) ----

func TestUploadResponseShapes(t *testing.T) {
	e := newEnv(t)

	t.Run("zero declared digests renders empty originalChecksums", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/acme/undecl.bin",
			strings.NewReader("no headers at all"), nil)
		raw := body(t, resp)
		if resp.StatusCode != 201 {
			t.Fatalf("put = %d: %s", resp.StatusCode, raw)
		}
		var fi struct {
			Checksums struct {
				Sha256 string `json:"sha256"`
			} `json:"checksums"`
			OriginalChecksums map[string]any `json:"originalChecksums"`
		}
		if err := json.Unmarshal([]byte(raw), &fi); err != nil {
			t.Fatalf("json: %v (%s)", err, raw)
		}
		if fi.Checksums.Sha256 == "" {
			t.Fatal("server-side checksums missing")
		}
		// Upload context with zero declarations: the echo must be an EMPTY
		// object, not a fallback to the stored triple.
		if len(fi.OriginalChecksums) != 0 {
			t.Fatalf("originalChecksums = %v, want empty object on zero declarations", fi.OriginalChecksums)
		}
	})

	t.Run("declared digests echo exactly those algorithms", func(t *testing.T) {
		content := "declared subset"
		sha, _, md5v := digestsOf(content)
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/acme/decl.bin",
			strings.NewReader(content), map[string]string{
				"X-Checksum-Sha256": sha,
				"X-Checksum-Md5":    md5v,
			})
		raw := body(t, resp)
		if resp.StatusCode != 201 {
			t.Fatalf("put = %d: %s", resp.StatusCode, raw)
		}
		var fi struct {
			OriginalChecksums struct {
				Sha1   string `json:"sha1"`
				Sha256 string `json:"sha256"`
				Md5    string `json:"md5"`
			} `json:"originalChecksums"`
		}
		if err := json.Unmarshal([]byte(raw), &fi); err != nil {
			t.Fatalf("json: %v", err)
		}
		if fi.OriginalChecksums.Sha1 != "" {
			t.Fatalf("sha1 echoed though not declared: %+v", fi.OriginalChecksums)
		}
		if fi.OriginalChecksums.Sha256 != sha || fi.OriginalChecksums.Md5 != md5v {
			t.Fatalf("declared digests not echoed: %+v", fi.OriginalChecksums)
		}
	})

	t.Run("folder item carries no checksum objects", func(t *testing.T) {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/dir/", nil, nil)
		raw := body(t, resp)
		if resp.StatusCode != 201 {
			t.Fatalf("mkdir = %d: %s", resp.StatusCode, raw)
		}
		if strings.Contains(raw, "checksums") {
			t.Fatalf("folder body leaks checksum objects (emptyFolderSHA sentinel): %s", raw)
		}
		if strings.Contains(raw, strings.Repeat("0", 64)) {
			t.Fatalf("folder body leaks the all-zero sentinel: %s", raw)
		}
	})
}

// ---- GET/HEAD headers ----

func TestGetHeadHeaders(t *testing.T) {
	e := newEnv(t)
	content := "header fixture"
	sha, sha1v, md5v := digestsOf(content)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/acme/h.bin", strings.NewReader(content), nil); resp.StatusCode != 201 {
		t.Fatalf("put: %d", resp.StatusCode)
	}

	t.Run("GET headers", func(t *testing.T) {
		resp := e.do(t, http.MethodGet, "/binflow/generic-local/acme/h.bin", nil, nil)
		got := body(t, resp)
		if resp.StatusCode != 200 || got != content {
			t.Fatalf("GET = %d %q", resp.StatusCode, got)
		}
		checkHeader(t, resp, "X-Checksum-Sha256", sha)
		checkHeader(t, resp, "X-Checksum-Sha1", sha1v)
		checkHeader(t, resp, "X-Checksum-Md5", md5v)
		checkHeader(t, resp, "ETag", sha1v)
		checkHeader(t, resp, "Accept-Ranges", "bytes")
		checkHeader(t, resp, "Content-Type", "application/octet-stream")
		if resp.Header.Get("Last-Modified") == "" {
			t.Fatal("Last-Modified missing")
		}
		if cl := resp.Header.Get("Content-Length"); cl != fmt.Sprint(len(content)) {
			t.Fatalf("Content-Length = %q", cl)
		}
	})

	t.Run("HEAD headers", func(t *testing.T) {
		resp := e.do(t, http.MethodHead, "/binflow/generic-local/acme/h.bin", nil, nil)
		defer resp.Body.Close() //nolint:errcheck // read-only probe
		if resp.StatusCode != 200 {
			t.Fatalf("HEAD = %d", resp.StatusCode)
		}
		checkHeader(t, resp, "X-Checksum-Sha256", sha)
		if cl := resp.Header.Get("Content-Length"); cl != fmt.Sprint(len(content)) {
			t.Fatalf("Content-Length = %q", cl)
		}
	})

	t.Run("anonymous GET 200 (simulated T-14 semantics)", func(t *testing.T) {
		e.anon = true
		defer func() { e.anon = false }()
		resp := e.do(t, http.MethodGet, "/binflow/generic-local/acme/h.bin", nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("anonymous GET = %d, want 200", resp.StatusCode)
		}
		body(t, resp)
	})
}

func checkHeader(t *testing.T, resp *http.Response, name, want string) {
	t.Helper()
	if got := resp.Header.Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}

// ---- DELETE idempotency ----

func TestDeleteIdempotent(t *testing.T) {
	e := newEnv(t)
	path := "/binflow/generic-local/acme/del.bin"
	if resp := e.do(t, http.MethodPut, path, strings.NewReader("x"), nil); resp.StatusCode != 201 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	resp := e.do(t, http.MethodDelete, path, nil, nil)
	got := body(t, resp)
	if resp.StatusCode != 204 || got != "" {
		t.Fatalf("DELETE = %d %q, want 204 empty", resp.StatusCode, got)
	}
	if resp := e.do(t, http.MethodDelete, path, nil, nil); resp.StatusCode != 404 {
		t.Fatalf("repeat DELETE = %d, want 404", resp.StatusCode)
	}
	if resp := e.do(t, http.MethodGet, path, nil, nil); resp.StatusCode != 404 {
		body(t, resp)
		t.Fatalf("GET after delete = %d, want 404", resp.StatusCode)
	}
}

// ---- checksum-deploy reuses content: no transfer, node serves ----

func TestChecksumDeployZeroTransfer(t *testing.T) {
	e := newEnv(t)
	content := "deployable"
	sha, _, _ := digestsOf(content)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/src/one.bin", strings.NewReader(content), nil); resp.StatusCode != 201 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/dst/copy.bin", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": sha})
	if resp.StatusCode != 201 {
		t.Fatalf("checksum deploy = %d, body=%s", resp.StatusCode, body(t, resp))
	}
	got := e.do(t, http.MethodGet, "/binflow/generic-local/dst/copy.bin", nil, nil)
	if b := body(t, got); got.StatusCode != 200 || b != content {
		t.Fatalf("deployed copy = %d %q", got.StatusCode, b)
	}
}

// ---- folder GET on content path: 404-shaped ----

func TestFolderGetOnContentPath(t *testing.T) {
	e := newEnv(t)
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/dir/", nil, nil); resp.StatusCode != 201 {
		t.Fatalf("mkdir: %d %s", resp.StatusCode, body(t, resp))
	}
	// folder node itself: no body on the content path
	resp := e.do(t, http.MethodGet, "/binflow/generic-local/dir/", nil, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("folder GET = %d, want 404 (content path serves files only)", resp.StatusCode)
	}
	body(t, resp)
	// folder delete recurses
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/dir/inner.bin", strings.NewReader("x"), nil); resp.StatusCode != 201 {
		t.Fatalf("inner put: %d", resp.StatusCode)
	}
	if resp := e.do(t, http.MethodDelete, "/binflow/generic-local/dir/", nil, nil); resp.StatusCode != 204 {
		body(t, resp)
		t.Fatalf("folder delete = %d", resp.StatusCode)
	}
	if resp := e.do(t, http.MethodGet, "/binflow/generic-local/dir/inner.bin", nil, nil); resp.StatusCode != 404 {
		body(t, resp)
		t.Fatal("inner file survived folder delete")
	}
}
