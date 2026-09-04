package cargo

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

// The deframer table (spec section 5.1's byte-for-byte frame), under
// CG-2's two failure families: the legal frame round-trips; the SHAPE
// defects (a truncated length prefix, an absurd declared length, trailing
// bytes) are errFramingDefect (the 500 family, Artifactory's uncaught
// RuntimeException); every truncation the stream's own END produces (a
// frame that declares more than the body carries) is the plain-error
// IOException track (the 200 + warnings.other arm).
func TestDecodePublishFrame(t *testing.T) {
	meta := `{"name":"mycrate","vers":"0.1.0"}`
	crate := []byte("pretend this is a gzipped tarball")

	t.Run("legal", func(t *testing.T) {
		rawJSON, spool, err := decodePublishFrame(t.TempDir(), bytes.NewReader(publishBody(meta, crate)))
		if err != nil {
			t.Fatalf("decodePublishFrame: %v", err)
		}
		defer func() { _ = os.Remove(spool) }()
		if string(rawJSON) != meta {
			t.Errorf("metadata = %q, want %q", rawJSON, meta)
		}
		got, rerr := os.ReadFile(spool)
		if rerr != nil {
			t.Fatalf("read spool: %v", rerr)
		}
		if !bytes.Equal(got, crate) {
			t.Errorf("crate = %q, want %q", got, crate)
		}
	})

	t.Run("empty metadata frame is legal", func(t *testing.T) {
		// A zero-length metadata frame parses as JSON "" — parsePublishMeta
		// rejects it; the deframer itself must carry it through.
		body := append(uint32le(0), publishBodyBody(crate)...)
		rawJSON, spool, err := decodePublishFrame(t.TempDir(), bytes.NewReader(body))
		if err != nil {
			t.Fatalf("decodePublishFrame: %v", err)
		}
		defer func() { _ = os.Remove(spool) }()
		if len(rawJSON) != 0 {
			t.Errorf("metadata = %q, want empty", rawJSON)
		}
	})

	defects := []struct {
		name string
		body []byte
	}{
		{"empty body", nil},
		{"truncated metadata length prefix", uint32le(9)[:2]},
		{"missing crate length prefix", func() []byte {
			b := uint32le(uint32(len(meta)))
			return append(b, meta...)
		}()},
		{"trailing byte after crate", func() []byte {
			b := publishBody(meta, crate)
			return append(b, '\n')
		}()},
		{"metadata frame over the cap", func() []byte {
			return append(uint32le(uint32(maxMetaJSON+1)), make([]byte, 16)...)
		}()},
	}
	for _, tc := range defects {
		t.Run(tc.name, func(t *testing.T) {
			_, spool, err := decodePublishFrame(t.TempDir(), bytes.NewReader(tc.body))
			if !errors.Is(err, errFramingDefect) {
				t.Fatalf("decodePublishFrame err = %v, want errFramingDefect (the 500 family)", err)
			}
			if spool != "" {
				t.Errorf("spool = %q, want empty on failure", spool)
			}
		})
	}

	truncations := []struct {
		name string
		body []byte
	}{
		{"metadata length overshoots body", func() []byte {
			b := uint32le(uint32(len(meta)))
			return append(b, meta[:len(meta)-3]...)
		}()},
		{"crate length overshoots body", func() []byte {
			b := append(uint32le(uint32(len(meta))), meta...)
			b = append(b, uint32le(uint32(len(crate)+10))...)
			return append(b, crate...)
		}()},
	}
	for _, tc := range truncations {
		t.Run(tc.name, func(t *testing.T) {
			_, spool, err := decodePublishFrame(t.TempDir(), bytes.NewReader(tc.body))
			if err == nil || errors.Is(err, errFramingDefect) {
				t.Fatalf("decodePublishFrame err = %v, want a plain truncation error (the 200 family)", err)
			}
			if spool != "" {
				t.Errorf("spool = %q, want empty on failure", spool)
			}
		})
	}
}

// publishBodyBody frames one crate-only tail (the metadata frame of
// length 0 already consumed — see the empty-metadata case).
func publishBodyBody(crate []byte) []byte {
	var out bytes.Buffer
	out.Write(uint32le(uint32(len(crate))))
	out.Write(crate)
	return out.Bytes()
}

// parsePublishMeta validation table (the spec's name + SemVer gates).
func TestParsePublishMeta(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{"minimal", `{"name":"mycrate","vers":"0.1.0"}`, false},
		{"full", `{"name":"mycrate","vers":"1.0.0-alpha.1+b","deps":[],"features":{},"description":"d","keywords":["k"],"categories":["c"],"links":"repo"}`, false},
		{"not json", `nonsense`, true},
		{"bad name", `{"name":"1bad","vers":"0.1.0"}`, true},
		{"bad version", `{"name":"mycrate","vers":"0.1"}`, true},
		{"missing name", `{"vers":"0.1.0"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePublishMeta([]byte(tc.json))
			if (err != nil) != tc.wantErr {
				t.Fatalf("parsePublishMeta err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// crateProps table (spec section 4: name/version always, the descriptive
// family only when present, keywords/categories ';'-joined).
func TestCrateProps(t *testing.T) {
	t.Run("minimal", func(t *testing.T) {
		m := &publishMeta{Name: "mycrate", Vers: "0.1.0"}
		props := crateProps(m)
		if got := props[propName]; len(got) != 1 || got[0] != "mycrate" {
			t.Errorf("crate.name = %v", got)
		}
		if got := props[propVersion]; len(got) != 1 || got[0] != "0.1.0" {
			t.Errorf("crate.version = %v", got)
		}
		for _, key := range []string{propDescription, propKeywords, propCategories} {
			if _, ok := props[key]; ok {
				t.Errorf("absent fact %q must be an absent key", key)
			}
		}
	})

	t.Run("full", func(t *testing.T) {
		m := &publishMeta{
			Name:        "mycrate",
			Vers:        "0.1.0",
			Description: "demo crate",
			Keywords:    []string{"demo", "test"},
			Categories:  []string{"cli"},
		}
		props := crateProps(m)
		if got := firstProp(props, propDescription); got != "demo crate" {
			t.Errorf("crate.description = %q", got)
		}
		if got := firstProp(props, propKeywords); got != "demo;test" {
			t.Errorf("crate.keywords = %q, want 'demo;test'", got)
		}
		if got := firstProp(props, propCategories); got != "cli" {
			t.Errorf("crate.categories = %q", got)
		}
		// The ';' convention round-trips.
		joined := splitSemi(firstProp(props, propKeywords))
		if len(joined) != 2 || joined[0] != "demo" || joined[1] != "test" {
			t.Errorf("splitSemi = %v", joined)
		}
	})
}
