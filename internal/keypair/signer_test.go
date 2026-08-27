package keypair_test

// T-319 signing-seam acceptance (spec section 3.6): the detached-armor and
// clearsign forms the T-321 (debian InRelease / Release.gpg) and T-322
// (rpm repomd.xml.asc) legs consume, verified with the library's own
// checkers, plus the error taxonomy that drives the unsigned posture.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgpclearsign "github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/lzwzzy/binflow/internal/keypair"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// newSignerEnv assembles the SigningService over a generated, stored and
// associated pair (RSA-2048 for speed).
func newSignerEnv(t *testing.T, passphrase string) (*keypair.SigningService, *keypairEnv) {
	t.Helper()
	e := newKeypairEnv(t)
	e.generateFixture("repo-key", passphrase)
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"repo-key"}`})
	e.repos.put(&metadata.Repo{RepoKey: "unsigned", Type: "local", PackageType: "debian", Config: `{}`})
	svc, err := keypair.NewSigningService(e.md.GpgKeypairs(), seam(e.cipher), e.repos)
	if err != nil {
		t.Fatalf("NewSigningService: %v", err)
	}
	return svc, e
}

func TestSignerDetachedArmorVerifies(t *testing.T) {
	svc, _ := newSignerEnv(t, "s3cret")
	ctx := context.Background()

	// The repomd.xml.asc / Release.gpg form.
	release := []byte("Origin: BinFlow\nSuite: bookworm\nCodename: bookworm\n")
	sig, err := svc.DetachedArmor(ctx, "deb-l", release)
	if err != nil {
		t.Fatalf("DetachedArmor: %v", err)
	}
	if !strings.Contains(sig, "BEGIN PGP SIGNATURE") {
		t.Fatalf("detached signature is not armored: %q", sig[:32])
	}

	// Verify with the PUBLIC key the plane serves (the repository's own
	// public-key face — the mirror of what dnf/apt do with repomd.xml.key).
	publicKey, err := svc.PublicKey(ctx, "deb-l")
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(publicKey))
	if err != nil || len(ring) == 0 {
		t.Fatalf("public key parse: %v", err)
	}
	block, err := armor.Decode(strings.NewReader(sig))
	if err != nil {
		t.Fatalf("armor decode of the signature: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(ring, bytes.NewReader(release), block.Body, nil); err != nil {
		t.Fatalf("detached verification against the served public key: %v", err)
	}

	// Tampered content must fail the same check (the signature binds bytes).
	if _, err := openpgp.CheckDetachedSignature(ring,
		bytes.NewReader([]byte("Origin: BinFlow\nSuite: lunar\n")), block.Body, nil); err == nil {
		t.Fatalf("tampered content verified — the signature does not bind")
	}
}

func TestSignerClearsignVerifies(t *testing.T) {
	svc, _ := newSignerEnv(t, "s3cret")
	ctx := context.Background()

	// The InRelease form: the message stays readable in place.
	inrelease := []byte("Origin: BinFlow\nSuite: bookworm\nDate: Thu, 27 Aug 2026 12:00:00 UTC\n")
	signed, err := svc.Clearsign(ctx, "deb-l", inrelease)
	if err != nil {
		t.Fatalf("Clearsign: %v", err)
	}
	if !bytes.Contains(signed, inrelease[:len(inrelease)-1]) {
		t.Fatalf("clearsigned body lost the readable message: %q", signed[:80])
	}
	if !bytes.Contains(signed, []byte("-----BEGIN PGP SIGNATURE-----")) {
		t.Fatalf("clearsigned body carries no signature block")
	}
	block, rest := pgpclearsign.Decode(signed)
	if block == nil {
		t.Fatalf("clearsign.Decode returned no block (rest %q)", rest[:40])
	}
	if !bytes.Equal(bytes.TrimSpace(block.Plaintext), bytes.TrimSpace(inrelease)) {
		t.Fatalf("plaintext roundtrip mismatch: %q", block.Plaintext)
	}
	ring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(mustPublicKey(ctx, t, svc, "deb-l")))
	if err != nil {
		t.Fatalf("public key parse: %v", err)
	}
	if _, err := block.VerifySignature(ring, nil); err != nil {
		t.Fatalf("clearsign verification: %v", err)
	}
}

func mustPublicKey(ctx context.Context, t *testing.T, svc *keypair.SigningService, repoKey string) string {
	t.Helper()
	key, err := svc.PublicKey(ctx, repoKey)
	if err != nil {
		t.Fatalf("PublicKey(%s): %v", repoKey, err)
	}
	return key
}

func TestSignerErrorTaxonomy(t *testing.T) {
	svc, _ := newSignerEnv(t, "s3cret")
	ctx := context.Background()

	// Unassociated repository → ErrNoKeypair (the unsigned-mode signal).
	_, err := svc.DetachedArmor(ctx, "unsigned", []byte("x"))
	if !wantErrIs(t, err, keypair.ErrNoKeypair) {
		return
	}
	_, err = svc.Clearsign(ctx, "unsigned", []byte("x"))
	if !wantErrIs(t, err, keypair.ErrNoKeypair) {
		return
	}
	// Unknown repository → the same unsigned signal (never a crash).
	if _, err := svc.PublicKey(ctx, "no-such-repo"); !wantErrIs(t, err, keypair.ErrNoKeypair) {
		return
	}
}

func TestSignerNoMasterKeyDegrades(t *testing.T) {
	// A sealed pair without the master key (the instance lost
	// BINFLOW_REMOTE_CREDENTIALS_KEY): the seam answers ErrUnavailable
	// (skip signing), never panics and never leaks. The row is seeded
	// directly — the write path refuses without the key by design.
	e := newKeypairEnvOpt(t, false)
	if err := e.md.GpgKeypairs().PutKeypair(context.Background(), &metadata.GpgKeypairRecord{
		PairName: "repo-key", PairType: keypair.PairTypeGPG, Alias: "a",
		PublicKey:     "-----BEGIN PGP PUBLIC KEY BLOCK-----\n(seeded)\n",
		PrivateKeyEnc: "enc:v1:seeded", PassphraseEnc: "enc:v1:seeded",
		Algorithm: "RSA-2048", CreatedAt: "t", UpdatedAt: "t", UpdatedBy: "root",
	}); err != nil {
		t.Fatalf("PutKeypair: %v", err)
	}
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"repo-key"}`})
	svc, err := keypair.NewSigningService(e.md.GpgKeypairs(), nil, e.repos)
	if err != nil {
		t.Fatalf("NewSigningService: %v", err)
	}
	if _, err := svc.DetachedArmor(context.Background(), "deb-l", []byte("x")); !wantErrIs(t, err, keypair.ErrUnavailable) {
		return
	}
	if _, err := svc.Clearsign(context.Background(), "deb-l", []byte("x")); !wantErrIs(t, err, keypair.ErrUnavailable) {
		return
	}
	// The public key still serves (no unsealing needed).
	if _, err := svc.PublicKey(context.Background(), "deb-l"); err != nil {
		t.Fatalf("PublicKey without master key: %v", err)
	}
}

