// LOOP 010 L010-2 (ledger rest/permissions-v1-empty-actions-validation-skip):
// the keyed face's empty-actions principal validation exemption, against the
// live-reference probe matrix (:8082, 7.161.20, arms n1-n7 wire-verified
// 2026-09-12): the reference's validator walks only principals whose action
// list is NON-EMPTY — an empty list grants nothing, and its holder skips
// BOTH the admin guard and the unknown-name checks (user and group alike),
// answering 201. Principals bearing actions keep the full L006/L007-1
// validation chain, ordering included (admin scan first among bearers).
package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestPermissionsV1EmptyActionsValidationSkip(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "l0102-loc")

	for _, tc := range []struct {
		name       string
		target     string
		body       string
		wantStatus int
		wantIn     string // substring of the body ("" = none asserted)
	}{
		{
			name:       "unknown user with empty actions passes (arm n1)",
			target:     "l0102-n1",
			body:       `{"repositories":["l0102-loc"],"principals":{"users":{"nosuchuser-l0102":[]}}}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "admin with empty actions passes (arm n2)",
			target:     "l0102-n2",
			body:       `{"repositories":["l0102-loc"],"principals":{"users":{"admin":[]}}}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "unknown group with empty actions passes (arm n4)",
			target:     "l0102-n4",
			body:       `{"repositories":["l0102-loc"],"principals":{"groups":{"nosuchgroup-l0102":[]}}}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "unknown user bearing actions still refuses (arm n3 control)",
			target:     "l0102-n3",
			body:       `{"repositories":["l0102-loc"],"principals":{"users":{"nosuchuser-l0102":["r"]}}}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "Permission target contains a reference to a non-existing user: 'nosuchuser-l0102'.",
		},
		{
			name:       "admin bearing actions still refuses (arm n6 control)",
			target:     "l0102-n6",
			body:       `{"repositories":["l0102-loc"],"principals":{"users":{"admin":["m"]}}}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "The user: 'admin'' has admin privileges, and cannot be added to a Permission Target.",
		},
		{
			name:   "mixed: exempt admin [] beside a bearer ghost names the ghost (arm n5)",
			target: "l0102-n5",
			body: `{"repositories":["l0102-loc"],"principals":{"users":{` +
				`"admin":[],"nosuchuser-l0102":["r"]}}}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "Permission target contains a reference to a non-existing user: 'nosuchuser-l0102'.",
		},
		{
			name:   "mixed: admin bearer beside an exempt ghost names the admin (arm n7 ordering)",
			target: "l0102-n7",
			body: `{"repositories":["l0102-loc"],"principals":{"users":{` +
				`"admin":["m"],"nosuchuser-l0102":[]}}}`,
			wantStatus: http.StatusBadRequest,
			wantIn:     "The user: 'admin'' has admin privileges, and cannot be added to a Permission Target.",
		},
	} {
		resp := t215As(t, h, http.MethodPut, "api/security/permissions/"+tc.target,
			adminUser, adminPass, tc.body)
		raw := readAllT444(t, resp)
		if resp.StatusCode != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d; body=%s", tc.name, resp.StatusCode, tc.wantStatus, raw)
			continue
		}
		if tc.wantIn != "" && !strings.Contains(raw, tc.wantIn) {
			t.Errorf("%s: body %q lacks %q", tc.name, raw, tc.wantIn)
		}
	}
}

// TestPermissionsV1EmptyActionsRichFaceFrozen: the exemption is the KEYED
// face's mirror of the reference validator — BinFlow's rich face
// (/api/v1/permissions POST) has no reference counterpart and keeps its
// frozen 400 posture for an unknown principal, empty actions or not.
func TestPermissionsV1EmptyActionsRichFaceFrozen(t *testing.T) {
	h := newHarness(t)
	seedRepo(t, h, "l0102-loc")

	resp := t215As(t, h, http.MethodPost, "api/v1/permissions", adminUser, adminPass,
		`{"name":"l0102-rich","repos":["l0102-loc"],"principals":{"users":{"nosuchuser-l0102":[]}}}`)
	raw := readAllT444(t, resp)
	if resp.StatusCode != http.StatusBadRequest ||
		!strings.Contains(raw, "Unable to find user by name 'nosuchuser-l0102'.") {
		t.Fatalf("rich face unknown principal with empty actions = %d body=%s, want the frozen 400", resp.StatusCode, raw)
	}
}
