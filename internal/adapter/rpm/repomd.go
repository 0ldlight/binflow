package rpm

// The repodata generator (rpm.md sections 2.1-2.3 / 4.2-4.4): the three
// index documents (primary/filelists/other), the checksum-prefixed .xml.gz
// file naming, the repomd.xml field chain, the comps group-file chain —
// all rendered through encoding/xml's streaming writer over hand-built
// tokens (namespace declarations included verbatim, the community-standard
// http://linux.duke.edu/metadata/* family).
//
// Checksums are SHA-256 everywhere (TL-5: BinFlow's deliberate divergence
// from Artifactory's SHA-1 default — the file-name digest, the repomd
// checksum type spelling AND the primary pkgid use one algorithm; dnf 4/5
// accept sha256 natively, the form real distribution repositories carry).

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"strconv"
)

// The XML namespaces (community standard).
const (
	nsCommon   = "http://linux.duke.edu/metadata/common"
	nsRpm      = "http://linux.duke.edu/metadata/rpm"
	nsFilelist = "http://linux.duke.edu/metadata/filelists"
	nsOther    = "http://linux.duke.edu/metadata/other"
	nsRepo     = "http://linux.duke.edu/metadata/repo"
)

// checksumType is the repomd checksum type spelling under TL-5.
const checksumType = "sha256"

// dataEntry is one repomd <data> record.
type dataEntry struct {
	typ  string // primary | filelists | other | group | group_gz
	href string // repo-root-relative location href ("repodata/<file>" form relative to the reindex root)
	body []byte // the compressed form (what location href serves)
	open []byte // the uncompressed form (open-checksum/open-size)
}

// sha256Hex digests one body.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// gzipBytes renders the compressed form. Deterministic: no name, no mtime
// — identical XML re-compresses to identical bytes, so an unchanged
// repository's reindex produces the same digest-prefixed file names
// instead of piling up identical generations.
func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pkgEntry is one indexed package: the parsed header plus the storage
// facts primary/filelists/other render.
type pkgEntry struct {
	hdr      *Header
	path     string // repo-relative storage path (the location href verbatim)
	sha256   string
	size     int64
	fileTime int64 // unix seconds of the node's UpdatedAt
}

// indexSet is the rendered trio plus its inputs.
type indexSet struct {
	entries   []pkgEntry
	primary   []byte
	filelists []byte
	other     []byte
	withFiles bool
}

// renderIndexes builds the uncompressed XML bodies for the entry set.
// Packages whose fields carry XML-invalid characters drop out of the
// AFFECTED index only (rpm.md section 5 step 4 / S12): the three documents
// are validated independently.
func renderIndexes(entries []pkgEntry, withFiles bool) *indexSet {
	set := &indexSet{withFiles: withFiles}
	var pOK, fOK, oOK []pkgEntry
	for _, e := range entries {
		if validXMLFields(e, true) {
			pOK = append(pOK, e)
		}
		if withFiles && validXMLFiles(e) {
			fOK = append(fOK, e)
		}
		if validXMLChangelog(e) {
			oOK = append(oOK, e)
		}
	}
	set.entries = entries
	set.primary = renderPrimary(pOK)
	set.filelists = renderFilelists(fOK)
	set.other = renderOther(oOK)
	return set
}

// xmlTokenWriter is the tiny token-stream helper (xml.Encoder with the
// declaration).
func startXMLDoc(buf *bytes.Buffer, root xml.StartElement) *xml.Encoder {
	enc := xml.NewEncoder(buf)
	_, _ = buf.WriteString(xml.Header)
	_ = enc.EncodeToken(root)
	return enc
}

// renderPrimary writes the primary document.
func renderPrimary(entries []pkgEntry) []byte {
	var buf bytes.Buffer
	root := xml.StartElement{
		Name: xml.Name{Local: "metadata"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "xmlns"}, Value: nsCommon},
			{Name: xml.Name{Local: "xmlns:rpm"}, Value: nsRpm},
			{Name: xml.Name{Local: "packages"}, Value: strconv.Itoa(len(entries))},
		},
	}
	enc := startXMLDoc(&buf, root)
	for _, e := range entries {
		writePrimaryPackage(enc, e)
	}
	closeRoot(enc, root)
	_ = enc.Flush()
	return buf.Bytes()
}

