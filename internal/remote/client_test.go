package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// hitServer counts requests that reached the handler, so tests can assert
// the negative condition at the heart of M42: a rejected target must see
// zero upstream traffic.
type hitServer struct {
	*httptest.Server
	hits *atomic.Int64
}

func newHitServer(t *testing.T, h http.HandlerFunc) *hitServer {
	t.Helper()
	hits := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)
	return &hitServer{Server: srv, hits: hits}
}

// newClient builds a client against a loopback httptest upstream; the
// exemption flag is on because httptest binds 127.0.0.1 — allowed tests
// must reach it, denied tests must be stopped before connecting.
func newClient(t *testing.T, baseURL string, mutate func(*Options)) *Client {
	t.Helper()
	opts := Options{
		RepoKey:              "test-remote",
		BaseURL:              baseURL,
		AllowPrivateUpstream: true,
		RetryBackoff:         time.Millisecond,
		Logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&opts)
	}
	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestClientDeniesLoopbackWithoutExemption is the M42 core scenario: a
// repository pointing at 127.0.0.1 is created fine (scheme is valid) but
// every request is denied pre-connect with a WARN audit line, and the
// upstream logs zero hits.
func TestClientDeniesLoopbackWithoutExemption(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	})
	logger, buf := testLogger()
	c := newClient(t, up.URL, func(o *Options) {
		o.AllowPrivateUpstream = false
		o.Logger = logger
	})
	res, err := c.Fetch(context.Background(), Request{Path: "dir/up.bin"})
	if err == nil {
		t.Fatalf("fetch to loopback upstream without exemption succeeded: %d bytes", len(res.Body))
	}
	var rej *RejectionError
	if !errors.As(err, &rej) {
		t.Fatalf("error %v is not a *RejectionError", err)
	}
	if rej.Category != CategoryLoopback || rej.RepoKey != "test-remote" {
		t.Errorf("category/repo = %q/%q, want loopback/test-remote", rej.Category, rej.RepoKey)
	}
	if n := up.hits.Load(); n != 0 {
		t.Errorf("upstream saw %d requests, want 0 (denied pre-connect)", n)
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "repo=test-remote") {
		t.Errorf("missing WARN audit line: %q", out)
	}
}

// TestClientBlockedIPMatrix drives the client against every literal class
// the PRD enumerates for M42 (plus broadcast/reserved): all are denied at
// the pre-connect check.
func TestClientBlockedIPMatrix(t *testing.T) {
	for _, tc := range []struct{ name, url, cat string }{
		{"loopback", "http://127.0.0.1:9099/x", CategoryLoopback},
		{"rfc1918 10", "http://10.1.2.3/x", CategoryPrivateV4},
		{"rfc1918 192.168", "http://192.168.1.1/x", CategoryPrivateV4},
		{"metadata service", "http://169.254.169.254/latest/meta-data/", CategoryLinkLocal},
		{"ipv6 loopback", "http://[::1]:8080/x", CategoryLoopback},
		{"unspecified", "http://0.0.0.0/x", CategoryUnspecified},
		{"multicast", "http://224.0.0.5/x", CategoryMulticast},
		{"broadcast", "http://255.255.255.255/x", CategoryBroadcast},
		{"reserved", "http://240.0.0.9/x", CategoryReserved},
		// Review B1/B2: transition formats and zones are denied at the
		// client door as well, not only inside the guard.
		{"nat64 wraps metadata service", "http://[64:ff9b::a9fe:a9fe]/x", CategoryLinkLocal},
		{"nat64 wraps loopback", "http://[64:ff9b::7f00:1]/x", CategoryLoopback},
		{"6to4 wraps loopback", "http://[2002:7f00:1::]/x", CategoryLoopback},
		{"teredo", "http://[2001:0::7f00:1]/x", CategoryTeredo},
		{"ipv4-compatible wraps loopback", "http://[::7f00:1]/x", CategoryLoopback},
		{"zone wraps link-local", "http://[fe80::1%25en0]/x", CategoryLinkLocal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newClient(t, "", func(o *Options) {
				o.AllowPrivateUpstream = false
				o.BaseURL = tc.url
			})
			_, err := c.Fetch(context.Background(), Request{Path: "x"})
			if err == nil {
				t.Fatalf("fetch to %s succeeded, want denial", tc.url)
			}
			var rej *RejectionError
			if !errors.As(err, &rej) || rej.Category != tc.cat {
				t.Fatalf("error = %v, want rejection %q", err, tc.cat)
			}
		})
	}
}

