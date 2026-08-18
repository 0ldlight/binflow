package docker

// T-54 regression: the anonymous-token subject seeding dedupes PER HANDLER,
// not per process. The process-wide sync.Once this replaces cached the first
// assembly's success across `go test -count>1` iterations — every later
// harness built on a fresh database then 500'd anonymous token issuance
// because its own store never got the subject row. Also pins that a failed
// seed is not cached: the next request retries instead of staying broken for
// the handler's lifetime.

import (
	"context"
	"errors"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// fakeUserStore is a minimal anonymousSubjectSeed double.
type fakeUserStore struct {
	users  map[string]*metadata.User
	getErr error // injected lookup failure (busy-shaped, one shot)
}

func newFakeUserStore() *fakeUserStore { return &fakeUserStore{users: map[string]*metadata.User{}} }

func (s *fakeUserStore) Get(_ context.Context, username string) (*metadata.User, error) {
	if s.getErr != nil {
		err := s.getErr
		s.getErr = nil // one-shot: the retry must observe a healthy store
		return nil, err
	}
	u, ok := s.users[username]
	if !ok {
		return nil, metadata.ErrUserNotFound
	}
	return u, nil
}

func (s *fakeUserStore) Create(_ context.Context, u *metadata.User) error {
	s.users[u.Username] = u
	return nil
}

func TestSeedAnonymousOncePerHandlerIsolation(t *testing.T) {
	ctx := context.Background()

	// Handler A seeds successfully against its own store.
	storeA := newFakeUserStore()
	hA := &Handler{}
	if err := hA.seedAnonymousOnce(ctx, storeA); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	if _, ok := storeA.users[anonymousSubject]; !ok {
		t.Fatal("subject row missing from handler A's store")
	}

	// Handler B on a FRESH store must seed ITS OWN row — the process-wide
	// cache used to skip this and B's token issuance 500'd (T-54).
	storeB := newFakeUserStore()
	hB := &Handler{}
	if err := hB.seedAnonymousOnce(ctx, storeB); err != nil {
		t.Fatalf("second handler's seed: %v", err)
	}
	if _, ok := storeB.users[anonymousSubject]; !ok {
		t.Fatal("subject row missing from handler B's store: per-process cache leak")
	}

	// Success caches per handler: no re-seed churn on later requests.
	n := len(storeA.users)
	for i := 0; i < 3; i++ {
		if err := hA.seedAnonymousOnce(ctx, storeA); err != nil {
			t.Fatalf("cached seed call %d: %v", i, err)
		}
	}
	if len(storeA.users) != n {
		t.Fatalf("row count changed across cached calls: %d -> %d", n, len(storeA.users))
	}

	// Failure is NOT cached: a transient store error self-heals next call.
	storeC := newFakeUserStore()
	storeC.getErr = errors.New("database is locked (5) (SQLITE_BUSY)")
	hC := &Handler{}
	if err := hC.seedAnonymousOnce(ctx, storeC); err == nil {
		t.Fatal("injected lookup failure unexpectedly succeeded")
	}
	if err := hC.seedAnonymousOnce(ctx, storeC); err != nil {
		t.Fatalf("retry after transient failure: %v", err)
	}
	if _, ok := storeC.users[anonymousSubject]; !ok {
		t.Fatal("subject row missing after self-healed retry")
	}

	// nil store stays the honest assembly error.
	if err := (&Handler{}).seedAnonymousOnce(ctx, nil); err == nil {
		t.Fatal("nil seed must fail")
	}
}
