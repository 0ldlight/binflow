// T-219 acceptance, auth plane (M7 FR-68 / ADR-0027 decisions 3/4): the
// mint-grant ledger (issue/consume/binding/single-use/TTL) and the password
// leg's provider dispatch (local argon2 verify, LDAP re-bind through the
// wired connector with the resolve-back-to-caller check).

package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// stepUpFixture is a store-backed service over seeded local, LDAP and OIDC
// rows, with the LDAP arm wired to the shared in-memory directory mock.
type stepUpFixture struct {
	svc *auth.Service
	st  metadata.Store
	ctx context.Context
}

func newStepUpFixture(t *testing.T, wireLDAP bool) *stepUpFixture {
	t.Helper()
	ctx := context.Background()
	st, err := metadata.Open(ctx, metadata.Options{
		Driver: "sqlite",
		Path:   filepath.Join(t.TempDir(), "binflow.db"),
	})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// local row WITH a password, ldap row (empty hash, DN-bound), oidc row.
	makeUser(t, ctx, st.Users(), "loc", "loc-pw", false, "local", "")
	makeUser(t, ctx, st.Users(), "jdoe", "", false, "ldap", "uid=jdoe,dc=example,dc=com")
	makeUser(t, ctx, st.Users(), "ssouser", "", false, "oidc", "oidc-sub-1")

	svc := auth.NewFromStore(st, false)
	if wireLDAP {
		mock := newMockLDAPConn()
		mock.addUser("uid=jdoe,dc=example,dc=com", "jdoe-ldap-pw", map[string][]string{
			"uid":         {"jdoe"},
			"objectClass": {"posixAccount"},
		})
		prov, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled:        true,
			URL:            "ldap://ldap.example.com:389",
			BaseDN:         "dc=example,dc=com",
			UserFilter:     "(&(objectClass=posixAccount)(uid=%s))",
			UserIDAttr:     "uid",
			PoolSize:       2,
			ConnectTimeout: 2 * time.Second,
			RequestTimeout: 5 * time.Second,
		}, auth.NewLDAPResolver(st.Users()),
			auth.LDAPDialer(func(context.Context, string, ...ldap.DialOpt) (auth.LDAPConn, error) {
				return mock, nil
			}))
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		t.Cleanup(prov.Close)
		svc = svc.WithLDAP(prov)
	}
	return &stepUpFixture{svc: svc, st: st, ctx: ctx}
}

// TestStepUpGrantLifecycle: issue -> consume happy path, then the rejection
// table — replay (single-use), wrong user, wrong session, unknown grant,
// expiry — and that a binding-mismatch attempt BURNS the grant just like a
// successful consumption does.
func TestStepUpGrantLifecycle(t *testing.T) {
	f := newStepUpFixture(t, false)
	const (
		user    = "ssouser"
		session = "sess-hash-1"
	)
	grant, err := f.svc.IssueStepUpGrant(f.ctx, user, session, 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueStepUpGrant: %v", err)
	}
	if len(grant) != 64 { // 256-bit, hex — the ADR's opaque-string shape
		t.Errorf("grant length = %d, want 64 hex chars", len(grant))
	}
	if !f.svc.ConsumeStepUpGrant(grant, user, session) {
		t.Fatal("first consumption of a fresh bound grant was refused")
	}
	if f.svc.ConsumeStepUpGrant(grant, user, session) {
		t.Error("replayed grant was accepted; grants are single-use")
	}

	// A grant presented with the WRONG binding must still burn.
	g2, err := f.svc.IssueStepUpGrant(f.ctx, user, session, 5*time.Minute)
	if err != nil {
		t.Fatalf("IssueStepUpGrant #2: %v", err)
	}
	if f.svc.ConsumeStepUpGrant(g2, "someone-else", session) {
		t.Error("grant consumed under a foreign username")
	}
	if f.svc.ConsumeStepUpGrant(g2, user, session) {
		t.Error("a grant probed with a wrong binding survived; it must burn on first presentation")
	}

	// Unknown values are refused without side effects.
	if f.svc.ConsumeStepUpGrant("no-such-grant", user, session) {
		t.Error("unknown grant accepted")
	}
}

// TestStepUpGrantExpiry lives in stepup_internal_test.go: planting an
// already-expired ledger row is the only deterministic way past the
// positive-TTL issuance guard.

