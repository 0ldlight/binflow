// T-217 acceptance surface (PRD M7 FR-65, V07~V10; ADR-0026 decision 3/4,
// architecture section 7.1 family 4's exception and family 7): the manage
// action on the permission-target wire, the m-holder coverage arm on the
// target edit verbs, the single-repo configuration family through
// CanManageRepo, and the usage endpoint's OR arm. Every fixture grant goes
// through the REAL REST wire — the manage letter itself is under test —
// except where noted (the T-215 seam helper stays the pattern for rows the
// wire cannot spell, which after this ticket is none).
//
// Wire note: the PRD V07 skeleton spells the target edit as PUT
// /api/v1/permissions/{name} with a 200; the shipped plane (section 7.1
// inventory, router.go's actual registrations) is POST /api/v1/permissions
// — create or wholly replace by body name — answering 201. The same
// architecture-over-PRD-skeleton ruling T-215's review applied to V01
// applies here; the assertions below pin the real wire.

package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// t217Setup provisions the FR-65 fixtures: the app-local repository with one
// artifact, a second repository outside the coverage, the app-admins group
// with carol in it, the manage-only carol2, plain dave, and the t-app target
// granting app-admins r/w/d/m through the REST wire (the manage letter's
// write half is itself under test).
func t217Setup(t *testing.T, h *harness) {
	t.Helper()
	t215Admin(t, h, http.MethodPut, "api/repositories/app-local",
		`{"rclass":"local","packageType":"generic"}`, 200)
	t215Admin(t, h, http.MethodPut, "api/repositories/other-local",
		`{"rclass":"local","packageType":"generic"}`, 200)
	if resp := t95Upload(h, "app-local", "lib.a", "lib-bytes"); resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed artifact: status %d (%s)", resp.StatusCode, mustGet(t, resp))
	}
	for _, u := range [][3]string{
		{"carol", "carol-pw", "carol@t.io"},
		{"carol2", "carol2-pw", "carol2@t.io"},
		{"dave", "dave-pw", "dave@t.io"},
	} {
		t215Admin(t, h, http.MethodPut, "api/security/users/"+u[0],
			`{"name":"`+u[0]+`","email":"`+u[2]+`","password":"`+u[1]+`","admin":false}`, 201)
	}
	// app-admins with carol (the membership rides the group wire, so carol's
	// manage bit arrives through Principal.Groups exactly as production).
	t215Admin(t, h, http.MethodPut, "api/security/groups/app-admins",
		`{"name":"app-admins","description":"app team"}`, 201)
	t215Admin(t, h, http.MethodPost, "api/security/users/carol",
		`{"groups":["app-admins"]}`, 200)
	// t-app: app-admins hold read/write/delete/manage on app-local.
	t217PutTarget(t, h, adminUser, adminPass, "t-app", `["app-local"]`,
		`{"groups":{"app-admins":["read","write","delete","manage"]},"users":{}}`, 201)
}

// t217PutTarget writes one permission target through the real wire and
// demands the status (the create-or-replace verb, PRD E-24/FR-5-AC8).
func t217PutTarget(t *testing.T, h *harness, user, pass, name, repos, principals string, want int) {
	t.Helper()
	body := `{"name":"` + name + `","repos":` + repos + `,"includePatterns":["**"],"principals":` + principals + `}`
	resp := t215As(t, h, http.MethodPost, "api/v1/permissions", user, pass, body)
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("POST /api/v1/permissions %s as %s = %d, want %d (body: %s)",
			name, user, resp.StatusCode, want, raw)
	}
}

// t217ListTargets decodes GET /api/v1/permissions (admin).
func t217ListTargets(t *testing.T, h *harness) []permissionBodyT217 {
	t.Helper()
	body := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)
	var out []permissionBodyT217
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode permission list: %v (%s)", err, body)
	}
	return out
}

// permissionBodyT217 mirrors the wire shape of one permission target.
type permissionBodyT217 struct {
	Name       string `json:"name"`
	Repos      []string
	Principals struct {
		Users  map[string][]string `json:"users"`
		Groups map[string][]string `json:"groups"`
	} `json:"principals"`
}

