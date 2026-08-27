package rpm

// The RPM header parser (rpm.md section 2.4 — the ticket's self-built
// "redline equivalent"): a pure-Go reader of the on-disk package format.
// The format facts come from the public RPM file-format documentation
// (lead + signature header + header + payload; big-endian fixed-width
// integers; the 16-byte index-entry grid) — no external dependency, no
// code translation from any reference implementation.
//
// Layout (all multi-byte integers big-endian):
//
//	lead (96 bytes): magic ED AB EE DB, major, minor, type(2), archnum(2),
//	                 name(66, NUL-padded), osnum(2), signature_type(2), pad(16)
//	signature header: one header structure, then padding to an 8-byte boundary
//	header:          one header structure (the tag set this package decodes)
//	payload:         cpio archive (never read here)
//
// header structure:
//
//	magic 8E AD E8, version(1, currently 1), reserved(3),
//	nindex(4), store_size(4),
//	nindex × index entry { tag(4), type(4), offset(4), count(4) },
//	store (store_size bytes; offsets in the entries are store-relative)

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Format constants (public format spec).
const (
	leadMagic    = 0xedabeedb // bytes ED AB EE DB
	leadSize     = 96
	headerMagic  = 0x8ead_e8 // bytes 8E AD E8
	headerVer    = 1
	indexEntrySz = 16
)

// Tag IDs (the public rpmtag numbering; only the tags the indexer and the
// rpm.metadata.* property set consume are named).
const (
	tagName           = 1000
	tagVersion        = 1001
	tagRelease        = 1002
	tagEpoch          = 1003
	tagSummary        = 1004
	tagDescription    = 1005
	tagBuildTime      = 1006
	tagSize           = 1009
	tagVendor         = 1011
	tagLicense        = 1014
	tagPackager       = 1015
	tagGroup          = 1016
	tagURL            = 1020
	tagArch           = 1022
	tagSourceRPM      = 1044
	tagArchiveSize    = 1046
	tagProvideName    = 1047
	tagRequireFlags   = 1048
	tagRequireName    = 1049
	tagRequireVersion = 1050
	tagConflictFlags  = 1053
	tagConflictName   = 1054
	tagConflictVer    = 1055
	tagChangelogTime  = 1080
	tagChangelogName  = 1081
	tagChangelogText  = 1082
	tagObsoleteName   = 1090
	tagProvideFlags   = 1112
	tagProvideVer     = 1113
	tagObsoleteFlags  = 1114
	tagObsoleteVer    = 1115
	tagDirIndexes     = 1116
	tagBaseNames      = 1117
	tagDirNames       = 1118
	tagRecommendName  = 5046
	tagRecommendVer   = 5047
	tagRecommendFlg   = 5048
	tagSuggestName    = 5049
	tagSuggestVer     = 5050
	tagSuggestFlg     = 5051
	tagLongSize       = 5009 // RPM v6's 64-bit size companion
)

// Index-entry data types (public format spec).
const (
	typNull        = 0
	typChar        = 1
	typInt8        = 2
	typInt16       = 3
	typInt32       = 4
	typInt64       = 5
	typString      = 6
	typBlob        = 7
	typStringArray = 8
	typI18nString  = 9
)

// Dependency sense flags (public rpmds numbering — the subset the
// primary.xml flags attribute renders).
const (
	senseLess       = 0x02
	senseGreater    = 0x04
	senseEqual      = 0x08
	sensePreReq     = 0x40 // RPMSENSE_PREREQ — the pre="1" marker's classic bit
	senseScriptPre  = 0x200
	senseScriptPost = 0x400
)

// Hostile-input bounds: a real main header's store stays in the low
// single-digit megabytes (file-list-heavy packages); anything past these
// bounds is a corrupt or crafted blob, refused without allocation.
const (
	maxIndexEntries = 1 << 16
	maxStoreBytes   = 128 << 20
	maxStringsOf    = 1 << 20
)

