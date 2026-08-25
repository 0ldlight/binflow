package repo_test

// M10 T-283: the D3 weave point (architecture section 15.1.5 weave 2) —
// repo.Service's package-type legality question extended by the addon-plane
// verdict seam. Legs:
//
//   - nil gate (every pre-M10 stack): the static five-type enum rules alone,
//     byte-identical M9 behavior — the pilot types stay "unknown" (invariant 1);
//   - a registry-known unlocked slot EXTENDS the legal set (T-282 leftover 2):
//     a local go repository becomes creatable, all three classes legal;
//   - a known-but-locked slot refuses create/update/delete with the pointed
//     clause (D3: tier / allowlist / the addons.disabled breaker) and records
//     the license.addon.denied audit row;
//   - the static five keep their M3 class rulings under the overlay
//     (docker stays local-only) and the floor posture (a wired gate with
//     everything unlocked changes nothing for them);
//   - the virtual-member rule (FR-85.1④): a locked member refuses the
//     virtual create/update;
//   - the SERVICE-layer content paths never consult the gate (the
//     architect's risk-1 seam: the license gate is the HTTP verb face's;
//     service-level writes — the remote pull-through's landing shape — are
//     exempt by construction, pinned here from the service side).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// fakePkgGate is a verdict table: package type -> verdict. Everything not in
// the table answers the zero verdict (unknown), mirroring the assembly
// adapter's registry miss.
type fakePkgGate struct {
	verdicts map[string]repo.PackageTypeVerdict
	asked    []string
}

func (g *fakePkgGate) Verdict(_ context.Context, packageType string) repo.PackageTypeVerdict {
	g.asked = append(g.asked, packageType)
	return g.verdicts[packageType]
}

// gateEnv is an env with the seam wired (the AttachPackageTypeGate posture
// cmd assembly will use).
func gateEnv(t TB, verdicts map[string]repo.PackageTypeVerdict) (*env, *fakePkgGate) {
	t.Helper()
	e := newEnv(t)
	g := &fakePkgGate{verdicts: verdicts}
	repo.AttachPackageTypeGate(e.svc, g)
	return e, g
}

var (
	unlockedGo = repo.PackageTypeVerdict{Known: true, Unlocked: true}
	lockedGo   = repo.PackageTypeVerdict{
		Known:    true,
		Unlocked: false,
		Refusal:  "license tier 'community' < 'pro'",
	}
	disabledNpm = repo.PackageTypeVerdict{
		Known:    true,
		Unlocked: false,
		Refusal:  "disabled by configuration (addons.disabled) — remove the entry and restart to restore",
	}
)

// TestT283NilGateKeepsStaticEnum: without the seam the static enum is the
// whole check — go is "unknown" exactly as in M9 (invariant 1's service half).
func TestT283NilGateKeepsStaticEnum(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t283-go", Type: repo.TypeLocal, PackageType: "go",
	})
	if err == nil || !errors.Is(err, repo.ErrInvalidRepoType) || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("nil-gate go create err = %v, want the static enum's ErrInvalidRepoType", err)
	}
}

// TestT283DynamicLegalSet: a registry-known unlocked slot is creatable on
// every class (the static M3 class rulings are five-type rulings; the pilot
// types carry none), and the row lands with the requested package type.
func TestT283DynamicLegalSet(t *testing.T) {
	e, g := gateEnv(t, map[string]repo.PackageTypeVerdict{"go": unlockedGo})
	mustCreateRepo(t, e, "t283-gen") // the virtual arm's member
	for _, rclass := range []string{repo.TypeLocal, repo.TypeRemote, repo.TypeVirtual} {
		row, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
			RepoKey: "t283-go-" + rclass, Type: rclass, PackageType: "go",
			Config: virtualOrRemoteConfig(rclass),
		})
		if err != nil {
			t.Fatalf("create %s go repo: %v", rclass, err)
		}
		if row.PackageType != "go" {
			t.Fatalf("stored package type = %q, want go", row.PackageType)
		}
	}
	if len(g.asked) == 0 {
		t.Fatal("the gate was never consulted")
	}
}

