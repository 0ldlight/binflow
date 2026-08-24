package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// T-253/E1 (ADR-0030 / architecture section 14.1, FR-79.1): the batch usage
// endpoint GET /api/v1/storage/usage over the real stack — bare array (K20),
// server-side visibility filtering with the family-7 OR formula set-shaped
// (invisible repositories absent, zero leakage), the ?repos= point-naming
// and ?include=counts arms, and row-for-row agreement with the single-repo
// endpoint (AC1).

// t253Row decodes one batch row; the counts fields are pointers so their
// PRESENCE is assertable (include=counts must render them even when zero,
// the default shape must not render them at all).
type t253Row struct {
	Repo       string  `json:"repo"`
	UsedBytes  int64   `json:"usedBytes"`
	QuotaBytes int64   `json:"quotaBytes"`
	NodeCount  *int64  `json:"nodeCount"`
	UpdatedAt  *string `json:"updatedAt"`
}

// t253Setup provisions six repositories (r00 carrying a quota and one file),
// the role fixtures (readonly_admin, a partially-granted reader, a manage-only
// holder, a grantless user) and the two permission targets — everything
// through the real wire paths.
func t253Setup(t *testing.T, h *harness) {
	t.Helper()
	t253Admin(t, h, http.MethodPut, "api/repositories/t253-r00",
		`{"rclass":"local","packageType":"generic","quotaBytes":512}`, 200)
	for _, key := range []string{"t253-r01", "t253-r02", "t253-r03", "t253-r04", "t253-r05"} {
		t253Admin(t, h, http.MethodPut, "api/repositories/"+key,
			`{"rclass":"local","packageType":"generic"}`, 200)
	}
	// Non-zero usage leg: one file (the folder sentinel rows the ancestors
	// materialize must not count).
	resp := h.do(http.MethodPut, "/binflow/t253-r00/a/b.bin", adminUser, adminPass, []byte("t253-payload"), nil)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("seed upload: status %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	t253Admin(t, h, http.MethodPut, "api/security/users/roat",
		`{"name":"roat","email":"roat@t.io","password":"roat-pw","adminRole":"readonly_admin"}`, 201)
	t253Admin(t, h, http.MethodPut, "api/security/users/u8t",
		`{"name":"u8t","email":"u8t@t.io","password":"u8t-pw","admin":false}`, 201)
	t253Admin(t, h, http.MethodPut, "api/security/users/mhold",
		`{"name":"mhold","email":"mhold@t.io","password":"mhold-pw","admin":false}`, 201)
	t253Admin(t, h, http.MethodPut, "api/security/users/nogt",
		`{"name":"nogt","email":"nogt@t.io","password":"nogt-pw","admin":false}`, 201)

	// u8t reads three of the six repositories (the partial-visibility
	// fixture); mhold holds the manage bit on r03 ONLY (the OR arm's m half,
	// no read grant anywhere).
	t253Admin(t, h, http.MethodPost, "api/v1/permissions",
		`{"name":"t253-read","repos":["t253-r00","t253-r01","t253-r02"],`+
			`"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{"u8t":["read"]},"groups":{}}}`, 201)
	t253Admin(t, h, http.MethodPost, "api/v1/permissions",
		`{"name":"t253-m","repos":["t253-r03"],`+
			`"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{"mhold":["manage"]},"groups":{}}}`, 201)
}

// t253Admin issues an admin request demanding the status, returning the body.
func t253Admin(t *testing.T, h *harness, method, path, body string, want int) string {
	t.Helper()
	resp := h.do(method, "/binflow/"+path, adminUser, adminPass, []byte(body), nil)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	if resp.StatusCode != want {
		t.Fatalf("%s %s (admin) = %d, want %d (body %s)", method, path, resp.StatusCode, want, got)
	}
	return string(got)
}

// t253Get issues a batch GET as one principal, demanding the status and
// returning the raw body.
func t253Get(t *testing.T, h *harness, query, user, pass string, want int) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/v1/storage/usage"+query, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read usage batch body: %v", err)
	}
	if resp.StatusCode != want {
		t.Fatalf("GET usage%s as %s = %d, want %d (body %s)", query, user, resp.StatusCode, want, got)
	}
	return string(got)
}

// t253Decode parses the bare-array body.
func t253Decode(t *testing.T, body string) []t253Row {
	t.Helper()
	var rows []t253Row
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		t.Fatalf("usage batch body is not a JSON array: %v (body %s)", err, body)
	}
	return rows
}

