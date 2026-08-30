package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// The signature chain (webhook.md section 6 / ADR-0041 decision 5):
// HMAC-SHA256 over the STORED payload bytes, hex-encoded — the exact form
// the official verification command produces, so a receiver verifies with
// the documented one-liner:
//
//	echo -n '<actual payload>' | openssl sha256 -hmac "<secret>"
//
// Header name X-JFrog-Event-Auth (official, verbatim). The dual-state
// semantics of use_secret_for_signing (webhook.md 6): false sends the
// SECRET ITSELF in the header (official passthrough mode); true sends the
// hex HMAC of the payload and the secret never travels (the header choice
// for the digest is the spec's single documented auth header — the
// mid-confidence reading registered in webhook.md V1; the acceptance
// assertion anchors to that file).
const EventAuthHeader = "X-JFrog-Event-Auth"

// Signature computes the hex HMAC-SHA256 digest of payload under secret.
func Signature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
