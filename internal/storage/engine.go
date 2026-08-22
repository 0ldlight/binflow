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

	"github.com/lzwzzy/binflow/internal/metadata"
)

// DefaultSessionTTL is how long an abandoned upload session may linger before
// the startup sweep removes it (ADR-0006). Sessions are persisted in the
// metadata store's upload_sessions table (T-209); their temp data bytes live
// under <data>/uploads/<uuid> and are reclaimed by the same sweep.
const DefaultSessionTTL = 24 * time.Hour

const (
	blobsDirName   = "blobs"
	uploadsDirName = "uploads"
	dataFileName   = "data"
	sha256HexLen   = 64
	sha1HexLen     = 40
	md5HexLen      = 32
)

// sessionState is the persisted per-session bookkeeping, shaped after
// architecture section 4.1: {"version","id","created_at","received"}.
//
//   - version pins the shape for future extensions (a field addition must
//     bump it); v1 is the section 4.1 shape.
//   - received counts the bytes durably recorded so far. It is persisted for
//     observability but is NOT trusted on ResumeSession — a crash between a
//     data write and the state write desyncs it, so ResumeSession re-derives
//     the offset from the data file size and re-hashes the partial bytes to
//     rebuild the digest chain (hash state is not serializable).
type sessionState struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Received  int64     `json:"received"`
}

// Options configures OpenEngine.
type Options struct {
	// SessionTTL bounds the age of an upload session before the startup sweep
	// deletes it. Zero means DefaultSessionTTL.
	SessionTTL time.Duration
	// Now overrides the clock (tests only). Nil uses time.Now.
	Now func() time.Time
	// Sessions is the persistence seam for upload sessions (metadata
	// upload_sessions table). When nil, session state is not persisted:
	// BeginSession still works (session data stays on disk) but ResumeSession
	// always fails with ErrSessionNotFound and the startup sweep skips its
	// row pass (the orphan-dir scan still runs — crash residue stays
	// reclaimable). The blob-only GC path (which never touches sessions) is
	// the one caller that may leave it nil.
	Sessions metadata.UploadSessionStore
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
//	<root>/uploads/<uuid>                 in-progress upload data (temp files)
//
// It then sweeps expired upload sessions (age > ttl) from the metadata store
// (via opts.Sessions, when configured) and their temp files. A failing sweep
// fails the open with that error and leaves all pre-existing data on disk
// untouched — including in-flight upload dirs the sweep contract must keep.
// The engine never owns the metadata schema; session rows live in the
// upload_sessions table the metadata layer manages (T-209, architecture
// section 2).
func OpenEngine(root string, opts Options) (Engine, error) {
	if root == "" {
		return nil, errors.New("storage: open: root path is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage: open %s: %w", root, err)
	}
	for _, dir := range []string{abs, filepath.Join(abs, blobsDirName), filepath.Join(abs, uploadsDirName)} {
		// 0o700: the data dir holds opaque artifact bytes; no group/other
		// access is ever needed (gosec G301 wants 0750, we go stricter).
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("storage: open: create %s: %w", dir, err)
		}
	}
	e := &engine{root: abs, opts: opts, sessions: make(map[string]*uploadSession)}
	if err := e.sweepSessions(time.Now()); err != nil {
		// Fail the open, but never modify on-disk data in response to a sweep
		// failure: the error may be a transient DB fault (e.g. SQLITE_BUSY)
		// while the root still holds resumable upload dirs protected by
		// unexpired rows, and wiping uploads/ would destroy in-flight uploads
		// the sweep contract must keep. Nothing is deleted; the next Open
		// retries the sweep.
		return nil, fmt.Errorf("storage: open %s: %w", abs, err)
	}
	return e, nil
}

// blobPath maps a sha256 to its content-addressed path. The digest check is
// a path-traversal guard: a malformed value must never reach the filesystem.
// The path shape is shared with the backup helpers (BlobPath), which address
// blobs outside any engine instance.
func (e *engine) blobPath(sha256 string) (string, error) { return BlobPath(e.root, sha256) }

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

