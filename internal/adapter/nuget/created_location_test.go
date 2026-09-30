package nuget

// T-567 / BIN-49 (L036 section 2's six-point ruling), the nuget pair:
//
//   - the BARE-content PUT 201 renders its Location header as the
//     ABSOLUTE, context-prefixed address (scheme://host/binflow/<repo>/
//     <deployment path>) — the A face (Artifactory 7.161.26 behind the
//     /artifactory context root) renders the bare PUT 201 so live; the
//     bare repo-relative form is a mis-anchored value a client resolves
//     against the wrong base. (The probe's companion X-Checksum-Sha256 on
//     that A face landed later in T-575 / L037 Arm 2 — see
//     checksum_header_test.go.)
//   - the v3 push 201 (both URL shapes) carries NO Location header — the
//     A face's multipart PUT 201 carries none, and the former
//     flatcontainer/<id>/<version>/<file> value was mis-anchored: a
//     client's relative resolution stacked a second flatcontainer path.

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
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

// T-579 / BIN-61 family: the 201 bodies. The bare-content PUT 201 carries
// the ItemCreated envelope (CT + Jackson-pretty-printed field set, the
// live A raw of 2026-09-30 — Artifactory 7.161.26 — pinned in
// created.go); the v3 push 201 carries the publish family's text body
// (L038 candidate ①'s closure).

// envelopeCreatedRE normalizes the envelope's created timestamp (the one
// field the clock owns).
var envelopeCreatedRE = regexp.MustCompile(`"created" : "[^"]*"`)

// TestBarePutCreatedEnvelope: the bare-content PUT 201 body is the
// ItemCreated envelope — byte-exact against the A raw's shape (2-space
// indent, " : " separators, uri last, no trailing newline) with the
// originalChecksums rule the raw pins: sha256 always (declared-else-
// measured), sha1/md5 only when declared. The uri equals the Location
// header byte-for-byte.
func TestBarePutCreatedEnvelope(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-env", repo.TypeLocal)

	tests := []struct {
		name string
		id   string
		hdr  map[string]string // the X-Checksum-* request headers
	}{
		{name: "no checksum headers", id: "Env.Nohdr", hdr: nil},
		{name: "correct sha256 declared", id: "Env.Hdr", hdr: map[string]string{}},
		{name: "all three declared", id: "Env.All3", hdr: map[string]string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := buildNupkg(t, tc.id, "1.0.0", flatDeps("none"))
			body := pkg.body
			sha256v, sha1v, md5v := sha256Hex(body), sha1Hex(body), md5Hex(body)
			switch tc.name {
			case "correct sha256 declared":
				tc.hdr["X-Checksum-Sha256"] = sha256v
			case "all three declared":
				tc.hdr["X-Checksum-Sha256"] = sha256v
				tc.hdr["X-Checksum-Sha1"] = sha1v
				tc.hdr["X-Checksum-Md5"] = md5v
			}
			rel := tc.id + "/1.0.0/" + tc.id + ".1.0.0" + suffixNupkg
			path := "/binflow/ng-env/" + rel
			status, respBody, hdr := s.put(path, body, tc.hdr)
			if status != http.StatusCreated {
				t.Fatalf("bare PUT = (%d, %s), want 201", status, respBody)
			}
			if ct := hdr.Get("Content-Type"); ct != contentTypeItemCreated {
				t.Errorf("201 Content-Type = %q, want %q", ct, contentTypeItemCreated)
			}
			loc := hdr.Get("Location")
			if want := s.srv.URL + path; loc != want {
				t.Errorf("201 Location = %q, want %q", loc, want)
			}

			// The measured checksums block (always the full triple).
			checksumsBlock := "  \"checksums\" : {\n" +
				"    \"sha1\" : \"" + sha1v + "\",\n" +
				"    \"md5\" : \"" + md5v + "\",\n" +
				"    \"sha256\" : \"" + sha256v + "\"\n" +
				"  },"
			// The originalChecksums block per the raw's rule.
			oc := "  \"originalChecksums\" : {\n"
			switch tc.name {
			case "all three declared":
				oc += "    \"sha1\" : \"" + sha1v + "\",\n" +
					"    \"md5\" : \"" + md5v + "\",\n" +
					"    \"sha256\" : \"" + sha256v + "\"\n"
			default: // sha256 always (declared value == measured value)
				oc += "    \"sha256\" : \"" + sha256v + "\"\n"
			}
			oc += "  },"
			want := "{\n" +
				"  \"repo\" : \"ng-env\",\n" +
				"  \"path\" : \"/" + rel + "\",\n" +
				"  \"created\" : \"<TS>\",\n" +
				"  \"createdBy\" : \"admin\",\n" +
				"  \"downloadUri\" : \"" + loc + "\",\n" +
				"  \"mimeType\" : \"application/x-nupkg\",\n" +
				"  \"size\" : \"" + strconv.Itoa(len(body)) + "\",\n" +
				checksumsBlock + "\n" +
				oc + "\n" +
				"  \"uri\" : \"" + loc + "\"\n" +
				"}"
			got := envelopeCreatedRE.ReplaceAllString(respBody, `"created" : "<TS>"`)
			if got != want {
				t.Errorf("201 envelope body mismatch\n got: %q\nwant: %q", got, want)
			}
			if strings.HasSuffix(respBody, "\n") {
				t.Errorf("201 envelope body carries a trailing newline (the A raw ends at the closing brace)")
			}
		})
	}
}

// TestV3PushCreatedBody: both v3 push entrances' 201 body is the publish
// family's text — "Successfully published NuPkg to: flatcontainer/<id>/
// <version>/<file>" (the ADDRESSED form's A-raw shape; the DIRECT form
// renders B's own canonical landing — A routes the publish base through
// its v2 catch-all, the registered candidate ② layout divergence).
func TestV3PushCreatedBody(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-env", repo.TypeLocal)

	tests := []struct {
		name string
		id   string
		path string
	}{
		{
			name: "ADDRESSED form",
			id:   "Env.PushA",
			path: pushPath("ng-env", "env.pusha", "1.0.0"),
		},
		{
			name: "DIRECT form (publish base)",
			id:   "Env.PushD",
			path: apiPath("ng-env") + "/" + segFlat,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkg := buildNupkg(t, tc.id, "1.0.0", flatDeps("none"))
			status, body, hdr := s.do(http.MethodPut, tc.path, adminUser, adminPass, bytesReader(pkg.body), nil)
			if status != http.StatusCreated {
				t.Fatalf("push = (%d, %s), want 201", status, body)
			}
			// The landing path lowercases the package id (the nuget
			// layout's id case-folding).
			low := strings.ToLower(tc.id)
			want := "Successfully published NuPkg to: " + segFlat + "/" + low + "/1.0.0/" + low + ".1.0.0" + suffixNupkg
			if body != want {
				t.Errorf("201 body = %q, want %q", body, want)
			}
			if ct := hdr.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
				t.Errorf("201 Content-Type = %q, want the v2 face's text/plain renderer", ct)
			}
			if v := hdr.Get("X-Checksum-Sha256"); v != "" {
				t.Errorf("201 X-Checksum-Sha256 = %q, want none", v)
			}
			if v := hdr.Get("Location"); v != "" {
				t.Errorf("201 Location = %q, want none", v)
			}
		})
	}
}
