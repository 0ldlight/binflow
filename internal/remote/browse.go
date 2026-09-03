package remote

// The remote-browsing enumeration engine (M16 T-442, FR-147.1; the seam
// docs/reverse/remote-browsing.md specifies for the `listRemoteFolderItems`
// optional档's batch 1): given one remote repository and a folder, enumerate
// the UPSTREAM's children of that folder as display-only rows.
//
// Scope and semantics (remote-browsing.md anchors):
//
//   - Batch 1 covers the three "one document = whole tree" package types —
//     helm classic (index.yaml), debian (dists/<suite> metadata) and rpm
//     (repodata/repomd.xml + primary). BrowseSupported answers the set; the
//     other ten types have no root-level enumeration API and are refused
//     (§3's "不可行" rows — rendering a fake tree is worse than an honest
//     refusal).
//   - THE OPTIONAL FLAG IS THE CALLER'S: this engine never reads
//     listRemoteFolderItems. The flag defaults to false and the off posture
//     is "nobody calls BrowseRemote" — behavior diff zero by construction
//     (T-448 wires the repo-service leg that decides to call).
//   - Authorization rides BrowsePermit, the caller's own allow() handed in
//     as a closure — the ACL source is literally the same function the
//     content plane consults, and a nil or refusing permit answers
//     ErrBrowseDenied with ZERO upstream contact (越权仓零枚举).
//   - Upstream contact is read-only GETs through the engine's own guarded
//     client (credentials, socket timeout, SSRF chain — clientFor), and the
//     enumeration writes NOTHING: no node rows, no cache-state rows, no
//     negative cache. Derived rows are display-only (§4-3); the parsed tree
//     lives in an in-process TTL cache mapped onto the repository's
//     metadataRetrievalCachePeriodSecs (§1: the official "cached per the
//     Metadata Retrieval Cache Period" semantics — one fetch per window).
//   - Degradation (§4-1/§4-2): an upstream fault (transport, 5xx, refused)
//     does NOT fail the call — the answer carries Degraded with the fault
//     summary and no entries, so the caller keeps serving the cached rows
//     beside an error-layer note instead of a collapsed tree. Transport and
//     5xx faults open the same assumed-offline window the pull-through
//     shares, so a dead upstream is probed at most once per silence period.
//   - Index documents ride the client's 64 MiB buffered-body cap
//     (DefaultMaxBufferedBody — the C-layer parameter remote-browsing.md
//     R-d asked this ticket to pin) plus a 256 MiB inflation cap for the
//     compressed deb/rpm indexes (Fedora/EPEL-scale raw primary.xml fits;
//     a gzip bomb cannot balloon process memory), and the derived path set
//     carries its own bound.

