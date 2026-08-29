package repo_test

// T-290 (FR-90.2, LC-12 / L25): the smart remote effective-field subset at
// the repository-model layer —
//
//	socketTimeoutMs / socketTimeoutMillis   (ms-granularity IO timeout)
//	metadataRetrievalTimeoutSecs            (per-repo metadata wait cap)
//	missRetrievalCachePeriodSecs            (PRD alias of missedRetrieval…)
//	unusedArtifactsCleanupPeriodHours       (P2 field-only; engine M11)
//
// and the M11 pair (enableTokenAuthentication / contentSynchronisation) —
// refused by name through M10 (the no-inert-fields posture), ACCEPTED and
// effective since T-317 (FR-101.1). Every rule lives in repo.Service/
// config.go; this file pins acceptance, canonical echo, the 014 row mirror
// and the refusal family.
//
// T-346 (FR-113.1, the T-290-2 carryover): the canonical echo spelling is
// socketTimeoutMillis; socketTimeoutMs is an input-only alias that the echo
// never carries. The input bodies below deliberately keep BOTH spellings so
// the alias acceptance and the canonical flip are each pinned on their own.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
)

// TestT290SmartRemoteFieldRoundTrip: the FR-90.2 subset lands in the
// canonical config echo AND the remote_configs row (the 014 mirror), with
// every spelling the wire accepts.
func TestT290SmartRemoteFieldRoundTrip(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "tuned-remote", `{"url":"https://up.example.org",`+
		`"socketTimeoutMs":2500,"metadataRetrievalTimeoutSecs":30,`+
		`"missRetrievalCachePeriodSecs":45,"unusedArtifactsCleanupPeriodHours":72}`)

	row, err := e.md.Remote().GetConfig(ctx, "tuned-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.SocketTimeoutMs != 2500 {
		t.Fatalf("row socket_timeout_ms = %d, want 2500", row.SocketTimeoutMs)
	}
	if row.MetadataRetrievalTimeoutSecs != 30 {
		t.Fatalf("row metadata_retrieval_timeout_secs = %d, want 30", row.MetadataRetrievalTimeoutSecs)
	}
	if row.UnusedCleanupPeriodHours != 72 {
		t.Fatalf("row unused_cleanup_period_hours = %d, want 72", row.UnusedCleanupPeriodHours)
	}

	got, err := e.svc.GetRepo(ctx, admin(), "tuned-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	cfg := remoteCfgOf(t, got)
	for k, v := range map[string]any{
		"socketTimeoutMillis":               float64(2500),
		"socketTimeoutSecs":                 float64(3), // ceil of 2500ms — never over-reports
		"metadataRetrievalTimeoutSecs":      float64(30),
		"missedRetrievalCachePeriodSecs":    float64(45), // canonical spelling carries the alias's value
		"unusedArtifactsCleanupPeriodHours": float64(72),
	} {
		if cfg[k] != v {
			t.Fatalf("config[%s] = %v (%T), want %v", k, cfg[k], cfg[k], v)
		}
	}
	// FR-113.1: the old PRD spelling was accepted as INPUT (the body above)
	// but the echo never carries it — canonicalized away.
	if _, ok := cfg["socketTimeoutMs"]; ok {
		t.Fatalf("echo carries the input-only alias socketTimeoutMs: %v", cfg)
	}
}

