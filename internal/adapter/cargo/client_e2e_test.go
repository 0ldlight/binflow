package cargo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestCargoClientEndToEnd drives the REAL cargo client against a real
// assembled BinFlow stack — the protocol ticket's hard requirement (no
// HTTP-layer test alone proves the wire). Legs (cargo.md section 9's
// command matrix, L-r1~L-r6; L-r7's remote pull-through is the M11
// remote ticket's own):
//
//	L-r1 bare probes: config.json, an empty index path, a missing
//	    download;
//	L-r3 publish: cargo new/package/publish --registry binflow, then the
//	    index row appears and its cksum reconciles with BOTH the stored
//	    node and the downloaded .crate bytes (shasum -a 256 semantics);
//	L-r4 consumer: cargo add --registry binflow + cargo build through
//	    the sparse index and the download plane;
//	L-r5 yank/unyank: the real client flips the row, downloads keep
//	    serving;
//	L-r6 search: the official contract over the real data.
//
// It is environment-gated (BINFLOW_T294_CLIENT_E2E=1) with a cargo
// toolchain on PATH; the gate skips silently so toolchain-less CI stays
// green. The ticket log records a full gated run.
func TestCargoClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T294_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T294_CLIENT_E2E=1 (with a cargo toolchain on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Fatalf("cargo toolchain unavailable on PATH: %v (brew install rust / rustup; the ticket runbook)", err)
	}

	s := newStack(t)
	s.seedRepo(t, "cargo-local", repo.TypeLocal)
	reg := s.srv.URL + "/binflow/cargo-local"

	// A real API token: cargo's sparse HTTP registries authenticate with
	// a BARE `Authorization: <token>` value — no scheme (the crates.io
	// compatibility form; probed live, cargo 1.98 — see the ticket log
	// section 5 for the probe transcript that motivated internal/auth's
	// bare-token arm).
	issued, err := s.auth.Issue(t.Context(), adminUser, 0)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	env := cargoEnv(t, reg, issued.AccessToken)

	// ---- L-r1: the bare probes ----
	status, body, _ := s.get("/binflow/cargo-local/index/config.json")
	if status != http.StatusOK || !strings.Contains(body, `"dl":"`+reg+`/v1/crates"`) {
		t.Fatalf("L-r1 config.json = (%d, %s)", status, body)
	}
	status, body, _ = s.get("/binflow/cargo-local/index/my/cr/mycrate")
	if status != http.StatusNotFound {
		t.Fatalf("L-r1 empty index = (%d, %s), want 404", status, body)
	}
	status, body, _ = s.get("/binflow/cargo-local/v1/crates/mycrate/0.1.0/download")
	if status != http.StatusNotFound || body != `{"errors":[{"detail":"unable to download crate"}]}` {
		t.Fatalf("L-r1 missing download = (%d, %s), want the pinned 404 body", status, body)
	}

	// ---- L-r3: publish through the real client ----
	work := t.TempDir()
	crateDir := filepath.Join(work, "mycrate")
	runCargo(t, env, work, "new", "mycrate", "--lib", "--vcs", "none")
	writeCrateManifest(t, crateDir, "mycrate", "0.1.0", "binflow e2e crate")
	out := runCargo(t, env, crateDir, "publish", "--registry", "binflow", "--allow-dirty", "--no-verify")
	t.Logf("L-r3 cargo publish: %s", oneLine(out))

	// The index row appears; the cksum reconciliation (the AC's anchor):
	// row cksum == downloaded bytes' sha256.
	status, body, _ = s.get("/binflow/cargo-local/index/my/cr/mycrate")
	if status != http.StatusOK {
		t.Fatalf("L-r3 index after publish = (%d, %s)", status, body)
	}
	rows := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(rows) != 1 {
		t.Fatalf("L-r3 index rows = %d (body %q)", len(rows), body)
	}
	var line struct {
		Name   string `json:"name"`
		Vers   string `json:"vers"`
		Cksum  string `json:"cksum"`
		Yanked bool   `json:"yanked"`
	}
	if err := json.Unmarshal([]byte(rows[0]), &line); err != nil {
		t.Fatalf("L-r3 row %q: %v", rows[0], err)
	}
	if line.Name != "mycrate" || line.Vers != "0.1.0" {
		t.Fatalf("L-r3 row identity = %+v", line)
	}
	status, body, _ = s.get("/binflow/cargo-local/v1/crates/mycrate/0.1.0/download")
	if status != http.StatusOK {
		t.Fatalf("L-r3 download = (%d, %s)", status, body)
	}
	if sum := sha256hex([]byte(body)); sum != line.Cksum {
		t.Fatalf("L-r3 cksum reconciliation: row %s != shasum -a 256 of the download %s", line.Cksum, sum)
	}
	t.Logf("L-r3 cksum reconciliation: %s == %s", line.Cksum, line.Cksum)

	// ---- L-r4: a consumer resolves, downloads and builds ----
	consumer := filepath.Join(work, "consumer")
	runCargo(t, env, work, "new", "consumer", "--vcs", "none")
	out = runCargo(t, env, consumer, "add", "mycrate", "--registry", "binflow")
	t.Logf("L-r4 cargo add: %s", oneLine(out))
	out = runCargo(t, env, consumer, "build")
	t.Logf("L-r4 cargo build: %s", oneLine(out))

	// ---- L-r5: yank flips the row (downloads keep serving), unyank back ----
	out = runCargo(t, env, consumer, "yank", "--registry", "binflow", "mycrate@0.1.0")
	t.Logf("L-r5 cargo yank: %s", oneLine(out))
	status, body, _ = s.get("/binflow/cargo-local/index/my/cr/mycrate")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":true`) {
		t.Fatalf("L-r5 post-yank row = (%d, %s)", status, body)
	}
	status, _, _ = s.get("/binflow/cargo-local/v1/crates/mycrate/0.1.0/download")
	if status != http.StatusOK {
		t.Fatalf("L-r5 post-yank download = %d, want the keep-serving semantic", status)
	}
	out = runCargo(t, env, consumer, "yank", "--undo", "--registry", "binflow", "mycrate@0.1.0")
	t.Logf("L-r5 cargo unyank (--undo): %s", oneLine(out))
	status, body, _ = s.get("/binflow/cargo-local/index/my/cr/mycrate")
	if status != http.StatusOK || !strings.Contains(body, `"yanked":false`) {
		t.Fatalf("L-r5 post-unyank row = (%d, %s)", status, body)
	}

	// ---- L-r6: search over the real data ----
	status, body, _ = s.get("/binflow/cargo-local/api/v1/crates?q=mycrate&per_page=10")
	if status != http.StatusOK || !strings.Contains(body, `"name":"mycrate"`) ||
		!strings.Contains(body, `"max_version":"0.1.0"`) || !strings.Contains(body, `"meta":{"total":1}`) {
		t.Fatalf("L-r6 search = (%d, %s)", status, body)
	}
}

