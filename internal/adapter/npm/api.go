package npm

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Protocol is the package-type identifier this adapter serves; it is both the
// registry key and the repositories.package_type value httpapi dispatches on.
const Protocol = "npm"

// contentTypeSLIM is the npm "corgi" packument variant (npm ci optimization):
// a document stripped of fields no installer needs. The Accept header
// negotiates it and the response echoes the exact type verbatim
// (spec section 2.2, high confidence).
const contentTypeSLIM = "application/vnd.npm.install-v1+json"

// Options carries the deployment facts the npm plane needs at assembly.
type Options struct {
	// BaseURL is the externally visible origin (server.base_url). Empty means
	// derive per request from r.Host (+ X-Forwarded-Proto), the same posture
	// as the docker challenge realm. Every rewritten dist.tarball URL is
	// built from it.
	BaseURL string
}

// Handler is the npm registry protocol adapter (architecture section 5.4.2).
type Handler struct {
	svc   repo.Service
	repos repo.ClassReader
	opts  Options

	// authz answers the publish chain's step-3 permission question
	// (spec section 2.3: write on the tarball path -> 403 "Cannot deploy
	// to '<tarballPath>'") BEFORE any storage write; repo.Service re-checks
	// the same grant inside Put (defense in depth). nil skips the early
	// check and relies on the service's own gate.
	authz auth.Authorizer
	// tokens mints the login token (NE-06, TokenRegistry reuse — the /v2/token
	// dual-entry precedent). nil makes login answer 503, never panic.
	tokens auth.TokenRegistry
	// users verifies the login body credential (the couch login PUT carries
	// name/password in JSON when the client holds no Basic header). nil
	// disables that verification path.
	users UserDirectory
	// ledger is the read-only blob digest ledger: packument ETag/X-Checksum-Sha1
	// come from the stored document's ledger row (spec section 2.4). nil
	// degrades to computing the sha1 of the served bytes.
	ledger BlobLedger
	// clock stamps packument time fields (RFC3339 UTC); overridable in tests.
	clock nowClock
	// docMu serializes the packument's read-modify-write transitions
	// (publish merge, dist-tag moves, unpublish, deprecate). Node writes
	// themselves are store-atomic; the DOCUMENT is one JSON blob whose
	// merges must not interleave (two concurrent publishes of different
	// versions would otherwise lose one version). Process-local by design —
	// single-instance M3 (the sqlite metadata layer serializes writers the
	// same way).
	docMu sync.Mutex
}

// BlobLedger is the digest-lookup seam (same consumer-side convention as
// generic.BlobLedger / docker.BlobLedger; satisfied by metadata.Store.Blobs()).
type BlobLedger interface {
	Get(ctx context.Context, sha256 string) (*metadata.Blob, error)
}

// UserDirectory is the login-flow user lookup (satisfied structurally by
// metadata.UserStore; kept as a consumer-side interface so tests stub it
// without a store).
type UserDirectory interface {
	Get(ctx context.Context, username string) (*metadata.User, error)
}

// New wires the handler. svc is required; repos powers the N4 strict-404
// guard (a request reaching this handler for a repository whose package type
// is not npm answers 404 — defense in depth; httpapi's dispatch already
// guarantees it for mounted assemblies). Optional collaborators attach via
// WithAuth/WithLedger.
func New(svc repo.Service, repos repo.ClassReader, opts Options) *Handler {
	return &Handler{svc: svc, repos: repos, opts: opts, clock: clockUTC}
}

// WithAuth attaches the login/authorization collaborators (NE-06): the token
// registry that mints npm login tokens, the user directory that verifies the
// couch login body credential, and the optional early-write authorizer.
func (h *Handler) WithAuth(tokens auth.TokenRegistry, users UserDirectory, authz auth.Authorizer) *Handler {
	h.tokens, h.users, h.authz = tokens, users, authz
	return h
}

// WithLedger attaches the blob digest ledger (packument ETag / tarball sha1
// headers). A separate setter keeps New's signature stable for the assembly
// sites that predate it (the docker WithStorage precedent).
func (h *Handler) WithLedger(ledger BlobLedger) *Handler {
	h.ledger = ledger
	return h
}

// Register registers the handler AND its metadata provider under the single
// Protocol literal (T-63 review N2: one exported registration point keeps
// handler dispatch and metadata classification from ever drifting apart).
// cmd assembly calls it exactly once; a duplicate panics at startup.
func Register(h *Handler) {
	adapter.Register(h)
	adapter.RegisterMetadata(provider{})
}

