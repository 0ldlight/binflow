package replication_test

// T-195 plane unit tests: the docker/npm/pypi push planes against scripted
// protocol targets, with the source-side metadata seam faked. These pin the
// WIRE SHAPES (addresses, headers, bodies, idempotence probes); the
// two-instance integration tests (engine_protocols integration, real
// adapters on both sides) pin the end-to-end behavior.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/replication"
)

// fakeMeta is the MetaSource stand-in: fixed package types, node facts and
// docker tag pointers.
type fakeMeta struct {
	pkg   map[string]string                           // repoKey -> package type
	nodes map[string]map[string]*replication.NodeMeta // repoKey -> path -> meta
	tags  map[string][]string                         // repoKey+"/"+image+"/"+hex -> tags
	props map[string]map[string][]string              // repoKey+"/"+path -> properties (T-317)
}

func (f *fakeMeta) PackageType(_ context.Context, repoKey string) (string, error) {
	if pt, ok := f.pkg[repoKey]; ok {
		return pt, nil
	}
	return "", fmt.Errorf("repo %s: %w", repoKey, replication.ErrMetaNotFound)
}

func (f *fakeMeta) Node(_ context.Context, repoKey, path string) (*replication.NodeMeta, error) {
	if m, ok := f.nodes[repoKey][path]; ok && m != nil {
		return m, nil
	}
	return nil, fmt.Errorf("node %s/%s: %w", repoKey, path, replication.ErrMetaNotFound)
}

func (f *fakeMeta) DockerTags(_ context.Context, repoKey, image, digestHex string) ([]string, error) {
	return f.tags[repoKey+"/"+image+"/"+digestHex], nil
}

func (f *fakeMeta) NodeProps(_ context.Context, repoKey, path string) (map[string][]string, error) {
	return f.props[repoKey+"/"+path], nil
}

// protoTarget records one request against a scripted protocol target.
type protoReq struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   string
}

// protoTarget is a programmable target that answers from a route table keyed
// by "METHOD path?query"; unmatched requests answer 404 (the honest default
// every BinFlow plane gives). Bodies of matched routes are still recorded.
type protoTarget struct {
	mu     sync.Mutex
	routes map[string]int // "METHOD path" -> status (special-cased below)
	heads  map[string]http.Header
	bodies map[string]string // "METHOD path" -> response body
	reqs   []protoReq
}

func newProtoTarget() *protoTarget {
	return &protoTarget{
		routes: map[string]int{},
		heads:  map[string]http.Header{},
		bodies: map[string]string{},
	}
}

func (p *protoTarget) set(method, path string, status int, hdr http.Header, body string) {
	p.routes[method+" "+path] = status
	if hdr != nil {
		p.heads[method+" "+path] = hdr
	}
	p.bodies[method+" "+path] = body
}

func (p *protoTarget) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	p.mu.Lock()
	p.reqs = append(p.reqs, protoReq{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
		Header: r.Header.Clone(), Body: string(body),
	})
	status, ok := p.routes[r.Method+" "+r.URL.Path]
	hdr := p.heads[r.Method+" "+r.URL.Path]
	respBody := p.bodies[r.Method+" "+r.URL.Path]
	p.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	for k, vv := range hdr {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(status)
	if respBody != "" {
		_, _ = w.Write([]byte(respBody))
	}
}

func (p *protoTarget) requests() []protoReq {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]protoReq(nil), p.reqs...)
}

// newPlaneFixture wires an engine over the real 009 store plus a scripted
// protocol target and the fake metadata seam (the plane selector needs it).
func newPlaneFixture(t *testing.T, target *protoTarget, meta *fakeMeta, cfgMutate func(*replication.ReplicationConfig)) *engineFixture {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(target.handler))
	t.Cleanup(server.Close)
	return newEngineFixture(t, &scriptTarget{}, &replication.EngineOptions{Meta: meta}, func(c *replication.ReplicationConfig) {
		c.TargetURL = server.URL
		if cfgMutate != nil {
			cfgMutate(c)
		}
	})
}

