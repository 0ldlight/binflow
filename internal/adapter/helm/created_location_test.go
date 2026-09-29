package helm

// T-567 / BIN-49 (L036 section 2's six-point ruling): every 201 the helm
// adapter renders carries its Location header as the ABSOLUTE,
// context-prefixed address of the landed node (scheme://host/binflow/
// <repo>/<deployment path>) — the shape the A face (Artifactory 7.161.26
// behind the /artifactory context root) renders live:
// http://…/artifactory/<repo>/charts/<f>.tgz, path part the deployment
// path unchanged. The bare repo-relative form the handler used to render
// is a mis-anchored value a client resolves against the wrong base.
// Covers both writeCreated callers: the chart PUT chain and the plain
// storage face (.prov sidecars).

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestCreatedLocationContextPrefix: every write face's 201 Location is the
// absolute, context-prefixed address, and the value GETs the landed bytes
// through the same /binflow routing.
func TestCreatedLocationContextPrefix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-loc", repo.TypeLocal, `{}`)

	tests := []struct {
		name string
		path string
		body []byte
	}{
		{
			name: "chart PUT (root path)",
			path: "/binflow/helm-loc/locchart-0.1.0.tgz",
			body: fixtureChart(t, "locchart", defaultChartYAML("locchart", "0.1.0"), nil),
		},
		{
			name: "chart PUT (subdir path)",
			path: "/binflow/helm-loc/stable/locchart-0.2.0.tgz",
			body: fixtureChart(t, "locchart", defaultChartYAML("locchart", "0.2.0"), nil),
		},
		{
			name: "plain storage face (.prov sidecar)",
			path: "/binflow/helm-loc/locchart-0.1.0.tgz.prov",
			body: []byte("prov-bytes"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, body, hdr := s.put(tc.path, tc.body, nil)
			if status != http.StatusCreated {
				t.Fatalf("PUT = (%d, %s), want 201", status, body)
			}
			want := s.srv.URL + tc.path
			if loc := hdr.Get("Location"); loc != want {
				t.Errorf("Location = %q, want the absolute %q", loc, want)
			}
			// Resolvability anchor: the Location addresses the landed node
			// through the same /binflow routing (never a bare-root 404).
			gstatus, gbody, _ := s.get(tc.path)
			if gstatus != http.StatusOK || gbody != string(tc.body) {
				t.Errorf("GET deployed path = (%d, %d bytes), want 200 with the landed bytes", gstatus, len(gbody))
			}
		})
	}
}
