package httpapi_test

// T-323R tests on the T-332 wire: the /api/v1/uploads plane's restart
// visibility under the token-addressed endpoints. The crash model is server
// replacement — a SECOND full stack over a seam whose persisted rows
// survive, exactly what a kill -9 leaves behind: the engine's
// upload_sessions row (with the caller blob carrying the token binding)
// and the server-side upload state survive, the plane's in-process registry
// does not. The seam fake carries the caller-blob rows the way the real
// S3Engine does (verbatim both ways) and relays each Commit into the
// CURRENT stack's real engine, so the finish task's assembly puts the blob
// exactly where the client's checksum-deploy PUT looks for it.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/storage"
)

// fakeMPUContextRow is one persisted session row: the engine session's
// durable bytes plus the caller blob the context begin wrote.
type fakeMPUContextRow struct {
	buf    []byte
	caller []byte
	done   bool
}

// fakeMPUContextSeam stands in for *storage.S3Engine on BOTH stacks: it
// implements MultipartUploads and MultipartUploadContexts over rows that
// outlive any one harness, and its Commit relays into the current stack's
// engine (setEngine re-points it when the "restarted" stack is built).
type fakeMPUContextSeam struct {
	mu      sync.Mutex
	eng     storage.Engine
	rows    map[string]*fakeMPUContextRow
	begins  atomic.Int64
	resumes atomic.Int64
}

func newFakeMPUContextSeam() *fakeMPUContextSeam {
	return &fakeMPUContextSeam{rows: map[string]*fakeMPUContextRow{}}
}

// setEngine re-points the commit relay at the restarted stack's engine.
func (f *fakeMPUContextSeam) setEngine(eng storage.Engine) {
	f.mu.Lock()
	f.eng = eng
	f.mu.Unlock()
}

// lockRow fetches one row under the seam lock; ok=false is ErrSessionNotFound.
func (f *fakeMPUContextSeam) lockRow(id string) (*fakeMPUContextRow, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok || row.done {
		return nil, false
	}
	return row, true
}

func (f *fakeMPUContextSeam) BeginMultipartSessionContext(_ context.Context, _ int64, caller []byte) (storage.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(caller) == 0 {
		return nil, fmt.Errorf("fake seam: caller context required")
	}
	f.begins.Add(1)
	id := fmt.Sprintf("ctx-mpu-%d", f.begins.Load())
	f.rows[id] = &fakeMPUContextRow{caller: append([]byte(nil), caller...)}
	return &fakeMPUContextSession{seam: f, id: id}, nil
}

func (f *fakeMPUContextSeam) BeginMultipartSession(_ context.Context, _ int64) (storage.Session, error) {
	// The plain begin writes no caller blob — the pre-T-323R row shape.
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins.Add(1)
	id := fmt.Sprintf("plain-mpu-%d", f.begins.Load())
	f.rows[id] = &fakeMPUContextRow{}
	return &fakeMPUContextSession{seam: f, id: id}, nil
}

func (f *fakeMPUContextSeam) ResumeSessionContext(_ context.Context, id string) (storage.Session, []byte, error) {
	f.mu.Lock()
	row, ok := f.rows[id]
	var caller []byte
	if ok {
		caller = row.caller
	}
	f.mu.Unlock()
	f.resumes.Add(1)
	if !ok || row.done {
		return nil, nil, fmt.Errorf("fake seam: resume %s: %w", id, storage.ErrSessionNotFound)
	}
	if len(caller) == 0 {
		// The engine contract: the context lane never adopts a caller-less
		// row, and the refusal must leave the row untouched.
		return nil, nil, fmt.Errorf("fake seam: resume %s: %w: no caller context", id, storage.ErrSessionNotFound)
	}
	return &fakeMPUContextSession{seam: f, id: id}, caller, nil
}

// fakeMPUContextSession is the seam's session handle: a thin view over the
// row's durable buffer (what the real engine's Offset reports after a
// resume: the last flushed boundary).
type fakeMPUContextSession struct {
	seam *fakeMPUContextSeam
	id   string
}

func (s *fakeMPUContextSession) ID() string { return s.id }

func (s *fakeMPUContextSession) Offset() int64 {
	row, ok := s.seam.lockRow(s.id)
	if !ok {
		return 0
	}
	return int64(len(row.buf))
}

func (s *fakeMPUContextSession) Append(_ context.Context, r io.Reader) (int64, error) {
	n, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.seam.mu.Lock()
	defer s.seam.mu.Unlock()
	row, ok := s.seam.rows[s.id]
	if !ok || row.done {
		return 0, fmt.Errorf("session %s already finalized", s.id)
	}
	row.buf = append(row.buf, n...)
	return int64(len(row.buf)), nil
}

