package repo_test

// T-345's service-level legs: the delete-seam capture (four/five-tuple
// marking, layout, skip set), the disabled/gated zero-regression forms, the
// restore roundtrip (sha256 + properties + trash-marker strip), empty/clean
// zero-residue, the retention clock, the GC/cleanup immunity negative
// assertions, and the system-repository guards.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// trashAuditEvents snapshots the fake audit log's events.
func trashAuditEvents(l *auditLog) []repo.AuditEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]repo.AuditEvent, len(l.events))
	copy(out, l.events)
	return out
}

// trashEnable turns the feature on for one environment (spec defaults).
func trashEnable(t *testing.T, e *env, days int) {
	t.Helper()
	if days <= 0 {
		days = repo.TrashDefaultRetentionDays
	}
	repo.ConfigureTrash(e.svc, repo.TrashConfig{Enabled: true, RetentionDays: days})
}

// trashGet reads one node row of the can.
func trashGet(t *testing.T, e *env, path string) *metadata.Node {
	t.Helper()
	n, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, path)
	if err != nil {
		t.Fatalf("trash node %s: %v", path, err)
	}
	return n
}

// trashCountRows counts every row of the can.
func trashCountRows(t *testing.T, e *env) int {
	t.Helper()
	rows, err := e.md.Nodes().ListByPrefix(context.Background(), repo.TrashRepoKey, "")
	if err != nil {
		t.Fatalf("trash list: %v", err)
	}
	return len(rows)
}

// ---- AC1: the delete-seam capture ----

func TestTrashCaptureMarksFiveTuple(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "com/acme/lib/1.0/lib.jar", "jar-bytes")

	if err := e.svc.Delete(context.Background(), admin(), "libs", "com/acme/lib/1.0/lib.jar"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The original answers the idempotent 404; the can holds the copy at
	// the structural layout path.
	if _, err := e.md.Nodes().Get(context.Background(), "libs", "com/acme/lib/1.0/lib.jar"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("original node still present: %v", err)
	}
	n := trashGet(t, e, "libs/com/acme/lib/1.0/lib.jar")
	if n.Sha256 != shaOf("jar-bytes") {
		t.Fatalf("trash sha256 = %s, want the original content's", n.Sha256)
	}

	// The five-tuple (AC1 asserts the four; originalRepositoryType is the
	// storage-layout set's fifth).
	props, err := e.md.NodeProps().List(context.Background(), repo.TrashRepoKey, "libs/com/acme/lib/1.0/lib.jar")
	if err != nil {
		t.Fatalf("trash props: %v", err)
	}
	want := map[string]string{
		repo.PropTrashDeletedBy:              "admin",
		repo.PropTrashOriginalRepository:     "libs",
		repo.PropTrashOriginalRepositoryType: "local",
		repo.PropTrashOriginalPath:           "com/acme/lib/1.0/lib.jar",
	}
	for k, v := range want {
		got := props[k]
		if len(got) != 1 || got[0] != v {
			t.Fatalf("props[%s] = %v, want [%s]", k, got, v)
		}
	}
	if ts := props[repo.PropTrashTime]; len(ts) != 1 || ts[0] == "" {
		t.Fatalf("props[trash.time] = %v, want one epoch-ms value", ts)
	}

	// The built-in repository row materialized, local generic, and the
	// delete audit row carries the trash marker.
	row, err := e.md.Repos().Get(context.Background(), repo.TrashRepoKey)
	if err != nil {
		t.Fatalf("trash repo row: %v", err)
	}
	if row.Type != repo.TypeLocal || row.PackageType != repo.PackageGeneric {
		t.Fatalf("trash repo = %s/%s, want local/generic", row.Type, row.PackageType)
	}
	var marked bool
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionDelete && ev.Repo == "libs" && strings.Contains(ev.Detail, `"trash":true`) {
			marked = true
		}
	}
	if !marked {
		t.Fatalf("no delete audit row carries the trash marker: %+v", trashAuditEvents(e.au))
	}
}

