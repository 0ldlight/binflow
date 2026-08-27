package conan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The conan management face (spec section 3.3 / ADR-0034's dispatchAPI
// family): POST /binflow/api/conan/reindex (whole repository, repoKey in
// the query or JSON body) and POST /binflow/api/conan/{repoPath}/reindex
// (the path form — the first segments carry the repository key, the rest a
// coordinate-root sub-path).
//
// ADR-0034's mount is two dispatchAPI cases the ASSEMBLY owns (the router
// wire-up note rides this package's ticket report); ManagementHandler is
// the business body those cases call, and it carries its own gate (the
// route family's authentication + CanManageRepo(write) contract) so a bare
// mount cannot bypass it.

// RepoManager is the management-plane gate seam (satisfied by
// auth.Service's ManagementAuthorizer facet).
type RepoManager interface {
	CanManageRepo(ctx context.Context, p *repo.Principal, repoKey string, write bool) bool
}

// ManagementHandler serves the conan reindex family.
type ManagementHandler struct {
	svc   repo.Service
	repos repo.ClassReader
	gate  RepoManager
}

// NewManagementHandler wires the management face. gate may be nil (the
// endpoints then answer 503 rather than serving ungated — fail closed).
func NewManagementHandler(svc repo.Service, repos repo.ClassReader, gate RepoManager) *ManagementHandler {
	return &ManagementHandler{svc: svc, repos: repos, gate: gate}
}

// ServeHTTP dispatches the two reindex spellings. The request carries the
// adapter's principal seam (the router's chain boxes it; a direct mount
// must do the same).
func (m *ManagementHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// /binflow/api/conan/<rest> — the adapter-family writer is not mounted
	// here (this face has no data endpoints), but the pinned capability
	// headers ride it anyway: every conan response carries the family.
	cw := &capWriter{ResponseWriter: w, class: repo.TypeLocal, clientVer: r.Header.Get(hdrClientVer)}
	p := adapter.PrincipalFrom(r.Context())

	if r.Method != http.MethodPost {
		cw.Header().Set("Allow", http.MethodPost)
		writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the conan reindex endpoints")
		return
	}
	rest := strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "binflow/api/conan/")
	switch {
	case rest == "reindex":
		m.serveReindexQuery(cw, r, p)
	case strings.HasSuffix(rest, "/reindex"):
		m.serveReindexPath(cw, r, p, strings.TrimSuffix(rest, "/reindex"))
	default:
		writePlain(cw, http.StatusNotFound, "not found")
	}
}

// serveReindexQuery answers the whole-repository form: repoKey (and an
// optional path) arrive as query parameters or JSON body fields.
func (m *ManagementHandler) serveReindexQuery(cw *capWriter, r *http.Request, p *repo.Principal) {
	q := r.URL.Query()
	req := reindexRequest{RepoKey: q.Get("repoKey"), Path: q.Get("path")}
	if req.RepoKey == "" {
		var body reindexRequest
		if err := readBodyJSON(r, &body, maxURLBody); err != nil {
			writePlain(cw, http.StatusBadRequest, err.Error())
			return
		}
		if body.RepoKey != "" {
			req.RepoKey = body.RepoKey
		}
		if body.Path != "" {
			req.Path = body.Path
		}
	}
	m.runReindex(cw, r, p, req)
}

// reindexRequest is the JSON body shape both forms accept.
type reindexRequest struct {
	RepoKey string `json:"repoKey"`
	Path    string `json:"path"`
}

// serveReindexPath answers the path form: /api/conan/<repoKey>[/<sub>]/reindex.
func (m *ManagementHandler) serveReindexPath(cw *capWriter, r *http.Request, p *repo.Principal, repoPath string) {
	if repoPath == "" {
		writePlain(cw, http.StatusNotFound, "not found")
		return
	}
	key, sub, _ := strings.Cut(repoPath, "/")
	m.runReindex(cw, r, p, reindexRequest{RepoKey: key, Path: sub})
}

