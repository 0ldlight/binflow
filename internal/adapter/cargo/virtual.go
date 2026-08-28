package cargo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual-repository face (spec section 8's virtual row, T-318): the
// aggregate cargo registry over a member list of local and remote
// repositories, resolved in the service's two-bucket order.
//
//	GET  index/config.json         synthesized self-pointing at the VIRTUAL
//	                               repository (dl/api cite the aggregate, so
//	                               every download crosses the first-hit
//	                               member resolution — the remote face's
//	                               posture, one plane up)
//	GET  index/{pkgPath}           THE MERGE: every member's own index file,
//	                               read through the ungated member seam in
//	                               member order, deduplicated on
//	                               (name, vers ignoring build metadata) with
//	                               the FIRST-SEEN member's raw row kept
//	                               (spec section 12's ruling ① — the BinFlow
//	                               recommendation, resolving the raw
//	                               concatenation the reference performs: a
//	                               duplicate row would violate the official
//	                               uniqueness MUST, and the first-seen row is
//	                               the copy first-hit download serves anyway),
//	                               then ordered by SemVer like the local face
//	                               writes them
//	GET  v1/crates/{n}/{v}/download  svc.Get's first-hit member resolution
//	                               (local nodes, remote members through the
//	                               FR-20 chain, X-BinFlow-Resolved-From riding
//	                               the reader)
//	GET  api/v1/crates?q=…          the members' search rows merged: local
//	                               members from their stored facts (the local
//	                               face's own rule), remote members through
//	                               their query-keyed search markers (the
//	                               T-316 cache shape IS a pull-through fetch
//	                               of that member's upstream search), names
//	                               unioned first-seen
//	PUT  api/v1/crates/new          routed onto the configured
//	                               defaultDeploymentRepo — the whole ordinary
//	                               local publish chain runs against the MEMBER
//	                               (the npm loadPackumentForWrite posture:
//	                               the post-landing sidecar and index rewrite
//	                               must target the member, never the virtual);
//	                               un-routed answers RE-08's C5 405 before
//	                               the body drains
//	yank/unyank                     first-hit: the member that would serve
//	                               the download takes the flag (a local
//	                               holder gets the property plus its own
//	                               index rewrite); a REMOTE holder refuses —
//	                               a cached upstream copy is not BinFlow's
//	                               to flag, the upstream owns the truth
//	bare content                    GET first-hit; PUT rides the write route
//	                               (landing in the member, convergence there);
//	                               DELETE refuses — deletes never propagate
//	                               through a virtual (RE-08)
//
// Member failures follow the npm/pypi/conan aggregation rule: an unfound
// member contributes nothing; a CLASSIFIED failure (the remote engine's
// SSRF 400, hardFail 502) is tolerated so one bad member cannot block the
// read — but an aggregation where NOTHING was collected surfaces the
// remembered failure instead of masking it as a plain 404.

// maxIndexFileBytes bounds one member index read (real index files run to a
// few hundred KB; beyond this the member is treated as unreadable — the npm
// packumentReadLimit tolerance).
const maxIndexFileBytes = 16 << 20

// serveVirtual dispatches one request on a virtual repository. The class
// door has already run; kindRoot and kindGitFace never reach here (the
// class-independent arms in ServeHTTP answer them), but both keep their
// arms for the same defensive parity remote.go carries.
func (h *Handler) serveVirtual(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey string, rt route) {
	switch rt.kind {
	case kindRoot:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusOK)
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the repository root")
		}
	case kindGitFace:
		writeEnvelope(w, http.StatusNotFound, msgGitDeprecated)

	case kindConfig:
		// Synthesized per request, never a storage node — identical arms to
		// the local and remote faces; only the cited repository differs (the
		// virtual: downloads must cross the aggregate).
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveConfig(w, r, repoKey)
		case http.MethodPut, http.MethodDelete, http.MethodPost:
			writeEnvelope(w, http.StatusForbidden,
				"'index/config.json' is server-generated (the synthesized sparse entry document); direct writes are not permitted")
		default:
			w.Header().Set("Allow", "GET, HEAD")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on config.json")
		}

	case kindIndexFile:
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			h.serveVirtualIndexFile(ctx, w, r, repoKey, rt)
		case http.MethodPut:
			h.serveVirtualDerivedWrite(ctx, w, r, p, repoKey, segIndex+"/"+rt.pkgPath)
		case http.MethodDelete:
			// Deletes never propagate through a virtual repository: the
			// service's own refusal (the routed/un-routed wording pair) is
			// the honest answer, rendered verbatim.
			if err := h.svc.Delete(ctx, p, repoKey, segIndex+"/"+rt.pkgPath); err != nil {
				h.writeError(w, err, repoKey, segIndex+"/"+rt.pkgPath)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on index files")
		}

	case kindDownload:
		// svc.Get on a virtual repository IS the first-hit member walk; the
		// local arm serves it verbatim and serveNode merges the reader's
		// X-BinFlow-Resolved-From (plus a remote winner's cache hints).
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveDownload(ctx, w, r, p, repoKey, rt)

	case kindSearch:
		if !h.requireMethod(w, r, http.MethodGet, http.MethodHead) {
			return
		}
		h.serveVirtualSearch(ctx, w, r, repoKey)

	case kindPublish:
		if r.Method != http.MethodPut {
			w.Header().Set("Allow", http.MethodPut)
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the publish target")
			return
		}
		target := h.virtualWriteTarget(ctx, repoKey)
		if target == "" {
			// Un-routed: RE-08's C5 refusal, restated so it answers BEFORE
			// the publish frame drains (the npm/conan write-face posture;
			// the wording is repo-semantics section 8.2's pinned spelling).
			w.Header().Set("Allow", http.MethodGet)
			writeEnvelope(w, http.StatusMethodNotAllowed, msgNoDeploymentRepo(repoKey))
			return
		}
		// Routed: the ordinary local publish chain against the MEMBER — the
		// crate, the sidecar and the index rewrite all land there (a
		// rewrite addressed at the virtual key would die at svc.List's
		// aggregate refusal and leave the member's index stale).
		h.servePublish(ctx, w, r, p, target)

	case kindYank, kindUnyank:
		want, flag := http.MethodDelete, true
		if rt.kind == kindUnyank {
			want, flag = http.MethodPut, false
		}
		if r.Method != want {
			w.Header().Set("Allow", want)
			writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on the "+strings.TrimPrefix(rt.path, "api/v1/crates/")+" target")
			return
		}
		h.serveVirtualYank(ctx, w, p, repoKey, rt.name, rt.version, flag)

	case kindBareContent:
		h.serveVirtualBareContent(ctx, w, r, p, repoKey, rt.path)

	default:
		writeEnvelope(w, http.StatusNotFound, "not found")
	}
}

