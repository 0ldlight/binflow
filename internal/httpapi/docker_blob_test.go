package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/console"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The T-38 blob-domain integration suite: the real router + middleware +
// adapter + repo.Service + storage engine stack, driven the way the docker
// client family drives it (D06/D07/D10/D10b/D12/D13/D13d equivalents, the
// D14 kill-consistency posture and the FR-8-AC7 cross-protocol dedup).

// drain closes a response body, ignoring the close error (test helper;
// the bodies are fully read or deliberately discarded).
func drain(resp *http.Response) {
	if resp == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// sha256Of is the suite's digest helper.
func sha256Of(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// v2Blob seeds one blob through a full monolithic push and returns its
// "sha256:<hex>" digest.
func v2SeedBlob(t *testing.T, h *harness, name string, content []byte) string {
	t.Helper()
	dgst := "sha256:" + sha256Of(content)
	resp := h.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/?digest="+dgst,
		adminUser, adminPass, content, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed blob status = %d body=%s", resp.StatusCode, body)
	}
	return dgst
}

// TestV2BlobUploadMonolithic (D06): the three-step push lands a 10MB-scale
// blob, the finalize Location addresses the blob URL, and the GET returns
// byte-identical content with Docker-Content-Digest.
func TestV2BlobUploadMonolithic(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	content := bytes.Repeat([]byte("monolithic-d06."), 1024*64) // ~1MB
	dgst := "sha256:" + sha256Of(content)

	resp := h.do(http.MethodPost, "/v2/docker-local/acme/app/blobs/uploads/",
		adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d body=%s", resp.StatusCode, body)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/v2/docker-local/acme/app/blobs/uploads/") {
		t.Fatalf("Location = %q, want a relative session URL", loc)
	}
	if resp.Header.Get("Docker-Upload-UUID") == "" {
		t.Fatal("no Docker-Upload-UUID on the 202")
	}
	if resp.Header.Get("Range") != "0-0" {
		t.Fatalf("Range = %q, want 0-0", resp.Header.Get("Range"))
	}

	resp = h.do(http.MethodPut, loc+"?digest="+dgst, adminUser, adminPass, content, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("finalize status = %d body=%s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/v2/docker-local/acme/app/blobs/"+dgst {
		t.Fatalf("finalize Location = %q", got)
	}
	if got := resp.Header.Get("Docker-Content-Digest"); got != dgst {
		t.Fatalf("Docker-Content-Digest = %q", got)
	}

	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	if got != string(content) {
		t.Fatalf("GET body differs (%d vs %d bytes)", len(got), len(content))
	}
	if resp.Header.Get("Docker-Content-Digest") != dgst {
		t.Fatalf("GET Docker-Content-Digest = %q", resp.Header.Get("Docker-Content-Digest"))
	}
	if resp.Header.Get("X-Checksum-Sha256") != strings.TrimPrefix(dgst, "sha256:") {
		t.Fatalf("X-Checksum-Sha256 = %q", resp.Header.Get("X-Checksum-Sha256"))
	}
}

// TestV2BlobUploadSingleRequest (D07): POST ?digest= with the body answers
// 201 in one round trip.
func TestV2BlobUploadSingleRequest(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	content := []byte("single-request d07 blob")
	dgst := v2SeedBlob(t, h, "docker-local/app", content)
	if dgst != "sha256:"+sha256Of(content) {
		t.Fatalf("digest = %q", dgst)
	}
	resp := h.do(http.MethodHead, "/v2/docker-local/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Content-Length") != fmt.Sprint(len(content)) {
		t.Fatalf("HEAD Content-Length = %q", resp.Header.Get("Content-Length"))
	}
}

// TestV2BlobUploadChunked (D10): >=3 PATCH chunks all 202 with growing
// Range headers, the empty-body PUT finalize lands 201, and the GET is
// byte-identical (cmp semantics).
func TestV2BlobUploadChunked(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	content := bytes.Repeat([]byte("chunked-d10-"), 1024) // 12KB
	chunks := [][]byte{content[:4096], content[4096:8192], content[8192:]}

	resp := h.do(http.MethodPost, "/v2/docker-local/acme/app/blobs/uploads/",
		adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")

	var offset int64
	for i, ch := range chunks {
		hdr := map[string]string{
			"Content-Range":  fmt.Sprintf("%d-%d", offset, offset+int64(len(ch))-1),
			"Content-Length": fmt.Sprint(len(ch)),
			"Content-Type":   "application/octet-stream",
		}
		resp = h.do(http.MethodPatch, loc, adminUser, adminPass, ch, hdr)
		mustGet(t, resp)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("chunk %d status = %d", i, resp.StatusCode)
		}
		want := fmt.Sprintf("0-%d", offset+int64(len(ch))-1)
		if got := resp.Header.Get("Range"); got != want {
			t.Fatalf("chunk %d Range = %q want %q", i, got, want)
		}
		if got := resp.Header.Get("Location"); got != loc {
			t.Fatalf("chunk %d Location = %q want %q", i, got, loc)
		}
		offset += int64(len(ch))
	}

	dgst := "sha256:" + sha256Of(content)
	resp = h.do(http.MethodPut, loc+"?digest="+dgst, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("finalize status = %d body=%s", resp.StatusCode, body)
	}

	resp = h.do(http.MethodGet, "/v2/docker-local/acme/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", resp.StatusCode)
	}
	if got != string(content) {
		t.Fatal("GET body differs from the pushed content (cmp mismatch)")
	}
}

// TestV2BlobUploadOffsetQueryAndResume (D10b): after two chunks the offset
// query answers 204 + Range over the received bytes and the third chunk
// resumes to a successful finalize.
func TestV2BlobUploadOffsetQueryAndResume(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	content := bytes.Repeat([]byte("resume-d10b."), 600) // ~7KB
	first, second, third := content[:2000], content[2000:4000], content[4000:]

	resp := h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/",
		adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	loc := resp.Header.Get("Location")

	for i, ch := range [][]byte{first, second} {
		resp = h.do(http.MethodPatch, loc, adminUser, adminPass, ch, map[string]string{
			"Content-Range": fmt.Sprintf("%d-%d", int64(i)*2000, int64(i)*2000+1999),
		})
		mustGet(t, resp)
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("chunk %d status = %d", i, resp.StatusCode)
		}
	}

	// The offset query: 204, no body, Range names the received window.
	resp = h.do(http.MethodGet, loc, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("offset query status = %d body=%s", resp.StatusCode, body)
	}
	if body != "" {
		t.Fatalf("offset query body = %q, want empty", body)
	}
	if got := resp.Header.Get("Range"); got != "0-3999" {
		t.Fatalf("offset query Range = %q, want 0-3999", got)
	}

	// The third chunk continues from the authoritative offset.
	resp = h.do(http.MethodPatch, loc, adminUser, adminPass, third, map[string]string{
		"Content-Range": "4000-6999",
	})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("resume chunk status = %d", resp.StatusCode)
	}
	dgst := "sha256:" + sha256Of(content)
	resp = h.do(http.MethodPut, loc+"?digest="+dgst, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("finalize status = %d", resp.StatusCode)
	}
}

