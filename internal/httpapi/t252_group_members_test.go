package httpapi_test

// T-252 (M9, ADR-0030 / architecture 14.1 E5, FR-78.3): the group-member
// query end to end on the real stack (sqlite metadata, real auth.Service,
// real router).
//
//   - ?includeUsers=true widens the single-group body with userNames
//     (sorted, empty = [] never null); every other spelling — absent,
//     "false", junk, wrong case — renders the plain three-field body with
//     no new status code (the additive contract's hard edge);
//   - the 404 for an unknown name keeps its wording and precedence with
//     the parameter present;
//   - the LIST stays unwidened (K19: no membersCount, no userNames — the
//     census has one source of truth, the user_groups rows);
//   - the E2 users.groups view and the E5 userNames view of those same
//     rows agree (the cross-view consistency the group editor rides);
//   - the N+1 gate: one includeUsers=true GET is ONE membership query and
//     ONE group read, whatever the member count — never a per-user walk.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// t252Group PUTs a group through the admin API (the T-97 wire path).
func t252Group(t *testing.T, h *harness, name string) {
	t.Helper()
	resp := h.do(http.MethodPut, "/binflow/api/security/groups/"+name, adminUser, adminPass,
		[]byte(`{"name":"`+name+`","description":"t252"}`),
		map[string]string{"Content-Type": "application/json"})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create group %s = %d body=%s", name, resp.StatusCode, mustGet(t, resp))
	}
	_ = resp.Body.Close()
}

