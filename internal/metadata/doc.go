// Package metadata owns all SQL state (repositories, nodes, blobs, users,
// tokens, permissions, audit events) behind a dialect-neutral Store with
// embedded migrations (architecture sections 3.2 and 6).
//
// Layout:
//
//	api.go            Store interface, six sub-store interfaces, row types, sentinels
//	store.go          Open: driver dispatch, PRAGMAs, admin seed (sqliteStore)
//	migrate.go        embedded migrator (transactions + schema_migrations ledger)
//	migrations/       SQL per dialect; sqlite live, postgres placeholder
//	password.go       argon2id hash/verify (t=1, m=64MiB, p=4, PHC strings; auth re-exports)
//	substores.go      RepoStore / NodeStore / BlobStore implementations
//	substores_auth.go UserStore / TokenStore / PermissionStore / AuditStore
//
// Conventions (ADR-0007): timestamps are RFC3339 UTC text; booleans are
// INTEGER 0/1; SQL stays inside the SQLite/Postgres common subset; the
// database handle is pooled at one connection (SQLite single-writer) and the
// sub-stores are safe for concurrent use.
//
// The argon2id helpers stay here (T-11): auth imports this package for its
// store adapters, so the implementation cannot move up without a cycle;
// internal/auth re-exports them as the auth-owned surface.
package metadata
