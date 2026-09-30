// BIN-70 / T-588: the generic client-checksum plane's sidecar PUT body-size
// guard (L040 Arm 3 / N5, sz-* + szb-* legs, A 7.161.26 双轮): the ceiling is
// 1024 bytes INCLUSIVE — ≤1024 passes into the ordinary comparison/registration
// flow (wrong value = 409 Checksum error WITH write-through), >1024 is refused
// with the verbatim `Suspicious checksum file, content length of N bytes is
// bigger than allowed.` (N = the exact body length) and NOTHING registered.
// Chunked bodies (no Content-Length) are caught by a bounded read.
package generic_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// suspiciousMsg is the reference's verbatim refusal wording (L040 Arm 3).
func suspiciousMsg(n int64) string {
	return fmt.Sprintf("Suspicious checksum file, content length of %d bytes is bigger than allowed.", n)
}

// hiddenReader hides the concrete reader type so net/http cannot infer a
// Content-Length — the request goes out chunked (ContentLength = -1),
// exercising the read-side arm of the guard instead of the header pre-check.
type hiddenReader struct{ r io.Reader }

func (h hiddenReader) Read(p []byte) (int, error) { return h.r.Read(p) }

// TestChecksumPutBodySizeGuard pins the guard's boundary and both arms
// against a PRESENT source (every L040 sz-*/szb-* leg ran this shape).
func TestChecksumPutBodySizeGuard(t *testing.T) {
	e := newEnv(t)
	src := "t588/src.bin"
	if resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src,
		strings.NewReader("source-bytes"), nil); resp.StatusCode != http.StatusCreated {
		t.Fatalf("source seed = %d (%s)", resp.StatusCode, body(t, resp))
	}
	_, sha1S, _ := digestsOf("source-bytes")

	// ≤1024 passes the guard: a wrong value lands in the ORDINARY 409
	// comparison arm (and still writes through, the L037/L040 model).
	passCases := []struct {
		name string
		size int
	}{
		{"1000B wrong value reaches comparison", 1000},
		{"1024B boundary passes (inclusive)", 1024},
	}
	for _, tc := range passCases {
		t.Run(tc.name, func(t *testing.T) {
			declared := strings.Repeat("a", tc.size)
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
				strings.NewReader(declared), nil)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("PUT = %d, want 409 Checksum error (body=%s)", resp.StatusCode, body(t, resp))
			}
			want := fmt.Sprintf("Checksum error for '%s': received '%s' but actual is '%s'",
				src+".sha1", declared, sha1S)
			if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
				t.Fatalf("409 body = %s\nwant message = %q", got, want)
			}
			// Write-through survives the guard's addition: the GET face
			// echoes the declared value (L040's 1000B leg, oc.sha1 written).
			get := e.do(t, http.MethodGet, "/binflow/generic-local/"+src+".sha1", nil, nil)
			if get.StatusCode != http.StatusOK || body(t, get) != declared {
				t.Errorf("GET echo = %d %q, want 200 with the written-through declared value", get.StatusCode, body(t, get))
			}
		})
	}

	// >1024 is refused with the verbatim Suspicious wording and nothing
	// registered. The last registration (1024B leg above) is overwritten by
	// nothing — asserted after the refusal legs below.
	refuseCases := []struct {
		name    string
		size    int64
		chunked bool
	}{
		{"1025B boundary refuses (header arm)", 1025, false},
		{"4000B refuses", 4000, false},
		{"64000B refuses", 64000, false},
		{"1025B chunked refuses (read arm)", 1025, true},
	}
	for _, tc := range refuseCases {
		t.Run(tc.name, func(t *testing.T) {
			var rd io.Reader = strings.NewReader(strings.Repeat("a", int(tc.size)))
			if tc.chunked {
				rd = hiddenReader{strings.NewReader(strings.Repeat("a", int(tc.size)))}
			}
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1", rd, nil)
			if resp.StatusCode != http.StatusConflict {
				t.Fatalf("PUT = %d, want 409 Suspicious (body=%s)", resp.StatusCode, body(t, resp))
			}
			want := suspiciousMsg(tc.size)
			if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
				t.Fatalf("409 body = %s\nwant message verbatim = %q", got, want)
			}
		})
	}

	// Nothing registered by any refusal: the GET face still carries the
	// 1024B leg's write-through value, not any 'a'×1025+ body.
	get := e.do(t, http.MethodGet, "/binflow/generic-local/"+src+".sha1", nil, nil)
	if get.StatusCode != http.StatusOK || body(t, get) != strings.Repeat("a", 1024) {
		t.Errorf("GET after refusals = %d %q, want the 1024B leg's registered value untouched",
			get.StatusCode, body(t, get))
	}

	// Regression: a normal SMALL correct sidecar still succeeds end to end.
	_, _, md5S := digestsOf("source-bytes")
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".md5",
		strings.NewReader(md5S), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("small correct .md5 PUT = %d (body=%s) — guard must not disturb the success arm",
			resp.StatusCode, body(t, resp))
	}
}

// TestChecksumPutBodySizeGuardDoesNotTouchOtherPlanes pins the guard's scope:
// an oversized body on a NON-checksum path is an ordinary deploy (the guard
// lives on the client-checksum plane alone), and the declared-length arm
// answers without reading the body — a 1MB Content-Length is refused from
// the header alone, so no unbounded read ever happens on this plane.
func TestChecksumPutBodySizeGuardDoesNotTouchOtherPlanes(t *testing.T) {
	e := newEnv(t)
	// Ordinary deploy of an oversized (64KB) file: 201, bytes served back.
	big := strings.Repeat("x", 64000)
	resp := e.do(t, http.MethodPut, "/binflow/generic-local/t588/big.bin", strings.NewReader(big), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ordinary oversized PUT = %d (body=%s), want 201 — guard is plane-scoped",
			resp.StatusCode, body(t, resp))
	}
	get := e.do(t, http.MethodGet, "/binflow/generic-local/t588/big.bin", nil, nil)
	if get.StatusCode != http.StatusOK || body(t, get) != big {
		t.Errorf("GET big.bin = %d (len=%d), want the full 64000 bytes", get.StatusCode, len(body(t, get)))
	}

	// Header arm on a MISSING source: the guard precedes the existence
	// probe (the maven family's seam order), so a declared 1MB body answers
	// the Suspicious 409 without the 404 miss shape — and without reading
	// the megabyte (the read is bounded to 1025 bytes by construction).
	src := "t588/never-seeded.bin"
	resp = e.do(t, http.MethodPut, "/binflow/generic-local/"+src+".sha1",
		strings.NewReader(strings.Repeat("a", 1<<20)), nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("1MB declared sidecar PUT = %d, want 409 Suspicious (body=%s)",
			resp.StatusCode, body(t, resp))
	}
	if want := suspiciousMsg(1 << 20); !strings.Contains(body(t, resp), `"`+want+`"`) {
		t.Errorf("409 body = %s\nwant message verbatim = %q", body(t, resp), want)
	}
}
