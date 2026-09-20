// Release-bundle system repository deletion (L027-1 c43/c55): the
// release-bundles row the store face auto-creates (bundle.EnsureSystemRepo,
// by construction outside the management plane's package-type matrix) must
// be deletable over the same REST face the reference exposes — the reference
// answers 200 to DELETE /api/repositories/release-bundles (L026-1 cleanup
// precedent). Rows of genuinely unknown package types keep the static
// refusal (defense in depth is untouched).
package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

func TestDeleteRepoReleaseBundlesSystemRow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	seed := func(key, pkg string) {
		t.Helper()
		if err := e.md.Repos().Create(ctx, &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: pkg,
			Config: "{}", CreatedAt: "2026-09-17T00:00:00Z", UpdatedAt: "2026-09-17T00:00:00Z",
		}); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	// The system population: package type releasebundles (exactly the row
	// bundle.EnsureSystemRepo writes). Deleting it succeeds — running it
	// through the matrix validation strands the auto-created row forever.
	seed(bundle.SystemRepoKey, bundle.PackageTypeReleaseBundles)
	if err := e.svc.DeleteRepo(ctx, admin(), bundle.SystemRepoKey, false); err != nil {
		t.Fatalf("DeleteRepo(release-bundles) = %v, want nil (reference deletes the system row)", err)
	}

	// A row with a genuinely unknown package type keeps the static refusal
	// (the delete-path backstop is not weakened for foreign rows).
	seed("l027q-foreign", "not-a-package-type")
	err := e.svc.DeleteRepo(ctx, admin(), "l027q-foreign", false)
	if !errors.Is(err, repo.ErrInvalidRepoType) {
		t.Fatalf("DeleteRepo(unknown package type) = %v, want ErrInvalidRepoType", err)
	}
}
