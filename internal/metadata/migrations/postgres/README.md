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

The migrator currently embeds `migrations/sqlite/*.sql` only
(see ../migrate.go).
