package httpapi_test

// T-368's REST legs (FR-118): the two operator knobs switched from
// hard-coded to config-driven. Three families live here:
//
//	echo    GET /api/v1/system/settings — the knob echo face: defaults,
//	        custom values, and the role door (anonymous 401 / plain 403 /
//	        readonly_admin 200 / admin 200, the addons-plane posture).
//	wire    the config plane actually flips the folder download — the M12
//	        L14 assertion SWITCHED: the default-off 403 stays verbatim
//	        (the red line: an unconfigured boot is byte-for-byte M12),
//	        and the on-state arms (200 + zip sha256 reconciliation,
//	        anonymous sub-switch, the over-limit 400) now spell their
//	        input as binflow.yaml values.
//	pin     the loader defaults equal the consumer defaults — the
//	        default-unchanged red line as a compile-checked equality
//	        (config's numbers cannot drift from repo's spec column).

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/adapter"
	"github.com/lzwzzy/binflow/internal/adapter/generic"
	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/httpapi"
	"github.com/lzwzzy/binflow/internal/license"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/storage"
)

// t368FolderFromConfig is the cmd assembly mapping — the exact literal
// the conductor's main.go diff carries (architecture section 11.45: "one
// cmd assembly line"). It lives in the test as the pinned spelling: if
// the config plane or the service shape renames a field, this mapping
// breaks the build instead of silently wiring a zero.
func t368FolderFromConfig(c config.FolderDownloadConfig) repo.FolderDownloadConfig {
	return repo.FolderDownloadConfig{
		Enabled:                 c.Enabled,
		EnabledForAnonymous:     c.EnabledForAnonymous,
		MaxDownloadSizeMb:       c.MaxDownloadSizeMb,
		MaxFiles:                c.MaxFiles,
		MaxConcurrentRequests:   c.MaxConcurrentRequests,
		EnabledEmptyDirectories: c.EnabledEmptyDirectories,
	}
}

// t368Stack is the t343 assembly (license plane + archive family) plus
// the two things T-368 adds: a mutated config.Config as the single knob
// source, and the readonly_admin/plain users the echo's role legs need.
type t368Stack struct {
	t343Stack
	cfg *config.Config
}

func newT368Stack(t *testing.T, mutate func(*config.Config)) *t368Stack {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := storage.OpenEngine(dataDir, storage.Options{})
	if err != nil {
		t.Fatalf("storage.OpenEngine: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	md, err := metadata.Open(ctx, metadata.Options{Driver: "sqlite", Path: dataDir + "/binflow.db"})
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = md.Close() })

	cfg := config.Defaults()
	cfg.Storage.DataDir = dataDir
	cfg.Security.AnonymousAccess = false
	if mutate != nil {
		mutate(cfg)
	}
	authSvc := auth.NewFromStore(md, cfg.Security.AnonymousAccess)
	seedLicenseUser(t, md, "roat", "roat-pw", "readonly_admin")
	seedLicenseUser(t, md, "plain", "plain-pw", "user")
	svc := repo.New(st, md, authSvc, audit.New(md, true))

	// THE knob consumption point (the cmd wiring, exercised here end to
	// end): the resolved config drives the service's folder-download
	// plane.
	repo.ConfigureFolderDownload(svc, t368FolderFromConfig(cfg.FolderDownload))

	k := newTestKeys(t)
	mgr, err := license.New(license.Options{
		Store:      md.Licenses(),
		VerifyKeys: k.keys,
		Audit:      audit.BestEffort(audit.New(md, true)),
	})
	if err != nil {
		t.Fatalf("license.New: %v", err)
	}
	if err := mgr.Load(ctx); err != nil {
		t.Fatalf("license Load: %v", err)
	}

	s := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Auth:     authSvc,
		Authz:    authSvc,
		Metadata: md,
		Repos:    md.Repos(),
		ReposSvc: svc,
		License:  mgr,
		Addons:   productionManifest(),
		Adapters: []adapter.Handler{generic.New(svc, md.Blobs())},
	}, nil)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &t368Stack{t343Stack: t343Stack{ts: ts, md: md, svc: svc, mgr: mgr, keys: k}, cfg: cfg}
}

// ---- the echo face ----

