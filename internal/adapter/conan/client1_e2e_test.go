package conan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
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
//	conan remove -r   the v1 recipe delete — TWICE: once on a plain
//	                  coordinate, once on a multi-revision one (T-369's
//	                  D8 leg: the whole revision tree must go, the client's
//	                  own revision `0` included)
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

	// ---- D8 flip leg (T-369): the multi-revision whole-tree remove ----
	// Re-upload through the client (registers revision `0`), then seed a
	// SECOND revision through the v2 plane — the v2 write's fresh
	// .timestamp makes it the index's latest, so the retired T-308
	// posture (delete only the latest chain) would have kept revision `0`
	// alive. The flipped semantics must clear BOTH.
	run("upload", "hello/1.0@myuser/stable", "-r", "binflow", "--all", "--confirm")
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	if code, body, _ := s.putRecipeFile("conan-local", r, fixtureRev(9), "conanfile.py", []byte("v2-seeded sibling")); code != http.StatusCreated {
		t.Fatalf("sibling revision PUT = (%d, %s)", code, body)
	}
	if code, body, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/revisions")); code != 200 || !strings.Contains(body, `"0"`) {
		t.Fatalf("pre-remove revisions = (%d, %s), want both revisions live", code, body)
	}
	run("remove", "hello/1.0@myuser/stable", "-r", "binflow", "-f")
	if code, _, _ := s.get(v1("conan-local", "conans/hello/1.0/myuser/stable")); code != 404 {
		t.Fatalf("post-remove v1 snapshot = %d, want 404", code)
	}
	if code, body, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/revisions")); code != 404 {
		t.Fatalf("post-remove revisions = (%d, %s), want 404 (every revision gone)", code, body)
	}
	if code, _, _ := s.get(v2("conan-local", "hello/1.0/myuser/stable/revisions/0/files/conanfile.py")); code != 404 {
		t.Fatalf("post-remove revision-0 file = %d, want 404 (the client's own revision went with the tree)", code)
	}
	_ = version
}

