package repo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/storage"
	"github.com/lzwzzy/binflow/internal/webhook"
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
	// ErrInvalidSearchQuery: a search use case rejected its parameters — a
	// missing name, no checksum at all, a malformed digest or an oversized
	// repos filter (T-92, SR-01/SR-02). The HTTP layer maps this to 400.
	ErrInvalidSearchQuery = errors.New("invalid search query")
	// ErrSearchUnavailable: the configured metadata store does not carry the
	// search seam (metadata.NodeSearcher). Unreachable with the production
	// sqlite store; the HTTP layer maps this to 500.
	ErrSearchUnavailable = errors.New("search is not available on this store")
	// ErrPatternRejected: a repository's includesPattern/excludesPattern
	// governance refused the path (FR-24-AC4/W12a, repo-semantics section 6
	// erratum two: upload 409 / download 404 — the unfound download arm wraps
	// ErrNodeNotFound instead, this sentinel is the UPLOAD refusal's cause).
	ErrPatternRejected = errors.New("path rejected by repository include/exclude patterns")
	// ErrQuotaExceeded: the repository's quotaBytes ceiling refused the write
	// (GE-05/W26, FR-31). The concrete refusal is a *StatusError carrying the
	// exact 413 body (message with "quota exceeded" plus the used/quota
	// values); this sentinel is its cause so callers can branch without
	// string matching.
	ErrQuotaExceeded = errors.New("quota exceeded")
	// ErrPackageTypeNotAvailable: the package type's addon slot is registered
	// but not unlocked on this instance (M10 T-283, ADR-0032's D3: an
	// insufficient license tier, an allowlist that does not name it, or the
	// addons.disabled circuit breaker). The CONFIGURATION-plane refusal of
	// the closed degradation set — httpapi maps it to 400 (the repo
	// validation 400 family, deliberately not a licensing 403; the PRD
	// wanted 403 here, the divergence is registered for T-293's K25
	// final ruling).
	ErrPackageTypeNotAvailable = errors.New("package type not available")
)

// Repository types and package types (architecture section 6 DDL).
const (
	TypeLocal   = "local"
	TypeRemote  = "remote"
	TypeVirtual = "virtual"

	PackageGeneric = "generic"
	// PackageDocker serves LOCAL since M2 (FR-7-AC1), REMOTE since T-392
	// (M14 FR-129 — the /v2 pull-through rides the family-shared remote
	// data chain T-363 opened), and VIRTUAL since T-431 (M15 PRD Q6's
	// ruling — the T-365 aggregated read plane is family-shared, so a
	// docker virtual walks its members with helmoci virtual's semantics).
	PackageDocker = "docker"
	// PackageMaven/PackageNpm/PackagePypi open for all three classes in M3
	// (FR-15-AC1): local is served by the protocol adapters (T-67/T-69/T-70),
	// remote by the pull-through fetcher (T-66), virtual by the two-bucket
	// resolver (T-71).
	PackageMaven = "maven"
	PackageNpm   = "npm"
	PackagePypi  = "pypi"
	// PackageHelm is the classic Helm chart repository face (M11 T-309):
	// index.yaml + tgz on LOCAL repositories; the registry-v2 HelmOCI face
	// is a SEPARATE package type (HL-3 — the docker adapter serves it), and
	// the two families never share one virtual repository (the
	// validateHelmFamilyMix rule).
	PackageHelm = "helm"
	// PackageHelmOCI is the registry-v2 Helm face (HL-3): adapter routing
	// reuses docker's /v2 plane; the slot and adapter land with their own
	// ticket. Declared here so the virtual member-mix rule (T-309) and the
	// future adapter share one spelling.
	PackageHelmOCI = "helmoci"
)

// Reserved repo keys (ADR-0008, union finalized by the T-108 errata at the
// M4 PRD v1.1 baseline and extended by the T-91 review wave): they collide
// with /binflow routing segments — api/v2 (ADR-0008), docs (ADR-0011),
// console/ui (ADR-0014 as amended to the ui mount; the console spelling
// stays reserved against M5 collisions), assets (the console's fingerprinted
// asset mount: same shadowing-reachability shape as ui, architecture review
// of T-91 ruled it into the union). A repository under any of these keys
// would be unreachable behind its segment, so creation is refused (PRD
// FR-23-AC2/W01b; existing rows in legacy databases start up with a WARN —
// a P2 debt item, route stays occupied).
var reservedRepoKeys = map[string]bool{
	"api":     true,
	"v2":      true,
	"docs":    true,
	"console": true,
	"ui":      true,
	"assets":  true,
}

// Authorization actions (architecture section 3.4). Aliased from auth so
// repo and auth can never drift apart on the action vocabulary. ActionManage
// (M7, ADR-0026) is the repo-scoped admin bit: it never participates in
// content writes — the content plane keeps consuming r/w/d only — and its
// consumer here is the Usage observability gate's OR arm (family 7, T-217).
const (
	ActionRead   = auth.ActionRead
	ActionWrite  = auth.ActionWrite
	ActionDelete = auth.ActionDelete
	ActionManage = auth.ActionManage
)

