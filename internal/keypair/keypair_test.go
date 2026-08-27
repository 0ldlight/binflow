package keypair_test

// T-319 domain acceptance (ADR-0038 / docs/design/gpg-keypair.md): the
// manager's CRUD/generate/verify faces over the real enc:v1 chain and the
// real SQLite store, the at-rest sealing invariants (both secret columns
// carry enc:v1, plaintext never lands), the structurally-absent key
// material in every echo, the in-use delete guard, and the boot posture.
//
// Key material is generated at RSA-2048 inside these tests (the fastest
// legal size — the 4096 default is pinned by a unit check, not by paying
// its keygen cost per case); the gpg-client interop leg lives in
// gpgclient_test.go.

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// keypairEnv is a store + cipher + manager assembly; repos is an optional
// in-memory repo source for the guard/reference faces.
type keypairEnv struct {
	t      *testing.T
	md     metadata.Store
	mgr    *keypair.Manager
	repos  *fakeRepoSource
	cipher *remote.Cipher
}

func newKeypairEnv(t *testing.T) *keypairEnv {
	t.Helper()
	return newKeypairEnvOpt(t, true)
}

func newKeypairEnvOpt(t *testing.T, withCipher bool) *keypairEnv {
	t.Helper()
	ctx := context.Background()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })
	e := &keypairEnv{t: t, md: md, repos: newFakeRepoSource()}
	var c *remote.Cipher
	if withCipher {
		c, err = remote.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("remote.NewCipher: %v", err)
		}
	}
	e.cipher = c
	e.mgr, err = keypair.NewManager(keypair.Options{
		Store:  md.GpgKeypairs(),
		Cipher: seam(c),
		Repos:  e.repos,
	})
	if err != nil {
		t.Fatalf("keypair.NewManager: %v", err)
	}
	return e
}

// seam mirrors cmd's cipherSeam (nil *remote.Cipher → nil interface).
func seam(c *remote.Cipher) keypair.SecretCipher {
	if c == nil {
		return nil
	}
	return c
}

// generateFixture generates and stores one pair (RSA-2048 for speed) and
// returns the material's armored forms.
func (e *keypairEnv) generateFixture(name, passphrase string) keypair.Material {
	e.t.Helper()
	summary, err := e.mgr.Generate(context.Background(), keypair.GenerateInput{
		PairName:   name,
		KeyBits:    2048,
		Passphrase: passphrase,
	}, "root")
	if err != nil {
		e.t.Fatalf("Generate(%q): %v", name, err)
	}
	if summary.PairName != name || summary.PairType != keypair.PairTypeGPG {
		e.t.Fatalf("Generate echo = %+v", summary)
	}
	if !strings.Contains(summary.PublicKey, "BEGIN PGP PUBLIC KEY BLOCK") {
		e.t.Fatalf("Generate echo publicKey is not an armored public block: %q", summary.PublicKey[:40])
	}
	return keypair.Material{PublicArmored: summary.PublicKey}
}

func TestGenerateDefaultsPinned(t *testing.T) {
	if keypair.GenerateBitsDefault != 4096 {
		t.Fatalf("GenerateBitsDefault = %d, want 4096 (spec section 2.3)", keypair.GenerateBitsDefault)
	}
	if keypair.RepoConfigField != "keyPairName" {
		t.Fatalf("RepoConfigField = %q, want keyPairName (spec section 3.3)", keypair.RepoConfigField)
	}
}