func TestTrashCaptureFolderTree(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "app/1.0/a.bin", "aaa")
	put(t, e, admin(), "libs", "app/1.0/sub/b.bin", "bbb")

	if err := e.svc.Delete(context.Background(), admin(), "libs", "app/"); err != nil {
		t.Fatalf("Delete folder: %v", err)
	}
	// The folder row, both files and the nested folder row all landed,
	// each carrying its own originalPath.
	for _, p := range []string{"libs/app/", "libs/app/1.0/", "libs/app/1.0/a.bin", "libs/app/1.0/sub/", "libs/app/1.0/sub/b.bin"} {
		if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, p); err != nil {
			t.Fatalf("trash row %s missing: %v", p, err)
		}
	}
	props, err := e.md.NodeProps().List(context.Background(), repo.TrashRepoKey, "libs/app/1.0/sub/b.bin")
	if err != nil {
		t.Fatalf("props: %v", err)
	}
	if got := props[repo.PropTrashOriginalPath]; len(got) != 1 || got[0] != "app/1.0/sub/b.bin" {
		t.Fatalf("nested originalPath = %v", got)
	}
}

// ---- AC6: the zero-regression forms ----

func TestTrashDisabledByDefaultIsHardDelete(t *testing.T) {
	e := newEnv(t) // NO ConfigureTrash: the zero config is disabled
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")

	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := e.md.Repos().Get(context.Background(), repo.TrashRepoKey); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("trash repo materialized while disabled: %v", err)
	}
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionDelete && strings.Contains(ev.Detail, "trash") {
			t.Fatalf("disabled delete carries a trash marker: %+v", ev)
		}
	}
}

func TestTrashGateLockedKeepsHardDelete(t *testing.T) {
	e := newEnv(t)
	repo.ConfigureTrash(e.svc, repo.DefaultTrashConfig())
	repo.AttachTrashGate(e.svc, lockedGate{})
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")

	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := e.md.Repos().Get(context.Background(), repo.TrashRepoKey); !errors.Is(err, metadata.ErrRepoNotFound) {
		t.Fatalf("trash repo materialized behind a locked gate: %v", err)
	}
}

type lockedGate struct{}

func (lockedGate) Unlocked(context.Context) bool { return false }

// ---- the locally-generated skip set ----

func TestTrashSkipSet(t *testing.T) {
	cases := []struct {
		path string
		skip bool
	}{
		{".jfrog/system/file.bin", true},
		{"dists/stable/main/binary-amd64/Packages", true},
		{"dists/stable/by-hash/SHA256/abc", true},
		{"some/root/repodata/primary.xml.gz", true},
		{"_tmp_123/repodata/repomd.xml", true},
		{"com/acme/lib/maven-metadata.xml", true},
		{"com/acme/lib/maven-metadata.xml.sha1", true},
		{"com/acme/lib/1.0/acme.jar", false},
		{"pool/main/a/app/app_1.0.deb", false},
		{"packages/acme/1.0/acme-1.0.rpm", false},
		{"charts/acme-1.0.tgz", false},
	}
	for _, c := range cases {
		if got := trashSkipPathOf(t, c.path); got != c.skip {
			t.Errorf("skip(%q) = %v, want %v", c.path, got, c.skip)
		}
	}
}

// trashSkipPathOf reaches the internal predicate through the service: a
// skip-path delete leaves the can empty, a captured one does not.
func trashSkipPathOf(t *testing.T, path string) (notCaptured bool) {
	t.Helper()
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "rr")
	put(t, e, admin(), "rr", path, "x")
	if err := e.svc.Delete(context.Background(), admin(), "rr", path); err != nil {
		t.Fatalf("Delete(%s): %v", path, err)
	}
	return trashCountRows(t, e) == 0
}

// ---- AC2: the restore roundtrip ----

