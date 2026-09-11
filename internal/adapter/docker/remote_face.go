package docker

// The REMOTE read face's client-visible header set and list pagination
// (L003-2, evidence reports/compatibility/L002-contract-replay.md #1/#2/
// #6/#7/#11 + the raw captures it indexes): the reference decorates every
// served manifest/blob copy with the full artifact face — the checksum
// family, Etag (=sha1, unquoted), Last-Modified (=the moment BinFlow/the
// reference cached the artifact, NOT any upstream mtime — both live
// captures carry the landing second), Accept-Ranges, the
// Content-Disposition/X-Artifactory-Filename pair, and
// X-Artifactory-Origin-Remote-Path (the upstream URL the copy traces to);
// manifest HEAD adds X-Artifactory-Docker-Registry (<repoKey>). The LIST
// endpoints honor the client's n/last window over the aggregated full
// list, answer an invalid n with the generic error model's 404 "Not Found"
// (NOT the local plane's official 400), and emit the Link rel="next"
// header exactly while truncation leaves a further page.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Header names of the remote face's Artifactory-parity set.
const (
	hdrOriginRemotePath   = "X-Artifactory-Origin-Remote-Path"
	hdrFilename           = "X-Artifactory-Filename"
	hdrDockerRegistry     = "X-Artifactory-Docker-Registry"
	hdrContentDisposition = "Content-Disposition"
)

// remoteFace carries the address facts the remote copy face needs beyond
// the landed node: the repository key the CLIENT addressed (manifest
// HEAD's X-Artifactory-Docker-Registry value — the cache row's RepoKey
// would answer the wrong key) and the full upstream URL the copy traces to
// (X-Artifactory-Origin-Remote-Path; "" omits the header — a face that
// cannot resolve its upstream degrades the header, never the serve).
//
// N7 (L003-2 review B): the DIRECT arm resolves its origin LAZILY — the
// plane facts lookup (two row reads through the RemoteV2Plane seam) runs
// only when a serve actually emits the header, so the unfound, negative
// and fault arms that never serve a copy never pay it. The VIRTUAL walk
// keeps the eager value: its facts are already in hand by the time the
// face is built (the member session resolved them).
type remoteFace struct {
	registry string
	// origin is the eager path value (the virtual walk's facts-derived
	// spelling); originFn the lazy direct-arm resolver. originPath folds
	// the two into the one read the copy servers perform.
	origin   string
	originFn func(ctx context.Context, p *Principal) string
}

// originPath resolves the face's X-Artifactory-Origin-Remote-Path value at
// serve time: the eager value wins, the lazy resolver runs only when there
// is one, "" omits the header (the degradation contract above).
func (f remoteFace) originPath(ctx context.Context, p *Principal) string {
	if f.origin != "" {
		return f.origin
	}
	if f.originFn != nil {
		return f.originFn(ctx, p)
	}
	return ""
}

// remoteOriginBase resolves the upstream registry root of one direct
// remote repository through the v2 plane seam; "" when the seam cannot
// answer.
func (h *Handler) remoteOriginBase(ctx context.Context, p *Principal, repoKey string) string {
	plane := h.remotePlane()
	if plane == nil {
		return ""
	}
	facts, err := plane.RemoteUpstream(ctx, p, repoKey)
	if err != nil || facts == nil {
		return ""
	}
	return facts.URL
}

// manifestFace builds the manifest copy face of one request: the addressed
// repository key plus the upstream manifest URL (the reference echoes the
// REQUESTED reference — the tag verbatim, the digest re-prefixed — never
// the resolved digest; capture a_mf.h ends in /manifests/t1 for a tag
// request). The origin stays lazy (N7 above).
func (h *Handler) manifestFace(ref nameRef, reference string) remoteFace {
	return remoteFace{
		registry: ref.repoKey,
		originFn: func(ctx context.Context, p *Principal) string {
			return originRemotePath(h.remoteOriginBase(ctx, p, ref.repoKey), v2WireManifestPath(ref.image, reference))
		},
	}
}

// blobFace builds the blob copy face of one request (the origin lazy, N7).
func (h *Handler) blobFace(ref nameRef, hex string) remoteFace {
	return remoteFace{
		registry: ref.repoKey,
		originFn: func(ctx context.Context, p *Principal) string {
			return originRemotePath(h.remoteOriginBase(ctx, p, ref.repoKey), v2WireBlobPath(ref.image, hex))
		},
	}
}

// originRemotePath joins the upstream root and one wire path; an empty
// base answers "" (the header-degradation contract above).
func originRemotePath(base, wire string) string {
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + wire
}

// originRemoteOf is the facts-carrying form the virtual walk uses (nil
// facts answer "", the same degradation).
func originRemoteOf(facts *repo.RemoteUpstream, wire string) string {
	if facts == nil {
		return ""
	}
	return originRemotePath(facts.URL, wire)
}

// nodeLastModified renders the node's cache-landing timestamp in HTTP
// date form (UpdatedAt with a CreatedAt fallback): the remote face's
// Last-Modified is the moment the artifact LANDED — both live captures
// carry the fetch second, distinct from any upstream mtime.
func nodeLastModified(node *metadata.Node) string {
	if node == nil {
		return ""
	}
	for _, ts := range []string{node.UpdatedAt, node.CreatedAt} {
		if ts == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		return t.UTC().Format(http.TimeFormat)
	}
	return ""
}

// manifestFilename is the manifest face's artifact filename: an index
// media type lands as list.manifest.json, a single manifest as
// manifest.json (capture a_mf.h's Content-Disposition/X-Artifactory-
// Filename pair; the contract's ^(list\.)?manifest\.json$ pattern).
func manifestFilename(mediaType string) string {
	if mediaType == mediaTypeDockerList || mediaType == mediaTypeOCIIndex {
		return "list.manifest.json"
	}
	return "manifest.json"
}

// blobFilename is the blob face's artifact filename — the digest-keyed
// spelling sha256__<hex> (capture a_blob.h).
func blobFilename(hex string) string { return "sha256__" + hex }

// remotePageSize resolves the remote LIST face's n parameter: absent = 0,
// the unbounded full aggregation (the remote face's own default — the
// local plane's official 100-page window is NOT the reference remote
// face's); an n that is not a positive decimal integer answers the remote
// face's own invalid-n shape — 404 with the generic error model's pretty
// "Not Found" (capture a_tagsinv.h), never the local plane's official 400
// PAGINATION_NUMBER_INVALID.
func remotePageSize(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("n")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		writeStatusFormError(w, http.StatusNotFound, "Not Found")
		return 0, false
	}
	return n, true
}
