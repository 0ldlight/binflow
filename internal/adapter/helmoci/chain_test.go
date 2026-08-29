package helmoci

// The HelmOCI protocol chain over the real /v2 plane (AC1's server-side
// half — the helm-client legs run in the ticket's live validation): one
// helm-shaped push (config + chart layer + provenance layer, the three
// Helm media types) through the blob upload and manifest PUT endpoints,
// then the pull-side round trip — manifest by tag and by digest
// byte-identical, the config/layer blobs back, the tags listing, the
// catalog row, and the content plane's uniform 404.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The Helm OCI media-type chain (helm.md section 8.1 — the three types the
// acceptance asserts verbatim).
const (
	mtOCIManifest = "application/vnd.oci.image.manifest.v1+json"
	mtHelmConfig  = "application/vnd.cncf.helm.config.v1+json"
	mtHelmChart   = "application/vnd.cncf.helm.chart.content.v1.tar+gzip"
	mtHelmProv    = "application/vnd.cncf.helm.chart.provenance.v1.prov"
)

// pushBlob lands one blob through the single-request monolithic upload
// (POST /v2/<name>/blobs/uploads/?digest=sha256:<hex> with the body).
func (s *stack) pushBlob(t *testing.T, name string, body []byte, wantErrCode string) (int, string) {
	t.Helper()
	path := fmt.Sprintf("/v2/helmoci-local/%s/blobs/uploads/?digest=sha256:%s", name, sha256HexOf(body))
	status, respBody, _ := s.post(path, body, nil)
	if wantErrCode == "" && status != http.StatusCreated {
		t.Fatalf("blob push %s status = %d, want 201 (body %s)", path, status, respBody)
	}
	return status, respBody
}

// helmManifestBody renders one helm-shaped OCI image manifest: the config
// descriptor over cfg, then the chart layer and (when prov is non-nil) the
// provenance layer — exactly the descriptor chain helm push produces.
func helmManifestBody(cfg, chart, prov []byte) []byte {
	layers := []string{
		fmt.Sprintf(`{"mediaType":%q,"digest":"sha256:%s","size":%d}`, mtHelmChart, sha256HexOf(chart), len(chart)),
	}
	if prov != nil {
		layers = append(layers,
			fmt.Sprintf(`{"mediaType":%q,"digest":"sha256:%s","size":%d}`, mtHelmProv, sha256HexOf(prov), len(prov)))
	}
	return []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":%q,"digest":"sha256:%s","size":%d},"layers":[%s]}`,
		mtOCIManifest, mtHelmConfig, sha256HexOf(cfg), len(cfg), strings.Join(layers, ",")))
}

