package pypi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Range and conditional-request semantics for downloads — the M1 download
// contract (rest-api.md section 1.4, FR-4-AC14/AC15), ported from the
// generic adapter so PyPI distribution paths behave byte-identically to the
// content plane (PE-03 "reuse the M1 download chain"):
//
//   - Single byte range  -> 206 + Content-Range + the exact slice.
//   - Unsatisfiable range (start >= total, malformed numbers, start > end,
//     empty suffix, unsatisfiable-at-zero) -> 416 with
//     Content-Range: bytes */<total>.
//   - Non-"bytes" units and multi-range sets are NOT implemented -> the
//     header is ignored and the full 200 body is served (never 5xx).
//
// The If-None-Match comparison is weak (RFC 9110 13.1.2): the stored ETag
// is an unquoted digest, but clients quote it or prefix W/ with abandon.
// If-None-Match wins over If-Modified-Since when both are present.

// errRangeMalformed marks a byte-range-set the parser cannot reduce to a
// single satisfiable range: anything from here is a 416, never a 5xx.
var errRangeMalformed = errors.New("malformed range header")

// httpRangeParser carries the total length through one header evaluation.
type httpRangeParser struct{ total int64 }

// parseRange evaluates a Range header value against total. It returns the
// single satisfiable [start,end] (inclusive, both bounded < total), or
// malformed=true when the header names an unsatisfiable/malformed range
// (caller answers 416), or ignore=true when the header should be ignored
// (multi-range, non-bytes unit, absent, empty spec) and the full 200 body
// served.
func (p httpRangeParser) parseRange(header string) (r httpRange, malformed bool, ignore bool) {
	spec := strings.TrimSpace(header)
	if spec == "" {
		return httpRange{}, false, true
	}
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return httpRange{}, false, true // no unit at all: ignore, full body
	}
	if !strings.EqualFold(strings.TrimSpace(spec[:eq]), "bytes") {
		return httpRange{}, false, true // other units are ignored
	}
	set := strings.TrimSpace(spec[eq+1:])
	// An empty byte-range-set (bytes=) is an invalid ranges-specifier; a
	// server MAY ignore it (RFC 9110 14.2). Multi-range sets are out of
	// scope for the M1 contract.
	if set == "" || strings.ContainsRune(set, ',') {
		return httpRange{}, false, true
	}
	r, malformed = p.parseOne(set)
	return r, malformed, false
}

// parseOne reduces one byte-range-spec to its interval.
func (p httpRangeParser) parseOne(spec string) (httpRange, bool) {
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return httpRange{}, true
	}
	first, last := spec[:dash], spec[dash+1:]
	// Inner whitespace (bytes=10- 20) breaks the integer tokens; whitespace
	// around the whole header and around "=" is tolerated as OWS.
	if strings.ContainsAny(first+last, " \t") || first == "" && last == "" {
		return httpRange{}, true
	}

	switch {
	case first == "": // suffix: bytes=-N, the final N bytes
		n, err := parseUint(last)
		if err != nil {
			return httpRange{}, true
		}
		if n == 0 || p.total == 0 {
			return httpRange{}, true // zero-length suffix, or an empty entity
		}
		if n > p.total {
			n = p.total // suffix longer than the entity: whole body, still 206
		}
		return httpRange{start: p.total - n, end: p.total - 1}, false
	case last == "": // open: bytes=N-, from N to EOF (bounded by total)
		start, err := parseUint(first)
		if err != nil {
			return httpRange{}, true
		}
		if p.total == 0 || start >= p.total {
			return httpRange{}, true
		}
		return httpRange{start: start, end: p.total - 1}, false
	default:
		start, err := parseUint(first)
		if err != nil {
			return httpRange{}, true
		}
		end, err := parseUint(last)
		if err != nil {
			return httpRange{}, true
		}
		if start > end {
			return httpRange{}, true // bytes=50-10 has no satisfiable reading
		}
		if p.total == 0 || start >= p.total {
			return httpRange{}, true // bytes=100-99 on a 100-byte body
		}
		if end >= p.total {
			end = p.total - 1 // bytes=0-999999999 clamps to the entity
		}
		return httpRange{start: start, end: end}, false
	}
}

// parseUint parses a non-negative decimal with no sign, spaces or overflow.
func parseUint(s string) (int64, error) {
	if s == "" {
		return 0, errRangeMalformed
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, errRangeMalformed
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errRangeMalformed // > int64: no honest slice exists
	}
	return n, nil
}

// httpRange is an inclusive byte interval within the entity.
type httpRange struct{ start, end int64 }

func (r httpRange) length() int64 { return r.end - r.start + 1 }

// contentRange renders the 206 header value "bytes <start>-<end>/<total>".
func (r httpRange) contentRange(total int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, total)
}

// evalConditional implements the If-None-Match / If-Modified-Since half of
// RFC 9110 13.2.2 for GET/HEAD. Precedence when both headers are present:
// If-None-Match is evaluated and If-Modified-Since is ignored. Returns true
// when the store is fresh for this client: answer 304.
func evalConditional(r *http.Request, etag string, lastModified time.Time) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return etagMatch(inm, etag)
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !lastModified.IsZero() {
		t, err := http.ParseTime(ims)
		if err != nil {
			return false // unparseable date: serve 200
		}
		// Truncation to whole seconds mirrors Last-Modified's granularity.
		return !lastModified.Truncate(time.Second).After(t)
	}
	return false
}

// etagMatch implements weak comparison over a comma-separated entity-tag
// list; "*" matches any existing representation. Used by both the download
// chain (unquoted sha1) and the simple index (quoted sha256) — the tag
// spelling is normalized before comparison either way.
func etagMatch(header, etag string) bool {
	if etag == "" {
		return false
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if normalizeETag(tag) == normalizeETag(etag) {
			return true
		}
	}
	return false
}

// normalizeETag strips a W/ weakness prefix and DQUOTEs, lowercasing the
// remainder: the stored forms are opaque hex digests, so this is token
// shaping, not case folding of a meaningful field.
func normalizeETag(tag string) string {
	if len(tag) >= 2 && tag[0:2] == "W/" {
		tag = tag[2:]
	}
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	return strings.ToLower(strings.TrimSpace(tag))
}
