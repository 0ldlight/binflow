package nuget

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The search face (the official Search Query Service spec) — the
// q/take/skip/prerelease/semVerLevel set.
//
// LOCAL repositories answer from STORED FACTS (the T-287 posture, the
// T-304 L3 local-half ruling: unchanged). REMOTE repositories answer from
// the UPSTREAM'S OWN SEARCH SERVICE in real time (nuget.md section 8.1 —
// the T-304 L3'/L7 ruling): the SearchQueryService @id resolved through
// the cached upstream service index, queried directly, the answer's URLs
// rewritten onto this instance; every failure of that chain degrades to
// the repository's landed facts (the offline arm — never a 5xx).
// VIRTUAL repositories merge the two (section 8.2): local members'
// metadata facts and remote members' live upstream candidates in the
// same priority bucket, id-level putIfAbsent across buckets, pagination
// in memory over the merged set (v3virtual.go).

// searchResponse is the query body.
type searchResponse struct {
	TotalHits int          `json:"totalHits"`
	Data      []*searchHit `json:"data"`
}

// searchHit is one package row.
type searchHit struct {
	ID             string             `json:"id"`
	Version        string             `json:"version"`
	Description    string             `json:"description"`
	Summary        string             `json:"summary"`
	Title          string             `json:"title"`
	LicenseURL     string             `json:"licenseUrl"`
	ProjectURL     string             `json:"projectUrl"`
	IconURL        string             `json:"iconUrl"`
	Authors        []string           `json:"authors"`
	Owners         []string           `json:"owners"`
	Tags           []string           `json:"tags"`
	TotalDownloads int64              `json:"totalDownloads"`
	Verified       bool               `json:"verified"`
	Versions       []searchHitVersion `json:"versions"`
	// Registration is the hit's registration-index URL (section 8.2-6: the
	// output's registration base — the -semver2 spelling when the request
	// carried semVerLevel). Empty on local facts rows before the virtual
	// merge annotates them.
	Registration string `json:"registration,omitempty"`
}

// searchHitVersion is one version row of a hit.
type searchHitVersion struct {
	Version   string `json:"version"`
	Downloads int64  `json:"downloads"`
}

// searchMaxTake is the take ceiling (the official service bounds take;
// the pilot caps the facts walk at the same order).
const searchMaxTake = 1000

// searchDefaultTake is the official default page size.
const searchDefaultTake = 20

// serveSearch renders GET query.
func (h *Handler) serveSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, class string) {
	q := r.URL.Query()
	term := lowerASCII(trimSpaceASCII(q.Get("q")))
	take := parseBounded(q.Get("take"), searchDefaultTake)
	skip := parseBounded(q.Get("skip"), 0)
	includePrerelease := q.Get("prerelease") == "true"

	switch class {
	case repo.TypeRemote:
		// Section 8.1's live proxy; the landed-facts walk behind it.
		sq := v3SearchQuery{term: term, skip: skip, take: take, prerelease: includePrerelease, semVerLevel: q.Get("semVerLevel")}
		if body, ok := h.v3RemoteSearchBody(ctx, r, p, repoKey, sq); ok {
			writeJSON(w, http.StatusOK, body)
			return
		}
		h.serveSearchFacts(ctx, w, p, repoKey, term, take, skip, includePrerelease, true)
	case repo.TypeVirtual:
		h.serveV3VirtualSearch(ctx, w, r, repoKey, v3SearchQuery{
			term: term, skip: skip, take: take, prerelease: includePrerelease, semVerLevel: q.Get("semVerLevel"),
		})
	default:
		h.serveSearchFacts(ctx, w, p, repoKey, term, take, skip, includePrerelease, false)
	}
}

// serveSearchFacts renders the stored-facts answer (the local class, and
// the remote class's offline degradation). deep widens the remote walk to
// every stored nupkg-shaped node — the .nuGetV3 markers land packages
// outside the canonical tree.
func (h *Handler) serveSearchFacts(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, term string, take, skip int, includePrerelease, deep bool) {
	packages := h.searchFacts(ctx, p, repoKey, term, includePrerelease, deep)
	hits := make([]*searchHit, 0, len(packages))
	for _, pkg := range packages {
		hits = append(hits, renderSearchHit(pkg, includePrerelease))
	}
	h.writeSearchPage(w, hits, skip, take)
}

