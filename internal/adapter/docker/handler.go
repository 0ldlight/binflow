package docker

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
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

// ServeHTTP is the /v2 business body for M2's foundation scope:
//
//   - GET/HEAD /v2 and /v2/ — the version-check ping (DE-01/D04);
//   - every other route — name resolution and the repo gate (ADR-0010
//     clause 3), with upload/manifest/catalog bodies landing in T-37
//     through T-40 (their routes currently fall through to the spec-body
//     404, which is the DE-16 posture for anything not implemented).
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

// servePing implements DE-01/D04: anonymous access open -> 200 {} with the
// api-version header; closed or an unauthenticated non-GET -> 401 with the
// Bearer challenge (realm = <base>/v2/token, service="binflow", ADR-0010
// clause 4 — NOT the PRD v1.0 wording, which ADR-0010 superseded and PRD
// v1.1 wrote back).
func (h *Handler) servePing(w http.ResponseWriter, r *http.Request) {
	p := adapter.PrincipalFrom(r.Context())
	if h.opts.AnonymousAccess || p != nil {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// The ping endpoint is a GET resource; other verbs answer the
			// spec body's method posture (405 + UNSUPPORTED).
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
		return
	}
	h.challenge(w, r, "")
}

// challenge renders the 401 + Bearer challenge (ADR-0010 clause 4). scope
// is empty on the ping endpoint (identity only); endpoint-scoped
// challenges (repository:<name>:pull,push) are T-37's to derive.
func (h *Handler) challenge(w http.ResponseWriter, r *http.Request, scope string) {
	ch := fmt.Sprintf(`Bearer realm="%s",service="%s"`, h.realmBase(r)+TokenPath, ServiceID)
	if scope != "" {
		ch += fmt.Sprintf(`,scope="%s"`, scope)
	}
	hdr := w.Header()
	hdr.Set("WWW-Authenticate", ch)
	writeSpecError(w, http.StatusUnauthorized, ErrCodeUnauthorized,
		"authentication required", nil)
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
		// The token endpoint's issuing logic is T-37; the route is part of
		// the /v2 namespace, so until then it answers the DE-16 404 rather
		// than the root-path envelope.
		writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
			"token endpoint is not implemented yet (T-37)", nil)
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

	if !h.opts.AnonymousAccess && adapter.PrincipalFrom(r.Context()) == nil {
		// Closed instance: the name routes challenge before the repo gate
		// (identical to the reference registries — a 404 here would leak
		// repository existence to unauthenticated probes, and NAME_UNKNOWN
		// would be indistinguishable from the real thing).
		h.challenge(w, r, "")
		return
	}

	row, err := h.repos.Get(ref.repoKey)
	if err != nil || row == nil || row.PackageType() != Protocol {
		// One 404 for all three causes (NAME_UNKNOWN, spec wording):
		// "repo does not exist" and "repo is not a docker repo" must not
		// be distinguishable to an unauthenticated caller.
		writeSpecError(w, http.StatusNotFound, ErrCodeNameUnknown,
			fmt.Sprintf("repository name not known to registry: %q", ref.repoKey), nil)
		return
	}

	// Foundation scope: every content route (blobs/manifests/tags/uploads/
	// referrers) lands in T-37..T-40. Until then they answer the DE-16
	// spec-body 404 — including referrers (DE-15: deliberately not
	// implemented in M2).
	writeSpecError(w, http.StatusNotFound, ErrCodeUnsupported,
		"registry route /v2/"+ref.repoKey+"/"+ref.image+"/"+ref.tail+
			" is not implemented in BinFlow M2 yet", nil)
}

// SessionRegistry exposes the upload-session seam T-38 will drive. It is
// created once per handler here so the constructor surface does not change
// when blob uploads land.
type sessionRegistry struct{}

func newSessionRegistry() *sessionRegistry { return &sessionRegistry{} }
