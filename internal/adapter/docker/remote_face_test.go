package docker

// L003-2 (evidence reports/compatibility/L002-contract-replay.md #6/#7/#11
// + the raw captures a_mf.h/a_mfh2.h/a_blob.h/a_blobr.h): the remote read
// face serves manifest/blob copies with the reference's full artifact
// header set — checksum family, Etag(=sha1), Last-Modified(=cache-landing
// time), Accept-Ranges, Content-Disposition/X-Artifactory-Filename,
// X-Artifactory-Origin-Remote-Path (the upstream URL the copy traces to),
// and manifest HEAD's X-Artifactory-Docker-Registry — and the LIST
// endpoints honor the client's n/last window with the reference's own
// invalid-n shape (404 pretty "Not Found") and Link rel="next" exactly
// while a further page exists.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
)

// TestRemoteFaceManifestHeaderSet: the manifest copy face — fresh MISS,
// the cached HIT, and the by-digest HEAD — all carry the full header set
// with the capture's value semantics (Etag = sha1 unquoted, the requested
// reference echoed in Origin-Remote-Path, HEAD's own Docker-Registry
// header naming the ADDRESSED repository).
func TestRemoteFaceManifestHeaderSet(t *testing.T) {
	down, upSrv, _, manifest, _, _ := newDockerRemoteFixture(t)
	manifestHex := sha256Hex(manifest)

	assertFace := func(hdr http.Header, what, wantOrigin string) {
		t.Helper()
		if got := hdr.Get("Etag"); got != sumSha1(manifest) {
			t.Errorf("%s Etag = %q, want the unquoted sha1", what, got)
		}
		if got := hdr.Get(hdrChecksumSha1); got != sumSha1(manifest) {
			t.Errorf("%s X-Checksum-Sha1 = %q", what, got)
		}
		if got := hdr.Get(hdrChecksumMd5); got != sumMd5(manifest) {
			t.Errorf("%s X-Checksum-Md5 = %q", what, got)
		}
		if got := hdr.Get(hdrChecksumSha256); got != manifestHex {
			t.Errorf("%s X-Checksum-Sha256 = %q", what, got)
		}
		if got := hdr.Get(hdrContentDigest); got != "sha256:"+manifestHex {
			t.Errorf("%s Docker-Content-Digest = %q", what, got)
		}
		if got := hdr.Get("Last-Modified"); got == "" {
			t.Errorf("%s Last-Modified missing", what)
		} else if _, err := http.ParseTime(got); err != nil {
			t.Errorf("%s Last-Modified %q unparseable: %v", what, got, err)
		}
		if got := hdr.Get("Accept-Ranges"); got != "bytes" {
			t.Errorf("%s Accept-Ranges = %q", what, got)
		}
		if got := hdr.Get(hdrContentDisposition); got != `attachment; filename="manifest.json"` {
			t.Errorf("%s Content-Disposition = %q", what, got)
		}
		if got := hdr.Get(hdrFilename); got != "manifest.json" {
			t.Errorf("%s X-Artifactory-Filename = %q", what, got)
		}
		if got := hdr.Get(hdrOriginRemotePath); got != wantOrigin {
			t.Errorf("%s X-Artifactory-Origin-Remote-Path = %q, want %q (the REQUESTED reference echoed)", what, got, wantOrigin)
		}
	}

	// Fresh fetch (MISS): the full set rides the proxied serve.
	code, body, hdr := down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK || body != string(manifest) {
		t.Fatalf("fresh manifest GET = (%d, %d bytes), want the proxied copy", code, len(body))
	}
	assertFace(hdr, "MISS GET", upSrv.URL+"/v2/up-local/myapp/manifests/1.0")
	if got := hdr.Get("Content-Type"); got != mediaTypeDockerManifest {
		t.Errorf("Content-Type = %q", got)
	}

	// Cached HIT: the header set does not shrink (E2-3).
	code, _, hdr = down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests)
	if code != http.StatusOK {
		t.Fatalf("cached manifest GET = %d", code)
	}
	assertFace(hdr, "HIT GET", upSrv.URL+"/v2/up-local/myapp/manifests/1.0")

	// By-digest HEAD: the HEAD face adds X-Artifactory-Docker-Registry
	// naming the addressed repository key; Origin-Remote-Path echoes the
	// DIGEST spelling of the request (the requested reference, verbatim).
	code, _, hdr = down.serveReq(http.MethodHead, "/v2/docker-remote/up-local/myapp/manifests/sha256:"+manifestHex, nil, nil)
	if code != http.StatusOK {
		t.Fatalf("manifest HEAD = %d", code)
	}
	assertFace(hdr, "HEAD", upSrv.URL+"/v2/up-local/myapp/manifests/sha256:"+manifestHex)
	if got := hdr.Get(hdrDockerRegistry); got != "docker-remote" {
		t.Errorf("HEAD X-Artifactory-Docker-Registry = %q, want the addressed repoKey", got)
	}
}