// ---- docker plane (D2) ----

// TestDockerBlobPushAddresses pins the blob leg's wire shape: HEAD the
// digest address, then the single-request monolithic upload with ?digest=.
func TestDockerBlobPushAddresses(t *testing.T) {
	hexSum := testSHA256(testPayload)
	nodePath := "t195/app/blobs/" + hexSum
	meta := &fakeMeta{pkg: map[string]string{"libs-local": "docker"}}
	target := newProtoTarget()
	// The probe miss; the upload answers 201 with the digest confirmation.
	target.set(http.MethodHead, "/v2/mirror/t195/app/blobs/sha256:"+hexSum, http.StatusNotFound, nil, "")
	hdr := http.Header{}
	hdr.Set("Docker-Content-Digest", "sha256:"+hexSum)
	target.set(http.MethodPost, "/v2/mirror/t195/app/blobs/uploads", http.StatusCreated, hdr, "")

	f := newPlaneFixture(t, target, meta, nil)
	f.blobs.content[hexSum] = testPayload
	f.engine.Enqueue(f.ctx, "libs-local", nodePath, hexSum)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")

	reqs := target.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (HEAD probe + POST upload)", len(reqs))
	}
	if reqs[0].Method != http.MethodHead || reqs[0].Path != "/v2/mirror/t195/app/blobs/sha256:"+hexSum {
		t.Fatalf("probe = %s %s, want HEAD of the digest address", reqs[0].Method, reqs[0].Path)
	}
	if reqs[1].Method != http.MethodPost || reqs[1].Path != "/v2/mirror/t195/app/blobs/uploads" {
		t.Fatalf("upload = %s %s, want POST of the uploads route", reqs[1].Method, reqs[1].Path)
	}
	if reqs[1].Query != "digest=sha256%3A"+hexSum {
		t.Fatalf("upload query = %q, want the sha256 digest parameter", reqs[1].Query)
	}
	if reqs[1].Body != testPayload {
		t.Fatalf("upload body = %q, want the blob bytes", reqs[1].Body)
	}
}

// TestDockerBlobIdempotentHit: the target already serves the digest — zero
// uploads, zero source blob opens.
func TestDockerBlobIdempotentHit(t *testing.T) {
	hexSum := testSHA256(testPayload)
	nodePath := "t195/app/blobs/" + hexSum
	meta := &fakeMeta{pkg: map[string]string{"libs-local": "docker"}}
	target := newProtoTarget()
	hdr := http.Header{}
	hdr.Set("Docker-Content-Digest", "sha256:"+hexSum)
	target.set(http.MethodHead, "/v2/mirror/t195/app/blobs/sha256:"+hexSum, http.StatusOK, hdr, "")

	f := newPlaneFixture(t, target, meta, nil)
	f.engine.Enqueue(f.ctx, "libs-local", nodePath, hexSum)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")
	if reqs := target.requests(); len(reqs) != 1 || reqs[0].Method != http.MethodHead {
		t.Fatalf("requests = %+v, want exactly the HEAD probe", reqs)
	}
	if opens := len(f.blobs.opens); opens != 0 {
		t.Fatalf("source blob opened %d times, want 0", opens)
	}
}

