package cargo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The sparse index face (spec section 3): the config.json entry document
// and the per-crate NDJSON files, plus the whole-file rewrite that keeps
// them true.

// indexLine is one version's NDJSON row (spec section 3.3's field set;
// deps/features ride verbatim from the publish frame — RawMessage keeps
// the bytes stable across rewrites). v/features2 are omitted: BinFlow
// puts every feature in `features` (the spec's recommended merge), and
// the optional v marker buys nothing on cargo 1.8x.
type indexLine struct {
	Name     string          `json:"name"`
	Vers     string          `json:"vers"`
	Deps     json.RawMessage `json:"deps"`
	Cksum    string          `json:"cksum"`
	Features json.RawMessage `json:"features"`
	Yanked   bool            `json:"yanked"`
	Links    string          `json:"links,omitempty"`
}

// normalizeDepsFeatures repairs the absent-field cases: an absent deps
// renders "[]", an absent features "{}" (the index schema expects the
// array/object shapes, never null).
func (l *indexLine) normalizeDepsFeatures() {
	if len(l.Deps) == 0 || string(l.Deps) == "null" {
		l.Deps = json.RawMessage("[]")
	}
	if len(l.Features) == 0 || string(l.Features) == "null" {
		l.Features = json.RawMessage("{}")
	}
}

// configDocument synthesizes index/config.json (spec section 3.1): dl/api
// self-pointing at this repository's own planes, base per TL-1
// (Options.BaseURL = server.base_url; empty falls back to the request's
// scheme+host). auth-required appears only when the instance refuses
// anonymous reads — cargo then sends credentials on the index/download
// requests after its 401-retry handshake.
func (h *Handler) configDocument(origin, repoKey string) []byte {
	api := origin + "/binflow/" + repoKey
	doc := map[string]any{
		"dl":  api + "/v1/crates",
		"api": api,
	}
	if !h.opts.AnonymousAccess {
		doc["auth-required"] = true
	}
	body, err := json.Marshal(doc)
	if err != nil {
		// unreachable: flat string map
		return []byte(`{"dl":"","api":""}`)
	}
	return body
}

