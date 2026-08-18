package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-39 manifest-domain integration suite: the real router + middleware
// + adapter + repo.Service + storage + metadata stack (D08/D13b/D13c
// equivalents and the item-info surface).

const (
	ctDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	ctDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
	ctOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	ctOCIIndex       = "application/vnd.oci.image.index.v1+json"
)

// v2SeedManifest pushes one blob and one manifest referencing it through
// the real chain; the manifest body is built from the given layer content.
// Returns (manifestBody, digest).
func v2SeedManifest(t *testing.T, h *harness, name, tag, layer string) ([]byte, string) {
	t.Helper()
	layerDgst := v2SeedBlob(t, h, name, []byte(layer))
	cfgDgst := v2SeedBlob(t, h, name, []byte(`{"architecture":"amd64","os":"linux"}`))
	body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+ctDockerManifest+`",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":35},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":%d}]}`,
		cfgDgst, layerDgst, len(layer)))
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/"+tag, adminUser, adminPass, body,
		map[string]string{"Content-Type": ctDockerManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest push status = %d body=%s", resp.StatusCode, out)
	}
	dgst := resp.Header.Get("Docker-Content-Digest")
	if dgst != "sha256:"+sha256Of(body) {
		t.Fatalf("pushed digest = %q, computed sha256:%s", dgst, sha256Of(body))
	}
	return body, dgst
}

// TestV2ManifestPushPullRoundTrip (D08's manifest core, FR-9-AC1): push by
// tag, read back by digest AND by tag — the body is byte-identical to the
// pushed bytes (cmp), Docker-Content-Digest matches on every response, and
// HEAD carries the headers with no body.
func TestV2ManifestPushPullRoundTrip(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	body, dgst := v2SeedManifest(t, h, "docker-local/acme/app", "v1", "roundtrip-layer")

	for _, ref := range []string{dgst, "v1"} {
		resp := h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+ref,
			adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
		got := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d", ref, resp.StatusCode)
		}
		if got != string(body) {
			t.Fatalf("GET %s body differs from the pushed bytes (%d vs %d)", ref, len(got), len(body))
		}
		if resp.Header.Get("Docker-Content-Digest") != dgst {
			t.Fatalf("GET %s Docker-Content-Digest = %q want %q", ref,
				resp.Header.Get("Docker-Content-Digest"), dgst)
		}
		if resp.Header.Get("Content-Type") != ctDockerManifest {
			t.Fatalf("GET %s Content-Type = %q", ref, resp.Header.Get("Content-Type"))
		}

		hresp := h.do(http.MethodHead, "/v2/docker-local/acme/app/manifests/"+ref,
			adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
		hout := mustGet(t, hresp)
		if hresp.StatusCode != http.StatusOK {
			t.Fatalf("HEAD %s status = %d", ref, hresp.StatusCode)
		}
		if len(hout) != 0 {
			t.Fatalf("HEAD %s carried a body", ref)
		}
		if hresp.Header.Get("Docker-Content-Digest") != dgst {
			t.Fatalf("HEAD %s digest = %q want %q", ref,
				hresp.Header.Get("Docker-Content-Digest"), dgst)
		}
		if hresp.Header.Get("Content-Length") != fmt.Sprint(len(body)) {
			t.Fatalf("HEAD %s length = %q want %d", ref,
				hresp.Header.Get("Content-Length"), len(body))
		}
	}
}

// TestV2ManifestTagOverwriteByDigestStable (FR-9-AC2): repointing a tag
// serves the new manifest by tag while the old digest stays resolvable.
func TestV2ManifestTagOverwriteByDigestStable(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	oldBody, oldDgst := v2SeedManifest(t, h, "docker-local/acme/app", "v1", "old-layer")
	newBody, _ := v2SeedManifest(t, h, "docker-local/acme/app", "v1", "new-layer-different")
	if oldDgst == "" || newBody == nil {
		t.Fatal("seeding failed")
	}

	resp := h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/v1",
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || got != string(newBody) {
		t.Fatalf("by-tag after overwrite = %d identical=%v", resp.StatusCode, got == string(newBody))
	}

	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+oldDgst,
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	got = mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("old digest after overwrite = %d body=%s", resp.StatusCode, got)
	}
	if got != string(oldBody) {
		t.Fatal("old manifest body drifted")
	}
}

