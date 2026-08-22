// Package client provides an HTTP client for the BinFlow REST API.
//
// Client wraps the standard library's http.Client with convenience methods for
// BinFlow's management plane: repository CRUD, artifact management, user
// administration, and API token operations. Every method targets the
// /binflow/api/** route family (architecture section 7.1).
//
// The package does not import internal/storage, internal/metadata, or
// internal/httpapi — it is a pure consumer of the REST API, suitable for use
// in CLI tools, migration scripts, and integration tests.
//
// Error handling follows the server's errors[] envelope (envelope.go): the
// client parses the JSON body and surfaces structured StatusError values so
// callers can inspect the status code and message without JSON decoding.
//
// Imports policy: only standard library + project-internal packages that are
// NOT server-side internals. The current dependency footprint is zero (stdlib
// only). The ONE sanctioned exception is test-only: realstack_test.go imports
// the server stack (internal/httpapi and its collaborators) to cross-validate
// this package's decode faces against the real handlers — the library itself
// stays a pure REST consumer.
//
// Response-shape policy (T-189): every decode face targets the REAL server
// behavior as implemented by internal/httpapi (plain-text repo mutations,
// OAuth-style token body with int64 token_id, nested item checksums, int64
// list sizes). Where a pre-alignment spelling diverged, decode keeps a
// narrowly-documented legacy fallback arm; the real shape is always tried
// first and is the asserted contract in tests.
//
// Request-shape policy (T-191): the same rule holds for the ENCODE face —
// request bodies carry the field spellings internal/httpapi actually decodes
// (RepoCreateRequest.Members rides the wire as "repositories", Includes as
// "includesPattern", Excludes as "excludesPattern"). Go field names are API
// stability for callers; only the tags cross the wire.
package client
