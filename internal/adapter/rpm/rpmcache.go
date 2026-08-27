package rpm

// The .rpmcache parse cache (rpm.md section 2.4 / S8): one JSON record per
// package under <DataDir>/.rpmcache/<repoKey>/<storage path>, holding the
// parsed header plus the identity facts that invalidate it. BinFlow keys
// invalidation on the node's sha256 + size (the node's own truth) instead
// of Artifactory's sha1 + mtime — the sha256 is strictly stronger, and the
// size pins the same "content changed" question the mtime answered.
//
// The cache is a pure optimization: a miss (or a disabled cache — empty
// DataDir) falls back to parsing the stored bytes; a corrupt record is
// discarded, never trusted.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// cacheDirName is the cache root's name under DataDir.
const cacheDirName = ".rpmcache"

// rpmCache is the on-disk parse cache (nil-safe: dir "" disables it).
type rpmCache struct{ dir string }

// cacheRecord is one stored parse result.
type cacheRecord struct {
	Sha256 string  `json:"sha256"`
	Size   int64   `json:"size"`
	Header *Header `json:"header"`
}

// newRpmCache builds the cache rooted at DataDir/.rpmcache.
func newRpmCache(dataDir string) *rpmCache {
	if dataDir == "" {
		return &rpmCache{}
	}
	return &rpmCache{dir: filepath.Join(dataDir, cacheDirName)}
}

// enabled reports a wired cache.
func (c *rpmCache) enabled() bool { return c != nil && c.dir != "" }

// fileFor is one package's cache path (the storage path mirrored).
func (c *rpmCache) fileFor(repoKey, path string) string {
	return filepath.Join(c.dir, repoKey, filepath.FromSlash(path))
}

// load answers the parsed header when the record matches the node's
// identity; every other outcome is a miss (nil).
func (c *rpmCache) load(repoKey, path, sha256 string, size int64) *Header {
	if !c.enabled() || sha256 == "" {
		return nil
	}
	body, err := os.ReadFile(c.fileFor(repoKey, path)) //nolint:gosec // G304: paths built from the repo key + validated node paths
	if err != nil {
		return nil
	}
	var rec cacheRecord
	if err := json.Unmarshal(body, &rec); err != nil {
		return nil
	}
	if rec.Sha256 != sha256 || rec.Size != size || rec.Header == nil {
		return nil
	}
	return rec.Header
}

// store persists one parse result (best effort — a full disk or an
// unwritable data directory degrades to parse-on-demand).
func (c *rpmCache) store(repoKey, path, sha256 string, size int64, hdr *Header) {
	if !c.enabled() || hdr == nil || sha256 == "" {
		return
	}
	file := c.fileFor(repoKey, path)
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return
	}
	body, err := json.Marshal(cacheRecord{Sha256: sha256, Size: size, Header: hdr})
	if err != nil {
		return
	}
	_ = os.WriteFile(file, body, 0o644) //nolint:gosec // G306: instance-local cache records
}

// remove drops one package's record (the delete event's sync).
func (c *rpmCache) remove(repoKey, path string) {
	if !c.enabled() {
		return
	}
	_ = os.Remove(c.fileFor(repoKey, path))
}
