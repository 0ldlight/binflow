package conan

// T-355A (FR-110.2 / T-340 AC1's tail, the D-5 carryover): the repo-config
// switch forceConanAuthentication. Spec conan.md section 2's auth gate: on a
// forced repository an anonymous request to ANY conan endpoint answers 401
// (v1/v2 data endpoints asserted one by one) with the client-guiding Basic
// challenge; the capability header family rides the refusal like every
// other response. Default false keeps the ordinary content-plane ACL
// (anonymous reads pass on an anonymous-enabled instance, writes always
// demanded credentials — TestAnonymousWriteRefused's posture).

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestForceConanAuthentication: the forced plane's per-endpoint 401 sweep,
// the authenticated pass-through, the default-false posture and the
// flip-off roundtrip.
func TestForceConanAuthentication(t *testing.T) {
	s := newStack(t)
	s.seedRepoCfg(t, "cn-forced", repo.TypeLocal, `{"forceConanAuthentication":true}`)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	if code, body, _ := s.putRecipeFile("cn-forced", r, fixtureRev(1), "conanfile.py", []byte("x")); code != http.StatusCreated {
		t.Fatalf("seed PUT = (%d, %s), want 201", code, body)
	}
	rev, pid := fixtureRev(1), fixturePID(1)

	// The per-endpoint sweep: every family answers 401 before any endpoint
	// logic runs (even the arms that would 404 missing content). The READ
	// arms meet the adapter gate's plane shape — body, Basic challenge and
	// the capability header family; the WRITE arms are refused one layer
	// earlier by the anonymous-write middleware (writes are never anonymous
	// whatever the switch says — its JSON envelope is that plane's shape),
	// so only the status is asserted there.
	for _, tt := range []struct {
		name   string
		method string
		path   string
		write  bool
	}{
		{"v1 ping", http.MethodGet, v1("cn-forced", "ping"), false},
		{"v2 ping", http.MethodGet, repoPath("cn-forced") + "/" + segV2 + "/" + segPing, false},
		{"v1 check_credentials", http.MethodGet, v1("cn-forced", "users/check_credentials"), false},
		{"v1 search", http.MethodGet, v1("cn-forced", "conans/search"), false},
		{"v1 recipe snapshot", http.MethodGet, v1("cn-forced", "conans/hello/1.0/myuser/stable"), false},
		{"v1 package snapshot", http.MethodGet, v1("cn-forced", "conans/hello/1.0/myuser/stable/packages/"+pid), false},
		{"v1 digest", http.MethodGet, v1("cn-forced", "conans/hello/1.0/myuser/stable/digest"), false},
		{"v1 download_urls", http.MethodGet, v1("cn-forced", "conans/hello/1.0/myuser/stable/download_urls"), false},
		{"v1 upload_urls", http.MethodPost, v1("cn-forced", "conans/hello/1.0/myuser/stable/upload_urls"), true},
		{"v1 files", http.MethodGet, v1("cn-forced", "files/myuser/hello/1.0/stable/export/conanfile.py"), false},
		{"v1 packages/delete", http.MethodPost, v1("cn-forced", "conans/hello/1.0/myuser/stable/packages/delete"), true},
		{"v2 search", http.MethodGet, v2("cn-forced", "search?q=hello*"), false},
		{"v2 latest", http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/latest"), false},
		{"v2 revisions", http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/revisions"), false},
		{"v2 files list", http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/revisions/"+rev+"/files"), false},
		{"v2 file get", http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/revisions/"+rev+"/files/conanfile.py"), false},
		{"v2 file put", http.MethodPut, v2("cn-forced", "hello/1.0/myuser/stable/revisions/"+rev+"/files/other.txt"), true},
		{"v2 package latest", http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/revisions/"+rev+"/packages/"+pid+"/latest"), false},
		{"v2 revision delete", http.MethodDelete, v2("cn-forced", "hello/1.0/myuser/stable/revisions/"+rev), true},
	} {
		code, body, hdr := s.do(tt.method, tt.path, "", "", nil, nil)
		if code != http.StatusUnauthorized {
			t.Errorf("%s: = %d (body %s), want 401", tt.name, code, body)
			continue
		}
		if tt.write {
			continue
		}
		if body != "unauthorized user" {
			t.Errorf("%s: body = %q, want the plane's 401 wording", tt.name, body)
		}
		if got := hdr.Get("WWW-Authenticate"); got != `Basic realm="BinFlow Realm"` {
			t.Errorf("%s: WWW-Authenticate = %q, want the Basic challenge", tt.name, got)
		}
		if hdr.Get(hdrServerCaps) == "" {
			t.Errorf("%s: the capability family must ride the 401 (spec section 2)", tt.name)
		}
	}

	// Credential-carrying traffic is untouched on a forced repository: the
	// read answers its ordinary 200 (the seeded revision document).
	code, body, _ := s.do(http.MethodGet, v2("cn-forced", "hello/1.0/myuser/stable/latest"),
		adminUser, adminPass, nil, nil)
	if code != http.StatusOK || body == "" {
		t.Fatalf("authenticated latest = (%d, %s), want 200 with the revision document", code, body)
	}

	// Default false (no key): the anonymous read plane keeps passing — the
	// T-351 D-5 posture "字段缺省 = 普通内容面 ACL" stays observable.
	s.seedRepo(t, "cn-open", repo.TypeLocal)
	if code, _, _ := s.putRecipeFile("cn-open", r, fixtureRev(1), "conanfile.py", []byte("x")); code != http.StatusCreated {
		t.Fatalf("seed PUT (open) = %d, want 201", code)
	}
	if code, _, _ := s.get(v2("cn-open", "hello/1.0/myuser/stable/latest")); code != http.StatusOK {
		t.Errorf("anonymous latest (default false) = %d, want 200", code)
	}

	// The flip-off roundtrip (T-340 AC1's "关闭往返"): an explicit false
	// reopens the plane — the operator's disable update works only because
	// the config value survives as false, never collapses to absent.
	s.setRepoConfig(t, "cn-forced", `{"forceConanAuthentication":false}`)
	if code, _, _ := s.get(v2("cn-forced", "hello/1.0/myuser/stable/latest")); code != http.StatusOK {
		t.Errorf("anonymous latest after flip-off = %d, want 200", code)
	}
}

// TestForceConanAuthenticationOtherClasses: the gate reads the stored blob
// on every class it serves. The REST transport lands the key on the LOCAL
// arm only (remote/virtual canonical forms drop unknown fields by design),
// so the arm that can ever fire off-local is the hand-seeded blob — pinned
// here so the enforcement contract is class-complete even though the
// product's write plane cannot produce it.
func TestForceConanAuthenticationOtherClasses(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-member", repo.TypeLocal)
	s.seedVirtualRepo(t, "cn-virt-forced", []string{"cn-member"}, "")
	s.setRepoConfig(t, "cn-virt-forced", `{"repositories":["cn-member"],"forceConanAuthentication":true}`)

	if code, body, hdr := s.get(v2("cn-virt-forced", "search?q=*")); code != http.StatusUnauthorized || body != "unauthorized user" {
		t.Errorf("forced virtual search = (%d, %q), want the 401 shape", code, body)
	} else if hdr.Get("WWW-Authenticate") == "" {
		t.Errorf("forced virtual 401 lacks the Basic challenge")
	}
	// ... while the handshake's authenticated arm stays the class face it
	// always was (the only_v2 capability row).
	if code, _, _ := s.do(http.MethodGet, v1("cn-virt-forced", "ping"), adminUser, adminPass, nil, nil); code != http.StatusOK {
		t.Errorf("authenticated ping on forced virtual = %d, want 200", code)
	}
}

// TestForceConanAuthenticationMalformedBlob: a hand-mangled blob the probe
// cannot read degrades to false — the read side's own defense, matching the
// deb normalized() posture (the write side's type gate lives in
// repo.validateLocalConfig; httpapi names the field on a mistyped PUT).
func TestForceConanAuthenticationMalformedBlob(t *testing.T) {
	s := newStack(t)
	s.seedRepoCfg(t, "cn-mangled", repo.TypeLocal, `{"forceConanAuthentication":"yes"}`)
	if code, _, _ := s.get(v1("cn-mangled", "ping")); code != http.StatusOK {
		t.Errorf("malformed switch: ping = %d, want 200 (unreadable = false)", code)
	}
}

// TestClientForcedAuthentication (T-355A; gate BINFLOW_T355A_CLIENT_E2E=1
// with a conan 2.x on PATH): the REAL client meets the forced plane. Legs:
// the anonymous read dies on the gate's 401 (a non-TTY run cannot answer
// the challenge); the login leg passes — the authenticate trio carries the
// credential past the gate by design; the previously failing read then
// succeeds and sees the seeded revision.
func TestClientForcedAuthentication(t *testing.T) {
	if os.Getenv("BINFLOW_T355A_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T355A_CLIENT_E2E=1 (with a conan 2.x on PATH) to run the forced-plane client leg")
	}
	conanBin := requireConan(t)
	if v := conanVersion(t, conanBin); !strings.Contains(v, "version 2.") {
		t.Fatalf("conan 2.x required, found %q", v)
	}

	s := newStack(t)
	s.seedRepoCfg(t, "conan-forced", repo.TypeLocal, `{"forceConanAuthentication":true}`)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	if code, body, _ := s.putRecipeFile("conan-forced", r, fixtureRev(1), "conanfile.py", []byte("x")); code != http.StatusCreated {
		t.Fatalf("seed PUT = (%d, %s), want 201", code, body)
	}
	remote := s.srv.URL + "/binflow/conan-forced"

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

	run("profile", "detect", "--force")
	run("remote", "add", "binflow", remote, "--force")

	// The anonymous leg: the client has no credential to offer. conan 2
	// meets the gate's 401 challenge, reports "needs authentication" and
	// turns to its interactive credential prompt — the client-guiding
	// login the AC names; with no TTY the prompt EOFs and the JSON face
	// reports the per-remote error (exit code 0, the soft-error posture).
	// The OUTPUT is therefore the assertion surface, not the exit code.
	out := run("list", "hello/1.0@myuser/stable#*", "-r", "binflow", "--format=json")
	if !strings.Contains(out, "needs authentication") ||
		!strings.Contains(out, "EOF when reading a line") {
		t.Fatalf("anonymous list output does not show the guided-login demand:\n%s", out)
	}

	// The login leg walks the handshake trio — authenticate carries Basic
	// credentials, so the gate passes it — and stores the credential.
	run("remote", "login", "binflow", adminUser, "-p", adminPass)

	// The same read now succeeds and sees the seeded revision.
	listing := run("list", "hello/1.0@myuser/stable#*", "-r", "binflow", "--format=json")
	if !strings.Contains(listing, "myuser") || !strings.Contains(listing, fixtureRev(1)[:16]) {
		t.Fatalf("authenticated list output lacks the seeded revision:\n%s", listing)
	}
}
