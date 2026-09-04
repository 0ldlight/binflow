package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-254 (M9 E6/E9, ADR-0030 / architecture section 14.1.6 + 14.1.9,
// FR-79.2): GET /api/v1/permissions?filter=manage over the real stack —
// the m-holder readability face. Under test:
//
//   - the role matrix: admin/readonly_admin receive the full list
//     BYTE-EQUAL to their no-filter response; an m-holder (direct or via
//     group) receives exactly the targets whose repositories sit inside
//     its coverage; everyone else answers the SAME 403 the frozen route
//     gate answers — a principal without the manage bit cannot distinguish
//     the two branches;
//   - the no-filter branch's byte-level snapshot (M9's additive-only
//     decree, architecture 14.5-2);
//   - seed-m9's u9 leg at fixture scale: t-in listed with COMPLETE fields,
//     t-out / the group target / the partially-covered target appearing
//     nowhere in the body (NFR-S49 zero leakage, negative grep);
//   - the parameter edges along T-253's ?include conventions;
//   - the N+1 gate: coverage evaluation and filtered rendering are
//     single-trip, whatever the target count.

// t254FrozenForbidden is the route gate's 403 body, byte for byte — the
// same writeError rendering the empty-coverage arm must reproduce.
const t254FrozenForbidden = "{\n  \"errors\": [\n    {\n      \"status\": 403,\n      \"message\": \"administrator privileges required\"\n    }\n  ]\n}\n"

// t254Setup provisions the seed-m9 fixture shape at six-repository scale:
//
//	r00 r01  t-in   u9h MANAGE            — u9h's coverage
//	r02 r03  t-out  u9h READ              — outside: hidden, POST stays 403
//	r04 r05  t-grp  GROUP m9-g01h MANAGE  — u1h (member) sees it, u9h not
//	r02 r03  t-u8r  u8h READ              — invisible to u9h (repos outside)
//	r00 r02  t-part partm MANAGE          — partially inside u9h's coverage
//	                                     //  (r00 yes, r02 no): still hidden
//
// The interesting consequence the subset rule carries (and the partm leg
// pins): the filter is about REPOSITORIES, not principals — partm holds m
// on r00+r02, so t-part renders for it, and a target whose every repo sits
// inside the coverage renders for ANY holder of that coverage (B1's replace
// arm keys on the stored repos, not on who held grants).
func t254Setup(t *testing.T, h *harness) {
	t.Helper()
	for i := 0; i < 6; i++ {
		t215Admin(t, h, http.MethodPut, fmt.Sprintf("api/repositories/t254-r0%d", i),
			`{"rclass":"local","packageType":"generic"}`, 200)
	}
	for _, u := range []struct{ name, role string }{
		{"u9h", ""}, {"u1h", ""}, {"u8h", ""}, {"nog", ""}, {"partm", ""}, {"roat", `"adminRole":"readonly_admin",`},
	} {
		body := `{"name":"` + u.name + `","email":"` + u.name + `@t.io","password":"` + u.name + `-pw",` + u.role + `"admin":false}`
		t215Admin(t, h, http.MethodPut, "api/security/users/"+u.name, body, 201)
	}
	t215Admin(t, h, http.MethodPut, "api/security/groups/m9-g01h", `{"name":"m9-g01h","description":"t254"}`, 201)
	t215Admin(t, h, http.MethodPost, "api/security/users/u1h", `{"groups":["m9-g01h"]}`, 200)

	target := func(name string, repos string, principals string) {
		t.Helper()
		t215Admin(t, h, http.MethodPost, "api/v1/permissions",
			`{"name":"`+name+`","repos":`+repos+`,"includePatterns":["**"],"excludePatterns":[],"principals":`+principals+`}`, 201)
	}
	target("t-in", `["t254-r00","t254-r01"]`, `{"users":{"u9h":["manage"]},"groups":{}}`)
	target("t-out", `["t254-r02","t254-r03"]`, `{"users":{"u9h":["read"]},"groups":{}}`)
	target("t-grp", `["t254-r04","t254-r05"]`, `{"users":{},"groups":{"m9-g01h":["read","write","delete","manage"]}}`)
	target("t-u8r", `["t254-r02","t254-r03"]`, `{"users":{"u8h":["read"]},"groups":{}}`)
	target("t-part", `["t254-r00","t254-r02"]`, `{"users":{"partm":["manage"]},"groups":{}}`)
}

// t254Get issues one permissions GET, demanding the status and returning
// the raw body.
func t254Get(t *testing.T, h *harness, query, user, pass string, want int) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/permissions"+query, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read permissions body: %v", err)
	}
	if resp.StatusCode != want {
		t.Fatalf("GET permissions%s as %s = %d, want %d (body %s)", query, user, resp.StatusCode, want, got)
	}
	return string(got)
}

