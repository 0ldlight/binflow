package repo_test

// ADR-0050 (LOOP 008 L008-1b): the remote repository update face's
// merge-on-omit three-column matrix, pinned seat by seat at the service
// layer — omitted keys keep the stored canonical form, scalar null/""
// clears, explicit 0 stores 0 (create face included), the object family
// keeps on omit/null and REPLACES wholesale on an explicit object ({}
// resets, an unmentioned sub-key lands false — probes A14/A15 on the live
// reference). The ARRAY column of the reference matrix (customHttpHeaders:
// null and [] both keep; no clear channel) has no seat in BinFlow's remote
// model — the field rides the scenario-D unknown-field drop, so its arms
// live in the differential legs (A4/A6), not here.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// l008SeedConfig seeds every matrix-relevant seat with a NON-default value
// so "kept" can never pass by accident (a default-valued seat would keep
// and reset identically).
const l008SeedConfig = `{"url":"https://up.example.org/base","username":"seed-user","password":"seed-pass",` +
	`"hardFail":true,"retrievalCachePeriodSecs":3600,"missedRetrievalCachePeriodSecs":7200,` +
	`"socketTimeoutSecs":30,"metadataRetrievalCachePeriodSecs":1200,` +
	`"enableTokenAuthentication":true,` +
	`"contentSynchronisation":{"enabled":true,"statisticsEnabled":true,"propertiesEnabled":true,"sourceOrigin":true},` +
	`"repoLayoutRef":"simple-default","blackedOut":true,"maxUniqueSnapshots":5,` +
	`"archiveBrowsingEnabled":true}`

// l008UpdateRemote runs one update through the service.
func l008UpdateRemote(t *testing.T, e *env, key, config string) error {
	t.Helper()
	_, err := e.svc.UpdateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: key, Type: repo.TypeRemote, PackageType: repo.PackageGeneric, Config: config,
	})
	return err
}

// l008RemoteRow fetches the remote_configs row for assertions.
func l008RemoteRow(t *testing.T, e *env, key string) *metadata.RemoteConfig {
	t.Helper()
	row, err := e.md.Remote().GetConfig(context.Background(), key)
	if err != nil {
		t.Fatalf("GetConfig(%s): %v", key, err)
	}
	return row
}

