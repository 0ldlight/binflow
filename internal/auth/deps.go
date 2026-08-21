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
	return adaptUser(u), nil
}

// GetByProvider implements oidcUserResolver. It iterates the user list to
// find the first user whose (provider, provider_id) matches the given pair.
// This is O(n) in the number of users, which is acceptable for the initial
// implementation (the OIDC Bearer arm is called per-request, but the resolve
// path is only triggered on first authentication when auto-create is on, and
// even then the user list is small). A future optimization can add a direct
// provider+provider_id index to the metadata layer.
func (a userStoreAdapter) GetByProvider(ctx context.Context, prov Provider, providerID string) (*ProviderUser, error) {
	users, err := a.s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: list users for provider resolve: %w", err)
	}
	for _, u := range users {
		// When metadata.User has Provider/ProviderID fields, use them
		// directly. Until then, all users default to ProviderLocal, so
		// a non-local provider will never match.
		au := adaptUser(u)
		if au.Provider == prov && au.ProviderID == providerID {
			return &ProviderUser{
				Username: au.Username,
				IsAdmin:  au.IsAdmin,
				Enabled:  au.Enabled,
			}, nil
		}
	}
	return nil, fmt.Errorf("auth: %s user %q: %w", prov, providerID, ErrProviderUserNotFound)
}

// adaptUser converts a metadata.User to the auth package's internal user type.
func adaptUser(u *metadata.User) user {
	provider := Provider(u.Provider)
	switch provider {
	case ProviderOIDC, ProviderLDAP:
		// known external providers
	default:
		provider = ProviderLocal
	}
	return user{
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		IsAdmin:      u.IsAdmin,
		Enabled:      u.Enabled,
		Provider:     provider,
		ProviderID:   u.ProviderID,
	}
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

// groupStoreAdapter adapts metadata.GroupStore onto the name-oriented
// groupSource (T-97): rows become names, order preserved.
type groupStoreAdapter struct{ s metadata.GroupStore }

func (a groupStoreAdapter) GroupsOfUser(ctx context.Context, username string) ([]string, error) {
	groups, err := a.s.GroupsOfUser(ctx, username)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names, nil
}

// userCreatorAdapter adapts metadata.UserStore for the userCreator seam.
type userCreatorAdapter struct{ s metadata.UserStore }

func (a userCreatorAdapter) Create(ctx context.Context, params NewUserParams) error {
	now := metadata.Now()
	mu := &metadata.User{
		Username:     params.Username,
		PasswordHash: params.PasswordHash,
		IsAdmin:      params.IsAdmin,
		Enabled:      params.Enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
		Provider:     string(params.Provider),
		ProviderID:   params.ProviderID,
	}
	return a.s.Create(ctx, mu)
}

// NewLDAPResolver wraps a metadata.UserStore into an LDAPUserResolver. It is
// used by tests and by the LDAP provider wiring in cmd/binflow-server.
func NewLDAPResolver(store metadata.UserStore) LDAPUserResolver {
	return userStoreAdapter{s: store}
}

// NewOIDCResolver wraps a metadata.UserStore into an OIDCUserResolver (the
// same store-backed lookup NewLDAPResolver exposes, named for the OIDC
// provider's seam so cmd assembly reads symmetrically).
func NewOIDCResolver(store metadata.UserStore) OIDCUserResolver {
	return userStoreAdapter{s: store}
}

// UserCreator re-exports the auto-create seam (ADR-0020 decision 4) so
// callers outside the package can name and pass it. cmd assembly needs it:
// WithOIDC's creator parameter REPLACES whatever NewFromStore wired, and
// passing nil there silently disables auto-create — the exported constructor
// hands the caller the same store-backed implementation NewFromStore uses
// (T-157 leftover 2).
type UserCreator = userCreator

// NewUserCreator wraps a metadata.UserStore into the auto-create seam. The
// created row mirrors NewFromStore's wiring: provider/provider_id carried,
// CreatedAt/UpdatedAt stamped, PasswordHash exactly as given (empty for
// OIDC/LDAP users).
func NewUserCreator(store metadata.UserStore) UserCreator {
	return userCreatorAdapter{s: store}
}

// NewFromStore wires Service over a metadata.Store. anonymousRead is
// config.Security.AnonymousAccess. The browser-session arm (M4, ADR-0014)
// and the group-membership fill (M4, T-97) are wired unconditionally:
// metadata.Open always carries the 004 web_sessions/groups tables, so every
// store-backed service is also the console's SessionRegistry and carries
// SE-07's union semantics.
//
// The OIDC Bearer arm (M6, ADR-0020) is NOT wired by default — it requires
// an IdentityProvider implementation (T-154). Call WithOIDC after this
// to activate the arm.
func NewFromStore(st metadata.Store, anonymousRead bool) *Service {
	svc := New(
		userStoreAdapter{s: st.Users()},
		tokenStoreAdapter{s: st.Tokens()},
		permissionStoreAdapter{s: st.Permissions()},
		anonymousRead,
	).WithSessions(st.WebSessions()).WithGroups(groupStoreAdapter{s: st.Groups()})
	// Wire the userCreator so OIDC and LDAP auto-create paths work when the
	// service is backed by a real store. Callers that want a different
	// creator (e.g. tests) can still override it via WithOIDC.
	svc.userCreator = userCreatorAdapter{s: st.Users()}
	return svc
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
