// The create face's service contract (M17 T-513 / ADR-0046 Errata ② E6):
// the conflict tri-state over the REAL 025 store — 202-arm create,
// 200-arm resume (same manifest, not COMPLETE, pending rows re-resolve,
// the state may flip), the two 409 arms (different manifest / already
// COMPLETE) — the snapshot freeze (a resolved row never re-resolves), the
// sha256-pin refusal, the validation floor and the two service gates
// (entitlement, capability).

package bundle_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/bundle"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// bundleFixture is the domain's unit stack: the real migrated store, the
// real auth.Service (both seams), the nodes seam for snapshots and a
// feature gate that is ON (the entitlement arms have their own test).
type bundleFixture struct {
	ctx     context.Context
	st      metadata.Store
	authSvc *auth.Service
	svc     *bundle.Service
}

func newBundleFixture(t *testing.T) *bundleFixture {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	// The nodes table foreign-keys into repositories — the fixture's
	// manifest rows address one seeded local repository.
	now := metadata.Now()
	if err := st.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	authSvc := auth.NewFromStore(st, false)
	svc := bundle.New(st.Bundles(), authSvc, authSvc,
		bundle.WithNodes(st.Nodes()),
		bundle.WithFeatureGate(func(context.Context) bool { return true }))
	return &bundleFixture{ctx: ctx, st: st, authSvc: authSvc, svc: svc}
}

// seedNode plants one node row (the snapshot's source of truth) with the
// blob row the nodes FK requires.
func (f *bundleFixture) seedNode(t *testing.T, repo, path, sha string, size int64) {
	t.Helper()
	if err := f.st.Blobs().Put(f.ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: metadata.Now()}); err != nil {
		t.Fatalf("seed blob %s: %v", sha, err)
	}
	now := metadata.Now()
	if err := f.st.Nodes().Put(f.ctx, &metadata.Node{
		RepoKey: repo, Path: path, Sha256: sha, Size: size,
		Mime: "application/octet-stream", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node %s/%s: %v", repo, path, err)
	}
}

// dropNode removes one node row (the pending transition's driver).
func (f *bundleFixture) dropNode(t *testing.T, repo, path string) {
	t.Helper()
	if err := f.st.Nodes().Delete(f.ctx, repo, path); err != nil {
		t.Fatalf("drop node %s/%s: %v", repo, path, err)
	}
}

// admin is the write-capable principal (EffectiveRole derives admin from
// the Admin flag).
func adminP() *auth.Principal { return &auth.Principal{Name: "root", Admin: true} }

// req is one create request over an identity set.
func req(name, version string, ids ...[2]string) *bundle.CreateRequest {
	items := make([]*bundle.ManifestItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, &bundle.ManifestItem{Repo: id[0], Path: id[1]})
	}
	return &bundle.CreateRequest{Name: name, Version: version, Items: items}
}