// TestClientRedirectSchemeDeniedEvenUnderExemption: the scheme chain point
// (NFR-S13 point 1) holds on every hop even for an exempted repository —
// an httptest upstream (exemption on, loopback) redirecting to file:// or
// gopher:// is refused before the second hop connects.
func TestClientRedirectSchemeDeniedEvenUnderExemption(t *testing.T) {
	for _, tc := range []struct{ name, location string }{
		{"302 to file scheme", "file:///etc/passwd"},
		{"302 to gopher scheme", "gopher://127.0.0.1:70/x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, tc.location, http.StatusFound)
					return
				}
				_, _ = w.Write([]byte("should not be reached"))
			})
			logger, buf := testLogger()
			c := newClient(t, up.URL, func(o *Options) { o.Logger = logger })
			_, err := c.Fetch(context.Background(), Request{Path: "start"})
			if err == nil {
				t.Fatalf("redirect to %q followed, want denial", tc.location)
			}
			var rej *RejectionError
			if !errors.As(err, &rej) || rej.Category != CategoryScheme {
				t.Fatalf("error = %v, want scheme rejection", err)
			}
			if n := up.hits.Load(); n != 1 {
				t.Errorf("upstream hits = %d, want 1 (only the initial hop)", n)
			}
			if !strings.Contains(buf.String(), "level=WARN") {
				t.Errorf("redirect denial not WARN-logged: %q", buf.String())
			}
		})
	}
}

