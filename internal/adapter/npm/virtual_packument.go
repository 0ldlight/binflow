package npm

// The virtual-repository packument merge (T-72, FR-21-AC5; maven-npm-pypi.md
// section 2.6, high confidence): a package document read through a VIRTUAL
// repository is the in-memory merge of every member's own packument, walked
// in the four-bucket order with the §5.3 cache dedup applied (T-538: the
// walk consumes the Facet discriminant of design virtual-four-bucket.md
// §2.1 — every cache-facet step is dropped whenever the order carries any
// remote body step, so one remote merges as ONE unit, never also its
// standing copy) — computed PER REQUEST (the Artifactory virtual cache's
// TTL-600 optimization is deliberately not replicated; the PRD's
// "语义等价，缓存优化 M4" ruling).
//
// Merge rules:
//
//   - the FIRST member holding the document is the BASE: its identity
//     fields and its generic root fields (description, readme, ...) win —
//     "通用字段取先到者";
//   - later members contribute versions putIfAbsent (an existing version is
//     never overwritten — the first member's copy is the one resolution
//     would serve anyway);
//   - dist-tags and time are key-wise unions under the same first-wins
//     rule;
//   - latest follows the §2.6 CONDITIONAL ("被排除模式过滤后最新版本与
//     `latest` 标签重算", L031 r2≡r3 live evidence): the base member's
//     tag value passes through untouched while its target survives the
//     merged version set — with no exclusion pattern in effect the
//     reference (Artifactory 7.161.26) keeps latest even when it points
//     below the union's greatest; the recompute (repoint at the greatest
//     visible version) fires only when the target is no longer in the
//     version set. The unconditional recompute T-538 shipped was a
//     misreading of that clause (T-548).
//
// Failure policy (the PRD gives npm no explicit member-failure clause; the
// ruling follows the collection posture of the pypi face, its closest
// mechanism relative): an unfound member contributes nothing; a CLASSIFIED
// member failure (the remote engine's SSRF 400, hardFail 502) is skipped so
// one bad member cannot block the package — but when NO member produced a
// document, the remembered failure is the honest answer instead of a bare
// 404.
//
// The merged response keeps the base member's X-BinFlow-Resolved-From (plus
// whatever hints its stream carried — a remote base's cache headers): the
// base is the contributor that wins every tie, which is exactly the
// diagnostic the header exists for.
//
// Write faces NEVER see the merge: a publish/dist-tag/unpublish through a
// routed virtual lands in the deployment member, and saving a merged
// document there would copy the other members' versions into it — the
// write plane reads the TARGET member's own document (loadPackumentForWrite
// below).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// packumentReadLimit bounds one member document read (real packuments run
// to a few hundred KB; beyond this the member is treated as unreadable and
// skipped — the same tolerance as an unparseable document).
const packumentReadLimit = 16 << 20

// memberPackument is one member's parsed contribution.
type memberPackument struct {
	member string
	node   *metadata.Node
	hints  http.Header
	doc    map[string]any
}

// loadVirtualPackument reads the merged packument of a virtual repository.
// The returned node is SYNTHETIC: the base member's timestamps with an
// empty sha256, so the serving face's ETag falls back to the RENDERED
// merged body's sha1 (the ledger row would name one member's bytes) while
// Last-Modified keeps a real timestamp.
func (h *Handler) loadVirtualPackument(ctx context.Context, repoKey, name string) (map[string]any, *metadata.Node, http.Header, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		return nil, nil, nil, err
	}
	order = dedupeCacheFacetSteps(ctx, order)
	var (
		members []memberPackument
		failure *repo.StatusError
	)
	for _, m := range order {
		rc, node, err := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, packumentPath(name))
		if err != nil {
			if errors.Is(err, repo.ErrNodeNotFound) {
				continue // member does not carry the package
			}
			var se *repo.StatusError
			if errors.As(err, &se) {
				if failure == nil {
					failure = se
				}
				continue // one member's classified fault must not block the rest
			}
			return nil, nil, nil, err // an internal fault is a honest 500
		}
		hints := readerHints(rc)
		raw, rerr := io.ReadAll(io.LimitReader(rc, packumentReadLimit))
		_ = rc.Close() //nolint:errcheck // read-only fd; the bytes are already in hand
		if rerr != nil {
			slog.WarnContext(ctx, "npm: virtual member packument unreadable — skipped",
				"virtual", repoKey, "member", m.Key, "error", rerr.Error())
			continue
		}
		doc, derr := decodeDoc(raw)
		if derr != nil {
			slog.WarnContext(ctx, "npm: virtual member packument unparseable — skipped",
				"virtual", repoKey, "member", m.Key, "error", derr.Error())
			continue
		}
		if hints == nil {
			hints = http.Header{}
		}
		hints.Set(repo.HdrResolvedFrom, m.Key)
		members = append(members, memberPackument{member: m.Key, node: node, hints: hints, doc: doc})
	}
	if len(members) == 0 {
		if failure != nil {
			return nil, nil, nil, failure
		}
		return nil, nil, nil, fmt.Errorf("packument %s/%s: %w", repoKey, name, repo.ErrNodeNotFound)
	}
	merged := mergeVirtualPackuments(members)
	node := &metadata.Node{
		UpdatedAt: members[0].node.UpdatedAt,
		CreatedAt: members[0].node.CreatedAt,
		// Sha256 deliberately empty: the ETag must hash the RENDERED merge.
	}
	return merged, node, members[0].hints, nil
}

