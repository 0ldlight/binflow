// The promotion and retention orchestration of the build domain (M17 T-509,
// FR-152.2 / ADR-0045 decision 5 + decision 9 and Errata ④㋔㋕): the promote
// verb's w(targetRepo) ∧ r(buildRepo) gate, the cross-repository migration
// over the repository domain's OWN carriers (CopyOrMove for generic
// artifacts, the manifest-closure replay through the docker face for images —
// reuse, never a second executor), the append-only promotion history row,
// and the retention window's discard pass with its per-run deletion audit.
// The wire shapes are frozen by docs/reverse/build-info.md §2.4/§2.5 (the
// JFrog official REST reference plus the first-party OpenAPI, high
// confidence); the per-item permission ladder the carriers run is the
// operations family's own (ADR-0045 decision 9: the caller's principal rides
// the carrier, target w / source r / move d all re-checked per item).

package build

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// Carrier is the repository-domain face the promote/retention orchestration
// consumes (the CopyMoveService + docker replay + delete subset of
// repo.Service — the concrete service satisfies it structurally, the
// NodeChecker/RepoLookup precedent). nil seams answer ErrPromoteUnavailable:
// the promote faces degrade to the honest 503, never to a silent no-op.
type Carrier interface {
	// GetRepo resolves one repository configuration (ErrRepoNotFound arms
	// the promote target validation).
	GetRepo(ctx context.Context, p *Principal, repoKey string) (*metadata.Repo, error)
	// CopyOrMove is the generic-artifact migration carrier (the whole
	// five-stage pipeline: per-item permission ladder, quota, property
	// carry, trash/observer/webhook chain, audit row).
	CopyOrMove(ctx context.Context, p *Principal, req repo.CopyMoveRequest) (*repo.CopyMoveResult, error)
	// PutManifest is the docker index replay (manifest node + index row +
	// tag pointer + ref ledger; the idempotent-republish arm makes it safe
	// over a carrier-copied node).
	PutManifest(ctx context.Context, p *Principal, repoKey, image, digest, tag, mediaType string, size int64, refs []*metadata.DockerRef) (*repo.PutManifestResult, error)
	// DeleteManifest drops a source manifest's node plus its index rows
	// with the same-transaction tag/refs cascade (the move arm's docker
	// source cleanup — a moved node row alone would leave ghost tags).
	DeleteManifest(ctx context.Context, p *Principal, repoKey, image, digest string) error
	// Delete removes one node (the retention deleteBuildArtifacts leg).
	Delete(ctx context.Context, p *Principal, repoKey, path string) error
}

// DockerIndex is the docker index read face the closure walk consumes (the
// metadata.DockerStore subset — index rows, ungated: the reads feed a
// migration whose per-item gates run inside the carriers).
type DockerIndex interface {
	GetManifest(ctx context.Context, repoKey, image, digest string) (*metadata.DockerManifest, error)
	ListRefsByManifest(ctx context.Context, repoKey, image, manifestDigest string) ([]*metadata.DockerRef, error)
	ListTagsByImage(ctx context.Context, repoKey, image string) ([]*metadata.DockerTag, error)
}

// PropsWriter is the node-property merge seam (metadata.NodePropStore's
// Merge subset) the promotion body's properties arm writes through.
type PropsWriter interface {
	Merge(ctx context.Context, repoKey, path string, props map[string][]string) error
}

// ErrPromoteUnavailable marks a stack without the carrier seams (a
// metadata-less or service-less unit build): the promote faces answer the
// honest 503 instead of pretending.
var ErrPromoteUnavailable = errors.New("build: promotion carrier is not available")

// WithCarrier wires the repository-domain carrier (nil keeps the promote
// faces at ErrPromoteUnavailable; the assembly passes the SAME repo.Service
// instance every other domain holds).
func WithCarrier(c Carrier) Option { return func(s *Service) { s.carrier = c } }

// WithDocker wires the docker index read face of the closure walk.
func WithDocker(d DockerIndex) Option { return func(s *Service) { s.docker = d } }

// WithProps wires the node-property merge seam of the properties arm.
func WithProps(w PropsWriter) Option { return func(s *Service) { s.props = w } }

// WithAudit wires the best-effort audit recorder (audit.Recorder; the same
// logger every other audited surface writes through, ADR-0045 decision 10).
func WithAudit(a audit.Recorder) Option { return func(s *Service) { s.auditRec = a } }

// ---- wire shapes (build-info.md §2.4/§2.5, field set verbatim) ----

// PromotionRequest is the POST /api/build/promote/{name}/{number} body. The
// pointer fields carry the official tri-state defaults the JSON `null` /
// absent pair cannot express on a plain bool: artifacts defaults TRUE and
// failFast defaults TRUE (§2.4; the official schema marks six fields
// required — schema-generator noise, the runtime tolerates defaults, §9 #2).
type PromotionRequest struct {
	Status       string            `json:"status"`
	Comment      string            `json:"comment"`
	CiUser       string            `json:"ciUser"`
	Timestamp    string            `json:"timestamp"`
	DryRun       bool              `json:"dryRun"`
	SourceRepo   string            `json:"sourceRepo"`
	TargetRepo   string            `json:"targetRepo"`
	Copy         bool              `json:"copy"`
	Artifacts    *bool             `json:"artifacts"`
	Dependencies bool              `json:"dependencies"`
	Scopes       []string          `json:"scopes"`
	Properties   map[string]string `json:"properties"`
	FailFast     *bool             `json:"failFast"`
}

