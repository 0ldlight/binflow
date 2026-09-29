package cargo

// T-567 / BIN-49 (L036 section 2's six-point ruling), the INFERENCE-level
// leg: the A face's cargo repository creation is gated by its Custom Base
// URL demand (the live probe could not build the repo), so the bare-write
// 201 Location's absolute, context-prefixed shape rests on the
// cross-family uniform form — the generic/maven/deb/rpm/helm/nuget-bare
// siblings all render the 201 Location absolutely through the context path
// on live evidence, and the former bare repo-relative value is the same
// mis-anchored form those probes convicted. Every bare PUT (the plain,
// index-family and sidecar paths — serveDerivedWrite's whole face) is
// covered table-driven.

import (
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/repo"
)

// TestDerivedWriteLocationContextPrefix: every bare PUT's 201 Location is
// the absolute, context-prefixed address, and the value GETs the landed
// bytes through the same /binflow routing (the unknown-crate legs stand as
// written — no facts to converge from).
func TestDerivedWriteLocationContextPrefix(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "cargo-loc", repo.TypeLocal)

	tests := []struct {
		name string
		rel  string // repo-relative deployment path
		body string
	}{
		{
			name: "plain file outside the derived families",
			rel:  "docs/readme.txt",
			body: "readme-bytes",
		},
		{
			name: "index-family file (unknown crate)",
			rel:  "index/my/cr/handwritten",
			body: "hand-written row\n",
		},
		{
			name: "sidecar file (unknown crate)",
			rel:  ".cargo/crates/loc/loc-0.1.0.json",
			body: `{"name":"loc","vers":"0.1.0","deps":[]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := repoPath("cargo-loc") + "/" + tc.rel
			status, body, hdr := s.put(path, []byte(tc.body), nil)
			if status != http.StatusCreated {
				t.Fatalf("bare PUT = (%d, %s), want 201", status, body)
			}
			want := s.srv.URL + path
			if loc := hdr.Get("Location"); loc != want {
				t.Errorf("Location = %q, want the absolute %q", loc, want)
			}
			// Resolvability anchor: the Location addresses the landed node
			// through the same /binflow routing (never a bare-root 404).
			gstatus, gbody, _ := s.get(path)
			if gstatus != http.StatusOK || gbody != tc.body {
				t.Errorf("GET deployed path = (%d, %q), want 200 with the landed bytes", gstatus, gbody)
			}
		})
	}
}
