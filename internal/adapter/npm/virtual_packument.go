package npm

// The virtual-repository packument merge (T-72, FR-21-AC5; maven-npm-pypi.md
// section 2.6, high confidence): a package document read through a VIRTUAL
// repository is the in-memory merge of every member's own packument, walked
// in the two-bucket order — computed PER REQUEST (the Artifactory virtual
// cache's TTL-600 optimization is deliberately not replicated; the PRD's
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
//   - latest is RECOMPUTED off the merged version set (the union may crown
//     a version no single member would have tagged).
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

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

// mergeVirtualPackuments folds the member documents (two-bucket order)
// into one: base identity and generic fields, putIfAbsent versions, the
// dist-tags/time unions, latest recomputed.
func mergeVirtualPackuments(members []memberPackument) map[string]any {
	base := copyDoc(members[0].doc)
	if len(members) == 1 {
		recomputeLatestTag(base)
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
	recomputeLatestTag(base)
	return base
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

// virtualDeploymentTarget is the tolerant write-route probe of a virtual
// repository's config JSON (the npm-side restatement of repo's own reader —
// adapter packages share no unexported code, the area rule): the primary
// spelling plus the two Artifactory aliases raw-seeded rows may carry. A
// config that fails the strict shape still gets its truthful answer: no
// route.
func virtualDeploymentTarget(config string) string {
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
