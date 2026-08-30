package repo_test

// M13 T-365 (FR-116.2): the registry-v2 virtual aggregation's SERVICE
// half — the four read use cases walk the member order (first-seen
// semantics, the priority bucket ahead of declaration order), the listings
// answer unions, the V2VirtualPlane seam guards its member reads by
// membership, and the write refusal renders the C5 405 un-routed plus the
// honest push-through wording when a route IS configured. The full /v2
// serving walk (upstream conversations included) is the adapter packages'
// chain tests.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedT365Repo writes one registry-v2 family repository row (config
// verbatim — the priority mark rides it).
func seedT365Repo(t *testing.T, e *env, key, rclass, config string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: repo.PackageHelmOCI, Config: config,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// seedT365Virtual writes one virtual helmoci row plus its member ledger.
func seedT365Virtual(t *testing.T, e *env, key, config string, members ...string) {
	t.Helper()
	seedT365Repo(t, e, key, repo.TypeVirtual, config)
	if err := e.md.Virtual().SetMembers(context.Background(), key, members); err != nil {
		t.Fatalf("seed virtual members %s: %v", key, err)
	}
}

// seedT365RemoteChart lands one cached chart into a REMOTE member through
// the T-363 seam (the cached rows are the member's resolution state; the
// manifest digest is the body's MEASURED sha256 — the landing enforces it).
func seedT365RemoteChart(t *testing.T, e *env, member, version string) string {
	t.Helper()
	ctx := context.Background()
	if _, gerr := e.md.Remote().GetConfig(ctx, member); errors.Is(gerr, metadata.ErrRemoteConfigNotFound) {
		if err := e.md.Remote().CreateConfig(ctx, &metadata.RemoteConfig{
			RepoKey: member, URL: "http://127.0.0.1:1/v2/up", ContentTTLSeconds: 7200, MetadataTTLSeconds: 600,
		}); err != nil {
			t.Fatalf("seed remote config %s: %v", member, err)
		}
	}
	body := "t365-remote-chart-" + member + "-" + version
	manifestHex := sha256HexOf(body)
	plane := e.svc.(repo.RemoteV2Plane)
	if _, err := plane.LandRemoteBlob(ctx, admin(), member, "mychart/manifests/"+manifestHex, manifestHex,
		"application/vnd.oci.image.manifest.v1+json", strings.NewReader(body)); err != nil {
		t.Fatalf("land remote chart %s: %v", member, err)
	}
	if err := plane.RecordRemoteManifest(ctx, admin(), member, "mychart", manifestHex, version,
		"application/vnd.oci.image.manifest.v1+json", int64(len(body)), nil); err != nil {
		t.Fatalf("record remote manifest %s: %v", member, err)
	}
	return manifestHex
}

// TestT365VirtualReadUseCases: the four read use cases over a virtual
// repository — first-seen resolution in the two-bucket order (the priority
// mark reorders), the tag UNION with first-seen dedupe, the image UNION
// rendered under the virtual key, and the unknown-image refusal.
func TestT365VirtualReadUseCases(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedT365Repo(t, e, "t365-hl", repo.TypeLocal, "{}")
	seedT365Repo(t, e, "t365-hr", repo.TypeRemote, "{}")
	seedT365Virtual(t, e, "t365-virt", `{"repositories":["t365-hl","t365-hr"]}`, "t365-hl", "t365-hr")

	localDigest := digestOf("t365-local")
	putManifest(t, e, admin(), "t365-hl", "mychart", localDigest, "0.1.0", digestOf("cfg1"))
	putManifest(t, e, admin(), "t365-hl", "mychart", digestOf("t365-local-2"), "0.2.0")
	remoteDigest := seedT365RemoteChart(t, e, "t365-hr", "0.1.0")
	seedT365RemoteChart(t, e, "t365-hr", "0.9.0")

	// First-seen: the declaration order serves the local member's digest.
	tag, err := e.svc.ResolveTag(ctx, admin(), "t365-virt", "mychart", "0.1.0")
	if err != nil {
		t.Fatalf("ResolveTag on the virtual: %v", err)
	}
	if tag.Digest != localDigest {
		t.Fatalf("ResolveTag digest = %q, want the local member's %q (first-seen)", tag.Digest, localDigest)
	}

	// The priority bucket jumps the remote member ahead: its digest wins.
	if err := e.md.Repos().Update(ctx, &metadata.Repo{
		RepoKey: "t365-hr", Type: repo.TypeRemote, PackageType: repo.PackageHelmOCI,
		Config: `{"priorityResolution":true}`,
	}); err != nil {
		t.Fatalf("mark the remote member priority: %v", err)
	}
	tag, err = e.svc.ResolveTag(ctx, admin(), "t365-virt", "mychart", "0.1.0")
	if err != nil {
		t.Fatalf("ResolveTag after the priority mark: %v", err)
	}
	if tag.Digest != remoteDigest {
		t.Fatalf("ResolveTag digest = %q, want the priority member's %q", tag.Digest, remoteDigest)
	}

	// By-digest routing: each digest resolves through whichever member
	// holds it, with no fallback to the other's rows.
	for _, tc := range []struct{ digest, owner string }{
		{localDigest, "the local member"},
		{remoteDigest, "the remote member"},
	} {
		m, merr := e.svc.ResolveManifest(ctx, admin(), "t365-virt", "mychart", tc.digest)
		if merr != nil || m.Digest != tc.digest {
			t.Fatalf("ResolveManifest(%s) = (%v, %+v), want the row of %s", tc.digest, merr, m, tc.owner)
		}
	}
	if _, err := e.svc.ResolveManifest(ctx, admin(), "t365-virt", "mychart", digestOf("nowhere")); !errors.Is(err, repo.ErrManifestNotFound) {
		t.Errorf("ResolveManifest of an unknown digest error = %v, want ErrManifestNotFound", err)
	}

	// The tag UNION: both members' versions, deduped, globally sorted.
	tags, err := e.svc.ListTags(ctx, admin(), "t365-virt", "mychart", 0, "")
	if err != nil {
		t.Fatalf("ListTags on the virtual: %v", err)
	}
	var names []string
	for _, t := range tags {
		names = append(names, t.Tag)
	}
	if strings.Join(names, ",") != "0.1.0,0.2.0,0.9.0" {
		t.Fatalf("ListTags union = %v, want [0.1.0 0.2.0 0.9.0]", names)
	}
	// The official cursor slices the union.
	tags, err = e.svc.ListTags(ctx, admin(), "t365-virt", "mychart", 2, "0.1.0")
	if err != nil || len(tags) != 2 || tags[0].Tag != "0.2.0" || tags[1].Tag != "0.9.0" {
		t.Fatalf("ListTags page = (%v, %d rows), want [0.2.0 0.9.0]", err, len(tags))
	}

	// The image UNION, rendered under the VIRTUAL key.
	putManifest(t, e, admin(), "t365-hl", "otherchart", digestOf("t365-other"), "1.0.0")
	images, err := e.svc.ListImages(ctx, admin(), "t365-virt", 0, "")
	if err != nil {
		t.Fatalf("ListImages on the virtual: %v", err)
	}
	if strings.Join(images, ",") != "t365-virt/mychart,t365-virt/otherchart" {
		t.Fatalf("ListImages union = %v, want both charts under the virtual key", images)
	}

	// An image no member knows keeps the NAME_UNKNOWN semantics.
	if _, err := e.svc.ListTags(ctx, admin(), "t365-virt", "nochart", 0, ""); !errors.Is(err, repo.ErrImageNotFound) {
		t.Errorf("ListTags of an unknown image error = %v, want ErrImageNotFound", err)
	}

	// The write plane keeps its refusal: the read walks did not open one.
	if _, err := e.svc.PutManifest(ctx, admin(), "t365-virt", "mychart", digestOf("x"), "9.9.9",
		"application/vnd.oci.image.manifest.v1+json", 10, nil); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("PutManifest on the virtual error = %v, want ErrRepoTypeNotSupported", err)
	}
}