// Audit actions emitted by this package (architecture section 3.5),
// aliased from the audit package's vocabulary. The quota action joined that
// vocabulary with T-93 (PRD FR-29's M4 set) and is aliased like the rest —
// one spelling, no drift (review NB2).
const (
	AuditActionDeploy     = audit.ActionDeploy
	AuditActionDownload   = audit.ActionDownload
	AuditActionDelete     = audit.ActionDelete
	AuditActionRepoCreate = audit.ActionRepoCreate
	AuditActionRepoUpdate = audit.ActionRepoUpdate
	AuditActionRepoDelete = audit.ActionRepoDelete

	// AuditActionQuotaExceeded is appended (with a WARN log) every time a
	// write is refused by the repository's quotaBytes ceiling (GE-05/W26).
	AuditActionQuotaExceeded = audit.ActionQuotaExceeded

	// AuditActionAddonDenied records one addon-gate refusal on the
	// configuration plane (M10 T-283, PRD FR-85.4: "变更面——建仓拒绝逐条").
	// The spelling is the ticket's license.* vocabulary — the PRD's
	// "addon.gate.deny" and the header name it pairs with are a registered
	// divergence; this constant is the one spelling both emitters (this
	// package's D3 refusals and httpapi's D2/D4 refusals) share. Detail
	// carries {"addon", "refusal"}; Actor/Repo/Path carry the request's.
	// Adding it to audit.Actions()' picker list is the audit owner's
	// one-liner (the license.* words' T-279 note).
	AuditActionAddonDenied = "license.addon.denied"
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
// answer the C5 405 (T-71); virtual List aggregates the member children
// union over the same order (T-412, FR-136).
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
	// plus everything beneath it (T-12 review B2). A VIRTUAL repository
	// answers the member children UNION over the resolution order — first
	// member winning a path two members carry; rows keep their member repo
	// key. A remote member contributes cache rows by default; a member (or
	// repository) with listRemoteFolderItems=true additionally merges its
	// upstream-derived DISPLAY rows — zero 落库, cache rows first, and gated
	// on the member's own read (T-412/T-448; repo-semantics §8.5).
	List(ctx context.Context, p *Principal, repoKey, prefix string) ([]*metadata.Node, error)

	// CreateRepo validates and persists a new repository configuration.
	// Authentication is demanded here; the AUTHORIZATION door is the caller's
	// (T-217/FR-65: httpapi's family-6 create-arm CapRepoWrite branch —
	// repository creation is not delegated to manage holders). Direct
	// callers must gate themselves.
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
	// immutable. Authentication is demanded here; the AUTHORIZATION door is
	// the caller's (T-217/FR-65: httpapi's family-7 repoManage write gate,
	// which manage holders pass through Can(repo, "", m)). Direct callers
	// must gate themselves.
	UpdateRepo(ctx context.Context, p *Principal, r *metadata.Repo) (*metadata.Repo, error)
	// DeleteRepo removes the repository. Non-empty repositories require
	// deleteContent=true (ErrRepoNotEmpty names the flag otherwise); with it,
	// every node is removed first. Admin only — this method KEEPS the
	// service-level admin door (family 6: deletion is never delegated to
	// manage holders, T-217).
	DeleteRepo(ctx context.Context, p *Principal, repoKey string, deleteContent bool) error

	// Usage reports one repository's quota observability state (GE-06/W26b,
	// FR-31): UsedBytes is the repo_usage logical total (the same number the
	// quota gate enforces against), QuotaBytes the configured ceiling (0 =
	// unlimited, the default). The gate is the family-7 OR formula
	// (T-217/K11): a read grant OR the manage bit on the repository, with
	// admin/readonly_admin passing through the role's global read; everyone
	// else answers ErrForbidden (ErrUnauthorized anonymous). Works for every
	// repository class: a remote answers its (unmetered, Q2) counter state —
	// typically the 005 migration snapshot — and a virtual zero, since
	// virtual writes route onto a local member and are metered there.
	Usage(ctx context.Context, p *Principal, repoKey string) (*UsageReport, error)

	// UsageBatch reports the usage view of EVERY repository the principal may
	// see (M9 E1, ADR-0030 / architecture section 14.1, FR-79.1): the set
	// form of Usage for the fan-out collapse behind
	// GET /api/v1/storage/usage. The data source is ONE aggregate query
	// (Usage().List); visibility is the SAME family-7 OR formula applied per
	// repository — CanManageRepo(read) ∨ Can(r), W26b verbatim, so the batch
	// row set can never disagree with the single-repo endpoint's decision.
	// Repositories the caller cannot read are absent from the result, not
	// errors: the endpoint answers a filtered view (empty set = empty slice,
	// never ErrForbidden — the caller is legitimately authenticated), and an
	// invisible repository's existence is not leaked (a point-named key that
	// is unknown OR invisible is silently missing, indistinguishably).
	// q.Repos nil means "every visible repository"; a non-nil slice
	// point-names a subset (duplicates and empties collapse). Anonymous
	// answers ErrUnauthorized. q.IncludeCounts fills NodeCount/UpdatedAt on
	// every returned row.
	UsageBatch(ctx context.Context, p *Principal, q UsageBatchQuery) ([]*UsageBatchReport, error)

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

	// RewriteSubtreePrefix re-homes every node row under the FOLDER prefix
	// srcPrefix onto the corresponding path under dstPrefix (a path P under
	// src becomes dstPrefix + P[len(srcPrefix):]) — the ADR-0042 boot-sweep
	// primitive (T-371, FR-119.2). This is a SYSTEM-STATE operation: no
	// principal, no permission gate, and ZERO user-plane side effects — no
	// copy/move observer, no webhook emission (an internal re-layout is not
	// a user action), no per-node audit; the blob store is never touched
	// (rows keep their sha256 — checksum addressing makes the move a
	// metadata rewrite), and node properties do not ride (the conan package
	// trees this was built for carry none).
	//
	// Disposition when a row already exists at a target path (the caller's
	// observable, never a silent loss): same sha256 → the source row is
	// dropped and counted Deduped (the retransmit-idempotent form, content
	// single copy); different sha256 → the row with the NEWER UpdatedAt
	// resides at the target and the pair is reported in Conflicts (the
	// loser's content stays in the blob store, recoverable within GC's
	// grace). An empty source subtree is ErrNodeNotFound; a dstPrefix inside
	// srcPrefix is ErrInvalidPath; a non-local repository is
	// ErrRepoTypeNotSupported. Durability: writes are per-row
	// (PutNodeWithUsage / DeleteNodeWithUsage pairs) — the one caller runs
	// this BEFORE the HTTP listener (ADR-0042 decision 1), so a half-applied
	// tree is structurally unobservable and an interrupted sweep resumes by
	// predicate on the next boot.
	RewriteSubtreePrefix(ctx context.Context, repoKey, srcPrefix, dstPrefix string) (*SubtreeRewrite, error)

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

	// ---- Search use cases (T-92, FR-26 / SR-01/SR-02) ----
	//
	// The M4 search face. Both methods return FILE nodes only (folder rows
	// never surface), ordered by (repo, path), and both run the same
	// visibility rule: a node appears exactly when the caller could GET it
	// (NFR-S24 — admin sees everything, everyone else is filtered through
	// the content-plane read ACL; anonymous follows the anonymous_access
	// channel and a closed instance denies with ErrForbidden before any
	// store access). Malformed parameters answer ErrInvalidSearchQuery.

	// SearchArtifacts returns every file node whose repo-relative path
	// contains name as a literal, case-INsensitive substring (K64's
	// calibration, aql.md section 0-5 — SQL LIKE with both sides folded;
	// the `*` wildcard family stays unimplemented: the official and
	// decompiled readings both treat name bytes literally). The gavc/prop/
	// pattern arms of that former P2 note landed on the
	// LegacySearchService face (T-417). repos narrows the candidate
	// repositories (nil/empty = all); unknown keys simply match nothing. An
	// empty name answers ErrInvalidSearchQuery.
	SearchArtifacts(ctx context.Context, p *Principal, name string, repos []string) ([]*metadata.Node, error)
	// SearchChecksum returns every file node referencing a blob addressed by
	// any of the query's digests (union; cross-repository references are all
	// reported — the "which paths hold this content" question). Digests are
	// bare hex, case-normalized; sha1/md5 resolve through the blobs ledger.
	// An all-empty or malformed query answers ErrInvalidSearchQuery.
	SearchChecksum(ctx context.Context, p *Principal, q ChecksumQuery, repos []string) ([]*metadata.Node, error)

	// ---- AQL scope seams (T-413, FR-133.2 / ADR-0043 pt 4) ----
	//
	// The two-stage read-only ACL weave of the AQL engine: SearchScope is
	// the collection-level first stage (the repo_key predicate the engine
	// compiles into the WHERE tree — the repoFilter narrowing of the SQL,
	// hoisted to the caller's whole readable set), CanRead the row-level
	// second stage over the path-scoped repositories. Zero side effects by
	// contract: no writes, no audit rows, no webhooks.

	// SearchScope returns every local or remote repository the principal
	// can read at least one path of, classified PathScoped when every read
	// grant covering the repository carries include/exclude path patterns
	// (those repositories' rows need the CanRead re-check; a pattern-free
	// target covers every path, so its rows skip the second stage at zero
	// per-row cost). Admin and readonly-admin principals — and anonymous
	// callers on an open instance — read globally: every local and remote
	// repository, PathScoped=false. Virtual repositories never enter the
	// scope (nodes carry no virtual rows, aql.md §7). Anonymous callers on
	// a closed instance meet ErrForbidden (the searchGate rule, T-92's
	// gate); an empty scope is a legitimate answer — the engine
	// short-circuits to an empty result without touching SQL.
	SearchScope(ctx context.Context, p *Principal) ([]ReadScope, error)
	// CanRead reports whether p may read repoKey/path — the same
	// allow(read) decision a download and T-92's filterVisible run, so the
	// query plane and the content plane can never disagree on visibility.
	CanRead(ctx context.Context, p *Principal, repoKey, path string) bool
}

