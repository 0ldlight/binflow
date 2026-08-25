package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Document-format v1 constants (architecture section 15.1.1). typ and alg
// are fixed by this format revision; a mismatch is refused before any
// signature work.
const (
	// DocType is the payload typ value of format v1.
	DocType = "binflow-license"
	// DocAlg is the payload alg value (JWS spelling of EdDSA).
	DocAlg = "EdDSA"
	// DocVersion is the payload ver value this build understands.
	DocVersion = 1
)

// clockLeeway is the time-window tolerance on both bounds (notBefore and
// expiresAt): licenses are day-granularity while server clocks deviate at
// the millisecond level, so a bounded skew must not flip a verdict. It is
// NOT a grace period — D6 has no grace; anything past expiresAt+leeway is
// expired, immediately.
const clockLeeway = time.Hour

// Verification failure sentinels. They classify the failure for callers
// (the HTTP layer maps them onto the two wire codes LICENSE_INVALID /
// LICENSE_EXPIRED) without ever carrying payload bytes, the licensee or the
// signature in the error text — error strings join logs, and those must
// stay redacted (NFR-S52).
var (
	// ErrBadDocument: not a two-segment document, bad base64url, or payload
	// is not a JSON object.
	ErrBadDocument = errors.New("license: malformed document")
	// ErrBadHeader: typ/alg/ver mismatch or a payload field failed its
	// closed-set/shape check.
	ErrBadHeader = errors.New("license: document header or fields rejected")
	// ErrUnknownKey: kid is absent from the verify-key table.
	ErrUnknownKey = errors.New("license: unknown signing key id")
	// ErrBadSignature: ed25519 verification failed.
	ErrBadSignature = errors.New("license: signature verification failed")
	// ErrExpired: the document's expiresAt is in the past beyond the
	// leeway.
	ErrExpired = errors.New("license: document expired")
	// ErrNotYetValid: the document's notBefore is in the future beyond the
	// leeway.
	ErrNotYetValid = errors.New("license: document not valid yet")
)

// payload is the wire form of the license JSON (camelCase per section
// 15.1.1). Unknown fields are tolerated on purpose: ver guards the version
// contract, and a newer minor shape must not brick verification on
// already-shipped binaries.
type payload struct {
	Typ       string          `json:"typ"`
	Alg       string          `json:"alg"`
	Kid       string          `json:"kid"`
	Ver       int             `json:"ver"`
	LicenseID string          `json:"licenseId"`
	Licensee  string          `json:"licensee"`
	Tier      string          `json:"tier"`
	IssuedAt  string          `json:"issuedAt"`
	NotBefore string          `json:"notBefore"`
	ExpiresAt *string         `json:"expiresAt"` // nil = perpetual (community tier only)
	Addons    []string        `json:"addons"`    // nil/empty = tier-wide unlock
	Limits    json.RawMessage `json:"limits"`    // reserved, echoed verbatim
}

// Document is the verified, in-memory form of a license document: every
// field passed the full chain (format, signature, window). Callers treat it
// as read-only.
type Document struct {
	LicenseID      string
	Licensee       string
	Tier           Tier
	IssuedAt       time.Time
	NotBefore      time.Time
	ExpiresAt      time.Time // zero = perpetual
	Perpetual      bool
	AddonAllowlist []string // nil/empty = no explicit allowlist
	Limits         json.RawMessage
}

