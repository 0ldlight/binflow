package replication_test

// T-262 — FR-82-AC6: the npm push plane under a NON-ADMIN target principal
// (T-249 leftover 2). T-249 moved the packument's version-append transition
// onto the write grant (SkipOverwriteCheck) and left open whether the
// replication engine's packument merge write would trip the same overwrite
// arm. The audit's answer is pinned here from two sides:
//
//   - the plane-level table below pins the WRITE FORM the engine issues per
//     scenario (a one-version publish document through the target's publish
//     face — never a raw/full packument PUT) and how each of the three
//     T-249 scenarios resolves, including the race arm's 403 → terminal
//     classification (ADR-0021 first-write-wins);
//   - the two-instance test below runs the real target arm chain
//     (authorizeContentPut + the T-249 append classifier) over HTTP Basic
//     with a principal whose grant is read+write and deliberately WITHOUT
//     delete.
//
// Spec anchors: maven-npm-pypi.md section 2.3 step 4 (existing tarball →
// 403 "Cannot modify pre-existing version" for EVERYONE — the version-
// immutability layer), section 2.1 (dist-tag face errors are 403
// write/annotate, never a delete demand), repo-semantics.md section 3 (the
// overwrite pair the append exemption narrows).

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // npm dist.shasum protocol digest
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- shared plumbing ----

// startReplicationAs is startReplication with explicit target credentials:
// the engine authenticates against the target as the given principal, NOT
// as admin (the T-262 audit's whole point).
func startReplicationAs(t *testing.T, a, target *binflow, sourceRepo, targetRepo, username, password string) replication.Store {
	t.Helper()
	ctx := context.Background()
	store := a.replicationStore(t)

	key := bytes.Repeat([]byte{9}, 32)
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	encPw, err := cipher.Encrypt(password)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := store.CreateConfig(ctx, &replication.ReplicationConfig{
		Name:              "dr-" + sourceRepo + "-to-" + targetRepo,
		SourceRepo:        sourceRepo,
		TargetURL:         target.url,
		TargetRepo:        targetRepo,
		TargetUsername:    username,
		TargetPasswordEnc: encPw,
		Enabled:           true,
		CreatedAt:         metadata.Now(),
		UpdatedAt:         metadata.Now(),
	}); err != nil {
		t.Fatalf("CreateConfig: %v", err)
	}

	engine, err := replication.NewEngine(store, a.st, replication.EngineOptions{
		Cipher: cipher,
		Audit:  audit.New(a.md, true),
		Meta:   replication.NewStoreMetaSource(a.md),
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	repo.AttachReplicator(a.svc, engine)
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = engine.Run(runCtx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("engine Run did not return after cancel")
		}
		engine.CloseIdleConnections()
	})
	return store
}

// seedWriteOnlyPushPrincipal creates a target-side NON-ADMIN user whose
// grant on repoKey is read+write and deliberately NOT delete — the
// principal class T-249's leftover worried about, materialized.
func seedWriteOnlyPushPrincipal(t *testing.T, b *binflow, repoKey, username, password string) {
	t.Helper()
	ctx := context.Background()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password for %s: %v", username, err)
	}
	if err := b.md.Users().Create(ctx, &metadata.User{
		Username: username, PasswordHash: hash, IsAdmin: false, Enabled: true,
	}); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	target := "t262-" + repoKey
	if err := b.md.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
		Name: target, Repos: `["` + repoKey + `"]`, Includes: "[]", Excludes: "[]",
	}, []*metadata.PermissionPrincipal{{
		TargetName: target, Principal: username, PrincipalType: "user",
		CanRead: true, CanWrite: true, CanDelete: false,
	}}); err != nil {
		t.Fatalf("seed permission for %s: %v", username, err)
	}
}

// npmPublishDocFor composes one valid single-version publish document for
// the real publish face (digests computed from the tarball, the face's own
// step-8/step-9 checks in mind).
func npmPublishDocFor(t *testing.T, name, version string, tarball []byte) []byte {
	t.Helper()
	sha1Sum := sha1.Sum(tarball) //nolint:gosec // npm dist.shasum protocol digest
	sha512Sum := sha512.Sum512(tarball)
	doc := map[string]any{
		"_id": name, "name": name,
		"versions": map[string]any{
			version: map[string]any{
				"name": name, "version": version,
				"dist": map[string]any{
					"tarball":   name + "/-/" + name + "-" + version + ".tgz",
					"shasum":    hex.EncodeToString(sha1Sum[:]),
					"integrity": "sha512-" + base64.StdEncoding.EncodeToString(sha512Sum[:]),
				},
			},
		},
		"dist-tags": map[string]any{"latest": version},
		"_attachments": map[string]any{
			name + "-" + version + ".tgz": map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tarball),
			},
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("compose publish document for %s@%s: %v", name, version, err)
	}
	return body
}

