package maven

// Server-side unique-snapshot rewriting (spec section 1.3; wire form pinned
// by the L013-4 matrix and the L014-2 live probes against artifactory-ux
// 7.161.20). A PUT whose file name still carries the -SNAPSHOT spelling
// lands under the timestamped {module}-{baseRev}-yyyyMMdd.HHmmss-N spelling
// instead, while the version DIRECTORY keeps the -SNAPSHOT name:
//
//   - FOLLOWER files — the pom and every classified artifact — join the
//     CURRENT trip: the stored maven-metadata.xml's <snapshot> block when
//     it carries a timestamp, else the newest unique file in the directory,
//     else (empty directory) they mint (now, 1);
//   - the MAIN artifact (no classifier, not the pom) LEADS: it mints
//     (now, buildNumber+1) — buildNumber from the stored metadata, 1-based
//     without any — EXCEPT when a file of the same (extension, classifier)
//     already sits at buildNumber >= the candidate: re-deploying that
//     coordinate overwrites its existing unique spelling in place.
//
// Already-unique file names never adjust, whatever the behavior; non-unique
// and deployer repositories store the uploaded name (the caller checks the
// behavior before calling). The build.timestamp matrix property registers
// on the node but does NOT steer the timestamp — the reference minted
// server-now with the property present (L014-2 probe, spec section 1.3
// drift, reported to compatibility-engineering).

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/lzwzzy/binflow/internal/repo"
)

// uniqueName renders the timestamped spelling of one snapshot fact.
func uniqueName(module, baseRev string, af artifactFile) string {
	clf := ""
	if af.classifier != "" {
		clf = "-" + af.classifier
	}
	return fmt.Sprintf("%s-%s-%s-%d%s.%s", module, baseRev, af.ts, af.buildNum, clf, af.ext)
}

// adjustUniqueSnapshot returns the storage file name for one -SNAPSHOT
// artifact PUT under snapshotVersionBehavior=unique. It is nil-safe: a
// handler without the calculator seam (nodes=nil) keeps the uploaded name —
// trip numbering is metadata machinery, disabled together.
func (c *calculator) adjustUniqueSnapshot(ctx context.Context, p *repo.Principal, repoKey string, l Layout) string {
	if af, files, tripTs, tripN, ok := c.snapshotState(ctx, p, repoKey, l); ok {
		now := c.now().Format("20060102.150405")
		if af.ext == "pom" || af.classifier != "" {
			// Follower: join the trip in progress.
			if tripTs == "" {
				if latest := newestUnique(files, false); latest != nil {
					return uniqueName(l.Module, l.BaseRev, artifactFile{
						ext: af.ext, classifier: af.classifier,
						unique: true, ts: latest.ts, buildNum: latest.buildNum,
					})
				}
				tripTs, tripN = now, 1
			}
			return uniqueName(l.Module, l.BaseRev, artifactFile{
				ext: af.ext, classifier: af.classifier, unique: true, ts: tripTs, buildNum: tripN,
			})
		}
		// Leader: a fresh trip unless this coordinate already sits at (or
		// beyond) the candidate — then overwrite it in place.
		cand := int64(1)
		if tripTs != "" {
			cand = tripN + 1
		}
		if cur := coordinateFile(files, af); cur != nil && cur.buildNum >= cand {
			return uniqueName(l.Module, l.BaseRev, *cur)
		}
		return uniqueName(l.Module, l.BaseRev, artifactFile{
			ext: af.ext, classifier: af.classifier, unique: true, ts: now, buildNum: cand,
		})
	}
	return l.File
}

// adjustCompanionTarget names the artifact a -SNAPSHOT checksum companion
// registers against (spec section 1.3: the companion follows its main
// file's buildNumber): the EXISTING unique file of the same coordinate —
// the wire's companion 201 Location addresses the landed main file — or
// the follower spelling when no file of that coordinate exists (the
// target-must-exist check then answers the request).
func (c *calculator) adjustCompanionTarget(ctx context.Context, p *repo.Principal, repoKey string, l Layout) string {
	if af, files, tripTs, tripN, ok := c.snapshotState(ctx, p, repoKey, l); ok {
		if cur := coordinateFile(files, af); cur != nil {
			return uniqueName(l.Module, l.BaseRev, *cur)
		}
		if tripTs == "" {
			if latest := newestUnique(files, false); latest != nil {
				tripTs, tripN = latest.ts, latest.buildNum
			} else {
				tripTs, tripN = c.now().Format("20060102.150405"), 1
			}
		}
		return uniqueName(l.Module, l.BaseRev, artifactFile{
			ext: af.ext, classifier: af.classifier, unique: true, ts: tripTs, buildNum: tripN,
		})
	}
	return l.File
}

