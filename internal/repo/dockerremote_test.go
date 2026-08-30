package repo_test

// M13 T-363: the registry-v2 remote pull-through's SERVICE half — the
// RemoteV2Plane seams the docker adapter's upstream session consumes.
// Landing runs the engine's own invariants (blob-first, checksum
// addressing, TTL cache rows), probing is read-only, the miss record
// answers unfound inside its window, and the manifest-row recording makes
// the read use cases (ResolveTag/ResolveManifest/ListTags/ListImages)
// serve a remote repository's cached state.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// sha256HexOf renders one body's real digest (the landed-content helper —
// digestOf builds synthetic shapes for row keys only, the landing seam
// ENFORCES the measured sha256).
func sha256HexOf(b string) string {
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// seedV2Remote seeds one remote registry-v2 family repository plus its
// remote_configs row (the create plane's typed landing, done directly —
// the REST create legs live in the adapter packages).
func seedV2Remote(t *testing.T, e *env, key, packageType, url string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
	if err := e.md.Remote().CreateConfig(context.Background(), &metadata.RemoteConfig{
		RepoKey: key, URL: url, ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
	}); err != nil {
		t.Fatalf("seed remote config %s: %v", key, err)
	}
}

// TestRemoteV2LandAndProbe: the landing/probing pair — land one body
// checksum-addressed, probe HIT; reland the same bytes idempotently; a
// DIFFERENT body under the same expected digest refuses with the storage
// checksum sentinel and lands nothing.
func TestRemoteV2LandAndProbe(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")

	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the service does not implement repo.RemoteV2Plane")
	}
	body := []byte("the chart manifest body")
	hex := sha256HexOf(string(body))

	node, err := plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", "mychart/manifests/"+hex, hex,
		"application/vnd.oci.image.manifest.v1+json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("LandRemoteBlob: %v", err)
	}
	if node.Sha256 != hex || node.Size != int64(len(body)) {
		t.Fatalf("landed node = (%s, %d), want (%s, %d)", node.Sha256, node.Size, hex, len(body))
	}
	if node.CreatedBy != "remote-proxy" {
		t.Errorf("node.CreatedBy = %q, want remote-proxy", node.CreatedBy)
	}

	probe, err := plane.ProbeRemoteCache(ctx, admin(), "helmoci-remote", "mychart/manifests/"+hex)
	if err != nil {
		t.Fatalf("ProbeRemoteCache: %v", err)
	}
	if probe.State != repo.RemoteProbeHit || probe.Node == nil || probe.Node.Sha256 != hex {
		t.Fatalf("probe after landing = (%s, %v), want HIT with the node", probe.State, probe.Node)
	}

	// Idempotent reland: same bytes, same path — first-seen provenance
	// survives, the probe stays HIT.
	again, err := plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", "mychart/manifests/"+hex, hex,
		"application/vnd.oci.image.manifest.v1+json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("reland: %v", err)
	}
	if again.CreatedAt != node.CreatedAt {
		t.Errorf("reland moved CreatedAt (%q -> %q) — first-seen provenance must survive", node.CreatedAt, again.CreatedAt)
	}

	// The enforced digest: a body that is not the digest it claims never
	// lands (the checksum-addressed layout's load-bearing rule).
	_, err = plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", "mychart/blobs/"+hex, hex,
		"application/octet-stream", strings.NewReader("different bytes entirely"))
	if !errors.Is(err, storage.ErrChecksumMismatch) {
		t.Fatalf("mismatched land error = %v, want storage.ErrChecksumMismatch", err)
	}
	if _, perr := e.md.Nodes().Get(ctx, "helmoci-remote", "mychart/blobs/"+hex); !errors.Is(perr, metadata.ErrNodeNotFound) {
		t.Errorf("mismatched land left a node behind: %v", perr)
	}
}

