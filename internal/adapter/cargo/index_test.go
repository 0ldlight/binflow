package cargo

import (
	"encoding/json"
	"strings"
	"testing"
)

// The index line schema (spec section 3.3): field set, the deps/features
// normalization, yanked always rendered, links omitted when absent.
func TestIndexLineSchema(t *testing.T) {
	t.Run("full row", func(t *testing.T) {
		l := indexLine{
			Name:     "mycrate",
			Vers:     "0.1.0",
			Deps:     json.RawMessage(`[{"name":"serde","req":"^1","features":[],"optional":false,"default_features":true,"kind":"normal","registry":"https://github.com/rust-lang/crates.io-index"}]`),
			Cksum:    "aa11",
			Features: json.RawMessage(`{"default":["std"]}`),
			Links:    "repo",
		}
		l.normalizeDepsFeatures()
		row, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, token := range []string{`"name":"mycrate"`, `"vers":"0.1.0"`, `"cksum":"aa11"`, `"yanked":false`, `"links":"repo"`, `"deps":[`, `"features":{"default":["std"]}`} {
			if !strings.Contains(string(row), token) {
				t.Errorf("row %s misses %s", row, token)
			}
		}
	})

	t.Run("minimal row normalizes deps and features", func(t *testing.T) {
		l := indexLine{Name: "a", Vers: "1.0.0", Cksum: "bb22"}
		l.normalizeDepsFeatures()
		row, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		for _, token := range []string{`"deps":[]`, `"features":{}`, `"yanked":false`} {
			if !strings.Contains(string(row), token) {
				t.Errorf("row %s misses %s", row, token)
			}
		}
		if strings.Contains(string(row), "links") {
			t.Errorf("row %s must omit an empty links", row)
		}
		if strings.Contains(string(row), `"v"`) {
			t.Errorf("row %s must omit the v marker (features-only merge)", row)
		}
	})

	t.Run("null frames normalize too", func(t *testing.T) {
		l := indexLine{Name: "a", Vers: "1.0.0", Deps: json.RawMessage("null"), Features: json.RawMessage("null")}
		l.normalizeDepsFeatures()
		if string(l.Deps) != "[]" || string(l.Features) != "{}" {
			t.Errorf("normalize = %s / %s, want [] / {}", l.Deps, l.Features)
		}
	})
}

// configDocument table (spec section 3.1 + TL-1 + the auth-required arm).
func TestConfigDocument(t *testing.T) {
	t.Run("anonymous default", func(t *testing.T) {
		h := New(nil, nil, nil, nil, Options{AnonymousAccess: true})
		body := h.configDocument("http://example.test", "cargo-local")
		want := `{"api":"http://example.test/binflow/cargo-local","dl":"http://example.test/binflow/cargo-local/v1/crates"}`
		if string(body) != want {
			t.Errorf("config = %s, want %s", body, want)
		}
	})

	t.Run("auth-required when anonymous is off", func(t *testing.T) {
		h := New(nil, nil, nil, nil, Options{AnonymousAccess: false})
		body := h.configDocument("http://example.test", "cargo-local")
		if !strings.Contains(string(body), `"auth-required":true`) {
			t.Errorf("config = %s, want auth-required:true", body)
		}
	})
}
