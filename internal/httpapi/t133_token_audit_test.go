package httpapi_test

// T-133: FR-45 token 签发/吊销落审计 (DM-03, D-104-2 修复).
// Acceptance criteria:
//   G31a: token.issue audit on token create (actor / fingerprint / TTL)
//   G31b: token.revoke on revoke (actor / fingerprint), revoked token replay 401
//   G31c: /v2/token short-TTL session tokens never produce token.issue
//   NFR-S3: no plaintext token in detail (redaction chain)
//   Full REST coverage: form/JSON for create, by-value/by-id for revoke

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
)

// t133Audit returns the audit events currently stored in the test harness's
// metadata store. It uses the same Logger chain the server uses (Redact
// included, enabled=true) so the query surface sees exactly what the write
// surface stored.
func t133Audit(t *testing.T, h *harness) []audit.Event {
	t.Helper()
	lg := audit.New(h.md, true)
	// Fetch all events with a high limit (the test suite has a small number).
	page, err := lg.Query(context.Background(), audit.Filter{Limit: 1000})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}
	return page.Events
}

// t133MintToken issues a client_credentials token as admin and returns the
// access_token and token_id. The caller is responsible for closing the
// response body.
func t133MintToken(t *testing.T, h *harness) (string, int64) {
	t.Helper()
	resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte("grant_type=client_credentials"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mint status = %d; body=%s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenID     int64  `json:"token_id"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("mint body %q: %v", body, err)
	}
	if tok.AccessToken == "" || tok.TokenID == 0 {
		t.Fatalf("mint: token=%q id=%d", tok.AccessToken, tok.TokenID)
	}
	return tok.AccessToken, tok.TokenID
}

// ---- G31a: token.issue audit on create ----

// TestTokenIssueAuditForm verifies that a form-encoded token create produces a
// token.issue audit event with the correct actor, fingerprint, and TTL.
func TestTokenIssueAuditForm(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte("grant_type=client_credentials"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   *int64 `json:"expires_in"`
		TokenID     int64  `json:"token_id"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}

	events := t133Audit(t, h)
	// Find the token.issue event.
	var issueEvent *audit.Event
	for i := range events {
		if events[i].Action == audit.ActionTokenIssue {
			issueEvent = &events[i]
			break
		}
	}
	if issueEvent == nil {
		t.Fatal("no token.issue audit event found")
	}
	if issueEvent.Actor != adminUser {
		t.Fatalf("actor = %q, want %q", issueEvent.Actor, adminUser)
	}

	// The fingerprint in the detail must match sha256(plaintext)[:8].
	expectedFP := auth.TokenFingerprint(tok.AccessToken)
	var detail map[string]any
	if err := json.Unmarshal([]byte(issueEvent.Detail), &detail); err != nil {
		t.Fatalf("detail %q is not valid JSON: %v", issueEvent.Detail, err)
	}
	if fp, ok := detail["fingerprint"].(string); !ok || fp != expectedFP {
		t.Fatalf("fingerprint = %v, want %s", detail["fingerprint"], expectedFP)
	}
	// TTL must be present and positive.
	if ttl, ok := detail["ttl_seconds"].(float64); !ok || ttl <= 0 {
		t.Fatalf("ttl_seconds = %v, want a positive number", detail["ttl_seconds"])
	}
}

