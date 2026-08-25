package goproxy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The local-face protocol legs (goproxy.md sections 2/4/5): the PUT trio's
// validation chain, the GET trio's byte-for-byte contract with the synthesis
// chains, @v/list's zip-only aggregation, @latest's candidate order, the
// HEAD .mod probe and the wire's content types.

// putModule stores one version trio through the adapter.
func putModule(s *stack, t *testing.T, module, version string, mod, info string, zip []byte) {
	t.Helper()
	base := "/binflow/go-local/" + module + "/@v/" + version
	if status, body, _ := s.put(base+".zip", zip, nil); status != http.StatusCreated {
		t.Fatalf("PUT .zip: status %d, body %s", status, body)
	}
	if status, body, _ := s.put(base+".mod", []byte(mod), nil); status != http.StatusCreated {
		t.Fatalf("PUT .mod: status %d, body %s", status, body)
	}
	if status, body, _ := s.put(base+".info", []byte(info), nil); status != http.StatusCreated {
		t.Fatalf("PUT .info: status %d, body %s", status, body)
	}
}

// TestLocalTrioRoundtrip: PUT then GET is byte-for-byte, with the content
// types and checksum headers of the endpoint table.
func TestLocalTrioRoundtrip(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)

	zipBody := []byte("PK\x03\x04zip-fixture")
	modBody := "module example.com/mymod\n\ngo 1.21\n"
	infoBody := `{"Version":"v1.0.2","Time":"2024-01-02T03:04:05Z"}`
	putModule(s, t, "example.com/mymod", "v1.0.2", modBody, infoBody, zipBody)

	cases := []struct {
		ext   string
		body  string
		ctype string
	}{
		{ext: "zip", body: string(zipBody), ctype: "application/zip"},
		{ext: "mod", body: modBody, ctype: "text/plain; charset=utf-8"},
		{ext: "info", body: infoBody, ctype: "application/json"},
	}
	for _, c := range cases {
		status, body, hdr := s.get("/binflow/go-local/example.com/mymod/@v/v1.0.2." + c.ext)
		if status != http.StatusOK {
			t.Fatalf("GET .%s: status %d, body %s", c.ext, status, body)
		}
		if body != c.body {
			t.Errorf("GET .%s body = %q, want %q (byte-for-byte)", c.ext, body, c.body)
		}
		if got := hdr.Get("Content-Type"); got != c.ctype {
			t.Errorf("GET .%s Content-Type = %q, want %q", c.ext, got, c.ctype)
		}
		sum := sha256.Sum256([]byte(c.body))
		if got := hdr.Get("X-Checksum-Sha256"); got != hex.EncodeToString(sum[:]) {
			t.Errorf("GET .%s X-Checksum-Sha256 = %q, want the measured digest", c.ext, got)
		}
	}
}

// TestPutValidationChain: the 400 family of section 5 — version grammar,
// module-major consistency, .info JSON contract, .mod module identity.
func TestPutValidationChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	base := "/binflow/go-local/example.com/mymod/@v/"

	bad := []struct {
		path string
		body string
		want string
	}{
		{path: base + "1.0.0.zip", body: "x", want: "invalid version"},
		{path: base + "main.mod", body: "module example.com/mymod\n", want: "invalid version"},
		{path: "/binflow/go-local/example.com/mymod/v2/@v/v1.0.0.info", body: `{"Version":"v1.0.0"}`, want: "major version"},
		{path: base + "v1.0.0.info", body: `{"Version":"v1.0.1"}`, want: "does not match"},
		{path: base + "v1.0.0.info", body: `{"Version":"main"}`, want: "not a canonical version"},
		{path: base + "v1.0.0.info", body: `{"Time":"2024-01-02T03:04:05Z"}`, want: "Version is required"},
		{path: base + "v1.0.0.info", body: `{"Version":"v1.0.0","Time":"yesterday"}`, want: "RFC 3339"},
		{path: base + "v1.0.0.info", body: `not json`, want: "not a JSON object"},
		{path: base + "v1.0.0.mod", body: "module example.com/other\n", want: "does not match"},
		{path: base + "v1.0.0.mod", body: "go 1.21\n", want: "no module directive"},
	}
	for _, c := range bad {
		status, body, _ := s.put(c.path, []byte(c.body), nil)
		if status != http.StatusBadRequest {
			t.Errorf("PUT %s: status %d, want 400 (body %s)", c.path, status, body)
			continue
		}
		if !strings.Contains(body, c.want) {
			t.Errorf("PUT %s: body %q does not contain %q", c.path, body, c.want)
		}
	}
}

