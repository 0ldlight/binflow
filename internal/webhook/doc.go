// Package webhook is the unified-event webhook plane (M13 T-362/T-364,
// FR-114/115 / ADR-0041): subscription CRUD over the 018 tables, the
// single Emit seam the repository domain's mutation tails call, the
// fan-out enqueue into the delivery outbox, the synchronous one-shot send
// the REST test endpoint drives, and the Dispatcher — the worker pool
// that drains the outbox with the anchored retry semantics, the
// rate-limit shape, the troubleshooting record ring and the metrics
// family.
//
// Layering (ADR-0041 decision 1): internal/webhook.Bus is the ONLY domain
// entry — a domain seam calls Emit(ctx, Event) and never learns about
// subscriptions; matching, entitlement gating, envelope instantiation and
// the outbox insert all live here. The package imports metadata (the
// store handle), remote (the SSRF Guard and the enc:v1 Cipher), audit
// (the dead-letter word's event shape) and metrics (the generic registry
// the five webhook families register on); it never imports license — the
// entitlement verdict arrives as an injected gate func, and repo/httpapi
// reach it through consumer-side seams.
//
// Delivery (Dispatcher, T-364): the wire parameters are the T-358 anchor's
// official defaults — retryCount 5 with the first try counted, a FIXED
// 10s retry wait (no backoff curve), retrying only on send failure or
// >=500 answers (4xx/3xx are terminal), a 30s whole-attempt timeout, the
// 1000/s + 10000-burst token bucket pacing attempt starts, and the
// 50000-cap concurrent-delivery rejection at the Emit seam. Dead rows are
// queryable and replayable in the outbox; there is no further persistent
// dead-letter store — the observable outlets are the record ring
// (streamMaxLen 10000 with a 30s janitor), the metric family and one WARN
// line per death (webhook.md 5.1-5.3 / section 7).
package webhook
