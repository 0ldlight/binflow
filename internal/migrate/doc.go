// Package migrate implements the Artifactory-to-BinFlow migration pipeline
// (T-167; artifact phase, empty-target guard and run report added by
// T-196 for the T-175 D6/D7/D8 findings).
//
// The pipeline is a four-phase read-convert-write flow. The first three
// run in a fixed order (later phases reference earlier ones: virtual
// repositories reference migrated members, users reference repositories
// only through groups, tokens reference users); the artifact phase runs
// LAST so the cheap configuration phases fail fast — a missing password
// strategy or a refused target aborts before the bulk data copy starts:
//
//	repos -> users -> tokens -> artifacts
//
//	  - reader.go reads the source Artifactory instance over its REST API
//	    (/api/repositories, /api/security/users, /api/security/token —
//	    shapes per docs/reverse/rest-api.md and docs/reverse/auth-model.md;
//	    the artifact face is the documented ?list&deep=1 file listing plus
//	    the plain GET download, with a FolderInfo walk as the fallback for
//	    sources that refuse repo-root listings).
//	  - converter.go maps Artifactory repository/user configurations onto
//	    BinFlow's model, deciding what can be carried and what must be
//	    skipped (with a recorded reason for every skip).
//	  - writer.go writes the converted entities into the target BinFlow
//	    instance through internal/client (the only sanctioned dependency
//	    on the client package — it isolates the pipeline from client API
//	    drift).
//	  - artifacts.go copies repository content: list, download (bounded
//	    concurrency, disk-buffered, digest-verified), upload through the
//	    plain-file face. generic is the primary supported type; maven
//	    copies too (plain layout files); docker/npm/pypi repository
//	    CONFIGURATIONS migrate but their artifacts are honestly left
//	    behind with a recorded reason (their upload faces are
//	    protocol-specific — the T-175 D2/D3/D4 evidence).
//	  - report.go renders migration_report.json (D8): per-phase counts,
//	    per-repository artifact rows, skip reasons and failure lists.
//
// Semantics:
//
//   - Empty-target guard (FR-63-AC1, D6): before any write the target must
//     hold ZERO repositories. A populated target refuses the run with a
//     non-zero exit; --allow-non-empty overrides it and records the merge
//     semantics (create-or-replace repositories, same-path artifacts
//     overwritten, unnamed target content untouched). --resume against an
//     endpoint-bound progress file with records continues regardless — the
//     occupancy is the migration's own earlier writes.
//   - Dry-run performs every read and every conversion but no writes; the
//     progress file is not touched.
//   - Progress is persisted after every configuration item and in batches
//     for artifacts (JSON, atomic rename), keyed by phase and item name;
//     --resume reloads it and skips completed items. Every write verb is
//     create-or-replace on the target, so a crashed run can be resumed
//     safely and a re-run of a completed item is harmless.
//   - Single-artifact failures never abort the artifact phase (PRD
//     FR-63): they are recorded and retried by --resume.
//   - Token values are NEVER migratable: Artifactory's token listing is
//     metadata-only (auth-model.md section 3.3 — the value is returned
//     exactly once at creation). The token phase therefore only accounts
//     for source tokens and reports them as skipped; operators recreate
//     tokens on the target and redistribute the values.
//
// Acceptance against a real Artifactory instance is a conditional leg
// (ticket Q9, pending the user's environment decision): the reader targets
// the documented REST shapes and is validated here against mock servers
// that implement those shapes.
package migrate
