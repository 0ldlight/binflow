// T-L007-1 (known-divergence#rest/permissions-v1-response-shape-residuals,
// D04-R18): the classic /api/security/permissions face's residual rendering
// arms, closed against the live reference (:8082, 7.161.20, wire-verified
// 2026-09-12):
//
//   - DELETE {name} unknown -> 404 errors-envelope "Not Found" (arm 2's
//     envelope family extends to the delete verb — probed);
//   - GET {name} unknown    -> 404 errors-envelope "Not Found" (arm 2);
//   - PUT with an admin-privileged user principal -> 400, the reference's
//     wording verbatim including its doubled quote mark (arm 3), with the
//     probed precedence: the admin scan beats the unknown-user scan, the
//     repository validation beats both;
//   - PUT with an unknown group -> the reference's group wording (a fresh
//     L007 finding beyond the ledger's three arms, probed verbatim);
//   - the rich /api/v1/permissions face stays FROZEN: 204 delete, plain
//     404 — the residual closure is scoped to the classic face.

package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// v1ErrEnvelope decodes one errors-envelope body and returns its single
// entry's status/message pair (the reference's generic error shape).
func v1ErrEnvelope(t *testing.T, raw string) (float64, string) {
	t.Helper()
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode error envelope %q: %v", raw, err)
	}
	if len(env.Errors) != 1 {
		t.Fatalf("error envelope %q carries %d entries, want exactly one", raw, len(env.Errors))
	}
	return float64(env.Errors[0].Status), env.Errors[0].Message
}

// TestPermissionsV1UnknownNameEnvelope (arm 2): both read-and-delete unknown
// names answer the reference's errors envelope carrying the generic
// "Not Found" — status code inside the body mirrors the HTTP status.
func TestPermissionsV1UnknownNameEnvelope(t *testing.T) {
	h := newHarness(t)

	for _, tc := range []struct {
		verb   string
		method string
	}{
		{"detail", http.MethodGet},
		{"delete", http.MethodDelete},
	} {
		resp := t215As(t, h, tc.method, "api/security/permissions/l007-ghost", adminUser, adminPass, "")
		raw := readAllT444(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s unknown name = %d body=%s, want 404", tc.verb, resp.StatusCode, raw)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("%s unknown name content-type = %q, want application/json (envelope)", tc.verb, got)
		}
		status, msg := v1ErrEnvelope(t, raw)
		if status != http.StatusNotFound || msg != "Not Found" {
			t.Errorf("%s unknown name envelope = (%0.f, %q), want (404, Not Found)", tc.verb, status, msg)
		}
	}
}

// TestPermissionsV1AdminSubjectRefusal (arm 3): an admin-privileged user
// principal on the keyed PUT answers the reference's 400 verbatim — the
// doubled quote mark is the reference's own. Precedence is probed: the
// admin scan runs before the unknown-user scan, and a non-admin known user
// still lands his grant.
func TestPermissionsV1AdminSubjectRefusal(t *testing.T) {
	h := newHarnessCfg(t, nil, [][2]string{{"l007dev", "l007dev-pw"}})
	seedRepo(t, h, "generic-local")

	// The refusal, verbatim (the double ' is the reference's own typo).
	resp := t215As(t, h, http.MethodPut, "api/security/permissions/l007-adm", adminUser, adminPass,
		`{"repositories":["generic-local"],"principals":{"users":{"admin":["r"]}}}`)
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest ||
		raw != "The user: 'admin'' has admin privileges, and cannot be added to a Permission Target." {
		t.Fatalf("admin principal = %d body=%s, want the reference's 400 verbatim", resp.StatusCode, raw)
	}

	// Precedence leg 1: an admin beside an UNKNOWN user still answers the
	// admin message (the admin scan beats the unknown-user scan).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l007-adm", adminUser, adminPass,
		`{"repositories":["generic-local"],"principals":{"users":{"admin":["r"],"l007-ghost-user":["r"]}}}`)
	raw = readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "has admin privileges") {
		t.Fatalf("admin+unknown = %d body=%s, want the admin refusal", resp.StatusCode, raw)
	}

	// Precedence leg 2: an unknown repository beside an admin still answers
	// the repository message (the repository scan beats both).
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l007-adm", adminUser, adminPass,
		`{"repositories":["l007-ghost-repo"],"principals":{"users":{"admin":["r"]}}}`)
	raw = readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(raw, "non-existing repository") {
		t.Fatalf("unknown-repo+admin = %d body=%s, want the repository 400", resp.StatusCode, raw)
	}

	// A known NON-admin user lands his grant — the refusal is the admin
	// seat, not the keyed face.
	resp = t215As(t, h, http.MethodPut, "api/security/permissions/l007-ok", adminUser, adminPass,
		`{"repositories":["generic-local"],"principals":{"users":{"l007dev":["r"]}}}`)
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusCreated {
		t.Fatalf("non-admin principal = %d body=%s, want 201", code, raw)
	}
}

// TestPermissionsV1UnknownGroupWording (fresh L007 finding): the keyed
// face's unknown-group 400 now carries the reference's own wording — no
// colon before the quoted name, unlike the user arm.
func TestPermissionsV1UnknownGroupWording(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")

	resp := t215As(t, h, http.MethodPut, "api/security/permissions/l007-grp", adminUser, adminPass,
		`{"repositories":["generic-local"],"principals":{"groups":{"l007-ghost-group":["r"]}}}`)
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest ||
		raw != "Permission target contains a reference to a non-existing group 'l007-ghost-group'." {
		t.Fatalf("unknown group = %d body=%s, want the reference wording verbatim", resp.StatusCode, raw)
	}

	// The rich face keeps its frozen group wording.
	resp = t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass,
		`{"name":"l007-rich-grp","repos":["generic-local"],"principals":{"groups":{"l007-ghost-group":["read"]}}}`)
	raw = readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest ||
		raw != "Unable to find group by name 'l007-ghost-group'." {
		t.Fatalf("rich-face unknown group = %d body=%s, want the frozen rich wording", resp.StatusCode, raw)
	}
}

// TestPermissionsRichFaceDeleteFrozen: the residual closure is scoped to
// the classic face — the rich /api/v1/permissions delete keeps its frozen
// 204 and plain 404.
func TestPermissionsRichFaceDeleteFrozen(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "generic-local")
	putTargetWire(t, h, "l007-rich-del", `{"users":{"admin":["read"]}}`, http.StatusCreated)

	resp := t215As(t, h, http.MethodDelete, "api/v1/permissions/l007-rich-del", adminUser, adminPass, "")
	if raw, code := readAllT444(t, resp), resp.StatusCode; code != http.StatusNoContent {
		t.Fatalf("rich delete = %d body=%s, want the frozen 204", code, raw)
	}
	resp = t215As(t, h, http.MethodDelete, "api/v1/permissions/l007-rich-del", adminUser, adminPass, "")
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusNotFound || raw != "permission target not found: l007-rich-del" {
		t.Fatalf("rich delete unknown = %d body=%s, want the frozen plain 404", resp.StatusCode, raw)
	}
}
