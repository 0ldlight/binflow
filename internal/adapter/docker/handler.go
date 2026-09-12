package docker

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler. Docker dispatches by its protocol
// key only in M2: the /v2 root-level route resolves the adapter through
// Protocol(), and package_type="docker" repository rows route here through
// the same key once repo.Service accepts them (T-35). The repo-CLASS claim
// ("local") is deliberately NOT registered — httpapi's adapter map keys
// classes globally and a second "local" claim would shadow generic's
// content-path dispatch (first-wins becomes last-wins silently). When
// docker repos need class-keyed dispatch, the map must grow a
// per-package-type namespace first (T-35's call).
func (h *Handler) RepoTypes() []string { return []string{} }

// ServeHTTP is the /v2 business body:
//
//   - GET/HEAD /v2 and /v2/ — the version-check ping (DE-01/D04);
//   - /v2/token — the token endpoint (T-37);
//   - blob routes — uploads (three push styles, offset query, cancel) and
//     the blob read plane (T-38);
//   - manifest routes — PUT/GET/HEAD/DELETE over tags and digests (T-39);
//   - the registry-level catalog route /v2/_catalog and the per-image
//     tags/list listing with the official pagination (T-40);
//   - every other name route — the repo gate (ADR-0010 clause 3); the
//     unimplemented remainder (referrers above all) falls through to the
//     spec-body 404, the DE-16 posture for anything not implemented.
//
// The middleware chain (requestID/accessLog/recover/CORS/authenticate) has
// already run upstream; the principal arrives through
// adapter.PrincipalFrom, same as every other adapter.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	if path == "/v2" || path == "/v2/" {
		h.servePing(w, r)
		return
	}
	h.serveNameRoute(w, r, path)
}

// RenderAuthFailure shapes a refused credential for the /v2 plane
// (T-33 review B1): 401 + Bearer challenge + spec body, identical to the
// closed-instance anonymous challenge. httpapi's router reaches it through
// the narrow v2AuthFailure interface so neither package imports the other.
// T-55 exception: the token endpoint's refused credential keeps the same
// Bearer challenge header but renders Artifactory's generic error model
// body ("Bad Credentials", L000-B C02 — the OAuth-form ruling applies to
// the endpoint's parameter 400s only).
// L003-2 exception: the PING route's refused credential is its own face
// (L002-2 capture a_pingbad.h) — a Basic realm challenge plus the pretty
// "Bad Credentials" body, NOT a Bearer re-challenge: a client that
// presented credentials is told the credential failed, not that
// negotiation is needed.
// L004-1: the refused-BEARER arms split by token state (live reference
// :8082, 7.161.20) — unknown "Props Authentication Token not found",
// expired "Token failed verification: expired" — while the Basic family
// keeps "Bad Credentials"; see bearerRefusalMessage.
func (h *Handler) RenderAuthFailure(w http.ResponseWriter, r *http.Request) {
	if r.URL != nil && isTokenRoute(r.URL.EscapedPath()) {
		h.renderTokenAuthFailure(w, r)
		return
	}
	if r.URL != nil {
		if path := r.URL.EscapedPath(); path == "/v2" || path == "/v2/" {
			h.pingRefusedCredential(w, r)
			return
		}
	}
	h.challenge(w, r, "")
}

// The refused-credential message families of the reference's /v2 faces
// (L004-1 live captures a_ping_unknownbearer/a_ping_expiredbearer/
// a_ping_revokedbearer/a_pingbad): the Basic arm and every unobserved
// Bearer corner keep the L000-B C02 wording; the two OBSERVED Bearer
// states carry their own messages verbatim. The reference's fourth arm —
// "Token failed verification: revoked" — is unreachable under BinFlow's
// revocation model (revoke deletes the row, so revoked verifies as
// unknown): a model-level divergence registered in the L004-1 report.
const (
	// Wire-level message literals (parity strings probed from the reference),
	// not credentials; gosec's name heuristic misfires on the word
	// Credentials/Token — per-spec annotations below.
	msgBadCredentials     = "Bad Credentials"                      // #nosec G101 -- parity message literal
	msgPropsTokenNotFound = "Props Authentication Token not found" // #nosec G101 -- parity message literal
	msgTokenFailedExpired = "Token failed verification: expired"   // #nosec G101 -- parity message literal
)

