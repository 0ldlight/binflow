package metadata_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// putUser inserts a user row (email optional) so membership FKs resolve.
func putUser(t *testing.T, st metadata.Store, username, email string) {
	t.Helper()
	hash, err := metadata.HashPassword("pw-" + username)
	if err != nil {
		t.Fatalf("hashing for %s: %v", username, err)
	}
	now := metadata.Now()
	if err := st.Users().Create(context.Background(), &metadata.User{
		Username: username, PasswordHash: hash, Enabled: true,
		Email: email, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
}

func putGroup(t *testing.T, st metadata.Store, name, description string) {
	t.Helper()
	now := metadata.Now()
	if err := st.Groups().Create(context.Background(), &metadata.Group{
		Name: name, Description: description, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create group %s: %v", name, err)
	}
}

func groupNames(gs []*metadata.Group) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

// T-90 AC ②: group CRUD — create/duplicate/get/update/missing/delete/list.
func TestGroupStoreCRUD(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	putGroup(t, st, "devs", "developers")
	if err := st.Groups().Create(ctx, &metadata.Group{
		Name: "devs", Description: "dup", CreatedAt: now, UpdatedAt: now,
	}); err == nil {
		t.Fatal("duplicate group name must fail (UNIQUE)")
	}
	got, err := st.Groups().Get(ctx, "devs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID <= 0 || got.Description != "developers" || got.CreatedAt != now {
		t.Fatalf("group roundtrip = %+v", got)
	}
	if _, err := st.Groups().Get(ctx, "nope"); !errors.Is(err, metadata.ErrGroupNotFound) {
		t.Fatalf("get missing err = %v, want ErrGroupNotFound", err)
	}

	putGroup(t, st, "qa", "quality")
	updated := metadata.Now()
	if err := st.Groups().Update(ctx, &metadata.Group{
		Name: "devs", Description: "eng", UpdatedAt: updated,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err = st.Groups().Get(ctx, "devs")
	if err != nil || got.Description != "eng" || got.UpdatedAt != updated {
		t.Fatalf("after update = %+v (err %v)", got, err)
	}
	if err := st.Groups().Update(ctx, &metadata.Group{
		Name: "nope", Description: "x", UpdatedAt: updated,
	}); !errors.Is(err, metadata.ErrGroupNotFound) {
		t.Fatalf("update missing err = %v, want ErrGroupNotFound", err)
	}

	list, err := st.Groups().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Join(groupNames(list), ",") != "devs,qa" {
		t.Fatalf("list = %v, want [devs qa] ordered by name", groupNames(list))
	}

	if err := st.Groups().Delete(ctx, "qa"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.Groups().Delete(ctx, "qa"); !errors.Is(err, metadata.ErrGroupNotFound) {
		t.Fatalf("double delete err = %v, want ErrGroupNotFound", err)
	}
}

// T-90 AC ②: membership upsert / release-by-user / resolve-by-user.
func TestGroupMembership(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putUser(t, st, "jane", "")
	putUser(t, st, "bob", "")
	putGroup(t, st, "devs", "")
	putGroup(t, st, "qa", "")

	// Unknown user starts group-less: empty slice, not an error.
	gs, err := st.Groups().GroupsOfUser(ctx, "jane")
	if err != nil || len(gs) != 0 {
		t.Fatalf("GroupsOfUser fresh = %v (err %v), want empty", groupNames(gs), err)
	}

	// Upsert: apply, re-apply (idempotent), with duplicates in the list.
	for i, want := range [][]string{{"devs", "qa"}, {"devs", "qa", "devs"}, {"devs", "qa"}} {
		if err := st.Groups().SetUserGroups(ctx, "jane", want); err != nil {
			t.Fatalf("SetUserGroups #%d: %v", i, err)
		}
		got, err := st.Groups().GroupsOfUser(ctx, "jane")
		if err != nil {
			t.Fatalf("GroupsOfUser #%d: %v", i, err)
		}
		if strings.Join(groupNames(got), ",") != "devs,qa" {
			t.Fatalf("membership #%d = %v, want [devs qa]", i, groupNames(got))
		}
	}

	// Replace releases the stale membership (the per-user release arm).
	if err := st.Groups().SetUserGroups(ctx, "jane", []string{"qa"}); err != nil {
		t.Fatalf("SetUserGroups replace: %v", err)
	}
	if got, _ := st.Groups().GroupsOfUser(ctx, "jane"); strings.Join(groupNames(got), ",") != "qa" {
		t.Fatalf("membership after replace = %v, want [qa]", groupNames(got))
	}

	// Clear.
	if err := st.Groups().SetUserGroups(ctx, "jane", nil); err != nil {
		t.Fatalf("SetUserGroups clear: %v", err)
	}
	if got, _ := st.Groups().GroupsOfUser(ctx, "jane"); len(got) != 0 {
		t.Fatalf("membership after clear = %v, want empty", groupNames(got))
	}

	// Independent users: bob's set does not leak into jane's.
	if err := st.Groups().SetUserGroups(ctx, "bob", []string{"devs"}); err != nil {
		t.Fatalf("SetUserGroups bob: %v", err)
	}
	if err := st.Groups().SetUserGroups(ctx, "jane", []string{"qa"}); err != nil {
		t.Fatalf("SetUserGroups jane: %v", err)
	}
	if got, _ := st.Groups().GroupsOfUser(ctx, "bob"); strings.Join(groupNames(got), ",") != "devs" {
		t.Fatalf("bob membership = %v, want [devs]", groupNames(got))
	}

	// Unknown group: typed error carrying the name (the SE-06 400 wording
	// source).
	err = st.Groups().SetUserGroups(ctx, "jane", []string{"devs", "ghost"})
	if !errors.Is(err, metadata.ErrGroupNotFound) {
		t.Fatalf("unknown group err = %v, want ErrGroupNotFound", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error %q does not name the missing group", err)
	}
	// The failed replace must not have half-applied.
	if got, _ := st.Groups().GroupsOfUser(ctx, "jane"); strings.Join(groupNames(got), ",") != "qa" {
		t.Fatalf("membership after failed replace = %v, want unchanged [qa]", groupNames(got))
	}

	// Unknown user: the FK rejects the row.
	if err := st.Groups().SetUserGroups(ctx, "ghost", []string{"devs"}); err == nil {
		t.Fatal("SetUserGroups for missing user must fail (FK)")
	}
}

// T-90 AC ③: member cascade — deleting a group drops its memberships, and a
// membership only ever resolves through existing groups.
func TestGroupDeleteCascadesMembers(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putUser(t, st, "jane", "")
	putGroup(t, st, "devs", "")
	putGroup(t, st, "qa", "")
	if err := st.Groups().SetUserGroups(ctx, "jane", []string{"devs", "qa"}); err != nil {
		t.Fatalf("SetUserGroups: %v", err)
	}
	if err := st.Groups().Delete(ctx, "devs"); err != nil {
		t.Fatalf("group delete: %v", err)
	}
	got, err := st.Groups().GroupsOfUser(ctx, "jane")
	if err != nil || strings.Join(groupNames(got), ",") != "qa" {
		t.Fatalf("membership after group delete = %v (err %v), want [qa]", groupNames(got), err)
	}

	// User-side cascade: deleting the user drops their membership rows.
	if err := st.Users().Delete(ctx, "jane"); err != nil {
		t.Fatalf("user delete: %v", err)
	}
	// jane is gone; re-create the same name must start group-less (rows
	// really were removed, not orphaned under the old user id space).
	putUser(t, st, "jane", "")
	got, err = st.Groups().GroupsOfUser(ctx, "jane")
	if err != nil || len(got) != 0 {
		t.Fatalf("membership after user delete+recreate = %v (err %v), want empty", groupNames(got), err)
	}
}

// T-90 AC ①/②: users.email lands (004 column) and round-trips through
// create, update and list (the FR-27-AC8 / SE-05 surface).
func TestUserEmailColumn(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	putUser(t, st, "jane", "jane@example.test")
	got, err := st.Users().Get(ctx, "jane")
	if err != nil || got.Email != "jane@example.test" {
		t.Fatalf("email roundtrip = %q (err %v)", got.Email, err)
	}

	// Legacy-shaped row (created without an email statement) reads ''.
	putUser(t, st, "old-timer", "")
	if got, _ := st.Users().Get(ctx, "old-timer"); got.Email != "" {
		t.Fatalf("unset email = %q, want empty", got.Email)
	}

	if err := st.Users().UpdateEmail(ctx, "jane", "jane2@example.test"); err != nil {
		t.Fatalf("update email: %v", err)
	}
	if got, _ := st.Users().Get(ctx, "jane"); got.Email != "jane2@example.test" {
		t.Fatalf("email after update = %q", got.Email)
	}
	if err := st.Users().UpdateEmail(ctx, "nope", "x@example.test"); !errors.Is(err, metadata.ErrUserNotFound) {
		t.Fatalf("update email missing err = %v, want ErrUserNotFound", err)
	}

	list, err := st.Users().List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byName := map[string]string{}
	for _, u := range list {
		byName[u.Username] = u.Email
	}
	if byName["admin"] != "" || byName["jane"] != "jane2@example.test" || byName["old-timer"] != "" {
		t.Fatalf("list emails = %v", byName)
	}
	// GetByPasswordHash keeps the widened row shape. The stored hash is
	// salted, so reuse the row's own hash rather than re-hashing.
	stored, err := st.Users().Get(ctx, "jane")
	if err != nil {
		t.Fatalf("get jane: %v", err)
	}
	byHash, err := st.Users().GetByPasswordHash(ctx, stored.PasswordHash)
	if err != nil || byHash.Email != "jane2@example.test" {
		t.Fatalf("get-by-hash = %+v (err %v)", byHash, err)
	}
}

func sessionHash(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func putSession(t *testing.T, st metadata.Store, plaintext, username, createdAt, expiresAt string) {
	t.Helper()
	if err := st.WebSessions().Create(context.Background(), &metadata.WebSession{
		IDHash: sessionHash(plaintext), Username: username,
		CreatedAt: createdAt, ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("session create %s: %v", plaintext, err)
	}
}

// T-90 AC ②: web session lifecycle — create/get-by-hash/touch/revoke with
// the tokens-rule storage form (sha256 only, NFR-S2 shape).
func TestWebSessionStore(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	now := metadata.Now()

	putUser(t, st, "jane", "")
	plaintext := "bf-web-session-plaintext-9c1d2e"
	idHash := sessionHash(plaintext)
	if err := st.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: idHash, Username: "jane",
		CreatedAt: now, ExpiresAt: "2027-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Same id twice: primary key conflict.
	if err := st.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: idHash, Username: "jane", CreatedAt: now, ExpiresAt: "2027-01-01T00:00:00Z",
	}); err == nil {
		t.Fatal("duplicate session id hash must fail")
	}

	got, err := st.WebSessions().GetBySHA256(ctx, idHash)
	if err != nil {
		t.Fatalf("get-by-hash: %v", err)
	}
	if got.Username != "jane" || got.CreatedAt != now || got.ExpiresAt != "2027-01-01T00:00:00Z" {
		t.Fatalf("session roundtrip = %+v", got)
	}
	if got.LastUsedAt != "" || got.RevokedAt != "" {
		t.Fatalf("fresh session carries activity stamps: %+v", got)
	}
	if _, err := st.WebSessions().GetBySHA256(ctx, sessionHash("never-issued")); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("get missing err = %v, want ErrWebSessionNotFound", err)
	}

	// NFR-S2 shape: the plaintext must not appear anywhere in the database
	// files — only its sha256 is stored.
	dbPath := st.(interface{ DBPath() string }).DBPath()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(dbPath + suffix) // fixed suffix over the test temp database (G304 excluded globally)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("reading %s: %v", dbPath+suffix, err)
		}
		if strings.Contains(string(data), plaintext) {
			t.Fatalf("session plaintext found in %s — web_sessions must store sha256 only", dbPath+suffix)
		}
	}

	if err := st.WebSessions().Touch(ctx, idHash, "2026-08-19T01:02:03Z"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	if got, _ = st.WebSessions().GetBySHA256(ctx, idHash); got.LastUsedAt != "2026-08-19T01:02:03Z" {
		t.Fatalf("last_used_at after touch = %q", got.LastUsedAt)
	}
	if err := st.WebSessions().Touch(ctx, sessionHash("never-issued"), now); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("touch missing err = %v, want ErrWebSessionNotFound", err)
	}

	// Revoke is idempotent; the row stays readable so replays can be told
	// apart from unknown cookies.
	if err := st.WebSessions().Revoke(ctx, idHash, "2026-08-19T02:00:00Z"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := st.WebSessions().Revoke(ctx, idHash, "2026-08-19T02:00:01Z"); err != nil {
		t.Fatalf("re-revoke: %v", err)
	}
	if got, _ = st.WebSessions().GetBySHA256(ctx, idHash); got.RevokedAt != "2026-08-19T02:00:01Z" {
		t.Fatalf("revoked_at = %q", got.RevokedAt)
	}
	if err := st.WebSessions().Revoke(ctx, sessionHash("never-issued"), now); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("revoke missing err = %v, want ErrWebSessionNotFound", err)
	}

	if err := st.WebSessions().Delete(ctx, idHash); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.WebSessions().Delete(ctx, idHash); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("double delete err = %v, want ErrWebSessionNotFound", err)
	}
}

// T-90 AC ②: sweep candidates — expired and revoked rows surface, live rows
// do not; ordering and limit hold; deleting a user cascades their sessions.
func TestWebSessionSweepCandidates(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putUser(t, st, "jane", "")
	putUser(t, st, "bob", "")

	// expires_at spread: two expired, one live, one revoked-but-live.
	putSession(t, st, "exp-old", "jane", "2026-08-01T00:00:00Z", "2026-08-01T01:00:00Z")
	putSession(t, st, "exp-new", "jane", "2026-08-01T00:00:00Z", "2026-08-19T09:00:00Z")
	putSession(t, st, "live", "jane", "2026-08-19T10:00:00Z", "2027-01-01T00:00:00Z")
	putSession(t, st, "revoked", "bob", "2026-08-19T10:00:00Z", "2027-01-01T00:00:00Z")
	if err := st.WebSessions().Revoke(ctx, sessionHash("revoked"), "2026-08-19T11:00:00Z"); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	now := "2026-08-19T12:00:00Z"
	got, err := st.WebSessions().ListSweepable(ctx, now, 0)
	if err != nil {
		t.Fatalf("ListSweepable: %v", err)
	}
	// Ordered by expires_at (the two expired first), revoked rows after.
	wantOrder := []string{sessionHash("exp-old"), sessionHash("exp-new"), sessionHash("revoked")}
	if len(got) != len(wantOrder) {
		t.Fatalf("sweepable = %d rows, want %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].IDHash != w {
			t.Fatalf("sweepable[%d] = %s, want %s (order by expires_at)", i, got[i].IDHash, w)
		}
	}

	// limit is honored.
	if got, err = st.WebSessions().ListSweepable(ctx, now, 1); err != nil || len(got) != 1 {
		t.Fatalf("ListSweepable limit=1 = %d rows (err %v)", len(got), err)
	}
	// A later "now" still reports nothing for the live session.
	if got, err = st.WebSessions().ListSweepable(ctx, "2026-08-19T12:00:01Z", 0); err != nil || len(got) != 3 {
		t.Fatalf("sweepable just past now = %d rows (err %v), want 3 (live session stays out)", len(got), err)
	}

	// Sweeper loop: delete each candidate; nothing sweepable remains.
	for _, w := range got {
		if err := st.WebSessions().Delete(ctx, w.IDHash); err != nil {
			t.Fatalf("sweep delete: %v", err)
		}
	}
	if got, err = st.WebSessions().ListSweepable(ctx, now, 0); err != nil || len(got) != 0 {
		t.Fatalf("sweepable after sweep = %d rows (err %v), want 0", len(got), err)
	}
	if _, err := st.WebSessions().GetBySHA256(ctx, sessionHash("live")); err != nil {
		t.Fatalf("live session lost by sweep: %v", err)
	}

	// User delete cascades their sessions (FK).
	if err := st.Users().Delete(ctx, "jane"); err != nil {
		t.Fatalf("user delete: %v", err)
	}
	if _, err := st.WebSessions().GetBySHA256(ctx, sessionHash("live")); !errors.Is(err, metadata.ErrWebSessionNotFound) {
		t.Fatalf("session survived user delete: err = %v, want ErrWebSessionNotFound", err)
	}
}

// T-90 AC ②: AuditStore.Query filter matrix — equality filters, closed-open
// Since/Until window, default limit.
func TestAuditQueryFilters(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	appendEvent := func(time, actor, action, repo string) {
		t.Helper()
		if err := st.Audits().Append(ctx, &metadata.AuditEvent{
			Time: time, Actor: actor, Action: action, RepoKey: repo, Path: "p", Detail: "{}",
		}); err != nil {
			t.Fatalf("append %s/%s: %v", actor, action, err)
		}
	}
	// Times are deliberately out of insertion order: Query orders by time,
	// not id.
	appendEvent("2026-08-19T10:00:00Z", "jane", "deploy", "lib")
	appendEvent("2026-08-19T11:00:00Z", "ci-bot", "deploy", "lib")
	appendEvent("2026-08-19T12:00:00Z", "jane", "delete", "lib")
	appendEvent("2026-08-19T12:00:00Z", "jane", "deploy", "app")
	appendEvent("2026-08-19T13:00:00Z", "admin", "login.success", "")

	tests := []struct {
		name  string
		query metadata.AuditQuery
		want  []string // "<time>|<actor>|<action>|<repo>" newest-first
	}{
		{"no filter", metadata.AuditQuery{}, []string{
			"2026-08-19T13:00:00Z|admin|login.success|",
			"2026-08-19T12:00:00Z|jane|deploy|app",
			"2026-08-19T12:00:00Z|jane|delete|lib",
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"actor", metadata.AuditQuery{Actor: "jane"}, []string{
			"2026-08-19T12:00:00Z|jane|deploy|app",
			"2026-08-19T12:00:00Z|jane|delete|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"action", metadata.AuditQuery{Action: "deploy"}, []string{
			"2026-08-19T12:00:00Z|jane|deploy|app",
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"repo", metadata.AuditQuery{RepoKey: "lib"}, []string{
			"2026-08-19T12:00:00Z|jane|delete|lib",
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"actor+action", metadata.AuditQuery{Actor: "jane", Action: "delete"}, []string{
			"2026-08-19T12:00:00Z|jane|delete|lib",
		}},
		{"since closed bound", metadata.AuditQuery{Since: "2026-08-19T11:00:00Z"}, []string{
			"2026-08-19T13:00:00Z|admin|login.success|",
			"2026-08-19T12:00:00Z|jane|deploy|app",
			"2026-08-19T12:00:00Z|jane|delete|lib",
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
		}},
		{"until open bound", metadata.AuditQuery{Until: "2026-08-19T12:00:00Z"}, []string{
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"window", metadata.AuditQuery{Since: "2026-08-19T10:00:00Z", Until: "2026-08-19T13:00:00Z"}, []string{
			"2026-08-19T12:00:00Z|jane|deploy|app",
			"2026-08-19T12:00:00Z|jane|delete|lib",
			"2026-08-19T11:00:00Z|ci-bot|deploy|lib",
			"2026-08-19T10:00:00Z|jane|deploy|lib",
		}},
		{"empty result", metadata.AuditQuery{Actor: "nobody"}, nil},
		{"limit", metadata.AuditQuery{Limit: 1}, []string{
			"2026-08-19T13:00:00Z|admin|login.success|",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.Audits().Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			var gotKeys []string
			for _, e := range got {
				gotKeys = append(gotKeys, fmt.Sprintf("%s|%s|%s|%s", e.Time, e.Actor, e.Action, e.RepoKey))
			}
			if len(gotKeys) != len(tt.want) {
				t.Fatalf("Query = %v, want %v", gotKeys, tt.want)
			}
			for i := range gotKeys {
				if gotKeys[i] != tt.want[i] {
					t.Fatalf("Query[%d] = %q, want %q (full: %v)", i, gotKeys[i], tt.want[i], gotKeys)
				}
			}
		})
	}

	// Ties on time order by id DESC within the equal band (the two 12:00
	// events: deploy|app was inserted after delete|lib, so it leads).
	got, err := st.Audits().Query(ctx, metadata.AuditQuery{Since: "2026-08-19T12:00:00Z", Until: "2026-08-19T13:00:00Z"})
	if err != nil {
		t.Fatalf("Query ties: %v", err)
	}
	if len(got) != 2 || got[0].Action != "deploy" || got[1].Action != "delete" {
		t.Fatalf("tie order = %+v, want deploy(newer id) before delete", got)
	}

	// Limit defaults to 100 when unset (mirrors List).
	for i := 0; i < 105; i++ {
		appendEvent(fmt.Sprintf("2026-08-20T00:%02d:00Z", i%60), "bulk", "probe", "lib")
	}
	got, err = st.Audits().Query(ctx, metadata.AuditQuery{Actor: "bulk"})
	if err != nil {
		t.Fatalf("Query default limit: %v", err)
	}
	if len(got) != 100 {
		t.Fatalf("default limit = %d rows, want 100", len(got))
	}
}

// T-90 AC ②: keyset cursor walk — page-following must cover the full set
// exactly once, stay strictly descending, and reject malformed cursors.
func TestAuditQueryCursorWalk(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	// 7 events over 3 distinct times with same-time ties (id is the
	// tiebreaker). Times inserted out of order to prove time-ordered output.
	type seed struct{ time, actor string }
	seeds := []seed{
		{"2026-08-19T02:00:00Z", "u1"},
		{"2026-08-19T03:00:00Z", "u2"},
		{"2026-08-19T01:00:00Z", "u3"},
		{"2026-08-19T02:00:00Z", "u4"},
		{"2026-08-19T03:00:00Z", "u5"},
		{"2026-08-19T02:00:00Z", "u6"},
		{"2026-08-19T03:00:00Z", "u7"},
	}
	for _, s := range seeds {
		if err := st.Audits().Append(ctx, &metadata.AuditEvent{
			Time: s.time, Actor: s.actor, Action: "probe", Detail: "{}",
		}); err != nil {
			t.Fatalf("append %s: %v", s.actor, err)
		}
	}

	full, err := st.Audits().Query(ctx, metadata.AuditQuery{})
	if err != nil {
		t.Fatalf("full Query: %v", err)
	}
	if len(full) != len(seeds) {
		t.Fatalf("full set = %d, want %d", len(full), len(seeds))
	}

	// Walk in pages of 2 (3 pages + remainder).
	var walked []int64
	cursor := ""
	for {
		page, err := st.Audits().Query(ctx, metadata.AuditQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("Query page (cursor %q): %v", cursor, err)
		}
		for _, e := range page {
			walked = append(walked, e.ID)
		}
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		cursor = last.Time + "|" + fmt.Sprintf("%d", last.ID)
	}
	if len(walked) != len(full) {
		t.Fatalf("walked %d ids, want %d", len(walked), len(full))
	}
	for i := range full {
		if walked[i] != full[i].ID {
			t.Fatalf("walk[%d] = id %d, want %d — pagination drifted", i, walked[i], full[i].ID)
		}
	}

	// A cursor past the oldest row ends the walk.
	oldest := full[len(full)-1]
	page, err := st.Audits().Query(ctx, metadata.AuditQuery{Cursor: oldest.Time + "|" + fmt.Sprintf("%d", oldest.ID)})
	if err != nil || len(page) != 0 {
		t.Fatalf("page past oldest = %d rows (err %v), want 0", len(page), err)
	}

	// Malformed cursors are typed errors.
	for _, bad := range []string{"garbage", "time-only", "2026-01-01T00:00:00Z|abc", "2026-01-01T00:00:00Z|-1", "2026-01-01T00:00:00Z|0", "a|b|c"} {
		if _, err := st.Audits().Query(ctx, metadata.AuditQuery{Cursor: bad}); !errors.Is(err, metadata.ErrInvalidCursor) {
			t.Fatalf("cursor %q err = %v, want ErrInvalidCursor", bad, err)
		}
	}
}

// T-90 AC ③ (-race): concurrent membership churn on shared groups — the
// per-user replace stays isolated under WAL concurrency.
func TestGroupMembershipConcurrentChurn(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	users := []string{"u1", "u2", "u3", "u4"}
	for _, u := range users {
		putUser(t, st, u, "")
	}
	putGroup(t, st, "devs", "")
	putGroup(t, st, "qa", "")

	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func(user string) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				groups := []string{"devs", "qa"}
				if i%2 == 0 {
					groups = []string{"qa", "devs"}
				}
				if err := st.Groups().SetUserGroups(ctx, user, groups); err != nil {
					t.Errorf("SetUserGroups %s #%d: %v", user, i, err)
					return
				}
				got, err := st.Groups().GroupsOfUser(ctx, user)
				if err != nil {
					t.Errorf("GroupsOfUser %s #%d: %v", user, i, err)
					return
				}
				if strings.Join(groupNames(got), ",") != "devs,qa" {
					t.Errorf("membership %s #%d = %v, want [devs qa]", user, i, groupNames(got))
					return
				}
			}
		}(u)
	}
	wg.Wait()
}

// T-90 AC ③ (-race): concurrent session touch on one row — every write
// lands and the final value is one of the writers'.
func TestWebSessionConcurrentTouch(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	putUser(t, st, "jane", "")
	idHash := sessionHash("race-touch")
	if err := st.WebSessions().Create(ctx, &metadata.WebSession{
		IDHash: idHash, Username: "jane",
		CreatedAt: metadata.Now(), ExpiresAt: "2027-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	const writers = 16
	stamps := make([]string, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		stamps[i] = fmt.Sprintf("2026-08-19T%02d:%02d:00Z", 1+i%10, i)
		wg.Add(1)
		go func(stamp string) {
			defer wg.Done()
			if err := st.WebSessions().Touch(ctx, idHash, stamp); err != nil {
				t.Errorf("touch %s: %v", stamp, err)
			}
		}(stamps[i])
	}
	wg.Wait()

	got, err := st.WebSessions().GetBySHA256(ctx, idHash)
	if err != nil {
		t.Fatalf("get after race: %v", err)
	}
	for _, s := range stamps {
		if got.LastUsedAt == s {
			return // one of the writers won — order is unspecified
		}
	}
	t.Fatalf("last_used_at = %q, none of the writers' stamps %v", got.LastUsedAt, stamps)
}
