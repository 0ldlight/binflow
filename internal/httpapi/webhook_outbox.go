package httpapi

// The webhook outbox row-level REST plane (M17 T-496, FR-159.2 / LC-109):
// the dead-letter replay and the filtered, paginated row query over the
// 018 webhook_deliveries outbox. Artifactory has no public counterpart
// (rest-compat-matrix D09 row 8 — a BinFlow-native C-layer enhancement),
// so the wire follows BinFlow's own v1 conventions rather than the
// official Event-service namespace: the /api/v1 management face, the
// audit read's filter+keyset-cursor page envelope, and the replication
// run family's {id}/verb addressing.
//
// Endpoints (the LC-109 registered form — the ticket's candidate,
// finalized on the wire by webhook_outbox_test.go's legs):
//
//	GET  /api/v1/webhooks/outbox                filter + page the rows
//	POST /api/v1/webhooks/outbox/{id}/replay    reset one dead row
//
// Permissions (the event plane's ADR-0041 decision 9 mapping carried
// over): reads ride system:read — readonly_admin sees the outbox (the
// FR-115.5 posture); the replay write rides system:write AND the webhook
// feature gate (decision 8 seam 1: write verbs pass through RequireAddon
// — community 403s with the license header). The query deliberately
// skips the feature gate: D1's ruling that an already-configured plane
// stays visible on a locked instance extends to its delivery history —
// operators triage dead letters before they license the slot.
//
// Every other verb or spelling under /api/v1/webhooks/** keeps the
// family's E-26 404.

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// auditWebhookReplay is the replay endpoint's audit word (ADR-0041
// decision 10's family, sixth word): spelled here as a literal, the
// props.*/artifact.copy precedent — joining audit.Actions()' picker list
// stays the audit owner's one-liner.
const auditWebhookReplay = "webhook.delivery.replay"

// handleWebhookOutboxList serves GET /api/v1/webhooks/outbox (LC-109):
// one page of outbox rows, newest first, narrowed by ?subscription=
// (key), ?status= (closed set) and ?event_type=, paged by ?limit= (1..
// webhook.OutboxLimitMax, default 50) and ?cursor= (the previous page's
// nextCursor). Each parameter is optional; a malformed limit, an unknown
// status or a cursor this instance never issued answers the E-01 400
// (the audit face's posture); an unknown subscription or event_type is a
// filter over an empty result, not an address, so it answers an empty
// page.
func (s *Server) handleWebhookOutboxList(w http.ResponseWriter, r *http.Request) {
	if s.deps.Webhooks == nil {
		s.log.Warn("httpapi: webhook plane requested but not configured", "path", r.URL.Path)
		writeError(w, http.StatusServiceUnavailable, "webhook subscriptions are not configured on this instance")
		return
	}
	q := r.URL.Query()
	f := webhook.OutboxFilter{
		SubscriptionKey: q.Get("subscription"),
		EventType:       q.Get("event_type"),
		Cursor:          q.Get("cursor"),
	}
	if raw := q.Get("status"); raw != "" {
		switch raw {
		case webhook.StatusPending, webhook.StatusDelivering, webhook.StatusDelivered, webhook.StatusDead:
			f.Status = raw
		default:
			writeError(w, http.StatusBadRequest,
				"status must be one of pending, delivering, delivered, dead (got "+strconv.Quote(raw)+")")
			return
		}
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > webhook.OutboxLimitMax {
			writeError(w, http.StatusBadRequest,
				"limit must be an integer between 1 and "+strconv.Itoa(webhook.OutboxLimitMax))
			return
		}
		f.Limit = n
	}
	page, err := s.deps.Webhooks.Outbox(r.Context(), f)
	if err != nil {
		if errors.Is(err, metadata.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		s.writeWebhookStoreError(w, err)
		return
	}
	rows := make([]*webhook.DeliveryView, 0, len(page.Rows))
	for _, row := range page.Rows {
		rows = append(rows, row.View())
	}
	writeJSONBody(w, http.StatusOK, outboxPageBody{Deliveries: rows, NextCursor: page.NextCursor})
}

// outboxPageBody is the row-level read's envelope. nextCursor is always
// present — "" marks the last page (the audit envelope's rule).
type outboxPageBody struct {
	Deliveries []*webhook.DeliveryView `json:"deliveries"`
	NextCursor string                  `json:"nextCursor"`
}

// handleWebhookOutboxReplay serves POST /api/v1/webhooks/outbox/{id}/replay
// (LC-109): reset one dead row to pending (attempts zeroed, due now) and
// wake the delivery engine — the operator's dead-letter remedy. The body
// is not consumed: replay re-delivers the STORED envelope snapshot
// verbatim (ADR-0041 decision 3 — the snapshot is final wire bytes; the
// row's subscription edits since death do not rewrite it). The answer is
// the post-flip row echo (status pending, attempts 0); a missing row
// answers 404, a row that is not dead answers 409 (the state-conflict
// family: pending/delivering rows are already live, delivered rows are
// finished — replaying either would double-deliver).
func (s *Server) handleWebhookOutboxReplay(w http.ResponseWriter, r *http.Request, idRaw string) {
	if s.deps.Webhooks == nil {
		s.log.Warn("httpapi: webhook plane requested but not configured", "path", r.URL.Path)
		writeError(w, http.StatusServiceUnavailable, "webhook subscriptions are not configured on this instance")
		return
	}
	if !s.requireWebhookAddon(w, r) {
		return
	}
	id, err := url.PathUnescape(idRaw)
	if err != nil || id == "" || len(id) > 64 {
		writeError(w, http.StatusBadRequest, "malformed delivery id in path")
		return
	}
	row, err := s.deps.Webhooks.Replay(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, webhook.ErrNotFound):
			writeError(w, http.StatusNotFound, "delivery not found")
		case errors.Is(err, webhook.ErrNotDead):
			writeError(w, http.StatusConflict, "delivery is not dead (only dead rows replay)")
		default:
			s.writeWebhookStoreError(w, err)
		}
		return
	}
	s.recordWebhookAudit(r, auditWebhookReplay,
		`{"delivery":"`+row.ID+`","subscription":"`+row.SubscriptionKey+`"}`)
	writeJSONBody(w, http.StatusOK, row.View())
}
