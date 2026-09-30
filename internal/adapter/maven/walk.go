package maven

// The plain-SNAPSHOT walk resolve (T-562 / BIN-44, contracts
// maven/plain-snapshot-unique-walk-resolve, maven/non-snapshot-spelling-
// get-gate-404 and maven/virtual-plain-walk-cross-member-selection — the
// L035 A-oracle walk family, live 7.161.26): a GET/HEAD of a plain
// -SNAPSHOT file name whose version directory holds timestamped-spelling
// candidates of the same (artifact, baseRev, classifier, extension) family
// answers the SELECTED candidate's entity — bytes, validators
// (ETag/Content-Length), Range slices and checksum sidecars all address
// the RESOLVED target, never the requested spelling.
//
// Two selection layers, two different keys (L035 W2 vs W4 — do not mix):
//   - member-internal: max(filename ts, buildNumber) — the fixed-width ts
//     spelling compares chronologically as a string, buildNumber compares
//     NUMERICALLY ("10" beats "9"), a ts tie breaks on buildNumber;
//     upload order and storage mtime do not participate (W2 s1/s2/s3);
//   - cross-member (virtual): the candidate's storage mtime — the last
//     landed candidate wins; member declaration order and the filename
//     keys do NOT participate (W4 v2 refuted global filename-ts, v3
//     refuted declaration order).
//
// The walk only ever answers a storage MISS of the plain spelling (a
// stored plain file keeps its ordinary-plane service, the T-559 storage
// face), the version-level maven-metadata.xml never walks (t7), and a
// directory without a same-family candidate keeps the ordinary 404 (t9 —
// no cross-extension fallback).

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// isPlainSnapshotArtifact reports the walk's trigger face (W1 arm 1): an
// artifact request whose file name carries the plain -SNAPSHOT spelling.
func isPlainSnapshotArtifact(l Layout) bool {
	return l.Kind == KindArtifact && l.Snapshot && !l.Timestamped
}

// versionDirPrefix renders l's version directory as a node-listing prefix
// (groupId path / module / versionDir) — the directory the walk and the
// calculator's snapshot arithmetic both read.
func versionDirPrefix(l Layout) string {
	return strings.ReplaceAll(l.OrgPath, ".", "/") + "/" + l.Module + "/" + l.VersionDir
}

// walkCandidate is one timestamped-spelling candidate of the requested
// family inside one version directory.
type walkCandidate struct {
	path     string    // repo-relative candidate path
	ts       string    // filename timestamp, yyyyMMdd.HHmmss
	buildNum int64     // filename build number
	mtime    time.Time // the node's storage stamp (the W4 cross-member key)
}

// walkCandidates filters one version-directory listing down to the
// requested family's timestamped candidates. The layout parse already pins
// artifact and baseRev to the directory spelling, so the four-tuple match
// reduces to (extension, classifier) here (W2 s4/s5: extension and
// classifier families resolve independently).
func walkCandidates(nodes []*metadata.Node, prefix string, want artifactFile) []walkCandidate {
	out := make([]walkCandidate, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || strings.HasSuffix(n.Path, "/") {
			continue // folder row
		}
		rel := strings.TrimPrefix(n.Path, prefix+"/")
		if rel == "" || strings.Contains(rel, "/") || metadataFileNames[rel] {
			continue // only artifact files directly inside the version directory
		}
		l, err := Parse(n.Path)
		if err != nil || l.Kind != KindArtifact || !l.Timestamped {
			continue
		}
		af, ok := parseArtifactFile(l)
		if !ok || af.ext != want.ext || af.classifier != want.classifier {
			continue
		}
		out = append(out, walkCandidate{path: n.Path, ts: af.ts, buildNum: af.buildNum, mtime: nodeTime(n)})
	}
	return out
}

