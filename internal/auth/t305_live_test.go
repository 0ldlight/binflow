package auth_test

// T-305's LIVE Keycloak leg (AC3: 改 issuer / 失效旧 IdP). Gated on
// BINFLOW_T305_KEYCLOAK_A / _B — two real Keycloak issuers — so the suite
// stays hermetic by default and QA (or a developer with the containers up)
// runs the real-discovery, real-issuer-swap round trip:
//
//	1. PUT the OIDC section against issuer A (real discovery at write time);
//	2. PUT a candidate against a DEAD issuer — the write REFUSES and the
//	   live configuration stays on A (D7);
//	3. PUT the section against issuer B — the swap succeeds and the very
//	   next consultation walks B (the OAuth2 endpoints flipped hosts);
//	4. stop A ("失效旧 IdP") — B keeps answering.
//
// The full signed-token round trip stays in the hermetic suites (the
// httpapi OIDC tests mint and verify real RS256 tokens against in-process
// IdPs); this leg's subject is the REAL discovery wire and the live swap.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
)

func TestAuthConfigKeycloakLiveIssuerSwap(t *testing.T) {
	issuerA, issuerB := os.Getenv("BINFLOW_T305_KEYCLOAK_A"), os.Getenv("BINFLOW_T305_KEYCLOAK_B")
	if issuerA == "" || issuerB == "" {
		t.Skip("live Keycloak leg: set BINFLOW_T305_KEYCLOAK_A/_B to two issuer URLs (e.g. http://127.0.0.1:8081/realms/master)")
	}
	ctx := context.Background()
	st := openConfigStore(t)
	m := newTestManager(t, st, func(o *auth.ConfigOptions) {
		o.OIDCResolver = auth.NewOIDCResolver(st.Users())
	})
	if err := m.Load(ctx, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
	svc := auth.NewFromStore(st, false).WithAuthConfig(m)

	// A's discovery document is really reachable (the leg's precondition).
	if err := probeLiveIssuer(ctx, issuerA); err != nil {
		t.Fatalf("issuer A unreachable: %v", err)
	}
	if err := probeLiveIssuer(ctx, issuerB); err != nil {
		t.Fatalf("issuer B unreachable: %v", err)
	}

	body := func(issuer string) []byte {
		return []byte(fmt.Sprintf(`{"enabled":true,"issuer_url":%q,"client_id":"binflow-live","redirect_url":"http://binflow.example.com/binflow/api/v1/oidc/callback"}`, issuer))
	}

	// 1. Arm against the real issuer A.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC, body(issuerA), "admin"); err != nil {
		t.Fatalf("PUT(issuer A) against the live Keycloak = %v, want success", err)
	}
	if !svc.OIDCWired() {
		t.Fatal("the OIDC arm did not activate against the live issuer")
	}
	endpointsA := m.CurrentOIDC().OAuth2Config().Endpoint.AuthURL

	// 2. A dead-issuer candidate REFUSES; A stays in force.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC, body("http://127.0.0.1:1/realms/master"), "admin"); err == nil {
		t.Fatal("PUT(dead issuer) succeeded, want the discovery refusal (D7)")
	}
	if got := m.CurrentOIDC().OAuth2Config().Endpoint.AuthURL; got != endpointsA {
		t.Fatalf("the refused PUT moved the live configuration: %s", got)
	}

	// 3. The swap to B: real discovery against the second Keycloak, the
	// next consultation walks B.
	if _, _, err := m.PutAuthSection(ctx, auth.SectionOIDC, body(issuerB), "admin"); err != nil {
		t.Fatalf("PUT(issuer B) = %v, want success", err)
	}
	endpointsB := m.CurrentOIDC().OAuth2Config().Endpoint.AuthURL
	if endpointsB == endpointsA || !strings.Contains(endpointsB, hostOf(issuerB)) {
		t.Fatalf("the live provider still walks A after the swap: %s", endpointsB)
	}
	if !svc.OIDCWired() {
		t.Fatal("the arm dropped during the live swap")
	}

	// 4. A is dead to us now: a candidate pointing back at A while A is
	// down would refuse (QA stops the container between the legs); the
	// in-suite stand-in is the dead-port check already run. What the test
	// CAN pin here: the swap did not disturb the store's sealed row.
	rec, err := st.AuthConfigs().GetAuthConfig(ctx, auth.SectionOIDC)
	if err != nil || rec == nil || strings.Contains(rec.Doc, "http://127.0.0.1:1") {
		t.Fatalf("the stored row drifted from the last accepted write: %+v (%v)", rec, err)
	}
}

// probeLiveIssuer asserts one issuer's discovery document answers 200.
func probeLiveIssuer(ctx context.Context, issuer string) error {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet,
		strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discovery status %d", resp.StatusCode)
	}
	return nil
}

// hostOf extracts scheme://host of an issuer URL (log-friendly).
func hostOf(issuer string) string {
	if i := strings.Index(issuer[8:], "/"); i >= 0 { // past "http://" / "https://"
		return issuer[:8+i]
	}
	return issuer
}
