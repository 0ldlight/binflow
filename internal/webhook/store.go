package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Store is the webhook plane's persistence contract (the replication
// package's Store precedent: the interface lives with the domain, the
// SQLite implementation speaks SQL directly over a second handle to the
// metadata database — migrations, PRAGMAs and the pool belong to the
// metadata layer).
type Store interface {
	// CreateSubscription persists a validated subscription; a key
	// collision answers ErrDuplicateKey.
	CreateSubscription(ctx context.Context, s *Subscription) error
	// GetSubscription resolves one key; a miss answers ErrNotFound.
	GetSubscription(ctx context.Context, key string) (*Subscription, error)
	// ListSubscriptions returns every subscription ordered by key.
	ListSubscriptions(ctx context.Context) ([]*Subscription, error)
	// UpdateSubscription replaces a stored row's mutable fields (key and
	// project_key are immutable, enforced by the caller); a miss answers
	// ErrNotFound.
	UpdateSubscription(ctx context.Context, s *Subscription) error
	// DeleteSubscription drops the row; in-flight deliveries cascade
	// (stopping delivery — ADR-0041 decision 3). A miss answers ErrNotFound.
	DeleteSubscription(ctx context.Context, key string) error
	// MatchSubscriptions returns the ENABLED subscriptions subscribed to
	// (domain, eventType) — criteria filtering is the caller's (it needs
	// the event's repo/path).
	MatchSubscriptions(ctx context.Context, domain, eventType string) ([]*Subscription, error)
	// EnqueueDeliveries batch-inserts pending rows in ONE transaction (the
	// fan-out's atomic unit — ADR-0041 decision 4's "同步小事务").
	EnqueueDeliveries(ctx context.Context, rows []*Delivery) error
	// CountPending is the outbox watermark (tests and the T-364 metrics
	// family's queue_depth).
	CountPending(ctx context.Context) (int64, error)
	// ListDeliveries returns the outbox rows of one subscription, newest
	// first (the console's "recent deliveries" window and AC assertions).
	ListDeliveries(ctx context.Context, subscriptionID string, limit int) ([]*Delivery, error)

	// ---- the dispatcher seams (T-364, ADR-0041 decision 4) ----
	// The dispatcher owns every status transition past T-362's pending
	// inserts; each method below is one transition, spelled so a caller
	// cannot express an illegal one.

	// GetSubscriptionByID resolves the delivery target by row id (the
	// outbox carries subscription_id, not key); a miss answers ErrNotFound.
	GetSubscriptionByID(ctx context.Context, id string) (*Subscription, error)
	// GetDelivery reads one outbox row; a miss answers ErrNotFound.
	GetDelivery(ctx context.Context, id string) (*Delivery, error)
	// ClaimDueDelivery atomically moves ONE due pending row to delivering
	// (attempts+1 — the interrupted-attempt-stays-counted posture) and
	// returns it. nil, nil when nothing is due: the worker then parks on
	// its poll timer or the wake signal. The claim is a compare-and-set
	// that re-asserts BOTH queue predicates (status pending AND the row
	// still due at `now`), so concurrent workers can never share a row
	// and a row whose retry moved into the future mid-scan cannot be
	// claimed early.
	ClaimDueDelivery(ctx context.Context, now string) (*Delivery, error)
	// SweepDelivering reverts every delivering row to pending at startup
	// (attempts preserved — kill -9 mid-attempt recovery, the ADR's
	// decision 4 sweep) and answers how many rows it recovered.
	SweepDelivering(ctx context.Context) (int64, error)
	// MarkDelivered lands the terminal success: status delivered,
	// delivered_at stamped, last_error cleared.
	MarkDelivered(ctx context.Context, id string, attempts int64, deliveredAt string, statusCode int64) error
	// ScheduleRetry returns a failed row to pending with next_attempt_at
	// set (the fixed retryWait interval — webhook.md 5.2 has no backoff
	// curve) and the attempt's observability columns recorded.
	ScheduleRetry(ctx context.Context, id string, attempts int64, nextAttemptAt, lastError string, statusCode *int64) error
	// MarkDead lands the terminal abandonment (4xx/3xx answer, or the
	// retry budget exhausted): status dead — queryable, replayable, and
	// only ever removed by replay or the subscription's cascade.
	MarkDead(ctx context.Context, id string, attempts int64, lastError string, statusCode *int64) error
	// ReplayDelivery resets one dead row to pending (attempts zeroed,
	// next_attempt_at = now) — the ADR decision 4 replay face. A missing
	// row answers ErrNotFound; a row that is not dead answers ErrNotDead.
	ReplayDelivery(ctx context.Context, id, nextAttemptAt string) error
}

