package conan

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

	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual-repository face (spec section 7's virtual row, S11's merge
// rules). File bodies need nothing here — svc.Get on a virtual repository
// already walks the two-bucket member order first-found (local members'
// nodes, remote members through the FR-20 chain, X-BinFlow-Resolved-From
// riding the reader). The aggregations live in this file:
//
//   - revisions/latest: the members' index documents merged — revisions
//     deduplicated by their string with the FIRST-SEEN member's row kept,
//     then ordered by the index's own rule (time descending, ties by
//     revision descending); latest is the merged head (spec: "revisions
//     清单跨成员按 time 归并去重（同名修订取首见成员）；latest 取归并后首项");
//   - files listings: the UNION of every member's file-name set (spec:
//     "files 清单跨成员并集") — a local member contributes its node facts,
//     a remote member its marker-cached upstream listing;
//   - the packageId search: the pid-keyed union of every member's rows
//     (first-seen member wins a pid);
//   - search: the local members' stored facts — a remote member's whole
//     catalogue is not enumerable through the member seam (the T-287
//     constraint, nuget's precedent posture);
//   - writes: PUT rides the shared arm — the service routes a configured
//     defaultDeploymentRepo onto the member and an un-routed virtual
//     answers the C5 405 there; the registration tail's index read is
//     member-targeted (below) so a routed write never copies the other
//     members' revisions into the target. DELETE refuses in-handler — the
//     local arms open with a subtree probe and svc.List does not serve
//     the virtual class.
//
// Member failures follow the npm/pypi aggregation rule: an unfound member
// contributes nothing; a CLASSIFIED failure (the remote engine's SSRF 400,
// hardFail 502) is skipped so one bad member cannot block the read — but
// an aggregation where NOTHING was collected surfaces the remembered
// failure instead of masking it as a plain 404.

// serveVirtual dispatches the v2 family on a virtual repository. The class
// door has already refused the v1 data plane with S6's 400.
func (h *Handler) serveVirtual(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	switch rt.kind {
	// ---- the class-independent handshake (both version prefixes) ----
	case kindV1Ping:
		h.servePing(cw)
	case kindV1Authenticate:
		h.serveAuthenticate(ctx, cw, p)
	case kindV1CheckCredentials:
		h.serveCheckCredentials(cw, p)

	// ---- search: the local members' stored facts ----
	case kindV2Search:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveVirtualSearch(ctx, cw, r, repoKey)

	// ---- the revision chain, off the merged index documents ----
	case kindV2Latest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, err := h.mergedRecipeIndex(ctx, repoKey, rt.ref)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		latest, ok := latestOf(doc.Revisions)
		if !ok {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		writeJSONDoc(cw, latest)
	case kindV2Revisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, err := h.mergedRecipeIndex(ctx, repoKey, rt.ref)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		if len(doc.Revisions) == 0 {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		if doc.Reference == "" {
			doc.Reference = rt.ref.wireRef()
		}
		writeJSONDoc(cw, doc)

	// ---- the packageId metadata union ----
	case kindV2RefSearch, kindV2RevSearch:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		rrev := rt.rRev
		if rrev == "" {
			rev, _, err := h.resolveRRevVirtual(ctx, repoKey, rt.ref)
			if err != nil {
				h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
				return
			}
			rrev = rev
		}
		h.serveVirtualRefSearch(ctx, cw, repoKey, rt.ref, rrev)

	// ---- the files family: the union listing / first-found body ----
	case kindV2Files:
		root := rt.ref.coordinateRoot()
		h.serveVirtualFiles(ctx, cw, r, p, repoKey, rt,
			recipeExportPrefix(root, rt.rRev), recipeFilesMarker(root, rt.rRev),
			func(path string) string { return recipeFile(root, rt.rRev, path) })
	case kindV2PkgFiles:
		root := rt.ref.coordinateRoot()
		h.serveVirtualFiles(ctx, cw, r, p, repoKey, rt,
			pkgFilePrefix(root, rt.rRev, rt.pid, rt.pRev), pkgFilesMarker(root, rt.rRev, rt.pid, rt.pRev),
			func(path string) string { return pkgFile(root, rt.rRev, rt.pid, rt.pRev, path) })

	// ---- the package revision chain, off the merged pkg indexes ----
	case kindV2PkgLatest:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, err := h.mergedPkgIndex(ctx, repoKey, rt.ref, rt.rRev, rt.pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		latest, ok := latestOf(doc.Revisions)
		if !ok {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		writeJSONDoc(cw, latest)
	case kindV2PkgRevisions:
		if !h.requireMethod(cw, r, http.MethodGet, http.MethodHead) {
			return
		}
		doc, err := h.mergedPkgIndex(ctx, repoKey, rt.ref, rt.rRev, rt.pid)
		if err != nil {
			h.writeError(cw, err, repoKey, rt.ref.coordinateRoot())
			return
		}
		if len(doc.Revisions) == 0 {
			writePlain(cw, http.StatusNotFound, msgNoRevisions)
			return
		}
		if doc.Reference == "" {
			doc.Reference = rt.ref.wireRef() + "#" + rt.rRev + ":" + rt.pid
		}
		writeJSONDoc(cw, doc)

	// ---- the write family ----
	case kindV2RecipeDelete, kindV2RevDelete, kindV2PackagesDelete, kindV2PkgRevDelete:
		if !h.requireMethod(cw, r, http.MethodDelete) {
			return
		}
		h.refuseVirtualDelete(cw, repoKey)

	default:
		writePlain(cw, http.StatusNotFound, "not found")
	}
}

// refuseVirtualDelete answers the DELETE family on a virtual repository:
// deletes never propagate through the member resolution (RE-08 / T-71),
// with the wording keyed on whether a write route exists — the repo
// package's own refusal, restated because the local arms' subtree probe
// (a svc.List) does not serve the virtual class.
func (h *Handler) refuseVirtualDelete(cw *capWriter, repoKey string) {
	cw.Header().Set("Allow", http.MethodGet)
	routed := false
	if h.repos != nil {
		if row, err := h.repos.Get(context.Background(), repoKey); err == nil {
			routed = conanDeploymentTarget(row.Config) != ""
		}
	}
	msg := fmt.Sprintf(
		"No local repository was configured as local deployment repository for the (%s) virtual repository.", repoKey)
	if routed {
		msg = fmt.Sprintf(
			"Deletes are not propagated through the virtual repository '%s'; delete the artifact in its member repository directly.", repoKey)
	}
	writePlain(cw, http.StatusMethodNotAllowed, msg)
}

// conanDeploymentTarget is the tolerant write-route probe of a virtual
// repository's config JSON (the npm-side restatement of repo's own reader —
// adapter packages share no unexported code, the area rule): the primary
// spelling plus the two Artifactory aliases raw-seeded rows may carry.
func conanDeploymentTarget(config string) string {
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

// ---- member document reads (the T-72 aggregation seam) ----

// memberDoc reads one member's copy of a document path through the ungated
// aggregation seam (the caller's virtual read gate already ran). The
// unfound family maps to (nil, nil); a classified failure keeps its
// rendering for the caller's tolerate-or-surface decision.
func (h *Handler) memberDoc(ctx context.Context, virtualKey, member, path string) ([]byte, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	if rerr != nil {
		return nil, fmt.Errorf("read member document %s/%s: %w", member, path, rerr)
	}
	return raw, nil
}

// aggregationFailure remembers the first classified member failure of one
// collection (the npm/pypi rule: tolerated, but never masked when nothing
// else answered).
type aggregationFailure struct{ err *repo.StatusError }

// note keeps the FIRST classified failure.
func (a *aggregationFailure) note(err error) bool {
	var se *repo.StatusError
	if !errors.As(err, &se) {
		return false // an internal fault is the caller's honest 500
	}
	if a.err == nil {
		a.err = se
	}
	return true
}

// tolerateMemberFailure logs one member's aggregation failure and moves on.
func tolerateMemberFailure(ctx context.Context, virtualKey, member, what string, err error) {
	slog.WarnContext(ctx, "conan: virtual member failed during aggregation (tolerated)",
		"virtual", virtualKey, "member", member, "what", what, "error", err.Error())
}

// mergedRecipeIndex walks the member order and merges every member's
// recipe index document (S11: time-ordered union, first-seen row per
// revision string).
func (h *Handler) mergedRecipeIndex(ctx context.Context, virtualKey string, rf ref) (*recipeIndexDoc, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	root := rf.coordinateRoot()
	merged := &recipeIndexDoc{}
	seen := map[string]bool{}
	var failure aggregationFailure
	for _, m := range order {
		raw, derr := h.memberDoc(ctx, virtualKey, m.Key, recipeIndex(root))
		if derr != nil {
			if failure.note(derr) {
				tolerateMemberFailure(ctx, virtualKey, m.Key, "recipe index merge", derr)
				continue
			}
			return nil, derr
		}
		if raw == nil {
			continue // the member does not carry the coordinate
		}
		var doc recipeIndexDoc
		if jerr := json.Unmarshal(raw, &doc); jerr != nil {
			continue // an unparseable member document contributes nothing
		}
		for _, e := range doc.Revisions {
			if seen[e.Revision] {
				continue
			}
			seen[e.Revision] = true
			merged.Revisions = append(merged.Revisions, e)
		}
	}
	if len(merged.Revisions) == 0 && failure.err != nil {
		return nil, failure.err
	}
	merged.Reference = rf.wireRef()
	sortRevisions(merged.Revisions)
	return merged, nil
}

// mergedPkgIndex merges the packageId's index documents the same way.
func (h *Handler) mergedPkgIndex(ctx context.Context, virtualKey string, rf ref, rrev, pid string) (*pkgIndexDoc, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	path := pkgIndex(rf.coordinateRoot(), rrev, pid)
	merged := &pkgIndexDoc{}
	seen := map[string]bool{}
	var failure aggregationFailure
	for _, m := range order {
		raw, derr := h.memberDoc(ctx, virtualKey, m.Key, path)
		if derr != nil {
			if failure.note(derr) {
				tolerateMemberFailure(ctx, virtualKey, m.Key, "package index merge", derr)
				continue
			}
			return nil, derr
		}
		if raw == nil {
			continue
		}
		var doc pkgIndexDoc
		if jerr := json.Unmarshal(raw, &doc); jerr != nil {
			continue
		}
		for _, e := range doc.Revisions {
			if seen[e.Revision] {
				continue
			}
			seen[e.Revision] = true
			merged.Revisions = append(merged.Revisions, e)
		}
	}
	if len(merged.Revisions) == 0 && failure.err != nil {
		return nil, failure.err
	}
	merged.Reference = rf.wireRef() + "#" + rrev + ":" + pid
	sortRevisions(merged.Revisions)
	return merged, nil
}

// resolveRRevVirtual is the virtual class's implicit-latest resolver (the
// merged chain's head).
func (h *Handler) resolveRRevVirtual(ctx context.Context, virtualKey string, rf ref) (string, revEntry, error) {
	doc, err := h.mergedRecipeIndex(ctx, virtualKey, rf)
	if err != nil {
		return "", revEntry{}, err
	}
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		return "", revEntry{}, fmt.Errorf("%w: no revision of %s", repo.ErrNodeNotFound, rf.wireRef())
	}
	return latest.Revision, latest, nil
}

// memberFileNames computes one member's file-name set under prefix: a
// local member's node facts, a remote member's marker-cached listing
// document.
func (h *Handler) memberFileNames(ctx context.Context, virtualKey string, m repo.VirtualMember, prefix, marker string) (map[string]bool, error) {
	if m.Type == repo.TypeLocal {
		nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, prefix)
		if err != nil {
			return nil, err
		}
		names := map[string]bool{}
		for _, n := range nodes {
			name, ok := listedFileName(n.Path, prefix)
			if ok {
				names[name] = true
			}
		}
		return names, nil
	}
	raw, err := h.memberDoc(ctx, virtualKey, m.Key, marker)
	if err != nil || raw == nil {
		return nil, err
	}
	var doc filesResponse
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return map[string]bool{}, nil // an unparseable listing contributes nothing
	}
	names := make(map[string]bool, len(doc.Files))
	for name := range doc.Files {
		names[name] = true
	}
	return names, nil
}

// listedFileName recognizes one listable file row under prefix (the
// local listFiles rule: folders and .timestamp markers never surface).
func listedFileName(path, prefix string) (string, bool) {
	if len(path) > 0 && path[len(path)-1] == '/' {
		return "", false
	}
	name := ""
	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return "", false
		}
		name = path[len(prefix):]
	} else {
		name = path
	}
	if name == "" || name == timestampFile || strings.HasSuffix(name, "/"+timestampFile) {
		return "", false
	}
	return name, true
}

// serveVirtualFiles serves the v2 files family on a virtual repository:
// the listing is the members' UNION (empty → the pinned 404), the file
// bodies ride svc.Get's first-found member resolution, and a PUT rides
// the shared arm (the service's write route).
func (h *Handler) serveVirtualFiles(ctx context.Context, cw *capWriter, r *http.Request, p *repo.Principal,
	repoKey string, rt route, prefix, marker string, nodePathOf func(path string) string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if rt.path == "" {
			h.serveVirtualFileList(ctx, cw, repoKey, prefix, marker)
			return
		}
		path := nodePathOf(rt.path)
		rc, node, err := h.svc.Get(ctx, p, repoKey, path)
		if err != nil {
			h.writeError(cw, err, repoKey, path)
			return
		}
		h.serveNode(ctx, cw, r, node, rc, "application/octet-stream")
	case http.MethodPut:
		path := nodePathOf(rt.path)
		if rt.pid == "" {
			h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, "", "", path)
			return
		}
		h.serveFilePut(ctx, cw, r, p, repoKey, rt.ref, rt.rRev, rt.pid, rt.pRev, path)
	default:
		cw.Header().Set("Allow", "GET, HEAD, PUT")
		writePlain(cw, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the files target")
	}
}

