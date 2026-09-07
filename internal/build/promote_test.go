// The promotion and retention laws (M17 T-509, FR-152.2 / ADR-0045 decision
// 5 + decision 9): the promote verb's w(target) ∧ r(buildRepo) gate (the
// NFR-S81 probe, denial before existence), the status-only flip onto the
// append-only history, the generic-artifact migration over the real
// CopyOrMove carrier (move default, copy arm, dry run, failFast dangling
// law), the docker manifest-closure replay with its sha256 reconciliation,
// and the retention window (minimumBuildDate floor, count window,
// exemptions, artifact deletion, the per-run deletion audit).

package build_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/build"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// promoteWorld is the full promote stack: the real storage engine, metadata
// store, auth service, repository service (the carrier, discovered by
// assertion the way httpapi assembles it) and the build service with every
// T-509 seam wired.
type promoteWorld struct {
	store   metadata.Store
	svc     repo.Service
	builds  *build.Service
	carrier build.Carrier
	t       *testing.T
}

var (
	adminP = &auth.Principal{Name: "admin", Role: auth.RoleAdmin, Admin: true}
	// travis holds r+w+d+a on the dev/target repos and r on the build repo
	// (the CI promoter); veronica holds r on the build repo only (no target
	// w — the gate's refuser); ursula holds w on the target but no read
	// anywhere (the r(buildRepo) refuser); wenda holds r(buildRepo) plus
	// w/d(target) but NO annotate (the properties arm's refuser).
	travis   = &auth.Principal{Name: "travis", Role: auth.RoleUser}
	veronica = &auth.Principal{Name: "veronica", Role: auth.RoleUser}
	ursula   = &auth.Principal{Name: "ursula", Role: auth.RoleUser}
	wenda    = &auth.Principal{Name: "wenda", Role: auth.RoleUser}
)

func newPromoteWorld(t *testing.T) *promoteWorld {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{
		Path: filepath.Join(dataDir, "binflow.db"), AdminPassword: "pw",
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	authSvc := auth.NewFromStore(md, false)
	au := audit.New(md, true)
	svc := repo.New(st, md, authSvc, au)
	carrier, ok := svc.(build.Carrier)
	if !ok {
		t.Fatalf("repo.Service does not satisfy build.Carrier — the assembly assertion would fail")
	}
	w := &promoteWorld{
		store: md, svc: svc, carrier: carrier, t: t,
		builds: build.New(md.Builds(), authSvc,
			build.WithNodes(md.Nodes()),
			build.WithCarrier(carrier),
			build.WithDocker(md.Docker()),
			build.WithProps(md.NodeProps()),
			build.WithAudit(audit.BestEffort(au))),
	}

	now := "2026-09-07T09:00:00Z"
	mk := func(l ...string) string {
		t.Helper()
		b, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	put := func(name string, repos []string, principal string, r, wr, d, a bool) {
		t.Helper()
		if err := md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: mk(repos...), Includes: mk("**"), Excludes: mk(),
			CreatedAt: now, UpdatedAt: now,
		}, []*metadata.PermissionPrincipal{{
			TargetName: name, Principal: principal, PrincipalType: "user",
			CanRead: r, CanWrite: wr, CanDelete: d, CanAnnotate: a,
		}}); err != nil {
			t.Fatalf("PutTarget %s: %v", name, err)
		}
	}
	for _, u := range []string{"travis", "veronica", "ursula", "wenda"} {
		hash, err := auth.HashPassword("pw-" + u)
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if err := md.Users().Create(ctx, &metadata.User{
			Username: u, PasswordHash: hash, Enabled: true,
		}); err != nil {
			t.Fatalf("seed user %s: %v", u, err)
		}
	}
	// travis: the full promoter over both content repos and the build repo
	// (annotate included — the properties arm rides a(targetRepo)).
	put("dev-full", []string{"dev-libs", "rel-libs", "dev-docker", "rel-docker"}, "travis", true, true, true, true)
	put("dev-build", []string{metadata.DefaultBuildRepo}, "travis", true, true, true, false)
	// veronica: build-repo read only (the missing target-write refuser).
	put("dev-build-read", []string{metadata.DefaultBuildRepo}, "veronica", true, false, false, false)
	// ursula: target write only, no build-repo read.
	put("rel-write", []string{"rel-libs"}, "ursula", false, true, true, false)
	// wenda: the properties-arm refuser — r(buildRepo) + w/d(target), no a.
	put("wenda-target", []string{"rel-libs"}, "wenda", false, true, true, false)
	put("wenda-build", []string{metadata.DefaultBuildRepo}, "wenda", true, false, false, false)
	return w
}