// t254Names projects the target names, asserting the bare-array shape and
// the name ordering while at it.
func t254Names(t *testing.T, body string) []string {
	t.Helper()
	if !strings.HasPrefix(strings.TrimSpace(body), "[") {
		t.Fatalf("body is not a bare array: %s", body)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("targets not ordered by name: %v", names)
		}
	}
	return names
}

// TestT254FilterManageRoleMatrix: every authenticated role against both
// branches of the endpoint.
func TestT254FilterManageRoleMatrix(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t254Setup(t, h)

	cases := []struct {
		name string
		user string
		pass string
		want []string // nil wants the frozen 403
	}{
		{"admin filters to the full list", adminUser, adminPass,
			[]string{"t-grp", "t-in", "t-out", "t-part", "t-u8r"}},
		{"readonly_admin filters to the full list", "roat", "roat-pw",
			[]string{"t-grp", "t-in", "t-out", "t-part", "t-u8r"}},
		{"direct m-holder sees its editable set", "u9h", "u9h-pw", []string{"t-in"}},
		{"group m-holder sees the group's target", "u1h", "u1h-pw", []string{"t-grp"}},
		{"a holder of a partial overlap sees only its own", "partm", "partm-pw", []string{"t-part"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := t254Get(t, h, "?filter=manage", tc.user, tc.pass, http.StatusOK)
			got := t254Names(t, body)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("filtered names = %v, want %v", got, tc.want)
			}
		})
	}
	// The no-coverage principals (u8h read-only, nog grantless) answer the
	// 403 arm — asserted byte for byte just below.

	// The empty-coverage arms: the SAME 403 bytes the frozen route gate
	// answers, so a principal without the manage bit cannot distinguish
	// the branches (zero new distinguishability).
	for _, user := range []string{"u8h", "nog"} {
		filtered := t254Get(t, h, "?filter=manage", user, user+"-pw", http.StatusForbidden)
		if filtered != t254FrozenForbidden {
			t.Fatalf("%s: filtered 403 body = %q, want the frozen route body %q", user, filtered, t254FrozenForbidden)
		}
		plain := t254Get(t, h, "", user, user+"-pw", http.StatusForbidden)
		if plain != filtered {
			t.Fatalf("%s: no-filter 403 (%q) and filtered 403 (%q) differ", user, plain, filtered)
		}
	}

	// The security readers' filtered response is BYTE-EQUAL to their
	// no-filter response (14.1.6: 与无 filter 响应等价).
	for _, tc := range []struct{ user, pass string }{
		{adminUser, adminPass}, {"roat", "roat-pw"},
	} {
		plain := t254Get(t, h, "", tc.user, tc.pass, http.StatusOK)
		filtered := t254Get(t, h, "?filter=manage", tc.user, tc.pass, http.StatusOK)
		if plain != filtered {
			t.Fatalf("%s: filtered body differs from the no-filter body\n--- no-filter ---\n%s\n--- filter=manage ---\n%s",
				tc.user, plain, filtered)
		}
	}

	// Anonymous keeps the route's 401 challenge on both branches.
	for _, query := range []string{"", "?filter=manage"} {
		resp := h.do(http.MethodGet, "/binflow/api/v1/permissions"+query, "", "", nil, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %q = %d, want 401", query, resp.StatusCode)
		}
		if chal := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(chal, `Basic realm="`) {
			t.Fatalf("anonymous %q WWW-Authenticate = %q, want the Basic challenge", query, chal)
		}
	}
}

