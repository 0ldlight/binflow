package auth_test

// T-331 acceptance (auth side): the SAML service-provider encryption
// keypair — the §3.3 save-time ensure (generate when absent, reuse when
// present), the forced rotation that invalidates the previous certificate,
// the sealed-at-rest posture, the no-master-key refusal and the boot-time
// unseal fail-fast.

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// newSAMLKeyManager opens a manager over a throwaway store with the real
// enc:v1 chain (the sealed-doc assertions speak the actual wire format).
func newSAMLKeyManager(t *testing.T, cipher auth.SecretCipher) (*auth.ConfigManager, metadata.Store) {
	t.Helper()
	st := openConfigStore(t)
	m, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:     auth.NewConfigStoreAdapter(st.AuthConfigs()),
		Cipher:    cipher,
		PoolGrace: -1,
		Now:       func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewAuthConfigManager: %v", err)
	}
	if err := m.Load(context.Background(), nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m, st
}

// parsePEMCert decodes and parses one CERTIFICATE PEM document.
func parsePEMCert(t *testing.T, pemText string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(pemText))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("not a PEM CERTIFICATE block: %.40s", pemText)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

// TestSAMLSPKeypairAbsentThenRotate: the empty state reads "", rotation
// mints a valid self-signed RSA-2048 certificate with the expected
// validity window, and the read agrees afterwards.
func TestSAMLSPKeypairAbsentThenRotate(t *testing.T) {
	m, _ := newSAMLKeyManager(t, cipherFor(t))
	ctx := context.Background()

	if got, err := m.GetSAMLSPCertificate(ctx); err != nil || got != "" {
		t.Fatalf("absent read = (%q, %v), want (\"\", nil)", got, err)
	}

	certPEM, err := m.RotateSAMLSPKeypair(ctx, "admin")
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	cert := parsePEMCert(t, certPEM)
	if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok {
		t.Fatalf("public key type = %T, want RSA", cert.PublicKey)
	}
	if cert.PublicKey.(*rsa.PublicKey).N.BitLen() != 2048 {
		t.Fatalf("RSA bits = %d, want 2048", cert.PublicKey.(*rsa.PublicKey).N.BitLen())
	}
	if cert.Subject.CommonName != "binflow-saml-sp" || len(cert.Subject.Organization) != 1 || cert.Subject.Organization[0] != "BinFlow" {
		t.Fatalf("subject = %+v, want the anchored BinFlow SP subject", cert.Subject)
	}
	if want := time.Date(2026, 8, 28, 11, 0, 0, 0, time.UTC); !cert.NotBefore.Equal(want) {
		t.Fatalf("NotBefore = %v, want %v (the 1h backdate)", cert.NotBefore, want)
	}
	// 10*365h over 2026..2036 crosses three leap days (2028/2032/2036),
	// so 3650 days from 2026-08-28 lands on 2036-08-25.
	if want := time.Date(2036, 8, 25, 12, 0, 0, 0, time.UTC); !cert.NotAfter.Equal(want) {
		t.Fatalf("NotAfter = %v, want %v (the ~10y lifetime)", cert.NotAfter, want)
	}
	if cert.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		t.Fatalf("KeyUsage = %b, want keyEncipherment set", cert.KeyUsage)
	}

	if got, err := m.GetSAMLSPCertificate(ctx); err != nil || got != certPEM {
		t.Fatalf("post-rotate read drifts: equal=%v err=%v", got == certPEM, err)
	}
}

// TestSAMLSPKeypairRotationInvalidatesPrevious: a forced rotation serves a
// different certificate and the previous one is gone from the read face
// (one live pair per instance — §3.2 regenerate semantics).
func TestSAMLSPKeypairRotationInvalidatesPrevious(t *testing.T) {
	m, _ := newSAMLKeyManager(t, cipherFor(t))
	ctx := context.Background()

	first, err := m.RotateSAMLSPKeypair(ctx, "admin")
	if err != nil {
		t.Fatalf("rotate 1: %v", err)
	}
	second, err := m.RotateSAMLSPKeypair(ctx, "admin")
	if err != nil {
		t.Fatalf("rotate 2: %v", err)
	}
	if first == second {
		t.Fatal("rotation returned the identical certificate — the pair was not replaced")
	}
	parsePEMCert(t, second) // the new artifact must parse too
	if got, _ := m.GetSAMLSPCertificate(ctx); got != second {
		t.Fatal("the read face still serves the previous certificate after rotation")
	}
}

