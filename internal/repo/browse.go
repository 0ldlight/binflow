package repo

// The remote-browsing optional档's service wiring (M16 T-448, FR-147.2;
// docs/reverse/remote-browsing.md — the seam T-442's engine left for this
// ticket). The flag lives in the canonical remote config
// (listRemoteFolderItems, default false); the ENGINE never reads it — the
// off posture is "nobody calls BrowseRemote", so behavior diff zero for
// every repository without the flag is true by construction.
//
// What the flag turns on (spec anchors):
//
//   - Listing merge (§1 "on 行为"): a directory listing of the remote
//     repository (and, through §8.5's expanded口径, of a virtual repository
//     with such a member) carries the upstream's DERIVED display rows
//     beside the cache rows — display-only, zero 落库 (§4-3): no node row,
//     no blob, no cache-state row, no negative cache. A path the cache
//     already holds keeps the CACHE row (its real digest/size/mtime — the
//     cached-first dedupe rule); a derived file row is recognizable by its
//     empty sha256 (every landed file node carries its blob digest).
//   - Folder resolution: a folder spelling that exists only upstream
//     answers ErrIsFolder off the enumeration snapshot, so the tree can be
//     EXPANDED into uncached remote directories (T-461's consumption).
//     Display-only, zero writes — the ADR-0013 posture a virtual read
//     already upholds.
//   - Click-through: a derived FILE row is an ordinary repository-relative
//     path — the download plane's pull-through chain serves it, and the
//     T-438 single-source count lands with the audit row (arm 3,
//     remote-serving). No second counting site exists to wire.
//   - Degradation (§4-1/§4-2): an upstream fault NEVER fails the listing —
//     the cached rows stay and the fault rides the listing's remote-layer
//     note (RemoteBrowseListing.RemoteDegraded; Service.List drops the note
//     and keeps the rows). The engine's assumed-offline window bounds
//     upstream contact, so a dead upstream is probed at most once per
//     silence period.
//   - Authorization: the enumeration's permit is the service's OWN allow()
//     handed in as a closure — literally the same function the content
//     plane consults. On the direct face the listing gate has already run;
//     on the VIRTUAL face (§5) a remote member's derived rows additionally
//     require read on the MEMBER (越权仓远端行零泄漏), while the member's
//     cache rows keep the T-412 posture (the virtual's own gate covers
//     them — browsing a member's whole upstream tree is a different
//     exposure class than resolving a path the caller already knows).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// remoteBrowseRepo is the repository facts the browse wiring needs: the
// member key, its package type and its config JSON. A slim copy of the
// repository row so the virtual member walk can hand its already-loaded
// facts over without a second store read.
type remoteBrowseRepo struct {
	key         string
	packageType string
	config      string
}

// browseRepoOf projects one repository row onto the browse facts.
func browseRepoOf(row *metadata.Repo) remoteBrowseRepo {
	return remoteBrowseRepo{key: row.RepoKey, packageType: row.PackageType, config: row.Config}
}

