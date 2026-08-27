package deb

// The index engine (debian.md sections 2 / 3.3 / 4 / 5): the full
// recomputation one distribution goes through, invoked by the management
// plane (httpapi's /api/deb family, whole-repository) and — unlike rpm's
// RP-2 opt-in — by EVERY debPUT/delete on the automatic local repository
// (the FR-97.1 chain: upload registers coordinates, the engine rebuilds
// the affected dist's Packages/Sources + compression set + By-Hash
// copies + Release, asynchronously — the PUT's 201 never waits on it).
//
// One distribution's run, in the spec's order:
//
//  1. collect: walk the repository's nodes, group every .deb/.dsc by its
//     stored coordinate properties (the cartesian product of the three
//     axes — a package registered for stable/main/{amd64,i386} lands in
//     four index contexts), parsing each body's control paragraph;
//  2. render: per (component, architecture) the Packages body (plus the
//     compression set: plain + .gz always, the optional formats default
//     ["bz2"]), per component the Sources body when source packages
//     exist; the forced architecture families (TL-4: i386,amd64 by
//     default) generate EMPTY Packages too — the empty-index guarantee
//     the board's final ruling pins;
//  3. write by-hash mirrors first, then the canonical files, then the
//     Release (section 5's ordering: an apt mid-update reads either the
//     old canonical name or the new by-hash address, never a torn one);
//  3b. the signature pair (T-321, sign.go): a repository whose keypair
//     seam resolves writes InRelease (clearsign) + Release.gpg (detached
//     armor) of the Release just landed;
//  4. signature sweep: an unsigned (or rotated) recompute deletes stale
//     Release.gpg / InRelease (DB-1 — an old signature must not outlive
//     the Release it signed; the freshly written pair is exempt);
//  5. retention: keep the newest historyCycles by-hash GENERATIONS per
//     algorithm directory (section 5: the current plus history; the
//     current generation never prunes), drop canonical files the new
//     generation no longer names.
//
// The per-repository index lock (the helm/rpm posture) serializes runs
// against concurrent uploads and management reindexes.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
	"github.com/ulikunitz/xz/lzma"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// ---- repository configuration (the deb section of the config blob) ----

// The by-hash policy values (debian.md section 5).
const (
	byHashAll    = "ALL"
	byHashSHA256 = "SHA256"
	byHashNone   = "NONE"
)

// RepoConfig is the deb section of the repository's config blob. Every
// field optional; every default the spec's or the board ruling's.
type RepoConfig struct {
	// ByHash is the by-hash policy: ALL / SHA256 / NONE. Default NONE —
	// the public DebianRepository/Format default (Acquire-By-Hash
	// absent); docs/reverse does not pin Artifactory's own default, the
	// divergence register carries the note.
	ByHash string `json:"byHash"`
	// OptionalIndexCompressionFormats are the optional compression
	// spellings beyond the mandatory plain + .gz (section 2.1: default
	// ["bz2"]). T-314 restored the renderable subset: xz and lzma write
	// through the ulikunitz/xz dependency already on the deb parse path.
	// DIVERGENCE (carried from T-310, still registered): bz2 has no writer
	// in the dependency set and the network-isolated build cannot add one
	// (dsnet/compress absent from the module cache) — a configured "bz2"
	// degrades away with one WARN, and the default set (["bz2"]) therefore
	// still renders plain + .gz only. apt needs any ONE index form (the
	// format marks every compression optional).
	OptionalIndexCompressionFormats []string `json:"optionalIndexCompressionFormats"`
	// DefaultArchitectures is TL-4's forced architecture family set:
	// these families' Packages files generate for every component even
	// when empty. Default "i386,amd64" (the board's final ruling; the
	// PRD's "default off" was overturned). "" disables forcing.
	DefaultArchitectures string `json:"debianDefaultArchitectures"`
	// HistoryCycles bounds the by-hash generations kept per algorithm
	// directory (section 5: current plus history; official floor is 2).
	HistoryCycles int `json:"historyCycles"`
	// Origin / Label feed the Release header fields (section 4.1: the
	// repository configuration, falling back to the repository key).
	Origin string `json:"origin"`
	Label  string `json:"label"`
}

