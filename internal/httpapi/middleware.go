package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
)

// Middleware wraps one HTTP handler layer (architecture section 7.2).
type Middleware func(http.Handler) http.Handler

// chain composes middlewares so the FIRST entry is the outermost layer:
// chain(a, b, c)(h) runs a -> b -> c -> h. The order below is fixed by the
// architecture and is asserted by TestMiddlewareOrder.
func chain(ms ...Middleware) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		h := final
		for i := len(ms) - 1; i >= 0; i-- {
			h = ms[i](h)
		}
		return h
	}
}

// requestID mints a per-request correlation id, echoes it on the response
// and stores it for the access log. Runs FIRST so every later log line —
// including the access log's own record of a panicking request — can refer
// to it.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(withRequestID(r.Context(), id)))
	})
}

// statusRecorder captures the response status and byte counts for the
// access log.
//
// Status semantics (T-14 review B3): the FIRST WriteHeader call records
// its code unconditionally, and a bare Write never overwrites a recorded
// status. So a handler that streams body bytes and only then discovers
// the failure (partial write followed by WriteHeader(5xx)) is logged with
// the error code, not the 200 the wire already committed — the access
// log must expose the failure, not mirror the transport.
type statusRecorder struct {
	http.ResponseWriter
	status int
	// headerWritten: an explicit WriteHeader ran (status line decided).
	headerWritten bool
	// wrote: body bytes have been written (response committed on the
	// wire, implicitly as 200 unless WriteHeader ran first).
	wrote    bool
	bytesOut int64
}

func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.headerWritten {
		s.status = code
		s.headerWritten = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytesOut += int64(n)
	if n > 0 {
		s.wrote = true
	}
	return n, err
}

// committed reports whether the response has gone out on the wire —
// explicitly (WriteHeader) or implicitly (body bytes). Once committed the
// status line can no longer be rewritten; appending an error body would
// only graft JSON onto a partial 200 (T-14 review M1).
func (s *statusRecorder) committed() bool { return s.headerWritten || s.wrote }

// markStatus overrides the recorded status for the access log WITHOUT
// touching the wire. Used by recoverPanic when the response is already
// committed: the error must be observable in the log even though the
// bytes cannot be recalled.
func (s *statusRecorder) markStatus(code int) { s.status = code }

// Flush forwards to the wrapped writer when it supports flushing (streaming
// downloads must not be buffered by the recorder).
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// bytesIn counts the request body size for the access log.
func bytesIn(r *http.Request) int64 {
	if r.ContentLength > 0 {
		return r.ContentLength
	}
	return 0
}

// accessLog writes one structured log line per request (NFR-S3): method,
// path, status, duration_ms, remote_addr, user (anonymous when
// unauthenticated), bytes_in, bytes_out and the request id. It NEVER logs
// headers — the Authorization / X-JFrog-Art-Api values ride in them. The
// path is logged from EscapedPath so the record matches what the client
// sent (and dot-segment probes stay visible to operators).
//
// Position: second in the chain, OUTSIDE recover, so a panicking handler's
// aborted request still gets its access-log line with the 500 recover
// produced. The principal is threaded back OUT of the inner chain through
// the mutable logFields holder below: the authenticator runs deeper in
// the chain, so a plain context value could never propagate outward
// (contexts only flow inward).
func accessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := newStatusRecorder(w)
			fields := &logFields{}
			r = r.WithContext(context.WithValue(r.Context(), ctxKeyLogFields, fields))
			next.ServeHTTP(rec, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "access",
				slog.String("method", r.Method),
				slog.String("path", r.URL.EscapedPath()),
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user", fields.userName()),
				slog.Int64("bytes_in", bytesIn(r)),
				slog.Int64("bytes_out", rec.bytesOut),
				slog.String("request_id", requestIDFrom(r.Context())),
			)
		})
	}
}

// logFields is the outward thread for values the inner chain computes.
// It is written once by the authenticator (before any handler runs) and
// read once by the access log after the inner chain returns, so the
// single slot needs no lock; the atomic value keeps even a pathological
// concurrent write honest.
type logFields struct {
	user atomic.Value // string
}

func (f *logFields) setUserName(name string) { f.user.Store(name) }

func (f *logFields) userName() string {
	if v, ok := f.user.Load().(string); ok {
		return v
	}
	return "anonymous"
}

