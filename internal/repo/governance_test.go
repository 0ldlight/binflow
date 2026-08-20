package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// T-95: the governance plane at the service layer — includes/excludes
// patterns (W12a/FR-24-AC4) and the repo quota ceiling (W26/W26b/W27,
// FR-31/GE-05). Every case runs through the public Service face, the same
// path the adapters drive, so the gates' placement inside the Put family is
// what these tests pin (the five protocol adapters carry zero changes by
// contract).

// govRepo creates one local generic repository carrying a raw config blob.
func govRepo(t TB, e *env, key, config string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: config,
	}); err != nil {
		t.Fatalf("CreateRepo(%s, %s): %v", key, config, err)
	}
}

// statusOf extracts a *repo.StatusError, failing when the error is not one.
func statusOf(t TB, err error, wantCode int) *repo.StatusError {
	t.Helper()
	if err == nil {
		t.Fatalf("error is nil, want a %d refusal", wantCode)
	}
	var se *repo.StatusError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a *repo.StatusError", err)
	}
	if se.Code != wantCode {
		t.Fatalf("StatusError code = %d, want %d (message %q)", se.Code, wantCode, se.Message)
	}
	return se
}

// usageOf reads the service usage view.
func usageOf(t TB, e *env, key string) *repo.UsageReport {
	t.Helper()
	u, err := e.svc.Usage(context.Background(), admin(), key)
	if err != nil {
		t.Fatalf("Usage(%s): %v", key, err)
	}
	return u
}

// ---- W12a: include/exclude patterns ----

// TestGovernancePatternsUpload409Download404 pins the dual-value ruling
// (repo-semantics section 9 erratum: upload 409 / download 404): an upload
// outside includesPattern or inside excludesPattern answers 409 whose
// message names the patterns; a download of an excluded path answers the
// byte-identical not-found of a missing node.
func TestGovernancePatternsUpload409Download404(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	govRepo(t, e, "pattern-local", `{"includesPattern":"**/*.jar","excludesPattern":"secret/**"}`)

	// Upload outside includes: 409 with the pattern in the message.
	_, err := e.svc.Put(ctx, admin(), "pattern-local", "a/t.txt",
		strings.NewReader("x"), storage.BlobRef{}, "")
	se := statusOf(t, err, 409)
	if !errors.Is(err, repo.ErrPatternRejected) {
		t.Errorf("include refusal does not wrap ErrPatternRejected: %v", err)
	}
	if !strings.Contains(se.Message, "**/*.jar") {
		t.Errorf("include refusal message %q does not name the pattern", se.Message)
	}

	// Upload inside excludes (even with an include hit): 409 naming the exclude.
	_, err = e.svc.Put(ctx, admin(), "pattern-local", "secret/t.jar",
		strings.NewReader("x"), storage.BlobRef{}, "")
	se = statusOf(t, err, 409)
	if !errors.Is(err, repo.ErrPatternRejected) {
		t.Errorf("exclude refusal does not wrap ErrPatternRejected: %v", err)
	}
	if !strings.Contains(se.Message, "secret/**") {
		t.Errorf("exclude refusal message %q does not name the pattern", se.Message)
	}

	// An include hit outside the exclude passes, and reads back.
	if _, err := e.svc.Put(ctx, admin(), "pattern-local", "pub/t.jar",
		strings.NewReader("j"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("allowed jar upload: %v", err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "pattern-local", "pub/t.jar"); err != nil {
		t.Fatalf("allowed jar download: %v", err)
	}

	// Download arm: configure the exclude AFTER content exists — the gate,
	// not the absence, must answer the 404 (and the wording is the ordinary
	// node-miss shape).
	govRepo(t, e, "late-exclude", `{}`)
	put(t, e, admin(), "late-exclude", "secret/x.bin", "s")
	put(t, e, admin(), "late-exclude", "pub/y.bin", "p")
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "late-exclude", Config: `{"excludesPattern":"secret/**"}`,
	}); err != nil {
		t.Fatalf("UpdateRepo(late-exclude): %v", err)
	}
	_, _, err = e.svc.Get(ctx, admin(), "late-exclude", "secret/x.bin")
	if !errors.Is(err, repo.ErrNodeNotFound) {
		t.Fatalf("excluded download error = %v, want ErrNodeNotFound", err)
	}
	if got, want := err.Error(), "node late-exclude/secret/x.bin: "+repo.ErrNodeNotFound.Error(); got != want {
		t.Errorf("excluded download wording %q, want the ordinary miss %q", got, want)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "late-exclude", "pub/y.bin"); err != nil {
		t.Fatalf("non-excluded download broke: %v", err)
	}

	// The M1~M3 regression clause: an unconfigured repository answers the
	// same operations with the historical behavior.
	mustCreateRepo(t, e, "plain-local")
	if _, err := e.svc.Put(ctx, admin(), "plain-local", "a/t.txt",
		strings.NewReader("x"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("default repo .txt upload: %v", err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "plain-local", "a/t.txt"); err != nil {
		t.Fatalf("default repo .txt download: %v", err)
	}
}

// TestGovernanceConfigValidation: quotaBytes is validated at config time —
// a negative value is a 400-shaped refusal, patterns ride the passthrough.
func TestGovernanceConfigValidation(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "bad-quota", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"quotaBytes":-1}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("negative quotaBytes error = %v, want ErrInvalidRepoConfig", err)
	}
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "bad-quota", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"quotaBytes":"big"}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("string quotaBytes error = %v, want ErrInvalidRepoConfig", err)
	}
	// Zero and valid values pass (0 = unlimited).
	for _, cfg := range []string{`{}`, `{"quotaBytes":0}`, `{"quotaBytes":1024}`} {
		if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: "ok-quota", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
			Config: cfg,
		}); err != nil {
			t.Fatalf("CreateRepo(%s): %v", cfg, err)
		}
		if err := e.svc.DeleteRepo(ctx, admin(), "ok-quota", false); err != nil {
			t.Fatalf("DeleteRepo(ok-quota): %v", err)
		}
	}
}

