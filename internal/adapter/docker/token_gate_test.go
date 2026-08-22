// T-204 acceptance surface, arm 1 (T-192 leftover 2): the /v2/token
// form-credential exchange verifies passwords through the gated verifier
// seam — never the pure package function — and the seam is exactly the
// auth service cmd and the httpapi harness already wire, so the token
// endpoint shares the service's one argon2 gate with the Basic arm.
// Package-internal so the probe can assert the recovered collaborator.

package docker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// gateVerifier is a scriptable PasswordVerifier: it records its calls and,
// when blocking, answers only after the request context ended (the
// cancellation test's stand-in for a derivation queued behind a full
// gate).
type gateVerifier struct {
	verdict bool
	err     error
	block   bool // wait for ctx.Done before answering
	calls   atomic.Int32
	sawDone atomic.Int32
}

func (v *gateVerifier) VerifyPassword(ctx context.Context, _, _ string) (bool, error) {
	v.calls.Add(1)
	if v.block {
		<-ctx.Done()
		v.sawDone.Store(1)
		return false, ctx.Err()
	}
	return v.verdict, v.err
}

// authorizerVerifier satisfies both auth.Authorizer and
// adapter.PasswordVerifier — the shape New's capability probe recovers.
type authorizerVerifier struct{ gateVerifier }

func (*authorizerVerifier) Can(context.Context, *auth.Principal, string, string, string) bool {
	return false
}

// seedFormUser builds the user store carrying one enabled local row.
func seedFormUser(name string) *fakeUsers {
	return &fakeUsers{rows: map[string]*metadata.User{
		name: {Username: name, PasswordHash: "$argon2id$v=19$m=8192,t=1,p=1$AA$AA", Enabled: true},
	}}
}

// formTokenRequest drives one POST /v2/token form-credential exchange with
// no header principal — the spelling that reaches authenticateForm.
func formTokenRequest(ctx context.Context, user, pass string) (*http.Request, *captureWriter) {
	req := &http.Request{Method: http.MethodPost,
		URL:    &url.URL{Path: TokenPath, RawQuery: "service=binflow"},
		Header: http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}},
		Body: io.NopCloser(strings.NewReader(
			"grant_type=password&username=" + user + "&password=" + pass + "&service=binflow")),
		RemoteAddr: "127.0.0.1:9",
	}
	if ctx != nil {
		req = req.WithContext(ctx)
	}
	return req, &captureWriter{hdr: http.Header{}}
}

// Table: the exchange's verdict is the wired verifier's verdict, and an
// assembly without the identity service fails the form credential closed
// (never an ungated fallback to the pure argon2 call).
func TestFormCredentialUsesWiredVerifier(t *testing.T) {
	tests := []struct {
		name        string
		verdict     bool
		verifyErr   error
		wantStatus  int
		wantSubject string
	}{
		{"verifier accepts", true, nil, http.StatusOK, "ci"},
		{"verifier rejects", false, nil, http.StatusUnauthorized, ""},
		{"verifier error folds into the uniform rejection", false,
			fmt.Errorf("auth: acquiring hash slot: %w", context.Canceled), http.StatusUnauthorized, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := &fakeTokens{}
			h := New(nil, NewStaticRepoLookup(nil),
				&authorizerVerifier{gateVerifier{verdict: tt.verdict, err: tt.verifyErr}},
				tokens, seedFormUser("ci"),
				Options{AnonymousAccess: false, TokenTTL: time.Hour}, nil)

			req, w := formTokenRequest(context.Background(), "ci", "pw")
			h.ServeHTTP(w, req)

			if w.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", w.status, tt.wantStatus, w.body.String())
			}
			if tt.wantSubject != "" && tokens.subject != tt.wantSubject {
				t.Fatalf("token subject = %q, want %q", tokens.subject, tt.wantSubject)
			}
		})
	}

	t.Run("no verifier wired fails closed", func(t *testing.T) {
		h := New(nil, NewStaticRepoLookup(nil), fakeAuthorizer{}, &fakeTokens{},
			seedFormUser("ci"), Options{AnonymousAccess: false, TokenTTL: time.Hour}, nil)
		if h.verifier != nil {
			t.Fatal("fake authorizer must not satisfy the verifier probe")
		}
		req, w := formTokenRequest(context.Background(), "ci", "pw")
		h.ServeHTTP(w, req)
		if w.status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want the fail-closed 401; body=%s", w.status, w.body.String())
		}
	})
}