// ---- the write route ----

// virtualWriteTarget resolves the write route of one virtual repository:
// its configured defaultDeploymentRepo, "" when un-routed. The probe is the
// repo package's own reader restated locally (adapter packages share no
// unexported code, the area rule; the npm/conan twin).
func (h *Handler) virtualWriteTarget(ctx context.Context, virtualKey string) string {
	if h.repos == nil {
		return ""
	}
	row, err := h.repos.Get(ctx, virtualKey)
	if err != nil || row.Type != repo.TypeVirtual {
		return ""
	}
	return cargoDeploymentTarget(row.Config)
}

// cargoDeploymentTarget is the tolerant write-route probe of a virtual
// repository's config JSON: the primary spelling plus the two Artifactory
// aliases raw-seeded rows may carry. A config that fails the strict shape
// still gets its truthful answer: no route.
func cargoDeploymentTarget(config string) string {
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

// msgNoDeploymentRepo is RE-08's pinned C5 body (repo-semantics section
// 8.2), restated so the virtual write refusals ride the cargo errors
// envelope without an import — the conan twin; M52's equality assertion
// compares this spelling.
func msgNoDeploymentRepo(virtualKey string) string {
	return fmt.Sprintf(
		"No local repository was configured as local deployment repository for the (%s) virtual repository.", virtualKey)
}

// serveVirtualDerivedWrite lands one bare PUT on a virtual repository: a
// ROUTED write swaps its addressing onto the deployment member (svc.Put
// would route it too, but the post-landing index convergence must run
// against the member — the virtual key cannot List); an un-routed write
// falls through to the shared arm, where the service's own C5 405 answers.
func (h *Handler) serveVirtualDerivedWrite(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, virtualKey, path string) {
	if target := h.virtualWriteTarget(ctx, virtualKey); target != "" {
		h.serveDerivedWrite(ctx, w, r, p, target, path)
		return
	}
	h.serveDerivedWrite(ctx, w, r, p, virtualKey, path)
}

// serveVirtualBareContent is the raw storage face on a virtual repository:
// reads ride svc.Get's first-hit member resolution (hints merged
// structurally by serveNode), PUT rides the write route with the
// convergence landing in the member, DELETE is the service's never-propagate
// refusal.
func (h *Handler) serveVirtualBareContent(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
		if err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		ctype := node.Mime
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		h.serveNode(ctx, w, r, node, rc, ctype)
	case http.MethodPut:
		h.serveVirtualDerivedWrite(ctx, w, r, p, repoKey, rel)
	case http.MethodDelete:
		if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
			h.writeError(w, err, repoKey, rel)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, DELETE")
		writeEnvelope(w, http.StatusMethodNotAllowed, "method "+r.Method+" is not supported on content paths")
	}
}

