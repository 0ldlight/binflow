package repo_test

// M15 T-431 (PRD Q6's ruling): docker × VIRTUAL opens on the creation
// matrix. The aggregated READ plane T-365 built for helmoci is
// family-shared, so a docker virtual walks its members with exactly the
// helmoci precedent's semantics — first-seen resolution over the two-bucket
// member order, tag/image unions rendered under the VIRTUAL key, remote
// members contributing their cached rows. Three-state regression lives
// across the docker package tests: local (M2) and remote (T-392) keep their
// behavior in docker_test.go; this file owns the virtual cell plus the
// member rules that reach the CREATION face now that the matrix admits the
// combination (T-367's same-type rider was previously only reachable
// through helmoci virtuals).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT431DockerVirtualCreatesAndResolves: the opened cell end to end at
// the service face — create through CreateRepo (the real admission path),
// then pull semantics through the members: the local member's tag answers
// first-seen, the remote member's CACHED rows answer without upstream
// contact (T-363 D-2), the tag union dedupes first-seen, and the catalog
// renders the union under the virtual key.
func TestT431DockerVirtualCreatesAndResolves(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	// Members through the real creation path: a docker local member and a
	// docker remote member (the remote cell T-392 opened — the same-family
	// pull-through the walk drives as a member).
	mustCreateDockerRepo(t, e, "t431-dl")
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t431-dr", Type: repo.TypeRemote, PackageType: repo.PackageDocker,
		Config: `{"url":"https://registry.example.com/v2"}`,
	}); err != nil {
		t.Fatalf("CreateRepo(remote docker member): %v", err)
	}

	row, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t431-dv", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
		Config: `{"repositories":["t431-dl","t431-dr"]}`,
	})
	if err != nil {
		t.Fatalf("CreateRepo(virtual docker) error = %v, want nil (T-431 opened the cell)", err)
	}
	if row.Type != repo.TypeVirtual || row.PackageType != repo.PackageDocker {
		t.Fatalf("row = (%s, %s), want (virtual, docker)", row.Type, row.PackageType)
	}
	var cfg struct {
		Repositories []string `json:"repositories"`
	}
	if err := json.Unmarshal([]byte(row.Config), &cfg); err != nil {
		t.Fatalf("canonical config %q: %v", row.Config, err)
	}
	if strings.Join(cfg.Repositories, ",") != "t431-dl,t431-dr" {
		t.Fatalf("canonical members = %v, want the declaration order", cfg.Repositories)
	}
	members, err := e.md.Virtual().ListMembers(ctx, "t431-dv")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 || members[0].MemberRepo != "t431-dl" || members[1].MemberRepo != "t431-dr" {
		t.Fatalf("member ledger = %+v, want [t431-dl t431-dr]", members)
	}

	// Content: the local member holds 1.0.0, the remote member's CACHE holds
	// 2.0.0 (landed through the T-363 seam — the walk's remote arm reads
	// exactly these rows, no upstream contact in this test). seedT365RemoteChart
	// lands under the image name "mychart", so the local member publishes
	// there too — one image across both members is what the union legs below
	// assert.
	localDigest := digestOf("t431-local")
	putManifest(t, e, admin(), "t431-dl", "mychart", localDigest, "1.0.0")
	remoteDigest := seedT365RemoteChart(t, e, "t431-dr", "2.0.0")

	// First-seen: the local member is ahead in declaration order, its tag
	// row answers for 1.0.0.
	tag, err := e.svc.ResolveTag(ctx, admin(), "t431-dv", "mychart", "1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag on the virtual: %v", err)
	}
	if tag.Digest != localDigest {
		t.Fatalf("ResolveTag digest = %q, want the local member's %q", tag.Digest, localDigest)
	}

	// The remote member's cached rows answer their own tag.
	tag, err = e.svc.ResolveTag(ctx, admin(), "t431-dv", "mychart", "2.0.0")
	if err != nil {
		t.Fatalf("ResolveTag (remote member's cached tag): %v", err)
	}
	if tag.Digest != remoteDigest {
		t.Fatalf("ResolveTag digest = %q, want the remote member's cached %q", tag.Digest, remoteDigest)
	}

	// By-digest resolution walks the same order (the adapter's by-digest
	// route rides ResolveManifest).
	mf, err := e.svc.ResolveManifest(ctx, admin(), "t431-dv", "mychart", remoteDigest)
	if err != nil {
		t.Fatalf("ResolveManifest on the virtual: %v", err)
	}
	if mf.Digest != remoteDigest {
		t.Fatalf("ResolveManifest digest = %q, want %q", mf.Digest, remoteDigest)
	}

	// The tag UNION under the virtual key, sorted.
	tags, err := e.svc.ListTags(ctx, admin(), "t431-dv", "mychart", 0, "")
	if err != nil {
		t.Fatalf("ListTags on the virtual: %v", err)
	}
	if len(tags) != 2 || tags[0].Tag != "1.0.0" || tags[1].Tag != "2.0.0" {
		t.Fatalf("tag union = %+v, want [1.0.0 2.0.0]", tags)
	}

	// The catalog renders the image union under the VIRTUAL key (not the
	// members').
	images, err := e.svc.ListImages(ctx, admin(), "t431-dv", 0, "")
	if err != nil {
		t.Fatalf("ListImages on the virtual: %v", err)
	}
	if len(images) != 1 || images[0] != "t431-dv/mychart" {
		t.Fatalf("image union = %v, want [t431-dv/mychart]", images)
	}

	// The write plane keeps its refusal: a manifest publish against the
	// virtual key demands a local repository (the /v2 adapter renders the
	// honest 405 via V2WriteRefusal before this arm ever runs).
	if _, err := e.svc.PutManifest(ctx, admin(), "t431-dv", "mychart", digestOf("t431-push"),
		"3.0.0", "application/vnd.docker.distribution.manifest.v2+json", 1, nil); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Fatalf("PutManifest on the virtual = %v, want ErrRepoTypeNotSupported", err)
	}
}

// TestT431DockerVirtualMemberMix: the same-type member rider (T-367) now
// guards the docker virtual's CREATION face — a generic or helmoci member
// refuses with the mix wording, a same-type member set creates.
func TestT431DockerVirtualMemberMix(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	mustCreateDockerRepo(t, e, "t431-d2")
	mustCreateRepo(t, e, "t431-gen")
	seedT365Repo(t, e, "t431-ho", repo.TypeLocal, "{}") // a helmoci local row (the gated slot)

	tests := []struct {
		name    string
		members string
	}{
		{"generic member", `{"repositories":["t431-gen"]}`},
		{"helmoci member (the family-sharing sibling)", `{"repositories":["t431-ho"]}`},
		{"cross-type member set", `{"repositories":["t431-d2","t431-gen"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
				RepoKey: "t431-dv-mix", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
				Config: tt.members,
			})
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("CreateRepo(docker virtual, %s) error = %v, want ErrInvalidRepoConfig", tt.members, err)
			}
			if !strings.Contains(err.Error(), "cannot mix") || !strings.Contains(err.Error(), "docker virtual aggregates docker repositories only") {
				t.Fatalf("error does not carry the same-type member wording: %v", err)
			}
		})
	}

	// The same-type member set is the one that creates.
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t431-dv-ok", Type: repo.TypeVirtual, PackageType: repo.PackageDocker,
		Config: `{"repositories":["t431-d2"]}`,
	}); err != nil {
		t.Fatalf("CreateRepo(docker virtual, same-type member): %v", err)
	}
}