// bearerRefusalMessage classifies one refused Bearer credential for the
// message-typed 401 arms: the middleware already refused it; the adapter
// re-verifies through the SAME TokenRegistry decision point (no second
// state machine) only to learn WHICH arm the refusal was. A non-Bearer
// scheme, a missing registry, a re-verify that races valid, and every
// unobserved corner (owner disabled, scope unusable) answer the generic
// Basic-family wording.
func (h *Handler) bearerRefusalMessage(ctx context.Context, r *http.Request) string {
	scheme, value, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || value == "" || h.tokens == nil {
		return msgBadCredentials
	}
	_, err := h.tokens.Verify(ctx, value)
	switch {
	case err == nil:
		return msgBadCredentials // lost the race with the middleware's refusal
	case errors.Is(err, auth.ErrTokenExpired):
		return msgTokenFailedExpired
	case errors.Is(err, auth.ErrTokenUnknown):
		return msgPropsTokenNotFound
	default:
		return msgBadCredentials
	}
}

// pingRefusedCredential renders the ping route's refused-credential arm
// (capture a_pingbad.h + the L004-1 Bearer captures): `Basic realm=
// "Artifactory Realm"` — the realm string verbatim from the reference,
// what a docker client surfaces in its login prompt — plus the generic
// error model's pretty body with the arm's own message and the ping
// face's charset Content-Type.
func (h *Handler) pingRefusedCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Artifactory Realm"`)
	writeStatusFormErrorCT(w, http.StatusUnauthorized, h.bearerRefusalMessage(r.Context(), r), contentTypeJSONCharset)
}

// servePing implements DE-01 (D44-1/C6 errata form): an AUTHENTICATED
// ping answers 200 {} with the api-version header; every unauthenticated
// ping — on an anonymous-open instance too — answers 401 with the Bearer
// challenge (realm = <base>/v2/token, service = the request's host,
// ADR-0010 clause 4 as aligned by L000-B C01). The unconditional challenge is what ping-caching clients
// (docker daemon, containers/image: they authenticate only against the
// challenge the ping cached) need to ever negotiate; anonymous access
// flows through the anonymous token instead of a challenge-free ping, and
// a wrong-password login now fails at the token exchange instead of being
// waved through by a 200 ping (FR-11-AC4). The v1.0/v1.1 "anonymous open
// -> 200" posture is superseded by the same ruling.
func (h *Handler) servePing(w http.ResponseWriter, r *http.Request) {
	if principalOf(r) == nil {
		h.pingAnonymousChallenge(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		// The ping endpoint is a GET resource; other verbs answer the
		// spec body's method posture (405 + UNSUPPORTED) with the
		// mandatory Allow header (RFC 9110 MUST).
		w.Header().Set("Allow", "GET, HEAD")
		writeSpecError(w, http.StatusMethodNotAllowed, ErrCodeUnsupported,
			fmt.Sprintf("method %s is not supported on /v2/", r.Method), nil)
		return
	}
	writeAPIVersion(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte("{}"))
	}
}

// pingAnonymousChallenge is the ping face's anonymous 401: the standard
// Bearer challenge body with the ping face's own Content-Type spelling —
// application/json;charset=ISO-8859-1 (capture a_ping.h; every OTHER /v2
// JSON face answers the bare application/json, so the spelling lives here
// and not in the shared error writers).
func (h *Handler) pingAnonymousChallenge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", h.bearerChallenge(r, ""))
	writeSpecErrorCT(w, http.StatusUnauthorized, ErrCodeUnauthorized,
		"authentication required", nil, contentTypeJSONCharset)
}

// challenge renders the 401 + Bearer challenge (ADR-0010 clause 4). scope
// is empty on the ping endpoint (identity only); endpoint-scoped
// challenges (repository:<name>:pull,push) are T-37's to derive.
func (h *Handler) challenge(w http.ResponseWriter, r *http.Request, scope string) {
	hdr := w.Header()
	hdr.Set("WWW-Authenticate", h.bearerChallenge(r, scope))
	writeSpecError(w, http.StatusUnauthorized, ErrCodeUnauthorized,
		"authentication required", nil)
}