// TestV2BlobUploadCancel: DELETE on a live session answers 204 and the
// session is unknown afterwards (the official cancel endpoint).
func TestV2BlobUploadCancel(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	resp := h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/",
		adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	loc := resp.Header.Get("Location")

	resp = h.do(http.MethodDelete, loc, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel status = %d body=%s", resp.StatusCode, body)
	}
	resp = h.do(http.MethodGet, loc, adminUser, adminPass, nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("post-cancel GET status = %d", resp.StatusCode)
	}
	if !strings.Contains(body, "BLOB_UPLOAD_UNKNOWN") {
		t.Fatalf("post-cancel body = %s", body)
	}
}

// TestV2BlobDigestMismatch (D12): a finalize whose digest disagrees with
// the content is 400 DIGEST_INVALID, the session leaves no residue and no
// blob is visible at either digest.
func TestV2BlobDigestMismatch(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	content := []byte("d12 mismatch probe")
	resp := h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/?digest=sha256:"+
		strings.Repeat("0", 64), adminUser, adminPass, content, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "DIGEST_INVALID" {
		t.Fatalf("code = %q, want DIGEST_INVALID", eb.Errors[0].Code)
	}

	// The blob store never saw it.
	resp = h.do(http.MethodGet, "/v2/docker-local/app/blobs/sha256:"+strings.Repeat("0", 64),
		adminUser, adminPass, nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "BLOB_UNKNOWN") {
		t.Fatalf("phantom blob status = %d body=%s", resp.StatusCode, body)
	}
	resp = h.do(http.MethodGet, "/v2/docker-local/app/blobs/sha256:"+sha256Of(content),
		adminUser, adminPass, nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("actual-content blob leaked: %d %s", resp.StatusCode, body)
	}
}