func (s *fakeMPUContextSession) Commit(ctx context.Context, expect storage.BlobRef) (storage.BlobRef, error) {
	s.seam.mu.Lock()
	row, ok := s.seam.rows[s.id]
	if !ok || row.done {
		s.seam.mu.Unlock()
		return storage.BlobRef{}, fmt.Errorf("session %s already finalized", s.id)
	}
	row.done = true
	payload := row.buf
	delete(s.seam.rows, s.id)
	eng := s.seam.eng
	s.seam.mu.Unlock()

	inner, err := eng.BeginSession(ctx)
	if err != nil {
		return storage.BlobRef{}, err
	}
	if _, err := inner.Append(ctx, bytes.NewReader(payload)); err != nil {
		_ = inner.Abort(ctx)
		return storage.BlobRef{}, err
	}
	return inner.Commit(ctx, expect)
}

func (s *fakeMPUContextSession) Abort(_ context.Context) error {
	s.seam.mu.Lock()
	defer s.seam.mu.Unlock()
	if row, ok := s.seam.rows[s.id]; ok {
		row.done = true
		delete(s.seam.rows, s.id)
	}
	return nil
}

// newRestartStacks builds TWO full stacks sharing one context seam: the
// first plays the pre-crash process, the second the restart (a fresh Server
// means a fresh in-process registry, exactly the kill -9 shape).
func newRestartStacks(t *testing.T) (a, b *harness, seam *fakeMPUContextSeam) {
	t.Helper()
	seam = newFakeMPUContextSeam()
	wire := func(d *httpapi.Deps) {
		eng, ok := d.GC.(storage.Engine)
		if !ok {
			t.Fatal("stack GC seam is not a storage.Engine")
		}
		seam.setEngine(eng)
		d.Uploads = seam
	}
	a = newHarnessFull(t, nil, nil, nil, wire, nil)
	b = newHarnessFull(t, nil, nil, nil, wire, nil)
	seedRepo(t, a, "generic-local")
	seedRepo(t, b, "generic-local")
	return a, b, seam
}

// TestUploadsRestartResumeUnderToken is T-323R's acceptance leg on the
// flipped wire: create + part 1 on the first process (the token in hand),
// kill -9 (stack swap), then the restarted process must honor the SAME
// token — status 200 (the lazy rebuild from the persisted row), the
// remaining parts, complete?sha1= 202, the Finished checksum-deploy token,
// and the client-side landing reading back byte for byte.
func TestUploadsRestartResumeUnderToken(t *testing.T) {
	h1, h2, seam := newRestartStacks(t)

	tok := mpuCreateToken(t, h1, adminUser, adminPass, "generic-local", "big/restart.bin", 5)
	p1, p2, p3, whole := mpuPayload()
	if code, _ := mpuPutPart(t, h1, tok, 1, p1); code != http.StatusOK {
		t.Fatalf("part 1 (pre-crash) = %d", code)
	}

	// The restart: from here every request rides the SECOND stack — a fresh
	// process whose registry has never seen the id. The seam's row (the
	// engine's upload_sessions row + caller blob in production) survived,
	// and the token's binding rides that blob.
	code, st := mpuPostToken(t, h2, "status", tok, "")
	if code != http.StatusOK || st["status"] != "PARTS" {
		t.Fatalf("post-restart status = %d %v, want 200 Uploading (the token re-materializes the session)", code, st)
	}
	if seam.resumes.Load() != 1 {
		t.Fatalf("seam resumes = %d, want exactly 1 (the lazy rebuild)", seam.resumes.Load())
	}

	// The remaining parts ride the resumed session on the restarted
	// process, still on the token lane.
	for i, part := range [][]byte{p2, p3} {
		if code, _ := mpuPutPart(t, h2, tok, i+2, part); code != http.StatusOK {
			t.Fatalf("post-restart part %d PUT = %d", i+2, code)
		}
	}
	wholeSHA1 := sha1Hex(t, whole)
	if code, _ := mpuPostToken(t, h2, "complete", tok, "?sha1="+wholeSHA1); code != http.StatusAccepted {
		t.Fatalf("post-restart complete = %d, want 202", code)
	}
	done := mpuStatusPoll(t, h2, tok, "FINISHED")
	depTok, _ := done["checksumToken"].(string)
	if depTok == "" {
		t.Fatalf("post-restart Finished carries no checksum-deploy token: %v", done)
	}

	// The client's landing through the content plane, then byte-for-byte.
	resp := h2.do(http.MethodPut, "/binflow/generic-local/big/restart.bin", "", "", nil,
		map[string]string{
			"Authorization":     "Bearer " + depTok,
			"X-Checksum-Deploy": "true",
			"X-Checksum-Sha1":   wholeSHA1,
		})
	func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("post-restart checksum-deploy = %d, want 201", resp.StatusCode)
	}
	resp = h2.do(http.MethodGet, "/binflow/generic-local/big/restart.bin", adminUser, adminPass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, whole) {
		t.Fatalf("post-restart artifact GET = %d (%d bytes), want the 11MiB corpus", resp.StatusCode, len(got))
	}
}