// writeSearchPage sorts, pages and renders one hit set (totalHits = the
// full size, section 8.2-5).
func (h *Handler) writeSearchPage(w http.ResponseWriter, hits []*searchHit, skip, take int) {
	sort.Slice(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })
	total := len(hits)
	lo, hi := skip, skip+take
	if lo > total {
		lo = total
	}
	if hi > total {
		hi = total
	}
	body, merr := json.Marshal(searchResponse{TotalHits: total, Data: hits[lo:hi]})
	if merr != nil {
		writePlain(w, http.StatusInternalServerError, merr.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// searchFacts collects the stored-facts package rows (the term and
// prerelease filters applied). deep selects the any-node identity walk
// (the remote degradation); the local class keeps the strict canonical
// walk (sidecar noise and foreign files never surface).
func (h *Handler) searchFacts(ctx context.Context, p *repo.Principal, repoKey, term string, includePrerelease, deep bool) []*searchPackage {
	var grouped map[string][]*metadata.Node
	if deep {
		nodes, err := h.svc.List(ctx, p, repoKey, "")
		if err != nil {
			return nil // the local-only listing seam: a remote walk degrades to empty
		}
		grouped = map[string][]*metadata.Node{}
		for _, n := range nodes {
			if id, _, ok := splitAnyNupkgNode(n.Path); ok {
				grouped[id] = append(grouped[id], n)
			}
		}
	} else {
		nodes, err := h.svc.List(ctx, p, repoKey, "")
		if err != nil {
			return nil
		}
		grouped = groupNodesByPackage(nodes)
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

// searchPackage is one package's fact bundle.
type searchPackage struct {
	id    string
	facts []versionFacts
}

// groupNodesByPackage groups one node listing by package id (the nupkg
// nodes only — sidecars and foreign files never surface).
func groupNodesByPackage(nodes []*metadata.Node) map[string][]*metadata.Node {
	grouped := map[string][]*metadata.Node{}
	for _, n := range nodes {
		id, _, ok := splitNupkgPath(n.Path)
		if !ok {
			continue
		}
		grouped[id] = append(grouped[id], n)
	}
	return grouped
}

// splitNupkgPath splits a stored flatcontainer nupkg path into
// (id, version); ok is false for every other shape.
func splitNupkgPath(path string) (string, string, bool) {
	id, rest, found := strings.Cut(path, "/")
	if !found || !validPackageID(id) {
		return "", "", false
	}
	version, file, found := strings.Cut(rest, "/")
	if !found {
		return "", "", false
	}
	if file != id+"."+version+suffixNupkg {
		return "", "", false
	}
	if _, ok := normalizeNuGetVersion(version); !ok {
		return "", "", false
	}
	return lowerASCII(id), version, true
}

// renderableFacts reports whether any version survives the prerelease
// filter.
func renderableFacts(facts []versionFacts, includePrerelease bool) bool {
	for _, f := range facts {
		if includePrerelease || !isPrereleaseVersion(f.ref.version) {
			return true
		}
	}
	return false
}

// renderSearchHit renders one package row (the best version first: the
// newest stable, or the newest prerelease when that is all there is; the
// description family stays empty on the facts-only walk — the search hit
// is identity + versions, the registration is the metadata authority).
func renderSearchHit(pkg *searchPackage, includePrerelease bool) *searchHit {
	hit := &searchHit{
		ID:       pkg.id,
		Authors:  []string{},
		Owners:   []string{},
		Tags:     []string{},
		Versions: []searchHitVersion{},
	}
	var best versionFacts
	have := false
	for i := len(pkg.facts) - 1; i >= 0; i-- {
		if !includePrerelease && isPrereleaseVersion(pkg.facts[i].ref.version) {
			continue
		}
		best, have = pkg.facts[i], true
		break
	}
	if !have {
		best = pkg.facts[len(pkg.facts)-1]
	}
	hit.Version = best.ref.version
	for _, f := range pkg.facts {
		hit.Versions = append(hit.Versions, searchHitVersion{Version: f.ref.version})
	}
	return hit
}

// parseBounded parses a non-negative query integer with a default and the
// take ceiling.
func parseBounded(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if n > searchMaxTake {
		return searchMaxTake
	}
	return n
}
