package build

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// Principal is the request identity, aliased from the auth package (the
// repo/api.go precedent): one declaration shared by both sides of the
// boundary, nil means anonymous.
type Principal = auth.Principal

// Authorizer is the consumer-side ACL seam (architecture section 3.4,
// ADR-0045 decision 4): the auth package's Service satisfies it
// structurally. The SAME assembled instance the repository domain
// consumes must be injected here — same-source by construction, the
// single-decision-point discipline (ADR-0026 decision 5 / ADR-0043
// decision 4). nil means "fail closed": only admin principals pass, the
// posture when no authorizer is wired.
type Authorizer interface {
	// Can reports whether p may perform action (r|w|d|m|a) on
	// repoKey/path. For the build domain repoKey is the build_repo
	// logical key and path is the build NAME.
	Can(ctx context.Context, p *Principal, repoKey, path, action string) bool
}

// ErrForbidden is the authorization denial of every gated face — the
// httpapi layer maps it to 403 (NFR-S80's first arm).
var ErrForbidden = errors.New("build: forbidden")

// Service is the build domain's ACL face (T-507, FR-152.1) and, since
// T-508, its write orchestration: the allow() mirror, the CanRead
// projection and the server-side visible-set filter the read faces run —
// the upload/append faces (T-508) and the promote/retention faces (T-509)
// weave onto the same mirror, so every verb of the domain flows through
// one decision point.
type Service struct {
	store Store
	az    Authorizer
	// nodes resolves the artifact association against the live nodes table
	// (upload.go's NodeChecker; nil = every artifact lands record-only).
	nodes NodeChecker
	// carrier is the repository-domain migration face promote/retention
	// consume (T-509's Carrier; nil keeps those faces at the honest
	// ErrPromoteUnavailable).
	carrier Carrier
	// docker is the docker index read face of the promotion closure walk.
	docker DockerIndex
	// props is the node-property merge seam of the promotion properties arm.
	props PropsWriter
	// auditRec records the domain's audit rows best-effort (T-509 landing
	// of ADR-0045 decision 10's +5 words; nil = the bare unit stack).
	auditRec audit.Recorder
	// mergeMu serializes the append face's read-modify-write (two
	// concurrent appends must both land — a stale-base last-writer-wins
	// would silently drop a whole merge; CI-frequency traffic makes the
	// global lock free).
	mergeMu sync.Mutex
}

// New wires the service over the BuildStore seam with the SAME
// auth.Service instance the rest of the platform holds (passing a
// different authorizer here is an assembly bug, not a capability; nil is
// the fail-closed build). The optional seams (node resolution) ride the
// options — the two-argument call every T-507 face makes is unchanged.
func New(store Store, az Authorizer, opts ...Option) *Service {
	s := &Service{store: store, az: az}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// allow mirrors repo/service.go's allow shape for shape (ADR-0045
// decision 4: p.Admin short-circuit, nil fail-closed, az.Can delegate —
// the same evaluation chain the content plane runs, so readonly_admin's
// role short-circuit and the wildcard-bucket rules apply identically).
func (s *Service) allow(ctx context.Context, p *Principal, repoKey, path, action string) bool {
	if p != nil && p.Admin {
		return true
	}
	if s.az == nil {
		return false
	}
	return s.az.Can(ctx, p, repoKey, path, action)
}

// CanRead is the public read-only projection of the same allow(read)
// decision the gated read faces run (the repo.CanRead precedent,
// ADR-0043 pt 4's row-level second stage — T-511's build-scope weave
// consumes this too).
func (s *Service) CanRead(ctx context.Context, p *Principal, buildRepo, buildName string) bool {
	return s.allow(ctx, p, buildRepo, buildName, auth.ActionRead)
}

// GetBuild returns one run's header behind the r(buildRepo, buildName)
// gate. A denied principal gets ErrForbidden — never the row, never a
// not-found masquerade (the denial itself is the answer; the row's
// existence is not a secret here because the caller named its
// coordinates, but its contents never leave). started = ” resolves the
// latest run of (name, number, repo).
func (s *Service) GetBuild(ctx context.Context, p *Principal, c Coordinate) (*metadata.Build, error) {
	c = c.Resolve()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !s.CanRead(ctx, p, c.Repo, c.Name) {
		return nil, fmt.Errorf("build %s#%s: %w", c.Name, c.Number, ErrForbidden)
	}
	b, err := s.store.GetBuild(ctx, c.Name, c.Number, c.Started, c.Repo)
	if err != nil {
		return nil, fmt.Errorf("get build %s#%s: %w", c.Name, c.Number, err)
	}
	return b, nil
}

// ListBuildNames returns the names projection (one row per
// (build_name, build_repo) with the latest started) FILTERED TO THE
// VISIBLE SET on the server side: every row is evaluated against
// allow(r) over its own (build_repo, build_name) before it joins the
// answer — the zero-leak listing law (NFR-S80's third arm; not a client
// filter, the reference's row-level build filtering posture).
func (s *Service) ListBuildNames(ctx context.Context, p *Principal) ([]*metadata.BuildName, error) {
	rows, err := s.store.ListBuildNames(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list build names: %w", err)
	}
	out := make([]*metadata.BuildName, 0, len(rows))
	for _, r := range rows {
		if s.CanRead(ctx, p, r.Repo, r.Name) {
			out = append(out, r)
		}
	}
	return out, nil
}

// ListBuildNumbers returns every run of one build name, newest first,
// behind the same per-row visible-set filter — a name the principal
// cannot read under one build_repo answers that repo's rows only, and a
// name invisible everywhere answers empty (zero leakage across repos).
func (s *Service) ListBuildNumbers(ctx context.Context, p *Principal, name string) ([]*metadata.BuildNumber, error) {
	if err := ValidateBuildName(name); err != nil {
		return nil, fmt.Errorf("list build numbers: %w", err)
	}
	rows, err := s.store.ListBuildNumbers(ctx, name, "")
	if err != nil {
		return nil, fmt.Errorf("list build numbers %s: %w", name, err)
	}
	out := make([]*metadata.BuildNumber, 0, len(rows))
	for _, r := range rows {
		if s.CanRead(ctx, p, r.Repo, name) {
			out = append(out, r)
		}
	}
	return out, nil
}
