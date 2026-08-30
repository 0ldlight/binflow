package docker

import (
	"context"
	"errors"
	"fmt"
	"sort"

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
	return repoRow{key: row.RepoKey, pkg: row.PackageType, class: row.Type}, nil
}

// List enumerates every repository row in key order (the catalog's
// repository set, T-40). Unlike Get there is no not-found mapping — the
// caller treats any error as a listing fault.
func (l storeRepoLookup) List(ctx context.Context) ([]RepoRow, error) {
	rows, err := l.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker: repo row listing: %w", err)
	}
	out := make([]RepoRow, len(rows))
	for i, row := range rows {
		out[i] = repoRow{key: row.RepoKey, pkg: row.PackageType, class: row.Type}
	}
	return out, nil
}

// repoRow is the adapter-private view of one repository row.
type repoRow struct {
	key   string
	pkg   string
	class string
}

func (r repoRow) Key() string         { return r.key }
func (r repoRow) PackageType() string { return r.pkg }
func (r repoRow) Class() string       { return r.class }

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
	return repoRow{key: key, pkg: pkg}, nil
}

// List enumerates the table's rows in key order.
func (l staticRepoLookup) List(_ context.Context) ([]RepoRow, error) {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]RepoRow, len(keys))
	for i, k := range keys {
		out[i] = repoRow{key: k, pkg: l[k]}
	}
	return out, nil
}
