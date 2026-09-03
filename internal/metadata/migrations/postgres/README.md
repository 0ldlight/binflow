# Postgres migrations (placeholder)

BinFlow M1 ships the SQLite dialect only (ADR-0005/0007, PRD FR-3-AC10:
configuring `metadata.driver: postgres` makes `metadata.Open` fail with
"Postgres support is not enabled").

When Postgres support lands, this directory must hold `NNN_*.sql` files whose
version numbers track the SQLite dialect one-to-one (ADR-0007: both dialects
evolve in lockstep; the logical schema contract is
docs/design/architecture.md section 6). Rules for migration SQL:

- timestamps stay RFC3339 UTC text, booleans stay dialect-common (0/1 vs
  BOOLEAN is resolved per dialect file, never shared),
- no SQLite-only features in the postgres files and vice versa
  (AUTOINCREMENT/RETURNING are already avoided on the sqlite side),
- `id` columns: SQLite uses the ROWID alias `INTEGER PRIMARY KEY`, Postgres
  uses `SERIAL`/`GENERATED` (dialect-local choice recorded in the SQL files).

Version history of the sqlite dialect (postgres files must mirror these
one-to-one when the dialect lands):

- 001_init: M1 schema (repositories, remote_configs, blobs, nodes, users,
  tokens, permission_targets, permission_principals, audit_events,
  virtual_members).
- 002_docker: docker domain tables (docker_manifests, docker_tags,
  docker_refs; architecture section 6 final DDL).
- 003_remote_virtual: remote domain widening — remote_configs gains
  content_ttl_seconds/metadata_ttl_seconds/allow_private_upstream and renames
  unreachable_mask to blocked_out (the password column carries 'enc:v1:'
  AES-256-GCM ciphertext from T-66 on); new remote_cache validator table plus
  idx_remote_cache_expiry; virtual_members gains no DDL (ADR-0013 position
  semantics, comments only); idx_blobs_sha1 seam for T-73's sha1 fast-path.
- 004_console_governance: console/governance domain — users gains email; audit
  query indexes idx_audit_actor/idx_audit_action; new groups, user_groups
  (membership; the T-108 errata name, draft was group_members), web_sessions
  (id_hash=sha256 primary key, idx_web_sessions_user) and repo_usage tables
  (architecture section 6 final DDL, ADR-0014/0015).
- 005_quota_usage_backfill: seeds one repo_usage row per repository from the
  SUM of its node sizes (ON CONFLICT DO NOTHING); timestamp via the dialect's
  UTC-RFC3339 expression (sqlite strftime, postgres to_char(now() AT TIME
  ZONE 'utc', ...) — dialect-local choice).
- 006_user_groups_username_index: idx_user_groups_username on
  user_groups(username) — the GroupsOfUser hot lookup (T-97 review NB1).
- 007_folder_rows_backfill (ADR-0016): sentinel blobs row (sha256 = 64 x '0',
  size 0) FIRST — the nodes.sha256 FK makes it a hard prerequisite — then one
  folder node row per ancestor directory of every stored node. Porting notes
  for the postgres file: ON CONFLICT DO NOTHING replaces INSERT OR IGNORE;
  the recursive CTE ports with strpos(path, '/') for instr() and
  substr/length unchanged; created_by='' and created_at = updated_at = the
  youngest descendant's created_at (MAX) keep the value deterministic across
  replays.
- 008_oidc_ldap: auth provider columns (oidc_* on users, ldap_dn; ADR-0020).
- 009_replication: replication/federation tables (replications,
  replication_tasks; ADR-0021).
- 010_upload_sessions: local-filestore upload session persistence (T-209).
  The session id is a uuid text primary key in both dialects (no sequence),
  state is opaque JSON owned by the storage engine.
- 011_rbac_role: users.role closed-set text column with the is_admin=1 →
  'admin' backfill (is_admin stays as the compatibility mirror, same-statement
  maintenance, removal M8) + permission_principals.can_manage INTEGER (the 'm'
  action bit, repo-scoped). Statements are dialect-common (ADR-0026).
- 012_license: the licenses row (M10, ADR-0032 / architecture 15.1.2) —
  single-license model (id CHECK-pinned to 1), doc verbatim + derived
  columns, expires_at NULL = perpetual. Statements are dialect-common.
- 013_node_props: the artifact properties table (M10 T-286, ADR-0033) —
  multi-value-as-multi-row set semantics, composite FKs cascade node and
  repository deletes. Statements are dialect-common.
- 014_remote_tuning: remote_configs smart remote effective fields (M10
  T-290, FR-90.2) — socket_timeout_ms / metadata_retrieval_timeout_secs /
  unused_cleanup_period_hours, all 0 = unset (no backfill; the fetcher
  falls back to the legacy JSON fields). Statements are dialect-common.
- 018_webhook: the unified-event webhook plane (M13 T-362, ADR-0041) —
  webhook_subscriptions (key UNIQUE, criteria JSON text, secret_enc
  enc:v1-or-empty, secrets_enc named-secret map), the
  webhook_subscription_events child table (multi-event-type filters, the
  (subscription_id, event_type) pair keyed) and the webhook_deliveries
  outbox (status closed set, (status, next_attempt_at) queue index,
  ON DELETE CASCADE from the subscription). Porting notes: booleans are
  the INTEGER 0/1 convention on the sqlite side, BOOLEAN here; every id is
  a uuid text primary key (no sequences), timestamps stay RFC3339 UTC
  text. (015~017 predate this entry and carry no postgres notes here —
  their sqlite files are the contract.)
- 019_replication_globals: the global blockPush/blockPull emergency-brake
  row (M15 T-422, FR-138.3 / replication.md §9.2-B) — replication_globals,
  ONE row with the id CHECK-pinned to 1 (the same single-row shape as
  012_license), the two direction flags and the last-flip bookkeeping.
  Statements are dialect-common (booleans as 0/1 on the sqlite side,
  BOOLEAN here).
- 020_node_download_stats: the per-node download statistics widening of
  nodes (M16 T-438, FR-146.2 / ADR-0044 K69) — download_count /
  last_downloaded_at / last_downloaded_by / remote_download_count, all
  zero-defaulted with no backfill (pre-020 rows mean "never downloaded");
  folder rows stay zero by the counting UPDATE's marker exclusion, and the
  four columns are the download plane's single counting channel. Statements
  are dialect-common.
- 021_schedules: the unified cron schedules ledger (M16 T-446, FR-150.1 /
  ADR-0044 decision 2) — one row per scheduled full-type job keyed by
  (domain, key) with the domain CHECK closed set (maintenance | backup |
  replication), the last_status closed set, RFC3339-UTC next_run/last_run
  ('' = unscheduled / never) and the idx_schedules_due tick index. "No row
  = not scheduled" is the single-state semantics; nothing is preseeded.
  Statements are dialect-common.

The migrator currently embeds `migrations/sqlite/*.sql` only
(see ../migrate.go).
