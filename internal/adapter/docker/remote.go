package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// The registry-v2 REMOTE plane (M13 T-363, FR-116.1; the behavior spec is
// helm.md section 8.3, the K51 increment). A remote docker/helmoci
// repository proxies the OCI Distribution conversation upstream:
//
//   - manifest GET/HEAD by tag or digest: resolve locally when the cache
//     holds (docker_tags/docker_manifests rows + a fresh remote_cache
//     entry), otherwise talk upstream — Accept negotiated, Bearer
//     challenge danced — land the body checksum-addressed and record the
//     index rows so the next resolution is a HIT;
//   - blob GET/HEAD: the same probe/land flow over the digest-keyed blob
//     path, STREAMED through a storage session (unbounded body, flat heap
//     — FR-20-AC11) with the digest enforced at commit;
//   - tags/list and the repo-domain _catalog: the upstream LIST
//     conversation (L000-B C15/C16, evidence E7) — the tags/catalog pages
//     fetched live from the upstream (Link rel="next" aggregation), never
//     the cached rows alone;
//   - every observed write verb: Artifactory's 400 upload refusal (C11);
//     the unobserved combinations keep RE-05's 405 + Allow: GET.
//
// The upstream SESSION rides internal/remote's exported outbound client
// (the engine's own NFR-S13 chain: guarded dials, per-hop re-screening,
// socket timeouts, idempotent retries, no compression), pooled per
// repository like the engine's clientFor. The CACHE half runs through
// repo.Service's RemoteV2Plane seam (probe / land / miss-record / manifest
// rows), which owns the same landing invariants as the engine — the two
// writers cannot disagree about what a cached copy is.

// remoteSessions is the per-handler pool of upstream sessions.
type remoteSessions struct {
	mu      sync.Mutex
	entries map[string]*remoteSessionEntry
}

// remoteSessionEntry is one repository's session: two clients (with the
// repository credential for the first attempt and the token exchange;
// without any credential for the Bearer retries, so the dance's token
// reaches the upstream verbatim) plus the Bearer token cache keyed on the
// challenge's scope.
type remoteSessionEntry struct {
	sig    string
	authed *remote.Client
	anon   *remote.Client

	mu     sync.Mutex
	tokens map[string]*remoteBearerToken
}

// remoteBearerToken is one exchanged upstream token with its expiry.
type remoteBearerToken struct {
	value     string
	expiresAt time.Time
}

// bearerTokenCacheTTL is the fallback window when the token endpoint
// answers without expires_in (60s — the distribution token endpoints'
// own default shape).
const bearerTokenCacheTTL = time.Minute

// bearerTokenMaxTTL caps a misbehaving endpoint's declared lifetime.
const bearerTokenMaxTTL = time.Hour

