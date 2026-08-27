package deb

// The client-facing end-to-end legs:
//
//   - TestClientE2EReleaseChain (always on): the Release→Packages→.deb
//     sweep apt walks, with every checksum field reconciled against the
//     bytes actually served — the section 7 "server obligation"
//     contract (Release's SHA256 entry = the served Packages.gz bytes;
//     the stanza's SHA256 = the served .deb bytes), exercised through
//     the real router with a real HTTP client.
//
//   - TestClientE2EAptContainer (env-gated, BINFLOW_DEB_E2E_APT=1): the
//     REAL client chain — a genuine dpkg-deb-built .deb uploaded with
//     curl through the debPUT face, then sources.list + apt-get update +
//     apt-get install inside a Debian container against this test's own
//     live server ([trusted=yes] unsigned mode). Gated so ordinary
//     `go test` runs stay hermetic; the ticket's verification run
//     enables it and archives the output.

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// releaseChecksumOf parses one Release checksum section entry for a path
// (the test-side apt arithmetic).
func releaseChecksumOf(t *testing.T, release, section, path string) string {
	t.Helper()
	lines := strings.Split(release, "\n")
	in := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, section+":") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if !strings.HasPrefix(ln, " ") {
			break // the next section header
		}
		fields := strings.Fields(ln)
		if len(fields) == 3 && fields[2] == path {
			return fields[0]
		}
	}
	t.Fatalf("Release %s section has no entry for %s:\n%s", section, path, release)
	return ""
}

// gunzipE2E decompresses one body (test-side helper).
func gunzipE2E(t *testing.T, body []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip open: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return out
}

// TestClientE2EReleaseChain: the whole update sweep with digest
// reconciliation at every hop (byHash=ALL so the by-hash family joins
// the reconciliation).
func TestClientE2EReleaseChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-e2e", repo.TypeLocal, `{"byHash":"ALL"}`)
	pkg := helloDeb("e2epkg", "1.0-1", "amd64")
	if status, b, _ := s.debPut(t, "/binflow/deb-e2e/pool/main/e/e2epkg/e2epkg_1.0-1_amd64.deb",
		pkg, "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("debPUT = (%d, %s)", status, b)
	}
	s.waitIndex(t, "/binflow/deb-e2e/dists/stable/Release")

	// Release → Packages.gz: the SHA256 entry must equal the served bytes.
	status, release, _ := s.get("/binflow/deb-e2e/dists/stable/Release")
	if status != http.StatusOK {
		t.Fatalf("Release = %d", status)
	}
	gzPath := "main/binary-amd64/Packages.gz"
	want := releaseChecksumOf(t, release, "SHA256", gzPath)
	status, gz, _ := s.get("/binflow/deb-e2e/dists/stable/" + gzPath)
	if status != http.StatusOK {
		t.Fatalf("Packages.gz = %d", status)
	}
	if got := sha256Hex([]byte(gz)); got != want {
		t.Errorf("Packages.gz sha256: Release %s ≠ wire %s", want, got)
	}
	if md5want := releaseChecksumOf(t, release, "MD5Sum", gzPath); md5want != md5Of([]byte(gz)) {
		t.Errorf("Packages.gz md5: Release %s ≠ wire %s", md5want, md5Of([]byte(gz)))
	}

	// Packages.gz → stanza → .deb: Filename + SHA256 + Size against the
	// served artifact (the apt install arithmetic).
	packages := string(gunzipE2E(t, []byte(gz)))
	if !strings.Contains(packages, "Filename: pool/main/e/e2epkg/e2epkg_1.0-1_amd64.deb\n") {
		t.Fatalf("stanza Filename wrong:\n%s", packages)
	}
	stanzaSha := stanzaField(packages, "SHA256")
	stanzaSize := stanzaField(packages, "Size")
	if stanzaSha != sha256Hex(pkg) {
		t.Errorf("stanza SHA256 %s ≠ .deb digest %s", stanzaSha, sha256Hex(pkg))
	}
	if stanzaSize != itoa(len(pkg)) {
		t.Errorf("stanza Size %s ≠ %d", stanzaSize, len(pkg))
	}
	status, body, hdr := s.get("/binflow/deb-e2e/pool/main/e/e2epkg/e2epkg_1.0-1_amd64.deb")
	if status != http.StatusOK || sha256Hex([]byte(body)) != stanzaSha {
		t.Fatalf("the .deb the stanza points at does not serve its digest (status %d)", status)
	}
	if hdr.Get("X-Checksum-Sha256") != stanzaSha {
		t.Errorf("download X-Checksum-Sha256 = %q", hdr.Get("X-Checksum-Sha256"))
	}

	// The by-hash mirrors of the uncompressed and compressed forms.
	plainPath := "main/binary-amd64/Packages"
	status, plain, _ := s.get("/binflow/deb-e2e/dists/stable/" + plainPath)
	if status != http.StatusOK {
		t.Fatalf("Packages = %d", status)
	}
	for algo, digest := range map[string]string{
		"SHA256": releaseChecksumOf(t, release, "SHA256", plainPath),
		"MD5Sum": releaseChecksumOf(t, release, "MD5Sum", plainPath),
	} {
		status, body, _ := s.get("/binflow/deb-e2e/dists/stable/main/binary-amd64/by-hash/" + algo + "/" + digest)
		if status != http.StatusOK || string(body) != plain {
			t.Errorf("by-hash/%s of Packages = %d (served %d bytes, want the canonical %d)", algo, status, len(body), len(plain))
		}
	}
}