// t217Target finds one target in the admin list view.
func t217Target(t *testing.T, h *harness, name string) permissionBodyT217 {
	t.Helper()
	for _, b := range t217ListTargets(t, h) {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("permission target %s missing from the list view", name)
	return permissionBodyT217{}
}

// TestT217ManageWireRoundTrip (V07 + the wire): the manage letter lands
// through POST, echoes through GET, and the holder it grants can exercise
// exactly the derived capability — carol edits t-app to add dave:[read], and
// dave then reads the artifact.
func TestT217ManageWireRoundTrip(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h)

	// The echo (hook 2, the principals view): the granted manage bit reads
	// back in the r/w/d/m order. T-444 (ADR-0044 K68): the body spelled the
	// write alias, the echo renders canonical deploy-cache — the alias arm
	// is receive-only.
	tb := t217Target(t, h, "t-app")
	if got := tb.Principals.Groups["app-admins"]; len(got) != 4 ||
		got[0] != "read" || got[1] != "deploy-cache" || got[2] != "delete" || got[3] != "manage" {
		t.Fatalf("t-app app-admins actions = %v, want [read deploy-cache delete manage]", got)
	}

	// V07: carol (app-admins, r/w/d/m on app-local) replaces t-app adding
	// dave:[read]. 201 is the wire's create-or-replace status (see the file
	// comment on the PRD skeleton's PUT/200).
	t217PutTarget(t, h, "carol", "carol-pw", "t-app", `["app-local"]`,
		`{"groups":{"app-admins":["read","write","delete","manage"]},"users":{"dave":["read"]}}`, 201)

	// dave's grant is live on the content plane immediately.
	if got := t215Code(t, h, http.MethodGet, "app-local/lib.a", "dave", "dave-pw", ""); got != http.StatusOK {
		t.Errorf("dave GET artifact after carol's edit = %d, want 200", got)
	}
	// dave holds read only — the write leg of the same orthogonality.
	if got := t215Code(t, h, http.MethodPut, "app-local/dave.bin", "dave", "dave-pw", "x"); got != http.StatusForbidden {
		t.Errorf("dave PUT artifact = %d, want 403 (read-only grant)", got)
	}
}

// TestT217ManageHolderBoundaryLegs (V08 + the zero-privilege proof): carol's
// coverage is {app-local} — every boundary stays 403 and the addressed
// entities survive byte-identical.
func TestT217ManageHolderBoundaryLegs(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h)
	// t-other: a target whose repositories sit OUTSIDE carol's coverage.
	t217PutTarget(t, h, adminUser, adminPass, "t-other", `["other-local"]`,
		`{"groups":{"app-admins":["read"]},"users":{}}`, 201)

	before := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)

	legs := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create repository (family 6 create arm)", http.MethodPut, "api/repositories/brand-new",
			`{"rclass":"local","packageType":"generic"}`},
		{"target beyond coverage (V08)", http.MethodPost, "api/v1/permissions",
			`{"name":"t-other","repos":["other-local"],"principals":{"groups":{"app-admins":["read","manage"]},"users":{}}}`},
		{"target partially intersecting coverage", http.MethodPost, "api/v1/permissions",
			`{"name":"t-mix","repos":["app-local","other-local"],"principals":{"users":{"carol":["read"]}}}`},
		{"user management (security:write)", http.MethodPut, "api/security/users/dave",
			`{"email":"hax@t.io","password":"hax-pw-123"}`},
		{"token revoke (security:write)", http.MethodPost, "api/security/token/revoke", "token=sha256-deadbeef"},
		{"repo config outside coverage (family 7)", http.MethodPost, "api/repositories/other-local",
			`{"quotaBytes":99999}`},
		{"repository delete (family 6)", http.MethodDelete, "api/repositories/app-local", ""},
	}
	for _, leg := range legs {
		if got := t215Code(t, h, leg.method, leg.path, "carol", "carol-pw", leg.body); got != http.StatusForbidden {
			t.Errorf("%s: carol = %d, want 403", leg.name, got)
		}
	}
	// The global list never opened to manage holders (family 5, section
	// 11.30): the DETAIL of a covered repository is carol's surface, the
	// inventory is not.
	if got := t215Code(t, h, http.MethodGet, "api/repositories", "carol", "carol-pw", ""); got != http.StatusForbidden {
		t.Errorf("GET /api/repositories as manage holder = %d, want 403 (no global list, family 5)", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/repositories/app-local", "carol", "carol-pw", ""); got != http.StatusOK {
		t.Errorf("GET /api/repositories/app-local as manage holder = %d, want 200 (family 7 detail)", got)
	}
	// Zero side effects: the permission inventory is byte-identical, the
	// denied create left no repository, and dave's account is untouched (the
	// denied user write would have rotated his password).
	if after := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200); after != before {
		t.Errorf("permission inventory changed across denied edits\nbefore: %s\nafter:  %s", before, after)
	}
	if got := t215Code(t, h, http.MethodGet, "api/repositories/brand-new", adminUser, adminPass, ""); got != http.StatusNotFound {
		t.Errorf("GET brand-new after denied create = %d, want 404", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/v1/session", "dave", "dave-pw", ""); got != http.StatusOK {
		t.Errorf("dave's password survived the denied user write = %d, want 200", got)
	}
}

