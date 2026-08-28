package nuget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The v3 upstream proxy (nuget.md sections 8.1 and 9): the remote class's
// v3 faces resolve the upstream's SERVICE INDEX first — fetched through the
// engine at the .nuGetV3/feed.json marker (the section 9.3 cache layout,
// the repository's metadata TTL riding the provider's Classify) — and every
// later upstream resource is addressed by the TYPE ladder of section 9.2,
// never by a path-prefix constant:
//
//   - the SEARCH face resolves SearchQueryService (→ /3.0.0-beta →
//     /3.0.0-rc) and GETs the absolute @id DIRECTLY through the guarded
//     egress client (section 8.1-4: the search host is commonly not the
//     repository base — nuget.org's azuresearch — and the result never
//     lands as an artifact);
//   - the DOCUMENT faces (registration family, flatcontainer family) join
//     the resolved @id's PATH onto the engine's base-joined fetch under
//     .nuGetV3/<upstream-path>/… (the section 9.3 layout: the marker
//     carries the full upstream path, so the provider's UpstreamPath facet
//     is the identity strip).
//
// The constant spellings below (v3/index.json, v3-flatcontainer,
// v3/registration5-gz-semver2) are the nuget.org-family DEFAULTS — the
// xsd default v3FeedUrl's relative spelling and T-287's observed @id
// spellings. They are the fallback when the upstream index cannot be
// resolved, which keeps the default upstream OBSERVABLY EQUIVALENT to the
// as-built behavior (the ticket's regression guardrail); a heterogeneous
// or re-spelled upstream adapts through its own index instead.

const (
	// v3CacheDir is the section 9.3 cache directory under a remote
	// repository's namespace.
	v3CacheDir = ".nuGetV3"
	// v3FeedMarker is the cached upstream service index's storage path.
	v3FeedMarker = v3CacheDir + "/feed.json"
	// v3FeedUpstreamPath is the upstream service index's path under the
	// repository URL (the xsd default v3FeedUrl's relative spelling).
	v3FeedUpstreamPath = "v3/index.json"
	// v3FallbackFlatPath is the flatcontainer family's fallback upstream
	// path (nuget.org's current @id spelling).
	v3FallbackFlatPath = "v3-flatcontainer"
	// v3FallbackRegPath is the registration family's fallback upstream
	// path (nuget.org's current @id spelling).
	v3FallbackRegPath = "v3/registration5-gz-semver2"
	// v3SearchRetryTake is the take of the section 8.1-5 retry (an upstream
	// 400 answered again with take=100 once).
	v3SearchRetryTake = 100
	// v3VirtualMemberTake is the section 8.2-2 fixed candidate page every
	// remote member of a virtual search contributes.
	v3VirtualMemberTake = 1000
)

// v3docReader reads one storage-path document through a repository or
// member seam. An error is the seam's own (the caller maps or skips).
type v3docReader func(path string) ([]byte, error)

// v3RepoReader builds the direct repository reader (svc.Get, error intact
// for the writeError mapping).
func (h *Handler) v3RepoReader(ctx context.Context, p *repo.Principal, repoKey string) v3docReader {
	return func(path string) ([]byte, error) {
		rc, _, err := h.svc.Get(ctx, p, repoKey, path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
		body, err := io.ReadAll(io.LimitReader(rc, 64<<20))
		if err != nil {
			return nil, fmt.Errorf("read cached document %s: %w", path, err)
		}
		return gunzipIfNeeded(body), nil
	}
}

// v3MemberReader builds the member-seam reader (virtual faces): any seam
// miss reads as the not-found sentinel the walks skip.
func (h *Handler) v3MemberReader(ctx context.Context, virtualKey, member string) v3docReader {
	return func(path string) ([]byte, error) {
		body, ok := h.readMemberDocument(ctx, virtualKey, member, path)
		if !ok {
			return nil, errV3MemberMiss
		}
		return body, nil
	}
}

// errV3MemberMiss is the member-seam miss sentinel (never surfaces; the
// walks treat it as "this member contributes nothing").
var errV3MemberMiss = fmt.Errorf("nuget v3: member document miss")

// upstreamIndex is one parsed upstream service index.
type upstreamIndex struct {
	resources []serviceIndexResource
}

// v3ResolveUpstreamIndex fetches and parses the upstream service index
// through the reader (the engine's marker cache stands behind it). Every
// failure — seam error, unparseable body, no resources — returns nil: the
// callers fall back to the nuget.org-family constants (the guardrail).
func v3ResolveUpstreamIndex(read v3docReader) *upstreamIndex {
	body, err := read(v3FeedMarker)
	if err != nil || len(body) == 0 {
		return nil
	}
	var doc serviceIndexDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil
	}
	if len(doc.Resources) == 0 {
		return nil
	}
	return &upstreamIndex{resources: doc.Resources}
}

