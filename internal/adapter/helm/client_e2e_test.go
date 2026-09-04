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
	"bytes"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
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

// TestHelmClientUploadUnderReadOnlyServerTemp is T-474's client-facing
// pin: the incident's exact topology — the SERVER process facing a
// read-only OS temp dir (UAT's read-only-rootfs container; the pre-fix
// classic PUT answered 500 there) while the CLIENT runs on a normal host.
// The chart PUT (the curl-deployment shape) and the consume chain the CI
// leg_helm matrix drives — repo add → update → pull, bytes identical —
// must both sail: the spool stages on the storage volume's staging dir,
// never the OS temp dir. Gated like the T-309 matrix.
func TestHelmClientUploadUnderReadOnlyServerTemp(t *testing.T) {
	if os.Getenv("BINFLOW_T309_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T309_CLIENT_E2E=1 (with helm on PATH) to run the real-client matrix")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm unavailable on PATH: %v", err)
	}

	// Stack and client scratch FIRST: their t.TempDir roots must resolve
	// against the REAL OS temp; only then is the server's view poisoned.
	s := newStack(t) // SpoolDir = <dataDir>/staging, the cmd posture
	s.seedRepo(t, "helm-local", repo.TypeLocal, "{}")
	work := t.TempDir()
	t.Setenv("TMPDIR", mustReadOnlyDir(t))

	chart := fixtureChart(t, "mychart", defaultChartYAML("mychart", "1.0.99"), nil)
	status, body, _ := s.put("/binflow/helm-local/mychart-1.0.99.tgz", chart, nil)
	if status != http.StatusCreated {
		t.Fatalf("T-474 chart PUT under a read-only server OS temp = (%d, %s), want 201 (the incident answered 500)", status, body)
	}

	// The client rides a CLEAN temp dir (the CI runner's own posture —
	// the incident was server-side): later entries win in exec env, so
	// TMPDIR is restored for the helm subprocess.
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("helm", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"HELM_CONFIG_HOME="+filepath.Join(work, "helm"),
			"HELM_CACHE_HOME="+filepath.Join(work, "helm"),
			"HELM_DATA_HOME="+filepath.Join(work, "helm"),
			"TMPDIR="+work)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run(work, "repo", "add", "bf-t474", s.srv.URL+"/binflow/helm-local")
	out := run(work, "repo", "update", "bf-t474")
	t.Logf("T-474 helm repo update: %s", oneLine(out))
	pullDir := filepath.Join(work, "pull")
	if err := os.MkdirAll(pullDir, 0o750); err != nil {
		t.Fatalf("mk pull dir: %v", err)
	}
	run(pullDir, "pull", "bf-t474/mychart", "--version", "1.0.99")
	pulled := readFile(t, filepath.Join(pullDir, "mychart-1.0.99.tgz"))
	if !bytes.Equal(pulled, chart) {
		t.Fatalf("T-474 pulled bytes differ: sha256 %s vs %s", sha256Hex(pulled), sha256Hex(chart))
	}
}

// oneLine flattens command output for the log.
func oneLine(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " | "), "\r", ""))
}