// VerifyDocument runs the full offline verification chain (the single
// function the install, startup and ticker paths share — section 15.1.2):
//
//	trim -> split on '.' -> base64url decode -> JSON unmarshal
//	-> typ/alg/kid/ver static checks -> field shape checks
//	-> ed25519.Verify(pub, payloadBytes, sig)
//	-> time window (notBefore - leeway <= now <= expiresAt + leeway)
//
// The signature covers the DECODED payload bytes exactly as transmitted —
// no re-canonicalization, so there is exactly one byte string a signature
// can be about. now is a parameter, not time.Now(): the Manager injects its
// clock so expiry tests never sleep.
//
// The returned error wraps one of the sentinels above; its text never
// contains payload content, the licensee or signature bytes.
func VerifyDocument(doc string, keys map[string]ed25519.PublicKey, now time.Time) (*Document, error) {
	payloadBytes, sig, err := splitDocument(doc)
	if err != nil {
		return nil, err
	}

	var p payload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, fmt.Errorf("%w: payload is not JSON: %v", ErrBadDocument, jsonErrClass(err))
	}

	// Static header checks first (cheap, and a wrong typ/alg must never
	// reach cryptography with a key it was not selected for).
	switch {
	case p.Typ != DocType:
		return nil, fmt.Errorf("%w: typ mismatch", ErrBadHeader)
	case p.Alg != DocAlg:
		return nil, fmt.Errorf("%w: alg mismatch", ErrBadHeader)
	case p.Ver != DocVersion:
		return nil, fmt.Errorf("%w: unsupported ver %d", ErrBadHeader, p.Ver)
	case p.Kid == "":
		return nil, fmt.Errorf("%w: empty kid", ErrBadHeader)
	}
	pub, known := keys[p.Kid]
	if !known {
		return nil, fmt.Errorf("%w: kid %q", ErrUnknownKey, p.Kid)
	}

	d := &Document{}
	tier, ok := ParseTier(p.Tier)
	if !ok {
		return nil, fmt.Errorf("%w: tier not in closed set", ErrBadHeader)
	}
	d.Tier = tier
	if p.LicenseID == "" {
		return nil, fmt.Errorf("%w: empty licenseId", ErrBadHeader)
	}
	d.LicenseID = p.LicenseID
	d.Licensee = p.Licensee

	var errTS error
	if d.IssuedAt, errTS = parseTS(p.IssuedAt); errTS != nil {
		return nil, fmt.Errorf("%w: issuedAt: %w", ErrBadHeader, errTS)
	}
	if d.NotBefore, errTS = parseTS(p.NotBefore); errTS != nil {
		return nil, fmt.Errorf("%w: notBefore: %w", ErrBadHeader, errTS)
	}
	if p.ExpiresAt != nil {
		if d.ExpiresAt, errTS = parseTS(*p.ExpiresAt); errTS != nil {
			return nil, fmt.Errorf("%w: expiresAt: %w", ErrBadHeader, errTS)
		}
	} else if tier != TierCommunity {
		// Section 15.1.1: perpetual (null expiresAt) is a community-tier
		// affordance; a paid tier without an expiry is a malformed grant.
		return nil, fmt.Errorf("%w: %s tier requires expiresAt", ErrBadHeader, tier)
	}
	d.Perpetual = p.ExpiresAt == nil
	d.AddonAllowlist = normalizeAddonIDs(p.Addons)
	d.Limits = p.Limits

	if !ed25519.Verify(pub, payloadBytes, sig) {
		return nil, ErrBadSignature
	}

	// Time window with leeway on both bounds (see clockLeeway).
	if now.Before(d.NotBefore.Add(-clockLeeway)) {
		return nil, fmt.Errorf("%w: notBefore %s", ErrNotYetValid, d.NotBefore.Format(time.RFC3339))
	}
	if !d.Perpetual && now.After(d.ExpiresAt.Add(clockLeeway)) {
		return nil, fmt.Errorf("%w: expiresAt %s", ErrExpired, d.ExpiresAt.Format(time.RFC3339))
	}
	return d, nil
}

// IsVerificationError reports whether err is a document-verification
// failure (one of the chain's sentinels) as opposed to a persistence
// failure — the HTTP layer's split between the 400 LICENSE_* family and the
// 5xx store family.
func IsVerificationError(err error) bool {
	return errors.Is(err, ErrBadDocument) ||
		errors.Is(err, ErrBadHeader) ||
		errors.Is(err, ErrUnknownKey) ||
		errors.Is(err, ErrBadSignature) ||
		errors.Is(err, ErrExpired) ||
		errors.Is(err, ErrNotYetValid)
}

// splitDocument trims surrounding whitespace, splits the two-segment form
// and base64url-decodes both halves (raw/unpadded alphabet, the JWS compact
// convention).
func splitDocument(doc string) (payloadBytes, sig []byte, err error) {
	trimmed := strings.TrimSpace(doc)
	head, tail, ok := strings.Cut(trimmed, ".")
	if !ok || head == "" || tail == "" || strings.Contains(tail, ".") {
		return nil, nil, fmt.Errorf("%w: not a two-segment document", ErrBadDocument)
	}
	payloadBytes, err = base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: payload segment is not base64url", ErrBadDocument)
	}
	sig, err = base64.RawURLEncoding.DecodeString(tail)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: signature segment is not base64url", ErrBadDocument)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, nil, fmt.Errorf("%w: signature is %d bytes, want %d",
			ErrBadDocument, len(sig), ed25519.SignatureSize)
	}
	return payloadBytes, sig, nil
}

// parseTS parses an RFC3339 timestamp (the metadata house format; license
// timestamps follow the same discipline).
func parseTS(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("not RFC3339: %w", err)
	}
	return t.UTC(), nil
}

// normalizeAddonIDs copies the allowlist, dropping empty entries. An empty
// or missing addons list stays nil/empty — the Manager treats
// len()==0 as "no explicit allowlist" (tier-wide unlock); an explicitly
// empty list would be a meaningless grant, so it normalizes to the same
// thing rather than inventing a second "nothing unlocked" state.
func normalizeAddonIDs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// jsonErrClass reduces an encoding/json error to its category word. The
// category is safe for logs; the raw error would quote payload bytes.
func jsonErrClass(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return "type"
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return "syntax"
	}
	return "value"
}
