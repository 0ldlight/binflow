package remote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Rejection categories (NFR-S13 points 1 and 2): stable strings consumed by
// the WARN audit log, the 400 message composition in the fetcher (T-66) and
// tests.
const (
	// CategoryScheme: chain point 1 — the target (or a redirect hop) is not
	// http/https. Repository creation already rejects file://, ftp:// and
	// friends (FR-15-AC3); this is the request-time assertion.
	CategoryScheme = "scheme_not_http"
	// CategoryNoHost: the URL parses but carries no host (e.g. "http:///x").
	CategoryNoHost = "no_host"
	// CategoryLoopback: 127.0.0.0/8 or ::1.
	CategoryLoopback = "loopback"
	// CategoryPrivateV4: RFC1918 10/8, 172.16/12, 192.168/16.
	CategoryPrivateV4 = "private_rfc1918"
	// CategoryPrivateV6: IPv6 unique local area fc00::/7 (RFC4193).
	CategoryPrivateV6 = "private_ula"
	// CategoryLinkLocal: 169.254/16 (including the cloud metadata service
	// 169.254.169.254) and fe80::/10.
	CategoryLinkLocal = "link_local"
	// CategoryUnspecified: 0.0.0.0/8 and ::.
	CategoryUnspecified = "unspecified"
	// CategoryMulticast: 224.0.0.0/4 and ff00::/8.
	CategoryMulticast = "multicast"
	// CategoryBroadcast: 255.255.255.255.
	CategoryBroadcast = "broadcast"
	// CategoryReserved: the IPv4 reserved band 240.0.0.0/4.
	CategoryReserved = "reserved"
	// CategoryInvalidAddress: an address that could not be parsed at all —
	// defensive only, inputs are pre-parsed on every other path.
	CategoryInvalidAddress = "invalid_address"
)

// blockedRanges is the NFR-S13 point 2 screening list. Where prefixes
// overlap, the more specific entry must come first: the broadcast /32
// precedes the reserved 240/4 that contains it.
var blockedRanges = []struct {
	prefix   netip.Prefix
	category string
}{
	{netip.MustParsePrefix("0.0.0.0/8"), CategoryUnspecified},
	{netip.MustParsePrefix("::/128"), CategoryUnspecified},
	{netip.MustParsePrefix("10.0.0.0/8"), CategoryPrivateV4},
	{netip.MustParsePrefix("172.16.0.0/12"), CategoryPrivateV4},
	{netip.MustParsePrefix("192.168.0.0/16"), CategoryPrivateV4},
	{netip.MustParsePrefix("fc00::/7"), CategoryPrivateV6},
	{netip.MustParsePrefix("127.0.0.0/8"), CategoryLoopback},
	{netip.MustParsePrefix("::1/128"), CategoryLoopback},
	{netip.MustParsePrefix("169.254.0.0/16"), CategoryLinkLocal},
	{netip.MustParsePrefix("fe80::/10"), CategoryLinkLocal},
	{netip.MustParsePrefix("224.0.0.0/4"), CategoryMulticast},
	{netip.MustParsePrefix("ff00::/8"), CategoryMulticast},
	{netip.MustParsePrefix("255.255.255.255/32"), CategoryBroadcast},
	{netip.MustParsePrefix("240.0.0.0/4"), CategoryReserved},
}

// classifyIP maps one address to its NFR-S13 rejection category, or "" when
// the address is allowed. IPv4-mapped IPv6 addresses are collapsed first so
// ::ffff:10.0.0.1 cannot launder a private v4 address.
func classifyIP(ip netip.Addr) string {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return CategoryInvalidAddress
	}
	for _, r := range blockedRanges {
		if r.prefix.Contains(ip) {
			return r.category
		}
	}
	return ""
}

// RejectionError reports an NFR-S13 chain denial. The fetcher (T-66) maps it
// to a 400 E-01 response (PRD FR-20-AC3); the guard also emits one
// structured WARN line per rejection (repo key, refused target, category,
// phase) with no stack trace, per NFR-S13 point 7.
type RejectionError struct {
	RepoKey  string
	Target   string // host[:port] as requested
	IP       string // offending address; empty for scheme/host denials
	Category string
	Phase    string // "check" (pre-connect) or "dial"/"control" (connect-time)
}

