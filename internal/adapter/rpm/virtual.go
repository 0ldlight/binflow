package rpm

// The VIRTUAL repository's protocol faces (rpm.md section 6.3, RP-3's
// tightened minimal boundary):
//
//   - GET/HEAD repodata/repomd.xml and the digest-prefixed index files are
//     the AGGREGATED repodata: every non-cache member's repomd walks, its
//     primary/filelists/other(/modules/group/updateinfo) documents split
//     into package segments, the segments merge in member order (S11:
//     name+arch drops ONLY against priority members — non-priority members
//     never dedup among themselves), and the merged documents re-gzip
//     under fresh digest names with a fresh repomd. The aggregate is
//     UNSIGNED (RP-3).
//   - RP-3's tightening: NO repomd-sha1 cache-key persistence — the
//     aggregate caches IN PROCESS behind a singleflight with a short TTL,
//     so a client's repomd.xml fetch and its following index fetches by
//     digest stay coherent (the same aggregate answers within the window)
//     while member-list changes stay visible on the very next request
//     (the cache key carries the member-order signature, FR-15-AC6).
//   - The S11 collection rules that decide whether an aggregate exists at
//     all: fewer than two members WITH a repomd → NO aggregation, the read
//     passthroughs to first-hit member resolution; the synchronous compute
//     waits at most 5s (the spec's sync timeout) and then falls back to
//     the stale aggregate or the passthrough while the computation
//     continues in the background.
//   - .rpm downloads and every non-repodata path need nothing here —
//     svc.Get's first-hit member resolution IS the download plane; writes
//     ride the service's defaultDeploymentRepo routing (an un-routed
//     virtual answers the C5 405 there), and the upload chain's recompute
//     trigger targets the LANDING member (handler.go).
//
// BinFlow has no separate "-cache" member repositories (a remote member's
// copies land in the remote repository itself), so every member of the
// two-bucket order is a non-cache member.

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The RP-3 / S11 aggregation constants.
const (
	// aggTTLDefault is the in-process aggregate cache's short TTL (RP-3's
	// replacement for the repomd-sha1 persistence: the coherence window a
	// client's repomd→index fetch pair needs, short enough that member
	// content changes surface promptly).
	aggTTLDefault = 30 * time.Second
	// aggNegativeTTL bounds how long a "cannot aggregate" verdict is
	// remembered before the member walk runs again.
	aggNegativeTTL = 15 * time.Second
	// aggSyncTimeout is the spec's synchronous calculation timeout
	// (rpm.md section 6.3 step 2's 5000ms): past it the request falls back
	// to the stale aggregate or the passthrough while the compute runs on.
	aggSyncTimeout = 5 * time.Second
	// aggAsyncBudget bounds the background compute's lifetime (a stuck
	// member read must not leak a goroutine forever).
	aggAsyncBudget = 60 * time.Second
)

// errNotAggregatable is the S11 verdict "fewer than two members carry a
// repomd" — the caller passthroughs to first-hit resolution.
var errNotAggregatable = errors.New("rpm: fewer than two members carry repodata")

// msgVirtualUnsigned answers the repomd signature family on a virtual
// repository: the aggregate is unsigned (RP-3 — a virtual repository
// cannot carry a keyPairName association, and a MEMBER's signature would
// not verify against the merged repomd anyway).
const msgVirtualUnsigned = "The aggregated repomd of virtual repository '%s' is not signed (GPG metadata signing is a local-repository behavior and a member's signature would not verify against the merged repomd); serve signed repodata from a member repository."

// aggFile is one merged index body at its wire path.
type aggFile struct {
	body  []byte
	ctype string
}

// virtualAgg is one virtual root's aggregated repodata.
type virtualAgg struct {
	repomd []byte
	files  map[string]aggFile // absolute repo-relative wire path -> body
}

// aggEntry is one cached aggregate verdict (a built aggregate or the
// negative "cannot aggregate" mark) plus its member-order signature.
type aggEntry struct {
	agg      *virtualAgg // nil on the negative verdict
	sig      string
	built    time.Time
	negative bool
}

// aggFlight is one in-flight aggregate computation (RP-3's singleflight).
type aggFlight struct {
	done chan struct{}
	agg  *virtualAgg
	ok   bool
}

