package generic_test

// T-583 / BIN-65 (L039 Arm 2 / C3): the terminal-checksum interception
// family matches its suffix CASE-INSENSITIVELY — .SHA1/.Md5/.SHA256 and
// every mixed spelling route into the family with the same 404 wording,
// while the excluded faces (.sha512/.asc, compound tails) stay excluded
// in every spelling. 权威口径：reports/compatibility/L039-checksum-put-adjacent.md
// Arm 2（A 7.161.26 双轮）。

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// TestChecksumPutSuffixCaseInsensitive pins the family's case folding: an
// uppercase or mixed-case terminal suffix is the same client-checksum
// operation as the lowercase one, with the SOURCE reference keeping the
// client's original spelling minus the suffix.
func TestChecksumPutSuffixCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	hex40 := strings.Repeat("ab", 20)
	cases := []struct {
		name    string
		path    string // under /binflow/generic-local/
		wantSrc string
	}{
		{"upper SHA1", "t583c/lone.txt.SHA1", "t583c/lone.txt"},
		{"mixed Md5", "t583c/lone.txt.Md5", "t583c/lone.txt"},
		{"upper SHA256", "t583c/lone.txt.SHA256", "t583c/lone.txt"},
		{"mixed sHa1", "t583c/lone.txt.sHa1", "t583c/lone.txt"},
		{"uppercase source name kept", "t583c/LONE.TXT.SHA1", "t583c/LONE.TXT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+tc.path,
				strings.NewReader(hex40), nil)
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("PUT %s = %d, want 404 (body=%s)", tc.path, resp.StatusCode, body(t, resp))
			}
			want := fmt.Sprintf("Target file to set checksum on doesn't exist: generic-local:%s", tc.wantSrc)
			if got := body(t, resp); !strings.Contains(got, `"`+want+`"`) {
				t.Fatalf("404 body = %s\nwant message = %q", got, want)
			}
		})
	}

	// No node materialized under the namespace: every spelling is a
	// metadata-write interpretation, never a file deploy.
	nodes, err := e.svc.List(context.Background(), admin(), "generic-local", "t583c")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(nodes) != 0 {
		for _, n := range nodes {
			t.Errorf("unexpected node: %s", n.Path)
		}
	}
}

// TestChecksumPutSuffixCaseExclusionsStillExcluded pins the negative half:
// the EXCLUDED terminal spellings deploy as ordinary files whatever their
// case — folding applies to the three family keys only.
func TestChecksumPutSuffixCaseExclusionsStillExcluded(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{
		"t583x/lone.txt.SHA512",
		"t583x/lone.txt.ASC",
		"t583x/lone.txt.SHA1.BAK",
		"t583x/lone.txt.Md5.old",
	} {
		resp := e.do(t, http.MethodPut, "/binflow/generic-local/"+path,
			strings.NewReader("payload-for-"+path), nil)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("PUT %s = %d, want 201 ordinary deploy (body=%s)", path, resp.StatusCode, body(t, resp))
		}
		get := e.do(t, http.MethodGet, "/binflow/generic-local/"+path, nil, nil)
		if got := body(t, get); get.StatusCode != http.StatusOK || got != "payload-for-"+path {
			t.Errorf("GET %s = %d %q, want the deployed bytes", path, get.StatusCode, got)
		}
	}
}
