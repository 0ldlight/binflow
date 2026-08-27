package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/addons"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/docs"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/metrics"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// RepoLookup is the consumer-side repository-metadata seam the router
// needs to resolve a repo key to its package type for dispatch. The real
// repo.Service's GetRepo demands an authenticated principal, but the
// ROUTER must resolve anonymous content reads too (anonymous GET passes
// before any adapter runs, ADR-0009): the seam therefore reads the row
// directly. The package type is routing data, not protected content — an
// anonymous reader who learns that a repo key exists has learned nothing
// the download itself wouldn't reveal, and the row's config fields never
// cross this seam (only PackageType is read).
type RepoLookup interface {
	Get(ctx context.Context, repoKey string) (*metadata.Repo, error)
}

// Deps carries every collaborator the HTTP core needs. All fields are
// required except Version/Revision (empty Version reports DefaultVersion).
type Deps struct {
	Config   *config.Config
	Auth     auth.Authenticator
	Authz    auth.Authorizer
	Metadata metadata.Store
	Repos    RepoLookup
	// ReposSvc is the repository use-case service behind the compatible
	// /api/repositories and /api/storage planes (T-15). The router's own
	// dispatch uses the lighter RepoLookup above; these handlers need the
	// full service (permission-checked operations).
	ReposSvc repo.Service
	// Passwords rotates account passwords (PUT /api/security/password and
	// its real-route alias).
	Passwords auth.PasswordChanger
	// Tokens issues and revokes API tokens (/api/security/token[/revoke]).
	Tokens auth.TokenRegistry
	// GC is the storage engine's mark-sweep face (POST /api/v1/system/gc,
	// T-94). Nil on stacks assembled without an engine — the endpoint
	// answers 503 rather than pretending a run happened.
	GC GarbageCollector
	// Cleanup is the unused-cleanup engine (M11 T-324, FR-102.2): the
	// cron-driven remote-cache policy face behind POST/GET
	// /api/v1/system/cleanup. Nil keeps POST at the honest 503 and GET at
	// the engine-less status body — unit stacks only; every assembled
	// server wires the engine (its Run loop is a cmd lifecycle concern,
	// the licenseMgr precedent).
	Cleanup CleanupEngine
	// Migration is the optional S3 migration engine (T-164). Nil when
	// migration is not configured — the endpoints answer 501.
	Migration MigrationStarter
	// BlobInventory is the engine-backed sizing seam behind the physical
	// half of /api/v1/storage/stats, the /metrics storage-byte gauge and the
	// GC candidate sizing (T-201, T-173 D-1). cmd wires it only when the
	// assembly's engine is the S3 one (a dual-write stack keeps the disk
	// walk — its data dir still carries every blob); nil preserves the M1
	// disk behavior.
	BlobInventory BlobInventory
	// Uploads is the S3 multipart-session seam behind /api/v1/uploads (M10
	// T-289, FR-90.1 / architecture section 15.4): cmd wires it exactly
	// when the assembly's engine carries storage.MultipartUploads (the pure
	// S3 backend). Nil — filestore and dual-write stacks — keeps all six
	// endpoints at the honest plain-text 501 (FR-90-AC3): the feature is
	// absent on that backend, not the route.
	Uploads storage.MultipartUploads
	// Replication is the push-replication store seam (T-180, ADR-0021):
	// the /api/v1/replications CRUD and /api/v1/replication/status ride it.
	// Nil leaves those endpoints at 501 — the console panel's "replication
	// not enabled" degradation (T-159 contract ruling 6).
	Replication replication.Store
	// ReplicationCipher seals target passwords at config-create time
	// (ADR-0012 enc:v1 at-rest form; the same master key the engine
	// decrypts with). Nil refuses password-carrying creates with a 400
	// naming BINFLOW_REMOTE_CREDENTIALS_KEY — plaintext is never stored.
	ReplicationCipher CredentialEncryptor
	// OIDC is the optional OIDC login-flow collaborator (T-157, ADR-0020,
	// OD-01/OD-02): cmd wires auth.OIDCProvider here when oidc.enabled=true
	// AND hands the same provider to the auth service via WithOIDC (the
	// Bearer arm the callback's token verification rides). Nil when OIDC is
	// disabled — both /api/v1/oidc routes then answer the E-26 404 so the
	// endpoint does not exist at all (FR-54-AC6/H29).
	OIDC OIDCLoginFlow
	// DataDir is storage.data_dir — the health probe writes there and the
	// stats endpoint sizes blobs/ under it.
	DataDir string
	// Console serves the console's three shapes: /binflow and /binflow/
	// (301 to the ui segment), /binflow/ui/** (SPA shell, history
	// fallback) and /binflow/assets/<hash> (immutable fingerprinted
	// output). T-89 delivers the handler; the router binds it to every
	// console segment (T-91).
	Console http.Handler
	// Docs serves the embedded help site on /binflow/docs/** (T-129,
	// ADR-0011/FR-41). Unlike the console seam (cmd wires it), a nil Docs
	// defaults to the embedded handler itself: the help center rides every
	// assembly — the embed placeholder is committed, the mount carries no
	// credential gate, and no assembly exists that wants a docs-less
	// binary. Tests may still inject a stub.
	Docs http.Handler
	// Adapters are the mounted protocol handlers (architecture section
	// 5.1: cmd passes adapter.All(); tests inject per-stack instances so
	// the process-wide registry never couples test cases together).
	Adapters []adapter.Handler
	// MgmtHandlers are the adapters' management-plane mounts (ADR-0034's
	// dispatchAPI family) keyed by protocol name — self-gated faces the
	// router only authenticates before delegating (the conan reindex
	// family, T-308: the handler carries its own CanManageRepo(write)
	// walk, the class check and both reindex spellings). An absent key
	// keeps those routes at the E-26 404 — unit stacks without the face.
	MgmtHandlers map[string]http.Handler
	// Metrics is the Prometheus registry behind root-level GET /metrics
	// (T-163, ADR-0022). cmd constructs exactly one per process; nil keeps
	// the endpoint at 503 and the metrics middleware out of the chain — the
	// pre-M6 posture for stacks that did not ask for instrumentation.
	Metrics *metrics.Registry
	// License is the entitlement manager behind GET/POST/DELETE
	// /api/system/license (M10 T-279, ADR-0032). Nil keeps GET at the
	// honest community-floor body (license.go's Docs-handler precedent)
	// and the two mutating verbs at 503 — pre-M10 unit stacks only; every
	// assembled server wires the real Manager.
	License LicenseManager
	// AuthConfigs is the authentication-configuration manager behind the
	// /api/v1/admin/security/{ldap,oauth,saml/config} plane (M11 T-305,
	// ADR-0035 / FR-92). Nil keeps all nine endpoints at the honest 503 —
	// unit stacks only; every assembled server wires the ConfigManager
	// (whose snapshot also feeds the auth service's hot arms).
	AuthConfigs AuthConfigPlane
	// Keypairs is the instance GPG keypair manager behind the
	// /api/security/keypair family, the repo association face and the
	// generation endpoint (M11 T-319, ADR-0038 / docs/design/
	// gpg-keypair.md). Nil keeps the plane at the honest 503 — unit stacks
	// only; every assembled server wires the keypair.Manager (the signing
	// seam the deb/rpm legs consume is assembled in cmd from the same
	// store + cipher).
	Keypairs KeypairPlane
	// Addons is the assembled addon registry (M10 T-282, ADR-0033): the
	// compile-time literal slice cmd builds. Nil keeps GET /api/v1/addons
	// at an honest empty array and the repo-create plane on repo.Service's
	// static enum (pre-M10 unit stacks); every assembled server wires the
	// full manifest, and httpapi.New then asserts every mounted adapter
	// protocol carries a package-type slot (the §15.2.4 missing-descriptor
	// panic).
	Addons   *addons.Registry
	Version  string
	Revision string
}