// TestT290SmartRemoteDefaults: a body with just the URL materializes the
// product defaults — socketTimeoutMillis 15000 (the M3 15s, spelled in the
// canonical xsd form since FR-113.1), metadataRetrievalTimeoutSecs 60,
// unusedArtifactsCleanupPeriodHours 0 (off — repo-semantics 7.1).
func TestT290SmartRemoteDefaults(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "default-remote", `{"url":"http://u"}`)

	got, err := e.svc.GetRepo(ctx, admin(), "default-remote")
	if err != nil {
		t.Fatalf("GetRepo: %v", err)
	}
	cfg := remoteCfgOf(t, got)
	for k, v := range map[string]any{
		"socketTimeoutMillis":               float64(15000),
		"socketTimeoutSecs":                 float64(15),
		"metadataRetrievalTimeoutSecs":      float64(60),
		"unusedArtifactsCleanupPeriodHours": float64(0),
	} {
		if cfg[k] != v {
			t.Fatalf("config[%s] = %v, want %v", k, cfg[k], v)
		}
	}
	row, err := e.md.Remote().GetConfig(ctx, "default-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.SocketTimeoutMs != 15000 || row.MetadataRetrievalTimeoutSecs != 60 || row.UnusedCleanupPeriodHours != 0 {
		t.Fatalf("row tuning fields = %d/%d/%d, want 15000/60/0",
			row.SocketTimeoutMs, row.MetadataRetrievalTimeoutSecs, row.UnusedCleanupPeriodHours)
	}
}

// TestT290SocketTimeoutSpellings: either ms spelling is accepted on input
// (socketTimeoutMillis canonical since FR-113.1, socketTimeoutMs the legacy
// PRD alias); either ms spelling wins over the legacy socketTimeoutSecs
// (only ms can express sub-second timeouts); agreement between the ms
// aliases round-trips; the echo always carries the canonical spelling only.
func TestT290SocketTimeoutSpellings(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "xsd-remote", `{"url":"http://u","socketTimeoutMillis":800}`)
	cfg := remoteCfgOf(t, mustGetRepo(t, e, "xsd-remote"))
	if cfg["socketTimeoutMillis"] != float64(800) {
		t.Fatalf("xsd spelling: socketTimeoutMillis = %v, want 800", cfg["socketTimeoutMillis"])
	}
	if _, ok := cfg["socketTimeoutMs"]; ok {
		t.Fatalf("xsd spelling: echo carries the alias socketTimeoutMs: %v", cfg)
	}
	if cfg["socketTimeoutSecs"] != float64(1) {
		t.Fatalf("xsd spelling: socketTimeoutSecs = %v, want 1 (ceil of 800ms)", cfg["socketTimeoutSecs"])
	}

	// ms wins over secs when both are present (documented precedence); the
	// legacy PRD spelling is the input here, the canonical one the echo.
	mustCreateRemote(t, e, "mixed-remote", `{"url":"http://u","socketTimeoutSecs":30,"socketTimeoutMs":1500}`)
	cfg = remoteCfgOf(t, mustGetRepo(t, e, "mixed-remote"))
	if cfg["socketTimeoutMillis"] != float64(1500) || cfg["socketTimeoutSecs"] != float64(2) {
		t.Fatalf("mixed spellings: ms=%v secs=%v, want 1500/2", cfg["socketTimeoutMillis"], cfg["socketTimeoutSecs"])
	}

	// secs-only input keeps the M3 exact-second round-trip.
	mustCreateRemote(t, e, "secs-remote", `{"url":"http://u","socketTimeoutSecs":30}`)
	cfg = remoteCfgOf(t, mustGetRepo(t, e, "secs-remote"))
	if cfg["socketTimeoutMillis"] != float64(30000) || cfg["socketTimeoutSecs"] != float64(30) {
		t.Fatalf("secs-only: ms=%v secs=%v, want 30000/30", cfg["socketTimeoutMillis"], cfg["socketTimeoutSecs"])
	}

	// A zero ms spelling yields back to the legacy secs field (the fetcher's
	// fallback order reads a zero column the same way — review minor 1).
	mustCreateRemote(t, e, "zero-ms-remote", `{"url":"http://u","socketTimeoutMs":0,"socketTimeoutSecs":30}`)
	cfg = remoteCfgOf(t, mustGetRepo(t, e, "zero-ms-remote"))
	if cfg["socketTimeoutMillis"] != float64(30000) || cfg["socketTimeoutSecs"] != float64(30) {
		t.Fatalf("zero-ms + secs: ms=%v secs=%v, want 30000/30", cfg["socketTimeoutMillis"], cfg["socketTimeoutSecs"])
	}

	// Alias zero-vs-value resolves to the value (not a disagreement).
	mustCreateRemote(t, e, "zero-alias-remote", `{"url":"http://u","socketTimeoutMs":0,"socketTimeoutMillis":2500}`)
	cfg = remoteCfgOf(t, mustGetRepo(t, e, "zero-alias-remote"))
	if cfg["socketTimeoutMillis"] != float64(2500) || cfg["socketTimeoutSecs"] != float64(3) {
		t.Fatalf("0+2500 alias: ms=%v secs=%v, want 2500/3", cfg["socketTimeoutMillis"], cfg["socketTimeoutSecs"])
	}
	_ = ctx
}

