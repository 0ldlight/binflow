package httpapi

// The AQL transport legs that need the package's own seams (M15 T-415):
// the error-family mapping over a scripted runner (the 429/408/500 arms
// would otherwise need real resource-gate slots), the envelope goldens with
// deterministic rows, both anonymous arms against a hand-built server, and
// the ADR-0043 pt 7 WriteTimeout pin.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/config"
	"github.com/lzwzzy/binflow/internal/metadata"
	"github.com/lzwzzy/binflow/internal/repo"
	"github.com/lzwzzy/binflow/internal/search"
)

// stubAQL scripts one Run outcome.
type stubAQL struct {
	res *search.Result
	err error
}

func (s *stubAQL) Run(context.Context, *repo.Principal, string) (*search.Result, error) {
	return s.res, s.err
}

// newAQLTransportServer builds the smallest Server whose handler chain the
// AQL endpoint needs, with the engine replaced by the given stub. The
// Console default and the nil-metrics posture keep the stack at the unit
// shape (the newPanicConsoleServer precedent).
func newAQLTransportServer(t *testing.T, mutate func(*config.Config), run aqlRunner) *Server {
	t.Helper()
	cfg := config.Defaults()
	if mutate != nil {
		mutate(cfg)
	}
	s := New(Deps{Config: cfg}, nil)
	s.aql = run
	return s
}

