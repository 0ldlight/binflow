package cargo

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The T-318 real-client matrix (cargo, environment-gated): the aggregate
// over a LOCAL member and a REMOTE member — the exact shape the ticket
// pins.
//
//	routed publish     cargo publish THROUGH the virtual (registry = the
//	                   virtual's sparse URL) → the write route lands the
//	                   crate in the deployment member, 200 warnings-only
//	local hit          cargo add/build of the local member's crate through
//	                   the virtual (index merge + first-hit download)
//	remote cached      cargo add/build of a crate only the UPSTREAM holds —
//	                   the merged index row comes from the remote member's
//	                   pull-through, the .crate crosses the engine
//	cross-member merge one crate with 0.1.0 in the local member and 0.2.0
//	                   upstream: the merged index lists BOTH rows, and the
//	                   0.2.0 build resolves through the remote member
//
// It is environment-gated (BINFLOW_T318_CLIENT_E2E=1) with a cargo
// toolchain on PATH; the gate skips silently so toolchain-less CI stays
// green. The ticket log records a full gated run.
func TestCargoClientVirtualChain(t *testing.T) {
	if os.Getenv("BINFLOW_T318_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T318_CLIENT_E2E=1 (with a cargo toolchain on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Fatalf("cargo toolchain unavailable on PATH: %v", err)
	}
	if out, err := exec.Command("cargo", "--version").CombinedOutput(); err == nil { //nolint:gosec // the real client is the point of the test
		t.Logf("V cargo %s", oneLine(string(out)))
	}

	s := newStack(t)
	s.seedRepo(t, "cargo-up", repo.TypeLocal)
	s.seedRepo(t, "cargo-rem", repo.TypeRemote)
	s.seedRemoteConfig(t, "cargo-rem", s.srv.URL+"/binflow/cargo-up")
	s.seedRepo(t, "cargo-dep", repo.TypeLocal)
	s.seedRepo(t, "cargo-v", repo.TypeVirtual)
	s.seedVirtualMembers(t, "cargo-v", "cargo-dep", "cargo-rem")
	s.setVirtualRoute(t, "cargo-v", "cargo-dep")

	issued, err := s.auth.Issue(t.Context(), adminUser, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	virtualEnv := cargoEnv(t, s.srv.URL+"/binflow/cargo-v", issued.AccessToken)
	upEnv := cargoEnv(t, s.srv.URL+"/binflow/cargo-up", issued.AccessToken)
	work := t.TempDir()

	// ---- the remote member's supply: publish into the UPSTREAM ----
	runCargo(t, upEnv, work, "new", "vfar", "--lib", "--vcs", "none")
	writeCrateManifest(t, filepath.Join(work, "vfar"), "vfar", "0.5.0", "the upstream-only crate")
	out := runCargo(t, upEnv, filepath.Join(work, "vfar"), "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("V upstream publish vfar: %s", oneLine(out))
	runCargo(t, upEnv, work, "new", "vdup", "--lib", "--vcs", "none")
	writeCrateManifest(t, filepath.Join(work, "vdup"), "vdup", "0.2.0", "the upstream leg of the merged crate")
	out = runCargo(t, upEnv, filepath.Join(work, "vdup"), "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("V upstream publish vdup 0.2.0: %s", oneLine(out))

	// ---- the routed publish: THROUGH the virtual ----
	runCargo(t, virtualEnv, work, "new", "vloc", "--lib", "--vcs", "none")
	writeCrateManifest(t, filepath.Join(work, "vloc"), "vloc", "0.1.0", "the local-member crate via the route")
	out = runCargo(t, virtualEnv, filepath.Join(work, "vloc"), "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("V routed publish vloc: %s", oneLine(out))
	status, body, _ := s.get(repoPath("cargo-dep") + "/index/vl/oc/vloc")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) {
		t.Fatalf("V routed publish must land in the deployment member (index = %d, %s)", status, body)
	}
	status, _, _ = s.get(repoPath("cargo-rem") + "/index/vl/oc/vloc")
	if status != http.StatusNotFound {
		t.Fatalf("V the remote member must not carry the routed crate (got %d)", status)
	}

	// ---- the local member's other leg of the merged crate ----
	runCargo(t, virtualEnv, work, "new", "vdup2", "--lib", "--vcs", "none")
	writeCrateManifest(t, filepath.Join(work, "vdup2"), "vdup", "0.1.0", "the local leg of the merged crate")
	out = runCargo(t, virtualEnv, filepath.Join(work, "vdup2"), "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("V routed publish vdup 0.1.0: %s", oneLine(out))
	// The merged index: BOTH members' rows in one file, SemVer-ordered.
	status, body, _ = s.get(repoPath("cargo-v") + "/index/vd/up/vdup")
	if status != http.StatusOK || !strings.Contains(body, `"vers":"0.1.0"`) || !strings.Contains(body, `"vers":"0.2.0"`) {
		t.Fatalf("V merged vdup index = (%d, %s), want both members' rows", status, body)
	}
	if i, j := strings.Index(body, `"vers":"0.1.0"`), strings.Index(body, `"vers":"0.2.0"`); i < 0 || j < 0 || i > j {
		t.Fatalf("V merged vdup rows must be SemVer-ordered:\n%s", body)
	}

	// ---- the consumer: resolve and build ALL THREE through the virtual ----
	consumer := filepath.Join(work, "consumer")
	runCargo(t, virtualEnv, work, "new", "consumer", "--vcs", "none")
	out = runCargo(t, virtualEnv, consumer, "add", "vloc", "--registry", "binflow")
	t.Logf("V cargo add vloc (local hit): %s", oneLine(out))
	out = runCargo(t, virtualEnv, consumer, "add", "vfar", "--registry", "binflow")
	t.Logf("V cargo add vfar (remote member): %s", oneLine(out))
	out = runCargo(t, virtualEnv, consumer, "add", "vdup@0.2.0", "--registry", "binflow")
	t.Logf("V cargo add vdup@0.2.0 (the remote member's row): %s", oneLine(out))
	out = runCargo(t, virtualEnv, consumer, "build")
	t.Logf("V cargo build: %s", oneLine(out))
	if !strings.Contains(out, "Finished") {
		t.Fatalf("V cargo build must finish, got:\n%s", out)
	}

	// The remote member's copy is now cached: a second download is a HIT.
	status, _, hdr := s.get(repoPath("cargo-v") + "/v1/crates/vfar/0.5.0/download")
	if status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("V post-build vfar download = (%d, cache %q), want the engine HIT", status, hdr.Get("X-BinFlow-Cache"))
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-rem" {
		t.Errorf("V vfar download Resolved-From = %q, want cargo-rem", got)
	}
	status, _, hdr = s.get(repoPath("cargo-v") + "/v1/crates/vloc/0.1.0/download")
	if status != http.StatusOK {
		t.Fatalf("V vloc download = %d, want 200", status)
	}
	if got := hdr.Get(repo.HdrResolvedFrom); got != "cargo-dep" {
		t.Fatalf("V vloc download Resolved-From = %q, want cargo-dep (the local hit)", got)
	}

	// ---- search through the aggregate: both members' rows ----
	status, body, _ = s.get(repoPath("cargo-v") + "/api/v1/crates?q=vdup&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"vdup"`) {
		t.Fatalf("V virtual search = (%d, %s), want vdup", status, body)
	}
}