// serveVirtualFileList renders the members' union file listing.
func (h *Handler) serveVirtualFileList(ctx context.Context, cw *capWriter, virtualKey, prefix, marker string) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(cw, err, virtualKey, prefix)
		return
	}
	union := map[string]bool{}
	var failure aggregationFailure
	for _, m := range order {
		names, merr := h.memberFileNames(ctx, virtualKey, m, prefix, marker)
		if merr != nil {
			if failure.note(merr) {
				tolerateMemberFailure(ctx, virtualKey, m.Key, "file listing union", merr)
				continue
			}
			h.writeError(cw, merr, virtualKey, prefix)
			return
		}
		for name := range names {
			union[name] = true
		}
	}
	if len(union) == 0 && failure.err != nil {
		h.writeError(cw, failure.err, virtualKey, prefix)
		return
	}
	if len(union) == 0 {
		writePlain(cw, http.StatusNotFound, msgPathNotFound)
		return
	}
	set := make(map[string]struct{}, len(union))
	for name := range union {
		set[name] = struct{}{}
	}
	writeJSONDoc(cw, filesResponse{Files: set})
}

// serveVirtualSearch renders the local members' stored-facts search (the
// T-287 posture: a remote member's uncached catalogue is not listable
// through the member seam).
func (h *Handler) serveVirtualSearch(ctx context.Context, cw *capWriter, r *http.Request, virtualKey string) {
	pattern := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	pattern = strings.TrimSuffix(pattern, "/*")
	re, err := globRegex(pattern)
	if err != nil {
		writePlain(cw, http.StatusBadRequest, "invalid search pattern: "+err.Error())
		return
	}
	order, oerr := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if oerr != nil {
		h.writeError(cw, oerr, virtualKey, "")
		return
	}
	refs := map[string]bool{}
	for _, m := range order {
		if m.Type != repo.TypeLocal {
			continue // the member seam refuses remote catalogue listings
		}
		nodes, lerr := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, "")
		if lerr != nil {
			tolerateMemberFailure(ctx, virtualKey, m.Key, "search union", lerr)
			continue
		}
		for _, n := range nodes {
			if rf, ok := coordinateOfIndexNode(n.Path); ok {
				refs[rf.wireRef()] = true
			}
		}
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

// serveVirtualRefSearch renders the pid-keyed union of every member's
// packageId rows (first-seen member wins a pid): a local member's rows
// derive from its facts, a remote member's arrive as the marker-cached
// upstream document.
func (h *Handler) serveVirtualRefSearch(ctx context.Context, cw *capWriter, virtualKey string, rf ref, rrev string) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(cw, err, virtualKey, rf.coordinateRoot())
		return
	}
	out := map[string]*pkgMeta{}
	var failure aggregationFailure
	for _, m := range order {
		rows, merr := h.memberPkgRows(ctx, virtualKey, m, rf, rrev)
		if merr != nil {
			if failure.note(merr) {
				tolerateMemberFailure(ctx, virtualKey, m.Key, "package metadata union", merr)
				continue
			}
			h.writeError(cw, merr, virtualKey, rf.coordinateRoot())
			return
		}
		for pid, meta := range rows {
			if _, ok := out[pid]; !ok {
				out[pid] = meta
			}
		}
	}
	if len(out) == 0 && failure.err != nil {
		h.writeError(cw, failure.err, virtualKey, rf.coordinateRoot())
		return
	}
	writeJSONDoc(cw, out)
}