// forRepo resolves (or rebuilds) the repository's session. facts come from
// the service seam; the signature rebuild posture mirrors the engine's
// clientFor — a config update swaps the clients on the next request.
func (p *remoteSessions) forRepo(repoKey string, facts *repo.RemoteUpstream) (*remoteSessionEntry, error) {
	sig := strings.Join([]string{
		facts.URL, facts.Username, facts.Password,
		strconv.FormatBool(facts.TokenAuth),
		strconv.FormatBool(facts.AllowPrivateUpstream),
		strconv.FormatInt(facts.SocketTimeoutMs, 10),
	}, "\x00")
	p.mu.Lock()
	cached := p.entries[repoKey]
	p.mu.Unlock()
	if cached != nil && cached.sig == sig {
		return cached, nil
	}
	timeout := time.Duration(facts.SocketTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = remote.DefaultSocketTimeout
	}
	authed, err := remote.NewClient(remote.Options{
		RepoKey:              repoKey,
		BaseURL:              facts.URL,
		Username:             facts.Username,
		Password:             facts.Password,
		TokenAuth:            facts.TokenAuth,
		AllowPrivateUpstream: facts.AllowPrivateUpstream,
		SocketTimeout:        timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("docker remote %s: upstream session: %w", repoKey, err)
	}
	// The credential-free twin: applyBasicAuth leaves the request's own
	// Authorization alone, so the dance's Bearer token rides verbatim.
	anon, err := remote.NewClient(remote.Options{
		RepoKey:              repoKey + "-bearer",
		BaseURL:              facts.URL,
		AllowPrivateUpstream: facts.AllowPrivateUpstream,
		SocketTimeout:        timeout,
	})
	if err != nil {
		authed.CloseIdleConnections()
		return nil, fmt.Errorf("docker remote %s: bearer session: %w", repoKey, err)
	}
	entry := &remoteSessionEntry{sig: sig, authed: authed, anon: anon, tokens: map[string]*remoteBearerToken{}}
	p.mu.Lock()
	if p.entries == nil {
		p.entries = map[string]*remoteSessionEntry{}
	}
	old := p.entries[repoKey]
	p.entries[repoKey] = entry
	p.mu.Unlock()
	if old != nil {
		old.authed.CloseIdleConnections()
		old.anon.CloseIdleConnections()
	}
	return entry, nil
}

// v2WireManifestPath builds the upstream request path of one manifest
// read: the /v2/ API root plus the image's namespace and the wire ref (a
// tag verbatim, a digest re-prefixed with the algorithm — the storage
// layout keeps bare hex, the wire keeps the sha256: spelling). The upstream
// URL is the REGISTRY ROOT (L000-F: the registry-v2 spec and Artifactory's
// E4 evidence — X-Artifactory-Origin-Remote-Path — both speak
// <root>/v2/<name>/manifests/<ref>).
func v2WireManifestPath(image, ref string) string {
	return "/v2/" + image + "/manifests/" + ref
}

// v2WireBlobPath builds the upstream request path of one blob read — the
// same /v2/ root as the manifest arm.
func v2WireBlobPath(image, hex string) string {
	return "/v2/" + image + "/blobs/" + digestPrefixHex(hex)
}

// manifestMissNodePath is the negative-cache key of one manifest
// reference's cold miss (C14, the L004-2 verdict): a digest request keys
// the digest-keyed manifest node path (the standing arm's own key — the
// miss record and a later landed copy address the same path), and a tag
// request keys a row under the image's tags/ namespace — a TAG has no
// landed copy to key, and this namespace cannot collide with the
// digest-keyed node layout (nodes live under manifests/<hex> and
// blobs/<hex> only; the tags/list ROUTE is a wire path, never a cache-row
// key). Both spellings stay legal node paths by construction (the image
// and tag characters were validated at the edge).
func manifestMissNodePath(image, reference string, isDigestRef bool, wantHex string) string {
	if isDigestRef {
		return manifestNodePath(image, wantHex)
	}
	return image + "/tags/" + reference
}

// upstreamAnswer is one classified upstream outcome shared by the two
// fetch arms: the status line plus, on the buffered (manifest) arm only,
// the body.
type upstreamAnswer struct {
	status  int
	statusT string
	header  http.Header
	body    []byte
}

// errUpstreamAuth marks an exhausted Bearer dance (the caller maps the
// standing 401/403 onto the unfound family with the upstream summary).
var errUpstreamAuth = errors.New("upstream refused the negotiated credential")

// fetchManifest performs the manifest conversation: one attempt with the
// cached token (or the repository credential), and when the upstream
// answers 401 with a Bearer challenge, the token exchange and exactly one
// retry. accept is the CLIENT's Accept set, forwarded verbatim (upstream
// content negotiation is the registry's business; BinFlow never converts).
// ifNoneMatch is the REVALIDATION arm's upstream validator (remote-cache-v2
// §5.3): non-empty only on the expired tag path, it rides the hop as
// If-None-Match — the digest-etag spelling `sha256:<hex>` is what
// distribution-family registries serve as the manifest ETag, so an
// unchanged tag answers 304 upstream and the caller slides the window
// instead of re-transferring the body.
// The manifest body is the buffered class — the local plane's 4MB ceiling
// applies to proxied ones too.
func (e *remoteSessionEntry) fetchManifest(ctx context.Context, image, ref string, accept []string, ifNoneMatch string) (*upstreamAnswer, error) {
	wire := v2WireManifestPath(image, ref)
	scope := "repository:" + image + ":pull"
	hdr := http.Header{}
	for _, v := range accept {
		hdr.Add("Accept", v)
	}
	if ifNoneMatch != "" {
		hdr.Set("If-None-Match", ifNoneMatch)
	}
	res, usedToken, err := e.attempt(ctx, e.buffered, wire, scope, hdr)
	if err != nil {
		return nil, err
	}
	if res.status != http.StatusUnauthorized {
		return res, nil
	}
	challenge := parseBearerChallenge(res.header.Get("WWW-Authenticate"))
	if challenge == nil || challenge.realm == "" {
		// A plain 401 (no Bearer dance available): the caller maps it onto
		// the unfound family with the upstream summary — the engine's own
		// 401/403 posture.
		return res, nil
	}
	return e.dance(ctx, challenge, scope, func(token string) (*upstreamAnswer, error) {
		_ = usedToken // the retry below drops the cache entry itself
		return e.attemptWithToken(ctx, e.buffered, wire, token, hdr)
	})
}

// revalidationValidator is the tag-path revalidation arm's upstream
// If-None-Match value off one standing copy: the quoted digest etag, or ""
// when the arm does not apply (no standing copy, or a digest-addressed
// request — digest content is immutable and its expired refetch stays
// unconditional, ADR-0012 decision 1's resolution-window reading).
func revalidationValidator(standing *remoteStanding, isDigestRef bool) string {
	if standing == nil || standing.node == nil || standing.dgst == "" || isDigestRef {
		return ""
	}
	return `"` + digestPrefix + standing.dgst + `"`
}

// serveRevalidatedManifest is the upstream-304 arm both manifest planes
// share (remote-cache-v2 §5.3, the docker-face client half the engine leg
// already signals as CacheRevalidated): the upstream confirmed the expired
// copy still stands, so the retrieval window slides — the standing body is
// re-landed through the arm's own sink, the one adapter-reachable TTL
// write (LandRemoteBlob refreshes the remote_cache row's clock; same
// digest, so the landing is the idempotent overwrite the direct 200 path
// performs) — and the copy serves 200 full with the REVALIDATED marker.
// Client conditionals are NOT evaluated on this arm: the live reference's
// revalidation serve answers 200 full regardless of the client's
// validators (L004-1 evidence), the E3-3 decompile's unconditional
// notModifiedResponse being a path the live flow's HEAD-first
// revalidation never reaches against a distribution upstream.
func (h *Handler) serveRevalidatedManifest(w http.ResponseWriter, r *http.Request, ref nameRef, standing *remoteStanding, reland func(mediaType string, body io.Reader) error, serve manifestCopyServer) {
	mediaType := standing.mediaType
	if mediaType == "" {
		mediaType = standing.node.Mime
	}
	if rc, oerr := h.openRemoteBlob(r.Context(), standing.node); oerr == nil {
		func() {
			defer rc.Close() //nolint:errcheck // read-only fd
			if lerr := reland(mediaType, rc); lerr != nil {
				// The serve stands on the confirmed copy; only the window
				// slide was lost (the next request revalidates again).
				h.log.WarnContext(r.Context(), "docker remote: revalidation window slide failed (serving the confirmed copy)",
					"repo", ref.repoKey, "image", ref.image, "digest", standing.dgst, "error", lerr.Error())
			}
		}()
	} else {
		h.log.WarnContext(r.Context(), "docker remote: revalidation window slide could not open the standing copy",
			"repo", ref.repoKey, "image", ref.image, "digest", standing.dgst, "error", oerr.Error())
	}
	serve(w, r, standing.node, mediaType, standing.dgst, standing.size, remote.CacheRevalidated, "")
}

// fetchBlobStream performs the blob conversation: one streaming attempt
// with the cached token (or the repository credential), and when the
// upstream answers 401 with a Bearer challenge, the token exchange and
// exactly one retry. A 200 answer carries the OPEN body the caller lands
// and closes; every other status arrives body-drained.
func (e *remoteSessionEntry) fetchBlobStream(ctx context.Context, image, hex string) (*remote.StreamResult, error) {
	wire := v2WireBlobPath(image, hex)
	scope := "repository:" + image + ":pull"
	res, usedToken, err := e.attemptStream(ctx, wire, scope)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusUnauthorized {
		return res, nil
	}
	challenge := parseBearerChallenge(res.Header.Get("WWW-Authenticate"))
	if challenge == nil || challenge.realm == "" {
		drainUpstream(res.Body)
		return res, nil
	}
	_ = usedToken // the dance drops the cache entry itself
	if usedToken {
		e.dropToken(scope)
	}
	token, terr := e.exchange(ctx, challenge, scope)
	drainUpstream(res.Body)
	if terr != nil {
		return nil, fmt.Errorf("%w: token exchange: %w", errUpstreamAuth, terr)
	}
	e.storeToken(scope, token)
	return e.attemptStreamWithToken(ctx, wire, token.value)
}

// remoteListMaxPages caps the upstream Link rel="next" aggregation of the
// list endpoints (tags/list, _catalog): Artifactory's
// remote.fetching.list.maximum.iteration default is 3 (evidence report
// section 8's key-defaults table; the configured hard ceiling of 15 needs
// no knob until a caller asks for one).
const remoteListMaxPages = 3

// fetchList runs one upstream LIST conversation (tags/list, _catalog,
// L000-B C15/C16): the first page through the same attempt/dance machinery
// the manifest arm uses, then the upstream's Link rel="next" chain
// followed up to remoteListMaxPages total requests (every page URL is a
// full absolute URL — the outbound client guard-checks each hop). Pages
// are buffered answers; the chain stops at the first non-200 or transport
// fault. The CALLER decides what a failed head page means — the two
// endpoints degrade differently (C15: tags render null; C16: the catalog
// renders empty).
func (e *remoteSessionEntry) fetchList(ctx context.Context, wire, scope string) ([]*upstreamAnswer, error) {
	fetch := func(ctx context.Context, c *remote.Client, req remote.Request) (*upstreamAnswer, error) {
		return e.buffered(ctx, c, req)
	}
	first, _, err := e.attempt(ctx, fetch, wire, scope, http.Header{})
	if err != nil {
		return nil, err
	}
	if first.status == http.StatusUnauthorized {
		if challenge := parseBearerChallenge(first.header.Get("WWW-Authenticate")); challenge != nil && challenge.realm != "" {
			first, err = e.dance(ctx, challenge, scope, func(token string) (*upstreamAnswer, error) {
				return e.attemptWithToken(ctx, fetch, wire, token, http.Header{})
			})
			if err != nil {
				return nil, err
			}
		}
	}
	pages := []*upstreamAnswer{first}
	seen := map[string]bool{}
	next := nextListLink(first.header.Get("Link"))
	for i := 1; i < remoteListMaxPages && next != "" && !seen[next]; i++ {
		seen[next] = true
		var page *upstreamAnswer
		var perr error
		if token := e.token(scope); token != "" {
			page, perr = e.buffered(ctx, e.anon, remote.Request{URL: next, Header: bearerAuthHeader(token)})
		} else {
			page, perr = e.buffered(ctx, e.authed, remote.Request{URL: next})
		}
		if perr != nil || page.status != http.StatusOK {
			break // a broken chain serves the pages that arrived
		}
		pages = append(pages, page)
		next = nextListLink(page.header.Get("Link"))
	}
	return pages, nil
}

// bearerAuthHeader is the one-header Authorization set of a token ride.
func bearerAuthHeader(token string) http.Header {
	return http.Header{"Authorization": []string{"Bearer " + token}}
}

// nextListLink extracts the rel="next" target of an RFC 8288 Link header
// (the pagination grammar the registry list endpoints emit:
// `<https://reg/v2/_catalog?last=x>; rel="next"`). Empty when the header
// carries no next relation — the aggregation's stop signal.
func nextListLink(header string) string {
	for _, field := range strings.Split(header, ",") {
		trimmed := strings.TrimSpace(field)
		target, params, found := strings.Cut(trimmed, ">")
		if !found {
			continue
		}
		target = strings.TrimPrefix(strings.TrimSpace(target), "<")
		for _, param := range strings.Split(params, ";") {
			key, value, found := strings.Cut(param, "=")
			if !found || strings.TrimSpace(key) != "rel" {
				continue
			}
			if strings.Trim(strings.TrimSpace(value), `"`) == "next" && target != "" {
				return target
			}
		}
	}
	return ""
}

// attempt runs one buffered request: the cached token when present (via
// the credential-free client), else the repository credential. usedToken
// reports which shape rode the wire.
func (e *remoteSessionEntry) attempt(ctx context.Context, fetch func(context.Context, *remote.Client, remote.Request) (*upstreamAnswer, error), wire, scope string, hdr http.Header) (*upstreamAnswer, bool, error) {
	if token := e.token(scope); token != "" {
		res, err := e.attemptWithToken(ctx, fetch, wire, token, hdr)
		return res, true, err
	}
	res, err := fetch(ctx, e.authed, remote.Request{Path: wire, Header: hdr})
	return res, false, err
}

// attemptWithToken runs one Bearer-authenticated buffered request through
// the credential-free client.
func (e *remoteSessionEntry) attemptWithToken(ctx context.Context, fetch func(context.Context, *remote.Client, remote.Request) (*upstreamAnswer, error), wire, token string, hdr http.Header) (*upstreamAnswer, error) {
	h := hdr.Clone()
	if h == nil {
		h = http.Header{}
	}
	h.Set("Authorization", "Bearer "+token)
	return fetch(ctx, e.anon, remote.Request{Path: wire, Header: h})
}

// buffered issues one GET and buffers the body under the manifest ceiling.
func (e *remoteSessionEntry) buffered(ctx context.Context, c *remote.Client, req remote.Request) (*upstreamAnswer, error) {
	res, err := c.Fetch(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return &upstreamAnswer{status: res.StatusCode, statusT: res.Status, header: res.Header}, nil
	}
	if len(res.Body) > manifestMaxBytes {
		return nil, fmt.Errorf("upstream manifest exceeds the %d byte limit: %w", manifestMaxBytes, remote.ErrBodyTooLarge)
	}
	return &upstreamAnswer{status: res.StatusCode, statusT: res.Status, header: res.Header, body: res.Body}, nil
}

// attemptStream runs one streaming request (the cached-token shape first).
func (e *remoteSessionEntry) attemptStream(ctx context.Context, wire, scope string) (*remote.StreamResult, bool, error) {
	if token := e.token(scope); token != "" {
		res, err := e.anon.Stream(ctx, bearerRequest(wire, token))
		return res, true, err
	}
	res, err := e.authed.Stream(ctx, remote.Request{Path: wire})
	return res, false, err
}

// attemptStreamWithToken runs one Bearer-authenticated streaming request.
func (e *remoteSessionEntry) attemptStreamWithToken(ctx context.Context, wire, token string) (*remote.StreamResult, error) {
	return e.anon.Stream(ctx, bearerRequest(wire, token))
}

// bearerRequest builds one Bearer-authenticated request.
func bearerRequest(wire, token string) remote.Request {
	return remote.Request{Path: wire, Header: bearerAuthHeader(token)}
}

// dance runs the exchange+retry half of the Bearer flow shared by the
// fetch arms: drop a refused cached token, exchange at the challenge's
// realm with the repository credential, cache the answer, retry exactly
// once with the fresh token.
func (e *remoteSessionEntry) dance(ctx context.Context, challenge *bearerChallenge, scope string, retry func(token string) (*upstreamAnswer, error)) (*upstreamAnswer, error) {
	e.dropToken(scope)
	token, terr := e.exchange(ctx, challenge, scope)
	if terr != nil {
		return nil, fmt.Errorf("%w: token exchange: %w", errUpstreamAuth, terr)
	}
	e.storeToken(scope, token)
	return retry(token.value)
}

// token returns the cached bearer for one scope ("" when none stands).
func (e *remoteSessionEntry) token(scope string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.tokens[scope]
	if t == nil || !time.Now().Before(t.expiresAt) {
		return ""
	}
	return t.value
}

// storeToken caches one exchanged bearer.
func (e *remoteSessionEntry) storeToken(scope string, t *remoteBearerToken) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tokens[scope] = t
}