// TestT368SystemSettingsEcho: the knob echo answers the resolved config
// — the spec defaults on an unconfigured boot, the operator's values
// when spelled, and the role door of the /api/v1 system plane.
func TestT368SystemSettingsEcho(t *testing.T) {
	const p = "/binflow/api/v1/system/settings"

	t.Run("defaults echo the M12 as-built", func(t *testing.T) {
		st := newT368Stack(t, nil)
		code, body := st.do(http.MethodGet, p, adminUser, adminPass, "")
		if code != http.StatusOK {
			t.Fatalf("GET = %d %s", code, body)
		}
		var got struct {
			FolderDownload struct {
				Enabled                 bool  `json:"enabled"`
				EnabledForAnonymous     bool  `json:"enabled_for_anonymous"`
				MaxDownloadSizeMb       int64 `json:"max_download_size_mb"`
				MaxFiles                int   `json:"max_files"`
				MaxConcurrentRequests   int   `json:"max_concurrent_requests"`
				EnabledEmptyDirectories bool  `json:"enabled_empty_directories"`
			} `json:"folder_download"`
			Trashcan struct {
				RetentionDays int `json:"retention_days"`
			} `json:"trashcan"`
		}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("body %q: %v", body, err)
		}
		fd := got.FolderDownload
		if fd.Enabled || fd.EnabledForAnonymous || fd.EnabledEmptyDirectories {
			t.Fatalf("switches = %+v, want all off (the section 2.1 column)", fd)
		}
		if fd.MaxDownloadSizeMb != 1024 || fd.MaxFiles != 5000 || fd.MaxConcurrentRequests != 10 {
			t.Fatalf("limits = %+v, want 1024/5000/10", fd)
		}
		if got.Trashcan.RetentionDays != 14 {
			t.Fatalf("retention_days = %d, want the M12 value 14", got.Trashcan.RetentionDays)
		}
	})

	t.Run("operator values echo verbatim", func(t *testing.T) {
		st := newT368Stack(t, func(c *config.Config) {
			c.FolderDownload = config.FolderDownloadConfig{
				Enabled: true, EnabledForAnonymous: true,
				MaxDownloadSizeMb: 512, MaxFiles: 40, MaxConcurrentRequests: 2,
				EnabledEmptyDirectories: true,
			}
			c.Trashcan.RetentionDays = 7
		})
		code, body := st.do(http.MethodGet, p, adminUser, adminPass, "")
		if code != http.StatusOK {
			t.Fatalf("GET = %d %s", code, body)
		}
		for _, want := range []string{
			`"enabled": true`,
			`"enabled_for_anonymous": true`,
			`"max_download_size_mb": 512`,
			`"max_files": 40`,
			`"max_concurrent_requests": 2`,
			`"enabled_empty_directories": true`,
			`"retention_days": 7`,
		} {
			if !bytes.Contains([]byte(body), []byte(want)) {
				t.Fatalf("body missing %s: %s", want, body)
			}
		}
	})

	t.Run("role door and read-only plane", func(t *testing.T) {
		st := newT368Stack(t, nil)
		if code, _ := st.do(http.MethodGet, p, "", "", ""); code != http.StatusUnauthorized {
			t.Fatalf("anonymous GET = %d, want 401", code)
		}
		if code, _ := st.do(http.MethodGet, p, "plain", "plain-pw", ""); code != http.StatusForbidden {
			t.Fatalf("plain user GET = %d, want 403", code)
		}
		if code, body := st.do(http.MethodGet, p, "roat", "roat-pw", ""); code != http.StatusOK {
			t.Fatalf("readonly_admin GET = %d %s, want 200", code, body)
		}
		// The write verbs have no route: restart-effective file knobs,
		// not REST-editable state (the E-26 404).
		if code, _ := st.do(http.MethodPut, p, adminUser, adminPass, "{}"); code != http.StatusNotFound {
			t.Fatalf("PUT = %d, want the E-26 404", code)
		}
		if code, _ := st.do(http.MethodPost, p, adminUser, adminPass, "{}"); code != http.StatusNotFound {
			t.Fatalf("POST = %d, want the E-26 404", code)
		}
	})
}

// ---- the wire face: the L14 assertion, switched ----

