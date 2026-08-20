package config

import (
	"testing"
	"time"
)

// TestConsoleSessionTTLDualKey covers the PRD v1.1 R4 dual-key resolution:
// console.session_ttl_hours is the primary spelling (default 24),
// console.session_ttl_seconds the override for test granularity — seconds
// wins when both are present, in YAML and through env alike.
func TestConsoleSessionTTLDualKey(t *testing.T) {
	t.Run("default when section absent", func(t *testing.T) {
		c := mustLoad(t, "", nil)
		if c.Console.SessionTTL != DefaultConsoleSessionTTL {
			t.Fatalf("SessionTTL = %s, want default %s", c.Console.SessionTTL, DefaultConsoleSessionTTL)
		}
	})
	t.Run("hours primary key", func(t *testing.T) {
		c := mustLoad(t, "console:\n  session_ttl_hours: 12\n", nil)
		if want := 12 * time.Hour; c.Console.SessionTTL != want {
			t.Fatalf("SessionTTL = %s, want %s", c.Console.SessionTTL, want)
		}
	})
	t.Run("seconds override key alone", func(t *testing.T) {
		c := mustLoad(t, "console:\n  session_ttl_seconds: 5\n", nil)
		if want := 5 * time.Second; c.Console.SessionTTL != want {
			t.Fatalf("SessionTTL = %s, want %s", c.Console.SessionTTL, want)
		}
	})
	t.Run("both set seconds wins", func(t *testing.T) {
		c := mustLoad(t, "console:\n  session_ttl_hours: 24\n  session_ttl_seconds: 30\n", nil)
		if want := 30 * time.Second; c.Console.SessionTTL != want {
			t.Fatalf("SessionTTL = %s, want %s (seconds must win)", c.Console.SessionTTL, want)
		}
	})
	t.Run("env hours", func(t *testing.T) {
		c := mustLoad(t, "", map[string]string{"BINFLOW_CONSOLE__SESSION_TTL_HOURS": "8"})
		if want := 8 * time.Hour; c.Console.SessionTTL != want {
			t.Fatalf("SessionTTL = %s, want %s", c.Console.SessionTTL, want)
		}
	})
	t.Run("env seconds wins over yaml hours", func(t *testing.T) {
		c := mustLoad(t, "console:\n  session_ttl_hours: 24\n",
			map[string]string{"BINFLOW_CONSOLE__SESSION_TTL_SECONDS": "5"})
		if want := 5 * time.Second; c.Console.SessionTTL != want {
			t.Fatalf("SessionTTL = %s, want %s (env override must win)", c.Console.SessionTTL, want)
		}
	})
	t.Run("unknown console key rejected", func(t *testing.T) {
		if _, err := loadWithEnv(t, "console:\n  session_ttl_minutes: 30\n", nil); err == nil {
			t.Fatal("unknown console key accepted, want strict rejection")
		}
	})
}

// TestConsoleSessionTTLValidation: the resolved value must stay positive —
// an explicit zero is a misconfiguration, not "expire immediately" (the
// storage TTL zero-value posture, architecture section 11 item 5, applied to
// the console key).
func TestConsoleSessionTTLValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"hours zero", "console:\n  session_ttl_hours: 0\n"},
		{"seconds zero", "console:\n  session_ttl_seconds: 0\n"},
		{"env seconds zero", "console:\n  session_ttl_hours: 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			if tc.name == "env seconds zero" {
				env["BINFLOW_CONSOLE__SESSION_TTL_SECONDS"] = "0"
			}
			if _, err := loadWithEnv(t, tc.body, env); err == nil {
				t.Fatal("zero session ttl accepted, want validation error")
			}
		})
	}
}

// TestDefaultsCarryConsoleTTL pins the default-set contract: hand-constructed
// configs (cmd's no-file boot path, test harnesses) see the 24h default, so
// consumers never need a zero-value fallback of their own.
func TestDefaultsCarryConsoleTTL(t *testing.T) {
	if got := Defaults().Console.SessionTTL; got != DefaultConsoleSessionTTL {
		t.Fatalf("Defaults().Console.SessionTTL = %s, want %s", got, DefaultConsoleSessionTTL)
	}
}
