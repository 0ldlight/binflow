// The webhook Emit facet (M17 T-510, FR-152.3 / ADR-0045 decision 7 +
// ADR-0041 decision 1): the build domain's mutation tails hand their
// events to the unified-event bus WITHOUT importing it — this file defines
// the consumer-side facet (the WebhookEmitter func type + the event's
// four-field facts, webhook.md 3.4's frozen payload), and the assembly
// layer adapts it onto webhook.Bus (httpapi's one-liner, the ADR-0041
// decision 8 gate-func layering). boundary_test.go's import ban is the
// structural guarantee that the seam stays injected.

package build

import (
	"context"
	"log/slog"
)

// The build domain's event types (webhook.md 3.4 — the bus side's
// registered spellings; the adapter maps them one to one).
const (
	EventUploaded = "uploaded"
	EventDeleted  = "deleted"
	EventPromoted = "promoted"
)

// WebhookEvent is the build domain's outbound event: the four payload
// fields verbatim (build_name/build_number/build_started/build_repo —
// Started is the canonical literal the run stores, Repo the resolved
// build_repo logical key) plus the acting principal the adapter projects
// onto the envelope's userContext.
type WebhookEvent struct {
	Type      string // EventUploaded | EventDeleted | EventPromoted
	Name      string
	Number    string
	Started   string
	Repo      string
	Principal *Principal
}

// WebhookEmitter is the injected Emit facet. The adapter's contract is the
// bus's own (ADR-0041 decision 1): synchronous-but-bounded, NEVER fails
// the caller, safe for concurrent use. nil = the bare unit stack (no
// weaving — the mutation tails keep their pre-T-510 behavior).
type WebhookEmitter func(ctx context.Context, e WebhookEvent)

// WithEmitter wires the webhook Emit facet (the assembly passes an adapter
// over the ONE webhook.Bus instance every other domain emits through —
// same-source by construction, the AttachWebhookEmitter precedent).
func WithEmitter(e WebhookEmitter) Option { return func(s *Service) { s.emitter = e } }

// emitWebhook hands one event to the injected facet at the SAME address the
// audit row writes (ADR-0045 decision 7: the success tail's two
// best-effort records land together). The bus's zero-change contract holds
// here too: a panicking ADAPTER is contained — the business operation
// already stood, and the adapter is assembly-layer code this package never
// sees (the bus shields its own body already).
func (s *Service) emitWebhook(ctx context.Context, e WebhookEvent) {
	if s.emitter == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil {
			slog.WarnContext(ctx, "build: webhook emit facet panicked (event dropped, main path unaffected)",
				"event", e.Type, "build", e.Name+"#"+e.Number, "panic", v)
		}
	}()
	s.emitter(ctx, e)
}
