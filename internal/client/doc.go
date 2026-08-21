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
// only).
package client
