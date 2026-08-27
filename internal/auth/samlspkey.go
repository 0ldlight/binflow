package auth

// The SAML service-provider encryption keypair (M11 T-331, closing the
// T-307 registered gap): the self-signed X.509 pair the IdP encrypts
// assertions to when the SAML section carries useEncryptedAssertion=true
// (docs/reverse/auth-integration.md v2 §3.1 field 6 / §3.3 flow 2). The
// public certificate is the artifact the admin hands the IdP; the private
// key exists only to decrypt incoming assertions at login time (the
// assertion-consumption runtime itself is out of FR-92 scope, ADR-0035 §7
// — this file is the management and storage face).
//
// Artifactory semantics (§3.2/§3.3, high confidence):
//
//   - saving a SAML section with useEncryptedAssertion=true generates the
//     pair when absent and REUSES it when present
//     (createStoreAndGetKeyPair(false) — the false is "do not force");
//   - the regenerate verb forces a fresh pair and thereby invalidates the
//     previous certificate ("affects every configuration using encrypted
//     assertions" — the pair is instance-global, one per BinFlow);
//   - the public certificate is served back verbatim (text/plain).
//
// Storage: one reserved auth_configs row (section "saml_sp_key") carrying
// a JSON doc with the enc:v1-sealed PKCS#8 private key and the cleartext
// certificate PEM — the same sealed-secret-in-doc posture the ldap and
// oidc section rows already use (ADR-0035 decision 5). The row is outside
// the closed write set: knownSection refuses it on the public plane and
// rebuildSnapshot's default arm ignores it, so it is reachable only
// through the methods below. A dedicated table was considered and left
// out deliberately: the T-331 area is the auth configuration surface, and
// the reserved row keeps the schema untouched (registered in the T-331
// report as a BinFlow C-level storage decision).

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// samlSPKeySection is the reserved auth_configs row key (never a member of
// authSections; the write plane's closed set keeps refusing it).
const samlSPKeySection = "saml_sp_key"

// Key material parameters. RSA-2048 is the SAML encrypted-assertion
// interop default (IdP xml-encryption transports); the ~10-year validity
// and the CN are BinFlow C-level choices — the reverse spec anchors the
// endpoint behavior but not the certificate profile (registered in the
// T-331 report).
const (
	samlSPKeyBits        = 2048
	samlSPKeyCommonName  = "binflow-saml-sp"
	samlSPKeyOrg         = "BinFlow"
	samlSPKeyCertLifetim = 10 * 365 * 24 * time.Hour
	samlSPKeyBackdate    = time.Hour // clock-skew headroom on NotBefore
	samlSPKeyAlgorithm   = "RSA-2048"
)

// ErrSAMLSPEncryptionKeyAbsent names the read of a not-yet-generated pair.
var ErrSAMLSPEncryptionKeyAbsent = errors.New("saml sp encryption certificate has not been generated")

// samlSPKeyDoc is the sealed wire form of the reserved row. Certificate is
// public material (cleartext PEM); PrivateKeyEnc is always enc:v1-sealed
// (PKCS#8 PEM sealed whole — plaintext key material never lands in the
// store and never leaves it: no method below returns the private key).
type samlSPKeyDoc struct {
	PrivateKeyEnc string `json:"private_key_enc"`
	Certificate   string `json:"certificate"`
	Algorithm     string `json:"algorithm"`
}

// GetSAMLSPCertificate returns the stored service-provider encryption
// certificate PEM, or "" when no pair has been generated yet. Reading the
// public half needs no master key (the certificate is stored cleartext).
func (m *ConfigManager) GetSAMLSPCertificate(ctx context.Context) (string, error) {
	rec, err := m.store.GetAuthConfig(ctx, samlSPKeySection)
	if err != nil {
		return "", fmt.Errorf("auth config: reading %s: %w", samlSPKeySection, err)
	}
	if rec == nil {
		return "", nil
	}
	var doc samlSPKeyDoc
	if err := json.Unmarshal([]byte(rec.Doc), &doc); err != nil {
		return "", fmt.Errorf("auth config: decoding %s: %w", samlSPKeySection, err)
	}
	if doc.Certificate == "" {
		return "", nil
	}
	return doc.Certificate, nil
}

// RotateSAMLSPKeypair forces a fresh service-provider pair (the §3.2
// regenerate semantic, also serving the BinFlow-native generate/replace
// verb) and returns the new public certificate PEM. The previous
// certificate stops being served the moment the row lands: there is
// exactly one live pair per instance.
func (m *ConfigManager) RotateSAMLSPKeypair(ctx context.Context, actor string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureSAMLSPKeypairLocked(ctx, actor, true)
}

