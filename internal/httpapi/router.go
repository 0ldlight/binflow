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
//
// The /v2 root-level exception (ADR-0010 clause 1) rides the SAME chain:
// requestID/accessLog/recover/CORS/authenticate all apply, but the prefix
// is not stripped and the route gate is the docker adapter's own — a 401
// on /v2 must render the registry spec body plus the Bearer challenge
// (realm=/v2/token), never the /binflow errors[] envelope (NFR-S10).
//
// csrfGuard sits between the authenticator and dispatch (T-91): it needs
// the resolved principal and must precede every route decision.
func (s *Server) rootHandler() http.Handler {
	return s.baseChain(chain(
		authenticate(s.deps.Auth),
		csrfGuard(s.log),
	)(http.HandlerFunc(s.dispatch)))
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
// section 7.2, order fixed): requestID -> accessLog -> recover -> CORS,
// with the metrics middleware (T-163) inserted between accessLog and
// recover when instrumentation is wired: it reads the statusRecorder
// accessLog installs, and recover sits INSIDE it so recovered panics still
// land in the counters. Absent entirely on metrics-less stacks — the
// section 7.2 order is untouched for them.
func (s *Server) baseChain(next http.Handler) http.Handler {
	ms := []Middleware{requestID, accessLog(s.log)}
	if s.metrics != nil {
		ms = append(ms, s.metrics.middleware())
	}
	ms = append(ms, recoverPanic(s.log), cors(s.deps.Config.Server.CORSOrigins))
	return chain(ms...)(next)
}

// dispatch branches on the raw escaped path (no normalization — see the
// package comment). It is also the single point where a rejected
// credential (T-33 review B1) is shaped per plane: /v2 answers the
// registry spec body plus the Bearer challenge (a docker client must
// never meet a Basic challenge mid-negotiation), /binflow keeps the
// errors[] envelope plus the Basic challenge.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()

	// /v2/** root-level exception (ADR-0010 clause 1): the docker registry
	// plane. Same middleware chain (the request reached this point through
	// it), no /binflow prefix to strip, principal handed to the adapter
	// through the shared adapter seam. The prefix test MUST run before the
	// /binflow branch below — /v2 is not under the product prefix at all.
	if path == "/v2" || strings.HasPrefix(path, "/v2/") {
		if reason, rejected := authRejectedFrom(r.Context()); rejected {
			// Presented-but-refused credential: same wire form as an
			// anonymous request on a closed instance — 401 + Bearer
			// challenge + spec body (docker-registry.md section 5.1).
			// The refusal reason stays in the log, not the body.
			s.writeV2AuthFailure(w, r, reason)
			return
		}
		if h, ok := s.adapters["docker"]; ok {
			h.ServeHTTP(w, withRootPrincipal(r, principalFrom(r.Context())))
			return
		}
		// No docker adapter mounted (assembly without one): honest spec-body
		// 404, keeping the /v2 plane's response contract even degraded.
		s.writeV2Unavailable(w)
		return
	}

	if reason, rejected := authRejectedFrom(r.Context()); rejected {
		// B1 (T-91 security review): the login ENTRY POINTS are EXEMPT from
		// the hard-401 — they are precisely where stale cookies belong
		// (expired, revoked, swept or tossed by a hostile sibling subdomain),
		// and the browser attaches them automatically (Path=/binflow).
		// Without the exemption, a tossed garbage cookie turns every
		// correct-credential login into an indefinite 401 (cookie-tossing
		// DoS). POST /api/v1/session verifies the body's username/password
		// itself; the two /api/v1/oidc routes are browser navigations that
		// never consume the presented credential (T-157). The
		// presented-but-rejected-never-downgrades posture is untouched for
		// every other route.
		if isLoginEntryPoint(r.Method, path) {
			s.log.DebugContext(r.Context(), "httpapi: rejected credential exempted on a login entry point",
				"path", path, "reason", reason)
		} else {
			// /binflow plane (and everything else): the historical hard-401
			// behavior, rendered here instead of inside the authenticator.
			w.Header().Set("WWW-Authenticate", basicChallenge)
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
	}

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
		// /binflow and /binflow/ -> 301 /binflow/ui/ (CE-01, T-89's
		// console handler; the M1 placeholder JSON is terminated).
		s.deps.Console.ServeHTTP(w, r)
	case rest == "/ui" || rest == "/ui/" || strings.HasPrefix(rest, "/ui/"),
		rest == "/assets" || rest == "/assets/" || strings.HasPrefix(rest, "/assets/"):
		// Console segments (T-91, PRD FR-23/CE-01/CE-02): the SPA's ui
		// segment (shell + history fallback) and the fingerprinted asset
		// mount. The console handler owns every spelling INSIDE these
		// segments — including out-of-segment 404s for dot-segment probes
		// (path.Clean backstop) — so it can never shadow a repository's
		// content plane: /binflow/<repo>/<path> keeps routing to
		// dispatchContent below.
		s.deps.Console.ServeHTTP(w, r)
	case rest == "/docs" || rest == "/docs/" || strings.HasPrefix(rest, "/docs/"):
		// Docs segment (T-129, PRD FR-41/DC-01, ADR-0011): the embedded
		// help center. Anonymous and read-only — product self-description
		// in the /healthz posture, deliberately outside every auth gate (an
		// anonymous_access=false instance still serves it). The repo key
		// "docs" is reserved (ADR-0008 T-108 union), so the segment can
		// never shadow a repository; the handler owns only spellings INSIDE
		// the segment.
		s.deps.Docs.ServeHTTP(w, r)
	case rest == "/api" || rest == "/api/":
		// Bare /binflow/api: no endpoint at this address.
		notImplemented(w, "/binflow/api")
	case strings.HasPrefix(rest, "/api/"):
		s.dispatchAPI(w, r, strings.TrimPrefix(rest, "/api/"))
	case rest == "/v2" || strings.HasPrefix(rest, "/v2/"):
		// /binflow/v2 is NOT a mirror of the root-level exception
		// (ADR-0010 clause 2: no double mount — the Location/realm/catalog
		// surfaces would need twin generators, and no docker client can
		// reach this spelling anyway). The E-26 envelope 404 stands.
		notImplemented(w, "docker registry v2 (/binflow/v2)")
	default:
		s.dispatchContent(w, r, rest)
	}
}