// TestStepUpGrantIssueValidation: issuance rejects the impossible bindings.
func TestStepUpGrantIssueValidation(t *testing.T) {
	f := newStepUpFixture(t, false)
	for name, tc := range map[string]struct {
		user, session string
		ttl           time.Duration
	}{
		"no username":      {"", "sess", time.Minute},
		"no session":       {"u", "", time.Minute},
		"non-positive ttl": {"u", "sess", 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.svc.IssueStepUpGrant(f.ctx, tc.user, tc.session, tc.ttl); err == nil {
				t.Error("issuance succeeded, want rejection")
			}
		})
	}
}

// TestVerifyStepUpPasswordLegs: the password leg dispatches on the ROW's
// provider — local rows verify against the stored hash, LDAP rows re-bind
// through the wired connector (wrong bind rejected; the bound identity must
// resolve back to the caller), and OIDC rows take no password at all.
func TestVerifyStepUpPasswordLegs(t *testing.T) {
	f := newStepUpFixture(t, true)
	cases := []struct {
		name    string
		user    string
		pass    string
		wantErr bool
	}{
		{name: "local correct password", user: "loc", pass: "loc-pw"},
		{name: "local wrong password", user: "loc", pass: "wrong", wantErr: true},
		{name: "local empty password", user: "loc", pass: "", wantErr: true},
		{name: "unknown user", user: "ghost", pass: "whatever", wantErr: true},
		{name: "ldap correct password", user: "jdoe", pass: "jdoe-ldap-pw"},
		{name: "ldap wrong password", user: "jdoe", pass: "wrong", wantErr: true},
		{name: "oidc row takes no password", user: "ssouser", pass: "anything", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := f.svc.VerifyStepUpPassword(f.ctx, tc.user, tc.pass)
			if tc.wantErr {
				if err == nil {
					t.Fatal("verification succeeded, want rejection")
				}
				if !errors.Is(err, auth.ErrStepUpInvalid) {
					t.Errorf("error = %v, want it to wrap auth.ErrStepUpInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("VerifyStepUpPassword: %v", err)
			}
		})
	}
}

// TestVerifyStepUpPasswordLDAPUnwired: an LDAP-owned row on a service whose
// LDAP arm is not wired is rejected (the leg cannot run), wrapping
// ErrStepUpInvalid — the uniform mint-surface refusal.
func TestVerifyStepUpPasswordLDAPUnwired(t *testing.T) {
	f := newStepUpFixture(t, false)
	err := f.svc.VerifyStepUpPassword(f.ctx, "jdoe", "jdoe-ldap-pw")
	if err == nil || !errors.Is(err, auth.ErrStepUpInvalid) {
		t.Errorf("error = %v, want ErrStepUpInvalid", err)
	}
}

// TestVerifyStepUpPasswordDisabledRow: a disabled local row refuses even the
// correct password — the step-up path applies the same account-state rule as
// every other authentication arm.
func TestVerifyStepUpPasswordDisabledRow(t *testing.T) {
	f := newStepUpFixture(t, true)
	if err := f.st.Users().SetEnabled(f.ctx, "loc", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	err := f.svc.VerifyStepUpPassword(f.ctx, "loc", "loc-pw")
	if err == nil || !errors.Is(err, auth.ErrStepUpInvalid) {
		t.Errorf("error = %v, want ErrStepUpInvalid", err)
	}
}

// TestStepUpLedgerSharedAcrossWithClones: the With* builders clone the
// Service struct, and the ledger must be ONE process-wide store behind every
// clone — cmd wires WithOIDC/WithLDAP after NewFromStore, and the grant
// issued through the final clone must be consumable through it.
func TestStepUpLedgerSharedAcrossWithClones(t *testing.T) {
	f := newStepUpFixture(t, false)
	clone := f.svc.WithPermissions(nil) // any With* clone
	clone = clone.WithOIDC(nil, nil)
	grant, err := clone.IssueStepUpGrant(f.ctx, "ssouser", "sess", time.Minute)
	if err != nil {
		t.Fatalf("IssueStepUpGrant: %v", err)
	}
	if !f.svc.ConsumeStepUpGrant(grant, "ssouser", "sess") {
		t.Error("grant issued via a clone was not consumable via the original service")
	}
}
