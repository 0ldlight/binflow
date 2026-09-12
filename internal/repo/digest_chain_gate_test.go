package repo_test

// L002-1 (C10, ADR-0047): the DigestChainGate facet — the marker-gate
// oracle over the docker_refs ledger. The adapter tests (docker/
// remote_chain_gate_test.go) pin the serving semantics end to end; this
// file pins the FACET contract: the ledger answer, the remote-class gate,
// and the virtual member twin's membership guard.

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestDigestChainGateLedger: the oracle reads the refs rows
// RecordRemoteManifest keeps — in-chain true, everything else false — and
// refuses non-remote repository classes.
func TestDigestChainGateLedger(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "docker-remote", repo.PackageDocker, "http://127.0.0.1:1/v2/up")

	gate, ok := e.svc.(repo.DigestChainGate)
	if !ok {
		t.Fatal("the service does not implement repo.DigestChainGate")
	}
	plane := e.svc.(repo.RemoteV2Plane)
	manifest := `{"schemaVersion":2,"mediaType":"application/vnd.docker.distribution.manifest.v2+json",` +
		`"config":{"mediaType":"application/vnd.docker.container.image.v1+json","digest":"sha256:` +
		sha256HexOf("gate-cfg") + `","size":8},"layers":[]}`
	manifestHex := sha256HexOf(manifest)
	if err := plane.RecordRemoteManifest(ctx, admin(), "docker-remote", "myimg", manifestHex, "1.0",
		"application/vnd.docker.distribution.manifest.v2+json", int64(len(manifest)), nil); err != nil {
		t.Fatalf("RecordRemoteManifest without refs: %v", err)
	}

	// No refs named yet: the gate is shut for every digest.
	in, err := gate.BlobInChain(ctx, admin(), "docker-remote", "myimg", sha256HexOf("gate-cfg"))
	if err != nil || in {
		t.Fatalf("empty ledger = (%v, %v), want (false, nil)", in, err)
	}
	// The ref edge lands with the manifest record — the gate opens.
	if err := e.md.Docker().PutRefs(ctx, "docker-remote", "myimg", manifestHex, []*metadata.DockerRef{
		{RepoKey: "docker-remote", Image: "myimg", ManifestDigest: manifestHex, BlobDigest: sha256HexOf("gate-cfg")},
	}); err != nil {
		t.Fatalf("PutRefs: %v", err)
	}
	in, err = gate.BlobInChain(ctx, admin(), "docker-remote", "myimg", sha256HexOf("gate-cfg"))
	if err != nil || !in {
		t.Fatalf("ledger answer = (%v, %v), want (true, nil)", in, err)
	}

	// Malformed inputs refuse (the plane's own validation family).
	if _, err := gate.BlobInChain(ctx, admin(), "docker-remote", "myimg", "not-a-digest"); !errors.Is(err, repo.ErrInvalidDigest) {
		t.Errorf("bad digest error = %v, want ErrInvalidDigest", err)
	}

	// Only REMOTE v2-family repositories carry chains.
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "plain-local", Type: repo.TypeLocal, PackageType: repo.PackageDocker, Config: "{}",
	}); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	if _, err := gate.BlobInChain(ctx, admin(), "plain-local", "myimg", sha256HexOf("gate-cfg")); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("local repo error = %v, want ErrRepoTypeNotSupported", err)
	}
}

// TestDigestChainGateVirtualMember: the membership-guarded twin — the
// member's ledger answers, a non-member key refuses, and a non-remote
// member never carries a pull-through chain.
func TestDigestChainGateVirtualMember(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedV2Remote(t, e, "member-remote", repo.PackageHelmOCI, "http://127.0.0.1:1/v2/up")
	seedT365Virtual(t, e, "gate-virt", "{}", "member-remote")

	gate := e.svc.(repo.DigestChainGate)
	if err := e.md.Docker().PutRefs(ctx, "member-remote", "mychart", "aabb", []*metadata.DockerRef{
		{RepoKey: "member-remote", Image: "mychart", ManifestDigest: "aabb", BlobDigest: sha256HexOf("gate-chart")},
	}); err != nil {
		t.Fatalf("PutRefs: %v", err)
	}
	in, err := gate.V2MemberBlobInChain(ctx, admin(), "gate-virt", "member-remote", "mychart", sha256HexOf("gate-chart"))
	if err != nil || !in {
		t.Fatalf("member chain = (%v, %v), want (true, nil)", in, err)
	}
	in, err = gate.V2MemberBlobInChain(ctx, admin(), "gate-virt", "member-remote", "mychart", sha256HexOf("unknown"))
	if err != nil || in {
		t.Fatalf("unknown digest = (%v, %v), want (false, nil)", in, err)
	}
	if _, err := gate.V2MemberBlobInChain(ctx, admin(), "gate-virt", "not-a-member", "mychart", sha256HexOf("gate-chart")); !errors.Is(err, repo.ErrRepoNotFound) {
		t.Errorf("non-member error = %v, want ErrRepoNotFound", err)
	}
}
