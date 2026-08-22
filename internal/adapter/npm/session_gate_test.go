// T-204 acceptance surface, arm 1 (T-192 leftover 2): the couch login's
// body-credential verification runs through the gated verifier seam —
// never the pure package function — and the seam is exactly the auth
// service the real assemblies pass as the token registry, so npm login
// shares the service's one argon2 gate with the Basic arm.
// Package-internal so the probe can assert the recovered collaborator.

package npm

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"

	"golang.org/x/crypto/argon2"
)

// gateRegistry is a TokenRegistry that is ALSO a PasswordVerifier: the
// shape WithAuth's capability probe recovers. verdict/verifyErr script the
// verification; block makes the call wait for the request context to end
// (the cancellation test's stand-in for a derivation queued behind a full
// gate).
type gateRegistry struct {
	issued    atomic.Int32
	verdict   bool
	verifyErr error
	block     bool
	sawDone   atomic.Int32
}

func (g *gateRegistry) Issue(_ context.Context, username string, ttl time.Duration) (*auth.IssuedToken, error) {
	g.issued.Add(1)
	return &auth.IssuedToken{AccessToken: "tok-" + username, TokenType: "Bearer",
		ExpiresIn: int64(ttl.Seconds()), Username: username}, nil
}

func (g *gateRegistry) Verify(context.Context, string) (*auth.Principal, error) {
	return nil, errors.New("not implemented in fake")
}

func (g *gateRegistry) Revoke(context.Context, string) error    { return nil }
func (g *gateRegistry) RevokeByID(context.Context, int64) error { return nil }

func (g *gateRegistry) VerifyPassword(ctx context.Context, _, _ string) (bool, error) {
	if g.block {
		<-ctx.Done()
		g.sawDone.Store(1)
		return false, ctx.Err()
	}
	return g.verdict, g.verifyErr
}

// plainRegistry is a TokenRegistry WITHOUT the PasswordVerifier capability
// (the pre-T-204 fake shape).
type plainRegistry struct{ issued atomic.Int32 }

func (p *plainRegistry) Issue(_ context.Context, username string, _ time.Duration) (*auth.IssuedToken, error) {
	p.issued.Add(1)
	return &auth.IssuedToken{AccessToken: "tok-" + username, TokenType: "Bearer"}, nil
}

func (p *plainRegistry) Verify(context.Context, string) (*auth.Principal, error) {
	return nil, errors.New("not implemented in fake")
}

func (p *plainRegistry) Revoke(context.Context, string) error    { return nil }
func (p *plainRegistry) RevokeByID(context.Context, int64) error { return nil }

// userDir is the minimal UserDirectory carrying one enabled local row.
type userDir struct{ name, hash string }

func (d userDir) Get(_ context.Context, name string) (*metadata.User, error) {
	if name == d.name {
		return &metadata.User{Username: d.name, PasswordHash: d.hash, Enabled: true}, nil
	}
	return nil, metadata.ErrUserNotFound
}

// couchLoginRequest builds one PUT couch login with the body credential
// pair and no header principal, returning the request and its recorder.
func couchLoginRequest(ctx context.Context, name, password string) (*http.Request, *httptest.ResponseRecorder) {
	body := fmt.Sprintf(`{"_id":"org.couchdb.user:%s","name":%q,"password":%q,"type":"user","roles":[]}`,
		name, name, password)
	req := httptest.NewRequest(http.MethodPut,
		"http://registry.test/npm-local/-/user/org.couchdb.user:"+name,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	return req, httptest.NewRecorder()
}

// Table: the login verdict is the wired verifier's verdict, and an
// assembly whose registry is not the identity service fails the body
// credential closed (never an ungated fallback).
func TestCouchLoginUsesWiredVerifier(t *testing.T) {
	const (
		name = "devel"
		hash = "$argon2id$v=19$m=8192,t=1,p=1$AA$AA"
	)
	tests := []struct {
		name       string
		verdict    bool
		verifyErr  error
		wantStatus int
		wantIssued int32
	}{
		{"verifier accepts", true, nil, http.StatusCreated, 1},
		{"verifier rejects", false, nil, http.StatusUnauthorized, 0},
		{"verifier error folds into the uniform rejection", false,
			fmt.Errorf("auth: acquiring hash slot: %w", context.Canceled), http.StatusUnauthorized, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &gateRegistry{verdict: tt.verdict, verifyErr: tt.verifyErr}
			h := New(nil, nil, Options{BaseURL: "http://registry.test"}).
				WithAuth(reg, userDir{name, hash}, nil)

			req, rr := couchLoginRequest(context.Background(), name, "pw")
			h.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tt.wantStatus, rr.Body.String())
			}
			if got := reg.issued.Load(); got != tt.wantIssued {
				t.Fatalf("tokens issued = %d, want %d", got, tt.wantIssued)
			}
			if tt.wantIssued == 1 {
				var resp map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
					t.Fatalf("login body: %v", err)
				}
				if resp["ok"] != true || resp["id"] != "org.couchdb.user:"+name {
					t.Fatalf("login body = %v, want ok=true with the couch id", resp)
				}
			}
		})
	}

	t.Run("registry without the capability fails closed", func(t *testing.T) {
		plain := &plainRegistry{}
		h := New(nil, nil, Options{}).WithAuth(plain, userDir{name, hash}, nil)
		if h.verifier != nil {
			t.Fatal("plain registry satisfied the verifier probe")
		}
		req, rr := couchLoginRequest(context.Background(), name, "pw")
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want the fail-closed 401; body=%s", rr.Code, rr.Body.String())
		}
		if got := plain.issued.Load(); got != 0 {
			t.Fatalf("tokens issued = %d, want 0 (no token without a verification)", got)
		}
	})
}

