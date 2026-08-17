package metadata_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// seedNodes inserts nodes under repo for each path, with per-path blobs so FK
// constraints resolve.
func seedNodes(t *testing.T, st metadata.Store, repo string, paths ...string) {
	t.Helper()
	ctx := context.Background()
	now := metadata.Now()
	putRepo(t, st, repo)
	for i, p := range paths {
		sha, size := fakeBlob(i + 2000)
		if err := st.Blobs().Put(ctx, &metadata.Blob{Sha256: sha, Size: size, CreatedAt: now}); err != nil {
			t.Fatalf("blob put: %v", err)
		}
		if err := st.Nodes().Put(ctx, &metadata.Node{
			RepoKey: repo, Path: p, Sha256: sha, Size: size, CreatedBy: "t",
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("node put %s: %v", p, err)
		}
	}
}

func pathsOf(t *testing.T, nodes []*metadata.Node) []string {
	t.Helper()
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Path)
	}
	return out
}

func equalPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Review B1: SQLite LIKE is ASCII case-insensitive by default, so the prefix
// arm of ListByPrefix/DeleteByPrefix would match across case (a silent
// data-loss path: DeleteByPrefix("LIB") deleting "lib/a.jar"). The DSN sets
// case_sensitive_like=1; these tests pin that behavior.
func TestCaseSensitivePrefixMatch(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedNodes(t, st, "m",
		"lib/a.jar",   // lowercase dir
		"LIB/b.jar",   // uppercase dir — a different path, not the same node
		"Lib/c.jar",   // mixed
		"lib-x/d.jar", // shares the "lib" prefix as a string
		"other/e.jar",
	)

	tests := []struct {
		name   string
		prefix string
		list   []string
	}{
		{"lowercase prefix excludes uppercase dirs", "lib", []string{"lib/a.jar"}},
		{"uppercase prefix excludes lowercase dirs", "LIB", []string{"LIB/b.jar"}},
		{"mixed-case prefix matches exactly itself", "Lib", []string{"Lib/c.jar"}},
		{"string-prefix sibling is not a path boundary", "lib-x", []string{"lib-x/d.jar"}},
		{"case-sensitive exact file", "LIB/b.jar", []string{"LIB/b.jar"}},
		{"wrong-case file finds nothing", "lib/B.jar", nil},
	}
	for _, tt := range tests {
		t.Run("list/"+tt.name, func(t *testing.T) {
			got, err := st.Nodes().ListByPrefix(ctx, "m", tt.prefix)
			if err != nil {
				t.Fatalf("ListByPrefix(%q): %v", tt.prefix, err)
			}
			if !equalPaths(pathsOf(t, got), tt.list) {
				t.Fatalf("ListByPrefix(%q) = %v, want %v", tt.prefix, pathsOf(t, got), tt.list)
			}
		})
	}

	// The destructive direction: an uppercase prefix must not delete
	// lowercase-prefixed nodes.
	n, err := st.Nodes().DeleteByPrefix(ctx, "m", "LIB")
	if err != nil {
		t.Fatalf("DeleteByPrefix(LIB): %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteByPrefix(LIB) removed %d rows, want 1 (only LIB/b.jar)", n)
	}
	remaining, err := st.Nodes().ListByPrefix(ctx, "m", "")
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	want := []string{"Lib/c.jar", "lib-x/d.jar", "other/e.jar", "lib/a.jar"}
	got := pathsOf(t, remaining)
	if len(got) != len(want) {
		t.Fatalf("remaining = %v, want %v", got, want)
	}
	found := map[string]bool{}
	for _, p := range got {
		found[p] = true
	}
	for _, p := range want {
		if !found[p] {
			t.Fatalf("node %s was deleted by DeleteByPrefix(LIB) — cross-case data loss", p)
		}
	}
	if found["LIB/b.jar"] {
		t.Fatal("LIB/b.jar survived its own DeleteByPrefix")
	}
}

// Review B1/M5: the LIKE wildcard characters %, _ and \ inside a prefix must
// match literally (likePrefix escapes them). Repo keys cannot contain them,
// but artifact paths can (e.g. "my_lib/"), so the escaping is pinned here.
func TestPrefixLikeWildcardsEscaped(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	seedNodes(t, st, "w",
		"my_lib/a.jar",      // underscore in dir
		"myXlib/b.jar",      // would match my_lib if _ were a wildcard
		"100pct/c.jar",      // percent in dir
		"100Xct/d.jar",      // would match 100pct if % were a wildcard
		"back\\slash/e.jar", // backslash itself
		"any/f.jar",
	)

	tests := []struct {
		prefix string
		want   []string
	}{
		{"my_lib", []string{"my_lib/a.jar"}},
		{"100pct", []string{"100pct/c.jar"}},
		{`back\slash`, []string{`back\slash/e.jar`}},
	}
	for _, tt := range tests {
		t.Run("prefix "+tt.prefix, func(t *testing.T) {
			got, err := st.Nodes().ListByPrefix(ctx, "w", tt.prefix)
			if err != nil {
				t.Fatalf("ListByPrefix(%q): %v", tt.prefix, err)
			}
			if !equalPaths(pathsOf(t, got), tt.want) {
				t.Fatalf("ListByPrefix(%q) = %v, want %v (LIKE wildcard leaked)", tt.prefix, pathsOf(t, got), tt.want)
			}
		})
	}

	n, err := st.Nodes().DeleteByPrefix(ctx, "w", "my_lib")
	if err != nil {
		t.Fatalf("DeleteByPrefix(my_lib): %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteByPrefix(my_lib) removed %d, want 1", n)
	}
	if _, err := st.Nodes().Get(ctx, "w", "myXlib/b.jar"); err != nil {
		t.Fatalf("myXlib/b.jar must survive DeleteByPrefix(my_lib): %v", err)
	}
}

// Review B2: the per-connection PRAGMAs ride in the DSN, so they must hold
// on every pooled connection, not just the first one. Hold several
// connections open concurrently (forcing the pool past one connection) and
// assert the PRAGMA state on each.
func TestPRAGMAsHoldOnEveryPooledConnection(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	raw := st.(interface {
		RawConn(ctx context.Context, fn func(*sql.Conn) error) error
	})

	const conns = 4
	if ncpu := metadata.NumCPUConcurrency(); conns > ncpu {
		t.Skipf("pool capped at NumCPU=%d, cannot force %d connections", ncpu, conns)
	}
	var wg sync.WaitGroup
	errs := make(chan error, conns)
	for i := 0; i < conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- raw.RawConn(ctx, func(conn *sql.Conn) error {
				// Hold the connection for the duration of the check so the
				// pool must open distinct ones.
				var fk, busy string
				if err := conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
					return err
				}
				if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
					return err
				}
				// case_sensitive_like has no readable value; assert it
				// behaviorally ('A' LIKE 'a' is true only when insensitive).
				var likeCaseInsensitive bool
				if err := conn.QueryRowContext(ctx, `SELECT 'A' LIKE 'a'`).Scan(&likeCaseInsensitive); err != nil {
					return err
				}
				if fk != "1" {
					return fmt.Errorf("foreign_keys = %q on a pooled connection, want 1 (FK would silently vanish)", fk)
				}
				if likeCaseInsensitive {
					return fmt.Errorf("LIKE is case-insensitive on a pooled connection; case_sensitive_like is not in force")
				}
				if busy != "5000" {
					return fmt.Errorf("busy_timeout = %q on a pooled connection, want 5000", busy)
				}
				return nil
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
