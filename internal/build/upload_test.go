// The upload/append orchestration's laws (M17 T-508, FR-152.2): the
// started-format gate's canonicalization, the overwrite arm's w-then-d
// ladder, the append merge's by-module-id law (append, never overwrite),
// the artifact association's resolve-or-record-only rule, and the
// malformed-document 400 family with its self-frozen verbatim messages.

package build_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// uploadWorld is the same-source assembly of build_acl_test's probeWorld,
// extended for the write faces: grants for the w-only and w+d arms.
type uploadWorld struct {
	store metadata.Store
	svc   *build.Service
	t     *testing.T
}

func newUploadWorld(t *testing.T) *uploadWorld {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Path:          filepath.Join(t.TempDir(), "binflow.db"),
		AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	az := auth.NewFromStore(st, false)
	w := &uploadWorld{
		store: st,
		svc:   build.New(st.Builds(), az, build.WithNodes(st.Nodes())),
		t:     t,
	}

	mk := func(l ...string) string {
		t.Helper()
		return mustMarshal(t, l)
	}
	now := "2026-09-07T09:00:00Z"
	put := func(name string, repos, includes, excludes []string, rows ...*metadata.PermissionPrincipal) {
		t.Helper()
		if err := st.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: mk(repos...), Includes: mk(includes...),
			Excludes: mk(excludes...), CreatedAt: now, UpdatedAt: now,
		}, rows); err != nil {
			t.Fatalf("PutTarget %s: %v", name, err)
		}
	}
	// wendell holds r+w on pub-* (no d anywhere) — the overwrite arm's
	// refuser. dean holds r+w+d on pub-* — the overwrite arm's allowed
	// caller (CI publishers read their own builds; r rides every grant).
	put("pub-w", []string{metadata.DefaultBuildRepo}, []string{"pub-*"}, nil,
		&metadata.PermissionPrincipal{TargetName: "pub-w", Principal: "wendell", PrincipalType: "user", CanRead: true, CanWrite: true})
	put("pub-wd", []string{metadata.DefaultBuildRepo}, []string{"pub-*"}, nil,
		&metadata.PermissionPrincipal{TargetName: "pub-wd", Principal: "dean", PrincipalType: "user", CanRead: true, CanWrite: true, CanDelete: true})
	put("team-wd", []string{"team-build-info"}, []string{"**"}, nil,
		&metadata.PermissionPrincipal{TargetName: "team-wd", Principal: "dean", PrincipalType: "user", CanRead: true, CanWrite: true, CanDelete: true})
	return w
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func (w *uploadWorld) upload(t *testing.T, p *build.Principal, doc, buildRepo string) (*build.UploadResult, error) {
	t.Helper()
	return w.svc.Upload(context.Background(), p, []byte(doc), buildRepo)
}

func (w *uploadWorld) getModules(t *testing.T, name, number, started, repo string) []*metadata.BuildModule {
	t.Helper()
	mods, err := w.store.Builds().ListModules(context.Background(), name, number, started, repo)
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	return mods
}

var (
	wendell = &auth.Principal{Name: "wendell", Role: auth.RoleUser}
	dean    = &auth.Principal{Name: "dean", Role: auth.RoleUser}
	eve     = &auth.Principal{Name: "eve", Role: auth.RoleUser}
)

// minimalDoc is a complete-enough build info document: the interpreted
// fields plus one uninterpreted rider the payload archive must keep.
const minimalDoc = `{
  "version": "1.0.1",
  "name": "pub-app",
  "number": "51",
  "type": "GENERIC",
  "started": "2026-09-07T10:00:00.000+0000",
  "buildAgent": {"name": "jenkins", "version": "2.4"},
  "url": "https://ci.example.org/job/pub-app/51",
  "modules": [
    {
      "id": "com.example:api:1.0",
      "type": "maven",
      "artifacts": [
        {"type": "jar", "sha1": "aa", "sha256": "bb", "md5": "cc",
         "name": "api-1.0.jar", "path": "libs/pub-app/api-1.0.jar"}
      ],
      "dependencies": [
        {"type": "jar", "sha1": "dd", "id": "junit:junit:4.13", "scopes": ["test"]}
      ]
    }
  ],
  "properties": {"env": "prod"}
}`