// isLoginEntryPoint reports whether (method, path) is one of the anonymous
// credential-presentation routes that must survive a presented-but-rejected
// cookie (the dispatch exemption above): the password login verifies its own
// body, and the two OIDC routes are browser navigations whose credential
// arrives later, inside the authorization code. Everything else keeps the
// hard-401 posture.
func isLoginEntryPoint(method, path string) bool {
	switch path {
	case prefix + "/api/v1/session":
		return method == http.MethodPost
	case prefix + "/api/v1/oidc/login", prefix + "/api/v1/oidc/callback":
		return method == http.MethodGet
	}
	return false
}

// v2AuthFailure is the docker adapter's seam for rendering an
// authentication failure on the /v2 plane (T-33 review B1): the spec error
// body plus the Bearer challenge whose realm points at /v2/token. The
// router holds only this narrow interface so httpapi never imports the
// docker package (adapter packages are leaves; the dependency direction
// in architecture section 2 forbids httpapi -> adapter/docker).
type v2AuthFailure interface {
	RenderAuthFailure(w http.ResponseWriter, r *http.Request)
}

// writeV2AuthFailure shapes a rejected credential for the /v2 plane. When
// the docker adapter is mounted it owns the rendering (realm derivation
// lives there); otherwise the static fallback keeps the same contract
// with a request-derived realm.
func (s *Server) writeV2AuthFailure(w http.ResponseWriter, r *http.Request, reason string) {
	s.log.Warn("httpapi: rejected credential on /v2",
		"path", r.URL.EscapedPath(), "reason", reason)
	if h, ok := s.adapters["docker"].(v2AuthFailure); ok {
		h.RenderAuthFailure(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	w.Header().Set("WWW-Authenticate",
		`Bearer realm="`+requestScheme(r)+"://"+r.Host+`/v2/token",service="binflow"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required","detail":null}]}` + "\n"))
}

// requestScheme resolves http vs https from the request (X-Forwarded-Proto
// honored) — the fallback realm builder only.
func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return strings.TrimSpace(strings.Split(proto, ",")[0])
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// writeV2Unavailable answers /v2 when no docker adapter is mounted. The
// body keeps the registry error schema (the client on this route is a
// registry client, not a BinFlow client); the status is the spec's
// UNSUPPORTED posture.
func (s *Server) writeV2Unavailable(w http.ResponseWriter) {
	s.log.Error("httpapi: /v2 request but no docker adapter mounted")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Docker-Distribution-Api-Version", "registry/2.0")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`{"errors":[{"code":"UNSUPPORTED","message":"docker registry is not enabled on this instance","detail":null}]}` + "\n"))
}

// withRootPrincipal boxes the principal into the adapter seam without
// touching the URL — the /v2 route receives the request VERBATIM (path,
// query, headers, body), because the registry protocol is addressed from
// the root, not under /binflow.
func withRootPrincipal(r *http.Request, p *auth.Principal) *http.Request {
	return r.WithContext(adapter.WithPrincipal(r.Context(), p))
}

// dispatchAPI routes /binflow/api/** after stripping /binflow/api. M1
// implements the system pair (ping, version), the /v1 pair (health, storage
// stats, permissions CRUD) and the compatible repository/storage/security
// planes (T-15); every other path answers the envelope 404 with "not
// implemented" wording (E-26②).
//
// Route gates (routeAuth) encode the management-plane rule of ADR-0009
// plus the M7 capability split (ADR-0026): everything under
// /binflow/api/** demands authentication, and the operations beyond
// self-service demand a closed-set capability (auth.CanManage) or the
// single-repo manage gate (auth.CanManageRepo) — see the section 7.1
// inventory. Two families carry their gate deeper than the route literal:
// the permission writes (family 4's exception — the OR of CapSecurityWrite
// with the m-holder coverage arm is body-dependent, permissions.go owns it)
// and the repository create arm (family 6 splits inside handleRepoPut,
// which knows whether the key exists). Where the write is additionally
// destructive the service layer still re-checks (repo.Service's DeleteRepo
// admin door, the GC and replication create/delete guards).
func (s *Server) dispatchAPI(w http.ResponseWriter, r *http.Request, rest string) {
	switch {
	case rest == "system/ping" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, handlePing)
	case rest == "system/version" && r.Method == http.MethodGet:
		// Artifactory can policy-restrict /api/system/version away from
		// anonymous readers (rest-api.md section 5); BinFlow M1 keeps it
		// open — it reveals only the product name and build id.
		s.enforce(w, r, routeAuth{}, s.handleVersion)

	// ---- /api/system/license (M10 T-279, ADR-0032 / section 15.1.4) ----
	// The singular license plane: install/query/uninstall over the single
	// document. GET rides system:read (readonly_admin sees the state);
	// the mutating verbs ride system:write (readonly_admin 403,
	// FR-84-AC7). The Artifactory plural path (/api/system/licenses) has
	// no route on purpose (LC-02 deliberate divergence: the E-26 404
	// below is the guidance — a JFrog-format document has zero chance of
	// installing), and every other verb on the singular path falls to the
	// same E-26 404.
	case rest == "system/license" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleLicenseGet)
	case rest == "system/license" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleLicenseInstall)
	case rest == "system/license" && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleLicenseDelete)

	// ---- /api/v1/addons (M10 T-282, ADR-0033 / section 15.2.5) ----
	// The slot manifest with live unlock evaluation: one bare-array GET on
	// system:read (readonly_admin sees the matrix, FR-86-AC1; a plain user
	// 403s at the capability door). Every other verb — POST/PUT/DELETE
	// /api/v1/addons, or any sub-path — falls to the E-26 404: the addon
	// plane has no write surface (licenses are installed on
	// /api/system/license; slot tiers are code, not data).
	case rest == "v1/addons" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleAddonsList)
	case rest == "v1/health" && r.Method == http.MethodGet:
		// Management plane (C28a is `-sfu admin` for a reason): the health
		// dashboard exposes instance internals, so it sits behind the
		// system-read capability like the rest of /api/v1. Deployers'
		// liveness/readiness probes use the unauthenticated /healthz and
		// /readyz instead.
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleV1Health)
	case rest == "v1/storage/stats" && r.Method == http.MethodGet:
		// Whole-instance blob/byte statistics (D2): operator data, a
		// read-capability question since M7 — a plain user must not learn
		// repository volume, readonly_admin may (the auditor's dashboard).
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleV1StorageStats)
	case rest == "v1/storage/usage" && r.Method == http.MethodGet:
		// Batch quota usage (M9 E1, ADR-0030 / section 14.1, T-253): the
		// set form of the per-repo endpoint below — one request for the
		// whole "used" column (bare array, K20). The route demands
		// authentication; visibility (the same family-7 OR formula, per
		// repository) is the use case's, and its denied arm is silent
		// exclusion: an authenticated caller always receives a filtered 200
		// view (empty set = []), never a 403.
		s.enforce(w, r, routeAuth{required: true}, s.handleStorageUsageBatch)
	case strings.HasPrefix(rest, "v1/storage/usage/"):
		// Per-repository quota usage (GE-06/W26b, T-95; family 7): one
		// segment after the prefix is the repo key. The route demands
		// authentication; the CanManageRepo(read)-OR-read-grant decision is
		// the use case's (a denied reader must see 403, not a 401
		// challenge). readonly_admin passes through Can's global r; the
		// m-holder OR-arm rides the same seam (FR-65/K11).
		key, tail := splitAPIName(rest, "v1/storage/usage/")
		if tail == "" && r.Method == http.MethodGet {
			s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
				s.handleStorageUsage(w, r, key)
			})
			return
		}
		notImplemented(w, "/binflow/api/"+rest)

	// ---- /api/v1/audit (GE-01, T-93; admin, append-only) ----
	// GET is the only verb with a route: every other spelling — PUT/DELETE
	// /api/v1/audit, /api/v1/audit/1, POST anything — falls to the E-26
	// 404, and that absence IS the append-only rule (W39: there is no
	// write surface to reach; the metadata layer has no UPDATE/DELETE
	// audit path either, NFR-S21).
	case rest == "v1/audit" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleAuditQuery)

	// ---- /api/v1/system/gc (GE-03, T-94; sync, lock-guarded) ----
	// POST is the only verb with a route: the job-status endpoint (GET) is
	// a recorded P2 debt (ADR-0015 erratum ②) — the last run is queried
	// through the audit trail's gc.run events, not a live status surface.
	// Every other spelling falls to the E-26 404. Gate = system:write with
	// NO dry-run exception (T-214①: readonly_admin does not POST write
	// routes, apply=false included).
	case rest == "v1/system/gc" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleSystemGC)

	// ---- /api/v1/storage/migration (T-164) ----
	case rest == "v1/storage/migration" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleMigrationStatus)
	case rest == "v1/storage/migration/start" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleMigrationStart)

	// ---- /api/v1/replications (T-180, ADR-0021) ----
	// The push-replication configuration plane. GET lists (secrets
	// excluded), POST creates, DELETE /{name} drops one config — its task
	// rows cascade via the 009 FK. No PUT yet: the ticket scoped the CRUD to
	// create/delete, and an update surface needs an enable/disable
	// semantics ruling first. The sibling /api/v1/replication/status below
	// is the console panel's aggregated read face (T-159).
	case rest == "v1/replications" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleReplicationList)
	case rest == "v1/replications" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleReplicationCreate)
	case strings.HasPrefix(rest, "v1/replications/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.withName(rest, "v1/replications/", s.handleReplicationDelete))

	// ---- /api/v1/replication/status (T-159/T-180) ----
	// The panel polls this every 10s; the data shape is pinned to the T-159
	// contract assumptions (see replication.go's header).
	case rest == "v1/replication/status" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleReplicationStatus)

	// ---- /api/v1/oidc (OD-01/OD-02, T-157; anonymous browser entry) ----
	// GET is the only verb with a route on either path: the flow is two
	// top-level browser navigations. The gate is empty because the
	// credential arrives INSIDE the flow (the authorization code), not with
	// the request; the handlers refuse to run when Deps.OIDC is unwired, so
	// a disabled instance answers the E-26 404 and the endpoints do not
	// exist (FR-54-AC6/H29).
	case rest == "v1/oidc/login" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleOIDCLogin)
	case rest == "v1/oidc/callback" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleOIDCCallback)

	// ---- /api/v1/auth/methods (T-179; anonymous capability discovery) ----
	// The login page's entry-point map: which of password/oidc/ldap this
	// instance offers. Anonymous by design (see auth_methods.go); every
	// other verb on the path falls to the E-26 404 below.
	case rest == "v1/auth/methods" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleAuthMethods)

	// ---- /api/v1/session (CE-03..05, T-91; ADR-0014 erratum 2) ----
	// POST is deliberately un-gated (it IS the credential presentation);
	// GET/DELETE demand an authenticated principal of any arm — whoami is
	// the SPA's route guard, DELETE revokes the presented session. Login
	// failures render inside the handler so the uniform 401 wording and the
	// auth.failed audit event stay in one place.
	case rest == "v1/session" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{}, s.handleSessionCreate)
	case rest == "v1/session" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true}, s.handleSessionWhoami)
	case rest == "v1/session" && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true}, s.handleSessionDelete)

	// ---- /api/v1/permissions (E-24; security plane) ----
	// The read verb is security:read — readonly_admin sees the grant map, a
	// plain user does not (the targets enumerate principals and their action
	// distribution).
	//
	// The WRITE verbs carry the section 7.1 family-4 EXCEPTION (T-217,
	// ADR-0026 decision 3 / FR-65): the gate is the body-dependent OR
	// "CapSecurityWrite ∨ target.repos ⊆ the caller's manage coverage",
	// evaluated inside the handlers (permissions.go) — the route therefore
	// demands only authentication, and the capability check moved there with
	// the coverage arm. Manage holders edit exactly the targets inside their
	// coverage; everyone else keeps the 403.
	case rest == "v1/permissions" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handlePermissionCreate)
	case rest == "v1/permissions" && r.Method == http.MethodGet:
		// E6 (M9, T-254, ADR-0030 / architecture 14.1.6): the filter arm's
		// gate is QUERY-dependent (CapSecurityRead full list ∨ non-empty
		// manage coverage filtered subset ∨ empty coverage the same 403),
		// so it is evaluated inside the handler — the write verbs' pattern
		// one section up. The route split is itself the only dispatch
		// change: a request without a non-empty filter value keeps the
		// frozen route and handler verbatim, byte for byte.
		if permissionManageFilterPresent(r) {
			s.enforce(w, r, routeAuth{required: true}, s.handlePermissionListManage)
		} else {
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead}, s.handlePermissionList)
		}
	case strings.HasPrefix(rest, "v1/permissions/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true},
			s.withName(rest, "v1/permissions/", s.handlePermissionDelete))

	// ---- /api/repositories (E-04..E-08) ----
	// The list sits on repo:read (family 5, D2/C22b): readonly_admin sees
	// the full inventory; a plain user must not (M1 has no per-repository
	// read-ACL data plane to filter the listing by caller — the permission
	// model is path-keyed; a filtered listing is M8+, §11.30). The
	// single-repo family walks CanManageRepo (family 7); repo creation and
	// deletion stay on the global repo:write gate (family 6) — the create
	// arm of PUT splits inside the handler, which knows whether the key
	// exists.
	case rest == "repositories" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapRepoRead}, s.handleRepoList)
	case strings.HasPrefix(rest, "repositories/"):
		key, tail := splitAPIName(rest, "repositories/")
		switch {
		case tail == "" && r.Method == http.MethodGet:
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleRepoGet(w, r, key)
				})
		case tail == "" && r.Method == http.MethodPut:
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleRepoPut(w, r, key)
				})
		case tail == "" && r.Method == http.MethodPost:
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleRepoPost(w, r, key)
				})
		case tail == "" && r.Method == http.MethodDelete:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapRepoWrite},
				func(w http.ResponseWriter, r *http.Request) {
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
		if _, ok := r.URL.Query()["permissions"]; ok && r.Method == http.MethodGet {
			// SE-08 (T-97 review B2, M7 family 7): the effective-permission
			// view is MANAGEMENT-plane data — it enumerates principal names
			// and their r/w/d/m distribution — so it walks the single-repo
			// manage gate: admin and readonly_admin read it, a plain user
			// needs the m action on the repository (upstream's first door on
			// this arm is canManage; BinFlow's m is the same question). An
			// empty gate here would let any visitor of an anonymous-read
			// instance enumerate users and groups, defeating the login
			// plane's existence-hiding.
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: repoKey}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleStoragePermissions(w, r, repoKey, rel)
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

	// ---- /api/search (SR-01/SR-02, T-92) ----
	// Exactly two entrances open the M1 E-26 search domain (PRD M4: the
	// domain opens artifact + checksum only); every other family member —
	// props/users/artifactory/pattern/badge, and any other verb on these
	// two paths — falls through to the E-26 404 (SR-04, intentional
	// incompatibility). The gates mirror /api/storage's read posture: the
	// use case owns the anonymous-channel decision, so a closed instance
	// answers the spec's 403 rather than a route-level 401 challenge.
	case rest == "search/artifact" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchArtifact)
	case rest == "search/checksum" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchChecksum)

	// ---- /api/security (E-16..E-19) ----
	case rest == "security/password" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true}, s.handleChangePasswordOwn)
	case rest == "security/users/authorization/changePassword" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleChangePasswordAlias)
	case rest == "security/token" && r.Method == http.MethodPost:
		// T-190 (PRD M6 v1.2 Q11, T-188 ruling — supersedes the M1 D3
		// admin-only subset): minting is open to EVERY authenticated caller
		// (local/OIDC/LDAP arms alike); the admin-vs-self distinction moved
		// into the handler (subject + TTL cap guardrails). Anonymous still
		// meets the 401 challenge here. List/revoke below stay admin-only
		// (auth-model.md sections 3.3/3.4).
		s.enforce(w, r, routeAuth{required: true, oauth: true}, s.handleTokenCreate)
	case rest == "security/token/revoke" && r.Method == http.MethodPost:
		// oauth: every non-2xx on the token family renders the OAuth error
		// body, authorization denials included (D3 follow-up: revoke's 403
		// previously leaked the errors[] envelope).
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite, oauth: true}, s.handleTokenRevoke)
	case rest == "security/users" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead}, s.handleUserList)
	case rest == "security/users" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.handleUserCreatePost)
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.withName(rest, "security/users/", s.handleUserGet))
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/users/", s.handleUserCreatePut))
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodPost:
		// SE-06 partial update (T-97): email/password/admin/adminRole and
		// the groups[] membership set. The changePassword alias above wins
		// by order — its exact-match case precedes this prefix case.
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/users/", s.handleUserUpdatePost))
	case strings.HasPrefix(rest, "security/users/") && r.Method == http.MethodDelete:
		// E4 (M9, T-251, ADR-0030): the delete verb on the users family.
		// CapSecurityWrite with NO manage-coverage arm — users are not
		// repo-domain principals, so the family-4 exception never applied
		// here; the guard chain (built-in/self/last-admin) lives in the
		// use case behind the facet.
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/users/", s.handleUserDelete))

	// ---- /api/security/groups (SE-01..04, T-97; security plane) ----
	// Errors inside the handlers are the user-management plain-text layer;
	// the capability gate itself renders the plane's envelope like every
	// other /api/security route.
	case rest == "security/groups" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead}, s.handleGroupList)
	case strings.HasPrefix(rest, "security/groups/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.withName(rest, "security/groups/", s.handleGroupGet))
	case strings.HasPrefix(rest, "security/groups/") && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/groups/", s.handleGroupPut))
	case strings.HasPrefix(rest, "security/groups/") && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/groups/", s.handleGroupPost))
	case strings.HasPrefix(rest, "security/groups/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/groups/", s.handleGroupDelete))

	default:
		// /binflow/api/<proto>/** protocol mounts (npm, pypi — the §5.4
		// reserved slot M1 promised): dispatched only when the protocol is
		// registered; otherwise the E-26 404 below stands, and the E-26
		// route assertions flip in the protocol tickets (T-69/T-70), never
		// ahead of them (T-61 risk R5).
		if s.dispatchAPIProtocolMount(w, r, rest) {
			return
		}
		notImplemented(w, "/binflow/api/"+rest)
	}
}

