package webhook

// The outbox row-level face (M17 T-496, FR-159.2 / LC-109): the query,
// pagination and replay carriers the BinFlow-native REST plane drives.
// Artifactory has no public counterpart for this face (rest-compat-matrix
// D09 row 8 — a C-layer BinFlow enhancement), so the shapes follow
// BinFlow's own v1 conventions: the audit read's filter+keyset-cursor
// page envelope and the outbox engine's existing status vocabulary.
//
// Everything here reads the 018 webhook_deliveries rows the engine
// already owns — no new table, no new status, no behavior change to the
// dispatcher. Replay is the one write, and it is the store's existing
// ReplayDelivery transition plus the bus's wake hook (the dispatcher
// injects its signal at NewDispatcher), so a replayed dead row is simply
// a pending row the running engine claims on the next wake or poll.

import (
	"context"
	"time"
)

// OutboxFilter is the row-level read's parameter set. The zero filter
// pages the whole outbox newest first.
type OutboxFilter struct {
	// SubscriptionKey narrows to one subscription's rows (the wire key,
	// resolved through the join). "" = every subscription. An unknown key
	// is a filter, not an address: it answers an empty page, never 404.
	SubscriptionKey string
	// Status narrows to one of the four closed-set statuses. "" = all.
	Status string
	// EventType narrows on the row's bare event_type column. "" = all.
	// Names collide across domains ("deleted" is artifact, docker and
	// build); the row itself is domain-less, so this is a coarse
	// operational filter, not a closed-set claim.
	EventType string
	// Limit caps the page (1..OutboxLimitMax; <= 0 = OutboxLimitDefault).
	Limit int
	// Cursor is the opaque keyset position the previous page's NextCursor
	// issued. "" = first page; anything the store cannot parse answers
	// metadata.ErrInvalidCursor (the audit read's posture).
	Cursor string
}

// Outbox page bounds (the audit face's 1..1000 posture, halved: outbox
// rows are wider than audit events — last_error alone can carry 4KB).
const (
	// OutboxLimitDefault is the page size an unparameterized read takes.
	OutboxLimitDefault = 50
	// OutboxLimitMax is the largest page a caller may ask for.
	OutboxLimitMax = 500
)

// OutboxRow is one outbox row joined with its subscription's wire key —
// the delivery target an operator can read, where the row itself carries
// only the subscription's UUID.
type OutboxRow struct {
	Delivery
	// SubscriptionKey is the joined webhook_subscriptions.key; "" when the
	// join finds no row (defensive: the FK cascade keeps orphans out, but
	// a read must never 500 on a mid-cascade snapshot).
	SubscriptionKey string
}

// OutboxPage is one filtered page, newest first, plus the following page's
// keyset cursor — "" on the last page so a client never guesses between
// "absent" and "empty" (the audit envelope's rule).
type OutboxPage struct {
	Rows       []*OutboxRow
	NextCursor string
}

// DeliveryView is the outbox row's wire echo (LC-109): every
// observability column plus the resolved subscription key. The payload is
// deliberately absent — those are the delivered bytes, already restated
// by the troubleshooting face when an operator needs them.
type DeliveryView struct {
	ID              string  `json:"id"`
	SubscriptionID  string  `json:"subscription_id"`
	SubscriptionKey string  `json:"subscription_key"`
	EventType       string  `json:"event_type"`
	Status          string  `json:"status"`
	Attempts        int64   `json:"attempts"`
	NextAttemptAt   string  `json:"next_attempt_at"`
	LastError       string  `json:"last_error"`
	LastStatusCode  *int64  `json:"last_status_code"`
	CreatedAt       string  `json:"created_at"`
	DeliveredAt     *string `json:"delivered_at"`
}

// View projects the joined row onto the wire echo.
func (r *OutboxRow) View() *DeliveryView {
	return &DeliveryView{
		ID:              r.ID,
		SubscriptionID:  r.SubscriptionID,
		SubscriptionKey: r.SubscriptionKey,
		EventType:       r.EventType,
		Status:          r.Status,
		Attempts:        r.Attempts,
		NextAttemptAt:   r.NextAttemptAt,
		LastError:       r.LastError,
		LastStatusCode:  r.LastStatusCode,
		CreatedAt:       r.CreatedAt,
		DeliveredAt:     r.DeliveredAt,
	}
}

// Outbox reads one filtered page of delivery rows.
func (b *Bus) Outbox(ctx context.Context, f OutboxFilter) (*OutboxPage, error) {
	return b.store.QueryOutbox(ctx, f)
}

// Replay resets one dead row to pending (attempts zeroed, due now) and
// wakes the engine — the ADR decision 4 replay face on the bus, behind
// the REST/console surface the assembly mounts. The returned row is the
// post-flip snapshot (status pending, attempts 0). notify is the
// dispatcher's wake hook; with no engine running it is nil and the row
// still resets — the next engine's poll claims it.
func (b *Bus) Replay(ctx context.Context, deliveryID string) (*OutboxRow, error) {
	if err := b.store.ReplayDelivery(ctx, deliveryID, b.now().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	row, err := b.store.GetDelivery(ctx, deliveryID)
	if err != nil {
		// The reset landed but the echo read failed: the caller still
		// learns the state through the row-level query face.
		return nil, err
	}
	out := &OutboxRow{Delivery: *row}
	if sub, serr := b.store.GetSubscriptionByID(ctx, row.SubscriptionID); serr == nil {
		out.SubscriptionKey = sub.Key
	}
	return out, nil
}