// TestHelmOCIPushPullChain: the full server-side round trip on a seeded
// LOCAL helmoci repository — the media-type chain lands verbatim and every
// pull-side read answers with the stored bytes.
func TestHelmOCIPushPullChain(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helmoci-local", repo.TypeLocal, Protocol)

	cfg := []byte(`{"name":"mychart","version":"0.1.0","description":"T-342 chain fixture","apiVersion":"v2"}`)
	chart := []byte("the chart tgz bytes (a real gzip body is the client leg's concern)")
	prov := []byte("-----BEGIN PGP SIGNATURE-----\nfixture provenance body\n-----END PGP SIGNATURE-----\n")

	// Blob uploads: config, chart content, provenance (helm pushes the
	// .prov as a plain second layer).
	s.pushBlob(t, "mychart", cfg, "")
	s.pushBlob(t, "mychart", chart, "")
	s.pushBlob(t, "mychart", prov, "")

	manifest := helmManifestBody(cfg, chart, prov)
	dgst := sha256HexOf(manifest)
	putStatus, putBody, putHdr := s.put("/v2/helmoci-local/mychart/manifests/0.1.0", manifest,
		map[string]string{"Content-Type": mtOCIManifest})
	if putStatus != http.StatusCreated {
		t.Fatalf("manifest PUT status = %d, want 201 (body %s)", putStatus, putBody)
	}
	if got := putHdr.Get("Docker-Content-Digest"); got != "sha256:"+dgst {
		t.Errorf("PUT Docker-Content-Digest = %q, want sha256:%s", got, dgst)
	}

	// Manifest GET by tag: byte-identical body, the stored media type, the
	// digest header — the pass-through contract (helm pull re-hashes the
	// body against the digest; any re-serialization would break it).
	getStatus, getBody, getHdr := s.get("/v2/helmoci-local/mychart/manifests/0.1.0")
	if getStatus != http.StatusOK {
		t.Fatalf("manifest GET status = %d, want 200 (body %s)", getStatus, getBody)
	}
	if getBody != string(manifest) {
		t.Errorf("manifest GET body drifts: got %d bytes, want the stored %d", len(getBody), len(manifest))
	}
	if got := getHdr.Get("Content-Type"); got != mtOCIManifest {
		t.Errorf("manifest GET Content-Type = %q, want %q", got, mtOCIManifest)
	}
	if got := getHdr.Get("Docker-Content-Digest"); got != "sha256:"+dgst {
		t.Errorf("manifest GET Docker-Content-Digest = %q, want sha256:%s", got, dgst)
	}
	// The three Helm media types ride the manifest body verbatim.
	for _, mt := range []string{mtHelmConfig, mtHelmChart, mtHelmProv} {
		if !strings.Contains(getBody, mt) {
			t.Errorf("manifest body does not carry the Helm media type %q", mt)
		}
	}

	// Accept negotiation: helm's pull sends the OCI manifest type only.
	status, body, _ := s.do(http.MethodGet, "/v2/helmoci-local/mychart/manifests/0.1.0", "", "", nil,
		map[string]string{"Accept": mtOCIManifest})
	if status != http.StatusOK {
		t.Fatalf("manifest GET (Accept %s) status = %d, want 200 (body %s)", mtOCIManifest, status, body)
	}

	// Manifest GET by digest (helm pull oci://...@sha256:<digest>).
	status, body, _ = s.get("/v2/helmoci-local/mychart/manifests/sha256:" + dgst)
	if status != http.StatusOK || body != string(manifest) {
		t.Errorf("manifest GET by digest = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(manifest))
	}

	// Blob reads: config and both layers round trip byte-identical.
	for _, blob := range [][]byte{cfg, chart, prov} {
		status, body, _ = s.get("/v2/helmoci-local/mychart/blobs/sha256:" + sha256HexOf(blob))
		if status != http.StatusOK || body != string(blob) {
			t.Errorf("blob GET = (%d, %d bytes), want (200, %d bytes)", status, len(body), len(blob))
		}
	}

	// tags/list: the chart version is the tag (helm's strict binding).
	status, body, _ = s.get("/v2/helmoci-local/mychart/tags/list")
	if status != http.StatusOK || !strings.Contains(body, `"0.1.0"`) {
		t.Errorf("tags/list = (%d, %s), want 200 naming 0.1.0", status, body)
	}

	// The catalog carries the helmoci repository's image (the family
	// shares the plane, so it shares the catalog).
	status, body, _ = s.do(http.MethodGet, "/v2/_catalog", adminUser, adminPass, nil, nil)
	if status != http.StatusOK || !strings.Contains(body, "helmoci-local/mychart") {
		t.Errorf("_catalog = (%d, %s), want 200 naming helmoci-local/mychart", status, body)
	}

	// HEAD manifest: the headers-only pull probe.
	status, _, hdr := s.do(http.MethodHead, "/v2/helmoci-local/mychart/manifests/0.1.0", "", "", nil, nil)
	if status != http.StatusOK {
		t.Errorf("manifest HEAD status = %d, want 200", status)
	} else if got := hdr.Get("Content-Type"); got != mtOCIManifest {
		t.Errorf("manifest HEAD Content-Type = %q, want %q", got, mtOCIManifest)
	}
}

// TestHelmOCIContentPlaneIsV2Only: the /binflow content face answers the
// registry family's spec-body 404 — the /v2 plane is the only protocol
// face (the docker package type's posture, byte-identical).
func TestHelmOCIContentPlaneIsV2Only(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helmoci-local", repo.TypeLocal, Protocol)

	status, body, _ := s.get("/binflow/helmoci-local/mychart/manifests/0.1.0")
	if status != http.StatusNotFound {
		t.Fatalf("content-plane GET status = %d, want 404 (body %s)", status, body)
	}
	if !strings.Contains(body, `"errors"`) || !strings.Contains(body, "UNSUPPORTED") {
		t.Errorf("content-plane body %q is not the registry spec 404", body)
	}
	status, body, _ = s.put("/binflow/helmoci-local/plain-file.txt", []byte("x"), nil)
	if status != http.StatusNotFound {
		t.Errorf("content-plane PUT status = %d, want 404 (body %s)", status, body)
	}
}

// TestHelmOCISecondVersionRepointsTag: a 0.2.0 push repoints the tag and
// keeps 0.1.0 reachable by digest (the tag/version semantics helm's
// no-version pull relies on through tags/list ordering).
func TestHelmOCISecondVersionRepointsTag(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helmoci-local", repo.TypeLocal, Protocol)

	pushVersion := func(version string) []byte {
		t.Helper()
		cfg := []byte(fmt.Sprintf(`{"name":"mychart","version":%q,"apiVersion":"v2"}`, version))
		chart := []byte("chart body " + version)
		s.pushBlob(t, "mychart", cfg, "")
		s.pushBlob(t, "mychart", chart, "")
		manifest := helmManifestBody(cfg, chart, nil)
		status, body, _ := s.put("/v2/helmoci-local/mychart/manifests/"+version, manifest,
			map[string]string{"Content-Type": mtOCIManifest})
		if status != http.StatusCreated {
			t.Fatalf("manifest PUT %s status = %d (body %s)", version, status, body)
		}
		return manifest
	}
	v1 := pushVersion("0.1.0")
	v2 := pushVersion("0.2.0")

	status, body, _ := s.get("/v2/helmoci-local/mychart/tags/list")
	if status != http.StatusOK || !strings.Contains(body, `"0.1.0"`) || !strings.Contains(body, `"0.2.0"`) {
		t.Errorf("tags/list = (%d, %s), want both versions", status, body)
	}
	// A digest reference still reaches the OLD manifest.
	status, body, _ = s.get("/v2/helmoci-local/mychart/manifests/sha256:" + sha256HexOf(v1))
	if status != http.StatusOK || body != string(v1) || body == string(v2) {
		t.Errorf("0.1.0 by digest = (%d, %d bytes), want the old manifest", status, len(body))
	}
}
