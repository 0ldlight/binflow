package helm

// TestHelmClientEndToEnd drives the REAL helm client against a real
// assembled BinFlow stack — the protocol ticket's hard requirement (no
// HTTP-layer test alone proves the wire). Legs (helm.md section 9's
// command matrix, L-h1~L-h5's local subset; L-h6 HelmOCI and L-h7 virtual
// are their own tickets):
//
//	L-h1 construct and upload: helm create + helm package, curl-style PUT
//	    of the .tgz (and the .prov), then the index.yaml reconciliation —
//	    the digest field equals shasum -a 256 of the tgz.
//	L-h2 consume: helm repo add → repo update (the index fetch) → search
//	    → show → pull (the .tgz download + helm's own digest check) →
//	    template (full local render, no cluster).
//	L-h3 provenance: gpg detach-sign the tgz, PUT the .prov, then
//	    helm pull --verify --keyring against the fetched .prov.
//	L-h4 the reindex management endpoints under a real client-shaped
//	    state (also covered in handler_test; here only the index
//	    reconciliation after the pull).
//
// It is environment-gated (BINFLOW_T309_CLIENT_E2E=1) with helm on PATH
// (helm 3.x per the spec; probed live on helm 4.2.4 — the classic repo
// face is unchanged) and gpg for the --verify leg. The gate skips
// silently so toolchain-less CI stays green. The ticket log records a
// full gated run.

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

func TestHelmClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T309_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T309_CLIENT_E2E=1 (with helm and gpg on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm unavailable on PATH: %v (brew install helm; the ticket runbook)", err)
	}
	haveGPG := true
	if _, err := exec.LookPath("gpg"); err != nil {
		haveGPG = false
	}

	s := newStack(t)
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	base := s.srv.URL + "/binflow/helm-local"

	work := t.TempDir()
	// An ISOLATED helm home: the client otherwise reads/writes the user's
	// own repositories.yaml (repo name collisions across runs and across
	// the operator's machine).
	helmEnv := []string{
		"HELM_CONFIG_HOME=" + filepath.Join(work, "helm"),
		"HELM_CACHE_HOME=" + filepath.Join(work, "helm"),
		"HELM_DATA_HOME=" + filepath.Join(work, "helm"),
	}
	run := func(dir string, env []string, args ...string) string {
		t.Helper()
		cmd := exec.Command("helm", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), helmEnv...)
		if env != nil {
			cmd.Env = append(cmd.Env, env...)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	runGPG := func(dir, gnupgHome string, args ...string) string {
		t.Helper()
		cmd := exec.Command("gpg", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GNUPGHOME="+gnupgHome)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("gpg %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// ---- L-h1: construct and upload ----
	run(work, nil, "create", "mychart")
	run(work, nil, "package", "mychart")
	tgz := filepath.Join(work, "mychart-0.1.0.tgz")
	chartBytes := readFile(t, tgz)

	status, body, _ := s.put("/binflow/helm-local/mychart-0.1.0.tgz", chartBytes, nil)
	if status != http.StatusCreated {
		t.Fatalf("L-h1 chart PUT = (%d, %s), want 201", status, body)
	}
	status, indexBody, _ := s.get("/binflow/helm-local/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("L-h1 index = (%d, %s), want 200", status, indexBody)
	}
	if !strings.Contains(indexBody, "digest: "+sha256Hex(chartBytes)) {
		t.Fatalf("L-h1 digest mismatch: index\n%s\ntgz sha256 %s", indexBody, sha256Hex(chartBytes))
	}
	if !strings.Contains(indexBody, `- mychart-0.1.0.tgz`) {
		t.Fatalf("L-h1 urls not relative:\n%s", indexBody)
	}

	// ---- L-h3 (sign early; the --verify leg rides L-h2's pull) ----
	// The prov format helm's verifier accepts is a CLEARTEXT-signed
	// two-part message block (helm pkg/provenance: clearsign.Decode, then
	// a "\n...\n" split into chart metadata and the files checksum map) —
	// the classic `helm sign` output shape. helm.md L-h3's
	// `gpg --armor --detach-sign` recipe produces a bare signature helm
	// REJECTS with "signature block not found"; the working recipe is
	// registered as a spec erratum in the ticket report.
	var keyring string
	if haveGPG {
		// A SHORT home: the gpg agent's unix socket path must stay under
		// the OS limit, which t.TempDir()'s deep macOS path overflows.
		gnupgHome, err := os.MkdirTemp("/tmp", "t309gpg")
		if err != nil {
			t.Fatalf("mk gnupghome: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(gnupgHome) })
		runGPG(work, gnupgHome, "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
			"--quick-generate-key", "T309 Signer <t309@example.com>", "ed25519", "sign", "0")
		payload := string(readFile(t, filepath.Join(work, "mychart", "Chart.yaml"))) +
			"\n...\nfiles:\n  mychart-0.1.0.tgz: sha256:" + sha256Hex(chartBytes) + "\n"
		payloadFile := filepath.Join(work, "prov-payload.yaml")
		if err := os.WriteFile(payloadFile, []byte(payload), 0o600); err != nil {
			t.Fatalf("write prov payload: %v", err)
		}
		runGPG(work, gnupgHome, "--batch", "--pinentry-mode", "loopback", "--passphrase", "",
			"--clearsign", "--output", "mychart-0.1.0.tgz.prov", payloadFile)
		prov := readFile(t, filepath.Join(work, "mychart-0.1.0.tgz.prov"))
		if !strings.Contains(string(prov), "BEGIN PGP SIGNED MESSAGE") {
			t.Fatalf("clearsign output missing the signed-message envelope:\n%s", prov)
		}
		if status, body, _ := s.put("/binflow/helm-local/mychart-0.1.0.tgz.prov", prov, nil); status != http.StatusCreated {
			t.Fatalf("L-h3 prov PUT = (%d, %s), want 201", status, body)
		}
		keyring = filepath.Join(work, "keyring.gpg")
		runGPG(work, gnupgHome, "--export", "--output", keyring, "t309@example.com")
	}

	// ---- L-h2: consume through the real client ----
	run(work, nil, "repo", "add", "binflow", base)
	out := run(work, nil, "repo", "update")
	t.Logf("L-h2 helm repo update: %s", oneLine(out))
	out = run(work, nil, "search", "repo", "binflow/mychart")
	if !strings.Contains(out, "mychart") || !strings.Contains(out, "0.1.0") {
		t.Fatalf("L-h2 search repo = %q", out)
	}
	out = run(work, nil, "show", "chart", "binflow/mychart")
	if !strings.Contains(out, "description") && !strings.Contains(out, "name: mychart") {
		t.Fatalf("L-h2 show chart = %q", out)
	}

	pullDir := filepath.Join(work, "pull")
	if err := os.MkdirAll(pullDir, 0o750); err != nil {
		t.Fatalf("mk pull dir: %v", err)
	}
	out = run(pullDir, nil, "pull", "binflow/mychart", "--version", "0.1.0")
	t.Logf("L-h2 helm pull: %s", oneLine(out))
	pulled := readFile(t, filepath.Join(pullDir, "mychart-0.1.0.tgz"))
	if sha256Hex(pulled) != sha256Hex(chartBytes) {
		t.Fatalf("L-h2 pulled tgz digest mismatch: %s vs %s", sha256Hex(pulled), sha256Hex(chartBytes))
	}

	// helm template renders the fetched chart end-to-end (no cluster).
	out = run(pullDir, nil, "template", "rel1", "binflow/mychart", "--version", "0.1.0")
	if !strings.Contains(out, "kind:") {
		t.Fatalf("L-h2 helm template = %q", out)
	}

	// ---- L-h3: the provenance leg ----
	if haveGPG {
		verifyDir := filepath.Join(work, "verify")
		if err := os.MkdirAll(verifyDir, 0o750); err != nil {
			t.Fatalf("mk verify dir: %v", err)
		}
		out = run(verifyDir, nil, "pull", "binflow/mychart", "--version", "0.1.0",
			"--verify", "--keyring", keyring)
		t.Logf("L-h3 helm pull --verify: %s", oneLine(out))
	} else {
		t.Log("L-h3 skipped: gpg not on PATH (brew install gnupg)")
	}

	// ---- L-h4: reindex under a client-shaped state ----
	if status, body, _ := s.post("/binflow/api/helm/helm-local/reindex"); status != http.StatusOK {
		t.Fatalf("L-h4 reindex = (%d, %s), want 200", status, body)
	}
	waitFor(t, func() bool {
		_, body, _ := s.get("/binflow/helm-local/index.yaml")
		return strings.Contains(body, "mychart-0.1.0.tgz")
	}, "reindex kept the entry")
	// The client still consumes the post-reindex index.
	out = run(work, nil, "repo", "update")
	t.Logf("L-h4 post-reindex repo update: %s", oneLine(out))

	// The alias face serves the same index to an Artifactory-habituated
	// repo URL (HL-1's compatibility promise).
	run(work, nil, "repo", "add", "binflow-alias", s.srv.URL+"/binflow/api/helm/helm-local")
	out = run(work, nil, "search", "repo", "binflow-alias/mychart")
	if !strings.Contains(out, "mychart") {
		t.Fatalf("alias repo search = %q", out)
	}

	// ---- helm install against a live cluster (its own gate: the leg
	// needs a reachable kube context, which CI does not owe us) ----
	if os.Getenv("BINFLOW_T309_KIND") == "1" {
		release := "t309-rel"
		installArgs := []string{"install", release, "binflow/mychart", "--version", "0.1.0",
			"--namespace", "default", "--wait"}
		if haveGPG {
			// The prov leg rides the install too (pull-under-the-hood +
			// verification before the cluster sees the chart).
			installArgs = append(installArgs, "--verify", "--keyring", keyring)
		}
		out = run(work, nil, installArgs...)
		t.Logf("helm install: %s", oneLine(out))
		out = run(work, nil, "uninstall", release, "--namespace", "default")
		t.Logf("helm uninstall: %s", oneLine(out))
	}
}

// readFile loads a file the test itself produced.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // test-owned path under t.TempDir
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// oneLine flattens command output for the log.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " | "), "\r", ""))
}
