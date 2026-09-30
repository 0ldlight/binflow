// T-623 / BIN-107: LDAP cfg.URL userinfo must never reach a rendered face
// (WARN log lines or error text). The URL's userinfo is dead weight on the
// wire — go-ldap v3.4.14's DialURL consumes only scheme and host
// (conn.go DialContext.dial; u.User is never read), and every bind is an
// explicit DN+password — so the render-side scrub is a zero-behavior change:
// the dial target keeps receiving the raw URL (asserted below). Placeholder
// credentials only.
package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	ldap "github.com/go-ldap/ldap/v3"

	"github.com/lzwzzy/binflow/internal/auth"
)

// t623 placeholder credential fragments (never a real secret).
const (
	t623User = "ulogin-t623"
	t623Pass = "FAKECRED-T623"
)

// assertNoT623Creds fails when rendered text carries the placeholder
// userinfo credential (password anywhere, or the user:pass separator form).
func assertNoT623Creds(t *testing.T, rendered string) {
	t.Helper()
	if strings.Contains(rendered, t623Pass) || strings.Contains(rendered, t623User+":") {
		t.Errorf("rendered text leaks the placeholder userinfo credential: %q", rendered)
	}
}

// TestLDAPURLUserinfoRedactedInLogs drives the two construction-time WARN
// faces (skip_tls_verify, start_tls-on-ldaps) with a userinfo-bearing URL and
// asserts the url= attribute is scrubbed; a userinfo-free URL must render
// byte-identical (identity property of redact.Userinfo).
func TestLDAPURLUserinfoRedactedInLogs(t *testing.T) {
	for _, tt := range []struct {
		name        string
		url         string
		wantURLAttr string
	}{
		{
			name:        "skip_tls_verify warn scrubs userinfo",
			url:         "ldaps://" + t623User + ":" + t623Pass + "@dir.example.com:636",
			wantURLAttr: "ldaps://dir.example.com:636",
		},
		{
			name:        "start_tls on ldaps warn scrubs userinfo",
			url:         "ldaps://" + t623User + ":" + t623Pass + "@dir.example.com:636",
			wantURLAttr: "ldaps://dir.example.com:636",
		},
		{
			name:        "userinfo-free url renders byte-identical",
			url:         "ldaps://dir.example.com:636",
			wantURLAttr: "ldaps://dir.example.com:636",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			// Both WARN triggers on: skip_tls_verify fires its WARN, and the
			// ldaps scheme fires the start_tls-ignored WARN.
			cfg := &auth.LDAPConfig{
				Enabled:       true,
				URL:           tt.url,
				BaseDN:        "dc=example,dc=com",
				SkipTLSVerify: true,
				StartTLS:      true,
			}
			prov, err := auth.NewLDAPProvider(cfg, nil, mockDialer(newMockLDAPConn(), mockDialKeepState()))
			if err != nil {
				t.Fatalf("NewLDAPProvider: %v", err)
			}
			t.Cleanup(prov.Close)

			out := buf.String()
			if !strings.Contains(out, "level=WARN") {
				t.Fatalf("log output %q carries no WARN", out)
			}
			if !strings.Contains(out, "url="+tt.wantURLAttr) {
				t.Errorf("log output %q does not carry the scrubbed url=%q", out, tt.wantURLAttr)
			}
			assertNoT623Creds(t, out)
		})
	}
}

// TestLDAPURLUserinfoRedactedInErrors covers the error-text faces: the two
// construction rejections, the default dialer's unreachable arm (a dead
// local port — connection refused is immediate and deterministic), and the
// StartTLS-upgrade failure inside the pool dialer — plus the two
// behavior-preservation properties: the classification sentinels survive,
// and the dial target still receives the raw URL.
func TestLDAPURLUserinfoRedactedInErrors(t *testing.T) {
	t.Run("construction scheme rejection scrubs userinfo", func(t *testing.T) {
		_, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled: true,
			URL:     "http://" + t623User + ":" + t623Pass + "@dir.example.com:80",
			BaseDN:  "dc=x",
		}, nil, nil)
		if err == nil {
			t.Fatal("NewLDAPProvider succeeded, want scheme rejection")
		}
		if !strings.Contains(err.Error(), "scheme must be ldap or ldaps") {
			t.Errorf("error = %v, want the scheme-rejection wording", err)
		}
		if !strings.Contains(err.Error(), "dir.example.com") {
			t.Errorf("error = %v, want the host to stay visible for the operator", err)
		}
		assertNoT623Creds(t, err.Error())
	})

	t.Run("construction empty-host rejection scrubs userinfo", func(t *testing.T) {
		_, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled: true,
			URL:     "ldap://" + t623User + ":" + t623Pass + "@",
			BaseDN:  "dc=x",
		}, nil, nil)
		if err == nil {
			t.Fatal("NewLDAPProvider succeeded, want empty-host rejection")
		}
		if !strings.Contains(err.Error(), "must be an ldap(s) URL") {
			t.Errorf("error = %v, want the malformed-URL wording", err)
		}
		assertNoT623Creds(t, err.Error())
	})

	t.Run("dial failure scrubs userinfo and keeps the sentinel", func(t *testing.T) {
		// nil dialer = the real defaultDialer; port 1 on loopback is dead.
		prov, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled: true,
			URL:     "ldap://" + t623User + ":" + t623Pass + "@127.0.0.1:1",
			BaseDN:  "dc=x",
		}, nil, nil)
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		t.Cleanup(prov.Close)

		_, err = prov.Bind(context.Background(), "alice", "alicepass")
		if err == nil {
			t.Fatal("Bind succeeded against a dead port, want dial failure")
		}
		if !strings.Contains(err.Error(), "ldap://127.0.0.1:1") {
			t.Errorf("error = %v, want the scrubbed dial address to stay visible", err)
		}
		assertNoT623Creds(t, err.Error())
		if !errors.Is(err, auth.ErrProviderUnreachable) {
			t.Errorf("error = %v, want the ErrProviderUnreachable classification preserved", err)
		}
	})

	t.Run("starttls upgrade failure scrubs the rendered url while the dial target stays raw", func(t *testing.T) {
		rawURL := "ldap://" + t623User + ":" + t623Pass + "@dir.example.com:389"
		var dialed string
		mock := newMockLDAPConn()
		mock.startTLSErr = errors.New("upgrade refused")
		dialer := func(_ context.Context, urlStr string, _ ...ldap.DialOpt) (auth.LDAPConn, error) {
			dialed = urlStr
			return mock, nil
		}
		prov, err := auth.NewLDAPProvider(&auth.LDAPConfig{
			Enabled:  true,
			URL:      rawURL,
			BaseDN:   "dc=x",
			StartTLS: true,
		}, nil, dialer)
		if err != nil {
			t.Fatalf("NewLDAPProvider: %v", err)
		}
		t.Cleanup(prov.Close)

		_, err = prov.Bind(context.Background(), "alice", "alicepass")
		if err == nil {
			t.Fatal("Bind succeeded despite the StartTLS refusal, want upgrade failure")
		}
		if !strings.Contains(err.Error(), "ldap://dir.example.com:389") {
			t.Errorf("error = %v, want the scrubbed upgrade address to stay visible", err)
		}
		assertNoT623Creds(t, err.Error())
		if !errors.Is(err, auth.ErrTLSHandshake) {
			t.Errorf("error = %v, want the ErrTLSHandshake classification preserved", err)
		}
		if dialed != rawURL {
			t.Errorf("dial target altered by the scrub: got %q, want the raw %q", dialed, rawURL)
		}
	})
}
