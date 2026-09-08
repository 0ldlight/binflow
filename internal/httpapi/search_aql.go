package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// The AQL REST entrance (M15 T-415, FR-133.3/.5 / ADR-0043 pt 6-8):
//
//	POST /binflow/api/search/aql[?compact=true]   body = AQL query text
//
// This file is the transport shell only: parameter parsing (text/plain body
// with the ?query= fallback, aql.md section 1), the anonymous gate's two
// spec arms (section 4 E5/E6), the error-family mapping (every *QueryError
// is a 400 with the parser's single-assembled copy; 429 + Retry-After and
// 408 come from the engine's resource gate) and the streaming envelope
// (section 3, rendered byte-faithfully from the t407 evidence). Query
// semantics, ACL weaving and resource governance all live in the engine
// (T-413) — rows arrive already visible, bounded and obfuscated.
//
// The wire envelope is NOT the shared writeJSONBody form: Artifactory
// streams it from fixed prefix/suffix constants (QUERY_PREFIX, live-verified
// byte-for-byte in reports/agents/t407-evidence/v01-basic-chain.txt), and
// ?compact compresses the ROW and range serialization only — the outer
// wrapper keeps its shape (section 3.1). Errors, by contrast, ride the plain
// E-01 errors[] envelope like every other BinFlow endpoint (ADR-0043 pt 6).

// msgAQLAuthRequired is the closed-instance anonymous arm, verbatim
// (aql.md section 4 E5, live v4b).
const msgAQLAuthRequired = "Authentication is required"

// msgAQLAnonymousRefused is the open-instance anonymous arm, verbatim
// including the trailing newline (aql.md section 4 E6 — decompiled copy, the
// live instance could not reproduce this arm; AQL is never anonymous).
const msgAQLAnonymousRefused = "Only non-anonymous users are allowed to access AQL queries\n"

// msgAQLBadRequestBody is the empty-body-AND-empty-query-param verdict,
// verbatim (aql.md section 4 E2 — the generic copy the 7.84.10 live run
// answers; E3 is the newer code path's drift, not implemented).
const msgAQLBadRequestBody = "Bad Request"

// msgAQLQueryTooLong mirrors the parser's E4 copy for the transport-side
// body cap (aql.md section 4 E4 / section 5). internal/search owns the
// single assembly point for parser messages; this literal exists because the
// transport layer rejects an oversized BODY before Parse ever runs — the
// aql_endpoint_test.go parity test pins the two spellings together.
const msgAQLQueryTooLong = "AQL query is too long; please reduce the query length to less than 6000 chars"

// The envelope scaffolding, verbatim from the streamer constants
// (aql.md section 3.1: leading \n, results on one line with " : " spacing,
// rows separated by "},{", two-space empty form, range block, trailing \n).
const (
	aqlEnvelopePrefix    = "\n{\n\"results\" : [ "
	aqlEnvelopeRowsClose = " ],\n\"range\" : "
	aqlEnvelopeSuffix    = "\n}\n"
)

// aqlSlowQueryThreshold is the >5s single-line WARN cut (ADR-0043 pt 8: half
// the K63 10s execution deadline; the engine measures its own segment, the
// transport layer reports — this is the reporting side).
const aqlSlowQueryThreshold = 5 * time.Second

// aqlRunner is the consumer-side seam over the T-413 engine: Run plus
// nothing. The real engine is assembled in New from Deps (ADR-0043 section
// 24.1: httpapi is the sole assembly point of search x repo x metadata); the
// interface exists so the transport mapping legs (429/408/500 arms) can be
// driven with a scripted runner without occupying real resource-gate slots.
type aqlRunner interface {
	Run(ctx context.Context, p *repo.Principal, query string) (*search.Result, error)
}

