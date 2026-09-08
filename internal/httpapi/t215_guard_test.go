// T-215 route-gate guard (FR-64, ADR-0026): the routeAuth migration is
// pinned by SOURCE ASSERTIONS, not just behavior. A gate that slips back to
// the pre-M7 admin boolean — or a new route whose capability spelling
// misses the closed set — would fail SILENTLY (readonly_admin and repo
// admins collect uniform 403s with nothing in the logs); this test is the
// compile-time-ish tripwire that turns that regression red.

package httpapi

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
)

// t215RouteGates is the migration's full extent (architecture section 7.1
// [M7] inventory, T-214 final): 25 global capability gates plus 4
// single-repo manage gates. T-215 left 26 + 4; T-217 (FR-65, the family-4
// exception the same inventory table documents) moved the two
// permission-write routes' gate into their handlers — the OR of
// CapSecurityWrite with the m-holder coverage arm is body-dependent, so the
// route literals carry only required:true and the regex below no longer
// sees them. T-251 (M9, ADR-0030 E4) added the 25th: DELETE
// /api/security/users/{name} on CapSecurityWrite, no coverage arm (users
// are not repo-domain principals). T-279 (M10, ADR-0032 / architecture
// section 15.1.4) added 26..28: GET /api/system/license on CapSystemRead
// and POST/DELETE /api/system/license on CapSystemWrite — the section 7.1
// family-1/2 rows the ADR registers. T-282 (M10, ADR-0033 / architecture
// section 15.2.5) added the 29th: GET /api/v1/addons on CapSystemRead (the
// addon status plane; readonly_admin sees the matrix, a plain user 403s).
// T-305 (M11, ADR-0035 / FR-92) added 30..38: the auth-config plane's nine
// routes — GET on CapSecurityRead x3 (ldap/oauth/saml config), PUT on
// CapSecurityWrite x3, and the three test-connection POSTs on
// CapSecurityWrite (they open outbound connections against admin-supplied
// targets; the M3 Guard screens them and the write gate keeps probing an
// admin action).
// Editing this constant is a deliberate route-gate change — update the
// inventory table with it.
const (
	t215ManageGates    = 79 // +3: T-345's trash family (empty/restore/clean, CapSystemWrite — the gc/cleanup destructive-management posture); +1: T-368's GET /api/v1/system/settings (CapSystemRead — the FR-118 knob echo, readonly_admin may see); +1: T-405's PUT /api/v1/replications/{id} (CapSystemWrite — the replication config family's create/delete posture); +1: T-420's POST /api/v1/replications/{id}/run (CapSystemWrite — the Replicate Now trigger, FR-138.1/replication.md §9.2-A; the family's write posture); +5: T-422's global-block family and Test faces (FR-138.2/138.3, replication.md §9.1-B/§9.3) — GET /api/v1/system/replications on CapSystemRead (the official camelCase pair, readonly_admin may see the brake state) + POST …/block + POST …/unblock on CapSystemWrite (the emergency brake flips) + POST /api/v1/replications/{id}/test and the id-less POST …/test draft face on CapSystemWrite (the connection probe, the family's write posture); +8: T-450's cron scheduling faces (FR-150.3/.4, ADR-0044 decision 7) — GET+PUT /api/v1/system/maintenance (CapSystemRead/CapSystemWrite — the gc/cleanup family's posture, T-214①), GET /api/v1/system/backups + GET /api/v1/system/backups/{key} on CapSystemRead, PUT /api/v1/system/backups + PUT …/{key} + DELETE …/{key} on CapSystemWrite (the backup config family's write posture), and GET /api/v1/system/schedules on CapSystemRead (the read-only ledger projection — readonly_admin may see every schedule's next-run, writes only exist on the three config faces); +3: T-452's QRL config face (FR-148.2, aql.md §14.4) — GET /api/v1/system/query_rate_limiter/config on CapSystemRead + POST and DELETE on CapSystemWrite (the license/settings family posture: Artifactory's RolesAllowed(admin) maps onto the v1-system capability family — a plain user 403s on all three, readonly_admin — a BinFlow-native role — may read); +1: T-493's GET /api/v1/system/logs on CapSystemRead (FR-157③, the System Logs process-log tail — the audit read's posture: readonly_admin may read the process stream, a plain user 403s); +1: T-513's POST /api/release/bundle on CapSystemWrite (FR-153.1, ADR-0046 decision 3 — the release-bundle create's management-plane write posture; the read faces of the family stay required-only, their dual gate is the handler's); +2: T-496's outbox row-level face (FR-159.2/LC-109, ADR-0041 decision 7 errata's as-built note) — GET /api/v1/webhooks/outbox on CapSystemRead (readonly_admin may read the delivery history, the FR-115.5/D1 posture) + POST /api/v1/webhooks/outbox/{id}/replay on CapSystemWrite (the dead-letter remedy; the webhook feature gate rides the handler, decision 8 seam 1)
	t215RepoManageBits = 9  // +2: T-309's helm reindex family; +1: T-311's yum reindex; +1: T-310's deb reindex (CanManageRepo, ADR-0034); +1: T-442's POST /api/repositories/{key}/test (CanManageRepo write — the remote form's upstream probe, FR-143.5; the config-edit arms' posture: a draft body may carry credentials, so a read-only manager cannot aim the stored credential at upstreams)
	// t215ManageGates +1 (M17 T-493, FR-157③): GET /api/v1/system/logs on
	// CapSystemRead — the System Logs process-log tail (the audit read's
	// posture: readonly_admin may read the process log, a plain user 403s;
	// anonymous the 401 challenge).
	// t215ManageGates +10 (M11 T-319, ADR-0038): the instance GPG keypair
	// plane — /api/security/keypair {POST,PUT,GET,verify POST,public GET,
	// {pairName} GET+DELETE}, /api/v1/admin/security/keypair/generate POST,
	// and the v2 repository association POST+DELETE (CapSecurityRead reads /
	// CapSecurityWrite writes, docs/design/gpg-keypair.md section 3.4).
	// t215ManageGates +2 (M11 T-324, FR-102.2): the unused-cleanup plane —
	// POST /api/v1/system/cleanup on CapSystemWrite (the gc family's
	// no-dry-run-exception rule, T-214①) and GET /api/v1/system/cleanup on
	// CapSystemRead (the status view readonly_admin may see).
	// t215ManageGates +3 (M11 T-331, auth-integration §3.2): the SAML SP
	// encryption certificate family — GET
	// /api/v1/admin/security/saml/config/key/public on CapSecurityRead (the
	// certificate is public material the readonly admin may hand the IdP)
	// and the two forced-generation verbs (PUT …/config/key/public/
	// regenerate, the Artifactory-compat face, plus the BinFlow-native
	// POST …/saml/key) on CapSecurityWrite.
	// t215ManageGates +1 (M13 T-368, FR-118): GET /api/v1/system/settings on
	// CapSystemRead — the operator-knob echo face (folder_download six
	// fields + trashcan.retention_days); read-only like the addons plane,
	// every other verb falls to the E-26 404.
)

