package repo_test

// M11 T-309: the virtual-repository Helm/HelmOCI family-mix refusal
// (helm.md section 8.2's closing note — the two protocol families cannot
// share one virtual repository). The check lives at member validation;
// the virtual implementation itself is T-313's. helmoci rows cannot be
// created through the service yet (its slot lands with its own ticket),
// so the members are seeded through the metadata store directly — the
// shape a migrated or forward-created row would carry.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedT309Repo writes one repository row through the store, bypassing the
// service validation (helmoci has no legal create path yet).
func seedT309Repo(t *testing.T, e *env, key, rclass, packageType string) {
	t.Helper()
	if err := e.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: rclass, PackageType: packageType, Config: "{}",
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

func TestT309HelmHelmOCINoMix(t *testing.T) {
	unlockedHelm := repo.PackageTypeVerdict{Known: true, Unlocked: true}
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"helm":    unlockedHelm,
		"helmoci": unlockedHelm, // the forward slot shape T-313's adapter rides
	})

	seedT309Repo(t, e, "t309-helm-local", repo.TypeLocal, repo.PackageHelm)
	seedT309Repo(t, e, "t309-helmoci-local", repo.TypeLocal, repo.PackageHelmOCI)

	tests := []struct {
		name      string
		own       string
		members   string
		wantRefus bool
	}{
		{
			name:      "helm members only",
			own:       repo.PackageHelm,
			members:   `{"repositories":["t309-helm-local"]}`,
			wantRefus: false,
		},
		{
			name:      "helm virtual with a helmoci member",
			own:       repo.PackageHelm,
			members:   `{"repositories":["t309-helm-local","t309-helmoci-local"]}`,
			wantRefus: true,
		},
		{
			name:      "helmoci virtual with a helm member",
			own:       repo.PackageHelmOCI,
			members:   `{"repositories":["t309-helmoci-local","t309-helm-local"]}`,
			wantRefus: true,
		},
		{
			name:      "generic virtual carrying both families",
			own:       repo.PackageGeneric,
			members:   `{"repositories":["t309-helm-local","t309-helmoci-local"]}`,
			wantRefus: true,
		},
		{
			name:      "generic virtual with the helm member alone",
			own:       repo.PackageGeneric,
			members:   `{"repositories":["t309-helm-local"]}`,
			wantRefus: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "t309-virt-" + strings.ReplaceAll(strings.ToLower(strings.ReplaceAll(tt.name, " ", "-")), "/", "-")
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: key, Type: repo.TypeVirtual, PackageType: tt.own, Config: tt.members,
			})
			if tt.wantRefus {
				if err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "cannot mix the Helm and HelmOCI") {
					t.Fatalf("create err = %v, want the family-mix ErrInvalidRepoConfig", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("create err = %v, want ok", err)
			}
		})
	}
}

// TestT309HelmFamilyUpdateArm: the refusal also guards the member-list
// UPDATE (a helm virtual cannot gain a helmoci member later).
func TestT309HelmFamilyUpdateArm(t *testing.T) {
	unlockedHelm := repo.PackageTypeVerdict{Known: true, Unlocked: true}
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"helm": unlockedHelm})
	seedT309Repo(t, e, "t309-h2-local", repo.TypeLocal, repo.PackageHelm)
	seedT309Repo(t, e, "t309-oci2-local", repo.TypeLocal, repo.PackageHelmOCI)

	created, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t309-virt2", Type: repo.TypeVirtual, PackageType: repo.PackageHelm,
		Config: `{"repositories":["t309-h2-local"]}`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	created.Config = `{"repositories":["t309-h2-local","t309-oci2-local"]}`
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), created); !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "cannot mix the Helm and HelmOCI") {
		t.Fatalf("update err = %v, want the family-mix refusal on the update arm", err)
	}
}
