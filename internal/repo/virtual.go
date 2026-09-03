package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/remote"
)

// The virtual repository resolution subdomain (T-71, FR-21, ADR-0013 as
// amended by the T-79 errata): two-bucket member ordering, first-hit-stops
// download resolution, and the optional write route onto a local deployment
// member. Everything here computes PER REQUEST off the member ledger — there
// is deliberately no resolution cache, so a member-list or priority-mark
// change is visible to the very next request (FR-15-AC6's second half).
//
// Two-bucket order (PRD C3, the BinFlow simplification of repo-semantics
// section 8.1's four buckets): Artifactory interleaves each remote's
// `<key>-cache` shadow repository with the remote itself; BinFlow has no
// shadow projection (ADR-0012 — cache nodes land in the remote repository's
// own namespace), so the four buckets collapse into two with identical
// client-observable behavior:
//
//	bucket 1: members marked priorityResolution=true, declaration order;
//	bucket 2: every other member, declaration order.
//
// priorityResolution is a PER-REPOSITORY field (Artifactory semantics,
// repo-semantics sections 7.1/8.1, default false): local members carry the
// mark in their caller-owned config JSON, remote members in the canonical
// remote form — one probe reads both.

// HdrResolvedFrom names the member that served a virtual resolution
// (ADR-0013): the diagnostic surface for "I got the older copy" complaints —
// first-hit-stops means member order, not content freshness, decides.
const HdrResolvedFrom = "X-BinFlow-Resolved-From"

// msgNoDeploymentRepo is the EXACT 405 body of the un-routed write refusal
// (repo-semantics section 8.2, high confidence; PRD C5 pins the spelling —
// M52 compares it).
func msgNoDeploymentRepo(virtualKey string) string {
	return fmt.Sprintf(
		"No local repository was configured as local deployment repository for the (%s) virtual repository.", virtualKey)
}

// refuseVirtualWrite is the StatusError of RE-08's un-routed write plane.
func refuseVirtualWrite(virtualKey string) error {
	return &StatusError{
		Code:    http.StatusMethodNotAllowed,
		Message: msgNoDeploymentRepo(virtualKey),
		Header:  http.Header{"Allow": []string{http.MethodGet}},
		cause:   fmt.Errorf("%w: virtual repository %q has no defaultDeploymentRepo", ErrRepoTypeNotSupported, virtualKey),
	}
}

// refuseVirtualDelete is DELETE-on-virtual's refusal. BinFlow deliberately
// does NOT replicate Artifactory's member-wise virtual delete (spec
// section 8.2 lists it medium confidence, M4 re-evaluation): cache deletes
// belong to the remote member itself (RE-06), artifact deletes to the member
// repository that holds them. The un-routed shape keeps the C5 message so
// M52's equality assertion holds for DELETE too; a routed repository gets
// the truthful wording instead (claiming "no local repository was
// configured" about one that IS would be a lie).
func refuseVirtualDelete(virtualKey string, routed bool) error {
	message := msgNoDeploymentRepo(virtualKey)
	if routed {
		message = fmt.Sprintf(
			"Deletes are not propagated through the virtual repository '%s'; delete the artifact in its member repository directly.", virtualKey)
	}
	return &StatusError{
		Code:    http.StatusMethodNotAllowed,
		Message: message,
		Header:  http.Header{"Allow": []string{http.MethodGet}},
		cause:   fmt.Errorf("%w: virtual repository deletes are not propagated", ErrRepoTypeNotSupported),
	}
}

// ---- read resolution ----

// virtualMember is one resolution step: the member key plus its class. The
// priority mark is consumed by the ordering pass and not carried. pkg/cfg
// are the member row's package type and config JSON — the T-448 browse fold
// reads the optional档 mark from them without a second repository load; the
// resolution walk itself ignores them.
type virtualMember struct {
	key string
	typ string // TypeLocal | TypeRemote
	pkg string
	cfg string
}

