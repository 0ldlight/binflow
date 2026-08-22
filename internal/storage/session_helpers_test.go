package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// memUploadSessions is an in-memory metadata.UploadSessionStore for tests. It
// models the sqlite store's semantics without a real database: Create fails
// on a duplicate id, Get/SetState/Delete surface ErrUploadSessionNotFound on
// a missing id, ListExpired orders by expires_at then id.
type memUploadSessions struct {
	mu   sync.Mutex
	rows map[string]*metadata.UploadSession
}

func newMemUploadSessions() *memUploadSessions {
	return &memUploadSessions{rows: make(map[string]*metadata.UploadSession)}
}

func (m *memUploadSessions) Create(_ context.Context, u *metadata.UploadSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[u.ID]; ok {
		return fmt.Errorf("mem: session %s already exists", u.ID)
	}
	cp := *u
	m.rows[u.ID] = &cp
	return nil
}

func (m *memUploadSessions) Get(_ context.Context, id string) (*metadata.UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.rows[id]
	if !ok {
		return nil, fmt.Errorf("mem: get %s: %w", id, metadata.ErrUploadSessionNotFound)
	}
	cp := *u
	return &cp, nil
}

func (m *memUploadSessions) SetState(_ context.Context, id, state string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.rows[id]
	if !ok {
		return fmt.Errorf("mem: set-state %s: %w", id, metadata.ErrUploadSessionNotFound)
	}
	u.State = state
	return nil
}

func (m *memUploadSessions) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return fmt.Errorf("mem: delete %s: %w", id, metadata.ErrUploadSessionNotFound)
	}
	delete(m.rows, id)
	return nil
}

func (m *memUploadSessions) ListExpired(_ context.Context, now string, limit int) ([]*metadata.UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*metadata.UploadSession
	for _, u := range m.rows {
		if u.ExpiresAt <= now {
			out = append(out, u)
		}
	}
	// Sort by expires_at then id for deterministic batches (stable order).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].ExpiresAt < out[i].ExpiresAt ||
				(out[j].ExpiresAt == out[i].ExpiresAt && out[j].ID < out[i].ID) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// countRows returns the number of rows, for state-integrity assertions.
func (m *memUploadSessions) countRows() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

// seedSessionRow fabricates a crash-residue row: an expired-by-age session row
// referencing id, as a crashed BeginSession/Append would have left it. The data
// length drives the state's received counter (bookkeeping only).
func seedSessionRow(t *testing.T, store *memUploadSessions, id string, data []byte) {
	t.Helper()
	now := time.Now()
	st := sessionState{Version: 1, ID: id, CreatedAt: now, Received: int64(len(data))}
	if err := store.Create(context.Background(), &metadata.UploadSession{
		ID:        id,
		State:     marshalSessionState(st),
		CreatedAt: now.UTC().Format(time.RFC3339),
		ExpiresAt: now.UTC().Format(time.RFC3339), // already expired
	}); err != nil {
		t.Fatalf("seed session row %s: %v", id, err)
	}
}

// expireAllRows rewinds every row's expires_at by age so ListExpired returns it.
func expireAllRows(t *testing.T, store *memUploadSessions, age time.Duration) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	past := time.Now().Add(-age)
	for id, u := range store.rows {
		u.ExpiresAt = past.UTC().Format(time.RFC3339)
		_ = id
	}
}