// TestDockerManifestPushWithTags pins the manifest leg: PUT by digest with
// the node row's mime, then one PUT per source tag pointer.
func TestDockerManifestPushWithTags(t *testing.T) {
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json"}`
	hexSum := testSHA256(manifest)
	nodePath := "t195/app/manifests/" + hexSum
	meta := &fakeMeta{
		pkg: map[string]string{"libs-local": "docker"},
		nodes: map[string]map[string]*replication.NodeMeta{
			"libs-local": {nodePath: {Mime: "application/vnd.docker.distribution.manifest.v2+json"}},
		},
		tags: map[string][]string{"libs-local/t195/app/" + hexSum: {"1.0", "latest"}},
	}
	target := newProtoTarget()
	dgstHdr := http.Header{}
	dgstHdr.Set("Docker-Content-Digest", "sha256:"+hexSum)
	target.set(http.MethodHead, "/v2/mirror/t195/app/manifests/sha256:"+hexSum, http.StatusNotFound, nil, "")
	target.set(http.MethodPut, "/v2/mirror/t195/app/manifests/sha256:"+hexSum, http.StatusCreated, dgstHdr, "")
	target.set(http.MethodGet, "/v2/mirror/t195/app/manifests/1.0", http.StatusNotFound, nil, "")
	target.set(http.MethodPut, "/v2/mirror/t195/app/manifests/1.0", http.StatusCreated, dgstHdr, "")
	// "latest" already points here: the GET confirms it, no PUT follows.
	target.set(http.MethodGet, "/v2/mirror/t195/app/manifests/latest", http.StatusOK, dgstHdr, manifest)

	f := newPlaneFixture(t, target, meta, nil)
	f.blobs.content[hexSum] = manifest
	f.engine.Enqueue(f.ctx, "libs-local", nodePath, hexSum)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")

	reqs := target.requests()
	var puts []protoReq
	for _, r := range reqs {
		if r.Method == http.MethodPut {
			puts = append(puts, r)
		}
	}
	if len(puts) != 2 {
		t.Fatalf("PUTs = %d, want 2 (digest + the drifted tag)", len(puts))
	}
	for _, put := range puts {
		if ct := put.Header.Get("Content-Type"); ct != "application/vnd.docker.distribution.manifest.v2+json" {
			t.Fatalf("PUT %s Content-Type = %q, want the stored manifest mime", put.Path, ct)
		}
		if put.Body != manifest {
			t.Fatalf("PUT %s body = %q, want the exact manifest bytes", put.Path, put.Body)
		}
	}
	if puts[0].Path != "/v2/mirror/t195/app/manifests/sha256:"+hexSum ||
		puts[1].Path != "/v2/mirror/t195/app/manifests/1.0" {
		t.Fatalf("PUT paths = %q then %q, want digest first then the repointed tag",
			puts[0].Path, puts[1].Path)
	}
}

// TestDockerPlaneRejectsForeignLayout: a task whose node path is not the
// docker layout fails terminally without a single packet to the target.
func TestDockerPlaneRejectsForeignLayout(t *testing.T) {
	meta := &fakeMeta{pkg: map[string]string{"libs-local": "docker"}}
	f := newPlaneFixture(t, newProtoTarget(), meta, nil)
	f.engine.Enqueue(f.ctx, "libs-local", "org/app/1.bin", testSHA256(testPayload))
	task := f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusFailed
	}, "terminal failure (not the docker layout)")
	if !strings.Contains(task.LastError, "not retryable") {
		t.Fatalf("LastError = %q, want the not-retryable marker", task.LastError)
	}
}

// ---- pypi plane ----

