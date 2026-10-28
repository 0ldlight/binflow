// Recurrent-failure breaker tests (T-632). Internal package (auth): the
// breaker's clock is injected to make window/decay behavior deterministic
// without real sleeps, the instant hashVerify seam replaces argon2 (the
// semantics under test are the ledger's, not the derivation's), and the
// ledger interior is asserted directly where the wire cannot observe it.

package auth

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

// recurrentFakeUsers is the minimal userSource: a fixed row set of local
// users with precomputed hashes.
type recurrentFakeUsers struct {
	rows map[string]user
}

func (f *recurrentFakeUsers) Get(_ context.Context, username string) (user, error) {
	u, ok := f.rows[username]
	if !ok {
		return user{}, ErrUserNotFound
	}
	return u, nil
}

func (f *recurrentFakeUsers) UpdatePassword(_ context.Context, _, _ string) error { return nil }

// recurrentFakeTokens rejects every token lookup, so the Basic token
// fallback lands on the plain invalid-credentials verdict.
type recurrentFakeTokens struct{}

func (recurrentFakeTokens) Create(context.Context, token) (int64, error) { return 0, nil }
func (recurrentFakeTokens) GetBySHA256(context.Context, string) (token, error) {
	return token{}, ErrTokenNotFound
}
func (recurrentFakeTokens) Touch(context.Context, int64, string) error { return nil }
func (recurrentFakeTokens) Delete(context.Context, int64) error        { return nil }

// recurrentFakeLDAP is a wired-but-rejecting directory: it counts Bind
// calls so the login-endpoint test can prove the blocked verdict
// short-circuits BEFORE the LDAP fallback arm.
type recurrentFakeLDAP struct {
	binds int
}

func (p *recurrentFakeLDAP) ProviderName() Provider { return ProviderLDAP }
func (p *recurrentFakeLDAP) Authenticate(context.Context, string) (*Claims, error) {
	return nil, errors.New("recurrentFakeLDAP: bearer arm not used")
}
func (p *recurrentFakeLDAP) Resolve(context.Context, Provider, string) (*ProviderUser, error) {
	return nil, ErrProviderUserNotFound
}
func (p *recurrentFakeLDAP) Bind(_ context.Context, _, _ string) (*Claims, error) {
	p.binds++
	return nil, errors.New("recurrentFakeLDAP: bind rejected")
}

// recurrentClock is the injected breaker clock (shared by limiter reads
// and writes through s.recurrent.now).
type recurrentClock struct {
	mu sync.Mutex
	at time.Time
}

func newRecurrentClock() *recurrentClock {
	return &recurrentClock{at: time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)}
}