// BeginSession creates a new upload session: it inserts an upload_sessions row
// (when opts.Sessions is set), opens <root>/uploads/<uuid>/data for append and
// returns the live session. The data file on disk is the source of truth for
// content; the row carries bookkeeping (created/expires/state). A crash before
// the row lands leaves at worst an empty orphan dir under uploads/, reclaimed
// by the next startup sweep's orphan scan.
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
	dir := filepath.Join(e.root, uploadsDirName, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("storage: begin session %s: %w", id, err)
	}
	st := sessionState{Version: 1, ID: id, CreatedAt: e.opts.now()}
	expiresAt := st.CreatedAt.Add(e.opts.ttl())
	if ss := e.opts.Sessions; ss != nil {
		row := &metadata.UploadSession{
			ID:        id,
			State:     marshalSessionState(st),
			CreatedAt: st.CreatedAt.UTC().Format(time.RFC3339),
			ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		}
		if err := ss.Create(ctx, row); err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("storage: begin session %s: persist: %w", id, err)
		}
	}
	f, err := os.OpenFile(filepath.Join(dir, dataFileName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // engine-owned root + generated uuid (path safety: see blobPath guard)
	if err != nil {
		if ss := e.opts.Sessions; ss != nil {
			_ = ss.Delete(ctx, id)
		}
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("storage: begin session %s: %w", id, err)
	}
	s := &uploadSession{eng: e, id: id, dir: dir, file: f, state: st, digests: newDigesters()}
	e.mu.Lock()
	if e.closed { // Close raced us
		e.mu.Unlock()
		_ = f.Close()
		if ss := e.opts.Sessions; ss != nil {
			_ = ss.Delete(ctx, id)
		}
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("storage: begin session %s: %w", id, ErrEngineClosed)
	}
	e.sessions[id] = s
	e.mu.Unlock()
	return s, nil
}

// sessionRowExpired reports whether a persisted session row is past its
// expiry at now, on the same boundary the sweep's ListExpired uses
// (expires_at <= now counts as expired). ResumeSession re-checks it because
// the substore's Get does not filter by expires_at, and a resume racing the
// sweep must fail closed instead of resurrecting a row the sweep is about to
// reclaim (architecture section 5.3.1 contract 3). An empty or malformed
// expires_at also counts as expired: expiry is a write-path invariant
// (BeginSession always persists it), and a row that lost it must not live
// forever.
func sessionRowExpired(row *metadata.UploadSession, now time.Time) bool {
	if row == nil || row.ExpiresAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, row.ExpiresAt)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

// ResumeSession re-materializes a crashed/in-progress session from its
// persisted row and on-disk data file. It re-hashes the partial data to
// rebuild the digest chain (hash state is not serializable) and returns the
// session ready for further Append, with Offset reporting the re-derived
// data-file length — the authoritative offset source for REST resume
// (architecture sections 3.1 [M7] and 5.3.1 contract 1). A missing (or never
// persisted) row — or one already expired but not yet swept — yields
// ErrSessionNotFound (fail-closed; expired = unknown, and the sweep is the
// row's only legitimate reclamation path). A missing data file under a
// surviving unexpired row is recreated empty and the session resumes from
// offset 0; a missing data directory surfaces the underlying open error
// (not a sentinel).
func (e *engine) ResumeSession(ctx context.Context, id string) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, err)
	}
	if err := e.checkOpen(); err != nil {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, err)
	}
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrSessionNotFound)
	}
	ss := e.opts.Sessions
	if ss == nil {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrSessionNotFound)
	}
	row, err := ss.Get(ctx, id)
	if err != nil {
		if errors.Is(err, metadata.ErrUploadSessionNotFound) {
			return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrSessionNotFound)
		}
		return nil, fmt.Errorf("storage: resume session %s: %w", id, err)
	}
	// Fail closed on an expired-but-not-yet-swept row BEFORE touching the
	// data directory: the sweep owns reclamation, a resume racing it must not
	// resurrect the row, and a rejected resume must not O_CREATE files under
	// a directory the sweep is about to remove.
	if sessionRowExpired(row, e.opts.now()) {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrSessionNotFound)
	}
	dir := filepath.Join(e.root, uploadsDirName, id)
	f, err := os.OpenFile(filepath.Join(dir, dataFileName), os.O_CREATE|os.O_RDWR, 0o600) // re-hash reads + append writes both need the fd; O_CREATE tolerates a vanished data file with a surviving row
	if err != nil {
		return nil, fmt.Errorf("storage: resume session %s: %w", id, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("storage: resume session %s: %w", id, err)
	}
	// Re-hash the partial bytes to rebuild the digest chain; received is the
	// file size, NOT the (possibly stale) row state.
	d := newDigesters()
	if info.Size() > 0 {
		if _, err := io.Copy(d.writer(), io.NewSectionReader(f, 0, info.Size())); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("storage: resume session %s: rehash: %w", id, err)
		}
	}
	st := sessionState{Version: 1, ID: id, CreatedAt: e.opts.now(), Received: info.Size()}
	if row.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, row.CreatedAt); err == nil {
			st.CreatedAt = t
		}
	}
	if _, err := f.Seek(info.Size(), io.SeekStart); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("storage: resume session %s: seek: %w", id, err)
	}
	s := &uploadSession{eng: e, id: id, dir: dir, file: f, state: st, digests: d}
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		_ = f.Close()
		return nil, fmt.Errorf("storage: resume session %s: %w", id, ErrEngineClosed)
	}
	// A live session registered under the same id is replaced: the caller
	// holds the old one; resuming the same id is a caller error we resolve
	// deterministically by choosing the new one. The old session is detached
	// AFTER the lock is released (locking s.mu while holding e.mu would invert
	// the s.mu -> e.mu order finishLocked/forgetSession establish and deadlock).
	// detach also must not delete the dir or the DB row: the new session shares
	// them, and cleanup would destroy the very session this call is returning.
	var old *uploadSession
	if prev, ok := e.sessions[id]; ok {
		old = prev
	}
	e.sessions[id] = s
	e.mu.Unlock()
	if old != nil {
		old.detach()
	}
	return s, nil
}

