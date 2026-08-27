package rpm

// The reindex engine (rpm.md sections 2.2-2.3 / 4.2-4.4): the full
// recomputation one yum root's repodata goes through, invoked by the
// management plane (httpapi's /api/yum family) and — only when the
// repository opts in with calculateYumMetadata=true (RP-2's final ruling:
// the BinFlow default is Artifactory's FALSE — an upload stores, the
// repodata recomputes when the reindex endpoint or the explicit switch
// says so) — by the upload/delete chain.
//
// One root's run, in the spec's order:
//
//  1. clear the legacy *.sqlite.bz2 metadata and any stale repomd.xml.asc
//     / .key pair (unsigned mode — no keypair system yet, K-1 pending; a
//     surviving old signature would let clients "verify" a new repomd
//     against it);
//  2. parse every .rpm under the root (the .rpmcache answers unchanged
//     packages without re-reading bytes);
//  3. render the trio (filelists only with enableFileListsIndexing),
//     gzip, name after the compressed digest;
//  4. stage the new files under _tmp_<nanoTime><hash>/repodata/… then
//     promote them into <root>/repodata/ — the blob layer dedupes, the
//     promote reuses the staged blob (checksum-deploy semantics), and the
//     client never observes a half-written generation: new digest names
//     coexist with the old ones until repomd.xml itself flips last;
//  5. write the new repomd.xml (plus the comps group chain's two entries);
//  6. keep the newest N generations per index type (default 3), delete
//     older and stale-spelling files, drop the staging directory.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// RepoConfig is the rpm section of the repository's config blob (the
// Artifactory field spellings; every field optional, every default the
// spec's).
type RepoConfig struct {
	CalculateYumMetadata    bool   `json:"calculateYumMetadata"`
	YumRootDepth            int    `json:"yumRootDepth"`
	EnableFileListsIndexing bool   `json:"enableFileListsIndexing"`
	YumGroupFileNames       string `json:"yumGroupFileNames"`
}

// defaultGroupFileNames is yumGroupFileNames' default (the convention the
// spec names).
const defaultGroupFileNames = "comps.xml"

// generationsToKeep is rpm.metadata.history.cycles.to.keep's default 3
// (rpm.md section 2.3 — the retention knob ships at its spec default).
const generationsToKeep = 3

// maxIndexReadBytes bounds one stored metadata read.
const maxIndexReadBytes = 64 << 20

// parseRepoConfig reads the section off the config blob; a malformed blob
// means the defaults (the config plane's own validation owns hard errors).
func parseRepoConfig(config string) RepoConfig {
	var probe RepoConfig
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return RepoConfig{}
	}
	return probe
}

// groupNames splits yumGroupFileNames into the normalized list.
func (c RepoConfig) groupNames() []string {
	raw := c.YumGroupFileNames
	if strings.TrimSpace(raw) == "" {
		raw = defaultGroupFileNames
	}
	var out []string
	for _, n := range strings.Split(raw, ",") {
		n = strings.TrimSpace(n)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

// configFor loads the repository row and its rpm section.
func (h *Handler) configFor(ctx context.Context, repoKey string) (RepoConfig, error) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return RepoConfig{}, errRepoNotFound(repoKey)
		}
		return RepoConfig{}, fmt.Errorf("load repository %s: %w", repoKey, err)
	}
	return parseRepoConfig(row.Config), nil
}

// ReindexRepository recomputes EVERY candidate root's repodata (the
// management plane's whole-repository run; the per-repository index lock
// serializes it against concurrent runs and the upload-triggered
// recomputes).
func (h *Handler) ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error {
	return h.withIndexLock(repoKey, func() error {
		cfg, err := h.configFor(ctx, repoKey)
		if err != nil {
			return err
		}
		roots, err := h.candidateRoots(ctx, p, repoKey, cfg.YumRootDepth)
		if err != nil {
			return err
		}
		for _, root := range roots {
			if err := h.reindexRootLocked(ctx, p, repoKey, root, cfg); err != nil {
				return err
			}
		}
		return nil
	})
}