// ---- the index merge (spec section 8's virtual row / ruling ①) ----

// parsedIndexRow is one candidate row of a member's index file: the RAW
// line (served verbatim — the merge filters and orders, it never re-renders
// a member's bytes, so an upstream row's own field set survives) plus the
// parsed order/dedup keys.
type parsedIndexRow struct {
	name string
	vers string
	raw  []byte
}

// parseIndexRows splits one member's NDJSON into candidate rows. A
// malformed line contributes nothing (the merged file must never carry a
// row the merge cannot order or dedup); a trailing newline is not a row.
func parseIndexRows(raw []byte) []parsedIndexRow {
	var out []parsedIndexRow
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Name string `json:"name"`
			Vers string `json:"vers"`
		}
		if err := json.Unmarshal(line, &probe); err != nil || probe.Name == "" || probe.Vers == "" {
			continue
		}
		out = append(out, parsedIndexRow{name: strings.ToLower(probe.Name), vers: probe.Vers, raw: line})
	}
	return out
}

// memberIndexBytes reads one member's copy of an index file through the
// ungated aggregation seam (the conan memberDoc posture): unfound maps to
// (nil, "", nil, nil); a classified failure keeps its rendering for the
// caller's tolerate-or-surface decision. The returns are the body, the
// contributing node's UpdatedAt (the merged Last-Modified's newest input)
// and the reader's response hints (a remote member's X-BinFlow-Cache
// family — the BASE member's copy rides the merged response, the npm
// merged-packument posture).
func (h *Handler) memberIndexBytes(ctx context.Context, virtualKey, member, path string) ([]byte, string, http.Header, error) {
	rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return nil, "", nil, nil
		}
		return nil, "", nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	var hints http.Header
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		hints = extra.ExtraHeaders()
	}
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexFileBytes))
	if rerr != nil {
		return nil, "", nil, fmt.Errorf("read member index %s/%s: %w", member, path, rerr)
	}
	return raw, node.UpdatedAt, hints, nil
}

// tolerateVirtualMember logs one member's aggregation failure and moves on.
func tolerateVirtualMember(ctx context.Context, virtualKey, member, what string, err error) {
	slog.WarnContext(ctx, "cargo: virtual member failed during aggregation (tolerated)",
		"virtual", virtualKey, "member", member, "what", what, "error", err.Error())
}

