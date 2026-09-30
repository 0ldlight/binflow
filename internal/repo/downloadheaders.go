package repo

import (
	"net/http"
	"net/url"
	"path"
	"strconv"
	"unicode/utf8"
)

// Download-face wire header names of the disposition family (T-608 /
// BIN-90, pinned live on the reference 7.161.26 by T-601's dual-round
// probe). Kept here as the single spelling every artifact-body renderer
// consults, mirroring the adapter/mimetable single-source discipline: the
// generic and maven handlers import this file instead of growing local
// copies, and any future adapter plane that serves artifact bodies joins
// the same source. The X-Checksum-* strip entries below spell the wire
// names too — adapters keep their own constants for the faces they set.
const (
	hdrContentDisposition  = "Content-Disposition"
	hdrArtifactoryFilename = "X-Artifactory-Filename"
)

// SetDownloadDisposition renders the disposition pair every artifact-body
// download face carries (T-601 section 2, the A model, live dual-round):
// the Content-Disposition header is ALWAYS sent — an ASCII base name takes
// the dual-parameter form (quoted filename= plus an RFC 5987 filename*
// carrying the raw name), a name carrying any non-ASCII byte takes the
// filename* parameter ONLY, with no ASCII fallback (byte-identical to the
// live wire's pct-encoded spelling, uppercase percent-hex UTF-8) — and
// X-Artifactory-Filename echoes the URL-encoded base name. Both values
// derive from the SERVED path's final segment alone; an empty or
// root-only path sets nothing (folder faces never reach a body render).
//
// Status faces: 200/206 render the pair with the body descriptors, the 304
// keeps it (net/http's 304 suppression strips only Content-Type,
// Content-Length and Transfer-Encoding), and the 416 strip list removes
// it — callers do not branch on status for this pair.
func SetDownloadDisposition(hdr http.Header, relPath string) {
	name := path.Base(relPath)
	if name == "" || name == "." || name == "/" {
		return
	}
	encoded := url.PathEscape(name)
	if isASCII(name) {
		hdr.Set(hdrContentDisposition,
			`attachment; filename="`+name+`"; filename*=UTF-8''`+encoded)
	} else {
		hdr.Set(hdrContentDisposition, "attachment; filename*=UTF-8''"+encoded)
	}
	hdr.Set(hdrArtifactoryFilename, encoded)
}

// rangeNotSatisfiableStrip is the body-descriptive header family the
// reference's 416 drops (T-601 §二: the bare set keeps only Content-Range,
// Content-Length and the infra trio — Content-Type, ETag, the X-Checksum
// triple, the disposition pair, Accept-Ranges and Last-Modified all go).
// BinFlow's own X-BinFlow-* hint family is deliberately absent: F4's
// INTENTIONAL-keep ruling (T-601 §四) survives every status.
var rangeNotSatisfiableStrip = []string{
	"Content-Type",
	"ETag",
	"Last-Modified",
	"Accept-Ranges",
	hdrContentDisposition,
	hdrArtifactoryFilename,
	"X-Checksum-Md5",
	"X-Checksum-Sha1",
	"X-Checksum-Sha256",
}

// WriteRangeNotSatisfiable renders the 416 bare set: it strips every
// body-descriptive header the caller already assembled, then answers
// exactly Content-Range: bytes */<total> and Content-Length: 0 (the live
// reference's unsatisfiable-range shape, T-601 leg gl-range-oob). Callers
// hand it the response AFTER their ordinary header assembly so the strip
// stays the one place the 416 posture lives.
func WriteRangeNotSatisfiable(w http.ResponseWriter, total int64) {
	hdr := w.Header()
	for _, k := range rangeNotSatisfiableStrip {
		hdr.Del(k)
	}
	hdr.Set("Content-Range", "bytes */"+strconv.FormatInt(total, 10))
	hdr.Set("Content-Length", "0")
	w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
}

// isASCII reports whether s is entirely ASCII bytes — the disposition
// form's switch: any non-ASCII byte moves Content-Disposition to the
// filename*-only spelling.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
