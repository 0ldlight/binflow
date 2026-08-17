package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/lzwzzy/binflow/internal/auth"
)

// ctxKey namespaces the request-scoped values this package threads through
// the middleware chain.
type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyPrincipal
	ctxKeyLogFields
)

// HeaderRequestID is the response/request header carrying the per-request
// correlation id (architecture section 7.2 first middleware).
const HeaderRequestID = "X-Request-Id"

// newRequestID mints a 128-bit random id. Randomness, not a counter: the id
// lands in access logs and error diagnostics and must not leak request
// volume ordering to anonymous readers.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand never fails on the supported platforms; degrade to a
		// deterministic marker rather than 500-ing a request over telemetry.
		return "unavailable"
	}
	return hex.EncodeToString(b[:])
}

// withRequestID stores the id for the access log.
func withRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// requestIDFrom returns the id stored by the requestID middleware ("" when
// absent — e.g. unit tests driving a bare handler).
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// principalBox wraps the principal so a nil (anonymous) principal stays
// distinguishable from "middleware never ran" (untyped nil interface).
type principalBox struct{ p *auth.Principal }

// withPrincipal stores the authenticated principal; p may be nil
// (anonymous, ADR-0009). The value crosses into adapters through
// adapter.WithPrincipal at the dispatch boundary (same boxed shape, their
// own key), keeping the two packages' context keys independent.
func withPrincipal(ctx context.Context, p *auth.Principal) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipal, principalBox{p: p})
}

// principalFrom returns the principal stored by the authenticator
// middleware; nil means anonymous or "chain not run" — both route through
// the same deny-by-default authorization path, so they need no distinction.
func principalFrom(ctx context.Context) *auth.Principal {
	box, ok := ctx.Value(ctxKeyPrincipal).(principalBox)
	if !ok {
		return nil
	}
	return box.p
}

// userName resolves the access-log "user" field: the authenticated name, or
// the literal "anonymous" (ticket: "user 匿名记 anonymous").
func userName(p *auth.Principal) string {
	if p == nil || p.Name == "" {
		return "anonymous"
	}
	return p.Name
}
