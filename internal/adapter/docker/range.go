package docker

import (
	"strconv"
	"strings"
)

// The byte-range slice of the blob read path (DE-06/FR-8-AC8). This is the
// M1 conditional/range base (generic/conditional.go) restated for the docker
// plane: the generic implementation is package-private and its semantics
// are the documented contract, so the same rules ship here rather than a
// cross-package export that would couple the two adapters' HTTP surfaces.
//
// Contract (identical to M1's, which the AC pins as "复用 M1 条件请求基建"):
//   - single satisfiable range -> 206 + Content-Range + the exact slice;
//   - unsatisfiable (start >= total, malformed numbers, start > end, empty
//     suffix, unsatisfiable-at-zero) -> 416 with "bytes */<total>";
//   - non-"bytes" units and multi-range sets are ignored (full 200 body).

// byteRange is an inclusive byte interval within the entity.
type byteRange struct{ start, end int64 }

func (r byteRange) length() int64 { return r.end - r.start + 1 }

// contentRange renders the 206 header value "bytes <start>-<end>/<total>".
func (r byteRange) contentRange(total int64) string {
	return "bytes " + strconv.FormatInt(r.start, 10) + "-" +
		strconv.FormatInt(r.end, 10) + "/" + strconv.FormatInt(total, 10)
}

// rangeParser carries the total through one header evaluation.
type rangeParser struct{ total int64 }

// parseRange evaluates a Range header against the total. malformed=true
// means 416; ignore=true means serve the full body (absent, other units,
// multi-range, empty set).
func (p rangeParser) parseRange(header string) (r byteRange, malformed, ignore bool) {
	spec := strings.TrimSpace(header)
	if spec == "" {
		return byteRange{}, false, true
	}
	eq := strings.IndexByte(spec, '=')
	if eq < 0 {
		return byteRange{}, false, true
	}
	if !strings.EqualFold(strings.TrimSpace(spec[:eq]), "bytes") {
		return byteRange{}, false, true
	}
	set := strings.TrimSpace(spec[eq+1:])
	if set == "" || strings.ContainsRune(set, ',') {
		return byteRange{}, false, true
	}
	r, malformed = p.parseOne(set)
	return r, malformed, false
}

// parseOne reduces one byte-range-spec to its interval; the bool marks a
// malformed/unsatisfiable spec.
func (p rangeParser) parseOne(spec string) (byteRange, bool) {
	dash := strings.IndexByte(spec, '-')
	if dash < 0 {
		return byteRange{}, true
	}
	first, last := spec[:dash], spec[dash+1:]
	if strings.ContainsAny(first+last, " \t") || first == "" && last == "" {
		return byteRange{}, true
	}
	switch {
	case first == "": // suffix bytes=-N
		n, err := parseUint64(last)
		if err != nil {
			return byteRange{}, true
		}
		if n == 0 || p.total == 0 {
			return byteRange{}, true
		}
		if n > p.total {
			n = p.total
		}
		return byteRange{start: p.total - n, end: p.total - 1}, false
	case last == "": // open bytes=N-
		start, err := parseUint64(first)
		if err != nil {
			return byteRange{}, true
		}
		if p.total == 0 || start >= p.total {
			return byteRange{}, true
		}
		return byteRange{start: start, end: p.total - 1}, false
	default:
		start, err := parseUint64(first)
		if err != nil {
			return byteRange{}, true
		}
		end, err := parseUint64(last)
		if err != nil {
			return byteRange{}, true
		}
		if start > end || p.total == 0 || start >= p.total {
			return byteRange{}, true
		}
		if end >= p.total {
			end = p.total - 1
		}
		return byteRange{start: start, end: end}, false
	}
}
