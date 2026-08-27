package rpm

// The REMOTE repository's protocol face (rpm.md section 6's remote row):
// a single-upstream mirror whose every read is a pull-through — the
// shared FR-20 engine behind svc.Get owns the cache, the guarded upstream
// hop and the stale downgrade, and the path grammar is pure storage
// addressing, so NO wire translation exists (the upstream URL is joined
// with the repo-relative path by the engine itself).
//
// What lives HERE:
//
//   - the expirable-set classification the engine's dual TTL consumes
//     (provider.Classify, S10): repomd.xml, the non-digest-prefixed
//     repodata files (group files, modules uploads) and the key-class
//     spellings refresh on the metadata TTL; the digest-prefixed index
//     generations cache with artifact semantics (they are immutable by
//     construction — a new generation means a NEW digest name).
//   - the property backfill (section 6 remote step 3): after a .rpm's
//     copy lands through the engine, the header parses asynchronously and
//     the rpm.metadata.* set merges onto the node — the facts search and
//     a future localization consume.
//   - the write posture: PUT never gets past the service's RE-05 read-only
//     405 (the engine-wide refusal, routed straight so the body never
//     spools through the parse chain first); DELETE rides the engine-wide
//     RE-06 cache-eviction verb (204 after a copy, 404 without one) —
//     BinFlow's cross-protocol posture, a registered divergence from the
//     Artifactory literal "PUT/DELETE 拒绝" (the DELETE half).
//
// The remote repository's own repodata is NEVER recomputed locally (the
// upstream's repodata caches verbatim); the virtual re-merge a repodata
// landing would trigger is bounded by the aggregate cache's short TTL
// (RP-3 — no persistent merge state to invalidate).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lzwzzy/binflow/internal/repo"
)

// NodeProps is the node-property seam the remote .rpm backfill writes
// (satisfied by metadata.Store.NodeProps(); nil disables the backfill —
// the copies still serve, only the rpm.metadata.* facts stay unwritten).
type NodeProps interface {
	// List returns one node's property set (the backfill's has-it check).
	List(ctx context.Context, repoKey, path string) (map[string][]string, error)
	// Merge replaces the named keys' values on one node (the backfill
	// write; the node must exist).
	Merge(ctx context.Context, repoKey, path string, props map[string][]string) error
}

// serveRemoteRpm is the remote .rpm read: the engine's pull-through, then
// (off the request's hot path) the header-parse backfill the landed copy
// feeds. HEAD lands a copy just like GET does, so both fire it.
func (h *Handler) serveRemoteRpm(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel string) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, rel)
	if err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	if h.props != nil {
		// principal may be nil (anonymous): an anonymous read still lands
		// the copy, and the backfill's own re-read runs under the same
		// anonymous posture — an instance that denies it answers with the
		// warn-and-skip below.
		principal := p
		//nolint:gosec // G118: the backfill deliberately detaches from the
		// request's lifetime — the response never waits on it, and the
		// copy is already landed (the goroutine's own read is a cache
		// hit, never a second upstream fetch).
		go h.backfillRpmProps(context.Background(), principal, repoKey, rel)
	}
	h.serveNode(ctx, w, r, node, rc, "application/x-rpm")
}

// backfillRpmProps parses one landed remote .rpm and merges the
// rpm.metadata.* set onto its node when it does not carry one yet
// (idempotent; failures log and leave the copy serving).
func (h *Handler) backfillRpmProps(ctx context.Context, p *repo.Principal, repoKey, rel string) {
	if existing, err := h.props.List(ctx, repoKey, rel); err == nil {
		if _, ok := existing["rpm.metadata.name"]; ok {
			return
		}
	}
	rc, _, err := h.svc.Get(ctx, p, repoKey, rel)
	if err != nil {
		slog.WarnContext(ctx, "rpm: remote property backfill could not re-read the copy",
			slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", err.Error()))
		return
	}
	hdr, perr := ParseHeader(rc)
	_ = rc.Close() //nolint:errcheck // read-only fd
	if perr != nil {
		slog.WarnContext(ctx, "rpm: remote property backfill skipped (not an RPM body)",
			slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", perr.Error()))
		return
	}
	props := rpmProps(hdr)
	if len(props) == 0 {
		return
	}
	if err := h.props.Merge(ctx, repoKey, rel, props); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		slog.WarnContext(ctx, "rpm: remote property backfill merge failed",
			slog.String("repo", repoKey), slog.String("path", rel), slog.String("error", err.Error()))
	}
}

// serveRemoteWrite routes a PUT onto the service so the engine-wide RE-05
// read-only 405 answers with its pinned wording and Allow header (the
// spool-and-parse the local upload chain runs never executes).
func (h *Handler) serveRemoteWrite(ctx context.Context, w http.ResponseWriter, r *http.Request, p *repo.Principal, repoKey, rel, ctype string) {
	expect, err := declaredDigests(r.Header)
	if err != nil {
		writeText(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.svc.Put(ctx, p, repoKey, rel, r.Body, expect, ctype); err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	// Unreachable in practice (the service refuses remote writes); the
	// landing shape stays for honesty if that refusal ever moves.
	writeText(w, http.StatusCreated, fmt.Sprintf("'%s/%s' stored", repoKey, rel))
}

// serveRemoteEvict routes a DELETE onto the service's RE-06 cache
// eviction (204 after a copy, 404 without one — the next read refetches).
func (h *Handler) serveRemoteEvict(ctx context.Context, w http.ResponseWriter, p *repo.Principal, repoKey, rel string) {
	if err := h.svc.Delete(ctx, p, repoKey, rel); err != nil {
		h.writeError(w, err, repoKey, rel)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
