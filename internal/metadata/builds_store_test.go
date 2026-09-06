// The build-info table family's store faces (024, M17 T-507 /
// ADR-0045 decision 2 + Errata): the four-tuple run identity, the
// latest-run resolution, the projections, the module segment's atomic
// replace, the REAL nodes association with its record-only NULL shape,
// the append-only promotions, the property set semantics, the cascade
// chain, and the migration's reopen idempotency.

package metadata_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

const (
	stampA = "2026-09-07T10:00:00.000+0000"
	stampB = "2026-09-07T11:30:00.000+0000"
)

func putBuildRow(t *testing.T, st metadata.Store, b *metadata.Build) {
	t.Helper()
	if err := st.Builds().PutBuild(context.Background(), b); err != nil {
		t.Fatalf("builds put %s#%s: %v", b.Name, b.Number, err)
	}
}

func TestBuildsStorePutGetLatestRun(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)

	// Same name+number, two DIFFERENT started stamps: two runs (the
	// four-tuple, ADR-0045 Errata ④㋑).
	putBuildRow(t, st, &metadata.Build{
		Name: "myapp", Number: "51", Started: stampA, Type: "MAVEN",
		Payload:   `{"name":"myapp","number":"51"}`,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})
	putBuildRow(t, st, &metadata.Build{
		Name: "myapp", Number: "51", Started: stampB, Type: "MAVEN",
		Payload:   `{"name":"myapp","number":"51","run":2}`,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})

	got, err := st.Builds().GetBuild(ctx, "myapp", "51", stampA, "")
	if err != nil {
		t.Fatalf("get exact run: %v", err)
	}
	if got.Started != stampA || got.Payload != `{"name":"myapp","number":"51"}` {
		t.Errorf("exact get = started %q payload %q, want the first run", got.Started, got.Payload)
	}

	// started = '' resolves the LATEST run (the single-build GET default).
	got, err = st.Builds().GetBuild(ctx, "myapp", "51", "", "")
	if err != nil {
		t.Fatalf("get latest run: %v", err)
	}
	if got.Started != stampB || got.Payload != `{"name":"myapp","number":"51","run":2}` {
		t.Errorf("latest get = started %q, want %q (MAX started)", got.Started, stampB)
	}

	// The empty repo coordinate normalizes to the default logical key.
	got, err = st.Builds().GetBuild(ctx, "myapp", "51", stampA, "artifactory-build-info")
	if err != nil {
		t.Fatalf("get with explicit default repo: %v", err)
	}
	if got.Repo != metadata.DefaultBuildRepo {
		t.Errorf("repo = %q, want the default logical key", got.Repo)
	}

	if _, err := st.Builds().GetBuild(ctx, "myapp", "52", "", ""); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Errorf("get(missing number) = %v, want ErrBuildNotFound", err)
	}
	if _, err := st.Builds().GetBuild(ctx, "myapp", "51", "1999-01-01T00:00:00.000+0000", ""); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Errorf("get(missing started) = %v, want ErrBuildNotFound", err)
	}
}

func TestBuildsStorePutUpsertKeepsCreated(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	later := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	putBuildRow(t, st, &metadata.Build{
		Name: "ci", Number: "7", Started: stampA, Type: "GENERIC",
		CreatedBy: "first-ci", CreatedAt: now, UpdatedBy: "first-ci", UpdatedAt: now,
	})
	// The PUT overwrite: same four-tuple, new type/payload/stamps; the
	// creation bookkeeping survives (the run is re-published, not born
	// again — the schedules Put law).
	putBuildRow(t, st, &metadata.Build{
		Name: "ci", Number: "7", Started: stampA, Type: "GRADLE", Payload: `{"v":2}`,
		CreatedBy: "must-not-apply", CreatedAt: "1999-01-01T00:00:00Z",
		UpdatedBy: "second-ci", UpdatedAt: later,
	})
	got, err := st.Builds().GetBuild(ctx, "ci", "7", stampA, "")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Type != "GRADLE" || got.Payload != `{"v":2}` || got.UpdatedBy != "second-ci" {
		t.Errorf("upserted row = %+v, want the replacement columns", got)
	}
	if got.CreatedBy != "first-ci" || got.CreatedAt != now {
		t.Errorf("created = %q/%q, want the ORIGINAL creation stamp kept", got.CreatedBy, got.CreatedAt)
	}

	// An empty Repo on write normalizes to the default (never repo='').
	if got.Repo != metadata.DefaultBuildRepo {
		t.Errorf("repo after empty-write = %q, want %q", got.Repo, metadata.DefaultBuildRepo)
	}
}

