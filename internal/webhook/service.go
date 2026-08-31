package webhook

// The subscription application logic and the troubleshooting face the
// REST plane drives (T-362's seven endpoints, webhook.md section 1). The
// Bus is the application service: validation ran at parse, this layer
// adds identity, immutability and the secret three-state semantics, then
// persists through Store.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// attemptResult is one send attempt's observable outcome (the test
// endpoint's response body and the troubleshooting record's halves).
type attemptResult struct {
	StatusCode      int    `json:"status_code"`
	ElapsedMillis   int64  `json:"elapsed_millis"`
	Error           string `json:"error,omitempty"`
	responseBody    string
	responseHeaders http.Header
	requestHeaders  http.Header
}

// TestOutcome is POST /subscriptions/test's answer: the attempt always
// runs, so the HTTP status is 200 either way (the endpoint's own error
// table has no target-failure code — webhook.md section 1 row 6); the
// outcome carries the verdict (AC-1: "响应含 attempt 结果——状态码/耗时").
type TestOutcome struct {
	Message string        `json:"message"`
	OK      bool          `json:"ok"`
	Attempt attemptResult `json:"attempt"`
}

// TroubleshootingRecord is the排障 record schema (webhook.md section 7:
// the REST response and the log line are the same shape). The M13 store
// is a bounded in-process ring (streamMaxLen's spirit); the log/redis
// persistence forms belong to the delivery-engine ticket.
type TroubleshootingRecord struct {
	Timestamp     int64          `json:"timestamp"` // UNIX ms, attempt start
	ElapsedMillis int64          `json:"elapsed_millis"`
	Errors        []string       `json:"errors"`
	Request       recordRequest  `json:"request"`
	Response      recordResponse `json:"response"`
	Event         recordEvent    `json:"event"`
}

type recordRequest struct {
	Method           string      `json:"method"`
	URL              string      `json:"url"`
	Headers          http.Header `json:"headers"`
	Payload          string      `json:"payload"`
	RetriesAttempted int         `json:"retries_attempted"`
}

type recordResponse struct {
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    string      `json:"body"`
}

type recordEvent struct {
	ID              string         `json:"id"`
	SubscriptionKey string         `json:"subscription_key"`
	Domain          string         `json:"domain"`
	EventType       string         `json:"event_type"`
	Data            map[string]any `json:"data"`
	Source          string         `json:"source"`
}

// TroubleshootQuery is GET /troubleshooting's filter (webhook.md section
// 1 row 7): subscription/target filter the live stream; start/end/count
// page the history (end/count only meaningful with start; start=0 reads
// from the oldest record).
type TroubleshootQuery struct {
	Subscription string
	Target       string
	Start        int64 // UNIX ms; 0 = from the oldest
	End          int64 // UNIX ms; 0 = unbounded
	Count        int   // 0 = default 100
}

// The troubleshooting ring's bounds (webhook.md section 7's Redis shape,
// BinFlow's in-process equivalent — the T-364 anchor): streamMaxLen 10000
// records kept by a janitor that runs every cleanupIntervalMillis 30000
// and trims the oldest overflow. Between janitor runs the ring may grow
// past the target (the official stream does too); ringHardCap is the
// memory-safety ceiling the append path enforces itself so a runaway
// burst between ticks cannot grow the ring without bound.
const (
	ringCapacity = 10000
	ringHardCap  = 2 * ringCapacity
)

