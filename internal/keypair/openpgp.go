package keypair

import (
	"bytes"
	"crypto"
	"fmt"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// The OpenPGP mechanics (RFC 4880 family — public specifications are the
// behavior source, ADR-0001's clean-room rule; the library is
// ProtonMail/go-crypto v1.4.1, ADR-0038 decision 1). Everything that knows
// the library's types lives in this file so the domain types (keypair.go)
// and the signing seam (signer.go) stay library-agnostic.

// Entity is the parsed OpenPGP key material (the library type, aliased so
// consumers type against the keypair package while the import stays here).
type Entity = openpgp.Entity

// signatureHash is the hash the signing paths pin: SHA-256 is the
// strongest digest every apt and dnf release in the support matrix
// verifies (MD5/SHA-1-only clients are long gone; SHA-512 would drop some
// RPM-era verifiers — rpm.md section 4.3 / debian.md section 4 posture).
var signatureHash = crypto.SHA256

// GenerateParams carries the generation knobs (spec section 2.2/2.3); the
// zero values fall back to the documented defaults.
type GenerateParams struct {
	KeyBits    int    // 0 → GenerateBitsDefault (4096)
	Passphrase string // '' → unprotected key (still sealed at rest)
	UIDName    string // '' → "BinFlow"
	UIDComment string // '' → "repository metadata signing"
	UIDEmail   string // '' → "binflow@localhost"
}

// Generation UID defaults (spec section 2.3).
const (
	defaultUIDName    = "BinFlow"
	defaultUIDComment = "repository metadata signing"
	defaultUIDEmail   = "binflow@localhost"
)

// GenerateKey creates a fresh signing key pair server-side: an RSA primary
// signing key plus the standard encryption subkey (openpgp NewEntity
// shape), no expiry, optionally S2K-protected with the passphrase, and
// serialized to both armored blocks. The returned Material's armored forms
// are what the store seals (import/generation share the at-rest shape).
func GenerateKey(p GenerateParams) (*Material, error) {
	bits := p.KeyBits
	if bits == 0 {
		bits = GenerateBitsDefault
	}
	name := p.UIDName
	if name == "" {
		name = defaultUIDName
	}
	comment := p.UIDComment
	if comment == "" {
		comment = defaultUIDComment
	}
	email := p.UIDEmail
	if email == "" {
		email = defaultUIDEmail
	}
	cfg := &packet.Config{
		RSABits:     bits,
		DefaultHash: signatureHash,
	}
	ent, err := openpgp.NewEntity(name, comment, email, cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: generating RSA-%d key: %w", ErrBadMaterial, bits, err)
	}
	if p.Passphrase != "" {
		if err := ent.EncryptPrivateKeys([]byte(p.Passphrase), cfg); err != nil {
			return nil, fmt.Errorf("%w: protecting the generated key: %w", ErrBadMaterial, err)
		}
	}
	publicArmored, err := armorPublic(ent)
	if err != nil {
		return nil, err
	}
	privateArmored, err := armorPrivate(ent, cfg)
	if err != nil {
		return nil, err
	}
	return &Material{
		Entity:         ent,
		PublicArmored:  publicArmored,
		PrivateArmored: privateArmored,
		Algorithm:      AlgorithmOf(ent),
	}, nil
}

// Material is validated key material: the parsed entity plus the armored
// blocks and the display summary. Import stores the armored blocks
// as-offered (generation: as-produced) — "import verbatim" keeps the
// at-rest blob free of re-encoding drift (ADR-0038 decision 2).
type Material struct {
	Entity         *Entity
	PublicArmored  string
	PrivateArmored string
	Algorithm      string
}

// ParseKeyPair validates an offered pair (the import and verify faces):
// both blocks must parse as OpenPGP armor carrying keys, and the public
// block's primary key must be the same key as the private block's (the
// fingerprint equality — the strongest "these belong together" check short
// of a sign/verify roundtrip, which UnlockKey plus the caller complete).
func ParseKeyPair(publicArmored, privateArmored string) (*Material, error) {
	pub, err := readArmoredKeyRing(publicArmored, "publicKey")
	if err != nil {
		return nil, err
	}
	priv, err := readArmoredKeyRing(privateArmored, "privateKey")
	if err != nil {
		return nil, err
	}
	if len(pub) == 0 {
		return nil, fmt.Errorf("%w: the publicKey block carries no key", ErrBadMaterial)
	}
	if len(priv) == 0 {
		return nil, fmt.Errorf("%w: the privateKey block carries no key", ErrBadMaterial)
	}
	if !bytes.Equal(pub[0].PrimaryKey.Fingerprint, priv[0].PrimaryKey.Fingerprint) {
		return nil, fmt.Errorf("%w: the publicKey and privateKey blocks belong to different keys", ErrBadMaterial)
	}
	return &Material{
		Entity:         priv[0],
		PublicArmored:  publicArmored,
		PrivateArmored: privateArmored,
		Algorithm:      AlgorithmOf(priv[0]),
	}, nil
}

// ParsePrivateKey parses one armored private-key block (the signing-time
// entry: the stored blob is private-only; the public half travels inside
// the private entity).
func ParsePrivateKey(privateArmored string) (*Entity, error) {
	entities, err := readArmoredKeyRing(privateArmored, "privateKey")
	if err != nil {
		return nil, err
	}
	if len(entities) == 0 {
		return nil, fmt.Errorf("%w: the privateKey block carries no key", ErrBadMaterial)
	}
	return entities[0], nil
}

// UnlockKey opens the entity's private keys with the passphrase: a
// protected key without a passphrase refuses (the inert-key trap — it
// could never sign), and a passphrase that does not open the key refuses.
// An unprotected key with an empty passphrase is the legal no-op.
func UnlockKey(ent *Entity, passphrase string) error {
	if passphrase == "" {
		if isProtected(ent) {
			return fmt.Errorf("%w: the private key is passphrase-protected but no passphrase was supplied", ErrBadMaterial)
		}
		return nil
	}
	if err := ent.DecryptPrivateKeys([]byte(passphrase)); err != nil {
		return fmt.Errorf("%w: the passphrase does not open the private key", ErrBadMaterial)
	}
	return nil
}

// unlockForImport is the import/update/verify-material gate: pair
// consistency (ParseKeyPair already ran) plus the passphrase gate.
func unlockForImport(m *Material, passphrase string) error {
	return UnlockKey(m.Entity, passphrase)
}

// isProtected reports whether any private key material in the entity is
// still S2K-encrypted.
func isProtected(ent *Entity) bool {
	if ent.PrivateKey != nil && ent.PrivateKey.Encrypted {
		return true
	}
	for _, sub := range ent.Subkeys {
		if sub.PrivateKey != nil && sub.PrivateKey.Encrypted {
			return true
		}
	}
	return false
}

// algoNames maps the packet algorithm numbers to their display names (the
// RFC 4880/4880bis algorithm registry spellings; the library exposes the
// numbers, not the names).
var algoNames = map[packet.PublicKeyAlgorithm]string{
	packet.PubKeyAlgoRSA:            "RSA",
	packet.PubKeyAlgoRSAEncryptOnly: "RSA",
	packet.PubKeyAlgoRSASignOnly:    "RSA",
	packet.PubKeyAlgoElGamal:        "ElGamal",
	packet.PubKeyAlgoDSA:            "DSA",
	packet.PubKeyAlgoECDH:           "ECDH",
	packet.PubKeyAlgoECDSA:          "ECDSA",
	packet.PubKeyAlgoEdDSA:          "EdDSA",
	packet.PubKeyAlgoX25519:         "X25519",
	packet.PubKeyAlgoX448:           "X448",
	packet.PubKeyAlgoEd25519:        "Ed25519",
	packet.PubKeyAlgoEd448:          "Ed448",
}

// AlgorithmOf renders the display summary of the primary key material
// ("RSA-4096", "Ed25519-256", ...).
func AlgorithmOf(ent *Entity) string {
	name, ok := algoNames[ent.PrimaryKey.PubKeyAlgo]
	if !ok {
		name = fmt.Sprintf("algo-%d", ent.PrimaryKey.PubKeyAlgo)
	}
	bits, err := ent.PrimaryKey.BitLength()
	if err != nil {
		return name
	}
	return fmt.Sprintf("%s-%d", name, bits)
}

// SelfTestSign proves the unlocked entity can actually sign and that its
// own public half verifies the signature (the stored-pair verify's
// end-to-end arm, spec divergence D-5).
func SelfTestSign(ent *Entity) error {
	msg := []byte("binflow keypair self-test\n")
	var sig bytes.Buffer
	if err := openpgp.DetachSign(&sig, ent, bytes.NewReader(msg), signConfig()); err != nil {
		return fmt.Errorf("%w: self-test signature failed: %w", ErrBadMaterial, err)
	}
	if _, err := openpgp.CheckDetachedSignature(openpgp.EntityList{ent}, bytes.NewReader(msg), &sig, signConfig()); err != nil {
		return fmt.Errorf("%w: self-test verification failed: %w", ErrBadMaterial, err)
	}
	return nil
}

// DetachedArmor produces the RFC 4880 armored detached signature of data
// (the repomd.xml.asc / Release.gpg form).
func DetachedArmor(ent *Entity, data []byte) (string, error) {
	var out bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&out, ent, bytes.NewReader(data), signConfig()); err != nil {
		return "", fmt.Errorf("openpgp detached signature: %w", err)
	}
	return out.String(), nil
}

