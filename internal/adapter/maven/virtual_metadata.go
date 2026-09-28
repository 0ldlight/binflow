package maven

// The virtual-repository maven-metadata.xml aggregation (T-72, FR-21-AC6 /
// ME-05's virtual face; maven-npm-pypi.md section 1.6 and repo-semantics
// section 8.3, high confidence): a GET of the standard metadata document
// through a VIRTUAL repository is intercepted BEFORE the first-hit download
// resolution and answered from an IN-MEMORY MERGE of the members' own
// documents, walked in the four-bucket seam's order (T-531: the walk
// consumes the Facet discriminant of design virtual-four-bucket.md §2.1 —
// cache-facet steps are skipped, virtual-resolution.md §5.1 "跳过所有 cache
// 仓": the remote 本体 step carries the cache semantics, one remote merges
// as ONE unit, never also its standing copy).
//
// Merge rules (the spec's four, plus the MNG-5180 rider):
//
//   - versions: deduplicated, re-sorted with the Maven version comparator;
//   - latest/release: recomputed off the sorted union (snapshots count for
//     latest, release is the last non-SNAPSHOT);
//   - the snapshot block: the member with the larger buildNumber wins
//     (timestamp breaks ties, then member order);
//   - snapshotVersions: merged per (extension x classifier) with the same
//     buildNumber precedence — the section of the merge Maven's own client
//     lacks (MNG-5180 drops it), which is exactly why the server side must
//     carry it.
//
// foundByPriority: once a PRIORITY-bucket member has produced a document,
// the non-priority bucket is not consulted at all (repo-semantics 8.3) —
// the marked member's numbering is authoritative for the whole GAV.
//
// Nothing is cached: every request recomputes off the members' current
// documents (the spec's "不缓存、每次现算"; members' own documents remain
// the T-68 calculator's business). A classified member failure (the remote
// engine's SSRF 400, hardFail 502) propagates verbatim — the "任一成员被
// block 则整体透传 block" rule; an unfound member (no document, negative
// cache, offline without a copy) just contributes nothing. A member whose
// document does not parse is skipped with a WARN: one corrupt member must
// not take the whole version index down.
//
// The merged response deliberately carries no X-BinFlow-Resolved-From: it
// is a virtual-level derivation, not any member's copy (the header stays
// the first-hit download face's answer).

import (
	"context"
	"crypto/md5"  //nolint:gosec // protocol digest of a served body, never a security primitive
	"crypto/sha1" //nolint:gosec // protocol digest of a served body, never a security primitive
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
)

// memberMetadataDoc is one member's parsed contribution.
type memberMetadataDoc struct {
	member string
	node   mavenNodeFacts
	doc    metadataXML
}

// mavenNodeFacts carries the serving node's timestamps (Last-Modified of
// the merged response is the newest contributor's).
type mavenNodeFacts struct {
	lastModified time.Time
}

// serveVirtualMetadata intercepts the aggregatable metadata family on a
// virtual repository: the standard maven-metadata.xml document and the
// checksum sidecars of it. It reports whether it consumed the request; the
// plugin-group variant and every other path fall back to the ordinary
// first-hit transfer plane.
func (h *Handler) serveVirtualMetadata(ctx context.Context, w http.ResponseWriter, r *http.Request,
	repoKey, relPath string, l Layout) bool {
	if l.Kind == KindMetadata && l.File == metadataFileName {
		docs, err := h.collectVirtualMetadata(ctx, repoKey, relPath)
		if err != nil {
			h.writeServiceError(w, err, r.Method, repoKey, relPath)
			return true
		}
		if len(docs) == 0 {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return true
		}
		body := renderVirtualMetadata(docs, l, r.UserAgent())
		h.writeDerivedMetadata(w, r, body, newestDocTime(docs))
		return true
	}
	if l.Kind == KindSidecar && l.TargetKind == KindMetadata {
		if _, file := splitDirFile(l.Target); file != metadataFileName {
			return false // the plugin-group variant's sidecar: first-hit
		}
		if l.Algo == "sha512" {
			// Same ruling as the local sidecar face: no honest computed
			// sha512 exists under the three-digest model.
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return true
		}
		docs, err := h.collectVirtualMetadata(ctx, repoKey, l.Target)
		if err != nil {
			h.writeServiceError(w, err, r.Method, repoKey, relPath)
			return true
		}
		if len(docs) == 0 {
			writeError(w, http.StatusNotFound, notFoundMessage(repoKey, relPath))
			return true
		}
		body := renderVirtualMetadata(docs, l, r.UserAgent())
		h.writeDerivedSidecar(w, r, body, l.Algo)
		return true
	}
	return false
}