// seedLocalRepo creates one local repository of the package type.
func (w *promoteWorld) seedLocalRepo(t *testing.T, key, pkg string) {
	t.Helper()
	now := "2026-09-07T09:00:00Z"
	if err := w.store.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: "local", PackageType: pkg,
		Config: "{}", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedNode lands one file node row over a blob ledger row (the checksum
// addressing means the carrier never needs the filestore bytes).
func (w *promoteWorld) seedNode(t *testing.T, repoKey, path, sha256 string, size int64) {
	t.Helper()
	ctx := context.Background()
	now := "2026-09-07T09:00:00Z"
	if err := w.store.Blobs().Put(ctx, &metadata.Blob{Sha256: sha256, Size: size, CreatedAt: now}); err != nil {
		t.Fatalf("seed blob %s: %v", sha256, err)
	}
	if err := w.store.Nodes().Put(ctx, &metadata.Node{
		RepoKey: repoKey, Path: path, Sha256: sha256, Size: size,
		CreatedBy: "ci", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed node %s/%s: %v", repoKey, path, err)
	}
}

// uploadBuild publishes one build info document (the T-508 face).
func (w *promoteWorld) uploadBuild(t *testing.T, p *auth.Principal, doc string) *build.UploadResult {
	t.Helper()
	res, err := w.builds.Upload(context.Background(), p, []byte(doc), "")
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return res
}

// promote issues one promotion through the service face.
func (w *promoteWorld) promote(t *testing.T, p *auth.Principal, name, number, body string) (*build.PromotionResult, error) {
	t.Helper()
	return w.builds.Promote(context.Background(), p, build.Coordinate{Name: name, Number: number}, []byte(body))
}

// node returns one node row (nil when absent).
func (w *promoteWorld) node(t *testing.T, repoKey, path string) *metadata.Node {
	t.Helper()
	n, err := w.store.Nodes().Get(context.Background(), repoKey, path)
	if errors.Is(err, metadata.ErrNodeNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("node get %s/%s: %v", repoKey, path, err)
	}
	return n
}

// auditRows reads the audit trail filtered by action (newest first).
func (w *promoteWorld) auditRows(t *testing.T, action string) []*metadata.AuditEvent {
	t.Helper()
	rows, err := w.store.Audits().Query(context.Background(), metadata.AuditQuery{Action: action, Limit: 50})
	if err != nil {
		t.Fatalf("audit query %s: %v", action, err)
	}
	return rows
}

// genericDoc builds one build info document whose single artifact associates
// the given node (matching sha256 → the association resolves).
func genericDoc(name, number, started, repoKey, path, sha string) string {
	return fmt.Sprintf(`{
	  "name": %q, "number": %q, "type": "GENERIC", "started": %q,
	  "modules": [{"id": "m1", "artifacts": [
	    {"type": "bin", "sha256": %q, "name": "app.bin", "path": %q}
	  ]}]
	}`, name, number, started, sha, repoKey+"/"+path)
}

// TestPromoteStatusOnlyFlipsStatusAndWritesHistoryAndAudit: the status-only
// arm — no targetRepo, one append-only history row, the CURRENT status is
// the newest row, nothing migrated, the build.promote audit row present
// (plus the upload's build.upload row: the +5 words' emit sites).
func TestPromoteStatusOnlyFlipsStatusAndWritesHistoryAndAudit(t *testing.T) {
	w := newPromoteWorld(t)
	res := w.uploadBuild(t, travis, genericDoc("flip-app", "7",
		"2026-09-07T10:00:00.000+0000", "dev-libs", "flip/7.bin", "aa11"))

	// Caller-supplied timestamps keep the append-only order deterministic
	// (§2.4: the CURRENT status is the max-by-timestamp row — the body's
	// timestamp field is the spec's own ordering mechanism).
	if _, err := w.promote(t, travis, "flip-app", "7",
		`{"status":"staged","ciUser":"jenkins","timestamp":"2026-09-07T12:00:01.000+0000"}`); err != nil {
		t.Fatalf("status-only promote: %v", err)
	}
	if _, err := w.promote(t, travis, "flip-app", "7",
		`{"status":"released","timestamp":"2026-09-07T12:00:02.000+0000"}`); err != nil {
		t.Fatalf("second promote: %v", err)
	}
	history, err := w.store.Builds().ListPromotions(context.Background(), "flip-app", "7", res.Started, "")
	if err != nil {
		t.Fatalf("ListPromotions: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history rows = %d, want 2 (append-only)", len(history))
	}
	if history[0].Status != "released" || history[1].Status != "staged" {
		t.Fatalf("history order = [%s, %s], want newest-first released/staged",
			history[0].Status, history[1].Status)
	}
	if history[1].CiUser != "jenkins" || history[0].CiUser != "" {
		t.Fatalf("ciUser carry = %+v", history)
	}
	promotes := w.auditRows(t, audit.ActionBuildPromote)
	if len(promotes) != 2 {
		t.Fatalf("build.promote audit rows = %d, want 2", len(promotes))
	}
	if promotes[0].Path != "flip-app" || promotes[0].RepoKey != metadata.DefaultBuildRepo {
		t.Fatalf("promote audit row = %+v", promotes[0])
	}
	// The upload face's own word landed with it.
	if rows := w.auditRows(t, audit.ActionBuildUpload); len(rows) != 1 {
		t.Fatalf("build.upload audit rows = %d, want 1", len(rows))
	}
}

// TestPromoteGateArmsAndNoOracle: the ADR-0045 decision-5 gate on the wire's
// service face — no w(target) refuses, no r(buildRepo) refuses, and BOTH
// refusals precede the run's existence (no oracle, NFR-S80/S81).
func TestPromoteGateArmsAndNoOracle(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "rel-libs", "generic")
	w.uploadBuild(t, travis, genericDoc("gate-app", "1",
		"2026-09-07T10:00:00.000+0000", "dev-libs", "gate/1.bin", "bb22"))
	body := `{"status":"released","targetRepo":"rel-libs"}`

	for _, tc := range []struct {
		name string
		p    *auth.Principal
	}{
		{"no grants at all", veronica},              // r(buildRepo) holds, no w(target)
		{"target write without build read", ursula}, // w(target) holds, no r(buildRepo)
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := w.promote(t, tc.p, "gate-app", "1", body); !errors.Is(err, build.ErrForbidden) {
				t.Fatalf("gate = %v, want ErrForbidden", err)
			}
			// The same refusal for a run that does not exist: no oracle.
			if _, err := w.promote(t, tc.p, "ghost-app", "9", body); !errors.Is(err, build.ErrForbidden) {
				t.Fatalf("gate (nonexistent run) = %v, want the SAME ErrForbidden", err)
			}
		})
	}
	// The properties arm demands annotate on the target (Errata ④㋔): wenda
	// holds r(buildRepo) + w(target) but no a.
	props := `{"status":"released","targetRepo":"rel-libs","properties":{"release":"v1"}}`
	if _, err := w.promote(t, wenda, "gate-app", "1", props); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("properties arm without annotate = %v, want ErrForbidden", err)
	}
	if _, err := w.promote(t, wenda, "ghost-app", "1", props); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("properties arm denial (nonexistent run) = %v, want the SAME ErrForbidden", err)
	}
}