// Create validates-and-persists one subscription (the POST arm). The
// request must already be parsed (ParseSubscriptionRequest); this method
// seals the secrets and stamps identity.
func (b *Bus) Create(ctx context.Context, req *SubscriptionRequest, actor string) (*Subscription, error) {
	if req == nil || req.EventFilter == nil || len(req.Handlers) != 1 {
		return nil, fmt.Errorf("webhook: create: %w", ErrValidation)
	}
	handler, err := req.seal(b.cipher)
	if err != nil {
		return nil, err
	}
	now := b.now().Format(time.RFC3339Nano)
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("webhook: create id: %w", err)
	}
	sub := &Subscription{
		ID:          id,
		Key:         req.Key,
		ProjectKey:  req.ProjectKey,
		Description: req.Description,
		Enabled:     *req.Enabled,
		Domain:      req.EventFilter.Domain,
		EventTypes:  append([]string(nil), req.EventFilter.EventTypes...),
		Criteria:    req.EventFilter.Criteria,
		Handler:     handler,
		Debug:       *req.Debug,
		CreatedAt:   now,
		CreatedBy:   actor,
		UpdatedAt:   now,
		UpdatedBy:   actor,
	}
	parsed, err := ParseCriteria(sub.Criteria)
	if err != nil {
		return nil, err
	}
	sub.Parsed = parsed
	if err := b.store.CreateSubscription(ctx, sub); err != nil {
		return nil, err
	}
	return sub, nil
}

// Get is the single-subscription read arm.
func (b *Bus) Get(ctx context.Context, key string) (*Subscription, error) {
	return b.store.GetSubscription(ctx, key)
}

// List is the collection read arm.
func (b *Bus) List(ctx context.Context) ([]*Subscription, error) {
	return b.store.ListSubscriptions(ctx)
}

// Update applies the PUT semantics (webhook.md 2.4): key is
// path-identified and immutable; project_key must match the stored value
// exactly; enabled/event_filter/handlers are required; the predefined
// handler's secret is three-state (omit-or-sentinel = preserve, plaintext
// = rotate, "" = wipe); the custom handler's secrets are a full-list
// replace.
func (b *Bus) Update(ctx context.Context, key string, req *SubscriptionRequest, actor string) (*Subscription, error) {
	if req == nil || req.EventFilter == nil || len(req.Handlers) != 1 {
		return nil, fmt.Errorf("webhook: update: %w", ErrValidation)
	}
	stored, err := b.store.GetSubscription(ctx, key)
	if err != nil {
		return nil, err
	}
	if req.Key != "" && req.Key != stored.Key {
		return nil, invalidf("key cannot be changed (path identifies the subscription)")
	}
	if req.ProjectKey != stored.ProjectKey {
		return nil, invalidf("project_key must match the stored value and cannot be changed")
	}
	dto := &req.Handlers[0]
	handler := Handler{
		Type:                dto.HandlerType,
		URL:                 dto.URL,
		Proxy:               dto.Proxy,
		UseSecretForSigning: dto.UseSecretForSigning,
		CustomHTTPHeaders:   append([]HeaderPair(nil), dto.CustomHTTPHeaders...),
		Method:              dto.Method,
		Payload:             dto.Payload,
		HTTPHeaders:         append([]HeaderPair(nil), dto.HTTPHeaders...),
	}
	// Predefined secret, three-state.
	switch {
	case dto.Secret == nil, dto.Secret != nil && *dto.Secret == secretSentinel:
		handler.SecretEnc = stored.Handler.SecretEnc
	case *dto.Secret == "":
		handler.SecretEnc = "" // wipe
	default:
		if b.cipher == nil {
			return nil, ErrNoCipher
		}
		enc, err := b.cipher.Encrypt(*dto.Secret)
		if err != nil {
			return nil, fmt.Errorf("webhook: sealing handler secret: %w", err)
		}
		handler.SecretEnc = enc
	}
	// Custom secrets, full-list replace with per-name preserve.
	if dto.HandlerType == "custom-webhook" {
		merged, err := req.mergeSecrets(stored.Handler.Secrets, b.cipher)
		if err != nil {
			return nil, err
		}
		handler.Secrets = merged
	} else {
		handler.Secrets = map[string]string{}
	}
	parsed, err := ParseCriteria(req.EventFilter.Criteria)
	if err != nil {
		return nil, err
	}
	stored.Description = req.Description
	stored.Enabled = *req.Enabled
	stored.Domain = req.EventFilter.Domain
	stored.EventTypes = append([]string(nil), req.EventFilter.EventTypes...)
	stored.Criteria = req.EventFilter.Criteria
	stored.Handler = handler
	stored.Debug = *req.Debug
	stored.UpdatedAt = b.now().Format(time.RFC3339Nano)
	stored.UpdatedBy = actor
	stored.Parsed = parsed
	if err := b.store.UpdateSubscription(ctx, stored); err != nil {
		return nil, err
	}
	return stored, nil
}