// TestPypiPushMultipartShape pins the upload leg: probe on the bare content
// entrance, then the warehouse multipart POST at the repository root.
func TestPypiPushMultipartShape(t *testing.T) {
	wheel := "fake wheel bytes for the pypi plane"
	hexSum := testSHA256(wheel)
	nodePath := "demo/1.0.0/demo-1.0.0-py3-none-any.whl"
	meta := &fakeMeta{
		pkg: map[string]string{"libs-local": "pypi"},
		nodes: map[string]map[string]*replication.NodeMeta{
			"libs-local": {nodePath: {Mime: "application/zip"}},
		},
	}
	target := newProtoTarget()
	target.set(http.MethodHead, "/binflow/mirror/"+nodePath, http.StatusNotFound, nil, "")
	target.set(http.MethodPost, "/binflow/mirror", http.StatusOK, nil, "")

	f := newPlaneFixture(t, target, meta, nil)
	f.blobs.content[hexSum] = wheel
	f.engine.Enqueue(f.ctx, "libs-local", nodePath, hexSum)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")

	reqs := target.requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (HEAD probe + POST upload)", len(reqs))
	}
	post := reqs[1]
	if post.Method != http.MethodPost || post.Path != "/binflow/mirror" {
		t.Fatalf("upload = %s %s, want POST at the repository root", post.Method, post.Path)
	}
	ct := post.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "multipart/form-data; boundary=") {
		t.Fatalf("Content-Type = %q, want multipart/form-data", ct)
	}
	for _, want := range []string{
		`:action`, `file_upload`, `name`, `demo`, `version`, `1.0.0`,
		`sha256_digest`, hexSum, `filename="demo-1.0.0-py3-none-any.whl"`, wheel,
	} {
		if !strings.Contains(post.Body, want) {
			t.Fatalf("multipart body misses %q:\n%s", want, post.Body)
		}
	}
}

// TestPypiPushIdempotentHit: the target already serves the same checksum —
// no upload, no source blob open.
func TestPypiPushIdempotentHit(t *testing.T) {
	hexSum := testSHA256("already there")
	nodePath := "demo/1.0.0/demo-1.0.0-py3-none-any.whl"
	meta := &fakeMeta{pkg: map[string]string{"libs-local": "pypi"}}
	target := newProtoTarget()
	hdr := http.Header{}
	hdr.Set("X-Checksum-Sha256", hexSum)
	target.set(http.MethodHead, "/binflow/mirror/"+nodePath, http.StatusOK, hdr, "")

	f := newPlaneFixture(t, target, meta, nil)
	f.engine.Enqueue(f.ctx, "libs-local", nodePath, hexSum)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")
	if reqs := target.requests(); len(reqs) != 1 || reqs[0].Method != http.MethodHead {
		t.Fatalf("requests = %+v, want exactly the probe", reqs)
	}
	if opens := len(f.blobs.opens); opens != 0 {
		t.Fatalf("source blob opened %d times, want 0", opens)
	}
}

// ---- npm plane (D3) ----

// npmFixtureDoc builds a stored source packument with one version whose
// tarball rides fakeBlobs.
func npmFixtureDoc(name, version string) (npmDoc2, string) {
	tarPath := name + "/-/" + name + "-" + version + ".tgz"
	doc := npmDoc2{
		"_id":  name,
		"name": name,
		"versions": map[string]any{
			version: map[string]any{
				"name": name, "version": version,
				"dist": map[string]any{"tarball": "/" + tarPath},
			},
		},
		"dist-tags": map[string]any{"latest": version},
	}
	return doc, tarPath
}

type npmDoc2 = map[string]any

