package repo_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// T-253/E1: Service.UsageBatch — the set form of Usage behind
// GET /api/v1/storage/usage (ADR-0030 / architecture section 14.1, FR-79.1).
// These cases pin the service-level contract: the visibility filtering (the
// family-7 OR formula per repository, silent exclusion instead of 403), the
// point-named subset semantics, the counts arm, the single-repo agreement,
// and the one-aggregate-query shape (the N+1 ban at the seam).

// t253Authz is a per-(user, repo, action) authorizer: what the wire's
// permission targets express, at the granularity the batch needs (the shared
// policyAuthz fake keys grants by path prefix only, which cannot model "read
// on r01 but not r02").
type t253Authz struct {
	grants map[string]map[string][]string // user -> repoKey -> actions
}

func (a *t253Authz) Can(_ context.Context, p *repo.Principal, repoKey, _, action string) bool {
	if p == nil {
		return false
	}
	if p.Admin {
		return true
	}
	for _, act := range a.grants[p.Name][repoKey] {
		if act == action {
			return true
		}
	}
	return false
}

func (a *t253Authz) grant(user, repoKey string, actions ...string) {
	if a.grants == nil {
		a.grants = map[string]map[string][]string{}
	}
	if a.grants[user] == nil {
		a.grants[user] = map[string][]string{}
	}
	a.grants[user][repoKey] = append(a.grants[user][repoKey], actions...)
}

// t253CountingUsage counts the sub-store reads — the N+1 guard: one
// UsageBatch is exactly one List and zero per-repo Gets whatever the
// repository count or the filtering shape.
type t253CountingUsage struct {
	metadata.UsageStore
	lists, gets int
}

func (u *t253CountingUsage) List(ctx context.Context, includeCounts bool) ([]metadata.UsageRow, error) {
	u.lists++
	return u.UsageStore.List(ctx, includeCounts)
}

func (u *t253CountingUsage) Get(ctx context.Context, repoKey string) (*metadata.RepoUsage, error) {
	u.gets++
	return u.UsageStore.Get(ctx, repoKey)
}

type t253CountingStore struct {
	metadata.Store
	usage *t253CountingUsage
}

func (s *t253CountingStore) Usage() metadata.UsageStore { return s.usage }

// t253Env builds a service with the per-repo authorizer and the counting
// store (real engine + real sqlite, the fakes_test.go fidelity posture).
func t253Env(t *testing.T, az *t253Authz) (*t253CountingStore, repo.Service) {
	t.Helper()
	ctx := context.Background()
	eng, err := storage.OpenEngine(t.TempDir(), storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = eng.Close() })
	inner, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: t.TempDir() + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })
	cs := &t253CountingStore{Store: inner, usage: &t253CountingUsage{UsageStore: inner.Usage()}}
	svc := repo.NewWithClock(eng, cs, az, nil, func() time.Time {
		return time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	})
	return cs, svc
}

// t253Seed creates n repositories r00..r(nn-1) (r00 carrying quotaBytes 1024)
// and uploads one file into the first two (non-zero usage), returning the
// keys in creation order (= lexicographic order).
func t253Seed(t *testing.T, svc repo.Service, n int) []string {
	t.Helper()
	ctx := context.Background()
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = "r" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		cfg := "{}"
		if i == 0 {
			cfg = `{"quotaBytes":1024}`
		}
		if _, err := svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: keys[i], Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
			Config: cfg,
		}); err != nil {
			t.Fatalf("CreateRepo(%s): %v", keys[i], err)
		}
	}
	for i := 0; i < 2 && i < n; i++ {
		body := "payload-" + keys[i]
		if _, err := svc.Put(ctx, admin(), keys[i], "a/b.bin",
			strings.NewReader(body), storage.BlobRef{}, "application/octet-stream"); err != nil {
			t.Fatalf("Put(%s): %v", keys[i], err)
		}
	}
	return keys
}