// selectWalkCandidate is the W2 member-internal rule: max(filename ts,
// buildNumber). A ts tie breaks on buildNumber numerically — never
// lexicographically and never by upload order (s2b uploaded 2 then 1 and
// 2 still won).
func selectWalkCandidate(cands []walkCandidate) *walkCandidate {
	var best *walkCandidate
	for i := range cands {
		c := &cands[i]
		if best == nil || c.ts > best.ts || (c.ts == best.ts && c.buildNum > best.buildNum) {
			best = c
		}
	}
	return best
}

// servePlainWalk intercepts the plain-spelling artifact family on a LOCAL
// or VIRTUAL repository and answers the walk resolve when the plain
// spelling is a storage miss. It reports whether it wrote the response;
// false always falls through to the ordinary transfer plane (which renders
// the miss's 404 — t9's no-candidate arm lands there unchanged).
func (h *Handler) servePlainWalk(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, relPath string, l Layout, rowType string) bool {
	if h.nodes == nil {
		return false // the facts seam is unwired: no walk without it
	}
	tl := l
	target := relPath
	switch {
	case isPlainSnapshotArtifact(l):
	case l.Kind == KindSidecar && l.TargetKind == KindArtifact:
		// The checksum sidecar walks too (t5/t6): the sidecar body is the
		// RESOLVED entity's digest.
		var err error
		tl, err = Parse(l.Target)
		if err != nil || !isPlainSnapshotArtifact(tl) {
			return false
		}
		target = l.Target
	default:
		return false // metadata documents read directly (t7); other families keep their plane
	}
	if rowType != repo.TypeLocal && rowType != repo.TypeVirtual {
		return false // remote repositories keep the pull-through plane (the remote walk face is unobserved)
	}
	// Only a storage miss walks: a stored plain spelling (the non-unique
	// home's verbatim landing) keeps its ordinary-plane service.
	rc, _, err := h.svc.Get(ctx, p, repoKey, target)
	if err == nil {
		_ = rc.Close() //nolint:errcheck // read-only fd; existence was the only question
		return false
	}
	if !errors.Is(err, repo.ErrNodeNotFound) {
		return false // authorization and classified failures render on the ordinary plane
	}
	if rowType == repo.TypeVirtual {
		return h.serveVirtualWalk(ctx, w, r, repoKey, l, tl)
	}

	want, ok := parseArtifactFile(tl)
	if !ok {
		return false
	}
	prefix := versionDirPrefix(tl)
	nodes, lerr := h.nodes.ListByPrefix(ctx, repoKey, prefix)
	if lerr != nil {
		return false
	}
	cand := selectWalkCandidate(walkCandidates(nodes, prefix, want))
	if cand == nil {
		return false
	}
	if l.Kind == KindSidecar {
		h.serveSidecarOfPath(ctx, w, r, p, repoKey, cand.path, l.Algo, rowType, false)
		return true
	}
	h.serveFile(ctx, w, r, p, repoKey, cand.path)
	return true
}

// serveVirtualWalk is the W4 virtual face: every walking member resolves
// its own candidate under the W2 rule, then the cross-member pick uses the
// candidates' STORAGE mtime — the last landed candidate wins (declaration
// order and filename (ts, bn) do not participate). A storage-stamp tie
// resolves to the later member in walk order: the store's stamps are
// second-granular, and the honest same-second tie key is unobserved on A
// (the contract's unobserved arm) — the later walk position is the only
// deterministic proxy for "landed last" a tie still carries.
func (h *Handler) serveVirtualWalk(ctx context.Context, w http.ResponseWriter, r *http.Request,
	virtualKey string, l, tl Layout) bool {
	want, ok := parseArtifactFile(tl)
	if !ok {
		return false
	}
	steps, err := h.virtualMetadataSteps(ctx, virtualKey)
	if err != nil {
		return false // the ordinary plane renders the failure
	}
	// Cache facets and snapshot-refusing members never walk the snapshot
	// plane (§3.6 — the same skips the metadata aggregation and the
	// repo-layer virtual resolution apply).
	steps = filterMetadataSteps(steps, levelSnapshot)
	prefix := versionDirPrefix(tl)
	var best *walkCandidate
	var bestMember string
	for _, s := range steps {
		nodes, lerr := h.svc.ListVirtualMember(ctx, virtualKey, s.Key, prefix)
		if lerr != nil {
			// Remote members refuse the facts listing (their aggregated
			// state is an upstream document) — they contribute no
			// candidates; the remote walk face itself is unobserved (L035
			// NOT_RUN list).
			continue
		}
		cand := selectWalkCandidate(walkCandidates(nodes, prefix, want))
		if cand == nil {
			continue
		}
		if best == nil || !cand.mtime.Before(best.mtime) {
			best, bestMember = cand, s.Key
		}
	}
	if best == nil {
		return false
	}
	rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, bestMember, best.path)
	if err != nil {
		return false // the ordinary plane renders the miss
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	if l.Kind == KindSidecar {
		applyReaderHints(w, rc) // the sidecar leg bypasses serveNode — apply the member stream's hints here
		h.writeSidecarDigest(ctx, w, r, node, l.Algo, virtualKey, best.path)
		return true
	}
	h.serveNode(w, r, rc, node, best.path) // serveNode applies the reader hints itself (no double apply)
	return true
}