func TestBuildsStoreFourTupleSeparatesRuns(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)

	// Identical (name, number, started) under two build_repo keys: two
	// distinct runs — the four-tuple is the identity, the repo its fourth
	// element.
	putBuildRow(t, st, &metadata.Build{
		Name: "shared", Number: "1", Started: stampA,
		CreatedBy: "a", CreatedAt: now, UpdatedBy: "a", UpdatedAt: now,
	})
	putBuildRow(t, st, &metadata.Build{
		Name: "shared", Number: "1", Started: stampA, Repo: "team-build-info",
		CreatedBy: "b", CreatedAt: now, UpdatedBy: "b", UpdatedAt: now,
	})

	for _, repo := range []string{metadata.DefaultBuildRepo, "team-build-info"} {
		if _, err := st.Builds().GetBuild(ctx, "shared", "1", stampA, repo); err != nil {
			t.Fatalf("get under repo %q: %v", repo, err)
		}
	}

	// The unfiltered numbers projection spans both repos, each row
	// carrying its own repo (the visible-set walk's input shape).
	numbers, err := st.Builds().ListBuildNumbers(ctx, "shared", "")
	if err != nil {
		t.Fatalf("list numbers: %v", err)
	}
	if len(numbers) != 2 {
		t.Fatalf("numbers = %d rows, want 2 (one per repo)", len(numbers))
	}
	seen := map[string]bool{}
	for _, n := range numbers {
		seen[n.Repo] = true
	}
	if !seen[metadata.DefaultBuildRepo] || !seen["team-build-info"] {
		t.Errorf("repos in numbers = %v, want both logical keys", seen)
	}

	// The repo-filtered projection narrows to one.
	only, err := st.Builds().ListBuildNumbers(ctx, "shared", "team-build-info")
	if err != nil {
		t.Fatalf("list numbers (repo): %v", err)
	}
	if len(only) != 1 || only[0].Repo != "team-build-info" {
		t.Fatalf("repo-filtered numbers = %+v, want the one team row", only)
	}
}

func TestBuildsStoreNamesProjection(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)

	seed := []metadata.Build{
		{Name: "beta", Number: "3", Started: stampA},
		{Name: "alpha", Number: "1", Started: stampA},
		{Name: "alpha", Number: "2", Started: stampB},                          // alpha's LAST run
		{Name: "alpha", Number: "9", Started: stampA, Repo: "team-build-info"}, // another repo's alpha
	}
	for i := range seed {
		b := seed[i]
		b.CreatedBy, b.CreatedAt, b.UpdatedBy, b.UpdatedAt = "ci", now, "ci", now
		putBuildRow(t, st, &b)
	}

	names, err := st.Builds().ListBuildNames(ctx, "")
	if err != nil {
		t.Fatalf("list names: %v", err)
	}
	want := []metadata.BuildName{
		{Name: "alpha", Repo: metadata.DefaultBuildRepo, LastStarted: stampB},
		{Name: "alpha", Repo: "team-build-info", LastStarted: stampA},
		{Name: "beta", Repo: metadata.DefaultBuildRepo, LastStarted: stampA},
	}
	if len(names) != len(want) {
		t.Fatalf("names = %+v, want %+v", names, want)
	}
	for i, n := range names {
		if *n != want[i] {
			t.Errorf("names[%d] = %+v, want %+v (grouped by name+repo, MAX(started), ordered)", i, *n, want[i])
		}
	}

	only, err := st.Builds().ListBuildNames(ctx, metadata.DefaultBuildRepo)
	if err != nil {
		t.Fatalf("list names (repo): %v", err)
	}
	if len(only) != 2 || only[0].Name != "alpha" || only[0].LastStarted != stampB {
		t.Fatalf("repo-filtered names = %+v, want alpha(stampB)+beta of the default repo", only)
	}
}