// Protocol implements adapter.Handler.
func (h *Handler) Protocol() string { return Protocol }

// RepoTypes implements adapter.Handler: npm serves local repositories in M3
// (remote proxies and virtual aggregation route through the service-layer
// engines, invisible to this adapter; architecture section 5.4 preamble).
func (h *Handler) RepoTypes() []string { return []string{repo.TypeLocal} }

// Layout implements adapter.Handler. It differs from the generic layout in
// exactly one rule: the DOMAIN ROOT (empty relPath — "GET /api/npm/<repo>/"
// is a documented connectivity probe, spec section 0) is a legal npm address
// and must not be a 400. Everything else — percent-decoding at the entry,
// dot-segment rejection on the DECODED form, control bytes, empty segments,
// reserved repo keys, path budget — reuses the shared defense via
// adapter.NormalizeRelPath (FR-4-AC10/AC11, NFR-S18).
func (h *Handler) Layout(r *http.Request) (string, string, error) {
	if r == nil || r.URL == nil {
		return "", "", fmt.Errorf("%w: empty request URL", adapter.ErrBadRequestPath)
	}
	raw := r.URL.EscapedPath()
	if raw == "" {
		raw = r.URL.Path
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("%w: malformed percent-encoding in %q: %w", adapter.ErrBadRequestPath, raw, err)
	}
	decoded = strings.TrimPrefix(decoded, "/")
	if decoded == "" {
		return "", "", fmt.Errorf("%w: path is empty (no repository key)", adapter.ErrBadRequestPath)
	}
	key, rest, _ := strings.Cut(decoded, "/")
	if key == "" {
		return "", "", fmt.Errorf("%w: empty repository key", adapter.ErrBadRequestPath)
	}
	if len(key) > adapter.MaxRepoKeyLen {
		return "", "", fmt.Errorf("%w: repository key longer than %d characters", adapter.ErrBadRequestPath, adapter.MaxRepoKeyLen)
	}
	if adapter.IsReservedSegment(key) {
		return "", "", fmt.Errorf("%w: %q is a reserved routing segment", adapter.ErrBadRequestPath, key)
	}
	if rest != "" {
		if err := adapter.NormalizeRelPath(rest); err != nil {
			return "", "", err
		}
	}
	return key, rest, nil
}

// ServeHTTP routes one npm protocol request. The N4 guard (T-63 review):
// only npm repositories answer here — anything else is a strict 404, never a
// best-effort content-plane guess. Unrecognized npm paths (search, audits,
// random content paths) are 404 with the E-26 not-implemented wording
// (NE-08, M58).
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repoKey, relPath, err := h.Layout(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()

	if h.repos != nil {
		row, err := h.repos.Get(ctx, repoKey)
		if err != nil {
			repoRowError(w, repoKey, err)
			return
		}
		if row.PackageType != Protocol {
			// N4 strict rejection: the mount spelling was erased by the
			// rewrite seam, but the ROW decides — and a non-npm row must
			// never be served npm semantics (Artifactory answers 404 here).
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("repository '%s' is not an npm repository", repoKey))
			return
		}
	}

	rt, ok := parseRoute(relPath)
	if !ok {
		writeNotImplemented(w, relPath)
		return
	}
	p := adapter.PrincipalFrom(ctx)

	switch rt.kind {
	case routeRoot:
		h.serveRoot(w, r)
	case routePing:
		h.servePing(w, r)
	case routeWhoami:
		h.serveWhoami(w, r, p)
	case routeUserLogin:
		h.serveLogin(ctx, w, r, p, rt.userID)
	case routeDistTags:
		h.serveDistTags(ctx, w, r, p, repoKey, rt.name)
	case routeDistTag:
		h.serveDistTag(ctx, w, r, p, repoKey, rt.name, rt.tag)
	case routePackument:
		h.servePackumentRoute(ctx, w, r, p, repoKey, rt.name)
	case routeTagOrVersion:
		h.serveTagOrVersion(ctx, w, r, p, repoKey, rt.name, rt.tail)
	case routePackageRev:
		h.servePackageRev(ctx, w, r, p, repoKey, rt.name, rt.rev)
	case routeTarball:
		h.serveTarball(ctx, w, r, p, repoKey, rt.name, rt.file)
	case routeTarballRev:
		h.serveTarballRev(ctx, w, r, p, repoKey, rt.name, rt.file)
	default:
		writeNotImplemented(w, relPath)
	}
}

// Principal is the caller identity, aliased like every adapter does.
type Principal = auth.Principal