// config defaults.
const (
	defaultByHash        = byHashNone
	defaultArchitectures = "i386,amd64"
	defaultHistoryCycles = 3
)

// maxIndexReadBytes bounds one stored body read (the control member
// probe of a pathological .deb).
const maxIndexReadBytes = 96 << 20

// parseRepoConfig reads the section off the config blob; a malformed
// blob means the defaults (the config plane's own validation owns hard
// errors).
func parseRepoConfig(config string) RepoConfig {
	var probe RepoConfig
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return RepoConfig{}
	}
	return probe
}

// normalized returns the config with defaults applied. The optional
// compression set filters to the RENDERABLE spellings (xz / lzma since
// T-314); "bz2" still carries no writer in this dependency set and is
// dropped here (the registered divergence, see the field comment) — a
// name that parses but cannot render would produce broken companion
// files, so the default ["bz2"] degrades to the mandatory plain + .gz
// pair every apt accepts.
func (c RepoConfig) normalized() RepoConfig {
	switch c.ByHash {
	case byHashAll, byHashSHA256:
	default:
		c.ByHash = defaultByHash
	}
	if c.HistoryCycles <= 0 {
		c.HistoryCycles = defaultHistoryCycles
	}
	// TL-4's final ruling: the forced families default ON ("i386,amd64");
	// the explicit "none" spelling is the opt-out.
	if strings.TrimSpace(c.DefaultArchitectures) == "" {
		c.DefaultArchitectures = defaultArchitectures
	}
	c.OptionalIndexCompressionFormats = c.renderableCompanions()
	return c
}

