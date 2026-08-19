package remote

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() (*slog.Logger, *bytes.Buffer) {
	buf := new(bytes.Buffer)
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func publicAddrs() []netip.Addr {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}
}

// TestCheckURLMatrix is the NFR-S13 point 1/2 screening matrix: every
// blocked class from the PRD list, both protocol families, literal and
// resolved forms, plus the allowPrivateUpstream exemption. Resolution is
// injected so the suite never touches the network.
func TestCheckURLMatrix(t *testing.T) {
	privateHost := func(ips ...netip.Addr) func(context.Context, string) ([]netip.Addr, error) {
		return func(context.Context, string) ([]netip.Addr, error) { return ips, nil }
	}
	cases := []struct {
		name      string
		url       string
		resolve   func(context.Context, string) ([]netip.Addr, error)
		allow     bool
		wantErr   bool
		wantCat   string
		wantPhase string // "check" for pre-connect denials; "" when no denial
	}{
		{name: "public https hostname", url: "https://repo.example.com/maven2/x.jar", resolve: privateHost(publicAddrs()...)},
		{name: "public ipv4 literal", url: "http://93.184.216.34/file.jar"},
		{name: "public ipv6 literal", url: "http://[2606:2800:220:1:248:1893:25c8:1946]/file.jar"},

		{name: "loopback 127.0.0.1", url: "http://127.0.0.1:9099/dir/up.bin", wantErr: true, wantCat: CategoryLoopback, wantPhase: "check"},
		{name: "loopback 127.42.0.9 (whole 127/8)", url: "http://127.42.0.9/x", wantErr: true, wantCat: CategoryLoopback, wantPhase: "check"},
		{name: "loopback ::1", url: "http://[::1]:8080/x", wantErr: true, wantCat: CategoryLoopback, wantPhase: "check"},
		{name: "loopback via v4-mapped v6", url: "http://[::ffff:127.0.0.1]/x", wantErr: true, wantCat: CategoryLoopback, wantPhase: "check"},
		{name: "private via v4-mapped v6", url: "http://[::ffff:192.168.1.1]/x", wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check"},

		{name: "rfc1918 10/8", url: "http://10.1.2.3/x", wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check"},
		{name: "rfc1918 172.16/12 in range", url: "http://172.31.255.1/x", wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check"},
		{name: "rfc1918 172.16/12 lower boundary out", url: "http://172.15.255.1/x"},
		{name: "rfc1918 172.16/12 upper boundary out", url: "http://172.32.0.1/x"},
		{name: "rfc1918 192.168/16", url: "http://192.168.1.1/x", wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check"},
		{name: "192.169/16 is public", url: "http://192.169.0.1/x"},

		{name: "cloud metadata 169.254.169.254", url: "http://169.254.169.254/latest/meta-data/", wantErr: true, wantCat: CategoryLinkLocal, wantPhase: "check"},
		{name: "link local 169.254.0.9", url: "http://169.254.0.9/x", wantErr: true, wantCat: CategoryLinkLocal, wantPhase: "check"},
		{name: "link local fe80::/10", url: "http://[fe80::1]/x", wantErr: true, wantCat: CategoryLinkLocal, wantPhase: "check"},
		{name: "fec0:: site-local is screened as reserved-era ula", url: "http://[fc00::1]/x", wantErr: true, wantCat: CategoryPrivateV6, wantPhase: "check"},
		{name: "ula fd00::/8 (fc00::/7 upper half)", url: "http://[fd12:3456::1]/x", wantErr: true, wantCat: CategoryPrivateV6, wantPhase: "check"},

		{name: "unspecified 0.0.0.0", url: "http://0.0.0.0/x", wantErr: true, wantCat: CategoryUnspecified, wantPhase: "check"},
		{name: "unspecified ::", url: "http://[::]/x", wantErr: true, wantCat: CategoryUnspecified, wantPhase: "check"},
		{name: "multicast v4", url: "http://224.0.0.1/x", wantErr: true, wantCat: CategoryMulticast, wantPhase: "check"},
		{name: "multicast v6", url: "http://[ff02::1]/x", wantErr: true, wantCat: CategoryMulticast, wantPhase: "check"},
		{name: "broadcast 255.255.255.255", url: "http://255.255.255.255/x", wantErr: true, wantCat: CategoryBroadcast, wantPhase: "check"},
		{name: "reserved 240.0.0.0/4", url: "http://240.0.0.1/x", wantErr: true, wantCat: CategoryReserved, wantPhase: "check"},

		{name: "scheme file", url: "file:///etc/passwd", wantErr: true, wantCat: CategoryScheme, wantPhase: "check"},
		{name: "scheme gopher", url: "gopher://127.0.0.1:70/x", wantErr: true, wantCat: CategoryScheme, wantPhase: "check"},
		{name: "scheme ftp", url: "ftp://example.com/x", wantErr: true, wantCat: CategoryScheme, wantPhase: "check"},
		{name: "scheme case-insensitive https ok", url: "HTTPS://repo.example.com/x", resolve: privateHost(publicAddrs()...)},
		{name: "empty host", url: "http:///x", wantErr: true, wantCat: CategoryNoHost, wantPhase: "check"},
		{name: "userinfo does not hide the host", url: "http://user:pass@10.0.0.9/x", wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check"},

		// point 2: ALL resolved IPs must pass — a mixed public/private
		// answer rejects the whole target.
		{
			name: "hostname resolving public+private rejected",
			url:  "http://upstream.test/x",
			resolve: privateHost(
				netip.MustParseAddr("93.184.216.34"),
				netip.MustParseAddr("10.0.0.5"),
			),
			wantErr: true, wantCat: CategoryPrivateV4, wantPhase: "check",
		},
		{
			name:      "hostname resolving only public allowed",
			url:       "http://upstream.test/x",
			resolve:   privateHost(publicAddrs()...),
			wantErr:   false,
			wantPhase: "",
		},

		// allowPrivateUpstream exempts the IP list but never the scheme.
		{name: "exempt: private literal allowed", url: "http://10.0.0.5/x", allow: true},
		{name: "exempt: loopback allowed", url: "http://127.0.0.1:9099/x", allow: true},
		{name: "exempt: metadata allowed", url: "http://169.254.169.254/x", allow: true},
		{name: "exempt: scheme still enforced", url: "file:///etc/passwd", allow: true, wantErr: true, wantCat: CategoryScheme, wantPhase: "check"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, buf := testLogger()
			g := NewGuard(GuardOptions{
				RepoKey:              "ssrf-remote",
				AllowPrivateUpstream: tc.allow,
				Logger:               logger,
				Resolve:              tc.resolve,
			})
			err := g.CheckURL(context.Background(), tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("CheckURL(%q) = nil, want rejection %q", tc.url, tc.wantCat)
				}
				var rej *RejectionError
				if !errors.As(err, &rej) {
					t.Fatalf("CheckURL(%q) error %v is not a *RejectionError", tc.url, err)
				}
				if rej.Category != tc.wantCat {
					t.Errorf("category = %q, want %q", rej.Category, tc.wantCat)
				}
				if rej.RepoKey != "ssrf-remote" {
					t.Errorf("repo key = %q, want %q", rej.RepoKey, "ssrf-remote")
				}
				if rej.Target == "" {
					t.Error("rejection carries no target")
				}
				if !IsRejection(err) {
					t.Error("IsRejection(err) = false for a rejection error")
				}
				// NFR-S13 point 7: one WARN line, structured, no stack.
				out := buf.String()
				if !strings.Contains(out, "level=WARN") {
					t.Errorf("no WARN log line for rejection; got %q", out)
				}
				for _, field := range []string{"repo=ssrf-remote", "category=" + tc.wantCat, "target=", "phase=" + tc.wantPhase} {
					if !strings.Contains(out, field) {
						t.Errorf("WARN line missing %q; got %q", field, out)
					}
				}
				if strings.Contains(out, "goroutine") || strings.Contains(out, ".go:") {
					t.Errorf("WARN line carries a stack trace; got %q", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("CheckURL(%q) = %v, want nil", tc.url, err)
			}
			if out := buf.String(); strings.Contains(out, "WARN") {
				t.Errorf("unexpected WARN on an allowed target: %q", out)
			}
		})
	}
}

// TestCheckURLResolveFailureIsNotRejection: DNS failure is an upstream
// reachability problem (the fetcher's assumed-offline path), not an SSRF
// denial — no WARN line is emitted for it.
func TestCheckURLResolveFailureIsNotRejection(t *testing.T) {
	logger, buf := testLogger()
	g := NewGuard(GuardOptions{
		RepoKey: "r",
		Logger:  logger,
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			return nil, &net.DNSError{Err: "no such host", Name: "gone.test", IsNotFound: true}
		},
	})
	err := g.CheckURL(context.Background(), "http://gone.test/x")
	if err == nil {
		t.Fatal("expected a resolution error")
	}
	if IsRejection(err) {
		t.Errorf("resolution failure classified as rejection: %v", err)
	}
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("resolution failure logged a WARN: %q", buf.String())
	}
}

// TestGuardDialRebinding is chain point 3 (DNS-rebinding defense): a target
// whose pre-flight resolution looked public must still be refused when the
// connect-time resolution flips to a private address — the dial never goes
// back to the system resolver after validation, and the rejection surfaces
// as a *RejectionError with phase=dial.
func TestGuardDialRebinding(t *testing.T) {
	var calls atomic.Int32
	logger, buf := testLogger()
	g := NewGuard(GuardOptions{
		RepoKey: "rebind-remote",
		Logger:  logger,
		Resolve: func(context.Context, string) ([]netip.Addr, error) {
			switch calls.Add(1) {
			case 1:
				return publicAddrs(), nil // pre-flight CheckURL
			default:
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil // rebinding
			}
		},
	})
	ctx := context.Background()
	if err := g.CheckURL(ctx, "http://upstream.test/x"); err != nil {
		t.Fatalf("pre-flight check failed: %v", err)
	}
	conn, err := g.Dialer(time.Second)(ctx, "tcp", "upstream.test:80")
	if err == nil {
		_ = conn.Close()
		t.Fatal("dial to a rebound loopback address succeeded")
	}
	var rej *RejectionError
	if !errors.As(err, &rej) {
		t.Fatalf("dial error %v is not a *RejectionError", err)
	}
	if rej.Category != CategoryLoopback || rej.Phase != "dial" {
		t.Errorf("category/phase = %q/%q, want %q/%q", rej.Category, rej.Phase, CategoryLoopback, "dial")
	}
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "phase=dial") {
		t.Errorf("dial rejection not WARN-logged with phase=dial: %q", out)
	}
}

// TestGuardDialPrivateLiteralRejected: the dial path screens literal IPs
// too, so even a client that bypassed CheckURL cannot connect inward.
func TestGuardDialPrivateLiteralRejected(t *testing.T) {
	logger, _ := testLogger()
	g := NewGuard(GuardOptions{RepoKey: "r", Logger: logger})
	for _, addr := range []string{"10.0.0.1:80", "169.254.169.254:80", "192.168.0.1:8080"} {
		conn, err := g.Dialer(time.Second)(context.Background(), "tcp", addr)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("dial to %s succeeded, want rejection", addr)
		}
		if !IsRejection(err) {
			t.Errorf("dial to %s: error %v is not a rejection", addr, err)
		}
	}
}

// TestGuardDialExemptedRoundTrip exercises the happy path of the guarded
// dialer end to end on a loopback listener under the exemption: resolution
// screening, IP pinning, the Control second check and the idle-deadline
// connection wrapper all stay transparent to a ping/pong exchange.
func TestGuardDialExemptedRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = io.Copy(conn, conn)
			_ = conn.Close()
		}
	}()
	logger, buf := testLogger()
	g := NewGuard(GuardOptions{RepoKey: "loop-ok", AllowPrivateUpstream: true, Logger: logger})
	conn, err := g.Dialer(2*time.Second)(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("guarded dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write through idle-deadline wrapper: %v", err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read through idle-deadline wrapper: %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("echo = %q, want %q", got, "ping")
	}
	if strings.Contains(buf.String(), "WARN") {
		t.Errorf("exempted dial produced a WARN: %q", buf.String())
	}
}
