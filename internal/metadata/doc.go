// Package metadata owns all SQL state (repositories, nodes, blobs, users,
// tokens, permissions, audit events, docker index tables) behind a
// dialect-neutral Store with embedded migrations (architecture sections 3.2
// and 6).
//
// Layout:
//
//	api.go            Store interface, sub-store interfaces, row types, sentinels
//	store.go          Open: driver dispatch, PRAGMAs, admin seed (sqliteStore)
//	migrate.go        embedded migrator (transactions + schema_migrations ledger)
//	migrations/       SQL per dialect; sqlite live, postgres placeholder
//	password.go       argon2id hash/verify (t=1, m=64MiB, p=4, PHC strings; auth re-exports)
//	substores.go      RepoStore / NodeStore / BlobStore implementations
//	substores_auth.go UserStore / TokenStore / PermissionStore / AuditStore
//	substores_docker.go DockerStore: docker_manifests / docker_tags / docker_refs
//	substores_remote_virtual.go RemoteStore / VirtualStore:
//	                   remote_configs / remote_cache / virtual_members
//
// Conventions (ADR-0007): timestamps are RFC3339 UTC text; booleans are
// INTEGER 0/1; SQL stays inside the SQLite/Postgres common subset; the
// database handle is pooled at one connection (SQLite single-writer) and the
// sub-stores are safe for concurrent use.
//
// The argon2id helpers stay here (T-11): auth imports this package for its
// store adapters, so the implementation cannot move up without a cycle;
// internal/auth re-exports them as the auth-owned surface.
//
// # docker_refs consistency contract (architecture 11.12)
//
// docker_refs deliberately carries NO database-level foreign keys — not to
// repositories(repo_key) and not to docker_manifests' composite primary key,
// of which it references only partial columns (SQLite would require the FK
// to cover a UNIQUE key; declaring partial-column references is not
// supported). Consistency is instead a store + service layer contract:
//
//   - deleting a manifest (DockerStore.DeleteManifest) drops its refs and
//     repointing tags in the SAME transaction — a manifest row and its ref
//     ledger never diverge for readers;
//   - deleting a whole image (DockerStore.DeleteImage) moves all three
//     tables in one transaction;
//   - repository teardown: docker_manifests and docker_tags cascade via
//     their ON DELETE CASCADE FK, but docker_refs does not — the teardown
//     path MUST call DockerStore.DeleteRepoRefs while the repository rows
//     are being dropped, or ref rows linger as garbage that the GC keeps
//     honoring (they extend the live set forever);
//   - the GC mark set is nodes ∪ docker_refs (architecture 4.4): a blob row
//     referenced by a docker_refs edge is live even with no node row. Orphan
//     tags (a tag whose digest has no docker_manifests row) are outside this
//     contract and must be prevented by the service layer's delete ordering;
//     QA covers them.
//
// docker node layout (002_docker note, no extra tables): manifest blob node
// path "<image>/manifests/<digest-hex>", layer/config blob node path
// "<image>/blobs/<digest-hex>".
package metadata