// wantArtifacts resolves the artifacts tri-state (default true).
func (r *PromotionRequest) wantArtifacts() bool { return r.Artifacts == nil || *r.Artifacts }

// wantFailFast resolves the failFast tri-state (default true).
func (r *PromotionRequest) wantFailFast() bool { return r.FailFast == nil || *r.FailFast }

// PromotionMessage is one messages[] row of the promote response (§1: level
// ∈ error|warning|info — the lowercase wire spelling, not the carrier's
// logback enum rendering).
type PromotionMessage struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// PromotionResult is the promote orchestration's outcome.
type PromotionResult struct {
	Messages []PromotionMessage `json:"messages"`
	// DryRun echoes the zero-side-effects posture of the run.
	DryRun bool `json:"-"`
	// Status is the appended promotion status ("" on a dry run).
	Status string `json:"-"`
	// StatusOnly marks the no-targetRepo arm (nothing migrated).
	StatusOnly bool `json:"-"`
	// Artifacts counts the associated artifacts that migrated (an image's
	// whole closure counts as one — the build associated the manifest).
	Artifacts int `json:"-"`
}

// msg appends one message row.
func (res *PromotionResult) msg(level, format string, args ...any) {
	res.Messages = append(res.Messages, PromotionMessage{Level: level, Message: fmt.Sprintf(format, args...)})
}

// abortf builds the failFast refusal: a per-item failure promoted to the
// request's own status. The carrier's *repo.StatusError shape rides so the
// wire layer renders the exact code and message verbatim (the operations
// passthrough posture, one error type across the seam).
func abortf(status int, format string, args ...any) error {
	return &repo.StatusError{Code: status, Message: fmt.Sprintf(format, args...)}
}

