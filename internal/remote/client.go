package remote

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Outbound defaults, per PRD v1.2 NFR-S13 with the T-79 errata (decision 3
// of ADR-0012 as amended): five redirect hops, a 15s socket timeout, a 64MB
// cap on buffered metadata-class responses; ADR-0012 decision 5 fixes the
// retry policy at two extra attempts with exponential backoff.
const (
	// DefaultSocketTimeout is the per-repository socketTimeoutSecs default
	// (repo-semantics section 7.1, socketTimeoutMillis 15000).
	DefaultSocketTimeout = 15 * time.Second
	// DefaultMaxRedirects is the redirect hop limit (T-79 errata: the
	// ADR-0012 draft value of 3 is superseded by the PRD's 5).
	DefaultMaxRedirects = 5
	// DefaultMaxBufferedBody caps buffered (metadata-class) responses:
	// packument, simple index, maven-metadata.xml and friends. Artifact
	// bodies stream and are not subject to this cap (NFR-S13 point 5).
	DefaultMaxBufferedBody int64 = 64 << 20 // 64MB
	// DefaultRetries is the number of extra attempts for idempotent
	// requests after a retriable transport failure.
	DefaultRetries = 2
	// DefaultRetryBackoff is the base delay of the exponential backoff
	// (200ms, then 400ms).
	DefaultRetryBackoff = 200 * time.Millisecond

	// userAgent identifies BinFlow to upstreams when the caller sets none.
	userAgent = "binflow-remote/1.0"
)

// Sentinel errors. The fetcher (T-66) maps these onto the FR-20 response
// matrix: chain rejections to 400, oversized buffered bodies to 502.
var (
	// ErrTooManyRedirects: the redirect chain exceeded MaxRedirects hops.
	ErrTooManyRedirects = errors.New("too many upstream redirects")
	// ErrBodyTooLarge: a buffered response exceeded MaxBufferedBody
	// (NFR-S13 point 5: over-limit buffered responses report 502).
	ErrBodyTooLarge = errors.New("upstream response exceeds buffered body limit")
	// ErrMethodNotSupported: the outbound surface is read-only — GET and
	// HEAD only. Remote repositories are never writable upstream
	// (FR-20-AC9), so nothing else may leave the process.
	ErrMethodNotSupported = errors.New("outbound method not supported")
)

// Options configures one outbound client, i.e. one remote repository's
// upstream policy. Zero-value fields fall back to the Default* constants.
type Options struct {
	// RepoKey names the repository in WARN audit lines.
	RepoKey string
	// BaseURL is the upstream base; Request.Path is joined onto it.
	BaseURL string
	// Username and Password are the upstream credential, passed through
	// as-is — decryption from at-rest storage is the fetcher's concern
	// (T-66, ADR-0012 decision 4). Both empty means anonymous.
	Username string
	Password string
	// TokenAuth is the remote repository's enableTokenAuthentication
	// (T-317, FR-101.1; repo-semantics 7.1 "enableTokenAuthentication 切
	// token 头"): when true the Password is a bearer TOKEN and travels as
	// "Authorization: Bearer <password>" instead of the Basic pair — the
	// smart-remote posture of authenticating to the upstream with a
	// reference token. It changes only the header spelling; the SSRF
	// chain, per-hop re-screening and credential-leak rules are identical.
	TokenAuth bool
	// AllowPrivateUpstream is the NFR-S13 exemption; it is granted at the
	// repository layer (admin-only, audited — T-64) and merely flows
	// through here.
	AllowPrivateUpstream bool
	// SocketTimeout <= 0 means DefaultSocketTimeout.
	SocketTimeout time.Duration
	// MaxRedirects: 0 means DefaultMaxRedirects; a negative value clamps
	// to 0 — redirects are then never followed and the first 30x response
	// is final.
	MaxRedirects int
	// MaxBufferedBody: 0 means DefaultMaxBufferedBody; a negative value
	// clamps to 0, which REMOVES the cap entirely. That is a test seam
	// only — production callers must always leave a positive value, since
	// the 502 contract for oversized metadata responses (NFR-S13 point 5)
	// is enforced here.
	MaxBufferedBody int64
	// Retries: extra attempts for idempotent requests. 0 means
	// DefaultRetries; negative clamps to 0 (no retries).
	Retries int
	// RetryBackoff <= 0 means DefaultRetryBackoff.
	RetryBackoff time.Duration
	// Logger receives chain WARN lines; nil means slog.Default().
	Logger *slog.Logger
	// Resolve overrides host resolution (nil = system resolver). Test
	// seam, injected into the guard.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// Client is the outbound HTTP client for one remote repository: a
// stdlib-only transport (ADR-0005 / ADR-0012 decision 5) whose every dial
// passes the NFR-S13 chain. A Client is safe for concurrent use.
type Client struct {
	opts       Options
	guard      *Guard
	httpClient *http.Client
	baseURL    *url.URL
}

// NewClient validates the base URL and assembles the guarded transport.
// The scheme assertion here mirrors the repository-creation check
// (FR-15-AC3) — private hosts are deliberately NOT screened at construction
// (T-79 errata point 5: IP screening happens at request time only, because
// DNS and network topology change).
func NewClient(opts Options) (*Client, error) {
	if opts.SocketTimeout <= 0 {
		opts.SocketTimeout = DefaultSocketTimeout
	}
	if opts.MaxRedirects == 0 {
		opts.MaxRedirects = DefaultMaxRedirects
	}
	if opts.MaxRedirects < 0 {
		opts.MaxRedirects = 0
	}
	if opts.MaxBufferedBody == 0 {
		opts.MaxBufferedBody = DefaultMaxBufferedBody
	}
	if opts.MaxBufferedBody < 0 {
		opts.MaxBufferedBody = 0
	}
	if opts.Retries == 0 {
		opts.Retries = DefaultRetries
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = DefaultRetryBackoff
	}

	var base *url.URL
	if opts.BaseURL != "" {
		u, err := url.Parse(opts.BaseURL)
		if err != nil {
			return nil, fmt.Errorf("remote %s: base url: %w", opts.RepoKey, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("remote %s: base url scheme %q: only http and https are allowed", opts.RepoKey, u.Scheme)
		}
		if u.Host == "" {
			return nil, fmt.Errorf("remote %s: base url has no host", opts.RepoKey)
		}
		base = u
	}

	guard := NewGuard(GuardOptions{
		RepoKey:              opts.RepoKey,
		AllowPrivateUpstream: opts.AllowPrivateUpstream,
		Logger:               opts.Logger,
		Resolve:              opts.Resolve,
	})
	transport := &http.Transport{
		// Every dial resolves, screens all addresses and pins a validated
		// IP (chain point 3); TLS verification keeps using the URL hostname
		// because the transport derives ServerName from the request, not
		// from what the custom dialer connects to.
		DialContext: guard.Dialer(opts.SocketTimeout),
		// Connect-adjacent timeouts all follow the repository's
		// socketTimeoutSecs; mid-body stalls are caught by the
		// idle-deadline connection wrapper instead of any whole-request
		// timeout (which would cap artifact streaming).
		TLSHandshakeTimeout:   opts.SocketTimeout,
		ResponseHeaderTimeout: opts.SocketTimeout,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       90 * time.Second,
		// HTTP/1.1 only: the idle-deadline conn wrapper must not gate
		// multiplexed h2 streams, and the upstream matrix (Maven Central,
		// npmjs, PyPI) serves h1.1 universally. Predictable timeout
		// semantics beat h2 head-of-line savings on a proxy path.
		ForceAttemptHTTP2: false,
		// Never negotiate compression: artifact bytes must flow through
		// bit-for-bit (architecture section 4.5 — landed content and the
		// served response are identical), and transparent gzip would
		// silently break upstream Content-Length/checksum relationships.
		DisableCompression: true,
	}
	return &Client{
		opts:  opts,
		guard: guard,
		httpClient: &http.Client{
			Transport: transport,
			// Redirects are followed manually so that every hop re-runs the
			// full chain before connecting (NFR-S13 point 4).
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL: base,
	}, nil
}

// CloseIdleConnections releases pooled upstream connections (repository
// deletion / shutdown path).
func (c *Client) CloseIdleConnections() {
	c.httpClient.CloseIdleConnections()
}

// Request is one outbound call. URL wins when set; otherwise BaseURL and
// Path are joined.
type Request struct {
	// Method: GET or HEAD (default GET). Anything else fails with
	// ErrMethodNotSupported before any packet leaves the process.
	Method string
	// Path is the repository-relative path joined onto BaseURL.
	Path string
	// URL is an absolute-URL override; it is still fully guard-checked.
	URL string
	// Header carries extra upstream headers (conditional-GET validators
	// from the fetcher, Accept negotiation, ...). A repository's
	// credentials, when configured, overwrite any Authorization entry.
	Header http.Header
}

// Result is a fully buffered upstream response, for metadata-class content
// (npm packument, PyPI simple index, maven-metadata.xml — NFR-S13 point 5).
// Non-2xx statuses are returned as-is: only transport and chain failures
// are errors; interpreting statuses is the fetcher's state machine (T-66).
type Result struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
	// FinalURL is the URL of the last hop actually served (after manual
	// redirect following).
	FinalURL string
}

// StreamResult is an unbounded streaming response for artifact-class
// content. The caller must Close Body; BytesRead reports how many body
// bytes have been read so far (streamed-byte counter for the fetcher's
// stats logging, ADR-0012 decision 5).
type StreamResult struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       io.ReadCloser
	// FinalURL is the URL of the last hop actually served.
	FinalURL string

	counted *countingBody
}

// BytesRead reports the number of body bytes read so far; safe to call
// while another goroutine drains Body.
func (s *StreamResult) BytesRead() int64 {
	if s.counted == nil {
		return 0
	}
	return s.counted.n.Load()
}

// countingBody wraps a response body with an atomic byte counter.
type countingBody struct {
	io.ReadCloser
	n atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n.Add(int64(n))
	return n, err
}

// JoinURL joins an upstream base URL and a repository-relative path
// ("https://up.example/ma" + "junit/junit/4.13.2/a.jar"), collapsing
// redundant slashes on either side of the seam.
func JoinURL(base, path string) string {
	b := strings.TrimRight(base, "/")
	if path == "" {
		return b
	}
	return b + "/" + strings.TrimLeft(path, "/")
}

// Fetch performs the request and buffers the response, enforcing the
// MaxBufferedBody cap (64MB by default): an over-limit response fails with
// an error wrapping ErrBodyTooLarge — the fetcher truncates and reports
// 502 (NFR-S13 point 5).
func (c *Client) Fetch(ctx context.Context, req Request) (*Result, error) {
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	limit := c.opts.MaxBufferedBody
	// NFR-S13 point 5 caps buffered BODIES, and a HEAD carries no body:
	// its declared Content-Length describes the artifact being probed
	// (the fetcher's revalidation path), so probing an artifact larger
	// than the cap must not fail the probe (review B4).
	isHead := requestMethod(req) == http.MethodHead
	if limit > 0 && !isHead && resp.ContentLength > limit {
		// Fail fast on a declared size without moving the excess bytes.
		return nil, fmt.Errorf("%w: declared %d exceeds %d bytes",
			ErrBodyTooLarge, resp.ContentLength, limit)
	}
	var body []byte
	if limit > 0 {
		body, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
	} else {
		body, err = io.ReadAll(resp.Body)
	}
	if err != nil {
		return nil, fmt.Errorf("remote %s: read body: %w", c.opts.RepoKey, err)
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, fmt.Errorf("%w: exceeded %d bytes", ErrBodyTooLarge, limit)
	}
	return &Result{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       body,
		FinalURL:   resp.Request.URL.String(),
	}, nil
}

// Stream performs the request and returns the response with an unbounded,
// byte-counting body for artifact-class content. The caller must Close
// Body; mid-body stalls abort after the socket timeout.
func (c *Client) Stream(ctx context.Context, req Request) (*StreamResult, error) {
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}
	counted := &countingBody{ReadCloser: resp.Body}
	return &StreamResult{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       counted,
		FinalURL:   resp.Request.URL.String(),
		counted:    counted,
	}, nil
}

// requestMethod resolves the outbound verb: GET by default, upper-cased
// and trimmed.
func requestMethod(req Request) string {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		return http.MethodGet
	}
	return method
}

