package docker

import (
	"context"
	"errors"

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
// (NAME_UNKNOWN), so the caller does not need the distinction.
func (l storeRepoLookup) Get(key string) (RepoRow, error) {
	row, err := l.store.Get(context.Background(), key)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) || errors.Is(err, repo.ErrRepoNotFound) {
			return nil, nil
		}
		return nil, err
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
func (l staticRepoLookup) Get(key string) (RepoRow, error) {
	pkg, ok := l[key]
	if !ok {
		return nil, nil
	}
	return repoRow{pkg: pkg}, nil
}
