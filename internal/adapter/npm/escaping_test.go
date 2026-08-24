package npm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/client"
	"github.com/lzwzzy/binflow/internal/storage"
)

// T-261 (FR-82-AC5) — the packument tarball-URL escaping contract. The
// adapter's third per-segment escape isomorph collapsed into
// client.EscapePathSegments (T-231's exported contract; T-233 took the migrate
// reader's copy). These tests pin the WIRE SPELLING byte-for-byte so the
// convergence is provably behavior-neutral and any future drift of the shared
// contract breaks loudly at the npm faces too.
//
// In-protocol note: npm's own publish path can never produce these paths —
// package names are charset-gated ([A-Za-z0-9._~-], npmname.go NFR-S18) and
// versions are strict semver. The escaped spellings fire on STORED dist
// references whose rel carries special characters: a remote-proxied packument
// from an upstream with legacy names, or a -rev PUT echoing served URLs back
// (the npm 10 unpublish spelling) — both reduce to a BinFlow-relative literal
// rel at store time and re-escape at serve time.

// escapedNameMatrix is the T-231 five-character regression matrix applied to
// the npm face (name-shaped single segments), plus the npm structural edges:
// the legal-charset surface must pass through UNTOUCHED (escaping is a no-op
// there — registry.npmjs.org spells scoped and tilde names literally), while
// every character a URL cannot carry raw is percent-encoded.
var escapedNameMatrix = []struct {
	name string // literal segment spelling
	esc  string // exact wire spelling
}{
	{"t261-plain", "t261-plain"},
	{"t261~esc", "t261~esc"},
	{"@t261/scoped~pkg", "@t261/scoped~pkg"},
	{"t261 esc", "t261%20esc"},
	{"t261%esc", "t261%25esc"},
	{"t261#esc", "t261%23esc"},
	{"t261?esc", "t261%3Fesc"},
	{"t261中文", "t261%E4%B8%AD%E6%96%87"},
	{"sym'bols$esc", "sym%27bols$esc"},
	{"semi;colon,esc", "semi%3Bcolon%2Cesc"},
}

