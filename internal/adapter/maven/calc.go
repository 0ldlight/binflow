package maven

// The maven-metadata.xml calculator (FR-17 / ME-04..07, T-68). The server
// owns three observable behaviors over the metadata family:
//
//   - a TRIGGER TAXONOMY (spec section 1.4, high confidence): unique
//     snapshot files and non-unique poms recalculate their parent VERSION
//     directory synchronously (the response blocks on it — subsequent
//     snapshot numbering reads it); every other artifact does so
//     asynchronously; a pom additionally recalculates the grandparent
//     (module) directory asynchronously and non-recursively; deletes
//     recalculate the affected directory trees of both sides
//     asynchronously; a client maven-metadata.xml PUT is accepted on the
//     generic upload chain and triggers the affected directory's
//     recalculation (v1.1: the authoritative content is always recomputed
//     from server storage facts, which is what makes concurrent deploys
//     merge-equivalent instead of last-writer-wins).
//
//   - two generators with spec-fixed content: the version-GROUP (module)
//     document (versions = pom-bearing subdirectories, Maven order,
//     latest/release/lastUpdated) and the SNAPSHOT version-directory
//     document (buildNumber/timestamp from the newest unique snapshot
//     pom, snapshotVersions per extension x classifier).
//
//   - a DELETION rule: a recalculated directory with no pom left loses
//     its metadata document and checksum sidecars — except when the
//     existing document is snapshot-typed in a non-snapshot directory
//     (RTFACT-6242: keep content a Maven 2 client manually deployed
//     there).
//
// Concurrency contract (FR-17-AC5): recalcs of the same directory never
// interleave — one process-wide execution lock orders them, and every
// recalc lists storage facts INSIDE the lock, so the last recalc's write
// reflects every deploy that completed before its trigger. Deploys
// landing after a recalc's trigger enqueue their own; the system
// converges with no lost versions and no 5xx (calculator failures are
// logged, never surfaced on the content plane).

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// snapshotSuffix is the version-directory spelling of a snapshot.
const snapshotSuffix = "-SNAPSHOT"

// metadataFileName is the standard document the calculator owns; the
// plugin-group variant (metadata-maven-metadata.xml) has no server-side
// generator (nothing derives its content from storage facts), so PUTs of
// it store as-is with no trigger.
const metadataFileName = "maven-metadata.xml"

// metadataMime is the stored Content-Type of a computed metadata document
// (the .xml entry of the adapter's deterministic table).
const metadataMime = "application/xml"

// metadataReadLimit bounds the RTFACT-6242 content probe (a metadata
// document is a few KB; a hand-migrated node beyond this is treated as
// unreadable, i.e. kept — the fail-safe direction of the guard).
const metadataReadLimit = 1 << 20

// asyncRecalcTimeout bounds one asynchronous recalculation.
const asyncRecalcTimeout = 30 * time.Second

// snapshotFileRevRegExp captures the unique-snapshot integration revision
// (yyyyMMdd.HHmmss-N) at the head of a file name's post-version tail.
var snapshotFileRevRegExp = regexp.MustCompile(`^([0-9]{8}\.[0-9]{6})-([0-9]+)`)

// metadataSidecars are the checksum companions removed together with a
// metadata document. sha512 companions are a P2 follow-up (PRD FR-17
// note); .sha256 joins the two spec-named algorithms because it is part
// of BinFlow's computed set.
var metadataSidecars = []string{".sha1", ".md5", ".sha256"}

// calculator is the maven-metadata recalculation engine. It is owned by
// the Handler and safe for concurrent use.
type calculator struct {
	svc   repo.Service
	nodes NodeReader
	now   func() time.Time

	// exec serializes recalculation EXECUTIONS process-wide (the sync
	// deploy-path recalcs and the asynchronous ones share it): two recalcs
	// of the same directory must never interleave a stale listing's write
	// after a fresher one's. A single lock (not per-directory maps) keeps
	// the ordering argument local — recalcs are short (one prefix listing
	// plus one small write) and deploy frequencies never contend.
	exec sync.Mutex
	// wg tracks in-flight ASYNCHRONOUS recalcs; the test harness drains it
	// for deterministic assertions.
	wg sync.WaitGroup
}

