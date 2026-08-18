package docker

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestParseV2Name: the ADR-0010 clause 3 split — first segment = repo key,
// remainder = image name, tail = endpoint route — with the NFR-S11
// traversal defenses judged on the DECODED form.
func TestParseV2Name(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		repoKey string
		image   string
		tail    string
		errKind string // "", "shape" (404), or "bad" (400)
	}{
		{name: "two-segment name", path: "/v2/team1/app/manifests/latest",
			repoKey: "team1", image: "app", tail: "manifests/latest"},
		{name: "nested image", path: "/v2/team1/acme/app/manifests/sha256:abc",
			repoKey: "team1", image: "acme/app", tail: "manifests/sha256:abc"},
		{name: "three-deep nested image", path: "/v2/dev/group/team/app/blobs/sha256:ff",
			repoKey: "dev", image: "group/team/app", tail: "blobs/sha256:ff"},
		{name: "tags list", path: "/v2/team1/app/tags/list",
			repoKey: "team1", image: "app", tail: "tags/list"},
		{name: "uploads route", path: "/v2/team1/app/blobs/uploads/",
			repoKey: "team1", image: "app", tail: "blobs/uploads/"},
		{name: "referrers", path: "/v2/team1/app/referrers/sha256:aa",
			repoKey: "team1", image: "app", tail: "referrers/sha256:aa"},
		{name: "image named manifests (tail greedy after one segment)", path: "/v2/team1/manifests/manifests/latest",
			repoKey: "team1", image: "manifests", tail: "manifests/latest"},

		{name: "single-segment name", path: "/v2/ubuntu", errKind: "shape"},
		{name: "single-segment with route-looking tail", path: "/v2/manifests/latest", errKind: "shape"},
		{name: "empty name", path: "/v2/", errKind: "shape"},
		{name: "name with no route tail", path: "/v2/team1/app", errKind: "shape"},

		{name: "dot-dot segment", path: "/v2/team1/../etc/passwd", errKind: "bad"},
		{name: "encoded dot-dot", path: "/v2/team1/%2e%2e/etc", errKind: "bad"},
		{name: "dot segment", path: "/v2/team1/./app/manifests/latest", errKind: "bad"},
		{name: "double slash", path: "/v2/team1//app/manifests/latest", errKind: "bad"},
		{name: "malformed percent-encoding", path: "/v2/team1/%zz/manifests/latest", errKind: "bad"},

		// Review B2: the repository-key segment is inside the dot-segment
		// defense — these previously fell through to the 404 shape branch.
		{name: "dot-dot in repo key slot (review B2)", path: "/v2/../etc/passwd", errKind: "bad"},
		{name: "dot in repo key slot (review B2)", path: "/v2/./x/manifests/latest", errKind: "bad"},
		{name: "dot-dot repo key with route tail (review B2)", path: "/v2/../binflow/app/manifests/latest", errKind: "bad"},
		{name: "empty repo key then route (review B2)", path: "/v2//app/manifests/latest", errKind: "bad"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ref, err := parseV2Name(tc.path)
			switch tc.errKind {
			case "":
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				if ref.repoKey != tc.repoKey || ref.image != tc.image || ref.tail != tc.tail {
					t.Fatalf("got %q/%q/%q, want %q/%q/%q",
						ref.repoKey, ref.image, ref.tail, tc.repoKey, tc.image, tc.tail)
				}
			case "shape":
				if err == nil || !isNotAName(err) {
					t.Fatalf("err = %v, want a 404-shaped shape rejection", err)
				}
			case "bad":
				if err == nil {
					t.Fatal("parse unexpectedly succeeded")
				}
				if isNotAName(err) {
					t.Fatalf("err = %v, want the 400-shaped ErrBadRequestPath chain", err)
				}
				if !errors.Is(err, adapter.ErrBadRequestPath) {
					t.Fatalf("err = %v, does not wrap adapter.ErrBadRequestPath", err)
				}
			}
		})
	}
}

// TestLayoutImplementsSPI: Layout satisfies the adapter contract, keyed on
// the request's EscapedPath so raw spellings survive verbatim.
func TestLayoutImplementsSPI(t *testing.T) {
	h := New(nil, NewStaticRepoLookup(nil), nil, nil, nil, Options{}, nil)
	r := &http.Request{URL: &url.URL{Path: "/v2/team1/app/manifests/latest", RawPath: ""}}
	key, rel, err := h.Layout(r)
	if err != nil || key != "team1" || rel != "app" {
		t.Fatalf("Layout = %q/%q/%v", key, rel, err)
	}
}

// TestRepoTypesProtocolKeyOnly pins the M2 dispatch contract: docker
// registers its protocol key and does NOT claim repo classes (a "local"
// claim would shadow generic's content dispatch in the httpapi map).
func TestRepoTypesProtocolKeyOnly(t *testing.T) {
	h := New(nil, NewStaticRepoLookup(nil), nil, nil, nil, Options{}, nil)
	if h.Protocol() != "docker" {
		t.Fatalf("Protocol = %q", h.Protocol())
	}
	if len(h.RepoTypes()) != 0 {
		t.Fatalf("RepoTypes = %v, want empty in M2 (see handler.go doc)", h.RepoTypes())
	}
}

// TestSpecErrorEnvelopeShape: the error body is exactly the registry
// schema with the api-version header (the wrong-verb ping needs an
// authenticated principal — unauthenticated pings challenge first since
// D44-1).
func TestSpecErrorEnvelopeShape(t *testing.T) {
	h := New(nil, NewStaticRepoLookup(nil), nil, nil, nil, Options{AnonymousAccess: true}, nil)
	w := &captureWriter{hdr: http.Header{}}
	r := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/v2/"}}
	r = r.WithContext(adapter.WithPrincipal(r.Context(), &Principal{Name: "admin", Admin: true}))
	h.ServeHTTP(w, r)

	if w.status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", w.status)
	}
	if got := w.hdr.Get(HeaderAPIVersion); got != "registry/2.0" {
		t.Fatalf("api-version header = %q", got)
	}
	if !strings.Contains(w.body.String(), `"code":"UNSUPPORTED"`) {
		t.Fatalf("body = %s", w.body.String())
	}
	if !strings.Contains(w.body.String(), `"detail":null`) {
		t.Fatalf("detail must serialize as null: %s", w.body.String())
	}
}

// captureWriter is a minimal ResponseWriter recorder.
type captureWriter struct {
	hdr    http.Header
	status int
	body   strings.Builder
}

func (c *captureWriter) Header() http.Header         { return c.hdr }
func (c *captureWriter) Write(p []byte) (int, error) { return c.body.Write(p) }
func (c *captureWriter) WriteHeader(code int)        { c.status = code }
