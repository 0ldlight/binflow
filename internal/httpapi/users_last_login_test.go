package httpapi_test

// FR-146.3 (M16, T-454): the users-list lastLoggedIn projection end to
// end on the real stack.
//
//   - REAL login events (POST /api/v1/session, the plane that writes
//     login.success) surface in the very next list call — the query-time
//     derivation ruling: no materialization, no delay window;
//   - never-logged-in users render NO lastLoggedIn key at all (raw-map
//     key-set assertion, the T-252 byte-stability pattern);
//   - seeded multi-login rows collapse to the newest per user;
//   - the widening is additive: every pre-M16 field still present, the
//     default (username ASC) order untouched;
//   - visibility: the data rides the existing admin gate — non-admin 403,
//     anonymous 401 (NFR-S76: no new exposure);
//   - the N+1 gate (T-253 counting-decorator pattern via the harness
//     storeMutate seam): one list GET is ONE LastActionTimes call and ZERO
//     audit Query calls, whether the instance holds one user or a hundred;
//   - a failing derivation fails closed: 500, not a column of fake
//     "never logged in".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// lastLoginRows GETs the users list as raw maps, so key PRESENCE (not just
// value) is assertable.
func lastLoginRows(t *testing.T, h *harness) []map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET users = %d body=%s", resp.StatusCode, body)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("list body %q: %v", body, err)
	}
	if len(rows) == 0 {
		t.Fatal("empty list")
	}
	return rows
}

// rowOf picks the row of one user.
func rowOf(t *testing.T, rows []map[string]any, name string) map[string]any {
	t.Helper()
	for _, r := range rows {
		if r["name"] == name {
			return r
		}
	}
	t.Fatalf("user %q missing from list %v", name, rows)
	return nil
}

// assertRFC3339UTC pins the list family's time spelling on a value.
func assertRFC3339UTC(t *testing.T, field, v string) {
	t.Helper()
	if _, err := time.Parse(time.RFC3339, v); err != nil {
		t.Fatalf("%s = %q is not RFC3339: %v", field, v, err)
	}
	if !strings.HasSuffix(v, "Z") {
		t.Fatalf("%s = %q is not UTC (Z suffix)", field, v)
	}
}

// seedLogin appends one login.success audit row with a controlled time
// (deterministic MAX semantics without sleeping between real logins).
func seedLogin(t *testing.T, h *harness, actor, ts string) {
	t.Helper()
	if err := h.md.Audits().Append(context.Background(), &metadata.AuditEvent{
		Time: ts, Actor: actor, Action: "login.success", Detail: "{}",
	}); err != nil {
		t.Fatalf("seed login event for %s: %v", actor, err)
	}
}

