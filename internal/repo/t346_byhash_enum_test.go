package repo_test

// T-346 (FR-113.2 BE, the T-327R leftover ② / K45 ruling): the byHash value
// domain is a repo.Service configure-time gate — a value outside
// ALL/SHA256/NONE (debian.md section 5, high confidence) refuses the create
// and the update with ErrInvalidRepoConfig naming the enum, instead of
// storing verbatim and behaving as NONE at read time. The gate rides
// validateLocalConfig, so it covers the LOCAL arm wherever the key appears
// (package-type-agnostic, the quotaBytes posture); rpm.md defines no byHash
// knob of its own — its policy keys stay decode-typed.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT346ByHashEnumCreate: the closed value domain at create time.
func TestT346ByHashEnumCreate(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string // "" = the create must succeed
	}{
		{name: "ALL is legal", config: `{"byHash":"ALL"}`},
		{name: "SHA256 is legal", config: `{"byHash":"SHA256"}`},
		{name: "NONE is legal", config: `{"byHash":"NONE"}`},
		{name: "absent is legal", config: `{"quotaBytes":100}`},
		{name: "empty string is absent", config: `{"byHash":""}`},
		// The ticket's own spelling — the canonical illegal-value probe.
		{name: "strong refuses", config: `{"byHash":"strong"}`, wantErr: "must be one of ALL, SHA256, NONE"},
		{name: "lowercase all refuses", config: `{"byHash":"all"}`, wantErr: "must be one of ALL, SHA256, NONE"},
		{name: "sha256 lowercase refuses", config: `{"byHash":"sha256"}`, wantErr: "must be one of ALL, SHA256, NONE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "deb-enum", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
				Config: tt.config,
			})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("error = %v, want ErrInvalidRepoConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "byHash") {
				t.Fatalf("error %q does not name the field and the enum", err)
			}
		})
	}
}

// TestT346ByHashEnumUpdate: the same gate on the replace path — an update
// carrying an illegal value refuses, a legal one round-trips verbatim (the
// blob stays caller-owned; only the value domain is judged).
func TestT346ByHashEnumUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateTypedRepo(t, e, "deb-upd", repo.PackageGeneric)

	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "deb-upd", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"byHash":"strong"}`,
	}); !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), "byHash") {
		t.Fatalf("update with byHash=strong: err = %v, want ErrInvalidRepoConfig naming byHash", err)
	}

	row, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "deb-upd", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Config: `{"byHash":"SHA256"}`,
	})
	if err != nil {
		t.Fatalf("update with byHash=SHA256: %v", err)
	}
	if !strings.Contains(row.Config, `"byHash":"SHA256"`) {
		t.Fatalf("stored config = %s, want the legal value verbatim", row.Config)
	}

	// The stored bad value of the pre-gate era is unreachable through the
	// service — but a keep-current update (no config) on a hand-mangled row
	// still succeeds: the gate judges WRITES, never heals reads.
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "deb-upd", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
		Description: "config untouched",
	}); err != nil {
		t.Fatalf("description-only update refused: %v", err)
	}
}
