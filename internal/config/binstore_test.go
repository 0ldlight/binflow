package config

// T-306 (FR-93 / ADR-0036) tests: the binstore.yaml chain schema matrix,
// the Q5 three-branch coexistence rules, discovery order, and the four
// fail-fast shapes (bad file with path+line, plaintext secret, reserved
// provider name, illegal combination).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeMainAndBinstore materializes a binflow.yaml (always with a tempdir
// data_dir, so Validate's filesystem probe stays inside the test sandbox)
// and, when binstore != "", a binstore.yaml next to it; it returns the main
// file's path.
func writeMainAndBinstore(t *testing.T, main, binstore string) string {
	t.Helper()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "binflow.yaml")
	main = "storage:\n  data_dir: " + filepath.Join(dir, "data") + "\n" + main
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatalf("writing binflow.yaml: %v", err)
	}
	if binstore != "" {
		if err := os.WriteFile(filepath.Join(dir, BinstoreFileName), []byte(binstore), 0o600); err != nil {
			t.Fatalf("writing binstore.yaml: %v", err)
		}
	}
	return mainPath
}

func loadBoth(t *testing.T, main, binstore string) (*Config, error) {
	t.Helper()
	path := writeMainAndBinstore(t, main, binstore)
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

const s3MemberYAML = "  - type: s3\n" +
	"    bucket: bkt\n" +
	"    region: us-east-1\n" +
	"    endpoint: http://127.0.0.1:9000\n" +
	"    access_key_id: test-access-key\n" +
	"    use_path_style: true\n"

const s3ChainYAML = "version: 1\nchain:\n" + s3MemberYAML

// TestBinstoreThreeChainsResolve pins the three legal chains (plus the two
// migration sub-modes) against the resolved Config they must produce
// (ADR-0036 decision 3's mapping).
func TestBinstoreThreeChainsResolve(t *testing.T) {
	t.Setenv(S3SecretEnvVar, "env-secret")
	cases := []struct {
		name      string
		binstore  string
		backend   string
		migration MigrationConfig
		s3        S3Config
		providers []string
		mode      string
	}{
		{
			name:      "filestore single chain",
			binstore:  "version: 1\nchain:\n  - type: filestore\n",
			backend:   StorageBackendDisk,
			migration: MigrationConfig{Concurrency: DefaultMigrationConcurrency},
			s3:        S3Config{UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency},
			providers: []string{"filestore"},
		},
		{
			name:     "s3 single chain",
			binstore: s3ChainYAML,
			backend:  StorageBackendS3,
			migration: MigrationConfig{
				Concurrency: DefaultMigrationConcurrency,
			},
			s3: S3Config{
				Bucket: "bkt", Region: "us-east-1", Endpoint: "http://127.0.0.1:9000",
				AccessKeyID: "test-access-key", UsePathStyle: true,
				UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency,
			},
			providers: []string{"s3"},
		},
		{
			name: "dual-write chain",
			binstore: "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
				"migration:\n  mode: dual-write\n  concurrency: 7\n",
			backend:   StorageBackendS3,
			migration: MigrationConfig{Enabled: true, Completed: false, Concurrency: 7},
			s3: S3Config{
				Bucket: "bkt", Region: "us-east-1", Endpoint: "http://127.0.0.1:9000",
				AccessKeyID: "test-access-key", UsePathStyle: true,
				UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency,
			},
			providers: []string{"filestore", "s3"},
			mode:      MigrationModeDualWrite,
		},
		{
			name: "completed chain",
			binstore: "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
				"migration:\n  mode: completed\n",
			backend:   StorageBackendS3,
			migration: MigrationConfig{Enabled: true, Completed: true, Concurrency: DefaultMigrationConcurrency},
			s3: S3Config{
				Bucket: "bkt", Region: "us-east-1", Endpoint: "http://127.0.0.1:9000",
				AccessKeyID: "test-access-key", UsePathStyle: true,
				UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency,
			},
			providers: []string{"filestore", "s3"},
			mode:      MigrationModeCompleted,
		},
		{
			name: "bypass chain keeps disk live with s3 pre-declared",
			binstore: "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
				"migration:\n  mode: bypass\n",
			backend:   StorageBackendDisk,
			migration: MigrationConfig{Concurrency: DefaultMigrationConcurrency},
			s3: S3Config{
				Bucket: "bkt", Region: "us-east-1", Endpoint: "http://127.0.0.1:9000",
				AccessKeyID: "test-access-key", UsePathStyle: true,
				UploadPartSize: DefaultS3UploadPartSize, UploadConcurrency: DefaultS3UploadConcurrency,
			},
			providers: []string{"filestore", "s3"},
			mode:      MigrationModeBypass,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadBoth(t, "", tc.binstore)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Storage.Backend != tc.backend {
				t.Errorf("backend = %q, want %q", cfg.Storage.Backend, tc.backend)
			}
			if cfg.Storage.Migration != tc.migration {
				t.Errorf("migration = %+v, want %+v", cfg.Storage.Migration, tc.migration)
			}
			got := cfg.Storage.S3
			want := tc.s3
			want.SecretAccessKey = "env-secret" // the env escape hatch always reaches the S3 params
			if got != want {
				t.Errorf("s3 = %+v, want %+v", got, want)
			}
			if len(cfg.Storage.Chain.Providers) != len(tc.providers) {
				t.Fatalf("chain providers = %v, want %v", cfg.Storage.Chain.Providers, tc.providers)
			}
			for i := range tc.providers {
				if cfg.Storage.Chain.Providers[i] != tc.providers[i] {
					t.Errorf("chain providers = %v, want %v", cfg.Storage.Chain.Providers, tc.providers)
					break
				}
			}
			if cfg.Storage.Chain.Mode != tc.mode {
				t.Errorf("chain mode = %q, want %q", cfg.Storage.Chain.Mode, tc.mode)
			}
			if !strings.HasSuffix(cfg.Storage.Chain.Source, BinstoreFileName) {
				t.Errorf("chain source = %q, want the binstore.yaml path", cfg.Storage.Chain.Source)
			}
			if len(cfg.StartupWarnings) != 0 {
				t.Errorf("branch ② (embedded chain keys absent) must be silent, got warnings %v", cfg.StartupWarnings)
			}
		})
	}
}