// TestGroupDetailIncludeUsers walks the parameter spellings and the gate.
// The "off" legs assert the body carries NO userNames key at all (decode
// into a raw map and inspect the key set — presence, not just emptiness),
// which is the byte-stability half of the additive contract.
func TestGroupDetailIncludeUsers(t *testing.T) {
	h := newHarness(t)

	t252Group(t, h, "devs")
	t252Group(t, h, "empty")
	t252Group(t, h, "qa")
	// devs gains u-b before u-a: the wire set must come back ordered by
	// username whatever the per-user write order was.
	t251CreateUser(t, h, "u-b", "b@t.io", "pw-b", nil, []string{"devs"})
	t251CreateUser(t, h, "u-a", "a@t.io", "pw-a", nil, []string{"qa", "devs"})

	tests := []struct {
		name       string
		group      string
		query      string
		asUser     string // "" = admin, unless anon
		asPass     string
		anon       bool
		wantStatus int
		wantUsers  []string // nil = the userNames key must be ABSENT
	}{
		{
			name:       "no parameter renders the plain body",
			group:      "devs",
			wantStatus: http.StatusOK,
		},
		{
			name:       "true renders the sorted member set",
			group:      "devs",
			query:      "?includeUsers=true",
			wantStatus: http.StatusOK,
			wantUsers:  []string{"u-a", "u-b"},
		},
		{
			name:       "true on a member-less group renders an empty array",
			group:      "empty",
			query:      "?includeUsers=true",
			wantStatus: http.StatusOK,
			wantUsers:  []string{},
		},
		{
			name:       "false is off",
			group:      "devs",
			query:      "?includeUsers=false",
			wantStatus: http.StatusOK,
		},
		{
			name:       "junk value is off, not an error",
			group:      "devs",
			query:      "?includeUsers=bogus",
			wantStatus: http.StatusOK,
		},
		{
			name:       "the literal is case-sensitive (only =true is specified)",
			group:      "devs",
			query:      "?includeUsers=TRUE",
			wantStatus: http.StatusOK,
		},
		{
			name:       "repeated parameter takes the first value (Go canonical)",
			group:      "devs",
			query:      "?includeUsers=false&includeUsers=true",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown group with the parameter is the plain 404",
			group:      "ghost",
			query:      "?includeUsers=true",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown group without the parameter unchanged",
			group:      "ghost",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "plain user never passes the route gate",
			group:      "devs",
			query:      "?includeUsers=true",
			asUser:     "u-a",
			asPass:     "pw-a",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "anonymous is challenged",
			group:      "devs",
			query:      "?includeUsers=true",
			anon:       true,
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, pass := adminUser, adminPass
			if tc.anon {
				user, pass = "", ""
			} else if tc.asUser != "" {
				user, pass = tc.asUser, tc.asPass
			}
			resp := h.do(http.MethodGet, "/binflow/api/security/groups/"+tc.group+tc.query, user, pass, nil, nil)
			body := mustGet(t, resp)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d body=%s, want %d", resp.StatusCode, body, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusNotFound && body != "Group not found" {
				t.Fatalf("404 body = %q, want the family wording", body)
			}
			if tc.wantStatus != http.StatusOK {
				return
			}
			var detail map[string]json.RawMessage
			if err := json.Unmarshal([]byte(body), &detail); err != nil {
				t.Fatalf("decode %q: %v", body, err)
			}
			raw, present := detail["userNames"]
			if tc.wantUsers == nil {
				if present {
					t.Fatalf("body carries userNames though the parameter is off: %s", body)
				}
				// The plain body is exactly the pre-M9 trio.
				if len(detail) != 3 {
					t.Fatalf("plain body keys = %v, want exactly name/uri/description", keysOf(detail))
				}
				return
			}
			if !present {
				t.Fatalf("body lacks userNames: %s", body)
			}
			var got []string
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode userNames %s: %v", raw, err)
			}
			if got == nil || strings.Join(got, ",") != strings.Join(tc.wantUsers, ",") {
				t.Fatalf("userNames = %v (nil=%t), want %v", got, got == nil, tc.wantUsers)
			}
		})
	}

	// readonly_admin holds security:read — the gate is unchanged by the
	// parameter, so the widened body is served to it too.
	t251CreateUser(t, h, "auditor", "a@t.io", "pw-aud", nil, nil)
	promote := h.do(http.MethodPost, "/binflow/api/security/users/auditor", adminUser, adminPass,
		[]byte(`{"adminRole":"readonly_admin"}`), map[string]string{"Content-Type": "application/json"})
	if promote.StatusCode != http.StatusOK {
		t.Fatalf("promote auditor = %d body=%s", promote.StatusCode, mustGet(t, promote))
	}
	_ = promote.Body.Close()
	resp := h.do(http.MethodGet, "/binflow/api/security/groups/devs?includeUsers=true", "auditor", "pw-aud", nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("readonly_admin leg = %d %s, want 200", resp.StatusCode, body)
	}
	var widened struct {
		UserNames []string `json:"userNames"`
	}
	if err := json.Unmarshal([]byte(body), &widened); err != nil || widened.UserNames == nil {
		t.Fatalf("readonly_admin leg body %s carries no userNames array: %v", body, err)
	}
	if strings.Join(widened.UserNames, ",") != "u-a,u-b" {
		t.Fatalf("readonly_admin userNames = %v, want [u-a u-b]", widened.UserNames)
	}
}