// ClearsignBody produces the RFC 4880 cleartext-signature form of data
// (the InRelease form): the message stays readable in place with the
// signature block attached.
func ClearsignBody(ent *Entity, data []byte) ([]byte, error) {
	key, ok := ent.SigningKey(time.Now())
	if !ok || key.PrivateKey == nil {
		return nil, fmt.Errorf("the key carries no usable signing key: %w", ErrBadMaterial)
	}
	var out bytes.Buffer
	plaintext, err := clearsign.Encode(&out, key.PrivateKey, signConfig())
	if err != nil {
		return nil, fmt.Errorf("openpgp clearsign setup: %w", err)
	}
	if _, err := plaintext.Write(data); err != nil {
		return nil, fmt.Errorf("openpgp clearsign write: %w", err)
	}
	if err := plaintext.Close(); err != nil {
		return nil, fmt.Errorf("openpgp clearsign seal: %w", err)
	}
	return out.Bytes(), nil
}

// signConfig is the shared signing posture: the pinned digest.
func signConfig() *packet.Config {
	return &packet.Config{DefaultHash: signatureHash}
}

// readArmoredKeyRing parses one armored key block with a field-named
// error.
func readArmoredKeyRing(armoredText, field string) (openpgp.EntityList, error) {
	if strings.TrimSpace(armoredText) == "" {
		return nil, fmt.Errorf("%w: %s is required", ErrBadMaterial, field)
	}
	entities, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredText))
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not parsable OpenPGP armor: %w", ErrBadMaterial, field, err)
	}
	return entities, nil
}

// armorPublic serializes the entity's public half to an armored block.
func armorPublic(ent *Entity) (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("armor encode (public): %w", err)
	}
	if err := ent.Serialize(w); err != nil {
		return "", fmt.Errorf("serialize public key: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("armor close (public): %w", err)
	}
	return buf.String(), nil
}

// armorPrivate serializes the entity's private half to an armored block
// (EncryptPrivateKeys must have run already when protection is wanted).
// The WITHOUT-SIGNING variant is mandatory here: SerializePrivate re-signs
// the identity bindings, which needs the plaintext signer — nil once the
// keys are S2K-encrypted — while NewEntity's own signatures are already
// valid and need no re-sign.
func armorPrivate(ent *Entity, cfg *packet.Config) (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	if err != nil {
		return "", fmt.Errorf("armor encode (private): %w", err)
	}
	if err := ent.SerializePrivateWithoutSigning(w, cfg); err != nil {
		return "", fmt.Errorf("serialize private key: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("armor close (private): %w", err)
	}
	return buf.String(), nil
}