// TestRemoteUpdateMergeMatrix: table-driven over the four input forms
// (omit / explicit null / explicit empty / explicit value) crossed with the
// modeled families (scalar strings incl. credentials, numeric periods,
// booleans, the object family). Each row runs against a fresh seed and
// asserts the canonical echo AND the remote_configs row.
func TestRemoteUpdateMergeMatrix(t *testing.T) {
	tests := []struct {
		name string
		body string
		refusals
		check func(t *testing.T, cfg map[string]any, row, before *metadata.RemoteConfig)
	}{
		{
			name: "scalar omit keeps url and every field (the A1/A9 family)",
			body: `{"hardFail":false}`,
			check: func(t *testing.T, cfg map[string]any, row, _ *metadata.RemoteConfig) {
				if cfg["url"] != "https://up.example.org/base" {
					t.Fatalf("url = %v, want kept", cfg["url"])
				}
				if cfg["username"] != "seed-user" {
					t.Fatalf("username = %v, want kept", cfg["username"])
				}
				if cfg["hardFail"] != false {
					t.Fatalf("hardFail = %v, want the explicit false", cfg["hardFail"])
				}
				if cfg["retrievalCachePeriodSecs"] != float64(3600) {
					t.Fatalf("retrievalCachePeriodSecs = %v, want kept 3600", cfg["retrievalCachePeriodSecs"])
				}
				if cfg["socketTimeoutMillis"] != float64(30000) {
					t.Fatalf("socketTimeoutMillis = %v, want kept 30000", cfg["socketTimeoutMillis"])
				}
				if cfg["repoLayoutRef"] != "simple-default" || cfg["blackedOut"] != true ||
					cfg["maxUniqueSnapshots"] != float64(5) || cfg["archiveBrowsingEnabled"] != true {
					t.Fatalf("round-trip domains = %v/%v/%v/%v, want all kept",
						cfg["repoLayoutRef"], cfg["blackedOut"], cfg["maxUniqueSnapshots"], cfg["archiveBrowsingEnabled"])
				}
				if row.URL != "https://up.example.org/base" || row.Username != "seed-user" {
					t.Fatalf("row = %q/%q, want kept", row.URL, row.Username)
				}
			},
		},
		{
			name: "empty object keeps everything",
			body: `{}`,
			check: func(t *testing.T, cfg map[string]any, row, _ *metadata.RemoteConfig) {
				if cfg["url"] != "https://up.example.org/base" || cfg["hardFail"] != true {
					t.Fatalf("cfg after {} = %v/%v, want kept", cfg["url"], cfg["hardFail"])
				}
				if row.ContentTTLSeconds != 3600 {
					t.Fatalf("row content ttl = %d, want kept 3600", row.ContentTTLSeconds)
				}
			},
		},
		{
			name: "credential null clears username, password row untouched (single-sided)",
			body: `{"username":null}`,
			check: func(t *testing.T, cfg map[string]any, row, before *metadata.RemoteConfig) {
				if _, ok := cfg["username"]; ok {
					t.Fatalf("canonical echo still carries username %q", cfg["username"])
				}
				if row.Username != "" {
					t.Fatalf("row username = %q, want cleared", row.Username)
				}
				if row.Password != before.Password {
					t.Fatalf("row password changed on a username-only update (%q -> %q)", before.Password, row.Password)
				}
			},
		},
		{
			name: "credential empty string clears password, username untouched (single-sided)",
			body: `{"password":""}`,
			check: func(t *testing.T, _ map[string]any, row, before *metadata.RemoteConfig) {
				if row.Password != "" {
					t.Fatalf("row password = %q, want cleared", row.Password)
				}
				if row.Username != before.Username {
					t.Fatalf("row username changed on a password-only update (%q -> %q)", before.Username, row.Username)
				}
			},
		},
		{
			name: "credential write lands, the other seat kept",
			body: `{"username":"bot"}`,
			check: func(t *testing.T, cfg map[string]any, row, before *metadata.RemoteConfig) {
				if cfg["username"] != "bot" || row.Username != "bot" {
					t.Fatalf("username = %v/%q, want written", cfg["username"], row.Username)
				}
				if row.Password != before.Password {
					t.Fatalf("row password changed on a username-only update")
				}
			},
		},
		{
			name: "period explicit 0 stores 0 (update face)",
			body: `{"retrievalCachePeriodSecs":0,"missedRetrievalCachePeriodSecs":0,` +
				`"metadataRetrievalCachePeriodSecs":0}`,
			check: func(t *testing.T, cfg map[string]any, row, _ *metadata.RemoteConfig) {
				for k := range map[string]bool{
					"retrievalCachePeriodSecs": true, "missedRetrievalCachePeriodSecs": true,
					"metadataRetrievalCachePeriodSecs": true,
				} {
					if cfg[k] != float64(0) {
						t.Fatalf("cfg[%s] = %v, want the explicit 0", k, cfg[k])
					}
				}
				if row.ContentTTLSeconds != 0 || row.MetadataTTLSeconds != 0 {
					t.Fatalf("row ttl = %d/%d, want 0/0", row.ContentTTLSeconds, row.MetadataTTLSeconds)
				}
			},
		},
		{
			name:     "period negative refuses",
			body:     `{"retrievalCachePeriodSecs":-1}`,
			refusals: refusals{substr: "must not be negative"},
		},
		{
			name:     "alias disagreement refuses on update",
			body:     `{"missedRetrievalCachePeriodSecs":60,"missRetrievalCachePeriodSecs":90}`,
			refusals: refusals{substr: "disagree"},
		},
		{
			name: "object omit keeps the whole family",
			body: `{"hardFail":false}`,
			check: func(t *testing.T, cfg map[string]any, _, _ *metadata.RemoteConfig) {
				cs := l008csOf(t, cfg)
				if !cs["enabled"].(bool) || !cs["statisticsEnabled"].(bool) ||
					!cs["propertiesEnabled"].(bool) || !cs["sourceOrigin"].(bool) {
					t.Fatalf("contentSynchronisation = %v, want the stored family", cs)
				}
			},
		},
		{
			name: "object null keeps the whole family (probe A15)",
			body: `{"contentSynchronisation":null}`,
			check: func(t *testing.T, cfg map[string]any, _, _ *metadata.RemoteConfig) {
				cs := l008csOf(t, cfg)
				if !cs["statisticsEnabled"].(bool) || !cs["propertiesEnabled"].(bool) {
					t.Fatalf("contentSynchronisation after null = %v, want kept", cs)
				}
			},
		},
		{
			name: "object empty resets the whole family (probe A7)",
			body: `{"contentSynchronisation":{}}`,
			check: func(t *testing.T, cfg map[string]any, _, _ *metadata.RemoteConfig) {
				cs := l008csOf(t, cfg)
				for k, v := range cs {
					if v != false {
						t.Fatalf("contentSynchronisation[%s] = %v after {}, want false", k, v)
					}
				}
			},
		},
		{
			name: "object partial REPLACES: unmentioned sub-keys reset (probe A14)",
			body: `{"contentSynchronisation":{"statisticsEnabled":true}}`,
			check: func(t *testing.T, cfg map[string]any, _, _ *metadata.RemoteConfig) {
				cs := l008csOf(t, cfg)
				if cs["statisticsEnabled"] != true {
					t.Fatalf("statisticsEnabled = %v, want the written true", cs["statisticsEnabled"])
				}
				if cs["enabled"] != false || cs["propertiesEnabled"] != false || cs["sourceOrigin"] != false {
					t.Fatalf("unmentioned sub-keys = %v/%v/%v, want all reset false",
						cs["enabled"], cs["propertiesEnabled"], cs["sourceOrigin"])
				}
			},
		},
		{
			name: "object explicit write replaces sub-keywise",
			body: `{"contentSynchronisation":{"enabled":true,"sourceOrigin":true}}`,
			check: func(t *testing.T, cfg map[string]any, _, _ *metadata.RemoteConfig) {
				cs := l008csOf(t, cfg)
				if cs["enabled"] != true || cs["sourceOrigin"] != true {
					t.Fatalf("written sub-keys = %v/%v, want true/true", cs["enabled"], cs["sourceOrigin"])
				}
				if cs["statisticsEnabled"] != false || cs["propertiesEnabled"] != false {
					t.Fatalf("unmentioned sub-keys = %v/%v, want false/false", cs["statisticsEnabled"], cs["propertiesEnabled"])
				}
			},
		},
		{
			name:     "url blank refuses on update too",
			body:     `{"url":""}`,
			refusals: refusals{substr: "url is required"},
		},
		{
			name: "url write re-validates and lands",
			body: `{"url":"https://new.example.org/x/"}`,
			check: func(t *testing.T, cfg map[string]any, row, _ *metadata.RemoteConfig) {
				if cfg["url"] != "https://new.example.org/x" {
					t.Fatalf("url = %v, want the rewritten value (trailing slash trimmed)", cfg["url"])
				}
				if row.URL != "https://new.example.org/x" {
					t.Fatalf("row url = %q, want the rewritten value", row.URL)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The credentials master key makes the sealed password
			// observable (enc:v1), so the keep-vs-clear arms assert the
			// real row, not the WARN-dropped "".
			t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY",
				base64.StdEncoding.EncodeToString(bytes32(0x33)))
			e := newEnv(t)
			mustCreateRemote(t, e, "m-remote", l008SeedConfig)
			before := l008RemoteRow(t, e, "m-remote")
			if !strings.HasPrefix(before.Password, "enc:v1:") {
				t.Fatalf("seeded row password = %q, want the sealed form", before.Password)
			}
			err := l008UpdateRemote(t, e, "m-remote", tt.body)
			if tt.substr != "" {
				if !errors.Is(err, repo.ErrInvalidRepoConfig) || !strings.Contains(err.Error(), tt.substr) {
					t.Fatalf("error = %v, want ErrInvalidRepoConfig naming %q", err, tt.substr)
				}
				return
			}
			if err != nil {
				t.Fatalf("update: %v", err)
			}
			got, gerr := e.svc.GetRepo(context.Background(), admin(), "m-remote")
			if gerr != nil {
				t.Fatalf("GetRepo: %v", gerr)
			}
			tt.check(t, remoteCfgOf(t, got), l008RemoteRow(t, e, "m-remote"), before)
		})
	}
}