// SQLiteStore is the SQLite implementation over the 018 tables.
type SQLiteStore struct{ db *sql.DB }

// NewSQLiteStore wraps a migrated metadata database handle (the
// openReplicationDB posture: the caller keeps ownership of closing).
func NewSQLiteStore(db *sql.DB) *SQLiteStore { return &SQLiteStore{db: db} }

var _ Store = (*SQLiteStore)(nil)

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func wrapStoreErr(label, key string, err error) error {
	if err == nil {
		return nil
	}
	if metadata.IsStoreBusy(err) {
		return fmt.Errorf("webhook: %s (%s): %w: %w", label, key, metadata.ErrStoreBusy, err)
	}
	return fmt.Errorf("webhook: %s (%s): %w", label, key, err)
}

const subColumns = `id, key, project_key, description, enabled, domain, criteria, handler_type,
	url, proxy, secret_enc, use_secret_for_signing, custom_http_headers, method, payload_tpl,
	http_headers, secrets_enc, debug, created_at, created_by, updated_at, updated_by`

// scanSubscription reads one subscription row (*sql.Row and *sql.Rows
// both satisfy the narrow Scan seam).
func scanSubscription(row interface{ Scan(...any) error }) (*Subscription, error) {
	s := &Subscription{}
	var customHeaders, httpHeaders, secretsEnc, criteria string
	var useSign int
	err := row.Scan(&s.ID, &s.Key, &s.ProjectKey, &s.Description, &s.Enabled, &s.Domain, &criteria,
		&s.Handler.Type, &s.Handler.URL, &s.Handler.Proxy, &s.Handler.SecretEnc, &useSign,
		&customHeaders, &s.Handler.Method, &s.Handler.Payload, &httpHeaders, &secretsEnc,
		&s.Debug, &s.CreatedAt, &s.CreatedBy, &s.UpdatedAt, &s.UpdatedBy)
	if err != nil {
		return nil, err
	}
	s.Handler.UseSecretForSigning = useSign != 0
	s.Criteria = json.RawMessage(criteria)
	_ = json.Unmarshal([]byte(customHeaders), &s.Handler.CustomHTTPHeaders)
	_ = json.Unmarshal([]byte(httpHeaders), &s.Handler.HTTPHeaders)
	s.Handler.Secrets = map[string]string{}
	_ = json.Unmarshal([]byte(secretsEnc), &s.Handler.Secrets)
	if p, err := ParseCriteria(s.Criteria); err == nil {
		s.Parsed = p
	}
	return s, nil
}

// loadEventTypes reads the child rows of one subscription.
func (s *SQLiteStore) loadEventTypes(ctx context.Context, id string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_type FROM webhook_subscription_events WHERE subscription_id = ? ORDER BY event_type`, id)
	if err != nil {
		return nil, wrapStoreErr("load event types", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, wrapStoreErr("load event types scan", id, err)
		}
		out = append(out, t)
	}
	return out, wrapStoreErr("load event types rows", id, rows.Err())
}

// CreateSubscription implements Store.
func (s *SQLiteStore) CreateSubscription(ctx context.Context, sub *Subscription) error {
	customHeaders := marshalJSON(sub.Handler.CustomHTTPHeaders)
	httpHeaders := marshalJSON(sub.Handler.HTTPHeaders)
	secrets := marshalJSON(sub.Handler.Secrets)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapStoreErr("create begin", sub.Key, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO webhook_subscriptions
		(id, key, project_key, description, enabled, domain, criteria, handler_type,
		 url, proxy, secret_enc, use_secret_for_signing, custom_http_headers, method, payload_tpl,
		 http_headers, secrets_enc, debug, created_at, created_by, updated_at, updated_by)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, sub.Key, sub.ProjectKey, sub.Description, boolToInt(sub.Enabled), sub.Domain,
		string(sub.Criteria), sub.Handler.Type, sub.Handler.URL, sub.Handler.Proxy,
		sub.Handler.SecretEnc, boolToInt(sub.Handler.UseSecretForSigning), customHeaders,
		sub.Handler.Method, sub.Handler.Payload, httpHeaders, secrets, boolToInt(sub.Debug),
		sub.CreatedAt, sub.CreatedBy, sub.UpdatedAt, sub.UpdatedBy); err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("webhook: create %s: %w", sub.Key, ErrDuplicateKey)
		}
		return wrapStoreErr("create", sub.Key, err)
	}
	for _, t := range sub.EventTypes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO webhook_subscription_events (subscription_id, event_type) VALUES (?, ?)`,
			sub.ID, t); err != nil {
			return wrapStoreErr("create event row "+t, sub.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapStoreErr("create commit", sub.Key, err)
	}
	return nil
}

