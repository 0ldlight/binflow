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
//   - tags/list: the cached tag rows (a tag lands with its manifest — no
//     upstream tags/list proxying, helm.md 8.3's scope line);
//   - every write verb: 405 + Allow: GET (RE-05 — remote is a read-only
//     proxy cache; cache invalidation is the REST plane's RE-06 arm).
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
// read: the image's namespace plus the wire ref (a tag verbatim, a digest
// re-prefixed with the algorithm — the storage layout keeps bare hex, the
// wire keeps the sha256: spelling).
func v2WireManifestPath(image, ref string) string {
	return image + "/manifests/" + ref
}

// v2WireBlobPath builds the upstream request path of one blob read.
func v2WireBlobPath(image, hex string) string {
	return image + "/blobs/" + digestPrefixHex(hex)
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
// The manifest body is the buffered class — the local plane's 4MB ceiling
// applies to proxied ones too.
func (e *remoteSessionEntry) fetchManifest(ctx context.Context, image, ref string, accept []string) (*upstreamAnswer, error) {
	wire := v2WireManifestPath(image, ref)
	scope := "repository:" + image + ":pull"
	hdr := http.Header{}
	for _, v := range accept {
		hdr.Add("Accept", v)
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
	return remote.Request{Path: wire, Header: http.Header{"Authorization": []string{"Bearer " + token}}}
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

// manifestUnfound is the manifest arm's unfound shape.
func manifestUnfound(reference string) remoteUnfound {
	return remoteUnfound{
		code:    ErrCodeManifestUnknown,
		message: fmt.Sprintf("manifest unknown to registry: %s", reference),
		detail:  map[string]string{"reference": reference},
	}
}

// blobUnfound is the blob arm's unfound shape.
func blobUnfound(digest string) remoteUnfound {
	return remoteUnfound{
		code:    ErrCodeBlobUnknown,
		message: fmt.Sprintf("blob unknown to registry: %s", digest),
		detail:  map[string]string{"digest": digest},
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
// RE-05's 405 with Allow: GET (mirroring refuseNonLocalWrite's wording —
// the same refusal the local write plane renders through the verbatim
// seam, answered here BEFORE any upload session is minted).
func (h *Handler) serveRemoteRoute(w http.ResponseWriter, r *http.Request, ref nameRef) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("Remote repository '%s' is a read-only proxy cache; deployments to remote repositories are not accepted.", ref.repoKey), nil)
		return
	}
	switch {
	case strings.HasPrefix(ref.tail, blobsTail):
		h.serveRemoteBlob(w, r, ref, ref.tail)
	case strings.HasPrefix(ref.tail, manifestsTail):
		h.serveRemoteManifest(w, r, ref, ref.tail)
	case ref.tail == tagsListTail:
		// The cached tag rows (ListTags admits the read plane's classes
		// since T-363): a tag lands with its manifest, so the listing is
		// the local fact base — no upstream tags/list proxying.
		h.serveTagsList(w, r, ref)
	default:
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"registry route /v2/"+ref.repoKey+"/"+ref.image+"/"+ref.tail+" is not implemented in BinFlow M2 yet", nil)
	}
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
	unfound := manifestUnfound(reference)
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
			h.serveRemoteManifestCopy(w, r, probe.Node, mediaType, dgst, size, remote.CacheHit, "")
			return
		case repo.RemoteProbeNegative:
			unfound.write(w, "")
			return
		}
		standing = &remoteStanding{node: probe.Node, mediaType: mediaType, dgst: dgst, size: size}
	case isMissError(rerr):
		// No local rows: the upstream conversation decides.
	default:
		h.writeManifestReadError(w, r, rerr, ref, reference)
		return
	}

	entry, _, serr := h.remoteSession(ctx, p, ref.repoKey)
	if serr != nil {
		h.writeRemoteServeError(w, r, serr, ref)
		return
	}
	fetched, ferr := entry.fetchManifest(ctx, ref.image, reference, r.Header.Values("Accept"))
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
				h.serveRemoteManifestCopy(w, r, standing.node, mediaType, standing.dgst, size, remote.CacheStale, summary)
			}
		}
		h.writeRemoteFetchFault(w, r, ferr, ref, unfound, serveStale)
		return
	}
	switch fetched.status {
	case http.StatusOK:
		h.landRemoteManifest(w, r, ref, reference, isDigestRef, wantHex, fetched)
		return
	case http.StatusNotFound:
		// The engine's step: record the miss (digest-keyed paths only),
		// then an expired copy still serves (STALE).
		if standing != nil && standing.node != nil {
			_ = plane.CacheRemoteMiss(ctx, p, ref.repoKey, manifestNodePath(ref.image, standing.dgst)) //nolint:errcheck // best-effort bookkeeping; the serve stands
			h.serveRemoteManifestCopy(w, r, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, "upstream 404 (expired copy served)")
			return
		}
		unfound.write(w, "")
	case http.StatusUnauthorized, http.StatusForbidden:
		// Credentials refused upstream: unfound with the summary (the
		// engine's 401/403 posture — no negative cache, the credential
		// state is correctable).
		if standing != nil && standing.node != nil {
			h.serveRemoteManifestCopy(w, r, standing.node, standing.mediaType, standing.dgst, standing.size,
				remote.CacheStale, fmt.Sprintf("upstream %d %s", fetched.status, fetched.statusT))
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered %d %s; credentials refused or insufficient", fetched.status, fetched.statusT))
	default:
		// Other 4xx/5xx and anomalies: degrade like the engine's offline
		// arm — stale copy with the marker, unfound without one.
		if standing != nil && standing.node != nil {
			h.serveRemoteManifestCopy(w, r, standing.node, standing.mediaType, standing.dgst, standing.size,
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
func (h *Handler) landRemoteManifest(w http.ResponseWriter, r *http.Request, ref nameRef, reference string, isDigestRef bool, wantHex string, fetched *upstreamAnswer) {
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
	}, h.serveRemoteManifestCopy)
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
	// still serves verbatim).
	refs := remoteManifestRefs(fetched.body)
	tag := ""
	if !isDigestRef {
		tag = reference
	}
	if rerr := sink.record(dgst, tag, mediaType, int64(len(fetched.body)), refs); rerr != nil {
		h.log.WarnContext(ctx, "docker remote: manifest index rows not recorded (serving the landed copy)",
			"repo", ref.repoKey, "image", ref.image, "digest", dgst, "error", rerr.Error())
	}
	serve(w, r, node, mediaType, dgst, int64(len(fetched.body)), remote.CacheMiss, "")
}

// remoteManifestRefs extracts a fetched manifest's descriptor digests
// (best-effort: the pass-through parse the local plane owns, degraded to
// no refs when the body does not parse — the upstream already served it).
func remoteManifestRefs(body []byte) []*metadata.DockerRef {
	parsed, err := parseManifest("", body)
	if err != nil {
		return nil
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
	return out
}

// serveRemoteManifestCopy streams one landed/expired manifest copy with
// the read contract (Docker-Content-Digest, the stored media type, the
// cache markers). HEAD carries the headers only.
func (h *Handler) serveRemoteManifestCopy(w http.ResponseWriter, r *http.Request, node *metadata.Node, mediaType, dgst string, size int64, cacheState, upstreamError string) {
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
		h.serveRemoteBlobCopy(w, r, probe.Node, remote.CacheHit, "")
		return
	case repo.RemoteProbeNegative:
		unfound.write(w, "")
		return
	}
	var standing *remoteStanding
	if probe.Node != nil {
		standing = &remoteStanding{node: probe.Node, dgst: hex, size: probe.Node.Size}
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
				h.serveRemoteBlobCopy(w, r, standing.node, remote.CacheStale, summary)
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
		h.serveRemoteBlobCopy(w, r, node, remote.CacheMiss, "")
	case http.StatusNotFound:
		drainUpstream(stream.Body)
		_ = plane.CacheRemoteMiss(ctx, p, ref.repoKey, path) //nolint:errcheck // best-effort bookkeeping; the serve stands
		if standing != nil && standing.node != nil {
			h.serveRemoteBlobCopy(w, r, standing.node, remote.CacheStale, "upstream 404 (expired copy served)")
			return
		}
		unfound.write(w, "")
	default:
		drainUpstream(stream.Body)
		if standing != nil && standing.node != nil {
			h.serveRemoteBlobCopy(w, r, standing.node, remote.CacheStale,
				fmt.Sprintf("upstream %d %s", stream.StatusCode, stream.Status))
			return
		}
		unfound.write(w, fmt.Sprintf("upstream answered %d %s", stream.StatusCode, stream.Status))
	}
}

// serveRemoteBlobCopy streams one blob copy through the local plane's
// server (Range, checksum family, Docker-Content-Digest) plus the cache
// markers.
func (h *Handler) serveRemoteBlobCopy(w http.ResponseWriter, r *http.Request, node *metadata.Node, cacheState, upstreamError string) {
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
	blob := storage.BlobRef{Sha256: digestHexOfNode(node), Size: node.Size}
	if row := h.ledgerRow(r.Context(), blob.Sha256); row != nil {
		if row.Sha1 != "" {
			blob.Sha1 = row.Sha1
		}
		if row.Md5 != "" {
			blob.Md5 = row.Md5
		}
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
				ref.repoKey, unfound.message, err), nil)
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
