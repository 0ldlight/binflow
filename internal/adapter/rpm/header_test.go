package rpm

// Table-driven coverage for the self-built RPM header parser (rpm.md
// section 2.4): the fixtures are HAND-BUILT binaries — the lead + signature
// header + padding + main header + payload container assembled by this
// file — so every tag type and every six-group shape is exercised without
// any external binary blob checked into the tree.

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// ---- the fixture builder ----

type fixtureEntry struct {
	tag, typ, count int32
	payload         []byte
}

// headerBuilder assembles one header structure.
type headerBuilder struct{ entries []fixtureEntry }

func (b *headerBuilder) raw(tag, typ int32, payload []byte, count int32) {
	b.entries = append(b.entries, fixtureEntry{tag: tag, typ: typ, count: count, payload: payload})
}

func (b *headerBuilder) str(tag int32, v string) { b.raw(tag, typString, append([]byte(v), 0), 1) }

func (b *headerBuilder) strArr(tag int32, vs ...string) {
	var p []byte
	for _, v := range vs {
		p = append(p, []byte(v)...)
		p = append(p, 0)
	}
	b.raw(tag, typStringArray, p, int32(len(vs)))
}

func (b *headerBuilder) i18n(tag int32, vs ...string) {
	var p []byte
	for _, v := range vs {
		p = append(p, []byte(v)...)
		p = append(p, 0)
	}
	b.raw(tag, typI18nString, p, int32(len(vs)))
}

func (b *headerBuilder) i32arr(tag int32, vs ...int32) {
	p := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint32(p[i*4:], uint32(v))
	}
	b.raw(tag, typInt32, p, int32(len(vs)))
}

func (b *headerBuilder) i64arr(tag int32, vs ...int64) {
	p := make([]byte, 8*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint64(p[i*8:], uint64(v))
	}
	b.raw(tag, typInt64, p, int32(len(vs)))
}

// bytes renders the header structure: 16-byte intro, the 16-byte grid, the
// store.
func (b *headerBuilder) bytes() []byte {
	var store []byte
	grid := make([]byte, 0, 16*len(b.entries))
	for _, e := range b.entries {
		offset := len(store)
		store = append(store, e.payload...)
		row := make([]byte, 16)
		binary.BigEndian.PutUint32(row[0:4], uint32(e.tag))
		binary.BigEndian.PutUint32(row[4:8], uint32(e.typ))
		binary.BigEndian.PutUint32(row[8:12], uint32(offset))
		binary.BigEndian.PutUint32(row[12:16], uint32(e.count))
		grid = append(grid, row...)
	}
	out := make([]byte, 16, 16+len(grid)+len(store))
	out[0], out[1], out[2], out[3] = 0x8e, 0xad, 0xe8, 0x01
	binary.BigEndian.PutUint32(out[8:12], uint32(len(b.entries)))
	binary.BigEndian.PutUint32(out[12:16], uint32(len(store)))
	out = append(out, grid...)
	out = append(out, store...)
	return out
}

// fixtureRPM wraps header structures into a whole .rpm body: lead +
// signature header (zero entries — the parser skips the region; a real
// signature's own tags are not consumed in unsigned mode) + 8-byte
// alignment padding + the main header + a dummy payload.
func fixtureRPM(name string, sig *headerBuilder, main *headerBuilder, payload []byte) []byte {
	if sig == nil {
		sig = &headerBuilder{}
	}
	lead := make([]byte, leadSize)
	lead[0], lead[1], lead[2], lead[3] = 0xed, 0xab, 0xee, 0xdb
	lead[4], lead[5] = 3, 0
	copy(lead[8:8+66], name)
	out := append(lead, sig.bytes()...)
	if pad := (8 - len(out)%8) % 8; pad != 0 {
		out = append(out, make([]byte, pad)...)
	}
	out = append(out, main.bytes()...)
	return append(out, payload...)
}