// TestTarballURLWireSpellingPinned pins the exact wire spelling of the shared
// escaping contract over the matrix: separating slashes stay literal, the
// legal npm charset passes through, and the T-231 five-character family
// (space, %, #, ?, UTF-8) percent-encodes — the same bytes the pre-T-261
// package-private copy produced (isomorph-probed over a 299-case corpus
// before the convergence landed).
func TestTarballURLWireSpellingPinned(t *testing.T) {
	for _, tt := range escapedNameMatrix {
		rel := tt.name + "/-/" + tt.name + "-1.0.0.tgz"
		want := tt.esc + "/-/" + tt.esc + "-1.0.0.tgz"
		if got := client.EscapePathSegments(rel); got != want {
			t.Errorf("EscapePathSegments(%q) = %q, want %q", rel, got, want)
		}
	}
	// Structural edges beyond the name matrix.
	edges := []struct{ in, want string }{
		{"", ""},
		{"dir/", "dir/"},
		{"a%2Falready.tgz", "a%252Falready.tgz"}, // literal % double-encodes
		{"x+y/z.tgz", "x+y/z.tgz"},               // '+' is reserved-legal in segments
		{"1.0.0+build.7.tgz", "1.0.0+build.7.tgz"},
	}
	for _, tt := range edges {
		if got := client.EscapePathSegments(tt.in); got != tt.want {
			t.Errorf("EscapePathSegments(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRenderPackumentEscapesTarballRefs pins the render face: every version's
// dist.tarball is rewritten to the registry mount with the escaped spelling,
// both in the full packument and the SLIM ("corgi") negotiation (dist is on
// the SLIM whitelist — the escaped URL must survive the reduction).
func TestRenderPackumentEscapesTarballRefs(t *testing.T) {
	doc := func(rel string) map[string]any {
		return map[string]any{
			"_id": "t261 esc", "name": "t261 esc",
			"dist-tags": map[string]any{"latest": "1.0.0"},
			"versions": map[string]any{
				"1.0.0": map[string]any{
					"name": "t261 esc", "version": "1.0.0",
					"dist": map[string]any{"tarball": rel, "shasum": "aa"},
				},
			},
		}
	}
	wantURL := "http://registry.test/binflow/api/npm/npm-local/t261%20esc/-/t261%20esc-1.0.0.tgz"

	t.Run("stored literal rel", func(t *testing.T) {
		body, ct, err := renderPackument(doc("t261 esc/-/t261 esc-1.0.0.tgz"),
			"t261 esc", "http", "registry.test", "http://registry.test", "npm-local", false)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if ct != "application/json" {
			t.Fatalf("content type = %q", ct)
		}
		if want := `"tarball":"` + wantURL + `"`; !strings.Contains(string(body), want) {
			t.Fatalf("rendered body missing %s:\n%s", want, body)
		}
	})

	t.Run("echoed absolute URL reduces and re-escapes identically", func(t *testing.T) {
		// The -rev echo shape: the client PUTs back the SERVED absolute URL;
		// the render must normalize it to the same literal rel first.
		body, _, err := renderPackument(doc(wantURL),
			"t261 esc", "http", "registry.test", "http://registry.test", "npm-local", false)
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if want := `"tarball":"` + wantURL + `"`; !strings.Contains(string(body), want) {
			t.Fatalf("rendered body missing %s:\n%s", want, body)
		}
	})

	t.Run("SLIM negotiation keeps the escaped URL", func(t *testing.T) {
		body, ct, err := renderPackument(doc("t261%esc/-/t261%esc-1.0.0.tgz"),
			"t261%esc", "http", "registry.test", "http://registry.test", "npm-local", true)
		if err != nil {
			t.Fatalf("render slim: %v", err)
		}
		if ct != contentTypeSLIM {
			t.Fatalf("slim content type = %q", ct)
		}
		want := `"tarball":"http://registry.test/binflow/api/npm/npm-local/t261%25esc/-/t261%25esc-1.0.0.tgz"`
		if !strings.Contains(string(body), want) {
			t.Fatalf("slim body missing %s:\n%s", want, body)
		}
	})
}

// TestServeFacesEscapedTarballRoundtrip walks the escaped spelling through
// the HTTP faces on the real-collaborator stack (real storage + repo
// service). The production firing shape: a LEGAL package name whose stored
// dist.tarball reference carries special characters — a -rev echo of a served
// URL or a remote-relayed packument reduces to such a literal rel at store
// time (npm's own publish charset gate can never produce it). The tarball
// FETCH then goes out at the escaped URL and the adapter Layout must decode
// it back to the literal node path and stream the bytes.
//
// versionDistTarball's resolution order (spec section 2.4, pre-existing and
// untouched by the convergence) has two arms the expectations encode:
//
//   - a binflow-mount URL whose tail survives reduction is honored verbatim
//     (every matrix character except a literal '%');
//   - a '%' in the tail fails the tail's SECOND percent-decode — and the
//     -rev echo's stored BARE rel never matches the mount shape at all — so
//     both fall back to the canonical <pkg>/-/<pkg>-<version>.tgz, which the
//     post-echo assertion pins as-is.
func TestServeFacesEscapedTarballRoundtrip(t *testing.T) {
	// The PACKAGE name stays legal so the charset-validated faces (publish,
	// packument GET) stay in play; the tarball REL name walks the matrix.
	for _, tt := range escapedNameMatrix {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			ctx := context.Background()
			const pkg = "t261-legal"
			payload := "T261-TARBALL:" + tt.name
			rel := tt.name + "/-/" + tt.name + "-1.0.0.tgz"
			const prefix = "http://registry.test/binflow/api/npm/npm-local/"
			canonicalURL := prefix + pkg + "/-/" + pkg + "-1.0.0.tgz"
			honoredURL := prefix + tt.esc + "/-/" + tt.esc + "-1.0.0.tgz"
			renderedURL := honoredURL
			if tt.name == "t261%esc" {
				renderedURL = canonicalURL // the % tail's double-decode fails
			}

			put := func(path, body, ctype string) {
				t.Helper()
				sum := sha256.Sum256([]byte(body))
				_, err := s.svc.Put(ctx, adminPrincipal, "npm-local", path,
					bytes.NewReader([]byte(body)),
					storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}, ctype)
				if err != nil {
					t.Fatalf("seed node %s: %v", path, err)
				}
			}

			// Tarball bytes at the literal (unescaped) node path.
			put(rel, payload, "application/octet-stream")
			// The packument of the LEGAL package whose dist reference is the
			// special-character binflow-mount URL — the -rev echo shape.
			doc := map[string]any{
				"_id": pkg, "name": pkg,
				"dist-tags": map[string]any{"latest": "1.0.0"},
				"versions": map[string]any{
					"1.0.0": map[string]any{
						"name": pkg, "version": "1.0.0",
						"dist": map[string]any{
							"tarball": honoredURL,
							"shasum":  "aa",
						},
					},
				},
			}
			docBody, err := encodeDoc(doc)
			if err != nil {
				t.Fatalf("encode doc: %v", err)
			}
			put(packumentPath(pkg), string(docBody), "application/json")

			wantField := `"tarball":"` + renderedURL + `"`

			// Face 1: the packument render (renderPackument).
			rr := s.call(http.MethodGet, "/npm-local/"+pkg, "", adminPrincipal, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("packument GET = %d; body=%s", rr.Code, bodyOf(rr))
			}
			if !strings.Contains(bodyOf(rr), wantField) {
				t.Fatalf("packument missing %s:\n%s", wantField, bodyOf(rr))
			}

			// Face 2: the one-version manifest render (serveVersion).
			rr = s.call(http.MethodGet, "/npm-local/"+pkg+"/1.0.0", "", adminPrincipal, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("version GET = %d; body=%s", rr.Code, bodyOf(rr))
			}
			if !strings.Contains(bodyOf(rr), wantField) {
				t.Fatalf("version manifest missing %s:\n%s", wantField, bodyOf(rr))
			}

			// Face 3: fetch the tarball AT the escaped URL — the layout must
			// percent-decode back to the literal node path and stream bytes.
			rr = s.call(http.MethodGet, "/npm-local/"+tt.esc+"/-/"+tt.esc+"-1.0.0.tgz",
				"", adminPrincipal, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("tarball GET = %d; body=%s", rr.Code, bodyOf(rr))
			}
			if got := bodyOf(rr); got != payload {
				t.Fatalf("tarball bytes = %q, want %q", got, payload)
			}
			if ct := rr.Header().Get("Content-Type"); ct != "application/octet-stream" {
				t.Fatalf("tarball content type = %q", ct)
			}

			// Face 4: the npm 10 echo — PUT the SERVED packument back at -rev,
			// then re-render. applyRevDocument stores the reduced BARE rel,
			// which the render's resolution order does not recognize, so the
			// reference collapses to the canonical spelling — pre-existing
			// behavior, pinned byte-for-byte (a change here is a semantic
			// decision, never a silent side effect of the convergence).
			served := s.call(http.MethodGet, "/npm-local/"+pkg, "", adminPrincipal, nil)
			rr = s.call(http.MethodPut, "/npm-local/"+pkg+"/-rev/1-abc",
				bodyOf(served), adminPrincipal, map[string]string{"Content-Type": "application/json"})
			if rr.Code != http.StatusOK {
				t.Fatalf("echo -rev PUT = %d; body=%s", rr.Code, bodyOf(rr))
			}
			rr = s.call(http.MethodGet, "/npm-local/"+pkg, "", adminPrincipal, nil)
			want := `"tarball":"` + canonicalURL + `"`
			if rr.Code != http.StatusOK || !strings.Contains(bodyOf(rr), want) {
				t.Fatalf("post-echo packument missing %s (code %d):\n%s", want, rr.Code, bodyOf(rr))
			}
		})
	}
}

// TestSpecialCharNameUnvalidatedFaces covers the faces that take a
// special-character PACKAGE NAME directly (they run no charset gate by
// design): the one-version manifest (serveVersion) and the tarball fetch.
// The packument GET face answers its pinned 400 (validatePackageName precedes
// the render — unchanged posture, pinned here so the convergence cannot be
// mistaken for a validation change).
func TestSpecialCharNameUnvalidatedFaces(t *testing.T) {
	for _, tt := range []struct{ name, esc string }{
		{"t261 esc", "t261%20esc"},
		{"t261%esc", "t261%25esc"},
		{"t261中文", "t261%E4%B8%AD%E6%96%87"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newStack(t)
			ctx := context.Background()
			payload := "T261-TARBALL:" + tt.name
			rel := tt.name + "/-/" + tt.name + "-1.0.0.tgz"

			put := func(path, body, ctype string) {
				t.Helper()
				sum := sha256.Sum256([]byte(body))
				_, err := s.svc.Put(ctx, adminPrincipal, "npm-local", path,
					bytes.NewReader([]byte(body)),
					storage.BlobRef{Sha256: hex.EncodeToString(sum[:])}, ctype)
				if err != nil {
					t.Fatalf("seed node %s: %v", path, err)
				}
			}
			put(rel, payload, "application/octet-stream")
			doc := map[string]any{
				"_id": tt.name, "name": tt.name,
				"dist-tags": map[string]any{"latest": "1.0.0"},
				"versions": map[string]any{
					"1.0.0": map[string]any{
						"name": tt.name, "version": "1.0.0",
						"dist": map[string]any{
							"tarball": "http://registry.test/binflow/api/npm/npm-local/" +
								tt.esc + "/-/" + tt.esc + "-1.0.0.tgz",
							"shasum": "aa",
						},
					},
				},
			}
			docBody, err := encodeDoc(doc)
			if err != nil {
				t.Fatalf("encode doc: %v", err)
			}
			put(packumentPath(tt.name), string(docBody), "application/json")

			wantURL := "http://registry.test/binflow/api/npm/npm-local/" +
				tt.esc + "/-/" + tt.esc + "-1.0.0.tgz"

			// The packument GET stays the pinned charset 400 (pre-render
			// gate, untouched by the convergence).
			rr := s.call(http.MethodGet, "/npm-local/"+tt.esc, "", adminPrincipal, nil)
			if rr.Code != http.StatusBadRequest || !strings.Contains(bodyOf(rr), "illegal character") {
				t.Fatalf("packument GET at special name = %d (want 400 charset); body=%s", rr.Code, bodyOf(rr))
			}

			// serveVersion renders the escaped URL without a charset gate.
			rr = s.call(http.MethodGet, "/npm-local/"+tt.esc+"/1.0.0", "", adminPrincipal, nil)
			if rr.Code != http.StatusOK {
				t.Fatalf("version GET = %d; body=%s", rr.Code, bodyOf(rr))
			}
			if want := `"tarball":"` + wantURL + `"`; !strings.Contains(bodyOf(rr), want) {
				t.Fatalf("version manifest missing %s:\n%s", want, bodyOf(rr))
			}

			// The tarball fetch at the escaped URL streams the bytes.
			rr = s.call(http.MethodGet, "/npm-local/"+tt.esc+"/-/"+tt.esc+"-1.0.0.tgz",
				"", adminPrincipal, nil)
			if rr.Code != http.StatusOK || bodyOf(rr) != payload {
				t.Fatalf("tarball GET = %d; body=%s", rr.Code, bodyOf(rr))
			}

			// The -rev echo is IDEMPOTENT for the special name itself: the
			// canonical fallback equals the stored rel (both are
			// <name>/-/<name>-<version>.tgz), so whatever arm the resolution
			// order takes — mount-URL reduction, or the bare-rel canonical
			// fallback after the echo — the rendered URL comes back
			// byte-identical.
			rr = s.call(http.MethodPut, "/npm-local/"+tt.esc+"/-rev/1-abc",
				mustJSON(doc), adminPrincipal, map[string]string{"Content-Type": "application/json"})
			if rr.Code != http.StatusOK {
				t.Fatalf("echo -rev PUT = %d; body=%s", rr.Code, bodyOf(rr))
			}
			rr = s.call(http.MethodGet, "/npm-local/"+tt.esc+"/1.0.0", "", adminPrincipal, nil)
			if want := `"tarball":"` + wantURL + `"`; rr.Code != http.StatusOK || !strings.Contains(bodyOf(rr), want) {
				t.Fatalf("post-echo version manifest missing %s (code %d):\n%s", want, rr.Code, bodyOf(rr))
			}
		})
	}
}
