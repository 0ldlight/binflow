package adapter

import (
	"context"

	"github.com/lzwzzy/binflow/internal/auth"
)

// principalKey is the context.Context key carrying the authenticated
// principal (nil value = anonymous). httpapi's authenticator middleware
// stores it after resolving the Authorization header.
type principalKey struct{}

// WithPrincipal returns a context that carries p for downstream handlers.
// p may be nil (anonymous): the value is stored as a typed *auth.Principal
// wrapper so PrincipalFrom can distinguish "anonymous" from "absent".
func WithPrincipal(ctx context.Context, p *auth.Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principalBox{p: p})
}

// PrincipalFrom resolves the request principal: *auth.Principal from the
// context httpapi's middleware populated, or nil for anonymous. A context
// without the middleware's value is treated as anonymous — the adapter
// never authenticates itself (that is httpapi's job, architecture section
// 5.1), it only consumes the decision.
func PrincipalFrom(ctx context.Context) *auth.Principal {
	box, ok := ctx.Value(principalKey{}).(principalBox)
	if !ok {
		return nil
	}
	return box.p
}

type principalBox struct{ p *auth.Principal }
