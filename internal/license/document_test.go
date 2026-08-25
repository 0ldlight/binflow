package license_test

// T-279 AC 1 (document v1 + verification chain): table-driven proof of the
// full offline chain — format, static typ/alg/kid/ver checks, ed25519
// signature over the exact payload bytes, and the time window with the 1h
// leeway on both bounds. Test key pairs are generated per run and injected
// through VerifyDocument (the ADR-mandated seam); the embedded production
// constant is never read here.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/license"
)

// testKid is the verify-key id of every test document.
const testKid = "test-key"

// testKeys is one generated signing pair plus its single-entry verify table.
type testKeys struct {
	priv ed25519.PrivateKey
	keys map[string]ed25519.PublicKey
}

func newTestKeys(t *testing.T) testKeys {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return testKeys{priv: priv, keys: map[string]ed25519.PublicKey{testKid: pub}}
}

// spec returns a valid pro document bound to this key (every mutation
// helper keeps the binding: a forged document is a WELL-FORMED signature
// over bad payload bytes, exactly the class the chain must catch).
func (k testKeys) spec(now time.Time) docSpec {
	s := defaultSpec(now)
	s.signWith = k.priv
	return s
}

// docSpec is the mutable payload blueprint: every field defaults to a valid
// pro document and one test case tweaks exactly what it probes.
type docSpec struct {
	typ        string
	alg        string
	kid        string
	ver        int
	licenseID  string
	licensee   string
	tier       string
	issuedAt   string
	notBefore  string
	expiresAt  *string
	addons     []string
	limits     string
	signWith   ed25519.PrivateKey // nil = refuse to sign (guards accidents)
	rawPayload []byte             // non-nil = sign these exact bytes instead
	truncSig   int                // >0 = truncate the signature to this length
}

func defaultSpec(now time.Time) docSpec {
	exp := now.Add(365 * 24 * time.Hour).Format(time.RFC3339)
	return docSpec{
		typ: "binflow-license", alg: "EdDSA", kid: testKid, ver: 1,
		licenseID: "0f1e2d3c-test", licensee: "Acme Corp", tier: "pro",
		issuedAt:  now.Add(-24 * time.Hour).Format(time.RFC3339),
		notBefore: now.Add(-1 * time.Hour).Format(time.RFC3339),
		expiresAt: &exp,
	}
}