// fullHeader is the canonical multi-shape header: identity tags, all six
// dependency groups, the file-list triple, the changelog triple.
func fullHeader() *headerBuilder {
	b := &headerBuilder{}
	b.str(tagName, "mypkg")
	b.str(tagVersion, "1.0.0")
	b.str(tagRelease, "1.el9")
	b.i32arr(tagEpoch, 1)
	b.str(tagArch, "x86_64")
	b.str(tagLicense, "MIT and GPLv2+")
	b.str(tagVendor, "BinFlow Test Labs")
	b.i18n(tagSummary, "A test package")
	b.i18n(tagDescription, "Longer description\nsecond line")
	b.i18n(tagGroup, "Unspecified")
	b.str(tagURL, "https://example.com/mypkg")
	b.str(tagPackager, "Tester <tester@example.com>")
	b.str(tagSourceRPM, "mypkg-1.0.0-1.el9.src.rpm")
	b.i32arr(tagBuildTime, 1735689600)
	b.i32arr(tagSize, 4096)
	b.i32arr(tagArchiveSize, 8192)

	b.strArr(tagProvideName, "mypkg", "libmypkg.so.1()(64bit)")
	b.strArr(tagProvideVer, "1:1.0.0-1.el9", "1.0.0")
	b.i32arr(tagProvideFlags, senseEqual, senseEqual|senseGreater)
	b.strArr(tagRequireName, "libc.so.6(GLIBC_2.34)(64bit)", "/bin/sh", "rpmlib(CompressedFileNames)")
	b.strArr(tagRequireVersion, "2.34-58.el9", "", "3.0.4-1")
	b.i32arr(tagRequireFlags, senseGreater|senseEqual, 0, senseGreater|senseEqual|sensePreReq)
	b.strArr(tagConflictName, "otherpkg")
	b.strArr(tagConflictVer, "0.9")
	b.i32arr(tagConflictFlags, senseLess)
	b.strArr(tagObsoleteName, "oldpkg")
	b.strArr(tagObsoleteVer, "0.1")
	b.i32arr(tagObsoleteFlags, senseLess|senseEqual)
	b.strArr(tagRecommendName, "enhancepkg")
	b.strArr(tagRecommendVer, "2:1.0")
	b.i32arr(tagRecommendFlg, senseEqual)
	b.strArr(tagSuggestName, "nicepkg")
	b.strArr(tagSuggestVer, "3.0-2")
	b.i32arr(tagSuggestFlg, senseGreater)

	b.strArr(tagDirNames, "/usr/bin/", "/usr/share/mypkg/")
	b.i32arr(tagDirIndexes, 0, 1, 1)
	b.strArr(tagBaseNames, "mypkg", "README.md", "data.txt")

	b.i32arr(tagChangelogTime, 1735600000, 1735500000)
	b.strArr(tagChangelogName, "Tester <tester@example.com>", "Tester <tester@example.com>")
	b.strArr(tagChangelogText, "- initial build", "- wip")
	return b
}

// ---- identity / NEVRA ----

