package docker

// The remote read face's CLIENT-side conditional semantics (L004-1, live
// reference :8082 Artifactory-pro 7.161.20 — the overturn of L000-B E3-2's
// "If-None-Match 恒不被消费"): a GET against a VALID cached copy answers
// 304 when the client's validator matches, and the two faces evaluate
// preconditions differently — the manifest face parses If-None-Match as an
// entity-tag list (a tag must be QUOTED, W/ tolerated, compared on the
// inner value; an unquoted or `*` spelling parses to zero tags and is
// DROPPED, falling through to If-Modified-Since; a present non-matching
// list answers 200 with the date precondition ignored) and answers a BARE
// 304 (the api-version header alone); the blob face compares tokens
// quote-insensitively (an unquoted etag matches), a present If-None-Match
// of any spelling blocks If-Modified-Since entirely, and the 304 keeps the
// FULL artifact face. HEAD never answers 304 on either face, and the
// expired-window revalidation serves never evaluate client conditionals
// (they answer 200 full — the copy the client holds was just reconfirmed
// upstream, a different arm than the fresh-window conditional).

import (
	"net/http"
	"strings"
)

// manifestClientNotModified evaluates the manifest face's GET
// preconditions against the copy about to be served: etag is the face's
// served validator (the unquoted sha1; "" when the ledger has no row —
// then only the date arm can answer), lastModified its Last-Modified
// spelling. See the file comment for the reference matrix this implements.
func manifestClientNotModified(r *http.Request, etag, lastModified string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if tags := quotedEntityTags(inm); len(tags) > 0 {
			for _, tag := range tags {
				if tag == etag {
					return true
				}
			}
			return false // a present non-matching list: the date arm is ignored
		}
		// Zero parseable tags (the unquoted/`*` spellings): dropped, and the
		// date arm decides.
	}
	return notModifiedSince(r.Header.Get("If-Modified-Since"), lastModified)
}

// blobClientNotModified evaluates the blob face's GET preconditions: the
// token comparison is quote-INSENSITIVE (the live capture's unquoted sha1
// answers 304, unlike the manifest face), and any present If-None-Match —
// matching or not, quoted or not — blocks If-Modified-Since entirely.
func blobClientNotModified(r *http.Request, etag, lastModified string) bool {
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
		return false
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

// quotedEntityTags parses an If-None-Match value into its QUOTED
// entity-tag inner values (W/ prefix tolerated, weak comparison): a token
// without surrounding quotes is not an entity-tag and parses to nothing —
// the live reference drops it rather than failing the precondition.
func quotedEntityTags(inm string) []string {
	var tags []string
	for _, tok := range strings.Split(inm, ",") {
		v := strings.TrimSpace(tok)
		v = strings.TrimPrefix(v, "W/")
		if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
			tags = append(tags, strings.Trim(v, `"`))
		}
	}
	return tags
}