// TestT254NoFilterFrozenBytes: the no-filter branch's byte-level snapshot
// (architecture 14.5-2's golden). Any field, ordering or formatting drift
// of the frozen face reds this test; additive change rides the filter arm.
func TestT254NoFilterFrozenBytes(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t254Setup(t, h)

	resp := h.do(http.MethodGet, "/binflow/api/v1/permissions", adminUser, adminPass, nil, nil)
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("no-filter admin = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	want := strings.Join([]string{
		"[",
		`  {`,
		`    "name": "t-grp",`,
		`    "repos": [`,
		`      "t254-r04",`,
		`      "t254-r05"`,
		`    ],`,
		`    "includePatterns": [`,
		`      "**"`,
		`    ],`,
		`    "excludePatterns": [],`,
		`    "principals": {`,
		`      "users": {},`,
		`      "groups": {`,
		`        "m9-g01h": [`,
		`          "read",`,
		`          "deploy-cache",`,
		`          "delete",`,
		`          "manage"`,
		`        ]`,
		`      }`,
		`    }`,
		`  },`,
		`  {`,
		`    "name": "t-in",`,
		`    "repos": [`,
		`      "t254-r00",`,
		`      "t254-r01"`,
		`    ],`,
		`    "includePatterns": [`,
		`      "**"`,
		`    ],`,
		`    "excludePatterns": [],`,
		`    "principals": {`,
		`      "users": {`,
		`        "u9h": [`,
		`          "manage"`,
		`        ]`,
		`      },`,
		`      "groups": {}`,
		`    }`,
		`  },`,
		`  {`,
		`    "name": "t-out",`,
		`    "repos": [`,
		`      "t254-r02",`,
		`      "t254-r03"`,
		`    ],`,
		`    "includePatterns": [`,
		`      "**"`,
		`    ],`,
		`    "excludePatterns": [],`,
		`    "principals": {`,
		`      "users": {`,
		`        "u9h": [`,
		`          "read"`,
		`        ]`,
		`      },`,
		`      "groups": {}`,
		`    }`,
		`  },`,
		`  {`,
		`    "name": "t-part",`,
		`    "repos": [`,
		`      "t254-r00",`,
		`      "t254-r02"`,
		`    ],`,
		`    "includePatterns": [`,
		`      "**"`,
		`    ],`,
		`    "excludePatterns": [],`,
		`    "principals": {`,
		`      "users": {`,
		`        "partm": [`,
		`          "manage"`,
		`        ]`,
		`      },`,
		`      "groups": {}`,
		`    }`,
		`  },`,
		`  {`,
		`    "name": "t-u8r",`,
		`    "repos": [`,
		`      "t254-r02",`,
		`      "t254-r03"`,
		`    ],`,
		`    "includePatterns": [`,
		`      "**"`,
		`    ],`,
		`    "excludePatterns": [],`,
		`    "principals": {`,
		`      "users": {`,
		`        "u8h": [`,
		`          "read"`,
		`        ]`,
		`      },`,
		`      "groups": {}`,
		`    }`,
		`  }`,
		`]`,
	}, "\n")
	if body != want {
		t.Fatalf("no-filter body drifted from the frozen snapshot:\n--- got ---\n%s\n--- want ---\n%s", body, want)
	}
}