// cargoEnv builds the isolated client environment: a fresh CARGO_HOME
// (no inherited registry config), the sparse index URL and the bearer
// token for the binflow registry.
func cargoEnv(t *testing.T, reg, token string) []string {
	t.Helper()
	home, err := os.MkdirTemp("", "t294-cargo")
	if err != nil {
		t.Fatalf("temp CARGO_HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = filepath.Walk(home, func(p string, _ os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chmod(p, 0o700) //nolint:gosec // test scratch dir
			}
			return nil
		})
		_ = os.RemoveAll(home)
	})
	return append(os.Environ(),
		"CARGO_HOME="+home,
		"CARGO_REGISTRIES_BINFLOW_INDEX=sparse+"+reg+"/index/",
		"CARGO_REGISTRIES_BINFLOW_TOKEN="+token,
		"CARGO_NET_RETRY=2",
	)
}

// runCargo runs one cargo command under env in dir.
func runCargo(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("cargo", args...) //nolint:gosec // the real client is the point of the test
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("cargo %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// writeCrateManifest replaces the scaffold manifest with a publishable
// one (description and license are what cargo's publish validation
// demands; cargo new's scaffold leaves both out and its [dependencies]
// section would swallow appended keys).
func writeCrateManifest(t *testing.T, dir, name, vers, description string) {
	t.Helper()
	manifest := fmt.Sprintf(`[package]
name = %q
version = %q
edition = "2021"
description = %q
license = "MIT OR Apache-2.0"
publish = ["binflow"]

[dependencies]
`, name, vers, description)
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
}

// oneLine squeezes a client transcript to its last meaningful line.
func oneLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