// ReadScope is one repository of the caller's AQL search scope (T-413,
// FR-133.2 / ADR-0043 pt 4). The engine weaves the whole scope into the
// query's WHERE tree and re-checks only the PathScoped rows through
// Service.CanRead — the second stage that keeps path include/exclude
// patterns a single-sourced decision (auth's matcher, never a SQL
// translation).
type ReadScope struct {
	// Repo is the repository key.
	Repo string
	// PathScoped marks a repository whose read grants all carry path
	// patterns: rows in it must clear CanRead before they surface.
	PathScoped bool
}

// UsageReport is the GE-06 usage view: the repository's metered total and
// its configured ceiling, projected straight onto /api/v1/storage/usage/{repo}.
// (The name avoids both the repo.RepoUsage stutter revive flags and the
// method/type homonymy of a bare Usage.)
type UsageReport struct {
	RepoKey    string
	UsedBytes  int64
	QuotaBytes int64 // 0 = unlimited (the default)
}

// UsageBatchQuery carries E1's two optional knobs (architecture section 14.1
// E1): the point-named repository subset and the counts arm. The zero query
// is "every visible repository, base fields only".
type UsageBatchQuery struct {
	// Repos nil means no ?repos= parameter: every repository in the caller's
	// visibility set. A non-nil slice point-names keys (the ?repos=a,b,c
	// form): the result is the intersection with the existing-and-visible
	// rows, so unknown, duplicated or invisible keys simply contribute
	// nothing — no error, no existence signal.
	Repos []string
	// IncludeCounts is the ?include=counts arm: every row additionally
	// carries NodeCount (FILE nodes only) and UpdatedAt (the repository
	// CONFIG change moment, repositories.updated_at — not the newest
	// artifact time; the semantic is pinned in architecture section 14.1).
	IncludeCounts bool
}