// dropToken invalidates one scope's cached bearer (an upstream that
// refused a token it issued mid-window).
func (e *remoteSessionEntry) dropToken(scope string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.tokens, scope)
}

// bearerChallenge is one parsed WWW-Authenticate: Bearer challenge.
type bearerChallenge struct {
	realm   string
	service string
	scope   string
}

// parseBearerChallenge parses `Bearer realm="...",service="...",scope="..."`
// (every field optional, values optionally quoted — the distribution
// spec's auth challenge grammar). Non-Bearer challenges answer nil.
func parseBearerChallenge(header string) *bearerChallenge {
	rest, ok := strings.CutPrefix(strings.TrimSpace(header), "Bearer")
	if !ok {
		return nil
	}
	ch := &bearerChallenge{}
	for _, field := range splitChallengeFields(rest) {
		key, value, found := strings.Cut(field, "=")
		if !found {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "realm":
			ch.realm = value
		case "service":
			ch.service = value
		case "scope":
			ch.scope = value
		}
	}
	return ch
}

// splitChallengeFields splits a challenge's comma-separated fields.
func splitChallengeFields(s string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			b.WriteByte(c)
		case c == ',' && !inQuote:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

// bearerTokenResponse is the token endpoint's body (token or access_token
// — the distribution token spec allows both spellings).
type bearerTokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// exchange performs the token dance's exchange: GET the challenge's realm
// with the repository credential (the docker-remote posture — the same
// Basic pair the first attempt carried; an anonymous repository exchanges
// anonymously), service/scope echoed from the challenge.
func (e *remoteSessionEntry) exchange(ctx context.Context, ch *bearerChallenge, fallbackScope string) (*remoteBearerToken, error) {
	q := url.Values{}
	if ch.service != "" {
		q.Set("service", ch.service)
	}
	scope := ch.scope
	if scope == "" {
		scope = fallbackScope
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	target := ch.realm
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + q.Encode()
	}
	res, err := e.authed.Fetch(ctx, remote.Request{URL: target})
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint answered %d %s", res.StatusCode, res.Status)
	}
	var body bearerTokenResponse
	if jerr := json.Unmarshal(res.Body, &body); jerr != nil {
		return nil, fmt.Errorf("token endpoint body: %w", jerr)
	}
	value := body.Token
	if value == "" {
		value = body.AccessToken
	}
	if value == "" {
		return nil, errors.New("token endpoint returned no token")
	}
	ttl := time.Duration(body.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = bearerTokenCacheTTL
	}
	if ttl > bearerTokenMaxTTL {
		ttl = bearerTokenMaxTTL
	}
	return &remoteBearerToken{value: value, expiresAt: time.Now().Add(ttl)}, nil
}

// drainUpstream discards one unanswered upstream body (bounded) and closes
// it so the pooled connection survives.
func drainUpstream(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 8<<10))
	_ = body.Close()
}

// ---- the serving plane ----

// remoteUnfound carries the unfound family's wire shape of one reference
// (the error code and message the degradation matrix answers with).
type remoteUnfound struct {
	code    string
	message string
	detail  map[string]string
}

// manifestUnfound is the manifest arm's unfound shape — the observed
// Artifactory body verbatim (L000-B C12/E6-1): a STATIC message and a
// detail whose key is "manifest" carrying the IMAGE path (the name
// without the tag/digest reference), never the raw reference.
func manifestUnfound(image string) remoteUnfound {
	return remoteUnfound{
		code:    ErrCodeManifestUnknown,
		message: "The named manifest is not known to the registry.",
		detail:  map[string]string{"manifest": image},
	}
}

// blobUnfound is the blob arm's unfound shape — the observed Artifactory
// body verbatim (L000-B C13/E6-2): a STATIC message (no digest suffix)
// and a detail whose key is "blobSum", the docker v2 blob-addressing
// spelling.
func blobUnfound(digest string) remoteUnfound {
	return remoteUnfound{
		code:    ErrCodeBlobUnknown,
		message: "blob unknown to registry",
		detail:  map[string]string{"blobSum": digest},
	}
}

// write answers the unfound shape, optionally with an upstream summary.
func (u remoteUnfound) write(w http.ResponseWriter, summary string) {
	msg := u.message
	if summary != "" {
		msg = fmt.Sprintf("%s (%s)", u.message, summary)
	}
	writeSpecError(w, http.StatusNotFound, u.code, msg, u.detail)
}

// serveRemoteRoute answers every request the /v2 plane receives against a
// REMOTE repository row (T-363): reads proxy pull-through, writes answer
// Artifactory's upload refusal (L000-B C11/E5: 400 + the generic error
// model's per-endpoint copy — "Unable to upload blobs/a manifest to..." —
// answered here BEFORE any upload session is minted).
func (h *Handler) serveRemoteRoute(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.refuseRemoteWrite(w, r, ref)
		return
	}
	switch {
	case strings.HasPrefix(ref.tail, blobsTail):
		h.serveRemoteBlob(w, r, ref, ref.tail)
	case strings.HasPrefix(ref.tail, manifestsTail):
		h.serveRemoteManifest(w, r, ref, ref.tail)
	case ref.tail == tagsListTail:
		h.serveRemoteTagsList(w, r, ref)
	default:
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"registry route /v2/"+ref.repoKey+"/"+ref.image+"/"+ref.tail+" is not implemented in BinFlow M2 yet", nil)
	}
}