// TestRemoteFaceBlobHeaderSet: the blob copy face on the 200 AND the 206
// window — Last-Modified, the sha256__<hex> filename pair and
// Origin-Remote-Path ride both (capture a_blob.h/a_blobr.h), on top of
// the checksum family the body server already owns.
func TestRemoteFaceBlobHeaderSet(t *testing.T) {
	down, upSrv, _, _, cfg, _ := newDockerRemoteFixture(t)
	// Land the chain (manifest first: the chain gate admits its blobs).
	if code, body, _ := down.get("/v2/docker-remote/up-local/myapp/manifests/1.0", acceptManifests); code != http.StatusOK {
		t.Fatalf("manifest GET = (%d, %s)", code, body)
	}
	cfgHex := sha256Hex(cfg)
	wantOrigin := upSrv.URL + "/v2/up-local/myapp/blobs/sha256:" + cfgHex

	code, body, hdr := down.get("/v2/docker-remote/up-local/myapp/blobs/sha256:"+cfgHex, nil)
	if code != http.StatusOK || body != string(cfg) {
		t.Fatalf("blob GET = (%d, %d bytes)", code, len(body))
	}
	for _, tc := range []struct{ name, key, want string }{
		{"Last-Modified present", "Last-Modified", ""},
		{"filename disposition", hdrContentDisposition, `attachment; filename="sha256__` + cfgHex + `"`},
		{"filename header", hdrFilename, "sha256__" + cfgHex},
		{"origin remote path", hdrOriginRemotePath, wantOrigin},
	} {
		if got := hdr.Get(tc.key); tc.want == "" && got == "" || tc.want != "" && got != tc.want {
			t.Errorf("blob GET %s = %q, want %q", tc.name, got, tc.want)
		}
	}

	// The 206 window carries the same face.
	code, _, hdr = down.get("/v2/docker-remote/up-local/myapp/blobs/sha256:"+cfgHex, map[string]string{"Range": "bytes=0-9"})
	if code != http.StatusPartialContent {
		t.Fatalf("blob Range GET = %d, want 206", code)
	}
	if got := hdr.Get(hdrOriginRemotePath); got != wantOrigin {
		t.Errorf("blob 206 Origin-Remote-Path = %q", got)
	}
	if got := hdr.Get("Last-Modified"); got == "" {
		t.Error("blob 206 Last-Modified missing")
	}
	if got := hdr.Get(hdrFilename); got != "sha256__"+cfgHex {
		t.Errorf("blob 206 X-Artifactory-Filename = %q", got)
	}
}

// TestRemoteListPagination: tags/list and the repo-domain _catalog honor
// the client's n/last window over the aggregated full list — n caps the
// page with a Link rel="next" exactly while a further page exists, last
// is the exclusive cursor, an invalid n answers the generic error model's
// 404 "Not Found" (BEFORE any upstream contact), and the bare GET keeps
// the full-list default (captures a_tagsn.h/a_tagsl.h/a_tagsinv.h).
func TestRemoteListPagination(t *testing.T) {
	tags := []string{"t1", "t2", "t3"}
	up, hits := newTagsStubUpstream(t, &tags)
	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", up.URL)

	// The invalid-n 404 is local and fast: no upstream roundtrip.
	code, body, _ := down.get("/v2/docker-remote/myimg/tags/list?n=abc", nil)
	if code != http.StatusNotFound || !strings.Contains(body, `"message": "Not Found"`) {
		t.Fatalf("invalid n = (%d, %s), want the 404 Not Found status form", code, body)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("invalid n contacted the upstream %d times", got)
	}

	// n caps the page, Link points at the remainder.
	code, body, hdr := down.get("/v2/docker-remote/myimg/tags/list?n=2", nil)
	if code != http.StatusOK {
		t.Fatalf("n=2 = %d", code)
	}
	var page tagsBody
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("n=2 body %q: %v", body, err)
	}
	if len(page.Tags) != 2 || page.Tags[0] != "t1" || page.Tags[1] != "t2" {
		t.Fatalf("n=2 tags = %v, want [t1 t2]", page.Tags)
	}
	if want := `</v2/docker-remote/myimg/tags/list?last=t2&n=2>; rel="next"`; hdr.Get("Link") != want {
		t.Fatalf("n=2 Link = %q, want %q", hdr.Get("Link"), want)
	}

	// last is the exclusive cursor; the terminal page carries no Link.
	code, body, hdr = down.get("/v2/docker-remote/myimg/tags/list?n=2&last=t1", nil)
	if code != http.StatusOK {
		t.Fatalf("n=2&last=t1 = %d", code)
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("n=2&last=t1 body %q: %v", body, err)
	}
	if len(page.Tags) != 2 || page.Tags[0] != "t2" || page.Tags[1] != "t3" {
		t.Fatalf("n=2&last=t1 tags = %v, want [t2 t3]", page.Tags)
	}
	if hdr.Get("Link") != "" {
		t.Fatalf("terminal page Link = %q, want none", hdr.Get("Link"))
	}

	// The bare GET keeps the full-list default.
	code, body, hdr = down.get("/v2/docker-remote/myimg/tags/list", nil)
	if code != http.StatusOK {
		t.Fatalf("bare tags/list = %d", code)
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("bare body %q: %v", body, err)
	}
	if len(page.Tags) != 3 || hdr.Get("Link") != "" {
		t.Fatalf("bare tags = %v (Link %q), want the full list without Link", page.Tags, hdr.Get("Link"))
	}
}

