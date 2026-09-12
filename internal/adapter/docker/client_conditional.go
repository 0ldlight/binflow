package docker

// The remote read face's CLIENT-side conditional semantics (L004-1, live
// reference :8082 Artifactory-pro 7.161.20 — the overturn of L000-B E3-2's
// "If-None-Match 恒不被消费"; the D1 verdict of LOOP 005, authority
// reports/compatibility/L004-304-ping-diff.md §2): a GET against a VALID
// cached copy answers 304 when the client's validator matches, and the two
// read faces evaluate preconditions IDENTICALLY — the If-None-Match
// comparison is by VALUE, quote- and weak-prefix-insensitive (the unquoted
// sha1 spelling matches, the M2/M2d arms), and ANY present If-None-Match —
// matching or not, quoted or not — seals the If-Modified-Since arm (the
// M4b/M2b/M2c arms). Only the 304 RESPONSE shapes differ: the manifest face
// answers a BARE 304 (the api-version header alone), the blob face a 304
// carrying the FULL artifact face. HEAD never answers 304 on either face,
// and the expired-window revalidation serves never evaluate client
// conditionals (they answer 200 full — the copy the client holds was just
// reconfirmed upstream, a different arm than the fresh-window conditional).
//
// D1 history: the first cut parsed the manifest face's If-None-Match as a
// QUOTED-tag-only list and dropped unquoted spellings onto the date arm —
// live-falsified (M2/M2c/M2d were the three DIVERGENT arms of the 18-arm
// matrix; real docker/containerd/oras clients send quoted etags, so the
// impact was matrix-observable rather than client-felt). The verdict and
// the overturning capture live in reports/compatibility/L004-304-ping-diff.md
// §1/§2/§5.1 (divergence D1); this file implements it.

import (
	"net/http"
	"strings"
)

// clientConditionalNotModified evaluates the GET preconditions both read
// faces share (the D1 verdict: the faces differ only in their 304 response
// shape, not in evaluation): etag is the face's served validator (the
// unquoted sha1; "" when the ledger has no row — then only the date arm
// can answer), lastModified its Last-Modified spelling. A present
// If-None-Match compares token-by-token on the inner value (quotes and the
// W/ prefix stripped — the unquoted spelling matches), and — matching or
// not — blocks the If-Modified-Since arm entirely.
func clientConditionalNotModified(r *http.Request, etag, lastModified string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if etag == "" {
			return false
		}
		for _, tok := range strings.Split(inm, ",") {
			v := strings.TrimSpace(tok)
			v = strings.TrimPrefix(v, "W/")
			v = strings.Trim(v, `"`)
			if v == etag {
				return true
			}
		}
		return false // a present non-matching list: the date arm is sealed
	}
	return notModifiedSince(r.Header.Get("If-Modified-Since"), lastModified)
}

// notModifiedSince is the shared date arm: not-modified when the copy's
// Last-Modified is not after the request's If-Modified-Since (a future
// date trivially satisfies it, the live capture's 2027 leg).
func notModifiedSince(ims, lastModified string) bool {
	if ims == "" || lastModified == "" {
		return false
	}
	t, err := http.ParseTime(ims)
	if err != nil {
		return false
	}
	lm, err := http.ParseTime(lastModified)
	if err != nil {
		return false
	}
	return !lm.After(t)
}
