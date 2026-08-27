package rpm

// The virtual aggregation's merge engine (rpm.md section 6.3 step 4 / S11):
// the members' index documents split into their raw top-level child
// segments — the spec's "流式逐行拼接" is a VERBATIM byte concat of the
// member package entries under a fresh root, so nothing a member's primary
// carries (an optional field the local generator omits, an upstream's extra
// element) can be lost in translation. The segment boundaries come off
// xml.Decoder's InputOffset pairs; the identity each entry dedups on
// (name+arch) is read alongside: primary entries from their <name>/<arch>
// child elements, filelists/other entries from the package element's own
// attributes.

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// segEntry is one member index document's top-level child: its raw XML
// bytes plus the name+arch identity the priority dedup keys on. An entry
// whose identity did not resolve (name and arch both empty) never dedups
// out — the conservative keep.
type segEntry struct {
	raw  []byte
	name string
	arch string
}

// indexSegments is one member index document in segment form.
type indexSegments struct {
	segs []segEntry
}

// extractSegments splits one index document into its top-level children.
// attrIdentity selects the identity source: filelists/other carry the
// package identity as ATTRIBUTES of the child element itself; primary (and
// updateinfo, whose entries carry no identity at all) read it — when
// present — from the child's <name>/<arch> sub-elements.
func extractSegments(doc []byte, attrIdentity bool) (*indexSegments, error) {
	// The decoder is the standard library's own XML parser with NO
	// DTD/entity resolution wired (encoding/xml declines external
	// entities by default) — the decoded segments are inert bytes.
	dec := xml.NewDecoder(bytes.NewReader(doc)) //nolint:gosec // G709: inert segment split, no entity expansion
	out := &indexSegments{}
	depth := 0
	childStart := int64(-1)
	childName, childArch := "", ""
	sawRoot := false
	for {
		next := dec.InputOffset() // the byte the upcoming token starts at
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse index document: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch depth {
			case 0:
				sawRoot = true
			case 1:
				childStart = next
				childName, childArch = "", ""
				if attrIdentity {
					childName, childArch = attrOf(t.Attr, "name"), attrOf(t.Attr, "arch")
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth == 1 && childStart >= 0 {
				seg := doc[childStart:dec.InputOffset()]
				name, arch := childName, childArch
				if !attrIdentity {
					name, arch = innerIdentity(seg)
				}
				out.segs = append(out.segs, segEntry{raw: seg, name: name, arch: arch})
				childStart = -1
			}
		}
	}
	if !sawRoot {
		return nil, fmt.Errorf("no root element in index document")
	}
	return out, nil
}

// attrOf reads one attribute value off a start element (namespace-agnostic
// on purpose: the identity attributes carry none).
func attrOf(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// innerIdentity reads a primary package segment's <name>/<arch> child
// elements (depth 2 — the package element's own children; dependency entry
// names are attributes and never match).
func innerIdentity(seg []byte) (name, arch string) {
	dec := xml.NewDecoder(bytes.NewReader(seg)) //nolint:gosec // G709: inert field decode, no entity expansion
	depth := 0
	want := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return name, arch
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && (t.Name.Local == "name" || t.Name.Local == "arch") {
				want = t.Name.Local
			}
		case xml.CharData:
			if want != "" && depth == 2 {
				switch want {
				case "name":
					name = string(t)
				case "arch":
					arch = string(t)
				}
				want = ""
			}
		case xml.EndElement:
			depth--
		}
	}
}

// dedupKey is the S11 merge key: the name+arch string concatenation.
func (s segEntry) dedupKey() string { return s.name + "\x00" + s.arch }

// memberSegs is one member's contribution to one index family.
type memberSegs struct {
	priority bool
	segs     []segEntry
}

// mergePackageSegments applies the S11 rule: priority members' entries are
// ALWAYS kept; a non-priority member's entry drops only when a PRIORITY
// member already contributed the same name+arch — non-priority members
// never dedup among themselves (their duplicates coexist, Artifactory's
// literal behavior and BinFlow's minimal boundary alike).
func mergePackageSegments(contribs []memberSegs) []segEntry {
	priKeys := map[string]bool{}
	for _, c := range contribs {
		if !c.priority {
			continue
		}
		for i := range c.segs {
			priKeys[c.segs[i].dedupKey()] = true
		}
	}
	var out []segEntry
	for _, c := range contribs {
		for i := range c.segs {
			if !c.priority && priKeys[c.segs[i].dedupKey()] {
				continue
			}
			out = append(out, c.segs[i])
		}
	}
	return out
}

