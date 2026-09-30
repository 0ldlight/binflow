// T-598 / BIN-80 (ledger maven/sidecar-get-x-checksum-sha256-echo, live A
// 7.161.26 legs m1-get-sha1-set / m1-head-sha1-set, L041 Arm 1): the
// sidecar face's header rendering is VERB-CONDITIONAL — GET answers the
// bare infra set (CT/Content-Length/Last-Modified, nothing a client could
// validate the sidecar bytes against), HEAD answers the full validator set
// (Accept-Ranges, ETag and the X-Checksum-{Md5,Sha1,Sha256} triple of the
// SOURCE artifact). These tests pin both halves of the flip plus the
// conditional consequences (GET's If-None-Match goes inert with the
// retracted ETag; If-Modified-Since keeps its 304 off the rendered stamp).
package maven

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// assertBareSidecarGet fails when a sidecar GET response carries any
// validator the A GET face does not render.
func assertBareSidecarGet(t *testing.T, resp *http.Response, wantBody string, wantLen int) {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sidecar GET = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	for _, h := range []string{"ETag", "Accept-Ranges", "X-Checksum-Md5", "X-Checksum-Sha1", "X-Checksum-Sha256"} {
		if v := resp.Header.Get(h); v != "" {
			t.Errorf("sidecar GET carries %s: %q, want none (A GET face renders the bare infra set)", h, v)
		}
	}
	if ct := resp.Header.Get("Content-Type"); ct != sidecarContentType {
		t.Errorf("sidecar GET Content-Type = %q, want %q", ct, sidecarContentType)
	}
	if resp.Header.Get("Last-Modified") == "" {
		t.Error("sidecar GET lost Last-Modified (an infra header the A face keeps)")
	}
	if got := string(drain(t, resp)); got != wantBody {
		t.Errorf("sidecar GET body = %q, want %q", got, wantBody)
	}
	if resp.Header.Get("Content-Length") != strconv.Itoa(wantLen) {
		t.Errorf("sidecar GET Content-Length = %q, want %d", resp.Header.Get("Content-Length"), wantLen)
	}
}

// TestSidecarHeaderVerbModel walks the GET and HEAD halves of one seeded
// artifact across the registered, computed and wrong-registered arms.
func TestSidecarHeaderVerbModel(t *testing.T) {
	hs := newHarness(t)
	jar := "com/diff/t598/1.0.0/t598-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, m5, s256 := digests(jarBytes)

	// Register the sha1 the way a real client does (correct value) and a
	// WRONG md5 through the 409 write-through, so the registered echo, the
	// wrong-declared echo and the on-demand computed arms all exist before
	// the verb halves walk them.
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1 registration = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	wrong := strings.Repeat("0", 32)
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".md5", []byte(wrong), nil, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("md5 wrong-value registration = %d, want the 409 write-through (%s)", resp.StatusCode, drain(t, resp))
	}

	// GET half — zero validator headers on every arm: the registered echo
	// (.sha1), the wrong-declared echo (.md5) and the on-demand computed
	// digest (.sha256), each with its bare hex length.
	assertBareSidecarGet(t, hs.serve(http.MethodGet, "/maven-local/"+jar+".sha1", nil, nil, true), s1, len(s1))
	assertBareSidecarGet(t, hs.serve(http.MethodGet, "/maven-local/"+jar+".md5", nil, nil, true), wrong, len(wrong))
	assertBareSidecarGet(t, hs.serve(http.MethodGet, "/maven-local/"+jar+".sha256", nil, nil, true), s256, len(s256))

	// HEAD half — the full validator set, every key addressing the SOURCE
	// artifact: ETag is the unquoted source sha1, the triple is the source's
	// computed triple (the declared WRONG md5 never mirrors into it),
	// Accept-Ranges rides along, and the body stays suppressed with the
	// sidecar's own Content-Length.
	for _, algo := range []string{"sha1", "sha256", "md5"} {
		h := hs.serve(http.MethodHead, "/maven-local/"+jar+"."+algo, nil, nil, true)
		if h.StatusCode != http.StatusOK {
			t.Fatalf("HEAD .%s = %d (%s)", algo, h.StatusCode, drain(t, h))
		}
		if got := h.Header.Get("ETag"); got != s1 {
			t.Errorf("HEAD .%s ETag = %q, want the source sha1 %q", algo, got, s1)
		}
		if got := h.Header.Get("X-Checksum-Md5"); got != m5 {
			t.Errorf("HEAD .%s X-Checksum-Md5 = %q, want the source %q", algo, got, m5)
		}
		if got := h.Header.Get("X-Checksum-Sha1"); got != s1 {
			t.Errorf("HEAD .%s X-Checksum-Sha1 = %q, want the source %q", algo, got, s1)
		}
		if got := h.Header.Get("X-Checksum-Sha256"); got != s256 {
			t.Errorf("HEAD .%s X-Checksum-Sha256 = %q, want the source %q", algo, got, s256)
		}
		if got := h.Header.Get("Accept-Ranges"); got != "bytes" {
			t.Errorf("HEAD .%s Accept-Ranges = %q, want bytes", algo, got)
		}
		if ct := h.Header.Get("Content-Type"); ct != sidecarContentType {
			t.Errorf("HEAD .%s Content-Type = %q, want %q", algo, ct, sidecarContentType)
		}
		if h.Header.Get("Last-Modified") == "" {
			t.Errorf("HEAD .%s lost Last-Modified", algo)
		}
		if b := drain(t, h); len(b) != 0 {
			t.Errorf("HEAD .%s body = %q, want empty", algo, b)
		}
	}

	// The wrong-declared md5's HEAD leg pinned above (X-Checksum-Md5 = the
	// computed m5, Content-Length = the declared value's length) closes the
	// model: the validators address the source, the body echoes the
	// declaration.
	if got := hs.serve(http.MethodHead, "/maven-local/"+jar+".md5", nil, nil, true).Header.Get("X-Checksum-Md5"); got != m5 {
		t.Errorf("HEAD .md5 (declared %q) X-Checksum-Md5 = %q, want the computed %q — the triple addresses the source", wrong, got, m5)
	}
	if got := hs.serve(http.MethodHead, "/maven-local/"+jar+".md5", nil, nil, true).Header.Get("Content-Length"); got != strconv.Itoa(len(wrong)) {
		t.Errorf("HEAD .md5 Content-Length = %q, want the sidecar body's %d", got, len(wrong))
	}
}