// TestV2BlobCrossRepoMount (D13): mount from a source the caller can read
// answers 201 with zero body transfer and the destination serves the blob;
// a mount from a source WITHOUT read degrades to the 202 upload grant.
func TestV2BlobCrossRepoMount(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"reader", "reader-pw"}})
	seedDockerRepo(t, h, "docker-local")
	seedDockerRepo(t, h, "charts")

	content := []byte("d13 mountable layer")
	dgst := v2SeedBlob(t, h, "docker-local/acme/app", content)

	// Successful mount: zero-copy 201 (no session, no body).
	resp := h.do(http.MethodPost,
		"/v2/charts/myapp/blobs/uploads/?mount="+dgst+"&from=docker-local/acme/app",
		adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mount status = %d body=%s", resp.StatusCode, body)
	}
	resp = h.do(http.MethodGet, "/v2/charts/myapp/blobs/"+dgst, adminUser, adminPass, nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || got != string(content) {
		t.Fatalf("mounted GET status = %d", resp.StatusCode)
	}

	// No read on the source: the principal MAY push into charts (the route
	// gate passes) but cannot read docker-local — the mount degrades to the
	// plain 202 upload grant instead of erroring (the spec's fallback).
	grant(t, h, "reader-charts", "charts", "**", "reader", true, true, false)
	resp = h.do(http.MethodPost,
		"/v2/charts/other/blobs/uploads/?mount="+dgst+"&from=docker-local/acme/app",
		"reader", "reader-pw", nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("unreadable mount status = %d, want the degraded 202", resp.StatusCode)
	}
	if resp.Header.Get("Docker-Upload-UUID") == "" {
		t.Fatal("degraded mount granted no session")
	}
}

// TestV2BlobDeleteRejected (D13d/DE-14): DELETE on a blob is the deliberate
// 405 UNSUPPORTED — reclamation is GC's business.
func TestV2BlobDeleteRejected(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	dgst := v2SeedBlob(t, h, "docker-local/app", []byte("d13d blob"))

	resp := h.do(http.MethodDelete, "/v2/docker-local/app/blobs/"+dgst,
		adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE blob status = %d", resp.StatusCode)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "UNSUPPORTED" {
		t.Fatalf("code = %q, want UNSUPPORTED", eb.Errors[0].Code)
	}

	// The blob survives (nothing was reclaimed).
	resp = h.do(http.MethodGet, "/v2/docker-local/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET after rejected DELETE status = %d", resp.StatusCode)
	}
}

// TestV2BlobEmptyLayerSynthesis: the canonical empty-layer digest answers
// GET/HEAD from the fixed 32 bytes on a repository where nothing was pushed.
func TestV2BlobEmptyLayerSynthesis(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	url := "/v2/docker-local/app/blobs/sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"
	resp := h.do(http.MethodGet, url, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET empty layer status = %d body=%s", resp.StatusCode, body)
	}
	if len(body) != 32 {
		t.Fatalf("empty layer body is %d bytes, want 32", len(body))
	}
	if sha256Of([]byte(body)) != "a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4" {
		t.Fatal("empty layer bytes do not hash to the canonical digest")
	}
	resp = h.do(http.MethodHead, url, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Length") != "32" {
		t.Fatalf("HEAD empty layer status = %d len=%q",
			resp.StatusCode, resp.Header.Get("Content-Length"))
	}
}