// virtualOrRemoteConfig feeds the minimal legal config per class (remote
// needs a url; virtual wants a members list to be meaningful but "{}" is
// accepted for the legality leg).
func virtualOrRemoteConfig(rclass string) string {
	switch rclass {
	case repo.TypeRemote:
		return `{"url":"https://upstream.example/Go"}`
	case repo.TypeVirtual:
		return `{"repositories":["t283-gen"]}`
	default:
		return `{}`
	}
}

// TestT283LockedSlotRefusals: D3's three faces — create, update, delete — all
// refuse a known-but-locked slot with the pointed clause and the audit row.
func TestT283LockedSlotRefusals(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"go": lockedGo})
	ctx := context.Background()

	_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-go", Type: repo.TypeLocal, PackageType: "go",
	})
	if !errors.Is(err, repo.ErrPackageTypeNotAvailable) {
		t.Fatalf("locked create err = %v, want ErrPackageTypeNotAvailable", err)
	}
	if want := "package type not available on this instance: package type 'go' is not available (license tier 'community' < 'pro')"; err.Error() != want {
		t.Fatalf("D3 message = %q, want %q", err.Error(), want)
	}

	// A pre-existing go repository (created under a license that later left,
	// seeded straight through the store like any expiry scenario):
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "t283-go-seeded", Type: repo.TypeLocal, PackageType: "go", Config: "{}",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: "t283-go-seeded", Description: "x"}); !errors.Is(err, repo.ErrPackageTypeNotAvailable) {
		t.Fatalf("locked update err = %v, want ErrPackageTypeNotAvailable", err)
	}
	if err := e.svc.DeleteRepo(ctx, admin(), "t283-go-seeded", false); !errors.Is(err, repo.ErrPackageTypeNotAvailable) {
		t.Fatalf("locked delete err = %v, want ErrPackageTypeNotAvailable", err)
	}
	// The refused delete left the row in place (85.3's "仓配置零删除").
	if _, err := e.md.Repos().Get(ctx, "t283-go-seeded"); err != nil {
		t.Fatalf("row vanished after a refused delete: %v", err)
	}

	// Every refusal is audited one row each (85.4's 变更面 clause): the
	// create named the would-be key, the update/delete the seeded one.
	denied := 0
	for _, ev := range e.au.events {
		if ev.Action == repo.AuditActionAddonDenied {
			denied++
			if ev.Actor != "admin" || (ev.Repo != "t283-go" && ev.Repo != "t283-go-seeded") {
				t.Fatalf("denied row wrong: %+v", ev)
			}
			if !strings.Contains(ev.Detail, `"addon":"go"`) || !strings.Contains(ev.Detail, "license tier") {
				t.Fatalf("denied detail lacks addon/refusal: %s", ev.Detail)
			}
		}
	}
	if denied != 3 {
		t.Fatalf("license.addon.denied rows = %d, want 3 (create+update+delete)", denied)
	}
}

// TestT283DisabledBreakerShape: the addons.disabled clause rides the same
// seam — message names the knob and the recovery path.
func TestT283DisabledBreakerShape(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"npm": disabledNpm})
	_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "t283-npm", Type: repo.TypeLocal, PackageType: repo.PackageNpm,
	})
	if !errors.Is(err, repo.ErrPackageTypeNotAvailable) {
		t.Fatalf("disabled create err = %v", err)
	}
	if !strings.Contains(err.Error(), "disabled by configuration") || !strings.Contains(err.Error(), "addons.disabled") {
		t.Fatalf("disabled message lacks the knob/recovery: %v", err)
	}
}

// TestT283StaticFiveUnchangedUnderOverlay: with the seam wired and
// everything unlocked, the static five behave exactly as before — and
// docker's local-only M3 ruling SURVIVES the overlay (a registry-known
// docker slot must not smuggle remote/virtual docker in).
func TestT283StaticFiveUnchangedUnderOverlay(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{
		"generic": unlockedGo, "docker": unlockedGo, "npm": unlockedGo,
	})
	ctx := context.Background()
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-generic", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
	}); err != nil {
		t.Fatalf("generic create under overlay: %v", err)
	}
	_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-docker-remote", Type: repo.TypeRemote, PackageType: repo.PackageDocker,
		Config: `{"url":"https://upstream.example"}`,
	})
	if !errors.Is(err, repo.ErrRepoTypeNotSupported) || !strings.Contains(err.Error(), "local-only") {
		t.Fatalf("docker remote under overlay = %v, want the M3 class ruling", err)
	}
	// Unknown values keep the static enum's refusal (the service backstop
	// behind httpapi's dynamic 400).
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-bogus", Type: repo.TypeLocal, PackageType: "wheelbarrow",
	}); !errors.Is(err, repo.ErrInvalidRepoType) {
		t.Fatalf("unknown type under overlay = %v, want ErrInvalidRepoType", err)
	}
}

