package deb

// T-327G: the by-hash retention window counts GENERATIONS, not entries
// (debian.md section 5: "当前版必须可取，历史版应保留 ≥2 代——按代数保留，
// 超出裁最旧"). One recompute lands a whole index family's compression
// spellings (plain + .gz + the optional set) into each by-hash/<ALGO>
// directory, so the pre-T-327G entry-count window could prune the CURRENT
// generation's copies whenever historyCycles fell below the spelling
// count — an address the freshly written Release still advertises, gone.
// The T-327R report registered the defect after the REST plane unlocked
// historyCycles (the default 3 had masked it against the mandatory
// plain + .gz pair).

import (
	"net/http"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// ---- the pure retention plan ----

// byHashNode builds one bucket entry for the plan tests.
func byHashNode(updated, digest string) *metadata.Node {
	return &metadata.Node{Path: "dists/stable/main/binary-amd64/by-hash/SHA256/" + digest, UpdatedAt: updated}
}

// assertPruned compares the plan's prune set against the expected digests.
func assertPruned(t *testing.T, got []*metadata.Node, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("prune plan returned %d entries (%v), want %d (%v)",
			len(got), pathsOf(got), len(want), want)
	}
	set := map[string]bool{}
	for _, n := range got {
		set[n.Path] = true
	}
	for _, d := range want {
		if !set["dists/stable/main/binary-amd64/by-hash/SHA256/"+d] {
			t.Errorf("prune plan missing %s (got %v)", d, pathsOf(got))
		}
	}
}

// pathsOf renders a node slice's paths (failure messages only).
func pathsOf(ns []*metadata.Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.Path)
	}
	return out
}

// TestByHashPrunePlan: the retention window's unit — current-generation
// protection by digest (however low the cycles), stale entries grouped
// into generations by write timestamp, the window bounding the TOTAL
// generations (current included), same-second runs merging (the safe
// direction: extra history, never less).
func TestByHashPrunePlan(t *testing.T) {
	// One spelling set per generation: plain + .gz + .xz (the T-327R
	// defect shape — three entries per run in one algorithm directory).
	gen := func(tag string) []*metadata.Node {
		return []*metadata.Node{
			byHashNode("2026-08-27T10:0"+tag+":00Z", tag+"a"),
			byHashNode("2026-08-27T10:0"+tag+":00Z", tag+"b"),
			byHashNode("2026-08-27T10:0"+tag+":00Z", tag+"c"),
		}
	}
	curr := map[string]bool{
		"dists/stable/main/binary-amd64/by-hash/SHA256/9a": true,
		"dists/stable/main/binary-amd64/by-hash/SHA256/9b": true,
		"dists/stable/main/binary-amd64/by-hash/SHA256/9c": true,
	}
	curNodes := []*metadata.Node{
		byHashNode("2026-08-27T10:09:00Z", "9a"),
		byHashNode("2026-08-27T10:09:00Z", "9b"),
		byHashNode("2026-08-27T10:09:00Z", "9c"),
	}
	bucket := func(genTags ...string) []*metadata.Node {
		var ns []*metadata.Node
		for _, tag := range genTags {
			ns = append(ns, gen(tag)...)
		}
		return ns
	}

	tests := []struct {
		name    string
		nodes   []*metadata.Node
		current map[string]bool
		cycles  int
		want    []string
	}{
		{
			name:    "cycles below the spelling floor keeps every current entry",
			nodes:   append(bucket("8"), curNodes...),
			current: curr,
			cycles:  1,
			want:    []string{"8a", "8b", "8c"},
		},
		{
			name:    "window counts generations not entries",
			nodes:   append(append(bucket("7", "8"), curNodes...), bucket("6")...),
			current: curr,
			cycles:  3,
			want:    []string{"6a", "6b", "6c"},
		},
		{
			name:    "cycles one drops every stale generation whole",
			nodes:   append(bucket("7", "6", "8"), curNodes...),
			current: curr,
			cycles:  1,
			want:    []string{"8a", "8b", "8c", "7a", "7b", "7c", "6a", "6b", "6c"},
		},
		{
			// Two runs inside one second count as ONE generation: the
			// merged group's whole entry set survives together (an entry
			// counter would strand the fourth), and only the genuinely
			// older generation leaves the window.
			name: "same-second runs merge into one generation (safe direction)",
			nodes: append([]*metadata.Node{
				byHashNode("2026-08-27T10:08:00Z", "8a"),
				byHashNode("2026-08-27T10:08:00Z", "8b"),
				byHashNode("2026-08-27T10:08:00Z", "8d"), // the merged run's fourth entry
			}, append(bucket("7"), curNodes...)...),
			current: curr,
			cycles:  2,
			want:    []string{"7a", "7b", "7c"},
		},
		{
			name:    "current generation consumes a window slot",
			nodes:   append(bucket("8", "7"), curNodes...),
			current: curr,
			cycles:  2,
			want:    []string{"7a", "7b", "7c"},
		},
		{
			name:    "empty current set (policy flipped off) ages the tree out",
			nodes:   bucket("8", "7", "6", "5"),
			current: map[string]bool{},
			cycles:  3,
			want:    []string{"6a", "6b", "6c", "5a", "5b", "5c"},
		},
		{
			name:    "non-positive cycles (direct call) keeps nothing stale",
			nodes:   bucket("8", "7"),
			current: map[string]bool{},
			cycles:  0,
			want:    []string{"8a", "8b", "8c", "7a", "7b", "7c"},
		},
		{
			name: "a refreshed current digest stays protected whatever its timestamp",
			nodes: append(bucket("8"), []*metadata.Node{
				byHashNode("2026-08-27T10:00:00Z", "9a"), // unchanged content, old stamp
			}...),
			current: curr,
			cycles:  1,
			want:    []string{"8a", "8b", "8c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPruned(t, byHashPrunePlan(tt.nodes, tt.current, tt.cycles), tt.want...)
		})
	}
}