// Promote is POST /api/build/promote/{name}/{number}: one promotion of a
// build run — the status flip (one append-only history row; the CURRENT
// status is the newest row, never a mutated one) plus, when targetRepo is
// named, the cross-repository migration of the run's ASSOCIATED artifacts.
//
// Gate ladder (rejection precedes existence — no oracle for the denied):
//
//  1. body parse + the promote-specific validation (the 400 family);
//  2. w(targetRepo, "") ∧ r(buildRepo, buildName) — ADR-0045 decision 5. A
//     promotion WITHOUT targetRepo (the status-only arm, §2.4) drops the w
//     half: nothing outside the build domain is touched. body properties
//     additionally demand a(targetRepo, "") — Errata ④㋔, the official
//     target-annotate link of the properties arm;
//  3. the run must exist (404), THEN the target repository (must exist and
//     be LOCAL — virtual/remote refuse, decision 5's target ruling);
//  4. per-artifact: the carriers re-run their own ladders (source r, target
//     w, move d, quota, patterns) — promote adds the missing-artifact law on
//     top: failFast (default) refuses the whole promotion on the first
//     dangling association, otherwise the row is skipped with a warning
//     (ADR-0045 decision 5's C-layer ruling).
//
// dryRun runs every validation with ZERO side effects (no carrier execution,
// no history row, no audit). copy=false (default) MOVES: generic artifacts
// through the carrier's move arm, docker manifests through DeleteManifest's
// cascading source cleanup.
//
// dependencies=true is accepted and answered with a warning: BinFlow build
// dependencies carry no node association by design (inv-4 D1), so there is
// nothing the carrier could migrate — registered M17 divergence, §9-style.
func (s *Service) Promote(ctx context.Context, p *Principal, c Coordinate, raw []byte) (*PromotionResult, error) {
	c = c.Resolve()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Started != "" {
		started, err := NormalizeStarted(c.Started)
		if err != nil {
			return nil, err
		}
		c.Started = started
	}
	var req PromotionRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("promotion body is not valid JSON: %w: %s", ErrInvalidBuildInfo, err.Error())
	}
	if hasControl(req.Status) || hasControl(req.Comment) || hasControl(req.CiUser) ||
		hasControl(req.SourceRepo) || hasControl(req.TargetRepo) {
		return nil, fmt.Errorf("promotion body carries control characters: %w", ErrInvalidBuildInfo)
	}
	if req.Timestamp != "" {
		if _, err := NormalizeStarted(req.Timestamp); err != nil {
			return nil, fmt.Errorf("promotion timestamp: %w", err)
		}
	}
	props, err := promotionProps(req.Properties)
	if err != nil {
		return nil, err
	}

	statusOnly := req.TargetRepo == ""
	if !statusOnly && s.carrier == nil {
		return nil, fmt.Errorf("promote %s#%s: %w", c.Name, c.Number, ErrPromoteUnavailable)
	}

	// Gate 2 — before the run lookup, before the target row: a denied caller
	// learns nothing about either (NFR-S80's no-oracle law).
	if !s.CanRead(ctx, p, c.Repo, c.Name) {
		return nil, fmt.Errorf("promote %s#%s: %w", c.Name, c.Number, ErrForbidden)
	}
	if !statusOnly {
		if !s.allow(ctx, p, req.TargetRepo, "", auth.ActionWrite) {
			return nil, fmt.Errorf("promote %s#%s to %s: %w", c.Name, c.Number, req.TargetRepo, ErrForbidden)
		}
		if len(props) > 0 && !s.allow(ctx, p, req.TargetRepo, "", auth.ActionAnnotate) {
			return nil, fmt.Errorf("promote %s#%s properties: %w", c.Name, c.Number, ErrForbidden)
		}
	}

	// Gate 3 — the run, then the target repository.
	b, err := s.store.GetBuild(ctx, c.Name, c.Number, c.Started, c.Repo)
	if err != nil {
		return nil, fmt.Errorf("promote %s#%s: %w", c.Name, c.Number, err)
	}
	var tgtRow *metadata.Repo
	if !statusOnly {
		if tgtRow, err = s.carrier.GetRepo(ctx, p, req.TargetRepo); err != nil {
			if errors.Is(err, repo.ErrRepoNotFound) {
				return nil, fmt.Errorf("promotion target repository %s not found: %w", req.TargetRepo, ErrInvalidBuildInfo)
			}
			return nil, fmt.Errorf("promote %s#%s target lookup: %w", c.Name, c.Number, err)
		}
		if tgtRow.Type != repo.TypeLocal {
			return nil, fmt.Errorf("promotion target repository %s is a %s repository; the target must be a local repository: %w",
				req.TargetRepo, tgtRow.Type, ErrInvalidBuildInfo)
		}
	}

	res := &PromotionResult{DryRun: req.DryRun, StatusOnly: statusOnly, Messages: []PromotionMessage{}}
	if req.Dependencies {
		res.msg("warning", "dependencies=true has no migration effect in BinFlow: build dependencies carry no artifact association (record-only by design)")
	}
	switch {
	case statusOnly:
		res.msg("info", "Promotion of %s#%s to status '%s' completed (no target repository named; nothing migrated)",
			c.Name, c.Number, req.Status)
	case !req.wantArtifacts():
		res.msg("info", "Promotion of %s#%s to %s completed with artifacts=false (no artifacts migrated)",
			c.Name, c.Number, req.TargetRepo)
	default:
		if err := s.migrateArtifacts(ctx, p, b, &req, tgtRow, props, res); err != nil {
			return nil, err
		}
	}

	if req.DryRun {
		res.msg("info", "Dry run of promotion %s#%s to %s completed with zero side effects",
			c.Name, c.Number, targetLabel(req.TargetRepo))
		return res, nil
	}

	// The status flip: one append-only history row (the immutable `started`
	// stays the run's own; promoted_at is the caller's timestamp when it
	// named one, the server clock otherwise — the newest row IS the current
	// status, §2.4's max-by-timestamp ruling).
	promotedAt, err := promotionStamp(req.Timestamp)
	if err != nil {
		return nil, err
	}
	actor := actorOf(p)
	if err := s.store.AppendPromotion(ctx, &metadata.BuildPromotion{
		ID: promotionID(), Name: b.Name, Number: b.Number, Started: b.Started, Repo: b.Repo,
		Status: req.Status, TargetRepo: req.TargetRepo, CiUser: req.CiUser, Comment: req.Comment,
		ParamsJSON: string(raw), PromotedBy: actor, PromotedAt: promotedAt,
	}); err != nil {
		return nil, fmt.Errorf("promote %s#%s history append: %w", c.Name, c.Number, err)
	}
	res.Status = req.Status
	s.recordAudit(ctx, audit.Event{
		Actor: actor, Action: audit.ActionBuildPromote,
		Repo: promotionAuditRepo(req.TargetRepo, b.Repo), Path: b.Name,
		Detail: fmt.Sprintf(`{"build":%q,"number":%q,"target":%q,"status":%q,"dry":%t,"artifacts":%d}`,
			b.Name, b.Number, req.TargetRepo, req.Status, req.DryRun, res.Artifacts),
	})
	return res, nil
}