// handleSearchAQL serves POST /api/search/aql. The route carries NO gate
// (routeAuth{}): AQL's two anonymous arms are the handler's own first lines
// so a route-level 401 challenge cannot mask the closed-instance copy, the
// same posture the /api/storage read family keeps.
func (s *Server) handleSearchAQL(w http.ResponseWriter, r *http.Request) {
	if s.aql == nil {
		s.log.ErrorContext(r.Context(), "httpapi: AQL endpoint reached but no search engine is assembled")
		writeError(w, http.StatusServiceUnavailable, "search is not available on this instance")
		return
	}
	// The anonymous gate (aql.md section 1: "匿名不可用"): closed instance ->
	// 401 challenge with the E5 copy; open instance -> the E6 403. Both arms
	// precede body consumption — an anonymous caller never occupies a
	// resource slot.
	p := principalFrom(r.Context())
	if p == nil {
		if !s.deps.Config.Security.AnonymousAccess {
			w.Header().Set("WWW-Authenticate", basicChallenge)
			writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
			return
		}
		writeError(w, http.StatusForbidden, msgAQLAnonymousRefused)
		return
	}
	query, ok := readAQLQuery(w, r)
	if !ok {
		return // response already written (oversized body / unreadable body)
	}
	if query == "" {
		writeError(w, http.StatusBadRequest, msgAQLBadRequestBody)
		return
	}

	// The virtual keys the compiler expands during this run are collected
	// through the context seam below — they decide the implicit
	// virtual_repos projection (aql.md section 7-3) after the rows return.
	ctx, expanded := withAQLExpandedVirtuals(r.Context())
	start := time.Now()
	res, err := s.aql.Run(ctx, p, query)
	s.observeAQLQuery(time.Since(start), query)
	if err != nil {
		s.writeAQLRunError(w, r, err)
		return
	}
	s.writeAQLResult(w, r, res, *expanded, aqlCompactParam(r))
}

// readAQLQuery extracts the query text: the request body first (text/plain,
// but no content-type gate — live traffic posts form-encoded and bare bodies
// alike), the ?query= parameter only as the fallback for an empty body
// (aql.md section 1, live v4c). The body is read through a hard cap of
// MaxQueryLen+1 bytes so an unbounded body can never be buffered — a body
// over the cap answers the parser's own E4 copy without reaching Parse.
func readAQLQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(search.MaxQueryLen)+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, msgAQLBadRequestBody)
		return "", false
	}
	if len(body) > search.MaxQueryLen {
		writeError(w, http.StatusBadRequest, msgAQLQueryTooLong)
		return "", false
	}
	if q := strings.TrimSpace(string(body)); q != "" {
		return q, true
	}
	return strings.TrimSpace(r.URL.Query().Get("query")), true
}