// ---- GE-05/W26: the quota ceiling across the Put family ----

// TestQuotaPutStreamingArm is W26's exact sequence: 800 bytes under a 1024
// ceiling land, the second 800 refused 413 with used/quota in the message,
// quota.exceeded audited, and NOTHING written (W26b's atomicity).
func TestQuotaPutStreamingArm(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	govRepo(t, e, "tiny", `{"quotaBytes":1024}`)

	body := strings.Repeat("a", 800)
	if _, err := e.svc.Put(ctx, admin(), "tiny", "a.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("first 800B upload: %v", err)
	}
	if u := usageOf(t, e, "tiny"); u.UsedBytes != 800 || u.QuotaBytes != 1024 {
		t.Fatalf("usage after first upload = %+v, want used 800 quota 1024", u)
	}

	_, err := e.svc.Put(ctx, admin(), "tiny", "b.bin",
		strings.NewReader(body), storage.BlobRef{}, "")
	se := statusOf(t, err, 413)
	if !errors.Is(err, repo.ErrQuotaExceeded) {
		t.Errorf("quota refusal does not wrap ErrQuotaExceeded: %v", err)
	}
	for _, want := range []string{"quota exceeded", "800", "1024"} {
		if !strings.Contains(se.Message, want) {
			t.Errorf("quota refusal message %q misses %q", se.Message, want)
		}
	}

	// Atomicity (W26b): the refused path never landed, the counter never
	// moved, and the audit record carries the five facts.
	if _, nerr := e.md.Nodes().Get(ctx, "tiny", "b.bin"); !errors.Is(nerr, metadata.ErrNodeNotFound) {
		t.Fatalf("refused node visible: %v", nerr)
	}
	if u := usageOf(t, e, "tiny"); u.UsedBytes != 800 {
		t.Fatalf("usage after refusal = %d, want 800", u.UsedBytes)
	}
	var quotaEvt *repo.AuditEvent
	for i, ev := range e.au.events { //nolint:gocritic // captured by index below
		if ev.Action == repo.AuditActionQuotaExceeded {
			quotaEvt = &e.au.events[i]
			break
		}
	}
	if quotaEvt == nil {
		t.Fatalf("no %s audit event recorded", repo.AuditActionQuotaExceeded)
	}
	if quotaEvt.Repo != "tiny" || quotaEvt.Path != "b.bin" || quotaEvt.Actor != "admin" {
		t.Errorf("quota audit event = %+v", quotaEvt)
	}
	for _, want := range []string{`"used":800`, `"quota":1024`, `"repo":"tiny"`, `"path":"b.bin"`, `"actor":"admin"`} {
		if !strings.Contains(quotaEvt.Detail, want) {
			t.Errorf("quota audit detail %q misses %q", quotaEvt.Detail, want)
		}
	}
}