// TestSAMLSPKeypairSealedAtRest: the reserved row's private half is
// enc:v1-sealed and carries no PEM PRIVATE KEY plaintext.
func TestSAMLSPKeypairSealedAtRest(t *testing.T) {
	m, st := newSAMLKeyManager(t, cipherFor(t))
	ctx := context.Background()

	if _, err := m.RotateSAMLSPKeypair(ctx, "admin"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	rec, err := st.AuthConfigs().GetAuthConfig(ctx, "saml_sp_key")
	if err != nil || rec == nil {
		t.Fatalf("reserved row: %v %v", rec, err)
	}
	var doc struct {
		PrivateKeyEnc string `json:"private_key_enc"`
		Certificate   string `json:"certificate"`
	}
	if err := json.Unmarshal([]byte(rec.Doc), &doc); err != nil {
		t.Fatalf("decode reserved doc: %v", err)
	}
	if !strings.HasPrefix(doc.PrivateKeyEnc, "enc:v1:") {
		t.Fatalf("private half not sealed: %.30s", doc.PrivateKeyEnc)
	}
	if strings.Contains(rec.Doc, "PRIVATE KEY") && !strings.HasPrefix(doc.PrivateKeyEnc, "enc:v1:") {
		t.Fatal("plaintext private-key material in the stored row")
	}
	if !strings.Contains(doc.Certificate, "BEGIN CERTIFICATE") {
		t.Fatalf("public half is not a PEM certificate: %.30s", doc.Certificate)
	}
}

// TestSAMLSPKeypairNoMasterKeyRefusal: without the master key every
// generation path refuses (ErrNoMasterKey) and nothing lands.
func TestSAMLSPKeypairNoMasterKeyRefusal(t *testing.T) {
	m, st := newSAMLKeyManager(t, nil)
	ctx := context.Background()

	if _, err := m.RotateSAMLSPKeypair(ctx, "admin"); !errors.Is(err, auth.ErrNoMasterKey) {
		t.Fatalf("rotate without master key = %v, want ErrNoMasterKey", err)
	}
	if rec, err := st.AuthConfigs().GetAuthConfig(ctx, "saml_sp_key"); err != nil || rec != nil {
		t.Fatalf("refused write still landed: %v %v", rec, err)
	}

	// The §3.3 save leg refuses the SAME way: an encrypted-assertion PUT
	// collapses into the nothing-saved posture.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionSAML,
		[]byte(`{"useEncryptedAssertion":true}`), "admin"); !errors.Is(err, auth.ErrNoMasterKey) {
		t.Fatalf("PUT with useEncryptedAssertion without master key = %v, want ErrNoMasterKey", err)
	}
	if _, err := m.GetSAMLSPCertificate(ctx); err != nil {
		t.Fatalf("absent read after refusal: %v", err)
	}
}

// TestPutAuthSectionSAMLEnsuresSPKeypair: the §3.3 flow-2 semantics — an
// encrypted-assertion save generates when absent and REUSES when present;
// a non-encrypted save neither generates nor disturbs an existing pair.
func TestPutAuthSectionSAMLEnsuresSPKeypair(t *testing.T) {
	m, _ := newSAMLKeyManager(t, cipherFor(t))
	ctx := context.Background()

	// A disabled section still carries the §3.3 flow: the spec keys the
	// ensure on useEncryptedAssertion, not on enableIntegration.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionSAML,
		[]byte(`{"useEncryptedAssertion":true}`), "admin"); err != nil {
		t.Fatalf("PUT (ensure) = %v", err)
	}
	first, err := m.GetSAMLSPCertificate(ctx)
	if err != nil || first == "" {
		t.Fatalf("ensure did not generate: (%q, %v)", first, err)
	}

	// Second encrypted save reuses (no silent rotation).
	if _, _, err := m.PutAuthSection(ctx, auth.SectionSAML,
		[]byte(`{"useEncryptedAssertion":true}`), "admin"); err != nil {
		t.Fatalf("PUT (reuse) = %v", err)
	}
	if got, _ := m.GetSAMLSPCertificate(ctx); got != first {
		t.Fatal("the encrypted save silently rotated the pair — §3.3 false arm is reuse")
	}

	// A non-encrypted save leaves the stored pair untouched.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionSAML,
		[]byte(`{"useEncryptedAssertion":false}`), "admin"); err != nil {
		t.Fatalf("PUT (plain) = %v", err)
	}
	if got, _ := m.GetSAMLSPCertificate(ctx); got != first {
		t.Fatal("a non-encrypted save disturbed the stored pair")
	}
}

// TestSAMLSPKeypairBootUnsealFailFast: a stored pair the cipher cannot
// open fails Load with ErrSecretUnreadable; the matching key passes.
func TestSAMLSPKeypairBootUnsealFailFast(t *testing.T) {
	st := openConfigStore(t)
	good := cipherFor(t)
	m, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:  auth.NewConfigStoreAdapter(st.AuthConfigs()),
		Cipher: good,
		Now:    func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("NewAuthConfigManager: %v", err)
	}
	if err := m.Load(context.Background(), nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := m.RotateSAMLSPKeypair(context.Background(), "admin"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	// A DIFFERENT master key must fail the boot (wrong-key rotation).
	other, err := remote.NewCipher([]byte("fedcba9876543210fedcba9876543210"))
	if err != nil {
		t.Fatalf("remote.NewCipher(other): %v", err)
	}
	m2, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:  auth.NewConfigStoreAdapter(st.AuthConfigs()),
		Cipher: other,
	})
	if err != nil {
		t.Fatalf("NewAuthConfigManager(other): %v", err)
	}
	if err := m2.Load(context.Background(), nil); !errors.Is(err, auth.ErrSecretUnreadable) {
		t.Fatalf("Load with the wrong key = %v, want ErrSecretUnreadable", err)
	}

	// The matching key boots clean (the unseal check is a probe, not a
	// consumption).
	m3, err := auth.NewAuthConfigManager(auth.ConfigOptions{
		Store:  auth.NewConfigStoreAdapter(st.AuthConfigs()),
		Cipher: good,
	})
	if err != nil {
		t.Fatalf("NewAuthConfigManager(good): %v", err)
	}
	if err := m3.Load(context.Background(), nil); err != nil {
		t.Fatalf("Load with the right key: %v", err)
	}
}