func TestBuildsStoreNumbersNewestFirst(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)

	for _, run := range []struct{ number, started string }{
		{"10", stampA},
		{"2", stampB}, // newest run, LOW lexical number — ordering is by started
		{"7", stampA},
	} {
		putBuildRow(t, st, &metadata.Build{
			Name: "ord", Number: run.number, Started: run.started,
			CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
		})
	}
	numbers, err := st.Builds().ListBuildNumbers(ctx, "ord", "")
	if err != nil {
		t.Fatalf("list numbers: %v", err)
	}
	gotOrder := []string{}
	for _, n := range numbers {
		gotOrder = append(gotOrder, n.Number)
	}
	// stampB sorts after stampA lexicographically (zone-stable wire
	// literals): the "2" run leads; the two stampA runs follow by number
	// DESC as TEXT ("7" > "10" — build numbers are CI free-form strings,
	// never integers) — "latest = take first".
	wantOrder := []string{"2", "7", "10"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("number order = %v, want %v (started DESC, number DESC)", gotOrder, wantOrder)
		}
	}
}

func TestBuildsStoreModulesSegment(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	putBuildRow(t, st, &metadata.Build{
		Name: "seg", Number: "1", Started: stampA,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})

	mods := []*metadata.BuildModule{
		{
			ID: "mod-b", Type: "maven",
			Artifacts: []*metadata.BuildArtifact{
				{Seq: 0, Name: "b.jar", Type: "jar", Sha1: "b1", Sha256: "b2", Md5: "b3"},
			},
		},
		{
			ID: "mod-a", Type: "gradle",
			Artifacts: []*metadata.BuildArtifact{
				{Seq: 1, Name: "a-sources.jar", Type: "jar"},
				{Seq: 0, Name: "a.jar", Type: "jar"},
			},
			Dependencies: []*metadata.BuildDependency{
				{Seq: 0, ID: "org.slf4j:slf4j-api:2.0.9", Type: "maven", Scopes: "compile,test", Sha1: "d1"},
				{Seq: 1, ID: "junit:junit:4.13.2", Type: "maven", Scopes: "test"},
			},
		},
	}
	if err := st.Builds().PutModules(ctx, "seg", "1", stampA, "", mods); err != nil {
		t.Fatalf("put modules: %v", err)
	}
	got, err := st.Builds().ListModules(ctx, "seg", "1", stampA, "")
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	if len(got) != 2 || got[0].ID != "mod-a" || got[1].ID != "mod-b" {
		t.Fatalf("modules = %+v, want [mod-a mod-b] ordered by id", got)
	}
	a := got[0]
	if len(a.Artifacts) != 2 || a.Artifacts[0].Seq != 0 || a.Artifacts[0].Name != "a.jar" ||
		a.Artifacts[1].Name != "a-sources.jar" {
		t.Errorf("mod-a artifacts = %+v, want wire order by seq", a.Artifacts)
	}
	if len(a.Dependencies) != 2 || a.Dependencies[0].ID != "org.slf4j:slf4j-api:2.0.9" ||
		a.Dependencies[0].Scopes != "compile,test" {
		t.Errorf("mod-a dependencies = %+v, want seq order with scopes joined", a.Dependencies)
	}
	if a.Type != "gradle" || got[1].Type != "maven" {
		t.Errorf("module types = %q/%q, want round-tripped", a.Type, got[1].Type)
	}

	// The replace law: PutModules swaps the segment whole, not merge.
	if err := st.Builds().PutModules(ctx, "seg", "1", stampA, "", []*metadata.BuildModule{
		{ID: "only", Type: "generic"},
	}); err != nil {
		t.Fatalf("put modules (replace): %v", err)
	}
	got, err = st.Builds().ListModules(ctx, "seg", "1", stampA, "")
	if err != nil {
		t.Fatalf("list modules (replace): %v", err)
	}
	if len(got) != 1 || got[0].ID != "only" || len(got[0].Artifacts) != 0 {
		t.Fatalf("replaced segment = %+v, want the single empty module", got)
	}

	// An orphan segment cannot land: the FK to the parent run rejects it
	// (the append face's 404 law rides this — T-508 maps the error).
	if err := st.Builds().PutModules(ctx, "seg", "1", stampA, "no-such-repo", mods); err == nil {
		t.Fatal("put modules under a missing parent repo must fail (FK)")
	}
	if err := st.Builds().PutModules(ctx, "ghost", "1", stampA, "", mods); err == nil {
		t.Fatal("put modules under a missing parent run must fail (FK)")
	}

	// The cascade: deleting the run takes the segment with it.
	if err := st.Builds().DeleteBuild(ctx, "seg", "1", stampA, ""); err != nil {
		t.Fatalf("delete build: %v", err)
	}
	if got, err := st.Builds().ListModules(ctx, "seg", "1", stampA, ""); err != nil || len(got) != 0 {
		t.Fatalf("modules after run delete = %+v (%v), want empty (cascade)", got, err)
	}
	if err := st.Builds().DeleteBuild(ctx, "seg", "1", stampA, ""); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Errorf("delete(missing) = %v, want ErrBuildNotFound", err)
	}
}