// newCalculator wires the engine. now may be nil (UTC time.Now).
func newCalculator(svc repo.Service, nodes NodeReader, now func() time.Time) *calculator {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &calculator{svc: svc, nodes: nodes, now: now}
}

// trigger addresses one directory whose metadata must be recomputed.
type trigger struct {
	repoKey string
	orgPath string // dotted groupId ("com.acme")
	module  string // artifactId directory
	version string // version directory; "" addresses the module directory
}

// dirPath renders the trigger's directory as a repository-relative path.
func (t trigger) dirPath() string {
	p := strings.ReplaceAll(t.orgPath, ".", "/")
	if p != "" {
		p += "/"
	}
	p += t.module
	if t.version != "" {
		p += "/" + t.version
	}
	return p
}

// metadataPath is the maven-metadata.xml node inside the directory.
func (t trigger) metadataPath() string { return t.dirPath() + "/" + metadataFileName }

// isSnapshotDir reports whether the addressed directory itself carries the
// -SNAPSHOT spelling (the version-dir generator's precondition).
func (t trigger) isSnapshotDir() bool {
	return t.version != "" && strings.HasSuffix(t.version, snapshotSuffix)
}

// stamp renders the calculator's UTC moment in the metadata timestamp
// format (yyyyMMddHHmmss, the spec's lastUpdated spelling).
func (c *calculator) stamp() string { return c.now().Format("20060102150405") }

// recalcSync runs one recalculation on the caller's goroutine, logging
// failures (a calculator fault must never fail the content plane: the
// artifact is already stored, and the next trigger self-heals).
func (c *calculator) recalcSync(ctx context.Context, p *repo.Principal, t trigger) {
	if c == nil || c.nodes == nil {
		return
	}
	c.exec.Lock()
	defer c.exec.Unlock()
	var err error
	if t.version == "" {
		err = c.recalcModule(ctx, p, t)
	} else {
		err = c.recalcVersionDir(ctx, p, t)
	}
	if err != nil {
		slog.ErrorContext(ctx, "maven: metadata recalculation failed",
			slog.String("repo", t.repoKey), slog.String("dir", t.dirPath()),
			slog.String("error", err.Error()))
	}
}

// recalcAsync runs one recalculation off the request path. The trigger
// principal is captured by value (the request context dies with the
// response); a fresh bounded context carries the work.
func (c *calculator) recalcAsync(p *repo.Principal, t trigger) {
	if c == nil || c.nodes == nil {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), asyncRecalcTimeout)
		defer cancel()
		c.recalcSync(ctx, p, t)
	}()
}

// ---- trigger taxonomy (the handler-facing entry points) ----

// afterArtifactDeploy fires ME-04's taxonomy for one landed artifact. The
// parent (version) directory is synchronous for unique snapshot files and
// non-unique poms — the response blocks until the version document exists,
// because subsequent snapshot numbering reads it — and asynchronous for
// every other artifact; a pom additionally recalculates the grandparent
// module directory (the version list), asynchronously and non-recursively.
func (c *calculator) afterArtifactDeploy(ctx context.Context, p *repo.Principal, repoKey string, l Layout) {
	if c == nil {
		return
	}
	pom := isPomFile(l.File)
	version := trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module, version: l.VersionDir}
	if l.Timestamped || (pom && l.Snapshot) {
		c.recalcSync(ctx, p, version)
	} else {
		c.recalcAsync(p, version)
	}
	if pom {
		c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module})
	}
}

// afterMetadataDeploy fires the client-metadata PUT trigger (v1.1): the
// stored client document is never the version list's source — the affected
// directory is recomputed from storage facts, synchronously, so the 201
// the client reads back already reflects the authoritative content (the
// equivalent-merge semantics FR-17-AC5 asserts).
//
// The affected directory of `X/maven-metadata.xml` is X. A -SNAPSHOT
// spelling addresses the version document; anything else is recalculated
// as a module (version-group) directory — a release version directory has
// no generator, and the group recalculation's no-pom cleanup governs a
// manually placed document there (ruling: the spec defines content rules
// for version-group and SNAPSHOT directories only).
func (c *calculator) afterMetadataDeploy(ctx context.Context, p *repo.Principal, repoKey string, l Layout) {
	if c == nil || l.File != metadataFileName {
		return
	}
	if strings.HasSuffix(l.Module, snapshotSuffix) {
		org, module := splitDottedLast(l.OrgPath)
		c.recalcSync(ctx, p, trigger{repoKey: repoKey, orgPath: org, module: module, version: l.Module})
		return
	}
	c.recalcSync(ctx, p, trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module})
}