// UsageBatchReport is one E1 row: the single-repo UsageReport fields (the
// wire row stays field-for-field isomorphic with the usage/{repo} body) plus
// the two ?include=counts extras, zero-valued when counts were not requested.
type UsageBatchReport struct {
	UsageReport
	// NodeCount is the repository's FILE node count (folder sentinel rows
	// excluded); meaningful only when the query asked for counts.
	NodeCount int64
	// UpdatedAt is repositories.updated_at, the configuration change
	// moment; meaningful only when the query asked for counts.
	UpdatedAt string
}

// PutOptions tunes PutWithOptions for the regenerable-content family
// (T-68's adapter SPI exemption, repo-semantics section 3's high-confidence
// rule: "checksum sidecar files (.sha1 etc.) and maven-metadata.xml never
// trigger the overwrite check — freely rewritable") and carries the
// deploy-time properties of the M10 matrix-parameter peel (T-286,
// architecture section 15.3.1: "既有 PutOpts SPI 缝〔T-67〕顺势承载").
type PutOptions struct {
	// SkipOverwriteCheck exempts the write from the OVERWRITE half of the
	// permission pair: a pre-existing node at the path with a DIFFERENT
	// checksum no longer additionally requires DELETE permission on it.
	// The write grant itself still applies, and the idempotent-retransmit
	// shortcut (same declared sha256) keeps its meaning — only the
	// delete-permission demand is lifted, exactly the freedom the spec
	// grants the freely-rewritable family.
	SkipOverwriteCheck bool
	// Properties are deploy-time matrix properties ("PUT r/a.bin;k=v"):
	// they land on the deployed node with the SAME merge semantics as the
	// REST ?properties PUT (same-key value-set replace, other keys kept —
	// architecture section 11.40), so a redeploy annotates rather than
	// clobbers. nil/empty (every non-matrix deploy) writes nothing —
	// existing properties survive a plain re-PUT. ValidatePropSet guards
	// the shape; adapters only ever pass sets ParseMatrixProps produced.
	Properties map[string][]string
}

// SubtreeRewrite reports one RewriteSubtreePrefix outcome (ADR-0042's
// reconciliation vocabulary: the caller renders the per-repository line and
// applies the conflict=0 green gate on top of these counts).
type SubtreeRewrite struct {
	// Moved is the count of node rows re-homed src→dst with their sha256
	// (and every other stored field) unchanged.
	Moved int
	// Deduped is the count of source rows dropped because the target path
	// already held the SAME sha256 — the retransmit-idempotent form; content
	// ends single-copy, nothing is lost.
	Deduped int
	// Conflicts reports the target paths where a DIFFERENT sha256 already
	// resided; the newer row (UpdatedAt) won and stays at the target, the
	// loser's content remains in the blob store within GC's grace.
	Conflicts []RewriteConflict
}

