// Package audit appends repository and security events to an append-only
// log and serves filtered queries (architecture section 3.5).
//
// Append is best-effort by contract: an audit failure is logged (structured
// slog, never containing credentials — NFR-S3) but does not block the
// business operation it describes (architecture section 11 item 4 records
// this as a deliberate M1 compromise; strict two-phase audit is M4+).
//
// Redaction (NFR-S3): events flow through Redact before storage, which
// blanks the credential-shaped fields detail authors most commonly slip in.
// The package never logs Authorization headers, passwords or token
// plaintexts on any path.
package audit