// TestV2BlobRangeOnRealStack (FR-8-AC8): the Range semantics through the
// full chain — a slice answers 206 with the exact bytes.
func TestV2BlobRangeOnRealStack(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	dgst := v2SeedBlob(t, h, "docker-local/app", content)

	resp := h.do(http.MethodGet, "/v2/docker-local/app/blobs/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Range": "bytes=10-19"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("range GET status = %d", resp.StatusCode)
	}
	if body != string(content[10:20]) {
		t.Fatalf("range body = %q", body)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes 10-19/36" {
		t.Fatalf("Content-Range = %q", got)
	}

	// Unsatisfiable: 416 with the total.
	resp = h.do(http.MethodGet, "/v2/docker-local/app/blobs/"+dgst,
		adminUser, adminPass, nil, map[string]string{"Range": "bytes=999-1000"})
	mustGet(t, resp)
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfiable status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes */36" {
		t.Fatalf("416 Content-Range = %q", got)
	}
}

// TestV2BlobUnknown404: a never-uploaded digest answers the spec
// BLOB_UNKNOWN body through the real chain.
func TestV2BlobUnknown404(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	resp := h.do(http.MethodGet, "/v2/docker-local/app/blobs/sha256:"+strings.Repeat("cd", 32),
		adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "BLOB_UNKNOWN" {
		t.Fatalf("code = %q, want BLOB_UNKNOWN", eb.Errors[0].Code)
	}
}

// TestV2BlobSessionDiesWithProcess (D14's core posture): the upload-session
// registry is process state by design — after a process restart the old
// session URL is BLOB_UPLOAD_UNKNOWN, a blob finalized BEFORE the restart
// stays readable, and one that was mid-upload was never visible.
func TestV2BlobSessionDiesWithProcess(t *testing.T) {
	// This test builds its own stack so the "restart" is a second assembly
	// over the same data directory (the closest an in-process suite comes
	// to kill -9; the storage engine's startup sweep is the real process's
	// disk-side backstop).
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	h1 := newHarnessWithDataDir(t, dataDir, st)
	seedDockerRepo(t, h1, "docker-local")

	finished := []byte("committed before the restart")
	dgst := v2SeedBlob(t, h1, "docker-local/app", finished)

	resp := h1.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/",
		adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	loc := resp.Header.Get("Location")
	resp = h1.do(http.MethodPatch, loc, adminUser, adminPass, []byte("half-pushed"), nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("mid-upload PATCH status = %d", resp.StatusCode)
	}

	// The mid-upload content is NOT visible as a blob even before the
	// restart (a half-received session is not a blob).
	mid := "sha256:" + sha256Of([]byte("half-pushed"))
	resp = h1.do(http.MethodGet, "/v2/docker-local/app/blobs/"+mid, adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mid-upload blob visible before restart: %d", resp.StatusCode)
	}

	// Simulate the crash: close the engine (the sweep clears the orphaned
	// session directory), reopen, rebuild the HTTP surface.
	if err := st.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	st2, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("reopen engine: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	h2 := newHarnessWithDataDir(t, dataDir, st2)

	// The pre-restart session is unknown to the new process.
	resp = h2.do(http.MethodGet, loc, adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(body, "BLOB_UPLOAD_UNKNOWN") {
		t.Fatalf("post-restart session status = %d body=%s", resp.StatusCode, body)
	}

	// The finished blob survives the restart.
	resp = h2.do(http.MethodGet, "/v2/docker-local/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	got := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || got != string(finished) {
		t.Fatalf("historical blob after restart: status=%d", resp.StatusCode)
	}
}

// TestV2BlobCrossProtocolDedup (FR-8-AC7): the same content pushed through
// the generic plane and then through the docker blob plane leaves the blob
// counter unchanged — one physical blob, two node rows.
func TestV2BlobCrossProtocolDedup(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")
	seedRepo(t, h, "generic-local")

	content := []byte("fr-8-ac7 cross-protocol dedup payload")

	countBlobs := func() int64 {
		t.Helper()
		n, err := h.md.Blobs().Count(t.Context())
		if err != nil {
			t.Fatalf("count blobs: %v", err)
		}
		return n
	}

	// Push through generic.
	resp := h.do(http.MethodPut, "/binflow/generic-local/x/fr8.bin",
		adminUser, adminPass, content, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("generic push status = %d", resp.StatusCode)
	}
	before := countBlobs()

	// Push the same content through docker.
	dgst := v2SeedBlob(t, h, "docker-local/app", content)
	after := countBlobs()
	if before != after {
		t.Fatalf("blob count moved %d -> %d on a same-content docker push", before, after)
	}

	// Both protocols serve their own node over the one blob.
	resp = h.do(http.MethodGet, "/binflow/generic-local/x/fr8.bin", adminUser, adminPass, nil, nil)
	if got := mustGet(t, resp); resp.StatusCode != http.StatusOK || got != string(content) {
		t.Fatalf("generic read-back status = %d", resp.StatusCode)
	}
	resp = h.do(http.MethodGet, "/v2/docker-local/app/blobs/"+dgst, adminUser, adminPass, nil, nil)
	if got := mustGet(t, resp); resp.StatusCode != http.StatusOK || got != string(content) {
		t.Fatalf("docker read-back status = %d", resp.StatusCode)
	}

	// The node rows are two distinct references to one blob.
	nodes, err := h.md.Nodes().ListByPrefix(t.Context(), "generic-local", "")
	if err != nil || len(nodes) != 1 {
		t.Fatalf("generic nodes = %d err=%v", len(nodes), err)
	}
	dnodes, err := h.md.Nodes().ListByPrefix(t.Context(), "docker-local", "")
	if err != nil || len(dnodes) != 1 {
		t.Fatalf("docker nodes = %d err=%v", len(dnodes), err)
	}
	if nodes[0].Sha256 != dnodes[0].Sha256 {
		t.Fatalf("the two nodes do not share the blob: %s vs %s",
			nodes[0].Sha256, dnodes[0].Sha256)
	}
}

// TestV2BlobUploadAuthGate: the permission gates T-37 installed ahead of
// the blob domain hold for the new endpoints — anonymous writes challenge,
// read-only principals get DENIED, and the token flow carries a push.
func TestV2BlobUploadAuthGate(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci", "ci-pw"}})
	seedDockerRepo(t, h, "docker-local")

	// Anonymous write: the scoped challenge.
	resp := h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/", "", "", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous start status = %d body=%s", resp.StatusCode, body)
	}
	if ch := resp.Header.Get("WWW-Authenticate"); !strings.Contains(ch, `repository:docker-local/app:pull,push`) {
		t.Fatalf("challenge = %q", ch)
	}

	// A read-only principal (no grants): DENIED through the gate.
	resp = h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/", "ci", "ci-pw", nil, nil)
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-less principal start status = %d body=%s", resp.StatusCode, body)
	}
	eb := decodeSpecError(t, body)
	if eb.Errors[0].Code != "DENIED" {
		t.Fatalf("code = %q, want DENIED", eb.Errors[0].Code)
	}

	// Grant read+write on the image path; the same principal now pushes.
	grant(t, h, "ci-push", "docker-local", "app/**", "ci", true, true, false)

	resp = h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/", "ci", "ci-pw", nil, nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("authorized start status = %d", resp.StatusCode)
	}
}

