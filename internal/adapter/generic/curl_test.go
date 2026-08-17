package generic_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The curl black-box suite reproduces the PRD's acceptance commands against
// a real httptest server: C07/C08/C09/C13/C14/C15a/C15b/C16/C18/C23 plus
// the slow-upload kill (FR-2-AC3, package-level reproduction). curl is the
// "real client" for the generic protocol; skipping is honest when the
// binary is unavailable (CI images must install curl).

func curlPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not available on PATH; generic protocol client test skipped")
	}
	return p
}

// curlServer mounts the handler behind a /binflow-stripping mux with the
// admin principal injected (the T-14 middleware contract simulated; the
// handler itself never authenticates).
func curlServer(t *testing.T) (*httptest.Server, *storage.Engine) {
	t.Helper()
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	md, err := metadata.Open(context.Background(), metadata.Options{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "binflow.db")})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	svc := repo.New(st, md, allowAll{}, nil)
	if _, err := svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	h := generic.New(svc, md.Blobs(), st.Open)
	// NOTE: no ServeMux — it normalizes dot-segments with a 3xx redirect
	// before the handler ever sees them, which would hide the adapter's own
	// 400 defense from this black-box test. A bare handler mount is exactly
	// what T-14 will do (mux routes, adapter handles).
	mount := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/binflow")
		r2 := r.Clone(adapter.WithPrincipal(r.Context(), admin()))
		r2.URL.Path = rel
		h.ServeHTTP(w, r2)
	})
	srv := httptest.NewServer(mount)
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
		_ = md.Close()
	})
	return srv, &st
}

// curl runs the real curl binary and returns (stdout+stderr, exit code).
func curl(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(curlPath(t), args...) //nolint:gosec // test-only client invocation
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	code := 0
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("curl run: %v (%s)", err, out)
	}
	return string(out), code
}

func shasum(t *testing.T, dir, file string) string {
	t.Helper()
	cmd := exec.Command("shasum", "-a", "256", "--", file) //nolint:gosec // fixture under t.TempDir
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shasum: %v (%s)", err, out)
	}
	return strings.Fields(string(out))[0]
}

