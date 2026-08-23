package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultRetryMax is the default maximum number of retry attempts (exponential
// backoff: 1s, 2s, 4s, 8s, 16s).
const DefaultRetryMax = 5

// DefaultTimeout is the default per-request timeout.
const DefaultTimeout = 30 * time.Second

// DefaultBaseURL is the default BinFlow server base URL.
const DefaultBaseURL = "http://localhost:8080"

// Client is a typed HTTP client for the BinFlow management REST API.
// Zero value is usable (defaults to http.DefaultClient, DefaultBaseURL, no
// auth) but the typical construction is through New() or NewWithClient().
//
// The client targets the /binflow/api/** route family and does not import
// internal/storage, internal/metadata or internal/httpapi — it is a pure
// REST consumer.
type Client struct {
	// HTTP is the underlying http.Client. When nil, http.DefaultClient is used.
	HTTP *http.Client

	// BaseURL is the BinFlow server base URL (e.g., "http://localhost:8080").
	// No trailing slash. Defaults to DefaultBaseURL when empty.
	BaseURL string

	// Token is the fixed API token sent as the Authorization header.
	// When empty, no Authorization header is injected.
	Token string

	// RetryMax is the maximum number of retries for transient failures
	// (5xx, network errors). A zero value selects DefaultRetryMax; a
	// negative value disables retries entirely (exactly one attempt).
	RetryMax int

	// Progress is an optional callback invoked for upload progress. The
	// callback receives the total bytes expected (Content-Length, or -1
	// when unknown) and the number of bytes sent so far. It is called from
	// the request goroutine and must be short-running.
	Progress func(total, sent int64)
}

// New returns a Client with defaults: http.DefaultClient, DefaultBaseURL, no
// auth token, and DefaultRetryMax retries.
func New() *Client {
	return &Client{
		HTTP:     http.DefaultClient,
		BaseURL:  DefaultBaseURL,
		RetryMax: DefaultRetryMax,
	}
}

// NewWithClient returns a Client backed by the given http.Client, with the
// same defaults as New() for the other fields.
func NewWithClient(hc *http.Client) *Client {
	c := New()
	c.HTTP = hc
	return c
}

// httpClient returns the underlying http.Client, defaulting when nil.
func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

// baseURL returns the base URL, defaulting when empty.
func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

// retryMax returns the retry count, defaulting when zero.
func (c *Client) retryMax() int {
	if c.RetryMax == 0 {
		return DefaultRetryMax
	}
	if c.RetryMax < 0 {
		return 0
	}
	return c.RetryMax
}

// absURL joins the base URL with the given path (which should start with "/").
func (c *Client) absURL(p string) string {
	return c.baseURL() + p
}

// ---------------------------------------------------------------------------
// Request helpers
// ---------------------------------------------------------------------------

// newRequest creates an http.Request with the given method, path, and body.
// The Authorization header is injected when the client has a non-empty Token.
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.absURL(path), body)
	if err != nil {
		return nil, fmt.Errorf("client: build request %s %s: %w", method, path, err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// do executes a request with retry logic. It returns the parsed status error
// on non-2xx responses and the decoded JSON body on success.
func (c *Client) do(req *http.Request, out interface{}) error {
	body, err := c.doRaw(req)
	if err != nil {
		return err
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("client: decode response: %w", err)
	}
	return nil
}

// doRaw executes a request with retry logic and returns the raw response body.
func (c *Client) doRaw(req *http.Request) ([]byte, error) {
	var lastErr error
	retries := c.retryMax()

	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, 8s, 16s.
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		// Clone the request because the body may have been consumed.
		r := req.Clone(req.Context())
		if req.Body != nil {
			// Read the original body again for retry.
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("client: read request body for retry: %w", err)
			}
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := c.httpClient().Do(r)
		if err != nil {
			lastErr = fmt.Errorf("client: %s %s: %w", req.Method, req.URL.Path, err)
			if !isRetryable(err) {
				return nil, lastErr
			}
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if readErr != nil {
			lastErr = fmt.Errorf("client: read response body: %w", readErr)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return body, nil
		}

		// Parse the errors[] envelope.
		se := parseStatusError(resp.StatusCode, body)
		if resp.StatusCode < 500 {
			// 4xx: client error — do not retry.
			return nil, se
		}
		// 5xx: server error — retryable.
		lastErr = se
	}
	return nil, fmt.Errorf("client: retries exhausted after %d attempts: %w", retries+1, lastErr)
}

// isRetryable returns true for transient network errors that should be retried.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation is not retryable.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// DNS / network errors are retryable.
	msg := err.Error()
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "no such host")
}

// ---------------------------------------------------------------------------
// Error envelope
// ---------------------------------------------------------------------------