// TestPromoteGenericArtifactsMoveAndCopy: the migration arm over the real
// carrier — the default MOVES (source row gone), copy=true keeps it, the
// promotion history row lands, and body properties ride the landed node.
func TestPromoteGenericArtifactsMoveAndCopy(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	w.seedLocalRepo(t, "rel-libs", "generic")
	w.seedNode(t, "dev-libs", "mv/1.bin", "cc33", 11)
	w.seedNode(t, "dev-libs", "cp/2.bin", "dd44", 22)
	doc := `{
	  "name": "mv-app", "number": "3", "type": "GENERIC",
	  "started": "2026-09-07T10:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [
	    {"type": "bin", "sha256": "cc33", "name": "1.bin", "path": "dev-libs/mv/1.bin"},
	    {"type": "bin", "sha256": "dd44", "name": "2.bin", "path": "dev-libs/cp/2.bin"}
	  ]}]
	}`
	w.uploadBuild(t, travis, doc)

	res, err := w.promote(t, travis, "mv-app", "3",
		`{"status":"released","targetRepo":"rel-libs","properties":{"release":"v2"}}`)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if res.Artifacts != 2 {
		t.Fatalf("migrated = %d, want 2 (res: %+v)", res.Artifacts, res.Messages)
	}
	// The move arm: target rows landed with the source checksums, source
	// rows left, properties rode the landed nodes.
	for _, path := range []string{"mv/1.bin", "cp/2.bin"} {
		tgt := w.node(t, "rel-libs", path)
		if tgt == nil {
			t.Fatalf("target %s did not land", path)
		}
		if w.node(t, "dev-libs", path) != nil {
			t.Fatalf("source %s survived the default move", path)
		}
	}
	props, err := w.store.NodeProps().List(context.Background(), "rel-libs", "mv/1.bin")
	if err != nil || len(props["release"]) != 1 || props["release"][0] != "v2" {
		t.Fatalf("promoted properties = %v (err %v), want release=v2", props, err)
	}
	// The copy arm keeps the source.
	w.seedNode(t, "dev-libs", "again/3.bin", "ee55", 33)
	doc2 := strings.Replace(doc, `"name": "mv-app"`, `"name": "cp-app"`, 1)
	doc2 = strings.Replace(doc2, `"artifacts": [
	    {"type": "bin", "sha256": "cc33", "name": "1.bin", "path": "dev-libs/mv/1.bin"},
	    {"type": "bin", "sha256": "dd44", "name": "2.bin", "path": "dev-libs/cp/2.bin"}
	  ]`, `"artifacts": [
	    {"type": "bin", "sha256": "ee55", "name": "3.bin", "path": "dev-libs/again/3.bin"}
	  ]`, 1)
	w.uploadBuild(t, travis, doc2)
	if _, err := w.promote(t, travis, "cp-app", "3",
		`{"status":"released","targetRepo":"rel-libs","copy":true}`); err != nil {
		t.Fatalf("copy promote: %v", err)
	}
	if w.node(t, "dev-libs", "again/3.bin") == nil {
		t.Fatal("copy=true deleted the source — copy must keep it")
	}
	if w.node(t, "rel-libs", "again/3.bin") == nil {
		t.Fatal("copy=true did not land the target")
	}
}