// TestT368FolderDownloadKnobSwitch: the config plane flips the folder
// download. The M12 L14 baseline (default off, the verbatim 403) is
// asserted UNCHANGED — the red line — and the on-state arms now arrive
// through the knob (PRD section 5.4's registered assertion switch).
func TestT368FolderDownloadKnobSwitch(t *testing.T) {
	enabled := func(extra func(*config.Config)) func(*config.Config) {
		return func(c *config.Config) {
			c.FolderDownload.Enabled = true
			if extra != nil {
				extra(c)
			}
		}
	}

	t.Run("unconfigured boot keeps the M12 403 verbatim", func(t *testing.T) {
		st := newT368Stack(t, nil)
		st.installPro(t)
		st.createRepo(t, "lib")
		st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		code, body, _ := st.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib?archiveType=zip", adminUser, adminPass, nil, nil)
		if code != http.StatusForbidden || !strings.Contains(envelopeMsg(t, body),
			"Download Folder functionality is disabled.") {
			t.Fatalf("default-off = %d %s, want the verbatim step-6 403", code, body)
		}
	})

	t.Run("knob on answers 200 and the zip reconciles", func(t *testing.T) {
		st := newT368Stack(t, enabled(nil))
		st.installPro(t)
		st.createRepo(t, "lib")
		st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		st.putContent(t, "lib", "d/sub/b.bin", []byte("beta-bytes"))

		// The echo and the enforced behavior agree (one Config behind
		// both).
		code, body, _ := st.doBytes(http.MethodGet,
			"/binflow/api/v1/system/settings", adminUser, adminPass, nil, nil)
		if code != http.StatusOK || !bytes.Contains(body, []byte(`"enabled": true`)) {
			t.Fatalf("echo = %d %s", code, body)
		}

		code, raw, hdr := st.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib/d?archiveType=zip", adminUser, adminPass, nil, nil)
		if code != http.StatusOK {
			t.Fatalf("folder download = %d %s", code, raw)
		}
		if got := hdr.Get("Content-Type"); got != "application/zip" {
			t.Fatalf("content type = %q", got)
		}
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatalf("body is not a zip: %v", err)
		}
		got := map[string]string{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			_ = rc.Close()
			got[f.Name] = string(b)
		}
		if got["d/a.txt"] != "alpha-bytes" || got["d/sub/b.bin"] != "beta-bytes" {
			t.Fatalf("round trip = %v", got)
		}
	})

	t.Run("anonymous sub-switch gates the 401 wording", func(t *testing.T) {
		// Off (the spec default): anonymous answers the step-1 401 with
		// its own wording even with the master switch on.
		st := newT368Stack(t, enabled(nil))
		st.installPro(t)
		st.createRepo(t, "lib")
		st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		code, body, _ := st.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib?archiveType=zip", "", "", nil, nil)
		if code != http.StatusUnauthorized || !strings.Contains(envelopeMsg(t, body),
			"You must be logged in to download a folder or repository.") {
			t.Fatalf("anonymous off = %d %s, want the verbatim step-1 401", code, body)
		}

		// On: the anonymous download itself succeeds — on an instance
		// where anonymous content reads are allowed at all (ADR-0009's
		// security.anonymous_access; the sub-switch cannot open a repo
		// the instance keeps closed, §2.2 step 5 stays the judge).
		st2 := newT368Stack(t, func(c *config.Config) {
			c.FolderDownload.Enabled = true
			c.FolderDownload.EnabledForAnonymous = true
			c.Security.AnonymousAccess = true
		})
		st2.installPro(t)
		st2.createRepo(t, "lib")
		st2.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		code, raw, _ := st2.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib?archiveType=zip", "", "", nil, nil)
		if code != http.StatusOK {
			t.Fatalf("anonymous on = %d %s", code, raw)
		}
	})

	t.Run("knob-spelled limits reject over-limit requests", func(t *testing.T) {
		st := newT368Stack(t, enabled(func(c *config.Config) {
			c.FolderDownload.MaxFiles = 1
		}))
		st.installPro(t)
		st.createRepo(t, "lib")
		st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		st.putContent(t, "lib", "d/b.txt", []byte("beta-bytes"))
		code, body, _ := st.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib/d?archiveType=zip", adminUser, adminPass, nil, nil)
		if code != http.StatusBadRequest || !strings.Contains(envelopeMsg(t, body),
			"Number of files under the path 'lib/d' (2) exceeds the max allowed file count for folder download (1).") {
			t.Fatalf("max_files arm = %d %s, want the verbatim step-7 400", code, body)
		}
	})

	t.Run("empty-directories switch rides the knob", func(t *testing.T) {
		st := newT368Stack(t, enabled(func(c *config.Config) {
			c.FolderDownload.EnabledEmptyDirectories = true
		}))
		st.installPro(t)
		st.createRepo(t, "lib")
		st.putContent(t, "lib", "d/a.txt", []byte("alpha-bytes"))
		// Seed an empty folder row directly (uploads materialize
		// ancestors only when they carry files — the repo-side fixture
		// precedent).
		if err := st.md.Nodes().Put(context.Background(), &metadata.Node{
			RepoKey: "lib", Path: "d/hollow/", Sha256: metadata.FolderMarkerSHA,
		}); err != nil {
			t.Fatalf("seed empty folder: %v", err)
		}
		code, raw, _ := st.doBytes(http.MethodGet,
			"/binflow/api/archive/download/lib?archiveType=zip", adminUser, adminPass, nil, nil)
		if code != http.StatusOK {
			t.Fatalf("folder download = %d %s", code, raw)
		}
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatalf("body is not a zip: %v", err)
		}
		var emptyDir bool
		for _, f := range zr.File {
			if f.Name == "d/hollow/" {
				emptyDir = true
			}
		}
		if !emptyDir {
			t.Fatalf("enabled_empty_directories did not carry the empty d/hollow/ entry")
		}
	})
}

// TestT368DefaultsEqualConsumerDefaults: the default-unchanged red line
// as an equality — the loader's defaults ARE the consumer's spec column
// (config cannot import repo, so the pin lives here where both are
// visible). A drift on either side fails this test, not a deployment.
func TestT368DefaultsEqualConsumerDefaults(t *testing.T) {
	cfg := config.Defaults()
	// Through the cmd mapping: the mapped loader default IS the service's
	// spec column, field by field.
	if got := t368FolderFromConfig(cfg.FolderDownload); got != repo.DefaultFolderDownloadConfig() {
		t.Fatalf("mapped config defaults %+v != repo spec column %+v", got, repo.DefaultFolderDownloadConfig())
	}
	if cfg.Trashcan.RetentionDays != repo.TrashDefaultRetentionDays {
		t.Fatalf("config retention default = %d, want repo's %d (the M12 as-built)",
			cfg.Trashcan.RetentionDays, repo.TrashDefaultRetentionDays)
	}
}
