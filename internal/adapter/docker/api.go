// Package docker is the Docker Registry HTTP API V2 adapter (M2), mounted
// at the root-level /v2 exception route (ADR-0010): unlike every other
// product endpoint it does NOT live under the /binflow prefix, because the
// docker client family hardcodes /v2/... and cannot be configured with a
// sub-path prefix.
package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Compile-time wiring checks.
var _ RepoLookup = storeRepoLookup{}

// Protocol is the package-type identifier this adapter serves.
const Protocol = "docker"

// ServiceID is the token-flow service name announced in the Bearer
// challenge (ADR-0010 clause 4: service="binflow").
const ServiceID = "binflow"

// TokenPath is the adapter's own token endpoint (ADR-0010 clause 4), the
// target of the challenge's realm. T-33 mounted the route seam; T-37
// implemented the issuing logic.
const TokenPath = "/v2/token"

// catalogPath is the registry-level catalog route (architecture section
// 5.3, GET /v2/_catalog). T-40 implements it; the placeholder branch here
// exists so the "_-"prefixed registry endpoint can never be captured by a
// repository key (spec reserves that prefix).
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
	PackageType() string
}

// RepoLookup resolves a repository key to its row. Only PackageType is
// consulted — the same routing-data-only posture as httpapi's seam (the
// row's config never crosses here). The docker route reaches the adapter
// WITHOUT the /binflow dispatch that normally performs this lookup, so the
// adapter holds its own. ctx flows from the request so a slow query dies
// with the connection (T-33 review B2).
type RepoLookup interface {
	Get(ctx context.Context, key string) (RepoRow, error)
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