// TestUserListLastLoginProjection: the presence, absence, MAX and
// compatibility table.
func TestUserListLastLoginProjection(t *testing.T) {
	h := newHarness(t)
	t251CreateUser(t, h, "u-alice", "alice@t.io", "pw-a", nil, nil)
	t251CreateUser(t, h, "u-bob", "bob@t.io", "pw-b", nil, nil)
	t251CreateUser(t, h, "u-never", "never@t.io", "pw-n", nil, nil)

	// Seeded multi-login rows: the newest per user must win whatever the
	// append order was. RFC3339 UTC text compares chronologically.
	seedLogin(t, h, "u-alice", "2026-01-01T00:00:00Z")
	seedLogin(t, h, "u-alice", "2026-03-04T05:06:07Z")
	seedLogin(t, h, "u-alice", "2026-02-02T00:00:00Z")
	seedLogin(t, h, "u-bob", "2026-05-05T00:00:00Z")

	t.Run("seeded history projects the newest login per user", func(t *testing.T) {
		rows := lastLoginRows(t, h)
		if got := rowOf(t, rows, "u-alice")["lastLoggedIn"]; got != "2026-03-04T05:06:07Z" {
			t.Fatalf("u-alice lastLoggedIn = %v, want 2026-03-04T05:06:07Z", got)
		}
		if got := rowOf(t, rows, "u-bob")["lastLoggedIn"]; got != "2026-05-05T00:00:00Z" {
			t.Fatalf("u-bob lastLoggedIn = %v, want 2026-05-05T00:00:00Z", got)
		}
	})

	t.Run("no login history renders no lastLoggedIn key at all", func(t *testing.T) {
		row := rowOf(t, lastLoginRows(t, h), "u-never")
		if _, present := row["lastLoggedIn"]; present {
			t.Fatalf("u-never carries lastLoggedIn = %v; absent users must render no key (omitempty)", row["lastLoggedIn"])
		}
	})

	t.Run("a real console login surfaces in the very next list call", func(t *testing.T) {
		// The query-time derivation ruling: whatever the login plane just
		// wrote is visible immediately — no materialization lag.
		if resp, _ := loginJSON(t, h, "u-never", "pw-n"); resp.StatusCode != http.StatusOK {
			t.Fatalf("login u-never = %d body=%s", resp.StatusCode, mustGet(t, resp))
		}
		row := rowOf(t, lastLoginRows(t, h), "u-never")
		v, _ := row["lastLoggedIn"].(string)
		if v == "" {
			t.Fatal("u-never still has no lastLoggedIn right after a successful login")
		}
		assertRFC3339UTC(t, "lastLoggedIn", v)
		// A second login a moment later must move the projection forward
		// (audit times are second-granular).
		first := v
		time.Sleep(1100 * time.Millisecond)
		if resp, _ := loginJSON(t, h, "u-never", "pw-n"); resp.StatusCode != http.StatusOK {
			t.Fatalf("second login u-never = %d", resp.StatusCode)
		}
		row = rowOf(t, lastLoginRows(t, h), "u-never")
		v2, _ := row["lastLoggedIn"].(string)
		assertRFC3339UTC(t, "lastLoggedIn", v2)
		if v2 <= first {
			t.Fatalf("lastLoggedIn did not advance after a newer login: %q then %q", first, v2)
		}
	})

	t.Run("failed logins never become lastLoggedIn values", func(t *testing.T) {
		login(t, h, `{"username":"u-never","password":"wrong"}`, "application/json")
		row := rowOf(t, lastLoginRows(t, h), "u-never")
		v, _ := row["lastLoggedIn"].(string)
		assertRFC3339UTC(t, "lastLoggedIn", v) // unchanged shape, still the success time
	})

	t.Run("the widening is additive and order untouched", func(t *testing.T) {
		rows := lastLoginRows(t, h)
		var names []string
		for _, r := range rows {
			for _, field := range []string{"name", "uri", "realm", "source", "email", "adminRole", "enabled", "groups"} {
				if _, present := r[field]; !present {
					t.Fatalf("row %v lost the pre-M16 field %q", r, field)
				}
			}
			names = append(names, r["name"].(string))
		}
		for i := 1; i < len(names); i++ {
			if names[i-1] > names[i] {
				t.Fatalf("default order broken by the projection: %v", names)
			}
		}
	})
}

// TestUserListLastLoginVisibilityGates (NFR-S76): the projection rides the
// list's existing security-manage gate — a non-admin caller never sees the
// column (or the list at all), anonymous meets the challenge.
func TestUserListLastLoginVisibilityGates(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"ci-peek", "ci-pw"}})
	seedLogin(t, h, "admin", "2026-01-01T00:00:00Z")

	resp := h.do(http.MethodGet, "/binflow/api/security/users", "ci-peek", "ci-pw", nil, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin = %d, want 403", resp.StatusCode)
	}
	_ = mustGet(t, resp)

	resp = h.do(http.MethodGet, "/binflow/api/security/users", "", "", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", resp.StatusCode)
	}
	_ = mustGet(t, resp)
}

// lastLoginCountingAudits counts the audit-store reads the list path may
// issue; everything else passes through to the real store.
type lastLoginCountingAudits struct {
	metadata.AuditStore
	lastAction int
	queries    int
}