// renderableCompanions keeps the configured optional compression names
// this release can actually render (deterministic order: the spellings
// sorted), silently dropping duplicates and unknown or unrenderable
// spellings — the bz2 gap is WARN-logged at config load (configFor), not
// per file.
func (c RepoConfig) renderableCompanions() []string {
	var out []string
	for _, f := range c.OptionalIndexCompressionFormats {
		name := strings.TrimSpace(f)
		if _, ok := companionWriters[name]; !ok {
			continue
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// droppedCompanions lists the configured optional spellings that cannot
// render in this release (the WARN set).
func (c RepoConfig) droppedCompanions() []string {
	var out []string
	for _, f := range c.OptionalIndexCompressionFormats {
		name := strings.TrimSpace(f)
		if _, ok := companionWriters[name]; !ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

// forcedArches splits DefaultArchitectures into the sorted forced set
// ("none" — the explicit opt-out — answers empty).
func (c RepoConfig) forcedArches() []string {
	if strings.EqualFold(strings.TrimSpace(c.DefaultArchitectures), "none") {
		return nil
	}
	var out []string
	for _, a := range strings.Split(c.DefaultArchitectures, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// companionWriters are the optional index compression spellings this
// release can render (deterministic output: same body in, same bytes
// out — the by-hash digests key on that). xz and lzma ride the
// ulikunitz/xz dependency the .deb parse path already carries; bz2 stays
// the registered gap (no writer in the dependency set, network-isolated
// build).
var companionWriters = map[string]func([]byte) []byte{
	"xz":   xzBody,
	"lzma": lzmaBody,
}

// byHashEnabled reports the Acquire-By-Hash posture (any policy but
// NONE).
func byHashEnabled(policy string) bool { return policy == byHashAll || policy == byHashSHA256 }

// ---- the engine ----

// distIndex is one distribution's collected facts.
type distIndex struct {
	bins map[binKey][]pkgEntry // (component, architecture) -> stanzas
	srcs map[string][]srcEntry // component -> stanzas
}

// binKey is one binary index context.
type binKey struct{ comp, arch string }

// collect walks the repository and builds every distribution's index
// facts. skipped counts the files the engine passed over (no stored
// coordinates, or a body whose control paragraph failed to parse).
func (h *Handler) collect(ctx context.Context, p *repo.Principal, repoKey string) (map[string]*distIndex, int, error) {
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		return nil, 0, fmt.Errorf("list %s: %w", repoKey, err)
	}
	out := map[string]*distIndex{}
	distOf := func(name string) *distIndex {
		d, ok := out[name]
		if !ok {
			d = &distIndex{bins: map[binKey][]pkgEntry{}, srcs: map[string][]srcEntry{}}
			out[name] = d
		}
		return d
	}
	skipped := 0
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		switch {
		case strings.HasSuffix(n.Path, suffixDeb):
			if err := h.collectDeb(ctx, p, repoKey, n, distOf, &skipped); err != nil {
				return nil, 0, err
			}
		case strings.HasSuffix(n.Path, suffixDsc):
			if err := h.collectDsc(ctx, p, repoKey, n, distOf, &skipped); err != nil {
				return nil, 0, err
			}
		}
	}
	return out, skipped, nil
}

// propsOf reads one node's stored coordinate properties.
func (h *Handler) propsOf(ctx context.Context, repoKey, path string) map[string][]string {
	if h.props == nil {
		return nil
	}
	props, err := h.props.List(ctx, repoKey, path)
	if err != nil {
		slog.WarnContext(ctx, "deb: coordinate properties unreadable",
			slog.String("repo", repoKey), slog.String("path", path), slog.String("error", err.Error()))
		return nil
	}
	return props
}

// collectDeb folds one .deb into the distributions its coordinates name
// (the cartesian product — a multi-value axis registers the file in
// every named context).
func (h *Handler) collectDeb(ctx context.Context, p *repo.Principal, repoKey string, n *metadata.Node, distOf func(string) *distIndex, skipped *int) error {
	raw := h.propsOf(ctx, repoKey, n.Path)
	coords := coordinates{
		distributions: raw[PropDebDistribution],
		components:    raw[PropDebComponent],
		architectures: raw[PropDebArchitecture],
	}
	if !coords.complete() {
		*skipped++ // no (complete) coordinates: stored but never indexed
		return nil
	}
	entry := pkgEntry{path: n.Path, sha256: n.Sha256, size: n.Size}
	if h.blobs != nil && n.Sha256 != "" {
		if b, err := h.blobs.Get(ctx, n.Sha256); err == nil && b != nil {
			entry.sha1, entry.md5 = b.Sha1, b.Md5
		}
	}
	if err := h.parseDebNode(ctx, p, repoKey, n.Path, &entry); err != nil {
		*skipped++
		slog.WarnContext(ctx, "deb: index skipped an unparsable package",
			slog.String("repo", repoKey), slog.String("path", n.Path), slog.String("error", err.Error()))
	}
	for _, d := range coords.distributions {
		for _, comp := range coords.components {
			for _, a := range coords.architectures {
				distOf(d).bins[binKey{comp: comp, arch: a}] = append(
					distOf(d).bins[binKey{comp: comp, arch: a}], entry)
			}
		}
	}
	return nil
}

// parseDebNode reads a stored .deb and parses its control paragraph into
// the entry; a parse failure leaves the entry unindexable (the caller
// decides warn-and-skip).
func (h *Handler) parseDebNode(ctx context.Context, p *repo.Principal, repoKey, nodePath string, entry *pkgEntry) error {
	rc, _, err := h.svc.Get(ctx, p, repoKey, nodePath)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	doc, err := parseDebControl(io.LimitReader(rc, maxIndexReadBytes))
	if err != nil {
		return err
	}
	if doc == nil || doc.Get("Package") == "" || doc.Get("Version") == "" {
		return fmt.Errorf("%w: control paragraph lacks Package/Version", ErrNotDebArchive)
	}
	entry.doc = doc
	return nil
}

// collectDsc folds one .dsc in (architecture is the server's: source).
func (h *Handler) collectDsc(ctx context.Context, p *repo.Principal, repoKey string, n *metadata.Node, distOf func(string) *distIndex, skipped *int) error {
	raw := h.propsOf(ctx, repoKey, n.Path)
	dists, comps := raw[PropDscDistribution], raw[PropDscComponent]
	if len(dists) == 0 || len(comps) == 0 {
		*skipped++
		return nil
	}
	entry := srcEntry{path: n.Path}
	rc, _, err := h.svc.Get(ctx, p, repoKey, n.Path)
	if err != nil {
		return err
	}
	doc, perr := parseDsc(io.LimitReader(rc, maxControlBytes))
	_ = rc.Close() //nolint:errcheck // read-only fd
	entry.doc = doc
	if perr != nil || !entry.indexable() {
		*skipped++
		slog.WarnContext(ctx, "deb: index skipped an unparsable source package",
			slog.String("repo", repoKey), slog.String("path", n.Path), slog.String("error", fmt.Sprintf("%v", perr)))
		return nil
	}
	for _, d := range dists {
		for _, comp := range comps {
			distOf(d).srcs[comp] = append(distOf(d).srcs[comp], entry)
		}
	}
	return nil
}

// ReindexRepository recomputes EVERY distribution's index tree (the
// management plane's whole-repository run; the per-repository index lock
// serializes it against the upload-triggered recomputes).
func (h *Handler) ReindexRepository(ctx context.Context, p *repo.Principal, repoKey string) error {
	return h.withIndexLock(repoKey, func() error {
		cfg, err := h.configFor(ctx, repoKey)
		if err != nil {
			return err
		}
		byDist, skipped, err := h.collect(ctx, p, repoKey)
		if err != nil {
			return err
		}
		dists := make([]string, 0, len(byDist))
		for d := range byDist {
			dists = append(dists, d)
		}
		sort.Strings(dists)
		for _, d := range dists {
			if err := h.reindexDistLocked(ctx, p, repoKey, d, byDist[d], cfg); err != nil {
				return err
			}
		}
		slog.InfoContext(ctx, "deb: repository reindex complete",
			slog.String("repo", repoKey), slog.Int("distributions", len(dists)), slog.Int("skipped", skipped))
		return nil
	})
}

// recomputeDists is the upload/delete chain's entry: the same engine on
// the coordinate distributions only, dispatched in the background (the
// async posture of section 3.3 — the PUT's 201 never waits on it).
func (h *Handler) recomputeDists(ctx context.Context, p *repo.Principal, repoKey string, dists []string) {
	if len(dists) == 0 {
		return
	}
	principal := p
	//nolint:gosec // G118: the recompute deliberately detaches from the
	// request's lifetime — the async posture must survive the client
	// hanging up, and the per-repo index lock serializes runs.
	go func() {
		bg := context.Background()
		if err := h.withIndexLock(repoKey, func() error {
			cfg, err := h.configFor(bg, repoKey)
			if err != nil {
				return err
			}
			byDist, _, err := h.collect(bg, principal, repoKey)
			if err != nil {
				return err
			}
			for _, d := range dists {
				if err := h.reindexDistLocked(bg, principal, repoKey, d, byDist[d], cfg); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			slog.ErrorContext(ctx, "deb: automatic index recompute failed",
				slog.String("repo", repoKey), slog.String("error", err.Error()))
		}
	}()
}

// configFor loads the repository row and its deb section. A configured
// optional compression spelling this release cannot render (bz2, the
// registered gap) degrades away with one WARN per run — never a broken
// companion file.
func (h *Handler) configFor(ctx context.Context, repoKey string) (RepoConfig, error) {
	row, err := h.repos.Get(ctx, repoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return RepoConfig{}, errRepoNotFound(repoKey)
		}
		return RepoConfig{}, fmt.Errorf("load repository %s: %w", repoKey, err)
	}
	cfg := parseRepoConfig(row.Config)
	for _, name := range cfg.droppedCompanions() {
		slog.WarnContext(ctx, "deb: optional index compression has no writer in this release — degraded (plain + .gz always render)",
			slog.String("repo", repoKey), slog.String("format", name))
	}
	return cfg.normalized(), nil
}

// reindexDistLocked runs one distribution's full recomputation. di may
// be nil (the distribution vanished — the sweep below empties its tree).
func (h *Handler) reindexDistLocked(ctx context.Context, p *repo.Principal, repoKey, dist string, di *distIndex, cfg RepoConfig) error {
	if di == nil {
		di = &distIndex{bins: map[binKey][]pkgEntry{}, srcs: map[string][]srcEntry{}}
	}
	forced := cfg.forcedArches()

	// The component set: every component a binary or source entry names.
	compSet := map[string]bool{}
	for k := range di.bins {
		compSet[k.comp] = true
	}
	for c := range di.srcs {
		compSet[c] = true
	}
	comps := sortedKeys(compSet)

	// The architecture line: the union of the binary coordinates plus the
	// forced families, pseudo architectures filtered (section 4.1).
	archSet := map[string]bool{}
	for k := range di.bins {
		archSet[k.arch] = true
	}
	arches := archLine(sortedKeys(archSet), forced)

	// 2. Render.
	var files []indexFile
	add := func(rel string, body []byte, ctype string) {
		files = append(files, newIndexFile(rel, body, ctype))
	}
	distRoot := dirDists + "/" + dist
	for _, comp := range comps {
		// The binary families: every architecture the entries named for
		// this component (pseudo ones included — binary-all is a real
		// index family) plus the forced set (TL-4: empty Packages too).
		binArchSet := map[string]bool{}
		for k := range di.bins {
			if k.comp == comp {
				binArchSet[k.arch] = true
			}
		}
		for _, a := range forced {
			binArchSet[a] = true
		}
		for _, arch := range sortedKeys(binArchSet) {
			entries := sortableEntries(di.bins, comp, arch)
			base := distRoot + "/" + comp + "/binary-" + arch
			body := renderPackagesBody(entries)
			add(base+"/Packages", body, indexContentType("Packages"))
			add(base+"/Packages.gz", gzipBody(body), indexContentType("Packages.gz"))
			for _, name := range cfg.OptionalIndexCompressionFormats {
				add(base+"/Packages."+name, companionBody(name, body), indexContentType("Packages."+name))
			}
		}
		if srcs := di.srcs[comp]; len(srcs) > 0 {
			sort.Slice(srcs, func(i, j int) bool { return srcs[i].path < srcs[j].path })
			base := distRoot + "/" + comp + "/" + archSource
			body := renderSourcesBody(srcs)
			add(base+"/Sources", body, indexContentType("Sources"))
			add(base+"/Sources.gz", gzipBody(body), indexContentType("Sources.gz"))
			for _, name := range cfg.OptionalIndexCompressionFormats {
				add(base+"/Sources."+name, companionBody(name, body), indexContentType("Sources."+name))
			}
		}
	}

	// An emptied distribution (no components left) keeps no skeleton: no
	// Release, no index family — the sweep below clears the whole tree
	// (a Release advertising zero components would serve apt nothing but
	// confusion).
	if len(comps) == 0 {
		if err := h.sweepDist(ctx, p, repoKey, distRoot, nil, indexFile{}, nil, cfg); err != nil {
			return err
		}
		slog.InfoContext(ctx, "deb: emptied distribution swept",
			slog.String("repo", repoKey), slog.String("dist", dist))
		return nil
	}

	// The Release: paths relative to dists/<dist>/ (section 4.1's
	// "relative to the Release file" rule).
	relFiles := make([]indexFile, 0, len(files))
	for _, f := range files {
		relFiles = append(relFiles, indexFile{
			path:    strings.TrimPrefix(f.path, distRoot+"/"),
			body:    f.body,
			ctype:   f.ctype,
			digests: f.digests,
		})
	}
	origin := cfg.Origin
	if origin == "" {
		origin = repoKey
	}
	label := cfg.Label
	if label == "" {
		label = repoKey
	}
	release := newIndexFile(distRoot+"/Release", renderReleaseBody(releaseDoc{
		dist:       dist,
		components: comps,
		arches:     arches,
		policy:     cfg.ByHash,
		origin:     origin,
		label:      label,
		date:       h.now(),
		files:      relFiles,
	}), "text/plain; charset=utf-8")

	// 3. Write: by-hash mirrors first, then the canonical files, then
	// Release (section 5's ordering).
	if byHashEnabled(cfg.ByHash) {
		for _, f := range files {
			if err := h.writeByHashCopies(ctx, p, repoKey, f, cfg.ByHash); err != nil {
				return err
			}
		}
	}
	for _, f := range files {
		if err := h.writeIndexFile(ctx, p, repoKey, f); err != nil {
			return err
		}
	}
	if err := h.writeIndexFile(ctx, p, repoKey, release); err != nil {
		return err
	}

	// 3b. The signature pair (T-321): the Release is the commit point, the
	// signatures land beside it. The unsigned posture writes nothing here
	// and lets the sweep clear whatever stale signatures remain (DB-1).
	var sigs []indexFile
	inRel, relGpg, signed, serr := h.releaseSignatures(ctx, repoKey, release.body)
	if serr != nil {
		return serr
	}
	if signed {
		sigs = append(sigs,
			newIndexFile(distRoot+"/InRelease", inRel, ctypeInRelease),
			newIndexFile(distRoot+"/Release.gpg", []byte(relGpg), ctypeReleaseGpg),
		)
		for _, f := range sigs {
			if err := h.writeIndexFile(ctx, p, repoKey, f); err != nil {
				return err
			}
		}
	}

	// 4-5. The signature sweep and the retention/stale sweep.
	if err := h.sweepDist(ctx, p, repoKey, distRoot, files, release, sigs, cfg); err != nil {
		return err
	}

	slog.InfoContext(ctx, "deb: distribution reindex complete",
		slog.String("repo", repoKey), slog.String("dist", dist),
		slog.Int("components", len(comps)), slog.Int("index-files", len(files)))
	return nil
}

// sortableEntries renders one (comp, arch) bucket path-ordered.
func sortableEntries(bins map[binKey][]pkgEntry, comp, arch string) []pkgEntry {
	entries := bins[binKey{comp: comp, arch: arch}]
	out := make([]pkgEntry, 0, len(entries))
	for _, e := range entries {
		if e.indexable() {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// sortedKeys renders a string set sorted.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// gzipBody renders the .gz companion (deterministic: no timestamp —
// same content, same bytes, same digest every run).
func gzipBody(body []byte) []byte {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	// The header's ModTime stays zero (deterministic output).
	if _, err := zw.Write(body); err != nil {
		return nil // unreachable: a bytes.Buffer write never fails
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

// companionBody renders one optional compression spelling (normalized()
// has already filtered the set to companionWriters' keys).
func companionBody(name string, body []byte) []byte {
	if fn, ok := companionWriters[name]; ok {
		return fn(body)
	}
	return nil
}

// xzBody renders the .xz companion (deterministic: the xz stream header
// carries no timestamp; same body in, same bytes out).
func xzBody(body []byte) []byte {
	var buf bytes.Buffer
	zw, err := xz.NewWriter(&buf)
	if err != nil {
		return nil // unreachable: a bytes.Buffer constructor never fails
	}
	if _, err := zw.Write(body); err != nil {
		return nil // unreachable: see above
	}
	if err := zw.Close(); err != nil {
		return nil // unreachable: see above
	}
	return buf.Bytes()
}

// lzmaBody renders the .lzma companion (the LZMA-alone format, the
// legacy apt spelling; deterministic like xzBody).
func lzmaBody(body []byte) []byte {
	var buf bytes.Buffer
	zw, err := lzma.NewWriter(&buf)
	if err != nil {
		return nil // unreachable: see xzBody
	}
	if _, err := zw.Write(body); err != nil {
		return nil // unreachable: see xzBody
	}
	if err := zw.Close(); err != nil {
		return nil // unreachable: see xzBody
	}
	return buf.Bytes()
}

// writeIndexFile lands one rendered file at its canonical path (the
// regenerable-content posture: SkipOverwriteCheck, measured digest).
func (h *Handler) writeIndexFile(ctx context.Context, p *repo.Principal, repoKey string, f indexFile) error {
	ref := storage.BlobRef{Sha256: f.digests.sha256}
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, f.path,
		bytes.NewReader(f.body), ref, f.ctype,
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write index %s: %w", f.path, err)
	}
	return nil
}

// byHashAddresses lists one index file's digest-named mirror addresses
// beside it (same directory). The policy owns the algorithm set: SHA256
// serves the SHA256 family only (debian.md section 5). The sweep's
// current-generation protection derives the same addresses (T-327G) —
// one spelling of the path grammar, never two.
func byHashAddresses(f indexFile, policy string) []string {
	dir := parentDir(f.path)
	algos := []struct {
		name string
		hex  string
	}{
		{"MD5Sum", f.digests.md5},
		{"SHA1", f.digests.sha1},
		{"SHA256", f.digests.sha256},
	}
	if policy == byHashSHA256 {
		algos = algos[2:]
	}
	out := make([]string, 0, len(algos))
	for _, a := range algos {
		out = append(out, dir+"/"+dirByHash+"/"+a.name+"/"+a.hex)
	}
	return out
}

// writeByHashCopies lands one index file's digest-named mirrors beside
// it (same directory; the blob layer dedupes the identical bytes).
func (h *Handler) writeByHashCopies(ctx context.Context, p *repo.Principal, repoKey string, f indexFile, policy string) error {
	for _, path := range byHashAddresses(f, policy) {
		ref := storage.BlobRef{Sha256: f.digests.sha256}
		if _, err := h.svc.PutWithOptions(ctx, p, repoKey, path,
			bytes.NewReader(f.body), ref, f.ctype,
			repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
			return fmt.Errorf("write by-hash %s: %w", path, err)
		}
	}
	return nil
}

// sweepDist runs the signature sweep and the stale-file retention: the
// freshly written signature pair (sigs) survives, stale signature files
// never outlive an unsigned or rotated recompute (DB-1), canonical family
// files the new generation does not name go, and by-hash digests keep the
// newest historyCycles GENERATIONS per algorithm directory (the current
// generation never prunes; whole by-hash trees of vanished index
// directories go).
func (h *Handler) sweepDist(ctx context.Context, p *repo.Principal, repoKey, distRoot string, files []indexFile, release indexFile, sigs []indexFile, cfg RepoConfig) error {
	nodes, err := h.svc.List(ctx, p, repoKey, distRoot)
	if err != nil {
		return fmt.Errorf("list %s: %w", distRoot, err)
	}
	current := map[string]bool{release.path: true}
	liveDirs := map[string]bool{} // index directories the new generation serves
	for _, f := range files {
		current[f.path] = true
		liveDirs[parentDir(f.path)] = true
	}
	// The current generation's by-hash addresses (T-327G): the digests
	// this run just wrote, spelled exactly as writeByHashCopies writes
	// them. Under a disabled policy nothing was written, so nothing is
	// protected — the dormant tree of a flipped-off policy ages out
	// through the stale generations like any other history.
	currentGen := map[string]bool{}
	if byHashEnabled(cfg.ByHash) {
		for _, f := range files {
			for _, path := range byHashAddresses(f, cfg.ByHash) {
				currentGen[path] = true
			}
		}
	}
	for _, s := range sigs {
		current[s.path] = true // this generation's signatures survive; an unsigned recompute passes nil here
	}
	byHashBuckets := map[string][]*metadata.Node{} // the by-hash/<ALGO> directory -> its digest files
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		switch {
		case current[n.Path]:
			continue
		case n.Path == distRoot+"/Release.gpg" || n.Path == distRoot+"/InRelease":
			// DB-1: an unsigned (or rotated) recompute — a stale signature
			// must not outlive the Release it signed.
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("sweep stale signature %s: %w", n.Path, err)
			}
		case inByHash(n.Path):
			algoDir := parentDir(n.Path) // .../<index-dir>/by-hash/<ALGO>
			indexDir := dirOf(dirOf(algoDir))
			if !liveDirs[indexDir] {
				// The index directory vanished: the whole history tree goes.
				if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
					return fmt.Errorf("sweep orphan by-hash %s: %w", n.Path, err)
				}
				continue
			}
			byHashBuckets[algoDir] = append(byHashBuckets[algoDir], n)
		default:
			// A canonical-family file the new generation does not name (a
			// vanished component/architecture, a dropped optional format).
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("sweep stale index %s: %w", n.Path, err)
			}
		}
	}
	// By-hash retention (T-327G): the unit is the GENERATION, not the
	// entry — one run lands a whole index family's compression spellings
	// (plain + .gz + the optional set) into each by-hash/<ALGO> directory,
	// so the entry-count window this sweep used before could prune the
	// CURRENT generation's copies whenever historyCycles fell below the
	// spelling count (the registered T-327R defect: apt mid-update loses a
	// digest the Release still advertises).
	for _, ns := range byHashBuckets {
		for _, n := range byHashPrunePlan(ns, currentGen, cfg.HistoryCycles) {
			if err := h.svc.Delete(ctx, p, repoKey, n.Path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
				return fmt.Errorf("prune by-hash %s: %w", n.Path, err)
			}
		}
	}
	return nil
}

// byHashPrunePlan returns one algorithm directory's by-hash entries the
// retention window drops (debian.md section 5: the current version must
// stay fetchable; history keeps historyCycles generations, oldest beyond
// the window pruned — the generation-count implementation is the spec's
// medium-confidence arm).
//
// The current generation (current, keyed by digest address) never prunes,
// however low historyCycles runs — and it consumes one slot: the window
// bounds the directory's TOTAL generations, so cycles=1 keeps the current
// generation alone and every stale entry goes. The stale entries group
// into generations by their write timestamp: node timestamps are RFC3339
// seconds, so one run's copies share one (the grouping key); fast
// successive runs inside a second MERGE — one generation counted as two
// would over-prune, two as one merely keeps extra history, the safe
// direction. historyCycles <= 0 (only reachable by direct call; the
// engine's config floor normalizes it) keeps none of the stale entries.
func byHashPrunePlan(ns []*metadata.Node, current map[string]bool, historyCycles int) []*metadata.Node {
	stale := make([]*metadata.Node, 0, len(ns))
	for _, n := range ns {
		if !current[n.Path] {
			stale = append(stale, n)
		}
	}
	// Deterministic order: recency first, the path as the tiebreak —
	// second-granularity timestamps tie fast successive runs (the rpm
	// posture).
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].UpdatedAt != stale[j].UpdatedAt {
			return stale[i].UpdatedAt > stale[j].UpdatedAt
		}
		return stale[i].Path > stale[j].Path
	})
	var prune []*metadata.Node
	gen := 0 // 0 = the newest stale generation
	for i, n := range stale {
		if i > 0 && n.UpdatedAt != stale[i-1].UpdatedAt {
			gen++
		}
		if gen < historyCycles-1 {
			continue
		}
		prune = append(prune, n)
	}
	return prune
}

// inByHash reports whether a path sits inside a by-hash/ tree.
func inByHash(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == dirByHash {
			return true
		}
	}
	return false
}

// dirOf strips the final segment ("" at the root).
func dirOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}

// ---- the per-repository index lock (the helm/rpm refcounted posture) ----

// indexMutexes serializes the index rewrites per repository key.
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