// memberFacet is this face's mirror of the four-bucket seam's Facet
// discriminant (repo.FacetPlain / repo.FacetCache on repo.VirtualMember —
// T-530, design virtual-four-bucket.md §2.1). The mirror keeps the walk
// step self-contained for the pure traversal tests; the seam crossing is
// the explicit mapping in facetOfStep.
type memberFacet uint8

const (
	facetPlain memberFacet = iota
	facetCache
)

// facetOfStep maps the four-bucket seam's facet onto this face's mirror.
// Explicit cases, never a bare type conversion: an unknown future facet
// value degrades to plain WITH a WARN — the step then walks the FR-20
// body semantics (visible behavior, one read of the remote), never a
// silent numeric coincidence (fail-open guard, Reviewer B②).
func facetOfStep(ctx context.Context, m repo.VirtualMember) memberFacet {
	switch m.Facet {
	case repo.FacetPlain:
		return facetPlain
	case repo.FacetCache:
		return facetCache
	default:
		slog.WarnContext(ctx, "maven: unknown virtual member facet — treated as plain",
			slog.String("member", m.Key), slog.Uint64("facet", uint64(m.Facet)))
		return facetPlain
	}
}

// metadataLevel classifies the addressed metadata document for the §5.1
// traversal skips. The version directory's spelling decides (the same
// suffix heuristic mergeMetadataDocs splits the merge algorithm on): a
// -SNAPSHOT directory is the snapshot-level document; every other
// metadata document — the module version list — merges at module level.
type metadataLevel uint8

const (
	levelModule metadataLevel = iota
	levelSnapshot
)

// metadataLevelOf classifies a repository-relative metadata path.
func metadataLevelOf(path string) metadataLevel {
	dir, _ := splitDirFile(path)
	if strings.HasSuffix(dir, snapshotSuffix) {
		return levelSnapshot
	}
	return levelModule
}

// metadataWalkStep is one member step of the aggregation walk, in the
// shape the four-bucket seam delivers it (design §2.1's ResolutionStep),
// plus the maven policy flags the §5.1 level skips key on. Cache-facet
// steps carry the REMOTE's key and the lenient flag defaults — the facet
// check alone drops them before any flag is consulted, so no row read
// happens on their account (N3).
type metadataWalkStep struct {
	Key             string
	Facet           memberFacet
	Priority        bool
	HandleReleases  bool
	HandleSnapshots bool
}

// filterMetadataSteps applies the §5.1 traversal skips for one addressed
// level: a cache-facet step never participates (the remote 本体 step
// carries the cache semantics — one remote, one read, never the standing
// copy too), and a member whose policy refuses the level's class does not
// contribute (snapshot-level documents skip handleSnapshots=false members
// — §5.1 explicit; module-level lists skip handleReleases=false, §3.4's
// release skip — the adapter-side mirror of the walk layer's T-541
// implementation, same rule, same member-row source). The
// foundByPriority short-circuit is NOT applied here: it keys on document
// production, which only the walk observes.
func filterMetadataSteps(steps []metadataWalkStep, level metadataLevel) []metadataWalkStep {
	out := make([]metadataWalkStep, 0, len(steps))
	for _, s := range steps {
		if s.Facet == facetCache {
			continue
		}
		if level == levelSnapshot && !s.HandleSnapshots {
			continue
		}
		if level == levelModule && !s.HandleReleases {
			// T-531's drift point 1, still standing as a differential
			// candidate: §5.1 names only the snapshot-level skip — this
			// module-level branch is §3.4's mirror, implemented in the
			// walk layer too since T-541 (internal/repo getVirtual). A
			// differential refutation flips BOTH sites together, never
			// this branch alone.
			continue
		}
		out = append(out, s)
	}
	return out
}

