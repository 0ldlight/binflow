package rpm

// The client-facing end-to-end legs:
//
//   - TestClientE2EChecksumReconciliation (always on): the full
//     repomd→indexes→packages sweep a package manager walks, with every
//     checksum field reconciled against the bytes actually served — the
//     section 5 "server obligation" contract, exercised through the real
//     router with a real HTTP client (the curl-equivalent chain).
//
//   - TestClientE2EDnfContainer (env-gated, BINFLOW_RPM_E2E_DNF=1): the
//     REAL client chain — a genuine rpmbuild-made .rpm uploaded with
//     curl, the reindex triggered, then dnf config/makecache/repoquery/
//     install inside a Rocky Linux 9 container against this test's own
//     live server. Gated so ordinary `go test` runs stay hermetic; the
//     ticket's verification run enables it and archives the output.

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// depFixture builds a package whose header REQUIRES another package —
// the dependency-resolution evidence dnf repoquery/install consume.
func depFixture(name, requires, version string) []byte {
	b := pkgHeader(name, version, "1", "noarch")
	b.strArr(tagRequireName, requires, "rpmlib(CompressedFileNames)")
	b.strArr(tagRequireVersion, "1.0-1", "3.0.4-1")
	b.i32arr(tagRequireFlags, senseGreater|senseEqual, senseGreater|senseEqual)
	return fixturePackage(name+"-"+version+"-1.noarch.rpm", b)
}

// TestClientE2EChecksumReconciliation: the whole download sweep with
// digest reconciliation at every hop.
func TestClientE2EChecksumReconciliation(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "rpm-e2e", repo.TypeLocal, "{}")
	base := pkgFixture("base", "1.0", "1", "noarch")
	dep := depFixture("consumer", "base", "1.0")
	if status, b, _ := s.put("/binflow/rpm-e2e/base-1.0-1.noarch.rpm", base, nil); status != http.StatusCreated {
		t.Fatalf("base PUT = (%d, %s)", status, b)
	}
	if status, b, _ := s.put("/binflow/rpm-e2e/consumer-1.0-1.noarch.rpm", dep, nil); status != http.StatusCreated {
		t.Fatalf("consumer PUT = (%d, %s)", status, b)
	}
	if status, b, _ := s.post("/binflow/api/yum/rpm-e2e?async=0"); status != http.StatusOK {
		t.Fatalf("reindex = (%d, %s)", status, b)
	}

	status, repomd, _ := s.get("/binflow/rpm-e2e/repodata/repomd.xml")
	if status != http.StatusOK {
		t.Fatalf("repomd = %d", status)
	}
	var doc repomdDoc
	if err := xml.Unmarshal([]byte(repomd), &doc); err != nil {
		t.Fatalf("repomd parse: %v", err)
	}
	if len(doc.Data) < 2 {
		t.Fatalf("repomd carries %d data entries", len(doc.Data))
	}
	for _, d := range doc.Data {
		st, gz, _ := s.get("/binflow/rpm-e2e/" + d.Location.Href)
		if st != http.StatusOK {
			t.Fatalf("%s serves %d", d.Location.Href, st)
		}
		if got := sha256Hex([]byte(gz)); got != d.Checksum.Value {
			t.Errorf("%s checksum: repomd %s ≠ wire %s", d.Type, d.Checksum.Value, got)
		}
		if got := sha256Hex(gunzip(t, []byte(gz))); got != d.OpenChecksum.Value {
			t.Errorf("%s open-checksum: repomd %s ≠ wire %s", d.Type, d.OpenChecksum.Value, got)
		}
		if len(gz) != d.Size || len(gunzip(t, []byte(gz))) != d.OpenSize {
			t.Errorf("%s size fields disagree with the wire bytes", d.Type)
		}
		if d.Type == "primary" {
			xmlBody := string(gunzip(t, []byte(gz)))
			// The pkgid checksums must be the .rpm files' own sha256.
			if !strings.Contains(xmlBody, `>`+sha256Hex(base)+`<`) {
				t.Error("primary pkgid does not carry the base rpm's digest")
			}
			// The dependency a resolver walks.
			if !strings.Contains(xmlBody, `<rpm:entry name="base" flags="GE" epoch="0" ver="1.0" rel="1">`) {
				t.Error("primary lost the consumer's requirement on base")
			}
		}
	}
	// The artifacts themselves: the location hrefs serve the stored bytes.
	for name, want := range map[string][]byte{
		"base-1.0-1.noarch.rpm":     base,
		"consumer-1.0-1.noarch.rpm": dep,
	} {
		st, body, hdr := s.get("/binflow/rpm-e2e/" + name)
		if st != http.StatusOK || sha256Hex([]byte(body)) != sha256Hex(want) {
			t.Fatalf("%s download broken (status %d)", name, st)
		}
		if hdr.Get("X-Checksum-Sha256") != sha256Hex(want) {
			t.Errorf("%s X-Checksum-Sha256 disagrees", name)
		}
	}
}

