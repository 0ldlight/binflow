package maven

// T-599 / BIN-81 (ledger maven/srvgen-declared-header-drop, L041 Arm 6
// s6-put-*-decl-zero legs): under server-generated-checksums the deploy's
// declared headers (X-Checksum-Md5/Sha1, all-zero placeholders included)
// register into originalChecksums UNCONDITIONALLY — "computed values serve,
// declared values archive" — while the checksums triple and every GET echo
// stay the computed digests. Three arms land the archive: the GAV artifact
// face (putFile), the GAV .sha512 ordinary-file face (putFile via the plain
// artifact parse) and the un-GAV-able .sha512 face, whose refuse gate now
// follows the checksum-policy domain (201 + archive under srvgen; the
// client-checksums default keeps the commit's own mismatch 409).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestSrvgenDeployDeclaredHeaderArchive walks the three srvgen arms with
// placeholder-zero declared digests: 201 everywhere, the envelope's
// originalChecksums carries the zeros verbatim next to the computed sha256,
// the node's client columns hold them, and the read faces (artifact GET
// headers, sidecar GET echo) keep serving the computed digests despite the
// registration.
func TestSrvgenDeployDeclaredHeaderArchive(t *testing.T) {
	hs := newHarness(t)
	zeroMd5, zeroSha1 := strings.Repeat("0", 32), strings.Repeat("0", 40)
	s1, m5, s256 := digests(jarBytes)
	decl := map[string]string{"X-Checksum-Md5": zeroMd5, "X-Checksum-Sha1": zeroSha1}

	cases := []struct {
		name string
		path string
	}{
		{name: "gav-jar", path: "/maven-lenient/com/acme/t599/1.0.0/t599-1.0.0.jar"},
		{name: "gav-sha512", path: "/maven-lenient/com/acme/t599b/1.0.0/t599b-1.0.0.jar.sha512"},
		{name: "nongav-sha512", path: "/maven-lenient/arm/r.txt.sha512"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := hs.serve(http.MethodPut, tc.path, jarBytes, decl, true)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT %s = %d, want 201 (body=%s)", tc.path, resp.StatusCode, drain(t, resp))
			}
			var env struct {
				Checksums         *checksums `json:"checksums"`
				OriginalChecksums *checksums `json:"originalChecksums"`
			}
			if err := json.Unmarshal(drain(t, resp), &env); err != nil {
				t.Fatalf("envelope parse: %v", err)
			}
			if env.Checksums == nil || env.Checksums.Sha1 != s1 || env.Checksums.Md5 != m5 || env.Checksums.Sha256 != s256 {
				t.Errorf("checksums = %+v, want the computed triple", env.Checksums)
			}
			oc := env.OriginalChecksums
			if oc == nil || oc.Sha1 != zeroSha1 || oc.Md5 != zeroMd5 || oc.Sha256 != s256 {
				t.Errorf("originalChecksums = %+v, want placeholder zeros + computed sha256", oc)
			}

			rel := strings.TrimPrefix(tc.path, "/maven-lenient/")
			node, err := hs.md.Nodes().Get(context.Background(), "maven-lenient", rel)
			if err != nil {
				t.Fatalf("node read: %v", err)
			}
			if node.ClientMd5 != zeroMd5 || node.ClientSha1 != zeroSha1 {
				t.Errorf("client columns = (%q,%q), want the declared zeros verbatim",
					node.ClientSha1, node.ClientMd5)
			}

			// The read faces stay computed despite the registration: the
			// artifact GET's echo header and the sidecar GET's body (the
			// srvgen overlay stays OFF — the deploy's own registration must
			// not flip the echo the terminal-checksum family keeps pinned).
			get := hs.serve(http.MethodGet, tc.path, nil, nil, true)
			if get.StatusCode != http.StatusOK {
				t.Fatalf("GET = %d, want 200", get.StatusCode)
			}
			if got := get.Header.Get("X-Checksum-Sha256"); got != s256 {
				t.Errorf("GET X-Checksum-Sha256 = %q, want computed %q", got, s256)
			}
			if string(drain(t, get)) != string(jarBytes) {
				t.Errorf("GET body drifted from the deployed bytes")
			}
		})
	}

	// The .md5 sidecar echo of the GAV jar: computed, not the registered
	// zeros (the s6-get family's "computed serves" half).
	if got := string(drain(t, hs.serve(http.MethodGet,
		"/maven-lenient/com/acme/t599/1.0.0/t599-1.0.0.jar.md5", nil, nil, true))); got != m5 {
		t.Fatalf("srvgen GET .md5 after declared deploy = %q, want computed %q", got, m5)
	}
	hs.waitCalc()
}