// TestT254FilterManageIsolation: seed-m9's u9 leg at fixture scale — t-in
// listed with COMPLETE fields (the editor hydration face), every
// out-of-coverage target's name, repository, principal and group absent
// from the raw body (the negative grep, NFR-S49).
func TestT254FilterManageIsolation(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t254Setup(t, h)

	body := t254Get(t, h, "?filter=manage", "u9h", "u9h-pw", http.StatusOK)

	var rows []struct {
		Name            string   `json:"name"`
		Repos           []string `json:"repos"`
		IncludePatterns []string `json:"includePatterns"`
		ExcludePatterns []string `json:"excludePatterns"`
		Principals      struct {
			Users  map[string][]string `json:"users"`
			Groups map[string][]string `json:"groups"`
		} `json:"principals"`
	}
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("decode: %v (body %s)", err, body)
	}
	if len(rows) != 1 || rows[0].Name != "t-in" {
		t.Fatalf("rows = %v, want exactly t-in", rows)
	}
	row := rows[0]
	if strings.Join(row.Repos, ",") != "t254-r00,t254-r01" {
		t.Errorf("t-in repos = %v", row.Repos)
	}
	if strings.Join(row.IncludePatterns, ",") != "**" || row.ExcludePatterns == nil || len(row.ExcludePatterns) != 0 {
		t.Errorf("t-in patterns = %v / %v", row.IncludePatterns, row.ExcludePatterns)
	}
	if actions := row.Principals.Users["u9h"]; strings.Join(actions, ",") != "manage" {
		t.Errorf("t-in u9h actions = %v, want [manage]", actions)
	}
	if row.Principals.Groups == nil || len(row.Principals.Groups) != 0 {
		t.Errorf("t-in groups = %v, want {}", row.Principals.Groups)
	}

	// The negative grep: nothing outside the coverage appears ANYWHERE —
	// not the other targets' names, not their repositories, not their
	// principals, not the holder's own read grant.
	for _, leak := range []string{
		`"t-out"`, `"t-grp"`, `"t-part"`, `"t-u8r"`,
		`"t254-r02"`, `"t254-r03"`, `"t254-r04"`, `"t254-r05"`,
		`"u8h"`, `"u1h"`, `"partm"`, `"m9-g01h"`, `"read"`,
	} {
		if strings.Contains(body, leak) {
			t.Errorf("u9h's filtered body leaks %s: %s", leak, body)
		}
	}
}

// TestT254FilterManageParamEdges: the parameter conventions along T-253's
// ?include — empty ask tolerated as no ask, whitespace trimmed, unknown
// values explicitly rejected with the E-01 envelope, repeated parameters
// all validated.
func TestT254FilterManageParamEdges(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t254Setup(t, h)

	t.Run("empty value is no ask at all", func(t *testing.T) {
		// The frozen route serves it: admin gets the full list byte-equal
		// to the no-filter body, the m-holder the frozen 403.
		plain := t254Get(t, h, "", adminUser, adminPass, http.StatusOK)
		if got := t254Get(t, h, "?filter=", adminUser, adminPass, http.StatusOK); got != plain {
			t.Fatalf("?filter= admin body differs from the no-filter body")
		}
		if got := t254Get(t, h, "?filter=", "u9h", "u9h-pw", http.StatusForbidden); got != t254FrozenForbidden {
			t.Fatalf("?filter= u9h body = %q, want the frozen 403", got)
		}
	})
	t.Run("whitespace ask trims to manage", func(t *testing.T) {
		if got := t254Names(t, t254Get(t, h, "?filter=%20%20manage%20", "u9h", "u9h-pw", http.StatusOK)); strings.Join(got, ",") != "t-in" {
			t.Fatalf("padded manage = %v, want [t-in]", got)
		}
	})
	t.Run("unknown values are the explicit 400", func(t *testing.T) {
		for _, bad := range []string{"?filter=bogus", "?filter=manage&filter=bogus", "?filter=MANAGE", "?filter=read"} {
			body := t254Get(t, h, bad, adminUser, adminPass, http.StatusBadRequest)
			if !strings.Contains(body, `"errors"`) {
				t.Fatalf("%s body is not the errors[] envelope: %s", bad, body)
			}
			if !strings.Contains(body, "unknown filter value") {
				t.Fatalf("%s body lacks the unknown-value wording: %s", bad, body)
			}
		}
	})
	t.Run("the 400 precedes the data plane for the m-holder too", func(t *testing.T) {
		// Validation runs before any coverage evaluation: a bogus filter
		// answers 400 even for a principal that would otherwise 403 —
		// but the body names no target, so nothing leaks.
		body := t254Get(t, h, "?filter=bogus", "nog", "nog-pw", http.StatusBadRequest)
		if strings.Contains(body, "t-in") {
			t.Fatalf("400 body leaked a target name: %s", body)
		}
	})
}