// migrateArtifacts runs the per-artifact ladder of the migration arm.
func (s *Service) migrateArtifacts(ctx context.Context, p *Principal, b *metadata.Build,
	req *PromotionRequest, tgtRow *metadata.Repo, props map[string][]string, res *PromotionResult) error {
	modules, err := s.store.ListModules(ctx, b.Name, b.Number, b.Started, b.Repo)
	if err != nil {
		return fmt.Errorf("promote %s#%s modules read: %w", b.Name, b.Number, err)
	}
	failFast := req.wantFailFast()
	repoRows := map[string]*metadata.Repo{}
	srcRow := func(key string) (*metadata.Repo, error) {
		if row, ok := repoRows[key]; ok {
			return row, nil
		}
		row, err := s.carrier.GetRepo(ctx, p, key)
		if err != nil {
			return nil, err
		}
		repoRows[key] = row
		return row, nil
	}

	var propTargets []string
	for _, m := range modules {
		for _, a := range m.Artifacts {
			if a.RepoKey == "" || a.Path == "" {
				// Record-only row: no live association to migrate. The
				// failFast law (decision 5): the whole promotion refuses on
				// the first dangling reference; otherwise skip + warn.
				if failFast {
					return abortf(http.StatusBadRequest,
						"module %q artifact %q has no live node association (missing or checksum-refuted at publish time); the build cannot be promoted with failFast=true",
						m.ID, a.Name)
				}
				res.msg("warning", "module %q artifact %q skipped: no live node association", m.ID, a.Name)
				continue
			}
			if a.RepoKey == req.TargetRepo {
				res.msg("info", "module %q artifact %s/%s is already in the target repository", m.ID, a.RepoKey, a.Path)
				continue
			}
			if req.SourceRepo != "" && a.RepoKey != req.SourceRepo {
				continue // the sourceRepo filter excludes this artifact's repository
			}

			// The live-node probe: the association is a REAL FK, but the
			// node may have left since publish (ON DELETE SET NULL keeps
			// the row). Missing = the dangling law above.
			if s.nodes != nil {
				if _, err := s.nodes.Get(ctx, a.RepoKey, a.Path); err != nil {
					if errors.Is(err, metadata.ErrNodeNotFound) {
						if failFast {
							return abortf(http.StatusNotFound,
								"artifact %s/%s of build %s#%s is missing from its repository; the build cannot be promoted with failFast=true",
								a.RepoKey, a.Path, b.Name, b.Number)
						}
						res.msg("warning", "artifact %s/%s skipped: the node no longer exists", a.RepoKey, a.Path)
						continue
					}
					return fmt.Errorf("promote %s#%s probe %s/%s: %w", b.Name, b.Number, a.RepoKey, a.Path, err)
				}
			}

			row, err := srcRow(a.RepoKey)
			if err != nil {
				if errors.Is(err, repo.ErrRepoNotFound) {
					if failFast {
						return abortf(http.StatusNotFound,
							"artifact %s/%s names a repository that no longer exists", a.RepoKey, a.Path)
					}
					res.msg("warning", "artifact %s/%s skipped: the source repository no longer exists", a.RepoKey, a.Path)
					continue
				}
				return fmt.Errorf("promote %s#%s source lookup %s: %w", b.Name, b.Number, a.RepoKey, err)
			}

			migrated, err := s.migrateOne(ctx, p, req, row, tgtRow, a, res)
			if err != nil {
				return err
			}
			if migrated {
				res.Artifacts++
				propTargets = append(propTargets, a.Path)
			}
		}
	}

	// The properties arm (§2.4: properties hang on the PROMOTED artifacts,
	// independent of the repository semantics; Errata ④㋔ — gated by
	// a(targetRepo) above). Applied post-migration onto each landed target
	// node; a merge failure is a warning, never a lost promotion.
	if len(props) > 0 && !req.DryRun {
		if s.props == nil {
			return fmt.Errorf("promote %s#%s properties: %w", b.Name, b.Number, ErrPromoteUnavailable)
		}
		for _, path := range propTargets {
			if err := s.props.Merge(ctx, req.TargetRepo, path, props); err != nil {
				res.msg("warning", "properties on %s/%s could not be written: %v", req.TargetRepo, path, err)
			}
		}
	}
	res.msg("info", "Promotion of %s#%s to %s completed, %d artifact(s) migrated (%s)",
		b.Name, b.Number, req.TargetRepo, res.Artifacts, moveVerb(req.Copy))
	return nil
}

// migrateOne migrates one associated artifact: the docker arm (a manifest
// path inside a docker repository — the closure replay) or the generic arm
// (the CopyOrMove carrier). Reports whether the artifact migrated.
func (s *Service) migrateOne(ctx context.Context, p *Principal,
	req *PromotionRequest, srcRow, tgtRow *metadata.Repo, a *metadata.BuildArtifact, res *PromotionResult) (bool, error) {
	if image, digest, ok := splitDockerManifestPath(a.Path); ok && srcRow.PackageType == "docker" {
		if tgtRow.PackageType != "docker" {
			return false, abortf(http.StatusBadRequest,
				"docker image %s/%s cannot be promoted to %s: the target repository's package type is %s, not docker",
				a.RepoKey, image, req.TargetRepo, tgtRow.PackageType)
		}
		return s.replayDocker(ctx, p, req, a.RepoKey, image, digest, res)
	}
	if tgtRow.PackageType == "docker" {
		return false, abortf(http.StatusBadRequest,
			"artifact %s/%s is not a docker manifest and cannot be promoted into the docker repository %s",
			a.RepoKey, a.Path, req.TargetRepo)
	}
	cm, err := s.carrier.CopyOrMove(ctx, p, repo.CopyMoveRequest{
		Op: carrierOp(req.Copy), SrcRepo: a.RepoKey, SrcPath: a.Path,
		TargetRepo: req.TargetRepo, TargetPath: a.Path,
		DryRun: req.DryRun,
	})
	if err != nil {
		return false, s.carrierFailure(req, err, res)
	}
	return foldCarrierCall(res, cm, req, "artifact "+a.RepoKey+"/"+a.Path)
}

