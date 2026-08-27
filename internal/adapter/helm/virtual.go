package helm

// The VIRTUAL repository's protocol faces (helm.md section 7): the
// aggregated repo-root index.yaml (the MERGER strategy) with the S8 URL
// rewriting, plus the two dependency-proxy faces shared with the remote
// class. Chart/prov downloads need nothing here — svc.Get's two-bucket
// member resolver IS the first-hit strategy.
//
// Caching posture (a deliberate BinFlow simplification over S7's on-disk
// .index/<ctx>/<perm>/ cache): the aggregation computes PER REQUEST off
// the member ledger, the same posture as the pypi/npm/maven virtual
// metadata faces — a member-list change and a member chart upload are
// visible to the very next request (FR-15-AC6), and there is no virtual
// cache whose invalidation the local upload chain would have to fire
// (section 4.1 step 5 becomes vacuous). The members' own FR-20 caches
// provide the upstream quieting. The .index storage spelling stays
// reserved against client writes (the DB-3 posture, T-309).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/lzwzzy/binflow/internal/repo"
)

// msgIndexUnsupportedLocation is the sub-path index refusal (helm.md
// section 2's verbatim head; the spec's ellipsis interpolates this
// surface's spelling).
const msgIndexUnsupportedLocation = "The index file is being requested from an unsupported location; the aggregated index is served only at the repository root of '%s'."

// indexMerge is the aggregated index in the making: per (name, version)
// FIRST-WINS by member order (S13 — the earlier member is the one
// resolution would serve, so its entry keeps the digest and metadata that
// match the bytes the download plane hands out).
type indexMerge struct {
	entries map[string][]mergedEntry
	seen    map[string]bool
}

// mergedEntry is one parsed member entry plus the member context its
// urls[0] is rewritten against.
type mergedEntry struct {
	node *yaml.Node
	mc   memberCtx
}

// fold merges one member entry; a later member's same name+version drops
// out.
func (m *indexMerge) fold(name string, e *yaml.Node, mc memberCtx) {
	key := name + "\x00" + entryVersion(e)
	if m.seen[key] {
		return
	}
	m.seen[key] = true
	m.entries[name] = append(m.entries[name], mergedEntry{node: e, mc: mc})
}

// memberContext loads one member's rewriting context: the class and, for
// remote members, the upstream URL off the remote_configs seam (the
// chartsBaseUrl fallback IS the repository URL — a divergent chartsBaseUrl
// seat would land with the config schema ticket; registered).
func (h *Handler) memberContext(ctx context.Context, m repo.VirtualMember) memberCtx {
	mc := memberCtx{key: m.Key, typ: m.Type}
	if m.Type != repo.TypeRemote || h.remotes == nil {
		return mc
	}
	cfg, err := h.remotes.GetConfig(ctx, m.Key)
	if err != nil || cfg == nil {
		return mc
	}
	mc.chartsBase = cfg.URL
	return mc
}

// serveVirtualIndex answers GET/HEAD index.yaml on a virtual repository.
// HEAD is the cheap membership probe (section 7 point 5: members exist →
// 200 with no body, none → 404); GET walks the two-bucket order, reads
// every member's index (a local member's stored node; a remote member's
// upstream index through the full FR-20 chain), rewrites the urls and
// renders.
func (h *Handler) serveVirtualIndex(ctx context.Context, w http.ResponseWriter, r *http.Request, repoKey string) {
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, indexPath)
		return
	}
	if r.Method == http.MethodHead {
		if len(order) == 0 {
			writeText(w, http.StatusNotFound, fmt.Sprintf("'%s/%s' not found", repoKey, indexPath))
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	merge := &indexMerge{entries: map[string][]mergedEntry{}, seen: map[string]bool{}}
	contributed := 0
	var failure *repo.StatusError
	for _, m := range order {
		doc, ok, merr := h.readMemberIndex(ctx, repoKey, m.Key)
		if merr != nil {
			var se *repo.StatusError
			if errors.As(merr, &se) {
				// One member's classified fault must not block the rest
				// (the pypi/npm aggregation posture); the FIRST failure is
				// remembered and surfaces only when nothing else answered.
				if failure == nil {
					failure = se
				}
				slog.WarnContext(ctx, "helm: virtual member index failed — skipped",
					slog.String("virtual", repoKey), slog.String("member", m.Key), slog.String("error", se.Message))
				continue
			}
			h.writeError(w, merr, repoKey, indexPath)
			return
		}
		if !ok {
			continue // the member has no index yet (an empty local repo, an upstream miss)
		}
		contributed++
		mc := h.memberContext(ctx, m)
		for name, list := range doc.entries {
			for _, e := range list {
				merge.fold(name, e, mc)
			}
		}
	}
	if contributed == 0 {
		if failure != nil {
			h.writeError(w, failure, repoKey, indexPath)
			return
		}
		h.writeError(w, fmt.Errorf("node %s/%s: %w", repoKey, indexPath, repo.ErrNodeNotFound), repoKey, indexPath)
		return
	}

	body := merge.render(h.now(), h.opts.BaseURL, h.externalPatterns())
	hdr := w.Header()
	hdr.Set("Content-Type", "text/yaml")
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G104: the computed index body; a client hang is not an error here
}

// readMemberIndex reads one member's index document through the ungated
// aggregation seam. ok=false is the member's quiet miss.
func (h *Handler) readMemberIndex(ctx context.Context, virtualKey, member string) (*indexDoc, bool, error) {
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, member, indexPath)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) || errors.Is(err, repo.ErrIsFolder) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // read-only fd
	body, rerr := io.ReadAll(io.LimitReader(rc, maxIndexBytes))
	if rerr != nil {
		return nil, false, fmt.Errorf("read %s/%s: %w", member, indexPath, rerr)
	}
	return parseIndex(body), true, nil
}

