package docker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The T-39 manifest-domain suite (package-internal): the real handler over
// the real storage engine plus a manifest-aware fake service for the
// service-error branches, and full-table validation-chain coverage.

// manifestFixture is one coherent pushable image: config + layer blobs
// pre-landed through the blob plane, and a schema2 manifest referencing
// them.
type manifestFixture struct {
	configBytes  []byte
	layerBytes   []byte
	manifest     []byte
	manifestDgst string
}

// newManifestFixture builds the canonical happy-path manifest.
func newManifestFixture(t *testing.T, bh *blobHarness) *manifestFixture {
	t.Helper()
	f := &manifestFixture{
		configBytes: []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`),
		layerBytes:  []byte("layer-bytes-of-the-fixture"),
	}
	pushBlob := func(content []byte) string {
		dgst := "sha256:" + sha256Hex(content)
		resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
			bytes.NewReader(content), nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("fixture blob push status = %d body=%s", resp.StatusCode, body)
		}
		return dgst
	}
	cfg := pushBlob(f.configBytes)
	layer := pushBlob(f.layerBytes)
	f.manifest = []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":%d}]}`,
		cfg, len(f.configBytes), layer, len(f.layerBytes)))
	f.manifestDgst = "sha256:" + sha256Hex(f.manifest)
	return f
}

// putManifest issues one manifest PUT with the given reference and type.
func (bh *blobHarness) putManifest(t *testing.T, reference string, body []byte, contentType string) *http.Response {
	t.Helper()
	hdr := map[string]string{"Content-Type": contentType}
	return bh.serve(http.MethodPut, "/v2/team1/app/manifests/"+reference, bytes.NewReader(body), hdr)
}

// getManifest issues one manifest GET/HEAD with optional Accept values.
func (bh *blobHarness) getManifest(t *testing.T, method, reference string, accept ...string) *http.Response {
	t.Helper()
	hdr := map[string]string{}
	if len(accept) > 0 {
		hdr["Accept"] = strings.Join(accept, ", ")
	}
	return bh.serve(method, "/v2/team1/app/manifests/"+reference, nil, hdr)
}

// ---- AC1: PUT happy paths ----

// TestManifestPutByTagAndByDigest (FR-9-AC1, DE-08): a tag push and a
// by-digest push both answer 201 + Location + Docker-Content-Digest, the
// node lands at the manifest layout path with the Content-Type as its mime
// (FR-7-AC4), and the manifest index row exists with the right media type.
func TestManifestPutByTagAndByDigest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reference string
	}{
		{name: "by tag", reference: "v1"},
		{name: "by digest", reference: "DIGEST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bh := newBlobHarness(t)
			f := newManifestFixture(t, bh)
			ref := tc.reference
			if ref == "DIGEST" {
				ref = f.manifestDgst
			}
			resp := bh.putManifest(t, ref, f.manifest, mediaTypeDockerManifest)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("PUT status = %d body=%s", resp.StatusCode, body)
			}
			if got := resp.Header.Get("Docker-Content-Digest"); got != f.manifestDgst {
				t.Fatalf("Docker-Content-Digest = %q want %q", got, f.manifestDgst)
			}
			wantLoc := "/v2/team1/app/manifests/" + f.manifestDgst
			if got := resp.Header.Get("Location"); got != wantLoc {
				t.Fatalf("Location = %q want %q", got, wantLoc)
			}
			if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
				t.Fatalf("api-version header = %q", got)
			}

			// The body landed through the blob plane (manifests ARE blobs:
			// the node at <image>/blobs/<hex> is the reference-integrity
			// anchor) and the manifest row carries the media type.
			hex := strings.TrimPrefix(f.manifestDgst, "sha256:")
			bh.svc.mu.Lock()
			content, ok := bh.svc.blobs["team1/app/blobs/"+hex]
			var row *metadata.DockerManifest
			for _, m := range bh.svc.manifests {
				if m.RepoKey == "team1" && m.Image == "app" && m.Digest == hex {
					row = m
				}
			}
			bh.svc.mu.Unlock()
			if !ok {
				t.Fatalf("no blob node at app/blobs/%s", hex)
			}
			if !bytes.Equal(content, f.manifest) {
				t.Fatal("stored manifest body differs from the pushed bytes")
			}
			if row == nil {
				t.Fatal("no manifest index row landed")
			}
			if row.MediaType != mediaTypeDockerManifest {
				t.Fatalf("manifest row media type = %q want %q", row.MediaType, mediaTypeDockerManifest)
			}
			if row.Size != int64(len(f.manifest)) {
				t.Fatalf("manifest row size = %d want %d", row.Size, len(f.manifest))
			}
		})
	}
}

