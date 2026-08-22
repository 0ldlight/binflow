package adapter

import "context"

// PasswordVerifier is the gated argon2id verification seam for adapter
// login paths (T-204 / T-192 leftover 2): docker's /v2/token
// form-credential exchange and npm's couch login verify one password
// against a stored PHC hash, and that derivation must run inside the auth
// service's concurrency gate with request-scoped cancellation — the pure
// package function auth.VerifyPassword starts an unbounded ~64 MiB
// derivation that never notices the client hung up (the T-172 D-1 defect
// class).
//
// The interface is defined here, at the consumer, per project convention;
// *auth.Service satisfies it. Assemblies that wire a non-Service
// collaborator (test fakes) simply do not implement it, and the login
// paths fail closed — they never fall back to the ungated pure call.
type PasswordVerifier interface {
	// VerifyPassword reports whether password matches the stored argon2id
	// PHC string, acquiring a hash-gate slot first. A non-nil error means
	// ctx ended while queued: there is no verdict, and the error is a
	// transport-level abandonment, not a credential rejection (it does not
	// satisfy auth.ErrInvalidCredentials).
	VerifyPassword(ctx context.Context, password, encodedHash string) (bool, error)
}