func TestBuildsStoreArtifactNodeAssociation(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)

	// A real node to associate with: repo + blob + node (the FK chain the
	// association hangs on).
	if err := st.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs-release", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	const sha = "1111111111111111111111111111111111111111111111111111111111111111"
	if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: 3, CreatedAt: now}); err != nil {
		t.Fatalf("put blob: %v", err)
	}
	if err := st.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "libs-release", Path: "com/acme/app.jar", Sha256: sha, Size: 3,
		CreatedBy: "ci", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("put node: %v", err)
	}

	putBuildRow(t, st, &metadata.Build{
		Name: "link", Number: "4", Started: stampA,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})
	mods := []*metadata.BuildModule{{
		ID: "mod", Type: "maven",
		Artifacts: []*metadata.BuildArtifact{
			// Associated: the wire path's repo segment split into
			// (repo_key, path) — a REAL nodes reference.
			{Seq: 0, Name: "app.jar", Type: "jar", Sha256: sha, RepoKey: "libs-release", Path: "com/acme/app.jar"},
			// Record-only: no checksums resolved, no association claimed.
			{Seq: 1, Name: "report.txt", Type: "txt"},
		},
	}}
	if err := st.Builds().PutModules(ctx, "link", "4", stampA, "", mods); err != nil {
		t.Fatalf("put modules: %v", err)
	}
	got, err := st.Builds().ListModules(ctx, "link", "4", stampA, "")
	if err != nil {
		t.Fatalf("list modules: %v", err)
	}
	linked, bare := got[0].Artifacts[0], got[0].Artifacts[1]
	if linked.RepoKey != "libs-release" || linked.Path != "com/acme/app.jar" {
		t.Errorf("associated artifact = %q/%q, want the nodes reference round-tripped",
			linked.RepoKey, linked.Path)
	}
	if bare.RepoKey != "" || bare.Path != "" {
		t.Errorf("record-only artifact = %q/%q, want the empty (NULL) association",
			bare.RepoKey, bare.Path)
	}

	// The association must be REAL: a dangling (repo_key, path) is
	// rejected by the FK, not silently stored.
	if err := st.Builds().PutModules(ctx, "link", "4", stampA, "", []*metadata.BuildModule{{
		ID: "mod", Type: "maven",
		Artifacts: []*metadata.BuildArtifact{
			{Seq: 0, Name: "x", RepoKey: "libs-release", Path: "no/such/node.jar"},
		},
	}}); err == nil {
		t.Fatal("a dangling nodes association must fail (FK)")
	}

	// The record OUTLIVES the node: deleting the node clears the
	// association (ON DELETE SET NULL) and keeps EVERY artifact row — the
	// failed dangling write above left the original segment in place, so
	// both rows (associated + record-only) are still here.
	if err := st.Nodes().Delete(ctx, "libs-release", "com/acme/app.jar"); err != nil {
		t.Fatalf("delete node: %v", err)
	}
	got, err = st.Builds().ListModules(ctx, "link", "4", stampA, "")
	if err != nil {
		t.Fatalf("list modules after node delete: %v", err)
	}
	if len(got[0].Artifacts) != 2 {
		t.Fatalf("artifacts after node delete = %+v, want both rows kept", got[0].Artifacts)
	}
	if got[0].Artifacts[0].RepoKey != "" || got[0].Artifacts[0].Path != "" {
		t.Errorf("association after node delete = %q/%q, want cleared (SET NULL)",
			got[0].Artifacts[0].RepoKey, got[0].Artifacts[0].Path)
	}
	if got[0].Artifacts[0].Name != "app.jar" || got[0].Artifacts[0].Sha256 != sha {
		t.Errorf("record after node delete = %+v, want the wire fields kept", got[0].Artifacts[0])
	}
}

