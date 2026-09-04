package httpapi

// The backup configuration plane (M16 T-450, FR-150.3 / ADR-0044 decision
// 7②): the /api/v1/system/backups CRUD over TWO stores — the 022 backups
// table carries the payload (enabled, the server path the artifacts land
// under) and the 021 schedules ledger carries the cron half under
// domain='backup' with the same key.
//
//	GET    /api/v1/system/backups        list (payload + cron projection)
//	PUT    /api/v1/system/backups        upsert, key from the body (the
//	                                    official single-PUT form, ADR 7②)
//	GET    /api/v1/system/backups/{key}  one backup's full state
//	PUT    /api/v1/system/backups/{key}  upsert-by-key (the path wins)
//	DELETE /api/v1/system/backups/{key}  drop payload AND schedule row
//
// Wire field names follow the ADR's soft-seam ⑦ set (backupKey, cronExp,
// nextBackupTime — the official backup-face spellings); the server-path
// field is exportPath. Artifactory descriptor fields without a BinFlow
// carrier (repository subsets — BinFlow's export is the whole-instance
// snapshot — plus incremental, retention rotation, zip archival and
// mail-on-error) are deliberately absent (022's registered gap).
//
// ADR-0015 erratum ②'s boundary is untouched: /api/export/** stays 404 and
// import stays CLI-only — this face CONFIGURES scheduled backups; it never
// runs or restores one interactively.

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/scheduler"
)

// backupBody is the create/update wire shape. cronExp empty = the backup
// keeps its payload row but is NOT scheduled (the ledger row is deleted —
// the single-state law); nextBackupTime is the Artifactory form's writable
// first-run stamp (RFC3339; must be in the future, ADR-0044 decision 9②;
// omitted = computed from the expression).
type backupBody struct {
	BackupKey      string `json:"backupKey"`
	Enabled        *bool  `json:"enabled"`
	CronExp        string `json:"cronExp"`
	NextBackupTime string `json:"nextBackupTime"`
	ExportPath     string `json:"exportPath"`
}