// aqlCompactParam reads ?compact (aql.md section 1/3.1): the decompiled
// boolean parameter, documented as compact=true. An absent, bare or
// unparsable value keeps the pretty serialization.
func aqlCompactParam(r *http.Request) bool {
	v := r.URL.Query().Get("compact")
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// writeAQLRunError maps the engine's failure families onto the wire
// (ADR-0043 pt 6 + Errata 5): *QueryError -> 400 with the parser's verbatim
// copy; ErrResourceBusy -> 429 + Retry-After (body copy verbatim, header the
// C-layer machine-readable arm); ErrQueryTimeout -> 408 (NOT 503/504 — the
// Errata 5 final ruling); repo.ErrForbidden -> the 401 arm (defense in
// depth: the handler gate answers it first); everything else -> the honest
// 500, logged.
func (s *Server) writeAQLRunError(w http.ResponseWriter, r *http.Request, err error) {
	var qe *search.QueryError
	switch {
	case errors.As(err, &qe):
		writeError(w, http.StatusBadRequest, qe.Msg)
	case errors.Is(err, search.ErrResourceBusy):
		w.Header().Set("Retry-After", strconv.Itoa(int(search.BusyRetryAfter/time.Second)))
		s.countAQLRejection("concurrency")
		writeError(w, http.StatusTooManyRequests, search.ErrResourceBusy.Error())
	case errors.Is(err, search.ErrQueryTimeout):
		s.countAQLRejection("timeout")
		writeError(w, http.StatusRequestTimeout, err.Error())
	case errors.Is(err, search.ErrQueryUnavailable):
		s.log.ErrorContext(r.Context(), "httpapi: AQL engine has no executor wired", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "AQL query execution failed")
	case errors.Is(err, repo.ErrForbidden):
		w.Header().Set("WWW-Authenticate", basicChallenge)
		writeError(w, http.StatusUnauthorized, msgAQLAuthRequired)
	default:
		s.log.ErrorContext(r.Context(), "httpapi: AQL query failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "AQL query execution failed")
	}
}

// writeAQLResult renders the streaming envelope from the executed result:
// rows through the plan's projection echo list (the output order the query
// asked for), the range object from the window echoes (start_pos = the
// query's own offset, end_pos/total = the returned row count — the streaming
// convention of aql.md section 3.2, limit echoed only when the query stated
// one), the truncation surfaces on BOTH layers (the C-layer header and the
// official range.notification copy, ADR-0043 Errata 6).
func (s *Server) writeAQLResult(w http.ResponseWriter, r *http.Request, res *search.Result, expanded []string, compact bool) {
	if res.EntryDomain != "" && res.EntryDomain != "items" {
		// The build-family entries (T-511): rows render through the build
		// field registry — same envelope, same range contract.
		s.writeAQLBuildResult(w, res, compact)
		return
	}
	fields := res.Plan.Output
	virtualQueried := len(expanded) > 0
	for _, f := range fields {
		if f.Kind == search.OutputVirtualRepos {
			virtualQueried = true
		}
	}
	var rev map[string][]string
	if virtualQueried && s.aqlVirtual != nil {
		var err error
		rev, err = s.aqlVirtual.reverse(r.Context())
		if err != nil {
			s.log.ErrorContext(r.Context(), "httpapi: AQL virtual-repository index failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "AQL query execution failed")
			return
		}
	}
	// aql.md section 7-3: querying a virtual repository implicitly adds the
	// virtual_repos output field. Only an explicit include can have put it
	// in the projection already; otherwise it lands after the plan's fields.
	implicitVirtual := len(expanded) > 0 && !aqlOutputHasVirtual(fields)

	var b strings.Builder
	b.WriteString(aqlEnvelopePrefix)
	for i, row := range res.Rows {
		if i > 0 {
			b.WriteByte(',')
		}
		s.renderAQLRow(&b, row, fields, implicitVirtual, rev, compact)
	}
	b.WriteString(aqlEnvelopeRowsClose)
	renderAQLRange(&b, res, compact)
	b.WriteString(aqlEnvelopeSuffix)

	w.Header().Set("Content-Type", "application/json")
	if res.Truncated {
		w.Header().Set(search.TruncatedHeader, "true")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, b.String()) //nolint:errcheck // response body; a vanished client is not an error
}

// aqlOutputHasVirtual reports whether the projection already carries the
// virtual_repos field.
func aqlOutputHasVirtual(fields []search.OutputField) bool {
	for _, f := range fields {
		if f.Kind == search.OutputVirtualRepos {
			return true
		}
	}
	return false
}

// renderAQLRow renders one result row through the projection echo list:
// pretty form is one field per line at two spaces (the v01/v05 shape),
// compact form is a single line. Property projections aggregate into the
// one nested "properties" member at the position of the first property
// entry (the stats-domain nesting precedent of aql.md section 3.3).
func (s *Server) renderAQLRow(b *strings.Builder, row *metadata.NodeQueryRow, fields []search.OutputField,
	implicitVirtual bool, rev map[string][]string, compact bool) {
	b.WriteByte('{')
	first := true
	emit := func(key, val string) {
		if !first {
			b.WriteByte(',')
		}
		first = false
		if compact {
			b.WriteString(aqlJSONString(key))
			b.WriteByte(':')
			b.WriteString(val)
			return
		}
		b.WriteString("\n  ")
		b.WriteString(aqlJSONString(key))
		b.WriteString(" : ")
		b.WriteString(val)
	}
	propsEmitted := false
	statsEmitted := false
	for _, f := range fields {
		switch f.Kind {
		case search.OutputProp:
			if propsEmitted {
				continue
			}
			propsEmitted = true
			emit("properties", renderAQLProps(row, fields, compact))
		case search.OutputStat:
			// The statistics domain nests once, at the first stat entry's
			// position (aql.md §3.3/§14.1 — the "stats" : [ {…} ] array of
			// live v16): every named stat member lands inside the one
			// object, in include order.
			if statsEmitted {
				continue
			}
			statsEmitted = true
			emit("stats", renderAQLStats(row, fields, compact))
		case search.OutputVirtualRepos:
			emit("virtual_repos", renderAQLStringArray(aqlVirtualsOf(rev, row.RepoKey), compact))
		default:
			val, ok := aqlItemValue(f.Field, row)
			if !ok {
				// Unreachable by construction: the registry rejects fields
				// without a storage mapping before a plan ever renders.
				val = "null"
			}
			emit(f.Key, val)
		}
	}
	if implicitVirtual {
		emit("virtual_repos", renderAQLStringArray(aqlVirtualsOf(rev, row.RepoKey), compact))
	}
	if !compact {
		b.WriteString("\n")
	}
	b.WriteByte('}')
}

// renderAQLProps renders the aggregated "properties" value: one entry per
// (key, value) pair of the projected property keys ("*" = every property the
// node carries), sorted by key then value for determinism. The empty form is
// the two-space "[ ]" (the v05 virtual_repos shape); the non-empty pretty
// form nests at four spaces with "},{" separators (the stats entry shape,
// live v16) — the exact property wire form has no live sample yet (aql.md
// section 12 V-d: verify against an instance with property data).
func renderAQLProps(row *metadata.NodeQueryRow, fields []search.OutputField, compact bool) string {
	all, wanted := false, map[string]bool{}
	for _, f := range fields {
		if f.Kind != search.OutputProp {
			continue
		}
		if f.PropKey == "*" {
			all = true
		} else {
			wanted[f.PropKey] = true
		}
	}
	type kv struct{ k, v string }
	var pairs []kv
	for k, values := range row.Props {
		if !all && !wanted[k] {
			continue
		}
		for _, v := range values {
			pairs = append(pairs, kv{k, v})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	if len(pairs) == 0 {
		if compact {
			return "[]"
		}
		return "[ ]"
	}
	entry := func(p kv, compact bool) string {
		if compact {
			return `{"key":` + aqlJSONString(p.k) + `,"value":` + aqlJSONString(p.v) + `}`
		}
		return "{\n    \"key\" : " + aqlJSONString(p.k) + ",\n    \"value\" : " + aqlJSONString(p.v) + "\n  }"
	}
	var parts []string
	for _, p := range pairs {
		parts = append(parts, entry(p, compact))
	}
	if compact {
		return "[" + strings.Join(parts, ",") + "]"
	}
	return "[ " + strings.Join(parts, ",") + " ]"
}

// renderAQLStats renders the nested "stats" value: one object whose fields
// are exactly the include-named statistics members, in echo order (aql.md
// §14.1 — include replaces the domain's default set; live v16 shows the
// `[ {\n    "field" : value\n  } ]` shape). Column-backed members read the
// row; the smart-remote remote_* stubs render their constant zero values
// (0 for the counter, null for the rest — the spec's mapping ruling, data
// never fabricated).
func renderAQLStats(row *metadata.NodeQueryRow, fields []search.OutputField, compact bool) string {
	type entry struct{ k, v string }
	var entries []entry
	for _, f := range fields {
		if f.Kind != search.OutputStat {
			continue
		}
		switch f.Field {
		case search.FieldStatDownloaded:
			if row.LastDownloadedAt == "" {
				entries = append(entries, entry{"downloaded", "null"})
				continue
			}
			entries = append(entries, entry{"downloaded", aqlJSONDate(row.LastDownloadedAt)})
		case search.FieldStatDownloads:
			entries = append(entries, entry{"downloads", strconv.FormatInt(row.DownloadCount, 10)})
		case search.FieldStatDownloadedBy:
			if row.LastDownloadedBy == "" {
				entries = append(entries, entry{"downloaded_by", "null"})
				continue
			}
			entries = append(entries, entry{"downloaded_by", aqlJSONString(row.LastDownloadedBy)})
		case search.FieldStatRemoteDownloads:
			entries = append(entries, entry{"remote_downloads", "0"})
		case search.FieldStatRemoteDownloaded:
			entries = append(entries, entry{"remote_downloaded", "null"})
		case search.FieldStatRemoteDownloadedBy:
			entries = append(entries, entry{"remote_downloaded_by", "null"})
		case search.FieldStatRemoteOrigin:
			entries = append(entries, entry{"remote_origin", "null"})
		case search.FieldStatRemotePath:
			entries = append(entries, entry{"remote_path", "null"})
		}
	}
	render := func(e entry, compact bool) string {
		if compact {
			return `"` + e.k + `":` + e.v
		}
		return `    "` + e.k + `" : ` + e.v
	}
	var parts []string
	for _, e := range entries {
		parts = append(parts, render(e, compact))
	}
	if compact {
		return "[{" + strings.Join(parts, ",") + "}]"
	}
	return "[ {\n" + strings.Join(parts, ",\n") + "\n  } ]"
}

// aqlVirtualsOf reads the member->virtuals reverse map, tolerating a nil map
// (no virtual repositories on the instance).
func aqlVirtualsOf(rev map[string][]string, member string) []string {
	if rev == nil {
		return nil
	}
	return rev[member]
}

// renderAQLStringArray renders a string-array value: scalars stay on one
// line ("[ \"a\", \"b\" ]", the two-space empty form of v05); compact drops
// the padding entirely.
func renderAQLStringArray(vals []string, compact bool) string {
	if len(vals) == 0 {
		if compact {
			return "[]"
		}
		return "[ ]"
	}
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, aqlJSONString(v))
	}
	if compact {
		return "[" + strings.Join(parts, ",") + "]"
	}
	return "[ " + strings.Join(parts, ", ") + " ]"
}

// renderAQLRange renders the range tail object (aql.md section 3.2):
// start_pos = the query's own offset echo, end_pos/total = the returned row
// count (the streaming convention — NOT a whole-set total), limit only when
// the query stated one, notification exactly when the cap+1 probe saw more
// raw rows (the verbatim A-layer copy, ADR-0043 Errata 6).
func renderAQLRange(b *strings.Builder, res *search.Result, compact bool) {
	type entry struct{ k, v string }
	rows := strconv.Itoa(len(res.Rows))
	entries := []entry{
		{"start_pos", strconv.FormatInt(res.Offset, 10)},
		{"end_pos", rows},
		{"total", rows},
	}
	if res.HasLimit {
		entries = append(entries, entry{"limit", strconv.FormatInt(res.Limit, 10)})
	}
	if res.Truncated {
		entries = append(entries, entry{"notification", aqlJSONString(search.TruncationNotice)})
	}
	if compact {
		b.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`"` + e.k + `":` + e.v)
		}
		b.WriteByte('}')
		return
	}
	b.WriteString("{\n")
	for i, e := range entries {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(`  "` + e.k + `" : ` + e.v)
	}
	b.WriteString("\n}")
}

// aqlItemValue renders one storage-backed projection field of a row. The
// bool reports a known field; dates normalize to the ISO8601-milliseconds
// UTC form of the live samples ("2026-08-23T08:19:04.618Z").
func aqlItemValue(f search.FieldID, row *metadata.NodeQueryRow) (string, bool) {
	switch f {
	case search.FieldRepo:
		return aqlJSONString(row.RepoKey), true
	case search.FieldPath:
		return aqlJSONString(row.ParentPath), true
	case search.FieldName:
		return aqlJSONString(row.Name), true
	case search.FieldType:
		return aqlJSONString(row.Type), true
	case search.FieldSize:
		return strconv.FormatInt(row.Size, 10), true
	case search.FieldCreated:
		return aqlJSONDate(row.CreatedAt), true
	case search.FieldModified, search.FieldUpdated:
		return aqlJSONDate(row.UpdatedAt), true
	case search.FieldCreatedBy:
		return aqlJSONString(row.CreatedBy), true
	case search.FieldDepth:
		return strconv.FormatInt(row.Depth, 10), true
	case search.FieldActualMD5:
		return aqlJSONString(row.Md5), true
	case search.FieldActualSHA1:
		return aqlJSONString(row.Sha1), true
	case search.FieldSha256:
		return aqlJSONString(row.Sha256), true
	}
	return "", false
}

// aqlJSONString renders one JSON string scalar.
func aqlJSONString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

// aqlJSONDate renders a stored timestamp in the live echo format: ISO8601
// with milliseconds, UTC. A value that does not parse (a hand-seeded row,
// a drifted schema) passes through verbatim rather than being faked.
func aqlJSONDate(raw string) string {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return `"` + t.UTC().Format("2006-01-02T15:04:05.000Z") + `"`
	}
	return aqlJSONString(raw)
}

