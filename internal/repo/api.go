package repo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
)

// Sentinel errors. Errors returned by Service wrap one of these; callers match
// with errors.Is and map to HTTP statuses at the httpapi layer.
var (
	// ErrRepoNotFound: the repository key does not exist (404).
	ErrRepoNotFound = errors.New("repository not found")
	// ErrRepoExists: a repository with the same key already exists (400/409).
	ErrRepoExists = errors.New("repository already exists")
	// ErrInvalidRepoKey: the key does not match [a-z][a-z0-9-]{1,62}
	// (PRD FR-3-AC4).
	ErrInvalidRepoKey = errors.New("invalid repository key")
	// ErrReservedRepoKey: the key collides with a routing segment reserved by
	// ADR-0008 ("api", "v2").
	ErrReservedRepoKey = errors.New("repository key is reserved")
	// ErrInvalidRepoType: unknown rclass, or an attempt to change it.
	ErrInvalidRepoType = errors.New("invalid repository type")
	// ErrRepoTypeNotSupported: a (rclass, packageType) pair the M3 matrix
	// does not serve — docker on remote/virtual repositories (FR-15-AC7) —
	// or a content operation on a class whose engine has not landed yet.
	// httpapi translates this to a 400-shaped response.
	ErrRepoTypeNotSupported = errors.New("repository type not supported")
	// ErrInvalidRepoConfig: the config blob is not valid JSON.
	ErrInvalidRepoConfig = errors.New("invalid repository config")
	// ErrRepoNotEmpty: DeleteRepo on a non-empty repository without
	// deleteContent (message names the deleteContent flag, FR-3-AC5).
	ErrRepoNotEmpty = errors.New("repository is not empty")
	// ErrNodeNotFound: no node at the path (idempotent 404 semantics,
	// repo-semantics section 4).
	ErrNodeNotFound = errors.New("node not found")
	// ErrIsFolder: Get addressed a folder node (trailing-slash path). The
	// node metadata is still returned; adapters use this to render
	// FolderInfo instead of streaming a body.
	ErrIsFolder = errors.New("node is a folder")
	// ErrOrphanBlob: PutFromBlob was handed a blob that exists physically
	// but has no blobs-ledger row (crash-window residue awaiting GC).
	// Materializing a node from it would freeze an incomplete digest record
	// (the ledger row is the sha1/md5 source of truth and is never
	// back-filled), so the operation refuses; the caller maps this to a
	// plain "content not found" (404), not a 500.
	ErrOrphanBlob = errors.New("blob exists in the filestore but not in the ledger")
	// ErrInvalidPath: malformed artifact path (empty segment, dot segment,
	// absolute path, too long, ...).
	ErrInvalidPath = errors.New("invalid artifact path")
	// ErrUnauthorized: anonymous principal where authentication is required
	// (401 challenge).
	ErrUnauthorized = errors.New("authentication required")
	// ErrForbidden: authenticated principal without the required grant (403).
	ErrForbidden = errors.New("permission denied")
	// ErrManifestNotFound: no docker_manifests row at (repo, image, digest);
	// the adapter maps this to the spec body's MANIFEST_UNKNOWN (404).
	ErrManifestNotFound = errors.New("docker manifest not found")
	// ErrTagNotFound: no docker_tags row at (repo, image, tag); the adapter
	// maps this to MANIFEST_UNKNOWN by-ref (404) — the spec has no dedicated
	// TAG_UNKNOWN code.
	ErrTagNotFound = errors.New("docker tag not found")
	// ErrInvalidDigest: a docker digest that is not the M2 shape —
	// "sha256:" followed by exactly 64 lowercase hex characters (docker
	// digest = sha256 of the manifest body, the only algorithm M2 serves;
	// adapters strip the algorithm prefix before calling in).
	ErrInvalidDigest = errors.New("invalid docker digest")
	// ErrInvalidTag: a tag outside the spec charset
	// [a-zA-Z0-9_][a-zA-Z0-9._-]{0,127} (the DDL comment's rule, applied at
	// this layer because docker_tags has no DB-level constraint for it).
	ErrInvalidTag = errors.New("invalid docker tag")
	// ErrInvalidImage: an image relative name that is empty, has empty /
	// dot segments, or exceeds the node-path budget — the name the adapter
	// extracted after the repo key must be a legal node path prefix.
	ErrInvalidImage = errors.New("invalid docker image name")
	// ErrImageNotFound: ListTags/ListImages addressed an image with no
	// manifest rows (docker-registry.md section 6: tags/list on an unknown
	// image is NAME_UNKNOWN, not an empty list).
	ErrImageNotFound = errors.New("docker image not found")
	// ErrInvalidManifest: a manifest parameter that is not a name/digest/tag
	// shape problem but a bad manifest descriptor — an empty media type, a
	// negative size, a nil ref row. The adapter maps this to the spec body's
	// MANIFEST_INVALID (400); keeping it apart from ErrInvalidImage/ErrTag
	// lets the /v2 plane pick the right error code instead of guessing.
	ErrInvalidManifest = errors.New("invalid docker manifest parameter")
	// ErrInvalidCursor: a pagination cursor (last) that cannot address this
	// page's key space — a malformed tag on tags/list, or a "<repoKey>/..."
	// name from another repository on the catalog. The adapter maps this to
	// a 400 (the official family PAGINATION_NUMBER_INVALID lives in).
	ErrInvalidCursor = errors.New("invalid pagination cursor")
)