// t253Keys projects the row order (asserting it while at it).
func t253Keys(t *testing.T, rows []t253Row) []string {
	t.Helper()
	keys := make([]string, len(rows))
	for i, r := range rows {
		keys[i] = r.Repo
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("rows not ordered by repo key: %v", keys)
		}
	}
	return keys
}

// TestT253UsageBatchRoleMatrix walks the three roles plus the two filtered
// faces and the anonymous boundary — every authenticated caller receives a
// 200 filtered view (the empty visibility set is [], never 403), and an
// invisible repository's key appears nowhere in the body (AC2's negative
// grep at the smallest scale).
func TestT253UsageBatchRoleMatrix(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t253Setup(t, h)

	cases := []struct {
		name     string
		user     string
		pass     string
		wantKeys []string
	}{
		{"admin sees every repository", adminUser, adminPass,
			[]string{"t253-r00", "t253-r01", "t253-r02", "t253-r03", "t253-r04", "t253-r05"}},
		{"readonly_admin sees the full read face", "roat", "roat-pw",
			[]string{"t253-r00", "t253-r01", "t253-r02", "t253-r03", "t253-r04", "t253-r05"}},
		{"partially-granted reader sees its subset", "u8t", "u8t-pw",
			[]string{"t253-r00", "t253-r01", "t253-r02"}},
		{"manage-only holder rides the OR arm", "mhold", "mhold-pw",
			[]string{"t253-r03"}},
		{"grantless user gets the empty view", "nogt", "nogt-pw", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := t253Get(t, h, "", tc.user, tc.pass, http.StatusOK)
			if !strings.HasPrefix(strings.TrimSpace(body), "[") {
				t.Fatalf("body is not a bare array: %s", body)
			}
			rows := t253Decode(t, body)
			got := t253Keys(t, rows)
			if len(got) != len(tc.wantKeys) {
				t.Fatalf("rows = %v, want %v", got, tc.wantKeys)
			}
			for i := range got {
				if got[i] != tc.wantKeys[i] {
					t.Fatalf("rows = %v, want %v", got, tc.wantKeys)
				}
			}
			// The invisible keys appear nowhere in the raw body — not as
			// rows, not in any corner of the payload.
			visible := map[string]bool{}
			for _, k := range tc.wantKeys {
				visible[k] = true
			}
			for _, key := range []string{"t253-r00", "t253-r01", "t253-r02", "t253-r03", "t253-r04", "t253-r05"} {
				if !visible[key] && strings.Contains(body, `"`+key+`"`) {
					t.Errorf("invisible repository %q leaked into the body", key)
				}
			}
		})
	}

	// The empty view renders the empty ARRAY, not null.
	if body := t253Get(t, h, "", "nogt", "nogt-pw", http.StatusOK); strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty visibility set body = %q, want exactly []", body)
	}

	// Anonymous keeps the route's 401 challenge (the single-repo posture).
	resp := h.do(http.MethodGet, "/binflow/api/v1/storage/usage", "", "", nil, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous batch = %d, want 401", resp.StatusCode)
	}
	if chal := resp.Header.Get("WWW-Authenticate"); !strings.HasPrefix(chal, `Basic realm="`) {
		t.Fatalf("WWW-Authenticate = %q, want the Basic challenge", chal)
	}
}

// TestT253UsageBatchRowShape: admin's rows carry the metered total and the
// quota (local-only, 0 for the rest), and each row equals the single-repo
// endpoint's body field for field (AC1's wire-side half).
func TestT253UsageBatchRowShape(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t253Setup(t, h)

	rows := t253Decode(t, t253Get(t, h, "", adminUser, adminPass, http.StatusOK))
	if len(rows) != 6 {
		t.Fatalf("admin rows = %d, want 6", len(rows))
	}
	byKey := map[string]t253Row{}
	for _, r := range rows {
		byKey[r.Repo] = r
	}
	if r := byKey["t253-r00"]; r.UsedBytes != int64(len("t253-payload")) || r.QuotaBytes != 512 {
		t.Errorf("r00 = %+v, want usedBytes %d quotaBytes 512", r, len("t253-payload"))
	}
	if r := byKey["t253-r04"]; r.UsedBytes != 0 || r.QuotaBytes != 0 {
		t.Errorf("r04 = %+v, want all-zero (untouched, no quota)", r)
	}

	// Row-for-row agreement with the single-repo endpoint, admin side AND
	// the filtered reader side (both must serve the same numbers).
	for _, tc := range []struct{ user, pass string }{
		{adminUser, adminPass}, {"u8t", "u8t-pw"},
	} {
		for _, r := range t253Decode(t, t253Get(t, h, "", tc.user, tc.pass, http.StatusOK)) {
			resp := h.do(http.MethodGet, "/binflow/api/v1/storage/usage/"+r.Repo, tc.user, tc.pass, nil, nil)
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusOK {
				t.Fatalf("single usage %s as %s: status %d (%s)", r.Repo, tc.user, resp.StatusCode, err)
			}
			var single usageBodyT95
			if err := json.Unmarshal(body, &single); err != nil {
				t.Fatalf("single usage %s body: %v", r.Repo, err)
			}
			if single.Repo != r.Repo || single.UsedBytes != r.UsedBytes || single.QuotaBytes != r.QuotaBytes {
				t.Errorf("%s: batch %+v vs single %+v disagree", r.Repo, r, single)
			}
		}
	}
}