// TestNormalizeStartedCanonicalizesEveryAcceptedSpelling: the started gate
// accepts the Java canonical, RFC3339-with-fraction and seconds-precision
// zone forms and renders ONE canonical UTC literal — the stored-form
// uniformity that keeps MAX(started) honest (T-507 leftover 1).
func TestNormalizeStartedCanonicalizesEveryAcceptedSpelling(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		fails bool
	}{
		{"java-canonical", "2013-01-01T10:11:12.123+0100", "2013-01-01T09:11:12.123+0000", false},
		{"utc-canonical-already", "2026-09-07T10:00:00.000+0000", "2026-09-07T10:00:00.000+0000", false},
		{"rfc3339-z", "2026-09-07T10:00:00Z", "2026-09-07T10:00:00.000+0000", false},
		{"rfc3339-offset-colon", "2026-09-07T18:00:00.500+08:00", "2026-09-07T10:00:00.500+0000", false},
		{"rfc3339-nanos", "2026-09-07T10:00:00.123456789Z", "2026-09-07T10:00:00.123+0000", false},
		{"seconds-precision-zone", "2026-09-07T12:00:00+0200", "2026-09-07T10:00:00.000+0000", false},
		{"date-only", "2026-09-07", "", true},
		{"epoch", "1709049600000", "", true},
		{"empty", "", "", true},
		{"garbage", "not-a-stamp", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := build.NormalizeStarted(tc.in)
			if tc.fails {
				if err == nil {
					t.Fatalf("NormalizeStarted(%q) = %q, want error", tc.in, got)
				}
				if !errors.Is(err, build.ErrInvalidBuildInfo) {
					t.Fatalf("error = %v, want ErrInvalidBuildInfo wrap", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeStarted(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeStarted(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestUploadCreatesRunWithCanonicalStartedAndArchivedPayload: the first
// publication lands the run with the CANONICAL started literal (an
// offset-carried input still stores UTC), the interpreted segments
// normalized and the document archived byte-for-byte.
func TestUploadCreatesRunWithCanonicalStartedAndArchivedPayload(t *testing.T) {
	w := newUploadWorld(t)
	doc := strings.Replace(minimalDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T18:00:00.000+0800"`, 1)
	res, err := w.upload(t, dean, doc, "")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if !res.Created {
		t.Fatal("first publication reported the overwrite arm")
	}
	if res.Started != "2026-09-07T10:00:00.000+0000" {
		t.Fatalf("stored started = %q, want the canonical UTC literal", res.Started)
	}
	if res.Repo != metadata.DefaultBuildRepo {
		t.Fatalf("resolved repo = %q, want %q", res.Repo, metadata.DefaultBuildRepo)
	}
	b, err := w.store.Builds().GetBuild(context.Background(), "pub-app", "51", res.Started, "")
	if err != nil {
		t.Fatalf("GetBuild: %v", err)
	}
	if b.Payload != doc {
		t.Fatalf("payload not archived byte-for-byte:\n got %s\nwant %s", b.Payload, doc)
	}
	mods := w.getModules(t, "pub-app", "51", res.Started, "")
	if len(mods) != 1 || mods[0].ID != "com.example:api:1.0" {
		t.Fatalf("modules = %+v, want the one interpreted module", mods)
	}
	if len(mods[0].Dependencies) != 1 || mods[0].Dependencies[0].ID != "junit:junit:4.13" ||
		mods[0].Dependencies[0].Scopes != "test" {
		t.Fatalf("dependencies = %+v", mods[0].Dependencies)
	}
	props, err := w.store.Builds().ListProperties(context.Background(), "pub-app", "51", res.Started, "")
	if err != nil || len(props) != 1 || props[0].Name != "env" || props[0].Value != "prod" {
		t.Fatalf("properties = %+v (err %v)", props, err)
	}
}

// TestUploadArtifactAssociationResolvesOrRecords: an artifact whose path
// names a LIVE node carries the association; a missing node (or a sha256
// the node refutes) lands record-only — the row survives, no link claimed.
func TestUploadArtifactAssociationResolvesOrRecords(t *testing.T) {
	w := newUploadWorld(t)
	ctx := context.Background()
	now := "2026-09-07T09:00:00Z"
	// The node's FK chain: repo + blob + node (the 024 store test's seed).
	if err := w.store.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "libs", Type: "local", PackageType: "generic",
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := w.store.Blobs().Put(ctx, &metadata.Blob{Sha256: "bb", Size: 3, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob: %v", err)
	}
	if err := w.store.Nodes().Put(ctx, &metadata.Node{
		RepoKey: "libs", Path: "pub-app/api-1.0.jar",
		Sha256: "bb", Size: 3, CreatedBy: "ci", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	res, err := w.upload(t, dean, minimalDoc, "")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	mods := w.getModules(t, "pub-app", "51", res.Started, "")
	if len(mods) != 1 || len(mods[0].Artifacts) != 1 {
		t.Fatalf("artifacts = %+v", mods)
	}
	a := mods[0].Artifacts[0]
	if a.RepoKey != "libs" || a.Path != "pub-app/api-1.0.jar" {
		t.Fatalf("association = %s/%s, want libs/pub-app/api-1.0.jar", a.RepoKey, a.Path)
	}

	// A document whose sha256 disagrees with the live node: record-only.
	mismatch := strings.Replace(minimalDoc, `"sha256": "bb"`, `"sha256": "ee"`, 1)
	mismatch = strings.Replace(mismatch, `"number": "51"`, `"number": "52"`, 1)
	res2, err := w.upload(t, dean, mismatch, "")
	if err != nil {
		t.Fatalf("upload mismatch: %v", err)
	}
	mods2 := w.getModules(t, "pub-app", "52", res2.Started, "")
	if a := mods2[0].Artifacts[0]; a.RepoKey != "" || a.Path != "" {
		t.Fatalf("refuted association was kept: %s/%s", a.RepoKey, a.Path)
	}
	// A path with no live node behind it: record-only too.
	missing := strings.Replace(minimalDoc, "libs/pub-app/api-1.0.jar", "libs/nope.jar", 1)
	missing = strings.Replace(missing, `"number": "51"`, `"number": "53"`, 1)
	res3, err := w.upload(t, dean, missing, "")
	if err != nil {
		t.Fatalf("upload missing: %v", err)
	}
	mods3 := w.getModules(t, "pub-app", "53", res3.Started, "")
	if a := mods3[0].Artifacts[0]; a.RepoKey != "" || a.Path != "" {
		t.Fatalf("unresolvable association was kept: %s/%s", a.RepoKey, a.Path)
	}
}

// TestUploadOverwriteArmRequiresDelete: the official ladder — w alone
// creates, w without d cannot re-publish the same run, w+d replaces it
// whole (modules and properties included).
func TestUploadOverwriteArmRequiresDelete(t *testing.T) {
	w := newUploadWorld(t)
	if _, err := w.upload(t, wendell, minimalDoc, ""); err != nil {
		t.Fatalf("wendell first publish (w arm): %v", err)
	}
	_, err := w.upload(t, wendell, minimalDoc, "")
	if !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("wendell re-publish = %v, want ErrForbidden (overwrite needs d)", err)
	}
	// A different started is a DIFFERENT run: creation, not overwrite.
	other := strings.Replace(minimalDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T11:30:00.000+0000"`, 1)
	if res, err := w.upload(t, wendell, other, ""); err != nil || !res.Created {
		t.Fatalf("wendell new-run publish = %v (created %v), want clean create", err, res.Created)
	}
	// dean (w+d) re-publishes the original run: replacement.
	replaced := strings.Replace(minimalDoc,
		`"properties": {"env": "prod"}`, `"properties": {"env": "stage"}`, 1)
	res, err := w.upload(t, dean, replaced, "")
	if err != nil {
		t.Fatalf("dean re-publish: %v", err)
	}
	if res.Created {
		t.Fatal("re-publication reported the create arm")
	}
	props, err := w.store.Builds().ListProperties(context.Background(), "pub-app", "51", res.Started, "")
	if err != nil || len(props) != 1 || props[0].Value != "stage" {
		t.Fatalf("properties after replacement = %+v (err %v)", props, err)
	}
	// The custom buildRepo arm: dean's team grant reaches a custom key.
	teamDoc := strings.Replace(minimalDoc, `"name": "pub-app"`, `"name": "team-app"`, 1)
	if _, err := w.upload(t, dean, teamDoc, "team-build-info"); err != nil {
		t.Fatalf("custom buildRepo publish: %v", err)
	}
}

// TestUploadRejectsMalformedDocuments: the 400 family's self-frozen
// verbatim messages (the reference pins no 400 wording, §9 #6 — BinFlow
// freezes its own and the wire tests assert these strings).
func TestUploadRejectsMalformedDocuments(t *testing.T) {
	w := newUploadWorld(t)
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{"not json", `{"name": "pub-app",`, "build info is not valid JSON"},
		{"no name", `{"number": "1", "started": "2026-09-07T10:00:00.000+0000"}`, "build name is empty"},
		{"no number", `{"name": "pub-app", "started": "2026-09-07T10:00:00.000+0000"}`, "build number is empty"},
		{"no started", `{"name": "pub-app", "number": "1"}`, `build started "" must be an ISO8601 timestamp`},
		{"bad started", `{"name": "pub-app", "number": "1", "started": "yesterday"}`, "must be an ISO8601 timestamp"},
		{"name with slash", `{"name": "pub/app", "number": "1", "started": "2026-09-07T10:00:00.000+0000"}`, "contains '/'"},
		{"module without id", `{"name": "pub-app", "number": "1", "started": "2026-09-07T10:00:00.000+0000", "modules": [{"type": "maven"}]}`, "module id is empty"},
		{"duplicate module id", `{"name": "pub-app", "number": "1", "started": "2026-09-07T10:00:00.000+0000", "modules": [{"id": "m"}, {"id": "m"}]}`, `duplicate module id "m"`},
		{"dependency without id", `{"name": "pub-app", "number": "1", "started": "2026-09-07T10:00:00.000+0000", "modules": [{"id": "m", "dependencies": [{"type": "jar"}]}]}`, "dependency with an empty id"},
		{"property without name", `{"name": "pub-app", "number": "1", "started": "2026-09-07T10:00:00.000+0000", "properties": {"": "v"}}`, "build property name is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.upload(t, dean, tc.doc, "")
			if err == nil {
				t.Fatalf("upload accepted a malformed document")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
			if !errors.Is(err, build.ErrInvalidBuildInfo) && !errors.Is(err, build.ErrInvalidCoordinate) {
				t.Fatalf("error = %v, want the 400-family sentinel", err)
			}
		})
	}
}

// TestUploadGateRefusesBeforeLookup: an unauthorized caller meets
// ErrForbidden even when the build does not exist — rejection precedes
// existence, no oracle (NFR-S80's first arm on the write face).
func TestUploadGateRefusesBeforeLookup(t *testing.T) {
	w := newUploadWorld(t)
	if _, err := w.upload(t, eve, minimalDoc, ""); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("eve publish = %v, want ErrForbidden", err)
	}
	secret := strings.Replace(minimalDoc, `"name": "pub-app"`, `"name": "secret"`, 1)
	if _, err := w.upload(t, eve, secret, ""); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("eve publish (nonexistent name) = %v, want the SAME ErrForbidden", err)
	}
	// dean's grant is pattern-bound to pub-* — a name outside the pattern
	// is denied the same way, existent or not.
	if _, err := w.upload(t, dean, secret, ""); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("dean publish outside the pattern = %v, want ErrForbidden", err)
	}
}

// appendDoc builds one append body: a JSON ARRAY of modules.
func appendDoc(t *testing.T, modules ...string) []byte {
	t.Helper()
	return []byte("[" + strings.Join(modules, ",") + "]")
}

// TestAppendMergesByModuleIDWithoutOverwriting: the AC's merge law — a
// second publish carrying a dependencies section MERGES into the same
// module (key = module id), the module's earlier rows survive, sibling
// modules are untouched, new ids land as new modules.
func TestAppendMergesByModuleIDWithoutOverwriting(t *testing.T) {
	w := newUploadWorld(t)
	res, err := w.upload(t, dean, minimalDoc, "")
	if err != nil {
		t.Fatalf("seed upload: %v", err)
	}
	started := res.Started

	body := appendDoc(t,
		`{"id": "com.example:api:1.0", "type": "maven",
		  "artifacts": [{"type": "pom", "sha1": "11", "name": "api-1.0.pom", "path": "libs/pub-app/api-1.0.pom"}],
		  "dependencies": [{"type": "jar", "sha1": "22", "id": "org:lib:2.0"}]}`,
		`{"id": "com.example:web:1.0", "type": "maven",
		  "dependencies": [{"type": "jar", "sha1": "33", "id": "org:web-dep:1.0"}]}`)
	if _, err := w.svc.Append(context.Background(), dean, build.Coordinate{
		Name: "pub-app", Number: "51"}, body); err != nil {
		t.Fatalf("append: %v", err)
	}

	mods := w.getModules(t, "pub-app", "51", started, "")
	if len(mods) != 2 {
		t.Fatalf("modules after append = %d (%+v), want 2 (merged + new)", len(mods), mods)
	}
	var api, web *metadata.BuildModule
	for _, m := range mods {
		switch m.ID {
		case "com.example:api:1.0":
			api = m
		case "com.example:web:1.0":
			web = m
		}
	}
	if api == nil || web == nil {
		t.Fatalf("module ids after append: %+v", mods)
	}
	// The merge law's core: the original rows SURVIVE the second publish.
	if len(api.Artifacts) != 2 || len(api.Dependencies) != 2 {
		t.Fatalf("api module after append: %d artifacts, %d dependencies — want 2/2 (appended, not overwritten)",
			len(api.Artifacts), len(api.Dependencies))
	}
	if api.Artifacts[0].Name != "api-1.0.jar" || api.Artifacts[1].Name != "api-1.0.pom" {
		t.Fatalf("artifact order after append: %+v", api.Artifacts)
	}
	if api.Dependencies[0].ID != "junit:junit:4.13" || api.Dependencies[1].ID != "org:lib:2.0" {
		t.Fatalf("dependency order after append: %+v", api.Dependencies)
	}
	if len(web.Dependencies) != 1 || web.Dependencies[0].ID != "org:web-dep:1.0" {
		t.Fatalf("new module's dependencies: %+v", web.Dependencies)
	}
}

// TestAppendParentMustExist: the missing parent answers the not-found
// sentinel (the wire's verbatim 404 source) — and the ladder is honest: a
// denied caller meets forbidden FIRST, without the oracle.
func TestAppendParentMustExist(t *testing.T) {
	w := newUploadWorld(t)
	body := appendDoc(t, `{"id": "m"}`)
	_, err := w.svc.Append(context.Background(), dean, build.Coordinate{
		Name: "pub-ghost", Number: "1"}, body)
	if !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Fatalf("append onto missing parent = %v, want ErrBuildNotFound", err)
	}
	if _, err := w.svc.Append(context.Background(), eve, build.Coordinate{
		Name: "pub-ghost", Number: "1"}, body); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("denied append onto missing parent = %v, want ErrForbidden FIRST", err)
	}
	// w without d is still refused: the official Deploy ∧ Delete gate.
	res, err := w.upload(t, wendell, minimalDoc, "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := w.svc.Append(context.Background(), wendell, build.Coordinate{
		Name: "pub-app", Number: "51", Started: res.Started}, body); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("w-only append = %v, want ErrForbidden (append needs w ∧ d)", err)
	}
}

// TestAppendResolvesLatestRunAndNormalizesStarted: started=” lands the
// merge on the LATEST run; a caller-passed ?started= literal in a
// different zone form still addresses the same stored run.
func TestAppendResolvesLatestRunAndNormalizesStarted(t *testing.T) {
	w := newUploadWorld(t)
	early := strings.Replace(minimalDoc,
		`"started": "2026-09-07T10:00:00.000+0000"`,
		`"started": "2026-09-07T08:00:00.000+0000"`, 1)
	if _, err := w.upload(t, dean, early, ""); err != nil {
		t.Fatalf("seed early run: %v", err)
	}
	res, err := w.upload(t, dean, minimalDoc, "") // the 10:00 run — the latest
	if err != nil {
		t.Fatalf("seed latest run: %v", err)
	}
	body := appendDoc(t, `{"id": "late-marker", "dependencies": [{"id": "x:y:1"}]}`)
	if _, err := w.svc.Append(context.Background(), dean, build.Coordinate{
		Name: "pub-app", Number: "51"}, body); err != nil {
		t.Fatalf("append latest: %v", err)
	}
	if _, err := w.svc.Append(context.Background(), dean, build.Coordinate{
		Name: "pub-app", Number: "51",
		Started: "2026-09-07T18:00:00.000+0800", // the 10:00 UTC run, another zone
	}, appendDoc(t, `{"id": "zone-marker"}`)); err != nil {
		t.Fatalf("append via offset-carried started: %v", err)
	}

	late := w.getModules(t, "pub-app", "51", res.Started, "")
	var names []string
	for _, m := range late {
		names = append(names, m.ID)
	}
	if !contains(names, "late-marker") || !contains(names, "zone-marker") {
		t.Fatalf("latest run's modules = %v, want both markers", names)
	}
	earlyMods := w.getModules(t, "pub-app", "51", "2026-09-07T08:00:00.000+0000", "")
	var earlyIDs []string
	for _, m := range earlyMods {
		earlyIDs = append(earlyIDs, m.ID)
	}
	if contains(earlyIDs, "late-marker") || contains(earlyIDs, "zone-marker") {
		t.Fatalf("early run absorbed the merge: %v", earlyIDs)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// TestGetBuildDetailAssemblesTheRun: the query orchestration reads the
// gated header plus the three child segments of the RESOLVED run.
func TestGetBuildDetailAssemblesTheRun(t *testing.T) {
	w := newUploadWorld(t)
	res, err := w.upload(t, dean, minimalDoc, "")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	detail, err := w.svc.GetBuildDetail(context.Background(), dean, build.Coordinate{
		Name: "pub-app", Number: "51"})
	if err != nil {
		t.Fatalf("GetBuildDetail: %v", err)
	}
	if detail.Build.Started != res.Started {
		t.Fatalf("detail started = %q, want the resolved run's %q", detail.Build.Started, res.Started)
	}
	if len(detail.Modules) != 1 || len(detail.Properties) != 1 || len(detail.Promotions) != 0 {
		t.Fatalf("detail segments: %d modules, %d properties, %d promotions",
			len(detail.Modules), len(detail.Properties), len(detail.Promotions))
	}
	if _, err := w.svc.GetBuildDetail(context.Background(), eve, build.Coordinate{
		Name: "pub-app", Number: "51"}); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("eve detail = %v, want ErrForbidden", err)
	}
	if _, err := w.svc.GetBuildDetail(context.Background(), dean, build.Coordinate{
		Name: "pub-app", Number: "404"}); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Fatalf("missing detail = %v, want ErrBuildNotFound", err)
	}
}
