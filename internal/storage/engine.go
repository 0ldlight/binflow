package storage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultSessionTTL is how long an abandoned upload session may linger under
// <data>/sessions before the startup sweep removes it (ADR-0006).
const DefaultSessionTTL = 24 * time.Hour

const (
	blobsDirName     = "blobs"
	sessionsDirName  = "sessions"
	sessionDataFile  = "data"
	sessionStateFile = "state.json"
	stateVersion     = 1
	sha256HexLen     = 64
	sha1HexLen       = 40
	md5HexLen        = 32
)

// sessionState is the on-disk state.json payload, shaped after
// architecture section 4.1: {"id","created_at","received","sha256":null}.
//
//   - sha256 stays a literal null: digests are never persisted, because a
//     crash invalidates the running hash chain and M1 restarts uploads from
//     zero (ResumeSession is the M2 seam).
//   - received counts the bytes durably recorded so far. M1 updates it after
//     every Append so the file never lies; no M1 reader consumes it yet.
//   - version pins the shape for future extensions (a field addition must
//     bump it); v1 is the section 4.1 shape above.
type sessionState struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Received  int64     `json:"received"`
	Sha256    *string   `json:"sha256"` // always null in M1 (digests not persisted)
}

// Options configures OpenEngine.
type Options struct {
	// SessionTTL bounds the age of session directories before the startup
	// sweep deletes them. Zero means DefaultSessionTTL.
	SessionTTL time.Duration
	// Now overrides the clock (tests only). Nil uses time.Now.
	Now func() time.Time
}

func (o *Options) ttl() time.Duration {
	if o.SessionTTL <= 0 {
		return DefaultSessionTTL
	}
	return o.SessionTTL
}

func (o *Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// engine is the stdlib-only storage Engine. It is safe for concurrent use.
type engine struct {
	root     string
	opts     Options
	sf       singleflight
	mu       sync.RWMutex // guards closed and sessions
	closed   bool
	sessions map[string]*uploadSession
}

// OpenEngine opens (or initializes) the blob store under root:
//
//	<root>/blobs/<sha256[0:2]>/<sha256>   content-addressed, immutable
//	<root>/sessions/<uuid>/{data,state.json}
//
// It then sweeps expired sessions (age > ttl). The engine never touches any
// metadata store; blob rows live in SQLite (T-10, architecture section 2).
func OpenEngine(root string, opts Options) (Engine, error) {
	if root == "" {
		return nil, errors.New("storage: open: root path is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", root, err)
	}
	for _, dir := range []string{abs, filepath.Join(abs, blobsDirName), filepath.Join(abs, sessionsDirName)} {
		// 0o700: the data dir holds opaque artifact bytes; no group/other
		// access is ever needed (gosec G301 wants 0750, we go stricter).
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("storage: open: create %s: %w", dir, err)
		}
	}
	e := &engine{root: abs, opts: opts, sessions: make(map[string]*uploadSession)}
	if err := e.sweepSessions(time.Now()); err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", abs, err)
	}
	return e, nil
}

// blobPath maps a sha256 to its content-addressed path. The digest check is
// a path-traversal guard: a malformed value must never reach the filesystem.
func (e *engine) blobPath(sha256 string) (string, error) {
	if !validSha256(sha256) {
		return "", fmt.Errorf("storage: invalid sha256 %q", sha256)
	}
	return filepath.Join(e.root, blobsDirName, sha256[:2], sha256), nil
}

func validSha256(s string) bool {
	if len(s) != sha256HexLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("storage: uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (e *engine) checkOpen() error {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return ErrEngineClosed
	}
	return nil
}

