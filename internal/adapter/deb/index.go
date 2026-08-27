package deb

// The index renderers (debian.md sections 2 / 4): Packages stanzas, the
// Sources stanza family, and the Release meta-index. All rendering is
// deterministic — entries order by path, fields keep the control
// paragraph's own order, the server-owned tail appends in a fixed
// sequence — so the same repository state renders byte-identical indexes
// (and the by-hash digests only move when content really moves).

import (
	"crypto/md5"  //nolint:gosec // the Debian Release format mandates the MD5Sum section; preimage resistance is not the property in play
	"crypto/sha1" //nolint:gosec // the Debian Release format mandates the SHA1 section; preimage resistance is not the property in play
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ---- index file digests ----

// indexDigests is one rendered index file's measured facts.
type indexDigests struct {
	size   int
	md5    string
	sha1   string
	sha256 string
}

// measureIndex digests one rendered body (all three algorithms: the
// Release sections and the by-hash trees key on them per policy).
func measureIndex(body []byte) indexDigests {
	m := md5.Sum(body)   //nolint:gosec // format-mandated (see import note)
	s1 := sha1.Sum(body) //nolint:gosec // format-mandated (see the import note)
	s2 := sha256.Sum256(body)
	return indexDigests{
		size:   len(body),
		md5:    hex.EncodeToString(m[:]),
		sha1:   hex.EncodeToString(s1[:]),
		sha256: hex.EncodeToString(s2[:]),
	}
}

// indexFile is one file the engine writes for a distribution: its
// repository-relative path, its body, its content type and its digests.
type indexFile struct {
	path    string
	body    []byte
	ctype   string
	digests indexDigests
}

// newIndexFile measures the body once.
func newIndexFile(path string, body []byte, ctype string) indexFile {
	return indexFile{path: path, body: body, ctype: ctype, digests: measureIndex(body)}
}

// ---- Packages stanzas (section 3.2 / the section 7 checksum chain) ----

// serverOwnedFields never pass through from the client's control
// paragraph: the Packages index owns them (Filename/Size/MD5sum/SHA1/
// SHA256 describe the .deb file, not the package), and the checksums
// sections belong to the .dsc family only.
var serverOwnedFields = map[string]bool{
	"Filename": true, "Size": true, "MD5sum": true, "SHA1": true, "SHA256": true,
	"Files": true, "Checksums-Sha1": true, "Checksums-Sha256": true, "Checksums-Sha512": true,
}

// pkgEntry is one .deb's index facts.
type pkgEntry struct {
	doc    *controlDoc // nil when the body failed to parse (skipped upstream)
	path   string      // repository-relative .deb path — the Filename field
	sha256 string
	sha1   string
	md5    string
	size   int64
}

// indexable reports whether the entry can render a stanza: the control
// paragraph must carry the identity fields the index keys on.
func (e pkgEntry) indexable() bool {
	return e.doc != nil && e.doc.Get("Package") != "" && e.doc.Get("Version") != ""
}

// renderPackagesBody renders one binary-<arch> index: one stanza per
// entry (path order), each ending in the blank separator, the file
// ending with a trailing newline after the last stanza.
func renderPackagesBody(entries []pkgEntry) []byte {
	var b strings.Builder
	for _, e := range entries {
		for _, f := range e.doc.fieldsOf() {
			if serverOwnedFields[f.Key] {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", f.Key, f.Value)
		}
		fmt.Fprintf(&b, "Filename: %s\n", e.path)
		fmt.Fprintf(&b, "Size: %d\n", e.size)
		if e.md5 != "" {
			fmt.Fprintf(&b, "MD5sum: %s\n", e.md5)
		}
		if e.sha1 != "" {
			fmt.Fprintf(&b, "SHA1: %s\n", e.sha1)
		}
		if e.sha256 != "" {
			fmt.Fprintf(&b, "SHA256: %s\n", e.sha256)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// ---- Sources stanzas (section 3.4) ----

// srcEntry is one .dsc's index facts.
type srcEntry struct {
	doc  *controlDoc
	path string
}

// indexable reports whether the source stanza can render.
func (e srcEntry) indexable() bool {
	return e.doc != nil && e.doc.Get("Source") != "" && e.doc.Get("Version") != ""
}

// dscPassThrough are the .dsc fields the Sources index carries verbatim
// (the official stanza family); Checksums-* ride as continuation-line
// fields through the same pass.
var dscFields = map[string]bool{
	"Source": true, "Version": true, "Binary": true, "Maintainer": true,
	"Uploaders": true, "Architecture": true, "Section": true, "Priority": true,
	"Standards-Version": true, "Format": true, "Vcs-Browser": true, "Vcs-Git": true,
	"Build-Depends": true, "Build-Depends-Indep": true, "Build-Conflicts": true,
	"Homepage": true, "Description": true, "Files": true,
	"Checksums-Sha1": true, "Checksums-Sha256": true, "Checksums-Sha512": true,
}

// renderSourcesBody renders one source/ index: the .dsc's stanza with
// Source renamed to Package (the index spelling), Directory pointing at
// the .dsc's own directory (the relative base of the checksums entries)
// and the checksums sections passed through verbatim.
func renderSourcesBody(entries []srcEntry) []byte {
	var b strings.Builder
	for _, e := range entries {
		for _, f := range e.doc.fieldsOf() {
			if !dscFields[f.Key] {
				continue
			}
			key := f.Key
			if key == "Source" {
				key = "Package"
			}
			fmt.Fprintf(&b, "%s: %s\n", key, f.Value)
		}
		dir := parentDir(e.path)
		if dir != "" {
			fmt.Fprintf(&b, "Directory: %s\n", dir)
		}
		b.WriteString("\n")
	}
	return []byte(b.String())
}

// parentDir is the path's directory prefix ("" at the repository root).
func parentDir(p string) string {
	if i := strings.LastIndexByte(p, '/'); i > 0 {
		return p[:i]
	}
	return ""
}

// ---- Release (section 4.1) ----

// releaseDoc is one distribution's Release facts.
type releaseDoc struct {
	dist       string
	components []string // sorted, space-joined
	arches     []string // sorted, space-joined (pseudo any/all filtered)
	policy     string   // the by-hash policy: ALL / SHA256 / NONE
	origin     string
	label      string
	date       time.Time
	files      []indexFile // the distribution's index set (paths relative to dists/<dist>/)
}

// renderReleaseBody renders the meta-index. The checksum sections key
// on the by-hash policy (section 4.1: NONE/ALL carry MD5Sum+SHA1+SHA256;
// SHA256 carries SHA256 only), every entry lists the CANONICAL file
// paths (the by-hash mirrors are the same bytes under their digest
// names — apt resolves them through the entry's own hash).
func renderReleaseBody(d releaseDoc) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "Origin: %s\n", d.origin)
	fmt.Fprintf(&b, "Label: %s\n", d.label)
	fmt.Fprintf(&b, "Suite: %s\n", d.dist)
	fmt.Fprintf(&b, "Codename: %s\n", d.dist)
	fmt.Fprintf(&b, "Date: %s\n", d.date.UTC().Format(releaseDateFormat))
	if byHashEnabled(d.policy) {
		b.WriteString("Acquire-By-Hash: yes\n")
	}
	fmt.Fprintf(&b, "Components: %s\n", strings.Join(d.components, " "))
	archKey := "Architectures"
	if len(d.arches) == 1 {
		archKey = "Architecture"
	}
	fmt.Fprintf(&b, "%s: %s\n", archKey, strings.Join(d.arches, " "))

	sections := []struct {
		name string
		hash func(indexDigests) string
	}{
		{"MD5Sum", func(g indexDigests) string { return g.md5 }},
		{"SHA1", func(g indexDigests) string { return g.sha1 }},
		{"SHA256", func(g indexDigests) string { return g.sha256 }},
	}
	if d.policy == byHashSHA256 {
		sections = sections[2:]
	}
	for _, sec := range sections {
		fmt.Fprintf(&b, "%s:\n", sec.name)
		for _, f := range d.files {
			fmt.Fprintf(&b, " %s %17d %s\n", sec.hash(f.digests), f.digests.size, f.path)
		}
	}
	return []byte(b.String())
}

// releaseDateFormat is the Date field's pinned spelling (RFC
// "EEE, dd MMM yyyy HH:mm:ss UTC" — section 4.1).
const releaseDateFormat = "Mon, 02 Jan 2006 15:04:05 UTC"

// archLine renders the Architectures line's value: sorted, deduplicated,
// the any/all pseudo architectures filtered (section 4.1).
func archLine(arches []string, forced []string) []string {
	set := map[string]bool{}
	for _, a := range arches {
		if pseudoArches[a] {
			continue
		}
		set[a] = true
	}
	for _, a := range forced {
		if !pseudoArches[a] {
			set[a] = true
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}
