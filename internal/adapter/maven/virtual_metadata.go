package maven

// The virtual-repository maven-metadata.xml aggregation (T-72, FR-21-AC6 /
// ME-05's virtual face; maven-npm-pypi.md section 1.6 and repo-semantics
// section 8.3, high confidence): a GET of the standard metadata document
// through a VIRTUAL repository is intercepted BEFORE the first-hit download
// resolution and answered from an IN-MEMORY MERGE of the members' own
// documents, walked in the two-bucket order.
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
		body := renderMetadata(mergeMetadataDocs(docs, l))
		h.writeMergedMetadata(w, r, body, newestDocTime(docs))
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
		body := renderMetadata(mergeMetadataDocs(docs, l))
		h.writeMergedSidecar(w, r, body, l.Algo)
		return true
	}
	return false
}

// collectVirtualMetadata walks the two-bucket order reading every member's
// copy of the addressed metadata path. The foundByPriority short-circuit
// stops the walk before the non-priority bucket once a marked member has
// produced a document.
func (h *Handler) collectVirtualMetadata(ctx context.Context, virtualKey, path string) ([]memberMetadataDoc, error) {
	order, err := h.svc.VirtualMemberOrder(ctx, virtualKey)
	if err != nil {
		return nil, err
	}
	docs := make([]memberMetadataDoc, 0, len(order))
	sawPriorityDoc := false
	for _, m := range order {
		if !m.Priority && sawPriorityDoc {
			break // foundByPriority: the marked member's document is authoritative
		}
		rc, node, err := h.svc.ReadVirtualMember(ctx, virtualKey, m.Key, path)
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
				slog.String("virtual", virtualKey), slog.String("member", m.Key),
				slog.String("path", path), slog.String("error", rerr.Error()))
			continue
		}
		var doc metadataXML
		if uerr := xml.Unmarshal(raw, &doc); uerr != nil {
			slog.WarnContext(ctx, "maven: virtual member metadata unparseable — skipped",
				slog.String("virtual", virtualKey), slog.String("member", m.Key),
				slog.String("path", path), slog.String("error", uerr.Error()))
			continue
		}
		if m.Priority {
			sawPriorityDoc = true
		}
		docs = append(docs, memberMetadataDoc{member: m.Key, doc: doc, node: mavenNodeFacts{lastModified: nodeTime(node)}})
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

// writeMergedMetadata serves the merged document with the transfer plane's
// header habits: the digests are computed over the MERGED bytes (a client
// checksum-verification must hold against exactly what was served), ETag is
// the unquoted sha1, and If-None-Match short-circuits 304. No Range: the
// document is a small derivation, and advertising ranges it cannot honor
// would be the worse lie.
func (h *Handler) writeMergedMetadata(w http.ResponseWriter, r *http.Request, body []byte, lastMod time.Time) {
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

// writeMergedSidecar serves the computed checksum of the MERGED document —
// the same server-computed contract the local sidecar face upholds, with
// the merged body as its target.
func (h *Handler) writeMergedSidecar(w http.ResponseWriter, r *http.Request, body []byte, algo string) {
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
