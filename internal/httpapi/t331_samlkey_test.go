package httpapi_test

// T-331 REST acceptance: the SAML service-provider certificate family —
// the full curl-equivalent chain (fresh 404 → generate → download →
// config-referenced reuse → rotation invalidating the previous
// certificate), the capability gates, the managerless 503 and the audit
// trail with zero key material.

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	t331PublicPath     = "/binflow/api/v1/admin/security/saml/config/key/public"
	t331RegeneratePath = "/binflow/api/v1/admin/security/saml/config/key/public/regenerate"
	t331GeneratePath   = "/binflow/api/v1/admin/security/saml/key"
)

// t331Body drains and returns one response body.
func t331Body(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// t331ParseCert proves one response body is a parseable CERTIFICATE PEM.
func t331ParseCert(t *testing.T, body string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(body))
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("body is not a PEM CERTIFICATE:\n%.60s", body)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

// TestSAMLKeyFullChain: generate → download → save-references-reuse →
// rotate → old certificate gone (the one-live-pair semantics).
func TestSAMLKeyFullChain(t *testing.T) {
	a := newAuthConfigStack(t)

	// Fresh instance: the download answers the 404 envelope.
	resp := a.do(http.MethodGet, t331PublicPath, "admin", adminPass, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET public (fresh) = %d, want 404", resp.StatusCode)
	}
	if body := t331Body(t, resp); !strings.Contains(body, "has not been generated") {
		t.Fatalf("fresh 404 body = %s", body)
	}

	// Generate (the BinFlow-native verb): 200 text/plain certificate.
	resp = a.do(http.MethodPost, t331GeneratePath, "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST key = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain (spec §3.2)", ct)
	}
	first := t331Body(t, resp)
	if cert := t331ParseCert(t, first); cert.Subject.CommonName != "binflow-saml-sp" {
		t.Fatalf("CN = %q", cert.Subject.CommonName)
	}

	// Download agrees byte-for-byte.
	resp = a.do(http.MethodGet, t331PublicPath, "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET public = %d, want 200", resp.StatusCode)
	}
	if got := t331Body(t, resp); got != first {
		t.Fatal("the download drifts from the generated certificate")
	}

	// An encrypted-assertion save REUSES the live pair (§3.3 false arm),
	// never silently rotating it.
	resp = a.do(http.MethodPut, "/binflow/api/v1/admin/security/saml/config", "admin", adminPass,
		[]byte(`{"useEncryptedAssertion":true}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT saml config = %d, want 200", resp.StatusCode)
	}
	_ = t331Body(t, resp)
	resp = a.do(http.MethodGet, t331PublicPath, "admin", adminPass, nil)
	if got := t331Body(t, resp); got != first {
		t.Fatal("the encrypted-assertion save rotated the pair — §3.3 is generate-or-REUSE")
	}

	// Regenerate (the Artifactory-compat verb): a NEW certificate is
	// served and the previous one stops being downloadable.
	resp = a.do(http.MethodPut, t331RegeneratePath, "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT regenerate = %d, want 200", resp.StatusCode)
	}
	second := t331Body(t, resp)
	if second == first {
		t.Fatal("regenerate returned the identical certificate")
	}
	c1, c2 := t331ParseCert(t, first), t331ParseCert(t, second)
	if c1.SerialNumber.Cmp(c2.SerialNumber) == 0 {
		t.Fatal("the rotated certificate reuses the serial number")
	}
	resp = a.do(http.MethodGet, t331PublicPath, "admin", adminPass, nil)
	if got := t331Body(t, resp); got != second {
		t.Fatal("the read face still serves the pre-rotation certificate")
	}

	// The generate verb also forces (create-or-replace, one semantic).
	resp = a.do(http.MethodPost, t331GeneratePath, "admin", adminPass, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST key (second) = %d, want 200", resp.StatusCode)
	}
	if third := t331Body(t, resp); third == second {
		t.Fatal("the generate verb did not replace the live pair")
	}
}

// TestSAMLKeyRouteGates: the capability matrix (reads CapSecurityRead —
// the readonly admin may hand the certificate to the IdP; both writes are
// admin actions).
func TestSAMLKeyRouteGates(t *testing.T) {
	a := newAuthConfigStack(t)

	tests := []struct {
		name     string
		method   string
		path     string
		user     string
		pass     string
		wantCode int
	}{
		{"anonymous download", http.MethodGet, t331PublicPath, "", "", http.StatusUnauthorized},
		{"plain user download", http.MethodGet, t331PublicPath, "u1", "u1-pw", http.StatusForbidden},
		{"readonly download ok", http.MethodGet, t331PublicPath, "roat", "roat-pw", http.StatusNotFound},
		{"readonly generate denied", http.MethodPost, t331GeneratePath, "roat", "roat-pw", http.StatusForbidden},
		{"readonly regenerate denied", http.MethodPut, t331RegeneratePath, "roat", "roat-pw", http.StatusForbidden},
		{"plain user generate denied", http.MethodPost, t331GeneratePath, "u1", "u1-pw", http.StatusForbidden},
		{"unknown verb on family", http.MethodDelete, t331PublicPath, "admin", adminPass, http.StatusNotFound},
		{"wrong verb on regenerate", http.MethodPost, t331RegeneratePath, "admin", adminPass, http.StatusNotFound},
		{"wrong verb on generate", http.MethodPut, t331GeneratePath, "admin", adminPass, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := a.do(tt.method, tt.path, tt.user, tt.pass, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantCode {
				t.Fatalf("%s %s as %q = %d, want %d", tt.method, tt.path, tt.user, resp.StatusCode, tt.wantCode)
			}
		})
	}
}

// TestSAMLKeyManagerless503: a stack without the manager keeps the three
// routes at the honest 503.
func TestSAMLKeyManagerless503(t *testing.T) {
	a := newAuthConfigStackNoManager(t)
	for _, spec := range []struct{ method, path string }{
		{http.MethodGet, t331PublicPath},
		{http.MethodPut, t331RegeneratePath},
		{http.MethodPost, t331GeneratePath},
	} {
		resp := a.do(spec.method, spec.path, "admin", adminPass, nil)
		_ = t331Body(t, resp)
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s %s without a manager = %d, want 503", spec.method, spec.path, resp.StatusCode)
		}
	}
}

// TestSAMLKeyAuditTrail: every rotation leaves its samlkey audit row with
// the verb word only — no certificate bytes travel.
func TestSAMLKeyAuditTrail(t *testing.T) {
	a := newAuthConfigStack(t)
	ctx := context.Background()

	gen := a.do(http.MethodPost, t331GeneratePath, "admin", adminPass, nil)
	genCert := t331Body(t, gen)
	regen := a.do(http.MethodPut, t331RegeneratePath, "admin", adminPass, nil)
	regenCert := t331Body(t, regen)
	if gen.StatusCode != http.StatusOK || regen.StatusCode != http.StatusOK {
		t.Fatalf("rotate statuses = %d/%d", gen.StatusCode, regen.StatusCode)
	}

	events, err := a.md.Audits().List(ctx, "", "", 100)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	var sawGenerate, sawRegenerate bool
	for _, e := range events {
		if e.Action != "auth.config.samlkey.generate" && e.Action != "auth.config.samlkey.regenerate" {
			continue
		}
		if e.Action == "auth.config.samlkey.generate" {
			sawGenerate = true
		} else {
			sawRegenerate = true
		}
		if strings.Contains(e.Detail, genCert) || strings.Contains(e.Detail, regenCert) ||
			strings.Contains(e.Detail, "CERTIFICATE") {
			t.Fatalf("audit detail carries certificate material: %s", e.Detail)
		}
		if !strings.Contains(e.Detail, `"saml"`) || !strings.Contains(e.Detail, "redacted") {
			t.Fatalf("detail misses the section/redaction markers: %s", e.Detail)
		}
	}
	if !sawGenerate || !sawRegenerate {
		t.Fatalf("audit rows missing: generate=%v regenerate=%v", sawGenerate, sawRegenerate)
	}

	// The stored reserved row is sealed (no plaintext private key).
	rec, err := a.md.AuthConfigs().GetAuthConfig(ctx, "saml_sp_key")
	if err != nil || rec == nil {
		t.Fatalf("reserved row: %v %v", rec, err)
	}
	if !strings.Contains(rec.Doc, "enc:v1:") || strings.Contains(rec.Doc, "PRIVATE KEY-----") {
		t.Fatalf("reserved row is not sealed: %s", rec.Doc)
	}
}