// bearerChallenge builds the WWW-Authenticate Bearer value (the one
// construction both challenge sites share — the route gate and the token
// endpoint's refused-credential arm). The service= value is the request's
// own host[:port] echoed back (L000-B C01: Artifactory's
// service="localhost:8082" is the registry host the client addressed, not
// a product constant; the token spec's service semantics = the registry
// host). A request without a Host (synthetic zero-value requests) falls
// back to the ServiceID constant.
func (h *Handler) bearerChallenge(r *http.Request, scope string) string {
	service := r.Host
	if service == "" {
		service = ServiceID
	}
	ch := fmt.Sprintf(`Bearer realm="%s",service="%s"`, h.realmBase(r)+TokenPath, service)
	if scope != "" {
		ch += fmt.Sprintf(`,scope="%s"`, scope)
	}
	return ch
}

// realmBase resolves the externally visible origin for the challenge
// realm: server.base_url when configured, else derived from the request
// (X-Forwarded-Proto/-Host honored, then the native Host; scheme defaults
// to http absent any signal — plain-HTTP registries are the LAN norm and
// docker login tolerates it). Trailing slashes are normalized so the
// concatenated realm is always exactly one path.
func (h *Handler) realmBase(r *http.Request) string {
	if base := strings.TrimRight(h.opts.BaseURL, "/"); base != "" {
		return base
	}
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = strings.TrimSpace(strings.Split(proto, ",")[0])
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

// serveNameRoute handles everything under /v2/<name>/...: parse the name
// (ADR-0010 clause 3), reject dot-segment escapes with 400 (NFR-S11),
// answer single-segment/shape misses with the spec 404, then gate the
// repository row: missing or not package_type=docker -> 404 NAME_UNKNOWN
// (D24 posture; the docker plane never renders the /binflow envelope,
// NFR-S10).
func (h *Handler) serveNameRoute(w http.ResponseWriter, r *http.Request, path string) {
	switch {
	case path == TokenPath || strings.HasPrefix(path, TokenPath+"/"):
		h.serveToken(w, r)
		return
	case path == catalogPath:
		// The catalog endpoint (architecture section 5.3, GET /v2/_catalog)
		// — T-40. The dedicated branch keeps the registry-level
		// "_-"prefixed route OUT of the name parser's repo-key slot (spec
		// reserves that prefix — a user repository named "_catalog" can
		// never hijack it).
		h.serveCatalog(w, r)
		return
	case strings.HasPrefix(path, catalogPath+"/"):
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"unknown registry route "+path, nil)
		return
	case repoCatalogKey(path) != "":
		// The repo-domain catalog, GET /v2/<repoKey>/_catalog (L000-B C16):
		// intercepted before the name parser for the same reserved-prefix
		// reason — a "_catalog" image slot carries no registry route.
		h.serveRepoCatalog(w, r, path, repoCatalogKey(path))
		return
	case !strings.HasPrefix(path, "/v2/"):
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"unknown /v2 route "+path, nil)
		return
	}

	ref, err := parseV2Name(path)
	if err != nil {
		if isNotAName(err) {
			writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported, err.Error(), nil)
			return
		}
		// Dot segments, bad escapes, oversized keys: 400 (NFR-S11).
		writeSpecError(w, http.StatusBadRequest, ErrCodeUnsupported, err.Error(), nil)
		return
	}

	if !h.opts.AnonymousAccess && principalOf(r) == nil {
		// Closed instance: the name routes challenge before the repo gate
		// (identical to the reference registries — a 404 here would leak
		// repository existence to unauthenticated probes, and NAME_UNKNOWN
		// would be indistinguishable from the real thing). The challenge
		// carries the endpoint-derived scope (AC2): the client negotiates a
		// token that matches the operation it was denied.
		h.challenge(w, r, deriveChallengeScope(r.Method, ref))
		return
	}

	row, err := h.repos.Get(r.Context(), ref.repoKey)
	if err != nil {
		// A genuine lookup failure is NOT a missing repository (T-33
		// review B2): swallowing it as NAME_UNKNOWN would render a DB
		// outage as "image missing" — docker clients would loop on
		// re-push. Log the failure and answer the spec-body 500; the
		// indistinguishability security posture is unaffected (a DB
		// fault reveals nothing about repository existence).
		h.log.ErrorContext(r.Context(), "docker: repository lookup failed",
			"repo", ref.repoKey, "error", err.Error())
		writeSpecError(w, http.StatusInternalServerError, ErrCodeUnknown,
			"repository lookup failed", nil)
		return
	}
	if row == nil || !servesV2Plane(row.PackageType()) {
		// One 404 for both causes (NAME_UNKNOWN, spec wording): "repo does
		// not exist" and "repo is not a registry-v2 family repo" must not be
		// distinguishable to an unauthenticated caller.
		writeSpecError(w, http.StatusNotFound, ErrCodeNameUnknown,
			fmt.Sprintf("repository name not known to registry: %q", ref.repoKey), nil)
		return
	}

	// Authorization gate (T-37 AC2/D22): the scope the endpoint derives
	// doubles as its permission question — pull maps onto read, push onto
	// write, delete onto delete (ADR-0010 clause 5). Anonymous reads pass
	// only while the flag is on (Can's nil rule); an authenticated
	// insufficient principal gets 403 DENIED, an anonymous write gets the
	// scoped challenge. This gate is ahead of the content handlers so
	// T-39/T-40 inherit it without re-deriving the mapping.
	if !h.authorizeRoute(w, r, ref) {
		return
	}

	// The REMOTE branch (T-363, FR-116.1): a pull-through repository takes
	// its own read plane — the proxy conversation, the cache and the RE-05
	// write refusal — AFTER the route's scope gate (an unauthorized
	// principal must not aim BinFlow at upstream URLs; the service seam
	// re-checks the read too). helm.md section 8.3 is the behavior spec.
	if row.Class() == repo.TypeRemote {
		h.serveRemoteRoute(w, r, ref)
		return
	}

	// The VIRTUAL branch (T-365, FR-116.2): an aggregated repository walks
	// its members on the read side — a local member's copy, a remote
	// member's pull-through, first-seen semantics over the two-bucket
	// order — and refuses writes with the service-rendered 405. The branch
	// sits after the route's scope gate like the remote one (the virtual
	// key is the addressed permission surface).
	if row.Class() == repo.TypeVirtual {
		h.serveVirtualRoute(w, r, ref)
		return
	}

	// Blob domain (T-38): the uploads route family and the blob read plane.
	// The manifest domain follows (T-39), then the tag listing (T-40).
	// Everything else answers the DE-16 spec-body 404 — including
	// referrers (DE-15: deliberately not implemented in M2).
	if ref.tail == "blobs/uploads" || strings.HasPrefix(ref.tail, "blobs/uploads/") {
		h.serveBlobUploads(w, r, ref, ref.tail)
		return
	}
	if strings.HasPrefix(ref.tail, "blobs/") {
		h.serveBlob(w, r, ref, ref.tail)
		return
	}
	if strings.HasPrefix(ref.tail, "manifests/") {
		h.serveManifest(w, r, ref, ref.tail)
		return
	}
	if ref.tail == tagsListTail {
		h.serveTagsList(w, r, ref)
		return
	}
	writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
		"registry route /v2/"+ref.repoKey+"/"+ref.image+"/"+ref.tail+
			" is not implemented in BinFlow M2 yet", nil)
}

