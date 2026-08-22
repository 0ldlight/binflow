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
// [M7] inventory, T-214 final): 26 global capability gates plus 4
// single-repo manage gates, over the 30 routes that carried admin:true.
// Editing this constant is a deliberate route-gate change — update the
// inventory table with it.
const (
	t215ManageGates    = 26
	t215RepoManageBits = 4
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