// Repository types and package types (architecture section 6 DDL).
const (
	TypeLocal   = "local"
	TypeRemote  = "remote"
	TypeVirtual = "virtual"

	PackageGeneric = "generic"
	// PackageDocker is local-only across M2/M3 (FR-7-AC1, FR-15-AC7): the
	// registry proxy and aggregation semantics of remote/v2 are unverified
	// spec ground (docker-registry.md section 9, PRD Q4) — M4 re-evaluates.
	PackageDocker = "docker"
	// PackageMaven/PackageNpm/PackagePypi open for all three classes in M3
	// (FR-15-AC1): local is served by the protocol adapters (T-67/T-69/T-70),
	// remote by the pull-through fetcher (T-66), virtual by the two-bucket
	// resolver (T-71).
	PackageMaven = "maven"
	PackageNpm   = "npm"
	PackagePypi  = "pypi"
)

// Reserved repo keys (ADR-0008): they collide with /binflow routing segments.
var reservedRepoKeys = map[string]bool{
	"api": true,
	"v2":  true,
}

// Authorization actions (architecture section 3.4). Aliased from auth so
// repo and auth can never drift apart on the action vocabulary.
const (
	ActionRead   = auth.ActionRead
	ActionWrite  = auth.ActionWrite
	ActionDelete = auth.ActionDelete
)

// Audit actions emitted by this package (architecture section 3.5),
// aliased from the audit package's vocabulary.
const (
	AuditActionDeploy     = audit.ActionDeploy
	AuditActionDownload   = audit.ActionDownload
	AuditActionDelete     = audit.ActionDelete
	AuditActionRepoCreate = audit.ActionRepoCreate
	AuditActionRepoUpdate = audit.ActionRepoUpdate
	AuditActionRepoDelete = audit.ActionRepoDelete
)

// Principal is the caller identity (architecture section 3.4). It is the
// auth package's own type, aliased here so both sides of the boundary share
// one declaration while the section 3.3 signatures stay readable. nil means
// anonymous (ADR-0009).
type Principal = auth.Principal

// Authorizer is the consumer-side ACL seam (architecture section 3.4). The
// auth package's implementation satisfies it structurally. nil inside New
// means "fail closed": only admin principals pass — the correct posture when
// no authorizer is wired.
type Authorizer interface {
	// Can reports whether p may perform action (r|w|d) on repoKey/path.
	// Anonymous policy (anonymous_access, ADR-0009) is the implementation's
	// decision, not this package's.
	Can(ctx context.Context, p *Principal, repoKey, path, action string) bool
}

// AuditEvent is one best-effort audit record (architecture section 3.5),
// aliased to the audit package's canonical type. Time and Actor are stamped
// by the injected logger when empty; this package fills Actor itself.
type AuditEvent = audit.Event

// PutManifestResult reports what a manifest publish did. TagRepointed is the
// digest the tag pointed at immediately BEFORE this call, and non-empty only
// when the tag actually moved (a fresh tag, or a same-digest republish that
// left it in place, reports ""). The adapter answers 201 either way
// (docker-registry.md section 10: the official protocol has no conflict
// semantics on tag repointing, Q4 ruling keeps overwriting legal).
type PutManifestResult struct {
	Manifest *metadata.DockerManifest
	// Node is the manifest blob's node at the docker layout path.
	Node *metadata.Node
	// TagRepointed is the tag's previous digest when this publish moved the
	// tag to a different digest; "" when the tag is new or already pointed
	// here.
	TagRepointed string
}

