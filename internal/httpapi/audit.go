// Audit query endpoint (PRD M4 FR-29 / GE-01, T-93): the read face of the
// append-only audit log.
//
//	GET /binflow/api/v1/audit?repo=&actor=&action=&since=&until=&limit=&cursor=
//
// Admin only — a non-admin principal must not read other actors' actions
// (403), an unauthenticated caller gets the management-plane 401 challenge.
// The response is {"events":[{id,time,actor,action,repo,path,detail}],
// "nextCursor"}: detail is a JSON OBJECT (the stored object string rendered
// as-is, "{}" for any row that holds something else), time is RFC3339 UTC
// verbatim from the row, events run newest-first and nextCursor is the
// opaque keyset cursor of the next page ("" on the last page).
//
// Append-only (W39 / NFR-S21): GET is the ONLY verb this file routes.
// Every other spelling — PUT/DELETE/POST /api/v1/audit, /api/v1/audit/1 —
// falls through to the E-26 404: there is no write surface to reach, and
// the metadata layer carries no UPDATE/DELETE path for audit rows either
// (pinned on the source by internal/audit's append-only scan).

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/lzwzzy/binflow/internal/audit"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// GE-01 pagination contract: an absent limit means 100, and anything above
// the cap is refused rather than silently clamped — an operator asking for
// 1001 rows wants to learn the cap, not receive a quietly different page.
const (
	auditLimitDefault = 100
	auditLimitMax     = 1000
)

// auditEventBody is one events[] entry. Detail is json.RawMessage so a
// stored object renders as a JSON object, never as the string that carries
// it in the row (PRD GE-01: "detail 为对象非字符串").
type auditEventBody struct {
	ID     int64           `json:"id"`
	Time   string          `json:"time"`
	Actor  string          `json:"actor"`
	Action string          `json:"action"`
	Repo   string          `json:"repo"`
	Path   string          `json:"path"`
	Detail json.RawMessage `json:"detail"`
}

// auditPageBody is the GE-01 envelope. nextCursor is always present — ""
// marks the last page, so a client never has to guess between "absent"
// and "empty" shapes.
type auditPageBody struct {
	Events     []auditEventBody `json:"events"`
	NextCursor string           `json:"nextCursor"`
}

// handleAuditQuery serves GET /api/v1/audit (W22/W22b). Every parameter is
// optional; each malformed value answers the E-01 400: a limit outside
// 1..1000, a since/until that is not RFC3339 (normalized to UTC before the
// store's textual window comparison — T-90 review note), or a cursor this
// instance never issued.
func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	if s.auditLog == nil {
		writeError(w, http.StatusServiceUnavailable, "audit log is not available on this instance")
		return
	}
	q := r.URL.Query()

	limit := auditLimitDefault
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > auditLimitMax {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("limit must be an integer between 1 and %d", auditLimitMax))
			return
		}
		limit = n
	}

	f := audit.Filter{
		Repo:   q.Get("repo"),
		Actor:  q.Get("actor"),
		Action: q.Get("action"),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	}
	since, err := audit.NormalizeTimestamp(q.Get("since"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	until, err := audit.NormalizeTimestamp(q.Get("until"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	f.Since, f.Until = since, until

	page, err := s.auditLog.Query(r.Context(), f)
	if err != nil {
		// metadata.ErrInvalidCursor (NOT repo.ErrInvalidCursor — same name,
		// different plane: docker tags pagination): a cursor this store
		// never issued is client input, answered as 400.
		if errors.Is(err, metadata.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		s.log.ErrorContext(r.Context(), "httpapi: audit query failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "audit query failed")
		return
	}

	events := make([]auditEventBody, 0, len(page.Events))
	for _, e := range page.Events {
		events = append(events, auditEventBody{
			ID: e.ID, Time: e.Time, Actor: e.Actor, Action: e.Action,
			Repo: e.Repo, Path: e.Path, Detail: auditDetailObject(e.Detail),
		})
	}
	writeJSONBody(w, http.StatusOK, auditPageBody{Events: events, NextCursor: page.NextCursor})
}

// auditDetailObject renders a stored Detail payload as a JSON object:
// valid object JSON passes through verbatim; everything else (empty, a
// JSON scalar or array, malformed text) normalizes to {}. Redaction
// already happened at append time (audit.Redact) — this only fixes the
// wire shape.
func auditDetailObject(detail string) json.RawMessage {
	d := strings.TrimSpace(detail)
	if len(d) > 0 && d[0] == '{' && json.Valid([]byte(d)) {
		return json.RawMessage(d)
	}
	return json.RawMessage("{}")
}