func TestTrashRestoreRoundtrip(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "com/acme/lib/1.0/lib.jar", "jar-bytes")
	// An original property rides the roundtrip; the trash markers must not.
	if err := e.md.NodeProps().Merge(context.Background(), "libs", "com/acme/lib/1.0/lib.jar",
		map[string][]string{"license": {"apache-2.0"}}); err != nil {
		t.Fatalf("seed props: %v", err)
	}
	if err := e.svc.Delete(context.Background(), admin(), "libs", "com/acme/lib/1.0/lib.jar"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	svc := e.svc.(repo.TrashService)
	res, err := svc.TrashRestore(context.Background(), admin(), repo.TrashRestoreRequest{
		Path: "libs/com/acme/lib/1.0/lib.jar",
	})
	if err != nil {
		t.Fatalf("TrashRestore: %v", err)
	}
	if res.HTTPStatus != 200 || res.Artifacts != 1 {
		t.Fatalf("restore result = %+v", res)
	}

	// sha256 roundtrip + property restoration + marker strip.
	n, err := e.md.Nodes().Get(context.Background(), "libs", "com/acme/lib/1.0/lib.jar")
	if err != nil {
		t.Fatalf("restored node: %v", err)
	}
	if n.Sha256 != shaOf("jar-bytes") {
		t.Fatalf("restored sha256 = %s", n.Sha256)
	}
	props, err := e.md.NodeProps().List(context.Background(), "libs", "com/acme/lib/1.0/lib.jar")
	if err != nil {
		t.Fatalf("restored props: %v", err)
	}
	if got := props["license"]; len(got) != 1 || got[0] != "apache-2.0" {
		t.Fatalf("original property lost: %v", props)
	}
	for _, k := range []string{repo.PropTrashTime, repo.PropTrashDeletedBy, repo.PropTrashOriginalRepository, repo.PropTrashOriginalPath, repo.PropTrashOriginalRepositoryType} {
		if _, ok := props[k]; ok {
			t.Fatalf("trash marker %s survived the restore: %v", k, props)
		}
	}
	// The can no longer holds the entry; an audit row landed.
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/com/acme/lib/1.0/lib.jar"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("trash entry survived the restore: %v", err)
	}
	var restored bool
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionTrashRestore {
			restored = true
		}
	}
	if !restored {
		t.Fatalf("no trash.restore audit row")
	}
}

func TestTrashRestoreFolderAndOverride(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "src")
	mustCreateRepo(t, e, "dst")
	put(t, e, admin(), "src", "app/1.0/a.bin", "aaa")
	put(t, e, admin(), "src", "app/1.0/b.bin", "bbb")

	// Delete a folder subtree; restore its CHILD subtree by its own trash
	// path — the per-node originalPath must address it exactly.
	if err := e.svc.Delete(context.Background(), admin(), "src", "app/"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	svc := e.svc.(repo.TrashService)
	if _, err := svc.TrashRestore(context.Background(), admin(), repo.TrashRestoreRequest{
		Path: "src/app/1.0/b.bin",
	}); err != nil {
		t.Fatalf("restore child: %v", err)
	}
	if n := trashGetRestored(t, e, "src", "app/1.0/b.bin"); n.Sha256 != shaOf("bbb") {
		t.Fatalf("child restore sha mismatch: %s", n.Sha256)
	}

	// The `to` override restores the remaining entry into ANOTHER
	// repository at an exact path.
	if _, err := svc.TrashRestore(context.Background(), admin(), repo.TrashRestoreRequest{
		Path:       "src/app/1.0/a.bin",
		TargetRepo: "dst",
		TargetPath: "moved/elsewhere/a.bin",
	}); err != nil {
		t.Fatalf("restore to: %v", err)
	}
	if n := trashGetRestored(t, e, "dst", "moved/elsewhere/a.bin"); n.Sha256 != shaOf("aaa") {
		t.Fatalf("override restore sha mismatch: %s", n.Sha256)
	}
}

func trashGetRestored(t *testing.T, e *env, repoKey, path string) *metadata.Node {
	t.Helper()
	n, err := e.md.Nodes().Get(context.Background(), repoKey, path)
	if err != nil {
		t.Fatalf("restored %s/%s: %v", repoKey, path, err)
	}
	return n
}