// TestCreateConflictTriState walks the whole ladder: fresh create (the
// 202 arm), same-manifest resume while INPROGRESS (the 200 arm — pending
// rows re-resolve, the state flips COMPLETE), then both 409 arms (same
// manifest on a COMPLETE bundle; different manifest on any bundle).
func TestCreateConflictTriState(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "app/1.0/app.jar", "aa11", 100)

	// 202 arm: one row resolves, one is not on this instance -> INPROGRESS.
	out, err := f.svc.Create(f.ctx, adminP(), req("rel-2026", "1.0",
		[2]string{"libs", "app/1.0/app.jar"}, [2]string{"libs", "app/1.0/app.pom"}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !out.Created || out.State != bundle.StateInProgress || out.Pending != 1 {
		t.Fatalf("create outcome = %+v, want created/INPROGRESS/1 pending", out)
	}

	// 200 arm precondition: land the missing artifact, resume with the
	// SAME manifest — same digest, not COMPLETE.
	f.seedNode(t, "libs", "app/1.0/app.pom", "bb22", 50)
	out, err = f.svc.Create(f.ctx, adminP(), req("rel-2026", "1.0",
		[2]string{"libs", "app/1.0/app.jar"}, [2]string{"libs", "app/1.0/app.pom"}))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if out.Created || out.State != bundle.StateComplete || out.Pending != 0 {
		t.Fatalf("resume outcome = %+v, want resumed/COMPLETE/0 pending", out)
	}
	_, items, err := f.svc.GetBundleWithItems(f.ctx, adminP(), "rel-2026", "1.0")
	if err != nil {
		t.Fatalf("descriptor read: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("manifest = %d rows, want 2", len(items))
	}
	for _, it := range items {
		if it.Sha256 == "" {
			t.Errorf("row %s/%s still pending after the resume", it.RepoKey, it.Path)
		}
	}

	// 409 arm one: the bundle is COMPLETE — even the SAME manifest refuses.
	_, err = f.svc.Create(f.ctx, adminP(), req("rel-2026", "1.0",
		[2]string{"libs", "app/1.0/app.jar"}, [2]string{"libs", "app/1.0/app.pom"}))
	if !errors.Is(err, bundle.ErrBundleConflict) {
		t.Fatalf("create on COMPLETE = %v, want ErrBundleConflict", err)
	}

	// 409 arm two: a DIFFERENT manifest (the digest moves) on the same pair.
	_, err = f.svc.Create(f.ctx, adminP(), req("rel-2026", "1.0",
		[2]string{"libs", "app/1.0/app.jar"}))
	if !errors.Is(err, bundle.ErrBundleConflict) {
		t.Fatalf("create with different manifest = %v, want ErrBundleConflict", err)
	}
}

// TestCreateResumeKeepsFrozenSnapshots: a resolved row never re-resolves —
// the resume arm re-reads only PENDING rows, so an artifact that changed
// after its snapshot keeps the release-moment checksum (the time-point
// record law).
func TestCreateResumeKeepsFrozenSnapshots(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "frozen-sha", 10)
	f.seedNode(t, "libs", "b.bin", "later-sha", 20)

	if _, err := f.svc.Create(f.ctx, adminP(), req("rel", "2", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	b, err := f.st.Bundles().GetBundle(f.ctx, "rel", "2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if b.State != bundle.StateComplete {
		t.Fatalf("single-resolved create state = %s, want COMPLETE", b.State)
	}

	// The artifact changes underneath; the stored snapshot must not move
	// (there is no resume on a COMPLETE bundle — but the readback shows
	// the frozen value, the record's own law).
	f.dropNode(t, "libs", "a.bin")
	f.seedNode(t, "libs", "a.bin", "changed-sha", 99)
	_, items, err := f.svc.GetBundleWithItems(f.ctx, adminP(), "rel", "2")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if items[0].Sha256 != "frozen-sha" || items[0].Size != 10 {
		t.Fatalf("snapshot moved: %+v, want the frozen release-moment values", items[0])
	}
}

// TestCreateResumeReResolvesOnlyPending: on a pending row the resume DOES
// re-resolve (that is the arm's whole point) and an already-resolved
// sibling keeps its snapshot.
func TestCreateResumeReResolvesOnlyPending(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)

	if _, err := f.svc.Create(f.ctx, adminP(), req("rel", "3",
		[2]string{"libs", "a.bin"}, [2]string{"libs", "b.bin"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	// While INPROGRESS, the resolved artifact changes; the pending one
	// lands. The resume must freeze a.bin at its ORIGINAL snapshot and
	// resolve b.bin fresh.
	f.dropNode(t, "libs", "a.bin")
	f.seedNode(t, "libs", "a.bin", "sha-a-LATER", 11)
	f.seedNode(t, "libs", "b.bin", "sha-b", 20)

	out, err := f.svc.Create(f.ctx, adminP(), req("rel", "3",
		[2]string{"libs", "a.bin"}, [2]string{"libs", "b.bin"}))
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if out.State != bundle.StateComplete {
		t.Fatalf("resume state = %s, want COMPLETE", out.State)
	}
	_, items, err := f.svc.GetBundleWithItems(f.ctx, adminP(), "rel", "3")
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	byPath := map[string]*metadata.BundleItem{}
	for _, it := range items {
		byPath[it.Path] = it
	}
	if got := byPath["a.bin"]; got.Sha256 != "sha-a" || got.Size != 10 {
		t.Errorf("a.bin snapshot = %s/%d, want the FROZEN sha-a/10 (never re-resolved)", got.Sha256, got.Size)
	}
	if got := byPath["b.bin"]; got.Sha256 != "sha-b" || got.Size != 20 {
		t.Errorf("b.bin snapshot = %s/%d, want the fresh sha-b/20", got.Sha256, got.Size)
	}
}

// TestCreateShaPinMismatch: a caller-pinned sha256 that disagrees with
// the live artifact refuses the whole create (a wrong pin must not be
// silently snapshotted).
func TestCreateShaPinMismatch(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	_, err := f.svc.Create(f.ctx, adminP(), &bundle.CreateRequest{
		Name:    "rel",
		Version: "4",
		Items:   []*bundle.ManifestItem{{Repo: "libs", Path: "a.bin", Sha256: "deadbeef"}},
	})
	if !errors.Is(err, bundle.ErrInvalidBundle) {
		t.Fatalf("pinned mismatch = %v, want ErrInvalidBundle", err)
	}
	if !strings.Contains(err.Error(), "pins sha256") {
		t.Errorf("mismatch wording = %q, want the pin refusal", err.Error())
	}
}

// TestCreateValidation: the 400 floor — table over bad names, bad
// versions, bad rows, duplicates and the empty manifest.
func TestCreateValidation(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	okRow := [2]string{"libs", "a.bin"}

	tests := []struct {
		name string
		req  *bundle.CreateRequest
		want string
	}{
		{"empty name", req("", "1", okRow), "name is empty"},
		{"name first char separator", req("-rel", "1", okRow), "first character alphanumeric"},
		{"name bad character", req("rel 9", "1", okRow), "alphanumerics and -_: only"},
		{"name too long", req(strings.Repeat("a", 256), "1", okRow), "exceeds 255 bytes"},
		{"empty version", req("rel", "", okRow), "version is empty"},
		{"version with slash", req("rel", "1/0", okRow), "version"},
		{"empty manifest", req("rel", "1"), "manifest is empty"},
		{"null manifest row", &bundle.CreateRequest{Name: "rel", Version: "1",
			Items: []*bundle.ManifestItem{nil}}, "null row"},
		{"row empty repo", req("rel", "1", [2]string{"", "a.bin"}), "repository is empty"},
		{"row bucket repo", req("rel", "1", [2]string{"ANY DISTRIBUTION", "a.bin"}), "wildcard bucket"},
		{"row empty path", req("rel", "1", [2]string{"libs", ""}), "path is empty"},
		{"row absolute path", req("rel", "1", [2]string{"libs", "/a.bin"}), "relative artifact path"},
		{"row traversal path", req("rel", "1", [2]string{"libs", "../a.bin"}), "'..' segment"},
		{"row dot-segment path", req("rel", "1", [2]string{"libs", "./a.bin"}), "segment"},
		{"duplicate identity", req("rel", "1", okRow, okRow), "twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.svc.Create(f.ctx, adminP(), tt.req)
			if !errors.Is(err, bundle.ErrInvalidBundle) {
				t.Fatalf("Create = %v, want ErrInvalidBundle", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("wording = %q, want it to contain %q", err.Error(), tt.want)
			}
		})
	}

	// No row may have landed from a refused create.
	if rows, err := f.st.Bundles().ListBundleNames(f.ctx); err != nil || len(rows) != 0 {
		t.Fatalf("refused creates left %d rows (err %v), want none", len(rows), err)
	}
}

// TestCreateFeatureGate: the data-plane seam — nil or a false verdict
// refuses every WRITE before validation even runs; reads keep working
// (D1, asserted by the same fixture reading the pre-existing bundle).
func TestCreateFeatureGate(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	if _, err := f.svc.Create(f.ctx, adminP(), req("rel", "5", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("create with gate on: %v", err)
	}

	off := bundle.New(f.st.Bundles(), f.authSvc, f.authSvc,
		bundle.WithFeatureGate(func(context.Context) bool { return false }))
	if _, err := off.Create(f.ctx, adminP(), req("rel", "6", [2]string{"libs", "a.bin"})); !errors.Is(err, bundle.ErrFeatureOff) {
		t.Fatalf("create with gate off = %v, want ErrFeatureOff", err)
	}
	nilGate := bundle.New(f.st.Bundles(), f.authSvc, f.authSvc) // nil = fail closed
	if _, err := nilGate.Create(f.ctx, adminP(), req("rel", "7", [2]string{"libs", "a.bin"})); !errors.Is(err, bundle.ErrFeatureOff) {
		t.Fatalf("create with nil gate = %v, want ErrFeatureOff", err)
	}
	// D1: the locked instance still READS what was written.
	if _, err := off.GetBundle(f.ctx, adminP(), "rel", "5"); err != nil {
		t.Fatalf("read with gate off: %v", err)
	}
}

// TestCreateCapabilityGate: CapSystemWrite — a plain user and a
// readonly_admin both refuse the create face (readonly_admin is read-only
// BY ROLE, the short-circuit law), the admin passes.
func TestCreateCapabilityGate(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)

	plain := &auth.Principal{Name: "dev"}
	ro := &auth.Principal{Name: "auditor", Role: auth.RoleReadOnlyAdmin}
	for _, p := range []*auth.Principal{plain, ro, nil} {
		if _, err := f.svc.Create(f.ctx, p, req("rel", "8", [2]string{"libs", "a.bin"})); !errors.Is(err, bundle.ErrForbidden) {
			t.Fatalf("create by %v = %v, want ErrForbidden", p, err)
		}
	}
	if _, err := f.svc.Create(f.ctx, adminP(), req("rel", "8", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("create by admin: %v", err)
	}
}

// TestCreateAuditRows: both arms of the tri-state write one bundle.create
// row (ADR-0046 decision 12's two-word landing — the outcome field carries
// the arm; the 409 arm writes nothing).
func TestCreateAuditRows(t *testing.T) {
	f := newBundleFixture(t)
	f.seedNode(t, "libs", "a.bin", "sha-a", 10)
	audited := bundle.New(f.st.Bundles(), f.authSvc, f.authSvc,
		bundle.WithNodes(f.st.Nodes()),
		bundle.WithFeatureGate(func(context.Context) bool { return true }),
		bundle.WithAudit(audit.BestEffort(audit.New(f.st, true))))

	if _, err := audited.Create(f.ctx, adminP(), req("rel", "9", [2]string{"libs", "a.bin"})); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := audited.Create(f.ctx, adminP(), req("rel", "9",
		[2]string{"libs", "a.bin"}, [2]string{"libs", "b.bin"})); !errors.Is(err, bundle.ErrBundleConflict) {
		t.Fatalf("different-manifest create = %v, want the conflict (no audit row)", err)
	}
	rows, err := f.st.Audits().Query(f.ctx, metadata.AuditQuery{Action: audit.ActionBundleCreate, Limit: 10})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("bundle.create rows = %d, want exactly one (the 409 arm writes nothing)", len(rows))
	}
	if rows[0].Actor != "root" || !strings.Contains(rows[0].Detail, `"outcome":"created"`) {
		t.Fatalf("row = %+v, want the admin's created outcome", rows[0])
	}
}