// AuditLogger is the consumer-side audit seam, satisfied structurally by
// audit.Logger. Append failures never block business operations (technical
// debt ledger #4); this package swallows and logs them so the guarantee
// holds regardless of the injected implementation.
type AuditLogger interface {
	Append(ctx context.Context, e AuditEvent) error
}

// Service orchestrates the repository use cases (architecture section 3.3):
// artifact Get/Put/Delete/List for local repositories plus repository CRUD.
// M3 dispatches per repository class: remote reads pull through the proxy
// engine (T-66), virtual reads resolve over the member two-bucket order and
// virtual writes route onto the configured local deployment member or
// answer the C5 405 (T-71); aggregate virtual LIST is deferred (P2).
//
// The interface is organized in two contract segments (architecture section
// 5.4, M3/T-63): the public use-case face (content + repository management)
// and, at the tail, the adapter SPI face protocol adapters extend. See the
// segment banners inside.
//
// Put's transaction boundary is the correctness core: storage Commit puts the
// physical blob in place first, then the metadata writes land blob-first
// (blobs row, then node row). A crash in between leaves an unreferenced blob
// for GC's grace period — never metadata pointing at a missing blob
// (architecture sections 3.2/3.3; the nodes.sha256 FK enforces the order).
type Service interface {
	// ---- Public use-case face (architecture section 3.3) ----
	//
	// The product-stable use cases: the content plane and repository
	// management. Every upper layer may program against this segment — the
	// REST planes (httpapi /api/storage and /api/repositories), the console,
	// a future CLI — and so may protocol adapters. The segment split is a
	// CONTRACT marker (oss-structure section 6 insight 2: OSS papi/capi
	// separation keeps addon code from reaching around the stable face),
	// not two Go types: adapters additionally consume the SPI segment below.

	// Get opens the node's blob for reading (caller closes) and returns its
	// metadata. Missing node → ErrNodeNotFound; missing repo → ErrRepoNotFound.
	//
	// REMOTE repositories (M3, T-66, architecture section 5.4): the read is
	// authorized first, then dispatched to the pull-through engine — the
	// RE-04 six-step flow (negative cache, TTL-classed copy, guarded upstream
	// fetch, stale-while-error downgrade). Two contract points for callers:
	//   - failures may be a *StatusError carrying the exact client-facing
	//     status, message and headers (RE-04's 404/502/400 wordings, RE-05's
	//     405+Allow); the unfound family still wraps ErrNodeNotFound, so
	//     pre-M3 mappings keep answering 404;
	//   - the returned reader may implement ExtraHeaders() http.Header — the
	//     engine's response hints (X-BinFlow-Cache, X-Binflow-Upstream-Error)
	//     which serving layers merge onto the response structurally, without
	//     learning the repository class.
	Get(ctx context.Context, p *Principal, repoKey, path string) (io.ReadSeekCloser, *metadata.Node, error)
	// Put streams body into a storage session, commits it against the
	// client-declared digests in expect and persists the node. Overwrite
	// semantics follow repo-semantics section 3: same declared sha256 on an
	// existing node is an idempotent retransmit (no delete-permission check,
	// created/createdBy preserved); a different checksum requires delete
	// permission on the old node. A path with a trailing slash creates a
	// folder node (empty body required).
	Put(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string) (*metadata.Node, error)
	// PutFromBlob creates (or idempotently re-creates) a node pointing at an
	// already-committed blob — the checksum-deploy use case (X-Checksum-Deploy,
	// rest-api.md section 1.3), reached through the Service so adapters never
	// touch storage directly (architecture section 5.1's exception clause:
	// "extend repo.Service, do not bypass"). The blob is addressed by
	// ref.Sha256, or — since T-73 (PRD §6.4-1, the maven ecosystem's sha1
	// dominance) — by ref.Sha1 alone when no sha256 is declared: the ledger's
	// sha1 index (idx_blobs_sha1) resolves it to the sha256-keyed row before
	// any other step runs, so a sha1 miss is the same ErrNodeNotFound the
	// sha256 miss answers (C15b's indistinguishable 404). The addressed blob
	// must exist in BOTH the filestore and the blobs ledger: an orphan blob
	// (physical file present, ledger row missing — the crash-window residue
	// GC eventually collects) yields ErrOrphanBlob instead of silently
	// materializing a node whose ancillary digests would be lost forever.
	// The ledger row is the digest source of truth: sha1/md5 are read from
	// it, never re-derived from the client's claim. Permission, overwrite
	// and idempotent-retransmit semantics are identical to Put.
	PutFromBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error)
	// PutLandedBlob creates (or idempotently re-creates) a blobs-ledger row
	// PLUS the node for a blob that is ALREADY committed in the filestore —
	// the PutFromBlob variant for callers that own a completed storage
	// session and hold the session's own digest triple (architecture section
	// 11.13, the M3 debt closure). docker's upload finalize (T-38) and the
	// future maven/npm chunked uploads call it right after Session.Commit so
	// the ledger+node rows land without re-reading the bytes: the previous
	// workaround streamed the landed blob back through Put, an O(size)
	// read-and-rehash per finalize that PutLandedBlob deletes.
	//
	// The contract difference against PutFromBlob: the ledger row does NOT
	// need to pre-exist — this method WRITES it (blob-first, Blobs.Put's
	// ON CONFLICT DO NOTHING keeps any existing row authoritative for
	// sha1/md5), so ref should carry the session-computed digests; a ref
	// lacking them materializes a row they will never be back-filled into
	// (the same permanence PutFromBlob's ErrOrphanBlob guard protects
	// against, which is why THAT method keeps refusing orphans). Only the
	// physical blob must exist (verified with an O(1) Open, never a
	// re-read). Permission, overwrite and idempotent-retransmit semantics
	// are identical to Put/PutFromBlob.
	PutLandedBlob(ctx context.Context, p *Principal, repoKey, path string, ref storage.BlobRef, mime string) (*metadata.Node, error)
	// Delete removes the node reference only (the blob is GC's business).
	// A directory path deletes recursively and prunes folder rows left empty.
	// Missing path → ErrNodeNotFound (idempotent 404 semantics).
	Delete(ctx context.Context, p *Principal, repoKey, path string) error
	// List returns every node under prefix ("" = whole repository), ordered
	// by path. Folder nodes (path ending in "/") are included. The prefix is
	// normalized: "d" and "d/" are equivalent and both return the folder row
	// plus everything beneath it (T-12 review B2).
	List(ctx context.Context, p *Principal, repoKey, prefix string) ([]*metadata.Node, error)

	// CreateRepo validates and persists a new repository configuration.
	// Admin only in M1 (repository management is an admin-plane operation).
	CreateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error)
	// GetRepo returns one repository configuration (any authenticated
	// principal).
	GetRepo(ctx context.Context, p *Principal, repoKey string) (*metadata.Repo, error)
	// ListRepos returns every repository ordered by key.
	ListRepos(ctx context.Context, p *Principal) ([]*metadata.Repo, error)
	// ListReposFiltered is ListRepos with the E-04 query filters (M04,
	// FR-15-AC5): repoType and packageType are EXACT-match column filters,
	// "" meaning "no filter on this axis". Unknown or misspelled values are
	// not errors — they match nothing and the result is an empty slice
	// (rest-api.md section 2: "invalid type/packageType -> empty array, no
	// error"), the semantics M1's E-04 already promised.
	ListReposFiltered(ctx context.Context, p *Principal, repoType, packageType string) ([]*metadata.Repo, error)
	// UpdateRepo updates description/config; type and package type are
	// immutable. Admin only.
	UpdateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error)
	// DeleteRepo removes the repository. Non-empty repositories require
	// deleteContent=true (ErrRepoNotEmpty names the flag otherwise); with it,
	// every node is removed first. Admin only.
	DeleteRepo(ctx context.Context, p *Principal, repoKey string, deleteContent bool) error

	// ---- Adapter SPI face (architecture section 5.4) ----
	//
	// The seams protocol adapters program against, beyond the public
	// use-case face above (oss-structure section 6 insight 2: separating
	// the "stable external face" from the "module SPI" is what keeps addon
	// code from reaching around the service into storage/metadata
	// internals). M3's protocol tickets add their orchestration methods in
	// THIS segment and nowhere else (maven T-67, npm T-69, pypi T-70); the
	// class-lookup half of this face is the ClassReader seam declared
	// below the interface.

	// PutWithOptions is Put with the adapter SPI's regenerable-content
	// exemption knob (T-68, closing the T-67 leftover). Every gate but the
	// exempted one is byte-for-byte Put's; Put itself is PutWithOptions
	// with the zero options (see PutOptions), so the two can never drift.
	// Consumers are the maven plane's metadata family — client re-PUTs of
	// checksum sidecars / maven-metadata.xml and the server-side metadata
	// calculator's writes (FR-17) — which rewrites those nodes on every
	// deploy; every other content caller keeps using Put.
	PutWithOptions(ctx context.Context, p *Principal, repoKey, path string, body io.Reader, expect storage.BlobRef, mime string, opts PutOptions) (*metadata.Node, error)

	// ---- Virtual aggregation face (T-72, FR-21-AC5/AC6) ----
	//
	// The per-protocol metadata aggregations (maven maven-metadata.xml
	// in-memory merge, npm packument merge, PyPI simple-index collection)
	// walk the members THEMSELVES — a merge cannot stop at the first hit
	// the way the download resolver does. The three methods below expose
	// exactly the T-71 resolution machinery the aggregations need, in the
	// two-bucket order, with the caller's read gate already satisfied on
	// the VIRTUAL key: members are resolution internals (getVirtual's own
	// posture), so these reads do NOT re-gate the principal per member —
	// a caller authorized on the virtual sees the merged member state even
	// when it holds no grant on a member. The membership guard inside each
	// method (the member must sit in the virtual's current order) is what
	// keeps the ungated reads from ever addressing an arbitrary repository.

	// VirtualMemberOrder returns the two-bucket resolution order of one
	// virtual repository, computed fresh off the member ledger on every
	// call (member changes are immediately effective, FR-15-AC6). Each
	// entry carries the member's class and its priority-bucket mark (the
	// maven foundByPriority short-circuit keys on it). A non-virtual or
	// unknown key answers ErrRepoNotFound / ErrInvalidRepoType.
	VirtualMemberOrder(ctx context.Context, virtualKey string) ([]VirtualMember, error)
	// ReadVirtualMember reads one member's copy of ONE metadata document
	// path (the maven metadata node, an npm packument node, a remote
	// member's simple-index page): a local member answers from its nodes,
	// a remote member through the full FR-20 chain — cache, stale
	// downgrade included. ErrNodeNotFound is the member's "no such
	// document" (upstream unfound, negative cache, offline without a
	// copy); a *StatusError is a classified member failure whose exact
	// rendering the caller propagates (maven: the block pass-through) or
	// tolerates (npm/pypi: the "one member failing must not block the
	// others" rule) per its protocol's aggregation spec. The returned
	// reader may carry the remote engine's response hints.
	ReadVirtualMember(ctx context.Context, virtualKey, member, path string) (io.ReadSeekCloser, *metadata.Node, error)
	// ListVirtualMember lists the node facts of one LOCAL member under a
	// prefix — the input the regenerated-from-facts metadata (the PyPI
	// simple index) merges on. Remote members refuse (their aggregated
	// state is an upstream document, ReadVirtualMember's business).
	ListVirtualMember(ctx context.Context, virtualKey, member, prefix string) ([]*metadata.Node, error)

	// ---- Docker use cases (M2, FR-7 through FR-9) ----
	//
	// The docker adapter owns the wire protocol; these methods own the
	// metadata orchestration so the adapter never touches the store directly
	// (architecture section 5.1: cross-layer bypass is the one thing the
	// layering forbids). Digest parameters are BARE hex sha256 — adapters
	// strip the "sha256:" prefix at the protocol edge (architecture section
	// 5.3 ruling 2: no algorithm conversion, M2 serves sha256 only).

	// PutManifest publishes one manifest that has ALREADY been committed as a
	// blob (the adapter uploads the body through the normal blob path first).
	// It writes the node at the docker layout path (<image>/manifests/<hex>,
	// architecture section 6) plus the manifests index row, the requested
	// tag's pointer (a re-push with the same tag REPOINTS it — Q4 ruling;
	// TagRepointed reports the move) and the ref ledger of the config/layer
	// digests m cites (PutRefs replaces the set atomically). tag may be empty
	// for a digest-only push.
	//
	// Manifest bodies are immutable (digest = sha256 of the body), so a
	// same-digest re-push is an idempotent republish: it skips the write
	// gate (the docker twin of Put's retransmit rule), leaves the manifest
	// row untouched (no created_by/media_type/size drift — a caller cannot
	// move a served Content-Type by re-announcing a digest it does not own)
	// and only re-runs the tag/refs upserts. A node at the layout path whose
	// sha256 is a DIFFERENT digest (only reachable by a forged internal call
	// or a hash collision) additionally requires delete permission on the
	// image, mirroring Put's overwrite pair.
	PutManifest(ctx context.Context, p *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) (*PutManifestResult, error)
	// ResolveManifest maps ref (a bare digest hex) to the manifest row.
	// Missing manifest → ErrManifestNotFound; the adapter derives the node
	// path for the blob read.
	ResolveManifest(ctx context.Context, p *Principal, repoKey, image, digest string) (*metadata.DockerManifest, error)
	// ResolveTag maps a tag to the digest it currently points at (empty
	// digest → the manifest was deleted; ErrTagNotFound when no such tag).
	ResolveTag(ctx context.Context, p *Principal, repoKey, image, tag string) (*metadata.DockerTag, error)
	// ListTags returns the image's tags lexicographically ordered, sliced by
	// the official pagination contract: at most n entries (n<=0 = all) after
	// the exclusive last cursor (a malformed cursor is ErrInvalidCursor). An
	// image without any manifest row is ErrImageNotFound (NAME_UNKNOWN); an
	// existing image with zero tags returns an empty slice — the "tags":null
	// vs [] rendering is the adapter's call (PRD R4).
	ListTags(ctx context.Context, p *Principal, repoKey, image string, n int, last string) ([]*metadata.DockerTag, error)
	// ListImages returns "<repoKey>/<image>" names with at least one manifest
	// row, lexicographically ordered and sliced like ListTags; a cursor from
	// another repository's key space is ErrInvalidCursor. It is the
	// /v2/_catalog source (T-40 renders the wire form).
	ListImages(ctx context.Context, p *Principal, repoKey string, n int, last string) ([]string, error)
	// DeleteManifest removes the manifest's node plus its index row, and the
	// store cascades the tag pointers and ref rows IN THE SAME TRANSACTION
	// (architecture section 11.12 — this method deliberately calls the
	// store's DeleteManifest rather than issuing three deletes: the
	// same-transaction cascade is the correctness property FR-9-AC6 rests
	// on). Referenced blobs are NOT touched — reclamation is GC's business
	// (FR-9-AC7 semantics).
	DeleteManifest(ctx context.Context, p *Principal, repoKey, image, digest string) error
	// DeleteRepoDocker drops the docker index rows of a repository as part of
	// its teardown: manifests/tags first (DeleteImage, one transaction per
	// image), then — driven by DeleteRepo AFTER the repositories row is gone
	// — the FK-less docker_refs rows (DeleteRepoRefs, architecture section
	// 11.12). DeleteRepo calls the two halves around Repos().Delete; this
	// method is the exported seam for the manifests/tags half so T-38+ can
	// test the teardown ordering contract.
	DeleteRepoDocker(ctx context.Context, repoKey string) (int64, error)
}