// backupResponse is the read shape: the payload row flattened with the
// cron projection. NextScheduleBackup is the ledger's next_run (” =
// unscheduled — the Artifactory column's empty state).
type backupResponse struct {
	BackupKey          string `json:"backupKey"`
	Enabled            bool   `json:"enabled"`
	ExportPath         string `json:"exportPath"`
	CronExp            string `json:"cronExp"`
	NextScheduleBackup string `json:"nextScheduleBackup"`
	LastRun            string `json:"lastRun"`
	LastStatus         string `json:"lastStatus"`
	LastError          string `json:"lastError"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

// validBackupKey is the backup key charset: the replication-name rule (1..64
// runes of [A-Za-z0-9._-] starting alphanumeric) — the key is one URL
// segment on the {key} routes and one ledger key; the closed charset blocks
// lookalike separators.
func validBackupKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for i, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// backupResponseOf renders one payload row plus its schedule projection.
func (s *Server) backupResponseOf(r *http.Request, b *metadata.Backup) backupResponse {
	resp := backupResponse{
		BackupKey:  b.Key,
		Enabled:    b.Enabled,
		ExportPath: b.ExportDir,
		CreatedAt:  b.CreatedAt,
		UpdatedAt:  b.UpdatedAt,
	}
	if row, err := s.readScheduleRow(r, scheduler.DomainBackup, b.Key); err == nil && row != nil {
		resp.CronExp = row.CronExpr
		if row.Enabled && row.CronExpr != "" {
			resp.NextScheduleBackup = row.NextRunAt
		}
		resp.LastRun = row.LastRunAt
		resp.LastStatus = row.LastStatus
		resp.LastError = row.LastError
	}
	return resp
}

// upsertBackup is the PUT kernel both PUT spellings share (body-key and
// path-key — the path wins when both carry one, the family's addressing
// law). It lands the payload row, then the schedule row (single-state law
// on the cron), then the backup.schedule.set word.
func (s *Server) upsertBackup(w http.ResponseWriter, r *http.Request, pathKey string) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "backup configuration requires an administrator account")
		return
	}
	var body backupBody
	if err := decodeJSONBodyOf(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := strings.TrimSpace(pathKey)
	if key == "" {
		key = strings.TrimSpace(body.BackupKey)
	}
	if !validBackupKey(key) {
		writeError(w, http.StatusBadRequest,
			"backupKey must be 1..64 characters of letters, digits, '.', '_' or '-', starting alphanumeric")
		return
	}
	exportPath := strings.TrimSpace(body.ExportPath)
	if exportPath == "" {
		// The export carrier needs a landing directory; refusing the
		// write beats a schedule that fails on every fire.
		writeError(w, http.StatusBadRequest, "exportPath is required (the server directory the backup artifacts land under)")
		return
	}
	if err := validExportPathShape(exportPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	// The cron arm validates BEFORE the payload row is written: a refused
	// expression must not half-land the backup (the same law the
	// writeScheduleCron landing below enforces, applied early so the 022
	// row only ever exists behind a storable schedule arm).
	if verr := validateCronArm(key, strings.TrimSpace(body.CronExp), strings.TrimSpace(body.NextBackupTime)); verr != nil {
		s.writeScheduleError(w, verr, "backups")
		return
	}

	// Payload row first (upsert keeps the creation stamp; the store's law).
	now := metadata.Now()
	row := &metadata.Backup{
		Key: key, Enabled: enabled, ExportDir: exportPath,
		CreatedAt: now, CreatedBy: p.Name, UpdatedAt: now, UpdatedBy: p.Name,
	}
	if cur, gerr := s.deps.Metadata.Backups().Get(r.Context(), key); gerr == nil {
		row.CreatedAt, row.CreatedBy = cur.CreatedAt, cur.CreatedBy
	} else if !errors.Is(gerr, metadata.ErrBackupNotFound) {
		writeError(w, http.StatusInternalServerError, "backups: reading: "+gerr.Error())
		return
	}
	if err := s.deps.Metadata.Backups().Put(r.Context(), row); err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "backups store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "backups: writing: "+err.Error())
		return
	}

	// The cron half: enabled mirrors the payload's bit so one switch parks
	// both stores; the empty expression deletes the ledger row.
	cleared, nextRun, err := s.writeScheduleCron(r, scheduler.DomainBackup, key,
		strings.TrimSpace(body.CronExp), enabled, strings.TrimSpace(body.NextBackupTime), p.Name)
	if err != nil {
		s.writeScheduleError(w, err, "backups")
		return
	}
	detail := auditDetail("key", key, "action", "cleared",
		"exportPath", exportPath, "enabled", boolText(enabled))
	if !cleared {
		detail = auditDetail("key", key, "action", "set",
			"cronExp", strings.TrimSpace(body.CronExp), "next_run", nextRun,
			"exportPath", exportPath, "enabled", boolText(enabled))
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: audit.ActionBackupScheduleSet,
		Detail: detail,
	})

	fresh, err := s.deps.Metadata.Backups().Get(r.Context(), key)
	if err != nil {
		writeError(w, http.StatusInternalServerError,
			"backup "+key+" saved but unreadable: "+err.Error())
		return
	}
	writeJSONBody(w, http.StatusOK, s.backupResponseOf(r, fresh))
}

// validExportPathShape is the cheap shape gate on the server path: an
// absolute path (the export kernel resolves and disjoint-checks it against
// the data directory at fire time — the late gates stay in the carrier,
// this gate only refuses what can never be one).
func validExportPathShape(path string) error {
	if len(path) < 2 || path[0] != '/' {
		return errors.New("exportPath must be an absolute server path (starting with '/')")
	}
	if strings.Contains(path, "..") {
		return errors.New("exportPath must not contain '..' segments")
	}
	return nil
}

// handleSystemBackupsList serves GET /api/v1/system/backups: every payload
// row with its cron projection, key-ordered (the store's order).
func (s *Server) handleSystemBackupsList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.deps.Metadata.Backups().List(r.Context())
	if err != nil {
		if metadata.IsStoreBusy(err) {
			writeError(w, http.StatusServiceUnavailable, "backups store is busy, retry shortly: "+err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "backups: listing: "+err.Error())
		return
	}
	out := make([]backupResponse, 0, len(rows))
	for _, b := range rows {
		out = append(out, s.backupResponseOf(r, b))
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"backups": out})
}

// handleSystemBackupsPut serves PUT /api/v1/system/backups (the official
// body-key form).
func (s *Server) handleSystemBackupsPut(w http.ResponseWriter, r *http.Request) {
	s.upsertBackup(w, r, "")
}

// handleSystemBackupsPutKey serves PUT /api/v1/system/backups/{key}.
func (s *Server) handleSystemBackupsPutKey(w http.ResponseWriter, r *http.Request, key string) {
	s.upsertBackup(w, r, key)
}

// handleSystemBackupsGet serves GET /api/v1/system/backups/{key}.
func (s *Server) handleSystemBackupsGet(w http.ResponseWriter, r *http.Request, key string) {
	b, err := s.deps.Metadata.Backups().Get(r.Context(), key)
	if err != nil {
		switch {
		case errors.Is(err, metadata.ErrBackupNotFound):
			writeError(w, http.StatusNotFound, "backup not found: "+key)
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable, "backups store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "backups: reading: "+err.Error())
		}
		return
	}
	writeJSONBody(w, http.StatusOK, s.backupResponseOf(r, b))
}

// handleSystemBackupsDelete serves DELETE /api/v1/system/backups/{key}:
// the payload row AND the schedule row die together (ADR-0044 decision 2's
// entity-delete linkage — a schedule without its entity must not survive).
func (s *Server) handleSystemBackupsDelete(w http.ResponseWriter, r *http.Request, key string) {
	p := principalFrom(r.Context())
	if p == nil || !p.Admin {
		writeError(w, http.StatusForbidden, "backup configuration requires an administrator account")
		return
	}
	if err := s.deps.Metadata.Backups().Delete(r.Context(), key); err != nil {
		switch {
		case errors.Is(err, metadata.ErrBackupNotFound):
			writeError(w, http.StatusNotFound, "backup not found: "+key)
		case metadata.IsStoreBusy(err):
			writeError(w, http.StatusServiceUnavailable, "backups store is busy, retry shortly: "+err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "backups: deleting: "+err.Error())
		}
		return
	}
	if derr := s.deps.Metadata.Schedules().Delete(r.Context(), scheduler.DomainBackup, key); derr != nil && !errors.Is(derr, metadata.ErrScheduleNotFound) {
		// The payload row is gone; a schedule-row failure is surfaced but
		// not fatal — the runner tombstone-cleans orphan rows at fire time.
		s.log.WarnContext(r.Context(), "httpapi: backup schedule row outlived its payload",
			"key", key, "error", derr.Error())
	}
	s.audit.Record(r.Context(), audit.Event{
		Actor:  p.Name,
		Action: audit.ActionBackupScheduleSet,
		Detail: auditDetail("key", key, "action", "deleted"),
	})
	w.WriteHeader(http.StatusNoContent)
}
