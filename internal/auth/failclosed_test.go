package auth_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lzwzzy/binflow/internal/auth"
	"github.com/lzwzzy/binflow/internal/metadata"
)

// B-3: "store error -> Can = false" (fail closed) is a headline AC of this
// ticket. These tests swap the permission source through the exported seam
// (auth.PermissionSource + Service.WithPermissions) and prove both
// fail-closed branches deny — with a real grant in place, so the denial is
// demonstrably caused by the error and not by missing rows.

// failingPerms fails PrincipalsFor, ListTargets, or returns a healthy row
// set (control case).
type failingPerms struct {
	failPrincipals bool
	failTargets    bool
	// goodRows is returned when failPrincipals is false, letting the
	// ListTargets branch run with a live grant in hand.
	goodRows []auth.PermissionRow
}

func (f *failingPerms) ListTargets(context.Context) ([]auth.Target, error) {
	if f.failTargets {
		return nil, errors.New("boom: targets table unavailable")
	}
	return nil, nil
}

func (f *failingPerms) PrincipalsFor(_ context.Context, _ string) ([]auth.PermissionRow, error) {
	if f.failPrincipals {
		return nil, errors.New("boom: principals query failed")
	}
	return f.goodRows, nil
}

// newFailClosedService builds a real-store service with a broken (or
// controlled) permission plane.
func newFailClosedService(t *testing.T, perms *failingPerms) *auth.Service {
	t.Helper()
	f := newFixture(t, true)
	return f.svc.WithPermissions(perms)
}

func TestCanFailsClosedOnPrincipalsError(t *testing.T) {
	ci := &auth.Principal{Name: "ci-bot"}
	svc := newFailClosedService(t, &failingPerms{failPrincipals: true})
	for _, action := range []string{auth.ActionRead, auth.ActionWrite, auth.ActionDelete} {
		if svc.Can(context.Background(), ci, "generic-local", "a.bin", action) {
			t.Fatalf("Can(%s) granted despite PrincipalsFor failing; must deny (fail closed)", action)
		}
	}
}

func TestCanFailsClosedOnTargetsError(t *testing.T) {
	// A live grant row is returned; only ListTargets fails.
	svc := newFailClosedService(t, &failingPerms{
		failTargets: true,
		goodRows: []auth.PermissionRow{{
			TargetName: "t", Principal: "ci-bot", PrincipalType: "user", CanRead: true,
		}},
	})
	ci := &auth.Principal{Name: "ci-bot"}
	if svc.Can(context.Background(), ci, "generic-local", "a.bin", auth.ActionRead) {
		t.Fatal("Can granted despite ListTargets failing; must deny (fail closed)")
	}
}

// Control: the same construction with a healthy permission source that
// carries a grant (target exists, repo listed, everything granted) does
// authorize — proves the denials above come from the error branches.
func TestCanHealthyControl(t *testing.T) {
	f := newFixture(t, true)
	svc := f.svc.WithPermissions(healthyPerms{})
	ci := &auth.Principal{Name: "ci-bot"}
	if !svc.Can(f.ctx, ci, "generic-local", "a.bin", auth.ActionRead) {
		t.Fatal("healthy permission source with a full grant must authorize")
	}
}

type healthyPerms struct{}

func (healthyPerms) ListTargets(context.Context) ([]auth.Target, error) {
	return []auth.Target{{
		Name:     "t",
		Repos:    `["generic-local"]`,
		Includes: `["**"]`,
		Excludes: `[]`,
	}}, nil
}

func (healthyPerms) PrincipalsFor(_ context.Context, _ string) ([]auth.PermissionRow, error) {
	return []auth.PermissionRow{{
		TargetName: "t", Principal: "ci-bot", PrincipalType: "user", CanRead: true,
	}}, nil
}

// NFR-S3 on the fail-closed log path (previously never triggered): capture
// the slog output of a real store failure and assert it carries only the
// four non-sensitive keys — no credentials, no header names, no payloads.
func TestFailClosedLogHygiene(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	svc := newFailClosedService(t, &failingPerms{failPrincipals: true})
	if svc.Can(context.Background(), &auth.Principal{Name: "ci-bot"}, "generic-local", "a.bin", auth.ActionRead) {
		t.Fatal("must deny")
	}

	logged := buf.String()
	if !strings.Contains(logged, "permission lookup failed") {
		t.Fatalf("fail-closed log line missing; got:\n%s", logged)
	}
	for _, key := range []string{"repo=", "user=", "action=", "error="} {
		if !strings.Contains(logged, key) {
			t.Fatalf("log line lacks key %q:\n%s", key, logged)
		}
	}
	for _, banned := range []string{"Authorization", "password", "X-JFrog", "Basic "} {
		if strings.Contains(logged, banned) {
			t.Fatalf("fail-closed log leaks %q:\n%s", banned, logged)
		}
	}
}

// B-1: the exported sentinels alias the metadata sentinels (same error
// value), so errors.Is matches either spelling.
func TestSentinelAliases(t *testing.T) {
	if !errors.Is(auth.ErrTokenNotFound, metadata.ErrTokenNotFound) {
		t.Fatal("auth.ErrTokenNotFound must be the same sentinel as metadata.ErrTokenNotFound")
	}
	if !errors.Is(auth.ErrUserNotFound, metadata.ErrUserNotFound) {
		t.Fatal("auth.ErrUserNotFound must be the same sentinel as metadata.ErrUserNotFound")
	}
}