// RewriteConflict is one differing-sha collision the rewrite resolved
// newer-wins.
type RewriteConflict struct {
	// Path is the TARGET path the collision happened at.
	Path string
	// Kept is the sha256 of the row that won and resides at Path.
	Kept string
	// Dropped is the sha256 of the loser — its blob is untouched and
	// recoverable until GC's grace expires.
	Dropped string
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
//
// T-367 widening note: exactly one consumer additionally reads the row's
// canonical CONFIG JSON through this seam — the helm virtual face's member
// rewriting resolves the member's chartsBaseUrl (a public, non-protected
// field; the canonical remote form never carries a password). The class
// stays the seam's routing payload; the config read is that one consumer's
// own, documented at its call site (helm memberContext).
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
	// FetchAbsolute pulls one ABSOLUTE third-party URL through the same
	// state machine and lands it at the repository-relative storage path
	// (T-367, FR-117 — the helm _external dependency face; helm.md section
	// 6/S10). Credential-less egress by contract (the target is a third
	// party, never the configured upstream).
	FetchAbsolute(ctx context.Context, repoKey, path, target string) (*remote.FetchResult, error)
	// Invalidate drops the local cache of one path (RE-06: DELETE on a
	// remote repository deletes the cached copy only, never upstream); it
	// reports whether anything was cached (204 vs 404).
	Invalidate(ctx context.Context, repoKey, path string) (bool, error)
	// Forget drops the repository's in-process state (client pool, offline
	// window) — the DeleteRepo teardown hook.
	Forget(repoKey string)
	// BrowseRemote enumerates the upstream children of one folder as
	// DISPLAY-ONLY rows (T-442's engine; T-448's listing wiring is the
	// first consumer). The engine never reads the listRemoteFolderItems
	// flag — the CALLER decides to call, which is what keeps the optional
	//档's off posture at behavior diff zero. Authorization rides permit, the
	// caller's own allow() handed in as a closure and consulted BEFORE any
	// upstream contact (nil or refusing → ErrBrowseDenied, zero rows); an
	// upstream fault answers Degraded with a nil error, so a listing face
	// keeps serving its cached rows beside the note.
	BrowseRemote(ctx context.Context, permit remote.BrowsePermit, repoKey, folder string) (*remote.BrowseResult, error)
}

// The concrete engine satisfies the seam (compile-time pin).
var _ RemoteFetcher = (*remote.Engine)(nil)

// RemoteExternalPlane is the absolute-URL dependency pull-through seam
// (M13 T-367, FR-117, helm.md section 6/S10): the helm _external face's
// hops run through the SAME engine machinery a path-joined fetch does —
// the folded proxy path IS the storage path, so the landing, the negative
// cache, the TTL classes and the stale downgrade are the ordinary
// path-keyed ones, and a second pull serves from the local copy with zero
// egress. The consumer resolves the capability by type-asserting Service
// (the RemoteV2Plane precedent — an optional SPI segment; an assembly
// without the seam answers the pre-T-367 pass-through instead).
type RemoteExternalPlane interface {
	// FetchExternal pulls one absolute dependency URL into repoKey's cache
	// at path (the folded _external spelling), read-gated like Get.
	// Unfound outcomes wrap ErrNodeNotFound; a *StatusError renders
	// verbatim (the SSRF 400 family included).
	FetchExternal(ctx context.Context, p *Principal, repoKey, path, target string) (io.ReadSeekCloser, *metadata.Node, error)
	// FetchVirtualExternal is the membership-guarded member twin: the
	// virtual _external walk lands into the REMOTE MEMBER's cache (the
	// ReadVirtualMember posture — the read gate has already run on the
	// VIRTUAL key, the member must currently sit in its order, no
	// per-member re-gate).
	FetchVirtualExternal(ctx context.Context, p *Principal, virtualKey, member, path, target string) (io.ReadSeekCloser, *metadata.Node, error)
}

// RemoteBrowseListing is ListWithRemote's answer: the ordinary listing rows
// plus the remote-browse layer's state (M16 T-448, FR-147.2). RemoteDegraded
// is non-empty only when an ENGAGED layer (a flag-on remote repository, or
// such a member of a virtual) met an upstream fault or sits inside the
// assumed-offline silence — the rows are then the cached ones alone, never a
// failed listing (remote-browsing.md §4-1: 枚举失败不整树塌). A flag-off or
// all-healthy tree answers "".
type RemoteBrowseListing struct {
	Nodes          []*metadata.Node
	RemoteDegraded string
}

// RemoteBrowsePlane is the optional档's enriched listing seam (M16 T-448,
// FR-147.2 — the RemoteV2Plane precedent, an optional SPI segment): the
// exact walk Service.List performs plus the remote layer's degradation
// note, so a consumer that renders the optional档's error state (the tree
// face, T-461) can show WHY the remote layer went quiet beside the cached
// rows instead of the note being lost to List's plain slice. One resolution
// channel: List is this walk with the note dropped, so the two faces can
// never disagree about a tree.
type RemoteBrowsePlane interface {
	// ListWithRemote is Service.List's walk with the remote layer's state
	// note attached. Same gates, same ordering, same rows.
	ListWithRemote(ctx context.Context, p *Principal, repoKey, prefix string) (*RemoteBrowseListing, error)
}

// The concrete service satisfies the seam (compile-time pin).
var _ RemoteBrowsePlane = (*service)(nil)