// ---- the virtual-repository index ----

// virtualMemberLister is the member-ledger seam the index reads (satisfied
// by metadata's virtual store).
type virtualMemberLister interface {
	ListMembers(ctx context.Context, virtualRepo string) ([]*metadata.VirtualMember, error)
}

// virtualIndex serves both AQL faces of the virtual-repository registry
// (aql.md section 7): the engine's Members resolver (a virtual key is a
// legal query VALUE the compiler expands onto its member set — the virtual
// repository is never a query entity because nodes carry no virtual rows)
// and the endpoint's member->virtuals reverse map (the virtual_repos output
// projection). Computed per call off the live ledger, the same
// per-request-freshness rule repo's own resolution runs on.
type virtualIndex struct {
	repos   metadata.RepoStore
	members virtualMemberLister
}

// newVirtualIndex builds the index over a metadata store.
func newVirtualIndex(md metadata.Store) *virtualIndex {
	return &virtualIndex{repos: md.Repos(), members: md.Virtual()}
}

// Members implements search.VirtualResolver: the member keys of a virtual
// repository, in stored order; nil for anything that is not a virtual
// repository (the planner keeps the original predicate, which matches no
// storage row — the honest empty set of section 7-4). A key that names a
// real virtual repository is recorded on the request's collector so the
// endpoint can add the implicit virtual_repos output (section 7-3).
func (v *virtualIndex) Members(ctx context.Context, key string) ([]string, error) {
	row, err := v.repos.Get(ctx, key)
	if err != nil {
		if errors.Is(err, metadata.ErrRepoNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("aql virtual index: repository %s: %w", key, err)
	}
	if row.Type != repo.TypeVirtual {
		return nil, nil
	}
	rows, err := v.members.ListMembers(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("aql virtual index: members of %s: %w", key, err)
	}
	out := make([]string, 0, len(rows))
	for _, m := range rows {
		out = append(out, m.MemberRepo)
	}
	if len(out) > 0 {
		if c, ok := ctx.Value(aqlExpandedVirtualsKey{}).(*[]string); ok && c != nil {
			*c = append(*c, key)
		}
	}
	return out, nil
}

// reverse builds the member->virtual-keys map the virtual_repos projection
// reads (one pass over the registry and every virtual's member list —
// virtual counts are instance-small; a hot instance can cache behind the
// same seam later without touching the wire).
func (v *virtualIndex) reverse(ctx context.Context) (map[string][]string, error) {
	all, err := v.repos.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("aql virtual index: listing repositories: %w", err)
	}
	rev := make(map[string][]string)
	for _, r := range all {
		if r.Type != repo.TypeVirtual {
			continue
		}
		members, err := v.members.ListMembers(ctx, r.RepoKey)
		if err != nil {
			return nil, fmt.Errorf("aql virtual index: members of %s: %w", r.RepoKey, err)
		}
		for _, m := range members {
			rev[m.MemberRepo] = append(rev[m.MemberRepo], r.RepoKey)
		}
	}
	for member := range rev {
		sort.Strings(rev[member])
	}
	return rev, nil
}