// TestV2ManifestAcceptNegotiation (DE-09 calibration): an Accept set
// excluding the stored type answers 404 MANIFEST_UNKNOWN — no schema
// conversion, no fallback.
func TestV2ManifestAcceptNegotiation(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	_, dgst := v2SeedManifest(t, h, "docker-local/acme/app", "v1", "accept-layer")

	resp := h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Accept": ctOCIManifest + ", " + ctOCIIndex})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disjoint Accept status = %d body=%s", resp.StatusCode, body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "MANIFEST_UNKNOWN" {
		t.Fatalf("code = %q want MANIFEST_UNKNOWN", eb.Errors[0].Code)
	}

	// The wildcard still admits it.
	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Accept": "*/*"})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("*/* status = %d", resp.StatusCode)
	}
}

// TestV2ManifestMissingBlob (D13b, FR-9-AC3): a manifest referencing a
// never-uploaded blob answers 400 MANIFEST_BLOB_UNKNOWN and nothing lands.
func TestV2ManifestMissingBlob(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	body := []byte(`{"schemaVersion":2,"mediaType":"` + ctOCIManifest +
		`","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` +
		strings.Repeat("1", 64) + `","size":2},"layers":[]}`)
	resp := h.do(http.MethodPut, "/v2/docker-local/acme/broken/manifests/m1", adminUser, adminPass, body,
		map[string]string{"Content-Type": ctOCIManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, out)
	}
	eb := decodeSpecError(t, out)
	if eb.Errors[0].Code != "MANIFEST_BLOB_UNKNOWN" {
		t.Fatalf("code = %q want MANIFEST_BLOB_UNKNOWN", eb.Errors[0].Code)
	}

	// Nothing landed: the manifest and the tag are unknown.
	resp = h.do(http.MethodGet, "/v2/docker-local/acme/broken/manifests/m1",
		adminUser, adminPass, nil, map[string]string{"Accept": ctOCIManifest})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("rejected manifest resolvable = %d", resp.StatusCode)
	}
}

// TestV2ManifestDigestMismatch (FR-9-AC4): a by-digest PUT whose reference
// disagrees with the body answers 400 DIGEST_INVALID.
func TestV2ManifestDigestMismatch(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	_, _ = v2SeedManifest(t, h, "docker-local/acme/app", "seed", "digest-mismatch-layer")

	cfgDgst := v2SeedBlob(t, h, "docker-local/acme/app", []byte(`{"a":1}`))
	body := []byte(`{"schemaVersion":2,"mediaType":"` + ctDockerManifest +
		`","config":{"mediaType":"x","digest":"` + cfgDgst + `","size":7},"layers":[]}`)
	resp := h.do(http.MethodPut, "/v2/docker-local/acme/app/manifests/sha256:"+strings.Repeat("0", 64),
		adminUser, adminPass, body, map[string]string{"Content-Type": ctDockerManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, out)
	}
	eb := decodeSpecError(t, out)
	if eb.Errors[0].Code != "DIGEST_INVALID" {
		t.Fatalf("code = %q want DIGEST_INVALID", eb.Errors[0].Code)
	}
}