// A client that hangs up while its verification waits cancels the context
// the verifier observes — the point of adding ctx to this path (T-172
// D-1's second leg: in-flight login work must notice the disconnect).
func TestFormCredentialCancelReachesVerifier(t *testing.T) {
	v := &authorizerVerifier{}
	v.block = true
	h := New(nil, NewStaticRepoLookup(nil), v, &fakeTokens{}, seedFormUser("ci"),
		Options{AnonymousAccess: false, TokenTTL: time.Hour}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	req, w := formTokenRequest(ctx, "ci", "pw")
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()
	time.Sleep(5 * time.Millisecond) // let the request reach the verifier
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled form exchange did not return within 2s (verifier never saw the ctx)")
	}
	if v.sawDone.Load() != 1 {
		t.Fatal("verifier did not observe the canceled request context")
	}
	if w.status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want the uniform 401", w.status)
	}
}

// New's capability probe recovers the verifier from the identity service
// the real assemblies pass as authz — the same object, hence the same
// gate, as the Basic arm.
func TestNewProbesVerifierFromAuthorizer(t *testing.T) {
	if h := New(nil, NewStaticRepoLookup(nil), fakeAuthorizer{}, nil, nil, Options{}, nil); h.verifier != nil {
		t.Fatal("fake authorizer satisfied the PasswordVerifier probe")
	}

	svc := auth.New(nil, nil, nil, false)
	h := New(nil, NewStaticRepoLookup(nil), svc, nil, nil, Options{}, nil)
	if h.verifier == nil {
		t.Fatal("real auth service as authz did not yield a verifier")
	}
	if h.verifier != adapter.PasswordVerifier(svc) {
		t.Fatal("probed verifier is not the wired auth service instance")
	}
}

// stormPHC derives an argon2id PHC string at reduced storm parameters
// (m=16 MiB, t=1, p=1 — above the m>=8*p spec floor): a real derivation
// on the true verify path, fast enough to storm (T-192's small-parameter
// injection pattern).
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

// Storm through the REAL service: the form exchange must run inside the
// service's gate, so a gate of 1 serializes the storm's derivations. With
// T = one derivation's cost, N concurrent exchanges must take a
// substantial multiple of T; an ungated path runs them in parallel and
// finishes near T. The deterministic bound proof lives in internal/auth —
// this pins the wiring end to end (the request path really is the gated
// one).
func TestFormCredentialStormThroughGate(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	const (
		stormUser = "storm-ci"
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
	h := New(nil, NewStaticRepoLookup(nil), svc, svc, md.Users(),
		Options{AnonymousAccess: false, TokenTTL: time.Hour}, nil)
	if h.verifier == nil {
		t.Fatal("probe did not bind the gated service")
	}

	// Measure one derivation's cost through the exported entry: one
	// throwaway warmup absorbs argon2's first-call init, then the minimum
	// of three runs approximates the storm's per-derivation cost.
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
			req, w := formTokenRequest(ctx, stormUser, stormPW)
			h.ServeHTTP(w, req)
			if w.status != http.StatusOK {
				t.Errorf("storm exchange status = %d, want 200; body=%s",
					w.status, w.body.String())
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("storm of %d form exchanges (gate=1): %s total, single derivation >= %s (%.1fx)",
		n, elapsed, single, float64(elapsed)/float64(single))
	// Gate=1 serializes all n derivations; require well over half of the
	// fully serialized n*single so a parallel ungated run cannot pass (CI
	// load only inflates elapsed, never shrinks it).
	if min := time.Duration(0.6 * float64(n) * float64(single)); elapsed < min {
		t.Fatalf("storm elapsed %s < %s (0.6 x %d x single derivation): derivations ran in parallel, the exchange is not gated",
			elapsed, min, n)
	}
}