// PutOptions tunes PutWithOptions for the regenerable-content family
// (T-68's adapter SPI exemption, repo-semantics section 3's high-confidence
// rule: "checksum sidecar files (.sha1 etc.) and maven-metadata.xml never
// trigger the overwrite check — freely rewritable").
type PutOptions struct {
	// SkipOverwriteCheck exempts the write from the OVERWRITE half of the
	// permission pair: a pre-existing node at the path with a DIFFERENT
	// checksum no longer additionally requires DELETE permission on it.
	// The write grant itself still applies, and the idempotent-retransmit
	// shortcut (same declared sha256) keeps its meaning — only the
	// delete-permission demand is lifted, exactly the freedom the spec
	// grants the freely-rewritable family.
	SkipOverwriteCheck bool
}

// StatusError is a service-level failure that already knows its exact
// client-facing rendering: HTTP status, body message and optional response
// headers (Allow on a 405, ...). It exists so repository-CLASS semantics can
// stay entirely in the service layer (architecture section 5.4: adapters are
// unaware of the three classes) while the remote engine's outcomes — the
// RE-04 fault matrix, RE-05's read-only refusal — still reach the wire
// verbatim. Adapters render it as-is; Unwrap carries an optional sentinel
// (ErrNodeNotFound for the unfound family) so older mappings that predate
// this type keep their status.
type StatusError struct {
	// Code is the HTTP status.
	Code int
	// Message is the exact response body message.
	Message string
	// Header carries extra response headers (may be nil).
	Header http.Header

	cause error
}

