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

// The search face (the official Search Query Service spec) — the minimal
// q/take/skip/prerelease set PRD 88.1 scopes.
//
// BinFlow's search answers from STORED FACTS: the local class's packages,
// the remote class's landed pull-through copies, and the virtual class's
// local members (a remote member's uncached catalogue is not listable
// through the member seam — the discovery convenience only; restore and
// add-package-with-explicit-version never consult search). The upstream's
// own search service (azuresearch on nuget.org) lives on a different host
// from the configured upstream base and carries its query state in the
// URL, neither of which the engine's path-joined upstream hop can
// address. T-287 ruling, registered for the spec ticket.

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

	packages, err := h.searchIndex(ctx, p, repoKey, class)
	if err != nil {
		h.writeError(w, err, repoKey, "")
		return
	}

	hits := make([]*searchHit, 0, len(packages))
	for _, pkg := range packages {
		if term != "" && !strings.Contains(pkg.id, term) {
			continue
		}
		if !renderableFacts(pkg.facts, includePrerelease) {
			continue
		}
		hits = append(hits, renderSearchHit(pkg, includePrerelease))
	}
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

// searchPackage is one package's fact bundle.
type searchPackage struct {
	id    string
	facts []versionFacts
}

// searchIndex collects the repository's package facts.
func (h *Handler) searchIndex(ctx context.Context, p *repo.Principal, repoKey, class string) ([]*searchPackage, error) {
	var grouped map[string][]*metadata.Node
	switch class {
	case repo.TypeVirtual:
		order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
		if err != nil {
			return nil, err
		}
		grouped = map[string][]*metadata.Node{}
		seen := map[string]bool{}
		for _, m := range order {
			if m.Type != repo.TypeLocal {
				continue // the member seam refuses remote listings (doc.go's note)
			}
			nodes, lerr := h.svc.ListVirtualMember(ctx, repoKey, m.Key, "")
			if lerr != nil {
				continue
			}
			for _, n := range nodes {
				id, _, ok := splitNupkgPath(n.Path)
				if !ok || seen[n.Path] {
					continue
				}
				seen[n.Path] = true
				grouped[id] = append(grouped[id], n)
			}
		}
	default:
		nodes, err := h.svc.List(ctx, p, repoKey, "")
		if err != nil {
			return nil, err
		}
		grouped = groupNodesByPackage(nodes)
	}

	out := make([]*searchPackage, 0, len(grouped))
	for id, nodes := range grouped {
		facts := make([]versionFacts, 0, len(nodes))
		for _, n := range nodes {
			_, version, _ := splitNupkgPath(n.Path)
			facts = append(facts, versionFacts{ref: pkgRef{id: id, version: version}, node: n})
		}
		sort.Slice(facts, func(i, j int) bool {
			return compareNuGetVersions(facts[i].ref.version, facts[j].ref.version) < 0
		})
		out = append(out, &searchPackage{id: id, facts: facts})
	}
	return out, nil
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