// refuseRemoteWrite answers every write verb on the remote plane with the
// observed Artifactory shape (L000-B C11, evidence E5-1..3): 400 plus the
// generic error model's message per endpoint family —
//
//   - POST/PATCH/PUT on blobs/uploads*: "Unable to upload blobs to a
//     remote repository."
//   - PUT on manifests/<ref>: "Unable to upload a manifest to a remote
//     repository."
//   - DELETE on manifests/<ref>: "Unable to delete a manifest from a
//     remote repository."
//
// Verb/path combinations the evidence never exercised (a blob DELETE, an
// upload-session cancel) keep the standing 405 + Allow: GET refusal —
// RE-05's read-only contract still holds for them, unobserved shape
// unchanged.
func (h *Handler) refuseRemoteWrite(w http.ResponseWriter, r *http.Request, ref nameRef) {
	switch {
	case strings.HasPrefix(ref.tail, uploadsTailPrefix) &&
		(r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut):
		writeStatusFormError(w, http.StatusBadRequest, "Unable to upload blobs to a remote repository.")
	case strings.HasPrefix(ref.tail, manifestsTail) && r.Method == http.MethodPut:
		writeStatusFormError(w, http.StatusBadRequest, "Unable to upload a manifest to a remote repository.")
	case strings.HasPrefix(ref.tail, manifestsTail) && r.Method == http.MethodDelete:
		writeStatusFormError(w, http.StatusBadRequest, "Unable to delete a manifest from a remote repository.")
	default:
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", ref.repoKey), nil)
	}
}

// serveRemoteTagsList implements the remote plane's tags/list (L000-B
// C15, evidence E7-1): the listing is the LIVE upstream aggregation — the
// tags pages fetched from the upstream every call (pretty body, the name
// the upstream itself reported — Artifactory echoes its own normalized
// image name, e.g. docker.io's library/ prefix — dictionary-sorted full
// tag set), NOT the local cache's tag rows (the fallback-to-cache switch
// defaults off, E7's key table). A failed head page (transport fault,
// non-200, unparseable body) answers 200 with "tags":null — the same
// empty-200 degradation the catalog arm observes (E7-2); the exact
// null-vs-empty spelling of this unobserved corner is BinFlow's own (the
// local plane's R4 empty form).
func (h *Handler) serveRemoteTagsList(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("method %s is not supported on tags/list", r.Method), nil)
		return
	}
	// The remote face's own n semantics (remote_face.go): the invalid-n
	// 404 answers BEFORE any upstream contact.
	n, ok := remotePageSize(w, r)
	if !ok {
		return
	}
	last := r.URL.Query().Get("last")
	ctx := r.Context()
	p := principalOf(r)
	entry, _, serr := h.remoteSession(ctx, p, ref.repoKey)
	if serr != nil {
		h.writeRemoteServeError(w, r, serr, ref)
		return
	}
	name, tags := ref.image, []string(nil)
	pages, err := entry.fetchList(ctx, "/v2/"+ref.image+"/"+tagsListTail, "repository:"+ref.image+":"+scopeActionPull)
	if err != nil {
		h.log.WarnContext(ctx, "docker remote: upstream tags/list failed",
			"repo", ref.repoKey, "image", ref.image, "error", err.Error())
	} else if first := pages[0]; first.status == http.StatusOK {
		var head tagsBody
		if jerr := json.Unmarshal(first.body, &head); jerr != nil {
			h.log.WarnContext(ctx, "docker remote: upstream tags/list body unparseable",
				"repo", ref.repoKey, "image", ref.image, "error", jerr.Error())
		} else {
			if head.Name != "" {
				name = head.Name
			}
			tags = append(tags, head.Tags...)
			for _, page := range pages[1:] {
				var next tagsBody
				if jerr := json.Unmarshal(page.body, &next); jerr == nil {
					tags = append(tags, next.Tags...)
				}
			}
			sort.Strings(tags)
		}
	} else {
		h.log.WarnContext(ctx, "docker remote: upstream tags/list answered non-200",
			"repo", ref.repoKey, "image", ref.image, "status", first.status)
	}
	// The client's n/last window over the aggregated FULL list (L003-2):
	// n=0 (absent) serves everything, an explicit page truncates with the
	// Link rel="next" header exactly while a further page exists (the
	// remote face ABSORBS the upstream's own pagination and re-windows it
	// under its own path — captures a_tagsn.h/a_tagsl.h).
	if n > 0 {
		var more bool
		tags, more = slicePage(tags, last, n)
		if more && len(tags) > 0 {
			w.Header().Set("Link", nextPageLink(tagsListPath(ref), tags[len(tags)-1], n))
		}
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(tagsBody{Name: name, Tags: tags})
}

// serveRepoCatalog implements the repo-domain catalog route
// /v2/<repoKey>/_catalog (L000-B C16, evidence E7-2): 200 always — the
// repositories of the LIVE upstream catalog when the upstream serves one
// (Link pages aggregated), an EMPTY list when it does not (docker.io has
// no /v2/_catalog; Artifactory answers 200 {"repositories":[]} after the
// upstream refusal — the fallback-to-cache switch defaults off). The
// route is intercepted before the name parser (a "_catalog" image slot
// carries no registry route); non-remote rows keep that standing 404.
func (h *Handler) serveRepoCatalog(w http.ResponseWriter, r *http.Request, path, repoKey string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("method %s is not supported on the catalog", r.Method), nil)
		return
	}
	p := principalOf(r)
	if p == nil && !h.opts.AnonymousAccess {
		h.challenge(w, r, scopeRegistryCatalog)
		return
	}
	ctx := r.Context()
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		h.log.ErrorContext(ctx, "docker: repository lookup failed",
			"repo", repoKey, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"repository lookup failed", nil)
		return
	}
	if row == nil || !servesV2Plane(row.PackageType()) {
		writeSpecError(w, http.StatusNotFound, ErrCodeNameUnknown,
			fmt.Sprintf("repository name not known to registry: %q", repoKey), nil)
		return
	}
	if !h.authorizeRoute(w, r, nameRef{repoKey: repoKey}) {
		return
	}
	if row.Class() != repo.TypeRemote {
		// The aggregation face is the remote plane's; every other class
		// keeps the standing route shape this path always answered (the
		// name parser's not-a-name 404 — reproduced verbatim).
		_, perr := parseV2Name(path) // always a notAName rejection here
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, perr.Error(), nil)
		return
	}
	entry, _, serr := h.remoteSession(ctx, p, repoKey)
	if serr != nil {
		h.writeRemoteServeError(w, r, serr, nameRef{repoKey: repoKey})
		return
	}
	// The same n/last contract as the tags face (remote_face.go), on the
	// repo-domain catalog path.
	n, ok := remotePageSize(w, r)
	if !ok {
		return
	}
	last := r.URL.Query().Get("last")
	repositories := []string{}
	pages, err := entry.fetchList(ctx, catalogPath, scopeRegistryCatalog)
	switch {
	case err != nil:
		h.log.WarnContext(ctx, "docker remote: upstream catalog failed",
			"repo", repoKey, "error", err.Error())
	case pages[0].status != http.StatusOK:
		h.log.WarnContext(ctx, "docker remote: upstream catalog answered non-200",
			"repo", repoKey, "status", pages[0].status)
	default:
		for _, page := range pages {
			var body catalogBody
			if jerr := json.Unmarshal(page.body, &body); jerr == nil {
				repositories = append(repositories, body.Repositories...)
			}
		}
		sort.Strings(repositories)
	}
	if n > 0 {
		var more bool
		repositories, more = slicePage(repositories, last, n)
		if more && len(repositories) > 0 {
			// The reference's repo-domain catalog Link points at the
			// REGISTRY-LEVEL /v2/_catalog path (live capture: `</v2/_catalog?
			// last=…&n=…>; rel="next"` served from /v2/<repoKey>/_catalog) —
			// a reference quirk copied verbatim, not a typo.
			w.Header().Set("Link", nextPageLink(catalogPath, repositories[len(repositories)-1], n))
		}
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(catalogBody{Repositories: repositories})
}

// remotePlane resolves the service's v2 remote capability; nil when the
// assembled service predates the seam (a bare test double).
func (h *Handler) remotePlane() repo.RemoteV2Plane {
	plane, _ := h.svc.(repo.RemoteV2Plane)
	return plane
}

