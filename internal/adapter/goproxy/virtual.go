package goproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The virtual-repository face (goproxy.md section 6.3). Version-file GETs
// need nothing here — svc.Get already walks the two-bucket member order
// first-hit-stops (the local-first posture is the member list's front).
// The two aggregations live in this file:
//
//   - @v/list: the cross-member UNION, deduplicated in member order and
//     rendered in the stable lexicographic form;
//   - @latest: every member's candidate collected, the global best by the
//     section 4.5 order picked, and THAT member's original body served.
//
// Member failures are tolerated (the npm/pypi aggregation rule: one member
// failing must not block the others) — except that a member's classified
// non-unfound failure on the winning read path (the download resolver's
// strictness) already propagated inside svc.Get; the aggregations here only
// ever see per-member misses.

// memberListVersions computes one member's contribution to the union list.
func (h *Handler) memberListVersions(ctx context.Context, virtualKey string, m repo.VirtualMember, module string) ([]string, error) {
	if m.Type == repo.TypeLocal {
		nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, module+segVersionMarker)
		if err != nil {
			return nil, err
		}
		var versions []string
		for _, n := range nodes {
			if v, ok := versionOfZipNode(n.Path); ok {
				versions = append(versions, v)
			}
		}
		return versions, nil
	}
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, m.Key, versionListMarker(module))
	if err != nil {
		return nil, err
	}
	body, err := upstreamBody(rc)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, line := range splitLines(body) {
		if line != "" {
			versions = append(versions, line)
		}
	}
	return versions, nil
}

// splitLines splits a text/plain list body (CRLF tolerant).
func splitLines(body []byte) []string {
	lines := make([]string, 0, 16)
	start := 0
	for i, b := range body {
		if b == '\n' || b == '\r' {
			lines = append(lines, string(body[start:i]))
			if b == '\r' && i+1 < len(body) && body[i+1] == '\n' {
				start = i + 2
			} else {
				start = i + 1
			}
		}
	}
	lines = append(lines, string(body[start:]))
	return lines
}

// serveVirtualList renders the union list (member order dedup, stable
// lexicographic output). The member reads run ungated through the
// aggregation seams — the virtual's own read gate already ran.
func (h *Handler) serveVirtualList(ctx context.Context, w http.ResponseWriter, virtualKey string, t target) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, t.module)
		return
	}
	seen := map[string]bool{}
	var versions []string
	for _, m := range order {
		mv, err := h.memberListVersions(ctx, virtualKey, m, t.module)
		if err != nil {
			h.tolerateMemberFailure(ctx, virtualKey, m.Key, "list aggregation", err)
			continue
		}
		for _, v := range mv {
			if !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
	}
	if len(versions) == 0 {
		writePlain(w, http.StatusNotFound, msgNotFound)
		return
	}
	writeVersionList(w, versions)
}

// tolerateMemberFailure logs one member's aggregation failure and moves on
// (the aggregation rule: a failing member must not block the rest).
func (h *Handler) tolerateMemberFailure(ctx context.Context, virtualKey, member, what string, err error) {
	slog.WarnContext(ctx, "goproxy: virtual member failed during aggregation (tolerated)",
		slog.String("virtual", virtualKey), slog.String("member", member),
		slog.String("what", what), slog.String("error", err.Error()))
}

// readLocalMemberNode looks one path up in the virtual's LOCAL members
// only (membership-guarded, ungated reads — the ReadVirtualMember seam).
// It is the +incompatible .mod probe: remote members are deliberately not
// asked (section 6.2's no-upstream rule holds through the aggregation).
// A member error other than the not-found family is tolerated as a miss
// (the synthesis is the deterministic fallback).
func (h *Handler) readLocalMemberNode(ctx context.Context, virtualKey, path string) (io.ReadSeekCloser, *metadata.Node, bool) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, nil, false
	}
	for _, m := range order {
		if m.Type != repo.TypeLocal {
			continue
		}
		rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, m.Key, path)
		if err == nil {
			return rc, node, true
		}
		if !errors.Is(err, repo.ErrNodeNotFound) {
			h.tolerateMemberFailure(ctx, virtualKey, m.Key, "+incompatible .mod probe", err)
		}
	}
	return nil, nil, false
}

// latestCandidate is one member's @latest answer.
type latestCandidate struct {
	version string
	body    []byte
}

// memberLatestCandidate computes one member's @latest candidate: a local
// member's best registered version (its .info, synthesized from the zip
// timestamp when absent — in memory only, the write-back belongs to the
// direct local face), a remote member's cached upstream @latest body.
func (h *Handler) memberLatestCandidate(ctx context.Context, virtualKey string, m repo.VirtualMember, module string) (latestCandidate, bool, error) {
	if m.Type == repo.TypeLocal {
		nodes, err := h.svc.ListVirtualMember(ctx, virtualKey, m.Key, module+segVersionMarker)
		if err != nil {
			return latestCandidate{}, false, err
		}
		var versions []string
		zips := map[string]*metadata.Node{}
		for _, n := range nodes {
			if v, ok := versionOfZipNode(n.Path); ok {
				versions = append(versions, v)
				zips[v] = n
			}
		}
		best, ok := pickLatest(versions)
		if !ok {
			return latestCandidate{}, false, nil
		}
		infoPath := module + segVersionMarker + best + ".info"
		if rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, m.Key, infoPath); err == nil {
			body, rerr := upstreamBody(rc)
			if rerr != nil {
				return latestCandidate{}, false, rerr
			}
			return latestCandidate{version: best, body: body}, true, nil
		} else if !errors.Is(err, repo.ErrNodeNotFound) {
			return latestCandidate{}, false, err
		}
		body, merr := json.Marshal(infoBody{Version: best, Time: firstNonEmptyRFC3339(zips[best].UpdatedAt, zips[best].CreatedAt)})
		if merr != nil {
			return latestCandidate{}, false, merr
		}
		return latestCandidate{version: best, body: body}, true, nil
	}
	rc, _, err := h.svc.ReadVirtualMember(ctx, virtualKey, m.Key, latestMarker(module))
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return latestCandidate{}, false, nil
		}
		return latestCandidate{}, false, err
	}
	body, rerr := upstreamBody(rc)
	if rerr != nil {
		return latestCandidate{}, false, rerr
	}
	var probe infoBody
	if jerr := json.Unmarshal(body, &probe); jerr != nil || probe.Version == "" {
		// An upstream body without a Version cannot compete in the global
		// order; treat it as no candidate.
		return latestCandidate{}, false, nil
	}
	return latestCandidate{version: probe.Version, body: body}, true, nil
}

// serveVirtualLatest collects the members' candidates, picks the global
// best by the section 4.5 order and serves that member's original body.
func (h *Handler) serveVirtualLatest(ctx context.Context, w http.ResponseWriter, virtualKey string, t target) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		h.writeError(w, err, virtualKey, t.module)
		return
	}
	var (
		best   latestCandidate
		winner string
	)
	for _, m := range order {
		c, ok, err := h.memberLatestCandidate(ctx, virtualKey, m, t.module)
		if err != nil {
			h.tolerateMemberFailure(ctx, virtualKey, m.Key, "latest aggregation", err)
			continue
		}
		if !ok {
			continue
		}
		if winner == "" || CompareVersions(c.version, best.version) > 0 {
			best, winner = c, m.Key
		}
	}
	if winner == "" {
		writePlain(w, http.StatusNotFound, msgNotFound)
		return
	}
	w.Header().Set("X-BinFlow-Resolved-From", winner)
	h.serveSynth(w, best.body, "application/json", digestTriple{})
}