// afterDelete fires ME-07: the source side's affected directory tree,
// asynchronously. An artifact's deletion touches its version directory and
// the module version list; a metadata deletion touches the document's own
// directory (the recalculation regenerates it when the facts still
// warrant one — the same reason a manual delete cannot keep a module
// document down while its poms exist).
func (c *calculator) afterDelete(p *repo.Principal, repoKey string, l Layout) {
	if c == nil {
		return
	}
	switch l.Kind {
	case KindArtifact:
		c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module, version: l.VersionDir})
		c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module})
	case KindMetadata:
		if l.File != metadataFileName {
			return
		}
		if strings.HasSuffix(l.Module, snapshotSuffix) {
			org, module := splitDottedLast(l.OrgPath)
			c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: org, module: module, version: l.Module})
			c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: org, module: module})
			return
		}
		c.recalcAsync(p, trigger{repoKey: repoKey, orgPath: l.OrgPath, module: l.Module})
	}
}

// ---- version-group (module) generator ----

// recalcModule rebuilds `{orgPath}/{module}/maven-metadata.xml`:
//
//	versions  = child directories that contain at least one *.pom
//	            (checksum sidecars of a pom do not count), sorted with the
//	            Maven version comparator;
//	latest    = the sorted last element (snapshots included);
//	release   = the last non-SNAPSHOT element (omitted when every version
//	            is a snapshot);
//	lastUpdated = the recalculation moment (yyyyMMddHHmmss UTC).
//
// No pom-bearing child → the document and its checksum sidecars are
// removed (RTFACT-6242 guard below).
func (c *calculator) recalcModule(ctx context.Context, p *repo.Principal, t trigger) error {
	prefix := t.dirPath()
	nodes, err := c.nodes.ListByPrefix(ctx, t.repoKey, prefix)
	if err != nil {
		return fmt.Errorf("list %s/%s: %w", t.repoKey, prefix, err)
	}
	versions := map[string]bool{}
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue // folder row
		}
		rel := strings.TrimPrefix(n.Path, prefix+"/")
		child, file, found := strings.Cut(rel, "/")
		if !found || child == "" || strings.Contains(file, "/") {
			continue // only a pom DIRECTLY inside a child version directory counts
		}
		if isPomFile(file) {
			versions[child] = true
		}
	}
	if len(versions) == 0 {
		return c.removeMetadata(ctx, p, t)
	}
	vs := make([]string, 0, len(versions))
	for v := range versions {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool {
		return VersionComparator{}.CompareVersions(vs[i], vs[j]) < 0
	})
	doc := metadataXML{
		GroupID:    t.orgPath,
		ArtifactID: t.module,
		Versioning: versioningXML{
			Latest:      vs[len(vs)-1],
			Release:     lastRelease(vs),
			LastUpdated: c.stamp(),
			Versions:    &versionsXML{Version: vs},
		},
	}
	return c.writeMetadata(ctx, p, t, renderMetadata(doc))
}

// lastRelease returns the last non-SNAPSHOT version ("" when none).
func lastRelease(vs []string) string {
	for i := len(vs) - 1; i >= 0; i-- {
		if !strings.HasSuffix(vs[i], snapshotSuffix) {
			return vs[i]
		}
	}
	return ""
}

// ---- SNAPSHOT version-directory generator ----

