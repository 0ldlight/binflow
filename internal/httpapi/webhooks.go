package httpapi

// The webhook subscription REST plane (M13 T-362, FR-114 / ADR-0041
// decision 9): the seven official endpoints under the Event-service
// namespace, mounted on the BinFlow prefix as /binflow/event/api/v1/** (the
// E-26 no-root-mirror rule; the official /event/api/v1 spelling keeps its
// segments verbatim after the prefix). Management plane per ADR-0034:
// dispatchAPI-style explicit routes with routeAuth gates, never an adapter
// mount.
//
// Permissions (ADR-0041 decision 9, the internal:webhook mapping): reads
// (list/get/troubleshooting) ride system:read — readonly_admin sees the
// plane (FR-115.5); writes (create/update/delete/test) ride system:write.
// The webhook FEATURE gate rides the slot (decision 8): write verbs pass
// through RequireAddon (community 403 + the license header; the
// addons.disabled breaker's no-header refusal), reads stay open (D1 —
// subscriptions already configured remain visible on a locked instance).
//
// The router case that reaches dispatchEventAPI is the assembly's
// one-liner (conductor-owned router wiring):
//
//	case rest == "/event" || strings.HasPrefix(rest, "/event/"):
//	    s.dispatchEventAPI(w, r, strings.TrimPrefix(rest, "/event"))

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/webhook"
)

// WebhookAddonID is the feature slot's identifier (slots.go's 19th slot).
const WebhookAddonID = "webhook"

// The plane's audit actions (ADR-0041 decision 10's five words minus the
// dispatcher-owned webhook.dead_letter): spelled here as literals, the
// props.*/artifact.copy precedent — joining audit.Actions()' picker list is
// the audit owner's one-liner, flagged in the T-362 log.
const (
	auditWebhookCreate = "webhook.subscription.create"
	auditWebhookUpdate = "webhook.subscription.update"
	auditWebhookDelete = "webhook.subscription.delete"
	auditWebhookTest   = "webhook.subscription.test"
)

// WebhookPlane is the consumer-side seam the assembled webhook.Bus
// satisfies: the subscription CRUD face, the synchronous test send, the
// troubleshooting read, the outbox row-level face (FR-159.2) and the Emit
// seam the domain handlers share. nil in Deps keeps the endpoints at the
// honest 503 (unit stacks only; every assembled server wires the Bus).
type WebhookPlane interface {
	Create(ctx context.Context, req *webhook.SubscriptionRequest, actor string) (*webhook.Subscription, error)
	Get(ctx context.Context, key string) (*webhook.Subscription, error)
	List(ctx context.Context) ([]*webhook.Subscription, error)
	Update(ctx context.Context, key string, req *webhook.SubscriptionRequest, actor string) (*webhook.Subscription, error)
	Delete(ctx context.Context, key string) error
	Test(ctx context.Context, req *webhook.SubscriptionRequest, actor webhook.Actor) (*webhook.TestOutcome, error)
	Troubleshooting(ctx context.Context, q webhook.TroubleshootQuery) ([]webhook.TroubleshootingRecord, error)
	Outbox(ctx context.Context, f webhook.OutboxFilter) (*webhook.OutboxPage, error)
	Replay(ctx context.Context, deliveryID string) (*webhook.OutboxRow, error)
	Emit(ctx context.Context, ev webhook.Event)
}

// dispatchEventAPI routes /binflow/event/** after stripping /binflow/event
// (the dispatchAPI family's shape). rest arrives with a leading slash.
func (s *Server) dispatchEventAPI(w http.ResponseWriter, r *http.Request, rest string) {
	if s.deps.Webhooks == nil {
		s.log.Warn("httpapi: webhook plane requested but not configured", "path", rest)
		writeError(w, http.StatusServiceUnavailable, "webhook subscriptions are not configured on this instance")
		return
	}
	switch {
	case rest == "/api/v1/subscriptions":
		switch r.Method {
		case http.MethodGet:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleWebhookList)
		case http.MethodPost:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleWebhookCreate)
		default:
			notImplemented(w, r.Method+" /event/api/v1/subscriptions")
		}
	case rest == "/api/v1/subscriptions/test" && r.Method == http.MethodPost:
		// The draft-configuration trial send (a FULL body, never a key
		// reference — webhook.md section 1 row 6). Routed before the {key}
		// arm so the literal "test" can never collide with a subscription
		// key (keys may legally spell "test").
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite}, s.handleWebhookTest)
	case strings.HasPrefix(rest, "/api/v1/subscriptions/"):
		key, err := url.PathUnescape(strings.TrimPrefix(rest, "/api/v1/subscriptions/"))
		if err != nil || key == "" || strings.Contains(key, "/") {
			writeError(w, http.StatusBadRequest, "malformed subscription key in path")
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead},
				func(w http.ResponseWriter, r *http.Request) { s.handleWebhookGet(w, r, key) })
		case http.MethodPut:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
				func(w http.ResponseWriter, r *http.Request) { s.handleWebhookUpdate(w, r, key) })
		case http.MethodDelete:
			s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemWrite},
				func(w http.ResponseWriter, r *http.Request) { s.handleWebhookDelete(w, r, key) })
		default:
			notImplemented(w, r.Method+" /event/api/v1/subscriptions/{key}")
		}
	case rest == "/api/v1/troubleshooting" && r.Method == http.MethodGet:
		s.enforce(w, r, routeAuth{required: true, manage: auth.CapSystemRead}, s.handleWebhookTroubleshooting)
	default:
		// The E-26 family 404 (unknown spelling or verb inside the plane).
		notFoundPrefixHint(w, prefix+"/event"+rest)
	}
}

