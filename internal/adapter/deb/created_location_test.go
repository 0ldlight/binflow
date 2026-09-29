package deb

// T-567 / BIN-49 (L036 section 2's six-point ruling): every 201 the deb
// adapter renders carries its Location header as the ABSOLUTE,
// context-prefixed address of the landed node (scheme://host/binflow/
// <repo>/<deployment path>) — the shape the A face (Artifactory 7.161.26
// behind the /artifactory context root) renders live:
// http://…/artifactory/<repo>/pool/main/r/<n>/<f>.deb, path part the
// deployment path unchanged. The bare repo-relative form the handler used
// to render is a mis-anchored value a client resolves against the wrong
// base. Covers all three writeCreated callers: the debPUT binary face,
// the .dsc source face and the plain storage face.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestCreatedLocationContextPrefix: every write face's 201 Location is the
// absolute, context-prefixed address (matrix parameters ride the debPUT
// legs but are NOT part of the Location), and the value GETs the landed
// bytes through the same /binflow routing.
func TestCreatedLocationContextPrefix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-loc", repo.TypeLocal, `{}`)

	tests := []struct {
		name string
		// path is the request path under /binflow; the debPUT legs carry
		// their matrix coordinates inline (the wire shape debPut builds).
		path string
		body []byte
	}{
		{
			name: "debPUT binary face (matrix coordinates)",
			path: "/binflow/deb-loc/pool/main/m/locpkg/locpkg_1.0_amd64.deb;deb.distribution=stable;deb.component=main;deb.architecture=amd64",
			body: helloDeb("locpkg", "1.0", "amd64"),
		},
		{
			name: "debPUT source face (.dsc with dsc.* coordinates)",
			path: "/binflow/deb-loc/pool/main/s/locsrc/locsrc_1.0-1.dsc;dsc.distribution=stable;dsc.component=main",
			body: fixtureDsc("locsrc", "1.0-1",
				"Checksums-Sha256:\n aaaa 111 locsrc_1.0.tar.gz\n bbbb 222 locsrc_1.0-1.dsc\n"),
		},
		{
			name: "plain storage face (companion tarball)",
			path: "/binflow/deb-loc/pool/main/m/locpkg/locpkg_1.0.tar.gz",
			body: []byte("tarball-bytes"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body, hdr := s.put(tc.path, tc.body, nil)
			if status != http.StatusCreated {
				t.Fatalf("PUT = (%d, %s), want 201", status, body)
			}
			// The deployment path is the request path WITHOUT the matrix
			// parameters — the path part A renders unchanged.
			deployed := strings.SplitN(tc.path, ";", 2)[0]
			want := s.srv.URL + deployed
			if loc := hdr.Get("Location"); loc != want {
				t.Errorf("Location = %q, want the absolute %q", loc, want)
			}
			// Resolvability anchor: the Location addresses the landed node
			// through the same /binflow routing (never a bare-root 404).
			gstatus, gbody, _ := s.get(deployed)
			if gstatus != http.StatusOK || gbody != string(tc.body) {
				t.Errorf("GET deployed path = (%d, %d bytes), want 200 with the landed bytes", gstatus, len(gbody))
			}
		})
	}
}
