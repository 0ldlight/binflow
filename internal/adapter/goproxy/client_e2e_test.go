package goproxy

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestClientEndToEnd drives the REAL go toolchain against a real assembled
// BinFlow stack — FR-87's hard requirement (a protocol ticket is not done
// on HTTP-layer tests alone). Legs (goproxy.md section 7.2's command
// matrix, L10~L13):
//
//	L11 local: GOPROXY=$BASE/binflow/go-local GOPRIVATE='*'
//	    go mod download + go build (scratch module), including the
//	    uppercase-module escape case example.com/Upper/Mod;
//	L12 remote: pull-through with the second-download cache assertion
//	    (upstream hit count stays 1);
//	L13 virtual: local-first resolution (the local module hits the local
//	    member, the remote module walks to the remote member).
//
// It is environment-gated: set BINFLOW_T285_CLIENT_E2E=1 with a go
// toolchain on PATH. The gate skips silently when `go` is missing so CI
// without a toolchain stays green; the ticket log records a full gated run.
func TestClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T285_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T285_CLIENT_E2E=1 (with a go toolchain on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Fatalf("go toolchain unavailable on PATH: %v (see the ticket runbook)", err)
	}

	s := newStack(t)
	up := newFakeUpstream(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", up.srv.URL)
	s.seedVirtualRepo(t, "go-virt", "go-local", "go-local", "go-remote")

	// Seed the local member with two REAL modules (valid module zips, not
	// PK fixtures): the lowercase one and the uppercase-escape case.
	for _, m := range []struct{ module, version string }{
		{"example.com/mymod", "v1.0.2"},
		{"example.com/Upper/Mod", "v1.0.0"},
	} {
		seedRealModule(t, s, "go-local", m.module, m.version)
	}
	// The remote member's upstream serves one module as a real zip too.
	seedUpstreamModule(t, up, "example.com/rem/mod", "v1.0.0")

	env := clientEnv(t, s.srv.URL+"/binflow/go-local")

	// ---- L11: local repository, real client ----
	dir := t.TempDir()
	runGo(t, env, dir, "mod", "init", "example.com/scratch")
	runGo(t, env, dir, "mod", "edit", "-require=example.com/mymod@v1.0.2")
	runGo(t, env, dir, "mod", "download", "example.com/mymod@v1.0.2")
	writeMain(t, dir, "example.com/mymod")
	out := runGo(t, env, dir, "build", "./...")
	t.Logf("L11 go build: %s", oneLine(out))

	// The uppercase escape case: storage example.com/Upper/Mod, wire
	// example.com/!upper/!mod (the three-state rule under the real client).
	runGo(t, env, dir, "mod", "edit", "-require=example.com/Upper/Mod@v1.0.0")
	runGo(t, env, dir, "mod", "download", "example.com/Upper/Mod@v1.0.0")
	writeMain(t, dir, "example.com/Upper/Mod")
	out = runGo(t, env, dir, "build", "./...")
	t.Logf("L11 uppercase go build: %s", oneLine(out))
	// The real client addressed the escaped wire spelling.
	if n := up.count("/example.com/!upper/!mod/@v/v1.0.0.zip"); n != 0 {
		t.Errorf("uppercase module leaked to the remote upstream (%d contacts)", n)
	}

	// ---- L12: remote pull-through, cache assertion ----
	envR := clientEnv(t, s.srv.URL+"/binflow/go-remote")
	runGo(t, envR, dir, "mod", "download", "example.com/rem/mod@v1.0.0")
	runGo(t, envR, dir, "mod", "download", "example.com/rem/mod@v1.0.0")
	if n := up.count("/example.com/rem/mod/@v/v1.0.0.zip"); n != 1 {
		t.Errorf("L12 upstream zip contacts = %d, want 1 (the second download must be the cache hit)", n)
	} else {
		t.Log("L12 upstream contacted exactly once across two downloads")
	}

	// ---- L13: virtual, local-first ----
	envV := clientEnv(t, s.srv.URL+"/binflow/go-virt")
	runGo(t, envV, dir, "mod", "download", "example.com/mymod@v1.0.2") // hits the local member
	if n := up.count("/example.com/mymod/@v/v1.0.2.zip"); n != 0 {
		t.Errorf("L13 local-first miss: the remote upstream was contacted for the local module (%d)", n)
	}
	runGo(t, envV, dir, "mod", "download", "example.com/rem/mod@v1.0.0") // walks to the remote member
	writeMain(t, dir, "example.com/rem/mod")
	out = runGo(t, envV, dir, "build", "./...")
	t.Logf("L13 go build (virtual-resolved imports): %s", oneLine(out))
}