// mustGetRepo is the GetRepo helper of this file.
func mustGetRepo(t *testing.T, e *env, key string) *metadata.Repo {
	t.Helper()
	got, err := e.svc.GetRepo(context.Background(), admin(), key)
	if err != nil {
		t.Fatalf("GetRepo(%s): %v", key, err)
	}
	return got
}

// TestT290SmartRemoteValidation: the refusal family — alias disagreement,
// negative values — every arm a 400-shaped ErrInvalidRepoConfig naming the
// field.
func TestT290SmartRemoteValidation(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantSubstr string
	}{
		{"ms aliases disagree", `{"url":"http://u","socketTimeoutMs":1500,"socketTimeoutMillis":2500}`, "disagree"},
		{"miss aliases disagree", `{"url":"http://u","missedRetrievalCachePeriodSecs":60,"missRetrievalCachePeriodSecs":90}`, "disagree"},
		{"miss aliases agree", `{"url":"http://u","missedRetrievalCachePeriodSecs":60,"missRetrievalCachePeriodSecs":60}`, ""},
		{"negative socketTimeoutMs", `{"url":"http://u","socketTimeoutMs":-1}`, "must not be negative"},
		{"negative metadataRetrievalTimeoutSecs", `{"url":"http://u","metadataRetrievalTimeoutSecs":-60}`, "must not be negative"},
		{"negative unusedCleanupPeriodHours", `{"url":"http://u","unusedArtifactsCleanupPeriodHours":-24}`, "must not be negative"},
		{"zero keeps defaults", `{"url":"http://u","socketTimeoutMs":0,"metadataRetrievalTimeoutSecs":0}`, ""},
		// Review minor 1: an explicit 0 counts as ABSENT in alias
		// comparison (aligned with the fetcher's 0=unset consumption).
		{"ms zero + xsd value resolves to value", `{"url":"http://u","socketTimeoutMs":0,"socketTimeoutMillis":800}`, ""},
		{"xsd zero + ms value resolves to value", `{"url":"http://u","socketTimeoutMillis":0,"socketTimeoutMs":2500}`, ""},
		{"ms double zero keeps default", `{"url":"http://u","socketTimeoutMs":0,"socketTimeoutMillis":0}`, ""},
		{"miss alias zero + value resolves to value", `{"url":"http://u","missedRetrievalCachePeriodSecs":0,"missRetrievalCachePeriodSecs":90}`, ""},
		{"miss alias value + zero resolves to value", `{"url":"http://u","missRetrievalCachePeriodSecs":0,"missedRetrievalCachePeriodSecs":60}`, ""},
		{"non-zero disagreement still refuses", `{"url":"http://u","socketTimeoutMs":1500,"socketTimeoutMillis":2500}`, "disagree"},
		// Review minor 2 (T-290) + T-317 inversion: trailing garbage is
		// malformed JSON — the strict single-value gate must not be
		// bypassable by appending junk after the object (the M11 field now
		// legal inside it), and a clean blob keeps parsing.
		{"trailing garbage after M11 field refuses", `{"url":"http://u","contentSynchronisation":{"enabled":true}}garbage`, "trailing data"},
		{"trailing garbage on a clean blob refuses", `{"url":"http://u"}garbage`, "trailing data"},
		{"second JSON value refuses", `{"url":"http://u"}{"url":"http://u"}`, "trailing data"},
		{"trailing whitespace is fine", "{\"url\":\"http://u\"}  \n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
				RepoKey: "t290-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
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
				t.Fatalf("error %q does not contain %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestT290M11FieldsAccepted (inverted by T-317 / FR-101.1): the M10-era
// by-name 400 of enableTokenAuthentication / contentSynchronisation is
// RETIRED — both fields are accepted, effective (consumed by internal/
// remote's repoPolicy) and echoed canonically. The no-inert-fields rule of
// FR-90.2 stays satisfied by their consumers, not by refusal.
func TestT290M11FieldsAccepted(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "m11-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://u","enableTokenAuthentication":true,` +
			`"contentSynchronisation":{"enabled":true,"propertiesEnabled":true}}`,
	})
	if err != nil {
		t.Fatalf("create with both fields: %v", err)
	}
	cfg := remoteCfgOf(t, mustGetRepo(t, e, "m11-remote"))
	if cfg["enableTokenAuthentication"] != true {
		t.Fatalf("enableTokenAuthentication = %v, want true", cfg["enableTokenAuthentication"])
	}
	cs, ok := cfg["contentSynchronisation"].(map[string]any)
	if !ok {
		t.Fatalf("contentSynchronisation = %v (%T), want an object", cfg["contentSynchronisation"], cfg["contentSynchronisation"])
	}
	for k, v := range map[string]any{
		"enabled": true, "statisticsEnabled": false, "propertiesEnabled": true, "sourceOrigin": false,
	} {
		if cs[k] != v {
			t.Fatalf("contentSynchronisation[%s] = %v, want %v", k, cs[k], v)
		}
	}
	// A null value is ABSENT (the explicit-null-is-default read every other
	// nullable field gets), and the scenario-D tolerance arm (M3 contract)
	// still passes.
	e2 := newEnv(t)
	if _, err := e2.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "m11-null", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://u","contentSynchronisation":null}`,
	}); err != nil {
		t.Fatalf("null contentSynchronisation refused: %v", err)
	}
	e3 := newEnv(t)
	if _, err := e3.svc.CreateRepo(context.Background(), admin(), &metadata.Repo{
		RepoKey: "scen-d", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://u","proxyRef":"","shareConfiguration":false,"maxUniqueSnapshots":5}`,
	}); err != nil {
		t.Fatalf("scenario-D tolerance broken: %v", err)
	}
}