// renderSegmentDoc reassembles merged segments under a fresh root: the XML
// declaration plus the hand-built root open tag (the standard namespaces
// the family's documents share), the raw member segments verbatim, and the
// closing tag. packagesAttr is the packages="N" root attribute's value.
func renderSegmentDoc(rootOpen, rootClose string, segs []segEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(rootOpen)
	buf.WriteString(strconv.Itoa(len(segs)))
	buf.WriteString(`">`)
	for i := range segs {
		buf.WriteByte('\n')
		buf.Write(segs[i].raw)
	}
	buf.WriteString(rootClose)
	return buf.Bytes()
}

// The per-family root tags (the same shapes the local generator emits, so
// a merged document is byte-compatible with a local one of the same
// entries — namespaces included).
const (
	rootOpenPrimary   = `<metadata xmlns="` + nsCommon + `" xmlns:rpm="` + nsRpm + `" packages="`
	rootClosePrimary  = `</metadata>`
	rootOpenFilelists = `<filelists xmlns="` + nsFilelist + `" packages="`
	rootCloseFilelist = `</filelists>`
	rootOpenOther     = `<otherdata xmlns="` + nsOther + `" packages="`
	rootCloseOther    = `</otherdata>`
	rootOpenUpdates   = `<updates xmlns="http://linux.duke.edu/metadata/updateinfo">`
	rootCloseUpdates  = `</updates>`
	rootOpenComps     = `<comps>`
	rootCloseComps    = `</comps>`
)

// renderMergedPrimary renders the merged primary document.
func renderMergedPrimary(segs []segEntry) []byte {
	return renderSegmentDoc(rootOpenPrimary, rootClosePrimary, segs)
}

// renderMergedFilelists renders the merged filelists document.
func renderMergedFilelists(segs []segEntry) []byte {
	return renderSegmentDoc(rootOpenFilelists, rootCloseFilelist, segs)
}

// renderMergedOther renders the merged other/changelog document.
func renderMergedOther(segs []segEntry) []byte {
	return renderSegmentDoc(rootOpenOther, rootCloseOther, segs)
}

// renderMergedUpdateinfo renders the merged updateinfo document (the
// packages attribute is not part of this family's root).
func renderMergedUpdateinfo(segs []segEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(rootOpenUpdates)
	for i := range segs {
		buf.WriteByte('\n')
		buf.Write(segs[i].raw)
	}
	buf.WriteString(rootCloseUpdates)
	return buf.Bytes()
}

// renderMergedComps renders the merged comps document (same shape rule as
// updateinfo — <comps> carries no count attribute).
func renderMergedComps(segs []segEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	buf.WriteString(rootOpenComps)
	for i := range segs {
		buf.WriteByte('\n')
		buf.Write(segs[i].raw)
	}
	buf.WriteString(rootCloseComps)
	return buf.Bytes()
}

// mergeModuleDocs concatenates the members' modularity documents into one
// YAML multi-document stream (modules.yaml IS a --- separated stream, so
// concatenation is the merge).
func mergeModuleDocs(docs [][]byte) []byte {
	var buf bytes.Buffer
	for _, d := range docs {
		s := bytes.TrimSpace(d)
		if len(s) == 0 {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n---\n")
		}
		buf.Write(s)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// gunzipBytes decompresses one member index body.
func gunzipBytes(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer func() { _ = zr.Close() }() //nolint:errcheck // read-only fd
	out, err := io.ReadAll(io.LimitReader(zr, maxIndexReadBytes))
	if err != nil {
		return nil, fmt.Errorf("gzip read: %w", err)
	}
	return out, nil
}

// safeMemberHref folds one member repomd's location href onto the storage
// path the member read runs at: the href is REPO-RELATIVE to the yum root
// the aggregate serves ("repodata/<file>"), must stay inside that root (a
// hostile upstream repomd must never steer a member read at paths outside
// the repository), and keeps its literal spelling otherwise.
func safeMemberHref(root, href string) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "/") {
		return "", false
	}
	clean := path.Clean(href)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return joinRoot(root, clean), true
}
