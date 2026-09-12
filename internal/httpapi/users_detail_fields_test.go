// T-L007-1 (D04-R02, matrix evidence: status enum closed set / no enabled
// boolean on the reference / lastLoggedIn conditional / groups empty-set
// shape): the GET /api/security/users/{name} echo field set, aligned with
// the live reference (:8082, 7.161.20, wire-verified 2026-09-12 — admin,
// anonymous and a fresh never-logged-in user probed). The reference's own
// columns join the echo (status enum, mfaStatus, the four addon-role bits,
// lastLoggedInMillis, offlineMode, shouldInvite) while BinFlow's documented
// superset stays (adminRole/enabled/source and the unconditional
// email/groups — the console user editor seeds off d.email/[...d.groups],
// the reference's omit-when-unset renderings would break it).

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// userDetailFields GETs one user detail as a raw map (key PRESENCE, not
// just values, is assertable) and pins the 200.
func userDetailFields(t *testing.T, h *harness, name string) map[string]any {
	t.Helper()
	resp := h.do(http.MethodGet, "/binflow/api/security/users/"+name, adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET users/%s = %d body=%s, want 200", name, resp.StatusCode, raw)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatalf("decode detail %q: %v", raw, err)
	}
	return fields
}

// TestUserDetailFieldSet: table over the account states the matrix row's
// evidence names — every row renders the aligned echo set (reference
// columns + BinFlow superset, no strays), the status enum tracks the
// enabled column, lastLoggedIn is conditional, lastLoggedInMillis always 0
// (the probed reference quirk: 0 even beside a populated lastLoggedIn).
func TestUserDetailFieldSet(t *testing.T) {
	h := newHarness(t)
	disabled := false
	t251CreateUser(t, h, "u-l007-plain", "plain@t.io", "pw-1", nil, nil)
	t251CreateUser(t, h, "u-l007-off", "off@t.io", "pw-2", &disabled, nil)
	t251CreateUser(t, h, "u-l007-logged", "logged@t.io", "pw-3", nil, nil)
	seedLogin(t, h, "u-l007-logged", "2026-09-01T08:09:10Z")

	for _, tc := range []struct {
		name            string
		wantStatus      string
		wantEnabled     bool
		wantLastLogin   string // "" = the key must be ABSENT
		wantAdminRole   string
		checkLastLogged bool
	}{
		{"admin", "ENABLED", true, "", "admin", false},
		{"u-l007-plain", "ENABLED", true, "", "user", false},
		{"u-l007-off", "DISABLED", false, "", "user", false},
		{"u-l007-logged", "ENABLED", true, "2026-09-01T08:09:10Z", "user", true},
	} {
		f := userDetailFields(t, h, tc.name)

		// The aligned echo set: reference columns plus BinFlow's superset,
		// exactly — a stray or missing key fails the row.
		want := map[string]bool{
			"name": true, "email": true, "admin": true,
			"policyViewer": true, "policyManager": true, "watchManager": true, "reportsManager": true,
			"profileUpdatable": true, "internalPasswordDisabled": true,
			"groups": true, "lastLoggedInMillis": true,
			"realm": true, "source": true, "adminRole": true, "enabled": true,
			"offlineMode": true, "disableUIAccess": true, "mfaStatus": true, "status": true,
			"shouldInvite": true,
		}
		if tc.wantLastLogin != "" {
			want["lastLoggedIn"] = true
		}
		got := map[string]bool{}
		for k := range f {
			got[k] = true
		}
		for k := range want {
			if !got[k] {
				t.Errorf("%s: detail key %q missing (got %v)", tc.name, k, got)
			}
		}
		for k := range got {
			if !want[k] {
				t.Errorf("%s: detail key %q is a stray (got %v)", tc.name, k, got)
			}
		}

		if f["status"] != tc.wantStatus {
			t.Errorf("%s: status = %v, want %q (the enabled column's closed set)", tc.name, f["status"], tc.wantStatus)
		}
		if f["enabled"] != tc.wantEnabled {
			t.Errorf("%s: enabled = %v, want %t (the superset boolean stays in lockstep)", tc.name, f["enabled"], tc.wantEnabled)
		}
		if f["adminRole"] != tc.wantAdminRole {
			t.Errorf("%s: adminRole = %v, want %q", tc.name, f["adminRole"], tc.wantAdminRole)
		}
		if got, present := f["lastLoggedIn"]; tc.wantLastLogin == "" {
			if present {
				t.Errorf("%s: lastLoggedIn = %v, want no key (absent)", tc.name, got)
			}
		} else if got != tc.wantLastLogin {
			t.Errorf("%s: lastLoggedIn = %v, want %q", tc.name, got, tc.wantLastLogin)
		}
		// The probed reference constants: millis renders ALWAYS and was 0 on
		// every probed row (even beside a populated lastLoggedIn — the admin
		// row), mfaStatus is the "NONE" enum value.
		if f["lastLoggedInMillis"] != float64(0) {
			t.Errorf("%s: lastLoggedInMillis = %v, want 0 (the probed constant)", tc.name, f["lastLoggedInMillis"])
		}
		if f["mfaStatus"] != "NONE" {
			t.Errorf("%s: mfaStatus = %v, want NONE", tc.name, f["mfaStatus"])
		}
		for _, bit := range []string{"policyViewer", "policyManager", "watchManager", "reportsManager", "offlineMode", "shouldInvite"} {
			if f[bit] != false {
				t.Errorf("%s: %s = %v, want false (no such seat in BinFlow)", tc.name, bit, f[bit])
			}
		}
	}

	// The never-logged-in row's conditional key, pinned raw: u-l007-plain
	// must render NO lastLoggedIn key at all (omitempty, the reference's
	// conditional-presence shape).
	if _, present := userDetailFields(t, h, "u-l007-plain")["lastLoggedIn"]; present {
		t.Error("never-logged-in row carries lastLoggedIn; the reference renders no key")
	}
}

// TestUserDetailFieldSetNegative: the unknown name answers the reference's
// generalized errors envelope (L009-3, ledger rest/users-v1-get-unknown-
// style: "Not Found" — the same wording as an unknown path — not a named
// "User not found").
func TestUserDetailFieldSetNegative(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodGet, "/binflow/api/security/users/l007-ghost", adminUser, adminPass, nil, nil)
	raw := mustGet(t, resp)
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if resp.StatusCode != http.StatusNotFound ||
		resp.Header.Get("Content-Type") != "application/json" ||
		json.Unmarshal([]byte(raw), &env) != nil ||
		len(env.Errors) != 1 || env.Errors[0].Status != 404 || env.Errors[0].Message != "Not Found" {
		t.Fatalf("unknown user = %d body=%s, want the 404 errors envelope \"Not Found\"", resp.StatusCode, raw)
	}
}