// npmPublishOn publishes one version through the instance's publish face.
func npmPublishOn(t *testing.T, b *binflow, repoKey, name, version string, tarball []byte, user, pw string) {
	t.Helper()
	resp, body := b.do(http.MethodPut, "/binflow/"+repoKey+"/"+name, user, pw,
		npmPublishDocFor(t, name, version, tarball))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("publish %s@%s on %s as %s: status %d (%s), want 201",
			name, version, repoKey, user, resp.StatusCode, body)
	}
}

// npmTargetPackument fetches and decodes the instance's served packument.
func npmTargetPackument(t *testing.T, b *binflow, repoKey, name string) map[string]any {
	t.Helper()
	resp, body := b.getWithAccept("/binflow/"+repoKey+"/"+name, "application/json")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("packument %s/%s on %s: status %d (%s), want 200",
			repoKey, name, b.url, resp.StatusCode, body)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("packument %s/%s is not JSON: %v", repoKey, name, err)
	}
	return doc
}

// npmTargetTarball fetches the served tarball bytes of one version.
func npmTargetTarball(t *testing.T, b *binflow, repoKey, name, version string) []byte {
	t.Helper()
	path := "/binflow/" + repoKey + "/" + name + "/-/" + name + "-" + version + ".tgz"
	resp, body := b.do(http.MethodGet, path, "", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tarball %s on %s: status %d, want 200", path, b.url, resp.StatusCode)
	}
	return body
}

// npmVersionManifestJSON renders one served version manifest stably (sorted
// map keys) so byte-identity across phases is assertable.
func npmVersionManifestJSON(t *testing.T, doc map[string]any, version string) string {
	t.Helper()
	versions, _ := doc["versions"].(map[string]any)
	m, ok := versions[version].(map[string]any)
	if !ok {
		t.Fatalf("packument lacks version %s: versions=%v", version, versions)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest %s: %v", version, err)
	}
	return string(raw)
}

// ---- plane-level table (scripted target): write form per scenario ----

