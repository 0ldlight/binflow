package maven

import (
	"net/http"
	"strings"
	"testing"
)

// TestChecksumDeploySha1Only: the T-73 maven slice (PRD §6.4-1) — a sha1-only
// zero-transfer deploy of an already-present jar to a NEW GAV is a 201, the
// landed node serves the identical bytes with the ledger's digests, and the
// C15 miss/malformed/both-empty family keeps its shapes. Policy gates bind a
// zero-byte deploy like any other (ME-08), and the FR-17 metadata calculator
// still fires.
func TestChecksumDeploySha1Only(t *testing.T) {
	hs := newHarness(t)
	// A pom fact first: the module's maven-metadata.xml computes off the pom
	// (a jar alone does not materialize the module document).
	if resp := hs.serve(http.MethodPut, pomPath, []byte("<project/>"), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom seed: %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed deploy: %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	hs.waitCalc()
	s1, _, s256 := digests(jarBytes)

	// The form the ticket is about: sha1 ONLY, new GAV, zero body.
	newGAV := "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar"
	resp := hs.serve(http.MethodPut, newGAV, nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha1": s1}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1-only checksum deploy = %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	if v := resp.Header.Get("X-Checksum-Sha256"); v != s256 {
		t.Fatalf("response sha256 = %q, want the resolved blob's %q", v, s256)
	}

	// The deployed node serves the identical bytes with the ledger digests.
	got := hs.serve(http.MethodGet, newGAV, nil, nil, true)
	b := drain(t, got)
	if got.StatusCode != http.StatusOK || string(b) != string(jarBytes) {
		t.Fatalf("deployed copy = %d %q", got.StatusCode, b)
	}
	if v := got.Header.Get("X-Checksum-Sha1"); v != s1 {
		t.Fatalf("deployed sha1 header = %q, want %q", v, s1)
	}

	// The FR-17 calculator fed off the zero-transfer deploy too: the pom
	// checksum-deployed into the 2.0.0 directory is a version fact, and the
	// module document picks it up (versions track pom facts; the jar alone
	// lands the bytes without registering a version).
	pom2 := "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.pom"
	pomSha1, _, _ := digests([]byte("<project/>"))
	resp = hs.serve(http.MethodPut, pom2, nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha1": pomSha1}, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("pom checksum deploy = %d %s", resp.StatusCode, string(drain(t, resp)))
	}
	hs.waitCalc()
	meta := hs.serve(http.MethodGet, "/maven-local/com/acme/demo-app/maven-metadata.xml", nil, nil, true)
	if mb := string(drain(t, meta)); meta.StatusCode != http.StatusOK || !strings.Contains(mb, "<version>2.0.0</version>") {
		t.Fatalf("metadata after checksum deploy = %d %.200s", meta.StatusCode, mb)
	}

	// Miss: the C15b 404 and wording.
	miss := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/3.0.0/demo-app-3.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1": strings.Repeat("f", 40)}, true)
	if miss.StatusCode != http.StatusNotFound || !strings.Contains(string(drain(t, miss)), "no content found for the given checksum") {
		t.Fatalf("sha1 miss = %d %s", miss.StatusCode, string(drain(t, miss)))
	}

	// Both keys empty: the C15c 400.
	empty := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/3.0.0/demo-app-3.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true"}, true)
	if empty.StatusCode != http.StatusBadRequest || !strings.Contains(string(drain(t, empty)), "no checksum header") {
		t.Fatalf("both-empty = %d %s", empty.StatusCode, string(drain(t, empty)))
	}

	// Malformed sha1: the C15d 404.
	bad := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/3.0.0/demo-app-3.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha1": "notachecksum"}, true)
	if bad.StatusCode != http.StatusNotFound || !strings.Contains(string(drain(t, bad)), "malformed sha1 value") {
		t.Fatalf("malformed sha1 = %d %s", bad.StatusCode, string(drain(t, bad)))
	}

	// The sha256 form keeps working alongside the new one.
	sha256Only := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/4.0.0/demo-app-4.0.0.jar", nil,
		map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha256": s256}, true)
	if sha256Only.StatusCode != http.StatusCreated {
		t.Fatalf("sha256-form deploy = %d %s", sha256Only.StatusCode, string(drain(t, sha256Only)))
	}
}

// TestChecksumDeployMavenGuards: the maven-plane specifics — sidecar and
// metadata paths refuse the zero-transfer form (their chains own those
// nodes), and the ME-08 policy gates bind a zero-byte deploy like any other.
func TestChecksumDeployMavenGuards(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed deploy: %d", resp.StatusCode)
	}
	s1, _, _ := digests(jarBytes)
	hdr := map[string]string{"X-Checksum-Deploy": "true", "X-Checksum-Sha1": s1}

	// Sidecar path: 400 (the sidecar chain owns those nodes).
	sidecar := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar.sha1", nil, hdr, true)
	if sidecar.StatusCode != http.StatusBadRequest || !strings.Contains(string(drain(t, sidecar)), "artifact paths only") {
		t.Fatalf("sidecar checksum deploy = %d %s", sidecar.StatusCode, string(drain(t, sidecar)))
	}
	// Metadata path: 400 likewise.
	meta := hs.serve(http.MethodPut, "/maven-local/com/acme/demo-app/maven-metadata.xml", nil, hdr, true)
	if meta.StatusCode != http.StatusBadRequest || !strings.Contains(string(drain(t, meta)), "artifact paths only") {
		t.Fatalf("metadata checksum deploy = %d %s", meta.StatusCode, string(drain(t, meta)))
	}

	// Release-refusing repository: the gate fires BEFORE the deploy, zero
	// bytes or not (ME-08 binds artifact uploads of the refused version
	// type).
	refused := hs.serve(http.MethodPut, "/maven-relonly/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar", nil, hdr, true)
	if refused.StatusCode != http.StatusConflict || !strings.Contains(string(drain(t, refused)), "handling of releases is disabled") {
		t.Fatalf("policy-gated checksum deploy = %d %s", refused.StatusCode, string(drain(t, refused)))
	}

	// Snapshot-refusing repository with a SNAPSHOT GAV: same gate.
	snapRefused := hs.serve(http.MethodPut, "/maven-snaponly/com/acme/demo-app/1.0.0-SNAPSHOT/demo-app-1.0.0-SNAPSHOT.jar", nil, hdr, true)
	if snapRefused.StatusCode != http.StatusConflict || !strings.Contains(string(drain(t, snapRefused)), "handling of snapshots is disabled") {
		t.Fatalf("snapshot-gated checksum deploy = %d %s", snapRefused.StatusCode, string(drain(t, snapRefused)))
	}
}