// TestQuotaPutFamily covers the OTHER three Put faces plus docker's
// manifest publish — every landing is bounded, wherever its bytes came from
// (NFR-S23: checksum-deploy "秒传" and docker's landed-blob finalize/mount
// cannot slip past the ceiling the streaming arm enforces).
func TestQuotaPutFamily(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	govRepo(t, e, "tiny", `{"quotaBytes":1000}`)
	mustCreateRepo(t, e, "feeder")
	body := strings.Repeat("a", 800)

	// Seed a blob the ledger knows, from the unmetered feeder repo.
	if _, err := e.svc.Put(ctx, admin(), "feeder", "seed.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("feeder upload: %v", err)
	}

	// Fill the quota repo to the brim.
	if _, err := e.svc.Put(ctx, admin(), "tiny", "a.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("tiny upload: %v", err)
	}

	// PutWithOptions (the maven metadata family's face): bounded.
	_, err := e.svc.PutWithOptions(ctx, admin(), "tiny", "b.bin",
		strings.NewReader(body), storage.BlobRef{}, "", repo.PutOptions{})
	_ = statusOf(t, err, 413)

	// PutFromBlob (checksum-deploy / docker mount): bounded.
	_, err = e.svc.PutFromBlob(ctx, admin(), "tiny", "c.bin",
		storage.BlobRef{Sha256: shaOf(body)}, "")
	_ = statusOf(t, err, 413)
	if _, nerr := e.md.Nodes().Get(ctx, "tiny", "c.bin"); !errors.Is(nerr, metadata.ErrNodeNotFound) {
		t.Fatalf("checksum-deploy node visible after refusal: %v", nerr)
	}

	// PutLandedBlob (docker finalize and friends): bounded — commit a
	// session behind the service's back, then try to land it.
	sess, err := e.st.BeginSession(ctx)
	if err != nil {
		t.Fatalf("BeginSession: %v", err)
	}
	if _, err := sess.Append(ctx, strings.NewReader(body)); err != nil {
		t.Fatalf("session append: %v", err)
	}
	ref, err := sess.Commit(ctx, storage.BlobRef{})
	if err != nil {
		t.Fatalf("session commit: %v", err)
	}
	_, err = e.svc.PutLandedBlob(ctx, admin(), "tiny", "d.bin", ref, "")
	_ = statusOf(t, err, 413)
	if _, nerr := e.md.Nodes().Get(ctx, "tiny", "d.bin"); !errors.Is(nerr, metadata.ErrNodeNotFound) {
		t.Fatalf("landed node visible after refusal: %v", nerr)
	}

	// Docker push (W26c's server-side arm): the wire is bounded at the
	// BLOB uploads — layers ride PutLandedBlob (covered above) and the
	// manifest body rides Put, because the adapter lands it as a blob node
	// BEFORE PutManifest (manifest.go step 1). PutManifest itself carries no
	// quota check on purpose: the two-node layout (blobs/<hex> +
	// manifests/<hex>) would otherwise demand the ceiling cover the same
	// bytes twice and refuse a push that fits.
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "tiny-docker", Type: repo.TypeLocal, PackageType: repo.PackageDocker,
		Config: `{"quotaBytes":150}`,
	}); err != nil {
		t.Fatalf("CreateRepo(tiny-docker): %v", err)
	}
	m1Body := strings.Repeat("m", 100)
	m1 := shaOf(m1Body)
	if _, err := e.svc.Put(ctx, admin(), "tiny-docker", "app/blobs/"+m1,
		strings.NewReader(m1Body),
		storage.BlobRef{Sha256: m1}, ""); err != nil {
		t.Fatalf("first manifest body (the adapter's step 1): %v", err)
	}
	if _, err := e.svc.PutManifest(ctx, admin(), "tiny-docker", "app", m1, "v1",
		"application/vnd.docker.distribution.manifest.v2+json", 100,
		mkRefs("tiny-docker", "app", m1, digestOf("cfg1"))); err != nil {
		t.Fatalf("manifest publish over the landed body: %v", err)
	}
	m2Body := strings.Repeat("n", 100)
	m2 := shaOf(m2Body)
	_, err = e.svc.Put(ctx, admin(), "tiny-docker", "app/blobs/"+m2,
		strings.NewReader(m2Body),
		storage.BlobRef{Sha256: m2}, "")
	_ = statusOf(t, err, 413)
	// Atomic: no node, no manifest index row, no tag of the refused push.
	if _, nerr := e.md.Nodes().Get(ctx, "tiny-docker", "app/manifests/"+m2); !errors.Is(nerr, metadata.ErrNodeNotFound) {
		t.Fatalf("refused manifest node visible: %v", nerr)
	}
	if _, nerr := e.md.Docker().GetManifest(ctx, "tiny-docker", "app", m2); !errors.Is(nerr, metadata.ErrManifestNotFound) {
		t.Fatalf("refused manifest index row visible: %v", nerr)
	}
	if _, nerr := e.md.Docker().GetTag(ctx, "tiny-docker", "app", "v2"); !errors.Is(nerr, metadata.ErrTagNotFound) {
		t.Fatalf("refused tag visible: %v", nerr)
	}

	// The idempotent retransmit exemption: re-announcing bytes the
	// repository already holds changes nothing and never 413s.
	if _, err := e.svc.Put(ctx, admin(), "tiny", "a.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("idempotent retransmit at the ceiling: %v", err)
	}
}