// carrierOp renders the carrier verb of the promotion's copy flag (copy
// defaults false = move, §2.4).
func carrierOp(isCopy bool) string {
	if isCopy {
		return repo.OpCopy
	}
	return repo.OpMove
}

// dockerClosure is a manifest's transitive reference set prepared for replay.
type dockerClosure struct {
	image    string
	root     string
	manifest []*closureMember // children first, the root LAST
	blobs    []string         // unique leaf blob digests, sorted
	tags     []string         // source tags pointing at the root digest
}

// closureMember is one manifest of the closure with its source index facts.
type closureMember struct {
	digest    string
	mediaType string
	size      int64
	refs      []*metadata.DockerRef
}

// replayDocker migrates one docker manifest closure: the plan (BFS over the
// ref ledger), then — leaf blobs first through the CopyOrMove carrier
// (property carry, per-item gates), child manifests and the root LAST
// through PutManifest (node + index row + tag pointers + ref ledger, the
// docker face's own landing) so the tag pointers land over complete
// content — and for the move arm the source cleanup via DeleteManifest
// (its same-transaction tag/refs cascade is what keeps a moved image from
// leaving ghost tags behind). The sha256 reconciliation (the AC's 对账)
// verifies every landed target node against the digest the layout path
// itself names.
func (s *Service) replayDocker(ctx context.Context, p *Principal, req *PromotionRequest,
	srcRepo, image, root string, res *PromotionResult) (bool, error) {
	if s.docker == nil {
		return false, fmt.Errorf("docker promotion of %s/%s: %w", srcRepo, image, ErrPromoteUnavailable)
	}
	cl, err := s.planDockerClosure(ctx, srcRepo, image, root)
	if err != nil {
		return false, err
	}

	// Leaf blobs: one carrier call per unique digest.
	for _, blob := range cl.blobs {
		path := cl.image + "/blobs/" + blob
		cm, err := s.carrier.CopyOrMove(ctx, p, repo.CopyMoveRequest{
			Op: carrierOp(req.Copy), SrcRepo: srcRepo, SrcPath: path,
			TargetRepo: req.TargetRepo, TargetPath: path, DryRun: req.DryRun,
		})
		if err != nil {
			return false, s.carrierFailure(req, err, res)
		}
		if ok, err := foldCarrierCall(res, cm, req, "blob "+srcRepo+"/"+path); err != nil || !ok {
			return ok, err
		}
	}
	// Manifests: children first, root last. PutManifest owns the manifest
	// node AND the index rows (its two-step contract — the blob is already
	// in the global ledger the source push committed); routing the node
	// through the carrier FIRST would trip the publish's idempotent-
	// republish arm, which deliberately leaves the index row alone. The
	// ROOT additionally replays every source tag currently pointing at it.
	// (Divergence, registered: source-node PROPERTIES on a manifest do not
	// ride — the promotion body's properties arm still lands on the target
	// manifest node; the blobs' properties ride the carrier as usual.)
	if !req.DryRun {
		for i, m := range cl.manifest {
			replayTags := []string{""}
			if i == len(cl.manifest)-1 {
				replayTags = append(replayTags, cl.tags...)
			}
			for _, tag := range replayTags {
				if _, err := s.carrier.PutManifest(ctx, p, req.TargetRepo, cl.image, m.digest, tag,
					m.mediaType, m.size, m.refs); err != nil {
					return false, s.carrierFailure(req, err, res)
				}
			}
		}
	}
	// Move-arm source cleanup: the manifest node row moved with the carrier,
	// but the index rows (manifest/tag/refs) only DeleteManifest's cascade
	// clears — a move without it leaves the source serving ghost tags.
	if !req.Copy && !req.DryRun {
		for i := len(cl.manifest) - 1; i >= 0; i-- {
			m := cl.manifest[i]
			if err := s.carrier.DeleteManifest(ctx, p, srcRepo, cl.image, m.digest); err != nil {
				res.msg("warning", "source cleanup of %s/%s/manifests/%s failed: %v (the target promotion stands; the source index row lingers)",
					srcRepo, cl.image, m.digest, err)
			}
		}
	}
	// The sha256 reconciliation: every landed node's checksum must equal the
	// digest its layout path names — the manifest+blob full-migration proof.
	if !req.DryRun {
		for _, blob := range cl.blobs {
			if err := s.reconcileNode(ctx, req.TargetRepo, cl.image+"/blobs/"+blob, blob); err != nil {
				return false, err
			}
		}
		for _, m := range cl.manifest {
			if err := s.reconcileNode(ctx, req.TargetRepo, cl.image+"/manifests/"+m.digest, m.digest); err != nil {
				return false, err
			}
		}
	}
	if !req.DryRun {
		res.msg("info", "docker image %s/%s@sha256:%s promoted to %s (%d manifest(s), %d blob(s), %d tag(s))",
			srcRepo, cl.image, cl.root[:12], req.TargetRepo, len(cl.manifest), len(cl.blobs), len(cl.tags))
	}
	return !req.DryRun, nil
}

