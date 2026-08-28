package nuget

import (
	"context"
	"io"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual v2 search merge (nuget.md section 7.2, the nine steps):
//
//  1. member resolution through the two-bucket order (prioritised bucket
//     first), every member contributing;
//  2. fan-out — local members answer from stored facts (the deep chain),
//     remote members through the section 7.1 upstream proxy (the marker
//     read through the member seam);
//  3. $select stays reset across the fan-out (BinFlow never projects
//     member-side — the option is accepted and ignored, one shape back);
//  4. GetUpdates() without $orderby orders by version (the auto-injection
//     — sortV2Rows applies it);
//  5. id-level dedup ACROSS buckets: a package id the higher-priority
//     bucket already answered is dropped from every lower bucket WHOLE
//     (never version-merged across buckets); within a bucket members merge
//     in order, first-seen version wins;
//  6. every entry's download link cites THIS virtual's v2 Download face
//     (the renderer's linksV2 arm);
//  7. ordering: id lowercase ascending, then the NuGet version ascending;
//  8. the IsLatest legacy aggregation over the merged set (the default
//     mode — recomputeV2Latest); $inlinecount's total is the merged size;
//  9. the include/exclude pattern final filter — BinFlow's virtual
//     repositories carry no per-repo include/exclude patterns (the
//     governance plane is the local member's), so this step is the no-op
//     the deviation register records, and skip/top page the merged rows.
func (h *Handler) v2VirtualRows(ctx context.Context, _ *http.Request, virtualKey string, req *v2FeedReq) ([]v2Row, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}

	var ids []string
	switch req.kind {
	case kindV2FindPackages:
		ids = []string{req.id}
	case kindV2GetUpdates:
		ids = req.updates.ids
	}

	var rows []v2Row
	seenVersions := map[string]bool{}
	answered := map[string]bool{} // ids the answered buckets own
	for _, prioritised := range []bool{true, false} {
		bucketSeen := map[string]bool{}
		for _, m := range order {
			if m.Priority != prioritised {
				continue
			}
			var memberRows []v2Row
			if m.Type == repo.TypeLocal {
				memberRows = h.v2MemberLocalRows(ctx, virtualKey, m.Key, ids)
			} else {
				memberRows = h.v2MemberRemoteRows(ctx, virtualKey, m.Key, req)
			}
			for _, row := range memberRows {
				idLow := lowerASCII(row.id)
				if answered[idLow] {
					continue // 7.2-5: the id belongs to a higher bucket
				}
				key := idLow + "\x00" + row.version
				if seenVersions[key] {
					continue
				}
				seenVersions[key] = true
				rows = append(rows, row)
				bucketSeen[idLow] = true
			}
		}
		for id := range bucketSeen {
			answered[id] = true
		}
	}
	return rows, nil
}

// v2MemberLocalRows collects one LOCAL member's facts through the member
// seam (the deep chain: canonical directory first, the listing walk behind
// it), rendered as rows with the member's own latest computation — the
// merge recomputes the flags over the merged set anyway.
func (h *Handler) v2MemberLocalRows(ctx context.Context, virtualKey, member string, ids []string) []v2Row {
	if ids != nil {
		var rows []v2Row
		for _, raw := range ids {
			id := lowerASCII(raw)
			if id == "" || !validPackageID(id) {
				continue
			}
			rows = append(rows, rowsOfFacts(h.collectMemberFactsDeep(ctx, virtualKey, member, id))...)
		}
		return rows
	}
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, member, "")
	if err != nil {
		return nil
	}
	refs := map[string][]storedRef{}
	for _, n := range nodes {
		if id, version, ok := splitAnyNupkgNode(n.Path); ok {
			refs[id] = append(refs[id], storedRef{path: n.Path, version: version, node: n})
		}
	}
	read := func(path string) ([]byte, bool) {
		rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, path)
		if err != nil {
			return nil, false
		}
		defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
		body, rerr := io.ReadAll(io.LimitReader(rc, 8<<20))
		if rerr != nil {
			return nil, false
		}
		return body, true
	}
	var rows []v2Row
	for id, list := range refs {
		rows = append(rows, rowsOfFacts(factsOfRefs(read, id, list))...)
	}
	return rows
}

// collectMemberFactsDeep is the member-seam twin of collectLocalFactsDeep:
// the canonical facts UNION the listing walk's matches (a member holding
// both shapes answers with every version).
func (h *Handler) collectMemberFactsDeep(ctx context.Context, virtualKey, member, id string) []versionFacts {
	facts, _ := h.collectMemberFacts(ctx, virtualKey, member, id)
	nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, member, "")
	if err != nil {
		return facts
	}
	seen := map[string]bool{}
	for _, f := range facts {
		seen[f.ref.version] = true
	}
	deep := false
	var refs []storedRef
	for _, n := range nodes {
		if gotID, version, ok := splitAnyNupkgNode(n.Path); ok && gotID == id {
			refs = append(refs, storedRef{path: n.Path, version: version, node: n})
			if !seen[version] {
				deep = true
			}
		}
	}
	if !deep {
		return facts
	}
	read := func(path string) ([]byte, bool) { return h.readMemberSidecar(ctx, virtualKey, member, path) }
	return factsOfRefs(read, id, refs)
}

// rowsOfFacts maps facts onto render rows.
func rowsOfFacts(facts []versionFacts) []v2Row {
	rows := make([]v2Row, 0, len(facts))
	for _, f := range facts {
		rows = append(rows, rowFromFacts(f))
	}
	return rows
}

// v2MemberRemoteRows reads one REMOTE member's proxied feed through the
// member seam (the marker read rides the member's full pull-through chain).
func (h *Handler) v2MemberRemoteRows(ctx context.Context, virtualKey, member string, req *v2FeedReq) []v2Row {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, v2CachePath(v2ResourceOf(req)))
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, rerr := io.ReadAll(io.LimitReader(rc, 64<<20))
	if rerr != nil {
		return nil
	}
	rows, perr := parseV2Feed(gunzipIfNeeded(body))
	if perr != nil {
		return nil
	}
	return rows
}