// Server is the assembled HTTP surface (architecture section 7).
type Server struct {
	deps     Deps
	log      *slog.Logger
	adapters map[string]adapter.Handler // package type -> handler
	// mgmt holds the adapters' management faces (Deps.MgmtHandlers keyed
	// by protocol); the conan reindex routes reach for "conan" here.
	mgmt map[string]http.Handler
	// sessions is the console-session facet of Deps.Auth (nil when the
	// injected authenticator is not a full auth.Service — unit-test fakes).
	// Discovered by assertion so cmd's Deps wiring stays untouched: the
	// session cookie VERIFIES through the shared Authenticator either way.
	sessions sessionRegistry
	// audit records login events on the /api/v1/session plane (best-effort
	// Recorder; the same contract repo.Service consumes).
	audit audit.Recorder
	// auditLog is the query facet of the same logger (GET /api/v1/audit,
	// T-93). nil on metadata-less unit stacks — the endpoint answers 503
	// rather than panicking there.
	auditLog audit.Logger
	// permView is the effective-permission facet of Deps.Authz (GET
	// /api/storage/**?permissions, T-97/SE-08); nil when the injected
	// authorizer is not the full auth.Service (unit fakes) — the endpoint
	// answers 503 there instead of panicking.
	permView permissionViewer
	// stepUp is the token-mint step-up facet of Deps.Auth (M7, T-219,
	// ADR-0027); nil when the injected authenticator is not the full
	// auth.Service (unit fakes) — an auth.token_step_up=on stack then fails
	// CLOSED at the mint gate instead of minting past the facet.
	stepUp stepUpRegistry
	// userDelete is the user-delete facet of Deps.Auth (M9, T-251/E4,
	// ADR-0030); nil when the injected authenticator is not the full
	// auth.Service (unit fakes) — DELETE /api/security/users/{name} then
	// answers 503 instead of panicking on a missing collaborator.
	userDelete userDeleter
	// metrics is the instrumentation built from Deps.Metrics (T-163): the
	// four family handles, the request-counting middleware and the scrape-
	// time snapshot sources. nil when Deps.Metrics is nil.
	metrics *instrumentation
	// addonsEval is the license facets the addon status view consumes (M10
	// T-282): the Manager's gate + state, discovered from Deps.License so
	// cmd's wiring stays untouched (the session/permView facet precedent).
	// nil — a bare LicenseManager fake, or no license collaborator at all —
	// evaluates the manifest on the community floor.
	addonsEval addons.Evaluator
	// uploads is the /api/v1/uploads session registry (M10 T-289): the
	// process-side id -> session table the six endpoints resolve through.
	// Always built (harmless on stacks without the seam — every endpoint
	// answers the 501 before it ever consults the registry).
	uploads *mpuRegistry
	srv     *http.Server
}