func TestParseHeaderIdentity(t *testing.T) {
	h, err := ParseHeader(bytes.NewReader(fixtureRPM("mypkg-1.0.0-1.el9.x86_64", nil, fullHeader(), []byte("payload"))))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	want := map[string]string{
		"Name": "mypkg", "Version": "1.0.0", "Release": "1.el9", "Epoch": "1",
		"Arch": "x86_64", "License": "MIT and GPLv2+", "Vendor": "BinFlow Test Labs",
		"Summary": "A test package", "Group": "Unspecified", "URL": "https://example.com/mypkg",
		"Packager": "Tester <tester@example.com>", "SourceRPM": "mypkg-1.0.0-1.el9.src.rpm",
	}
	got := map[string]string{
		"Name": h.Name, "Version": h.Version, "Release": h.Release, "Epoch": h.Epoch,
		"Arch": h.Arch, "License": h.License, "Vendor": h.Vendor,
		"Summary": h.Summary, "Group": h.Group, "URL": h.URL,
		"Packager": h.Packager, "SourceRPM": h.SourceRPM,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	if h.NEVRA() != "mypkg-1:1.0.0-1.el9.x86_64" {
		t.Errorf("NEVRA = %q", h.NEVRA())
	}
	if h.BuildTime != 1735689600 || h.Size != 4096 || h.ArchiveSize != 8192 {
		t.Errorf("numeric fields = build %d size %d archive %d", h.BuildTime, h.Size, h.ArchiveSize)
	}
}

func TestParseHeaderEpochZeroNEVRAShort(t *testing.T) {
	b := fullHeader()
	b.i32arr(tagEpoch, 0)
	h, err := ParseHeader(bytes.NewReader(fixtureRPM("x", nil, b, nil)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.NEVRA() != "mypkg-1.0.0-1.el9.x86_64" {
		t.Errorf("epoch-0 NEVRA = %q", h.NEVRA())
	}
}

// ---- the six dependency groups ----

func TestParseHeaderDependencyGroups(t *testing.T) {
	h, err := ParseHeader(bytes.NewReader(fixtureRPM("x", nil, fullHeader(), nil)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	type want struct {
		group string
		d     Dependency
	}
	tests := []want{
		{"Provides", Dependency{Name: "mypkg", Flags: senseEqual, Epoch: "1", Version: "1.0.0", Release: "1.el9"}},
		{"Provides", Dependency{Name: "libmypkg.so.1()(64bit)", Flags: senseEqual | senseGreater, Version: "1.0.0"}},
		{"Requires", Dependency{Name: "libc.so.6(GLIBC_2.34)(64bit)", Flags: senseGreater | senseEqual, Version: "2.34", Release: "58.el9"}},
		{"Requires", Dependency{Name: "/bin/sh", Flags: 0}},
		{"Requires", Dependency{Name: "rpmlib(CompressedFileNames)", Flags: senseGreater | senseEqual | sensePreReq, Version: "3.0.4", Release: "1"}},
		{"Conflicts", Dependency{Name: "otherpkg", Flags: senseLess, Version: "0.9"}},
		{"Obsoletes", Dependency{Name: "oldpkg", Flags: senseLess | senseEqual, Version: "0.1"}},
		{"Recommends", Dependency{Name: "enhancepkg", Flags: senseEqual, Epoch: "2", Version: "1.0"}},
		{"Suggests", Dependency{Name: "nicepkg", Flags: senseGreater, Version: "3.0", Release: "2"}},
	}
	for _, w := range tests {
		var got []Dependency
		switch w.group {
		case "Provides":
			got = h.Provides
		case "Requires":
			got = h.Requires
		case "Conflicts":
			got = h.Conflicts
		case "Obsoletes":
			got = h.Obsoletes
		case "Recommends":
			got = h.Recommends
		case "Suggests":
			got = h.Suggests
		}
		found := false
		for _, d := range got {
			if d == w.d {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s missing %+v (have %+v)", w.group, w.d, got)
		}
	}
	// Group sizes: the fixture's exact counts.
	if len(h.Provides) != 2 || len(h.Requires) != 3 || len(h.Conflicts) != 1 ||
		len(h.Obsoletes) != 1 || len(h.Recommends) != 1 || len(h.Suggests) != 1 {
		t.Errorf("group sizes = %d/%d/%d/%d/%d/%d",
			len(h.Provides), len(h.Requires), len(h.Conflicts), len(h.Obsoletes), len(h.Recommends), len(h.Suggests))
	}
}

func TestParseHeaderFilesAndChangelog(t *testing.T) {
	h, err := ParseHeader(bytes.NewReader(fixtureRPM("x", nil, fullHeader(), nil)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	wantFiles := []string{"/usr/bin/mypkg", "/usr/share/mypkg/README.md", "/usr/share/mypkg/data.txt"}
	if len(h.Files) != len(wantFiles) {
		t.Fatalf("files = %v, want %v", h.Files, wantFiles)
	}
	for i, w := range wantFiles {
		if h.Files[i] != w {
			t.Errorf("files[%d] = %q, want %q", i, h.Files[i], w)
		}
	}
	if len(h.Changelog) != 2 || h.Changelog[0].Time != 1735600000 ||
		h.Changelog[0].Name != "Tester <tester@example.com>" || h.Changelog[0].Text != "- initial build" {
		t.Errorf("changelog = %+v", h.Changelog)
	}
}

// ---- v6 LONGSIZE and the signature-region padding ----

func TestParseHeaderLongSizeWins(t *testing.T) {
	b := fullHeader()
	b.i64arr(tagLongSize, 1<<33)
	h, err := ParseHeader(bytes.NewReader(fixtureRPM("x", nil, b, nil)))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.Size != 1<<33 {
		t.Errorf("LONGSIZE not honored: size = %d, want %d", h.Size, int64(1)<<33)
	}
}

func TestParseHeaderSignaturePadding(t *testing.T) {
	// A signature region with ONE string entry renders 16 + 16 + N bytes —
	// with N not 8-aligned the parser must skip the alignment padding to
	// find the main header's magic.
	sig := &headerBuilder{}
	sig.str(1000, "sig") // store of 4 bytes → region ends unaligned
	body := fixtureRPM("x", sig, fullHeader(), nil)
	h, err := ParseHeader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if h.Name != "mypkg" {
		t.Fatalf("main header misplaced: name = %q", h.Name)
	}
	// The header range must land inside the real body and cover the header.
	if h.HeaderStart <= 0 || h.HeaderEnd <= h.HeaderStart || h.HeaderEnd > int64(len(body)) {
		t.Errorf("header range = %d..%d (body %d)", h.HeaderStart, h.HeaderEnd, len(body))
	}
}

// ---- malformed shapes ----

func TestParseHeaderMalformed(t *testing.T) {
	valid := fixtureRPM("x", nil, fullHeader(), []byte("p"))
	tests := []struct {
		name string
		body []byte
		err  string
	}{
		{"empty", nil, "read lead"},
		{"truncated lead", valid[:40], "read lead"},
		{"not rpm", append([]byte{1, 2, 3, 4}, valid[4:]...), "not an RPM"},
		{"truncated header", valid[:len(valid)-10], "store"},
	}
	for _, tt := range tests {
		_, err := ParseHeader(bytes.NewReader(tt.body))
		if err == nil || !strings.Contains(err.Error(), tt.err) {
			t.Errorf("%s: err = %v, want it to mention %q", tt.name, err, tt.err)
		}
	}

	// A header whose index grid claims an absurd entry count is refused
	// before allocation.
	evil := make([]byte, leadSize)
	copy(evil, []byte{0xed, 0xab, 0xee, 0xdb})
	evil = append(evil, 0x8e, 0xad, 0xe8, 0x01, 0, 0, 0, 0)
	evil = binary.BigEndian.AppendUint32(evil, 0xfffffff) // nindex
	evil = binary.BigEndian.AppendUint32(evil, 16)
	if _, err := ParseHeader(bytes.NewReader(evil)); err == nil || !strings.Contains(err.Error(), "out of bounds") {
		t.Errorf("hostile nindex: err = %v", err)
	}

	// Missing identity: NAME absent.
	noName := &headerBuilder{}
	noName.str(tagVersion, "1")
	noName.str(tagArch, "x86_64")
	if _, err := ParseHeader(bytes.NewReader(fixtureRPM("x", nil, noName, nil))); err == nil ||
		!strings.Contains(err.Error(), "NAME or ARCH") {
		t.Errorf("missing NAME: err = %v", err)
	}

	// A store offset pointing past the store yields absent strings, not a
	// panic.
	offGrid := &headerBuilder{}
	offGrid.raw(tagName, typString, []byte("x"), 1)
	raw := offGrid.bytes()
	binary.BigEndian.PutUint32(raw[16+8:16+12], 0x7ffffff0) // offset past the store
	body := fixtureRPM("x", nil, &headerBuilder{}, nil)
	body = append(body[:len(body):len(body)], raw...)
	if h, err := ParseHeader(bytes.NewReader(body)); err == nil {
		if h.Name != "" {
			t.Errorf("out-of-store offset returned %q", h.Name)
		}
	} else {
		t.Logf("out-of-store offset: %v", err)
	}
}

// ---- EVR and sense spellings ----

func TestParseEVR(t *testing.T) {
	tests := []struct {
		in                      string
		epoch, version, release string
	}{
		{"", "", "", ""},
		{"1.0", "", "1.0", ""},
		{"1.0-2.el9", "", "1.0", "2.el9"},
		{"2:1.0-2", "2", "1.0", "2"},
		{"2:1.0", "2", "1.0", ""},
		{"not:an:epoch-1", "", "not:an:epoch", "1"}, // non-digit prefix is no epoch
	}
	for _, tt := range tests {
		e, v, r := parseEVR(tt.in)
		if e != tt.epoch || v != tt.version || r != tt.release {
			t.Errorf("parseEVR(%q) = %q/%q/%q, want %q/%q/%q",
				tt.in, e, v, r, tt.epoch, tt.version, tt.release)
		}
	}
}

func TestSenseString(t *testing.T) {
	tests := []struct {
		flags uint32
		want  string
	}{
		{0, ""},
		{senseLess, "LT"},
		{senseGreater, "GT"},
		{senseEqual, "EQ"},
		{senseLess | senseEqual, "LE"},
		{senseGreater | senseEqual, "GE"},
		{senseLess | senseGreater | senseEqual, "LE"}, // LESS wins the LE branch first
	}
	for _, tt := range tests {
		if got := senseString(tt.flags); got != tt.want {
			t.Errorf("senseString(%#x) = %q, want %q", tt.flags, got, tt.want)
		}
	}
	if !isPreDep(sensePreReq) || !isPreDep(senseScriptPost) || isPreDep(0) {
		t.Error("isPreDep misclassifies the pre family")
	}
}