// Delete is the DELETE arm (deliveries cascade in the store).
func (b *Bus) Delete(ctx context.Context, key string) error {
	return b.store.DeleteSubscription(ctx, key)
}

// Deliveries lists a subscription's recent outbox rows (newest first).
func (b *Bus) Deliveries(ctx context.Context, key string, limit int) ([]*Delivery, error) {
	sub, err := b.store.GetSubscription(ctx, key)
	if err != nil {
		return nil, err
	}
	return b.store.ListDeliveries(ctx, sub.ID, limit)
}

// Test drives the synchronous one-shot send (webhook.md section 1 row 6:
// a FULL subscription body, not a key reference — the draft configuration
// fires as-is, nothing persisted, no outbox row, no retry). The synthetic
// event's shape is the domain's documented payload with marker values.
func (b *Bus) Test(ctx context.Context, req *SubscriptionRequest, actor Actor) (*TestOutcome, error) {
	if req == nil || req.EventFilter == nil || len(req.EventFilter.EventTypes) == 0 || len(req.Handlers) != 1 {
		return nil, fmt.Errorf("webhook: test: %w", ErrValidation)
	}
	handler, err := req.seal(b.cipher)
	if err != nil {
		return nil, err
	}
	ev := syntheticEvent(req.EventFilter.Domain, req.EventFilter.EventTypes[0], req.Key, actor)
	payload, err := b.envelopeFor(&Subscription{Key: req.Key}, ev)
	if err != nil {
		return nil, err
	}
	secret := ""
	if handler.HasSecret() && b.cipher != nil {
		if plain, _, derr := b.cipher.Decrypt(handler.SecretEnc); derr == nil {
			secret = plain
		}
	}
	attempt := b.sendAttempt(ctx, &handler, payload, secret)
	b.recordAttempt(&handler, payload, attempt, 0, *req.Debug)
	out := &TestOutcome{Attempt: *attempt, OK: attempt.StatusCode >= 200 && attempt.StatusCode < 300}
	if out.OK {
		out.Message = "Test successful"
	} else if attempt.Error != "" {
		out.Message = "Test attempt failed: " + attempt.Error
	} else {
		out.Message = fmt.Sprintf("Test attempt failed: receiver answered %d", attempt.StatusCode)
	}
	return out, nil
}

// syntheticEvent builds the test endpoint's marker event (honest sample
// values, never a real artifact reference; the empty sha256 is the real
// sha256-of-nothing digest).
const testDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func syntheticEvent(domain, eventType, key string, actor Actor) Event {
	ev := Event{
		Domain: domain,
		Type:   eventType,
		Actor:  actor,
	}
	switch domain {
	case DomainArtifact, DomainArtifactProperty, DomainDocker:
		ev.Repo = "example-repo"
		ev.Path = "example/path/artifact.bin"
		ev.Name = "artifact.bin"
		ev.Sha256 = testDigest
		ev.Size = 1024
	}
	switch domain {
	case DomainArtifact:
		if eventType == TypeArtifactMoved || eventType == TypeArtifactCopied {
			ev.SourceRepoPath = "example-repo/example/path/artifact.bin"
			ev.TargetRepoPath = "example-target/example/path/artifact.bin"
		}
	case DomainArtifactProperty:
		ev.PropertyKey = "example-key"
		ev.PropertyValues = []string{"example-value"}
	case DomainDocker:
		ev.ImageName = "example-image"
		ev.Tag = "latest"
		ev.ImageType = "oci"
	}
	_ = key
	return ev
}