// TestBinstoreSchemaFailFast walks the schema validation matrix: every row
// refuses the boot, and the error carries the file path, the offending line
// and the pointed key/message (ADR-0036 decision 7 — BinFlow's own error
// spelling; Artifactory's was never located, config-formats §1 C7).
func TestBinstoreSchemaFailFast(t *testing.T) {
	cases := []struct {
		name    string
		yamlDoc string
		want    []string
		wantOff string // "" = no line assertion
	}{
		{
			name:    "yaml syntax error",
			yamlDoc: "version: 1\nchain: [",
			want:    []string{"binstore.yaml", "line"},
		},
		{
			name:    "document is not a mapping",
			yamlDoc: "- type: filestore",
			want:    []string{"must be a YAML mapping", "line 1"},
		},
		{
			name:    "empty file",
			yamlDoc: "",
			want:    []string{"file is empty", "line 1"},
		},
		{
			name:    "null document",
			yamlDoc: "null",
			want:    []string{"file is empty"},
		},
		{
			name:    "unknown top-level key",
			yamlDoc: "version: 1\nbackend: disk\nchain:\n  - type: filestore\n",
			want:    []string{`unknown key "backend"`, "line 2"},
		},
		{
			name:    "duplicate top-level key",
			yamlDoc: "chain:\n  - type: filestore\nchain:\n  - type: s3\n",
			want:    []string{`duplicate key "chain"`, "line 3"},
		},
		{
			name:    "wrong version",
			yamlDoc: "version: 2\nchain:\n  - type: filestore\n",
			want:    []string{"version must be 1", "line 1"},
		},
		{
			name:    "version as string",
			yamlDoc: "version: \"1\"\nchain:\n  - type: filestore\n",
			want:    []string{"version must be 1"},
		},
		{
			name:    "chain missing",
			yamlDoc: "version: 1\n",
			want:    []string{"chain is required"},
		},
		{
			name:    "chain not a list",
			yamlDoc: "chain: filestore\n",
			want:    []string{"chain must be a YAML list", "line 1"},
		},
		{
			name:    "empty chain",
			yamlDoc: "chain: []\n",
			want:    []string{"at least one provider", "line 1"},
		},
		{
			name:    "entry missing type",
			yamlDoc: "chain:\n  - bucket: bkt\n",
			want:    []string{"missing its type key", "line 2"},
		},
		{
			name:    "reserved cache-fs refuses with honest message",
			yamlDoc: "chain:\n  - type: cache-fs\n",
			want:    []string{`"cache-fs"`, "reserved slot", "line 2"},
		},
		{
			name:    "reserved azure refuses",
			yamlDoc: "chain:\n  - type: azure\n",
			want:    []string{`"azure"`, "reserved slot"},
		},
		{
			name:    "reserved gs refuses",
			yamlDoc: "chain:\n  - type: gs\n",
			want:    []string{`"gs"`, "reserved slot"},
		},
		{
			name:    "unknown provider type",
			yamlDoc: "chain:\n  - type: glacier\n",
			want:    []string{`unknown provider type "glacier"`, "closed set: filestore, s3", "line 2"},
		},
		{
			name:    "filestore dir is a reserved extension slot",
			yamlDoc: "chain:\n  - type: filestore\n    dir: /data\n",
			want:    []string{"dir is a reserved extension slot", "line 3"},
		},
		{
			name:    "filestore takes no parameters",
			yamlDoc: "chain:\n  - type: filestore\n    bucket: bkt\n",
			want:    []string{"unknown filestore key", "line 3"},
		},
		{
			name:    "s3 unknown key",
			yamlDoc: "chain:\n  - type: s3\n    bucket: bkt\n    zone: us-east-1\n",
			want:    []string{`unknown s3 provider key "zone"`, "line 4"},
		},
		{
			name:    "s3 non-string param",
			yamlDoc: "chain:\n  - type: s3\n    bucket: 17\n",
			want:    []string{`key "bucket" must be a string`},
		},
		{
			name:    "s3 negative concurrency param",
			yamlDoc: "chain:\n  - type: s3\n    upload_concurrency: 0\n",
			want:    []string{"must be a positive integer"},
		},
		{
			name:    "duplicate provider",
			yamlDoc: "chain:\n  - type: filestore\n  - type: filestore\n",
			want:    []string{`"filestore" appears twice`, "line 3"},
		},
		{
			name:    "illegal order s3 then filestore",
			yamlDoc: "chain:\n  - type: s3\n    bucket: bkt\n  - type: filestore\nmigration:\n  mode: dual-write\n",
			want:    []string{"illegal chain order [s3, filestore]", "line 2"},
		},
		{
			name:    "dual chain without migration block",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\n",
			want:    []string{"requires a migration block", "line 2"},
		},
		{
			name:    "migration block on single filestore chain",
			yamlDoc: "chain:\n  - type: filestore\nmigration:\n  mode: bypass\n",
			want:    []string{"only legal on the [filestore, s3] chain", "cache-fs shape", "line 4"},
		},
		{
			name:    "migration block on single s3 chain",
			yamlDoc: "chain:\n  - type: s3\nmigration:\n  mode: dual-write\n",
			want:    []string{"only legal on the [filestore, s3] chain"},
		},
		{
			name:    "migration mode outside closed set",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\nmigration:\n  mode: full-dual\n",
			want:    []string{`migration.mode "full-dual" is not one of bypass, dual-write, completed`, "line 5"},
		},
		{
			name:    "migration mode missing",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\nmigration:\n  concurrency: 3\n",
			want:    []string{"migration.mode is required", "line 5"},
		},
		{
			name:    "migration concurrency zero",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\nmigration:\n  mode: bypass\n  concurrency: 0\n",
			want:    []string{"migration.concurrency must be a positive integer", "line 6"},
		},
		{
			name:    "migration concurrency non-int",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\nmigration:\n  mode: bypass\n  concurrency: many\n",
			want:    []string{"migration.concurrency must be a positive integer"},
		},
		{
			name:    "migration unknown key",
			yamlDoc: "chain:\n  - type: filestore\n  - type: s3\nmigration:\n  mode: bypass\n  batch: 10\n",
			want:    []string{`unknown migration key "batch"`, "line 6"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeMainAndBinstore(t, "", "x") // placeholder to create the dir
			dir := filepath.Dir(path)
			binstore := filepath.Join(dir, BinstoreFileName)
			if err := os.WriteFile(binstore, []byte(tc.yamlDoc), 0o600); err != nil {
				t.Fatalf("writing binstore.yaml: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load succeeded, want refusal for %q", tc.yamlDoc)
			}
			msg := err.Error()
			if !strings.Contains(msg, binstore) {
				t.Errorf("error does not name the binstore.yaml path %q: %s", binstore, msg)
			}
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q\n  does not contain %q", msg, want)
				}
			}
		})
	}
}

