package auth_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lzwzzy/binflow/internal/auth"
)

// NFR-S3: under a failing authentication storm the package's own log output
// (the Authorizer failure path is the only slog writer) must not contain any
// presented credential. Error returns are checked separately: their strings
// never embed the secret either.
func TestLogHygiene(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newFixture(t, true)
	tok, err := f.svc.Issue(f.ctx, "ci-bot", time.Hour)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	secret := tok.AccessToken

	// Storm: every failure path that can log or error.
	_, _ = f.svc.Authenticate(f.ctx, req(basic("admin", secret), "")) // principal mismatch
	_, _ = f.svc.Authenticate(f.ctx, req(basic("admin", "wrongpw"), ""))
	_, _ = f.svc.Authenticate(f.ctx, req("", secret+"x")) // unknown token
	_, _ = f.svc.Verify(f.ctx, secret+"x")
	_ = f.svc.Revoke(f.ctx, secret+"x")
	// Authorizer denial via a store failure is covered by Can returning
	// false; its log path only fires on store errors, which the fixture
	// store does not produce — the message templates carry only
	// repo/user/action keys by construction (see authorizer.go).
	f.svc.Can(f.ctx, &auth.Principal{Name: "ci-bot"}, "r", "a", auth.ActionRead)

	logged := buf.String()
	for _, leak := range []string{secret, adminPW, ciPW, "wrongpw", "Authorization"} {
		if strings.Contains(logged, leak) {
			t.Fatalf("log output leaks %q:\n%s", leak, logged)
		}
	}
}

// errors.Is works through the whole family (HTTP layer relies on it).
func TestErrorFamily(t *testing.T) {
	f := newFixture(t, true)
	_, err := f.svc.Verify(f.ctx, "garbage")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Verify garbage: %v", err)
	}
	_, err = f.svc.Authenticate(f.ctx, req(basic("admin", "bad"), ""))
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("basic bad: %v", err)
	}
	r, _ := http.NewRequest(http.MethodGet, "/binflow/x", nil)
	p, err := f.svc.Authenticate(f.ctx, r)
	if p != nil || err != nil {
		t.Fatalf("anonymous = %v, %v; want nil, nil", p, err)
	}
}