// refusals is the refusal arm of the matrix table.
type refusals struct {
	substr string
}

// l008csOf extracts the canonical contentSynchronisation object.
func l008csOf(t *testing.T, cfg map[string]any) map[string]any {
	t.Helper()
	cs, ok := cfg["contentSynchronisation"].(map[string]any)
	if !ok {
		t.Fatalf("canonical config carries no contentSynchronisation object: %v", cfg)
	}
	return cs
}

// TestRemoteUpdateCredentialKeepSealedRow: the full three-column matrix on
// the password alone — omit keeps the sealed row byte-for-byte, an explicit
// value reseals, and the username never rides along.
func TestRemoteUpdateCredentialKeepSealedRow(t *testing.T) {
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY",
		base64.StdEncoding.EncodeToString(bytes32(0x44)))
	e := newEnv(t)
	mustCreateRemote(t, e, "cred-remote", l008SeedConfig)
	before := l008RemoteRow(t, e, "cred-remote")

	// Omit: hardFail-only update, the sealed password survives verbatim.
	if err := l008UpdateRemote(t, e, "cred-remote", `{"hardFail":false}`); err != nil {
		t.Fatalf("omit update: %v", err)
	}
	if after := l008RemoteRow(t, e, "cred-remote"); after.Password != before.Password {
		t.Fatalf("sealed password changed on omit (%q -> %q)", before.Password, after.Password)
	}

	// Explicit value: resealed (a different ciphertext of the new secret).
	if err := l008UpdateRemote(t, e, "cred-remote", `{"password":"rotated"}`); err != nil {
		t.Fatalf("rotate update: %v", err)
	}
	after := l008RemoteRow(t, e, "cred-remote")
	if after.Password == before.Password || !strings.HasPrefix(after.Password, "enc:v1:") {
		t.Fatalf("rotated password = %q, want a fresh sealed form", after.Password)
	}
}