// payloadJSON renders the spec's wire form (camelCase, section 15.1.1).
func (s docSpec) payloadJSON(t *testing.T) []byte {
	t.Helper()
	m := map[string]any{
		"typ": s.typ, "alg": s.alg, "kid": s.kid, "ver": s.ver,
		"licenseId": s.licenseID, "licensee": s.licensee, "tier": s.tier,
		"issuedAt": s.issuedAt, "notBefore": s.notBefore,
	}
	if s.expiresAt != nil {
		m["expiresAt"] = *s.expiresAt
	}
	if s.addons != nil {
		m["addons"] = s.addons
	}
	if s.limits != "" {
		m["limits"] = json.RawMessage(s.limits)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

// sign builds the two-segment document for the spec.
func (s docSpec) sign(t *testing.T) string {
	t.Helper()
	payloadBytes := s.rawPayload
	if payloadBytes == nil {
		payloadBytes = s.payloadJSON(t)
	}
	if s.signWith == nil {
		t.Fatal("spec has no signing key")
	}
	sig := ed25519.Sign(s.signWith, payloadBytes)
	if s.truncSig > 0 {
		sig = sig[:s.truncSig]
	}
	return base64.RawURLEncoding.EncodeToString(payloadBytes) + "." +
		base64.RawURLEncoding.EncodeToString(sig)
}

// patch returns a spec whose payload carries one overridden JSON field.
func (s docSpec) patch(t *testing.T, key string, value any) docSpec {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(s.payloadJSON(t), &m); err != nil {
		t.Fatalf("unmarshal payload for patch: %v", err)
	}
	if value == nil {
		delete(m, key)
	} else {
		m[key] = value
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal patched payload: %v", err)
	}
	s.rawPayload = b
	return s
}

// tamperSegment flips the first base64url character of one segment — a
// one-byte change on the wire.
func tamperSegment(t *testing.T, doc string, seg int) string {
	t.Helper()
	parts := strings.Split(doc, ".")
	if len(parts) != 2 {
		t.Fatalf("tamperSegment: doc is not two segments: %q", doc)
	}
	s := []byte(parts[seg])
	if s[0] == 'A' {
		s[0] = 'B'
	} else {
		s[0] = 'A'
	}
	parts[seg] = string(s)
	return parts[0] + "." + parts[1]
}

// tamperPayloadContent changes one byte INSIDE the decoded payload (the
// signature segment stays untouched): still-valid JSON, now-signed-over
// different bytes — the pure signature-check leg.
func tamperPayloadContent(t *testing.T, doc string) string {
	t.Helper()
	head, tail, _ := strings.Cut(doc, ".")
	payload, err := base64.RawURLEncoding.DecodeString(head)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	i := strings.Index(string(payload), "Acme")
	if i < 0 {
		t.Fatalf("payload does not contain the licensee anchor: %s", payload)
	}
	payload[i] = 'X' // Acme -> Xcme
	return base64.RawURLEncoding.EncodeToString(payload) + "." + tail
}

// TestVerifyDocumentChain is the AC-1 table: one row per chain link.
func TestVerifyDocumentChain(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	k := newTestKeys(t)
	other := newTestKeys(t) // a different issuer: the forged-key leg

	perp := k.spec(now)
	perp.tier = "community"
	perp.expiresAt = nil // perpetual

	enter := k.spec(now)
	enter.tier = "enterprise"
	enter.addons = []string{"ha", "xray-integration"}
	enter.limits = `{"maxUsers":50}`

	soonExpiry := now.Add(-30 * time.Minute).Format(time.RFC3339)
	leewayExpired := k.spec(now)
	leewayExpired.expiresAt = &soonExpiry

	futureNB := now.Add(30 * time.Minute).Format(time.RFC3339)
	leewayFuture := k.spec(now)
	leewayFuture.notBefore = futureNB

	tests := []struct {
		name    string
		doc     string
		wantErr error
		check   func(*testing.T, *license.Document)
	}{
		{
			name: "valid pro document verifies and projects",
			check: func(t *testing.T, d *license.Document) {
				if d.Tier != license.TierPro || d.LicenseID != "0f1e2d3c-test" || d.Licensee != "Acme Corp" {
					t.Fatalf("projection wrong: %+v", d)
				}
				if d.Perpetual || d.AddonAllowlist != nil || d.Limits != nil {
					t.Fatalf("absent optionals should stay zero: %+v", d)
				}
			},
		},
		{
			name: "community perpetual document",
			doc:  perp.sign(t),
			check: func(t *testing.T, d *license.Document) {
				if !d.Perpetual || d.Tier != license.TierCommunity {
					t.Fatalf("perpetual community wrong: %+v", d)
				}
			},
		},
		{
			name: "enterprise with addon allowlist and limits",
			doc:  enter.sign(t),
			check: func(t *testing.T, d *license.Document) {
				if d.Tier != license.TierEnterprise {
					t.Fatalf("tier wrong: %+v", d)
				}
				if len(d.AddonAllowlist) != 2 || d.AddonAllowlist[0] != "ha" {
					t.Fatalf("allowlist wrong: %v", d.AddonAllowlist)
				}
				if string(d.Limits) != `{"maxUsers":50}` {
					t.Fatalf("limits not verbatim: %s", d.Limits)
				}
			},
		},
		{
			name:    "one payload byte tampered",
			doc:     tamperPayloadContent(t, k.spec(now).sign(t)),
			wantErr: license.ErrBadSignature,
		},
		{
			name:    "one signature byte tampered",
			doc:     tamperSegment(t, k.spec(now).sign(t), 1),
			wantErr: license.ErrBadSignature,
		},
		{
			name:    "wrong kid",
			doc:     k.spec(now).patch(t, "kid", "bf-lic-9999").sign(t),
			wantErr: license.ErrUnknownKey,
		},
		{
			name:    "empty kid",
			doc:     k.spec(now).patch(t, "kid", "").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "wrong typ",
			doc:     k.spec(now).patch(t, "typ", "jwt").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "wrong alg",
			doc:     k.spec(now).patch(t, "alg", "ES256").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "future version",
			doc:     k.spec(now).patch(t, "ver", 2).sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "tier outside the closed set",
			doc:     k.spec(now).patch(t, "tier", "pro_xray").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "empty licenseId",
			doc:     k.spec(now).patch(t, "licenseId", "").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "expired beyond leeway",
			doc:     expiredBy(k, now, 2*time.Hour).sign(t),
			wantErr: license.ErrExpired,
		},
		{
			name: "expired within the 1h leeway still verifies",
			doc:  leewayExpired.sign(t),
		},
		{
			name:    "notBefore future beyond leeway",
			doc:     notBeforeIn(k, now, 2*time.Hour).sign(t),
			wantErr: license.ErrNotYetValid,
		},
		{
			name: "notBefore within the 1h leeway still verifies",
			doc:  leewayFuture.sign(t),
		},
		{
			name:    "pro tier without expiresAt is malformed",
			doc:     k.spec(now).patch(t, "expiresAt", nil).sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "not a two-segment document",
			doc:     "garbage",
			wantErr: license.ErrBadDocument,
		},
		{
			name:    "three segments",
			doc:     k.spec(now).sign(t) + ".extra",
			wantErr: license.ErrBadDocument,
		},
		{
			name:    "payload segment not base64url",
			doc:     "!!not-base64!!." + k.spec(now).sign(t)[strings.Index(k.spec(now).sign(t), ".")+1:],
			wantErr: license.ErrBadDocument,
		},
		{
			name:    "payload is not JSON",
			doc:     rawPayload(k, []byte("not-json")).sign(t),
			wantErr: license.ErrBadDocument,
		},
		{
			name:    "signature wrong length",
			doc:     truncatedSig(k, now, 10).sign(t),
			wantErr: license.ErrBadDocument,
		},
		{
			name:    "unparseable notBefore",
			doc:     k.spec(now).patch(t, "notBefore", "2026-13-45").sign(t),
			wantErr: license.ErrBadHeader,
		},
		{
			name:    "test-key document under a different issuer key",
			doc:     reSign(k.spec(now), other.priv).sign(t),
			wantErr: license.ErrBadSignature,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := tt.doc
			if doc == "" {
				doc = k.spec(now).sign(t)
			}
			d, err := license.VerifyDocument(doc, k.keys, now)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("VerifyDocument accepted a document that must be rejected")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want wraps %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyDocument: %v", err)
			}
			if tt.check != nil {
				tt.check(t, d)
			}
		})
	}
}

