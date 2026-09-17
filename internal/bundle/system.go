// L026-6 (D08-R03/R05): the release-bundle domain's system face — the
// release-bundles system repository the store endpoint auto-creates
// (release-bundle.md §2.2/§10.4 arm 5, wire p48/p48b: rclass local,
// packageType releasebundles, list-hidden) and the incomplete-cleanup
// period knob the config face reads/writes (§10.6: factory default 720
// hours, wire p06). Both ride the domain service so every REST consumer
// shares one state.

package bundle

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The system-repository literals (release-bundle.md §2.2, high confidence:
// the product's createReleaseBundlesRepoIfNotExist + the official
// "automatically created and used by default" page, both anchored by wire
// p48b; PACKAGE_TYPE_RB = "releasebundles" is the official OSS source
// constant BuildConstants.java).
const (
	// SystemRepoKey is the default storing repository every store request
	// without an explicit storingRepo resolves to.
	SystemRepoKey = "release-bundles"
	// PackageTypeReleaseBundles is the repository row's package type — the
	// marker that distinguishes a release-bundle repository from every
	// other local repository (the store face's INVALID_RB_REPO arm and the
	// repositories list's hidden rule both key on it).
	PackageTypeReleaseBundles = "releasebundles"
	// DefaultCleanupPeriodHours is the config face's factory default
	// (release-bundle.md §10.6, wire p06: {"incompleteCleanupPeriodHours": 720}).
	DefaultCleanupPeriodHours = 720
)

// RepoStore is the narrow repository-row seam the system-repo provisioning
// needs (satisfied structurally by metadata.RepoStore). The domain never
// goes through repo.Service here: the system repository is BY CONSTRUCTION
// outside the management plane's validated package-type matrix (a plain
// PUT /api/repositories with packageType releasebundles is refused there),
// so the domain owns its own row.
type RepoStore interface {
	Get(ctx context.Context, repoKey string) (*metadata.Repo, error)
	Create(ctx context.Context, r *metadata.Repo) error
}

// WithRepoStore wires the system-repo provisioning seam. Without it
// EnsureSystemRepo is an honest no-op (the bare unit stack).
func WithRepoStore(rs RepoStore) Option {
	return func(s *Service) { s.repos = rs }
}

// EnsureSystemRepo provisions the release-bundles system repository if it
// does not exist yet (the store face's default-storing-repo side effect,
// wire p48: the side effect fires BEFORE the JWS parse arm). Idempotent; a
// concurrent winner's duplicate-insert failure is re-read and treated as
// success.
func (s *Service) EnsureSystemRepo(ctx context.Context) error {
	if s.repos == nil {
		return nil // bare unit stack: no provisioning seam wired
	}
	if _, err := s.repos.Get(ctx, SystemRepoKey); err == nil {
		return nil
	} else if !errors.Is(err, metadata.ErrRepoNotFound) {
		return fmt.Errorf("bundle: system repo %s lookup: %w", SystemRepoKey, err)
	}
	now := metadata.Now()
	if err := s.repos.Create(ctx, &metadata.Repo{
		RepoKey: SystemRepoKey, Type: "local", PackageType: PackageTypeReleaseBundles,
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		// The race arm: another request created the row between the lookup
		// and the insert — the outcome the caller needs already sits there.
		if _, reread := s.repos.Get(ctx, SystemRepoKey); reread == nil {
			return nil
		}
		return fmt.Errorf("bundle: system repo %s create: %w", SystemRepoKey, err)
	}
	return nil
}

// CleanupPeriodHours returns the incomplete-bundle cleanup period (the
// config face's value; DefaultCleanupPeriodHours until a PUT changes it).
//
// ponytail: process-lifetime state — the reference persists the knob in its
// database; BinFlow's minimal face holds it in memory (no KV/settings seam
// exists in metadata yet, and a settings migration is out of this ticket's
// area). A restart resets to the factory default; move to a settings row
// when the metadata package grows one.
func (s *Service) CleanupPeriodHours() int {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	return s.cleanupHours
}

// SetCleanupPeriodHours stores the config face's PUT value (h >= 0; the
// handler validates, the setter trusts).
func (s *Service) SetCleanupPeriodHours(h int) {
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	s.cleanupHours = h
}