// t254CountingPerms counts the permission-plane reads — the N+1 guard.
type t254CountingPerms struct {
	metadata.PermissionStore
	lists, allRows, forRepo, gets int
}

func (p *t254CountingPerms) ListTargets(ctx context.Context) ([]*metadata.PermissionTarget, error) {
	p.lists++
	return p.PermissionStore.ListTargets(ctx)
}

func (p *t254CountingPerms) Principals(ctx context.Context) ([]*metadata.PermissionPrincipal, error) {
	p.allRows++
	return p.PermissionStore.Principals(ctx)
}

func (p *t254CountingPerms) PrincipalsFor(ctx context.Context, repoKey string) ([]*metadata.PermissionPrincipal, error) {
	p.forRepo++
	return p.PermissionStore.PrincipalsFor(ctx, repoKey)
}

func (p *t254CountingPerms) GetTarget(ctx context.Context, name string) (*metadata.PermissionTarget, []*metadata.PermissionPrincipal, error) {
	p.gets++
	return p.PermissionStore.GetTarget(ctx, name)
}

type t254CountingStore struct {
	metadata.Store
	perms *t254CountingPerms
}

func (s *t254CountingStore) Permissions() metadata.PermissionStore { return s.perms }

// TestT254FilterManageSingleTrip: the N+1 gate — one filtered GET is a
// CONSTANT number of permission-plane reads whatever the target count:
// the m-holder arm walks ListTargets+Principals once for the coverage
// (auth.ManageCoverage) and once for the rendering — four fixed reads,
// never a per-repo PrincipalsFor and never a per-target GetTarget; the
// admin arm is rendering alone; the frozen no-filter walk keeps its
// historical GetTarget-per-target shape (documented contrast, not a
// regression).
func TestT254FilterManageSingleTrip(t *testing.T) {
	var perms *t254CountingPerms
	h := newHarnessAuth(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil,
		func(md metadata.Store) metadata.Store {
			perms = &t254CountingPerms{PermissionStore: md.Permissions()}
			return &t254CountingStore{Store: md, perms: perms}
		}, nil)
	t254Setup(t, h)

	// Baseline after seeding: only reads issued BY the guarded requests
	// count.
	lists, allRows, forRepo, gets := perms.lists, perms.allRows, perms.forRepo, perms.gets

	// Holder arm over five targets...
	t254Get(t, h, "?filter=manage", "u9h", "u9h-pw", http.StatusOK)
	holder := []int{perms.lists - lists, perms.allRows - allRows, perms.forRepo - forRepo, perms.gets - gets}
	// ...and the K-independence proof: adding targets must not move the
	// counts.
	for i := 0; i < 5; i++ {
		t215Admin(t, h, http.MethodPost, "api/v1/permissions",
			fmt.Sprintf(`{"name":"t254-x%d","repos":["t254-r0%d"],"includePatterns":["**"],"excludePatterns":[],"principals":{"users":{"u9h":["manage"]},"groups":{}}}`, i, i), 201)
	}
	lists, allRows, forRepo, gets = perms.lists, perms.allRows, perms.forRepo, perms.gets
	t254Get(t, h, "?filter=manage", "u9h", "u9h-pw", http.StatusOK)
	grown := []int{perms.lists - lists, perms.allRows - allRows, perms.forRepo - forRepo, perms.gets - gets}

	for _, tc := range []struct {
		name                                  string
		got                                   []int
		wantLists, wantRows, wantFor, wantGet int
	}{
		{"holder arm, 5 targets", holder, 2, 2, 0, 0},
		{"holder arm, 10 targets (K-independence)", grown, 2, 2, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := []int{tc.wantLists, tc.wantRows, tc.wantFor, tc.wantGet}
			for j, label := range []string{"ListTargets", "Principals", "PrincipalsFor", "GetTarget"} {
				if tc.got[j] != want[j] {
					t.Errorf("%s calls = %d, want %d (%v)", label, tc.got[j], want[j], tc.got)
				}
			}
		})
	}

	// Admin arm: rendering alone (no coverage question).
	lists, allRows, forRepo, gets = perms.lists, perms.allRows, perms.forRepo, perms.gets
	t254Get(t, h, "?filter=manage", adminUser, adminPass, http.StatusOK)
	if got := []int{perms.lists - lists, perms.allRows - allRows, perms.forRepo - forRepo, perms.gets - gets}; got[0] != 1 || got[1] != 1 || got[2] != 0 || got[3] != 0 {
		t.Errorf("admin arm reads = %v, want [1 1 0 0]", got)
	}

	// The frozen no-filter walk keeps its per-target GetTarget shape.
	lists, allRows, forRepo, gets = perms.lists, perms.allRows, perms.forRepo, perms.gets
	t254Get(t, h, "", adminUser, adminPass, http.StatusOK)
	if got := []int{perms.lists - lists, perms.allRows - allRows, perms.forRepo - forRepo, perms.gets - gets}; got[0] != 1 || got[1] != 0 || got[2] != 0 || got[3] != 10 {
		t.Errorf("no-filter reads = %v, want the frozen [1 0 0 10] (one GetTarget per target)", got)
	}
}

