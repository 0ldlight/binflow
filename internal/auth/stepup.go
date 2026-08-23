// Token-minting step-up authentication (M7, FR-68 / ADR-0027 as finalized by
// T-214): the second credential POST /api/security/token demands from a
// NON-ADMIN web-session caller when auth.token_step_up is enabled.
//
// Two legs, selected by the caller's owning provider (users.provider):
//
//   - local / ldap: body step_up_password. The local leg re-runs the argon2id
//     comparison against the row's stored hash (inside the service's hash
//     gate, T-192); the LDAP leg re-binds against the directory through the
//     SAME connector the login endpoint uses, then resolves the bound
//     identity back to the local row and requires it to be the caller.
//   - oidc: body step_up_grant. The grant is an opaque 256-bit random string
//     minted by the OIDC callback after a prompt=login re-authentication
//     (httpapi/oidc.go carries the flow); only its sha256 is stored, it is
//     bound to {username, session_id} and SINGLE-USE — the first consumption
//     attempt with the correct grant value deletes it, whatever the binding
//     outcome (a grant that has been seen outside the mint flow must never
//     answer a second time).
//
// The grant ledger is in-process memory (ADR-0027 decision 4: single-instance
// deployment is the stated fact; a restart empties it and users re-authenticate
// — deliberately no schema change). Expired entries are swept on issuance;
// unconsumed ones die with their TTL either way.

package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrStepUpInvalid rejects a presented step-up credential: wrong password,
// failed LDAP bind, or an unknown/expired/replayed/misbound grant. The HTTP
// layer renders it as 401 step_up_invalid (ADR-0027 decision 5). A MISSING
// credential is not this error — the handler answers 401 step_up_required
// before any verification runs.
var ErrStepUpInvalid = errors.New("auth: step-up credential rejected")

// stepUpGrant is one ledger entry. Keyed by sha256(plaintext), which is the
// only form stored — the plaintext appears exactly once, in the callback's
// redirect (NFR-S2, the same visibility rule as tokens and sessions).
type stepUpGrant struct {
	username    string
	sessionHash string
	expiresAt   time.Time
}

// stepUpLedger is the in-memory single-use grant store. The mutex guards the
// map; entries are values, so a locked read+delete is the whole
// consume-and-burn transaction.
type stepUpLedger struct {
	mu     sync.Mutex
	grants map[string]stepUpGrant
}

func newStepUpLedger() *stepUpLedger {
	return &stepUpLedger{grants: make(map[string]stepUpGrant)}
}

// grantBytes matches the session/token entropy budget: 32 crypto/rand bytes
// (256 bits — the "opaque 256-bit random string" of ADR-0027 decision 4).
const grantBytes = 32

// IssueStepUpGrant mints one single-use mint grant bound to
// {username, sessionHash}, live for ttl (the config's grant TTL — callers
// pass config.Auth.TokenStepUpGrantTTL, whose [60, 3600] domain config
// validation owns). Issuance also sweeps expired entries: the ledger holds
// at most the outstanding grants of users mid-mint, so the occasional O(n)
// pass on a human-scale event is the whole reclamation story.
func (s *Service) IssueStepUpGrant(_ context.Context, username, sessionHash string, ttl time.Duration) (string, error) {
	if s.stepUpGrants == nil {
		return "", fmt.Errorf("auth: step-up ledger not initialized")
	}
	if username == "" || sessionHash == "" {
		return "", fmt.Errorf("auth: step-up grant requires a username and a session binding")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("auth: step-up grant ttl must be positive, got %s", ttl)
	}
	buf := make([]byte, grantBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: step-up grant entropy: %w", err)
	}
	grant := hex.EncodeToString(buf)
	now := time.Now()
	s.stepUpGrants.mu.Lock()
	defer s.stepUpGrants.mu.Unlock()
	for k, g := range s.stepUpGrants.grants {
		if now.After(g.expiresAt) {
			delete(s.stepUpGrants.grants, k)
		}
	}
	s.stepUpGrants.grants[grantHash(grant)] = stepUpGrant{
		username:    username,
		sessionHash: sessionHash,
		expiresAt:   now.Add(ttl),
	}
	return grant, nil
}

// ConsumeStepUpGrant settles one mint attempt. It reports whether the grant
// value is correct, unexpired, live and bound to exactly this
// {username, sessionHash}. A grant whose VALUE matches is deleted on this
// call regardless of the verdict: single-use means a replay, a binding probe
// or an expiry race must never leave a second chance behind (ADR-0027
// decision 4, "消费即删").
func (s *Service) ConsumeStepUpGrant(grant, username, sessionHash string) bool {
	if s.stepUpGrants == nil || grant == "" {
		return false
	}
	h := grantHash(grant)
	s.stepUpGrants.mu.Lock()
	defer s.stepUpGrants.mu.Unlock()
	g, ok := s.stepUpGrants.grants[h]
	delete(s.stepUpGrants.grants, h) // burn first: the value was presented
	if !ok {
		return false
	}
	if time.Now().After(g.expiresAt) {
		return false
	}
	// The binding comparison is over server-generated digests and the
	// caller's own username, not over a secret the caller could tune, so
	// plain equality is the honest operator here (the same posture as the
	// token-registry's sha256 row lookup).
	return g.username == username && g.sessionHash == sessionHash
}

