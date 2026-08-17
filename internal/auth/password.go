package auth

import "github.com/lzwzzy/binflow/internal/metadata"

// Argon2id password hashing, auth-owned API surface (architecture section
// 3.4). The bytes live in internal/metadata/password.go because the
// dependency edge runs auth -> metadata (auth's store adapters, deps.go)
// and the admin seed needs the hasher at metadata.Open time — moving the
// implementation up would need a hook indirection worse than the current
// placement. See the placement note there; deviation recorded for the
// architect in reports/agents/T-11.md. Parameters of record (ADR-0009 /
// PRD NFR-S1): argon2id t=1, m=64 MiB, p=4, 32-byte tag, 16-byte salt,
// PHC strings carrying their parameters.

// HashPassword derives the argon2id PHC string for a plaintext password.
func HashPassword(password string) (string, error) { return metadata.HashPassword(password) }

// VerifyPassword checks a plaintext against a stored argon2id PHC hash in
// constant time; malformed hashes fail closed.
func VerifyPassword(password, encoded string) bool {
	return metadata.VerifyPassword(password, encoded)
}