import (
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"go.yaml.in/yaml/v3"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// Browse batch-1 package types (repo.PackageHelm / keypair_config.go's
// "debian" and "rpm" spellings mirrored — the import would cycle).
const (
	browseTypeHelm = "helm"
	browseTypeDeb  = "debian"
	browseTypeRpm  = "rpm"
)

// browseDefaultMetadataTTL is the enumeration cache window when the
// repository row carries no metadata TTL (the 001 DDL default is 600s;
// hand-built rows can carry zero).
const browseDefaultMetadataTTL = 600 * time.Second

// browseMaxInflated bounds one DECOMPRESSED index document (§4-4's index
// protection): compressed deb/rpm indexes are fetched under the client's
// 64 MiB cap, and the inflation below refuses beyond this bound.
const browseMaxInflated = 256 << 20

// browseMaxPaths bounds one enumerated tree: past this the index is
// treated as a fault, not a tree (a hostile document could otherwise
// materialize millions of display rows).
const browseMaxPaths = 500_000

// BrowsePermit is the ACL seam: the caller hands its own allow() in as a
// closure, so the enumeration's authorization source is the very function
// the content plane consults (T-442 AC2's "ACL 同 allow() 源"). It is
// consulted once, with the normalized folder, BEFORE anything else — a nil
// or refusing permit answers ErrBrowseDenied with zero upstream contact
// and zero rows revealed, cached or not.
type BrowsePermit func(folder string) bool

// BrowseEntry is one display-only remote-derived row (§4-3: synthesized per
// query, never landed — zero node/blob writes). Path is the repository-
// relative path WITHOUT a trailing slash; IsFolder marks intermediate
// directories implied by deeper paths.
type BrowseEntry struct {
	Path     string
	IsFolder bool
}

// BrowseResult is one folder enumeration. Degraded is non-empty when the
// remote layer entered its error state (upstream fault or the
// assumed-offline window): Entries is then empty and the caller serves the
// local cache rows with this note as the error layer (§4-1) — never a
// failed listing.
type BrowseResult struct {
	Entries  []BrowseEntry
	Degraded string
}

// The enumeration's face-level refusals (mapped by the caller; distinct
// sentinels so T-448's wiring can pick its own statuses).
var (
	// ErrBrowseDenied: the permit refused the folder (or was nil — fail
	// closed, the allow() posture's own rule).
	ErrBrowseDenied = errors.New("remote browse: folder not permitted for this principal")
	// ErrBrowseUnsupportedType: the package type has no root-level upstream
	// enumeration (batch 1 = helm, debian, rpm).
	ErrBrowseUnsupportedType = errors.New("remote browse: package type has no remote enumeration (batch 1: helm, debian, rpm)")
	// ErrBrowseInvalidFolder: the folder spelling is not a legal node path.
	ErrBrowseInvalidFolder = errors.New("remote browse: invalid folder path")
)

// BrowseSupported reports whether a package type belongs to the
// remote-browsing batch 1 (the caller's gate for wiring the optional档).
func BrowseSupported(packageType string) bool {
	switch packageType {
	case browseTypeHelm, browseTypeDeb, browseTypeRpm:
		return true
	default:
		return false
	}
}

// BrowseRemote enumerates the upstream children of one folder on a remote
// repository. The parsed whole-tree snapshot is cached in-process for the
// repository's metadata TTL, so one browsing session costs the upstream at
// most one index fetch per window per repository.
func (e *Engine) BrowseRemote(ctx context.Context, permit BrowsePermit, repoKey, folder string) (*BrowseResult, error) {
	folder, err := normalizeBrowseFolder(folder)
	if err != nil {
		return nil, fmt.Errorf("browse %s: %w", repoKey, err)
	}
	// The permit runs first: an unauthorized caller learns nothing about
	// the upstream tree — not even a cached snapshot (AC2's zero-leak
	// probe point).
	if permit == nil || !permit(folder) {
		return nil, fmt.Errorf("browse %s/%s: %w", repoKey, folder, ErrBrowseDenied)
	}
	row, cfg, pol, err := e.loadRepo(ctx, repoKey)
	if err != nil {
		return nil, err
	}
	if !BrowseSupported(row.PackageType) {
		return nil, fmt.Errorf("browse %s: %w: %q", repoKey, ErrBrowseUnsupportedType, row.PackageType)
	}

	// A fresh local snapshot answers without any upstream packet — the
	// enumeration's own copy of the pull-through's step-4 posture (the
	// offline window below governs upstream CONTACT, and a fresh local
	// tree needs none).
	if tree, ok := e.browseSnapshot(repoKey, cfg); ok {
		return &BrowseResult{Entries: browseChildren(tree.paths, folder)}, nil
	}
	// §4-2: inside the assumed-offline silence the enumeration presents
	// its error state and does not touch the upstream.
	if _, offline := e.offlineWindow(repoKey, e.now()); offline {
		return &BrowseResult{Degraded: fmt.Sprintf(
			"remote enumeration unavailable: repository '%s' is assumed offline", repoKey)}, nil
	}

	// Serialize refreshes per process (browsing is a rare UI action; a
	// stampede would multiply 64 MiB index fetches for no benefit). The
	// cache is re-checked under the lock so a waitor serves the winner's
	// snapshot.
	e.browseMu.Lock()
	defer e.browseMu.Unlock()
	if tree, ok := e.browseSnapshotLocked(repoKey, cfg); ok {
		return &BrowseResult{Entries: browseChildren(tree.paths, folder)}, nil
	}

	var paths []string
	var fault *browseFault
	switch row.PackageType {
	case browseTypeHelm:
		paths, fault = e.browseHelm(ctx, repoKey, cfg, pol)
	case browseTypeDeb:
		paths, fault = e.browseDeb(ctx, repoKey, cfg, pol)
	case browseTypeRpm:
		paths, fault = e.browseRpm(ctx, repoKey, cfg, pol)
	}
	if fault != nil && fault.unfound {
		// The protocol enumerators absorb their own unfound arms; this is
		// the belt-and-braces spelling (an unfound index degrades nothing).
		return &BrowseResult{}, nil
	}
	if fault != nil {
		if fault.offline {
			e.markOffline(repoKey, e.now().Add(time.Duration(offlineSecs(pol))*time.Second))
		}
		e.log.WarnContext(ctx, "remote: browse enumeration degraded",
			slog.String("repo", repoKey), slog.String("folder", folder),
			slog.String("reason", fault.msg))
		return &BrowseResult{Degraded: "remote enumeration unavailable: " + fault.msg}, nil
	}
	if len(paths) > browseMaxPaths {
		e.log.WarnContext(ctx, "remote: browse tree exceeded the path bound",
			slog.String("repo", repoKey), slog.Int("paths", len(paths)))
		return &BrowseResult{Degraded: fmt.Sprintf(
			"remote enumeration unavailable: upstream index enumerates more than %d paths", browseMaxPaths)}, nil
	}
	paths = browseDedupeSort(paths)
	// An empty tree means the upstream simply has no index at the expected
	// address (or, on deb, no suite has ever been pulled): healthy-but-
	// empty, and NOT cached — an upstream that gains its first index
	// appears on the next browse instead of after a TTL.
	if len(paths) == 0 {
		e.log.InfoContext(ctx, "remote: browse enumeration found no upstream index",
			slog.String("repo", repoKey))
		return &BrowseResult{}, nil
	}
	ttl := time.Duration(cfg.MetadataTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = browseDefaultMetadataTTL
	}
	e.browseStoreLocked(repoKey, cfg, paths, ttl)
	e.log.InfoContext(ctx, "remote: browse tree refreshed",
		slog.String("repo", repoKey), slog.String("package_type", row.PackageType),
		slog.Int("paths", len(paths)))
	return &BrowseResult{Entries: browseChildren(paths, folder)}, nil
}

// ---- the in-process snapshot cache ----

// browseTree is one cached whole-tree snapshot (sorted, deduplicated file
// paths).
type browseTree struct {
	paths     []string
	expiresAt time.Time
	// sig pins the egress-relevant identity (upstream URL + username): a
	// configuration change invalidates the snapshot even inside the TTL.
	sig string
}

// browseSignature renders the cache identity of one repository config.
func browseSignature(cfg *metadata.RemoteConfig) string {
	return strings.Join([]string{cfg.URL, cfg.Username}, "\x00")
}

// browseSnapshot returns the fresh cached tree, if any.
func (e *Engine) browseSnapshot(repoKey string, cfg *metadata.RemoteConfig) (*browseTree, bool) {
	e.browseMu.Lock()
	defer e.browseMu.Unlock()
	return e.browseSnapshotLocked(repoKey, cfg)
}

// browseSnapshotLocked is browseSnapshot for a caller already holding
// browseMu (the refresh path's double-check).
func (e *Engine) browseSnapshotLocked(repoKey string, cfg *metadata.RemoteConfig) (*browseTree, bool) {
	tree := e.browseTrees[repoKey]
	if tree == nil || tree.sig != browseSignature(cfg) || !e.now().Before(tree.expiresAt) {
		return nil, false
	}
	return tree, true
}

// browseStoreLocked keeps one refreshed snapshot for the repository's
// metadata TTL (§1's mapping of the official Metadata Retrieval Cache
// Period) — browseMu already held by the refresh path.
func (e *Engine) browseStoreLocked(repoKey string, cfg *metadata.RemoteConfig, paths []string, ttl time.Duration) {
	if e.browseTrees == nil {
		e.browseTrees = map[string]*browseTree{}
	}
	e.browseTrees[repoKey] = &browseTree{
		paths:     paths,
		expiresAt: e.now().Add(ttl),
		sig:       browseSignature(cfg),
	}
}

// browseForget drops the cached snapshot (the Forget teardown hook — a
// deleted repository must not hold memory).
func (e *Engine) browseForget(repoKey string) {
	e.browseMu.Lock()
	defer e.browseMu.Unlock()
	delete(e.browseTrees, repoKey)
}

// ---- fault classification ----

// browseFault is one enumeration attempt's classified failure. unfound
// means the upstream simply has no index at the expected address (404 — a
// healthy-but-empty tree, no degradation note); offline marks the
// transport/5xx family that opens the assumed-offline window (§4-2 — the
// same circuit breaker the pull-through shares).
type browseFault struct {
	unfound bool
	offline bool
	msg     string
}

// browseFetch pulls one upstream index document through the repository's
// guarded client — a read-only GET, buffered under the client's 64 MiB
// cap, decompressed by file extension. Nothing lands: the body exists for
// parsing only.
func (e *Engine) browseFetch(ctx context.Context, client *Client, upPath string) ([]byte, *browseFault) {
	res, err := client.Fetch(ctx, Request{Path: upPath})
	if err != nil {
		if errors.Is(err, ErrBodyTooLarge) {
			return nil, &browseFault{msg: fmt.Sprintf(
				"index document '%s' exceeds the %d MiB browse cap", upPath, DefaultMaxBufferedBody>>20)}
		}
		var rej *RejectionError
		if errors.As(err, &rej) {
			// The guard's screening refusal: operator input, not an
			// upstream fault — no offline mark (the engine's own posture).
			return nil, &browseFault{msg: fmt.Sprintf("upstream '%s': %s", upPath, err.Error())}
		}
		return nil, &browseFault{
			offline: true,
			msg:     fmt.Sprintf("upstream '%s': connection failed", upPath),
		}
	}
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return browseDecompress(upPath, res.Body)
	case res.StatusCode == http.StatusNotFound, res.StatusCode == http.StatusGone:
		return nil, &browseFault{unfound: true}
	case res.StatusCode == http.StatusUnauthorized, res.StatusCode == http.StatusForbidden:
		return nil, &browseFault{msg: fmt.Sprintf(
			"upstream '%s' refused the request (%s)", upPath, res.Status)}
	default:
		return nil, &browseFault{
			offline: true,
			msg:     fmt.Sprintf("upstream '%s' answered %s", upPath, res.Status),
		}
	}
}