// resourceID resolves the first @id of the given types whose URL parses
// ("" when none matches).
func (u *upstreamIndex) resourceID(types ...string) string {
	if u == nil {
		return ""
	}
	for _, want := range types {
		for _, r := range u.resources {
			if r.Type != want {
				continue
			}
			if _, ok := v3URLPath(r.ID); ok {
				return r.ID
			}
		}
	}
	return ""
}

// v3URLPath extracts one absolute URL's path, slash-trimmed ("" with ok
// false for relative, host-less or malformed spellings).
func v3URLPath(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	p := strings.Trim(u.Path, "/")
	if p == "" {
		return "", false
	}
	return p, true
}

// searchServiceAbsolute resolves the SearchQueryService family's ABSOLUTE
// @id (the section 8.1-2 ladder: SearchQueryService → /3.0.0-beta →
// /3.0.0-rc, first hit) — the address the direct egress GET targets. ""
// when the index names none.
func (u *upstreamIndex) searchServiceAbsolute() string {
	return u.resourceID("SearchQueryService", "SearchQueryService/3.0.0-beta", "SearchQueryService/3.0.0-rc")
}

// registrationPath resolves the registration family's upstream path. The
// semver2 shape walks the versioned ladder first (RegistrationsBaseUrl/
// 3.6.0 → /Versioned — section 9.2's FeedUtils replacement table) and then
// falls back to the plain family, which itself walks the section 9.1 row
// (plain → /3.4.0 → /3.0.0-rc → /3.0.0-beta). An unresolvable index or an
// index naming none of the family returns the nuget.org-family fallback.
func (u *upstreamIndex) registrationPath(semver2 bool) string {
	plain := []string{"RegistrationsBaseUrl", "RegistrationsBaseUrl/3.4.0", "RegistrationsBaseUrl/3.0.0-rc", "RegistrationsBaseUrl/3.0.0-beta"}
	types := plain
	if semver2 {
		types = append([]string{"RegistrationsBaseUrl/3.6.0", "RegistrationsBaseUrl/Versioned"}, plain...)
	}
	if id := u.resourceID(types...); id != "" {
		if p, ok := v3URLPath(id); ok {
			return p
		}
	}
	return v3FallbackRegPath
}

// flatPath resolves the flatcontainer family's upstream path
// (PackageBaseAddress/3.0.0; the fallback otherwise).
func (u *upstreamIndex) flatPath() string {
	if id := u.resourceID("PackageBaseAddress/3.0.0"); id != "" {
		if p, ok := v3URLPath(id); ok {
			return p
		}
	}
	return v3FallbackFlatPath
}

// v3CachePath renders one upstream document's storage marker:
// .nuGetV3/<upstream-path>/<tail>. The upstream path rides the marker
// verbatim, so the provider's UpstreamPath facet is the identity strip.
func v3CachePath(upstreamPath, tail string) string {
	return v3CacheDir + "/" + strings.Trim(upstreamPath, "/") + "/" + tail
}

// v3FlatMarkerPath resolves one canonical flatcontainer tail onto its
// dynamic marker path through the repository's cached upstream index (the
// v2 download faces' first probe — the section 9.4 packageContent rewrite
// cites the v2 Download face, so a v3-cached copy must serve there).
func (h *Handler) v3FlatMarkerPath(ctx context.Context, p *repo.Principal, repoKey, tail string) string {
	read := h.v3RepoReader(ctx, p, repoKey)
	return v3CachePath(v3ResolveUpstreamIndex(read).flatPath(), tail)
}

// ---- the search egress client (section 8.1-4: the direct upstream GET) ----

// v3egressEntry is one cached egress client plus its signature.
type v3egressEntry struct {
	sig    string
	client *remote.Client
}

// v3egressPool caches one guarded outbound client per repository (the helm
// external-egress posture in miniature — no credentials ride this face:
// the at-rest credential cipher lives in the engine, and the public search
// services are anonymous; the deviation register in the ticket log carries
// the authenticated-feed degradation).
type v3egressPool struct {
	mu      sync.Mutex
	clients map[string]*v3egressEntry
}

