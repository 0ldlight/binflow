package adapter

import (
	"errors"
	"strings"
	"testing"
)

// The matrix-parameter peel (M10 T-286, architecture section 15.3.1 /
// ADR-0033): the strip grammar, the legacy ';' fallback (decision 11.39's
// five-fixture survival surface) and the closed property rules.

func TestSplitMatrixParams(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantClean  string
		wantMatrix string
	}{
		{name: "no semicolon", in: "r/a/b.bin", wantClean: "r/a/b.bin"},
		{name: "single pair", in: "r/a/b.bin;build=77", wantClean: "r/a/b.bin", wantMatrix: ";build=77"},
		{name: "two pairs", in: "r/a/b.bin;build=77;env=prod", wantClean: "r/a/b.bin", wantMatrix: ";build=77;env=prod"},
		{name: "value keeps equals", in: "r/a.bin;k=a=b", wantClean: "r/a.bin", wantMatrix: ";k=a=b"},
		{name: "value keeps slash", in: "r/a.bin;k=x/y", wantClean: "r/a.bin", wantMatrix: ";k=x/y"},
		{name: "folder trailing slash survives peel", in: "r/dir/;k=v", wantClean: "r/dir/", wantMatrix: ";k=v"},
		{name: "repo-key segment pairs strip too", in: "r;team=x/a.bin", wantClean: "r", wantMatrix: ";team=x/a.bin"},
		{name: "comma multi-value", in: "r/a.bin;k=1,2", wantClean: "r/a.bin", wantMatrix: ";k=1,2"},

		// The legacy fallback: any non-paired ';' region keeps the M1
		// literal path (decision 11.39; the seed-m10 fixture shapes).
		{name: "legacy single nonpaired", in: "r/legacy/a;b.bin", wantClean: "r/legacy/a;b.bin"},
		{name: "legacy adr example", in: "r/legacy/file;name.jar", wantClean: "r/legacy/file;name.jar"},
		{name: "legacy multi nonpaired", in: "r/legacy/x;y;z.txt", wantClean: "r/legacy/x;y;z.txt"},
		{name: "legacy folder segment", in: "r/legacy/dir;d/nested.bin", wantClean: "r/legacy/dir;d/nested.bin"},
		{name: "legacy maven layout", in: "r/com/acme;lib/1.0/acme;lib-1.0.jar", wantClean: "r/com/acme;lib/1.0/acme;lib-1.0.jar"},
		{name: "mixed paired then bare tail stays literal", in: "r/a.bin;build=77;extra", wantClean: "r/a.bin;build=77;extra"},
		{name: "trailing bare semicolon stays literal", in: "r/a.bin;k=v;", wantClean: "r/a.bin;k=v;"},
		{name: "bare semicolon only", in: "r/a.bin;", wantClean: "r/a.bin;"},
		{name: "double semicolon stays literal", in: "r/a.bin;;k=v", wantClean: "r/a.bin;;k=v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, matrix := SplitMatrixParams(tt.in)
			if clean != tt.wantClean || matrix != tt.wantMatrix {
				t.Fatalf("SplitMatrixParams(%q) = (%q, %q), want (%q, %q)",
					tt.in, clean, matrix, tt.wantClean, tt.wantMatrix)
			}
		})
	}
}