// reconcileNode verifies one landed target node's sha256 against the
// expected digest — the closure's per-node 对账 row.
func (s *Service) reconcileNode(ctx context.Context, repoKey, path, want string) error {
	if s.nodes == nil {
		return nil
	}
	n, err := s.nodes.Get(ctx, repoKey, path)
	if err != nil {
		return abortf(http.StatusInternalServerError,
			"promotion reconciliation: node %s/%s did not land: %v", repoKey, path, err)
	}
	if !strings.EqualFold(n.Sha256, want) {
		return abortf(http.StatusInternalServerError,
			"promotion reconciliation: node %s/%s landed with sha256 %s, want %s", repoKey, path, n.Sha256, want)
	}
	return nil
}

// planDockerClosure walks the manifest's transitive ref set in the SOURCE
// repository: the root manifest, every manifest-family child (index lists
// recurse), every leaf blob deduped, and the source tags currently pointing
// at the root. Children are ordered before the root (the replay's
// content-before-pointers order).
func (s *Service) planDockerClosure(ctx context.Context, srcRepo, image, root string) (*dockerClosure, error) {
	visited := map[string]bool{}
	var walk func(digest string) ([]*closureMember, error)
	walk = func(digest string) ([]*closureMember, error) {
		if visited[digest] {
			return nil, nil
		}
		visited[digest] = true
		m, err := s.docker.GetManifest(ctx, srcRepo, image, digest)
		if err != nil {
			if errors.Is(err, metadata.ErrManifestNotFound) {
				return nil, abortf(http.StatusNotFound,
					"docker manifest %s/%s@sha256:%s is missing from its repository", srcRepo, image, digest)
			}
			return nil, fmt.Errorf("docker closure %s/%s@%s: %w", srcRepo, image, digest, err)
		}
		member := &closureMember{digest: digest, mediaType: m.MediaType, size: m.Size}
		refs, err := s.docker.ListRefsByManifest(ctx, srcRepo, image, digest)
		if err != nil {
			return nil, fmt.Errorf("docker refs %s/%s@%s: %w", srcRepo, image, digest, err)
		}
		member.refs = refs
		var out []*closureMember
		for _, r := range refs {
			if isDockerManifestFamily(r.ChildMediaType) {
				children, err := walk(r.BlobDigest)
				if err != nil {
					return nil, err
				}
				out = append(out, children...)
			}
		}
		return append(out, member), nil // children first, this manifest after
	}
	members, err := walk(root)
	if err != nil {
		return nil, err
	}
	blobs := map[string]bool{}
	for _, m := range members {
		for _, r := range m.refs {
			if !isDockerManifestFamily(r.ChildMediaType) {
				blobs[r.BlobDigest] = true
			}
		}
	}
	cl := &dockerClosure{image: image, root: root, manifest: members}
	for b := range blobs {
		cl.blobs = append(cl.blobs, b)
	}
	sort.Strings(cl.blobs)
	tags, err := s.docker.ListTagsByImage(ctx, srcRepo, image)
	if err != nil {
		return nil, fmt.Errorf("docker tags %s/%s: %w", srcRepo, image, err)
	}
	for _, t := range tags {
		if t.Digest == root {
			cl.tags = append(cl.tags, t.Tag)
		}
	}
	sort.Strings(cl.tags)
	return cl, nil
}

// foldCarrierCall merges one carrier call's outcome into the promotion's
// message stream: ERROR/WARN rows ride along (level-mapped to the promote
// wire set), the carrier's own INFO summary is dropped (the promotion writes
// exactly one summary). An error-status outcome aborts under failFast (the
// first error row's own wording) and degrades to the recorded error rows
// otherwise.
func foldCarrierCall(res *PromotionResult, cm *repo.CopyMoveResult, req *PromotionRequest, what string) (bool, error) {
	if cm == nil {
		return false, nil
	}
	firstErr := ""
	for _, m := range cm.Messages {
		switch m.Level {
		case "ERROR":
			res.msg("error", "%s: %s", what, m.Message)
			if firstErr == "" {
				firstErr = m.Message
			}
		case "WARN":
			res.msg("warning", "%s: %s", what, m.Message)
		}
	}
	if cm.HTTPStatus >= 400 {
		if req.wantFailFast() {
			return false, &repo.StatusError{Code: cm.HTTPStatus, Message: firstErr}
		}
		return false, nil
	}
	return !cm.DryRun, nil
}

