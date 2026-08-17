package generic_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// rangeFixture is a 100-byte deterministic body; its ETag (sha1) is fetched
// from a live response in the tests, never recomputed here.
const rangePath = "/binflow/generic-local/acme/range.bin"

// rangeBody builds the fixture: byte i = 'a' + i%26 keeps every offset
// addressable by eye in a failure message.
func rangeBody() string {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteByte(byte('a' + i%26))
	}
	return b.String()
}

func putRangeFixture(t *testing.T, e *env) (content, etag string, lastModified time.Time) {
	t.Helper()
	content = rangeBody()
	if resp := e.do(t, http.MethodPut, rangePath, strings.NewReader(content), nil); resp.StatusCode != 201 {
		t.Fatalf("put fixture: %d %s", resp.StatusCode, body(t, resp))
	}
	head := e.do(t, http.MethodHead, rangePath, nil, nil)
	defer head.Body.Close() //nolint:errcheck // read-only probe
	etag = head.Header.Get("ETag")
	if etag == "" {
		t.Fatal("fixture ETag missing")
	}
	lm, err := http.ParseTime(head.Header.Get("Last-Modified"))
	if err != nil {
		t.Fatalf("fixture Last-Modified: %v", err)
	}
	return content, etag, lm
}

// TestRangeRequests is the full FR-4-AC14 matrix over the wire: single
// ranges slice exactly, illegal forms get 416 + bytes */<total>, and
// unimplemented forms degrade to a full 200 instead of a 5xx.
func TestRangeRequests(t *testing.T) {
	e := newEnv(t)
	content, _, _ := putRangeFixture(t, e)
	total := fmt.Sprint(len(content))

	tests := []struct {
		name      string
		method    string
		header    string
		want      int
		wantRange string // Content-Range expectation ("" = no strict check)
		wantLen   string // Content-Length expectation ("" = no strict check)
		wantBody  string // exact body expectation when non-empty
	}{
		{name: "closed range head", method: http.MethodGet, header: "bytes=0-99", want: 206,
			wantRange: "bytes 0-99/100", wantLen: "100", wantBody: content},
		{name: "closed range first half", method: http.MethodGet, header: "bytes=0-49", want: 206,
			wantRange: "bytes 0-49/100", wantLen: "50", wantBody: content[:50]},
		{name: "single byte", method: http.MethodGet, header: "bytes=42-42", want: 206,
			wantRange: "bytes 42-42/100", wantLen: "1", wantBody: content[42:43]},
		{name: "last byte", method: http.MethodGet, header: "bytes=99-99", want: 206,
			wantRange: "bytes 99-99/100", wantLen: "1", wantBody: content[99:]},
		{name: "end clamped to total", method: http.MethodGet, header: "bytes=90-999999999", want: 206,
			wantRange: "bytes 90-99/100", wantLen: "10", wantBody: content[90:]},
		{name: "open range", method: http.MethodGet, header: "bytes=10-", want: 206,
			wantRange: "bytes 10-99/100", wantLen: "90", wantBody: content[10:]},
		{name: "open range from zero", method: http.MethodGet, header: "bytes=0-", want: 206,
			wantRange: "bytes 0-99/100", wantLen: "100", wantBody: content},
		{name: "suffix range", method: http.MethodGet, header: "bytes=-10", want: 206,
			wantRange: "bytes 90-99/100", wantLen: "10", wantBody: content[90:]},
		{name: "suffix equals total", method: http.MethodGet, header: "bytes=-100", want: 206,
			wantRange: "bytes 0-99/100", wantLen: "100", wantBody: content},
		{name: "suffix longer than total clamps", method: http.MethodGet, header: "bytes=-150", want: 206,
			wantRange: "bytes 0-99/100", wantLen: "100", wantBody: content},

		{name: "start beyond total", method: http.MethodGet, header: "bytes=100-", want: 416,
			wantRange: "bytes */100"},
		{name: "start far beyond total (curl -r 999999999-)", method: http.MethodGet, header: "bytes=999999999-", want: 416,
			wantRange: "bytes */100"},
		{name: "closed range start beyond total", method: http.MethodGet, header: "bytes=100-200", want: 416,
			wantRange: "bytes */100"},
		{name: "start equals total", method: http.MethodGet, header: "bytes=100-99", want: 416,
			wantRange: "bytes */100"},
		{name: "start greater than end", method: http.MethodGet, header: "bytes=50-10", want: 416,
			wantRange: "bytes */100"},
		{name: "non-numeric", method: http.MethodGet, header: "bytes=abc", want: 416,
			wantRange: "bytes */100"},
		{name: "non-numeric pair", method: http.MethodGet, header: "bytes=abc-def", want: 416,
			wantRange: "bytes */100"},
		{name: "non-numeric suffix", method: http.MethodGet, header: "bytes=-abc", want: 416,
			wantRange: "bytes */100"},
		{name: "zero suffix", method: http.MethodGet, header: "bytes=-0", want: 416,
			wantRange: "bytes */100"},
		{name: "double dash", method: http.MethodGet, header: "bytes=--5", want: 416,
			wantRange: "bytes */100"},
		{name: "signed start", method: http.MethodGet, header: "bytes=+10-", want: 416,
			wantRange: "bytes */100"},
		{name: "inner space", method: http.MethodGet, header: "bytes=10- 20", want: 416,
			wantRange: "bytes */100"},
		{name: "trailing junk", method: http.MethodGet, header: "bytes=1-2x", want: 416,
			wantRange: "bytes */100"},

		{name: "multi-range serves full 200", method: http.MethodGet, header: "bytes=0-49,60-69", want: 200,
			wantLen: "100", wantBody: content},
		{name: "multi-range spaced", method: http.MethodGet, header: "bytes=0-49, 60-69", want: 200,
			wantLen: "100", wantBody: content},
		{name: "non-bytes unit ignored", method: http.MethodGet, header: "items=0-5", want: 200,
			wantLen: "100", wantBody: content},
		{name: "no equal sign ignored", method: http.MethodGet, header: "bytes", want: 200,
			wantLen: "100", wantBody: content},
		{name: "empty spec ignored", method: http.MethodGet, header: "bytes=", want: 200,
			wantLen: "100", wantBody: content},

		{name: "HEAD honors 206", method: http.MethodHead, header: "bytes=0-49", want: 206,
			wantRange: "bytes 0-49/100", wantLen: "50"},
		{name: "HEAD honors 416", method: http.MethodHead, header: "bytes=100-", want: 416,
			wantRange: "bytes */100"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" ["+tt.header+"]", func(t *testing.T) {
			resp := e.do(t, tt.method, rangePath, nil, map[string]string{"Range": tt.header})
			defer resp.Body.Close() //nolint:errcheck // read-only probe
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.wantRange != "" {
				if got := resp.Header.Get("Content-Range"); got != tt.wantRange {
					t.Fatalf("Content-Range = %q, want %q", got, tt.wantRange)
				}
			}
			if tt.want == 416 && resp.Header.Get("Content-Range") != "bytes */"+total {
				t.Fatalf("416 Content-Range = %q, want bytes */%s", resp.Header.Get("Content-Range"), total)
			}
			if tt.wantLen != "" {
				if got := resp.Header.Get("Content-Length"); got != tt.wantLen {
					t.Fatalf("Content-Length = %q, want %q", got, tt.wantLen)
				}
			}
			// Body: HEAD responses and 416s have none; everything else must
			// match the exact slice (HEAD wantBody rows are absent by design).
			b := body(t, resp)
			if tt.method == http.MethodHead || tt.want == 416 {
				if b != "" {
					t.Fatalf("body must be empty, got %q", b)
				}
				return
			}
			if tt.wantBody != "" && b != tt.wantBody {
				t.Fatalf("body = %q (len %d), want %q (len %d)", b, len(b), tt.wantBody, len(tt.wantBody))
			}
		})
	}
}