func (c *recurrentClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *recurrentClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

const (
	recurrentUser      = "breaker-user"
	recurrentBystander = "bystander"
	recurrentUserPW    = "right-password" //nolint:gosec // throwaway test credential
)

// recurrentFixture is one breaker test world: a New-built service over
// fake stores, the injected clock wired into the limiter, and an instant
// hashVerify comparator (correct password succeeds without a derivation).
type recurrentFixture struct {
	svc   *Service
	clock *recurrentClock
}

func newRecurrentFixture(t *testing.T) *recurrentFixture {
	t.Helper()
	hash, err := HashPassword(recurrentUserPW)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	rows := map[string]user{}
	for _, name := range []string{recurrentUser, recurrentBystander} {
		rows[name] = user{
			Username: name, PasswordHash: hash, Enabled: true,
			Provider: ProviderLocal, Role: RoleUser,
		}
	}
	s := New(&recurrentFakeUsers{rows: rows}, recurrentFakeTokens{}, nil, true)
	clock := newRecurrentClock()
	s.recurrent.now = clock.Now
	s.hashVerify = func(_ context.Context, password, encoded string) bool {
		return password == recurrentUserPW && encoded == hash
	}
	return &recurrentFixture{svc: s, clock: clock}
}

// attempt presents one Basic credential through the exported entry point
// (the request shape every real caller uses) and returns the verdict.
func (f *recurrentFixture) attempt(t *testing.T, username, password string) error {
	t.Helper()
	r, err := http.NewRequest(http.MethodGet, "/binflow/generic-local/a.bin", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.SetBasicAuth(username, password)
	_, aerr := f.svc.Authenticate(context.Background(), r)
	return aerr
}

// wantBlocked asserts the verdict IS the breaker's: the blocked sentinel
// rides the chain and the wire text is the verbatim reference sentence.
func wantBlocked(t *testing.T, err error, wantText string) {
	t.Helper()
	if err == nil {
		t.Fatal("want a BlockedError, got success")
	}
	if !errors.Is(err, ErrRecurrentFailureBlocked) {
		t.Fatalf("want ErrRecurrentFailureBlocked in chain, got %v", err)
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("blocked verdict must stay 401-compatible (ErrInvalidCredentials in chain), got %v", err)
	}
	if got := err.Error(); got != wantText {
		t.Fatalf("wire text drift:\n got: %q\nwant: %q", got, wantText)
	}
}

// TestRecurrentFailureBreaker walks the AC matrix end-to-end: below
// threshold the posture is byte-for-byte the historical uniform
// rejection; at threshold the verbatim countdown block arms; the window
// lapses into a fresh count; success clears; the block is per-username;
// the countdown decays with the clock, not with probing.
func TestRecurrentFailureBreaker(t *testing.T) {
	// verbatimAt4 is the wire text a freshly armed block must render
	// byte-for-byte (window = 4s; "(s)" spelling is part of the text).
	verbatimAt4 := "This request is blocked due to recurrent request failures, please try again in 4 second(s)"

	cases := []struct {
		name string
		run  func(t *testing.T, f *recurrentFixture)
	}{
		{
			name: "below threshold every attempt keeps the uniform rejection",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold-1; i++ {
					err := f.attempt(t, recurrentUser, "wrong")
					if !errors.Is(err, ErrInvalidCredentials) {
						t.Fatalf("attempt %d: want ErrInvalidCredentials, got %v", i+1, err)
					}
					if errors.Is(err, ErrRecurrentFailureBlocked) {
						t.Fatalf("attempt %d: blocked below the threshold", i+1)
					}
				}
			},
		},
		{
			name: "at threshold the next attempt is blocked with the verbatim wire text",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold; i++ {
					if err := f.attempt(t, recurrentUser, "wrong"); errors.Is(err, ErrRecurrentFailureBlocked) {
						t.Fatalf("arming attempt %d itself must not be blocked", i+1)
					}
				}
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"), verbatimAt4)
			},
		},
		{
			name: "the block precedes verification (correct credential refused while armed)",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold; i++ {
					_ = f.attempt(t, recurrentUser, "wrong")
				}
				// While armed nothing is verified: even the RIGHT password
				// gets the countdown, at zero argon2 cost.
				wantBlocked(t, f.attempt(t, recurrentUser, recurrentUserPW), verbatimAt4)
			},
		},
		{
			name: "the window lapses into a fresh count",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold; i++ {
					_ = f.attempt(t, recurrentUser, "wrong")
				}
				f.clock.Advance(recurrentFailureWindow + time.Second)
				// First post-window attempt: not blocked (self-heal), and
				// it takes a FULL fresh count to re-arm.
				for i := 0; i < recurrentFailureThreshold-1; i++ {
					err := f.attempt(t, recurrentUser, "wrong")
					if errors.Is(err, ErrRecurrentFailureBlocked) {
						t.Fatalf("post-lapse attempt %d blocked: the count did not restart", i+1)
					}
				}
				_ = f.attempt(t, recurrentUser, "wrong") // the fresh threshold arm
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"), verbatimAt4)
			},
		},
		{
			name: "a successful authentication clears the accumulated count",
			run: func(t *testing.T, f *recurrentFixture) {
				// Defensive posture (the reference arm was not observed for
				// this case): threshold-1 failures then the RIGHT password.
				for i := 0; i < recurrentFailureThreshold-1; i++ {
					_ = f.attempt(t, recurrentUser, "wrong")
				}
				if err := f.attempt(t, recurrentUser, recurrentUserPW); err != nil {
					t.Fatalf("correct credential rejected: %v", err)
				}
				// The counter must be empty again: threshold-1 further
				// failures stay unblocked.
				for i := 0; i < recurrentFailureThreshold-1; i++ {
					if err := f.attempt(t, recurrentUser, "wrong"); errors.Is(err, ErrRecurrentFailureBlocked) {
						t.Fatalf("attempt %d after success blocked: success did not clear", i+1)
					}
				}
			},
		},
		{
			name: "the block is scoped to the probed username",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold+2; i++ {
					_ = f.attempt(t, recurrentUser, "wrong")
				}
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"), verbatimAt4)
				// A different username's first bad credential is judged on
				// its own count, never the breaker-user's.
				err := f.attempt(t, recurrentBystander, "wrong")
				if !errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrRecurrentFailureBlocked) {
					t.Fatalf("bystander dragged into the block: %v", err)
				}
			},
		},
		{
			name: "the countdown decays with the clock, not with probing",
			run: func(t *testing.T, f *recurrentFixture) {
				for i := 0; i < recurrentFailureThreshold; i++ {
					_ = f.attempt(t, recurrentUser, "wrong")
				}
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"),
					"This request is blocked due to recurrent request failures, please try again in 4 second(s)")
				f.clock.Advance(2500 * time.Millisecond)
				// Ceiling: a 1.5s remainder names the whole second above it.
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"),
					"This request is blocked due to recurrent request failures, please try again in 2 second(s)")
				// More probing at the same instant must not push the
				// countdown back up.
				wantBlocked(t, f.attempt(t, recurrentUser, "wrong"),
					"This request is blocked due to recurrent request failures, please try again in 2 second(s)")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, newRecurrentFixture(t))
		})
	}
}