// TestManifestPutEmptyLayerExemption: a manifest whose ONLY layer is the
// canonical empty layer pushes without that blob ever being uploaded
// (docker push's scratch-image path; T-38's synthesis constant).
func TestManifestPutEmptyLayerExemption(t *testing.T) {
	bh := newBlobHarness(t)
	cfg := "sha256:" + sha256Hex([]byte(`{"config":true}`))
	resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+cfg,
		strings.NewReader(`{"config":true}`), nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("config push status = %d", resp.StatusCode)
	}
	body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",`+
			`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":14},`+
			`"layers":[{"mediaType":"application/vnd.oci.image.layer.v1.tar+gzip","digest":"sha256:%s","size":32}]}`,
		cfg, emptyLayerDigestHex))
	resp = bh.putManifest(t, "scratch", body, mediaTypeOCIManifest)
	out := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("empty-layer manifest status = %d body=%s", resp.StatusCode, out)
	}
}

// TestManifestPutSubjectHeader ([OCI] 1.1, docker-registry.md section 3
// step 10): a manifest carrying a subject answers OCI-Subject; one without
// the field does not.
func TestManifestPutSubjectHeader(t *testing.T) {
	bh := newBlobHarness(t)
	cfg := "sha256:" + sha256Hex([]byte("cfg-subject"))
	for _, c := range []string{"cfg-subject"} {
		resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest=sha256:"+sha256Hex([]byte(c)),
			strings.NewReader(c), nil)
		readBody(t, resp)
	}
	subject := "sha256:" + strings.Repeat("7b", 32)
	body := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json",`+
			`"config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"%s","size":11},`+
			`"layers":[],"subject":{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"%s","size":123}}`,
		cfg, subject))
	resp := bh.putManifest(t, "sbom", body, mediaTypeOCIManifest)
	out := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("subject manifest status = %d body=%s", resp.StatusCode, out)
	}
	if got := resp.Header.Get("OCI-Subject"); got != subject {
		t.Fatalf("OCI-Subject = %q want %q", got, subject)
	}

	// Without a subject: no header at all.
	f := newManifestFixture(t, bh)
	resp = bh.putManifest(t, "plain", f.manifest, mediaTypeDockerManifest)
	readBody(t, resp)
	if got := resp.Header.Get("OCI-Subject"); got != "" {
		t.Fatalf("OCI-Subject present without a subject field: %q", got)
	}
}

// ---- AC2: GET/HEAD, Accept negotiation, tag semantics ----

// TestManifestGetRoundTrip (FR-9-AC1/AC2, DE-09): the body returned by
// by-digest and by-tag reads is bit-for-bit the pushed bytes, with
// Docker-Content-Digest and the stored Content-Type; HEAD carries the
// headers with no body.
func TestManifestGetRoundTrip(t *testing.T) {
	bh := newBlobHarness(t)
	f := newManifestFixture(t, bh)
	resp := bh.putManifest(t, "v1", f.manifest, mediaTypeDockerManifest)
	readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("push status = %d", resp.StatusCode)
	}

	for _, ref := range []string{"v1", f.manifestDgst} {
		resp := bh.getManifest(t, http.MethodGet, ref, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", ref, resp.StatusCode, body)
		}
		if !bytes.Equal(body, f.manifest) {
			t.Fatalf("GET %s body differs from the pushed bytes (%d vs %d)",
				ref, len(body), len(f.manifest))
		}
		if got := resp.Header.Get("Docker-Content-Digest"); got != f.manifestDgst {
			t.Fatalf("GET %s Docker-Content-Digest = %q", ref, got)
		}
		if got := resp.Header.Get("Content-Type"); got != mediaTypeDockerManifest {
			t.Fatalf("GET %s Content-Type = %q", ref, got)
		}
		if got := resp.Header.Get("Docker-Distribution-Api-Version"); got != "registry/2.0" {
			t.Fatalf("GET %s api-version = %q", ref, got)
		}

		hresp := bh.getManifest(t, http.MethodHead, ref, mediaTypeDockerManifest)
		hbody := readBody(t, hresp)
		if hresp.StatusCode != http.StatusOK {
			t.Fatalf("HEAD %s status = %d", ref, hresp.StatusCode)
		}
		if len(hbody) != 0 {
			t.Fatalf("HEAD %s carried a body: %d bytes", ref, len(hbody))
		}
		if got := hresp.Header.Get("Content-Length"); got != fmt.Sprint(len(f.manifest)) {
			t.Fatalf("HEAD %s Content-Length = %q", ref, got)
		}
		if got := hresp.Header.Get("Docker-Content-Digest"); got != f.manifestDgst {
			t.Fatalf("HEAD %s Docker-Content-Digest = %q", ref, got)
		}
	}
}

