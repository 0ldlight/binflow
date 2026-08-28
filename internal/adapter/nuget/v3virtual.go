package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual v3 search merge (nuget.md section 8.2, the six steps):
//
//  1. member resolution through the two priority buckets — local and
//     remote members stand SIDE BY SIDE inside one bucket
//     (PRIORITISED_{LOCAL,REMOTE} before NON_PRIORITISED_{…});
//  2. fan-out — local members answer from metadata facts (the term's
//     filter words over the stored catalogue), remote members through the
//     section 8.1 upstream proxy at the fixed candidate page take=1000;
//  3. bucket merge — group by package id, the representative is the
//     HIGHEST semver entry, versions the union of every member's set;
//  4. cross-bucket merge is id-level putIfAbsent: a package id a higher
//     bucket answered never surfaces from a lower one (the v2 section
//     7.2-5 twin);
//  5. pagination runs IN MEMORY over the merged set, totalHits the full
//     merged size;
//  6. the output's registration URLs cite this virtual's
//     registration[-semver2] base (semVerLevel requests the -semver2
//     spelling — section 8.1-7).
//
// The single REMOTE repository's live proxy (section 8.1) lives here too:
// the body is served with every URL token rewritten, the upstream's own
// field set intact.

// v3RemoteSearchBody runs the section 8.1 chain for one remote repository
// and returns the REWRITTEN body (ok false whenever the chain gave
// nothing — the caller degrades to the landed-facts walk).
func (h *Handler) v3RemoteSearchBody(ctx context.Context, r *http.Request, p *repo.Principal, repoKey string, q v3SearchQuery) ([]byte, bool) {
	read := h.v3RepoReader(ctx, p, repoKey)
	body, ok := h.v3UpstreamSearch(ctx, repoKey, read, q)
	if !ok {
		return nil, false
	}
	idx := v3ResolveUpstreamIndex(read)
	tbl := v3BuildRewriteTable(h.baseURLFor(r), repoKey, idx, false, q.semVerLevel != "")
	out := v3RewriteJSON(body, tbl)
	var probe searchResponse
	if json.Unmarshal(out, &probe) != nil {
		return nil, false
	}
	return out, true
}

// v3MergedHit is one package id's merge state (steps 3–4).
type v3MergedHit struct {
	best     *searchHit
	versions map[string]bool
}

// serveV3VirtualSearch renders the merged virtual answer.
func (h *Handler) serveV3VirtualSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey string, q v3SearchQuery) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, "")
		return
	}
	origin := h.baseURLFor(r)

	var hits []*searchHit
	answered := map[string]bool{} // ids the answered buckets own (step 4)
	for _, prioritised := range []bool{true, false} {
		bucket := map[string]*v3MergedHit{} // this bucket's merged set (step 3)
		for _, m := range order {
			if m.Priority != prioritised {
				continue
			}
			var memberHits []*searchHit
			if m.Type == repo.TypeLocal {
				memberHits = h.v3LocalMemberHits(ctx, repoKey, m.Key, q)
			} else {
				memberHits = h.v3RemoteMemberHits(ctx, repoKey, m.Key, q)
			}
			for _, hit := range memberHits {
				if hit == nil || hit.ID == "" {
					continue
				}
				id := lowerASCII(hit.ID)
				mh, seen := bucket[id]
				if !seen {
					mh = &v3MergedHit{best: hit, versions: map[string]bool{}}
					bucket[id] = mh
				}
				if compareNuGetVersions(hit.Version, mh.best.Version) > 0 {
					mh.best = hit // the representative is the highest semver entry
				}
				if len(hit.Versions) == 0 {
					mh.versions[hit.Version] = true
				}
				for _, v := range hit.Versions {
					if v.Version != "" {
						mh.versions[v.Version] = true
					}
				}
			}
		}
		for id, mh := range bucket {
			if answered[id] {
				continue // step 4: the id belongs to a higher bucket
			}
			out := mh.best
			versions := make([]string, 0, len(mh.versions))
			for v := range mh.versions {
				versions = append(versions, v)
			}
			sort.Slice(versions, func(i, j int) bool {
				return compareNuGetVersions(versions[i], versions[j]) < 0
			})
			out.Versions = make([]searchHitVersion, 0, len(versions))
			for _, v := range versions {
				out.Versions = append(out.Versions, searchHitVersion{Version: v})
			}
			// Step 6: the merged output cites THIS virtual's registration
			// base (the -semver2 spelling for semVerLevel requests).
			base := regBase(origin, repoKey)
			if q.semVerLevel != "" {
				base = regSemVer2Base(origin, repoKey)
			}
			out.Registration = base + lowerASCII(out.ID) + "/" + fileIndex
			hits = append(hits, out)
			answered[id] = true
		}
	}
	h.writeSearchPage(w, hits, q.skip, q.take)
}

// v3LocalMemberHits renders one LOCAL member's facts as candidate hits
// (the section 8.2-2 metadata arm — the term and prerelease filters the
// facts walk applies).
func (h *Handler) v3LocalMemberHits(ctx context.Context, virtualKey, member string, q v3SearchQuery) []*searchHit {
	packages := h.memberSearchFacts(ctx, virtualKey, member, q.term, q.prerelease)
	hits := make([]*searchHit, 0, len(packages))
	for _, pkg := range packages {
		hits = append(hits, renderSearchHit(pkg, q.prerelease))
	}
	return hits
}

// memberSearchFacts is the facts walk through the member seam (the
// member's stored nupkg nodes, canonical and marker-landed alike).
func (h *Handler) memberSearchFacts(ctx context.Context, virtualKey, member, term string, includePrerelease bool) []*searchPackage {
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, member, "")
	if err != nil {
		return nil
	}
	grouped := map[string][]*metadata.Node{}
	for _, n := range nodes {
		if id, _, ok := splitAnyNupkgNode(n.Path); ok {
			grouped[id] = append(grouped[id], n)
		}
	}
	out := make([]*searchPackage, 0, len(grouped))
	for id, nodes := range grouped {
		if term != "" && !strings.Contains(id, term) {
			continue
		}
		facts := make([]versionFacts, 0, len(nodes))
		for _, n := range nodes {
			_, version, _ := splitAnyNupkgNode(n.Path)
			facts = append(facts, versionFacts{ref: pkgRef{id: id, version: version}, node: n})
		}
		if !renderableFacts(facts, includePrerelease) {
			continue
		}
		sort.Slice(facts, func(i, j int) bool {
			return compareNuGetVersions(facts[i].ref.version, facts[j].ref.version) < 0
		})
		out = append(out, &searchPackage{id: id, facts: facts})
	}
	return out
}

// v3RemoteMemberHits pulls one REMOTE member's live candidates (section
// 8.2-2: the section 8.1 proxy at the fixed take=1000, every URL already
// rewritten onto the MEMBER's bases — the merged output re-anchors onto
// the virtual's in step 6).
func (h *Handler) v3RemoteMemberHits(ctx context.Context, virtualKey, member string, q v3SearchQuery) []*searchHit {
	read := h.v3MemberReader(ctx, virtualKey, member)
	candidates := q
	candidates.skip = 0
	candidates.take = v3VirtualMemberTake
	body, ok := h.v3UpstreamSearch(ctx, member, read, candidates)
	if !ok {
		return nil
	}
	idx := v3ResolveUpstreamIndex(read)
	tbl := v3BuildRewriteTable("", member, idx, false, false)
	var doc searchResponse
	if json.Unmarshal(v3RewriteJSON(body, tbl), &doc) != nil {
		return nil
	}
	return doc.Data
}