func TestTrashRestoreRefusals(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	svc := e.svc.(repo.TrashService)
	ctx := context.Background()

	// A miss answers the idempotent 404 family.
	if _, err := svc.TrashRestore(ctx, admin(), repo.TrashRestoreRequest{Path: "libs/missing.bin"}); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("restore miss = %v, want ErrNodeNotFound", err)
	}
	// The trash can is never a destination.
	if _, err := svc.TrashRestore(ctx, admin(), repo.TrashRestoreRequest{
		Path: "libs/a.bin", TargetRepo: repo.TrashRepoKey,
	}); err == nil || !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("restore into the can = %v, want ErrSystemRepo", err)
	}
	// The captured repository has been deleted since: ErrRepoNotFound.
	if err := e.md.Repos().Delete(ctx, "libs"); err != nil {
		t.Fatalf("drop repo: %v", err)
	}
	if _, err := svc.TrashRestore(ctx, admin(), repo.TrashRestoreRequest{Path: "libs/a.bin"}); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("restore to a gone repo = %v, want ErrRepoNotFound", err)
	}
	// Anonymous restore is refused.
	if _, err := svc.TrashRestore(ctx, nil, repo.TrashRestoreRequest{Path: "libs/a.bin"}); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous restore = %v, want ErrUnauthorized", err)
	}
}

// ---- AC4: empty / clean ----

func TestTrashEmptyZeroResidue(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")
	put(t, e, admin(), "libs", "b.bin", "bbb")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if err := e.svc.Delete(context.Background(), admin(), "libs", "b.bin"); err != nil {
		t.Fatalf("Delete b: %v", err)
	}
	if trashCountRows(t, e) == 0 {
		t.Fatalf("nothing captured")
	}

	svc := e.svc.(repo.TrashService)
	sum, err := svc.TrashEmpty(context.Background(), admin())
	if err != nil {
		t.Fatalf("TrashEmpty: %v", err)
	}
	if sum.Files != 2 || sum.Removed != sum.Files+sum.Folders {
		t.Fatalf("summary = %+v", sum)
	}
	if got := trashCountRows(t, e); got != 0 {
		t.Fatalf("empty left %d rows", got)
	}
	// The can itself persists (empty), and the audit row landed.
	if _, err := e.md.Repos().Get(context.Background(), repo.TrashRepoKey); err != nil {
		t.Fatalf("trash repo dropped by empty: %v", err)
	}
	var emptied bool
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionTrashEmpty {
			emptied = true
		}
	}
	if !emptied {
		t.Fatalf("no trash.empty audit row")
	}
	// A second empty is the honest zero.
	sum2, err := svc.TrashEmpty(context.Background(), admin())
	if err != nil || sum2.Removed != 0 {
		t.Fatalf("second empty = %+v %v", sum2, err)
	}
}

func TestTrashCleanOneEntry(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")
	put(t, e, admin(), "libs", "d/b.bin", "bbb")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete a: %v", err)
	}
	if err := e.svc.Delete(context.Background(), admin(), "libs", "d/"); err != nil {
		t.Fatalf("Delete d: %v", err)
	}

	svc := e.svc.(repo.TrashService)
	sum, err := svc.TrashClean(context.Background(), admin(), "libs/d/")
	if err != nil {
		t.Fatalf("TrashClean: %v", err)
	}
	if sum.Files != 1 {
		t.Fatalf("clean summary = %+v", sum)
	}
	// The other entry survives; the cleaned subtree is gone.
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/a.bin"); err != nil {
		t.Fatalf("sibling purged by clean: %v", err)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/d/b.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("cleaned subtree survived: %v", err)
	}
	if _, err := svc.TrashClean(context.Background(), admin(), "libs/missing"); !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("clean miss = %v", err)
	}
}

// ---- AC3: the retention clock ----