// cacheFacetOfStep classifies one walk step's facet — the seam crossing the
// maven face's facetOfStep shares the shape of (T-531): explicit cases,
// never a bare comparison an unknown future facet value could silently
// coincide with. An unrecognized facet degrades to plain WITH a WARN: the
// step then walks as the ordinary member read (visible behavior), never a
// silent numeric accident (fail-open guard, maven's Reviewer B②).
func cacheFacetOfStep(ctx context.Context, m repo.VirtualMember) bool {
	switch m.Facet {
	case repo.FacetPlain:
		return false
	case repo.FacetCache:
		return true
	default:
		slog.WarnContext(ctx, "npm: unknown virtual member facet — treated as plain",
			slog.String("member", m.Key), slog.Uint64("facet", uint64(m.Facet)))
		return false
	}
}

// dedupeCacheFacetSteps applies the §5.3 cache dedup to one packument walk
// (design virtual-four-bucket.md §5.3 npm row): a member order that carries
// ANY remote body step drops EVERY cache-facet step — the body step's FR-20
// chain already holds the remote's cache semantics, and ReadVirtualMember
// matches members BY KEY (facet-insensitive), so walking both steps would
// query the same repository twice (同仓双查) and merge its document twice.
// An order with no remote body step keeps its cache steps: the only producer
// of such orders (design §2.3's far-end suppression gate, seam F7) does not
// exist yet, but the rule is stated against the sequence, so it already
// holds when that seam lands.
func dedupeCacheFacetSteps(ctx context.Context, order []repo.VirtualMember) []repo.VirtualMember {
	isCache := make([]bool, len(order))
	hasRemoteBody := false
	for i, m := range order {
		isCache[i] = cacheFacetOfStep(ctx, m)
		if m.Type == repo.TypeRemote && !isCache[i] {
			hasRemoteBody = true
		}
	}
	if !hasRemoteBody {
		return order
	}
	out := make([]repo.VirtualMember, 0, len(order))
	for i, m := range order {
		if !isCache[i] {
			out = append(out, m)
		}
	}
	return out
}

// mergeVirtualPackuments folds the member documents (two-bucket order)
// into one: base identity and generic fields, putIfAbsent versions, the
// dist-tags/time unions, latest repointed only when its target is gone
// (repointFilteredLatest below).
func mergeVirtualPackuments(members []memberPackument) map[string]any {
	base := copyDoc(members[0].doc)
	if len(members) == 1 {
		repointFilteredLatest(base)
		return base
	}
	versions := versionsOf(base)
	tags := distTagsOf(base)
	tm := mapOf(base["time"])
	for _, m := range members[1:] {
		for v, mv := range versionsOf(m.doc) {
			if versions[v] == nil {
				versions[v] = mv // putIfAbsent: the earlier member wins
			}
		}
		for tag, v := range distTagsOf(m.doc) {
			if _, ok := tags[tag]; !ok {
				tags[tag] = v
			}
		}
		if other := mapOf(m.doc["time"]); other != nil {
			if tm == nil {
				tm = map[string]any{}
			}
			for k, v := range other {
				if _, ok := tm[k]; !ok {
					tm[k] = v
				}
			}
		}
	}
	base["versions"] = versions
	tagAny := map[string]any{}
	for k, v := range tags {
		tagAny[k] = v
	}
	base["dist-tags"] = tagAny
	if tm != nil {
		base["time"] = tm
	}
	repointFilteredLatest(base)
	return base
}

