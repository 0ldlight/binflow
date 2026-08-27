package helm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The repo-root index.yaml (helm.md sections 4-5): the read-modify-write
// document every chart landing recomputes.
//
// Serialization contract (section 5.1 + S4):
//   - apiVersion: v1, entries.<chart>: [version entries], generated:
//     <ISO8601 now> — the timestamp is EVERY write's current moment;
//   - one chart's versions sort SemVer DESCENDING (newest first; invalid
//     spellings fall back to string descending);
//   - an entry = the Chart.yaml field set (raw copy — unknown keys ride
//     verbatim) plus digest (bare-hex sha256 of the tgz), created (index
//     time) and urls (ONE download URL);
//   - urls are RELATIVE by default (HL-2): urls = [<仓内路径>]. The
//     absolute mode is a reserved Options seat no config key exposes;
//   - version/appVersion always double-quoted (a 1.0 scalar would decode
//     as float otherwise); minimal quoting elsewhere; no document start
//     marker; no serverInfo;
//   - one roundtrip check per written entry: a chart whose entry cannot
//     re-parse drops out of the index entirely (logged, never fatal).

// indexPath is the storage path of the repo-root index.
const indexPath = fileIndex

// maxIndexBytes bounds one stored index.yaml read (a hostile or corrupt
// node must fail the recompute, not the process).
const maxIndexBytes = 64 << 20

// indexDoc is the parsed repo-root index.
type indexDoc struct {
	entries map[string][]*yaml.Node // chart name -> entry mapping nodes
}

// parseIndex decodes one stored index body; an unparsable document is the
// empty doc (the recompute regenerates from storage — the client-visible
// index is rebuilt, never trusted).
//
// The parse decodes the WHOLE document into one yaml.Node and walks the
// tree by hand: yaml.v3 only materializes real nodes for a TOP-LEVEL Node
// target — a nested map[string][]*yaml.Node decode leaves zero-Kind nodes
// behind, which would re-encode as `null` entries and lose the chart.
func parseIndex(body []byte) *indexDoc {
	empty := &indexDoc{entries: map[string][]*yaml.Node{}}
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return empty
	}
	root := doc.Content[0]
	if root == nil || root.Kind != yaml.MappingNode {
		return empty
	}
	out := &indexDoc{entries: map[string][]*yaml.Node{}}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		if key.Value != "entries" || val.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(val.Content); j += 2 {
			name, list := val.Content[j], val.Content[j+1]
			if list.Kind != yaml.SequenceNode {
				continue
			}
			for _, item := range list.Content {
				if item != nil && item.Kind == yaml.MappingNode {
					out.entries[name.Value] = append(out.entries[name.Value], item)
				}
			}
		}
	}
	return out
}

// entryVersion extracts one entry node's version field ("" when absent).
func entryVersion(entry *yaml.Node) string {
	if entry == nil || entry.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(entry.Content); i += 2 {
		if entry.Content[i].Value == "version" {
			return entry.Content[i+1].Value
		}
	}
	return ""
}

// entryURL extracts one entry node's first urls item ("" when absent).
func entryURL(entry *yaml.Node) string {
	if entry == nil || entry.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(entry.Content); i += 2 {
		if entry.Content[i].Value != "urls" {
			continue
		}
		seq := entry.Content[i+1]
		if seq.Kind == yaml.SequenceNode && len(seq.Content) > 0 {
			return seq.Content[0].Value
		}
	}
	return ""
}

// upsertEntry replaces the same name+version entry (the S1 remove+add
// overwrite) and keeps the per-chart order SemVer descending.
func (d *indexDoc) upsertEntry(name string, entry *yaml.Node) {
	version := entryVersion(entry)
	kept := d.entries[name][:0:0]
	for _, e := range d.entries[name] {
		if entryVersion(e) != version {
			kept = append(kept, e)
		}
	}
	kept = append(kept, entry)
	sortEntriesDesc(kept)
	d.entries[name] = kept
}