// TestPromoteDryRunHasZeroSideEffects: dryRun=true walks the whole ladder
// (gate, 404, target validation) and lands NOTHING — no node, no history.
func TestPromoteDryRunHasZeroSideEffects(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	w.seedLocalRepo(t, "rel-libs", "generic")
	w.seedNode(t, "dev-libs", "dry/1.bin", "ff66", 5)
	res := w.uploadBuild(t, travis, genericDoc("dry-app", "1",
		"2026-09-07T10:00:00.000+0000", "dev-libs", "dry/1.bin", "ff66"))

	out, err := w.promote(t, travis, "dry-app", "1",
		`{"status":"released","targetRepo":"rel-libs","dryRun":true}`)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !out.DryRun || out.Artifacts != 0 {
		t.Fatalf("dry-run result = %+v", out)
	}
	if w.node(t, "rel-libs", "dry/1.bin") != nil {
		t.Fatal("dry run landed a target node")
	}
	if w.node(t, "dev-libs", "dry/1.bin") == nil {
		t.Fatal("dry run moved the source node")
	}
	history, _ := w.store.Builds().ListPromotions(context.Background(), "dry-app", "1", res.Started, "")
	if len(history) != 0 {
		t.Fatalf("dry run appended %d history rows, want 0", len(history))
	}
	// A dry run still refuses honestly: the gate and the 404 stay live.
	if _, err := w.promote(t, veronica, "dry-app", "1",
		`{"targetRepo":"rel-libs","dryRun":true}`); !errors.Is(err, build.ErrForbidden) {
		t.Fatalf("dry-run gate = %v, want ErrForbidden", err)
	}
	if _, err := w.promote(t, travis, "ghost", "1",
		`{"targetRepo":"rel-libs","dryRun":true}`); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Fatalf("dry-run 404 = %v, want ErrBuildNotFound", err)
	}
}