// virtualAggs is the handler's in-process aggregate cache (RP-3: no
// persistent cache-key state, singleflight + short TTL only).
type virtualAggs struct {
	mu      sync.Mutex
	entries map[string]*aggEntry
	flights map[string]*aggFlight
}

// orderSignature renders the member-order signature a cache key carries:
// membership, class and priority marks — a member-list change invalidates
// instantly (FR-15-AC6) while member content keeps its TTL window.
func orderSignature(order []repo.VirtualMember) string {
	var b strings.Builder
	for _, m := range order {
		b.WriteString(m.Key)
		b.WriteByte(':')
		b.WriteString(m.Type)
		if m.Priority {
			b.WriteByte('!')
		}
		b.WriteByte(' ')
	}
	return b.String()
}

// aggregateFor resolves one virtual root's aggregate through the cache:
// the fresh entry first, then the singleflight (waiting at most the sync
// timeout), computing when this caller leads. ok is false when the
// verdict is "not aggregatable", the sync timeout fired (the fallback is
// the stale aggregate, then the passthrough), or the compute failed.
func (h *Handler) aggregateFor(ctx context.Context, repoKey, root string, order []repo.VirtualMember) (*virtualAgg, bool) {
	key := repoKey + "\x00" + root
	sig := orderSignature(order)
	now := h.now()

	h.aggs.mu.Lock()
	if h.aggs.entries == nil {
		h.aggs.entries = map[string]*aggEntry{}
	}
	if e, ok := h.aggs.entries[key]; ok && e.sig == sig {
		ttl := time.Duration(aggTTLDefault)
		if h.opts.AggTTL > 0 {
			ttl = h.opts.AggTTL
		}
		if e.negative {
			ttl = aggNegativeTTL
		}
		if now.Sub(e.built) < ttl {
			h.aggs.mu.Unlock()
			return e.agg, !e.negative
		}
	}
	if f, ok := h.aggs.flights[key]; ok {
		h.aggs.mu.Unlock()
		select {
		case <-f.done:
			return f.agg, f.ok
		case <-time.After(aggSyncTimeout):
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
	f := &aggFlight{done: make(chan struct{})}
	if h.aggs.flights == nil {
		h.aggs.flights = map[string]*aggFlight{}
	}
	h.aggs.flights[key] = f
	h.aggs.mu.Unlock()

	// The leader detaches from the request's lifetime: the S11 timeout
	// posture is "the calculation continues asynchronously, THIS request
	// falls back" — the compute runs under its own budget and populates
	// the cache for the requests behind it.
	//nolint:gosec // G118: the aggregate compute deliberately detaches
	// from the request's lifetime (the sync-timeout-continue-async
	// posture); its own budget bounds the run.
	go func() {
		bctx, cancel := context.WithTimeout(context.Background(), aggAsyncBudget)
		defer cancel()
		agg, err := h.computeAggregate(bctx, repoKey, root, order)
		h.aggs.mu.Lock()
		entry := &aggEntry{sig: sig, built: h.now()}
		if err == nil {
			entry.agg = agg
			f.agg, f.ok = agg, true
		} else {
			entry.negative = true
			if !errors.Is(err, errNotAggregatable) {
				slog.WarnContext(bctx, "rpm: virtual aggregate failed (cached negative)",
					"virtual", repoKey, "root", root, "error", err.Error())
			}
		}
		h.aggs.entries[key] = entry
		delete(h.aggs.flights, key)
		h.aggs.mu.Unlock()
		close(f.done)
	}()

	select {
	case <-f.done:
		return f.agg, f.ok
	case <-time.After(aggSyncTimeout):
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
}

// staleAggregate serves the timeout fallback's first stop: a stale entry
// of the SAME member order (S11's "回落缓存"; a changed member list
// prefers the passthrough over the old members' aggregate).
func (h *Handler) staleAggregate(repoKey, root string, sig string) *virtualAgg {
	h.aggs.mu.Lock()
	defer h.aggs.mu.Unlock()
	e, ok := h.aggs.entries[repoKey+"\x00"+root]
	if !ok || e.negative || e.sig != sig {
		return nil
	}
	return e.agg
}

// InvalidateVirtual drops every cached aggregate of one virtual repository
// (the /api/yum virtual branch's "trigger re-merge": the next read
// rebuilds — the seam the management plane's virtual arms call once they
// land in httpapi).
func (h *Handler) InvalidateVirtual(repoKey string) {
	h.aggs.mu.Lock()
	prefix := repoKey + "\x00"
	for k := range h.aggs.entries {
		if strings.HasPrefix(k, prefix) {
			delete(h.aggs.entries, k)
		}
	}
	h.aggs.mu.Unlock()
}

// ---- the collection and merge ----

// memberRepomd is one member's parsed repomd document.
type memberRepomd struct {
	m     repo.VirtualMember
	hrefs map[string]string // data type -> location href
}

// computeAggregate builds one virtual root's merged repodata (S11 steps
// 3-5).
func (h *Handler) computeAggregate(ctx context.Context, repoKey, root string, order []repo.VirtualMember) (*virtualAgg, error) {
	var members []memberRepomd
	for _, m := range order {
		raw, err := h.memberDoc(ctx, repoKey, m.Key, joinRoot(root, fileRepomd))
		if err != nil {
			// One member's classified fault must not block the aggregate
			// (the npm/pypi aggregation posture): skip, remember in the
			// log. An ordinary miss is the member's quiet absence.
			slog.WarnContext(ctx, "rpm: virtual member repomd failed — skipped",
				"virtual", repoKey, "member", m.Key, "error", err.Error())
			continue
		}
		if raw == nil {
			continue
		}
		hrefs := parseRepomdLocations(raw)
		if len(hrefs) == 0 {
			continue // a repomd without parsable data entries: member skipped
		}
		members = append(members, memberRepomd{m: m, hrefs: hrefs})
	}
	if len(members) < 2 {
		return nil, errNotAggregatable
	}

	agg := &virtualAgg{files: map[string]aggFile{}}
	var dataEls []*dataEntry
	add := func(d *dataEntry, ctype string) {
		agg.files[joinRoot(root, d.href)] = aggFile{body: d.body, ctype: ctype}
		dataEls = append(dataEls, d)
	}

	// primary / filelists / other: the package families with the S11
	// name+arch priority dedup (applied uniformly so filelists/other never
	// dangle behind a dropped primary entry).
	primary := h.collectFamily(ctx, repoKey, root, members, "primary", false)
	d, err := dataEntryFor("primary", renderMergedPrimary(mergePackageSegments(primary)))
	if err != nil {
		return nil, err
	}
	add(d, "application/gzip")
	if other := h.collectFamily(ctx, repoKey, root, members, "other", true); len(other) > 0 {
		d, err := dataEntryFor("other", renderMergedOther(mergePackageSegments(other)))
		if err != nil {
			return nil, err
		}
		add(d, "application/gzip")
	}
	if fl := h.collectFamily(ctx, repoKey, root, members, "filelists", true); len(fl) > 0 {
		d, err := dataEntryFor("filelists", renderMergedFilelists(mergePackageSegments(fl)))
		if err != nil {
			return nil, err
		}
		add(d, "application/gzip")
	}

	// modules: the modularity stream, concatenated from the NON-PRIORITY
	// members only (S11's literal collection rule; registered with its
	// confidence in the ticket report).
	var modDocs [][]byte
	for _, mm := range members {
		if mm.m.Priority {
			continue
		}
		if body, ok := h.memberIndexBody(ctx, repoKey, root, mm, "modules"); ok {
			modDocs = append(modDocs, body)
		}
	}
	if len(modDocs) > 0 {
		d, err := dataEntryFile("modules", "modules.yaml.gz", mergeModuleDocs(modDocs))
		if err != nil {
			return nil, err
		}
		add(d, "application/gzip")
	}

	// updateinfo: the same non-priority-only collection, segment-concat
	// under <updates>.
	var updSegs []memberSegs
	for _, mm := range members {
		if mm.m.Priority {
			continue
		}
		if body, ok := h.memberIndexBody(ctx, repoKey, root, mm, "updateinfo"); ok {
			if segs, err := extractSegments(body, false); err == nil {
				updSegs = append(updSegs, memberSegs{segs: segs.segs})
			}
		}
	}
	if merged := mergePackageSegments(updSegs); len(merged) > 0 {
		d, err := dataEntryFor("updateinfo", renderMergedUpdateinfo(merged))
		if err != nil {
			return nil, err
		}
		add(d, "application/gzip")
	}

	// group (comps): every member's group document (the plain spelling,
	// falling back to the .gz), segments concatenated; the first
	// contributing member's file name keeps its spelling.
	var groupSegs []segEntry
	groupName := ""
	for _, mm := range members {
		body, name, ok := h.memberGroupBody(ctx, repoKey, root, mm)
		if !ok {
			continue
		}
		if groupName == "" {
			groupName = name
		}
		if segs, err := extractSegments(body, false); err == nil {
			groupSegs = append(groupSegs, segs.segs...)
		}
	}
	if len(groupSegs) > 0 && groupName != "" {
		g, gg, err := groupDataEntries(groupName, renderMergedComps(groupSegs))
		if err != nil {
			return nil, err
		}
		add(g, "text/xml")
		add(gg, "application/gzip")
	}

	agg.repomd = renderRepomd(dataEls, h.now().Unix())
	slog.InfoContext(ctx, "rpm: virtual repodata aggregate built",
		"virtual", repoKey, "root", root, "members", len(members))
	return agg, nil
}

// collectFamily gathers every member's contribution to one package index
// family, in member order (the merge input).
func (h *Handler) collectFamily(ctx context.Context, repoKey, root string, members []memberRepomd, typ string, attrIdentity bool) []memberSegs {
	var out []memberSegs
	for _, mm := range members {
		body, ok := h.memberIndexBody(ctx, repoKey, root, mm, typ)
		if !ok {
			continue // the member does not carry the family (or it failed)
		}
		segs, err := extractSegments(body, attrIdentity)
		if err != nil {
			slog.WarnContext(ctx, "rpm: virtual member index unparseable — family skipped for the member",
				"virtual", repoKey, "member", mm.m.Key, "family", typ, "error", err.Error())
			continue
		}
		out = append(out, memberSegs{priority: mm.m.Priority, segs: segs.segs})
	}
	return out
}

// memberIndexBody reads one member's index document of the given repomd
// data type, decompressed. ok is false when the member carries no such
// entry or the read failed (both are the member's quiet absence for the
// family).
func (h *Handler) memberIndexBody(ctx context.Context, repoKey, root string, mm memberRepomd, typ string) ([]byte, bool) {
	href, ok := mm.hrefs[typ]
	if !ok {
		return nil, false
	}
	return h.memberHrefBody(ctx, repoKey, root, mm.m.Key, typ, href)
}

// memberHrefBody reads and decompresses one member repomd href's file.
func (h *Handler) memberHrefBody(ctx context.Context, repoKey, root, member, typ, href string) ([]byte, bool) {
	full, ok := safeMemberHref(root, href)
	if !ok {
		slog.WarnContext(ctx, "rpm: virtual member repomd carries an unsafe location href — entry skipped",
			"virtual", repoKey, "member", member, "type", typ, "href", href)
		return nil, false
	}
	raw, err := h.memberDoc(ctx, repoKey, member, full)
	if err != nil || raw == nil {
		if err != nil {
			slog.WarnContext(ctx, "rpm: virtual member index read failed — family skipped for the member",
				"virtual", repoKey, "member", member, "type", typ, "error", err.Error())
		}
		return nil, false
	}
	if strings.HasSuffix(href, ".gz") {
		out, err := gunzipBytes(raw)
		if err != nil {
			slog.WarnContext(ctx, "rpm: virtual member index gunzip failed — family skipped for the member",
				"virtual", repoKey, "member", member, "type", typ, "error", err.Error())
			return nil, false
		}
		return out, true
	}
	return raw, true
}

// memberGroupBody reads one member's comps document: the plain group
// spelling first, the group_gz body gunzipped as the fallback. The
// returned name is the file spelling the repomd href carries (the first
// contributing member's spelling names the aggregate's pair).
func (h *Handler) memberGroupBody(ctx context.Context, repoKey, root string, mm memberRepomd) ([]byte, string, bool) {
	if href, ok := mm.hrefs["group"]; ok {
		if body, ok := h.memberHrefBody(ctx, repoKey, root, mm.m.Key, "group", href); ok {
			return body, groupFileNameOf(href), true
		}
	}
	if href, ok := mm.hrefs["group_gz"]; ok {
		if body, ok := h.memberHrefBody(ctx, repoKey, root, mm.m.Key, "group_gz", href); ok {
			name := strings.TrimSuffix(groupFileNameOf(href), ".gz")
			return body, name, true
		}
	}
	return nil, "", false
}

// groupFileNameOf renders one group href's file spelling: the digest
// prefix (if any) stripped, e.g. "repodata/<digest>-comps.xml" →
// "comps.xml".
func groupFileNameOf(href string) string {
	base := path.Base(href)
	if rest, ok := stripDigestPrefix(base); ok {
		base = rest
	}
	return base
}

// memberDoc reads one member's document through the ungated aggregation
// seam; (nil, nil) is the member's quiet miss.
func (h *Handler) memberDoc(ctx context.Context, virtualKey, member, docPath string) ([]byte, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, docPath)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, maxIndexReadBytes))
	if err != nil {
		return nil, fmt.Errorf("read member document %s/%s: %w", member, docPath, err)
	}
	return raw, nil
}

