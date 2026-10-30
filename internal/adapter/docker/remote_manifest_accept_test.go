package docker

// T-L043-C-b: the pull-through manifest fetch must offer the upstream the
// FIXED canonical Accept set (canonicalRemoteManifestAccept), never the
// client's verbatim set. A client-negotiated answer (schema1/prettyjws —
// the L043-wave3 live D2/D3 shape against a conversion-serving upstream)
// lands the negotiated representation into the digest-addressed cache node,
// and every later client then receives it regardless of its own Accept:
// the cache holds ONE representation per digest. The legs below pin:
//
//   - the upstream conversation always carries the canonical set (schema2,
//     the list, the OCI pair — schema1 never offered);
//   - a prettyjws-only client's cold fetch cannot poison the cache (the
//     landing's own Accept gate refuses the negotiated canonical type —
//     the registered residual divergence, nothing lands);
//   - a schema2 client afterwards reads the canonical schema2 bytes from
//     the cache (MISS then frozen HIT, phase-independent, no prettyjws
//     ever) on both the direct remote face and the virtual member walk.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/remote"
)

// schema1SignedBody is the prettyjws-shaped representation a
// conversion-serving upstream hands to an Accept that admits it (the
// distribution-family server posture — reverse docker-registry.md section
// 4 #5: a stored schema2 converts to schema1 signed on such a serve).
var schema1SignedBody = []byte(`{"schemaVersion":1,"name":"up/myapp","tag":"l043","architecture":"amd64","fsLayers":[],"history":[],"signatures":[{"protected":"l043-c-b-probe"}]}`)

// upstreamAcceptAdmits is the fake upstream's strict negotiation: a token
// list parsed from ALL Accept headers, wildcards honored, absent = admit
// (the same default posture the real plane keeps).
func upstreamAcceptAdmits(acceptHeaders []string, mediaType string) bool {
	sawToken := false
	for _, hv := range acceptHeaders {
		for _, tok := range strings.Split(hv, ",") {
			tok = strings.TrimSpace(strings.SplitN(tok, ";", 2)[0])
			if tok == "" {
				continue
			}
			sawToken = true
			if tok == mediaType || tok == "*/*" {
				return true
			}
		}
	}
	return !sawToken
}