// TestPromoteTargetValidation: the target must exist and be LOCAL (decision
// 5) — a missing key and a virtual key both refuse 400-class.
func TestPromoteTargetValidation(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	now := "2026-09-07T09:00:00Z"
	if err := w.store.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: "virt-libs", Type: "virtual", PackageType: "generic",
		Config: `{"members": ["dev-libs"]}`, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed virtual: %v", err)
	}
	w.uploadBuild(t, travis, genericDoc("tgt-app", "1",
		"2026-09-07T10:00:00.000+0000", "dev-libs", "t/1.bin", "0a0a"))

	for _, tc := range []struct {
		name   string
		body   string
		wantIn string
	}{
		{"missing repo", `{"targetRepo":"no-such"}`, "not found"},
		{"virtual target", `{"targetRepo":"virt-libs"}`, "must be a local repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// admin passes the w(target) gate unconditionally, so the
			// validation refusal itself is what answers (a grantless user
			// meets the no-oracle 403 instead — the gate test pins that).
			_, err := w.promote(t, adminP, "tgt-app", "1", tc.body)
			if err == nil {
				t.Fatalf("target %q accepted", tc.body)
			}
			if !errors.Is(err, build.ErrInvalidBuildInfo) {
				t.Fatalf("error = %v, want the 400-family sentinel", err)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("message %q missing %q", err.Error(), tc.wantIn)
			}
		})
	}
}

// TestPromoteFailFastDanglingArtifacts: the decision-5 C-layer ruling — a
// record-only (unassociated) artifact REFUSES under the failFast default
// and SKIPS with a warning under failFast=false.
func TestPromoteFailFastDanglingArtifacts(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	w.seedLocalRepo(t, "rel-libs", "generic")
	// No live node behind the artifact path → record-only association.
	w.uploadBuild(t, travis, genericDoc("dangle-app", "1",
		"2026-09-07T10:00:00.000+0000", "dev-libs", "gone/1.bin", "1b1b"))

	_, err := w.promote(t, travis, "dangle-app", "1", `{"targetRepo":"rel-libs"}`)
	var se *repo.StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadRequest {
		t.Fatalf("failFast dangling = %v, want a 400 StatusError", err)
	}
	if !strings.Contains(se.Message, "no live node association") {
		t.Fatalf("dangling message = %q", se.Message)
	}
	res, err := w.promote(t, travis, "dangle-app", "1", `{"targetRepo":"rel-libs","failFast":false}`)
	if err != nil {
		t.Fatalf("failFast=false promote: %v", err)
	}
	if res.Artifacts != 0 {
		t.Fatalf("migrated = %d, want 0", res.Artifacts)
	}
	warned := false
	for _, m := range res.Messages {
		if m.Level == "warning" && strings.Contains(m.Message, "no live node association") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("messages carry no dangling warning: %+v", res.Messages)
	}
}

// seedDockerImage publishes one image through the repository service's own
// faces: config and layer blob nodes, the manifest node, the index row, the
// tag pointer and the ref ledger — the layout a compliant docker push
// leaves behind. When indexChildren is non-empty the root manifest is an
// INDEX whose children are the given manifests (the closure's recursion
// case).
func (w *promoteWorld) seedDockerImage(t *testing.T, repoKey, image, tag string, hex64 func(string) string) (root string) {
	t.Helper()
	ctx := context.Background()
	mediaManifest := "application/vnd.docker.distribution.manifest.v2+json"
	mediaIndex := "application/vnd.docker.distribution.manifest.list.v2+json"

	// PutManifest writes the manifest NODE itself (putNode: blob row +
	// node row) — pre-seeding the node would trip the idempotent-republish
	// arm, which deliberately leaves the index row alone.
	pushManifest := func(dgst string, isIndex bool, refs []*metadata.DockerRef) {
		mediaType := mediaManifest
		if isIndex {
			mediaType = mediaIndex
		}
		if _, err := w.carrier.PutManifest(ctx, adminP, repoKey, image, dgst, tag, mediaType,
			int64(100+len(dgst)), refs); err != nil {
			t.Fatalf("PutManifest %s/%s@%s: %v", repoKey, image, dgst, err)
		}
	}

	if len(indexChildren) == 0 {
		w.seedNode(t, repoKey, image+"/blobs/"+hex64("config"), hex64("config"), 64)
		w.seedNode(t, repoKey, image+"/blobs/"+hex64("layer"), hex64("layer"), 128)
		root = hex64("manifest")
		pushManifest(root, false, []*metadata.DockerRef{
			{RepoKey: repoKey, Image: image, ManifestDigest: root, BlobDigest: hex64("config"),
				ChildMediaType: "application/vnd.docker.container.image.v1+json"},
			{RepoKey: repoKey, Image: image, ManifestDigest: root, BlobDigest: hex64("layer"),
				ChildMediaType: "application/vnd.docker.image.rootfs.diff.tar.gzip"},
		})
		return root
	}
	// The index shape: two child manifests, each with its own config blob,
	// the index citing both children (the closure must walk BOTH levels).
	var indexRefs []*metadata.DockerRef
	for _, child := range indexChildren {
		w.seedNode(t, repoKey, image+"/blobs/"+hex64("cfg-"+child), hex64("cfg-"+child), 64)
		childDigest := hex64("child-" + child)
		pushManifest(childDigest, false, []*metadata.DockerRef{
			{RepoKey: repoKey, Image: image, ManifestDigest: childDigest, BlobDigest: hex64("cfg-" + child),
				ChildMediaType: "application/vnd.docker.container.image.v1+json"},
		})
		indexRefs = append(indexRefs, &metadata.DockerRef{
			RepoKey: repoKey, Image: image, ManifestDigest: hex64("index"), BlobDigest: childDigest,
			ChildMediaType: mediaManifest,
		})
	}
	root = hex64("index")
	pushManifest(root, true, indexRefs)
	return root
}