// browseDecompress inflates one fetched document by its file extension
// (.gz/.bz2/.xz — the three compressions apt repositories actually publish;
// everything else passes through verbatim), bounded by browseMaxInflated.
func browseDecompress(upPath string, body []byte) ([]byte, *browseFault) {
	var rdr io.Reader
	switch {
	case strings.HasSuffix(upPath, ".gz"):
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, &browseFault{msg: fmt.Sprintf("gunzip '%s': %v", upPath, err)}
		}
		defer func() { _ = zr.Close() }() //nolint:errcheck // read-only inflation
		rdr = zr
	case strings.HasSuffix(upPath, ".bz2"):
		rdr = bzip2.NewReader(bytes.NewReader(body))
	case strings.HasSuffix(upPath, ".xz"):
		xr, err := xz.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, &browseFault{msg: fmt.Sprintf("xz-decode '%s': %v", upPath, err)}
		}
		rdr = xr
	default:
		rdr = bytes.NewReader(body)
	}
	// The +1 makes an exactly-at-cap document distinguishable from an
	// over-cap one without a second read.
	out, err := io.ReadAll(io.LimitReader(rdr, browseMaxInflated+1))
	if err != nil {
		return nil, &browseFault{msg: fmt.Sprintf("decompress '%s': %v", upPath, err)}
	}
	if int64(len(out)) > browseMaxInflated {
		return nil, &browseFault{msg: fmt.Sprintf(
			"index document '%s' inflates beyond the %d MiB browse cap", upPath, browseMaxInflated>>20)}
	}
	return out, nil
}