// recalcVersionDir rebuilds `{orgPath}/{module}/{version}/maven-metadata.xml`.
// The SNAPSHOT spelling is the spec's generator (buildNumber/timestamp
// from the newest unique snapshot pom — buildNumber numeric compare —
// fixed 1 without a timestamp when no unique pom exists; snapshotVersions
// per extension x classifier, newest entry each; `<updated>` of an entry
// is its timestamp with the dot removed, a non-unique entry carries the
// recalculation stamp).
//
// groupId/artifactId are derived from the DIRECTORY (ruling, see
// parseArtifactFile: under maven-2-default the directory spelling IS the
// coordinate — the layout validated the file names against it at upload —
// and a pom whose embedded coordinates disagree with its path is a client
// error BinFlow does not launder into metadata).
//
// A RELEASE version directory has no generator (the spec defines content
// rules for version-group and SNAPSHOT directories only; release version
// discovery is the module document's job, matching Maven Central's
// convention). Its recalculation is the cleanup pass: a metadata document
// left in a directory without any pom goes, under the same RTFACT-6242
// guard.
func (c *calculator) recalcVersionDir(ctx context.Context, p *repo.Principal, t trigger) error {
	prefix := t.dirPath()
	nodes, err := c.nodes.ListByPrefix(ctx, t.repoKey, prefix)
	if err != nil {
		return fmt.Errorf("list %s/%s: %w", t.repoKey, prefix, err)
	}
	files := make([]artifactFile, 0, len(nodes))
	hasPom := false
	for _, n := range nodes {
		if strings.HasSuffix(n.Path, "/") {
			continue
		}
		rel := strings.TrimPrefix(n.Path, prefix+"/")
		if rel == "" || strings.Contains(rel, "/") {
			continue // only files DIRECTLY inside the version directory
		}
		if metadataFileNames[rel] {
			continue
		}
		l, perr := Parse(n.Path)
		if perr != nil || l.Kind != KindArtifact {
			continue
		}
		af, ok := parseArtifactFile(l)
		if !ok {
			continue
		}
		af.createdAt = n.CreatedAt
		if af.ext == "pom" {
			hasPom = true
		}
		files = append(files, af)
	}

	if !t.isSnapshotDir() {
		if hasPom {
			return nil // release version directory: nothing to generate
		}
		return c.removeMetadata(ctx, p, t)
	}
	if len(files) == 0 {
		return c.removeMetadata(ctx, p, t)
	}

	// The snapshot block: the newest unique snapshot POM is the source
	// (buildNumber numeric compare, timestamp ties); without a unique pom
	// the spec fixes buildNumber 1 with no timestamp. A unique jar without
	// any pom (a manual deploy) still carries its own N — taking the max
	// over unique files preserves monotonic numbering instead of restarting
	// at 1 below content that is already stored (ruling on spec silence).
	head := newestUnique(files, true)
	if head == nil {
		head = newestUnique(files, false)
	}
	snap := &snapshotXML{BuildNumber: 1}
	if head != nil {
		snap = &snapshotXML{Timestamp: head.ts, BuildNumber: head.buildNum}
	}

	// snapshotVersions: the newest entry per (extension x classifier) —
	// recency by the node's created timestamp, ties broken by buildNumber
	// then file timestamp, so a re-deployed pair replaces its predecessor.
	type key struct{ ext, classifier string }
	newest := map[key]artifactFile{}
	for _, af := range files {
		k := key{af.ext, af.classifier}
		if cur, ok := newest[k]; !ok || af.newerThan(cur) {
			newest[k] = af
		}
	}
	keys := make([]key, 0, len(newest))
	for k := range newest {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ext != keys[j].ext {
			return keys[i].ext < keys[j].ext
		}
		return keys[i].classifier < keys[j].classifier
	})
	stamp := c.stamp()
	entries := make([]snapshotVersionXML, 0, len(keys))
	for _, k := range keys {
		af := newest[k]
		updated := stamp
		if af.unique {
			updated = strings.ReplaceAll(af.ts, ".", "")
		}
		entries = append(entries, snapshotVersionXML{
			Extension: af.ext, Classifier: af.classifier, Value: af.value, Updated: updated,
		})
	}

	doc := metadataXML{
		GroupID:    t.orgPath,
		ArtifactID: t.module,
		Version:    t.version,
		Versioning: versioningXML{
			Snapshot:         snap,
			LastUpdated:      stamp,
			SnapshotVersions: &snapshotVersionsXML{Entry: entries},
		},
	}
	return c.writeMetadata(ctx, p, t, renderMetadata(doc))
}

// newestUnique picks the newest unique-snapshot file of a class — poms
// when pomOnly, every unique file otherwise — nil when the class is empty.
// "Newest" = highest buildNumber, timestamp breaking ties.
func newestUnique(files []artifactFile, pomOnly bool) *artifactFile {
	var best *artifactFile
	for i := range files {
		af := &files[i]
		if !af.unique || (pomOnly && af.ext != "pom") {
			continue
		}
		if best == nil || af.buildNum > best.buildNum ||
			(af.buildNum == best.buildNum && af.ts > best.ts) {
			best = af
		}
	}
	return best
}