// TestQuotaThreeStatesAndFallBack: quotaBytes 0 (the default) never
// rejects; the exact boundary passes; reads and deletes never consult the
// ceiling; deleting frees room for the next write (W27).
func TestQuotaThreeStatesAndFallBack(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	// State one — unlimited (quotaBytes absent): the M1~M3 behavior.
	mustCreateRepo(t, e, "free")
	body := strings.Repeat("a", 800)
	for _, p := range []string{"a.bin", "b.bin", "c.bin"} {
		if _, err := e.svc.Put(ctx, admin(), "free", p,
			strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
			t.Fatalf("unlimited repo upload %s: %v", p, err)
		}
	}
	if u := usageOf(t, e, "free"); u.QuotaBytes != 0 || u.UsedBytes != 2400 {
		t.Fatalf("unlimited usage = %+v, want quota 0 used 2400", u)
	}

	// State two — the exact boundary: used+delta == quota passes.
	govRepo(t, e, "edge", `{"quotaBytes":800}`)
	if _, err := e.svc.Put(ctx, admin(), "edge", "a.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("exact-boundary upload: %v", err)
	}

	// State three — full: reads and deletes stay unlimited, and the
	// counter falls back with the delete (W27).
	govRepo(t, e, "full", `{"quotaBytes":800}`)
	if _, err := e.svc.Put(ctx, admin(), "full", "a.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("full repo upload: %v", err)
	}
	if _, _, err := e.svc.Get(ctx, admin(), "full", "a.bin"); err != nil {
		t.Fatalf("read at the ceiling: %v", err)
	}
	if err := e.svc.Delete(ctx, admin(), "full", "a.bin"); err != nil {
		t.Fatalf("delete at the ceiling: %v", err)
	}
	if u := usageOf(t, e, "full"); u.UsedBytes != 0 {
		t.Fatalf("usage after delete = %d, want 0", u.UsedBytes)
	}
	if _, err := e.svc.Put(ctx, admin(), "full", "b.bin",
		strings.NewReader(body), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("re-upload after fallback: %v", err)
	}

	// Overwrite accounting: crossing the ceiling with a bigger body is
	// refused (delta math includes the row being replaced), a smaller
	// overwrite passes and lands on the reduced total.
	govRepo(t, e, "swap", `{"quotaBytes":1000}`)
	if _, err := e.svc.Put(ctx, admin(), "swap", "s.bin",
		strings.NewReader(strings.Repeat("a", 600)), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("swap upload: %v", err)
	}
	if _, err := e.svc.Put(ctx, admin(), "swap", "s.bin",
		strings.NewReader(strings.Repeat("b", 1001)), storage.BlobRef{}, ""); err == nil {
		t.Fatal("growing overwrite past the ceiling passed")
	} else {
		_ = statusOf(t, err, 413)
	}
	if _, err := e.svc.Put(ctx, admin(), "swap", "s.bin",
		strings.NewReader(strings.Repeat("c", 400)), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("shrinking overwrite: %v", err)
	}
	if u := usageOf(t, e, "swap"); u.UsedBytes != 400 {
		t.Fatalf("usage after shrinking overwrite = %d, want 400", u.UsedBytes)
	}
}