// BeginSession creates <root>/sessions/<uuid>/{data,state.json} and returns
// the live session.
func (e *engine) BeginSession(ctx context.Context) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: begin session: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: begin session: %w", err)
	}
	id, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("storage: begin session: %w", err)
	}
	dir := filepath.Join(e.root, sessionsDirName, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: begin session %s: %w", id, err)
	}
	st := sessionState{Version: stateVersion, ID: id, CreatedAt: e.opts.now()}
	if err := writeSessionState(dir, st); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("storage: begin session %s: %w", id, err)
	}
	f, err := os.OpenFile(filepath.Join(dir, sessionDataFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // engine-owned root + generated uuid (path safety: see blobPath guard)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("storage: begin session %s: %w", id, err)
	}
	s := &uploadSession{eng: e, id: id, dir: dir, file: f, state: st, digests: newDigesters()}
	e.mu.Lock()
	if e.closed { // Close raced us
		e.mu.Unlock()
		_ = f.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("storage: begin session %s: %w", id, ErrEngineClosed)
	}
	e.sessions[id] = s
	e.mu.Unlock()
	return s, nil
}

// ResumeSession returns ErrSessionNotFound in M1 (chunked uploads are M2;
// the interface seam stays, architecture section 3.1).
func (e *engine) ResumeSession(_ context.Context, id string) (Session, error) {
	return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrSessionNotFound)
}

func writeSessionState(dir string, st sessionState) error {
	blob, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	p := filepath.Join(dir, sessionStateFile)
	tmp := p + ".tmp"
	if err := writeFileSync(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return syncDir(dir)
}

// writeFileSync writes b to path, fsyncs and closes; callers rename it into
// place (temp + fsync + rename, the atomicity triad).
func writeFileSync(path string, b []byte, mode os.FileMode) (retErr error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) // callers pass engine-internal temp paths only
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); retErr == nil && cerr != nil {
			retErr = cerr
		}
	}()
	if _, err := f.Write(b); err != nil {
		return err
	}
	return f.Sync()
}

// syncDir fsyncs a directory so a preceding rename/create is durable
// (step 4 of the ADR-0006 protocol).
func syncDir(dir string) error {
	d, err := os.Open(dir) // dir is engine-owned (blob shard or session dir)
	if err != nil {
		return err
	}
	defer d.Close() //nolint:errcheck // read-only fd, Close error has no actionable meaning
	return d.Sync()
}

// sessionCreatedAt resolves the birth time of a session directory, preferring
// state.json (survives mtime touch) and falling back to the dir mtime.
func sessionCreatedAt(dir string) (time.Time, bool) {
	b, err := os.ReadFile(filepath.Join(dir, sessionStateFile)) // path built from engine-owned root + uuid
	if err == nil {
		var st sessionState
		if json.Unmarshal(b, &st) == nil && !st.CreatedAt.IsZero() {
			return st.CreatedAt, true
		}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// sweepSessions removes session directories older than ttl. Sessions that
// are live in this process are never removed. A corrupt or missing
// state.json does not block the sweep: the mtime fallback still ages the
// directory out.
func (e *engine) sweepSessions(now time.Time) error {
	root := filepath.Join(e.root, sessionsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("storage: sweep sessions: %w", err)
	}
	var firstErr error
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(root, ent.Name())
		if e.isLiveSession(ent.Name()) {
			continue
		}
		createdAt, ok := sessionCreatedAt(dir)
		if !ok {
			continue // vanished between ReadDir and stat; next sweep retries
		}
		if now.Sub(createdAt) <= e.opts.ttl() {
			continue
		}
		if err := os.RemoveAll(dir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("storage: sweep sessions: remove %s: %w", dir, err)
		}
	}
	return firstErr
}

func (e *engine) isLiveSession(id string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.sessions == nil {
		return false
	}
	_, ok := e.sessions[id]
	return ok
}

// forgetSession unregisters s if it is still the registered instance.
func (e *engine) forgetSession(s *uploadSession) {
	e.mu.Lock()
	if cur, ok := e.sessions[s.id]; ok && cur == s {
		delete(e.sessions, s.id)
	}
	e.mu.Unlock()
}

// Open returns a ReadSeekCloser over the blob body. The returned BlobRef
// carries Sha256 and Size only: ancillary digests are metadata-store facts
// (ADR-0006 keeps no sidecar files next to blobs); use Stat for a full
// digest pass.
func (e *engine) Open(ctx context.Context, sha256 string) (io.ReadSeekCloser, BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: open blob: %w", err)
	}
	path, err := e.blobPath(sha256)
	if err != nil {
		return nil, BlobRef{}, fmt.Errorf("storage: open blob: %w", err)
	}
	f, err := os.Open(path) // path is validated hex by blobPath
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, BlobRef{}, fmt.Errorf("storage: open blob %s: %w", sha256, ErrBlobNotFound)
		}
		return nil, BlobRef{}, fmt.Errorf("storage: open blob %s: %w", sha256, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, BlobRef{}, fmt.Errorf("storage: open blob %s: %w", sha256, err)
	}
	return f, BlobRef{Sha256: sha256, Size: info.Size()}, nil
}

