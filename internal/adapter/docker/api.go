// Package docker is the Docker Registry HTTP API V2 adapter (M2), mounted
// at the root-level /v2 exception route (ADR-0010): unlike every other
// product endpoint it does NOT live under the /binflow prefix, because the
// docker client family hardcodes /v2/... and cannot be configured with a
// sub-path prefix.
package docker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Compile-time wiring checks.
var _ RepoLookup = storeRepoLookup{}

// Protocol is the package-type identifier this adapter serves.
const Protocol = "docker"

// servesV2Plane reports whether one repository package type belongs to the
// registry-v2 family this plane serves (HL-3, helm.md section 8.2):
// "docker" itself plus "helmoci" — the HelmOCI package type rides the SAME
// /v2 wire surface through this handler (Artifactory's isDockerGroup =
// {Docker, OCI, HelmOCI} is the reference posture: one docker v2 stack
// serves the whole family, differentiated only by which repository rows
// route to it). The helmoci package type's own adapter
// (internal/adapter/helmoci) is the registration and content-plane shell;
// the protocol face lives here, so the family set is a docker-plane fact,
// not an assembly knob.
func servesV2Plane(packageType string) bool {
	return packageType == Protocol || packageType == repo.PackageHelmOCI
}

// ServiceID is the token-flow service name announced in the Bearer
// challenge (ADR-0010 clause 4: service="binflow").
const ServiceID = "binflow"

// TokenPath is the adapter's own token endpoint (ADR-0010 clause 4), the
// target of the challenge's realm. T-33 mounted the route seam; T-37
// implemented the issuing logic.
const TokenPath = "/v2/token"

// catalogPath is the registry-level catalog route (architecture section
// 5.3, GET /v2/_catalog). T-40 implements it; the route branch keeps the
// "_-"prefixed registry endpoint out of the name parser's repo-key slot
// (spec reserves that prefix — a repository named "_catalog" can never
// hijack it).
const catalogPath = "/v2/_catalog"

// Options carries the deployment facts the docker plane needs at assembly.
type Options struct {
	// AnonymousAccess is security.anonymous_access (ADR-0009): the ping
	// endpoint's 200-vs-401 switch for M2 (PRD Q2 ruling: the global key
	// governs docker reads too; a per-repo knob is M4).
	AnonymousAccess bool
	// BaseURL is server.base_url — the externally visible origin the
	// challenge realm is built from when set; empty means derive from the
	// request (X-Forwarded-* honored, ADR-0010 clause 4).
	BaseURL string
	// TokenTTL is auth.token_default_ttl — the docker token lifetime
	// (Q3 interim ruling: default 720h, no refresh). Non-positive values
	// are floored to 1h by the handler: this endpoint's posture is
	// "limited TTL" (AC1), so a misconfigured zero must not mint
	// non-expiring tokens.
	TokenTTL time.Duration
}

// Handler is the docker v2 protocol adapter (architecture section 5.3).
type Handler struct {
	svc    repo.Service
	repos  RepoLookup
	authz  auth.Authorizer
	tokens auth.TokenRegistry
	users  anonymousSubjectSeed
	opts   Options
	log    *slog.Logger
	sess   *sessionRegistry
	// verifier is the gated password-verification seam the form-credential
	// exchange runs through (T-204 / T-192 leftover 2). New recovers it
	// from the authz collaborator — every production assembly passes the
	// *auth.Service there, so the token endpoint shares the service's one
	// argon2 gate with the Basic arm. nil (bare test assemblies with fake
	// authorizers) fails the form-credential path closed; it never falls
	// back to the ungated pure auth.VerifyPassword.
	verifier adapter.PasswordVerifier
	// store drives the blob-upload sessions (T-38). The architecture's
	// "adapters talk to repo.Service only" rule bends exactly once here —
	// the section 5.3 session-docking ruling: the upload endpoints own the
	// protocol state (received offset, UUID pairing) around
	// storage.Session's Append/Commit/Abort; repo.Service's Put consumes a
	// whole body in one call and cannot express chunked offset alignment.
	// The seam is an interface so tests stub it without an engine.
	store storage.Engine
	// ledger is the read-only blob digest ledger (sha1/md5 of a landed
	// blob; ADR-0006 keeps no sidecar files). Same consumer-side convention
	// as generic.BlobLedger.
	ledger BlobLedger
	// anonSeedMu/anonSeeded dedupe the anonymous-token subject seeding PER
	// HANDLER (= per assembled store). The state was process-wide until
	// T-54: a `go test -count>1` iteration rebuilds the stack on a fresh
	// database while the process-wide cache kept answering "seeded" from
	// the first iteration's now-discarded store — every later harness's
	// anonymous token issuance 500'd on the missing subject row. Per-handler
	// restores the production invariant (one handler, one store, one seed)
	// without cross-assembly leakage. Failures are NOT cached: a transient
	// store error self-heals on the next request.
	anonSeedMu sync.Mutex
	anonSeeded bool
	// remotes is the REMOTE repositories' upstream session pool (T-363):
	// per-repository guarded clients plus the Bearer token cache. The
	// zero value is ready (the map grows on first use).
	remotes remoteSessions
}

