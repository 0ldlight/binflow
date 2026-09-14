package conan

// L019 (LOOP 019, conan D2/D3/D4 alignment) — the local plane's error
// rendering after the conductor rulings: the ghost-GET family answers the
// reference's errors[] envelope ("Not Found"), the ghost-delete family
// keeps its "Couldn't find path '<p>'" message inside the envelope without
// the as-built trailing slash, and the v2 packageId-metadata search
// rejects the three evidenced query spellings with the reference's
// "Unexpected query syntax:" plain text.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// isNotFoundEnvelope matches the D2 404 body: the errors[] envelope
// carrying the reference's global "Not Found" message (format-agnostic —
// normalize R2 owns the indent style).
func isNotFoundEnvelope(body string) bool {
	return strings.Contains(body, `"errors"`) && strings.Contains(body, `"Not Found"`)
}

// TestErrorEnvelopeLocalPlane pins the D2 family on the local plane: every
// ghost-GET 404 is the envelope, the ghost-delete 404 quotes the bare path
// (no trailing slash), and the bodies ride Content-Type application/json.
func TestErrorEnvelopeLocalPlane(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev := fixtureRev(2)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))

	cases := []struct {
		name    string
		method  string
		path    string
		wantMsg string
	}{
		{"ghost latest", http.MethodGet, v2("cn-local", "nope/1.0/_/_/latest"), msgNotFound},
		{"ghost revisions", http.MethodGet, v2("cn-local", "nope/1.0/_/_/revisions"), msgNotFound},
		{"ghost pkg latest", http.MethodGet,
			v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/packages/"+fixturePID(1)+"/latest"), msgNotFound},
		{"ghost revision files list", http.MethodGet,
			v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(9)+"/files"), msgNotFound},
		{"v1 ghost snapshot", http.MethodGet, v1("cn-local", "conans/nope/1.0/_/_"), msgNotFound},
		{"v2 ghost file GET", http.MethodGet,
			v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/files/missing.py"), msgNotFound},
		{
			"v2 ghost revision delete — bare quoted path",
			http.MethodDelete, v2("cn-local", "hello/1.0/myuser/stable/revisions/"+fixtureRev(9)),
			"Couldn't find path 'myuser/hello/1.0/stable/" + fixtureRev(9) + "'",
		},
		{
			"v2 ghost recipe delete — bare quoted path",
			http.MethodDelete, v2("cn-local", "nope/1.0/_/_"),
			"Couldn't find path '_/nope/1.0/_'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			var body string
			var hdr http.Header
			switch tc.method {
			case http.MethodDelete:
				code, body, hdr = s.delete(tc.path)
			default:
				code, body, hdr = s.get(tc.path)
			}
			if code != http.StatusNotFound {
				t.Fatalf("%s = (%d, %q), want 404", tc.path, code, body)
			}
			if !strings.Contains(body, `"errors"`) || !strings.Contains(body, tc.wantMsg) {
				t.Errorf("%s body = %q, want the envelope carrying %q", tc.path, body, tc.wantMsg)
			}
			if strings.HasSuffix(tc.wantMsg, "'") && strings.Contains(body, "/'") {
				t.Errorf("%s body = %q, want the quoted path without a trailing slash", tc.path, body)
			}
			if ct := hdr.Get("Content-Type"); ct != "application/json" {
				t.Errorf("%s content-type = %q, want application/json", tc.path, ct)
			}
		})
	}
}

// TestV1GhostDeleteIdempotent pins D3: the v1 whole-tree delete of a ghost
// coordinate answers the idempotent 200 empty (the v2 plane keeps its 404
// envelope — the reference itself splits the two planes).
func TestV1GhostDeleteIdempotent(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)

	for i := 0; i < 2; i++ {
		code, body, _ := s.delete(v1("cn-local", "conans/hello/1.0/myuser/stable"))
		if code != http.StatusOK || body != "" {
			t.Errorf("v1 ghost delete #%d = (%d, %q), want the idempotent (200, empty)", i+1, code, body)
		}
	}
	// The v2 twin stays the 404 envelope.
	code, body, _ := s.delete(v2("cn-local", "hello/1.0/myuser/stable"))
	if code != http.StatusNotFound || !strings.Contains(body, "Couldn't find path 'myuser/hello/1.0/stable'") {
		t.Errorf("v2 ghost delete = (%d, %q), want the 404 Couldn't-find-path envelope", code, body)
	}
}

// TestRefSearchQuerySyntax pins D4: the v2 packageId-metadata search
// rejects the three evidenced query spellings with the reference's
// plain-text 400 under a json content type; the v1 twin and every other
// spelling keep the as-built face.
func TestRefSearchQuerySyntax(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cn-local", repo.TypeLocal)
	r := ref{name: "hello", version: "1.0", user: "myuser", channel: "stable"}
	rev := fixtureRev(2)
	s.putRecipeFile("cn-local", r, rev, "conanfile.py", []byte("x"))
	s.putPkgFile("cn-local", r, rev, fixturePID(1), fixtureRev(4), "conan_package.tgz", []byte("tgz"))

	cases := []struct {
		name string
		q    string
		want string
	}{
		{"empty q", "", "Unexpected query syntax: "},
		{"lone star", "*", "Unexpected query syntax: *"},
		{
			"wire ref re-serialized",
			"hello/1.0@myuser/stable",
			"Unexpected query syntax: hello, version=1.0, user=myuser, channel=stable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := v2("cn-local", "hello/1.0/myuser/stable/search") + "?q=" + url.QueryEscape(tc.q)
			code, body, hdr := s.get(path)
			if code != http.StatusBadRequest || body != tc.want {
				t.Errorf("search?q=%q = (%d, %q), want (400, %q)", tc.q, code, body, tc.want)
			}
			// The reference labels this plain text application/json
			// (v2-09a-pkgmeta.hdr) — Content-Type is a kept semantic.
			if ct := hdr.Get("Content-Type"); ct != "application/json" {
				t.Errorf("content-type = %q, want application/json", ct)
			}
		})
	}

	// An unexplored spelling falls through to the as-built face, and the
	// v1 twin never runs the gate.
	for _, path := range []string{
		v2("cn-local", "hello/1.0/myuser/stable/search?q=os%3DMacos"),
		v2("cn-local", "hello/1.0/myuser/stable/revisions/"+rev+"/search?q=os%3DMacos"),
		v1("cn-local", "conans/hello/1.0/myuser/stable/search"),
	} {
		if code, body, _ := s.get(path); code != http.StatusOK || !strings.Contains(body, fixturePID(1)) {
			t.Errorf("%s = (%d, %s), want the as-built 200 pid map", path, code, body)
		}
	}
}