// send runs the request through the manual redirect loop. Chain points 1
// and 2 re-run for every hop before any connection is attempted (NFR-S13
// point 4); chain point 3 runs inside the transport's guarded dial.
func (c *Client) send(ctx context.Context, req Request) (*http.Response, error) {
	method := requestMethod(req)
	if method != http.MethodGet && method != http.MethodHead {
		return nil, fmt.Errorf("remote %s: method %s: %w", c.opts.RepoKey, method, ErrMethodNotSupported)
	}
	raw, err := c.targetURL(req)
	if err != nil {
		return nil, err
	}
	cur, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("remote %s: target url: %w", c.opts.RepoKey, err)
	}
	header := req.Header.Clone()
	if header == nil {
		header = http.Header{}
	}
	c.applyBasicAuth(header)
	return c.follow(ctx, method, cur, header, c.roundTrip)
}

// hopFunc issues one attempt (with retry) at one hop; a seam so tests can
// drive the redirect loop's per-hop screening matrix without a reachable
// public first hop — a loopback httptest upstream cannot play that role,
// and the exemption flag would otherwise mask exactly the denials under
// test.
type hopFunc func(ctx context.Context, method, rawURL string, header http.Header) (*http.Response, error)

// follow is the manual redirect loop: every hop — the initial URL and each
// redirect target — re-runs chain points 1 and 2 before hop is invoked, so
// a forbidden target is denied without a single outbound packet, up to
// MaxRedirects followed hops.
func (c *Client) follow(ctx context.Context, method string, start *url.URL, header http.Header, hop hopFunc) (*http.Response, error) {
	cur := start
	redirects := 0
	for {
		if err := c.guard.CheckURL(ctx, cur.String()); err != nil {
			return nil, fmt.Errorf("remote %s: hop %d: %w", c.opts.RepoKey, redirects, err)
		}
		resp, err := hop(ctx, method, cur.String(), header)
		if err != nil {
			return nil, err
		}
		location := resp.Header.Get("Location")
		if !redirectable(resp.StatusCode) || location == "" {
			return resp, nil
		}
		drain(resp)
		next, err := cur.Parse(location)
		if err != nil {
			return nil, fmt.Errorf("remote %s: redirect location %q: %w", c.opts.RepoKey, location, err)
		}
		redirects++
		if redirects > c.opts.MaxRedirects {
			return nil, fmt.Errorf("remote %s: exceeded %d redirect hops: %w",
				c.opts.RepoKey, c.opts.MaxRedirects, ErrTooManyRedirects)
		}
		if next.Host != cur.Host {
			// Credentials configured for the upstream must never leak to a
			// different host on a redirect (browser and Go stdlib
			// behavior; the spec is silent — BinFlow security default).
			header.Del("Authorization")
		}
		// A Location URL must never smuggle userinfo either: net/http
		// turns URL userinfo into a Basic Authorization header on the next
		// hop, handing an upstream-controlled "credential" to whatever
		// host the chain lands on (review follow-up #4).
		next.User = nil
		// The outbound surface is GET/HEAD only, so the RFC's 301/302/303
		// POST-to-GET rewrite rules never apply: the method is carried
		// across hops unchanged (307/308 semantics for every hop).
		cur = next
	}
}