// Error implements error with the exact client message — the adapter writes
// it into the errors[] envelope verbatim.
func (e *StatusError) Error() string { return e.Message }

// Unwrap keeps sentinel-based mappings (ErrNodeNotFound and friends) working
// through the typed rendering.
func (e *StatusError) Unwrap() error { return e.cause }

// NewStatusError builds a StatusError wrapping the given sentinel cause.
func NewStatusError(code int, message string, header http.Header, cause error) *StatusError {
	return &StatusError{Code: code, Message: message, Header: header, cause: cause}
}

// ClassReader is the adapter SPI face's read-only repository-class seam
// (architecture section 5.4): a protocol adapter must know the CLASS of the
// repository it is serving — maven's checksum sidecar passes a remote
// repository's engine 404 through (M3 PRD RE/04) instead of masking it with
// a local lookup — without a principal (anonymous content reads reach
// adapters before any management-plane gate; GetRepo's authentication
// demand does not fit) and without the row's protected fields: only the
// class is routing data, the same posture as httpapi's RepoLookup seam.
//
// Structurally satisfied by metadata.RepoStore; assembly wires it exactly
// like the routing seam (cmd passes md.Repos() — the docker package's
// NewRepoLookup is the same precedent). Callers map
// metadata.ErrRepoNotFound onto their protocol's not-found wording.
type ClassReader interface {
	// Get returns the repository row for repoKey; only Type (the class)
	// crosses this seam by contract. ErrRepoNotFound (the metadata sentinel)
	// when the key has no row.
	Get(ctx context.Context, repoKey string) (*metadata.Repo, error)
}