func TestCurlRoundtrip(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	srv, _ := curlServer(t)
	base := srv.URL + "/binflow"
	dir := t.TempDir()

	// Fixture: 256 KiB of urandom (big enough for --limit-rate, small
	// enough to keep the suite fast).
	fixture := filepath.Join(dir, "artifact.bin")
	if err := exec.Command("dd", "if=/dev/urandom", "of="+fixture, "bs=1024", "count=256").Run(); err != nil {
		t.Fatalf("dd: %v", err)
	}
	sha := shasum(t, dir, "artifact.bin")

	type step struct {
		name string
		run  func(t *testing.T)
	}
	steps := []step{
		{"C07 upload + server checksum echo", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-T", "artifact.bin",
				"-w", "\n%{http_code}", base+"/generic-local/acme/artifact.bin")
			if !strings.HasSuffix(out, "\n201") {
				t.Fatalf("C07: %q", out)
			}
			var fi struct {
				Checksums struct {
					Sha256 string `json:"sha256"`
				} `json:"checksums"`
				CreatedBy string `json:"createdBy"`
				Size      string `json:"size"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n201")), &fi); err != nil {
				t.Fatalf("C07 body: %v (%s)", err, out)
			}
			if fi.Checksums.Sha256 != sha {
				t.Fatalf("C07 checksum %q != %q", fi.Checksums.Sha256, sha)
			}
			if fi.CreatedBy != "admin" || fi.Size == "" {
				t.Fatalf("C07 createdBy/size = %q/%q", fi.CreatedBy, fi.Size)
			}
		}},
		{"C08 download + verify", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-o", "dl.bin",
				"-D", "dl.headers", base+"/generic-local/acme/artifact.bin")
			if info, err := os.Stat(filepath.Join(dir, "dl.bin")); err != nil || info.Size() == 0 {
				t.Fatalf("C08 dl.bin missing/empty (stat=%v); curl=%s", err, out)
			}
			if got := shasum(t, dir, "dl.bin"); got != sha {
				t.Fatalf("C08 sha mismatch: %s", got)
			}
			hdr, _ := os.ReadFile(filepath.Join(dir, "dl.headers"))
			hs := strings.ToLower(string(hdr))
			for _, want := range []string{"x-checksum-sha256:", "x-checksum-sha1:", "x-checksum-md5:", "etag:", "last-modified:", "accept-ranges: bytes"} {
				if !strings.Contains(hs, want) {
					t.Fatalf("C08 headers lack %q:\n%s", want, hs)
				}
			}
			if !strings.Contains(hs, "x-checksum-sha256: "+sha) {
				t.Fatalf("C08 X-Checksum-Sha256 wrong:\n%s", hs)
			}
		}},
		{"C09 HEAD", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-I", "-u", "admin:pw", base+"/generic-local/acme/artifact.bin")
			if !strings.Contains(out, "X-Checksum-Sha256: "+sha) {
				t.Fatalf("C09 HEAD headers:\n%s", out)
			}
			if !strings.Contains(out, "Content-Length: 262144") {
				t.Fatalf("C09 Content-Length:\n%s", out)
			}
		}},
		{"C13 matching client checksum 201", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-T", "artifact.bin",
				"-H", "X-Checksum-Sha256: "+sha, "-o", "/dev/null", "-w", "%{http_code}",
				base+"/generic-local/acme/v2.bin")
			if out != "201" {
				t.Fatalf("C13 = %s", out)
			}
		}},
		{"C14 mismatched checksum 409 with received/actual", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-T", "artifact.bin",
				"-H", "X-Checksum-Sha256: "+strings.Repeat("0", 64),
				"-w", "\n%{http_code}", base+"/generic-local/acme/bad.bin")
			if !strings.HasSuffix(out, "\n409") {
				t.Fatalf("C14 status: %q", out)
			}
			if !strings.Contains(out, "received") || !strings.Contains(out, "actual") {
				t.Fatalf("C14 message lacks received/actual: %q", out)
			}
			// The node must not exist after the rejection.
			out2, _ := curl(t, dir, "-s", "-o", "/dev/null", "-w", "%{http_code}",
				"-u", "admin:pw", base+"/generic-local/acme/bad.bin")
			if out2 != "404" {
				t.Fatalf("C14 leftover node: %s", out2)
			}
		}},
		{"C15a checksum deploy hit 201", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-X", "PUT",
				"-H", "X-Checksum-Deploy: true", "-H", "X-Checksum-Sha256: "+sha,
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/acme/copy.bin")
			if out != "201" {
				t.Fatalf("C15a = %s", out)
			}
			out2, _ := curl(t, dir, "-s", "-o", "copy.bin", "-w", "%{http_code}",
				"-u", "admin:pw", base+"/generic-local/acme/copy.bin")
			if out2 != "200" {
				t.Fatalf("C15a download = %s", out2)
			}
			if shasum(t, dir, "copy.bin") != sha {
				t.Fatalf("C15a copy sha mismatch: %s", shasum(t, dir, "copy.bin"))
			}
		}},
		{"C15b checksum deploy miss 404", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-X", "PUT",
				"-H", "X-Checksum-Deploy: true", "-H", "X-Checksum-Sha256: "+strings.Repeat("f", 64),
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/acme/miss.bin")
			if out != "404" {
				t.Fatalf("C15b = %s", out)
			}
		}},
		{"C16 mkdir trailing slash 201", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-X", "PUT",
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/acme/")
			if out != "201" {
				t.Fatalf("C16 = %s", out)
			}
		}},
		{"C18 delete 204 then idempotent 404", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-u", "admin:pw", "-X", "DELETE",
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/acme/artifact.bin")
			if out != "204" {
				t.Fatalf("C18 first = %s", out)
			}
			out2, _ := curl(t, dir, "-s", "-u", "admin:pw", "-X", "DELETE",
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/acme/artifact.bin")
			if out2 != "404" {
				t.Fatalf("C18 repeat = %s", out2)
			}
			out3, _ := curl(t, dir, "-s", "-o", "/dev/null", "-w", "%{http_code}",
				"-u", "admin:pw", base+"/generic-local/acme/artifact.bin")
			if out3 != "404" {
				t.Fatalf("C18 GET after delete = %s", out3)
			}
		}},
		{"C23 anonymous GET 200 (no Authorization header)", func(t *testing.T) {
			out, _ := curl(t, dir, "-s", "-o", "anon.bin", "-w", "%{http_code}",
				base+"/generic-local/acme/v2.bin")
			if out != "200" {
				t.Fatalf("C23 = %s", out)
			}
			got := shasum(t, dir, "anon.bin")
			if got != sha {
				t.Fatalf("C23 content mismatch: %s != %s", got, sha)
			}
		}},
		{"path traversal 400 (raw and %2e%2e)", func(t *testing.T) {
			for _, p := range []string{
				"/generic-local/a/../../etc/passwd",
				"/generic-local/a/%2e%2e/%2e%2e/etc/passwd",
				"/generic-local/%2E%2E/secret",
			} {
				out, _ := curl(t, dir, "-s", "--path-as-is", "-u", "admin:pw", "-T", "artifact.bin",
					"-o", "/dev/null", "-w", "%{http_code}", base+p)
				if out != "400" {
					t.Fatalf("traversal %s = %s, want 400", p, out)
				}
			}
		}},
		{"oversize path 400", func(t *testing.T) {
			long := strings.Repeat("a", 600)
			out, _ := curl(t, dir, "-s", "--path-as-is", "-u", "admin:pw",
				"-o", "/dev/null", "-w", "%{http_code}", base+"/generic-local/"+long+".bin")
			if out != "400" {
				t.Fatalf("oversize = %s", out)
			}
		}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			s.run(t)
		})
	}
}

// TestCurlSlowUploadInterrupted reproduces FR-2-AC3 at the package level:
// a rate-limited upload killed mid-flight must leave no visible node.
// The kill is driven from Go (context deadline -> Process.Kill), the
// curl(1) --limit-rate flag does the slowing — equivalent to the PRD's
// `timeout -s KILL 3 curl ... --limit-rate 64k` without needing GNU
// timeout(1) (macOS ships none).
func TestCurlSlowUploadInterrupted(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available")
	}
	srv, _ := curlServer(t)
	base := srv.URL + "/binflow"
	dir := t.TempDir()
	big := filepath.Join(dir, "big.bin")
	if err := exec.Command("dd", "if=/dev/urandom", "of="+big, "bs=1024", "count=512").Run(); err != nil {
		t.Fatalf("dd: %v", err)
	}

	// 512 KiB at 64 KiB/s takes ~8s; kill at 2s — well before Commit.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, curlPath(t),
		"-s", "-u", "admin:pw", "-T", "big.bin", "--limit-rate", "64k",
		base+"/generic-local/acme/big.bin")
	cmd.Dir = dir
	cmd.Cancel = func() error { return cmd.Process.Kill() } // KILL, not TERM: no graceful finish
	_ = cmd.Run()                                           // non-zero exit is the expected outcome

	// The artifact must NOT be visible: no node, hence 404.
	out, _ := curl(t, dir, "-s", "-o", "/dev/null", "-w", "%{http_code}",
		"-u", "admin:pw", base+"/generic-local/acme/big.bin")
	if out != "404" {
		t.Fatalf("interrupted upload visible: GET = %s, want 404", out)
	}
}

// TestCurlAnonymousWriteChallenged: the handler itself performs no
// authentication — with a nil principal the service's own write gate is
// what answers. The 401 challenge here proves the anonymous write path
// fails closed even without T-14's middleware in front.
func TestCurlAnonymousWriteChallenged(t *testing.T) {
	st, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	md, err := metadata.Open(context.Background(), metadata.Options{Driver: "sqlite", Path: filepath.Join(t.TempDir(), "b.db")})
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	// No authorizer: nil principal must fail closed for writes.
	svc := repo.New(st, md, nil, nil)
	if _, err := svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	h := generic.New(svc, md.Blobs(), st.Open)
	mux := http.NewServeMux()
	mux.HandleFunc("/binflow/", func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/binflow")
		r2 := r.Clone(adapter.WithPrincipal(r.Context(), nil)) // anonymous, no T-14 chain
		r2.URL.Path = rel
		h.ServeHTTP(w, r2)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		_ = st.Close()
		_ = md.Close()
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), []byte("anon"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := curl(t, dir, "-s", "-T", "x.bin", "-o", "/dev/null", "-w", "%{http_code}",
		srv.URL+"/binflow/generic-local/anon/x.bin")
	if out != "401" {
		t.Fatalf("anonymous PUT = %s, want 401", out)
	}
}