func TestGenerateSealAndEcho(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	e.generateFixture("deb-signing", "s3cret")

	// At-rest invariants: both secret columns sealed, public plaintext.
	rec, err := e.md.GpgKeypairs().GetKeypair(ctx, "deb-signing")
	if err != nil || rec == nil {
		t.Fatalf("GetKeypair: %v, %v", rec, err)
	}
	if !remote.IsEncrypted(rec.PrivateKeyEnc) || !remote.IsEncrypted(rec.PassphraseEnc) {
		t.Fatalf("sealed columns lost the enc:v1 form: private=%q pass=%q",
			rec.PrivateKeyEnc[:12], rec.PassphraseEnc[:12])
	}
	if !strings.Contains(rec.PublicKey, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatalf("public column is not the armored public block")
	}
	if rec.Algorithm != "RSA-2048" {
		t.Fatalf("algorithm = %q, want RSA-2048", rec.Algorithm)
	}

	// The echo never carries key material (structural absence: marshal the
	// summary and assert the field names are absent).
	got, err := e.mgr.Get(ctx, "deb-signing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	body, _ := json.Marshal(got)
	for _, forbidden := range []string{"privateKey", "passphrase", "PrivateKey", "Passphrase"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("summary echo carries key material field %q: %s", forbidden, body)
		}
	}
	if got.UpdatedBy != "root" || got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("provenance fields missing: %+v", got)
	}
}

func TestGenerateDuplicateRefuses(t *testing.T) {
	e := newKeypairEnv(t)
	e.generateFixture("kp", "")
	_, err := e.mgr.Generate(context.Background(), keypair.GenerateInput{PairName: "kp", KeyBits: 2048}, "root")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate Generate err = %v, want the exists refusal", err)
	}
}