// indexChildren is the seedDockerImage recursion knob (set per test).
var indexChildren []string

// TestPromoteDockerImageClosureReplayAndReconciliation: the docker leg —
// one build-associated manifest promotes its WHOLE closure (config/layer
// blobs, child manifests of an index), the target serves the tag, the move
// arm cleans the source index (no ghost tags), and every landed node's
// sha256 equals the digest its layout path names (the AC's 对账).
func TestPromoteDockerImageClosureReplayAndReconciliation(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-docker", "docker")
	w.seedLocalRepo(t, "rel-docker", "docker")
	hex64 := func(s string) string {
		h := fmt.Sprintf("%x", s)
		return h + strings.Repeat("0", 64-len(h))
	}
	indexChildren = []string{"amd64", "arm64"}
	t.Cleanup(func() { indexChildren = nil })
	root := w.seedDockerImage(t, "dev-docker", "myapp", "1", hex64)

	doc := fmt.Sprintf(`{
	  "name": "img-app", "number": "5", "type": "DOCKER", "started": "2026-09-07T10:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [
	    {"type": "docker", "sha256": %q, "name": "myapp:1", "path": "dev-docker/myapp/manifests/%s"}
	  ]}]
	}`, root, root)
	w.uploadBuild(t, travis, doc)

	if _, err := w.promote(t, travis, "img-app", "5", `{"status":"released","targetRepo":"rel-docker"}`); err != nil {
		t.Fatalf("docker promote: %v", err)
	}

	ctx := context.Background()
	// The tag serves in the target, at the root digest.
	tag, err := w.store.Docker().GetTag(ctx, "rel-docker", "myapp", "1")
	if err != nil {
		t.Fatalf("target tag: %v", err)
	}
	if tag.Digest != root {
		t.Fatalf("target tag digest = %s, want the root %s", tag.Digest, root)
	}
	// The closure landed: index + both children + their config blobs, each
	// node's sha256 == its path digest (the reconciliation law — a mismatch
	// would have aborted the promotion, so reaching here IS the proof, and
	// the explicit probe below pins it against regressions).
	mustNode := func(path, want string) {
		n := w.node(t, "rel-docker", path)
		if n == nil {
			t.Fatalf("closure member %s did not land", path)
		}
		if n.Sha256 != want {
			t.Fatalf("closure member %s sha256 = %s, want %s", path, n.Sha256, want)
		}
	}
	mustNode("myapp/manifests/"+root, root)
	for _, child := range indexChildren {
		mustNode("myapp/manifests/"+hex64("child-"+child), hex64("child-"+child))
		mustNode("myapp/blobs/"+hex64("cfg-"+child), hex64("cfg-"+child))
	}
	// The ref ledger followed: the target index cites both children.
	refs, err := w.store.Docker().ListRefsByManifest(ctx, "rel-docker", "myapp", root)
	if err != nil || len(refs) != 2 {
		t.Fatalf("target index refs = %v (err %v), want 2 children", refs, err)
	}
	// The index ROW itself landed (the wire regression this pins: a
	// carrier-copied node would trip PutManifest's idempotent arm and skip
	// it — a tag pointing at no manifest row is a 404 pull).
	mrow, err := w.store.Docker().GetManifest(ctx, "rel-docker", "myapp", root)
	if err != nil || mrow.MediaType != "application/vnd.docker.distribution.manifest.list.v2+json" {
		t.Fatalf("target index row = %+v (err %v)", mrow, err)
	}
	// The move arm cleaned the SOURCE: no ghost tag, no manifest row.
	if _, err := w.store.Docker().GetTag(ctx, "dev-docker", "myapp", "1"); !errors.Is(err, metadata.ErrTagNotFound) {
		t.Fatalf("source tag survived the move: %v", err)
	}
	if _, err := w.store.Docker().GetManifest(ctx, "dev-docker", "myapp", root); !errors.Is(err, metadata.ErrManifestNotFound) {
		t.Fatalf("source manifest row survived the move: %v", err)
	}
	if w.node(t, "dev-docker", "myapp/manifests/"+root) != nil {
		t.Fatal("source manifest node survived the move")
	}
	// The copy arm keeps the source (one more image, copy=true).
	root2 := w.seedDockerImage(t, "dev-docker", "keepapp", "2", hex64)
	doc2 := fmt.Sprintf(`{
	  "name": "keep-app", "number": "1", "started": "2026-09-07T11:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [
	    {"type": "docker", "sha256": %q, "name": "keepapp:2", "path": "dev-docker/keepapp/manifests/%s"}
	  ]}]
	}`, root2, root2)
	w.uploadBuild(t, travis, doc2)
	if _, err := w.promote(t, travis, "keep-app", "1",
		`{"status":"released","targetRepo":"rel-docker","copy":true}`); err != nil {
		t.Fatalf("copy docker promote: %v", err)
	}
	if _, err := w.store.Docker().GetTag(ctx, "dev-docker", "keepapp", "2"); err != nil {
		t.Fatalf("copy arm dropped the source tag: %v", err)
	}
	if _, err := w.store.Docker().GetTag(ctx, "rel-docker", "keepapp", "2"); err != nil {
		t.Fatalf("copy arm missed the target tag: %v", err)
	}
}

