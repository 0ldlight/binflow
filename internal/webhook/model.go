package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Subscription is one stored webhook subscription (webhook.md section 2's
// wire shape, domain form). The wire echo re-derives from these fields;
// Criteria carries the raw JSON verbatim so a GET returns the filter
// byte-for-byte as validated (strict-parse) rather than a re-serialization
// that could drift key order or spelling.
type Subscription struct {
	ID          string
	Key         string
	ProjectKey  string
	Description string
	Enabled     bool
	Domain      string
	EventTypes  []string
	// Criteria is the stored filter JSON (already strict-validated).
	Criteria json.RawMessage
	Handler  Handler
	Debug    bool
	// Parsed is Criteria's in-process form (matching); nil when the row
	// predates a parse (never — writes validate first).
	Parsed *CriteriaFilter

	CreatedAt string
	CreatedBy string
	UpdatedAt string
	UpdatedBy string
}

// Handler is the subscription's single delivery handler (webhook.md 2.3's
// oneOf, discriminated by Type). SecretEnc/SecretsEnc hold enc:v1
// ciphertext (or "" / nil = absent); the plaintext never crosses this
// struct except at parse time on the way into the cipher.
type Handler struct {
	Type                string // "webhook" | "custom-webhook"
	URL                 string
	Proxy               string
	SecretEnc           string // predefined handler; enc:v1 or ""
	UseSecretForSigning bool
	CustomHTTPHeaders   []HeaderPair
	// Custom-webhook fields.
	Method      string // default POST
	Payload     string // Go template over the envelope
	HTTPHeaders []HeaderPair
	Secrets     map[string]string // name -> enc:v1 (never plaintext)
}

// HeaderPair is one {name, value} custom header.
type HeaderPair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HasSecret reports whether the predefined handler carries a secret.
func (h *Handler) HasSecret() bool { return h.SecretEnc != "" }

// Delivery is one outbox row (ADR-0041 decision 3): the envelope snapshot
// is final wire bytes — the dispatcher POSTs Payload as-is and signs the
// stored bytes.
type Delivery struct {
	ID             string
	SubscriptionID string
	EventType      string
	Payload        string
	Status         string
	Attempts       int64
	NextAttemptAt  string
	LastError      string
	LastStatusCode *int64
	CreatedAt      string
	DeliveredAt    *string
}

// Delivery statuses (the DDL's CHECK closed set).
const (
	StatusPending    = "pending"
	StatusDelivering = "delivering"
	StatusDelivered  = "delivered"
	StatusDead       = "dead"
)

// Sentinel errors; httpapi maps them onto the REST error table.
var (
	// ErrValidation marks a request-shape refusal (400): key pattern,
	// unknown domain/event type, strict criteria rejection, handler shape.
	ErrValidation = errors.New("webhook: invalid subscription")
	// ErrNotFound marks a missing subscription key (404, "Subscription not
	// found" — webhook.md section 1's exact wording).
	ErrNotFound = errors.New("webhook: subscription not found")
	// ErrDuplicateKey marks a create colliding on the key (400 family per
	// the endpoint's error table).
	ErrDuplicateKey = errors.New("webhook: subscription key already exists")
	// ErrNoCipher marks a secret-bearing write with no master key
	// configured (the enc:v1 POSTURE arm — ADR-0041 decision 5).
	ErrNoCipher = errors.New("webhook: secret provided but no master key is configured")
)

// validationError carries the 400 message.
type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
func (e *validationError) Unwrap() error { return ErrValidation }

func invalidf(format string, args ...any) error {
	return &validationError{msg: fmt.Sprintf(format, args...)}
}

// ---- wire DTOs (webhook.md section 2, OpenAPI verbatim field names) ----

// SubscriptionRequest is the POST/PUT body (WebhookSubscriptionCreate /
// WebhookSubscriptionUpdate — one shape: the update just ignores/validates
// key differently). Secret fields use pointers for the three-state
// semantics (nil = omit).
type SubscriptionRequest struct {
	Key         string          `json:"key"`
	ProjectKey  string          `json:"project_key"`
	Description string          `json:"description"`
	Enabled     *bool           `json:"enabled"`
	EventFilter *EventFilterDTO `json:"event_filter"`
	Handlers    []HandlerDTO    `json:"handlers"`
	Debug       *bool           `json:"debug"`
}

// Cipher is the seal seam the request parser seals plaintext secrets
// through (remote.Cipher satisfies it; nil refuses secret-bearing writes).
type Cipher interface {
	Encrypt(secret string) (string, error)
}

// EventFilterDTO is event_filter.
type EventFilterDTO struct {
	Domain     string          `json:"domain"`
	EventTypes []string        `json:"event_types"`
	Criteria   json.RawMessage `json:"criteria"`
}

// HandlerDTO is the oneOf handler. The custom-webhook arm's secrets are a
// full-list replace on update (webhook.md 2.4).
type HandlerDTO struct {
	HandlerType         string           `json:"handler_type"`
	URL                 string           `json:"url"`
	Proxy               string           `json:"proxy"`
	Secret              *string          `json:"secret"`
	UseSecretForSigning bool             `json:"use_secret_for_signing"`
	CustomHTTPHeaders   []HeaderPair     `json:"custom_http_headers"`
	Method              string           `json:"method"`
	Payload             string           `json:"payload"`
	HTTPHeaders         []HeaderPair     `json:"http_headers"`
	Secrets             []NamedSecretDTO `json:"secrets"`
}

// NamedSecretDTO is one custom-webhook named secret; Value nil = omit
// (preserve on update), "" = wipe, plaintext = add/rotate.
type NamedSecretDTO struct {
	Name  string  `json:"name"`
	Value *string `json:"value"`
}

// secretSentinel is the masked echo of a stored secret (ADR-0041 decision
// 5: the ciphertext never echoes; a PUT returning the sentinel verbatim
// preserves the stored value).
const secretSentinel = "********"

// SubscriptionView is the wire echo (WebhookSubscription): field set and
// order per webhook.md 2.4 — key, project_key, description, enabled,
// event_filter, handlers, debug.
type SubscriptionView struct {
	Key         string         `json:"key"`
	ProjectKey  string         `json:"project_key"`
	Description string         `json:"description"`
	Enabled     bool           `json:"enabled"`
	EventFilter EventFilterDTO `json:"event_filter"`
	Handlers    []HandlerView  `json:"handlers"`
	Debug       bool           `json:"debug"`
}

// HandlerView is the handler echo: the predefined arm's secret is the
// sentinel when set (never the ciphertext); the custom arm's secrets carry
// names only ("values are never returned").
type HandlerView struct {
	HandlerType         string           `json:"handler_type"`
	URL                 string           `json:"url"`
	Proxy               string           `json:"proxy"`
	Secret              string           `json:"secret,omitempty"`
	UseSecretForSigning bool             `json:"use_secret_for_signing"`
	CustomHTTPHeaders   []HeaderPair     `json:"custom_http_headers"`
	Method              string           `json:"method,omitempty"`
	Payload             string           `json:"payload,omitempty"`
	HTTPHeaders         []HeaderPair     `json:"http_headers"`
	Secrets             []NamedSecretRef `json:"secrets"`
}

// NamedSecretRef is the name-only echo of a custom-webhook secret.
type NamedSecretRef struct {
	Name string `json:"name"`
}

// View projects the stored subscription onto the wire echo.
func (s *Subscription) View() *SubscriptionView {
	h := HandlerView{
		HandlerType:         s.Handler.Type,
		URL:                 s.Handler.URL,
		Proxy:               s.Handler.Proxy,
		UseSecretForSigning: s.Handler.UseSecretForSigning,
		CustomHTTPHeaders:   s.Handler.CustomHTTPHeaders,
		HTTPHeaders:         s.Handler.HTTPHeaders,
	}
	if h.CustomHTTPHeaders == nil {
		h.CustomHTTPHeaders = []HeaderPair{}
	}
	if h.HTTPHeaders == nil {
		h.HTTPHeaders = []HeaderPair{}
	}
	if s.Handler.HasSecret() {
		h.Secret = secretSentinel
	}
	if s.Handler.Type == "custom-webhook" {
		h.Method = s.Handler.Method
		h.Payload = s.Handler.Payload
		refs := make([]NamedSecretRef, 0, len(s.Handler.Secrets))
		for name := range s.Handler.Secrets {
			refs = append(refs, NamedSecretRef{Name: name})
		}
		sortSecretRefs(refs)
		h.Secrets = refs
	}
	crit := s.Criteria
	if len(crit) == 0 {
		crit = json.RawMessage("{}")
	}
	types := append([]string(nil), s.EventTypes...)
	if types == nil {
		types = []string{}
	}
	return &SubscriptionView{
		Key:         s.Key,
		ProjectKey:  s.ProjectKey,
		Description: s.Description,
		Enabled:     s.Enabled,
		EventFilter: EventFilterDTO{Domain: s.Domain, EventTypes: types, Criteria: crit},
		Handlers:    []HandlerView{h},
		Debug:       s.Debug,
	}
}

func sortSecretRefs(refs []NamedSecretRef) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].Name < refs[j-1].Name; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// ---- shape constants (webhook.md 2.1/2.3 and 5.2's size limits) ----

var keyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_\-]+$`)

var secretNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

const (
	maxKeyLength       = 500
	maxURLLength       = 2048
	maxHeadersCount    = 50
	maxHeadersBytes    = 5120
	maxSecretsCount    = 50
	maxSecretsBytes    = 5120
	maxPayloadTemplate = 51200
)

var allowedMethods = map[string]bool{
	"POST": true, "PUT": true, "GET": true, "PATCH": true, "DELETE": true,
}

// ParseSubscriptionRequest decodes and validates one request body (the
// shared create/update/test parser — test sends a full subscription body,
// webhook.md section 1 row 6). cipher seals plaintext secrets; nil refuses
// secret-bearing requests with ErrNoCipher.
func ParseSubscriptionRequest(body []byte, cipher Cipher) (*SubscriptionRequest, error) {
	var req SubscriptionRequest
	if len(body) == 0 {
		return nil, invalidf("request body is required")
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// Unknown top-level fields are tolerated per the official loose
		// object posture ONLY when they are not recognizable; the OpenAPI
		// carries additionalProperties:false on the create shape, so a
		// strict decode is the honest reading. Criteria inside
		// event_filter stays raw (its own strict pass runs below).
		return nil, invalidf("malformed subscription body: %v", err)
	}
	if err := req.Validate(cipher); err != nil {
		return nil, err
	}
	return &req, nil
}

// Validate runs the closed-shape rules. It is also the update/test
// pre-flight (update additionally checks key/project_key immutability at
// the store layer, which knows the stored row).
func (req *SubscriptionRequest) Validate(cipher Cipher) error {
	if req.Key == "" {
		return invalidf("key is required")
	}
	if len(req.Key) > maxKeyLength {
		return invalidf("key exceeds %d characters", maxKeyLength)
	}
	if !keyPattern.MatchString(req.Key) {
		return invalidf("key must match ^[A-Za-z][A-Za-z0-9_-]+$ (letter first, then letters, digits, underscore or hyphen)")
	}
	if req.Enabled == nil {
		// Schema default: created disabled (webhook.md 2.1).
		v := false
		req.Enabled = &v
	}
	if req.Debug == nil {
		v := false
		req.Debug = &v
	}
	if req.EventFilter == nil {
		return invalidf("event_filter is required")
	}
	if err := validateEventFilter(req.EventFilter); err != nil {
		return err
	}
	if len(req.Handlers) != 1 {
		return invalidf("handlers must carry exactly one entry (minItems: 1, maxItems: 1)")
	}
	return validateHandler(&req.Handlers[0], cipher)
}

func validateEventFilter(f *EventFilterDTO) error {
	if !ValidDomain(f.Domain) {
		return invalidf("unknown event domain %q (legal: %s)", f.Domain, strings.Join(Domains(), ", "))
	}
	if len(f.EventTypes) == 0 {
		return invalidf("event_filter.event_types must carry at least one entry")
	}
	for _, t := range f.EventTypes {
		if _, ok := Lookup(f.Domain, t); !ok {
			return invalidf("event type %q is not registered for domain %q", t, f.Domain)
		}
	}
	if len(f.Criteria) == 0 || string(f.Criteria) == "null" {
		f.Criteria = json.RawMessage("{}")
		return nil
	}
	parsed, err := ParseCriteria(f.Criteria)
	if err != nil {
		return err
	}
	// Re-serialize the parsed form so the STORED criteria is the
	// canonical strict spelling (echo and matching share one shape).
	canonical, merr := json.Marshal(parsed)
	if merr != nil {
		return invalidf("criteria canonicalization: %v", merr)
	}
	f.Criteria = json.RawMessage(canonical)
	return nil
}

func validateHandler(h *HandlerDTO, cipher Cipher) error {
	switch h.HandlerType {
	case "webhook":
	case "custom-webhook":
	default:
		return invalidf("handler_type must be \"webhook\" or \"custom-webhook\" (got %q)", h.HandlerType)
	}
	if h.URL == "" {
		return invalidf("handlers[0].url is required")
	}
	if err := ValidateTargetURL(h.URL); err != nil {
		return err
	}
	if h.Secret != nil && *h.Secret != "" && h.HandlerType == "webhook" {
		if cipher == nil {
			return fmt.Errorf("%w (set %s to seal webhook secrets)", ErrNoCipher, "BINFLOW_REMOTE_CREDENTIALS_KEY")
		}
		if _, err := cipher.Encrypt(*h.Secret); err != nil {
			return fmt.Errorf("sealing handler secret: %w", err)
		}
	}
	if h.HandlerType == "custom-webhook" {
		if h.Method == "" {
			h.Method = "POST"
		}
		if !allowedMethods[h.Method] {
			return invalidf("handlers[0].method must be one of POST, PUT, GET, PATCH, DELETE (got %q)", h.Method)
		}
		if len(h.Payload) > maxPayloadTemplate {
			return invalidf("handlers[0].payload exceeds %d bytes", maxPayloadTemplate)
		}
		if len(h.Secrets) > maxSecretsCount {
			return invalidf("handlers[0].secrets exceeds %d entries", maxSecretsCount)
		}
		bytes := 0
		seen := map[string]bool{}
		for _, s := range h.Secrets {
			if !secretNamePattern.MatchString(s.Name) {
				return invalidf("secret name %q must match ^[a-zA-Z_][a-zA-Z0-9_]*$", s.Name)
			}
			if seen[s.Name] {
				return invalidf("duplicate secret name %q", s.Name)
			}
			seen[s.Name] = true
			if s.Value != nil {
				bytes += len(*s.Value)
			}
		}
		if bytes > maxSecretsBytes {
			return invalidf("handlers[0].secrets exceeds %d bytes in total", maxSecretsBytes)
		}
		if len(h.Secrets) > 0 && cipher == nil {
			for _, s := range h.Secrets {
				if s.Value != nil && *s.Value != "" {
					return fmt.Errorf("%w (set %s to seal webhook secrets)", ErrNoCipher, "BINFLOW_REMOTE_CREDENTIALS_KEY")
				}
			}
		}
	}
	headers := h.CustomHTTPHeaders
	if h.HandlerType == "custom-webhook" {
		headers = h.HTTPHeaders
	}
	if len(headers) > maxHeadersCount {
		return invalidf("handlers[0] carries more than %d headers", maxHeadersCount)
	}
	hb := 0
	for _, hp := range headers {
		if hp.Name == "" {
			return invalidf("header name is empty")
		}
		hb += len(hp.Name) + len(hp.Value)
	}
	if hb > maxHeadersBytes {
		return invalidf("handlers[0] headers exceed %d bytes in total", maxHeadersBytes)
	}
	return nil
}

// ValidateTargetURL is the static target check (ADR-0041 decision 6):
// http/https only (the Guard re-asserts at connect time), host required,
// userinfo and fragments refused, length bounded. The network-screening
// half (private ranges, DNS rebinding) belongs to the Guard.
func ValidateTargetURL(raw string) error {
	if len(raw) > maxURLLength {
		return invalidf("handlers[0].url exceeds %d characters", maxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return invalidf("handlers[0].url does not parse: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return invalidf("handlers[0].url scheme must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return invalidf("handlers[0].url carries no host")
	}
	if u.User != nil {
		return invalidf("handlers[0].url must not carry userinfo")
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return invalidf("handlers[0].url must not carry a fragment")
	}
	return nil
}

// sealRequest encrypts the plaintext secret arms of a parsed request into
// the handler's stored form. Wipe strings ("") are normalized to absent.
func (req *SubscriptionRequest) seal(cipher Cipher) (Handler, error) {
	dto := &req.Handlers[0]
	h := Handler{
		Type:                dto.HandlerType,
		URL:                 dto.URL,
		Proxy:               dto.Proxy,
		UseSecretForSigning: dto.UseSecretForSigning,
		CustomHTTPHeaders:   append([]HeaderPair(nil), dto.CustomHTTPHeaders...),
		Method:              dto.Method,
		Payload:             dto.Payload,
		HTTPHeaders:         append([]HeaderPair(nil), dto.HTTPHeaders...),
		Secrets:             map[string]string{},
	}
	if dto.Secret != nil && *dto.Secret != "" && dto.HandlerType == "webhook" {
		if cipher == nil {
			return h, ErrNoCipher
		}
		enc, err := cipher.Encrypt(*dto.Secret)
		if err != nil {
			return h, fmt.Errorf("sealing handler secret: %w", err)
		}
		h.SecretEnc = enc
	}
	for _, s := range dto.Secrets {
		if s.Value == nil {
			continue // preserve marker — resolved against the stored row
		}
		if *s.Value == "" {
			continue // wipe — absence is the stored form
		}
		if cipher == nil {
			return h, ErrNoCipher
		}
		enc, err := cipher.Encrypt(*s.Value)
		if err != nil {
			return h, fmt.Errorf("sealing secret %q: %w", s.Name, err)
		}
		h.Secrets[s.Name] = enc
	}
	return h, nil
}

// mergeSecrets resolves the custom-webhook full-list-replace semantics
// against the stored row (webhook.md 2.4): a name the update omits is
// REMOVED; a name whose Value was nil keeps the stored ciphertext; a
// plaintext rotates; "" wipes to absent.
func (req *SubscriptionRequest) mergeSecrets(stored map[string]string, cipher Cipher) (map[string]string, error) {
	out := map[string]string{}
	for _, s := range req.Handlers[0].Secrets {
		if s.Value == nil {
			if enc, ok := stored[s.Name]; ok {
				out[s.Name] = enc
			}
			continue
		}
		if *s.Value == "" {
			continue // wiped: name survives with no value
		}
		if cipher == nil {
			return nil, ErrNoCipher
		}
		enc, err := cipher.Encrypt(*s.Value)
		if err != nil {
			return nil, fmt.Errorf("sealing secret %q: %w", s.Name, err)
		}
		out[s.Name] = enc
	}
	return out, nil
}