func (a *lastLoginCountingAudits) LastActionTimes(ctx context.Context, action string) (map[string]string, error) {
	a.lastAction++
	return a.AuditStore.LastActionTimes(ctx, action)
}

func (a *lastLoginCountingAudits) Query(ctx context.Context, q metadata.AuditQuery) ([]*metadata.AuditEvent, error) {
	a.queries++
	return a.AuditStore.Query(ctx, q)
}

type lastLoginCountingStore struct {
	metadata.Store
	audits *lastLoginCountingAudits
}

func (s *lastLoginCountingStore) Audits() metadata.AuditStore { return s.audits }

// TestUserListLastLoginNPlusOneGate: one list GET is exactly ONE
// LastActionTimes call and ZERO audit Query calls, and the counts are
// user-count-independent (1 user vs 101 users) — the single-GROUP BY
// posture AC1 pins; a per-user walk would scale with the instance.
func TestUserListLastLoginNPlusOneGate(t *testing.T) {
	var audits *lastLoginCountingAudits
	h := newHarnessAuth(t, nil, nil, func(md metadata.Store) metadata.Store {
		audits = &lastLoginCountingAudits{AuditStore: md.Audits()}
		return &lastLoginCountingStore{Store: md, audits: audits}
	}, nil)

	// One small instance first: admin alone, with a login.
	loginJSON(t, h, adminUser, adminPass)
	before := audits.lastAction
	lastLoginRows(t, h)
	smallList, smallQueries := audits.lastAction-before, audits.queries

	// Grow to 101 users, every one with a login history.
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("u-%03d", i)
		hash, err := auth.HashPassword("pw")
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if err := h.md.Users().Create(ctx, &metadata.User{
			Username: name, PasswordHash: hash, IsAdmin: false, Enabled: true,
		}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		if err := h.md.Audits().Append(ctx, &metadata.AuditEvent{
			Time: "2026-01-01T00:00:00Z", Actor: name, Action: "login.success", Detail: "{}",
		}); err != nil {
			t.Fatalf("seed login %s: %v", name, err)
		}
	}

	before = audits.lastAction
	rows := lastLoginRows(t, h)
	if len(rows) != 101 {
		t.Fatalf("list = %d rows, want 101", len(rows))
	}
	bigList, bigQueries := audits.lastAction-before, audits.queries

	if smallList != 1 || bigList != 1 {
		t.Fatalf("LastActionTimes calls: small=%d big=%d, want 1 and 1 (K-independence is the N+1 proof)", smallList, bigList)
	}
	if smallQueries != 0 || bigQueries != 0 {
		t.Fatalf("audit Query calls during list: small=%d big=%d, want 0 (the projection never pages the log)", smallQueries, bigQueries)
	}
}

// lastLoginFailingAudits breaks only the derivation.
type lastLoginFailingAudits struct{ metadata.AuditStore }

func (a *lastLoginFailingAudits) LastActionTimes(context.Context, string) (map[string]string, error) {
	return nil, errLastLoginDerivation
}

var errLastLoginDerivation = errors.New("derivation store down")

type lastLoginFailingStore struct{ metadata.Store }

func (s *lastLoginFailingStore) Audits() metadata.AuditStore { return &lastLoginFailingAudits{} }

// TestUserListLastLoginFailsClosed: a broken derivation answers 500 — never
// a 200 whose column quietly claims nobody ever logged in (a half-derived
// Last Login column would mislead the security review it exists for).
func TestUserListLastLoginFailsClosed(t *testing.T) {
	h := newHarnessAuth(t, nil, nil, func(md metadata.Store) metadata.Store {
		return &lastLoginFailingStore{Store: md}
	}, nil)
	resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500 (fail closed)", resp.StatusCode, body)
	}
	if !strings.Contains(body, "derive last logins") {
		t.Fatalf("body %q lacks the derivation context", body)
	}
}