// TestT253UsageBatchReposParam walks the ?repos= point-naming arm: subsets,
// duplicates, whitespace, unknown keys (silently absent — no error, no
// existence signal) and the present-but-empty spelling.
func TestT253UsageBatchReposParam(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t253Setup(t, h)

	t.Run("point-named subset", func(t *testing.T) {
		got := t253Keys(t, t253Decode(t, t253Get(t, h, "?repos=t253-r01,t253-r03", adminUser, adminPass, http.StatusOK)))
		want := []string{"t253-r01", "t253-r03"}
		if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	})
	t.Run("unknown key is silently absent", func(t *testing.T) {
		got := t253Keys(t, t253Decode(t, t253Get(t, h, "?repos=t253-r01,no-such-repo", adminUser, adminPass, http.StatusOK)))
		if len(got) != 1 || got[0] != "t253-r01" {
			t.Fatalf("rows = %v, want only t253-r01", got)
		}
	})
	t.Run("invisible key is indistinguishable from unknown", func(t *testing.T) {
		// u8t may read r00..r02; naming the forbidden r03 yields exactly the
		// same shape as naming a repository that does not exist.
		got := t253Keys(t, t253Decode(t, t253Get(t, h, "?repos=t253-r00,t253-r03", "u8t", "u8t-pw", http.StatusOK)))
		if len(got) != 1 || got[0] != "t253-r00" {
			t.Fatalf("rows = %v, want only t253-r00", got)
		}
	})
	t.Run("duplicates and padding collapse", func(t *testing.T) {
		got := t253Keys(t, t253Decode(t, t253Get(t, h,
			"?repos=t253-r01,t253-r01,%20t253-r02%20,", adminUser, adminPass, http.StatusOK)))
		if len(got) != 2 || got[0] != "t253-r01" || got[1] != "t253-r02" {
			t.Fatalf("rows = %v, want [t253-r01 t253-r02]", got)
		}
	})
	t.Run("repeated parameter accumulates", func(t *testing.T) {
		got := t253Keys(t, t253Decode(t, t253Get(t, h,
			"?repos=t253-r01&repos=t253-r04", adminUser, adminPass, http.StatusOK)))
		if len(got) != 2 || got[0] != "t253-r01" || got[1] != "t253-r04" {
			t.Fatalf("rows = %v, want [t253-r01 t253-r04]", got)
		}
	})
	t.Run("present-but-empty value names nothing", func(t *testing.T) {
		if body := t253Get(t, h, "?repos=", adminUser, adminPass, http.StatusOK); strings.TrimSpace(body) != "[]" {
			t.Fatalf("body = %q, want []", body)
		}
	})
}