func TestBuildsStorePromotionsAppendOnly(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	putBuildRow(t, st, &metadata.Build{
		Name: "rel", Number: "20", Started: stampA,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})

	addPromo := func(id, status, at string) {
		t.Helper()
		if err := st.Builds().AppendPromotion(ctx, &metadata.BuildPromotion{
			ID: id, Name: "rel", Number: "20", Started: stampA,
			Status: status, TargetRepo: "libs-release", CiUser: "jenkins",
			Comment: "nightly", PromotedBy: "dev", PromotedAt: at,
		}); err != nil {
			t.Fatalf("append promotion %s: %v", id, err)
		}
	}
	addPromo("p1", "staged", "2026-09-07T10:05:00Z")
	addPromo("p2", "released", "2026-09-07T11:05:00Z")

	rows, err := st.Builds().ListPromotions(ctx, "rel", "20", stampA, "")
	if err != nil {
		t.Fatalf("list promotions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("promotions = %d rows, want 2 (append-only history)", len(rows))
	}
	if rows[0].ID != "p2" || rows[0].Status != "released" {
		t.Errorf("history head = %+v, want p2/released (current = newest row)", rows[0])
	}
	if rows[0].CiUser != "jenkins" || rows[0].Comment != "nightly" || rows[0].PromotedBy != "dev" {
		t.Errorf("six-tuple round-trip = %+v, want every member kept", rows[0])
	}

	// A FREE-string status lands verbatim — no closed set, no rejection
	// (ADR-0045 Errata ②).
	addPromo("p3", "自定义-stage-v2", "2026-09-07T12:05:00Z")
	rows, err = st.Builds().ListPromotions(ctx, "rel", "20", stampA, "")
	if err != nil || rows[0].Status != "自定义-stage-v2" {
		t.Fatalf("free status = %q (%v), want verbatim", rows[0].Status, err)
	}

	// Duplicate ids are refused (the writer's uniqueness duty).
	if err := st.Builds().AppendPromotion(ctx, &metadata.BuildPromotion{
		ID: "p1", Name: "rel", Number: "20", Started: stampA,
		Status: "x", PromotedBy: "dev", PromotedAt: now,
	}); err == nil {
		t.Fatal("duplicate promotion id must fail")
	}

	// The cascade: the run's history leaves with the run.
	if err := st.Builds().DeleteBuild(ctx, "rel", "20", stampA, ""); err != nil {
		t.Fatalf("delete build: %v", err)
	}
	if rows, err := st.Builds().ListPromotions(ctx, "rel", "20", stampA, ""); err != nil || len(rows) != 0 {
		t.Fatalf("promotions after run delete = %+v (%v), want empty (cascade)", rows, err)
	}
}

func TestBuildsStorePropertiesSetSemantics(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	putBuildRow(t, st, &metadata.Build{
		Name: "props", Number: "2", Started: stampA,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})

	put := func(props ...*metadata.BuildProperty) {
		t.Helper()
		if err := st.Builds().PutProperties(ctx, "props", "2", stampA, "", props); err != nil {
			t.Fatalf("put properties: %v", err)
		}
	}
	put(&metadata.BuildProperty{Name: "env", Value: "prod"}, &metadata.BuildProperty{Name: "channel", Value: "beta"})
	got, err := st.Builds().ListProperties(ctx, "props", "2", stampA, "")
	if err != nil {
		t.Fatalf("list properties: %v", err)
	}
	if len(got) != 2 || got[0].Name != "channel" || got[1].Name != "env" {
		t.Fatalf("properties = %+v, want both ordered by name", got)
	}

	// Set semantics: the second Put REPLACES, never merges.
	put(&metadata.BuildProperty{Name: "env", Value: "staging"})
	got, err = st.Builds().ListProperties(ctx, "props", "2", stampA, "")
	if err != nil {
		t.Fatalf("list properties (replace): %v", err)
	}
	if len(got) != 1 || got[0].Value != "staging" {
		t.Fatalf("properties after replace = %+v, want only env=staging", got)
	}
}

// TestBuildsStoreMigrationIdempotent: the 024 family lands once and
// reopening re-runs nothing — the rows and the ledger row both survive a
// second Open (the ADR-0007 idempotency law, the annotate test's shape).
func TestBuildsStoreMigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "binflow.db")

	st, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("open (seed): %v", err)
	}
	now := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	putBuildRow(t, st, &metadata.Build{
		Name: "keep", Number: "1", Started: stampA, Payload: `{"keep":true}`,
		CreatedBy: "ci", CreatedAt: now, UpdatedBy: "ci", UpdatedAt: now,
	})
	if err := st.Builds().PutModules(ctx, "keep", "1", stampA, "", []*metadata.BuildModule{
		{ID: "m1", Type: "maven", Artifacts: []*metadata.BuildArtifact{{Seq: 0, Name: "a.jar"}}},
	}); err != nil {
		t.Fatalf("put modules (seed): %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close (seed): %v", err)
	}

	st2, err := metadata.Open(ctx, metadata.Options{Path: path, AdminPassword: "pw"})
	if err != nil {
		t.Fatalf("open (reopen): %v", err)
	}
	defer func() { _ = st2.Close() }()

	db := liveDB(t, path)
	var v int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 24`).Scan(&v); err != nil || v != 1 {
		t.Fatalf("schema_migrations v24 rows = %d (%v), want exactly 1 after reopen", v, err)
	}
	b, err := st2.Builds().GetBuild(ctx, "keep", "1", stampA, "")
	if err != nil || b.Payload != `{"keep":true}` {
		t.Fatalf("build after reopen = %+v (%v), want the seeded row intact", b, err)
	}
	mods, err := st2.Builds().ListModules(ctx, "keep", "1", stampA, "")
	if err != nil || len(mods) != 1 || mods[0].ID != "m1" {
		t.Fatalf("modules after reopen = %+v (%v), want the seeded segment intact", mods, err)
	}
}