func TestGenerateBadInput(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	tests := []struct {
		name string
		in   keypair.GenerateInput
		want string
	}{
		{"empty name", keypair.GenerateInput{}, "pairName is required"},
		{"bad charset", keypair.GenerateInput{PairName: "1bad"}, "must start with a letter"},
		{"bad bits", keypair.GenerateInput{PairName: "kp", KeyBits: 1024}, "keyBits"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.mgr.Generate(ctx, tc.in, "root")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// importFixture builds the ImportInput of an independently generated pair.
func importFixture(t *testing.T, name, passphrase string) (keypair.ImportInput, keypair.Material) {
	t.Helper()
	material, err := keypair.GenerateKey(keypair.GenerateParams{
		KeyBits:    2048,
		Passphrase: passphrase,
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return keypair.ImportInput{
		PairName:   name,
		PairType:   keypair.PairTypeGPG,
		Alias:      "alias-" + name,
		PrivateKey: material.PrivateArmored,
		PublicKey:  material.PublicArmored,
		Passphrase: passphrase,
	}, keypair.Material{PublicArmored: material.PublicArmored, PrivateArmored: material.PrivateArmored}
}

func TestImportCreateOrReplaceAndUpdate(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()

	// Import creates.
	first, _ := importFixture(t, "rpm-signing", "pass1")
	if _, err := e.mgr.Import(ctx, first, "root"); err != nil {
		t.Fatalf("Import create: %v", err)
	}
	created, err := e.mgr.Get(ctx, "rpm-signing")
	if err != nil {
		t.Fatalf("Get after import: %v", err)
	}

	// Import replaces (the POST create-or-replace contract).
	second, _ := importFixture(t, "rpm-signing", "pass2")
	if _, err := e.mgr.Import(ctx, second, "root"); err != nil {
		t.Fatalf("Import replace: %v", err)
	}
	replaced, err := e.mgr.Get(ctx, "rpm-signing")
	if err != nil {
		t.Fatalf("Get after replace: %v", err)
	}
	if replaced.CreatedAt != created.CreatedAt {
		t.Fatalf("replace reset CreatedAt: %q -> %q", created.CreatedAt, replaced.CreatedAt)
	}
	if replaced.PublicKey == created.PublicKey {
		t.Fatalf("replace did not swap the public key material")
	}

	// Update refuses an absent name (PUT is the replace-only face).
	absent, _ := importFixture(t, "absent", "")
	if _, err := e.mgr.Update(ctx, absent, "root"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Update absent err = %v, want not found", err)
	}

	// List sees the one pair.
	all, err := e.mgr.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("List = %v, %v; want the single pair", all, err)
	}
}

func TestImportMaterialValidation(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	good, _ := importFixture(t, "kp", "right")
	other, otherMat := importFixture(t, "other", "")

	tests := []struct {
		name string
		in   keypair.ImportInput
		want string
	}{
		{
			name: "non-GPG pairType",
			in:   keypair.ImportInput{PairName: "kp", PairType: "RSA", PrivateKey: good.PrivateKey, PublicKey: good.PublicKey},
			want: "pairType",
		},
		{
			name: "unparsable armor",
			in:   keypair.ImportInput{PairName: "kp", PairType: "GPG", PrivateKey: "not armor", PublicKey: "not armor"},
			want: "not parsable OpenPGP armor",
		},
		{
			name: "public/private mismatch",
			in:   keypair.ImportInput{PairName: "kp", PairType: "GPG", PrivateKey: good.PrivateKey, PublicKey: otherMat.PublicArmored},
			want: "different keys",
		},
		{
			name: "protected key without passphrase",
			in:   keypair.ImportInput{PairName: "kp", PairType: "GPG", PrivateKey: good.PrivateKey, PublicKey: good.PublicKey},
			want: "no passphrase was supplied",
		},
		{
			name: "wrong passphrase",
			in:   keypair.ImportInput{PairName: "kp", PairType: "GPG", PrivateKey: good.PrivateKey, PublicKey: good.PublicKey, Passphrase: "wrong"},
			want: "passphrase does not open",
		},
		{
			name: "bad name charset",
			in:   keypair.ImportInput{PairName: "9bad", PairType: "GPG", PrivateKey: other.PrivateKey, PublicKey: other.PublicKey},
			want: "must start with a letter",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.mgr.Import(ctx, tc.in, "root")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVerifyFaces(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	e.generateFixture("deb-signing", "s3cret")

	// Stored-pair verify (the BinFlow extension).
	if err := e.mgr.VerifyStored(ctx, "deb-signing"); err != nil {
		t.Fatalf("VerifyStored: %v", err)
	}
	if err := e.mgr.VerifyStored(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("VerifyStored absent = %v", err)
	}

	// Material verify (the Artifactory form) accepts good material and
	// refuses the mismatched pair.
	good, _ := importFixture(t, "v-kp", "right")
	if err := e.mgr.VerifyMaterial(good); err != nil {
		t.Fatalf("VerifyMaterial good: %v", err)
	}
	bad := good
	bad.Passphrase = "wrong"
	if err := e.mgr.VerifyMaterial(bad); err == nil {
		t.Fatalf("VerifyMaterial wrong passphrase passed")
	}
}

func TestDeleteInUseGuard(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	e.generateFixture("shared", "")
	e.generateFixture("free", "")

	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"shared"}`})
	e.repos.put(&metadata.Repo{RepoKey: "rpm-l", Type: "local", PackageType: "rpm", Config: `{"keyPairName":"shared"}`})

	err := e.mgr.Delete(ctx, "shared")
	if err == nil || !strings.Contains(err.Error(), "deb-l") || !strings.Contains(err.Error(), "rpm-l") {
		t.Fatalf("in-use Delete err = %v, want the guard naming both referencing repositories", err)
	}

	// The Get echo carries the referencing repositories.
	got, _ := e.mgr.Get(ctx, "shared")
	if len(got.Repositories) != 2 || got.Repositories[0] != "deb-l" || got.Repositories[1] != "rpm-l" {
		t.Fatalf("repositories echo = %v, want [deb-l rpm-l]", got.Repositories)
	}

	// Unreferenced deletes; the referenced one deletes after the repos move on.
	if err := e.mgr.Delete(ctx, "free"); err != nil {
		t.Fatalf("unreferenced Delete: %v", err)
	}
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{}`})
	e.repos.put(&metadata.Repo{RepoKey: "rpm-l", Type: "local", PackageType: "rpm", Config: `{}`})
	if err := e.mgr.Delete(ctx, "shared"); err != nil {
		t.Fatalf("Delete after disassociation: %v", err)
	}
	if err := e.mgr.Delete(ctx, "shared"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("second Delete err = %v, want not found", err)
	}
}

func TestPublicKeyOfRepo(t *testing.T) {
	e := newKeypairEnv(t)
	ctx := context.Background()
	e.generateFixture("deb-signing", "")
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"deb-signing"}`})
	e.repos.put(&metadata.Repo{RepoKey: "unsigned", Type: "local", PackageType: "debian", Config: `{}`})

	key, err := e.mgr.PublicKeyOfRepo(ctx, "deb-l")
	if err != nil || !strings.Contains(key, "BEGIN PGP PUBLIC KEY BLOCK") {
		t.Fatalf("PublicKeyOfRepo = %q, %v", key, err)
	}
	if _, err := e.mgr.PublicKeyOfRepo(ctx, "unsigned"); err == nil {
		t.Fatalf("unassociated repo answered a public key")
	}
	if _, err := e.mgr.PublicKeyOfRepo(ctx, "no-such-repo"); err == nil {
		t.Fatalf("unknown repo answered a public key")
	}
}

func TestNoMasterKeyPosture(t *testing.T) {
	e := newKeypairEnvOpt(t, false) // cipher absent
	ctx := context.Background()

	// Empty store boots; the write refuses.
	if err := e.mgr.BootCheck(ctx); err != nil {
		t.Fatalf("BootCheck with no rows and no cipher: %v", err)
	}
	good, _ := importFixture(t, "kp", "")
	if _, err := e.mgr.Import(ctx, good, "root"); err == nil || !strings.Contains(err.Error(), "no master key") {
		t.Fatalf("Import without cipher err = %v, want the no-master-key refusal", err)
	}

	// Rows without a cipher fail the boot.
	if err := e.md.GpgKeypairs().PutKeypair(ctx, &metadata.GpgKeypairRecord{
		PairName: "stray", PairType: "GPG", Alias: "a", PublicKey: "pub",
		PrivateKeyEnc: "enc:v1:xxx", PassphraseEnc: "enc:v1:yyy", Algorithm: "RSA-2048",
		CreatedAt: "t", UpdatedAt: "t", UpdatedBy: "root",
	}); err != nil {
		t.Fatalf("PutKeypair: %v", err)
	}
	if err := e.mgr.BootCheck(ctx); err == nil || !strings.Contains(err.Error(), "no master key") {
		t.Fatalf("BootCheck with rows and no cipher = %v, want the fail-fast", err)
	}
}

func TestRepoConfigReference(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		want    string
		present bool
		wantErr bool
	}{
		{"absent", `{"quotaBytes":5}`, "", false, false},
		{"set", `{"keyPairName":"kp-1"}`, "kp-1", true, false},
		{"cleared", `{"keyPairName":""}`, "", true, false},
		{"empty blob", ``, "", false, false},
		{"wrong type", `{"keyPairName":42}`, "", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, present, err := keypair.RepoConfigReference(tc.config)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", name)
				}
				return
			}
			if err != nil || name != tc.want || present != tc.present {
				t.Fatalf("got (%q,%v,%v), want (%q,%v,nil)", name, present, err, tc.want, tc.present)
			}
		})
	}
}

// fakeRepoSource is the in-memory RepoSource of the domain tests.
type fakeRepoSource struct{ rows map[string]*metadata.Repo }

func newFakeRepoSource() *fakeRepoSource { return &fakeRepoSource{rows: map[string]*metadata.Repo{}} }

func (f *fakeRepoSource) Get(_ context.Context, repoKey string) (*metadata.Repo, error) {
	if r, ok := f.rows[repoKey]; ok {
		return r, nil
	}
	return nil, metadata.ErrRepoNotFound
}

func (f *fakeRepoSource) List(_ context.Context) ([]*metadata.Repo, error) {
	var out []*metadata.Repo
	for _, r := range f.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoKey < out[j].RepoKey })
	return out, nil
}

func (f *fakeRepoSource) put(r *metadata.Repo) { f.rows[r.RepoKey] = r }