// TestT253UsageBatchIncludeCounts: the counts arm renders nodeCount (file
// nodes only — the ancestor folder rows of the seeded file must not count)
// and updatedAt on EVERY row including the zero-count ones, while the
// default shape carries neither field; an unknown include value is the
// explicit 400 (the governance family's refuse-don't-ignore convention).
func TestT253UsageBatchIncludeCounts(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t253Setup(t, h)

	withCounts := t253Decode(t, t253Get(t, h, "?include=counts", adminUser, adminPass, http.StatusOK))
	if len(withCounts) != 6 {
		t.Fatalf("rows = %d, want 6", len(withCounts))
	}
	for _, r := range withCounts {
		if r.NodeCount == nil || r.UpdatedAt == nil {
			t.Fatalf("row %s missing counts fields: %+v", r.Repo, r)
		}
		if r.UpdatedAt != nil && *r.UpdatedAt == "" {
			t.Errorf("row %s: updatedAt is empty", r.Repo)
		}
	}
	byKey := map[string]t253Row{}
	for _, r := range withCounts {
		byKey[r.Repo] = r
	}
	if n := byKey["t253-r00"].NodeCount; *n != 1 {
		t.Errorf("r00 nodeCount = %d, want 1 (folder sentinel excluded)", *n)
	}
	if n := byKey["t253-r04"].NodeCount; *n != 0 {
		t.Errorf("r04 nodeCount = %d, want 0 (rendered, not omitted)", *n)
	}

	// Default shape: neither field anywhere in the raw body.
	plain := t253Get(t, h, "", adminUser, adminPass, http.StatusOK)
	if strings.Contains(plain, "nodeCount") || strings.Contains(plain, "updatedAt") {
		t.Fatalf("default shape carries counts fields: %s", plain)
	}
	// The empty include value is tolerated (an empty ask, like every other
	// optional parameter's empty spelling).
	t253Get(t, h, "?include=", adminUser, adminPass, http.StatusOK)

	// Unknown include values are rejected with the E-01 envelope.
	for _, bad := range []string{"?include=bogus", "?include=counts&include=bogus", "?include=COUNTS"} {
		body := t253Get(t, h, bad, adminUser, adminPass, http.StatusBadRequest)
		if !strings.Contains(body, `"errors"`) {
			t.Fatalf("%s body is not the errors[] envelope: %s", bad, body)
		}
	}
}

// TestT253UsageBatchRouteEdges: the new route's verb boundary (only GET has
// a route) and the single-repo endpoint's unchanged behavior beside it.
func TestT253UsageBatchRouteEdges(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)
	t253Setup(t, h)

	resp := h.do(http.MethodPost, "/binflow/api/v1/storage/usage", adminUser, adminPass, []byte("{}"), nil)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST batch = %d, want the unrouted 404 (body %s)", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "not implemented") {
		t.Fatalf("POST batch body lacks the E-26 wording: %s", body)
	}

	// The single-repo endpoint is byte-shape unchanged beside the new route.
	if u := t95Usage(t, h, "t253-r00"); u.UsedBytes != int64(len("t253-payload")) || u.QuotaBytes != 512 {
		t.Fatalf("single-repo endpoint drifted: %+v", u)
	}
	// A trailing slash keeps hitting the per-repo branch's unknown-key 404
	// (the empty segment), never the batch route.
	resp = h.do(http.MethodGet, "/binflow/api/v1/storage/usage/", adminUser, adminPass, nil, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("trailing-slash = %d, want 404", resp.StatusCode)
	}
}

// TestT253UsageBatchFiftyRepos: the fan-out shape at the PRD's fixture scale
// — 50 repositories, one request, and the partially-granted reader's subset
// stays exact at scale (the perf budget itself is recorded against the live
// seeded instance in the ticket log, not asserted here where CI jitter
// would flake it).
func TestT253UsageBatchFiftyRepos(t *testing.T) {
	h := newHarnessCfg(t, func(c *mutatedConfig) { c.Security.AnonymousAccess = false }, nil)

	var keys []string
	for i := 0; i < 50; i++ {
		key := "t253-50r" + string(rune('a'+i/26)) + string(rune('a'+i%26)) // t253-50raa..t253-50rbx
		keys = append(keys, key)
		t253Admin(t, h, http.MethodPut, "api/repositories/"+key,
			`{"rclass":"local","packageType":"generic"}`, 200)
	}
	// u8 reads every other repository: 25 of 50.
	var readRepos []string
	for i := 0; i < 50; i += 2 {
		readRepos = append(readRepos, keys[i])
	}
	t253Admin(t, h, http.MethodPut, "api/security/users/half",
		`{"name":"half","email":"half@t.io","password":"half-pw","admin":false}`, 201)
	t253Admin(t, h, http.MethodPost, "api/v1/permissions",
		`{"name":"t253-half","repos":[`+`"`+strings.Join(readRepos, `","`)+`"],`+
			`"includePatterns":["**"],"excludePatterns":[],`+
			`"principals":{"users":{"half":["read"]},"groups":{}}}`, 201)

	if got := len(t253Decode(t, t253Get(t, h, "", adminUser, adminPass, http.StatusOK))); got != 50 {
		t.Fatalf("admin rows = %d, want 50", got)
	}
	rows := t253Decode(t, t253Get(t, h, "", "half", "half-pw", http.StatusOK))
	if len(rows) != 25 {
		t.Fatalf("half rows = %d, want 25", len(rows))
	}
	for i, r := range rows {
		if r.Repo != readRepos[i] {
			t.Fatalf("half row %d = %s, want %s", i, r.Repo, readRepos[i])
		}
	}
}