// memberPkgRows computes one member's packageId rows.
func (h *Handler) memberPkgRows(ctx context.Context, virtualKey string, m repo.VirtualMember, rf ref, rrev string) (map[string]*pkgMeta, error) {
	if m.Type == repo.TypeRemote {
		raw, err := h.memberDoc(ctx, virtualKey, m.Key, refSearchMarker(rf.coordinateRoot(), rrev))
		if err != nil || raw == nil {
			return map[string]*pkgMeta{}, err
		}
		var doc map[string]*pkgMeta
		if jerr := json.Unmarshal(raw, &doc); jerr != nil {
			return map[string]*pkgMeta{}, nil
		}
		if doc == nil {
			doc = map[string]*pkgMeta{}
		}
		return doc, nil
	}
	// A local member: derive from its facts (the local arm's own assembly,
	// member-seam reads throughout).
	pkgRoot := rf.coordinateRoot() + "/" + rrev + "/" + dirPackage + "/"
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, pkgRoot)
	if err != nil {
		return nil, err
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
		out[pid] = h.memberPackageMeta(ctx, virtualKey, m.Key, rf, rrev, pid)
	}
	return out, nil
}

// memberPackageMeta assembles one member-local packageId's row: the newest
// pRev's conaninfo.txt through the member seam (the local packageMeta's
// rule, ungated member reads throughout).
func (h *Handler) memberPackageMeta(ctx context.Context, virtualKey, member string, rf ref, rrev, pid string) *pkgMeta {
	meta := &pkgMeta{
		Settings:   map[string]string{},
		Options:    map[string]string{},
		Requires:   map[string]string{},
		RecipeHash: rrev,
	}
	prev, ok := h.memberLatestPRev(ctx, virtualKey, member, rf, rrev, pid)
	if !ok {
		return meta
	}
	raw, err := h.memberDoc(ctx, virtualKey, member, pkgFile(rf.coordinateRoot(), rrev, pid, prev, "conaninfo.txt"))
	if err != nil || raw == nil {
		return meta
	}
	parseConaninfo(string(raw), meta)
	return meta
}

// memberLatestPRev resolves the member's newest package revision through
// its own index document.
func (h *Handler) memberLatestPRev(ctx context.Context, virtualKey, member string, rf ref, rrev, pid string) (string, bool) {
	raw, err := h.memberDoc(ctx, virtualKey, member, pkgIndex(rf.coordinateRoot(), rrev, pid))
	if err != nil || raw == nil {
		return "", false
	}
	var doc pkgIndexDoc
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return "", false
	}
	sortRevisions(doc.Revisions)
	latest, ok := latestOf(doc.Revisions)
	if !ok {
		return "", false
	}
	return latest.Revision, true
}