// writePrimaryPackage renders one <package type="rpm"> block.
func writePrimaryPackage(enc *xml.Encoder, e pkgEntry) {
	pkg := el("package", attr("type", "rpm"))
	start(enc, pkg)
	leaf(enc, "name", e.hdr.Name)
	leaf(enc, "arch", e.hdr.Arch)
	span(enc, el("version",
		attr("epoch", epochOrZero(e.hdr.Epoch)), attr("ver", e.hdr.Version), attr("rel", e.hdr.Release)))
	span(enc, el("checksum", attr("type", checksumType), attr("pkgid", "YES")), e.sha256)
	if e.hdr.Summary != "" {
		leaf(enc, "summary", e.hdr.Summary)
	}
	if e.hdr.Description != "" {
		leaf(enc, "description", e.hdr.Description)
	}
	if e.hdr.Packager != "" {
		leaf(enc, "packager", e.hdr.Packager)
	}
	if e.hdr.URL != "" {
		leaf(enc, "url", e.hdr.URL)
	}
	build := e.hdr.BuildTime
	span(enc, el("time", attr("file", strconv.FormatInt(e.fileTime, 10)), attr("build", strconv.FormatInt(build, 10))))
	span(enc, el("size",
		attr("package", strconv.FormatInt(e.size, 10)),
		attr("installed", strconv.FormatInt(e.hdr.Size, 10)),
		attr("archive", strconv.FormatInt(e.hdr.ArchiveSize, 10))))
	span(enc, el("location", attr("href", e.path)))
	start(enc, el("format"))
	if e.hdr.License != "" {
		leaf(enc, "rpm:license", e.hdr.License)
	}
	if e.hdr.Vendor != "" {
		leaf(enc, "rpm:vendor", e.hdr.Vendor)
	}
	if e.hdr.Group != "" {
		leaf(enc, "rpm:group", e.hdr.Group)
	}
	span(enc, el("rpm:header_range",
		attr("start", strconv.FormatInt(e.hdr.HeaderStart, 10)),
		attr("end", strconv.FormatInt(e.hdr.HeaderEnd, 10))))
	writeDepGroup(enc, "rpm:provides", e.hdr.Provides, false)
	writeDepGroup(enc, "rpm:requires", e.hdr.Requires, true)
	writeDepGroup(enc, "rpm:conflicts", e.hdr.Conflicts, false)
	writeDepGroup(enc, "rpm:obsoletes", e.hdr.Obsoletes, false)
	writeDepGroup(enc, "rpm:recommends", e.hdr.Recommends, false)
	writeDepGroup(enc, "rpm:suggests", e.hdr.Suggests, false)
	end(enc, el("format"))
	end(enc, pkg)
}

// writeDepGroup renders one rpm:<group> block (empty groups omitted —
// createrepo's shape; dnf treats absence and emptiness alike).
func writeDepGroup(enc *xml.Encoder, name string, deps []Dependency, requires bool) {
	if len(deps) == 0 {
		return
	}
	g := el(name)
	start(enc, g)
	for _, d := range deps {
		a := []xml.Attr{attr("name", d.Name)}
		if s := senseString(d.Flags); s != "" {
			a = append(a, attr("flags", s))
			// createrepo renders epoch/ver/rel only for sensed entries.
			a = append(a, attr("epoch", epochOrZero(d.Epoch)), attr("ver", d.Version))
			if d.Release != "" {
				a = append(a, attr("rel", d.Release))
			}
		}
		if requires && isPreDep(d.Flags) {
			a = append(a, attr("pre", "1"))
		}
		span(enc, el("rpm:entry", a...))
	}
	end(enc, g)
}