// listRemoteFolderItems reads the optional档 mark out of a remote
// repository's config JSON — the memberPriorityResolution posture: the
// canonical form always carries the boolean, a probe failure means a
// hand-mangled row, and the safe answer is the product default (off, the
// whole pre-T-448 behavior).
func listRemoteFolderItems(config string) bool {
	var probe struct {
		ListRemoteFolderItems bool `json:"listRemoteFolderItems"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return false
	}
	return probe.ListRemoteFolderItems
}

// browsePermit is the ACL seam handed to the engine: the caller's own
// allow() as a closure, consulted with the engine's normalized folder
// spelling (no trailing slash) BEFORE any upstream contact.
func (s *service) browsePermit(ctx context.Context, p *Principal, repoKey string) remote.BrowsePermit {
	return func(folder string) bool {
		return s.allow(ctx, p, repoKey, folder, ActionRead)
	}
}

// remoteBrowseRows enumerates one remote repository's upstream-derived
// display rows for a folder listing. The answer is nil (and the note empty)
// whenever the optional档 cannot engage: flag off, package type outside the
// engine's batch-1 set, no engine wired, or the permit refused the folder
// (the zero-leak posture — a refusing member contributes zero derived rows,
// which is the gate's answer, not a degradation). An upstream fault answers
// zero rows plus the layer's error-state note; the CALLER keeps serving the
// cached rows either way (§4-1: 枚举失败不整树塌).
func (s *service) remoteBrowseRows(ctx context.Context, p *Principal, m remoteBrowseRepo, prefix string) ([]*metadata.Node, string) {
	if !listRemoteFolderItems(m.config) || !remote.BrowseSupported(m.packageType) || s.remoteEng == nil {
		return nil, ""
	}
	res, err := s.remoteEng.BrowseRemote(ctx, s.browsePermit(ctx, p, m.key), m.key, prefix)
	if err != nil {
		if errors.Is(err, remote.ErrBrowseDenied) {
			return nil, "" // the member gate refused: zero rows, zero note
		}
		// Anything else (an unexpected store fault inside the engine, an
		// invalid folder the listing plane should have normalized away):
		// degrade the remote layer, never the listing.
		slog.WarnContext(ctx, "repo: remote folder enumeration degraded",
			slog.String("repo", m.key), slog.String("folder", prefix), slog.String("reason", err.Error()))
		return nil, "remote enumeration unavailable: " + err.Error()
	}
	if res.Degraded != "" {
		return nil, res.Degraded
	}
	out := make([]*metadata.Node, 0, len(res.Entries)+1)
	for _, e := range res.Entries {
		out = append(out, browseDisplayNode(m.key, e))
	}
	// The folder's own display row rides along — the cached arm's listing
	// shape (the folder marker plus everything beneath it). The parent
	// enumeration reads the snapshot the entries call just warmed, so this
	// costs no second upstream contact; a folder the parent does not mark
	// (a file spelling, an absent path) contributes nothing.
	if prefix != "" {
		if n := s.remoteBrowseFolderRow(ctx, p, m, prefix); n != nil {
			out = append(out, n)
		}
	}
	return out, ""
}

// browseDisplayNode synthesizes one display-only row (§4-3): a folder
// carries the shared folder-marker sentinel and the trailing-slash storage
// spelling; a file carries no digest and no size — both are unknown until
// the pull-through lands the artifact, which is exactly what distinguishes
// a derived row from a cached one (every landed file node carries its blob
// digest). Nothing here is or becomes a stored row.
func browseDisplayNode(repoKey string, e remote.BrowseEntry) *metadata.Node {
	if e.IsFolder {
		return &metadata.Node{RepoKey: repoKey, Path: e.Path + "/", Sha256: emptyFolderSHA}
	}
	return &metadata.Node{RepoKey: repoKey, Path: e.Path}
}

// remoteBrowseFolderRow answers the folder face for a folder that exists
// only upstream: the PARENT's enumeration is the proof (its entry marks the
// folder even when the folder is empty upstream), so an empty-but-listed
// directory resolves like any other. nil — and no folder claim — whenever
// the optional档 is off, the folder is absent, the layer is degraded (the
// caller then falls through to the engine, whose own error state speaks) or
// the permit refuses (zero-leak: fail closed).
func (s *service) remoteBrowseFolderRow(ctx context.Context, p *Principal, m remoteBrowseRepo, folder string) *metadata.Node {
	if !listRemoteFolderItems(m.config) || !remote.BrowseSupported(m.packageType) || s.remoteEng == nil {
		return nil
	}
	parent := ""
	if i := strings.LastIndexByte(folder, '/'); i >= 0 {
		parent = folder[:i]
	}
	res, err := s.remoteEng.BrowseRemote(ctx, s.browsePermit(ctx, p, m.key), m.key, parent)
	if err != nil || res.Degraded != "" {
		return nil
	}
	for _, e := range res.Entries {
		if e.Path == folder && e.IsFolder {
			return browseDisplayNode(m.key, e)
		}
	}
	return nil
}

// foldBrowseRows merges derived display rows into a cached listing (§4-3's
// cached-first dedupe, key = the path without the folder slash so a cached
// marker and a derived folder spelling are the same row) and restores the
// interface's path order.
func foldBrowseRows(cached, derived []*metadata.Node) []*metadata.Node {
	if len(derived) == 0 {
		return cached
	}
	seen := make(map[string]bool, len(cached))
	for _, n := range cached {
		seen[strings.TrimSuffix(n.Path, "/")] = true
	}
	out := make([]*metadata.Node, 0, len(cached)+len(derived))
	out = append(out, cached...)
	for _, n := range derived {
		if seen[strings.TrimSuffix(n.Path, "/")] {
			continue // the cache row owns this path
		}
		seen[strings.TrimSuffix(n.Path, "/")] = true
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// listRows is the listing pipeline every face shares (one resolution
// channel — Service.List and the RemoteBrowsePlane's enriched twin are the
// same walk, so the wire faces and the note-carrying consumer can never
// disagree about a tree): validate, gate, dispatch by class, and fold the
// optional档's remote-derived rows in. The second return is the remote
// layer's degradation note ("" when every engaged layer is healthy).
func (s *service) listRows(ctx context.Context, p *Principal, row *metadata.Repo, prefix string) ([]*metadata.Node, string, error) {
	if row.Type == TypeVirtual {
		return s.listVirtual(ctx, p, row.RepoKey, prefix)
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, row.RepoKey, prefix)
	if err != nil {
		return nil, "", fmt.Errorf("list %s/%s: %w", row.RepoKey, prefix, err)
	}
	derived, degraded := s.remoteBrowseRows(ctx, p, browseRepoOf(row), prefix)
	return foldBrowseRows(nodes, derived), degraded, nil
}
