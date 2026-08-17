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

The migrator currently embeds `migrations/sqlite/*.sql` only
(see ../migrate.go).
