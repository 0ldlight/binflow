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
// deployScopeGuard (M12 T-349, FR-113.3) takes the same position for the
// narrow checksum-deploy tokens: one pre-routing decision point, every
// plane covered.
func (s *Server) rootHandler() http.Handler {
	return s.baseChain(chain(
		authenticate(s.deps.Auth),
		csrfGuard(s.log),
		deployScopeGuard(s.log),
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
			// Weave 1's /v2 arm (M10 T-283, license_gate.go): the registry
			// plane bypasses the /binflow prefix, so the addon gate must
			// live HERE or the docker slot would stand outside it. The
			// token endpoint is an auth plane, never a gated write.
			if !s.gateV2Write(w, r, path) {
				return
			}
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
		//
		// T-332 adds the MPU CAPABILITY routes to the exemption family: the
		// session token those endpoints take is a plane-issued capability
		// the shared verifier cannot know, so a rejected Bearer there is the
		// EXPECTED first leg of the flow, not an attack signal. The handler
		// owns the verdict — every credential that is not the capability
		// still meets the middleware's own 401 (mpuCredentialVerdict), so
		// the never-downgrade posture survives on these routes too.
		if isLoginEntryPoint(r.Method, path) {
			s.log.DebugContext(r.Context(), "httpapi: rejected credential exempted on a login entry point",
				"path", path, "reason", reason)
		} else if isMPUCapabilityRoute(r.Method, path) {
			s.log.DebugContext(r.Context(), "httpapi: rejected credential deferred to the MPU capability lane",
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
	case rest == "/event" || rest == "/event/":
		// Bare /binflow/event: no endpoint at this address (the /api shape).
		notImplemented(w, "/binflow/event")
	case strings.HasPrefix(rest, "/event/"):
		// The unified-event webhook plane (M13 T-362, ADR-0041): the
		// official Event-service namespace /event/api/v1/** under the
		// BinFlow prefix.
		s.dispatchEventAPI(w, r, strings.TrimPrefix(rest, "/event"))
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

// isMPUCapabilityRoute reports whether (method, path) is one of the MPU
// plane's token-addressed endpoints (T-332/ADR-0039): the four per-session
// verbs plus the part PUT target. On these routes the presented credential
// IS the plane's session capability — a value the shared verifier rejects
// by construction — so the dispatch-level hard-401 defers to the handler,
// which re-renders the shared 401 for every non-capability credential.
func isMPUCapabilityRoute(method, path string) bool {
	switch path {
	case prefix + "/api/v1/uploads/urlPart",
		prefix + "/api/v1/uploads/status",
		prefix + "/api/v1/uploads/complete",
		prefix + "/api/v1/uploads/abort":
		return method == http.MethodPost
	}
	return method == http.MethodPut && strings.HasPrefix(path, prefix+"/api/v1/uploads/part/")
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
// admin door, the GC and replication create/update/delete guards).
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

	// ---- /api/v1/system/cleanup (M11 T-324, FR-102.2; sync, lock-guarded)
	// ----
	// POST triggers one run (dry-run default, the gc family's posture —
	// apply is the explicit step after reviewing the dry report); GET is
	// the live status face (schedule, counters, last report, per-repo
	// policy). Gate = system:write / system:read respectively, the same
	// no-dry-run-exception rule as gc (T-214①). Every other spelling
	// falls to the E-26 404.
	case rest == "v1/system/cleanup" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleSystemCleanupPOST)
	case rest == "v1/system/cleanup" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleSystemCleanupGET)

	// ---- /api/v1/system/settings (M13 T-368, FR-118; read-only echo) --
	// The operator-knob echo face: the resolved folder_download six-field
	// family and trashcan.retention_days, knob-scoped by design (never a
	// config dump — no secrets, DSNs or paths; see system_settings.go).
	// GET is the only verb with a route; PUT/POST/DELETE and any
	// sub-path fall to the E-26 404 — these are restart-effective file
	// knobs, not REST-editable state (the addons-plane posture).
	case rest == "v1/system/settings" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleSystemSettings)

	// ---- /api/v1/system/maintenance (M16 T-450, FR-150.3 / ADR-0044
	// decision 7①) ----
	// The maintenance plane's three cron slots (gc / cleanup-unused-cache /
	// cleanup-virtual), each one 021 ledger row under domain='maintenance'.
	// GET reads the projection (cron + next-run + last-run), PUT writes one
	// or more slot arms. Gate = system:read / system:write (the GC/cleanup
	// family's posture — readonly_admin reads, never writes, T-214①). The
	// manual faces (POST /system/gc, /system/cleanup) are untouched —
	// "Run Now" stays those routes. Every other spelling falls to the E-26
	// 404.
	case rest == "v1/system/maintenance" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleSystemMaintenanceGET)
	case rest == "v1/system/maintenance" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleSystemMaintenancePUT)

	// ---- /api/v1/system/backups (M16 T-450, FR-150.3 / ADR-0044 decision
	// 7②) ----
	// The backup configuration CRUD over the 022 payload table + the 021
	// ledger's domain='backup' rows. PUT upserts (the body-key form follows
	// the official single-PUT shape, the {key} form is the by-key alias);
	// DELETE drops payload AND schedule row together. The ADR-0015 erratum
	// ② boundary holds: /api/export/** stays 404, import stays CLI-only —
	// this face configures, it never restores. Gates = system:read /
	// system:write. Every other spelling falls to the E-26 404.
	case rest == "v1/system/backups" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleSystemBackupsList)
	case rest == "v1/system/backups" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleSystemBackupsPut)
	case strings.HasPrefix(rest, "v1/system/backups/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead},
			s.withName(rest, "v1/system/backups/", s.handleSystemBackupsGet))
	case strings.HasPrefix(rest, "v1/system/backups/") && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.withName(rest, "v1/system/backups/", s.handleSystemBackupsPutKey))
	case strings.HasPrefix(rest, "v1/system/backups/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.withName(rest, "v1/system/backups/", s.handleSystemBackupsDelete))

	// ---- /api/v1/system/schedules (M16 T-450, FR-150.3 / ADR-0044
	// decision 7④) ----
	// The ledger's read-only projection (?domain= narrows to one
	// closed-set domain): the FE's next-run countdown / failure / disabled
	// rendering. Read-only by design — writes only exist on the three
	// config faces. Gate = system:read (the addons-plane posture).
	case rest == "v1/system/schedules" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleSystemSchedulesGET)

	// ---- /api/v1/system/query_rate_limiter (M16 T-452, FR-148.2 / aql.md
	// §14.4; K72's admin REST over the DB-query rate plane) ----
	// The limiter's three operations: GET reads the effective settings,
	// POST is the merge-write (the tri-state's BinFlow carrier — the
	// optional "mode" field, see system_qrl.go), DELETE restores the
	// factory state. Gates ride the v1-system-plane capability family
	// (system:read / system:write — the license/settings posture; a plain
	// user meets 403 on all three). Every other spelling falls to the
	// E-26 404.
	case rest == "v1/system/query_rate_limiter/config" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleQRLConfigGet)
	case rest == "v1/system/query_rate_limiter/config" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleQRLConfigPost)
	case rest == "v1/system/query_rate_limiter/config" && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleQRLConfigDelete)

	// ---- /api/v1/system/replications (M15 T-422, FR-138.3; the global
	// blockPush/blockPull emergency brake — replication.md §9.1-B, the
	// A-layer three-endpoint form per §9.6's landing ruling) ----
	// GET answers the official camelCase pair; the two POST verbs take the
	// push/pull query params (default "true"; non-"true" leaves the
	// direction alone this call, §9.2-B-1) and answer text/plain (§9.2-B-3).
	// The block NEVER gates this family itself — configuration traffic keeps
	// flowing while the brake is on (§9.2-B-6 posture, t226's UI-API
	// finding). Every other spelling falls to the E-26 404.
	case rest == "v1/system/replications" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleReplicationGlobalBlockGet)
	case rest == "v1/system/replications/block" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.handleReplicationGlobalBlockSet(true))
	case rest == "v1/system/replications/unblock" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.handleReplicationGlobalBlockSet(false))

	// ---- /api/v1/storage/migration (T-164) ----
	case rest == "v1/storage/migration" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleMigrationStatus)
	case rest == "v1/storage/migration/start" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleMigrationStart)

	// ---- /api/v1/replications (T-180, ADR-0021; PUT T-405; POST run T-420) --
	// The push-replication configuration plane. GET lists (secrets
	// excluded), POST creates, PUT /{id} flips one config's enabled bit (the
	// console start/stop switch — the T-405 mini face, scoped to the bit
	// alone), POST /{id}/run schedules one full sync of the addressed config
	// (the Replicate Now face, T-420/FR-138.1 — async seeding, observability
	// rides the status face below), DELETE /{name} drops one config — its
	// task rows cascade via the 009 FK. The sibling /api/v1/replication/
	// status below is the console panel's aggregated read face (T-159).
	case rest == "v1/replications" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleReplicationList)
	case rest == "v1/replications" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleReplicationCreate)
	case strings.HasPrefix(rest, "v1/replications/") && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.withName(rest, "v1/replications/", s.handleReplicationUpdate))
	case strings.HasPrefix(rest, "v1/replications/") && r.Method == http.MethodPost:
		// Only the /{id}/run, /{id}/test and the id-less draft /test
		// spellings have routes; every other POST under the prefix keeps
		// the family's E-26 404.
		idRaw, tail := splitAPIName(rest, "v1/replications/")
		if idRaw != "" && tail == "run" {
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
				func(w http.ResponseWriter, r *http.Request) { s.handleReplicationRun(w, r, idRaw) })
			return
		}
		if idRaw != "" && tail == "test" {
			// T-422 (FR-138.2, §9.3): probe the stored config, the optional
			// body overriding single fields for this probe alone.
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
				func(w http.ResponseWriter, r *http.Request) { s.handleReplicationTest(w, r, idRaw) })
			return
		}
		if idRaw == "test" && tail == "" {
			// T-422 (§9.2-C-9): the id-less DRAFT face — an unsaved form
			// probes its candidate; nothing is read from or written to the
			// store beyond the audit row.
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleReplicationTestDraft)
			return
		}
		notImplemented(w, "/binflow/api/"+rest)
	case strings.HasPrefix(rest, "v1/replications/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
			s.withName(rest, "v1/replications/", s.handleReplicationDelete))

	// ---- /api/v1/uploads (M10 T-289, FR-90.1 / architecture §15.4; wire
	// flipped whole to the Artifactory shape in M11 T-332 / ADR-0039) ----
	// Six endpoints, every data verb POST with QueryParam parameters, plus
	// the urlPart contract's PUT target (part). create and config keep the
	// route auth gate (RolesAllowed admin,user — the principal door); the
	// four per-session verbs and the part PUT ride the CAPABILITY lane: the
	// session token the create verb issued is the only credential they take
	// (the part PUT also accepts shared credentials + `w`, its manual-driver
	// arm), which is why they carry NO enforce gate — the handler owns the
	// verdict and renders the shared middleware's own 401 for every
	// credential that is not the token (the dispatch-level exemption,
	// isMPUCapabilityRoute). A backend without storage.MultipartUploads
	// (filestore, dual-write) answers the honest plain-text 501 on the five
	// data endpoints; config is the probe and answers 200 supported:false
	// (FR-90-AC3 + ADR-0039).
	case rest == "v1/uploads/create" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUploadsCreate)
	case rest == "v1/uploads/config" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true}, s.handleUploadsConfig)
	case rest == "v1/uploads/urlPart" && r.Method == http.MethodPost:
		s.handleUploadsURLPart(w, r)
	case rest == "v1/uploads/status" && r.Method == http.MethodPost:
		s.handleUploadsStatus(w, r)
	case rest == "v1/uploads/complete" && r.Method == http.MethodPost:
		s.handleUploadsComplete(w, r)
	case rest == "v1/uploads/abort" && r.Method == http.MethodPost:
		s.handleUploadsAbort(w, r)
	case strings.HasPrefix(rest, "v1/uploads/part/") && r.Method == http.MethodPut:
		s.withUploadsPart(rest, "v1/uploads/part/", s.handleUploadsPart)(w, r)

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

	// ---- /api/v1/admin/security/{ldap,oauth,saml/config} (M11 T-305,
	// ADR-0035 / FR-92) ----
	// The authentication-configuration plane: one read/write/test trio
	// per protocol section (dispatchAPI explicit routes, errors[]
	// envelope — the ADR-0034 management-face posture). Reads sit on
	// CapSecurityRead (readonly_admin sees the masked sections, 92.4);
	// writes and test connections on CapSecurityWrite. The test verbs
	// are writes because they OPEN OUTBOUND CONNECTIONS against
	// operator-supplied targets (the M3 Guard machine screens them —
	// NFR-S60's "zero new SSRF face" rides the guard, and the tighter
	// gate keeps probing an admin action).
	case rest == "v1/admin/security/ldap" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.handleAuthConfigGet(auth.SectionLDAP))
	case rest == "v1/admin/security/ldap" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigPut(auth.SectionLDAP))
	case rest == "v1/admin/security/ldap/test" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigTest(auth.SectionLDAP))
	case rest == "v1/admin/security/oauth" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.handleAuthConfigGet(auth.SectionOIDC))
	case rest == "v1/admin/security/oauth" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigPut(auth.SectionOIDC))
	case rest == "v1/admin/security/oauth/test" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigTest(auth.SectionOIDC))
	case rest == "v1/admin/security/saml/config" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.handleAuthConfigGet(auth.SectionSAML))
	case rest == "v1/admin/security/saml/config" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigPut(auth.SectionSAML))
	case rest == "v1/admin/security/saml/config/test" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleAuthConfigTest(auth.SectionSAML))

	// ---- /api/v1/admin/security/saml/{config/key/public…,key} (M11 T-331,
	// the T-307 registered gap; auth-integration §3.2) ----
	// The SAML service-provider encryption certificate family. The
	// download/regenerate pair re-homes Artifactory's
	// /ui/api/v1/admin/security/saml/config/key/public[/regenerate] faces
	// (text/plain both ways, high confidence); the POST key verb is the
	// BinFlow-native generation face — Artifactory generates the pair only
	// implicitly on an encrypted-assertion save, and BinFlow keeps that
	// §3.3 behavior while also exposing the explicit create-or-replace
	// action (the GPG plane's D-1 posture). Reads ride CapSecurityRead
	// (the certificate is public material the readonly admin may hand to
	// the IdP); both writes force a fresh instance pair and answer the new
	// certificate — rotation invalidates the old one atomically.
	case rest == "v1/admin/security/saml/config/key/public" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.handleSAMLKeyPublic)
	case rest == "v1/admin/security/saml/config/key/public/regenerate" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleSAMLKeyRotate(auditActionSAMLKeyRegenerate))
	case rest == "v1/admin/security/saml/key" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.handleSAMLKeyRotate(auditActionSAMLKeyGenerate))

	// ---- /api/security/keypair* and the keypair association faces
	// (M11 T-319, ADR-0038 / docs/design/gpg-keypair.md) ----
	// The Artifactory-compatible instance keypair family plus the
	// BinFlow-native generation endpoint and the v2 repository-association
	// face. Reads sit on CapSecurityRead, every write and the verify verb
	// on CapSecurityWrite (spec divergence D-1: BinFlow's RBAC posture —
	// public-key distribution rides the content plane files, not these
	// endpoints). Literal routes precede the /{pairName} prefix arm so
	// "verify" and the public-key family can never be read as pair names.
	case rest == "security/keypair" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.handleKeypairImport)
	case rest == "security/keypair" && r.Method == http.MethodPut:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.handleKeypairUpdate)
	case rest == "security/keypair" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead}, s.handleKeypairList)
	case rest == "security/keypair/verify" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.handleKeypairVerify)
	case strings.HasPrefix(rest, "security/keypair/public/repositories/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.withName(rest, "security/keypair/public/repositories/", s.handleKeypairPublicByRepo))
	case strings.HasPrefix(rest, "security/keypair/") && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityRead},
			s.withName(rest, "security/keypair/", s.handleKeypairGet))
	case strings.HasPrefix(rest, "security/keypair/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite},
			s.withName(rest, "security/keypair/", s.handleKeypairDelete))

	// ---- /api/v1/admin/security/keypair/generate (T-319; the BinFlow
	// native keygen — Artifactory publishes no generation REST, spec
	// section 2.2) ----
	case rest == "v1/admin/security/keypair/generate" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.handleKeypairGenerate)

	// ---- /api/v2/repositories/{repoKey}/keyPairs (T-319; the Artifactory
	// 7.19 association face: plain-text body carries the pair name; the
	// write rides the repository update path, so the class/package-type
	// matrix and the reference-existence rule apply verbatim) ----
	case strings.HasPrefix(rest, "v2/repositories/") && strings.HasSuffix(rest, "/keyPairs") && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.routeKeypairAssociate(rest))
	case strings.HasPrefix(rest, "v2/repositories/") && strings.Contains(rest, "/keyPairs/") && r.Method == http.MethodDelete:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSecurityWrite}, s.routeKeypairDisassociate(rest))

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
		case tail == "test" && r.Method == http.MethodPost:
			// T-442 (FR-143.5): the remote form's Test connectivity probe.
			// The gate matches the configuration-write arms — the probe is
			// part of the editing workflow and may carry draft credentials,
			// so a read-only manager cannot aim the stored credential at
			// arbitrary hosts through it.
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleRepositoryTest(w, r, key)
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
		if _, ok := r.URL.Query()["stats"]; ok && r.Method == http.MethodGet {
			// M16 T-438 (FR-146.2 / ADR-0044 K69): the per-node download
			// statistics (StatsInfo). The route keeps the item-info read
			// gate — the counts are content-plane facts, anonymous follows
			// the flag; the identity arm (lastDownloadedBy) is a FIELD-level
			// gate inside the handler on CapSystemRead, the audit log read's
			// capability (K69 decision 5's "non-tier callers get the field
			// omitted", which is why this route must NOT 403 them here).
			s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
				s.handleStorageStats(w, r, repoKey, rel)
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
		if _, ok := r.URL.Query()["properties"]; ok {
			// FR-89 (M10 T-286, architecture section 15.3.3): the property
			// read/write family — the E-09 gap's redemption, riding the same
			// route as a third query arm. GET keeps the item-info read gate
			// (content-plane semantics, anonymous follows the flag); the
			// mutating verbs demand authentication at the route and the
			// path's `w` inside the handler (the same Authorizer the
			// content plane consults — properties are metadata, not content,
			// so no overwrite/`d` coupling). Every other verb on the arm
			// falls to the E-26 404 below, the family's frozen posture for
			// spellings it does not define.
			switch r.Method {
			case http.MethodGet:
				s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
					s.handleStoragePropertiesGet(w, r, repoKey, rel)
				})
			case http.MethodPut:
				s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
					s.handleStoragePropertiesPut(w, r, repoKey, rel)
				})
			case http.MethodDelete:
				s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
					s.handleStoragePropertiesDelete(w, r, repoKey, rel)
				})
			default:
				notImplemented(w, "/binflow/api/"+rest)
			}
			return
		}
		if r.Method == http.MethodGet {
			s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
				s.handleStorageItem(w, r, repoKey, rel)
			})
			return
		}
		notImplemented(w, "/binflow/api/"+rest)

	// ---- /api/copy, /api/move (M12 T-339, FR-105.1 / repo-operations.md
	// sections 0/1) ----
	// POST is the only verb with a route; the bare and keyless spellings
	// still reach the handler so IT can answer the spec's 400 parameter
	// messages ("Source repository key is empty"), not the E-26 404. The
	// route demands authentication (RolesAllowed admin,user — the family's
	// per-item chain is the real permission matrix); the license gate is
	// the handler's first line (Q4: the family rides the pro slot).
	case rest == "copy" || rest == "copy/" || strings.HasPrefix(rest, "copy/"):
		if r.Method != http.MethodPost {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
			s.handleCopyMove(w, r, "copy", rest)
		})
	case rest == "move" || rest == "move/" || strings.HasPrefix(rest, "move/"):
		if r.Method != http.MethodPost {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
			s.handleCopyMove(w, r, "move", rest)
		})

	// ---- /api/archive/download/{repoKey}[/{path}] (M12 T-343, FR-105.3 /
	// repo-operations.md sections 0/2) ----
	// The folder download. GET is the only verb with a route; the route
	// carries NO required gate — the §2.2 template's anonymous 401 carries
	// its own spec wording inside the handler, and a route-level 401
	// challenge would mask it. The license gate (the family's shared
	// repo-operations slot) is the handler's first line.
	case rest == "archive/download" || rest == "archive/download/" || strings.HasPrefix(rest, "archive/download/"):
		if r.Method != http.MethodGet {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{}, func(w http.ResponseWriter, r *http.Request) {
			s.handleArchiveDownload(w, r, rest)
		})

	// ---- /api/trash (M12 T-345, FR-106 / inv-2-surface section 1.A) ----
	// The trash family's three verbs: POST empty, POST restore/{path}
	// (to/transaction-size ride the query), DELETE clean/{path}. Browsing
	// is NOT a fourth route — the standing storage face serves
	// /api/storage/auto-trashcan (item info, ?list, ?properties), the
	// console tree's Trash Can node. The route gate is system:write (the
	// destructive-management posture of the gc/cleanup family:
	// readonly_admin 403); the license gate is the handler's first line
	// (the Q3 interim: the trashcan slot rides at pro).
	case rest == "trash/empty" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleTrashEmpty)
	case rest == "trash/restore" || rest == "trash/restore/" || strings.HasPrefix(rest, "trash/restore/"):
		if r.Method != http.MethodPost {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, func(w http.ResponseWriter, r *http.Request) {
			s.handleTrashRestore(w, r, rest)
		})
	case rest == "trash/clean" || rest == "trash/clean/" || strings.HasPrefix(rest, "trash/clean/"):
		if r.Method != http.MethodDelete {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, func(w http.ResponseWriter, r *http.Request) {
			s.handleTrashClean(w, r, rest)
		})

	// ---- /api/search (SR-01/SR-02, T-92 + AQL M15 T-415 + T-417 trio + M16 usage T-440) ----
	// The legacy pair opened the M1 E-26 search domain (PRD M4: artifact +
	// checksum only). The M15 axis completes the first batch: AQL's POST
	// entrance (T-415) plus the old-search trio gavc/prop/pattern (T-417,
	// FR-134 — the SR-03/SR-04 closure assertions flipped in that ticket);
	// M16 adds the usage member (T-440, FR-148.1) — the 404-empty-set
	// family's first door, riding the AQL engine's fixed statistics-domain
	// template. Every remaining family member — users/artifactory/badge,
	// the misspelled plural "props" (the official member is prop), and any
	// foreign verb on an open path — keeps the E-26 404. The gates mirror
	// /api/storage's read posture: the use case owns the anonymous-channel
	// decision, so a closed instance answers the spec's 403 rather than a
	// route-level 401 challenge.
	//
	// The AQL entrance (FR-133.3, aql.md §1) is the family's POST member:
	// the query rides a text/plain body (?query= fallback, ?compact toggle),
	// and the anonymous gate lives INSIDE the handler — AQL is never
	// anonymous, and its two spec arms (401 closed instance / 403 open
	// instance, §4 E5/E6) would both be masked by a route-level 401
	// challenge. Usage (aql.md §14.2) is the same handler-gated shape: a
	// privileged non-anonymous face whose anonymous arm is the 401
	// challenge. Foreign verbs on the path keep the E-26 404 like the rest
	// of the family.
	case rest == "search/aql" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{}, s.handleSearchAQL)
	case rest == "search/artifact" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchArtifact)
	case rest == "search/checksum" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchChecksum)
	case rest == "search/gavc" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchGavc)
	case rest == "search/prop" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchProp)
	case rest == "search/pattern" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchPattern)
	case rest == "search/usage" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{}, s.handleSearchUsage)
	case rest == "search/creation" && r.Method == http.MethodGet:
		// M16 T-452 (FR-148.3 / aql.md §14.3): the K65 carry-over pair's
		// first door — the 404-empty family, epoch-milliseconds range. The
		// anonymous arm is the handler's 401 (the usage posture), so the
		// route gate stays open like the family's.
		s.enforce(w, r, routeAuth{}, s.handleSearchCreation)
	case rest == "search/dates" && r.Method == http.MethodGet:
		// M16 T-452: the pair's second door (dateFields CSV over the closed
		// four-name set).
		s.enforce(w, r, routeAuth{}, s.handleSearchDates)

	// ---- /api/{artifactsearch,stashResults,packagesSearch,syntax-search}
	// (M16 T-452, FR-148.3 / aql.md §14.5 — the UI search family, re-homed
	// onto the api tree: the SAML key family precedent; BinFlow has no
	// ui/api REST segment) ----
	// Family gate: @RolesAllowed(admin, user) → any non-anonymous principal
	// (a 401 challenge for anonymous, never a capability door). The read
	// doors reuse the legacy kernels' own ACL weave; stashResults ships in
	// the anchor's factory-off posture (every operation the verbatim 404
	// copy); deleteArtifact is deliberately unrouted (the anchor's own
	// read-only registration — the E-26 404 answers it).
	case rest == "artifactsearch/quick" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIArtifactSearchQuick)
	case rest == "artifactsearch/gavc" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIArtifactSearchGavc)
	case rest == "artifactsearch/checksum" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIArtifactSearchChecksum)
	case rest == "artifactsearch/trash" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIArtifactSearchTrash)
	case rest == "artifactsearch/pkg/tonative" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIArtifactSearchToNative)
	case strings.HasPrefix(rest, "artifactsearch/pkg/"):
		key, tail := splitAPIName(rest, "artifactsearch/pkg/")
		if key != "" && tail == "" {
			switch r.Method {
			case http.MethodGet:
				s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
					s.handleUIArtifactSearchPkgGet(w, r, key)
				})
			case http.MethodPost:
				s.enforce(w, r, routeAuth{required: true}, func(w http.ResponseWriter, r *http.Request) {
					s.handleUIArtifactSearchPkgPost(w, r, key)
				})
			default:
				notImplemented(w, "/binflow/api/"+rest)
			}
			return
		}
		notImplemented(w, "/binflow/api/"+rest)
	case (rest == "stashResults" || strings.HasPrefix(rest, "stashResults/")) &&
		(r.Method == http.MethodPost || r.Method == http.MethodGet || r.Method == http.MethodDelete):
		// The factory-off family: all ten operations answer the verbatim
		// 404 copy (aql.md §14.5 — the Smart Searches pro-tier ruling
		// BinFlow ships with; the E-26 envelope carries the message).
		s.enforce(w, r, routeAuth{required: true}, handleUIStashDisabled)
	case rest == "packagesSearch/leadFile" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIPackagesLeadFile)
	case rest == "packagesSearch/artifacts" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUIPackagesArtifacts)
	case rest == "syntax-search" && r.Method == http.MethodPost:
		s.enforce(w, r, routeAuth{required: true}, s.handleUISyntaxSearch)

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

	// ---- /api/helm/{repoKey}/reindex[/{path}] (M11 T-309, ADR-0034
	// clause 1 / helm.md section 5.2) ----
	// The classic-Helm management plane: the dispatchAPI EXPLICIT family,
	// gated authentication + the single-repo manage bit (CanManageRepo —
	// the ADR's management-face posture, NOT the apiProtocolMounts content
	// alias below). The bare form schedules the whole-repository async
	// rebuild; the /{path} form runs the partial rebuild synchronously.
	// Every other /api/helm spelling falls through to the content alias.
	case strings.HasPrefix(rest, "helm/"):
		key, tail := splitAPIName(rest, "helm/")
		switch {
		case tail == "reindex" && r.Method == http.MethodPost && key != "":
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleHelmReindex(w, r, key)
				})
		case strings.HasPrefix(tail, "reindex/") && r.Method == http.MethodPost && key != "":
			nodePath, ok := splitHelmReindexPath(tail)
			if !ok {
				notImplemented(w, "/binflow/api/"+rest)
				return
			}
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleHelmReindexPath(w, r, key, nodePath)
				})
		default:
			// The content alias (GET/HEAD only) or the E-26 404.
			if s.dispatchAPIProtocolMount(w, r, rest) {
				return
			}
			notImplemented(w, "/binflow/api/"+rest)
		}

	// ---- /api/yum/{repoKey} (M11 T-311, ADR-0034 / rpm.md section 3.2) ----
	// The YUM management plane: the dispatchAPI EXPLICIT family, gated
	// authentication + the single-repo manage bit (CanManageRepo — the
	// ADR's management-face posture). The bare /api/yum spelling answers
	// the blank-key 400 branch; async=1 schedules the whole-repository
	// recomputation in the background, async=0 runs it synchronously (or
	// hits the 409 auto-async conflict). Every other /api/yum spelling
	// falls to the E-26 404.
	case rest == "yum" || rest == "yum/":
		if r.Method == http.MethodPost {
			s.enforce(w, r, routeAuth{required: true}, s.handleYumReindexBlankKey)
			return
		}
		notImplemented(w, "/binflow/api/"+rest)
	case strings.HasPrefix(rest, "yum/"):
		key, tail := splitAPIName(rest, "yum/")
		if tail == "" && r.Method == http.MethodPost && key != "" {
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleYumReindex(w, r, key)
				})
			return
		}
		notImplemented(w, "/binflow/api/"+rest)

	// ---- /api/deb/reindex/{repoKey} (M11 T-310, ADR-0034 / debian.md
	// section 4.3) ----
	// The Debian management plane: the dispatchAPI EXPLICIT family, gated
	// authentication + the single-repo manage bit (CanManageRepo — the
	// ADR's management-face posture, the /api/yum shape). The Artifactory
	// spelling puts reindex BEFORE the key; the bare /api/deb/reindex
	// spelling answers the blank-key 400 branch. Every other /api/deb
	// spelling falls to the E-26 404.
	case rest == "deb/reindex" || rest == "deb/reindex/":
		if r.Method == http.MethodPost {
			s.enforce(w, r, routeAuth{required: true}, s.handleDebReindexBlankKey)
			return
		}
		notImplemented(w, "/binflow/api/"+rest)
	case strings.HasPrefix(rest, "deb/reindex/"):
		key := strings.TrimPrefix(rest, "deb/reindex/")
		if r.Method == http.MethodPost && key != "" {
			s.enforce(w, r, routeAuth{required: true, repoManage: &repoManageGate{repo: key, write: true}},
				func(w http.ResponseWriter, r *http.Request) {
					s.handleDebReindex(w, r, key)
				})
			return
		}
		notImplemented(w, "/binflow/api/"+rest)

	// ---- /api/conan/…/reindex (M11 T-308, ADR-0034 / conan.md 3.1) ----
	// The conan management plane rides a self-contained adapter handler
	// (conan.ManagementHandler): the router only authenticates — the
	// handler itself walks the CanManageRepo(write) gate, the local-only
	// class check and both reindex spellings (the whole-repository form
	// with repoKey in query/body, and the path form
	// /api/conan/{repoKey}[/{sub}]/reindex). The two spellings intercept
	// before the generic protocol mount below; every other /api/conan
	// spelling stays the data plane's.
	case rest == "conan/reindex" ||
		(strings.HasPrefix(rest, "conan/") && strings.HasSuffix(rest, "/reindex")):
		h, ok := s.mgmt["conan"]
		if !ok {
			notImplemented(w, "/binflow/api/"+rest)
			return
		}
		s.enforce(w, r, routeAuth{required: true}, h.ServeHTTP)

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
// …/api/pypi/<repo>/simple/…) rather than bare content paths, and nuget's
// v3/v2 planes live at …/api/nuget/{v3,v2}/<repo>/… (the Artifactory-
// compatible spellings PRD FR-88 fixes); helm (T-309, HL-1) mounts its
// read-only download alias …/api/helm/<repo>/… for Artifactory-habituated
// `helm repo add` URLs (see apiReadOnlyMounts). The list is CLOSED by
// design: the api segment primarily hosts REST routes, so a protocol
// earns a mount only through its own ticket — maven mounts content paths
// with zero httpapi changes, and look-alikes (…/api/pypi-ui/**) stay on
// the E-26 404 permanently.
var apiProtocolMounts = []string{"npm", "pypi", "nuget", "helm"}

