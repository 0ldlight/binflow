package pypi

// T-72's real-client leg: pip through a VIRTUAL repository — the M54
// one-shot download (the local member's package and the remote member's
// upstream package through one index URL) and the M55 merge observable
// (both members' versions of ONE project resolving through the same
// virtual page). twine seeds the local member with a real upload.
//
// Environment-gated like T-70's suite: set BINFLOW_T72_CLIENT_E2E=1 with a
// python that has pip (the T-70 runbook venv by default; a plain pip3 on
// PATH also works). Skips silently without the gate so client-less CI
// stays green; the ticket log records a full run.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

func TestVirtualClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T72_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T72_CLIENT_E2E=1 (with a pip on PATH) to run the real-client virtual matrix")
	}
	pipBin := os.Getenv("BINFLOW_T72_PIP")
	if pipBin == "" {
		if _, err := os.Stat("/tmp/t70venv/bin/pip"); err == nil {
			pipBin = "/tmp/t70venv/bin/pip"
		} else if bin, err := exec.LookPath("pip3"); err == nil {
			pipBin = bin
		}
	}
	if pipBin == "" {
		t.Skip("no pip available (BINFLOW_T72_PIP / /tmp/t70venv / PATH)")
	}
	out, err := exec.Command(pipBin, "--version").Output()
	if err != nil {
		t.Skipf("pip --version failed: %v", err)
	}
	t.Logf("pip client %s", strings.TrimSpace(string(out)))

	s := newStackCfg(t, nil)
	ctx := context.Background()

	// The mock upstream serves the project pages and the real wheel files
	// (a second copy of demo-lib at 0.2.0 plus an up-only project).
	var mu sync.Mutex
	hits := 0
	upDists := t.TempDir()
	// Wheels only, and under the underscored distribution spelling the wheel
	// filename spec demands (pip rejects dashed wheel names; PEP 503 still
	// resolves them onto the dashed project page).
	mkDists(t, upDists, "wheel:up_pkg:1.0.0", "wheel:demo_lib:0.2.0")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		data, ok := serveUpstreamFile(upDists, r.URL.Path)
		if ok {
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(upstream.Close)

	for _, row := range []*metadata.Repo{
		{RepoKey: "pyv-a", Type: repo.TypeLocal, PackageType: repo.PackagePypi},
		{RepoKey: "pyv-rem", Type: repo.TypeRemote, PackageType: repo.PackagePypi,
			Config: `{"url":"` + upstream.URL + `","allowPrivateUpstream":true}`},
		{RepoKey: "pyv-virt", Type: repo.TypeVirtual, PackageType: repo.PackagePypi,
			Config: `{"repositories":["pyv-a","pyv-rem"]}`},
	} {
		if _, err := s.svc.CreateRepo(ctx, &repo.Principal{Name: "admin", Admin: true}, row); err != nil {
			t.Fatalf("CreateRepo(%s): %v", row.RepoKey, err)
		}
	}

	// Seed the local member with a REAL twine upload (demo-lib 0.1.0).
	dists := t.TempDir()
	// Underscored spellings (valid wheel filenames; PEP 503 maps them
	// onto the dashed project page both members serve).
	mkDists(t, dists, "wheel:demo_lib:0.1.0", "sdist:demo_lib:0.1.0")
	if out, err := runPip(t, t.TempDir(), twineBinOrDefault(), "upload",
		"--repository-url", s.srv.URL+"/binflow/api/pypi/pyv-a",
		"-u", adminUser, "-p", adminPass, "--non-interactive", dists+"/*"); err != nil {
		t.Fatalf("twine upload to the local member failed: %s", out)
	} else {
		t.Logf("seed twine upload: %s", oneLineUp(out))
	}

	indexURL := s.srv.URL + "/binflow/api/pypi/pyv-virt/simple"

	// Diagnostic: what the merged page actually carries before pip reads it.
	if resp, err := http.Get(indexURL + "/up-pkg/"); err != nil {
		t.Fatalf("probe merged page: %v", err)
	} else {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Logf("merged up-pkg page (status %d):\n%s", resp.StatusCode, body)
	}

	// M54: one pip download through the VIRTUAL fetches BOTH members'
	// packages — demo-lib 0.1.0 from the local member, up-pkg pulled
	// through the remote member.
	dest := t.TempDir()
	if out, err := runPip(t, dest, pipBin, "download", "--no-deps", "--disable-pip-version-check",
		"--index-url", indexURL, "up-pkg", "demo-lib==0.1.0"); err != nil {
		t.Fatalf("M54 pip download through the virtual failed: %s", out)
	} else {
		t.Logf("M54 pip download: %s", oneLineUp(out))
	}
	for _, want := range []string{"up_pkg-1.0.0", "demo_lib-0.1.0"} {
		if !dirContains(dest, want) {
			t.Errorf("M54: %s not downloaded through the virtual (dir has %v)", want, listDir(dest))
		}
	}

	// M55: the merge observable — demo-lib exists in BOTH members (0.1.0
	// local, 0.2.0 upstream); the SAME virtual page supplies either
	// version, and pip's version listing reports the union.
	dest2 := t.TempDir()
	if out, err := runPip(t, dest2, pipBin, "download", "--no-deps", "--disable-pip-version-check",
		"--index-url", indexURL, "demo-lib==0.2.0"); err != nil {
		t.Fatalf("M55 pip download of the other member's version failed: %s", out)
	}
	if !dirContains(dest2, "demo_lib-0.2.0") {
		t.Errorf("M55: demo-lib 0.2.0 not resolvable through the merged page (dir has %v)", listDir(dest2))
	}
	if out, err := runPip(t, t.TempDir(), pipBin, "index", "versions", "demo-lib",
		"--index-url", indexURL, "--disable-pip-version-check"); err != nil {
		t.Logf("pip index versions unavailable (older pip?): %s", oneLineUp(out))
	} else {
		for _, want := range []string{"0.1.0", "0.2.0"} {
			if !strings.Contains(out, want) {
				t.Errorf("M55: pip index versions missing %s: %s", want, oneLineUp(out))
			}
		}
		t.Logf("M55 pip index versions: %s", oneLineUp(out))
	}

	mu.Lock()
	upHits := hits
	mu.Unlock()
	if upHits == 0 {
		t.Errorf("the remote member was never consulted (upstream hits 0)")
	}
	t.Logf("upstream requests through the virtual: %d", upHits)
}