// TestHelmRemoteVirtualClientEndToEnd drives the REAL helm client through
// the T-313 faces (helm.md section 9's L-h7 matrix): a REMOTE repository
// proxying an upstream chart repository (repo add/update/search/pull, the
// second-hit cache) and a VIRTUAL repository aggregating the local and
// remote members (the rewritten download urls, the _external dependency
// leg, first-wins). Environment-gated like the T-309 matrix
// (BINFLOW_T313_CLIENT_E2E=1); the gate skips silently so
// toolchain-less CI stays green.
func TestHelmRemoteVirtualClientEndToEnd(t *testing.T) {
	if os.Getenv("BINFLOW_T313_CLIENT_E2E") != "1" {
		t.Skip("set BINFLOW_T313_CLIENT_E2E=1 (with helm on PATH) to run the remote+virtual client matrix")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("helm unavailable on PATH: %v", err)
	}

	s := newStack(t)
	// The upstream chart repository: a BinFlow LOCAL helm repo (a real
	// chart repository by construction). upchart lands through the normal
	// PUT chain; the ext host is the dependency carrier OUTSIDE the
	// upstream's urls.
	s.seedRepo(t, "helm-upstream", repo.TypeLocal, "{}")
	upchart := fixtureChart(t, "upchart", defaultChartYAML("upchart", "0.1.0"), nil)
	if status, body, _ := s.put("/binflow/helm-upstream/upchart-0.1.0.tgz", upchart, nil); status != http.StatusCreated {
		t.Fatalf("upstream chart PUT = (%d, %s)", status, body)
	}
	extchart := fixtureChart(t, "extchart", defaultChartYAML("extchart", "1.0.0"), nil)
	ext := newChartUpstream(t, map[string]string{"/extchart-1.0.0.tgz": string(extchart)})

	// The upstream index carries one RELATIVE-urls entry (the mirror shape
	// whose downloads flow through the proxy) plus one entry pointing at
	// the external host (the dependency-rewrite input) plus a shared
	// name+version that the virtual's LOCAL member shadows (first-wins).
	// The service-level write the client plane refuses does the seeding
	// (the TestReindexEndpoints posture).
	sharedLocal := fixtureChart(t, "shared", defaultChartYAML("shared", "1.0.0"), nil)
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	if status, body, _ := s.put("/binflow/helm-l/shared-1.0.0.tgz", sharedLocal, nil); status != http.StatusCreated {
		t.Fatalf("local member chart PUT = (%d, %s)", status, body)
	}
	upstreamIndex := "apiVersion: v1\nentries:\n" +
		"  upchart:\n  - name: upchart\n    version: \"0.1.0\"\n    digest: " + sha256Hex(upchart) + "\n    created: \"2026-08-27T00:00:00Z\"\n    urls:\n    - upchart-0.1.0.tgz\n" +
		"  extchart:\n  - name: extchart\n    version: \"1.0.0\"\n    digest: " + sha256Hex(extchart) + "\n    created: \"2026-08-27T00:00:00Z\"\n    urls:\n    - " + ext.srv.URL + "/extchart-1.0.0.tgz\n" +
		"  shared:\n  - name: shared\n    version: \"1.0.0\"\n    digest: " + strings.Repeat("ab", 32) + "\n    created: \"2026-08-27T00:00:00Z\"\n    urls:\n    - " + s.srv.URL + "/binflow/helm-upstream/shared-1.0.0.tgz\n"
	if _, err := s.svc.Put(context.Background(), &auth.Principal{Name: adminUser, Admin: true},
		"helm-upstream", "index.yaml", strings.NewReader(upstreamIndex),
		storage.BlobRef{Sha256: sha256Hex([]byte(upstreamIndex))}, "text/yaml"); err != nil {
		t.Fatalf("seed the upstream index: %v", err)
	}

	s.seedRemoteRepo(t, "helm-remote", s.srv.URL+"/binflow/helm-upstream")
	s.seedVirtualRepo(t, "helm-virt", "helm-l", "helm-l", "helm-remote")

	work := t.TempDir()
	helmEnv := []string{
		"HELM_CONFIG_HOME=" + filepath.Join(work, "helm"),
		"HELM_CACHE_HOME=" + filepath.Join(work, "helm"),
		"HELM_DATA_HOME=" + filepath.Join(work, "helm"),
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("helm", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), helmEnv...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("helm %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}

	// ---- R1: the remote repository (repo add → update → search → pull,
	// the second-hit cache) ----
	run(work, "repo", "add", "bf-remote", s.srv.URL+"/binflow/helm-remote")
	out := run(work, "repo", "update")
	t.Logf("R1 helm repo update (remote): %s", oneLine(out))
	out = run(work, "search", "repo", "bf-remote/upchart")
	if !strings.Contains(out, "upchart") {
		t.Fatalf("R1 search = %q", out)
	}
	// The cache assertions at the exact path the client addresses.
	status, _, hdr := s.get("/binflow/helm-remote/upchart-0.1.0.tgz")
	if status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "MISS" {
		t.Fatalf("R1 first chart fetch = (%d, cache %q), want 200/MISS", status, hdr.Get("X-BinFlow-Cache"))
	}
	pullDir := filepath.Join(work, "r1")
	if err := os.MkdirAll(pullDir, 0o750); err != nil {
		t.Fatalf("mk pull dir: %v", err)
	}
	run(pullDir, "pull", "bf-remote/upchart", "--version", "0.1.0")
	if got := readFile(t, filepath.Join(pullDir, "upchart-0.1.0.tgz")); sha256Hex(got) != sha256Hex(upchart) {
		t.Fatalf("R1 pulled digest mismatch: %s vs %s", sha256Hex(got), sha256Hex(upchart))
	}
	if status, _, hdr = s.get("/binflow/helm-remote/upchart-0.1.0.tgz"); status != http.StatusOK || hdr.Get("X-BinFlow-Cache") != "HIT" {
		t.Fatalf("R1 second chart fetch = (%d, cache %q), want 200/HIT (AC4)", status, hdr.Get("X-BinFlow-Cache"))
	}

	// ---- V1: the virtual repository (aggregate + the S8 rewrites) ----
	run(work, "repo", "add", "bf-virt", s.srv.URL+"/binflow/helm-virt")
	out = run(work, "repo", "update")
	t.Logf("V1 helm repo update (virtual): %s", oneLine(out))
	for _, chart := range []string{"upchart", "extchart", "shared"} {
		if out = run(work, "search", "repo", "bf-virt/"+chart); !strings.Contains(out, chart) {
			t.Fatalf("V1 search %s = %q", chart, out)
		}
	}
	// The rewritten urls land in the index the client consumed: the
	// relative mirror path, the folded _external form, the first-wins
	// local digest.
	status, index, _ := s.get("/binflow/helm-virt/index.yaml")
	if status != http.StatusOK {
		t.Fatalf("V1 index = %d", status)
	}
	for _, want := range []string{
		"- upchart-0.1.0.tgz",
		"- _external/http/" + hostOf(ext.srv.URL) + "/extchart-1.0.0.tgz",
		"- shared-1.0.0.tgz",
		"digest: " + sha256Hex(sharedLocal),
	} {
		if !strings.Contains(index, want+"\n") {
			t.Errorf("V1 index missing %q:\n%s", want, index)
		}
	}
	if strings.Contains(index, strings.Repeat("ab", 32)) {
		t.Errorf("V1 first-wins lost: the shadowed member's digest survived:\n%s", index)
	}
	// Pulls through the virtual: the mirror chart (member pull-through)
	// and the external dependency (the _external egress).
	vDir := filepath.Join(work, "v1")
	if err := os.MkdirAll(vDir, 0o750); err != nil {
		t.Fatalf("mk pull dir: %v", err)
	}
	run(vDir, "pull", "bf-virt/upchart", "--version", "0.1.0")
	if got := readFile(t, filepath.Join(vDir, "upchart-0.1.0.tgz")); sha256Hex(got) != sha256Hex(upchart) {
		t.Fatalf("V1 pulled upchart digest mismatch")
	}
	run(vDir, "pull", "bf-virt/extchart", "--version", "1.0.0")
	if got := readFile(t, filepath.Join(vDir, "extchart-1.0.0.tgz")); sha256Hex(got) != sha256Hex(extchart) {
		t.Fatalf("V1 pulled extchart digest mismatch (the _external leg)")
	}
	// First-wins on the wire: the LOCAL member's bytes serve.
	run(vDir, "pull", "bf-virt/shared", "--version", "1.0.0")
	if got := readFile(t, filepath.Join(vDir, "shared-1.0.0.tgz")); sha256Hex(got) != sha256Hex(sharedLocal) {
		t.Fatalf("V1 first-wins lost: the served bytes are not the local member's")
	}
	if n := ext.hitCount("/extchart-1.0.0.tgz"); n < 1 {
		t.Errorf("V1 _external leg never reached the external host (hits %d)", n)
	}

	// ---- V2: the Artifactory-habituated alias on the virtual face ----
	run(work, "repo", "add", "bf-virt-alias", s.srv.URL+"/binflow/api/helm/helm-virt")
	if out = run(work, "search", "repo", "bf-virt-alias/upchart"); !strings.Contains(out, "upchart") {
		t.Fatalf("V2 alias search = %q", out)
	}

	// ---- R2/V3: helm install against a live cluster (its own gate: the
	// legs need a reachable kube context) — the remote repository's chart
	// (the pull-through download path) and the virtual repository's
	// external dependency (the _external egress path under install). ----
	if os.Getenv("BINFLOW_T313_KIND") == "1" {
		for _, spec := range []struct{ release, repoRef, chart, version string }{
			{"t313-remote", "bf-remote/upchart", "upchart", "0.1.0"},
			{"t313-virt", "bf-virt/extchart", "extchart", "1.0.0"},
		} {
			out = run(work, "install", spec.release, spec.repoRef, "--version", spec.version,
				"--namespace", "default", "--wait")
			t.Logf("helm install %s: %s", spec.release, oneLine(out))
			out = run(work, "uninstall", spec.release, "--namespace", "default")
			t.Logf("helm uninstall %s: %s", spec.release, oneLine(out))
		}
	}
}