// TestClientRedirectPrivateFollowedUnderExemption documents the exemption's
// redirect semantics: an allowPrivateUpstream repository may follow a
// redirect into private space (that is what the flag means — internal
// Nexus chains); the attempt is a plain transport error, never a chain
// rejection. Port 1 on loopback is closed, so the followed hop fails fast
// with ECONNREFUSED.
func TestClientRedirectPrivateFollowedUnderExemption(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://127.0.0.1:1/sink", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("unreachable"))
	})
	c := newClient(t, up.URL, nil) // exemption is the newClient default
	_, err := c.Fetch(context.Background(), Request{Path: "start"})
	if err == nil {
		t.Fatal("fetch to a closed port succeeded")
	}
	if IsRejection(err) {
		t.Fatalf("exempted private redirect was rejected by the chain: %v", err)
	}
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Errorf("err = %v, want connection refused (hop was actually attempted)", err)
	}
	if n := up.hits.Load(); n != 1 {
		t.Errorf("upstream hits = %d, want 1", n)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func hopResponse(forURL, location string) *http.Response {
	u, _ := url.Parse(forURL) // forURL came from url.URL.String(); always valid
	resp := &http.Response{
		Header:  http.Header{},
		Body:    io.NopCloser(strings.NewReader("")),
		Request: &http.Request{URL: u},
	}
	if location != "" {
		resp.StatusCode = http.StatusFound
		resp.Header.Set("Location", location)
	} else {
		resp.StatusCode = http.StatusOK
	}
	return resp
}

// TestFollowRedirectScreensEveryHop is the M42 redirect half of the matrix:
// with the exemption off, the loop re-runs the full chain on every hop and
// refuses the forbidden target before the second hop is invoked. The hop
// seam stands in for the transport because a loopback httptest upstream
// cannot play a public hop 0 — the exemption that makes it reachable would
// mask exactly the denials under test.
func TestFollowRedirectScreensEveryHop(t *testing.T) {
	for _, tc := range []struct {
		name     string
		location string
		resolve  func(context.Context, string) ([]netip.Addr, error)
		cat      string
	}{
		{name: "302 to metadata service", location: "http://169.254.169.254/latest/meta-data/", cat: CategoryLinkLocal},
		{name: "302 to rfc1918 10/8", location: "http://10.9.8.7/x", cat: CategoryPrivateV4},
		{name: "302 to rfc1918 192.168/16", location: "http://192.168.0.9/x", cat: CategoryPrivateV4},
		{name: "302 to loopback", location: "http://127.0.0.1:9099/x", cat: CategoryLoopback},
		{name: "302 to ::1", location: "http://[::1]/x", cat: CategoryLoopback},
		{name: "302 to 0.0.0.0", location: "http://0.0.0.0/x", cat: CategoryUnspecified},
		{name: "302 to nat64-wrapped metadata service", location: "http://[64:ff9b::a9fe:a9fe]/latest/", cat: CategoryLinkLocal},
		{name: "302 to 6to4-wrapped loopback", location: "http://[2002:7f00:1::]/x", cat: CategoryLoopback},
		{
			name:     "302 to hostname resolving private",
			location: "http://internal-upstream.test/latest",
			resolve: func(_ context.Context, host string) ([]netip.Addr, error) {
				if host == "internal-upstream.test" {
					return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
				}
				return publicAddrs(), nil
			},
			cat: CategoryPrivateV4,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolve := tc.resolve
			if resolve == nil {
				resolve = func(context.Context, string) ([]netip.Addr, error) { return publicAddrs(), nil }
			}
			logger, buf := testLogger()
			c := newClient(t, "http://public-upstream.test", func(o *Options) {
				o.AllowPrivateUpstream = false
				o.Logger = logger
				o.Resolve = resolve
			})
			var hops atomic.Int32
			_, err := c.follow(context.Background(), http.MethodGet,
				mustParseURL(t, "http://public-upstream.test/start"), http.Header{},
				func(_ context.Context, _ string, rawURL string, _ http.Header) (*http.Response, error) {
					hops.Add(1)
					if strings.HasSuffix(rawURL, "/start") {
						return hopResponse(rawURL, tc.location), nil
					}
					return hopResponse(rawURL, ""), nil
				})
			if err == nil {
				t.Fatalf("redirect to %q followed, want denial", tc.location)
			}
			var rej *RejectionError
			if !errors.As(err, &rej) || rej.Category != tc.cat {
				t.Fatalf("error = %v, want rejection %q", err, tc.cat)
			}
			if n := hops.Load(); n != 1 {
				t.Errorf("hops invoked = %d, want 1 (target screened before the hop)", n)
			}
			if !strings.Contains(buf.String(), "level=WARN") {
				t.Errorf("hop denial not WARN-logged: %q", buf.String())
			}
		})
	}
}

// TestFollowRedirectDropsCrossHostCredentials: within the loop, credentials
// leave with hop 1 only when hop 2 is the same host.
func TestFollowRedirectDropsCrossHostCredentials(t *testing.T) {
	c := newClient(t, "", func(o *Options) {
		o.Username, o.Password = "alice", "s3cret"
		o.BaseURL = "http://upstream.test"
		o.Resolve = func(context.Context, string) ([]netip.Addr, error) {
			return publicAddrs(), nil
		}
	})
	header := http.Header{}
	c.applyBasicAuth(header)
	var hop1Auth, hop2Auth atomic.Value
	_, err := c.follow(context.Background(), http.MethodGet,
		mustParseURL(t, "http://upstream.test/start"), header,
		func(_ context.Context, _ string, rawURL string, _ http.Header) (*http.Response, error) {
			if strings.HasSuffix(rawURL, "/start") {
				hop1Auth.Store(header.Get("Authorization"))
				return hopResponse(rawURL, "http://other-upstream.test/sink"), nil
			}
			hop2Auth.Store(header.Get("Authorization"))
			return hopResponse(rawURL, ""), nil
		})
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	if got, _ := hop1Auth.Load().(string); got == "" {
		t.Error("hop 1 (the configured upstream) saw no credentials")
	}
	if got, _ := hop2Auth.Load().(string); got != "" {
		t.Errorf("cross-host hop received Authorization %q, want none", got)
	}
}

