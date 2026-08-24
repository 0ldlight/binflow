package metadata_test

import (
	"context"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
)

// T-254 (M9 E9): PermissionStore.Principals — the unkeyed all-rows read
// auth.ManageCoverage walks. The contract under test: every row of every
// target in ONE query, target-then-principal order (PrincipalsFor's order),
// and the empty table answering empty.

func TestPermissionPrincipalsAll(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if rows, err := st.Permissions().Principals(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("empty table Principals() = %v, %v; want empty, nil", rows, err)
	}

	putRepo(t, st, "t254-r0")
	putRepo(t, st, "t254-r1")
	mk := func(name, repo string, principals []*metadata.PermissionPrincipal) {
		t.Helper()
		if err := st.Permissions().PutTarget(ctx, &metadata.PermissionTarget{
			Name: name, Repos: `["` + repo + `"]`, Includes: "[]", Excludes: "[]",
			CreatedAt: "2026-08-24T00:00:00Z", UpdatedAt: "2026-08-24T00:00:00Z",
		}, principals); err != nil {
			t.Fatalf("PutTarget(%s): %v", name, err)
		}
	}
	mk("t254-a", "t254-r0", []*metadata.PermissionPrincipal{
		{TargetName: "t254-a", Principal: "zoe", PrincipalType: "user", CanRead: true},
		{TargetName: "t254-a", Principal: "app-admins", PrincipalType: "group", CanManage: true},
	})
	mk("t254-b", "t254-r1", []*metadata.PermissionPrincipal{
		{TargetName: "t254-b", Principal: "amy", PrincipalType: "user", CanManage: true},
	})

	rows, err := st.Permissions().Principals(ctx)
	if err != nil {
		t.Fatalf("Principals: %v", err)
	}
	type row struct{ target, principal, ptype string }
	var got []row
	var manageFlags []bool
	for _, r := range rows {
		got = append(got, row{r.TargetName, r.Principal, r.PrincipalType})
		manageFlags = append(manageFlags, r.CanManage)
	}
	want := []row{
		{"t254-a", "app-admins", "group"},
		{"t254-a", "zoe", "user"},
		{"t254-b", "amy", "user"},
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %+v, want %+v (order: target then principal)", i, got[i], want[i])
		}
	}
	if manageFlags[0] != true || manageFlags[1] != false || manageFlags[2] != true {
		t.Fatalf("can_manage flags = %v, want [true false true]", manageFlags)
	}

	// The per-target slice Principals serves must agree with GetTarget's
	// own rows — the E6 renderer buckets the former while the frozen
	// no-filter list rides the latter; a disagreement would make the two
	// faces drift.
	for _, name := range []string{"t254-a", "t254-b"} {
		_, viaGet, err := st.Permissions().GetTarget(ctx, name)
		if err != nil {
			t.Fatalf("GetTarget(%s): %v", name, err)
		}
		var viaAll []*metadata.PermissionPrincipal
		for _, r := range rows {
			if r.TargetName == name {
				viaAll = append(viaAll, r)
			}
		}
		if len(viaGet) != len(viaAll) {
			t.Fatalf("%s: GetTarget rows %d vs Principals rows %d", name, len(viaGet), len(viaAll))
		}
		for i := range viaGet {
			if viaGet[i].Principal != viaAll[i].Principal || viaGet[i].PrincipalType != viaAll[i].PrincipalType ||
				viaGet[i].CanRead != viaAll[i].CanRead || viaGet[i].CanWrite != viaAll[i].CanWrite ||
				viaGet[i].CanDelete != viaAll[i].CanDelete || viaGet[i].CanManage != viaAll[i].CanManage {
				t.Fatalf("%s row %d: GetTarget %+v vs Principals %+v disagree", name, i, viaGet[i], viaAll[i])
			}
		}
	}
}