// New assembles the server. deps.Console may be nil (a bare console
// placeholder is installed) and log may be nil (slog.Default()).
func New(deps Deps, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if deps.Console == nil {
		deps.Console = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "console is not configured")
		})
	}
	// The docs default is the real embedded handler, not a placeholder 404:
	// the help center is part of the binary (ADR-0011 embed delivery), the
	// placeholder shell keeps the embed complete on node-less checkouts,
	// and the route is credential-free product self-description — see the
	// Deps.Docs field note.
	if deps.Docs == nil {
		deps.Docs = docs.Handler()
	}
	// Dispatch keys on the repository's package type ONLY (architecture
	// section 5.1, T-33/T-48 errata collected by T-63): a handler's key is
	// its Protocol() — the repositories.package_type value the router
	// dispatches on. The class keys ("local"/"remote"/"virtual") this map
	// used to carry were zero-read dead writes and the only collision
	// source: generic, docker, maven, npm and pypi all serve class=local
	// from M3, which a class key could not tell apart. Do not reintroduce
	// them. Later entries on the (unique) package-type key still REPLACE
	// earlier ones — a package type served twice is an assembly bug the
	// adapter registry's own Register panic catches first.
	adapters := make(map[string]adapter.Handler, len(deps.Adapters))
	for _, h := range deps.Adapters {
		adapters[h.Protocol()] = h
	}
	// The management mounts copy verbatim (nil-safe): an assembly without
	// a face simply leaves the map empty and the routes stay E-26.
	mgmt := make(map[string]http.Handler, len(deps.MgmtHandlers))
	for proto, h := range deps.MgmtHandlers {
		mgmt[proto] = h
	}
	// The five-core retro-fit's assembly guard (M10 T-282, §15.2.4): with a
	// registry mounted, every mounted adapter protocol MUST carry a
	// package-type slot — a mounted package type without a descriptor would
	// be the one capability outside the addon model, invisible to
	// /api/v1/addons and ungated by the T-283 weave. An assembly bug panics
	// here at startup, never at request time (adapter.Register's posture).
	if deps.Addons != nil {
		for _, h := range deps.Adapters {
			if _, ok := deps.Addons.ForPackageType(h.Protocol()); !ok {
				panic(fmt.Sprintf(
					"httpapi: adapter protocol %q has no addon slot in the assembled registry — every mounted package type must carry a descriptor (ADR-0033 five-core retro-fit)",
					h.Protocol()))
			}
		}
	}
	s := &Server{deps: deps, log: log, adapters: adapters, mgmt: mgmt, uploads: newMPURegistry()}
	// Instrumentation (T-163) attaches before route() runs below — the
	// mounted /metrics handler and the base chain both read s.metrics.
	if deps.Metrics != nil {
		s.metrics = newInstrumentation(deps)
	}
	// Console-session facet discovery (T-91): the real auth.Service carries
	// it; an injected fake stays session-less and the login endpoints
	// answer 503 rather than crashing on a missing collaborator.
	if sr, ok := deps.Auth.(sessionRegistry); ok {
		s.sessions = sr
	}
	// Effective-permission facet discovery (T-97): the real auth.Service
	// computes the ?permissions view; a bare Authorizer fake stays
	// view-less and the endpoint answers 503 instead of panicking.
	if pv, ok := deps.Authz.(permissionViewer); ok {
		s.permView = pv
	}
	// Step-up facet discovery (M7, T-219): the real auth.Service carries
	// the mint-grant ledger and the password-leg verifier; a bare
	// Authenticator fake stays facet-less and the step-up gate fails closed.
	if su, ok := deps.Auth.(stepUpRegistry); ok {
		s.stepUp = su
	}
	// User-delete facet discovery (M9, T-251/E4): the real auth.Service
	// carries the guard chain and cascade; a bare Authenticator fake stays
	// facet-less and the endpoint answers 503 instead of panicking.
	if ud, ok := deps.Auth.(userDeleter); ok {
		s.userDelete = ud
	}
	// Addon-evaluation facet discovery (M10, T-282): the real Manager
	// carries the gate and state the addon status view joins; a bare
	// LicenseManager fake stays facet-less and the view evaluates the
	// community floor.
	if ev, ok := deps.License.(addons.Evaluator); ok {
		s.addonsEval = ev
	}
	// The audit recorder for the login plane: same store, same enabled
	// toggle and the same redaction chain every other audited surface uses.
	// Assemblies without a metadata store (unit-test stacks) get the no-op
	// fallback — a nil Recorder would panic on the first login event. The
	// query facet (T-93) shares the same logger so the read plane sees
	// exactly what the write plane stored.
	if deps.Metadata != nil {
		lg := audit.New(deps.Metadata, deps.Config.Audit.Enabled)
		s.audit = audit.BestEffort(lg)
		s.auditLog = lg
	} else {
		s.audit = noopRecorder{}
	}

	s.srv = &http.Server{
		Handler:           s.route(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// route is the top-level request switch. net/http's ServeMux is used ONLY
// for the two probe patterns (exact matches that can never contain a dot
// segment): every other path must bypass mux cleaning entirely, because
// ServeMux redirects any un-normalized path — even through a lone
// catch-all pattern — and that would rewrite /binflow/r/a/../b into
// /binflow/r/b ahead of the adapter's 400 defense (FR-4-AC10; probe
// evidence in router.go's package comment).
func (s *Server) route() http.Handler {
	probes := http.NewServeMux()
	probes.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		s.probeHandler(false).ServeHTTP(w, r)
	})
	probes.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		s.probeHandler(true).ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/healthz", "/readyz":
			probes.ServeHTTP(w, r)
		case "/metrics":
			// Root-level Prometheus scrape endpoint (T-163, ADR-0022 /
			// PRD FR-61): same probe-exemption family as /healthz, with its
			// own base chain (plus the auth gate when metrics.require_auth
			// is on).
			s.metricsHandler().ServeHTTP(w, r)
		default:
			s.rootHandler().ServeHTTP(w, r)
		}
	})
}