// TestManifestTagOverwrite (FR-9-AC2): after a tag repoints, by-tag serves
// the NEW manifest while the OLD one stays reachable by its digest.
func TestManifestTagOverwrite(t *testing.T) {
	bh := newBlobHarness(t)
	f1 := newManifestFixture(t, bh)
	resp := bh.putManifest(t, "v1", f1.manifest, mediaTypeDockerManifest)
	readBody(t, resp)

	// A second manifest: different layer content -> different digest.
	layer2 := []byte("second-layer-content-for-overwrite")
	d2 := "sha256:" + sha256Hex(layer2)
	resp = bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+d2,
		bytes.NewReader(layer2), nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("layer2 push status = %d", resp.StatusCode)
	}
	cfgDgst := "sha256:" + sha256Hex(f1.configBytes)
	m2 := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",`+
			`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
			`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":%d}]}`,
		cfgDgst, len(f1.configBytes), d2, len(layer2)))
	resp = bh.putManifest(t, "v1", m2, mediaTypeDockerManifest)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("overwrite status = %d body=%s", resp.StatusCode, body)
	}

	// by-tag = new; both digests still resolvable.
	resp = bh.getManifest(t, http.MethodGet, "v1", mediaTypeDockerManifest)
	got := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, m2) {
		t.Fatalf("by-tag after overwrite = %d (new body: %v)", resp.StatusCode, bytes.Equal(got, m2))
	}
	resp = bh.getManifest(t, http.MethodGet, f1.manifestDgst, mediaTypeDockerManifest)
	got = readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, f1.manifest) {
		t.Fatalf("old manifest by digest = %d (old body: %v)", resp.StatusCode, bytes.Equal(got, f1.manifest))
	}
}

// TestManifestAcceptNegotiation (DE-09 calibration): an Accept set that
// excludes the stored type and carries no wildcard answers 404
// MANIFEST_UNKNOWN — never a schema2->1 conversion; wildcards and absent
// Accept admit everything.
func TestManifestAcceptNegotiation(t *testing.T) {
	bh := newBlobHarness(t)
	f := newManifestFixture(t, bh)
	resp := bh.putManifest(t, "v1", f.manifest, mediaTypeDockerManifest)
	readBody(t, resp)

	tests := []struct {
		name   string
		accept []string
		status int
	}{
		{name: "exact type", accept: []string{mediaTypeDockerManifest}, status: http.StatusOK},
		{name: "several types incl stored", accept: []string{mediaTypeDockerSchema1, mediaTypeDockerManifest}, status: http.StatusOK},
		{name: "star-star", accept: []string{"*/*"}, status: http.StatusOK},
		{name: "type wildcard", accept: []string{"application/*"}, status: http.StatusOK},
		{name: "absent Accept", accept: nil, status: http.StatusOK},
		{name: "disjoint set", accept: []string{mediaTypeOCIManifest, mediaTypeOCIIndex}, status: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := bh.getManifest(t, http.MethodGet, "v1", tc.accept...)
			body := readBody(t, resp)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d want %d body=%s", resp.StatusCode, tc.status, body)
			}
			if tc.status == http.StatusNotFound {
				assertSpecCode(t, body, "MANIFEST_UNKNOWN")
			}
		})
	}
}

// ---- AC3: multi-arch index ----

