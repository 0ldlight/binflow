package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// storeRepoLookup adapts metadata's RepoStore to the adapter's RepoLookup
// seam. It is the production wiring; tests inject their own stubs.
type storeRepoLookup struct {
	store metadata.RepoStore
}

// NewRepoLookup wraps a metadata RepoStore.
func NewRepoLookup(store metadata.RepoStore) RepoLookup {
	return storeRepoLookup{store: store}
}

// Get resolves the key, mapping not-found onto (nil, nil) — the docker
// plane treats a missing row and a wrong package type identically
// (NAME_UNKNOWN), so the caller does not need the distinction. Every
// other error propagates: the caller (T-33 review B2) logs it and answers
// a spec-body 500, never a disguised NAME_UNKNOWN.
func (l storeRepoLookup) Get(ctx context.Context, key string) (RepoRow, error) {
	row, err := l.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) || errors.Is(err, repo.ErrRepoNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("docker: repo row lookup for %q: %w", key, err)
	}
	if row == nil {
		return nil, nil
	}
	return repoRow{pkg: row.PackageType}, nil
}

// repoRow is the adapter-private view of one repository row.
type repoRow struct{ pkg string }

func (r repoRow) PackageType() string { return r.pkg }

// staticRepoLookup is a fixed table (assembly shims and tests).
type staticRepoLookup map[string]string

// NewStaticRepoLookup builds a RepoLookup from a package-type table.
func NewStaticRepoLookup(table map[string]string) RepoLookup { return staticRepoLookup(table) }

// Get resolves the key or yields (nil, nil) for unknown keys.
func (l staticRepoLookup) Get(_ context.Context, key string) (RepoRow, error) {
	pkg, ok := l[key]
	if !ok {
		return nil, nil
	}
	return repoRow{pkg: pkg}, nil
}