// Dependency is one PROVIDE/REQUIRE/CONFLICT/OBSOLETE/RECOMMEND/SUGGEST
// entry — the five-tuple the spec's section 2.4 names (name/flags/epoch/
// version/release; epoch/version/release parsed out of the wire's
// "E:V-R" string).
type Dependency struct {
	Name    string
	Flags   uint32
	Epoch   string
	Version string
	Release string
}

// ChangelogEntry is one CHANGELOG triple (TIME/NAME/TEXT parallel arrays).
type ChangelogEntry struct {
	Time int64
	Name string
	Text string
}

// Header is the decoded main header of one .rpm.
type Header struct {
	Name        string
	Version     string
	Release     string
	Epoch       string
	Arch        string
	License     string
	Group       string
	Vendor      string
	Summary     string
	Description string
	URL         string
	Packager    string
	SourceRPM   string
	BuildTime   int64
	Size        int64
	ArchiveSize int64

	Provides   []Dependency
	Requires   []Dependency
	Conflicts  []Dependency
	Obsoletes  []Dependency
	Recommends []Dependency
	Suggests   []Dependency

	Files     []string
	Changelog []ChangelogEntry

	// HeaderStart/HeaderEnd are the main header's byte range inside the
	// package — primary.xml's rpm:header_range renders them (real values
	// from the parse, never invented).
	HeaderStart int64
	HeaderEnd   int64
}

// NEVRA renders the package identity in the conventional display form.
func (h *Header) NEVRA() string {
	if h == nil {
		return ""
	}
	if h.Epoch != "" && h.Epoch != "0" {
		return fmt.Sprintf("%s-%s:%s-%s.%s", h.Name, h.Epoch, h.Version, h.Release, h.Arch)
	}
	return fmt.Sprintf("%s-%s-%s.%s", h.Name, h.Version, h.Release, h.Arch)
}

// ParseHeader reads one whole .rpm body and decodes its main header. The
// payload is never read: the parse stops at the end of the header region.
func ParseHeader(r io.Reader) (*Header, error) {
	br := newCountingReader(r)

	lead := make([]byte, leadSize)
	if _, err := io.ReadFull(br, lead); err != nil {
		return nil, fmt.Errorf("read lead: %w", err)
	}
	if m := binary.BigEndian.Uint32(lead[0:4]); m != leadMagic {
		return nil, fmt.Errorf("not an RPM package (lead magic %08x)", m)
	}

	// The signature region: one header structure plus alignment padding to
	// the next 8-byte boundary.
	sigStart := int64(br.n)
	sig, err := readHeaderBlock(br)
	if err != nil {
		return nil, fmt.Errorf("signature header: %w", err)
	}
	sigEnd := int64(br.n)
	if pad := (8 - (sigEnd-sigStart)%8) % 8; pad != 0 {
		if _, err := io.CopyN(io.Discard, br, pad); err != nil {
			return nil, fmt.Errorf("signature padding: %w", err)
		}
	}

	hdrStart := int64(br.n)
	store, err := readHeaderBlock(br)
	if err != nil {
		return nil, fmt.Errorf("main header: %w", err)
	}
	hdrEnd := int64(br.n)
	_ = sig // the signature's own tags are not consumed (unsigned mode)

	out := &Header{HeaderStart: hdrStart, HeaderEnd: hdrEnd}
	out.Name = store.str(tagName)
	out.Version = store.str(tagVersion)
	out.Release = store.str(tagRelease)
	out.Arch = store.str(tagArch)
	out.License = store.str(tagLicense)
	out.Vendor = store.str(tagVendor)
	out.Summary = store.str(tagSummary)
	out.Packager = store.str(tagPackager)
	out.URL = store.str(tagURL)
	out.SourceRPM = store.str(tagSourceRPM)
	// SUMMARY/DESCRIPTION/GROUP are I18NSTRING on the wire — the first
	// string of the set is the C locale's, the value the index renders.
	out.Description = store.str(tagDescription)
	out.Group = store.str(tagGroup)
	if e := store.intAt(tagEpoch, 0); e >= 0 {
		out.Epoch = strconv.FormatInt(e, 10)
	}
	if t := store.intAt(tagBuildTime, 0); t >= 0 {
		out.BuildTime = t
	}
	// LONGSIZE (v6) wins when present; SIZE is the classic 32-bit tag.
	if s, ok := store.int64First(tagLongSize); ok {
		out.Size = s
	} else if s := store.intAt(tagSize, 0); s >= 0 {
		out.Size = s
	}
	if s := store.intAt(tagArchiveSize, 0); s >= 0 {
		out.ArchiveSize = s
	}

	out.Provides = store.deps(tagProvideName, tagProvideVer, tagProvideFlags)
	out.Requires = store.deps(tagRequireName, tagRequireVersion, tagRequireFlags)
	out.Conflicts = store.deps(tagConflictName, tagConflictVer, tagConflictFlags)
	out.Obsoletes = store.deps(tagObsoleteName, tagObsoleteVer, tagObsoleteFlags)
	out.Recommends = store.deps(tagRecommendName, tagRecommendVer, tagRecommendFlg)
	out.Suggests = store.deps(tagSuggestName, tagSuggestVer, tagSuggestFlg)

	out.Files = store.files()
	out.Changelog = store.changelog()

	if out.Name == "" || out.Arch == "" {
		return nil, fmt.Errorf("package header lacks NAME or ARCH")
	}
	return out, nil
}