// serveSidecarOfPath answers the sidecar face with the digest of the
// artifact at path — the walk's sidecar leg (t5/t6: the digest of the
// RESOLVED entity) and the direct face's exit. Since BIN-76 / T-594 every
// error rendering on this face points at the SOURCE (path) — never the
// .md5/.sha1 terminal spelling. The miss WORDING is per-face (the A wire's
// own families): the local plane keeps the ordinary `File not found.;`
// download miss, the virtual plane answers its resolution family
// `Could not find resource; Path:` (L039 Arm 1 / L040 c2-v, live
// re-pinned T-594). overlayClient (the direct face only) echoes the
// STORED client declaration first when one exists (ADR-0052 decision 4:
// the overlay mechanism is repo.OriginalChecksums); the walk legs pass
// false — their contracts pin the resolved entity's computed digest.
func (h *Handler) serveSidecarOfPath(ctx context.Context, w http.ResponseWriter, r *http.Request,
	p *repo.Principal, repoKey, path, algo, rowType string, overlayClient bool) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, path)
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) && rowType == repo.TypeVirtual {
			// BIN-76 / T-594: the virtual read plane's miss wording, A
			// verbatim — the source-citing resolution family, not the
			// ordinary download miss.
			writeError(w, http.StatusNotFound,
				fmt.Sprintf("Could not find resource; Path: '%s:%s'", repoKey, path))
			return
		}
		h.writeServiceError(w, err, r.Method, repoKey, path)
		return
	}
	applyReaderHints(w, rc)
	_ = rc.Close() //nolint:errcheck // read-only fd; the digest comes from the ledger
	if overlayClient && algo != "sha512" {
		if value := clientChecksumValueOf(node, algo); value != "" {
			h.writeSidecarBody(w, r, value, node)
			return
		}
		// The on-demand computation matrix is sha256-ONLY (BIN-76 / T-594,
		// ledger maven/sidecar-get-ondemand-matrix, L041 Arm 1
		// m1-get-md5-unset): on the overlay-armed face (the client-policy
		// local plane and the virtual read plane, T-587) an unset md5/sha1
		// answers the checksum family's own 404 citing the SOURCE — no
		// computed fallback, A never generates those digests on demand.
		// The faces OUTSIDE this branch keep the computed answer: the
		// server-generated-checksums plane (L039 Arm 6: A serves the
		// computed md5 under srvgen whatever was declared — the overlay
		// gate's ADR-0052 6.2 posture), the metadata targets (the derived
		// document contract owns their digests) and the walk legs (their
		// contracts pin the resolved entity's computed digest).
		if algo != "sha256" {
			writeError(w, http.StatusNotFound, fmt.Sprintf("Checksum not found for %s", path))
			return
		}
	}
	h.writeSidecarDigest(ctx, w, r, node, algo, repoKey, path)
}
