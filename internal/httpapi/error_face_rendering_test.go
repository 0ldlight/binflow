package httpapi_test

// T-620 (BIN-102): the error rendering plane family. The shared envelope
// seam answers with the reference's wire faces, calibrated by the T-615
// live leg set against Artifactory 7.161.26:
//
//   - repo-path CONTENT plane errors and every 401 arm (cross-plane):
//     Content-Type "application/json;charset=ISO-8859-1" (no space);
//   - protocol/management API planes (/api/**): bare "application/json";
//   - the body layout is Jackson pretty, byte-exact: a space before
//     every colon, the entry's braces hugging the array brackets, no
//     trailing newline, and < > & emitted raw (L009-3).
//
// The registry spec plane (/v2) renders its own bodies in the docker
// adapter and never passes the envelope — its posture is pinned by the
// docker adapter tests, not here.

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// jacksonErrorsBody is the envelope's exact byte form for one entry —
// the same layout the reference's Jackson pretty printer emits.
func jacksonErrorsBody(status int, message string) string {
	return "{\n  \"errors\" : [ {\n    \"status\" : " + strconv.Itoa(status) +
		",\n    \"message\" : \"" + message + "\"\n  } ]\n}"
}

const ctJSONCharset = "application/json;charset=ISO-8859-1"

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestErrorFacePlaneMatrix(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	tests := []struct {
		name       string
		method     string
		path       string
		user       string
		pass       string
		wantCT     string
		wantBody   string // byte-exact when non-empty
		layoutOnly bool   // assert status+CT+layout markers, not the message
	}{
		{
			// T-615 leg x-norepo-get: the content plane's repository-miss
			// 404 rides the charset face.
			name:   "content plane repo miss 404 carries charset",
			method: http.MethodGet,
			path:   "/binflow/difftest-no-such-repo-t620/missing.bin",
			user:   adminUser,
			pass:   adminPass,
			wantCT: ctJSONCharset,
			wantBody: jacksonErrorsBody(http.StatusNotFound,
				"Failed to find the repository 'difftest-no-such-repo-t620' specified in the request."),
		},
		{
			// T-615 leg x-norepo-head: HEAD keeps the face, the body is
			// discarded by net/http (framing itself is INTENTIONAL drift).
			name:     "content plane HEAD 404 same face empty body",
			method:   http.MethodHead,
			path:     "/binflow/difftest-no-such-repo-t620/missing.bin",
			user:     adminUser,
			pass:     adminPass,
			wantCT:   ctJSONCharset,
			wantBody: "",
		},
		{
			// T-615 leg c-auth401: presented-but-refused credentials on the
			// content plane take the 401 arm — charset regardless of plane.
			name:   "content plane bad credentials 401 carries charset",
			method: http.MethodGet,
			path:   "/binflow/generic-local/missing.txt",
			user:   "difftest-no-such-user-t620",
			pass:   "whatever",
			wantCT: ctJSONCharset,
			wantBody: jacksonErrorsBody(http.StatusUnauthorized,
				"invalid credentials"),
		},
		{
			// T-615 leg x-api-anon401: the 401 arm is cross-plane — the
			// management API's anonymous refusal also carries charset.
			name:       "api plane anonymous 401 carries charset",
			method:     http.MethodGet,
			path:       "/binflow/api/repositories",
			wantCT:     ctJSONCharset,
			layoutOnly: true,
		},
		{
			// The bare face survives on the API plane: E-26's unimplemented
			// 404 keeps the bare media type.
			name:   "api plane unimplemented 404 stays bare",
			method: http.MethodGet,
			path:   "/binflow/api/v9/none",
			user:   adminUser,
			pass:   adminPass,
			wantCT: "application/json",
			wantBody: jacksonErrorsBody(http.StatusNotFound,
				"/binflow/api/v9/none is not implemented in BinFlow"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := h.do(tt.method, tt.path, tt.user, tt.pass, nil, nil)
			defer drain(resp)
			if got := resp.Header.Get("Content-Type"); got != tt.wantCT {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantCT)
			}
			body := readBody(t, resp)
			if tt.wantBody != "" && body != tt.wantBody {
				t.Fatalf("body = %q,\nwant  %q", body, tt.wantBody)
			}
			if tt.layoutOnly || tt.wantBody != "" {
				if !strings.HasPrefix(body, "{\n  \"errors\" : [ {\n") ||
					!strings.HasSuffix(body, "\n  } ]\n}") {
					t.Fatalf("body %q is not the Jackson pretty envelope", body)
				}
			}
		})
	}
}

// TestErrorFaceJacksonRawAmpersandPin pins L009-3 inside the new layout:
// the reference's Jackson serializer emits & raw; Go's default JSON HTML
// escaping would rewrite it to the &-escape sequence. The storage item
// miss echoes the requested path, and & survives URL path spelling raw.
func TestErrorFaceJacksonRawAmpersandPin(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	resp := h.do(http.MethodGet, "/binflow/api/storage/generic-local/sub/a&b-missing.txt",
		adminUser, adminPass, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want the item-miss 404 (body %s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want the bare api-plane face", got)
	}
	if !strings.Contains(body, "a&b-missing.txt") || !strings.Contains(body, `"message" : "`) {
		t.Fatalf("body %q must carry the path and the Jackson layout raw", body)
	}
	if strings.Contains(body, "\\u0026") {
		t.Fatalf("body %q must not HTML-escape the ampersand", body)
	}
}