// clientFor returns the repository's egress client, rebuilding on a policy
// signature change (the helm extClientPool twin).
func (p *v3egressPool) clientFor(repoKey string, allowPrivate bool, timeoutMs int64) (*remote.Client, error) {
	sig := fmt.Sprintf("%t\x00%d", allowPrivate, timeoutMs)
	p.mu.Lock()
	cached := p.clients[repoKey]
	p.mu.Unlock()
	if cached != nil && cached.sig == sig {
		return cached.client, nil
	}
	client, err := remote.NewClient(remote.Options{
		RepoKey:              repoKey + "-nuget-v3-search",
		AllowPrivateUpstream: allowPrivate,
		SocketTimeout:        time.Duration(timeoutMs) * time.Millisecond,
	})
	if err != nil {
		return nil, fmt.Errorf("nuget %s: v3 search egress client: %w", repoKey, err)
	}
	p.mu.Lock()
	if p.clients == nil {
		p.clients = map[string]*v3egressEntry{}
	}
	old := p.clients[repoKey]
	p.clients[repoKey] = &v3egressEntry{sig: sig, client: client}
	p.mu.Unlock()
	if old != nil {
		old.client.CloseIdleConnections()
	}
	return client, nil
}

// v3EgressFetch GETs one absolute URL through the repository's guarded
// egress client (nil body on any transport/chain failure — the member
// contributes empty, never a 5xx; section 8.1-6).
func (h *Handler) v3EgressFetch(ctx context.Context, repoKey, target string) ([]byte, int) {
	allowPrivate, timeoutMs := false, int64(0)
	if h.remote != nil {
		if cfg, err := h.remote.GetConfig(ctx, repoKey); err == nil && cfg != nil {
			allowPrivate, timeoutMs = cfg.AllowPrivateUpstream, cfg.SocketTimeoutMs
		}
	}
	client, err := h.egress.clientFor(repoKey, allowPrivate, timeoutMs)
	if err != nil {
		return nil, 0
	}
	res, err := client.Fetch(ctx, remote.Request{URL: target})
	if err != nil {
		return nil, 0
	}
	return res.Body, res.StatusCode
}

// v3SearchQuery is the section 8.1-3 parameter set.
type v3SearchQuery struct {
	term        string
	skip        int
	take        int
	prerelease  bool
	semVerLevel string
}

// url renders the query URL against one search @id base.
func (q v3SearchQuery) url(base string) string {
	v := url.Values{}
	if q.term != "" {
		v.Set("q", q.term)
	}
	if q.skip > 0 {
		v.Set("skip", fmt.Sprint(q.skip))
	}
	v.Set("take", fmt.Sprint(q.take))
	if q.prerelease {
		v.Set("prerelease", "true")
	}
	if q.semVerLevel != "" {
		v.Set("semVerLevel", q.semVerLevel)
	}
	sep := "?"
	if strings.HasSuffix(base, "?") {
		sep = ""
	}
	return base + sep + v.Encode()
}

// v3UpstreamSearch runs the section 8.1 chain for one upstream: resolve the
// search @id, compose the query URL, direct GET, retry a 400 once at
// take=100, and hand back the raw body. ok is false whenever the chain gave
// nothing (no index, no SearchQueryService, transport fault, non-200,
// empty body) — the caller falls back.
func (h *Handler) v3UpstreamSearch(ctx context.Context, repoKey string, read v3docReader, q v3SearchQuery) ([]byte, bool) {
	idx := v3ResolveUpstreamIndex(read)
	abs := idx.searchServiceAbsolute()
	if abs == "" {
		return nil, false
	}
	body, status := h.v3EgressFetch(ctx, repoKey, q.url(abs))
	if status == http.StatusBadRequest && q.take != v3SearchRetryTake {
		retry := q
		retry.take = v3SearchRetryTake
		body, status = h.v3EgressFetch(ctx, repoKey, retry.url(abs))
	}
	if status != http.StatusOK || len(body) == 0 {
		return nil, false
	}
	return gunzipIfNeeded(body), true
}

// ---- the rewrite tables (sections 8.1-7 and 9.4) ----

// v3rewriteTable carries the upstream path prefixes (slash-trimmed) and the
// BinFlow bases one URL-token rewrite matches against. Longest prefix
// wins, so the semver2 and plain registration spellings coexist.
type v3rewriteTable struct {
	origin  string
	repoKey string
	// regUp are the upstream registration paths (any spelling) and regOut
	// the BinFlow registration base they rewrite onto.
	regUp  []string
	regOut string
	// flatUp are the upstream flatcontainer paths; flatV2 is the section
	// 9.4 arm (registration documents: packageContent cites the v2 Download
	// face) — search results set it false and cite the flatcontainer base.
	flatUp  []string
	flatV2  bool
	flatOut string
}