// serveAQL drives the handler with an authenticated admin principal.
func serveAQL(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	p := &auth.Principal{Name: "admin", Admin: true}
	req = req.WithContext(withPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	s.handleSearchAQL(rec, req)
	return rec
}

// TestAQLRunErrorMapping is the error-family table (ADR-0043 pt 6 + Errata
// 5): every QueryError kind is a 400 carrying the parser's verbatim copy,
// the busy gate answers 429 + Retry-After with the official lowercase body,
// the deadline answers 408 (never 503/504), and everything else is the
// honest logged 500.
func TestAQLRunErrorMapping(t *testing.T) {
	tests := []struct {
		name        string
		run         aqlRunner
		wantStatus  int
		wantBody    string
		wantHeader  string
		headerValue string
	}{
		{
			name:        "syntax QueryError is a 400 with the E1 copy",
			run:         &stubAQL{err: &search.QueryError{Kind: search.ErrSyntax, Msg: "Failed to parse query: x, it looks like there is syntax error near the following sub-query: x"}},
			wantStatus:  http.StatusBadRequest,
			wantBody:    "Failed to parse query: x",
			wantHeader:  "Retry-After",
			headerValue: "",
		},
		{
			name:        "busy gate: 429, Retry-After 1, official body",
			run:         &stubAQL{err: search.ErrResourceBusy},
			wantStatus:  http.StatusTooManyRequests,
			wantBody:    `"message": "too many requests"`,
			wantHeader:  "Retry-After",
			headerValue: "1",
		},
		{
			name:        "deadline: 408 with the cause rendered",
			run:         &stubAQL{err: fmt.Errorf("%w: context deadline exceeded", search.ErrQueryTimeout)},
			wantStatus:  http.StatusRequestTimeout,
			wantBody:    "AQL query execution timed out: context deadline exceeded",
			wantHeader:  "Retry-After",
			headerValue: "",
		},
		{
			name:       "no executor wired: honest 500",
			run:        &stubAQL{err: search.ErrQueryUnavailable},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "AQL query execution failed",
		},
		{
			name:       "store failure: honest 500",
			run:        &stubAQL{err: fmt.Errorf("engine: executing query: disk gone")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   "AQL query execution failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newAQLTransportServer(t, nil, tt.run)
			rec := serveAQL(t, s, http.MethodPost, "/binflow/api/search/aql", "items.find({})")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d\nbody: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %s, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantHeader != "" && rec.Header().Get(tt.wantHeader) != tt.headerValue {
				t.Fatalf("%s = %q, want %q", tt.wantHeader, rec.Header().Get(tt.wantHeader), tt.headerValue)
			}
		})
	}
}

// TestAQLNoEngineAnswers503 pins the degraded posture: a stack without the
// engine (metadata-less unit stacks) refuses honestly instead of querying
// unfiltered.
func TestAQLNoEngineAnswers503(t *testing.T) {
	s := newAQLTransportServer(t, nil, nil)
	rec := serveAQL(t, s, http.MethodPost, "/binflow/api/search/aql", "items.find({})")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "search is not available") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

// TestAQLAnonymousArms pins the two spec arms at the handler level (aql.md
// section 1/4 E5/E6): the closed instance challenges with the 401 copy, the
// open instance refuses with the E6 copy including its trailing newline.
func TestAQLAnonymousArms(t *testing.T) {
	build := func(anon bool) *Server {
		return newAQLTransportServer(t, func(c *config.Config) {
			c.Security.AnonymousAccess = anon
		}, &stubAQL{res: &search.Result{Plan: &search.Plan{}}})
	}

	req := httptest.NewRequest(http.MethodPost, "/binflow/api/search/aql", strings.NewReader("items.find({})"))
	rec := httptest.NewRecorder()
	build(false).handleSearchAQL(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("closed-instance status = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Authentication is required") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if ch := rec.Header().Get("WWW-Authenticate"); !strings.HasPrefix(ch, "Basic") {
		t.Fatalf("WWW-Authenticate = %q, want the Basic challenge", ch)
	}

	req = httptest.NewRequest(http.MethodPost, "/binflow/api/search/aql", strings.NewReader("items.find({})"))
	rec = httptest.NewRecorder()
	build(true).handleSearchAQL(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("open-instance status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `Only non-anonymous users are allowed to access AQL queries\n`) {
		t.Fatalf("body = %s, want the E6 copy with the escaped trailing newline", rec.Body.String())
	}
}

// TestAQLEnvelopeGoldens is the byte-exact envelope over deterministic
// rows: the date normalization (offset zone to UTC millis), depth/size as
// bare numbers, the v07 page range, and the compact twin of the same row.
func TestAQLEnvelopeGoldens(t *testing.T) {
	res := &search.Result{
		Plan: &search.Plan{
			Output: []search.OutputField{
				{Key: "repo", Kind: search.OutputItem, Field: search.FieldRepo},
				{Key: "path", Kind: search.OutputItem, Field: search.FieldPath},
				{Key: "name", Kind: search.OutputItem, Field: search.FieldName},
				{Key: "type", Kind: search.OutputItem, Field: search.FieldType},
				{Key: "size", Kind: search.OutputItem, Field: search.FieldSize},
				{Key: "created", Kind: search.OutputItem, Field: search.FieldCreated},
				{Key: "depth", Kind: search.OutputItem, Field: search.FieldDepth},
			},
			Offset: 1, HasOffset: true, Limit: 1, HasLimit: true,
		},
		Rows: []*metadata.NodeQueryRow{{
			RepoKey: "libs-release", Path: "com/acme/app/1.0.0/app.jar",
			ParentPath: "com/acme/app/1.0.0", Name: "app.jar", Type: "file",
			Depth: 4, Size: 2048, CreatedBy: "admin",
			// A +08:00 offset spelling must normalize to the UTC millis echo.
			CreatedAt: "2026-08-23T16:19:04.618123+08:00",
		}},
		Offset: 1, HasOffset: true, Limit: 1, HasLimit: true,
	}
	s := newAQLTransportServer(t, nil, &stubAQL{res: res})

	rec := serveAQL(t, s, http.MethodPost, "/binflow/api/search/aql", "items.find({})")
	wantPretty := "\n{\n" +
		"\"results\" : [ {\n" +
		"  \"repo\" : \"libs-release\",\n" +
		"  \"path\" : \"com/acme/app/1.0.0\",\n" +
		"  \"name\" : \"app.jar\",\n" +
		"  \"type\" : \"file\",\n" +
		"  \"size\" : 2048,\n" +
		"  \"created\" : \"2026-08-23T08:19:04.618Z\",\n" +
		"  \"depth\" : 4\n" +
		"} ],\n" +
		"\"range\" : {\n  \"start_pos\" : 1,\n  \"end_pos\" : 1,\n  \"total\" : 1,\n  \"limit\" : 1\n}\n" +
		"}\n"
	if rec.Body.String() != wantPretty {
		t.Fatalf("pretty body =\n%q\nwant=\n%q", rec.Body.String(), wantPretty)
	}

	rec = serveAQL(t, s, http.MethodPost, "/binflow/api/search/aql?compact=true", "items.find({})")
	wantCompact := "\n{\n\"results\" : [ " +
		"{\"repo\":\"libs-release\",\"path\":\"com/acme/app/1.0.0\",\"name\":\"app.jar\",\"type\":\"file\",\"size\":2048,\"created\":\"2026-08-23T08:19:04.618Z\",\"depth\":4}" +
		" ],\n\"range\" : {\"start_pos\":1,\"end_pos\":1,\"total\":1,\"limit\":1}\n}\n"
	if rec.Body.String() != wantCompact {
		t.Fatalf("compact body =\n%q\nwant=\n%q", rec.Body.String(), wantCompact)
	}
}

// TestAQLTruncationSurfaces pins both truncation layers on one response
// (ADR-0043 Errata 6): the C-layer header and the official notification
// copy inside the range tail, verbatim.
func TestAQLTruncationSurfaces(t *testing.T) {
	res := &search.Result{
		Plan:      &search.Plan{Output: []search.OutputField{{Key: "name", Kind: search.OutputItem, Field: search.FieldName}}},
		Rows:      []*metadata.NodeQueryRow{{RepoKey: "r", Path: "a.bin", Name: "a.bin", Type: "file"}},
		Truncated: true,
	}
	s := newAQLTransportServer(t, nil, &stubAQL{res: res})
	rec := serveAQL(t, s, http.MethodPost, "/binflow/api/search/aql", "items.find({})")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get(search.TruncatedHeader); got != "true" {
		t.Fatalf("truncation header = %q", got)
	}
	if !strings.Contains(rec.Body.String(),
		"\n  \"notification\" : \"AQL query reached the search hard limit, results are trimmed.\"") {
		t.Fatalf("notification missing\nbody: %s", rec.Body.String())
	}
}

// TestServerWriteTimeoutStaysUnset pins ADR-0043 pt 7 (the T-392 ruling):
// the shared Server sets NO global WriteTimeout — the content plane streams
// legitimate minute-scale writes, and AQL's duration governance lives in the
// engine deadline. ReadHeaderTimeout stays 10s.
func TestServerWriteTimeoutStaysUnset(t *testing.T) {
	s := New(Deps{Config: config.Defaults()}, nil)
	if s.srv.WriteTimeout != 0 {
		t.Fatalf("WriteTimeout = %s, want 0 (ADR-0043 pt 7)", s.srv.WriteTimeout)
	}
	if s.srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want 10s", s.srv.ReadHeaderTimeout)
	}
}

// TestAQLQueryDigest pins the slow-query WARN's digest rule (one line, 200
// visible characters).
func TestAQLQueryDigest(t *testing.T) {
	if got := aqlQueryDigest("short"); got != "short" {
		t.Fatalf("digest = %q", got)
	}
	long := strings.Repeat("x", 250)
	if got := aqlQueryDigest(long); got != strings.Repeat("x", 200)+"..." {
		t.Fatalf("digest length = %d, want 203", len(got))
	}
}
