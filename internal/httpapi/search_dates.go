package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The dates/creation doors (M16 T-452, FR-148.3 / aql.md §14.3 — the K65
// carry-over from M15's §5.7 panorama, where the pair was ruled "wire 腿
// 非同构" and deferred to M16):
//
//	GET /binflow/api/search/creation?from=<epoch-ms>[&to=][&repos=]
//	GET /binflow/api/search/dates?from=<epoch-ms>[&to=][&dateFields=][&repos=]
//
// The 404-empty family's second and third members (usage opened it in
// T-440): a miss answers the verbatim {"errors":[{404,"No results
// found."}]} copy; the rows are the family's thin uri-plus-one-date shape;
// the parameters are epoch milliseconds. Both doors ride the engine's fixed
// templates (RunCreation/RunDates) — the same gate, deadline, cap and ACL
// weave every other query plane takes, so the zero-leak posture is the
// weave's, not a new one.
//
// Registered divergences/pending items (aql.md §14.3/§14.7): to-absent
// follows the OFFICIAL now() reading (V-j, the two sources conflict);
// dateFields-absent defaults to {created, lastModified} (V-k, no registered
// default — the creation door's own fixed pair); the dates door's HIT row
// shape has no live sample (only the empty-set arm was live-verified, v8o)
// — BinFlow renders the creation door's {uri, created} thin row (the
// shared-parent ruling of §14.3's "两端点同一父类"); the from-missing 400
// carries the decompiled verbatim copy inside the BinFlow envelope (the
// envelope form is the plane's, the message is the anchor's).

// msgDatesFromRequired is the from-missing 400's verbatim copy (§14.3,
// single quotes included).
const msgDatesFromRequired = "'from' parameter cannot be empty!"

// dateRangeRunner is the consumer-side seam over the engine's two fixed
// templates (the usageRunner precedent — asserted off the assembled engine
// so the scripted aqlRunner fakes of the transport legs keep compiling
// untouched).
type dateRangeRunner interface {
	RunCreation(ctx context.Context, p *repo.Principal, cq search.CreationQuery) (*search.Result, error)
	RunDates(ctx context.Context, p *repo.Principal, dq search.DatesQuery) (*search.Result, error)
}

// handleSearchCreation serves GET /api/search/creation (§14.3): from is
// required epoch milliseconds; to is optional (absent = now, V-j); repos
// narrows. A hit row is {uri, created} where the echoed created value is
// the item's created when THAT falls inside the request range, else its
// lastModified — the decompiled echo fallback (V-o: a row matched through
// lastModified echoes the modified instant in the created field; medium
// confidence, pinned by test).
func (s *Server) handleSearchCreation(w http.ResponseWriter, r *http.Request) {
	runner, from, to, repos, ok := s.parseDateRange(w, r)
	if !ok {
		return
	}
	start := time.Now()
	res, err := runner.RunCreation(r.Context(), principalFrom(r.Context()), search.CreationQuery{
		From: from, To: to, Repos: repos,
	})
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeAQLRunError(w, r, err) // the engine's families: 400/429/408/500
		return
	}
	s.writeDateRangeResults(w, r, res, from, to)
}

// handleSearchDates serves GET /api/search/dates (§14.3): dateFields is an
// optional CSV over the closed four-name set; an unknown name answers the
// decompiled verbatim 400 with the enum echo. The row shape rides the
// shared-parent {uri, created} thin form.
func (s *Server) handleSearchDates(w http.ResponseWriter, r *http.Request) {
	runner, from, to, repos, ok := s.parseDateRange(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var fields []string
	if raw := q.Get("dateFields"); strings.TrimSpace(raw) != "" {
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !search.ValidDatesField(name) {
				writeError(w, http.StatusBadRequest, unknownDatesFieldCopy(name))
				return
			}
			fields = append(fields, name)
		}
	}
	start := time.Now()
	res, err := runner.RunDates(r.Context(), principalFrom(r.Context()), search.DatesQuery{
		From: from, To: to, Fields: fields, Repos: repos,
	})
	s.observeLegacySearch(time.Since(start))
	if err != nil {
		s.writeAQLRunError(w, r, err)
		return
	}
	s.writeDateRangeResults(w, r, res, from, to)
}

