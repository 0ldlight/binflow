package nuget

import (
	"context"
	"net/http"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// The v3 flatcontainer push faces' duplicate-arm matrix (nuget.md
// section 5.4, K59 — the Q6 ruling of 2026-08-31 = align on 409; the
// same-family flip D-10/T-378 performed on the v2 face).

// TestV3FlatPushDuplicateArm: section 5.4's four-arm matrix over the
// DIRECT push form (the publish base itself — the modern dotnet client's
// shape), table-driven:
//
//	②a different bytes + w-only principal → 409, the official wording;
//	②b SAME bytes + w-only principal      → 409, the official wording —
//	  the Q6/T-401 FLIP: the as-built posture (the overwrite gate's 403,
//	  and before D-10 the idempotent 201) is reversed HERE; T-401 is that
//	  leg's exemption of record;
//	③  the delete right holds             → overwrite, 201 (both the
//	  different-bytes and the same-bytes spelling), the package file
//	  serves the pushed bytes;
//	④  fresh package                      → 201, w alone suffices.
//
// The addressed form's ② leg and the no-probe guards (remote, unrouted
// virtual — neither may turn 409, and neither may consult existence)
// ride below the table.
func TestV3FlatPushDuplicateArm(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-local", repo.TypeLocal)
	s.seedRepo(t, "ng-remote", repo.TypeRemote)
	s.seedUser(t, "writer", "writerpass") // w but no d
	s.seedGrant(t, "nuget-writer", "writer", "ng-local", true, true, false)

	// pushDirect builds one fresh package and PUTs it DIRECTLY to the
	// publish base (no id/version in the URL — the nuspec is the
	// identity). Identical id/version/deps rebuild byte-identical
	// packages: the same-bytes arms rest on the fixture's determinism.
	pushDirect := func(repoKey, id, deps, user, pass string) (int, string, http.Header, string) {
		pkg := buildNupkg(t, id, "1.0.0", deps)
		status, body, hdr := s.do(http.MethodPut, apiPath(repoKey)+"/"+segFlat, user, pass, bytesReader(pkg.body), nil)
		return status, body, hdr, string(pkg.body)
	}

	tests := []struct {
		name string
		id   string
		// seed true first lands the package as admin with seedDeps (arm ④
		// seeds nothing — the case push IS the first).
		seed     bool
		seedDeps string
		pushDeps string
		asWriter bool
		want     int
		// want409 exact: the official wording IS the body's first line.
		want409Exact bool
		// downloadWant: after a 201, the package file must serve the
		// case's own pushed bytes (arm ③'s overwrite proof).
		downloadWant bool
	}{
		{
			name:     "arm2a-different-bytes-w-only",
			id:       "Dup.A",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("Serilog", "4.0.0"),
			asWriter: true,
			want:     http.StatusConflict,
		},
		{
			name:     "arm2b-same-bytes-w-only (Q6 flip, T-401; as-built 403 reversed)",
			id:       "Dup.B",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("none"), // == seed deps: byte-identical package
			asWriter: true,
			want:     http.StatusConflict,
		},
		{
			name:         "arm3-d-right-overwrites-different-bytes",
			id:           "Dup.C",
			seed:         true,
			seedDeps:     flatDeps("none"),
			pushDeps:     flatDeps("Serilog", "4.0.0"),
			asWriter:     false, // admin holds d
			want:         http.StatusCreated,
			downloadWant: true,
		},
		{
			name:     "arm3-d-right-same-bytes-retransmit",
			id:       "Dup.E",
			seed:     true,
			seedDeps: flatDeps("none"),
			pushDeps: flatDeps("none"), // same bytes, but d holds: still 201
			asWriter: false,
			want:     http.StatusCreated,
		},
		{
			name:     "arm4-fresh-package",
			id:       "Dup.D",
			pushDeps: flatDeps("none"),
			asWriter: true,
			want:     http.StatusCreated,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.seed {
				if status, body, _, _ := pushDirect("ng-local", tc.id, tc.seedDeps, adminUser, adminPass); status != http.StatusCreated {
					t.Fatalf("seed push: %d %s", status, body)
				}
			}
			user, pass := adminUser, adminPass
			if tc.asWriter {
				user, pass = "writer", "writerpass"
			}
			status, body, hdr, pushed := pushDirect("ng-local", tc.id, tc.pushDeps, user, pass)
			if status != tc.want {
				t.Fatalf("push = %d %s, want %d", status, body, tc.want)
			}
			switch tc.want {
			case http.StatusConflict:
				// The official wording, verbatim (section 5.4 A1).
				if got := firstLine(body); got != msgPushDuplicate {
					t.Errorf("409 body = %q, want the exact official wording %q", got, msgPushDuplicate)
				}
			case http.StatusCreated:
				wantPath := lowerASCII(tc.id) + "/1.0.0/" + lowerASCII(tc.id) + ".1.0.0" + suffixNupkg
				if loc := hdr.Get("Location"); loc != wantPath {
					t.Errorf("201 Location = %q, want %q", loc, wantPath)
				}
			}
			if tc.downloadWant {
				gstatus, gbody, _ := s.get(packagePath("ng-local", lowerASCII(tc.id), "1.0.0", "nupkg"))
				if gstatus != http.StatusOK || gbody != pushed {
					t.Errorf("overwritten package file = (%d, len %d), want the pushed bytes", gstatus, len(gbody))
				}
			}
		})
	}

	// The ADDRESSED form (flatcontainer/<id>/<version>, the PRD's carrier
	// and the curl surface) is the same push face — arm ② rides it too
	// (Dup.A already exists from arm 2a; the writer still holds no d).
	pkg := buildNupkg(t, "Dup.A", "1.0.0", flatDeps("Serilog", "9.9.9"))
	status, body, _ := s.do(http.MethodPut, pushPath("ng-local", "dup.a", "1.0.0"), "writer", "writerpass", bytesReader(pkg.body), nil)
	if status != http.StatusConflict || firstLine(body) != msgPushDuplicate {
		t.Errorf("addressed-form arm2 = (%d, %q), want 409 + the official wording", status, firstLine(body))
	}

	// The no-probe guards: neither the REMOTE push (the service's
	// read-only door) nor the UNROUTED VIRTUAL push (the routing refusal)
	// may answer the 409 — existence is never consulted on a class that
	// cannot land.
	s.seedRepo(t, "ng-virt", repo.TypeVirtual)
	pkg2 := buildNupkg(t, "Dup.R", "1.0.0", flatDeps("none"))
	if status, body, _ := s.put(apiPath("ng-remote")+"/"+segFlat, pkg2.body, nil); status == http.StatusConflict {
		t.Errorf("remote push = 409 %q — the read-only refusal family must own it", firstLine(body))
	}
	if status, body, _ := s.put(apiPath("ng-virt")+"/"+segFlat, pkg2.body, nil); status == http.StatusConflict {
		t.Errorf("unrouted virtual push = 409 %q — the routing refusal must own it", firstLine(body))
	}
}