// recomputeRoot is the upload/delete chain's entry: the same single-root
// run under the same lock, dispatched in the background (the async work
// posture of section 4.1).
func (h *Handler) recomputeRoot(ctx context.Context, p *repo.Principal, repoKey, root string) {
	if err := h.withIndexLock(repoKey, func() error {
		cfg, err := h.configFor(ctx, repoKey)
		if err != nil {
			return err
		}
		return h.reindexRootLocked(ctx, p, repoKey, root, cfg)
	}); err != nil {
		slog.ErrorContext(ctx, "rpm: automatic repodata recompute failed",
			slog.String("repo", repoKey), slog.String("root", root), slog.String("error", err.Error()))
	}
}

// candidateRoots enumerates the yum roots a full recompute visits: the
// distinct first-yumRootDepth directory prefixes across the repository's
// storage (depth 0 = the single repository root). Paths under a _tmp_
// first segment never contribute (the staging area's own files are not
// repository content).
func (h *Handler) candidateRoots(ctx context.Context, p *repo.Principal, repoKey string, depth int) ([]string, error) {
	if depth <= 0 {
		return []string{""}, nil
	}
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", repoKey, err)
	}
	set := map[string]bool{}
	for _, n := range nodes {
		if isTmpPath(n.Path) || strings.HasSuffix(n.Path, "/") {
			continue
		}
		if root, ok := yumRootOf(n.Path, depth); ok {
			set[root] = true
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out, nil
}

// joinRoot spells one storage path under a reindex root.
func joinRoot(root, rel string) string {
	if root == "" {
		return rel
	}
	return root + "/" + rel
}

// reindexRootLocked runs one root's full recomputation (section 4.2).
func (h *Handler) reindexRootLocked(ctx context.Context, p *repo.Principal, repoKey, root string, cfg RepoConfig) error {
	repodataRel := joinRoot(root, dirRepodata)

	// 1. The legacy-sqlite sweep and the stale-signature removal.
	if err := h.cleanupRepodataLocked(ctx, p, repoKey, repodataRel); err != nil {
		return err
	}

	// 2. Collect and parse the .rpm set.
	entries, skipped, err := h.collectEntries(ctx, p, repoKey, root)
	if err != nil {
		return err
	}

	// 3-4. Render, stage, promote.
	set := renderIndexes(entries, cfg.EnableFileListsIndexing)
	stageDir := h.stageDir()
	var dataEls []*dataEntry
	staged := map[string]storage.BlobRef{}
	stage := func(d *dataEntry) error {
		ref, err := h.stageFile(ctx, p, repoKey, stageDir, d)
		if err != nil {
			return err
		}
		staged[d.href] = ref
		dataEls = append(dataEls, d)
		return nil
	}
	if d, err := dataEntryFor("primary", set.primary); err != nil {
		return err
	} else if err := stage(d); err != nil {
		return err
	}
	if d, err := dataEntryFor("other", set.other); err != nil {
		return err
	} else if err := stage(d); err != nil {
		return err
	}
	if cfg.EnableFileListsIndexing {
		if d, err := dataEntryFor("filelists", set.filelists); err != nil {
			return err
		} else if err := stage(d); err != nil {
			return err
		}
	}

	// The comps group chain (section 4.4): rename uploads to their digest
	// spellings, generate the .gz companions, append the two entries.
	groups, err := h.processGroupsLocked(ctx, p, repoKey, repodataRel, cfg.groupNames())
	if err != nil {
		return err
	}
	for _, g := range groups {
		if err := stage(g); err != nil {
			return err
		}
	}

	// 5. Promote everything under <root>/repodata/, then repomd.xml LAST
	// (the atomic flip: until this write, the previous generation keeps
	// serving, and the new digest-named files are invisible to clients).
	for _, d := range dataEls {
		if err := h.promoteFile(ctx, p, repoKey, joinRoot(root, d.href), staged[d.href]); err != nil {
			return err
		}
	}
	repomdBody := renderRepomd(dataEls, h.now().Unix())
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, joinRoot(root, fileRepomd),
		strings.NewReader(string(repomdBody)), blobRefOf(repomdBody), "text/xml",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write repomd: %w", err)
	}

	// 6. Retention: keep the newest generations per index type, drop the
	// stale group spellings, clean the staging tree.
	if err := h.pruneGenerationsLocked(ctx, p, repoKey, repodataRel, cfg, dataEls); err != nil {
		return err
	}
	if err := h.dropStaging(ctx, p, repoKey, stageDir); err != nil {
		return err
	}

	slog.InfoContext(ctx, "rpm: repodata recompute complete",
		slog.String("repo", repoKey), slog.String("root", root),
		slog.Int("packages", len(entries)), slog.Int("skipped", skipped))
	return nil
}