// unknownDatesFieldCopy renders the unknown-dateFields 400 message verbatim
// (§14.3: the "unknown!, possible" spelling is preserved as decomplied; the
// enum echo is the four-value closed set in wire order).
func unknownDatesFieldCopy(name string) string {
	return "Date field name '" + name + "' unknown!, possible values are: [" +
		strings.Join(search.DatesFieldNames, ", ") + "]"
}

// parseDateRange performs the two doors' shared preamble: the anonymous 401
// challenge (§14.3: privileged non-anonymous), the engine-presence check,
// and the from/to/repos parameter grammar. ok=false means the response is
// already written.
func (s *Server) parseDateRange(w http.ResponseWriter, r *http.Request) (dateRangeRunner, int64, int64, []string, bool) {
	p := principalFrom(r.Context())
	if p == nil {
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
		return nil, 0, 0, nil, false
	}
	runner, ok := s.aql.(dateRangeRunner)
	if s.aql == nil || !ok {
		s.log.ErrorContext(r.Context(), "httpapi: dates-range search endpoint reached but no search engine is assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return nil, 0, 0, nil, false
	}
	q := r.URL.Query()
	rawFrom := strings.TrimSpace(q.Get("from"))
	if rawFrom == "" {
		writeError(w, http.StatusBadRequest, msgDatesFromRequired)
		return nil, 0, 0, nil, false
	}
	from, err := strconv.ParseInt(rawFrom, 10, 64)
	if err != nil || from < 0 {
		writeError(w, http.StatusBadRequest, "Date-range search requires a non-negative 'from' epoch-milliseconds value.")
		return nil, 0, 0, nil, false
	}
	var to int64
	if rawTo := strings.TrimSpace(q.Get("to")); rawTo != "" {
		to, err = strconv.ParseInt(rawTo, 10, 64)
		if err != nil || to < 0 {
			writeError(w, http.StatusBadRequest, "Date-range search requires a non-negative 'to' epoch-milliseconds value.")
			return nil, 0, 0, nil, false
		}
	}
	return runner, from, to, searchReposFilter(q.Get("repos")), true
}

// dateRangeRow is the doors' thin row: the storage uri plus the echoed
// date (§14.3).
type dateRangeRow struct {
	URI     string `json:"uri"`
	Created string `json:"created"`
}

// dateRangeResults is the doors' envelope.
type dateRangeResults struct {
	Results []dateRangeRow `json:"results"`
}

// writeDateRangeResults renders the shared response: the empty set is the
// family's verbatim 404; each row's created echo applies the V-o fallback
// (created inside the range echoes created; a row that only matched through
// lastModified echoes the modified instant). The K63 truncation header
// rides the family's shared surface.
func (s *Server) writeDateRangeResults(w http.ResponseWriter, r *http.Request, res *search.Result, from, to int64) {
	if len(res.Rows) == 0 {
		writeError(w, http.StatusNotFound, msgUsageNoResults)
		return
	}
	if res.Truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	base := requestBase(r)
	rows := make([]dateRangeRow, 0, len(res.Rows))
	for _, row := range res.Rows {
		rows = append(rows, dateRangeRow{
			URI:     storageURI(base, row.RepoKey, row.Path),
			Created: echoedDate(row, from, to),
		})
	}
	writeJSONBody(w, http.StatusOK, dateRangeResults{Results: rows})
}

// echoedDate applies the V-o fallback: the created instant when it falls
// inside (from, to] (to of 0 means now), else the modified instant — the
// value a lastModified-matched row echoes in the created field. Rendering
// is the family's ISO8601-milliseconds form; an unparseable stored value
// passes through verbatim rather than being faked.
func echoedDate(row *metadata.NodeQueryRow, from, to int64) string {
	if created, ok := parseStoredTime(row.CreatedAt); ok {
		if to == 0 {
			to = time.Now().UnixMilli()
		}
		if ms := created.UnixMilli(); ms > from && ms <= to {
			return isoMillisUTC(row.CreatedAt)
		}
	}
	if row.UpdatedAt != "" {
		return isoMillisUTC(row.UpdatedAt)
	}
	return isoMillisUTC(row.CreatedAt)
}

// parseStoredTime parses one stored RFC3339 timestamp.
func parseStoredTime(v string) (time.Time, bool) {
	if v == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