// remoteSession resolves the repository's upstream session through the
// service seam (facts + read gate) and the client pool.
func (h *Handler) remoteSession(ctx context.Context, p *Principal, repoKey string) (*remoteSessionEntry, *repo.RemoteUpstream, error) {
	plane := h.remotePlane()
	if plane == nil {
		return nil, nil, fmt.Errorf("docker remote %s: the service carries no v2 remote plane", repoKey)
	}
	facts, err := plane.RemoteUpstream(ctx, p, repoKey)
	if err != nil {
		return nil, nil, err
	}
	if facts.BlockedOut {
		return nil, facts, &repo.StatusError{
			Code: http.StatusNotFound,
			Message: fmt.Sprintf("The repository '%s' is blacked out and cannot serve content.",
				repoKey),
		}
	}
	entry, err := h.remotes.forRepo(repoKey, facts)
	if err != nil {
		return nil, facts, err
	}
	return entry, facts, nil
}

// remoteStanding is the local cache state one remote read resolved: the
// standing copy (nil when nothing is cached) plus its serving metadata.
type remoteStanding struct {
	node      *metadata.Node
	mediaType string
	dgst      string
	size      int64
}

// serveRemoteManifest implements the manifest pull-through for a remote
// repository: resolve locally (tag row / manifest row + cache probe), then
// the upstream conversation, then landing + index rows, with the engine's
// degradation matrix on upstream faults (expired copy served STALE with
// the X-Binflow-Upstream-Error marker; nothing cached answers unfound,
// never a naked 5xx).
func (h *Handler) serveRemoteManifest(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	reference, ok := strings.CutPrefix(tail, manifestsTail)
	if !ok || reference == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown manifest route "+tail, nil)
		return
	}
	unfound := manifestUnfound(ref.image)
	// The reference shape runs BEFORE anything else (the local plane's
	// rule): a sha256: spelling must parse, anything else must be a legal
	// tag.
	isDigestRef := strings.HasPrefix(reference, digestPrefix)
	var wantHex string
	if isDigestRef {
		hexPart, err := parseDigestParam(reference)
		if err != nil {
			writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
				fmt.Sprintf("manifest reference %q is not a valid sha256 digest", reference),
				map[string]string{"digest": reference})
			return
		}
		wantHex = hexPart
	} else if err := validateManifestTag(reference); err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeManifestInvalid,
			fmt.Sprintf("manifest reference %q is neither a digest nor a legal tag: %v", reference, err), nil)
		return
	}

	ctx := r.Context()
	p := principalOf(r)
	face := h.manifestFace(ref, reference)
	plane := h.remotePlane()
	if plane == nil {
		h.log.ErrorContext(ctx, "docker: remote manifest without the v2 plane seam", "repo", ref.repoKey)
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"the registry's service carries no v2 remote plane", nil)
		return
	}

	// Local resolution: the tag/manifest rows plus the cache probe decide
	// HIT / STALE / negative / miss.
	var standing *remoteStanding
	dgst, mediaType, size, rerr := h.resolveRemoteRef(r, ref, reference)
	switch {
	case rerr == nil:
		probe, perr := plane.ProbeRemoteCache(ctx, p, ref.repoKey, manifestNodePath(ref.image, dgst))
		if perr != nil {
			h.writeManifestReadError(w, r, perr, ref, reference)
			return
		}
		switch probe.State {
		case repo.RemoteProbeHit:
			h.serveRemoteManifestCopy(w, r, face, probe.Node, mediaType, dgst, size, remote.CacheHit, "")
			return
		case repo.RemoteProbeNegative:
			unfound.write(w, "")
			return
		}
		standing = &remoteStanding{node: probe.Node, mediaType: mediaType, dgst: dgst, size: size}
	case isMissError(rerr):
		// No local rows: the cold-miss negative memory decides first (C14,
		// the L004-2 verdict — the reference answers repeated misses locally
		// inside missedRetrievalCachePeriodSecs): a fresh miss record for
		// THIS reference was written by an earlier upstream 404 and answers
		// the unfound family with zero upstream contact until it expires.
		missProbe, mperr := plane.ProbeRemoteCache(ctx, p, ref.repoKey, manifestMissNodePath(ref.image, reference, isDigestRef, wantHex))
		if mperr != nil {
			h.writeManifestReadError(w, r, mperr, ref, reference)
			return
		}
		if missProbe.State == repo.RemoteProbeNegative {
			unfound.write(w, "")
			return
		}
		// The upstream conversation decides.
	default:
		h.writeManifestReadError(w, r, rerr, ref, reference)
		return
	}

	entry, _, serr := h.remoteSession(ctx, p, ref.repoKey)
	if serr != nil {
		h.writeRemoteServeError(w, r, serr, ref)
		return
	}
	fetched, ferr := entry.fetchManifest(ctx, ref.image, reference, r.Header.Values("Accept"),
		revalidationValidator(standing, isDigestRef))
	if ferr != nil {
		var serveStale func(string)
		if standing != nil && standing.node != nil {
			mediaType, size := standing.mediaType, standing.size
			if mediaType == "" {
				mediaType = standing.node.Mime
			}
			if size == 0 {
				size = standing.node.Size
			}
			serveStale = func(summary string) {
				h.serveRemoteManifestCopy(w, r, face, standing.node, mediaType, standing.dgst, size, remote.CacheStale, summary)
			}
		}
		h.writeRemoteFetchFault(w, r, ferr, ref, unfound, serveStale)
		return
	}
	switch fetched.status {
	case http.StatusOK:
		h.landRemoteManifest(w, r, ref, reference, isDigestRef, wantHex, fetched, face)
		return
	case http.StatusNotModified:
		// The revalidation arm's answer (§5.3): upstream confirmed the
		// standing copy — slide the window, serve it REVALIDATED. An
		// unsolicited 304 (no standing copy was offered a validator) is the
		// anomaly family below it.
		if standing != nil && standing.node != nil {
			h.serveRevalidatedManifest(w, r, ref, standing, func(mediaType string, body io.Reader) error {
				_, lerr := plane.LandRemoteBlob(ctx, p, ref.repoKey, manifestNodePath(ref.image, standing.dgst), standing.dgst, mediaType, body)
				return lerr
			}, func(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
				h.serveRemoteManifestCopy(w, r, face, node, mediaType, dgst, size, cacheState, upstreamError)
			})
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered 304 %s without a validator being offered", fetched.statusT))
	case http.StatusNotFound:
		// The engine's step: record the miss (digest-keyed paths only),
		// then an expired copy still serves (STALE).
		if standing != nil && standing.node != nil {
			_ = plane.CacheRemoteMiss(ctx, p, ref.repoKey, manifestNodePath(ref.image, standing.dgst)) //nolint:errcheck // best-effort bookkeeping; the serve stands
			h.serveRemoteManifestCopy(w, r, face, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, "upstream 404 (expired copy served)")
			return
		}
		// The cold miss records the reference-keyed miss row (C14): the
		// next ask of the same tag/digest answers locally inside the
		// missedTTL window, then re-asks the upstream — the standing arm's
		// digest-keyed write above is the same memory for a copy that
		// expired between asks.
		_ = plane.CacheRemoteMiss(ctx, p, ref.repoKey, manifestMissNodePath(ref.image, reference, isDigestRef, wantHex)) //nolint:errcheck // best-effort bookkeeping; the serve stands
		unfound.write(w, "")
	case http.StatusUnauthorized, http.StatusForbidden:
		// Credentials refused upstream: unfound with the summary (the
		// engine's 401/403 posture — no negative cache, the credential
		// state is correctable).
		if standing != nil && standing.node != nil {
			h.serveRemoteManifestCopy(w, r, face, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, fmt.Sprintf("upstream %d %s", fetched.status, fetched.statusT))
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered %d %s; credentials refused or insufficient", fetched.status, fetched.statusT))
	default:
		// Other 4xx/5xx and anomalies: degrade like the engine's offline
		// arm — stale copy with the marker, unfound without one.
		if standing != nil && standing.node != nil {
			h.serveRemoteManifestCopy(w, r, face, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, fmt.Sprintf("upstream %d %s", fetched.status, fetched.statusT))
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered %d %s", fetched.status, fetched.statusT))
	}
}

// remoteManifestSink owns WHERE one fetched manifest lands (T-365): the
// direct remote plane sinks into its own repository through the
// RemoteV2Plane seam; a virtual walk sinks into the winning remote MEMBER
// through the V2VirtualPlane seam. The landing semantics — the digest
// verdicts, the checksum-addressed node, the index rows — are one code
// path; only the addressing differs.
type remoteManifestSink struct {
	land   func(path, expectHex, mime string, body io.Reader) (*metadata.Node, error)
	record func(dgst, tag, mediaType string, size int64, refs []*metadata.DockerRef) error
}

// manifestCopyServer is the manifest copy serve both landing arms share
// (serveRemoteManifestCopy's own signature).
type manifestCopyServer func(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string)

// landRemoteManifest lands one fetched manifest for the DIRECT remote
// plane: the sink addresses the repository the request named.
func (h *Handler) landRemoteManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string, isDigestRef bool, wantHex string, fetched *upstreamAnswer, face remoteFace) {
	plane := h.remotePlane()
	if plane == nil {
		h.log.ErrorContext(r.Context(), "docker: remote manifest without the v2 plane seam", "repo", ref.repoKey)
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"the registry's service carries no v2 remote plane", nil)
		return
	}
	p := principalOf(r)
	h.landFetchedManifest(w, r, ref, reference, isDigestRef, wantHex, fetched, remoteManifestSink{
		land: func(path, expectHex, mime string, body io.Reader) (*metadata.Node, error) {
			return plane.LandRemoteBlob(r.Context(), p, ref.repoKey, path, expectHex, mime, body)
		},
		record: func(dgst, tag, mediaType string, size int64, refs []*metadata.DockerRef) error {
			return plane.RecordRemoteManifest(r.Context(), p, ref.repoKey, ref.image, dgst, tag, mediaType, size, refs)
		},
	}, func(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
		h.serveRemoteManifestCopy(w, r, face, node, mediaType, dgst, size, cacheState, upstreamError)
	})
}

