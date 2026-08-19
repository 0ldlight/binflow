package maven

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Range and conditional-request semantics — the M1 download contract
// (FR-4-AC14/AC15) maven inherits verbatim (ME-02/M21). This file mirrors
// the generic adapter's conditional.go: adapters are leaves and share no
// private helpers, so the wire contract is duplicated on purpose and must
// not drift (both spellings answer identically; the maven tests pin the
// inherited cases).
//
//   - Single byte range  -> 206 + Content-Range + the exact slice.
//   - Unsatisfiable/malformed range -> 416 + "Content-Range: bytes */<total>".
//   - Multi-range and non-"bytes" units are ignored (full 200, never 5xx).
//   - If-None-Match compares weakly (bare/"quoted"/W/"weak"), and wins over
//     If-Modified-Since when both are present.

// errRangeMalformed marks a byte-range-set the parser cannot reduce to a
// single satisfiable range: 416 territory, never a 5xx.
var errRangeMalformed = errors.New("malformed range header")

// httpRangeParser carries the total entity length through one evaluation.
type httpRangeParser struct{ total int64 }

// parseRange evaluates a Range header against total: the single
// satisfiable interval, malformed=true (416), or ignore=true (full body).
func (p httpRangeParser) parseRange(header string) (r httpRange, malformed bool, ignore bool) {
	spec := strings.TrimSpace(header)
	if spec == "" {
		return httpRange{}, false, true
	}
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return httpRange{}, false, true
	}
	if !strings.EqualFold(strings.TrimSpace(spec[:eq]), "bytes") {
		return httpRange{}, false, true
	}
	set := strings.TrimSpace(spec[eq+1:])
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
	if strings.ContainsAny(first+last, " \t") || first == "" && last == "" {
		return httpRange{}, true
	}

	switch {
	case first == "": // suffix: bytes=-N
		n, err := parseUint(last)
		if err != nil {
			return httpRange{}, true
		}
		if n == 0 || p.total == 0 {
			return httpRange{}, true
		}
		if n > p.total {
			n = p.total
		}
		return httpRange{start: p.total - n, end: p.total - 1}, false
	case last == "": // open: bytes=N-
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
		if start > end || p.total == 0 || start >= p.total {
			return httpRange{}, true
		}
		if end >= p.total {
			end = p.total - 1
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
		return 0, errRangeMalformed
	}
	return n, nil
}

// httpRange is an inclusive byte interval within the entity.
type httpRange struct{ start, end int64 }

func (r httpRange) length() int64 { return r.end - r.start + 1 }

func (r httpRange) contentRange(total int64) string {
	return fmt.Sprintf("bytes %d-%d/%d", r.start, r.end, total)
}

// evalConditional implements the If-None-Match / If-Modified-Since half of
// RFC 9110 13.2.2 for GET/HEAD: true means fresh, answer 304.
func evalConditional(r *http.Request, etag string, lastModified time.Time) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		return etagMatch(inm, etag)
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !lastModified.IsZero() {
		t, err := http.ParseTime(ims)
		if err != nil {
			return false
		}
		return !lastModified.Truncate(time.Second).After(t)
	}
	return false
}

// etagMatch implements weak comparison over a comma-separated tag list.
func etagMatch(header, etag string) bool {
	if etag == "" {
		return false
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimSpace(tag)
		if tag == "*" {
			return true
		}
		if normalizeETag(tag) == etag {
			return true
		}
	}
	return false
}

// normalizeETag strips a W/ weakness prefix and DQUOTEs, lowercasing the
// remainder (the stored form is an opaque hex digest).
func normalizeETag(tag string) string {
	if len(tag) >= 2 && tag[0:2] == "W/" {
		tag = tag[2:]
	}
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		tag = tag[1 : len(tag)-1]
	}
	return strings.ToLower(strings.TrimSpace(tag))
}
