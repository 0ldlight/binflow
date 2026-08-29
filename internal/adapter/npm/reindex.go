package npm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The copy-side index recompute entry (M12 T-354, the §15.4.2 leftover
// seam / architecture §11.44): unlike the deb index (property coordinates)
// or the conan chain (path revision roots), the npm packument is a STORED
// DOCUMENT the publish plane merges version by version — a copy that lands
// tarballs through the copy/move pipeline bypasses that plane entirely, so
// the derived index (versions, dist-tags, dist digests) silently misses
// the landed versions. T-351's L16 evidence: after the copy-family
// operation a real `npm install <pkg>` answers notarget.
//
// The rebuild is the maven calculator's posture: server-side derivation
// over STORAGE FACTS. The tarball IS the source of truth for a version —
// npm packs `package/package.json` (the full manifest) into every tarball
// — so the recompute reads each stored tarball once, extracts its
// manifest, derives the dist triple (shasum/integrity measured over the
// bytes, the tarball path from the node row), and rewrites the packument
// node. Runs as the caller-supplied principal — the cmd assembly passes
// repo.SystemPrincipal() (the trash chain's internal identity; the D1
// server-internal license posture holds: the gate is the HTTP verb face's,
// an in-process recompute never re-asks the question).

// manifestEntry is the manifest's member path inside an npm tarball (the
// npm-packlist layout: every pack root is "package/").
const manifestEntry = "package/package.json"

// reindex limits: one tarball read is bounded (a hostile or corrupt pack
// must not stream forever), and the manifest member itself is a few KB.
const (
	maxReindexTarball = 512 << 20
	maxReindexMember  = 4 << 20
)

// ReindexDirs recomputes the packument of every package the candidate
// directory set names. A landed tarball's parent directory is the tarball
// directory (<name>/-); a landed packument's parent is the package
// directory itself (<name>) — both spellings map onto the same package
// name, and the rebuild over the tarball facts is idempotent either way.
// Non-npm directory shapes (a generic-path copy into an npm repository)
// fail name validation and are skipped with one log line — there is no
// package to rebuild under them.
func (h *Handler) ReindexDirs(ctx context.Context, p *Principal, repoKey string, dirs []string) error {
	seen := map[string]bool{}
	var errs []error
	for _, dir := range dirs {
		name := packageNameOfDir(dir)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if err := validatePackageName(name); err != nil {
			// Not an npm package directory: a copy may land arbitrary paths
			// in the repository, only npm-shaped ones carry an index.
			slog.DebugContext(ctx, "npm: reindex skipped a non-package directory",
				slog.String("repo", repoKey), slog.String("dir", dir))
			continue
		}
		if err := h.rebuildPackument(ctx, p, repoKey, name); err != nil {
			errs = append(errs, fmt.Errorf("package %s: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// packageNameOfDir derives the npm package name a candidate directory
// addresses: the tarball directory (<name>/-) names its package; any other
// spelling is read as the package directory itself. "" means the directory
// cannot name a package (the repository root or a bare tarball marker).
func packageNameOfDir(dir string) string {
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" || dir == "-" {
		return ""
	}
	if path.Base(dir) == "-" {
		parent := path.Dir(dir)
		if parent == "." || parent == "/" {
			return ""
		}
		return parent
	}
	return dir
}

// rebuildPackument regenerates one package's document from the stored
// tarballs. Additive by contract — the copy observer only fires when files
// landed — so the rebuild keeps every old-document field it can honestly
// carry (top-level metadata, per-version time stamps, dist-tag placements
// whose target survives) and derives the rest; a package with no tarball
// under its directory is left untouched (nothing landed under this name).
func (h *Handler) rebuildPackument(ctx context.Context, p *Principal, repoKey, name string) error {
	tbPrefix := name + "/" + tarballDir
	nodes, err := h.svc.List(ctx, p, repoKey, strings.TrimSuffix(tbPrefix, "/"))
	if err != nil {
		return fmt.Errorf("list %s: %w", tbPrefix, err)
	}
	var tarballs []*metadata.Node
	for _, n := range nodes {
		if n == nil || strings.HasSuffix(n.Path, "/") || !strings.HasPrefix(n.Path, tbPrefix) {
			continue
		}
		if !isTarballFilename(path.Base(n.Path)) {
			continue
		}
		tarballs = append(tarballs, n)
	}
	if len(tarballs) == 0 {
		return nil
	}

	h.docMu.Lock()
	defer h.docMu.Unlock()
	oldDoc, _, _, err := h.loadPackumentForWrite(ctx, p, repoKey, name)
	if err != nil && !errors.Is(err, repo.ErrNodeNotFound) {
		return fmt.Errorf("load packument: %w", err)
	}

	now := h.clock()
	doc := newPackument(name)
	var oldTime map[string]any
	if oldDoc != nil {
		for _, k := range topLevelKeep {
			if v, ok := oldDoc[k]; ok && v != nil {
				doc[k] = v
			}
		}
		oldTime = mapOf(oldDoc["time"])
		if v, ok := oldTime["created"]; ok {
			doc["time"].(map[string]any)["created"] = v
		}
	}
	tm := doc["time"].(map[string]any)
	versions := doc["versions"].(map[string]any)
	for _, n := range tarballs {
		manifest, dist, err := h.readTarballFacts(ctx, p, repoKey, n.Path)
		if err != nil {
			return err
		}
		version := stringOf(manifest["version"])
		if version == "" {
			if v, ok := versionOfTarballName(name, path.Base(n.Path)); ok {
				version = v
			}
		}
		if version == "" {
			return fmt.Errorf("tarball %s: no version in manifest or file name", n.Path)
		}
		if versions[version] != nil {
			continue // both spellings of one tarball (.tgz/.tar.gz): first wins
		}
		m := copyManifest(manifest)
		if stringOf(m["name"]) == "" {
			m["name"] = name
		}
		m["_id"] = name + "@" + version
		m["version"] = version
		m["dist"] = map[string]any{
			"tarball":   n.Path,
			"shasum":    hex.EncodeToString(dist.sha1),
			"integrity": "sha512-" + base64.StdEncoding.EncodeToString(dist.sha512),
		}
		versions[version] = m
		if v, ok := oldTime[version]; ok {
			tm[version] = v // a surviving version keeps its publish stamp
		} else {
			tm[version] = now
		}
	}
	if len(versions) == 0 {
		return nil // unreachable (len(tarballs) > 0); the guard keeps the shape total
	}

	// dist-tags: every old placement whose target survives carries over;
	// recomputeLatestTag then guarantees the npm-mandatory latest tag points
	// at a version that exists (the notarget flip — a stale or missing
	// latest is exactly what T-351's npm install repro read).
	if oldDoc != nil {
		tags := doc["dist-tags"].(map[string]any)
		for tag, v := range distTagsOf(oldDoc) {
			if versions[v] != nil {
				tags[tag] = v
			}
		}
	}
	recomputeLatestTag(doc)
	tm["modified"] = now
	bumpRev(doc)
	return h.savePackument(ctx, p, repoKey, name, oldDoc, doc)
}

// tarballDigests is the measured dist triple of one stored tarball.
type tarballDigests struct {
	sha1   []byte
	sha512 []byte
}

// readTarballFacts reads one stored tarball, measuring the dist digests
// over the WHOLE raw bytes (the publish plane holds the attachment in
// memory too — same bound, same posture) and extracting the
// package/package.json manifest. A tarball without the manifest member, or
// one whose manifest is not a JSON object, is an error — the rebuild
// refuses to guess.
func (h *Handler) readTarballFacts(ctx context.Context, p *Principal, repoKey, nodePath string) (map[string]any, tarballDigests, error) {
	var out tarballDigests
	rc, _, err := h.svc.Get(ctx, p, repoKey, nodePath)
	if err != nil {
		return nil, out, fmt.Errorf("read %s: %w", nodePath, err)
	}
	raw, err := io.ReadAll(io.LimitReader(rc, maxReindexTarball))
	closeErr := rc.Close() //nolint:errcheck // read-only fd; the read error wins
	if err != nil {
		return nil, out, fmt.Errorf("read %s: %w", nodePath, err)
	}
	if closeErr != nil {
		return nil, out, fmt.Errorf("read %s: %w", nodePath, closeErr)
	}

	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, out, fmt.Errorf("tarball %s: %w", nodePath, err)
	}
	defer gz.Close() //nolint:errcheck // a bytes.Reader needs no closing
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, out, fmt.Errorf("tarball %s: %w", nodePath, err)
		}
		name := strings.TrimPrefix(hd.Name, "./")
		if name != manifestEntry {
			continue
		}
		member, err := io.ReadAll(io.LimitReader(tr, maxReindexMember))
		if err != nil {
			return nil, out, fmt.Errorf("manifest %s: %w", nodePath, err)
		}
		dec := json.NewDecoder(bytes.NewReader(member))
		dec.UseNumber()
		var m map[string]any
		if err := dec.Decode(&m); err != nil || m == nil {
			return nil, out, fmt.Errorf("manifest %s: not a JSON object", nodePath)
		}
		// The digest triple rides the publish plane's single spelling
		// (dist.shasum + the ssri integrity reference's sha512).
		sha1Sum, _, sha512Sum := digestTriple(raw)
		out.sha1, out.sha512 = sha1Sum, sha512Sum
		return m, out, nil
	}
	return nil, out, fmt.Errorf("tarball %s: no %s member", nodePath, manifestEntry)
}

// versionOfTarballName recovers the version from the tarball FILE NAME
// (<name>-<version>.tgz, the C8 layout spelling) when the manifest carries
// none: strip the name prefix and the extension, then require a semver.
func versionOfTarballName(name, file string) (string, bool) {
	for _, ext := range []string{tarballExt, tarballExt2} {
		rest := strings.TrimSuffix(file, ext)
		if rest == file {
			continue
		}
		if v := strings.TrimPrefix(rest, name+"-"); v != rest && v != "" {
			if _, err := parseSemver(v); err == nil {
				return v, true
			}
		}
	}
	return "", false
}