// landFetchedManifest is the shared landing of one upstream manifest
// answer: digest verified (the computed sha256 is the only addressing
// truth; a by-digest request whose body disagrees is an upstream anomaly,
// answered 502, never landed), node landed checksum-addressed, index rows
// recorded (the tag when the request was a tag), then served MISS through
// the arm's own copy server.
func (h *Handler) landFetchedManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string, isDigestRef bool, wantHex string, fetched *upstreamAnswer, sink remoteManifestSink, serve manifestCopyServer) {
	ctx := r.Context()

	dgst := sha256HexOf(fetched.body)
	if declared := fetched.header.Get(hdrContentDigest); declared != "" && declared != digestPrefix+dgst {
		// Registered, not enforced — the engine's declared-checksum
		// posture; the measured digest remains the addressing truth.
		h.log.WarnContext(ctx, "docker remote: upstream manifest digest differs from the measured one (registered, not enforced)",
			"repo", ref.repoKey, "image", ref.image, "reference", reference,
			"declared", declared, "actual", digestPrefix+dgst)
	}
	if isDigestRef && wantHex != dgst {
		h.log.ErrorContext(ctx, "docker remote: upstream served the wrong manifest for a digest request",
			"repo", ref.repoKey, "image", ref.image, "wanted", wantHex, "served", dgst)
		writeSpecError(w, http.StatusBadGateway, ErrCodeUnknown,
			fmt.Sprintf("upstream served manifest %s for reference %s", digestPrefix+dgst, reference), nil)
		return
	}
	mediaType := mediaTypeOfHeader(fetched.header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = mimeOctetStream
	}
	if !acceptAllows(r.Header.Values("Accept"), mediaType) {
		writeSpecError(w, http.StatusNotFound, ErrCodeManifestUnknown,
			fmt.Sprintf("manifest %s is not available in an accepted media type (stored: %s)", reference, mediaType),
			map[string]string{"mediaType": mediaType})
		return
	}

	node, lerr := sink.land(manifestNodePath(ref.image, dgst), dgst, mediaType, bytes.NewReader(fetched.body))
	if lerr != nil {
		h.writeRemoteServeError(w, r, lerr, ref)
		return
	}
	// The ref edges are best-effort cache population (helm.md 8.3: the
	// upstream is the structural authority; a body this plane cannot parse
	// still serves verbatim). The NEGOTIATED media type feeds the parse —
	// the refs ledger is the ADR-0047 chain gate's substrate, and an
	// untyped parse would silently starve it (best-effort means the failure
	// must at least be real, not an always-on input bug).
	refs, perr := remoteManifestRefs(mediaType, fetched.body)
	if perr != nil {
		// The parse-failure dual of the unrecorded-refs WARN below (L003-2
		// review A): the body still serves verbatim (pass-through), but no
		// ref rows exist for it, so the ADR-0047 chain gate will refuse this
		// manifest's uncached blobs — the degradation must be visible in the
		// log, not inferred from later 404s.
		h.log.WarnContext(ctx, "docker remote: manifest refs not parsed (serving verbatim; the chain gate will not admit this manifest's uncached blobs)",
			"repo", ref.repoKey, "image", ref.image, "digest", dgst,
			"cache_result", "refs-unparsed", "error", perr.Error())
	}
	tag := ""
	if !isDigestRef {
		tag = reference
	}
	if rerr := sink.record(dgst, tag, mediaType, int64(len(fetched.body)), refs); rerr != nil {
		// The chain gate consumes these rows (ADR-0047): a manifest whose
		// refs never landed makes its uncached blobs locally unfetchable —
		// the same coupling Artifactory's marker-write failure carries
		// (INTENTIONAL, ADR-0047 edge ①). The WARN carries the engine's
		// cache_result field so an operator alerting on that line sees the
		// degradation; best-effort stands (the landed copy serves).
		h.log.WarnContext(ctx, "docker remote: manifest index rows not recorded (serving the landed copy; the chain gate will refuse this manifest's uncached blobs)",
			"repo", ref.repoKey, "image", ref.image, "digest", dgst,
			"cache_result", "chain-unrecorded", "error", rerr.Error())
	}
	serve(w, r, node, mediaType, dgst, int64(len(fetched.body)), remote.CacheMiss, "")
}

// remoteManifestRefs extracts a fetched manifest's descriptor digests
// (best-effort: the pass-through parse the local plane owns; a body that
// does not parse against mediaType answers no refs AND the parse error —
// the caller logs it, the upstream already served the body). These rows
// are the ADR-0047 chain gate's ledger.
func remoteManifestRefs(mediaType string, body []byte) ([]*metadata.DockerRef, error) {
	parsed, err := parseManifest(mediaType, body)
	if err != nil {
		return nil, err
	}
	out := make([]*metadata.DockerRef, 0, len(parsed.refs))
	seen := make(map[string]struct{}, len(parsed.refs))
	for _, mr := range parsed.refs {
		hexPart, perr := parseDigestParam(mr.Digest)
		if perr != nil {
			continue
		}
		if _, dup := seen[hexPart]; dup {
			continue
		}
		seen[hexPart] = struct{}{}
		out = append(out, &metadata.DockerRef{BlobDigest: hexPart, ChildMediaType: mr.MediaType})
	}
	return out, nil
}