// ensureSAMLSPKeypairLocked is the single generation entry point.
// force=false is §3.3 flow 2 (generate when absent, reuse when present —
// createStoreAndGetKeyPair(false)); force=true replaces whatever is
// stored. Callers hold m.mu.
func (m *ConfigManager) ensureSAMLSPKeypairLocked(ctx context.Context, actor string, force bool) (string, error) {
	rec, err := m.store.GetAuthConfig(ctx, samlSPKeySection)
	if err != nil {
		return "", fmt.Errorf("auth config: reading %s: %w", samlSPKeySection, err)
	}
	if rec != nil && !force {
		var doc samlSPKeyDoc
		if err := json.Unmarshal([]byte(rec.Doc), &doc); err != nil {
			return "", fmt.Errorf("auth config: decoding %s: %w", samlSPKeySection, err)
		}
		if doc.Certificate != "" && doc.PrivateKeyEnc != "" {
			return doc.Certificate, nil // §3.3: reuse, never a silent rotation
		}
	}

	privPEM, certPEM, err := generateSAMLSPKeyPair(m.now())
	if err != nil {
		return "", err
	}
	if m.cipher == nil {
		// Same posture as every static-secret write: refuse before anything
		// lands rather than storing key material in the clear.
		return "", fmt.Errorf("auth config: generating %s: %w", samlSPKeySection, ErrNoMasterKey)
	}
	sealed, err := m.cipher.Encrypt(privPEM)
	if err != nil {
		return "", fmt.Errorf("auth config: sealing %s private key: %w", samlSPKeySection, err)
	}
	doc := samlSPKeyDoc{PrivateKeyEnc: sealed, Certificate: certPEM, Algorithm: samlSPKeyAlgorithm}
	blob, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("auth config: encoding %s: %w", samlSPKeySection, err)
	}
	if err := m.store.PutAuthConfig(ctx, &StoredAuthConfig{
		Section:   samlSPKeySection,
		Doc:       string(blob),
		UpdatedAt: m.now().Format(time.RFC3339),
		UpdatedBy: actor,
	}); err != nil {
		return "", fmt.Errorf("auth config: persisting %s: %w", samlSPKeySection, err)
	}
	m.log.InfoContext(ctx, "auth config: saml sp encryption keypair generated",
		"replaced_existing", rec != nil, "actor", actor)
	return certPEM, nil
}

// verifySAMLSPKeyAtBoot fail-fasts on a stored pair the cipher cannot
// unseal (the ErrSecretUnreadable posture of the section secrets — a
// rotated-away master key must surface at boot, not at the first login).
// A missing row is the normal unconfigured state and passes.
func (m *ConfigManager) verifySAMLSPKeyAtBoot(ctx context.Context) error {
	rec, err := m.store.GetAuthConfig(ctx, samlSPKeySection)
	if err != nil {
		return fmt.Errorf("auth config: reading %s: %w", samlSPKeySection, err)
	}
	if rec == nil {
		return nil
	}
	var doc samlSPKeyDoc
	if err := json.Unmarshal([]byte(rec.Doc), &doc); err != nil {
		return fmt.Errorf("auth config: decoding %s: %w", samlSPKeySection, err)
	}
	if doc.Certificate == "" && doc.PrivateKeyEnc == "" {
		return nil // an empty shell row is inert, not a boot condition
	}
	if _, err := parseCertificatePEM(doc.Certificate); err != nil {
		return fmt.Errorf("auth config: %s certificate: %w", samlSPKeySection, err)
	}
	if m.cipher == nil {
		return fmt.Errorf("auth config: %s: %w", samlSPKeySection, ErrSecretUnreadable)
	}
	if _, _, err := m.cipher.Decrypt(doc.PrivateKeyEnc); err != nil {
		return fmt.Errorf("auth config: %s: %w", samlSPKeySection, ErrSecretUnreadable)
	}
	return nil
}

// generateSAMLSPKeyPair mints the self-signed pair: an RSA-2048 key and
// its X.509 certificate (KeyUsage encryption-oriented — keyEncipherment
// for the RSA-OAEP transport xml-encryption uses; no EKU, the
// unrestricted posture IdPs accept most widely).
func generateSAMLSPKeyPair(now time.Time) (privPEM, certPEM string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, samlSPKeyBits)
	if err != nil {
		return "", "", fmt.Errorf("auth config: generating %s rsa key: %w", samlSPKeySection, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("auth config: generating %s serial: %w", samlSPKeySection, err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   samlSPKeyCommonName,
			Organization: []string{samlSPKeyOrg},
		},
		NotBefore:             now.Add(-samlSPKeyBackdate),
		NotAfter:              now.Add(samlSPKeyCertLifetim),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("auth config: creating %s certificate: %w", samlSPKeySection, err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("auth config: marshaling %s private key: %w", samlSPKeySection, err)
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	return privPEM, certPEM, nil
}
