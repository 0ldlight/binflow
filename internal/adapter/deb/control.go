package deb

// The .deb and .dsc body parsers (debian.md sections 3.2 / 3.4).
//
// A .deb is an ar archive (the public Debian binary package format)
// whose members are debian-binary, control.tar.* and data.tar.*. The
// index engine needs the control.tar member only: a small tar holding
// ./control — the dpkg paragraph (Package, Version, Architecture,
// Depends, Description, ...) the Packages stanza renders from. Modern
// dpkg-deb compresses the member with xz (the default since Debian's
// 2011-era toolchain flip); gzip/zstd/bzip2/plain spellings stay in the
// wild, so all five read.
//
// A .dsc is ONE Debian control paragraph, optionally OpenPGP-armored
// (the armor header and Hash: lines precede a blank line; the paragraph
// follows). The Sources stanza renders from it.

import (
	"archive/tar"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

// Parse guards (a control paragraph is a few KiB; the bounds exist so a
// hostile archive cannot stream unbounded through the parser).
const (
	maxControlBytes  = 1 << 20 // one control paragraph's ceiling
	maxDebProbeBytes = 1 << 20 // the ar head region the control member lives in
)

// ErrNotDebArchive wraps every "the body is not a .deb we can read"
// outcome: the upload chain treats it as parse-degraded (the package
// still stores; the indexer skips it with a WARN — the rpm posture), so
// the sentinel never reaches the client as an error.
var ErrNotDebArchive = errors.New("not a readable debian package archive")

// controlField is one paragraph field; Value keeps the raw value with
// continuation lines verbatim (multiline values like Description and the
// Checksums-* sections render byte-faithfully).
type controlField struct {
	Key   string
	Value string
}

// controlDoc is one parsed Debian control paragraph with the field order
// preserved (the stanza renderers pass fields through in the file's own
// order and only append the server-owned tail).
type controlDoc struct {
	fields []controlField
	index  map[string]int
}

// Get returns the field's value ("" when absent).
func (d *controlDoc) Get(key string) string {
	if d == nil {
		return ""
	}
	if i, ok := d.index[key]; ok {
		return d.fields[i].Value
	}
	return ""
}

// fieldsOf exposes the ordered set (the renderers' input).
func (d *controlDoc) fieldsOf() []controlField {
	if d == nil {
		return nil
	}
	return d.fields
}

// parseControlParagraph reads ONE paragraph from the stream: "Key: value"
// lines, continuation lines starting with space/tab, an optional PGP
// armor prologue before a blank line, and the paragraph ending at the
// next blank line or EOF (the .dsc Files/Checksums sections are
// continuation lines of their field, not separate paragraphs).
func parseControlParagraph(r io.Reader, limit int64) (*controlDoc, error) {
	br := newLineReader(io.LimitReader(r, limit))
	doc := &controlDoc{index: map[string]int{}}
	var cur *controlField
	for {
		line, err := br.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		switch {
		case line == "":
			// Blank line: paragraph boundary. Before any field it ends the
			// PGP armor prologue; after fields it ends the paragraph (a
			// signed .dsc's trailing signature block never reads).
			if len(doc.fields) > 0 {
				return doc, nil
			}
			continue
		case line[0] == ' ' || line[0] == '\t':
			if cur != nil {
				cur.Value += "\n" + line
			}
			continue
		case strings.HasPrefix(line, "-----BEGIN PGP"):
			// The armor header block: skip to the blank separator line.
			if err := skipArmor(br); err != nil {
				return nil, err
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || !validFieldKey(key) {
			// Garbage: the document ended at the boundary; hand back what
			// parsed rather than failing the whole parse.
			if len(doc.fields) > 0 {
				return doc, nil
			}
			return nil, fmt.Errorf("%w: line %q is not a field", ErrNotDebArchive, line)
		}
		value = strings.TrimPrefix(value, " ")
		doc.fields = append(doc.fields, controlField{Key: key, Value: value})
		doc.index[key] = len(doc.fields) - 1
		cur = &doc.fields[len(doc.fields)-1]
	}
	if len(doc.fields) == 0 {
		return nil, fmt.Errorf("%w: no fields parsed", ErrNotDebArchive)
	}
	return doc, nil
}

// skipArmor drains the OpenPGP armor header block ("Hash: ..." lines up
// to the blank separator).
func skipArmor(br *lineReader) error {
	for {
		line, err := br.readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return io.EOF
			}
			return err
		}
		if line == "" {
			return nil
		}
	}
}

// validFieldKey checks the Debian field-name grammar: alphanumeric head,
// alphanumerics and hyphens after.
func validFieldKey(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// lineReader reads \n-terminated lines, stripping the trailing \r\n or
// \n (control documents are Unix text; a CR just before LF is tolerated).
type lineReader struct {
	r   io.Reader
	buf []byte
	eof bool
}

func newLineReader(r io.Reader) *lineReader { return &lineReader{r: r} }

// readLine returns the next line without its terminator; io.EOF only
// when the stream is exhausted (a final line without a terminator still
// yields its text first).
func (lr *lineReader) readLine() (string, error) {
	for {
		if i := bytes.IndexByte(lr.buf, '\n'); i >= 0 {
			line := lr.buf[:i]
			lr.buf = lr.buf[i+1:]
			return strings.TrimSuffix(string(line), "\r"), nil
		}
		if lr.eof {
			if len(lr.buf) == 0 {
				return "", io.EOF
			}
			line := lr.buf
			lr.buf = nil
			return string(line), nil
		}
		chunk := make([]byte, 4096)
		n, err := lr.r.Read(chunk)
		if n > 0 {
			lr.buf = append(lr.buf, chunk[:n]...)
		}
		if err != nil {
			lr.eof = true
			if err != io.EOF {
				return "", err
			}
		}
	}
}

// ---- the .deb ar/tar walk ----

// arMagic is the ar global header.
var arMagic = []byte("!<arch>\n")

// parseDebControl opens a .deb body and parses its control.tar member's
// control file. The walk stops as soon as the control member is
// consumed, so a multi-gigabyte data.tar never reads.
func parseDebControl(body io.Reader) (*controlDoc, error) {
	hdr := make([]byte, 8)
	if _, err := io.ReadFull(body, hdr); err != nil {
		return nil, fmt.Errorf("%w: short ar global header: %w", ErrNotDebArchive, err)
	}
	if !bytes.Equal(hdr, arMagic) {
		return nil, fmt.Errorf("%w: bad ar magic %q", ErrNotDebArchive, hdr)
	}
	for {
		member, err := nextArMember(body)
		if err != nil {
			return nil, err
		}
		if member == nil {
			return nil, fmt.Errorf("%w: no control member found", ErrNotDebArchive)
		}
		if !strings.HasPrefix(member.name, "control.tar") {
			continue
		}
		return controlFromTar(decompressMember(member.name, bytes.NewReader(member.data)))
	}
}

// arMember is one ar member: its name and full payload (the probe bound
// keeps the read bounded; the control member is a few KiB).
type arMember struct {
	name string
	data []byte
}

// nextArMember reads the next member header plus its payload, consuming
// the two-byte alignment pad an odd size leaves behind, so the stream is
// positioned at the following header on return. A nil member with nil
// error is EOF.
func nextArMember(r io.Reader) (*arMember, error) {
	raw := make([]byte, 60)
	if _, err := io.ReadFull(r, raw); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: ar member header: %w", ErrNotDebArchive, err)
	}
	if string(raw[58:60]) != "`\n" {
		return nil, fmt.Errorf("%w: bad ar member magic %q", ErrNotDebArchive, raw[58:60])
	}
	sizeStr := strings.TrimSpace(string(raw[48:58]))
	var size int64
	for i := 0; i < len(sizeStr); i++ {
		c := sizeStr[i]
		if c < '0' || c > '9' {
			return nil, fmt.Errorf("%w: ar member size %q is not decimal", ErrNotDebArchive, sizeStr)
		}
		size = size*10 + int64(c-'0')
	}
	if size < 0 || size > maxDebProbeBytes {
		return nil, fmt.Errorf("%w: ar member size %d out of bounds", ErrNotDebArchive, size)
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, fmt.Errorf("%w: ar member payload: %w", ErrNotDebArchive, err)
	}
	if size%2 == 1 {
		if _, err := io.ReadFull(r, make([]byte, 1)); err != nil {
			return nil, fmt.Errorf("%w: ar alignment pad: %w", ErrNotDebArchive, err)
		}
	}
	name := strings.TrimSpace(string(raw[0:16]))
	name = strings.TrimSuffix(name, "/") // the GNU long-name terminator convention
	return &arMember{name: name, data: data}, nil
}

// decompressMember wraps one control.tar payload per its compression
// suffix (xz the modern default, gzip/zstd/bzip2 in the wild, plain tar
// the legacy spelling).
func decompressMember(name string, r io.Reader) io.Reader {
	switch {
	case strings.HasSuffix(name, ".gz"):
		zr, err := gzip.NewReader(r)
		if err != nil {
			return errReader{err}
		}
		return zr
	case strings.HasSuffix(name, ".xz"):
		xr, err := xz.NewReader(r)
		if err != nil {
			return errReader{err}
		}
		return xr
	case strings.HasSuffix(name, ".zst"):
		zr, err := zstd.NewReader(r)
		if err != nil {
			return errReader{err}
		}
		return zr.IOReadCloser()
	case strings.HasSuffix(name, ".bz2"):
		return bzip2.NewReader(r)
	default:
		return r
	}
}

// errReader yields one error then EOF (the decompressor constructors'
// failure arm).
type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// controlFromTar scans the control tar for the control member ("control"
// or "./control" — dpkg names it ./control) and parses it.
func controlFromTar(r io.Reader) (*controlDoc, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%w: no control file in the control member", ErrNotDebArchive)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: control member tar scan: %w", ErrNotDebArchive, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(hdr.Name)
		if name != "control" && name != "./control" {
			continue
		}
		return parseControlParagraph(tr, maxControlBytes)
	}
}

// parseDsc parses a .dsc body (one paragraph, optional PGP armor).
func parseDsc(body io.Reader) (*controlDoc, error) {
	return parseControlParagraph(body, maxControlBytes)
}