// TestBinstorePlaintextSecretRefused: the secret scan covers the whole file
// at any depth (ADR-0036 decision 4), names the line and points at the env
// escape hatch.
func TestBinstorePlaintextSecretRefused(t *testing.T) {
	cases := []struct {
		name    string
		yamlDoc string
		want    []string
	}{
		{
			name: "secret_access_key inside the s3 provider",
			yamlDoc: "version: 1\nchain:\n  - type: s3\n" +
				"    bucket: bkt\n    secret_access_key: hunter2\n",
			want: []string{"line 5", "secret_access_key", S3SecretEnvVar},
		},
		{
			name:    "secret-shaped key at the top level",
			yamlDoc: "version: 1\nadmin_password: x\nchain:\n  - type: filestore\n",
			want:    []string{"line 2", "admin_password", SecretEnvVar},
		},
		{
			name:    "secret-shaped key inside a provider",
			yamlDoc: "chain:\n  - type: filestore\n    password: x\n",
			want:    []string{"line 3", "password"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeMainAndBinstore(t, "", "x")
			dir := filepath.Dir(path)
			binstore := filepath.Join(dir, BinstoreFileName)
			if err := os.WriteFile(binstore, []byte(tc.yamlDoc), 0o600); err != nil {
				t.Fatalf("writing binstore.yaml: %v", err)
			}
			_, err := Load(path)
			if err == nil {
				t.Fatalf("Load succeeded, want refusal")
			}
			msg := err.Error()
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q\n  does not contain %q", msg, want)
				}
			}
		})
	}
}