// repomdProbe is the tolerant repomd location reader (the member
// collection's only interest: which data types live at which hrefs).
type repomdProbe struct {
	Data []struct {
		Type     string `xml:"type,attr"`
		Location struct {
			Href string `xml:"href,attr"`
		} `xml:"location"`
	} `xml:"data"`
}

// parseRepomdLocations extracts the data type → location href map; a
// document without any parsable data entry answers nil.
func parseRepomdLocations(body []byte) map[string]string {
	var probe repomdProbe
	if err := xml.Unmarshal(body, &probe); err != nil { //nolint:gosec // G709: inert href decode, no entity expansion
		return nil
	}
	out := map[string]string{}
	for _, d := range probe.Data {
		if d.Type != "" && d.Location.Href != "" {
			out[d.Type] = d.Location.Href
		}
	}
	return out
}

// ---- the serving faces ----

// serveVirtualRepomd answers GET/HEAD repodata/repomd.xml on a virtual
// repository: the ≥2-member aggregate, with the S11 fallback ladder
// (fresh → flight-with-timeout → stale → first-hit passthrough).
func (h *Handler) serveVirtualRepomd(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, root, rel string) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	if len(order) >= 2 {
		if agg, ok := h.aggregateFor(ctx, repoKey, root, order); ok {
			writeBody(w, r, http.StatusOK, "text/xml", agg.repomd)
			return
		}
		if stale := h.staleAggregate(repoKey, root, orderSignature(order)); stale != nil {
			writeBody(w, r, http.StatusOK, "text/xml", stale.repomd)
			return
		}
	}
	// Fewer than two members, an unbuildable aggregate, or the sync
	// timeout with nothing stale: the first-hit member resolution.
	h.serveStoredFile(ctx, w, r, repoKey, rel, "text/xml")
}

// serveVirtualIndex answers GET/HEAD of a digest-prefixed index file on a
// virtual repository: the aggregate's own file when its digest is the
// current one, the first-hit member resolution otherwise (a member-local
// file, or the honest 404 of a generation the aggregate replaced).
func (h *Handler) serveVirtualIndex(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey, root, rel, ctype string) {
	if order, err := h.svc.VirtualMemberOrder(ctx, repoKey); err == nil && len(order) >= 2 {
		if agg, ok := h.aggregateFor(ctx, repoKey, root, order); ok {
			if f, hit := agg.files[rel]; hit {
				writeBody(w, r, http.StatusOK, f.ctype, f.body)
				return
			}
		}
	}
	h.serveStoredFile(ctx, w, r, repoKey, rel, ctype)
}

// writeBody renders one computed body (headers on HEAD, bytes on GET).
func writeBody(w http.ResponseWriter, r *http.Request, status int, ctype string, body []byte) {
	hdr := w.Header()
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body) //nolint:gosec // G705: server-computed bytes, never client input
}
