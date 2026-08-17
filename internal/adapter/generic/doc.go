// Package generic implements the raw/binflow content-path adapter: PUT/GET/
// HEAD/DELETE over /binflow/<repo>/<path> with Artifactory-compatible
// semantics (rest-api.md section 1, high confidence). It is the M1
// reference implementation of the adapter SPI.
//
// # Range and conditional requests (FR-4-AC14/AC15)
//
// Downloads honor single-range and conditional requests per RFC 9110 with
// Artifactory's error shapes (rest-api.md 1.4):
//
//   - One satisfiable byte range ("bytes=0-99", "bytes=10-", "bytes=-10")
//     answers 206 with Content-Range and exactly that slice; an end beyond
//     the entity clamps to it.
//   - An unsatisfiable or malformed spec (start >= total, start > end,
//     non-numeric, zero-length suffix) answers 416 with
//     "Content-Range: bytes */<total>" and no body.
//   - Multi-range sets and non-"bytes" units are NOT implemented: the
//     header is ignored and the full 200 body is served — never a 5xx.
//   - If-None-Match compares weakly: BinFlow stores the ETag as a bare
//     sha1, and bare, "quoted" and W/"weak" client spellings all match.
//     If-None-Match wins over If-Modified-Since when both are present.
//     A hit answers 304 with no body, on GET and HEAD alike.
package generic