// TestNpmSyncOverwriteAdjudication walks the three T-249 scenarios plus the
// race arm through the npm plane against a scripted target, pinning:
//
//	scenario               engine write form                        outcome
//	---------------------  ----------------------------------------  --------
//	target lacks package   one-version publish doc (fresh)          success
//	target lacks version   one-version publish doc (append)         success
//	same version, other    NO write at all — missing-only diff      success
//	  data (true rewrite)  (target-side versions survive)
//	race: version lands    publish PUT answers the pinned 403       TERMINAL
//	between GET and PUT    (step 4, for everyone incl. admin)       (ADR-0021)
//
// The engine never PUTs a full packument at the package root: every write
// rides the publish face (a one-version document with the tarball
// attached) or the single-tag face, so the T-249 append classifier on the
// TARGET is what the target-side principal meets — and appending is the
// exact transition it exempts.
func TestNpmSyncOverwriteAdjudication(t *testing.T) {
	const name = "@t262/pkg"
	tarballs := map[string]string{
		"1.0.0": "t262 tarball payload zero",
		"1.0.1": "t262 tarball payload one - different bytes",
	}
	// Source manifests carry their own dist.shasum; the scripted target
	// only needs distinguishable data.
	manifest := func(v, shasum string) map[string]any {
		return map[string]any{"name": name, "version": v, "dist": map[string]any{
			"tarball": name + "/-/" + name + "-" + v + ".tgz", "shasum": shasum,
		}}
	}
	targetDocWith := func(v, shasum, latest string) string {
		raw, _ := json.Marshal(map[string]any{
			"versions":  map[string]any{v: manifest(v, shasum)},
			"dist-tags": map[string]any{"latest": latest},
			"_rev":      "1-deadbeef",
			"time":      map[string]any{"created": "t", "modified": "t"},
		})
		return string(raw)
	}

	tests := []struct {
		name        string
		srcVersions map[string]any
		srcLatest   string
		targetDoc   string // "" = 404: the target lacks the package
		publishSt   int    // 0 = the publish PUT must never happen
		wantFailed  bool   // terminal (not-retryable) task
		wantReqs    int
		wantPubVers []string // the publish PUT's versions set, when it fires
	}{
		{
			name: "target lacks the package: fresh publish",
			srcVersions: map[string]any{
				"1.0.0": manifest("1.0.0", "src-00"),
			},
			srcLatest:   "1.0.0",
			targetDoc:   "",
			publishSt:   http.StatusCreated,
			wantReqs:    3, // GET, PUT publish, PUT dist-tag
			wantPubVers: []string{"1.0.0"},
		},
		{
			name: "target has the package, lacks the new version: append",
			srcVersions: map[string]any{
				"1.0.0": manifest("1.0.0", "src-00"),
				"1.0.1": manifest("1.0.1", "src-11"),
			},
			srcLatest:   "1.0.1",
			targetDoc:   targetDocWith("1.0.0", "src-00", "1.0.0"),
			publishSt:   http.StatusCreated,
			wantReqs:    3,
			wantPubVers: []string{"1.0.1"}, // ONLY the missing version, never a full-document replacement
		},
		{
			name: "target serves the same version with different data: no write at all",
			srcVersions: map[string]any{
				"1.0.1": manifest("1.0.1", "src-11"),
			},
			srcLatest: "1.0.1",
			targetDoc: targetDocWith("1.0.1", "target-own-9c", "1.0.1"),
			publishSt: 0, // the true-overwrite form is unreachable: the diff is missing-only
			wantReqs:  1, // exactly the packument GET
		},
		{
			name: "race: version lands between GET and PUT: 403 is terminal",
			srcVersions: map[string]any{
				"1.0.1": manifest("1.0.1", "src-11"),
			},
			srcLatest:  "1.0.1",
			targetDoc:  `{"versions":{},"dist-tags":{}}`, // the fetched doc lags the target's tarball
			publishSt:  http.StatusForbidden,
			wantFailed: true,
			wantReqs:   2, // GET, PUT publish (refused) — no dist-tag PUT after the refusal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDoc := map[string]any{
				"_id": name, "name": name,
				"versions":  tt.srcVersions,
				"dist-tags": map[string]any{"latest": tt.srcLatest},
				"time":      map[string]any{"created": "t", "modified": "t"},
				"readme":    "T-262 scenario document",
			}
			raw, err := json.Marshal(srcDoc)
			if err != nil {
				t.Fatalf("marshal source packument: %v", err)
			}
			docHex := testSHA256(string(raw))

			meta := &fakeMeta{
				pkg: map[string]string{"libs-local": "npm"},
				nodes: map[string]map[string]*replication.NodeMeta{
					"libs-local": {name + "/packument.json": {Sha256: docHex}},
				},
			}
			for v := range tt.srcVersions {
				meta.nodes["libs-local"][name+"/-/"+name+"-"+v+".tgz"] =
					&replication.NodeMeta{Sha256: testSHA256(tarballs[v])}
			}

			target := newProtoTarget()
			if tt.targetDoc == "" {
				target.set(http.MethodGet, "/binflow/mirror/"+name, http.StatusNotFound, nil, "")
			} else {
				target.set(http.MethodGet, "/binflow/mirror/"+name, http.StatusOK, nil, tt.targetDoc)
			}
			if tt.publishSt != 0 {
				target.set(http.MethodPut, "/binflow/mirror/"+name, tt.publishSt, nil, `{"success":true}`)
			}
			target.set(http.MethodPut, "/binflow/mirror/-/package/"+name+"/dist-tags/latest",
				http.StatusCreated, nil, `{"ok":"created new tag"}`)

			f := newPlaneFixture(t, target, meta, nil)
			f.blobs.content[docHex] = string(raw)
			for v := range tt.srcVersions {
				f.blobs.content[testSHA256(tarballs[v])] = tarballs[v]
			}
			f.engine.Enqueue(f.ctx, "libs-local", name+"/packument.json", docHex)

			if tt.wantFailed {
				task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
					return task.Status == replication.TaskStatusFailed
				}, "terminal failure")
				if !strings.Contains(task.LastError, "not retryable") {
					t.Fatalf("task last_error = %q, want the not-retryable marker (the 403 step-4 refusal is a first-write-wins conflict, not a transient fault)", task.LastError)
				}
				if !strings.Contains(task.LastError, "403") {
					t.Fatalf("task last_error = %q, want the target's 403 folded in", task.LastError)
				}
			} else {
				f.waitTask(t, func(task *replication.ReplicationTask) bool {
					return task.Status == replication.TaskStatusSuccess
				}, "success")
			}

			reqs := target.requests()
			if len(reqs) != tt.wantReqs {
				var saw []string
				for _, r := range reqs {
					saw = append(saw, r.Method+" "+r.Path)
				}
				t.Fatalf("target requests = %v, want %d (the write form for this scenario is fixed)", saw, tt.wantReqs)
			}
			if len(reqs) > 0 && reqs[0].Method != http.MethodGet {
				t.Fatalf("first request = %s %s, want the packument GET", reqs[0].Method, reqs[0].Path)
			}

			if tt.wantPubVers != nil {
				var pub *protoReq
				for i := range reqs {
					if reqs[i].Method == http.MethodPut && reqs[i].Path == "/binflow/mirror/"+name {
						pub = &reqs[i]
					}
				}
				if pub == nil {
					t.Fatalf("no publish PUT among %d requests", len(reqs))
				}
				var sent map[string]any
				if err := json.Unmarshal([]byte(pub.Body), &sent); err != nil {
					t.Fatalf("publish body is not JSON: %v", err)
				}
				versions, _ := sent["versions"].(map[string]any)
				got := make([]string, 0, len(versions))
				for v := range versions {
					got = append(got, v)
				}
				sort.Strings(got)
				if strings.Join(got, ",") != strings.Join(tt.wantPubVers, ",") {
					t.Fatalf("publish document versions = %v, want exactly %v (the one-version publish form — never a full packument replacement)", got, tt.wantPubVers)
				}
				atts, _ := sent["_attachments"].(map[string]any)
				if len(atts) != 1 {
					t.Fatalf("publish _attachments = %v, want the one tarball", sent["_attachments"])
				}
			}
		})
	}
}