// TestT217ManageOnlyHolderOrthogonality (V09 + the usage OR arm): manage
// WITHOUT r/w/d carries no data-plane power — carol2 reads and writes the
// repository CONFIGURATION but neither artifacts nor artifact metadata,
// while the usage endpoint (whose gate is Can(r) ∨ Can(m)) flips open: the
// section 7.1 "manage holder without r" row that was unobservable before
// this ticket (no REST seam granted m).
func TestT217ManageOnlyHolderOrthogonality(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h)
	// carol2: manage ONLY — no read, no write, no delete, groupless.
	t217PutTarget(t, h, adminUser, adminPass, "t-mo", `["app-local"]`,
		`{"groups":{},"users":{"carol2":["manage"]}}`, 201)

	// The single-repo configuration family (family 7, through CanManageRepo).
	if got := t215Code(t, h, http.MethodGet, "api/repositories/app-local", "carol2", "carol2-pw", ""); got != http.StatusOK {
		t.Errorf("GET repo detail as manage-only holder = %d, want 200", got)
	}
	// A fresh target inside the coverage, then its replace (both edit arms).
	t217PutTarget(t, h, "carol2", "carol2-pw", "t-c2", `["app-local"]`,
		`{"groups":{},"users":{"dave":["read"]}}`, 201)
	t217PutTarget(t, h, "carol2", "carol2-pw", "t-c2", `["app-local"]`,
		`{"groups":{},"users":{}}`, 201)
	// The quota write rides the same family-7 write gate (K11: a holder who
	// may set quotaBytes may not be blind to the usage).
	if got := t215Code(t, h, http.MethodPost, "api/repositories/app-local", "carol2", "carol2-pw",
		`{"quotaBytes":12345}`); got != http.StatusOK {
		t.Errorf("POST quota as manage-only holder = %d, want 200 (family 7 write)", got)
	}
	if got := t215Code(t, h, http.MethodPut, "api/repositories/app-local", "carol2", "carol2-pw",
		`{"rclass":"local","packageType":"generic","description":"by carol2"}`); got != http.StatusOK {
		t.Errorf("PUT replace arm as manage-only holder = %d, want 200 (family 7 write)", got)
	}

	// The data plane stays closed: manage implies no r, no w.
	if got := t215Code(t, h, http.MethodGet, "app-local/lib.a", "carol2", "carol2-pw", ""); got != http.StatusForbidden {
		t.Errorf("GET artifact as manage-only holder = %d, want 403 (manage does not imply read)", got)
	}
	if got := t215Code(t, h, http.MethodPut, "app-local/x.bin", "carol2", "carol2-pw", "x"); got != http.StatusForbidden {
		t.Errorf("PUT artifact as manage-only holder = %d, want 403 (manage does not imply write)", got)
	}

	// The usage OR arm (family 7): open for the m-without-r holder, open for
	// a read-only holder (carol reads through the group row), closed for a
	// user with no grant on either axis — W26b's read arm unchanged.
	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/app-local", "carol2", "carol2-pw", ""); got != http.StatusOK {
		t.Errorf("usage as manage-only holder = %d, want 200 (the Can(m) OR arm)", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/app-local", "carol", "carol-pw", ""); got != http.StatusOK {
		t.Errorf("usage as read-granted carol = %d, want 200 (the Can(r) arm, W26b unchanged)", got)
	}
	t215Admin(t, h, http.MethodPut, "api/security/users/nogrant",
		`{"name":"nogrant","email":"n@t.io","password":"nog-pw","admin":false}`, 201)
	if got := t215Code(t, h, http.MethodGet, "api/v1/storage/usage/app-local", "nogrant", "nog-pw", ""); got != http.StatusForbidden {
		t.Errorf("usage as grantless user = %d, want 403", got)
	}
}

