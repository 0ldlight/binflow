package nuget

// T-567 / BIN-49 (L036 section 2's six-point ruling), the nuget pair:
//
//   - the BARE-content PUT 201 renders its Location header as the
//     ABSOLUTE, context-prefixed address (scheme://host/binflow/<repo>/
//     <deployment path>) — the A face (Artifactory 7.161.26 behind the
//     /artifactory context root) renders the bare PUT 201 so live; the
//     bare repo-relative form is a mis-anchored value a client resolves
//     against the wrong base. (The probe's companion X-Checksum-Sha256 on
//     that A face is NOT taken here — unfaced surface, out of scope.)
//   - the v3 push 201 (both URL shapes) carries NO Location header — the
//     A face's multipart PUT 201 carries none, and the former
//     flatcontainer/<id>/<version>/<file> value was mis-anchored: a
//     client's relative resolution stacked a second flatcontainer path.

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestBarePutLocationContextPrefix: the bare-content PUT's 201 Location is
// the absolute, context-prefixed address, and the value GETs the landed
// bytes through the same /binflow routing.
func TestBarePutLocationContextPrefix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-loc", repo.TypeLocal)

	tests := []struct {
		name string
		rel  string // repo-relative deployment path (no v2/v3 plane segment)
	}{
		{name: "nested path", rel: "docs/readme.txt"},
		{name: "flat path", rel: "notes.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/binflow/ng-loc/" + tc.rel
			status, body, hdr := s.put(path, []byte("loc-bytes"), nil)
			if status != http.StatusCreated {
				t.Fatalf("bare PUT = (%d, %s), want 201", status, body)
			}
			want := s.srv.URL + path
			if loc := hdr.Get("Location"); loc != want {
				t.Errorf("Location = %q, want the absolute %q", loc, want)
			}
			// Resolvability anchor: the Location addresses the landed node
			// through the same /binflow routing (never a bare-root 404).
			gstatus, gbody, _ := s.get(path)
			if gstatus != http.StatusOK || gbody != "loc-bytes" {
				t.Errorf("GET deployed path = (%d, %q), want 200 with the landed bytes", gstatus, gbody)
			}
		})
	}
}

// TestV3PushCreatedHasNoLocation: both v3 push URL shapes (the DIRECT
// publish-base form and the addressed flatcontainer/<id>/<version> form)
// answer 201 WITHOUT a Location header (T-567: the A face's push 201
// carries none; the removed header's value was mis-anchored).
func TestV3PushCreatedHasNoLocation(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-loc", repo.TypeLocal)

	tests := []struct {
		name string
		id   string // the nuspec identity (the DIRECT form's only source)
		path string
	}{
		{
			name: "DIRECT form (publish base)",
			id:   "Loc.Direct",
			path: apiPath("ng-loc") + "/" + segFlat,
		},
		{
			name: "ADDRESSED form (flatcontainer/<id>/<version>)",
			id:   "Loc.Addr",
			path: pushPath("ng-loc", "loc.addr", "1.0.0"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := buildNupkg(t, tc.id, "1.0.0", flatDeps("none"))
			status, body, hdr := s.do(http.MethodPut, tc.path, adminUser, adminPass, bytesReader(pkg.body), nil)
			if status != http.StatusCreated {
				t.Fatalf("push = (%d, %s), want 201", status, body)
			}
			if loc := hdr.Get("Location"); loc != "" {
				t.Errorf("201 Location = %q, want none (T-567: the A face's push 201 carries no Location)", loc)
			}
		})
	}
}