// TestV2ManifestMultiArchIndex (D08b equivalent, FR-9-AC5 — hand-built
// linux/amd64+arm64 index; buildx itself is T-44's): the index pushes by
// tag, pulls byte-identical, and every child manifest resolves by digest.
func TestV2ManifestMultiArchIndex(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	name := "docker-local/acme/multi"

	pushBlob := func(content string) string {
		t.Helper()
		return v2SeedBlob(t, h, name, []byte(content))
	}
	layer := pushBlob("multi-arch-shared-layer")

	var childDgsts []string
	childBodies := map[string][]byte{}
	for _, arch := range []string{"amd64", "arm64"} {
		cfg := pushBlob(fmt.Sprintf(`{"architecture":%q,"os":"linux"}`, arch))
		child := []byte(fmt.Sprintf(
			`{"schemaVersion":2,"mediaType":"`+ctDockerManifest+`",`+
				`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":35},`+
				`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":23}]}`,
			cfg, layer))
		childDgst := "sha256:" + sha256Of(child)
		resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/"+childDgst, adminUser, adminPass, child,
			map[string]string{"Content-Type": ctDockerManifest})
		out := mustGet(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("child %s push = %d body=%s", arch, resp.StatusCode, out)
		}
		childDgsts = append(childDgsts, childDgst)
		childBodies[childDgst] = child
	}

	index := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"`+ctDockerList+`",`+
			`"manifests":[{"mediaType":"`+ctDockerManifest+`","digest":"%s","platform":{"architecture":"amd64","os":"linux"}},`+
			`{"mediaType":"`+ctDockerManifest+`","digest":"%s","platform":{"architecture":"arm64","os":"linux"}}]}`,
		childDgsts[0], childDgsts[1]))
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/1", adminUser, adminPass, index,
		map[string]string{"Content-Type": ctDockerList})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("index push = %d body=%s", resp.StatusCode, out)
	}
	indexDgst := resp.Header.Get("Docker-Content-Digest")

	// The index round-trips (crane manifest semantics: byte-identical).
	resp = h.do(http.MethodGet, "/v2/"+name+"/manifests/1", adminUser, adminPass, nil,
		map[string]string{"Accept": ctDockerList})
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || got != string(index) {
		t.Fatalf("index GET = %d identical=%v", resp.StatusCode, got == string(index))
	}
	if resp.Header.Get("Content-Type") != ctDockerList {
		t.Fatalf("index Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Docker-Content-Digest") != indexDgst {
		t.Fatalf("index digest = %q want %q", resp.Header.Get("Docker-Content-Digest"), indexDgst)
	}

	// Every child is by-digest resolvable (crane's fallback path).
	for _, cd := range childDgsts {
		resp := h.do(http.MethodGet, "/v2/"+name+"/manifests/"+cd, adminUser, adminPass, nil,
			map[string]string{"Accept": ctDockerManifest})
		got := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("child %s = %d", cd, resp.StatusCode)
		}
		if got != string(childBodies[cd]) {
			t.Fatalf("child %s body drifted", cd)
		}
	}
}

// TestV2ManifestDeleteByDigest (D13c, FR-9-AC6): 202, then 404 by digest
// and by tag, tag rows cascaded, referenced layers untouched.
func TestV2ManifestDeleteByDigest(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	layer := "d13c-referenced-layer"
	_, dgst := v2SeedManifest(t, h, "docker-local/acme/app", "v1", layer)

	resp := h.do(http.MethodDelete, "/v2/docker-local/acme/app/manifests/"+dgst,
		adminUser, adminPass, nil, nil)
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("DELETE status = %d body=%s", resp.StatusCode, out)
	}

	for _, ref := range []string{dgst, "v1"} {
		resp := h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+ref,
			adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s after delete = %d", ref, resp.StatusCode)
		}
		eb := decodeSpecError(t, body)
		if eb.Errors[0].Code != "MANIFEST_UNKNOWN" {
			t.Fatalf("code = %q", eb.Errors[0].Code)
		}
	}

	// The tag row is gone (the cascade, through the service contract).
	if _, err := h.md.Docker().GetTag(t.Context(), "docker-local", "acme/app", "v1"); err == nil {
		t.Fatal("tag row survived the manifest delete")
	}

	// The referenced layer is untouched.
	layerDgst := "sha256:" + sha256Of([]byte(layer))
	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/blobs/"+layerDgst,
		adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("referenced layer after delete = %d", resp.StatusCode)
	}
}

// TestV2ManifestDeleteByTagRejected (FR-9-AC7): 405 UNSUPPORTED, and the
// tag keeps serving.
func TestV2ManifestDeleteByTagRejected(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	_, dgst := v2SeedManifest(t, h, "docker-local/acme/app", "v1", "bytag-layer")

	resp := h.do(http.MethodDelete, "/v2/docker-local/acme/app/manifests/v1",
		adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE by tag = %d body=%s", resp.StatusCode, body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "UNSUPPORTED" {
		t.Fatalf("code = %q want UNSUPPORTED", eb.Errors[0].Code)
	}

	// Both the tag and the manifest survive.
	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/v1",
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tag after rejected delete = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest after rejected delete = %d", resp.StatusCode)
	}
}

// TestV2ManifestItemInfo (FR-7-AC4's docker face): the /api/storage item
// surface lists the manifest and blob nodes of a pushed image with the
// manifest's Content-Type as the node's mime.
func TestV2ManifestItemInfo(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	body, dgst := v2SeedManifest(t, h, "docker-local/app", "v1", "iteminfo-layer")
	hex := strings.TrimPrefix(dgst, "sha256:")

	// The manifest node's FileInfo carries the manifest media type.
	resp := h.do(http.MethodGet, "/binflow/api/storage/docker-local/app/manifests/"+hex,
		adminUser, adminPass, nil, nil)
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest item info = %d body=%s", resp.StatusCode, out)
	}
	var info struct {
		Repo     string `json:"repo"`
		Path     string `json:"path"`
		MimeType string `json:"mimeType"`
		Size     string `json:"size"`
	}
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("item info body %s: %v", out, err)
	}
	if info.MimeType != ctDockerManifest {
		t.Fatalf("manifest node mime = %q want %q", info.MimeType, ctDockerManifest)
	}
	if info.Size != fmt.Sprint(len(body)) {
		t.Fatalf("manifest node size = %q want %d", info.Size, len(body))
	}

	// The repo root's FolderInfo lists the image folder (the docker
	// layout's first level — item info renders the docker tree like any
	// other repository's).
	resp = h.do(http.MethodGet, "/binflow/api/storage/docker-local", adminUser, adminPass, nil, nil)
	out = mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repo root info = %d body=%s", resp.StatusCode, out)
	}
	if !strings.Contains(out, `"/app"`) || !strings.Contains(out, `"folder"`) {
		t.Fatalf("repo root listing misses the image folder: %s", out)
	}

	// The node tree carries the full docker layout: the manifest node, the
	// manifest's own blob node (the body landed through the blob plane —
	// manifests ARE blobs) and every referenced blob.
	nodes, err := h.md.Nodes().ListByPrefix(t.Context(), "docker-local", "")
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	var haveManifest, haveManifestBlob, haveLayerBlob bool
	for _, n := range nodes {
		switch {
		case n.Path == "app/manifests/"+hex:
			haveManifest = true
		case n.Path == "app/blobs/"+hex:
			haveManifestBlob = true
		case strings.HasPrefix(n.Path, "app/blobs/") && strings.HasSuffix(n.Path, sha256Of([]byte("iteminfo-layer"))):
			haveLayerBlob = true
		}
	}
	if !haveManifest || !haveManifestBlob || !haveLayerBlob {
		t.Fatalf("docker layout nodes incomplete (manifest=%v manifestBlob=%v layerBlob=%v): %+v",
			haveManifest, haveManifestBlob, haveLayerBlob, nodes)
	}

	// A blob node answers FileInfo too (octet-stream, the blob plane's
	// stored mime). The prefix carries no trailing slash (likePrefix's
	// subtree arm is prefix+"/%" — a trailing slash would double it).
	blobNodes, err := h.md.Nodes().ListByPrefix(t.Context(), "docker-local", "app/blobs")
	if err != nil || len(blobNodes) != 3 { // manifest body + config + layer
		t.Fatalf("blob nodes = %d err=%v", len(blobNodes), err)
	}
	for _, n := range blobNodes {
		resp := h.do(http.MethodGet, "/binflow/api/storage/docker-local/"+n.Path,
			adminUser, adminPass, nil, nil)
		out := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("blob item info %s = %d", n.Path, resp.StatusCode)
		}
		if !strings.Contains(out, `"application/octet-stream"`) {
			t.Fatalf("blob node mime not octet-stream: %s", out)
		}
	}
}

// TestV2ManifestOCISubjectHeader: a manifest carrying an OCI 1.1 subject
// echoes OCI-Subject on the 201 ([OCI] referrers; the referrers endpoint
// itself stays DE-15's 404).
func TestV2ManifestOCISubjectHeader(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	name := "docker-local/acme/sbom"
	cfg := v2SeedBlob(t, h, name, []byte("sbom-config"))
	subject := "sha256:" + strings.Repeat("7c", 32)

	body := []byte(`{"schemaVersion":2,"mediaType":"` + ctOCIManifest +
		`","config":{"mediaType":"application/vnd.oci.empty.v1+json","digest":"` + cfg + `","size":11},` +
		`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar","digest":"sha256:` +
		"a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4" + `","size":32}],` +
		`"subject":{"mediaType":"` + ctOCIManifest + `","digest":"` + subject + `","size":100}}`)
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/1.0.0", adminUser, adminPass, body,
		map[string]string{"Content-Type": ctOCIManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("sbom push = %d body=%s", resp.StatusCode, out)
	}
	if got := resp.Header.Get("OCI-Subject"); got != subject {
		t.Fatalf("OCI-Subject = %q want %q", got, subject)
	}
}

