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

// TestClientRemoteVirtualEndToEnd drives the REAL conan 2.x client through
// the T-312 faces: a REMOTE repository whose upstream is the same server's
// local repository (the pull-through chain, handshake included) and a
// VIRTUAL repository aggregating the local member with that remote member
// (merged reads plus a routed upload). Legs:
//
//	R-c1 seed: conan create + upload of revision one into the local
//	    repository (the remote's upstream);
//	R-c2 remote read chain: remote add + LOGIN on the remote repository
//	    (the handshake trio served on the remote class), then a
//	    cache-cleared conan install --requires THROUGH the remote — the
//	    full proxy chain (latest, files listing, package revisions, every
//	    file body) pulls through and the payload is the upstream's;
//	R-c3 virtual merged read: a second coordinate uploaded into the local
//	    member, then a cache-cleared install THROUGH the virtual (the
//	    merged latest resolves, bodies serve first-found);
//	R-c4 routed upload: conan upload THROUGH the virtual lands in the
//	    deployment member and the merged view immediately reflects it
//	    (conan list through the virtual);
//	R-c5 the remote's honest search refusal: the client's pattern listing
//	    on the remote surfaces the not-proxied answer.
//
// Environment-gated: BINFLOW_T312_CLIENT_E2E=1 with a conan 2.x on PATH
// (the T-308 venv works: PATH=/tmp/t308-venv/bin).
func TestClientRemoteVirtualEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T312_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T312_CLIENT_E2E=1 (with a conan 2.x on PATH) to run the real-client remote/virtual matrix")
	}
	conanBin := requireConan(t)
	if v := conanVersion(t, conanBin); !strings.Contains(v, "version 2.") {
		t.Fatalf("conan 2.x required, found %q", v)
	}

	s := newStack(t)
	s.seedRepo(t, "conan-local", repo.TypeLocal)
	s.seedRepo(t, "conan-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "conan-remote", s.srv.URL+"/binflow/conan-local")
	s.seedVirtualRepo(t, "conan-virt", []string{"conan-local", "conan-remote"}, "conan-local")

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
	// expectFail runs a command that is ALLOWED to fail and returns its
	// combined output (the refusal legs).
	expectFail := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(conanBin, args...)
		cmd.Env = append(os.Environ(), "CONAN_HOME="+home)
		out, _ := cmd.CombinedOutput()
		return string(out)
	}

	run("profile", "detect", "--force")

	src := t.TempDir()
	conanfile := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, "conanfile.py"), []byte(body), 0o644); err != nil {
			t.Fatalf("write conanfile: %v", err)
		}
	}

	// ---- R-c1: seed revision one into the local (the remote's upstream) ----
	run("remote", "add", "binflow", s.srv.URL+"/binflow/conan-local", "--force")
	run("remote", "login", "binflow", adminUser, "-p", adminPass)
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

	// ---- R-c2: the remote repository's read chain ----
	run("remote", "add", "binflow-remote", s.srv.URL+"/binflow/conan-remote", "--force")
	run("remote", "login", "binflow-remote", adminUser, "-p", adminPass)
	run("remove", "hello/1.0@myuser/stable", "-c") // drop the cache: the install must pull through
	consumer := t.TempDir()
	install := exec.Command(conanBin, "install", "--requires=hello/1.0@myuser/stable",
		"-r", "binflow-remote", "--build=missing", "-pr=default")
	install.Dir = consumer
	install.Env = append(os.Environ(), "CONAN_HOME="+home)
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("conan install through the remote: %v\n%s", err, out)
	}
	var content []byte
	for _, root := range []string{consumer, home} {
		if b := findFile(t, root, "hello.txt"); b != nil {
			content = b
			break
		}
	}
	if content == nil || !strings.Contains(string(content), "revision one") {
		t.Fatalf("remote-installed payload = %q, want the upstream revision's", content)
	}

	// ---- R-c3: the virtual's merged read ----
	// A second coordinate lands in the local member; the virtual's install
	// must resolve and serve it (merged latest, first-found body).
	conanfile(`import os

from conan import ConanFile
from conan.tools.files import save

class HelloConan(ConanFile):
    name = "hello"
    version = "2.0"
    settings = "os", "arch"

    def package(self):
        save(self, os.path.join(self.package_folder, "hello2.txt"), "virtual revision")
`)
	run("create", src, "--user=myuser", "--channel=stable", "-pr=default")
	run("upload", "hello/2.0@myuser/stable", "-r", "binflow", "--confirm")
	run("remove", "hello/2.0@myuser/stable", "-c")

	run("remote", "add", "binflow-virt", s.srv.URL+"/binflow/conan-virt", "--force")
	run("remote", "login", "binflow-virt", adminUser, "-p", adminPass)
	consumer2 := t.TempDir()
	install2 := exec.Command(conanBin, "install", "--requires=hello/2.0@myuser/stable",
		"-r", "binflow-virt", "--build=missing", "-pr=default")
	install2.Dir = consumer2
	install2.Env = append(os.Environ(), "CONAN_HOME="+home)
	if out, err := install2.CombinedOutput(); err != nil {
		t.Fatalf("conan install through the virtual: %v\n%s", err, out)
	}
	content = nil
	for _, root := range []string{consumer2, home} {
		if b := findFile(t, root, "hello2.txt"); b != nil {
			content = b
			break
		}
	}
	if content == nil || !strings.Contains(string(content), "virtual revision") {
		t.Fatalf("virtual-installed payload = %q, want the local member's revision", content)
	}

	// ---- R-c4: a routed upload through the virtual ----
	conanfile(`import os

from conan import ConanFile
from conan.tools.files import save

class HelloConan(ConanFile):
    name = "hello"
    version = "3.0"
    settings = "os", "arch"

    def package(self):
        save(self, os.path.join(self.package_folder, "hello3.txt"), "routed upload")
`)
	run("create", src, "--user=myuser", "--channel=stable", "-pr=default")
	run("upload", "hello/3.0@myuser/stable", "-r", "binflow-virt", "--confirm")

	// The merged view immediately reflects the routed write (conan list
	// walks the virtual's search + revisions).
	listing := run("list", "hello/3.0@myuser/stable#*", "-r", "binflow-virt", "--format=json")
	jsonStart := strings.Index(listing, "{")
	if jsonStart < 0 {
		t.Fatalf("conan list through the virtual produced no JSON:\n%s", listing)
	}
	var listed map[string]map[string]struct {
		Revisions map[string]json.RawMessage `json:"revisions"`
	}
	if err := json.Unmarshal([]byte(listing[jsonStart:]), &listed); err != nil {
		t.Fatalf("conan list --format=json: %v\n%s", err, listing)
	}
	virtView, ok := listed["binflow-virt"]
	if !ok {
		t.Fatalf("conan list json lacks the virtual remote:\n%s", listing)
	}
	if _, ok := virtView["hello/3.0@myuser/stable"]; !ok {
		t.Fatalf("conan list through the virtual lacks the routed upload:\n%s", listing)
	}
	// The routed file physically sits in the deployment member.
	rf := ref{name: "hello", version: "3.0", user: "myuser", channel: "stable"}
	code, body, _ := s.get(v2("conan-virt", "hello/3.0/myuser/stable/revisions"))
	if code != 200 {
		t.Fatalf("virtual revisions after routed upload = %d (body %s)", code, body)
	}

	// ---- R-c5: the remote's honest search refusal surfaces client-side ----
	_ = expectFail("list", "hello/*", "-r", "binflow-remote")
	code, body, _ = s.get(v2("conan-remote", "search?q=hello/*"))
	if code != 404 || body != msgRemoteSearch {
		t.Errorf("remote search = (%d, %q), want the T-287 refusal", code, body)
	}
	_ = rf
}