// TestClientRedirectFollowsSameOrigin: same-origin redirects are followed
// (FR-20-AC12: "302 → 公网同源路径 → 跟随成功"), the final URL is reported,
// and credentials survive a same-host hop.
func TestClientRedirectFollowsSameOrigin(t *testing.T) {
	var sawAuth atomic.Bool
	up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/one":
			http.Redirect(w, r, "/two", http.StatusFound) // relative form
		case "/two":
			http.Redirect(w, r, upURL(r)+"/three", http.StatusFound) // absolute form
		default:
			sawAuth.Store(r.Header.Get("Authorization") != "")
			_, _ = w.Write([]byte("final"))
		}
	})
	c := newClient(t, up.URL, func(o *Options) {
		o.Username, o.Password = "alice", "s3cret"
	})
	res, err := c.Fetch(context.Background(), Request{Path: "one"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(res.Body) != "final" {
		t.Errorf("body = %q, want %q", res.Body, "final")
	}
	if !strings.HasSuffix(res.FinalURL, "/three") {
		t.Errorf("final url = %q, want /three", res.FinalURL)
	}
	if !sawAuth.Load() {
		t.Error("Basic credentials were dropped on a same-host redirect")
	}
	if n := up.hits.Load(); n != 3 {
		t.Errorf("upstream hits = %d, want 3", n)
	}
}

func upURL(r *http.Request) string {
	return "http://" + r.Host
}

// TestClientRedirectHopLimit: five hops are followed, the sixth is refused
// (T-79 errata: the limit is 5, not the ADR draft's 3).
func TestClientRedirectHopLimit(t *testing.T) {
	redirect := func(w http.ResponseWriter, r *http.Request, stopAt int) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/hop/%d", &n); err != nil {
			t.Errorf("bad path %q", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if n >= stopAt {
			_, _ = w.Write([]byte("done"))
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", n+1), http.StatusFound)
	}
	t.Run("five hops allowed then final", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
			redirect(w, r, 5)
		})
		c := newClient(t, up.URL, nil)
		res, err := c.Fetch(context.Background(), Request{Path: "hop/0"})
		if err != nil {
			t.Fatalf("fetch through 5 hops: %v", err)
		}
		if string(res.Body) != "done" || !strings.HasSuffix(res.FinalURL, "/hop/5") {
			t.Errorf("body/url = %q/%q, want done//hop/5", res.Body, res.FinalURL)
		}
		if n := up.hits.Load(); n != 6 {
			t.Errorf("upstream hits = %d, want 6 (initial + 5 hops)", n)
		}
	})
	t.Run("sixth hop refused", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
			redirect(w, r, 50)
		})
		c := newClient(t, up.URL, nil)
		_, err := c.Fetch(context.Background(), Request{Path: "hop/0"})
		if !errors.Is(err, ErrTooManyRedirects) {
			t.Fatalf("err = %v, want ErrTooManyRedirects", err)
		}
		if n := up.hits.Load(); n != 6 {
			t.Errorf("upstream hits = %d, want 6 (initial + 5 followed hops)", n)
		}
	})
}