// aqlExpandedVirtualsKey keys the per-request collector of expanded virtual
// keys. The engine is process-shared while the collector is per query: the
// context is the only channel the resolver can see the request through, and
// one Run is one goroutine's sequential compile, so the slice needs no lock.
type aqlExpandedVirtualsKey struct{}

// withAQLExpandedVirtuals returns a context carrying a fresh collector plus
// the collector itself.
func withAQLExpandedVirtuals(ctx context.Context) (context.Context, *[]string) {
	var expanded []string
	return context.WithValue(ctx, aqlExpandedVirtualsKey{}, &expanded), &expanded
}

// observeAQLQuery records one executed query (any outcome) on the search
// family — the plane counter and the latency histogram — and emits the >5s
// single-line WARN (ADR-0043 pt 8: query digest + duration). The WARN fires
// on metrics-less stacks too: the log is the always-on observer, the
// counters the opt-in one (the countAddonGate posture).
func (s *Server) observeAQLQuery(d time.Duration, query string) {
	if s.metrics != nil {
		s.metrics.searchQueries.Inc("plane", "aql")
		s.metrics.searchDur.Observe(d.Seconds())
	}
	if d > aqlSlowQueryThreshold {
		s.log.Warn("httpapi: slow AQL query",
			"duration_ms", d.Milliseconds(), "query", aqlQueryDigest(query))
	}
}

// countAQLRejection records one resource-gate rejection (reason:
// "concurrency" for the 429 arm, "timeout" for the 408 arm). Metrics-less
// stacks count nothing and gate exactly the same.
func (s *Server) countAQLRejection(reason string) {
	if s.metrics == nil {
		return
	}
	s.metrics.searchRej.Inc("reason", reason)
}

// aqlQueryDigest trims a query to a log-friendly prefix (the slow-query WARN
// stays one line; the full text is capped at 6,000 chars by the parser, the
// digest at 200 by the reporter).
func aqlQueryDigest(query string) string {
	const digestLen = 200
	if len(query) > digestLen {
		return query[:digestLen] + "..."
	}
	return query
}