// TestRetentionWindowDeletesOutsideAndKeepsInside: the §2.5 window —
// minimumBuildDate floors the deletable set, count caps the survivors,
// buildNumbersNotToBeDiscarded exempts, deleteBuildArtifacts removes the
// associated nodes, and every discarded run leaves one build.delete audit
// row beside the op-level build.retention row.
func TestRetentionWindowDeletesOutsideAndKeepsInside(t *testing.T) {
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	ctx := context.Background()

	seed := func(number, started string) {
		t.Helper()
		w.seedNode(t, "dev-libs", "ret/"+number+".bin", "f"+number, 7)
		w.uploadBuild(t, travis, genericDoc("ret-app", number, started, "dev-libs", "ret/"+number+".bin", "f"+number))
	}
	seed("1", "2026-08-01T10:00:00.000+0000") // outside the floor
	seed("2", "2026-08-15T10:00:00.000+0000") // outside the floor — but exempted
	seed("3", "2026-09-01T10:00:00.000+0000") // fresh, beyond count=2 → discarded
	seed("4", "2026-09-05T10:00:00.000+0000") // fresh — inside the count window
	seed("5", "2026-09-06T10:00:00.000+0000") // fresh — inside the count window

	plan, err := w.builds.PrepareRetention(ctx, travis, "ret-app", "", build.RetentionRequest{
		DeleteBuildArtifacts:         true,
		Count:                        2,
		MinimumBuildDate:             "2026-09-01T00:00:00Z",
		BuildNumbersNotToBeDiscarded: []string{"2"},
	})
	if err != nil {
		t.Fatalf("PrepareRetention: %v", err)
	}
	res, err := plan.Execute(ctx, w.builds, travis)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Deleted) != 2 || res.Kept != 3 {
		t.Fatalf("window = deleted %v kept %d, want {1,3} kept 3", res.Deleted, res.Kept)
	}
	for _, gone := range []string{"ret-app#1", "ret-app#3"} {
		if !contains(res.Deleted, gone) {
			t.Fatalf("window discarded %v, want %s among them", res.Deleted, gone)
		}
	}
	numbers, err := w.store.Builds().ListBuildNumbers(ctx, "ret-app", "")
	if err != nil {
		t.Fatalf("ListBuildNumbers: %v", err)
	}
	var left []string
	for _, n := range numbers {
		left = append(left, n.Number)
	}
	for _, gone := range []string{"1", "3"} {
		if contains(left, gone) {
			t.Fatalf("run %s survived the window: %v", gone, left)
		}
	}
	for _, keep := range []string{"2", "4", "5"} {
		if !contains(left, keep) {
			t.Fatalf("run %s was discarded — exemption/count violation: %v", keep, left)
		}
	}
	// deleteBuildArtifacts removed the discarded runs' nodes; the survivors'
	// nodes stand.
	if w.node(t, "dev-libs", "ret/1.bin") != nil || w.node(t, "dev-libs", "ret/3.bin") != nil {
		t.Fatal("deleteBuildArtifacts did not remove the discarded runs' nodes")
	}
	if w.node(t, "dev-libs", "ret/2.bin") == nil {
		t.Fatal("deleteBuildArtifacts touched an exempted run's node")
	}
	// The audit trail: one build.delete row per discarded run, one
	// build.retention row for the window.
	if rows := w.auditRows(t, audit.ActionBuildDelete); len(rows) != 2 {
		t.Fatalf("build.delete audit rows = %d, want 2", len(rows))
	}
	if rows := w.auditRows(t, audit.ActionBuildRetention); len(rows) != 1 {
		t.Fatalf("build.retention audit rows = %d, want 1", len(rows))
	}
}