// serveConfig answers GET index/config.json, with conditional-request
// support (cargo fetches it once per session — the ETag is the body's
// sha256, computed in-process).
func (h *Handler) serveConfig(w http.ResponseWriter, r *http.Request, repoKey string) {
	origin := h.baseURLFor(r)
	body := h.configDocument(origin, repoKey)
	etag := `"` + blobRefOf(body).Sha256 + `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Content-Type", "application/json")
	hdr.Set("X-Content-Type-Options", "nosniff")
	if conditionalNotModified(r, etag, "") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed JSON
}

// serveIndexFile answers GET index/{pkgPath} from the stored node: 200
// text/plain NDJSON + ETag (= the file's sha256) + Last-Modified, with
// If-None-Match/If-Modified-Since → 304 (spec section 5.3's cache row).
// No node is the 404 + envelope (official allows 404/410/451; BinFlow
// takes 404).
func (h *Handler) serveIndexFile(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, pkgPath string) {
	path := segIndex + "/" + pkgPath
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		h.writeError(w, err, repoKey, path)
		return
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	etag := `"` + node.Sha256 + `"`
	lastMod := httpTime(node.UpdatedAt)
	hdr := w.Header()
	hdr.Set("ETag", etag)
	if lastMod != "" {
		hdr.Set("Last-Modified", lastMod)
	}
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	if conditionalNotModified(r, etag, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if node.Size > 0 {
		hdr.Set("Content-Length", strconv.FormatInt(node.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, rc) // client aborts surface as short writes, not errors
}

// conditionalNotModified evaluates If-None-Match (precedence) then
// If-Modified-Since against the response's validators. lastMod is the
// http.TimeFormat spelling ("" when unknown).
func conditionalNotModified(r *http.Request, etag, lastMod string) bool {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		for _, part := range strings.Split(inm, ",") {
			t := strings.TrimSpace(part)
			t = strings.TrimPrefix(t, "W/")
			if t == "*" || t == etag {
				return true
			}
		}
		return false
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && lastMod != "" {
		t, err := http.ParseTime(ims)
		if err == nil {
			lm, err := http.ParseTime(lastMod)
			if err == nil && !lm.After(t) {
				return true
			}
		}
	}
	return false
}

// httpTime converts one RFC3339 timestamp (the metadata layer's storage
// form) into the HTTP date form; "" when unparsable.
func httpTime(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return ""
	}
	return t.UTC().Format(http.TimeFormat)
}

// ---- the whole-file rewrite (spec section 3.3) ----

// rewriteMutexes is the per-(repoKey, crate) mutex set serializing the
// whole-file index rewrites (B1's lost-update guard — see rewriteIndex).
// Refcounted so the map does not grow with the crate population: an key
// leaves the map the moment its last holder (waiting included) drops it,
// and acquire-before-block ordering keeps two concurrent acquirers of a
// key on ONE mutex instance — the refcount only reaches zero after every
// waiter has passed through.
type rewriteMutexes struct {
	mu   sync.Mutex
	held map[string]*refcountedMutex
}

// refcountedMutex is one keyed lock plus its holder count.
type refcountedMutex struct {
	mu   sync.Mutex
	refs int
}

// withLock runs fn under key's mutex.
func (s *rewriteMutexes) withLock(key string, fn func()) {
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

// rewriteIndex regenerates index/{pkgPath} of one crate from stored
// facts: every version's node row (cksum = the storage-measured sha256 —
// the download reconciliation anchor), its metadata sidecar (deps/
// features verbatim) and its yanked property. One line per version in
// SemVer order; yank state lives on the node properties, so the rewrite
// is the single place the flag ever flips (spec section 6).
//
// The write runs INSIDE the triggering request: the official protocol
// allows index lag (cargo polls a short window after publish and only
// warns), and a synchronous rewrite is the zero-lag point of that
// allowance — L-r3's immediate poll sees the row.
//
// The List→assemble→Put critical section is serialized PER (repoKey,
// crate) — the whole-file rewrite's lost-update guard (T-294 review B1:
// two concurrent rewrites of one crate each snapshot before the other's
// landing, last-write-wins silently drops rows, breaking the
// one-line-per-version invariant). Callers land their own state change
// (the .crate node, the yank property) BEFORE entering, so every
// critical section observes every prior mutation of that crate — the
// last writer's snapshot is complete. DIFFERENT crates stay concurrent
// (the lock key is the one index file's identity; the httpapi
// mpuRegistry per-session precedent). The mutex set is process-local:
// this adapter is the sole writer of index/*, and BinFlow's assembly is
// single-process today — a multi-node deployment is HA's ticket, not
// this one's.
func (h *Handler) rewriteIndex(ctx context.Context, p *repo.Principal, repoKey, name string) error {
	var buildErr error
	h.rewrites.withLock(repoKey+"/"+name, func() {
		buildErr = h.rewriteIndexLocked(ctx, p, repoKey, name)
	})
	return buildErr
}

// rewriteIndexLocked is rewriteIndex's body under the per-crate lock.
func (h *Handler) rewriteIndexLocked(ctx context.Context, p *repo.Principal, repoKey, name string) error {
	nodes, err := h.svc.List(ctx, p, repoKey, crateDir(name))
	if err != nil {
		return fmt.Errorf("list %s versions: %w", name, err)
	}
	lines := make([]indexLine, 0, len(nodes))
	for _, n := range nodes {
		_, version, ok := splitCrateNode(n.Path)
		if !ok {
			continue
		}
		line, err := h.indexLineOf(ctx, p, repoKey, name, version, n)
		if err != nil {
			return err
		}
		lines = append(lines, *line)
	}
	if len(lines) == 0 {
		return nil // nothing stored: nothing to write (a stray delete leaves the stale file)
	}
	sort.Slice(lines, func(i, j int) bool {
		if c := compareSemver(lines[i].Vers, lines[j].Vers); c != 0 {
			return c < 0
		}
		return lines[i].Vers < lines[j].Vers
	})
	var body bytes.Buffer
	for _, l := range lines {
		l.normalizeDepsFeatures()
		row, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("render index line %s %s: %w", l.Name, l.Vers, err)
		}
		body.Write(row)
		body.WriteByte('\n')
	}
	path := segIndex + "/" + indexPath(name)
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, path,
		bytes.NewReader(body.Bytes()), blobRefOf(body.Bytes()),
		"text/plain; charset=utf-8", repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// indexLineOf assembles one version's row: cksum from the node row,
// yanked from the node properties, deps/features/links from the metadata
// sidecar (absent sidecar = the external-import shape: the property
// backfill renders an empty dep set and feature map, spec section 4).
func (h *Handler) indexLineOf(ctx context.Context, p *repo.Principal, repoKey, name, version string, n *metadata.Node) (*indexLine, error) {
	line := &indexLine{
		Name:  name,
		Vers:  version,
		Cksum: n.Sha256,
	}
	if h.props != nil {
		props, err := h.props.List(ctx, repoKey, n.Path)
		if err != nil {
			return nil, fmt.Errorf("read properties %s: %w", n.Path, err)
		}
		if _, yanked := props[propYanked]; yanked {
			line.Yanked = true
		}
	}
	sidecar, err := h.readMetaSidecar(ctx, p, repoKey, name, version)
	if err != nil {
		return nil, err
	}
	if sidecar != nil {
		line.Deps, line.Features, line.Links = sidecar.Deps, sidecar.Features, sidecar.Links
	}
	line.normalizeDepsFeatures()
	return line, nil
}

// readMetaSidecar reads one version's publish-metadata sidecar; nil when
// absent (the import shape — not an error).
func (h *Handler) readMetaSidecar(ctx context.Context, p *repo.Principal, repoKey, name, version string) (*publishMeta, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, metaPath(name, version))
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrRepoNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sidecar %s: %w", metaPath(name, version), err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, maxMetaJSON))
	if err != nil {
		return nil, fmt.Errorf("read sidecar %s: %w", metaPath(name, version), err)
	}
	m, err := parsePublishMeta(raw)
	if err != nil {
		// A sidecar that fails today's validation (imported under older
		// rules) degrades to the backfill row, never a broken index.
		return nil, nil
	}
	return m, nil
}