// TestPutChecksumChain: malformed declared digests are 400, a well-formed
// disagreement is 409 (the client-checksums policy).
func TestPutChecksumChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	path := "/binflow/go-local/example.com/mymod/@v/v1.0.0.zip"

	status, body, _ := s.put(path, []byte("x"), map[string]string{"X-Checksum-Sha256": "nothex"})
	if status != http.StatusBadRequest || !strings.Contains(body, "X-Checksum-Sha256") {
		t.Errorf("malformed sha256: (%d, %s), want 400 naming the header", status, body)
	}
	status, body, _ = s.put(path, []byte("x"), map[string]string{"X-Checksum-Sha256": strings.Repeat("a", 64)})
	if status != http.StatusConflict || !strings.Contains(body, "Checksum error") {
		t.Errorf("checksum mismatch: (%d, %s), want 409", status, body)
	}
	// The agreeing declaration lands (and is idempotent on retransmit).
	sum := sha256.Sum256([]byte("x"))
	good := map[string]string{"X-Checksum-Sha256": hex.EncodeToString(sum[:])}
	if status, body, _ := s.put(path, []byte("x"), good); status != http.StatusCreated {
		t.Errorf("agreeing checksum: (%d, %s), want 201", status, body)
	}
	if status, body, _ := s.put(path, []byte("x"), good); status != http.StatusCreated {
		t.Errorf("idempotent retransmit: (%d, %s), want 201", status, body)
	}
}

// TestUppercaseModuleRoundtrip: the !lower three-state through the full
// stack — wire escaped, storage decoded, GET both spellings serves the same
// bytes (the FR-87 case example.com/Upper/Mod).
func TestUppercaseModuleRoundtrip(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)

	putModule(s, t, "example.com/Upper/Mod", "v1.0.0",
		"module example.com/Upper/Mod\n", `{"Version":"v1.0.0"}`, []byte("PK"))

	for _, wire := range []string{
		"/binflow/go-local/example.com/!upper/!mod/@v/v1.0.0.info", // escaped
		"/binflow/go-local/example.com/Upper/Mod/@v/v1.0.0.info",   // decoded (curl convenience)
	} {
		status, body, _ := s.get(wire)
		if status != http.StatusOK || body != `{"Version":"v1.0.0"}` {
			t.Errorf("GET %s = (%d, %s), want the stored info", wire, status, body)
		}
	}
	// The list serves the module under its escaped spelling too.
	status, body, _ := s.get("/binflow/go-local/example.com/!upper/!mod/@v/list")
	if status != http.StatusOK || !strings.Contains(body, "v1.0.0") {
		t.Errorf("escaped list = (%d, %s)", status, body)
	}
}

// TestLocalListAggregation: zip-only registration (a version whose .mod and
// .info exist but no .zip stays out), sorted output, the empty-list 404
// tightening (S13), and pseudo-versions absent from a PUT-driven register
// by construction.
func TestLocalListAggregation(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)

	putModule(s, t, "example.com/mymod", "v1.1.0", "module example.com/mymod\n", `{"Version":"v1.1.0"}`, []byte("PK2"))
	putModule(s, t, "example.com/mymod", "v1.0.0", "module example.com/mymod\n", `{"Version":"v1.0.0"}`, []byte("PK1"))
	// A neighbor module must not leak into the list.
	putModule(s, t, "example.com/other", "v9.9.9", "module example.com/other\n", `{"Version":"v9.9.9"}`, []byte("PK3"))

	status, body, _ := s.get("/binflow/go-local/example.com/mymod/@v/list")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, body %s", status, body)
	}
	if body != "v1.0.0\nv1.1.0\n" {
		t.Errorf("list body = %q, want the sorted pure version column", body)
	}

	// Zip-only: dropping the .zip node deregisters the version even though
	// its .mod/.info remain (the wire face carries no DELETE verb — the
	// endpoint table is GET/HEAD/PUT only — so the service face does it).
	if err := s.svc.Delete(t.Context(), adminPrincipal(), "go-local", "example.com/mymod/@v/v1.1.0.zip"); err != nil {
		t.Fatalf("svc.Delete .zip: %v", err)
	}
	_, body, _ = s.get("/binflow/go-local/example.com/mymod/@v/list")
	if strings.Contains(body, "v1.1.0") {
		t.Errorf("list after zip delete still contains v1.1.0: %q", body)
	}

	// Unknown module: the S13 404.
	status, body, _ = s.get("/binflow/go-local/example.com/nosuch/@v/list")
	if status != http.StatusNotFound || strings.TrimSpace(body) != "not found" {
		t.Errorf("unknown module list = (%d, %q), want (404, not found)", status, body)
	}
}