// TestUsageAccountingMatrix walks the counter through the service's write
// and delete faces: multi-file sums, overwrites, single and recursive
// deletes, folder markers (size zero, never counted).
func TestUsageAccountingMatrix(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRepo(t, e, "meter")

	put(t, e, admin(), "meter", "a.bin", strings.Repeat("a", 10))
	put(t, e, admin(), "meter", "d/b.bin", strings.Repeat("b", 20))
	put(t, e, admin(), "meter", "d/c.bin", strings.Repeat("c", 30))
	if u := usageOf(t, e, "meter"); u.UsedBytes != 60 {
		t.Fatalf("usage after three uploads = %d, want 60", u.UsedBytes)
	}

	// Folder deploy: a marker row of size zero.
	put(t, e, admin(), "meter", "d/", "")
	if u := usageOf(t, e, "meter"); u.UsedBytes != 60 {
		t.Fatalf("usage after folder deploy = %d, want 60", u.UsedBytes)
	}

	// Overwrite in place.
	put(t, e, admin(), "meter", "d/b.bin", strings.Repeat("B", 25))
	if u := usageOf(t, e, "meter"); u.UsedBytes != 65 {
		t.Fatalf("usage after overwrite = %d, want 65", u.UsedBytes)
	}

	// Single delete.
	if err := e.svc.Delete(ctx, admin(), "meter", "a.bin"); err != nil {
		t.Fatalf("delete a.bin: %v", err)
	}
	if u := usageOf(t, e, "meter"); u.UsedBytes != 55 {
		t.Fatalf("usage after delete = %d, want 55", u.UsedBytes)
	}

	// Recursive folder delete drops the subtree's total (folder row's own 0
	// included).
	if err := e.svc.Delete(ctx, admin(), "meter", "d/"); err != nil {
		t.Fatalf("delete d/: %v", err)
	}
	if u := usageOf(t, e, "meter"); u.UsedBytes != 0 {
		t.Fatalf("usage after folder delete = %d, want 0", u.UsedBytes)
	}
}