// TestConan1LayoutSweepRoundtrip (T-371 / D-F2, ADR-0042 AC-2): the REAL
// conan 1.66 client's install roundtrip is EQUIVALENT across the legacy
// double layout and the migrated spec layout. Legs:
//
//	create + upload    the client lands the package (fixed binary, spec
//	                   layout) and a baseline install captures the cache
//	                   digest map;
//	corrupt            the package FILE rows are re-homed onto the
//	                   double-spelled paths at the service seam — the exact
//	                   storage state every pre-T-371 v1 channel upload held
//	                   (the .timestamp and index.json rows were ALWAYS spec
//	                   and stay put) — and the D-F2 faces turn visible:
//	                   prefixed snapshot keys, empty ref-search settings;
//	sweep              SweepV1FilesLayout re-homes the tree;
//	install again      a cache-wiped install succeeds with the SAME cache
//	                   digests (the client's settings-matching leg rides
//	                   the recovered conaninfo fields — the `-q` data the
//	                   defect starved).
//
// Environment-gated like the matrix above (BINFLOW_T308_CLIENT1_E2E=1).
func TestConan1LayoutSweepRoundtrip(t *testing.T) {
	if os.Getenv("BINFLOW_T308_CLIENT1_E2E") != "1" {
		t.Skip("set BINFLOW_T308_CLIENT1_E2E=1 (with a conan 1.x on PATH) to run the conan 1 client matrix")
	}
	conanBin, _ := requireConan1(t)

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
	profiles := filepath.Join(home, ".conan", "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "default"), []byte(conan1Profile), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	run("remote", "add", "binflow", s.srv.URL+"/binflow/conan-local", "--force")
	run("user", "-p", adminPass, "-r", "binflow", adminUser)

	// ---- create + upload + the baseline install ----
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "conanfile.py"), []byte(conan1Recipe), 0o644); err != nil {
		t.Fatalf("write conanfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.txt"), []byte("sweep payload"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	run("create", src, "myuser/stable")
	run("upload", "hello/1.0@myuser/stable", "-r", "binflow", "--all", "--confirm")
	run("remove", "hello/1.0@myuser/stable", "-f")
	consumer := t.TempDir()
	install := func() string {
		t.Helper()
		cmd := exec.Command(conanBin, "install", "hello/1.0@myuser/stable", "-r", "binflow", "--build=missing")
		cmd.Dir = consumer
		cmd.Env = append(os.Environ(), "CONAN_USER_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("conan install: %v\n%s", err, out)
		}
		return string(out)
	}
	install()

	// cacheDigests snapshots the cached hello package's file digests (the
	// roundtrip-equivalence ledger — the downloaded bytes).
	cacheDigests := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		root := filepath.Join(home, ".conan", "data", "hello")
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || !info.Mode().IsRegular() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			out[rel] = shaOfFile(t, p)
			return nil
		})
		if err != nil {
			t.Fatalf("walk cache: %v", err)
		}
		return out
	}
	baseline := cacheDigests()
	if len(baseline) == 0 {
		t.Fatalf("baseline cache snapshot is empty")
	}

	// ---- corrupt onto the legacy double layout (the pre-T-371 storage
	// state): move every package FILE row under the pRev root onto the
	// double-spelled path, service seam (index.json and .timestamp were
	// always spec and stay). ----
	ctx := context.Background()
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	code, body, _ := s.get(v1("conan-local", "conans/hello/1.0/myuser/stable/search"))
	if code != http.StatusOK {
		t.Fatalf("ref search = (%d, %s)", code, body)
	}
	var meta map[string]*pkgMeta
	if err := json.Unmarshal([]byte(body), &meta); err != nil || len(meta) != 1 {
		t.Fatalf("ref search = (%d, %s): %v", code, body, err)
	}
	pid := ""
	for p := range meta {
		pid = p
	}
	pRevRoot := pkgFilePrefix(r.coordinateRoot(), revDefaultV1, pid, revDefaultV1)
	specRows, err := s.svc.List(ctx, adminPrincipal(), "conan-local", strings.TrimSuffix(pRevRoot, "/"))
	if err != nil {
		t.Fatalf("list pRev tree: %v", err)
	}
	moved := 0
	for _, n := range specRows {
		if n.Path == pRevRoot || strings.HasSuffix(n.Path, "/") || n.Path == pRevRoot+".timestamp" {
			continue // folder rows and the .timestamp stay where the legacy writer put them
		}
		rc, _, err := s.svc.Get(ctx, adminPrincipal(), "conan-local", n.Path)
		if err != nil {
			t.Fatalf("read %s: %v", n.Path, err)
		}
		raw, rerr := io.ReadAll(rc)
		_ = rc.Close()
		if rerr != nil {
			t.Fatalf("drain %s: %v", n.Path, rerr)
		}
		double := pRevRoot + dirPackage + "/" + pid + "/" + strings.TrimPrefix(n.Path, pRevRoot)
		if _, err := s.svc.Put(ctx, adminPrincipal(), "conan-local", double,
			bytes.NewReader(raw), storage.BlobRef{Sha256: n.Sha256}, "application/octet-stream"); err != nil {
			t.Fatalf("re-home %s: %v", double, err)
		}
		if err := s.svc.Delete(ctx, adminPrincipal(), "conan-local", n.Path); err != nil {
			t.Fatalf("drop spec row %s: %v", n.Path, err)
		}
		moved++
	}
	if moved == 0 {
		t.Fatalf("corruption moved nothing (fixture build failed)")
	}

	// The D-F2 faces are visible again on the legacy tree: prefixed
	// snapshot keys and the starved settings map.
	code, body, _ = s.get(v1("conan-local", "conans/hello/1.0/myuser/stable/packages/"+pid))
	if code != http.StatusOK {
		t.Fatalf("legacy package snapshot = (%d, %s)", code, body)
	}
	if !strings.Contains(body, `"package/`+pid+`/conaninfo.txt"`) {
		t.Errorf("legacy snapshot keys = %s, want the prefixed D-F2 spelling", body)
	}
	code, body, _ = s.get(v1("conan-local", "conans/hello/1.0/myuser/stable/search"))
	if code != http.StatusOK || !strings.Contains(body, `"settings":{}`) {
		t.Errorf("legacy ref search = (%d, %s), want the empty-settings D-F2 face", code, body)
	}

	// ---- sweep + the equivalent install ----
	if err := SweepV1FilesLayout(ctx, s.svc, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	code, body, _ = s.get(v1("conan-local", "conans/hello/1.0/myuser/stable/search"))
	if code != http.StatusOK || strings.Contains(body, `"settings":{}`) {
		t.Errorf("post-sweep ref search = (%d, %s), want recovered settings", code, body)
	}

	run("remove", "hello/1.0@myuser/stable", "-f")
	out := install()
	if !strings.Contains(out, "hello") && !strings.Contains(out, "Installed") {
		t.Fatalf("post-sweep install output lacks the installation:\n%s", out)
	}
	assertSameDigests(t, cacheDigests(), baseline)
}

// shaOfFile hashes one file's bytes.
func shaOfFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// assertSameDigests fails with the differing entries.
func assertSameDigests(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("cache snapshots differ in size: %d vs %d", len(got), len(want))
	}
	for k, v := range want {
		if gv, ok := got[k]; !ok {
			t.Errorf("post-sweep cache lacks %q (the roundtrip lost a file)", k)
		} else if gv != v {
			t.Errorf("post-sweep cache %q digest = %s, want the baseline %s", k, gv, v)
		}
	}
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

