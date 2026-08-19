package remote

import (
	"bytes"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	t.Cleanup(func() {})
	return key
}

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	sealed, err := c.Encrypt("s3cret-upstream")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(sealed, "enc:v1:") {
		t.Fatalf("sealed form = %q, want enc:v1: prefix", sealed)
	}
	if strings.Contains(sealed, "s3cret") {
		t.Fatalf("sealed form leaks the plaintext: %q", sealed)
	}
	got, legacy, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if legacy {
		t.Fatalf("legacy = true for sealed value")
	}
	if got != "s3cret-upstream" {
		t.Fatalf("round trip = %q", got)
	}
}

func TestCipherUniqueNonces(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	a, _ := c.Encrypt("same-password")
	b, _ := c.Encrypt("same-password")
	if a == b {
		t.Fatalf("two seals of one secret must differ (fresh nonce per seal)")
	}
}

func TestCipherWrongKeyFails(t *testing.T) {
	c1, _ := NewCipher(bytes.Repeat([]byte{1}, 32))
	c2, _ := NewCipher(bytes.Repeat([]byte{2}, 32))
	sealed, err := c1.Encrypt("x")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, _, err := c2.Decrypt(sealed); err == nil {
		t.Fatalf("decrypt under the wrong key must fail")
	}
}

func TestCipherNilRefuses(t *testing.T) {
	var c *Cipher
	if sealed, err := c.Encrypt("x"); err == nil || sealed != "" {
		t.Fatalf("nil cipher encrypt must refuse, got (%q, %v)", sealed, err)
	}
	if _, _, err := c.Decrypt("enc:v1:AAAA"); err == nil {
		t.Fatalf("nil cipher decrypt of sealed value must refuse")
	}
	// Legacy plaintext passes through even without a key (the read-side
	// tolerance; the startup migration is what retires such rows).
	secret, legacy, err := c.Decrypt("plain-old-password")
	if err != nil || !legacy || secret != "plain-old-password" {
		t.Fatalf("legacy read = (%q, %t, %v)", secret, legacy, err)
	}
}

func TestCipherMalformedBodies(t *testing.T) {
	c, _ := NewCipher(testKey(t))
	for name, stored := range map[string]string{
		"bad base64": "enc:v1:!!!not-base64!!!",
		"short body": "enc:v1:" + base64.StdEncoding.EncodeToString([]byte{1, 2}),
		"not a seal": "enc:v1:" + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 40)),
	} {
		if _, _, err := c.Decrypt(stored); err == nil {
			t.Fatalf("%s: decrypt must fail", name)
		}
	}
}

func TestLoadKeyFromEnv(t *testing.T) {
	t.Setenv(CredentialsEnvVar, "")
	if key, err := LoadKey(); key != nil || err != nil {
		t.Fatalf("unset env = (%v, %v), want (nil, nil)", key, err)
	}
	t.Setenv(CredentialsEnvVar, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	key, err := LoadKey()
	if err != nil || len(key) != 32 {
		t.Fatalf("valid env = (%d bytes, %v)", len(key), err)
	}
	t.Setenv(CredentialsEnvVar, base64.StdEncoding.EncodeToString([]byte{1, 2, 3}))
	if _, err := LoadKey(); err == nil || !strings.Contains(err.Error(), CredentialsEnvVar) {
		t.Fatalf("wrong-length env must fail naming %s: %v", CredentialsEnvVar, err)
	}
	t.Setenv(CredentialsEnvVar, "not base64 @@@")
	if _, err := LoadKey(); err == nil {
		t.Fatalf("malformed env must fail")
	}
	if _, ok := os.LookupEnv(CredentialsEnvVar); !ok {
		t.Fatalf("env should still be set after LoadKey")
	}
}
