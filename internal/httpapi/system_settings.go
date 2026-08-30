package httpapi

// GET /binflow/api/v1/system/settings — the operator-knob echo face
// (M13 T-368, PRD FR-118): the resolved values of the two YAML knob
// families this milestone switched on, folder_download (the six-field
// directory-zip family of repo-operations.md section 2.1) and
// trashcan.retention_days. The web form face is deliberately LATER
// (the ticket scopes to the server-side REST arm); this endpoint is
// what the future form and the docs will sit on.
//
// Scope rulings:
//
//   - KNOB-SCOPED, not a config dump: the body carries only the behavior
//     knobs operators flip at runtime. It must never grow secrets, DSNs
//     or file paths — the addons/license posture of "honest, minimal,
//     read-only" is the design ceiling (adding the next knob family is
//     additive; adding a credentials-bearing field is a review stop).
//   - The echo is the RESOLVED CONFIG (YAML + env + defaults), the same
//     source the cmd assembly pushes onto the consumers
//     (repo.ConfigureFolderDownload / NewTrashEngine's RetentionDays), so
//     the echo and the enforced behavior cannot disagree on a real
//     assembly — both read one Config.
//   - Read-only: GET is the only verb with a route; everything else
//     falls to the E-26 404 (the addons-plane precedent — these are
//     file-level restart-effective knobs, not REST-editable state).
//   - Restart-effective like the whole config file: a knob change is a
//     restart, the §2.1 hot-reload clause being a REGISTERED divergence
//     recorded on the ConfigureFolderDownload seam.

import (
	"encoding/json"
	"net/http"

	"github.com/lzwzzy/binflow/internal/config"
)

// folderDownloadSettings is the folder_download echo object: the six
// fields in the YAML spellings the config section owns.
type folderDownloadSettings struct {
	Enabled                 bool  `json:"enabled"`
	EnabledForAnonymous     bool  `json:"enabled_for_anonymous"`
	MaxDownloadSizeMb       int64 `json:"max_download_size_mb"`
	MaxFiles                int   `json:"max_files"`
	MaxConcurrentRequests   int   `json:"max_concurrent_requests"`
	EnabledEmptyDirectories bool  `json:"enabled_empty_directories"`
}

// trashcanSettings is the trashcan echo object: the retention window the
// purge cron consumes.
type trashcanSettings struct {
	RetentionDays int `json:"retention_days"`
}

// systemSettings is the GET response body.
type systemSettings struct {
	FolderDownload folderDownloadSettings `json:"folder_download"`
	Trashcan       trashcanSettings       `json:"trashcan"`
}

// handleSystemSettings serves GET /binflow/api/v1/system/settings. The
// route gate already demanded an authenticated system:read principal
// (readonly_admin sees the matrix, a plain user 403s — the /api/v1/health
// and /api/v1/addons posture).
func (s *Server) handleSystemSettings(w http.ResponseWriter, _ *http.Request) {
	body, err := json.MarshalIndent(settingsFromConfig(s.deps.Config), "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "render settings response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// settingsFromConfig maps the resolved Config onto the echo shape — the
// one spelling of the mapping (the handler and the tests share it, so a
// drift between echo and expectation is a compile error).
func settingsFromConfig(c *config.Config) systemSettings {
	return systemSettings{
		FolderDownload: folderDownloadSettings{
			Enabled:                 c.FolderDownload.Enabled,
			EnabledForAnonymous:     c.FolderDownload.EnabledForAnonymous,
			MaxDownloadSizeMb:       c.FolderDownload.MaxDownloadSizeMb,
			MaxFiles:                c.FolderDownload.MaxFiles,
			MaxConcurrentRequests:   c.FolderDownload.MaxConcurrentRequests,
			EnabledEmptyDirectories: c.FolderDownload.EnabledEmptyDirectories,
		},
		Trashcan: trashcanSettings{RetentionDays: c.Trashcan.RetentionDays},
	}
}