// TestV2ManifestPassThroughContentType (FR-12's carrier test, R3's
// resolution): an unknown manifest Content-Type with a valid body shape
// lands and serves back the SAME type verbatim — Helm OCI configs ride this.
func TestV2ManifestPassThroughContentType(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	name := "docker-local/charts"
	const helmCT = "application/vnd.cncf.helm.config.v1+json"
	cfg := v2SeedBlob(t, h, name, []byte("helm-chart-config"))

	body := []byte(`{"schemaVersion":2,"mediaType":"` + helmCT +
		`","config":{"mediaType":"` + helmCT + `","digest":"` + cfg + `","size":17},"layers":[]}`)
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/mychart-1.0.0", adminUser, adminPass, body,
		map[string]string{"Content-Type": helmCT})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("helm manifest push = %d body=%s", resp.StatusCode, out)
	}
	dgst := resp.Header.Get("Docker-Content-Digest")

	resp = h.do(http.MethodGet, "/v2/"+name+"/manifests/mychart-1.0.0", adminUser, adminPass, nil,
		map[string]string{"Accept": helmCT})
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("helm manifest GET = %d", resp.StatusCode)
	}
	if got != string(body) {
		t.Fatal("helm manifest body drifted")
	}
	if resp.Header.Get("Content-Type") != helmCT {
		t.Fatalf("served Content-Type = %q want the pass-through value %q",
			resp.Header.Get("Content-Type"), helmCT)
	}
	if resp.Header.Get("Docker-Content-Digest") != dgst {
		t.Fatal("digest drifted on the pass-through read")
	}
}