// carrierFailure folds a carrier FATAL error (a precheck refusal already
// carrying its exact status, or an infrastructure fault): a 4xx StatusError
// degrades to a recorded error row under failFast=false (the rest of the
// promotion may still land), everything else propagates — the *StatusError
// verbatim, an infrastructure fault as the honest wrapped 500.
func (s *Service) carrierFailure(req *PromotionRequest, err error, res *PromotionResult) error {
	var se *repo.StatusError
	if errors.As(err, &se) {
		if se.Code >= 400 && se.Code < 500 && !req.wantFailFast() {
			res.msg("error", "%s", se.Message)
			return nil
		}
		return se
	}
	return fmt.Errorf("promotion carrier: %w", err)
}

// ---- retention (§2.5) ----

// RetentionRequest is the POST /api/build/retention/{name} body, the
// official four-field set verbatim: the artifact-deletion flag, the count
// window (keep the newest N runs; <= 0 = no count window), the
// minimumBuildDate floor (an ISO8601 timestamp — builds STARTED before it
// are discardable; not a day count, the Errata ① correction), and the
// exemption list.
type RetentionRequest struct {
	DeleteBuildArtifacts         bool     `json:"deleteBuildArtifacts"`
	Count                        int      `json:"count"`
	MinimumBuildDate             string   `json:"minimumBuildDate"`
	BuildNumbersNotToBeDiscarded []string `json:"buildNumbersNotToBeDiscarded"`
}

// RetentionPlan is the VALIDATED window: the gate, the 404 and the discard
// set all decided, zero deletions run. The wire face answers from the plan
// and executes it inline (async=false) or detached (async=true — the
// official default): "setting retention" and "running the window" are one
// motion here, the async arm only moves the execution off the request path
// (BinFlow M17 carries no background retention worker; the divergence from
// the reference's durable name-level policy is registered — the 024 family
// has no retention table and the payload archive is immutable).
type RetentionPlan struct {
	name, repo string
	req        RetentionRequest
	discard    []*metadata.BuildNumber // the runs outside the window
	kept       int
}

// RetentionResult reports one executed window.
type RetentionResult struct {
	Deleted []string `json:"-"` // "name#number" per discarded run
	Kept    int      `json:"-"`
}