// TestT290UpdateReplacesTuningFields: PUT-style update carries the tuning
// fields (full-replace semantics — the M3 T-80 ruling) and heals the row
// mirror.
func TestT290UpdateReplacesTuningFields(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	mustCreateRemote(t, e, "upd-remote", `{"url":"http://u","socketTimeoutMs":9000,"metadataRetrievalTimeoutSecs":20,`+
		`"unusedArtifactsCleanupPeriodHours":48,"missRetrievalCachePeriodSecs":120}`)
	if _, err := e.svc.UpdateRepo(ctx, admin(), &metadata.Repo{
		RepoKey: "upd-remote", Type: repo.TypeRemote, PackageType: repo.PackageGeneric,
		Config: `{"url":"http://u2","socketTimeoutMs":4000,"metadataRetrievalTimeoutSecs":10,` +
			`"unusedArtifactsCleanupPeriodHours":0,"missRetrievalCachePeriodSecs":60}`,
	}); err != nil {
		t.Fatalf("UpdateRepo: %v", err)
	}
	row, err := e.md.Remote().GetConfig(ctx, "upd-remote")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if row.URL != "http://u2" || row.SocketTimeoutMs != 4000 ||
		row.MetadataRetrievalTimeoutSecs != 10 || row.UnusedCleanupPeriodHours != 0 {
		t.Fatalf("row after update = %+v, want url u2 / 4000 / 10 / 0", row)
	}
	cfg := remoteCfgOf(t, mustGetRepo(t, e, "upd-remote"))
	if cfg["missedRetrievalCachePeriodSecs"] != float64(60) {
		t.Fatalf("missed period after update = %v, want 60", cfg["missedRetrievalCachePeriodSecs"])
	}
}