// TestV2ManifestAnonymousGate: the T-37 gates hold on the manifest routes —
// an anonymous write challenges with the push scope, an anonymous read
// passes the gate (anonymous_access on) and reaches the content answer.
func TestV2ManifestAnonymousGate(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	resp := h.do(http.MethodPut, "/v2/docker-local/app/manifests/v1", "", "", []byte(`{}`), nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous PUT = %d body=%s", resp.StatusCode, body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `repository:docker-local/app:pull,push`) {
		t.Fatalf("challenge = %q", ch)
	}

	// Anonymous read with the gate open: the manifest is unknown, not 401.
	resp = h.do(http.MethodGet, "/v2/docker-local/app/manifests/none", "", "", nil,
		map[string]string{"Accept": ctDockerManifest})
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("anonymous GET = %d body=%s", resp.StatusCode, body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "MANIFEST_UNKNOWN" {
		t.Fatalf("code = %q", eb.Errors[0].Code)
	}
}

// TestV2ManifestEmptyLayerExemption: a manifest whose layer list is exactly
// the canonical empty layer pushes without that blob ever being uploaded.
func TestV2ManifestEmptyLayerExemption(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	name := "docker-local/acme/scratch"
	cfg := v2SeedBlob(t, h, name, []byte(`{"architecture":"arm64","os":"linux"}`))

	body := []byte(`{"schemaVersion":2,"mediaType":"` + ctOCIManifest +
		`","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + cfg + `","size":35},` +
		`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:` +
		"a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4" + `","size":32}]}`)
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/scratch", adminUser, adminPass, body,
		map[string]string{"Content-Type": ctOCIManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("scratch push = %d body=%s", resp.StatusCode, out)
	}
}

// TestV2ManifestLargeBodyRejected (section 6.5 ruling ③): a manifest body
// past 4MB answers 400 MANIFEST_INVALID and nothing lands.
func TestV2ManifestLargeBodyRejected(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	name := "docker-local/acme/big"
	cfg := v2SeedBlob(t, h, name, []byte("big-config"))

	pad := strings.Repeat("q", 4<<20)
	body := []byte(`{"schemaVersion":2,"mediaType":"` + ctDockerManifest +
		`","config":{"mediaType":"x","digest":"` + cfg + `","size":10},"layers":[],"pad":"` + pad + `"}`)
	resp := h.do(http.MethodPut, "/v2/"+name+"/manifests/big", adminUser, adminPass, body,
		map[string]string{"Content-Type": ctDockerManifest})
	out := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("oversized manifest = %d body=%s", resp.StatusCode, out)
	}
	eb := decodeSpecError(t, out)
	if eb.Errors[0].Code != "MANIFEST_INVALID" {
		t.Fatalf("code = %q want MANIFEST_INVALID", eb.Errors[0].Code)
	}

	// Nothing landed.
	if manifests, err := h.md.Docker().ListManifestsByImage(t.Context(), "docker-local", "acme/big"); err != nil || len(manifests) != 0 {
		t.Fatalf("oversized manifest landed: %d err=%v", len(manifests), err)
	}
}