// TestSidecarHeaderVerbModelConditionals pins the conditional halves the
// verb model implies: the retracted GET ETag makes If-None-Match inert
// (never a 304 off a header the face no longer serves), If-Modified-Since
// keeps earning its 304 off the rendered stamp, and the HEAD ETag answers
// If-None-Match with the source sha1.
func TestSidecarHeaderVerbModelConditionals(t *testing.T) {
	hs := newHarness(t)
	jar := "com/diff/t598c/1.0.0/t598c-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, _, s256 := digests(jarBytes)

	// GET If-None-Match against the sidecar's own digest: 200 — no served
	// ETag means no match (the pre-T-598 self-added ETag's 290 face is
	// retired with the header itself).
	resp := hs.serve(http.MethodGet, "/maven-local/"+jar+".sha256", nil,
		map[string]string{"If-None-Match": s256}, true)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET If-None-Match = %d, want 200 (inert: the GET face serves no ETag)", resp.StatusCode)
	}
	drain(t, resp)

	// GET If-Modified-Since at a future instant: 304 off Last-Modified.
	resp = hs.serve(http.MethodGet, "/maven-local/"+jar+".sha256", nil,
		map[string]string{"If-Modified-Since": "Mon, 01 Jan 2035 00:00:00 GMT"}, true)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("GET If-Modified-Since (future) = %d, want 304", resp.StatusCode)
	}
	drain(t, resp)

	// HEAD If-None-Match with the source sha1 the HEAD face renders as
	// ETag: 304.
	resp = hs.serve(http.MethodHead, "/maven-local/"+jar+".sha256", nil,
		map[string]string{"If-None-Match": s1}, true)
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("HEAD If-None-Match (source sha1) = %d, want 304", resp.StatusCode)
	}
	drain(t, resp)
}

// TestSidecarHeaderVerbModelFaces keeps the surrounding forms honest: the
// unset md5/sha1 404 family (both verbs — the miss renders before any
// header face exists), the miss wording pointing at the SOURCE, and the
// VIRTUAL face carrying the same verb model (its GET echoes the member's
// registered value bare, its HEAD triple answers the member's source).
func TestSidecarHeaderVerbModelFaces(t *testing.T) {
	hs := newHarness(t)
	if _, err := hs.svc.CreateRepo(context.Background(), adminP, &metadata.Repo{
		RepoKey:     "t598-virt",
		Type:        repo.TypeVirtual,
		PackageType: Protocol,
		Config:      `{"repositories":["maven-local"]}`,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}
	jar := "com/diff/t598f/1.0.0/t598f-1.0.0.jar"
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar, jarBytes, nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	s1, m5, s256 := digests(jarBytes)
	if resp := hs.serve(http.MethodPut, "/maven-local/"+jar+".sha1", []byte(s1), nil, true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("sha1 registration = %d (%s)", resp.StatusCode, drain(t, resp))
	}

	// Unset md5 keeps its 404 on both verbs, citing the source.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		resp := hs.serve(method, "/maven-local/"+jar+".md5", nil, nil, true)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s unset .md5 = %d, want 404 (%s)", method, resp.StatusCode, drain(t, resp))
			continue
		}
		if got := string(drain(t, resp)); !strings.Contains(got, "Checksum not found for "+jar) {
			t.Errorf("%s unset .md5 body = %s, want the source-citing family", method, got)
		}
	}

	// Virtual face: the registered echo rides bare on GET, the member's
	// computed triple answers HEAD.
	assertBareSidecarGet(t, hs.serve(http.MethodGet, "/t598-virt/"+jar+".sha1", nil, nil, true), s1, len(s1))
	h := hs.serve(http.MethodHead, "/t598-virt/"+jar+".sha256", nil, nil, true)
	if h.StatusCode != http.StatusOK {
		t.Fatalf("virtual HEAD .sha256 = %d (%s)", h.StatusCode, drain(t, h))
	}
	checks := []struct{ hdrName, want string }{
		{"ETag", s1}, {"X-Checksum-Md5", m5}, {"X-Checksum-Sha1", s1},
		{"X-Checksum-Sha256", s256}, {"Accept-Ranges", "bytes"},
	}
	for _, c := range checks {
		if got := h.Header.Get(c.hdrName); got != c.want {
			t.Errorf("virtual HEAD .sha256 %s = %q, want %q", c.hdrName, got, c.want)
		}
	}
	if b := drain(t, h); len(b) != 0 {
		t.Errorf("virtual HEAD body = %q, want empty", b)
	}
}
