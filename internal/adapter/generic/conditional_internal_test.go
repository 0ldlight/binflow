package generic

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The parser matrix is the single-row decision table behind FR-4-AC14: every
// Range form the spec or a client has been observed to send, with the total
// fixed at 100 bytes.
func TestParseRangeMatrix(t *testing.T) {
	const total = int64(100)
	tests := []struct {
		header    string
		malformed bool
		ignore    bool
		start     int64
		end       int64
	}{
		{header: "bytes=0-99", start: 0, end: 99},
		{header: "bytes=0-0", start: 0, end: 0},
		{header: "bytes=99-99", start: 99, end: 99},
		{header: "bytes=10-", start: 10, end: 99},
		{header: "bytes=0-", start: 0, end: 99},
		{header: "bytes=-10", start: 90, end: 99},
		{header: "bytes=-100", start: 0, end: 99},
		{header: "bytes=-150", start: 0, end: 99}, // suffix longer than entity clamps
		{header: "bytes=0-999999999", start: 0, end: 99},
		{header: "bytes=9999999999999999999999-", malformed: true}, // > int64
		{header: "bytes=100-", malformed: true},                    // start >= total
		{header: "bytes=100-99", malformed: true},
		{header: "bytes=100-200", malformed: true},
		{header: "bytes=50-10", malformed: true},
		{header: "bytes=abc", malformed: true},
		{header: "bytes=abc-def", malformed: true},
		{header: "bytes=-abc", malformed: true},
		{header: "bytes=-0", malformed: true},
		{header: "bytes=--5", malformed: true},
		{header: "bytes=+10-", malformed: true},
		{header: "bytes=10- 20", malformed: true},
		{header: "bytes =0-5", start: 0, end: 5}, // OWS around "=" tolerated
		{header: "bytes=1-2x", malformed: true},
		{header: "bytes=0-49,60-69", ignore: true}, // multi-range not implemented
		{header: "bytes=0-49, 60-69", ignore: true},
		{header: "items=0-5", ignore: true}, // non-bytes unit
		{header: "chunks=0-5", ignore: true},
		{header: "bytes", ignore: true},  // no "=" at all
		{header: "bytes=", ignore: true}, // empty byte-range-set
		{header: "", ignore: true},
		{header: "   ", ignore: true},
	}
	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			rng, malformed, ignore := httpRangeParser{total: total}.parseRange(tt.header)
			if malformed != tt.malformed || ignore != tt.ignore {
				t.Fatalf("parseRange(%q) malformed=%v ignore=%v, want %v/%v",
					tt.header, malformed, ignore, tt.malformed, tt.ignore)
			}
			if !malformed && !ignore {
				if rng.start != tt.start || rng.end != tt.end {
					t.Fatalf("parseRange(%q) = [%d,%d], want [%d,%d]",
						tt.header, rng.start, rng.end, tt.start, tt.end)
				}
				if rng.length() <= 0 || rng.end >= total {
					t.Fatalf("range [%d,%d] not a satisfiable slice of %d", rng.start, rng.end, total)
				}
			}
		})
	}
}

// On an empty entity every byte-range-spec is unsatisfiable: RFC 9110's
// first-byte-pos rules have no valid position in a 0-length representation,
// and even a positive suffix has nothing to select.
func TestParseRangeEmptyEntity(t *testing.T) {
	for _, header := range []string{"bytes=0-", "bytes=0-0", "bytes=0-99", "bytes=-5", "bytes=-0"} {
		_, malformed, ignore := httpRangeParser{total: 0}.parseRange(header)
		if !malformed || ignore {
			t.Fatalf("parseRange(%q) on empty entity: malformed=%v ignore=%v, want true/false", header, malformed, ignore)
		}
	}
}

