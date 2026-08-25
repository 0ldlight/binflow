package license

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// EmbeddedVerifyKeyID is the key id of the production verify key compiled
// into the binary (ADR-0032 decision 1: the kid -> public key table is a
// compile-time constant with a single element today and the seam for multi
// kid rotation later). Documents carrying any other kid are refused before
// any signature work — switching issuers is a code change, not a config
// toggle.
const EmbeddedVerifyKeyID = "bf-lic-2026"

// embeddedVerifyKeyHex is the production ed25519 public key (32-byte raw
// form, hex encoded). It was generated at T-279 bootstrap with:
//
//	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
//
// and ONLY the public half is stored here. The private counterpart was
// discarded immediately: it must never live in the repository, the build
// environment or any committed artifact (the security floor — writing key
// material is a stop-and-ask action).
//
// Consequence, stated honestly: no license can currently be issued against
// this key, so a stock binary verifies nothing and stays on the community
// floor — which is exactly the fail-safe posture of invariant 1 (an
// unlicensed instance is m9-done, byte for byte). When the first production
// issuance is needed, release engineering generates the authoritative pair
// with `bf license keygen` (T-281) and swaps this one constant; the kid
// seam above absorbs the rotation. Tests NEVER touch this key: they inject
// their own key pairs through the Manager constructor / VerifyDocument
// (the ADR-mandated seam — no build tags, no edits to this file).
const embeddedVerifyKeyHex = "8065b3abc22b8e11b7b58d43140d5470c352816633a2c880e74d5b8913665e00"

// EmbeddedVerifyKeys returns the production verify-key table (kid -> public
// key). The map is rebuilt per call and belongs to the caller; assembly
// passes it straight into Manager options.
func EmbeddedVerifyKeys() (map[string]ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(embeddedVerifyKeyHex)
	if err != nil {
		return nil, fmt.Errorf("license: decoding embedded verify key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("license: embedded verify key is %d bytes, want %d",
			len(raw), ed25519.PublicKeySize)
	}
	return map[string]ed25519.PublicKey{EmbeddedVerifyKeyID: ed25519.PublicKey(raw)}, nil
}