// TestClientBufferedBodyLimit: metadata-class responses are capped at
// MaxBufferedBody — both the declared-length fast path and the chunked
// read path (NFR-S13 point 5; the fetcher maps this to 502).
func TestClientBufferedBodyLimit(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 2048)
	t.Run("declared content length over limit", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			_, _ = w.Write(payload)
		})
		c := newClient(t, up.URL, func(o *Options) { o.MaxBufferedBody = 1024 })
		_, err := c.Fetch(context.Background(), Request{Path: "packument.json"})
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("err = %v, want ErrBodyTooLarge", err)
		}
	})
	t.Run("chunked body over limit", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK) // no Content-Length: chunked
			_, _ = w.Write(payload)
		})
		c := newClient(t, up.URL, func(o *Options) { o.MaxBufferedBody = 1024 })
		_, err := c.Fetch(context.Background(), Request{Path: "packument.json"})
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("err = %v, want ErrBodyTooLarge", err)
		}
	})
	t.Run("exactly at limit passes", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(bytes.Repeat([]byte("a"), 1024))
		})
		c := newClient(t, up.URL, func(o *Options) { o.MaxBufferedBody = 1024 })
		res, err := c.Fetch(context.Background(), Request{Path: "packument.json"})
		if err != nil {
			t.Fatalf("fetch at limit: %v", err)
		}
		if len(res.Body) != 1024 {
			t.Errorf("body length = %d, want 1024", len(res.Body))
		}
	})
	// Review B4: the cap governs buffered BODIES. A HEAD declares the size
	// of the artifact being probed without carrying a body, so probing an
	// artifact larger than the cap must not fail (the fetcher's
	// revalidation path would otherwise see phantom 502s).
	t.Run("head with large declared content length passes", func(t *testing.T) {
		up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "8589934592") // 8GB artifact
		})
		c := newClient(t, up.URL, func(o *Options) { o.MaxBufferedBody = 1024 })
		res, err := c.Fetch(context.Background(), Request{Method: "HEAD", Path: "huge.jar"})
		if err != nil {
			t.Fatalf("HEAD probe of oversized artifact: %v", err)
		}
		if len(res.Body) != 0 {
			t.Errorf("HEAD body = %d bytes, want 0", len(res.Body))
		}
		if got := res.Header.Get("Content-Length"); got != "8589934592" {
			t.Errorf("declared length = %q, want passthrough", got)
		}
	})
}