// TestRemoteCatalogPagination: the same n/last window on the repo-domain
// _catalog (the ticket's symmetric ruling; the reference capture set has
// the tags face only — the catalog Link path is the repo-domain shape).
func TestRemoteCatalogPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": []string{"x", "y", "z"}})
	}))
	t.Cleanup(srv.Close)
	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", srv.URL)

	code, body, hdr := down.get("/v2/docker-remote/_catalog?n=2", nil)
	if code != http.StatusOK {
		t.Fatalf("catalog n=2 = (%d, %s)", code, body)
	}
	var cat catalogBody
	if err := json.Unmarshal([]byte(body), &cat); err != nil {
		t.Fatalf("catalog body %q: %v", body, err)
	}
	if len(cat.Repositories) != 2 || cat.Repositories[0] != "x" || cat.Repositories[1] != "y" {
		t.Fatalf("catalog n=2 = %v, want [x y]", cat.Repositories)
	}
	// The reference's repo-domain catalog Link points at the registry-level
	// /v2/_catalog path (live L003-2 capture) — the quirk, copied verbatim.
	if want := `</v2/_catalog?last=y&n=2>; rel="next"`; hdr.Get("Link") != want {
		t.Fatalf("catalog Link = %q, want %q", hdr.Get("Link"), want)
	}

	code, body, hdr = down.get("/v2/docker-remote/_catalog?n=2&last=y", nil)
	if code != http.StatusOK {
		t.Fatalf("catalog last=y = (%d, %s)", code, body)
	}
	if err := json.Unmarshal([]byte(body), &cat); err != nil {
		t.Fatalf("catalog body %q: %v", body, err)
	}
	if len(cat.Repositories) != 1 || cat.Repositories[0] != "z" || hdr.Get("Link") != "" {
		t.Fatalf("catalog last=y = %v (Link %q), want the terminal [z]", cat.Repositories, hdr.Get("Link"))
	}

	code, body, _ = down.get("/v2/docker-remote/_catalog?n=abc", nil)
	if code != http.StatusNotFound || !strings.Contains(body, `"message": "Not Found"`) {
		t.Fatalf("catalog invalid n = (%d, %s), want the 404 Not Found status form", code, body)
	}
}

// TestRemoteFaceIndexFilename: an index media type lands the
// list.manifest.json filename pair (the ^(list\.)?manifest\.json$ rule).
func TestRemoteFaceIndexFilename(t *testing.T) {
	child := []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeOCIManifest +
		`","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:` + sha256Hex([]byte("icfg")) + `","size":2}}`)
	childDgst := "sha256:" + sha256Hex(child)
	index := []byte(`{"schemaVersion":2,"mediaType":"` + mediaTypeOCIIndex +
		`","manifests":[{"mediaType":"` + mediaTypeOCIManifest + `","digest":"` + childDgst + `","size":10}]}`)
	up := &remotePullStack{newCatalogStack(t, true)}
	up.seedDockerRepo("up-local")
	// The index's child manifest (and the child's config blob — the push
	// chain is presence-checked level by level).
	if code, body, _ := up.serveReq(http.MethodPost, "/v2/up-local/myapp/blobs/uploads/?digest=sha256:"+sha256Hex([]byte("icfg")),
		strings.NewReader("icfg"), nil); code != http.StatusCreated {
		t.Fatalf("upstream config push = (%d, %s)", code, body)
	}
	if code, body, _ := up.serveReq(http.MethodPut, "/v2/up-local/myapp/manifests/child",
		strings.NewReader(string(child)), map[string]string{"Content-Type": mediaTypeOCIManifest}); code != http.StatusCreated {
		t.Fatalf("upstream child PUT = (%d, %s)", code, body)
	}
	if code, body, _ := up.serveReq(http.MethodPut, "/v2/up-local/myapp/manifests/idx",
		strings.NewReader(string(index)), map[string]string{"Content-Type": mediaTypeOCIIndex}); code != http.StatusCreated {
		t.Fatalf("upstream index PUT = (%d, %s)", code, body)
	}
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(adapter.WithPrincipal(r.Context(), up.admin))
		up.h.ServeHTTP(w, r)
	}))
	t.Cleanup(upSrv.Close)

	down := &remotePullStack{newCatalogStack(t, true)}
	down.seedDockerRemoteRepo(t, "docker-remote", upSrv.URL)

	code, _, hdr := down.get("/v2/docker-remote/up-local/myapp/manifests/idx", map[string]string{
		"Accept": mediaTypeOCIIndex + ", " + mediaTypeOCIManifest})
	if code != http.StatusOK {
		t.Fatalf("index GET = %d", code)
	}
	if got := hdr.Get(hdrFilename); got != "list.manifest.json" {
		t.Fatalf("index filename = %q, want list.manifest.json", got)
	}
	if got := hdr.Get(hdrContentDisposition); got != `attachment; filename="list.manifest.json"` {
		t.Fatalf("index disposition = %q", got)
	}
}