// TestRemoteManifestFetchCanonicalAccept: cold fetches offer the canonical
// Accept set upstream; the cache holds the canonical schema2 representation;
// later clients read it phase-independently on both faces.
func TestRemoteManifestFetchCanonicalAccept(t *testing.T) {
	cfgDgst := "sha256:" + sha256Hex([]byte("{}"))
	schema2Body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+mediaTypeDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":2},`+
			`"layers":[]}`, cfgDgst))

	var mu sync.Mutex
	var manifestAccepts []string // one entry per manifest request the upstream answered
	var hits atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.HasPrefix(r.URL.Path, "/v2/up/myapp/manifests/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		manifestAccepts = append(manifestAccepts, strings.Join(r.Header.Values("Accept"), ","))
		mu.Unlock()
		switch {
		case upstreamAcceptAdmits(r.Header.Values("Accept"), mediaTypeDockerManifest):
			w.Header().Set("Content-Type", mediaTypeDockerManifest)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(schema2Body)
		case upstreamAcceptAdmits(r.Header.Values("Accept"), mediaTypeDockerSchema1Signed):
			w.Header().Set("Content-Type", mediaTypeDockerSchema1Signed)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(schema1SignedBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(up.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "d-remote", up.URL)
	down.seedDockerVirtualRepo(t, "d-virt", "d-remote")

	// assertCanonicalSaw: every upstream manifest conversation so far rode
	// the canonical set — each supported type present, schema1 absent.
	assertCanonicalSaw := func(t *testing.T, wantCount int) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if len(manifestAccepts) != wantCount {
			t.Fatalf("upstream manifest conversations = %d, want %d (seen: %q)",
				len(manifestAccepts), wantCount, manifestAccepts)
		}
		for i, got := range manifestAccepts {
			for _, mt := range canonicalRemoteManifestAccept {
				if !strings.Contains(got, mt) {
					t.Fatalf("upstream conversation %d Accept %q: missing canonical type %s", i, got, mt)
				}
			}
			if strings.Contains(got, mediaTypeDockerSchema1Signed) || strings.Contains(got, mediaTypeDockerSchema1) {
				t.Fatalf("upstream conversation %d Accept %q: the client's schema1 set leaked upstream", i, got)
			}
		}
	}

	// ---- leg A: prettyjws-only client, direct face, cold fetch ----
	// The upstream serves the canonical type (it was never offered
	// prettyjws); the landing's client-Accept gate refuses it — 404, and
	// NOTHING lands (the registered residual divergence). The pollution
	// vector is dead: the cache stays empty, not prettyjws.
	code, body, _ := down.get("/v2/d-remote/up/myapp/manifests/l043a",
		map[string]string{"Accept": mediaTypeDockerSchema1Signed})
	if code != http.StatusNotFound {
		t.Fatalf("prettyjws-only cold fetch = (%d, %s), want 404 (stored type outside the client Accept)", code, body)
	}
	assertCanonicalSaw(t, 1)

	// ---- leg B: schema2 client (docker-daemon shape: schema2 + prettyjws
	// together), direct face ----
	daemonAccept := map[string]string{"Accept": strings.Join([]string{
		mediaTypeDockerManifest, mediaTypeDockerSchema1Signed,
	}, ", ")}
	code, body, hdr := down.get("/v2/d-remote/up/myapp/manifests/l043b", daemonAccept)
	if code != http.StatusOK {
		t.Fatalf("schema2 cold fetch = (%d, %s), want 200", code, body)
	}
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Fatalf("cold fetch Content-Type = %q, want %q", got, mediaTypeDockerManifest)
	}
	if got := hdr.Get(remote.HdrCacheState); got != remote.CacheMiss {
		t.Fatalf("cold fetch cache state = %q, want MISS", got)
	}
	if string(body) != string(schema2Body) {
		t.Fatalf("cold fetch body drifted from the canonical schema2 bytes")
	}
	assertCanonicalSaw(t, 2)

	// ---- leg B repeat: the cache answers, and it answers SCHEMA2 ----
	// (the L043 D3 shape: the second client phase must not read the first
	// phase's negotiated representation — here there never was one).
	code, body, hdr = down.get("/v2/d-remote/up/myapp/manifests/l043b", daemonAccept)
	if code != http.StatusOK {
		t.Fatalf("repeat fetch = (%d, %s), want 200", code, body)
	}
	if got := hdr.Get(remote.HdrCacheState); got != remote.CacheHit {
		t.Fatalf("repeat fetch cache state = %q, want HIT (the cached copy answers)", got)
	}
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Fatalf("cached copy Content-Type = %q, want %q (the cache holds schema2, not prettyjws)", got, mediaTypeDockerManifest)
	}
	if string(body) != string(schema2Body) {
		t.Fatalf("cached copy body drifted from the canonical schema2 bytes")
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("upstream contacts after the repeat = %d, want 2 (the HIT must not re-open the conversation)", got)
	}
	assertCanonicalSaw(t, 2)

	// ---- leg C: the virtual member walk, same shape ----
	code, body, _ = down.get("/v2/d-virt/up/myapp/manifests/l043c",
		map[string]string{"Accept": mediaTypeDockerSchema1Signed})
	if code != http.StatusNotFound {
		t.Fatalf("virtual prettyjws-only cold fetch = (%d, %s), want 404", code, body)
	}
	assertCanonicalSaw(t, 3)

	code, body, hdr = down.get("/v2/d-virt/up/myapp/manifests/l043c", daemonAccept)
	if code != http.StatusOK {
		t.Fatalf("virtual schema2 cold fetch = (%d, %s), want 200", code, body)
	}
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Fatalf("virtual cold fetch Content-Type = %q, want %q", got, mediaTypeDockerManifest)
	}
	if string(body) != string(schema2Body) {
		t.Fatalf("virtual cold fetch body drifted from the canonical schema2 bytes")
	}

	code, body, hdr = down.get("/v2/d-virt/up/myapp/manifests/l043c", daemonAccept)
	if code != http.StatusOK {
		t.Fatalf("virtual repeat fetch = (%d, %s), want 200", code, body)
	}
	if got := hdr.Get(remote.HdrCacheState); got != remote.CacheHit {
		t.Fatalf("virtual repeat fetch cache state = %q, want HIT", got)
	}
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Fatalf("virtual cached copy Content-Type = %q, want %q", got, mediaTypeDockerManifest)
	}
	if string(body) != string(schema2Body) {
		t.Fatalf("virtual cached copy body drifted from the canonical schema2 bytes")
	}
	if got := hits.Load(); got != 4 {
		t.Fatalf("upstream contacts at the end = %d, want 4 (one cold conversation per leg)", got)
	}
}