// requireWebhookAddon is the write-verb feature gate (ADR-0041 decision 8,
// seam 1): RequireAddon over the slot's own MinTier so the tier matrix
// stays slots.go's single source. false means the refusal was rendered.
func (s *Server) requireWebhookAddon(w http.ResponseWriter, r *http.Request) bool {
	minTier := license.TierPro
	if s.deps.Addons != nil {
		if a, ok := s.deps.Addons.ByID(WebhookAddonID); ok {
			minTier = a.MinTier
		}
	}
	return s.RequireAddon(w, r, WebhookAddonID, minTier)
}

// handleWebhookList serves GET /event/api/v1/subscriptions: the bare array
// of stored subscriptions (read gate, no feature gate — D1).
func (s *Server) handleWebhookList(w http.ResponseWriter, r *http.Request) {
	subs, err := s.deps.Webhooks.List(r.Context())
	if err != nil {
		s.writeWebhookStoreError(w, err)
		return
	}
	out := make([]*webhook.SubscriptionView, 0, len(subs))
	for _, sub := range subs {
		out = append(out, sub.View())
	}
	writeJSONBody(w, http.StatusOK, out)
}

// handleWebhookCreate serves POST /event/api/v1/subscriptions: 201 with
// the stored echo (webhook.md section 1 row 2).
func (s *Server) handleWebhookCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebhookAddon(w, r) {
		return
	}
	req, err := s.parseWebhookBody(w, r)
	if err != nil {
		return
	}
	sub, err := s.deps.Webhooks.Create(r.Context(), req, actorName(r))
	if err != nil {
		s.writeWebhookError(w, err)
		return
	}
	s.recordWebhookAudit(r, auditWebhookCreate, webhookAuditDetail(sub.Key, sub.Domain, sub.Enabled))
	writeJSONBody(w, http.StatusCreated, sub.View())
}

// handleWebhookGet serves GET /event/api/v1/subscriptions/{key}: the echo,
// or the endpoint table's exact 404 wording.
func (s *Server) handleWebhookGet(w http.ResponseWriter, r *http.Request, key string) {
	sub, err := s.deps.Webhooks.Get(r.Context(), key)
	if err != nil {
		s.writeWebhookError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, sub.View())
}

// handleWebhookUpdate serves PUT /event/api/v1/subscriptions/{key}: 204
// with no body (webhook.md section 1 row 4).
func (s *Server) handleWebhookUpdate(w http.ResponseWriter, r *http.Request, key string) {
	if !s.requireWebhookAddon(w, r) {
		return
	}
	req, err := s.parseWebhookBody(w, r)
	if err != nil {
		return
	}
	sub, err := s.deps.Webhooks.Update(r.Context(), key, req, actorName(r))
	if err != nil {
		s.writeWebhookError(w, err)
		return
	}
	s.recordWebhookAudit(r, auditWebhookUpdate, webhookAuditDetail(sub.Key, sub.Domain, sub.Enabled))
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhookDelete serves DELETE /event/api/v1/subscriptions/{key}: 204
// (deliveries cascade in the store).
func (s *Server) handleWebhookDelete(w http.ResponseWriter, r *http.Request, key string) {
	if !s.requireWebhookAddon(w, r) {
		return
	}
	if err := s.deps.Webhooks.Delete(r.Context(), key); err != nil {
		s.writeWebhookError(w, err)
		return
	}
	s.recordWebhookAudit(r, auditWebhookDelete, `{"key":"`+key+`"}`)
	w.WriteHeader(http.StatusNoContent)
}

// handleWebhookTest serves POST /event/api/v1/subscriptions/test: the
// synchronous one-shot send of a draft configuration — nothing persisted,
// one attempt, the outcome in the body (AC-1's "响应含 attempt 结果").
func (s *Server) handleWebhookTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireWebhookAddon(w, r) {
		return
	}
	req, err := s.parseWebhookBody(w, r)
	if err != nil {
		return
	}
	p := principalFrom(r.Context())
	outcome, err := s.deps.Webhooks.Test(r.Context(), req, webhook.Actor{
		ID:      actorName(r),
		IsToken: p != nil && p.TokenID > 0,
		Realm:   webhook.RealmFor(principalSource(p)),
	})
	if err != nil {
		s.writeWebhookError(w, err)
		return
	}
	s.recordWebhookAudit(r, auditWebhookTest, `{"key":"`+req.Key+`","status_code":`+
		strconv.Itoa(outcome.Attempt.StatusCode)+`,"ok":`+strconv.FormatBool(outcome.OK)+`}`)
	writeJSONBody(w, http.StatusOK, outcome)
}

