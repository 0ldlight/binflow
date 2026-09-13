package maven

// Server-side unique-snapshot rewriting (spec section 1.3; wire form pinned
// by the L013-4 matrix and the L014-2 live probes against artifactory-ux
// 7.161.20). A PUT whose file name still carries the -SNAPSHOT spelling
// lands under the timestamped {module}-{baseRev}-yyyyMMdd.HHmmss-N spelling
// instead, while the version DIRECTORY keeps the -SNAPSHOT name:
//
//   - the POM never joins the current trip (review-B F1, wire f1a/f1b/n6):
//     it opens the NEXT one — N = trip+1, the timestamp of a file already
//     at that N (completing a leader's trip in progress), else now. In a
//     pomless directory (no trip) it behaves as a follower;
//   - CLASSIFIED artifacts join the CURRENT trip: the pom-sourced trip,
//     else the first unique file of the listing, else (now, 1);
//   - the MAIN artifact (no classifier, not the pom) LEADS: it mints
//     (now, trip.buildNumber+1) — 1-based without any trip — EXCEPT when a
//     file of the same (extension, classifier) already sits at buildNumber
//     >= the candidate: re-deploying that coordinate overwrites its
//     existing unique spelling in place.
//
// Already-unique file names never adjust, whatever the behavior; non-unique
// and deployer repositories store the uploaded name (the caller checks the
// behavior before calling). The build.timestamp matrix property registers
// on the node but does NOT steer the timestamp — the reference minted
// server-now with the property present (L014-2 probe, spec section 1.3
// drift, reported to compatibility-engineering).
//
// Review L014-2 B1: the trip is DERIVED from the same ungated storage facts
// (NodeReader.ListByPrefix) the metadata generator itself reads — never via
// svc.Get, whose read grant would make a write-without-read deployer's
// numbering silently degrade to in-place overwrites of the previous build.
// Derivation equals the stored document's <snapshot> block whenever the
// document is server-computed (recalcVersionDir's head selection is the
// same pom-first/max rule and every unique landing recalculates
// synchronously), and it is never STALER than the stored document (the
// facts list includes files whose recalc is still in flight).

import (
	"context"
	"fmt"
	"strings"
)

// uniqueName renders the timestamped spelling of one snapshot fact.
func uniqueName(module, baseRev string, af artifactFile) string {
	clf := ""
	if af.classifier != "" {
		clf = "-" + af.classifier
	}
	return fmt.Sprintf("%s-%s-%s-%d%s.%s", module, baseRev, af.ts, af.buildNum, clf, af.ext)
}

// tripOf picks the directory's current (timestamp, buildNumber) trip off
// its file facts: the newest unique POM — the same head selection the
// reference's maven-metadata.xml carries (its version-dir document exists
// ONLY when a pom does, so pom-sourced is exactly the reference's metadata
// presence). nil in a pomless directory.
func tripOf(files []artifactFile) *artifactFile {
	return newestUnique(files, true)
}

// adjustUniqueSnapshot returns the storage file name for one -SNAPSHOT
// artifact PUT under snapshotVersionBehavior=unique, consulting the
// repository the write actually addresses (the ROUTED member under a
// virtual key — putFile's job). It is nil-safe: a handler without the
// calculator seam (nodes=nil) keeps the uploaded name — trip numbering is
// metadata machinery, disabled together.
func (c *calculator) adjustUniqueSnapshot(ctx context.Context, repoKey string, l Layout) string {
	af, files, ok := c.snapshotState(ctx, repoKey, l)
	if !ok {
		return l.File
	}
	now := c.now().Format("20060102.150405")
	trip := tripOf(files)
	if af.ext == "pom" && trip != nil {
		// Review-B F1 (wire f1a/f1b/n6): a pom NEVER joins the current
		// trip — it opens the NEXT one (N = trip+1), taking the timestamp
		// of a file already sitting at that N (completing a leader's trip
		// in progress) and server-now when N is untouched. The reference's
		// metadata requires a pom to exist, so a pom re-deploy always
		// advances the numbering.
		n := trip.buildNum + 1
		ts := ""
		for _, f := range files {
			if f.buildNum == n && f.ts > ts {
				ts = f.ts
			}
		}
		if ts == "" {
			ts = now
		}
		return uniqueName(l.Module, l.BaseRev, artifactFile{
			ext: af.ext, classifier: af.classifier, unique: true, ts: ts, buildNum: n,
		})
	}
	if af.classifier != "" || af.ext == "pom" {
		// Follower (classifier, or a pom in a pomless directory): join the
		// trip in progress — the pom-sourced trip, else the FIRST unique
		// file of the listing (probe n3: the low trip, not the max), else
		// (now, 1) in an empty directory.
		ts, n := now, int64(1)
		switch {
		case trip != nil:
			ts, n = trip.ts, trip.buildNum
		case len(files) > 0:
			ts, n = files[0].ts, files[0].buildNum
		}
		return uniqueName(l.Module, l.BaseRev, artifactFile{
			ext: af.ext, classifier: af.classifier, unique: true, ts: ts, buildNum: n,
		})
	}
	// Leader: a fresh trip unless this coordinate already sits at (or
	// beyond) the candidate — then overwrite it in place.
	cand := int64(1)
	if trip != nil {
		cand = trip.buildNum + 1
	}
	if cur := coordinateFile(files, af); cur != nil && cur.buildNum >= cand {
		return uniqueName(l.Module, l.BaseRev, *cur)
	}
	return uniqueName(l.Module, l.BaseRev, artifactFile{
		ext: af.ext, classifier: af.classifier, unique: true, ts: now, buildNum: cand,
	})
}

// adjustCompanionTarget names the artifact a -SNAPSHOT checksum companion
// registers against (spec section 1.3: the companion follows its main
// file's buildNumber): the EXISTING unique file of the same coordinate —
// the wire's companion 201 Location addresses the landed main file — or
// the trip spelling when no file of that coordinate exists (the
// target-must-exist check then answers the request).
func (c *calculator) adjustCompanionTarget(ctx context.Context, repoKey string, l Layout) string {
	af, files, ok := c.snapshotState(ctx, repoKey, l)
	if !ok {
		return l.File
	}
	if cur := coordinateFile(files, af); cur != nil {
		return uniqueName(l.Module, l.BaseRev, *cur)
	}
	ts, n := c.now().Format("20060102.150405"), int64(1)
	if trip := tripOf(files); trip != nil {
		ts, n = trip.ts, trip.buildNum
	}
	return uniqueName(l.Module, l.BaseRev, artifactFile{
		ext: af.ext, classifier: af.classifier, unique: true, ts: ts, buildNum: n,
	})
}

// snapshotState gathers the adjustment inputs; ok=false keeps the uploaded
// name (nil calculator/seam, non-snapshot or already-unique path, or a
// tail that does not decompose).
func (c *calculator) snapshotState(ctx context.Context, repoKey string, l Layout) (af artifactFile, files []artifactFile, ok bool) {
	if c == nil || c.nodes == nil || !l.Snapshot || l.Timestamped {
		return artifactFile{}, nil, false
	}
	af, ok = parseArtifactFile(l)
	if !ok {
		return artifactFile{}, nil, false
	}
	return af, c.snapshotDirFacts(ctx, repoKey, l), true
}

// snapshotDirFacts lists the unique-snapshot artifact facts directly inside
// the version directory (the adjustment's ungated fact source). A listing
// failure degrades to "no facts" — a (now, 1) mint then decides, never the
// error.
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
