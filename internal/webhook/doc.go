// Package webhook is the unified-event webhook plane (M13 T-362, FR-114 /
// ADR-0041): subscription CRUD over the 018 tables, the single Emit seam
// the repository domain's mutation tails call, the fan-out enqueue into
// the delivery outbox, and the synchronous one-shot send the REST test
// endpoint drives.
//
// Layering (ADR-0041 decision 1): internal/webhook.Bus is the ONLY domain
// entry — a domain seam calls Emit(ctx, Event) and never learns about
// subscriptions; matching, entitlement gating, envelope instantiation and
// the outbox insert all live here. The package imports metadata (the
// store handle) and remote (the SSRF Guard and the enc:v1 Cipher); it
// never imports license — the entitlement verdict arrives as an injected
// gate func, and repo/httpapi reach it through consumer-side seams.
//
// Delivery (the dispatcher that drains webhook_deliveries with retries,
// dead-lettering and the metrics family) is T-364's engine on the same
// tables; this package only writes pending rows (plus the test path's
// direct, no-outbox send).
package webhook