// recoverPanic converts handler panics into 500s and an error log line
// (architecture section 7.2). http.Server's own recover would abort
// the connection without a response body — unacceptable for an API. The
// body's shape follows the request's plane (T-33 review B1, same family
// as the authenticator's rejection split): /v2 answers the registry spec
// body with the api-version header, every other path the errors[] envelope.
//
// Mid-stream panics (T-14 review M1): when the response is already
// committed — a streaming download wrote body bytes before the panic —
// the status line and part of the body have left the process. Writing the
// 500 envelope then would APPEND JSON to a partial 200, handing the client
// a silently corrupted artifact. In that state recover only logs (and
// marks the status for the access log) and lets net/http truncate the
// connection: a broken download is retryable, a corrupted one is not.
func recoverPanic(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						// The handler asked for a silent transport abort
						// (client timeouts on streaming responses); re-panic
						// so net/http tears the connection down quietly.
						panic(rec)
					}
					logger.ErrorContext(r.Context(), "httpapi: panic recovered",
						slog.String("method", r.Method),
						slog.String("path", r.URL.EscapedPath()),
						slog.String("request_id", requestIDFrom(r.Context())),
						slog.Any("panic", rec),
					)
					if rec2, ok := w.(*statusRecorder); ok && rec2.committed() {
						rec2.markStatus(http.StatusInternalServerError)
						return
					}
					if isV2Plane(r.URL.EscapedPath()) {
						writeV2InternalError(w)
						return
					}
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// isV2Plane reports whether the path belongs to the /v2 registry plane
// (ADR-0010 clause 1). Used by the failure-rendering middlewares to pick
// the response contract; the path is the raw escaped spelling — the same
// value dispatch branches on, so the two can never disagree.
func isV2Plane(path string) bool {
	return path == "/v2" || strings.HasPrefix(path, "/v2/")
}

// writeV2InternalError renders the registry-plane 500 (spec body plus the
// api-version header). Kept next to the envelope writer so the two
// contracts stay visibly parallel.
func writeV2InternalError(w http.ResponseWriter) {
	hdr := w.Header()
	hdr.Set("Content-Type", "application/json")
	hdr.Set("Docker-Distribution-Api-Version", "registry/2.0")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(`{"errors":[{"code":"UNKNOWN","message":"internal server error","detail":null}]}` + "\n"))
}

// cors adds configurable cross-origin headers (architecture section 7.2:
// "CORS(可配)"). An empty origins list means same-origin policy: no headers
// are emitted, browsers enforce their default. A "*"-free explicit list
// echoes the requesting Origin only when listed; "*" (or an explicit list
// containing it) answers any origin, and credentials are allowed because
// BinFlow auth rides Authorization headers, not cookies.
func cors(allowedOrigins []string) Middleware {
	allowAny := false
	set := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAny = true
			continue
		}
		set[strings.ToLower(strings.TrimSpace(o))] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			switch {
			case origin == "":
				// Same-origin / non-browser client: no CORS headers needed.
			case allowAny:
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", "*")
				h.Set("Vary", "Origin")
			case set[strings.ToLower(origin)]:
				h := w.Header()
				h.Set("Access-Control-Allow-Origin", origin)
				h.Set("Vary", "Origin")
				h.Set("Access-Control-Allow-Credentials", "true")
			default:
				w.Header().Add("Vary", "Origin")
			}
			if origin != "" && r.Method == http.MethodOptions {
				h := w.Header()
				h.Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers",
					"Authorization, Content-Type, X-Checksum-Sha1, X-Checksum-Sha256, X-Checksum-Md5, X-Checksum-Deploy, X-JFrog-Art-Api, X-Explode-Archive")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routeAuth is the per-route authorization requirement the authorizer
// middleware enforces.
type routeAuth struct {
	// required: the request must carry a valid credential (management
	// plane; content writes). Anonymous yields a 401 challenge.
	required bool
	// admin: the credential must belong to an administrator (repository
	// mutation, user/token/permission management). A non-admin principal
	// yields 403.
	admin bool
	// oauth: failures on this route render the token plane's OAuth error
	// body ({"error","error_description"}) instead of the errors[]
	// envelope, per the PRD section 5.1 three-format split. Used by the
	// /api/security/token family, including its authorization denials.
	oauth bool
	// action is the Authorizer.Can action for content paths ("r", "w",
	// "d"). Empty means "no content-path check" (management plane checks
	// admin/permission inside its own handlers, T-15).
	action string
	// anonymous reads: the anonymous-open decision lives inside
	// auth.Authorizer.Can (it owns Security.AnonymousAccess), so this
	// struct needs no flag of its own — a nil principal with
	// action=read consults the flag there (ADR-0009).
}

