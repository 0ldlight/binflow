package bundle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Principal is the request identity, aliased from the auth package (the
// build.Service precedent): one declaration shared by both sides of the
// boundary, nil means anonymous.
type Principal = auth.Principal

// Authorizer is the consumer-side ACL seam of the Any Distribution
// channel (ADR-0046 decision 3): the auth package's Service satisfies it
// structurally, and the SAME assembled instance the platform holds must be
// injected (single-decision-point discipline). The channel call is
// Can(p, auth.BucketAnyDistribution, bundle_name, r) — the bucket literal
// rides the repoKey position and the bundle name the path position, so a
// permission target's includes/excludes apply BY NAME (T-491's frozen
// semantics; nil means the channel arm fails closed).
type Authorizer interface {
	Can(ctx context.Context, p *Principal, repoKey, path, action string) bool
}

// CapabilitySource is the consumer-side management-capability seam (the
// CanManage projection, the CanManageRepo seam's sibling): the auth
// package's Service satisfies it structurally. nil fails closed — only the
// admin short-circuit inside the callers survives a missing seam.
type CapabilitySource interface {
	CanManage(ctx context.Context, p *Principal, capability auth.ManagementCapability) bool
}

// NodeChecker is the consumer-side node-resolution seam behind the
// manifest snapshot: metadata's NodeStore satisfies it structurally (the
// build.NodeChecker precedent). nil keeps every row pending — the
// metadata-less unit-stack posture, honest, never an error.
type NodeChecker interface {
	Get(ctx context.Context, repoKey, path string) (*metadata.Node, error)
}

// The service's error vocabulary. ErrForbidden is the ACL denial (403 at
// the wire); ErrFeatureOff is the entitlement refusal of the data-plane
// seam (the REST seam renders its own richer 403 — header, breaker
// wording — BEFORE the request ever reaches here; this sentinel is the
// defense-in-depth answer for internal callers); ErrBundleConflict is the
// 409 arm of the tri-state.
var (
	ErrForbidden      = errors.New("bundle: forbidden")
	ErrFeatureOff     = errors.New("bundle: the release-bundle feature is not enabled on this instance")
	ErrBundleConflict = errors.New("bundle: bundle already exists")
)

// Service is the release-bundle domain's face (M17 T-513, FR-153.1): the
// create orchestration with its conflict tri-state, the read projections
// with their dual gate, and the Any Distribution channel consumption —
// every verb of the domain flows through the same two decision points
// (CanManage for the capability arms, Can for the channel arm).
type Service struct {
	store Store
	az    Authorizer
	caps  CapabilitySource
	nodes NodeChecker
	// auditRec records the domain's audit rows best-effort (ADR-0046
	// decision 12's two words; nil = the bare unit stack).
	auditRec audit.Recorder
	// featureOn is the entitlement verdict of the data-plane seam
	// (ADR-0032 weave 2's feature-addon form, the webhook Bus Gate
	// precedent): nil or a false answer keeps every WRITE face refused —
	// a feature without a verdict is off, never on. Reads never consult
	// it (D1: data is not hostage to entitlement).
	featureOn func(ctx context.Context) bool
	// createMu serializes the create/resume path's read-modify-write (the
	// build mergeMu precedent): two concurrent creates of one pair must
	// land as create-then-resume, and a resume racing a completion must
	// not drop the state flip.
	createMu sync.Mutex
}

// Option configures the optional seams of New (the WithXxx convention).
type Option func(*Service)

// WithNodes wires the node-resolution seam the manifest snapshot resolves
// through. Without it every manifest row stays pending (INPROGRESS).
func WithNodes(n NodeChecker) Option {
	return func(s *Service) { s.nodes = n }
}

// WithAudit wires the audit recorder the bundle.create rows write through.
func WithAudit(r audit.Recorder) Option {
	return func(s *Service) { s.auditRec = r }
}

// WithFeatureGate wires the entitlement verdict (the data-plane seam).
// The closure comes from the assembly — internal/bundle never imports the
// license or addons packages — and answers "may the feature's writes
// run". nil fails CLOSED.
func WithFeatureGate(on func(ctx context.Context) bool) Option {
	return func(s *Service) { s.featureOn = on }
}

// New wires the service over the BundleStore seam: the SAME auth.Service
// instance the platform holds feeds both the channel seam (az) and the
// capability seam (caps) — passing a different one here is an assembly
// bug, not a capability; nil is the fail-closed build. The optional seams
// ride the options.
func New(store Store, az Authorizer, caps CapabilitySource, opts ...Option) *Service {
	s := &Service{store: store, az: az, caps: caps}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// canWrite is the create face's gate: CapSystemWrite (ADR-0046 decision 3
// — admin or the system-write capability; readonly_admin and every plain
// user refuse).
func (s *Service) canWrite(ctx context.Context, p *Principal) bool {
	return s.caps != nil && s.caps.CanManage(ctx, p, auth.CapSystemWrite)
}

// canRead is the read faces' dual gate: CapSystemRead ∨ the Any
// Distribution channel (decision 3). The admin short-circuit is explicit
// first (the build allow() shape); the capability arm covers
// readonly_admin; the channel arm carries the bundle name in the path
// position so includes/excludes apply by name. Both seams missing fails
// closed.
func (s *Service) canRead(ctx context.Context, p *Principal, bundleName string) bool {
	if p != nil && p.Admin {
		return true
	}
	if s.caps != nil && s.caps.CanManage(ctx, p, auth.CapSystemRead) {
		return true
	}
	if s.az == nil {
		return false
	}
	return s.az.Can(ctx, p, auth.BucketAnyDistribution, bundleName, auth.ActionRead)
}

// CreateRequest is the create face's interpreted body: the explicit
// manifest subset of the official {uuid, signature, aql} form (soft-seam
// ⑥ — AQL assembly is the T-511+ flip point; signature/uuid ride no
// M17 face).
type CreateRequest struct {
	Name    string
	Version string
	Items   []*ManifestItem
}

// CreateOutcome reports which arm of the tri-state answered.
type CreateOutcome struct {
	// Created: the 202 arm — no prior row sat at (name, version).
	Created bool
	// State after the write (COMPLETE when every row carries its
	// snapshot, INPROGRESS while some are pending).
	State string
	// Digest is the stored signature placeholder.
	Digest string
	// Pending counts the manifest rows without a snapshot.
	Pending int
}

// Create is POST /api/release/bundle (the explicit-manifest degrade of
// the official AQL-assembly face): the conflict tri-state of Errata ② E6 —
//
//	no prior row                     -> 202 (Created)
//	same digest, state != COMPLETE   -> 200 (resume: pending rows
//	                                   re-resolve, the state may flip)
//	different digest, or COMPLETE    -> 409 (ErrBundleConflict)
//
// The "same signature" arm is digest equality because no signing chain
// exists in the minimal face (E6's content-digest ruling); the digest
// covers identities only, so a resume after artifacts landed is the same
// digest while any manifest change is not.
//
// Pending-row policy (the C-layer ruling this domain owns, E6's
// "语义对位，字面自定"): a manifest row whose artifact is not on this
// instance lands PENDING (” sha256) rather than refusing — the
// record-domain analog of the official asynchronous artifact copy, whose
// §3.1 all-artifacts-exist precondition belongs to the face-out store
// endpoint. The bundle is INPROGRESS until every row carries its
// snapshot; a caller-sent sha256 pin that disagrees with the live node
// DOES refuse (a wrong pin must not be silently snapshotted).
func (s *Service) Create(ctx context.Context, p *Principal, req *CreateRequest) (*CreateOutcome, error) {
	if s.featureOn == nil || !s.featureOn(ctx) {
		return nil, ErrFeatureOff
	}
	if err := ValidateBundleName(req.Name); err != nil {
		return nil, err
	}
	if err := ValidateBundleVersion(req.Version); err != nil {
		return nil, err
	}
	if len(req.Items) == 0 {
		return nil, fmt.Errorf("the manifest is empty: %w", ErrInvalidBundle)
	}
	if len(req.Items) > maxManifestItems {
		return nil, fmt.Errorf("the manifest carries %d items (max %d): %w",
			len(req.Items), maxManifestItems, ErrInvalidBundle)
	}
	seen := make(map[string]bool, len(req.Items))
	for _, it := range req.Items {
		if it == nil {
			return nil, fmt.Errorf("the manifest carries a null row: %w", ErrInvalidBundle)
		}
		if err := validateManifestRow(it.Repo, it.Path); err != nil {
			return nil, err
		}
		if hasControl(it.Sha256) {
			return nil, fmt.Errorf("manifest row %s/%s carries a malformed sha256 pin: %w",
				it.Repo, it.Path, ErrInvalidBundle)
		}
		id := it.Repo + "/" + it.Path
		if seen[id] {
			return nil, fmt.Errorf("the manifest lists %s twice: %w", id, ErrInvalidBundle)
		}
		seen[id] = true
	}
	if !s.canWrite(ctx, p) {
		return nil, fmt.Errorf("bundle %s/%s: %w", req.Name, req.Version, ErrForbidden)
	}

	actor := actorOf(p)
	now := time.Now().UTC().Format(time.RFC3339)
	digest := manifestDigest(req.Items)

	// The whole evaluate-insert-resume path under one lock: InsertBundle
	// is the race-safe discriminator (the UNIQUE key answers the loser),
	// the lock keeps the resume's replace from racing a sibling.
	s.createMu.Lock()
	defer s.createMu.Unlock()

	rows, err := s.snapshot(ctx, actor, now, req.Items)
	if err != nil {
		return nil, err
	}
	state, pending := stateOf(rows)
	header := &metadata.Bundle{
		Name: req.Name, Version: req.Version, State: state, Signature: digest,
		CreatedBy: actor, CreatedAt: now, UpdatedBy: actor, UpdatedAt: now,
	}
	if err := s.store.InsertBundle(ctx, header, rows); err == nil {
		s.recordAudit(ctx, audit.Event{
			Actor: actor, Action: audit.ActionBundleCreate,
			Path: req.Name + "/" + req.Version,
			Detail: fmt.Sprintf(`{"outcome":"created","state":%q,"items":%d,"pending":%d}`,
				state, len(rows), pending),
		})
		return &CreateOutcome{Created: true, State: state, Digest: digest, Pending: pending}, nil
	} else if !errors.Is(err, metadata.ErrBundleExists) {
		return nil, fmt.Errorf("bundle %s/%s insert: %w", req.Name, req.Version, err)
	}

	// The conflict evaluation (E6): load the winner, compare digests and
	// state. A different digest is the UNMATCHING arm; COMPLETE is the
	// ALREADY-COMPLETED arm — both 409, the reason stays internal (the
	// wire body is the frozen generic form, never the reason).
	existing, err := s.store.GetBundle(ctx, req.Name, req.Version)
	if err != nil {
		return nil, fmt.Errorf("bundle %s/%s conflict lookup: %w", req.Name, req.Version, err)
	}
	if existing.Signature != digest || existing.State == StateComplete {
		return nil, fmt.Errorf("bundle %s/%s (%s): %w", req.Name, req.Version, existing.State, ErrBundleConflict)
	}
	stored, err := s.store.ListItems(ctx, req.Name, req.Version)
	if err != nil {
		return nil, fmt.Errorf("bundle %s/%s items read: %w", req.Name, req.Version, err)
	}
	if got := digestOfSnapshotRows(stored); got != digest {
		// The row's digest and its stored identity set disagree — a
		// store-level inconsistency, never the caller's conflict: surface
		// it as the honest fault, not a 409.
		return nil, fmt.Errorf("bundle %s/%s stored manifest digest %s disagrees with signature %s: %w",
			req.Name, req.Version, got, digest, errInternalInconsistency)
	}
	// Resume: merged = every pending row re-resolved, every snapshotted
	// row kept verbatim (the time-point snapshot never re-resolves).
	merged := mergeSnapshots(stored, s.snapshotPending(ctx, actor, now, stored))
	state, pending = stateOf(merged)
	if err := s.store.ReplaceItems(ctx, req.Name, req.Version, state, merged, actor, now); err != nil {
		return nil, fmt.Errorf("bundle %s/%s resume: %w", req.Name, req.Version, err)
	}
	s.recordAudit(ctx, audit.Event{
		Actor: actor, Action: audit.ActionBundleCreate,
		Path: req.Name + "/" + req.Version,
		Detail: fmt.Sprintf(`{"outcome":"resumed","state":%q,"items":%d,"pending":%d}`,
			state, len(merged), pending),
	})
	return &CreateOutcome{Created: false, State: state, Digest: digest, Pending: pending}, nil
}

// errInternalInconsistency marks the readback mismatch (the 500 family's
// service face; the wire maps it to the generic handler error).
var errInternalInconsistency = errors.New("bundle: stored manifest is inconsistent with its signature")

// snapshot resolves every manifest row against the live nodes table and
// mints the store rows: an existing node contributes its sha256/size
// (frozen from this moment), everything else stays pending. A caller pin
// that disagrees with the node refuses the whole create.
func (s *Service) snapshot(ctx context.Context, actor, now string, items []*ManifestItem) ([]*metadata.BundleItem, error) {
	rows := make([]*metadata.BundleItem, 0, len(items))
	for _, it := range items {
		row := &metadata.BundleItem{
			ID: newID(), RepoKey: it.Repo, Path: it.Path,
			AddedAt: now, AddedBy: actor,
		}
		if s.nodes == nil {
			rows = append(rows, row)
			continue
		}
		node, err := s.nodes.Get(ctx, it.Repo, it.Path)
		if err != nil {
			if !errors.Is(err, metadata.ErrNodeNotFound) {
				return nil, fmt.Errorf("manifest row %s/%s lookup: %w", it.Repo, it.Path, err)
			}
			rows = append(rows, row) // pending — not on this instance (yet)
			continue
		}
		if it.Sha256 != "" && node.Sha256 != "" && !strings.EqualFold(it.Sha256, node.Sha256) {
			return nil, fmt.Errorf("manifest row %s/%s pins sha256 %s but the artifact is %s: %w",
				it.Repo, it.Path, it.Sha256, node.Sha256, ErrInvalidBundle)
		}
		row.ID = newID()
		row.Sha256 = node.Sha256
		row.Size = node.Size
		rows = append(rows, row)
	}
	return rows, nil
}

// snapshotPending re-resolves only the pending rows of a stored segment
// (the resume arm): snapshotted rows keep their frozen values.
func (s *Service) snapshotPending(ctx context.Context, actor, now string, stored []*metadata.BundleItem) map[string]*metadata.BundleItem {
	fresh := make(map[string]*metadata.BundleItem)
	if s.nodes == nil {
		return fresh
	}
	for _, row := range stored {
		if row.Sha256 != "" {
			continue // frozen — the time-point snapshot never re-resolves
		}
		node, err := s.nodes.Get(ctx, row.RepoKey, row.Path)
		if err != nil || node.Sha256 == "" {
			continue // still absent (or an unreadable node): stays pending
		}
		fresh[row.RepoKey+"/"+row.Path] = &metadata.BundleItem{
			ID: newID(), RepoKey: row.RepoKey, Path: row.Path,
			Sha256: node.Sha256, Size: node.Size, AddedAt: now, AddedBy: actor,
		}
	}
	return fresh
}

// mergeSnapshots overlays the fresh resolutions onto the stored segment
// (ordered by identity — the store's read order, deterministic).
func mergeSnapshots(stored []*metadata.BundleItem, fresh map[string]*metadata.BundleItem) []*metadata.BundleItem {
	out := make([]*metadata.BundleItem, 0, len(stored))
	for _, row := range stored {
		if f, ok := fresh[row.RepoKey+"/"+row.Path]; ok {
			out = append(out, f)
			continue
		}
		out = append(out, row)
	}
	return out
}

// stateOf derives the closed-set value from the snapshot set: COMPLETE
// when every row carries a sha256, INPROGRESS while any is pending.
func stateOf(rows []*metadata.BundleItem) (state string, pending int) {
	for _, row := range rows {
		if row.Sha256 == "" {
			pending++
		}
	}
	if pending > 0 {
		return StateInProgress, pending
	}
	return StateComplete, 0
}

// newID mints the item row's uuid (the build_promotions id form).
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand refusing is unrecoverable; the hex of zeros would
		// collide — fail loudly instead.
		panic(fmt.Sprintf("bundle: uuid mint failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return hex.EncodeToString(b[:])
}

// GetBundle returns one bundle's header behind the dual read gate. A
// denied principal gets ErrForbidden — never the row, never a not-found
// masquerade (the build GetBuild posture: the denial itself is the
// answer).
func (s *Service) GetBundle(ctx context.Context, p *Principal, name, version string) (*metadata.Bundle, error) {
	if err := ValidateBundleName(name); err != nil {
		return nil, err
	}
	if err := ValidateBundleVersion(version); err != nil {
		return nil, err
	}
	if !s.canRead(ctx, p, name) {
		return nil, fmt.Errorf("bundle %s/%s: %w", name, version, ErrForbidden)
	}
	b, err := s.store.GetBundle(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("bundle %s/%s get: %w", name, version, err)
	}
	return b, nil
}

// GetBundleWithItems is the descriptor face's assembly: the gated header
// plus its manifest segment (pending rows included — they are the
// INPROGRESS face).
func (s *Service) GetBundleWithItems(ctx context.Context, p *Principal, name, version string) (*metadata.Bundle, []*metadata.BundleItem, error) {
	b, err := s.GetBundle(ctx, p, name, version)
	if err != nil {
		return nil, nil, err
	}
	items, err := s.store.ListItems(ctx, name, version)
	if err != nil {
		return nil, nil, fmt.Errorf("bundle %s/%s items: %w", name, version, err)
	}
	return b, items, nil
}

// ListBundleNames returns the names projection FILTERED TO THE VISIBLE
// SET on the server side: every row is evaluated against the dual read
// gate over its own name before it joins the answer — the zero-leak
// listing law (the build ListBuildNames precedent, NFR-S81's probe face).
func (s *Service) ListBundleNames(ctx context.Context, p *Principal) ([]*metadata.BundleName, error) {
	rows, err := s.store.ListBundleNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("bundle names list: %w", err)
	}
	out := make([]*metadata.BundleName, 0, len(rows))
	for _, r := range rows {
		if s.canRead(ctx, p, r.Name) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListBundleVersions returns every version of one name behind the same
// per-row visible-set filter — a name invisible to the caller answers
// empty (zero leakage; the wire renders the family's 404).
func (s *Service) ListBundleVersions(ctx context.Context, p *Principal, name string) ([]*metadata.BundleVersion, error) {
	if err := ValidateBundleName(name); err != nil {
		return nil, err
	}
	if !s.canRead(ctx, p, name) {
		return nil, fmt.Errorf("bundle %s versions: %w", name, ErrForbidden)
	}
	rows, err := s.store.ListBundleVersions(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("bundle %s versions list: %w", name, err)
	}
	return rows, nil
}

// actorOf renders the audit actor of a principal ("" = anonymous; the
// recorder stamps its own default).
func actorOf(p *Principal) string {
	if p == nil {
		return ""
	}
	return p.Name
}

// recordAudit writes one best-effort row (nil recorder = the bare unit
// stack, honestly silent).
func (s *Service) recordAudit(ctx context.Context, e audit.Event) {
	if s.auditRec == nil {
		return
	}
	s.auditRec.Record(ctx, e)
}
