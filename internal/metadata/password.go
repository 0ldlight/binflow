package metadata

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (ADR-0009 / PRD NFR-S1): t=1 iteration, m=64 MiB,
// p=4 lanes, 32-byte tag, 16-byte random salt. Hashes are stored as PHC-style
// strings so each hash carries the parameters it was produced with.
//
// Placement note (T-11): architecture section 3.4 assigns password hashing
// to the auth package, but the dependency edge runs auth -> metadata (auth's
// store adapters live in internal/auth/deps.go), so the implementation
// cannot move up without an import cycle or a hook indirection. Both were
// judged worse than this file's current home: the admin seed needs the
// hasher at Open time, before any auth construction. The functions stay
// exported here and internal/auth re-exports them as auth.HashPassword /
// auth.VerifyPassword so callers see the auth-owned API surface. Deviation
// recorded in reports/agents/T-11.md for the architect.
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // KiB
	argon2Threads = 4
	argon2KeyLen  = 32
	argon2SaltLen = 16
)

// ErrInvalidPasswordHash is returned when a stored hash is not a valid
// argon2id PHC string.
var ErrInvalidPasswordHash = errors.New("metadata: invalid argon2id password hash")

// errHashMismatch marks a well-formed hash whose tag does not match; it is
// distinct from ErrInvalidPasswordHash so malformed stored hashes (data
// corruption) are distinguishable from a plain wrong password.
var errHashMismatch = errors.New("metadata: argon2 tag mismatch")

// HashPassword derives the argon2id PHC string for a plaintext password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("metadata: generating argon2 salt: %w", err)
	}
	tag := argon2.IDKey([]byte(password), salt,
		argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag)), nil
}

// VerifyPassword reports whether password matches the stored argon2id PHC
// hash. Parameters are read from the hash itself; the tag comparison is
// constant-time. A malformed hash fails closed.
func VerifyPassword(password, encoded string) bool {
	ok, err := verifyArgon2(password, encoded)
	return err == nil && ok
}

// verifyArgon2 decodes the PHC string and re-derives the tag. A non-nil error
// means the stored hash is unusable (ErrInvalidPasswordHash); errHashMismatch
// means the hash was well formed but the password differs.
func verifyArgon2(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, tag
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, ErrInvalidPasswordHash
	}
	iters, memory, threads, err := parseArgon2Params(parts[3])
	if err != nil {
		return false, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, ErrInvalidPasswordHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, ErrInvalidPasswordHash
	}
	if memory < 8*int64(threads) { // argon2 spec floor, guards absurd inputs
		return false, ErrInvalidPasswordHash
	}
	got := argon2.IDKey([]byte(password), salt, iters, uint32(memory), threads, uint32(len(want))) // memory bounded by the argon2 spec floor check above (G115 excluded globally)
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, errHashMismatch
	}
	return true, nil
}

// parseArgon2Params parses "m=65536,t=1,p=4" in any parameter order.
func parseArgon2Params(field string) (iters uint32, memory int64, threads uint8, err error) {
	var haveIters, haveMemory, haveThreads bool
	for _, kv := range strings.Split(field, ",") {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return 0, 0, 0, fmt.Errorf("bad parameter %q", kv)
		}
		switch key {
		case "t":
			var t uint64
			t, err = strconv.ParseUint(value, 10, 32)
			iters = uint32(t)
			haveIters = err == nil
		case "m":
			memory, err = strconv.ParseInt(value, 10, 64)
			haveMemory = err == nil
		case "p":
			var p uint64
			p, err = strconv.ParseUint(value, 10, 8)
			threads = uint8(p)
			haveThreads = err == nil
		default:
			return 0, 0, 0, fmt.Errorf("unknown parameter %q", key)
		}
		if err != nil {
			return 0, 0, 0, err
		}
	}
	if !haveIters || !haveMemory || !haveThreads || iters == 0 || memory <= 0 || threads == 0 {
		return 0, 0, 0, fmt.Errorf("incomplete parameters %q", field)
	}
	return iters, memory, threads, nil
}
