package repo_test

// T-317 (FR-101.1 / K37) at the repository-model layer: the smart remote
// pair's canonical echo and validation family — the defaults materialize in
// the echo (both false, the four sub-flags spelled), mistyped shapes refuse
// naming the field, unknown sub-fields drop one level down (scenario D at
// the sub-object), and the update plane's full-replace semantics RESET the
// pair when the body omits it.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT317SmartRemotePairDefaults: a bare remote create echoes the canonical
// materialization of BOTH fields — enableTokenAuthentication false and the
// four-flag contentSynchronisation object — and the remote_configs row
// stays untouched by the pair (no columns; the JSON is the single source,
// the hardFail precedent).
func TestT317SmartRemotePairDefaults(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "bare-remote", `{"url":"http://u"}`)
	cfg := remoteCfgOf(t, mustGetRepo(t, e, "bare-remote"))
	if cfg["enableTokenAuthentication"] != false {
		t.Fatalf("enableTokenAuthentication = %v, want false (canonical echo)", cfg["enableTokenAuthentication"])
	}
	cs, ok := cfg["contentSynchronisation"].(map[string]any)
	if !ok {
		t.Fatalf("contentSynchronisation = %v (%T), want the four-flag object", cfg["contentSynchronisation"], cfg["contentSynchronisation"])
	}
	for k, v := range map[string]any{
		"enabled": false, "statisticsEnabled": false, "propertiesEnabled": false, "sourceOrigin": false,
	} {
		if cs[k] != v {
			t.Fatalf("contentSynchronisation[%s] = %v, want %v", k, cs[k], v)
		}
	}
	row, err := e.md.Remote().GetConfig(ctx, "bare-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.URL != "http://u" {
		t.Fatalf("row url = %q, want the canonical trim", row.URL)
	}
}

// TestT317SmartRemotePairValidation: the refusal family of the pair — every
// arm a 400-shaped ErrInvalidRepoConfig naming the field or sub-field.
func TestT317SmartRemotePairValidation(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantSubstr string
	}{
		{"token auth wrong type", `{"url":"http://u","enableTokenAuthentication":"yes"}`, "enableTokenAuthentication"},
		{"content sync as bool", `{"url":"http://u","contentSynchronisation":true}`, "contentSynchronisation"},
		{"content sync as string", `{"url":"http://u","contentSynchronisation":"on"}`, "contentSynchronisation"},
		{"content sync as array", `{"url":"http://u","contentSynchronisation":[true]}`, "contentSynchronisation"},
		{"sub-field wrong type", `{"url":"http://u","contentSynchronisation":{"enabled":"sure"}}`, "contentSynchronisation"},
		{"null counts as absent", `{"url":"http://u","contentSynchronisation":null}`, ""},
		{"null token auth counts as absent", `{"url":"http://u","enableTokenAuthentication":null}`, ""},
		{"explicit false survives", `{"url":"http://u","enableTokenAuthentication":false}`, ""},
		{"unknown sub-field drops", `{"url":"http://u","contentSynchronisation":{"enabled":true,"futureFlag":true}}`, ""},
		{"nested empty object", `{"url":"http://u","contentSynchronisation":{}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "t317-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
				Config: tt.config,
			})
			if tt.wantSubstr == "" {
				if err != nil {
					t.Fatalf("unexpected refusal: %v", err)
				}
				return
			}
			if !errors.Is(err, repo.ErrInvalidRepoConfig) {
				t.Fatalf("error = %v, want ErrInvalidRepoConfig", err)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error %q does not name %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestT317SmartRemotePairFullReplace: PUT-style update is full-replace — a
// body without the pair RESETS it to the defaults (the M3 T-80 ruling the
// tuning fields already follow), and a body that sets it replaces every
// sub-flag.
func TestT317SmartRemotePairFullReplace(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "upd-remote", `{"url":"http://u","enableTokenAuthentication":true,`+
		`"contentSynchronisation":{"enabled":true,"propertiesEnabled":true,"statisticsEnabled":true}}`)
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "upd-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://u"}`,
	}); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	cfg := remoteCfgOf(t, mustGetRepo(t, e, "upd-remote"))
	if cfg["enableTokenAuthentication"] != false {
		t.Fatalf("after replace: enableTokenAuthentication = %v, want false", cfg["enableTokenAuthentication"])
	}
	cs := cfg["contentSynchronisation"].(map[string]any)
	if cs["enabled"] != false || cs["statisticsEnabled"] != false {
		t.Fatalf("after replace: contentSynchronisation = %v, want the default object", cs)
	}
}