// TestT283VirtualMemberGate: FR-85.1④ — a locked member refuses the virtual
// create; an unlocked one passes; a five-core member never meets the
// question.
func TestT283VirtualMemberGate(t *testing.T) {
	ctx := context.Background()
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"go": lockedGo})
	mustCreateRepo(t, e, "t283-gen")
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "t283-go-m", Type: repo.TypeLocal, PackageType: "go", Config: "{}",
	}); err != nil {
		t.Fatalf("seed go member: %v", err)
	}

	_, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-virt", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["t283-go-m","t283-gen"]}`,
	})
	if !errors.Is(err, repo.ErrPackageTypeNotAvailable) {
		t.Fatalf("virtual with locked member = %v, want ErrPackageTypeNotAvailable", err)
	}
	if !strings.Contains(err.Error(), `member 't283-go-m'`) || !strings.Contains(err.Error(), `'go'`) {
		t.Fatalf("member refusal does not name the member and its type: %v", err)
	}

	// A five-core-only virtual on the same gated service: no refusal (the
	// ungated member never meets the question).
	if _, err := e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "t283-virt-ok", Type: repo.TypeVirtual, PackageType: repo.PackageGeneric,
		Config: `{"repositories":["t283-gen"]}`,
	}); err != nil {
		t.Fatalf("five-core virtual under gate: %v", err)
	}
}

// TestT283ServiceWritesBypassGate: the architect's risk-1 seam, service
// half — the license gate is the HTTP verb face's, so service-level content
// writes (the remote pull-through's landing shape: a Get-driven internal
// Put) never consult it. A LOCKED go repository still serves Get and accepts
// service-level Puts.
func TestT283ServiceWritesBypassGate(t *testing.T) {
	e, _ := gateEnv(t, map[string]repo.PackageTypeVerdict{"go": lockedGo})
	ctx := context.Background()
	if err := e.md.Repos().Create(ctx, &metadata.Repo{
		RepoKey: "t283-go", Type: repo.TypeLocal, PackageType: "go", Config: "{}",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The internal write (pull-through landing shape): succeeds, gate or no
	// gate — the seam answered zero package-type questions on this path.
	n := put(t, e, admin(), "t283-go", "example.com/mod/@v/v1.0.0.zip", "bytes")
	if n.Sha256 == "" {
		t.Fatal("internal Put landed no blob")
	}
	rc, got, err := e.svc.Get(ctx, admin(), "t283-go", "example.com/mod/@v/v1.0.0.zip")
	if err != nil {
		t.Fatalf("Get on a locked repository: %v", err)
	}
	_ = rc.Close()
	if got.Sha256 != n.Sha256 {
		t.Fatalf("Get returned %q, want the landed %q", got.Sha256, n.Sha256)
	}

	// And the delete of content (not the repository row) is likewise a
	// content-plane verb the service owns — D2's 403 is the HTTP face's.
	if err := e.svc.Delete(ctx, admin(), "t283-go", "example.com/mod/@v/v1.0.0.zip"); err != nil {
		t.Fatalf("content Delete on a locked repository: %v", err)
	}
}

// TestT283AttachOnFakeServiceIsBestEffort: AttachPackageTypeGate on a
// non-concrete Service warns and skips — never a panic (the wiring-layer
// posture AttachReplicator established).
func TestT283AttachOnFakeServiceIsBestEffort(_ *testing.T) {
	repo.AttachPackageTypeGate(fakeRepoService{}, &fakePkgGate{})
}

type fakeRepoService struct{ repo.Service }
