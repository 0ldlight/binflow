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
	rows, err := s.db.QueryContext(ctx, `SELECT id, subscription_id, event_type, payload, status,
		attempts, next_attempt_at, last_error, last_status_code, created_at, delivered_at
		FROM webhook_deliveries WHERE subscription_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		subscriptionID, limit)
	if err != nil {
		return nil, wrapStoreErr("list deliveries", subscriptionID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []*Delivery
	for rows.Next() {
		d := &Delivery{}
		var lastStatus sql.NullInt64
		var delivered sql.NullString
		if err := rows.Scan(&d.ID, &d.SubscriptionID, &d.EventType, &d.Payload, &d.Status,
			&d.Attempts, &d.NextAttemptAt, &d.LastError, &lastStatus, &d.CreatedAt, &delivered); err != nil {
			return nil, wrapStoreErr("list deliveries scan", subscriptionID, err)
		}
		if lastStatus.Valid {
			v := lastStatus.Int64
			d.LastStatusCode = &v
		}
		if delivered.Valid {
			v := delivered.String
			d.DeliveredAt = &v
		}
		out = append(out, d)
	}
	return out, wrapStoreErr("list deliveries rows", subscriptionID, rows.Err())
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