// Stat streams the blob once, verifies it hashes to its own path and returns
// all three digests. Corruption detection entry point for consistency checks.
func (e *engine) Stat(ctx context.Context, sha256 string) (BlobRef, error) {
	if err := ctx.Err(); err != nil {
		return BlobRef{}, fmt.Errorf("storage: stat blob: %w", err)
	}
	path, err := e.blobPath(sha256)
	if err != nil {
		return BlobRef{}, fmt.Errorf("storage: stat blob: %w", err)
	}
	f, err := os.Open(path) // path is validated hex by blobPath
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return BlobRef{}, fmt.Errorf("storage: stat blob %s: %w", sha256, ErrBlobNotFound)
		}
		return BlobRef{}, fmt.Errorf("storage: stat blob %s: %w", sha256, err)
	}
	defer f.Close() //nolint:errcheck // read-only fd
	d := newDigesters()
	size, err := io.Copy(d.writer(), f)
	if err != nil {
		return BlobRef{}, fmt.Errorf("storage: stat blob %s: %w", sha256, err)
	}
	sums := d.sums()
	if sums.sha256 != sha256 {
		return BlobRef{}, fmt.Errorf("storage: stat blob %s: %w: content hashes to %s", sha256, ErrBlobCorrupt, sums.sha256)
	}
	return BlobRef{Sha256: sums.sha256, Sha1: sums.sha1, Md5: sums.md5, Size: size}, nil
}

// Delete physically removes a blob (GC-only entry point; runtime artifact
// deletion removes metadata references instead, architecture section 4.4).
// Refused after Close: a shut-down engine must not mutate the store.
func (e *engine) Delete(ctx context.Context, sha256 string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storage: delete blob: %w", err)
	}
	if err := e.checkOpen(); err != nil {
		return fmt.Errorf("storage: delete blob %s: %w", sha256, err)
	}
	path, err := e.blobPath(sha256)
	if err != nil {
		return fmt.Errorf("storage: delete blob: %w", err)
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("storage: delete blob %s: %w", sha256, ErrBlobNotFound)
		}
		return fmt.Errorf("storage: delete blob %s: %w", sha256, err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("storage: delete blob %s: %w", sha256, err)
	}
	return nil
}

// Close aborts live sessions (their temp data is garbage the next sweep
// reclaims) and marks the engine unusable. Idempotent.
func (e *engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	live := make([]*uploadSession, 0, len(e.sessions))
	for _, s := range e.sessions {
		live = append(live, s)
	}
	e.sessions = nil
	e.mu.Unlock()
	var firstErr error
	for _, s := range live {
		if err := s.cleanup(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("storage: close: session %s: %w", s.id, err)
		}
	}
	return firstErr
}

// normalizeHex validates a client-supplied digest of the given length and
// lowercases it; an empty string passes (digest not supplied). Uppercase hex
// from sloppy clients is accepted case-insensitively.
func normalizeHex(s string, want int) (string, error) {
	if s == "" {
		return "", nil
	}
	if len(s) != want {
		return "", fmt.Errorf("digest length %d, want %d", len(s), want)
	}
	low := strings.ToLower(s)
	for i := 0; i < len(low); i++ {
		c := low[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("digest %q is not hexadecimal", s)
		}
	}
	return low, nil
}