// stanzaField extracts one field's value from the first stanza.
func stanzaField(stanza, field string) string {
	for _, ln := range strings.Split(stanza, "\n") {
		if strings.HasPrefix(ln, field+": ") {
			return strings.TrimPrefix(ln, field+": ")
		}
	}
	return ""
}

// itoa renders the decimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// TestClientE2EAptContainer is the REAL-client leg (see the file comment
// for the gate). It needs docker and host.docker.internal reachability.
func TestClientE2EAptContainer(t *testing.T) {
	if os.Getenv("BINFLOW_DEB_E2E_APT") != "1" {
		t.Skip("set BINFLOW_DEB_E2E_APT=1 to run the container apt leg (needs docker)")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	s := newStack(t)
	s.seedRepo(t, "deb-apt", repo.TypeLocal, `{"byHash":"ALL"}`)
	host := "http://host.docker.internal:" + portOf(s.srv.URL)

	share := t.TempDir()

	// Step 1: build a genuine .deb inside the container (dpkg-deb's own
	// toolchain — control.tar.xz, the modern default compression).
	build := []string{
		"set -e",
		"mkdir -p /pkg/DEBIAN /pkg/usr/bin",
		"printf 'Package: binflow-e2e\\nVersion: 1.0-1\\nArchitecture: amd64\\nMaintainer: BinFlow <t@binflow.dev>\\nDescription: BinFlow T-310 client verification package\\n' > /pkg/DEBIAN/control",
		"printf '#!/bin/sh\\necho binflow t310\\n' > /pkg/usr/bin/binflow-e2e",
		"chmod 0755 /pkg/usr/bin/binflow-e2e",
		"dpkg-deb --build /pkg /e2e/binflow-e2e_1.0-1_amd64.deb >/dev/null",
		"dpkg-deb --info /e2e/binflow-e2e_1.0-1_amd64.deb | head -8",
	}
	out := runDockerDeb(t, share, "debian:bookworm", strings.Join(build, "\n"))
	t.Logf("container build:\n%s", out)

	// Step 2: the debPUT face from inside the container (curl).
	up := []string{
		"set -e",
		"command -v curl >/dev/null || apt-get update >/dev/null 2>&1 && apt-get install -y curl >/dev/null 2>&1 || true",
		`curl -sf -u admin:password -T /e2e/binflow-e2e_1.0-1_amd64.deb \
		 -o /dev/null -w 'PUT %{http_code}\n' \
		 '` + host + `/binflow/deb-apt/pool/main/b/binflow-e2e/binflow-e2e_1.0-1_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64'`,
	}
	out = runDockerDeb(t, share, "debian:bookworm", strings.Join(up, "\n"))
	if !strings.Contains(out, "PUT 201") {
		t.Fatalf("container debPUT failed:\n%s", out)
	}
	t.Logf("container debPUT:\n%s", out)

	// Step 3: wait for the index (the async recompute), then the apt
	// consumer chain.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if st, _, _ := s.get("/binflow/deb-apt/dists/stable/Release"); st == http.StatusOK {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if st, _, _ := s.get("/binflow/deb-apt/dists/stable/Release"); st != http.StatusOK {
		t.Fatal("Release never materialized for the container leg")
	}

	apt := []string{
		"set -e",
		"printf 'deb [trusted=yes] " + host + "/binflow/deb-apt stable main\\n' > /etc/apt/sources.list.d/binflow.list",
		"apt-get update 2>&1 | tail -3",
		"apt-get install -y --no-install-recommends binflow-e2e 2>&1 | tail -3",
		"dpkg -s binflow-e2e | grep -E '^(Status|Version)'",
		"binflow-e2e",
		"apt-get update 2>&1 | grep -c 'binflow'" + "",
	}
	out = runDockerDeb(t, share, "debian:bookworm", strings.Join(apt, "\n"))
	t.Logf("container apt chain:\n%s", out)
	for _, want := range []string{
		"Status: install ok installed",
		"Version: 1.0-1",
		"binflow t310",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("apt chain output missing %q:\n%s", want, out)
		}
	}
}