// The metadata store's Repos sub-store satisfies the class seam without an
// adapter type (compile-time pin of that claim, same convention as the
// docker package's seam assertions).
var _ ClassReader = metadata.RepoStore(nil)

// RemoteFetcher is the consumer-side seam of the M3 remote proxy engine
// (architecture section 5.4: repo.Service.Get dispatches type=remote to
// internal/remote.Fetch; the engine is assembled inside repo.New so neither
// cmd nor the adapters ever see it). nil inside the service means "no remote
// engine" (only reachable in tests that build the struct directly): Get on a
// remote repository then answers ErrRepoTypeNotSupported.
type RemoteFetcher interface {
	// Fetch pulls one path through the RE-04 six-step flow. The caller has
	// already validated the path, resolved the repository and authorized the
	// read; a *remote.FetchError renders verbatim (StatusError mapping) and
	// anything else is a plain 500.
	Fetch(ctx context.Context, repoKey, path string) (*remote.FetchResult, error)
	// Invalidate drops the local cache of one path (RE-06: DELETE on a
	// remote repository deletes the cached copy only, never upstream); it
	// reports whether anything was cached (204 vs 404).
	Invalidate(ctx context.Context, repoKey, path string) (bool, error)
	// Forget drops the repository's in-process state (client pool, offline
	// window) — the DeleteRepo teardown hook.
	Forget(repoKey string)
}