// apiProtocolMounts lists the protocols that also mount under the reserved
// /binflow/api segment (architecture sections 5.4.2/5.4.3): npm and pypi
// clients address the registry API prefix (…/api/npm/<repo>/<pkg>,
// …/api/pypi/<repo>/simple/…) rather than bare content paths. The list is
// CLOSED by design: the api segment primarily hosts REST routes, so a
// protocol earns a mount only through its own ticket — maven mounts content
// paths with zero httpapi changes, and look-alikes (…/api/pypi-ui/**) stay
// on the E-26 404 permanently.
var apiProtocolMounts = []string{"npm", "pypi"}

// dispatchAPIProtocolMount serves /binflow/api/<proto>/** by REWRITING the
// request onto the content plane — /binflow/api/npm/<repo>/<rest> becomes
// /binflow/<repo>/<rest> — and handing it to the standard content dispatch.
// The rewrite is the whole seam: authorization keys off the same first
// segment (splitFirstSegment sees the rewritten path), the repository row
// resolves once, and the adapter receives exactly the request shape the
// content plane gives it — /binflow stripped, escaped spelling verbatim.
// The api mount and the bare content mount therefore address one node
// namespace (PRD M3: the content path is the same node's second entrance),
// and dispatch still follows the repo row's package type, never the URL's
// protocol spelling alone.
//
// It reports whether a mounted protocol consumed the request; false leaves
// the E-26 envelope 404 to the caller (unregistered protocol, or a prefix
// outside apiProtocolMounts).
func (s *Server) dispatchAPIProtocolMount(w http.ResponseWriter, r *http.Request, rest string) bool {
	for _, proto := range apiProtocolMounts {
		if !strings.HasPrefix(rest, proto+"/") {
			continue
		}
		if _, ok := s.adapters[proto]; !ok {
			// Protocol not mounted on this instance: keep the historical
			// E-01/E-26 envelope 404. The route's assertions flip in the
			// protocol ticket that registers the handler (R5), not here.
			return false
		}
		tail := strings.TrimPrefix(rest, proto+"/")
		s.dispatchContent(w, withAPIProtocolPrefix(r, proto, tail), "")
		return true
	}
	return false
}

// withAPIProtocolPrefix rewrites the request URL from
// /binflow/api/<proto>/<tail> to /binflow/<tail>, preserving the ESCAPED
// spelling verbatim: RawPath carries the raw bytes so %2f vs %2F keep their
// client case and dot segments survive untouched to adapter.Layout — the
// same contract withStrippedPrefix upholds for the content plane. The
// decoded Path is trimmed mechanically (the same literal on both spellings;
// the leading /binflow never contains an escape), and the request context
// flows through unmodified — dispatchContent re-boxes the principal into
// the adapter seam itself.
func withAPIProtocolPrefix(r *http.Request, proto, tail string) *http.Request {
	escaped := prefix + "/" + tail
	decoded := prefix + strings.TrimPrefix(r.URL.Path, prefix+"/api/"+proto)
	u := *r.URL
	u.Path = decoded
	u.RawPath = escaped
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = &u
	r2.RequestURI = r.RequestURI
	return r2
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