// TestManifestIndexPushPull (FR-9-AC5, D08b equivalent — hand-built index
// in place of buildx): two per-arch manifests land by digest, the index
// referencing them pushes by tag, the pull round-trips the exact index
// bytes, and every child stays resolvable by digest.
func TestManifestIndexPushPull(t *testing.T) {
	bh := newBlobHarness(t)
	pushBlob := func(content string) string {
		dgst := "sha256:" + sha256Hex([]byte(content))
		resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest="+dgst,
			strings.NewReader(content), nil)
		readBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("blob %s push status = %d", content, resp.StatusCode)
		}
		return dgst
	}

	// Two per-arch child manifests, each with its own config (the arch is
	// in the config bytes) and the shared layer.
	layer := pushBlob("shared-multi-arch-layer")
	var children []string
	for _, arch := range []string{"amd64", "arm64"} {
		cfg := pushBlob(fmt.Sprintf(`{"architecture":%q,"os":"linux"}`, arch))
		child := []byte(fmt.Sprintf(
			`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",`+
				`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"%s","size":%d},`+
				`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"%s","size":22}]}`,
			cfg, len(fmt.Sprintf(`{"architecture":%q,"os":"linux"}`, arch)), layer))
		childDgst := "sha256:" + sha256Hex(child)
		resp := bh.putManifest(t, childDgst, child, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("child %s push status = %d body=%s", arch, resp.StatusCode, body)
		}
		children = append(children, childDgst)
	}

	index := []byte(fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.list.v2+json",`+
			`"manifests":[{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","digest":"%s","platform":{"architecture":"amd64","os":"linux"}},`+
			`{"mediaType":"application/vnd.docker.distribution.manifest.v2+json","digest":"%s","platform":{"architecture":"arm64","os":"linux"}}]}`,
		children[0], children[1]))
	resp := bh.putManifest(t, "multi", index, mediaTypeDockerList)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("index push status = %d body=%s", resp.StatusCode, body)
	}
	indexDgst := resp.Header.Get("Docker-Content-Digest")

	// The index round-trips exactly (crane manifest semantics).
	resp = bh.getManifest(t, http.MethodGet, "multi", mediaTypeDockerList)
	got := readBody(t, resp)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, index) {
		t.Fatalf("index GET status = %d identical=%v", resp.StatusCode, bytes.Equal(got, index))
	}
	if hdr := resp.Header.Get("Content-Type"); hdr != mediaTypeDockerList {
		t.Fatalf("index Content-Type = %q", hdr)
	}
	if hdr := resp.Header.Get("Docker-Content-Digest"); hdr != indexDgst {
		t.Fatalf("index Docker-Content-Digest = %q want %q", hdr, indexDgst)
	}

	// Every child is resolvable by digest through the manifest plane
	// (crane's fallback path).
	for i, child := range children {
		resp := bh.getManifest(t, http.MethodGet, child, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("child %d by digest status = %d body=%s", i, resp.StatusCode, body)
		}
	}
}

// ---- AC4: DELETE semantics ----

// TestManifestDeleteByDigest (FR-9-AC6, DE-10): 202, the manifest goes 404
// by digest AND by tag afterwards, and the referenced blobs survive (GC
// owns the bytes).
func TestManifestDeleteByDigest(t *testing.T) {
	bh := newBlobHarness(t)
	f := newManifestFixture(t, bh)
	resp := bh.putManifest(t, "v1", f.manifest, mediaTypeDockerManifest)
	readBody(t, resp)

	resp = bh.serve(http.MethodDelete, "/v2/team1/app/manifests/"+f.manifestDgst, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("DELETE status = %d body=%s", resp.StatusCode, body)
	}

	for _, ref := range []string{f.manifestDgst, "v1"} {
		resp := bh.getManifest(t, http.MethodGet, ref, mediaTypeDockerManifest)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s after delete = %d", ref, resp.StatusCode)
		}
		assertSpecCode(t, body, "MANIFEST_UNKNOWN")
	}

	// The referenced layer is untouched (FR-9-AC6).
	layerDgst := "sha256:" + sha256Hex(f.layerBytes)
	resp = bh.serve(http.MethodGet, "/v2/team1/app/blobs/"+layerDgst, nil, nil)
	readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("referenced layer after manifest delete = %d", resp.StatusCode)
	}
}