// The concrete engine satisfies the seam (compile-time pin).
var _ RemoteFetcher = (*remote.Engine)(nil)

// New builds the Service from its collaborator contracts (architecture
// section 3.3). st and md are required; az may be nil (admin-only mode); au
// may be nil (auditing disabled).
//
// PANIC CLAUSE (M3, T-66): the constructor also assembles the remote proxy
// engine and runs the ADR-0012 startup credential pass — a store holding
// remote credentials with no BINFLOW_REMOTE_CREDENTIALS_KEY (or a value the
// stored ciphertext does not open under) is an unusable configuration, and
// the constructor PANICS with a message naming the environment variable
// (FR-15-AC9-2's fail-fast: process startup fails, exit code non-zero).
// Callers that must pre-flight this without the panic can run
// remote.NewEngine(st, md, remote.EngineOptions{}) for the same check as an
// error.
func New(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger) Service {
	return NewWithClock(st, md, az, au, func() time.Time { return time.Now().UTC() })
}

// NewWithClock is New with an injected clock (RFC3339 UTC timestamps come
// from it). Exported for integration tests that must advance time
// deterministically. The same PANIC CLAUSE as New applies (see its godoc).
func NewWithClock(st storage.Engine, md metadata.Store, az Authorizer, au AuditLogger, now func() time.Time) Service {
	return newService(st, md, az, au, now)
}