// renderFilelists writes the filelists document.
func renderFilelists(entries []pkgEntry) []byte {
	var buf bytes.Buffer
	root := xml.StartElement{
		Name: xml.Name{Local: "filelists"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: nsFilelist},
			{Name: xml.Name{Local: "packages"}, Value: strconv.Itoa(len(entries))}},
	}
	enc := startXMLDoc(&buf, root)
	for _, e := range entries {
		pkg := el("package", attr("pkgid", e.sha256), attr("name", e.hdr.Name), attr("arch", e.hdr.Arch))
		start(enc, pkg)
		span(enc, el("version",
			attr("epoch", epochOrZero(e.hdr.Epoch)), attr("ver", e.hdr.Version), attr("rel", e.hdr.Release)))
		for _, f := range e.hdr.Files {
			leaf(enc, "file", f)
		}
		end(enc, pkg)
	}
	closeRoot(enc, root)
	_ = enc.Flush()
	return buf.Bytes()
}

// renderOther writes the other/changelog document.
func renderOther(entries []pkgEntry) []byte {
	var buf bytes.Buffer
	root := xml.StartElement{
		Name: xml.Name{Local: "otherdata"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: nsOther},
			{Name: xml.Name{Local: "packages"}, Value: strconv.Itoa(len(entries))}},
	}
	enc := startXMLDoc(&buf, root)
	for _, e := range entries {
		pkg := el("package", attr("pkgid", e.sha256), attr("name", e.hdr.Name), attr("arch", e.hdr.Arch))
		start(enc, pkg)
		span(enc, el("version",
			attr("epoch", epochOrZero(e.hdr.Epoch)), attr("ver", e.hdr.Version), attr("rel", e.hdr.Release)))
		for _, c := range e.hdr.Changelog {
			span(enc, el("changelog",
				attr("date", strconv.FormatInt(c.Time, 10)), attr("author", c.Name)), c.Text)
		}
		end(enc, pkg)
	}
	closeRoot(enc, root)
	_ = enc.Flush()
	return buf.Bytes()
}

// dataEntryFor gzips one body and names the file after its compressed
// digest (the checksum-prefixed naming, rpm.md section 2.1).
func dataEntryFor(typ string, body []byte) (*dataEntry, error) {
	gz, err := gzipBytes(body)
	if err != nil {
		return nil, fmt.Errorf("gzip %s: %w", typ, err)
	}
	return &dataEntry{
		typ:  typ,
		href: "repodata/" + sha256Hex(gz) + "-" + typ + ".xml.gz",
		body: gz,
		open: body,
	}, nil
}

// renderRepomd writes the index-of-indexes document. The child order
// follows the generator the spec read out (location → checksum →
// timestamp → size → open-size → open-checksum); <revision> carries the
// empty content (S14 — Artifactory's literal, dnf tolerates).
func renderRepomd(entries []*dataEntry, nowUnix int64) []byte {
	var buf bytes.Buffer
	root := xml.StartElement{
		Name: xml.Name{Local: "repomd"},
		Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: nsRepo}},
	}
	enc := startXMLDoc(&buf, root)
	leaf(enc, "revision", "")
	for _, d := range entries {
		if d == nil {
			continue
		}
		data := el("data", attr("type", d.typ))
		start(enc, data)
		span(enc, el("location", attr("href", d.href)))
		span(enc, el("checksum", attr("type", checksumType)), sha256Hex(d.body))
		span(enc, el("timestamp"), strconv.FormatInt(nowUnix, 10))
		span(enc, el("size"), strconv.Itoa(len(d.body)))
		span(enc, el("open-size"), strconv.Itoa(len(d.open)))
		span(enc, el("open-checksum", attr("type", checksumType)), sha256Hex(d.open))
		end(enc, data)
	}
	closeRoot(enc, root)
	_ = enc.Flush()
	return buf.Bytes()
}

