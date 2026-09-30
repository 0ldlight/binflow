package helm

// The download-miss 404 face (BIN-103/T-621): the GET/HEAD file miss on
// the helm adapter renders the SHARED errors[] envelope (internal/errface)
// with the reference's download-side wording, and the media type follows
// the plane matrix — the repo-path content plane answers the charset
// spelling, the read-only /api/helm download alias the bare one (the
// T-615 legs ar1-h2-tgz-* / ar1-h-tgz-* against Artifactory 7.161.26,
// both 132-byte bodies identical). The write-side miss fallback keeps its
// plain-text refusal (unpinned face), pinned here against ride-along.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// jacksonMissBody is the pinned body layout for the download miss.
func jacksonMissBody(repoKey, path string) string {
	return fmt.Sprintf("{\n  \"errors\" : [ {\n    \"status\" : 404,\n"+
		"    \"message\" : \"File not found.; Path: '%s:%s'\"\n  } ]\n}", repoKey, path)
}

// TestDownloadMissFacePlaneMatrix walks the four pinned legs — GET/HEAD on
// both spellings of a missing chart — through the full router (the alias
// rewrite runs, so the plane decision exercises the RequestURI seam).
func TestDownloadMissFacePlaneMatrix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-miss", repo.TypeLocal, "{}")

	const chart = "missing-chart-9.9.9.tgz"
	tests := []struct {
		name   string
		method string
		path   string
		wantCT string
	}{
		{"content plane GET", http.MethodGet, "/binflow/helm-miss/" + chart, "application/json;charset=ISO-8859-1"},
		{"content plane HEAD", http.MethodHead, "/binflow/helm-miss/" + chart, "application/json;charset=ISO-8859-1"},
		{"api alias GET", http.MethodGet, "/binflow/api/helm/helm-miss/" + chart, "application/json"},
		{"api alias HEAD", http.MethodHead, "/binflow/api/helm/helm-miss/" + chart, "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var status int
			var body string
			var hdr http.Header
			if tt.method == http.MethodHead {
				status, _, hdr = s.do(http.MethodHead, tt.path, "", "", nil, nil)
			} else {
				status, body, hdr = s.get(tt.path)
			}
			if status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", status)
			}
			if got := hdr.Get("Content-Type"); got != tt.wantCT {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantCT)
			}
			if tt.method == http.MethodHead {
				return // the transport discards HEAD bodies (framing is INTENTIONAL drift)
			}
			if want := jacksonMissBody("helm-miss", chart); body != want {
				t.Fatalf("body = %q,\nwant   %q", body, want)
			}
		})
	}
}

// TestDownloadMissFaceRawAmpersand pins L009-3 on the miss face: the
// requested path rides the message raw (no HTML escaping).
func TestDownloadMissFaceRawAmpersand(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-miss", repo.TypeLocal, "{}")
	_, body, _ := s.get("/binflow/helm-miss/a&b-1.0.0.tgz")
	if !strings.Contains(body, "a&b-1.0.0.tgz") || !strings.Contains(body, `"message" : "`) {
		t.Fatalf("body %q must carry the path and the Jackson layout raw", body)
	}
	if strings.Contains(body, "\\u0026") {
		t.Fatalf("body %q must not HTML-escape the ampersand", body)
	}
}

// TestWriteSideMissKeepsPlainText pins the ride-along guard: the virtual
// DELETE miss (the unpinned own-storage fallback arm, virtual-resolution
// section 7.5) keeps its plain-text refusal — the envelope conversion is
// the read face's alone.
func TestWriteSideMissKeepsPlainText(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "helm-l", repo.TypeLocal, "{}")
	s.seedVirtualRepo(t, "helm-v", "helm-l", "helm-l")
	if status, _, _ := s.put("/binflow/helm-v/routed-1.0.0.tgz",
		fixtureChart(t, "routed", defaultChartYAML("routed", "1.0.0"), nil), nil); status != http.StatusCreated {
		t.Fatalf("virtual PUT = %d, want 201", status)
	}
	status, body, hdr := s.delete("/binflow/helm-v/routed-1.0.0.tgz")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if got := hdr.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want the unchanged plain-text face", got)
	}
	if !strings.Contains(body, "'helm-v/routed-1.0.0.tgz' not found") {
		t.Fatalf("body %q must keep the refusal wording", body)
	}
	if strings.Contains(body, `"errors"`) {
		t.Fatalf("body %q must not render the envelope on the write-side fallback", body)
	}
}
