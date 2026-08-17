package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// The three adapters below bridge metadata's concrete sub-stores onto the
// small consumer-side interfaces Service depends on. They exist so the core
// files (authenticator/authorizer/token) do not import the store package and
// stay testable against fakes. Assembly happens in cmd (T-16) via
// NewFromStore, or directly with New for tests.

// userStoreAdapter adapts metadata.UserStore.
type userStoreAdapter struct{ s metadata.UserStore }

func (a userStoreAdapter) Get(ctx context.Context, username string) (user, error) {
	u, err := a.s.Get(ctx, username)
	if err != nil {
		if isNotFound(err, metadata.ErrUserNotFound) {
			return user{}, fmt.Errorf("auth: user %q: %w", username, ErrUserNotFound)
		}
		return user{}, err
	}
	return user{Username: u.Username, PasswordHash: u.PasswordHash, IsAdmin: u.IsAdmin, Enabled: u.Enabled}, nil
}

func (a userStoreAdapter) UpdatePassword(ctx context.Context, username, passwordHash string) error {
	return a.s.UpdatePassword(ctx, username, passwordHash)
}

// tokenStoreAdapter adapts metadata.TokenStore.
type tokenStoreAdapter struct{ s metadata.TokenStore }

func (a tokenStoreAdapter) Create(ctx context.Context, t token) (int64, error) {
	return a.s.Create(ctx, &metadata.Token{
		Username: t.Username, TokenSHA256: t.TokenSHA256,
		ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt,
	})
}

func (a tokenStoreAdapter) GetBySHA256(ctx context.Context, sha256 string) (token, error) {
	t, err := a.s.GetBySHA256(ctx, sha256)
	if err != nil {
		if isNotFound(err, metadata.ErrTokenNotFound) {
			return token{}, fmt.Errorf("auth: token lookup: %w", ErrTokenNotFound)
		}
		return token{}, err
	}
	return token{
		ID: t.ID, Username: t.Username, TokenSHA256: t.TokenSHA256,
		ExpiresAt: t.ExpiresAt, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt,
	}, nil
}

func (a tokenStoreAdapter) Touch(ctx context.Context, id int64, lastUsedAt string) error {
	return a.s.Touch(ctx, id, lastUsedAt)
}

func (a tokenStoreAdapter) Delete(ctx context.Context, id int64) error {
	err := a.s.Delete(ctx, id)
	if err != nil && isNotFound(err, metadata.ErrTokenNotFound) {
		return fmt.Errorf("auth: token %d: %w", id, ErrTokenNotFound)
	}
	return err
}

// permissionStoreAdapter adapts metadata.PermissionStore.
type permissionStoreAdapter struct{ s metadata.PermissionStore }

func (a permissionStoreAdapter) ListTargets(ctx context.Context) ([]Target, error) {
	ts, err := a.s.ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Target, len(ts))
	for i, t := range ts {
		out[i] = Target{Name: t.Name, Repos: t.Repos, Includes: t.Includes, Excludes: t.Excludes}
	}
	return out, nil
}

func (a permissionStoreAdapter) PrincipalsFor(ctx context.Context, repoKey string) ([]PermissionRow, error) {
	ps, err := a.s.PrincipalsFor(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	out := make([]PermissionRow, len(ps))
	for i, p := range ps {
		out[i] = PermissionRow{
			ID: p.ID, TargetName: p.TargetName, Principal: p.Principal,
			PrincipalType: p.PrincipalType,
			CanRead:       p.CanRead, CanWrite: p.CanWrite, CanDelete: p.CanDelete,
		}
	}
	return out, nil
}

// isNotFound reports whether err wraps want (sentinel from metadata).
func isNotFound(err, want error) bool { return errors.Is(err, want) }

// NewFromStore wires Service over a metadata.Store. anonymousRead is
// config.Security.AnonymousAccess.
func NewFromStore(st metadata.Store, anonymousRead bool) *Service {
	return New(
		userStoreAdapter{s: st.Users()},
		tokenStoreAdapter{s: st.Tokens()},
		permissionStoreAdapter{s: st.Permissions()},
		anonymousRead,
	)
}

// PermissionSource is the exported permission-plane seam of Service: the
// same shape New consumes for its permission store. It exists so callers
// that need to observe or force permission-plane behavior (fail-closed
// tests, caching wrappers) can swap the source without rebuilding the
// whole service.
type PermissionSource = permissionSource

// WithPermissions returns a copy of svc whose permission source is replaced.
// Users/tokens/verifier are shared with svc.
func (s *Service) WithPermissions(perms PermissionSource) *Service {
	clone := *s
	clone.permissions = perms
	return &clone
}
