package config

import "testing"

// T-368 (FR-118): the two operator knobs — folder_download (the six-field
// family of repo-operations.md section 2.1) and trashcan.retention_days.
// The T-366 dead-key lesson is the shape of every test here: the raw
// section, the apply-env arm and the default are THREE separate places,
// and a knob that lands in only two of them still loads green while doing
// nothing — so each leg pins all three forms (YAML, env, default).

// TestFolderDownloadKnobDefaults: an unconfigured boot resolves the
// section 2.1 spec column exactly — the default-unchanged red line. The
// M12 as-built never wired a config plane; these are the numbers the
// service's own DefaultFolderDownloadConfig ships, so the knob adds a
// way to spell the posture without moving it.
func TestFolderDownloadKnobDefaults(t *testing.T) {
	c := mustLoad(t, "", nil)
	want := FolderDownloadConfig{
		Enabled:                 false,
		EnabledForAnonymous:     false,
		MaxDownloadSizeMb:       1024,
		MaxFiles:                5000,
		MaxConcurrentRequests:   10,
		EnabledEmptyDirectories: false,
	}
	if c.FolderDownload != want {
		t.Fatalf("FolderDownload = %+v, want the section 2.1 column %+v", c.FolderDownload, want)
	}
}

// TestFolderDownloadKnobResolution: every field resolves from YAML, from
// the generic double-underscore env form, and env wins over YAML — the
// replication/webhook section precedent, table-driven over the six
// fields.
func TestFolderDownloadKnobResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want FolderDownloadConfig
	}{
		{
			name: "full yaml section",
			yaml: "folder_download:\n" +
				"  enabled: true\n" +
				"  enabled_for_anonymous: true\n" +
				"  max_download_size_mb: 512\n" +
				"  max_files: 40\n" +
				"  max_concurrent_requests: 2\n" +
				"  enabled_empty_directories: true\n",
			want: FolderDownloadConfig{
				Enabled: true, EnabledForAnonymous: true,
				MaxDownloadSizeMb: 512, MaxFiles: 40, MaxConcurrentRequests: 2,
				EnabledEmptyDirectories: true,
			},
		},
		{
			name: "partial yaml keeps the other defaults",
			yaml: "folder_download:\n  enabled: true\n",
			want: FolderDownloadConfig{
				Enabled:               true,
				MaxDownloadSizeMb:     1024,
				MaxFiles:              5000,
				MaxConcurrentRequests: 10,
			},
		},
		{
			name: "yaml zero limit is unlimited not default",
			yaml: "folder_download:\n  enabled: true\n  max_files: 0\n",
			want: FolderDownloadConfig{
				Enabled: true, MaxDownloadSizeMb: 1024,
				MaxFiles: 0, MaxConcurrentRequests: 10,
			},
		},
		{
			name: "env switch on",
			env:  map[string]string{"BINFLOW_FOLDER_DOWNLOAD__ENABLED": "true"},
			want: FolderDownloadConfig{
				Enabled:           true,
				MaxDownloadSizeMb: 1024, MaxFiles: 5000, MaxConcurrentRequests: 10,
			},
		},
		{
			name: "env switch spelled on",
			env:  map[string]string{"BINFLOW_FOLDER_DOWNLOAD__ENABLED": "on"},
			want: FolderDownloadConfig{
				Enabled:           true,
				MaxDownloadSizeMb: 1024, MaxFiles: 5000, MaxConcurrentRequests: 10,
			},
		},
		{
			name: "env anonymous sub-switch",
			env: map[string]string{
				"BINFLOW_FOLDER_DOWNLOAD__ENABLED":                   "true",
				"BINFLOW_FOLDER_DOWNLOAD__ENABLED_FOR_ANONYMOUS":     "true",
				"BINFLOW_FOLDER_DOWNLOAD__ENABLED_EMPTY_DIRECTORIES": "1",
			},
			want: FolderDownloadConfig{
				Enabled: true, EnabledForAnonymous: true, EnabledEmptyDirectories: true,
				MaxDownloadSizeMb: 1024, MaxFiles: 5000, MaxConcurrentRequests: 10,
			},
		},
		{
			name: "env limits",
			env: map[string]string{
				"BINFLOW_FOLDER_DOWNLOAD__MAX_DOWNLOAD_SIZE_MB":    "2048",
				"BINFLOW_FOLDER_DOWNLOAD__MAX_FILES":               "60",
				"BINFLOW_FOLDER_DOWNLOAD__MAX_CONCURRENT_REQUESTS": "3",
			},
			want: FolderDownloadConfig{
				MaxDownloadSizeMb: 2048, MaxFiles: 60, MaxConcurrentRequests: 3,
			},
		},
		{
			name: "env wins over yaml",
			yaml: "folder_download:\n  enabled: true\n  max_files: 40\n",
			env: map[string]string{
				"BINFLOW_FOLDER_DOWNLOAD__ENABLED":   "false",
				"BINFLOW_FOLDER_DOWNLOAD__MAX_FILES": "7",
			},
			want: FolderDownloadConfig{
				MaxDownloadSizeMb: 1024, MaxFiles: 7, MaxConcurrentRequests: 10,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustLoad(t, tc.yaml, tc.env)
			if c.FolderDownload != tc.want {
				t.Fatalf("FolderDownload = %+v, want %+v", c.FolderDownload, tc.want)
			}
		})
	}
}

