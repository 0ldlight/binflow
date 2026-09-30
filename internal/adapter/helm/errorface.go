package helm

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/errface"
)

// The download-miss 404 face (BIN-103/T-621): the shared errors[] envelope
// (internal/errface — the single renderer, no local copy) with the
// reference's download-side wording, byte-pinned by the T-615 legs against
// Artifactory 7.161.26 (raw captures ar1-h-tgz-get/ar1-h2-tgz-get, both
// 132 bytes, identical bodies):
//
//	File not found.; Path: '<repoKey>:<repo-relative path>'
//
// The plane picks the media type spelling — the same matrix httpapi's
// dispatchContent applies at its envelope seam: the repo-path content
// plane answers the charset form, the read-only /api/helm download alias
// the bare one. The router rewrites the alias URL before the adapter, but
// the request line's URI — kept verbatim in RequestURI through both
// withAPIProtocolPrefix and withStrippedPrefix — still carries the
// original spelling, so the adapter decides from the request, never from
// the rewritten path.

// aliasPrefix is the /api/helm download alias's original wire spelling
// (productPrefix is ADR-0008's single product namespace, the same constant
// the 201 Location rendering cites).
const aliasPrefix = productPrefix + "/api/helm/"

// apiAliasRequest reports whether the request arrived through the read-only
// /api/helm alias (r is nil-safe: a bare-mount caller without a request
// reads as the content plane, the dominant mount).
func apiAliasRequest(r *http.Request) bool {
	return r != nil && strings.HasPrefix(r.RequestURI, aliasPrefix)
}

// writeDownloadMiss renders the GET/HEAD file-miss 404 on the shared
// envelope face.
func writeDownloadMiss(w http.ResponseWriter, r *http.Request, repoKey, path string) {
	errface.Write(w, http.StatusNotFound,
		fmt.Sprintf("File not found.; Path: '%s:%s'", repoKey, path),
		!apiAliasRequest(r))
}
