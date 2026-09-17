package deb

// L027-3 (D02 config, debian.md // 2.1 errata + d02-config/deb-package-
// conditional-keys): the debian LOCAL row's package-type-conditional
// configuration seats, pinned against the reference wire (pro 7.161.15
// live probes L026-5 l026r-optdeb-* + L027-3 l027x-*, A-side assets
// deleted after capture). The measured faces:
//
//	v1 (+ v2 batch, same projection)  deb 62 keys / generic 61
//	v2 single + configurations        deb 21 keys / generic 18
//
// The deb deltas: v1 gains optionalIndexCompressionFormats (default []
// — an empty JSON array, never null); the narrower faces additionally
// gain ddebSupported and debianTrivialLayout (shared v1 seats, deb-
// conditional on v2/configurations); enableDebianSupport defaults true
// on deb rows alone. Stored blob values win over every default. The
// rows seed directly through the metadata store (the license gate owns
// the write plane, not this read face; the dev-instance community gate
// is why the reference's own deb face needed the licensed A instance).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// seedConfigRow writes one repository row directly (any package type —
// the config read face renders stored rows, however they landed).
func (s *stack) seedConfigRow(t *testing.T, key, packageType, config string) {
	t.Helper()
	if err := s.md.Repos().Create(context.Background(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeLocal, PackageType: packageType, Config: config,
	}); err != nil {
		t.Fatalf("seed repo %s: %v", key, err)
	}
}

// TestDebConfigFaceConditionalKeys: the 62/21-key deb faces against the
// 61/18-key generic control, on every admin configuration read face.
func TestDebConfigFaceConditionalKeys(t *testing.T) {
	s := newStack(t)
	s.seedConfigRow(t, "deb-face", Protocol, "{}")
	s.seedConfigRow(t, "gen-face", "generic", "{}")

	getMap := func(path string) map[string]any {
		t.Helper()
		status, body, _ := s.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, status, body)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return m
	}
	configRow := func(debKey, genKey string) (map[string]any, map[string]any) {
		m := getMap("/binflow/api/repositories/configurations")
		var deb, gen map[string]any
		for _, e := range m["LOCAL"].([]any) {
			row := e.(map[string]any)
			if row["key"] == debKey {
				deb = row
			}
			if row["key"] == genKey {
				gen = row
			}
		}
		if deb == nil || gen == nil {
			t.Fatalf("configurations lacks %s/%s", debKey, genKey)
		}
		return deb, gen
	}
	batchRow := func(key string) map[string]any {
		m := getMap("/binflow/api/v2/repositories/batch?names=" + key)
		v, _ := m[key].(map[string]any)
		if v == nil {
			t.Fatalf("batch body lacks %s: %v", key, m)
		}
		return v
	}

	tests := []struct {
		label            string
		deb, gen         map[string]any
		wantDeb, wantGen int
		wantDebOnly      []string // deb face minus generic face
		dialect          string   // rclass on v1/batch/configurations, type on v2
	}{
		{
			label:   "v1 deb 62 vs generic 61",
			deb:     getMap("/binflow/api/repositories/deb-face"),
			gen:     getMap("/binflow/api/repositories/gen-face"),
			wantDeb: 62, wantGen: 61,
			wantDebOnly: []string{"optionalIndexCompressionFormats"},
			dialect:     "rclass",
		},
		{
			label:   "v2 deb 21 vs generic 18",
			deb:     getMap("/binflow/api/v2/repositories/deb-face"),
			gen:     getMap("/binflow/api/v2/repositories/gen-face"),
			wantDeb: 21, wantGen: 18,
			wantDebOnly: []string{"ddebSupported", "debianTrivialLayout", "optionalIndexCompressionFormats"},
			dialect:     "type",
		},
		{
			label:   "configurations deb 21 vs generic 18",
			deb:     func() map[string]any { d, _ := configRow("deb-face", "gen-face"); return d }(),
			gen:     func() map[string]any { _, g := configRow("deb-face", "gen-face"); return g }(),
			wantDeb: 21, wantGen: 18,
			wantDebOnly: []string{"ddebSupported", "debianTrivialLayout", "optionalIndexCompressionFormats"},
			dialect:     "rclass",
		},
		{
			label:   "batch = v1 projection deb 62",
			deb:     batchRow("deb-face"),
			gen:     batchRow("gen-face"),
			wantDeb: 62, wantGen: 61,
			wantDebOnly: []string{"optionalIndexCompressionFormats"},
			dialect:     "rclass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if len(tt.deb) != tt.wantDeb {
				t.Errorf("deb face = %d keys, want %d", len(tt.deb), tt.wantDeb)
			}
			if len(tt.gen) != tt.wantGen {
				t.Errorf("generic face = %d keys, want %d", len(tt.gen), tt.wantGen)
			}
			var only []string
			for k := range tt.deb {
				if _, ok := tt.gen[k]; !ok {
					only = append(only, k)
				}
			}
			sort.Strings(only)
			if fmt.Sprint(only) != fmt.Sprint(tt.wantDebOnly) {
				t.Errorf("deb-only keys = %v, want %v", only, tt.wantDebOnly)
			}
			if tt.deb[tt.dialect] != repo.TypeLocal {
				t.Errorf("dialect key %s = %v", tt.dialect, tt.deb[tt.dialect])
			}
		})
	}

	t.Run("conditional key default shapes", func(t *testing.T) {
		v1 := getMap("/binflow/api/repositories/deb-face")
		oicf, ok := v1["optionalIndexCompressionFormats"].([]any)
		if !ok || oicf == nil || len(oicf) != 0 {
			t.Errorf("v1 optionalIndexCompressionFormats = %#v, want empty JSON array", v1["optionalIndexCompressionFormats"])
		}
		if v1["enableDebianSupport"] != true {
			t.Errorf("v1 enableDebianSupport = %v, want true (deb default)", v1["enableDebianSupport"])
		}
		if v1["ddebSupported"] != false || v1["debianTrivialLayout"] != false {
			t.Errorf("v1 shared deb seats = %v/%v, want false/false", v1["ddebSupported"], v1["debianTrivialLayout"])
		}
		gen := getMap("/binflow/api/repositories/gen-face")
		if gen["enableDebianSupport"] != false {
			t.Errorf("generic enableDebianSupport = %v, want false", gen["enableDebianSupport"])
		}
		if _, has := gen["optionalIndexCompressionFormats"]; has {
			t.Error("generic face carries optionalIndexCompressionFormats")
		}
		v2 := getMap("/binflow/api/v2/repositories/deb-face")
		if v2["ddebSupported"] != false || v2["debianTrivialLayout"] != false {
			t.Errorf("v2 deb pair = %v/%v, want false/false", v2["ddebSupported"], v2["debianTrivialLayout"])
		}
		if _, has := v2["enableDebianSupport"]; has {
			t.Error("v2 face carries enableDebianSupport (narrower set)")
		}
	})
}