// TestUsageSameTransactionRollback: an injected failure inside the combined
// node+usage write leaves NEITHER behind — the counter cannot drift from a
// half-written node (the same-transaction contract GE-05's exactness rests
// on; "nodes.put" is the injection point the combined write kept firing).
func TestUsageSameTransactionRollback(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbDir := t.TempDir()

	base, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: filepath.Join(dbDir, "seed.db")})
	if err != nil {
		t.Fatalf("metadata.Open(seed): %v", err)
	}
	if err := base.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "generic-local", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: "{}", CreatedAt: "2026-08-18T00:00:00Z", UpdatedAt: "2026-08-18T00:00:00Z",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	var failed bool
	hooked := wrapHooks(base, func(op string) error {
		if op == "nodes.put" && !failed {
			failed = true
			return errors.New("injected node+usage write failure")
		}
		return nil
	})
	eng, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("OpenEngine: %v", err)
	}
	t.Cleanup(func() {
		_ = eng.Close()
		_ = base.Close()
	})
	svc := repo.NewWithClock(eng, hooked, nil, nil, timeUTC)

	if _, err := svc.Put(ctx, admin(), "generic-local", "acme/x.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err == nil {
		t.Fatal("Put survived the injected failure")
	}
	// Neither the node nor the counter moved.
	if _, nerr := base.Nodes().Get(ctx, "generic-local", "acme/x.bin"); !errors.Is(nerr, metadata.ErrNodeNotFound) {
		t.Fatalf("node visible after injected failure: %v", nerr)
	}
	if u, uerr := base.Usage().Get(ctx, "generic-local"); uerr != nil || u.LogicalBytes != 0 {
		t.Fatalf("usage after injected failure = %+v (%v), want 0", u, uerr)
	}
	// The retry (one-shot failure) lands both together.
	if _, err := svc.Put(ctx, admin(), "generic-local", "acme/x.bin",
		strings.NewReader("payload"), storage.BlobRef{}, ""); err != nil {
		t.Fatalf("retry after injected failure: %v", err)
	}
	if u, uerr := base.Usage().Get(ctx, "generic-local"); uerr != nil || u.LogicalBytes != int64(len("payload")) {
		t.Fatalf("usage after retry = %+v (%v), want %d", u, uerr, len("payload"))
	}
}

// ---- GE-06: the usage use case's own gate ----

// TestUsageUseCaseGate: admin passes, a read-granted principal passes, an
// ungranted one gets ErrForbidden, anonymous ErrUnauthorized, an unknown
// repository ErrRepoNotFound.
func TestUsageUseCaseGate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	govRepo(t, e, "tiny", `{"quotaBytes":1024}`)
	put(t, e, admin(), "tiny", "a.bin", strings.Repeat("a", 800))

	if _, err := e.svc.Usage(ctx, nil, "tiny"); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous usage error = %v, want ErrUnauthorized", err)
	}
	if _, err := e.svc.Usage(ctx, alice(), "tiny"); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("ungranted usage error = %v, want ErrForbidden", err)
	}
	e.az.add("alice", repo.ActionRead, "")
	u, err := e.svc.Usage(ctx, alice(), "tiny")
	if err != nil {
		t.Fatalf("read-granted usage: %v", err)
	}
	if u.RepoKey != "tiny" || u.UsedBytes != 800 || u.QuotaBytes != 1024 {
		t.Fatalf("usage = %+v, want {tiny 800 1024}", u)
	}
	if _, err := e.svc.Usage(ctx, admin(), "no-such-repo"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Fatalf("unknown repo usage error = %v, want ErrRepoNotFound", err)
	}

	// A virtual repository answers zero (writes route onto a member and
	// meter there), an unmetered empty repository zero too.
	govRepo(t, e, "member", `{}`)
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["member"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(virt): %v", err)
	}
	vu, err := e.svc.Usage(ctx, admin(), "virt")
	if err != nil {
		t.Fatalf("virtual usage: %v", err)
	}
	if vu.UsedBytes != 0 || vu.QuotaBytes != 0 {
		t.Fatalf("virtual usage = %+v, want zeroes", vu)
	}
}
