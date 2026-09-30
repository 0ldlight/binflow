package repo_test

// T-585 (BIN-67 / L039 C6): the checksumPolicyType value domain is a
// repo.Service configure-time gate — the REST config plane accepts exactly
// {client-checksums, server-generated-checksums} (live-pinned on the reference
// 7.161.26, L039 Arm 6); every other value refuses create AND update with the
// reference's literal wording `No checksum policy type found for type: <v>`
// carried verbatim by a *StatusError (Error() IS the client message; the
// ErrInvalidRepoConfig cause keeps httpapi's 400 mapping). Absent/empty/null
// stays legal (no policy stored — the read plane renders the product default).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestChecksumPolicyTypeEnumCreate walks the closed domain at create time.
func TestChecksumPolicyTypeEnumCreate(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantMsg string // "" = the create must succeed
	}{
		{name: "client-checksums is legal", config: `{"checksumPolicyType":"client-checksums"}`},
		{name: "server-generated-checksums is legal", config: `{"checksumPolicyType":"server-generated-checksums"}`},
		{name: "absent is legal", config: `{"maxUniqueSnapshots":1}`},
		{name: "empty string is legal", config: `{"checksumPolicyType":""}`},
		{name: "null is legal", config: `{"checksumPolicyType":null}`},
		// The ledger's own spelling — the canonical illegal-value probe.
		{name: "none refuses", config: `{"checksumPolicyType":"none"}`,
			wantMsg: "No checksum policy type found for type: none"},
		{name: "adapter alias spelling refuses on the REST face", config: `{"checksumPolicyType":"generate-if-absent"}`,
			wantMsg: "No checksum policy type found for type: generate-if-absent"},
		{name: "case variant refuses", config: `{"checksumPolicyType":"Client-Checksums"}`,
			wantMsg: "No checksum policy type found for type: Client-Checksums"},
		{name: "the value interpolates verbatim", config: `{"checksumPolicyType":"bogus"}`,
			wantMsg: "No checksum policy type found for type: bogus"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "cpt-enum", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
				Config: tt.config,
			})
			if tt.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("error = %v, want ErrInvalidRepoConfig cause", err)
			}
			if err.Error() != tt.wantMsg {
				t.Fatalf("error = %q, want the reference literal %q", err.Error(), tt.wantMsg)
			}
			var se *repo.StatusError
			if !errors.As(err, &se) || se.Code != 400 {
				t.Fatalf("error = %v, want a *StatusError with code 400", err)
			}
		})
	}
}

// TestChecksumPolicyTypeEnumUpdate: the same gate on the replace path — an
// update carrying an illegal value refuses with the literal, a legal one
// round-trips verbatim (the blob stays caller-owned; only the value domain is
// judged), and a keep-current update never re-validates the stored blob.
func TestChecksumPolicyTypeEnumUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "cpt-upd", repo.PackageMaven)

	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "cpt-upd", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Config: `{"checksumPolicyType":"none"}`,
	}); err == nil || err.Error() != "No checksum policy type found for type: none" {
		t.Fatalf("update with none: err = %v, want the reference literal", err)
	}

	row, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "cpt-upd", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Config: `{"checksumPolicyType":"server-generated-checksums"}`,
	})
	if err != nil {
		t.Fatalf("update with the legal spelling: %v", err)
	}
	if !strings.Contains(row.Config, `"checksumPolicyType":"server-generated-checksums"`) {
		t.Fatalf("stored config = %s, want the legal value verbatim", row.Config)
	}

	// The gate judges WRITES, never heals reads: a description-only update
	// on a row whose stored blob carries a pre-gate value still succeeds.
	mustCreateTypedRepo(t, e, "cpt-legacy", repo.PackageMaven)
	if err := e.md.Repos().Update(ctx, &metadata.Repo{
		RepoKey: "cpt-legacy", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Config: `{"checksumPolicyType":"none"}`,
	}); err != nil {
		t.Fatalf("seed hand-mangled row: %v", err)
	}
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "cpt-legacy", Type: repo.TypeLocal, PackageType: repo.PackageMaven,
		Description: "config untouched",
	}); err != nil {
		t.Fatalf("description-only update refused: %v", err)
	}
}