// TestManifestDeleteByTagRejected (FR-9-AC7, DE-10): a tag reference
// answers 405 UNSUPPORTED and the tag keeps serving.
func TestManifestDeleteByTagRejected(t *testing.T) {
	bh := newBlobHarness(t)
	f := newManifestFixture(t, bh)
	resp := bh.putManifest(t, "v1", f.manifest, mediaTypeDockerManifest)
	readBody(t, resp)

	resp = bh.serve(http.MethodDelete, "/v2/team1/app/manifests/v1", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE by tag status = %d body=%s", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "UNSUPPORTED")
	if allow := resp.Header.Get("Allow"); allow == "" {
		t.Fatal("405 carries no Allow header")
	}

	// The tag still serves the manifest.
	resp = bh.getManifest(t, http.MethodGet, "v1", mediaTypeDockerManifest)
	readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tag after rejected delete = %d", resp.StatusCode)
	}
}

// TestManifestDeleteUnknownDigest: deleting a digest nothing ever pushed
// answers 404 MANIFEST_UNKNOWN (the spec's idempotent miss).
func TestManifestDeleteUnknownDigest(t *testing.T) {
	bh := newBlobHarness(t)
	resp := bh.serve(http.MethodDelete, "/v2/team1/app/manifests/sha256:"+strings.Repeat("ab", 32), nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	assertSpecCode(t, body, "MANIFEST_UNKNOWN")
}

// ---- AC5: the validation chain, table-driven over every failure branch ----

// TestManifestValidationChain (table-driven): every rejected PUT shape and
// the exact code it must answer — bad JSON, missing fields, by-digest
// mismatch, missing blob, over-limit body, schema1, illegal reference,
// bad descriptor digest.
func TestManifestValidationChain(t *testing.T) {
	newCase := func(t *testing.T) (*blobHarness, *manifestFixture) {
		bh := newBlobHarness(t)
		return bh, newManifestFixture(t, bh)
	}
	valid := func(f *manifestFixture) []byte { return f.manifest }

	tests := []struct {
		name        string
		reference   string
		contentType string
		build       func(t *testing.T, bh *blobHarness, f *manifestFixture) []byte
		wantCode    string
	}{
		{
			name:        "not JSON at all",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte("this is not json")
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "JSON but not a manifest object",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`[1,2,3]`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "missing schemaVersion",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				var m map[string]any
				if err := json.Unmarshal(valid(f), &m); err != nil {
					t.Fatal(err)
				}
				delete(m, "schemaVersion")
				return mustJSON(t, m)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "missing config",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				var m map[string]any
				if err := json.Unmarshal(valid(f), &m); err != nil {
					t.Fatal(err)
				}
				delete(m, "config")
				return mustJSON(t, m)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "config without digest",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"application/vnd.docker.container.image.v1+json","size":2},"layers":[]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "layer without digest",
			reference:   "v1",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:` +
					sha256Hex(f.configBytes) + `","size":1},"layers":[{"mediaType":"x","size":1}]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "index without manifests",
			reference:   "v1",
			contentType: mediaTypeDockerList,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerList + `"}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "index child without digest",
			reference:   "v1",
			contentType: mediaTypeDockerList,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerList +
					`","manifests":[{"mediaType":"` + mediaTypeDockerManifest + `","size":1}]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "schema1 refused (interim ruling)",
			reference:   "v1",
			contentType: mediaTypeDockerSchema1,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":1,"name":"x","tag":"v1","fsLayers":[]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "schema1 signed refused",
			reference:   "v1",
			contentType: mediaTypeDockerSchema1Signed,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":1,"name":"x","tag":"v1","fsLayers":[],"signatures":[]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "missing Content-Type",
			reference:   "v1",
			contentType: "",
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return valid(f)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "pass-through unknown type parsed by body shape (accepted)",
			reference:   "v1",
			contentType: "application/vnd.cncf.helm.config.v1+json",
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				// A helm-style manifest: config/layers present, unknown
				// mediaTypes inside (FR-12 depends on this landing).
				return []byte(`{"schemaVersion":2,"mediaType":"application/vnd.cncf.helm.config.v1+json",` +
					`"config":{"mediaType":"application/vnd.cncf.helm.config.v1+json","digest":"sha256:` +
					sha256Hex(f.configBytes) + `","size":1},"layers":[]}`)
			},
			wantCode: "", // 201
		},
		{
			name:        "by-digest reference mismatch (FR-9-AC4)",
			reference:   "sha256:" + strings.Repeat("0", 64),
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return valid(f)
			},
			wantCode: "DIGEST_INVALID",
		},
		{
			name:        "by-digest reference malformed",
			reference:   "sha256:tooshort",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return valid(f)
			},
			wantCode: "DIGEST_INVALID",
		},
		{
			// Percent-encoded space: "1 bad tag" on the wire.
			name:        "reference neither digest nor tag",
			reference:   "1%20bad%20tag",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return valid(f)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "missing config blob (D13b / FR-9-AC3)",
			reference:   "broken",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:` +
					strings.Repeat("1", 64) + `","size":2},"layers":[]}`)
			},
			wantCode: "MANIFEST_BLOB_UNKNOWN",
		},
		{
			name:        "missing layer blob",
			reference:   "broken",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:` +
					sha256Hex(f.configBytes) + `","size":2},` +
					`"layers":[{"mediaType":"x","digest":"sha256:` + strings.Repeat("2", 64) + `","size":2}]}`)
			},
			wantCode: "MANIFEST_BLOB_UNKNOWN",
		},
		{
			name:        "index child manifest missing (lazy gate)",
			reference:   "broken",
			contentType: mediaTypeDockerList,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerList +
					`","manifests":[{"mediaType":"` + mediaTypeDockerManifest + `","digest":"sha256:` +
					strings.Repeat("3", 64) + `","size":1}]}`)
			},
			wantCode: "MANIFEST_BLOB_UNKNOWN",
		},
		{
			name:        "descriptor digest malformed",
			reference:   "broken",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, _ *manifestFixture) []byte {
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"x","digest":"notadigest","size":1},"layers":[]}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
		{
			name:        "body over the 4MB limit (section 6.5 ruling 3)",
			reference:   "big",
			contentType: mediaTypeDockerManifest,
			build: func(_ *testing.T, _ *blobHarness, f *manifestFixture) []byte {
				// A structurally valid manifest padded past the limit with a
				// JSON string field (the parse would succeed — the limit
				// must refuse before parsing matters).
				pad := strings.Repeat("p", manifestMaxBytes)
				return []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
					`","config":{"mediaType":"x","digest":"sha256:` + sha256Hex(f.configBytes) +
					`","size":1},"layers":[],"pad":"` + pad + `"}`)
			},
			wantCode: "MANIFEST_INVALID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bh, f := newCase(t)
			body := tc.build(t, bh, f)
			hdr := map[string]string{"Content-Type": tc.contentType}
			resp := bh.serve(http.MethodPut, "/v2/team1/app/manifests/"+tc.reference,
				bytes.NewReader(body), hdr)
			out := readBody(t, resp)
			if tc.wantCode == "" {
				if resp.StatusCode != http.StatusCreated {
					t.Fatalf("status = %d body=%s", resp.StatusCode, out)
				}
				return
			}
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s", resp.StatusCode, out)
			}
			assertSpecCode(t, out, tc.wantCode)
		})
	}
}

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// ---- service-error branches ----

