// Routing shape (architecture section 7.1, T-13's empirical conclusion):
// net/http applies cleanPath plus a 301/307 redirect to any request whose
// escaped path contains dot segments or double slashes BEFORE a handler
// runs — including through ServeMux with a single catch-all pattern
// (verified against Go 1.26: a raw-TCP "GET /binflow/r/a/../b" gets 307
// -> /binflow/r/b even when the only registered pattern is "/"). That
// redirect would let a client address a different node than the URL it
// signed checksums for, and it silently eats the adapter layout's 400
// defense (FR-4-AC10/AC11 — T-13's tests mount the adapter bare for
// exactly this reason).
//
// So the router below is a hand-rolled dispatcher over EscapedPath: no
// ServeMux on the product paths, no cleanPath, no implicit redirects. The
// raw spelling reaches adapter.Layout untouched, where dot segments are
// judged on the decoded form and rejected with 400.

package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// prefix is the single product namespace (ADR-0008).
const prefix = "/binflow"

// rootHandler is the single entry point for every non-probe path. The
// shared base chain (requestID -> accessLog -> recover -> CORS) wraps the
// authenticator, then dispatch branches per route and attaches the route's
// authorizer gate in front of the terminal handler (architecture section
// 7.2 order: authenticator precedes authorizer precedes handler).
func (s *Server) rootHandler() http.Handler {
	return s.baseChain(chain(authenticate(s.deps.Auth))(http.HandlerFunc(s.dispatch)))
}

// probeHandler serves /healthz and /readyz with the base chain only:
// probes carry no credentials, and a K8s liveness check must not pay the
// auth-round-trip cost or pollute auth failure metrics. /healthz answers
// "process alive" unconditionally. /readyz answers the section 7.1
// contract in full — metadata ping AND a storage writability probe (T-14
// review B1): an instance whose data directory went read-only (read-only
// volume, full disk, bad permission) must stop receiving traffic,
// uploads included.
func (s *Server) probeHandler(ready bool) http.Handler {
	terminal := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ready {
			if err := s.deps.Metadata.Ping(r.Context()); err != nil {
				writeError(w, http.StatusServiceUnavailable, "metadata not ready")
				return
			}
			if st := s.probeStorage(); st.Status != "ok" {
				writeError(w, http.StatusServiceUnavailable, "storage not ready: "+st.Detail)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})
	return s.baseChain(terminal)
}

// baseChain is the route-independent head of the chain (architecture
// section 7.2, order fixed): requestID -> accessLog -> recover -> CORS.
func (s *Server) baseChain(next http.Handler) http.Handler {
	return chain(
		requestID,
		accessLog(s.log),
		recoverPanic(s.log),
		cors(s.deps.Config.Server.CORSOrigins),
	)(next)
}

// dispatch branches on the raw escaped path (no normalization — see the
// package comment).
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()

	if !strings.HasPrefix(path, prefix) {
		// E-26①: no root mirror. Everything outside /binflow is a 404; the
		// message carries the /binflow prefix hint so /artifactory
		// migrants see the fix in the error body.
		notFoundPrefixHint(w, path)
		return
	}

	rest := strings.TrimPrefix(path, prefix)
	switch {
	case rest == "" || rest == "/":
		// /binflow and /binflow/ -> console placeholder (M1 JSON, M4 SPA).
		s.deps.Console.ServeHTTP(w, r)
	case rest == "/api" || rest == "/api/":
		// Bare /binflow/api: no endpoint at this address.
		notImplemented(w, "/binflow/api")
	case strings.HasPrefix(rest, "/api/"):
		s.dispatchAPI(w, r, strings.TrimPrefix(rest, "/api/"))
	case rest == "/v2" || strings.HasPrefix(rest, "/v2/"):
		// M2 docker mount point; a plain E-26 404 until then (never a
		// redirect, never an empty 200).
		notImplemented(w, "docker registry v2 (/binflow/v2)")
	default:
		s.dispatchContent(w, r, rest)
	}
}

