package conan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The revision index layer (spec sections 4/5; TL-3): index.json is the
// revisions endpoint's own response body (isomorphic, time-descending,
// first entry = latest) and .timestamp is the revision's birth mark —
// written once at the first successful file PUT into the revision and
// never overwritten.
//
// The read-modify-write cycle (read index, add one revision, write index)
// is serialized PER index file identity (the cargo B1 discipline: two
// concurrent uploads of files into the same coordinate each snapshot
// before the other's landing and last-write-wins would silently drop the
// other's revision row).

// indexLocks is the keyed mutex set (the cargo rewriteMutexes shape
// verbatim — refcounted so the map does not grow with the coordinate
// population, release-after-unlock so two concurrent acquirers of one key
// always meet on ONE mutex instance).
type indexLocks struct {
	mu   sync.Mutex
	held map[string]*refcountedMutex
}

// refcountedMutex is one keyed lock plus its holder count.
type refcountedMutex struct {
	mu   sync.Mutex
	refs int
}

// withLock runs fn under key's mutex.
func (s *indexLocks) withLock(key string, fn func()) {
	s.mu.Lock()
	km, ok := s.held[key]
	if !ok {
		km = &refcountedMutex{}
		if s.held == nil {
			s.held = map[string]*refcountedMutex{}
		}
		s.held[key] = km
	}
	km.refs++
	s.mu.Unlock()

	km.mu.Lock()
	defer func() {
		km.mu.Unlock()
		// Release AFTER unlocking: the refcount reaches zero only once
		// every holder-and-waiter has passed, so a later acquirer of the
		// same key can never build a second live mutex beside a blocked
		// one.
		s.mu.Lock()
		defer s.mu.Unlock()
		km.refs--
		if km.refs == 0 {
			delete(s.held, key)
		}
	}()
	fn()
}

// ---- index.json shapes (spec section 4: revisions-response-isomorphic) ----

// revEntry is one revision row: the revision string and its ISO8601 time.
type revEntry struct {
	Revision string `json:"revision"`
	Time     string `json:"time"`
}

// recipeIndexDoc is the recipe coordinate's index document.
type recipeIndexDoc struct {
	Reference string     `json:"reference"` // name/version@user/channel
	Revisions []revEntry `json:"revisions"`
}

// pkgIndexDoc is the packageId's index document.
type pkgIndexDoc struct {
	Reference string     `json:"reference"` // name/version@user/channel#rRev:pid
	Revisions []revEntry `json:"revisions"`
}

// sortRevisions orders the list per spec section 4: time descending, ties
// broken by revision string descending.
func sortRevisions(revs []revEntry) {
	sort.SliceStable(revs, func(i, j int) bool {
		if revs[i].Time != revs[j].Time {
			return revs[i].Time > revs[j].Time
		}
		return revs[i].Revision > revs[j].Revision
	})
}

// readRecipeIndex loads the coordinate's index; a missing node is the
// empty index (nil revisions, no error) — the callers decide what an empty
// chain answers.
func (h *Handler) readRecipeIndex(ctx context.Context, p *repo.Principal, repoKey, root string) (*recipeIndexDoc, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, recipeIndex(root))
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return &recipeIndexDoc{Reference: "", Revisions: nil}, nil
		}
		return nil, fmt.Errorf("read recipe index %s: %w", recipeIndex(root), err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	if err != nil {
		return nil, fmt.Errorf("read recipe index %s: %w", recipeIndex(root), err)
	}
	var doc recipeIndexDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		// A corrupted index degrades to empty rather than bricking the
		// coordinate: reindex repairs it from the .timestamp facts.
		return &recipeIndexDoc{Reference: "", Revisions: nil}, nil
	}
	sortRevisions(doc.Revisions)
	return &doc, nil
}

// readPkgIndex loads the packageId's index (same posture).
func (h *Handler) readPkgIndex(ctx context.Context, p *repo.Principal, repoKey, root, rrev, pid string) (*pkgIndexDoc, error) {
	path := pkgIndex(root, rrev, pid)
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return &pkgIndexDoc{Reference: "", Revisions: nil}, nil
		}
		return nil, fmt.Errorf("read package index %s: %w", path, err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	if err != nil {
		return nil, fmt.Errorf("read package index %s: %w", path, err)
	}
	var doc pkgIndexDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return &pkgIndexDoc{Reference: "", Revisions: nil}, nil
	}
	sortRevisions(doc.Revisions)
	return &doc, nil
}

