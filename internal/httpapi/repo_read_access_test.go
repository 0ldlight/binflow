// L026-7: the repo READ faces decouple from the permission model (the
// L026-5 spec ruling, rest/m-holder-repo-read-faces — wire reports/
// compatibility/l026r-wire/a-holder-*/a-noperm-*): any authenticated
// non-admin — a manage-only holder and a zero-permission user alike, in
// and out of their coverage — collects the SAME shapes:
//
//	v1 single repo   200, four keys {key, rclass, packageType, description}
//	v2 single repo   200, four keys {key, type, packageType, description}
//	v1 list          200, entries byte-identical to admin
//	configurations   403, the bare Forbidden errors envelope
//
// The artifact download/write permission gates are untouched (they live on
// the content plane); this file pins only the three read faces plus the
// configurations face's unchanged 403.

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// l027Setup provisions the wire-probe fixtures: two repositories (one
// inside the holder's manage coverage, one outside), a manage-only holder
// (target grants m on the covered repo, nothing else) and a user with no
// permission rows at all — the two A-side probe personas.
func l027Setup(t *testing.T, h *harness) (holdKey, outKey string) {
	t.Helper()
	holdKey, outKey = "l027-hold", "l027-out"
	for _, k := range []string{holdKey, outKey} {
		t215Admin(t, h, http.MethodPut, "api/repositories/"+k,
			`{"rclass":"local","packageType":"generic"}`, 200)
	}
	// holder: manage ONLY on holdKey (the m letter rides the real target
	// wire, exactly like the reference probe's l026r-pt fixture).
	t217PutTarget(t, h, adminUser, adminPass, "l027-pt", `["`+holdKey+`"]`,
		`{"groups":{},"users":{"holder":["manage"]}}`, 201)
	return holdKey, outKey
}

// TestRepoReadFacesOpenToEveryAuthenticatedUser: the three read faces x
// two probe personas (manage holder, zero-permission user), each against
// the covered and the uncovered repository — one shape everywhere.
func TestRepoReadFacesOpenToEveryAuthenticatedUser(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{
		{"holder", "holder-pw"},
		{"noperm", "noperm-pw"},
	})
	holdKey, outKey := l027Setup(t, h)

	personas := []struct{ label, user, pass string }{
		{"manage holder", "holder", "holder-pw"},
		{"zero-permission user", "noperm", "noperm-pw"},
	}
	single := []struct{ label, key string }{
		{"inside coverage", holdKey},
		{"outside coverage", outKey},
	}

	for _, p := range personas {
		for _, s := range single {
			t.Run(p.label+" v1 detail "+s.label, func(t *testing.T) {
				m := l027GetMap(t, h, "/binflow/api/repositories/"+s.key, p.user, p.pass)
				assertKeySetExact(t, "v1", m, "key rclass packageType description")
				if m["key"] != s.key || m["rclass"] != "local" || m["packageType"] != "generic" {
					t.Errorf("v1 projection values = %v, want key=%s rclass=local packageType=generic", m, s.key)
				}
			})
			t.Run(p.label+" v2 detail "+s.label, func(t *testing.T) {
				m := l027GetMap(t, h, "/binflow/api/v2/repositories/"+s.key, p.user, p.pass)
				assertKeySetExact(t, "v2", m, "key type packageType description")
				if m["key"] != s.key || m["type"] != "local" || m["packageType"] != "generic" {
					t.Errorf("v2 projection values = %v, want key=%s type=local packageType=generic", m, s.key)
				}
			})
		}

		t.Run(p.label+" v1 list is the admin body verbatim", func(t *testing.T) {
			admin := l027Get(t, h, "/binflow/api/repositories", adminUser, adminPass)
			got := l027Get(t, h, "/binflow/api/repositories", p.user, p.pass)
			if got != admin {
				t.Errorf("%s list body diverges from admin\nadmin: %s\ngot:   %s", p.label, admin, got)
			}
			if !json.Valid([]byte(got)) || got[0] != '[' {
				t.Errorf("%s list body is not a JSON array: %s", p.label, got)
			}
		})

		t.Run(p.label+" configurations stays 403 Forbidden", func(t *testing.T) {
			resp := h.do(http.MethodGet, "/binflow/api/repositories/configurations", p.user, p.pass, nil, nil)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("configurations status = %d, want 403 (body: %s)", resp.StatusCode, mustGet(t, resp))
			}
			var env struct {
				Errors []struct {
					Status  int    `json:"status"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal([]byte(mustGet(t, resp)), &env); err != nil {
				t.Fatalf("configurations body is not the errors envelope: %v", err)
			}
			if len(env.Errors) != 1 || env.Errors[0].Status != 403 || env.Errors[0].Message != "Forbidden" {
				t.Errorf("configurations envelope = %+v, want one {403 Forbidden}", env.Errors)
			}
		})
	}

	// The write faces keep their gates: the same personas cannot create,
	// update or delete (the read-face opening grants no configuration
	// power — FR-65's boundary, re-pinned here so the read change cannot
	// ride along silently).
	for _, leg := range []struct {
		name, method, path, body string
	}{
		{"create (family 6)", http.MethodPut, "api/repositories/l027-new", `{"rclass":"local","packageType":"generic"}`},
		{"update (family 7 write)", http.MethodPost, "api/repositories/" + outKey, `{"description":"nope"}`},
		{"delete (family 6)", http.MethodDelete, "api/repositories/" + outKey, ""},
	} {
		for _, p := range personas {
			if got := t215Code(t, h, leg.method, leg.path, p.user, p.pass, leg.body); got != http.StatusForbidden {
				t.Errorf("%s %s as %s = %d, want 403 (read faces opened, writes did not)",
					leg.name, leg.path, p.label, got)
			}
		}
	}
}

// l027Get fetches one body as the given principal, demanding 200.
func l027Get(t *testing.T, h *harness, path, user, pass string) string {
	t.Helper()
	resp := h.do(http.MethodGet, path, user, pass, nil, nil)
	defer func() { _ = resp.Body.Close() }()
	body := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s as %s = %d, want 200 (body: %s)", path, user, resp.StatusCode, body)
	}
	return body
}

// l027GetMap is l027Get decoded as one JSON object.
func l027GetMap(t *testing.T, h *harness, path, user, pass string) map[string]any {
	t.Helper()
	return decodeJSONMap(t, l027Get(t, h, path, user, pass))
}