// serveVirtualIndexFile answers GET index/{pkgPath} on a virtual repository
// with the merged member files: rows deduplicated on
// (name, vers-ignoring-build-metadata) with the FIRST-SEEN member's raw row
// kept, ordered by SemVer (the local face's own ordering — a deterministic
// render, so the computed ETag is stable across identical member states).
// Validators follow the index row of spec section 5.3's cache table: ETag
// is the RENDERED merge's sha256 (no single node backs it), Last-Modified
// the newest contributing node's timestamp, both honored on 304.
func (h *Handler) serveVirtualIndexFile(ctx context.Context, w http.ResponseWriter, r *http.Request, virtualKey string, rt route) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, segIndex+"/"+rt.pkgPath)
		return
	}
	path := segIndex + "/" + rt.pkgPath
	var (
		rows     []parsedIndexRow
		seen     = map[string]bool{}
		newest   string // newest contributing node's UpdatedAt
		base     string // first contributing member: wins every duplicate tie
		baseHint http.Header
		failure  *repo.StatusError
	)
	for _, m := range order {
		raw, updated, hints, merr := h.memberIndexBytes(ctx, virtualKey, m.Key, path)
		if merr != nil {
			var se *repo.StatusError
			if errors.As(merr, &se) {
				if failure == nil {
					failure = se
				}
				tolerateVirtualMember(ctx, virtualKey, m.Key, "index merge", merr)
				continue
			}
			h.writeError(w, merr, virtualKey, path) // an internal fault is the honest 500
			return
		}
		if raw == nil {
			continue // the member does not carry the crate
		}
		if base == "" {
			base = m.Key
			baseHint = hints
		}
		if updated > newest {
			newest = updated
		}
		for _, row := range parseIndexRows(raw) {
			key := row.name + "\x00" + normalizeVersionKey(row.vers)
			if seen[key] {
				continue
			}
			seen[key] = true
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 && failure != nil {
		h.writeError(w, failure, virtualKey, path)
		return
	}
	if len(rows) == 0 {
		writeEnvelope(w, http.StatusNotFound, "not found")
		return
	}
	sort.Slice(rows, func(i, j int) bool {
		if c := compareSemver(rows[i].vers, rows[j].vers); c != 0 {
			return c < 0
		}
		if rows[i].vers != rows[j].vers {
			return rows[i].vers < rows[j].vers
		}
		return bytes.Compare(rows[i].raw, rows[j].raw) < 0
	})
	var body bytes.Buffer
	for _, row := range rows {
		body.Write(row.raw)
		body.WriteByte('\n')
	}
	rendered := body.Bytes()
	etag := `"` + blobRefOf(rendered).Sha256 + `"`
	lastMod := httpTime(newest)
	hdr := w.Header()
	for k, vv := range baseHint {
		for _, v := range vv {
			hdr.Add(k, v)
		}
	}
	if base != "" {
		// The base member wins every duplicate tie — exactly the
		// diagnostic the header exists for (the npm merged-packument
		// posture).
		hdr.Set(repo.HdrResolvedFrom, base)
	}
	hdr.Set("ETag", etag)
	if lastMod != "" {
		hdr.Set("Last-Modified", lastMod)
	}
	hdr.Set("Content-Type", "text/plain; charset=utf-8")
	hdr.Set("X-Content-Type-Options", "nosniff")
	if conditionalNotModified(r, etag, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Length", strconv.Itoa(len(rendered)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(rendered) //nolint:gosec // G705: member-served index rows, never request bytes
}

// ---- the search merge ----

// serveVirtualSearch answers GET api/v1/crates on a virtual repository:
// every member's search rows merged, names unioned FIRST-SEEN (the member
// that would serve the download wins the name — the ordering the whole
// virtual rides on), the merged set paged like the local face. Local
// members contribute their stored facts (the local face's own best-version
// rule, read on the MEMBER's nodes and properties); remote members
// contribute their upstream search through the query-keyed marker (the
// T-316 cache shape — ReadVirtualMember on a remote member IS that member's
// pull-through fetch of its upstream search endpoint, cached per query).
func (h *Handler) serveVirtualSearch(ctx context.Context, w http.ResponseWriter, r *http.Request, virtualKey string) {
	q := r.URL.Query()
	term := strings.ToLower(strings.TrimSpace(q.Get("q")))
	perPage := parsePerPage(q.Get("per_page"))

	order, oerr := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if oerr != nil {
		h.writeError(w, oerr, virtualKey, "")
		return
	}
	bestOf := map[string]*searchHit{}
	var (
		failure   *repo.StatusError
		collected bool // at least one member produced a successful read
	)
	for _, m := range order {
		var (
			rows []*searchHit
			merr error
		)
		if m.Type == repo.TypeLocal {
			rows, merr = h.memberSearchRows(ctx, virtualKey, m.Key, term)
		} else {
			rows, merr = h.memberRemoteSearchRows(ctx, virtualKey, m.Key, r.URL.RawQuery)
		}
		if merr != nil {
			var se *repo.StatusError
			if errors.As(merr, &se) {
				if failure == nil {
					failure = se
				}
				tolerateVirtualMember(ctx, virtualKey, m.Key, "search merge", merr)
				continue
			}
			h.writeError(w, merr, virtualKey, "")
			return
		}
		collected = true
		for _, hit := range rows {
			if _, ok := bestOf[hit.Name]; !ok {
				bestOf[hit.Name] = hit
			}
		}
	}
	if !collected && failure != nil {
		h.writeError(w, failure, virtualKey, "")
		return
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
		hits = append(hits, bestOf[name])
	}
	body, merr := json.Marshal(searchResponse{Crates: hits, Meta: searchMeta{Total: len(names)}})
	if merr != nil {
		writeEnvelope(w, http.StatusInternalServerError, merr.Error())
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// memberSearchRows derives one LOCAL member's search rows from its stored
// facts: the local face's own rule (best non-yanked version per crate, the
// wildcard term over name and description, the 1000-artifact scan cap)
// restated over the member seam — node facts via ListVirtualMember,
// properties on the member's own namespace.
func (h *Handler) memberSearchRows(ctx context.Context, virtualKey, member, term string) ([]*searchHit, error) {
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, member, dirCrates+"/")
	if err != nil {
		return nil, err
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
		props := h.memberPropsOf(ctx, member, n.Path)
		if _, yanked := props[propYanked]; yanked {
			continue // yanked versions never surface
		}
		description := firstProp(props, propDescription)
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
	out := make([]*searchHit, 0, len(bestOf))
	for name, b := range bestOf {
		out = append(out, &searchHit{Name: name, MaxVersion: b.version, Description: b.description})
	}
	return out, nil
}

// memberRemoteSearchRows reads one REMOTE member's search contribution
// through its query-keyed marker: the pull-through fetch of that member's
// upstream search endpoint (T-316's cache shape), parsed as the search
// contract the upstream serves. An unfound marker (negative cache, an
// offline upstream without a copy) contributes nothing.
func (h *Handler) memberRemoteSearchRows(ctx context.Context, virtualKey, member, rawQuery string) ([]*searchHit, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, searchCachePath(rawQuery))
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, maxIndexFileBytes))
	if rerr != nil {
		return nil, fmt.Errorf("read member search %s: %w", member, rerr)
	}
	var doc searchResponse
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return []*searchHit{}, nil // an unparseable upstream body contributes nothing
	}
	return doc.Crates, nil
}

