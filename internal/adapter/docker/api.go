// Package docker is the Docker Registry HTTP API V2 adapter (M2), mounted
// at the root-level /v2 exception route (ADR-0010): unlike every other
// product endpoint it does NOT live under the /binflow prefix, because the
// docker client family hardcodes /v2/... and cannot be configured with a
// sub-path prefix.
package docker

import (
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Compile-time wiring checks.
var _ RepoLookup = storeRepoLookup{}

// Protocol is the package-type identifier this adapter serves.
const Protocol = "docker"

// ServiceID is the token-flow service name announced in the Bearer
// challenge (ADR-0010 clause 4: service="binflow").
const ServiceID = "binflow"

// TokenPath is the adapter's own token endpoint (ADR-0010 clause 4), the
// target of the challenge's realm. T-33 mounts only the route seam; the
// issuing logic itself is T-37.
const TokenPath = "/v2/token"

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
}

// Handler is the docker v2 protocol adapter (architecture section 5.3).
type Handler struct {
	svc   repo.Service
	repos RepoLookup
	opts  Options
	sess  *sessionRegistry
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
// adapter holds its own.
type RepoLookup interface {
	Get(key string) (RepoRow, error)
}

// New wires the handler. svc may be nil in the foundation state (content
// endpoints arrive with T-38/T-39); repos must be non-nil so name
// resolution answers today.
func New(svc repo.Service, repos RepoLookup, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, opts: opts, sess: newSessionRegistry()}
}

// Principal is the caller identity, aliased like every adapter does.
type Principal = auth.Principal
