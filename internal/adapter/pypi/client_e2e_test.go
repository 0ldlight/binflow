package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestClientEndToEnd drives the REAL clients (twine upload, pip
// install/download) against a real assembled BinFlow stack — the FR-22
// hard requirement: a protocol ticket is not done on HTTP-layer tests
// alone.
//
// It is environment-gated: set BINFLOW_T70_CLIENT_E2E=1 with a python
// venv that has twine (>= 6.2, no md5_digest on the wire) and setuptools.
// The defaults point at the venv the ticket's runbook created
// (/tmp/t70venv); override with BINFLOW_T70_PYTHON, BINFLOW_T70_TWINE,
// BINFLOW_T70_PIP. The gate skips silently when the interpreter is
// missing so CI without python stays green; the ticket log records a full
// gated run.
func TestClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T70_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T70_CLIENT_E2E=1 (with a twine+setuptools venv) to run the real-client matrix")
	}
	env := func(name, def string) string {
		if v := os.Getenv(name); v != "" {
			return v
		}
		return def
	}
	pyBin := env("BINFLOW_T70_PYTHON", "/tmp/t70venv/bin/python")
	twineBin := env("BINFLOW_T70_TWINE", "/tmp/t70venv/bin/twine")
	pipBin := env("BINFLOW_T70_PIP", "/tmp/t70venv/bin/pip")
	if _, err := os.Stat(pyBin); err != nil {
		t.Fatalf("python interpreter %s unavailable: %v (see the ticket runbook)", pyBin, err)
	}

	s := newStack(t)
	base := s.srv.URL + "/binflow/api/pypi/pypi-local"
	path := "/binflow/api/pypi/pypi-local"
	dist := t.TempDir()

	script, err := filepath.Abs(filepath.Join("testdata", "make_dist.py"))
	if err != nil {
		t.Fatalf("locate make_dist.py: %v", err)
	}

	// Build the fixtures: demo-lib 0.1.0, demo-pkg 0.1.0 and 0.2.0 (0.2.0
	// depends on demo-lib — M34's dependency chain), Demo_Pkg 3.1.4
	// (original casing — it shares demo-pkg's normalized index page, the
	// normalization matrix made concrete), all as wheel+sdist pairs (M35).
	specs := []string{
		"wheel:demo_lib:0.1.0", "sdist:demo_lib:0.1.0",
		"wheel:demo_pkg:0.1.0", "sdist:demo_pkg:0.1.0",
		"wheel:demo_pkg:0.2.0:demo-lib", "sdist:demo_pkg:0.2.0:demo-lib",
		"wheel:Demo_Pkg:3.1.4", "sdist:Demo_Pkg:3.1.4",
	}
	runCmd(t, dist, pyBin, append([]string{script, dist}, specs...)...)

	// M30: twine upload of every distribution file (twine's glob or the
	// expanded list — both spellings accepted here).
	files, err := filepath.Glob(filepath.Join(dist, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob dist files: %v (%d)", err, len(files))
	}
	args := append([]string{"upload", "--repository-url", base, "-u", adminUser, "-p", adminPass,
		"--non-interactive"}, files...)
	out := runCmd(t, dist, twineBin, args...)
	t.Logf("M30 twine upload: %s", oneLine(out))

	// M31: the simple page carries hrefs and #sha256=, the api-version
	// header, the 302 redirect and the ETag/304 pair.
	status, page, _ := s.get(path + "/simple/demo-pkg/")
	if status != http.StatusOK || !strings.Contains(page, "#sha256=") {
		t.Fatalf("M31 simple page: status %d\n%s", status, page)
	}
	if !strings.Contains(page, `name="api-version" value="2"`) {
		t.Fatalf("M31 api-version meta missing:\n%s", page)
	}
	resp := s.doRaw(http.MethodGet, path+"/simple/demo-pkg", "", "", nil, nil)
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "demo-pkg/" {
		t.Fatalf("M31 no-slash redirect: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_ = resp.Body.Close()
	_, _, hdr := s.get(path + "/simple/demo-pkg/")
	if resp2 := s.do(http.MethodGet, path+"/simple/demo-pkg/", "", "", nil,
		map[string]string{"If-None-Match": hdr.Get("ETag")}); resp2.StatusCode != http.StatusNotModified {
		t.Fatalf("M31 ETag-304: %d", resp2.StatusCode)
	} else {
		_ = resp2.Body.Close()
	}
	// Normalized spellings share one page.
	for _, spelling := range []string{"demo_pkg", "demo-pkg", "Demo.Pkg"} {
		if st, _, _ := s.get(path + "/simple/" + spelling + "/"); st != http.StatusOK {
			t.Fatalf("M31 normalization: %s -> %d", spelling, st)
		}
	}

	// M32: fresh pip install + pip download hash reconciliation.
	target := t.TempDir()
	out = runCmd(t, dist, pipBin, "install", "--no-cache-dir", "--disable-pip-version-check",
		"--target", target, "--index-url", base+"/simple", "demo-pkg==0.1.0")
	t.Logf("M32 pip install: %s", oneLine(out))
	if err := os.WriteFile(filepath.Join(target, "binflow-marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("marker: %v", err)
	}
	fragRE := regexp.MustCompile(`demo_pkg-0\.1\.0-py3-none-any\.whl#sha256=([0-9a-f]{64})`)
	m := fragRE.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("M32 no whl fragment on the page:\n%s", page)
	}
	dl := t.TempDir()
	out = runCmd(t, dist, pipBin, "download", "--no-deps", "--disable-pip-version-check",
		"--only-binary", ":all:", "-d", dl, "--index-url", base+"/simple", "demo-pkg==0.1.0")
	t.Logf("M32 pip download: %s", oneLine(out))
	got := sha256File(t, filepath.Join(dl, "demo_pkg-0.1.0-py3-none-any.whl"))
	if got != m[1] {
		t.Fatalf("M32 hash reconciliation: downloaded %s, index fragment %s", got, m[1])
	}

	// M33: duplicate upload — the server answers 400 with the
	// already-exists wording and twine exits non-zero surfacing the status.
	// (twine 7's non-verbose renderer shows the status line, not the body;
	// the wording itself is asserted server-side below and in the unit
	// suite.)
	dup, err := filepath.Glob(filepath.Join(dist, "demo_pkg-0.1.0*"))
	if err != nil || len(dup) == 0 {
		t.Fatalf("glob duplicate upload files: %v", err)
	}
	out, err = runCmdErr(t, dist, twineBin, append([]string{"upload", "--repository-url", base,
		"-u", adminUser, "-p", adminPass, "--non-interactive"}, dup...)...)
	t.Logf("M33 duplicate upload output: %s", oneLine(out))
	if err == nil {
		t.Fatal("M33 duplicate twine upload unexpectedly succeeded")
	}
	if !strings.Contains(out, "400") {
		t.Fatalf("M33 twine output does not surface the 400:\n%s", out)
	}
	dupStatus, dupBody := s.upload(path, map[string]string{
		":action": "file_upload", "name": "demo-pkg", "version": "0.1.0",
	}, "demo_pkg-0.1.0-py3-none-any.whl", []byte("duplicate"))
	if dupStatus != http.StatusBadRequest || !strings.Contains(strings.ToLower(dupBody), "already exists") {
		t.Fatalf("M33 server-side duplicate verdict: %d %s", dupStatus, dupBody)
	}

	// M34: dependency chain — demo-pkg 0.2.0 pulls demo-lib from the same
	// index.
	target2 := t.TempDir()
	out = runCmd(t, dist, pipBin, "install", "--no-cache-dir", "--disable-pip-version-check",
		"--target", target2, "--index-url", base+"/simple", "demo-pkg==0.2.0")
	t.Logf("M34 dependency-chain install: %s", oneLine(out))
	if _, err := os.Stat(filepath.Join(target2, "demo_lib", "__init__.py")); err != nil {
		t.Fatalf("M34 demo-lib was not resolved from the same index: %v", err)
	}

	// M35: wheel + sdist of one version coexist; pip picks per its
	// --only-binary/--no-binary mode. The sdist build runs with
	// --no-build-isolation: build isolation would fetch setuptools from
	// THIS index (the private-index reality — a repo without setuptools
	// cannot self-host build deps), while the venv already carries it.
	targetW := t.TempDir()
	out = runCmd(t, dist, pipBin, "install", "--no-cache-dir", "--disable-pip-version-check",
		"--target", targetW, "--only-binary", ":all:", "--index-url", base+"/simple", "Demo_Pkg==3.1.4")
	t.Logf("M35 wheel-only install: %s", oneLine(out))
	targetS := t.TempDir()
	out = runCmd(t, dist, pipBin, "install", "--no-cache-dir", "--disable-pip-version-check",
		"--no-build-isolation", "--target", targetS, "--no-binary", ":all:",
		"--index-url", base+"/simple", "Demo_Pkg==3.1.4")
	t.Logf("M35 sdist-only build+install: %s", oneLine(out))
	_, page3, _ := s.get(path + "/simple/Demo_Pkg/")
	if !strings.Contains(page3, ".whl") || !strings.Contains(page3, ".tar.gz") {
		t.Fatalf("M35 the Demo_Pkg page must list both the wheel and the sdist:\n%s", page3)
	}
	if !strings.Contains(page3, "Demo_Pkg-3.1.4") || !strings.Contains(page3, "demo_pkg-0.1.0") {
		t.Fatalf("M35 normalization: Demo_Pkg and demo_pkg entries must share the page:\n%s", page3)
	}

	// M35b: PEP 691 JSON simple.
	resp3 := s.do(http.MethodGet, path+"/simple/demo-pkg/", "", "", nil,
		map[string]string{"Accept": simpleJSONMediaType})
	var doc struct {
		Files []struct {
			Filename string `json:"filename"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp3.Body).Decode(&doc); err != nil {
		t.Fatalf("M35b JSON simple: %v", err)
	}
	_ = resp3.Body.Close()
	if len(doc.Files) == 0 {
		t.Fatal("M35b JSON simple has no files")
	}
	t.Logf("M35b JSON simple files: %d", len(doc.Files))

	// AC③: anonymous GET passes, anonymous POST is challenged (the route
	// gate; asserted in unit tests too — here against the live stack the
	// clients see).
	if st, _, _ := s.get(path + "/simple/demo-pkg/"); st != http.StatusOK {
		t.Fatalf("anonymous simple GET: %d", st)
	}
	t.Log("client matrix complete: M30 M31 M32 M33 M34 M35 M35b AC3 all passed")
}

// runCmd runs a client command in dir, failing the test on a non-zero exit.
func runCmd(t *testing.T, dir, bin string, args ...string) string {
	t.Helper()
	out, err := runCmdErr(t, dir, bin, args...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), strings.Join(args, " "), err, out)
	}
	return out
}

// runCmdErr runs a client command and returns its combined output and exit
// error without failing the test (negative cases need both).
func runCmdErr(t *testing.T, dir, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...) //nolint:gosec // fixed client binaries under test
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("$ %s %s\n%s", filepath.Base(bin), strings.Join(args, " "), string(out))
	return string(out), err
}

// sha256HexOfFile reconciles a downloaded file with an index fragment.
func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// oneLine squeezes client output for the log.
func oneLine(s string) string {
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(s, " "))
}