// dispatchAPI routes /binflow/api/** after stripping /binflow/api. M1
// implements the system pair (ping, version), the /v1 pair (health, storage
// stats, permissions CRUD) and the compatible repository/storage/security
// planes (T-15); every other path answers the envelope 404 with "not
// implemented" wording (E-26②).
//
// Route gates (routeAuth.required) encode the management-plane rule of
// ADR-0009: everything under /binflow/api/** demands authentication. Where
// the operation is additionally admin-only, the terminal handler still
// consults the principal (repo.Service's requireAdmin or an explicit check)
// — the route gate alone would let any authenticated user through.
func (s *Server) dispatchAPI(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "system/ping" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, handlePing)
	case rest == "system/version" && r.Method == http.MethodGet:
		// Artifactory can policy-restrict /api/system/version away from
		// anonymous readers (rest-api.md section 5); BinFlow M1 keeps it
		// open — it reveals only the product name and build id.
		s.enforce(w, r, routeAuth{}, s.handleVersion)
	case rest == "v1/health" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true}, s.handleV1Health)
	case rest == "v1/storage/stats" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true}, s.handleV1StorageStats)

	// ---- /api/v1/permissions (E-24; admin) ----
	case rest == "v1/permissions" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, admin: true}, s.handlePermissionCreate)
	case rest == "v1/permissions" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, admin: true}, s.handlePermissionList)
	case strings.HasPrefix(rest, "v1/permissions/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, admin: true},
			s.withName(rest, "v1/permissions/", s.handlePermissionDelete))

	// ---- /api/repositories (E-04..E-08; admin for mutations) ----
	case rest == "repositories" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true}, s.handleRepoList)
	case strings.HasPrefix(rest, "repositories/"):
		key, tail := splitAPIName(rest, "repositories/")
		switch {
		case tail == "" && r.Method == http.MethodGet:
			s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
				s.handleRepoGet(w, r, key)
			})
		case tail == "" && r.Method == http.MethodPut:
			s.enforce(w, r, routeAuth{required: true, admin: true}, func(w http.ResponseWriter, r *http.Request) {
				s.handleRepoPut(w, r, key)
			})
		case tail == "" && r.Method == http.MethodPost:
			s.enforce(w, r, routeAuth{required: true, admin: true}, func(w http.ResponseWriter, r *http.Request) {
				s.handleRepoPost(w, r, key)
			})
		case tail == "" && r.Method == http.MethodDelete:
			s.enforce(w, r, routeAuth{required: true, admin: true}, func(w http.ResponseWriter, r *http.Request) {
				s.handleRepoDelete(w, r, key)
			})
		default:
			notImplemented(w, "/binflow/api/"+rest)
		}

	// ---- /api/storage (E-09/E-10; read = content plane semantics) ----
	// The item-info and ?list read gates mirror the content plane rather
	// than the management plane (PRD E-09: "内容元数据按匿名可读实现"):
	// anonymous GET passes while anonymous_access is on. ?list is the
	// documented exception — the handler itself answers the anonymous 403
	// (rest-api.md section 3), which is why its route gate does NOT carry
	// required: a 401 challenge would mask the spec's status.
	case strings.HasPrefix(rest, "storage/"):
		repoKey, rel := splitStoragePath(rest)
		if repoKey == "" {
			notImplemented(w, "/binflow/api/storage")
			return
		}
		if _, ok := r.URL.Query()["list"]; ok && r.Method == http.MethodGet {
			s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
				s.handleStorageList(w, r, repoKey, rel)
			})
			return
		}
		if r.Method == http.MethodGet {
			s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
				s.handleStorageItem(w, r, repoKey, rel)
			})
			return
		}
		notImplemented(w, "/binflow/api/"+rest)

	// ---- /api/security (E-16..E-19) ----
	case rest == "security/password" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true}, s.handleChangePasswordOwn)
	case rest == "security/users/authorization/changePassword" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleChangePasswordAlias)
	case rest == "security/token" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleTokenCreate)
	case rest == "security/token/revoke" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, admin: true}, s.handleTokenRevoke)
	case rest == "security/users" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, admin: true}, s.handleUserList)
	case rest == "security/users" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, admin: true}, s.handleUserCreatePost)
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, admin: true},
			s.withName(rest, "security/users/", s.handleUserGet))
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, admin: true},
			s.withName(rest, "security/users/", s.handleUserCreatePut))

	default:
		notImplemented(w, "/binflow/api/"+rest)
	}
}

// withName adapts a (w, r, name) handler to an http.HandlerFunc by peeling
// name off the route tail. An empty or multi-segment remainder falls to the
// E-26 404 instead of routing a bogus name.
func (s *Server) withName(rest, prefix string, h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, tail := splitAPIName(rest, prefix)
		if name == "" || tail != "" {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		h(w, r, name)
	}
}

// splitAPIName splits rest after prefix into (name, remainder): the first
// segment is the name, everything after the following slash is the tail.
// Both are returned still escaped — handlers unescape where the value is a
// user-visible identifier.
func splitAPIName(rest, prefix string) (name, tail string) {
	seg := strings.TrimPrefix(rest, prefix)
	name, tail, _ = strings.Cut(seg, "/")
	return name, tail
}

