package repo_test

// T-319 repository-config association matrix (ADR-0038 decision 5 /
// docs/design/gpg-keypair.md section 3.3): local debian/rpm accept the
// keyPairName reference (existence-checked); every other local package
// type — helm included, HL-4 — refuses the field by name; remote and
// virtual refuse it on every package type; and the update path carries
// the same rules as the create path.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// t319SeedKeypair writes one keypair row directly (the store-level seed —
// the manager's write path needs the cipher, out of scope here).
func t319SeedKeypair(t *testing.T, e *env, name string) {
	t.Helper()
	if err := e.md.GpgKeypairs().PutKeypair(context.Background(), &metadata.GpgKeypairRecord{
		PairName: name, PairType: keypair.PairTypeGPG, Alias: "a",
		PublicKey:     "-----BEGIN PGP PUBLIC KEY BLOCK-----\n(seed)\n",
		PrivateKeyEnc: "enc:v1:x", PassphraseEnc: "enc:v1:y",
		Algorithm: "RSA-2048", CreatedAt: "t", UpdatedAt: "t", UpdatedBy: "root",
	}); err != nil {
		t.Fatalf("seed keypair %s: %v", name, err)
	}
}

// sanitizeKeyToken makes a repo-key-safe token out of a package type.
func sanitizeKeyToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestT319LocalKeypairRefMatrix(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"debian": {Known: true, Unlocked: true},
		"rpm":    {Known: true, Unlocked: true},
		"helm":   {Known: true, Unlocked: true},
	})
	t319SeedKeypair(t, e, "kp-live")

	tests := []struct {
		name        string
		packageType string
		config      string
		wantRefusal string // "" = accept
	}{
		{"debian accepts a live reference", "debian", `{"keyPairName":"kp-live"}`, ""},
		{"rpm accepts a live reference", "rpm", `{"keyPairName":"kp-live"}`, ""},
		{"debian dangling reference refuses", "debian", `{"keyPairName":"kp-missing"}`, "does not exist"},
		{"debian explicit empty clears", "debian", `{"keyPairName":""}`, ""},
		{"helm refuses the field (HL-4)", "helm", `{"keyPairName":"kp-live"}`, "not accepted"},
		{"generic refuses the field", "generic", `{"keyPairName":"kp-live"}`, "not accepted"},
		{"no field passes", "debian", `{"quotaBytes":5}`, ""},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "t319-" + string(rune('a'+i)) + "-" + sanitizeKeyToken(tc.packageType),
				Type:    repo.TypeLocal, PackageType: tc.packageType, Config: tc.config,
			})
			if tc.wantRefusal == "" {
				if err != nil {
					t.Fatalf("create refused: %v", err)
				}
				return
			}
			if err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), tc.wantRefusal) {
				t.Fatalf("err = %v, want ErrInvalidRepoConfig naming %q", err, tc.wantRefusal)
			}
		})
	}
}

func TestT319RemoteVirtualRefuseKeypairRef(t *testing.T) {
	e := newEnv(t)
	t319SeedKeypair(t, e, "kp-live")

	remoteBody := `{"url":"https://upstream.example/deb","keyPairName":"kp-live"}`
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t319-remote", Type: repo.TypeRemote, PackageType: "generic", Config: remoteBody,
	}); err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "local-repository behavior") {
		t.Fatalf("remote create err = %v, want the keyPairName refusal", err)
	}

	virtualBody := `{"repositories":["t319-member"],"keyPairName":"kp-live"}`
	if _, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t319-virtual", Type: repo.TypeVirtual, PackageType: "generic", Config: virtualBody,
	}); err == nil || !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "local-repository behavior") {
		t.Fatalf("virtual create err = %v, want the keyPairName refusal", err)
	}
}

func TestT319UpdatePathCarriesTheRules(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"debian": {Known: true, Unlocked: true},
		"helm":   {Known: true, Unlocked: true},
	})
	t319SeedKeypair(t, e, "kp-live")

	created, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t319-upd", Type: repo.TypeLocal, PackageType: "debian", Config: `{}`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// A dangling reference on update refuses.
	upd := *created
	upd.Config = `{"keyPairName":"kp-missing"}`
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &upd); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("update with dangling ref err = %v", err)
	}

	// A live reference lands and echoes through the config blob.
	upd.Config = `{"keyPairName":"kp-live"}`
	after, err := e.svc.UpdateRepo(context.Background(), admin(), &upd)
	if err != nil {
		t.Fatalf("update with live ref: %v", err)
	}
	if name, present, rerr := keypair.RepoConfigReference(after.Config); rerr != nil || !present || name != "kp-live" {
		t.Fatalf("stored config lost the reference: (%q,%v,%v)", name, present, rerr)
	}

	// The helm refusal on update too.
	helm, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t319-helm", Type: repo.TypeLocal, PackageType: "helm", Config: `{}`,
	})
	if err != nil {
		t.Fatalf("helm create: %v", err)
	}
	hupd := *helm
	hupd.Config = `{"keyPairName":"kp-live"}`
	if _, err := e.svc.UpdateRepo(context.Background(), admin(), &hupd); err == nil || !strings.Contains(err.Error(), "not accepted") {
		t.Fatalf("helm update err = %v, want the refusal", err)
	}
}
