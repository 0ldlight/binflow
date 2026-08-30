-- 018_webhook.sql (sqlite dialect) — the unified-event webhook plane's two
-- tables plus the subscription/event-type child table (M13 T-362, FR-114 /
-- ADR-0041 decisions 3 and 7).
--
-- Calibration against the ADR's draft DDL (the ADR's own "锚点回填位"
-- clause: webhook.md outranks the draft once T-358 lands): the official
-- subscription wire (webhook.md section 2) is ONE subscription carrying a
-- multi-event-type filter (`event_filter.event_types[]` inside ONE domain),
-- not the single-event-type-per-row shape the draft sketched. The ADR
-- anticipated exactly this ("若 T-358 证实多事件型订阅 → 子表 additive 迁移，
-- 机制条款不变"); 018 lands new, so the child table ships in the primary
-- schema instead of as a later additive step. Every mechanism clause
-- (outbox snapshot payload, enc:v1 secret column, status closed set,
-- (status, next_attempt_at) queue predicate) is the ADR's verbatim.
--
-- webhook_subscriptions: the subscription rows. `key` is the wire
-- identifier (webhook.md 2.1 pattern ^[A-Za-z][A-Za-z0-9_\-]+$, <= 500);
-- `criteria` stores the filter JSON verbatim (strict unknown-key
-- validation happens at the parse layer, matching is in-process over the
-- parsed form); `secret_enc` is enc:v1 AES-256-GCM ciphertext or '' (no
-- secret — no signature header); `secrets_enc` is the custom-webhook
-- handler's named-secret map {name: enc:v1}, names only on echo.
--
-- webhook_subscription_events: the (subscription, event_type) pairs.
-- event_type names are NOT unique across domains ("deleted" lives in
-- artifact, docker and build; "delete_failed" in distribution and
-- destination), so matching joins on the pair (subscription.domain is
-- carried by the parent row) — the (subscription_id, event_type) primary
-- key stays unique within one subscription because a subscription's types
-- all come from its single domain.
--
-- Idempotent: CREATE TABLE IF NOT EXISTS throughout, matching the
-- post-011 convention (012/015/016) the t212 rewind test replays under.
--
-- webhook_deliveries: the outbox. `payload` is the fan-out-time envelope
-- snapshot (the final wire bytes for THIS subscription: subscription_key
-- baked in, HMAC computed over these stored bytes at delivery time — a
-- later subscription edit never rewrites an already-enqueued event);
-- `next_attempt_at` is the queue's time predicate
-- (status='pending' AND next_attempt_at <= now); ON DELETE CASCADE drops
-- in-flight rows with their subscription (stopping delivery, no orphans —
-- ADR decision 3). The dispatcher (T-364, ADR decision 4) owns every
-- status transition; T-362 only inserts pending rows.

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
	id                      TEXT PRIMARY KEY,            -- uuid
	key                     TEXT NOT NULL UNIQUE,        -- wire identifier (webhook.md 2.1)
	project_key             TEXT NOT NULL DEFAULT '',
	description             TEXT NOT NULL DEFAULT '',
	enabled                 INTEGER NOT NULL DEFAULT 0,  -- schema default: created disabled
	domain                  TEXT NOT NULL,               -- 13-domain closed set (webhook.md 3)
	criteria                TEXT NOT NULL DEFAULT '{}',  -- filter JSON, strict-parse
	handler_type            TEXT NOT NULL,               -- 'webhook' | 'custom-webhook'
	url                     TEXT NOT NULL,
	proxy                   TEXT NOT NULL DEFAULT '',    -- proxy KEY, never a URL
	secret_enc              TEXT NOT NULL DEFAULT '',    -- enc:v1 or '' (predefined handler)
	use_secret_for_signing  INTEGER NOT NULL DEFAULT 0,
	custom_http_headers     TEXT NOT NULL DEFAULT '[]',  -- [{name,value}]
	method                  TEXT NOT NULL DEFAULT '',    -- custom handler, default POST
	payload_tpl             TEXT NOT NULL DEFAULT '',    -- custom handler Go template
	http_headers            TEXT NOT NULL DEFAULT '[]',  -- custom handler [{name,value}]
	secrets_enc             TEXT NOT NULL DEFAULT '{}',  -- custom handler {name: enc:v1}
	debug                   INTEGER NOT NULL DEFAULT 0,
	created_at              TEXT NOT NULL,
	created_by              TEXT NOT NULL,
	updated_at              TEXT NOT NULL,
	updated_by              TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_subscription_events (
	subscription_id TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
	event_type      TEXT NOT NULL,
	PRIMARY KEY (subscription_id, event_type)
);

CREATE INDEX IF NOT EXISTS idx_webhook_subscription_events_type ON webhook_subscription_events (event_type);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
	id               TEXT PRIMARY KEY,                   -- uuid
	subscription_id  TEXT NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
	event_type       TEXT NOT NULL,
	payload          TEXT NOT NULL,                      -- envelope snapshot (wire bytes)
	status           TEXT NOT NULL CHECK (status IN ('pending','delivering','delivered','dead')),
	attempts         INTEGER NOT NULL DEFAULT 0,
	next_attempt_at  TEXT NOT NULL,
	last_error       TEXT NOT NULL DEFAULT '',
	last_status_code INTEGER,
	created_at       TEXT NOT NULL,
	delivered_at     TEXT
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_queue ON webhook_deliveries (status, next_attempt_at);