// manifestFakeService asserts the adapter's mapping of repo.Service
// failures on the manifest plane: every sentinel renders its protocol
// status, and store-shaped failures render 500 without leaking internals
// into success codes.
func TestManifestServiceErrorMapping(t *testing.T) {
	bh := newBlobHarness(t)

	// A minimal manifest whose descriptors are all satisfiable: a config
	// blob pushed through the blob plane plus the exempt empty layer. The
	// reference gate passes, so the INJECTED service error is the first
	// failure the response carries.
	cfgContent := []byte("svc-err-config")
	resp := bh.serve(http.MethodPost, "/v2/team1/app/blobs/uploads/?digest=sha256:"+sha256Hex(cfgContent),
		bytes.NewReader(cfgContent), nil)
	readBody(t, resp)
	minimal := []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeDockerManifest +
		`","config":{"mediaType":"x","digest":"sha256:` + sha256Hex(cfgContent) + `","size":15},` +
		`"layers":[{"mediaType":"x","digest":"sha256:` + emptyLayerDigestHex + `","size":32}]}`)

	tests := []struct {
		name     string
		inject   func(svc *fakeService)
		method   string
		path     string
		body     []byte
		wantStat int
		wantCode string
	}{
		{
			name:     "publish denied",
			inject:   func(svc *fakeService) { svc.putManifestErr = fmt.Errorf("write: %w", repo.ErrForbidden) },
			method:   http.MethodPut,
			path:     "/v2/team1/app/manifests/v1",
			body:     minimal,
			wantStat: http.StatusForbidden,
			wantCode: "DENIED",
		},
		{
			name:     "publish requires auth",
			inject:   func(svc *fakeService) { svc.putManifestErr = fmt.Errorf("write: %w", repo.ErrUnauthorized) },
			method:   http.MethodPut,
			path:     "/v2/team1/app/manifests/v1",
			body:     minimal,
			wantStat: http.StatusUnauthorized,
			wantCode: "UNAUTHORIZED",
		},
		{
			name:     "resolve unknown manifest",
			inject:   func(svc *fakeService) { svc.resolveErr = fmt.Errorf("m: %w", repo.ErrManifestNotFound) },
			method:   http.MethodGet,
			path:     "/v2/team1/app/manifests/sha256:" + strings.Repeat("f", 64),
			wantStat: http.StatusNotFound,
			wantCode: "MANIFEST_UNKNOWN",
		},
		{
			name:     "unknown tag",
			inject:   func(svc *fakeService) { svc.resolveErr = fmt.Errorf("t: %w", repo.ErrTagNotFound) },
			method:   http.MethodGet,
			path:     "/v2/team1/app/manifests/nope",
			wantStat: http.StatusNotFound,
			wantCode: "MANIFEST_UNKNOWN",
		},
		{
			name:     "read denied",
			inject:   func(svc *fakeService) { svc.resolveErr = fmt.Errorf("r: %w", repo.ErrForbidden) },
			method:   http.MethodGet,
			path:     "/v2/team1/app/manifests/sha256:" + strings.Repeat("f", 64),
			wantStat: http.StatusForbidden,
			wantCode: "DENIED",
		},
		{
			name:     "delete unknown",
			inject:   func(svc *fakeService) { svc.deleteErr = fmt.Errorf("d: %w", repo.ErrManifestNotFound) },
			method:   http.MethodDelete,
			path:     "/v2/team1/app/manifests/sha256:" + strings.Repeat("f", 64),
			wantStat: http.StatusNotFound,
			wantCode: "MANIFEST_UNKNOWN",
		},
		{
			name:     "delete denied",
			inject:   func(svc *fakeService) { svc.deleteErr = fmt.Errorf("d: %w", repo.ErrForbidden) },
			method:   http.MethodDelete,
			path:     "/v2/team1/app/manifests/sha256:" + strings.Repeat("f", 64),
			wantStat: http.StatusForbidden,
			wantCode: "DENIED",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.inject(bh.svc)
			var rdr io.Reader
			if tc.body != nil {
				rdr = bytes.NewReader(tc.body)
			}
			resp := bh.serve(tc.method, tc.path, rdr,
				map[string]string{"Content-Type": mediaTypeDockerManifest})
			body := readBody(t, resp)
			if resp.StatusCode != tc.wantStat {
				t.Fatalf("status = %d want %d body=%s", resp.StatusCode, tc.wantStat, body)
			}
			assertSpecCode(t, body, tc.wantCode)
		})
	}
}