// TestTokenIssueAuditJSON verifies that a JSON-encoded token create also
// produces a token.issue audit event.
func TestTokenIssueAuditJSON(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte(`{"grant_type":"client_credentials","expires_in":3600}`),
		map[string]string{"Content-Type": "application/json"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   *int64 `json:"expires_in"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}

	events := t133Audit(t, h)
	var issueEvent *audit.Event
	for i := range events {
		if events[i].Action == audit.ActionTokenIssue {
			issueEvent = &events[i]
			break
		}
	}
	if issueEvent == nil {
		t.Fatal("no token.issue audit event found (JSON body)")
	}
	if issueEvent.Actor != adminUser {
		t.Fatalf("actor = %q, want %q", issueEvent.Actor, adminUser)
	}

	expectedFP := auth.TokenFingerprint(tok.AccessToken)
	var detail map[string]any
	if err := json.Unmarshal([]byte(issueEvent.Detail), &detail); err != nil {
		t.Fatalf("detail %q is not valid JSON: %v", issueEvent.Detail, err)
	}
	if fp, ok := detail["fingerprint"].(string); !ok || fp != expectedFP {
		t.Fatalf("fingerprint = %v, want %s", detail["fingerprint"], expectedFP)
	}
	// The TTL should be 3600 (the JSON body specifies it).
	if ttl, ok := detail["ttl_seconds"].(float64); !ok || ttl != 3600 {
		t.Fatalf("ttl_seconds = %v, want 3600", detail["ttl_seconds"])
	}
}

// TestTokenIssueAuditExpiresInZero verifies the audit event for a
// never-expiring token (expires_in=0 -> ttl_seconds=0).
func TestTokenIssueAuditExpiresInZero(t *testing.T) {
	h := newHarness(t)

	resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
		[]byte("grant_type=client_credentials&expires_in=0"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; body=%s", resp.StatusCode, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal([]byte(body), &tok); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}

	events := t133Audit(t, h)
	var issueEvent *audit.Event
	for i := range events {
		if events[i].Action == audit.ActionTokenIssue {
			issueEvent = &events[i]
			break
		}
	}
	if issueEvent == nil {
		t.Fatal("no token.issue audit event found (expires_in=0)")
	}

	expectedFP := auth.TokenFingerprint(tok.AccessToken)
	var detail map[string]any
	if err := json.Unmarshal([]byte(issueEvent.Detail), &detail); err != nil {
		t.Fatalf("detail %q is not valid JSON: %v", issueEvent.Detail, err)
	}
	if fp, ok := detail["fingerprint"].(string); !ok || fp != expectedFP {
		t.Fatalf("fingerprint = %v, want %s", detail["fingerprint"], expectedFP)
	}
	if ttl, ok := detail["ttl_seconds"].(float64); !ok || ttl != 0 {
		t.Fatalf("ttl_seconds = %v, want 0 (never expires)", detail["ttl_seconds"])
	}
}

// ---- G31b: token.revoke audit on revoke ----

// TestTokenRevokeAuditByValue verifies that revoking a token by its plaintext
// value produces a token.revoke audit event with the correct fingerprint.
func TestTokenRevokeAuditByValue(t *testing.T) {
	h := newHarness(t)

	token, _ := t133MintToken(t, h)
	expectedFP := auth.TokenFingerprint(token)

	resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token="+token),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || body != "Token revoked" {
		t.Fatalf("revoke status = %d body = %q", resp.StatusCode, body)
	}

	events := t133Audit(t, h)
	var revokeEvent *audit.Event
	for i := range events {
		if events[i].Action == audit.ActionTokenRevoke {
			revokeEvent = &events[i]
			break
		}
	}
	if revokeEvent == nil {
		t.Fatal("no token.revoke audit event found")
	}
	if revokeEvent.Actor != adminUser {
		t.Fatalf("actor = %q, want %q", revokeEvent.Actor, adminUser)
	}

	var detail map[string]any
	if err := json.Unmarshal([]byte(revokeEvent.Detail), &detail); err != nil {
		t.Fatalf("detail %q is not valid JSON: %v", revokeEvent.Detail, err)
	}
	if fp, ok := detail["fingerprint"].(string); !ok || fp != expectedFP {
		t.Fatalf("fingerprint = %v, want %s", detail["fingerprint"], expectedFP)
	}

	// G31b: revoked token replay must be 401.
	resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, token, nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token replay status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestTokenRevokeAuditByID verifies that revoking a token by its token_id
// produces a token.revoke audit event with the token_id in the detail.
func TestTokenRevokeAuditByID(t *testing.T) {
	h := newHarness(t)

	token, id := t133MintToken(t, h)

	resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token_id="+itoa(id)),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || body != "Token revoked" {
		t.Fatalf("revoke-by-id status = %d body = %q", resp.StatusCode, body)
	}

	events := t133Audit(t, h)
	var revokeEvent *audit.Event
	for i := range events {
		if events[i].Action == audit.ActionTokenRevoke {
			revokeEvent = &events[i]
			break
		}
	}
	if revokeEvent == nil {
		t.Fatal("no token.revoke audit event found (by id)")
	}
	if revokeEvent.Actor != adminUser {
		t.Fatalf("actor = %q, want %q", revokeEvent.Actor, adminUser)
	}

	var detail map[string]any
	if err := json.Unmarshal([]byte(revokeEvent.Detail), &detail); err != nil {
		t.Fatalf("detail %q is not valid JSON: %v", revokeEvent.Detail, err)
	}
	if tid, ok := detail["token_id"].(float64); !ok || int64(tid) != id {
		t.Fatalf("token_id = %v, want %d", detail["token_id"], id)
	}

	// Revoked token replay is 401.
	resp = h.do(http.MethodGet, "/binflow/api/repositories", adminUser, token, nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked token (by id) replay status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestTokenRevokeIdempotentAudit verifies that revoking an already-revoked
// token (by value) does NOT produce a second token.revoke audit event; the
// "Token not found" path is not an audit-worthy event.
func TestTokenRevokeIdempotentAudit(t *testing.T) {
	h := newHarness(t)

	token, _ := t133MintToken(t, h)

	// First revoke: succeeds, produces audit event.
	resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token="+token),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || body != "Token revoked" {
		t.Fatalf("first revoke status = %d body = %q", resp.StatusCode, body)
	}

	// Count the token.revoke events after the first revoke.
	events := t133Audit(t, h)
	revokeCount := 0
	for _, e := range events {
		if e.Action == audit.ActionTokenRevoke {
			revokeCount++
		}
	}
	if revokeCount != 1 {
		t.Fatalf("revoke count after first revoke = %d, want 1", revokeCount)
	}

	// Second revoke: idempotent "Token not found", no audit event.
	resp = h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token="+token),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	body = mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || body != "Token not found" {
		t.Fatalf("second revoke status = %d body = %q", resp.StatusCode, body)
	}

	events = t133Audit(t, h)
	revokeCount = 0
	for _, e := range events {
		if e.Action == audit.ActionTokenRevoke {
			revokeCount++
		}
	}
	if revokeCount != 1 {
		t.Fatalf("revoke count after second revoke = %d, want 1 (idempotent, no second audit)", revokeCount)
	}
}

// ---- NFR-S3: plaintext token never appears in audit detail ----

// TestTokenPlaintextNeverInAudit verifies that the plaintext access token
// never appears in any audit event detail payload, even after the token was
// created.
func TestTokenPlaintextNeverInAudit(t *testing.T) {
	h := newHarness(t)

	token, _ := t133MintToken(t, h)
	// Also revoke to produce a token.revoke event.
	resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token="+token),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	_ = mustGet(t, resp)

	// Fetch all audit events and check that the plaintext token is nowhere.
	events := t133Audit(t, h)
	for _, e := range events {
		if strings.Contains(e.Detail, token) {
			t.Fatalf("audit event %s with id=%d leaks the plaintext token: detail=%s",
				e.Action, e.ID, e.Detail)
		}
	}
}

// TestTokenAuditDetailIsObject verifies that every token.issue and
// token.revoke audit event has a structured Detail object (GE-01 contract).
func TestTokenAuditDetailIsObject(t *testing.T) {
	h := newHarness(t)

	token, _ := t133MintToken(t, h)
	resp := h.do(http.MethodPost, "/binflow/api/security/token/revoke", adminUser, adminPass,
		[]byte("token="+token),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	_ = mustGet(t, resp)

	events := t133Audit(t, h)
	for _, e := range events {
		if e.Action != audit.ActionTokenIssue && e.Action != audit.ActionTokenRevoke {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(e.Detail), &m); err != nil {
			t.Fatalf("token audit event %s detail is not a JSON object: %q -> %v",
				e.Action, e.Detail, err)
		}
		if m == nil {
			t.Fatalf("token audit event %s detail unmarshalled to nil", e.Action)
		}
	}
}

// ---- G31c: /v2/token does NOT produce token.issue ----

// TestDockerTokenNoAuditIssue verifies that /v2/token (docker short-TTL
// session tokens) does NOT produce token.issue audit events. The structural
// isolation is ensured by the docker adapter not importing the audit package
// and the /v2/token route going through the docker handler, not the
// httpapi token handler.
func TestDockerTokenNoAuditIssue(t *testing.T) {
	h := newHarness(t)

	// Snapshot the audit events before the docker token call.
	before := t133Audit(t, h)
	beforeIssueCount := 0
	for _, e := range before {
		if e.Action == audit.ActionTokenIssue {
			beforeIssueCount++
		}
	}

	// Request a docker token via /v2/token.
	resp := h.do(http.MethodGet, "/v2/token?account=admin&client_id=docker&service=binflow",
		adminUser, adminPass, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("docker token status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	// Verify no new token.issue events appeared.
	after := t133Audit(t, h)
	afterIssueCount := 0
	for _, e := range after {
		if e.Action == audit.ActionTokenIssue {
			afterIssueCount++
		}
	}
	if afterIssueCount != beforeIssueCount {
		t.Fatalf("docker /v2/token produced token.issue audit events: before=%d after=%d",
			beforeIssueCount, afterIssueCount)
	}
}

// TestDockerTokenNoAuditIssueFormCredentials verifies that /v2/token with
// form credentials (grant_type=password) also does not produce token.issue
// audit events.
func TestDockerTokenNoAuditIssueFormCredentials(t *testing.T) {
	h := newHarness(t)

	before := t133Audit(t, h)
	beforeIssueCount := 0
	for _, e := range before {
		if e.Action == audit.ActionTokenIssue {
			beforeIssueCount++
		}
	}

	resp := h.do(http.MethodPost, "/v2/token",
		"", "",
		[]byte("grant_type=password&username=admin&password=password&service=binflow"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("docker token (form) status = %d; body=%s", resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()

	after := t133Audit(t, h)
	afterIssueCount := 0
	for _, e := range after {
		if e.Action == audit.ActionTokenIssue {
			afterIssueCount++
		}
	}
	if afterIssueCount != beforeIssueCount {
		t.Fatalf("docker /v2/token (form) produced token.issue: before=%d after=%d",
			beforeIssueCount, afterIssueCount)
	}
}

// TestTokenCreateAuditMultiple verifies that issuing multiple tokens produces
// one token.issue audit event per token.
func TestTokenCreateAuditMultiple(t *testing.T) {
	h := newHarness(t)

	// Mint three tokens.
	for i := 0; i < 3; i++ {
		resp := h.do(http.MethodPost, "/binflow/api/security/token", adminUser, adminPass,
			[]byte("grant_type=client_credentials"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mint %d: status = %d body=%s", i, resp.StatusCode, mustGet(t, resp))
		}
	}

	events := t133Audit(t, h)
	issueCount := 0
	for _, e := range events {
		if e.Action == audit.ActionTokenIssue {
			issueCount++
			if e.Actor != adminUser {
				t.Fatalf("token.issue event has actor=%q, want %q", e.Actor, adminUser)
			}
		}
	}
	if issueCount != 3 {
		t.Fatalf("token.issue count = %d, want 3", issueCount)
	}
}