// bytes32 builds one 32-byte test key.
func bytes32(fill byte) []byte {
	b := make([]byte, 32)
	for i := range b {
		b[i] = fill
	}
	return b
}

// TestRemoteExplicitZeroAcrossAliasSpellings: Review B2 — the explicit-0
// semantics must be UNIFORM across every alias position after ADR-0050
// decision 3 (the pre-fix resolver swallowed a position-b zero: an explicit
// missRetrievalCachePeriodSecs:0 stored 0 while missRetrieval…Secs:0 fell
// back to the default, and BOTH socket spellings' zeros were unwrapped to
// absent). Live evidence (:8082, L008-1b probe): the reference stores
// socketTimeoutMillis:0 verbatim on create AND update, and the 0 WINS over
// a co-sent socketTimeoutSecs (millis:0+secs:30 lands 0); socketTimeoutMs
// and socketTimeoutSecs are not reference fields, so their behavior follows
// the ADR's uniform literal. Both faces, every spelling, one rule: 0 is 0.
func TestRemoteExplicitZeroAcrossAliasSpellings(t *testing.T) {
	type seat struct {
		bodyKey string
		echoKey string
	}
	seats := []seat{
		{"socketTimeoutMillis", "socketTimeoutMillis"},
		{"socketTimeoutMs", "socketTimeoutMillis"}, // canonicalized away
		{"missedRetrievalCachePeriodSecs", "missedRetrievalCachePeriodSecs"},
		{"missRetrievalCachePeriodSecs", "missedRetrievalCachePeriodSecs"}, // alias of the seat above
		{"socketTimeoutSecs", "socketTimeoutMillis"},                       // legacy M3 field, ms mirror
	}
	t.Run("create face", func(t *testing.T) {
		for _, s := range seats {
			t.Run(s.bodyKey, func(t *testing.T) {
				e := newEnv(t)
				mustCreateRemote(t, e, "zero-alias",
					fmt.Sprintf(`{"url":"http://u","%s":0}`, s.bodyKey))
				cfg := remoteCfgOf(t, mustGetRepo(t, e, "zero-alias"))
				if cfg[s.echoKey] != float64(0) {
					t.Fatalf("echo[%s] after %s:0 = %v, want the explicit 0", s.echoKey, s.bodyKey, cfg[s.echoKey])
				}
			})
		}
	})
	t.Run("update face", func(t *testing.T) {
		for _, s := range seats {
			t.Run(s.bodyKey, func(t *testing.T) {
				e := newEnv(t)
				mustCreateRemote(t, e, "zero-alias",
					`{"url":"http://u","socketTimeoutMillis":30000,"missedRetrievalCachePeriodSecs":7200}`)
				if err := l008UpdateRemote(t, e, "zero-alias",
					fmt.Sprintf(`{"%s":0}`, s.bodyKey)); err != nil {
					t.Fatalf("update with %s:0: %v", s.bodyKey, err)
				}
				cfg := remoteCfgOf(t, mustGetRepo(t, e, "zero-alias"))
				if cfg[s.echoKey] != float64(0) {
					t.Fatalf("echo[%s] after %s:0 update = %v, want the explicit 0", s.echoKey, s.bodyKey, cfg[s.echoKey])
				}
			})
		}
	})
	t.Run("alias yield needs both spellings", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRemote(t, e, "zero-alias", `{"url":"http://u","socketTimeoutMs":0,"socketTimeoutMillis":800}`)
		cfg := remoteCfgOf(t, mustGetRepo(t, e, "zero-alias"))
		if cfg["socketTimeoutMillis"] != float64(800) {
			t.Fatalf("yield arm = %v, want 800 (a zero side yields only when BOTH spellings are given)", cfg["socketTimeoutMillis"])
		}
	})
	t.Run("explicit millis 0 wins over socketTimeoutSecs (probe parity)", func(t *testing.T) {
		e := newEnv(t)
		mustCreateRemote(t, e, "zero-alias", `{"url":"http://u","socketTimeoutMillis":0,"socketTimeoutSecs":30}`)
		cfg := remoteCfgOf(t, mustGetRepo(t, e, "zero-alias"))
		if cfg["socketTimeoutMillis"] != float64(0) {
			t.Fatalf("precedence arm = %v, want 0 (the reference lands 0 for this body)", cfg["socketTimeoutMillis"])
		}
	})
}