// maxIndexBytes bounds one index document read (a revision chain far
// beyond this is a runaway client's work, not a package's life).
const maxIndexBytes = 8 << 20

// latestOf returns the newest revision entry; ok is false on an empty
// chain.
func latestOf(revs []revEntry) (revEntry, bool) {
	if len(revs) == 0 {
		return revEntry{}, false
	}
	return revs[0], true
}

// resolveRRev maps an addressed revision to the concrete one: "" resolves
// through the index (the v1 planes' implicit latest), anything else is the
// literal address.
func (h *Handler) resolveRRev(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev string) (string, revEntry, error) {
	if rrev != "" {
		return rrev, revEntry{Revision: rrev}, nil
	}
	doc, err := h.readRecipeIndex(ctx, p, repoKey, r.coordinateRoot())
	if err != nil {
		return "", revEntry{}, err
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		return "", revEntry{}, fmt.Errorf("%w: no revision of %s", repo.ErrNodeNotFound, r.wireRef())
	}
	return latest.Revision, latest, nil
}

// ---- .timestamp (TL-3: first write wins, forever) ----

// ensureTimestamp lands the revision's .timestamp if it is absent and
// returns the time it now carries. The ms-epoch content is the revision's
// birth mark; index.json's time field is its ISO8601 rendering.
func (h *Handler) ensureTimestamp(ctx context.Context, p *repo.Principal, repoKey, path string, now time.Time) (string, error) {
	if _, _, err := h.svc.Get(ctx, p, repoKey, path); err == nil {
		return "", nil // present: first write already happened — TL-3
	} else if !errors.Is(err, repo.ErrNodeNotFound) && !errors.Is(err, repo.ErrIsFolder) {
		return "", fmt.Errorf("probe %s: %w", path, err)
	}
	ms := now.UnixMilli()
	body := []byte(strconv.FormatInt(ms, 10))
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, path, bytes.NewReader(body),
		blobRefOf(body), "text/plain; charset=utf-8",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return msToRFC3339(ms), nil
}

// readTimestamp returns the .timestamp's ISO8601 time; ok is false when
// the marker is absent (a revision whose index row was hand-removed).
func (h *Handler) readTimestamp(ctx context.Context, p *repo.Principal, repoKey, path string) (string, bool, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, 64))
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return "", false, nil // not a ms epoch: treat as absent
	}
	return msToRFC3339(ms), true, nil
}

// msToRFC3339 renders one ms epoch in the index's time spelling — UTC
// RFC3339 with milliseconds, the lexicographic-order-preserving form the
// sort rule rides on.
func msToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// ---- the registration cycle (the concurrency-protected core) ----

// registerRecipeRevision is the recipe plane's after-PUT tail: ensure the
// revision's .timestamp (first write wins), then add the revision to
// index.json if it is not there. Serialized per (repo, coordinate).
func (h *Handler) registerRecipeRevision(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev string) error {
	root := r.coordinateRoot()
	var out error
	h.index.withLock(repoKey+"/"+root, func() {
		out = h.registerRecipeRevisionLocked(ctx, p, repoKey, r, rrev)
	})
	return out
}