// TestManifestMethodMatrix: unsupported verbs answer 405 + Allow.
func TestManifestMethodMatrix(t *testing.T) {
	bh := newBlobHarness(t)
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		resp := bh.serve(method, "/v2/team1/app/manifests/v1", nil, nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d body=%s", method, resp.StatusCode, body)
		}
		assertSpecCode(t, body, "UNSUPPORTED")
		if allow := resp.Header.Get("Allow"); !strings.Contains(allow, "GET") {
			t.Fatalf("%s Allow = %q", method, allow)
		}
	}
}

// TestManifestRouteShapes: bare "manifests" without a reference answers the
// spec 404 (the name parser already rejects it; the guard is total).
func TestManifestRouteShapes(t *testing.T) {
	bh := newBlobHarness(t)
	resp := bh.serve(http.MethodGet, "/v2/team1/app/manifests", nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
}

// ---- pure-function tables ----

// TestParseManifestTable: the structural parser's classification matrix.
func TestParseManifestTable(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     bool
		wantRefs    int
	}{
		{name: "docker schema2", contentType: mediaTypeDockerManifest,
			body:     `{"schemaVersion":2,"config":{"digest":"sha256:` + strings.Repeat("a", 64) + `"},"layers":[{"digest":"sha256:` + strings.Repeat("b", 64) + `"}]}`,
			wantRefs: 2},
		{name: "oci manifest", contentType: mediaTypeOCIManifest,
			body:     `{"schemaVersion":2,"config":{"digest":"sha256:` + strings.Repeat("a", 64) + `"},"layers":[]}`,
			wantRefs: 1},
		{name: "docker list", contentType: mediaTypeDockerList,
			body:     `{"schemaVersion":2,"manifests":[{"digest":"sha256:` + strings.Repeat("c", 64) + `"}]}`,
			wantRefs: 1},
		{name: "oci index", contentType: mediaTypeOCIIndex,
			body:     `{"schemaVersion":2,"manifests":[]}`,
			wantRefs: 0},
		{name: "schema1 refused", contentType: mediaTypeDockerSchema1,
			body: `{"schemaVersion":1}`, wantErr: true},
		{name: "no content type", contentType: "",
			body: `{"schemaVersion":2}`, wantErr: true},
		{name: "unknown type with image shape", contentType: "application/vnd.example+json",
			body:     `{"schemaVersion":2,"config":{"digest":"sha256:` + strings.Repeat("a", 64) + `"},"layers":[]}`,
			wantRefs: 1},
		{name: "unknown type with neither shape", contentType: "application/vnd.example+json",
			body: `{"schemaVersion":2,"data":[1]}`, wantErr: true},
		{name: "index type with image body", contentType: mediaTypeDockerList,
			body: `{"schemaVersion":2,"config":{"digest":"sha256:xxx"}}`, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseManifest(tc.contentType, []byte(tc.body))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseManifest accepted %q/%q", tc.contentType, tc.body)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseManifest: %v", err)
			}
			if len(parsed.refs) != tc.wantRefs {
				t.Fatalf("refs = %d want %d", len(parsed.refs), tc.wantRefs)
			}
		})
	}
}