// RemoteV2Plane is the registry-v2 remote pull-through seam (M13 T-363,
// FR-116.1, helm.md section 8.3): the OCI Distribution upstream
// conversation — Accept negotiation, the 401/WWW-Authenticate Bearer token
// exchange, tag-to-digest resolution — exceeds the generic engine's
// path-joined fetch, so the docker adapter drives that conversation with
// its own session over the engine's EXPORTED outbound client. The CACHE
// half stays service-owned through this seam: probing, landing and
// miss-recording run the same invariants the engine's land() owns
// (blob-first commit, checksum-addressed blobs, TTL cache rows, GC holds),
// so the two cache writers can never disagree about what a cached copy is.
//
// The consumer resolves the capability by type-asserting Service (the
// RemoteFetcher precedent — an optional SPI segment, so test doubles that
// embed the interface keep compiling and an assembly without the seam
// answers honestly instead of half-serving).
type RemoteV2Plane interface {
	// RemoteUpstream resolves one remote repository's upstream connection
	// facts — URL, credential (password DECRYPTED for the session, never
	// persisted or echoed by the caller), egress policy and cache TTLs.
	// Read-gated: the caller has already passed the /v2 endpoint's own
	// scope question; this is the defense-in-depth re-check.
	RemoteUpstream(ctx context.Context, p *Principal, repoKey string) (*RemoteUpstream, error)
	// ProbeRemoteCache answers the local-cache state of one storage path
	// WITHOUT any upstream contact (the engine's steps 3+4, read-only):
	// negative window, fresh copy, expired copy, or nothing.
	ProbeRemoteCache(ctx context.Context, p *Principal, repoKey, path string) (*RemoteProbe, error)
	// LandRemoteBlob lands one upstream-fetched body checksum-addressed
	// (the engine's land() invariants; expectHex is enforced by the storage
	// commit — a body that is not the digest it claims never lands).
	LandRemoteBlob(ctx context.Context, p *Principal, repoKey, path, expectHex, mime string, body io.Reader) (*metadata.Node, error)
	// CacheRemoteMiss records one upstream miss (the negative cache).
	CacheRemoteMiss(ctx context.Context, p *Principal, repoKey, path string) error
	// RecordRemoteManifest records the docker index rows of one cached
	// remote manifest (manifest row, tag pointer, best-effort refs) —
	// cache population, not a deploy: no write-plane gates run.
	RecordRemoteManifest(ctx context.Context, p *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) error
}

// V2VirtualPlane is the registry-v2 virtual aggregation seam (M13 T-365,
// FR-116.2): a VIRTUAL docker/helmoci repository aggregates its members'
// registry-v2 state — manifest/tag rows, blob nodes, remote cache facts —
// through these membership-guarded reads. The docker adapter drives the
// walk itself because a remote member's miss must become an UPSTREAM
// conversation (the RemoteV2Plane posture), which is the adapter's session
// to run; everything local stays service-owned here.
//
// The guard is what keeps these reads ungated safe: every member-scoped
// method asserts the member sits in the virtual's CURRENT two-bucket order
// (the ReadVirtualMember posture — members are resolution internals, the
// /v2 route gate already answered the permission question on the VIRTUAL
// key, so no per-member re-gate runs). The consumer resolves the
// capability by type-asserting Service (the RemoteV2Plane precedent).
type V2VirtualPlane interface {
	// V2MemberOrder returns the two-bucket resolution order of one
	// registry-v2 family virtual repository (family-checked: a non-virtual
	// or non-v2 key answers ErrRepoTypeNotSupported).
	V2MemberOrder(ctx context.Context, virtualKey string) ([]VirtualMember, error)
	// V2MemberManifest answers ONE member's local fact base for a manifest
	// reference (a bare-digest or tag spelling, exactly as the /v2 route
	// parsed it) WITHOUT any upstream contact: the resolved digest and
	// serving facts when the member's rows answer, plus the standing node.
	// A member whose rows do not answer is a WALK MISS (Digest ""), not an
	// error — a remote member's Cache=RemoteProbeMiss is the caller's
	// signal to run the upstream conversation for that member.
	V2MemberManifest(ctx context.Context, p *Principal, virtualKey, member, image, reference string) (*V2MemberManifest, error)
	// V2MemberBlob answers one member's standing blob node at the
	// digest-keyed blob layout path (local member node / remote member
	// cache probe, RemoteV2Plane.ProbeRemoteCache's states verbatim).
	V2MemberBlob(ctx context.Context, p *Principal, virtualKey, member, image, hex string) (*V2MemberBlob, error)
	// V2MemberUpstream resolves one REMOTE member's upstream connection
	// facts (RemoteUpstream's shape; the membership-guarded twin of
	// RemoteV2Plane.RemoteUpstream).
	V2MemberUpstream(ctx context.Context, p *Principal, virtualKey, member string) (*RemoteUpstream, error)
	// V2LandMemberBlob lands one upstream-fetched body into the REMOTE
	// member's cache (RemoteV2Plane.LandRemoteBlob's invariants, guarded
	// by membership instead of the member's own permission pair).
	V2LandMemberBlob(ctx context.Context, p *Principal, virtualKey, member, path, expectHex, mime string, body io.Reader) (*metadata.Node, error)
	// V2RecordMemberManifest records the docker index rows of one cached
	// remote-member manifest (RemoteV2Plane.RecordRemoteManifest's
	// posture, membership-guarded).
	V2RecordMemberManifest(ctx context.Context, p *Principal, virtualKey, member, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) error
	// V2CacheMemberMiss records one upstream miss against the REMOTE
	// member (RemoteV2Plane.CacheRemoteMiss, membership-guarded).
	V2CacheMemberMiss(ctx context.Context, p *Principal, virtualKey, member, path string) error
	// V2WriteRefusal renders the /v2 write refusal of one registry-v2
	// family virtual repository (a *StatusError the adapter answers
	// verbatim): the C5 405 when no write route is configured, and — a
	// route IS configured — the honest wording that registry-v2
	// push-through routing is not implemented (the target is named, never
	// claimed).
	V2WriteRefusal(ctx context.Context, virtualKey string) *StatusError
}