// TestRemoteV2NegativeCache: the miss record answers NEGATIVE inside its
// window without any upstream contact — and the window moves with the
// injectable clock (the default missedTTL is 1800s).
func TestRemoteV2NegativeCache(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane := e.svc.(repo.RemoteV2Plane)

	path := "mychart/blobs/" + digestOf("neg")
	if err := plane.CacheRemoteMiss(ctx, admin(), "helmoci-remote", path); err != nil {
		t.Fatalf("CacheRemoteMiss: %v", err)
	}
	probe, err := plane.ProbeRemoteCache(ctx, admin(), "helmoci-remote", path)
	if err != nil {
		t.Fatalf("ProbeRemoteCache: %v", err)
	}
	if probe.State != repo.RemoteProbeNegative || probe.Node != nil {
		t.Fatalf("probe inside the miss window = (%s, %v), want NEGATIVE without a node", probe.State, probe.Node)
	}
	e.clk.Advance(1801 * time.Second)
	probe, err = plane.ProbeRemoteCache(ctx, admin(), "helmoci-remote", path)
	if err != nil {
		t.Fatalf("ProbeRemoteCache after the window: %v", err)
	}
	if probe.State != repo.RemoteProbeMiss {
		t.Fatalf("probe after the miss window = %s, want MISS", probe.State)
	}
}

// TestRemoteV2StaleAfterTTL: a landed copy's freshness window follows the
// repository's content TTL (7200s default) — the STALE state is the
// degradation input the adapter serves on upstream faults.
func TestRemoteV2StaleAfterTTL(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane := e.svc.(repo.RemoteV2Plane)

	hex := sha256HexOf("body")
	path := "mychart/manifests/" + hex
	if _, err := plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", path, hex, "application/vnd.oci.image.manifest.v1+json",
		strings.NewReader("body")); err != nil {
		t.Fatalf("LandRemoteBlob: %v", err)
	}
	e.clk.Advance(7201 * time.Second)
	probe, err := plane.ProbeRemoteCache(ctx, admin(), "helmoci-remote", path)
	if err != nil {
		t.Fatalf("ProbeRemoteCache: %v", err)
	}
	if probe.State != repo.RemoteProbeStale || probe.Node == nil {
		t.Fatalf("probe after the TTL = (%s, %v), want STALE with the standing copy", probe.State, probe.Node)
	}
}

// TestRecordRemoteManifestRows: the manifest-row recording turns a landed
// remote copy into resolution state — ResolveTag/ResolveManifest answer,
// ListTags lists, ListImages catalogs (the read-plane admission T-363
// widened), and the write plane still refuses the repository.
func TestRecordRemoteManifestRows(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	plane := e.svc.(repo.RemoteV2Plane)

	manifest := []byte(`{"schemaVersion":2,"config":{"mediaType":"application/vnd.cncf.helm.config.v1+json","digest":"sha256:` +
		digestOf("cfg") + `"},"layers":[{"mediaType":"application/vnd.cncf.helm.chart.content.v1.tar+gzip","digest":"sha256:` +
		digestOf("layer") + `"}]}`)
	manifestHex := sha256HexOf(string(manifest))
	refs := []*metadata.DockerRef{
		{RepoKey: "helmoci-remote", Image: "mychart", ManifestDigest: manifestHex,
			BlobDigest: digestOf("cfg"), ChildMediaType: "application/vnd.cncf.helm.config.v1+json"},
		{RepoKey: "helmoci-remote", Image: "mychart", ManifestDigest: manifestHex,
			BlobDigest: digestOf("layer"), ChildMediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip"},
	}
	if _, err := plane.LandRemoteBlob(ctx, admin(), "helmoci-remote", "mychart/manifests/"+manifestHex, manifestHex,
		"application/vnd.oci.image.manifest.v1+json", bytes.NewReader(manifest)); err != nil {
		t.Fatalf("LandRemoteBlob: %v", err)
	}
	if err := plane.RecordRemoteManifest(ctx, admin(), "helmoci-remote", "mychart", manifestHex, "0.1.0",
		"application/vnd.oci.image.manifest.v1+json", int64(len(manifest)), refs); err != nil {
		t.Fatalf("RecordRemoteManifest: %v", err)
	}

	tag, err := e.svc.ResolveTag(ctx, admin(), "helmoci-remote", "mychart", "0.1.0")
	if err != nil || tag.Digest != manifestHex {
		t.Fatalf("ResolveTag on the remote row = (%v, %q), want the recorded digest", err, tag.Digest)
	}
	m, err := e.svc.ResolveManifest(ctx, admin(), "helmoci-remote", "mychart", manifestHex)
	if err != nil || m.MediaType != "application/vnd.oci.image.manifest.v1+json" {
		t.Fatalf("ResolveManifest = (%v, %+v)", err, m)
	}
	tags, err := e.svc.ListTags(ctx, admin(), "helmoci-remote", "mychart", 0, "")
	if err != nil || len(tags) != 1 || tags[0].Tag != "0.1.0" {
		t.Fatalf("ListTags = (%v, %d rows)", err, len(tags))
	}
	images, err := e.svc.ListImages(ctx, admin(), "helmoci-remote", 0, "")
	if err != nil || len(images) != 1 || images[0] != "helmoci-remote/mychart" {
		t.Fatalf("ListImages = (%v, %v)", err, images)
	}

	// The write plane keeps its refusal (RE-05): the read admission did
	// not open a deploy path.
	if _, err := e.svc.PutManifest(ctx, admin(), "helmoci-remote", "mychart", manifestHex, "0.2.0",
		"application/vnd.oci.image.manifest.v1+json", 10, nil); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("PutManifest on the remote row error = %v, want ErrRepoTypeNotSupported", err)
	}

	// The ref edges landed (the GC reference facts of the cached manifest).
	edges, err := e.md.Docker().ListRefsByManifest(ctx, "helmoci-remote", "mychart", manifestHex)
	if err != nil || len(edges) != 2 {
		t.Fatalf("ListRefsByManifest = (%v, %d rows), want the two recorded edges", err, len(edges))
	}
}

