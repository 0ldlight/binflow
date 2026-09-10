package helmoci

// L002-1 (C10, ADR-0047 DigestChainGate): the helmoci REMOTE plane rides
// the docker /v2 stack, so the marker gate covers the family by
// construction — this file pins that on the real helmoci stack: a
// cold digest no manifest chain named is refused locally with zero
// upstream roundtrips, and the chart's own blobs (in-chain after the
// manifest lands) proxy upstream as before. The docker package's
// remote_chain_gate_test.go owns the full shape matrix (refs-missing,
// eviction, negative-cache boundary, virtual walk).

import (
	"net/http"
	"strings"
	"testing"
)

// TestRemoteChainGateHelmOCI: the family coverage — the gate refuses
// out-of-chain digests on a helmoci remote row without touching the
// upstream (the pre-fix digest blind proxy gone), and the in-chain chart
// blobs keep proxying once a manifest chain has landed.
func TestRemoteChainGateHelmOCI(t *testing.T) {
	down, _, _, hits, manifest, cfg, chart := newRemoteFixture(t)

	// No manifest pulled yet: every digest is out-of-chain. A random one
	// answers the E6-2 unfound shape locally; the upstream counter never
	// moves (zero roundtrips — the marker-gate posture, E4-2).
	random := "sha256:" + sha256HexOf([]byte("helmoci-out-of-chain"))
	before := hits.Load()
	status, body, _ := down.get("/v2/helmoci-remote/helmoci-local/mychart/blobs/" + random)
	if status != http.StatusNotFound {
		t.Fatalf("out-of-chain blob status = %d, want the local 404 (body %s)", status, body)
	}
	if !strings.Contains(body, `"code":"BLOB_UNKNOWN"`) || !strings.Contains(body, `"blobSum":"`+random+`"`) {
		t.Errorf("out-of-chain body %q is not the E6-2 blobSum shape", body)
	}
	if hits.Load() != before {
		t.Fatalf("the shut gate contacted the upstream: %d -> %d hits", before, hits.Load())
	}

	// The chain lands (manifest pull); the chart's config+layer blobs —
	// uncached — proxy upstream (in-chain, E4-1).
	status, body, _ = down.do(http.MethodGet, "/v2/helmoci-remote/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK || body != string(manifest) {
		t.Fatalf("manifest pull = (%d, %d bytes), want the upstream copy", status, len(body))
	}
	for _, blob := range [][]byte{cfg, chart} {
		status, body, hdr := down.get("/v2/helmoci-remote/helmoci-local/mychart/blobs/sha256:" + sha256HexOf(blob))
		if status != http.StatusOK || body != string(blob) {
			t.Fatalf("in-chain blob GET = (%d, %d bytes), want the proxied copy", status, len(body))
		}
		if got := hdr.Get("X-BinFlow-Cache"); got != "MISS" {
			t.Errorf("in-chain blob X-BinFlow-Cache = %q, want MISS (the upstream fetch)", got)
		}
	}
}