// keysOf lists a decoded object's keys for failure messages.
func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestGroupListUnwidened pins K19: the list carries neither membersCount
// nor userNames anywhere in the raw body, and every entry is exactly the
// pre-M9 trio. The member census lives in ONE source (user_groups rows)
// projected by E2 (users.groups) and E5 (this file) on demand.
func TestGroupListUnwidened(t *testing.T) {
	h := newHarness(t)
	t252Group(t, h, "devs")
	t252Group(t, h, "empty")
	t251CreateUser(t, h, "u-a", "a@t.io", "pw-a", nil, []string{"devs"})

	resp := h.do(http.MethodGet, "/binflow/api/security/groups", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list = %d body=%s", resp.StatusCode, body)
	}
	for _, banned := range []string{"userNames", "membersCount"} {
		if strings.Contains(body, banned) {
			t.Fatalf("list body carries %s (K19 pins the list unwidened): %s", banned, body)
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, it := range items {
		if len(it) != 3 {
			t.Fatalf("entry keys = %v, want exactly name/uri/description: %s", keysOf(it), body)
		}
	}
}

// TestGroupMembershipCrossView (E2 x E5, task item 3): the same user_groups
// rows seen from the user side (list groups[], detail groups[]) and from
// the group side (?includeUsers=true userNames) must agree exactly — the
// group editor's shuttle and the users page's Groups column are two views
// of one fact, and this is the wire-level proof they cannot drift.
func TestGroupMembershipCrossView(t *testing.T) {
	h := newHarness(t)

	for _, g := range []string{"t252-g1", "t252-g2", "t252-g3"} {
		t252Group(t, h, g)
	}
	// Overlapping memberships across all three groups, declared in mixed
	// order so ordering must come from the reads, not the writes.
	t251CreateUser(t, h, "t252-u1", "u1@t.io", "pw-u1", nil, []string{"t252-g3", "t252-g1"})
	t251CreateUser(t, h, "t252-u2", "u2@t.io", "pw-u2", nil, []string{"t252-g2"})
	t251CreateUser(t, h, "t252-u3", "u3@t.io", "pw-u3", nil, []string{"t252-g1", "t252-g2", "t252-g3"})
	t251CreateUser(t, h, "t252-u4", "u4@t.io", "pw-u4", nil, nil) // group-less

	// View 1: the E2 users list — derive group -> members client-side
	// (exactly how the console's groups page derives its census, K19).
	resp := h.do(http.MethodGet, "/binflow/api/security/users", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("users list = %d body=%s", resp.StatusCode, body)
	}
	var users []*t251ListItem
	if err := json.Unmarshal([]byte(body), &users); err != nil {
		t.Fatalf("decode users %q: %v", body, err)
	}
	fromList := map[string][]string{}
	for _, u := range users {
		if u.Groups == nil {
			t.Fatalf("user %s groups = null, want []", u.Name)
		}
		for _, g := range u.Groups {
			fromList[g] = append(fromList[g], u.Name)
		}
	}

	// View 2: E5 per-group reads.
	fromGroup := map[string][]string{}
	for _, g := range []string{"t252-g1", "t252-g2", "t252-g3"} {
		resp := h.do(http.MethodGet, "/binflow/api/security/groups/"+g+"?includeUsers=true", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("group %s = %d body=%s", g, resp.StatusCode, body)
		}
		var detail struct {
			UserNames []string `json:"userNames"`
		}
		if err := json.Unmarshal([]byte(body), &detail); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		if detail.UserNames == nil {
			t.Fatalf("group %s userNames = null, want []", g)
		}
		fromGroup[g] = detail.UserNames
	}

	want := map[string][]string{
		"t252-g1": {"t252-u1", "t252-u3"},
		"t252-g2": {"t252-u2", "t252-u3"},
		"t252-g3": {"t252-u1", "t252-u3"},
	}
	for g, members := range want {
		if strings.Join(fromGroup[g], ",") != strings.Join(members, ",") {
			t.Fatalf("E5 userNames[%s] = %v, want %v", g, fromGroup[g], members)
		}
		if strings.Join(fromList[g], ",") != strings.Join(members, ",") {
			t.Fatalf("E2-derived members[%s] = %v, want %v", g, fromList[g], members)
		}
	}

	// View 3: the per-user detail's groups[] (the pre-M9 face) — each
	// user's set matches what both aggregate views imply.
	for name, groups := range map[string][]string{
		"t252-u1": {"t252-g1", "t252-g3"},
		"t252-u2": {"t252-g2"},
		"t252-u3": {"t252-g1", "t252-g2", "t252-g3"},
		"t252-u4": {},
	} {
		resp := h.do(http.MethodGet, "/binflow/api/security/users/"+name, adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("user %s = %d body=%s", name, resp.StatusCode, body)
		}
		var detail struct {
			Groups []string `json:"groups"`
		}
		if err := json.Unmarshal([]byte(body), &detail); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		if strings.Join(detail.Groups, ",") != strings.Join(groups, ",") {
			t.Fatalf("detail groups[%s] = %v, want %v", name, detail.Groups, groups)
		}
	}
}

// ---- N+1 gate (T-253's counting-decorator pattern, task item 2) ----

// t252CountingGroups counts the reads the E5 path may issue; everything
// else passes through to the real store.
type t252CountingGroups struct {
	metadata.GroupStore
	gets        int
	memberships int
}

func (g *t252CountingGroups) Get(ctx context.Context, name string) (*metadata.Group, error) {
	g.gets++
	return g.GroupStore.Get(ctx, name)
}

func (g *t252CountingGroups) MembershipsByGroup(ctx context.Context, group string) ([]string, error) {
	g.memberships++
	return g.GroupStore.MembershipsByGroup(ctx, group)
}

// t252CountingUsers counts Users().Get — the per-user read a fan-out would
// have to pay. The credential lookup itself costs one per request; a
// per-member walk would scale this with the member count.
type t252CountingUsers struct {
	metadata.UserStore
	gets int
}

func (u *t252CountingUsers) Get(ctx context.Context, username string) (*metadata.User, error) {
	u.gets++
	return u.UserStore.Get(ctx, username)
}

type t252CountingStore struct {
	metadata.Store
	groups *t252CountingGroups
	users  *t252CountingUsers
}

func (s *t252CountingStore) Groups() metadata.GroupStore { return s.groups }
func (s *t252CountingStore) Users() metadata.UserStore   { return s.users }

// TestGroupDetailIncludeUsersNPlusOneGate: one includeUsers=true GET is
// exactly one group read plus one membership JOIN, and its per-user read
// count is the credential lookup alone — the same 1 for a one-member group
// and a five-member group (K-independence is the N+1 proof; the pre-M9
// console pattern this endpoint retires cost one GET per member).
func TestGroupDetailIncludeUsersNPlusOneGate(t *testing.T) {
	var groups *t252CountingGroups
	var users *t252CountingUsers
	h := newHarnessAuth(t, nil, nil, func(md metadata.Store) metadata.Store {
		groups = &t252CountingGroups{GroupStore: md.Groups()}
		users = &t252CountingUsers{UserStore: md.Users()}
		return &t252CountingStore{Store: md, groups: groups, users: users}
	}, nil)

	t252Group(t, h, "t252-one")
	t252Group(t, h, "t252-five")
	t251CreateUser(t, h, "t252-m1", "m1@t.io", "pw-m1", nil, []string{"t252-one", "t252-five"})
	for i := 2; i <= 5; i++ {
		name := "t252-m" + string(rune('0'+i))
		t251CreateUser(t, h, name, name+"@t.io", "pw-"+name, nil, []string{"t252-five"})
	}

	// The parameterless GET never touches the membership seam at all.
	before := groups.memberships
	resp := h.do(http.MethodGet, "/binflow/api/security/groups/t252-five", adminUser, adminPass, nil, nil)
	_ = mustGet(t, resp)
	_ = resp.Body.Close()
	if groups.memberships != before {
		t.Fatalf("parameterless GET ran membership queries: %d -> %d", before, groups.memberships)
	}

	for _, tc := range []struct {
		group string
		mates int
	}{
		{"t252-one", 1}, {"t252-five", 5},
	} {
		g0, u0, m0 := groups.gets, users.gets, groups.memberships
		resp := h.do(http.MethodGet, "/binflow/api/security/groups/"+tc.group+"?includeUsers=true", adminUser, adminPass, nil, nil)
		body := mustGet(t, resp)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d body=%s", tc.group, resp.StatusCode, body)
		}
		if got := groups.gets - g0; got != 1 {
			t.Fatalf("%s: group reads = %d, want 1 (the detail Get)", tc.group, got)
		}
		if got := groups.memberships - m0; got != 1 {
			t.Fatalf("%s: membership queries = %d, want exactly 1", tc.group, got)
		}
		if got := users.gets - u0; got != 1 {
			t.Fatalf("%s with %d members: per-user reads = %d, want 1 (the credential lookup — a fan-out would scale with K)",
				tc.group, tc.mates, got)
		}
	}
}