// A client that hangs up while its verification waits cancels the context
// the verifier observes — the point of adding ctx to this path.
func TestCouchLoginCancelReachesVerifier(t *testing.T) {
	reg := &gateRegistry{verdict: true}
	reg.block = true
	h := New(nil, nil, Options{}).WithAuth(reg,
		userDir{"devel", "$argon2id$v=19$m=8192,t=1,p=1$AA$AA"}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	req, rr := couchLoginRequest(ctx, "devel", "pw")
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()
	time.Sleep(5 * time.Millisecond) // let the request reach the verifier
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled login did not return within 2s (verifier never saw the ctx)")
	}
	if reg.sawDone.Load() != 1 {
		t.Fatal("verifier did not observe the canceled request context")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the uniform 401", rr.Code)
	}
}

// WithAuth's capability probe recovers the verifier from the identity
// service the real assemblies pass as the token registry — the same
// object, hence the same gate, as the Basic arm.
func TestWithAuthProbesVerifierFromRegistry(t *testing.T) {
	svc := auth.New(nil, nil, nil, false)
	h := New(nil, nil, Options{}).WithAuth(svc, nil, nil)
	if h.verifier == nil {
		t.Fatal("real auth service as the registry did not yield a verifier")
	}
	if h.verifier != adapter.PasswordVerifier(svc) {
		t.Fatal("probed verifier is not the wired auth service instance")
	}
}

// stormPHC derives an argon2id PHC string at reduced storm parameters
// (m=16 MiB, t=1, p=1): a real derivation on the true verify path, fast
// enough to storm (T-192's small-parameter injection pattern).
func stormPHC(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("salt: %v", err)
	}
	const kiB = 16 * 1024
	tag := argon2.IDKey([]byte(password), salt, 1, kiB, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=1$%s$%s", kiB, 1,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag))
}

// Storm through the REAL service: the couch login must run inside the
// service's gate, so a gate of 1 serializes the storm's derivations (see
// the docker twin for the timing rationale; the deterministic bound proof
// lives in internal/auth).
func TestCouchLoginStormThroughGate(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	const (
		stormUser = "storm-dev"
		stormPW   = "storm-pw" //nolint:gosec // throwaway test fixture password
	)
	hash := stormPHC(t, stormPW)
	now := metadata.Now()
	if err := md.Users().Create(ctx, &metadata.User{
		Username: stormUser, PasswordHash: hash, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed storm user: %v", err)
	}

	svc := auth.NewFromStore(md, false).WithHashConcurrency(1)
	h := New(nil, nil, Options{}).WithAuth(svc, md.Users(), nil)
	if h.verifier == nil {
		t.Fatal("probe did not bind the gated service")
	}

	// Measure one derivation's cost through the exported entry (one
	// throwaway warmup absorbs argon2's first-call init).
	if ok, verr := svc.VerifyPassword(ctx, stormPW, hash); verr != nil || !ok {
		t.Fatalf("warmup verify = (%v, %v), want (true, nil)", ok, verr)
	}
	single := time.Duration(1 << 62)
	for i := 0; i < 3; i++ {
		start := time.Now()
		ok, verr := svc.VerifyPassword(ctx, stormPW, hash)
		if verr != nil || !ok {
			t.Fatalf("measure verify #%d = (%v, %v), want (true, nil)", i, ok, verr)
		}
		if el := time.Since(start); el < single {
			single = el
		}
	}

	const n = 6
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPut,
				"http://registry.test/npm-local/-/user/org.couchdb.user:"+stormUser,
				strings.NewReader(fmt.Sprintf(
					`{"_id":"org.couchdb.user:%s","name":%q,"password":%q}`, stormUser, stormUser, stormPW)))
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != http.StatusCreated {
				t.Errorf("storm login status = %d, want 201; body=%s", rr.Code, rr.Body.String())
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("storm of %d couch logins (gate=1): %s total, single derivation >= %s (%.1fx)",
		n, elapsed, single, float64(elapsed)/float64(single))
	if min := time.Duration(0.6 * float64(n) * float64(single)); elapsed < min {
		t.Fatalf("storm elapsed %s < %s (0.6 x %d x single derivation): derivations ran in parallel, the login is not gated",
			elapsed, min, n)
	}
}