// groupDataEntries builds the comps pair (group + group_gz) for one group
// document body (rpm.md section 4.4). name is the CONFIGURED FILE NAME in
// full ("comps.xml"): the renamed spelling is <digest>-<name>, the .gz
// companion appends .gz to it.
func groupDataEntries(name string, xmlBody []byte) (*dataEntry, *dataEntry, error) {
	gz, err := gzipBytes(xmlBody)
	if err != nil {
		return nil, nil, fmt.Errorf("gzip group %s: %w", name, err)
	}
	dx := sha256Hex(xmlBody)
	dg := sha256Hex(gz)
	group := &dataEntry{
		typ:  "group",
		href: "repodata/" + dx + "-" + name,
		body: xmlBody,
		open: xmlBody,
	}
	groupGZ := &dataEntry{
		typ:  "group_gz",
		href: "repodata/" + dg + "-" + name + ".gz",
		body: gz,
		open: xmlBody,
	}
	return group, groupGZ, nil
}

// ---- token helpers ----

func attr(k, v string) xml.Attr { return xml.Attr{Name: xml.Name{Local: k}, Value: v} }

func el(name string, attrs ...xml.Attr) xml.StartElement {
	return xml.StartElement{Name: xml.Name{Local: name}, Attr: attrs}
}

// start writes one start tag.
func start(enc *xml.Encoder, s xml.StartElement) { _ = enc.EncodeToken(s) }

// end writes one end tag.
func end(enc *xml.Encoder, s xml.StartElement) { _ = enc.EncodeToken(s.End()) }

// span writes a start tag, optional chardata and the end tag.
func span(enc *xml.Encoder, s xml.StartElement, body ...string) {
	start(enc, s)
	if len(body) > 0 {
		_ = enc.EncodeToken(xml.CharData(body[0]))
	}
	end(enc, s)
}

// leaf is span with text.
func leaf(enc *xml.Encoder, name, body string) {
	span(enc, el(name), body)
}

// closeRoot closes the root element.
func closeRoot(enc *xml.Encoder, root xml.StartElement) { end(enc, root) }

// epochOrZero normalizes the XML's epoch default.
func epochOrZero(e string) string {
	if e == "" {
		return "0"
	}
	return e
}

// ---- XML validity (S12) ----

// validXMLFields checks the header fields primary renders.
func validXMLFields(e pkgEntry, _ bool) bool {
	h := e.hdr
	for _, s := range []string{h.Name, h.Arch, h.Summary, h.Description, h.Packager,
		h.URL, h.License, h.Vendor, h.Group} {
		if !validXMLString(s) {
			return false
		}
	}
	for _, g := range [][]Dependency{h.Provides, h.Requires, h.Conflicts, h.Obsoletes, h.Recommends, h.Suggests} {
		for _, d := range g {
			if !validXMLString(d.Name) || !validXMLString(d.Version) || !validXMLString(d.Release) {
				return false
			}
		}
	}
	return true
}

// validXMLFiles checks the file-list fields.
func validXMLFiles(e pkgEntry) bool {
	for _, f := range e.hdr.Files {
		if !validXMLString(f) {
			return false
		}
	}
	return true
}

// validXMLChangelog checks the changelog fields.
func validXMLChangelog(e pkgEntry) bool {
	for _, c := range e.hdr.Changelog {
		if !validXMLString(c.Name) || !validXMLString(c.Text) {
			return false
		}
	}
	return true
}

// validXMLString reports one XML 1.0-encodable string: only the legal
// characters (tab/LF/CR and the ≥ 0x20 ranges); the token writer escapes
// the metacharacters themselves.
func validXMLString(s string) bool {
	for _, r := range s {
		switch {
		case r == 0x09 || r == 0x0a || r == 0x0d:
		case r >= 0x20 && r <= 0xd7ff:
		case r >= 0xe000 && r <= 0xfffd:
		case r >= 0x10000 && r <= 0x10ffff:
		default:
			return false
		}
	}
	return true
}

// stripDigestPrefix splits <digest>-rest; ok is false when base does not
// carry the prefix.
func stripDigestPrefix(base string) (string, bool) {
	if !isDigestPrefixed(base) {
		return "", false
	}
	return base[65:], true
}