// TestAcceptAllowsTable: the negotiation matrix.
func TestAcceptAllowsTable(t *testing.T) {
	const stored = mediaTypeOCIManifest
	tests := []struct {
		name   string
		accept []string
		want   bool
	}{
		{name: "absent", accept: nil, want: true},
		{name: "exact", accept: []string{stored}, want: true},
		{name: "list with exact", accept: []string{mediaTypeDockerManifest + ", " + stored}, want: true},
		{name: "star-star", accept: []string{"*/*"}, want: true},
		{name: "type wildcard", accept: []string{"application/*"}, want: true},
		{name: "q parameters stripped", accept: []string{stored + ";q=0.5"}, want: true},
		{name: "disjoint", accept: []string{mediaTypeDockerManifest}, want: false},
		{name: "wrong wildcard type", accept: []string{"text/*"}, want: false},
		{name: "empty tokens only", accept: []string{""}, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := acceptAllows(tc.accept, stored); got != tc.want {
				t.Fatalf("acceptAllows(%v) = %v want %v", tc.accept, got, tc.want)
			}
		})
	}
}

// TestValidateManifestTagTable: the tag charset.
func TestValidateManifestTagTable(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"v1", true}, {"latest", true}, {"V1.0-beta_2", true}, {"_under", true},
		{"", false}, {".dot", false}, {"-dash", false},
		{"1 bad tag", false}, {"tag#hash", false},
		{strings.Repeat("a", 128), true}, {strings.Repeat("a", 129), false},
	}
	for _, tc := range tests {
		err := validateManifestTag(tc.tag)
		if (err == nil) != tc.want {
			t.Errorf("validateManifestTag(%q) err = %v want ok=%v", tc.tag, err, tc.want)
		}
	}
}

// TestManifestNodePathLayout: the manifest layout spelling matches the
// service layer's (the mirror is load-bearing — an adapter/service drift
// would strand every manifest node).
func TestManifestNodePathLayout(t *testing.T) {
	if got := manifestNodePath("acme/app", "aa"); got != "acme/app/manifests/aa" {
		t.Fatalf("manifestNodePath = %q", got)
	}
	if got := manifestURL("team1", "acme/app", "aa"); got != "/v2/team1/acme/app/manifests/sha256:aa" {
		t.Fatalf("manifestURL = %q", got)
	}
}
