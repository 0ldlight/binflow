package repo_test

// M14 T-392 (FR-129): docker remote pull-through's CONFIGURATION plane —
// the matrix cell that admits a REMOTE docker repository (the /v2 remote
// data chain T-363 built is family-shared, so the admission IS the docker
// half of the feature). Legs:
//
//   - the create on the plain (community, gate-less) service: no license
//     question exists for the core-five docker slot, and the typed config
//     row lands with the canonical defaults;
//   - the per-protocol config seats stay per-protocol (chartsBaseUrl is a
//     helm remote's field, refused by name on docker);
//   - the update face admits the row (expiry/rotate flows keep working);
//   - the read-plane admission (loadV2ReadRepo) and the RemoteV2Plane
//     index seam answer for a remote DOCKER row — the catalog/tags faces
//     read the same tables the landing writes.

import (
	"context"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// t392CreateDockerRemote creates one remote docker repository through the
// service (the FR-129 admission) and fails the test on any refusal.
func t392CreateDockerRemote(t *testing.T, e *env, key, config string) {
	t.Helper()
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageDocker, Config: config,
	}); err != nil {
		t.Fatalf("create remote docker repo %s: %v", key, err)
	}
}

// TestT392DockerRemoteCreateCommunity: the matrix cell opens on the plain
// service — no gate wired, no license installed: the community posture
// (docker is a core-five slot; remote rides the seam, no new slot).
func TestT392DockerRemoteCreateCommunity(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	t392CreateDockerRemote(t, e, "docker-remote",
		`{"url":"https://registry-1.docker.io/v2","allowPrivateUpstream":true}`)

	// The typed row: repositories + remote_configs with the canonical
	// defaults the create-time canonicalization writes.
	row, err := e.svc.GetRepo(ctx, admin(), "docker-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	if row.Type != repo.TypeRemote || row.PackageType != repo.PackageDocker {
		t.Fatalf("row = (%s, %s), want (remote, docker)", row.Type, row.PackageType)
	}
	cfg, err := e.md.Remote().GetConfig(ctx, "docker-remote")
	if err != nil {
		t.Fatalf("remote config row: %v", err)
	}
	if cfg.URL != "https://registry-1.docker.io/v2" {
		t.Fatalf("config url = %q, want the canonical upstream root", cfg.URL)
	}
	if !cfg.AllowPrivateUpstream {
		t.Fatal("allowPrivateUpstream did not reach the typed row")
	}
	if cfg.ContentTTLSeconds != 7200 {
		t.Fatalf("content TTL = %d, want the product default 7200", cfg.ContentTTLSeconds)
	}

	// The list filters see the row on both axes (the console's remote face
	// and the docker filter).
	rows, err := e.svc.ListReposFiltered(ctx, admin(), repo.TypeRemote, repo.PackageDocker)
	if err != nil {
		t.Fatalf("ListReposFiltered: %v", err)
	}
	if len(rows) != 1 || rows[0].RepoKey != "docker-remote" {
		t.Fatalf("filtered rows = %+v, want the one docker remote", rows)
	}
}

// TestT392DockerRemoteConfigSeats: the remote config's per-protocol seats
// stay per-protocol — chartsBaseUrl (T-367's helm seat) is refused BY NAME
// on a docker remote (an inert accepted field is the trap; a silently
// dropped one is a lie).
func TestT392DockerRemoteConfigSeats(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "docker-remote", Type: repo.TypeRemote, PackageType: repo.PackageDocker,
		Config: `{"url":"https://registry-1.docker.io/v2","chartsBaseUrl":"https://charts.example"}`,
	})
	if !strings.Contains(err.Error(), "chartsBaseUrl") || !strings.Contains(err.Error(), "helm remote") {
		t.Fatalf("chartsBaseUrl on docker remote = %v, want the by-name refusal naming the helm seat", err)
	}
}

// TestT392DockerRemoteUpdateAdmitted: the update face keeps admitting the
// row (rotate/expiry flows) — the description edit is the minimal write.
func TestT392DockerRemoteUpdateAdmitted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	t392CreateDockerRemote(t, e, "docker-remote", `{"url":"https://registry-1.docker.io/v2"}`)
	row, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "docker-remote", Description: "hub pull-through",
	})
	if err != nil {
		t.Fatalf("UpdateRepo on remote docker: %v", err)
	}
	if row.Description != "hub pull-through" {
		t.Fatalf("description = %q, want the edit", row.Description)
	}
	if row.Type != repo.TypeRemote || row.PackageType != repo.PackageDocker {
		t.Fatalf("row drifted to (%s, %s)", row.Type, row.PackageType)
	}
}

// TestT392DockerRemoteReadPlaneAdmitted: the registry-v2 READ plane and the
// RemoteV2Plane index seam answer for a remote DOCKER row — the manifest
// row recorded through the seam (exactly what the adapter's landing writes)
// backs the tags/list and image-listing faces the /v2 catalog reads.
func TestT392DockerRemoteReadPlaneAdmitted(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	t392CreateDockerRemote(t, e, "docker-remote", `{"url":"https://registry-1.docker.io/v2"}`)

	plane, ok := e.svc.(repo.RemoteV2Plane)
	if !ok {
		t.Fatal("the concrete service does not implement RemoteV2Plane")
	}
	dgst := strings.Repeat("ab", 32)
	if err := plane.RecordRemoteManifest(ctx, admin(), "docker-remote", "myapp", dgst, "1.0",
		"application/vnd.docker.distribution.manifest.v2+json", 528, nil); err != nil {
		t.Fatalf("RecordRemoteManifest on docker remote: %v", err)
	}

	tags, err := e.svc.ListTags(ctx, admin(), "docker-remote", "myapp", 0, "")
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 1 || tags[0].Tag != "1.0" || tags[0].Digest != dgst {
		t.Fatalf("tags = %+v, want the recorded 1.0@%s", tags, dgst)
	}
	images, err := e.svc.ListImages(ctx, admin(), "docker-remote", 0, "")
	if err != nil {
		t.Fatalf("ListImages: %v", err)
	}
	if len(images) != 1 || images[0] != "docker-remote/myapp" {
		t.Fatalf("images = %v, want the recorded docker-remote/myapp", images)
	}
}