// TestLocalLatestCandidateOrder: release beats pre-release beats pseudo
// (the section 4.5 order) over the registered zip set.
func TestLocalLatestCandidateOrder(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	putModule(s, t, "example.com/mymod", "v1.0.0-beta.1", "module example.com/mymod\n", `{"Version":"v1.0.0-beta.1"}`, []byte("PKb"))
	putModule(s, t, "example.com/mymod", "v0.0.0-20180102030405-0123456789ab", "module example.com/mymod\n", `{"Version":"v0.0.0-20180102030405-0123456789ab"}`, []byte("PKp"))

	status, body, _ := s.get("/binflow/go-local/example.com/mymod/@latest")
	if status != http.StatusOK || !strings.Contains(body, `"Version":"v1.0.0-beta.1"`) {
		t.Errorf("latest over {pre, pseudo} = (%d, %s), want the pre-release winner", status, body)
	}

	// A release anywhere in the set wins outright.
	putModule(s, t, "example.com/mymod", "v0.9.0", "module example.com/mymod\n", `{"Version":"v0.9.0"}`, []byte("PKr"))
	_, body, _ = s.get("/binflow/go-local/example.com/mymod/@latest")
	if !strings.Contains(body, `"Version":"v0.9.0"`) {
		t.Errorf("latest over {release, pre, pseudo} = %s, want the release winner", body)
	}
}

// TestInfoSynthesisChain: a version with a .zip but no .info serves the
// synthesized minimal body and writes the copy back (the second GET serves
// the stored copy — identical bytes).
func TestInfoSynthesisChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	base := "/binflow/go-local/example.com/mymod/@v/v1.2.3"
	if status, body, _ := s.put(base+".zip", []byte("PK"), nil); status != http.StatusCreated {
		t.Fatalf("PUT .zip: %d %s", status, body)
	}
	if status, body, _ := s.put(base+".mod", []byte("module example.com/mymod\n"), nil); status != http.StatusCreated {
		t.Fatalf("PUT .mod: %d %s", status, body)
	}

	wantPrefix := `{"Version":"v1.2.3","Time":"`
	for i := 0; i < 2; i++ {
		status, body, hdr := s.get(base + ".info")
		if status != http.StatusOK {
			t.Fatalf("synthesized GET #%d: status %d, body %s", i, status, body)
		}
		if !strings.HasPrefix(body, wantPrefix) {
			t.Errorf("synthesized body #%d = %s, want the %q prefix", i, body, wantPrefix)
		}
		if got := hdr.Get("Content-Type"); got != "application/json" {
			t.Errorf("synthesized Content-Type = %q", got)
		}
	}
	// The write-back rides the trigger principal's write grant: an
	// authenticated read persists the synthesized copy, an anonymous read
	// re-synthesizes the same deterministic body.
	status, body, _ := s.do(http.MethodGet, base+".info", adminUser, adminPass, nil, nil)
	if status != http.StatusOK || !strings.HasPrefix(body, wantPrefix) {
		t.Fatalf("authenticated synthesized GET = (%d, %s)", status, body)
	}
	// The write-back landed a node at the .info path (svc.List normalizes
	// the trailing-slash prefix; the raw store seam does not).
	nodes, err := s.svc.List(t.Context(), adminPrincipal(), "go-local", "example.com/mymod/@v/")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Path == "example.com/mymod/@v/v1.2.3.info" {
			found = true
		}
	}
	if !found {
		t.Error("the synthesized .info was not written back to storage")
	}
}

