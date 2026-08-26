package cargo

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The search face (spec section 7): the official contract shape —
// {"crates":[{"name","max_version","description"}],"meta":{"total":N}}.
//
// Matching is the spec's wildcard reading: *<q>* over the crate name and
// the description (case-insensitive — the crates.io posture), over at
// most 1000 scanned artifacts, deduplicated per crate at the highest
// non-yanked version. Authentication follows the global anonymous read
// policy (spec section 8's BinFlow decision) — an anonymous-enabled
// instance serves search bare, exactly the official no-Authorization
// posture; an instance that disables anonymous reads gates every content
// read, search included.

// searchScanCap bounds the facts walk (spec section 7's 1000).
const searchScanCap = 1000

// searchDefaultPage / searchMaxPage are per_page's default and ceiling.
const (
	searchDefaultPage = 10
	searchMaxPage     = 100
)

// searchResponse is the query body.
type searchResponse struct {
	Crates []*searchHit `json:"crates"`
	Meta   searchMeta   `json:"meta"`
}

// searchMeta carries the deduplicated total.
type searchMeta struct {
	Total int `json:"total"`
}

// searchHit is one crate row (the official three-field shape).
type searchHit struct {
	Name        string `json:"name"`
	MaxVersion  string `json:"max_version"`
	Description string `json:"description"`
}

// serveSearch implements GET api/v1/crates?q=…&per_page=….
func (h *Handler) serveSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string) {
	q := r.URL.Query()
	term := strings.ToLower(strings.TrimSpace(q.Get("q")))
	perPage := parsePerPage(q.Get("per_page"))

	nodes, err := h.svc.List(ctx, p, repoKey, dirCrates+"/")
	if err != nil {
		h.writeError(w, err, repoKey, "")
		return
	}

	type best struct {
		version     string
		description string
	}
	bestOf := map[string]*best{}
	scanned := 0
	for _, n := range nodes {
		if scanned >= searchScanCap {
			break
		}
		name, version, ok := splitCrateNode(n.Path)
		if !ok {
			continue
		}
		scanned++
		props := h.propsOf(ctx, repoKey, n.Path)
		if _, yanked := props[propYanked]; yanked {
			continue // spec section 7: yanked versions never surface
		}
		description := firstProp(props, propDescription)
		// The wildcard match runs against BOTH fact sources; the name test
		// is cheap and early, the description only for survivors of a
		// non-empty term.
		if term != "" &&
			!strings.Contains(strings.ToLower(name), term) &&
			!strings.Contains(strings.ToLower(description), term) {
			continue
		}
		b, seen := bestOf[name]
		if !seen {
			bestOf[name] = &best{version: version, description: description}
			continue
		}
		if compareSemver(version, b.version) > 0 {
			b.version, b.description = version, description
		}
	}

	names := make([]string, 0, len(bestOf))
	for name := range bestOf {
		names = append(names, name)
	}
	sort.Strings(names)

	hi := perPage
	if hi > len(names) {
		hi = len(names)
	}
	hits := make([]*searchHit, 0, hi)
	for _, name := range names[:hi] {
		b := bestOf[name]
		hits = append(hits, &searchHit{Name: name, MaxVersion: b.version, Description: b.description})
	}
	body, merr := json.Marshal(searchResponse{Crates: hits, Meta: searchMeta{Total: len(names)}})
	if merr != nil {
		writeEnvelope(w, http.StatusInternalServerError, merr.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// propsOf reads one node's properties (an empty map on any failure —
// search degrades to name-only facts, never errors).
func (h *Handler) propsOf(ctx context.Context, repoKey, path string) map[string][]string {
	if h.props == nil {
		return nil
	}
	props, err := h.props.List(ctx, repoKey, path)
	if err != nil {
		return nil
	}
	return props
}

// firstProp returns the first value of a single-valued property.
func firstProp(props map[string][]string, key string) string {
	if vv, ok := props[key]; ok && len(vv) > 0 {
		return vv[0]
	}
	return ""
}

// parsePerPage bounds per_page: default 10, ceiling 100, garbage or
// non-positive values fall back to the default.
func parsePerPage(s string) int {
	if s == "" {
		return searchDefaultPage
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return searchDefaultPage
	}
	if n > searchMaxPage {
		return searchMaxPage
	}
	return n
}