// clientEnv builds the go command environment (goproxy.md section 7.1,
// with the T-285 erratum): GOPROXY pointed at BinFlow and the sumdb OFF.
//
// The reverse spec's L11 literal — GOPRIVATE='*' — is NOT usable verbatim
// on the modern toolchain (verified go1.26.6): GOPRIVATE is the DEFAULT of
// GONOPROXY, so it sends every module to VCS resolution and BYPASSES
// GOPROXY entirely ("unrecognized import path … ?go-get=1"). The operative
// form of the AC2 posture "everything private through BinFlow" is
// GOPROXY=<binflow> + GOSUMDB=off (the same table's air-gapped row);
// GOPRIVATE remains the right knob only when the DIRECT/VCS fallback is
// wanted for a module subset. Registered for T-278's erratum list.
func clientEnv(t *testing.T, proxy string) []string {
	t.Helper()
	// NOT t.TempDir: the go module cache marks its files read-only, which
	// would fail t.TempDir's own RemoveAll; a manual dir with a
	// chmod-then-remove cleanup keeps the test honest.
	base, err := os.MkdirTemp("", "t285-go")
	if err != nil {
		t.Fatalf("temp base: %v", err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(base, func(p string, _ os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chmod(p, 0o700) //nolint:gosec // test scratch dir
			}
			return nil
		})
		_ = os.RemoveAll(base)
	})
	return append(os.Environ(),
		"GOFLAGS=-mod=mod",
		"GOPROXY="+proxy,
		"GOSUMDB=off",
		"GOTOOLCHAIN=local",
		"GOCACHE="+filepath.Join(base, "gocache"),
		"GOMODCACHE="+filepath.Join(base, "gomodcache"),
		"GOPATH="+filepath.Join(base, "gopath"),
		"GOENV=off",
		"CGO_ENABLED=0",
	)
}

// runGo executes the go toolchain in dir with env.
func runGo(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(goBinFor(t), args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

var goBinPath string

func goBinFor(t *testing.T) string {
	t.Helper()
	if goBinPath == "" {
		var err error
		goBinPath, err = exec.LookPath("go")
		if err != nil {
			t.Fatalf("go toolchain unavailable: %v", err)
		}
	}
	return goBinPath
}

// writeMain writes a main.go importing the module (a blank import proves
// the module resolved AND built).
func writeMain(t *testing.T, dir, module string) {
	t.Helper()
	src := fmt.Sprintf("package main\n\nimport _ %q\n\nfunc main() {}\n", module)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
}

// seedRealModule PUTs a REAL module (a valid module zip + matching .mod and
// .info) through the adapter's upload face.
func seedRealModule(t *testing.T, s *stack, repoKey, module, version string) {
	t.Helper()
	mod := fmt.Sprintf("module %s\n\ngo 1.21\n", module)
	info := fmt.Sprintf(`{"Version":%q,"Time":"2024-01-02T03:04:05Z"}`, version)
	zblob := moduleZip(t, module, version, map[string]string{
		"go.mod": mod,
		"pkg.go": fmt.Sprintf("package %s\n\n// Value proves the module body round-trips.\nconst Value = %q\n", pkgName(module), module+"@"+version),
	})
	base := fmt.Sprintf("/binflow/%s/%s/@v/%s", repoKey, module, version)
	if status, body, _ := s.put(base+".zip", zblob, nil); status != http.StatusCreated {
		t.Fatalf("seed .zip %s: %d %s", base, status, body)
	}
	if status, body, _ := s.put(base+".mod", []byte(mod), nil); status != http.StatusCreated {
		t.Fatalf("seed .mod %s: %d %s", base, status, body)
	}
	if status, body, _ := s.put(base+".info", []byte(info), nil); status != http.StatusCreated {
		t.Fatalf("seed .info %s: %d %s", base, status, body)
	}
}

// seedUpstreamModule registers one real module on the fake upstream.
func seedUpstreamModule(t *testing.T, up *fakeUpstream, module, version string) {
	t.Helper()
	mod := fmt.Sprintf("module %s\n\ngo 1.21\n", module)
	info := fmt.Sprintf(`{"Version":%q,"Time":"2024-01-02T03:04:05Z"}`, version)
	zblob := moduleZip(t, module, version, map[string]string{
		"go.mod": mod,
		"pkg.go": fmt.Sprintf("package %s\n\nconst Value = %q\n", pkgName(module), module+"@"+version),
	})
	prefix := "/" + module + "/@v/" + version
	up.register(t, prefix+".zip", "application/zip", zblob)
	up.register(t, prefix+".mod", "text/plain; charset=utf-8", []byte(mod))
	up.register(t, prefix+".info", "application/json", []byte(info))
	up.register(t, "/"+module+"/@v/list", "text/plain; charset=utf-8", []byte(version+"\n"))
	up.register(t, "/"+module+"/@latest", "application/json", []byte(info))
}

// pkgName derives a Go package name from a module path (last element,
// lowercased, non-identifier characters stripped).
func pkgName(module string) string {
	seg := strings.ToLower(module[strings.LastIndexByte(module, '/')+1:])
	var b strings.Builder
	for _, r := range seg {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	name := b.String()
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return "pkg" + name
	}
	return name
}

// moduleZip builds a REAL module zip: every entry prefixed
// "<module>@<version>/" (the official Module zip files shape).
func moduleZip(t *testing.T, module, version string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range sortedKeys(files) {
		w, err := zw.Create(module + "@" + version + "/" + name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}