// TestUploadsRestartResumeSingleFlight: concurrent first requests with the
// same restarted token must funnel into ONE engine resume — the engine
// resolves same-id concurrent resumes last-writer-wins, so two flyers would
// leak that verdict as a 5xx.
func TestUploadsRestartResumeSingleFlight(t *testing.T) {
	h1, h2, seam := newRestartStacks(t)

	tok := mpuCreateToken(t, h1, adminUser, adminPass, "generic-local", "big/race.bin", 5)
	p1, _, _, _ := mpuPayload()
	if code, _ := mpuPutPart(t, h1, tok, 1, p1); code != http.StatusOK {
		t.Fatalf("part 1 = %d", code)
	}

	const n = 8
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code, raw := mpuPostToken(t, h2, "status", tok, "")
			if code != http.StatusOK {
				t.Errorf("concurrent status %d = %d: %v", i, code, raw)
			}
		}(i)
	}
	wg.Wait()
	if got := seam.resumes.Load(); got != 1 {
		t.Fatalf("engine resumes = %d, want exactly 1 (the per-id funnel)", got)
	}
}

// TestUploadsRestartWithoutContextSeamKeepsProcessOnly: a seam without the
// context pair (the pre-T-323R shape) keeps the process-only posture — the
// token works in-process (the binding lives in the registry) but a restart
// forgets the session and every token verb answers the plain 404.
func TestUploadsRestartWithoutContextSeamKeepsProcessOnly(t *testing.T) {
	var seam *fakeMPUSeam
	h1 := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		seam = &fakeMPUSeam{eng: d.GC.(storage.Engine)}
		d.Uploads = seam
	}, nil)
	seedRepo(t, h1, "generic-local")
	code, created := mpuCreate(t, h1, adminUser, adminPass, "generic-local", "big/old.bin", 5)
	if code != http.StatusOK {
		t.Fatalf("create = %d: %v", code, created)
	}
	tok := created["token"].(string)

	// In-process the token drives the plane (the registry holds the
	// binding even though the row carries none).
	if code, _ := mpuPostToken(t, h1, "status", tok, ""); code != http.StatusOK {
		t.Fatalf("in-process status on the context-less seam = %d, want 200", code)
	}

	h2 := newHarnessFull(t, nil, nil, nil, func(d *httpapi.Deps) {
		d.Uploads = &fakeMPUSeam{eng: d.GC.(storage.Engine)}
	}, nil)
	if code, _ := mpuPostToken(t, h2, "status", tok, ""); code != http.StatusNotFound {
		t.Fatalf("post-restart status = %d, want the process-only 404", code)
	}
}

// fakeMPUSeam is the context-less stand-in (the pre-T-323R shape) — used
// only by the process-only posture test above.
type fakeMPUSeam struct {
	eng   storage.Engine
	begun atomic.Int64
}

func (f *fakeMPUSeam) BeginMultipartSession(_ context.Context, partSize int64) (storage.Session, error) {
	f.begun.Add(1)
	return &fakeMPUSession{eng: f.eng, partSize: partSize, id: fmt.Sprintf("mpu-%d", f.begun.Load())}, nil
}

// fakeMPUSession implements storage.Session in the seam's shape.
type fakeMPUSession struct {
	eng      storage.Engine
	id       string
	partSize int64

	mu   sync.Mutex
	buf  []byte
	done bool
}

func (s *fakeMPUSession) ID() string { return s.id }

func (s *fakeMPUSession) Offset() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.buf))
}

func (s *fakeMPUSession) Append(_ context.Context, r io.Reader) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return 0, fmt.Errorf("session %s already finalized", s.id)
	}
	n, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	s.buf = append(s.buf, n...)
	return int64(len(s.buf)), nil
}

func (s *fakeMPUSession) Commit(ctx context.Context, expect storage.BlobRef) (storage.BlobRef, error) {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return storage.BlobRef{}, fmt.Errorf("session %s already finalized", s.id)
	}
	s.done = true
	payload := s.buf
	s.mu.Unlock()
	inner, err := s.eng.BeginSession(ctx)
	if err != nil {
		return storage.BlobRef{}, err
	}
	if _, err := inner.Append(ctx, bytes.NewReader(payload)); err != nil {
		_ = inner.Abort(ctx)
		return storage.BlobRef{}, err
	}
	return inner.Commit(ctx, expect)
}

func (s *fakeMPUSession) Abort(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	return nil
}