// cleanupRepodataLocked deletes the legacy sqlite metadata and the stale
// signature pair of the PREVIOUS generation (unsigned mode — section 4.3:
// no key means no signature, and an old signature must not survive).
func (h *Handler) cleanupRepodataLocked(ctx context.Context, p *repo.Principal, repoKey, repodataRel string) error {
	nodes, err := h.svc.List(ctx, p, repoKey, repodataRel)
	if err != nil {
		if errors.Is(err, repo.ErrRepoNotFound) {
			return errRepoNotFound(repoKey)
		}
		return fmt.Errorf("list %s: %w", repodataRel, err)
	}
	for _, n := range nodes {
		base := path.Base(n.Path)
		drop := false
		switch {
		case strings.HasSuffix(base, ".sqlite.bz2"):
			drop = true
		case repodataRel == dirRepodata && (n.Path == fileRepomd+".asc" || n.Path == fileRepomd+".key"):
			drop = true
		case repodataRel != dirRepodata && (n.Path == repodataRel+"/repomd.xml.asc" || n.Path == repodataRel+"/repomd.xml.key"):
			drop = true
		}
		if drop {
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("delete stale metadata %s: %w", n.Path, err)
			}
		}
	}
	return nil
}

// collectEntries parses every .rpm under the root (cache-first); a package
// that fails to parse skips with a WARN (section 5 step 3: the PUT was
// accepted, the indexer quietly omits it).
func (h *Handler) collectEntries(ctx context.Context, p *repo.Principal, repoKey, root string) ([]pkgEntry, int, error) {
	prefix := ""
	if root != "" {
		prefix = root
	}
	nodes, err := h.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return nil, 0, fmt.Errorf("list %s under %q: %w", repoKey, prefix, err)
	}
	var entries []pkgEntry
	skipped := 0
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") || !isRpmPath(n.Path) {
			continue
		}
		if root != "" && !strings.HasPrefix(n.Path+"/", root+"/") {
			continue
		}
		hdr := h.cache.load(repoKey, n.Path, n.Sha256, n.Size)
		if hdr == nil {
			hdr, err = h.parseStoredRpm(ctx, p, repoKey, n.Path)
			if err != nil {
				skipped++
				slog.WarnContext(ctx, "rpm: reindex skipped an unparsable package",
					slog.String("repo", repoKey), slog.String("path", n.Path), slog.String("error", err.Error()))
				continue
			}
			h.cache.store(repoKey, n.Path, n.Sha256, n.Size, hdr)
		}
		entries = append(entries, pkgEntry{
			hdr:      hdr,
			path:     n.Path,
			sha256:   n.Sha256,
			size:     n.Size,
			fileTime: nodeUnix(n.UpdatedAt),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries, skipped, nil
}

// parseStoredRpm opens one stored node and parses its header.
func (h *Handler) parseStoredRpm(ctx context.Context, p *repo.Principal, repoKey, nodePath string) (*Header, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, nodePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	return ParseHeader(io.LimitReader(rc, maxIndexReadBytes))
}

// stageFile writes one index body into the staging tree and returns the
// committed blob reference (the promote reuses the blob — the spec's tmp
// directory move, blob-deduplicated).
func (h *Handler) stageFile(ctx context.Context, p *repo.Principal, repoKey, stageDir string, d *dataEntry) (storage.BlobRef, error) {
	ref := blobRefOf(d.body)
	stagePath := joinRoot(stageDir, d.href)
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, stagePath,
		strings.NewReader(string(d.body)), ref, "application/gzip",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return storage.BlobRef{}, fmt.Errorf("stage %s: %w", d.href, err)
	}
	return ref, nil
}

// promoteFile lands a staged blob at its final path (checksum-deploy
// semantics: the blob exists, only the node row is new).
func (h *Handler) promoteFile(ctx context.Context, p *repo.Principal, repoKey, finalPath string, ref storage.BlobRef) error {
	if _, err := h.svc.PutFromBlob(ctx, p, repoKey, finalPath, ref, "application/gzip"); err != nil {
		return fmt.Errorf("promote %s: %w", finalPath, err)
	}
	return nil
}

// dropStaging removes the run's staging tree.
func (h *Handler) dropStaging(ctx context.Context, p *repo.Principal, repoKey, stageDir string) error {
	if err := h.svc.Delete(ctx, p, repoKey, stageDir+"/"); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		return fmt.Errorf("drop staging %s: %w", stageDir, err)
	}
	return nil
}