// ---- folder normalization and child derivation ----

// normalizeBrowseFolder canonicalizes the query folder: "" (or "/") is the
// repository root; anything else is a slash-joined relative path whose
// segments must be plain names (no dot segments, no empties, no
// backslashes — the same defense validateNodePath applies to node paths).
func normalizeBrowseFolder(folder string) (string, error) {
	folder = strings.TrimSuffix(strings.TrimPrefix(folder, "/"), "/")
	if folder == "" {
		return "", nil
	}
	for _, seg := range strings.Split(folder, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.Contains(seg, "\\") {
			return "", fmt.Errorf("%w: %q", ErrBrowseInvalidFolder, folder)
		}
	}
	return folder, nil
}

// browseChildren derives one folder's direct children from the whole-tree
// file-path set: a path segment followed by deeper segments is a folder, a
// terminal segment is a file. Entries are unique and sorted by path.
func browseChildren(paths []string, folder string) []BrowseEntry {
	prefix := ""
	if folder != "" {
		prefix = folder + "/"
	}
	seen := map[string]bool{}
	out := make([]BrowseEntry, 0, 16)
	for _, p := range paths {
		if prefix != "" && !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		if rest == "" {
			continue
		}
		child, isFolder := rest, false
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			child, isFolder = rest[:i], true
		}
		full := child
		if folder != "" {
			full = folder + "/" + child
		}
		if !seen[full] {
			seen[full] = true
			out = append(out, BrowseEntry{Path: full, IsFolder: isFolder})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// browseDedupeSort sorts and drops duplicate paths (the same pool package
// legitimately appears in several architectures' Packages indexes).
func browseDedupeSort(paths []string) []string {
	sort.Strings(paths)
	out := paths[:0]
	var prev string
	for i, p := range paths {
		if i > 0 && p == prev {
			continue
		}
		prev = p
		out = append(out, p)
	}
	return out
}

// ---- helm classic (remote-browsing.md §3 row 1: index.yaml = the whole tree) ----

// browseHelm parses the upstream repo-root index.yaml: every entry's first
// url names one chart archive path (relative spellings are the repository
// tree; absolute spellings contribute their path component — a divergent
// chartsBaseUrl upstream still enumerates under its url paths).
func (e *Engine) browseHelm(ctx context.Context, repoKey string, cfg *metadata.RemoteConfig, pol repoPolicy) ([]string, *browseFault) {
	client, err := e.clientFor(repoKey, cfg, pol)
	if err != nil {
		return nil, &browseFault{msg: err.Error()}
	}
	body, fault := e.browseFetch(ctx, client, "index.yaml")
	if fault != nil {
		if fault.unfound {
			return nil, nil
		}
		return nil, fault
	}
	paths := []string{"index.yaml"}
	for _, p := range parseHelmIndexPaths(body) {
		if p != "index.yaml" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// parseHelmIndexPaths walks one index.yaml and extracts every entry's
// urls[0] path. A targeted walk (the read side needs only urls — the
// read-modify-write document engine lives in the helm adapter).
func parseHelmIndexPaths(body []byte) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 {
		return nil
	}
	root := doc.Content[0]
	if root == nil || root.Kind != yaml.MappingNode {
		return nil
	}
	var out []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "entries" {
			continue
		}
		entries := root.Content[i+1]
		if entries.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(entries.Content); j += 2 {
			versions := entries.Content[j+1]
			if versions.Kind != yaml.SequenceNode {
				continue
			}
			for _, entry := range versions.Content {
				if p := helmEntryPath(entry); p != "" {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// helmEntryPath extracts one entry node's first urls item as a
// repository-relative path ("" when absent or unusable).
func helmEntryPath(entry *yaml.Node) string {
	if entry == nil || entry.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(entry.Content); i += 2 {
		if entry.Content[i].Value != "urls" {
			continue
		}
		seq := entry.Content[i+1]
		if seq.Kind != yaml.SequenceNode || len(seq.Content) == 0 {
			return ""
		}
		return cleanUpstreamPath(seq.Content[0].Value)
	}
	return ""
}

// cleanUpstreamPath reduces one index-declared artifact reference to a
// repository-relative path: query and fragment drop, an absolute URL keeps
// only its path, leading slashes strip, and dot segments reject (an index
// entry must never walk outside the repository tree).
func cleanUpstreamPath(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if i := strings.Index(ref, "://"); i >= 0 {
		u, err := url.Parse(ref)
		if err != nil {
			return ""
		}
		ref = u.Path
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimPrefix(ref, "/")
	if ref == "" {
		return ""
	}
	for _, seg := range strings.Split(ref, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
	}
	return ref
}

// ---- debian (§3 row 2: dists/<suite> metadata enumeration) ----

// browseDeb synthesizes the tree from the suite metadata: the suites the
// local cache has ever pulled (Debian publishes no suite index — the
// honest discovery source is the cached dists/ rows), each suite's Release
// file (its checksum sections enumerate the dists/<suite>/ index files),
// and each binary Packages index (its Filename stanzas enumerate pool/).
//
// Batch-1 scope notes: source indexes (Sources.*) are enumerated as FILES
// (they ride the Release checksum sections) but their stanzas' pool source
// files are not derived; InRelease is not listed by Release checksums and
// is not probed for separately. A flat repository (Packages at the root,
// no dists/) has no suite to discover and enumerates nothing remote.
func (e *Engine) browseDeb(ctx context.Context, repoKey string, cfg *metadata.RemoteConfig, pol repoPolicy) ([]string, *browseFault) {
	suites, err := e.browseDebSuites(ctx, repoKey)
	if err != nil {
		return nil, &browseFault{msg: fmt.Sprintf("discover cached suites: %v", err)}
	}
	if len(suites) == 0 {
		return nil, nil
	}
	client, cerr := e.clientFor(repoKey, cfg, pol)
	if cerr != nil {
		return nil, &browseFault{msg: cerr.Error()}
	}
	paths := make([]string, 0, 64)
	for _, suite := range suites {
		releasePath := "dists/" + suite + "/Release"
		body, fault := e.browseFetch(ctx, client, releasePath)
		if fault != nil {
			if fault.unfound {
				// A suite whose Release vanished upstream (or whose cached
				// rows are stale) contributes nothing; the rest enumerate.
				continue
			}
			return nil, fault
		}
		files := parseDebRelease(body)
		if len(files) == 0 {
			continue
		}
		paths = append(paths, releasePath)
		for _, f := range files {
			paths = append(paths, "dists/"+suite+"/"+f)
		}
		// ONE Packages index per index directory: Release lists every
		// published variant and any single one enumerates pool/.
		for _, f := range pickDebPackagesIndexes(files) {
			idx, fault := e.browseFetch(ctx, client, "dists/"+suite+"/"+f)
			if fault != nil {
				// A missing or unreadable single index degrades nothing:
				// the tree keeps the files Release does list.
				continue
			}
			paths = append(paths, parseDebPackagesFilenames(idx)...)
		}
	}
	return paths, nil
}

// pickDebPackagesIndexes reduces the Release-listed binary Packages
// variants to one per index directory, preferring the cheapest spelling
// (uncompressed, then .gz/.bz2/.xz in that order).
func pickDebPackagesIndexes(files []string) []string {
	best := map[string]string{}
	for _, f := range files {
		if !isDebPackagesIndex(f) {
			continue
		}
		dir := f
		if i := strings.LastIndexByte(f, '/'); i >= 0 {
			dir = f[:i]
		}
		if cur, ok := best[dir]; ok && debIndexRank(cur) <= debIndexRank(f) {
			continue
		}
		best[dir] = f
	}
	out := make([]string, 0, len(best))
	for _, f := range best {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// debIndexRank orders one Packages index variant by parse cost.
func debIndexRank(rel string) int {
	switch {
	case strings.HasSuffix(rel, ".gz"):
		return 1
	case strings.HasSuffix(rel, ".bz2"):
		return 2
	case strings.HasSuffix(rel, ".xz"):
		return 3
	default:
		return 0
	}
}

// browseDebSuites discovers the suite set from the repository's cached
// node rows under dists/ — the suites some apt client has already walked
// (the pull-through lands Release/InRelease rows; folder rows and index
// files alike name their suite).
func (e *Engine) browseDebSuites(ctx context.Context, repoKey string) ([]string, error) {
	nodes, err := e.md.Nodes().ListByPrefix(ctx, repoKey, "dists")
	if err != nil {
		return nil, fmt.Errorf("list cached dists rows: %w", err)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		rest := strings.TrimPrefix(strings.TrimSuffix(n.Path, "/"), "dists/")
		if rest == "" {
			continue
		}
		suite, _, _ := strings.Cut(rest, "/")
		if suite != "" {
			seen[suite] = true
		}
	}
	suites := make([]string, 0, len(seen))
	for s := range seen {
		suites = append(suites, s)
	}
	sort.Strings(suites)
	return suites, nil
}

// isDebPackagesIndex reports whether one Release-listed file is a binary
// Packages index (the Filename stanzas to enumerate pool/ from).
func isDebPackagesIndex(rel string) bool {
	dir, base := "", rel
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		dir, base = rel[:i+1], rel[i+1:]
	}
	return strings.HasPrefix(base, "Packages") && strings.Contains(dir, "/binary-")
}

// debReleaseSumsSections are the Release sections whose continuation lines
// enumerate files (any spelling carried an empty-components header is not
// one of them).
var debReleaseSumsSections = map[string]bool{
	"MD5Sum": true, "SHA1": true, "SHA256": true, "SHA512": true,
	"Checksums-Sha1": true, "Checksums-Sha256": true, "Checksums-Sha512": true,
}

// parseDebRelease extracts the file paths a Release document's checksum
// sections list (canonical, relative to dists/<suite>/ — the by-hash
// mirrors included, faithful to the upstream tree).
func parseDebRelease(body []byte) []string {
	var out []string
	inSums := false
	for _, line := range strings.Split(string(body), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			// A header field ends any open checksum section.
			field, _, _ := strings.Cut(line, ":")
			inSums = debReleaseSumsSections[strings.TrimSpace(field)]
			continue
		}
		if !inSums {
			continue
		}
		// " <hash> <size> <path>" — one file per continuation line.
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			if p := cleanUpstreamPath(fields[len(fields)-1]); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// parseDebPackagesFilenames extracts the Filename field of every stanza
// (paragraph-separated "Key: value" lines) — the pool-side package paths.
func parseDebPackagesFilenames(body []byte) []string {
	var out []string
	for _, stanza := range strings.Split(string(body), "\n\n") {
		for _, line := range strings.Split(stanza, "\n") {
			v, ok := strings.CutPrefix(line, "Filename:")
			if !ok {
				continue
			}
			// The FIRST Filename field of a stanza wins — a continuation
			// line of another field's value must not shadow it.
			if p := cleanUpstreamPath(strings.TrimSpace(v)); p != "" {
				out = append(out, p)
			}
			break
		}
	}
	return out
}

// ---- rpm (§3 row 3: repodata/repomd.xml + primary.xml) ----

// browseRpm enumerates from repodata/repomd.xml: every <data> location is
// a repodata file row, and the primary document's <package> locations are
// the package file rows.
func (e *Engine) browseRpm(ctx context.Context, repoKey string, cfg *metadata.RemoteConfig, pol repoPolicy) ([]string, *browseFault) {
	client, err := e.clientFor(repoKey, cfg, pol)
	if err != nil {
		return nil, &browseFault{msg: err.Error()}
	}
	repomdPath := "repodata/repomd.xml"
	body, fault := e.browseFetch(ctx, client, repomdPath)
	if fault != nil {
		if fault.unfound {
			return nil, nil
		}
		return nil, fault
	}
	files, primaryHref := parseRepomd(body)
	paths := append([]string{repomdPath}, files...)
	if primaryHref == "" {
		return paths, nil
	}
	primary, fault := e.browseFetch(ctx, client, primaryHref)
	if fault != nil {
		// A missing/unreadable primary keeps the repodata file rows — the
		// tree degrades to what repomd.xml itself enumerates.
		return paths, nil
	}
	paths = append(paths, parsePrimaryLocations(primary)...)
	return paths, nil
}

// parseRepomd walks one repomd.xml and returns every <data>'s location
// href plus the primary document's href ("" when the repository carries
// none).
func parseRepomd(body []byte) (files []string, primary string) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var inData, inPrimary bool
	for {
		tok, err := dec.Token()
		if err != nil {
			return files, primary
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "data":
				inData = true
				inPrimary = attrValue(el.Attr, "type") == "primary"
			case "location":
				if !inData {
					continue
				}
				href := cleanUpstreamPath(attrValue(el.Attr, "href"))
				if href == "" {
					continue
				}
				files = append(files, href)
				if inPrimary {
					primary = href
				}
			}
		case xml.EndElement:
			if el.Name.Local == "data" {
				inData, inPrimary = false, false
			}
		}
	}
}

// parsePrimaryLocations walks one primary.xml and returns every package's
// location href (the repository-relative .rpm paths).
func parsePrimaryLocations(body []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var inPackage bool
	var out []string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "package":
				inPackage = true
			case "location":
				if !inPackage {
					continue
				}
				if href := cleanUpstreamPath(attrValue(el.Attr, "href")); href != "" {
					out = append(out, href)
				}
			}
		case xml.EndElement:
			if el.Name.Local == "package" {
				inPackage = false
			}
		}
	}
}

// attrValue reads one XML attribute ("" when absent).
func attrValue(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