// mkDists builds distribution fixtures with the T-70 runbook script.
func mkDists(t *testing.T, outdir string, specs ...string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "make_dist.py"))
	if err != nil {
		t.Fatalf("locate make_dist.py: %v", err)
	}
	py := os.Getenv("BINFLOW_T72_PYTHON")
	if py == "" {
		if _, err := os.Stat("/tmp/t70venv/bin/python"); err == nil {
			py = "/tmp/t70venv/bin/python"
		} else {
			py = "python3"
		}
	}
	args := append([]string{script, outdir}, specs...)
	cmd := exec.Command(py, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make_dist %v failed: %v\n%s", specs, err, out)
	}
}

// serveUpstreamFile answers an upstream request with the matching fixture
// file (a project page synthesized from the built dists, or the dist
// bytes themselves) — report false when the path matches nothing.
func serveUpstreamFile(dists string, path string) ([]byte, bool) {
	name, tail := splitUpstreamSimple(path)
	if tail == "" {
		// /simple/<name>/ — the page of every fixture file of that project.
		entries := upstreamPageFor(dists, name)
		if entries == "" {
			return nil, false
		}
		return []byte(indexHead + entries + indexFoot), true
	}
	// A file path: serve the dist verbatim if it exists (with or without
	// the conventional packages/ prefix the hrefs carry).
	for _, cand := range []string{path, strings.TrimPrefix(path, "/packages/"), strings.TrimPrefix(path, "packages/")} {
		full := filepath.Join(dists, filepath.Base(cand))
		if data, err := os.ReadFile(full); err == nil {
			return data, true
		}
	}
	return nil, false
}

// splitUpstreamSimple recognizes /simple/<name>/ (name, "") versus a file
// path (name-prefix stripped when present).
func splitUpstreamSimple(path string) (string, string) {
	trimmed := strings.Trim(path, "/")
	if rest, ok := strings.CutPrefix(trimmed, "simple/"); ok && !strings.Contains(rest, "/") {
		return rest, ""
	}
	if rest, ok := strings.CutPrefix(trimmed, "packages/"); ok {
		return "", rest
	}
	return "", trimmed
}

// upstreamPageFor renders one upstream project page (real sha256
// fragments pip verifies against) from the built fixtures. The fixture
// script names files after the LITERAL project spelling, so both the
// dashed and the underscored prefix match.
func upstreamPageFor(dists, name string) string {
	entries := ""
	files, err := os.ReadDir(dists)
	if err != nil {
		return ""
	}
	prefixes := []string{name + "-", strings.ReplaceAll(name, "-", "_") + "-"}
	for _, f := range files {
		fn := f.Name()
		matched := false
		for _, p := range prefixes {
			if strings.HasPrefix(fn, p) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dists, fn))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		entries += `<a href="../../packages/` + fn + `#sha256=` + hex.EncodeToString(sum[:]) + `">` + fn + "</a>\n"
	}
	return entries
}

// twineBinOrDefault resolves the twine binary (the T-70 venv default).
func twineBinOrDefault() string {
	if b := os.Getenv("BINFLOW_T72_TWINE"); b != "" {
		return b
	}
	if _, err := os.Stat("/tmp/t70venv/bin/twine"); err == nil {
		return "/tmp/t70venv/bin/twine"
	}
	return "twine"
}

// runPip runs one client command in dir.
func runPip(t *testing.T, dir, bin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// dirContains reports whether any file name in dir contains sub.
func dirContains(dir, sub string) bool {
	for _, n := range listDir(dir) {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

// listDir lists a directory's file names (empty on error).
func listDir(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// oneLineUp flattens client output for the log.
func oneLineUp(s string) string { return strings.Join(strings.Fields(s), " ") }