// TestSrvgenDeployWithoutDeclaredKeepsComputedOc pins the no-declaration
// regression: a srvgen deploy without X-Checksum-* leaves the client
// columns empty and the envelope's originalChecksums the {sha256} singleton
// — the archive arm must not fabricate keys nothing registered.
func TestSrvgenDeployWithoutDeclaredKeepsComputedOc(t *testing.T) {
	hs := newHarness(t)
	_, _, s256 := digests(jarBytes)
	resp := hs.serve(http.MethodPut, "/maven-lenient/com/acme/t599n/1.0.0/t599n-1.0.0.jar", jarBytes, nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	var env struct {
		OriginalChecksums *checksums `json:"originalChecksums"`
	}
	if err := json.Unmarshal(drain(t, resp), &env); err != nil {
		t.Fatalf("envelope parse: %v", err)
	}
	oc := env.OriginalChecksums
	if oc == nil || oc.Sha256 != s256 || oc.Sha1 != "" || oc.Md5 != "" {
		t.Errorf("originalChecksums = %+v, want {sha256: computed} only", oc)
	}
	node, err := hs.md.Nodes().Get(context.Background(), "maven-lenient",
		"com/acme/t599n/1.0.0/t599n-1.0.0.jar")
	if err != nil {
		t.Fatalf("node read: %v", err)
	}
	if node.ClientMd5 != "" || node.ClientSha1 != "" || node.ClientSha256 != "" {
		t.Errorf("client columns = (%q,%q,%q), want all empty",
			node.ClientSha256, node.ClientMd5, node.ClientSha256)
	}
	hs.waitCalc()
}

// TestClientPolicyDeclaredRegistrationAndRefusal pins the policy-domain
// contrast on both ordinary arms: the client-checksums default keeps the
// commit's own mismatch 409 (whitelist #8 family, zero regression) and a
// matching declaration still registers through the ordinary chain.
func TestClientPolicyDeclaredRegistrationAndRefusal(t *testing.T) {
	hs := newHarness(t)
	zeroMd5, zeroSha1 := strings.Repeat("0", 32), strings.Repeat("0", 40)
	s1, m5, s256 := digests(jarBytes)
	wrong := map[string]string{"X-Checksum-Md5": zeroMd5, "X-Checksum-Sha1": zeroSha1}

	for _, path := range []string{
		"/maven-local/com/acme/t599c/1.0.0/t599c-1.0.0.jar",
		"/maven-local/arm/r.txt.sha512",
	} {
		resp := hs.serve(http.MethodPut, path, jarBytes, wrong, true)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("client-policy PUT %s = %d, want 409 (body=%s)", path, resp.StatusCode, drain(t, resp))
		}
		if msg := string(drain(t, resp)); !strings.Contains(msg, "Checksum error for '") ||
			// the reshape picks the first mismatched algorithm of its
			// sha256→sha1→md5 scan: the sha1 placeholder surfaces.
			!strings.Contains(msg, "received '"+zeroSha1+"'") {
			t.Errorf("409 body = %q, want the storage mismatch family citing the placeholder", msg)
		}
	}

	ok := map[string]string{"X-Checksum-Md5": m5, "X-Checksum-Sha1": s1}
	resp := hs.serve(http.MethodPut, "/maven-local/com/acme/t599d/1.0.0/t599d-1.0.0.jar", jarBytes, ok, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("client-policy matching PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	var env struct {
		Checksums         *checksums `json:"checksums"`
		OriginalChecksums *checksums `json:"originalChecksums"`
	}
	if err := json.Unmarshal(drain(t, resp), &env); err != nil {
		t.Fatalf("envelope parse: %v", err)
	}
	if env.OriginalChecksums == nil || env.OriginalChecksums.Md5 != m5 || env.OriginalChecksums.Sha1 != s1 {
		t.Errorf("originalChecksums = %+v, want the registered declared set", env.OriginalChecksums)
	}
	if env.Checksums == nil || env.Checksums.Sha256 != s256 {
		t.Errorf("checksums = %+v, want the computed triple", env.Checksums)
	}
	hs.waitCalc()
}
