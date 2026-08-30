package webhook

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// Event is the domain seam's single argument (ADR-0041 decision 1: the
// repository domain's mutation tails call Emit with exactly this and never
// learn about subscriptions). Domain+Type must be a wired closed-set pair;
// the data fields are the superset of the three wired domains' payloads
// (webhook.md section 3) — the envelope builder picks per domain, so no
// field is ever invented beyond the spec's tables.
type Event struct {
	Domain string
	Type   string

	Repo   string
	Path   string
	Name   string // derived from Path when empty
	Sha256 string
	Size   int64

	// moved/copied extras (webhook.md 3.1): matched on the SOURCE repo.
	SourceRepoPath string
	TargetRepoPath string

	// docker extras (webhook.md 3.3).
	ImageName string
	Tag       string
	ImageType string // "oci" | "docker"

	// artifact_property extras (webhook.md 3.2).
	PropertyKey    string
	PropertyValues []string

	// Actor is the envelope's userContext (webhook.md section 4).
	Actor Actor
}

// Actor is the userContext triple (official field names).
type Actor struct {
	ID      string `json:"id"`
	IsToken bool   `json:"isToken"`
	Realm   string `json:"realm"`
}

// RealmFor maps BinFlow's auth provider source onto the envelope's realm
// ("local" is the official "internal"; oidc/ldap pass through).
func RealmFor(source string) string {
	switch source {
	case "", "local":
		return "internal"
	default:
		return source
	}
}

// envelope is the outbound wire body (webhook.md section 4, the 7-field
// documented superset — BinFlow's registered stance on the 5-vs-7 field
// discrepancy the spec itself carries).
type envelope struct {
	Domain          string         `json:"domain"`
	EventType       string         `json:"event_type"`
	Data            map[string]any `json:"data"`
	SubscriptionKey string         `json:"subscription_key"`
	JPDOrigin       string         `json:"jpd_origin"`
	Source          string         `json:"source"`
	UserContext     Actor          `json:"userContext"`
}

// Bus is the unified-event bus (ADR-0041's single Emit seam) plus the
// subscription application logic the REST plane drives. It is safe for
// concurrent use.
type Bus struct {
	store  Store
	gate   func(context.Context) bool
	cipher *remote.Cipher
	repos  metadata.RepoStore
	guard  *remote.Guard
	origin string
	source string
	log    *slog.Logger
	now    func() time.Time

	failures atomic.Int64 // enqueue failures (WARN-visible; the T-364 metric family reads it)

	ringMu sync.Mutex
	ring   []TroubleshootingRecord
}

// BusOptions assembles the Bus. Gate is the entitlement verdict injected
// by the assembly (ADR-0041 decision 8: internal/webhook never imports
// license) — nil fails CLOSED (a bus with no verdict enqueues nothing);
// AllowPrivateTarget mirrors `webhook.allow_private_target` (default
// false — the deliberate asymmetry against replication's default-allow).
type BusOptions struct {
	Store              Store
	Gate               func(context.Context) bool
	Cipher             *remote.Cipher
	Repos              metadata.RepoStore
	Origin             string
	NodeID             string
	AllowPrivateTarget bool
	Logger             *slog.Logger
	Now                func() time.Time
}