// virtualMemberOrder computes the two-bucket resolution order of one virtual
// repository, fresh off the ledger on every call (member changes are
// immediately effective — the FR-15-AC6 contract this resolver serves).
//
// Drift tolerance: the ledger and the repositories rows are kept consistent
// by the config-time validations and the FK cascades, but a member that has
// vanished or drifted virtual between write and read (delete plus recreate
// races) is SKIPPED with a WARN rather than failing the whole repository —
// one stale member must not turn every virtual read into an outage. Any
// other lookup failure is a store fault and propagates.
func (s *service) virtualMemberOrder(ctx context.Context, virtualKey string) ([]virtualMember, error) {
	rows, err := s.md.Virtual().ListMembers(ctx, virtualKey)
	if err != nil {
		return nil, fmt.Errorf("virtual %s members: %w", virtualKey, err)
	}
	priority, rest := make([]virtualMember, 0, len(rows)), make([]virtualMember, 0, len(rows))
	for _, row := range rows {
		member, err := s.md.Repos().Get(ctx, row.MemberRepo)
		if err != nil {
			if errors.Is(err, metadata.ErrRepoNotFound) {
				slog.WarnContext(ctx, "repo: virtual member listed but missing — skipped",
					"virtual", virtualKey, "member", row.MemberRepo)
				continue
			}
			return nil, fmt.Errorf("virtual %s member %s: %w", virtualKey, row.MemberRepo, err)
		}
		switch member.Type {
		case TypeLocal, TypeRemote:
			m := virtualMember{key: member.RepoKey, typ: member.Type, pkg: member.PackageType, cfg: member.Config}
			if memberPriorityResolution(member.Config) {
				priority = append(priority, m)
			} else {
				rest = append(rest, m)
			}
		default:
			// Nested virtuals are refused at config time; reaching one here
			// means the member was recreated as virtual after validation.
			slog.WarnContext(ctx, "repo: virtual member drifted virtual — skipped (nested virtual repositories are not supported)",
				"virtual", virtualKey, "member", row.MemberRepo)
		}
	}
	order := make([]virtualMember, 0, len(priority)+len(rest))
	order = append(order, priority...)
	order = append(order, rest...)
	return order, nil
}

