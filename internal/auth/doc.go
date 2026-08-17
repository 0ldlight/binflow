// Package auth authenticates requests (Basic password or API token), issues
// and revokes tokens, and decides path-level authorization
// (architecture section 3.4; implemented by T-11).
//
// Redaction red line (NFR-S3): no function in this package writes a
// presented credential — password, token plaintext, Authorization header —
// to an error message or log line. Credential failures are typed
// (ErrInvalidCredentials and friends) with static wording; TestLogHygiene
// enforces this by capturing the package's slog output under a failing
// authentication storm.
package auth