// apiReadOnlyMounts lists the protocols whose api/<proto> alias is
// READ-ONLY (T-309 / HL-1): the classic-Helm alias serves the
// Artifactory-habituated download face (api/helm/<repo>/index.yaml and
// chart paths for `helm repo add`), while every UPLOAD addresses the
// content plane — the exact curl -T posture JFrog documents. A write verb
// through the alias answers 405 (Allow: GET, HEAD) before the rewrite,
// so the alias can never become a second upload entrance.
var apiReadOnlyMounts = map[string]bool{"helm": true}

// apiPlaneMounts lists the protocols whose api mount carries a PLANE
// segment before the repository key (nuget's v3/v2). For these the
// rewrite is plane-aware: /binflow/api/nuget/v3/<repo>/<rest> rewrites to
// /binflow/<repo>/v3/<rest>, so the content dispatch's repo lookup, the
// addon gate and the adapter dispatch all run on the repository key
// exactly once, and the plane stays part of the repository's own path
// namespace (the adapter's route grammar).
var apiPlaneMounts = map[string]bool{"nuget": true}

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
		if apiReadOnlyMounts[proto] && r.Method != http.MethodGet && r.Method != http.MethodHead {
			// The read-only alias's 405 (HL-1): uploads address the content
			// plane; the api/<proto> spelling never becomes a second write
			// entrance.
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed,
				"The /binflow/api/"+proto+" alias is read-only; upload and delete address the content plane (/binflow/<repository>/...).")
			return true
		}
		tail := strings.TrimPrefix(rest, proto+"/")
		if apiPlaneMounts[proto] {
			// The plane-aware arm (nuget): tail = {plane}/{repoKey}/{rest},
			// the rewrite swaps the two front segments so the content
			// dispatch sees the repository key first. An absent or unknown
			// plane is not this mount's grammar — false leaves the E-26
			// envelope 404.
			r2, ok := withAPIPlanePrefix(r, proto, tail)
			if !ok {
				return false
			}
			s.dispatchContent(w, r2, "")
			return true
		}
		s.dispatchContent(w, withAPIProtocolPrefix(r, proto, tail), "")
		return true
	}
	return false
}

