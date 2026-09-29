package maven

// T-559 / BIN-41: plain-SNAPSHOT spelling resolution (contract
// maven/plain-snapshot-path-resolve) — a path carrying a -SNAPSHOT
// segment, with the artifact stored under that exact spelling, resolves
// as an ORDINARY storage path: the member-direct read and the virtual
// resolve both hit and serve the stored bytes; the spelling neither
// rewrites the resolution nor 404s. The unique home's plain-GET face
// (nothing stored at the plain spelling — the deploy rewrote it to the
// timestamped name) stays 404: resolving it to the latest timestamped
// copy is the snapshot walk family (virtual-resolution.md §3.6), which
// the contract fences to an independent follow-up ticket — deliberately
// NOT implemented here.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestPlainSnapshotPathResolve walks the contract's three arms on a home
// that stores the plain spelling verbatim (snapshotVersionBehavior=
// non-unique; its handle* pair is all-default, which is exactly the
// control leg's home shape — the spelling resolution is handle*-free).
func TestPlainSnapshotPathResolve(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	for _, r := range []*metadata.Repo{
		{RepoKey: "maven-vplain", Type: repo.TypeVirtual, PackageType: Protocol,
			Config: `{"repositories":["maven-nonunique"]}`},
	} {
		if _, err := hs.svc.CreateRepo(ctx, adminP, r); err != nil {
			t.Fatalf("seed %s: %v", r.RepoKey, err)
		}
	}

	const plain = "/maven-nonunique/com/x/pl/1.0.0-SNAPSHOT/pl-1.0.0-SNAPSHOT.pom"
	pom := []byte("<project><groupId>com.x</groupId><artifactId>pl</artifactId><version>1.0.0-SNAPSHOT</version></project>")
	resp := hs.serve(http.MethodPut, plain, pom, nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("plain PUT = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	if loc := resp.Header.Get("Location"); !strings.HasSuffix(loc, "pl-1.0.0-SNAPSHOT.pom") {
		t.Fatalf("plain PUT Location = %q, want the verbatim spelling stored", loc)
	}

	// Arm 1 — member direct read: 200, the stored bytes verbatim.
	if code, got := mustGet(t, hs, plain); code != http.StatusOK || string(got) != string(pom) {
		t.Errorf("member direct GET = %d %.60s, want 200 the stored pom", code, got)
	}

	// Arm 2 — virtual resolve over the holding member: 200, same bytes.
	vplain := "/maven-vplain" + strings.TrimPrefix(plain, "/maven-nonunique")
	if code, got := mustGet(t, hs, vplain); code != http.StatusOK || string(got) != string(pom) {
		t.Errorf("virtual resolve GET = %d %.60s, want 200 the same pom", code, got)
	}

	// Arm 3 (control, handle* all-default home) — the checksum companion
	// of the plain spelling resolves as an ordinary path too: the
	// server-computed digest of the stored target.
	if code, got := mustGet(t, hs, plain+".sha1"); code != http.StatusOK {
		t.Errorf("plain spelling sidecar GET = %d, want 200 (%s)", code, got)
	} else if s1, _, _ := digests(pom); strings.TrimSpace(got) != s1 {
		t.Errorf("plain spelling sidecar = %q, want the target's sha1 %q", got, s1)
	}
}

// TestPlainSnapshotUniqueHomeBoundary is the FLIPPED anchor the contract
// names (⑩ maven/plain-snapshot-unique-walk-resolve, T-562 stage 2 /
// BIN-44): in a unique-behavior home the plain PUT lands under the
// timestamped spelling, and the plain-spelling GET now resolves through
// the walk to that timestamped entity (200 + the deployed bytes) — the
// pre-T-562 honest 404 was the fenced interim face; the boundary test
// flipped with the walk's landing, as its own pre-flip note directed.
func TestPlainSnapshotUniqueHomeBoundary(t *testing.T) {
	hs := newHarness(t)
	const plain = "/maven-unique/com/x/pl/1.0.0-SNAPSHOT/pl-1.0.0-SNAPSHOT.pom"
	pom := []byte("<project><groupId>com.x</groupId><artifactId>pl</artifactId><version>1.0.0-SNAPSHOT</version></project>")
	resp := hs.serve(http.MethodPut, plain, pom, nil, true)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("plain PUT into unique home = %d (%s)", resp.StatusCode, drain(t, resp))
	}
	loc := resp.Header.Get("Location")
	if strings.HasSuffix(loc, "-SNAPSHOT.pom") {
		t.Fatalf("unique home stored the plain spelling verbatim (%q) — the §1.3 rewrite regressed", loc)
	}
	// The walk resolve (W1): the plain GET answers the rewritten entity.
	if code, got := mustGet(t, hs, plain); code != http.StatusOK || string(got) != string(pom) {
		t.Errorf("plain GET in unique home = %d %.60s, want 200 the rewritten entity's bytes (walk resolve)", code, got)
	}
	// The rewritten spelling serves the same face (W3): byte-identical.
	tsPath := loc
	if i := strings.Index(tsPath, "/binflow/"); i >= 0 {
		tsPath = tsPath[i+len("/binflow"):]
	}
	if code, got := mustGet(t, hs, tsPath); code != http.StatusOK || string(got) != string(pom) {
		t.Errorf("rewritten-spelling GET %s = %d %.60s, want 200 the same bytes", tsPath, code, got)
	}
}