// TestDebConfigFaceSetValuesWin: a stored deb config blob overrides every
// conditional default — the reference's set-value arm (["bz2"] echoes
// verbatim, key count unchanged); enableDebianSupport follows the stored
// value (the reference ignoring an explicit update-to-false is a write-
// plane divergence registered in the L027-3 report, not this read face).
func TestDebConfigFaceSetValuesWin(t *testing.T) {
	s := newStack(t)
	s.seedConfigRow(t, "deb-set", Protocol,
		`{"optionalIndexCompressionFormats":["bz2","xz"],"enableDebianSupport":false,`+
			`"ddebSupported":true,"debianTrivialLayout":true}`)

	getMap := func(path string) map[string]any {
		t.Helper()
		status, body, _ := s.do(http.MethodGet, path, adminUser, adminPass, nil, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d body=%s", path, status, body)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return m
	}

	tests := []struct {
		label    string
		m        map[string]any
		wantKeys int
	}{
		{"v1 set arm", getMap("/binflow/api/repositories/deb-set"), 62},
		// 22 = the 21 seats + the stored enableDebianSupport riding the
		// unmodeled-blob echo (it is a v1-family seat only; L026-3's echo
		// contract adds stored non-seat keys verbatim).
		{"v2 set arm", getMap("/binflow/api/v2/repositories/deb-set"), 22},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if len(tt.m) != tt.wantKeys {
				t.Errorf("face = %d keys, want %d (set values change no key count)", len(tt.m), tt.wantKeys)
			}
			if got := fmt.Sprint(tt.m["optionalIndexCompressionFormats"]); got != "[bz2 xz]" {
				t.Errorf("optionalIndexCompressionFormats = %v, want [bz2 xz]", tt.m["optionalIndexCompressionFormats"])
			}
		})
	}
	v1, v2 := getMap("/binflow/api/repositories/deb-set"), getMap("/binflow/api/v2/repositories/deb-set")
	if v1["enableDebianSupport"] != false {
		t.Errorf("v1 enableDebianSupport = %v, want false (stored wins)", v1["enableDebianSupport"])
	}
	if v2["ddebSupported"] != true || v2["debianTrivialLayout"] != true {
		t.Errorf("v2 deb pair = %v/%v, want true/true (stored wins)", v2["ddebSupported"], v2["debianTrivialLayout"])
	}
}