// TestT217CoverageMatrix: the family-4 exception's subset rule, table-driven
// over the three shapes — empty coverage, strict subset, partial
// intersection — on BOTH edit verbs (create/replace and delete), plus the
// existence-hiding 403 for unknown names asked by non-security-writers.
func TestT217CoverageMatrix(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h)
	// A second holder whose coverage spans BOTH repositories.
	t217PutTarget(t, h, adminUser, adminPass, "t-wide", `["app-local","other-local"]`,
		`{"groups":{},"users":{"carol2":["manage"]}}`, 201)

	cases := []struct {
		name  string
		user  string
		repos string
		want  int
	}{
		{"empty coverage", "dave", `["app-local"]`, http.StatusForbidden},
		{"subset of coverage", "carol2", `["app-local"]`, http.StatusCreated},
		{"exact coverage", "carol2", `["app-local","other-local"]`, http.StatusCreated},
		{"partial intersection", "carol", `["app-local","other-local"]`, http.StatusForbidden},
		{"disjoint", "carol", `["other-local"]`, http.StatusForbidden},
		{"empty repos body (vacuous set denies)", "carol2", `[]`, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)
			t217PutTarget(t, h, tc.user, tc.user+"-pw", "t-matrix-"+strings.ReplaceAll(tc.name, " ", "-"),
				tc.repos, `{"groups":{},"users":{}}`, tc.want)
			if tc.want == http.StatusForbidden {
				if after := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200); after != before {
					t.Errorf("denied edit changed the inventory")
				}
			}
		})
	}

	// DELETE legs: inside coverage succeeds, outside fails, and the unknown
	// name hides behind the same 403 for non-writers (the honest 404 stays
	// the security writer's).
	t217PutTarget(t, h, adminUser, adminPass, "t-del-in", `["app-local"]`, `{"groups":{},"users":{}}`, 201)
	t217PutTarget(t, h, adminUser, adminPass, "t-del-out", `["other-local"]`, `{"groups":{},"users":{}}`, 201)
	if got := t215Code(t, h, http.MethodDelete, "api/v1/permissions/t-del-in", "carol", "carol-pw", ""); got != http.StatusNoContent {
		t.Errorf("DELETE target inside coverage = %d, want 204", got)
	}
	if got := t215Code(t, h, http.MethodDelete, "api/v1/permissions/t-del-out", "carol", "carol-pw", ""); got != http.StatusForbidden {
		t.Errorf("DELETE target outside coverage = %d, want 403", got)
	}
	// The denied delete left the target standing.
	t217Target(t, h, "t-del-out")
	if got := t215Code(t, h, http.MethodDelete, "api/v1/permissions/no-such-target", "carol", "carol-pw", ""); got != http.StatusForbidden {
		t.Errorf("DELETE unknown target as manage holder = %d, want 403 (existence stays hidden)", got)
	}
	if got := t215Code(t, h, http.MethodDelete, "api/v1/permissions/no-such-target", adminUser, adminPass, ""); got != http.StatusNotFound {
		t.Errorf("DELETE unknown target as admin = %d, want the honest 404", got)
	}
	// A grantless user never learns target names either.
	if got := t215Code(t, h, http.MethodDelete, "api/v1/permissions/t-del-out", "dave", "dave-pw", ""); got != http.StatusForbidden {
		t.Errorf("DELETE target as grantless user = %d, want 403", got)
	}

	// Review round B1: SAME-NAMED replace whose body sits inside the coverage
	// but whose EXISTING row does not. POST is create-or-replace — without
	// the union judgment carol (coverage {app-local}) would wholesale-replace
	// t-ent and revoke dave's read on other-local; the union must 403 and the
	// inventory must survive byte-identical.
	t217PutTarget(t, h, adminUser, adminPass, "t-ent", `["other-local"]`,
		`{"groups":{},"users":{"dave":["read"]}}`, 201)
	beforeB1 := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200)
	t217PutTarget(t, h, "carol", "carol-pw", "t-ent", `["app-local"]`,
		`{"groups":{},"users":{}}`, http.StatusForbidden)
	if after := t215Admin(t, h, http.MethodGet, "api/v1/permissions", "", 200); after != beforeB1 {
		t.Errorf("same-named out-of-coverage replace changed the inventory\nbefore: %s\nafter:  %s", beforeB1, after)
	}
	// The control: a replace whose existing row ALSO sits inside the coverage
	// stays a 201 (union ⊆ coverage — V07's shape, unchanged by the fix).
	t217PutTarget(t, h, adminUser, adminPass, "t-in", `["app-local"]`, `{"groups":{},"users":{}}`, 201)
	t217PutTarget(t, h, "carol", "carol-pw", "t-in", `["app-local"]`,
		`{"groups":{},"users":{"dave":["read"]}}`, 201)
}

