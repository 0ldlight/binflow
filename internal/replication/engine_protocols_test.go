package replication_test

// T-195 AC ⑤: the two-instance integration scenario across the four
// protocols (generic/docker/npm/pypi). Both instances are full in-process
// assemblies — every protocol adapter mounted, exactly like cmd's
// newAssembledServer — so the source legs drive the REAL client-facing
// upload faces (docker /v2, npm publish, pypi multipart) and the engine's
// protocol planes must then land the same content on the target's
// corresponding faces. generic keeps its T-162 coverage in
// engine_integration_test.go; it is re-pinned here once on the full-adapter
// stack to prove no dispatch regression.

import (
	"bytes"
	"context"
	"crypto/sha1" //nolint:gosec // npm dist.shasum protocol digest
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/docker"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/adapter/npm"
	"github.com/lzwzzy/binflow/internal/adapter/pypi"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/replication"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// newBinFlowFull assembles one instance with EVERY protocol adapter mounted
// (cmd's newAssembledServer composition minus the global Register calls —
// those are process-wide singletons, and a test binary builds many stacks).
func newBinFlowFull(t *testing.T, name, adminPw string, repos []*metadata.Repo) *binflow {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("%s: storage.OpenEngine: %v", name, err)
	}
	t.Cleanup(func() { _ = st.Close() })

	md, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite", Path: dataDir + "/binflow.db", AdminPassword: adminPw,
	})
	if err != nil {
		t.Fatalf("%s: metadata.Open: %v", name, err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := fullStackConfig(t, dataDir)
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	svc := repo.New(st, md, authSvc, audit.New(md, true))

	admin := &repo.Principal{Name: "admin", Admin: true}
	for _, r := range repos {
		r.CreatedAt = metadata.Now()
		r.UpdatedAt = metadata.Now()
		if _, err := svc.CreateRepo(ctx, admin, r); err != nil {
			t.Fatalf("%s: CreateRepo %s: %v", name, r.RepoKey, err)
		}
	}

	dockerHandler := docker.New(svc, docker.NewRepoLookup(md.Repos()),
		authSvc, authSvc, md.Users(), docker.Options{
			AnonymousAccess: cfg.Security.AnonymousAccess,
			TokenTTL:        time.Hour,
		}, nil).
		WithStorage(st, md.Blobs())
	npmHandler := npm.New(svc, md.Repos(), npm.Options{}).
		WithAuth(authSvc, md.Users(), authSvc).
		WithLedger(md.Blobs())
	pypiHandler := pypi.New(svc, md.Repos(), md.Blobs(), st)

	srv := httpapi.New(httpapi.Deps{
		Config:    cfg,
		Auth:      authSvc,
		Authz:     authSvc,
		Metadata:  md,
		Repos:     md.Repos(),
		ReposSvc:  svc,
		Passwords: authSvc,
		Tokens:    authSvc,
		GC:        st,
		DataDir:   dataDir,
		Adapters:  []adapter.Handler{generic.New(svc, md.Blobs()), dockerHandler, npmHandler, pypiHandler},
		Version:   "test",
	}, nil)
	ts := httpapiServer(t, srv)
	return &binflow{t: t, url: ts.URL, dataDir: dataDir, svc: svc, st: st, md: md, adminPw: adminPw}
}

// startReplication wires one engine on the source instance toward the target
// repository, with the metadata seam the protocol planes select on. Returns
// the store for task polling. Credentials are the target's admin — the
// T-262 non-admin leg uses startReplicationAs.
func startReplication(t *testing.T, a *binflow, target *binflow, sourceRepo, targetRepo string) replication.Store {
	t.Helper()
	return startReplicationAs(t, a, target, sourceRepo, targetRepo, "admin", target.adminPw)
}

// waitForAllTasksSuccess polls until the config holds n successful tasks
// (and no failure), then returns them.
func waitForAllTasksSuccess(t *testing.T, store replication.Store, n int) []*replication.ReplicationTask {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		configs, err := store.ListConfigs(context.Background())
		if err != nil || len(configs) != 1 {
			t.Fatalf("ListConfigs: %v (%d)", err, len(configs))
		}
		tasks, err := store.ListTasks(context.Background(), configs[0].ID, 50)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		ok, failed := 0, 0
		for _, task := range tasks {
			switch task.Status {
			case replication.TaskStatusSuccess:
				ok++
			case replication.TaskStatusFailed:
				failed++
			}
		}
		if failed > 0 {
			for _, task := range tasks {
				if task.Status == replication.TaskStatusFailed {
					t.Fatalf("task %s failed: %s", task.NodePath, task.LastError)
				}
			}
		}
		if ok >= n {
			return tasks
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d successful tasks", n)
	return nil
}

// ---- docker (D2) ----

// TestTwoInstanceDockerReplication: docker push on A (monolithic blob
// uploads + tagged manifest PUT) replicates through the /v2 plane, and B
// serves the image BY TAG with the same digest and layer bytes.
func TestTwoInstanceDockerReplication(t *testing.T) {
	const image = "t195/hello"
	b := newBinFlowFull(t, "B-docker", "pw-b-docker", []*metadata.Repo{
		{RepoKey: "docker-dst", Type: repo.TypeLocal, PackageType: repo.PackageDocker, Config: "{}"},
	})
	a := newBinFlowFull(t, "A-docker", "pw-a-docker", []*metadata.Repo{
		{RepoKey: "docker-src", Type: repo.TypeLocal, PackageType: repo.PackageDocker, Config: "{}"},
	})
	store := startReplication(t, a, b, "docker-src", "docker-dst")

	layer := []byte("the layer payload of the t195 image")
	config := []byte(`{"architecture":"amd64","os":"linux","rootfs":{"type":"layers","diff_ids":[]}}`)
	layerHex, configHex := sha256Hex(layer), sha256Hex(config)
	for _, blob := range []struct {
		hex  string
		body []byte
	}{
		{layerHex, layer},
		{configHex, config},
	} {
		resp, _ := a.do(http.MethodPost,
			"/v2/docker-src/"+image+"/blobs/uploads?digest=sha256:"+blob.hex, "admin", "pw-a-docker", blob.body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("blob upload on A: status %d, want 201", resp.StatusCode)
		}
	}

	manifest := fmt.Sprintf(`{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",`+
		`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:%s","size":%d},`+
		`"layers":[{"mediaType":"application/vnd.docker.image.rootfs.diff.tar.gzip","digest":"sha256:%s","size":%d}]}`,
		configHex, len(config), layerHex, len(layer))
	manifestHex := sha256Hex([]byte(manifest))
	req, _ := http.NewRequest(http.MethodPut, a.url+"/v2/docker-src/"+image+"/manifests/1", strings.NewReader(manifest))
	req.SetBasicAuth("admin", "pw-a-docker")
	req.Header.Set("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("manifest PUT on A: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("manifest PUT on A: status %d, want 201", resp.StatusCode)
	}

	// Four tasks: config, layer, manifest body, manifest node (+tags).
	waitForAllTasksSuccess(t, store, 4)

	// B serves the tag with the same digest, the manifest body bit-for-bit,
	// and the layer bytes.
	mresp, mbody := b.getWithAccept("/v2/docker-dst/"+image+"/manifests/1",
		"application/vnd.docker.distribution.manifest.v2+json")
	if mresp.StatusCode != http.StatusOK {
		t.Fatalf("manifest by tag on B: status %d (%s), want 200", mresp.StatusCode, mbody)
	}
	if got := mresp.Header.Get("Docker-Content-Digest"); got != "sha256:"+manifestHex {
		t.Fatalf("B tag resolves to digest %q, want sha256:%s", got, manifestHex)
	}
	if string(mbody) != manifest {
		t.Fatalf("B manifest body drifted:\n got %s\nwant %s", mbody, manifest)
	}
	dresp, dbody := b.getWithAccept("/v2/docker-dst/"+image+"/manifests/sha256:"+manifestHex,
		"application/vnd.docker.distribution.manifest.v2+json")
	if dresp.StatusCode != http.StatusOK || !bytes.Equal(dbody, []byte(manifest)) {
		t.Fatalf("manifest by digest on B: status %d, want 200 with the same bytes", dresp.StatusCode)
	}
	lresp, lbody := b.do(http.MethodGet, "/v2/docker-dst/"+image+"/blobs/sha256:"+layerHex, "", "", nil)
	if lresp.StatusCode != http.StatusOK || !bytes.Equal(lbody, layer) {
		t.Fatalf("layer on B: status %d bytes-equal=%t, want 200/true", lresp.StatusCode, bytes.Equal(lbody, layer))
	}
}

// ---- npm (D3) ----

// TestTwoInstanceNpmReplication: npm publish on A replicates through the
// publish + dist-tag faces, and B's registry serves the packument (version
// AND dist-tag) plus the tarball with the same bytes.
func TestTwoInstanceNpmReplication(t *testing.T) {
	const name, version = "@t195/repkg", "1.0.1"
	b := newBinFlowFull(t, "B-npm", "pw-b-npm", []*metadata.Repo{
		{RepoKey: "npm-dst", Type: repo.TypeLocal, PackageType: repo.PackageNpm, Config: "{}"},
	})
	a := newBinFlowFull(t, "A-npm", "pw-a-npm", []*metadata.Repo{
		{RepoKey: "npm-src", Type: repo.TypeLocal, PackageType: repo.PackageNpm, Config: "{}"},
	})
	store := startReplication(t, a, b, "npm-src", "npm-dst")

	tarball := []byte("a pretend npm tarball for t195")
	sha512Sum := sha512.Sum512(tarball)
	sha1Sum := sha1.Sum(tarball) //nolint:gosec // dist.shasum protocol digest
	publish := map[string]any{
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
	body, _ := json.Marshal(publish)
	resp, _ := a.do(http.MethodPut, "/binflow/npm-src/"+name, "admin", "pw-a-npm", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("npm publish on A: status %d, want 201", resp.StatusCode)
	}

	// Two tasks: the tarball node and the packument node.
	waitForAllTasksSuccess(t, store, 2)

	presp, pbody := b.getWithAccept("/binflow/npm-dst/"+name, "application/json")
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("packument on B: status %d (%s), want 200", presp.StatusCode, pbody)
	}
	var doc map[string]any
	if err := json.Unmarshal(pbody, &doc); err != nil {
		t.Fatalf("packument on B is not JSON: %v", err)
	}
	versions, _ := doc["versions"].(map[string]any)
	if versions[version] == nil {
		t.Fatalf("packument on B lacks version %s: %s", version, pbody)
	}
	tags, _ := doc["dist-tags"].(map[string]any)
	if tags["latest"] != version {
		t.Fatalf("dist-tags on B = %v, want latest=%s (the dist-tag face must converge)", tags, version)
	}

	tresp, tbody := b.do(http.MethodGet,
		"/binflow/npm-dst/"+name+"/-/"+name+"-"+version+".tgz", "", "", nil)
	if tresp.StatusCode != http.StatusOK || !bytes.Equal(tbody, tarball) {
		t.Fatalf("tarball on B: status %d bytes-equal=%t, want 200/true", tresp.StatusCode, bytes.Equal(tbody, tarball))
	}
}

// ---- pypi (D4) ----

// TestTwoInstancePypiReplication: a twine-style multipart upload on A (the
// PutLandedBlob chain T-175 D4 found unhooked) enqueues and lands on B
// through the warehouse upload face; B's simple index and download face
// serve it.
func TestTwoInstancePypiReplication(t *testing.T) {
	const filename = "demo-2.1.0-py3-none-any.whl"
	b := newBinFlowFull(t, "B-pypi", "pw-b-pypi", []*metadata.Repo{
		{RepoKey: "pypi-dst", Type: repo.TypeLocal, PackageType: repo.PackagePypi, Config: "{}"},
	})
	a := newBinFlowFull(t, "A-pypi", "pw-a-pypi", []*metadata.Repo{
		{RepoKey: "pypi-src", Type: repo.TypeLocal, PackageType: repo.PackagePypi, Config: "{}"},
	})
	store := startReplication(t, a, b, "pypi-src", "pypi-dst")

	wheel := []byte("a wheel-shaped byte string for t195")
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	for field, value := range map[string]string{
		":action":       "file_upload",
		"name":          "demo",
		"version":       "2.1.0",
		"sha256_digest": sha256Hex(wheel),
	} {
		if err := mw.WriteField(field, value); err != nil {
			t.Fatalf("WriteField %s: %v", field, err)
		}
	}
	fw, err := mw.CreateFormFile("content", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(wheel); err != nil {
		t.Fatalf("write wheel: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("Close multipart: %v", err)
	}

	req, _ := http.NewRequest(http.MethodPost, a.url+"/binflow/pypi-src", bytes.NewReader(form.Bytes()))
	req.SetBasicAuth("admin", "pw-a-pypi")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("pypi upload on A: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pypi upload on A: status %d, want 200", resp.StatusCode)
	}

	// One task: the distribution file node (the D4 hook).
	waitForAllTasksSuccess(t, store, 1)

	sresp, sbody := b.do(http.MethodGet, "/binflow/pypi-dst/simple/demo/", "", "", nil)
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("simple index on B: status %d (%s), want 200", sresp.StatusCode, sbody)
	}
	if !strings.Contains(string(sbody), filename) {
		t.Fatalf("simple index on B lacks %s:\n%s", filename, sbody)
	}
	dresp, dbody := b.do(http.MethodGet, "/binflow/pypi-dst/demo/2.1.0/"+filename, "", "", nil)
	if dresp.StatusCode != http.StatusOK || !bytes.Equal(dbody, wheel) {
		t.Fatalf("wheel on B: status %d bytes-equal=%t, want 200/true", dresp.StatusCode, bytes.Equal(dbody, wheel))
	}
}

// ---- generic (regression pin on the full-adapter stack) ----

// TestTwoInstanceGenericReplicationOnFullStack re-pins the T-162 generic
// path with every adapter mounted: plane selection must leave generic
// repositories on the plain REST push.
func TestTwoInstanceGenericReplicationOnFullStack(t *testing.T) {
	b := newBinFlowFull(t, "B-generic", "pw-b-generic", []*metadata.Repo{
		{RepoKey: "generic-dst", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	a := newBinFlowFull(t, "A-generic", "pw-a-generic", []*metadata.Repo{
		{RepoKey: "generic-src", Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: "{}"},
	})
	store := startReplication(t, a, b, "generic-src", "generic-dst")

	payload := []byte("generic payload on the full adapter stack")
	resp, _ := a.do(http.MethodPut, "/binflow/generic-src/org/app/1.bin", "admin", "pw-a-generic", payload)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload on A: status %d, want 201", resp.StatusCode)
	}
	waitForAllTasksSuccess(t, store, 1)
	gresp, gbody := b.do(http.MethodGet, "/binflow/generic-dst/org/app/1.bin", "", "", nil)
	if gresp.StatusCode != http.StatusOK || !bytes.Equal(gbody, payload) {
		t.Fatalf("generic on B: status %d bytes-equal=%t, want 200/true", gresp.StatusCode, bytes.Equal(gbody, payload))
	}
	if got := gresp.Header.Get("X-Checksum-Sha256"); got != sha256Hex(payload) {
		t.Fatalf("X-Checksum-Sha256 on B = %q, want %q", got, sha256Hex(payload))
	}
}

// ---- helpers ----

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fullStackConfig builds the shared assembly config for one instance.
func fullStackConfig(_ *testing.T, dataDir string) *config.Config {
	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	return cfg
}

// httpapiServer runs one assembled server behind an httptest listener.
func httpapiServer(t *testing.T, srv *httpapi.Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// getWithAccept issues an anonymous GET with one Accept value.
func (b *binflow) getWithAccept(path, accept string) (*http.Response, []byte) {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.url+path, nil)
	if err != nil {
		b.t.Fatalf("build GET %s: %v", path, err)
	}
	req.Header.Set("Accept", accept)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}