func TestTrashRetentionWindow(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 14)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "keep/a.bin", "aaa")
	put(t, e, admin(), "libs", "old/b.bin", "bbb")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "keep/"); err != nil {
		t.Fatalf("Delete keep: %v", err)
	}

	eng, err := repo.NewTrashEngine(repo.TrashEngineOptions{
		Store: e.md, Audit: e.au, RetentionDays: 14, Now: e.clk.Now,
	})
	if err != nil {
		t.Fatalf("NewTrashEngine: %v", err)
	}

	// Day 13: inside the window, nothing goes.
	e.clk.Advance(13 * 24 * time.Hour)
	rep, err := eng.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce(13d): %v", err)
	}
	if rep.Files != 0 || rep.Folders != 0 {
		t.Fatalf("13-day pass purged: %+v", rep)
	}
	if trashCountRows(t, e) == 0 {
		t.Fatalf("13-day pass emptied the can")
	}

	// Capture a second entry NOW (fresh trash.time), then cross the window
	// for the first one only.
	if err := e.svc.Delete(context.Background(), admin(), "libs", "old/"); err != nil {
		t.Fatalf("Delete old: %v", err)
	}
	e.clk.Advance(2 * 24 * time.Hour) // first entry is 15 days old, second 1
	rep, err = eng.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce(15d): %v", err)
	}
	if rep.Files != 1 {
		t.Fatalf("15-day pass files = %d, want 1: %+v", rep.Files, rep)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/keep/a.bin"); !errors.Is(err, metadata.ErrNodeNotFound) {
		t.Fatalf("expired entry survived: %v", err)
	}
	if _, err := e.md.Nodes().Get(context.Background(), repo.TrashRepoKey, "libs/old/b.bin"); err != nil {
		t.Fatalf("fresh entry purged inside the window: %v", err)
	}
	// The audit row landed.
	var retained bool
	for _, ev := range trashAuditEvents(e.au) {
		if ev.Action == repo.AuditActionTrashRetention && ev.Actor == repo.ActorTrashRetention {
			retained = true
		}
	}
	if !retained {
		t.Fatalf("no trash.retention audit row")
	}

	// A locked gate skips without error.
	locked, err := repo.NewTrashEngine(repo.TrashEngineOptions{
		Store: e.md, Audit: e.au, RetentionDays: 14, Now: e.clk.Now, Gate: lockedGate{},
	})
	if err != nil {
		t.Fatalf("NewTrashEngine(locked): %v", err)
	}
	rep, err = locked.RunOnce(context.Background())
	if err != nil || rep.Skipped == "" {
		t.Fatalf("locked pass = %+v %v, want a skip report", rep, err)
	}
}

// ---- AC6: the GC/cleanup immunity negative assertions ----