// TestT365VirtualSeamGuardAndFacts: the V2VirtualPlane seam — the family
// and class gates of the member order, the membership guard, and the
// member fact shapes (a local member's node, a remote member's cache
// states).
func TestT365VirtualSeamGuardAndFacts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedT365Repo(t, e, "t365-hl", repo.TypeLocal, "{}")
	seedT365Repo(t, e, "t365-hr", repo.TypeRemote, "{}")
	seedT365Virtual(t, e, "t365-virt", `{"repositories":["t365-hl","t365-hr"]}`, "t365-hl", "t365-hr")
	seedT365Repo(t, e, "t365-plain", repo.TypeLocal, "{}") // a generic local, no membership

	plane, ok := e.svc.(repo.V2VirtualPlane)
	if !ok {
		t.Fatal("the service carries no V2VirtualPlane seam")
	}

	order, err := plane.V2MemberOrder(ctx, "t365-virt")
	if err != nil || len(order) != 2 || order[0].Key != "t365-hl" || order[0].Type != repo.TypeLocal {
		t.Fatalf("V2MemberOrder = (%v, %+v), want the two-bucket member order", err, order)
	}

	// The gates: a local key and a foreign-family virtual both refuse.
	if _, err := plane.V2MemberOrder(ctx, "t365-hl"); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("V2MemberOrder on a local repo error = %v, want ErrRepoTypeNotSupported", err)
	}
	seedT365Virtual(t, e, "t365-generic-virt", `{"repositories":["t365-plain"]}`, "t365-plain")
	if err := e.md.Repos().Update(context.Background(), &metadata.Repo{
		RepoKey: "t365-generic-virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric, Config: `{}`,
	}); err != nil {
		t.Fatalf("flip the second virtual to generic: %v", err)
	}
	if _, err := plane.V2MemberOrder(ctx, "t365-generic-virt"); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("V2MemberOrder on a generic virtual error = %v, want ErrRepoTypeNotSupported", err)
	}

	// The membership guard: a repository outside the order never reads.
	if _, err := plane.V2MemberManifest(ctx, admin(), "t365-virt", "t365-plain", "mychart", "0.1.0"); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Errorf("V2MemberManifest on a non-member error = %v, want ErrRepoNotFound", err)
	}

	// A local member's facts: the digest, the row's media type, the node.
	localDigest := digestOf("t365-local")
	putManifest(t, e, admin(), "t365-hl", "mychart", localDigest, "0.1.0")
	facts, err := plane.V2MemberManifest(ctx, admin(), "t365-virt", "t365-hl", "mychart", "0.1.0")
	if err != nil || facts.Digest != localDigest || facts.Node == nil || facts.Cache != "" {
		t.Fatalf("local member facts = (%v, %+v), want the digest, node and no cache state", err, facts)
	}
	// A miss is not an error: the walk continues.
	facts, err = plane.V2MemberManifest(ctx, admin(), "t365-virt", "t365-hl", "mychart", "9.9.9")
	if err != nil || facts.Digest != "" || facts.Node != nil {
		t.Fatalf("local member miss = (%v, %+v), want the Digest-less miss shape", err, facts)
	}

	// A remote member with nothing cached: the upstream-decides posture.
	facts, err = plane.V2MemberManifest(ctx, admin(), "t365-virt", "t365-hr", "mychart", "0.1.0")
	if err != nil || facts.Digest != "" || facts.Cache != repo.RemoteProbeMiss {
		t.Fatalf("remote member uncached = (%v, %+v), want the RemoteProbeMiss posture", err, facts)
	}
	// After a cached land: the HIT shape.
	remoteDigest := seedT365RemoteChart(t, e, "t365-hr", "0.1.0")
	facts, err = plane.V2MemberManifest(ctx, admin(), "t365-virt", "t365-hr", "mychart", "0.1.0")
	if err != nil || facts.Digest != remoteDigest || facts.Node == nil || facts.Cache != repo.RemoteProbeHit {
		t.Fatalf("remote member cached = (%v, %+v), want the HIT shape", err, facts)
	}

	// The blob twin: a remote member's cached layer probes HIT; a local
	// member without the blob answers the node-less miss.
	layer := "the t365 chart layer"
	layerHex := sha256HexOf(layer)
	rplane := e.svc.(repo.RemoteV2Plane)
	if _, err := rplane.LandRemoteBlob(ctx, admin(), "t365-hr", "mychart/blobs/"+layerHex, layerHex,
		"application/vnd.cncf.helm.chart.content.v1.tar+gzip", strings.NewReader(layer)); err != nil {
		t.Fatalf("land remote layer: %v", err)
	}
	blob, err := plane.V2MemberBlob(ctx, admin(), "t365-virt", "t365-hr", "mychart", layerHex)
	if err != nil || blob.Node == nil || blob.Cache != repo.RemoteProbeHit {
		t.Fatalf("remote member blob = (%v, %+v), want the cached node", err, blob)
	}
	blob, err = plane.V2MemberBlob(ctx, admin(), "t365-virt", "t365-hl", "mychart", layerHex)
	if err != nil || blob.Node != nil {
		t.Fatalf("local member blob miss = (%v, %+v), want the node-less miss", err, blob)
	}
}