// virtualMetadataSteps materializes the aggregation walk: the four-bucket
// member order with each step's facet (the facetOfStep seam mapping) and
// the maven policy flags read off the member's own row through the
// ClassReader config seam (the T-367 precedent: helm's memberContext
// reads a public canonical field the same way — handle* is routing data,
// not a protected field). A cache-facet step gets NO row read (N3): it
// never survives filterMetadataSteps — the facet alone drops it — and its
// key is the remote's, whose body step reads that same row once anyway.
// A member whose row read fails walks with the lenient defaults (both
// handles true): a flaky row read must not silently drop a member's
// contributions.
func (h *Handler) virtualMetadataSteps(ctx context.Context, virtualKey string) ([]metadataWalkStep, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	steps := make([]metadataWalkStep, 0, len(order))
	for _, m := range order {
		s := metadataWalkStep{
			Key:      m.Key,
			Facet:    facetOfStep(ctx, m),
			Priority: m.Priority,
			// lenient defaults; the row read below refines them
			HandleReleases:  true,
			HandleSnapshots: true,
		}
		if s.Facet == facetCache {
			steps = append(steps, s) // dropped by the facet check; no row read
			continue
		}
		if row, rerr := h.class.Get(ctx, m.Key); rerr == nil {
			rc := ParseRepoConfig(row.Config)
			s.HandleReleases, s.HandleSnapshots = rc.HandleReleases, rc.HandleSnapshots
		} else {
			slog.WarnContext(ctx, "maven: virtual member row unreadable — policy flags defaulted",
				slog.String("virtual", virtualKey), slog.String("member", m.Key),
				slog.String("error", rerr.Error()))
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// collectVirtualMetadata walks the filtered member order reading every
// step's copy of the addressed metadata path. The foundByPriority
// short-circuit stops the walk before the non-priority bucket once a
// marked member has produced a document.
func (h *Handler) collectVirtualMetadata(ctx context.Context, virtualKey, path string) ([]memberMetadataDoc, error) {
	steps, err := h.virtualMetadataSteps(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	steps = filterMetadataSteps(steps, metadataLevelOf(path))
	docs := make([]memberMetadataDoc, 0, len(steps))
	sawPriorityDoc := false
	for _, s := range steps {
		if !s.Priority && sawPriorityDoc {
			break // foundByPriority: the marked member's document is authoritative
		}
		rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, s.Key, path)
		if err != nil {
			if errors.Is(err, repo.ErrNodeNotFound) {
				continue // member has no document at this path
			}
			// A classified member failure renders verbatim (the block
			// pass-through); anything else is an honest 500.
			return nil, err
		}
		raw, rerr := io.ReadAll(io.LimitReader(rc, metadataReadLimit))
		_ = rc.Close() //nolint:errcheck // read-only fd; the bytes are already in hand
		if rerr != nil {
			slog.WarnContext(ctx, "maven: virtual member metadata unreadable — skipped",
				slog.String("virtual", virtualKey), slog.String("member", s.Key),
				slog.String("path", path), slog.String("error", rerr.Error()))
			continue
		}
		var doc metadataXML
		if uerr := xml.Unmarshal(raw, &doc); uerr != nil {
			slog.WarnContext(ctx, "maven: virtual member metadata unparseable — skipped",
				slog.String("virtual", virtualKey), slog.String("member", s.Key),
				slog.String("path", path), slog.String("error", uerr.Error()))
			continue
		}
		if s.Priority {
			sawPriorityDoc = true
		}
		docs = append(docs, memberMetadataDoc{member: s.Key, doc: doc, node: mavenNodeFacts{lastModified: nodeTime(node)}})
	}
	return docs, nil
}

// mergeMetadataDocs folds the member documents (two-bucket order) into one.
// The level split follows the calculator's own heuristic: the addressed
// directory ending in -SNAPSHOT is the version-level (snapshot) document,
// everything else merges as the module version list.
func mergeMetadataDocs(docs []memberMetadataDoc, l Layout) metadataXML {
	if len(docs) == 0 {
		return metadataXML{}
	}
	out := metadataXML{
		GroupID:    docs[0].doc.GroupID,
		ArtifactID: docs[0].doc.ArtifactID,
		Version:    docs[0].doc.Version,
	}
	if strings.HasSuffix(l.Module, snapshotSuffix) {
		out.Versioning = mergeSnapshotVersioning(docs)
		return out
	}
	out.Versioning = mergeModuleVersioning(docs)
	// The A-form tail on the merged module document too (L014-2 a3 wire):
	// `<version>` = the recomputed latest, snapshots included — not some
	// member's stale tail element.
	out.Version = out.Versioning.Latest
	return out
}

// mergeModuleVersioning merges version-group documents: the versions union
// in Maven order with latest/release recomputed and the newest lastUpdated.
func mergeModuleVersioning(docs []memberMetadataDoc) versioningXML {
	seen := map[string]bool{}
	versions := make([]string, 0, 8)
	for _, d := range docs {
		if d.doc.Versioning.Versions == nil {
			continue
		}
		for _, v := range d.doc.Versioning.Versions.Version {
			if !seen[v] {
				seen[v] = true
				versions = append(versions, v)
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool {
		return VersionComparator{}.CompareVersions(versions[i], versions[j]) < 0
	})
	out := versioningXML{LastUpdated: maxLastUpdated(docs)}
	if len(versions) > 0 {
		out.Latest = versions[len(versions)-1] // snapshots included (the calculator's own rule)
		out.Release = lastRelease(versions)
		out.Versions = &versionsXML{Version: versions}
	}
	return out
}

// mergeSnapshotVersioning merges SNAPSHOT version-directory documents: the
// snapshot block of the largest buildNumber, snapshotVersions per
// (extension x classifier) under the same precedence (MNG-5180: the merge
// Maven's client cannot do), the newest lastUpdated.
func mergeSnapshotVersioning(docs []memberMetadataDoc) versioningXML {
	out := versioningXML{LastUpdated: maxLastUpdated(docs)}
	bestBN := int64(-1)
	var best *snapshotXML
	type key struct{ ext, classifier string }
	entries := map[key]snapshotVersionXML{}
	entryBN := map[key]int64{}
	for _, d := range docs {
		bn := int64(0)
		if s := d.doc.Versioning.Snapshot; s != nil {
			bn = s.BuildNumber
			if best == nil || bn > bestBN || (bn == bestBN && s.Timestamp > best.Timestamp) {
				bnCopy := *s
				best, bestBN = &bnCopy, bn
			}
		}
		if sv := d.doc.Versioning.SnapshotVersions; sv != nil {
			for _, e := range sv.Entry {
				k := key{e.Extension, e.Classifier}
				if curBN, ok := entryBN[k]; !ok || bn > curBN {
					entries[k] = e
					entryBN[k] = bn
				}
			}
		}
	}
	if best != nil {
		out.Snapshot = best
	}
	if len(entries) > 0 {
		keys := make([]key, 0, len(entries))
		for k := range entries {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].ext != keys[j].ext {
				return keys[i].ext < keys[j].ext
			}
			return keys[i].classifier < keys[j].classifier
		})
		list := make([]snapshotVersionXML, 0, len(keys))
		for _, k := range keys {
			list = append(list, entries[k])
		}
		out.SnapshotVersions = &snapshotVersionsXML{Entry: list}
	}
	return out
}

// maxLastUpdated is the newest member lastUpdated (yyyyMMddHHmmss spellings
// compare chronologically as strings); the merge moment would churn every
// response, the members' own stamps keep the merged body stable per state.
func maxLastUpdated(docs []memberMetadataDoc) string {
	best := ""
	for _, d := range docs {
		if d.doc.Versioning.LastUpdated > best {
			best = d.doc.Versioning.LastUpdated
		}
	}
	return best
}

// javaAgentUA matches the bare java-agent User-Agent spellings
// ("Java/1.8.0_391", "java/17.0.2") — the [Jj]ava/(.+) full-match family.
var javaAgentUA = regexp.MustCompile(`^[Jj]ava/.+`)

// clientSupportsM3SnapshotVersions is the §5.1 merge rider's client-side
// predicate (virtual-resolution.md section 5.1 row 4: snapshotVersions —
// the v3 markers — merge only when the client declares M3 snapshot-marker
// support). Evidence: L030 case 1 (dual, A = 7.161.26) — a Maven 3 UA
// ("Apache-Maven/3.9.16 (Java …)") gets the merge, a bare java-agent UA
// ("Java/1.8.0_391", full-match of the reference's [Jj]ava/(.+) family)
// gets the document WITHOUT <snapshotVersions>; the report's advisory
// (decompile-anchored) adds the corners: an absent User-Agent defaults to
// capable, and Ivy/Wharf deployers are not.
func clientSupportsM3SnapshotVersions(ua string) bool {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return true
	}
	if javaAgentUA.MatchString(ua) {
		return false
	}
	// Ivy/Wharf product token: the name before the version slash
	// ("Apache Ivy/2.5.2", "Ivy/2.4.0", "Wharf/1.0").
	product := ua
	if i := strings.IndexByte(product, '/'); i >= 0 {
		product = product[:i]
	}
	product = strings.ToLower(strings.TrimSpace(product))
	return !strings.HasSuffix(product, "ivy") && !strings.HasSuffix(product, "wharf")
}

// isSnapshotLevelMetadata reports whether l addresses (or sidecars) the
// SNAPSHOT version-directory metadata document — the level the §5.1
// snapshotVersions rider keys on, under the same suffix heuristic the
// merge algorithm splits on.
func isSnapshotLevelMetadata(l Layout) bool {
	return strings.HasSuffix(l.Module, snapshotSuffix)
}

// renderVirtualMetadata merges the member documents and applies the §5.1
// M3 rider (T-542, BIN-16 / L030 case 1): a client the capability
// predicate rejects is served the snapshot-level merge WITHOUT
// <snapshotVersions> — the <snapshot> block survives, module-level
// documents pass untouched (they carry no snapshotVersions to begin with).
func renderVirtualMetadata(docs []memberMetadataDoc, l Layout, ua string) []byte {
	doc := mergeMetadataDocs(docs, l)
	if isSnapshotLevelMetadata(l) && !clientSupportsM3SnapshotVersions(ua) {
		doc.Versioning.SnapshotVersions = nil
	}
	return renderMetadata(doc)
}

// strippedSnapshotMetadata reads a LOCAL repository's SNAPSHOT version
// document and produces its §5.1 rider transform — the document without
// <snapshotVersions> — for a client the capability predicate rejects. It
// reports the derived body and the stored node's timestamp; a nil body means
// "not servable as derived — fall back to the verbatim transfer plane"
// (svc.Get error mapped by the caller, unreadable or unparseable document:
// the skip-not-fatal posture of the merge face).
func (h *Handler) strippedSnapshotMetadata(ctx context.Context, p *repo.Principal,
	repoKey, relPath string) ([]byte, time.Time) {
	rc, node, err := h.svc.Get(ctx, p, repoKey, relPath)
	if err != nil {
		return nil, time.Time{}
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, rerr := io.ReadAll(io.LimitReader(rc, metadataReadLimit))
	if rerr != nil {
		slog.WarnContext(ctx, "maven: member snapshot metadata unreadable — serving verbatim",
			slog.String("repo", repoKey), slog.String("path", relPath), slog.String("error", rerr.Error()))
		return nil, time.Time{}
	}
	var doc metadataXML
	if uerr := xml.Unmarshal(raw, &doc); uerr != nil {
		slog.WarnContext(ctx, "maven: member snapshot metadata unparseable — serving verbatim",
			slog.String("repo", repoKey), slog.String("path", relPath), slog.String("error", uerr.Error()))
		return nil, time.Time{}
	}
	doc.Versioning.SnapshotVersions = nil
	return renderMetadata(doc), nodeTime(node)
}

// serveSnapshotMetadataStripped serves a LOCAL repository's own SNAPSHOT
// version document with the <snapshotVersions> section removed — the member
// plane of the same §5.1 rider (T-542; L030 case 1's member control leg:
// the A side strips there too). It reports false to fall back to the
// verbatim transfer plane.
func (h *Handler) serveSnapshotMetadataStripped(ctx context.Context, w http.ResponseWriter,
	r *http.Request, p *repo.Principal, repoKey, relPath string) bool {
	body, lastMod := h.strippedSnapshotMetadata(ctx, p, repoKey, relPath)
	if body == nil {
		return false
	}
	h.writeDerivedMetadata(w, r, body, lastMod)
	return true
}

// writeDerivedMetadata serves a server-derived metadata body — the virtual
// merge, or a member document with a serving transform (T-542's strip) —
// with the transfer plane's header habits: the digests are computed over
// the DERIVED bytes (a client checksum-verification must hold against
// exactly what was served), ETag is the unquoted sha1, and If-None-Match
// short-circuits 304. No Range: the document is a small derivation, and
// advertising ranges it cannot honor would be the worse lie.
func (h *Handler) writeDerivedMetadata(w http.ResponseWriter, r *http.Request, body []byte, lastMod time.Time) {
	sums := digestsOfBody(body)
	hdr := w.Header()
	hdr.Set("Content-Type", metadataMime)
	hdr.Set(hdrChecksumSha256, sums.sha256)
	hdr.Set(hdrChecksumSha1, sums.sha1)
	hdr.Set(hdrChecksumMd5, sums.md5)
	hdr.Set("ETag", sums.sha1)
	if !lastMod.IsZero() {
		hdr.Set("Last-Modified", lastMod.UTC().Format(http.TimeFormat))
	}
	if evalConditional(r, sums.sha1, lastMod) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// writeDerivedSidecar serves the computed checksum of a DERIVED metadata
// document (the merged, or the stripped) — the same server-computed
// contract the local sidecar face upholds, with the derived body as its
// target.
func (h *Handler) writeDerivedSidecar(w http.ResponseWriter, r *http.Request, body []byte, algo string) {
	sums := digestsOfBody(body)
	digest := map[string]string{"sha256": sums.sha256, "sha1": sums.sha1, "md5": sums.md5}[algo]
	hdr := w.Header()
	hdr.Set("Content-Type", sidecarContentType)
	hdr.Set("Content-Length", strconv.Itoa(len(digest)))
	hdr.Set("ETag", digest)
	hdr.Set(hdrChecksumSha256, sums.sha256)
	hdr.Set(hdrChecksumSha1, sums.sha1)
	if evalConditional(r, digest, time.Time{}) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, digest)
}

// bodyDigests is the computed triple of a served body.
type bodyDigests struct{ sha256, sha1, md5 string }

// digestsOfBody computes the three protocol digests of body.
func digestsOfBody(body []byte) bodyDigests {
	s1 := sha1.Sum(body) //nolint:gosec // protocol digest of a served body, never a security primitive
	m5 := md5.Sum(body)  //nolint:gosec // protocol digest of a served body, never a security primitive
	return bodyDigests{sha256: sha256Hex(body), sha1: hex.EncodeToString(s1[:]), md5: hex.EncodeToString(m5[:])}
}

// newestDocTime is the newest contributing node timestamp (the merged
// document's Last-Modified).
func newestDocTime(docs []memberMetadataDoc) time.Time {
	var best time.Time
	for _, d := range docs {
		if d.node.lastModified.After(best) {
			best = d.node.lastModified
		}
	}
	return best
}

// splitDirFile splits a repository-relative path into its directory stack
// and final segment.
func splitDirFile(path string) (dir, file string) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}
