package cargo

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The T-316 real-client matrix (cargo 1.98, environment-gated): the CG-2
// publish arms over the LOCAL repository and the full remote chain over a
// BinFlow self-referential upstream.
//
//	CG-2 success arm    cargo publish → 200, warnings-only body, the index
//	                    row reconciles (cksum == the downloaded bytes)
//	CG-2 overwrite arm  a second publish of the SAME version (the
//	                    principal may delete) → the overwrite 200
//	CG-2 failure arm A  a bogus credential → 401 (the permission track)
//	CG-2 failure arm B  an oversized description (>1KiB property value) →
//	                    200 + warnings.other carrying the failure string —
//	                    the exact Artifactory wire cargo renders as a
//	                    WARNING, exit 0
//	remote chain        config.json self-pointing + config.original.json
//	                    preserved; cargo add/build resolves and downloads
//	                    through the remote (MISS→HIT); search proxies; the
//	                    upstream-deleted cache proof (zero upstream traffic)
//
// It is environment-gated (BINFLOW_T316_CLIENT_E2E=1) with a cargo
// toolchain on PATH; the gate skips silently so toolchain-less CI stays
// green. The ticket log records a full gated run.

func TestCargoClientCG2Arms(t *testing.T) {
	if os.Getenv("BINFLOW_T316_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T316_CLIENT_E2E=1 (with a cargo toolchain on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Fatalf("cargo toolchain unavailable on PATH: %v", err)
	}

	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	reg := s.srv.URL + "/binflow/cargo-local"

	issued, err := s.auth.Issue(t.Context(), adminUser, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	env := cargoEnv(t, reg, issued.AccessToken)
	work := t.TempDir()

	// ---- the success arm ----
	crateDir := filepath.Join(work, "okay")
	runCargo(t, env, work, "new", "okay", "--lib", "--vcs", "none")
	writeCrateManifest(t, crateDir, "okay", "0.1.0", "binflow cg2 success crate")
	out := runCargo(t, env, crateDir, "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("CG-2 success arm: %s", oneLine(out))
	status, body, _ := s.get("/binflow/cargo-local/index/ok/ay/okay")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) || strings.Contains(body, `"errors"`) {
		t.Fatalf("success-arm index = (%d, %s)", status, body)
	}

	// ---- the overwrite arm (a duplicate the principal may delete) ----
	// Observed with cargo 1.98: the client pre-flights against the sparse
	// index and refuses a duplicate LOCALLY ("already exists on `binflow`
	// index") — it never sends the second PUT. The server-side overwrite
	// (D-3: no conflict arm, a deletable duplicate is overwritten) is
	// therefore driven by replaying the publish wire directly; the
	// client-side refusal is logged as the ticket's client-behavior
	// finding: the Artifactory overwrite arm is invisible to a modern
	// cargo whose index read succeeds.
	out, exitErr := runCargoErr(t, env, crateDir, "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("CG-2 overwrite arm (client pre-flight): exit=%v out=%s", exitErr != nil, oneLine(out))
	if exitErr == nil || !strings.Contains(out, "already exists") {
		t.Fatalf("overwrite pre-flight: expected cargo's local duplicate refusal, got:\n%s", out)
	}
	status, respBody, _ := s.put("/binflow/cargo-local/api/v1/crates/new",
		publishBody(`{"name":"okay","vers":"0.1.0","description":"overwritten"}`, fixtureCrate("okay", "0.1.0")), nil)
	if status != http.StatusOK {
		t.Fatalf("overwrite arm: the replayed publish = %d (%s), want the overwrite 200", status, respBody)
	}
	if strings.Contains(respBody, `"errors"`) {
		t.Fatalf("overwrite arm body = %s: the errors key must never appear", respBody)
	}

	// ---- failure arm A: the permission track (bogus credential) ----
	// A FRESH crate name (the pre-flight duplicate check must not fire);
	// the client sends the PUT with the bogus bearer and the server's 401
	// must fail the publish client-side.
	nopeDir := filepath.Join(work, "nope")
	runCargo(t, env, work, "new", "nope", "--lib", "--vcs", "none")
	writeCrateManifest(t, nopeDir, "nope", "0.1.0", "the unauthenticated crate")
	badEnv := cargoEnv(t, reg, "not-a-real-token")
	out, exitErr = runCargoErr(t, badEnv, nopeDir, "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("CG-2 arm A (401): exit=%v out=%s", exitErr != nil, oneLine(out))
	if exitErr == nil {
		t.Fatalf("arm A: the bogus-credential publish must fail client-side, output:\n%s", out)
	}
	if !strings.Contains(out, "401") && !strings.Contains(strings.ToLower(out), "unauthorized") {
		t.Fatalf("arm A: cargo must surface the server's 401, got:\n%s", out)
	}

	// ---- failure arm B: the 200 + warnings.other track ----
	// A description past the 1KiB property-value ceiling dies in the
	// landing validation — the parse family's CG-2 track. The server
	// answers 200 + warnings.other; cargo renders the string as a warning
	// and exits 0 (the Artifactory wire's observable quirk).
	bigDir := filepath.Join(work, "toobig")
	runCargo(t, env, work, "new", "toobig", "--lib", "--vcs", "none")
	writeCrateManifest(t, bigDir, "toobig", "0.1.0", strings.Repeat("oversized ", 200))
	out, exitErr = runCargoErr(t, env, bigDir, "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("CG-2 arm B (200+warnings.other): exit=%v out=%s", exitErr != nil, oneLine(out))
	if !strings.Contains(out, "Failed to publish with error") {
		t.Fatalf("arm B: cargo output must carry the warnings.other string, got:\n%s", out)
	}
	if exitErr != nil {
		t.Fatalf("arm B: cargo must treat the 200+warnings body as success, got exit error:\n%s", out)
	}
	// Nothing landed.
	status, body, _ = s.get("/binflow/cargo-local/index/to/ob/toobig")
	if status != http.StatusNotFound {
		t.Fatalf("arm B: the refused crate must leave no index row (got %d, %s)", status, body)
	}
}

func TestCargoClientRemoteChain(t *testing.T) {
	if os.Getenv("BINFLOW_T316_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T316_CLIENT_E2E=1 (with a cargo toolchain on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Fatalf("cargo toolchain unavailable on PATH: %v", err)
	}

	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	s.seedRepo(t, "cargo-remote", repo.TypeRemote)
	// The self-referential upstream: the remote proxies this stack's own
	// local cargo repository.
	s.seedRemoteConfig(t, "cargo-remote", s.srv.URL+"/binflow/cargo-local")
	reg := s.srv.URL + "/binflow/cargo-remote"

	issued, err := s.auth.Issue(t.Context(), adminUser, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	// Publish into the UPSTREAM through the real client.
	upEnv := cargoEnv(t, s.srv.URL+"/binflow/cargo-local", issued.AccessToken)
	work := t.TempDir()
	runCargo(t, upEnv, work, "new", "proxied", "--lib", "--vcs", "none")
	writeCrateManifest(t, filepath.Join(work, "proxied"), "proxied", "0.3.0", "the upstream crate")
	out := runCargo(t, upEnv, filepath.Join(work, "proxied"), "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("R upstream publish: %s", oneLine(out))

	// ---- the remote entry documents ----
	status, body, _ := s.get("/binflow/cargo-remote/index/config.json")
	if status != http.StatusOK || !strings.Contains(body, `"`+reg+`/v1/crates"`) {
		t.Fatalf("R config.json = (%d, %s), want dl self-pointing at the remote", status, body)
	}
	status, body, _ = s.get("/binflow/cargo-remote/" + fileOriginalConfig)
	if status != http.StatusOK || !strings.Contains(body, "/binflow/cargo-local/v1/crates") {
		t.Fatalf("R config.original.json = (%d, %s), want the upstream form preserved", status, body)
	}

	// ---- resolve and build THROUGH the remote ----
	remoteEnv := cargoEnv(t, reg, issued.AccessToken)
	consumer := filepath.Join(work, "consumer")
	runCargo(t, remoteEnv, work, "new", "consumer", "--vcs", "none")
	out = runCargo(t, remoteEnv, consumer, "add", "proxied", "--registry", "binflow")
	t.Logf("R cargo add: %s", oneLine(out))
	out = runCargo(t, remoteEnv, consumer, "build")
	t.Logf("R cargo build: %s", oneLine(out))

	// The download crossed the cache: MISS first, HIT after.
	status, _, hdr := s.get("/binflow/cargo-remote/v1/crates/proxied/0.3.0/download")
	if status != http.StatusOK {
		t.Fatalf("R download = %d", status)
	}
	if got := hdr.Get("X-BinFlow-Cache"); got != "HIT" {
		t.Fatalf("R post-build download cache state = %q, want HIT (the build warmed it)", got)
	}

	// ---- search through the remote (proxied from the upstream's facts) ----
	status, body, _ = s.get("/binflow/cargo-remote/api/v1/crates?q=proxied&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"proxied"`) {
		t.Fatalf("R search = (%d, %s)", status, body)
	}
	var resp struct {
		Crates []struct {
			Name string `json:"name"`
		} `json:"crates"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil || len(resp.Crates) != 1 {
		t.Fatalf("R search body = %s (%v)", body, err)
	}

	// ---- THE cache proof: delete the crate upstream, keep serving ----
	status, _, _ = s.delete("/binflow/cargo-local/" + cratePath("proxied", "0.3.0"))
	if status != http.StatusNoContent {
		t.Fatalf("R upstream delete = %d, want 204", status)
	}
	status, _, _ = s.get("/binflow/cargo-remote/v1/crates/proxied/0.3.0/download")
	if status != http.StatusOK {
		t.Fatalf("R post-deletion download = %d, want the cached copy", status)
	}
	if _, _, dh := s.get("/binflow/cargo-remote/v1/crates/proxied/0.3.0/download"); dh.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("R post-deletion download must be a cache HIT")
	}
	status, body, _ = s.get("/binflow/cargo-remote/index/pr/ox/proxied")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.3.0"`) {
		t.Fatalf("R post-deletion index = (%d, %s), want the cached row", status, body)
	}

	// A fresh consumer build still resolves from the cache alone (the
	// upstream no longer holds the crate).
	fresh := filepath.Join(work, "fresh")
	runCargo(t, remoteEnv, work, "new", "fresh", "--vcs", "none")
	out = runCargo(t, remoteEnv, fresh, "add", "proxied", "--registry", "binflow")
	t.Logf("R post-deletion cargo add: %s", oneLine(out))
	out = runCargo(t, remoteEnv, fresh, "build")
	t.Logf("R post-deletion cargo build: %s", oneLine(out))
}

// runCargoErr is runCargo without the fatal-on-error: the caller asserts
// WHICH outcome the client saw.
func runCargoErr(t *testing.T, env []string, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("cargo", args...) //nolint:gosec // the real client is the point of the test
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}