// runReindex is the shared body: gate, class check, rebuild, response.
func (m *ManagementHandler) runReindex(cw *capWriter, r *http.Request, p *repo.Principal, req reindexRequest) {
	if p == nil {
		cw.Header().Set("WWW-Authenticate", `Basic realm="BinFlow Realm"`)
		writePlain(cw, http.StatusUnauthorized, "authentication required")
		return
	}
	if req.RepoKey == "" {
		writePlain(cw, http.StatusBadRequest, "repoKey is required")
		return
	}
	if m.gate == nil {
		writePlain(cw, http.StatusServiceUnavailable, "the conan management gate is not configured on this instance")
		return
	}
	if !m.gate.CanManageRepo(r.Context(), p, req.RepoKey, true) {
		writePlain(cw, http.StatusForbidden, "forbidden")
		return
	}
	ctx := r.Context()
	row, err := m.repos.Get(ctx, req.RepoKey)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			writePlain(cw, http.StatusNotFound,
				fmt.Sprintf("Failed to find the repository '%s' specified in the request.", req.RepoKey))
			return
		}
		writePlain(cw, http.StatusInternalServerError, "load repository: "+err.Error())
		return
	}
	if row.Type != repo.TypeLocal {
		// Spec section 3.3: reindex is local-only.
		writePlain(cw, http.StatusBadRequest,
			fmt.Sprintf("Unsupported Conan reindex request for '%s': only local repositories carry a conan index", req.RepoKey))
		return
	}
	count, err := m.rebuild(ctx, p, req.RepoKey, req.Path)
	if err != nil {
		writePlain(cw, http.StatusInternalServerError, err.Error())
		return
	}
	writePlain(cw, http.StatusOK, fmt.Sprintf(
		"Calculated Conan index for repository '%s' (path '%s'): %d revisions reindexed.", req.RepoKey, req.Path, count))
}

// rebuild walks the coordinate trees under sub and regenerates every
// index.json from the stored .timestamp facts (the drift repair reindex
// exists for): for each recipe coordinate, for each revision directory,
// for each packageId, the index rows are rebuilt. Synchronous on purpose —
// BinFlow has no async job plane for conan yet, and a local walk is
// bounded by the repository's own size (the divergence from the spec's
// "async dispatch" is registered in the ticket report).
func (m *ManagementHandler) rebuild(ctx context.Context, p *repo.Principal, repoKey, sub string) (int, error) {
	h := &Handler{svc: m.svc}
	prefix := strings.Trim(sub, "/")
	if prefix != "" {
		prefix += "/"
	}
	nodes, err := m.svc.List(ctx, p, repoKey, prefix)
	if err != nil {
		return 0, fmt.Errorf("walk %s: %w", prefix, err)
	}
	// Collect the revision roots: paths of length 5 under a coordinate
	// (<u>/<n>/<v>/<c>/<rrev>/) and package dirs beneath them.
	revRoots := map[string]bool{}
	pkgDirs := map[string]bool{}
	for _, n := range nodes {
		if !strings.HasSuffix(n.Path, "/") {
			continue // only folder rows address roots
		}
		trimmed := strings.TrimSuffix(n.Path, "/")
		segs := strings.Split(trimmed, "/")
		base := len(segs)
		switch {
		case base == 5:
			revRoots[trimmed] = true
		case base == 7 && segs[5] == dirPackage:
			pkgDirs[trimmed] = true
		}
	}
	count := 0
	for root := range revRoots {
		segs := strings.Split(root, "/")
		rf, err := parseRef(segs[1], segs[2], segs[0], segs[3])
		if err != nil {
			continue
		}
		if err := h.registerRecipeRevision(ctx, p, repoKey, rf, segs[4]); err != nil {
			return 0, fmt.Errorf("reindex %s: %w", root, err)
		}
		count++
	}
	for dir := range pkgDirs {
		segs := strings.Split(dir, "/")
		rf, err := parseRef(segs[1], segs[2], segs[0], segs[3])
		if err != nil {
			continue
		}
		if err := h.rebuildPkgIndex(ctx, p, repoKey, rf, segs[4], segs[6]); err != nil {
			return 0, fmt.Errorf("reindex %s: %w", dir, err)
		}
	}
	return count, nil
}

// rebuildPkgIndex regenerates one packageId's index from the pRev
// directories beneath it (each directory with a .timestamp or any file
// counts as a revision).
func (h *Handler) rebuildPkgIndex(ctx context.Context, p *repo.Principal, repoKey string, rf ref, rrev, pid string) error {
	root := rf.coordinateRoot()
	pd := pkgDir(root, rrev, pid) + "/"
	nodes, err := h.svc.List(ctx, p, repoKey, pd)
	if err != nil {
		return fmt.Errorf("walk %s: %w", pd, err)
	}
	prevs := map[string]bool{}
	for _, n := range nodes {
		rest := strings.TrimPrefix(n.Path, pd)
		prev, more, found := strings.Cut(rest, "/")
		if !found || !validRevision(prev) {
			continue
		}
		if more == "" && !strings.HasSuffix(n.Path, "/") {
			continue // a stray file directly under the pid dir is not a revision
		}
		prevs[prev] = true
	}
	names := make([]string, 0, len(prevs))
	for prev := range prevs {
		names = append(names, prev)
	}
	sort.Strings(names)
	for _, prev := range names {
		if err := h.registerPkgRevision(ctx, p, repoKey, rf, rrev, pid, prev); err != nil {
			return err
		}
	}
	return nil
}
