package repo_test

// T-423 (FR-139.2, T-377 D1): the registry-v2 cache-fill row writes ride
// the busy retry budget (the service half's mirror of the engine's land()
// wraps). Each write site fails once busy-class and the landing must still
// complete — the pull that already paid for the upstream transfer never
// surfaces a 500 for transient store contention.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// busyErr builds a store-shaped busy-class error (the wrapExec product
// shape; the classifier contract is errors.Is-based).
func busyErr(label string) error {
	return fmt.Errorf("metadata: %s: %w: database is locked (5) (SQLITE_BUSY)",
		label, metadata.ErrStoreBusy)
}

// busyFailOnce wraps a sub-store method: the first failN calls answer
// busy-class, then the delegate takes over. calls counts executions.
type busyFailOnce struct {
	fail  atomic.Int64
	calls atomic.Int64
}

func (b *busyFailOnce) take() bool  { b.calls.Add(1); return b.fail.Add(-1) >= 0 }
func (b *busyFailOnce) arm(n int64) { b.fail.Store(n) }

type busyMD struct {
	metadata.Store
	blobFail, nodeFail, cacheFail   busyFailOnce
	manifestFail, tagFail, refsFail busyFailOnce
}

type busyBlobs struct {
	metadata.BlobStore
	parent *busyMD
}

func (b busyBlobs) Put(ctx context.Context, blob *metadata.Blob) error {
	if b.parent.blobFail.take() {
		return busyErr("blobs put")
	}
	return b.BlobStore.Put(ctx, blob)
}

type busyNodes struct {
	metadata.NodeStore
	parent *busyMD
}

func (b busyNodes) Put(ctx context.Context, n *metadata.Node) error {
	if b.parent.nodeFail.take() {
		return busyErr("nodes put")
	}
	return b.NodeStore.Put(ctx, n)
}

type busyRemote struct {
	metadata.RemoteStore
	parent *busyMD
}

func (b busyRemote) PutCache(ctx context.Context, e *metadata.RemoteCacheEntry) error {
	if b.parent.cacheFail.take() {
		return busyErr("remote-cache put")
	}
	return b.RemoteStore.PutCache(ctx, e)
}

type busyDocker struct {
	metadata.DockerStore
	parent *busyMD
}

func (b busyDocker) PutManifest(ctx context.Context, m *metadata.DockerManifest) error {
	if b.parent.manifestFail.take() {
		return busyErr("docker manifest put")
	}
	return b.DockerStore.PutManifest(ctx, m)
}

func (b busyDocker) PutTag(ctx context.Context, t *metadata.DockerTag) error {
	if b.parent.tagFail.take() {
		return busyErr("docker tag put")
	}
	return b.DockerStore.PutTag(ctx, t)
}

func (b busyDocker) PutRefs(ctx context.Context, repoKey, image, digest string, refs []*metadata.DockerRef) error {
	if b.parent.refsFail.take() {
		return busyErr("docker refs put")
	}
	return b.DockerStore.PutRefs(ctx, repoKey, image, digest, refs)
}

func (m *busyMD) Blobs() metadata.BlobStore    { return busyBlobs{m.Store.Blobs(), m} }
func (m *busyMD) Nodes() metadata.NodeStore    { return busyNodes{m.Store.Nodes(), m} }
func (m *busyMD) Remote() metadata.RemoteStore { return busyRemote{m.Store.Remote(), m} }
func (m *busyMD) Docker() metadata.DockerStore { return busyDocker{m.Store.Docker(), m} }