// TestBlockedErrorWireContract pins the message arithmetic (ceiling
// seconds) and the dual sentinel chain: the blocked sentinel for the 403
// arm, ErrInvalidCredentials so pre-wiring planes keep the uniform 401.
func TestBlockedErrorWireContract(t *testing.T) {
	cases := []struct {
		name      string
		remaining time.Duration
		want      string
	}{
		{
			name:      "fresh window reads 4",
			remaining: 4 * time.Second,
			want:      "This request is blocked due to recurrent request failures, please try again in 4 second(s)",
		},
		{
			name:      "fractional remainder ceils up",
			remaining: 2500 * time.Millisecond,
			want:      "This request is blocked due to recurrent request failures, please try again in 3 second(s)",
		},
		{
			name:      "sub-second remainder still reads 1",
			remaining: 900 * time.Millisecond,
			want:      "This request is blocked due to recurrent request failures, please try again in 1 second(s)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &BlockedError{Username: recurrentUser, RetryIn: tc.remaining}
			if got := e.Error(); got != tc.want {
				t.Fatalf("wire text:\n got: %q\nwant: %q", got, tc.want)
			}
			if !errors.Is(e, ErrRecurrentFailureBlocked) {
				t.Error("ErrRecurrentFailureBlocked missing from the chain")
			}
			if !errors.Is(e, ErrInvalidCredentials) {
				t.Error("ErrInvalidCredentials missing from the chain (401-compatibility broken)")
			}
		})
	}
}

// TestRecurrentFailureIgnoresGateAbandonment: a hash-gate context
// cancellation is a transport-level abandonment, NOT a credential
// verdict (the T-192 posture) — it must leave the breaker ledger
// untouched, or a disconnected client could walk its own username into
// a block without a single adjudicated failure.
func TestRecurrentFailureIgnoresGateAbandonment(t *testing.T) {
	f := newRecurrentFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := http.NewRequest(http.MethodGet, "/binflow/generic-local/a.bin", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	r.SetBasicAuth(recurrentUser, "wrong")
	_, aerr := f.svc.Authenticate(ctx, r)
	if !errors.Is(aerr, context.Canceled) {
		t.Fatalf("want the gate abandonment verbatim (context.Canceled), got %v", aerr)
	}
	if errors.Is(aerr, ErrInvalidCredentials) || errors.Is(aerr, ErrRecurrentFailureBlocked) {
		t.Fatalf("abandonment misclassified as a credential verdict: %v", aerr)
	}
	f.svc.recurrent.mu.Lock()
	count := f.svc.recurrent.entries[recurrentUser].failures
	f.svc.recurrent.mu.Unlock()
	if count != 0 {
		t.Fatalf("abandonment recorded as failure %d/%d", count, recurrentFailureThreshold)
	}
	// The username is still threshold-1 away from a block.
	for i := 0; i < recurrentFailureThreshold-1; i++ {
		if berr := f.attempt(t, recurrentUser, "wrong"); errors.Is(berr, ErrRecurrentFailureBlocked) {
			t.Fatalf("attempt %d blocked: the abandonment consumed budget", i+1)
		}
	}
}

// TestAuthenticateCredentialsBlockedSkipsLDAP: the login endpoint's LDAP
// fallback must not offer a blocked username a second arm — the block is
// a final verdict for the login. The fake directory counts its Bind
// calls: after the block arms, no further bind may run.
func TestAuthenticateCredentialsBlockedSkipsLDAP(t *testing.T) {
	f := newRecurrentFixture(t)
	ldap := &recurrentFakeLDAP{}
	f.svc = f.svc.WithLDAP(ldap)
	// Pre-block: every login fails the local arm (counted) AND the
	// directory (binds++). The threshold-th failure arms the block; that
	// request itself still walked the whole chain.
	for i := 0; i < recurrentFailureThreshold; i++ {
		if _, err := f.svc.AuthenticateCredentials(context.Background(), recurrentUser, "wrong"); err == nil {
			t.Fatalf("login %d: want failure", i+1)
		}
	}
	preBlockBinds := ldap.binds
	// Blocked logins: refused before the fallback, bind count frozen.
	for i := 0; i < 3; i++ {
		_, err := f.svc.AuthenticateCredentials(context.Background(), recurrentUser, "wrong")
		wantBlocked(t, err,
			"This request is blocked due to recurrent request failures, please try again in 4 second(s)")
	}
	if ldap.binds != preBlockBinds {
		t.Fatalf("LDAP fallback ran behind the block: %d -> %d binds", preBlockBinds, ldap.binds)
	}
}

// TestRecurrentFailureConcurrent: a mixed storm — two hammered
// usernames, many sprayed singletons below the threshold — must leave
// the ledger internally consistent under -race, with both hammered
// usernames armed at the end and the spray neither arming nor leaking
// into them.
func TestRecurrentFailureConcurrent(t *testing.T) {
	f := newRecurrentFixture(t)
	const (
		storms       = 2
		stormWorkers = 16
		stormRounds  = 10
		sprayWorkers = 16
		sprayRounds  = 4 // below the threshold: pure counters, never armed
	)
	names := []string{recurrentUser, recurrentBystander}
	var wg sync.WaitGroup
	for s := 0; s < storms; s++ {
		for w := 0; w < stormWorkers; w++ {
			wg.Add(1)
			go func(name string) {
				defer wg.Done()
				for i := 0; i < stormRounds; i++ {
					_ = f.attempt(t, name, "wrong")
				}
			}(names[s])
		}
	}
	for w := 0; w < sprayWorkers; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "spray-" + string(rune('a'+n))
			for i := 0; i < sprayRounds; i++ {
				_ = f.attempt(t, name, "wrong")
			}
		}(w)
	}
	wg.Wait()

	for _, name := range names {
		if wait := f.svc.recurrent.blockedRemaining(name); wait <= 0 {
			t.Fatalf("%s not armed after a %d-failure storm", name, stormWorkers*stormRounds)
		}
	}
	f.svc.recurrent.mu.Lock()
	got := len(f.svc.recurrent.entries)
	f.svc.recurrent.mu.Unlock()
	if want := storms + sprayWorkers; got != want {
		t.Fatalf("ledger holds %d entries, want %d (%d stormed + %d sprayed)", got, want, storms, sprayWorkers)
	}
}

