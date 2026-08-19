package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// The at-rest credential chain of ADR-0012 decision 4 (as adopted by PRD
// v1.2 Q1 / FR-15-AC9): remote repository passwords are stored as
//
//	enc:v1:<base64(nonce[12] || ciphertext+tag)>
//
// under an AES-256-GCM key injected through the environment variable
// BINFLOW_REMOTE_CREDENTIALS_KEY (base64 of exactly 32 bytes; never in YAML,
// never on disk). The key is single and unrotated in M3 — rotation means
// re-entering credentials.

// CredentialsEnvVar is the master-key environment variable (ADR-0012
// decision 4; the name is fixed by PRD v1.2).
const CredentialsEnvVar = "BINFLOW_REMOTE_CREDENTIALS_KEY" //nolint:gosec // environment variable NAME, not a credential

// encPrefix marks a value as ciphertext of this scheme. Values without the
// prefix are legacy plaintext (written before the chain landed) and are
// migrated once, at startup, when the key is present.
const encPrefix = "enc:v1:"

// nonceSize is the GCM nonce length the scheme fixes at 12 bytes.
const nonceSize = 12

// ErrNoCredentialsKey reports that at-rest credentials exist (or a new one
// was offered) while no master key is configured: the startup fail-fast and
// the create-time refusal both surface it (FR-15-AC9-2/3).
var ErrNoCredentialsKey = errors.New("no master key: " + CredentialsEnvVar + " is not set")

// ErrBadCredentialsKey reports a key that is set but malformed.
var ErrBadCredentialsKey = errors.New("malformed master key: " + CredentialsEnvVar + " must be base64 of exactly 32 bytes")

// Cipher is the AES-256-GCM seal/open pair over the injected master key. A
// nil *Cipher means "no key configured": encryption refuses (never store
// plaintext), decryption refuses (never serve garbage), and stores holding
// no credential rows boot normally.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher validates the key length and assembles the AEAD.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("remote credentials key: want 32 bytes, got %d: %w", len(key), ErrBadCredentialsKey)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("remote credentials key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("remote credentials gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals secret into the enc:v1 transport form. Every call draws a
// fresh nonce, so encrypting the same password twice yields different text
// (nothing observable correlates rows by credential).
func (c *Cipher) Encrypt(secret string) (string, error) {
	if c == nil {
		return "", ErrNoCredentialsKey
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("remote credentials nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(secret), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// Decrypt opens an enc:v1 value. A value without the prefix is returned
// as-is with legacy=true: the pre-T-66 plaintext spelling, tolerated on read
// so an upgraded store keeps serving while the startup migration is the
// component that retires it.
func (c *Cipher) Decrypt(stored string) (secret string, legacy bool, err error) {
	if !strings.HasPrefix(stored, encPrefix) {
		return stored, true, nil
	}
	if c == nil {
		return "", false, fmt.Errorf("credential value is encrypted but %s is not set: %w", CredentialsEnvVar, ErrNoCredentialsKey)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, encPrefix))
	if err != nil {
		return "", false, fmt.Errorf("remote credentials body: %w", err)
	}
	if len(raw) < nonceSize {
		return "", false, fmt.Errorf("remote credentials body: shorter than the %d-byte nonce", nonceSize)
	}
	opened, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", false, fmt.Errorf("remote credentials open (wrong %s key?): %w", CredentialsEnvVar, err)
	}
	return string(opened), false, nil
}

// IsEncrypted reports whether a stored value already carries the scheme
// prefix (the startup scan's discriminator between ciphertext that needs the
// key and legacy plaintext that needs the one-time migration).
func IsEncrypted(stored string) bool { return strings.HasPrefix(stored, encPrefix) }

// LoadKey reads and validates the master key from the environment. The
// three outcomes the callers distinguish:
//
//	key == nil, err == nil: variable unset — legal while no credential row
//	                       exists (the startup scan decides);
//	key != nil:            a valid 32-byte key;
//	err != nil:            the variable is set but malformed — always fatal,
//	                       an operator said "there is a key" and it is wrong.
func LoadKey() (key []byte, err error) {
	raw, set := os.LookupEnv(CredentialsEnvVar)
	if !set || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	key, err = base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: base64: %w", CredentialsEnvVar, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s: decoded %d bytes: %w", CredentialsEnvVar, len(key), ErrBadCredentialsKey)
	}
	return key, nil
}