// TestNpmSyncPublishesVersionAndTags pins the convergence wire shape: GET
// the target packument, PUT the one-version publish document with the
// base64 attachment, PUT the drifted dist-tag.
func TestNpmSyncPublishesVersionAndTags(t *testing.T) {
	const name, version = "@t195/test-pkg", "1.0.1"
	tarball := "gzip-ish tarball bytes"
	tarHex := testSHA256(tarball)
	doc, tarPath := npmFixtureDoc(name, version)
	raw, _ := json.Marshal(doc)
	docHex := testSHA256(string(raw))

	meta := &fakeMeta{
		pkg: map[string]string{"libs-local": "npm"},
		nodes: map[string]map[string]*replication.NodeMeta{
			"libs-local": {
				name + "/packument.json": {Sha256: docHex},
				tarPath:                  {Sha256: tarHex},
			},
		},
	}
	target := newProtoTarget()
	target.set(http.MethodGet, "/binflow/mirror/"+name, http.StatusNotFound, nil, "")
	target.set(http.MethodPut, "/binflow/mirror/"+name, http.StatusCreated, nil, `{"success":true}`)
	target.set(http.MethodPut, "/binflow/mirror/-/package/"+name+"/dist-tags/latest", http.StatusCreated, nil, `{"ok":"created new tag"}`)

	f := newPlaneFixture(t, target, meta, nil)
	f.blobs.content[docHex] = string(raw)
	f.blobs.content[tarHex] = tarball
	// Both node kinds converge the package; drive the packument task.
	f.engine.Enqueue(f.ctx, "libs-local", name+"/packument.json", docHex)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")

	reqs := target.requests()
	if len(reqs) != 3 {
		for _, r := range reqs {
			t.Logf("saw %s %s", r.Method, r.Path)
		}
		t.Fatalf("requests = %d, want 3 (GET packument, PUT publish, PUT dist-tag)", len(reqs))
	}
	if reqs[0].Method != http.MethodGet || reqs[0].Path != "/binflow/mirror/"+name {
		t.Fatalf("packument fetch = %s %s", reqs[0].Method, reqs[0].Path)
	}
	pub := reqs[1]
	if pub.Method != http.MethodPut || pub.Path != "/binflow/mirror/"+name {
		t.Fatalf("publish = %s %s", pub.Method, pub.Path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(pub.Body), &sent); err != nil {
		t.Fatalf("publish body is not JSON: %v", err)
	}
	atts, ok := sent["_attachments"].(map[string]any)
	if !ok || len(atts) != 1 {
		t.Fatalf("publish _attachments = %v, want the one tarball", sent["_attachments"])
	}
	for key, av := range atts {
		if key != name+"-"+version+".tgz" {
			t.Fatalf("attachment key = %q, want the tarball filename", key)
		}
		data, _ := av.(map[string]any)["data"].(string)
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil || string(decoded) != tarball {
			t.Fatalf("attachment data = %q (%v), want the tarball bytes", data, err)
		}
	}
	if tag := reqs[2]; tag.Method != http.MethodPut || tag.Path != "/binflow/mirror/-/package/"+name+"/dist-tags/latest" {
		t.Fatalf("dist-tag = %s %s", tag.Method, tag.Path)
	}
	if body := strings.TrimSpace(reqs[2].Body); body != `"1.0.1"` {
		t.Fatalf("dist-tag body = %q, want the JSON-string version", body)
	}
}

// TestNpmSyncIdempotentSkip: the target already serves the version AND the
// tag — one GET, zero writes.
func TestNpmSyncIdempotentSkip(t *testing.T) {
	const name, version = "plain-pkg", "2.0.0"
	tarball := "tarball"
	tarHex := testSHA256(tarball)
	doc, tarPath := npmFixtureDoc(name, version)
	raw, _ := json.Marshal(doc)
	docHex := testSHA256(string(raw))

	meta := &fakeMeta{
		pkg: map[string]string{"libs-local": "npm"},
		nodes: map[string]map[string]*replication.NodeMeta{
			"libs-local": {
				name + "/packument.json": {Sha256: docHex},
				tarPath:                  {Sha256: tarHex},
			},
		},
	}
	targetDoc := map[string]any{
		"versions":  map[string]any{version: map[string]any{"name": name}},
		"dist-tags": map[string]any{"latest": version},
	}
	served, _ := json.Marshal(targetDoc)
	target := newProtoTarget()
	target.set(http.MethodGet, "/binflow/mirror/"+name, http.StatusOK, nil, string(served))

	f := newPlaneFixture(t, target, meta, nil)
	f.blobs.content[docHex] = string(raw)
	f.blobs.content[tarHex] = tarball
	f.engine.Enqueue(f.ctx, "libs-local", tarPath, tarHex)
	f.waitTask(t, func(task *replication.ReplicationTask) bool {
		return task.Status == replication.TaskStatusSuccess
	}, "success")
	if reqs := target.requests(); len(reqs) != 1 || reqs[0].Method != http.MethodGet {
		t.Fatalf("requests = %+v, want exactly the packument GET", reqs)
	}
}