// TestRecurrentLimiterSweep: crossing the sweep size drops lapsed
// entries wholesale while keeping the armed one and the fresh count.
func TestRecurrentLimiterSweep(t *testing.T) {
	f := newRecurrentFixture(t)
	l := f.svc.recurrent
	now := l.clock()
	l.mu.Lock()
	for i := 0; i < recurrentSweepSize-1; i++ {
		l.entries["stale-"+string(rune('a'+i%26))+time.Duration(i).String()] = recurrentEntry{
			blockedUntil: now.Add(-time.Minute), // lapsed
		}
	}
	l.entries["armed"] = recurrentEntry{blockedUntil: now.Add(recurrentFailureWindow)}
	l.entries["fresh"] = recurrentEntry{failures: 1}
	l.mu.Unlock()

	l.failure("fresh2") // crosses recurrentSweepSize -> sweeps
	l.mu.Lock()
	_, armedKept := l.entries["armed"]
	_, freshKept := l.entries["fresh"]
	_, fresh2 := l.entries["fresh2"]
	stale := len(l.entries) - 3 // everything beyond the three keepers
	l.mu.Unlock()
	if !armedKept || !freshKept || !fresh2 {
		t.Fatalf("sweep dropped live state (armed=%t fresh=%t fresh2=%t)", armedKept, freshKept, fresh2)
	}
	if stale != 0 {
		t.Fatalf("sweep left %d lapsed entries behind", stale)
	}
}

// TestRecurrentNilLimiterInert: hand-built services (bare &Service{}
// literals, the internal-test pattern) have no ledger — the whole arm
// must stay byte-for-byte the pre-T-632 posture, whatever the failure
// count.
func TestRecurrentNilLimiterInert(t *testing.T) {
	hash, err := HashPassword(recurrentUserPW)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	tokens := recurrentFakeTokens{}
	users := &recurrentFakeUsers{rows: map[string]user{
		recurrentUser: {Username: recurrentUser, PasswordHash: hash, Enabled: true, Provider: ProviderLocal, Role: RoleUser},
	}}
	s := &Service{
		users:    users,
		tokens:   tokens,
		verifier: &TokenVerifier{tokens: tokens, users: users},
		hashGate: newHashGate(1),
		hashVerify: func(_ context.Context, password, encoded string) bool {
			return password == recurrentUserPW && encoded == hash
		},
	}
	for i := 0; i < recurrentFailureThreshold+3; i++ {
		r, rerr := http.NewRequest(http.MethodGet, "/binflow/generic-local/a.bin", nil)
		if rerr != nil {
			t.Fatalf("new request: %v", rerr)
		}
		r.SetBasicAuth(recurrentUser, "wrong")
		_, aerr := s.Authenticate(context.Background(), r)
		if !errors.Is(aerr, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: want the uniform rejection, got %v", i+1, aerr)
		}
		if errors.Is(aerr, ErrRecurrentFailureBlocked) {
			t.Fatalf("attempt %d: blocked without a ledger", i+1)
		}
	}
}