// TestBinstoreQ5Branches pins ADR-0036 decision 5's three branches plus the
// divergence refusal.
func TestBinstoreQ5Branches(t *testing.T) {
	t.Setenv(S3SecretEnvVar, "env-secret")

	const embeddedS3 = "  backend: s3\n  s3:\n" +
		"    bucket: bkt\n    region: us-east-1\n    endpoint: http://127.0.0.1:9000\n" +
		"    access_key_id: test-access-key\n    use_path_style: true\n"

	t.Run("branch1 no binstore + embedded keys boots with warning", func(t *testing.T) {
		cfg, err := loadBoth(t, embeddedS3, "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Storage.Backend != StorageBackendS3 {
			t.Errorf("backend = %q, want s3 (embedded stays in effect)", cfg.Storage.Backend)
		}
		if !hasWarningContaining(cfg, "compatibility window") {
			t.Errorf("missing the branch-① migration hint, warnings = %v", cfg.StartupWarnings)
		}
		if cfg.Storage.Chain.Source != ChainSourceEmbedded || len(cfg.Storage.Chain.Providers) != 1 || cfg.Storage.Chain.Providers[0] != ProviderS3 {
			t.Errorf("chain label = %+v, want assembled [s3] from embedded", cfg.Storage.Chain)
		}
	})

	t.Run("branch1 env-only chain key warns too", func(t *testing.T) {
		t.Setenv("BINFLOW_STORAGE__BACKEND", "disk")
		cfg, err := loadBoth(t, "", "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !hasWarningContaining(cfg, "BINFLOW_STORAGE__BACKEND") {
			t.Errorf("missing the env-spelled branch-① hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("branch1 non-chain storage keys do not warn", func(t *testing.T) {
		cfg, err := loadBoth(t, "  session_ttl_hours: 12\n", "")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(cfg.StartupWarnings) != 0 {
			t.Errorf("data-plane keys must not trigger the hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("branch2 clean form is silent", func(t *testing.T) {
		cfg, err := loadBoth(t, "  gc_grace_hours: 48\n", "version: 1\nchain:\n  - type: filestore\n")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Storage.Backend != StorageBackendDisk {
			t.Errorf("backend = %q, want disk from binstore.yaml", cfg.Storage.Backend)
		}
		if len(cfg.StartupWarnings) != 0 {
			t.Errorf("branch ② must be silent, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("branch3 equivalent s3 file wins with warning", func(t *testing.T) {
		cfg, err := loadBoth(t, embeddedS3, s3ChainYAML)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Storage.S3.Bucket != "bkt" || cfg.Storage.Backend != StorageBackendS3 {
			t.Errorf("file must win: backend=%q bucket=%q", cfg.Storage.Backend, cfg.Storage.S3.Bucket)
		}
		if !hasWarningContaining(cfg, "equivalent storage chains") {
			t.Errorf("missing the branch-③ hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("branch3 equivalent dual-write", func(t *testing.T) {
		main := embeddedS3 + "  migration:\n    enabled: true\n"
		binstore := "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
			"migration:\n  mode: dual-write\n"
		cfg, err := loadBoth(t, main, binstore)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Storage.Migration.Enabled || cfg.Storage.Migration.Completed {
			t.Errorf("dual-write mapping wrong: %+v", cfg.Storage.Migration)
		}
		if !hasWarningContaining(cfg, "equivalent storage chains") {
			t.Errorf("missing the branch-③ hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("branch3 terminal equivalence completed vs plain s3", func(t *testing.T) {
		// ADR-0036 decision 3: "[s3] ≡ backend=s3, including the
		// post-migration terminal shape" — the completed chain and the plain
		// embedded s3 section assemble the same engine, so this is branch ③.
		binstore := "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
			"migration:\n  mode: completed\n"
		cfg, err := loadBoth(t, embeddedS3, binstore)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.Storage.Migration.Completed {
			t.Errorf("completed mode must carry into the config, got %+v", cfg.Storage.Migration)
		}
		if !hasWarningContaining(cfg, "equivalent storage chains") {
			t.Errorf("missing the branch-③ hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	t.Run("divergence backend refuses naming both files", func(t *testing.T) {
		path := writeMainAndBinstore(t, "  backend: disk\n", s3ChainYAML)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load succeeded, want divergence refusal")
		}
		msg := err.Error()
		for _, want := range []string{
			filepath.Join(filepath.Dir(path), BinstoreFileName),
			path,
			"assemble differently",
			"S3 provider is live",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q\n  does not contain %q", msg, want)
			}
		}
	})

	t.Run("divergence bucket value named", func(t *testing.T) {
		main := "  backend: s3\n  s3:\n    bucket: embedded-bkt\n    region: us-east-1\n" +
			"    endpoint: http://127.0.0.1:9000\n    access_key_id: k\n"
		path := writeMainAndBinstore(t, main, s3ChainYAML)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load succeeded, want divergence refusal")
		}
		msg := err.Error()
		for _, want := range []string{"storage.s3.bucket", "embedded-bkt", "bkt"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error %q\n  does not contain %q", msg, want)
			}
		}
	})

	t.Run("divergence mode armed on one side", func(t *testing.T) {
		main := embeddedS3 + "  migration:\n    enabled: true\n"
		path := writeMainAndBinstore(t, main, s3ChainYAML)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load succeeded, want divergence refusal")
		}
		if !strings.Contains(err.Error(), "dual-write migration mode") {
			t.Errorf("error does not name the mode divergence: %s", err)
		}
	})

	t.Run("divergence concurrency in dual-write", func(t *testing.T) {
		main := embeddedS3 + "  migration:\n    enabled: true\n    concurrency: 9\n"
		binstore := "version: 1\nchain:\n  - type: filestore\n" + s3MemberYAML +
			"migration:\n  mode: dual-write\n"
		path := writeMainAndBinstore(t, main, binstore)
		_, err := Load(path)
		if err == nil {
			t.Fatal("Load succeeded, want divergence refusal")
		}
		if !strings.Contains(err.Error(), "storage.migration.concurrency") {
			t.Errorf("error does not name the concurrency divergence: %s", err)
		}
	})

	t.Run("bypass equals disk with pre-declared s3 params", func(t *testing.T) {
		main := "  backend: disk\n  s3:\n    bucket: bkt\n"
		binstore := "version: 1\nchain:\n  - type: filestore\n  - type: s3\n" +
			"    bucket: bkt\nmigration:\n  mode: bypass\n"
		cfg, err := loadBoth(t, main, binstore)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Storage.Backend != StorageBackendDisk {
			t.Errorf("bypass must assemble disk, got %q", cfg.Storage.Backend)
		}
		if !hasWarningContaining(cfg, "equivalent storage chains") {
			t.Errorf("missing the branch-③ hint, warnings = %v", cfg.StartupWarnings)
		}
	})
}

func hasWarningContaining(cfg *Config, substr string) bool {
	for _, w := range cfg.StartupWarnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// TestBinstoreEnvIgnoredWhenFileOwnsChain (ADR-0036 decision 6): the
// chain-scoped env leftovers are skipped with a WARN — including values
// that would break validation if applied — while the secret env var keeps
// reaching the S3 params.
func TestBinstoreEnvIgnoredWhenFileOwnsChain(t *testing.T) {
	t.Setenv(S3SecretEnvVar, "env-secret")
	// backend=s3 without any S3 parameters would refuse the boot if the
	// env key were applied; the file's [filestore] chain must win instead.
	t.Setenv("BINFLOW_STORAGE__BACKEND", "s3")
	t.Setenv("BINFLOW_STORAGE__S3__BUCKET", "leaked-bucket")

	cfg, err := loadBoth(t, "", "version: 1\nchain:\n  - type: filestore\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.Backend != StorageBackendDisk {
		t.Errorf("backend = %q, want disk (the file owns the chain)", cfg.Storage.Backend)
	}
	if !hasWarningContaining(cfg, "BINFLOW_STORAGE__BACKEND is set but ignored") {
		t.Errorf("missing the ignore warning for BINFLOW_STORAGE__BACKEND, warnings = %v", cfg.StartupWarnings)
	}
	if !hasWarningContaining(cfg, "BINFLOW_STORAGE__S3__BUCKET is set but ignored") {
		t.Errorf("missing the ignore warning for BINFLOW_STORAGE__S3__BUCKET, warnings = %v", cfg.StartupWarnings)
	}
	if cfg.Storage.S3.SecretAccessKey != "env-secret" {
		t.Errorf("secret env must stay effective, got %q", cfg.Storage.S3.SecretAccessKey)
	}
}

// TestBinstorePermWarn: permissions wider than 0600 WARN (not refuse).
func TestBinstorePermWarn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are an approximation on Windows")
	}
	path := writeMainAndBinstore(t, "", "version: 1\nchain:\n  - type: filestore\n")
	binstore := filepath.Join(filepath.Dir(path), BinstoreFileName)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hasWarningContaining(cfg, "0600") {
		t.Errorf("0600 must not warn, warnings = %v", cfg.StartupWarnings)
	}

	if err := os.Chmod(binstore, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !hasWarningContaining(cfg, "wider than 0600") {
		t.Errorf("0644 must warn, warnings = %v", cfg.StartupWarnings)
	}
}

// TestBinstoreDiscoveryNextToMain: the lookup position is the EFFECTIVE
// main config's directory (ADR-0036 decision 1) — never the working
// directory, never $BINFLOW_HOME, when -c points elsewhere.
func TestBinstoreDiscoveryNextToMain(t *testing.T) {
	t.Setenv(S3SecretEnvVar, "env-secret")
	dirA := t.TempDir()
	mainA := filepath.Join(dirA, "binflow.yaml")
	if err := os.WriteFile(mainA, []byte("storage:\n  data_dir: "+filepath.Join(dirA, "data")+"\n"), 0o600); err != nil {
		t.Fatalf("writing main: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirA, BinstoreFileName), []byte(s3ChainYAML), 0o600); err != nil {
		t.Fatalf("writing binstore: %v", err)
	}

	cfg, err := Load(mainA)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.S3.Bucket != "bkt" {
		t.Errorf("bucket = %q, want the one declared next to the main file", cfg.Storage.S3.Bucket)
	}
	want := filepath.Join(dirA, BinstoreFileName)
	if cfg.Storage.Chain.Source != want {
		t.Errorf("chain source = %q, want %q", cfg.Storage.Chain.Source, want)
	}
}

// TestBuildDefaultConfig: the no-main-config boot still discovers
// binstore.yaml through the main config's own resolution order
// (./binstore.yaml, then $BINFLOW_HOME/binstore.yaml), and — since T-349
// (FR-113.5) — the discovery PRECEDES the env-chain validation: a present
// file owns the chain and its own errors, while an absent one leaves the
// env-only chain set to Validate's fail-fast.
func TestBuildDefaultConfig(t *testing.T) {
	t.Run("cwd binstore wins", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, BinstoreFileName),
			[]byte("version: 1\nchain:\n  - type: filestore\n"), 0o600); err != nil {
			t.Fatalf("writing binstore: %v", err)
		}
		t.Chdir(dir)
		cfg, err := BuildDefaultConfig("")
		if err != nil {
			t.Fatalf("BuildDefaultConfig: %v", err)
		}
		if cfg.Storage.Backend != StorageBackendDisk {
			t.Errorf("backend = %q, want disk", cfg.Storage.Backend)
		}
		if !strings.HasSuffix(cfg.Storage.Chain.Source, BinstoreFileName) {
			t.Errorf("chain source = %q", cfg.Storage.Chain.Source)
		}
	})

	t.Run("home fallback when cwd has none", func(t *testing.T) {
		t.Chdir(t.TempDir())
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, BinstoreFileName),
			[]byte("version: 1\nchain:\n  - type: filestore\n"), 0o600); err != nil {
			t.Fatalf("writing binstore: %v", err)
		}
		cfg, err := BuildDefaultConfig(home)
		if err != nil {
			t.Fatalf("BuildDefaultConfig: %v", err)
		}
		want := filepath.Join(home, BinstoreFileName)
		if cfg.Storage.Chain.Source != want {
			t.Errorf("chain source = %q, want %q", cfg.Storage.Chain.Source, want)
		}
	})

	t.Run("absent boots unchanged with env hint", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("BINFLOW_STORAGE__BACKEND", "disk")
		cfg, err := BuildDefaultConfig("")
		if err != nil {
			t.Fatalf("BuildDefaultConfig: %v", err)
		}
		if !hasWarningContaining(cfg, "compatibility window") {
			t.Errorf("missing the env-spelled branch-① hint, warnings = %v", cfg.StartupWarnings)
		}
	})

	// T-349 / FR-113.5 AC5: with no binstore.yaml anywhere, an incomplete
	// S3 env group refuses the boot AT LOAD TIME, naming the missing key.
	t.Run("absent + incomplete s3 env refuses naming the key", func(t *testing.T) {
		t.Chdir(t.TempDir())
		t.Setenv("BINFLOW_STORAGE__BACKEND", "s3")
		t.Setenv("BINFLOW_STORAGE__S3__BUCKET", "bkt")
		t.Setenv("BINFLOW_STORAGE__S3__REGION", "us-east-1")
		t.Setenv("BINFLOW_STORAGE__S3__ENDPOINT", "http://127.0.0.1:9000")
		t.Setenv(S3SecretEnvVar, "secret")
		// BINFLOW_STORAGE__S3__ACCESS_KEY_ID deliberately unset.
		_, err := BuildDefaultConfig("")
		if err == nil {
			t.Fatal("incomplete s3 env group booted, want the fail-fast refusal")
		}
		if !strings.Contains(err.Error(), "storage.s3.access_key_id is required when backend=s3") {
			t.Errorf("refusal %q does not name the missing key", err)
		}
	})

	// The T-325-registered inconsistency, closed: a PRESENT binstore.yaml
	// owns the chain, so the same incomplete env set is ignored + warned
	// (complete sets always were) instead of killing the boot — and the
	// file's own missing-parameter refusal fires through the final
	// Validate, exactly like the embedded spelling always did.
	t.Run("binstore owns chain; incomplete env ignored + warned", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, BinstoreFileName),
			[]byte("version: 1\nchain:\n  - type: filestore\n"), 0o600); err != nil {
			t.Fatalf("writing binstore: %v", err)
		}
		t.Chdir(dir)
		t.Setenv("BINFLOW_STORAGE__BACKEND", "s3")
		t.Setenv("BINFLOW_STORAGE__S3__BUCKET", "bkt")
		t.Setenv(S3SecretEnvVar, "secret")
		cfg, err := BuildDefaultConfig("")
		if err != nil {
			t.Fatalf("incomplete env chain set must not refuse when binstore.yaml owns the chain: %v", err)
		}
		if cfg.Storage.Backend != StorageBackendDisk {
			t.Errorf("backend = %q, want the file's disk", cfg.Storage.Backend)
		}
		if !hasWarningContaining(cfg, "BINFLOW_STORAGE__BACKEND is set but ignored") {
			t.Errorf("missing the ignored-env warning, warnings = %v", cfg.StartupWarnings)
		}
	})

	// The order pin (FR-113.5's 时序断言, as-built direction): a BROKEN
	// binstore.yaml and an incomplete env chain set in the same boot —
	// the file's own error wins, the env-spelled missing-secret complaint
	// never fires. The file owns the chain, so the file's diagnostics own
	// the boot.
	t.Run("broken binstore error outranks the env refusal", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, BinstoreFileName),
			[]byte("version: 1\nchain:\n  - type: cache-fs\n"), 0o600); err != nil {
			t.Fatalf("writing binstore: %v", err)
		}
		t.Chdir(dir)
		t.Setenv("BINFLOW_STORAGE__BACKEND", "s3")
		t.Setenv("BINFLOW_STORAGE__S3__BUCKET", "bkt")
		t.Setenv(S3SecretEnvVar, "secret")
		_, err := BuildDefaultConfig("")
		if err == nil {
			t.Fatal("broken binstore.yaml must refuse the boot")
		}
		if !strings.Contains(err.Error(), "cache-fs") || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("refusal %q is not the binstore.yaml error", err)
		}
		if strings.Contains(err.Error(), "access_key_id") {
			t.Errorf("refusal %q leaks the env-spelled complaint the file outranks", err)
		}
	})

	// The file owning the chain does NOT immunize a bad file-shaped chain:
	// an [s3] binstore.yaml with missing parameters refuses through the
	// final Validate — the binstore-semantic error, not an env one.
	t.Run("s3 binstore missing parameters refuses binstore-semantically", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, BinstoreFileName),
			[]byte("version: 1\nchain:\n  - type: s3\n    bucket: bkt\n"), 0o600); err != nil {
			t.Fatalf("writing binstore: %v", err)
		}
		t.Chdir(dir)
		t.Setenv(S3SecretEnvVar, "secret")
		_, err := BuildDefaultConfig("")
		if err == nil {
			t.Fatal("s3 binstore.yaml without region/endpoint/access_key_id must refuse")
		}
		if !strings.Contains(err.Error(), "storage.s3.") {
			t.Errorf("refusal %q does not name the s3 key", err)
		}
	})
}

// TestEmbeddedBootZeroChange: with no binstore.yaml anywhere and no chain
// keys declared, the boot is byte-for-byte the pre-T-306 shape — no
// warnings, disk backend, embedded chain label.
func TestEmbeddedBootZeroChange(t *testing.T) {
	cfg, err := loadBoth(t, "", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.StartupWarnings) != 0 {
		t.Errorf("warnings = %v, want none", cfg.StartupWarnings)
	}
	if cfg.Storage.Backend != DefaultStorageBackend {
		t.Errorf("backend = %q, want default %q", cfg.Storage.Backend, DefaultStorageBackend)
	}
	if cfg.Storage.Chain.Source != ChainSourceEmbedded ||
		len(cfg.Storage.Chain.Providers) != 1 || cfg.Storage.Chain.Providers[0] != ProviderFilestore ||
		cfg.Storage.Chain.Mode != "" {
		t.Errorf("chain label = %+v, want assembled [filestore] from embedded", cfg.Storage.Chain)
	}
}