// ---- low-level decode ----

// countingReader tracks bytes consumed (header-range rendering).
type countingReader struct {
	r io.Reader
	n int
}

func newCountingReader(r io.Reader) *countingReader { return &countingReader{r: r} }

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// headerBlock is one parsed header structure: the index grid plus the raw
// store, with typed accessors that bounds-check every entry against the
// store's real size.
type headerBlock struct {
	entries map[int32]*indexEntry
	store   []byte
}

// indexEntry is one 16-byte grid row.
type indexEntry struct {
	typ    int32
	offset int32
	count  int32
}

// readHeaderBlock consumes one header structure from r (intro + index grid
// + store).
func readHeaderBlock(r io.Reader) (*headerBlock, error) {
	intro := make([]byte, 16)
	if _, err := io.ReadFull(r, intro); err != nil {
		return nil, fmt.Errorf("read intro: %w", err)
	}
	if intro[0] != 0x8e || intro[1] != 0xad || intro[2] != 0xe8 {
		return nil, fmt.Errorf("bad header magic %02x %02x %02x", intro[0], intro[1], intro[2])
	}
	if intro[3] != headerVer {
		return nil, fmt.Errorf("unsupported header version %d", intro[3])
	}
	//nolint:gosec // G115: the raw wire words carry as uint32 first so the
	// range checks below reject the negative reinterpretation before any
	// use — the RPM format's own upper bound is far below int32.
	nindex := int32(binary.BigEndian.Uint32(intro[8:12])) //nolint:gosec // G115: range-checked below
	hsize := int32(binary.BigEndian.Uint32(intro[12:16])) //nolint:gosec // G115: range-checked below
	if nindex < 0 || nindex > maxIndexEntries {
		return nil, fmt.Errorf("index entry count %d out of bounds", nindex)
	}
	if hsize < 0 || hsize > maxStoreBytes {
		return nil, fmt.Errorf("store size %d out of bounds", hsize)
	}
	grid := make([]byte, int(nindex)*indexEntrySz)
	if _, err := io.ReadFull(r, grid); err != nil {
		return nil, fmt.Errorf("read index grid: %w", err)
	}
	store := make([]byte, int(hsize))
	if _, err := io.ReadFull(r, store); err != nil {
		return nil, fmt.Errorf("read store: %w", err)
	}
	hb := &headerBlock{entries: make(map[int32]*indexEntry, nindex), store: store}
	for i := int32(0); i < nindex; i++ {
		row := grid[i*indexEntrySz : (i+1)*indexEntrySz]
		tag := int32(binary.BigEndian.Uint32(row[0:4])) //nolint:gosec // G115: bounds-checked by nindex's range gate
		hb.entries[tag] = &indexEntry{
			typ:    int32(binary.BigEndian.Uint32(row[4:8])),   //nolint:gosec // G115: raw tag word, compared against the closed type set
			offset: int32(binary.BigEndian.Uint32(row[8:12])),  //nolint:gosec // G115: bounds-checked against the store at every read
			count:  int32(binary.BigEndian.Uint32(row[12:16])), //nolint:gosec // G115: bounds-checked against the store at every read
		}
	}
	return hb, nil
}