// artifactFile is one parsed snapshot-directory file fact.
type artifactFile struct {
	ext        string // extension after the last dot
	classifier string // "" when none
	unique     bool   // timestamped spelling
	ts         string // yyyyMMdd.HHmmss (unique only)
	buildNum   int64  // N (unique only)
	value      string // the file's version spelling (baseRev-ts-N | versionDir)
	createdAt  string // node.CreatedAt, recency for snapshotVersions
}

// newerThan orders two files of the same (ext, classifier): created time
// first (an honest recency signal), buildNumber then timestamp breaking
// ties (RFC3339 strings compare chronologically).
func (a artifactFile) newerThan(b artifactFile) bool {
	if a.createdAt != b.createdAt {
		return a.createdAt > b.createdAt
	}
	if a.buildNum != b.buildNum {
		return a.buildNum > b.buildNum
	}
	return a.ts > b.ts
}

// parseArtifactFile decomposes a layout-conformant artifact name into the
// snapshot generator's facts. The extension is the tail after the LAST dot
// (a classifier may itself contain dots); the classifier is what sits
// between the version spelling and that extension.
func parseArtifactFile(l Layout) (artifactFile, bool) {
	dot := strings.LastIndex(l.File, ".")
	if dot < 0 || dot == len(l.File)-1 {
		return artifactFile{}, false
	}
	af := artifactFile{ext: l.File[dot+1:]}
	head := l.File[:dot] // module-<version spelling>[-classifier]
	prefix := l.Module + "-"
	if !strings.HasPrefix(head, prefix) {
		return artifactFile{}, false
	}
	rest := head[len(prefix):]
	if l.Timestamped {
		if !strings.HasPrefix(rest, l.BaseRev+"-") {
			return artifactFile{}, false
		}
		m := snapshotFileRevRegExp.FindStringSubmatch(rest[len(l.BaseRev)+1:])
		if m == nil {
			return artifactFile{}, false
		}
		n, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return artifactFile{}, false
		}
		af.unique, af.ts, af.buildNum = true, m[1], n
		af.value = l.BaseRev + "-" + m[1] + "-" + m[2]
		af.classifier = strings.TrimPrefix(rest[len(l.BaseRev)+1+len(m[0]):], "-")
		return af, true
	}
	if !strings.HasPrefix(rest, l.VersionDir) {
		return artifactFile{}, false
	}
	af.value = l.VersionDir
	af.classifier = strings.TrimPrefix(rest[len(l.VersionDir):], "-")
	return af, true
}

// isPomFile reports whether a file name is a pom descriptor: a .pom
// extension and NOT a checksum sidecar (demo.pom counts, demo.pom.sha1
// does not — the sidecar registers a digest, never a descriptor).
func isPomFile(file string) bool {
	if _, _, ok := stripChecksumSuffix(file); ok {
		return false
	}
	return strings.HasSuffix(file, ".pom") && len(file) > len(".pom")
}

// ---- write / remove / guard ----

// writeMetadata lands the computed document through the service's
// regenerable-content exemption (the write grant of the trigger principal
// still applies; the overwrite half is lifted — the document is freely
// rewritable by definition). The measured sha256 rides the declared ref so
// an unchanged recompute is the idempotent retransmit.
func (c *calculator) writeMetadata(ctx context.Context, p *repo.Principal, t trigger, body []byte) error {
	_, err := c.svc.PutWithOptions(ctx, p, t.repoKey, t.metadataPath(),
		bytes.NewReader(body), storage.BlobRef{Sha256: sha256Hex(body)},
		metadataMime, repo.PutOptions{SkipOverwriteCheck: true})
	if err != nil {
		return fmt.Errorf("write %s/%s: %w", t.repoKey, t.metadataPath(), err)
	}
	return nil
}