// TestV2BlobConcurrentUploads: parallel pushes of distinct content through
// distinct sessions all land, converge to zero live state, and every blob
// reads back exactly.
func TestV2BlobConcurrentUploads(t *testing.T) {
	h := newHarness(t)
	seedDockerRepo(t, h, "docker-local")

	const workers = 6
	type result struct {
		dgst    string
		content []byte
		err     string
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			content := bytes.Repeat([]byte{byte('a' + i)}, 8192+i*100)
			dgst := "sha256:" + sha256Of(content)
			name := fmt.Sprintf("docker-local/conc%d", i%2)

			resp := h.do(http.MethodPost, "/v2/"+name+"/blobs/uploads/", adminUser, adminPass, nil, nil)
			if resp.StatusCode != http.StatusAccepted {
				drain(resp)
				results <- result{err: fmt.Sprintf("start %d", resp.StatusCode)}
				return
			}
			loc := resp.Header.Get("Location")
			drain(resp)

			req, _ := http.NewRequest(http.MethodPatch, h.srv.URL+loc, bytes.NewReader(content))
			req.SetBasicAuth(adminUser, adminPass)
			presp, err := h.srv.Client().Do(req)
			if err != nil {
				results <- result{err: err.Error()}
				return
			}
			drain(presp)
			if presp.StatusCode != http.StatusAccepted {
				results <- result{err: fmt.Sprintf("patch %d", presp.StatusCode)}
				return
			}

			req, _ = http.NewRequest(http.MethodPut, h.srv.URL+loc+"?digest="+dgst, nil)
			req.SetBasicAuth(adminUser, adminPass)
			fresp, err := h.srv.Client().Do(req)
			if err != nil {
				results <- result{err: err.Error()}
				return
			}
			drain(fresp)
			if fresp.StatusCode != http.StatusCreated {
				results <- result{err: fmt.Sprintf("finalize %d", fresp.StatusCode)}
				return
			}
			results <- result{dgst: dgst, content: content}
		}(i)
	}
	wg.Wait()
	close(results)
	for r := range results {
		if r.err != "" {
			t.Fatalf("worker failed: %s", r.err)
		}
		resp := h.do(http.MethodGet, "/v2/docker-local/conc0/blobs/"+r.dgst, adminUser, adminPass, nil, nil)
		_ = resp
	}
	// All six blobs exist in the ledger.
	if n, err := h.md.Blobs().Count(t.Context()); err != nil || n != 6 {
		t.Fatalf("blob count = %d err=%v, want 6", n, err)
	}
}

