package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The upstream SESSION's own legs (T-363, helm.md 8.3): the Bearer dance
// against a mock challenging registry (401 + WWW-Authenticate → token
// exchange with the repository credential → the Bearer retry), the token
// cache's window, the wire path spellings, and the challenge grammar.

// TestParseBearerChallenge pins the challenge grammar: quoted and bare
// values, field order, and the non-Bearer refusal.
func TestParseBearerChallenge(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   *bearerChallenge
	}{
		{name: "the distribution shape", header: `Bearer realm="https://auth.example.com/token",service="registry.example.com",scope="repository:mychart:pull"`,
			want: &bearerChallenge{realm: "https://auth.example.com/token", service: "registry.example.com", scope: "repository:mychart:pull"}},
		{name: "bare values", header: `Bearer realm=https://a/t,service=s`,
			want: &bearerChallenge{realm: "https://a/t", service: "s"}},
		{name: "realm only", header: `Bearer realm="https://a/t"`,
			want: &bearerChallenge{realm: "https://a/t"}},
		{name: "case-insensitive keys", header: `Bearer REALM="https://a/t",SERVICE="s"`,
			want: &bearerChallenge{realm: "https://a/t", service: "s"}},
		{name: "comma inside quotes stays one field", header: `Bearer realm="https://a/t?x=1,2",service="s"`,
			want: &bearerChallenge{realm: "https://a/t?x=1,2", service: "s"}},
		{name: "basic challenge is not ours", header: `Basic realm="x"`, want: nil},
		{name: "empty", header: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBearerChallenge(tt.header)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("parseBearerChallenge(%q) = %+v, want nil", tt.header, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseBearerChallenge(%q) = nil, want %+v", tt.header, tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("parseBearerChallenge(%q) = %+v, want %+v", tt.header, got, tt.want)
			}
		})
	}
}

// TestV2WirePaths pins the upstream wire spellings: the storage layout's
// bare hex becomes the wire's sha256: form; tags ride verbatim.
func TestV2WirePaths(t *testing.T) {
	if got := v2WireManifestPath("mychart", "0.1.0"); got != "mychart/manifests/0.1.0" {
		t.Errorf("manifest tag wire = %q", got)
	}
	hex := sha256HexOf([]byte("x"))
	if got := v2WireManifestPath("team/mychart", "sha256:"+hex); got != "team/mychart/manifests/sha256:"+hex {
		t.Errorf("manifest digest wire = %q", got)
	}
	if got := v2WireBlobPath("mychart", hex); got != "mychart/blobs/sha256:"+hex {
		t.Errorf("blob wire = %q", got)
	}
}

// challengingRegistry is one mock OCI registry that guards everything
// behind a Bearer token: the first unauthenticated request answers 401 +
// the challenge; the token endpoint exchanges the repository credential
// for a bearer; the bearer-authenticated requests serve. It counts the
// 401s, the exchanges and the served reads.
type challengingRegistry struct {
	srv       *httptest.Server
	url       string
	hits401   atomic.Int64
	exchanges atomic.Int64
	served    atomic.Int64
	secret    string
}

// newChallengingRegistry builds the mock. manifest is served at
// /v2/<image>/manifests/<ref> with the given media type and body; the blob
// at /v2/<image>/blobs/sha256:<hex> serves blobBody.
func newChallengingRegistry(t *testing.T, manifestBody []byte, mediaType string, blobHex string, blobBody []byte) *challengingRegistry {
	t.Helper()
	c := &challengingRegistry{secret: "tok-" + strconv.FormatInt(time.Now().UnixNano(), 36)}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		c.exchanges.Add(1)
		if user, pass, ok := r.BasicAuth(); !ok || user != "ci" || pass != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token": c.secret, "expires_in": 300})
	})
	mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+c.secret {
			c.hits401.Add(1)
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm=%q,service="mock-reg"`, c.srv.URL+"/token"))
			writeJSONError(w, http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/manifests/0.1.0") && manifestBody != nil:
			c.served.Add(1)
			w.Header().Set("Content-Type", mediaType)
			w.Header().Set("Docker-Content-Digest", "sha256:"+sha256HexOf(manifestBody))
			_, _ = w.Write(manifestBody)
		case strings.HasSuffix(r.URL.Path, "/blobs/sha256:"+blobHex):
			c.served.Add(1)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(blobBody)
		default:
			writeJSONError(w, http.StatusNotFound)
		}
	})
	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	c.url = c.srv.URL
	return c
}

func writeJSONError(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"errors":[{"code":"UNKNOWN","message":"mock %d"}]}`, status)
}