// TestT253UsageBatchVisibility: the role x visibility-set table at the
// service seam. Every authenticated caller gets a slice (empty set = empty
// slice, NOT ErrForbidden); invisible repositories are simply absent; the
// manage bit rides the OR arm exactly like the single-repo endpoint.
func TestT253UsageBatchVisibility(t *testing.T) {
	ctx := context.Background()
	az := &t253Authz{}
	_, svc := t253Env(t, az)
	keys := t253Seed(t, svc, 4)

	// alice reads r01 and r02; carol holds manage on r03 only; dave has
	// nothing (carol's grant is pure m — no read anywhere — to prove the m
	// arm alone admits a row).
	az.grant("alice", keys[1], repo.ActionRead)
	az.grant("alice", keys[2], repo.ActionRead)
	az.grant("carol", keys[3], repo.ActionManage)

	alice := &repo.Principal{Name: "alice"}
	carol := &repo.Principal{Name: "carol"}
	dave := &repo.Principal{Name: "dave"}

	cases := []struct {
		name string
		p    *repo.Principal
		want []string
	}{
		{"admin sees all", admin(), keys},
		{"reader sees its subset", alice, keys[1:3]},
		{"manage-only holder sees its coverage", carol, keys[3:4]},
		{"grantless user sees nothing", dave, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := svc.UsageBatch(ctx, tc.p, repo.UsageBatchQuery{})
			if err != nil {
				t.Fatalf("UsageBatch(%s): %v", tc.p.Name, err)
			}
			if len(rows) != len(tc.want) {
				t.Fatalf("rows = %d (%v), want %d", len(rows), rows, len(tc.want))
			}
			for i, r := range rows {
				if r.RepoKey != tc.want[i] {
					t.Errorf("row %d = %s, want %s", i, r.RepoKey, tc.want[i])
				}
			}
		})
	}

	// The empty view is an EMPTY SLICE (renders [] on the wire), not nil.
	rows, err := svc.UsageBatch(ctx, dave, repo.UsageBatchQuery{})
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("grantless batch = (%v, %v), want (empty non-nil slice, nil)", rows, err)
	}

	// Anonymous answers the route's 401 sentinel, never a filtered view.
	if _, err := svc.UsageBatch(ctx, nil, repo.UsageBatchQuery{}); !errors.Is(err, repo.ErrUnauthorized) {
		t.Fatalf("anonymous batch error = %v, want ErrUnauthorized", err)
	}

	// OR-arm parity with the single-repo endpoint: carol may Usage(r03)
	// (manage arm) and is refused Usage(r00) with ErrForbidden — the batch
	// row set must agree with both decisions.
	if _, err := svc.Usage(ctx, carol, keys[3]); err != nil {
		t.Fatalf("Usage(manage repo) = %v, want pass (OR arm)", err)
	}
	if _, err := svc.Usage(ctx, carol, keys[0]); !errors.Is(err, repo.ErrForbidden) {
		t.Fatalf("Usage(uncovered repo) = %v, want ErrForbidden", err)
	}
}

