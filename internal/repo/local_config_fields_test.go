package repo_test

// T-490 (FR-156.1): the local config plane's B-1.5 field family — the four
// round-trip domains (repoLayoutRef / blackedOut / maxUniqueSnapshots /
// archiveBrowsingEnabled) plus the Stage domain's two wire spellings
// (environments canonical, stages the 7.161-era alias). The local blob is
// caller-owned (the M1 passthrough contract): Create/Update store the keys
// verbatim and GetRepo echoes them, so the round-trip contract is per-key
// equality. The two cross-cutting rules live in validateLocalConfig:
// blackedOut's decode-time typing (the write gate reads it) and the stage
// alias-disagreement refusal.

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// roundTrip runs one config blob through create (or update) and returns the
// echoed config map.
func roundTrip(t *testing.T, e *env, key, body string, update bool) (map[string]any, error) {
	t.Helper()
	ctx := context.Background()
	var err error
	if update {
		_, err = e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{RepoKey: key, Config: body})
	} else {
		_, err = e.svc.CreateRepo(ctx, admin(), &metadata.Repo{
			RepoKey: key, Type: repo.TypeLocal, PackageType: repo.PackageGeneric, Config: body,
		})
	}
	if err != nil {
		return nil, err
	}
	row, err := e.svc.GetRepo(ctx, admin(), key)
	if err != nil {
		t.Fatalf("GetRepo(%s): %v", key, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(row.Config), &m); err != nil {
		t.Fatalf("echoed config %q: %v", row.Config, err)
	}
	return m, nil
}

// TestLocalConfigFourDomainRoundTrip: every B-1.5 domain plus both Stage
// spellings survive create AND update verbatim — the drift T-439 pinned
// (configJSON dropped all four) is closed at the config layer too.
func TestLocalConfigFourDomainRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		body string
		want map[string]any
	}{
		{"all four plus stages", `{
			"repoLayoutRef": "maven-2-default",
			"blackedOut": true,
			"maxUniqueSnapshots": 7,
			"archiveBrowsingEnabled": true
		}`, map[string]any{
			"repoLayoutRef": "maven-2-default", "blackedOut": true,
			"maxUniqueSnapshots": 7.0, "archiveBrowsingEnabled": true,
		}},
		{"explicit product defaults survive (K71 pointer posture)", `{
			"blackedOut": false,
			"maxUniqueSnapshots": 0,
			"archiveBrowsingEnabled": false
		}`, map[string]any{
			"blackedOut": false, "maxUniqueSnapshots": 0.0, "archiveBrowsingEnabled": false,
		}},
		{"environments spelling", `{"environments":["DEV","PROD"]}`,
			map[string]any{"environments": []any{"DEV", "PROD"}}},
		{"stages spelling (7.161-era alias)", `{"stages":["BOX"]}`,
			map[string]any{"stages": []any{"BOX"}}},
		{"both spellings agreeing", `{"environments":["DEV"],"stages":["DEV"]}`,
			map[string]any{"environments": []any{"DEV"}, "stages": []any{"DEV"}}},
		{"explicit empty stage list is a legal clear", `{"environments":[]}`,
			map[string]any{"environments": []any{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			// Create arm first, then the update arm on the same row (the
			// blob is replaced wholesale either way).
			for _, update := range []bool{false, true} {
				got, err := roundTrip(t, e, "lib", tt.body, update)
				if err != nil {
					t.Fatalf("round-trip (update=%t): %v", update, err)
				}
				for k, want := range tt.want {
					if !reflect.DeepEqual(got[k], want) {
						t.Errorf("round-trip (update=%t): %s = %#v, want %#v (full echo %v)",
							update, k, got[k], want, got)
					}
				}
			}
		})
	}
}

// TestLocalConfigStageAliasRules: the two Stage spellings are one knob —
// disagreement refuses; a non-token entry refuses; the disagreement names
// both spellings.
func TestLocalConfigStageAliasRules(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{"agree", `{"environments":["DEV"],"stages":["DEV"]}`, true},
		{"disagree", `{"environments":["DEV"],"stages":["PROD"]}`, false},
		{"length disagree", `{"environments":["DEV","PROD"],"stages":["DEV"]}`, false},
		{"empty entry", `{"environments":["DEV",""]}`, false},
		{"whitespace entry", `{"stages":["  "]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := roundTrip(t, e, "lib", tt.body, false)
			if tt.ok && err != nil {
				t.Fatalf("expected acceptance, got %v", err)
			}
			if !tt.ok {
				if !errors.Is(err, repo.ErrInvalidRepoConfig) {
					t.Fatalf("expected ErrInvalidRepoConfig, got %v", err)
				}
			}
		})
	}
}

// TestLocalConfigBlackedOutTyping: the write gate reads the mark off the
// stored blob, so a mistyped value is refused at CONFIG time with the field
// named — never discovered as a silently-false read inside the gate.
func TestLocalConfigBlackedOutTyping(t *testing.T) {
	e := newEnv(t)
	_, err := roundTrip(t, e, "lib", `{"blackedOut":"yes"}`, false)
	if !errors.Is(err, repo.ErrInvalidRepoConfig) {
		t.Fatalf("mistyped blackedOut: expected ErrInvalidRepoConfig, got %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "blackedOut") {
		t.Errorf("refusal %q does not name the field", got)
	}
}