// TestRemoteUpstreamFacts: the seam resolves the upstream bundle — URL and
// DECRYPTED password under a master key, the egress flags, and the product
// defaults for the TTL fields. The class/family/read gates refuse
// everything else.
func TestRemoteUpstreamFacts(t *testing.T) {
	key := bytes.Repeat([]byte{0x22}, 32)
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY", base64.StdEncoding.EncodeToString(key))
	e := newEnv(t)
	ctx := context.Background()

	// The row is seeded (the REST create gate is the adapter packages'
	// leg); the password lands through the crypto chain's own seal, the
	// same form CreateRepo writes.
	cipher, err := remote.NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	sealed, err := cipher.Encrypt("s3cret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	seedV2Remote(t, e, "helmoci-remote", repo.PackageHelmOCI, "http://127.0.0.1:9/v2/up")
	cfg, err := e.md.Remote().GetConfig(ctx, "helmoci-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	cfg.Username, cfg.Password = "ci", sealed
	if err := e.md.Remote().UpdateConfig(ctx, cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	plane := e.svc.(repo.RemoteV2Plane)
	facts, err := plane.RemoteUpstream(ctx, admin(), "helmoci-remote")
	if err != nil {
		t.Fatalf("RemoteUpstream: %v", err)
	}
	if facts.URL != "http://127.0.0.1:9/v2/up" || facts.Username != "ci" || facts.Password != "s3cret" {
		t.Fatalf("facts = (%q, %q, %q), want the configured triple", facts.URL, facts.Username, facts.Password)
	}
	if facts.ContentTTLSeconds != 7200 || facts.MissedTTLSeconds != 1800 || facts.SocketTimeoutMs != 15000 {
		t.Fatalf("defaults = (content %d, missed %d, socket %d), want (7200, 1800, 15000)",
			facts.ContentTTLSeconds, facts.MissedTTLSeconds, facts.SocketTimeoutMs)
	}

	// The gates: a LOCAL repository and a non-family remote both refuse;
	// a principal without the read grant refuses too.
	seedT342Repo(t, e, "helmoci-local", repo.TypeLocal, repo.PackageHelmOCI)
	seedV2Remote(t, e, "generic-remote", repo.PackageGeneric, "http://127.0.0.1:9")
	for _, key := range []string{"helmoci-local", "generic-remote"} {
		if _, err := plane.RemoteUpstream(ctx, admin(), key); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Errorf("RemoteUpstream on %s error = %v, want ErrRepoTypeNotSupported", key, err)
		}
	}
	plain := &repo.Principal{Name: "nobody"}
	if _, err := plane.RemoteUpstream(ctx, plain, "helmoci-remote"); !errors.Is(err, repo.ErrForbidden) {
		t.Errorf("RemoteUpstream without the read grant error = %v, want ErrForbidden", err)
	}
	if _, err := plane.ProbeRemoteCache(ctx, plain, "helmoci-remote", "x/y"); !errors.Is(err, repo.ErrForbidden) {
		t.Errorf("ProbeRemoteCache without the read grant error = %v, want ErrForbidden", err)
	}
}