// Handler exposes the assembled handler (tests drive it with httptest;
// Run drives it with a listener).
func (s *Server) Handler() http.Handler { return s.srv.Handler }

// Run listens, serves and blocks until the listener fails or the context
// is canceled. SIGINT/SIGTERM handling belongs to cmd (T-16): it calls
// Shutdown with the config's graceful timeout, then closes storage and
// metadata in that order (architecture section 7.4).
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.deps.Config.Server.Listen)
	if err != nil {
		return fmt.Errorf("httpapi: listen %s: %w", s.deps.Config.Server.Listen, err)
	}
	s.log.Info("binflow listening", "addr", ln.Addr().String(), "version", s.versionOrDefault())

	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("httpapi: serve: %w", err)
	case <-ctx.Done():
		return s.Shutdown(s.deps.Config.Server.GracefulTimeout)
	}
}

// Shutdown drains in-flight requests for at most timeout, then returns.
// Callers follow with storage.Close() then metadata.Close() (section 7.4);
// this method touches neither, so the ordering stays in one place (cmd).
func (s *Server) Shutdown(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = config.DefaultGracefulTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("httpapi: shutdown within %s: %w", timeout, err)
	}
	return nil
}

func (s *Server) versionOrDefault() string {
	if s.deps.Version != "" {
		return s.deps.Version
	}
	return DefaultVersion
}
