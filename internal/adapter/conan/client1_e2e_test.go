package conan

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestConan1ClientEndToEnd drives the REAL conan 1.x client (1.66) against
// the v1 data plane — the CN-1 final scope's own client. The stock
// environment has no conan 1.x (EOL); the ticket's fallback was the curl
// equivalence, but a pip-installable 1.66 was available in the run
// environment, so the real client ran instead. Legs:
//
//	conan user        login (v1/users/authenticate)
//	conan create      a real recipe + binary package, exported locally
//	conan upload      upload_urls + the files channel (recipe and package)
//	conan search      the v1 search endpoint
//	conan install     after a local-cache wipe: snapshot + download_urls +
//	                  the files channel GETs (the full download chain)
//	conan remove -r   the v1 recipe delete
//
// Environment-gated like the conan 2 leg: BINFLOW_T308_CLIENT1_E2E=1 with
// a conan 1.x on PATH.
func TestConan1ClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T308_CLIENT1_E2E") != "1" {
		t.Skip("set BINFLOW_T308_CLIENT1_E2E=1 (with a conan 1.x on PATH) to run the conan 1 client matrix")
	}
	conanBin, version := requireConan1(t)

	s := newStack(t)
	s.seedRepo(t, "conan-local", repo.TypeLocal)
	home := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(conanBin, args...)
		cmd.Env = append(os.Environ(), "CONAN_USER_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("conan %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// conan 1's own profile detection predates apple-clang 17 — pin a
	// fixed profile so the create does not depend on host detection.
	profiles := filepath.Join(home, ".conan", "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "default"), []byte(conan1Profile), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	// ---- login ----
	run("remote", "add", "binflow", s.srv.URL+"/binflow/conan-local")
	out := run("user", "-p", adminPass, "-r", "binflow", adminUser)
	if !strings.Contains(out, "Changed user") {
		t.Fatalf("conan user output lacks the login confirmation:\n%s", out)
	}

	// ---- create + upload ----
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "conanfile.py"), []byte(conan1Recipe), 0o644); err != nil {
		t.Fatalf("write conanfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.txt"), []byte("v1 client payload"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	run("create", src, "myuser/stable")
	out = run("upload", "hello/1.0@myuser/stable", "-r", "binflow", "--all", "--confirm")
	if !strings.Contains(out, "Uploaded conan recipe") {
		t.Fatalf("upload output lacks the recipe confirmation:\n%s", out)
	}

	// ---- search ----
	out = run("search", "-r", "binflow")
	if !strings.Contains(out, "hello/1.0@myuser/stable") {
		t.Fatalf("search output lacks the ref:\n%s", out)
	}

	// The server-side truth after the v1 upload: revision 0 registered,
	// the recipe files in the snapshot, the v2 plane sees the same tree.
	if code, body, _ := s.get(v1("conan-local", "conans/hello/1.0/myuser/stable")); code != 200 ||
		!strings.Contains(body, "conanfile.py") {
		t.Fatalf("post-upload snapshot = (%d, %s)", code, body)
	}
	if code, body, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/latest")); code != 200 ||
		!strings.Contains(body, `"revision":"0"`) {
		t.Fatalf("post-upload v2 latest = (%d, %s), want revision 0", code, body)
	}

	// ---- install from the remote (wipe the local cache first) ----
	run("remove", "hello/1.0@myuser/stable", "-f")
	consumer := t.TempDir()
	install := exec.Command(conanBin, "install", "hello/1.0@myuser/stable",
		"-r", "binflow", "--build=missing")
	install.Dir = consumer
	install.Env = append(os.Environ(), "CONAN_USER_HOME="+home)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("conan install: %v\n%s", err, out)
	} else if !strings.Contains(string(out), "hello") && !strings.Contains(string(out), "Installed") {
		t.Fatalf("install output lacks the installation:\n%s", out)
	}

	// ---- remove through the remote ----
	run("remove", "hello/1.0@myuser/stable", "-r", "binflow", "-f")
	if code, _, _ := s.get(v1("conan-local", "conans/hello/1.0/myuser/stable")); code != 404 {
		t.Fatalf("post-remove snapshot = %d, want 404", code)
	}
	_ = version
}

// conan1Profile is the pinned default profile (apple-clang 16 — the
// newest conan 1.66 knows).
const conan1Profile = `[settings]
os=Macos
os_build=Macos
arch=x86_64
arch_build=x86_64
compiler=apple-clang
compiler.version=16
compiler.libcxx=libc++
build_type=Release
`

// conan1Recipe is the conan 1 syntax recipe (conans package).
const conan1Recipe = `from conans import ConanFile


class HelloConan(ConanFile):
    name = "hello"
    version = "1.0"
    settings = "os", "arch"

    def package(self):
        self.copy("payload.txt")
`

// requireConan1 resolves a conan 1.x binary (any spelling of version 1).
func requireConan1(t *testing.T) (string, string) {
	t.Helper()
	for _, cand := range []string{"conan"} {
		if p, err := exec.LookPath(cand); err == nil {
			out, verr := exec.Command(p, "--version").CombinedOutput()
			if verr == nil && strings.Contains(string(out), "version 1.") {
				return p, strings.TrimSpace(string(out))
			}
		}
	}
	t.Fatal("conan 1.x not found on PATH (pip install conan==1.66.0)")
	return "", ""
}
