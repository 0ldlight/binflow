package repo_test

// M12 T-342: the registry-v2 package-type family at the service layer —
// helmoci repositories ride the SAME docker use cases (HL-3: one /v2
// stack, two package types), while the class rules keep their own verdicts
// (a helmoci remote/virtual row is not a local plane the use cases serve).
// The rows are seeded through the metadata store directly (the REST create
// gate legs live in internal/adapter/helmoci/gate_test.go — this file pins
// the service-layer widening only).

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedT342Repo writes one repository row through the store.
func seedT342Repo(t *testing.T, e *env, key, rclass, packageType string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// TestT342HelmOCIRidesDockerUseCases: PutManifest, ResolveTag and
// ListImages answer on a seeded LOCAL helmoci repository exactly as on a
// docker one (the family check's whole point).
func TestT342HelmOCIRidesDockerUseCases(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	seedT342Repo(t, e, "t342-helmoci-local", repo.TypeLocal, repo.PackageHelmOCI)

	d := digestOf("t342-helmoci-manifest")
	res := putManifest(t, e, admin(), "t342-helmoci-local", "mychart", d, "0.1.0", digestOf("cfg"), digestOf("layer"))
	if res.Manifest == nil {
		t.Fatal("PutManifest on a helmoci local repo returned no manifest row")
	}

	tag, err := e.svc.ResolveTag(ctx, admin(), "t342-helmoci-local", "mychart", "0.1.0")
	if err != nil {
		t.Fatalf("ResolveTag on helmoci: %v", err)
	}
	if tag.Digest != d {
		t.Errorf("ResolveTag digest = %q, want %q", tag.Digest, d)
	}
	m, err := e.svc.ResolveManifest(ctx, admin(), "t342-helmoci-local", "mychart", d)
	if err != nil {
		t.Fatalf("ResolveManifest on helmoci: %v", err)
	}
	if m.MediaType != "application/vnd.docker.distribution.manifest.v2+json" {
		t.Errorf("ResolveManifest media type = %q, want the stored value verbatim", m.MediaType)
	}
	images, err := e.svc.ListImages(ctx, admin(), "t342-helmoci-local", 0, "")
	if err != nil {
		t.Fatalf("ListImages on helmoci: %v", err)
	}
	if len(images) != 1 || images[0] != "t342-helmoci-local/mychart" {
		t.Errorf("ListImages = %v, want [t342-helmoci-local/mychart]", images)
	}
}

// TestT342HelmOCIClassRulesUnchanged: the family widening does not loosen
// the class rules — a seeded remote or virtual helmoci row still answers
// ErrRepoTypeNotSupported on the docker use cases (the local-plane loader
// refuses non-local classes for both family members alike).
func TestT342HelmOCIClassRulesUnchanged(t *testing.T) {
	e := newEnv(t)
	seedT342Repo(t, e, "t342-helmoci-remote", repo.TypeRemote, repo.PackageHelmOCI)
	seedT342Repo(t, e, "t342-helmoci-virtual", repo.TypeVirtual, repo.PackageHelmOCI)

	d := digestOf("any")
	for _, key := range []string{"t342-helmoci-remote", "t342-helmoci-virtual"} {
		if _, err := e.svc.PutManifest(context.Background(), admin(), key, "mychart", d, "0.1.0",
			"application/vnd.oci.image.manifest.v1+json", 10, nil); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Errorf("PutManifest on %s error = %v, want ErrRepoTypeNotSupported", key, err)
		}
		if _, err := e.svc.ListImages(context.Background(), admin(), key, 0, ""); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
			t.Errorf("ListImages on %s error = %v, want ErrRepoTypeNotSupported", key, err)
		}
	}
}

// TestT342ForeignTypesStillRefused: the family did not open the plane to
// any other package type (a generic local repository keeps refusing).
func TestT342ForeignTypesStillRefused(t *testing.T) {
	e := newEnv(t)
	seedT342Repo(t, e, "t342-generic-local", repo.TypeLocal, repo.PackageGeneric)

	d := digestOf("any")
	if _, err := e.svc.PutManifest(context.Background(), admin(), "t342-generic-local", "app", d, "v1",
		"application/vnd.oci.image.manifest.v1+json", 10, nil); !errors.Is(err, repo.ErrRepoTypeNotSupported) {
		t.Errorf("PutManifest on generic error = %v, want ErrRepoTypeNotSupported", err)
	}
}