// authorizeRoute enforces the endpoint-derived scope as the permission
// question (AC2/AC4: the challenge scope and the gate share one mapping —
// pull->r, push->w, delete->d over the <repoKey>/<image> subject). It
// returns false when it already rendered the failure. A nil Authorizer
// (bare test assemblies) fails closed for writes and open for reads.
func (h *Handler) authorizeRoute(w http.ResponseWriter, r *http.Request, ref nameRef) bool {
	p := principalOf(r)
	scope := deriveChallengeScope(r.Method, ref)
	_, _, actions, ok := splitScopeToken(scope)
	if !ok {
		// Unreachable (deriveChallengeScope only builds valid tokens); the
		// guard keeps the gate total without granting on a parse miss.
		writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
			"requested access to the resource is denied", nil)
		return false
	}
	for _, action := range actions {
		mapped := canActions[action]
		if len(mapped) == 0 {
			continue
		}
		allowed := false
		if h.authz != nil {
			allowed = h.authz.Can(r.Context(), p, ref.repoKey, ref.image, mapped[0])
		} else {
			allowed = p == nil && h.opts.AnonymousAccess && mapped[0] == auth.ActionRead
		}
		if !allowed {
			if p == nil {
				h.challenge(w, r, scope)
			} else {
				writeSpecError(w, http.StatusForbidden, ErrCodeDenied,
					fmt.Sprintf("requested access to the resource is denied: %s", scope), nil)
			}
			return false
		}
	}
	return true
}

// sessionRegistry's implementation lives in uploads.go (T-38): the UUID ->
// live-upload table carrying the protocol's received offset alongside the
// storage session.