// NewBus assembles the bus. The Guard is built here (scheme assertion +
// IP screening + connect-time pinning over the same machinery the remote
// engine uses; redirection is never followed — the sender refuses hops).
func NewBus(opts BusOptions) (*Bus, error) {
	if opts.Store == nil {
		return nil, errors.New("webhook: bus requires a store")
	}
	node := opts.NodeID
	if node == "" {
		node = newULID(time.Now())
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Bus{
		store:  opts.Store,
		gate:   opts.Gate,
		cipher: opts.Cipher,
		repos:  opts.Repos,
		guard: remote.NewGuard(remote.GuardOptions{
			RepoKey:              "webhook",
			AllowPrivateUpstream: opts.AllowPrivateTarget,
			Logger:               log,
		}),
		origin: opts.Origin,
		source: "binflow/binflow@" + node,
		log:    log,
		now:    now,
	}, nil
}

// EnqueueFailures reports the enqueue-failure count (the loss counter of
// ADR-0041 decision 10 — enqueue failures never block the main path, but
// they must be visible).
func (b *Bus) EnqueueFailures() int64 { return b.failures.Load() }

// Sealer exposes the sealing seam for the REST parse path (httpapi's
// adapter probe — nil means no master key, secret-bearing writes refuse).
func (b *Bus) Sealer() Cipher {
	if b.cipher == nil {
		return nil
	}
	return b.cipher
}

// PendingCount exposes the outbox watermark.
func (b *Bus) PendingCount(ctx context.Context) (int64, error) { return b.store.CountPending(ctx) }

// ---- the Emit seam ----

// Emit is the domain seam (ADR-0041 decision 1): entitlement gate ->
// closed-set assertion -> subscription matching (event pair + criteria) ->
// per-subscription envelope instantiation -> ONE batched outbox insert.
// It runs synchronously on the mutation tail (the small-transaction
// fan-out of decision 4) and NEVER fails the caller: the recover shield
// plus the WARN/failure-counter path keep the main path's zero-change
// contract (FR-114 AC6) even when the bus itself is buggy.
func (b *Bus) Emit(ctx context.Context, ev Event) {
	if b == nil {
		return
	}
	defer b.shield(ctx, ev)
	if b.gate == nil || !b.gate(ctx) {
		return // entitlement DENIED (or unwired): the weaving plane is off
	}
	et, ok := Lookup(ev.Domain, ev.Type)
	if !ok {
		b.log.WarnContext(ctx, "webhook: emit of unregistered event dropped",
			"domain", ev.Domain, "event_type", ev.Type)
		return
	}
	if et.Source != SourceWired {
		b.log.WarnContext(ctx, "webhook: emit of dormant event dropped (no trigger source)",
			"event", et.String())
		return
	}
	subs, err := b.store.MatchSubscriptions(ctx, ev.Domain, ev.Type)
	if err != nil {
		b.enqueueFailed(ctx, ev, err)
		return
	}
	now := b.now()
	var rows []*Delivery
	for _, sub := range subs {
		if !b.criteriaAdmit(ctx, sub, ev) {
			continue
		}
		payload, err := b.envelopeFor(sub, ev)
		if err != nil {
			b.enqueueFailed(ctx, ev, err)
			continue
		}
		id, err := newUUID()
		if err != nil {
			b.enqueueFailed(ctx, ev, err)
			continue
		}
		rows = append(rows, &Delivery{
			ID:             id,
			SubscriptionID: sub.ID,
			EventType:      ev.Type,
			Payload:        string(payload),
			Status:         StatusPending,
			NextAttemptAt:  now.Format(time.RFC3339Nano),
			CreatedAt:      now.Format(time.RFC3339Nano),
		})
	}
	if err := b.store.EnqueueDeliveries(ctx, rows); err != nil {
		b.enqueueFailed(ctx, ev, err)
	}
}

// shield keeps a bus panic off the request path (the notifyReplicator
// posture — the main path's contract is zero change, FR-114 AC6).
func (b *Bus) shield(ctx context.Context, ev Event) {
	if v := recover(); v != nil {
		b.failures.Add(1)
		b.log.WarnContext(ctx, "webhook: emit panicked (event dropped, main path unaffected)",
			"domain", ev.Domain, "event_type", ev.Type, "panic", v)
	}
}

// enqueueFailed is decision 10's visible-loss path: WARN plus the
// counter, never an error into the caller.
func (b *Bus) enqueueFailed(ctx context.Context, ev Event, err error) {
	b.failures.Add(1)
	b.log.WarnContext(ctx, "webhook: enqueue failed (event lost for its subscribers)",
		"domain", ev.Domain, "event_type", ev.Type, "repo", ev.Repo, "path", ev.Path, "error", err.Error())
}

// criteriaAdmit runs the criteria filter: repository selection (exact
// repoKeys or the anyLocal/anyRemote class families — the class lookup is
// lazy, only when a class arm could match) then the path patterns.
func (b *Bus) criteriaAdmit(ctx context.Context, sub *Subscription, ev Event) bool {
	f := sub.Parsed
	if f == nil {
		return false // unparseable stored criteria: admit nothing, loudly
	}
	if !f.MatchesRepo(ev.Repo, b.repoClass(ctx, ev.Repo, f)) {
		return false
	}
	return f.MatchesPath(ev.Path)
}

// repoClass resolves the repository row's class once per emit when a
// class arm (anyLocal/anyRemote) is in play; exact repoKeys matches and
// pattern-only filters never touch the repositories table.
func (b *Bus) repoClass(ctx context.Context, repoKey string, f *CriteriaFilter) string {
	if !f.AnyLocal && !f.AnyRemote {
		return ""
	}
	for _, k := range f.RepoKeys {
		if k == repoKey {
			return "" // exact hit: no class question
		}
	}
	if b.repos == nil {
		return ""
	}
	row, err := b.repos.Get(ctx, repoKey)
	if err != nil || row == nil {
		return ""
	}
	return row.Type
}

// envelopeFor instantiates the per-subscription wire body: the snapshot
// the outbox stores and the HMAC signs (decision 3 — subscription edits
// never rewrite an already-enqueued event).
func (b *Bus) envelopeFor(sub *Subscription, ev Event) ([]byte, error) {
	env := envelope{
		Domain:          ev.Domain,
		EventType:       ev.Type,
		Data:            dataFor(ev),
		SubscriptionKey: sub.Key,
		JPDOrigin:       b.origin,
		Source:          b.source,
		UserContext:     ev.Actor,
	}
	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("webhook: envelope for %s: %w", sub.Key, err)
	}
	return body, nil
}