// TestT365V2WriteRefusal: the /v2 write refusal's two wordings — the C5
// spelling verbatim when nothing is routed, the honest push-through
// wording (naming the configured target) when one is.
func TestT365V2WriteRefusal(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedT365Repo(t, e, "t365-hl", repo.TypeLocal, "{}")
	seedT365Virtual(t, e, "t365-virt", `{"repositories":["t365-hl"]}`, "t365-hl")
	plane := e.svc.(repo.V2VirtualPlane)

	se := plane.V2WriteRefusal(ctx, "t365-virt")
	if se == nil || se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("un-routed refusal = (%v, code %d), want the 405", se, codeOf(se))
	}
	if !strings.Contains(se.Message, "No local repository was configured as local deployment repository for the (t365-virt) virtual repository.") {
		t.Fatalf("un-routed message %q is not the C5 spelling", se.Message)
	}
	if allow := se.Header.Get("Allow"); allow != "GET" {
		t.Fatalf("un-routed Allow = %q, want GET", allow)
	}

	seedT365Virtual(t, e, "t365-virt-routed",
		`{"repositories":["t365-hl"],"defaultDeploymentRepo":"t365-hl"}`, "t365-hl")
	se = plane.V2WriteRefusal(ctx, "t365-virt-routed")
	if se == nil || se.Code != http.StatusMethodNotAllowed {
		t.Fatalf("routed refusal = (%v, code %d), want the 405", se, codeOf(se))
	}
	if !strings.Contains(se.Message, "t365-hl") || !strings.Contains(se.Message, "does not accept pushes on the registry v2 plane") {
		t.Fatalf("routed message %q does not name the target honestly", se.Message)
	}
}

// codeOf keeps the refusal assertions readable without importing net/http
// twice in this file's shape.
func codeOf(se *repo.StatusError) int {
	if se == nil {
		return 0
	}
	return se.Code
}
