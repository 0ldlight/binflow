package docker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// anonymousSubjectSeed is the consumer-side slice of metadata.UserStore the
// anonymous-token seeding needs. It exists so the adapter does not depend on
// the whole store interface (the same consumer-side-interface convention as
// RepoLookup).
type anonymousSubjectSeed interface {
	Get(ctx context.Context, username string) (*metadata.User, error)
	Create(ctx context.Context, u *metadata.User) error
}

// anonymousSubject is the token row subject for anonymous docker tokens.
// TokenRegistry rows carry a NOT NULL username, and a real account must
// never own a token the anonymous public can obtain (privilege injection:
// any grant that account has would leak into every anonymous pull).
// "_docker_anonymous" is a synthetic disabled account seeded by the
// adapter; it can never log in (disabled, no usable password hash) and
// carries no grants, so the token it owns authenticates as anonymous.
const anonymousSubject = "_docker_anonymous"

// anonymousPasswordHash is the synthetic account's stored hash: an argon2id
// PHC string of random bytes no one knows. The account is ALSO disabled
// (enabled=0), which is the actual "can never log in" guarantee — the hash
// only keeps the row shape valid for VerifyPassword even if someone flips
// the flag in the database.
const anonymousPasswordHash = "$argon2id$v=19$m=65536,t=2,p=1$" +
	"YW5vbnltb3VzLWRvY2tlci10b2tlbi1zZWVk$" +
	"YW5vbnltb3VzLWRvY2tlci10b2tlbi1zZWVk"

// ensureAnonymousSubject makes the synthetic "_docker_anonymous" account
// exist. TokenRegistry rows need a username (NOT NULL, FK to users), and a
// real account must never own the tokens anonymous pulls obtain — any grant
// of that account would leak into every anonymous Bearer. The account is
// disabled and carries no grants, so its tokens authenticate as anonymous
// (the verifier refuses disabled owners — an anonymous docker token
// therefore 401s the moment an operator enables it, which is the correct
// failure direction: closed, not open).
//
// Idempotent and race-safe: a concurrent/prior insert surfaces as
// ErrDuplicate or a UNIQUE constraint wrap, and the row's presence is
// re-verified before any error is reported.
func ensureAnonymousSubject(ctx context.Context, seed anonymousSubjectSeed) error {
	if _, err := seed.Get(ctx, anonymousSubject); err == nil {
		return nil
	} else if !errors.Is(err, metadata.ErrUserNotFound) {
		return fmt.Errorf("docker: anonymous token subject lookup: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	err := seed.Create(ctx, &metadata.User{
		Username:     anonymousSubject,
		PasswordHash: anonymousPasswordHash,
		IsAdmin:      false,
		Enabled:      false,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err == nil {
		return nil
	}
	// Lost a race with another instance/seeder: the row existing is the win
	// condition, re-check before failing.
	if _, gerr := seed.Get(ctx, anonymousSubject); gerr == nil {
		return nil
	}
	return fmt.Errorf("docker: seeding anonymous token subject: %w", err)
}

// anonSeedOnce guards the per-process seeding attempt: the token endpoint
// can be hit concurrently on a fresh instance, and even though the seeding
// is idempotent, paying the lookup once per process is the whole point.
var anonSeedOnce sync.Once
var anonSeedErr error

// seedAnonymousOnce runs ensureAnonymousSubject once per process against
// seed. The sync.Once holds the FIRST seed seen: assemblies swap stores in
// tests, so a handler built after a failed seed retries with its own store
// through the direct call — this wrapper is the production path's dedupe.
func seedAnonymousOnce(ctx context.Context, seed anonymousSubjectSeed) error {
	if seed == nil {
		return errors.New("docker: no user store wired for the anonymous token subject")
	}
	anonSeedOnce.Do(func() { anonSeedErr = ensureAnonymousSubject(ctx, seed) })
	return anonSeedErr
}