// memberPropsOf reads one node's properties from the MEMBER's namespace (a
// virtual read may resolve across members, but the crate.yanked flag and
// the description live where the node lives). An empty map on any failure —
// search degrades to name-only facts, never errors.
func (h *Handler) memberPropsOf(ctx context.Context, member, path string) map[string][]string {
	if h.props == nil {
		return nil
	}
	props, err := h.props.List(ctx, member, path)
	if err != nil {
		return nil
	}
	return props
}

// ---- the yank family on the aggregate ----

// serveVirtualYank answers yank/unyank on a virtual repository by FIRST-HIT
// semantics: the member that would serve the download is the member whose
// copy the flag describes. A LOCAL holder takes the property and its own
// index rewrite (the ordinary yank body, addressed at the member); a REMOTE
// holder refuses — the cached copy is the upstream's truth, and a local
// yank flag on it would silently diverge every future cache refresh. The
// member comes off the resolution reader's X-BinFlow-Resolved-From hint,
// the diagnostic surface the service wraps every virtual hit with.
func (h *Handler) serveVirtualYank(ctx context.Context, w http.ResponseWriter, p *repo.Principal, virtualKey, name, version string, flag bool) {
	path := cratePath(name, version)
	rc, _, err := h.svc.Get(ctx, p, virtualKey, path)
	if err != nil {
		h.writeError(w, err, virtualKey, path)
		return
	}
	member := ""
	if extra, ok := rc.(interface{ ExtraHeaders() http.Header }); ok {
		member = extra.ExtraHeaders().Get(repo.HdrResolvedFrom)
	}
	_ = rc.Close() //nolint:errcheck // existence + provenance probe only
	if member == "" {
		writeEnvelope(w, http.StatusInternalServerError, "the virtual resolution did not name its member")
		return
	}
	class, err := h.classOf(ctx, member)
	if err != nil {
		h.writeError(w, err, member, path)
		return
	}
	if class != repo.TypeLocal {
		w.Header().Set("Allow", http.MethodGet)
		writeEnvelope(w, http.StatusMethodNotAllowed, msgRemoteWrite(member))
		return
	}
	if h.props == nil {
		writeEnvelope(w, http.StatusInternalServerError, "property store is not wired")
		return
	}
	var storeErr error
	if flag {
		storeErr = h.props.Merge(ctx, member, path, map[string][]string{propYanked: {"true"}})
	} else {
		storeErr = h.props.Delete(ctx, member, path, []string{propYanked})
	}
	if storeErr != nil {
		writeEnvelope(w, http.StatusInternalServerError, "update the yank property: "+storeErr.Error())
		return
	}
	if err := h.rewriteIndex(ctx, p, member, name); err != nil {
		writeEnvelope(w, http.StatusInternalServerError, "index rewrite: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, []byte(`{"ok":true}`))
}
