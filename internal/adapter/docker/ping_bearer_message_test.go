package docker

// L004-1: the refused-BEARER arms of the /v2 faces split by token state
// (live reference :8082, 7.161.20 — captures
// a_ping_unknownbearer/a_ping_expiredbearer/a_ping_revokedbearer on the
// ping face, a_tok_expiredbearer on the token endpoint): unknown answers
// the pretty "Props Authentication Token not found", expired the pretty
// "Token failed verification: expired", and the Basic family keeps
// "Bad Credentials". The reference's fourth arm ("Token failed
// verification: revoked") is unreachable under BinFlow's revoke-deletes-
// the-row model — a revoked token verifies as unknown (the model-level
// divergence the L004-1 report registers).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
)

// verifyOnlyTokens is the classification seam's test double: Verify answers
// the constructed verdict, everything else is unreachable on these legs.
type verifyOnlyTokens struct {
	principal *auth.Principal
	err       error
}

func (v *verifyOnlyTokens) Issue(context.Context, string, time.Duration) (*auth.IssuedToken, error) {
	return nil, errors.New("unused on these legs")
}

func (v *verifyOnlyTokens) Verify(context.Context, string) (*auth.Principal, error) {
	return v.principal, v.err
}

func (v *verifyOnlyTokens) Revoke(context.Context, string) error { return nil }

func (v *verifyOnlyTokens) RevokeByID(context.Context, int64) error { return nil }

// TestPingRefusedBearerMessageArms: the ping face's 401 message splits by
// the re-verified token state while the challenge and charset face stay
// the captured shape on every arm.
func TestPingRefusedBearerMessageArms(t *testing.T) {
	expiredErr := fmt.Errorf("%w: %w", auth.ErrTokenExpired, auth.ErrInvalidCredentials)
	unknownErr := fmt.Errorf("%w: %w", auth.ErrTokenUnknown, auth.ErrInvalidCredentials)
	otherErr := fmt.Errorf("auth: token owner disabled: %w", auth.ErrInvalidCredentials)

	cases := []struct {
		name        string
		tokens      *verifyOnlyTokens
		authz       string
		wantMessage string
	}{
		{"unknown bearer", &verifyOnlyTokens{err: unknownErr}, "Bearer never-issued", msgPropsTokenNotFound},
		{"expired bearer", &verifyOnlyTokens{err: expiredErr}, "Bearer stale", msgTokenFailedExpired},
		{"other bearer refusal keeps the generic wording", &verifyOnlyTokens{err: otherErr}, "Bearer odd", msgBadCredentials},
		{"re-verify races valid keeps the generic wording", &verifyOnlyTokens{principal: &auth.Principal{Name: "ci"}}, "Bearer raced", msgBadCredentials},
		{"basic keeps the generic wording", nil, "Basic dXNlcjpwYXNz", msgBadCredentials},
		{"no credential keeps the generic wording", nil, "", msgBadCredentials},
		{"no registry keeps the generic wording", nil, "Bearer never-issued", msgBadCredentials},
	}
	for _, tc := range cases {
		var tokens auth.TokenRegistry
		if tc.tokens != nil {
			tokens = tc.tokens
		}
		h := newTokenHandler(nil, tokens, &fakeUsers{}, Options{AnonymousAccess: true, BaseURL: "http://reg.example"})
		w := &captureWriter{hdr: http.Header{}}
		req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/v2/"}, Host: "reg.example", Header: http.Header{}}
		if tc.authz != "" {
			req.Header.Set("Authorization", tc.authz)
		}
		h.RenderAuthFailure(w, req)

		if w.status != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", tc.name, w.status)
		}
		var eb statusFormBody
		if err := json.Unmarshal([]byte(w.body.String()), &eb); err != nil {
			t.Fatalf("%s: body %q: %v", tc.name, w.body.String(), err)
		}
		if len(eb.Errors) != 1 || eb.Errors[0].Message != tc.wantMessage {
			t.Errorf("%s: message = %q, want %q", tc.name, w.body.String(), tc.wantMessage)
		}
		if got := w.hdr.Get("WWW-Authenticate"); got != `Basic realm="Artifactory Realm"` {
			t.Errorf("%s: challenge = %q, want the Basic realm form", tc.name, got)
		}
		if got := w.hdr.Get("Content-Type"); got != contentTypeJSONCharset {
			t.Errorf("%s: Content-Type = %q, want the ping charset spelling", tc.name, got)
		}
	}

	// The token endpoint's refused Bearer splits the same way (capture
	// a_tok_expiredbearer.h) while its challenge keeps the T-55 Bearer form.
	h := newTokenHandler(nil, &verifyOnlyTokens{err: expiredErr}, &fakeUsers{}, Options{AnonymousAccess: true, BaseURL: "http://reg.example"})
	w := &captureWriter{hdr: http.Header{}}
	req := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: TokenPath}, Host: "reg.example", Header: http.Header{}}
	req.Header.Set("Authorization", "Bearer stale")
	h.RenderAuthFailure(w, req)
	var eb statusFormBody
	if err := json.Unmarshal([]byte(w.body.String()), &eb); err != nil {
		t.Fatalf("token endpoint body %q: %v", w.body.String(), err)
	}
	if len(eb.Errors) != 1 || eb.Errors[0].Message != msgTokenFailedExpired {
		t.Errorf("token endpoint message = %q, want %q", w.body.String(), msgTokenFailedExpired)
	}
	if !bearerChallengeForm(w.hdr.Get("WWW-Authenticate")) {
		t.Errorf("token endpoint challenge = %q, want the Bearer form (T-55)", w.hdr.Get("WWW-Authenticate"))
	}
}

// bearerChallengeForm reports whether the challenge is the Bearer dance
// (the token endpoint's pinned shape).
func bearerChallengeForm(v string) bool {
	return len(v) > 7 && v[:7] == "Bearer "
}