// processGroupsLocked runs the comps chain for one root: every configured
// group name's uploaded spelling renames to <digest>-<name>.xml (content
// unchanged → same digest → the same spelling every run), the .gz
// companion regenerates, older same-name spellings drop. The returned
// entries carry the group/group_gz repomd records.
func (h *Handler) processGroupsLocked(ctx context.Context, p *repo.Principal, repoKey, repodataRel string, groupNames []string) ([]*dataEntry, error) {
	if len(groupNames) == 0 {
		return nil, nil
	}
	nodes, err := h.svc.List(ctx, p, repoKey, repodataRel)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", repodataRel, err)
	}
	byBase := map[string]*metadata.Node{}
	for _, n := range nodes {
		if !strings.HasSuffix(n.Path, "/") {
			byBase[path.Base(n.Path)] = n
		}
	}
	var out []*dataEntry
	for _, name := range groupNames {
		// The configured value is the whole FILE NAME (the convention the
		// spec names: "comps.xml").
		plain := byBase[name]
		renamed := ""
		for base, n := range byBase {
			rest, ok := stripDigestPrefix(base)
			if ok && rest == name {
				renamed = n.Path
				break
			}
		}
		if plain == nil && renamed == "" {
			continue
		}
		var body []byte
		switch {
		case plain != nil:
			b, err := h.readNode(ctx, p, repoKey, plain.Path)
			if err != nil {
				return nil, err
			}
			digest := sha256Hex(b)
			want := joinRoot(repodataRel, digest+"-"+name)
			if renamed != "" && renamed == want {
				// Content identical to the renamed copy: drop the fresh
				// un-prefixed upload, reuse the entry (section 4.4's
				// same-content rule).
				if err := h.svc.Delete(ctx, p, repoKey, plain.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
					return nil, fmt.Errorf("delete duplicate group upload %s: %w", plain.Path, err)
				}
				body = b
				break
			}
			// Rename: land the digest-spelled copy from the uploaded blob,
			// then remove the un-prefixed original.
			if _, err := h.svc.PutFromBlob(ctx, p, repoKey, want,
				storage.BlobRef{Sha256: plain.Sha256}, "text/xml"); err != nil {
				return nil, fmt.Errorf("rename group file %s: %w", plain.Path, err)
			}
			if err := h.svc.Delete(ctx, p, repoKey, plain.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return nil, fmt.Errorf("delete un-renamed group file %s: %w", plain.Path, err)
			}
			body = b
		default: // renamed != ""
			b, err := h.readNode(ctx, p, repoKey, renamed)
			if err != nil {
				return nil, err
			}
			body = b
		}
		g, gg, err := groupDataEntries(name, body)
		if err != nil {
			return nil, err
		}
		// Land the .gz companion (idempotent: deterministic gzip → same
		// digest → same path every run), then drop same-name stale
		// spellings of BOTH forms.
		if _, err := h.svc.PutWithOptions(ctx, p, repoKey, joinRoot(repodataRel, path.Base(gg.href)),
			strings.NewReader(string(gg.body)), blobRefOf(gg.body), "application/gzip",
			repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
			return nil, fmt.Errorf("write group gz: %w", err)
		}
		if err := h.dropStaleGroupSpellings(ctx, p, repoKey, repodataRel, name, path.Base(g.href), path.Base(gg.href)); err != nil {
			return nil, err
		}
		out = append(out, g, gg)
	}
	return out, nil
}

// dropStaleGroupSpellings removes the same group name's other spellings
// (older digest prefixes and the .xml.gz variants the new run replaced).
func (h *Handler) dropStaleGroupSpellings(ctx context.Context, p *repo.Principal, repoKey, repodataRel, name, keepXML, keepGZ string) error {
	nodes, err := h.svc.List(ctx, p, repoKey, repodataRel)
	if err != nil {
		return fmt.Errorf("list %s: %w", repodataRel, err)
	}
	for _, n := range nodes {
		base := path.Base(n.Path)
		if base == keepXML || base == keepGZ || base == name {
			continue
		}
		rest, ok := stripDigestPrefix(base)
		if !ok {
			continue
		}
		if rest == name || rest == name+".gz" {
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("delete stale group spelling %s: %w", n.Path, err)
			}
		}
	}
	return nil
}