// entry returns the grid row for tag (nil when absent).
func (hb *headerBlock) entry(tag int32) *indexEntry { return hb.entries[tag] }

// str reads one STRING/I18NSTRING scalar ("" when absent or malformed).
func (hb *headerBlock) str(tag int32) string {
	ss := hb.strs(tag, 1)
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

// strs reads up to want strings of a STRING/STRING_ARRAY/I18NSTRING entry
// (want < 0 = all; nil when the entry is absent).
func (hb *headerBlock) strs(tag int32, want int) []string {
	e := hb.entry(tag)
	if e == nil {
		return nil
	}
	if e.typ != typString && e.typ != typStringArray && e.typ != typI18nString {
		return nil
	}
	limit := e.count
	if e.typ == typString {
		limit = 1
	}
	if limit < 0 || limit > maxStringsOf {
		return nil
	}
	if want >= 0 && int64(limit) > int64(want) {
		limit = int32(want) //nolint:gosec // G115: want is a caller literal (1), never near int32
	}
	var out []string
	off := int(e.offset)
	for i := int32(0); i < limit; i++ {
		end, ok := hb.stringAt(off)
		if !ok {
			return out
		}
		out = append(out, string(hb.store[off:end]))
		off = end + 1
	}
	return out
}

// stringAt finds the NUL terminator of the store string at off.
func (hb *headerBlock) stringAt(off int) (int, bool) {
	if off < 0 || off > len(hb.store) {
		return 0, false
	}
	rest := hb.store[off:]
	for i, c := range rest {
		if c == 0 {
			return off + i, true
		}
	}
	return 0, false
}

// typeWidth is one scalar type's byte width (0 = variable-length).
func typeWidth(typ int32) int {
	switch typ {
	case typChar, typInt8:
		return 1
	case typInt16:
		return 2
	case typInt32:
		return 4
	case typInt64:
		return 8
	default:
		return 0
	}
}

// intAt reads the idx-th INT* element (missing or malformed → -1; callers
// treat negative as absent).
func (hb *headerBlock) intAt(tag int32, idx int) int64 {
	v, ok := hb.intAtOK(tag, idx)
	if !ok {
		return -1
	}
	return v
}

// intAtOK is intAt with the found bit.
func (hb *headerBlock) intAtOK(tag int32, idx int) (int64, bool) {
	e := hb.entry(tag)
	if e == nil || idx < 0 || int64(idx) >= int64(e.count) { //nolint:gosec // G115: idx is a loop index bounded by the array it walked
		return 0, false
	}
	w := typeWidth(e.typ)
	if w == 0 {
		return 0, false
	}
	off := int(e.offset) + idx*w
	if off < 0 || off+w > len(hb.store) {
		return 0, false
	}
	//nolint:gosec // G115: the signed reinterpretation IS the format — RPM
	// stores signed integers; widening to int64 afterwards is lossless.
	switch e.typ {
	case typChar, typInt8:
		return int64(int8(hb.store[off])), true
	case typInt16:
		return int64(int16(binary.BigEndian.Uint16(hb.store[off : off+2]))), true
	case typInt32:
		return int64(int32(binary.BigEndian.Uint32(hb.store[off : off+4]))), true
	case typInt64:
		return int64(binary.BigEndian.Uint64(hb.store[off : off+8])), true
	}
	return 0, false
}

// int64First reads the first INT64 element (the LONGSIZE companion's arm).
func (hb *headerBlock) int64First(tag int32) (int64, bool) {
	e := hb.entry(tag)
	if e == nil || e.typ != typInt64 || e.count < 1 {
		return 0, false
	}
	return hb.intAtOK(tag, 0)
}

// deps assembles one dependency group from its name/version/flags triple.
// The wire's version string is "E:V-R"; the flags array aligns by index.
func (hb *headerBlock) deps(nameTag, verTag, flagsTag int32) []Dependency {
	names := hb.strs(nameTag, -1)
	if len(names) == 0 {
		return nil
	}
	vers := hb.strs(verTag, -1)
	fe := hb.entry(flagsTag)
	out := make([]Dependency, 0, len(names))
	for i, name := range names {
		var d Dependency
		d.Name = name
		if i < len(vers) {
			d.Epoch, d.Version, d.Release = parseEVR(vers[i])
		}
		if fe != nil && fe.typ == typInt32 {
			if v, ok := hb.intAtOK(flagsTag, i); ok {
				d.Flags = uint32(v) //nolint:gosec // G115: the sense-flag word, masked against the known bits at render
			}
		}
		out = append(out, d)
	}
	return out
}

// files reconstructs the file list from BASENAMES × DIRINDEXES × DIRNAMES.
// Mismatched arrays (a corrupt header) yield a nil list, never a panic.
func (hb *headerBlock) files() []string {
	bases := hb.strs(tagBaseNames, -1)
	idxs := hb.ints(tagDirIndexes)
	dirs := hb.strs(tagDirNames, -1)
	if len(bases) == 0 || len(bases) != len(idxs) {
		return nil
	}
	out := make([]string, 0, len(bases))
	for i, base := range bases {
		di := int(idxs[i])
		if di < 0 || di >= len(dirs) {
			continue
		}
		out = append(out, dirs[di]+base)
	}
	return out
}

// changelog pairs the TIME/NAME/TEXT arrays (min length governs).
func (hb *headerBlock) changelog() []ChangelogEntry {
	times := hb.ints(tagChangelogTime)
	names := hb.strs(tagChangelogName, -1)
	texts := hb.strs(tagChangelogText, -1)
	n := len(names)
	if len(times) < n {
		n = len(times)
	}
	if len(texts) < n {
		n = len(texts)
	}
	if n == 0 {
		return nil
	}
	out := make([]ChangelogEntry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ChangelogEntry{Time: times[i], Name: names[i], Text: texts[i]})
	}
	return out
}