func TestSignerWrongStoredPassphraseDegrades(t *testing.T) {
	// A pair generated with passphrase A but stored under passphrase B
	// (hand-edited store): the seam answers ErrUnavailable at open time.
	e := newKeypairEnv(t)
	material, err := keypair.GenerateKey(keypair.GenerateParams{KeyBits: 2048, Passphrase: "actual"})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	privateEnc, err := e.cipher.Encrypt(material.PrivateArmored)
	if err != nil {
		t.Fatalf("encrypt private: %v", err)
	}
	wrongPass, err := e.cipher.Encrypt("other")
	if err != nil {
		t.Fatalf("encrypt pass: %v", err)
	}
	if err := e.md.GpgKeypairs().PutKeypair(context.Background(), &metadata.GpgKeypairRecord{
		PairName: "repo-key", PairType: keypair.PairTypeGPG, Alias: "a",
		PublicKey: material.PublicArmored, PrivateKeyEnc: privateEnc, PassphraseEnc: wrongPass,
		Algorithm: "RSA-2048", CreatedAt: "t", UpdatedAt: "t", UpdatedBy: "root",
	}); err != nil {
		t.Fatalf("PutKeypair: %v", err)
	}
	e.repos.put(&metadata.Repo{RepoKey: "deb-l", Type: "local", PackageType: "debian", Config: `{"keyPairName":"repo-key"}`})
	svc, _ := keypair.NewSigningService(e.md.GpgKeypairs(), seam(e.cipher), e.repos)
	if _, err := svc.DetachedArmor(context.Background(), "deb-l", []byte("x")); !wantErrIs(t, err, keypair.ErrUnavailable) {
		return
	}
}

// wantErrIs reports whether err wraps want (a Fatalf on the miss).
func wantErrIs(t *testing.T, err, want error) bool {
	t.Helper()
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
	return true
}