// GetSubscription implements Store.
func (s *SQLiteStore) GetSubscription(ctx context.Context, key string) (*Subscription, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+subColumns+` FROM webhook_subscriptions WHERE key = ?`, key)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("webhook: get %s: %w", key, ErrNotFound)
	}
	if err != nil {
		return nil, wrapStoreErr("get", key, err)
	}
	if sub.EventTypes, err = s.loadEventTypes(ctx, sub.ID); err != nil {
		return nil, err
	}
	return sub, nil
}

// ListSubscriptions implements Store.
func (s *SQLiteStore) ListSubscriptions(ctx context.Context) ([]*Subscription, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+subColumns+` FROM webhook_subscriptions ORDER BY key`)
	if err != nil {
		return nil, wrapStoreErr("list", "", err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, wrapStoreErr("list scan", "", err)
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapStoreErr("list rows", "", err)
	}
	for _, sub := range out {
		if sub.EventTypes, err = s.loadEventTypes(ctx, sub.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpdateSubscription implements Store.
func (s *SQLiteStore) UpdateSubscription(ctx context.Context, sub *Subscription) error {
	customHeaders := marshalJSON(sub.Handler.CustomHTTPHeaders)
	httpHeaders := marshalJSON(sub.Handler.HTTPHeaders)
	secrets := marshalJSON(sub.Handler.Secrets)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapStoreErr("update begin", sub.Key, err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE webhook_subscriptions SET
		description = ?, enabled = ?, criteria = ?, handler_type = ?, url = ?, proxy = ?,
		secret_enc = ?, use_secret_for_signing = ?, custom_http_headers = ?, method = ?,
		payload_tpl = ?, http_headers = ?, secrets_enc = ?, debug = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`,
		sub.Description, boolToInt(sub.Enabled), string(sub.Criteria), sub.Handler.Type,
		sub.Handler.URL, sub.Handler.Proxy, sub.Handler.SecretEnc,
		boolToInt(sub.Handler.UseSecretForSigning), customHeaders, sub.Handler.Method,
		sub.Handler.Payload, httpHeaders, secrets, boolToInt(sub.Debug),
		sub.UpdatedAt, sub.UpdatedBy, sub.ID)
	if err != nil {
		return wrapStoreErr("update", sub.Key, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapStoreErr("update rows", sub.Key, err)
	} else if n == 0 {
		return fmt.Errorf("webhook: update %s: %w", sub.Key, ErrNotFound)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM webhook_subscription_events WHERE subscription_id = ?`, sub.ID); err != nil {
		return wrapStoreErr("update clear events", sub.Key, err)
	}
	for _, t := range sub.EventTypes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO webhook_subscription_events (subscription_id, event_type) VALUES (?, ?)`,
			sub.ID, t); err != nil {
			return wrapStoreErr("update event row "+t, sub.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapStoreErr("update commit", sub.Key, err)
	}
	return nil
}

// DeleteSubscription implements Store.
func (s *SQLiteStore) DeleteSubscription(ctx context.Context, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webhook_subscriptions WHERE key = ?`, key)
	if err != nil {
		return wrapStoreErr("delete", key, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return wrapStoreErr("delete rows", key, err)
	} else if n == 0 {
		return fmt.Errorf("webhook: delete %s: %w", key, ErrNotFound)
	}
	return nil
}

// MatchSubscriptions implements Store: enabled rows joined on the
// (domain, event_type) pair — event names collide across domains, so the
// domain predicate rides the parent row.
func (s *SQLiteStore) MatchSubscriptions(ctx context.Context, domain, eventType string) ([]*Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subColumns+`
		FROM webhook_subscriptions s
		JOIN webhook_subscription_events e ON e.subscription_id = s.id
		WHERE s.enabled = 1 AND s.domain = ? AND e.event_type = ?
		ORDER BY s.key`, domain, eventType)
	if err != nil {
		return nil, wrapStoreErr("match", domain+"/"+eventType, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, wrapStoreErr("match scan", domain+"/"+eventType, err)
		}
		if sub.EventTypes, err = s.loadEventTypes(ctx, sub.ID); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, wrapStoreErr("match rows", domain+"/"+eventType, rows.Err())
}

// EnqueueDeliveries implements Store: one transaction, every row.
func (s *SQLiteStore) EnqueueDeliveries(ctx context.Context, rows []*Delivery) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return wrapStoreErr("enqueue begin", "", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, d := range rows {
		if _, err := tx.ExecContext(ctx, `INSERT INTO webhook_deliveries
			(id, subscription_id, event_type, payload, status, attempts, next_attempt_at,
			 last_error, last_status_code, created_at, delivered_at)
			VALUES (?,?,?,?,?,?,?,'',NULL,?,NULL)`,
			d.ID, d.SubscriptionID, d.EventType, d.Payload, StatusPending, 0,
			d.NextAttemptAt, d.CreatedAt); err != nil {
			return wrapStoreErr("enqueue row", d.SubscriptionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return wrapStoreErr("enqueue commit", "", err)
	}
	return nil
}

// CountPending implements Store.
func (s *SQLiteStore) CountPending(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM webhook_deliveries WHERE status = ?`, StatusPending).Scan(&n); err != nil {
		return 0, wrapStoreErr("count pending", "", err)
	}
	return n, nil
}

// ListDeliveries implements Store.
func (s *SQLiteStore) ListDeliveries(ctx context.Context, subscriptionID string, limit int) ([]*Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+deliveryColumns+`
		FROM webhook_deliveries WHERE subscription_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		subscriptionID, limit)
	if err != nil {
		return nil, wrapStoreErr("list deliveries", subscriptionID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, wrapStoreErr("list deliveries scan", subscriptionID, err)
		}
		out = append(out, d)
	}
	return out, wrapStoreErr("list deliveries rows", subscriptionID, rows.Err())
}

// ---- the dispatcher seams ----

// claimCandidates bounds the candidate scan one claim examines: a small
// window past the queue index, so a worker that loses every race to peers
// simply reports "nothing due" and the next poll retries.
const claimCandidates = 16

// GetSubscriptionByID implements Store.
func (s *SQLiteStore) GetSubscriptionByID(ctx context.Context, id string) (*Subscription, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+subColumns+` FROM webhook_subscriptions WHERE id = ?`, id)
	sub, err := scanSubscription(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("webhook: get subscription %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, wrapStoreErr("get subscription", id, err)
	}
	if sub.EventTypes, err = s.loadEventTypes(ctx, sub.ID); err != nil {
		return nil, err
	}
	return sub, nil
}

// deliveryColumns is the outbox row projection every delivery read shares.
const deliveryColumns = `id, subscription_id, event_type, payload, status, attempts,
	next_attempt_at, last_error, last_status_code, created_at, delivered_at`

// scanDelivery reads one outbox row off the narrow Scan seam.
func scanDelivery(row interface{ Scan(...any) error }) (*Delivery, error) {
	d := &Delivery{}
	var lastStatus sql.NullInt64
	var delivered sql.NullString
	if err := row.Scan(&d.ID, &d.SubscriptionID, &d.EventType, &d.Payload, &d.Status,
		&d.Attempts, &d.NextAttemptAt, &d.LastError, &lastStatus, &d.CreatedAt, &delivered); err != nil {
		return nil, err
	}
	if lastStatus.Valid {
		v := lastStatus.Int64
		d.LastStatusCode = &v
	}
	if delivered.Valid {
		v := delivered.String
		d.DeliveredAt = &v
	}
	return d, nil
}

// GetDelivery implements Store.
func (s *SQLiteStore) GetDelivery(ctx context.Context, id string) (*Delivery, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+deliveryColumns+` FROM webhook_deliveries WHERE id = ?`, id)
	d, err := scanDelivery(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("webhook: get delivery %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, wrapStoreErr("get delivery", id, err)
	}
	return d, nil
}

// ClaimDueDelivery implements Store: scan a due-candidate window over the
// queue index, then compare-and-set each candidate pending->delivering
// (attempts+1 in the same statement, so the attempt count can never drift
// from the claim). The CAS re-asserts BOTH queue predicates — status AND
// the due stamp: between a peer's scan and its CAS, a row can complete a
// whole failed attempt and come back pending with a FUTURE next_attempt_at
// (the fixed retry interval), and a claim that only re-checked status
// would fire that retry immediately, collapsing the interval to the
// scan-to-CAS gap. Re-asserting `next_attempt_at <= now` inside the UPDATE
// makes the due-ness decision atomic with the claim; a CAS that loses
// either predicate leaves RowsAffected zero and this loop moves on.
func (s *SQLiteStore) ClaimDueDelivery(ctx context.Context, now string) (*Delivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM webhook_deliveries
		 WHERE status = ? AND next_attempt_at <= ?
		 ORDER BY next_attempt_at, created_at, id LIMIT ?`,
		StatusPending, now, claimCandidates)
	if err != nil {
		return nil, wrapStoreErr("claim scan", "", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, wrapStoreErr("claim scan rows", "", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, wrapStoreErr("claim scan iter", "", err)
	}
	_ = rows.Close()
	for _, id := range ids {
		res, err := s.db.ExecContext(ctx,
			`UPDATE webhook_deliveries SET status = ?, attempts = attempts + 1
			 WHERE id = ? AND status = ? AND next_attempt_at <= ?`,
			StatusDelivering, id, StatusPending, now)
		if err != nil {
			return nil, wrapStoreErr("claim", id, err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return nil, wrapStoreErr("claim rows", id, err)
		} else if n == 1 {
			return s.GetDelivery(ctx, id)
		}
		// Lost the race to a peer worker (or the row's due stamp moved
		// into the future mid-scan): try the next candidate.
	}
	return nil, nil
}

// SweepDelivering implements Store: the startup recovery pass. next_attempt_at
// is left untouched — every delivering row got there by being due, so the
// reverted row is immediately claimable again.
func (s *SQLiteStore) SweepDelivering(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ? WHERE status = ?`, StatusPending, StatusDelivering)
	if err != nil {
		return 0, wrapStoreErr("sweep delivering", "", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, wrapStoreErr("sweep delivering rows", "", err)
	}
	return n, nil
}

// MarkDelivered implements Store.
func (s *SQLiteStore) MarkDelivered(ctx context.Context, id string, attempts int64, deliveredAt string, statusCode int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = ?, delivered_at = ?, last_error = '', last_status_code = ?
		 WHERE id = ?`, StatusDelivered, attempts, deliveredAt, statusCode, id)
	if err != nil {
		return wrapStoreErr("mark delivered", id, err)
	}
	return nil
}

// ScheduleRetry implements Store.
func (s *SQLiteStore) ScheduleRetry(ctx context.Context, id string, attempts int64, nextAttemptAt, lastError string, statusCode *int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = ?, next_attempt_at = ?, last_error = ?, last_status_code = ?
		 WHERE id = ?`, StatusPending, attempts, nextAttemptAt, lastError, nullInt64(statusCode), id)
	if err != nil {
		return wrapStoreErr("schedule retry", id, err)
	}
	return nil
}

// MarkDead implements Store.
func (s *SQLiteStore) MarkDead(ctx context.Context, id string, attempts int64, lastError string, statusCode *int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = ?, last_error = ?, last_status_code = ?
		 WHERE id = ?`, StatusDead, attempts, lastError, nullInt64(statusCode), id)
	if err != nil {
		return wrapStoreErr("mark dead", id, err)
	}
	return nil
}

// ReplayDelivery implements Store.
func (s *SQLiteStore) ReplayDelivery(ctx context.Context, id, nextAttemptAt string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webhook_deliveries SET status = ?, attempts = 0, next_attempt_at = ?, last_error = ''
		 WHERE id = ? AND status = ?`, StatusPending, nextAttemptAt, id, StatusDead)
	if err != nil {
		return wrapStoreErr("replay", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return wrapStoreErr("replay rows", id, err)
	}
	if n == 1 {
		return nil
	}
	if _, gerr := s.GetDelivery(ctx, id); gerr != nil {
		return gerr // the row does not exist at all
	}
	return fmt.Errorf("webhook: replay %s: %w", id, ErrNotDead)
}

// nullInt64 renders the optional status-code column value.
func nullInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// boolToInt maps the dialect's boolean convention.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalJSON renders a column value ("[]"/"{}" for nil).
func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		if _, ok := v.(map[string]string); ok {
			return "{}"
		}
		return "[]"
	}
	return string(b)
}