// TestConan1PackagesDeleteUnderscoreRef drives the real conan 1.66 client's
// packages-only remote remove against the `_/_` coordinate form (D-F). A
// ref without user/channel serializes as `_/_` on the v1 wire
// (client_routes: `ref.user or "_"`), and `conan remove <ref> -q ... -r` is
// the client's POST packages/delete leg: it lists the pids through the v1
// ref search, then posts the whole batch — a 404 there fails the command
// outright (remover raises NotFoundException on a concrete ref).
//
// Legs: anonymous-ref create/upload (the conan-2-shaped `_/_` coordinate,
// reached through the conan 1 wire), real-client packages-only remove on
// the `_/_` form, the same on the user/channel form, and the raw D-F wire
// (a batch mixing a live pid with a stale one) asserted at 200 + tree gone.
func TestConan1PackagesDeleteUnderscoreRef(t *testing.T) {
	if os.Getenv("BINFLOW_T308_CLIENT1_E2E") != "1" {
		t.Skip("set BINFLOW_T308_CLIENT1_E2E=1 (with a conan 1.x on PATH) to run the conan 1 client matrix")
	}
	conanBin, _ := requireConan1(t)

	s := newStack(t)
	s.seedRepo(t, "conan-local", repo.TypeLocal)
	home := t.TempDir()
	run := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := exec.Command(conanBin, args...)
		cmd.Env = append(os.Environ(), "CONAN_USER_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("conan %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	profiles := filepath.Join(home, ".conan", "profiles")
	if err := os.MkdirAll(profiles, 0o755); err != nil {
		t.Fatalf("mkdir profiles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profiles, "default"), []byte(conan1Profile), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	run(t, "remote", "add", "binflow", s.srv.URL+"/binflow/conan-local", "--force")
	run(t, "user", "-p", adminPass, "-r", "binflow", adminUser)

	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "conanfile.py"), []byte(conan1Recipe), 0o644); err != nil {
		t.Fatalf("write recipe: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "payload.txt"), []byte("anon payload"), 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	// pidOf reads the server's own v1 ref search for the named ref form.
	pidOf := func(t *testing.T, refPath string) string {
		t.Helper()
		code, body, _ := s.get(v1("conan-local", refPath+"/search"))
		if code != http.StatusOK {
			t.Fatalf("ref search %s = (%d, %s)", refPath, code, body)
		}
		var meta map[string]map[string]any
		if err := json.Unmarshal([]byte(body), &meta); err != nil || len(meta) == 0 {
			t.Fatalf("ref search %s body %q: %v", refPath, body, err)
		}
		for pid := range meta {
			return pid
		}
		return ""
	}

	for _, tc := range []struct {
		name   string // subtest label
		ref    string // client-side ref spelling
		v1Ref  string // v1 wire path (name/version/user/channel)
		userCh bool   // user/channel vs anonymous `_/_` spelling
	}{
		{name: "underscore", ref: "hello/1.0@_/_", v1Ref: "conans/hello/1.0/_/_"},
		{name: "user-channel", ref: "hello/1.0@myuser/stable", v1Ref: "conans/hello/1.0/myuser/stable", userCh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.userCh {
				run(t, "create", src, "myuser/stable")
			} else {
				run(t, "create", src)
			}
			out := run(t, "upload", tc.ref, "-r", "binflow", "--all", "--confirm")
			if !strings.Contains(out, "Uploaded conan recipe") {
				t.Fatalf("upload output lacks the recipe confirmation:\n%s", out)
			}
			// The `_/_` tree is real: the v1 snapshot answers through the
			// underscore coordinate.
			if code, body, _ := s.get(v1("conan-local", tc.v1Ref)); code != http.StatusOK ||
				!strings.Contains(body, "conanfile.py") {
				t.Fatalf("post-upload snapshot = (%d, %s)", code, body)
			}
			pid := pidOf(t, tc.v1Ref)

			// The client's packages-only remove: POST packages/delete
			// with the pid batch. A 404 here fails the command outright
			// (the pre-fix D-F posture).
			out = run(t, "remove", tc.ref, "-p", pid, "-r", "binflow", "-f")
			t.Logf("conan remove -p output:\n%s", out)
			if code, _, _ := s.get(v1("conan-local", tc.v1Ref+"/packages/"+pid)); code != http.StatusNotFound {
				t.Fatalf("post-remove package snapshot = %d, want 404", code)
			}
			// Packages-only: the recipe survives.
			if code, _, _ := s.get(v1("conan-local", tc.v1Ref)); code != http.StatusOK {
				t.Fatalf("post-remove recipe snapshot = %d, want 200", code)
			}

			// The raw D-F wire on the same tree shape: re-upload, then a
			// batch mixing the live pid with a stale one (the conan-2
			// coordinate's leftover-binary posture) — 200, live tree gone.
			run(t, "upload", tc.ref, "-r", "binflow", "--all", "--confirm")
			stale := strings.Repeat("ab", 20) // 40-hex pid with no tree
			code, body, _ := s.post(v1("conan-local", tc.v1Ref+"/packages/delete"),
				[]byte(`{"package_ids":["`+pid+`","`+stale+`"]}`), nil)
			if code != http.StatusOK || body != "" {
				t.Fatalf("mixed-batch packages/delete = (%d, %q), want (200, \"\")", code, body)
			}
			if code, _, _ = s.get(v1("conan-local", tc.v1Ref+"/packages/"+pid)); code != http.StatusNotFound {
				t.Fatalf("post-batch package snapshot = %d, want 404", code)
			}
			// Cleanup: drop the coordinate for the next subtest.
			if code, _, _ := s.delete(v1("conan-local", tc.v1Ref)); code != http.StatusOK {
				t.Fatalf("coordinate cleanup delete = %d, want 200", code)
			}
		})
	}
}
