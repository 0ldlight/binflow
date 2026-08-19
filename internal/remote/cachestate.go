package remote

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// The pull-through cache state model (ADR-0012 decision 1 as amended by the
// T-79 errata, PRD C4): one remote_cache row per (repo, path) carries the
// conditional-GET validators and the TTL clock; the cached bytes are an
// ordinary blob+node in the remote repository's own namespace. Two content
// classes ride different TTLs — immutable artifacts (content, default 7200s)
// and regenerable protocol documents (metadata, default 600s) — and a third
// row kind records upstream misses (negative cache, missedRetrievalCacheSecs
// default 1800s).

// Cache-state tokens of the X-BinFlow-Cache response header (ADR-0012
// "consequences"; the QA assertion surface of M41/M44).
const (
	// CacheHit: a local copy within its TTL window served the request.
	CacheHit = "HIT"
	// CacheMiss: no usable local copy; the response was fetched upstream and
	// landed.
	CacheMiss = "MISS"
	// CacheStale: an EXPIRED local copy served the request — the upstream
	// was unfound (expired-but-serving) or unavailable (stale-while-error).
	CacheStale = "STALE"
	// CacheRevalidated: the upstream confirmed the expired copy still stands
	// (an unsolicited 304 in M3's direct-GET posture) and only the clock
	// moved.
	CacheRevalidated = "REVALIDATED"
)

// Response header names (exact spellings of PRD FR-20/RE-04).
const (
	// HdrCacheState reports which cache state served the body.
	HdrCacheState = "X-BinFlow-Cache"
	// HdrUpstreamError carries the upstream-fault summary whenever a stale
	// copy is served during an upstream error (BinFlow observability
	// extension, harmless to clients that ignore it).
	HdrUpstreamError = "X-Binflow-Upstream-Error"
)

// cacheKindNegative is the remote_cache kind for a miss record. The kind
// column is free text to the store (T-62), so the fetcher owns this value
// without widening the metadata API.
const cacheKindNegative = "negative"

// cacheStateNegative is the LOG token of a negative-cache serve (review
// side-fix: it was logged as STALE, which reads like an expired-copy serve
// on the QA dashboards). It is not a response-header value — clients keep
// seeing a plain 404.
const cacheStateNegative = "NEGATIVE"

// checksumSuffixes are the sidecar spellings RE-04 step 2 refuses to proxy:
// checksums are only ever served from cache entries or server computation,
// never fetched from the upstream.
var checksumSuffixes = []string{".sha1", ".md5", ".sha256", ".sha512"}

// msgChecksumsNotDownloadable is the EXACT 404 body message of the checksum
// sidecar refusal (repo-semantics section 7.2 step 2, high confidence; the
// M45 assertion compares for equality).
const msgChecksumsNotDownloadable = "Checksums are not downloadable."

// isChecksumPath reports whether path addresses a checksum sidecar.
func isChecksumPath(path string) bool {
	for _, suffix := range checksumSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// classifyPath resolves the cache class of one repository path: the
// registered protocol's MetadataProvider decides (architecture section 5.4);
// repositories whose package type registered no provider — generic, or a
// protocol ticket not yet landed — default every path to content (the safe
// side: a long TTL on an immutable object is harmless, a short TTL on one is
// not). The returned kind is one of metadata.RemoteCacheKind{Content,Metadata}.
func classifyPath(packageType, path string) string {
	if p, ok := adapter.ForProtocol(packageType); ok {
		if p.Classify(path) == adapter.KindMetadata {
			return metadata.RemoteCacheKindMetadata
		}
	}
	return metadata.RemoteCacheKindContent
}

// ttlFor maps a cache kind onto the repository's TTL field: artifacts take
// the long content TTL, protocol documents the short metadata TTL (both live
// on the remote_configs row; the service writes the product defaults 7200/600,
// T-64), and miss records take missedRetrievalCachePeriodSecs from the
// canonical repositories.config JSON.
func ttlFor(kind string, contentTTL, metadataTTL, missedTTL int64) int64 {
	switch kind {
	case metadata.RemoteCacheKindMetadata:
		return metadataTTL
	case cacheKindNegative:
		return missedTTL
	default:
		return contentTTL
	}
}

// entryFresh reports whether a cache row is still inside its TTL window.
// expiresAt is RFC3339 UTC text whose lexicographic order is chronological
// (the 003 column contract), but parsing keeps that honest against any
// hand-written row.
func entryFresh(expiresAt string, now time.Time) bool {
	if expiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return now.Before(t)
}

// negativeEntry builds the miss record of one path.
func negativeEntry(repoKey, path string, now time.Time, ttl int64) *metadata.RemoteCacheEntry {
	return &metadata.RemoteCacheEntry{
		RepoKey:   repoKey,
		Path:      path,
		Kind:      cacheKindNegative,
		FetchedAt: rfc3339(now),
		ExpiresAt: expiry(now, ttl),
	}
}

// contentEntry builds the validator row for a fetched copy.
func contentEntry(repoKey, path, etag, lastModified, kind string, now time.Time, ttl int64) *metadata.RemoteCacheEntry {
	return &metadata.RemoteCacheEntry{
		RepoKey:      repoKey,
		Path:         path,
		ETag:         etag,
		LastModified: lastModified,
		Kind:         kind,
		FetchedAt:    rfc3339(now),
		ExpiresAt:    expiry(now, ttl),
	}
}

// rfc3339 renders the store's timestamp spelling.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// expiry adds a TTL to now; a non-positive TTL yields an already-expired
// row (the "no freshness window" reading — every request revalidates).
func expiry(now time.Time, ttlSeconds int64) string {
	if ttlSeconds <= 0 {
		return rfc3339(now)
	}
	return rfc3339(now.Add(time.Duration(ttlSeconds) * time.Second))
}

// hintedReader carries the fetch's response hints (X-BinFlow-Cache and, on
// stale service, X-Binflow-Upstream-Error) across the service boundary: the
// serving adapter probes ExtraHeaders() structurally — no import, no
// repository-class knowledge (architecture section 5.4's "adapters are
// unaware of the three classes" stays mechanical, not aspirational).
type hintedReader struct {
	io.ReadSeekCloser
	hints http.Header
}

// ExtraHeaders implements the adapter-side probe seam.
func (r *hintedReader) ExtraHeaders() http.Header { return r.hints }

// hinted wraps one opened blob with the fetch's response hints.
func hinted(body io.ReadSeekCloser, cacheState, upstreamError string) *hintedReader {
	h := http.Header{}
	if cacheState != "" {
		h.Set(HdrCacheState, cacheState)
	}
	if upstreamError != "" {
		h.Set(HdrUpstreamError, upstreamError)
	}
	return &hintedReader{ReadSeekCloser: body, hints: h}
}