func TestParseMatrixProps(t *testing.T) {
	t.Run("empty region is no props", func(t *testing.T) {
		props, err := ParseMatrixProps("")
		if err != nil || props != nil {
			t.Fatalf("ParseMatrixProps(\"\") = (%v, %v)", props, err)
		}
	})

	t.Run("pairs parse with multi-value accumulation", func(t *testing.T) {
		props, err := ParseMatrixProps(";build=77;env=prod;env=dev")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(props) != 2 || props["build"][0] != "77" || len(props["env"]) != 2 {
			t.Fatalf("props = %+v", props)
		}
	})

	t.Run("illegal key is ErrBadRequestPath", func(t *testing.T) {
		// The PRD's FR-89-AC3 shape: ";bad key=1" -> 400 on the deploy arm.
		for _, matrix := range []string{";bad key=1", ";1bad=1", ";bad-key;x=1", ";=v", ";bad/=1"} {
			_, err := ParseMatrixProps(matrix)
			if err == nil {
				t.Fatalf("ParseMatrixProps(%q) accepted an illegal key", matrix)
			}
			if !errors.Is(err, ErrBadRequestPath) {
				t.Fatalf("ParseMatrixProps(%q) err = %v, want ErrBadRequestPath wrap", matrix, err)
			}
		}
	})

	t.Run("value rules", func(t *testing.T) {
		if _, err := ParseMatrixProps(";k="); err == nil {
			t.Fatal("empty value must be refused")
		}
		if _, err := ParseMatrixProps(";k=" + strings.Repeat("x", 1025)); err == nil {
			t.Fatal("oversize value must be refused")
		}
		if _, err := ParseMatrixProps(";k=bad\x01value"); err == nil {
			t.Fatal("control byte in value must be refused")
		}
		if _, err := ParseMatrixProps(";k=" + strings.Repeat("x", 1024)); err != nil {
			t.Fatalf("value at the 1024B limit refused: %v", err)
		}
	})

	t.Run("cardinality caps", func(t *testing.T) {
		var b strings.Builder
		for i := 0; i < 65; i++ {
			b.WriteString(";k" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "=v")
		}
		if _, err := ParseMatrixProps(b.String()); err == nil {
			t.Fatal("more than 64 pairs must be refused")
		}
	})

	t.Run("unpaired segment through the direct API", func(t *testing.T) {
		// SplitMatrixParams never produces this shape; the direct call
		// answers the defensive 400 rather than a silent no-op.
		if _, err := ParseMatrixProps(";k=v;nopair"); !errors.Is(err, ErrBadRequestPath) {
			t.Fatalf("unpaired segment err = %v", err)
		}
	})
}

func TestLayoutMatrixParams(t *testing.T) {
	t.Run("paired suffix addresses the stripped path", func(t *testing.T) {
		key, rel, err := Layout(layoutReq(t, "generic-local/ci/app.bin;build=77;env=prod"))
		if err != nil {
			t.Fatalf("Layout: %v", err)
		}
		if key != "generic-local" || rel != "ci/app.bin" {
			t.Fatalf("Layout = %q/%q", key, rel)
		}
	})
	t.Run("legacy semicolons keep the literal path", func(t *testing.T) {
		key, rel, err := Layout(layoutReq(t, "generic-local/legacy/file;name.jar"))
		if err != nil {
			t.Fatalf("Layout: %v", err)
		}
		if key != "generic-local" || rel != "legacy/file;name.jar" {
			t.Fatalf("Layout = %q/%q", key, rel)
		}
	})
	t.Run("illegal key is a 400 on every verb", func(t *testing.T) {
		if _, _, err := Layout(layoutReq(t, "generic-local/a.bin;bad key=1")); !errors.Is(err, ErrBadRequestPath) {
			t.Fatalf("Layout err = %v", err)
		}
	})
	t.Run("traversal behind the peel still rejected", func(t *testing.T) {
		if _, _, err := Layout(layoutReq(t, "generic-local/a/../../etc;k=v")); !errors.Is(err, ErrBadRequestPath) {
			t.Fatalf("Layout err = %v", err)
		}
	})
}

func TestResolveContentProps(t *testing.T) {
	key, rel, props, err := ResolveContent(layoutReq(t, "generic-local/ci/app.bin;build=77;env=prod"))
	if err != nil {
		t.Fatalf("ResolveContent: %v", err)
	}
	if key != "generic-local" || rel != "ci/app.bin" {
		t.Fatalf("ResolveContent = %q/%q", key, rel)
	}
	if len(props) != 2 || props["build"][0] != "77" || props["env"][0] != "prod" {
		t.Fatalf("props = %+v", props)
	}
	// The no-matrix request resolves with nil props (the plain deploy).
	_, _, none, err := ResolveContent(layoutReq(t, "generic-local/ci/app.bin"))
	if err != nil || none != nil {
		t.Fatalf("plain path props = %+v err=%v", none, err)
	}
}