// BlobLedger is the digest-lookup seam for download headers and mount
// responses (satisfied by metadata.Store.Blobs()).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// RepoRow is the consumer-side shape of the repository row the docker
// plane reads. An interface, not metadata.Repo, so tests stub the lookup
// without opening a store.
type RepoRow interface {
	// Key is the repository key — the catalog's repository enumeration
	// (T-40) needs it alongside the routing data Get already carried.
	Key() string
	PackageType() string
	// Class is the repository class ("local"/"remote"/"virtual"). Since
	// T-363 the plane branches on it BEFORE the content handlers: a REMOTE
	// row takes the pull-through read plane and the RE-05 write refusal.
	// The static lookup's rows carry "" (class-less), which the branch
	// treats as not-remote — the pre-T-363 behavior.
	Class() string
}

// RepoLookup resolves repository rows for the docker plane. Only routing
// data is consulted — the same posture as httpapi's seam (the row's config
// never crosses here). The docker route reaches the adapter WITHOUT the
// /binflow dispatch that normally performs this lookup, so the adapter
// holds its own. ctx flows from the request so a slow query dies with the
// connection (T-33 review B2). List (T-40) enumerates every repository row
// for the catalog — the caller filters by package type and visibility; the
// rows arrive ordered by key.
type RepoLookup interface {
	Get(ctx context.Context, key string) (RepoRow, error)
	List(ctx context.Context) ([]RepoRow, error)
}

// New wires the handler. svc may be nil in the foundation state (content
// endpoints arrive with T-38/T-39); repos must be non-nil so name
// resolution answers today. authz/tokens drive the token flow (T-37): they
// may be nil in tests that do not exercise /v2/token — a nil authz only
// empties the response's narrowed scope field, and a nil tokens makes the
// token endpoint answer 503 rather than panic. users seeds the synthetic
// anonymous-token subject (nil only in tests; the anonymous token path then
// 500s honestly). log may be nil (slog.Default()).
//
// store (blob-upload sessions, T-38) and ledger (blob digest rows) may be
// nil: without store the upload endpoints answer the store-failure 500/503
// (no test assembly needs a fake there that does not wire one), without
// ledger the checksum headers degrade to sha256-only.
func New(svc repo.Service, repos RepoLookup, authz auth.Authorizer, tokens auth.TokenRegistry,
	users metadata.UserStore, opts Options, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		svc:    svc,
		repos:  repos,
		authz:  authz,
		tokens: tokens,
		opts:   opts,
		log:    log,
		sess:   newSessionRegistry(),
	}
	// Recover the gated verification capability from the identity service
	// the assembler already wired (cmd and the httpapi harness pass the
	// *auth.Service as authz; the same capability-probe precedent as
	// AuthenticateCredentials' LDAP Bind). A fake authorizer leaves the
	// verifier nil and the form-credential exchange fails closed.
	if pv, ok := authz.(adapter.PasswordVerifier); ok {
		h.verifier = pv
	}
	if users != nil {
		h.users = userSeed{store: users}
	}
	return h
}

// WithStorage attaches the blob-upload session engine and the digest ledger
// (T-38). A separate setter keeps the T-37 constructor signature stable —
// the assembly sites that predate the blob domain keep compiling, and the
// two production wiring points (cmd/main, httpapi harness) call this once.
func (h *Handler) WithStorage(store storage.Engine, ledger BlobLedger) *Handler {
	h.store = store
	h.ledger = ledger
	return h
}

// userSeed adapts metadata.UserStore onto the anonymous-subject seeding
// seam (consumer-side interface, same convention as RepoLookup).
type userSeed struct{ store metadata.UserStore }

func (s userSeed) Get(ctx context.Context, username string) (*metadata.User, error) {
	return s.store.Get(ctx, username)
}

func (s userSeed) Create(ctx context.Context, u *metadata.User) error {
	return s.store.Create(ctx, u)
}

// Principal is the caller identity, aliased like every adapter does.
type Principal = auth.Principal

// Role is the closed-set role of a principal (M7, ADR-0026), aliased beside
// Principal so the token endpoint's form leg can carry it without importing
// the auth package at every construction site.
type Role = auth.Role