// TestVerifyDocumentWhitespaceTolerance: the install body may carry a
// trailing newline (every `curl -d @file` habit); surrounding whitespace is
// trimmed, inner bytes are not.
func TestVerifyDocumentWhitespaceTolerance(t *testing.T) {
	now := time.Now().UTC()
	k := newTestKeys(t)
	if _, err := license.VerifyDocument("  \n"+k.spec(now).sign(t)+"\n", k.keys, now); err != nil {
		t.Fatalf("whitespace-surrounded document rejected: %v", err)
	}
}

// TestTierClosedSet pins the total order and the spellings.
func TestTierClosedSet(t *testing.T) {
	if license.TierCommunity >= license.TierPro || license.TierPro >= license.TierEnterprise {
		t.Fatal("tier total order broken")
	}
	for _, tier := range license.Tiers() {
		got, ok := license.ParseTier(tier.String())
		if !ok || got != tier {
			t.Fatalf("ParseTier(%q) = (%v,%v), want (%v,true)", tier.String(), got, ok, tier)
		}
	}
	if _, ok := license.ParseTier("enterprise_plus"); ok {
		t.Fatal("ParseTier accepted an out-of-set spelling")
	}
	if got := license.Tier(42).String(); got != "community" {
		t.Fatalf("out-of-set String() = %q, want the community fallback", got)
	}
}

func expiredBy(k testKeys, now time.Time, d time.Duration) docSpec {
	exp := now.Add(-d).Format(time.RFC3339)
	s := k.spec(now)
	s.expiresAt = &exp
	return s
}

func notBeforeIn(k testKeys, now time.Time, d time.Duration) docSpec {
	s := k.spec(now)
	s.notBefore = now.Add(d).Format(time.RFC3339)
	return s
}

func rawPayload(k testKeys, b []byte) docSpec {
	s := k.spec(time.Now().UTC())
	s.rawPayload = b
	return s
}

func truncatedSig(k testKeys, now time.Time, n int) docSpec {
	s := k.spec(now)
	s.truncSig = n
	return s
}

// reSign re-signs the spec's payload bytes with a different private key.
func reSign(s docSpec, priv ed25519.PrivateKey) docSpec {
	s.signWith = priv
	return s
}