// TestModIncompatibleSynthesis: a +incompatible version without a .mod gets
// the single module line — on local and on remote alike (the remote arm is
// remote_test.go's no-upstream leg).
func TestModIncompatibleSynthesis(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	base := "/binflow/go-local/example.com/legacy/@v/v1.0.0+incompatible"
	if status, body, _ := s.put(base+".zip", []byte("PK"), nil); status != http.StatusCreated {
		t.Fatalf("PUT +incompatible .zip: %d %s", status, body)
	}
	status, body, _ := s.get(base + ".mod")
	if status != http.StatusOK || body != "module example.com/legacy\n" {
		t.Errorf("+incompatible .mod = (%d, %q), want the synthesized module line", status, body)
	}
	// A stored .mod wins over the synthesis.
	if status, body, _ := s.put(base+".mod", []byte("module example.com/legacy\n\ngo 1.16\n"), nil); status != http.StatusCreated {
		t.Fatalf("PUT stored .mod: %d %s", status, body)
	}
	_, body, _ = s.get(base + ".mod")
	if body != "module example.com/legacy\n\ngo 1.16\n" {
		t.Errorf("stored +incompatible .mod = %q, want the stored copy", body)
	}
}

// TestHeadModProbe: HEAD .mod answers 200 + Content-Length on a hit and
// 410 Gone on a miss (the endpoint table's HEAD row / S9).
func TestHeadModProbe(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)
	putModule(s, t, "example.com/mymod", "v1.0.0", "module example.com/mymod\n", `{"Version":"v1.0.0"}`, []byte("PK"))

	status, _, hdr := s.do(http.MethodHead, "/binflow/go-local/example.com/mymod/@v/v1.0.0.mod", "", "", nil, nil)
	if status != http.StatusOK || hdr.Get("Content-Length") == "" {
		t.Errorf("HEAD hit = %d (len %q), want 200 + Content-Length", status, hdr.Get("Content-Length"))
	}
	status, _, _ = s.do(http.MethodHead, "/binflow/go-local/example.com/mymod/@v/v9.9.9.mod", "", "", nil, nil)
	if status != http.StatusGone {
		t.Errorf("HEAD miss = %d, want 410", status)
	}
}

// TestRepoRootProbeAndUnknownPaths: the 200 root probe, the 404 unknown
// shapes (sumdb family included) and the protocol's text/plain errors.
func TestRepoRootProbeAndUnknownPaths(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-local", repo.TypeLocal)

	status, body, hdr := s.get("/binflow/go-local/")
	if status != http.StatusOK || body != "" {
		t.Errorf("root probe = (%d, %q), want 200 empty", status, body)
	}
	if got := hdr.Get("Content-Type"); strings.HasPrefix(got, "text/plain") {
		t.Errorf("root probe Content-Type = %q", got)
	}
	for _, p := range []string{
		"/binflow/go-local/sumdb/sum.golang.org/supported",
		"/binflow/go-local/sumdb/sum.golang.org/lookup/example.com/m@v1.0.0",
		"/binflow/go-local/example.com/mymod",
		"/binflow/go-local/example.com/mymod/@v/v1.0.0.txt",
	} {
		status, body, hdr := s.get(p)
		if status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, status)
			continue
		}
		if !strings.HasPrefix(hdr.Get("Content-Type"), "text/plain") {
			t.Errorf("GET %s error Content-Type = %q, want text/plain", p, hdr.Get("Content-Type"))
		}
		_ = body // the go command only inspects statuses; body checked for type only
	}
	// The !! escape is the 400 family.
	status, _, _ = s.get("/binflow/go-local/example.com/!!mod/@v/v1.0.0.zip")
	if status != http.StatusBadRequest {
		t.Errorf("!! escape GET = %d, want 400", status)
	}
}

// TestPutOnRemoteRefused: the remote class refuses writes with the
// service's read-only 405 (RE-05 — section 6.2 defers to the existing
// BinFlow face).
func TestPutOnRemoteRefused(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "go-remote", repo.TypeRemote)
	s.seedRemoteConfig(t, "go-remote", "https://upstream.example")
	status, body, _ := s.put("/binflow/go-remote/example.com/m/@v/v1.0.0.zip", []byte("PK"), nil)
	if status != http.StatusMethodNotAllowed {
		t.Errorf("PUT on remote = (%d, %s), want 405", status, body)
	}
}
