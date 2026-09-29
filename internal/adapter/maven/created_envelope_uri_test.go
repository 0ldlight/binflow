package maven

// T-561 / BIN-42 (known-divergence
// deploy-created-envelope-uri-missing-context-prefix): the deploy 201
// created-envelope's uri/downloadUri carry the /binflow context prefix
// (A-face shape: /artifactory-prefixed self-reference), and the uri
// resolves back to the deployed bytes through /binflow-prefixed routing —
// the resolvability anchor the L034 differential arm re-checks against the
// A side. Covers both writeCreated callers: the byte-deploy chain and the
// checksum-deploy chain.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
)

// prefixRouter mimics httpapi's content-plane mount (router.go's
// withStrippedPrefix, minimized to the routing fact under test): a request
// under /binflow/ reaches the adapter with the prefix stripped and an admin
// principal boxed on the adapter seam; anything else is a plain 404.
func prefixRouter(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.EscapedPath()
		if !strings.HasPrefix(raw, "/binflow/") {
			http.NotFound(w, r)
			return
		}
		u := *r.URL
		u.Path = strings.TrimPrefix(r.URL.Path, "/binflow")
		u.RawPath = strings.TrimPrefix(raw, "/binflow")
		r2 := r.Clone(r.Context())
		r2.URL = &u
		h.ServeHTTP(w, r2.WithContext(adapter.WithPrincipal(
			r2.Context(), &auth.Principal{Name: "admin", Admin: true})))
	})
}

// TestDeployCreatedEnvelopeURIContextPrefix: every byte-deploy face (jar
// artifact, pom, client maven-metadata.xml) renders the 201 envelope's
// uri/downloadUri as scheme://host/binflow/<repo>/<path>, and a GET of the
// uri through the same prefixed routing answers 200.
func TestDeployCreatedEnvelopeURIContextPrefix(t *testing.T) {
	hs := newHarness(t)
	srv := httptest.NewServer(prefixRouter(hs.h))
	t.Cleanup(srv.Close)
	client := srv.Client()

	cases := []struct {
		name string
		path string // content-plane path (after /binflow)
		body []byte
		hdr  map[string]string
		// literalBody: the GET serves the deployed bytes verbatim; the
		// metadata document is server-recalculated, so only 200 is pinned.
		literalBody bool
	}{
		{
			name: "jar artifact deploy",
			path: jarPath,
			body: jarBytes,
			hdr: func() map[string]string {
				s1, _, s256 := digests(jarBytes)
				return map[string]string{"X-Checksum-Sha1": s1, "X-Checksum-Sha256": s256}
			}(),
			literalBody: true,
		},
		{
			name:        "pom deploy",
			path:        pomPath,
			body:        []byte("<project/>"),
			literalBody: true,
		},
		{
			name: "maven-metadata.xml deploy",
			path: "/maven-local/com/acme/demo-app/maven-metadata.xml",
			body: []byte("<metadata><versioning/></metadata>"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPut, srv.URL+"/binflow"+tc.path,
				bytes.NewReader(tc.body))
			if err != nil {
				t.Fatalf("build PUT: %v", err)
			}
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("PUT: %v", err)
			}
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT = %d (%s)", resp.StatusCode, drain(t, resp))
			}
			var env fileInfo
			if err := json.Unmarshal(drain(t, resp), &env); err != nil {
				t.Fatalf("envelope decode: %v", err)
			}
			want := srv.URL + "/binflow" + tc.path
			if env.URI != want {
				t.Errorf("uri = %q, want %q", env.URI, want)
			}
			if env.DownloadURI != want {
				t.Errorf("downloadUri = %q, want %q", env.DownloadURI, want)
			}

			// Resolvability anchor: the envelope uri GETs back through the
			// /binflow routing (never a bare-root 404).
			got, err := client.Get(env.URI)
			if err != nil {
				t.Fatalf("GET envelope uri: %v", err)
			}
			b := drain(t, got)
			if got.StatusCode != http.StatusOK {
				t.Fatalf("GET envelope uri = %d (%s)", got.StatusCode, b)
			}
			if tc.literalBody && !bytes.Equal(b, tc.body) {
				t.Errorf("GET body = %q, want the deployed %q", b, tc.body)
			}
		})
	}
}

// TestChecksumDeployCreatedEnvelopeURIContextPrefix: the zero-transfer
// checksum-deploy chain (writeCreated's second caller) renders the same
// prefixed envelope, and its uri resolves to the referenced blob's bytes.
func TestChecksumDeployCreatedEnvelopeURIContextPrefix(t *testing.T) {
	hs := newHarness(t)
	if resp := hs.deployJar("maven-local", "com/acme/demo-app/1.0.0/demo-app-1.0.0.jar", jarBytes); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed deploy: %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, _, _ := digests(jarBytes)

	srv := httptest.NewServer(prefixRouter(hs.h))
	t.Cleanup(srv.Close)

	newGAV := "/maven-local/com/acme/demo-app/2.0.0/demo-app-2.0.0.jar"
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/binflow"+newGAV, nil)
	if err != nil {
		t.Fatalf("build PUT: %v", err)
	}
	req.Header.Set("X-Checksum-Deploy", "true")
	req.Header.Set("X-Checksum-Sha1", s1)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("checksum deploy: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("checksum deploy = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	var env fileInfo
	if err := json.Unmarshal(drain(t, resp), &env); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	if want := srv.URL + "/binflow" + newGAV; env.URI != want || env.DownloadURI != want {
		t.Fatalf("envelope = %q/%q, want %q", env.URI, env.DownloadURI, want)
	}

	got, err := srv.Client().Get(env.URI)
	if err != nil {
		t.Fatalf("GET envelope uri: %v", err)
	}
	b := drain(t, got)
	if got.StatusCode != http.StatusOK || !bytes.Equal(b, jarBytes) {
		t.Fatalf("GET envelope uri = %d (%q), want 200 with the blob bytes", got.StatusCode, b)
	}
}