// pruneGenerationsLocked enforces the per-index-type retention (newest N
// by node UpdatedAt) and the filelists all-clear when the indexing is off
// (section 2.3's closing rule).
func (h *Handler) pruneGenerationsLocked(ctx context.Context, p *repo.Principal, repoKey, repodataRel string, cfg RepoConfig, current []*dataEntry) error {
	nodes, err := h.svc.List(ctx, p, repoKey, repodataRel)
	if err != nil {
		return fmt.Errorf("list %s: %w", repodataRel, err)
	}
	currentNames := map[string]bool{}
	for _, d := range current {
		currentNames[path.Base(d.href)] = true
	}
	buckets := map[string][]*metadata.Node{} // index type -> files
	for _, n := range nodes {
		base := path.Base(n.Path)
		if strings.HasSuffix(n.Path, "/") || base == "repomd.xml" || !isDigestPrefixed(base) {
			continue
		}
		buckets[indexTypeOf(base)] = append(buckets[indexTypeOf(base)], n)
	}
	for typ, files := range buckets {
		keep := generationsToKeep
		if typ == "filelists" && !cfg.EnableFileListsIndexing {
			keep = 0 // the all-clear: every filelists generation goes
		}
		sort.Slice(files, func(i, j int) bool { return files[i].UpdatedAt > files[j].UpdatedAt })
		for i, n := range files {
			if i < keep || currentNames[path.Base(n.Path)] {
				continue
			}
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("prune %s: %w", n.Path, err)
			}
		}
	}
	return nil
}

// indexTypeOf buckets one digest-prefixed repodata file by its index type.
func indexTypeOf(base string) string {
	rest := base[65:] // past <64hex>-
	switch {
	case strings.HasSuffix(rest, "primary.xml.gz"):
		return "primary"
	case strings.HasSuffix(rest, "other.xml.gz"):
		return "other"
	case strings.HasSuffix(rest, "filelists.xml.gz"):
		return "filelists"
	case strings.HasSuffix(rest, "modules.yaml.gz"):
		return "modules"
	case strings.HasSuffix(rest, ".xml.gz"):
		return "group_gz"
	case strings.HasSuffix(rest, ".xml"):
		return "group"
	default:
		return "other-spelling"
	}
}

// readNode reads one stored node's whole body (bounded).
func (h *Handler) readNode(ctx context.Context, p *repo.Principal, repoKey, nodePath string) ([]byte, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, nodePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", nodePath, err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	b, err := io.ReadAll(io.LimitReader(rc, maxIndexReadBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s body: %w", nodePath, err)
	}
	return b, nil
}

// stageDir is this run's staging directory: _tmp_<nanoTime><8hex>.
func (h *Handler) stageDir() string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		suffix = [4]byte{0, 0, 0, 1}
	}
	return tmpPrefix + strconv.FormatInt(h.now().UnixNano(), 10) + hex.EncodeToString(suffix[:])
}

// nodeUnix parses a node's RFC3339 UpdatedAt into unix seconds (0 on any
// parse trouble — repomd timestamps are freshness hints, not contracts).
func nodeUnix(updated string) int64 {
	if t, err := time.Parse(time.RFC3339, updated); err == nil {
		return t.Unix()
	}
	return 0
}

// ---- the per-repository index lock (the helm/cargo refcounted posture) ----

// indexMutexes serializes the repodata rewrites per repository key.
type indexMutexes struct {
	mu   sync.Mutex
	held map[string]*refcountedMutex
}

// refcountedMutex is one keyed lock plus its holder count.
type refcountedMutex struct {
	mu   sync.Mutex
	refs int
}

// withLock runs fn under key's mutex.
func (s *indexMutexes) withLock(key string, fn func()) {
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
		s.mu.Lock()
		defer s.mu.Unlock()
		km.refs--
		if km.refs == 0 {
			delete(s.held, key)
		}
	}()
	fn()
}

// withIndexLock runs fn under the repository's index lock.
func (h *Handler) withIndexLock(repoKey string, fn func() error) error {
	var err error
	h.rewrites.withLock(repoKey, func() { err = fn() })
	return err
}

// now resolves the handler clock (Options.Now, defaulting to time.Now).
func (h *Handler) now() time.Time {
	if h.opts.Now != nil {
		return h.opts.Now()
	}
	return time.Now()
}