// V2MemberManifest is one member's answer for a manifest reference during a
// virtual walk: the resolved identity and serving facts when the member's
// rows answer, the standing copy when one exists, and the remote-member
// cache state (RemoteProbe* constants; "" on a local member — local is the
// origin, not a cache).
type V2MemberManifest struct {
	// Digest is the member's resolved manifest digest ("" when the
	// member's rows do not answer — the walk continues).
	Digest string
	// MediaType and Size are the manifest ROW's serving facts.
	MediaType string
	Size      int64
	// Node is the standing copy at the member's manifest layout path (nil
	// when none stands — a local member with rows but no node is the crash
	// window and reads as a walk miss, probeLocalMember's posture).
	Node *metadata.Node
	// Cache is the remote-member probe state (RemoteProbeHit/Stale/
	// Negative/Miss); "" for a local member's answer.
	Cache string
}

// V2MemberBlob is one member's standing blob answer: the node when a copy
// stands, plus the remote-member cache state ("" on a local member).
type V2MemberBlob struct {
	Node  *metadata.Node
	Cache string
}

// RemoteUpstream is the upstream fact bundle of one registry-v2 remote
// repository (RemoteV2Plane.RemoteUpstream's result). Password is plaintext
// in memory for the adapter's session only — the caller must never log or
// echo it (NFR-S14).
type RemoteUpstream struct {
	URL                  string
	Username             string
	Password             string
	TokenAuth            bool // enableTokenAuthentication: the password IS a bearer token
	AllowPrivateUpstream bool
	SocketTimeoutMs      int64
	ContentTTLSeconds    int64
	MissedTTLSeconds     int64
	BlockedOut           bool
}

// The RemoteProbe states (the X-BinFlow-Cache tokens they serve).
const (
	// RemoteProbeHit: a fresh local copy serves the request (HIT).
	RemoteProbeHit = "HIT"
	// RemoteProbeStale: an expired local copy stands (STALE — served on an
	// upstream fault or upstream 404; revalidated by a successful fetch).
	RemoteProbeStale = "STALE"
	// RemoteProbeNegative: a fresh miss record answers unfound with zero
	// upstream packets.
	RemoteProbeNegative = "NEGATIVE"
	// RemoteProbeMiss: nothing local — the upstream conversation decides.
	RemoteProbeMiss = "MISS"
)

// RemoteProbe is one ProbeRemoteCache outcome: the state plus the standing
// node on HIT/STALE (nil otherwise).
type RemoteProbe struct {
	Node  *metadata.Node
	State string
}

// Replicator is the push-replication enqueue seam (M6, ADR-0021 decision 2:
// "repo.Service.Put 链末增加 replication.Enqueue 调用（异步，非阻塞）").
// The internal/replication engine satisfies it structurally; the seam lives
// here, on the consumer side, so repo never imports the replication package
// (the engine reads blobs through storage, not through this package — no
// cycle either direction).
//
// Enqueue is fire-and-forget: implementations never return an error, must be
// safe to call from any goroutine, and must complete quickly (persisting a
// pending task row and waking a worker). The Put tail invokes it on a
// detached context in its own goroutine, so an upload never waits on — and
// never fails because of — replication.
type Replicator interface {
	// Enqueue records that (repoKey, path) landed with sha256 (hex, the
	// node's blob digest; empty for folder nodes, which the hook skips).
	Enqueue(ctx context.Context, repoKey, path, sha256 string)
}

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

// AttachReplicator wires the push-replication enqueue seam (M6, ADR-0021)
// onto a Service built by New/NewWithClock: after this call, every
// successful file Put on a repository with an enabled replication config
// enqueues a pending replication task. Call it during assembly, BEFORE the
// first request is served (the field is plain; concurrent attach-while-
// serving is not part of the contract). A non-concrete Service (a test
// fake) is skipped with a WARN — the seam is best-effort at the wiring
// layer, never a startup hazard.
func AttachReplicator(s Service, r Replicator) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: AttachReplicator: service is not the concrete implementation; replication hook not wired")
		return
	}
	impl.repl = r
}