// TestClientDefaults pins the T-79 errata parameter face: 15s socket
// timeout, 5 redirect hops, 64MB buffered cap, 2 retries.
func TestClientDefaults(t *testing.T) {
	c, err := NewClient(Options{RepoKey: "d", BaseURL: "http://upstream.example.com"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.opts.SocketTimeout != 15*time.Second {
		t.Errorf("socket timeout = %v, want 15s", c.opts.SocketTimeout)
	}
	if c.opts.MaxRedirects != 5 {
		t.Errorf("max redirects = %d, want 5", c.opts.MaxRedirects)
	}
	if c.opts.MaxBufferedBody != int64(64<<20) {
		t.Errorf("max buffered body = %d, want %d", c.opts.MaxBufferedBody, 64<<20)
	}
	if c.opts.Retries != 2 {
		t.Errorf("retries = %d, want 2", c.opts.Retries)
	}
	if DefaultSocketTimeout != 15*time.Second || DefaultMaxRedirects != 5 ||
		DefaultMaxBufferedBody != int64(64<<20) || DefaultRetries != 2 {
		t.Error("Default* constants drifted from the PRD v1.2 values")
	}
}

// TestNewClientSchemeAssertion: construction rejects non-http base URLs —
// the request-time mirror of the FR-15-AC3 creation check.
func TestNewClientSchemeAssertion(t *testing.T) {
	for _, base := range []string{"file:///etc", "ftp://x", "gopher://y", "http://"} {
		if _, err := NewClient(Options{RepoKey: "d", BaseURL: base}); err == nil {
			t.Errorf("NewClient(%q) succeeded, want error", base)
		}
	}
	if _, err := NewClient(Options{RepoKey: "d", BaseURL: "http://10.0.0.1:9099"}); err != nil {
		t.Errorf("NewClient with private base url failed at construction: %v (IP screening is request-time only, T-79 errata 5)", err)
	}
}

// TestClientBasicAuthPassthrough: stored credentials travel as a Basic
// header, verbatim (pass-through; decryption belongs to T-66).
func TestClientBasicAuthPassthrough(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "alice" || pass != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	c := newClient(t, up.URL, func(o *Options) { o.Username, o.Password = "alice", "s3cret" })
	res, err := c.Fetch(context.Background(), Request{Path: "x"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
}

// TestClientRedirectCrossHostDropsCredentials: repository credentials must
// never leak to a different upstream host on a redirect (BinFlow security
// default; the spec is silent). The two httptest servers are both on
// 127.0.0.1 but different ports, so url.Host differs.
func TestClientRedirectCrossHostDropsCredentials(t *testing.T) {
	var targetAuth atomic.Value
	target := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		targetAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("cross"))
	})
	source := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			if r.Header.Get("Authorization") == "" {
				t.Error("source hop (the configured upstream) saw no credentials")
			}
			http.Redirect(w, r, target.URL+"/sink", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("source"))
	})
	c := newClient(t, source.URL, func(o *Options) { o.Username, o.Password = "alice", "s3cret" })
	res, err := c.Fetch(context.Background(), Request{Path: "start"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.StatusCode != http.StatusOK || string(res.Body) != "cross" {
		t.Fatalf("status/body = %d/%q, want 200/cross", res.StatusCode, res.Body)
	}
	if got, _ := targetAuth.Load().(string); got != "" {
		t.Errorf("cross-host hop received Authorization %q, want none", got)
	}
}

// TestClientRedirectLocationUserinfoStripped: net/http turns URL userinfo
// into a Basic Authorization header on the next hop, so an upstream that
// controls a Location could inject credentials into the following request.
// The loop strips userinfo from every redirect target (review follow-up
// #4); this anonymous client must arrive at the sink with no Authorization
// header at all.
func TestClientRedirectLocationUserinfoStripped(t *testing.T) {
	var sinkAuth atomic.Value
	up := newHitServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "http://injected:secret@"+r.Host+"/sink", http.StatusFound)
			return
		}
		sinkAuth.Store(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("sink"))
	})
	c := newClient(t, up.URL, nil) // anonymous: no credentials configured
	res, err := c.Fetch(context.Background(), Request{Path: "start"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if res.StatusCode != http.StatusOK || string(res.Body) != "sink" {
		t.Fatalf("status/body = %d/%q, want 200/sink", res.StatusCode, res.Body)
	}
	if got, _ := sinkAuth.Load().(string); got != "" {
		t.Errorf("Location userinfo leaked as Authorization %q, want none", got)
	}
}

// TestClientStreamCounter: artifact bodies stream unbounded and the byte
// counter follows the reader (ADR-0012 decision 5, streamed-byte counter).
func TestClientStreamCounter(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64*1024/16) // 64KiB
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	c := newClient(t, up.URL, nil)
	stream, err := c.Stream(context.Background(), Request{Path: "big.jar"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Body.Close() }()
	half := make([]byte, 1024)
	if _, err := io.ReadFull(stream.Body, half); err != nil {
		t.Fatalf("partial read: %v", err)
	}
	if n := stream.BytesRead(); n != 1024 {
		t.Errorf("bytes read after partial read = %d, want 1024", n)
	}
	rest, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := len(half) + len(rest); got != len(payload) {
		t.Errorf("total bytes = %d, want %d", got, len(payload))
	}
	if n := stream.BytesRead(); n != int64(len(payload)) {
		t.Errorf("final byte count = %d, want %d", n, len(payload))
	}
}

// TestClientRetryOnConnectionReset: an idempotent GET is retried twice with
// exponential backoff when the upstream breaks the connection before
// responding (ADR-0012 decision 5).
func TestClientRetryOnConnectionReset(t *testing.T) {
	var up *hitServer
	up = newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if up.hits.Load() <= 2 { // first two attempts: break the pipe
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("server does not support hijacking")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte("third time lucky"))
	})
	c := newClient(t, up.URL, func(o *Options) {
		o.Retries = 2
		o.RetryBackoff = time.Millisecond
	})
	res, err := c.Fetch(context.Background(), Request{Path: "x"})
	if err != nil {
		t.Fatalf("fetch after retries: %v", err)
	}
	if string(res.Body) != "third time lucky" {
		t.Errorf("body = %q", res.Body)
	}
	if n := up.hits.Load(); n != 3 {
		t.Errorf("upstream hits = %d, want 3 (1 + 2 retries)", n)
	}
}

