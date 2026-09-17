// L026-6 (D08-R03/R05): the system face's contract — the release-bundles
// system repository provisioning (idempotent, racing-safe, honest no-op
// without the seam) and the cleanup-period knob's factory default.

package bundle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/metadata"
)

func newSystemFixture(t *testing.T) (context.Context, metadata.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return ctx, st
}

// TestEnsureSystemRepoNoopSeam: without the provisioning seam the call is
// an honest no-op — never an error, never a row.
func TestEnsureSystemRepoNoopSeam(t *testing.T) {
	ctx, st := newSystemFixture(t)
	svc := bundle.New(st.Bundles(), nil, nil)
	if err := svc.EnsureSystemRepo(ctx); err != nil {
		t.Fatalf("bare stack EnsureSystemRepo = %v, want nil", err)
	}
	if _, err := st.Repos().Get(ctx, bundle.SystemRepoKey); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("bare stack created a row: %v", err)
	}
}

// TestEnsureSystemRepoProvisions: the wired seam provisions the system row
// with the p48b shape (rclass local, packageType releasebundles), the
// second call is idempotent, and a pre-existing row is never rewritten.
func TestEnsureSystemRepoProvisions(t *testing.T) {
	ctx, st := newSystemFixture(t)
	svc := bundle.New(st.Bundles(), nil, nil, bundle.WithRepoStore(st.Repos()))
	if err := svc.EnsureSystemRepo(ctx); err != nil {
		t.Fatalf("EnsureSystemRepo: %v", err)
	}
	row, err := st.Repos().Get(ctx, bundle.SystemRepoKey)
	if err != nil {
		t.Fatalf("system repo get: %v", err)
	}
	if row.Type != "local" || row.PackageType != bundle.PackageTypeReleaseBundles {
		t.Fatalf("system row = %+v, want local/%s", row, bundle.PackageTypeReleaseBundles)
	}
	firstUpdate := row.UpdatedAt
	if err := svc.EnsureSystemRepo(ctx); err != nil {
		t.Fatalf("idempotent EnsureSystemRepo: %v", err)
	}
	row, err = st.Repos().Get(ctx, bundle.SystemRepoKey)
	if err != nil || row.UpdatedAt != firstUpdate {
		t.Fatalf("second call rewrote the row: err=%v updated=%q want %q", err, row.UpdatedAt, firstUpdate)
	}
}

// TestCleanupPeriodKnob: the factory default is 720 (p06) and the setter
// is the config face's PUT arm.
func TestCleanupPeriodKnob(t *testing.T) {
	_, st := newSystemFixture(t)
	svc := bundle.New(st.Bundles(), nil, nil)
	if got := svc.CleanupPeriodHours(); got != 720 { // bundle.DefaultCleanupPeriodHours, p06
		t.Fatalf("default = %d, want 720", got)
	}
	svc.SetCleanupPeriodHours(48)
	if got := svc.CleanupPeriodHours(); got != 48 {
		t.Fatalf("after set = %d, want 48", got)
	}
	// An explicit 0 is a VALUE, not the unset sentinel (the getter must not
	// fall back to the factory default).
	svc.SetCleanupPeriodHours(0)
	if got := svc.CleanupPeriodHours(); got != 0 {
		t.Fatalf("explicit zero = %d, want 0", got)
	}
}
