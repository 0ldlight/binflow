// Package httpapi hosts the /binflow router, the fixed middleware chain,
// the unified errors[] envelope, health endpoints, system endpoints and
// graceful shutdown (architecture section 7; core by T-14, the compatible
// REST handlers join in T-15).
//
// Routing note: the dispatcher deliberately avoids net/http's ServeMux
// path cleaning for content paths — see router.go's package comment for
// the probe-backed reason (dot-segment requests must reach
// adapter.Layout verbatim so the 400 defense fires, FR-4-AC10).
package httpapi