// StatusError is a parsed BinFlow errors[] envelope. It implements the error
// interface and carries the HTTP status code and server message.
type StatusError struct {
	StatusCode int
	Message    string
	Body       string // raw JSON body for diagnostics
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("client: HTTP %d: %s", e.StatusCode, e.Message)
}

// serverErrorEnvelope matches the server's errorEnvelope shape (envelope.go).
type serverErrorEnvelope struct {
	Errors []serverErrorEntry `json:"errors"`
}

type serverErrorEntry struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// parseStatusError decodes the server's errors[] envelope. If the body is not
// parseable as the envelope, it falls back to using the status code and raw
// body as the message.
func parseStatusError(statusCode int, body []byte) *StatusError {
	var env serverErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && len(env.Errors) > 0 {
		msg := env.Errors[0].Message
		if msg == "" {
			msg = http.StatusText(statusCode)
		}
		return &StatusError{StatusCode: statusCode, Message: msg, Body: string(body)}
	}
	return &StatusError{StatusCode: statusCode, Message: string(body), Body: string(body)}
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// looksLikeJSON reports whether a 2xx body is a JSON document (first
// non-space byte '{' or '['). Endpoints whose real success body is plain
// text (the repository mutation plane) use this to branch: the real server
// never sends JSON there, but a pre-alignment peer might — sniffing the
// body is deliberately more robust than trusting the Content-Type header.
func looksLikeJSON(body []byte) bool {
	return strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "{") ||
		strings.HasPrefix(strings.TrimLeft(string(body), " \t\r\n"), "[")
}

// getJSON performs a GET request and unmarshals the JSON response into out.
func (c *Client) getJSON(ctx context.Context, path string, out interface{}) error {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// postJSON performs a POST request with the given body (JSON-encoded) and
// unmarshals the response into out.
func (c *Client) postJSON(ctx context.Context, path string, body, out interface{}) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: marshal request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, http.MethodPost, path, r)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// putJSON performs a PUT request with the given body (JSON-encoded) and
// unmarshals the response into out.
func (c *Client) putJSON(ctx context.Context, path string, body, out interface{}) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("client: marshal request body: %w", err)
		}
		r = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, http.MethodPut, path, r)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// deleteJSON performs a DELETE request and unmarshals the response into out.
func (c *Client) deleteJSON(ctx context.Context, path string, out interface{}) error {
	req, err := c.newRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

// ---------------------------------------------------------------------------
// Upload helpers
// ---------------------------------------------------------------------------

// uploadFile uploads a file to the given path using PUT with the raw body. The
// Content-Type is set to application/octet-stream unless ct is non-empty. If
// the client has a Progress callback, it wraps the body in a progress reader.
func (c *Client) uploadFile(ctx context.Context, path string, body io.Reader, size int64, ct string) error {
	if ct == "" {
		ct = "application/octet-stream"
	}

	// Wrap with progress callback if configured.
	if c.Progress != nil {
		body = &progressReader{r: body, total: size, cb: c.Progress}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.absURL(path), body)
	if err != nil {
		return fmt.Errorf("client: build upload request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Content-Type", ct)
	if size > 0 {
		req.ContentLength = size
	}

	return c.do(req, nil)
}

// progressReader wraps an io.Reader and calls a callback after each Read.
type progressReader struct {
	r     io.Reader
	total int64
	sent  int64
	cb    func(total, sent int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	if p.cb != nil {
		p.cb(p.total, p.sent)
	}
	return n, err
}

// ---------------------------------------------------------------------------
// Convenience: URL-escaped path segments
// ---------------------------------------------------------------------------

// pathEscape returns a URL-escaped segment safe for path construction.
func pathEscape(segment string) string {
	return url.PathEscape(segment)
}

// EscapePathSegments percent-escapes every segment of a slash-separated
// path so a literal '%', '#', '?', space or non-ASCII name survives the URL
// round trip; the separating slashes are never escaped, while a '/' inside
// a segment becomes %2F. This is the single wire-side escaping contract for
// artifact paths: every content-plane and storage-plane URL this package
// builds goes through it. It is isomorphic to the migrate reader's private
// helper (internal/migrate reader.go escapePathSegments) — the path the
// reader lists and downloads with one spelling is the path the writer must
// PUT with the same spelling; a raw '%' instead makes url.Parse fail with
// `invalid URL escape` and loses the artifact (T-228 defect D-1, fixed by
// T-231). RFC 3986 is the governing public spec for the encoding itself;
// the reader's spelling was proven against a real Artifactory 7.84.10
// source during the T-228 migration run.
func EscapePathSegments(path string) string {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = pathEscape(s)
	}
	return strings.Join(segs, "/")
}