// snapshotState gathers the adjustment inputs; ok=false keeps the uploaded
// name (nil calculator/seam, non-snapshot or already-unique path, or a
// tail that does not decompose).
func (c *calculator) snapshotState(ctx context.Context, p *repo.Principal, repoKey string, l Layout) (af artifactFile, files []artifactFile, tripTs string, tripN int64, ok bool) {
	if c == nil || c.nodes == nil || !l.Snapshot || l.Timestamped {
		return artifactFile{}, nil, "", 0, false
	}
	af, ok = parseArtifactFile(l)
	if !ok {
		return artifactFile{}, nil, "", 0, false
	}
	files = c.snapshotDirFacts(ctx, repoKey, l)
	tripTs, tripN = c.storedSnapshotTrip(ctx, p, repoKey, l)
	return af, files, tripTs, tripN, true
}

// snapshotDirFacts lists the unique-snapshot artifact facts directly inside
// the PUT's version directory (the adjustment's file-state fallback). A
// listing failure degrades to "no facts" — the metadata trip or (now, 1)
// then decides, never the error.
func (c *calculator) snapshotDirFacts(ctx context.Context, repoKey string, l Layout) []artifactFile {
	prefix := strings.ReplaceAll(l.OrgPath, ".", "/") + "/" + l.Module + "/" + l.VersionDir
	nodes, err := c.nodes.ListByPrefix(ctx, repoKey, prefix)
	if err != nil {
		return nil
	}
	files := make([]artifactFile, 0, len(nodes))
	for _, node := range nodes {
		if strings.HasSuffix(node.Path, "/") {
			continue
		}
		rel := strings.TrimPrefix(node.Path, prefix+"/")
		if rel == "" || strings.Contains(rel, "/") || metadataFileNames[rel] {
			continue
		}
		parsed, perr := Parse(node.Path)
		if perr != nil || parsed.Kind != KindArtifact || !parsed.Timestamped {
			continue
		}
		if af, ok := parseArtifactFile(parsed); ok {
			files = append(files, af)
		}
	}
	return files
}

// storedSnapshotTrip reads the version directory's stored maven-metadata.xml
// and returns its <snapshot> (timestamp, buildNumber) — ("", 0) when there
// is no document, no snapshot block or no timestamp (a non-unique-typed
// block carries no trip to join). Any read failure counts as no trip.
func (c *calculator) storedSnapshotTrip(ctx context.Context, p *repo.Principal, repoKey string, l Layout) (string, int64) {
	metaPath := strings.ReplaceAll(l.OrgPath, ".", "/") + "/" + l.Module + "/" + l.VersionDir + "/" + metadataFileName
	rc, _, err := c.svc.Get(ctx, p, repoKey, metaPath)
	if err != nil {
		return "", 0
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, metadataReadLimit))
	if err != nil {
		return "", 0
	}
	var doc metadataXML
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return "", 0
	}
	if s := doc.Versioning.Snapshot; s != nil && s.Timestamp != "" {
		return s.Timestamp, s.BuildNumber
	}
	return "", 0
}

// coordinateFile picks the newest unique file of the same (extension,
// classifier) as af — the coordinate a leader PUT would overwrite.
func coordinateFile(files []artifactFile, af artifactFile) *artifactFile {
	var best *artifactFile
	for i := range files {
		f := &files[i]
		if f.ext != af.ext || f.classifier != af.classifier {
			continue
		}
		if best == nil || f.buildNum > best.buildNum || (f.buildNum == best.buildNum && f.ts > best.ts) {
			best = f
		}
	}
	return best
}