// basicChallenge is the WWW-Authenticate response for missing credentials
// (FR-4-AC9/E-20). realm wording follows the generic adapter's deploy-time
// challenge.
const basicChallenge = `Basic realm="BinFlow Realm"`

// authenticate resolves the credential and stores the principal. A
// presented-but-rejected credential is NEVER downgraded to anonymous
// (probing with a stale token must not silently succeed where no token at
// all would fail); since T-33 review B1 the rejection is carried in the
// context as a signal and the ROUTING PLANE renders it — /binflow routes
// answer the errors[] envelope plus the Basic challenge (unchanged
// behavior), the /v2 registry plane answers the spec body plus the Bearer
// challenge (docker clients must not meet a Basic challenge mid-negotiation).
// A request with no credential at all stays anonymous and the route's own
// gate decides.
func authenticate(a auth.Authenticator) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := a.Authenticate(r.Context(), r)
			if err != nil {
				// Log the refusal for operators (the reason never reaches
				// the client), then hand the plane a decision to make.
				slog.WarnContext(r.Context(), "httpapi: credential rejected",
					slog.String("path", r.URL.EscapedPath()),
					slog.String("reason", err.Error()))
				next.ServeHTTP(w, r.WithContext(withAuthRejected(r.Context(), err.Error())))
				return
			}
			// Thread the resolved name outward to the access log (contexts
			// only flow inward, hence the mutable holder).
			if f, ok := r.Context().Value(ctxKeyLogFields).(*logFields); ok {
				f.setUserName(userName(p))
			}
			next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
		})
	}
}

// authorize enforces the route's requirement after authentication:
//
//   - required && anonymous         -> 401 challenge;
//   - admin && non-admin principal  -> 403;
//   - content action (r/w/d)        -> Authorizer.Can(principal, repo,
//     path, action); denied anonymous -> 401 challenge, denied
//     authenticated -> 403 (rest-api section 1.4 "403 -> 401 when
//     anonymous").
//
// Failure bodies follow the route's plane (PRD section 5.1 three-format
// split): the errors[] envelope by default, the OAuth shape on routes
// flagged oauth (the token family — its clients must be able to parse every
// non-2xx uniformly, auth-model.md section 3.1).
//
// The repoKey/path for the content check are resolved through the layout
// splitter — the same first-segment rule the dispatcher uses — so the
// authorization decision and the routing decision can never disagree on
// which repository a path addresses.
func authorize(a auth.Authorizer, req routeAuth) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := principalFrom(r.Context())
			if req.required && p == nil {
				w.Header().Set("WWW-Authenticate", basicChallenge)
				if req.oauth {
					writeOAuthError(w, http.StatusUnauthorized, "invalid_request", "authentication required")
					return
				}
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if req.admin && (p == nil || !p.Admin) {
				if req.oauth {
					writeOAuthError(w, http.StatusForbidden, "invalid_request", "administrator privileges required")
					return
				}
				writeError(w, http.StatusForbidden, "administrator privileges required")
				return
			}
			if req.action != "" {
				repoKey, rel := splitFirstSegment(r)
				if !a.Can(r.Context(), p, repoKey, rel, req.action) {
					if p == nil {
						w.Header().Set("WWW-Authenticate", basicChallenge)
						writeError(w, http.StatusUnauthorized, "authentication required")
						return
					}
					writeError(w, http.StatusForbidden, "permission denied")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// splitFirstSegment peels the first path segment (after /binflow) off the
// request path and DECODES it. Decoding the keying segment is what keeps
// the authorization check, the repository-row lookup and the adapter's
// own layout addressing one and the same string (T-14 review M2): a
// client spelling the key "generic%2Dlocal" would otherwise authorize and
// look up the escaped spelling while the adapter addresses the decoded
// one — two doors guarded against different names. Dot segments are
// deliberately NOT normalized here — the adapter layout stays the
// authority that rejects them (FR-4-AC10); the tail is returned in its
// raw escaped form and only the ACL keys off the decoded first segment.
//
// A malformed percent-escape in the segment (e.g. "gen%zz") fails
// PathUnescape: the segment is returned as-is so the adapter layout —
// which re-decodes with the same strictness — answers the 400.
func splitFirstSegment(r *http.Request) (repoKey, rel string) {
	rest := strings.TrimPrefix(r.URL.EscapedPath(), "/binflow")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		return "", ""
	}
	key, tail, _ := strings.Cut(rest, "/")
	if decoded, err := url.PathUnescape(key); err == nil {
		key = decoded
	}
	return key, tail
}