// TestV2ManifestSchema1Rejected (interim ruling): schema1 Content-Types
// answer 400 MANIFEST_INVALID.
func TestV2ManifestSchema1Rejected(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	for _, ct := range []string{
		"application/vnd.docker.distribution.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v1+prettyjws",
	} {
		resp := h.do(http.MethodPut, "/v2/docker-local/app/manifests/v1",
			adminUser, adminPass, []byte(`{"schemaVersion":1,"name":"x","tag":"v1","fsLayers":[]}`),
			map[string]string{"Content-Type": ct})
		out := mustGet(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s status = %d body=%s", ct, resp.StatusCode, out)
		}
		eb := decodeSpecError(t, out)
		if eb.Errors[0].Code != "MANIFEST_INVALID" {
			t.Fatalf("%s code = %q", ct, eb.Errors[0].Code)
		}
	}
}

// TestV2ManifestRefLedger (GC's second fact source, architecture 11.12):
// after a push, docker_refs carries one row per referenced blob and the
// cascade removes them with the manifest.
func TestV2ManifestRefLedger(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	layer := "ref-ledger-layer"
	_, dgst := v2SeedManifest(t, h, "docker-local/app", "v1", layer)
	hex := strings.TrimPrefix(dgst, "sha256:")

	refs, err := h.md.Docker().ListRefsByManifest(t.Context(), "docker-local", "app", hex)
	if err != nil {
		t.Fatalf("ListRefsByManifest: %v", err)
	}
	if len(refs) != 2 { // config + layer
		t.Fatalf("refs = %d want 2", len(refs))
	}

	resp := h.do(http.MethodDelete, "/v2/docker-local/app/manifests/"+dgst, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete = %d", resp.StatusCode)
	}
	refs, err = h.md.Docker().ListRefsByManifest(t.Context(), "docker-local", "app", hex)
	if err != nil {
		t.Fatalf("ListRefsByManifest after delete: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs survived the cascade: %d", len(refs))
	}
}

// TestV2ManifestRestartPersistence: manifest state is metadata + blobs, not
// process state — a second assembly over the same data directory serves
// everything the first one landed.
func TestV2ManifestRestartPersistence(t *testing.T) {
	dataDir := t.TempDir()
	st := openEngineOrFail(t, dataDir)
	h1 := newHarnessWithDataDir(t, dataDir, st)
	seedDockerRepo(t, h1, "docker-local")
	body, dgst := v2SeedManifest(t, h1, "docker-local/acme/app", "v1", "restart-layer")

	if err := st.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	st2 := openEngineOrFail(t, dataDir)
	t.Cleanup(func() { _ = st2.Close() })
	h2 := newHarnessWithDataDir(t, dataDir, st2)

	resp := h2.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/v1",
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-restart by-tag = %d", resp.StatusCode)
	}
	if got != string(body) {
		t.Fatal("post-restart manifest body drifted")
	}
	resp = h2.do(http.MethodGet, "/v2/docker-local/acme/app/manifests/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Accept": ctDockerManifest})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post-restart by-digest = %d", resp.StatusCode)
	}
}

// openEngineOrFail opens a storage engine for the restart tests (the
// second assembly reuses the same data directory).
func openEngineOrFail(t *testing.T, dataDir string) storage.Engine {
	t.Helper()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	return st
}