// TestFolderDownloadEnvRejectsNonPositive: the int keys demand a positive
// integer like every env int — 0 (= unlimited) is a YAML-only spelling
// (the auth.hash_concurrency rule).
func TestFolderDownloadEnvRejectsNonPositive(t *testing.T) {
	for _, kv := range []struct{ key, value string }{
		{"BINFLOW_FOLDER_DOWNLOAD__MAX_FILES", "0"},
		{"BINFLOW_FOLDER_DOWNLOAD__MAX_DOWNLOAD_SIZE_MB", "-5"},
		{"BINFLOW_FOLDER_DOWNLOAD__MAX_CONCURRENT_REQUESTS", "x"},
	} {
		if _, err := loadWithEnv(t, "", map[string]string{kv.key: kv.value}); err == nil {
			t.Fatalf("%s=%s loaded, want a positive-integer refusal", kv.key, kv.value)
		}
	}
}

// TestFolderDownloadSectionStrictSchema: unknown keys inside the section
// are rejected — including the Artifactory camelCase spellings, so a key
// copied from an Artifactory config.xml fails fast with the schema error
// instead of silently not applying (the dead-key posture).
func TestFolderDownloadSectionStrictSchema(t *testing.T) {
	for _, body := range []string{
		"folder_download:\n  enabled: true\n  bogus: 1\n",
		"folder_download:\n  maxDownloadSizeMb: 512\n",
		"folder_download:\n  enabledForAnonymous: true\n",
	} {
		if _, err := loadWithEnv(t, body, nil); err == nil {
			t.Fatalf("Load succeeded on %q, want the strict-schema error", body)
		}
	}
}

// TestFolderDownloadNegativeRefusesBoot: a negative limit is a typo
// nobody chose — Validate refuses the boot instead of clamping.
func TestFolderDownloadNegativeRefusesBoot(t *testing.T) {
	for _, body := range []string{
		"folder_download:\n  max_download_size_mb: -1\n",
		"folder_download:\n  max_files: -3\n",
		"folder_download:\n  max_concurrent_requests: -2\n",
	} {
		if _, err := loadWithEnv(t, body, nil); err == nil {
			t.Fatalf("Load succeeded on %q, want the negative-value refusal", body)
		}
	}
}

// TestTrashcanRetentionDaysResolution: the retention window resolves from
// YAML and env, env wins, and the default is the M12 as-built 14 — the
// value the purge cron ran on before the knob existed.
func TestTrashcanRetentionDaysResolution(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want int
	}{
		{name: "absent keeps the M12 value 14", want: 14},
		{name: "yaml 7", yaml: "trashcan:\n  retention_days: 7\n", want: 7},
		{name: "yaml 1 short window", yaml: "trashcan:\n  retention_days: 1\n", want: 1},
		{
			name: "yaml 0 is the default sentinel",
			yaml: "trashcan:\n  retention_days: 0\n",
			want: 0, // the engine maps <= 0 onto its spec default (14)
		},
		{
			name: "env override",
			env:  map[string]string{"BINFLOW_TRASHCAN__RETENTION_DAYS": "30"},
			want: 30,
		},
		{
			name: "env wins over yaml",
			yaml: "trashcan:\n  retention_days: 7\n",
			env:  map[string]string{"BINFLOW_TRASHCAN__RETENTION_DAYS": "2"},
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustLoad(t, tc.yaml, tc.env)
			if c.Trashcan.RetentionDays != tc.want {
				t.Fatalf("Trashcan.RetentionDays = %d, want %d", c.Trashcan.RetentionDays, tc.want)
			}
		})
	}
}

// TestTrashcanSectionStrictSchema: retention_days is the section's only
// key — the capture switch is not a config field (it rides the trashcan
// license slot), so an enabled key is a schema error, not a silent drop.
func TestTrashcanSectionStrictSchema(t *testing.T) {
	if _, err := loadWithEnv(t, "trashcan:\n  enabled: false\n", nil); err == nil {
		t.Fatal("Load succeeded with trashcan.enabled, want the strict-schema error")
	}
	if _, err := loadWithEnv(t, "trashcan:\n  retention_days: -1\n", nil); err == nil {
		t.Fatal("Load succeeded with a negative retention_days, want the refusal")
	}
}