// dataFor builds the domain payload (webhook.md section 3's field tables,
// verbatim field names; no timestamp on the artifact family — "不要发明
// 字段").
func dataFor(ev Event) map[string]any {
	name := ev.Name
	if name == "" {
		name = path.Base(strings.TrimSuffix(ev.Path, "/"))
	}
	d := map[string]any{
		"repo_key": ev.Repo,
		"path":     ev.Path,
		"name":     name,
		"sha256":   ev.Sha256,
		"size":     ev.Size,
	}
	switch ev.Domain {
	case DomainArtifact:
		if ev.Type == TypeArtifactMoved || ev.Type == TypeArtifactCopied {
			d["source_repo_path"] = ev.SourceRepoPath
			d["target_repo_path"] = ev.TargetRepoPath
		}
	case DomainArtifactProperty:
		d["property_key"] = ev.PropertyKey
		values := ev.PropertyValues
		if values == nil {
			values = []string{}
		}
		d["property_values"] = values
	case DomainDocker:
		d["image_name"] = ev.ImageName
		d["tag"] = ev.Tag
		d["platforms"] = []any{} // BinFlow stores no platform rows; honest empty set
		d["image_type"] = ev.ImageType
	}
	return d
}

// ---- identifiers ----

// newUUID mints a v4 UUID (the storage engine's stdlib pattern).
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("webhook: uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ulidAlphabet is Crockford base32 (the ULID spec's).
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newULID mints a 26-char ULID (48-bit millisecond timestamp + 80 random
// bits, Crockford base32) — the event-id form the official troubleshooting
// records carry (webhook.md section 4's ULID note). Stdlib-only.
func newULID(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms & 0xff)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		// Degenerate randomness still yields a distinct-enough id; the
		// record's uniqueness is an observability nicety, not correctness.
		for i := 6; i < 16; i++ {
			b[i] = byte(i*37 + int(ms&0xff))
		}
	}
	out := make([]byte, 0, 26)
	var bits uint64
	var nbits uint
	for _, by := range b {
		bits |= uint64(by) << nbits
		nbits += 8
		for nbits >= 5 {
			out = append(out, ulidAlphabet[bits&31])
			bits >>= 5
			nbits -= 5
		}
	}
	for nbits > 0 && len(out) < 26 {
		out = append(out, ulidAlphabet[bits&31])
		bits >>= 5
		if nbits < 5 {
			nbits = 0
		} else {
			nbits -= 5
		}
	}
	for len(out) < 26 {
		out = append(out, ulidAlphabet[0])
	}
	return string(out[:26])
}