// grantHash is the storage digest of a grant plaintext (sha256, hex).
func grantHash(grant string) string {
	digest := sha256.Sum256([]byte(grant))
	return hex.EncodeToString(digest[:])
}

// VerifyStepUpPassword settles the password leg of one step-up: the caller
// (already authenticated by their session) re-presents the identity source's
// password. The LEG is chosen by the user ROW's provider, not by the request:
// local rows verify against the stored argon2id hash through the shared hash
// gate; LDAP rows re-bind through the wired LDAP connector and the bound
// directory identity must resolve back to the caller's own row. Every
// failure — wrong password, disabled row, unwired provider, a directory
// outage, an OIDC-owned row (the wrong credential shape for that leg) —
// wraps ErrStepUpInvalid: the mint surface answers the uniform 401
// step_up_invalid and the cause stays in the server log (the same uniform
// rejection posture the login plane applies to provider outages).
func (s *Service) VerifyStepUpPassword(ctx context.Context, username, password string) error {
	if username == "" || password == "" {
		return fmt.Errorf("auth: step-up: empty username or password: %w", ErrStepUpInvalid)
	}
	u, err := s.users.Get(ctx, username)
	if err != nil {
		return fmt.Errorf("auth: step-up: resolve caller %q: %w", username, ErrStepUpInvalid)
	}
	if !u.Enabled {
		return fmt.Errorf("auth: step-up: user %q is disabled: %w", username, ErrStepUpInvalid)
	}
	switch u.Provider {
	case ProviderLocal:
		if u.PasswordHash == "" {
			return fmt.Errorf("auth: step-up: user %q has no local password: %w", username, ErrStepUpInvalid)
		}
		ok, verr := s.VerifyPassword(ctx, password, u.PasswordHash)
		if verr != nil {
			// Gate abandonment (client hung up while queued): the derivation
			// never ran, so there is no verdict — reject rather than pass,
			// with the transport cause intact for the log.
			return fmt.Errorf("auth: step-up verify %q: %w: %w", username, ErrStepUpInvalid, verr)
		}
		if !ok {
			return fmt.Errorf("auth: step-up: wrong password for %q: %w", username, ErrStepUpInvalid)
		}
		return nil
	case ProviderLDAP:
		return s.verifyStepUpLDAPBind(ctx, username, password)
	default:
		// OIDC-owned (or otherwise federated) rows do not take a password
		// here: their leg is the mint grant. Presenting one is the wrong
		// credential shape, not a missing one.
		return fmt.Errorf("auth: step-up: provider %q takes no step_up_password: %w", u.Provider, ErrStepUpInvalid)
	}
}

// verifyStepUpLDAPBind re-binds the caller against the directory through the
// wired connector — the same Bind the login endpoint's fallback arm uses —
// and requires the bound identity to BE the caller: Resolve(provider_id)
// must name the same row. No auto-create runs here (a step-up caller holds a
// session, so their row exists; creating rows from a re-verification would
// be a new identity plane).
func (s *Service) verifyStepUpLDAPBind(ctx context.Context, username, password string) error {
	if s.ldapProvider == nil {
		return fmt.Errorf("auth: step-up: ldap provider not wired: %w", ErrStepUpInvalid)
	}
	bindFn, ok := s.ldapProvider.(interface {
		Bind(ctx context.Context, username, password string) (*Claims, error)
	})
	if !ok {
		return fmt.Errorf("auth: step-up: ldap provider has no bind seam: %w", ErrStepUpInvalid)
	}
	claims, err := bindFn.Bind(ctx, username, password)
	if err != nil {
		return fmt.Errorf("auth: step-up: ldap bind for %q: %w: %w", username, ErrStepUpInvalid, err)
	}
	if claims == nil || claims.ProviderID == "" {
		return fmt.Errorf("auth: step-up: ldap bind returned no identity: %w", ErrStepUpInvalid)
	}
	pu, rerr := s.ldapProvider.Resolve(ctx, ProviderLDAP, claims.ProviderID)
	if rerr != nil {
		return fmt.Errorf("auth: step-up: ldap identity does not map to a local row: %w: %w", ErrStepUpInvalid, rerr)
	}
	if !strings.EqualFold(pu.Username, username) {
		return fmt.Errorf("auth: step-up: re-bound identity %q is not the session owner %q: %w",
			pu.Username, username, ErrStepUpInvalid)
	}
	if !pu.Enabled {
		return fmt.Errorf("auth: step-up: user %q is disabled: %w", username, ErrStepUpInvalid)
	}
	return nil
}
