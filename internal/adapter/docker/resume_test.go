package docker

// T-216 (FR-67): the cross-restart resume of a docker blob upload. The
// adapter's session registry is process memory; the [M7] lazy rebuild
// (architecture section 5.3.1) re-materializes a session from the engine's
// persisted state (upload_sessions row + uploads/<id>/data, T-209) on the
// first request that addresses it after a restart. These tests simulate the
// restart exactly as a kill -9 leaves it: the old engine and handler are
// abandoned (NOT closed — the raw crash posture; since ADR-0028/T-229 a
// Close would preserve the sessions too, but abandoning keeps the harsher
// fd-never-closed case pinned), and a fresh engine over the same root plus
// a fresh handler (empty sessionRegistry) serve the follow-up verbs.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- the session-store fake (the metadata.UploadSessionStore seam) ----

// memUploadSessions is the in-memory upload_sessions table shared by every
// "process" of one test: the row survives the simulated restart, which is
// the T-209 persistence contract the lazy rebuild stands on. failGet, when
// set, makes Get report a store fault (the rebuild's 5xx leg).
type memUploadSessions struct {
	mu      sync.Mutex
	rows    map[string]*metadata.UploadSession
	failGet error
}

func newMemUploadSessions() *memUploadSessions {
	return &memUploadSessions{rows: map[string]*metadata.UploadSession{}}
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
	if m.failGet != nil {
		return nil, m.failGet
	}
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
		return fmt.Errorf("mem: set state %s: %w", id, metadata.ErrUploadSessionNotFound)
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

func (m *memUploadSessions) ListExpired(_ context.Context, now string, _ int) ([]*metadata.UploadSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*metadata.UploadSession
	for _, u := range m.rows {
		if u.ExpiresAt <= now {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, nil
}

// backfillExpiry rewrites one row's expires_at directly (the test's sqlite
// "UPDATE upload_sessions SET expires_at = ..." equivalent).
func (m *memUploadSessions) backfillExpiry(id, expiresAt string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.rows[id]; ok {
		u.ExpiresAt = expiresAt
	}
}

// rowCount exposes the table size (the sweep-clean assertions).
func (m *memUploadSessions) rowCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

// ---- the restart harness ----

// resumeHarness is one test's worth of "processes" over a single data root:
// boot() assembles a fresh engine + handler (empty sessionRegistry), which
// is precisely the restarted server's state; the previous engine is
// abandoned, never closed (kill -9 semantics — and since ADR-0028/T-229 a
// Close would preserve the live session, not reclaim it).
type resumeHarness struct {
	t    *testing.T
	root string
	sess *memUploadSessions
	svc  *fakeService

	mu  sync.Mutex
	h   *Handler
	eng storage.Engine
}

func newResumeHarness(t *testing.T) *resumeHarness {
	t.Helper()
	rh := &resumeHarness{
		t:    t,
		root: t.TempDir(),
		sess: newMemUploadSessions(),
		svc:  newFakeService(),
	}
	rh.boot()
	return rh
}

// boot stands up one process: engine over the shared root + shared session
// store, handler with a FRESH sessionRegistry (the restart's empty memory).
func (rh *resumeHarness) boot() {
	rh.t.Helper()
	eng, err := storage.OpenEngine(rh.root, storage.Options{Sessions: rh.sess})
	if err != nil {
		rh.t.Fatalf("storage.OpenEngine: %v", err)
	}
	h := New(nil, NewStaticRepoLookup(map[string]string{"team1": "docker", "plain": "generic"}),
		adminPassAuthorizer{}, nil, nil, Options{AnonymousAccess: true}, nil).
		WithStorage(eng, nil)
	h.svc = rh.svc
	rh.svc.engine = eng
	rh.mu.Lock()
	rh.h, rh.eng = h, eng
	rh.mu.Unlock()
}

// restart simulates the process boundary: a fresh boot, the old engine and
// registry abandoned exactly as a kill -9 leaves them.
func (rh *resumeHarness) restart() { rh.boot() }

// serve runs one request through the CURRENT process's handler.
func (rh *resumeHarness) serve(method, path string, body io.Reader, hdr map[string]string) *http.Response {
	rh.t.Helper()
	rh.mu.Lock()
	h := rh.h
	rh.mu.Unlock()
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Result()
}

// startUpload drives the initiating POST and returns the upload URL + uuid.
func (rh *resumeHarness) startUpload(name string) (loc, uuid string) {
	rh.t.Helper()
	resp := rh.serve(http.MethodPost, "/v2/"+name+"/blobs/uploads/", nil, nil)
	body := readBody(rh.t, resp)
	if resp.StatusCode != http.StatusAccepted {
		rh.t.Fatalf("start upload status = %d body=%s", resp.StatusCode, body)
	}
	return resp.Header.Get("Location"), resp.Header.Get("Docker-Upload-UUID")
}

// uploadsDir reports the on-disk path of one session's data directory (the
// sweep-clean assertions look here).
func (rh *resumeHarness) uploadsDir(id string) string {
	return rh.root + "/uploads/" + id
}

// ---- the resume chain ----

// TestUploadResumeAcrossRestart is the FR-67 core chain (probe V15 shape):
// partial PATCH, process boundary, then offset query (the resume premise),
// continuation PATCH, digest finalize, and a bitwise-identical read back.
func TestUploadResumeAcrossRestart(t *testing.T) {
	rh := newResumeHarness(t)
	chunk1 := bytes.Repeat([]byte("resume-chunk-one-"), 32768) // 512KiB
	chunk2 := bytes.Repeat([]byte("tail-chunk-two!"), 4096)    // 64KiB
	whole := append(append([]byte{}, chunk1...), chunk2...)
	dgst := "sha256:" + sha256Hex(whole)

	loc, uuid := rh.startUpload("team1/app")

	resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(chunk1),
		map[string]string{"Content-Range": fmt.Sprintf("0-%d", len(chunk1)-1)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH chunk1 status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(chunk1)-1) {
		t.Fatalf("PATCH chunk1 Range = %q", got)
	}

	rh.restart() // kill -9: registry gone, row + data file survive

	// The offset query is the resume premise: 204 + the re-derived offset.
	resp = rh.serve(http.MethodGet, loc, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("GET after restart status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(chunk1)-1) {
		t.Fatalf("GET after restart Range = %q, want 0-%d", got, len(chunk1)-1)
	}
	if got := resp.Header.Get("Docker-Upload-UUID"); got != uuid {
		t.Fatalf("GET after restart Docker-Upload-UUID = %q, want %q", got, uuid)
	}

	// The continuation PATCH anchors at the re-derived offset.
	resp = rh.serve(http.MethodPatch, loc, bytes.NewReader(chunk2),
		map[string]string{"Content-Range": fmt.Sprintf("%d-%d", len(chunk1), len(whole)-1)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH chunk2 status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(whole)-1) {
		t.Fatalf("PATCH chunk2 Range = %q, want 0-%d", got, len(whole)-1)
	}

	resp = rh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT finalize status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}

	resp = rh.serve(http.MethodGet, "/v2/team1/app/blobs/"+dgst, nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET blob status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(got, whole) {
		t.Fatalf("blob read back differs bitwise: got %d bytes, want %d", len(got), len(whole))
	}
}

// TestUploadResumeMisalignedContentRange416 pins the misalignment leg after
// a restart (docker-registry.md 2.2#3 / 2.5#2): a wrong Content-Range start
// answers 416 with an EMPTY body and the authoritative Range header, and
// the session survives the refused attempt.
func TestUploadResumeMisalignedContentRange416(t *testing.T) {
	rh := newResumeHarness(t)
	chunk1 := bytes.Repeat([]byte("anchor-me-"), 1024) // 10KiB
	loc, _ := rh.startUpload("team1/app")
	resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(chunk1),
		map[string]string{"Content-Range": fmt.Sprintf("0-%d", len(chunk1)-1)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH chunk1 status = %d", resp.StatusCode)
	}

	rh.restart()

	// Client claims the server is at 0 (a restart-from-zero push) while the
	// session holds len(chunk1) bytes: anchor mismatch.
	resp = rh.serve(http.MethodPatch, loc, bytes.NewReader(chunk1),
		map[string]string{"Content-Range": fmt.Sprintf("0-%d", len(chunk1)-1)})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("misaligned PATCH status = %d body=%s", resp.StatusCode, body)
	}
	if len(body) != 0 {
		t.Fatalf("416 body = %q, want empty", body)
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(chunk1)-1) {
		t.Fatalf("416 Range = %q, want the authoritative 0-%d", got, len(chunk1)-1)
	}
	if got := resp.Header.Get("Location"); got != loc {
		t.Fatalf("416 Location = %q, want %q", got, loc)
	}

	// The refused attempt poisoned nothing: the correctly anchored PATCH
	// still lands (the client re-anchored using the 416's Range).
	chunk2 := []byte("tail")
	dgst := "sha256:" + sha256Hex(append(append([]byte{}, chunk1...), chunk2...))
	resp = rh.serve(http.MethodPatch, loc, bytes.NewReader(chunk2),
		map[string]string{"Content-Range": fmt.Sprintf("%d-%d", len(chunk1), len(chunk1)+len(chunk2)-1)})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("re-anchored PATCH status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	resp = rh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("PUT finalize status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
}

// TestUploadResumePutAndDeleteAfterRestart pins contract 2: PUT finalize and
// DELETE cancel are immediately executable on a rebuilt session — a client
// that restarts may collect its upload with either verb without any PATCH.
func TestUploadResumePutAndDeleteAfterRestart(t *testing.T) {
	t.Run("PUT finalize without a post-restart PATCH", func(t *testing.T) {
		rh := newResumeHarness(t)
		content := []byte("finalize me straight from the restart")
		loc, uuid := rh.startUpload("team1/app")
		_ = uuid
		if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
			t.Fatalf("PATCH status = %d", resp.StatusCode)
		}
		rh.restart()
		dgst := "sha256:" + sha256Hex(content)
		resp := rh.serve(http.MethodPut, loc+"?digest="+dgst, nil, nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT finalize status = %d body=%s", resp.StatusCode, readBody(t, resp))
		}
	})

	t.Run("DELETE cancel", func(t *testing.T) {
		rh := newResumeHarness(t)
		content := []byte("cancel me after the restart")
		loc, uuid := rh.startUpload("team1/app")
		if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
			t.Fatalf("PATCH status = %d", resp.StatusCode)
		}
		rh.restart()
		resp := rh.serve(http.MethodDelete, loc, nil, nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DELETE status = %d body=%s", resp.StatusCode, readBody(t, resp))
		}
		// The cancel reclaimed row and data: every verb now sees unknown.
		if rh.sess.rowCount() != 0 {
			t.Fatalf("session row survived the cancel: %d rows", rh.sess.rowCount())
		}
		if _, err := os.Stat(rh.uploadsDir(uuid)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("session dir survived the cancel: stat err = %v", err)
		}
		if resp := rh.serve(http.MethodGet, loc, nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET after cancel status = %d", resp.StatusCode)
		}
	})
}

// TestUploadResumeUnknownSession keeps the unknown-id posture: a uuid that
// never existed (or an assembly without the storage seam) answers 404
// BLOB_UPLOAD_UNKNOWN on every session verb — the rebuild must not invent
// sessions.
func TestUploadResumeUnknownSession(t *testing.T) {
	rh := newResumeHarness(t)
	loc := "/v2/team1/app/blobs/uploads/00000000-0000-0000-0000-000000000000"
	for _, verb := range []string{http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		resp := rh.serve(verb, loc, strings.NewReader("x"), nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s unknown session status = %d body=%s", verb, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), ErrCodeBlobUploadUnknown) {
			t.Fatalf("%s unknown session code: body=%s", verb, body)
		}
	}

	// The bare assembly (no storage seam wired) keeps the same posture.
	h := New(nil, NewStaticRepoLookup(map[string]string{"team1": "docker"}),
		adminPassAuthorizer{}, nil, nil, Options{AnonymousAccess: true}, nil)
	h.svc = newFakeService()
	req := httptest.NewRequest(http.MethodGet, loc, nil)
	req = req.WithContext(adapter.WithPrincipal(req.Context(), &auth.Principal{Name: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("bare assembly GET status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), ErrCodeBlobUploadUnknown) {
		t.Fatalf("bare assembly GET body = %s", rec.Body.String())
	}
}

// TestUploadResumeExpiredFailClosed pins contract 3 (expiry = unknown): an
// expired-but-not-yet-swept row must fail closed through the rebuild. Two
// legs: the row expired AFTER the boot (ResumeSession's own check answers)
// and the row expired BEFORE the boot (the startup sweep reclaims row and
// directory, T-209 semantics; the verbs then see unknown).
func TestUploadResumeExpiredFailClosed(t *testing.T) {
	patchOne := func(t *testing.T) (*resumeHarness, string, string) {
		rh := newResumeHarness(t)
		content := []byte("about to expire")
		loc, uuid := rh.startUpload("team1/app")
		if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
			t.Fatalf("PATCH status = %d", resp.StatusCode)
		}
		return rh, loc, uuid
	}
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)

	t.Run("expired row survives boot, resume refuses", func(t *testing.T) {
		rh, loc, uuid := patchOne(t)
		rh.restart() // row not yet expired at boot: the sweep leaves it
		rh.sess.backfillExpiry(uuid, past)
		for _, verb := range []string{http.MethodGet, http.MethodPatch} {
			resp := rh.serve(verb, loc, nil, nil)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), ErrCodeBlobUploadUnknown) {
				t.Fatalf("%s expired session: status=%d body=%s", verb, resp.StatusCode, body)
			}
		}
	})

	t.Run("expired before boot: sweep clears row and dir", func(t *testing.T) {
		rh, loc, uuid := patchOne(t)
		rh.sess.backfillExpiry(uuid, past)
		rh.restart() // the boot's sweep reclaims the expired row + dir
		if n := rh.sess.rowCount(); n != 0 {
			t.Fatalf("expired row survived the boot sweep: %d rows", n)
		}
		if _, err := os.Stat(rh.uploadsDir(uuid)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("session dir survived the boot sweep: stat err = %v", err)
		}
		if resp := rh.serve(http.MethodGet, loc, nil, nil); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET swept session status = %d", resp.StatusCode)
		}
	})
}

// TestUploadResumeStoreFailureNotPoisoned pins the store-failure leg: a
// broken rebuild answers 5xx WITHOUT caching anything — the next request
// re-enters the rebuild and succeeds once the store heals.
func TestUploadResumeStoreFailureNotPoisoned(t *testing.T) {
	rh := newResumeHarness(t)
	content := []byte("store fault then heal")
	loc, _ := rh.startUpload("team1/app")
	if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}
	rh.restart()

	rh.sess.mu.Lock()
	rh.sess.failGet = errors.New("injected store outage")
	rh.sess.mu.Unlock()
	resp := rh.serve(http.MethodGet, loc, nil, nil)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("GET during store outage status = %d body=%s", resp.StatusCode, body)
	}

	rh.sess.mu.Lock()
	rh.sess.failGet = nil
	rh.sess.mu.Unlock()
	resp = rh.serve(http.MethodGet, loc, nil, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("GET after healing status = %d body=%s", resp.StatusCode, readBody(t, resp))
	}
	if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(content)-1) {
		t.Fatalf("healed GET Range = %q", got)
	}
}