func (h *Handler) registerRecipeRevisionLocked(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev string) error {
	root := r.coordinateRoot()
	ts, err := h.ensureTimestamp(ctx, p, repoKey, recipeTimestamp(root, rrev), time.Now())
	if err != nil {
		return err
	}
	doc, err := h.readRecipeIndex(ctx, p, repoKey, root)
	if err != nil {
		return err
	}
	if ts == "" {
		// The marker pre-existed: its time is the revision's time (TL-3).
		if t, ok, terr := h.readTimestamp(ctx, p, repoKey, recipeTimestamp(root, rrev)); terr == nil && ok {
			ts = t
		}
	}
	for _, e := range doc.Revisions {
		if e.Revision == rrev {
			return nil // already registered (the key is the string)
		}
	}
	if ts == "" {
		ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	doc.Reference = r.wireRef()
	doc.Revisions = append(doc.Revisions, revEntry{Revision: rrev, Time: ts})
	sortRevisions(doc.Revisions)
	return h.writeRecipeIndex(ctx, p, repoKey, root, doc)
}

// registerPkgRevision is the package plane's tail, same shape; serialized
// per (repo, package directory).
func (h *Handler) registerPkgRevision(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev, pid, prev string) error {
	root := r.coordinateRoot()
	key := repoKey + "/" + pkgDir(root, rrev, pid)
	var out error
	h.index.withLock(key, func() {
		out = h.registerPkgRevisionLocked(ctx, p, repoKey, r, rrev, pid, prev)
	})
	return out
}

func (h *Handler) registerPkgRevisionLocked(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev, pid, prev string) error {
	root := r.coordinateRoot()
	tsPath := pkgTimestamp(root, rrev, pid, prev)
	ts, err := h.ensureTimestamp(ctx, p, repoKey, tsPath, time.Now())
	if err != nil {
		return err
	}
	doc, err := h.readPkgIndex(ctx, p, repoKey, root, rrev, pid)
	if err != nil {
		return err
	}
	if ts == "" {
		if t, ok, terr := h.readTimestamp(ctx, p, repoKey, tsPath); terr == nil && ok {
			ts = t
		}
	}
	for _, e := range doc.Revisions {
		if e.Revision == prev {
			return nil
		}
	}
	if ts == "" {
		ts = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	}
	doc.Reference = r.wireRef() + "#" + rrev + ":" + pid
	doc.Revisions = append(doc.Revisions, revEntry{Revision: prev, Time: ts})
	sortRevisions(doc.Revisions)
	return h.writePkgIndex(ctx, p, repoKey, root, rrev, pid, doc)
}

// writeRecipeIndex persists one recipe index document (regenerable
// metadata: freely rewritable, never an overwrite gate).
func (h *Handler) writeRecipeIndex(ctx context.Context, p *repo.Principal, repoKey, root string, doc *recipeIndexDoc) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("render recipe index %s: %w", recipeIndex(root), err)
	}
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, recipeIndex(root), bytes.NewReader(body),
		blobRefOf(body), "application/json", repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write %s: %w", recipeIndex(root), err)
	}
	return nil
}

// writePkgIndex persists one package index document.
func (h *Handler) writePkgIndex(ctx context.Context, p *repo.Principal, repoKey, root, rrev, pid string, doc *pkgIndexDoc) error {
	path := pkgIndex(root, rrev, pid)
	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("render package index %s: %w", path, err)
	}
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, path, bytes.NewReader(body),
		blobRefOf(body), "application/json", repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// removeRecipeRevisionEntry drops one revision row from the recipe index
// (the revision-delete tails). A missing row is fine (idempotent).
func (h *Handler) removeRecipeRevisionEntry(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev string) error {
	root := r.coordinateRoot()
	var out error
	h.index.withLock(repoKey+"/"+root, func() {
		doc, err := h.readRecipeIndex(ctx, p, repoKey, root)
		if err != nil {
			out = err
			return
		}
		kept := doc.Revisions[:0:0]
		for _, e := range doc.Revisions {
			if e.Revision != rrev {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(doc.Revisions) {
			return // nothing removed
		}
		doc.Revisions = kept
		out = h.writeRecipeIndex(ctx, p, repoKey, root, doc)
	})
	return out
}

// removePkgRevisionEntry drops one revision row from the package index.
func (h *Handler) removePkgRevisionEntry(ctx context.Context, p *repo.Principal, repoKey string, r ref, rrev, pid, prev string) error {
	root := r.coordinateRoot()
	key := repoKey + "/" + pkgDir(root, rrev, pid)
	var out error
	h.index.withLock(key, func() {
		doc, err := h.readPkgIndex(ctx, p, repoKey, root, rrev, pid)
		if err != nil {
			out = err
			return
		}
		kept := doc.Revisions[:0:0]
		for _, e := range doc.Revisions {
			if e.Revision != prev {
				kept = append(kept, e)
			}
		}
		if len(kept) == len(doc.Revisions) {
			return
		}
		doc.Revisions = kept
		out = h.writePkgIndex(ctx, p, repoKey, root, rrev, pid, doc)
	})
	return out
}
