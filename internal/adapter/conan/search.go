package conan

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The search faces (spec section 3.1's last three rows + section 3.2's
// first two): the repository-wide pattern query and the per-ref packageId
// metadata assembly.

// searchResponse is the pattern query's body (both planes, one shape).
type searchResponse struct {
	Results []string `json:"results"`
}

// pkgMeta is one packageId's metadata row — the four fields the conan
// client's package listings consume.
type pkgMeta struct {
	Settings   map[string]string `json:"settings"`
	Options    map[string]string `json:"options"`
	Requires   map[string]string `json:"requires"`
	RecipeHash string            `json:"recipe_hash"`
}

// serveSearch answers GET v{1,2}/conans/search?q=<pattern>: every
// coordinate whose wire ref matches. `*` is the wildcard; the match is
// case-insensitive over name/version@user/channel (the conan search
// posture — clients type lowercase patterns against mixed-case refs); an
// empty pattern matches everything. A trailing `/*` is the client's
// REVISION wildcard (conan list "ref/*"): the ref matches with the
// suffix stripped — the L-c3 real-client evidence.
func (h *Handler) serveSearch(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string) {
	pattern := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	pattern = strings.TrimSuffix(pattern, "/*")
	re, err := globRegex(pattern)
	if err != nil {
		writePlain(cw, http.StatusBadRequest, "invalid search pattern: "+err.Error())
		return
	}
	nodes, err := h.svc.List(ctx, p, repoKey, "")
	if err != nil {
		h.writeError(cw, err, repoKey, "")
		return
	}
	refs := map[string]bool{}
	for _, n := range nodes {
		rf, ok := coordinateOfIndexNode(n.Path)
		if !ok {
			continue
		}
		refs[rf.wireRef()] = true
	}
	out := make([]string, 0, len(refs))
	for ref := range refs {
		if re.MatchString(ref) {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	writeJSONDoc(cw, searchResponse{Results: out})
}

// globRegex compiles one `*`-wildcard pattern into an anchored,
// case-insensitive matcher (the conan search grammar: `*` spans anything,
// every other literal matches itself).
func globRegex(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("(?is)^")
	for _, chunk := range strings.Split(pattern, "*") {
		b.WriteString(regexp.QuoteMeta(chunk))
		b.WriteString(".*")
	}
	// The trailing .* of a pattern without a closing * would loosen the
	// anchor; rebuild without it when the pattern does not end in *.
	body := b.String()
	if !strings.HasSuffix(pattern, "*") {
		body = strings.TrimSuffix(body, ".*")
	}
	return regexp.Compile(body)
}

// coordinateOfIndexNode recognizes one recipe index node's path
// (<user>/<name>/<version>/<channel>/index.json) and returns the ref.
func coordinateOfIndexNode(path string) (ref, bool) {
	if !strings.HasSuffix(path, "/"+recipeIndexFile) {
		return ref{}, false
	}
	root := strings.TrimSuffix(path, "/"+recipeIndexFile)
	segs := strings.Split(root, "/")
	if len(segs) != 4 {
		return ref{}, false
	}
	rf, err := parseRef(segs[1], segs[2], segs[0], segs[3]) // storage order: user, name, version, channel
	if err != nil {
		return ref{}, false
	}
	return rf, true
}

// serveRefSearch answers GET <ref>/search and <ref>/revisions/{rRev}/search
// (plus the v1 conans/<ref>/search twin): every packageId under the
// revision, assembled from the latest pRev's conaninfo.txt (settings/
// options/requires) with recipe_hash = the recipe revision (hash mode's
// own definition of the value; spec section 4's property family, medium
// confidence, fixed in tests).
func (h *Handler) serveRefSearch(ctx context.Context, cw *capWriter, p *repo.Principal, repoKey string, rf ref, rrev string) {
	rev, _, err := h.resolveRRev(ctx, p, repoKey, rf, rrev)
	if err != nil {
		h.writeError(cw, err, repoKey, rf.coordinateRoot())
		return
	}
	pkgRoot := rf.coordinateRoot() + "/" + rev + "/" + dirPackage + "/"
	nodes, err := h.svc.List(ctx, p, repoKey, pkgRoot)
	if err != nil {
		h.writeError(cw, err, repoKey, pkgRoot)
		return
	}
	pids := map[string]bool{}
	for _, n := range nodes {
		rest := strings.TrimPrefix(n.Path, pkgRoot)
		pid, more, found := strings.Cut(rest, "/")
		if !found || more == "" || !validPackageID(pid) {
			continue
		}
		pids[pid] = true
	}
	out := map[string]*pkgMeta{}
	for pid := range pids {
		out[pid] = h.packageMeta(ctx, p, repoKey, rf, rev, pid)
	}
	writeJSONDoc(cw, out)
}

// packageMeta assembles one packageId's row: the newest pRev's
// conaninfo.txt parsed into the three maps (empty maps when the file is
// absent or unparsable — the pid still lists), recipe_hash = the recipe
// revision (hash mode's own definition of the value).
func (h *Handler) packageMeta(ctx context.Context, p *repo.Principal, repoKey string, rf ref, rrev, pid string) *pkgMeta {
	meta := &pkgMeta{
		Settings:   map[string]string{},
		Options:    map[string]string{},
		Requires:   map[string]string{},
		RecipeHash: rrev,
	}
	prev, ok, err := h.latestPRev(ctx, p, repoKey, rf, rrev, pid)
	if err != nil || !ok {
		return meta
	}
	rc, _, err := h.svc.Get(ctx, p, repoKey, pkgFile(rf.coordinateRoot(), rrev, pid, prev, "conaninfo.txt"))
	if err != nil {
		return meta
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw := make([]byte, 512<<10)
	n, rerr := rc.Read(raw)
	if rerr != nil && n == 0 {
		return meta
	}
	parseConaninfo(string(raw[:n]), meta)
	return meta
}

// parseConaninfo fills meta's three maps from a conaninfo.txt body: the
// [settings]/[options]/[requires] sections' key=value (requires entries
// are bare lines — dependency refs — and land as keys with empty values).
// Nil maps are initialized (the caller's empty-row shape).
func parseConaninfo(body string, meta *pkgMeta) {
	if meta.Settings == nil {
		meta.Settings = map[string]string{}
	}
	if meta.Options == nil {
		meta.Options = map[string]string{}
	}
	if meta.Requires == nil {
		meta.Requires = map[string]string{}
	}
	section := ""
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		switch section {
		case "settings", "options":
			k, v, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if section == "settings" {
				meta.Settings[k] = strings.TrimSpace(v)
			} else {
				meta.Options[k] = strings.TrimSpace(v)
			}
		case "requires":
			meta.Requires[strings.TrimSpace(line)] = ""
		}
	}
}