// TestConditionalRequests is the FR-4-AC15 wire matrix: If-None-Match in its
// three spellings plus "*", If-Modified-Since both sides of Last-Modified,
// precedence when both are present, and 304 carrying no body.
func TestConditionalRequests(t *testing.T) {
	e := newEnv(t)
	_, etag, lm := putRangeFixture(t, e)
	other := strings.Repeat("0", 40)
	before := lm.Add(-time.Hour).Format(http.TimeFormat)
	after := lm.Add(time.Hour).Format(http.TimeFormat)

	tests := []struct {
		name   string
		method string
		hdr    map[string]string
		want   int
	}{
		{name: "inm bare hit", method: http.MethodGet, hdr: map[string]string{"If-None-Match": etag}, want: 304},
		{name: "inm quoted hit", method: http.MethodGet, hdr: map[string]string{"If-None-Match": `"` + etag + `"`}, want: 304},
		{name: "inm weak hit", method: http.MethodGet, hdr: map[string]string{"If-None-Match": `W/"` + etag + `"`}, want: 304},
		{name: "inm star hit", method: http.MethodGet, hdr: map[string]string{"If-None-Match": "*"}, want: 304},
		{name: "inm miss serves 200", method: http.MethodGet, hdr: map[string]string{"If-None-Match": other}, want: 200},
		{name: "inm weak miss serves 200", method: http.MethodGet, hdr: map[string]string{"If-None-Match": `W/"` + other + `"`}, want: 200},
		{name: "inm list hit", method: http.MethodGet, hdr: map[string]string{"If-None-Match": `"x", W/"` + etag + `"`}, want: 304},
		{name: "inm list miss", method: http.MethodGet, hdr: map[string]string{"If-None-Match": `"x", "y"`}, want: 200},

		{name: "ims before Last-Modified serves 200", method: http.MethodGet, hdr: map[string]string{"If-Modified-Since": before}, want: 200},
		{name: "ims equal to Last-Modified is 304", method: http.MethodGet, hdr: map[string]string{"If-Modified-Since": lm.Format(http.TimeFormat)}, want: 304},
		{name: "ims after Last-Modified is 304", method: http.MethodGet, hdr: map[string]string{"If-Modified-Since": after}, want: 304},
		{name: "ims unparseable serves 200", method: http.MethodGet, hdr: map[string]string{"If-Modified-Since": "yesterday"}, want: 200},

		{name: "inm miss beats fresh ims (200)", method: http.MethodGet,
			hdr: map[string]string{"If-None-Match": other, "If-Modified-Since": after}, want: 200},
		{name: "inm hit beats stale ims (304)", method: http.MethodGet,
			hdr: map[string]string{"If-None-Match": etag, "If-Modified-Since": before}, want: 304},

		{name: "HEAD inm hit 304", method: http.MethodHead, hdr: map[string]string{"If-None-Match": etag}, want: 304},
		{name: "HEAD ims hit 304", method: http.MethodHead, hdr: map[string]string{"If-Modified-Since": after}, want: 304},
		{name: "HEAD inm miss 200", method: http.MethodHead, hdr: map[string]string{"If-None-Match": other}, want: 200},

		{name: "inm hit with range still 304 (condition precedes range)", method: http.MethodGet,
			hdr: map[string]string{"If-None-Match": etag, "Range": "bytes=0-9"}, want: 304},
		{name: "inm miss with range serves 206", method: http.MethodGet,
			hdr: map[string]string{"If-None-Match": other, "Range": "bytes=0-9"}, want: 206},
		{name: "ims hit with range still 304", method: http.MethodGet,
			hdr: map[string]string{"If-Modified-Since": after, "Range": "bytes=0-9"}, want: 304},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := e.do(t, tt.method, rangePath, nil, tt.hdr)
			defer resp.Body.Close() //nolint:errcheck // probe
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			b := body(t, resp)
			if tt.want == 304 && b != "" {
				t.Fatalf("304 body must be empty, got %q", b)
			}
			if tt.want == 304 {
				if et := resp.Header.Get("ETag"); et != "" && et != etag {
					t.Fatalf("304 ETag = %q, want %q or absent", et, etag)
				}
			}
		})
	}
}

// TestRange416EnvelopeShape pins the 416 wire shape end to end: the client
// never gets a partial body, and Content-Range is the Artifactory form.
func TestRange416EnvelopeShape(t *testing.T) {
	e := newEnv(t)
	content, _, _ := putRangeFixture(t, e)
	resp := e.do(t, http.MethodGet, rangePath, nil, map[string]string{"Range": "bytes=999999999-"})
	b := body(t, resp)
	if resp.StatusCode != 416 {
		t.Fatalf("status = %d, want 416", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Range"); got != "bytes */"+fmt.Sprint(len(content)) {
		t.Fatalf("Content-Range = %q", got)
	}
	if strings.HasPrefix(b, content[:1]) && len(b) == int(len(content)) {
		t.Fatal("416 leaked the full body")
	}
}
