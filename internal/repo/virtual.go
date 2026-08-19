package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
// priority mark is consumed by the ordering pass and not carried.
type virtualMember struct {
	key string
	typ string // TypeLocal | TypeRemote
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
			m := virtualMember{key: member.RepoKey, typ: member.Type}
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
// Member semantics per class:
//
//   - local: a plain node lookup. A folder row is NOT a download hit — the
//     aggregate browse is P2 (FR-21-AC8) — so folder-only members keep the
//     walk moving toward a member that actually holds the artifact.
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
		s.audit(ctx, AuditEvent{
			Actor: actor(p), Action: AuditActionDownload, Repo: virtualKey, Path: path,
			Detail: fmt.Sprintf(`{"resolvedFrom":%q}`, m.key),
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
		// Folder rows carry no body; aggregate directory reads are P2.
		return nil, nil, false, nil
	}
	rc, _, err := s.st.Open(ctx, n.Sha256)
	if err != nil {
		// Blob row present, file gone: the same repair-case posture as the
		// local Get — surfaced, never silently skipped.
		return nil, nil, false, fmt.Errorf("open blob %s for %s/%s: %w", n.Sha256, member, path, err)
	}
	return rc, n, true, nil
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