// removeEntry drops the name+version entry (the delete event's removal).
func (d *indexDoc) removeEntry(name, version string) {
	kept := d.entries[name][:0:0]
	for _, e := range d.entries[name] {
		if entryVersion(e) != version {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		delete(d.entries, name)
		return
	}
	d.entries[name] = kept
}

// removePrefix drops every entry whose urls[0] carries the path prefix
// (the partial-reindex removal rule — relative mode matches on <path>).
// The returned count feeds the reindex response.
func (d *indexDoc) removePrefix(prefix string) int {
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	removed := 0
	for name, list := range d.entries {
		kept := list[:0:0]
		for _, e := range list {
			if strings.HasPrefix(entryURL(e), prefix) {
				removed++
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(d.entries, name)
		} else {
			d.entries[name] = kept
		}
	}
	return removed
}

// sortEntriesDesc orders one chart's entries newest-first (SemVer
// descending; string descending tiebreak/fallback — section 5.1).
func sortEntriesDesc(entries []*yaml.Node) {
	sort.SliceStable(entries, func(i, j int) bool {
		vi, vj := entryVersion(entries[i]), entryVersion(entries[j])
		if c := compareSemver(vi, vj); c != 0 {
			return c > 0
		}
		return vi > vj
	})
}

// render serializes the document: apiVersion, entries (chart names
// sorted), generated. Chart.yaml-sourced values keep their node styles;
// server-added scalars pin their own.
func (d *indexDoc) render(now time.Time) []byte {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, scalarNode("apiVersion"), scalarNode("v1"))
	entries := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	names := make([]string, 0, len(d.entries))
	for n, list := range d.entries {
		if len(list) == 0 {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		chartList := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		chartList.Content = append(chartList.Content, d.entries[n]...)
		entries.Content = append(entries.Content, scalarNode(n), chartList)
	}
	root.Content = append(root.Content,
		scalarNode("entries"), entries,
		scalarNode("generated"), quotedScalar(now.UTC().Format(time.RFC3339)))
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		// unreachable: a hand-built node tree always encodes
		return []byte("apiVersion: v1\nentries: {}\ngenerated: \"\"\n")
	}
	_ = enc.Close()
	return buf.Bytes()
}

// scalarNode builds one plain string scalar; the style forces
// double-quoting where the contract demands it (created, version).
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// quotedScalar builds one double-quoted string scalar.
func quotedScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value, Style: yaml.DoubleQuotedStyle}
}

// buildEntry assembles one version entry: the Chart.yaml raw field copy
// (unknown keys verbatim) overridden by the server-added trio. ok is
// false when the single-entry roundtrip check fails (section 5.1 — the
// chart drops out of the index, logged by the caller).
func buildEntry(arc *chartArchive, relPath, digest string, now time.Time, absoluteBase string) (*yaml.Node, bool) {
	fields := map[string]*yaml.Node{}
	if arc.raw != nil && arc.raw.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(arc.raw.Content); i += 2 {
			key := arc.raw.Content[i].Value
			val := *arc.raw.Content[i+1]
			fields[key] = &val
		}
	}
	// The server-owned trio and the identity pair always win over whatever
	// the Chart.yaml spelled.
	url := relPath
	if absoluteBase != "" {
		url = strings.TrimRight(absoluteBase, "/") + "/" + relPath
	}
	urls := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	urls.Content = append(urls.Content, scalarNode(url))
	fields["name"] = scalarNode(arc.meta.Name.String())
	fields["version"] = quotedScalar(arc.meta.Version.String())
	if app, ok := fields["appVersion"]; ok {
		app.Style = yaml.DoubleQuotedStyle
	} else if arc.meta.AppVersion.String() != "" {
		fields["appVersion"] = quotedScalar(arc.meta.AppVersion.String())
	}
	fields["digest"] = scalarNode(digest)
	fields["created"] = quotedScalar(now.UTC().Format(time.RFC3339))
	fields["urls"] = urls

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range keys {
		entry.Content = append(entry.Content, scalarNode(k), fields[k])
	}
	if err := roundtripCheck(entry); err != nil {
		return nil, false
	}
	return entry, true
}