// TestV3FlatPushDuplicateArmVirtual: the routed virtual's push is the
// deployment member's publish (section 5.1's virtual row), so arm ②
// rides it through the member walk — and the deployment member is where
// the node must exist for the 409 to fire.
func TestV3FlatPushDuplicateArmVirtual(t *testing.T) {
	s := newStack(t)
	s.seedRepo(t, "ng-dep", repo.TypeLocal)
	s.seedRepo(t, "ng-virt2", repo.TypeVirtual)
	if err := s.md.Repos().Update(context.Background(), &metadata.Repo{
		RepoKey: "ng-virt2", Type: repo.TypeVirtual, PackageType: Protocol,
		Config: `{"defaultDeploymentRepo":"ng-dep"}`,
	}); err != nil {
		t.Fatalf("route virtual: %v", err)
	}
	s.seedVirtualMembers(t, "ng-virt2", "ng-dep")
	// The writer holds r+w on the virtual (the middleware's write door and
	// the probe's read) and r+w on the deployment member (the landing's
	// write) — but no d anywhere.
	s.seedUser(t, "writer", "writerpass")
	s.seedGrant(t, "virt-rw", "writer", "ng-virt2", true, true, false)
	s.seedGrant(t, "dep-write", "writer", "ng-dep", true, true, false)

	if status, body, _ := s.put(apiPath("ng-dep")+"/"+segFlat, buildNupkg(t, "Virt.Dup", "1.0.0", flatDeps("none")).body, nil); status != http.StatusCreated {
		t.Fatalf("seed via deployment member: %d %s", status, body)
	}
	pkg := buildNupkg(t, "Virt.Dup", "1.0.0", flatDeps("Serilog", "4.0.0"))
	status, body, _ := s.do(http.MethodPut, apiPath("ng-virt2")+"/"+segFlat, "writer", "writerpass", bytesReader(pkg.body), nil)
	if status != http.StatusConflict || firstLine(body) != msgPushDuplicate {
		t.Errorf("virtual arm2 = (%d, %q), want 409 + the official wording", status, firstLine(body))
	}
}