// serveRemoteManifestCopy streams one landed/expired manifest copy with
// the read contract (Docker-Content-Digest, the stored media type, the
// cache markers) and the remote face's full artifact header set (L003-2,
// capture a_mf.h/a_mfh2.h): the checksum family, Etag (=sha1 unquoted),
// Last-Modified (=the cache-landing timestamp), Accept-Ranges, the
// Content-Disposition/X-Artifactory-Filename pair and
// X-Artifactory-Origin-Remote-Path; HEAD adds X-Artifactory-Docker-Registry
// (the HEAD face's own header — the live capture's GET set plus it, a
// superset the contract's HEAD expectations read as present-tolerated).
// HEAD carries the headers only.
func (h *Handler) serveRemoteManifestCopy(w http.ResponseWriter, r *http.Request, face remoteFace, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
	// The ledger row feeds both the conditional arm and the checksum face —
	// one lookup, two consumers.
	var etag, md5 string
	if row := h.ledgerRow(r.Context(), dgst); row != nil {
		etag, md5 = row.Sha1, row.Md5
	}
	// The client-conditional arm (L004-1, the live reference's fresh-window
	// matrix): a HIT copy answers the manifest face's BARE 304 when the
	// client's validator matches — api-version and the cache marker alone,
	// no validators, no body. Every non-HIT serve (the revalidation, stale
	// and just-fetched arms) answers 200 full without evaluating the
	// client's conditionals, the live reference's own revalidation posture.
	if cacheState == remote.CacheHit && clientConditionalNotModified(r, etag, nodeLastModified(node)) {
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		hdr.Set(remote.HdrCacheState, remote.CacheHit)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rc, err := h.openRemoteBlob(r.Context(), node)
	if err != nil {
		h.log.ErrorContext(r.Context(), "docker remote: open cached manifest",
			"repo", node.RepoKey, "digest", dgst, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"open cached manifest: "+err.Error(), nil)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd

	hdr := w.Header()
	writeAPIVersionHdr(hdr)
	if cacheState != "" {
		hdr.Set(remote.HdrCacheState, cacheState)
	}
	if upstreamError != "" {
		hdr.Set(remote.HdrUpstreamError, upstreamError)
	}
	hdr.Set(hdrContentDigest, digestPrefix+dgst)
	if mediaType == "" {
		mediaType = mimeOctetStream
	}
	hdr.Set("Content-Type", mediaType)
	if etag != "" {
		hdr.Set(hdrChecksumSha1, etag)
		hdr.Set("ETag", etag)
	}
	if md5 != "" {
		hdr.Set(hdrChecksumMd5, md5)
	}
	hdr.Set(hdrChecksumSha256, dgst)
	if lm := nodeLastModified(node); lm != "" {
		hdr.Set("Last-Modified", lm)
	}
	hdr.Set("Accept-Ranges", "bytes")
	hdr.Set(hdrContentDisposition, `attachment; filename="`+manifestFilename(mediaType)+`"`)
	hdr.Set(hdrFilename, manifestFilename(mediaType))
	if o := face.originPath(r.Context(), principalOf(r)); o != "" {
		hdr.Set(hdrOriginRemotePath, o)
	}
	if r.Method == http.MethodHead {
		hdr.Set(hdrDockerRegistry, face.registry)
	}
	hdr.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc)
}

// serveRemoteBlob implements the blob pull-through: probe, upstream fetch
// (the dance included), landing through a STREAMING storage session with
// the digest ENFORCED (a body that is not the digest it was requested by
// never lands under that digest), the engine's degradation matrix on
// faults. Range/conditional semantics reuse the local plane's server.
func (h *Handler) serveRemoteBlob(w http.ResponseWriter, r *http.Request, ref nameRef, tail string) {
	digestParam, ok := strings.CutPrefix(tail, blobsTail)
	if !ok || digestParam == "" {
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, "unknown blob route "+tail, nil)
		return
	}
	unfound := blobUnfound(digestParam)
	hex, err := parseDigestParam(digestParam)
	if err != nil {
		writeSpecError(w, http.StatusBadRequest, ErrCodeDigestInvalid,
			fmt.Sprintf("digest %q is not a valid sha256 digest", digestParam),
			map[string]string{"digest": digestParam})
		return
	}
	if hex == emptyLayerDigestHex {
		h.serveEmptyLayer(w, r)
		return
	}

	ctx := r.Context()
	p := principalOf(r)
	face := h.blobFace(ref, hex)
	plane := h.remotePlane()
	if plane == nil {
		h.log.ErrorContext(ctx, "docker: remote blob without the v2 plane seam", "repo", ref.repoKey)
		writeSpecError(w, http.StatusServiceUnavailable, ErrCodeUnavailable,
			"the registry's service carries no v2 remote plane", nil)
		return
	}
	path := blobNodePath(ref.image, hex)
	probe, perr := plane.ProbeRemoteCache(ctx, p, ref.repoKey, path)
	if perr != nil {
		h.writeBlobReadError(w, r, perr, ref, hex)
		return
	}
	switch probe.State {
	case repo.RemoteProbeHit:
		h.serveRemoteBlobCopy(w, r, face, probe.Node, remote.CacheHit, "")
		return
	case repo.RemoteProbeNegative:
		unfound.write(w, "")
		return
	}
	var standing *remoteStanding
	if probe.Node != nil {
		standing = &remoteStanding{node: probe.Node, dgst: hex, size: probe.Node.Size}
	}

	// The marker gate (ADR-0047, C10): a cold miss must be named by a
	// manifest chain of this image — the refs ledger RecordRemoteManifest
	// keeps is the structural marker, and a digest no chain ever named
	// answers the unfound family LOCALLY: zero upstream contact, no
	// negative-cache row (the answer is deterministic, ADR-0048). A
	// standing copy never asks — the probe IS the node-existence
	// short-circuit, the landed blob its own marker.
	if standing == nil && !h.blobChainAdmits(ctx, p, ref, hex) {
		unfound.write(w, "")
		return
	}

	entry, _, serr := h.remoteSession(ctx, p, ref.repoKey)
	if serr != nil {
		h.writeRemoteServeError(w, r, serr, ref)
		return
	}
	stream, ferr := entry.fetchBlobStream(ctx, ref.image, hex)
	if ferr != nil {
		var serveStale func(string)
		if standing != nil && standing.node != nil {
			serveStale = func(summary string) {
				h.serveRemoteBlobCopy(w, r, face, standing.node, remote.CacheStale, summary)
			}
		}
		h.writeRemoteFetchFault(w, r, ferr, ref, unfound, serveStale)
		return
	}
	switch stream.StatusCode {
	case http.StatusOK:
		node, lerr := plane.LandRemoteBlob(ctx, p, ref.repoKey, path, hex,
			mediaTypeOfHeader(stream.Header.Get("Content-Type")), stream.Body)
		drainUpstream(stream.Body)
		if lerr != nil {
			h.writeRemoteServeError(w, r, lerr, ref)
			return
		}
		h.serveRemoteBlobCopy(w, r, face, node, remote.CacheMiss, "")
	case http.StatusNotFound:
		drainUpstream(stream.Body)
		_ = plane.CacheRemoteMiss(ctx, p, ref.repoKey, path) //nolint:errcheck // best-effort bookkeeping; the serve stands
		if standing != nil && standing.node != nil {
			h.serveRemoteBlobCopy(w, r, face, standing.node, remote.CacheStale, "upstream 404 (expired copy served)")
			return
		}
		unfound.write(w, "")
	default:
		drainUpstream(stream.Body)
		if standing != nil && standing.node != nil {
			h.serveRemoteBlobCopy(w, r, face, standing.node, remote.CacheStale,
				fmt.Sprintf("upstream %d %s", stream.StatusCode, stream.Status))
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered %d %s", stream.StatusCode, stream.Status))
	}
}

// chainGate resolves the service's ADR-0047 marker-gate oracle facet; nil
// when the assembled service predates the seam (a bare test double).
func (h *Handler) chainGate() repo.DigestChainGate {
	gate, _ := h.svc.(repo.DigestChainGate)
	return gate
}

// blobChainAdmits answers the ADR-0047 gate question for one digest on the
// DIRECT remote plane: false = the unfound family locally. An oracle fault
// must not wedge pulls — it admits (the upstream decides, the pre-ADR
// fetch posture) with the observability line the design asks for
// (cache_result-aligned fields, remote-cache-v2 §2.1.1).
func (h *Handler) blobChainAdmits(ctx context.Context, p *Principal, ref nameRef, hex string) bool {
	gate := h.chainGate()
	if gate == nil {
		return true
	}
	in, err := gate.BlobInChain(ctx, p, ref.repoKey, ref.image, hex)
	if err != nil {
		h.log.WarnContext(ctx, "docker remote: chain gate unavailable (fetching)",
			"repo", ref.repoKey, "image", ref.image, "path", blobNodePath(ref.image, hex),
			"cache_result", "chain-gate-error", "error", err.Error())
		return true
	}
	return in
}

// serveRemoteBlobCopy streams one blob copy through the local plane's
// server (Range, checksum family, Docker-Content-Digest) plus the cache
// markers and the remote face's artifact headers the body server does not
// own (L003-2, capture a_blob.h/a_blobr.h): Last-Modified (the
// cache-landing timestamp), the Content-Disposition/X-Artifactory-Filename
// pair (sha256__<hex>) and X-Artifactory-Origin-Remote-Path — set before
// the delegation so they ride both the 200 and the 206 window.
func (h *Handler) serveRemoteBlobCopy(w http.ResponseWriter, r *http.Request, face remoteFace, node *metadata.Node, cacheState, upstreamError string) {
	hex := digestHexOfNode(node)
	lm := nodeLastModified(node)
	blob := storage.BlobRef{Sha256: hex, Size: node.Size}
	if row := h.ledgerRow(r.Context(), blob.Sha256); row != nil {
		if row.Sha1 != "" {
			blob.Sha1 = row.Sha1
		}
		if row.Md5 != "" {
			blob.Md5 = row.Md5
		}
	}
	// The client-conditional arm (L004-1): a standing blob copy answers
	// 304 with the FULL artifact face riding it (the live capture's blob
	// 304 keeps Etag/Last-Modified/the checksum family/the filename pair —
	// the manifest face's bare 304 is NOT this face's shape), the token
	// comparison quote-insensitive, any If-None-Match blocking the date
	// arm, HEAD never conditional. No Content-Length: a 304 has no body.
	if clientConditionalNotModified(r, blob.Sha1, lm) {
		hdr := w.Header()
		writeAPIVersionHdr(hdr)
		if cacheState != "" {
			hdr.Set(remote.HdrCacheState, cacheState)
		}
		if upstreamError != "" {
			hdr.Set(remote.HdrUpstreamError, upstreamError)
		}
		hdr.Set(hdrContentDigest, digestPrefixHex(hex))
		if blob.Sha1 != "" {
			hdr.Set(hdrChecksumSha1, blob.Sha1)
			hdr.Set("ETag", blob.Sha1)
		}
		if blob.Md5 != "" {
			hdr.Set(hdrChecksumMd5, blob.Md5)
		}
		hdr.Set(hdrChecksumSha256, hex)
		hdr.Set("Accept-Ranges", "bytes")
		hdr.Set("Content-Type", mimeOctetStream)
		if lm != "" {
			hdr.Set("Last-Modified", lm)
		}
		hdr.Set(hdrContentDisposition, `attachment; filename="`+blobFilename(hex)+`"`)
		hdr.Set(hdrFilename, blobFilename(hex))
		if face.origin != "" {
			hdr.Set(hdrOriginRemotePath, face.origin)
		}
		w.WriteHeader(http.StatusNotModified)
		return
	}
	rc, err := h.openRemoteBlob(r.Context(), node)
	if err != nil {
		h.log.ErrorContext(r.Context(), "docker remote: open cached blob",
			"repo", node.RepoKey, "path", node.Path, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"open cached blob: "+err.Error(), nil)
		return
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	hdr := w.Header()
	if cacheState != "" {
		hdr.Set(remote.HdrCacheState, cacheState)
	}
	if upstreamError != "" {
		hdr.Set(remote.HdrUpstreamError, upstreamError)
	}
	if lm != "" {
		hdr.Set("Last-Modified", lm)
	}
	hdr.Set(hdrContentDisposition, `attachment; filename="`+blobFilename(hex)+`"`)
	hdr.Set(hdrFilename, blobFilename(hex))
	if o := face.originPath(r.Context(), principalOf(r)); o != "" {
		hdr.Set(hdrOriginRemotePath, o)
	}
	h.serveBlobBody(w, r, rc, blob)
}

// digestHexOfNode recovers the blob's bare hex digest off its digest-keyed
// layout path (the node row itself carries only the sha256 column, which
// IS the digest — the spelling this plane serves).
func digestHexOfNode(node *metadata.Node) string {
	if node == nil {
		return ""
	}
	return node.Sha256
}

// openRemoteBlob opens one cached blob through the storage engine (the
// session-docking seam the handler already owns — the ONLY storage access
// the /v2 plane performs outside upload sessions, architecture section
// 5.3's ruling).
func (h *Handler) openRemoteBlob(ctx context.Context, node *metadata.Node) (io.ReadSeekCloser, error) {
	if h.store == nil {
		return nil, errors.New("no storage engine wired")
	}
	rc, _, err := h.store.Open(ctx, node.Sha256)
	if err != nil {
		return nil, err
	}
	seekable, ok := rc.(io.ReadSeekCloser)
	if !ok {
		_ = rc.Close() //nolint:errcheck // best-effort close on a failed cast
		return nil, errors.New("storage backend does not support Seek")
	}
	return seekable, nil
}

// resolveRemoteRef maps the request reference onto the CACHED identity:
// ResolveTag for a tag, ResolveManifest for a digest (both admit the
// read-plane classes since T-363; a plain miss is the caller's signal to
// go upstream).
func (h *Handler) resolveRemoteRef(r *http.Request, ref nameRef, reference string) (dgst, mediaType string, size int64, err error) {
	if strings.HasPrefix(reference, digestPrefix) {
		hexPart, perr := parseDigestParam(reference)
		if perr != nil {
			return "", "", 0, fmt.Errorf("manifest reference %q: %w", reference, repo.ErrInvalidDigest)
		}
		m, merr := h.svc.ResolveManifest(r.Context(), principalOf(r), ref.repoKey, ref.image, hexPart)
		if merr != nil {
			return "", "", 0, merr
		}
		return hexPart, m.MediaType, m.Size, nil
	}
	t, terr := h.svc.ResolveTag(r.Context(), principalOf(r), ref.repoKey, ref.image, reference)
	if terr != nil {
		return "", "", 0, terr
	}
	if t.Digest == "" {
		return "", "", 0, fmt.Errorf("tag %s: %w", reference, repo.ErrManifestNotFound)
	}
	m, merr := h.svc.ResolveManifest(r.Context(), principalOf(r), ref.repoKey, ref.image, t.Digest)
	if merr != nil {
		return "", "", 0, merr
	}
	return t.Digest, m.MediaType, m.Size, nil
}

// isMissError reports whether the local resolution miss is the plain
// not-found family (anything else is an honest failure).
func isMissError(err error) bool {
	return errors.Is(err, repo.ErrTagNotFound) || errors.Is(err, repo.ErrManifestNotFound) ||
		errors.Is(err, repo.ErrImageNotFound) || errors.Is(err, repo.ErrNodeNotFound)
}

// writeRemoteServeError maps the SEAM failures (session assembly, landing,
// service refusals): a *repo.StatusError renders verbatim (the blocked-out
// 404 among them); authorization failures keep the challenge/denied
// shapes; the rest is the honest 500.
func (h *Handler) writeRemoteServeError(w http.ResponseWriter, r *http.Request, err error, ref nameRef) {
	var se *repo.StatusError
	if errors.As(err, &se) {
		h.log.WarnContext(r.Context(), "docker remote: pull-through refused",
			"repo", ref.repoKey, "status", se.Code, "error", se.Message)
		writeVerbatimStatusError(w, se)
		return
	}
	switch {
	case errors.Is(err, repo.ErrUnauthorized):
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
	case errors.Is(err, repo.ErrForbidden):
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
	case errors.Is(err, storage.ErrChecksumMismatch):
		// The enforced digest disagreed with the fetched body: an upstream
		// anomaly, never landed — the honest 502.
		h.log.ErrorContext(r.Context(), "docker remote: upstream body failed the digest check",
			"repo", ref.repoKey, "error", err.Error())
		writeSpecError(w, http.StatusBadGateway, ErrCodeUnknown,
			"upstream body failed the digest check: "+err.Error(), nil)
	default:
		h.log.ErrorContext(r.Context(), "docker remote: pull-through failed",
			"repo", ref.repoKey, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"remote pull-through failed: "+err.Error(), nil)
	}
}

// writeRemoteFetchFault maps the UPSTREAM conversation failures onto the
// engine's degradation matrix: an SSRF screening refusal answers 400; the
// buffered-body ceiling answers 502; a failed token exchange and every
// transport fault degrade — the expired copy serves STALE with the marker,
// and without one the answer is the unfound family (zero naked 5xx,
// FR-116.5). serveStale (nil when no copy stands) renders the arm's own
// stale serve with the summary already attached.
func (h *Handler) writeRemoteFetchFault(w http.ResponseWriter, r *http.Request, err error, ref nameRef, unfound remoteUnfound, serveStale func(summary string)) {
	var rej *remote.RejectionError
	switch {
	case errors.As(err, &rej):
		writeSpecError(w, http.StatusBadRequest, ErrCodeUnsupported,
			fmt.Sprintf("Cannot fetch '%s/%s': upstream target refused — private or suppressed upstream (%v)",
				ref.repoKey, ref.image, err), nil)
	case errors.Is(err, remote.ErrBodyTooLarge):
		writeSpecError(w, http.StatusBadGateway, ErrCodeUnknown,
			fmt.Sprintf("Failed to proxy '%s': %v", ref.repoKey, err), nil)
	default:
		summary := "upstream unreachable"
		if errors.Is(err, errUpstreamAuth) {
			summary = "upstream token exchange failed"
		}
		if serveStale != nil {
			serveStale(summary)
			return
		}
		h.log.WarnContext(r.Context(), "docker remote: "+summary,
			"repo", ref.repoKey, "error", err.Error())
		unfound.write(w, fmt.Sprintf("%s: %v", summary, err))
	}
}
