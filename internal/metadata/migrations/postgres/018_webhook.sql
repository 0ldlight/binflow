-- 018_webhook.sql (postgres dialect) — the unified-event webhook plane's two
-- tables plus the subscription/event-type child table (M13 T-362, FR-114 /
-- ADR-0041 decisions 3 and 7).
--
-- Mirrors the sqlite dialect file 018 one-to-one (ADR-0007 lockstep); see
-- that file's header for the full contract and the calibration notes.
-- Booleans stay INTEGER 0/1 — the convention the shipped postgres files
-- (009/021/024) set and internal/webhook/store.go's integer comparison
-- (`WHERE s.enabled = 1`) requires; every id is a uuid text primary key (no
-- sequences), timestamps stay RFC3339 UTC text. Idempotent: CREATE TABLE IF
-- NOT EXISTS, the post-011 convention.

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