// render serializes the merged index: the apiVersion/entries/generated
// head, chart names sorted, each chart's versions SemVer-descending, every
// entry's urls[0] rewritten against its member's context.
func (m *indexMerge) render(now time.Time, baseURL string, patterns []string) []byte {
	doc := &indexDoc{entries: map[string][]*yaml.Node{}}
	for name, list := range m.entries {
		for _, en := range list {
			clone := *en.node
			setEntryURL(&clone, rewriteVirtualURL(entryURL(en.node), en.mc, baseURL, patterns))
			doc.entries[name] = append(doc.entries[name], &clone)
		}
		sortEntriesDesc(doc.entries[name])
	}
	return doc.render(now)
}

// setEntryURL replaces one entry node's urls[0] (a no-op when the entry
// carries no urls — an upstream oddity that rides verbatim). The member
// documents are per-request parses, so the in-place splice is safe.
func setEntryURL(entry *yaml.Node, newURL string) {
	for i := 0; i+1 < len(entry.Content); i += 2 {
		if entry.Content[i].Value != "urls" {
			continue
		}
		seq := entry.Content[i+1]
		if seq.Kind != yaml.SequenceNode || len(seq.Content) == 0 {
			return
		}
		seq.Content[0].Value = newURL
		return
	}
}

// serveVirtualExternal answers GET _external/<protocol>/<url...> on a
// virtual repository: the remote members whose allow list admits the URL
// are the egress candidates, in two-bucket order; the first that answers
// (or the first classified refusal) serves. No admitting member at all is
// the pinned 400.
func (h *Handler) serveVirtualExternal(ctx context.Context, w http.ResponseWriter, repoKey, rel string) {
	target, err := parseExternalPath(rel, segExternal)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	admitted := false
	for _, m := range order {
		if m.Type != repo.TypeRemote {
			continue // a local member has no egress face
		}
		if !externalAllowed(h.externalPatterns(), target) {
			continue
		}
		admitted = true
		out := h.streamExternal(ctx, w, m.Key, target)
		if out.writeFn != nil {
			_ = out.writeFn()
			return
		}
		if out.served {
			return // the classified refusal is the answer (the same target would refuse on every member)
		}
		// An upstream miss walks on to the next member.
	}
	if !admitted {
		writeText(w, http.StatusBadRequest, fmt.Sprintf(msgNotExternalDependency, target))
		return
	}
	writeText(w, http.StatusNotFound, fmt.Sprintf(
		"Failed to find the external dependency '%s' in repository '%s'.", target, repoKey))
}

// serveVirtualTransitive answers GET _transitive/<protocol>/<url...> on a
// virtual repository: every member is read at the upstream-_external
// storage path (a remote member's read IS the path-joined upstream fetch;
// a local member's is a node lookup that misses), first hit serves.
func (h *Handler) serveVirtualTransitive(ctx context.Context, w http.ResponseWriter, r *http.Request, _ *repo.Principal, repoKey, rel string) {
	fetchPath := transitiveFetchPath(rel)
	order, err := h.svc.VirtualMemberOrder(ctx, repoKey)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	for _, m := range order {
		rc, node, merr := h.svc.ReadVirtualMember(ctx, repoKey, m.Key, fetchPath)
		if merr != nil {
			if errors.Is(merr, repo.ErrNodeNotFound) || errors.Is(merr, repo.ErrIsFolder) {
				continue
			}
			var se *repo.StatusError
			if errors.As(merr, &se) && se.Code != http.StatusNotFound {
				// A member's classified fault is its own answer (the SSRF
				// 400 family); an ordinary miss walks on.
				h.writeError(w, se, repoKey, rel)
				return
			}
			continue
		}
		h.serveNode(ctx, w, r, node, rc, nodeCType(node))
		return
	}
	h.writeError(w, fmt.Errorf("node %s/%s: %w", repoKey, rel, repo.ErrNodeNotFound), repoKey, rel)
}