// ---- the engine legs ----

// waitDigestMoves polls until the path serves 200 with a body whose
// SHA-256 left the previous generation's (a spelling lands after the
// plain file inside one run — per-spelling movement is the settle
// signal), then returns the new body.
func (s *stack) waitDigestMoves(t *testing.T, path, prevDigest string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		status, body, _ := s.get(path)
		last = body
		if status == http.StatusOK && sha256Hex([]byte(body)) != prevDigest {
			return body
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never moved off %s (last %d bytes)", path, prevDigest, len(last))
	return ""
}

// waitGone polls until the path 404s (the sweep is the run's last step —
// its effect needs its own poll).
func (s *stack) waitGone(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if status, _, _ := s.get(path); status == http.StatusNotFound {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never left (the retention sweep never landed)", path)
}

// waitStaysServed asserts the path keeps serving the historical body for
// a settle window (a sweep that would wrongly prune it runs within
// milliseconds of the canonical write — a second covers it by orders of
// magnitude).
func (s *stack) waitStaysServed(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		status, body, _ := s.get(path)
		if status != http.StatusOK || body != want {
			t.Fatalf("%s = (%d, %d bytes), want the historical body kept", path, status, len(body))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestByHashCyclesOneKeepsCurrentSpellings: the registered defect's exact
// shape — historyCycles=1 with THREE spellings per generation (plain +
// ..gz + the optional xz, ALL policy). After the index moves, every
// current spelling's digest address must still serve byte-identical
// content (the pre-T-327G window pruned current entries here) and every
// previous-generation address must age out.
func TestByHashCyclesOneKeepsCurrentSpellings(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-c1", repo.TypeLocal,
		`{"byHash":"ALL","optionalIndexCompressionFormats":["xz"],`+
			`"historyCycles":1,"debianDefaultArchitectures":"amd64"}`)

	base := "/binflow/deb-c1/dists/stable/main/binary-amd64"
	spellings := []string{"Packages", "Packages.gz", "Packages.xz"}

	digestsOf := func(t *testing.T, plainMovedFrom map[string]string) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, sp := range spellings {
			var body string
			if prev, ok := plainMovedFrom[sp]; ok {
				body = s.waitDigestMoves(t, base+"/"+sp, prev)
			} else {
				body = s.waitIndex(t, base+"/"+sp)
			}
			out[sp] = sha256Hex([]byte(body))
		}
		return out
	}

	// Generation one.
	if status, b, _ := s.debPut(t, "/binflow/deb-c1/pool/main/c/cycler/cycler_1.0_amd64.deb",
		helloDeb("cycler", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v1 debPUT = (%d, %s)", status, b)
	}
	v1 := digestsOf(t, nil)
	v1PlainBody := s.waitIndex(t, base+"/Packages")

	// Generation two: the index content moves under the same spelling set.
	if status, b, _ := s.debPut(t, "/binflow/deb-c1/pool/main/c/cycler/cycler_2.0_amd64.deb",
		helloDeb("cycler", "2.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v2 debPUT = (%d, %s)", status, b)
	}
	v2 := digestsOf(t, v1)
	v2PlainBody := s.waitIndex(t, base+"/Packages")
	if v2["Packages"] == v1["Packages"] {
		t.Fatal("the Packages index did not move between versions")
	}

	// The sweep (the run's last step) drops every previous-generation
	// address: cycles=1 keeps the current generation alone.
	s.waitGone(t, base+"/by-hash/SHA256/"+v1["Packages"])
	for _, sp := range spellings {
		if status, _, _ := s.get(base + "/by-hash/SHA256/" + v1[sp]); status != http.StatusNotFound {
			t.Errorf("v1 %s by-hash address = %d, want 404 (cycles=1 keeps the current generation alone)", sp, status)
		}
	}

	// THE regression: every CURRENT spelling's address still serves, with
	// byte-identical content — the pre-T-327G window could prune these
	// (cycles=1 < three spellings per generation).
	for _, sp := range spellings {
		path := base + "/by-hash/SHA256/" + v2[sp]
		var want string
		if sp == "Packages" {
			want = v2PlainBody
		} else {
			_, want, _ = s.get(base + "/" + sp) // the canonical spelling just settled
		}
		if status, body, _ := s.get(path); status != http.StatusOK || body != want {
			t.Errorf("current %s by-hash address = (%d, %d bytes), want the canonical bytes (current generation never prunes)", sp, status, len(body))
		}
	}
	// The ALL policy keeps the other algorithm families of the current
	// generation too.
	if status, _, _ := s.get(base + "/by-hash/MD5Sum/" + md5Of([]byte(v2PlainBody))); status != http.StatusOK {
		t.Errorf("current MD5Sum by-hash address = %d, want 200 (ALL policy)", status)
	}
	if status, _, _ := s.get(base + "/by-hash/MD5Sum/" + md5Of([]byte(v1PlainBody))); status != http.StatusNotFound {
		t.Errorf("v1 MD5Sum by-hash address = %d, want 404", status)
	}
}

// TestByHashCyclesZeroFoldsToDefault: the cycles=0 boundary. The spec
// leaves the knob's zero semantics open (debian.md section 5's open
// questions list the default; the official floor says history keeps ≥2
// generations), and the engine's standing normalization folds
// non-positive cycles to the default 3 — a "0 = keep nothing" reading
// would contradict the floor. Pinned here: zero keeps a history
// generation (the previous digest still serves after one index move).
func TestByHashCyclesZeroFoldsToDefault(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "deb-c0", repo.TypeLocal,
		`{"byHash":"SHA256","historyCycles":0,"debianDefaultArchitectures":"amd64"}`)

	base := "/binflow/deb-c0/dists/stable/main/binary-amd64"
	if status, b, _ := s.debPut(t, "/binflow/deb-c0/pool/main/f/folder/folder_1.0_amd64.deb",
		helloDeb("folder", "1.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v1 debPUT = (%d, %s)", status, b)
	}
	v1Body := s.waitIndex(t, base+"/Packages")
	v1Digest := sha256Hex([]byte(v1Body))

	if status, b, _ := s.debPut(t, "/binflow/deb-c0/pool/main/f/folder/folder_2.0_amd64.deb",
		helloDeb("folder", "2.0", "amd64"), "stable", []string{"main"}, []string{"amd64"}); status != http.StatusCreated {
		t.Fatalf("v2 debPUT = (%d, %s)", status, b)
	}
	s.waitDigestMoves(t, base+"/Packages", v1Digest)

	// The fold: the historical address stays served across the settle
	// window (a broken fold would prune it within milliseconds).
	s.waitStaysServed(t, base+"/by-hash/SHA256/"+v1Digest, v1Body)
}