func (e *RejectionError) Error() string {
	if e.IP != "" {
		return fmt.Sprintf("upstream target %s rejected: %s address %s", e.Target, e.Category, e.IP)
	}
	return fmt.Sprintf("upstream target %s rejected: %s", e.Target, e.Category)
}

// IsRejection reports whether err, or anything it wraps, is an NFR-S13 chain
// denial. Transport-layer wrapping (url.Error, net.OpError) preserves the
// chain via Unwrap.
func IsRejection(err error) bool {
	var r *RejectionError
	return errors.As(err, &r)
}

// Guard enforces the NFR-S13 outbound validation chain for one remote
// repository policy. A Guard is immutable and safe for concurrent use; the
// same Guard may back many concurrent requests.
type Guard struct {
	repoKey      string
	allowPrivate bool
	log          *slog.Logger
	resolve      func(ctx context.Context, host string) ([]netip.Addr, error)
}

// GuardOptions configures a Guard. The zero value is a guard that screens
// everything and logs to slog.Default().
type GuardOptions struct {
	// RepoKey names the repository in WARN audit lines.
	RepoKey string
	// AllowPrivateUpstream exempts the repository from the IP screening
	// list (NFR-S13 point 2; only admins may set it on the repository,
	// which is audited at the repo layer — T-64). Scheme assertion still
	// applies.
	AllowPrivateUpstream bool
	// Logger receives the WARN audit lines; nil means slog.Default().
	Logger *slog.Logger
	// Resolve overrides host resolution. nil means the system resolver.
	// It is a test seam: tests inject deterministic answers (including
	// DNS-rebinding flips) so the suite never touches the network.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
}

// NewGuard returns a Guard for one repository policy.
func NewGuard(opts GuardOptions) *Guard {
	g := &Guard{
		repoKey:      opts.RepoKey,
		allowPrivate: opts.AllowPrivateUpstream,
		log:          opts.Logger,
		resolve:      opts.Resolve,
	}
	if g.log == nil {
		g.log = slog.Default()
	}
	if g.resolve == nil {
		g.resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	return g
}

// reject emits the single WARN audit line for a denial and returns the
// corresponding error. Structured attrs only — no stack trace (NFR-S13
// point 7, the M42 forensic surface).
func (g *Guard) reject(ctx context.Context, target, ip, category, phase string) error {
	attrs := []any{
		slog.String("repo", g.repoKey),
		slog.String("target", target),
		slog.String("category", category),
		slog.String("phase", phase),
	}
	if ip != "" {
		attrs = append(attrs, slog.String("ip", ip))
	}
	g.log.WarnContext(ctx, "remote: outbound target rejected (ssrf-guard)", attrs...)
	return &RejectionError{
		RepoKey:  g.repoKey,
		Target:   target,
		IP:       ip,
		Category: category,
		Phase:    phase,
	}
}

// CheckURL runs chain points 1 and 2 (NFR-S13) for one hop: the scheme
// assertion and the all-IP screening of the host. The client calls it for
// the initial URL and for every redirect target before any connection is
// attempted, so a forbidden hop is denied without a single outbound packet.
//
// Hostname resolution failure is NOT a rejection: it is an upstream
// reachability problem and surfaces as a plain wrapped error for the
// fetcher's assumed-offline path (T-66).
func (g *Guard) CheckURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("ssrf-guard: parse url %q: %w", rawURL, err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return g.reject(ctx, u.String(), "", CategoryScheme, "check")
	}
	host := u.Hostname()
	if host == "" {
		return g.reject(ctx, u.String(), "", CategoryNoHost, "check")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if category := classifyIP(ip); category != "" && !g.allowPrivate {
			return g.reject(ctx, u.Host, ip.Unmap().String(), category, "check")
		}
		return nil
	}
	ips, err := g.resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("ssrf-guard: resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("ssrf-guard: resolve host %q: no addresses", host)
	}
	if g.allowPrivate {
		return nil
	}
	// Point 2: every resolved address must pass — a single private answer
	// rejects the whole target (a round-robin public/private DNS answer
	// must not smuggle the connection onto the private leg).
	for _, ip := range ips {
		if category := classifyIP(ip); category != "" {
			return g.reject(ctx, u.Host, ip.Unmap().String(), category, "check")
		}
	}
	return nil
}