// TestT217NoPrivilegeChain (invariant 1, ADR-0026 decision 4): the manage
// bit opens NO global capability — carol collects the management plane's
// 403s on both read and write faces while keeping the family-7 passes.
func TestT217NoPrivilegeChain(t *testing.T) {
	h := newHarness(t)
	t217Setup(t, h)

	reads := []string{
		"api/v1/health", "api/v1/storage/stats", "api/v1/audit",
		"api/security/users", "api/security/groups",
	}
	for _, path := range reads {
		if got := t215Code(t, h, http.MethodGet, path, "carol", "carol-pw", ""); got != http.StatusForbidden {
			t.Errorf("GET %s as manage holder = %d, want 403 (m opens no system:/security: read)", path, got)
		}
	}
	writes := []struct{ method, path, body string }{
		{http.MethodPost, "api/v1/system/gc", `{"apply":false}`},
		{http.MethodPost, "api/v1/storage/migration/start", ""},
		{http.MethodPost, "api/v1/replications", `{}`},
		{http.MethodPut, "api/security/groups/app-admins", `{"name":"app-admins"}`},
		{http.MethodPost, "api/security/users", `{"name":"eve","email":"e@t.io","password":"eve-pw-123"}`},
		{http.MethodPost, "api/security/users/carol", `{"adminRole":"admin"}`},
	}
	for _, w := range writes {
		if got := t215Code(t, h, w.method, w.path, "carol", "carol-pw", w.body); got != http.StatusForbidden {
			t.Errorf("%s %s as manage holder = %d, want 403 (m opens no write capability)", w.method, w.path, got)
		}
	}
	// Self-service (family 8) is untouched: whoami answers for carol.
	if got := t215Code(t, h, http.MethodGet, "api/v1/session", "carol", "carol-pw", ""); got != http.StatusOK {
		t.Errorf("whoami as manage holder = %d, want 200 (family 8 unchanged)", got)
	}
}
