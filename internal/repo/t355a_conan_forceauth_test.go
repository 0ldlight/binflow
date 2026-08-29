package repo_test

// T-355A (FR-110.2, the D-5 carryover): forceConanAuthentication is a
// BOOLEAN config field of the local repository blob — validateLocalConfig
// type-checks it at configure time (the byHash posture one layer down from
// httpapi's typed transport), so a hand-shaped create/update carrying a
// non-boolean value refuses with ErrInvalidRepoConfig naming the field
// instead of storing verbatim and silently reading as false in the conan
// adapter's probe. The boolean carries no value domain of its own (both
// spellings are legal; the product default is absent/false).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

func TestT355AForceConanAuthTypeGate(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string // "" = the create must succeed
	}{
		{name: "true is legal", config: `{"forceConanAuthentication":true}`},
		{name: "false is legal", config: `{"forceConanAuthentication":false}`},
		{name: "absent is legal", config: `{"quotaBytes":100}`},
		{name: "string refuses", config: `{"forceConanAuthentication":"yes"}`, wantErr: "forceConanAuthentication"},
		{name: "number refuses", config: `{"forceConanAuthentication":1}`, wantErr: "forceConanAuthentication"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "conan-auth", Type: repo.TypeLocal, PackageType: repo.PackageGeneric,
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
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not name the field", err)
			}
		})
	}
}