func t215MustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name) // tests run with the package dir as CWD
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestRouteGatesCarryNoBareAdminBoolean: router.go must not reintroduce the
// pre-M7 admin boolean on any route gate, and middleware.go must not
// consult one — every management-plane route speaks a closed-set capability
// or the repo-manage gate instead (ADR-0026; the T-215 migration diff).
func TestRouteGatesCarryNoBareAdminBoolean(t *testing.T) {
	src := t215MustRead(t, "router.go")

	lits := regexp.MustCompile(`routeAuth\{[^}]*`).FindAllString(src, -1)
	if len(lits) == 0 {
		t.Fatal("no routeAuth literals found in router.go — the source scan is broken")
	}
	adminKey := regexp.MustCompile(`\badmin\s*:`)
	for _, lit := range lits {
		if adminKey.MatchString(lit) {
			t.Errorf("bare admin gate survived in route literal: %s", lit)
		}
	}

	mw := t215MustRead(t, "middleware.go")
	if strings.Contains(mw, "req.admin") {
		t.Error("middleware.go still consults req.admin — the gate field must not come back")
	}
}

// TestRouteGateCapabilitySpellings: every manage: value in router.go names
// one of the six closed-set capability constants (a typo'd gate fails
// closed at runtime — admins would collect 403s — so it must fail HERE
// first), every repoManage value goes through the gate struct, and the
// migrated set keeps its full extent.
func TestRouteGateCapabilitySpellings(t *testing.T) {
	src := t215MustRead(t, "router.go")

	closed := map[string]bool{
		"CapSystemRead": true, "CapSystemWrite": true,
		"CapSecurityRead": true, "CapSecurityWrite": true,
		"CapRepoRead": true, "CapRepoWrite": true,
	}
	manageRe := regexp.MustCompile(`manage:\s*auth\.(Cap\w+)`)
	got := manageRe.FindAllStringSubmatch(src, -1)
	if len(got) != t215ManageGates {
		t.Errorf("router.go carries %d manage gates, want %d (the section 7.1 inventory extent; a change is deliberate only with the table)", len(got), t215ManageGates)
	}
	for _, m := range got {
		if !closed[m[1]] {
			t.Errorf("manage gate %q is outside the closed capability set", m[1])
		}
	}

	repoGates := regexp.MustCompile(`repoManage:\s*&repoManageGate\{`).FindAllString(src, -1)
	if len(repoGates) != t215RepoManageBits {
		t.Errorf("router.go carries %d repoManage gates, want %d", len(repoGates), t215RepoManageBits)
	}
	if strings.Contains(src, "repoManage:") && !strings.Contains(src, "&repoManageGate{") {
		t.Error("a repoManage gate bypasses the gate struct")
	}
}

// TestRepoManageGateFailsClosedWithoutFacet: an authorizer without the
// ManagementAuthorizer facet (unit fakes) is a DENY on both gate shapes —
// the fail-closed posture of ADR-0026's route gates. A pass here would turn
// every mis-assembled stack into an open one.
func TestRepoManageGateFailsClosedWithoutFacet(t *testing.T) {
	// facetlessCan implements the Authorizer interface and nothing else —
	// the shape of a unit-test fake predating M7.
	var a auth.Authorizer = facetlessCan{}
	fn := func(_ auth.ManagementAuthorizer) bool { return true }
	if managementAllowed(a, fn) {
		t.Error("managementAllowed granted through an authorizer without the facet")
	}
	if managementAllowed(nil, fn) {
		t.Error("managementAllowed granted through a nil authorizer")
	}
}

type facetlessCan struct{}

func (facetlessCan) Can(context.Context, *auth.Principal, string, string, string) bool { return true }