// withAPIPlanePrefix rewrites /binflow/api/<proto>/{plane}/{repo}/{rest}
// to /binflow/{repo}/{plane}/{rest} (the plane-segment twin of
// withAPIProtocolPrefix, preserving the ESCAPED spelling verbatim:
// RawPath carries the raw bytes so percent-encodings keep their client
// case and dot segments survive untouched to adapter.Layout). ok is
// false when the tail does not carry the two front segments (or the
// plane segment is not a plausible plane spelling — the nuget adapter's
// own grammar owns the final word; the router only moves the front two
// apart).
func withAPIPlanePrefix(r *http.Request, proto, tail string) (*http.Request, bool) {
	plane, after, found := strings.Cut(tail, "/")
	if !found || plane == "" {
		return nil, false
	}
	repoKey, rest, found := strings.Cut(after, "/")
	// T-337: an empty rest is the service-document root — nuget v2's
	// canonical base (/binflow/api/nuget/v2/<repo>[/] serves the doc on
	// GET and takes the v2 direct-push PUT). The adapter owns what an
	// empty remainder means per plane; the router only guarantees the
	// two front segments exist.
	if !found || repoKey == "" {
		return nil, false
	}
	// Plane spellings are short lowercase literals (v2/v3); a segment
	// with escapes or unusual length is not one, and passing it through
	// would address a repository key in the plane slot.
	if len(plane) > 8 {
		return nil, false
	}
	for i := 0; i < len(plane); i++ {
		if c := plane[i]; c < 'a' || c > 'z' {
			if c < '0' || c > '9' {
				return nil, false
			}
		}
	}
	escaped := prefix + "/" + repoKey + "/" + plane + "/" + rest
	decodedPrefix := prefix + "/api/" + proto + "/" + plane + "/" + repoKey
	decoded := prefix + "/" + repoKey + "/" + plane + strings.TrimPrefix(r.URL.Path, decodedPrefix)
	u := *r.URL
	u.Path = decoded
	u.RawPath = escaped
	r2 := new(http.Request)
	*r2 = *r
	r2.URL = &u
	r2.RequestURI = r.RequestURI
	return r2, true
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

// couchUserPrefix is the npm legacy login's user-id family
// (docs/reverse/npm.md K60-1: PUT /-/user/org.couchdb.user:<name>). The
// literal lives here, not in the npm adapter, because the adapter packages
// are leaves — httpapi never imports them (architecture section 2), so the
// router's own grammar for the exempt family is spelled out beside its
// consumer.
const couchUserPrefix = "org.couchdb.user:"

// isCouchUserDocument reports whether rel — the ESCAPED repo-relative tail
// of a content-plane request — spells the npm legacy login's couch
// user-document family (K60-1, plus the E409 retry spelling K60-6 pins as
// never-reachable-but-served): "-/user/org.couchdb.user:<name>" and
// ".../-rev/<rev>". The comparison runs on the escaped segments exactly as
// the client spelled them (npm encodes the username; the structural
// segments are literal), and the structural pins — a leading "-/" then
// "user", an id segment that starts at the couch prefix and carries no
// "/", at most the one "-rev/<rev>" pair — keep the family from swallowing
// any package, dist-tag or tarball address. The npm adapter's own decoded
// grammar (parseRoute) remains the authority: a spelling this predicate
// matches but that grammar rejects answers the adapter's 404, never a
// write.
func isCouchUserDocument(rel string) bool {
	rel = strings.TrimSuffix(rel, "/") // parseRoute tolerates one trailing slash
	segs := strings.Split(rel, "/")
	if len(segs) < 3 || segs[0] != "-" || segs[1] != "user" {
		return false
	}
	if id := segs[2]; !strings.HasPrefix(id, couchUserPrefix) || id == couchUserPrefix {
		return false
	}
	switch len(segs) {
	case 3:
		return true
	case 5:
		return segs[3] == "-rev" && segs[4] != ""
	}
	return false
}

// npmCouchLoginExempt reports whether the request is the npm legacy login
// PUT whose credential rides the BODY (K60-1's single fix surface, T-394):
// the couch convention sends no Authorization header, so the content
// plane's write gate would 401 the request at the door and the adapter's
// body-credential arm could never run. The gate stands down for exactly
// that path family — and only on an npm repository: the row is resolved
// HERE, before the gate, because the path family alone must never open an
// anonymous write on any other type's content plane (a generic repo asked
// to PUT "-/user/…" would happily store it as a node). An unknown repo
// key, a lookup failure or a non-npm type keeps the standard write gate,
// and the inner handler's own repo lookup stays the single dispatch
// authority (the extra Get here rides only the rare login PUT).
//
// Standing the gate down means the whole write-face posture for this one
// request: no credential requirement, no repo-path write ACL and no addon
// entitlement question — login is an authentication face, not an artifact
// write (K60 §2.3: the adapter verifies and mints, zero artifacts touch
// disk, and the route carries no repository permission check).
func (s *Server) npmCouchLoginExempt(r *http.Request) bool {
	if r.Method != http.MethodPut {
		return false
	}
	repoKey, rel := splitFirstSegment(r)
	if !isCouchUserDocument(rel) {
		return false
	}
	row, err := s.deps.Repos.Get(r.Context(), repoKey)
	if err != nil || row == nil {
		return false
	}
	return row.PackageType == repo.PackageNpm
}

// dispatchContent routes /binflow/<repo-key>/** to the adapter serving
// that repository's package type (architecture section 5.1 dispatch).
// Authorization runs on the raw (prefixed) path first — the ACL is keyed
// by repo name and does not depend on the repo row existing — then the
// adapter receives the request with the /binflow prefix stripped and the
// principal in its own context seam.
//
// Two archive-family intercepts (M12 T-343) live on this plane because
// their addressing is CROSS-PROTOCOL, not a package-type concern:
//
//   - a decoded "!/" marker addresses an ARCHIVE MEMBER (section 3) — it
//     is split and served before the adapter dispatch, so every mounted
//     protocol gets the family uniformly;
//   - a PUT carrying X-Explode-Archive[: -Atomic] (section 4) is the
//     exploded upload — intercepted inside the inner handler AFTER the
//     RBAC write gate and the package-type gate, BEFORE the adapter
//     dispatch (the M10 E-25 explicit 400 refusal, reversed per PRD
//     105.3; the adapters' own refusal arm stays as their bare-mount
//     defense, unreachable through this router).
func (s *Server) dispatchContent(w http.ResponseWriter, r *http.Request, _ string) {
	if tail, ok := archiveMemberTail(r); ok {
		s.handleArchiveMember(w, r, tail)
		return
	}
	action, required := contentAction(r.Method)
	p := principalFrom(r.Context())

	// The K60-1 gate exemption (T-394): the npm legacy login PUT carries
	// its credential in the body, couch-style, so the write gate must let
	// it through to the adapter's body arm. npmCouchLoginExempt narrows the
	// stand-down to that one path family on npm repositories — every other
	// write surface (publish, dist-tags, unpublish, the same path family
	// on any other repo type) keeps the gate below untouched.
	if required && s.npmCouchLoginExempt(r) {
		action, required = "", false
	}

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		repoKey, _ := splitFirstSegment(r)
		row, err := s.deps.Repos.Get(r.Context(), repoKey)
		if err != nil {
			s.writeRepoLookupError(w, repoKey, err)
			return
		}
		// The entitlement gate, weave point 1 (M10 T-283, section 15.1.5's
		// pseudocode site): WRITE verbs only — `required` is contentAction's
		// own write flag, reused verbatim — and strictly after the RBAC arm
		// that wrapped this inner handler. Reads fall straight through
		// (D1); the adapter-internal writes behind this point (a remote
		// pull-through landing its copy) never re-ask the question.
		if required && !s.gateAddonWrite(w, r, repoKey, row.PackageType) {
			return
		}
		// The exploded-upload intercept: the trigger headers are consumed
		// before any adapter sees the request (section 4; the license
		// question rides the family's shared repo-operations slot, asked
		// after the RBAC and package-type gates above).
		intent, explode, err := explodeIntent(r)
		switch {
		case err != nil:
			writeError(w, http.StatusBadRequest, err.Error())
			return
		case explode:
			s.handleExplode(w, r, repoKey, intent)
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
