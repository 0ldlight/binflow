// T-215 review round, B1 (correctness REQUEST_CHANGES): the family-6
// create-arm guard — repository creation stays on CapRepoWrite inside
// handleRepoPut — had NO discriminating coverage. Every readonly_admin and
// plain-user 403 on the route comes from the family-7 route gate
// (repoManage write), so those principals never reach the arm; the only
// principal that does is a manage holder whose target lists the very key
// being created, because targetListsRepo is EXACT membership and a target
// survives the deletion of its repository. Today repo.Service.CreateRepo's
// requireAdmin backstops the arm, but T-217 relaxes that service door —
// from then on this branch is the ONLY guard on repository creation.
//
// The fixture therefore arrives through the metadata seam (PutTarget +
// PermissionPrincipal{CanManage:true}; the REST plane rejects the manage
// letter until T-217), and the ghost target names a repository that was
// created and deleted so the key exists in coverage but not in the store.

package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// t215GrantManage seeds one permission-target principal row through the
// metadata seam — the manage bit the REST plane cannot spell yet (T-217).
func t215GrantManage(t *testing.T, h *harness, target, repoKey, user string, canRead bool) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	principals := []*metadata.PermissionPrincipal{{
		TargetName: target, Principal: user, PrincipalType: "user",
		CanRead: canRead, CanManage: true,
	}}
	err := h.md.Permissions().PutTarget(context.Background(), &metadata.PermissionTarget{
		Name: target, Repos: `["` + repoKey + `"]`, Includes: "[]", Excludes: "[]",
		CreatedAt: now, UpdatedAt: now,
	}, principals)
	if err != nil {
		t.Fatalf("seed manage target %s: %v", target, err)
	}
}

// t215ErrMessage decodes the errors[] envelope's first message.
func t215ErrMessage(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Errors []struct {
			Status  int    `json:"status"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.Errors) == 0 {
		t.Fatalf("decode error envelope %s: %v", body, err)
	}
	return env.Errors[0].Message
}

// TestT215ManageHolderCreateArmStaysAdminOnly is the B1 red-green pin. The
// three legs, each load-bearing:
//
//  1. the manage holder READS the single-repo family through the route
//     gate's m evaluation (200) — proof the fixture holds a live manage bit
//     and the gate really passes manage holders (a plain user's 403 on the
//     same row is the control);
//  2. the CREATE arm: the route gate passes (the ghost target lists the
//     key), and the family-6 branch must answer the 403 itself. The
//     MESSAGE is the discriminator: the branch renders the gate wording
//     "administrator privileges required", while the service backstop
//     (repo.Service requireAdmin) renders "admin privileges required:
//     permission denied". Deleting the branch flips this leg red — the
//     review's red-green requirement — because the 403 then arrives with
//     the service door's wording (verified: the leg fails at exactly this
//     assertion);
//  3. the ?permissions view renders the m letter (review N1): without
//     principalLetters' manage arm the holder would show ["r"] only.
func TestT215ManageHolderCreateArmStaysAdminOnly(t *testing.T) {
	h := newHarness(t)
	t215Setup(t, h)

	// The manage holder: a plain account whose manage bits ride two targets.
	t215Admin(t, h, http.MethodPut, "api/security/users/mholder",
		`{"name":"mholder","email":"m@t.io","password":"mh-pw","admin":false}`, 201)
	// t-mh: the live repository (read + manage — the view's path probe
	// itself needs read, the manage bit is what the assertions key on).
	t215GrantManage(t, h, "t-mh", "m7t", "mholder", true)
	// t-ghost: a key that exists in coverage but not in the store — create
	// the repository so the target names something real, grant, then delete
	// the repository (targets carry no cascade; the row survives).
	t215Admin(t, h, http.MethodPut, "api/repositories/m7ghost",
		`{"rclass":"local","packageType":"generic"}`, 200)
	t215GrantManage(t, h, "t-ghost", "m7ghost", "mholder", false)
	t215Admin(t, h, http.MethodDelete, "api/repositories/m7ghost", "", 200)

	// Leg 1: the manage bit opens the family-7 read arm for the holder, and
	// the plain user's 403 on the same row proves it was the m evaluation.
	if got := t215Code(t, h, http.MethodGet, "api/repositories/m7t", "mholder", "mh-pw", ""); got != http.StatusOK {
		t.Errorf("GET repo detail as manage holder = %d, want 200 (route gate passes via m)", got)
	}
	if got := t215Code(t, h, http.MethodGet, "api/repositories/m7t", "plain", "plain-pw", ""); got != http.StatusForbidden {
		t.Errorf("GET repo detail as plain user = %d, want 403 (no m, control leg)", got)
	}

	// Leg 2: the create arm. The route gate passes (t-ghost lists m7ghost);
	// the family-6 branch must be the door that answers.
	resp := t215As(t, h, http.MethodPut, "api/repositories/m7ghost", "mholder", "mh-pw",
		`{"rclass":"local","packageType":"generic"}`)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("PUT create arm as manage holder = %d, want 403 (body: %s)", resp.StatusCode, body)
	}
	msg := t215ErrMessage(t, body)
	if !strings.Contains(msg, "administrator privileges required") {
		t.Errorf("create-arm denial came from the wrong door: message %q — the family-6 branch "+
			"renders the gate wording; the service backstop would say %q",
			msg, "admin privileges required: permission denied")
	}
	// The repository must not exist: a denied create leaves nothing behind.
	if got := t215Code(t, h, http.MethodGet, "api/repositories/m7ghost", adminUser, adminPass, ""); got != http.StatusNotFound {
		t.Errorf("GET m7ghost after denied create = %d, want 404", got)
	}

	// Leg 3: the effective-permission view — fetched BY the manage holder
	// (the route gate's m evaluation passes; the path probe needs the read
	// bit the same row carries) — renders the manage letter (review N1):
	// exactly r+m, the two bits the row carries.
	view := t215As(t, h, http.MethodGet, "api/storage/m7t/?permissions", "mholder", "mh-pw", "")
	viewBody, _ := io.ReadAll(view.Body)
	_ = view.Body.Close()
	if view.StatusCode != http.StatusOK {
		t.Fatalf("GET ?permissions as manage holder = %d (body: %s)", view.StatusCode, viewBody)
	}
	var pv struct {
		Principals struct {
			Users map[string][]string `json:"users"`
		} `json:"principals"`
	}
	if err := json.Unmarshal(viewBody, &pv); err != nil {
		t.Fatalf("decode permissions view: %v", err)
	}
	letters, ok := pv.Principals.Users["mholder"]
	if !ok {
		t.Fatalf("permissions view omits the manage holder: %s", viewBody)
	}
	if len(letters) != 2 || letters[0] != "r" || letters[1] != "m" {
		t.Errorf("manage holder letters = %v, want [r m] (the m letter is the T-215 wire addition)", letters)
	}
}