// gatedResumeEngine wraps an engine's ResumeSession with a call counter and
// a start gate: every call blocks until the gate opens, so the single-flight
// test can hold the flyer mid-rebuild while the losing requests pile up.
type gatedResumeEngine struct {
	storage.Engine
	gate  chan struct{}
	calls atomic.Int32
}

func (e *gatedResumeEngine) ResumeSession(ctx context.Context, id string) (storage.Session, error) {
	e.calls.Add(1)
	<-e.gate
	return e.Engine.ResumeSession(ctx, id)
}

// TestUploadResumeSingleFlight pins the funnel: when concurrent first
// requests race to rebuild the same id, EXACTLY ONE engine ResumeSession
// happens (the engine resolves same-id resumes last-writer-wins; the loser
// of an unfunneled race would surface an engine-internal "already
// finalized" as a 500) and every request answers from the one session.
func TestUploadResumeSingleFlight(t *testing.T) {
	rh := newResumeHarness(t)
	content := []byte("one rebuild to serve them all")
	loc, _ := rh.startUpload("team1/app")
	if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}

	// Restart, then hand the fresh process's handler the engine through the
	// counting gate (the registry is empty again — the racers all miss).
	rh.restart()
	gated := &gatedResumeEngine{Engine: rh.eng, gate: make(chan struct{})}
	rh.mu.Lock()
	rh.h.store = gated
	rh.mu.Unlock()

	const racers = 8
	var wg sync.WaitGroup
	responses := make([]*http.Response, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			responses[i] = rh.serve(http.MethodGet, loc, nil, nil)
		}(i)
	}
	// Let the loser requests pile up on the funnel, then release the flyer.
	time.Sleep(50 * time.Millisecond)
	close(gated.gate)
	wg.Wait()

	if n := gated.calls.Load(); n != 1 {
		t.Fatalf("engine ResumeSession calls = %d, want exactly 1", n)
	}
	for i, resp := range responses {
		if resp == nil {
			t.Fatalf("racer %d: no response", i)
		}
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("racer %d status = %d body=%s", i, resp.StatusCode, readBody(t, resp))
		}
		if got := resp.Header.Get("Range"); got != fmt.Sprintf("0-%d", len(content)-1) {
			t.Fatalf("racer %d Range = %q, want 0-%d", i, got, len(content)-1)
		}
	}
}