// v3RewriteJSON walks one JSON document's string tokens, rewriting every
// URL token whose path matches a table prefix (the rewriteDocument posture
// verbatim: no decode/re-encode of the tree, non-URL bytes identical).
func v3RewriteJSON(body []byte, tbl *v3rewriteTable) []byte {
	if tbl == nil || !json.Valid(body) {
		return body
	}
	var b strings.Builder
	b.Grow(len(body))
	i := 0
	for i < len(body) {
		start := indexByteFrom(body, i, '"')
		if start < 0 {
			b.Write(body[i:])
			break
		}
		end := indexByteFrom(body, start+1, '"')
		if end < 0 {
			b.Write(body[i:])
			break
		}
		b.Write(body[i : start+1])
		b.WriteString(tbl.rewriteToken(string(body[start+1 : end])))
		b.WriteByte('"')
		i = end + 1
	}
	return []byte(b.String())
}

// rewriteToken rewrites one JSON string token (identity for non-URLs).
// The longest matching prefix wins, so a plain/semver2 registration pair
// whose spellings share a leading segment cannot shadow one another.
func (t *v3rewriteTable) rewriteToken(tok string) string {
	path, ok := v3TokenPath(tok)
	if !ok {
		return tok
	}
	bestReg, bestFlat := "", ""
	for _, up := range t.regUp {
		if (path == up || strings.HasPrefix(path, up+"/")) && len(up) > len(bestReg) {
			bestReg = up
		}
	}
	if bestReg != "" {
		return t.regOut + strings.TrimPrefix(strings.TrimPrefix(path, bestReg), "/")
	}
	for _, up := range t.flatUp {
		if (path == up || strings.HasPrefix(path, up+"/")) && len(up) > len(bestFlat) {
			bestFlat = up
		}
	}
	if bestFlat != "" {
		tail := strings.TrimPrefix(strings.TrimPrefix(path, bestFlat), "/")
		if t.flatV2 {
			if v2URL, ok := v2DownloadURL(t.origin, t.repoKey, tail); ok {
				return v2URL
			}
		}
		return t.flatOut + tail
	}
	return tok
}

// v3TokenPath extracts one JSON string token's URL path ("" with ok false
// for non-absolute spellings) — the rewriteDocument matching posture: by
// path shape, never by host.
func v3TokenPath(tok string) (string, bool) {
	const schemeMark = "://"
	i := strings.Index(tok, schemeMark)
	if i <= 0 {
		return "", false
	}
	rest := tok[i+len(schemeMark):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		return strings.Trim(rest[j+1:], "/"), true
	}
	return "", false
}

// v2DownloadURL maps one upstream flatcontainer tail
// (<id>/<version>/<id>.<version>.nupkg) onto THIS repository's v2 Download
// face (nuget.md section 9.4: the packageContent rewrite — id/version
// lowercased, the SemVer2 build metadata stripped for the non-smart
// default). ok is false for every other tail shape.
func v2DownloadURL(origin, repoKey, tail string) (string, bool) {
	tail = strings.Trim(tail, "/")
	id, rest, found := strings.Cut(tail, "/")
	if !found {
		return "", false
	}
	version, file, found := strings.Cut(rest, "/")
	if !found || file != id+"."+version+suffixNupkg {
		return "", false
	}
	if _, ok := normalizeNuGetVersion(version); !ok {
		return "", false
	}
	version, _, _ = strings.Cut(version, "+")
	return v2Base(origin, repoKey) + "/Download/" + lowerASCII(id) + "/" + lowerASCII(version), true
}

// buildRewriteTable assembles the table for one repository's rewrite off
// its resolved upstream index (nil index → the fallback constants).
func v3BuildRewriteTable(origin, repoKey string, idx *upstreamIndex, flatV2 bool, semVer2Out bool) *v3rewriteTable {
	reg, regSemVer2, flat := v3FallbackRegPath, v3FallbackRegPath, v3FallbackFlatPath
	if idx != nil {
		reg = idx.registrationPath(false)
		regSemVer2 = idx.registrationPath(true)
		flat = idx.flatPath()
	}
	regUp := []string{reg}
	if regSemVer2 != reg {
		regUp = append(regUp, regSemVer2) // the SemVer2 spelling coexists; longest-prefix match
	}
	regOut := regBase(origin, repoKey)
	if semVer2Out {
		regOut = regSemVer2Base(origin, repoKey)
	}
	return &v3rewriteTable{
		origin:  origin,
		repoKey: repoKey,
		regUp:   regUp,
		regOut:  regOut,
		flatUp:  []string{flat},
		flatV2:  flatV2,
		flatOut: flatBase(origin, repoKey),
	}
}