// memberPriorityResolution reads the per-repository priorityResolution mark
// out of a member's config JSON. Both spellings the canonical forms produce
// (the local passthrough blob and the remote canonical struct) carry the
// same field name; validateLocalConfig/parseRemoteConfig guarantee it is a
// boolean when present, so a probe failure means a hand-mangled row and the
// safe answer is the Artifactory default: false.
func memberPriorityResolution(config string) bool {
	var probe struct {
		PriorityResolution bool `json:"priorityResolution"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return false
	}
	return probe.PriorityResolution
}

// getVirtual is the Get branch of a virtual repository: walk the two-bucket
// member order, first hit wins. The read gate has already run on the VIRTUAL
// key at Get's top (authorization addresses the repository the caller named,
// members are resolution internals — the same posture as the remote branch:
// an unauthorized principal must not aim BinFlow at member upstreams).
//
// The FOLDER spelling (trailing slash) never enters the walk: it is the
// aggregate browse's question, answered from the members' stored rows by
// getVirtualFolder (T-412, FR-136.1) — a folder has no body to resolve, so
// first-hit-stops has nothing to decide.
//
// Member semantics per class on the FILE face:
//
//   - local: a plain node lookup. A folder row is NOT a download hit — it
//     carries no body — so folder-only members keep the walk moving toward a
//     member that actually holds the artifact.
//   - remote: the full FR-20 proxy chain (cache, stale downgrade, guarded
//     upstream contact — M50's upstream-package case depends on it). The
//     engine's classified outcome drives the R10 stale/miss rule: a RESULT
//     (success, which always carries a copy — fresh, landed or expired) is
//     this member's answer and stops the walk, even when the copy is stale;
//     only an UNFOUND miss (negative cache, no copy, offline without a copy)
//     continues to the next member. A classified NON-unfound failure (the
//     SSRF chain's 400, hardFail's 502) propagates verbatim: the strict
//     reading of "only a true 404 continues" — masking a security refusal or
//     a demanded hard failure as a virtual-wide 404 would hide a configured
//     behavior, and RE-08/QA surface both through the member's own wording.
//
// An exploratory miss leaves NO cache residue behind (ADR-0013: virtual
// member scans must not pollute the members' caches) — see
// clearProbedNegative.
func (s *service) getVirtual(ctx context.Context, p *Principal, virtualKey, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, nil, err
	}
	if isFolderNode(path) {
		// T-412 (FR-136.1): the folder spelling is the aggregate browse.
		// Same member order as the pull walk below — one resolution order,
		// two faces (the browse can never disagree with the download plane
		// about which member leads).
		return s.getVirtualFolder(ctx, p, virtualKey, path, order)
	}
	for _, m := range order {
		var (
			rc   io.ReadSeekCloser
			node *metadata.Node
			hit  bool
		)
		if m.typ == TypeRemote {
			rc, node, hit, err = s.probeRemoteMember(ctx, virtualKey, m.key, path)
		} else {
			rc, node, hit, err = s.probeLocalMember(ctx, m.key, path)
		}
		if err != nil {
			return nil, nil, err
		}
		if !hit {
			continue // member miss: next bucket entry
		}
		// The via-virtual arm (K69 arm 2): the audit row addresses the
		// VIRTUAL surface, the count lands on the MEMBER's row — and a
		// member that is itself a remote repository served this download
		// from its cache, so its row takes the remote column too.
		s.markDownload(ctx, p, downloadMark{
			auditRepo: virtualKey, auditPath: path,
			countRepo: m.key, countPath: path,
			origin:       downloadOriginVirtual(virtualKey),
			extra:        []string{fmt.Sprintf(`"resolvedFrom":%q`, m.key)},
			remoteServed: m.typ == TypeRemote,
		})
		return withResolvedFrom(rc, m.key), node, nil
	}
	return nil, nil, fmt.Errorf("node %s/%s: %w", virtualKey, path, ErrNodeNotFound)
}

// probeLocalMember looks the path up in a local member's namespace. The
// caller has already been authorized on the virtual repository; the member's
// own grants are not consulted (the virtual is the addressed surface).
func (s *service) probeLocalMember(ctx context.Context, member, path string) (io.ReadSeekCloser, *metadata.Node, bool, error) {
	n, err := s.md.Nodes().Get(ctx, member, path)
	if err != nil {
		if errors.Is(err, metadata.ErrNodeNotFound) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("node %s/%s: %w", member, path, err)
	}
	if n.Sha256 == emptyFolderSHA {
		// Folder rows carry no body; the folder SPELLING is answered by
		// getVirtualFolder before this walk ever runs, but a slash-less
		// probe that happens to hit a folder row keeps walking toward a
		// member that actually holds the artifact.
		return nil, nil, false, nil
	}
	rc, _, err := s.st.Open(ctx, n.Sha256)
	if err != nil {
		// Blob row present, file gone: the same repair-case posture as the
		// local Get — surfaced, never silently skipped.
		return nil, nil, false, fmt.Errorf("open blob %s for %s/%s: %w", n.Sha256, member, path, err)
	}
	seekable, ok := rc.(io.ReadSeekCloser)
	if !ok {
		_ = rc.Close()
		return nil, nil, false, fmt.Errorf("open blob %s for %s/%s: storage backend does not support Seek", n.Sha256, member, path)
	}
	return seekable, n, true, nil
}

// ---- the T-412 aggregate browse (FR-136.1/136.2) ----

// getVirtualFolder is the virtual Get's folder face: a trailing-slash probe
// answers through the members' STORED rows only — the folder marker of the
// first member in the shared order that holds one, or a synthesized marker
// when members hold only children beneath the path (pre-ADR-0016 remote
// landings wrote file rows without their ancestor folders). The upstream is
// NEVER probed (a directory is not an upstream resource — the T-406 ruling)
// and nothing is written into any member's namespace: the remote arm's
// read-side materialization is deliberately NOT copied here (a virtual read
// leaves every member exactly as it found it, ADR-0013), so the synthesized
// row is display-only and carries no provenance it cannot honestly claim.
func (s *service) getVirtualFolder(ctx context.Context, p *Principal, virtualKey, path string, order []virtualMember) (io.ReadSeekCloser, *metadata.Node, error) {
	dir := strings.TrimSuffix(path, "/")
	for _, m := range order {
		n, err := s.md.Nodes().Get(ctx, m.key, path)
		switch {
		case err == nil:
			// A non-marker row at a folder spelling is unwritable by any
			// current writer; should drift ever produce one, the spelling is
			// still the caller's question and answers the same way.
			return nil, n, fmt.Errorf("get %s/%s: %w", virtualKey, path, ErrIsFolder)
		case errors.Is(err, metadata.ErrNodeNotFound):
			// The marker may live in a later member — keep walking.
		default:
			return nil, nil, fmt.Errorf("node %s/%s (member %s): %w", virtualKey, path, m.key, err)
		}
	}
	// No member holds the marker. Children strictly beneath the path still
	// prove the directory (the caller's next call lists them): synthesize
	// the display row rather than 404 a folder that demonstrably lists.
	// ListByPrefix's LIKE also matches the dir's own spelling and sibling
	// prefixes sharing the leading bytes, so the child test is the exact
	// dir+"/" prefix, the same in-process filter Delete's candidate walk
	// applies.
	for _, m := range order {
		kids, err := s.md.Nodes().ListByPrefix(ctx, m.key, dir)
		if err != nil {
			return nil, nil, fmt.Errorf("list %s/%s (member %s): %w", virtualKey, dir, m.key, err)
		}
		for _, k := range kids {
			if strings.HasPrefix(k.Path, dir+"/") {
				return nil, &metadata.Node{
					RepoKey: virtualKey, Path: path, Sha256: emptyFolderSHA, Size: 0,
				}, fmt.Errorf("get %s/%s: %w", virtualKey, path, ErrIsFolder)
			}
		}
	}
	// T-448 (FR-147.2, repo-semantics §8.5): a folder that exists only in a
	// flag-on remote member's upstream tree resolves off the enumeration
	// snapshot — the same display-only synthesized marker this method
	// already answers for members with only cached children, in the same
	// member order, gated on the MEMBER's own allow() (zero-leak) and
	// writing nothing into any member's namespace (ADR-0013).
	for _, m := range order {
		if m.typ != TypeRemote {
			continue
		}
		if s.remoteBrowseFolderRow(ctx, p, remoteBrowseRepo{key: m.key, packageType: m.pkg, config: m.cfg}, dir) != nil {
			return nil, &metadata.Node{
				RepoKey: virtualKey, Path: path, Sha256: emptyFolderSHA, Size: 0,
			}, fmt.Errorf("get %s/%s: %w", virtualKey, path, ErrIsFolder)
		}
	}
	return nil, nil, fmt.Errorf("node %s/%s: %w", virtualKey, path, ErrNodeNotFound)
}

// listVirtual is List's virtual arm: the members' children UNION, walked in
// the SAME two-bucket order the pull resolution uses — a path two members
// carry answers the FIRST member's row, the exact row a pull of that path
// would serve, so the aggregate browse and the download plane can never
// disagree about which member owns a path (FR-136.2's "不引入第二解析通道":
// one virtualMemberOrder computation, two consumers). Rows keep their MEMBER
// repo key — the storage-plane truth, the same posture as the pull Get's
// winning node and aql.md §7-2's "result rows carry the actual storage repo".
//
// Remote members contribute their CACHE rows only (the T-406 listing
// posture): the upstream is never probed from a browse face, so a dead or
// slow upstream cannot fail or stall the tree. A memberless or all-empty
// virtual answers an honest empty page, never an error — the
// no-members/all-members-empty distinction the console's empty-state copy
// keys on rides the repositories face's member list, not this one.
//
// T-448 (FR-147.2, repo-semantics §8.5's expanded口径 — the reconciliation
// the spec's own wording always described): a remote member with
// listRemoteFolderItems=true ALSO contributes its upstream-derived display
// rows, folded AT the member's own position in the order (cache rows first,
// derived rows of the same member after — a cached row anywhere in the
// member's unit keeps its real digest), first member still winning any path.
// The derived rows are display-only (§4-3) and additionally gated on the
// MEMBER's own allow() — an unauthorized member leaks zero upstream rows
// (remote-browsing.md §5) while its cache rows keep the T-412 posture. The
// flag stays off by default, so the pre-T-448 "cache rows only" tree is the
// unchanged default. The second return aggregates the engaged members'
// degradation notes: a degraded layer never fails the tree (§4-1).
func (s *service) listVirtual(ctx context.Context, p *Principal, virtualKey, prefix string) ([]*metadata.Node, string, error) {
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, "", err
	}
	merged := make(map[string]*metadata.Node)
	cachedPaths := make(map[string]bool)
	var degraded []string
	for _, m := range order {
		nodes, err := s.md.Nodes().ListByPrefix(ctx, m.key, prefix)
		if err != nil {
			return nil, "", fmt.Errorf("list %s/%s (member %s): %w", virtualKey, prefix, m.key, err)
		}
		// The optional档's member unit: cache rows first, then the member's
		// derived rows (zero when the flag is off or the member gate
		// refuses) — one fold, both classes, at the member's position.
		var derived []*metadata.Node
		if m.typ == TypeRemote {
			var note string
			derived, note = s.remoteBrowseRows(ctx, p, remoteBrowseRepo{key: m.key, packageType: m.pkg, config: m.cfg}, prefix)
			if note != "" {
				degraded = append(degraded, note)
			}
		}
		for _, n := range nodes {
			cachedPaths[strings.TrimSuffix(n.Path, "/")] = true
			if _, dup := merged[n.Path]; dup {
				continue // an earlier member in the order owns this path
			}
			merged[n.Path] = n
		}
		for _, n := range derived {
			// §4-3's cached-first rule: a cache row ANYWHERE in the union
			// owns the path (its real digest beats a display row); the
			// slash-insensitive key folds the folder marker spelling into
			// the same path.
			if cachedPaths[strings.TrimSuffix(n.Path, "/")] {
				continue
			}
			if _, dup := merged[n.Path]; dup {
				continue // an earlier member in the order owns this path
			}
			merged[n.Path] = n
		}
	}
	out := make([]*metadata.Node, 0, len(merged))
	for _, n := range merged {
		out = append(out, n)
	}
	// ListByPrefix answers each member ordered by path; the union re-sorts
	// so the merged answer keeps the interface's path-ordered contract.
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, strings.Join(degraded, "; "), nil
}

// probeRemoteMember walks one remote member through the FR-20 chain and maps
// its outcome onto the R10 rule (result → hit; unfound → miss; anything else
// → propagate).
func (s *service) probeRemoteMember(ctx context.Context, virtualKey, member, path string) (io.ReadSeekCloser, *metadata.Node, bool, error) {
	if s.remoteEng == nil {
		return nil, nil, false, fmt.Errorf("%w: remote repositories have no engine wired", ErrRepoTypeNotSupported)
	}
	// The exploratory-miss guard (ADR-0013): remember whether a FRESH
	// negative entry already stands, so the cleanup below only ever drops
	// what THIS probe wrote — a direct GET's legitimate negative cache must
	// survive a virtual scan untouched.
	hadFreshNegative := s.hasFreshNegative(ctx, member, path)
	res, err := s.remoteEng.Fetch(ctx, member, path)
	if err != nil {
		var fe *remote.FetchError
		if !errors.As(err, &fe) {
			return nil, nil, false, fmt.Errorf("virtual %s remote member %s fetch %s: %w", virtualKey, member, path, err)
		}
		if fe.Unfound {
			// True miss (negative cache, no copy, offline without a copy):
			// undo the negative row the chain just wrote and walk on. A miss
			// probe must leave the member's cache exactly as it found it.
			if !hadFreshNegative {
				s.clearProbedNegative(ctx, virtualKey, member, path)
			}
			return nil, nil, false, nil
		}
		// Classified, non-unfound member failure: the engine already fixed
		// the exact client rendering; pass it through untouched.
		return nil, nil, false, &StatusError{Code: fe.Status, Message: fe.Message}
	}
	if !res.HasCopy {
		// Defensive consumption of the T-66 contract (R10): a successful
		// result always carries a copy. If a future engine revision ever
		// returns a copyless success, treating it as a miss is the only
		// order-preserving answer.
		slog.WarnContext(ctx, "repo: virtual remote member returned a copyless result — treated as a miss",
			"virtual", virtualKey, "member", member, "path", path)
		return nil, nil, false, nil
	}
	return res.Body, res.Node, true, nil
}

// negativeEntryKind mirrors internal/remote's unexported miss-row kind (the
// kind column is free text to the store, T-62; the fetcher owns the value).
// The two spellings must stay in sync — this const is the repo-side pin.
const negativeEntryKind = "negative"

// hasFreshNegative reports whether a fresh (inside its TTL window) negative
// entry stands for the path — the "already remembered miss" the engine's own
// step 3 would answer without writing anything new.
func (s *service) hasFreshNegative(ctx context.Context, member, path string) bool {
	entry, err := s.md.Remote().GetCache(ctx, member, path)
	if err != nil || entry.Kind != negativeEntryKind {
		return false
	}
	expires, perr := time.Parse(time.RFC3339, entry.ExpiresAt)
	return perr == nil && s.nowFn().Before(expires)
}

// clearProbedNegative drops the negative row an exploratory miss just wrote
// (ADR-0013: "virtual member scans must not pollute the caches"). Failure is
// logged, never propagated: the row's only effect would have been sparing
// the member one upstream request inside the miss window.
func (s *service) clearProbedNegative(ctx context.Context, virtualKey, member, path string) {
	if err := s.md.Remote().DeleteCache(ctx, member, path); err != nil && !errors.Is(err, metadata.ErrRemoteCacheNotFound) {
		slog.WarnContext(ctx, "repo: could not clear the negative row of an exploratory virtual miss",
			"virtual", virtualKey, "member", member, "path", path, "error", err.Error())
	}
}

// ---- the T-72 aggregation seam ----

// VirtualMember is one step of a virtual repository's two-bucket order in
// the shape the protocol metadata aggregations consume (T-72): the member
// key, its class (local members answer from their nodes, remote members
// through the FR-20 pull-through chain) and the priority-bucket mark the
// maven foundByPriority short-circuit keys on.
type VirtualMember struct {
	Key      string
	Type     string // TypeLocal | TypeRemote
	Priority bool
}

// VirtualMemberOrder implements Service.VirtualMemberOrder.
func (s *service) VirtualMemberOrder(ctx context.Context, virtualKey string) ([]VirtualMember, error) {
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	out := make([]VirtualMember, 0, len(order))
	for _, m := range order {
		out = append(out, VirtualMember{Key: m.key, Type: m.typ, Priority: s.memberIsPriority(ctx, m.key)})
	}
	return out, nil
}

// memberIsPriority re-reads one member's priority mark for the exported
// order. virtualMemberOrder consumed the same probe internally but does not
// carry the flag; a member whose row vanished between the two reads answers
// false (the unmarked bucket), which only reorders a member that is about to
// disappear from the ledger anyway.
func (s *service) memberIsPriority(ctx context.Context, member string) bool {
	row, err := s.md.Repos().Get(ctx, member)
	if err != nil {
		return false
	}
	return memberPriorityResolution(row.Config)
}

// ReadVirtualMember implements Service.ReadVirtualMember: one member's copy
// of a metadata document, read WITHOUT re-gating the caller — the virtual
// read gate has already run on the VIRTUAL key (the same posture as
// getVirtual: members are resolution internals, not addressed surfaces).
//
// The membership guard is what makes the ungated read safe by construction:
// the member must currently sit in the virtual's order, so the call can
// never be steered at an arbitrary repository.
//
// Negative caching deliberately STAYS (the difference against T-71's
// exploratory artifact probes): an aggregation member read IS a real member
// read of a metadata path — the exact case FR-20's miss cache exists for —
// and the pypi aggregation's PRD line explicitly reuses it ("remote 成员的
// 包不存在走独立负缓存，复用 FR-20 参数"). ADR-0013's "exploratory miss
// leaves no residue" governs download-resolution scans, not this face.
func (s *service) ReadVirtualMember(ctx context.Context, virtualKey, member, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	if err := validateNodePath(path); err != nil {
		return nil, nil, err
	}
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, nil, err
	}
	var step *virtualMember
	for i := range order {
		if order[i].key == member {
			step = &order[i]
			break
		}
	}
	if step == nil {
		return nil, nil, fmt.Errorf("read member %s of virtual %s: %w: not a current member",
			member, virtualKey, ErrRepoNotFound)
	}
	if step.typ == TypeRemote {
		return s.readRemoteMemberDoc(ctx, virtualKey, member, path)
	}
	rc, node, hit, err := s.probeLocalMember(ctx, member, path)
	if err != nil {
		return nil, nil, err
	}
	if !hit {
		return nil, nil, fmt.Errorf("node %s/%s: %w", member, path, ErrNodeNotFound)
	}
	s.markDownload(ctx, nil, downloadMark{
		auditRepo: virtualKey, auditPath: path,
		countRepo: member, countPath: path,
		origin:       downloadOriginVirtual(virtualKey),
		extra:        []string{fmt.Sprintf(`"resolvedFrom":%q`, member), `"aggregate":true`},
		remoteServed: false,
	})
	return rc, node, nil
}

// readRemoteMemberDoc walks one remote member through the FR-20 chain for a
// metadata read: a result is the member's answer (fresh, landed or
// stale-serving — HasCopy is always true there), an UNFOUND miss maps onto
// ErrNodeNotFound so aggregations treat it as "member has no such document",
// and a classified non-unfound failure keeps its exact rendering for the
// caller to propagate or tolerate per its protocol's rule.
func (s *service) readRemoteMemberDoc(ctx context.Context, virtualKey, member, path string) (io.ReadSeekCloser, *metadata.Node, error) {
	if s.remoteEng == nil {
		return nil, nil, fmt.Errorf("%w: remote repositories have no engine wired", ErrRepoTypeNotSupported)
	}
	res, err := s.remoteEng.Fetch(ctx, member, path)
	if err != nil {
		var fe *remote.FetchError
		if errors.As(err, &fe) {
			if fe.Unfound {
				return nil, nil, fmt.Errorf("node %s/%s: %w", member, path, ErrNodeNotFound)
			}
			return nil, nil, &StatusError{Code: fe.Status, Message: fe.Message}
		}
		return nil, nil, fmt.Errorf("virtual %s remote member %s fetch %s: %w", virtualKey, member, path, err)
	}
	if !res.HasCopy {
		slog.WarnContext(ctx, "repo: virtual remote member returned a copyless result — treated as a miss",
			"virtual", virtualKey, "member", member, "path", path)
		return nil, nil, fmt.Errorf("node %s/%s: %w", member, path, ErrNodeNotFound)
	}
	// The aggregation face's remote-member twin: the member's cache served
	// the document, so its row takes the remote column as well.
	s.markDownload(ctx, nil, downloadMark{
		auditRepo: virtualKey, auditPath: path,
		countRepo: member, countPath: path,
		origin:       downloadOriginVirtual(virtualKey),
		extra:        []string{fmt.Sprintf(`"resolvedFrom":%q`, member), `"aggregate":true`},
		remoteServed: true,
	})
	return res.Body, res.Node, nil
}

// ListVirtualMember implements Service.ListVirtualMember: the ungated node
// facts of one LOCAL member under a prefix — the input the protocols whose
// metadata is REGENERATED from storage facts (the PyPI simple index) merge
// on. Remote members answer an honest refusal: their aggregated state is an
// upstream document, not a node listing (ReadVirtualMember is that face).
func (s *service) ListVirtualMember(ctx context.Context, virtualKey, member, prefix string) ([]*metadata.Node, error) {
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/")
		if err := validateNodePath(prefix); err != nil {
			return nil, err
		}
	}
	order, err := s.virtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	var step *virtualMember
	for i := range order {
		if order[i].key == member {
			step = &order[i]
			break
		}
	}
	if step == nil {
		return nil, fmt.Errorf("list member %s of virtual %s: %w: not a current member",
			member, virtualKey, ErrRepoNotFound)
	}
	if step.typ != TypeLocal {
		return nil, fmt.Errorf("list member %s of virtual %s: %w: remote members are read as documents, not listings",
			member, virtualKey, ErrRepoTypeNotSupported)
	}
	nodes, err := s.md.Nodes().ListByPrefix(ctx, member, prefix)
	if err != nil {
		return nil, fmt.Errorf("list %s/%s: %w", member, prefix, err)
	}
	return nodes, nil
}

// ---- the resolution hint wrapper ----

// withResolvedFrom decorates a member body with X-BinFlow-Resolved-From and
// merges whatever hints the body already carries — a remote member's reader
// brings X-BinFlow-Cache (and X-Binflow-Upstream-Error on stale service), a
// local member's brings none. The adapter-side probe stays structural (the
// same interface{} seam T-66 introduced), so no upper layer learns what a
// virtual repository is.
func withResolvedFrom(body io.ReadSeekCloser, member string) io.ReadSeekCloser {
	h := http.Header{}
	if extra, ok := body.(interface{ ExtraHeaders() http.Header }); ok {
		for k, vv := range extra.ExtraHeaders() {
			h[k] = append([]string(nil), vv...)
		}
	}
	h.Set(HdrResolvedFrom, member)
	return &resolvedFromReader{ReadSeekCloser: body, hints: h}
}

// resolvedFromReader carries the resolution hints across the service
// boundary.
type resolvedFromReader struct {
	io.ReadSeekCloser
	hints http.Header
}

// ExtraHeaders implements the adapter-side probe seam.
func (r *resolvedFromReader) ExtraHeaders() http.Header { return r.hints }

// ---- write routing ----

// routeVirtualWrite resolves the write target of a virtual repository
// (FR-21-AC4, P1): the configured defaultDeploymentRepo, or RE-08's 405 when
// none is. The returned key is the repository the write actually lands in —
// the caller swaps its addressing onto it and runs the ORDINARY local write
// plane, so permissions, the overwrite pair and the checksum chain all
// follow the TARGET repository's semantics verbatim (the AC's wording), and
// the landed node is immediately visible to virtual reads (M53: resolution
// is computed per request off the member ledger).
//
// The target's existence and class are NOT re-verified here: the caller's
// re-load of the swapped key answers not-found and class refusals through
// the existing, tested paths (a drift after the config-time validation
// surfaces as ErrRepoNotFound, the honest answer for a vanished target).
func (s *service) routeVirtualWrite(row *metadata.Repo) (string, error) {
	if target := virtualRouteTarget(row.Config); target != "" {
		return target, nil
	}
	return "", refuseVirtualWrite(row.RepoKey)
}

// virtualRouteTarget tolerantly reads the write-route target out of a
// virtual repository's config JSON: the primary spelling of the canonical
// form plus the two Artifactory aliases raw-seeded rows may carry. The write
// plane only asks ONE question — "is a local deployment repository
// configured?" — so a config that fails the strict shape (an empty or
// hand-mangled blob written outside CreateRepo/UpdateRepo; adapter
// harnesses seed `{}` rows) still gets its truthful answer: no route, the
// C5 405. The strict validation (members exist, target is a local member)
// lives at config time; a drifted target surfaces through the caller's
// target re-load, not here.
func virtualRouteTarget(config string) string {
	var probe struct {
		DefaultDeploymentRepo    string `json:"defaultDeploymentRepo"`
		DefaultDeploymentRepoRef string `json:"defaultDeploymentRepoRef"`
		DeploymentRepository     string `json:"deploymentRepository"`
	}
	if err := json.Unmarshal([]byte(config), &probe); err != nil {
		return ""
	}
	for _, alias := range []string{probe.DefaultDeploymentRepo, probe.DefaultDeploymentRepoRef, probe.DeploymentRepository} {
		if alias != "" {
			return alias
		}
	}
	return ""
}

// virtualWriteRouted reports whether the virtual repository's config
// carries a write route (the DELETE refusal picks its wording on it — see
// refuseVirtualDelete).
func virtualWriteRouted(config string) bool {
	return virtualRouteTarget(config) != ""
}

// resolveWriteRepo resolves the repository a content write actually lands
// in. Virtual repositories route onto their configured local deployment
// member first (T-71): the returned row is then the TARGET's, so everything
// downstream of this call — the permission pair, the overwrite check, the
// checksum chain, the audit's repository — runs the TARGET repository's
// ordinary semantics verbatim (FR-21-AC4), and the landed node is visible
// to virtual reads on the very next request (M53). Every other non-local
// class keeps its refusal (RE-05 for remote).
func (s *service) resolveWriteRepo(ctx context.Context, repoKey string) (string, *metadata.Repo, error) {
	row, err := s.loadRepoRow(ctx, repoKey)
	if err != nil {
		return "", nil, err
	}
	if row.Type == TypeVirtual {
		target, err := s.routeVirtualWrite(row)
		if err != nil {
			return "", nil, err
		}
		repoKey = target
		if row, err = s.loadRepoRow(ctx, repoKey); err != nil {
			return "", nil, err
		}
	}
	if err := refuseNonLocalWrite(row); err != nil {
		return "", nil, err
	}
	return repoKey, row, nil
}