func TestETagMatchForms(t *testing.T) {
	const etag = "356a192b7913b04c54574d18c28d46e6395428ab" // sha1("1")
	tests := []struct {
		header string
		want   bool
	}{
		{header: etag, want: true},                                 // bare (BinFlow's own form)
		{header: `"` + etag + `"`, want: true},                     // strong quoted
		{header: `W/"` + etag + `"`, want: true},                   // weak (tolerated, rest-api.md 1.4)
		{header: "*", want: true},                                  // any current representation
		{header: `"` + strings.Repeat("0", 40) + `"`, want: false}, // quoted mismatch
		{header: strings.Repeat("0", 40), want: false},             // bare mismatch
		{header: `W/"` + strings.Repeat("0", 40) + `"`, want: false},
		{header: `"aaa", "` + etag + `"`, want: true},        // list, later tag
		{header: `"aaa", W/"` + etag + `", bbb`, want: true}, // list, weak middle
		{header: "", want: false},                            // empty header value
		{header: etag[:39], want: false},                     // truncation is not a prefix match
	}
	for _, tt := range tests {
		if got := etagMatch(tt.header, etag); got != tt.want {
			t.Errorf("etagMatch(%q, %s) = %v, want %v", tt.header, etag, got, tt.want)
		}
	}
	if etagMatch("*", "") {
		t.Error(`etagMatch("*", "") must be false: no ETag means no match, not wildcard hit`)
	}
}

func TestNormalizeETag(t *testing.T) {
	const v = "abc123"
	for in, want := range map[string]string{
		v:               v,
		`"` + v + `"`:   v,
		`W/"` + v + `"`: v,
		"  " + v + " ":  v,
		`W/"ABC"`:       "abc",
		`"`:             `"`, // unterminated quote stays literal
		`W/`:            "",  // bare W/ with nothing to weaken strips to empty
	} {
		if got := normalizeETag(in); got != want {
			t.Errorf("normalizeETag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvalConditional(t *testing.T) {
	const etag = "356a192b7913b04c54574d18c28d46e6395428ab"
	lm := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	before := lm.Add(-24 * time.Hour).Format(http.TimeFormat)
	after := lm.Add(24 * time.Hour).Format(http.TimeFormat)
	same := lm.Format(http.TimeFormat)
	tests := []struct {
		name string
		inm  string
		ims  string
		want bool
	}{
		{name: "no conditional headers"},
		{name: "inm bare hit", inm: etag, want: true},
		{name: "inm quoted hit", inm: `"` + etag + `"`, want: true},
		{name: "inm weak hit", inm: `W/"` + etag + `"`, want: true},
		{name: "inm star", inm: "*", want: true},
		{name: "inm miss", inm: strings.Repeat("0", 40)},
		{name: "ims before last-modified serves 200", ims: before},
		{name: "ims equal to last-modified is 304", ims: same, want: true},
		{name: "ims after last-modified is 304", ims: after, want: true},
		{name: "ims unparseable serves 200", ims: "not a date"},
		{name: "inm miss overrides fresh ims (200)", inm: strings.Repeat("0", 40), ims: after},
		{name: "inm hit overrides stale ims (304)", inm: etag, ims: before, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest(http.MethodGet, "/x", nil)
			if tt.inm != "" {
				r.Header.Set("If-None-Match", tt.inm)
			}
			if tt.ims != "" {
				r.Header.Set("If-Modified-Since", tt.ims)
			}
			if got := evalConditional(r, etag, lm); got != tt.want {
				t.Fatalf("evalConditional = %v, want %v", got, tt.want)
			}
		})
	}

	t.Run("ims with zero last-modified serves 200", func(t *testing.T) {
		r, _ := http.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("If-Modified-Since", after)
		if evalConditional(r, etag, time.Time{}) {
			t.Fatal("zero Last-Modified must not produce 304")
		}
	})
	t.Run("inm with empty etag serves 200", func(t *testing.T) {
		r, _ := http.NewRequest(http.MethodGet, "/x", nil)
		r.Header.Set("If-None-Match", `"`+etag+`"`)
		if evalConditional(r, "", lm) {
			t.Fatal("no ETag known must not produce 304")
		}
	})
}