func marshalSessionState(st sessionState) string {
	b, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(b)
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

// sweepSessions deletes expired upload sessions from the metadata store and
// their temp files under uploads/, then removes any orphan dirs left by a crash
// between MkdirAll and row Create (no row to age them out). Sessions that are
// live in this process are never removed. With no Sessions store configured,
// the orphan scan still runs (crash residue is still reclaimable) but no rows
// are consulted.
func (e *engine) sweepSessions(now time.Time) error {
	ss := e.opts.Sessions
	// A failing sweep fails the Open that triggered it (OpenEngine returns
	// the error) but never deletes on-disk data in response: expired sessions
	// the sweep already reclaimed stay reclaimed, everything else waits for
	// the next Open to retry.
	nowStr := now.UTC().Format(time.RFC3339)
	if ss != nil {
		rows, err := ss.ListExpired(context.Background(), nowStr, 0)
		if err != nil {
			return fmt.Errorf("storage: sweep sessions: %w", err)
		}
		var firstErr error
		for _, row := range rows {
			if e.isLiveSession(row.ID) {
				continue
			}
			dir := filepath.Join(e.root, uploadsDirName, row.ID)
			if err := os.RemoveAll(dir); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("storage: sweep sessions: remove %s: %w", row.ID, err)
				continue
			}
			if err := ss.Delete(context.Background(), row.ID); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("storage: sweep sessions: delete row %s: %w", row.ID, err)
			}
		}
		// orphan-dir scan: dirs under uploads/ with no surviving row are
		// residue from a crash before the row landed, reclaimed now.
		if err := e.sweepOrphanDirs(ss); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	return e.sweepOrphanDirs(nil)
}

// sweepOrphanDirs removes any directory under uploads/ that is neither a live
// session nor backed by a surviving row (via ss, when non-nil): a crash
// between MkdirAll and row Create leaves such a dir, and in the nil-store path
// there is no row at all. A still-valid (non-expired) row must protect its dir
// so ResumeSession can find it after a clean restart.
func (e *engine) sweepOrphanDirs(ss metadata.UploadSessionStore) error {
	root := filepath.Join(e.root, uploadsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("storage: sweep sessions: read %s: %w", root, err)
	}
	var firstErr error
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		id := ent.Name()
		if e.isLiveSession(id) {
			continue
		}
		if ss != nil {
			if _, err := ss.Get(context.Background(), id); err == nil {
				continue // surviving non-expired row protects the dir
			} else if !errors.Is(err, metadata.ErrUploadSessionNotFound) {
				if firstErr == nil {
					firstErr = fmt.Errorf("storage: sweep sessions: probe row %s: %w", id, err)
				}
				continue
			}
		}
		dir := filepath.Join(root, id)
		if err := os.RemoveAll(dir); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("storage: sweep sessions: remove orphan %s: %w", id, err)
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

// Open returns an io.ReadCloser over the blob body. The concrete value is an
// *os.File, which also implements io.ReadSeekCloser — callers that need Seek
// (e.g. HTTP Range requests) may type-assert. The returned BlobRef carries
// Sha256 and Size only: ancillary digests are metadata-store facts (ADR-0006
// keeps no sidecar files next to blobs); use Stat for a full digest pass.
// ADR-0019: the Engine interface returns io.ReadCloser so every backend
// (Disk, S3, memory) can implement it; the DiskEngine's *os.File is a
// superset.
func (e *engine) Open(ctx context.Context, sha256 string) (io.ReadCloser, BlobRef, error) {
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
// reclaims; their rows are deleted now) and marks the engine unusable.
// Idempotent.
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