// TestT254WriteArmCoverageSingleEvaluation: the family-4 write arms ride
// the same E9 seam since this ticket — one POST over a three-repository
// body evaluates the coverage ONCE (no per-repository PrincipalsFor walk,
// the pre-T-254 shape) and the deny keeps its exact wording.
func TestT254WriteArmCoverageSingleEvaluation(t *testing.T) {
	var perms *t254CountingPerms
	h := newHarnessAuth(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil,
		func(md metadata.Store) metadata.Store {
			perms = &t254CountingPerms{PermissionStore: md.Permissions()}
			return &t254CountingStore{Store: md, perms: perms}
		}, nil)
	// u9h holds m on three repositories through one target.
	for _, key := range []string{"t254-r00", "t254-r01", "t254-r02"} {
		t215Admin(t, h, http.MethodPut, "api/repositories/"+key, `{"rclass":"local","packageType":"generic"}`, 200)
	}
	t215Admin(t, h, http.MethodPut, "api/security/users/u9h",
		`{"name":"u9h","email":"u9h@t.io","password":"u9h-pw","admin":false}`, 201)
	t215Admin(t, h, http.MethodPost, "api/v1/permissions",
		`{"name":"t254-cover","repos":["t254-r00","t254-r01","t254-r02"],"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{"u9h":["manage"]},"groups":{}}}`, 201)

	lists, allRows, forRepo := perms.lists, perms.allRows, perms.forRepo
	resp := h.do(http.MethodPost, "/binflow/api/v1/permissions", "u9h", "u9h-pw", []byte(
		`{"name":"t254-mine","repos":["t254-r00","t254-r01","t254-r02"],"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{"u9h":["manage"]},"groups":{}}}`), nil)
	body := mustGet(t, resp)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("covered POST = %d, want 201 (body %s)", resp.StatusCode, body)
	}
	if got := perms.forRepo - forRepo; got != 0 {
		t.Errorf("covered POST issued %d PrincipalsFor calls, want 0 (the per-repo walk is gone)", got)
	}
	if got := perms.allRows - allRows; got != 1 {
		t.Errorf("covered POST issued %d Principals calls, want exactly 1 (one coverage evaluation)", got)
	}
	if got := perms.lists - lists; got > 2 {
		t.Errorf("covered POST issued %d ListTargets calls, want <= 2", got)
	}

	// The uncovered deny keeps the family-4 wording verbatim (t217's arm).
	resp = h.do(http.MethodPost, "/binflow/api/v1/permissions", "u9h", "u9h-pw", []byte(
		`{"name":"t254-notmine","repos":["t254-r00","t254-r99"],"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{},"groups":{}}}`), nil)
	// t254-r99 does not exist; the coverage deny must precede the unknown
	// repository validation (the pre-parsing posture).
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("uncovered POST = %d, want 403", resp.StatusCode)
	}
	if got := mustGet(t, resp); !strings.Contains(got, "manage coverage") {
		t.Fatalf("uncovered POST body = %q, want the family-4 coverage wording", got)
	}
	_ = resp.Body.Close()
}