// Dialer returns the transport dial function for this guard: chain point 3
// (NFR-S13, DNS-rebinding defense). The host is resolved here and every
// address is screened; the dial then pins one validated IP, so the system
// resolver is never consulted again for that connection ("dial the
// validated IP"). The inner dialer's Control callback re-screens the
// address actually being connected as the mandated second check.
//
// The returned connection carries a per-read/write idle deadline of
// timeout: a stalled upstream aborts mid-body (a whole-request
// http.Client.Timeout would instead cap artifact streaming, which it must
// not — FR-20-AC11).
func (g *Guard) Dialer(timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := g.dial(ctx, network, addr, timeout)
		if err != nil {
			return nil, err
		}
		return &idleTimeoutConn{Conn: conn, timeout: timeout}, nil
	}
}

func (g *Guard) dial(ctx context.Context, network, addr string, timeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("ssrf-guard: split address %q: %w", addr, err)
	}
	var pinned netip.Addr
	if ip, perr := netip.ParseAddr(host); perr == nil {
		if category := classifyIP(ip); category != "" && !g.allowPrivate {
			return nil, g.reject(ctx, addr, ip.Unmap().String(), category, "dial")
		}
		pinned = ip.Unmap()
	} else {
		ips, rerr := g.resolve(ctx, host)
		if rerr != nil {
			return nil, fmt.Errorf("ssrf-guard: resolve host %q: %w", host, rerr)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("ssrf-guard: resolve host %q: no addresses", host)
		}
		if !g.allowPrivate {
			for _, ip := range ips {
				if category := classifyIP(ip); category != "" {
					return nil, g.reject(ctx, addr, ip.Unmap().String(), category, "dial")
				}
			}
		}
		pinned = ips[0].Unmap()
	}
	d := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control: func(_, controlAddr string, _ syscall.RawConn) error {
			// Second screening of the address actually being connected.
			// The dial below pins an already-validated IP, so this is the
			// backstop required by NFR-S13 point 3 rather than the primary
			// defense; it must stay in place for any future refactor that
			// bypasses the pinning above.
			controlHost, _, cerr := net.SplitHostPort(controlAddr)
			if cerr != nil {
				return fmt.Errorf("ssrf-guard: control address %q: %w", controlAddr, cerr)
			}
			ip, cerr := netip.ParseAddr(controlHost)
			if cerr != nil {
				return fmt.Errorf("ssrf-guard: control address %q: %w", controlAddr, cerr)
			}
			if category := classifyIP(ip); category != "" && !g.allowPrivate {
				return g.reject(ctx, controlAddr, ip.Unmap().String(), category, "control")
			}
			return nil
		},
	}
	return d.DialContext(ctx, network, net.JoinHostPort(pinned.String(), port))
}

// idleTimeoutConn enforces the per-read/write socket timeout on the raw
// connection: every Read and Write first pushes the deadline one full
// timeout into the future, so the clock measures inactivity between
// packets, not total request duration. net/http has no per-request
// idle-read deadline of its own, and http.Client.Timeout would cap
// whole-body streaming (wrong for artifact downloads).
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) Read(b []byte) (int, error) {
	if err := c.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, fmt.Errorf("ssrf-guard: set read deadline: %w", err)
	}
	return c.Conn.Read(b)
}

func (c *idleTimeoutConn) Write(b []byte) (int, error) {
	if err := c.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return 0, fmt.Errorf("ssrf-guard: set write deadline: %w", err)
	}
	return c.Conn.Write(b)
}