// handleWebhookTroubleshooting serves GET /event/api/v1/troubleshooting
// (webhook.md section 1 row 7): subscription/target stream filters,
// start/end/count history windows.
func (s *Server) handleWebhookTroubleshooting(w http.ResponseWriter, r *http.Request) {
	q := webhook.TroubleshootQuery{
		Subscription: r.URL.Query().Get("subscription"),
		Target:       r.URL.Query().Get("target"),
	}
	q.Start, _ = strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	q.End, _ = strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
	q.Count, _ = strconv.Atoi(r.URL.Query().Get("count"))
	records, err := s.deps.Webhooks.Troubleshooting(r.Context(), q)
	if err != nil {
		s.writeWebhookStoreError(w, err)
		return
	}
	writeJSONBody(w, http.StatusOK, records)
}

// webhookAuditDetail renders the CRUD rows' detail.
func webhookAuditDetail(key, domain string, enabled bool) string {
	return `{"key":"` + key + `","domain":"` + domain + `","enabled":` + strconv.FormatBool(enabled) + `}`
}

// parseWebhookBody reads and validates one subscription body (the create/
// update/test shared parser). Validation failures render their own 400.
func (s *Server) parseWebhookBody(w http.ResponseWriter, r *http.Request) (*webhook.SubscriptionRequest, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "reading subscription body: "+err.Error())
		return nil, err
	}
	req, err := webhook.ParseSubscriptionRequest(body, s.webhookCipher())
	if err != nil {
		s.writeWebhookError(w, err)
		return nil, err
	}
	return req, nil
}

// webhookCipher adapts the assembly's sealing seam when the mounted plane
// exposes it (the concrete Bus does; test fakes need not).
func (s *Server) webhookCipher() webhook.Cipher {
	if c, ok := s.deps.Webhooks.(interface{ Sealer() webhook.Cipher }); ok {
		return c.Sealer()
	}
	return nil
}

// writeWebhookError maps the plane's sentinel set onto the endpoint table.
func (s *Server) writeWebhookError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhook.ErrNotFound):
		writeError(w, http.StatusNotFound, "Subscription not found")
	case errors.Is(err, webhook.ErrDuplicateKey):
		writeError(w, http.StatusBadRequest, "a subscription with this key already exists")
	case errors.Is(err, webhook.ErrNoCipher):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, webhook.ErrValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.writeWebhookStoreError(w, err)
	}
}

// writeWebhookStoreError is the infrastructure arm (busy → 503, else 500).
func (s *Server) writeWebhookStoreError(w http.ResponseWriter, err error) {
	if metadata.IsStoreBusy(err) {
		writeError(w, http.StatusServiceUnavailable, "storage is busy, retry shortly")
		return
	}
	s.log.Error("httpapi: webhook store failure", "error", err.Error())
	writeError(w, http.StatusInternalServerError, "webhook operation failed")
}

// recordWebhookAudit appends the plane's best-effort audit row (the detail
// carries the subscription key and domain; no target address — secrets and
// URLs stay out of the trail).
func (s *Server) recordWebhookAudit(r *http.Request, action, detail string) {
	s.audit.Record(r.Context(), audit.Event{
		Actor:  actorName(r),
		Action: action,
		Detail: detail,
	})
}

// emitWebhook is the httpapi-side seam callers (the property family) use:
// nil-plane no-op, so a stack without the bus keeps byte-identical
// behavior.
func (s *Server) emitWebhook(r *http.Request, ev webhook.Event) {
	if s.deps.Webhooks == nil {
		return
	}
	p := principalFrom(r.Context())
	ev.Actor = webhook.Actor{
		ID:      actorName(r),
		IsToken: p != nil && p.TokenID > 0,
		Realm:   webhook.RealmFor(principalSource(p)),
	}
	s.deps.Webhooks.Emit(r.Context(), ev)
}