// TestClientNoRetryAfterExhaustion: when every attempt breaks, the error
// surfaces after exactly 1 + Retries attempts.
func TestClientNoRetryAfterExhaustion(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("server does not support hijacking")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	})
	c := newClient(t, up.URL, func(o *Options) {
		o.Retries = 1
		o.RetryBackoff = time.Millisecond
	})
	if _, err := c.Fetch(context.Background(), Request{Path: "x"}); err == nil {
		t.Fatal("fetch succeeded against a breaking upstream")
	}
	if n := up.hits.Load(); n != 2 {
		t.Errorf("upstream hits = %d, want 2 (1 + 1 retry)", n)
	}
}

// TestClientTimeoutNotRetried: a socket timeout surfaces promptly (one
// attempt) instead of tripling client latency — timeouts feed the fetcher's
// assumed-offline path (T-66), and the spec's "超时" bullet is the
// enforcement, not a retry trigger.
func TestClientTimeoutNotRetried(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	})
	c := newClient(t, up.URL, func(o *Options) {
		o.SocketTimeout = 80 * time.Millisecond
		o.Retries = 2
		o.RetryBackoff = time.Millisecond
	})
	start := time.Now()
	_, err := c.Fetch(context.Background(), Request{Path: "x"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("fetch succeeded despite upstream stall")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("err = %v, want a net.Error timeout", err)
	}
	if elapsed > 350*time.Millisecond {
		t.Errorf("elapsed = %v, timeout should surface on the first attempt", elapsed)
	}
	if n := up.hits.Load(); n != 1 {
		t.Errorf("upstream hits = %d, want 1 (no timeout retry)", n)
	}
}

// TestClientStreamIdleTimeout: a mid-body stall aborts after the socket
// timeout (per-read idle deadline on the raw connection), well before any
// whole-request cap — the artifact streaming timeout semantics.
func TestClientStreamIdleTimeout(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("first-chunk"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(400 * time.Millisecond)
		_, _ = w.Write([]byte("stalled"))
	})
	c := newClient(t, up.URL, func(o *Options) { o.SocketTimeout = 80 * time.Millisecond })
	stream, err := c.Stream(context.Background(), Request{Path: "slow.bin"})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer func() { _ = stream.Body.Close() }()
	buf := make([]byte, 32)
	if _, err := stream.Body.Read(buf); err != nil {
		t.Fatalf("first read: %v", err)
	}
	start := time.Now()
	if _, err := stream.Body.Read(buf); err == nil {
		t.Fatal("second read succeeded despite upstream stall")
	}
	if elapsed := time.Since(start); elapsed > 350*time.Millisecond {
		t.Errorf("idle stall aborted after %v, want ~socket timeout", elapsed)
	}
}

// TestClientMethodRestriction: the outbound surface is read-only — no
// request is built for anything but GET/HEAD (FR-20-AC9's upstream side).
func TestClientMethodRestriction(t *testing.T) {
	up := newHitServer(t, func(http.ResponseWriter, *http.Request) {})
	c := newClient(t, up.URL, nil)
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		_, err := c.Fetch(context.Background(), Request{Method: method, Path: "x"})
		if !errors.Is(err, ErrMethodNotSupported) {
			t.Errorf("method %s: err = %v, want ErrMethodNotSupported", method, err)
		}
	}
	if n := up.hits.Load(); n != 0 {
		t.Errorf("upstream saw %d requests, want 0", n)
	}
}