// TestClientE2EDnfContainer is the REAL-client leg (see the file comment
// for the gate). It needs docker and host.docker.internal reachability.
func TestClientE2EDnfContainer(t *testing.T) {
	if os.Getenv("BINFLOW_RPM_E2E_DNF") != "1" {
		t.Skip("set BINFLOW_RPM_E2E_DNF=1 to run the container dnf leg (needs docker)")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker unavailable: %v", err)
	}

	s := newStack(t)
	s.seedRepo(t, "rpm-e2e", repo.TypeLocal, "{}")
	host := "http://host.docker.internal:" + portOf(t, s.srv.URL)

	share := t.TempDir()
	spec := `Name:           binflow-e2e
Version:        1.0
Release:        1%{?dist}
Summary:        BinFlow T-311 client verification package
License:        MIT
BuildArch:      noarch

%description
A genuine rpmbuild-made package exercising BinFlow's RPM local pipeline.

%prep
:

%build
:

%install
mkdir -p %{buildroot}/opt/binflow-e2e
echo "binflow t311" > %{buildroot}/opt/binflow-e2e/hello.txt

%files
/opt/binflow-e2e/hello.txt
`
	if err := os.WriteFile(filepath.Join(share, "e2e.spec"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	// Step 1: build the genuine rpm inside the container.
	build := []string{
		"set -e",
		"dnf install -y rpm-build >/dev/null",
		`mkdir -p ~/rpmbuild/{BUILD,RPMS,SOURCES,SPECS}`,
		"cp /e2e/e2e.spec ~/rpmbuild/SPECS/",
		"rpmbuild -bb ~/rpmbuild/SPECS/e2e.spec >/dev/null",
		"cp ~/rpmbuild/RPMS/noarch/*.rpm /e2e/",
		"ls /e2e/*.rpm",
	}
	runDocker(t, share, "rockylinux:9", strings.Join(build, "\n"))

	// Step 2: upload + reindex from inside the container (the curl face).
	pkg := firstRpm(t, share)
	up := []string{
		"set -e",
		"command -v curl >/dev/null || dnf install -y curl-minimal >/dev/null",
		fmt.Sprintf(`curl -sf -u admin:password -T /e2e/%s %s/binflow/rpm-e2e/%s -o /dev/null -w 'PUT %%{http_code}\n'`, pkg, host, pkg),
		fmt.Sprintf(`curl -sf -u admin:password -X POST '%s/binflow/api/yum/rpm-e2e?async=0' -w 'REINDEX %%{http_code}\n'`, host),
		fmt.Sprintf(`curl -sf %s/binflow/rpm-e2e/repodata/repomd.xml | grep -c '<data type'`, host),
	}
	out := runDocker(t, share, "rockylinux:9", strings.Join(up, "\n"))
	if !strings.Contains(out, "PUT 201") || !strings.Contains(out, "REINDEX 200") {
		t.Fatalf("upload/reindex leg failed:\n%s", out)
	}
	t.Logf("container upload+reindex:\n%s", out)

	// Step 3: the dnf consumer chain.
	dnf := []string{
		"set -e",
		fmt.Sprintf("printf '[binflow]\\nname=BinFlow RPM\\nbaseurl=%s/binflow/rpm-e2e\\nenabled=1\\ngpgcheck=0\\nrepo_gpgcheck=0\\n' > /etc/yum.repos.d/binflow.repo", host),
		"dnf clean all >/dev/null",
		"dnf makecache --disablerepo='*' --enablerepo=binflow -v 2>&1 | grep -E 'binflow.*(repo|primary|Metalink)' | head -5",
		"echo '--- repoquery ---'",
		"dnf repoquery --repo binflow binflow-e2e",
		"echo '--- repoquery --requires ---'",
		"dnf repoquery --repo binflow --requires binflow-e2e | head -8",
		"echo '--- install ---'",
		"dnf install -y --disablerepo='*' --enablerepo=binflow binflow-e2e",
		"rpm -q binflow-e2e",
		"cat /opt/binflow-e2e/hello.txt",
	}
	out = runDocker(t, share, "rockylinux:9", strings.Join(dnf, "\n"))
	t.Logf("container dnf chain:\n%s", out)
	for _, want := range []string{
		"binflow-e2e-0:", "binflow-e2e-1.0-1.el9.noarch",
		"binflow t311", "Complete!",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dnf chain output missing %q:\n%s", want, out)
		}
	}

	// Step 4: a second makecache in a FRESH container must succeed against
	// the unchanged repodata (every run re-writes the repo file — the
	// containers do not share state).
	again := []string{
		"set -e",
		fmt.Sprintf("printf '[binflow]\\nname=BinFlow RPM\\nbaseurl=%s/binflow/rpm-e2e\\nenabled=1\\ngpgcheck=0\\nrepo_gpgcheck=0\\n' > /etc/yum.repos.d/binflow.repo", host),
		"dnf clean expire-cache >/dev/null",
		"dnf makecache --disablerepo='*' --enablerepo=binflow 2>&1 | tail -1",
		"dnf repoquery --repo binflow binflow-e2e",
	}
	out = runDocker(t, share, "rockylinux:9", strings.Join(again, "\n"))
	t.Logf("second makecache:\n%s", out)
}

// runDocker runs one container command with the share directory mounted.
func runDocker(t *testing.T, share, image, script string) string {
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
func portOf(t *testing.T, url string) string {
	t.Helper()
	i := strings.LastIndexByte(url, ':')
	if i < 0 {
		t.Fatalf("no port in %q", url)
	}
	return url[i+1:]
}

// firstRpm finds the built package's file name in the share.
func firstRpm(t *testing.T, dir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.rpm"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no rpm built in %s (err %v)", dir, err)
	}
	return filepath.Base(matches[0])
}