// roundtripCheck re-parses one rendered entry (section 5.1's per-entry
// guard: a chart that cannot round-trip never reaches the stored index).
func roundtripCheck(entry *yaml.Node) error {
	var doc yaml.Node
	doc.Kind = yaml.DocumentNode
	doc.Content = []*yaml.Node{entry}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	var into map[string]any
	return yaml.Unmarshal(out.Bytes(), &into)
}

// ---- the read-modify-write engine ----

// indexMutexes is the per-repoKey mutex set serializing the index
// read-modify-write (the cargo rewriteMutexes posture verbatim — the
// whole-file rewrite's lost-update guard). Refcounted so the map does not
// grow with the repository population.
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

// readStoredIndex loads the repo-root index body; a missing node is the
// empty doc (the skeleton case), every other failure is an error.
func (h *Handler) readStoredIndex(ctx context.Context, p *repo.Principal, repoKey string) (*indexDoc, error) {
	rc, _, err := h.svc.Get(ctx, p, repoKey, indexPath)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return &indexDoc{entries: map[string][]*yaml.Node{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", indexPath, err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, err := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s body: %w", indexPath, err)
	}
	return parseIndex(body), nil
}

// writeStoredIndex lands the rendered body at the repo root. The write is
// the freely-regenerable family: no overwrite-permission demand.
func (h *Handler) writeStoredIndex(ctx context.Context, p *repo.Principal, repoKey string, body []byte) error {
	if _, err := h.svc.PutWithOptions(ctx, p, repoKey, indexPath,
		bytes.NewReader(body), blobRefOf(body), "text/yaml",
		repo.PutOptions{SkipOverwriteCheck: true}); err != nil {
		return fmt.Errorf("write %s: %w", indexPath, err)
	}
	return nil
}

// indexChartLocked is the PUT chain's index step under the per-repo lock:
// read (or skeleton) → replace the same name+version entry → write. A
// roundtrip-failing chart logs and drops out (never fails the upload —
// the blob is already landed).
func (h *Handler) indexChartLocked(ctx context.Context, p *repo.Principal, repoKey, relPath, digest string, arc *chartArchive) error {
	doc, err := h.readStoredIndex(ctx, p, repoKey)
	if err != nil {
		return err
	}
	now := h.now()
	absoluteBase := ""
	if h.opts.AbsoluteURLs {
		absoluteBase = h.opts.BaseURL + "/binflow/" + repoKey
	}
	entry, ok := buildEntry(arc, relPath, digest, now, absoluteBase)
	if !ok {
		slog.WarnContext(ctx, "helm: chart entry failed the index roundtrip check; skipped",
			slog.String("repo", repoKey), slog.String("path", relPath))
		return nil
	}
	doc.upsertEntry(arc.meta.Name.String(), entry)
	return h.writeStoredIndex(ctx, p, repoKey, doc.render(now))
}

// unindexChartLocked is the DELETE chain's index step under the lock: drop
// the name+version entry the node's chart.* properties identified.
func (h *Handler) unindexChartLocked(ctx context.Context, p *repo.Principal, repoKey, name, version string) error {
	doc, err := h.readStoredIndex(ctx, p, repoKey)
	if err != nil {
		return err
	}
	doc.removeEntry(name, version)
	return h.writeStoredIndex(ctx, p, repoKey, doc.render(h.now()))
}

// withIndexLock runs fn under the repository's index lock.
func (h *Handler) withIndexLock(repoKey string, fn func() error) error {
	var err error
	h.rewrites.withLock(repoKey, func() { err = fn() })
	return err
}
