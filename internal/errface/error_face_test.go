package errface

// The error face's byte contract, pinned independently of the httpapi seam
// tests (the leaf carries its own guard so a regression here fails here
// first): the Jackson pretty layout against the T-615 live captures, the
// plane matrix against the leg set, and the raw-& pin (L009-3).

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRenderMatchesT615Capture pins the body layout byte-for-byte against
// the raw capture ar1-h-tgz-get.body (132 bytes, Artifactory 7.161.26):
// a space before every colon, the entry's braces hugging the array
// brackets, no trailing newline.
func TestRenderMatchesT615Capture(t *testing.T) {
	got := Render(404, "File not found.; Path: 'difftest-t615-helm:missing-chart-9.9.9.tgz'")
	want := "{\n  \"errors\" : [ {\n    \"status\" : 404,\n" +
		"    \"message\" : \"File not found.; Path: 'difftest-t615-helm:missing-chart-9.9.9.tgz'\"\n  } ]\n}"
	if got != want {
		t.Fatalf("Render = %q,\nwant   %q", got, want)
	}
	if n := len(got); n != 132 {
		t.Fatalf("Render length = %d, want 132 (the capture's byte count)", n)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("Render must not end in a newline")
	}
}

// TestRenderRawAmpersand pins L009-3: < > & ride the message raw — Go's
// default JSON HTML escaping must not rewrite them.
func TestRenderRawAmpersand(t *testing.T) {
	got := Render(400, "Illegal characters in path <a&b>. Only 'a-z A-Z 0-9 . - _' are accepted.")
	for _, raw := range []string{"&", "<a", "b>"} {
		if !strings.Contains(got, raw) {
			t.Fatalf("Render %q must carry %s raw", got, raw)
		}
	}
	for _, esc := range []string{"\\u0026", "\\u003c", "\\u003e"} {
		if strings.Contains(got, esc) {
			t.Fatalf("Render %q must not HTML-escape (%s found)", got, esc)
		}
	}
}

// TestContentTypePlaneMatrix pins the media-type matrix: content plane and
// 401 arms answer the charset spelling, every other plane stays bare.
func TestContentTypePlaneMatrix(t *testing.T) {
	tests := []struct {
		name         string
		contentPlane bool
		status       int
		want         string
	}{
		{"content plane 404", true, http.StatusNotFound, Charset},
		{"content plane 400", true, http.StatusBadRequest, Charset},
		{"api plane 404", false, http.StatusNotFound, Plain},
		{"api plane 400", false, http.StatusBadRequest, Plain},
		{"401 arm on api plane", false, http.StatusUnauthorized, Charset},
		{"401 arm on content plane", true, http.StatusUnauthorized, Charset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContentType(tt.contentPlane, tt.status); got != tt.want {
				t.Fatalf("ContentType(%v, %d) = %q, want %q",
					tt.contentPlane, tt.status, got, tt.want)
			}
		})
	}
}

// TestWriteEmitsPlaneFace pins the combined emission: media type per the
// matrix, status verbatim, body the Jackson layout.
func TestWriteEmitsPlaneFace(t *testing.T) {
	tests := []struct {
		name         string
		contentPlane bool
		wantCT       string
	}{
		{"content plane", true, Charset},
		{"api plane", false, Plain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Write(rec, http.StatusNotFound, "File not found.; Path: 'r:p'", tt.contentPlane)
			resp := rec.Result()
			if got := resp.Header.Get("Content-Type"); got != tt.wantCT {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantCT)
			}
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", resp.StatusCode)
			}
			if body := rec.Body.String(); body != Render(http.StatusNotFound, "File not found.; Path: 'r:p'") {
				t.Fatalf("body = %q, want the Render layout", body)
			}
		})
	}
}