// roundTrip issues one attempt (with retry) at one hop.
func (c *Client) roundTrip(ctx context.Context, method, rawURL string, header http.Header) (*http.Response, error) {
	backoff := c.opts.RetryBackoff
	var lastErr error
	for attempt := 0; attempt <= c.opts.Retries; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff); err != nil {
				return nil, fmt.Errorf("remote %s: retry abandoned: %w", c.opts.RepoKey, err)
			}
			backoff *= 2
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return nil, fmt.Errorf("remote %s: build request: %w", c.opts.RepoKey, err)
		}
		req.Header = header.Clone()
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", userAgent)
		}
		resp, err := c.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !c.retriable(err) {
			return nil, fmt.Errorf("remote %s: upstream transport: %w", c.opts.RepoKey, err)
		}
	}
	return nil, fmt.Errorf("remote %s: upstream unreachable after %d attempts: %w",
		c.opts.RepoKey, c.opts.Retries+1, lastErr)
}

// retriable reports whether a transport failure is worth another idempotent
// attempt: connection-level breakage only. Timeouts are deliberately NOT
// retried — at 15s per attempt they must surface promptly to the fetcher's
// assumed-offline path instead of tripling client latency (the spec is
// silent on retrying timeouts; flagged as a conservative call in the T-65
// report). Chain rejections and caller cancellation are terminal.
func (c *Client) retriable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if IsRejection(err) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	// HTTP statuses are never retried here either: the fetcher's upstream
	// hit-count assertions (M41/M43/M44) must observe every attempt.
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed)
}