// TestRetentionGateAndUnknownName: the d(buildRepo) gate refuses before the
// lookup (no oracle), and an unknown name answers the family's 404.
func TestRetentionGateAndUnknownName(t *testing.T) {
	w := newPromoteWorld(t)
	ctx := context.Background()
	for _, name := range []string{"ret-app", "ghost-app"} {
		if _, err := w.builds.PrepareRetention(ctx, veronica, name, "",
			build.RetentionRequest{Count: 1}); !errors.Is(err, build.ErrForbidden) {
			t.Fatalf("retention gate (%s) = %v, want ErrForbidden (veronica holds no d)", name, err)
		}
	}
	if _, err := w.builds.PrepareRetention(ctx, travis, "ghost-app", "",
		build.RetentionRequest{Count: 1}); !errors.Is(err, metadata.ErrBuildNotFound) {
		t.Fatalf("unknown retention name = %v, want ErrBuildNotFound", err)
	}
	// A malformed floor refuses 400-class.
	if _, err := w.builds.PrepareRetention(ctx, travis, "ret-app", "",
		build.RetentionRequest{MinimumBuildDate: "yesterday"}); !errors.Is(err, build.ErrInvalidBuildInfo) {
		t.Fatalf("malformed minimumBuildDate = %v, want ErrInvalidBuildInfo", err)
	}
}

// TestPromoteLargeBuildMigratesWithoutServerError: NFR-P80's promotion arm —
// a build of 150 associated artifacts migrates through the carrier with a
// clean result (every artifact landed, ZERO error rows, no 5xx abort), the
// per-item aggregation carrying the whole load.
func TestPromoteLargeBuildMigratesWithoutServerError(t *testing.T) {
	if testing.Short() {
		t.Skip("the 150-artifact migration is a budget test, not a unit law")
	}
	w := newPromoteWorld(t)
	w.seedLocalRepo(t, "dev-libs", "generic")
	w.seedLocalRepo(t, "rel-libs", "generic")

	const n = 150
	arts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		sha := fmt.Sprintf("%064x", i)
		path := fmt.Sprintf("bulk/%04d.bin", i)
		w.seedNode(t, "dev-libs", path, sha, int64(i+1))
		arts = append(arts, fmt.Sprintf(
			`{"type": "bin", "sha256": %q, "name": "%04d.bin", "path": "dev-libs/%s"}`, sha, i, path))
	}
	doc := fmt.Sprintf(`{
	  "name": "bulk-app", "number": "1", "type": "GENERIC",
	  "started": "2026-09-07T10:00:00.000+0000",
	  "modules": [{"id": "m", "artifacts": [%s]}]
	}`, strings.Join(arts, ","))
	w.uploadBuild(t, travis, doc)

	res, err := w.promote(t, travis, "bulk-app", "1", `{"status":"released","targetRepo":"rel-libs"}`)
	if err != nil {
		t.Fatalf("large promote = %v (want the zero-5xx aggregate)", err)
	}
	if res.Artifacts != n {
		t.Fatalf("migrated = %d, want %d", res.Artifacts, n)
	}
	for _, m := range res.Messages {
		if m.Level == "error" || m.Level == "warning" {
			t.Fatalf("large promote carried a %s row: %s", m.Level, m.Message)
		}
	}
	for i := 0; i < n; i += 37 { // spot probe across the range
		if w.node(t, "rel-libs", fmt.Sprintf("bulk/%04d.bin", i)) == nil {
			t.Fatalf("artifact %d did not land in the target", i)
		}
	}
}