// remoteGetConfigFailingMD is the Review B1 store decorator: every
// Remote().GetConfig answers the injected error (create never calls it, so
// the failure arms exactly the credential-keep read of UpdateRepo).
type remoteGetConfigFailingMD struct {
	metadata.Store
	fail error
}

type remoteGetConfigFailing struct {
	metadata.RemoteStore
	fail error
}

func (r remoteGetConfigFailing) GetConfig(context.Context, string) (*metadata.RemoteConfig, error) {
	return nil, r.fail
}

func (m *remoteGetConfigFailingMD) Remote() metadata.RemoteStore {
	return remoteGetConfigFailing{m.Store.Remote(), m.fail}
}

// TestRemoteUpdateCredentialKeepSurfacesGetConfigFailure: Review B1 — a
// transient GetConfig failure in the credential-keep arm must REFUSE the
// update; the pre-fix code swallowed every error as "no row to keep" and
// the UpdateConfig below then overwrote the stored sealed password with ""
// (silent credential loss, the exact loss ADR-0050 decision 4 prevents).
func TestRemoteUpdateCredentialKeepSurfacesGetConfigFailure(t *testing.T) {
	t.Setenv("BINFLOW_REMOTE_CREDENTIALS_KEY",
		base64.StdEncoding.EncodeToString(bytes32(0x55)))
	ctx := context.Background()
	var raw metadata.Store
	e := newEnvCustom(t, func(md metadata.Store) metadata.Store {
		raw = md
		return &remoteGetConfigFailingMD{Store: md, fail: errors.New("transient db failure")}
	})
	mustCreateRemote(t, e, "b1-remote", l008SeedConfig)
	before, err := raw.Remote().GetConfig(ctx, "b1-remote")
	if err != nil {
		t.Fatalf("raw GetConfig: %v", err)
	}

	err = l008UpdateRemote(t, e, "b1-remote", `{"hardFail":true}`)
	if err == nil || !strings.Contains(err.Error(), "transient db failure") {
		t.Fatalf("update under GetConfig failure = %v, want the surfaced store error", err)
	}

	// The remote_configs row is untouched: the sealed password (and every
	// other column) survived the refused update verbatim.
	after, err := raw.Remote().GetConfig(ctx, "b1-remote")
	if err != nil {
		t.Fatalf("raw GetConfig after: %v", err)
	}
	if after.Password != before.Password {
		t.Fatalf("sealed password changed by the REFUSED update (%q -> %q)", before.Password, after.Password)
	}
	if after.URL != before.URL || after.Username != before.Username ||
		after.ContentTTLSeconds != before.ContentTTLSeconds ||
		after.SocketTimeoutMs != before.SocketTimeoutMs {
		t.Fatalf("remote_configs row changed by the refused update: %+v -> %+v", before, after)
	}
}