// TestT253UsageBatchValues: the row contents — metered totals, local-only
// quota parsing, and the counts arm's file-only nodeCount plus the config
// updatedAt; every value agrees with the single-repo Usage view (E1 AC1).
func TestT253UsageBatchValues(t *testing.T) {
	ctx := context.Background()
	_, svc := t253Env(t, &t253Authz{})
	keys := t253Seed(t, svc, 2)
	const payload = int64(len("payload-r00")) // 11 bytes; every seeded body is this shape

	rows, err := svc.UsageBatch(ctx, admin(), repo.UsageBatchQuery{IncludeCounts: true})
	if err != nil {
		t.Fatalf("UsageBatch: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// r00: one file plus its ancestor folder rows — the folder sentinel must
	// not count; quota 1024 from the config.
	if rows[0].RepoKey != keys[0] || rows[0].UsedBytes != payload || rows[0].QuotaBytes != 1024 {
		t.Errorf("r00 row = %+v, want used %d quota 1024", rows[0], payload)
	}
	if rows[0].NodeCount != 1 {
		t.Errorf("r00 nodeCount = %d, want 1 (folder rows excluded)", rows[0].NodeCount)
	}
	if rows[0].UpdatedAt == "" {
		t.Error("r00 updatedAt empty with counts on")
	}
	// r01: metered too (the seed uploads into the first two), no quota.
	if rows[1].UsedBytes != payload || rows[1].QuotaBytes != 0 {
		t.Errorf("r01 row = %+v, want used %d quota 0", rows[1], payload)
	}

	// Without counts the extra fields are zero-valued (the handler then
	// does not render them at all).
	plain, err := svc.UsageBatch(ctx, admin(), repo.UsageBatchQuery{})
	if err != nil {
		t.Fatalf("UsageBatch(plain): %v", err)
	}
	if plain[0].NodeCount != 0 || plain[0].UpdatedAt != "" {
		t.Errorf("plain row carries counts: %+v", plain[0])
	}

	// Single-repo agreement, value by value.
	for _, row := range rows {
		single, err := svc.Usage(ctx, admin(), row.RepoKey)
		if err != nil {
			t.Fatalf("Usage(%s): %v", row.RepoKey, err)
		}
		if single.UsedBytes != row.UsedBytes || single.QuotaBytes != row.QuotaBytes {
			t.Errorf("%s: batch %+v vs single %+v", row.RepoKey, row, single)
		}
	}
}

// TestT253UsageBatchNamedSubset: the ?repos= subset at the service seam —
// the intersection semantics including unknown keys (absent, no error) and
// the invisible-key indistinguishability.
func TestT253UsageBatchNamedSubset(t *testing.T) {
	ctx := context.Background()
	az := &t253Authz{}
	_, svc := t253Env(t, az)
	keys := t253Seed(t, svc, 3)
	az.grant("alice", keys[0], repo.ActionRead)
	alice := &repo.Principal{Name: "alice"}

	rows, err := svc.UsageBatch(ctx, admin(), repo.UsageBatchQuery{Repos: []string{keys[1], keys[2], "no-such", keys[1]}})
	if err != nil {
		t.Fatalf("UsageBatch(named): %v", err)
	}
	if len(rows) != 2 || rows[0].RepoKey != keys[1] || rows[1].RepoKey != keys[2] {
		t.Fatalf("named rows = %+v, want [%s %s]", rows, keys[1], keys[2])
	}

	// Alice names her readable repo, a forbidden one and an unknown one:
	// exactly one row comes back, and the two absences are the same shape.
	rows, err = svc.UsageBatch(ctx, alice, repo.UsageBatchQuery{Repos: []string{keys[0], keys[2], "no-such"}})
	if err != nil {
		t.Fatalf("UsageBatch(named, alice): %v", err)
	}
	if len(rows) != 1 || rows[0].RepoKey != keys[0] {
		t.Fatalf("alice named rows = %+v, want only %s", rows, keys[0])
	}

	// An empty (but non-nil) name list selects nothing; nil means no filter.
	rows, err = svc.UsageBatch(ctx, admin(), repo.UsageBatchQuery{Repos: []string{}})
	if err != nil || len(rows) != 0 || rows == nil {
		t.Fatalf("empty named set = (%v, %v), want empty non-nil", rows, err)
	}
}

// TestT253UsageBatchSingleQuery: the N+1 ban at the seam — one UsageBatch
// over 12 repositories is exactly ONE Usage().List and ZERO per-repo Gets,
// for the full view, the named subset and the filtered reader alike.
func TestT253UsageBatchSingleQuery(t *testing.T) {
	ctx := context.Background()
	az := &t253Authz{}
	cs, svc := t253Env(t, az)
	keys := t253Seed(t, svc, 12)
	az.grant("alice", keys[3], repo.ActionRead)
	// Baseline AFTER seeding: the seed's metered writes legitimately call
	// Get (the quota gate's pre-write read) — only reads issued BY the
	// batch are the N+1 regression.
	getsBefore := cs.usage.gets

	for _, tc := range []struct {
		name string
		p    *repo.Principal
		q    repo.UsageBatchQuery
	}{
		{"full view", admin(), repo.UsageBatchQuery{}},
		{"counts view", admin(), repo.UsageBatchQuery{IncludeCounts: true}},
		{"named subset", admin(), repo.UsageBatchQuery{Repos: keys[:3]}},
		{"filtered reader", &repo.Principal{Name: "alice"}, repo.UsageBatchQuery{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := cs.usage.lists
			if _, err := svc.UsageBatch(ctx, tc.p, tc.q); err != nil {
				t.Fatalf("UsageBatch: %v", err)
			}
			if got := cs.usage.lists - before; got != 1 {
				t.Errorf("UsageBatch issued %d List calls, want exactly 1", got)
			}
			if got := cs.usage.gets - getsBefore; got != 0 {
				t.Errorf("UsageBatch issued %d per-repo Get calls, want 0", got)
			}
		})
	}
}