// TestClientE2ERemoteVirtualAptContainer is the REAL-client leg of the
// T-314 faces (same gate as the local leg): the same dpkg-deb-built
// package serves an apt update+install THROUGH a remote repository
// (the pull-through mirror of the local origin) and, with a second
// package in a second member, THROUGH a virtual repository (the merged
// Release/Packages plus first-found downloads across a remote AND a
// local member).
func TestClientE2ERemoteVirtualAptContainer(t *testing.T) {
	if os.Getenv("BINFLOW_DEB_E2E_APT") != "1" {
		t.Skip("set BINFLOW_DEB_E2E_APT=1 to run the container apt leg (needs docker)")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	s := newStack(t)
	s.seedRepo(t, "deb-apt", repo.TypeLocal, `{"byHash":"ALL"}`)
	s.seedRepo(t, "deb-apt2", repo.TypeLocal, `{}`)
	s.seedRepo(t, "deb-mirror", repo.TypeRemote, `{}`)
	s.seedRemoteConfig(t, "deb-mirror", s.srv.URL+"/binflow/deb-apt")
	s.seedVirtualRepo(t, "deb-virt", []string{"deb-mirror", "deb-apt2"}, "")
	host := "http://host.docker.internal:" + portOf(s.srv.URL)

	share := t.TempDir()

	// Two genuine packages, one per member, built with dpkg-deb's own
	// toolchain.
	build := []string{
		"set -e",
		"mkdir -p /p1/DEBIAN /p1/usr/bin /p2/DEBIAN /p2/usr/bin /e2e",
		"printf 'Package: binflow-e2e\\nVersion: 1.0-1\\nArchitecture: amd64\\nMaintainer: BinFlow <t@binflow.dev>\\nDescription: BinFlow T-314 remote leg package\\n' > /p1/DEBIAN/control",
		"printf '#!/bin/sh\\necho binflow t314 remote\\n' > /p1/usr/bin/binflow-e2e && chmod 0755 /p1/usr/bin/binflow-e2e",
		"dpkg-deb --build /p1 /e2e/binflow-e2e_1.0-1_amd64.deb >/dev/null",
		"printf 'Package: binflow-virt\\nVersion: 2.0-1\\nArchitecture: amd64\\nMaintainer: BinFlow <t@binflow.dev>\\nDescription: BinFlow T-314 virtual member package\\n' > /p2/DEBIAN/control",
		"printf '#!/bin/sh\\necho binflow t314 virtual\\n' > /p2/usr/bin/binflow-virt && chmod 0755 /p2/usr/bin/binflow-virt",
		"dpkg-deb --build /p2 /e2e/binflow-virt_2.0-1_amd64.deb >/dev/null",
	}
	out := runDockerDeb(t, share, "debian:bookworm", strings.Join(build, "\n"))
	t.Logf("container build:\n%s", out)

	// Seed the members: the origin local (the remote's upstream) gets
	// binflow-e2e; the second local member gets binflow-virt.
	up := []string{
		"set -e",
		"command -v curl >/dev/null || apt-get update >/dev/null 2>&1 && apt-get install -y curl >/dev/null 2>&1 || true",
		"curl -sf -u admin:password -T /e2e/binflow-e2e_1.0-1_amd64.deb -o /dev/null -w 'PUT1 %{http_code}\\n' '" + host + "/binflow/deb-apt/pool/main/b/binflow-e2e/binflow-e2e_1.0-1_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64'",
		"curl -sf -u admin:password -T /e2e/binflow-virt_2.0-1_amd64.deb -o /dev/null -w 'PUT2 %{http_code}\\n' '" + host + "/binflow/deb-apt2/pool/main/v/binflow-virt/binflow-virt_2.0-1_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64'",
	}
	out = runDockerDeb(t, share, "debian:bookworm", strings.Join(up, "\n"))
	for _, want := range []string{"PUT1 201", "PUT2 201"} {
		if !strings.Contains(out, want) {
			t.Fatalf("container seeding failed (%s missing):\n%s", want, out)
		}
	}
	s.waitIndex(t, "/binflow/deb-apt/dists/stable/Release")
	s.waitIndex(t, "/binflow/deb-apt2/dists/stable/Release")

	// ---- the remote leg: update + install through the mirror ----
	remoteApt := []string{
		"set -e",
		"printf 'deb [trusted=yes] " + host + "/binflow/deb-mirror stable main\\n' > /etc/apt/sources.list.d/binflow.list",
		"apt-get update 2>&1 | tail -2",
		"apt-get install -y --no-install-recommends binflow-e2e 2>&1 | tail -2",
		"dpkg -s binflow-e2e | grep -E '^(Status|Version)'",
		"binflow-e2e",
		"rm -f /etc/apt/sources.list.d/binflow.list",
	}
	out = runDockerDeb(t, share, "debian:bookworm", strings.Join(remoteApt, "\n"))
	t.Logf("container remote apt chain:\n%s", out)
	for _, want := range []string{
		"Status: install ok installed",
		"Version: 1.0-1",
		"binflow t314 remote",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("remote apt chain missing %q:\n%s", want, out)
		}
	}

	// ---- the virtual leg: one update serving BOTH members (a fresh
	// container), both packages installed through the aggregate ----
	virtApt := []string{
		"set -e",
		"printf 'deb [trusted=yes] " + host + "/binflow/deb-virt stable main\\n' > /etc/apt/sources.list.d/binflow.list",
		"apt-get update 2>&1 | tail -2",
		"apt-get install -y --no-install-recommends binflow-virt binflow-e2e 2>&1 | tail -2",
		"dpkg -s binflow-virt | grep -E '^(Status|Version)'",
		"dpkg -s binflow-e2e | grep -E '^(Status|Version)'",
		"binflow-virt && binflow-e2e",
	}
	out = runDockerDeb(t, share, "debian:bookworm", strings.Join(virtApt, "\n"))
	t.Logf("container virtual apt chain:\n%s", out)
	for _, want := range []string{
		"Status: install ok installed",
		"Version: 2.0-1",
		"binflow t314 virtual",
		"binflow t314 remote",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("virtual apt chain missing %q:\n%s", want, out)
		}
	}
}

// runDockerDeb runs one container command with the share mounted.
func runDockerDeb(t *testing.T, share, image, script string) string {
	t.Helper()
	cmd := exec.Command("docker", "run", "--rm",
		"-v", share+":/e2e",
		"--add-host=host.docker.internal:host-gateway",
		image, "bash", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run failed: %v\n--- output ---\n%s", err, out)
	}
	return string(out)
}

// portOf extracts the httptest server's port.
func portOf(url string) string {
	i := strings.LastIndexByte(url, ':')
	if i < 0 {
		return ""
	}
	return url[i+1:]
}