// TestClientUserAgent: a default UA is set when the caller provides none
// and preserved when it does.
func TestClientUserAgent(t *testing.T) {
	var seen atomic.Value
	up := newHitServer(t, func(_ http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("User-Agent"))
	})
	c := newClient(t, up.URL, nil)
	if _, err := c.Fetch(context.Background(), Request{Path: "x"}); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got, _ := seen.Load().(string); got != userAgent {
		t.Errorf("default UA = %q, want %q", got, userAgent)
	}
	req := Request{Path: "x", Header: http.Header{"User-Agent": []string{"maven/3.9 (internal CI)"}}}
	if _, err := c.Fetch(context.Background(), req); err != nil {
		t.Fatalf("fetch with caller UA: %v", err)
	}
	if got, _ := seen.Load().(string); got != "maven/3.9 (internal CI)" {
		t.Errorf("caller UA = %q, want preserved", got)
	}
}

// TestClientHeadRequest: HEAD is a legal outbound verb (the fetcher's
// revalidation path, P1) and yields an empty body with intact headers.
func TestClientHeadRequest(t *testing.T) {
	up := newHitServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		w.WriteHeader(http.StatusOK)
	})
	c := newClient(t, up.URL, nil)
	res, err := c.Fetch(context.Background(), Request{Method: "HEAD", Path: "maven-metadata.xml"})
	if err != nil {
		t.Fatalf("head fetch: %v", err)
	}
	if len(res.Body) != 0 {
		t.Errorf("HEAD body = %q, want empty", res.Body)
	}
	if res.Header.Get("ETag") != `"abc"` {
		t.Errorf("ETag = %q", res.Header.Get("ETag"))
	}
}

// TestJoinURL covers the base/path seam the fetcher uses to build upstream
// URLs.
func TestJoinURL(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"https://up.example.com/maven2", "junit/junit/4.13.2/junit-4.13.2.jar", "https://up.example.com/maven2/junit/junit/4.13.2/junit-4.13.2.jar"},
		{"https://up.example.com/maven2/", "/leading/slash", "https://up.example.com/maven2/leading/slash"},
		{"http://up.example.com", "", "http://up.example.com"},
		{"http://up.example.com//", "x", "http://up.example.com/x"},
	}
	for _, tc := range cases {
		if got := JoinURL(tc.base, tc.path); got != tc.want {
			t.Errorf("JoinURL(%q, %q) = %q, want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

// TestClientRebindingThroughTransport: the full http.Client stack surfaces
// a connect-time rebinding flip as a *RejectionError — chain point 3 holds
// even when the pre-flight check passed on the previous answer.
func TestClientRebindingThroughTransport(t *testing.T) {
	var calls atomic.Int32
	logger, buf := testLogger()
	c := newClient(t, "", func(o *Options) {
		o.BaseURL = "http://rebind.test"
		o.AllowPrivateUpstream = false
		o.Logger = logger
		o.Resolve = func(context.Context, string) ([]netip.Addr, error) {
			if calls.Add(1) == 1 {
				return publicAddrs(), nil // CheckURL passes
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil // dial flips
		}
	})
	_, err := c.Fetch(context.Background(), Request{Path: "x"})
	if err == nil {
		t.Fatal("rebound fetch succeeded")
	}
	var rej *RejectionError
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *RejectionError through the transport stack", err)
	}
	if rej.Category != CategoryLoopback || rej.Phase != "dial" {
		t.Errorf("category/phase = %q/%q, want loopback/dial", rej.Category, rej.Phase)
	}
	if !strings.Contains(buf.String(), "phase=dial") {
		t.Errorf("dial rejection missing from WARN log: %q", buf.String())
	}
}