func (c *Client) targetURL(req Request) (string, error) {
	if req.URL != "" {
		return req.URL, nil
	}
	if c.baseURL == nil {
		return "", fmt.Errorf("remote %s: no base url configured and request has no url", c.opts.RepoKey)
	}
	return JoinURL(c.baseURL.String(), req.Path), nil
}

// applyBasicAuth sets the Authorization header from the repository's stored
// credentials — pass-through only (decryption is the fetcher's, T-66). With
// Options.TokenAuth (enableTokenAuthentication, T-317) the password IS the
// token and rides as a Bearer credential; a token-auth repository without a
// password stays anonymous (there is no token to present).
func (c *Client) applyBasicAuth(h http.Header) {
	if c.opts.TokenAuth {
		if c.opts.Password == "" {
			return
		}
		h.Set("Authorization", "Bearer "+c.opts.Password)
		return
	}
	if c.opts.Username == "" && c.opts.Password == "" {
		return
	}
	h.Set("Authorization", "Basic "+
		base64.StdEncoding.EncodeToString([]byte(c.opts.Username+":"+c.opts.Password)))
}

// drain discards a redirect body (bounded) so the pooled connection can be
// reused, then closes it.
func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	_ = resp.Body.Close()
}

func redirectable(status int) bool {
	switch status {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
