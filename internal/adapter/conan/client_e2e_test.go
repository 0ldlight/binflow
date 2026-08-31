package conan

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestClientEndToEnd drives the REAL conan 2.x client against a real
// assembled BinFlow stack — FR-96's hard requirement (a protocol ticket
// is not done on HTTP-layer tests alone). Legs (the L-c1..L-c5 matrix of
// docs/reverse/conan.md section 8):
//
//	L-c1 handshake: remote add + login (the v1 trio the conan 2 client
//	    walks even for v2-only work);
//	L-c2 upload: conan create + conan upload of TWO revisions of one
//	    recipe plus its binary package (the revision chain forms);
//	L-c3 install: a scratch consumer directory installs --requires from
//	    the remote (--build=missing never fires — the binary is served);
//	    conan list shows the revision chain;
//	L-c4 curl reconciliation: the revisions/latest endpoints' bodies
//	    checked against the client's own view;
//	L-c5 remove: conan remove -r deletes through the v2 plane.
//
// It is environment-gated: set BINFLOW_T308_CLIENT_E2E=1 with a conan 2.x
// on PATH. The gate skips silently when the env is unset so CI stays
// green; the ticket log records a full run.
func TestClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T308_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T308_CLIENT_E2E=1 (with a conan 2.x on PATH) to run the real-client matrix")
	}
	conanBin := requireConan(t)
	version := conanVersion(t, conanBin)
	if !strings.Contains(version, "version 2.") {
		t.Fatalf("conan 2.x required, found %q", version)
	}

	s := newStack(t)
	s.seedRepo(t, "conan-local", repo.TypeLocal)
	remote := s.srv.URL + "/binflow/conan-local"

	home := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(conanBin, args...)
		cmd.Env = append(os.Environ(), "CONAN_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("conan %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// ---- L-c1: profile + remote add + login ----
	run("profile", "detect", "--force")
	run("remote", "add", "binflow", remote, "--force")
	run("remote", "login", "binflow", adminUser, "-p", adminPass)

	// ---- L-c2: create + upload two revisions ----
	src := t.TempDir()
	conanfile := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, "conanfile.py"), []byte(body), 0o644); err != nil {
			t.Fatalf("write conanfile: %v", err)
		}
	}
	conanfile(`import os

from conan import ConanFile
from conan.tools.files import save

class HelloConan(ConanFile):
    name = "hello"
    version = "1.0"
    settings = "os", "arch"

    def package(self):
        save(self, os.path.join(self.package_folder, "hello.txt"), "revision one")
`)
	run("create", src, "--user=myuser", "--channel=stable", "-pr=default")
	run("upload", "hello/1.0@myuser/stable", "-r", "binflow", "--confirm")

	conanfile(`import os

from conan import ConanFile
from conan.tools.files import save

class HelloConan(ConanFile):
    name = "hello"
    version = "1.0"
    settings = "os", "arch"

    def package(self):
        save(self, os.path.join(self.package_folder, "hello.txt"), "revision two")
`)
	run("create", src, "--user=myuser", "--channel=stable", "-pr=default")
	run("upload", "hello/1.0@myuser/stable", "-r", "binflow", "--confirm")

	// ---- L-c4: reconcile the revision chain over raw HTTP ----
	code, body, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/revisions"))
	if code != 200 {
		t.Fatalf("revisions = %d (body %s)", code, body)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("revisions body %q: %v", body, err)
	}
	if len(doc.Revisions) != 2 {
		t.Fatalf("revisions = %d entries, want 2 (both uploads registered)", len(doc.Revisions))
	}
	if doc.Reference != "hello/1.0@myuser/stable" {
		t.Errorf("reference = %q", doc.Reference)
	}

	// ---- L-c3: install from the remote + list ----
	// Drop the local cache copy first so the install MUST walk the remote
	// download chain (latest resolution + files) rather than reusing the
	// create's cache — both revisions are cached after the two creates.
	run("remove", "hello/1.0@myuser/stable", "-c")
	consumer := t.TempDir()
	install := exec.Command(conanBin, "install", "--requires=hello/1.0@myuser/stable",
		"-r", "binflow", "--build=missing", "-pr=default")
	install.Dir = consumer
	install.Env = append(os.Environ(), "CONAN_HOME="+home)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("conan install: %v\n%s", err, out)
	}
	// The LATEST revision's payload is what installed. conan 2 keeps the
	// package folder under CONAN_HOME/p (the cache), not the consumer
	// directory — search both trees.
	var content []byte
	for _, root := range []string{consumer, home} {
		if b := findFile(t, root, "hello.txt"); b != nil {
			content = b
			break
		}
	}
	if content == nil {
		t.Fatalf("hello.txt not installed under %s or %s", consumer, home)
	}
	if !strings.Contains(string(content), "revision two") {
		t.Fatalf("installed payload = %q, want the LATEST revision's", string(content))
	}

	listing := run("list", "hello/1.0@myuser/stable#*", "-r", "binflow", "--format=json")
	// The status banner ("Connecting to remote…") rides stdout ahead of the
	// document; the JSON starts at the first brace.
	jsonStart := strings.Index(listing, "{")
	if jsonStart < 0 {
		t.Fatalf("conan list produced no JSON:\n%s", listing)
	}
	var listed map[string]map[string]struct {
		Revisions map[string]json.RawMessage `json:"revisions"`
	}
	if err := json.Unmarshal([]byte(listing[jsonStart:]), &listed); err != nil {
		t.Fatalf("conan list --format=json: %v\n%s", err, listing)
	}
	remoteView, ok := listed["binflow"]
	if !ok {
		t.Fatalf("conan list json lacks the binflow remote:\n%s", listing)
	}
	entry, ok := remoteView["hello/1.0@myuser/stable"]
	if !ok {
		t.Fatalf("conan list json lacks the ref:\n%s", listing)
	}
	if len(entry.Revisions) != 2 {
		t.Fatalf("conan list sees %d revisions (%v), want 2", len(entry.Revisions), entry.Revisions)
	}

	// ---- L-c5: remove through the remote ----
	// L16's 2.x parity leg (T-369): the no-revision DELETE takes the WHOLE
	// revision chain, so the chain endpoint itself answers 404 — a
	// latest-chain regression would leave it 200 with one entry.
	run("remove", "hello/1.0@myuser/stable", "-r", "binflow", "-c")
	if code, _, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/latest")); code != 404 {
		t.Fatalf("post-remove latest = %d, want 404", code)
	}
	if code, body, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/revisions")); code != 404 {
		t.Fatalf("post-remove revisions = (%d, %s), want 404 (whole tree gone)", code, body)
	}
}

// findFile walks dir for name.
func findFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	var found []byte
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == name {
			b, rerr := os.ReadFile(p)
			if rerr == nil {
				found = b
			}
		}
		return nil
	})
	return found
}

// requireConan resolves the conan binary (pipx's ~/.local/bin injection
// included).
func requireConan(t *testing.T) string {
	t.Helper()
	if p, err := exec.LookPath("conan"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	if p := filepath.Join(home, ".local", "bin", "conan"); fileExists(p) {
		return p
	}
	t.Fatal("conan not found on PATH (pipx install conan)")
	return ""
}

// conanVersion reports the client's own version string.
func conanVersion(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("conan --version: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// fileExists is the stat probe.
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