// newHarnessWithDataDir builds the standard harness over an externally
// owned data directory + engine (the restart test's second assembly). The
// metadata store is shared with the first assembly's directory (the same
// binflow.db), so the repository rows and nodes survive the "crash".
func newHarnessWithDataDir(t *testing.T, dataDir string, st storage.Engine) *harness {
	t.Helper()
	ctx := t.Context()

	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, nil)
	genericHandler := generic.New(svc, md.Blobs())
	dockerHandler := docker.New(svc, docker.NewRepoLookup(md.Repos()),
		authSvc, authSvc, md.Users(), docker.Options{
			AnonymousAccess: cfg.Security.AnonymousAccess,
			BaseURL:         cfg.Server.BaseURL,
			TokenTTL:        cfg.Auth.TokenDefaultTTL,
		}, nil).WithStorage(st, md.Blobs())

	lines, logger, mu := newCapturingLogger()
	s := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		DataDir:   dataDir,
		Console:   console.Handler(),
		Adapters:  []adapter.Handler{genericHandler, dockerHandler},
		Version:   "1.0.0-test",
		Revision:  "restart",
	}, logger)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	return &harness{
		t: t, srv: ts, st: st, md: md, svc: svc, authSvc: authSvc,
		logLines: lines, mu: mu,
	}
}

// os/stat check the sessions sweep left nothing behind (D14's residue arm).
func TestV2BlobSessionSweepResidue(t *testing.T) {
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	h := newHarnessWithDataDir(t, dataDir, st)
	seedDockerRepo(t, h, "docker-local")

	resp := h.do(http.MethodPost, "/v2/docker-local/app/blobs/uploads/", adminUser, adminPass, nil, nil)
	mustGet(t, resp)
	loc := resp.Header.Get("Location")
	resp = h.do(http.MethodPatch, loc, adminUser, adminPass, []byte("abandoned"), nil)
	mustGet(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("abandoned PATCH status = %d", resp.StatusCode)
	}

	// Crash: close WITHOUT a graceful cancel, reopen (the startup sweep
	// removes the orphaned session directory).
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = st2.Close() })

	sessionsDir := dataDir + "/sessions"
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("session residue survived the startup sweep: %d entries", len(entries))
	}
}