// PrepareRetention validates the retention request and computes the
// discard window WITHOUT deleting anything. Gate: d(buildRepo, name) before
// any lookup (no oracle); a name with no visible run answers the family's
// 404.
func (s *Service) PrepareRetention(ctx context.Context, p *Principal, name, buildRepo string, req RetentionRequest) (*RetentionPlan, error) {
	if err := ValidateBuildName(name); err != nil {
		return nil, err
	}
	if buildRepo == "" {
		buildRepo = metadata.DefaultBuildRepo
	}
	if req.Count < 0 {
		return nil, fmt.Errorf("retention count %d is negative: %w", req.Count, ErrInvalidBuildInfo)
	}
	floor := ""
	if req.MinimumBuildDate != "" {
		normalized, err := NormalizeStarted(req.MinimumBuildDate)
		if err != nil {
			return nil, fmt.Errorf("retention minimumBuildDate: %w", err)
		}
		floor = normalized
	}
	if !s.allow(ctx, p, buildRepo, name, auth.ActionDelete) {
		return nil, fmt.Errorf("retention %s: %w", name, ErrForbidden)
	}
	rows, err := s.store.ListBuildNumbers(ctx, name, buildRepo)
	if err != nil {
		return nil, fmt.Errorf("retention %s runs read: %w", name, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("retention %s: %w", name, metadata.ErrBuildNotFound)
	}
	exempt := map[string]bool{}
	for _, n := range req.BuildNumbersNotToBeDiscarded {
		exempt[n] = true
	}
	plan := &RetentionPlan{name: name, repo: buildRepo, req: req}
	for i, row := range rows { // newest first (ListBuildNumbers' contract)
		if exempt[row.Number] {
			plan.kept++
			continue
		}
		outside := false
		if floor != "" && row.Started < floor { // canonical UTC literals: lexicographic = chronological
			outside = true
		}
		if req.Count > 0 && i >= req.Count {
			outside = true
		}
		if outside {
			plan.discard = append(plan.discard, row)
		} else {
			plan.kept++
		}
	}
	return plan, nil
}

// Execute runs the prepared window: per discarded run, optionally the
// associated artifact nodes (deleteBuildArtifacts — best-effort, one slog
// row per failure, the run's deletion never blocks on an artifact), then
// the run row and its cascade, one build.delete audit row each, and the
// op-level build.retention row.
func (plan *RetentionPlan) Execute(ctx context.Context, s *Service, p *Principal) (*RetentionResult, error) {
	actor := actorOf(p)
	out := &RetentionResult{Kept: plan.kept}
	for _, run := range plan.discard {
		if plan.req.DeleteBuildArtifacts && s.carrier != nil {
			modules, err := s.store.ListModules(ctx, plan.name, run.Number, run.Started, plan.repo)
			if err != nil {
				return out, fmt.Errorf("retention %s#%s modules read: %w", plan.name, run.Number, err)
			}
			for _, m := range modules {
				for _, a := range m.Artifacts {
					if a.RepoKey == "" || a.Path == "" {
						continue
					}
					if err := s.carrier.Delete(ctx, p, a.RepoKey, a.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
						// Best-effort by design: an artifact the caller
						// cannot delete is logged and left — the run's own
						// deletion is the retention contract.
						slog.WarnContext(ctx, "build: retention artifact delete failed",
							"build", plan.name+"#"+run.Number,
							"node", a.RepoKey+"/"+a.Path, "error", err.Error())
					}
				}
			}
		}
		if err := s.store.DeleteBuild(ctx, plan.name, run.Number, run.Started, plan.repo); err != nil {
			return out, fmt.Errorf("retention %s#%s delete: %w", plan.name, run.Number, err)
		}
		out.Deleted = append(out.Deleted, plan.name+"#"+run.Number)
		s.recordAudit(ctx, audit.Event{
			Actor: actor, Action: audit.ActionBuildDelete,
			Repo: plan.repo, Path: plan.name,
			Detail: fmt.Sprintf(`{"number":%q,"started":%q,"artifacts":%t}`,
				run.Number, run.Started, plan.req.DeleteBuildArtifacts),
		})
	}
	s.recordAudit(ctx, audit.Event{
		Actor: actor, Action: audit.ActionBuildRetention,
		Repo: plan.repo, Path: plan.name,
		Detail: fmt.Sprintf(`{"count":%d,"minimumBuildDate":%q,"deleted":%d,"kept":%d}`,
			plan.req.Count, plan.req.MinimumBuildDate, len(out.Deleted), out.Kept),
	})
	return out, nil
}

// ---- helpers ----

// promotionProps validates and converts the body's properties map into the
// node-property merge shape (one value per key — the promotion body's
// map<string,string> form, §2.4).
func promotionProps(wire map[string]string) (map[string][]string, error) {
	if len(wire) == 0 {
		return nil, nil
	}
	if len(wire) > metadata.MaxNodePropKeys {
		return nil, fmt.Errorf("promotion carries %d property keys (max %d): %w",
			len(wire), metadata.MaxNodePropKeys, ErrInvalidBuildInfo)
	}
	out := make(map[string][]string, len(wire))
	for k, v := range wire {
		out[k] = []string{v}
	}
	if err := metadata.ValidatePropSet(out); err != nil {
		return nil, fmt.Errorf("promotion properties: %w: %s", ErrInvalidBuildInfo, err.Error())
	}
	return out, nil
}

// promotionStamp renders the promotion's timestamp: the caller's normalized
// literal when it named one, the server clock otherwise.
func promotionStamp(ts string) (string, error) {
	if ts == "" {
		return time.Now().UTC().Format(time.RFC3339), nil
	}
	return NormalizeStarted(ts)
}

// promotionID mints one history-row id (the append-only ledger's key; any
// unique spelling serves — a random 128-bit hex keeps it collision-free
// without a sequence).
func promotionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on the supported platforms; degrade to a
		// time-keyed spelling rather than losing the promotion over an id.
		return fmt.Sprintf("prom-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// recordAudit writes one best-effort row (nil recorder = the bare unit
// stack: the business operation stands, the trail does not).
func (s *Service) recordAudit(ctx context.Context, e audit.Event) {
	if s.auditRec == nil {
		return
	}
	s.auditRec.Record(ctx, e)
}

// promotionAuditRepo picks the audit row's repo coordinate: the target when
// one was named (the operation's destination), the build repo otherwise.
func promotionAuditRepo(target, buildRepo string) string {
	if target != "" {
		return target
	}
	return buildRepo
}

// moveVerb renders the migration verb for the summary line.
func moveVerb(isCopy bool) string {
	if isCopy {
		return "copied"
	}
	return "moved"
}

// targetLabel renders the target for the dry-run summary.
func targetLabel(target string) string {
	if target == "" {
		return "(status only)"
	}
	return target
}

// splitDockerManifestPath splits a docker layout manifest path
// "<image>/manifests/<hex64>" into its parts (the image may carry '/' of its
// own; the digest must be bare lowercase-or-uppercase hex — the layout's own
// spelling rules decide, no rewriting).
func splitDockerManifestPath(path string) (image, digest string, ok bool) {
	i := strings.LastIndex(path, "/manifests/")
	if i <= 0 {
		return "", "", false
	}
	image, digest = path[:i], path[i+len("/manifests/"):]
	if image == "" || !isHex64(digest) {
		return "", "", false
	}
	return image, digest, true
}

// isHex64 reports whether s is exactly 64 hex digits.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isHex := '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
		if !isHex {
			return false
		}
	}
	return true
}

// isDockerManifestFamily reports whether a child media type names a manifest
// or index (the closure recurses into these; everything else is a leaf
// blob). The four spellings mirror the docker adapter's own set — the
// deliberate duplication of the layout constants (the manifestNodePath
// precedent: exporting one string is not worth widening the seam).
func isDockerManifestFamily(mediaType string) bool {
	switch mediaType {
	case "application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.oci.image.index.v1+json":
		return true
	}
	return false
}