// repointFilteredLatest is the merge's CONDITIONAL latest rule (spec
// section 2.6 "被排除模式过滤后……重算"; L031 r2≡r3: with no exclusion
// pattern in effect the reference keeps the base member's latest even when
// it points below the union's greatest — the recompute T-538 shipped
// unconditionally was a misreading, T-548): the base member's tag value
// passes through untouched while its target survives the merged version
// set; the recompute fires only when the target is gone from the version
// set (an exclusion filter removed it, or the base document itself
// dangles), repointing at the greatest visible version. An ABSENT latest
// stays absent at the merge — the read-time crown (crownLatest,
// disttag.go) owns that projection on every read face.
func repointFilteredLatest(doc map[string]any) {
	tags := mapOf(doc["dist-tags"])
	if tags == nil {
		return
	}
	latest := stringOf(tags["latest"])
	if latest == "" {
		return
	}
	versions := versionsOf(doc)
	if versions[latest] != nil {
		return // the target survived: the base member's original value stands
	}
	if best := latestVersion(versions); best != "" {
		tags["latest"] = best
	}
}

// recomputeLatestTag points the latest tag at the greatest merged version
// (the adapter's own latestVersion convention — the same rule the
// unpublish repoint uses): the union can crown a version no single member
// tagged, and a stale member's latest must not outrank the merged truth.
// A package whose versions are all gone keeps whatever tags remain.
func recomputeLatestTag(doc map[string]any) {
	versions := versionsOf(doc)
	if len(versions) == 0 {
		return
	}
	best := latestVersion(versions)
	tags := mapOf(doc["dist-tags"])
	if tags == nil {
		tags = map[string]any{}
		doc["dist-tags"] = tags
	}
	tags["latest"] = best
}

// loadPackumentForWrite is the WRITE plane's document loader: on a virtual
// repository it reads the DEPLOYMENT TARGET member's own document (never
// the merge), because the write lands there and saving a merged document
// would copy the other members' versions into the target. An un-routed
// virtual falls through to the ordinary first-hit load — its writes answer
// the C5 405 at the service boundary regardless of what is read here.
func (h *Handler) loadPackumentForWrite(ctx context.Context, p *Principal, repoKey, name string) (map[string]any, *metadata.Node, http.Header, error) {
	if h.repos != nil {
		if row, err := h.repos.Get(ctx, repoKey); err == nil && row.Type == repo.TypeVirtual {
			if target := virtualDeploymentTarget(row.Config); target != "" {
				return h.readMemberPackument(ctx, repoKey, target, name)
			}
		}
	}
	return h.loadPackument(ctx, p, repoKey, name)
}

// readMemberPackument reads one member's own document through the
// ungated aggregation seam (the caller's write grant does not imply a read
// grant on the member — the same posture as the merged read face). A
// member without the document is a FRESH package, not an error.
func (h *Handler) readMemberPackument(ctx context.Context, virtualKey, member, name string) (map[string]any, *metadata.Node, http.Header, error) {
	rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, packumentPath(name))
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return nil, nil, nil, fmt.Errorf("packument %s/%s: %w", member, name, repo.ErrNodeNotFound)
		}
		return nil, nil, nil, err
	}
	hints := readerHints(rc)
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, packumentReadLimit))
	if rerr != nil {
		return nil, nil, nil, fmt.Errorf("read packument %s/%s: %w", member, name, rerr)
	}
	doc, derr := decodeDoc(raw)
	if derr != nil {
		return nil, nil, nil, fmt.Errorf("parse packument %s/%s: %w", member, name, derr)
	}
	return doc, node, hints, nil
}

// virtualDeploymentTarget is the npm-side thin alias of the adapter base's
// single-source write-route probe (T-590 hoist; the semantics and their
// golden live in internal/adapter/deploytarget.go).
func virtualDeploymentTarget(config string) string {
	return adapter.VirtualDeploymentTarget(config)
}