// splitStoragePath splits /api/storage/{repo}/{path} into the repo key and
// the decoded repo-relative remainder ("" when absent). Dot segments in the
// remainder are left to the service layer's validators (same defense the
// content plane relies on); a dot-segment or empty repo key yields "" so the
// caller answers the E-26 404.
func splitStoragePath(rest string) (repoKey, relPath string) {
	seg := strings.TrimPrefix(rest, "storage/")
	if seg == "" {
		return "", ""
	}
	key, tail, _ := strings.Cut(seg, "/")
	if key == "" || key == "." || key == ".." || strings.Contains(key, "/") {
		return "", ""
	}
	decoded, err := url.PathUnescape(tail)
	if err != nil {
		// A malformed escape cannot name a node; hand back the raw tail and
		// let the service's path validator reject it with 400.
		decoded = tail
	}
	return key, strings.TrimPrefix(decoded, "/")
}

// contentAction maps a content verb onto (action, credential-required).
// GET/HEAD are reads (anonymous allowed when the flag is on, ADR-0009);
// writes and deletes always require a credential. Other verbs carry no
// route gate — the adapter answers its own 405 with an Allow header.
func contentAction(method string) (action string, required bool) {
	switch method {
	case http.MethodGet, http.MethodHead:
		return auth.ActionRead, false
	case http.MethodPut, http.MethodPost:
		return auth.ActionWrite, true
	case http.MethodDelete:
		return auth.ActionDelete, true
	}
	return "", false
}

// dispatchContent routes /binflow/<repo-key>/** to the adapter serving
// that repository's package type (architecture section 5.1 dispatch).
// Authorization runs on the raw (prefixed) path first — the ACL is keyed
// by repo name and does not depend on the repo row existing — then the
// adapter receives the request with the /binflow prefix stripped and the
// principal in its own context seam.
func (s *Server) dispatchContent(w http.ResponseWriter, r *http.Request, _ string) {
	action, required := contentAction(r.Method)
	p := principalFrom(r.Context())

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repoKey, _ := splitFirstSegment(r)
		row, err := s.deps.Repos.Get(r.Context(), repoKey)
		if err != nil {
			s.writeRepoLookupError(w, repoKey, err)
			return
		}
		h, ok := s.adapters[row.PackageType]
		if !ok {
			// No adapter serves this package type. A repo row cannot hold
			// an unserved type in M1 (repo.Service validates it), so this
			// is a build-surface gap; it is logged and answered with the
			// same not-implemented envelope as E-26 rather than a 500.
			s.log.ErrorContext(r.Context(), "httpapi: no adapter mounted for package type",
				"package_type", row.PackageType, "repo", repoKey)
			notImplemented(w, "package type "+row.PackageType)
			return
		}
		h.ServeHTTP(w, withStrippedPrefix(r, p))
	})

	s.enforce(w, r, routeAuth{
		required: required,
		action:   action,
	}, inner)
}

// writeRepoLookupError maps a repo lookup failure onto the envelope:
// missing repository -> the spec's 404 wording (rest-api.md section 1.4,
// high confidence); anything else is an honest 500.
func (s *Server) writeRepoLookupError(w http.ResponseWriter, repoKey string, err error) {
	if errors.Is(err, repo.ErrRepoNotFound) || errors.Is(err, metadata.ErrRepoNotFound) {
		writeError(w, http.StatusNotFound,
			"Failed to find the repository '"+repoKey+"' specified in the request.")
		return
	}
	s.log.Error("httpapi: repository lookup failed", "repo", repoKey, "error", err.Error())
	writeError(w, http.StatusInternalServerError, "repository lookup failed")
}

// withStrippedPrefix returns a request whose URL carries the path minus
// the /binflow prefix, preserving the ESCAPED spelling: RawPath keeps the
// raw bytes so dot segments and percent-encoded characters survive
// verbatim to adapter.Layout (see the package comment), and Path carries
// the decoded remainder. The principal is re-boxed into the adapter
// package's own context seam (adapter.WithPrincipal): adapters read it
// from there and never authenticate themselves (architecture section
// 5.1). Headers and body are shared with the original request.
func withStrippedPrefix(r *http.Request, p *auth.Principal) *http.Request {
	raw := r.URL.EscapedPath()
	stripped := strings.TrimPrefix(raw, prefix)
	if stripped == "" {
		stripped = "/"
	}
	decoded := strings.TrimPrefix(r.URL.Path, prefix)
	if decoded == "" {
		decoded = "/"
	}
	u := *r.URL
	u.Path = decoded
	u.RawPath = stripped
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = &u
	r2.RequestURI = r.RequestURI
	return r2.WithContext(adapter.WithPrincipal(r.Context(), p))
}

// enforce wraps one terminal handler with the authorizer middleware for
// the given route requirement (authentication already ran upstream, so
// the architecture's chain order — authenticator before authorizer —
// holds even though the gate is attached per route).
func (s *Server) enforce(w http.ResponseWriter, r *http.Request, req routeAuth, h http.HandlerFunc) {
	authorize(s.deps.Authz, req)(h).ServeHTTP(w, r)
}