// ints reads a whole INT32 array entry.
func (hb *headerBlock) ints(tag int32) []int64 {
	e := hb.entry(tag)
	if e == nil || e.typ != typInt32 || e.count < 0 || e.count > maxStringsOf {
		return nil
	}
	out := make([]int64, 0, e.count)
	for i := int32(0); i < e.count; i++ {
		if v, ok := hb.intAtOK(tag, int(i)); ok {
			out = append(out, v)
		}
	}
	return out
}

// parseEVR splits one dependency version string "E:V-R" into its parts
// (epoch/release empty when the string carries neither; the callers render
// the XML default epoch "0" themselves).
func parseEVR(s string) (epoch, version, release string) {
	if s == "" {
		return "", "", ""
	}
	rest := s
	if i := strings.IndexByte(s, ':'); i > 0 && isDigits(s[:i]) {
		epoch, rest = s[:i], s[i+1:]
	}
	if i := strings.LastIndexByte(rest, '-'); i >= 0 {
		version, release = rest[:i], rest[i+1:]
	} else {
		version = rest
	}
	return epoch, version, release
}

// isDigits reports a non-empty all-digits string.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// senseString renders the flags word as primary.xml's LT/LE/GT/GE/EQ
// spelling ("" when the entry carries no comparison sense).
func senseString(flags uint32) string {
	less := flags&senseLess != 0
	greater := flags&senseGreater != 0
	equal := flags&senseEqual != 0
	switch {
	case less && equal:
		return "LE"
	case greater && equal:
		return "GE"
	case less:
		return "LT"
	case greater:
		return "GT"
	case equal:
		return "EQ"
	default:
		return ""
	}
}

// isPreDep reports the pre="1" marker's flag set (the classic PREREQ bit
// plus the v3 SCRIPT_PRE/SCRIPT_POST bits createrepo renders as pre).
func isPreDep(flags uint32) bool {
	return flags&(sensePreReq|senseScriptPre|senseScriptPost) != 0
}