// hardNoResumeEngine mirrors the S3Engine contract (pinned engine-side by
// storage's TestS3ResumeSessionNotSupported): ResumeSession is permanently
// ErrSessionNotFound — multipart state lives in the S3 service, not in any
// local table. The adapter's half of contract 5: such a backend keeps the
// hard 404 across a restart.
type hardNoResumeEngine struct{ storage.Engine }

func (hardNoResumeEngine) ResumeSession(_ context.Context, id string) (storage.Session, error) {
	return nil, fmt.Errorf("s3-like backend: resume %s: %w", id, storage.ErrSessionNotFound)
}

// TestUploadResumeS3BackendStays404 pins contract 5's adapter side: after a
// restart on a backend whose ResumeSession is permanently not-found, every
// session verb answers 404 BLOB_UPLOAD_UNKNOWN (the pre-T-216 posture is
// the S3 posture, unchanged).
func TestUploadResumeS3BackendStays404(t *testing.T) {
	rh := newResumeHarness(t)
	content := []byte("s3 has no local resume")
	loc, _ := rh.startUpload("team1/app")
	if resp := rh.serve(http.MethodPatch, loc, bytes.NewReader(content), nil); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("PATCH status = %d", resp.StatusCode)
	}

	// The restarted process assembles the S3-shaped backend over the same
	// registry-less starting point.
	eng, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil, NewStaticRepoLookup(map[string]string{"team1": "docker"}),
		adminPassAuthorizer{}, nil, nil, Options{AnonymousAccess: true}, nil).
		WithStorage(hardNoResumeEngine{eng}, nil)
	h.svc = newFakeService()
	rh.mu.Lock()
	rh.h = h
	rh.mu.Unlock()

	for _, verb := range []string{http.MethodGet, http.MethodPatch, http.MethodPut, http.MethodDelete} {
		resp := rh.serve(verb, loc, strings.NewReader("x"), nil)
		body := readBody(t, resp)
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), ErrCodeBlobUploadUnknown) {
			t.Fatalf("%s on S3-shaped backend: status=%d body=%s", verb, resp.StatusCode, body)
		}
	}
}