// ---- two-instance leg: the real arm chain under a write-only principal ----

// TestTwoInstanceNpmReplicationUnderWriteOnlyPrincipal runs the full loop —
// source publish → engine (Basic auth as a read+write, NO-delete principal)
// → the target's real publish/dist-tag faces → authorizeContentPut and the
// T-249 append classifier — across the three scenarios:
//
//  1. fresh package: the first version lands through the write grant alone;
//  2. append: the SECOND version lands the same way (the T-247/T-249 form:
//     the target's packument node already exists — pre-T-249 this exact
//     shape died 403 for a write-only principal);
//  3. true rewrite: the target already serves the same version with its own
//     data — the engine skips (missing-only diff), the task still succeeds,
//     and the target-side version survives untouched (ADR-0021
//     first-write-wins).
//
// A deprecate control at the end proves the principal really lacks delete
// and the strict overwrite arm still stands on this stack: rewriting an
// existing version manifest (deprecate) stays 403 for it, 201 for admin.
func TestTwoInstanceNpmReplicationUnderWriteOnlyPrincipal(t *testing.T) {
	const (
		srcRepo  = "npm-src"
		dstRepo  = "npm-dst"
		pushUser = "t262-repl-rw"
		pushPw   = "pw-t262-repl-rw"
	)
	b := newBinFlowFull(t, "B-npm-t262", "pw-b-t262", []*metadata.Repo{
		{RepoKey: dstRepo, Type: repo.TypeLocal, PackageType: repo.PackageNpm, Config: "{}"},
	})
	a := newBinFlowFull(t, "A-npm-t262", "pw-a-t262", []*metadata.Repo{
		{RepoKey: srcRepo, Type: repo.TypeLocal, PackageType: repo.PackageNpm, Config: "{}"},
	})
	seedWriteOnlyPushPrincipal(t, b, dstRepo, pushUser, pushPw)
	store := startReplicationAs(t, a, b, srcRepo, dstRepo, pushUser, pushPw)

	// -- scenario 1: fresh package (the engine principal creates it) --
	const appendPkg = "@t262/append"
	tar100 := []byte("t262 append tarball 1.0.0")
	npmPublishOn(t, a, srcRepo, appendPkg, "1.0.0", tar100, "admin", a.adminPw)
	waitForAllTasksSuccess(t, store, 2) // tarball node + packument node

	doc := npmTargetPackument(t, b, dstRepo, appendPkg)
	firstManifest := npmVersionManifestJSON(t, doc, "1.0.0")
	if tags, _ := doc["dist-tags"].(map[string]any); tags["latest"] != "1.0.0" {
		t.Fatalf("dist-tags after fresh sync = %v, want latest=1.0.0 (the single-tag face under the write-only grant)", tags)
	}
	if got := npmTargetTarball(t, b, dstRepo, appendPkg, "1.0.0"); !bytes.Equal(got, tar100) {
		t.Fatalf("tarball 1.0.0 on B drifted: %q", got)
	}

	// -- scenario 2: append (the packument node EXISTS; T-247's 403 shape) --
	tar101 := []byte("t262 append tarball 1.0.1 - second version")
	npmPublishOn(t, a, srcRepo, appendPkg, "1.0.1", tar101, "admin", a.adminPw)
	waitForAllTasksSuccess(t, store, 4)

	doc = npmTargetPackument(t, b, dstRepo, appendPkg)
	if got := npmVersionManifestJSON(t, doc, "1.0.0"); got != firstManifest {
		t.Fatalf("target's own 1.0.0 manifest rewritten by the append sync:\n got %s\nwant %s", got, firstManifest)
	}
	npmVersionManifestJSON(t, doc, "1.0.1") // must exist (Fatals inside if not)
	if tags, _ := doc["dist-tags"].(map[string]any); tags["latest"] != "1.0.1" {
		t.Fatalf("dist-tags after append sync = %v, want latest=1.0.1", tags)
	}
	if got := npmTargetTarball(t, b, dstRepo, appendPkg, "1.0.1"); !bytes.Equal(got, tar101) {
		t.Fatalf("tarball 1.0.1 on B drifted: %q", got)
	}

	// -- scenario 3: same version, different data (true rewrite never fired) --
	const conflictPkg = "@t262/conflict"
	tarB := []byte("t262 conflict tarball B-OWN bytes")
	tarA := []byte("t262 conflict tarball A bytes - DIFFERENT")
	npmPublishOn(t, b, dstRepo, conflictPkg, "1.0.0", tarB, "admin", b.adminPw) // target-side version first
	npmPublishOn(t, a, srcRepo, conflictPkg, "1.0.0", tarA, "admin", a.adminPw) // same version, other data
	waitForAllTasksSuccess(t, store, 6)                                         // both tasks succeed — the skip is NOT a failure

	if got := npmTargetTarball(t, b, dstRepo, conflictPkg, "1.0.0"); !bytes.Equal(got, tarB) {
		t.Fatalf("conflict tarball on B = %q, want the target's own bytes %q (first-write-wins; the source's rewrite must not land)", got, tarB)
	}
	cdoc := npmTargetPackument(t, b, dstRepo, conflictPkg)
	sha1B := sha1.Sum(tarB) //nolint:gosec // npm dist.shasum protocol digest
	if m := npmVersionManifestJSON(t, cdoc, "1.0.0"); !strings.Contains(m, hex.EncodeToString(sha1B[:])) {
		t.Fatalf("conflict manifest on B keeps the target's shasum: %s", m)
	}

	// -- control: the grant is honestly delete-less and the strict arm stands --
	dep := map[string]any{
		"_id": appendPkg, "name": appendPkg,
		"versions": map[string]any{
			"1.0.0": map[string]any{"name": appendPkg, "version": "1.0.0", "deprecated": "t262 control"},
		},
	}
	depBody, err := json.Marshal(dep)
	if err != nil {
		t.Fatalf("marshal deprecate document: %v", err)
	}
	resp, body := b.do(http.MethodPut, "/binflow/"+dstRepo+"/"+appendPkg, pushUser, pushPw, depBody)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("deprecate as %s = %d (%s), want 403 (field overwrite keeps the delete demand; the write-only grant must NOT pass it)", pushUser, resp.StatusCode, body)
	}
	resp, body = b.do(http.MethodPut, "/binflow/"+dstRepo+"/"+appendPkg, "admin", b.adminPw, depBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("deprecate as admin = %d (%s), want 201 (the strict arm is passable with delete, proving the arm is live)", resp.StatusCode, body)
	}
}
