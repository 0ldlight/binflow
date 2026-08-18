package docker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
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
// TokenRegistry rows carry a NOT NULL username (FK to users), and a REAL
// account must never own the tokens anonymous pulls obtain — any grant of
// that account would leak into every anonymous Bearer. "_docker_anonymous"
// is the synthetic alias: the /v2 plane maps its principal onto the
// anonymous identity (principalOf), so the tokens it owns carry exactly the
// anonymous rights (read-only while anonymous_access=true) and nothing
// else. D44-1: the account is ENABLED — since the ping always challenges,
// anonymous pulls flow through this token as a real Bearer, and the
// verifier refuses nothing. Grants to this account are IGNORED on /v2 (the
// mapping wins); operators must not confuse it with a service account.
const anonymousSubject = "_docker_anonymous"

// anonymousSecretLen is the entropy of the synthetic account's one-time
// password secret (256 bits, NFR-S2 class); the plaintext is discarded at
// seed time, so the stored argon2id hash is un-loginable by construction.
const anonymousSecretLen = 32

// ensureAnonymousSubject makes the synthetic "_docker_anonymous" account
// exist: enabled (its tokens must verify, D44-1), non-admin, grant-less,
// with the password hash of a random secret nobody kept. Idempotent and
// race-safe: a concurrent/prior insert surfaces as ErrDuplicate or a UNIQUE
// constraint wrap, and the row's presence is re-verified before any error
// is reported.
func ensureAnonymousSubject(ctx context.Context, seed anonymousSubjectSeed) error {
	if _, err := seed.Get(ctx, anonymousSubject); err == nil {
		return nil
	} else if !errors.Is(err, metadata.ErrUserNotFound) {
		return fmt.Errorf("docker: anonymous token subject lookup: %w", err)
	}
	secret := make([]byte, anonymousSecretLen)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("docker: anonymous token subject entropy: %w", err)
	}
	// Hash a random hex secret and drop it: the account can never be logged
	// into via password (a valid PHC keeps VerifyPassword's parse honest),
	// which is what makes "enabled" safe.
	hash, err := auth.HashPassword(hex.EncodeToString(secret))
	if err != nil {
		return fmt.Errorf("docker: hashing anonymous token subject secret: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	err = seed.Create(ctx, &metadata.User{
		Username:     anonymousSubject,
		PasswordHash: hash,
		IsAdmin:      false,
		Enabled:      true,
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

// seedAnonymousOnce runs ensureAnonymousSubject once per HANDLER against
// seed (per-handler dedupe state lives on Handler — see its field comment;
// the process-wide sync.Once this replaces leaked the first assembly's
// success into every later one, T-54). Concurrent first requests serialize
// on the handler mutex; ensureAnonymousSubject itself stays idempotent for
// the assemblies that race anyway. A failure is returned uncached so the
// next request retries — a transient store error must not poison anonymous
// pulls for the process lifetime.
func (h *Handler) seedAnonymousOnce(ctx context.Context, seed anonymousSubjectSeed) error {
	if seed == nil {
		return errors.New("docker: no user store wired for the anonymous token subject")
	}
	h.anonSeedMu.Lock()
	defer h.anonSeedMu.Unlock()
	if h.anonSeeded {
		return nil
	}
	if err := ensureAnonymousSubject(ctx, seed); err != nil {
		return err
	}
	h.anonSeeded = true
	return nil
}