func sha256HexBody(b string) string {
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// TestRemoteV2LandRetriesBusyRowWritesAndCompletes: one busy failure at
// each of the landing's three row sites (blob, node, cache state) — the
// landing still completes and every row is queryable afterwards.
func TestRemoteV2LandRetriesBusyRowWritesAndCompletes(t *testing.T) {
	var wrap *busyMD
	e := newEnvCustom(t, func(md metadata.Store) metadata.Store {
		wrap = &busyMD{Store: md}
		return wrap
	})
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	body := "chart-body-under-contention"
	hexd := sha256HexBody(body)
	path := "mychart/manifests/" + hexd

	wrap.blobFail.arm(1)
	wrap.nodeFail.arm(1)
	wrap.cacheFail.arm(1)

	node, err := plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", path, hexd,
		"application/vnd.oci.image.manifest.v1+json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("LandRemoteBlob under busy rows: %v", err)
	}
	if node.Sha256 != hexd || node.Size != int64(len(body)) {
		t.Fatalf("landed node = %+v, want the measured digest and size", node)
	}
	for _, site := range []struct {
		name        string
		calls, want int64
	}{
		{"blob row", wrap.blobFail.calls.Load(), 2},
		{"node row", wrap.nodeFail.calls.Load(), 2},
		{"cache-state row", wrap.cacheFail.calls.Load(), 2},
	} {
		if site.calls < site.want {
			t.Fatalf("%s executed %d times, want the retry to fire", site.name, site.calls)
		}
	}
	if _, err := e.md.Nodes().Get(ctx, "helmoci-remote", path); err != nil {
		t.Fatalf("node row missing after the budgeted landing: %v", err)
	}
	if entry, err := e.md.Remote().GetCache(ctx, "helmoci-remote", path); err != nil {
		t.Fatalf("cache-state row missing after the budgeted landing: %v", err)
	} else if entry.Kind != metadata.RemoteCacheKindContent {
		t.Fatalf("cache-state kind = %q, want content", entry.Kind)
	}
}

// TestRemoteV2MissRecordRetriesBusyRow: the negative-cache write survives
// one busy failure and the miss still answers unfound inside its window.
func TestRemoteV2MissRecordRetriesBusyRow(t *testing.T) {
	var wrap *busyMD
	e := newEnvCustom(t, func(md metadata.Store) metadata.Store {
		wrap = &busyMD{Store: md}
		return wrap
	})
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	wrap.cacheFail.arm(1)

	if err := plane.CacheRemoteMiss(ctx, admin(), "helmoci-remote", "gone/manifests/abc"); err != nil {
		t.Fatalf("CacheRemoteMiss under a busy row: %v", err)
	}
	if got := wrap.cacheFail.calls.Load(); got < 2 {
		t.Fatalf("cache-state executions = %d, want the retry to fire", got)
	}
	if _, err := plane.ProbeRemoteCache(ctx, admin(), "helmoci-remote", "gone/manifests/abc"); err != nil {
		t.Fatalf("probe after the budgeted miss record: %v", err)
	}
}

// TestRemoteV2ManifestRecordingRetriesBusyRows: the index-row writes
// (manifest, tag, refs) survive one busy failure each and the recorded
// state serves the read use cases afterwards.
func TestRemoteV2ManifestRecordingRetriesBusyRows(t *testing.T) {
	var wrap *busyMD
	e := newEnvCustom(t, func(md metadata.Store) metadata.Store {
		wrap = &busyMD{Store: md}
		return wrap
	})
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	manifestHex := sha256HexBody("m")
	layerHex := sha256HexBody("l")

	wrap.manifestFail.arm(1)
	wrap.tagFail.arm(1)
	wrap.refsFail.arm(1)

	if err := plane.RecordRemoteManifest(ctx, admin(), "helmoci-remote", "mychart", manifestHex, "1.0.0",
		"application/vnd.oci.image.manifest.v1+json", 42, []*metadata.DockerRef{
			{RepoKey: "helmoci-remote", Image: "mychart", ManifestDigest: manifestHex, BlobDigest: layerHex},
		}); err != nil {
		t.Fatalf("RecordRemoteManifest under busy rows: %v", err)
	}
	for _, site := range []struct {
		name  string
		calls int64
	}{
		{"manifest row", wrap.manifestFail.calls.Load()},
		{"tag row", wrap.tagFail.calls.Load()},
		{"ref rows", wrap.refsFail.calls.Load()},
	} {
		if site.calls < 2 {
			t.Fatalf("%s executed %d times, want the retry to fire", site.name, site.calls)
		}
	}
	tags, err := e.md.Docker().ListTagsByImage(ctx, "helmoci-remote", "mychart")
	if err != nil {
		t.Fatalf("ListTagsByImage after the budgeted recording: %v", err)
	}
	if len(tags) != 1 || tags[0].Tag != "1.0.0" || tags[0].Digest != manifestHex {
		t.Fatalf("recorded tags = %+v, want the 1.0.0 pointer at the manifest digest", tags)
	}
}