// removeMetadata deletes the document and its checksum sidecars when no
// pom-bearing fact remains (the version-group deletion rule). The
// RTFACT-6242 guard: in a directory that is NOT a -SNAPSHOT spelling, an
// existing document with snapshot-typed content (a snapshot block or any
// snapshotVersion entry) stays — Maven 2 clients manually deployed those
// against their own layout, and the protective non-delete keeps serving
// them. An unreadable document also stays (fail-safe direction).
func (c *calculator) removeMetadata(ctx context.Context, p *repo.Principal, t trigger) error {
	if !t.isSnapshotDir() {
		keep, err := c.isSnapshotTypedMetadata(ctx, p, t)
		if err != nil {
			slog.WarnContext(ctx, "maven: metadata deletion guard unreadable, keeping document",
				slog.String("repo", t.repoKey), slog.String("path", t.metadataPath()),
				slog.String("error", err.Error()))
			return nil
		}
		if keep {
			return nil
		}
	}
	for _, path := range append([]string{t.metadataPath()}, sidecarPaths(t.metadataPath())...) {
		if err := c.svc.Delete(ctx, p, t.repoKey, path); err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
			return fmt.Errorf("delete %s/%s: %w", t.repoKey, path, err)
		}
	}
	return nil
}

// isSnapshotTypedMetadata reports whether the directory's existing
// document carries snapshot-typed content (the RTFACT-6242 probe). A
// missing document is a plain "not typed" answer, not an error.
func (c *calculator) isSnapshotTypedMetadata(ctx context.Context, p *repo.Principal, t trigger) (bool, error) {
	rc, _, err := c.svc.Get(ctx, p, t.repoKey, t.metadataPath())
	if err != nil {
		if errors.Is(err, repo.ErrNodeNotFound) {
			return false, nil
		}
		return false, err
	}
	defer rc.Close() //nolint:errcheck // read-only fd
	raw, err := io.ReadAll(io.LimitReader(rc, metadataReadLimit))
	if err != nil {
		return false, err
	}
	var doc metadataXML
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return false, err
	}
	return doc.Versioning.Snapshot != nil || doc.Versioning.SnapshotVersions != nil, nil
}

// sidecarPaths lists the checksum-companion node paths of a metadata path.
func sidecarPaths(metaPath string) []string {
	out := make([]string, 0, len(metadataSidecars))
	for _, sfx := range metadataSidecars {
		out = append(out, metaPath+sfx)
	}
	return out
}

// splitDottedLast splits a dotted path into (all but last, last)
// ("com.acme.demo-app" -> "com.acme", "demo-app").
func splitDottedLast(dotted string) (head, last string) {
	if i := strings.LastIndex(dotted, "."); i >= 0 {
		return dotted[:i], dotted[i+1:]
	}
	return "", dotted
}

// ---- XML shapes ([MVN-MD] metadata model; encoding/xml owns escaping) ----

type metadataXML struct {
	XMLName    xml.Name      `xml:"metadata"`
	GroupID    string        `xml:"groupId"`
	ArtifactID string        `xml:"artifactId"`
	Version    string        `xml:"version,omitempty"` // version-level documents only
	Versioning versioningXML `xml:"versioning"`
}

type versioningXML struct {
	Latest           string               `xml:"latest,omitempty"`
	Release          string               `xml:"release,omitempty"`
	Versions         *versionsXML         `xml:"versions,omitempty"`
	Snapshot         *snapshotXML         `xml:"snapshot,omitempty"`
	LastUpdated      string               `xml:"lastUpdated,omitempty"`
	SnapshotVersions *snapshotVersionsXML `xml:"snapshotVersions,omitempty"`
}

type versionsXML struct {
	Version []string `xml:"version"`
}

type snapshotXML struct {
	Timestamp   string `xml:"timestamp,omitempty"`
	BuildNumber int64  `xml:"buildNumber"`
}

type snapshotVersionsXML struct {
	Entry []snapshotVersionXML `xml:"snapshotVersion"`
}

type snapshotVersionXML struct {
	Extension  string `xml:"extension"`
	Classifier string `xml:"classifier,omitempty"`
	Value      string `xml:"value"`
	Updated    string `xml:"updated"`
}

// renderMetadata serializes with the declaration and indentation Maven
// clients' tooling is used to, plus a trailing newline.
func renderMetadata(doc metadataXML) []byte {
	var buf bytes.Buffer
	buf.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		// metadataXML has no marshalling-failing field types; a failure
		// here is a programming fault, and the panic keeps it loud.
		panic(fmt.Sprintf("maven: encode metadata: %v", err))
	}
	buf.WriteString("\n")
	return buf.Bytes()
}