func TestTrashGCAndCleanupImmunity(t *testing.T) {
	dir := t.TempDir()
	dbDir := t.TempDir()
	ctx := context.Background()
	st, err := storage.OpenEngine(dir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dbDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	au := audit.New(md, true)
	clk := &clock{now: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	svc := repo.NewWithClock(st, md, nil, au, clk.Now)
	repo.ConfigureTrash(svc, repo.DefaultTrashConfig())
	adminP := &repo.Principal{Name: "admin", Admin: true}

	if _, err := svc.CreateRepo(ctx, adminP, &metadata.Repo{RepoKey: "libs", Type: repo.TypeLocal, PackageType: repo.PackageGeneric}); err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	if _, err := svc.Put(ctx, adminP, "libs", "a.bin", strings.NewReader("aaa"), storage.BlobRef{}, "application/octet-stream"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sha := shaOf("aaa")
	if err := svc.Delete(ctx, adminP, "libs", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Negative assertion 1: the GC mark set still counts the captured blob
	// as referenced (trash rows are node rows).
	live, err := repo.LiveChecksumSet(ctx, md)
	if err != nil {
		t.Fatalf("LiveChecksumSet: %v", err)
	}
	if _, ok := live[sha]; !ok {
		t.Fatalf("captured blob not in the GC mark set")
	}

	// Negative assertion 2: a full unused-cleanup APPLY run does not touch
	// the can (the policy leg walks remote repositories only; the gc leg
	// honors the mark set above). A millisecond grace (0 would fall back
	// to the 24h default) plus a pause makes the gc legs maximally hungry.
	time.Sleep(15 * time.Millisecond)
	cleanupEng, err := repo.NewCleanupEngine(repo.CleanupOptions{
		Store: md, Engine: st, Audit: au, AuditEnabled: true,
		DataDir: dir, Grace: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewCleanupEngine: %v", err)
	}
	if _, err := cleanupEng.RunOnce(ctx, repo.CleanupRunOptions{Trigger: repo.CleanupTriggerManual, Apply: true, Actor: "test"}); err != nil {
		t.Fatalf("cleanup RunOnce: %v", err)
	}
	rows, err := md.Nodes().ListByPrefix(ctx, repo.TrashRepoKey, "")
	if err != nil {
		t.Fatalf("trash list: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("unused-cleanup purged the trash can (immunity broken)")
	}

	// After a purge (empty), the blob leaves the mark set and the standing
	// gc reclaims it (grace 0 — the same engine's sweep face).
	if _, err := svc.(repo.TrashService).TrashEmpty(ctx, adminP); err != nil {
		t.Fatalf("TrashEmpty: %v", err)
	}
	live, err = repo.LiveChecksumSet(ctx, md)
	if err != nil {
		t.Fatalf("LiveChecksumSet(after): %v", err)
	}
	if _, ok := live[sha]; ok {
		t.Fatalf("emptied blob still marked live")
	}
	time.Sleep(10 * time.Millisecond)
	sweeper := st.(interface {
		GCSweep(ctx context.Context, m storage.GCMarker, grace time.Duration, apply bool) ([]string, error)
	})
	marker := &repo.CleanupMarker{Ctx: ctx, MD: md}
	deleted, err := sweeper.GCSweep(ctx, marker, time.Millisecond, true)
	if err != nil {
		t.Fatalf("GCSweep: %v", err)
	}
	found := false
	for _, d := range deleted {
		if d == sha {
			found = true
		}
	}
	if !found {
		t.Fatalf("emptied blob not reclaimed by GC: deleted=%v", deleted)
	}
}

// ---- the system-repository guards ----

func TestTrashSystemRepoGuards(t *testing.T) {
	e := newEnv(t)
	trashEnable(t, e, 0)
	mustCreateRepo(t, e, "libs")
	put(t, e, admin(), "libs", "a.bin", "aaa")
	if err := e.svc.Delete(context.Background(), admin(), "libs", "a.bin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	ctx := context.Background()

	// A user create squatting the key is refused (before or after the
	// built-in materializes).
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{RepoKey: repo.TrashRepoKey, Type: repo.TypeLocal, PackageType: repo.PackageGeneric}); !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("CreateRepo(auto-trashcan) = %v, want ErrSystemRepo", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: repo.TrashRepoKey, Description: "x"}); !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("UpdateRepo(auto-trashcan) = %v, want ErrSystemRepo", err)
	}
	if err := e.svc.DeleteRepo(ctx, admin(), repo.TrashRepoKey, true); !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("DeleteRepo(auto-trashcan) = %v, want ErrSystemRepo", err)
	}
	// Content-plane writes into the can are refused (the trash family is
	// its only writer).
	if _, err := e.svc.Put(ctx, admin(), repo.TrashRepoKey, "x.bin", strings.NewReader("x"), storage.BlobRef{}, "application/octet-stream"); !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("Put(auto-trashcan) = %v, want ErrSystemRepo", err)
	}
	if err := e.svc.Delete(ctx, admin(), repo.TrashRepoKey, "libs/a.bin"); !errors.Is(err, repo.ErrSystemRepo) {
		t.Fatalf("Delete(auto-trashcan) = %v, want ErrSystemRepo", err)
	}
	// A virtual member listing is refused (no aggregate exposure).
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "agg", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["` + repo.TrashRepoKey + `"]}`,
	}); err == nil || !strings.Contains(err.Error(), "trash can") {
		t.Fatalf("virtual member = %v, want the trash-can refusal", err)
	}
	// The captured content survived all of the above.
	if n := trashGet(t, e, "libs/a.bin"); n.Sha256 != shaOf("aaa") {
		t.Fatalf("captured content damaged: %s", n.Sha256)
	}
}

// The internal identity is the _system_ actor with the admin role.
func TestTrashSystemPrincipalShape(t *testing.T) {
	p := repo.SystemPrincipal()
	if p.Name != repo.SystemActorName || !p.Admin {
		t.Fatalf("SystemPrincipal = %+v", p)
	}
}