// newTestSession builds one session pool entry around the mock's facts.
func newTestSession(t *testing.T, url string) *remoteSessionEntry {
	t.Helper()
	pool := &remoteSessions{}
	entry, err := pool.forRepo("helmoci-remote", &repo.RemoteUpstream{
		URL: url + "/v2", Username: "ci", Password: "s3cret",
		AllowPrivateUpstream: true, // the loopback mock (the admin-set NFR-S13 exemption)
		SocketTimeoutMs:      2000, ContentTTLSeconds: 7200, MissedTTLSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("forRepo: %v", err)
	}
	return entry
}

// TestSessionBearerDance: the full chain — 401 with the challenge, the
// exchange (Basic credential at the realm), the Bearer retry, and the
// CACHED token serving the next fetch without a second 401 round trip.
func TestSessionBearerDance(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	up := newChallengingRegistry(t, manifest, "application/vnd.oci.image.manifest.v1+json", sha256HexOf([]byte("blob")), []byte("blob"))
	entry := newTestSession(t, up.url)
	ctx := context.Background()

	res, err := entry.fetchManifest(ctx, "mychart", "0.1.0", []string{"application/vnd.oci.image.manifest.v1+json"})
	if err != nil {
		t.Fatalf("fetchManifest: %v", err)
	}
	if res.status != http.StatusOK || string(res.body) != string(manifest) {
		t.Fatalf("first fetch = (%d, %q), want the manifest", res.status, res.body)
	}
	if up.hits401.Load() != 1 || up.exchanges.Load() != 1 {
		t.Fatalf("dance counts = (401s %d, exchanges %d), want (1, 1)", up.hits401.Load(), up.exchanges.Load())
	}
	if got := entry.token("repository:mychart:pull"); got != up.secret {
		t.Fatalf("cached token = %q, want the exchanged one", got)
	}

	// The cached token: the second fetch rides it without another 401.
	if _, err := entry.fetchManifest(ctx, "mychart", "0.1.0", nil); err != nil {
		t.Fatalf("second fetchManifest: %v", err)
	}
	if up.hits401.Load() != 1 || up.exchanges.Load() != 1 {
		t.Fatalf("cached-token counts = (401s %d, exchanges %d), want (1, 1)", up.hits401.Load(), up.exchanges.Load())
	}

	// The blob arm dances identically and streams.
	stream, err := entry.fetchBlobStream(ctx, "mychart", sha256HexOf([]byte("blob")))
	if err != nil {
		t.Fatalf("fetchBlobStream: %v", err)
	}
	if stream.StatusCode != http.StatusOK || stream.Body == nil {
		t.Fatalf("blob fetch = %d, want the streamed 200", stream.StatusCode)
	}
	_ = stream.Body.Close()
}

// TestSessionDanceRefusedCredential: a token endpoint that refuses the
// repository credential answers errUpstreamAuth (the caller's unfound
// family with the summary — never a naked 5xx).
func TestSessionDanceRefusedCredential(t *testing.T) {
	manifest := []byte(`{}`)
	up := newChallengingRegistry(t, manifest, "application/vnd.oci.image.manifest.v1+json", sha256HexOf([]byte("b")), []byte("b"))
	pool := &remoteSessions{}
	entry, err := pool.forRepo("helmoci-remote", &repo.RemoteUpstream{
		URL: up.url + "/v2", Username: "ci", Password: "WRONG",
		AllowPrivateUpstream: true, SocketTimeoutMs: 2000,
	})
	if err != nil {
		t.Fatalf("forRepo: %v", err)
	}
	_, err = entry.fetchManifest(context.Background(), "mychart", "0.1.0", nil)
	if err == nil {
		t.Fatal("fetchManifest with a refused credential succeeded")
	}
	if !strings.Contains(err.Error(), "token exchange") {
		t.Fatalf("error = %v, want the token-exchange summary", err)
	}
}

// TestSessionPlainUpstream: an upstream that never challenges serves the
// first attempt directly (the repository credential or anonymous).
func TestSessionPlainUpstream(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/mychart/manifests/0.1.0" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		_, _ = w.Write(manifest)
	}))
	t.Cleanup(srv.Close)

	entry := newTestSession(t, srv.URL)
	res, err := entry.fetchManifest(context.Background(), "mychart", "0.1.0", nil)
	if err != nil || res.status != http.StatusOK || string(res.body) != string(manifest) {
		t.Fatalf("plain fetch = (%v, %d, %q)", err, res.status, res.body)
	}
	res2, err := entry.fetchManifest(context.Background(), "mychart", "9.9.9", nil)
	if err != nil || res2.status != http.StatusNotFound {
		t.Fatalf("plain miss = (%v, %d), want the upstream 404 passed through", err, res2.status)
	}
}