// PackageTypeVerdict is the addon plane's answer for one package-type value
// (M10 T-283, ADR-0032 weave point 2 / ADR-0033): whether the instance
// registers a slot for it, and whether repositories of that type may be
// created right now. The refusal clause arrives pre-rendered ("license tier
// 'community' < 'pro'", "disabled by configuration (addons.disabled) …") so
// this package stays free of the license and addons imports — the verdict's
// derivation lives at the assembly point where both collaborators meet.
type PackageTypeVerdict struct {
	// Known reports whether a package-type addon slot is registered for the
	// value on this instance (the dynamic legal set — the static five-type
	// enum's extension, T-282 leftover 2).
	Known bool
	// Unlocked reports whether the slot may be used right now: tier
	// sufficiency, the document's addon allowlist and the addons.disabled
	// breaker all agreeing (license.Manager.AddonEnabled's single
	// evaluation, reached through the assembly adapter).
	Unlocked bool
	// Refusal is the pointed clause when Known && !Unlocked ("" otherwise
	// and when unknown) — D3's parenthetical, e.g.
	// `license tier 'community' < 'pro'`.
	Refusal string
}

// PackageTypeGate is the consumer-side seam CreateRepo/UpdateRepo/DeleteRepo
// consult (architecture section 15.1.5 weave point 2: "消费方经小接口注入，
// repo 不 import license 包"). Satisfied by the cmd assembly's adapter over
// addons.Registry + license.Manager; nil (every pre-M10 stack and the
// unwired test posture) keeps the static five-type enum as the whole
// legality check — byte-identical M9 behavior.
type PackageTypeGate interface {
	// Verdict answers for one package-type value. Implementations must be
	// safe for concurrent use and side-effect free (the license snapshot is
	// an atomic read; D6's expiry and install/uninstall flips are observed
	// per call with no tearing).
	Verdict(ctx context.Context, packageType string) PackageTypeVerdict
}

// AttachPackageTypeGate wires the addon-plane verdict seam onto a Service
// built by New/NewWithClock (the AttachReplicator precedent: the constructor
// signature stays stable for every existing caller). Call it during
// assembly, BEFORE the first request is served. A non-concrete Service (a
// test fake) is skipped with a WARN — best-effort at the wiring layer,
// never a startup hazard. With no gate attached the static enum rules alone
// (the pre-M10 posture, invariant 1).
func AttachPackageTypeGate(s Service, g PackageTypeGate) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: AttachPackageTypeGate: service is not the concrete implementation; package-type gate not wired")
		return
	}
	impl.pkgGate = g
}

// AttachCopyMoveObserver wires the copy-side index-recompute seam onto a
// Service built by New/NewWithClock (M12 T-339, the AttachReplicator
// precedent). The observer fires asynchronously after every completed
// non-dry COPY (move does not trigger the recalculation — repo-operations
// section 1.4) with the target repository and the candidate directory set.
// Call it during assembly, BEFORE the first request is served; a
// non-concrete Service is skipped with a WARN.
func AttachCopyMoveObserver(s Service, o CopyMoveObserver) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: AttachCopyMoveObserver: service is not the concrete implementation; copy observer not wired")
		return
	}
	impl.cmObserver = o
}

// WebhookEmitter is the unified-event bus seam (M13 T-362, ADR-0041
// decision 1: the repository domain's mutation tails call Emit with one
// event and never learn about subscriptions). Satisfied by the
// internal/webhook Bus; the seam lives here, consumer-side, so repo never
// imports the webhook package beyond the event type (matching, gating and
// the outbox are the bus's alone).
//
// Emit is synchronous-but-bounded by contract: implementations must never
// fail the caller (the bus's own recover/WARN path guarantees it), must
// complete in small-transaction time (the outbox batch insert), and must
// be safe for concurrent use.
type WebhookEmitter interface {
	Emit(ctx context.Context, e webhook.Event)
}

// AttachWebhookEmitter wires the unified-event seam onto a Service built
// by New/NewWithClock (the AttachReplicator precedent: the constructor
// signature stays stable for every existing caller). Call it during
// assembly, BEFORE the first request is served; a non-concrete Service is
// skipped with a WARN. With no emitter attached the mutation tails keep
// their pre-M13 behavior byte for byte (invariant: the seams are additive).
func AttachWebhookEmitter(s Service, e WebhookEmitter) {
	impl, ok := s.(*service)
	if !ok {
		slog.Warn("repo: AttachWebhookEmitter: service is not the concrete implementation; webhook seam not wired")
		return
	}
	impl.hooks = e
}

// hookActorOf projects a principal onto the outbound envelope's
// userContext triple (webhook.md section 4: id = the username or token
// subject, isToken, realm — BinFlow's "local" provider is the official
// "internal" spelling).
func hookActorOf(p *Principal) webhook.Actor {
	if p == nil {
		return webhook.Actor{ID: "anonymous", Realm: webhook.RealmFor("")}
	}
	return webhook.Actor{
		ID:      p.Name,
		IsToken: p.TokenID > 0,
		Realm:   webhook.RealmFor(string(p.Source)),
	}
}