// recordAttempt appends one record under the §7 retention rules: failures
// always, successes only when the subscription runs debug=true. The record
// is derived from the STORED payload (the envelope snapshot is the wire
// truth — domain/event_type/data/subscription_key/source all come off the
// bytes that were sent), and retries carries the §7 retries_attempted
// observable (attempts beyond the first; zero on the test path's single
// shot).
func (b *Bus) recordAttempt(h *Handler, payload []byte, attempt *attemptResult, retries int, debug bool) {
	if attempt.Error == "" && !debug {
		return // success without debug: not recorded
	}
	var env envelope
	_ = json.Unmarshal(payload, &env) // the snapshot was marshaled from this shape
	rec := TroubleshootingRecord{
		Timestamp:     b.now().UnixMilli(),
		ElapsedMillis: attempt.ElapsedMillis,
		Request: recordRequest{
			Method:           methodOf(h),
			URL:              h.URL,
			Headers:          redactedRequestHeaders(attempt.requestHeaders),
			Payload:          string(payload),
			RetriesAttempted: retries,
		},
		Response: recordResponse{
			Status:  attempt.StatusCode,
			Headers: attempt.responseHeaders,
			Body:    attempt.responseBody,
		},
		Event: recordEvent{
			ID:              newULID(b.now()),
			SubscriptionKey: env.SubscriptionKey,
			Domain:          env.Domain,
			EventType:       env.EventType,
			Data:            env.Data,
			Source:          env.Source,
		},
	}
	if rec.Event.Source == "" {
		rec.Event.Source = b.source
	}
	if attempt.Error != "" {
		rec.Errors = []string{attempt.Error}
	}
	b.ringMu.Lock()
	defer b.ringMu.Unlock()
	b.ring = append(b.ring, rec)
	if len(b.ring) > ringHardCap {
		b.ring = append([]TroubleshootingRecord(nil), b.ring[len(b.ring)-ringHardCap:]...)
	}
}

// redactedRequestHeaders copies the sent request headers with the auth
// header's value masked: the troubleshooting record must be safe to serve
// over REST (webhook.md section 7) while the secret never leaves the
// process (NFR-S66 — not in logs, not in audit, not in records).
func redactedRequestHeaders(h http.Header) http.Header {
	out := http.Header{}
	for name, vals := range h {
		if strings.EqualFold(name, EventAuthHeader) {
			out[name] = []string{secretSentinel}
			continue
		}
		out[name] = append([]string(nil), vals...)
	}
	return out
}

// trimTroubleshooting is the record ring's janitor (the dispatcher runs it
// on the 30s cleanup interval — webhook.md section 7's
// cleanupIntervalMillis equivalent): trim the oldest overflow down to the
// streamMaxLen-shaped target.
func (b *Bus) trimTroubleshooting() {
	b.ringMu.Lock()
	defer b.ringMu.Unlock()
	if len(b.ring) > ringCapacity {
		b.ring = append([]TroubleshootingRecord(nil), b.ring[len(b.ring)-ringCapacity:]...)
	}
}

func methodOf(h *Handler) string {
	if h.Type == "custom-webhook" && h.Method != "" {
		return h.Method
	}
	return http.MethodPost
}

// Troubleshooting reads the record ring under the query filters.
func (b *Bus) Troubleshooting(ctx context.Context, q TroubleshootQuery) ([]TroubleshootingRecord, error) {
	_ = ctx
	count := q.Count
	if count <= 0 {
		count = 100
	}
	b.ringMu.Lock()
	defer b.ringMu.Unlock()
	var out []TroubleshootingRecord
	for i := len(b.ring) - 1; i >= 0 && len(out) < count; i-- {
		rec := b.ring[i]
		if q.Start != 0 || q.End != 0 {
			if q.Start != 0 && rec.Timestamp < q.Start {
				continue
			}
			if q.End != 0 && rec.Timestamp > q.End {
				continue
			}
		}
		if q.Subscription != "" && rec.Event.SubscriptionKey != q.Subscription {
			continue
		}
		if q.Target != "" && !strings.Contains(rec.Request.URL, q.Target) {
			continue
		}
		out = append(out, rec)
	}
	if out == nil {
		out = []TroubleshootingRecord{}
	}
	return out, nil
}
